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
            job.resolve(data.result); pump();
          };
          worker.onerror = () => fail(new Error("The Go editor model could not start. Reload the editor to try again."));
          worker.onmessageerror = () => fail(new Error("The Go editor model response could not be read."));
        }
        active.timer = clock.setTimeout(() => fail(new Error("The Go editor model timed out. Reload the editor to try again.")), timeout);
        if (active.op === "place-view") {
          worker.postMessage({ id: active.id, op: active.op, project: active.config, view: active.plan });
        } else {
          const patch = {};
          for (const key of Object.keys(active.config)) if (active.config[key] !== sent[key]) patch[key] = active.config[key];
          const message = { id: active.id, op: active.op, keys: Object.keys(active.config), patch };
          if (active.op === "park-ride") message.parkRide = active.plan;
          if (active.op === "edit") message.edit = active.plan;
          worker.postMessage(message);
          sent = active.config;
        }
      } catch (error) { fail(error); }
    }
    return {
      call(config, op = "validate", plan, { background = false } = {}) {
        if (failed) return Promise.reject(failed);
        if (background) {
          for (let index = queue.length - 1; index >= 0; index--) if (queue[index].background) {
            queue.splice(index, 1)[0].reject(new DOMException("A newer draft replaced this validation.", "AbortError"));
          }
        }
        if (queue.length >= 8) return Promise.reject(new Error("The Go editor model is busy. Try the action again."));
        return new Promise((resolve, reject) => {
          const job = { id: ++nextID, config, op, plan, background, resolve, reject };
          const pendingBackground = background ? -1 : queue.findIndex((item) => item.background);
          if (pendingBackground < 0) queue.push(job); else queue.splice(pendingBackground, 0, job);
          pump();
        });
      },
      close() { fail(new Error("The Go editor model has stopped.")); },
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

  const api = { createClient, createEditQueue, bounded };
  if (typeof module !== "undefined" && module.exports) module.exports = api;
  root.PodsimGoEditor = api;
  if (typeof document !== "undefined" || typeof root.importScripts !== "function") return;

  let config = {};
  let needsFullSync = false;
  function invoke(command) {
    const result = JSON.parse(root.podsimEditorCall(JSON.stringify(command)));
    if (!result || typeof result !== "object" || Array.isArray(result) || result.fatal) throw new Error("Invalid or fatal Go response");
    return result;
  }
  // The worker initializes Go before it accepts model operations.
  const starting = startWorker();
  root.onmessage = async ({ data }) => {
    try {
      await starting;
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
        root.postMessage({ id: data.id, result: { ...checks, valid: result.valid === true } });
      } else if (data.op === "place-view") {
        if (!result.error && (!result.view || ![result.view.x, result.view.y, result.view.scale].every(Number.isFinite) || result.view.scale <= 0)) throw new Error("Invalid Go view response");
        root.postMessage({ id: data.id, result });
      } else if (data.op === "edit") {
        if (!result.error && (!result.change || !result.change.patch || typeof result.change.patch !== "object" || Array.isArray(result.change.patch) || (result.change.flag !== undefined && !["demandEnabled", "redistribution", "stationBuffers", "pickupReassignment"].includes(result.change.flag)))) throw new Error("Invalid Go edit response");
        root.postMessage({ id: data.id, result });
      } else {
        if (!result.error && (!result.profile || typeof result.profile !== "object" || typeof result.profile.id !== "string" || !Array.isArray(result.profile.bands) || !Array.isArray(result.profile.flows) || !result.demand || typeof result.demand !== "object" || typeof result.demand.pattern !== "string")) throw new Error("Invalid Go constructor response");
        root.postMessage({ id: data.id, result });
      }
    } catch (_) { root.postMessage({ id: data.id, error: "The Go editor model is unavailable. Reload the editor to try again." }); }
  };
  starting.catch(() => root.postMessage({ fatal: "The Go editor model could not start. Reload the editor to try again." }));
})(globalThis);
