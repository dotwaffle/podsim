(function (root) {
  "use strict";
  // inflate counts native decoder output before it allocates the joined bytes.
  async function inflate(bytes, limit, signal) {
    if (bytes.byteLength > 65 * 1024 * 1024) throw new Error("Compressed state is too large.");
    const reader = new ReadableStream({
      start(controller) { controller.enqueue(bytes); controller.close(); }
    }).pipeThrough(new DecompressionStream("gzip")).getReader();
    const abort = () => { reader.cancel().catch(() => {}); };
    signal?.addEventListener("abort", abort, { once: true });
    const chunks = []; let length = 0;
    try {
      if (signal?.aborted) throw new DOMException("Aborted", "AbortError");
      for (;;) {
        const { done, value } = await reader.read();
        if (signal?.aborted) throw new DOMException("Aborted", "AbortError");
        if (done) break;
        length += value.byteLength;
        if (length > limit) { await reader.cancel(); throw new Error("Inflated state is too large."); }
        chunks.push(value);
      }
      const result = new Uint8Array(length); let offset = 0;
      for (const chunk of chunks) { result.set(chunk, offset); offset += chunk.byteLength; }
      return result;
    } finally { signal?.removeEventListener("abort", abort); reader.releaseLock(); }
  }
  // Keep at most six one-second buckets, even during a long session.
  function createMetrics(now = () => performance.now()) {
    const started = now();
    let buckets = [], connection = "Waiting", applied = null, contact = null, kind = "None", reconnects = 0, attempts = 0;
    function recent(time) {
      const second = Math.floor(time / 1000);
      buckets = buckets.filter((bucket) => bucket.second > second - 5);
      return second;
    }
    function record(event, value, frameKind) {
      const time = now(), second = recent(time);
      let bucket = buckets.at(-1);
      if (!bucket || bucket.second !== second) {
        bucket = { second, bytes: 0, updates: 0, processing: 0 };
        buckets.push(bucket);
      }
      if (event === "received") bucket.bytes += value;
      if (event === "connecting") {
        connection = "Connecting";
        if (attempts++ > 0) reconnects++;
      }
      if (event === "disconnected") connection = "Disconnected";
      if (event === "heartbeat") contact = time;
      if (event === "applied") {
        bucket.updates++; bucket.processing += value;
        applied = contact = time; kind = frameKind; connection = "Connected";
      }
    }
    function snapshot() {
      const time = now(), second = recent(time);
      // Include the partial current second in the rate denominator.
      const seconds = Math.max(0.001, (time - Math.max(started, (second - 4) * 1000)) / 1000);
      const total = buckets.reduce((a, b) => ({ bytes: a.bytes + b.bytes, updates: a.updates + b.updates, processing: a.processing + b.processing }), { bytes: 0, updates: 0, processing: 0 });
      return {
        connection, reconnects, kind, bytesPerSecond: total.bytes / seconds, updatesPerSecond: total.updates / seconds,
        processingMS: total.updates ? total.processing / total.updates : null,
        stateAgeMS: applied === null ? null : time - applied,
        contactAgeMS: contact === null ? null : time - contact,
      };
    }
    return { record, snapshot };
  }

  // Probe the same origin only while the panel is visible. One request at a time.
  function createProbe({ fetch, now, setTimeout, clearTimeout }) {
    let active = null, result = "Not measured";
    async function run() {
      if (active) return;
      const controller = new AbortController();
      active = controller;
      const started = now();
      const timer = setTimeout(() => controller.abort(), 3000);
      try {
        const response = await fetch("/healthz", { cache: "no-store", signal: controller.signal });
        if (!response.ok || (await response.text()).trim() !== "ok") throw new Error("Health probe failed");
        if (!controller.signal.aborted) result = `${Math.round(now() - started)} ms`;
      } catch {
        result = "Unavailable";
      } finally {
        clearTimeout(timer);
        active = null;
      }
    }
    return { run, value: () => result, cancel: () => active?.abort() };
  }

  function mountDiagnostics() {
    const metrics = createMetrics();
    // A diagnostic display must never break state delivery.
    root.podsimStreamDiagnostic = (...args) => { try { metrics.record(...args); } catch { /* Display only. */ } };
    const panel = document.createElement("details");
    panel.id = "streamDiagnostics";
    panel.innerHTML = `<summary>Connection diagnostics</summary>
      <dl></dl>
      <p>Rates cover about 5 seconds. Stream bytes include received gzip messages and controls, excluding HTTP assets and network headers.</p>
      <p>Apply time includes decompression, decoding, and state assembly, but not drawing. State age grows normally while paused.</p>
      <p>HTTP round-trip includes server handling. One-way delay and socket backlog are unknown; no clock synchronization is assumed.</p>`;
    document.body.append(panel);
    const list = panel.querySelector("dl");
    const probe = createProbe({ fetch: root.fetch.bind(root), now: () => performance.now(), setTimeout: root.setTimeout.bind(root), clearTimeout: root.clearTimeout.bind(root) });
    let nextProbe = 0;
    const duration = (value) => value === null ? "Not measured" : value < 1000 ? `${Math.round(value)} ms` : `${(value / 1000).toFixed(1)} s`;
    function refresh() {
      if (!panel.open || document.hidden || root.frameElement?.inert) { probe.cancel(); return; }
      const s = metrics.snapshot();
      const rows = [
        ["Stream", s.connection], ["Received", `${(s.bytesPerSecond / 1024).toFixed(1)} KiB/s`],
        ["Applied updates", `${s.updatesPerSecond.toFixed(1)} /s`], ["Mean apply time", duration(s.processingMS)],
        ["Since state update", duration(s.stateAgeMS)], ["Since state / heartbeat", duration(s.contactAgeMS)],
        ["Last frame", s.kind], ["Reconnect attempts", String(s.reconnects)], ["HTTP round-trip", probe.value()],
      ];
      list.replaceChildren(...rows.flatMap(([label, value]) => {
        const term = document.createElement("dt"), description = document.createElement("dd");
        term.textContent = label; description.textContent = value;
        return [term, description];
      }));
      if (performance.now() >= nextProbe) { nextProbe = performance.now() + 5000; void probe.run(); }
    }
    panel.addEventListener("toggle", refresh);
    document.addEventListener("visibilitychange", refresh);
    root.addEventListener("pagehide", () => probe.cancel());
    root.setInterval(refresh, 1000);
  }
  const api = { inflate, createMetrics, createProbe };
  if (typeof document === "object") document.addEventListener("DOMContentLoaded", mountDiagnostics, { once: true });
  if (typeof module === "object" && module.exports) module.exports = api;
  else root.PodsimStream = api;
})(globalThis);
