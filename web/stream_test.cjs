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
