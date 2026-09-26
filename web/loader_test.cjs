"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const loader = require("./loader.js");

test("downloadTotal knows the size only for a body without content encoding", () => {
  for (const [name, headers, want] of [
    ["no headers", {}, null],
    ["length only", { "Content-Length": "28737787" }, 28737787],
    ["identity encoding", { "Content-Length": "1000", "Content-Encoding": "identity" }, 1000],
    ["gzip encoding", { "Content-Length": "6630784", "Content-Encoding": "gzip" }, null],
    ["gzip without length", { "Content-Encoding": "gzip" }, null],
    ["zero length", { "Content-Length": "0" }, null],
    ["invalid length", { "Content-Length": "many" }, null],
  ]) {
    assert.equal(loader.downloadTotal(new Headers(headers)), want, name);
  }
});

test("progressText shows a percent only when the total is known", () => {
  for (const [received, total, want] of [
    [0, null, "Downloading Podsim… 0.0 MB"],
    [12345678, null, "Downloading Podsim… 12.3 MB"],
    [28737787, null, "Downloading Podsim… 28.7 MB"],
    [0, 1000, "Downloading Podsim… 0%"],
    [259, 1000, "Downloading Podsim… 25%"],
    [1000, 1000, "Downloading Podsim… 100%"],
    [1500, 1000, "Downloading Podsim… 100%"],
  ]) {
    assert.equal(loader.progressText(received, total), want, `${received} of ${total}`);
  }
});

// bodyStream gives a body with a chunk of each size. When close is false,
// the body stays open until the test calls more(size).
function bodyStream(sizes, close = true) {
  let more;
  const stream = new ReadableStream({
    start(controller) {
      for (const size of sizes) controller.enqueue(new Uint8Array(size));
      if (close) controller.close();
      more = (size) => {
        controller.enqueue(new Uint8Array(size));
        controller.close();
      };
    },
  });
  return { stream, more };
}

// signal gives a promise and the function that resolves it.
function signal() {
  let resolve;
  const promise = new Promise((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

// settle waits until the in-memory streams have no more work.
function settle() {
  return new Promise((resolve) => setImmediate(resolve));
}

test("countBody counts a copy and keeps the response body for the caller", async () => {
  const response = new Response(bodyStream([3, 2]).stream, { headers: { "Content-Type": "application/wasm" } });
  const reports = [];
  const finished = signal();
  loader.countBody(response, (received, done) => {
    reports.push([received, done]);
    if (done) finished.resolve();
  });
  assert.equal(response.bodyUsed, false);
  assert.equal(response.headers.get("Content-Type"), "application/wasm");
  assert.equal((await response.arrayBuffer()).byteLength, 5);
  await finished.promise;
  assert.deepEqual(reports, [[3, false], [5, false], [5, true]]);
});

test("countBody does not report after the stop", async () => {
  const body = bodyStream([3], false);
  const response = new Response(body.stream);
  const reports = [];
  const first = signal();
  const stop = loader.countBody(response, (received, done) => {
    reports.push([received, done]);
    first.resolve();
  });
  await first.promise;
  stop();
  body.more(2);
  assert.equal((await response.arrayBuffer()).byteLength, 5);
  await settle();
  assert.deepEqual(reports, [[3, false]]);
});

test("countBody gives a stop function for a response without a body", () => {
  let reports = 0;
  loader.countBody(new Response(null), () => reports++)();
  assert.equal(reports, 0);
});

// fakeGo gives a Go object whose run calls program(go). The default exit
// keeps each exit code, so that a test can see that the loader still calls
// it.
function fakeGo(program = async () => {}) {
  return {
    importObject: {},
    exits: [],
    exit(code) {
      this.exits.push(code);
    },
    async run(instance) {
      assert.equal(instance, "instance");
      await program(this);
    },
  };
}

// startWith runs loader.start with fake browser steps. It gives each status
// text with its busy flag, and "started" where the program started. It also
// gives the number of errors that start wrote to the console. When
// steps.shown is set, startWith calls it with each status text.
async function startWith(t, steps) {
  const logged = t.mock.method(console, "error", () => {});
  const events = [];
  await loader.start({
    createGo: steps.createGo ?? (() => fakeGo()),
    fetchModule: steps.fetchModule ?? (async () => new Response(bodyStream([3, 2]).stream)),
    instantiate: steps.instantiate ?? (async () => ({ instance: "instance" })),
    show: (text, busy) => {
      events.push([text, busy]);
      steps.shown?.(text);
    },
    started: () => events.push("started"),
  });
  await settle();
  return { events, logged: logged.mock.callCount() };
}

test("start shows the download progress, then starts the program", async (t) => {
  const go = fakeGo();
  const counted = signal();
  const fetched = new Response(bodyStream([3, 2]).stream, { headers: { "Content-Length": "5" } });
  const { events, logged } = await startWith(t, {
    createGo: () => go,
    fetchModule: async () => fetched,
    instantiate: async (response, imports) => {
      // The browser keeps the compiled code only for the fetched response.
      assert.equal(response, fetched);
      assert.equal(imports, go.importObject);
      assert.equal((await response.arrayBuffer()).byteLength, 5);
      await counted.promise;
      return { instance: "instance" };
    },
    shown: (text) => {
      if (text === "Starting Podsim…") counted.resolve();
    },
  });
  assert.deepEqual(events, [
    ["Downloading Podsim… 60%", true],
    ["Downloading Podsim… 100%", true],
    ["Starting Podsim…", false],
    "started",
    ["Podsim stopped. Reload this page to start it again.", false],
  ]);
  assert.equal(logged, 0);
});

test("start keeps the failure text when the count ends later", async (t) => {
  const body = bodyStream([3], false);
  const counting = signal();
  const { events } = await startWith(t, {
    fetchModule: async () => new Response(body.stream, { headers: { "Content-Encoding": "gzip" } }),
    instantiate: async () => {
      await counting.promise;
      throw new Error("WebAssembly.instantiateStreaming(): length overflow");
    },
    shown: () => counting.resolve(),
  });
  body.more(2);
  await settle();
  assert.deepEqual(events, [
    ["Downloading Podsim… 0.0 MB", true],
    ["Podsim could not start. WebAssembly.instantiateStreaming(): length overflow.", false],
  ]);
});

test("start shows a start failure before the program runs", async (t) => {
  for (const [name, steps, want] of [
    ["no Go", { createGo: () => { throw new ReferenceError("Go is not defined"); } }, "Podsim could not start. Go is not defined."],
    ["no network", { fetchModule: async () => { throw new TypeError("Failed to fetch"); } }, "Podsim could not start. Failed to fetch."],
    ["missing file", { fetchModule: async () => new Response("", { status: 404 }) }, "Podsim could not start. Download failed: HTTP 404."],
    ["bad module", { instantiate: async () => { throw new WebAssembly.CompileError("expected magic word"); } }, "Podsim could not start. Expected magic word."],
  ]) {
    await t.test(name, async (t) => {
      const { events, logged } = await startWith(t, steps);
      assert.deepEqual(events.at(-1), [want, false]);
      assert.equal(events.includes("started"), false);
      assert.equal(logged, 1);
    });
  }
});

test("start shows the stop of the program after the start", async (t) => {
  for (const [name, program, exits, want] of [
    ["return from main", async () => {}, [], "Podsim stopped. Reload this page to start it again."],
    ["clean exit", async (go) => go.exit(0), [0], "Podsim stopped. Reload this page to start it again."],
    ["exit code", async (go) => go.exit(3), [3], "Podsim stopped. Exit code 3. Reload this page to start it again."],
    ["exception", async () => { throw new Error("unreachable"); }, [], "Podsim stopped. Unreachable. Reload this page to start it again."],
  ]) {
    await t.test(name, async (t) => {
      const go = fakeGo(program);
      const { events } = await startWith(t, { createGo: () => go });
      assert.deepEqual(events.slice(-2), ["started", [want, false]]);
      assert.deepEqual(go.exits, exits);
    });
  }
});

test("startFailureText and stopText give different messages", () => {
  for (const [name, got, want] of [
    ["download failure", loader.startFailureText(new Error("Download failed: HTTP 404")), "Podsim could not start. Download failed: HTTP 404."],
    ["message with a period", loader.startFailureText(new TypeError("Failed to fetch.")), "Podsim could not start. Failed to fetch."],
    ["empty message", loader.startFailureText(new Error("")), "Podsim could not start."],
    ["thrown text", loader.startFailureText("failed to fetch"), "Podsim could not start. Failed to fetch."],
    ["clean exit", loader.stopText(0, null), "Podsim stopped. Reload this page to start it again."],
    ["exit with an error", loader.stopText(1, null), "Podsim stopped. Exit code 1. Reload this page to start it again."],
    ["no exit code", loader.stopText(null, null), "Podsim stopped. Reload this page to start it again."],
    ["exception", loader.stopText(null, new Error("unreachable")), "Podsim stopped. Unreachable. Reload this page to start it again."],
  ]) {
    assert.equal(got, want, name);
  }
});
