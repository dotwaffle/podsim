"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const { execFileSync } = require("node:child_process");
const fs = require("node:fs");
const path = require("node:path");
const editor = require("./editor.js");

function connectedScenario() {
  let config = editor.addStation(editor.emptyConfig(), 100, 100, { name: "Alpha" });
  config = editor.addStation(config, 340, 100, { name: "Beta" });
  const [alpha, beta] = config.network.Stations;
  config = editor.addLane(config, alpha.Exit, beta.Entry, false);
  config = editor.addLane(config, beta.Exit, alpha.Entry, false);
  config.demand.destination = beta.ID;
  return editor.setFleetCount(config, alpha.ID, 1);
}

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
// server generator. It runs the command once for each preset. A test that
// calls it must use the needsGo options.
function generatedFile(preset) {
  if (!generatedFiles.has(preset)) {
    generatedFiles.set(preset, execFileSync("go", ["run", "./cmd/scenario", "-preset", preset], {
      cwd: repoRoot, encoding: "utf8", maxBuffer: 64 * 1024 * 1024,
    }));
  }
  return generatedFiles.get(preset);
}

// generatedProject loads a generated project as the editor loads the live
// server project.
function generatedProject(preset) {
  return editor.normalizeConfig(JSON.parse(generatedFile(preset)));
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
  config = editor.addBerth(config, stationID);
  const station = config.network.Stations[0];
  const removed = station.Berths[1];
  assert.equal(station.Berths.length, 2);
  assert.equal(config.network.Lanes.filter((lane) => lane.From === removed.Node || lane.To === removed.Node).length, 2);

  config = editor.setFleetCount(config, stationID, 2);
  assert.equal(config.fleet.length, 2);
  config = editor.removeBerth(config, stationID, removed.ID);

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
    "Demand profile weekend must contain 1 to 20000 flows.",
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

test("a station delete works on an imported export with no demand profiles", () => {
  // An older browser export has no demandProfiles field, and the import
  // keeps the scenario as it is.
  const scenario = connectedScenario();
  delete scenario.demandProfiles;
  const { scenario: config } = editor.parseDocument(JSON.stringify({ format: "podsim", version: 1, scenario }));
  const beta = config.network.Stations[1];

  assert.equal(editor.stationFlowCount(config, beta.ID), 0);
  const deleted = editor.deleteStation(config, beta.ID);
  assert.deepEqual(deleted.network.Stations.map((station) => station.ID), [config.network.Stations[0].ID]);
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

test("fleet counts never duplicate occupied berths", () => {
  let config = editor.addStation(editor.emptyConfig(), 200, 140, { name: "Depot", parkingOnly: true });
  const stationID = config.network.Stations[0].ID;
  config = editor.addBerth(config, stationID);
  config = editor.setFleetCount(config, stationID, 20);

  assert.equal(config.fleet.length, 2);
  assert.equal(new Set(config.fleet.map((pod) => pod.BerthID)).size, 2);
  assert.equal(new Set(config.fleet.map((pod) => pod.ID)).size, 2);
});

test("fleet rows give each station its pod count and berth limit", () => {
  const twoStations = () => {
    let config = editor.addStation(editor.emptyConfig(), 100, 100, { name: "Alpha" });
    config = editor.addStation(config, 340, 100, { name: "Beta" });
    return editor.addBerth(config, config.network.Stations[1].ID);
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
  const config = generatedProject("london");
  const lane = config.network.Lanes.find((item) => !item.StationID);
  lane.SpeedLimit = 0;
  const { errors, warnings } = editor.checkResults(config);
  assert.deepEqual(errors, [{ text: `Lane ${lane.ID} needs a positive speed limit.`, target: { type: "lane", id: lane.ID } }]);
  assert.deepEqual(warnings, []);
  assert.deepEqual(editor.checkSelection(config, errors[0].target), { type: "lane", id: lane.ID });
});

test("portable documents round trip the scenario and local background", () => {
  const config = connectedScenario();
  const background = { dataURL: "data:image/png;base64,AA==", x: -10, y: 5, width: 800, height: 600, opacity: 0.4 };
  const parsed = editor.parseDocument(editor.serializeDocument(config, background));

  assert.deepEqual(parsed.scenario, config);
  assert.deepEqual(parsed.background, background);
  assert.throws(() => editor.parseDocument('{"format":"podsim","version":2}'), /version field must be 1/);
  assert.throws(() => editor.parseDocument('{broken'), /not valid JSON/);
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
    { name: "a project of a different version", file: { ...scenario, version: 2 }, message: "The version field must be 1." },
    { name: "a project with no version", file: { ...scenario, version: undefined }, message: "The version field must be 1." },
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
  config.demand = { enabled: true, perMinute: 12, pattern: "profile", profile: "weekday", band: "am", seed: 9 };

  assert.deepEqual(editor.validateConfig(config), []);
  assert.deepEqual(editor.parseDocument(editor.serializeDocument(config)).scenario, config);
  config.demandProfiles[0].flows[0].weights = [-1];
  assert.ok(editor.validateConfig(config).some((error) => error.includes("invalid weight")));
});

test("the demand pattern change selects the first profile, or leaves a check error with no profiles", () => {
  // An older browser export has no demandProfiles field, and the import
  // keeps the scenario as it is.
  const exported = connectedScenario();
  delete exported.demandProfiles;
  const noProfiles = "The project has no demand profiles. Select another pattern.";
  const cases = [
    { name: "profiles present", config: profileScenario().config, profile: "weekday", band: "am", errors: [] },
    { name: "empty array", config: connectedScenario(), profile: "", band: "", errors: [noProfiles] },
    { name: "member missing", config: editor.parseDocument(JSON.stringify({ format: "podsim", version: 1, scenario: exported })).scenario, profile: "", band: "", errors: [noProfiles] },
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
    if (item.secondBerth !== undefined) config = editor.addBerth(config, config.network.Stations[item.secondBerth].ID);
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

for (const preset of ["scale100", "london"]) {
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
  });
}

// movedItems gives the nodes, lanes, and stations whose drawn position
// differs between two versions of a scenario.
function movedItems(before, after) {
  const same = (a, b) => JSON.stringify(a) === JSON.stringify(b);
  const positions = new Map(after.network.Nodes.map((node) => [node.ID, node.Position]));
  const controls = new Map(after.network.Lanes.map((lane) => [lane.ID, lane.Control]));
  const moved = new Set(before.network.Nodes.filter((node) => !same(node.Position, positions.get(node.ID))).map((node) => node.ID));
  return {
    nodeIDs: [...moved],
    laneIDs: before.network.Lanes.filter((lane) => moved.has(lane.From) || moved.has(lane.To) || !same(lane.Control, controls.get(lane.ID))).map((lane) => lane.ID),
    stationIDs: before.network.Stations.filter((station) => moved.has(station.Entry) || moved.has(station.Exit)).map((station) => station.ID),
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
  const config = generatedProject("london");
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
  const config = generatedProject("london");
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
// session: session_changed for another epoch, stale_project for a project
// command with another project revision, and command_rejected for a
// project command to a running simulation. commands records each command
// that the editor sent. options.stateSaved is the stateSaved member of the
// acknowledgment of an applied project. The acknowledgment omits the member
// when options.stateSaved is undefined.
function fakeSession(options) {
  const commands = [];
  const live = { epoch: "epoch-1", revision: options.liveRevision ?? 3, generation: 5, paused: options.paused };
  const reply = (status, body) => ({ ok: status < 300, status, json: async () => body });
  const acknowledgment = (rejection, command) => ({
    epoch: live.epoch, revision: commands.length, projectRevision: live.revision, generation: live.generation,
    ...(!rejection && command && command.action === "project" && options.stateSaved !== undefined ? { stateSaved: options.stateSaved } : {}),
    ...(rejection ? { errorCode: rejection.errorCode, error: rejection.error } : {}),
  });
  const handle = (command) => {
    if (command.epoch !== live.epoch) return { errorCode: "session_changed", error: "The server session changed. Review the current state and try again." };
    if (command.action === "pause") { live.paused = Boolean(command.paused); return null; }
    if (!live.paused) return { errorCode: "command_rejected", error: "pause the simulation before applying a project" };
    if (command.projectRevision !== live.revision) return { errorCode: "stale_project", error: "the project changed; reload it before applying edits" };
    Object.assign(live, { revision: 7, generation: live.generation + 1, paused: true });
    return null;
  };
  const fetch = async (url, init) => {
    if (url === "/api/project") return reply(200, { revision: live.revision, project: connectedScenario() });
    if (url === "/api/state") {
      return reply(200, { epoch: live.epoch, projectRevision: live.revision, generation: live.generation, simulation: { Paused: live.paused } });
    }
    const command = JSON.parse(init.body);
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
  const sessionChanged = "The server session changed. The draft stays on this page. Export the draft, reload the page, then import the draft.";
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
  const conflictStatus = "Apply conflict. Reload the page to get the current live scenario.";
  const sessionChanged = "The server session changed. The draft stays on this page. Export the draft, reload the page, then import the draft.";
  const sessionStatus = "The server session changed. Export the draft, reload the page, then import the draft.";
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
    assert.deepEqual(result, { revision: 7, stateSaved: item.stateSaved }, item.name);
    assert.deepEqual(editor.applyToast(result), [item.wantMessage, item.wantWarning], item.name);
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

  config = editor.addBerth(config, station.ID);
  assert.deepEqual(config.fleet[0], originalPod);
  const addedBerth = config.network.Stations[0].Berths[1];
  config = editor.setFleetCount(config, station.ID, 2);
  assert.deepEqual(config.fleet[0], originalPod);
  assert.equal(config.fleet[1].BerthID, addedBerth.ID);

  config = editor.removeBerth(config, station.ID, addedBerth.ID);
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
  config = editor.addBerth(config, config.network.Stations[0].ID);
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
 config.network.Lanes.push({...config.network.Lanes[0],ID:"parallel"});
 assert.deepEqual(editor.validateConfig(config),[]);
});
test("legacy market pattern uses the last passenger station when absent", () => {
 const config=connectedScenario();config.demand.pattern="market";config.demand.destination="";
 assert.equal(editor.normalizeConfig(config).demand.destination,config.network.Stations.at(-1).ID);
});

// cssRules gives the declarations of each rule in a style sheet, by
// selector. A rule with a selector list adds its declarations to each
// selector. A background-color declaration is stored as background, so
// that the later rule in cascade order sets the background. The parse does
// not know @media blocks, so use it only for selectors that no @media block
// sets.
function cssRules(css) {
  const rules = new Map();
  for (const [, selectors, body] of css.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
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
