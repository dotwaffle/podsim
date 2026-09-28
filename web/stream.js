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
  const api = { inflate };
  if (typeof module === "object" && module.exports) module.exports = api;
  else root.PodsimStream = api;
})(globalThis);
