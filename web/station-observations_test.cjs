"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const O = require("./station-observations.js");
const editor = require("./editor.js");
const packed = (value) => Buffer.from(value, "utf8").toString("base64");
function wireRequest(fields) {
  const value = { sharingConsent: "shared", service: "on-demand", id: 1, from: "s", to: "t", partySize: 1,
    podID: "", completed: false, requestedTick: 0, dispatchReason: "", ...fields };
  for (const key of ["from", "to", "podID", "dispatchReason", "serviceID", "legFrom"]) if (Object.hasOwn(value, key)) value[key] = packed(value[key]);
  return value;
}

function config() {
  return { network: {
    nodes: [{ id: "a", position: { x: 0, y: 0 } }, { id: "b", position: { x: 30, y: 0 } },
      { id: "c", position: { x: 30, y: 40 } }, { id: "d", position: { x: 60, y: 40 } }],
    lanes: [{ id: "straight", from: "a", to: "b", stationID: "s" },
      { id: "curve", from: "b", to: "c", stationID: "s", control: { x: 60, y: 0 } },
      { id: "road", from: "a", to: "d" }],
    stations: [{ id: "s", name: "Station S", berths: [{ id: "b1", node: "b" }, { id: "b2", node: "c" }, { id: "b3", node: "d" }] },
      { id: "t", name: "Station T", berths: [{ id: "bt", node: "a" }] }],
  } };
}

function envelope() {
  const identity = { epoch: "epoch-1", serverStart: "server-1", projectRevision: 7 };
  const pod = (id, fields) => ({ pod: { id, stationID: "s", berthID: "", laneID: "", activity: "Ready", stationPhase: "At berth", waitReason: "", blockedBy: "", ...fields } });
  return { topology: { ...identity, network: config().network }, frame: { state: { ...identity, revision: 120, generation: 2,
    simulation: { paused: false, tick: 100,
      berths: [{ id: "b1", occupant: "p1", reservedBy: "p1" }, { id: "b2", occupant: "", reservedBy: "p2" },
        { id: "b3", occupant: "", reservedBy: "" }, { id: "bt", occupant: "p4", reservedBy: "" }],
      vehicles: [pod("p1", { berthID: "b1" }), pod("p2", { stationID: "", maneuverStationID: "s", laneID: "straight", activity: "Boarding", waitReason: "Junction traffic", blockedBy: "p3" }),
        pod("p3", { laneID: "curve", activity: "Traveling", stationPhase: "Departing berth", waitReason: "Pod ahead", blockedBy: "p1" }), pod("p4", { stationID: "t", berthID: "bt" })],
      pending: [wireRequest({ id: 1, from: "s", dispatchReason: "No available pod" }), wireRequest({ id: 2, from: "t", legFrom: "s", dispatchReason: "" }), wireRequest({ id: 3, from: "t", dispatchReason: "Assigned" })],
    } } } };
}

function frozen(value) {
  if (value && typeof value === "object") { for (const child of Object.values(value)) frozen(child); Object.freeze(value); }
  return value;
}

test("packed HTTP text requires canonical base64, fatal UTF-8, valid IDs, and bounded bytes", () => {
  assert.equal(O.unpackOrderText(packed("station-1.+")), "station-1.+");
  assert.equal(O.unpackOrderText(""), "");
  for (const bad of [null, undefined, 0, "cw", "cw=", "c w==", "cw==\n", "cx==", "c3N=", "____", "====", "c===", "cw==="]) assert.throws(() => O.unpackOrderText(bad));
  for (const bytes of [[255], [0xc0, 0xaf], [0xed, 0xa0, 0x80], [0xe2, 0x82]]) assert.throws(() => O.unpackOrderText(Buffer.from(bytes).toString("base64"), true), /UTF-8/);
  for (const invalidID of ["a/b", "a b", "À", "\ufeffstation", "a\u0000", "a\u0080"]) assert.throws(() => O.unpackOrderText(packed(invalidID)), /ID characters/);
  for (const control of ["\u0000", "\n", "\u001f", "\u007f", "\u0080", "\u009f"]) assert.throws(() => O.unpackOrderText(packed("Reason" + control), true), /control character/);
  assert.equal(O.unpackOrderText(packed("a".repeat(64))).length, 64);
  assert.throws(() => O.unpackOrderText(packed("a".repeat(65))));
  assert.equal(O.unpackOrderText(packed("é".repeat(512)), true), "é".repeat(512));
  assert.throws(() => O.unpackOrderText(packed("é".repeat(513)), true));
  const reason = "\ufeffÀ quai 東京  ";
  assert.equal(O.unpackOrderText(packed(reason), true), reason);
  assert.equal(O.unpackOrderText(packed(reason), true).codePointAt(0), 0xfeff, "BOM must remain text, not be stripped by TextDecoder");
});

test("request unpacking defaults only omitempty/omitzero fields, rejects unknown/missing data, and never mutates wire objects", () => {
  const wire = frozen(wireRequest({})), before = JSON.stringify(wire), decoded = O.unpackPendingRequest(wire);
  assert.equal(decoded.from, "s"); assert.equal(decoded.to, "t"); assert.equal(decoded.podID, ""); assert.equal(decoded.dispatchReason, "");
  assert.equal(decoded.serviceID, ""); assert.equal(decoded.legFrom, ""); assert.equal(decoded.boardedTick, 0);
  assert.notEqual(decoded, wire); assert.equal(JSON.stringify(wire), before);
  for (const key of ["sharingConsent", "service", "id", "from", "to", "partySize", "podID", "completed", "requestedTick", "dispatchReason"]) {
    const missing = wireRequest({}); delete missing[key]; assert.throws(() => O.unpackPendingRequest(missing), undefined, key);
  }
  for (const key of ["serviceID", "legFrom", "boardedTick"]) for (const value of [null, undefined, {}, -1]) {
    assert.throws(() => O.unpackPendingRequest({ ...wireRequest({}), [key]: value }), undefined, key);
  }
  for (const key of ["From", "dispatchreason", "unexpected"]) assert.throws(() => O.unpackPendingRequest({ ...wireRequest({}), [key]: "" }), /Unknown/);
  assert.throws(() => O.unpackPendingRequest(wireRequest({ from: "" })), /origin/);
  assert.throws(() => O.unpackPendingRequest(wireRequest({ completed: true })));
  const allText = O.unpackPendingRequest(wireRequest({ podID: "p1", service: "express", serviceID: "service-1", legFrom: "s", dispatchReason: "\ufeffÀ quai 東京" }));
  assert.equal(allText.podID, "p1"); assert.equal(allText.serviceID, "service-1"); assert.equal(allText.legFrom, "s");
  assert.equal(allText.dispatchReason, "\ufeffÀ quai 東京");
});

test("actual Go EncodeStateJSON golden agrees on queued and empty HTTP pending observations", async () => {
  const filename = path.join(__dirname, "testdata", "station-observations.json");
  const fixture = JSON.parse(fs.readFileSync(filename, "utf8"));
  assert.equal(fixture.status, "GENERATED BY EncodeStateJSON", "Fixture has NOT been generated or verified. Run TestStationObservationsHTTPFixture with -update-station-observations first.");
  assert.deepEqual(fixture.cases.map((entry) => entry.name), ["queued", "empty-queue"]);
  for (const entry of fixture.cases) {
    const wire = frozen(entry.envelope), before = JSON.stringify(wire), report = O.observe(wire);
    assert.equal(report.status, "snapshot");
    for (const expected of entry.expected) {
      const station = report.stations.find((station) => station.id === expected.id);
      assert.ok(station); assert.equal(station.pendingOriginRequests, expected.pendingOriginRequests);
      assert.deepEqual(station.dispatchReasons, expected.dispatchReasons);
    }
    assert.equal(JSON.stringify(wire), before);
    const integrated = await editor.readStationObservations({ fetch: async () => response(wire) });
    assert.deepEqual(integrated, report);
    const requests = wire.frame.state.simulation.pending ?? [];
    for (const request of requests) for (const key of ["from", "to", "podID", "dispatchReason", "serviceID", "legFrom"]) {
      if (Object.hasOwn(request, key)) {
        assert.equal(packed(O.unpackOrderText(request[key], key === "dispatchReason")), request[key]);
      }
    }
  }
  const queued = fixture.cases[0].envelope.frame.state.simulation.pending;
  assert.equal(Object.hasOwn(queued[0], "serviceID"), false);
  assert.equal(Object.hasOwn(queued[0], "legFrom"), false);
  assert.equal(Object.hasOwn(queued[0], "boardedTick"), false);
  assert.equal(queued[0].dispatchReason, "");
  assert.equal(O.unpackPendingRequest(queued[1]).legFrom, "garden");
  assert.equal(O.unpackPendingRequest(queued[1]).dispatchReason, "\ufeffÀ quai 東京");
});

test("malformed packed pending data stays unavailable and is never mapped using encoded IDs", () => {
  for (const mutate of [
    (request) => { request.from = "s"; }, (request) => { request.to = "cx=="; },
    (request) => { request.podID = packed("invalid/pod"); }, (request) => { request.dispatchReason = packed("reason\n"); },
    (request) => { request.serviceID = packed("invalid service"); }, (request) => { request.legFrom = "cx=="; },
    (request) => { request.extra = ""; }, (request) => { delete request.dispatchReason; },
  ]) {
    const reply = envelope(); mutate(reply.frame.state.simulation.pending[0]);
    const station = O.observe(reply).stations[0];
    assert.equal(station.pendingOriginRequests, null); assert.equal(station.dispatchReasons, null);
  }
});

test("measurements default off, remain ephemeral, and reuse the existing lane length approximation", () => {
  const draft = frozen(config()), before = JSON.stringify(draft);
  assert.deepEqual(O.measurements(draft, { type: "station", id: "s" }, editor.curveLength).items, []);
  const report = O.measurements(draft, { type: "station", id: "s" }, editor.curveLength, true);
  assert.equal(report.items.length, 4);
  assert.deepEqual(report.items.map((item) => item.kind), ["lane", "lane", "spacing", "spacing"]);
  assert.equal(report.items[0].meters, 30);
  assert.equal(report.items[1].meters, editor.laneLength(draft, draft.network.lanes[1]));
  assert.ok(report.items[1].meters > 40);
  assert.match(report.items[1].method, /approximation, 16 segments/);
  assert.deepEqual(report.items.slice(2).map((item) => item.meters), [40, 30]);
  assert.match(report.items[2].method, /straight center chord, authored berth order/);
  const lane = O.measurements(draft, { type: "lane", id: "road" }, editor.curveLength, true);
  assert.equal(lane.items.length, 1);
  assert.equal(lane.items[0].meters, Math.hypot(60, 40));
  assert.equal(JSON.stringify(draft), before);
});

test("invalid, missing, duplicate, nonfinite, and out-of-bounds geometry is unavailable, not a zero length", () => {
  for (const value of [NaN, Infinity, -Infinity, "30", O.MAX_COORDINATE + 1]) {
    const draft = config(); draft.network.nodes[0].position.x = value;
    const report = O.measurements(draft, { type: "lane", id: "straight" }, editor.curveLength, true);
    assert.equal(report.items.length, 0); assert.match(report.unavailable.join(" "), /invalid|missing/);
  }
  for (const control of [{}, { x: 0, y: Infinity }, { x: 100001, y: 0 }]) {
    const draft = config(); draft.network.lanes[1].control = control;
    assert.equal(O.measurements(draft, { type: "lane", id: "curve" }, editor.curveLength, true).items.length, 0);
  }
  const duplicate = config(); duplicate.network.nodes.push(duplicate.network.nodes[0]);
  assert.match(O.measurements(duplicate, { type: "station", id: "s" }, editor.curveLength, true).unavailable[0], /unavailable/);
  const missing = config(); missing.network.stations[0].berths[1].node = "missing";
  const report = O.measurements(missing, { type: "station", id: "s" }, editor.curveLength, true);
  assert.equal(report.items.filter((item) => item.kind === "spacing").length, 0);
  assert.equal(report.unavailable.length, 2);
  assert.equal(O.measurements(config(), { type: "node", id: "a" }, editor.curveLength, true).items.length, 0);
  assert.equal(O.measurements(config(), { type: "lane", id: "straight" }, () => Infinity, true).items.length, 0);
  assert.equal(O.measurements(null, null, editor.curveLength, true).items.length, 0);
});

test("live observations use exclusive berth resource counts and preserve actual wait reasons", () => {
  const reply = frozen(envelope()), before = JSON.stringify(reply), report = O.observe(reply), station = report.stations[0];
  assert.equal(report.status, "snapshot");
  assert.deepEqual(station.occupancy, { occupied: 1, reservedEmpty: 1, free: 1, capacity: 3 });
  assert.equal(station.associatedPods, 3);
  assert.equal(station.idlePods, 1); assert.equal(station.boardingPods, 1); assert.equal(station.departingPods, 1); assert.equal(station.blockedPods, 2);
  assert.deepEqual(station.waitReasons, [{ reason: "Junction traffic", count: 1 }, { reason: "Pod ahead", count: 1 }]);
  assert.equal(station.pendingOriginRequests, 2);
  assert.deepEqual(station.dispatchReasons, [{ reason: "", count: 1 }, { reason: "No available pod", count: 1 }]);
  assert.equal(station.pods[1].blockedBy, "p3");
  assert.match(report.scope, /May now be stale/); assert.match(report.scope, /NOT MEASURED/);
  assert.match(report.association, /does not prove physical presence/);
  assert.equal(JSON.stringify(reply), before);
  assert.ok(!JSON.stringify(report).includes('"position"'));
  assert.ok(!JSON.stringify(report).includes('"network"'));
});

test("topology identities, bounded counters, and unique resource definitions gate observations", () => {
  for (const mutate of [
    (r) => { r.topology.epoch = "other"; }, (r) => { r.topology.serverStart = "other"; },
    (r) => { r.topology.projectRevision++; }, (r) => { delete r.topology.serverStart; },
    (r) => { r.frame.state.projectRevision = Infinity; }, (r) => { r.frame.state.revision = -1; },
    (r) => { r.frame.state.simulation.tick = Number.MAX_SAFE_INTEGER + 1; },
    (r) => { r.topology.epoch = r.frame.state.epoch = "invalid identity"; },
    (r) => { r.topology.network.stations[1].berths.push({ id: "b1" }); },
    (r) => { r.topology.network.lanes[0].stationID = "missing"; },
  ]) {
    const reply = envelope(); mutate(reply);
    const report = O.observe(reply); assert.equal(report.status, "unavailable"); assert.deepEqual(report.stations, []);
  }
  const base = { epoch: "epoch-1", serverStart: "server-1", projectRevision: 7 }, report = O.observe(envelope());
  assert.match(O.draftScope(report, base, true), /matches the draft base/);
  assert.match(O.draftScope(report, base, false), /Unsaved draft differs/);
  assert.match(O.draftScope(report, { ...base, projectRevision: 8 }, true), /identity differs/);
  assert.equal(O.sameIdentity(base, { ...base, serverStart: "server-2" }), false);
  assert.equal(O.identity({ ...base, projectRevision: "7" }), null);
});

test("missing or malformed source fields stay unavailable rather than zero-filled", () => {
  for (const source of ["berths", "vehicles", "pending"]) {
    for (const value of [undefined, null, {}, new Array(1), new Array(O.MAX_ITEMS + 1)]) {
      if (source === "pending" && value === null) continue;
      const reply = envelope(); reply.frame.state.simulation[source] = value;
      const station = O.observe(reply).stations[0];
      if (source === "berths") assert.equal(station.occupancy, null);
      if (source === "vehicles") { assert.equal(station.associatedPods, null); assert.equal(station.idlePods, null); assert.equal(station.waitReasons, null); }
      if (source === "pending") assert.equal(station.pendingOriginRequests, null);
    }
  }
  const reply = envelope(), sim = reply.frame.state.simulation;
  delete sim.berths[0].occupant; delete sim.vehicles[0].pod.activity; delete sim.vehicles[1].pod.waitReason; delete sim.vehicles[2].pod.stationPhase;
  delete sim.pending[0].dispatchReason;
  const station = O.observe(reply).stations[0];
  assert.equal(station.occupancy, null); assert.equal(station.idlePods, null); assert.equal(station.boardingPods, null);
  assert.equal(station.blockedPods, null); assert.equal(station.departingPods, null); assert.equal(station.waitReasons, null);
  assert.equal(station.pendingOriginRequests, null); assert.equal(station.dispatchReasons, null);
  assert.match(O.stationLines(station).join(" "), /Unavailable/);
  const duplicate = envelope(); duplicate.frame.state.simulation.vehicles.push(duplicate.frame.state.simulation.vehicles[0]);
  assert.equal(O.observe(duplicate).stations[0].associatedPods, null);
  const unknown = envelope(); unknown.frame.state.simulation.vehicles[0].pod.berthID = "unknown";
  assert.equal(O.observe(unknown).stations[0].associatedPods, null);
  const request = envelope(); request.frame.state.simulation.pending.push(request.frame.state.simulation.pending[0]);
  assert.equal(O.observe(request).stations[0].pendingOriginRequests, null);
});

test("valid empty sources yield observed zeros, without claiming unmeasured performance", () => {
  const reply = envelope(), sim = reply.frame.state.simulation;
  sim.vehicles = []; sim.pending = [];
  for (const berth of sim.berths) { berth.occupant = ""; berth.reservedBy = ""; }
  const station = O.observe(reply).stations[0];
  assert.deepEqual(station.occupancy, { occupied: 0, reservedEmpty: 0, free: 3, capacity: 3 });
  assert.equal(station.blockedPods, 0); assert.equal(station.pendingOriginRequests, 0);
  assert.deepEqual(station.waitReasons, []);
  assert.match(O.stationLines(station).join(" "), /none reported/);
});

test("explicit null and empty-array HTTP pending queues both mean observed zero", () => {
  for (const pending of [null, []]) {
    const reply = envelope(); reply.frame.state.simulation.pending = pending;
    const report = O.observe(reply);
    assert.equal(report.status, "snapshot");
    for (const station of report.stations) {
      assert.equal(station.pendingOriginRequests, 0);
      assert.deepEqual(station.dispatchReasons, []);
    }
  }
  const missing = envelope(); delete missing.frame.state.simulation.pending;
  assert.equal(O.observe(missing).stations[0].pendingOriginRequests, null);
});

function deferred() {
  let resolve, reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

test("manual reader never auto-reads and permits only one in-flight request, even after cancellation", async () => {
  const held = deferred(), fresh = deferred(), log = [], signals = [];
  const reader = O.createManualReader({ read: (signal) => { signals.push(signal); return signals.length === 1 ? held.promise : fresh.promise; }, onChange: (view) => log.push(view) });
  assert.equal(signals.length, 0); assert.equal(reader.view.status, "not-read");
  const first = reader.refresh();
  assert.equal(signals.length, 1); assert.equal(reader.view.busy, true);
  assert.equal(await reader.refresh(), false);
  reader.cancel();
  assert.equal(signals[0].aborted, true); assert.equal(reader.view.result, null);
  assert.equal(reader.view.busy, true); assert.equal(await reader.refresh(), false);
  held.resolve({ stale: true }); await first;
  assert.equal(reader.view.busy, false); assert.equal(reader.view.status, "cancelled"); assert.equal(reader.view.result, null);
  assert.ok(!log.some((view) => view.result?.stale));
  const next = reader.refresh(); fresh.resolve({ fresh: true }); await next;
  assert.equal(signals.length, 2); assert.equal(reader.view.result.fresh, true); assert.equal(reader.view.busy, false);
});

test("manual reader clears old observations on refresh, failures, and stale rejected responses", async () => {
  const values = [deferred(), deferred(), deferred()]; let calls = 0;
  const reader = O.createManualReader({ read: () => values[calls++].promise });
  let pending = reader.refresh(); values[0].resolve({ tick: 1 }); await pending;
  assert.equal(reader.view.result.tick, 1);
  pending = reader.refresh(); assert.equal(reader.view.result, null);
  values[1].reject(new Error("network failed")); await pending;
  assert.equal(reader.view.status, "unavailable"); assert.equal(reader.view.error, "network failed");
  pending = reader.refresh(); reader.cancel(); values[2].reject(new Error("late rejection")); await pending;
  assert.equal(reader.view.status, "cancelled"); assert.ok(!reader.view.error.includes("late"));
});

function response(body, media = editor.LIVE_STATE_ACCEPT) {
  return { ok: true, headers: { get: () => media }, json: async () => body };
}

test("live integration sends one GET with existing media validation and cancellation signal, without changing editor identity", async () => {
  const controller = new AbortController(), calls = [], connection = { epoch: "old", onServerStart: () => assert.fail("Observation must not change the editor base") };
  connection.fetch = async (url, init) => { calls.push({ url, init }); return response(envelope()); };
  const report = await editor.readStationObservations(connection, controller.signal);
  assert.equal(report.status, "snapshot"); assert.equal(calls.length, 1);
  assert.equal(calls[0].url, "/api/state"); assert.equal(calls[0].init.headers.Accept, editor.LIVE_STATE_ACCEPT);
  assert.equal(calls[0].init.signal, controller.signal); assert.equal(connection.epoch, "old");
  let decoded = false;
  await assert.rejects(editor.readStationObservations({ fetch: async () => ({ ...response(null, "application/json"), json: async () => { decoded = true; return null; } }) }), /unsupported media type/);
  assert.equal(decoded, false);
  const wrong = envelope(); wrong.topology.projectRevision++;
  await assert.rejects(editor.readStationObservations({ fetch: async () => response(wrong) }), /identity/);
  const removed = envelope(); removed.topology.textEncoding = "bad";
  await assert.rejects(editor.readStationObservations({ fetch: async () => response(removed) }), /not valid/);
});

class Element {
  constructor(name, attributes = {}) { this.name = name; this.attributes = attributes; this.children = []; this.textContent = ""; }
  append(child) { this.children.push(child); }
  replaceChildren() { this.children = []; }
  set innerHTML(_) { assert.fail("Measurements must use safe text nodes"); }
}

test("actual ruler renderer draws curves, endpoints, and meter labels with a safe accessible summary", () => {
  const draft = config(); draft.network.lanes[0].id = "<script>";
  const report = O.measurements(draft, { type: "station", id: "s" }, editor.curveLength, true);
  const layer = new Element("g"), summary = new Element("p"), create = (name, attributes) => new Element(name, attributes);
  editor.drawMeasurementReport(report, { layer, summary, create, scale: 2 });
  assert.equal(layer.children.filter((e) => e.name === "path").length, 4);
  assert.equal(layer.children.filter((e) => e.name === "circle").length, 8);
  assert.ok(layer.children.some((e) => e.name === "path" && e.attributes.d.includes(" Q ")));
  assert.match(summary.textContent, /30.00 m \(straight centerline\)/);
  assert.match(summary.textContent, /40.00 m \(straight center chord/);
  assert.match(summary.textContent, /<script>/);
  for (const scale of [NaN, Infinity, -1, 0]) {
    editor.drawMeasurementReport(report, { layer, summary, create, scale });
    for (const element of layer.children) assert.ok(!JSON.stringify(element.attributes).match(/NaN|Infinity/));
  }
  editor.drawMeasurementReport(report, { layer, summary, create, scale: 0.001 });
  assert.equal(layer.children.find((element) => element.name === "text").attributes["font-size"], 11000);
  editor.drawMeasurementReport(O.measurements(draft, null, editor.curveLength), { layer, summary, create, scale: 1 });
  assert.equal(layer.children.length, 0); assert.match(summary.textContent, /off/);
});

test("editor observation hooks are manual, default-off, shipped, and separate from live project mutation", () => {
  const html = fs.readFileSync(path.join(__dirname, "editor.html"), "utf8");
  const source = fs.readFileSync(path.join(__dirname, "editor.js"), "utf8");
  assert.match(html, /id="measurementToggle" type="checkbox"/);
  assert.doesNotMatch(html, /id="measurementToggle"[^>]*checked/);
  assert.match(html, /id="measurementLayer" aria-hidden="true"/);
  assert.match(html, /id="measurementSummary"/);
  assert.ok(html.indexOf('src="station-observations.js"') < html.indexOf('src="editor.js"'));
  for (const name of ["stationObservationRefresh", "stationObservationCancel", "stationObservationStation", "stationObservationStatus", "stationObservationDetails"]) assert.ok(html.includes(`id="${name}"`));
  assert.match(source, /measurements: false/);
  assert.match(source, /pagehide.*stationObservationReader.cancel/);
  assert.equal((source.match(/stationObservationReader.refresh\(/g) || []).length, 1);
  const section = source.slice(source.indexOf('$("#measurementToggle").addEventListener'), source.indexOf("let tilePageActive"));
  assert.doesNotMatch(section, /innerHTML|postCommand|setInterval|setTimeout|keeper\.schedule|history\./);
  const build = fs.readFileSync(path.join(__dirname, "../internal/cmd/buildweb/main.go"), "utf8");
  assert.ok(build.includes('"station-observations.js"'));
});

test("actual observation renderer treats server wait reasons and blocker IDs only as text", () => {
  const reply = envelope(), pod = reply.frame.state.simulation.vehicles[0].pod;
  pod.waitReason = '<img src=x onerror="alert(1)">'; pod.blockedBy = "<script>";
  const station = O.observe(reply).stations[0], container = new Element("div");
  editor.drawObservationLines(O.stationLines(station), { container, create: (name) => new Element(name) });
  assert.ok(container.children.every((element) => element.name === "p"));
  assert.match(container.children.map((element) => element.textContent).join(" "), /<img src=x/);
  assert.match(container.children.map((element) => element.textContent).join(" "), /blockedBy <script>/);
  editor.drawObservationLines(O.stationLines(null), { container, create: (name) => new Element(name) });
  assert.equal(container.children.length, 1); assert.match(container.children[0].textContent, /unavailable/);
});
