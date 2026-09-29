"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const { gzipSync } = require("node:zlib");
const { inflate } = require("./stream.js");

test("native stream inflation accepts one bounded member", async () => {
  const text = Buffer.from('{"kind":"full","value":0}');
  assert.deepEqual(Buffer.from(await inflate(gzipSync(text), text.length)), text);
});
test("native stream inflation rejects overflow and trailing members", async () => {
  const compressed = gzipSync(Buffer.alloc(1024 * 1024));
  await assert.rejects(inflate(compressed, 1024), /too large/);
  await assert.rejects(inflate(Buffer.concat([compressed, compressed]), 3 * 1024 * 1024));
  await assert.rejects(inflate(Buffer.concat([compressed, Buffer.from([0])]), 3 * 1024 * 1024));
});
test("native stream inflation honors cancellation", async () => {
  const controller = new AbortController(); controller.abort();
  await assert.rejects(inflate(gzipSync("state"), 100, controller.signal), { name: "AbortError" });
});

const { createMetrics, createProbe } = require("./stream.js");
test("diagnostics expire rates and distinguish paused state from live heartbeat", () => {
  let time = 0;
  const metrics = createMetrics(() => time);
  metrics.record("connecting");
  metrics.record("received", 2048);
  metrics.record("applied", 10, "full");
  time = 1000;
  assert.equal(metrics.snapshot().bytesPerSecond, 2048);
  assert.equal(metrics.snapshot().processingMS, 10);
  time = 6000;
  metrics.record("heartbeat");
  const s = metrics.snapshot();
  assert.equal(s.bytesPerSecond, 0);
  assert.equal(s.updatesPerSecond, 0);
  assert.equal(s.processingMS, null);
  assert.equal(s.stateAgeMS, 6000);
  assert.equal(s.contactAgeMS, 0);
  metrics.record("disconnected");
  assert.equal(metrics.snapshot().connection, "Disconnected");
  metrics.record("connecting");
  metrics.record("applied", 20, "full");
  assert.equal(metrics.snapshot().reconnects, 1);
  assert.equal(metrics.snapshot().stateAgeMS, 0);
});
test("diagnostic rate window excludes expired traffic across long sleeps", () => {
  let time = 250;
  const metrics = createMetrics(() => time);
  assert.equal(metrics.snapshot().stateAgeMS, null);
  for (let i = 0; i < 100; i++) {
    time += 1000;
    metrics.record("received", 1024);
    metrics.record("applied", 4, "delta");
  }
  time = 101000;
  assert.equal(metrics.snapshot().bytesPerSecond, 1024);
  assert.equal(metrics.snapshot().updatesPerSecond, 1);
  time += 100000;
  assert.equal(metrics.snapshot().bytesPerSecond, 0);
});
test("HTTP diagnostic probe prevents overlap and handles cancellation and errors", async () => {
  let time = 0, calls = 0, resolve;
  const probe = createProbe({
    now: () => time, setTimeout, clearTimeout,
    fetch: (_url, { signal, cache }) => {
      calls++;
      assert.equal(cache, "no-store");
      return new Promise((done, reject) => {
        resolve = done;
        signal.addEventListener("abort", () => reject(new Error("aborted")), { once: true });
      });
    },
  });
  const first = probe.run();
  await probe.run();
  assert.equal(calls, 1);
  time = 125;
  resolve({ ok: true, text: async () => "ok\n" });
  await first;
  assert.equal(probe.value(), "125 ms");
  const second = probe.run();
  probe.cancel();
  await second;
  assert.equal(probe.value(), "Unavailable");
  const third = probe.run();
  resolve({ ok: false });
  await third;
  assert.equal(probe.value(), "Unavailable");
});
