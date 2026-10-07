"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const { execFileSync } = require("node:child_process");
const fs = require("node:fs");
const path = require("node:path");
const zlib = require("node:zlib");
const editor = require("./editor.js");

// pngBytes gives a grayscale PNG file of width by height pixels. With
// header set, it gives only the signature and the IHDR chunk, which is
// enough for the header checks of an image that is too large to make.
function pngBytes(width, height, header = false) {
  const chunk = (type, data) => {
    const length = Buffer.alloc(4); length.writeUInt32BE(data.length);
    const body = Buffer.concat([Buffer.from(type, "latin1"), data]);
    const crc = Buffer.alloc(4); crc.writeUInt32BE(zlib.crc32(body));
    return Buffer.concat([length, body, crc]);
  };
  const ihdr = Buffer.alloc(13); ihdr.writeUInt32BE(width, 0); ihdr.writeUInt32BE(height, 4); ihdr[8] = 8;
  const start = [Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]), chunk("IHDR", ihdr)];
  if (header) return Buffer.concat(start);
  const rows = Buffer.alloc((width + 1) * height);
  return Buffer.concat([...start, chunk("IDAT", zlib.deflateSync(rows)), chunk("IEND", Buffer.alloc(0))]);
}

// jpegBytes gives the start of a JPEG file: the start marker, an APP0
// segment, and a baseline frame header of width by height pixels.
function jpegBytes(width, height) {
  const app0 = Buffer.from([0xff, 0xe0, 0x00, 0x10, 0x4a, 0x46, 0x49, 0x46, 0x00, 0x01, 0x01, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00]);
  const sof = Buffer.from([0xff, 0xc0, 0x00, 0x0b, 0x08, height >> 8, height & 0xff, width >> 8, width & 0xff, 0x01, 0x01, 0x11, 0x00]);
  return Buffer.concat([Buffer.from([0xff, 0xd8]), app0, sof, Buffer.from([0xff, 0xd9])]);
}

// dataURL gives the data URL of bytes with the media type type.
function dataURL(bytes, type = "png") {
  return `data:image/${type};base64,${Buffer.from(bytes).toString("base64")}`;
}

// DRAFTS holds the drafts of the page tests: two stations Alpha and Beta
// with lanes in both directions and one pod, and drafts made from it with
// the editor actions. The Go editor model makes these edits in the page,
// and the Go tests check them, so the page tests use fixed drafts.
// fixtureDraft gives a copy of one draft.
const DRAFTS = JSON.parse(fs.readFileSync(path.join(__dirname, "testdata", "editor", "drafts.json"), "utf8"));
const fixtureDraft = (name) => structuredClone(DRAFTS[name]);

function connectedScenario() {
  return fixtureDraft("connected");
}

function serviceScenario() {
  const config = connectedScenario();
  const classes = ["legacy", "compact", "group", "express"];
  for (const lane of config.network.lanes) lane.vehicleClasses = [...classes];
  for (const station of config.network.stations) {
    station.vehicleClasses = [...classes];
    for (const berth of station.berths) berth.vehicleClasses = [...classes];
  }
  config.fleet[0].class = "compact";
  config.expressServices = [{ id: "express", from: config.network.stations[0].id, to: config.network.stations[1].id, class: "express", partyLimit: 20 }];
  return config;
}


test("Express project round trips retain the explicit contract and whole service metadata", () => {
  const config = serviceScenario();
  config.orderContract = "express-v1"; config.fleet[0].class = "express";
  const before = structuredClone(config);
  const document = JSON.parse(editor.serializeDocument(config));
  assert.equal(document.format, "podsim"); assert.equal(document.version, 1);
  assert.deepEqual(document.scenario, before);
  assert.deepEqual(editor.parseDocument(JSON.stringify(config)).scenario, before);
  assert.deepEqual(editor.parseDocument(JSON.stringify(document)).scenario, before);
  assert.deepEqual(config, before);
  assert.match(editor.fleetClassNotice(config), /qualified express-v1 runtime/);
});

test("a service project imports bare or wrapped and preserves authored class and registry metadata", () => {
  const config = serviceScenario(), before = structuredClone(config);
  assert.deepEqual(editor.parseDocument(JSON.stringify(config)).scenario, config);
  const exported = JSON.parse(editor.serializeDocument(config, null));
  assert.equal(exported.version, 1);
  assert.equal(exported.scenario.version, 1);
  assert.deepEqual(editor.parseDocument(JSON.stringify(exported)).scenario, config);
  assert.deepEqual(config, before);
  assert.match(editor.fleetClassNotice(config), /New pods use legacy class/);
  assert.match(editor.fleetClassNotice(config), /Express pods cannot start/);
  // The lane Selection card sets guideway classes, so only pod classes need an import.
  assert.match(editor.fleetClassNotice(config), /Import a project to set pod classes and express services\./);
  assert.equal(editor.fleetClassNotice({ version: 1 }), "");
  assert.equal(editor.fleetClassNotice(connectedScenario()), "");
});

// chainScenario gives Alpha a berth chain like the generated stations use:
// entry, arrival node, berth, departure node, and exit.
function chainScenario() {
  return fixtureDraft("chain");
}

const repoRoot = path.join(__dirname, "..");
const generatedFiles = new Map();

// goOnPath reports if a go command is on PATH. The generated fixtures need
// it. GOTOOLCHAIN=local stops a toolchain switch, so a Go that is too old
// for go.mod counts as present and its fixture tests show the real error.
function goOnPath() {
  try {
    execFileSync("go", ["version"], { env: { ...process.env, GOTOOLCHAIN: "local" }, stdio: "ignore" });
    return true;
  } catch {
    return false;
  }
}

// fixtureOptions gives the node:test options for a test that needs a
// generated fixture. Without Go, the test skips with a reason. When
// PODSIM_REQUIRE_GO is 1, a missing Go is a failure, so the test runs.
function fixtureOptions(hasGo, env) {
  return { skip: hasGo || env.PODSIM_REQUIRE_GO === "1" ? false : "needs Go on PATH, run mise run test:web" };
}

const needsGo = fixtureOptions(goOnPath(), process.env);

// generatedFile runs the scenario command, so the fixture always matches the
// server generator. The flags change the station capacity. It runs the
// command once for each preset and flag list. The command writes a summary
// line to standard error, and a failure shows it. A test that calls it must
// use the needsGo options.
function generatedFile(preset, ...flags) {
  const key = [preset, ...flags].join(" ");
  if (!generatedFiles.has(key)) {
    generatedFiles.set(key, execFileSync("go", ["run", "./cmd/scenario", "-preset", preset, ...flags], {
      cwd: repoRoot, encoding: "utf8", maxBuffer: 64 * 1024 * 1024, stdio: "pipe",
    }));
  }
  return generatedFiles.get(key);
}

// generatedProject gives a generated project.
function generatedProject(preset, ...flags) {
  return JSON.parse(generatedFile(preset, ...flags));
}

test("projects keep version 1 through import and export", () => {
  const scenarios = { plain: connectedScenario(), service: serviceScenario(), banks: connectedScenario() };
  scenarios.express = { ...serviceScenario(), orderContract: "express-v1" };
  const station = scenarios.banks.network.stations[0];
  station.banks = [{ id: "a", entry: station.entry, exit: station.exit, berthIDs: station.berths.map((berth) => berth.id) }];
  for (const [name, config] of Object.entries(scenarios)) {
    const imported = editor.parseDocument(JSON.stringify(config)).scenario;
    assert.equal(imported.version, 1, name);
    assert.equal(editor.serializeDocument(config), JSON.stringify({ format: "podsim", version: 1, scenario: config }), `${name} export`);
  }
  for (const version of [2, 3, 4, 5]) {
    assert.throws(() => editor.parseDocument(JSON.stringify({ ...connectedScenario(), version })), new RegExp(`Project version ${version} is not supported: use version 1 with feature markers`));
  }
  assert.throws(() => editor.parseDocument(JSON.stringify({ ...connectedScenario(), version: 6 })), /The version field must be 1\./);
});

// decoderParity gives the shared table that the Go tests check against the
// server decoders: cmd/serve and the session project command.
function decoderParity() {
  const fixture = JSON.parse(fs.readFileSync(path.join(__dirname, "../internal/editormodel/testdata/decoder_parity.json"), "utf8"));
  return fixture.cases.map((item) => {
    const base = fixture.bases[item.base];
    assert.ok(base.includes(item.find), item.name);
    return { ...item, text: base.replace(item.find, () => item.replace) };
  });
}

test("import rejects a repeated member name as the server decoders do, and passes case variants to Go", () => {
  for (const item of decoderParity()) {
    const wrapped = `{"format":"podsim","version":1,"scenario":${item.text}}`;
    for (const text of [item.text, wrapped]) {
      assert.equal(editor.repeatedMember(text) !== null, item.duplicate, `${item.name} scan`);
      if (item.duplicate) assert.throws(() => editor.parseDocument(text), /repeats the member name/, item.name);
      // A file that the server accepts goes to Go unchanged. Go or the page rejects the others.
      else if (item.valid) assert.deepEqual(editor.parseDocument(text).scenario, JSON.parse(item.text), item.name);
    }
  }
  // The page gives its own messages for these files. Member names match
  // exactly, so a name that differs in case is not the network or version.
  const config = JSON.parse(decoderParity()[0].text);
  for (const [change, message] of [
    [(file) => { file.version = 6; }, /The version field must be 1\./],
    [(file) => { file.version = 5; }, /Project version 5 is not supported: use version 1 with feature markers\./],
    [(file) => { delete file.version; }, /The version field must be 1\./],
    [(file) => { file.network = null; }, /The network field must be an object/],
    [(file) => { delete file.network; file["vers\u0131on"] = 1; }, /no format field and no network field/],
    [(file) => { file.NETWORK = file.network; delete file.network; }, /no format field and no network field/],
    [(file) => { file.Version = file.version; delete file.version; }, /The version field must be 1\./],
  ]) {
    const file = structuredClone(config); change(file);
    assert.throws(() => editor.parseDocument(JSON.stringify(file)), message);
  }
  const deferred = { ...config, Network: null, Version: null };
  assert.deepEqual(editor.parseDocument(JSON.stringify(deferred)).scenario, deferred);
});

test("the repeated member scan reads names after escapes and ignores values", () => {
  const cases = [
    ['{"a":1,"b":{"a":2},"c":[{"a":3},{"a":4}]}', null],
    ['{"a":"\\"a\\":","b":"x\\\\","a":2}', "a"],
    ['{"a":[],"b":{},"c":[[{"d":1,"d":2}]]}', "d"],
    ['{"\\u0062":1,"b":2}', "b"],
    ['{"a":"{\\"b\\":1,\\"b\\":2}"}', null],
    ['[{"x":1},{"x":2}]', null],
    ['{"A":1,"a":2}', null],
    ['{"k":"a\\",\\"k\\":1"}', null],
  ];
  for (const [text, name] of cases) {
    JSON.parse(text);
    assert.equal(editor.repeatedMember(text), name, text);
  }
});

test("the lane class options show the classes that native allows on each project", () => {
  const config = connectedScenario();
  const lane = config.network.lanes[0].id;
  for (const project of [config, { ...config, orderContract: "express-v1" }]) {
    const state = editor.laneClassState(project, lane);
    assert.deepEqual(state.classes, ["legacy", "compact"]); assert.equal(state.disabled, false);
    assert.match(state.hint, /no class list, so Legacy and Compact pods can use it/);
  }
  const classed = structuredClone(config);
  classed.network.lanes[0].vehicleClasses = ["express", "compact"];
  assert.deepEqual(editor.laneClassState(classed, lane), { classes: ["compact", "express"], disabled: false, hint: "" });
  const source = fs.readFileSync(path.join(__dirname, "editor.js"), "utf8");
  for (const name of ["legacy", "compact", "group", "express"]) assert.match(source, new RegExp(`<input data-edit="lane-class" data-class="${name}" type="checkbox">`));
  assert.match(source, /queueGeometryEdit\(\{ action: "laneClasses", id, value \}, \{ controls: \[control\] \}\);/);
  assert.match(source, /box\.checked = classes\.classes\.includes\(box\.dataset\.class\); box\.disabled = classes\.disabled;/);
});

function nodePosition(config, id) {
  return config.network.nodes.find((node) => node.id === id).position;
}

// assertNear checks that two points are less than 1e-6 m apart.
function assertNear(actual, want, name) {
  assert.ok(Math.hypot(actual.x - want.x, actual.y - want.y) < 1e-6, `${name}: got ${JSON.stringify(actual)}, want ${JSON.stringify(want)}`);
}

// angleGap gives the difference in degrees between two bearings.
function angleGap(a, b) {
  return Math.abs((((a - b) % 360) + 540) % 360 - 180);
}

// Tottenham Court Road in the generated London project. The berths are 90 m
// and 165 m to the left of the entry-exit line.
const tottenhamCourtRoad = {
  entry: { x: -186.74321535145924, y: -1227.1687914758268 },
  exit: { x: -5.481657944129211, y: -1142.645139127687 },
  berths: [{ x: -58.07679309113132, y: -1266.4746661350553 }, { x: -26.380423460578925, y: -1334.447750162804 }],
};

test("the station bearing is the direction from the entry to the exit", () => {
  const cases = [
    { name: "right", exit: { x: 10, y: 0 }, want: 90 },
    { name: "down", exit: { x: 0, y: 10 }, want: 180 },
    { name: "left", exit: { x: -10, y: 0 }, want: 270 },
    { name: "up", exit: { x: 0, y: -10 }, want: 0 },
    { name: "up and to the right", exit: { x: 10, y: -10 }, want: 45 },
    { name: "up and to the left", exit: { x: -10, y: -10 }, want: 315 },
    { name: "entry and exit at one point", exit: { x: 0, y: 0 }, want: 90 },
  ];
  for (const tc of cases) assert.ok(angleGap(editor.stationBearing({ x: 0, y: 0 }, tc.exit), tc.want) < 1e-9, tc.name);
  assert.equal(Math.round(editor.stationBearing(tottenhamCourtRoad.entry, tottenhamCourtRoad.exit)), 115);
});

test("the station shape holds the station nodes along the station axes", () => {
  const pad = editor.STATION_PADDING;
  const cases = [
    { name: "a new station", entry: { x: 64, y: 100 }, exit: { x: 136, y: 100 }, points: [{ x: 100, y: 130 }], want: { center: { x: 100, y: 115 }, width: 72 + 2 * pad, height: 30 + 2 * pad, angle: 0, top: 100 - pad } },
    { name: "a station that points down", entry: { x: 100, y: 64 }, exit: { x: 100, y: 136 }, points: [{ x: 70, y: 100 }], want: { center: { x: 85, y: 100 }, width: 72 + 2 * pad, height: 30 + 2 * pad, angle: 90, top: 64 - pad } },
    { name: "a station that points left", entry: { x: 136, y: 100 }, exit: { x: 64, y: 100 }, points: [{ x: 100, y: 60 }, { x: 80, y: 80 }], want: { center: { x: 100, y: 80 }, width: 72 + 2 * pad, height: 40 + 2 * pad, angle: 180, top: 60 - pad } },
    { name: "entry and exit at one point", entry: { x: 5, y: 5 }, exit: { x: 5, y: 5 }, points: [], want: { center: { x: 5, y: 5 }, width: 2 * pad, height: 2 * pad, angle: 0, top: 5 - pad } },
  ];
  for (const tc of cases) {
    const shape = editor.stationShape(tc);
    assertNear(shape.center, tc.want.center, tc.name);
    for (const key of ["width", "height", "angle", "top"]) assert.ok(Math.abs(shape[key] - tc.want[key]) < 1e-9, `${tc.name} ${key}: ${shape[key]}`);
  }
});

test("fleet rows give each station its pod count and berth limit", () => {
  const twoStations = () => fixtureDraft("fleetTwoStations");
  const cases = [
    { name: "no stations", config: () => editor.emptyConfig(), want: [] },
    { name: "no pods", config: twoStations, want: [{ name: "Alpha", count: 0, max: 1 }, { name: "Beta", count: 0, max: 2 }] },
    { name: "pods at both stations", config: () => fixtureDraft("fleetBothStations"), want: [{ name: "Alpha", count: 1, max: 1 }, { name: "Beta", count: 2, max: 2 }] },
    { name: "pod at an unknown station", config: () => { const config = twoStations(); config.fleet.push({ id: "09", stationID: "gone", berthID: "gone-berth" }); return config; }, want: [{ name: "Alpha", count: 0, max: 1 }, { name: "Beta", count: 0, max: 2 }] },
  ];
  for (const tc of cases) {
    const config = tc.config();
    const want = tc.want.map((row, index) => ({ id: config.network.stations[index].id, ...row }));
    assert.deepEqual(editor.fleetRows(config), want, tc.name);
  }
});

test("the selection card gives the fields of the selected item", () => {
  // The draft has Alpha and Beta with lanes between them, a second berth at
  // Beta, Gamma turned to 359.6 degrees, Delta with no exit node, and a
  // junction at (64.25, 160).
  const config = fixtureDraft("selection");
  const [alpha, beta, gamma, delta] = config.network.stations;
  const lane = (from, to) => config.network.lanes.find((item) => item.from === from && item.to === to);
  const [straight, curved] = [lane(alpha.exit, beta.entry), lane(beta.exit, alpha.entry)];
  const junction = config.network.nodes.at(-1);
  delete alpha.parkingOnly;
  beta.parkingOnly = true;
  straight.speedLimit = 12.5;
  curved.speedLimit = 11.25;
  curved.control = { x: 220, y: 40 };
  const cases = [
    { name: "no selection", selection: null, want: null },
    {
      name: "station with one berth and no ParkingOnly", selection: { type: "station", id: alpha.id },
      want: { type: "station", id: alpha.id, name: "Alpha", bearing: 90, parkingOnly: false, canRemove: false, berths: [{ id: alpha.berths[0].id, selected: false }] },
    },
    {
      name: "parking station with a marked berth", selection: { type: "station", id: beta.id, berth: beta.berths[1].id },
      want: { type: "station", id: beta.id, name: "Beta", bearing: 90, parkingOnly: true, canRemove: true, berths: [{ id: beta.berths[0].id, selected: false }, { id: beta.berths[1].id, selected: true }] },
    },
    {
      name: "turned station rounds the bearing", selection: { type: "station", id: gamma.id },
      want: { type: "station", id: gamma.id, name: "Gamma", bearing: 0, parkingOnly: false, canRemove: false, berths: [{ id: gamma.berths[0].id, selected: false }] },
    },
    {
      name: "station with no exit node", selection: { type: "station", id: delta.id },
      want: { type: "station", id: delta.id, name: "Delta", bearing: 0, parkingOnly: false, canRemove: false, berths: [{ id: delta.berths[0].id, selected: false }] },
    },
    {
      name: "straight lane", selection: { type: "lane", id: straight.id },
      want: { type: "lane", id: straight.id, from: alpha.exit, to: beta.entry, speed: 45, length: editor.laneLength(config, straight), curved: false },
    },
    {
      name: "curved lane rounds the speed", selection: { type: "lane", id: curved.id },
      want: { type: "lane", id: curved.id, from: beta.exit, to: alpha.entry, speed: 41, length: editor.laneLength(config, curved), curved: true },
    },
    { name: "junction", selection: { type: "node", id: junction.id }, want: { type: "node", id: junction.id, x: 64.25, y: 160 } },
    { name: "missing station", selection: { type: "station", id: "gone" }, want: null },
    { name: "missing lane", selection: { type: "lane", id: "gone" }, want: null },
    { name: "missing junction", selection: { type: "node", id: "gone" }, want: null },
  ];
  for (const tc of cases) assert.deepEqual(editor.selectionCard(config, tc.selection), tc.want, tc.name);
});

test("a berth remove from the keyboard focuses the next, then the previous berth row", () => {
  const cases = [
    { name: "first row gives the new first row", berths: ["b1", "b2", "b3"], removed: "b1", want: "b2" },
    { name: "middle row gives the next row", berths: ["b1", "b2", "b3"], removed: "b2", want: "b3" },
    { name: "last row gives the previous row", berths: ["b1", "b2", "b3"], removed: "b3", want: "b2" },
    { name: "one berth left gives Add physical berth", berths: ["b1", "b2"], removed: "b2", want: "" },
    { name: "unknown berth gives Add physical berth", berths: ["b1", "b2", "b3"], removed: "gone", want: "" },
  ];
  for (const tc of cases) assert.equal(editor.berthFocusID(tc.berths, tc.removed), tc.want, tc.name);

  // On a station of the draft, the focused row is a row of the station
  // after the remove, and its Remove button is enabled. When the Remove
  // buttons are disabled, the focus goes to Add physical berth.
  // removed gives a copy of config without one berth of the first station.
  const removed = (config, berthID) => {
    const copy = structuredClone(config); const [station] = copy.network.stations;
    station.berths = station.berths.filter((berth) => berth.id !== berthID);
    return copy;
  };
  let config = fixtureDraft("undo").three;
  const stationID = config.network.stations[0].id;
  const selection = { type: "station", id: stationID };
  for (const count of [3, 2]) {
    const berthIDs = editor.selectionCard(config, selection).berths.map((berth) => berth.id);
    assert.equal(berthIDs.length, count);
    for (const berthID of berthIDs) {
      const after = editor.selectionCard(removed(config, berthID), selection);
      const focused = editor.berthFocusID(berthIDs, berthID);
      assert.equal(focused !== "", after.canRemove, `${count} berths, remove ${berthID}`);
      if (focused) assert.ok(after.berths.some((berth) => berth.id === focused), `${count} berths, remove ${berthID}`);
    }
    config = removed(config, berthIDs[0]);
  }
});

test("undo and redo keep the selection and the Selection panel focus, or focus the map when the item is gone", () => {
  // step gives the drafts before and after an undo or a redo of one change
  // from first to second. An undo restores first, and a redo restores
  // second.
  const step = (first, second, redo) => structuredClone(redo ? { before: first, after: second } : { before: second, after: first });
  // The undo fixture has the connected draft with two and three berths at
  // Alpha, three berths without the second, Gamma added, and Beta deleted.
  const one = connectedScenario(); const [alpha, beta] = one.network.stations; const lane = one.network.lanes[0];
  const { two, three, withoutB2, added, deletedBeta } = fixtureDraft("undo");
  const [b1, b2, b3] = three.network.stations[0].berths.map((berth) => berth.id);
  const curved = structuredClone(one); curved.network.lanes[0].control = { x: 220, y: 40 };
  const gamma = added.network.stations.at(-1);
  const station = { type: "station", id: alpha.id }; const button = (action, id = "") => ({ action, id });
  const cases = [
    { name: "undo of Add physical berth keeps its button", drafts: step(two, three), selection: station, control: button("add-berth"), want: { selection: station, focus: button("add-berth") } },
    { name: "redo of a curve keeps the curve button", drafts: step(one, curved, true), selection: { type: "lane", id: lane.id }, control: button("toggle-curve"), want: { selection: { type: "lane", id: lane.id }, focus: button("toggle-curve") } },
    { name: "undo of a berth remove keeps Remove of the same row", drafts: step(three, withoutB2), selection: station, control: button("remove-berth", b3), want: { selection: station, focus: button("remove-berth", b3) } },
    { name: "undo of a new berth moves Remove to the previous row", drafts: step(two, three), selection: station, control: button("remove-berth", b3), want: { selection: station, focus: button("remove-berth", b2) } },
    { name: "redo of a berth remove moves Remove to the next row", drafts: step(three, withoutB2, true), selection: station, control: button("remove-berth", b2), want: { selection: station, focus: button("remove-berth", b3) } },
    { name: "one berth left gives Add physical berth", drafts: step(one, two), selection: station, control: button("remove-berth", b1), want: { selection: station, focus: button("add-berth") } },
    { name: "a station selection with a gone berth stays", drafts: step(two, three), selection: { ...station, berth: b3 }, control: null, want: { selection: { ...station, berth: b3 }, focus: null } },
    { name: "undo of a new station clears the selection and focuses the map", drafts: step(one, added), selection: { type: "station", id: gamma.id }, control: button("delete-station"), want: { selection: null, focus: "map" } },
    { name: "redo of a station delete clears the selection and focuses the map", drafts: step(one, deletedBeta, true), selection: { type: "station", id: beta.id }, control: button("add-berth"), want: { selection: null, focus: "map" } },
    { name: "focus outside the panel stays with the item kept", drafts: step(two, three), selection: station, control: null, want: { selection: station, focus: null } },
    { name: "focus outside the panel stays with the item removed", drafts: step(one, added), selection: { type: "station", id: gamma.id }, control: null, want: { selection: null, focus: null } },
    { name: "no selection", drafts: step(two, three), selection: null, control: null, want: { selection: null, focus: null } },
  ];
  for (const tc of cases) {
    const after = structuredClone(tc.drafts.after);
    assert.deepEqual(editor.undoFocus({ ...tc.drafts, selection: tc.selection, control: tc.control }), tc.want, tc.name);
    // The choice does not change the draft that the history restores.
    assert.deepEqual(tc.drafts.after, after, tc.name);
  }
});

test("the checks summary gives errors, then warnings, then ready", () => {
  assert.deepEqual(editor.validationSummary(["A.", "B."], ["C."]), { tone: "bad", text: "2 problems must be fixed." });
  assert.deepEqual(editor.validationSummary(["A."], []), { tone: "bad", text: "1 problem must be fixed." });
  assert.deepEqual(editor.validationSummary([], ["C."]), { tone: "warn", text: "The scenario is ready to apply. It has 1 warning." });
  assert.deepEqual(editor.validationSummary([], ["C.", "D."]), { tone: "warn", text: "The scenario is ready to apply. It has 2 warnings." });
  assert.deepEqual(editor.validationSummary([], []), { tone: "good", text: "The scenario is ready to apply." });
});

test("the problem count gives the number of errors", () => {
  const cases = [
    { name: "no errors", count: 0, want: "" },
    { name: "one error", count: 1, want: "1 problem" },
    { name: "two errors", count: 2, want: "2 problems" },
    { name: "many errors", count: 150, want: "150 problems" },
  ];
  for (const item of cases) assert.equal(editor.problemCountText(item.count), item.want, item.name);
});

// checkClock gives a check timer on the mock clock of the test. runs counts
// the check runs, and each run gives its number.
function checkClock(t) {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  const counter = { runs: 0 };
  const checks = editor.createCheckTimer({ delay: editor.CHECK_DELAY, clock: globalThis, run: () => { counter.runs += 1; return counter.runs; } });
  return { checks, counter };
}

test("the checks run once after a series of changes", async (t) => {
  // Each step waits wait milliseconds, then calls schedule or run. The task
  // of the call then takes render milliseconds, or 0, and ends. runs is the
  // number of check runs after the step.
  const cases = [
    { name: "one change", steps: [{ call: "schedule", runs: 0 }, { wait: 149, runs: 0 }, { wait: 1, runs: 1 }, { wait: 1000, runs: 1 }] },
    {
      name: "changes closer than the delay",
      steps: [{ call: "schedule", runs: 0 }, { wait: 100, call: "schedule", runs: 0 }, { wait: 149, call: "schedule", runs: 0 }, { wait: 149, runs: 0 }, { wait: 1, runs: 1 }, { wait: 1000, runs: 1 }],
    },
    { name: "changes farther apart than the delay", steps: [{ call: "schedule", runs: 0 }, { wait: 150, call: "schedule", runs: 1 }, { wait: 150, runs: 2 }] },
    { name: "a change with a slow render", steps: [{ call: "schedule", render: 250, runs: 0 }, { wait: 149, runs: 0 }, { wait: 1, runs: 1 }] },
    {
      name: "a series of changes with slow renders",
      steps: [{ call: "schedule", render: 250, runs: 0 }, { call: "schedule", render: 250, runs: 0 }, { wait: 100, call: "schedule", render: 250, runs: 0 }, { wait: 150, runs: 1 }],
    },
    { name: "a run now cancels the wait", steps: [{ call: "schedule", runs: 0 }, { wait: 50, call: "run", runs: 1 }, { wait: 1000, runs: 1 }] },
    { name: "a run now with no wait", steps: [{ call: "run", runs: 1 }, { wait: 1000, runs: 1 }] },
  ];
  for (const item of cases) {
    await t.test(item.name, (t) => {
      const { checks, counter } = checkClock(t);
      for (const [index, step] of item.steps.entries()) {
        if (step.wait) t.mock.timers.tick(step.wait);
        if (step.call === "run") assert.equal(checks.run(), counter.runs, `${item.name} step ${index}`);
        else if (step.call) checks.schedule();
        if (step.call) t.mock.timers.tick(step.render || 0);
        assert.equal(counter.runs, step.runs, `${item.name} step ${index}`);
        if (step.call) assert.equal(checks.waiting, step.call === "schedule", `${item.name} step ${index}`);
      }
      assert.equal(checks.waiting, false, item.name);
    });
  }
});

test("a check selection finds only the objects of the draft", () => {
  const { config, arrival } = chainScenario();
  const [alpha, beta] = config.network.stations;
  const cases = [
    { name: "no target", target: null, want: null },
    { name: "a station", target: { type: "station", id: beta.id }, want: { type: "station", id: beta.id } },
    { name: "a missing station", target: { type: "station", id: "station-9" }, want: null },
    { name: "a lane", target: { type: "lane", id: "lane-1" }, want: { type: "lane", id: "lane-1" } },
    { name: "a missing lane", target: { type: "lane", id: "lane-99" }, want: null },
    { name: "a berth", target: { type: "berth", id: beta.berths[0].id }, want: { type: "station", id: beta.id, berth: beta.berths[0].id } },
    { name: "a missing berth", target: { type: "berth", id: "berth-9" }, want: null },
    { name: "a station entry node", target: { type: "node", id: alpha.entry }, want: { type: "station", id: alpha.id } },
    { name: "a berth chain node", target: { type: "node", id: arrival }, want: { type: "station", id: alpha.id } },
    { name: "a missing node", target: { type: "node", id: "node-99" }, want: null },
    { name: "a type that the map does not show", target: { type: "pod", id: config.fleet[0].id }, want: null },
  ];
  const select = editor.checkSelector(config);
  for (const item of cases) {
    assert.deepEqual(editor.checkSelection(config, item.target), item.want, item.name);
    assert.deepEqual(select(item.target), item.want, item.name);
  }
});

test("the focus stays on a Checks link when the checks run again", () => {
  // link gives a link with its own target, so only its text matches.
  // station gives a link for the station with the ID id.
  const link = (text) => ({ text, type: "lane", id: text });
  const station = (id, text) => ({ text, type: "station", id });
  const [a, b, c, d, x, y, z] = ["a", "b", "c", "d", "x", "y", "z"].map(link);
  const cases = [
    { name: "same check at the same place", before: [a, b, c], focused: 1, after: [a, b, c], want: "b" },
    { name: "same check at a new place", before: [a, b, c], focused: 1, after: [x, a, b, c], want: "b" },
    { name: "a gone check gives the next link", before: [a, b, c], focused: 1, after: [a, c], want: "c" },
    { name: "a gone first check gives the new first link", before: [a, b, c], focused: 0, after: [b, c], want: "b" },
    { name: "a gone last check gives the previous link", before: [a, b, c], focused: 2, after: [a, b], want: "b" },
    { name: "a gone last check gives the previous link, not a new link", before: [a, b, c], focused: 2, after: [a, b, x, y], want: "b" },
    { name: "a gone check and a gone next link give the previous link", before: [a, b, c, d], focused: 1, after: [x, d, a], want: "a" },
    { name: "a changed text gives the link for the same item",
      before: [a, station("S", "S cannot reach 3 passenger stations.")], focused: 1,
      after: [a, station("S", "S cannot reach 2 passenger stations.")], want: "S cannot reach 2 passenger stations." },
    { name: "a changed text at a new place gives the link for the same item",
      before: [station("S2", "S2 cannot reach 3 passenger stations."), station("S3", "S3 cannot reach 3 passenger stations.")], focused: 0,
      after: [station("S1", "S1 cannot reach 4 passenger stations."), station("S2", "S2 cannot reach 4 passenger stations."), station("S3", "S3 cannot reach 4 passenger stations.")],
      want: "S2 cannot reach 4 passenger stations." },
    { name: "a changed text gives the new text, not an old text for the same item",
      before: [station("S", "Station S needs a through lane."), station("S", "S cannot reach 3 passenger stations.")], focused: 1,
      after: [station("S", "Station S needs a through lane."), station("S", "S cannot reach 2 passenger stations.")], want: "S cannot reach 2 passenger stations." },
    { name: "a gone check gives another link for the same item before the next link",
      before: [station("S", "Station S needs a through lane."), station("S", "S cannot reach 3 passenger stations."), a], focused: 1,
      after: [station("S", "Station S needs a through lane."), a], want: "Station S needs a through lane." },
    { name: "a gone check gives the next link when its text changed",
      before: [station("S1", "S1 cannot reach 4 passenger stations."), station("S2", "S2 cannot reach 4 passenger stations.")], focused: 0,
      after: [station("S2", "S2 cannot reach 3 passenger stations.")], want: "S2 cannot reach 3 passenger stations." },
    { name: "no old link left gives the link at the same place", before: [a, b, c], focused: 1, after: [x, y, z], want: "y" },
    { name: "no old link left in a shorter list gives the last link", before: [a, b, c, d], focused: 3, after: [x], want: "x" },
    { name: "an empty list gives the heading", before: [a], focused: 0, after: [], want: "" },
    { name: "focus outside the list does not move", before: [a, b], focused: null, after: [a], want: null },
    { name: "focus outside an empty list does not move", before: [a], focused: null, after: [], want: null },
  ];
  for (const tc of cases) assert.equal(editor.checkFocusKey({ before: tc.before, focused: tc.focused, after: tc.after }), tc.want, tc.name);
});

test("the Checks heading can take the focus and shows a focus ring", () => {
  const html = fs.readFileSync(path.join(__dirname, "editor.html"), "utf8");
  assert.match(html, /<section id="checksPanel">\s*<div class="section-title"><h2 id="checksHeading" tabindex="-1">Checks<\/h2>/);
  const ring = cssRules(fs.readFileSync(path.join(__dirname, "editor.css"), "utf8")).get("h2:focus-visible");
  assert.ok(ring && parseFloat(ring.outline) > 0, "h2:focus-visible has no outline");
});

test("a check selection moves the view only when the map does not show the item well", () => {
  // The draft is the connected draft with a junction at (400, 300) and a
  // lane from the exit of Alpha to the junction.
  const config = fixtureDraft("selectionPoint");
  const junction = config.network.nodes.at(-1).id;
  const [alpha] = config.network.stations;
  const lane = config.network.lanes.at(-1);
  const exit = config.network.nodes.find((node) => node.id === alpha.exit).position;
  const points = [
    { name: "a junction", selection: { type: "node", id: junction }, want: { x: 400, y: 300 } },
    { name: "a straight lane", selection: { type: "lane", id: lane.id }, want: { x: (exit.x + 400) / 2, y: (exit.y + 300) / 2 } },
    { name: "a curved lane", selection: { type: "lane", id: lane.id }, control: { x: 300, y: 400 }, want: { x: exit.x / 4 + 150 + 100, y: exit.y / 4 + 200 + 75 } },
    { name: "a station", selection: { type: "station", id: alpha.id, berth: alpha.berths[0].id }, want: { x: 100, y: 100 } },
    { name: "a missing lane", selection: { type: "lane", id: "lane-99" }, want: null },
  ];
  for (const item of points) {
    const scenario = structuredClone(config);
    if (item.control) scenario.network.lanes.at(-1).control = item.control;
    assert.deepEqual(editor.selectionPoint(scenario, item.selection), item.want, item.name);
  }

  const size = { width: 900, height: 700 };
  const views = [
    { name: "an item on the map", view: { x: 0, y: 0, scale: 1 }, point: { x: 400, y: 300 }, want: { x: 0, y: 0, scale: 1 } },
    { name: "an item near the edge", view: { x: 0, y: 0, scale: 1 }, point: { x: 880, y: 300 }, want: { x: -430, y: 50, scale: 1 } },
    { name: "an item off the map", view: { x: 0, y: 0, scale: 2 }, point: { x: -100, y: 2000 }, want: { x: 650, y: -3650, scale: 2 } },
    { name: "an item on a map zoomed out below the label scale", view: { x: 10, y: 20, scale: 0.05 }, point: { x: 4000, y: 3000 }, want: { x: -1550, y: -1150, scale: editor.NODE_LABEL_SCALE } },
  ];
  for (const item of views) assert.deepEqual(editor.focusView({ view: item.view, point: item.point, size }), item.want, item.name);
});

test("portable documents round trip the scenario and local background", () => {
  const config = connectedScenario();
  const background = { dataURL: dataURL(pngBytes(2, 1)), x: -10, y: 5, width: 800, height: 600, opacity: 0.4 };
  const parsed = editor.parseDocument(editor.serializeDocument(config, background));

  assert.deepEqual(parsed.scenario, config);
  assert.deepEqual(parsed.background, background);
  // The image key of the page stays in this browser.
  const exported = editor.serializeDocument(config, { ...background, imageKey: TEST_KEY, frameState: "none" });
  assert.ok(!exported.includes("imageKey") && !exported.includes(TEST_KEY), "the export has no image key");
  assert.deepEqual(Object.keys(JSON.parse(exported).background), ["dataURL", "x", "y", "width", "height", "opacity"]);
  assert.throws(() => editor.parseDocument('{"format":"podsim","version":2}'), /version field must be 1/);
  assert.throws(() => editor.parseDocument('{broken'), /not valid JSON/);
});

test("the image checks read the size and the header of the image data before a decode", () => {
  const limit = editor.IMAGE_FILE_BYTES;
  // padded gives a JPEG of exactly bytes bytes: a frame header and zeros.
  const padded = (bytes) => { const head = jpegBytes(640, 480); return Buffer.concat([head, Buffer.alloc(bytes - head.length)]); };
  const accepted = [
    { name: "a PNG", url: dataURL(pngBytes(3, 2)), want: { width: 3, height: 2 } },
    { name: "a JPEG", url: dataURL(jpegBytes(640, 480), "jpeg"), want: { width: 640, height: 480 } },
    { name: "a JPEG with the PNG media type", url: dataURL(jpegBytes(640, 480), "png"), want: { width: 640, height: 480 } },
    { name: "a JPEG at the byte limit", url: dataURL(padded(limit), "jpeg"), want: { width: 640, height: 480 } },
    { name: "a PNG at the side limit", url: dataURL(pngBytes(editor.IMAGE_MAX_SIDE, 1, true)), want: { width: editor.IMAGE_MAX_SIDE, height: 1 } },
    { name: "a PNG at the pixel limit", url: dataURL(pngBytes(8192, 8192, true)), want: { width: 8192, height: 8192 } },
    { name: "a 48 megapixel photo", url: dataURL(jpegBytes(8064, 6048), "jpeg"), want: { width: 8064, height: 6048 } },
  ];
  for (const item of accepted) {
    const facts = editor.imageFacts(item.url);
    assert.deepEqual({ width: facts.width, height: facts.height }, item.want, item.name);
    // The bytes form gives the same facts, and the media type of the
    // signature.
    const bytes = editor.dataURLToBytes(item.url);
    assert.deepEqual(editor.imageBytesFacts(bytes), { ...facts, mime: item.name.includes("JPEG") || item.name.includes("photo") ? "image/jpeg" : "image/png" }, `${item.name}: bytes`);
    assert.deepEqual(Buffer.from(editor.dataURLToBytes(editor.bytesToDataURL(bytes, facts.mime))), Buffer.from(bytes), `${item.name}: round trip`);
  }
  assert.equal(editor.imageFacts(dataURL(padded(limit), "jpeg")).bytes, limit);
  assert.equal(editor.bytesToDataURL(editor.dataURLToBytes(dataURL(padded(limit), "jpeg")), "image/jpeg"), dataURL(padded(limit), "jpeg"), "a large image converts in parts");
  assert.throws(() => editor.imageBytesFacts(new Uint8Array(pngBytes(3, 2))), /not bytes/, "a Uint8Array is not an ArrayBuffer");
  assert.throws(() => editor.imageBytesFacts(new ArrayBuffer(limit + 1)), /must be 8 MiB or smaller/);

  const rejected = [
    { name: "bytes that are not an image", url: "data:image/png;base64,AAAA", want: /not a valid PNG or JPEG/ },
    { name: "a PNG signature with no IHDR chunk", url: dataURL(pngBytes(3, 2).subarray(0, 12)), want: /not a valid PNG or JPEG/ },
    { name: "a JPEG with no frame header", url: dataURL(Buffer.from([0xff, 0xd8, 0xff, 0xd9]), "jpeg"), want: /not a valid PNG or JPEG/ },
    { name: "a JPEG with a scan before the frame header", url: dataURL(Buffer.from([0xff, 0xd8, 0xff, 0xda, 0x00, 0x02, 0xff, 0xd9]), "jpeg"), want: /not a valid PNG or JPEG/ },
    { name: "text that is not base64", url: "data:image/png;base64,AA!A", want: /not valid base64/ },
    { name: "base64 with a wrong length", url: "data:image/png;base64,AAA", want: /not valid base64/ },
    { name: "a JPEG one byte over the limit", url: dataURL(padded(limit + 1), "jpeg"), want: /must be 8 MiB or smaller/ },
    { name: "a PNG with no width", url: dataURL(pngBytes(0, 5, true)), want: /has no pixels/ },
    { name: "a JPEG with no height", url: dataURL(jpegBytes(640, 0), "jpeg"), want: /has no pixels/ },
    { name: "a PNG over the side limit", url: dataURL(pngBytes(editor.IMAGE_MAX_SIDE + 1, 1, true)), want: /is 16385 by 1 pixels/ },
    { name: "a PNG over the pixel limit", url: dataURL(pngBytes(8193, 8192, true)), want: /is 8193 by 8192 pixels. The limit is 16384 pixels on each side and 67108864 pixels in total/ },
    { name: "a small PNG of 30000 by 30000 pixels", url: dataURL(pngBytes(30000, 30000, true)), want: /is 30000 by 30000 pixels/ },
    { name: "a JPEG of 65535 by 65535 pixels", url: dataURL(jpegBytes(65535, 65535), "jpeg"), want: /is 65535 by 65535 pixels/ },
  ];
  for (const item of rejected) {
    assert.throws(() => editor.imageFacts(item.url), item.want, item.name);
    if (!/base64/.test(item.want.source)) assert.throws(() => editor.imageBytesFacts(editor.dataURLToBytes(item.url)), item.want, `${item.name}: bytes`);
  }
});

test("a project import and a stored background reject image data that is not valid", () => {
  const config = connectedScenario();
  const background = { dataURL: dataURL(pngBytes(4, 2)), x: 0, y: 0, width: 400, height: 200, opacity: 0.5 };
  assert.deepEqual(editor.parseDocument(editor.serializeDocument(config, background)).background, background);
  const cases = [
    { name: "PNG bytes that are not an image", dataURL: "data:image/png;base64,iVBORw0KGgoAAAAAAAAAAA==", want: /not a valid PNG or JPEG/ },
    { name: "a large image in a small file", dataURL: dataURL(pngBytes(40000, 40000, true)), want: /is 40000 by 40000 pixels/ },
  ];
  for (const item of cases) {
    const bad = { ...background, dataURL: item.dataURL };
    assert.throws(() => editor.parseDocument(editor.serializeDocument(config, bad)), item.want, `${item.name}: import`);
    assert.throws(() => editor.storedBackground(recordOf({ ...TEST_BACKGROUND, dataURL: item.dataURL })), item.want, `${item.name}: restore`);
  }
});

test("the decode check needs a decoded image with the size of its header, and closes each bitmap", async () => {
  const bytes = editor.dataURLToBytes(dataURL(pngBytes(4, 2)));
  let closes = 0; const types = [];
  const decoded = (size) => ({ decode: async (blob) => { types.push(blob.type); return { ...size, close() { closes += 1; } }; } });
  const facts = await editor.checkImageBytes(bytes, decoded({ width: 4, height: 2 }));
  assert.deepEqual([facts.width, facts.height, facts.mime, types], [4, 2, "image/png", ["image/png"]]);
  assert.equal((await editor.checkImageBytes(bytes, decoded({ width: 2, height: 4 }))).width, 4, "a turned JPEG");
  let decodes = 0;
  const count = { decode: async () => { decodes += 1; return { width: 1, height: 1, close() {} }; } };
  await assert.rejects(editor.checkImageBytes(editor.dataURLToBytes(dataURL(pngBytes(30000, 30000, true))), count), /is 30000 by 30000 pixels/);
  assert.equal(decodes, 0, "the header check comes before the decode");
  await assert.rejects(editor.checkImageBytes(bytes, { decode: async () => { throw new Error("EncodingError"); } }), { message: "The browser cannot decode the background image." });
  await assert.rejects(editor.checkImageBytes(bytes, decoded({ width: 4, height: 3 })), /does not have the size in its header/);
  await assert.rejects(editor.checkImageBytes(bytes, decoded({ width: 0, height: 0 })), /has no pixels/);
  assert.equal(closes, 4, "each decoded bitmap closes once, also after a failed check");
  const aborted = new AbortController(); aborted.abort();
  await assert.rejects(editor.checkImageBytes(bytes, decoded({ width: 4, height: 2 }), aborted.signal), { name: "AbortError" });
  assert.equal(closes, 5, "an aborted check closes its bitmap");
});

test("a stored background restore decodes the image, and an edit during the decode is stopped", async () => {
  const record = recordOf(TEST_BACKGROUND);
  const cases = [
    { name: "an image that decodes", decoded: { width: 4, height: 2 }, want: TEST_BACKGROUND, warnings: [] },
    { name: "an image that does not decode", decoded: null, want: null, warnings: [`The browser cannot decode the background image. ${editor.STORED_BACKGROUND_KEPT_TEXT}`] },
  ];
  for (const item of cases) {
    const factory = fakeIndexedDB(); const store = editor.openRecordStore(factory, editor.BACKGROUND_STORE);
    await store.put(DRAFT_KEY, record);
    let finish; let started;
    const decoding = new Promise((resolve) => { started = resolve; });
    const decode = () => new Promise((resolve, reject) => {
      finish = () => (item.decoded ? resolve(item.decoded) : reject(new Error("EncodingError"))); started();
    });
    const page = startupPage(factory, { decode });
    await decoding;
    // Without the gate, a scenario edit during the decode leaves an undo
    // step with no background.
    let edits = 0;
    const edit = () => { edits += 1; page.history.replace({ scenario: { ...page.history.value.scenario, name: "Edited" }, background: page.history.value.background }); };
    const event = page.document.dispatch("change", fakeElement("scenarioName"), edit);
    assert.deepEqual([edits, event.prevented], [0, true], item.name);
    finish(); await page.started;
    assert.deepEqual([page.history.value.background, page.history.canUndo, page.warnings], [item.want, false, item.warnings], item.name);
    assert.deepEqual([await store.get(DRAFT_KEY), page.counts.writes], [record, 0], `${item.name}: the keeper does not write`);
    // An edit after the startup keeps the background in its undo step.
    page.document.dispatch("change", fakeElement("scenarioName"), edit);
    assert.equal(page.history.undo(), true);
    assert.deepEqual([edits, page.history.value.background], [1, item.want], item.name);
  }
});

test("the project import limit holds an export at the project and image limits", () => {
  // The name pads the compact project to the server limit. Only the size
  // matters here, so the long name does not have to be valid.
  const config = connectedScenario();
  config.name += "n".repeat(editor.SERVER_PROJECT_BYTES - Buffer.byteLength(JSON.stringify(config)));
  assert.equal(Buffer.byteLength(JSON.stringify(config)), editor.SERVER_PROJECT_BYTES);
  // An image of IMAGE_FILE_BYTES has this base64 form. Each number has the
  // longest JSON form of a JavaScript number.
  const base64 = "A".repeat(Math.ceil(editor.IMAGE_FILE_BYTES / 3) * 4 - 1) + "=";
  assert.equal(Buffer.from(base64, "base64").length, editor.IMAGE_FILE_BYTES);
  const wide = -1.2345678901234567e-300;
  const background = { dataURL: `data:image/jpeg;base64,${base64}`, x: wide, y: wide, width: wide, height: wide, opacity: wide };
  assert.equal(background.dataURL.length, editor.dataURLBytes(editor.IMAGE_FILE_BYTES));
  // The asset has a frame of wide numbers and each license member at its
  // limit, with characters that JSON writes as six bytes.
  const escaped = (limit) => "\u0001".repeat(limit);
  const license = Object.fromEntries(Object.entries(editor.LICENSE_LIMITS).map(([key, limit]) => [key, escaped(limit)]));
  const frame = { south: wide, north: wide, west: wide, east: wide, source: "web-mercator" };
  const exported = editor.serializeDocument(config, { ...background, asset: { frameState: "detached", frame, license } });
  assert.ok(exported.includes(`"method":"${"\\u0001".repeat(editor.LICENSE_LIMITS.method)}"`), "the asset is in the export");
  assert.ok(!exported.includes("\n"), "the export is not compact JSON");
  assert.ok(Buffer.byteLength(exported) <= editor.PROJECT_FILE_BYTES, `the export has ${Buffer.byteLength(exported)} bytes, more than ${editor.PROJECT_FILE_BYTES}`);
  assert.equal(editor.PROJECT_FILE_BYTES % (1024 * 1024), 0);
});

// geoAt gives the geo reference of a project at a latitude and a longitude
// in degrees, as the Go project package makes it.
function geoAt(latitude, longitude) {
  return { latitude, longitude, projection: "equirectangular", radius: 6371000 };
}

// LONDON_GEO is the geo reference of the London preset.
const LONDON_GEO = geoAt(51.5074, -0.1278);

test("the projection of three London stations gives the positions of the London preset", () => {
  // TestLondonPointsGolden in internal/scenarios writes the file from
  // londonPoint.
  const file = JSON.parse(fs.readFileSync(path.join(repoRoot, "internal", "scenarios", "testdata", "london_points.json"), "utf8"));
  assert.deepEqual(file.geo, LONDON_GEO);
  assert.equal(file.points.length, 3);
  for (const item of file.points) {
    const at = editor.projectPoint(file.geo, item.latitude, item.longitude);
    assertNear(at, { x: item.x, y: item.y }, item.id);
  }
});

test("the placement of a frame follows the geo reference, and alignment allows 0.5 m", () => {
  const frame = { south: 51.50, north: 51.52, west: -0.14, east: -0.10, source: "equirectangular" };
  const other = geoAt(51.52, -0.10);
  const place = editor.framePlacement(frame, LONDON_GEO);
  const corner = editor.projectPoint(LONDON_GEO, 51.52, -0.14);
  assert.deepEqual([place.x, place.y], [corner.x, corner.y], "the north-west corner");
  assert.ok(Math.abs(place.height - 6371000 * 0.02 * Math.PI / 180) < 1e-6, "the height is exact north to south");
  assert.ok(place.width > 0 && place.height > 0);
  const moved = editor.framePlacement(frame, other);
  assert.ok(Math.abs(moved.x - place.x) > 1000 && Math.abs(moved.width - place.width) > 0.5, "a different reference moves and scales the placement");
  assert.equal(editor.frameAligned(place, frame, LONDON_GEO), true);
  assert.equal(editor.frameAligned(moved, frame, other), true);
  assert.equal(editor.frameAligned(place, frame, other), false);
  assert.equal(editor.frameAligned({ ...place, x: place.x + 0.5 }, frame, LONDON_GEO), true, "0.5 m is aligned");
  assert.equal(editor.frameAligned({ ...place, height: place.height - 0.6 }, frame, LONDON_GEO), false, "0.6 m is not aligned");
  assert.equal(editor.frameAligned(place, frame, null), false, "no reference is not aligned");
});

test("the Web Mercator resample gives the latitude and the source row of each output row", () => {
  const frame = { south: 51.4, north: 51.6, west: -0.2, east: 0, source: "web-mercator" };
  const rows = editor.resampleRows(frame, 4, 1000);
  rows.forEach((row, index) => assert.ok(Math.abs(row.latitude - (51.6 - (index + 0.5) * 0.05)) < 1e-12, `row ${index}`));
  // The inverse Web Mercator formula gives the latitude of each source row.
  const top = editor.mercatorY(51.6); const span = top - editor.mercatorY(51.4);
  for (const row of rows) {
    const y = top - row.source / 1000 * span;
    assert.ok(Math.abs((2 * Math.atan(Math.exp(y)) - Math.PI / 2) * 180 / Math.PI - row.latitude) < 1e-12, `source row ${row.source}`);
  }
  // Web Mercator stretches the north, so the source rows of the north half
  // are farther apart than the rows of the south half.
  assert.ok(rows[1].source - rows[0].source > rows[3].source - rows[2].source);
  assert.ok(rows[0].source > 125 && rows[3].source > 875, "the north stretches");
  assert.deepEqual(editor.resampleRows(frame, 1, 10).map((row) => row.latitude), [51.5]);
  // The output size comes from the meters for each pixel in the reference.
  const geo = geoAt(51.5, -0.1);
  const place = editor.framePlacement(frame, geo);
  const size = editor.resampleSize(frame, geo, 10);
  assert.deepEqual([size.width, size.height, size.error], [Math.round(place.width / 10), Math.round(place.height / 10), ""]);
  assert.match(editor.resampleSize(frame, geo, 1).error, /The limit is 4096 pixels on each side/);
  assert.match(editor.resampleSize(frame, geo, 0).error, /must be a positive number/);
  assert.deepEqual([editor.resampleSize(frame, geo, 1e6).width, editor.resampleSize(frame, geo, 1e6).height], [1, 1], "each side is at least 1 pixel");
});

test("import names the missing or wrong field", () => {
  const scenario = connectedScenario();
  const cases = [
    { name: "an array", file: [], message: "The file must contain a JSON object." },
    { name: "null", file: null, message: "The file must contain a JSON object." },
    { name: "a string", file: "podsim", message: "The file must contain a JSON object." },
    { name: "a different format", file: { format: "other", version: 1, scenario }, message: 'The format field must be "podsim".' },
    { name: "an export of a different version", file: { format: "podsim", version: 2, scenario }, message: "The version field must be 1." },
    { name: "an export with no version", file: { format: "podsim", scenario }, message: "The version field must be 1." },
    { name: "an export with no scenario", file: { format: "podsim", version: 1 }, message: "The scenario field must be an object." },
    { name: "an export with a scenario list", file: { format: "podsim", version: 1, scenario: [scenario] }, message: "The scenario field must be an object." },
    { name: "an API reply", file: { revision: 3, project: scenario }, message: "The file has no format field and no network field." },
    { name: "a project with a network list", file: { ...scenario, network: [] }, message: "The network field must be an object." },
    { name: "a project with no version", file: { ...scenario, version: undefined }, message: "The version field must be 1." },
    { name: "a project of an earlier version", file: { ...scenario, version: 3 }, message: "Project version 3 is not supported: use version 1 with feature markers." },
  ];
  for (const item of cases) {
    assert.throws(() => editor.parseDocument(JSON.stringify(item.file)), { message: item.message }, item.name);
  }
});

test("portable OD profiles round trip", () => {
  const config = connectedScenario();
  const [alpha, beta] = config.network.stations;
  config.demandProfiles = [{
    id: "weekday", name: "Weekday", bands: [{ id: "am", name: "AM peak", startMinute: 420, durationMinutes: 180 }],
    stations: [alpha.id, beta.id], flows: [[0, 1, 3]],
  }];
  config.demand = { enabled: true, perMinute: 12, pattern: "profile", destination: "", profile: "weekday", band: "am", seed: 9 };

  assert.deepEqual(editor.parseDocument(editor.serializeDocument(config)).scenario, config);
});

test("portable projects preserve the shared ride party limit", () => {
  const config = connectedScenario();
  config.sharedRidePartyLimit = 4;
  assert.equal(editor.parseDocument(editor.serializeDocument(config)).scenario.sharedRidePartyLimit, 4);
});

test("portable projects preserve the shared ride mode and stop limit", () => {
  const config = connectedScenario();
  Object.assign(config, { sharedRidePartyLimit: 4, sharedRideMode: "drop-offs", sharedRideMaxStops: 5 });
  const imported = editor.parseDocument(editor.serializeDocument(config)).scenario;
  assert.deepEqual([imported.sharedRideMode, imported.sharedRideMaxStops], ["drop-offs", 5]);
});

test("portable projects preserve the platoon limit", () => {
  for (const limit of [0, 2, 3, 4]) {
    const config = connectedScenario();
    config.platoonLimit = limit;
    assert.equal(editor.parseDocument(editor.serializeDocument(config)).scenario.platoonLimit, limit, `limit ${limit}`);
  }
});

test("the platoon field offers the limits that the server accepts", () => {
  // The server accepts 0 for no platoons, and 2 to 4 pods.
  const html = fs.readFileSync(path.join(__dirname, "editor.html"), "utf8");
  const select = html.match(/<select id="platoonLimit">(.*?)<\/select>/);
  assert.ok(select, "the editor has no platoon field");
  const values = [...select[1].matchAll(/<option value="(\d+)">/g)].map((match) => Number(match[1]));
  assert.deepEqual(values, [0, 2, 3, 4]);
});

test("a fixture test skips without Go unless PODSIM_REQUIRE_GO is 1", () => {
  const reason = "needs Go on PATH, run mise run test:web";
  const cases = [
    { name: "Go", hasGo: true, env: {}, skip: false },
    { name: "Go and PODSIM_REQUIRE_GO", hasGo: true, env: { PODSIM_REQUIRE_GO: "1" }, skip: false },
    { name: "no Go", hasGo: false, env: {}, skip: reason },
    { name: "no Go and PODSIM_REQUIRE_GO", hasGo: false, env: { PODSIM_REQUIRE_GO: "1" }, skip: false },
    { name: "no Go and PODSIM_REQUIRE_GO 0", hasGo: false, env: { PODSIM_REQUIRE_GO: "0" }, skip: reason },
  ];
  for (const item of cases) assert.deepEqual(fixtureOptions(item.hasGo, item.env), { skip: item.skip }, item.name);
});

test("each station shape on the generated london project holds its station nodes", needsGo, () => {
  const config = generatedProject("london-central");
  const owners = editor.stationNodeOwners(config); const pad = editor.STATION_PADDING;
  for (const station of config.network.stations) {
    const points = [...owners].filter(([, stationID]) => stationID === station.id).map(([id]) => nodePosition(config, id));
    const shape = editor.stationShape({ entry: nodePosition(config, station.entry), exit: nodePosition(config, station.exit), points });
    const radians = shape.angle * Math.PI / 180; const cos = Math.cos(radians); const sin = Math.sin(radians);
    for (const at of points) {
      const x = at.x - shape.center.x; const y = at.y - shape.center.y;
      assert.ok(Math.abs(x * cos + y * sin) <= shape.width / 2 - pad + 1e-6, station.id);
      assert.ok(Math.abs(-x * sin + y * cos) <= shape.height / 2 - pad + 1e-6, station.id);
    }
  }
});

// movedItems gives the nodes, lanes, and stations whose drawn position
// differs between two versions of a scenario. A station shape holds all of
// its station nodes, so a station moves when one of its nodes moves.
function movedItems(before, after) {
  const same = (a, b) => JSON.stringify(a) === JSON.stringify(b);
  const positions = new Map(after.network.nodes.map((node) => [node.id, node.position]));
  const controls = new Map(after.network.lanes.map((lane) => [lane.id, lane.control]));
  const moved = new Set(before.network.nodes.filter((node) => !same(node.position, positions.get(node.id))).map((node) => node.id));
  const owners = editor.stationNodeOwners(before);
  return {
    nodeIDs: [...moved],
    laneIDs: before.network.lanes.filter((lane) => moved.has(lane.from) || moved.has(lane.to) || !same(lane.control, controls.get(lane.id))).map((lane) => lane.id),
    stationIDs: before.network.stations.filter((station) => [...moved].some((id) => owners.get(id) === station.id)).map((station) => station.id),
  };
}

test("a drag redraws only the items that it moves", () => {
  // The draft is the berth chain draft with a junction at (220, 260) and a
  // lane pair from the exit of Alpha to the junction.
  const config = fixtureDraft("drag"); const { arrival, departure } = fixtureDraft("chain");
  const [alpha, beta] = config.network.stations;
  const junction = config.network.nodes.at(-1).id;
  // moveNode gives a copy of config with the node id at (x, y). moveStation
  // gives a copy with each station node of the station moved by (dx, dy).
  const moveNode = (id, x, y) => {
    const copy = structuredClone(config); copy.network.nodes.find((node) => node.id === id).position = { x: x, y: y };
    return copy;
  };
  const moveStation = (id, dx, dy) => {
    const copy = structuredClone(config); const owners = editor.stationNodeOwners(config);
    for (const node of copy.network.nodes) if (owners.get(node.id) === id) node.position = { x: node.position.x + dx, y: node.position.y + dy };
    return copy;
  };
  const through = config.network.lanes.find((lane) => lane.from === alpha.entry && lane.to === alpha.exit);
  through.control = { x: 100, y: 60 };
  const curved = structuredClone(config);
  curved.network.lanes.find((lane) => lane.id === through.id).control = { x: 90, y: 40 };
  const cases = [
    { name: "a station drag", drag: { type: "station", id: alpha.id }, after: moveStation(alpha.id, 20, -10) },
    { name: "a junction drag", drag: { type: "node", id: junction }, after: moveNode(junction, 300, 280) },
    { name: "a curve drag", drag: { type: "control", id: through.id }, after: curved },
    { name: "an entry drag", drag: { type: "node", id: alpha.entry }, after: moveNode(alpha.entry, 40, 80) },
    { name: "a berth drag", drag: { type: "node", id: alpha.berths[0].node }, after: moveNode(alpha.berths[0].node, 100, 200) },
    { name: "a berth chain drag", drag: { type: "node", id: arrival }, after: moveNode(arrival, 50, 170) },
  ];
  for (const item of cases) assert.deepEqual(editor.dragTargets(config, item.drag), movedItems(config, item.after), item.name);

  const targets = editor.dragTargets(config, { type: "station", id: alpha.id });
  for (const id of [arrival, departure]) assert.ok(targets.nodeIDs.includes(id), id);
  assert.deepEqual(targets.stationIDs, [alpha.id]);
  assert.ok(!targets.nodeIDs.includes(beta.entry) && !targets.nodeIDs.includes(junction));
  assert.deepEqual(editor.dragTargets(config, { type: "control", id: through.id }), { nodeIDs: [], laneIDs: [through.id], stationIDs: [] });
});

test("fit centers the network at a scale of up to 3", () => {
  const cases = [
    { name: "an empty map", bounds: null, size: { width: 940, height: 824 }, want: { x: 0, y: 0, scale: 1 } },
    { name: "a small network", bounds: { minX: 0, minY: 0, maxX: 100, maxY: 50 }, size: { width: 940, height: 824 }, want: { x: 320, y: 337, scale: 3 } },
    { name: "a wide network", bounds: { minX: -1000, minY: 0, maxX: 1000, maxY: 100 }, size: { width: 500, height: 400 }, want: { x: 250, y: 190, scale: 0.2 } },
    { name: "a tall network", bounds: { minX: 0, minY: -1000, maxX: 100, maxY: 1000 }, size: { width: 500, height: 400 }, want: { x: 242.5, y: 200, scale: 0.15 } },
    { name: "a map smaller than its margin", bounds: { minX: 0, minY: 0, maxX: 1000, maxY: 1000 }, size: { width: 50, height: 50 }, want: { x: 24.5, y: 24.5, scale: 0.001 } },
  ];
  for (const item of cases) assert.deepEqual(editor.fitView(item.bounds, item.size), item.want, item.name);
});

test("the network bounds hold the nodes and the background", () => {
  const background = { x: -50, y: 10, width: 200, height: 100 };
  const nodes = editor.emptyConfig();
  nodes.network.nodes = [{ id: "node-1", position: { x: 20, y: -30 } }, { id: "node-2", position: { x: 400, y: 60 } }];
  const cases = [
    { name: "an empty map", config: editor.emptyConfig(), background: null, want: null },
    { name: "a background only", config: editor.emptyConfig(), background, want: { minX: -50, minY: 10, maxX: 150, maxY: 110 } },
    { name: "nodes only", config: nodes, background: null, want: { minX: 20, minY: -30, maxX: 400, maxY: 60 } },
    { name: "nodes and a background", config: nodes, background, want: { minX: -50, minY: -30, maxX: 400, maxY: 110 } },
  ];
  for (const item of cases) assert.deepEqual(editor.networkBounds(item.config, item.background), item.want, item.name);
});

test("the station flow count on the generated london project gives the flows that name the station", needsGo, () => {
  const config = generatedProject("london-central");
  const station = config.network.stations.find((item) => !item.parkingOnly);
  const naming = config.demandProfiles.flatMap((profile) => profile.flows.filter((flow) => profile.stations[flow[0]] === station.id || profile.stations[flow[1]] === station.id));
  assert.ok(naming.length > 0);
  assert.equal(editor.stationFlowCount(config, station.id), naming.length);
});

test("fit shows the whole London network below the 0.15 zoom floor", needsGo, () => {
  const config = generatedProject("london-central");
  const size = { width: 940, height: 824 };
  const bounds = editor.networkBounds(config, null);
  const view = editor.fitView(bounds, size);

  // London fits at about 0.055. The network fills the map width or height
  // inside the margin, and every node is on the map.
  assert.ok(view.scale < editor.MIN_ZOOM, `fit scale ${view.scale}`);
  const fill = Math.max((bounds.maxX - bounds.minX) * view.scale / (size.width - 100), (bounds.maxY - bounds.minY) * view.scale / (size.height - 100));
  assert.ok(Math.abs(fill - 1) < 1e-9, `fill ${fill}`);
  for (const node of config.network.nodes) {
    const x = view.x + node.position.x * view.scale; const y = view.y + node.position.y * view.scale;
    assert.ok(x >= 0 && x <= size.width && y >= 0 && y <= size.height, node.id);
  }
  assert.equal(editor.zoomScale({ scale: view.scale, factor: 0.8, fitScale: view.scale }), view.scale);
  const owners = editor.stationNodeOwners(config);
  for (const node of config.network.nodes.filter((item) => !owners.has(item.id))) {
    assert.equal(editor.nodeLabelSize({ scale: view.scale, id: node.id, selection: null, linkFrom: "" }), 0, node.id);
  }
});

test("the zoom floor is the lower of 0.15 and the fit scale", () => {
  const cases = [
    { name: "a small network stops at 0.15", scale: 0.2, factor: 0.5, fitScale: 1, want: editor.MIN_ZOOM },
    { name: "a large network zooms out below 0.15", scale: 0.2, factor: 0.5, fitScale: 0.055, want: 0.1 },
    { name: "a large network stops at its fit scale", scale: 0.06, factor: 0.8, fitScale: 0.055, want: 0.055 },
    { name: "zoom in stops at 5", scale: 4.5, factor: 1.25, fitScale: 1, want: 5 },
    { name: "a step out below the floor keeps the scale", scale: 0.05, factor: 0.8, fitScale: 0.1, want: 0.05 },
    { name: "a step in below the floor zooms in", scale: 0.05, factor: 1.25, fitScale: 0.1, want: 0.0625 },
  ];
  for (const item of cases) assert.equal(editor.zoomScale({ scale: item.scale, factor: item.factor, fitScale: item.fitScale }), item.want, item.name);
});

test("junction labels show only when zoomed in or selected", () => {
  // want is the font size of the label on the screen, in pixels. It is 0 when
  // the label does not show.
  const selected = { type: "node", id: "node-1" };
  const cases = [
    { name: "a junction at a scale below the London fit scale", scale: 0.0625, want: 0 },
    { name: "a junction just below the label scale", scale: editor.NODE_LABEL_SCALE * 0.99, want: 0 },
    { name: "a junction at the label scale", scale: editor.NODE_LABEL_SCALE, want: 4.5 },
    { name: "a junction at the fit scale of the default example", scale: 1, want: 9 },
    { name: "a junction zoomed in", scale: 2, want: 18 },
    { name: "a junction when another junction is selected", scale: 0.25, selection: { type: "node", id: "node-2" }, want: 0 },
    { name: "a junction when a lane with the same ID is selected", scale: 0.25, selection: { type: "lane", id: "node-1" }, want: 0 },
    { name: "the selected junction zoomed out", scale: 0.0625, selection: selected, want: 9 },
    { name: "the selected junction above the label scale", scale: 0.75, selection: selected, want: 9 },
    { name: "the selected junction zoomed in", scale: 2, selection: selected, want: 18 },
    { name: "the start node of a new guideway zoomed out", scale: 0.0625, linkFrom: "node-1", want: 9 },
    { name: "a junction when another node starts a new guideway", scale: 0.0625, linkFrom: "node-2", want: 0 },
  ];
  for (const item of cases) {
    const size = editor.nodeLabelSize({ scale: item.scale, id: "node-1", selection: item.selection || null, linkFrom: item.linkFrom || "" });
    assert.equal(size * item.scale, item.want, item.name);
  }
});

test("a lane with a reverse lane is one lane of a pair", () => {
  // The pair draft has junctions at (0, 0), (100, 0), and (100, 100), a lane
  // pair from the first to the second, and a one-way lane from the second
  // to the third. The other draft has no reverse lane in the pair.
  const config = fixtureDraft("pair"); const withoutReverse = fixtureDraft("pairWithoutReverse");
  const [a] = config.network.nodes.map((node) => node.id);
  for (const draft of [config, withoutReverse]) draft.network.lanes.push({ ...draft.network.lanes.at(-1), id: "parallel" });
  const [forward, reverse, oneWay] = config.network.lanes.map((lane) => lane.id);
  const withStationLane = structuredClone(config);
  Object.assign(withStationLane.network.lanes[1], { stationID: "station-1", stationRole: "through" });
  const cases = [
    { name: "a new paired guideway", config, want: [forward, reverse] },
    { name: "a pair with a station lane", config: withStationLane, want: [forward, reverse] },
    { name: "a pair after a delete of one lane", config: withoutReverse, want: [] },
    { name: "one-way lanes between the same nodes", config: { network: { lanes: config.network.lanes.filter((lane) => lane.id === oneWay || lane.id === "parallel") } }, want: [] },
    { name: "a lane from a node to the same node", config: { network: { lanes: [{ id: "loop", from: a, to: a }] } }, want: [] },
    { name: "the connected scenario", config: connectedScenario(), want: [] },
  ];
  for (const item of cases) assert.deepEqual([...editor.pairedLaneIDs(item.config)].sort(), item.want.sort(), item.name);
});

// curvePoint gives the point of a quadratic curve at t. A curve with no
// control point is a straight line.
function curvePoint(curve, t) {
  const control = curve.control || { x: (curve.from.x + curve.to.x) / 2, y: (curve.from.y + curve.to.y) / 2 };
  const u = 1 - t;
  return { x: u * u * curve.from.x + 2 * u * t * control.x + t * t * curve.to.x, y: u * u * curve.from.y + 2 * u * t * control.y + t * t * curve.to.y };
}

function assertNear(actual, want, name) {
  assert.ok(Math.abs(actual.x - want.x) < 1e-9 && Math.abs(actual.y - want.y) < 1e-9, `${name}: got ${JSON.stringify(actual)}, want ${JSON.stringify(want)}`);
}

test("a lane of a pair moves to the right of its direction of travel", () => {
  const r = Math.SQRT1_2;
  const cases = [
    { name: "no offset", lane: { from: { x: 0, y: 0 }, to: { x: 100, y: 0 } }, want: { from: { x: 0, y: 0 }, to: { x: 100, y: 0 }, middle: { x: 50, y: 0 } } },
    // The map Y axis points down, so the right of a lane to the east is +Y.
    { name: "a lane to the east", lane: { from: { x: 0, y: 0 }, to: { x: 100, y: 0 }, offset: 4 }, want: { from: { x: 0, y: 4 }, to: { x: 100, y: 4 }, middle: { x: 50, y: 4 } } },
    { name: "the reverse lane to the west", lane: { from: { x: 100, y: 0 }, to: { x: 0, y: 0 }, offset: 4 }, want: { from: { x: 100, y: -4 }, to: { x: 0, y: -4 }, middle: { x: 50, y: -4 } } },
    { name: "a lane to the south", lane: { from: { x: 0, y: 0 }, to: { x: 0, y: 30 }, offset: 2 }, want: { from: { x: -2, y: 0 }, to: { x: -2, y: 30 }, middle: { x: -2, y: 15 } } },
    { name: "a curve with no offset", lane: { from: { x: 0, y: 0 }, to: { x: 100, y: 0 }, control: { x: 50, y: 50 } }, want: { from: { x: 0, y: 0 }, to: { x: 100, y: 0 }, control: { x: 50, y: 50 }, middle: { x: 50, y: 25 } } },
    // Each end moves along the normal of the curve at that end, and the
    // middle point moves along the normal of the line from end to end.
    { name: "a curve", lane: { from: { x: 0, y: 0 }, to: { x: 100, y: 0 }, control: { x: 50, y: 50 }, offset: 2 }, want: { from: { x: -2 * r, y: 2 * r }, to: { x: 100 + 2 * r, y: 2 * r }, control: { x: 50, y: 54 - 2 * r }, middle: { x: 50, y: 27 } } },
    { name: "the reverse curve", lane: { from: { x: 100, y: 0 }, to: { x: 0, y: 0 }, control: { x: 50, y: 50 }, offset: 2 }, want: { from: { x: 100 - 2 * r, y: -2 * r }, to: { x: 2 * r, y: -2 * r }, control: { x: 50, y: 46 + 2 * r }, middle: { x: 50, y: 23 } } },
    { name: "a control point on the start node", lane: { from: { x: 0, y: 0 }, to: { x: 100, y: 0 }, control: { x: 0, y: 0 }, offset: 4 }, want: { from: { x: 0, y: 4 }, to: { x: 100, y: 4 }, control: { x: 0, y: 4 }, middle: { x: 25, y: 4 } } },
    { name: "two nodes at the same point", lane: { from: { x: 5, y: 5 }, to: { x: 5, y: 5 }, offset: 4 }, want: { from: { x: 5, y: 5 }, to: { x: 5, y: 5 }, middle: { x: 5, y: 5 } } },
  ];
  for (const item of cases) {
    const curve = editor.laneCurve(item.lane);
    assert.deepEqual(Object.keys(curve).sort(), Object.keys(item.want).sort(), item.name);
    for (const key of Object.keys(item.want)) assertNear(curve[key], item.want[key], `${item.name} ${key}`);
    // The chevron shows at the middle point, so it is on the drawn curve.
    assertNear(curvePoint(curve, 0.5), curve.middle, `${item.name} middle on the curve`);
  }
});

test("both lanes of a pair show at the same distance at each scale", () => {
  // The draft has a curved lane pair from (10, 20) to (130, 70).
  const config = fixtureDraft("curvedPair");
  const at = (id) => config.network.nodes.find((node) => node.id === id).position;
  for (const scale of [0.055, 0.5, 1, 5]) {
    const offset = editor.laneOffset({ paired: true, scale });
    const [forward, reverse] = config.network.lanes.map((lane) => editor.laneCurve({ from: at(lane.from), to: at(lane.to), control: lane.control, offset }));
    const gap = Math.hypot(forward.middle.x - reverse.middle.x, forward.middle.y - reverse.middle.y) * scale;
    assert.ok(Math.abs(gap - 2 * editor.LANE_PAIR_OFFSET) < 1e-9, `scale ${scale}: the middle points are ${gap} screen pixels apart`);
    assert.equal(editor.laneOffset({ paired: false, scale }), 0, `scale ${scale}: a lane that is not in a pair`);
  }
});

test("the lane path has a vertex at the middle point for the chevron", () => {
  const straight = editor.laneCurve({ from: { x: 0, y: 0 }, to: { x: 100, y: 0 }, offset: 4 });
  assert.equal(editor.lanePathData(straight), "M 0 4 L 50 4 L 100 4");
  const curve = editor.laneCurve({ from: { x: 0, y: 0 }, to: { x: 100, y: 0 }, control: { x: 40, y: 60 }, offset: 3 });
  const path = editor.lanePathData(curve);
  assert.match(path, /^M \S+ \S+ Q \S+ \S+ \S+ \S+ Q \S+ \S+ \S+ \S+$/);
  // The two halves of the path draw the same curve as the lane curve.
  const numbers = path.match(/-?[\d.e+-]+/g).map(Number);
  const point = (index) => ({ x: numbers[index], y: numbers[index + 1] });
  const halves = [{ from: point(0), control: point(2), to: point(4) }, { from: point(4), control: point(6), to: point(8) }];
  assertNear(halves[0].to, curve.middle, "the middle vertex");
  for (const t of [0, 0.1, 0.25, 0.4, 0.5, 0.6, 0.8, 1]) {
    const half = t < 0.5 ? halves[0] : halves[1];
    assertNear(curvePoint(half, t < 0.5 ? t * 2 : t * 2 - 1), curvePoint(curve, t), `t ${t}`);
  }
});

test("a lane shows its chevron only when it is long enough on the screen", () => {
  const cases = [
    { name: "a lane at the limit", length: editor.CHEVRON_LANE_LENGTH, scale: 1, want: true },
    { name: "a lane just below the limit", length: editor.CHEVRON_LANE_LENGTH - 0.1, scale: 1, want: false },
    { name: "a 30 m lane at the London fit scale", length: 30, scale: 0.055, want: false },
    { name: "a 500 m lane at the London fit scale", length: 500, scale: 0.055, want: true },
    { name: "a 30 m lane zoomed in", length: 30, scale: 1, want: true },
    { name: "a lane with nodes at the same point", length: 0, scale: 5, want: false },
    // The length is the length of the curve. Nodes 1 m apart with a 60 m
    // curve show a chevron at scale 1.
    { name: "a curve with nodes close together", length: editor.curveLength({ from: { x: 0, y: 0 }, to: { x: 1, y: 0 }, control: { x: 0.5, y: 60 } }), scale: 1, want: true },
  ];
  for (const item of cases) assert.equal(editor.showsChevron({ length: item.length, scale: item.scale }), item.want, item.name);
});

test("the length of a lane follows its curve", () => {
  // The curve from (0, 0) to (1, 0) with its control point at (0.5, 60) goes
  // 30 m up and back down. Its length is 60.03 m.
  const config = { network: { nodes: [{ id: "a", position: { x: 0, y: 0 } }, { id: "b", position: { x: 1, y: 0 } }] } };
  const cases = [
    { name: "a straight lane", got: editor.curveLength({ from: { x: 0, y: 0 }, to: { x: 30, y: 40 } }), want: 50 },
    { name: "a curve on the line between its nodes", got: editor.curveLength({ from: { x: 0, y: 0 }, to: { x: 100, y: 0 }, control: { x: 50, y: 0 } }), want: 100 },
    { name: "a curve with nodes close together", got: editor.curveLength({ from: { x: 0, y: 0 }, to: { x: 1, y: 0 }, control: { x: 0.5, y: 60 } }), want: 60.03 },
    { name: "the same curve as a lane", got: editor.laneLength(config, { from: "a", to: "b", control: { x: 0.5, y: 60 } }), want: 60.03 },
  ];
  for (const item of cases) assert.ok(Math.abs(item.got - item.want) < 0.05, `${item.name}: got ${item.got}, want ${item.want}`);
});

// fakeSession gives a fetch function that answers as the session API does.
// The live project has revision options.liveRevision, or 3, and
// options.paused is the state of the simulation before the apply. As on the
// server, a project command needs a paused simulation and the live project
// revision. An applied project gets revision 7, so a test can tell the
// revision of the reply from revision + 1. It also starts a new paused
// simulation with a new generation.
//
// options.before(command, live) runs before the server gets a command. It
// can change live, the values of the live session. It gives the rejection
// of the command, "network" for a command that the server does not get, or
// "lost" for a reply that the editor does not get. A rejection is an HTTP
// status, which gets command_rejected and a project save error, or an
// object with the status and the errorCode and error members of the
// acknowledgment. The server rejects commands with the error codes of the
// session: session_changed for another epoch or for a project command with
// another server start ID, stale_project for a project command with another
// project revision, and command_rejected for a project command to a running
// simulation. The server start ID starts as "start-1". commands records
// each command that the editor sent. options.stateSaved is the stateSaved
// member of the acknowledgment of an applied project. The acknowledgment
// omits the member when options.stateSaved is undefined.
// requestCommand gives the command of a request that postCommand sends. It
// decompresses a gzip body.
function requestCommand(init) {
  if (init.headers["Content-Encoding"] === "gzip") return JSON.parse(zlib.gunzipSync(Buffer.from(init.body)).toString());
  return JSON.parse(init.body);
}

// stateReply gives a response to a live state read with body and the
// state media type.
const stateReply = (body) => new Response(JSON.stringify(body), { headers: { "Content-Type": editor.LIVE_STATE_ACCEPT } });

function fakeSession(options) {
  const commands = [];
  const live = { epoch: "epoch-1", serverStart: "start-1", revision: options.liveRevision ?? 3, generation: 5, paused: options.paused };
  const reply = (status, body) => new Response(JSON.stringify(body), { status });
  const acknowledgment = (rejection, command) => ({
    epoch: live.epoch, revision: commands.length, projectRevision: live.revision, generation: live.generation,
    ...(!rejection && command && command.action === "project" && options.stateSaved !== undefined ? { stateSaved: options.stateSaved } : {}),
    ...(rejection ? { errorCode: rejection.errorCode, error: rejection.error } : {}),
  });
  const handle = (command) => {
    if (command.epoch !== live.epoch) return { errorCode: "session_changed", error: "The server session changed. Review the current state and try again." };
    if (command.action === "project" && command.serverStart && command.serverStart !== live.serverStart) return { errorCode: "session_changed", error: "The server restarted. Review the current state and try again." };
    if (command.action === "pause") { live.paused = Boolean(command.paused); return null; }
    if (!live.paused) return { errorCode: "command_rejected", error: "pause the simulation before applying a project" };
    if (command.projectRevision !== live.revision) return { errorCode: "stale_project", error: "the project changed; reload it before applying edits" };
    Object.assign(live, { revision: 7, generation: live.generation + 1, paused: true });
    return null;
  };
  const fetch = async (url, init) => {
    if (url === "/api/project") return reply(200, { revision: live.revision, project: options.project ?? connectedScenario() });
    if (url === "/api/state") {
      return stateReply({ topology: {}, frame: { state: { epoch: live.epoch, serverStart: live.serverStart, projectRevision: live.revision, generation: live.generation, simulation: { paused: live.paused } }, routes: [] } });
    }
    const command = requestCommand(init);
    commands.push(command);
    const failure = options.before ? options.before(command, live) : false;
    if (failure === "network") throw new TypeError("Failed to fetch");
    if (typeof failure === "number") return reply(failure, acknowledgment({ errorCode: "command_rejected", error: "save project: permission denied" }));
    if (typeof failure === "object" && failure) return reply(failure.status, acknowledgment(failure));
    const rejection = handle(command);
    if (failure === "lost") throw new TypeError("Failed to fetch");
    return reply(rejection ? 409 : 200, acknowledgment(rejection, command));
  };
  return { fetch, commands, live };
}

// failOn gives an options.before function for fakeSession. It gives failure
// for a command with the action. When paused is set, it gives failure only
// for a pause command with that paused value.
const failOn = (action, failure, paused) => (command) => command.action === action && (paused === undefined || command.paused === paused) && failure;

test("a failed apply resumes only the simulation that the editor paused", async () => {
  const conflict = "The live scenario changed. Your draft is safe.";
  const saveFailure = "Apply failed. Save project: permission denied.";
  const sessionChanged = "The server session changed. Your draft is safe.";
  const stopping = { status: 409, errorCode: "server_stopping", error: "The server is stopping. Try again after it restarts." };
  const cases = [
    { name: "apply succeeds", paused: false, wantRevision: 7, wantCommands: ["pause true", "project"], wantPaused: true },
    {
      name: "apply fails after the editor paused the simulation", paused: false, before: failOn("project", 409),
      wantCommands: ["pause true", "project", "pause false"], wantPaused: false,
      wantPause: "resumed", wantText: `${saveFailure} The editor resumed the simulation.`,
    },
    {
      name: "apply fails when the simulation was already paused", paused: true, before: failOn("project", 409),
      wantCommands: ["pause true", "project"], wantPaused: true,
      wantPause: "was-paused", wantText: `${saveFailure} The simulation remains paused.`,
    },
    {
      name: "the editor cannot resume the simulation", paused: false,
      before: (command) => (command.action === "project" || (command.action === "pause" && !command.paused)) && 409,
      wantCommands: ["pause true", "project", "pause false"], wantPaused: true,
      wantPause: "left-paused", wantText: `${saveFailure} The apply attempt paused the simulation and could not resume it.`,
    },
    {
      name: "the server does not get the project command", paused: false, before: failOn("project", "network"),
      wantCommands: ["pause true", "project", "pause false"], wantPaused: false,
      wantPause: "resumed", wantText: "Apply failed. Failed to fetch. The editor resumed the simulation.",
    },
    {
      name: "the server applies the project and the reply is lost", paused: false, before: failOn("project", "lost"),
      wantCommands: ["pause true", "project"], wantPaused: true, wantRevision: 7,
      wantPause: "restarted", wantText: "Apply failed. Failed to fetch. The simulation restarted after the pause. The editor did not resume it.",
    },
    {
      name: "another browser applies a project after the pause", paused: false,
      before: (command, live) => { if (command.action === "project") Object.assign(live, { revision: 4, generation: live.generation + 1 }); return false; },
      wantCommands: ["pause true", "project"], wantPaused: true, wantRevision: 4,
      wantPause: "restarted", wantText: `${conflict} The simulation restarted after the pause. The editor did not resume it.`,
    },
    {
      name: "the server restarts after the pause", paused: false,
      before: (command, live) => { if (command.action === "project") Object.assign(live, { epoch: "epoch-2", paused: false }); return false; },
      wantCommands: ["pause true", "project"], wantPaused: false,
      wantPause: "restarted", wantText: `${sessionChanged} The simulation restarted after the pause. The editor did not resume it.`,
    },
    {
      name: "a demand change after the pause keeps the simulation", paused: false,
      before: (command, live) => { if (command.action === "project") live.revision = 4; return false; },
      wantCommands: ["pause true", "project", "pause false"], wantPaused: false, wantRevision: 4,
      wantPause: "resumed", wantText: `${conflict} The editor resumed the simulation.`,
    },
    {
      name: "the server applies the pause and the reply is lost", paused: false, before: failOn("pause", "lost", true),
      wantCommands: ["pause true", "pause false"], wantPaused: false,
      wantPause: "resumed", wantText: "Apply failed. Failed to fetch. The editor resumed the simulation.",
    },
    {
      name: "the server rejects the pause", paused: false, before: failOn("pause", stopping, true),
      wantCommands: ["pause true"], wantPaused: false,
      wantPause: "not-paused", wantText: `Apply failed. ${stopping.error} The simulation was not paused.`,
    },
    {
      name: "the live project changed before the pause", paused: false, liveRevision: 5,
      wantCommands: [], wantPaused: false, wantRevision: 5,
      wantPause: "not-paused", wantText: `${conflict} The simulation was not paused.`,
    },
  ];
  for (const item of cases) {
    const server = fakeSession(item);
    const connection = { fetch: server.fetch, clientID: "editor-test", sequence: 0, epoch: "" };
    let applied = null;
    let error = null;
    try {
      applied = await editor.applyToServer({ connection, revision: 3, project: connectedScenario() });
    } catch (caught) { error = caught; }

    if (item.wantPause) {
      assert.ok(error, item.name);
      assert.equal(error.pause, item.wantPause, item.name);
      assert.equal(editor.applyFailureText(error), item.wantText, item.name);
    } else {
      assert.equal(error, null, item.name);
      assert.equal(applied.revision, item.wantRevision, item.name);
    }
    const commands = server.commands.map((command) => (command.action === "pause" ? `pause ${command.paused}` : command.action));
    assert.deepEqual(commands, item.wantCommands, item.name);
    for (const [index, command] of server.commands.entries()) {
      assert.deepEqual([command.client, command.sequence, command.epoch], ["editor-test", index + 1, "epoch-1"], item.name);
    }
    assert.equal(server.live.paused, item.wantPaused, item.name);
    assert.equal(server.live.revision, item.wantRevision ?? 3, item.name);
  }
});

test("a failed apply shows the reason from the server", async () => {
  const conflict = "The live scenario changed. Your draft is safe.";
  const conflictStatus = "Apply conflict. The live scenario changed.";
  const sessionChanged = "The server session changed. Your draft is safe.";
  const sessionStatus = "Apply conflict. The server session changed.";
  const failedStatus = "Apply failed. The draft stays on this page.";
  const saveError = "save project: create project file: open /srv/podsim/.podsim-project-1.tmp: permission denied";
  const cases = [
    {
      name: "the server finds a stale draft", paused: false,
      before: (command, live) => { if (command.action === "project") live.revision = 4; return false; },
      wantCode: "stale_project", wantText: `${conflict} The editor resumed the simulation.`, wantStatus: conflictStatus,
    },
    {
      name: "the editor finds a stale draft before the pause", paused: false, liveRevision: 5,
      wantCode: "stale_project", wantText: `${conflict} The simulation was not paused.`, wantStatus: conflictStatus,
    },
    {
      name: "the server cannot save the project file", paused: false,
      before: failOn("project", { status: 409, errorCode: "command_rejected", error: saveError }),
      wantCode: "command_rejected", wantStatus: failedStatus,
      wantText: "Apply failed. Save project: create project file: open /srv/podsim/.podsim-project-1.tmp: permission denied. The editor resumed the simulation.",
    },
    {
      name: "another browser resumes the simulation after the pause", paused: false,
      before: (command, live) => { if (command.action === "project") live.paused = false; return false; },
      wantCode: "command_rejected", wantStatus: failedStatus,
      wantText: "Apply failed. Pause the simulation before applying a project. The editor resumed the simulation.",
    },
    {
      name: "the server restarts before the pause", paused: false,
      before: (command, live) => { if (command.action === "pause") live.epoch = "epoch-2"; return false; },
      wantCode: "session_changed", wantText: `${sessionChanged} The simulation was not paused.`, wantStatus: sessionStatus,
    },
    {
      name: "the network fails", paused: false, before: failOn("project", "network"),
      wantCode: "", wantText: "Apply failed. Failed to fetch. The editor resumed the simulation.", wantStatus: failedStatus,
    },
  ];
  for (const item of cases) {
    const server = fakeSession(item);
    const connection = { fetch: server.fetch, clientID: "editor-test", sequence: 0, epoch: "" };
    await assert.rejects(editor.applyToServer({ connection, revision: 3, project: connectedScenario() }), (error) => {
      assert.equal(error.errorCode ?? "", item.wantCode, item.name);
      assert.equal(editor.applyFailureText(error), item.wantText, item.name);
      assert.equal(editor.applyFailureStatus(error), item.wantStatus, item.name);
      return true;
    }, item.name);
  }
});

// paddedScenario gives connectedScenario with a long name, so that its JSON
// has bytes bytes. The fake session does not check the name.
function paddedScenario(bytes) {
  const config = connectedScenario();
  config.name += "n".repeat(bytes - Buffer.byteLength(JSON.stringify(config)));
  assert.equal(Buffer.byteLength(JSON.stringify(config)), bytes);
  return config;
}

test("postCommand compresses a large command with gzip and sends a small command as plain JSON", async () => {
  const requests = [];
  const connection = {
    clientID: "editor-test", sequence: 0, epoch: "epoch-1",
    fetch: async (url, init) => { requests.push(init); return new Response(JSON.stringify({ epoch: "epoch-1" })); },
  };
  const project = paddedScenario(editor.SERVER_PROJECT_BYTES);
  await editor.postCommand(connection, { action: "pause", paused: true });
  await editor.postCommand(connection, { action: "project", projectRevision: 3, project });
  const [pause, apply] = requests;
  assert.equal(typeof pause.body, "string");
  assert.equal(pause.headers["Content-Encoding"], undefined);
  assert.ok(Buffer.byteLength(pause.body) <= editor.GZIP_COMMAND_BYTES);
  assert.ok(apply.body instanceof ArrayBuffer, "the project command body is an ArrayBuffer");
  assert.equal(apply.headers["Content-Encoding"], "gzip");
  assert.equal(apply.headers["Content-Type"], "application/json");
  assert.ok(apply.body.byteLength <= editor.SERVER_COMMAND_BYTES, `the compressed body has ${apply.body.byteLength} bytes`);
  assert.deepEqual(requestCommand(apply), { client: "editor-test", sequence: 2, epoch: "epoch-1", action: "project", projectRevision: 3, project });
});

test("the editor does not send a command that is larger than the server accepts", async () => {
  let requests = 0;
  const connection = { clientID: "editor-test", sequence: 0, epoch: "epoch-1", fetch: async () => { requests += 1; throw new Error("sent"); } };
  const envelope = Buffer.byteLength(JSON.stringify({ client: "editor-test", sequence: 1, epoch: "epoch-1", action: "pause", origin: "" }));
  const json = "x".repeat(editor.SERVER_COMMAND_JSON_BYTES + 1 - envelope);
  await assert.rejects(editor.postCommand(connection, { action: "pause", origin: json }), (error) => {
    assert.equal(error.status, 413);
    assert.equal(error.message, "The command has 32.07 MiB of JSON. The server accepts at most 32.06 MiB.");
    return true;
  });
  // Random base64 text compresses to about three quarters of its size.
  const random = require("node:crypto").randomBytes(16 * 1024 * 1024).toString("base64");
  await assert.rejects(editor.postCommand(connection, { action: "pause", origin: random }), (error) => {
    assert.equal(error.status, 413);
    assert.match(error.message, /^The compressed command has 16\.\d+ MiB\. The server accepts at most 16 MiB\.$/);
    return true;
  });
  assert.equal(requests, 0);
});

test("an apply that is too large fails before the pause, and a 413 reply gives the server limits", async () => {
  const server = fakeSession({ paused: false });
  const connection = { fetch: server.fetch, clientID: "editor-test", sequence: 0, epoch: "" };
  await assert.rejects(editor.applyToServer({ connection, revision: 3, project: paddedScenario(editor.SERVER_PROJECT_BYTES + 1) }), (error) => {
    assert.equal(error.status, 413);
    assert.equal(editor.applyFailureText(error), "Apply failed. The scenario has 32.01 MiB of JSON. The server accepts at most 32 MiB. The simulation was not paused.");
    assert.equal(editor.applyFailureStatus(error), "Apply failed. The draft stays on this page.");
    return true;
  });
  assert.deepEqual(server.commands, []);

  const refusing = fakeSession({ paused: false });
  const fetch = async (url, init) => {
    if (init && init.method && requestCommand(init).action === "project") return { ok: false, status: 413, json: async () => { throw new SyntaxError("not JSON"); } };
    return refusing.fetch(url, init);
  };
  const refused = { fetch, clientID: "editor-test", sequence: 0, epoch: "" };
  await assert.rejects(editor.applyToServer({ connection: refused, revision: 3, project: paddedScenario(editor.SERVER_PROJECT_BYTES) }), (error) => {
    assert.equal(error.status, 413);
    assert.equal(error.message, editor.SERVER_TOO_LARGE_TEXT);
    assert.equal(editor.applyFailureText(error), "Apply failed. The command is too large for the server. The server accepts at most 32.06 MiB of JSON and 16 MiB after compression. The editor resumed the simulation.");
    return true;
  });
  assert.deepEqual(commandList(refusing), ["pause true", "pause false"]);
});

test("an apply that the server refuses with plain text shows the text of the reply", async () => {
  const long = `the request origin ${"x".repeat(300)} is not permitted`;
  const cases = [
    {
      name: "503 with Retry-After", status: 503, headers: { "Retry-After": "1" }, text: "the server is busy with other large commands, try again\n",
      wantMessage: "the server is busy with other large commands, try again", wantRetryAfter: 1,
      wantText: "Apply failed. The server is busy with other large commands, try again. Wait 1 second, then apply again. The editor resumed the simulation.",
    },
    {
      name: "503 with Retry-After of more than one second", status: 503, headers: { "Retry-After": " 5 " }, text: "the server is busy with other large commands, try again",
      wantMessage: "the server is busy with other large commands, try again", wantRetryAfter: 5,
      wantText: "Apply failed. The server is busy with other large commands, try again. Wait 5 seconds, then apply again. The editor resumed the simulation.",
    },
    {
      name: "503 without Retry-After", status: 503, text: "the request ended while it waited for another large command",
      wantMessage: "the request ended while it waited for another large command", wantRetryAfter: null,
      wantText: "Apply failed. The request ended while it waited for another large command. Apply again later. The editor resumed the simulation.",
    },
    {
      name: "503 with a Retry-After date and no text", status: 503, headers: { "Retry-After": "Wed, 21 Oct 2026 07:28:00 GMT" }, text: "",
      wantMessage: "the server is busy (HTTP 503)", wantRetryAfter: null,
      wantText: "Apply failed. The server is busy (HTTP 503). Apply again later. The editor resumed the simulation.",
    },
    {
      name: "503 with Retry-After and only white space", status: 503, headers: { "Retry-After": "2" }, text: " \r\n\t ",
      wantMessage: "the server is busy (HTTP 503)", wantRetryAfter: 2,
      wantText: "Apply failed. The server is busy (HTTP 503). Wait 2 seconds, then apply again. The editor resumed the simulation.",
    },
    {
      name: "415 plain text", status: 415, headers: { "Accept-Encoding": "gzip" }, text: "unsupported content encoding \"br\"",
      wantMessage: "unsupported content encoding \"br\"", wantRetryAfter: null,
      wantText: "Apply failed. Unsupported content encoding \"br\". The editor resumed the simulation.",
    },
    {
      name: "a long 403 text is on one line and shorter", status: 403, text: `\n  ${long.replace("origin ", "origin\n\t")}  \n`,
      wantMessage: `${long.slice(0, 200)}...`, wantRetryAfter: null,
      wantText: `Apply failed. T${long.slice(1, 200)}... The editor resumed the simulation.`,
    },
    {
      name: "a 16 MiB 503 text gives only its start", status: 503, headers: { "Retry-After": "1" }, text: `${" ".repeat(1 << 20)}${"busy ".repeat(16 << 18)}`,
      wantMessage: `${"busy ".repeat(40).trimEnd()}...`, wantRetryAfter: 1,
      wantText: `Apply failed. B${"busy ".repeat(40).trimEnd().slice(1)}... Wait 1 second, then apply again. The editor resumed the simulation.`,
    },
    {
      name: "a text with much white space in the part that replyText reads", status: 503, text: `${"a".padEnd(4096)}b`,
      wantMessage: "a...", wantRetryAfter: null,
      wantText: "Apply failed. A... Apply again later. The editor resumed the simulation.",
    },
  ];
  for (const item of cases) {
    const server = fakeSession({ paused: false });
    const fetch = async (url, init) => {
      if (init && init.method && requestCommand(init).action === "project") {
        return new Response(item.text, { status: item.status, headers: { "Content-Type": "text/plain; charset=utf-8", ...item.headers } });
      }
      return server.fetch(url, init);
    };
    const connection = { fetch, clientID: "editor-test", sequence: 0, epoch: "" };
    await assert.rejects(editor.applyToServer({ connection, revision: 3, project: connectedScenario() }), (error) => {
      assert.equal(error.status, item.status, item.name);
      assert.equal(error.errorCode, "", item.name);
      assert.equal(error.message, item.wantMessage, item.name);
      assert.equal(error.retryAfter, item.wantRetryAfter, item.name);
      assert.equal(editor.applyFailureText(error), item.wantText, item.name);
      assert.equal(editor.applyFailureStatus(error), "Apply failed. The draft stays on this page.", item.name);
      return true;
    });
    assert.deepEqual(commandList(server), ["pause true", "pause false"], item.name);
  }
});

test("a JSON error reply gives its error text and no busy text, also with HTTP 503", async () => {
  const server = fakeSession({ paused: false, before: failOn("project", { status: 503, errorCode: "server_stopping", error: "The server is stopping. Try again after it restarts." }) });
  const connection = { fetch: server.fetch, clientID: "editor-test", sequence: 0, epoch: "" };
  await assert.rejects(editor.applyToServer({ connection, revision: 3, project: connectedScenario() }), (error) => {
    assert.equal(error.status, 503);
    assert.equal(error.errorCode, "server_stopping");
    assert.equal(error.message, "The server is stopping. Try again after it restarts.");
    assert.equal(error.retryAfter, null);
    assert.equal(editor.applyFailureText(error), "Apply failed. The server is stopping. Try again after it restarts. The editor resumed the simulation.");
    return true;
  });
});

test("an applied project warns when the server could not save the session state", async () => {
  const applied = "The scenario was applied. The simulation remains paused.";
  const unsaved = "The project is applied, but the server could not save the session state. A server crash can undo this change.";
  const cases = [
    { name: "the server saved the state", stateSaved: true, wantMessage: applied, wantWarning: false },
    { name: "the server could not save the state", stateSaved: false, wantMessage: unsaved, wantWarning: true },
    { name: "the reply has no stateSaved member", wantMessage: applied, wantWarning: false },
  ];
  for (const item of cases) {
    const server = fakeSession({ paused: false, stateSaved: item.stateSaved });
    const connection = { fetch: server.fetch, clientID: "editor-test", sequence: 0, epoch: "" };
    const result = await editor.applyToServer({ connection, revision: 3, project: connectedScenario() });
    assert.deepEqual(result, { revision: 7, stateSaved: item.stateSaved, serverStart: "start-1" }, item.name);
    assert.deepEqual(editor.applyToast(result), [item.wantMessage, item.wantWarning], item.name);
  }
});

test("the project command sends the server start ID of the draft", async () => {
  const sessionChanged = "The server session changed. Your draft is safe.";
  // want gives the server start ID in the project command, or null for a
  // command without the member.
  const cases = [
    { name: "the draft and the server have the same server start ID", serverStart: "start-1", wantCommands: ["pause true", "project"], want: "start-1", wantRevision: 7 },
    { name: "a draft without a server start ID", serverStart: "", wantCommands: ["pause true", "project"], want: null, wantRevision: 7 },
    {
      name: "the server restarted before the apply", serverStart: "start-0", wantCommands: [], wantPaused: false,
      wantPause: "not-paused", wantText: `${sessionChanged} The simulation was not paused.`,
    },
    {
      name: "the server restarted before the apply with another project revision", serverStart: "start-0", liveRevision: 4, wantCommands: [], wantPaused: false,
      wantPause: "not-paused", wantText: `${sessionChanged} The simulation was not paused.`,
    },
    {
      name: "the server restarted after the pause with the same epoch", serverStart: "start-1",
      before: (command, live) => { if (command.action === "project") Object.assign(live, { serverStart: "start-2", generation: live.generation + 1 }); return false; },
      wantCommands: ["pause true", "project"], want: "start-1", wantPaused: true,
      wantPause: "restarted", wantText: `${sessionChanged} The simulation restarted after the pause. The editor did not resume it.`,
    },
  ];
  for (const item of cases) {
    const server = fakeSession({ paused: false, before: item.before, liveRevision: item.liveRevision });
    const connection = { fetch: server.fetch, clientID: "editor-test", sequence: 0, epoch: "" };
    let applied = null;
    let error = null;
    try {
      applied = await editor.applyToServer({ connection, revision: 3, serverStart: item.serverStart, project: connectedScenario() });
    } catch (caught) { error = caught; }

    if (item.wantPause) {
      assert.ok(error, item.name);
      assert.equal(error.errorCode, "session_changed", item.name);
      assert.equal(error.pause, item.wantPause, item.name);
      assert.equal(editor.applyFailureText(error), item.wantText, item.name);
      assert.equal(server.live.revision, item.liveRevision ?? 3, item.name);
    } else {
      assert.equal(error, null, item.name);
      assert.equal(applied.revision, item.wantRevision, item.name);
      assert.equal(applied.serverStart, "start-1", item.name);
    }
    assert.deepEqual(server.commands.map((command) => (command.action === "pause" ? `pause ${command.paused}` : command.action)), item.wantCommands, item.name);
    const project = server.commands.find((command) => command.action === "project");
    if (project) assert.equal("serverStart" in project ? project.serverStart : null, item.want, item.name);
    if ("wantPaused" in item) assert.equal(server.live.paused, item.wantPaused, item.name);
  }
});

// asServed is a live normalizer that gives the project as the server sends
// it. The page passes the Go normalizer, and Go tests normalization.
const asServed = async (project) => project;

// snapshotServer gives a fake server for the GET requests of readSnapshot.
// Before each request, change gets the URL, the number of the request from
// 1, and live, so it can restart the server between two reads. urls
// records each request.
function snapshotServer(change = () => {}) {
  const live = { epoch: "epoch-1", serverStart: "start-1", revision: 3, name: "Old" };
  const urls = [];
  const reply = (body) => ({ ok: true, status: 200, json: async () => body });
  const fetch = async (url) => {
    urls.push(url); change(url, urls.length, live);
    if (url === "/api/project") return reply({ revision: live.revision, project: { ...connectedScenario(), name: live.name } });
    return stateReply({ topology: {}, frame: { state: { epoch: live.epoch, serverStart: live.serverStart, projectRevision: live.revision, generation: 5, simulation: { paused: true } }, routes: [] } });
  };
  return { live, urls, connection: { fetch, clientID: "editor-test", sequence: 0, epoch: "" } };
}

// restoreOn gives a change function for snapshotServer. Before request
// number, the server restarts and restores a different project with the
// same epoch and project revision.
const restoreOn = (number) => (_, count, live) => { if (count === number) Object.assign(live, { serverStart: "start-2", name: "Restored" }); };

test("readSnapshot gives the project and the server start ID of one server state", async () => {
  const cases = [
    { name: "a stable server", want: { name: "Old", serverStart: "start-1", revision: 3 }, wantReads: 3 },
    { name: "a restart between the project read and the second state read", change: restoreOn(3), want: { name: "Restored", serverStart: "start-2", revision: 3 }, wantReads: 6 },
    { name: "a restart between the first state read and the project read", change: restoreOn(2), want: { name: "Restored", serverStart: "start-2", revision: 3 }, wantReads: 6 },
    {
      name: "a project apply between the project read and the second state read",
      change: (_, count, live) => { if (count === 3) Object.assign(live, { revision: 4, name: "Applied" }); }, want: { name: "Applied", serverStart: "start-1", revision: 4 }, wantReads: 6,
    },
    {
      name: "a server that restarts before each state read", change: (url, count, live) => { if (url === "/api/state") live.serverStart = `start-${count}`; },
      wantError: "The live scenario changed during each read.", wantReads: 3 * editor.SNAPSHOT_ATTEMPTS,
    },
  ];
  for (const item of cases) {
    const server = snapshotServer(item.change);
    const got = await editor.readSnapshot(server.connection).then((snapshot) => snapshot, (error) => error);
    assert.equal(server.urls.length, item.wantReads, `${item.name}: reads`);
    assert.deepEqual(server.urls.slice(0, 3), ["/api/state", "/api/project", "/api/state"], item.name);
    if (item.wantError) {
      assert.ok(got instanceof Error, item.name);
      assert.equal(got.message, item.wantError, item.name);
      assert.equal(editor.applyFailureText({ message: got.message, pause: "not-paused" }), `Apply failed. ${item.wantError} The simulation was not paused.`, item.name);
      continue;
    }
    const { project, state } = got;
    assert.deepEqual({ name: project.project.name, serverStart: state.serverStart, revision: state.projectRevision }, item.want, item.name);
    assert.equal(project.revision, state.projectRevision, item.name);
  }
});

test("snapshotConsistent accepts only reads of one server state", () => {
  const state = { epoch: "epoch-1", serverStart: "start-1", projectRevision: 3 };
  const project = { revision: 3, project: {} };
  const cases = [
    { name: "the same state", before: state, project, want: true },
    { name: "another server start ID", before: { ...state, serverStart: "start-0" }, project, want: false },
    { name: "another epoch", before: { ...state, epoch: "epoch-0" }, project, want: false },
    { name: "another project revision", before: state, project: { ...project, revision: 2 }, want: false },
    { name: "a project reply without a revision", before: state, project: { project: {} }, want: false },
  ];
  for (const item of cases) assert.equal(editor.snapshotConsistent(item.before, item.project, item.after ?? state), item.want, item.name);
});

test("live reads and conflict actions await their supplied normalizer", async () => {
  const cases = [
    (connection, normalize) => editor.readLive(connection, normalize),
    (connection, normalize) => editor.loadLive({ connection, normalize, changed: false }),
    (connection, normalize) => editor.readConflict(connection, { errorCode: "stale_project" }, normalize),
    (connection, normalize) => editor.applyOverBase({ connection, normalize, revision: 3, serverStart: "start-1", confirm: () => true }),
  ];
  for (const read of cases) {
    const server = snapshotServer();
    let release, called = 0, settled = false;
    const normalization = new Promise((resolve) => { release = resolve; });
    const result = read(server.connection, async (project) => {
      called++;
      assert.equal(project.name, "Old");
      await normalization;
      return { ...project, name: "Normalized" };
    }).then((value) => { settled = true; return value; });
    while (!called) await new Promise((resolve) => setImmediate(resolve));
    assert.equal(settled, false);
    release();
    const value = await result;
    assert.equal(called, 1);
    if (value.project) assert.equal(value.project.name, "Normalized");
  }
});

test("readLive and the conflict actions never pair the old project with the new server start ID", async () => {
  for (const number of [2, 3]) {
    const name = `a restart before read ${number}`;
    const reader = snapshotServer(restoreOn(number));
    const live = await editor.readLive(reader.connection, asServed);
    assert.deepEqual({ name: live.project.name, revision: live.revision, epoch: live.epoch, serverStart: live.serverStart }, { name: "Restored", revision: 3, epoch: "epoch-1", serverStart: "start-2" }, name);

    const loader = snapshotServer(restoreOn(number));
    const loaded = await editor.loadLive({ normalize: asServed, connection: loader.connection, changed: false, confirm: () => false });
    const page = editor.liveDraft(loaded, { loaded: null, background: null });
    assert.deepEqual([page.value.scenario.name, page.draftBase.serverStart, loader.connection.epoch], ["Restored", "start-2", "epoch-1"], name);

    const failure = Object.assign(new Error("The server session changed."), { errorCode: "session_changed" });
    assert.deepEqual(await editor.readConflict(snapshotServer(restoreOn(number)).connection, failure, asServed), { code: "session_changed", revision: 3, serverStart: "start-2" }, name);
  }
  const changing = snapshotServer((url, count, live) => { if (url === "/api/state") live.serverStart = `start-${count}`; });
  const failure = Object.assign(new Error("The live scenario changed."), { errorCode: "stale_project" });
  assert.deepEqual(await editor.readConflict(changing.connection, failure, asServed), { code: "stale_project", revision: null, serverStart: "" }, "a server that keeps changing");
});

// conflictSession gives a fakeSession with a connection of epoch "epoch-1".
// reads counts the GET requests, and down makes each GET request fail.
function conflictSession(options) {
  const server = fakeSession({ paused: false, ...options });
  const session = { server, reads: 0, down: false };
  session.connection = {
    clientID: "editor-test", sequence: 0, epoch: "epoch-1",
    fetch: async (url, init) => {
      if (!init || !init.method) { session.reads += 1; if (session.down) throw new TypeError("Failed to fetch"); }
      return server.fetch(url, init);
    },
  };
  return session;
}

// commandList gives the actions of the commands that the editor sent.
const commandList = (server) => server.commands.map((command) => (command.action === "pause" ? `pause ${command.paused}` : command.action));

// restartServer changes the fake server as a restart that restores revision 4.
const restartServer = (live) => Object.assign(live, { epoch: "epoch-2", serverStart: "start-2", revision: 4 });

test("a stale or restarted apply shows the conflict actions with the live revision", async () => {
  // want gives the conflict, or null for no conflict actions.
  const cases = [
    { name: "the editor finds a stale draft before the pause", liveRevision: 5, want: { code: "stale_project", revision: 5, serverStart: "start-1" } },
    {
      name: "the server finds a stale draft", before: (command, live) => { if (command.action === "project") live.revision = 4; return false; },
      want: { code: "stale_project", revision: 4, serverStart: "start-1" },
    },
    { name: "the server restarted", serverStart: "start-1", restart: restartServer, want: { code: "session_changed", revision: 4, serverStart: "start-2" } },
    {
      name: "the server restarted with the same revision, and the draft has no server start ID", serverStart: "",
      restart: (live) => Object.assign(live, { epoch: "epoch-2", serverStart: "start-2" }), want: { code: "session_changed", revision: 3, serverStart: "start-2" },
    },
    { name: "the live state does not load after the conflict", liveRevision: 5, down: true, want: { code: "stale_project", revision: null, serverStart: "" } },
    { name: "the server cannot save the project file", before: failOn("project", 409), want: null },
  ];
  for (const item of cases) {
    const session = conflictSession({ before: item.before, liveRevision: item.liveRevision });
    if (item.restart) item.restart(session.server.live);
    const error = await editor.applyToServer({ connection: session.connection, revision: 3, serverStart: item.serverStart ?? "start-1", project: connectedScenario() }).then(() => null, (caught) => caught);
    assert.ok(error, item.name);
    session.down = Boolean(item.down); const reads = session.reads;
    const conflict = await editor.readConflict(session.connection, error, asServed);
    assert.deepEqual(conflict, item.want, item.name);
    assert.equal(session.reads - reads, item.want ? (item.down ? 1 : 3) : 0, `${item.name}: reads`);
    assert.equal(session.connection.epoch, "epoch-1", `${item.name}: the page keeps its epoch until an action`);
  }

  const stale = editor.conflictView({ code: "stale_project", revision: 5 });
  assert.deepEqual(stale, {
    text: "The live scenario changed to revision 5. Your draft is not applied.",
    hint: "Load live scenario replaces the changes in your draft. Apply over revision 5 replaces the live scenario with your draft.",
    applyLabel: "Apply over revision 5",
  });
  const restarted = editor.conflictView({ code: "session_changed", revision: 4 });
  assert.equal(restarted.text, "The server session changed. The live scenario is now revision 4. Your draft is not applied.");
  assert.equal(restarted.applyLabel, "Apply over revision 4");
});

test("Apply over sends the project command with the live revision, epoch and server start ID", async () => {
  const cases = [
    { name: "the user agrees", answer: true, wantBase: { revision: 4, epoch: "epoch-2", serverStart: "start-2" }, wantCommands: ["pause true", "project"] },
    { name: "the user cancels", answer: false, wantBase: null, wantCommands: [] },
    {
      name: "the live revision changed again", answer: true, change: (live) => { live.revision = 5; },
      wantBase: { revision: 4, epoch: "epoch-2", serverStart: "start-2" }, wantCommands: [], wantCode: "stale_project",
      wantConflict: { code: "stale_project", revision: 5, serverStart: "start-2" },
    },
    {
      name: "the server restarted again with the same revision", answer: true, change: (live) => Object.assign(live, { epoch: "epoch-3", serverStart: "start-3" }),
      wantBase: { revision: 4, epoch: "epoch-3", serverStart: "start-2" }, wantCommands: [], wantCode: "session_changed",
      wantConflict: { code: "session_changed", revision: 4, serverStart: "start-3" },
    },
  ];
  for (const item of cases) {
    const session = conflictSession({}); const { connection, server } = session;
    restartServer(server.live);
    const failure = await editor.applyToServer({ connection, revision: 3, serverStart: "start-1", project: connectedScenario() }).then(() => null, (caught) => caught);
    const conflict = await editor.readConflict(connection, failure, asServed);
    assert.deepEqual(conflict, { code: "session_changed", revision: 4, serverStart: "start-2" }, item.name);
    if (item.change) item.change(server.live);

    const questions = []; const reads = session.reads;
    const base = await editor.applyOverBase({ normalize: asServed, connection, revision: conflict.revision, serverStart: conflict.serverStart, confirm: (text) => { questions.push(text); return item.answer; } });
    assert.deepEqual(questions, [editor.applyOverQuestion(4)], item.name);
    assert.match(questions[0], /revision 4\?.*replaces the live scenario/, item.name);
    assert.deepEqual(base, item.wantBase, item.name);
    assert.equal(session.reads - reads, base ? 3 : 0, `${item.name}: reads`);
    assert.equal(connection.epoch, base ? base.epoch : "epoch-1", item.name);
    if (!base) { assert.deepEqual(commandList(server), [], item.name); continue; }

    const project = { ...connectedScenario(), name: "Draft" };
    const error = await editor.applyToServer({ connection, revision: base.revision, serverStart: base.serverStart, project }).then(() => null, (caught) => caught);
    assert.equal(error?.errorCode, item.wantCode, item.name);
    assert.deepEqual(commandList(server), item.wantCommands, item.name);
    for (const command of server.commands) assert.equal(command.epoch, base.epoch, item.name);
    const sent = server.commands.find((command) => command.action === "project");
    if (sent) assert.deepEqual([sent.projectRevision, sent.serverStart, sent.project.name], [4, "start-2", "Draft"], item.name);
    if (item.wantConflict) assert.deepEqual(await editor.readConflict(connection, error, asServed), item.wantConflict, item.name);
  }
});

test("Load live scenario replaces the base and clears the unsaved changes after the user agrees", async () => {
  const baseline = { scenario: JSON.stringify(connectedScenario()), revision: 3 };
  const oldBase = { revision: 3, epoch: "epoch-1", serverStart: "start-1" };
  const changed = { scenario: { ...connectedScenario(), name: "Changed" }, background: null };
  const cases = [
    { name: "the user agrees", draft: changed, answer: true, wantQuestions: [editor.LOAD_LIVE_QUESTION], wantLoaded: true },
    { name: "the user cancels", draft: changed, answer: false, wantQuestions: [editor.LOAD_LIVE_QUESTION], wantLoaded: false },
    { name: "a draft with no changes", draft: { scenario: connectedScenario(), background: null }, answer: false, wantQuestions: [], wantLoaded: true },
  ];
  const liveProject = { ...connectedScenario(), name: "Live" };
  const background = { dataURL: "data:image/png;base64,AAAA", x: 0, y: 0, width: 400, height: 200, opacity: 0.45 };
  for (const item of cases) {
    const session = conflictSession({ project: liveProject }); restartServer(session.server.live);
    const questions = [];
    const live = await editor.loadLive({
      normalize: asServed, connection: session.connection, changed: editor.draftChanged(item.draft, baseline),
      confirm: (text) => { questions.push(text); return item.answer; },
    });
    assert.deepEqual(questions, item.wantQuestions, item.name);
    if (!item.wantLoaded) {
      assert.equal(live, null, item.name);
      assert.deepEqual([session.reads, session.connection.epoch], [0, "epoch-1"], item.name);
      assert.notEqual(editor.draftRecordFor(item.draft, baseline, oldBase), null, `${item.name}: the draft keeps its changes`);
      continue;
    }
    assert.equal(session.connection.epoch, "epoch-2", item.name);
    const page = editor.liveDraft(live, { loaded: { scenario: connectedScenario(), background }, background });
    assert.deepEqual([page.loadedRevision, page.loadedStart], [4, "start-2"], item.name);
    assert.deepEqual(page.draftBase, { revision: 4, epoch: "epoch-2", serverStart: "start-2" }, item.name);
    assert.deepEqual([page.value.scenario.name, page.loaded.scenario.name], ["Live", "Live"], `${item.name}: the draft is the live project`);
    assert.deepEqual([page.value.background, page.loaded.background], [background, background], `${item.name}: the background stays`);
    assert.notEqual(page.value.scenario, page.loaded.scenario, `${item.name}: the draft and the base are copies`);
    const liveBaseline = { scenario: JSON.stringify(live.project), revision: live.revision };
    assert.equal(editor.draftRecordFor(page.value, liveBaseline, page.draftBase), null, `${item.name}: no unsaved changes`);
    assert.equal(editor.draftChanged(page.value, liveBaseline), false, item.name);
  }
});

test("after session_changed, the next command uses the new epoch", async () => {
  // The draft has no server start ID, so the server rejects the pause
  // command of the old epoch.
  const session = conflictSession({}); const { connection, server } = session;
  restartServer(server.live);
  const apply = () => editor.applyToServer({ connection, revision: 4, serverStart: "", project: connectedScenario() }).then(() => null, (caught) => caught);
  const first = await apply();
  assert.deepEqual([first.errorCode, first.pause], ["session_changed", "not-paused"]);
  assert.deepEqual((await editor.readConflict(connection, first, asServed)), { code: "session_changed", revision: 4, serverStart: "start-2" });
  const again = await apply();
  assert.equal(again.errorCode, "session_changed", "the page keeps the old epoch without an action");

  const live = await editor.loadLive({ normalize: asServed, connection, changed: false, confirm: () => false });
  assert.equal(live.epoch, "epoch-2");
  server.commands.length = 0;
  assert.equal(await apply(), null);
  assert.deepEqual(commandList(server), ["pause true", "project"]);
  for (const command of server.commands) assert.equal(command.epoch, "epoch-2");
});

// DRAFT_KEY is the record key of the keeper tests, a server origin.
const DRAFT_KEY = "http://podsim.test";

// TEST_KEY is the image key of TEST_BACKGROUND.
const TEST_KEY = "0123456789abcdef0123456789abcdef";

// TEST_BACKGROUND is a page background with an image key, a calibration
// and an opacity.
const TEST_BACKGROUND = { dataURL: dataURL(pngBytes(4, 2)), imageKey: TEST_KEY, x: -10, y: 5, width: 400, height: 200, opacity: 0.45 };

// recordOf gives the record that the background keeper saves for a page
// background with no frame and no license.
function recordOf(background) {
  return editor.backgroundRecordFor(background, { bytes: editor.dataURLToBytes(background.dataURL), frame: null, license: null });
}

// pageOf gives the page background of a restored value, as
// storedBackground gives it, as the page installs it.
function pageOf(restored) {
  const { imageKey, x, y, width, height, opacity } = restored.background;
  return { dataURL: editor.bytesToDataURL(restored.image.bytes, restored.image.mime), imageKey, x, y, width, height, opacity };
}

// backgroundKeeper gives a background keeper as the page makes it, with
// the snapshot of page.background. options adds hooks.
function backgroundKeeper(store, page, options = {}) {
  return editor.createDraftKeeper({
    store, key: DRAFT_KEY, delay: editor.DRAFT_SAVE_DELAY, clock: globalThis, deletes: true, holdUntilChange: true,
    snapshot: () => (page.background ? recordOf(page.background) : null), text: editor.backgroundRecordText, ...options,
  });
}

// savedRecord gives a draft record, as the editor saves it, with a scenario
// name that the tests can check.
function savedRecord(name, revision = 3) {
  return { scenario: { ...connectedScenario(), name }, revision, epoch: "epoch-1", serverStart: "start-1" };
}

// fakeDraftStore gives a draft store for createDraftKeeper that keeps the
// records in a Map. calls lists the get, put and delete calls. fail gives
// the error for a call name, or null.
function fakeDraftStore(fail) {
  const records = new Map(); const calls = [];
  const call = (name, action) => {
    calls.push(name); const error = fail ? fail(name) : null;
    return error ? Promise.reject(error) : Promise.resolve().then(action);
  };
  return {
    records, calls,
    get: (key) => call("get", () => structuredClone(records.get(key))),
    put: (key, record) => call("put", () => { records.set(key, structuredClone(record)); }),
    delete: (key) => call("delete", () => { records.delete(key); }),
  };
}

// draftKeeper gives a keeper with a mock clock. draft.record is the record
// that the snapshot gives. statuses lists the status changes.
function draftKeeper(t, store) {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  const draft = { record: null }; const statuses = [];
  const keeper = editor.createDraftKeeper({
    store, key: DRAFT_KEY, delay: editor.DRAFT_SAVE_DELAY, clock: globalThis,
    snapshot: () => (draft.record ? structuredClone(draft.record) : null), onStatus: (status) => statuses.push(status),
  });
  return { keeper, draft, statuses };
}

// runKeeperSteps runs the steps of a keeper test. Each step can set the
// draft record, make one call to the keeper, and wait wait milliseconds.
// Then the queued writes end. calls gives the store calls of the step,
// saved gives the name of the saved draft or null, and unsaved and status
// give the keeper values after the step.
async function runKeeperSteps(t, test, steps) {
  const { store, keeper, draft } = test;
  for (const step of steps) {
    const name = `${test.name}: ${step.name}`; const before = store ? store.calls.length : 0;
    if ("record" in step) draft.record = step.record;
    if (step.call) await keeper[step.call]();
    if (step.wait) t.mock.timers.tick(step.wait);
    await new Promise((resolve) => setImmediate(resolve));
    if (store) assert.deepEqual(store.calls.slice(before), step.calls ?? [], name);
    if (store && "saved" in step) assert.equal(store.records.get(DRAFT_KEY)?.scenario.name ?? null, step.saved, name);
    if ("unsaved" in step) assert.equal(keeper.unsaved, step.unsaved, name);
    if ("status" in step) assert.equal(keeper.status, step.status, name);
  }
}

test("the keeper saves the draft after the last change and deletes a draft with no changes", async (t) => {
  const delay = editor.DRAFT_SAVE_DELAY;
  const store = fakeDraftStore(); const { keeper, draft, statuses } = draftKeeper(t, store);
  await runKeeperSteps(t, { name: "keeper", store, keeper, draft }, [
    { name: "load with no saved draft", call: "load", calls: ["get"], saved: null, unsaved: false },
    { name: "a change before arm", record: savedRecord("A"), call: "schedule", wait: delay, saved: null, unsaved: true },
    { name: "arm saves the change", call: "arm", calls: ["put"], saved: "A", unsaved: false },
    { name: "a change waits for the delay", record: savedRecord("B"), call: "schedule", wait: delay - 1, saved: "A", unsaved: true },
    { name: "the delay ends", wait: 1, calls: ["put"], saved: "B", unsaved: false },
    { name: "a change", record: savedRecord("C"), call: "schedule", wait: delay - 1, saved: "B", unsaved: true },
    { name: "a second change starts the wait again", record: savedRecord("D"), call: "schedule", wait: delay - 1, saved: "B", unsaved: true },
    { name: "one save gives the last change", wait: 1, calls: ["put"], saved: "D", unsaved: false },
    { name: "a draft with no changes deletes the record", record: null, call: "schedule", wait: delay, calls: ["delete"], saved: null, unsaved: false },
    { name: "no change gives no write", call: "schedule", wait: delay, saved: null, unsaved: false },
    { name: "flush saves now", record: savedRecord("E"), call: "flush", calls: ["put"], saved: "E", unsaved: false },
    { name: "the same draft gives no write", call: "flush", saved: "E", unsaved: false },
    { name: "a new epoch is a change", record: { ...savedRecord("E"), epoch: "epoch-2" }, call: "flush", calls: ["put"], saved: "E", unsaved: false, status: "ok" },
  ]);
  assert.deepEqual(statuses, []);
});

test("the keeper keeps the saved draft until a restore or a discard, and clears it after an apply", async (t) => {
  const delay = editor.DRAFT_SAVE_DELAY;
  // Each case starts with the saved draft "Saved" and loads it. Until arm,
  // an edit does not replace the saved draft.
  const hold = [
    { name: "load gives the saved draft", call: "load", calls: ["get"], saved: "Saved", unsaved: false },
    { name: "a flush before a choice, as at a reload", record: null, call: "flush", saved: "Saved", unsaved: false },
    { name: "an edit before a choice", record: savedRecord("Edit"), call: "schedule", wait: delay, saved: "Saved", unsaved: true },
  ];
  const cases = [
    {
      name: "restore",
      steps: [...hold, { name: "restore saves the restored draft", record: { ...savedRecord("Saved", 5), epoch: "epoch-2" }, call: "arm", calls: ["put"], saved: "Saved", unsaved: false }],
    },
    {
      name: "discard with no edits",
      steps: [
        { name: "load", call: "load", calls: ["get"], saved: "Saved", unsaved: false },
        { name: "discard deletes the saved draft", call: "replace", calls: ["delete"], saved: null, unsaved: false },
        { name: "arm with no changes", call: "arm", saved: null, unsaved: false },
      ],
    },
    {
      name: "discard after an edit",
      steps: [
        ...hold,
        { name: "discard replaces the saved draft with the edit in one put", call: "replace", calls: ["put"], saved: "Edit", unsaved: false },
        { name: "arm after the discard", call: "arm", saved: "Edit", unsaved: false },
      ],
    },
    {
      name: "apply",
      steps: [
        { name: "load", call: "load", calls: ["get"], saved: "Saved" },
        { name: "restore", call: "arm", record: savedRecord("Saved"), saved: "Saved", unsaved: false },
        { name: "an edit", record: savedRecord("Edit"), call: "schedule", wait: delay, calls: ["put"], saved: "Edit" },
        { name: "an apply deletes the saved draft", record: null, call: "replace", calls: ["delete"], saved: null, unsaved: false },
        { name: "arm after the apply", call: "arm", saved: null, unsaved: false },
        { name: "an edit after the apply", record: savedRecord("Next"), call: "schedule", wait: delay, calls: ["put"], saved: "Next", unsaved: false },
      ],
    },
    {
      name: "an apply with a pending save",
      steps: [
        { name: "load", call: "load", calls: ["get"], saved: "Saved" },
        { name: "arm with the saved draft", record: savedRecord("Saved"), call: "arm", saved: "Saved" },
        { name: "an edit waits for the delay", record: savedRecord("Edit"), call: "schedule", wait: delay - 1, saved: "Saved", unsaved: true },
        { name: "the apply cancels the wait and deletes the saved draft", record: null, call: "replace", wait: delay, calls: ["delete"], saved: null, unsaved: false },
      ],
    },
  ];
  for (const item of cases) {
    await t.test(item.name, async (t) => {
      const store = fakeDraftStore(); store.records.set(DRAFT_KEY, savedRecord("Saved"));
      const { keeper, draft } = draftKeeper(t, store);
      await runKeeperSteps(t, { name: item.name, store, keeper, draft }, item.steps);
    });
  }
});

test("a failed draft save gives a status and does not throw", async (t) => {
  const quota = new DOMException("The quota is used.", "QuotaExceededError");
  const broken = new Error("The disk failed.");
  const cases = [
    {
      name: "no draft store", store: null, wantStatuses: [],
      steps: [
        { name: "load", call: "load", status: "off", unsaved: false },
        { name: "arm with a change", record: savedRecord("A"), call: "arm", wait: 1000, status: "off", unsaved: true },
        { name: "replace", call: "replace", status: "off", unsaved: true },
        { name: "no changes", record: null, call: "flush", status: "off", unsaved: false },
      ],
    },
    {
      name: "a failed load", fail: (call) => (call === "get" ? broken : null), wantStatuses: ["off"],
      steps: [
        { name: "load", call: "load", calls: ["get"], status: "off", unsaved: false },
        { name: "no write after the failed load", record: savedRecord("A"), call: "arm", wait: 1000, status: "off", unsaved: true },
      ],
    },
    {
      name: "a full storage", fail: (call) => (call === "put" ? quota : null), wantStatuses: ["full", "ok"],
      steps: [
        { name: "load", call: "load", calls: ["get"], status: "ok" },
        { name: "a save that the quota stops", record: savedRecord("A"), call: "arm", calls: ["put"], saved: null, status: "full", unsaved: true },
        { name: "the next flush tries again", call: "flush", calls: ["put"], saved: null, status: "full", unsaved: true },
        { name: "a delete still works", record: null, call: "flush", calls: ["delete"], status: "ok", unsaved: false },
      ],
    },
    {
      name: "a failed write", fail: (call) => (call === "delete" ? broken : null), wantStatuses: ["failed", "ok"],
      steps: [
        { name: "load", call: "load", calls: ["get"], status: "ok" },
        { name: "a save", record: savedRecord("A"), call: "arm", calls: ["put"], saved: "A", status: "ok", unsaved: false },
        { name: "a failed delete", record: null, call: "replace", calls: ["delete"], saved: "A", status: "failed", unsaved: false },
        { name: "a new change saves again", record: savedRecord("B"), call: "flush", calls: ["put"], saved: "B", status: "ok", unsaved: false },
      ],
    },
  ];
  for (const item of cases) {
    await t.test(item.name, async (t) => {
      const store = item.store === null ? null : fakeDraftStore(item.fail);
      const { keeper, draft, statuses } = draftKeeper(t, store);
      await runKeeperSteps(t, { name: item.name, store, keeper, draft }, item.steps);
      assert.deepEqual(statuses, item.wantStatuses, item.name);
    });
  }
});

// fakeIndexedDB gives an indexedDB object with the calls that
// openRecordStore uses. Each request and each transaction ends in a later
// task, as in a browser. Each database name has its own object stores.
// options.openError fails the open, and options.commitError aborts each
// readwrite transaction.
function fakeIndexedDB(options = {}) {
  const databases = new Map(); const log = [];
  const later = (callback) => setImmediate(callback);
  const makeDatabase = (stores) => ({
    createObjectStore(name) { log.push(`create ${name}`); stores.set(name, new Map()); },
    transaction(name, mode) {
      const data = stores.get(name); const transaction = { error: null };
      const request = (action) => {
        const item = {};
        later(() => {
          item.result = action(); if (item.onsuccess) item.onsuccess();
          later(() => {
            if (mode === "readwrite" && options.commitError) { transaction.error = options.commitError; transaction.onabort(); } else transaction.oncomplete();
          });
        });
        return item;
      };
      transaction.objectStore = () => ({
        get: (key) => request(() => structuredClone(data.get(key))),
        put: (value, key) => request(() => { if (!options.commitError) data.set(key, structuredClone(value)); return key; }),
        delete: (key) => request(() => { if (!options.commitError) data.delete(key); }),
      });
      return transaction;
    },
  });
  return {
    log, databases,
    open(name, version) {
      log.push(`open ${name} ${version}`); const request = {};
      later(() => {
        if (options.openError) { request.error = options.openError; request.onerror(); return; }
        const upgrade = !databases.has(name);
        if (upgrade) databases.set(name, new Map());
        request.result = makeDatabase(databases.get(name));
        if (upgrade) request.onupgradeneeded();
        request.onsuccess();
      });
      return request;
    },
  };
}

test("the IndexedDB record stores save, read and delete one record, and reject a failed commit", async (t) => {
  assert.equal(editor.openRecordStore(null, editor.DRAFT_STORE), null);
  assert.equal(editor.openRecordStore(undefined, editor.BACKGROUND_STORE), null);

  await t.test("a record round trips", async () => {
    const factory = fakeIndexedDB(); const store = editor.openRecordStore(factory, editor.DRAFT_STORE);
    assert.equal(await store.get(DRAFT_KEY), undefined);
    await store.put(DRAFT_KEY, savedRecord("A"));
    assert.deepEqual(await store.get(DRAFT_KEY), savedRecord("A"));
    await store.delete(DRAFT_KEY);
    assert.equal(await store.get(DRAFT_KEY), undefined);
    assert.deepEqual(factory.log, ["open podsim-editor 1", "create drafts"]);
  });

  await t.test("the draft and the background have their own database at version 1", async () => {
    const factory = fakeIndexedDB();
    const drafts = editor.openRecordStore(factory, editor.DRAFT_STORE); const backgrounds = editor.openRecordStore(factory, editor.BACKGROUND_STORE);
    const record = recordOf(TEST_BACKGROUND);
    await drafts.put(DRAFT_KEY, savedRecord("A")); await backgrounds.put(DRAFT_KEY, record);
    await drafts.delete(DRAFT_KEY);
    assert.deepEqual([await drafts.get(DRAFT_KEY), await backgrounds.get(DRAFT_KEY)], [undefined, record]);
    assert.deepEqual(factory.log, ["open podsim-editor 1", "create drafts", "open podsim-editor-backgrounds-2 1", "create backgrounds"]);
  });

  const quota = new DOMException("The quota is used.", "QuotaExceededError");
  await t.test("a quota error at the commit rejects the write", async () => {
    const factory = fakeIndexedDB({ commitError: quota }); const store = editor.openRecordStore(factory, editor.DRAFT_STORE);
    await assert.rejects(store.put(DRAFT_KEY, savedRecord("A")), (error) => error === quota);
    assert.equal(await store.get(DRAFT_KEY), undefined);
  });

  await t.test("a failed open rejects each call", async () => {
    const failure = new DOMException("The database is closed.", "UnknownError");
    const store = editor.openRecordStore(fakeIndexedDB({ openError: failure }), editor.DRAFT_STORE);
    for (const call of [() => store.get(DRAFT_KEY), () => store.put(DRAFT_KEY, savedRecord("A")), () => store.delete(DRAFT_KEY)]) {
      await assert.rejects(call(), (error) => error === failure);
    }
  });

  await t.test("the keeper reports the full storage of IndexedDB", async () => {
    const statuses = [];
    const keeper = editor.createDraftKeeper({
      store: editor.openRecordStore(fakeIndexedDB({ commitError: quota }), editor.DRAFT_STORE), key: DRAFT_KEY, delay: editor.DRAFT_SAVE_DELAY, clock: globalThis,
      snapshot: () => savedRecord("A"), onStatus: (status) => statuses.push(status),
    });
    assert.equal(await keeper.load(), null);
    await keeper.arm();
    assert.deepEqual([keeper.status, keeper.unsaved, statuses], ["full", true, ["full"]]);
  });
});

test("Pause and apply and the saved draft need a scenario change, and a background change is not a draft change", () => {
  const scenario = connectedScenario();
  const live = { scenario: JSON.stringify(scenario), revision: 3 };
  const base = { revision: 3, epoch: "epoch-1", serverStart: "start-1" };
  const cases = [
    { name: "the live scenario", draft: { scenario: connectedScenario(), background: null }, want: false },
    { name: "a changed scenario", draft: { scenario: { ...scenario, name: "Changed" }, background: null }, want: true },
    { name: "a new background", draft: { scenario, background: TEST_BACKGROUND }, want: false },
    { name: "a changed scenario and a background", draft: { scenario: { ...scenario, name: "Changed" }, background: TEST_BACKGROUND }, want: true },
  ];
  for (const item of cases) {
    assert.equal(editor.draftChanged(item.draft, live), item.want, item.name);
    const record = editor.draftRecordFor(item.draft, live, base);
    assert.deepEqual(record && Object.keys(record), item.want ? ["scenario", "revision", "epoch", "serverStart"] : null, `${item.name}: the saved draft has no background`);
  }
});

test("the background record holds the image key, the placement and the image bytes, and a restore checks it", () => {
  assert.equal(editor.backgroundRecordFor(null, null), null, "no background deletes the record");
  const bytes = editor.dataURLToBytes(TEST_BACKGROUND.dataURL);
  const record = editor.backgroundRecordFor({ ...TEST_BACKGROUND, note: "extra" }, { bytes, frame: null, license: null });
  const { dataURL: _, ...placement } = TEST_BACKGROUND;
  assert.deepEqual(record, { background: { ...placement, frameState: "none", image: { bytes, frame: null, license: null } } });
  const restored = editor.storedBackground(structuredClone(record));
  assert.deepEqual(restored, { background: { ...placement, frameState: "none" }, image: { bytes, mime: "image/png", pixelWidth: 4, pixelHeight: 2, frame: null, license: null } });
  assert.deepEqual(pageOf(restored), TEST_BACKGROUND, "the page gets the same background from the stored bytes");
  assert.equal(editor.storedBackground(undefined), null, "no record");
  assert.equal(editor.storedBackground(null), null, "no record");
  const frame = { south: 51.4, north: 51.6, west: -0.2, east: 0, source: "web-mercator" };
  const license = { source: "Council", attribution: "Map by the council", license: "CC-BY-4.0", licenseURL: "https://creativecommons.org/licenses/by/4.0/", copyrightURL: "", retrieved: "2026-09-28T12:00:00Z", method: "user supplied", notice: "" };
  const framed = editor.backgroundRecordFor({ ...TEST_BACKGROUND, frameState: "detached" }, { bytes, frame, license });
  assert.deepEqual(editor.storedBackground(structuredClone(framed)).image, { bytes, mime: "image/png", pixelWidth: 4, pixelHeight: 2, frame, license });
  // change gives a copy of record with a change of its background.
  const change = (edit) => { const copy = structuredClone(record); edit(copy.background); return copy; };
  const bad = [
    { name: "a record that is not an object", record: "background", want: /not a record/ },
    { name: "a record without a background", record: {}, want: /must be an object/ },
    { name: "an image key in upper case", record: change((item) => { item.imageKey = TEST_KEY.toUpperCase(); }), want: /no valid image key/ },
    { name: "a short image key", record: change((item) => { item.imageKey = "abc"; }), want: /no valid image key/ },
    { name: "a position that is not finite", record: change((item) => { item.x = null; }), want: /x value is invalid/ },
    { name: "a zero width", record: change((item) => { item.width = 0; }), want: /dimensions or opacity/ },
    { name: "an opacity above 1", record: change((item) => { item.opacity = 2; }), want: /dimensions or opacity/ },
    { name: "no image", record: change((item) => { delete item.image; }), want: /has no image/ },
    { name: "a data URL in place of the bytes", record: change((item) => { item.image.bytes = TEST_BACKGROUND.dataURL; }), want: /not bytes/ },
    { name: "bytes that are not an image", record: change((item) => { item.image.bytes = new ArrayBuffer(16); }), want: /not a valid PNG or JPEG/ },
    { name: "an unknown frame state", record: change((item) => { item.frameState = "loose"; }), want: /frame state must be/ },
    { name: "none with a frame", record: change((item) => { item.image.frame = frame; }), want: /"none" cannot have a frame/ },
    { name: "attached with no frame", record: change((item) => { item.frameState = "attached"; }), want: /"attached" needs a frame/ },
    { name: "a frame that is not valid", record: change((item) => { item.frameState = "detached"; item.image.frame = { ...frame, north: 81 }; }), want: /latitudes must be from -80 to 80/ },
    { name: "a license that is not valid", record: change((item) => { item.image.license = { ...license, licenseURL: "http://example.com/" }; }), want: /licenseURL must be an HTTPS URL/ },
  ];
  for (const item of bad) assert.throws(() => editor.storedBackground(item.record), item.want, item.name);
});

test("the license facts have their limits, text only and HTTPS links", () => {
  const license = { source: "", attribution: "", license: "", licenseURL: "", copyrightURL: "", retrieved: "", method: "", notice: "" };
  assert.equal(editor.licenseError(null), "");
  assert.equal(editor.licenseError(license), "", "empty members");
  const full = Object.fromEntries(Object.entries(editor.LICENSE_LIMITS).map(([key, limit]) => [key, "é".repeat(limit)]));
  full.licenseURL = `https://example.com/${"a".repeat(editor.LICENSE_LIMITS.licenseURL - 20)}`; full.copyrightURL = "https://www.openstreetmap.org/copyright"; full.retrieved = "2026-09-28T12:00:00.000+01:00";
  assert.equal(editor.licenseError(full), "", "each member at its limit");
  const cases = [
    { name: "a list", license: [], want: "The license must be an object." },
    { name: "a missing member", license: (({ notice: _, ...rest }) => rest)(license), want: "The license notice must be text." },
    { name: "another member", license: { ...license, tiles: "yes" }, want: 'The license has an unknown member "tiles".' },
    { name: "a number", license: { ...license, source: 3 }, want: "The license source must be text." },
    { name: "a long attribution", license: { ...license, attribution: "a".repeat(501) }, want: "The license attribution must have at most 500 characters." },
    { name: "an HTTP copyright URL", license: { ...license, copyrightURL: "http://www.openstreetmap.org/copyright" }, want: "The license copyrightURL must be an HTTPS URL." },
    { name: "a script link", license: { ...license, licenseURL: "javascript:alert(1)" }, want: "The license licenseURL must be an HTTPS URL." },
    { name: "a date with no time", license: { ...license, retrieved: "2026-09-28" }, want: "The license retrieved time must be an ISO 8601 time." },
  ];
  for (const item of cases) assert.equal(editor.licenseError(item.license), item.want, item.name);
});

test("a background keeper counts a pending or failed delete as unsaved until the delete commits", async (t) => {
  const broken = new Error("The disk failed.");
  // gated gives a store whose delete waits for release, or rejects with
  // broken when fail is set.
  const gated = (fail) => {
    const store = fakeDraftStore(); let release = null;
    const remove = store.delete;
    store.delete = (key) => (fail ? (store.calls.push("delete"), Promise.reject(broken)) : new Promise((resolve) => { release = () => resolve(remove(key)); }));
    return { store, release: () => release() };
  };
  for (const fail of [false, true]) {
    await t.test(fail ? "a rejected delete" : "a delayed delete", async () => {
      const { store, release } = gated(fail); store.records.set(DRAFT_KEY, recordOf(TEST_BACKGROUND));
      const page = { background: TEST_BACKGROUND };
      const keeper = backgroundKeeper(store, page);
      await keeper.load();
      assert.equal(keeper.unsaved, false, "before arm, the page has not decided");
      await keeper.arm();
      page.background = null;
      const armed = keeper.flush();
      assert.equal(keeper.unsaved, true, "the delete is pending");
      if (!fail) {
        await new Promise((resolve) => setImmediate(resolve));
        assert.equal(keeper.unsaved, true, "the delete still waits");
        release();
      }
      await armed;
      assert.deepEqual([keeper.unsaved, keeper.status], fail ? [true, "failed"] : [false, "ok"]);
      assert.equal(store.records.has(DRAFT_KEY), fail, "the record is gone only after a committed delete");
    });
  }

  await t.test("the draft keeper counts a null snapshot only while its delete is in the queue", async () => {
    const store = fakeDraftStore(); store.records.set(DRAFT_KEY, savedRecord("Saved"));
    const keeper = editor.createDraftKeeper({ store, key: DRAFT_KEY, delay: editor.DRAFT_SAVE_DELAY, clock: globalThis, snapshot: () => null });
    await keeper.load();
    assert.equal(keeper.unsaved, false, "the saved draft waits for an offer");
    const armed = keeper.arm();
    assert.equal(keeper.unsaved, true, "the delete is in the queue");
    await armed;
    assert.deepEqual([keeper.unsaved, store.records.has(DRAFT_KEY)], [false, false]);
  });
});

test("an image put in the queue and then Remove background stay unsaved until the delete commits", async () => {
  // The background store starts empty. Each put waits for release.
  const store = fakeDraftStore(); const puts = []; const put = store.put;
  store.put = (key, record) => new Promise((resolve) => { puts.push(() => resolve(put(key, record))); });
  const page = { background: null };
  const keeper = backgroundKeeper(store, page);
  await keeper.load(); await keeper.arm();
  page.background = TEST_BACKGROUND;
  const putDone = keeper.flush();
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(keeper.unsaved, true, "the image put is in the queue");
  page.background = null;
  assert.equal(keeper.unsaved, true, "Remove background while the put is in the queue");
  const deleteDone = keeper.flush();
  puts.shift()();
  await putDone;
  assert.equal(store.records.has(DRAFT_KEY), true, "the put committed first");
  assert.equal(keeper.unsaved, true, "the delete is still in the queue");
  await deleteDone;
  assert.deepEqual([keeper.unsaved, store.records.has(DRAFT_KEY), keeper.status], [false, false, "ok"], "the final state is empty");
});

// heldStore gives a draft store whose put and delete wait for the test.
// calls lists each write with the name of the record, or "delete". next
// ends the oldest waiting write, and fail rejects it with an error.
function heldStore() {
  const records = new Map(); const calls = []; const held = [];
  const hold = (name, action) => { calls.push(name); return new Promise((resolve, reject) => { held.push({ commit: () => { action(); resolve(); }, reject }); }); };
  return {
    records, calls,
    get: async (key) => structuredClone(records.get(key)),
    put: (key, record) => hold(`put ${record.name}`, () => records.set(key, structuredClone(record))),
    delete: (key) => hold("delete", () => records.delete(key)),
    next: () => held.shift().commit(),
    fail: (error) => held.shift().reject(error),
  };
}

// pinKeeper gives a keeper of page.record on store with a channel that
// counts its messages, and hooks that count the queued writes of each
// record name. pins gives the count of each name, and live gives the
// number of writes that did not settle.
function pinKeeper(store, page) {
  const pins = new Map(); const log = []; const commits = []; const channel = { messages: 0, postMessage() { channel.messages += 1; }, addEventListener() {} };
  const count = (record, step) => { const name = record ? record.name : "delete"; pins.set(name, (pins.get(name) || 0) + step); log.push(`${step > 0 ? "queued" : "settled"} ${name}`); };
  const keeper = editor.createDraftKeeper({
    store, key: DRAFT_KEY, delay: editor.DRAFT_SAVE_DELAY, clock: globalThis, channel,
    snapshot: () => (page.record ? { ...page.record } : null), onStatus: (status) => log.push(`status ${status}`),
    onQueued: (record) => count(record, 1), onSettled: (record, committed) => { count(record, -1); commits.push(committed); },
  });
  const live = () => [...pins.values()].reduce((sum, value) => sum + value, 0);
  return { keeper, pins, log, commits, channel, live };
}

const tick = () => new Promise((resolve) => setImmediate(resolve));

test("a newer write replaces the waiting write, and the replaced write settles with no store call", async () => {
  const store = heldStore(); const page = { record: { name: "X" } };
  const { keeper, pins, log, commits, channel, live } = pinKeeper(store, page);
  await keeper.load();
  const first = keeper.arm(); await tick();
  assert.deepEqual([store.calls, live(), pins.get("X")], [["put X"], 1, 1], "X runs");
  page.record = { name: "Y" }; const waiting = keeper.flush();
  assert.deepEqual([live(), keeper.unsaved], [2, true], "Y waits");
  page.record = { name: "X" }; const last = keeper.flush();
  await waiting;
  assert.deepEqual([store.calls, live(), pins.get("X"), pins.get("Y")], [["put X"], 2, 2, 0], "the second X replaces Y, and Y settles with no store call");
  assert.deepEqual([store.records.size, channel.messages, keeper.status], [0, 0, "ok"], "Y does not commit or broadcast");
  store.next(); await first; await tick();
  assert.deepEqual([store.calls, live(), pins.get("X"), keeper.unsaved], [["put X", "put X"], 1, 1, true], "the first X commits, then the second X runs");
  store.next(); await last;
  assert.deepEqual([live(), pins.get("X"), keeper.unsaved, channel.messages], [0, 0, false, 2]);
  assert.deepEqual(log, ["queued X", "queued Y", "queued X", "settled Y", "settled X", "settled X"], "the hooks run once for each write");
  assert.deepEqual(commits, [false, true, true], "only the writes that the store committed settle as committed");
  assert.deepEqual(store.records.get(DRAFT_KEY), { name: "X" });
});

test("a blocked keeper releases replaced records and image buffers before storage resumes", () => {
  const script = `
    const assert = require("node:assert/strict");
    const { queryObjects } = require("node:v8");
    const editor = require(${JSON.stringify(path.join(__dirname, "editor.js"))});
    class ImageBytes extends ArrayBuffer {}
    class Snapshot {
      constructor(name) { this.name = name; this.image = new ImageBytes(1024 * 1024); }
    }
    const held = []; const calls = []; const settled = []; const promises = [];
    let current = null;
    const keeper = editor.createDraftKeeper({
      store: { get: async () => null, put: async (_, record) => {
        calls.push(record.name); await new Promise((resolve) => held.push(resolve));
      } },
      key: "retention", clock: globalThis, snapshot: () => current,
      text: (record) => String(record.name), onSettled: (record) => settled.push(record.name),
    });
    const tick = () => new Promise((resolve) => setImmediate(resolve));
    const queue = (name) => { current = new Snapshot(name); promises.push(keeper.flush()); };
    (async () => {
      await keeper.load(); await keeper.arm();
      queue(0); await tick();
      for (let name = 1; name <= 24; name += 1) queue(name);
      await Promise.all(promises.slice(1, -1)); await tick();
      assert.deepEqual(calls, [0]);
      assert.equal(settled.length, 23);
      // queryObjects collects garbage before counting instances. No finalizer must run.
      assert.equal(queryObjects(Snapshot), 2, "only the running and waiting records remain");
      assert.equal(queryObjects(ImageBytes), 2, "only the running and waiting buffers remain");
      held.shift()(); await tick();
      assert.deepEqual(calls, [0, 24]);
      held.shift()(); await Promise.all(promises);
      current = null; await tick();
      assert.equal(queryObjects(Snapshot), 0, "settled records are released");
      assert.equal(queryObjects(ImageBytes), 0, "settled buffers are released");
      assert.equal(settled.length, 25);
      process.stdout.write("done");
    })().catch((error) => { console.error(error); process.exitCode = 1; });
  `;
  assert.equal(execFileSync(process.execPath, ["--disable-warning=ExperimentalWarning", "-e", script], { timeout: 10000, encoding: "utf8" }), "done");
});

test("a failed running write settles once, and the waiting write then commits", async () => {
  const store = heldStore(); const page = { record: { name: "X" } };
  const { keeper, pins, log, commits, live } = pinKeeper(store, page);
  await keeper.load();
  const first = keeper.arm(); await tick();
  page.record = { name: "Y" }; const second = keeper.flush();
  store.fail(new Error("The disk failed.")); await first;
  assert.deepEqual([keeper.status, pins.get("X"), live(), keeper.unsaved], ["failed", 0, 1, true], "X failed, and Y waits");
  await tick();
  assert.deepEqual(store.calls, ["put X", "put Y"]);
  store.next(); await second;
  assert.deepEqual([keeper.status, live(), keeper.unsaved, store.records.get(DRAFT_KEY)], ["ok", 0, false, { name: "Y" }]);
  assert.deepEqual(log, ["queued X", "queued Y", "status failed", "settled X", "status ok", "settled Y"]);
  assert.deepEqual(commits, [false, true]);
  await keeper.flush();
  assert.equal(store.calls.length, 2, "Y is in the store, so a flush does not write");
});

test("replace writes also when the waiting write has the same text", async () => {
  const store = heldStore(); const page = { record: { name: "X" } };
  const { keeper, log } = pinKeeper(store, page);
  await keeper.load();
  const first = keeper.arm(); await tick();
  page.record = { name: "Y" }; keeper.flush();
  const forced = keeper.replace();
  assert.deepEqual(log, ["queued X", "queued Y", "queued Y", "settled Y"], "the forced write replaces the waiting write");
  store.next(); await first; await tick();
  store.next(); await forced;
  assert.deepEqual([store.calls, store.records.get(DRAFT_KEY), keeper.unsaved], [["put X", "put Y"], { name: "Y" }, false]);
  page.record = null; const removed = keeper.flush(); await tick();
  assert.deepEqual(store.calls.at(-1), "delete");
  store.next(); await removed;
  assert.deepEqual(log.slice(-2), ["queued delete", "settled delete"], "a delete runs the hooks with no record");
});

test("the keeper text of a background record leaves out the image, and the image key tells two images apart", () => {
  const other = { ...TEST_BACKGROUND, imageKey: "fedcba9876543210fedcba9876543210", dataURL: dataURL(pngBytes(2, 4)) };
  const text = editor.backgroundRecordText(recordOf(TEST_BACKGROUND));
  assert.ok(!text.includes('"image"') && text.includes(TEST_KEY), text);
  assert.notEqual(editor.backgroundRecordText(recordOf(other)), text, "the same placement with another image");
  assert.equal(editor.backgroundRecordText(recordOf({ ...TEST_BACKGROUND, dataURL: other.dataURL })), text, "the key stands for the image");
  assert.equal(editor.backgroundRecordText({ background: { x: 1n } }), "invalid", "a record that JSON cannot write");
  assert.equal(editor.backgroundRecordText("text"), '"text"');
  assert.match(editor.newImageKey(globalThis.crypto), editor.IMAGE_KEY_PATTERN);
  assert.notEqual(editor.newImageKey(globalThis.crypto), editor.newImageKey(globalThis.crypto));
});

test("a stored background that does not restore stays in the store until a new background of the page", async () => {
  const factory = fakeIndexedDB(); const store = editor.openRecordStore(factory, editor.BACKGROUND_STORE);
  const broken = recordOf(TEST_BACKGROUND); broken.background.imageKey = "not a key";
  await store.put(DRAFT_KEY, broken);
  const page = startupPage(factory);
  await page.started;
  assert.deepEqual([page.history.value.background, page.warnings], [null, [`The stored background has no valid image key. ${editor.STORED_BACKGROUND_KEPT_TEXT}`]]);
  // An edit of the scenario and the flush at unload do not write.
  page.history.replace({ scenario: { ...page.history.value.scenario, name: "Edited" }, background: null });
  page.keeper.schedule(); await page.keeper.flush();
  assert.deepEqual([await store.get(DRAFT_KEY), page.keeper.unsaved, page.counts.writes], [broken, false, 0], "the record stays, and unload does not ask");
  // Remove background with no background does not change the page.
  await page.keeper.flush();
  assert.deepEqual(await store.get(DRAFT_KEY), broken);
  // A new image replaces the record, and an undo back to no background
  // then deletes it.
  page.history.replace({ scenario: page.history.value.scenario, background: TEST_BACKGROUND });
  page.keeper.schedule();
  assert.equal(page.keeper.unsaved, true);
  await page.keeper.flush();
  assert.deepEqual(await store.get(DRAFT_KEY), recordOf(TEST_BACKGROUND));
  page.history.undo(); await page.keeper.flush();
  assert.deepEqual([await store.get(DRAFT_KEY), page.keeper.unsaved], [undefined, false]);
});

test("a tab that restored an image does not write it back over the image of another tab", async () => {
  const factory = fakeIndexedDB(); const store = editor.openRecordStore(factory, editor.BACKGROUND_STORE);
  await store.put(DRAFT_KEY, recordOf(TEST_BACKGROUND));
  const page = startupPage(factory);
  await page.started;
  assert.deepEqual([page.history.value.background, page.counts.writes], [TEST_BACKGROUND, 0], "arm after the restore writes nothing");
  const other = { ...TEST_BACKGROUND, imageKey: "fedcba9876543210fedcba9876543210", x: 99 };
  await store.put(DRAFT_KEY, recordOf(other));
  page.history.replace({ scenario: { ...page.history.value.scenario, name: "Edited" }, background: page.history.value.background });
  page.keeper.schedule(); await page.keeper.flush();
  assert.deepEqual([await store.get(DRAFT_KEY), page.keeper.unsaved], [recordOf(other), false], "the store keeps the image of the other tab");
});

test("the restore reads only the new background database", async () => {
  const factory = fakeIndexedDB();
  const older = editor.openRecordStore(factory, { database: "podsim-editor-backgrounds", store: "backgrounds" });
  await older.put(DRAFT_KEY, { background: { dataURL: TEST_BACKGROUND.dataURL, x: 0, y: 0, width: 4, height: 2, opacity: 1 } });
  const page = startupPage(factory);
  await page.started;
  assert.deepEqual([page.history.value.background, page.warnings], [null, []], "the older record is not read");
  assert.deepEqual(factory.log, ["open podsim-editor-backgrounds 1", "create backgrounds", "open podsim-editor-backgrounds-2 1", "create backgrounds"]);
  assert.equal(editor.BACKGROUND_STORE.database, "podsim-editor-backgrounds-2");
});

test("restoreStoredBackground puts a valid stored background on the page, and warns of one that is not valid", async () => {
  const cases = [
    { name: "no record", record: null, want: null, warnings: [] },
    { name: "a valid record", record: recordOf(TEST_BACKGROUND), want: TEST_BACKGROUND, warnings: [] },
    { name: "a record that is not valid", record: { background: { x: 1 } }, want: null, warnings: [`The stored background has no valid image key. ${editor.STORED_BACKGROUND_KEPT_TEXT}`] },
  ];
  for (const item of cases) {
    const page = { background: null }; const warnings = [];
    const installed = await editor.restoreStoredBackground(item.record, { warn: (text) => warnings.push(text), install: (restored) => { page.background = pageOf(restored); } });
    assert.deepEqual([installed, page.background, warnings], [item.want !== null, item.want, item.warnings], item.name);
  }
});

// fakeElement gives an element with the calls that blockInput and
// startupExempt use. attributes holds its attributes.
function fakeElement(id, attributes = {}) {
  const element = {
    id, attributes: new Map(Object.entries(attributes)),
    setAttribute(name, value) { element.attributes.set(name, String(value)); },
    removeAttribute(name) { element.attributes.delete(name); },
    closest(selector) { return selector === `#${id}` ? element : null; },
  };
  return element;
}

// fakeStartupDocument gives a document with the calls that blockInput
// uses. It has the three regions that the editor page marks with
// data-startup. dispatch sends an event of type to target, and gives the
// event. The capture listeners of the document get the event first, as in
// a browser. Then handler runs as the listener of the page, unless a
// capture listener stopped the event.
function fakeStartupDocument() {
  const listeners = new Map();
  const regions = ["actions", "draftOffer", "workspace"].map((id) => fakeElement(id, { "data-startup": "" }));
  return {
    body: fakeElement("body"), regions,
    listenerCount: () => [...listeners.values()].reduce((count, set) => count + set.size, 0),
    querySelectorAll(selector) { assert.equal(selector, "[data-startup]"); return regions.filter((element) => element.attributes.has("data-startup")); },
    addEventListener(type, listener, options) {
      assert.deepEqual(options, { capture: true, passive: false });
      if (!listeners.has(type)) listeners.set(type, new Set());
      listeners.get(type).add(listener);
    },
    removeEventListener(type, listener, options) { assert.deepEqual(options, { capture: true, passive: false }); listeners.get(type)?.delete(listener); },
    dispatch(type, target, handler, fields = {}) {
      const event = {
        type, target, ...fields, stopped: false, prevented: false,
        stopImmediatePropagation() { event.stopped = true; }, preventDefault() { event.prevented = true; },
      };
      for (const listener of listeners.get(type) || []) { listener(event); if (event.stopped) break; }
      if (!event.stopped) handler(event);
      return event;
    },
  };
}

// startupPage starts an editor page with startEditor. The page keeps its
// background in factory, as the editor does. history holds the drafts of
// the page, and gate is its startup gate. options.readLive is a promise
// that the live load waits for. options.decode, when given, decodes the
// stored image for checkImageBytes, and gives its size. writes counts the
// writes that the background keeper queues.
function startupPage(factory, options = {}) {
  const document = fakeStartupDocument(); const gate = editor.createStartupGate();
  // history holds the drafts of the page. reset starts a new history,
  // replace adds an undo step, and undo goes back one step.
  const history = {
    steps: [{ scenario: editor.emptyConfig(), background: null }],
    get value() { return structuredClone(this.steps.at(-1)); },
    get canUndo() { return this.steps.length > 1; },
    reset(value) { this.steps = [value]; },
    replace(value) { this.steps.push(value); },
    undo() { if (this.steps.length < 2) return false; this.steps.pop(); return true; },
  };
  const page = { get background() { return history.value.background; } }; const counts = { writes: 0 };
  const keeper = backgroundKeeper(editor.openRecordStore(factory, editor.BACKGROUND_STORE), page, { onQueued: () => { counts.writes += 1; } });
  const warnings = [];
  const cleared = editor.blockInput(document, gate, editor.startupExempt);
  const decode = options.decode && ((restored) => editor.checkImageBytes(restored.image.bytes, { decode: async (blob) => ({ ...(await options.decode(blob)), close() {} }) }));
  const started = editor.startEditor({
    readDraft: async () => null, readBackground: () => keeper.load(),
    loadLive: async () => { await options.readLive; history.reset({ scenario: connectedScenario(), background: null }); },
    restore: (record) => editor.restoreStoredBackground(record, {
      decode, warn: (text) => warnings.push(text), install: (restored) => history.reset({ scenario: history.value.scenario, background: pageOf(restored) }),
    }),
    release: gate.release, armBackground: () => keeper.arm(), offerDraft: () => {},
  });
  return { document, gate, history, keeper, warnings, cleared, started, counts };
}

test("the startup gate blocks input until the release, except the Simulation link and Tab", async () => {
  const document = fakeStartupDocument(); const gate = editor.createStartupGate();
  const cleared = editor.blockInput(document, gate, editor.startupExempt);
  const map = fakeElement("networkMap"); const link = fakeElement("simulationLink");
  const marks = () => [document.body.attributes.get("aria-busy"), ...document.regions.map((element) => [element.attributes.has("inert"), element.attributes.has("data-startup")])];
  assert.deepEqual(marks(), ["true", [true, true], [true, true], [true, true]]);
  assert.equal(document.listenerCount(), editor.STARTUP_EVENTS.length);
  let runs = 0; const handler = () => { runs += 1; };
  for (const type of editor.STARTUP_EVENTS) {
    const event = document.dispatch(type, map, handler, { key: "z" });
    assert.deepEqual([event.stopped, event.prevented], [true, true], type);
  }
  assert.equal(runs, 0, "no page listener runs while the gate is closed");
  const passed = [
    document.dispatch("click", link, handler), document.dispatch("keydown", link, handler, { key: "Enter" }),
    document.dispatch("keydown", map, handler, { key: "Tab" }),
  ];
  assert.deepEqual([runs, passed.map((event) => event.prevented)], [3, [false, false, false]], "the link and Tab pass");

  gate.release(); gate.release();
  await cleared;
  assert.equal(gate.closed, false);
  assert.deepEqual(marks(), [undefined, [false, false], [false, false], [false, false]]);
  assert.equal(document.listenerCount(), 0);
  assert.equal(document.dispatch("click", map, handler).prevented, false);
  assert.equal(runs, 4, "input works after the release");

  const html = fs.readFileSync(path.join(__dirname, "editor.html"), "utf8");
  for (const tag of [/<div class="actions"[^>]*\sdata-startup[\s>]/, /<section id="draftOffer"[^>]*\sdata-startup[\s>]/, /<main class="workspace"[^>]*\sdata-startup[\s>]/]) assert.match(html, tag);
});

test("an import while the live scenario loads is stopped, and the stored background survives", async () => {
  const factory = fakeIndexedDB(); const store = editor.openRecordStore(factory, editor.BACKGROUND_STORE);
  const record = recordOf(TEST_BACKGROUND);
  await store.put(DRAFT_KEY, record);
  let finishLive; const page = startupPage(factory, { readLive: new Promise((resolve) => { finishLive = resolve; }) });
  // The import of a project file with no background removes the
  // background.
  let imports = 0;
  const importFile = () => { imports += 1; page.history.replace({ scenario: { ...page.history.value.scenario, name: "Imported" }, background: null }); };
  await new Promise((resolve) => setImmediate(resolve));
  const event = page.document.dispatch("change", fakeElement("projectImport"), importFile);
  assert.deepEqual([imports, event.prevented, page.gate.closed], [0, true, true], "the gate stops the import");
  finishLive(); await page.started;
  assert.deepEqual([page.history.value.background, page.history.canUndo, page.warnings], [TEST_BACKGROUND, false, []]);
  assert.deepEqual(await store.get(DRAFT_KEY), record, "the armed keeper keeps the record");
  page.document.dispatch("change", fakeElement("projectImport"), importFile);
  assert.equal(imports, 1, "an import after the startup runs");
});

test("the startup gate opens when a startup step fails, and the background keeper then does not start", async () => {
  const failure = new Error("The step failed.");
  // The page catches a failed live read, and loads the fallback draft.
  // Thus loadLive fails only on an error that the page does not expect.
  const cases = [
    { name: "the live read fails, and the page loads the fallback draft", fail: "", want: ["loadLive", "restore", "release", "armBackground", "offerDraft"] },
    { name: "the live load throws", fail: "loadLive", want: ["loadLive", "release"] },
    { name: "the restore throws", fail: "restore", want: ["loadLive", "restore", "release"] },
  ];
  for (const item of cases) {
    const calls = []; const gate = editor.createStartupGate();
    const step = (name, value) => async (argument) => {
      calls.push(name);
      if (value !== undefined) assert.equal(argument, value, name);
      if (item.fail === name) throw failure;
    };
    const started = editor.startEditor({
      readDraft: async () => "draft", readBackground: async () => "record",
      loadLive: step("loadLive"), restore: step("restore", "record"),
      release: () => { calls.push("release"); gate.release(); }, armBackground: step("armBackground"), offerDraft: step("offerDraft", "draft"),
    });
    if (item.fail) await assert.rejects(started, failure, item.name);
    else await started;
    assert.deepEqual([calls, gate.closed], [item.want, false], item.name);
  }
});

test("a saved draft read that does not end does not block the startup", async () => {
  const calls = []; const gate = editor.createStartupGate();
  let finishDraft; const saved = new Promise((resolve) => { finishDraft = resolve; });
  const started = editor.startEditor({
    readDraft: () => saved, readBackground: async () => null,
    loadLive: async () => { calls.push("loadLive"); }, restore: async () => { calls.push("restore"); },
    release: () => { calls.push("release"); gate.release(); },
    armBackground: async () => { calls.push("armBackground"); }, offerDraft: (record) => { calls.push(`offerDraft ${record}`); },
  });
  await new Promise((resolve) => setImmediate(resolve));
  assert.deepEqual([calls, gate.closed], [["loadLive", "restore", "release", "armBackground"], false], "the gate opens with no saved draft read");
  finishDraft("draft"); await started;
  assert.deepEqual(calls.slice(4), ["offerDraft draft"], "the offer comes when the read ends");
});

test("after the startup, a background change stays in the store, and a reload finds it", async () => {
  const factory = fakeIndexedDB();
  const first = startupPage(factory);
  await first.started; await first.cleared;
  first.document.dispatch("change", fakeElement("backgroundImport"), () => first.history.replace({ scenario: first.history.value.scenario, background: TEST_BACKGROUND }));
  await first.keeper.flush();
  const second = startupPage(factory);
  await second.started;
  assert.deepEqual([second.history.value.background, second.history.canUndo, second.warnings], [TEST_BACKGROUND, false, []]);
});

test("after an apply clears the saved draft, a reload still gets the background", async (t) => {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  const factory = fakeIndexedDB(); const scenario = { ...connectedScenario(), name: "Changed" };
  const page = { value: { scenario, background: TEST_BACKGROUND }, live: { scenario: JSON.stringify(connectedScenario()), revision: 3 } };
  const base = { revision: 3, epoch: "epoch-1", serverStart: "start-1" };
  const background = { get background() { return page.value.background; } };
  const keepers = () => ({
    draft: editor.createDraftKeeper({ store: editor.openRecordStore(factory, editor.DRAFT_STORE), key: DRAFT_KEY, delay: editor.DRAFT_SAVE_DELAY, clock: globalThis, snapshot: () => editor.draftRecordFor(page.value, page.live, base) }),
    background: backgroundKeeper(editor.openRecordStore(factory, editor.BACKGROUND_STORE), background),
  });
  const first = keepers();
  assert.deepEqual([await first.draft.load(), await first.background.load()], [null, null]);
  // The page imports the image after the startup.
  page.value = { scenario, background: null };
  await first.draft.arm(); await first.background.arm();
  page.value = { scenario, background: TEST_BACKGROUND };
  await first.background.flush();
  // A successful apply makes the draft the live baseline and clears the
  // saved draft. The background stays.
  page.live = { scenario: JSON.stringify(scenario), revision: 4 };
  await first.draft.replace();
  await first.draft.arm();

  const second = keepers();
  assert.equal(await second.draft.load(), null, "no saved draft after the apply");
  assert.deepEqual(pageOf(editor.storedBackground(await second.background.load())), TEST_BACKGROUND, "the reload gets the background");

  // Remove background deletes the record.
  await second.background.arm();
  page.value = { scenario, background: null };
  await second.background.flush();
  assert.equal(await keepers().background.load(), null, "the removed background is gone");
});

test("the page offers a saved draft only when it differs from the live scenario", () => {
  const live = { scenario: JSON.stringify(connectedScenario()), revision: 5 };
  const liveRecord = { scenario: connectedScenario(), revision: 5, epoch: "epoch-1", serverStart: "start-1" };
  const olderRecord = savedRecord("Changed"); delete olderRecord.serverStart;
  // want gives the revision, the epoch, the server start ID, the scenario
  // name, and the members of the offered draft, or null for no offer.
  const cases = [
    { name: "no saved draft", record: null, want: null },
    { name: "a record that is not an object", record: "draft", want: null },
    { name: "a record without a scenario", record: { revision: 3 }, want: null },
    { name: "a scenario that is an array", record: { scenario: [], revision: 3 }, want: null },
    { name: "the live scenario", record: liveRecord, want: null },
    { name: "the live scenario and a background of an older editor", record: { ...liveRecord, background: TEST_BACKGROUND }, want: null },
    { name: "a changed scenario", record: savedRecord("Changed"), want: { revision: 3, epoch: "epoch-1", serverStart: "start-1", name: "Changed", members: ["scenario"] } },
    { name: "a changed scenario and a background of an older editor", record: { ...savedRecord("Changed"), background: TEST_BACKGROUND }, want: { revision: 3, epoch: "epoch-1", serverStart: "start-1", name: "Changed", members: ["scenario"] } },
    { name: "a revision that is not a number", record: { ...savedRecord("Changed"), revision: "x" }, want: { revision: 0, epoch: "epoch-1", serverStart: "start-1", name: "Changed", members: ["scenario"] } },
    { name: "an epoch that is not a string", record: { ...savedRecord("Changed"), epoch: 7 }, want: { revision: 3, epoch: "", serverStart: "start-1", name: "Changed", members: ["scenario"] } },
    { name: "a record from an older editor without a server start ID", record: olderRecord, want: { revision: 3, epoch: "epoch-1", serverStart: "", name: "Changed", members: ["scenario"] } },
    { name: "a server start ID that is not a string", record: { ...savedRecord("Changed"), serverStart: 7 }, want: { revision: 3, epoch: "epoch-1", serverStart: "", name: "Changed", members: ["scenario"] } },
  ];
  for (const item of cases) {
    const offer = editor.draftOffer(item.record, live);
    assert.deepEqual(offer && { revision: offer.revision, epoch: offer.epoch, serverStart: offer.serverStart, name: offer.draft.scenario.name, members: Object.keys(offer.draft) }, item.want, item.name);
  }

  const texts = [
    { name: "the live revision", offer: { revision: 5, live: 5 }, want: "This browser has a saved draft that is not applied. The draft is based on revision 5." },
    {
      name: "an older revision", offer: { revision: 3, live: 5 },
      want: "This browser has a saved draft that is not applied. The draft is based on revision 3. The live scenario is now revision 5.",
    },
    { name: "no live scenario", offer: { revision: 3, live: null }, want: "This browser has a saved draft that is not applied. The draft is based on revision 3." },
    {
      name: "the same server start", offer: { revision: 5, live: 5, serverStart: "start-1", liveStart: "start-1" },
      want: "This browser has a saved draft that is not applied. The draft is based on revision 5.",
    },
    {
      name: "a server restart", offer: { revision: 5, live: 5, serverStart: "start-1", liveStart: "start-2" },
      want: "This browser has a saved draft that is not applied. The draft is based on revision 5. The draft was saved before the server restarted. After you restore it, Pause and apply stops with a conflict and does not change the live scenario. Then you can load the live scenario or apply the draft over it.",
    },
    {
      name: "a server restart and an older revision", offer: { revision: 3, live: 5, serverStart: "start-1", liveStart: "start-2" },
      want: `This browser has a saved draft that is not applied. The draft is based on revision 3. The live scenario is now revision 5. ${editor.DRAFT_RESTART_TEXT}`,
    },
    {
      name: "a draft from an older editor", offer: { revision: 5, live: 5, serverStart: "", liveStart: "start-2" },
      want: "This browser has a saved draft that is not applied. The draft is based on revision 5.",
    },
    {
      name: "no live server start", offer: { revision: 3, live: null, serverStart: "start-1", liveStart: "" },
      want: "This browser has a saved draft that is not applied. The draft is based on revision 3.",
    },
  ];
  for (const item of texts) assert.equal(editor.draftOfferText(item.offer), item.want, item.name);
});

test("a draft is from before a server restart only when both server start IDs are known and differ", () => {
  const cases = [
    { name: "the same server start", draft: "start-1", live: "start-1", want: false },
    { name: "a different server start", draft: "start-1", live: "start-2", want: true },
    { name: "no draft server start", draft: "", live: "start-2", want: false },
    { name: "no live server start", draft: "start-1", live: "", want: false },
    { name: "no server start IDs", draft: "", live: "", want: false },
  ];
  for (const item of cases) assert.equal(editor.draftBeforeRestart(item.draft, item.live), item.want, item.name);
});

test("the saved draft record keeps the draft base through the draft store", async () => {
  const live = { scenario: JSON.stringify(connectedScenario()), revision: 5 };
  const changed = { scenario: { ...connectedScenario(), name: "Changed" }, background: null };
  const base = { revision: 3, epoch: "epoch-1", serverStart: "start-1" };
  assert.equal(editor.draftRecordFor(changed, null, base), null, "no live baseline");
  assert.equal(editor.draftRecordFor({ scenario: connectedScenario(), background: null }, live, base), null, "no changes");

  const store = fakeDraftStore(null);
  await store.put(DRAFT_KEY, editor.draftRecordFor(changed, live, base));
  const offer = editor.draftOffer(await store.get(DRAFT_KEY), live);
  assert.deepEqual({ revision: offer.revision, epoch: offer.epoch, serverStart: offer.serverStart, name: offer.draft.scenario.name }, { ...base, name: "Changed" });
});

test("the status after a restore names the live changes that an older draft replaces", () => {
  const cases = [
    { name: "the live revision", draft: { revision: 5, live: 5 }, want: "Live revision 5. The restored draft is not applied." },
    {
      name: "an older revision", draft: { revision: 3, live: 5 },
      want: "Live revision 5. The restored draft is not applied. It is based on revision 3. Pause and apply replaces the live changes after revision 3.",
    },
    { name: "no live scenario", draft: { revision: 3, live: null }, want: "The restored draft is local." },
    {
      name: "the same server start and an older revision", draft: { revision: 3, live: 5, serverStart: "start-1", liveStart: "start-1" },
      want: "Live revision 5. The restored draft is not applied. It is based on revision 3. Pause and apply replaces the live changes after revision 3.",
    },
    {
      name: "a server restart and an older revision", draft: { revision: 3, live: 5, serverStart: "start-1", liveStart: "start-2" },
      want: "Live revision 5. The restored draft is not applied. It was saved before the server restarted. Pause and apply stops with a conflict and does not change the live scenario. After the conflict, the Apply over revision button replaces the live scenario with the draft.",
    },
    {
      name: "a server restart and the same revision", draft: { revision: 5, live: 5, serverStart: "start-1", liveStart: "start-2" },
      want: `Live revision 5. The restored draft is not applied. ${editor.RESTORED_RESTART_TEXT}`,
    },
    {
      name: "a draft from an older editor", draft: { revision: 3, live: 5, serverStart: "", liveStart: "start-2" },
      want: "Live revision 5. The restored draft is not applied. It is based on revision 3. Pause and apply replaces the live changes after revision 3.",
    },
  ];
  for (const item of cases) assert.equal(editor.restoreStatusText(item.draft), item.want, item.name);
});

test("each live state read gives the latest server start ID, so the saved draft offer tells of a later restart", async () => {
  const offerText = (liveStart) => editor.draftOfferText({ revision: 3, live: 3, serverStart: "start-1", liveStart });
  const server = snapshotServer();
  const starts = [];
  server.connection.onServerStart = (start) => starts.push(start);

  const live = await editor.readLive(server.connection, asServed);
  assert.equal(live.serverStart, "start-1");
  assert.equal(offerText(starts.at(-1)).includes(editor.DRAFT_RESTART_TEXT), false, "the load before the restart");

  // A restart while the offer shows. The page reads the state again when
  // the editor window gets the focus.
  server.live.serverStart = "start-2";
  await editor.readState(server.connection);
  assert.equal(starts.at(-1), "start-2", "the read after the restart");
  assert.equal(offerText(starts.at(-1)).includes(editor.DRAFT_RESTART_TEXT), true, "the offer after the restart");

  // A failed apply reads the conflict after a second restart.
  server.live.serverStart = "start-3";
  const failure = Object.assign(new Error("The server session changed."), { errorCode: "session_changed" });
  assert.deepEqual(await editor.readConflict(server.connection, failure, asServed), { code: "session_changed", revision: 3, serverStart: "start-3" });
  assert.equal(starts.at(-1), "start-3", "the conflict read");
  assert.equal(offerText(starts.at(-1)).includes(editor.DRAFT_RESTART_TEXT), true, "the offer after the conflict");

  // A connection without onServerStart reads as before.
  delete server.connection.onServerStart;
  assert.equal((await editor.readState(server.connection)).serverStart, "start-3");
});

// nested gives levels objects, each in the member next of the one
// before. A reply with nested(n) as its topology has n + 1 levels.
function nested(levels) {
  let value = {};
  for (let level = 1; level < levels; level++) value = { next: value };
  return value;
}

// stateReplyRefusals gives the state replies that the debug capture and
// the editor refuse because of their contract markers, their topology, a
// removed member or their size. envelope(markers) gives a valid reply with those
// contract markers at the root, in the topology and in the simulation. The
// incident, fault and emergency markers are only in the topology and in the
// simulation.
// shell_test.cjs has the same table.
function stateReplyRefusals(envelope) {
  const plain = envelope();
  const express = envelope({ orderContract: "express-v1" });
  const simulation = (reply, change) => ({ ...reply, frame: { ...reply.frame, state: { ...reply.frame.state, simulation: { ...reply.frame.state.simulation, ...change } } } });
  // incidentReply gives a plain reply with the incident marker value in
  // the topology and in the simulation, where the server puts it.
  const incidentReply = (value) => ({ ...simulation(plain, { incidentContract: value }), topology: { incidentContract: value } });
  const incident = incidentReply("incident-v1");
  // faultReply gives an incident reply with the fault marker value in the
  // topology and in the simulation, where the server puts it.
  const faultReply = (value) => ({ ...simulation(incident, { faultContract: value }), topology: { incidentContract: "incident-v1", faultContract: value } });
  const fault = faultReply("fault-v1");
  // emergencyReply gives an incident reply with the emergency marker value
  // in the topology and in the simulation, where the server puts it.
  const emergencyReply = (value) => ({ ...simulation(incident, { emergencyContract: value }), topology: { incidentContract: "incident-v1", emergencyContract: value } });
  const emergency = emergencyReply("emergency-v1");
  const without = (value, name) => Object.fromEntries(Object.entries(value).filter(([key]) => key !== name));
  const cases = [];
  // Earlier servers sent a textEncoding member and a couplingContract
  // marker. The presence of either anywhere in the reply refuses the
  // reply, whatever its value.
  for (const [name, sent] of [["textEncoding", "order-text-base64-v1"], ["couplingContract", "compact-pair-v1"]]) {
    for (const [kind, value] of [["", sent], ["an empty ", ""], ["a null ", null]]) {
      const member = { [name]: value };
      cases.push(
        [`${kind}${name} member at the root`, { ...express, ...member }],
        [`${kind}${name} member in the frame`, { ...plain, frame: { ...plain.frame, ...member } }],
        [`${kind}${name} member in the state`, { ...plain, frame: { ...plain.frame, state: { ...plain.frame.state, ...member } } }],
        [`${kind}${name} member in the topology`, { ...express, topology: { ...express.topology, ...member } }],
        [`${kind}${name} member in the simulation`, simulation(express, member)],
        [`${kind}${name} member in a route`, { ...plain, frame: { ...plain.frame, routes: [{ lanes: [], ...member }] } }],
      );
    }
  }
  cases.push(
    // The limits of a stream document of the server.
    ["a reply over the depth limit", { ...plain, topology: nested(64) }],
    ["a reply with an array over the element limit", { ...plain, topology: { lanes: new Array(65537).fill(0) } }],
    // The root, the topology and the simulation have the same markers.
    ["an Express root with an unmarked topology and simulation", { ...plain, orderContract: "express-v1" }],
    ["an unmarked root with an Express topology and simulation", without(express, "orderContract")],
    ["an Express root and topology with an unmarked simulation", { ...plain, orderContract: "express-v1", topology: { orderContract: "express-v1" } }],
    ["an unmarked Express topology", { ...express, topology: {} }],
    // The server decoder refuses a reply without a topology object.
    ["a reply without a topology", without(plain, "topology")],
    ["a reply with a null topology", { ...plain, topology: null }],
    ["a reply with a topology array", { ...plain, topology: [] }],
    ["a reply with a topology that is text", { ...plain, topology: "topology" }],
    // A marker has the one value that the server sends.
    ["an unknown order contract", envelope({ orderContract: "express-v2" })],
    ["an empty order contract", envelope({ orderContract: "" })],
    ["a null order contract", envelope({ orderContract: null })],
    ["an unknown order contract at the root only", { ...plain, orderContract: "express-v2" }],
    // The topology and the simulation carry the incident marker, and the
    // root does not. They have the same marker, with the one value that
    // the server sends.
    ["an incident marker at the root", { ...incident, incidentContract: "incident-v1" }],
    ["an incident marker at the root only", { ...plain, incidentContract: "incident-v1" }],
    ["an incident topology with an unmarked simulation", { ...plain, topology: { incidentContract: "incident-v1" } }],
    ["an incident simulation with an unmarked topology", simulation(plain, { incidentContract: "incident-v1" })],
  );
  for (const [kind, value] of [["an unknown", "incident-v2"], ["an empty", ""], ["a null", null]]) {
    cases.push(
      [`${kind} incident contract`, incidentReply(value)],
      [`${kind} incident contract in the simulation`, simulation(incident, { incidentContract: value })],
      [`${kind} incident contract in the topology`, { ...incident, topology: { incidentContract: value } }],
    );
  }
  cases.push(
    // The topology and the simulation carry the fault marker, and the root
    // does not. They have the same marker, with the one value that the
    // server sends, and the fault marker needs the incident marker.
    ["a fault marker at the root", { ...fault, faultContract: "fault-v1" }],
    ["a fault marker at the root only", { ...incident, faultContract: "fault-v1" }],
    ["a fault topology with an unmarked simulation", { ...incident, topology: { incidentContract: "incident-v1", faultContract: "fault-v1" } }],
    ["a fault simulation with an unmarked topology", simulation(incident, { faultContract: "fault-v1" })],
    ["a fault marker without the incident marker", { ...simulation(plain, { faultContract: "fault-v1" }), topology: { faultContract: "fault-v1" } }],
  );
  for (const [kind, value] of [["an unknown", "fault-v2"], ["an empty", ""], ["a null", null]]) {
    cases.push(
      [`${kind} fault contract`, faultReply(value)],
      [`${kind} fault contract in the simulation`, simulation(fault, { faultContract: value })],
      [`${kind} fault contract in the topology`, { ...fault, topology: { incidentContract: "incident-v1", faultContract: value } }],
      [`${kind} fault contract in the topology of an unmarked reply`, { ...incident, topology: { incidentContract: "incident-v1", faultContract: value } }],
      [`${kind} fault contract in the simulation of an unmarked reply`, simulation(incident, { faultContract: value })],
    );
  }
  cases.push(
    // The emergency marker has the rules of the fault marker.
    ["an emergency marker at the root", { ...emergency, emergencyContract: "emergency-v1" }],
    ["an emergency marker at the root only", { ...incident, emergencyContract: "emergency-v1" }],
    ["an emergency topology with an unmarked simulation", { ...incident, topology: { incidentContract: "incident-v1", emergencyContract: "emergency-v1" } }],
    ["an emergency simulation with an unmarked topology", simulation(incident, { emergencyContract: "emergency-v1" })],
    ["an emergency marker without the incident marker", { ...simulation(plain, { emergencyContract: "emergency-v1" }), topology: { emergencyContract: "emergency-v1" } }],
  );
  for (const [kind, value] of [["an unknown", "emergency-v2"], ["an empty", ""], ["a null", null]]) {
    cases.push(
      [`${kind} emergency contract`, emergencyReply(value)],
      [`${kind} emergency contract in the simulation`, simulation(emergency, { emergencyContract: value })],
      [`${kind} emergency contract in the topology`, { ...emergency, topology: { incidentContract: "incident-v1", emergencyContract: value } }],
      [`${kind} emergency contract in the topology of an unmarked reply`, { ...incident, topology: { incidentContract: "incident-v1", emergencyContract: value } }],
      [`${kind} emergency contract in the simulation of an unmarked reply`, simulation(incident, { emergencyContract: value })],
    );
  }
  return cases;
}

test("readState accepts the reply of each project kind and gives its state", async () => {
  const state = { epoch: "epoch-1", serverStart: "start-1", projectRevision: 3, generation: 5, simulation: { paused: false }, orders: "packed" };
  const without = (key) => Object.fromEntries(Object.entries(state).filter(([name]) => name !== key));
  const invalid = "The live state reply is not valid.";
  const unsupported = "The server replied with an unsupported media type.";
  // envelope gives a reply with markers at the root, in the topology and
  // in the simulation, as the server sends it.
  const envelope = (markers = {}) => ({ ...markers, topology: { ...markers }, frame: { state: { ...state, simulation: { ...state.simulation, ...markers } }, routes: [] } });
  const kind = (body) => ({ body, want: body.frame.state });
  const cases = [
    { name: "a plain project", ...kind(envelope()) },
    { name: "an Express project", ...kind(envelope({ orderContract: "express-v1" })) },
    { name: "an incident project", ...kind({ ...envelope(), topology: { incidentContract: "incident-v1" }, frame: { state: { ...state, simulation: { ...state.simulation, incidentContract: "incident-v1" } }, routes: [] } }) },
    { name: "a fault project", ...kind({ ...envelope(), topology: { incidentContract: "incident-v1", faultContract: "fault-v1" }, frame: { state: { ...state, simulation: { ...state.simulation, incidentContract: "incident-v1", faultContract: "fault-v1" } }, routes: [] } }) },
    { name: "an emergency project", ...kind({ ...envelope(), topology: { incidentContract: "incident-v1", emergencyContract: "emergency-v1" }, frame: { state: { ...state, simulation: { ...state.simulation, incidentContract: "incident-v1", emergencyContract: "emergency-v1", emergencies: {} } }, routes: [] } }) },
    { name: "a fault and emergency project", ...kind({ ...envelope(), topology: { incidentContract: "incident-v1", faultContract: "fault-v1", emergencyContract: "emergency-v1" }, frame: { state: { ...state, simulation: { ...state.simulation, incidentContract: "incident-v1", faultContract: "fault-v1", emergencyContract: "emergency-v1" } }, routes: [] } }) },
    { name: "a reply of the depth limit", ...kind({ ...envelope(), topology: nested(editor.MAX_STATE_DEPTH - 1) }) },
    { name: "a reply with an array of the element limit", ...kind({ ...envelope(), topology: { lanes: new Array(editor.MAX_STATE_ELEMENTS).fill(0) } }) },
    ...stateReplyRefusals(envelope).map(([name, body]) => ({ name, body, wantError: invalid })),
    { name: "a plain state without the envelope", body: state, wantError: invalid },
    { name: "an envelope with a null frame", body: { orderContract: "express-v1", frame: null }, wantError: invalid },
    { name: "an envelope with a frame array", body: { orderContract: "express-v1", frame: [state] }, wantError: invalid },
    { name: "an envelope without a state", body: { orderContract: "express-v1", frame: { routes: [] } }, wantError: invalid },
    { name: "an envelope with a state array", body: { orderContract: "express-v1", frame: { state: [] } }, wantError: invalid },
    { name: "an envelope with a revision only", body: { orderContract: "express-v1", frame: { state: { projectRevision: 3 } } }, wantError: invalid },
    { name: "an envelope with a nested error", body: { orderContract: "express-v1", frame: { state: { error: "bad" } } }, wantError: invalid },
    { name: "a reply that is not an object", body: [state], wantError: invalid },
    { name: "no epoch", body: { topology: {}, frame: { state: without("epoch") } }, wantError: invalid },
    { name: "an empty epoch", body: { topology: {}, frame: { state: { ...state, epoch: "" } } }, wantError: invalid },
    { name: "no server start ID", body: { topology: {}, frame: { state: without("serverStart") } }, wantError: invalid },
    { name: "an empty server start ID", body: { topology: {}, frame: { state: { ...state, serverStart: "" } } }, wantError: invalid },
    { name: "a server start ID that is a number", body: { topology: {}, frame: { state: { ...state, serverStart: 1 } } }, wantError: invalid },
    { name: "a revision that is text", body: { topology: {}, frame: { state: { ...state, projectRevision: "3" } } }, wantError: invalid },
    { name: "a negative revision", body: { topology: {}, frame: { state: { ...state, projectRevision: -1 } } }, wantError: invalid },
    { name: "a fractional revision", body: { topology: {}, frame: { state: { ...state, projectRevision: 3.5 } } }, wantError: invalid },
    { name: "no generation", body: { topology: {}, frame: { state: without("generation") } }, wantError: invalid },
    { name: "no simulation", body: { topology: {}, frame: { state: without("simulation") } }, wantError: invalid },
    { name: "a pause flag that is text", body: { topology: {}, frame: { state: { ...state, simulation: { paused: "false" } } } }, wantError: invalid },
    // A server of another version replies with another media type. The
    // editor refuses the reply before it reads the body.
    { name: "the state media type with a parameter", media: "Application/VND.podsim.state-6+json; charset=utf-8", body: { topology: {}, frame: { state, routes: [] } }, want: state },
    { name: "plain JSON", media: "application/json", body: { topology: {}, frame: { state, routes: [] } }, wantError: unsupported },
    { name: "the Express media type", media: "application/vnd.podsim.express-v1+json", body: { orderContract: "express-v1", textEncoding: "order-text-base64-v1", topology: {}, frame: { state, routes: [] } }, wantError: unsupported },
    { name: "an earlier state media type", media: "application/vnd.podsim.state-5+json", body: { topology: {}, frame: { state, routes: [] } }, wantError: unsupported },
    { name: "no media type", media: null, body: { topology: {}, frame: { state, routes: [] } }, wantError: unsupported },
  ];
  for (const item of cases) {
    const accepts = [], starts = [], reads = [];
    const media = item.media === undefined ? editor.LIVE_STATE_ACCEPT : item.media;
    const connection = {
      fetch: async (url, init) => {
        accepts.push(init.headers.Accept);
        return { ok: true, status: 200, headers: new Headers(media === null ? {} : { "Content-Type": media }), json: async () => { reads.push(url); return item.body; } };
      },
      onServerStart: (start) => starts.push(start),
    };
    if (item.wantError) {
      await assert.rejects(editor.readState(connection), { message: item.wantError }, item.name);
      assert.deepEqual(starts, [], `${item.name}: no server start ID before the check`);
      if (item.wantError === unsupported) assert.deepEqual(reads, [], `${item.name}: the body was read`);
    } else {
      assert.deepEqual(await editor.readState(connection), item.want, item.name);
      assert.deepEqual(starts, ["start-1"], item.name);
    }
    assert.deepEqual(accepts, [editor.LIVE_STATE_ACCEPT], item.name);
  }
  // The header names the one state media type. See the Go test
  // TestEditorReadsLiveStateOfEachProjectKind.
  assert.equal(editor.LIVE_STATE_ACCEPT, "application/vnd.podsim.state-6+json");
});

test("a failed apply tells the user to export a draft that the browser does not keep", () => {
  const off = editor.DRAFT_STORE_TEXT.off; const unsaved = editor.DRAFT_UNSAVED_TEXT;
  const stale = { errorCode: "stale_project", pause: "resumed", message: "stale" };
  const session = { errorCode: "session_changed", pause: "not-paused", message: "session" };
  const network = { errorCode: "", pause: "resumed", message: "Failed to fetch" };
  const cases = [
    {
      name: "a kept draft and a conflict", error: stale, note: "",
      wantText: "The live scenario changed. Your draft is safe. The editor resumed the simulation.",
      wantStatus: "Apply conflict. The live scenario changed.",
    },
    {
      name: "no draft store and a conflict", error: stale, note: off,
      wantText: `The live scenario changed. Your draft is safe. The editor resumed the simulation. ${off}`,
      wantStatus: `Apply conflict. The live scenario changed. ${off}`,
    },
    {
      name: "a kept draft and a new session", error: session, note: "",
      wantText: "The server session changed. Your draft is safe. The simulation was not paused.",
      wantStatus: "Apply conflict. The server session changed.",
    },
    {
      name: "unsaved changes and a new session", error: session, note: unsaved,
      wantText: `The server session changed. Your draft is safe. The simulation was not paused. ${unsaved}`,
      wantStatus: `Apply conflict. The server session changed. ${unsaved}`,
    },
    {
      name: "unsaved changes and a network error", error: network, note: unsaved,
      wantText: `Apply failed. Failed to fetch. The editor resumed the simulation. ${unsaved}`,
      wantStatus: `Apply failed. The draft stays on this page. ${unsaved}`,
    },
  ];
  for (const item of cases) {
    assert.equal(editor.applyFailureText(item.error, item.note), item.wantText, item.name);
    assert.equal(editor.applyFailureStatus(item.error, item.note), item.wantStatus, item.name);
    assert.doesNotMatch(`${editor.applyFailureText(item.error, item.note)} ${editor.applyFailureStatus(item.error, item.note)}`, /reload/i, item.name);
  }
});

test("an empty scenario and the fallback draft have the default settings", () => {
  const cases = [
    { name: "empty scenario", config: editor.emptyConfig },
    { name: "fallback draft", config: editor.fallbackConfig },
  ];
  for (const tc of cases) {
    const config = tc.config();
    assert.equal(config.sharedRidePartyLimit, 1, tc.name);
    assert.deepEqual([config.sharedRideMode, config.sharedRideMaxStops], ["drop-offs", 3], tc.name);
    assert.equal(config.platoonLimit, 0, tc.name);
  }
});

// cssRules gives the declarations of each rule in a style sheet, by
// selector. A rule with a selector list adds its declarations to each
// selector. A background-color declaration is stored as background, so
// that the later rule in cascade order sets the background. The parse
// removes comments first. It does not know @media blocks, so use it only
// for selectors that no @media block sets.
function cssRules(css) {
  const rules = new Map();
  for (const [, selectors, body] of css.replace(/\/\*[\s\S]*?\*\//g, "").matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    const declarations = Object.fromEntries([...body.matchAll(/([\w-]+)\s*:\s*([^;]+)/g)].map(([, name, value]) => [name === "background-color" ? "background" : name, value.trim()]));
    for (const selector of selectors.split(",").map((item) => item.trim())) rules.set(selector, { ...rules.get(selector), ...declarations });
  }
  return rules;
}

// contrastRatio gives the WCAG 2 contrast ratio of two #rrggbb colors.
function contrastRatio(first, second) {
  const luminance = (hex) => {
    const [r, g, b] = [1, 3, 5].map((start) => parseInt(hex.slice(start, start + 2), 16) / 255).map((c) => (c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4));
    return 0.2126 * r + 0.7152 * g + 0.0722 * b;
  };
  const [light, dark] = [luminance(first), luminance(second)].sort((a, b) => b - a);
  return (light + 0.05) / (dark + 0.05);
}

test("the Pause and apply text has 4.5:1 contrast or more in each state", () => {
  const rules = cssRules(fs.readFileSync(path.join(__dirname, "editor.css"), "utf8"));
  const root = rules.get(":root");
  const color = (value) => value.replace(/^var\((--[\w-]+)\)$/, (_, name) => root[name]);
  // Each state gives the rules that set the button colors, in cascade order.
  const states = [
    { name: "ready", rules: ["button", "button.primary"] },
    { name: "hover", rules: ["button", "button:hover", "button.primary", "button.primary:hover"] },
    { name: "busy", rules: ["button", "button:disabled", "button.primary", "button.primary:disabled"] },
  ];
  for (const state of states) {
    const style = Object.assign({}, ...state.rules.map((selector) => rules.get(selector)));
    const [text, background] = [color(style.color), color(style.background)];
    assert.equal(Number(style.opacity ?? 1), 1, `${state.name}: opacity lowers the contrast`);
    const ratio = contrastRatio(text, background);
    assert.ok(ratio >= 4.5, `${state.name}: ${text} on ${background} is ${ratio.toFixed(2)}:1`);
  }
});

test("the file input labels have the font and spacing of the other buttons", () => {
  const rules = cssRules(fs.readFileSync(path.join(__dirname, "editor.css"), "utf8"));
  const [label, fileButton, button] = ["label", ".button", "button"].map((selector) => rules.get(selector));
  // Each property gives the declarations that can set it: its own name or a shorthand.
  const sources = { "font-size": ["font-size", "font"], "line-height": ["line-height", "font"], "font-weight": ["font-weight", "font"], margin: ["margin"], padding: ["padding"] };
  for (const [property, names] of Object.entries(sources)) {
    const value = (style) => names.map((name) => style[name]).find((item) => item !== undefined);
    if (value(label) !== undefined) assert.notEqual(value(fileButton), undefined, `.button keeps the ${property} of the label rule`);
    assert.equal(value(fileButton), value(button), `${property} of .button is not the same as for button`);
  }
  // cssRules does not apply specificity, so no other rule for the labels can set these values again.
  for (const [selector, style] of rules) {
    if (selector === "label" || selector === ".button" || !/(^|[\s>+~])(label\.button|\.button|label)$/.test(selector)) continue;
    for (const names of Object.values(sources)) assert.ok(names.every((name) => style[name] === undefined), `${selector} sets the font or spacing of the file labels`);
  }
});

test("the focus ring of the map shows inside the map panel", () => {
  const rules = cssRules(fs.readFileSync(path.join(__dirname, "editor.css"), "utf8"));
  // The map panel clips the map, so a ring outside the map does not show.
  assert.equal(rules.get(".map-panel").overflow, "hidden");
  const ring = { ...rules.get("svg:focus-visible"), ...rules.get("#networkMap:focus-visible") };
  const width = parseFloat(ring.outline); const offset = parseFloat(ring["outline-offset"]);
  assert.ok(width > 0 && offset + width <= 0, `a ring of ${width} px at an offset of ${offset} px goes outside the map`);
});

// draftChannelHub gives channels that deliver each message to the other
// channels of the hub, as a BroadcastChannel with one name does.
function draftChannelHub() {
  const listeners = [];
  return () => {
    const channel = {
      postMessage: (data) => {
        for (const entry of listeners) if (entry.channel !== channel) queueMicrotask(() => entry.listener({ data: structuredClone(data) }));
      },
      addEventListener: (type, listener) => { if (type === "message") listeners.push({ channel, listener }); },
    };
    return channel;
  };
}

test("a tab knows when another tab replaced or deleted its saved draft", async (t) => {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  const store = fakeDraftStore(); const channel = draftChannelHub();
  const tab = (name, key = DRAFT_KEY) => {
    const draft = { record: null }; const displaced = [];
    const keeper = editor.createDraftKeeper({
      store, key, delay: editor.DRAFT_SAVE_DELAY, clock: globalThis, channel: channel(),
      snapshot: () => (draft.record ? structuredClone(draft.record) : null), onDisplaced: () => displaced.push(name),
    });
    return { keeper, draft, displaced };
  };
  const settle = () => new Promise((resolve) => setImmediate(resolve));
  const saved = () => store.records.get(DRAFT_KEY)?.scenario.name ?? null;
  const a = tab("a"); const b = tab("b"); const other = tab("other", "http://other.test");
  await a.keeper.load(); await b.keeper.load(); await other.keeper.load();

  a.draft.record = savedRecord("tab A"); await a.keeper.arm(); await settle();
  assert.equal(saved(), "tab A");
  assert.equal(a.keeper.unsaved, false, "a tab does not hear its own write");
  assert.deepEqual(a.displaced, []);

  b.draft.record = savedRecord("tab B"); await b.keeper.arm(); await settle();
  assert.equal(saved(), "tab B");
  assert.equal(a.keeper.unsaved, true, "the write of tab B replaced the draft of tab A");
  assert.deepEqual(a.displaced, ["a"]);
  assert.equal(b.keeper.unsaved, false);

  await a.keeper.flush(); await settle();
  assert.equal(saved(), "tab A", "the next flush of tab A writes its draft again");
  assert.equal(a.keeper.unsaved, false);
  assert.equal(b.keeper.unsaved, true, "then tab B knows that its draft is not saved");

  // An apply makes the draft of tab B equal to the live scenario, then clears the record.
  b.draft.record = null; await b.keeper.replace(); await settle();
  assert.equal(saved(), null, "an apply in tab B deletes the record");
  assert.equal(a.keeper.unsaved, true, "tab A knows that the delete removed its draft");
  assert.deepEqual(a.displaced, ["a", "a"]);

  other.draft.record = savedRecord("other server"); await other.keeper.arm(); await settle();
  assert.deepEqual(a.displaced, ["a", "a"], "a write for another server does not change tab A");
  assert.deepEqual(b.displaced, ["b", "b"], "tab B heard both writes of tab A, also the first one before it had a draft");
  assert.equal(b.keeper.unsaved, false, "tab B has no draft after its apply, so it has nothing to lose");
});

test("shellPage finds only a same-origin shell page as the parent", () => {
  const top = { podsimShell: true }; top.parent = top;
  const shell = { podsimShell: true };
  const blocked = Object.defineProperty({}, "podsimShell", { get() { throw new Error("SecurityError: Blocked a frame from accessing a cross-origin frame."); } });
  for (const [name, win, want] of [
    ["top page", top, null],
    ["shell page", { parent: shell }, shell],
    ["other page", { parent: {} }, null],
    ["marker that is not true", { parent: { podsimShell: "yes" } }, null],
    ["other origin", { parent: blocked }, null],
  ]) {
    assert.equal(editor.shellPage(win), want, name);
  }
});

// OSM_LICENSE is the license facts of a test image.
const OSM_LICENSE = {
  source: "OpenStreetMap", attribution: "© OpenStreetMap contributors", license: "ODbL-1.0", licenseURL: "https://opendatacommons.org/licenses/odbl/1-0/",
  copyrightURL: "https://www.openstreetmap.org/copyright", retrieved: "2026-09-28T12:00:00.000Z", method: "user supplied", notice: "Keep this notice.",
};

// LONDON_FRAME is a frame of about 1.4 by 1.1 km near the reference of
// LONDON_GEO.
const LONDON_FRAME = { south: 51.5, north: 51.51, west: -0.13, east: -0.11, source: "equirectangular" };

// imageSeed makes the image keys of testImage.
let imageSeed = 0;

// testImage gives a frozen image descriptor with a new key. options can
// give bytes, size, the byte count of the zero bytes, frame and license.
function testImage(options = {}) {
  imageSeed += 1;
  const bytes = options.bytes ?? new ArrayBuffer(options.size ?? 16);
  return editor.freezeImage({ key: imageSeed.toString(16).padStart(32, "0"), bytes, mime: "image/png", pixelWidth: 4, pixelHeight: 2, frame: options.frame ?? null, license: options.license ?? null });
}

// fakeGoSide gives the call and accept functions of a Go history with a
// fake Go side. Go keeps a list of entries and a head. The fake prepares
// each step at once, and accepts the proposal that it prepared last. As in
// Go, a replace or a commit with the value of the head entry changes
// nothing, and a commit must start from the head entry.
// entries gives the accepted entries, with the background that the page
// sent for each entry.
function fakeGoSide() {
  let entries = [], head = -1, revision = 0, ids = 0, proposal = null;
  const view = (list, at, changed) => ({
    head: list[at].id, revision: String(revision), changed, canUndo: at > 0, canRedo: at < list.length - 1,
    retained: list.map((entry) => entry.id), imageKeys: [...new Set(list.map((entry) => entry.background?.imageKey).filter(Boolean))], background: list[at].background,
  });
  return {
    get entries() { return entries; },
    async call(config, op, command) {
      assert.equal(op, "history");
      if (command.action === "discard") { proposal = null; return {}; }
      let list = entries, at = head, changed = true;
      const value = JSON.stringify([config, command.background ?? null]);
      const entry = () => ({ id: `s${++ids}`, value, background: structuredClone(command.background ?? null) });
      if (command.kind === "commitFrom" && command.before !== entries[head]?.id) return { error: "the history gesture has a stale starting snapshot" };
      if (command.kind === "reset") { list = [entry()]; at = 0; }
      else if ((command.kind === "replace" || command.kind === "commitFrom") && entries[head]?.value === value) changed = false;
      else if (command.kind === "replace" && command.record === false && head >= 0) { list = [...entries.slice(0, head), entry()]; at = head; }
      else if (command.kind === "replace" || command.kind === "commitFrom") { list = [...entries.slice(0, head + 1), entry()]; at = list.length - 1; }
      else if (command.kind === "undo" || command.kind === "redo") {
        // An undo with no past and a redo with no future change nothing.
        at = head + (command.kind === "undo" ? -1 : 1);
        if (!entries[at]) { at = head; changed = false; }
      } else assert.fail(`the fake Go history has no ${command.kind} step`);
      if (command.trimOldest) { list = list.slice(command.trimOldest); at -= command.trimOldest; }
      revision += 1;
      proposal = { list, at, view: view(list, at, changed), token: `p${revision}` };
      return { history: { ...proposal.view, proposal: proposal.token } };
    },
    async accept(_config, token) {
      assert.equal(token, proposal?.token, "the page accepts the last proposal");
      ({ list: entries, at: head } = proposal);
      const accepted = proposal.view; proposal = null;
      return { history: accepted };
    },
  };
}

// ownTestDraft gives a frozen copy of a draft, as the history of the page
// keeps it.
function ownTestDraft(value, previous) {
  const freeze = (item) => { if (item && typeof item === "object") { for (const child of Object.values(item)) freeze(child); Object.freeze(item); } return item; };
  return Object.is(value, previous) ? previous : freeze(structuredClone(value));
}

// testModel gives the background model of the page with the Go history
// client and a fake Go side. The initial draft is options.initial, or the
// scenario options.config, or the connected scenario, with no background.
// options.onChange gets each draft change, as the page gets it.
// options.wrapCall can wrap the Go history call, to hold a step. goEntries
// gives the entries of its fake Go side.
function testModel(options = {}) {
  const { createHistory } = require("./editor-model.js");
  const side = fakeGoSide();
  const model = editor.createGoBackgroundModel({
    initial: options.initial ?? { scenario: options.config ?? connectedScenario(), background: null }, cap: options.cap, onChange: options.onChange,
    historyFactory: (initial, onChange, onAcknowledged) => createHistory({ initial, own: ownTestDraft, clone: structuredClone, onChange, onAcknowledged, call: options.wrapCall ? options.wrapCall(side.call) : side.call, accept: side.accept }),
  });
  model.goEntries = () => side.entries;
  return model;
}

// readyModel gives testModel after the reset of its initial draft, as the
// page starts its history.
async function readyModel(options = {}) {
  const model = testModel(options);
  await model.history.reset(model.history.value);
  return model;
}

// placed gives a history background of image.
function placed(image, options = {}) {
  return { imageKey: image.key, x: 0, y: 0, width: 40, height: 20, opacity: 0.45, frameState: image.frame ? "attached" : "none", ...options };
}

// publishImage publishes image with the scenario of model, as an
// acquisition does. options can set baseline and value.
function publishImage(model, image, options = {}) {
  const ticket = model.start();
  return model.publish({ ticket, image, value: options.value ?? { scenario: model.history.value.scenario, background: placed(image) }, baseline: options.baseline });
}

// assertTable checks the image table invariants of model after step:
// each key of the history, the baseline and pinned resolves to a
// descriptor, the table holds no other key, each key keeps its first
// descriptor, seen maps each key to that descriptor, and no Go history
// entry or baseline holds binary data or a data URL.
function assertTable(model, step, seen, pinned = []) {
  const keys = model.history.keys(); const base = model.loaded.background;
  if (base) keys.add(base.imageKey);
  for (const key of pinned) keys.add(key);
  assert.deepEqual(new Set(model.keys()), keys, `${step}: the table holds each referenced key and no other key`);
  for (const key of keys) {
    const image = model.image(key);
    assert.ok(image && Object.isFrozen(image), `${step}: key ${key} resolves to a frozen descriptor`);
    if (!seen.has(key)) seen.set(key, image);
    assert.equal(image, seen.get(key), `${step}: the descriptor of ${key} does not change`);
  }
  for (const background of [...model.goEntries().map((entry) => entry.background), model.loaded.background]) {
    const text = JSON.stringify(background);
    assert.ok(!/bytes|dataURL|"image"/.test(text ?? ""), `${step}: the entry holds only the image key: ${text}`);
  }
}

test("each draft change schedules the checks, also an undo and a redo", async (t) => {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  const named = (name) => ({ scenario: { ...connectedScenario(), name }, background: null });
  const seen = [];
  let model = null;
  const checks = editor.createCheckTimer({ delay: editor.CHECK_DELAY, clock: globalThis, run: () => seen.push(model.history.value.scenario.name) });
  // As in the page, a draft change schedules the checks unless the change
  // keeps their result.
  model = testModel({ initial: named("A"), onChange: (checksUnchanged) => { if (!checksUnchanged) checks.schedule(); } });
  // settle waits for the change, ends its task, and waits for the checks.
  const settle = async (change) => { await change; t.mock.timers.tick(0); t.mock.timers.tick(editor.CHECK_DELAY); };
  await settle(model.history.reset(named("A")));
  seen.length = 0;
  // want is the draft name that the checks see after the change, or null
  // when the change does not change the draft and the checks do not run.
  const cases = [
    { name: "replace", change: () => model.history.replace(named("B")), want: "B" },
    { name: "replace with the same draft", change: () => model.history.replace(named("B")), want: null },
    { name: "undo", change: () => model.history.undo(), want: "A" },
    { name: "undo with no past", change: () => model.history.undo(), want: null },
    { name: "redo", change: () => model.history.redo(), want: "B" },
    { name: "redo with no future", change: () => model.history.redo(), want: null },
    { name: "replace without a record", change: () => model.history.replace(named("C"), false), want: "C" },
    { name: "commit of a coalesced change", change: () => model.history.commitFrom(model.history.snapshot, named("D")), want: "D" },
    { name: "a change that keeps the checks", change: () => model.history.replace(named("D2"), true, true), want: null },
    { name: "reset", change: () => model.history.reset(named("E")), want: "E" },
  ];
  for (const item of cases) {
    const before = seen.length;
    await settle(item.change());
    assert.deepEqual(seen.slice(before), item.want === null ? [] : [item.want], item.name);
  }

  // A change and undo and redo in fast series give one run, for the last
  // draft.
  const series = [model.history.replace(named("F")), model.history.undo(), model.history.redo(), model.history.undo()];
  await settle(Promise.all(series));
  assert.deepEqual(seen.slice(-1), ["E"]);
  assert.equal(seen.length, 7);
});

test("the frame lifecycle keeps the image, the placement, the frame state and the geo reference in one history step", async () => {
  const config = { ...connectedScenario(), geo: LONDON_GEO };
  const model = await readyModel({ config });
  const image = testImage({ frame: LONDON_FRAME, license: OSM_LICENSE });
  // framed gives value with the image at its frame in geo, as the Go place
  // proposal gives it. Go tests the reference choice of the proposal.
  const framed = (value, geo) => ({
    scenario: { ...value.scenario, geo },
    background: { imageKey: image.key, ...editor.framePlacement(LONDON_FRAME, geo), opacity: value.background?.opacity ?? editor.DEFAULT_OPACITY, frameState: "attached" },
  });
  await publishImage(model, image, { value: framed(model.history.value, LONDON_GEO) });
  const view = () => editor.frameView(model.history.value, model.image(image.key));
  assert.deepEqual(view(), { framed: true, state: "attached", warning: "", placeReason: "", calibrateReason: "Detach the frame before you calibrate the scale." });
  // Load live with a different reference keeps the placement and shows
  // the warning.
  const placement = model.history.background;
  const other = geoAt(51.52, -0.12);
  await model.history.replace({ scenario: { ...config, geo: other }, background: placement });
  assert.equal(view().warning, editor.FRAME_WARNING_TEXT);
  // Load live with no reference also shows it, and Place from frame needs
  // a reference choice.
  await model.history.replace({ scenario: connectedScenario(), background: placement });
  assert.deepEqual([view().warning, view().placeReason.length > 0, view().calibrateReason.length > 0], [editor.FRAME_WARNING_TEXT, true, true]);
  // Place from frame with a reference is one step with the new geo.
  await model.history.replace({ scenario: { ...config, geo: other }, background: placement });
  await model.history.replace(framed(model.history.value, other));
  assert.deepEqual([view().warning, model.history.value.scenario.geo], ["", other]);
  await model.history.undo();
  assert.deepEqual([view().warning, model.history.background], [editor.FRAME_WARNING_TEXT, placement], "one undo step brings back the placement");
  // Detach frame keeps the frame and the license of the image.
  await model.history.redo();
  await model.history.replace({ scenario: model.history.value.scenario, background: { ...model.history.background, frameState: "detached" } });
  assert.deepEqual(view(), { framed: true, state: "detached", warning: "", placeReason: "", calibrateReason: "" });
  assert.deepEqual([model.image(image.key).frame, model.image(image.key).license], [LONDON_FRAME, OSM_LICENSE]);
  // Remove background keeps the image while an undo step refers to it.
  await model.history.replace({ scenario: model.history.value.scenario, background: null });
  assert.equal(editor.frameView(model.history.value, null).framed, false);
  assert.ok(model.image(image.key));
  await model.history.undo();
  assert.equal(view().state, "detached", "an undo of the remove brings back the detached frame");
});

test("the image table keeps each referenced image through imports, undo, redo, reset, loads, restores and removes", async () => {
  const model = await readyModel(); const seen = new Map(); const scenario = () => model.history.value.scenario;
  const background = () => model.history.background;
  const steps = [
    ["import X", () => publishImage(model, testImage())],
    ["move X", () => model.history.replace({ scenario: scenario(), background: { ...background(), x: 25 } })],
    ["import framed Y", () => publishImage(model, testImage({ frame: LONDON_FRAME, license: OSM_LICENSE }))],
    ["undo", () => model.history.undo()],
    ["undo", () => model.history.undo()],
    ["redo", () => model.history.redo()],
    ["project import Z", () => publishImage(model, testImage(), { baseline: true })],
    ["remove Z", () => model.history.replace({ scenario: scenario(), background: null })],
    ["Reset draft", () => model.history.replace(model.loaded)],
    ["import W", () => publishImage(model, testImage())],
    ["Load live", () => {
      const live = { ...scenario(), name: "Live" };
      const { value, loaded } = editor.liveDraft({ revision: 4, epoch: "e", serverStart: "s", project: live }, { loaded: model.loaded, background: background() });
      model.setLoaded(loaded); return model.history.replace(value);
    }],
    ["Restore draft", () => model.history.reset({ scenario: { ...scenario(), name: "Saved" }, background: background() })],
    ["project import with no background", () => publishImage(model, null, { baseline: true, value: { scenario: connectedScenario(), background: null } })],
    ["undo", () => model.history.undo()],
  ];
  for (const [name, run] of steps) { await run(); assertTable(model, name, seen); }
  assert.ok(seen.size >= 4, "the sequence made four images");
  // Restore draft started a new history, so only the present image and
  // the baseline image stay.
  await model.history.reset({ scenario: scenario(), background: null });
  assertTable(model, "a new history", seen);
  assert.equal(model.keys().length, 0, "the Load live baseline had no background after the project import");
});

test("the image limit drops the oldest undo steps, keeps the present and the baseline images, and the admission check takes the cap", async () => {
  const eight = 8 * 1024 * 1024;
  const model = await readyModel();
  const base = testImage({ size: eight });
  assert.equal((await publishImage(model, base, { baseline: true })).dropped, 0);
  const counts = [];
  for (let index = 0; index < 18; index += 1) counts.push((await publishImage(model, testImage({ size: eight }))).dropped);
  // The baseline and the 15 images after it fill 128 MiB. The 16th image
  // drops the first step with no image, the step with the baseline image,
  // which the baseline keeps, and the step with the first image after it.
  // Then each new image drops one step.
  assert.deepEqual(counts, [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 3, 1, 1]);
  assert.ok(model.bytes <= editor.IMAGE_TABLE_BYTES, `${model.bytes} bytes`);
  assert.ok(model.image(base.key), "the baseline image stays");
  assert.ok(model.image(model.history.background.imageKey), "the present image stays");
  assert.equal(model.bytes, 16 * eight);

  // With a cap of 20 MiB, the present image, the baseline image and a
  // new image of 8 MiB do not fit, so the publish fails and keeps the
  // background.
  const small = await readyModel({ cap: 20 * 1024 * 1024 });
  const first = testImage({ size: eight });
  await publishImage(small, first, { baseline: true });
  const second = testImage({ size: eight });
  assert.equal((await publishImage(small, second)).error, "");
  assert.equal(small.history.canUndo, true);
  // The next publish drops the undo steps, but the admission check
  // counts the present and the baseline images, and the pinned image.
  small.pin(second.key);
  const result = await publishImage(small, testImage({ size: eight }));
  assert.match(result.error, /does not fit in the 20\.0 MiB image limit of this tab\. The images that the tab must keep use 16\.0 MiB, with 8\.0 MiB for writes to the browser store\./);
  assert.equal(small.history.background.imageKey, second.key, "the background does not change");
});

test("a keeper write and an export that start before a prune keep their bytes", async () => {
  const store = heldStore(); const model = await readyModel(); const commits = [];
  const keeper = editor.createDraftKeeper({
    store, key: DRAFT_KEY, delay: editor.DRAFT_SAVE_DELAY, clock: globalThis, deletes: true, holdUntilChange: true, text: editor.backgroundRecordText,
    snapshot: () => { const background = model.history.background; return background ? editor.backgroundRecordFor(background, model.image(background.imageKey)) : null; },
    onQueued: (record) => { if (record) model.pin(record.background.imageKey); },
    onSettled: (record, committed) => { if (record) model.unpin(record.background.imageKey); commits.push(committed); },
  });
  await keeper.load(); await keeper.arm();
  const bytes = editor.dataURLToBytes(dataURL(pngBytes(4, 2)));
  const image = testImage({ bytes });
  await publishImage(model, image);
  const exported = editor.serializeDocument(model.history.value.scenario, editor.exportBackground(model.history.background, model.image(image.key)));
  const write = keeper.flush(); await tick();
  assert.equal(model.pinCount(image.key), 1, "the queued write pins the image");
  // Load live with no background drops each history and baseline
  // reference to the image.
  model.setLoaded({ scenario: connectedScenario(), background: null }); await model.history.reset({ scenario: connectedScenario(), background: null });
  assert.ok(model.image(image.key), "the pinned image stays in the table");
  store.next(); await write;
  assert.deepEqual([model.pinCount(image.key), model.image(image.key), commits], [0, null, [true]], "the settled write releases the image");
  assert.deepEqual(new Uint8Array(store.records.get(DRAFT_KEY).background.image.bytes), new Uint8Array(bytes), "the store has the bytes");
  assert.equal(editor.parseDocument(exported).background.dataURL, dataURL(pngBytes(4, 2)), "the export has the bytes");
});

test("an abort, a newer acquisition or an edit drops the result of an acquisition", async () => {
  const model = await readyModel();
  // drop runs publish, which must fail with error and keep the draft, the
  // Go history and the image table.
  const drop = async (name, error, image, publish) => {
    const value = model.history.value; const entries = model.goEntries().length; const keys = model.keys();
    assert.deepEqual(await publish(), { error, dropped: 0 }, name);
    assert.deepEqual([model.history.value, model.goEntries().length, model.keys()], [value, entries, keys], `${name}: the draft, the history and the table do not change`);
    assert.equal(model.image(image.key), null, `${name}: the dropped image is not in the table`);
  };
  const request = (ticket, image) => ({ ticket, image, value: { scenario: model.history.value.scenario, background: placed(image) } });
  // An abort event.
  let ticket = model.start(); model.abort();
  assert.equal(ticket.signal.aborted, true);
  let image = testImage();
  await drop("an abort", "A newer action stopped the import.", image, () => model.publish(request(ticket, image)));
  // A new acquisition aborts the old one, and the new one still publishes.
  const older = model.start(); ticket = model.start();
  assert.deepEqual([older.signal.aborted, ticket.signal.aborted], [true, false], "a new acquisition aborts the old one");
  image = testImage();
  await drop("an older acquisition", "A newer action stopped the import.", image, () => model.publish(request(older, image)));
  image = testImage();
  assert.equal((await model.publish(request(ticket, image))).error, "", "the newer acquisition publishes");
  assert.ok(model.image(image.key));
  // Each check stops a publish alone: a current ticket whose own signal is
  // aborted, and a ticket of no acquisition with a live signal.
  ticket = model.start(); ticket.controller.abort(); image = testImage();
  await drop("an aborted current ticket", "A newer action stopped the import.", image, () => model.publish(request(ticket, image)));
  const foreign = { signal: new AbortController().signal, edits: model.edits };
  image = testImage();
  await drop("a ticket of no acquisition", "A newer action stopped the import.", image, () => model.publish(request(foreign, image)));
  // A node move after the start of the acquisition drops the result. The
  // Go place proposal checks the anchors, so the result has the draft of
  // the start.
  ticket = model.start(); image = testImage({ frame: LONDON_FRAME });
  const stale = request(ticket, image);
  const moved = model.history.value.scenario; moved.network.nodes[0].position = { x: 5, y: 0 };
  await model.history.replace({ scenario: moved, background: model.history.background });
  await drop("a node move", "The draft changed during the import.", image, () => model.publish(stale));
});

test("an abort while the Go history prepares a publish drops the result", async () => {
  // The Go history is asynchronous. The prepare step of the publish is held
  // until the abort, so the abort comes after the publish checks its ticket.
  let held = null;
  const wrapCall = (call) => async (config, op, command) => {
    if (held && command.action === "prepare") { const step = held; held = null; step.started(); await step.release; }
    return call(config, op, command);
  };
  const model = await readyModel({ wrapCall });
  let started, release;
  const startedStep = new Promise((resolve) => { started = resolve; });
  held = { started, release: new Promise((resolve) => { release = resolve; }) };
  const value = model.history.value; const entries = model.goEntries().length; const keys = model.keys();
  const ticket = model.start(); const image = testImage();
  const pending = model.publish({ ticket, image, value: { scenario: model.history.value.scenario, background: placed(image) } });
  await startedStep;
  model.abort(); release();
  assert.deepEqual(await pending, { error: "A newer action stopped the import.", dropped: 0 });
  assert.deepEqual([model.history.value, model.goEntries().length, model.keys()], [value, entries, keys], "the draft, the history and the table do not change");
  assert.equal(model.image(image.key), null, "the dropped image is not in the table");
});

test("a scenario edit during the decode of a project import fails the import and keeps the draft", async () => {
  const model = await readyModel(); const slot = editor.createDecoderSlot();
  const baseline = model.loaded;
  let finish;
  const deps = { decode: () => new Promise((resolve) => { finish = () => resolve({ width: 4, height: 2, close() {} }); }) };
  const ticket = model.start();
  const bytes = editor.dataURLToBytes(dataURL(pngBytes(4, 2)));
  const decoded = slot.run((signal) => editor.checkImageBytes(bytes, deps, signal), ticket.signal);
  await tick();
  const edited = model.history.value.scenario; edited.name = "Edited"; await model.history.replace({ scenario: edited, background: null });
  finish(); const facts = await decoded;
  const image = editor.freezeImage({ key: TEST_KEY, bytes, mime: facts.mime, pixelWidth: facts.width, pixelHeight: facts.height, frame: null, license: null });
  const result = await model.publish({ ticket, image, value: { scenario: connectedScenario(), background: placed(image) }, baseline: true });
  assert.equal(result.error, "The draft changed during the import.");
  assert.deepEqual([model.history.value.scenario.name, model.loaded, model.keys()], ["Edited", baseline, []]);
});

test("a drag that moved blocks a publish until its pointer up, and a drag with no move does not", async () => {
  const model = await readyModel();
  // An acquisition starts, then a drag begins and moves.
  let ticket = model.start();
  model.moveDrag();
  assert.match((await model.publish({ ticket, image: testImage(), value: model.history.value })).error, /changed during the import/);
  // An acquisition and an Import JSON that start and end while the moved
  // drag is open fail with the reason.
  for (const baseline of [false, true]) {
    ticket = model.start();
    assert.match((await model.publish({ ticket, image: testImage(), value: model.history.value, baseline })).error, /A drag or an opacity change was open/);
  }
  // The pointer up records the move.
  const moved = model.history.value.scenario; moved.network.nodes[0].position = { x: 1, y: 2 };
  model.endDrag(); await model.history.replace({ scenario: moved, background: null });
  assert.deepEqual(model.history.value.scenario.network.nodes[0].position, { x: 1, y: 2 });
  // A drag with no move does not block a publish.
  ticket = model.start(); const image = testImage();
  assert.equal((await model.publish({ ticket, image, value: { scenario: moved, background: placed(image) } })).error, "");
});

test("an opacity gesture starts again after a history change that it did not make", async () => {
  const model = await readyModel();
  const w = testImage(); const x = testImage();
  await publishImage(model, w); await publishImage(model, x);
  model.pressOpacity();
  await model.history.undo();
  const edited = model.history.value.scenario; edited.name = "Edited";
  await model.history.replace({ scenario: edited, background: model.history.background });
  assert.equal(model.setOpacity(0.8), true);
  assert.equal(await model.endOpacity(), true);
  assert.equal(model.history.keys().has(x.key), false, "no history entry holds X");
  assert.equal(model.image(x.key), null, "the table has no X");
  assert.deepEqual([model.history.background.imageKey, model.history.background.opacity], [w.key, 0.8]);
  await model.history.undo();
  assert.deepEqual([model.history.background.opacity, model.history.value.scenario.name], [0.45, "Edited"], "one undo step changes the opacity of W");
  assert.equal(await model.endOpacity(), false, "a second pointer up with no press changes nothing");

  // With no image before X, the slider move and the pointer up change
  // nothing.
  const lone = await readyModel(); await publishImage(lone, testImage());
  lone.pressOpacity(); await lone.history.undo();
  const renamed = lone.history.value.scenario; renamed.name = "Renamed";
  await lone.history.replace({ scenario: renamed, background: null });
  const before = JSON.stringify(lone.history.value);
  assert.deepEqual([lone.setOpacity(0.8), await lone.endOpacity(), JSON.stringify(lone.history.value)], [false, false, before]);
  assert.equal(lone.keys().length, 0);
});

test("pointercancel and a lost pointer capture end the opacity gesture as a pointer up does", async () => {
  for (const end of ["pointercancel", "lostpointercapture"]) {
    const model = await readyModel(); await publishImage(model, testImage());
    model.pressOpacity(); model.setOpacity(0.7);
    assert.equal(model.gestureChanged, true, `${end}: the gesture changed`);
    assert.match((await publishImage(model, testImage())).error, /opacity change was open/, `${end}: an open gesture blocks a publish`);
    assert.equal(await model.endOpacity(), true, `${end}: the change is one undo step`);
    assert.deepEqual([model.gestureOpen, model.gestureChanged], [false, false], `${end}: the gesture clears`);
    assert.equal((await publishImage(model, testImage())).error, "", `${end}: an import at once succeeds`);
    assert.equal(await model.endOpacity(), false, `${end}: a later pointer up changes nothing`);
    await model.history.undo(); await model.history.undo();
    assert.equal(model.history.background.opacity, 0.45, `${end}: the opacity step is one undo step`);
  }
  // The page ends the gesture at each of the three events.
  const source = fs.readFileSync(path.join(__dirname, "editor.js"), "utf8");
  assert.match(source, /for \(const name of \["pointerup", "pointercancel", "lostpointercapture"\]\) \$\("#backgroundOpacity"\)\.addEventListener\(name, endOpacity\);/);
});

// fakeDecoder gives decoder functions for the decoder slot that wait for
// the test and record each call in log. finish ends the oldest waiting
// decode with a bitmap of width by height pixels, and encoded ends the
// waiting encode with the bytes of a PNG. draws lists the drawImage calls.
function fakeDecoder() {
  const log = []; const decodes = []; const encodes = []; const draws = []; let bitmaps = 0;
  return {
    log, draws,
    decoding: () => decodes.length,
    finish: (width, height) => { const item = decodes.shift(); item.resolve({ width, height, close() { log.push(`close ${item.id}`); } }); },
    encoded: (bytes) => encodes.shift()(new Blob([bytes])),
    deps: {
      decode: (blob) => {
        bitmaps += 1; const id = bitmaps;
        assert.ok(blob instanceof Blob, "the decode gets a Blob");
        log.push(`decode ${id}`);
        return new Promise((resolve) => decodes.push({ id, resolve }));
      },
      canvas: (width, height) => {
        log.push(`canvas ${width}x${height}`);
        let size = { width, height };
        return {
          get width() { return size.width; }, set width(value) { if (value === 0 && size.width) log.push("release"); size = { ...size, width: value }; },
          get height() { return size.height; }, set height(value) { size = { ...size, height: value }; },
          getContext: () => ({ drawImage: (...args) => draws.push(args) }),
        };
      },
      encode: () => { log.push("encode"); return new Promise((resolve) => encodes.push(resolve)); },
    },
  };
}

test("the decoder slot runs one operation, replaces the waiting request, and releases each bitmap and canvas", async () => {
  const slot = editor.createDecoderSlot(); const fake = fakeDecoder();
  const frame = { ...LONDON_FRAME, source: "web-mercator" };
  const size = editor.resampleSize(frame, LONDON_GEO, 200);
  const request = { bytes: editor.dataURLToBytes(dataURL(pngBytes(8, 8))), frame, geo: LONDON_GEO, metersPerPixel: 200 };
  const resample = (controller) => slot.run((signal) => editor.resampleImage(request, fake.deps, signal), controller.signal);
  const first = new AbortController(); const firstRun = resample(first); await tick();
  assert.deepEqual(fake.log, ["decode 1"]);
  first.abort();
  const second = resample(new AbortController()); await tick();
  assert.deepEqual([fake.log, slot.busy, slot.waiting], [["decode 1"], true, true], "the second request waits for the aborted decode");
  const third = resample(new AbortController());
  await assert.rejects(second, { name: "AbortError" }, "the third request replaces the second one");
  assert.deepEqual(fake.log, ["decode 1"], "the replaced request has no decode");
  fake.finish(8, 8);
  await assert.rejects(firstRun, { name: "AbortError" });
  await tick();
  assert.deepEqual(fake.log, ["decode 1", "close 1", "decode 2"], "the third decode starts after the first bitmap closes");
  fake.finish(8, 8); await tick();
  assert.deepEqual(fake.log.slice(3), [`canvas ${size.width}x${size.height}`, "close 2", "encode"], "the source closes before the encode");
  assert.equal(fake.draws.length, size.height, "one draw for each output row");
  for (const [row, args] of fake.draws.entries()) {
    assert.equal(args[6], row);
    assert.ok(args[2] >= 0 && args[2] <= 7, `the source row ${args[2]} is in the image`);
  }
  assert.ok(fake.draws[0][2] < fake.draws.at(-1)[2], "the rows go from north to south");
  fake.encoded(pngBytes(size.width, size.height)); await tick(); await tick();
  assert.deepEqual(fake.log.slice(6), ["release", "decode 3"], "the canvas release comes before the decode of the new image");
  fake.finish(size.width, size.height);
  const result = await third;
  assert.deepEqual([result.facts.width, result.facts.height, result.facts.mime], [size.width, size.height, "image/png"]);
  assert.deepEqual(fake.log.slice(8), ["close 3"]);
  // A failed size check closes its bitmap.
  const failed = slot.run((signal) => editor.checkImageBytes(request.bytes, fake.deps, signal)); await tick();
  fake.finish(9, 8);
  await assert.rejects(failed, /does not have the size in its header/);
  const closes = fake.log.filter((item) => item.startsWith("close"));
  assert.deepEqual(closes, ["close 1", "close 2", "close 3", "close 4"], "each bitmap closes once");
  assert.equal(fake.log.filter((item) => item.startsWith("decode")).length, 4, "at most one decode ran at a time");
  // A request that is aborted when it gets the slot gives it back.
  const stopped = new AbortController(); stopped.abort();
  await assert.rejects(slot.run(() => assert.fail("no work runs"), stopped.signal), { name: "AbortError" });
  assert.equal(slot.busy, false);
});

test("an already-aborted decoder request preserves the newer waiting request", async () => {
  const slot = editor.createDecoderSlot(); const held = Promise.withResolvers(); const calls = [];
  const running = slot.run(() => held.promise); await tick();
  const waiting = slot.run(() => { calls.push("newer"); return "newer"; }).catch((error) => error);
  const old = new AbortController(); old.abort();
  let rejected;
  slot.run(() => calls.push("old"), old.signal).catch((error) => { rejected = error; });
  await tick();
  assert.equal(rejected?.name, "AbortError", "the aborted request rejects before the running operation ends");
  assert.equal(slot.waiting, true);
  assert.deepEqual(calls, [], "the running operation still owns the slot");
  held.resolve(); await running;
  assert.equal(await waiting, "newer");
  assert.deepEqual(calls, ["newer"]);
});

// pageImports runs the page import functions with the real model and decoder
// slot. UI stubs record messages without a browser DOM. The Go model accepts
// each project.
function pageImports() {
  const source = fs.readFileSync(path.join(__dirname, "editor.js"), "utf8");
  const names = ["importProject", "importBackground", "importFramedImage", "takeBackgroundBytes", "publishBackground"];
  const functions = names.map((name) => {
    const match = new RegExp(`\\n  (?:async )?function ${name}\\(`).exec(source);
    assert.ok(match, `the page has ${name}`);
    return source.slice(match.index, source.indexOf("\n  }\n", match.index) + 5);
  }).join("\n");
  const model = testModel({ config: { ...connectedScenario(), geo: LONDON_GEO } });
  const slot = editor.createDecoderSlot(); const fake = fakeDecoder(); const messages = []; let queued = 0;
  const controls = Object.fromEntries(Object.entries({
    geoSouth: LONDON_FRAME.south, geoNorth: LONDON_FRAME.north, geoWest: LONDON_FRAME.west, geoEast: LONDON_FRAME.east,
    geoSource: LONDON_FRAME.source, geoLicenseSource: "", geoAttribution: "", geoLicense: "", geoLicenseURL: "", geoCopyrightURL: "", geoNotice: "",
  }).map(([name, value]) => [`#${name}`, { value: String(value) }]));
  controls["#geoFile"] = { files: [] };
  const noop = () => {};
  const deps = {
    ...editor, model, decoder: fake.deps, root: globalThis, MIB: 1024 * 1024, ownDraft: ownTestDraft,
    goModel: { call: async () => ({ valid: true, errors: [] }) },
    normalizeWithGo: async (scenario) => scenario,
    metadataWithGo: async (metadata) => {
      const asset = metadata.asset || { frameState: "none", frame: null, license: null };
      const problem = editor.assetError(asset); if (problem) throw new Error(problem);
      return { ...metadata, asset };
    },
    backgroundProposal: async (config, command) => {
      // The fake places a framed image at its frame in the geo reference of
      // the draft. Go tests the reference choice and the placement checks.
      const opacity = command.background ? command.background.opacity : editor.DEFAULT_OPACITY;
      if (command.action === "place") return { value: { scenario: config, background: { imageKey: command.imageKey, ...editor.framePlacement(command.frame, config.geo), opacity, frameState: "attached" } } };
      assert.equal(command.action, "initialize");
      return { value: { scenario: config, background: { imageKey: command.imageKey, x: 0, y: 0, width: command.width, height: command.height, opacity, frameState: "none" } } };
    },
    slot: { get busy() { return slot.busy; }, run(...args) { queued += 1; return slot.run(...args); } },
    state: { history: model.history, get background() { return model.history.background; }, draftBase: {}, loadedStart: "test" },
    $: (selector) => { assert.ok(controls[selector], selector); return controls[selector]; },
    draft: () => model.history.value.scenario, referenceChoice: () => null,
    toast: (message) => messages.push(message), updateStatus: (message) => messages.push(message),
    render: noop, fitNetwork: noop, cancelPendingEdits: noop, checks: { run: noop }, reportPublish: noop, keptToast: () => ["saved"],
  };
  const imports = new Function(...Object.keys(deps), `${functions}\nreturn { ${names.join(", ")} };`)(...Object.values(deps));
  return { ...imports, model, slot, fake, controls, messages, queued: () => queued };
}

test("out-of-order file reads cannot queue an older import behind a blocked decoder", async (t) => {
  for (const kind of ["importProject", "importBackground", "importFramedImage"]) {
    await t.test(kind, async () => {
      const page = pageImports(); const held = Promise.withResolvers(); const read = Promise.withResolvers();
      const running = page.slot.run(() => held.promise); await tick();
      const bytes = editor.dataURLToBytes(dataURL(pngBytes(4, 2)));
      let reads = 0;
      const readFile = () => { reads += 1; return read.promise; };
      const file = { type: "image/png", size: bytes.byteLength, text: readFile, arrayBuffer: readFile };
      page.controls["#geoFile"].files = [file];
      const older = page[kind](file); await tick();
      assert.equal(reads, 1, "the older import has started its read");
      const newer = page.importBackground({ ...file, arrayBuffer: async () => bytes }); await tick();
      assert.equal(page.queued(), 1, "the newer import waits for the decoder");
      read.resolve(kind === "importProject"
        ? editor.serializeDocument(connectedScenario(), { ...TEST_BACKGROUND, dataURL: dataURL(pngBytes(4, 2)) }) : bytes);
      await older;
      assert.equal(page.queued(), 1, "the older read never reaches the slot");
      held.resolve(); await running; await tick();
      assert.deepEqual(page.fake.log, ["decode 1"]);
      page.fake.finish(4, 2); await newer;
      assert.ok(page.model.history.background, "the newer image publishes");
      assert.deepEqual(page.fake.log, ["decode 1", "close 1"]);
    });
  }
});

test("a project import keeps the scenario of the file", async () => {
  const scenario = connectedScenario();
  const page = pageImports(); const text = editor.serializeDocument(scenario);
  await page.importProject({ size: text.length, text: async () => text });
  assert.deepEqual(page.model.history.value.scenario, scenario);
  assert.equal(page.messages.at(-1), "The project was imported into the draft.");
});

test("invalid frame or license input cancels an older import without reading the new file", async (t) => {
  for (const [selector, value, error] of [["#geoNorth", "-90", /latitudes/], ["#geoLicenseURL", "http://example.com", /HTTPS/]]) {
    await t.test(selector, async () => {
      const page = pageImports(); const bytes = editor.dataURLToBytes(dataURL(pngBytes(4, 2)));
      const older = page.importBackground({ type: "image/png", size: bytes.byteLength, arrayBuffer: async () => bytes });
      await tick(); assert.deepEqual(page.fake.log, ["decode 1"]);
      page.controls[selector].value = value;
      page.controls["#geoFile"].files = [{ type: "image/png", size: bytes.byteLength, arrayBuffer: () => assert.fail("invalid input must not read the file") }];
      await page.importFramedImage();
      assert.match(page.messages.at(-1), error);
      page.fake.finish(4, 2); await older;
      assert.equal(page.model.history.background, null, "the older import cannot publish");
      assert.equal(page.model.bytes, 0);
      assert.deepEqual(page.fake.log, ["decode 1", "close 1"]);
    });
  }
});

test("the export writes the asset of each frame state, and the import gives it to Go", () => {
  const config = connectedScenario();
  const bytes = editor.dataURLToBytes(dataURL(pngBytes(4, 2)));
  const cases = [
    { frameState: "none", frame: null, license: null },
    { frameState: "none", frame: null, license: OSM_LICENSE },
    { frameState: "attached", frame: LONDON_FRAME, license: OSM_LICENSE },
    { frameState: "detached", frame: LONDON_FRAME, license: null },
  ];
  for (const item of cases) {
    const image = editor.freezeImage({ key: TEST_KEY, bytes, mime: "image/png", pixelWidth: 4, pixelHeight: 2, frame: item.frame, license: item.license });
    // An attached frame that is not aligned exports and imports.
    const background = { imageKey: TEST_KEY, x: 1, y: 2, width: 3, height: 4, opacity: 0.5, frameState: item.frameState };
    const text = editor.serializeDocument(config, editor.exportBackground(background, image));
    assert.ok(!text.includes(TEST_KEY), "the export has no image key");
    const parsed = editor.parseDocument(text);
    assert.deepEqual(parsed.background, { dataURL: dataURL(pngBytes(4, 2)), x: 1, y: 2, width: 3, height: 4, opacity: 0.5 });
    // A background with no frame and no license has no asset member. The
    // import gives the asset member to the Go metadata check as the file
    // gives it.
    const exported = item.frameState !== "none" || item.license !== null;
    assert.equal(text.includes('"asset"'), exported);
    assert.equal(Object.hasOwn(parsed.metadata, "asset"), exported);
    if (exported) assert.deepEqual(parsed.metadata.asset, item);
    assert.equal(editor.frameView({ scenario: { ...config, geo: LONDON_GEO }, background }, image).warning, item.frameState === "attached" ? editor.FRAME_WARNING_TEXT : "");
  }
  assert.equal(editor.exportBackground(null, null), null);
});

test("the attribution line links only HTTPS URLs, and the Background panel lists the license facts and the image bytes", () => {
  assert.deepEqual(editor.attributionParts(null), []);
  assert.deepEqual(editor.attributionParts(OSM_LICENSE), [
    { text: "© OpenStreetMap contributors", href: "" },
    { text: "Copyright", href: "https://www.openstreetmap.org/copyright" },
    { text: "ODbL-1.0", href: "https://opendatacommons.org/licenses/odbl/1-0/" },
  ]);
  assert.deepEqual(editor.attributionParts({ ...OSM_LICENSE, attribution: "", licenseURL: OSM_LICENSE.copyrightURL }), [
    { text: "OpenStreetMap", href: "" }, { text: "Copyright", href: "https://www.openstreetmap.org/copyright" },
  ], "one link for the same URL, and the source when there is no attribution");
  assert.deepEqual(editor.attributionParts({ ...OSM_LICENSE, copyrightURL: "javascript:alert(1)", licenseURL: "http://example.com" }), [{ text: "© OpenStreetMap contributors", href: "" }]);
  const image = testImage({ frame: LONDON_FRAME, license: OSM_LICENSE });
  const mib = 1024 * 1024;
  assert.deepEqual(editor.backgroundFacts({ image, held: 3 * mib, keeperBytes: mib, cap: editor.IMAGE_TABLE_BYTES, storedBytes: 2 * mib, restoreFailed: false }), [
    "Source: OpenStreetMap", "License: ODbL-1.0", "Retrieved: 2026-09-28T12:00:00.000Z", "Method: user supplied", "Notice: Keep this notice.",
    "This tab holds 3.0 MiB of images of its 128.0 MiB limit, with 1.0 MiB for writes to the browser store.",
    "The stored image has 2.0 MiB.", editor.BACKGROUND_DURABLE_TEXT,
  ]);
  assert.deepEqual(editor.backgroundFacts({ image: null, held: 0, keeperBytes: 0, cap: editor.IMAGE_TABLE_BYTES, storedBytes: 0, restoreFailed: true }), [
    "This tab holds 0.0 MiB of images of its 128.0 MiB limit, with 0.0 MiB for writes to the browser store.", editor.STORED_BACKGROUND_KEPT_TEXT,
  ]);
});

test("each abort event of the page aborts the open acquisition, and each import starts a new one", () => {
  const source = fs.readFileSync(path.join(__dirname, "editor.js"), "utf8");
  // body gives the source of the page function name.
  const body = (name) => {
    const match = new RegExp(`\\n  (?:async )?function ${name}\\(`).exec(source);
    assert.ok(match, `the page has ${name}`);
    return source.slice(match.index, source.indexOf("\n  }\n", match.index));
  };
  for (const name of ["stepHistory", "resetDraft", "restoreDraft", "loadLiveScenario", "closeGeoPanel", "queueBackgroundEdit"]) assert.match(body(name), /model\.abort\(\)/, `${name} aborts`);
  for (const name of ["importProject", "importBackground", "importFramedImage"]) assert.match(body(name), /model\.start\(\)/, `${name} starts a new acquisition`);
  for (const event of ['\\$\\("#resetButton"\\)\\.addEventListener\\("click", resetDraft\\);', 'root\\.addEventListener\\("beforeunload", \\(event\\) => \\{ model\\.abort\\(\\);']) {
    assert.match(source, new RegExp(event));
  }
});


// This fixture keeps the mainline throat fixed while station rows move.
function layoutFixture(count = 3, side = 1, degrees = 0) {
  const config = editor.emptyConfig(); const nodes = config.network.nodes; const lanes = config.network.lanes;
  const node = (ID, X, Y) => nodes.push({ id: ID, position: { x: X, y: Y } });
  const lane = (ID, From, To, StationRole, SeparationGroup = "station-plane") => lanes.push({ id: ID, from: From, to: To, speedLimit: 14, stationID: "layout", stationRole: StationRole, separationGroup: SeparationGroup });
  node("entry", -100, 120); node("exit", 100, 120); node("diverge", -60, 120 - side * 120); node("merge", 60, 120 - side * 120);
  lane("access-in", "diverge", "entry", "entry"); lane("access-out", "exit", "merge", "exit"); lane("bypass", "entry", "exit", "through");
  const station = { id: "layout", name: "Layout", entry: "entry", exit: "exit", parkingOnly: false, berths: [] };
  for (let index = 0; index < count; index += 1) {
    const id = String(index); const y = 120 + side * (90 + index * 75);
    node(`a${id}`, -100, y); node(`b${id}`, 0, y); node(`d${id}`, 100, y);
    lane(`al${id}`, index ? `a${index - 1}` : "entry", `a${id}`, "berth-access"); lane(`dl${id}`, `d${id}`, index ? `d${index - 1}` : "exit", "departure");
    lane(`in${id}`, `a${id}`, `b${id}`, "berth-access"); lane(`out${id}`, `b${id}`, `d${id}`, "departure");
    station.berths.push({ id: `berth${id}`, node: `b${id}`, separationGroup: "station-plane" });
  }
  config.network.stations.push(station); config.fleet.push({ id: "Pod01", berthID: "berth0" });
  const radians = degrees * Math.PI / 180;
  for (const item of nodes) { const { x: X, y: Y } = item.position; item.position = { x: X * Math.cos(radians) - Y * Math.sin(radians), y: X * Math.sin(radians) + Y * Math.cos(radians) }; }
  return config;
}

test("a canceled Go opacity proposal rolls back its owned preview", async () => {
  const source = fs.readFileSync(path.join(__dirname, "editor.js"), "utf8");
  const names = ["backgroundProposal", "queueBackgroundEdit"];
  const functions = names.map((name) => {
    const match = new RegExp(`\\n  (?:async )?function ${name}\\(`).exec(source);
    assert.ok(match);
    return source.slice(match.index, source.indexOf("\n  }\n", match.index) + 5);
  }).join("\n");
  const initial = { scenario: connectedScenario(), background: { imageKey: TEST_KEY, x: 0, y: 0, width: 40, height: 20, opacity: .45, frameState: "none" } };
  const model = testModel({ initial });
  const editQueue = require("./editor-model.js").createEditQueue();
  const state = { history: model.history, drag: null };
  const response = Promise.withResolvers();
  const noop = () => {};
  const deps = {
    model, state, editQueue, pendingInputs: new Map(), pendingBackgroundPreviews: new Set(),
    draft: () => model.history.snapshot.scenario,
    editorProposal: () => response.promise,
    render: noop, renderHistoryButtons: noop, renderApply: noop, restorePendingInputs: noop, toast: noop, updateStatus: noop,
  };
  const queue = new Function(...Object.keys(deps), `${functions}\nreturn queueBackgroundEdit;`)(...Object.values(deps));
  model.pressOpacity(); model.setOpacity(.8);
  const ended = model.takeOpacity();
  const job = queue({ action: "opacity", opacity: .8 }, { before: ended.entry, sourceEdits: model.edits });
  await tick();
  editQueue.cancel();
  response.resolve({ change: { patch: {}, background: { value: { ...initial.background, opacity: .8 } } } });
  assert.equal(await job, false);
  assert.equal(model.history.background.opacity, .45);
  assert.equal(model.history.canUndo, false);
});

function independentBankFixture() {
  const config = layoutFixture(); const station = config.network.stations[0];
  const second = structuredClone(config);
  for (const node of second.network.nodes) { node.id = `b-${node.id}`; node.position.x += 600; }
  for (const lane of second.network.lanes) { lane.id = `b-${lane.id}`; lane.from = `b-${lane.from}`; lane.to = `b-${lane.to}`; }
  const members = second.network.stations[0].berths;
  for (const berth of members) { berth.id = `b-${berth.id}`; berth.node = `b-${berth.node}`; }
  station.banks = [
    { id: "a", entry: station.entry, exit: station.exit, berthIDs: station.berths.map((berth) => berth.id) },
    { id: "b", entry: "b-entry", exit: "b-exit", berthIDs: members.map((berth) => berth.id) },
  ];
  station.berths.push(...members); config.network.nodes.push(...second.network.nodes); config.network.lanes.push(...second.network.lanes);
  for (const prefix of ["", "b-"]) {
    config.network.nodes.push({ id: `${prefix}road-in`, position: { x: prefix ? 540 : -60, y: -100 } }, { id: `${prefix}road-out`, position: { x: prefix ? 660 : 60, y: -100 } });
    config.network.lanes.push({ id: `${prefix}road-in`, from: `${prefix}road-in`, to: `${prefix}diverge`, speedLimit: 14 }, { id: `${prefix}road-out`, from: `${prefix}merge`, to: `${prefix}road-out`, speedLimit: 14 });
  }
  return config;
}

test("bank controls send scoped Go geometry commands", () => {
  const config = independentBankFixture(); const before = structuredClone(config);
  assert.deepEqual(editor.stationGeometryCommand(config, "layout", "addBerth", undefined, "b"), { action: "addBankBerth", id: "layout", value: "b" });
  assert.deepEqual(editor.stationGeometryCommand(config, "layout", "stationLayout", { pitch: 50, approachLength: 160 }, "b"), { action: "bankLayout", id: "layout", value: { bank: "b", pitch: 50, approachLength: 160 } });
  assert.throws(() => editor.stationGeometryCommand(config, "layout", "addBerth"), /Select a station bank/);
  const legacy = layoutFixture();
  assert.deepEqual(editor.stationGeometryCommand(legacy, "layout", "addBerth"), { action: "addBerth", id: "layout" });
  assert.deepEqual(editor.stationGeometryCommand(config, "layout", "removeBerth", "berth0", "a"), { action: "removeBerth", id: "layout", value: "berth0" });
  assert.deepEqual(config, before);
});

test("bank views select berth membership and dedicated anchor dimensions", () => {
  const config = independentBankFixture();
  const card = editor.selectionCard(config, { type: "station", id: "layout", bank: "b", berth: "b-berth1" });
  assert.equal(card.bank, "b"); assert.deepEqual(card.banks, [{ id: "a" }, { id: "b" }]);
  assert.deepEqual(card.berths, [{ id: "b-berth0", selected: false }, { id: "b-berth1", selected: true }, { id: "b-berth2", selected: false }]);
  assert.equal(editor.selectionCard(config, { type: "station", id: "layout", bank: "removed" }).bank, "a");
  assert.equal(editor.selectionCard(config, { type: "station", id: "layout", berth: "b-berth1" }).bank, "b");
  assert.match(editor.stationLayout(config, "layout").error, /Select/);
  const a = editor.stationLayout(config, "layout", "a"), b = editor.stationLayout(config, "layout", "b");
  assert.equal(a.error, ""); assert.equal(b.error, ""); assert.equal(b.pitch, 75); assert.equal(b.spacing, 200);
  assert.equal(b.station.entry, "b-entry"); assert.ok(b.approachLength > 120); assert.ok(b.departureLength > 120);
  Object.assign(config.network.lanes.find((lane) => lane.id === "b-road-in"), { stationID: "layout", stationRole: "approach" });
  assert.ok(editor.stationLayout(config, "layout", "b").approachLength > 120);
  config.network.lanes.push({ id: "shared", from: "b-diverge", to: "road-in", speedLimit: 14 });
  assert.equal(editor.stationLayout(config, "layout", "b").approachLength, null);
});

test("bank import and export preserve the bank metadata", () => {
  const config = independentBankFixture();
  const sandbox = { structuredClone, TextEncoder, PodsimTiles: require("./tiles.js") };
  require("node:vm").runInNewContext(fs.readFileSync(path.join(__dirname, "editor.js"), "utf8"), sandbox);
  const browser = sandbox.PodsimEditorModel;
  assert.deepEqual(JSON.parse(JSON.stringify(browser.parseDocument(JSON.stringify(config)).scenario)), config);
  assert.deepEqual(JSON.parse(JSON.stringify(browser.parseDocument(editor.serializeDocument(config)).scenario)), config);
});

test("bank station pointer previews include every gate and local row once", () => {
  const config = independentBankFixture();
  const targets = editor.dragTargets(config, { type: "station", id: "layout" });
  for (const id of ["entry", "exit", "b-entry", "b-exit", "b-a0", "b-b0", "b-d0"]) assert.ok(targets.nodeIDs.includes(id), id);
  assert.equal(new Set(targets.nodeIDs).size, targets.nodeIDs.length);
  assert.ok(!targets.nodeIDs.includes("diverge")); assert.ok(!targets.nodeIDs.includes("b-diverge"));
});

test("browser URL facts retain exact text and current URL acceptance", () => {
  for (const value of ["https://example.com", " HTTPS://example.com/path ", "https:example.com", "http://example.com", "relative", "https://example.com/\ud800"]) {
    const metadata = { asset: { frameState: "none", license: { licenseURL: value, copyrightURL: "" } } };
    const facts = editor.metadataURLFacts(metadata);
    assert.strictEqual(facts.asset, metadata.asset);
    assert.equal(facts.urlFacts.licenseURL.text, value);
    let https; try { https = new URL(value).protocol === "https:"; } catch (_) { https = false; }
    assert.equal(facts.urlFacts.licenseURL.https, https);
    assert.equal(Object.hasOwn(facts.urlFacts, "copyrightURL"), false);
  }
});

test("import admits only PNG and JPEG data URL prefixes", () => {
  const scenario = connectedScenario(), bytes = pngBytes(4, 2), base64 = Buffer.from(bytes).toString("base64");
  for (const prefix of ["data:image/gif;base64,", "garbage,", "", "data:image/png,", "DATA:image/png;base64,"]) {
    const background = { ...TEST_BACKGROUND, dataURL: prefix + base64 };
    const document = JSON.stringify({ format: "podsim", version: 1, scenario, background });
    assert.throws(() => editor.parseDocument(document), /PNG or JPEG data URL/);
  }
  for (const type of ["png", "jpeg"]) {
    const background = { ...TEST_BACKGROUND, dataURL: dataURL(bytes, type) };
    const document = JSON.stringify({ format: "podsim", version: 1, scenario, background });
    assert.equal(editor.parseDocument(document).background.dataURL, background.dataURL);
  }
});

test("import keeps the raw scenario and metadata for the Go checks", () => {
  const scenario = { ...connectedScenario(), fleet: [{ stationID: "missing", berthID: false }], demand: { pattern: "market", destination: false } };
  const background = { ...TEST_BACKGROUND, dataURL: dataURL(pngBytes(4, 2)), opacity: 5, asset: null };
  const parsed = editor.parseDocument(JSON.stringify({ format: "podsim", version: 1, scenario, background }));
  assert.deepEqual(parsed.scenario.fleet, scenario.fleet);
  assert.deepEqual(parsed.scenario.demand, scenario.demand);
  assert.equal(parsed.metadata.placement.opacity, 5);
  assert.equal(parsed.metadata.asset, null);
  assert.equal(Object.hasOwn(parsed.metadata, "dataURL"), false);
});

test("station layout rendering rejects older selection and geometry replies", async (t) => {
  const source = fs.readFileSync(path.join(__dirname, "editor.js"), "utf8");
  const start = source.indexOf("\n  function renderStationLayout("), end = source.indexOf("\n  }\n", start) + 5;
  assert.ok(start >= 0 && end > start);
  for (const change of ["selection", "geometry"]) await t.test(change, async () => {
    const inputs = Object.fromEntries(["pitch", "spacing", "setback", "approachLength", "departureLength"].map((key) => [key, { value: "", disabled: false, dataset: {} }]));
    const controls = new Map();
    const panel = { isConnected: true, querySelector(selector) {
      const layout = /^\[data-layout="([^"]+)"\]$/.exec(selector);
      if (layout) return inputs[layout[1]];
      if (!controls.has(selector)) controls.set(selector, { value: "", textContent: "", replaceChildren() {} });
      return controls.get(selector);
    }, querySelectorAll() { return Object.values(inputs); } };
    let config = { network: { stations: [{ id: "alpha" }, { id: "beta" }] } };
    const state = { selection: { id: "alpha" } }, calls = [];
    const deps = { state, draft: () => config, selectedBank: () => undefined, setControlValue: (control, value) => { control.value = value; },
      goModel: { call(project, op, layout) { const pending = Promise.withResolvers(); calls.push({ project, op, layout, pending }); return pending.promise; } },
    };
    const render = new Function(...Object.keys(deps), `${source.slice(start, end)}\nreturn renderStationLayout;`)(...Object.values(deps));
    render(panel, config, "alpha");
    if (change === "selection") state.selection = { id: "beta" };
    else config = { network: { stations: config.network.stations } };
    render(panel, config, state.selection.id);
    const summary = (pitch) => ({ layout: { pitch: { value: pitch, reason: "" }, spacing: { value: 200, reason: "" }, setback: { value: 120, reason: "" }, approachLength: { value: null, reason: "No bank" }, departureLength: { value: null, reason: "No bank" } } });
    calls[0].pending.resolve(summary(75)); await tick();
    assert.ok(Object.values(inputs).every((input) => input.disabled && input.value === ""), "stale layout must not enable the current controls");
    calls[1].pending.resolve(summary(40)); await tick();
    assert.equal(inputs.pitch.disabled, false); assert.equal(inputs.pitch.value, "40");
    assert.equal(calls[1].op, "stationLayout");assert.equal(calls[1].layout.stationID, state.selection.id);
  });
});
