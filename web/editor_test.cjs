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

function connectedScenario() {
  let config = editor.addStation(editor.emptyConfig(), 100, 100, { name: "Alpha" });
  config = editor.addStation(config, 340, 100, { name: "Beta" });
  const [alpha, beta] = config.network.Stations;
  config = editor.addLane(config, alpha.Exit, beta.Entry, false);
  config = editor.addLane(config, beta.Exit, alpha.Entry, false);
  config.demand.destination = beta.ID;
  return editor.setFleetCount(config, alpha.ID, 1);
}

function serviceScenario() {
  const config = connectedScenario();
  config.version = 3;
  const classes = ["legacy", "compact", "group", "express"];
  for (const lane of config.network.Lanes) lane.VehicleClasses = [...classes];
  for (const station of config.network.Stations) {
    station.VehicleClasses = [...classes];
    for (const berth of station.Berths) berth.VehicleClasses = [...classes];
  }
  config.fleet[0].Class = "compact";
  config.expressServices = [{ ID: "express", From: config.network.Stations[0].ID, To: config.network.Stations[1].ID, Class: "express", PartyLimit: 20 }];
  return config;
}


test("Express project round trips retain the explicit contract and whole service metadata", () => {
  const config = serviceScenario();
  config.version = 4; config.orderContract = "express-v1"; config.fleet[0].Class = "express";
  const before = structuredClone(config);
  const document = JSON.parse(editor.serializeDocument(config));
  assert.equal(document.format, "podsim"); assert.equal(document.version, 1);
  assert.deepEqual(document.scenario, before);
  assert.deepEqual(editor.parseDocument(JSON.stringify(config)).scenario, before);
  assert.deepEqual(editor.parseDocument(JSON.stringify(document)).scenario, before);
  const normalized = editor.normalizeConfig(config);
  assert.equal(normalized.version, 4); assert.equal(normalized.orderContract, "express-v1");
  assert.deepEqual(normalized.fleet, before.fleet); assert.deepEqual(normalized.expressServices, before.expressServices);
  assert.deepEqual(config, before);
  assert.match(editor.fleetClassNotice(config), /qualified express-v1 runtime/);
});

test("Express project contract selection rejects missing, null, unknown, and old-version markers", () => {
  for (const version of [1, 2, 3, 4]) {
    for (const contract of [undefined, null, "", "future", "express-v1"]) {
      if (version !== 4 && contract === undefined || version === 4 && contract === "express-v1") continue;
      const config = serviceScenario(); config.version = version;
      if (contract !== undefined) config.orderContract = contract;
      for (const wrapped of [config, {format: "podsim", version: 1, scenario: config}]) {
        assert.throws(() => editor.parseDocument(JSON.stringify(wrapped)), /contract|version 4/);
      }
      assert.throws(() => editor.normalizeConfig(config), /contract|version 4/);
    }
  }
});

test("compact station queue drafts require explicit version and dependencies", () => {
  for (const value of [null, "ordinary", "compact-v1"]) {
    for (const version of [1, 2]) {
      const config = serviceScenario(); config.version = version; config.stationQueueSpacing = value;
      assert.throws(() => editor.normalizeConfig(config), /version 3/);
    }
  }
  for (const value of [null, "", "other", true, "ordinary", "compact-v1"]) {
    for (const buffers of [false, true]) for (const limit of [0, 2, 3, 4]) {
      const config = serviceScenario(); config.stationQueueSpacing = value; config.stationBuffers = buffers; config.platoonLimit = limit;
      const errors = editor.validateConfig(config);
      const invalid = errors.filter((error) => /[Ss]tation queue|Compact station/.test(error));
      const valid = value === "ordinary" || value === "compact-v1" && buffers && limit >= 2;
      assert.equal(invalid.length === 0, valid, JSON.stringify({value, buffers, limit, errors}));
      assert.deepEqual(editor.normalizeConfig(config).stationQueueSpacing, value);
    }
  }
});
test("project 3 imports bare or wrapper 1 and preserves authored class and registry metadata", () => {
  const config = serviceScenario(), before = structuredClone(config);
  assert.deepEqual(editor.parseDocument(JSON.stringify(config)).scenario, config);
  const exported = JSON.parse(editor.serializeDocument(config, null));
  assert.equal(exported.version, 1);
  assert.equal(exported.scenario.version, 3);
  assert.deepEqual(editor.parseDocument(JSON.stringify(exported)).scenario, config);
  assert.deepEqual(editor.validateConfig(config), []);
  const normalized = editor.normalizeConfig(config);
  assert.equal(normalized.version, 3);
  assert.deepEqual(normalized.expressServices, config.expressServices);
  assert.deepEqual(normalized.fleet, config.fleet);
  normalized.expressServices[0].PartyLimit = 1;
  normalized.network.Stations[0].VehicleClasses.pop();
  assert.deepEqual(config, before);
  assert.match(editor.fleetClassNotice(config), /New pods use legacy class/);
  assert.match(editor.fleetClassNotice(config), /Express pods cannot start/);
  // The lane Selection card sets guideway classes, so only pod classes need an import.
  assert.match(editor.fleetClassNotice(config), /Import a project to set pod classes and express services\./);
  assert.equal(editor.fleetClassNotice({ version: 1 }), "");
  for (const version of [1, 2]) {
    const invalid = { ...config, version };
    assert.throws(() => editor.normalizeConfig(invalid), /version 3/);
    assert.throws(() => editor.parseDocument(JSON.stringify(invalid)), /version 3/);
  }
  const group = structuredClone(config); group.fleet[0].Class = "group";
  assert.deepEqual(editor.validateConfig(group), []);
  for (const classID of ["express", "unknown", null, ""]) {
    const invalid = structuredClone(config); invalid.fleet[0].Class = classID;
    assert.ok(editor.validateConfig(invalid).some((text) => /vehicle class|physical profile/.test(text)));
  }
});

test("project 3 is independent of banks and new metadata; old project fields require explicit version 3", () => {
  const config = connectedScenario(); config.version = 3;
  assert.deepEqual(editor.validateConfig(config), []);
  assert.equal(editor.normalizeConfig(config).version, 3);
  for (const value of [null, []]) {
    const invalid = { ...config, version: 1, expressServices: value };
    assert.throws(() => editor.parseDocument(JSON.stringify(invalid)), /version 3/);
    const deferred = editor.parseDocument(JSON.stringify(invalid), { deferMetadata: true });
    assert.deepEqual(deferred.scenario.expressServices, value);
  }
  const compactOnly = serviceScenario();
  compactOnly.network.Stations[0].VehicleClasses = ["compact"];
  delete compactOnly.fleet[0].Class;
  assert.ok(editor.validateConfig(compactOnly).some((text) => /incompatible station or berth/.test(text)));
  const future = serviceScenario();
  delete future.fleet[0].Class;
  assert.equal(editor.normalizeConfig(future).fleet[0].Class, undefined);
});

test("project-3 reference checks match native class and registry reports", () => {
  const fixture = JSON.parse(fs.readFileSync(path.join(__dirname, "../internal/editormodel/testdata/service_checks.json"), "utf8"));
  for (const item of fixture.cases) {
    const config = structuredClone(fixture.base);
    for (const change of item.changes) {
      const parent = change.path.slice(0, -1).reduce((current, key) => current[key], config);
      parent[change.path.at(-1)] = structuredClone(change.value);
    }
    assert.deepEqual(editor.checkResults(config), item.checks, item.name);
  }
});

// chainScenario gives Alpha a berth chain like the generated stations use:
// entry, arrival node, berth, departure node, and exit.
function chainScenario() {
  let config = connectedScenario();
  const alpha = config.network.Stations[0];
  const berth = alpha.Berths[0].Node;
  config.network.Lanes = config.network.Lanes.filter((lane) => !(lane.From === alpha.Entry && lane.To === berth) && !(lane.From === berth && lane.To === alpha.Exit));
  config = editor.addJunction(config, 64, 160);
  const arrival = config.network.Nodes.at(-1).ID;
  config = editor.addJunction(config, 136, 160);
  const departure = config.network.Nodes.at(-1).ID;
  for (const [from, to, role] of [[alpha.Entry, arrival, "berth-access"], [arrival, berth, "berth-access"], [berth, departure, "departure"], [departure, alpha.Exit, "departure"]]) {
    config = editor.addLane(config, from, to, false);
    Object.assign(config.network.Lanes.at(-1), { StationID: alpha.ID, StationRole: role });
  }
  return { config, arrival, departure };
}

// addedBerth gives the config after addBerth. The test fails when addBerth
// does not add the berth.
function addedBerth(config, stationID) {
  const result = editor.addBerth(config, stationID);
  assert.equal(result.error, "", result.error);
  return result.config;
}

// removedBerth gives the config after removeBerth. The test fails when
// removeBerth does not remove the berth.
function removedBerth(config, stationID, berthID) {
  const result = editor.removeBerth(config, stationID, berthID);
  assert.equal(result.error, "", result.error);
  return result.config;
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

// generatedProject loads a generated project as the editor loads the live
// server project.
function generatedProject(preset, ...flags) {
  return editor.normalizeConfig(JSON.parse(generatedFile(preset, ...flags)));
}

test("station creation makes separate safe entry, exit, and berth geometry", () => {
  const config = editor.addStation(editor.emptyConfig(), 200, 140, { name: "Central" });
  const station = config.network.Stations[0];

  assert.notEqual(station.Entry, station.Exit);
  assert.notEqual(station.Berths[0].Node, station.Entry);
  assert.equal(config.network.Nodes.length, 3);
  assert.equal(config.network.Lanes.length, 3);
  for (const lane of config.network.Lanes) assert.ok(editor.laneLength(config, lane) >= editor.MIN_LANE_LENGTH);
  assert.deepEqual(config.network.Lanes.map((lane) => lane.StationRole).sort(), ["berth-access", "departure", "through"]);
  assert.ok(config.network.Lanes.every((lane) => lane.StationID === station.ID));
});

test("capacity changes create physical lanes and clean placements on removal", () => {
  let config = editor.addStation(editor.emptyConfig(), 200, 140, { name: "Depot", parkingOnly: true });
  const stationID = config.network.Stations[0].ID;
  config = addedBerth(config, stationID);
  const station = config.network.Stations[0];
  const removed = station.Berths[1];
  assert.equal(station.Berths.length, 2);
  assert.equal(config.network.Lanes.filter((lane) => lane.From === removed.Node || lane.To === removed.Node).length, 2);

  config = editor.setFleetCount(config, stationID, 2);
  assert.equal(config.fleet.length, 2);
  config = removedBerth(config, stationID, removed.ID);

  assert.equal(config.network.Stations[0].Berths.length, 1);
  assert.equal(config.network.Nodes.some((node) => node.ID === removed.Node), false);
  assert.equal(config.network.Lanes.some((lane) => lane.From === removed.Node || lane.To === removed.Node), false);
  assert.equal(config.fleet.some((pod) => pod.BerthID === removed.ID), false);
});

test("junction and station deletes remove only explicit dependent objects", () => {
  let config = connectedScenario();
  config = editor.addJunction(config, 220, 260);
  const junction = config.network.Nodes.at(-1).ID;
  const station = config.network.Stations[0];
  config = editor.addLane(config, station.Exit, junction, false);
  const deletion = editor.deleteNode(config, junction);
  assert.equal(deletion.error, "");
  assert.equal(deletion.config.network.Lanes.some((lane) => lane.From === junction || lane.To === junction), false);

  const guarded = editor.deleteNode(config, station.Entry);
  assert.match(guarded.error, /station or berth/);
  config = editor.setFleetCount(config, station.ID, 1);
  config = editor.deleteStation(config, station.ID);
  assert.equal(config.network.Stations.some((item) => item.ID === station.ID), false);
  assert.equal(config.fleet.some((pod) => pod.StationID === station.ID), false);
});

// profileScenario gives three connected passenger stations and two demand
// profiles. The weekday profile has two flows that name Gamma. The weekend
// profile names only Alpha and Beta.
function profileScenario() {
  let config = connectedScenario();
  config = editor.addStation(config, 220, 300, { name: "Gamma" });
  const [alpha, beta, gamma] = config.network.Stations;
  config = editor.addLane(config, beta.Exit, gamma.Entry, false);
  config = editor.addLane(config, gamma.Exit, alpha.Entry, false);
  const band = (id) => ({ id, name: id, startMinute: 0, durationMinutes: 60 });
  config.demandProfiles = [
    { id: "weekday", name: "Weekday", bands: [band("am"), band("pm")], flows: [
      { from: alpha.ID, to: beta.ID, weights: [3, 1] },
      { from: alpha.ID, to: gamma.ID, weights: [2, 2] },
      { from: gamma.ID, to: beta.ID, weights: [1, 4] },
      { from: beta.ID, to: alpha.ID, weights: [1, 3] },
    ] },
    { id: "weekend", name: "Weekend", bands: [band("day")], flows: [{ from: beta.ID, to: alpha.ID, weights: [5] }] },
  ];
  Object.assign(config.demand, { pattern: "profile", profile: "weekday", band: "am" });
  return { config, alpha, beta, gamma };
}

test("the size check uses the network limits of the server", () => {
  const sizeError = "The network exceeds the supported size.";
  const base = connectedScenario();
  assert.ok(!editor.validateConfig(base).includes(sizeError));
  const [station] = base.network.Stations;
  const [lane] = base.network.Lanes;
  const cases = [
    { field: "Nodes", limit: editor.MAX_NODES, item: (index) => ({ ID: `pad-${index}`, Position: { X: 1000 + index, Y: 1000 } }) },
    { field: "Lanes", limit: editor.MAX_LANES, item: (index) => ({ ...lane, ID: `pad-${index}` }) },
    { field: "Stations", limit: editor.MAX_STATIONS, item: (index) => ({ ...station, ID: `pad-${index}`, Name: `Pad ${index}` }) },
  ];
  for (const { field, limit, item } of cases) {
    const padded = (count) => {
      const config = structuredClone(base);
      const items = config.network[field];
      while (items.length < count) items.push(item(items.length));
      return config;
    };
    assert.ok(!editor.validateConfig(padded(limit)).includes(sizeError), `${field} at the limit`);
    assert.ok(editor.validateConfig(padded(limit + 1)).includes(sizeError), `${field} past the limit`);
  }
});

test("add berth keeps the lanes at each node within the limit", () => {
  // A station that is not a chain has a lane from its entry to each berth.
  let config = connectedScenario();
  const beta = config.network.Stations[1];
  const count = (item, node) => item.network.Lanes.filter((lane) => lane.From === node).length + item.network.Lanes.filter((lane) => lane.To === node).length;
  while (count(config, beta.Entry) < editor.MAX_NODE_LANES) config = addedBerth(config, beta.ID);
  const full = editor.addBerth(config, beta.ID);
  assert.equal(full.config, config);
  assert.equal(full.error, `No space for another berth at ${beta.Name}. Node ${beta.Entry} would have ${editor.MAX_NODE_LANES + 1} lanes, more than ${editor.MAX_NODE_LANES}.`);
  assert.ok(!editor.validateConfig(config).some((error) => error.includes("lanes, more than")));

  // A chain row adds a lane at the arrival and the departure node of the
  // last row.
  const chain = chainScenario();
  let crowded = chain.config;
  for (let index = 0; count(crowded, chain.arrival) < editor.MAX_NODE_LANES; index++) {
    crowded = editor.addJunction(crowded, 2000 + 50 * index, 2000);
    crowded = editor.addLane(crowded, crowded.network.Nodes.at(-1).ID, chain.arrival, false);
  }
  const alpha = crowded.network.Stations[0];
  const blocked = editor.addBerth(crowded, alpha.ID);
  assert.equal(blocked.config, crowded);
  assert.equal(blocked.error, `No space for another berth at ${alpha.Name}. Node ${chain.arrival} would have ${editor.MAX_NODE_LANES + 1} lanes, more than ${editor.MAX_NODE_LANES}.`);
});

test("the check limits the lanes at a node and finds lanes with the same path", () => {
  const base = connectedScenario();
  const [lane] = base.network.Lanes;
  const hub = lane.From;
  const atHub = base.network.Lanes.filter((item) => item.From === hub).length + base.network.Lanes.filter((item) => item.To === hub).length;
  const withSpokes = (count) => {
    const config = structuredClone(base);
    for (let index = 0; index < count; index++) {
      config.network.Nodes.push({ ID: `spoke-${index}`, Position: { X: 5000 + 100 * index, Y: 5000 } });
      config.network.Lanes.push({ ID: `spoke-${index}`, From: hub, To: `spoke-${index}`, SpeedLimit: 10 });
    }
    return config;
  };
  const limitError = `Node ${hub} has ${editor.MAX_NODE_LANES + 1} lanes, more than ${editor.MAX_NODE_LANES}.`;
  assert.ok(!editor.validateConfig(withSpokes(editor.MAX_NODE_LANES - atHub)).some((error) => error.includes("lanes, more than")));
  assert.ok(editor.validateConfig(withSpokes(editor.MAX_NODE_LANES - atHub + 1)).includes(limitError));

  const copy = structuredClone(base);
  copy.network.Lanes.push({ ...lane, ID: "copy" });
  assert.ok(editor.validateConfig(copy).includes(`Lanes ${lane.ID} and copy have the same nodes and path.`));
  const curved = structuredClone(base);
  curved.network.Lanes.push({ ...lane, ID: "curved", Control: { X: 1, Y: 2 } });
  assert.ok(!editor.validateConfig(curved).some((error) => error.includes("same nodes and path")));
});

test("a station delete removes the demand flows that name the station", () => {
  const { config, beta, gamma } = profileScenario();
  assert.deepEqual(editor.validateConfig(config), []);
  assert.equal(editor.stationFlowCount(config, gamma.ID), 2);

  const deleted = editor.deleteStation(config, gamma.ID);
  const [weekday, weekend] = config.demandProfiles;
  assert.deepEqual(deleted.demandProfiles[0].flows, [weekday.flows[0], weekday.flows[3]]);
  assert.deepEqual(deleted.demandProfiles[1], weekend);
  assert.deepEqual(editor.validateConfig(deleted), []);

  // A profile with no flows stays, and validation reports it.
  assert.equal(editor.stationFlowCount(config, beta.ID), 4);
  const empty = editor.deleteStation(config, beta.ID);
  assert.deepEqual(empty.demandProfiles.map((profile) => profile.flows.length), [1, 0]);
  assert.deepEqual(editor.validateConfig(empty).filter((error) => error.startsWith("Demand profile")), [
    `Demand profile weekend must contain 1 to ${editor.MAX_FLOWS} flows.`,
    "Demand profile weekend has an empty band.",
  ]);
});

test("undo after a station delete restores the station and its demand flows", () => {
  const { config, gamma } = profileScenario();
  const original = { scenario: config, background: null };
  const history = editor.createHistory(original);

  assert.equal(history.replace({ scenario: editor.deleteStation(config, gamma.ID), background: null }), true);
  assert.equal(history.value.scenario.demandProfiles[0].flows.length, 2);
  assert.equal(history.undo(), true);
  assert.deepEqual(history.value, original);
});

test("operating flags preserve undo snapshots across graph edits and external copies", () => {
  const { config, gamma } = profileScenario();
  config.demand.enabled = false; config.redistribution = false;
  const original = { scenario: config, background: { imageKey: "kept" } };
  let changes = 0;
  const history = editor.createHistory(original, () => { changes++; });
  assert.equal(history.setOperatingFlag("demandEnabled", true), true);
  assert.equal(history.setOperatingFlag("redistribution", true), true);
  assert.equal(history.setOperatingFlag("redistribution", true), false);
  assert.equal(changes, 2);
  const enabled = history.value;
  const external = history.value;
  external.scenario.network.Stations[0].Name = "external mutation";
  external.scenario.demand.enabled = false;
  external.background.imageKey = "changed";
  assert.deepEqual(history.value, enabled);
  const edited = editor.deleteStation(history.value.scenario, gamma.ID);
  history.replace({ scenario: edited, background: original.background });
  assert.equal(history.undo(), true);
  assert.deepEqual(history.value, enabled);
  history.undo(); history.undo();
  assert.deepEqual(history.value, original);
  history.redo(); history.redo();
  assert.deepEqual(history.value, enabled);
  assert.equal(history.scenarioText, JSON.stringify(enabled.scenario));
  history.undo(); history.setOperatingFlag("demandEnabled", false);
  assert.equal(history.canRedo, false);
  assert.deepEqual(history.value, original);
  assert.throws(() => history.setOperatingFlag("network", true));
  assert.throws(() => history.setOperatingFlag("redistribution", "true"));
});

test("operating flags skip checks only for boolean-to-boolean changes", () => {
  const { config } = profileScenario();
  config.demand.enabled = false; config.redistribution = "invalid";
  const changes = [];
  const model = editor.createBackgroundModel({ initial: { scenario: config, background: null }, onChange: (unchanged) => changes.push(unchanged) });
  model.history.setOperatingFlag("demandEnabled", true);
  model.history.setOperatingFlag("redistribution", false);
  model.history.undo();
  assert.deepEqual(changes, [true, false, false]);
});

test("experimental flags keep independent undo history and graph ownership", () => {
  const { config } = profileScenario();
  const history = editor.createHistory({ scenario: config, background: null });
  const network = history.snapshot.scenario.network;
  assert.equal(history.setOperatingFlag("stationBuffers", true), true);
  assert.equal(history.snapshot.scenario.network, network);
  assert.equal(history.snapshot.scenario.pickupReassignment, false);
  assert.equal(history.setOperatingFlag("pickupReassignment", true), true);
  assert.equal(history.setOperatingFlag("pickupReassignment", true), false);
  assert.equal(history.snapshot.scenario.network, network);
  history.undo();
  assert.equal(history.snapshot.scenario.stationBuffers, true);
  assert.equal(history.snapshot.scenario.pickupReassignment, false);
  history.undo();
  assert.equal(history.snapshot.scenario.stationBuffers, false);
  history.redo(); history.redo();
  assert.equal(history.snapshot.scenario.stationBuffers, true);
  assert.equal(history.snapshot.scenario.pickupReassignment, true);
});

// couplingScenario gives the version 5 project that the Go model tests
// check against native validation.
function couplingScenario() {
  return JSON.parse(fs.readFileSync(path.join(__dirname, "../internal/editormodel/testdata/coupling_project.json"), "utf8"));
}

const COUPLING_KEYS = ["couplingContract", "couplingEnabled", "couplingSites", "couplingCorridors"];
const couplingText = (scenario) => JSON.stringify(Object.fromEntries(Object.entries(scenario).filter(([key]) => COUPLING_KEYS.includes(key))));

test("a version 5 project keeps its coupling members through wrapped and bare import and export", () => {
  for (const enabled of [true, false, undefined]) {
    const config = couplingScenario();
    if (enabled === undefined) delete config.couplingEnabled; else config.couplingEnabled = enabled;
    const exported = editor.serializeDocument(config);
    assert.deepEqual(JSON.parse(exported).scenario, config, `export ${enabled}`);
    assert.deepEqual(Object.keys(JSON.parse(exported).scenario), Object.keys(config), `export order ${enabled}`);
    for (const [name, text] of [["wrapped", exported], ["bare", JSON.stringify(config)]]) {
      const imported = editor.parseDocument(text).scenario;
      assert.equal(imported.version, 5, name);
      assert.equal(couplingText(imported), couplingText(config), `${name} ${enabled}`);
      assert.equal(Object.hasOwn(imported, "orderContract"), false, name);
      assert.equal(couplingText(JSON.parse(editor.serializeDocument(imported)).scenario), couplingText(config), `${name} re-export ${enabled}`);
      assert.deepEqual(editor.parseDocument(text, { deferMetadata: true }).scenario, config, `${name} deferred`);
    }
  }
  const express = { ...couplingScenario(), orderContract: "express-v1" };
  assert.equal(editor.parseDocument(JSON.stringify(express)).scenario.orderContract, "express-v1");
});

test("a version 5 project rejects a missing marker and malformed coupling members", () => {
  const cases = [
    [(config) => { delete config.couplingContract; }, /requires couplingContract compact-pair-v1/],
    [(config) => { config.couplingContract = "compact-pair-v2"; }, /requires couplingContract compact-pair-v1/],
    [(config) => { config.couplingEnabled = null; }, /train setting must be true or false/],
    [(config) => { config.couplingSites = null; }, /sites and corridors must be arrays/],
    [(config) => { config.couplingCorridors = {}; }, /sites and corridors must be arrays/],
    [(config) => { config.orderContract = ""; }, /accepts only orderContract express-v1/],
  ];
  for (const [change, message] of cases) {
    const config = couplingScenario(); change(config);
    for (const text of [JSON.stringify(config), editor.serializeDocument(config)]) assert.throws(() => editor.parseDocument(text), message);
  }
});

test("versions 1 to 4 reject coupling members, including null and empty values, and keep their own version", () => {
  const scenarios = { 1: connectedScenario(), 3: serviceScenario() };
  scenarios[4] = { ...serviceScenario(), version: 4, orderContract: "express-v1" };
  scenarios[2] = connectedScenario(); scenarios[2].version = 2;
  const station = scenarios[2].network.Stations[0];
  station.Banks = [{ ID: "a", Entry: station.Entry, Exit: station.Exit, BerthIDs: station.Berths.map((berth) => berth.ID) }];
  for (const [version, config] of Object.entries(scenarios)) {
    const imported = editor.parseDocument(JSON.stringify(config)).scenario;
    assert.equal(imported.version, Number(version), `version ${version}`);
    assert.equal(COUPLING_KEYS.some((key) => Object.hasOwn(imported, key)), false, `version ${version} gained a coupling member`);
    assert.equal(editor.serializeDocument(config), JSON.stringify({ format: "podsim", version: 1, scenario: config }), `version ${version} export`);
    for (const key of COUPLING_KEYS) {
      for (const value of [null, "", false, true, [], "compact-pair-v1"]) {
        const text = JSON.stringify({ ...config, [key]: value });
        assert.throws(() => editor.parseDocument(text), /Coupling fields require project version 5/, `version ${version} ${key} ${JSON.stringify(value)}`);
      }
    }
  }
  assert.throws(() => editor.parseDocument(JSON.stringify({ ...couplingScenario(), version: 6 })), /The version field must be 1, 2, 3, 4, or 5/);
});

test("the train option is one version 5 control, and off keeps the sites and corridors", async () => {
  const html = fs.readFileSync(path.join(__dirname, "editor.html"), "utf8");
  assert.match(html, /<label class="check" id="couplingEnabledLabel" hidden><input id="couplingEnabled" type="checkbox"> Coupled trains \(experimental\)<\/label>/);
  // The site and corridor lists have their own sections. The train option is the only coupling input of the page.
  assert.deepEqual([...html.matchAll(/<input id="(coupling[^"]*)"/g)].map((match) => match[1]), ["couplingEnabled"]);
  const source = fs.readFileSync(path.join(__dirname, "editor.js"), "utf8");
  assert.match(source, /\$\("#couplingEnabledLabel"\)\.hidden = \$\("#couplingEnabledHint"\)\.hidden = config\.version !== 5;/);
  assert.match(source, /\$\("#couplingEnabled"\)\.checked = config\.couplingEnabled === true;/);
  assert.match(source, /"pickupReassignment", "couplingEnabled"\]\) bindScalarInput\(id\);/);
  // The Go model proposes only the flag. The page merges the patch into the draft.
  const config = couplingScenario();
  const off = { ...config, couplingEnabled: false };
  assert.deepEqual(Object.keys(off), Object.keys(config));
  assert.equal(JSON.stringify(off.couplingSites), JSON.stringify(config.couplingSites));
  assert.equal(JSON.stringify(off.couplingCorridors), JSON.stringify(config.couplingCorridors));
  assert.equal(off.couplingContract, "compact-pair-v1");
  const imported = editor.parseDocument(editor.serializeDocument(off)).scenario;
  assert.equal(imported.couplingEnabled, false);
  assert.equal(couplingText(imported), couplingText(off));
  // The saved draft keeps the members for a restore.
  const store = editor.openRecordStore(fakeIndexedDB(), editor.DRAFT_STORE);
  const record = editor.draftRecordFor({ scenario: off }, { scenario: JSON.stringify(config) }, { revision: 1, epoch: "e", serverStart: "s" });
  await store.put(DRAFT_KEY, record);
  assert.equal(couplingText((await store.get(DRAFT_KEY)).scenario), couplingText(off));
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
      if (item.duplicate) assert.throws(() => editor.parseDocument(text, { deferMetadata: true }), /repeats the member name/, item.name);
      // A file that the server accepts goes to Go unchanged. Go or the page rejects the others.
      else if (item.valid) assert.deepEqual(editor.parseDocument(text, { deferMetadata: true }).scenario, JSON.parse(item.text), item.name);
    }
  }
  // The page keeps its own messages for a file with canonical names only.
  const config = JSON.parse(decoderParity()[0].text);
  for (const [change, message] of [
    [(file) => { file.version = 6; }, /The version field must be 1, 2, 3, 4, or 5/],
    [(file) => { delete file.version; }, /The version field must be 1, 2, 3, 4, or 5/],
    [(file) => { file.network = null; }, /The network field must be an object/],
    [(file) => { delete file.network; file["vers\u0131on"] = 1; }, /no format field and no network field/],
  ]) {
    const file = structuredClone(config); change(file);
    assert.throws(() => editor.parseDocument(JSON.stringify(file), { deferMetadata: true }), message);
  }
  const deferred = { ...config, Network: null, Version: null };
  assert.deepEqual(editor.parseDocument(JSON.stringify(deferred), { deferMetadata: true }).scenario, deferred);
});

test("the import folds a member name as Go strings.EqualFold does", () => {
  for (const [name, other, same] of [["network", "NETWORK", true], ["network", "networ\u212a", true], ["Nodes", "Node\u017f", true], ["version", "vers\u0131on", false], ["Class", "Cla\u00df", false], ["a_b", "a-b", false], ["versions", "version", false]]) {
    assert.equal(editor.foldName(name) === editor.foldName(other), same, `${name} ${other}`);
  }
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

test("a Go replacement gives the imported scenario its canonical member names", () => {
  const scenario = { NAME: "Old", version: 1, network: {} };
  assert.deepEqual(editor.importedScenario(scenario, { patch: { fleet: [] } }), { NAME: "Old", version: 1, network: {}, fleet: [] });
  const replaced = editor.importedScenario(scenario, { replace: { version: 1, name: "Old", network: {}, fleet: [{ ID: "01" }] }, patch: { fleet: [{ ID: "01", BerthID: "a" }] } });
  assert.deepEqual(Object.keys(replaced), ["version", "name", "network", "fleet"]);
  assert.deepEqual(replaced.fleet, [{ ID: "01", BerthID: "a" }]);
});

test("Convert to trains shows for versions 1 to 4 only after checks pass, and keeps one undo step", () => {
  const config = connectedScenario();
  assert.deepEqual(editor.convertTrainsState(couplingScenario(), null, false), { hidden: true, disabled: true, hint: "" });
  assert.equal(editor.convertTrainsState({ ...config, version: 6 }, null, false).hidden, true);
  assert.match(editor.convertTrainsState(config, null, false).hint, /checks must run/);
  assert.match(editor.convertTrainsState(config, { valid: false }, false).hint, /Fix the errors/);
  assert.match(editor.convertTrainsState(config, { valid: true }, true).hint, /Wait/);
  const ready = editor.convertTrainsState(config, { valid: true }, false);
  assert.equal(ready.disabled, false); assert.match(ready.hint, /Undo goes back to version 1/);
  // The Go patch arrives with an unspecified member order.
  const patch = { couplingSites: [], version: 5, couplingCorridors: [], couplingEnabled: false, couplingContract: "compact-pair-v1" };
  const converted = editor.trainsScenario(config, patch);
  assert.deepEqual(Object.keys(converted), [...Object.keys(config), "couplingContract", "couplingEnabled", "couplingSites", "couplingCorridors"]);
  assert.equal(editor.serializeDocument(converted), editor.serializeDocument(editor.trainsScenario(config, { ...patch })));
  const before = editor.serializeDocument(config);
  const history = editor.createHistory({ scenario: config, background: null });
  history.replace({ scenario: converted, background: null }, true);
  assert.equal(history.snapshot.scenario.version, 5);
  history.undo();
  assert.equal(editor.serializeDocument(history.value.scenario), before);
  history.redo();
  assert.equal(editor.serializeDocument(history.value.scenario), editor.serializeDocument(converted));
});

test("coupling site and corridor rows follow the berth row focus and show only for version 5", () => {
  for (const [ids, removed, focus] of [[["a", "b", "c"], "a", "b"], [["a", "b", "c"], "b", "c"], [["a", "b", "c"], "c", "b"], [["a", "b"], "b", "a"], [["a"], "a", ""], [["a"], "x", ""]]) {
    assert.equal(editor.couplingFocusID(ids, removed), focus, `${ids} ${removed}`);
  }
  const html = fs.readFileSync(path.join(__dirname, "editor.html"), "utf8");
  assert.match(html, /<button id="convertTrains" class="wide" type="button" hidden>Convert to trains<\/button>/);
  for (const [panel, button, rows] of [["couplingSitesPanel", "addCouplingSite", "couplingSiteRows"], ["couplingCorridorsPanel", "addCouplingCorridor", "couplingCorridorRows"]]) {
    assert.match(html, new RegExp(`<section id="${panel}" hidden>\\s*<div class="section-title"><h2 id="${panel.replace("Panel", "Heading")}" tabindex="-1">[^<]+</h2><button id="${button}" type="button">Add [a-z]+</button></div>`));
    assert.match(html, new RegExp(`<div id="${rows}"></div>`));
  }
  const source = fs.readFileSync(path.join(__dirname, "editor.js"), "utf8");
  assert.match(source, /\$\("#couplingSitesPanel"\)\.hidden = \$\("#couplingCorridorsPanel"\)\.hidden = !shown;/);
  assert.match(source, /const shown = config\.version === 5, laneID = selectedLaneID\(\);/);
  assert.match(source, /if \(removed && keyboard && ownsFocus && !button\.isConnected\) \{\n[^\n]+\n\s*else focusCouplingRow\(kind, couplingFocusID\(ids, command\.id\)\);/);
  assert.match(source, /queueCouplingEdit\(command, \{ button, keyboard: event\.detail === 0 \}\);/);
  assert.match(source, /if \(config === draft\(\)\) trainsVerdict = \{ valid: results\.valid === true && !results\.errors\.length \};/);
  assert.match(source, /if \(!checksUnchanged\) \{ trainsVerdict = null; checks\.schedule\(\); \}/);
  assert.match(source, /renderRailArrivals\(config, "departure"\); renderCoupling\(config\);/);
});

test("corridor rows are built again when a guideway changes, and Remove names the guideway it shows", () => {
  const sites = [{ id: "a" }, { id: "b" }];
  const corridor = (laneIds) => [{ id: "c", assemblySiteId: "a", splitSiteId: "b", laneIds }];
  // The same IDs and path lengths with other guideways need new rows.
  assert.notEqual(editor.couplingLayout(sites, corridor(["x", "y"])), editor.couplingLayout(sites, corridor(["x", "z"])));
  assert.notEqual(editor.couplingLayout(sites, corridor(["x", "y"])), editor.couplingLayout(sites, corridor(["y", "x"])));
  assert.notEqual(editor.couplingLayout(sites, corridor(["x"])), editor.couplingLayout([{ id: "a" }, { id: "d" }], corridor(["x"])));
  assert.equal(editor.couplingLayout(sites, corridor(["x"])), editor.couplingLayout(structuredClone(sites), corridor(["x"])));
  assert.equal(editor.couplingLayout(sites, corridor(null)), editor.couplingLayout(sites, corridor([])));
  const source = fs.readFileSync(path.join(__dirname, "editor.js"), "utf8");
  assert.match(source, /const layout = couplingLayout\(sites, corridors\);\n\s*if \(layout !== drawnCoupling/);
  assert.match(source, /remove\.dataset\.couplingLane = String\(laneID\); remove\.dataset\.couplingCount = String\(corridor\.laneIds\.length\);/);
  // The page sends the count that the row shows, not the count of the live draft.
  assert.match(source, /Object\.assign\(command, \{ index: Number\(index\), count: Number\(button\.dataset\.couplingCount\), laneId: button\.dataset\.couplingLane \}\);/);
});

test("guideway Remove in a corridor path moves the focus to the next or previous guideway", () => {
  for (const [count, index, focus] of [[3, 0, 0], [3, 1, 1], [3, 2, 1], [2, 1, 0], [2, 0, 0], [1, 0, -1]]) {
    assert.equal(editor.corridorLaneFocus(count, index), focus, `${count} ${index}`);
  }
  const source = fs.readFileSync(path.join(__dirname, "editor.js"), "utf8");
  assert.match(source, /removed = changed && \["removeSite", "removeCorridor", "removeCorridorLane"\]\.includes\(command\.action\);/);
  assert.match(source, /if \(command\.action === "removeCorridorLane"\) focusCorridorLane\(command\.id, corridorLaneFocus\(command\.count, command\.index\)\);/);
  // With Add not available, the focus goes to an element that can take it.
  assert.match(source, /else if \(!add\.disabled\) add\.focus\(\);\n\s*else \$\(kind === "site" \? "#couplingSitesHeading" : "#couplingCorridorsHeading"\)\.focus\(\);/);
  assert.match(source, /\(lane \|\| \(add\.disabled \? row\.querySelector\('\[data-coupling-action="removeCorridor"\]'\) : add\)\)\.focus\(\);/);
});

test("undo and redo move the focus from a removed coupling row to a near row", () => {
  for (const [before, after, id, focus] of [
    [["a", "b"], ["b"], "a", "b"], [["a", "b", "c"], ["a", "c"], "b", "c"], [["a", "b"], ["a"], "b", "a"],
    [["a"], [], "a", ""], [["a", "b"], ["a", "b"], "b", "b"], [["a"], ["a"], "x", ""], [["a", "b", "c"], ["a"], "b", "a"],
  ]) assert.equal(editor.couplingUndoFocusID(before, after, id), focus, `${before} ${after} ${id}`);
  const source = fs.readFileSync(path.join(__dirname, "editor.js"), "utf8");
  assert.match(source, /const couplingKind = \$\("#couplingSiteRows"\)\.contains\(focused\) \? "site" : \$\("#couplingCorridorRows"\)\.contains\(focused\) \? "corridor" : "";/);
  assert.match(source, /if \(focused\.isConnected \|\| document\.activeElement !== document\.body\) return;/);
  assert.match(source, /focusCouplingRow\(couplingKind, couplingUndoFocusID\(ids\(before\), ids\(draft\(\)\), focused\.dataset\.couplingId\)\);/);
});

test("the lane class options show the classes that native allows and need version 3, 4, or 5", () => {
  const config = connectedScenario();
  const lane = config.network.Lanes[0].ID;
  for (const version of [1, 2]) {
    assert.deepEqual(editor.laneClassState({ ...config, version }, lane), { classes: ["legacy", "compact"], disabled: true, hint: "Vehicle classes need project version 3, 4, or 5." });
  }
  for (const version of [3, 4]) {
    const state = editor.laneClassState({ ...config, version }, lane);
    assert.deepEqual(state.classes, ["legacy", "compact"]); assert.equal(state.disabled, false);
    assert.match(state.hint, /no class list, so Legacy and Compact pods can use it/); assert.doesNotMatch(state.hint, /coupling/);
  }
  const classed = structuredClone(config);
  classed.version = 5; classed.network.Lanes[0].VehicleClasses = ["express", "compact"];
  assert.deepEqual(editor.laneClassState(classed, lane), { classes: ["compact", "express"], disabled: false, hint: "A coupling site needs a straight guideway with Compact only." });
  const source = fs.readFileSync(path.join(__dirname, "editor.js"), "utf8");
  for (const name of ["legacy", "compact", "group", "express"]) assert.match(source, new RegExp(`<input data-edit="lane-class" data-class="${name}" type="checkbox">`));
  assert.match(source, /queueGeometryEdit\(\{ action: "laneClasses", id, value \}, \{ controls: \[control\] \}\);/);
  assert.match(source, /box\.checked = classes\.classes\.includes\(box\.dataset\.class\); box\.disabled = classes\.disabled;/);
});

test("station queue spacing is not available in a version 4 project", () => {
  const html = fs.readFileSync(path.join(__dirname, "editor.html"), "utf8");
  assert.match(html, /<p class="hint" id="stationQueueSpacingHint" hidden>Station queue spacing needs project version 3 or 5\. A version 4 project cannot use it\.<\/p>/);
  const source = fs.readFileSync(path.join(__dirname, "editor.js"), "utf8");
  assert.match(source, /const queueLocked = config\.version === 4;\n\s*\$\("#stationQueueSpacing"\)\.disabled = queueLocked; \$\("#stationQueueSpacingHint"\)\.hidden = !queueLocked;/);
});

test("experimental project flags round trip and reject non-Boolean values", () => {
  for (const field of ["stationBuffers", "pickupReassignment"]) {
    const config = connectedScenario();
    delete config[field];
    assert.equal(editor.parseDocument(editor.serializeDocument(config)).scenario[field], false);
    for (const enabled of [false, true]) {
      config[field] = enabled;
      assert.equal(editor.parseDocument(editor.serializeDocument(config)).scenario[field], enabled);
    }
    for (const value of [null, 0, 1, "true", "false", [], {}]) {
      config[field] = value;
      assert.throws(() => editor.parseDocument(editor.serializeDocument(config)), /setting must be true or false/);
    }
  }
});

test("a station delete works on a draft with no demand profiles", () => {
  // An older browser export has no demandProfiles field. The import adds an
  // empty list. The helpers also accept a draft with no such field.
  const scenario = connectedScenario();
  delete scenario.demandProfiles;
  const { scenario: imported } = editor.parseDocument(JSON.stringify({ format: "podsim", version: 1, scenario }));
  assert.deepEqual(imported.demandProfiles, []);

  const cases = [
    { name: "field missing", config: scenario },
    { name: "imported export", config: imported },
  ];
  for (const { name, config } of cases) {
    const beta = config.network.Stations[1];
    assert.equal(editor.stationFlowCount(config, beta.ID), 0, name);
    const deleted = editor.deleteStation(config, beta.ID);
    assert.deepEqual(deleted.network.Stations.map((station) => station.ID), [config.network.Stations[0].ID], name);
  }
});

test("station drag moves its component nodes and internal curve as one group", () => {
  let config = editor.addStation(editor.emptyConfig(), 200, 140, { name: "Central" });
  const station = config.network.Stations[0];
  const lane = config.network.Lanes.find((item) => item.From === station.Entry && item.To === station.Exit);
  lane.Control = { X: 200, Y: 100 };
  const before = config.network.Nodes.map((node) => ({ ...node.Position }));

  config = editor.moveStation(config, station.ID, 20, -10);
  config.network.Nodes.forEach((node, index) => {
    assert.equal(node.Position.X, before[index].X + 20);
    assert.equal(node.Position.Y, before[index].Y - 10);
  });
  assert.deepEqual(config.network.Lanes.find((item) => item.ID === lane.ID).Control, { X: 220, Y: 90 });
});

function nodePosition(config, id) {
  return config.network.Nodes.find((node) => node.ID === id).Position;
}

test("station drag moves its berth chain", () => {
  let { config, arrival, departure } = chainScenario();
  const [alpha, beta] = config.network.Stations;
  config = editor.addJunction(config, 64, 40);
  const approach = config.network.Nodes.at(-1).ID;
  config = editor.addLane(config, approach, alpha.Entry, false);
  Object.assign(config.network.Lanes.at(-1), { StationID: alpha.ID, StationRole: "approach" });
  const moved = editor.moveStation(config, alpha.ID, 20, -10);
  const owners = editor.stationNodeOwners(config);

  for (const id of [alpha.Entry, alpha.Exit, alpha.Berths[0].Node, arrival, departure, approach]) {
    assert.deepEqual(nodePosition(moved, id), { X: nodePosition(config, id).X + 20, Y: nodePosition(config, id).Y - 10 }, id);
    assert.equal(owners.get(id), alpha.ID, id);
  }
  assert.deepEqual(nodePosition(moved, beta.Entry), nodePosition(config, beta.Entry));
  assert.match(editor.deleteNode(config, arrival).error, /station or berth/);
});

test("a shared road node stays out of the station", () => {
  let { config, arrival, departure } = chainScenario();
  const [alpha, beta] = config.network.Stations;
  config = editor.addJunction(config, 64, 40);
  const road = config.network.Nodes.at(-1).ID;
  config = editor.addLane(config, beta.Exit, road, false);
  config = editor.addLane(config, road, alpha.Entry, false);
  Object.assign(config.network.Lanes.at(-1), { StationID: alpha.ID, StationRole: "approach" });

  assert.equal(editor.stationNodeOwners(config).has(road), false);
  assert.deepEqual(nodePosition(editor.moveStation(config, alpha.ID, 20, -10), road), nodePosition(config, road));
  assert.equal(editor.deleteNode(config, road).error, "");

  const deleted = editor.deleteStation(config, alpha.ID);
  const nodeIDs = new Set(deleted.network.Nodes.map((node) => node.ID));
  assert.ok(nodeIDs.has(road));
  assert.ok(deleted.network.Lanes.some((lane) => lane.From === beta.Exit && lane.To === road));
  for (const id of [alpha.Entry, alpha.Exit, alpha.Berths[0].Node, arrival, departure]) assert.equal(nodeIDs.has(id), false, id);
  assert.ok(deleted.network.Lanes.every((lane) => nodeIDs.has(lane.From) && nodeIDs.has(lane.To)));
});

// assertNear checks that two points are less than 1e-6 m apart.
function assertNear(actual, want, name) {
  assert.ok(Math.hypot(actual.X - want.X, actual.Y - want.Y) < 1e-6, `${name}: got ${JSON.stringify(actual)}, want ${JSON.stringify(want)}`);
}

// angleGap gives the difference in degrees between two bearings.
function angleGap(a, b) {
  return Math.abs((((a - b) % 360) + 540) % 360 - 180);
}

// assertLanesOnNodes checks that each lane in after has the same ID, From
// node, and To node as in before, and that after has both end nodes.
function assertLanesOnNodes(before, after, name) {
  const ends = (config) => config.network.Lanes.map((lane) => [lane.ID, lane.From, lane.To]);
  assert.deepEqual(ends(after), ends(before), name);
  const ids = new Set(after.network.Nodes.map((node) => node.ID));
  for (const lane of after.network.Lanes) assert.ok(ids.has(lane.From) && ids.has(lane.To), `${name} ${lane.ID}`);
}

// leftOf gives the unit vector to the left of the direction of travel from
// entry to exit. The map Y axis points down.
function leftOf(entry, exit) {
  const length = Math.hypot(exit.X - entry.X, exit.Y - entry.Y);
  return { X: (exit.Y - entry.Y) / length, Y: (entry.X - exit.X) / length };
}

// CLEARANCE is the clearance in meters that the simulation keeps between
// pods, sim.Clearance in the Go code.
const CLEARANCE = 12;

// segmentDistance gives the distance in meters from at to the line segment
// from a to b.
function segmentDistance(at, a, b) {
  const dx = b.X - a.X; const dy = b.Y - a.Y; const size = dx * dx + dy * dy;
  const t = size ? Math.min(1, Math.max(0, ((at.X - a.X) * dx + (at.Y - a.Y) * dy) / size)) : 0;
  return Math.hypot(at.X - a.X - t * dx, at.Y - a.Y - t * dy);
}

// lanePoints gives points along a lane, at most 1 m apart.
function lanePoints(config, lane) {
  const from = nodePosition(config, lane.From); const to = nodePosition(config, lane.To);
  const control = lane.Control || { X: (from.X + to.X) / 2, Y: (from.Y + to.Y) / 2 };
  const count = Math.max(1, Math.ceil(editor.laneLength(config, lane)));
  return Array.from({ length: count + 1 }, (_, index) => {
    const t = index / count; const u = 1 - t;
    return { X: u * u * from.X + 2 * u * t * control.X + t * t * to.X, Y: u * u * from.Y + 2 * u * t * control.Y + t * t * to.Y };
  });
}

// laneGap gives the distance in meters between two lanes, from the points
// of one lane to the parts of the other. A crossing gives a gap below 1 m.
// Two lanes whose end and control points are at least CLEARANCE apart on
// the X or the Y axis give Infinity.
function laneGap(config, first, second) {
  const box = (lane) => {
    const points = [nodePosition(config, lane.From), nodePosition(config, lane.To), ...(lane.Control ? [lane.Control] : [])];
    return { low: { X: Math.min(...points.map((at) => at.X)), Y: Math.min(...points.map((at) => at.Y)) }, high: { X: Math.max(...points.map((at) => at.X)), Y: Math.max(...points.map((at) => at.Y)) } };
  };
  const a = box(first); const b = box(second);
  if (["X", "Y"].some((axis) => a.low[axis] - b.high[axis] >= CLEARANCE || b.low[axis] - a.high[axis] >= CLEARANCE)) return Infinity;
  const path = lanePoints(config, second); let gap = Infinity;
  for (const at of lanePoints(config, first)) for (let index = 1; index < path.length; index += 1) gap = Math.min(gap, segmentDistance(at, path[index - 1], path[index]));
  return gap;
}

// assertChainRow checks a berth that addBerth added to a berth chain
// station. before and after are the configs before and after the add. The
// chain gets one row: one berth, three nodes and four lanes. The new row is
// the last row moved by pitch. Each new lane is at least CLEARANCE from each
// lane that shares no node with it. The add gives no new check error, and
// Remove on the new berth gives before again.
function assertChainRow(change, name) {
  const { before, after, stationID, pitch } = change;
  const find = (config) => config.network.Stations.find((item) => item.ID === stationID);
  const rows = editor.berthChain(after, find(after));
  assert.equal(rows.length, find(before).Berths.length + 1, name);
  assert.equal(after.network.Nodes.length - before.network.Nodes.length, 3, name);
  assert.equal(after.network.Lanes.length - before.network.Lanes.length, 4, name);
  const [last, added] = rows.slice(-2);
  for (const part of ["arrival", "departure"]) {
    const at = nodePosition(after, last[part]);
    assertNear(nodePosition(after, added[part]), { X: at.X + pitch.X, Y: at.Y + pitch.Y }, `${name} ${part}`);
  }
  const at = nodePosition(after, last.berth.Node);
  assertNear(nodePosition(after, added.berth.Node), { X: at.X + pitch.X, Y: at.Y + pitch.Y }, `${name} berth`);
  const kept = new Set(before.network.Lanes.map((lane) => lane.ID));
  const lanes = after.network.Lanes.filter((lane) => !kept.has(lane.ID));
  assert.deepEqual(lanes.map((lane) => lane.StationRole), ["berth-access", "departure", "berth-access", "departure"], name);
  assert.ok(lanes.every((lane) => lane.StationID === stationID), name);
  for (const lane of lanes) {
    for (const other of before.network.Lanes) {
      if ([other.From, other.To].some((id) => id === lane.From || id === lane.To)) continue;
      assert.ok(laneGap(after, lane, other) >= CLEARANCE, `${name} ${lane.ID} ${other.ID}`);
    }
  }
  assert.deepEqual(editor.validateConfig(after), editor.validateConfig(before), name);
  assert.deepEqual(removedBerth(after, stationID, added.berth.ID), before, name);
}

// berthPitch gives the distance from the berth node before the last berth
// node of the station to the last berth node.
function berthPitch(config, station) {
  const [previous, last] = station.Berths.slice(-2).map((berth) => nodePosition(config, berth.Node));
  return { X: last.X - previous.X, Y: last.Y - previous.Y };
}

// Tottenham Court Road in the generated London project. The berths are 90 m
// and 165 m to the left of the entry-exit line.
const tottenhamCourtRoad = {
  entry: { X: -186.74321535145924, Y: -1227.1687914758268 },
  exit: { X: -5.481657944129211, Y: -1142.645139127687 },
  berths: [{ X: -58.07679309113132, Y: -1266.4746661350553 }, { X: -26.380423460578925, Y: -1334.447750162804 }],
};

test("the station bearing is the direction from the entry to the exit", () => {
  const cases = [
    { name: "right", exit: { X: 10, Y: 0 }, want: 90 },
    { name: "down", exit: { X: 0, Y: 10 }, want: 180 },
    { name: "left", exit: { X: -10, Y: 0 }, want: 270 },
    { name: "up", exit: { X: 0, Y: -10 }, want: 0 },
    { name: "up and to the right", exit: { X: 10, Y: -10 }, want: 45 },
    { name: "up and to the left", exit: { X: -10, Y: -10 }, want: 315 },
    { name: "entry and exit at one point", exit: { X: 0, Y: 0 }, want: 90 },
  ];
  for (const tc of cases) assert.ok(angleGap(editor.stationBearing({ X: 0, Y: 0 }, tc.exit), tc.want) < 1e-9, tc.name);
  assert.equal(Math.round(editor.stationBearing(tottenhamCourtRoad.entry, tottenhamCourtRoad.exit)), 115);
  const config = editor.addStation(editor.emptyConfig(), 200, 140, {});
  const [station] = config.network.Stations;
  assert.equal(editor.stationBearing(nodePosition(config, station.Entry), nodePosition(config, station.Exit)), 90);
});

test("a station turns around its center and keeps its lanes on its nodes", () => {
  let { config, arrival, departure } = chainScenario();
  const [alpha, beta] = config.network.Stations;
  config = editor.addJunction(config, 64, 40);
  const road = config.network.Nodes.at(-1).ID;
  config = editor.addLane(config, road, alpha.Entry, false);
  const roadLane = config.network.Lanes.at(-1).ID;
  const through = config.network.Lanes.find((lane) => lane.From === alpha.Entry && lane.To === alpha.Exit);
  through.Control = { X: 100, Y: 70 };
  // The center is the middle of the entry-exit line.
  const center = { X: 100, Y: 100 };
  const stationNodes = [alpha.Entry, alpha.Exit, alpha.Berths[0].Node, arrival, departure];
  const cases = [
    { name: "a quarter turn clockwise", degrees: 90, bearing: 180 },
    { name: "a quarter turn counterclockwise", degrees: -90, bearing: 0 },
    { name: "a small turn", degrees: 25, bearing: 115 },
    { name: "a full turn", degrees: 360, bearing: 90 },
  ];
  for (const tc of cases) {
    const turned = editor.rotateStation(config, alpha.ID, tc.degrees);
    const cos = Math.cos(tc.degrees * Math.PI / 180); const sin = Math.sin(tc.degrees * Math.PI / 180);
    const turn = (at) => ({ X: center.X + (at.X - center.X) * cos - (at.Y - center.Y) * sin, Y: center.Y + (at.X - center.X) * sin + (at.Y - center.Y) * cos });
    for (const id of stationNodes) assertNear(nodePosition(turned, id), turn(nodePosition(config, id)), `${tc.name} ${id}`);
    assertNear(turned.network.Lanes.find((lane) => lane.ID === through.ID).Control, turn(through.Control), `${tc.name} through curve`);
    assert.ok(angleGap(editor.stationBearing(nodePosition(turned, alpha.Entry), nodePosition(turned, alpha.Exit)), tc.bearing) < 1e-9, tc.name);
    for (const id of [beta.Entry, beta.Exit, beta.Berths[0].Node, road]) assert.deepEqual(nodePosition(turned, id), nodePosition(config, id), `${tc.name} ${id}`);
    assertLanesOnNodes(config, turned, tc.name);
    // Each lane between two station nodes keeps its length. The road lane
    // follows the entry.
    for (const lane of turned.network.Lanes.filter((item) => item.StationID === alpha.ID)) {
      const before = config.network.Lanes.find((item) => item.ID === lane.ID);
      assert.ok(Math.abs(editor.laneLength(turned, lane) - editor.laneLength(config, before)) < 1e-9, `${tc.name} ${lane.ID}`);
    }
    const entry = nodePosition(turned, alpha.Entry);
    assert.equal(editor.laneLength(turned, turned.network.Lanes.find((lane) => lane.ID === roadLane)), Math.hypot(entry.X - 64, entry.Y - 40), tc.name);
  }

  const set = editor.setStationBearing(config, alpha.ID, 240);
  assert.ok(angleGap(editor.stationBearing(nodePosition(set, alpha.Entry), nodePosition(set, alpha.Exit)), 240) < 1e-9);
  assertNear(nodePosition(set, alpha.Berths[0].Node), nodePosition(editor.rotateStation(config, alpha.ID, 150), alpha.Berths[0].Node), "set bearing");
  // A bearing of 450 is 90, the current bearing, so nothing changes.
  for (const bearing of [90, 450, Number.NaN]) assert.equal(editor.setStationBearing(config, alpha.ID, bearing), config, String(bearing));
  assert.equal(editor.setStationBearing(config, "gone", 180), config);
});

test("a station node drag keeps its lanes on the node", () => {
  const { config, arrival } = chainScenario();
  const [alpha] = config.network.Stations;
  for (const id of [alpha.Entry, alpha.Exit, alpha.Berths[0].Node, arrival]) {
    const moved = editor.moveNode(config, id, 80, 170);
    assert.deepEqual(nodePosition(moved, id), { X: 80, Y: 170 }, id);
    assertLanesOnNodes(config, moved, id);
    assert.deepEqual(editor.dragTargets(config, { type: "node", id }).stationIDs, [alpha.ID], id);
  }
});

test("a new berth goes one berth pitch past the last berth on the station axis", () => {
  const along = (berths) => ({ entry: { X: -36, Y: 0 }, exit: { X: 36, Y: 0 }, berths });
  const [first, second] = tottenhamCourtRoad.berths;
  const left = leftOf(tottenhamCourtRoad.entry, tottenhamCourtRoad.exit);
  const parking = { entry: { X: 620, Y: 460 }, exit: { X: 340, Y: 460 }, berths: [{ X: 480, Y: 420 }, { X: 480, Y: 365 }] };
  const cases = [
    { name: "one berth to the right, as a new station has it", station: along([{ X: 0, Y: 30 }]), want: { X: 0, Y: 60 } },
    { name: "two berths use the farther berth", station: along([{ X: 0, Y: 30 }, { X: 0, Y: 55 }]), want: { X: 0, Y: 85 } },
    { name: "the berth order does not matter", station: along([{ X: 0, Y: 55 }, { X: 0, Y: 30 }]), want: { X: 0, Y: 85 } },
    { name: "a berth to the left", station: along([{ X: 0, Y: -30 }]), want: { X: 0, Y: -60 } },
    { name: "a berth off the center keeps its offset", station: along([{ X: 20, Y: 30 }]), want: { X: 20, Y: 60 } },
    { name: "berths on both sides use the side of the mean", station: along([{ X: 0, Y: 30 }, { X: 0, Y: -60 }, { X: 0, Y: 60 }]), want: { X: 0, Y: 90 } },
    { name: "a farther berth on the other side moves the mean to that side", station: along([{ X: 0, Y: 30 }, { X: 0, Y: -60 }]), want: { X: 0, Y: -90 } },
    { name: "no berths", station: along([]), want: { X: 0, Y: 30 } },
    { name: "a station that points down", station: { entry: { X: 0, Y: -36 }, exit: { X: 0, Y: 36 }, berths: [{ X: -30, Y: 0 }] }, want: { X: -60, Y: 0 } },
    { name: "Tottenham Court Road", station: tottenhamCourtRoad, want: { X: second.X + left.X * editor.BERTH_PITCH, Y: second.Y + left.Y * editor.BERTH_PITCH } },
    { name: "the example Parking station, with berths 55 m apart", station: parking, want: { X: 480, Y: 335 } },
  ];
  for (const tc of cases) assertNear(editor.nextBerthPosition(tc.station), tc.want, tc.name);

  // The Tottenham Court Road berth is on the side of the other berths.
  const side = (at) => Math.sign((tottenhamCourtRoad.exit.X - tottenhamCourtRoad.entry.X) * (at.Y - tottenhamCourtRoad.entry.Y) - (tottenhamCourtRoad.exit.Y - tottenhamCourtRoad.entry.Y) * (at.X - tottenhamCourtRoad.entry.X));
  assert.equal(side(editor.nextBerthPosition(tottenhamCourtRoad)), side(first));

  // In the example network, the new Parking berth keeps the clearance from
  // the bypass junction and its lanes.
  const [branch, bypass, merge] = [{ X: 300, Y: 260 }, { X: 470, Y: 300 }, { X: 670, Y: 260 }];
  const added = editor.nextBerthPosition(parking);
  for (const [from, to] of [[branch, bypass], [bypass, merge]]) assert.ok(segmentDistance(added, from, to) >= CLEARANCE, JSON.stringify(to));

  // addBerth places each berth on the axis of a turned station.
  let config = editor.addStation(editor.emptyConfig(), 200, 140, {});
  const stationID = config.network.Stations[0].ID;
  config = editor.setStationBearing(config, stationID, 30);
  for (let count = 2; count <= 4; count += 1) {
    config = addedBerth(config, stationID);
    const [previous, last] = config.network.Stations[0].Berths.slice(-2).map((berth) => nodePosition(config, berth.Node));
    // At a bearing of 30 degrees, the right of the direction of travel is
    // the direction 120 degrees clockwise from up.
    const axis = { X: Math.cos(30 * Math.PI / 180), Y: Math.sin(30 * Math.PI / 180) };
    assertNear(last, { X: previous.X + axis.X * editor.BERTH_PITCH, Y: previous.Y + axis.Y * editor.BERTH_PITCH }, `berth ${count}`);
  }
  for (const lane of config.network.Lanes) assert.ok(editor.laneLength(config, lane) >= editor.MIN_LANE_LENGTH, lane.ID);
});

test("add berth on a berth chain station adds a chain row", () => {
  const { config, arrival, departure } = chainScenario();
  const [alpha, beta] = config.network.Stations;
  // A station that addStation makes is not a chain, so addBerth adds a
  // berth with a lane from the entry and a lane to the exit.
  assert.equal(editor.berthChain(config, beta), null);
  const star = addedBerth(config, beta.ID);
  const berth = star.network.Stations[1].Berths.at(-1);
  assert.deepEqual(star.network.Nodes.slice(config.network.Nodes.length).map((node) => node.ID), [berth.Node]);
  assert.deepEqual(star.network.Lanes.slice(config.network.Lanes.length).map((lane) => [lane.From, lane.To]), [[beta.Entry, berth.Node], [berth.Node, beta.Exit]]);

  assert.deepEqual(editor.berthChain(config, alpha).map((row) => [row.arrival, row.berth.ID, row.departure]), [[arrival, alpha.Berths[0].ID, departure]]);
  // With one row, the pitch goes from the middle of the entry-exit line at
  // (100, 100) to the berth node at (100, 130).
  const two = addedBerth(config, alpha.ID);
  assertChainRow({ before: config, after: two, stationID: alpha.ID, pitch: { X: 0, Y: 30 } }, "one row");
  assertChainRow({ before: two, after: addedBerth(two, alpha.ID), stationID: alpha.ID, pitch: { X: 0, Y: 30 } }, "two rows");
  // Remove on a berth that is not the last row keeps its arrival and
  // departure nodes, because the next row uses them.
  const removed = removedBerth(two, alpha.ID, alpha.Berths[0].ID);
  assert.deepEqual(removed.network.Nodes.map((node) => node.ID), two.network.Nodes.map((node) => node.ID).filter((id) => id !== alpha.Berths[0].Node));
});

test("a lane polyline is the path of the lane in the simulation", () => {
  // TestLanePolylineGolden in internal/sim writes the file from
  // sim.Network.lanePoints.
  const file = JSON.parse(fs.readFileSync(path.join(repoRoot, "internal", "sim", "testdata", "lane_polylines.json"), "utf8"));
  const config = editor.normalizeConfig({ network: file.network });
  assert.ok(file.network.Lanes.filter((lane) => lane.Control).length >= 3);
  file.network.Lanes.forEach((lane, index) => {
    const got = editor.lanePolyline(nodePosition(config, lane.From), nodePosition(config, lane.To), lane.Control);
    assert.equal(got.length, file.points[index].length, lane.ID);
    got.forEach((at, point) => assertNear(at, file.points[index][point], `${lane.ID} point ${point}`));
  });
});

test("add berth on a berth chain station compares the new lanes with each other", () => {
  const { config } = chainScenario();
  const [alpha] = config.network.Stations;
  // With the berth at (80, 140), the new arrival link and the new lane out
  // of the berth are 5.4 m apart. They share no node.
  const moved = editor.moveNode(config, alpha.Berths[0].Node, 80, 140);
  assert.deepEqual(editor.validateConfig(moved), []);
  const result = editor.addBerth(moved, alpha.ID);
  assert.equal(result.config, moved);
  const [, lane, other] = /^No space for another berth at Alpha\. New lane (\S+) would be nearer than 12 m to lane (\S+)\.$/.exec(result.error) || [];
  const kept = new Set(moved.network.Lanes.map((item) => item.ID));
  assert.ok(lane && other && !kept.has(lane) && !kept.has(other), result.error);
});

test("remove berth on the last chain row keeps a node that another lane uses", () => {
  const { config } = chainScenario();
  const [alpha, beta] = config.network.Stations;
  const two = addedBerth(config, alpha.ID);
  const last = editor.berthChain(two, two.network.Stations[0]).at(-1);
  // Without other lanes on the row nodes, the removal gives a valid config.
  assert.deepEqual(editor.validateConfig(two), []);
  assert.deepEqual(editor.validateConfig(removedBerth(two, alpha.ID, last.berth.ID)), []);
  const departure = JSON.parse(JSON.stringify(two));
  departure.network.Lanes.find((lane) => lane.From === beta.Exit && lane.To === alpha.Entry).To = last.departure;
  const cases = [
    { name: "a shared departure node", config: editor.addLane(departure, alpha.Exit, alpha.Entry, false), node: last.departure },
    { name: "a shared arrival node", config: editor.addLane(two, last.arrival, beta.Entry, false), node: last.arrival },
  ];
  for (const tc of cases) {
    assert.deepEqual(editor.validateConfig(tc.config), [], tc.name);
    const lane = tc.config.network.Lanes.find((item) => (item.From === tc.node || item.To === tc.node) && item.StationID !== alpha.ID);
    const result = editor.removeBerth(tc.config, alpha.ID, last.berth.ID);
    assert.equal(result.config, tc.config, tc.name);
    assert.equal(result.error, `Berth ${last.berth.ID} at Alpha stays, because lane ${lane.ID} also uses node ${tc.node}.`, tc.name);
  }
});

// withoutStationFields gives a copy of config without StationID and
// StationRole on the lanes of the station, as a legacy import gives them.
function withoutStationFields(config, stationID) {
  const plain = JSON.parse(JSON.stringify(config));
  for (const lane of plain.network.Lanes.filter((item) => item.StationID === stationID)) { delete lane.StationID; delete lane.StationRole; }
  return plain;
}

test("import infers the station lanes that the simulation infers", () => {
  // TestStationLaneRolesGolden in internal/sim writes the file from
  // sim.inferStationLaneRoles.
  const file = JSON.parse(fs.readFileSync(path.join(repoRoot, "internal", "sim", "testdata", "station_lane_roles.json"), "utf8"));
  assert.ok(file.cases.some((tc) => tc.name === "chain") && file.cases.some((tc) => tc.name === "star") && file.cases.some((tc) => tc.name === "ring"));
  for (const tc of file.cases) {
    const input = JSON.parse(JSON.stringify(tc.network));
    const config = editor.normalizeConfig({ network: tc.network });
    assert.deepEqual(tc.network, input, `${tc.name}: the input stays the same`);
    assert.deepEqual(config.network.Lanes.map((lane) => ({ ID: lane.ID, StationID: lane.StationID || "", StationRole: lane.StationRole || "" })), tc.lanes, tc.name);
    // The inferred fields do not change the checks.
    const plain = { ...config, network: { ...config.network, Lanes: JSON.parse(JSON.stringify(tc.network.Lanes)) } };
    assert.deepEqual(editor.validateConfig(config), editor.validateConfig(plain), tc.name);
    // An explicit field stays.
    for (const [index, lane] of tc.network.Lanes.entries()) if (lane.StationRole) assert.deepEqual(config.network.Lanes[index], lane, `${tc.name} ${lane.ID}`);
  }
});

test("import infers no station lanes on a draft with a lane speed that the simulation rejects", () => {
  // The route search runs before the checks, on a restored draft too. With
  // the speed of -0.5 on lane b-a, the search found a route back from the
  // berth that went around a and b without end. A child process with a
  // time limit runs each case, so such a loop fails the test and does not
  // stop the test run.
  const nodes = ["e", "a", "b", "t", "x"].map((id, index) => ({ ID: id, Position: { X: 30 * index, Y: 0 } }));
  const lanes = [["e", "a"], ["a", "b"], ["b", "t"], ["t", "x"], ["e", "x"], ["b", "a"]].map(([from, to]) => ({ ID: `${from}-${to}`, From: from, To: to, SpeedLimit: 1 }));
  const stations = [{ ID: "s", Name: "S", Entry: "e", Exit: "x", Berths: [{ ID: "s-1", Node: "t" }], ParkingOnly: false }];
  const script = `const editor = require(${JSON.stringify(path.join(__dirname, "editor.js"))});
    const config = editor.normalizeConfig(JSON.parse(process.argv[1]));
    process.stdout.write(JSON.stringify(config.network.Lanes.filter((lane) => lane.StationID).map((lane) => lane.ID)));`;
  for (const speed of [1, -0.5, 0, null, "fast"]) {
    const network = { Nodes: nodes, Lanes: lanes.map((lane) => (lane.ID === "b-a" ? { ...lane, SpeedLimit: speed } : lane)), Stations: stations };
    const output = execFileSync(process.execPath, ["-e", script, JSON.stringify({ network })], { encoding: "utf8", timeout: 10000 });
    assert.deepEqual(JSON.parse(output), speed === 1 ? ["e-a", "a-b", "b-t", "t-x", "e-x"] : [], `speed ${speed}`);
  }
});

// longRouteNetwork gives a station whose berths are at the end of one
// chain of 1,000 lanes. berthNodes gives the node of each berth, and nodes
// b-0 to b-(berths - 1) exist.
function longRouteNetwork(berths, berthNodes) {
  const nodes = [{ ID: "e", Position: { X: 0, Y: -30 } }, { ID: "x", Position: { X: 60, Y: -30 } }];
  const lanes = [{ ID: "through", From: "e", To: "x", SpeedLimit: 14 }];
  let previous = "e";
  for (let index = 0; index < 1000; index += 1) {
    nodes.push({ ID: `c-${index}`, Position: { X: 0, Y: 30 * index } });
    lanes.push({ ID: `chain-${index}`, From: previous, To: `c-${index}`, SpeedLimit: 14 });
    previous = `c-${index}`;
  }
  for (let index = 0; index < berths; index += 1) {
    nodes.push({ ID: `b-${index}`, Position: { X: 30 + index, Y: 30000 } });
    lanes.push({ ID: `in-${index}`, From: previous, To: `b-${index}`, SpeedLimit: 14 }, { ID: `out-${index}`, From: `b-${index}`, To: "x", SpeedLimit: 14 });
  }
  const station = { ID: "s", Name: "S", Entry: "e", Exit: "x", Berths: berthNodes.map((node, index) => ({ ID: `s-${index}`, Node: node })), ParkingOnly: false };
  return { Nodes: nodes, Lanes: lanes, Stations: [station] };
}

test("import keeps one role for each lane and infers nothing on a station that the checks reject", () => {
  // Each of the 200 berths has the chain in its route. The inference keeps
  // one role for each lane of the station, not one for each lane of each
  // route.
  const berths = [...Array(200).keys()].map((index) => `b-${index}`);
  const valid = longRouteNetwork(200, berths);
  assert.equal(editor.inferStationLanes(valid), valid.Lanes.length);
  assert.ok(valid.Lanes.every((lane) => lane.StationID === "s"));
  assert.ok(valid.Lanes.filter((lane) => lane.ID.startsWith("chain-")).every((lane) => lane.StationRole === "berth-access"));
  // The simulation rejects 201 berths in a station, and a berth node that
  // two berths use.
  const cases = [
    { name: "201 berths", network: longRouteNetwork(201, [...berths, "b-200"]) },
    { name: "a shared berth node", network: longRouteNetwork(2, ["b-0", "b-1", "b-0"]) },
  ];
  for (const tc of cases) {
    assert.equal(editor.inferStationLanes(tc.network), 0, tc.name);
    assert.ok(tc.network.Lanes.every((lane) => !lane.StationID && !lane.StationRole), tc.name);
  }
  // A restored draft with 1,000 berths on the same node at the end of the
  // chain made the editor keep one role for each lane of each route. A
  // child process with a time limit runs the case.
  const draft = { network: longRouteNetwork(1, Array(1000).fill("b-0")) };
  const script = `const editor = require(${JSON.stringify(path.join(__dirname, "editor.js"))});
    const config = editor.normalizeConfig(JSON.parse(require("node:fs").readFileSync(0, "utf8")));
    process.stdout.write(String(config.network.Lanes.filter((lane) => lane.StationID || lane.StationRole).length));`;
  assert.equal(execFileSync(process.execPath, ["-e", script], { input: JSON.stringify(draft), encoding: "utf8", timeout: 10000 }), "0");
});

test("remove berth on an import without station lane fields gives the config before the add", () => {
  const star = connectedScenario();
  const chain = chainScenario().config;
  for (const [name, config] of [["star", star], ["chain", chain]]) {
    const [alpha] = config.network.Stations;
    const two = addedBerth(config, alpha.ID);
    const plain = withoutStationFields(two, alpha.ID);
    assert.ok(plain.network.Lanes.every((lane) => lane.StationID !== alpha.ID), name);
    // The import gives each station lane the values that addStation,
    // addBerth and the generators give. Thus Remove accepts the new berth.
    const { scenario: imported } = editor.parseDocument(editor.serializeDocument(plain, null));
    assert.deepEqual(imported, two, name);
    const berth = imported.network.Stations[0].Berths.at(-1);
    assert.deepEqual(removedBerth(imported, alpha.ID, berth.ID), config, name);
  }
});

test("add berth on an imported chain without station lane fields makes station lanes", () => {
  const { config } = chainScenario();
  const [alpha] = config.network.Stations;
  // The import infers the chain lanes, so the new row and Remove on the
  // new berth give the import again.
  const imported = editor.normalizeConfig(withoutStationFields(config, alpha.ID));
  assert.deepEqual(imported, config);
  assertChainRow({ before: imported, after: addedBerth(imported, alpha.ID), stationID: alpha.ID, pitch: { X: 0, Y: 30 } }, "imported chain");
});

test("remove berth keeps a berth when a lane that is not a station lane uses its node", () => {
  const config = connectedScenario();
  const [alpha, beta] = config.network.Stations;
  const two = addedBerth(config, alpha.ID);
  const berth = two.network.Stations[0].Berths.at(-1);
  // With only its station lanes on the berth node, the removal gives the
  // config before the add.
  assert.deepEqual(removedBerth(two, alpha.ID, berth.ID), config);
  const road = editor.addLane(two, berth.Node, beta.Entry, false);
  let other = editor.addJunction(two, 100, 220);
  other = editor.addLane(other, other.network.Nodes.at(-1).ID, berth.Node, false);
  other = editor.addLane(other, berth.Node, beta.Exit, false);
  Object.assign(other.network.Lanes.at(-1), { StationID: beta.ID, StationRole: "departure" });
  const [first, second] = other.network.Lanes.slice(-2).map((lane) => lane.ID);
  const cases = [
    { name: "a road lane", config: road, want: `lane ${road.network.Lanes.at(-1).ID} also uses its node ${berth.Node}. Delete this lane first.` },
    { name: "a road lane and a lane of a different station", config: other, want: `lanes ${first}, ${second} also use its node ${berth.Node}. Delete these lanes first.` },
  ];
  for (const tc of cases) {
    const before = JSON.parse(JSON.stringify(tc.config));
    const result = editor.removeBerth(tc.config, alpha.ID, berth.ID);
    assert.equal(result.config, tc.config, tc.name);
    assert.deepEqual(tc.config, before, tc.name);
    assert.equal(result.error, `Berth ${berth.ID} at Alpha stays, because ${tc.want}`, tc.name);
  }
});

test("add berth on a berth chain station names the station when a new lane has no clearance", () => {
  const { config } = chainScenario();
  const [alpha] = config.network.Stations;
  // The next arrival link goes from (64, 160) to (64, 190).
  const cases = [
    { name: "a lane across the arrival link", from: { X: 40, Y: 175 }, to: { X: 90, Y: 175 }, problem: "cross" },
    { name: "a lane 8 m from the arrival link", from: { X: 56, Y: 170 }, to: { X: 56, Y: 260 }, problem: "be nearer than 12 m to" },
  ];
  for (const tc of cases) {
    let blocked = editor.addJunction(editor.addJunction(config, tc.from.X, tc.from.Y), tc.to.X, tc.to.Y);
    const [from, to] = blocked.network.Nodes.slice(-2).map((node) => node.ID);
    blocked = editor.addLane(blocked, from, to, false);
    const road = blocked.network.Lanes.at(-1).ID;
    const link = editor.berthChain(blocked, alpha)[0].arrivalLink.ID;
    const result = editor.addBerth(blocked, alpha.ID);
    assert.equal(result.config, blocked, tc.name);
    assert.match(result.error, new RegExp(`^No space for another berth at Alpha\\. New lane (?!${link}\\b)\\S+ would ${tc.problem} lane ${road}\\.$`), tc.name);
  }
});

test("the station shape holds the station nodes along the station axes", () => {
  const pad = editor.STATION_PADDING;
  const cases = [
    { name: "a new station", entry: { X: 64, Y: 100 }, exit: { X: 136, Y: 100 }, points: [{ X: 100, Y: 130 }], want: { center: { X: 100, Y: 115 }, width: 72 + 2 * pad, height: 30 + 2 * pad, angle: 0, top: 100 - pad } },
    { name: "a station that points down", entry: { X: 100, Y: 64 }, exit: { X: 100, Y: 136 }, points: [{ X: 70, Y: 100 }], want: { center: { X: 85, Y: 100 }, width: 72 + 2 * pad, height: 30 + 2 * pad, angle: 90, top: 64 - pad } },
    { name: "a station that points left", entry: { X: 136, Y: 100 }, exit: { X: 64, Y: 100 }, points: [{ X: 100, Y: 60 }, { X: 80, Y: 80 }], want: { center: { X: 100, Y: 80 }, width: 72 + 2 * pad, height: 40 + 2 * pad, angle: 180, top: 60 - pad } },
    { name: "entry and exit at one point", entry: { X: 5, Y: 5 }, exit: { X: 5, Y: 5 }, points: [], want: { center: { X: 5, Y: 5 }, width: 2 * pad, height: 2 * pad, angle: 0, top: 5 - pad } },
  ];
  for (const tc of cases) {
    const shape = editor.stationShape(tc);
    assertNear(shape.center, tc.want.center, tc.name);
    for (const key of ["width", "height", "angle", "top"]) assert.ok(Math.abs(shape[key] - tc.want[key]) < 1e-9, `${tc.name} ${key}: ${shape[key]}`);
  }

  // A turned station has the same shape, turned with it.
  const { config } = chainScenario();
  const alpha = config.network.Stations[0];
  const shapeOf = (item) => {
    const ids = [...editor.stationNodeOwners(item)].filter(([, stationID]) => stationID === alpha.ID).map(([id]) => nodePosition(item, id));
    return editor.stationShape({ entry: nodePosition(item, alpha.Entry), exit: nodePosition(item, alpha.Exit), points: ids });
  };
  const before = shapeOf(config); const after = shapeOf(editor.rotateStation(config, alpha.ID, 30));
  assert.ok(Math.abs(after.width - before.width) < 1e-9 && Math.abs(after.height - before.height) < 1e-9);
  assert.ok(Math.abs(after.angle - before.angle - 30) < 1e-9);
  assertNear(after.center, { X: 100 + (before.center.Y - 100) * -Math.sin(Math.PI / 6), Y: 100 + (before.center.Y - 100) * Math.cos(Math.PI / 6) }, "turned center");
});

test("fleet counts never duplicate occupied berths", () => {
  let config = editor.addStation(editor.emptyConfig(), 200, 140, { name: "Depot", parkingOnly: true });
  const stationID = config.network.Stations[0].ID;
  config = addedBerth(config, stationID);
  config = editor.setFleetCount(config, stationID, 20);

  assert.equal(config.fleet.length, 2);
  assert.equal(new Set(config.fleet.map((pod) => pod.BerthID)).size, 2);
  assert.equal(new Set(config.fleet.map((pod) => pod.ID)).size, 2);
});

test("fleet rows give each station its pod count and berth limit", () => {
  const twoStations = () => {
    let config = editor.addStation(editor.emptyConfig(), 100, 100, { name: "Alpha" });
    config = editor.addStation(config, 340, 100, { name: "Beta" });
    return addedBerth(config, config.network.Stations[1].ID);
  };
  const cases = [
    { name: "no stations", config: () => editor.emptyConfig(), want: [] },
    { name: "no pods", config: twoStations, want: [{ name: "Alpha", count: 0, max: 1 }, { name: "Beta", count: 0, max: 2 }] },
    { name: "pods at both stations", config: () => { const config = twoStations(); const [alpha, beta] = config.network.Stations; return editor.setFleetCount(editor.setFleetCount(config, alpha.ID, 1), beta.ID, 2); }, want: [{ name: "Alpha", count: 1, max: 1 }, { name: "Beta", count: 2, max: 2 }] },
    { name: "pod at an unknown station", config: () => { const config = twoStations(); config.fleet.push({ ID: "09", StationID: "gone", BerthID: "gone-berth" }); return config; }, want: [{ name: "Alpha", count: 0, max: 1 }, { name: "Beta", count: 0, max: 2 }] },
  ];
  for (const tc of cases) {
    const config = tc.config();
    const want = tc.want.map((row, index) => ({ id: config.network.Stations[index].ID, ...row }));
    assert.deepEqual(editor.fleetRows(config), want, tc.name);
  }
});

test("the selection card gives the fields of the selected item", () => {
  let config = connectedScenario();
  config = addedBerth(config, config.network.Stations[1].ID);
  config = editor.addStation(config, 600, 100, { name: "Gamma" });
  config = editor.setStationBearing(config, config.network.Stations[2].ID, 359.6);
  config = editor.addStation(config, 900, 100, { name: "Delta" });
  config.network.Stations[3].Exit = "gone";
  config = editor.addJunction(config, 64.25, 160);
  const [alpha, beta, gamma, delta] = config.network.Stations;
  const lane = (from, to) => config.network.Lanes.find((item) => item.From === from && item.To === to);
  const [straight, curved] = [lane(alpha.Exit, beta.Entry), lane(beta.Exit, alpha.Entry)];
  const junction = config.network.Nodes.at(-1);
  delete alpha.ParkingOnly;
  beta.ParkingOnly = true;
  straight.SpeedLimit = 12.5;
  curved.SpeedLimit = 11.25;
  curved.Control = { X: 220, Y: 40 };
  const cases = [
    { name: "no selection", selection: null, want: null },
    {
      name: "station with one berth and no ParkingOnly", selection: { type: "station", id: alpha.ID },
      want: { type: "station", id: alpha.ID, name: "Alpha", bearing: 90, parkingOnly: false, canRemove: false, berths: [{ id: alpha.Berths[0].ID, selected: false }] },
    },
    {
      name: "parking station with a marked berth", selection: { type: "station", id: beta.ID, berth: beta.Berths[1].ID },
      want: { type: "station", id: beta.ID, name: "Beta", bearing: 90, parkingOnly: true, canRemove: true, berths: [{ id: beta.Berths[0].ID, selected: false }, { id: beta.Berths[1].ID, selected: true }] },
    },
    {
      name: "turned station rounds the bearing", selection: { type: "station", id: gamma.ID },
      want: { type: "station", id: gamma.ID, name: "Gamma", bearing: 0, parkingOnly: false, canRemove: false, berths: [{ id: gamma.Berths[0].ID, selected: false }] },
    },
    {
      name: "station with no exit node", selection: { type: "station", id: delta.ID },
      want: { type: "station", id: delta.ID, name: "Delta", bearing: 0, parkingOnly: false, canRemove: false, berths: [{ id: delta.Berths[0].ID, selected: false }] },
    },
    {
      name: "straight lane", selection: { type: "lane", id: straight.ID },
      want: { type: "lane", id: straight.ID, from: alpha.Exit, to: beta.Entry, speed: 45, length: editor.laneLength(config, straight), curved: false },
    },
    {
      name: "curved lane rounds the speed", selection: { type: "lane", id: curved.ID },
      want: { type: "lane", id: curved.ID, from: beta.Exit, to: alpha.Entry, speed: 41, length: editor.laneLength(config, curved), curved: true },
    },
    { name: "junction", selection: { type: "node", id: junction.ID }, want: { type: "node", id: junction.ID, x: 64.25, y: 160 } },
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
  let config = connectedScenario();
  const stationID = config.network.Stations[0].ID;
  for (let i = 0; i < 2; i += 1) config = addedBerth(config, stationID);
  const selection = { type: "station", id: stationID };
  for (const count of [3, 2]) {
    const berthIDs = editor.selectionCard(config, selection).berths.map((berth) => berth.id);
    assert.equal(berthIDs.length, count);
    for (const removed of berthIDs) {
      const after = editor.selectionCard(removedBerth(config, stationID, removed), selection);
      const focused = editor.berthFocusID(berthIDs, removed);
      assert.equal(focused !== "", after.canRemove, `${count} berths, remove ${removed}`);
      if (focused) assert.ok(after.berths.some((berth) => berth.id === focused), `${count} berths, remove ${removed}`);
    }
    config = removedBerth(config, stationID, berthIDs[0]);
  }
});

test("undo and redo keep the selection and the Selection panel focus, or focus the map when the item is gone", () => {
  // step gives the drafts before and after an undo or a redo of one change
  // from first to second, as the editor history restores them.
  const step = (first, second, redo) => {
    const history = editor.createHistory(first); history.replace(second);
    if (redo) history.undo();
    const before = history.value; assert.equal(redo ? history.redo() : history.undo(), true);
    return { before, after: history.value };
  };
  const one = connectedScenario(); const [alpha, beta] = one.network.Stations; const lane = one.network.Lanes[0];
  const two = addedBerth(one, alpha.ID); const three = addedBerth(two, alpha.ID);
  const [b1, b2, b3] = three.network.Stations[0].Berths.map((berth) => berth.ID);
  const withoutB2 = removedBerth(three, alpha.ID, b2);
  const curved = structuredClone(one); curved.network.Lanes[0].Control = { X: 220, Y: 40 };
  const added = editor.addStation(one, 600, 300, { name: "Gamma" }); const gamma = added.network.Stations.at(-1);
  const station = { type: "station", id: alpha.ID }; const button = (action, id = "") => ({ action, id });
  const cases = [
    { name: "undo of Add physical berth keeps its button", drafts: step(two, three), selection: station, control: button("add-berth"), want: { selection: station, focus: button("add-berth") } },
    { name: "redo of a curve keeps the curve button", drafts: step(one, curved, true), selection: { type: "lane", id: lane.ID }, control: button("toggle-curve"), want: { selection: { type: "lane", id: lane.ID }, focus: button("toggle-curve") } },
    { name: "undo of a berth remove keeps Remove of the same row", drafts: step(three, withoutB2), selection: station, control: button("remove-berth", b3), want: { selection: station, focus: button("remove-berth", b3) } },
    { name: "undo of a new berth moves Remove to the previous row", drafts: step(two, three), selection: station, control: button("remove-berth", b3), want: { selection: station, focus: button("remove-berth", b2) } },
    { name: "redo of a berth remove moves Remove to the next row", drafts: step(three, withoutB2, true), selection: station, control: button("remove-berth", b2), want: { selection: station, focus: button("remove-berth", b3) } },
    { name: "one berth left gives Add physical berth", drafts: step(one, two), selection: station, control: button("remove-berth", b1), want: { selection: station, focus: button("add-berth") } },
    { name: "a station selection with a gone berth stays", drafts: step(two, three), selection: { ...station, berth: b3 }, control: null, want: { selection: { ...station, berth: b3 }, focus: null } },
    { name: "undo of a new station clears the selection and focuses the map", drafts: step(one, added), selection: { type: "station", id: gamma.ID }, control: button("delete-station"), want: { selection: null, focus: "map" } },
    { name: "redo of a station delete clears the selection and focuses the map", drafts: step(one, editor.deleteStation(one, beta.ID), true), selection: { type: "station", id: beta.ID }, control: button("add-berth"), want: { selection: null, focus: "map" } },
    { name: "focus outside the panel stays with the item kept", drafts: step(two, three), selection: station, control: null, want: { selection: station, focus: null } },
    { name: "focus outside the panel stays with the item removed", drafts: step(one, added), selection: { type: "station", id: gamma.ID }, control: null, want: { selection: null, focus: null } },
    { name: "no selection", drafts: step(two, three), selection: null, control: null, want: { selection: null, focus: null } },
  ];
  for (const tc of cases) {
    const after = structuredClone(tc.drafts.after);
    assert.deepEqual(editor.undoFocus({ ...tc.drafts, selection: tc.selection, control: tc.control }), tc.want, tc.name);
    // The choice does not change the draft that the history restores.
    assert.deepEqual(tc.drafts.after, after, tc.name);
  }
});

test("validation reports short lanes and unreachable passenger pairs", () => {
  let config = editor.addStation(editor.emptyConfig(), 100, 100, { name: "Alpha" });
  config = editor.addStation(config, 340, 100, { name: "Beta" });
  config = editor.addJunction(config, 500, 100);
  const junction = config.network.Nodes.at(-1);
  config = editor.addJunction(config, 510, 100);
  config = editor.addLane(config, junction.ID, config.network.Nodes.at(-1).ID, false);
  const errors = editor.validateConfig(config);

  assert.ok(errors.some((error) => error.includes("shorter than 24 m")));
  assert.ok(errors.some((error) => error.includes("cannot reach")));
});

// The server accepts a junction with no lanes and a separate section, so the
// editor gives warnings for them and no errors.
test("a junction with no lanes gives one warning and no error", () => {
  const config = editor.addJunction(connectedScenario(), 600, 300);
  const junction = config.network.Nodes.at(-1);
  assert.deepEqual(editor.validateConfig(config), []);
  assert.deepEqual(editor.configWarnings(config), [`Junction ${junction.ID} is disconnected.`]);

  // The section check starts at a node with lanes, also when the first node
  // has no lanes.
  config.network.Nodes = [junction, ...config.network.Nodes.slice(0, -1)];
  assert.deepEqual(editor.validateConfig(config), []);
  assert.deepEqual(editor.configWarnings(config), [`Junction ${junction.ID} is disconnected.`]);
});

test("a separate section with no passenger station gives a warning and no error", () => {
  let config = editor.addJunction(connectedScenario(), 600, 300);
  const first = config.network.Nodes.at(-1).ID;
  config = editor.addJunction(config, 700, 300);
  config = editor.addLane(config, first, config.network.Nodes.at(-1).ID, true);
  assert.deepEqual(editor.validateConfig(config), []);
  assert.deepEqual(editor.configWarnings(config), ["The network has disconnected sections."]);

  // A parking station in a separate section is also only a warning.
  const parking = editor.addStation(connectedScenario(), 600, 300, { name: "Depot", parkingOnly: true });
  assert.deepEqual(editor.validateConfig(parking), []);
  assert.deepEqual(editor.configWarnings(parking), ["The network has disconnected sections."]);
});

test("the section check follows lanes in both directions", () => {
  // A one-way lane from a junction into a station connects the junction.
  let source = editor.addJunction(connectedScenario(), 100, 300);
  source = editor.addLane(source, source.network.Nodes.at(-1).ID, source.network.Stations[0].Entry, false);
  assert.deepEqual(editor.validateConfig(source), []);
  assert.deepEqual(editor.configWarnings(source), []);

  // A one-way lane from a station to a junction also connects the junction.
  let sink = editor.addJunction(connectedScenario(), 340, 300);
  sink = editor.addLane(sink, sink.network.Stations[1].Exit, sink.network.Nodes.at(-1).ID, false);
  assert.deepEqual(editor.validateConfig(sink), []);
  assert.deepEqual(editor.configWarnings(sink), []);
});

test("a passenger station in a separate section is an error", () => {
  const config = editor.addStation(connectedScenario(), 600, 300, { name: "Gamma" });
  assert.deepEqual(editor.validateConfig(config), ["Gamma cannot reach 2 passenger stations, and 2 passenger stations cannot reach Gamma."]);
  assert.deepEqual(editor.configWarnings(config), ["The network has disconnected sections."]);
});

// cutOffScenario gives Alpha, Beta, and Delta in a ring of one-way lanes,
// and Gamma with no lanes to the other stations. lanes adds lanes between
// Gamma and the ring. Each item of lanes is a pair of station names, from
// the exit of the first station to the entry of the second station.
function cutOffScenario(lanes) {
  let config = connectedScenario();
  const [alpha, beta] = config.network.Stations;
  config.network.Lanes = config.network.Lanes.filter((lane) => !(lane.From === beta.Exit && lane.To === alpha.Entry));
  config = editor.addStation(config, 340, 340, { name: "Delta" });
  config = editor.addStation(config, 600, 340, { name: "Gamma" });
  const station = (name) => config.network.Stations.find((item) => item.Name === name);
  for (const [from, to] of [["Beta", "Delta"], ["Delta", "Alpha"], ...lanes]) config = editor.addLane(config, station(from).Exit, station(to).Entry, false);
  return config;
}

// splitScenario gives two groups of passenger stations with no lanes between
// them. Alpha, Beta, and Delta are in a ring of one-way lanes. Gamma and
// Epsilon have lanes to each other.
function splitScenario() {
  let config = cutOffScenario([]);
  config = editor.addStation(config, 840, 340, { name: "Epsilon" });
  const [gamma, epsilon] = config.network.Stations.slice(-2);
  config = editor.addLane(config, gamma.Exit, epsilon.Entry, false);
  return editor.addLane(config, epsilon.Exit, gamma.Entry, false);
}

// isolateStation removes the lanes to the entry and from the exit of the
// station with the name.
function isolateStation(config, name) {
  const station = config.network.Stations.find((item) => item.Name === name);
  config.network.Lanes = config.network.Lanes.filter((lane) => lane.To !== station.Entry && lane.From !== station.Exit);
  return config;
}

// A station that is cut off from the other passenger stations gives one
// error, not one for each station pair. The error names the station, and a
// click on it selects the station.
test("a cut-off station gives one error that selects it", () => {
  const cases = [
    { name: "a station with no lanes", config: cutOffScenario([]),
      want: { Gamma: "Gamma cannot reach 3 passenger stations, and 3 passenger stations cannot reach Gamma." }, warnings: ["The network has disconnected sections."] },
    { name: "a station that pods can only leave", config: cutOffScenario([["Gamma", "Alpha"]]),
      want: { Gamma: "3 passenger stations cannot reach Gamma." }, warnings: [] },
    { name: "a station that pods can only enter", config: cutOffScenario([["Delta", "Gamma"]]),
      want: { Gamma: "Gamma cannot reach 3 passenger stations." }, warnings: [] },
    { name: "a network in two groups", config: splitScenario(),
      want: {
        Gamma: "Gamma cannot reach 3 passenger stations, and 3 passenger stations cannot reach Gamma.",
        Epsilon: "Epsilon cannot reach 3 passenger stations, and 3 passenger stations cannot reach Epsilon.",
      }, warnings: ["The network has disconnected sections."] },
    // Beta, Delta, and Gamma are in a chain of one-way lanes, so each group
    // has one station. Alpha is the first station, but it has no lanes, so
    // its group is not the main group.
    { name: "a first station with no lanes", config: isolateStation(cutOffScenario([["Delta", "Gamma"]]), "Alpha"),
      want: {
        Alpha: "Alpha cannot reach 3 passenger stations, and 3 passenger stations cannot reach Alpha.",
        Delta: "Delta cannot reach 2 passenger stations, and 2 passenger stations cannot reach Delta.",
        Gamma: "Gamma cannot reach 3 passenger stations, and Alpha cannot reach Gamma.",
      }, warnings: ["The network has disconnected sections."] },
  ];
  for (const item of cases) {
    const station = (name) => item.config.network.Stations.find((value) => value.Name === name);
    const want = Object.entries(item.want).map(([name, text]) => ({ text, target: { type: "station", id: station(name).ID } }));
    assert.deepEqual(editor.checkResults(item.config), { errors: want, warnings: item.warnings.map((text) => ({ text, target: null })) }, item.name);
    for (const result of want) assert.deepEqual(editor.checkSelection(item.config, result.target), result.target, item.name);
  }
});

// A station whose berths all have berth route errors is not in a group. Its
// row and column of reach are all true, so it would join the two groups.
test("a station with only broken berths does not join two groups", () => {
  const config = splitScenario();
  const alpha = config.network.Stations[0];
  const berth = alpha.Berths[0];
  config.network.Lanes = config.network.Lanes.filter((lane) => !(lane.From === alpha.Entry && lane.To === berth.Node));
  const station = (name) => config.network.Stations.find((item) => item.Name === name);
  const cutOff = (name) => ({ text: `${name} cannot reach 2 passenger stations, and 2 passenger stations cannot reach ${name}.`, target: { type: "station", id: station(name).ID } });
  assert.deepEqual(editor.checkResults(config), {
    errors: [{ text: `Berth ${berth.ID} needs an entry lane.`, target: { type: "berth", id: berth.ID } }, cutOff("Gamma"), cutOff("Epsilon")],
    warnings: [{ text: "The network has disconnected sections.", target: null }],
  });
});

// cutOffStations works on the reach table of stationReach. The main group
// is the largest group of stations that can all reach each other.
test("the cut-off stations are the stations outside the main group", () => {
  const table = (rows) => rows.map((row) => [...row].map((cell) => cell === "1"));
  const cases = [
    { name: "one group", reach: table(["111", "111", "111"]), checked: [true, true, true], want: [] },
    { name: "a larger group after a smaller group", reach: table(["1000", "0111", "0111", "0111"]), checked: [true, true, true, true],
      want: [{ index: 0, out: [1, 2, 3], in: [1, 2, 3] }] },
    { name: "two groups of the same size", reach: table(["1100", "1100", "0011", "0011"]), checked: [true, true, true, true],
      want: [{ index: 2, out: [0, 1], in: [0, 1] }, { index: 3, out: [0, 1], in: [0, 1] }] },
    { name: "a chain of one-way routes", reach: table(["111", "011", "001"]), checked: [true, true, true],
      want: [{ index: 1, out: [0], in: [2] }, { index: 2, out: [0, 1], in: [] }] },
    // All groups have one station. Station 0 has no routes, so it has the
    // most missing routes and is not the main group.
    { name: "a station with no routes first", reach: table(["1000", "0111", "0011", "0001"]), checked: [true, true, true, true],
      want: [{ index: 0, out: [1, 2, 3], in: [1, 2, 3] }, { index: 2, out: [0, 1], in: [0, 3] }, { index: 3, out: [0, 1, 2], in: [0] }] },
    // Station 0 has two berths that cannot reach each other, so it cannot
    // reach itself. It is still in its own group.
    { name: "a station whose berths cannot reach each other", reach: table(["01", "01"]), checked: [true, true],
      want: [{ index: 1, out: [0], in: [] }] },
    // A station with no berths to check has a row and a column of true. It
    // does not join the two groups.
    { name: "a station with no berths first", reach: table(["1111", "1110", "1110", "1001"]), checked: [false, true, true, true],
      want: [{ index: 3, out: [1, 2], in: [1, 2] }] },
    { name: "no stations to check", reach: table(["11", "11"]), checked: [false, false], want: [] },
  ];
  for (const item of cases) assert.deepEqual(editor.cutOffStations(item.reach, item.checked), item.want, item.name);
});

test("import accepts a project with only warnings", () => {
  const config = editor.addJunction(connectedScenario(), 600, 300);
  assert.deepEqual(editor.parseDocument(editor.serializeDocument(config, null)).scenario, config);
  delete config.demandProfiles;
  assert.deepEqual(editor.parseDocument(JSON.stringify(config)).scenario, editor.normalizeConfig(config));
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

test("each draft change schedules the checks, also an undo and a redo", (t) => {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  const named = (name) => ({ scenario: { ...connectedScenario(), name }, background: null });
  const seen = [];
  let history = null;
  const checks = editor.createCheckTimer({ delay: editor.CHECK_DELAY, clock: globalThis, run: () => seen.push(history.value.scenario.name) });
  history = editor.createHistory(named("A"), () => checks.schedule());
  // want is the draft name that the checks see after the change, or null
  // when the change does not change the draft and the checks do not run.
  const cases = [
    { name: "replace", change: () => history.replace(named("B")), want: "B" },
    { name: "replace with the same draft", change: () => history.replace(named("B")), want: null },
    { name: "undo", change: () => history.undo(), want: "A" },
    { name: "undo with no past", change: () => history.undo(), want: null },
    { name: "redo", change: () => history.redo(), want: "B" },
    { name: "redo with no future", change: () => history.redo(), want: null },
    { name: "replace without a record", change: () => history.replace(named("C"), false), want: "C" },
    { name: "commit of a coalesced change", change: () => history.commitFrom(named("B"), named("D")), want: "D" },
    { name: "reset", change: () => history.reset(named("E")), want: "E" },
  ];
  // settle ends the task of the change and waits for the checks.
  const settle = () => { t.mock.timers.tick(0); t.mock.timers.tick(editor.CHECK_DELAY); };
  for (const item of cases) {
    const before = seen.length;
    item.change();
    settle();
    assert.deepEqual(seen.slice(before), item.want === null ? [] : [item.want], item.name);
  }

  // Undo and redo in fast series give one run, for the last draft.
  history.replace(named("F"));
  history.undo(); history.redo(); history.undo();
  settle();
  assert.deepEqual(seen.slice(-1), ["E"]);
  assert.equal(seen.length, 7);
});

test("each check result names the object that a click selects", () => {
  const { config: chain, arrival } = chainScenario();
  const [alpha, beta] = chain.network.Stations;
  const berth = alpha.Berths[0].ID;
  const lane = chain.network.Lanes.find((item) => item.From === alpha.Exit && item.To === beta.Entry);
  // change makes an error or a warning. want is the result that it gives,
  // and select is the editor selection for its target.
  const cases = [
    { name: "a short lane", change: (config) => { config.network.Nodes.find((node) => node.ID === beta.Entry).Position = { ...config.network.Nodes.find((node) => node.ID === alpha.Exit).Position, X: 150 }; },
      want: { text: `Lane ${lane.ID} is shorter than 24 m.`, target: { type: "lane", id: lane.ID } }, select: { type: "lane", id: lane.ID } },
    { name: "a lane with no speed", change: (config) => { config.network.Lanes.find((item) => item.ID === lane.ID).SpeedLimit = 0; },
      want: { text: `Lane ${lane.ID} needs a positive speed limit.`, target: { type: "lane", id: lane.ID } }, select: { type: "lane", id: lane.ID } },
    { name: "a berth with no entry route", change: (config) => { config.network.Lanes = config.network.Lanes.filter((item) => item.To !== arrival); },
      want: { text: `Berth ${berth} needs an entry lane.`, target: { type: "berth", id: berth } }, select: { type: "station", id: alpha.ID, berth } },
    { name: "a station with no through lane", change: (config) => { config.network.Lanes = config.network.Lanes.filter((item) => !(item.From === beta.Entry && item.To === beta.Exit)); },
      want: { text: `Station ${beta.ID} needs a through lane.`, target: { type: "station", id: beta.ID } }, select: { type: "station", id: beta.ID } },
    // Alpha and Beta are two groups of the same size with the same number of
    // missing routes. Alpha is the first station in the project, so its
    // group is the main group, and the error selects Beta.
    { name: "a station that another station cannot reach", change: (config) => { config.network.Lanes = config.network.Lanes.filter((item) => item.ID !== lane.ID); },
      want: { text: "Alpha cannot reach Beta.", target: { type: "station", id: beta.ID } }, select: { type: "station", id: beta.ID } },
    { name: "a station that cannot reach another station", change: (config) => { config.network.Lanes = config.network.Lanes.filter((item) => !(item.From === beta.Exit && item.To === alpha.Entry)); },
      want: { text: "Beta cannot reach Alpha.", target: { type: "station", id: beta.ID } }, select: { type: "station", id: beta.ID } },
    { name: "a duplicate junction ID", change: (config) => { config.network.Nodes.push({ ID: arrival, Position: { X: 600, Y: 600 } }); },
      want: { text: `ID ${arrival} is used more than once.`, target: { type: "node", id: arrival } }, select: { type: "station", id: alpha.ID } },
    { name: "a pod in a missing station", change: (config) => { config.fleet[0].StationID = "gone"; },
      want: { text: `Pod ${chain.fleet[0].ID} has an invalid station or berth.`, target: { type: "station", id: "gone" } }, select: null },
    { name: "a demand setting", change: (config) => { config.demand.perMinute = 0; },
      want: { text: "Passenger demand must be 1 to 120 trips per minute.", target: null }, select: null },
  ];
  for (const item of cases) {
    const config = structuredClone(chain);
    item.change(config);
    const { errors, warnings } = editor.checkResults(config);
    assert.deepEqual(errors.map((result) => result.text), editor.validateConfig(config), item.name);
    assert.deepEqual(warnings, [], item.name);
    assert.deepEqual(errors.find((result) => result.text === item.want.text), item.want, item.name);
    assert.deepEqual(editor.checkSelection(config, item.want.target), item.select, item.name);
  }

  const config = editor.addJunction(chain, 600, 300);
  const junction = config.network.Nodes.at(-1).ID;
  assert.deepEqual(editor.checkResults(config), { errors: [], warnings: [{ text: `Junction ${junction} is disconnected.`, target: { type: "node", id: junction } }] });
  assert.deepEqual(editor.checkSelection(config, { type: "node", id: junction }), { type: "node", id: junction });
});

test("each lane, station, and berth error targets the object that it names", () => {
  const base = connectedScenario();
  const [alpha] = base.network.Stations;
  const berthNode = alpha.Berths[0].Node;
  const road = base.network.Lanes.find((lane) => !lane.StationID).ID;
  const roadLane = (config) => config.network.Lanes.find((lane) => lane.ID === road);
  // Each change gives one or more errors that start with "Lane", "Station",
  // or "Berth" and an ID. The target of each such error is that object.
  const cases = [
    { name: "a station with the same entry and exit", change: (config) => { config.network.Stations[0].Exit = config.network.Stations[0].Entry; } },
    { name: "a station with no name", change: (config) => { config.network.Stations[0].Name = ""; } },
    { name: "a station with an invalid parking setting", change: (config) => { config.network.Stations[0].ParkingOnly = "yes"; } },
    { name: "a station with an invalid berth", change: (config) => { config.network.Stations[0].Berths.push(null); } },
    { name: "a berth on the station entry", change: (config) => { config.network.Stations[0].Berths[0].Node = config.network.Stations[0].Entry; } },
    { name: "a berth with no lane out", change: (config) => { config.network.Lanes = config.network.Lanes.filter((lane) => lane.From !== berthNode); } },
    { name: "a berth with no lane in", change: (config) => { config.network.Lanes = config.network.Lanes.filter((lane) => lane.To !== berthNode); } },
    { name: "a berth with a second pod", change: (config) => { config.fleet.push({ ...config.fleet[0], ID: "pod-extra" }); } },
    { name: "a lane to a missing node", change: (config) => { roadLane(config).To = "missing"; } },
    { name: "a lane with an invalid control point", change: (config) => { roadLane(config).Control = { X: "a", Y: 1 }; } },
    { name: "a lane with an invalid station role", change: (config) => { Object.assign(roadLane(config), { StationID: alpha.ID, StationRole: "bogus" }); } },
    { name: "a lane of a missing station", change: (config) => { Object.assign(roadLane(config), { StationID: "gone", StationRole: "departure" }); } },
    { name: "a lane with no speed", change: (config) => { roadLane(config).SpeedLimit = 0; } },
  ];
  const named = /^(Lane|Station|Berth) (?!node )(\S+) /;
  for (const item of cases) {
    const config = structuredClone(base);
    item.change(config);
    const results = editor.checkResults(config).errors.filter((result) => named.test(result.text));
    assert.ok(results.length > 0, item.name);
    for (const result of results) {
      const [, type, id] = result.text.match(named);
      assert.deepEqual(result.target, { type: type.toLowerCase(), id }, `${item.name}: ${result.text}`);
    }
  }
});

test("a check selection finds only the objects of the draft", () => {
  const { config, arrival } = chainScenario();
  const [alpha, beta] = config.network.Stations;
  const cases = [
    { name: "no target", target: null, want: null },
    { name: "a station", target: { type: "station", id: beta.ID }, want: { type: "station", id: beta.ID } },
    { name: "a missing station", target: { type: "station", id: "station-9" }, want: null },
    { name: "a lane", target: { type: "lane", id: "lane-1" }, want: { type: "lane", id: "lane-1" } },
    { name: "a missing lane", target: { type: "lane", id: "lane-99" }, want: null },
    { name: "a berth", target: { type: "berth", id: beta.Berths[0].ID }, want: { type: "station", id: beta.ID, berth: beta.Berths[0].ID } },
    { name: "a missing berth", target: { type: "berth", id: "berth-9" }, want: null },
    { name: "a station entry node", target: { type: "node", id: alpha.Entry }, want: { type: "station", id: alpha.ID } },
    { name: "a berth chain node", target: { type: "node", id: arrival }, want: { type: "station", id: alpha.ID } },
    { name: "a missing node", target: { type: "node", id: "node-99" }, want: null },
    { name: "a type that the map does not show", target: { type: "pod", id: config.fleet[0].ID }, want: null },
  ];
  const select = editor.checkSelector(config);
  for (const item of cases) {
    assert.deepEqual(editor.checkSelection(config, item.target), item.want, item.name);
    assert.deepEqual(select(item.target), item.want, item.name);
  }
});

// checkLinks gives the links of the Checks list for a draft, as
// showValidation makes them. The links are the errors, then the warnings,
// that select an item of the draft.
function checkLinks(config) {
  const { errors, warnings } = editor.checkResults(config); const select = editor.checkSelector(config);
  return [...errors, ...warnings.map((result) => ({ ...result, text: `Warning: ${result.text}` }))]
    .filter((result) => select(result.target)).map((result) => ({ text: result.text, ...result.target }));
}

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

// These cases use the check messages of real drafts. A fix or a delete of
// the focused item gives the nearest link that is left, or the heading. A
// station whose message text changes keeps the focus.
test("the focus stays near its check when a draft change runs the checks again", () => {
  const setSpeed = (scenario, id, speed) => { const next = structuredClone(scenario); next.network.Lanes.find((lane) => lane.ID === id).SpeedLimit = speed; return next; };
  const texts = (links) => links.map((link) => link.text);
  const focusAfter = (before, focused, config) => editor.checkFocusKey({ before, focused, after: checkLinks(config) });

  // A fix of the first speed limit gives the next check, and a fix of the
  // last speed limit gives the heading.
  let config = connectedScenario();
  const [first, second] = config.network.Lanes.filter((lane) => !lane.StationID).map((lane) => lane.ID);
  config = setSpeed(setSpeed(config, first, 0), second, 0);
  let before = checkLinks(config);
  assert.deepEqual(texts(before), [`Lane ${first} needs a positive speed limit.`, `Lane ${second} needs a positive speed limit.`]);
  config = setSpeed(config, first, 10);
  assert.equal(focusAfter(before, 0, config), before[1].text);
  assert.equal(focusAfter(checkLinks(config), 0, setSpeed(config, second, 10)), "");

  // Three stations in a ring of lanes with no speed limit. A delete of the
  // last lane adds cut-off messages at the end of the list, but the focus
  // goes to the previous lane.
  config = editor.addStation(editor.emptyConfig(), 100, 100, { name: "Alpha" });
  config = editor.addStation(config, 340, 100, { name: "Beta" });
  config = editor.addStation(config, 340, 340, { name: "Gamma" });
  const ring = config.network.Stations;
  for (const [from, to] of [[0, 1], [1, 2], [2, 0]]) config = editor.addLane(config, ring[from].Exit, ring[to].Entry, false);
  config.demand.destination = ring[1].ID; config = editor.setFleetCount(config, ring[0].ID, 1);
  const lanes = config.network.Lanes.filter((lane) => !lane.StationID).map((lane) => lane.ID);
  for (const id of lanes) config = setSpeed(config, id, 0);
  before = checkLinks(config);
  assert.deepEqual(texts(before), lanes.map((id) => `Lane ${id} needs a positive speed limit.`));
  const deleted = editor.deleteLane(config, lanes[2]);
  assert.ok(checkLinks(deleted).length > 2, "the delete adds no cut-off messages");
  assert.equal(focusAfter(before, 2, deleted), before[1].text);

  // Alpha and Beta have lanes to each other. S1, S2, and S3 have no lanes,
  // so each has a cut-off message with station counts. The focus is on S2
  // while S1 is selected. Delete and then undo change the counts of S2,
  // but the focus stays on S2.
  config = connectedScenario();
  for (const [index, name] of ["S1", "S2", "S3"].entries()) config = editor.addStation(config, 100 + index * 240, 400, { name });
  const s1 = config.network.Stations.find((item) => item.Name === "S1").ID;
  const cutOff = (name, count) => `${name} cannot reach ${count} passenger stations, and ${count} passenger stations cannot reach ${name}.`;
  before = checkLinks(config);
  assert.deepEqual(texts(before), ["S1", "S2", "S3"].map((name) => cutOff(name, 4)));
  const deletedS1 = editor.deleteStation(config, s1);
  assert.equal(focusAfter(before, 1, deletedS1), cutOff("S2", 3));
  assert.equal(focusAfter(checkLinks(deletedS1), 0, config), cutOff("S2", 4));
});

test("the Checks heading can take the focus and shows a focus ring", () => {
  const html = fs.readFileSync(path.join(__dirname, "editor.html"), "utf8");
  assert.match(html, /<section id="checksPanel">\s*<div class="section-title"><h2 id="checksHeading" tabindex="-1">Checks<\/h2>/);
  const ring = cssRules(fs.readFileSync(path.join(__dirname, "editor.css"), "utf8")).get("h2:focus-visible");
  assert.ok(ring && parseFloat(ring.outline) > 0, "h2:focus-visible has no outline");
});

test("a check selection moves the view only when the map does not show the item well", () => {
  let config = editor.addJunction(connectedScenario(), 400, 300);
  const junction = config.network.Nodes.at(-1).ID;
  const [alpha] = config.network.Stations;
  config = editor.addLane(config, alpha.Exit, junction, false);
  const lane = config.network.Lanes.at(-1);
  const exit = config.network.Nodes.find((node) => node.ID === alpha.Exit).Position;
  const points = [
    { name: "a junction", selection: { type: "node", id: junction }, want: { X: 400, Y: 300 } },
    { name: "a straight lane", selection: { type: "lane", id: lane.ID }, want: { X: (exit.X + 400) / 2, Y: (exit.Y + 300) / 2 } },
    { name: "a curved lane", selection: { type: "lane", id: lane.ID }, control: { X: 300, Y: 400 }, want: { X: exit.X / 4 + 150 + 100, Y: exit.Y / 4 + 200 + 75 } },
    { name: "a station", selection: { type: "station", id: alpha.ID, berth: alpha.Berths[0].ID }, want: { X: 100, Y: 100 } },
    { name: "a missing lane", selection: { type: "lane", id: "lane-99" }, want: null },
  ];
  for (const item of points) {
    const scenario = structuredClone(config);
    if (item.control) scenario.network.Lanes.at(-1).Control = item.control;
    assert.deepEqual(editor.selectionPoint(scenario, item.selection), item.want, item.name);
  }

  const size = { width: 900, height: 700 };
  const views = [
    { name: "an item on the map", view: { x: 0, y: 0, scale: 1 }, point: { X: 400, Y: 300 }, want: { x: 0, y: 0, scale: 1 } },
    { name: "an item near the edge", view: { x: 0, y: 0, scale: 1 }, point: { X: 880, Y: 300 }, want: { x: -430, y: 50, scale: 1 } },
    { name: "an item off the map", view: { x: 0, y: 0, scale: 2 }, point: { X: -100, Y: 2000 }, want: { x: 650, y: -3650, scale: 2 } },
    { name: "an item on a map zoomed out below the label scale", view: { x: 10, y: 20, scale: 0.05 }, point: { X: 4000, Y: 3000 }, want: { x: -1550, y: -1150, scale: editor.NODE_LABEL_SCALE } },
  ];
  for (const item of views) assert.deepEqual(editor.focusView({ view: item.view, point: item.point, size }), item.want, item.name);
});

test("a check on the generated london project selects the lane that it names", needsGo, () => {
  const config = generatedProject("london-central");
  const lane = config.network.Lanes.find((item) => !item.StationID);
  lane.SpeedLimit = 0;
  const { errors, warnings } = editor.checkResults(config);
  assert.deepEqual(errors, [{ text: `Lane ${lane.ID} needs a positive speed limit.`, target: { type: "lane", id: lane.ID } }]);
  assert.deepEqual(warnings, []);
  assert.deepEqual(editor.checkSelection(config, errors[0].target), { type: "lane", id: lane.ID });
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

// LONDON_GEO is the geo reference of the London preset.
const LONDON_GEO = editor.makeGeo(51.5074, -0.1278);

// metersNorth gives the latitude that is meters north of the latitude of
// geo, in the projection of geo.
function metersNorth(geo, meters) {
  return editor.unprojectPoint(geo, { X: 0, Y: -meters }).latitude;
}

test("the projection of three London stations gives the positions of the London preset", () => {
  // TestLondonPointsGolden in internal/scenarios writes the file from
  // londonPoint.
  const file = JSON.parse(fs.readFileSync(path.join(repoRoot, "internal", "scenarios", "testdata", "london_points.json"), "utf8"));
  assert.deepEqual(file.geo, LONDON_GEO);
  assert.equal(file.points.length, 3);
  for (const item of file.points) {
    const at = editor.projectPoint(file.geo, item.latitude, item.longitude);
    assertNear(at, { X: item.x, Y: item.y }, item.id);
    const back = editor.unprojectPoint(file.geo, at);
    assert.ok(Math.abs(back.latitude - item.latitude) < 1e-12 && Math.abs(back.longitude - item.longitude) < 1e-12, `${item.id}: the inverse gives the place again`);
  }
});

test("the editor checks the geo reference as the server does", () => {
  const config = connectedScenario();
  assert.deepEqual(editor.validateConfig({ ...config, geo: LONDON_GEO }), []);
  assert.deepEqual(editor.validateConfig({ ...config, geo: null }), [], "null is no reference");
  assert.deepEqual(editor.validateConfig({ ...config, geo: editor.makeGeo(0, 0) }), [], "0, 0 is a valid reference");
  assert.deepEqual(editor.validateConfig({ ...config, geo: editor.makeGeo(-80, 180) }), [], "the limits");
  const bad = [
    { geo: "London", want: "The geo reference must be an object." },
    { geo: editor.makeGeo(80.001, 0), want: "The geo latitude must be from -80 to 80 degrees." },
    { geo: editor.makeGeo(0, -180.001), want: "The geo longitude must be from -180 to 180 degrees." },
    { geo: { ...LONDON_GEO, latitude: "51" }, want: "The geo latitude must be from -80 to 80 degrees." },
    { geo: { ...LONDON_GEO, projection: "web-mercator" }, want: 'The geo projection must be "equirectangular".' },
    { geo: { ...LONDON_GEO, radius: 6378137 }, want: "The geo radius must be 6371000 meters." },
  ];
  for (const item of bad) assert.deepEqual(editor.validateConfig({ ...config, geo: item.geo }), [item.want], item.want);
  assert.equal(editor.normalizeConfig({ ...config, geo: LONDON_GEO }).geo.latitude, 51.5074, "normalizeConfig keeps the reference");
  assert.equal("geo" in editor.normalizeConfig(config), false, "normalizeConfig adds no reference");
});

test("the placement of a frame follows the geo reference, and alignment allows 0.5 m", () => {
  const frame = { south: 51.50, north: 51.52, west: -0.14, east: -0.10, source: "equirectangular" };
  const other = editor.makeGeo(51.52, -0.10);
  const place = editor.framePlacement(frame, LONDON_GEO);
  const corner = editor.projectPoint(LONDON_GEO, 51.52, -0.14);
  assert.deepEqual([place.x, place.y], [corner.X, corner.Y], "the north-west corner");
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

test("a frame must be in the accuracy budget, the latitude limit, the antimeridian rule and the coordinate square", () => {
  const geo = editor.makeGeo(51.5, 0);
  // box gives a frame from the reference latitude to meters north, or to
  // meters south for a negative value.
  const box = (meters) => {
    const edge = metersNorth(geo, meters);
    return { south: Math.min(51.5, edge), north: Math.max(51.5, edge), west: -0.01, east: 0.01, source: "equirectangular" };
  };
  const cases = [
    { name: "a north edge at 25.1 km", frame: box(25100), ok: true },
    { name: "a north edge at 25.3 km", frame: box(25300), ok: false },
    { name: "a south edge at 25.4 km", frame: box(-25400), ok: true },
    { name: "a south edge at 25.6 km", frame: box(-25600), ok: false },
  ];
  for (const item of cases) assert.equal(editor.placementError(item.frame, geo) === "", item.ok, `${item.name}: ${editor.placementError(item.frame, geo)}`);
  assert.match(editor.placementError(box(25300), geo), /The east-west scale differs by 0\.5\d%, and the limit is 0\.5%\./);
  // A box across the equator has its smallest scale error at the edges and
  // its largest at the equator.
  const equator = { south: -6, north: 6, west: 0, east: 0.1, source: "web-mercator" };
  assert.match(editor.scaleError(editor.makeGeo(6, 0), equator.south, equator.north), /differs by 0\.55%/, "the equator fails");
  assert.equal(editor.scaleError(editor.makeGeo(6, 0), 5.9, 6.1), "", "the same reference passes near its latitude");
  assert.equal(editor.scaleError(editor.makeGeo(0, 0), -1, 1), "");
  const frame = { south: 51.4, north: 51.6, west: -0.2, east: 0, source: "equirectangular" };
  const rejected = [
    { name: "a latitude past 80", frame: { ...frame, south: 79, north: 80.5 }, want: /latitudes must be from -80 to 80/ },
    { name: "a box across the antimeridian", frame: { ...frame, west: 179, east: -179 }, want: /cannot cross the antimeridian/ },
    { name: "a longitude past 180", frame: { ...frame, east: 181 }, want: /longitudes must be from -180 to 180/ },
    { name: "south north of north", frame: { ...frame, south: 51.7 }, want: /south edge of the frame must be south/ },
    { name: "an unknown source", frame: { ...frame, source: "utm" }, want: /source must be "equirectangular" or "web-mercator"/ },
    { name: "a bound that is not a number", frame: { ...frame, west: "0" }, want: /bounds must be numbers/ },
    { name: "another member", frame: { ...frame, zoom: 3 }, want: /unknown member/ },
    { name: "a corner past the coordinate square", frame: { ...frame, west: 1, east: 1.5 }, want: /more than 100000 m from the reference/ },
  ];
  for (const item of rejected) assert.match(editor.placementError(item.frame, geo), item.want, item.name);
  assert.equal(editor.placementError(frame, null), "The project has no geographic reference.");
  assert.equal(editor.placementError(frame, geo), "");
});

test("an anchor maps the first node exactly and checks the residual of the second", () => {
  const geo = editor.makeGeo(51.5, -0.1);
  const place = (latitude, longitude, at) => ({ ...at, latitude, longitude });
  const start = editor.unprojectPoint(geo, { X: 250, Y: -400 });
  const a = place(start.latitude, start.longitude, { X: 250, Y: -400 });
  // east is the place 1 km east of a on the map.
  const east = editor.unprojectPoint(geo, { X: a.X + 1000, Y: a.Y });
  const result = editor.anchorGeo({ a, b: place(east.latitude, east.longitude, { X: a.X + 1000, Y: a.Y }) });
  assertNear(editor.projectPoint(result.geo, a.latitude, a.longitude), a, "a maps to its node");
  assert.ok(Math.abs(result.geo.latitude - 51.5) < 1e-12 && Math.abs(result.geo.longitude + 0.1) < 1e-12, "the anchor gives the reference of the places");
  assert.ok(result.residual < 1e-6 && result.error === "", "b on its node");
  // The network pair goes 1 km south, and the map pair goes 1 km east.
  const turned = editor.anchorGeo({ a, b: place(east.latitude, east.longitude, { X: a.X, Y: a.Y + 1000 }) });
  assert.ok(Math.abs(turned.residual - 1414.2) < 0.2, `the residual is ${turned.residual}`);
  assert.match(turned.error, /^The second anchor is 1414\.\d m from its node, more than 2% of the 1000\.0 m between the nodes\.$/);
  const near = editor.anchorGeo({ a, b: place(east.latitude, east.longitude, { X: a.X + 1000, Y: a.Y + 19 }) });
  assert.equal(near.error, "", "19 m is in 2% of 1 km");
  const far = editor.anchorGeo({ a, b: place(east.latitude, east.longitude, { X: a.X + 1000, Y: a.Y + 21 }) });
  assert.match(far.error, /more than 2%/, "21 m is more than 2% of 1 km");
  const close = editor.unprojectPoint(geo, { X: a.X + 99, Y: a.Y });
  assert.equal(editor.anchorGeo({ a, b: place(close.latitude, close.longitude, { X: a.X + 99, Y: a.Y }) }).error, "The anchor nodes must be at least 100 m apart.");
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
  const geo = editor.makeGeo(51.5, -0.1);
  const place = editor.framePlacement(frame, geo);
  const size = editor.resampleSize(frame, geo, 10);
  assert.deepEqual([size.width, size.height, size.error], [Math.round(place.width / 10), Math.round(place.height / 10), ""]);
  assert.match(editor.resampleSize(frame, geo, 1).error, /The limit is 4096 pixels on each side/);
  assert.match(editor.resampleSize(frame, geo, 0).error, /must be a positive number/);
  assert.deepEqual([editor.resampleSize(frame, geo, 1e6).width, editor.resampleSize(frame, geo, 1e6).height], [1, 1], "each side is at least 1 pixel");
});

test("import accepts a server project file", () => {
  // The server leaves out empty optional fields, such as the demand profiles.
  const config = connectedScenario();
  delete config.demandProfiles;
  const parsed = editor.parseDocument(JSON.stringify(config));

  assert.deepEqual(parsed, { scenario: editor.normalizeConfig(config), background: null });
  assert.deepEqual(parsed.scenario.demandProfiles, []);
  assert.equal(parsed.scenario.sharedRidePartyLimit, 1);
  assert.deepEqual(editor.validateConfig(parsed.scenario), []);

  config.fleet = [];
  assert.throws(() => editor.parseDocument(JSON.stringify(config)), /The project has 1 error\. The fleet must contain/);

  // The server accepts an empty pod berth and uses the first berth.
  const shorthand = connectedScenario();
  delete shorthand.demandProfiles;
  shorthand.fleet[0].BerthID = "";
  assert.equal(editor.parseDocument(JSON.stringify(shorthand)).scenario.fleet[0].BerthID, shorthand.network.Stations[0].Berths[0].ID);

  // The checks run before the editor adds the missing fields, so the editor
  // does not replace a demand rate that the server rejects.
  const noDemand = connectedScenario();
  delete noDemand.demandProfiles;
  noDemand.demand.perMinute = 0;
  assert.throws(() => editor.parseDocument(JSON.stringify(noDemand)), /Passenger demand must be 1 to 120/);
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
    { name: "a version 2 project without banks", file: { ...scenario, version: 2 }, message: "Version 2 projects need a banked station." },
    { name: "a project with no version", file: { ...scenario, version: undefined }, message: "The version field must be 1, 2, 3, 4, or 5." },
  ];
  for (const item of cases) {
    assert.throws(() => editor.parseDocument(JSON.stringify(item.file)), { message: item.message }, item.name);
  }
});

test("portable OD profiles validate and round trip", () => {
  const config = connectedScenario();
  const [alpha, beta] = config.network.Stations;
  config.demandProfiles = [{
    id: "weekday", name: "Weekday", bands: [{ id: "am", name: "AM peak", startMinute: 420, durationMinutes: 180 }],
    flows: [{ from: alpha.ID, to: beta.ID, weights: [3] }],
  }];
  config.demand = { enabled: true, perMinute: 12, pattern: "profile", destination: "", profile: "weekday", band: "am", seed: 9 };

  assert.deepEqual(editor.validateConfig(config), []);
  assert.deepEqual(editor.parseDocument(editor.serializeDocument(config)).scenario, config);
  config.demandProfiles[0].flows[0].weights = [-1];
  assert.ok(editor.validateConfig(config).some((error) => error.includes("invalid weight")));
});

test("the demand pattern change selects the first profile, or leaves a check error with no profiles", () => {
  const missing = connectedScenario();
  delete missing.demandProfiles;
  const noProfiles = "The project has no demand profiles. Select another pattern.";
  const cases = [
    { name: "profiles present", config: profileScenario().config, profile: "weekday", band: "am", errors: [] },
    { name: "empty array", config: connectedScenario(), profile: "", band: "", errors: [noProfiles] },
    { name: "member missing", config: missing, profile: "", band: "", errors: [noProfiles] },
    { name: "null value", config: { ...connectedScenario(), demandProfiles: null }, profile: "", band: "", errors: [noProfiles] },
    { name: "non-array value", config: { ...connectedScenario(), demandProfiles: "weekday" }, profile: "", band: "", errors: ["Demand profiles must be an array.", noProfiles] },
  ];
  for (const item of cases) {
    Object.assign(item.config.demand, { pattern: "balanced", profile: "", band: "" });
    const next = editor.setDemandPattern(item.config, "profile");
    assert.equal(item.config.demand.pattern, "balanced", item.name);
    assert.deepEqual([next.demand.pattern, next.demand.profile, next.demand.band], ["profile", item.profile, item.band], item.name);
    assert.deepEqual(editor.validateConfig(next), item.errors, item.name);
  }
  const { config } = profileScenario();
  Object.assign(config.demand, { profile: "weekend", band: "day" });
  const next = editor.setDemandPattern(config, "destination");
  assert.deepEqual([next.demand.pattern, next.demand.profile, next.demand.band], ["destination", "weekend", "day"]);
});

test("portable projects preserve the shared ride party limit", () => {
  const config = connectedScenario();
  config.sharedRidePartyLimit = 4;
  assert.deepEqual(editor.validateConfig(config), []);
  assert.equal(editor.parseDocument(editor.serializeDocument(config)).scenario.sharedRidePartyLimit, 4);
  config.sharedRidePartyLimit = 9;
  assert.ok(editor.validateConfig(config).some((error) => error.includes("shared ride party limit")));
});

test("portable projects preserve the shared ride mode and stop limit", () => {
  const config = connectedScenario();
  Object.assign(config, { sharedRidePartyLimit: 4, sharedRideMode: "drop-offs", sharedRideMaxStops: 5 });
  assert.deepEqual(editor.validateConfig(config), []);
  const imported = editor.parseDocument(editor.serializeDocument(config)).scenario;
  assert.deepEqual([imported.sharedRideMode, imported.sharedRideMaxStops], ["drop-offs", 5]);
  const cases = [
    { name: "an unknown mode", edit: { sharedRideMode: "pickups" }, error: "The shared ride mode must be destination or drop-offs." },
    { name: "a mode that is not text", edit: { sharedRideMode: 1 }, error: "The shared ride mode must be destination or drop-offs." },
    { name: "a stop limit above 7", edit: { sharedRideMaxStops: 8 }, error: "The shared ride stop limit must be 1 to 7." },
    { name: "a stop limit as text", edit: { sharedRideMaxStops: "3" }, error: "The shared ride stop limit must be 1 to 7." },
  ];
  for (const item of cases) {
    assert.deepEqual(editor.validateConfig({ ...config, ...item.edit }), [item.error], item.name);
  }
});

test("portable projects preserve the platoon limit", () => {
  // The server accepts 0 for no platoons, and 2 to 4 pods. A platoon of one
  // pod is not a platoon, so 1 is an error.
  const error = "The platoon limit must be 2 to 4, or 0 for no platoons.";
  for (const limit of [0, 2, 3, 4]) {
    const config = connectedScenario();
    config.platoonLimit = limit;
    assert.deepEqual(editor.validateConfig(config), [], `limit ${limit}`);
    assert.equal(editor.parseDocument(editor.serializeDocument(config)).scenario.platoonLimit, limit, `limit ${limit}`);
  }
  for (const limit of [1, 5, -1, 2.5, "4", null]) {
    const config = connectedScenario();
    config.platoonLimit = limit;
    assert.deepEqual(editor.validateConfig(config), [error], `limit ${limit}`);
    assert.throws(() => editor.parseDocument(editor.serializeDocument(config)), { message: `The project has 1 error. ${error}` }, `limit ${limit}`);
  }
});

test("the platoon field offers the limits that the server accepts", () => {
  const html = fs.readFileSync(path.join(__dirname, "editor.html"), "utf8");
  const select = html.match(/<select id="platoonLimit">(.*?)<\/select>/);
  assert.ok(select, "the editor has no platoon field");
  const values = [...select[1].matchAll(/<option value="(\d+)">/g)].map((match) => Number(match[1]));
  assert.deepEqual(values, [0, 2, 3, 4]);
  for (const limit of values) {
    const config = connectedScenario();
    config.platoonLimit = limit;
    assert.deepEqual(editor.validateConfig(config), [], `limit ${limit}`);
  }
});

test("import gives the default settings to a browser export that leaves them out", () => {
  // An older or edited browser export can leave out a setting. The import
  // adds the default value, as for a server project file. A party limit from
  // 1 to 8 stays, and a zero limit loads as 1, as on the server. The checks
  // run first, so an invalid value gives the same error as before.
  const limitError = "The project has 1 error. The shared ride party limit must be 1 to 8.";
  const cases = [
    { name: "no party limit", edit: (config) => { delete config.sharedRidePartyLimit; }, want: (config) => ({ ...config, sharedRidePartyLimit: 1 }) },
    { name: "a valid party limit", edit: (config) => { config.sharedRidePartyLimit = 4; }, want: (config) => config },
    { name: "a zero party limit, which the server loads as one", edit: (config) => { config.sharedRidePartyLimit = 0; }, want: (config) => ({ ...config, sharedRidePartyLimit: 1 }) },
    { name: "no shared ride mode", edit: (config) => { delete config.sharedRideMode; delete config.sharedRideMaxStops; }, want: (config) => ({ ...config, sharedRideMode: "drop-offs", sharedRideMaxStops: 3 }) },
    { name: "an empty mode and a zero stop limit, which the server loads as the defaults", edit: (config) => { Object.assign(config, { sharedRideMode: "", sharedRideMaxStops: 0 }); }, want: (config) => ({ ...config, sharedRideMode: "drop-offs", sharedRideMaxStops: 3 }) },
    { name: "the destination mode, which is not the default", edit: (config) => { config.sharedRideMode = "destination"; }, want: (config) => config },
    { name: "no join policy", edit: (config) => { delete config.sharedRideJoin; }, want: (config) => ({ ...config, sharedRideJoin: "unassigned" }) },
    { name: "an empty join policy", edit: (config) => { config.sharedRideJoin = ""; }, want: (config) => ({ ...config, sharedRideJoin: "unassigned" }) },
    { name: "assigned-party joins", edit: (config) => { config.sharedRideJoin = "reassign-existing"; }, want: (config) => config },
    { name: "an unknown join policy", edit: (config) => { config.sharedRideJoin = "all"; }, error: "The project has 1 error. The shared ride join policy must be unassigned or reassign-existing." },
    { name: "a join policy that is not text", edit: (config) => { config.sharedRideJoin = 1; }, error: "The project has 1 error. The shared ride join policy must be unassigned or reassign-existing." },
    { name: "no platoon limit", edit: (config) => { delete config.platoonLimit; }, want: (config) => ({ ...config, platoonLimit: 0 }) },
    { name: "a platoon limit", edit: (config) => { config.platoonLimit = 3; }, want: (config) => config },
    { name: "no demand profiles", edit: (config) => { delete config.demandProfiles; }, want: (config) => ({ ...config, demandProfiles: [] }) },
    { name: "a valid band that is not the first", edit: (config) => { Object.assign(config.demand, { profile: "weekday", band: "pm" }); }, want: (config) => config },
    { name: "a party limit above 8", edit: (config) => { config.sharedRidePartyLimit = 9; }, error: limitError },
    { name: "a party limit as text", edit: (config) => { config.sharedRidePartyLimit = "4"; }, error: limitError },
  ];
  for (const item of cases) {
    // The demand uses the second profile. The import keeps a valid profile
    // and band.
    const { config } = profileScenario();
    Object.assign(config.demand, { pattern: "balanced", profile: "weekend", band: "day" });
    item.edit(config);
    const file = JSON.stringify({ format: "podsim", version: 1, scenario: config });
    if (item.error) assert.throws(() => editor.parseDocument(file), { message: item.error }, item.name);
    else assert.deepEqual(editor.parseDocument(file), { scenario: item.want(config), background: null }, item.name);
  }
});

test("berth routes can use chains but not station nodes", () => {
  const cases = [
    { name: "a berth chain" },
    { name: "an entry route through another station entry", side: "entry", via: (stations) => stations[1].Entry },
    { name: "an entry route through another station berth", side: "entry", via: (stations) => stations[1].Berths[0].Node },
    { name: "an entry route through its own exit", side: "entry", via: (stations) => stations[0].Exit },
    { name: "an exit route through another station exit", side: "exit", via: (stations) => stations[1].Exit },
    { name: "an entry route against the lane direction", side: "entry", reverse: true },
    { name: "an exit route against the lane direction", side: "exit", reverse: true },
  ];
  for (const item of cases) {
    let { config, arrival, departure } = chainScenario();
    const berth = config.network.Stations[0].Berths[0];
    const expected = [];
    if (item.side) {
      const [from, to] = item.side === "entry" ? [arrival, berth.Node] : [berth.Node, departure];
      const lane = config.network.Lanes.find((candidate) => candidate.From === from && candidate.To === to);
      if (item.reverse) {
        [lane.From, lane.To] = [to, from];
      } else {
        const via = item.via(config.network.Stations);
        config.network.Lanes = config.network.Lanes.filter((candidate) => candidate !== lane);
        config = editor.addLane(editor.addLane(config, from, via, false), via, to, false);
      }
      expected.push(`Berth ${berth.ID} needs an ${item.side} lane.`);
    }
    assert.deepEqual(editor.validateConfig(config), expected, item.name);
  }
});

// The server checks passenger routes from each berth to each berth of the
// other passenger stations. The route does not have to go through the exit
// of the origin or the entry of the destination.
test("passenger routes go from berth to berth as on the server", () => {
  const cases = [
    { name: "a lane from the berth that skips the exit", route: ([alpha, beta]) => [alpha.Berths[0].Node, beta.Entry], want: [] },
    { name: "a lane to the berth that skips the entry", route: ([alpha, beta]) => [alpha.Exit, beta.Berths[0].Node], want: [] },
    { name: "a lane from one of two berths", secondBerth: 0, route: ([alpha, beta]) => [alpha.Berths[0].Node, beta.Entry], want: ["Alpha cannot reach Beta."] },
    { name: "a lane to one of two berths", secondBerth: 1, route: ([alpha, beta]) => [alpha.Exit, beta.Berths[0].Node], want: ["Alpha cannot reach Beta."] },
  ];
  for (const item of cases) {
    let config = connectedScenario();
    if (item.secondBerth !== undefined) config = addedBerth(config, config.network.Stations[item.secondBerth].ID);
    const [alpha, beta] = config.network.Stations;
    config.network.Lanes = config.network.Lanes.filter((lane) => !(lane.From === alpha.Exit && lane.To === beta.Entry));
    const [from, to] = item.route(config.network.Stations);
    config = editor.addLane(config, from, to, false);
    assert.deepEqual(editor.validateConfig(config), item.want, item.name);
  }
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

for (const preset of ["scale100", "london-central"]) {
  test(`the generated ${preset} project passes the editor checks`, needsGo, () => {
    const config = generatedProject(preset);
    assert.deepEqual(editor.validateConfig(config), []);
    assert.deepEqual(editor.configWarnings(config), []);

    // A delete removes the berth chains and the demand flows of the station,
    // and keeps the road network whole.
    const station = config.network.Stations.find((item) => !item.ParkingOnly);
    const deleted = editor.deleteStation(config, station.ID);
    assert.deepEqual(editor.validateConfig(deleted), []);
    assert.deepEqual(editor.configWarnings(deleted), []);
  });

  test(`import accepts the generated ${preset} project file`, needsGo, () => {
    assert.deepEqual(editor.parseDocument(generatedFile(preset)), { scenario: generatedProject(preset), background: null });
  });

  test(`a drag on the generated ${preset} project redraws only the moved items`, needsGo, () => {
    const config = generatedProject(preset);
    const station = config.network.Stations.find((item) => !item.ParkingOnly);
    const owners = editor.stationNodeOwners(config);
    const junction = config.network.Nodes.find((node) => !owners.has(node.ID));
    assert.deepEqual(editor.dragTargets(config, { type: "station", id: station.ID }), movedItems(config, editor.moveStation(config, station.ID, 20, -10)));
    assert.deepEqual(editor.dragTargets(config, { type: "node", id: junction.ID }), movedItems(config, editor.moveNode(config, junction.ID, junction.Position.X + 20, junction.Position.Y - 10)));
    const berth = station.Berths[0].Node; const at = nodePosition(config, berth);
    assert.deepEqual(editor.dragTargets(config, { type: "node", id: berth }), movedItems(config, editor.moveNode(config, berth, at.X + 20, at.Y - 10)));
  });
}

test("the editor accepts a generated london project near the node limit", needsGo, () => {
  // 3 berths at each passenger station, 200 berths at each Parking facility,
  // and a 40 meter pitch.
  const config = generatedProject("london-central", "-station-berths", "3", "-parking-berths", "200", "-berth-pitch", "40");
  assert.equal(config.network.Nodes.length, 3822);
  assert.ok(config.network.Nodes.length <= editor.MAX_NODES && config.network.Lanes.length <= editor.MAX_LANES);
  assert.deepEqual(editor.validateConfig(config), []);
});

test("each station shape on the generated london project holds its station nodes", needsGo, () => {
  const config = generatedProject("london-central");
  const owners = editor.stationNodeOwners(config); const pad = editor.STATION_PADDING;
  for (const station of config.network.Stations) {
    const points = [...owners].filter(([, stationID]) => stationID === station.ID).map(([id]) => nodePosition(config, id));
    const shape = editor.stationShape({ entry: nodePosition(config, station.Entry), exit: nodePosition(config, station.Exit), points });
    const radians = shape.angle * Math.PI / 180; const cos = Math.cos(radians); const sin = Math.sin(radians);
    for (const at of points) {
      const x = at.X - shape.center.X; const y = at.Y - shape.center.Y;
      assert.ok(Math.abs(x * cos + y * sin) <= shape.width / 2 - pad + 1e-6, station.ID);
      assert.ok(Math.abs(-x * sin + y * cos) <= shape.height / 2 - pad + 1e-6, station.ID);
    }
  }
});

test("add berth on a generated london station extends its berth chain", needsGo, () => {
  const config = generatedProject("london-central");
  const station = config.network.Stations.find((item) => item.Name === "Tottenham Court Road");
  // At a bearing of 135 degrees, the chain turns with the station.
  for (const bearing of [null, 135]) {
    const start = bearing === null ? config : editor.setStationBearing(config, station.ID, bearing);
    const pitch = berthPitch(start, station);
    assert.ok(Math.abs(Math.hypot(pitch.X, pitch.Y) - 75) < 1e-9, `bearing ${bearing}`);
    assertChainRow({ before: start, after: addedBerth(start, station.ID), stationID: station.ID, pitch }, `bearing ${bearing}`);
  }
  // The row crosses a guideway at Blackfriars, so the editor does not add it.
  const result = editor.addBerth(config, "940GZZLUBKF");
  assert.equal(result.config, config);
  assert.equal(result.error, "No space for another berth at Blackfriars. New lane 940GZZLUBKF-03-departure-link would cross lane london-link-027-ab-2.");
});

// kingsCrossRow is the third berth row of King's Cross St. Pancras in the
// output of scenario -preset london-central -berths 940GZZLUKSX=3. With three
// berths, the generator does not move a node of the london preset.
const kingsCrossRow = {
  nodes: [
    { ID: "940GZZLUKSX-03-arrival", Position: { X: 45.99691707495151, Y: -2946.1585711063485 } },
    { ID: "940GZZLUKSX-03-node", Position: { X: -18.281843893702387, Y: -2869.554126794451 } },
    { ID: "940GZZLUKSX-03-departure", Position: { X: -82.56060486235629, Y: -2792.949682482553 } },
  ],
  lanes: [
    ["arrival-link", "940GZZLUKSX-02-arrival", "940GZZLUKSX-03-arrival", "berth-access"],
    ["departure-link", "940GZZLUKSX-03-departure", "940GZZLUKSX-02-departure", "departure"],
    ["in", "940GZZLUKSX-03-arrival", "940GZZLUKSX-03-node", "berth-access"],
    ["out", "940GZZLUKSX-03-node", "940GZZLUKSX-03-departure", "departure"],
  ].map(([part, from, to, role]) => ({ ID: `940GZZLUKSX-03-${part}`, From: from, To: to, SpeedLimit: 14, SeparationGroup: "940GZZLUKSX-station", StationID: "940GZZLUKSX", StationRole: role })),
  berth: { ID: "940GZZLUKSX-03", Node: "940GZZLUKSX-03-node", SeparationGroup: "940GZZLUKSX-station" },
};

test("add berth on a generated london station gives the row of the london generator", needsGo, () => {
  const config = generatedProject("london-central");
  const added = addedBerth(config, "940GZZLUKSX");
  const nodes = added.network.Nodes.slice(config.network.Nodes.length);
  assert.deepEqual(nodes.map((node) => node.ID), kingsCrossRow.nodes.map((node) => node.ID));
  nodes.forEach((node, index) => assertNear(node.Position, kingsCrossRow.nodes[index].Position, node.ID));
  assert.deepEqual(added.network.Lanes.slice(config.network.Lanes.length), kingsCrossRow.lanes);
  assert.deepEqual(added.network.Stations.find((item) => item.ID === "940GZZLUKSX").Berths.at(-1), kingsCrossRow.berth);
});

test("add berth on the generated scale100 project extends the chain or names the station", needsGo, () => {
  const config = generatedProject("scale100");
  const refused = [];
  for (const station of config.network.Stations) {
    const result = editor.addBerth(config, station.ID);
    if (result.error) {
      assert.equal(result.config, config, station.ID);
      assert.ok(result.error.startsWith(`No space for another berth at ${station.Name}. `), result.error);
      refused.push(station.Name);
    } else assertChainRow({ before: config, after: result.config, stationID: station.ID, pitch: berthPitch(config, station) }, station.ID);
  }
  // At these stations, a seventh row puts the arrival chain nearer than
  // CLEARANCE to the departure chain of another station. The scenario
  // generator finds the same problem at Station 01 and Station 19 with
  // seven berths.
  assert.deepEqual(refused, ["01", "02", "03", "04", "07", "08", "09", "10", "11", "12", "13", "17", "18", "19"].map((number) => `Station ${number}`));
});

// movedItems gives the nodes, lanes, and stations whose drawn position
// differs between two versions of a scenario. A station shape holds all of
// its station nodes, so a station moves when one of its nodes moves.
function movedItems(before, after) {
  const same = (a, b) => JSON.stringify(a) === JSON.stringify(b);
  const positions = new Map(after.network.Nodes.map((node) => [node.ID, node.Position]));
  const controls = new Map(after.network.Lanes.map((lane) => [lane.ID, lane.Control]));
  const moved = new Set(before.network.Nodes.filter((node) => !same(node.Position, positions.get(node.ID))).map((node) => node.ID));
  const owners = editor.stationNodeOwners(before);
  return {
    nodeIDs: [...moved],
    laneIDs: before.network.Lanes.filter((lane) => moved.has(lane.From) || moved.has(lane.To) || !same(lane.Control, controls.get(lane.ID))).map((lane) => lane.ID),
    stationIDs: before.network.Stations.filter((station) => [...moved].some((id) => owners.get(id) === station.ID)).map((station) => station.ID),
  };
}

test("a drag redraws only the items that it moves", () => {
  let { config, arrival, departure } = chainScenario();
  const [alpha, beta] = config.network.Stations;
  config = editor.addJunction(config, 220, 260);
  const junction = config.network.Nodes.at(-1).ID;
  config = editor.addLane(config, alpha.Exit, junction, true);
  const through = config.network.Lanes.find((lane) => lane.From === alpha.Entry && lane.To === alpha.Exit);
  through.Control = { X: 100, Y: 60 };
  const curved = structuredClone(config);
  curved.network.Lanes.find((lane) => lane.ID === through.ID).Control = { X: 90, Y: 40 };
  const cases = [
    { name: "a station drag", drag: { type: "station", id: alpha.ID }, after: editor.moveStation(config, alpha.ID, 20, -10) },
    { name: "a junction drag", drag: { type: "node", id: junction }, after: editor.moveNode(config, junction, 300, 280) },
    { name: "a curve drag", drag: { type: "control", id: through.ID }, after: curved },
    { name: "an entry drag", drag: { type: "node", id: alpha.Entry }, after: editor.moveNode(config, alpha.Entry, 40, 80) },
    { name: "a berth drag", drag: { type: "node", id: alpha.Berths[0].Node }, after: editor.moveNode(config, alpha.Berths[0].Node, 100, 200) },
    { name: "a berth chain drag", drag: { type: "node", id: arrival }, after: editor.moveNode(config, arrival, 50, 170) },
  ];
  for (const item of cases) assert.deepEqual(editor.dragTargets(config, item.drag), movedItems(config, item.after), item.name);

  const targets = editor.dragTargets(config, { type: "station", id: alpha.ID });
  for (const id of [arrival, departure]) assert.ok(targets.nodeIDs.includes(id), id);
  assert.deepEqual(targets.stationIDs, [alpha.ID]);
  assert.ok(!targets.nodeIDs.includes(beta.Entry) && !targets.nodeIDs.includes(junction));
  assert.deepEqual(editor.dragTargets(config, { type: "control", id: through.ID }), { nodeIDs: [], laneIDs: [through.ID], stationIDs: [] });
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
  const nodes = editor.addJunction(editor.addJunction(editor.emptyConfig(), 20, -30), 400, 60);
  const cases = [
    { name: "an empty map", config: editor.emptyConfig(), background: null, want: null },
    { name: "a background only", config: editor.emptyConfig(), background, want: { minX: -50, minY: 10, maxX: 150, maxY: 110 } },
    { name: "nodes only", config: nodes, background: null, want: { minX: 20, minY: -30, maxX: 400, maxY: 60 } },
    { name: "nodes and a background", config: nodes, background, want: { minX: -50, minY: -30, maxX: 400, maxY: 110 } },
  ];
  for (const item of cases) assert.deepEqual(editor.networkBounds(item.config, item.background), item.want, item.name);
});

test("a station delete on the generated london project removes the flows of the station", needsGo, () => {
  const config = generatedProject("london-central");
  const station = config.network.Stations.find((item) => !item.ParkingOnly);
  const flowCount = (project) => project.demandProfiles.reduce((count, profile) => count + profile.flows.length, 0);
  const naming = config.demandProfiles.flatMap((profile) => profile.flows).filter((flow) => flow.from === station.ID || flow.to === station.ID);
  assert.ok(naming.length > 0);
  assert.equal(editor.stationFlowCount(config, station.ID), naming.length);

  const deleted = editor.deleteStation(config, station.ID);
  assert.equal(flowCount(config) - flowCount(deleted), naming.length);
  assert.deepEqual(editor.validateConfig(deleted).filter((error) => error.includes("invalid flow")), []);
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
  for (const node of config.network.Nodes) {
    const x = view.x + node.Position.X * view.scale; const y = view.y + node.Position.Y * view.scale;
    assert.ok(x >= 0 && x <= size.width && y >= 0 && y <= size.height, node.ID);
  }
  assert.equal(editor.zoomScale({ scale: view.scale, factor: 0.8, fitScale: view.scale }), view.scale);
  const owners = editor.stationNodeOwners(config);
  for (const node of config.network.Nodes.filter((item) => !owners.has(item.ID))) {
    assert.equal(editor.nodeLabelSize({ scale: view.scale, id: node.ID, selection: null, linkFrom: "" }), 0, node.ID);
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
  let config = editor.addJunction(editor.addJunction(editor.addJunction(editor.emptyConfig(), 0, 0), 100, 0), 100, 100);
  const [a, b, c] = config.network.Nodes.map((node) => node.ID);
  config = editor.addLane(config, a, b, true);
  config = editor.addLane(config, b, c, false);
  config.network.Lanes.push({ ...config.network.Lanes.at(-1), ID: "parallel" });
  const [forward, reverse, oneWay] = config.network.Lanes.map((lane) => lane.ID);
  const withStationLane = structuredClone(config);
  Object.assign(withStationLane.network.Lanes[1], { StationID: "station-1", StationRole: "through" });
  const cases = [
    { name: "a new paired guideway", config, want: [forward, reverse] },
    { name: "a pair with a station lane", config: withStationLane, want: [forward, reverse] },
    { name: "a pair after a delete of one lane", config: editor.deleteLane(config, reverse), want: [] },
    { name: "one-way lanes between the same nodes", config: { network: { Lanes: config.network.Lanes.filter((lane) => lane.ID === oneWay || lane.ID === "parallel") } }, want: [] },
    { name: "a lane from a node to the same node", config: { network: { Lanes: [{ ID: "loop", From: a, To: a }] } }, want: [] },
    { name: "the connected scenario", config: connectedScenario(), want: [] },
  ];
  for (const item of cases) assert.deepEqual([...editor.pairedLaneIDs(item.config)].sort(), item.want.sort(), item.name);
});

// curvePoint gives the point of a quadratic curve at t. A curve with no
// control point is a straight line.
function curvePoint(curve, t) {
  const control = curve.control || { X: (curve.from.X + curve.to.X) / 2, Y: (curve.from.Y + curve.to.Y) / 2 };
  const u = 1 - t;
  return { X: u * u * curve.from.X + 2 * u * t * control.X + t * t * curve.to.X, Y: u * u * curve.from.Y + 2 * u * t * control.Y + t * t * curve.to.Y };
}

function assertNear(actual, want, name) {
  assert.ok(Math.abs(actual.X - want.X) < 1e-9 && Math.abs(actual.Y - want.Y) < 1e-9, `${name}: got ${JSON.stringify(actual)}, want ${JSON.stringify(want)}`);
}

test("a lane of a pair moves to the right of its direction of travel", () => {
  const r = Math.SQRT1_2;
  const cases = [
    { name: "no offset", lane: { from: { X: 0, Y: 0 }, to: { X: 100, Y: 0 } }, want: { from: { X: 0, Y: 0 }, to: { X: 100, Y: 0 }, middle: { X: 50, Y: 0 } } },
    // The map Y axis points down, so the right of a lane to the east is +Y.
    { name: "a lane to the east", lane: { from: { X: 0, Y: 0 }, to: { X: 100, Y: 0 }, offset: 4 }, want: { from: { X: 0, Y: 4 }, to: { X: 100, Y: 4 }, middle: { X: 50, Y: 4 } } },
    { name: "the reverse lane to the west", lane: { from: { X: 100, Y: 0 }, to: { X: 0, Y: 0 }, offset: 4 }, want: { from: { X: 100, Y: -4 }, to: { X: 0, Y: -4 }, middle: { X: 50, Y: -4 } } },
    { name: "a lane to the south", lane: { from: { X: 0, Y: 0 }, to: { X: 0, Y: 30 }, offset: 2 }, want: { from: { X: -2, Y: 0 }, to: { X: -2, Y: 30 }, middle: { X: -2, Y: 15 } } },
    { name: "a curve with no offset", lane: { from: { X: 0, Y: 0 }, to: { X: 100, Y: 0 }, control: { X: 50, Y: 50 } }, want: { from: { X: 0, Y: 0 }, to: { X: 100, Y: 0 }, control: { X: 50, Y: 50 }, middle: { X: 50, Y: 25 } } },
    // Each end moves along the normal of the curve at that end, and the
    // middle point moves along the normal of the line from end to end.
    { name: "a curve", lane: { from: { X: 0, Y: 0 }, to: { X: 100, Y: 0 }, control: { X: 50, Y: 50 }, offset: 2 }, want: { from: { X: -2 * r, Y: 2 * r }, to: { X: 100 + 2 * r, Y: 2 * r }, control: { X: 50, Y: 54 - 2 * r }, middle: { X: 50, Y: 27 } } },
    { name: "the reverse curve", lane: { from: { X: 100, Y: 0 }, to: { X: 0, Y: 0 }, control: { X: 50, Y: 50 }, offset: 2 }, want: { from: { X: 100 - 2 * r, Y: -2 * r }, to: { X: 2 * r, Y: -2 * r }, control: { X: 50, Y: 46 + 2 * r }, middle: { X: 50, Y: 23 } } },
    { name: "a control point on the start node", lane: { from: { X: 0, Y: 0 }, to: { X: 100, Y: 0 }, control: { X: 0, Y: 0 }, offset: 4 }, want: { from: { X: 0, Y: 4 }, to: { X: 100, Y: 4 }, control: { X: 0, Y: 4 }, middle: { X: 25, Y: 4 } } },
    { name: "two nodes at the same point", lane: { from: { X: 5, Y: 5 }, to: { X: 5, Y: 5 }, offset: 4 }, want: { from: { X: 5, Y: 5 }, to: { X: 5, Y: 5 }, middle: { X: 5, Y: 5 } } },
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
  let config = editor.addJunction(editor.addJunction(editor.emptyConfig(), 10, 20), 130, 70);
  const [a, b] = config.network.Nodes.map((node) => node.ID);
  config = editor.addLane(config, a, b, true, { X: 90, Y: -10 });
  const at = (id) => config.network.Nodes.find((node) => node.ID === id).Position;
  for (const scale of [0.055, 0.5, 1, 5]) {
    const offset = editor.laneOffset({ paired: true, scale });
    const [forward, reverse] = config.network.Lanes.map((lane) => editor.laneCurve({ from: at(lane.From), to: at(lane.To), control: lane.Control, offset }));
    const gap = Math.hypot(forward.middle.X - reverse.middle.X, forward.middle.Y - reverse.middle.Y) * scale;
    assert.ok(Math.abs(gap - 2 * editor.LANE_PAIR_OFFSET) < 1e-9, `scale ${scale}: the middle points are ${gap} screen pixels apart`);
    assert.equal(editor.laneOffset({ paired: false, scale }), 0, `scale ${scale}: a lane that is not in a pair`);
  }
});

test("the lane path has a vertex at the middle point for the chevron", () => {
  const straight = editor.laneCurve({ from: { X: 0, Y: 0 }, to: { X: 100, Y: 0 }, offset: 4 });
  assert.equal(editor.lanePathData(straight), "M 0 4 L 50 4 L 100 4");
  const curve = editor.laneCurve({ from: { X: 0, Y: 0 }, to: { X: 100, Y: 0 }, control: { X: 40, Y: 60 }, offset: 3 });
  const path = editor.lanePathData(curve);
  assert.match(path, /^M \S+ \S+ Q \S+ \S+ \S+ \S+ Q \S+ \S+ \S+ \S+$/);
  // The two halves of the path draw the same curve as the lane curve.
  const numbers = path.match(/-?[\d.e+-]+/g).map(Number);
  const point = (index) => ({ X: numbers[index], Y: numbers[index + 1] });
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
    { name: "a curve with nodes close together", length: editor.curveLength({ from: { X: 0, Y: 0 }, to: { X: 1, Y: 0 }, control: { X: 0.5, Y: 60 } }), scale: 1, want: true },
  ];
  for (const item of cases) assert.equal(editor.showsChevron({ length: item.length, scale: item.scale }), item.want, item.name);
});

test("the length of a lane follows its curve", () => {
  // The curve from (0, 0) to (1, 0) with its control point at (0.5, 60) goes
  // 30 m up and back down. Its length is 60.03 m.
  const config = { network: { Nodes: [{ ID: "a", Position: { X: 0, Y: 0 } }, { ID: "b", Position: { X: 1, Y: 0 } }] } };
  const cases = [
    { name: "a straight lane", got: editor.curveLength({ from: { X: 0, Y: 0 }, to: { X: 30, Y: 40 } }), want: 50 },
    { name: "a curve on the line between its nodes", got: editor.curveLength({ from: { X: 0, Y: 0 }, to: { X: 100, Y: 0 }, control: { X: 50, Y: 0 } }), want: 100 },
    { name: "a curve with nodes close together", got: editor.curveLength({ from: { X: 0, Y: 0 }, to: { X: 1, Y: 0 }, control: { X: 0.5, Y: 60 } }), want: 60.03 },
    { name: "the same curve as a lane", got: editor.laneLength(config, { From: "a", To: "b", Control: { X: 0.5, Y: 60 } }), want: 60.03 },
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
      return reply(200, { epoch: live.epoch, serverStart: live.serverStart, projectRevision: live.revision, generation: live.generation, simulation: { Paused: live.paused } });
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
    assert.equal(error.message, "The command has 10.07 MiB of JSON. The server accepts at most 10.06 MiB.");
    return true;
  });
  // Random base64 text compresses to about three quarters of its size.
  const random = require("node:crypto").randomBytes(4 * 1024 * 1024).toString("base64");
  await assert.rejects(editor.postCommand(connection, { action: "pause", origin: random }), (error) => {
    assert.equal(error.status, 413);
    assert.match(error.message, /^The compressed command has 4\.\d+ MiB\. The server accepts at most 4 MiB\.$/);
    return true;
  });
  assert.equal(requests, 0);
});

test("an apply that is too large fails before the pause, and a 413 reply gives the server limits", async () => {
  const server = fakeSession({ paused: false });
  const connection = { fetch: server.fetch, clientID: "editor-test", sequence: 0, epoch: "" };
  await assert.rejects(editor.applyToServer({ connection, revision: 3, project: paddedScenario(editor.SERVER_PROJECT_BYTES + 1) }), (error) => {
    assert.equal(error.status, 413);
    assert.equal(editor.applyFailureText(error), "Apply failed. The scenario has 10.01 MiB of JSON. The server accepts at most 10 MiB. The simulation was not paused.");
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
    assert.equal(editor.applyFailureText(error), "Apply failed. The command is too large for the server. The server accepts at most 10.06 MiB of JSON and 4 MiB after compression. The editor resumed the simulation.");
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
    return reply({ epoch: live.epoch, serverStart: live.serverStart, projectRevision: live.revision, generation: 5, simulation: { Paused: true } });
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
    { name: "an older server without a server start ID", before: { ...state, serverStart: "" }, after: { ...state, serverStart: "" }, project, want: true },
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
    const live = await editor.readLive(reader.connection);
    assert.deepEqual({ name: live.project.name, revision: live.revision, epoch: live.epoch, serverStart: live.serverStart }, { name: "Restored", revision: 3, epoch: "epoch-1", serverStart: "start-2" }, name);

    const loader = snapshotServer(restoreOn(number));
    const loaded = await editor.loadLive({ connection: loader.connection, changed: false, confirm: () => false });
    const page = editor.liveDraft(loaded, { loaded: null, background: null });
    assert.deepEqual([page.value.scenario.name, page.draftBase.serverStart, loader.connection.epoch], ["Restored", "start-2", "epoch-1"], name);

    const failure = Object.assign(new Error("The server session changed."), { errorCode: "session_changed" });
    assert.deepEqual(await editor.readConflict(snapshotServer(restoreOn(number)).connection, failure), { code: "session_changed", revision: 3, serverStart: "start-2" }, name);
  }
  const changing = snapshotServer((url, count, live) => { if (url === "/api/state") live.serverStart = `start-${count}`; });
  const failure = Object.assign(new Error("The live scenario changed."), { errorCode: "stale_project" });
  assert.deepEqual(await editor.readConflict(changing.connection, failure), { code: "stale_project", revision: null, serverStart: "" }, "a server that keeps changing");
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
    const conflict = await editor.readConflict(session.connection, error);
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
    const conflict = await editor.readConflict(connection, failure);
    assert.deepEqual(conflict, { code: "session_changed", revision: 4, serverStart: "start-2" }, item.name);
    if (item.change) item.change(server.live);

    const questions = []; const reads = session.reads;
    const base = await editor.applyOverBase({ connection, revision: conflict.revision, serverStart: conflict.serverStart, confirm: (text) => { questions.push(text); return item.answer; } });
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
    if (item.wantConflict) assert.deepEqual(await editor.readConflict(connection, error), item.wantConflict, item.name);
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
      connection: session.connection, changed: editor.draftChanged(item.draft, baseline),
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
  assert.deepEqual((await editor.readConflict(connection, first)), { code: "session_changed", revision: 4, serverStart: "start-2" });
  const again = await apply();
  assert.equal(again.errorCode, "session_changed", "the page keeps the old epoch without an action");

  const live = await editor.loadLive({ connection, changed: false, confirm: () => false });
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
// background in factory, as the editor does. history is the undo history
// of the page, and gate is its startup gate. options.readLive is a promise
// that the live load waits for. options.decode, when given, decodes the
// stored image for checkImageBytes, and gives its size. writes counts the
// writes that the background keeper queues.
function startupPage(factory, options = {}) {
  const document = fakeStartupDocument(); const gate = editor.createStartupGate();
  const history = editor.createHistory({ scenario: editor.emptyConfig(), background: null });
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

  const live = await editor.readLive(server.connection);
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
  assert.deepEqual(await editor.readConflict(server.connection, failure), { code: "session_changed", revision: 3, serverStart: "start-3" });
  assert.equal(starts.at(-1), "start-3", "the conflict read");
  assert.equal(offerText(starts.at(-1)).includes(editor.DRAFT_RESTART_TEXT), true, "the offer after the conflict");

  // A connection without onServerStart reads as before.
  delete server.connection.onServerStart;
  assert.equal((await editor.readState(server.connection)).serverStart, "start-3");
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

test("station lane roles validate and round trip", () => {
  const config = connectedScenario();
  assert.deepEqual(editor.validateConfig(config), []);
  const parsed = editor.parseDocument(editor.serializeDocument(config));
  assert.deepEqual(parsed.scenario.network.Lanes, config.network.Lanes);

  config.network.Lanes[0].StationRole = "invalid";
  assert.ok(editor.validateConfig(config).some((error) => error.includes("invalid station role")));
});

test("portable import accepts the legacy market demand pattern", () => {
  const config = connectedScenario();
  const oldID = config.network.Stations[1].ID;
  config.network.Stations[1].ID = "market";
  for (const lane of config.network.Lanes) if (lane.StationID === oldID) lane.StationID = "market";
  config.demand.pattern = "market";
  config.demand.destination = "";
  const parsed = editor.parseDocument(editor.serializeDocument(config, null));

  assert.equal(parsed.scenario.demand.pattern, "destination");
  assert.equal(parsed.scenario.demand.destination, "market");
});

test("history coalesces a drag snapshot and supports redo", () => {
  const original = { scenario: connectedScenario(), background: { dataURL: "data:image/png;base64,AA==", x: 0, y: 0, width: 10, height: 10, opacity: 0.5 } };
  const moved = { scenario: editor.moveStation(original.scenario, original.scenario.network.Stations[0].ID, 25, 10), background: { ...original.background, opacity: 0.2 } };
  const history = editor.createHistory(original);

  assert.equal(history.commitFrom(original, moved), true);
  assert.equal(history.canUndo, true);
  assert.equal(history.undo(), true);
  assert.deepEqual(history.value, original);
  assert.equal(history.redo(), true);
  assert.deepEqual(history.value, moved);
  const background = history.background; background.x = 99;
  assert.deepEqual(history.background, moved.background, "background gives a copy");
  assert.equal(editor.createHistory({ scenario: original.scenario, background: null }).background, null);
});

test("history preserves the full draft through repeated undo", () => {
  const original = {
    scenario: connectedScenario(),
    background: { dataURL: "data:image/jpeg;base64,AA==", x: 2, y: 3, width: 40, height: 30, opacity: 0.5 },
  };
  const history = editor.createHistory(original);

  for (let i = 0; i < 60; i += 1) {
    const next = history.value;
    next.scenario.name = `Revision ${i}`;
    history.replace(next);
  }
  for (let i = 0; i < 60; i += 1) assert.equal(history.undo(), true);

  assert.deepEqual(history.value, original);
  assert.equal(history.undo(), false);
});

test("berth edits preserve pods in unchanged berths", () => {
  let config = connectedScenario();
  const station = config.network.Stations[0];
  const originalPod = { ...config.fleet[0] };

  config = addedBerth(config, station.ID);
  assert.deepEqual(config.fleet[0], originalPod);
  const newBerth = config.network.Stations[0].Berths[1];
  config = editor.setFleetCount(config, station.ID, 2);
  assert.deepEqual(config.fleet[0], originalPod);
  assert.equal(config.fleet[1].BerthID, newBerth.ID);

  config = removedBerth(config, station.ID, newBerth.ID);
  assert.deepEqual(config.fleet, [originalPod]);
});

test("a connected scenario passes all editor checks", () => {
  const config = connectedScenario();
  assert.deepEqual(editor.validateConfig(config), []);
  assert.deepEqual(editor.configWarnings(config), []);
});

test("validation reports malformed import values without throwing", () => {
  const config = connectedScenario();
  config.network.Nodes.unshift(null);
  config.network.Stations.push(null);
  config.network.Stations[0].Berths = { bad: true };
  config.network.Lanes.push(null);
  config.fleet.push(null);
  config.demand = "invalid";

  let errors;
  assert.doesNotThrow(() => { errors = editor.validateConfig(config); });
  assert.ok(errors.some((error) => error.includes("invalid value")));
  assert.ok(errors.some((error) => error.includes("at least one berth")));
  assert.ok(errors.some((error) => error.includes("Passenger demand")));
  assert.doesNotThrow(() => editor.configWarnings(config));
});

test("legacy import normalization defers malformed stations to validation", () => {
  const config = connectedScenario();
  config.network.Stations.unshift(null);
  config.fleet[0].BerthID = "";
  config.demand.pattern = "market";

  assert.throws(
    () => editor.parseDocument(editor.serializeDocument(config, null)),
    /The project has/,
  );
});

test("validation matches server guards for duplicate berth nodes and demand fields", () => {
  let config = connectedScenario();
  config = addedBerth(config, config.network.Stations[0].ID);
  config.network.Stations[0].Berths[1].Node = config.network.Stations[0].Berths[0].Node;
  config.demand.enabled = "yes";
  config.demand.destination = "x".repeat(65);

  const errors = editor.validateConfig(config);
  assert.ok(errors.some((error) => error.includes("Berth node") && error.includes("used more than once")));
  assert.ok(errors.some((error) => error.includes("enabled setting")));
  assert.ok(errors.some((error) => error.includes("destination is invalid")));
});

test("validation stays responsive at the supported station limit", () => {
  let config = editor.emptyConfig();
  for (let i = 0; i < 100; i += 1) config = editor.addStation(config, i * 120, (i % 2) * 120, { name: `Station ${i}` });
  for (let i = 0; i < 100; i += 1) {
    const current = config.network.Stations[i];
    const next = config.network.Stations[(i + 1) % 100];
    config = editor.addLane(config, current.Exit, next.Entry, false);
  }
  for (let i = 0; i < 20; i += 1) config = editor.setFleetCount(config, config.network.Stations[i].ID, 1);

  const start = performance.now();
  assert.deepEqual(editor.validateConfig(config), []);
  assert.deepEqual(editor.configWarnings(config), []);
  assert.ok(performance.now() - start < 500, "validation exceeded 500 ms");
});

 test("default berth shorthand loads and round trips", () => {
 const config = connectedScenario();
 config.fleet[0].BerthID = "";
 const normalized = editor.normalizeConfig(config);
 assert.equal(normalized.fleet[0].BerthID, config.network.Stations[0].Berths[0].ID);
 assert.deepEqual(editor.validateConfig(normalized), []);
 assert.equal(editor.parseDocument(editor.serializeDocument(config)).scenario.fleet[0].BerthID,normalized.fleet[0].BerthID);
 });
 test("empty fleets fail validation before apply", () => {
 const config=connectedScenario(); config.fleet=[];
 assert.ok(editor.validateConfig(config).some(error=>error.includes("fleet")));
 });

test("resource namespaces and parallel guideways match the server", () => {
 const config=connectedScenario();
 config.network.Lanes[0].ID=config.network.Stations[0].ID;
 // A parallel lane needs its own path, as on the server.
 const copy={...config.network.Lanes[0],ID:"parallel"};
 config.network.Lanes.push(copy);
 assert.deepEqual(editor.validateConfig(config),[`Lanes ${config.network.Stations[0].ID} and parallel have the same nodes and path.`]);
 copy.Control={X:0,Y:0};
 assert.deepEqual(editor.validateConfig(config),[]);
});
test("legacy market pattern uses the last passenger station when absent", () => {
 const config=connectedScenario();config.demand.pattern="market";config.demand.destination="";
 assert.equal(editor.normalizeConfig(config).demand.destination,config.network.Stations.at(-1).ID);
});

test("an empty scenario and the fallback draft have the settings of a normalized scenario", () => {
  const cases = [
    { name: "empty scenario", config: editor.emptyConfig },
    { name: "fallback draft", config: editor.fallbackConfig },
  ];
  for (const tc of cases) {
    const config = tc.config();
    const normalized = editor.normalizeConfig(config);
    assert.equal(config.sharedRidePartyLimit, 1, tc.name);
    assert.deepEqual([config.sharedRideMode, config.sharedRideMaxStops], ["drop-offs", 3], tc.name);
    assert.equal(config.platoonLimit, 0, tc.name);
    assert.deepEqual(config, normalized, tc.name);
    assert.deepEqual(editor.validateConfig(config), editor.validateConfig(normalized), tc.name);
    assert.deepEqual(editor.configWarnings(config), editor.configWarnings(normalized), tc.name);
  }
});

test("an exported fallback draft with a pod imports with its settings", () => {
  const fallback = editor.fallbackConfig();
  const config = editor.setFleetCount(fallback, fallback.network.Stations[0].ID, 1);
  assert.deepEqual(editor.validateConfig(config), []);
  const imported = editor.parseDocument(editor.serializeDocument(config)).scenario;
  assert.equal(imported.sharedRidePartyLimit, 1);
  assert.deepEqual(imported, config);
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

// placed gives a history background of image.
function placed(image, options = {}) {
  return { imageKey: image.key, x: 0, y: 0, width: 40, height: 20, opacity: 0.45, frameState: image.frame ? "attached" : "none", ...options };
}

// farNodes gives the two nodes of config that are the farthest apart.
function farNodes(config) {
  const nodes = config.network.Nodes; let best = [nodes[0], nodes[1]]; let far = -1;
  for (const a of nodes) for (const b of nodes) { const distance = Math.hypot(a.Position.X - b.Position.X, a.Position.Y - b.Position.Y); if (distance > far) { far = distance; best = [a, b]; } }
  return best;
}

// testModel gives a background model with the scenario config, or the
// connected scenario, and no background.
function testModel(options = {}) {
  return editor.createBackgroundModel({ initial: { scenario: options.config ?? connectedScenario(), background: null }, cap: options.cap });
}

// publishImage publishes image with the scenario of model, as an
// acquisition does. options can set baseline and value.
function publishImage(model, image, options = {}) {
  const ticket = model.start();
  return model.publish({ ticket, image, value: options.value ?? { scenario: model.history.value.scenario, background: placed(image) }, baseline: options.baseline });
}

// historyValues gives the values of all history entries of model, from
// the oldest undo step to the newest redo step. It walks the history with
// undo and redo, and leaves it as it was.
function historyValues(model) {
  const history = model.history; let back = 0;
  while (history.undo()) back += 1;
  const values = [history.value];
  let forward = 0;
  while (history.redo()) { values.push(history.value); forward += 1; }
  for (let step = forward; step > back; step -= 1) history.undo();
  return values;
}

// assertTable checks the image table invariants of model after step:
// each key of the history, the baseline and pinned resolves to a
// descriptor, the table holds no other key, each key keeps its first
// descriptor, seen maps each key to that descriptor, and no history entry
// or baseline holds binary data or a data URL.
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
  for (const value of [...historyValues(model), model.loaded]) {
    const text = JSON.stringify(value.background);
    assert.ok(!/bytes|dataURL|"image"/.test(text ?? ""), `${step}: the entry holds only the image key: ${text}`);
  }
}

test("the image table keeps each referenced image through imports, undo, redo, reset, loads, restores and removes", () => {
  const model = testModel(); const seen = new Map(); const scenario = () => model.history.value.scenario;
  const background = () => model.history.background;
  const steps = [
    ["import X", () => publishImage(model, testImage())],
    ["move X", () => model.history.replace({ scenario: scenario(), background: { ...background(), x: 25 } })],
    ["import framed Y", () => publishImage(model, testImage({ frame: LONDON_FRAME, license: OSM_LICENSE }))],
    ["detach Y", () => model.history.replace(editor.detachFrame(model.history.value))],
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
      model.setLoaded(loaded); model.history.replace(value);
    }],
    ["Restore draft", () => model.history.reset({ scenario: { ...scenario(), name: "Saved" }, background: background() })],
    ["project import with no background", () => publishImage(model, null, { baseline: true, value: { scenario: connectedScenario(), background: null } })],
    ["undo", () => model.history.undo()],
  ];
  for (const [name, run] of steps) { run(); assertTable(model, name, seen); }
  assert.ok(seen.size >= 4, "the sequence made four images");
  // Restore draft started a new history, so only the present image and
  // the baseline image stay.
  model.history.reset({ scenario: scenario(), background: null });
  assertTable(model, "a new history", seen);
  assert.equal(model.keys().length, 0, "the Load live baseline had no background after the project import");
});

test("the image limit drops the oldest undo steps, keeps the present and the baseline images, and the admission check takes the cap", () => {
  const eight = 8 * 1024 * 1024;
  const model = testModel();
  const base = testImage({ size: eight });
  assert.equal(publishImage(model, base, { baseline: true }).dropped, 0);
  const counts = [];
  for (let index = 0; index < 18; index += 1) counts.push(publishImage(model, testImage({ size: eight })).dropped);
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
  const small = testModel({ cap: 20 * 1024 * 1024 });
  const first = testImage({ size: eight });
  publishImage(small, first, { baseline: true });
  const second = testImage({ size: eight });
  assert.equal(publishImage(small, second).error, "");
  assert.equal(small.history.canUndo, true);
  // The next publish drops the undo steps, but the admission check
  // counts the present and the baseline images, and the pinned image.
  small.pin(second.key);
  const result = publishImage(small, testImage({ size: eight }));
  assert.match(result.error, /does not fit in the 20\.0 MiB image limit of this tab\. The images that the tab must keep use 16\.0 MiB, with 8\.0 MiB for writes to the browser store\./);
  assert.equal(small.history.background.imageKey, second.key, "the background does not change");
});

test("a keeper write and an export that start before a prune keep their bytes", async () => {
  const store = heldStore(); const model = testModel(); const commits = [];
  const keeper = editor.createDraftKeeper({
    store, key: DRAFT_KEY, delay: editor.DRAFT_SAVE_DELAY, clock: globalThis, deletes: true, holdUntilChange: true, text: editor.backgroundRecordText,
    snapshot: () => { const background = model.history.background; return background ? editor.backgroundRecordFor(background, model.image(background.imageKey)) : null; },
    onQueued: (record) => { if (record) model.pin(record.background.imageKey); },
    onSettled: (record, committed) => { if (record) model.unpin(record.background.imageKey); commits.push(committed); },
  });
  await keeper.load(); await keeper.arm();
  const bytes = editor.dataURLToBytes(dataURL(pngBytes(4, 2)));
  const image = testImage({ bytes });
  publishImage(model, image);
  const exported = editor.serializeDocument(model.history.value.scenario, editor.exportBackground(model.history.background, model.image(image.key)));
  const write = keeper.flush(); await tick();
  assert.equal(model.pinCount(image.key), 1, "the queued write pins the image");
  // Load live with no background drops each history and baseline
  // reference to the image.
  model.setLoaded({ scenario: connectedScenario(), background: null }); model.history.reset({ scenario: connectedScenario(), background: null });
  assert.ok(model.image(image.key), "the pinned image stays in the table");
  store.next(); await write;
  assert.deepEqual([model.pinCount(image.key), model.image(image.key), commits], [0, null, [true]], "the settled write releases the image");
  assert.deepEqual(new Uint8Array(store.records.get(DRAFT_KEY).background.image.bytes), new Uint8Array(bytes), "the store has the bytes");
  assert.equal(editor.parseDocument(exported).background.dataURL, dataURL(pngBytes(4, 2)), "the export has the bytes");
});

test("an abort, a newer acquisition or an edit drops the result of an acquisition", () => {
  const model = testModel();
  let ticket = model.start(); model.abort();
  assert.equal(ticket.signal.aborted, true);
  assert.deepEqual(model.publish({ ticket, image: testImage(), value: { scenario: connectedScenario(), background: null } }), { error: "A newer action stopped the import.", dropped: 0 }, "an abort event");
  const older = model.start(); ticket = model.start();
  assert.equal(older.signal.aborted, true, "a new acquisition aborts the old one");
  assert.match(model.publish({ ticket: older, image: testImage(), value: model.history.value }).error, /newer action/);
  // A node move after the anchor validation drops the result.
  const config = model.history.value.scenario; const [a, b] = farNodes(config);
  const at = (node) => ({ id: node.ID, ...editor.unprojectPoint(LONDON_GEO, node.Position) });
  ticket = model.start();
  const reference = editor.referenceFor({ config, frame: LONDON_FRAME, choice: { mode: "anchor", a: at(a), b: at(b) } });
  assert.equal(reference.error, undefined);
  const moved = model.history.value.scenario; moved.network.Nodes[0].Position = { X: a.Position.X + 5, Y: a.Position.Y };
  model.history.replace({ scenario: moved, background: null });
  const image = testImage({ frame: LONDON_FRAME });
  assert.equal(model.publish({ ticket, image, value: editor.framedValue(model.history.value, image, reference.geo) }).error, "The draft changed during the import.");
  assert.equal(model.image(image.key), null, "the dropped image is not in the table");
});

test("a scenario edit during the decode of a project import fails the import and keeps the draft", async () => {
  const model = testModel(); const slot = editor.createDecoderSlot();
  const baseline = model.loaded;
  let finish;
  const deps = { decode: () => new Promise((resolve) => { finish = () => resolve({ width: 4, height: 2, close() {} }); }) };
  const ticket = model.start();
  const bytes = editor.dataURLToBytes(dataURL(pngBytes(4, 2)));
  const decoded = slot.run((signal) => editor.checkImageBytes(bytes, deps, signal), ticket.signal);
  await tick();
  const edited = model.history.value.scenario; edited.name = "Edited"; model.history.replace({ scenario: edited, background: null });
  finish(); const facts = await decoded;
  const image = editor.freezeImage({ key: TEST_KEY, bytes, mime: facts.mime, pixelWidth: facts.width, pixelHeight: facts.height, frame: null, license: null });
  const result = model.publish({ ticket, image, value: { scenario: connectedScenario(), background: placed(image) }, baseline: true });
  assert.equal(result.error, "The draft changed during the import.");
  assert.deepEqual([model.history.value.scenario.name, model.loaded, model.keys()], ["Edited", baseline, []]);
});

test("a drag that moved blocks a publish until its pointer up, and a drag with no move does not", () => {
  const model = testModel();
  // An acquisition starts, then a drag begins and moves.
  let ticket = model.start();
  model.moveDrag();
  assert.match(model.publish({ ticket, image: testImage(), value: model.history.value }).error, /changed during the import/);
  // An acquisition and an Import JSON that start and end while the moved
  // drag is open fail with the reason.
  for (const baseline of [false, true]) {
    ticket = model.start();
    assert.match(model.publish({ ticket, image: testImage(), value: model.history.value, baseline }).error, /A drag or an opacity change was open/);
  }
  // The pointer up records the move.
  const moved = model.history.value.scenario; moved.network.Nodes[0].Position = { X: 1, Y: 2 };
  model.endDrag(); model.history.replace({ scenario: moved, background: null });
  assert.deepEqual(model.history.value.scenario.network.Nodes[0].Position, { X: 1, Y: 2 });
  // A drag with no move does not block a publish.
  ticket = model.start();
  assert.equal(model.publish({ ticket, image: testImage(), value: { scenario: moved, background: placed(testImage()) } }).error, "");
});

test("an opacity gesture starts again after a history change that it did not make", () => {
  const model = testModel();
  const w = testImage(); const x = testImage();
  publishImage(model, w); publishImage(model, x);
  model.pressOpacity();
  model.history.undo();
  const edited = model.history.value.scenario; edited.name = "Edited";
  model.history.replace({ scenario: edited, background: model.history.background });
  assert.equal(model.setOpacity(0.8), true);
  assert.equal(model.endOpacity(), true);
  assert.equal(model.history.keys().has(x.key), false, "no history entry holds X");
  assert.equal(model.image(x.key), null, "the table has no X");
  assert.deepEqual([model.history.background.imageKey, model.history.background.opacity], [w.key, 0.8]);
  model.history.undo();
  assert.deepEqual([model.history.background.opacity, model.history.value.scenario.name], [0.45, "Edited"], "one undo step changes the opacity of W");
  assert.equal(model.endOpacity(), false, "a second pointer up with no press changes nothing");

  // With no image before X, the slider move and the pointer up change
  // nothing.
  const lone = testModel(); publishImage(lone, testImage());
  lone.pressOpacity(); lone.history.undo();
  const renamed = lone.history.value.scenario; renamed.name = "Renamed";
  lone.history.replace({ scenario: renamed, background: null });
  const before = JSON.stringify(lone.history.value);
  assert.deepEqual([lone.setOpacity(0.8), lone.endOpacity(), JSON.stringify(lone.history.value)], [false, false, before]);
  assert.equal(lone.keys().length, 0);
});

test("pointercancel and a lost pointer capture end the opacity gesture as a pointer up does", () => {
  for (const end of ["pointercancel", "lostpointercapture"]) {
    const model = testModel(); publishImage(model, testImage());
    model.pressOpacity(); model.setOpacity(0.7);
    assert.equal(model.gestureChanged, true, `${end}: the gesture changed`);
    assert.match(publishImage(model, testImage()).error, /opacity change was open/, `${end}: an open gesture blocks a publish`);
    assert.equal(model.endOpacity(), true, `${end}: the change is one undo step`);
    assert.deepEqual([model.gestureOpen, model.gestureChanged], [false, false], `${end}: the gesture clears`);
    assert.equal(publishImage(model, testImage()).error, "", `${end}: an import at once succeeds`);
    assert.equal(model.endOpacity(), false, `${end}: a later pointer up changes nothing`);
    model.history.undo(); model.history.undo();
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
// slot. UI stubs record messages without a browser DOM.
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
    ...editor, model, decoder: fake.deps, root: globalThis, MIB: 1024 * 1024,
    metadataWithGo: async (metadata) => {
      const asset = metadata.asset || { frameState: "none", frame: null, license: null };
      const problem = editor.assetError(asset); if (problem) throw new Error(problem);
      return { ...metadata, asset };
    },
    backgroundProposal: async (config, command) => {
      if (command.action === "initialize") return { value: { scenario: config, background: { imageKey: command.imageKey, x: 0, y: 0, width: command.width, height: command.height, opacity: command.background ? command.background.opacity : editor.DEFAULT_OPACITY, frameState: "none" } } };
      const problem = editor.frameError(command.frame);
      if (problem) throw new Error(problem);
      const reference = editor.referenceFor({ config, frame: command.frame, choice: command.choice });
      if (reference.error) throw new Error(reference.error);
      return { value: editor.framedValue({ scenario: config, background: command.background }, { key: command.imageKey, frame: command.frame }, reference.geo), note: reference.note };
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

test("the frame lifecycle keeps the image, the placement, the frame state and the geo reference in one history step", () => {
  const config = { ...connectedScenario(), geo: LONDON_GEO };
  const model = testModel({ config });
  const image = testImage({ frame: LONDON_FRAME, license: OSM_LICENSE });
  publishImage(model, image, { value: editor.framedValue(model.history.value, image, LONDON_GEO) });
  const view = () => editor.frameView(model.history.value, model.image(image.key));
  assert.deepEqual(view(), { framed: true, state: "attached", warning: "", placeReason: "", calibrateReason: "Detach the frame before you calibrate the scale." });
  // Load live with a different reference keeps the placement and shows
  // the warning.
  const placement = model.history.background;
  const other = editor.makeGeo(51.52, -0.12);
  model.history.replace({ scenario: { ...config, geo: other }, background: placement });
  assert.equal(view().warning, editor.FRAME_WARNING_TEXT);
  // Load live with no reference also shows it, and Place from frame needs
  // a reference choice.
  const bare = connectedScenario();
  model.history.replace({ scenario: bare, background: placement });
  assert.deepEqual([view().warning, view().placeReason.length > 0], [editor.FRAME_WARNING_TEXT, true]);
  assert.match(editor.referenceFor({ config: bare, frame: LONDON_FRAME, choice: null }).error, /no geographic reference/);
  assert.match(editor.referenceFor({ config: bare, frame: LONDON_FRAME, choice: { mode: "adopt", confirmed: false } }).error, /Confirm/);
  const adopted = editor.referenceFor({ config: bare, frame: LONDON_FRAME, choice: { mode: "adopt", confirmed: true } });
  assert.deepEqual(adopted.geo, editor.frameCenterGeo(LONDON_FRAME));
  // Place from frame with a reference is one step with the new geo.
  model.history.replace({ scenario: { ...config, geo: other }, background: placement });
  model.history.replace(editor.framedValue(model.history.value, image, other));
  assert.deepEqual([view().warning, model.history.value.scenario.geo], ["", other]);
  model.history.undo();
  assert.deepEqual([view().warning, model.history.background], [editor.FRAME_WARNING_TEXT, placement], "one undo step brings back the placement");
  // Detach frame keeps the frame and the license of the image.
  model.history.redo(); model.history.replace(editor.detachFrame(model.history.value));
  assert.deepEqual(view(), { framed: true, state: "detached", warning: "", placeReason: "", calibrateReason: "" });
  assert.deepEqual([model.image(image.key).frame, model.image(image.key).license], [LONDON_FRAME, OSM_LICENSE]);
  // Remove background keeps the image while an undo step refers to it.
  model.history.replace({ scenario: model.history.value.scenario, background: null });
  assert.equal(editor.frameView(model.history.value, null).framed, false);
  assert.ok(model.image(image.key));
  // A project with no nodes adopts the image center with no question.
  assert.deepEqual(editor.referenceFor({ config: editor.emptyConfig(), frame: LONDON_FRAME, choice: null }).geo, editor.frameCenterGeo(LONDON_FRAME));
});

test("an anchor reference maps its nodes, and each anchor needs a node and a place", () => {
  const config = connectedScenario(); const [a, b] = farNodes(config);
  const at = (node) => ({ id: node.ID, ...editor.unprojectPoint(LONDON_GEO, node.Position) });
  const result = editor.referenceFor({ config, frame: LONDON_FRAME, choice: { mode: "anchor", a: at(a), b: at(b) } });
  assert.ok(Math.abs(result.geo.latitude - LONDON_GEO.latitude) < 1e-9 && Math.abs(result.geo.longitude - LONDON_GEO.longitude) < 1e-9, JSON.stringify(result));
  assert.match(result.note, /^The second anchor is 0\.0 m from its node\.$/);
  assert.match(editor.referenceFor({ config, frame: LONDON_FRAME, choice: { mode: "anchor", a: { ...at(a), id: "none" }, b: at(b) } }).error, /The anchor node "none" is not in the project/);
  assert.match(editor.referenceFor({ config, frame: LONDON_FRAME, choice: { mode: "anchor", a: { ...at(a), latitude: NaN }, b: at(b) } }).error, /needs a latitude and a longitude/);
  // A frame far from the reference fails the accuracy budget.
  assert.match(editor.referenceFor({ config: { ...config, geo: LONDON_GEO }, frame: { ...LONDON_FRAME, south: 52.5, north: 52.6 }, choice: null }).error, /too far north or south/);
});

test("the export writes the asset of each frame state, and the import checks it", () => {
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
    // A background with no frame and no license has no asset member, and
    // imports with the frame state "none".
    assert.equal(text.includes('"asset"'), item.frameState !== "none" || item.license !== null);
    assert.deepEqual(parsed.asset, item);
    assert.equal(editor.frameView({ scenario: { ...config, geo: LONDON_GEO }, background }, image).warning, item.frameState === "attached" ? editor.FRAME_WARNING_TEXT : "");
  }
  const documentWith = (asset) => JSON.stringify({ format: "podsim", version: 1, scenario: config, background: { dataURL: dataURL(pngBytes(4, 2)), x: 0, y: 0, width: 1, height: 1, opacity: 1, asset } });
  const rejected = [
    { name: "a list", asset: [], want: /asset must be an object/ },
    { name: "another member", asset: { frameState: "none", tiles: 1 }, want: /unknown member/ },
    { name: "an unknown state", asset: { frameState: "pinned" }, want: /frame state must be/ },
    { name: "none with a frame", asset: { frameState: "none", frame: LONDON_FRAME }, want: /"none" cannot have a frame/ },
    { name: "attached with no frame", asset: { frameState: "attached" }, want: /"attached" needs a frame/ },
    { name: "detached with no frame", asset: { frameState: "detached", frame: null }, want: /"detached" needs a frame/ },
    { name: "a frame across the antimeridian", asset: { frameState: "attached", frame: { ...LONDON_FRAME, west: 179, east: -179 } }, want: /antimeridian/ },
    { name: "an HTTP license link", asset: { frameState: "none", license: { ...OSM_LICENSE, licenseURL: "http://example.com" } }, want: /HTTPS/ },
  ];
  for (const item of rejected) assert.throws(() => editor.parseDocument(documentWith(item.asset)), item.want, item.name);
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

test("history edits share unchanged data and isolate undo snapshots", () => {
  const initial = { scenario: editor.fallbackConfig(), background: null };
  const history = editor.createHistory(initial);
  const before = history.snapshot;
  assert.ok(Object.isFrozen(before.scenario.network.Nodes));
  assert.ok(history.edit((config) => { config.demand.perMinute = 37; }));
  assert.equal(history.snapshot.scenario.network, before.scenario.network);
  assert.equal(before.scenario.demand.perMinute, initial.scenario.demand.perMinute);
  assert.equal(history.snapshot.scenario.demand.perMinute, 37);
  assert.equal(history.edit((config) => { config.demand.perMinute = 37; }), false);
  const changed = history.snapshot;
  assert.ok(history.undo()); assert.equal(history.snapshot, before);
  assert.ok(history.redo()); assert.equal(history.snapshot, changed);
  const copy = history.value; copy.scenario.demand.perMinute = 99;
  assert.equal(history.snapshot.scenario.demand.perMinute, 37);
  assert.ok(history.edit((config) => { config.network.Stations[0].Name = "Renamed"; }));
  assert.equal(history.snapshot.scenario.network.Lanes, before.scenario.network.Lanes);
  assert.equal(before.scenario.network.Stations[0].Name, initial.scenario.network.Stations[0].Name);
});

test("history replacement isolates external data and reuses unchanged branches", () => {
  const history = editor.createHistory({ scenario: editor.fallbackConfig(), background: null });
  const before = history.snapshot;
  const next = history.value; next.scenario.name = "Changed";
  assert.ok(history.replace(next));
  assert.equal(history.snapshot.scenario.network, before.scenario.network);
  next.scenario.name = "External edit";
  assert.equal(history.snapshot.scenario.name, "Changed");
  assert.equal(history.replace(history.value), false);
  history.undo(); assert.equal(history.snapshot, before);
});

test("history owns shallow-frozen inputs and opacity undo entries", () => {
  const history = editor.createHistory({ scenario: editor.fallbackConfig(), background: null });
  const external = editor.fallbackConfig(); external.name = "External";
  history.replace(Object.freeze({ scenario: external, background: null }));
  external.name = "Changed outside";
  assert.equal(history.snapshot.scenario.name, "External");
  const before = history.value;
  const next = history.value; next.scenario.name = "Gesture";
  history.commitFrom(before, next); history.undo();
  assert.ok(Object.isFrozen(history.snapshot.scenario.network.Nodes));
  assert.equal(history.snapshot.scenario.name, "External");
});

test("history edits finalize nested assignments and array changes", () => {
  const history = editor.createHistory({ scenario: editor.fallbackConfig(), background: null });
  const before = history.snapshot;
  history.edit(config => {
    config.extra = { demand: config.demand };
    config.fleet.push({ ID: "extra", StationID: "missing", BerthID: "missing" });
  });
  assert.ok(Object.isFrozen(history.snapshot.scenario.extra.demand));
  assert.equal(history.snapshot.scenario.extra.demand, before.scenario.demand);
  assert.equal(history.snapshot.scenario.fleet.length, before.scenario.fleet.length + 1);
  history.undo(); assert.equal(history.snapshot, before);
});

test("history preserves own JSON keys without prototype setters", () => {
  const history = editor.createHistory({ scenario: editor.fallbackConfig(), background: null });
  const next = history.value;
  next.scenario.extra = JSON.parse('{"__proto__":{"value":12}}');
  history.replace(next);
  const extra = history.snapshot.scenario.extra;
  assert.equal(Object.getPrototypeOf(extra), Object.prototype);
  assert.ok(Object.hasOwn(extra, "__proto__"));
  assert.equal(extra.__proto__.value, 12);
});

test("rail plans survive import normalization and mirror portable bounds", () => {
  const config = connectedScenario(); const [hub, destination] = config.network.Stations;
  const arrival = { id: "train", station: hub.ID, atSeconds: 60, walkingSeconds: 15, passengers: 120, destinations: [{ station: destination.ID, weight: 1 }] };
  config.railArrivals = [arrival];
  assert.deepEqual(editor.normalizeConfig(config).railArrivals, [arrival]);
  assert.deepEqual(editor.validateConfig(config), []);
  const tests = [
    ["id", ""], ["id", "x".repeat(65)], ["station", "missing"],
    ["atSeconds", -1], ["atSeconds", 86401], ["atSeconds", 86390], ["atSeconds", 1.5],
    ["walkingSeconds", -1], ["walkingSeconds", 3601],
    ["passengers", 0], ["passengers", 201], ["passengers", 1.5],
    ["destinations", []], ["destinations", new Array(17).fill(arrival.destinations[0])],
    ["destinations", [{ station: destination.ID, weight: 0 }]],
    ["destinations", [{ station: destination.ID, weight: 1000001 }]],
    ["destinations", [{ station: hub.ID, weight: 1 }]],
    ["destinations", [arrival.destinations[0], arrival.destinations[0]]],
  ];
  for (const [field, value] of tests) {
    const changed = structuredClone(config); changed.railArrivals[0][field] = value;
    assert.ok(editor.validateConfig(changed).some((error) => error.startsWith("Rail arrival")), `${field}=${JSON.stringify(value)}`);
  }
  config.railArrivals.push({ ...arrival, id: "second" });
  assert.ok(editor.validateConfig(config).some((error) => error.includes("one release tick")));
  config.railArrivals[0].passengers = 100; config.railArrivals[1].passengers = 100;
  assert.deepEqual(editor.validateConfig(config), []);
  config.railArrivals = Array.from({ length: 256 }, (_, index) => ({ ...arrival, id: String(index), atSeconds: index, walkingSeconds: 0, passengers: 1 }));
  assert.deepEqual(editor.validateConfig(config), []);
  config.railArrivals.push({ ...arrival, id: "extra" });
  assert.ok(editor.validateConfig(config).some((error) => error.includes("256")));
  config.railArrivals = Array.from({ length: 50 }, (_, index) => ({ ...arrival, id: String(index), atSeconds: index * 600, passengers: 200 }));
  assert.deepEqual(editor.validateConfig(config), []);
  config.railArrivals.push({ ...arrival, id: "extra", atSeconds: 40000, passengers: 1 });
  assert.ok(editor.validateConfig(config).some((error) => error.includes("10000")));
});


test("rail demand requires a plan even when disabled and retains its pattern", () => {
  const config = connectedScenario(); config.demand.pattern = "rail-arrivals";
  assert.ok(editor.validateConfig(config).some((error) => error.includes("nonempty arrival plan")));
  const [hub, destination] = config.network.Stations;
  config.railArrivals = [{ id: "train", station: hub.ID, atSeconds: 0, walkingSeconds: 0, passengers: 1, destinations: [{ station: destination.ID, weight: 1 }] }];
  for (const enabled of [false, true]) {
    config.demand.enabled = enabled;
    assert.deepEqual(editor.validateConfig(config), []);
    assert.equal(editor.normalizeConfig(config).demand.pattern, "rail-arrivals");
  }
});

test("rail arrival edits retain geometry, event identity, and portable round trips", () => {
  const original = connectedScenario(); let config = editor.addRailArrival(original).config;
  const [hub, destination] = config.network.Stations;
  assert.equal(config.railArrivals.length, 1);
  assert.deepEqual(editor.validateConfig(config), []);
  assert.equal(original.railArrivals, undefined);
  const id = config.railArrivals[0].id;
  config = editor.editRailArrival(config, id, (arrival) => { arrival.atSeconds = 600; arrival.walkingSeconds = 45; arrival.passengers = 100; arrival.destinations[0].weight = 9; return arrival; });
  assert.deepEqual(config.network, original.network);
  assert.deepEqual(config.fleet, original.fleet);
  assert.equal(config.railArrivals[0].id, id);
  assert.equal(config.railArrivals[0].station, hub.ID);
  assert.equal(config.railArrivals[0].destinations[0].station, destination.ID);
  assert.deepEqual(editor.parseDocument(editor.serializeDocument(config)).scenario.railArrivals, config.railArrivals);
  const added = editor.addRailArrival(config); assert.equal(added.error, "");
  assert.notEqual(added.config.railArrivals[1].id, id);
  assert.equal(added.config.railArrivals[1].atSeconds, 1200);
  assert.deepEqual(editor.removeRailArrival(added.config, added.config.railArrivals[1].id).railArrivals, config.railArrivals);
  assert.equal(editor.addRailDestination(config, id).config, config);
  assert.match(editor.addRailDestination(config, id).error, /already destinations/);
  let third = editor.addStation(config, 620, 240, { name: "Gamma" });
  const next = editor.addRailDestination(third, id); assert.equal(next.error, "");
  assert.equal(next.config.railArrivals[0].destinations.length, 2);
  assert.equal(third.railArrivals[0].destinations.length, 1);
});

test("station deletion cleans rail events and destinations without changing the source", () => {
  let config = editor.addRailArrival(connectedScenario()).config;
  config = editor.addStation(config, 620, 240, { name: "Gamma" });
  const [hub, destination, third] = config.network.Stations; const id = config.railArrivals[0].id;
  config = editor.addRailDestination(config, id).config;
  const before = structuredClone(config);
  assert.equal(editor.stationRailReferences(config, hub.ID), 1);
  assert.equal(editor.stationRailReferences(config, destination.ID), 1);
  assert.deepEqual(editor.deleteStation(config, hub.ID).railArrivals, []);
  const removed = editor.deleteStation(config, destination.ID);
  assert.deepEqual(removed.railArrivals[0].destinations, [{ station: third.ID, weight: 1 }]);
  assert.deepEqual(editor.deleteStation(removed, third.ID).railArrivals, []);
  assert.deepEqual(config, before);
});

test("rail changes form undo steps and preserve the frozen network", () => {
  const config = editor.addRailArrival(connectedScenario()).config;
  const history = editor.createHistory({ scenario: config, background: null });
  const network = history.snapshot.scenario.network; const id = history.value.scenario.railArrivals[0].id;
  const edited = editor.editRailArrival(history.snapshot.scenario, id, (arrival) => { arrival.walkingSeconds = 45; return arrival; });
  assert.equal(edited.network, network);
  history.replace({ scenario: edited, background: null });
  assert.equal(history.value.scenario.railArrivals[0].walkingSeconds, 45);
  assert.equal(history.undo(), true);
  assert.equal(history.value.scenario.railArrivals[0].walkingSeconds, 0);
  assert.equal(history.redo(), true);
  assert.equal(history.value.scenario.railArrivals[0].walkingSeconds, 45);
  const beforeDelete = history.value;
  history.replace({ scenario: editor.deleteStation(history.value.scenario, config.network.Stations[0].ID), background: null });
  assert.deepEqual(history.value.scenario.railArrivals, []);
  history.undo(); assert.deepEqual(history.value, beforeDelete);
});

test("rail helpers detach mutable input plans and unchanged nested destinations", () => {
  const config = editor.addRailArrival(editor.addRailArrival(connectedScenario()).config).config;
  const id = config.railArrivals[0].id;
  const results = [
    editor.addRailArrival(config).config,
    editor.removeRailArrival(config, id),
    editor.editRailArrival(config, id, (arrival) => { arrival.passengers = 1; return arrival; }),
  ];
  for (const result of results) {
    const untouched = result.railArrivals.find((arrival) => arrival.id === config.railArrivals[1].id);
    untouched.destinations[0].weight = 99;
    assert.equal(config.railArrivals[1].destinations[0].weight, 1);
  }
});

// This fixture keeps the mainline throat fixed while station rows move.
function layoutFixture(count = 3, side = 1, degrees = 0) {
  const config = editor.emptyConfig(); const nodes = config.network.Nodes; const lanes = config.network.Lanes;
  const node = (ID, X, Y) => nodes.push({ ID, Position: { X, Y } });
  const lane = (ID, From, To, StationRole, SeparationGroup = "station-plane") => lanes.push({ ID, From, To, SpeedLimit: 14, StationID: "layout", StationRole, SeparationGroup });
  node("entry", -100, 120); node("exit", 100, 120); node("diverge", -60, 120 - side * 120); node("merge", 60, 120 - side * 120);
  lane("access-in", "diverge", "entry", "entry"); lane("access-out", "exit", "merge", "exit"); lane("bypass", "entry", "exit", "through");
  const station = { ID: "layout", Name: "Layout", Entry: "entry", Exit: "exit", ParkingOnly: false, Berths: [] };
  for (let index = 0; index < count; index += 1) {
    const id = String(index); const y = 120 + side * (90 + index * 75);
    node(`a${id}`, -100, y); node(`b${id}`, 0, y); node(`d${id}`, 100, y);
    lane(`al${id}`, index ? `a${index - 1}` : "entry", `a${id}`, "berth-access"); lane(`dl${id}`, `d${id}`, index ? `d${index - 1}` : "exit", "departure");
    lane(`in${id}`, `a${id}`, `b${id}`, "berth-access"); lane(`out${id}`, `b${id}`, `d${id}`, "departure");
    station.Berths.push({ ID: `berth${id}`, Node: `b${id}`, SeparationGroup: "station-plane" });
  }
  config.network.Stations.push(station); config.fleet.push({ ID: "Pod01", BerthID: "berth0" });
  const radians = degrees * Math.PI / 180;
  for (const item of nodes) { const { X, Y } = item.Position; item.Position = { X: X * Math.cos(radians) - Y * Math.sin(radians), Y: X * Math.sin(radians) + Y * Math.cos(radians) }; }
  return config;
}

test("station layout dimensions preserve rotated and mirrored frames", () => {
  for (const side of [-1, 1]) for (const bearing of [0, 37, 90, 180, 271]) {
    const config = layoutFixture(3, side, bearing); const before = structuredClone(config);
    const layout = editor.stationLayout(config, "layout"); assert.equal(layout.error, "");
    assert.ok(Math.abs(layout.pitch - 75) < 1e-6); assert.ok(Math.abs(layout.spacing - 200) < 1e-6); assert.ok(Math.abs(layout.setback - 120) < 1e-6);
    const result = editor.setStationLayout(config, "layout", { pitch: 40, spacing: 160, setback: 140 }); assert.equal(result.error, "", result.error);
    const after = editor.stationLayout(result.config, "layout"); assert.equal(after.error, "");
    for (const [key, value] of Object.entries({ pitch: 40, spacing: 160, setback: 140 })) assert.ok(Math.abs(after[key] - value) < 1e-6, key);
    assert.deepEqual(config, before); assert.deepEqual(result.config.network.Lanes, config.network.Lanes); assert.deepEqual(result.config.network.Stations, config.network.Stations); assert.deepEqual(result.config.fleet, config.fleet);
    for (const id of ["diverge", "merge"]) assert.deepEqual(nodePosition(result.config, id), nodePosition(config, id));
    assert.deepEqual({ ...result.config, network: null }, { ...config, network: null });
    result.config.network.Nodes[0].Position.X += 1; assert.deepEqual(config, before);
  }
});

test("pitch keeps the first row fixed and mouth spacing keeps berth centers fixed", () => {
  const config = layoutFixture();
  const pitch = editor.setStationLayout(config, "layout", { pitch: 25 }); assert.equal(pitch.error, "");
  for (const id of ["entry", "exit", "a0", "b0", "d0"]) assert.deepEqual(nodePosition(pitch.config, id), nodePosition(config, id));
  assert.equal(nodePosition(pitch.config, "b2").Y - nodePosition(config, "b2").Y, -100);
  const spacing = editor.setStationLayout(config, "layout", { spacing: 120 }); assert.equal(spacing.error, "");
  for (const id of ["b0", "b1", "b2"]) assert.deepEqual(nodePosition(spacing.config, id), nodePosition(config, id));
  assert.equal(nodePosition(spacing.config, "entry").X, -60); assert.equal(nodePosition(spacing.config, "exit").X, 60);
  assert.equal(editor.setStationLayout(config, "layout", { pitch: 75, spacing: 200, setback: 120 }).config, config);
  assert.equal(editor.setStationLayout(config, "layout", {}).config, config);
});

test("one-row stations expose mouth and approach controls without inventing pitch", () => {
  const config = layoutFixture(1); const layout = editor.stationLayout(config, "layout"); assert.equal(layout.error, ""); assert.equal(layout.pitch, null);
  assert.match(editor.setStationLayout(config, "layout", { pitch: 50 }).error, /two rows/);
  assert.equal(editor.setStationLayout(config, "layout", { spacing: 150, setback: 160 }).error, "");
  config.network.Lanes = config.network.Lanes.filter((lane) => lane.ID !== "access-in");
  assert.equal(editor.stationLayout(config, "layout").setback, null);
  assert.match(editor.setStationLayout(config, "layout", { setback: 160 }).error, /paired, aligned throat/);
  assert.equal(editor.setStationLayout(config, "layout", { spacing: 150 }).error, "");
});

test("station dimension bounds and short lanes reject the whole edit", () => {
  const config = layoutFixture(); const before = structuredClone(config);
  for (const dimensions of [{ pitch: 24 }, { spacing: 47 }, { setback: 0 }, { pitch: NaN }, { spacing: Infinity }, { setback: 200000 }, { unknown: 30 }]) {
    const result = editor.setStationLayout(config, "layout", dimensions); assert.equal(result.config, config); assert.ok(result.error.startsWith("Layout:"), JSON.stringify(dimensions));
  }
  const short = editor.setStationLayout(config, "layout", { spacing: 120, setback: 10 }); assert.match(short.error, /access-in.*shorter than 24/); assert.equal(short.config, config);
  assert.deepEqual(config, before);
});

test("layout clearance accepts exactly 24 m lanes and 12 m gaps", () => {
  const config = layoutFixture(); const minimum = editor.setStationLayout(config, "layout", { spacing: 48 });
  assert.equal(minimum.error, "", minimum.error); assert.equal(editor.laneLength(minimum.config, minimum.config.network.Lanes.find((lane) => lane.ID === "in0")), 24);
  for (const gap of [12, 12 - 1e-6]) {
    const obstacle = layoutFixture(); obstacle.network.Nodes.push({ ID: "near-a", Position: { X: -75, Y: 250 + gap } }, { ID: "near-b", Position: { X: -25, Y: 250 + gap } });
    obstacle.network.Lanes.push({ ID: "near", From: "near-a", To: "near-b", SpeedLimit: 14, SeparationGroup: "station-plane" });
    const result = editor.setStationLayout(obstacle, "layout", { pitch: 40 });
    if (gap === 12) assert.equal(result.error, "", result.error); else { assert.equal(result.config, obstacle); assert.match(result.error, /in1.*near/); }
  }
});

test("custom, curved and shared berth layouts remain manual", () => {
  const cases = [
    (config) => { nodePosition(config, "b1").X += 1; },
    (config) => { nodePosition(config, "b2").Y += 1; },
    (config) => { config.network.Lanes[0].Control = { X: 0, Y: 10 }; },
    (config) => { config.network.Lanes.push({ ID: "foreign", From: "a0", To: "diverge", SpeedLimit: 14 }); },
    (config) => { config.network.Stations.push({ ID: "other", Entry: "a0", Exit: "exit", Berths: [] }); },
    (config) => { config.network.Stations[0].Berths[1].Node = "b0"; },
  ];
  for (const change of cases) { const config = layoutFixture(); change(config); const result = editor.setStationLayout(config, "layout", { pitch: 50 }); assert.equal(result.config, config); assert.ok(result.error); }
  assert.ok(editor.stationLayout(connectedScenario(), connectedScenario().network.Stations[0].ID).error);
});

test("station layout checks moved approaches and retains existing separation planes", () => {
  for (const group of ["", "station-plane", "other-plane"]) {
    const config = layoutFixture(); config.network.Nodes.push({ ID: "obstacle-a", Position: { X: -90, Y: 130 } }, { ID: "obstacle-b", Position: { X: -130, Y: 130 } });
    config.network.Lanes.push({ ID: "obstacle", From: "obstacle-a", To: "obstacle-b", SpeedLimit: 14, SeparationGroup: group });
    const result = editor.setStationLayout(config, "layout", { setback: 160 });
    if (group === "other-plane") { assert.equal(result.error, ""); assert.deepEqual(result.config.network.Lanes, config.network.Lanes); }
    else { assert.equal(result.config, config); assert.match(result.error, /access-in.*obstacle/); }
  }
});

test("layout preview is one undo step and export stores only node coordinates", () => {
  const config = layoutFixture(); const history = editor.createHistory({ scenario: config, background: null });
  const result = editor.setStationLayout(history.snapshot.scenario, "layout", { pitch: 50, spacing: 160, setback: 140 }); assert.equal(result.error, "");
  history.replace({ scenario: result.config, background: null });
  const exported = JSON.parse(JSON.stringify(history.value.scenario)); assert.deepEqual(Object.keys(exported), Object.keys(config));
  assert.deepEqual(exported, result.config); history.undo(); assert.deepEqual(history.value.scenario, config); history.redo(); assert.deepEqual(history.value.scenario, result.config);
});

test("layout controls recognize generated London and scale100 stations", needsGo, () => {
  for (const preset of ["london-central", "london-full", "scale100"]) {
    const config = generatedProject(preset); const stations = config.network.Stations.filter((station) => !station.ParkingOnly);
    assert.ok(stations.length > 10);
    for (const station of stations) {
      const layout = editor.stationLayout(config, station.ID); assert.equal(layout.error, "", station.Name); assert.ok(layout.spacing > 0); assert.ok(layout.setback > 0, station.Name);
    }
    const chosen = preset.startsWith("london") ? stations.find((station) => station.Name === "Acton Town") || stations.find((station) => station.Name === "Tottenham Court Road") : stations[4];
    const current = editor.stationLayout(config, chosen.ID);
    const result = editor.setStationLayout(config, chosen.ID, { pitch: 60, spacing: current.spacing * 0.9, setback: current.setback + 20 }); assert.equal(result.error, "", result.error);
    assert.deepEqual(editor.validateConfig(result.config), []);
  }
});

test("rail departures validate windows, weighted origins, and combined limits", () => {
  const config = editor.addRailDeparture(connectedScenario()).config;
  config.demand.pattern = "rail-services";
  assert.deepEqual(editor.validateConfig(config), []);
  assert.equal(editor.normalizeConfig(config).demand.pattern, "rail-services");
  const [hub, origin] = config.network.Stations;
  const base = config.railDepartures[0];
  for (const [field, value] of [
    ["id", ""], ["id", "x".repeat(65)], ["station", "missing"], ["atSeconds", 0], ["atSeconds", 86401],
    ["walkingSeconds", -1], ["walkingSeconds", 3601], ["requestFromSeconds", -1], ["requestFromSeconds", 301],
    ["requestUntilSeconds", 540], ["requestUntilSeconds", 1.5], ["passengers", 0], ["passengers", 201],
    ["origins", []], ["origins", [{ station: hub.ID, weight: 1 }]], ["origins", [{ station: origin.ID, weight: 0 }]],
    ["origins", [{ station: origin.ID, weight: 1000001 }]], ["origins", [base.origins[0], base.origins[0]]],
  ]) {
    const changed = structuredClone(config); changed.railDepartures[0][field] = value;
    assert.ok(editor.validateConfig(changed).some((error) => error.startsWith("Rail departure")), `${field}=${JSON.stringify(value)}`);
  }
  config.railDepartures = Array.from({ length: 15 }, (_, index) => ({ ...base, id: `out-${index}`, requestFromSeconds: index, requestUntilSeconds: index, passengers: 200 }));
  assert.deepEqual(editor.validateConfig(config), []);
  config.railDepartures.push({ ...base, id: "extra", requestFromSeconds: 30, requestUntilSeconds: 30, passengers: 1 });
  assert.ok(editor.validateConfig(config).some((error) => error.includes("3000")));
  config.railDepartures.pop();
  config.railArrivals = Array.from({ length: 35 }, (_, index) => ({ id: `in-${index}`, station: hub.ID, atSeconds: 1000 + index, walkingSeconds: 0, passengers: 200, destinations: [{ station: origin.ID, weight: 1 }] }));
  assert.deepEqual(editor.validateConfig(config), []);
  config.railArrivals.push({ ...config.railArrivals[0], id: "extra", atSeconds: 2000, passengers: 1 });
  assert.ok(editor.validateConfig(config).some((error) => error.includes("10000 combined")));
  config.railArrivals = [{ ...config.railArrivals[0], passengers: 1, atSeconds: 0 }];
  assert.ok(editor.validateConfig(config).some((error) => error.includes("one release tick")));
});

test("departure edits, origins, deletion, and undo retain owned plans", () => {
  let original = editor.addStation(connectedScenario(), 580, 100, { name: "Gamma" }); const [hub, origin, third] = original.network.Stations;
  original = editor.addLane(original, origin.Exit, third.Entry, false); original = editor.addLane(original, third.Exit, hub.Entry, false);
  let config = editor.addRailDeparture(original).config;
  assert.equal(original.railDepartures, undefined);
  const id = config.railDepartures[0].id;
  config = editor.addRailOrigin(config, id).config;
  const history = editor.createHistory({ scenario: config, background: null });
  const network = history.snapshot.scenario.network;
  const edited = editor.editRailDeparture(history.snapshot.scenario, id, (departure) => { departure.requestFromSeconds = 45; departure.origins[0].weight = 9; return departure; });
  assert.equal(edited.network, network);
  history.replace({ scenario: edited, background: null });
  assert.equal(history.value.scenario.railDepartures[0].requestFromSeconds, 45);
  assert.equal(history.undo(), true); assert.equal(history.value.scenario.railDepartures[0].requestFromSeconds, 0);
  assert.equal(history.redo(), true);
  assert.deepEqual(editor.parseDocument(editor.serializeDocument(history.value.scenario)).scenario.railDepartures, history.value.scenario.railDepartures);
  assert.equal(editor.stationRailReferences(config, hub.ID), 1);
  assert.equal(editor.stationRailReferences(config, origin.ID), 1);
  const removed = editor.deleteStation(config, origin.ID);
  assert.deepEqual(removed.railDepartures[0].origins, [{ station: third.ID, weight: 1 }]);
  assert.deepEqual(editor.deleteStation(removed, third.ID).railDepartures, []);
  history.replace({ scenario: editor.deleteStation(history.value.scenario, hub.ID), background: null });
  assert.deepEqual(history.value.scenario.railDepartures, []);
  history.undo(); assert.equal(history.value.scenario.railDepartures.length, 1);
  const added = editor.addRailDeparture(config).config;
  added.railDepartures[0].origins[0].weight = 99;
  assert.equal(config.railDepartures[0].origins[0].weight, 1);
  assert.deepEqual(editor.removeRailDeparture(config, id).railDepartures, []);
});

test("a canceled Go opacity proposal rolls back its owned preview", async () => {
  const source = fs.readFileSync(path.join(__dirname, "editor.js"), "utf8");
  const names = ["backgroundProposal", "queueBackgroundEdit"];
  const functions = names.map((name) => {
    const match = new RegExp(`\\n  (?:async )?function ${name}\\(`).exec(source);
    assert.ok(match);
    return source.slice(match.index, source.indexOf("\n  }\n", match.index) + 5);
  }).join("\n");
  const initial = { scenario: connectedScenario(), background: { imageKey: TEST_KEY, x: 0, y: 0, width: 40, height: 20, opacity: .45, frameState: "none" } };
  const model = editor.createBackgroundModel({ initial });
  const editQueue = require("./editor-model.js").createEditQueue();
  const state = { history: model.history, drag: null };
  const response = Promise.withResolvers();
  const noop = () => {};
  const deps = {
    model, state, editQueue, pendingInputs: new Map(), pendingBackgroundPreviews: new Set(),
    draft: () => model.history.snapshot.scenario,
    goModel: { call: () => response.promise },
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
  const config = layoutFixture(); const station = config.network.Stations[0];
  const second = structuredClone(config);
  for (const node of second.network.Nodes) { node.ID = `b-${node.ID}`; node.Position.X += 600; }
  for (const lane of second.network.Lanes) { lane.ID = `b-${lane.ID}`; lane.From = `b-${lane.From}`; lane.To = `b-${lane.To}`; }
  const members = second.network.Stations[0].Berths;
  for (const berth of members) { berth.ID = `b-${berth.ID}`; berth.Node = `b-${berth.Node}`; }
  station.Banks = [
    { ID: "a", Entry: station.Entry, Exit: station.Exit, BerthIDs: station.Berths.map((berth) => berth.ID) },
    { ID: "b", Entry: "b-entry", Exit: "b-exit", BerthIDs: members.map((berth) => berth.ID) },
  ];
  station.Berths.push(...members); config.network.Nodes.push(...second.network.Nodes); config.network.Lanes.push(...second.network.Lanes);
  for (const prefix of ["", "b-"]) {
    config.network.Nodes.push({ ID: `${prefix}road-in`, Position: { X: prefix ? 540 : -60, Y: -100 } }, { ID: `${prefix}road-out`, Position: { X: prefix ? 660 : 60, Y: -100 } });
    config.network.Lanes.push({ ID: `${prefix}road-in`, From: `${prefix}road-in`, To: `${prefix}diverge`, SpeedLimit: 14 }, { ID: `${prefix}road-out`, From: `${prefix}merge`, To: `${prefix}road-out`, SpeedLimit: 14 });
  }
  config.version = 2;
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
  assert.equal(b.station.Entry, "b-entry"); assert.ok(b.approachLength > 120); assert.ok(b.departureLength > 120);
  Object.assign(config.network.Lanes.find((lane) => lane.ID === "b-road-in"), { StationID: "layout", StationRole: "approach" });
  assert.ok(editor.stationLayout(config, "layout", "b").approachLength > 120);
  config.network.Lanes.push({ ID: "shared", From: "b-diverge", To: "road-in", SpeedLimit: 14 });
  assert.equal(editor.stationLayout(config, "layout", "b").approachLength, null);
});

test("bank import and export preserve metadata and nested array caps", () => {
  const config = independentBankFixture();
  const sandbox = { structuredClone, TextEncoder, PodsimTiles: require("./tiles.js") };
  require("node:vm").runInNewContext(fs.readFileSync(path.join(__dirname, "editor.js"), "utf8"), sandbox);
  const browser = sandbox.PodsimEditorModel;
  assert.deepEqual(JSON.parse(JSON.stringify(browser.parseDocument(JSON.stringify(config)).scenario)), config);
  assert.deepEqual(JSON.parse(JSON.stringify(browser.parseDocument(editor.serializeDocument(config)).scenario)), config);
  for (const banks of [null, [], Array(9).fill(config.network.Stations[0].Banks[0])]) {
    const invalid = structuredClone(config); invalid.network.Stations[0].Banks = banks;
    assert.throws(() => browser.parseDocument(JSON.stringify(invalid)), /1 to 8 banks/);
  }
  const invalid = structuredClone(config); invalid.network.Stations[0].Banks[0].BerthIDs = Array(201).fill("berth0");
  assert.throws(() => browser.parseDocument(JSON.stringify(invalid)), /1 to 200 berth IDs/);
  const legacy = structuredClone(config); legacy.version = 1;
  assert.throws(() => browser.parseDocument(JSON.stringify(legacy)), /Version 1.*banks/);
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

test("deferred import retains PNG and JPEG data URL prefix admission", () => {
  const scenario = connectedScenario(), bytes = pngBytes(4, 2), base64 = Buffer.from(bytes).toString("base64");
  for (const prefix of ["data:image/gif;base64,", "garbage,", "", "data:image/png,", "DATA:image/png;base64,"]) {
    const background = { ...TEST_BACKGROUND, dataURL: prefix + base64 };
    const document = JSON.stringify({ format: "podsim", version: 1, scenario, background });
    for (const deferMetadata of [false, true]) assert.throws(() => editor.parseDocument(document, { deferMetadata }), /PNG or JPEG data URL/);
  }
  for (const type of ["png", "jpeg"]) {
    const background = { ...TEST_BACKGROUND, dataURL: dataURL(bytes, type) };
    const document = JSON.stringify({ format: "podsim", version: 1, scenario, background });
    assert.equal(editor.parseDocument(document, { deferMetadata: true }).background.dataURL, background.dataURL);
  }
});

test("deferred import preserves raw compatibility and metadata until Go verdict", () => {
  const scenario = { ...connectedScenario(), fleet: [{ StationID: "missing", BerthID: false }], demand: { pattern: "market", destination: false } };
  const background = { ...TEST_BACKGROUND, dataURL: dataURL(pngBytes(4, 2)), opacity: 5, asset: null };
  const parsed = editor.parseDocument(JSON.stringify({ format: "podsim", version: 1, scenario, background }), { deferMetadata: true });
  assert.deepEqual(parsed.scenario.fleet, scenario.fleet);
  assert.deepEqual(parsed.scenario.demand, scenario.demand);
  assert.equal(parsed.metadata.placement.opacity, 5);
  assert.equal(parsed.metadata.asset, null);
  assert.equal(Object.hasOwn(parsed.metadata, "dataURL"), false);
  assert.throws(() => editor.parseDocument(JSON.stringify({ format: "podsim", version: 1, scenario, background })), /opacity/);
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
    let config = { network: { Stations: [{ ID: "alpha" }, { ID: "beta" }] } };
    const state = { selection: { id: "alpha" } }, calls = [];
    const deps = { state, draft: () => config, selectedBank: () => undefined, setControlValue: (control, value) => { control.value = value; },
      goModel: { call(project, op, layout) { const pending = Promise.withResolvers(); calls.push({ project, op, layout, pending }); return pending.promise; } },
    };
    const render = new Function(...Object.keys(deps), `${source.slice(start, end)}\nreturn renderStationLayout;`)(...Object.values(deps));
    render(panel, config, "alpha");
    if (change === "selection") state.selection = { id: "beta" };
    else config = { network: { Stations: config.network.Stations } };
    render(panel, config, state.selection.id);
    const summary = (pitch) => ({ layout: { pitch: { value: pitch, reason: "" }, spacing: { value: 200, reason: "" }, setback: { value: 120, reason: "" }, approachLength: { value: null, reason: "No bank" }, departureLength: { value: null, reason: "No bank" } } });
    calls[0].pending.resolve(summary(75)); await tick();
    assert.ok(Object.values(inputs).every((input) => input.disabled && input.value === ""), "stale layout must not enable the current controls");
    calls[1].pending.resolve(summary(40)); await tick();
    assert.equal(inputs.pitch.disabled, false); assert.equal(inputs.pitch.value, "40");
    assert.equal(calls[1].op, "stationLayout");assert.equal(calls[1].layout.stationID, state.selection.id);
  });
});

test("large lane masks select bounds without changing ordinary constants", () => {
  const { laneMinimumLength } = require("./editor-service-reference.cjs");
  assert.equal(editor.MIN_LANE_LENGTH, 24);
  assert.equal(editor.CLEARANCE, 12);
  for (const [VehicleClasses, minimum] of [[undefined, 24], [["compact"], 24], [["group"], 40], [["express"], 40], [["legacy", "group"], 40]]) {
    const lane = VehicleClasses ? { VehicleClasses } : {};
    assert.equal(laneMinimumLength(lane), minimum);
  }
  for (const [large, gap, conflict] of [[false, 12, false], [false, 11, true], [true, 19, true], [true, 20, false]]) {
    const config = { network: { Nodes: [
      { ID: "a", Position: { X: 0, Y: 0 } }, { ID: "b", Position: { X: 100, Y: 0 } },
      { ID: "c", Position: { X: 0, Y: gap } }, { ID: "d", Position: { X: 100, Y: gap } },
    ], Lanes: [{ ID: "first", From: "a", To: "b", SpeedLimit: 14 }, { ID: "second", From: "c", To: "d", SpeedLimit: 14 }] } };
    if (large) config.network.Lanes[0].VehicleClasses = ["group"];
    assert.equal(Boolean(editor.laneConflict(config, ["second"])), conflict);
  }
});
