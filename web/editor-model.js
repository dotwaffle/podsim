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
        const patch = {};
        for (const key of Object.keys(active.config)) if (active.config[key] !== sent[key]) patch[key] = active.config[key];
        worker.postMessage({ id: active.id, op: active.op, keys: Object.keys(active.config), patch, parkRide: active.plan });
        sent = active.config;
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
    root.importScripts("./wasm_exec.js", "./editor.js");
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

  const api = { createClient, bounded };
  if (typeof module !== "undefined" && module.exports) module.exports = api;
  root.PodsimGoEditor = api;
  if (typeof document !== "undefined" || typeof root.importScripts !== "function") return;

  let config = {};
  // Set the handler after startWorker imports the old model reference.
  const starting = startWorker();
  root.onmessage = async ({ data }) => {
    try {
      await starting;
      config = Object.fromEntries(data.keys.map((key) => [key, Object.hasOwn(data.patch, key) ? data.patch[key] : config[key]]));
      const command = { op: data.op, project: config };
      if (data.parkRide !== undefined) command[data.op === "place-view" ? "view" : "parkRide"] = data.parkRide;
      const result = JSON.parse(root.podsimEditorCall(JSON.stringify(command)));
      if (!result || typeof result !== "object" || Array.isArray(result) || result.fatal) throw new Error("Invalid or fatal Go response");
      if (data.op === "validate") {
        if (result.valid !== true && typeof result.error !== "string") throw new Error("Missing Go validation verdict");
        const checks = root.PodsimEditorModel.checkResults(config);
        if (result.error && !checks.errors.some((item) => item.text === result.error)) checks.errors.push({ text: result.error });
        root.postMessage({ id: data.id, result: { ...checks, valid: result.valid === true } });
      } else if (data.op === "place-view") {
        if (!result.error && (!result.view || ![result.view.x, result.view.y, result.view.scale].every(Number.isFinite) || result.view.scale <= 0)) throw new Error("Invalid Go view response");
        root.postMessage({ id: data.id, result });
      } else {
        if (!result.error && (!result.profile || typeof result.profile !== "object" || typeof result.profile.id !== "string" || !Array.isArray(result.profile.bands) || !Array.isArray(result.profile.flows) || !result.demand || typeof result.demand !== "object" || typeof result.demand.pattern !== "string")) throw new Error("Invalid Go constructor response");
        root.postMessage({ id: data.id, result });
      }
    } catch (_) { root.postMessage({ id: data.id, error: "The Go editor model is unavailable. Reload the editor to try again." }); }
  };
  starting.catch(() => root.postMessage({ fatal: "The Go editor model could not start. Reload the editor to try again." }));
})(globalThis);
