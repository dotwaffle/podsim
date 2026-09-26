(function (root) {
  "use strict";

  const MEGABYTE = 1e6;

  // downloadTotal gives the size of the response body in bytes from the
  // response headers, or null when the size is not known. When the body has
  // a content encoding such as gzip, Content-Length gives the encoded size,
  // but the browser gives the decoded bytes. Then the size is not known.
  function downloadTotal(headers) {
    const encoding = (headers.get("Content-Encoding") ?? "").trim().toLowerCase();
    if (encoding !== "" && encoding !== "identity") return null;
    const length = (headers.get("Content-Length") ?? "").trim();
    if (!/^\d+$/.test(length) || Number(length) === 0) return null;
    return Number(length);
  }

  // progressText gives the loader status for received bytes of the module.
  // When total is known, the status shows a percent. Otherwise it shows the
  // number of megabytes that the browser received.
  function progressText(received, total) {
    if (total) return `Downloading Podsim… ${Math.min(100, Math.floor(100 * received / total))}%`;
    return `Downloading Podsim… ${(received / MEGABYTE).toFixed(1)} MB`;
  }

  // countBody reads a copy of the body of response and calls
  // report(received, false) after each chunk, with the number of bytes so
  // far. At the end of the body, it calls report(received, true). The
  // original response stays unread for WebAssembly.instantiateStreaming, so
  // the browser can keep the compiled module in its code cache. countBody
  // gives a function that stops the count. After the stop, report is not
  // called again.
  function countBody(response, report) {
    if (!response.body) return () => {};
    const reader = response.clone().body.getReader();
    let stopped = false;
    (async () => {
      let received = 0;
      for (;;) {
        const { done, value } = await reader.read();
        if (stopped) return;
        received += value?.byteLength ?? 0;
        report(received, done);
        if (done) return;
      }
    })().catch(() => {});
    return () => {
      stopped = true;
      reader.cancel().catch(() => {});
    };
  }

  // reasonText gives the message of error as a sentence that starts with a
  // capital letter and ends with a period, or an empty string when the
  // message is empty.
  function reasonText(error) {
    const text = String(error?.message ?? error ?? "").trim().replace(/\.?$/, ".");
    return text === "." ? "" : `${text.charAt(0).toUpperCase()}${text.slice(1)}`;
  }

  // startFailureText gives the loader status when the page cannot download,
  // compile or start the module.
  function startFailureText(error) {
    return ["Podsim could not start.", reasonText(error)].filter(Boolean).join(" ");
  }

  // stopText gives the loader status when the Go program stops after it
  // started. exitCode is the exit code of the program, or null when it did
  // not exit. error is the exception that stopped the program, or null. The
  // program writes its own error to the browser console before it exits.
  function stopText(exitCode, error) {
    const reason = error ? reasonText(error) : exitCode ? `Exit code ${exitCode}.` : "";
    return ["Podsim stopped.", reason, "Reload this page to start it again."].filter(Boolean).join(" ");
  }

  // start downloads, compiles and runs the Go program. The page gives the
  // browser steps as functions, so that tests can replace them:
  //   createGo() gives a new Go object from wasm_exec.js.
  //   fetchModule() gives the response for the module.
  //   instantiate(response, imports) is WebAssembly.instantiateStreaming.
  //   show(text, busy) shows the status text. busy is true while the
  //   download counter changes.
  //   started() hides the status when the program starts.
  // While the module downloads, the status shows the progress. If the start
  // fails, the status shows startFailureText. If the program stops later,
  // the status shows stopText. start resolves after the failure or the stop.
  async function start({ createGo, fetchModule, instantiate, show, started }) {
    let go;
    let instance;
    let stopCount = () => {};
    try {
      go = createGo();
      const response = await fetchModule();
      if (!response.ok) throw new Error(`Download failed: HTTP ${response.status}`);
      const total = downloadTotal(response.headers);
      stopCount = countBody(response, (received, done) => {
        show(done ? "Starting Podsim…" : progressText(received, total), !done);
      });
      ({ instance } = await instantiate(response, go.importObject));
    } catch (error) {
      stopCount();
      show(startFailureText(error), false);
      console.error(error);
      return;
    }
    stopCount();
    let exitCode = null;
    const exit = go.exit;
    go.exit = (code) => {
      exitCode = code;
      exit.call(go, code);
    };
    const running = go.run(instance);
    started();
    try {
      await running;
      show(stopText(exitCode, null), false);
    } catch (error) {
      show(stopText(exitCode, error), false);
      console.error(error);
    }
  }

  const API = { downloadTotal, progressText, countBody, startFailureText, stopText, start };
  if (typeof module !== "undefined" && module.exports) module.exports = API;
  root.PodsimLoader = API;
})(typeof window !== "undefined" ? window : globalThis);
