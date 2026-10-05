"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const { createHistory, createOperations, createClient } = require("./editor-model.js");
const turn = () => new Promise((resolve) => setImmediate(resolve));
const clone = (value) => JSON.parse(JSON.stringify(value));
function freeze(value) { if (value && typeof value === "object") { for (const child of Object.values(value)) freeze(child); Object.freeze(value); } return value; }
const own = (value) => freeze(clone(value));
const draft = (name, imageKey) => ({ scenario: { name, network: { nodes: [{ id: "one", position: { x: 1, y: 2 } }] } }, background: imageKey ? { imageKey, opacity: .45 } : null });
const metadata = (head, revision, { retained = [head], imageKeys = [], canUndo = false, canRedo = false, changed = true, background = null } = {}) => ({ head, revision: String(revision), changed, canUndo, canRedo, retained, imageKeys, background });
const prepared = (view, token = `p${view.revision}`) => ({ history: { ...view, proposal: token } });
const deferred = () => { let resolve, reject; const promise = new Promise((yes, no) => { resolve = yes; reject = no; }); return { promise, resolve, reject }; };
function fixture() {
  const calls = [], accepts = [], changes = [], acks = [], fatals = [];
  let refused = null;
  const history = createHistory({ initial: draft("Initial"), own, clone,
    call(config, op, command) { const job = { config, op, command, ...deferred() }; calls.push(job); return job.promise; },
    accept(config, token) { if (refused) throw refused; const job = { config, token, ...deferred() }; accepts.push(job); return job.promise; },
    onChange: (unchanged) => changes.push({ snapshot: history.snapshot, unchanged }),
    onAcknowledged: () => acks.push([...history.keys()]), onFatal: (error) => fatals.push(error),
  });
  return { history, calls, accepts, changes, acks, fatals, refuse(error) { refused = error; } };
}
async function finish(f, work, view, { preparedIndex = f.calls.length - 1 } = {}) {
  await turn(); f.calls[preparedIndex < 0 ? f.calls.length - 1 : preparedIndex].resolve(prepared(view));
  await turn(); f.accepts.at(-1).resolve({ history: view }); return work;
}
async function initialize(f, value = draft("Initial")) {
  const view = metadata("s1", 1, { imageKeys: value.background ? [value.background.imageKey] : [], background: value.background });
  const work = f.history.reset(value); await turn(); await finish(f, work, view); return view;
}

test("browser history preserves owned class and registry snapshots through undo and redo", async () => {
  const f = fixture(), first = draft("Service");
  first.scenario.version = 1;
  first.scenario.fleet = [{ id: "pod", class: "compact" }];
  first.scenario.network.stations = [{ id: "hub", vehicleClasses: ["compact", "express"] }];
  first.scenario.expressServices = [{ id: "express", class: "express", partyLimit: 20 }];
  await initialize(f, first);
  first.scenario.fleet[0].class = "group";
  first.scenario.expressServices[0].partyLimit = 1;
  const old = f.history.snapshot;
  const next = f.history.value; next.scenario.expressServices = [];
  const replacement = f.history.replace(next); await turn();
  await finish(f, replacement, metadata("s2", 2, { retained: ["s1", "s2"], canUndo: true }));
  const undo = f.history.undo(); await turn();
  await finish(f, undo, metadata("s1", 3, { retained: ["s1", "s2"], canRedo: true }));
  assert.equal(f.history.snapshot, old);
  assert.equal(f.history.snapshot.scenario.fleet[0].class, "compact");
  assert.equal(f.history.snapshot.scenario.expressServices[0].partyLimit, 20);
  assert.ok(Object.isFrozen(f.history.snapshot.scenario.network.stations[0].vehicleClasses));
  const redo = f.history.redo(); await turn();
  await finish(f, redo, metadata("s2", 4, { retained: ["s1", "s2"], canUndo: true }));
  assert.deepEqual(f.history.snapshot.scenario.expressServices, []);
  assert.equal(f.history.snapshot.scenario.version, 1);
});

test("history publishes queued acceptance and waits before pruning images or starting the next transition", async () => {
  const f = fixture(); await initialize(f, draft("Initial", "a"));
  const first = f.history.replace(draft("Second", "b"), false);
  await turn(); const view = metadata("s2", 2, { imageKeys: ["b"], background: { imageKey: "b", opacity: .45 } });
  f.calls.at(-1).resolve(prepared(view)); await turn();
  assert.equal(f.history.snapshot.scenario.name, "Second"); assert.equal(f.history.pending, true);
  assert.deepEqual([...f.history.keys()].sort(), ["a", "b"]); assert.equal(f.acks.length, 1);
  const next = f.history.replace(draft("Third")); const callCount = f.calls.length; await turn(); assert.equal(f.calls.length, callCount);
  f.accepts.at(-1).resolve({ history: view }); assert.equal(await first, true); await turn();
  assert.deepEqual([...f.history.keys()], ["b"]); assert.equal(f.calls.length, callCount + 1);
  f.history.cancel(); f.calls.at(-1).resolve(prepared(metadata("s3", 3))); await turn();
  assert.equal(f.calls.at(-1).command.action, "discard"); f.calls.at(-1).resolve({ history: view });
  assert.equal(await next, false); assert.equal(f.history.snapshot.scenario.name, "Second");
});

test("undo and redo use Go IDs to select immutable render copies", async () => {
  const f = fixture(); await initialize(f); const first = f.history.snapshot;
  const next = draft("Second"); const replace = f.history.replace(next); next.scenario.name = "Outside mutation";
  await turn(); await finish(f, replace, metadata("s2", 2, { retained: ["s1", "s2"], canUndo: true }));
  const second = f.history.snapshot; assert.equal(second.scenario.name, "Second"); assert.ok(Object.isFrozen(second.scenario.network.nodes));
  const undo = f.history.undo(); await turn(); assert.deepEqual(f.calls.at(-1).command, { action: "prepare", kind: "undo", revision: "2" });
  await finish(f, undo, metadata("s1", 3, { retained: ["s1", "s2"], canRedo: true })); assert.equal(f.history.snapshot, first);
  const redo = f.history.redo(); await turn(); await finish(f, redo, metadata("s2", 4, { retained: ["s1", "s2"], canUndo: true })); assert.equal(f.history.snapshot, second);
  const copy = f.history.value; copy.scenario.name = "Mutated copy"; assert.equal(f.history.snapshot.scenario.name, "Second");
});

test("cancellation discards active proposals and drops dependent queued transitions", async () => {
  const f = fixture(); const initial = await initialize(f);
  const first = f.history.replace(draft("Canceled")); const dropped = f.history.replace(draft("Dependent"));
  await turn(); f.history.cancel(); f.calls.at(-1).resolve(prepared(metadata("s2", 2))); await turn();
  assert.equal(f.accepts.length, 1); assert.equal(f.calls.at(-1).command.action, "discard");
  f.calls.at(-1).resolve({ history: initial }); assert.equal(await first, false); assert.equal(await dropped, false);
  assert.equal(f.history.snapshot.scenario.name, "Initial");
  const latest = f.history.replace(draft("Latest")); await turn(); assert.equal(f.calls.at(-1).command.revision, "1");
  await finish(f, latest, metadata("s3", 2, { retained: ["s1", "s3"], canUndo: true })); assert.equal(f.history.snapshot.scenario.name, "Latest");
});

test("canceling after acceptance is queued leaves the accepted transition irrevocable", async () => {
  const f = fixture(); await initialize(f);
  const work = f.history.replace(draft("Accepted")); await turn(); const view = metadata("s2", 2, { retained: ["s1", "s2"], canUndo: true });
  f.calls.at(-1).resolve(prepared(view)); await turn(); f.history.cancel();
  f.accepts.at(-1).resolve({ history: view }); assert.equal(await work, true);
  assert.equal(f.history.snapshot.scenario.name, "Accepted"); assert.equal(f.history.canUndo, true); assert.equal(f.calls.at(-1).command.action, "prepare");
});

test("acceptance refusal preserves the draft and discards the pending native proposal", async () => {
  const f = fixture(); const initial = await initialize(f); f.refuse(new Error("Queue full"));
  const work = assert.rejects(f.history.replace(draft("Refused")), /Queue full/); await turn(); f.calls.at(-1).resolve(prepared(metadata("s2", 2))); await turn();
  assert.equal(f.history.snapshot.scenario.name, "Initial"); assert.equal(f.calls.at(-1).command.action, "discard");
  f.calls.at(-1).resolve({ history: initial }); await work; assert.equal(f.history.failed, null); assert.equal(f.fatals.length, 0);
});

test("fatal acceptance retains the visible export and both image sets", async (t) => {
  for (const failure of ["error", "mismatch", "transport"]) await t.test(failure, async () => {
    const f = fixture(); await initialize(f, draft("Initial", "a"));
    const work = assert.rejects(f.history.replace(draft("Visible", "b")), Error); await turn();
    const view = metadata("s2", 2, { imageKeys: ["b"], background: { imageKey: "b", opacity: .45 } });
    f.calls.at(-1).resolve(prepared(view)); await turn();
    if (failure === "error") f.accepts.at(-1).resolve({ error: "Rejected acceptance" });
    else if (failure === "mismatch") f.accepts.at(-1).resolve({ history: { ...view, revision: "3" } });
    else f.accepts.at(-1).reject(new Error("Worker timed out"));
    await work; assert.equal(f.history.value.scenario.name, "Visible"); assert.deepEqual([...f.history.keys()].sort(), ["a", "b"]);
    assert.equal(f.fatals.length, 1); assert.equal(f.history.preview(draft("Cannot mutate")), false);
    await assert.rejects(f.history.undo(), Error); await assert.rejects(f.history.flush(), Error);
  });
});

test("local opacity previews make one native gesture and retain the starting Go ID", async () => {
  const f = fixture(); await initialize(f, draft("Initial", "a"));
  const before = f.history.value, count = f.calls.length;
  const value = f.history.value; value.background.opacity = .6; assert.equal(f.history.preview(value), true);
  const next = f.history.value; next.background.opacity = .8; f.history.preview(next); assert.equal(f.calls.length, count);
  const work = f.history.commitFrom(before, next); await turn(); assert.equal(f.calls.at(-1).command.before, "s1");
  await finish(f, work, metadata("s2", 2, { retained: ["s1", "s2"], imageKeys: ["a"], canUndo: true, background: next.background }));
  assert.equal(f.history.snapshot.background.opacity, .8); const undo = f.history.undo(); await turn();
  await finish(f, undo, metadata("s1", 3, { retained: ["s1", "s2"], imageKeys: ["a"], canRedo: true, background: before.background }));
  assert.equal(f.history.background.opacity, .45); assert.equal(before.background.opacity, .45);
});

test("the acquisition guard is checked after preparation and before image registration", async () => {
  const f = fixture(); const initial = await initialize(f); let current = true, registered = false;
  const work = f.history.replace(draft("Import", "image"), true, false, { current: () => current, beforePublish: () => { registered = true; } });
  await turn(); current = false; f.calls.at(-1).resolve(prepared(metadata("s2", 2))); await turn();
  assert.equal(registered, false); assert.equal(f.history.snapshot.scenario.name, "Initial"); f.calls.at(-1).resolve({ history: initial }); assert.equal(await work, false);
});

test("no-op metadata preserves redo and reset still clears identical-content history", async () => {
  const f = fixture(); await initialize(f); const old = f.history.snapshot;
  const unchanged = f.history.replace(f.history.value); await turn(); const changeCount = f.changes.length;
  await finish(f, unchanged, metadata("s1", 2, { changed: false, canRedo: true })); assert.equal(f.history.snapshot, old); assert.equal(f.changes.length, changeCount); assert.equal(f.history.canRedo, true);
  const reset = f.history.reset(f.history.value); await turn(); await finish(f, reset, metadata("s2", 3)); assert.equal(f.history.canRedo, false); assert.equal(f.changes.length, changeCount + 1);
});

test("invalid prepared metadata fails closed without publishing", async () => {
  const f = fixture(); await initialize(f); const work = assert.rejects(f.history.replace(draft("Invalid")), /invalid/);
  await turn(); f.calls.at(-1).resolve({ history: { head: "s2" } }); await work;
  assert.equal(f.history.value.scenario.name, "Initial"); assert.equal(f.accepts.length, 1); assert.equal(f.fatals.length, 1);
});

test("history transport changes the client baseline after acknowledgment without transferring geometry", async () => {
  const sent = [], worker = { postMessage: (data) => sent.push(data), terminate() {} }, client = createClient({ makeWorker: () => worker });
  const first = { name: "First", network: { nodes: [] } }, second = { name: "Second", network: { nodes: [{ id: "two" }] } };
  const reply = (result) => worker.onmessage({ data: { id: sent.at(-1).id, result } });
  const start = client.call(second, "history", { action: "prepare", kind: "reset", revision: "0", background: null });
  assert.deepEqual(sent.at(-1).patch, second); reply(prepared(metadata("s2", 1))); await start;
  const acceptance = client.acceptHistory(first, "p1"); assert.deepEqual(Object.keys(sent.at(-1)).sort(), ["history", "id", "op"]);
  const validation = client.call({ ...first, name: "Renamed" }); assert.equal(sent.length, 2);
  reply({ history: metadata("s1", 2) }); await acceptance;
  assert.deepEqual(sent.at(-1).patch, { name: "Renamed" }); reply({ valid: true }); await validation; client.close();
});

test("worker history keeps typed-invalid drafts but suppresses structurally rejected synchronization", () => {
  for (const synced of [false, true]) {
    const commands = [], view = metadata("s1", 1), handler = createOperations((command) => {
      commands.push(command);
      return command.op === "sync" ? { error: "Invalid", synced } : prepared(view);
    });
    const result = handler.handle({ op: "history", keys: ["name"], patch: { name: false }, history: { action: "prepare", kind: "reset", revision: "0", background: null } });
    assert.equal(commands.length, synced ? 2 : 1); assert.equal(Boolean(result.history), synced);
  }
});

test("worker restores transport branches from Go IDs after undo and sends no full graph", () => {
  const commands = []; let nextView, preparedView;
  const handler = createOperations((command) => {
    commands.push(command);
    if (command.op === "sync") return { valid: true, synced: true };
    if (command.op === "history") return command.history.action === "prepare" ? prepared(preparedView) : { history: nextView };
    if (command.op === "checks") return { checks: { errors: [], warnings: [] } };
    return { valid: true };
  });
  const first = draft("First").scenario, second = { ...first, network: { nodes: [{ id: "two" }] }, name: "Second", map: {} };
  const transition = (kind, view, project) => {
    preparedView = nextView = view;
    handler.handle({ op: "history", keys: project && Object.keys(project), patch: project, history: { action: "prepare", kind, revision: "0", background: null } });
    handler.handle({ op: "history", history: { action: "accept", token: `p${view.revision}` } });
  };
  transition("reset", metadata("s1", 1), first);
  transition("replace", metadata("s2", 2, { retained: ["s1", "s2"], canUndo: true }), second);
  const count = commands.length; transition("undo", metadata("s1", 3, { retained: ["s1", "s2"], canRedo: true }));
  assert.deepEqual(commands.slice(count).map((c) => c.op), ["history", "history"]);
  handler.handle({ op: "validate", keys: Object.keys(first), patch: { name: "Renamed" } });
  const sync = commands.at(-3); assert.deepEqual(sync.patch, { name: "Renamed" }); assert.equal(sync.keys.includes("map"), false);
});

test("queued acceptance supersedes validation of older branches and preserves checks of the accepted project", async () => {
  const sent = [], worker = { postMessage: (data) => sent.push(data), terminate() {} }, client = createClient({ makeWorker: () => worker });
  const first = { name: "First" }, next = { name: "Next" };
  const reply = (result) => worker.onmessage({ data: { id: sent.at(-1).id, result } });
  const active = client.call(first);
  const stale = assert.rejects(client.call(first, "validate", undefined, { background: true }), { name: "AbortError" });
  const accepted = client.acceptHistory(next, "p1"); await stale;
  const current = client.call(next, "validate", undefined, { background: true });
  reply({ valid: true }); await active; assert.equal(sent.at(-1).history.action, "accept");
  reply({ history: metadata("s2", 2) }); await accepted; assert.deepEqual(sent.at(-1).patch, {});
  reply({ valid: true }); await current; assert.equal(sent.length, 3); client.close();
});

test("a newer local preview discards preparation without overwriting the visible preview", async () => {
  const f = fixture(); const initial = await initialize(f, draft("Initial", "a"));
  const work = assert.rejects(f.history.replace(draft("Settings")), /draft changed/); await turn();
  const preview = f.history.value; preview.background.opacity = .8; f.history.preview(preview);
  f.calls.at(-1).resolve(prepared(metadata("s2", 2))); await turn();
  assert.equal(f.calls.at(-1).command.action, "discard"); f.calls.at(-1).resolve({ history: initial });
  await work; assert.equal(f.history.value.scenario.name, "Initial"); assert.equal(f.history.background.opacity, .8);
});

test("an external gesture generation invalidates preparation without a render-copy change", async () => {
  const pending = deferred(), discarded = [], accepted = []; let edits = 0;
  const history = createHistory({ initial: draft("Initial"), own, clone,
    captureGuard: () => { const generation = edits; return () => generation === edits; },
    call: (_config, _op, command) => command.action === "prepare" ? pending.promise : (discarded.push(command), Promise.resolve({ history: {} })),
    accept: (...args) => { accepted.push(args); return Promise.resolve({ history: {} }); },
  });
  const work = assert.rejects(history.reset(draft("Loaded")), /draft changed/); await turn(); edits++;
  pending.resolve(prepared(metadata("s1", 1))); await work;
  assert.equal(accepted.length, 0); assert.equal(discarded.length, 1); assert.equal(history.value.scenario.name, "Initial");
});

test("image admission re-prepares one native replacement and publishes only the trimmed proposal", async () => {
  const f = fixture(); await initialize(f, draft("Initial", "a")); let published;
  const candidate = draft("Incoming", "b");
  const work = f.history.replace(candidate, true, false, {
    planTrim(view, lookup) { assert.equal(lookup("s1").background.imageKey, "a"); assert.equal(lookup(view.head).background.imageKey, "b"); return 1; },
    beforePublish(changed, view) { published = { changed, head: view.head }; },
  });
  await turn(); f.calls.at(-1).resolve(prepared(metadata("s2", 2, { retained: ["s1", "s2"], imageKeys: ["a", "b"], canUndo: true, background: candidate.background }), "p2"));
  await turn(); assert.equal(f.accepts.length, 1); assert.equal(f.history.snapshot.scenario.name, "Initial");
  assert.equal(f.calls.at(-1).command.trimOldest, 1); assert.equal(f.calls.at(-1).command.revision, "1");
  const view = metadata("s3", 2, { imageKeys: ["b"], background: candidate.background });
  f.calls.at(-1).resolve(prepared(view, "p3")); await turn(); assert.equal(f.accepts.at(-1).token, "p3");
  assert.deepEqual(published, { changed: true, head: "s3" });
  f.accepts.at(-1).resolve({ history: view }); assert.equal(await work, true); assert.equal(f.history.canUndo, false);
});

test("canceling the trimmed proposal keeps the old timeline and never registers the image", async () => {
  const f = fixture(); const initial = await initialize(f); let published = false;
  const work = f.history.replace(draft("Incoming", "b"), true, false, { planTrim: () => 1, beforePublish: () => { published = true; } });
  await turn(); f.calls.at(-1).resolve(prepared(metadata("s2", 2, { retained: ["s1", "s2"], canUndo: true })));
  await turn(); f.history.cancel(); f.calls.at(-1).resolve(prepared(metadata("s3", 2), "p3")); await turn();
  assert.deepEqual(f.calls.at(-1).command, { action: "discard", token: "p3" });
  f.calls.at(-1).resolve({ history: initial }); assert.equal(await work, false); assert.equal(published, false); assert.equal(f.accepts.length, 1);
});

test("an image admission error discards the untrimmed proposal without failing history", async () => {
  const f = fixture(); const initial = await initialize(f);
  const work = assert.rejects(f.history.replace(draft("Incoming"), true, false, { planTrim() { throw new Error("Protected images exceed the limit"); } }), /Protected images/);
  await turn(); f.calls.at(-1).resolve(prepared(metadata("s2", 2))); await turn();
  assert.equal(f.calls.at(-1).command.action, "discard"); f.calls.at(-1).resolve({ history: initial }); await work;
  assert.equal(f.history.failed, null); assert.equal(f.history.snapshot.scenario.name, "Initial");
});

test("a failed preparation transport stops history and preserves the visible draft", async () => {
  const f = fixture(); await initialize(f);
  const work = assert.rejects(f.history.replace(draft("Unpublished")), /Worker stopped/); await turn();
  f.calls.at(-1).reject(new Error("Worker stopped")); await work;
  assert.equal(f.history.snapshot.scenario.name, "Initial"); assert.equal(f.fatals.length, 1);
  await assert.rejects(f.history.flush(), /Worker stopped/);
});

test("a newer interaction after publication survives a delayed acknowledgment", async () => {
  const f = fixture(); await initialize(f, draft("Initial", "a")); let renders = 0;
  const work = f.history.replace(draft("Published", "a"), true, false, { beforePublish: () => { renders++; } });
  await turn(); const view = metadata("s2", 2, { retained: ["s1", "s2"], canUndo: true, imageKeys: ["a"], background: { imageKey: "a", opacity: .45 } });
  f.calls.at(-1).resolve(prepared(view)); await turn();
  const newer = f.history.value; newer.background.opacity = .8; f.history.preview(newer);
  f.accepts.at(-1).resolve({ history: view }); await work;
  assert.equal(renders, 1); assert.equal(f.history.snapshot.background.opacity, .8); assert.equal(f.history.canUndo, true);
});
