"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const { execFileSync } = require("node:child_process");
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

const generatedFiles = new Map();

// generatedFile runs the scenario command, so the fixture always matches the
// server generator. It runs the command once for each preset.
function generatedFile(preset) {
  if (!generatedFiles.has(preset)) {
    generatedFiles.set(preset, execFileSync("go", ["run", "./cmd/scenario", "-preset", preset], {
      cwd: path.join(__dirname, ".."), encoding: "utf8", maxBuffer: 64 * 1024 * 1024,
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

for (const preset of ["scale100", "london"]) {
  test(`the generated ${preset} project passes the editor checks`, () => {
    const config = generatedProject(preset);
    assert.deepEqual(editor.validateConfig(config), []);

    // A delete removes the berth chains and keeps the road network whole.
    const station = config.network.Stations.find((item) => !item.ParkingOnly);
    const errors = editor.validateConfig(editor.deleteStation(config, station.ID));
    assert.deepEqual(errors.filter((error) => !error.startsWith("Demand profile")), []);
  });

  test(`import accepts the generated ${preset} project file`, () => {
    assert.deepEqual(editor.parseDocument(generatedFile(preset)), { scenario: generatedProject(preset), background: null });
  });

  test(`a drag on the generated ${preset} project redraws only the moved items`, () => {
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

test("fit shows the whole London network below the 0.15 zoom floor", () => {
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
