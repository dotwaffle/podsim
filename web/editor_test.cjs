"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
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

test("station creation makes separate safe entry, exit, and berth geometry", () => {
  const config = editor.addStation(editor.emptyConfig(), 200, 140, { name: "Central" });
  const station = config.network.Stations[0];

  assert.notEqual(station.Entry, station.Exit);
  assert.notEqual(station.Berths[0].Node, station.Entry);
  assert.equal(config.network.Nodes.length, 3);
  assert.equal(config.network.Lanes.length, 3);
  for (const lane of config.network.Lanes) assert.ok(editor.laneLength(config, lane) >= editor.MIN_LANE_LENGTH);
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
  assert.throws(() => editor.parseDocument('{"format":"podsim","version":2}'), /version 1/);
  assert.throws(() => editor.parseDocument('{broken'), /not valid JSON/);
});

test("portable import accepts the legacy market demand pattern", () => {
  const config = connectedScenario();
  config.network.Stations[1].ID = "market";
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

test("a connected scenario passes all editor checks", () => {
  const config = connectedScenario();
  assert.deepEqual(editor.validateConfig(config), []);
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
