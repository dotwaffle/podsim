"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const { createClient, createEditQueue, bounded } = require("./editor-model.js");

function fixture() {
  const sent = [], timers = new Map();
  let nextTimer = 0, terminated = false;
  const worker = { postMessage: (data) => sent.push(data), terminate: () => { terminated = true; } };
  const clock = { setTimeout(fn) { const id = ++nextTimer; timers.set(id, fn); return id; }, clearTimeout(id) { timers.delete(id); } };
  const client = createClient({ makeWorker: () => worker, clock });
  return { client, sent, worker, timers, get terminated() { return terminated; } };
}

test("model client serializes jobs and transfers only changed project fields", async () => {
  const f = fixture();
  const network = {}, first = { network, demand: { seed: 1 }, map: {} }, next = { network, demand: { seed: 2 } };
  const one = f.client.call(first), two = f.client.call(next, "park-ride", { name: "Plan" });
  assert.equal(f.sent.length, 1);
  assert.deepEqual(f.sent[0].patch, first);
  f.worker.onmessage({ data: { id: 99, result: { valid: false } } });
  assert.equal(f.sent.length, 1);
  f.worker.onmessage({ data: { id: f.sent[0].id, result: { valid: true } } });
  assert.deepEqual(await one, { valid: true });
  assert.equal(f.sent.length, 2);
  assert.deepEqual(f.sent[1].patch, { demand: next.demand });
  assert.deepEqual(f.sent[1].keys, ["network", "demand"]);
  assert.equal(f.sent[1].op, "park-ride");
  assert.deepEqual(f.sent[1].parkRide, { name: "Plan" });
  f.worker.onmessage({ data: { id: f.sent[1].id, result: { error: "Invalid plan" } } });
  assert.deepEqual(await two, { error: "Invalid plan" });
  assert.equal(f.timers.size, 0);
});

test("browser edit queue orders actions and waits before dependent work", async () => {
  const queue = createEditQueue(), order = [];
  let release;
  const one = queue.submit(async (current) => { order.push("first"); await new Promise((resolve) => { release = resolve; }); assert.equal(current(), true); order.push("commit first"); return true; });
  const two = queue.submit(async () => { order.push("second"); return true; });
  const flushed = queue.flush().then((ok) => { order.push("flushed"); return ok; });
  await Promise.resolve();
  assert.deepEqual(order, ["first"]); assert.equal(queue.pending, true); assert.equal(queue.revision, 2);
  release(); assert.equal(await one, true); assert.equal(await two, true); assert.equal(await flushed, true);
  assert.deepEqual(order, ["first", "commit first", "second", "flushed"]);
});

test("canceling edits invalidates active replies, drops queued intents, and permits new work", async () => {
  const queue = createEditQueue(); let release, committed = 0;
  const first = queue.submit(async (current) => { await new Promise((resolve) => { release = resolve; }); if (!current()) return false; committed++; return true; });
  const dropped = queue.submit(() => { assert.fail("canceled action ran"); });
  await Promise.resolve(); queue.cancel(); assert.equal(queue.revision, 3);
  assert.equal(await dropped, false);
  const latest = queue.submit(() => { committed++; return true; });
  release(); assert.equal(await first, false); assert.equal(await latest, true); assert.equal(committed, 1);
});

test("coalesced edits retain the final intent after more inputs than the queue holds", async () => {
  const queue = createEditQueue(), order = [], pending = [];
  let release;
  const first = queue.submit(async () => { await new Promise((resolve) => { release = resolve; }); order.push("active"); return true; });
  await Promise.resolve();
  for (let value = 0; value < 20; value++) {
    pending.push(queue.submit(() => { order.push(value); return true; }, { key: "opacity" }));
    if (value === 9) pending.push(queue.submit(() => { order.push("other edit"); return true; }));
  }
  const flushed = queue.flush();
  release();
  assert.equal(await first, true);
  const results = await Promise.all(pending);
  assert.equal(results.filter(Boolean).length, 2);
  assert.equal(await flushed, true);
  assert.deepEqual(order, ["active", "other edit", 19]);
});

test("cancellation drops the final coalesced edit without publishing older values", async () => {
  const queue = createEditQueue();
  let release;
  const active = queue.submit(async (current) => { await new Promise((resolve) => { release = resolve; }); return current(); });
  const old = queue.submit(() => assert.fail("old edit ran"), { key: "opacity" });
  const latest = queue.submit(() => assert.fail("latest edit ran"), { key: "opacity" });
  await Promise.resolve(); queue.cancel(); release();
  assert.deepEqual(await Promise.all([active, old, latest]), [false, false, false]);
});

test("edit failures block a waiting action and do not poison later edits", async () => {
  const queue = createEditQueue();
  const rejected = assert.rejects(queue.submit(() => { throw new Error("Rejected edit"); }), /Rejected edit/);
  assert.equal(await queue.flush(), false); await rejected;
  assert.equal(await queue.submit(() => true), true);
  assert.equal(await queue.flush(), true);
});

test("browser edit queue bounds pending intentions", async () => {
  const queue = createEditQueue(); let release;
  const first = queue.submit(async (current) => { await new Promise((resolve) => { release = resolve; }); return current(); });
  const pending = Array.from({ length: 8 }, () => queue.submit(() => true));
  await assert.rejects(queue.submit(() => true), /busy/);
  await Promise.resolve(); queue.cancel(); release();
  assert.equal(await first, false); assert.deepEqual(await Promise.all(pending), Array(8).fill(false));
});

test("model client fails active and queued jobs on startup, transport, timeout, or shutdown", async (t) => {
  for (const [name, trigger] of [
    ["startup", (f) => f.worker.onerror()],
    ["transport", (f) => f.worker.onmessageerror()],
    ["fatal", (f) => f.worker.onmessage({ data: { fatal: "Model stopped" } })],
    ["timeout", (f) => [...f.timers.values()][0]()],
    ["shutdown", (f) => f.client.close()],
  ]) await t.test(name, async () => {
    const f = fixture();
    const one = assert.rejects(f.client.call({}), Error), two = assert.rejects(f.client.call({}), Error);
    trigger(f); await Promise.all([one, two]);
    assert.equal(f.terminated, true); assert.equal(f.timers.size, 0);
    await assert.rejects(f.client.call({}), Error);
    assert.equal(f.sent.length, 1);
  });
});

test("model client bounds pending work", async () => {
  const f = fixture();
  const pending = Array.from({ length: 9 }, () => assert.rejects(f.client.call({}), Error));
  await assert.rejects(f.client.call({}), /busy/);
  f.client.close(); await Promise.all(pending);
});

test("module stream rejects excess bytes and retains valid chunks", async () => {
  const stream = () => new ReadableStream({ start(controller) { controller.enqueue(new Uint8Array([1, 2])); controller.enqueue(new Uint8Array([3])); controller.close(); } });
  assert.deepEqual([...new Uint8Array(await new Response(bounded(stream(), 3)).arrayBuffer())], [1, 2, 3]);
  await assert.rejects(new Response(bounded(stream(), 2)).arrayBuffer(), /too large/);
});

test("explicit work runs before queued background validation", async () => {
  const f = fixture();
  const active = f.client.call({ name: "Active" }, "validate", undefined, { background: true });
  const superseded = assert.rejects(f.client.call({ name: "Old" }, "validate", undefined, { background: true }), { name: "AbortError" });
  const latest = f.client.call({ name: "Latest" }, "validate", undefined, { background: true });
  const explicit = f.client.call({ name: "Apply" });
  f.worker.onmessage({ data: { id: f.sent[0].id, result: { valid: true } } });
  await active;
  assert.equal(f.sent[1].patch.name, "Apply");
  f.worker.onmessage({ data: { id: f.sent[1].id, result: { valid: true } } });
  await explicit;
  assert.equal(f.sent[2].patch.name, "Latest");
  f.worker.onmessage({ data: { id: f.sent[2].id, result: { valid: true } } });
  await latest; await superseded;
});

test("malformed model results fail all waiters", async () => {
  const f = fixture();
  const rejected = assert.rejects(f.client.call({}), /invalid/);
  f.worker.onmessage({ data: { id: f.sent[0].id } });
  await rejected;
  assert.equal(f.terminated, true);
});

test("place navigation does not replace the project transfer baseline", async () => {
  const f = fixture(), network = {}, first = { name: "Draft", network };
  const initial = f.client.call(first);
  f.worker.onmessage({ data: { id: f.sent[0].id, result: { valid: true } } }); await initial;
  const place = f.client.call({ geo: { latitude: 0 } }, "place-view", { width: 900 });
  assert.deepEqual(f.sent[1].project, { geo: { latitude: 0 } });
  assert.equal(f.sent[1].patch, undefined);
  f.worker.onmessage({ data: { id: f.sent[1].id, result: { view: { x: 0, y: 0, scale: 1 } } } }); await place;
  const after = f.client.call({ name: "Renamed", network });
  assert.deepEqual(f.sent[2].patch, { name: "Renamed" });
  f.worker.onmessage({ data: { id: f.sent[2].id, result: { valid: true } } }); await after;
});

test("proposed edits use edit parameters and retain the transfer baseline until accepted", async () => {
  const f = fixture(), network = {}, original = { name: "Original", network };
  const command = { field: "name", value: "Proposed" };
  const pending = f.client.call(original, "edit", command);
  assert.deepEqual(f.sent[0].edit, command);
  assert.equal(Object.hasOwn(f.sent[0], "parkRide"), false);
  f.worker.onmessage({ data: { id: f.sent[0].id, result: { change: { patch: { name: "Proposed" } } } } });
  assert.deepEqual(await pending, { change: { patch: { name: "Proposed" } } });
  const discarded = f.client.call(original);
  assert.deepEqual(f.sent[1].patch, {});
  assert.equal(Object.hasOwn(f.sent[1], "edit"), false);
  f.worker.onmessage({ data: { id: f.sent[1].id, result: { valid: true } } }); await discarded;
  const accepted = f.client.call({ ...original, name: "Proposed" });
  assert.deepEqual(f.sent[2].patch, { name: "Proposed" });
  f.worker.onmessage({ data: { id: f.sent[2].id, result: { valid: true } } }); await accepted;
});
