(function (root) {
  "use strict";

  // createClient sends one model operation at a time and rejects stale transport replies.
  function createClient({ makeWorker, clock = root, timeout = 30000 }) {
    let worker = null, active = null, failed = null, nextID = 0, sent = {};
    const queue = [];
    function fail(error) {
      if (failed) return;
      failed = error instanceof Error ? error : new Error(String(error));
      worker?.terminate(); worker = null;
      if (active) { clock.clearTimeout(active.timer); active.reject(failed); active = null; }
      for (const job of queue.splice(0)) job.reject(failed);
    }
    function pump() {
      if (active || failed || !queue.length) return;
      active = queue.shift();
      try {
        if (!worker) {
          worker = makeWorker();
          worker.onmessage = ({ data }) => {
            if (data?.fatal) { fail(new Error(data.fatal)); return; }
            if (!active || data?.id !== active.id) return;
            if (!data.error && (!data.result || typeof data.result !== "object" || Array.isArray(data.result))) { fail(new Error("The Go editor model response is invalid.")); return; }
            const job = active; active = null; clock.clearTimeout(job.timer);
            if (data.error) { job.reject(new Error(data.error)); fail(new Error(data.error)); return; }
            if (job.op === "history" && job.plan.action === "accept" && !data.result.error) sent = job.config;
            job.resolve(data.result); pump();
          };
          worker.onerror = () => fail(new Error("The Go editor model could not start. Reload the editor to try again."));
          worker.onmessageerror = () => fail(new Error("The Go editor model response could not be read."));
        }
        active.timer = clock.setTimeout(() => fail(new Error("The Go editor model timed out. Reload the editor to try again.")), timeout);
        if (active.op === "history" && !historyCreates(active.plan)) {
          worker.postMessage({ id: active.id, op: active.op, history: active.plan });
        } else if (helperOperation(active.op)) {
          const message = { id: active.id, op: active.op };
          if (active.op === "backgroundMetadata") message.metadata = active.plan;
          else { message.project = active.config; if (active.op === "stationLayout") message.layout = active.plan; }
          worker.postMessage(message);
        } else if (active.op === "place-view") {
          worker.postMessage({ id: active.id, op: active.op, project: active.config, view: active.plan });
        } else {
          const patch = {};
          for (const key of Object.keys(active.config)) if (active.config[key] !== sent[key]) patch[key] = active.config[key];
          const message = { id: active.id, op: active.op, keys: Object.keys(active.config), patch };
          if (active.op === "park-ride") message.parkRide = active.plan;
          if (active.op === "edit") message.edit = active.plan;
          if (active.op === "history") message.history = active.plan;
          worker.postMessage(message);
          sent = active.config;
        }
      } catch (error) { fail(error); }
    }
    function enqueue(config, op, plan, background) {
      if (failed) throw failed;
      if (background) {
        for (let index = queue.length - 1; index >= 0; index--) if (queue[index].background) {
          queue.splice(index, 1)[0].reject(new DOMException("A newer draft replaced this validation.", "AbortError"));
        }
      }
      if (op === "stationLayout") for (let index = queue.length - 1; index >= 0; index--) {
        if (queue[index].op === "stationLayout") queue.splice(index, 1)[0].reject(new DOMException("A newer selection replaced this layout inspection.", "AbortError"));
      }
      if (queue.length >= 8) throw new Error("The Go editor model is busy. Try the action again.");
      return new Promise((resolve, reject) => {
        const job = { id: ++nextID, config, op, plan, background, resolve, reject };
        const pendingBackground = background ? -1 : queue.findIndex((item) => item.background);
        if (pendingBackground < 0) queue.push(job); else queue.splice(pendingBackground, 0, job);
        pump();
      });
    }
    return {
      call(config, op = "validate", plan, { background = false } = {}) {
        try { return enqueue(config, op, plan, background); } catch (error) { return Promise.reject(error); }
      },
      // A throw means acceptance was not queued and the draft must not publish.
      acceptHistory(config, token) {
        for (let index = queue.length - 1; index >= 0; index--) {
          const job = queue[index];
          if (job.background && (Object.keys(job.config).length !== Object.keys(config).length || Object.keys(config).some((key) => job.config[key] !== config[key]))) {
            queue.splice(index, 1)[0].reject(new DOMException("A newer draft replaced this validation.", "AbortError"));
          }
        }
        return enqueue(config, "history", { action: "accept", token }, false);
      },
      close() { fail(new Error("The Go editor model has stopped.")); },
    };
  }

  function helperOperation(op) { return ["backgroundMetadata", "canonicalImport", "stationLayout"].includes(op); }

  function checkedHelper(result, op) {
    if (!result || typeof result !== "object" || Array.isArray(result)) throw new Error("Invalid Go helper response");
    if (result.error !== undefined) {
      if (typeof result.error !== "string" || !result.error || Object.keys(result).some((key) => !["error", "valid"].includes(key)) || (Object.hasOwn(result, "valid") && result.valid !== false)) throw new Error("Invalid Go helper error");
      return result;
    }
    const object = (value) => value !== null && typeof value === "object" && !Array.isArray(value);
    const exact = (value, keys) => object(value) && Object.keys(value).length === keys.length && keys.every((key) => Object.hasOwn(value, key));
    if (op === "backgroundMetadata") {
      const metadata = result.metadata;
      if (!exact(result, ["valid", "metadata"]) || result.valid !== true || !object(metadata) || Object.keys(metadata).some((key) => !["placement", "asset"].includes(key)) ||
          !exact(metadata.asset, ["frameState", "frame", "license"]) || !["none", "attached", "detached"].includes(metadata.asset.frameState) ||
          !(metadata.asset.frame === null || exact(metadata.asset.frame, ["source", "south", "north", "west", "east"]) && ["equirectangular", "web-mercator"].includes(metadata.asset.frame.source) && ["south", "north", "west", "east"].every((key) => Number.isFinite(metadata.asset.frame[key]))) ||
          (metadata.asset.frameState === "none") !== (metadata.asset.frame === null) ||
          !(metadata.asset.license === null || exact(metadata.asset.license, ["source", "attribution", "license", "licenseURL", "copyrightURL", "retrieved", "method", "notice"]) && Object.values(metadata.asset.license).every((value) => typeof value === "string")) ||
          (metadata.placement !== undefined && (!exact(metadata.placement, ["x", "y", "width", "height", "opacity"]) || !Object.values(metadata.placement).every(Number.isFinite) || metadata.placement.width <= 0 || metadata.placement.height <= 0 || metadata.placement.opacity < 0 || metadata.placement.opacity > 1))) throw new Error("Invalid Go metadata response");
    } else if (op === "canonicalImport") {
      if (!exact(result, []) && !(exact(result, ["replace"]) && object(result.replace))) throw new Error("Invalid Go import response");
    } else {
      const keys = ["pitch", "spacing", "setback", "approachLength", "departureLength"];
      if (!exact(result, ["layout"]) || !exact(result.layout, keys) || !keys.every((key) => {
        const field = result.layout[key];
        return exact(field, ["value", "reason"]) && typeof field.reason === "string" && (field.value === null ? field.reason.length > 0 : Number.isFinite(field.value) && field.value > 0 && field.reason === "");
      })) throw new Error("Invalid Go layout response");
    }
    return result;
  }

  function historyCreates(command) {
    return command?.action === "prepare" && ["reset", "replace", "commitFrom"].includes(command.kind);
  }

  function checkedHistory(result, proposal) {
    if (result.error) throw new Error(result.error);
    const view = result.history;
    if (!view || typeof view.head !== "string" || !view.head || typeof view.revision !== "string" || !/^[1-9][0-9]*$/.test(view.revision) ||
        ![view.changed, view.canUndo, view.canRedo].every((value) => typeof value === "boolean") ||
        !Array.isArray(view.retained) || !view.retained.includes(view.head) || !view.retained.every((id) => typeof id === "string" && id) || new Set(view.retained).size !== view.retained.length ||
        !Array.isArray(view.imageKeys) || !view.imageKeys.every((key) => typeof key === "string" && key) ||
        !(view.background === null || view.background && typeof view.background === "object" && !Array.isArray(view.background)) ||
        (proposal ? typeof view.proposal !== "string" || !view.proposal : view.proposal !== undefined)) throw new Error("The Go history response is invalid.");
    return view;
  }

  function historySignature(view) {
    return JSON.stringify([view.head, view.revision, view.changed, view.canUndo, view.canRedo, view.retained, view.imageKeys, view.background]);
  }

  // createHistory stores render copies by Go IDs. Go owns the timeline.
  function createHistory({ initial, own, clone, call, accept, captureGuard = () => () => true, onChange = () => {}, onAcknowledged = () => {}, onFatal = () => {} }) {
    const cache = new Map(), identities = new WeakMap();
    let present = own(initial), view = null, heldKeys = [], generation = 0, pending = 0, failed = null;
    let tail = Promise.resolve();
    function fatal(error) {
      if (!failed) { failed = error instanceof Error ? error : new Error(String(error)); onFatal(failed); }
      return failed;
    }
    function identify(value) { if (view) identities.set(value, view.head); return value; }
    function notify(checksUnchanged) { onChange(checksUnchanged); }
    async function discard(token) {
      try {
        const result = await call(present.scenario, "history", { action: "discard", token });
        if (result.error) throw new Error(result.error);
      } catch (error) { throw fatal(error); }
    }
    async function prepare(config, command) {
      let result;
      try { result = await call(config, "history", command); } catch (error) { throw fatal(error); }
      if (result.error) return result;
      try { return { history: checkedHistory(result, true) }; } catch (error) { throw fatal(error); }
    }
    function run(kind, target, extra = {}, { current = () => true, beforePublish = () => {}, planTrim } = {}, checksUnchanged = false) {
      const ticket = generation;
      const candidate = target === undefined ? null : own(target, present);
      pending++;
      const work = tail.then(async () => {
        if (failed) throw failed;
        if (ticket !== generation || !current()) return false;
        const source = present;
        const unchanged = captureGuard(kind);
        if (!unchanged()) return false;
        const command = { action: "prepare", kind, revision: view?.revision || "0", ...extra };
        if (candidate) command.background = candidate.background;
        const result = await prepare(candidate?.scenario || present.scenario, command);
        if (result.error) throw new Error(result.error);
        let prepared;
        try { prepared = checkedHistory(result, true); } catch (error) { throw fatal(error); }
        if (ticket !== generation || !current()) {
          await discard(prepared.proposal);
          return false;
        }
        if (source !== present || !unchanged()) {
          await discard(prepared.proposal);
          throw new Error("The draft changed while history was prepared. Try the action again.");
        }
        if (planTrim) {
          let trim;
          try {
            trim = planTrim(prepared, (id) => id === prepared.head ? candidate : cache.get(id));
            if (!Number.isSafeInteger(trim) || trim < 0) throw new Error("The history trim count is invalid.");
          } catch (error) { await discard(prepared.proposal); throw error; }
          if (trim) {
            const trimmed = await prepare(candidate.scenario, { ...command, trimOldest: trim });
            if (trimmed.error) { await discard(prepared.proposal); throw new Error(trimmed.error); }
            try { prepared = checkedHistory(trimmed, true); } catch (error) { throw fatal(error); }
          }
          if (ticket !== generation || !current()) { await discard(prepared.proposal); return false; }
          if (source !== present || !unchanged()) {
            await discard(prepared.proposal);
            throw new Error("The draft changed while history was prepared. Try the action again.");
          }
        }
        const next = cache.get(prepared.head) || candidate;
        if (!next) throw fatal(new Error("The Go history snapshot is missing from the render cache."));
        let acknowledgment;
        try { acknowledgment = accept(next.scenario, prepared.proposal); }
        catch (error) {
          await discard(prepared.proposal);
          throw error;
        }
        // Acceptance is irrevocable. Image admission can release excluded bytes.
        heldKeys = [...new Set([...heldKeys, ...prepared.imageKeys])];
        cache.set(prepared.head, next); present = next;
        view = { ...prepared }; delete view.proposal;
        identify(present);
        try {
          beforePublish(prepared.changed, prepared);
          if (prepared.changed && kind !== "dropOldest") notify(checksUnchanged);
          const accepted = checkedHistory(await acknowledgment, false);
          if (historySignature(accepted) !== historySignature(prepared)) throw new Error("The Go history acknowledgment does not match its proposal.");
          heldKeys = accepted.imageKeys;
          const retained = new Set(accepted.retained);
          for (const id of cache.keys()) if (!retained.has(id)) cache.delete(id);
          onAcknowledged();
          return prepared.changed;
        } catch (error) {
          await acknowledgment.catch(() => {});
          throw fatal(error);
        }
      });
      const complete = work.finally(() => { pending--; });
      tail = complete.catch(() => {});
      return complete;
    }
    return {
      get value() { return identify(clone(present)); },
      get snapshot() { return present; },
      get scenarioText() { return JSON.stringify(present.scenario); },
      get background() { return present.background ? clone(present.background) : null; },
      get canUndo() { return Boolean(view?.canUndo); },
      get canRedo() { return Boolean(view?.canRedo); },
      get pending() { return pending > 0; },
      get failed() { return failed; },
      replace(next, record = true, checksUnchanged = false, options) { return run("replace", next, { record }, options, checksUnchanged); },
      reset(next, options) { return run("reset", next, {}, options); },
      commitFrom(before, next, options) {
        const id = identities.get(before);
        if (!id) return Promise.reject(new Error("The history gesture has no starting snapshot."));
        return run("commitFrom", next, { before: id }, options);
      },
      undo(options) { return run("undo", undefined, {}, options); },
      redo(options) { return run("redo", undefined, {}, options); },
      dropOldest(options) { return run("dropOldest", undefined, {}, options); },
      keys() { return new Set([...heldKeys, present.background?.imageKey].filter(Boolean)); },
      // Local previews change drawing only. They do not advance Go history.
      preview(next) {
        if (failed) return false;
        const owned = own(next, present);
        if (owned === present) return false;
        present = owned; identify(present); notify(false); return true;
      },
      cancel() { generation++; },
      async flush() { await tail; if (failed) throw failed; },
    };
  }

  // createEditQueue orders browser actions that depend on pending Go replies.
  // Cancellation invalidates active replies and drops actions that have not started.
  // A keyed action replaces its unsent predecessor and moves to the newest position.
  function createEditQueue({ onChange = () => {} } = {}) {
    let active = null, generation = 0, revision = 0;
    const queue = [];
    function pump() {
      if (active || !queue.length) return;
      const job = active = queue.shift();
      Promise.resolve().then(() => job.action(() => job.generation === generation))
        .then(job.resolve, job.reject).finally(() => { active = null; onChange(); pump(); });
    }
    return {
      get pending() { return Boolean(active || queue.length); },
      get revision() { return revision; },
      submit(action, { key } = {}) {
        const replaced = key === undefined ? -1 : queue.findIndex((job) => job.key === key);
        if (replaced >= 0) queue.splice(replaced, 1)[0].resolve(false);
        if (queue.length >= 8) return Promise.reject(new Error("The editor is busy. Try the action again."));
        revision++;
        let resolve, reject;
        const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
        queue.push({ action, key, generation, promise, resolve, reject }); pump(); onChange();
        return promise;
      },
      async flush() {
        const jobs = active ? [active, ...queue] : [...queue];
        const results = await Promise.all(jobs.map((job) => job.promise.catch(() => false)));
        return results.every((result) => result !== false);
      },
      cancel() { generation++; revision++; for (const job of queue.splice(0)) job.resolve(false); onChange(); },
    };
  }

  // bounded limits streamed bytes before the browser retains or compiles them.
  function bounded(stream, limit) {
    let count = 0;
    return stream.pipeThrough(new TransformStream({ transform(chunk, controller) {
      count += chunk.byteLength;
      if (count > limit) throw new Error("The Go editor module is too large.");
      controller.enqueue(chunk);
    } }));
  }

  async function startWorker() {
    if (typeof DecompressionStream !== "function" || typeof WebAssembly !== "object") throw new Error("This browser needs WebAssembly and native gzip decompression.");
    root.importScripts("./wasm_exec.js");
    const go = new root.Go();
    const response = await fetch("./editor-model.wasm.gz");
    if (!response.ok || !response.body) throw new Error("The Go editor module could not be downloaded.");
    const decoded = bounded(bounded(response.body, 8 * 1024 * 1024).pipeThrough(new DecompressionStream("gzip")), 32 * 1024 * 1024);
    const moduleResponse = new Response(decoded, { headers: { "Content-Type": "application/wasm" } });
    const { instance } = await WebAssembly.instantiateStreaming(moduleResponse, go.importObject);
    const running = go.run(instance);
    running.then(() => root.postMessage({ fatal: "The Go editor model stopped. Reload the editor to try again." }), () => root.postMessage({ fatal: "The Go editor model failed. Reload the editor to try again." }));
    if (typeof root.podsimEditorCall !== "function") throw new Error("The Go editor model did not initialize.");
  }

  // createOperations retains transport copies by Go IDs without a JS timeline.
  function createOperations(invoke) {
    let config = {}, needsFullSync = false, proposal = null;
    const snapshots = new Map();
    function historyOperation(data) {
      const command = data.history;
      if (historyCreates(command)) {
        config = Object.fromEntries(data.keys.map((key) => [key, Object.hasOwn(data.patch, key) ? data.patch[key] : config[key]]));
        const synced = invoke({ op: "sync", keys: data.keys, patch: needsFullSync ? config : data.patch });
        if (synced.valid !== true && typeof synced.error !== "string") throw new Error("Missing Go synchronization verdict");
        needsFullSync = !!synced.error;
        if (synced.error && synced.synced !== true) return synced;
      }
      const result = invoke({ op: "history", history: command });
      if (result.error) return result;
      if (command.action === "prepare") {
        const view = checkedHistory(result, true);
        const target = snapshots.get(view.head) || (historyCreates(command) ? config : null);
        if (!target) throw new Error("Missing Go history transport snapshot");
        proposal = { token: view.proposal, head: view.head, config: target };
      } else if (command.action === "accept") {
        const view = checkedHistory(result, false);
        if (!proposal || proposal.token !== command.token || proposal.head !== view.head) throw new Error("Invalid Go history transport acceptance");
        snapshots.set(view.head, proposal.config);
        config = proposal.config; needsFullSync = false; proposal = null;
        const retained = new Set(view.retained);
        for (const id of snapshots.keys()) if (!retained.has(id)) snapshots.delete(id);
      } else if (command.action === "discard") proposal = null;
      return result;
    }
    return { handle(data) {
      if (helperOperation(data.op)) {
        const command = { ...data }; delete command.id;
        return checkedHelper(invoke(command), data.op);
      }
      if (data.op === "history") return historyOperation(data);
      let result, synchronized = false;
      if (data.op === "place-view") result = invoke({ op: data.op, project: data.project, view: data.view });
      else {
        config = Object.fromEntries(data.keys.map((key) => [key, Object.hasOwn(data.patch, key) ? data.patch[key] : config[key]]));
        result = invoke({ op: "sync", keys: data.keys, patch: needsFullSync ? config : data.patch });
        if (result.valid !== true && typeof result.error !== "string") throw new Error("Missing Go synchronization verdict");
        needsFullSync = !!result.error;
        if (!result.error) {
          synchronized = true;
          const command = { op: data.op };
          if (data.parkRide !== undefined) command.parkRide = data.parkRide;
          if (data.edit !== undefined) command.edit = data.edit;
          result = invoke(command);
        } else if (data.op === "edit") {
          // An edit can repair a malformed draft without using older worker state.
          result = invoke({ op: "edit", project: config, edit: data.edit });
        }
      }
      if (data.op === "validate") {
        if (result.valid !== true && typeof result.error !== "string") throw new Error("Missing Go validation verdict");
        const checked = invoke(synchronized ? { op: "checks" } : { op: "checks", project: config });
        const checks = checked.checks || { errors: [{ text: checked.error || "The Go editor checks failed.", target: null }], warnings: [] };
        if (!Array.isArray(checks.errors) || !Array.isArray(checks.warnings) || ![...checks.errors, ...checks.warnings].every((row) => row && typeof row.text === "string" && row.text.length > 0)) throw new Error("Invalid Go check response");
        if (result.error && !checks.errors.some((item) => item.text === result.error)) checks.errors.push({ text: result.error });
        return { ...checks, valid: result.valid === true };
      } else if (data.op === "place-view") {
        if (!result.error && (!result.view || ![result.view.x, result.view.y, result.view.scale].every(Number.isFinite) || result.view.scale <= 0)) throw new Error("Invalid Go view response");
        return result;
      } else if (data.op === "edit") {
        if (!result.error && (!result.change || !result.change.patch || typeof result.change.patch !== "object" || Array.isArray(result.change.patch) || (result.change.flag !== undefined && !["demandEnabled", "redistribution", "stationBuffers", "pickupReassignment"].includes(result.change.flag)))) throw new Error("Invalid Go edit response");
        return result;
      } else {
        if (!result.error && (!result.profile || typeof result.profile !== "object" || typeof result.profile.id !== "string" || !Array.isArray(result.profile.bands) || !Array.isArray(result.profile.flows) || !result.demand || typeof result.demand !== "object" || typeof result.demand.pattern !== "string")) throw new Error("Invalid Go constructor response");
        return result;
      }
    } };
  }

  const api = { createClient, createEditQueue, createHistory, createOperations, checkedHelper, bounded };
  if (typeof module !== "undefined" && module.exports) module.exports = api;
  root.PodsimGoEditor = api;
  if (typeof document !== "undefined" || typeof root.importScripts !== "function") return;

  function invoke(command) {
    const result = JSON.parse(root.podsimEditorCall(JSON.stringify(command)));
    if (!result || typeof result !== "object" || Array.isArray(result) || result.fatal) throw new Error("Invalid or fatal Go response");
    return result;
  }
  const operations = createOperations(invoke);
  // The worker initializes Go before it accepts model operations.
  const starting = startWorker();
  root.onmessage = async ({ data }) => {
    try {
      await starting;
      root.postMessage({ id: data.id, result: operations.handle(data) });
    } catch (_) { root.postMessage({ id: data.id, error: "The Go editor model is unavailable. Reload the editor to try again." }); }
  };
  starting.catch(() => root.postMessage({ fatal: "The Go editor model could not start. Reload the editor to try again." }));
})(globalThis);
