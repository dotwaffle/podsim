(function (root) {
  "use strict";
  if (typeof document === "undefined" && typeof importScripts === "function") importScripts("./tiles.js");
  const Tiles = typeof module !== "undefined" && module.exports ? require("./tiles.js") : root.PodsimTiles;

  const MIN_LANE_LENGTH = 24;
  const DEFAULT_SPEED = 12;
  // A view scale is in screen pixels per meter. Fit keeps FIT_MARGIN pixels of
  // the map free, half on each side of the network.
  const MIN_ZOOM = 0.15;
  const MAX_ZOOM = 5;
  const MAX_FIT_ZOOM = 3;
  const FIT_MARGIN = 100;
  // NODE_LABEL_SCALE is the lowest scale at which all junctions show their ID.
  // NODE_LABEL_SIZE is the font size of a junction ID label in meters.
  const NODE_LABEL_SCALE = 0.5;
  const NODE_LABEL_SIZE = 9;
  // LANE_PAIR_OFFSET is the distance in screen pixels that each lane of a pair
  // moves to the right of its direction of travel. A pair is two lanes between
  // the same two nodes in opposite directions.
  const LANE_PAIR_OFFSET = 4;
  // CHEVRON_LANE_LENGTH is the shortest length in screen pixels of a lane that
  // shows its direction chevron.
  const CHEVRON_LANE_LENGTH = 24;
  // BERTH_PITCH is the distance in meters from a new station to its first
  // berth, and from the last berth to the berth that addBerth adds to a
  // station that is not a berth chain. A berth chain uses its own pitch. See
  // nextChainRow. STATION_PADDING is the distance in meters from the outer
  // station nodes to the edge of the drawn station shape.
  const BERTH_PITCH = 30;
  const STATION_PADDING = 12;
  // CLEARANCE is the clearance in meters around a pod, sim.Clearance in the
  // Go code. A new berth chain row must keep this distance from other lanes.
  const CLEARANCE = 12;
  // MAX_STATIONS, MAX_BERTHS, MAX_NODES, MAX_LANES, MAX_NODE_LANES, and
  // MAX_FLOWS are limits of the server (internal/project/config.go). Keep
  // them the same. MAX_BERTHS applies to each station. MAX_NODE_LANES
  // counts each lane at its From node and at its To node.
  const MAX_PODS = 300;
  const MAX_STATIONS = 300;
  const MAX_BERTHS = 200;
  const MAX_NODES = 5000;
  const MAX_LANES = 8000;
  const MAX_NODE_LANES = 64;
  const MAX_FLOWS = 65000;
  // IMAGE_FILE_BYTES is the largest background image file that the editor
  // imports. SERVER_PROJECT_BYTES mirrors project.MaxFileBytes on the server,
  // the largest compact project. A Go test in internal/project checks the
  // mirror. PROJECT_FILE_BYTES is the largest project file that the editor
  // imports. It holds an export of the largest project with the largest
  // background: the image as a base64 data URL, the project,
  // EXPORT_ALLOWANCE bytes for the other fields, and ASSET_ALLOWANCE bytes
  // for the frame and the license of the image, rounded up to a whole MiB.
  // The license has at most 16,900 characters, and JSON writes each one in
  // at most 6 bytes.
  const MIB = 1024 * 1024;
  const IMAGE_FILE_BYTES = 8 * MIB;
  // IMAGE_MAX_SIDE and IMAGE_MAX_PIXELS limit the pixel size of a
  // background image: each side, and the width times the height. A small
  // compressed file can have a very large image, and the browser keeps 4
  // bytes for each decoded pixel. IMAGE_MAX_PIXELS is 64 Mi pixels, 256 MiB
  // decoded. It holds a 48 megapixel photo of 8064 by 6048 pixels.
  const IMAGE_MAX_SIDE = 16384;
  const IMAGE_MAX_PIXELS = 64 * 1024 * 1024;
  const SERVER_PROJECT_BYTES = 10 * 1024 * 1024;
  const EXPORT_ALLOWANCE = 1024;
  const ASSET_ALLOWANCE = 128 * 1024;
  const PROJECT_FILE_BYTES = Math.ceil((dataURLBytes(IMAGE_FILE_BYTES) + SERVER_PROJECT_BYTES + EXPORT_ALLOWANCE + ASSET_ALLOWANCE) / MIB) * MIB;
  // SERVER_COMMAND_BYTES mirrors session.MaxCommandBytes, the largest body
  // of a command request. SERVER_COMMAND_JSON_BYTES mirrors
  // session.MaxInflatedCommandBytes, the largest command JSON in a gzip
  // body. A Go test in internal/session checks the mirrors. postCommand
  // compresses command JSON of more than GZIP_COMMAND_BYTES. A small
  // command, such as a pause, stays plain JSON, because the server applies
  // one gzip command at a time. The compression makes a project command
  // about 6 times smaller, so a slow link sends it in less time.
  const SERVER_COMMAND_BYTES = 4 * MIB;
  // GEO_PROJECTION, GEO_RADIUS and GEO_MAX_LATITUDE mirror
  // project.GeoProjection, project.GeoRadius and project.MaxGeoLatitude,
  // the limits of the geo reference of a project. MAX_COORDINATE mirrors
  // project.MaxCoordinate, the largest absolute value in meters of each
  // coordinate of a node. A Go test in internal/project checks the
  // mirrors.
  const GEO_PROJECTION = "equirectangular";
  const GEO_RADIUS = 6371000;
  const GEO_MAX_LATITUDE = 80;
  const MAX_COORDINATE = 100000;
  const SERVER_COMMAND_JSON_BYTES = SERVER_PROJECT_BYTES + 64 * 1024;
  const GZIP_COMMAND_BYTES = 64 * 1024;

  function clone(value) {
    return JSON.parse(JSON.stringify(value));
  }

  const frozenDrafts = new WeakSet();

  // ownDraft copies external mutable data and reuses equal history branches.
  function ownDraft(value, previous) {
    if (Object.is(value, previous)) return previous;
    if (!value || typeof value !== "object") return value;
    if (frozenDrafts.has(value)) return value;
    const out = Array.isArray(value) ? new Array(value.length) : {};
    const keys = Object.keys(value);
    for (const key of keys) Object.defineProperty(out, key, { value: ownDraft(value[key], previous?.[key]), enumerable: true, writable: true, configurable: true });
    if (previous && Array.isArray(previous) === Array.isArray(value) && (!Array.isArray(value) || previous.length === value.length) && keys.length === Object.keys(previous).length && keys.every((key) => Object.hasOwn(previous, key) && Object.is(out[key], previous[key]))) return previous;
    frozenDrafts.add(out); return Object.freeze(out);
  }

  function sameDraft(a, b) {
    if (Object.is(a, b)) return true;
    if (!a || !b || typeof a !== "object" || typeof b !== "object" || Array.isArray(a) !== Array.isArray(b) || (Array.isArray(a) && a.length !== b.length)) return false;
    const keys = Object.keys(a);
    return keys.length === Object.keys(b).length && keys.every((key) => Object.hasOwn(b, key) && sameDraft(a[key], b[key]));
  }

  // emptyConfig initializes each setting before the Go worker starts.
  function emptyConfig() {
    return {
      version: 1,
      name: "Untitled scenario",
      network: { Nodes: [], Lanes: [], Stations: [] },
      fleet: [],
      demand: { enabled: false, perMinute: 2, pattern: "balanced", destination: "", profile: "", band: "", seed: 1 },
      demandProfiles: [],
      sharedRidePartyLimit: 1,
      sharedRideMode: "drop-offs",
      sharedRideJoin: "unassigned",
      sharedRideMaxStops: 3,
      platoonLimit: 0,
      stationBuffers: false,
      pickupReassignment: false,
      redistribution: false,
    };
  }

  // fallbackConfig gives the local draft that the editor opens when the live
  // scenario cannot load. It has two stations with one lane in each
  // direction and no pods.
  function fallbackConfig() {
    const config = emptyConfig();
    config.network = {
      Nodes: [
        {"ID":"station-1-entry-1","Position":{"X":64,"Y":120}},
        {"ID":"station-1-exit-1","Position":{"X":136,"Y":120}},
        {"ID":"station-1-berth-node-1","Position":{"X":100,"Y":150}},
        {"ID":"station-2-entry-1","Position":{"X":304,"Y":120}},
        {"ID":"station-2-exit-1","Position":{"X":376,"Y":120}},
        {"ID":"station-2-berth-node-1","Position":{"X":340,"Y":150}},
      ],
      Lanes: [
        {"ID":"lane-1","From":"station-1-entry-1","To":"station-1-berth-node-1","SpeedLimit":12,"StationID":"station-1","StationRole":"berth-access"},
        {"ID":"lane-2","From":"station-1-berth-node-1","To":"station-1-exit-1","SpeedLimit":12,"StationID":"station-1","StationRole":"departure"},
        {"ID":"lane-3","From":"station-1-entry-1","To":"station-1-exit-1","SpeedLimit":12,"StationID":"station-1","StationRole":"through"},
        {"ID":"lane-4","From":"station-2-entry-1","To":"station-2-berth-node-1","SpeedLimit":12,"StationID":"station-2","StationRole":"berth-access"},
        {"ID":"lane-5","From":"station-2-berth-node-1","To":"station-2-exit-1","SpeedLimit":12,"StationID":"station-2","StationRole":"departure"},
        {"ID":"lane-6","From":"station-2-entry-1","To":"station-2-exit-1","SpeedLimit":12,"StationID":"station-2","StationRole":"through"},
        {"ID":"lane-7","From":"station-1-exit-1","To":"station-2-entry-1","SpeedLimit":12},
        {"ID":"lane-8","From":"station-2-exit-1","To":"station-1-entry-1","SpeedLimit":12},
      ],
      Stations: [
        {"ID":"station-1","Name":"Origin","Entry":"station-1-entry-1","Exit":"station-1-exit-1","Berths":[{"ID":"station-1-berth-1","Node":"station-1-berth-node-1"}],"ParkingOnly":false},
        {"ID":"station-2","Name":"Destination","Entry":"station-2-entry-1","Exit":"station-2-exit-1","Berths":[{"ID":"station-2-berth-1","Node":"station-2-berth-node-1"}],"ParkingOnly":false},
      ],
    };
    return config;
  }

  function point(config, nodeID) {
    const node = config.network.Nodes.find((item) => item && item.ID === nodeID);
    return node && node.Position;
  }

  // stationAxes gives the axes of a station from the positions of its entry
  // and exit. origin is the station center, the middle of the entry-exit
  // line. along is the unit vector from the entry to the exit. across is the
  // unit vector 90 degrees clockwise from along. The map Y axis points down,
  // so across points to the right of the direction of travel. When the
  // entry and the exit are at the same point, along points right.
  function stationAxes(entry, exit) {
    const dx = exit.X - entry.X; const dy = exit.Y - entry.Y; const length = Math.hypot(dx, dy);
    const along = length ? { X: dx / length, Y: dy / length } : { X: 1, Y: 0 };
    return { origin: { X: (entry.X + exit.X) / 2, Y: (entry.Y + exit.Y) / 2 }, along, across: { X: -along.Y, Y: along.X } };
  }

  // stationBearing gives the bearing of a station in degrees. The bearing is
  // the direction from the entry to the exit, clockwise from up on the map.
  // 0 is up, 90 is right, 180 is down, and 270 is left. The value is at
  // least 0 and less than 360.
  function stationBearing(entry, exit) {
    const { along } = stationAxes(entry, exit);
    return (Math.atan2(along.X, -along.Y) * 180 / Math.PI + 360) % 360;
  }

  // stationShape gives the drawn shape of a station. The shape is the
  // smallest rectangle along the station axes that holds the entry, the
  // exit, and shape.points, with STATION_PADDING meters on each side.
  // shape.points holds the positions of the other station nodes. center is
  // the center of the rectangle. width is its size along the entry-exit
  // line, and height is its size across that line. angle is the rotation of
  // the rectangle in degrees, clockwise from right, as SVG uses it. top is
  // the Y value of the highest corner.
  function stationShape(shape) {
    const { origin, along, across } = stationAxes(shape.entry, shape.exit);
    const low = { along: Infinity, across: Infinity }; const high = { along: -Infinity, across: -Infinity };
    for (const at of [shape.entry, shape.exit, ...shape.points]) {
      const x = at.X - origin.X; const y = at.Y - origin.Y;
      const offset = { along: x * along.X + y * along.Y, across: x * across.X + y * across.Y };
      for (const axis of ["along", "across"]) { low[axis] = Math.min(low[axis], offset[axis]); high[axis] = Math.max(high[axis], offset[axis]); }
    }
    const middle = { along: (low.along + high.along) / 2, across: (low.across + high.across) / 2 };
    const width = high.along - low.along + 2 * STATION_PADDING; const height = high.across - low.across + 2 * STATION_PADDING;
    const center = { X: origin.X + along.X * middle.along + across.X * middle.across, Y: origin.Y + along.Y * middle.along + across.Y * middle.across };
    const top = center.Y - Math.abs(along.Y) * width / 2 - Math.abs(across.Y) * height / 2;
    return { center, width, height, angle: Math.atan2(along.Y, along.X) * 180 / Math.PI, top };
  }

  // berthChain gives the berth rows of a berth chain station, in berth
  // order, or null when the station is not a berth chain. The generated
  // London and scale100 stations are berth chains. In a chain, each berth
  // node has one lane in, from its arrival node, and one lane out, to its
  // departure node. An arrival link goes from the arrival node of the
  // previous row, or from the station entry for the first row, to the
  // arrival node. A departure link goes from the departure node to the
  // departure node of the previous row, or to the station exit. The arrival
  // and departure nodes are not entry, exit or berth nodes. A station that
  // addStation makes is not a chain, because its berth lanes go directly to
  // the entry and the exit. Each row has berth, the berth, arrival and
  // departure, the node IDs, and arrivalLink, departureLink, inLane and
  // outLane, the lanes.
  function berthChain(config, station) {
    const core = stationCoreNodeIDs(station);
    const byFrom = new Map(); const byTo = new Map();
    for (const lane of config.network.Lanes) {
      if (!byFrom.has(lane.From)) byFrom.set(lane.From, []);
      if (!byTo.has(lane.To)) byTo.set(lane.To, []);
      byFrom.get(lane.From).push(lane); byTo.get(lane.To).push(lane);
    }
    const link = (from, to) => (byFrom.get(from) || []).find((lane) => lane.To === to);
    const rows = [];
    let arrival = station.Entry; let departure = station.Exit;
    for (const berth of station.Berths || []) {
      const ins = byTo.get(berth.Node) || []; const outs = byFrom.get(berth.Node) || [];
      if (ins.length !== 1 || outs.length !== 1) return null;
      const row = { berth, arrival: ins[0].From, departure: outs[0].To, inLane: ins[0], outLane: outs[0] };
      if (row.arrival === row.departure || core.has(row.arrival) || core.has(row.departure)) return null;
      row.arrivalLink = link(arrival, row.arrival); row.departureLink = link(row.departure, departure);
      if (!row.arrivalLink || !row.departureLink) return null;
      rows.push(row);
      arrival = row.arrival; departure = row.departure;
    }
    return rows.length ? rows : null;
  }

  // Station dimensions come from existing coordinates. Only aligned, straight
  // berth chains support these controls. Other layouts keep manual node edits.
  function stationLayout(config, stationID, bankID = "") {
    let station = config.network.Stations.find((item) => item.ID === stationID);
    if (station && Object.hasOwn(station, "Banks")) {
      const bank = station.Banks?.find((item) => item.ID === bankID);
      if (!bank) return { error: "Select a station bank." };
      station = bankStation(station, bank);
    }
    const rows = station && berthChain(config, station);
    const unsupported = (reason) => ({ error: reason, approachLength: bankID && station ? bankAccessLength(config, station, "entry") : null, departureLength: bankID && station ? bankAccessLength(config, station, "exit") : null });
    if (!rows) return unsupported("Layout controls require a straight berth chain.");
    const nodes = new Map(config.network.Nodes.map((node) => [node.ID, node.Position]));
    const entry = nodes.get(station.Entry); const exit = nodes.get(station.Exit);
    if (!entry || !exit) return unsupported("The station entry or exit is missing.");
    const frame = stationAxes(entry, exit); const spacing = Math.hypot(exit.X - entry.X, exit.Y - entry.Y);
    if (spacing < 2 * MIN_LANE_LENGTH) return unsupported("Entry/exit spacing must be at least 48 m.");
    const offset = (id, axis) => { const at = nodes.get(id); return at && (at.X - frame.origin.X) * frame[axis].X + (at.Y - frame.origin.Y) * frame[axis].Y; };
    const near = (a, b) => Number.isFinite(a) && Number.isFinite(b) && Math.abs(a - b) < 1e-6;
    const body = new Set([station.Entry, station.Exit]); const rowNodes = new Set(); const rowLanes = new Set();
    for (const row of rows) {
      for (const id of [row.arrival, row.berth.Node, row.departure]) {
        if (body.has(id)) return unsupported("Berth rows must use distinct nodes.");
        body.add(id); rowNodes.add(id);
      }
      for (const lane of [row.arrivalLink, row.departureLink, row.inLane, row.outLane]) rowLanes.add(lane.ID);
    }
    if (config.network.Stations.some((other) => other.ID !== stationID && [...stationCoreNodeIDs(other)].some((id) => body.has(id)))) return unsupported("Another station shares these nodes.");
    for (const lane of config.network.Lanes) {
      if (!body.has(lane.From) && !body.has(lane.To)) continue;
      if (lane.Control) return unsupported(`Curved lane ${lane.ID} requires manual node edits.`);
      if ((rowNodes.has(lane.From) || rowNodes.has(lane.To)) && (!rowLanes.has(lane.ID) || lane.StationID !== stationID)) return unsupported(`Lane ${lane.ID} shares a berth row node.`);
    }
    const firstDepth = offset(rows[0].berth.Node, "across"); const side = Math.sign(firstDepth);
    if (!side) return unsupported("Berth rows must lie on one side of the station mouth.");
    const depths = rows.map((row) => side * offset(row.berth.Node, "across"));
    const pitch = rows.length > 1 ? depths[1] - depths[0] : null;
    for (const [index, row] of rows.entries()) {
      if (!near(offset(row.arrival, "along"), -spacing / 2) || !near(offset(row.departure, "along"), spacing / 2) || !near(offset(row.berth.Node, "along"), 0) ||
          !near(offset(row.arrival, "across"), side * depths[index]) || !near(offset(row.departure, "across"), side * depths[index]) || depths[index] <= 0 ||
          (pitch !== null && (pitch < 25 - 1e-6 || !near(depths[index], depths[0] + index * pitch)))) return unsupported("Berth rows must form an aligned rectangular chain with uniform pitch of at least 25 m.");
    }
    const incoming = config.network.Lanes.filter((lane) => lane.To === station.Entry && lane.StationID === stationID && lane.StationRole === "entry");
    const outgoing = config.network.Lanes.filter((lane) => lane.From === station.Exit && lane.StationID === stationID && lane.StationRole === "exit");
    let setback = null;
    if (incoming.length === 1 && outgoing.length === 1 && !body.has(incoming[0].From) && !body.has(outgoing[0].To) &&
        near(offset(incoming[0].From, "across"), offset(outgoing[0].To, "across")) && near(offset(incoming[0].From, "along") + offset(outgoing[0].To, "along"), 0)) {
      const depth = -side * offset(incoming[0].From, "across");
      if (depth > 0) setback = depth;
    }
    return { station, rows, frame, side, body, pitch, spacing, setback, error: "",
      approachLength: bankID ? bankAccessLength(config, station, "entry") : null,
      departureLength: bankID ? bankAccessLength(config, station, "exit") : null };
  }

  function selectedBank(station, selection) {
    return station.Banks?.find((bank) => bank.ID === selection?.bank) || station.Banks?.find((bank) => bank.BerthIDs.includes(selection?.berth)) || station.Banks?.[0];
  }

  function bankStation(station, bank) {
    const ids = new Set(bank.BerthIDs);
    return { ...station, Banks: undefined, Entry: bank.Entry, Exit: bank.Exit, Berths: station.Berths.filter((berth) => ids.has(berth.ID)) };
  }

  function bankAccessLength(config, station, role) {
    const gate = role === "entry" ? station.Entry : station.Exit;
    const lanes = config.network.Lanes.filter((lane) => lane.StationID === station.ID && lane.StationRole === role && (role === "entry" ? lane.To === gate : lane.From === gate));
    if (lanes.length !== 1 || lanes[0].Control) return null;
    const anchor = role === "entry" ? lanes[0].From : lanes[0].To;
    const incident = config.network.Lanes.filter((lane) => lane.From === anchor || lane.To === anchor);
    if (incident.length !== 2 || incident.some((lane) => lane.Control) || config.network.Stations.some((other) => stationCoreNodeIDs(other).has(anchor))) return null;
    const otherRole = role === "entry" ? "approach" : "exit";
    if (incident.some((lane) => lane.ID !== lanes[0].ID && (lane.StationID && (lane.StationID !== station.ID || lane.StationRole !== otherRole) || lane.From === gate || lane.To === gate))) return null;
    const length = laneLength(config, lanes[0]);
    return length > 0 ? length : null;
  }

  function stationGeometryCommand(config, id, action, value, bankID = "") {
    const station = config.network.Stations.find((item) => item.ID === id);
    if (Object.hasOwn(station || {}, "Banks") && ["stationLayout", "addBerth"].includes(action)) {
      if (!station.Banks?.some((bank) => bank.ID === bankID)) throw new Error("Select a station bank.");
      return action === "addBerth" ? { action: "addBankBerth", id, value: bankID } : { action: "bankLayout", id, value: { bank: bankID, ...value } };
    }
    return value === undefined ? { action, id } : { action, id, value };
  }

  function stationCoreNodeIDs(station) {
    return new Set([station.Entry, station.Exit, ...(station.Banks || []).flatMap((bank) => [bank.Entry, bank.Exit]), ...(station.Berths || []).map((berth) => berth.Node)]);
  }

  // stationNodeOwners maps each station node to its station ID. A station has
  // its entry, exit, and berth nodes. It also has each node that only its
  // station lanes use, such as a node of a berth chain. A node that a road lane
  // or a lane of a different station also uses stays a junction.
  function stationNodeOwners(config) {
    const stationIDs = new Set(config.network.Stations.map((station) => station.ID));
    const owners = new Map();
    for (const lane of config.network.Lanes) {
      const stationID = stationIDs.has(lane.StationID) ? lane.StationID : "";
      for (const id of [lane.From, lane.To]) owners.set(id, owners.has(id) && owners.get(id) !== stationID ? "" : stationID);
    }
    for (const [id, stationID] of owners) if (!stationID) owners.delete(id);
    for (const station of config.network.Stations) for (const id of stationCoreNodeIDs(station)) owners.set(id, station.ID);
    return owners;
  }

  // stationNodeIDs gives the nodes of one station, as stationNodeOwners finds
  // them. A station drag or delete then also includes its berth chains.
  function stationNodeIDs(config, station) {
    const ids = stationCoreNodeIDs(station);
    for (const [id, stationID] of stationNodeOwners(config)) if (stationID === station.ID) ids.add(id);
    return ids;
  }

  // shiftNodes moves a set of nodes by an offset, in place. It also moves the
  // curve control point of each lane between two nodes of the set.
  function shiftNodes(config, shift) {
    for (const node of config.network.Nodes) {
      if (shift.ids.has(node.ID)) {
        node.Position.X += shift.dx;
        node.Position.Y += shift.dy;
      }
    }
    for (const lane of config.network.Lanes) {
      if (lane.Control && shift.ids.has(lane.From) && shift.ids.has(lane.To)) {
        lane.Control.X += shift.dx;
        lane.Control.Y += shift.dy;
      }
    }
  }

  // dragTargets gives the items that a drag moves. A station drag moves the
  // station nodes, and a node drag moves one node, also a station node. The
  // targets are these nodes, the lanes that touch them, and the stations
  // that the nodes are part of, because the station shape holds all of its
  // station nodes. A control drag moves the curve of one lane. The map
  // redraws only the targets during a drag.
  function dragTargets(config, drag) {
    const moved = new Set();
    if (drag.type === "station") {
      const station = config.network.Stations.find((item) => item.ID === drag.id);
      if (station) for (const id of stationNodeIDs(config, station)) moved.add(id);
    } else if (drag.type === "node") moved.add(drag.id);
    const owners = moved.size ? stationNodeOwners(config) : new Map();
    const stations = new Set([...moved].map((id) => owners.get(id)));
    return {
      nodeIDs: config.network.Nodes.filter((node) => moved.has(node.ID)).map((node) => node.ID),
      laneIDs: config.network.Lanes.filter((lane) => (drag.type === "control" && lane.ID === drag.id) || moved.has(lane.From) || moved.has(lane.To)).map((lane) => lane.ID),
      stationIDs: config.network.Stations.filter((station) => stations.has(station.ID)).map((station) => station.ID),
    };
  }

  // flowNamesStation reports whether a demand profile flow starts or ends at
  // the station.
  function flowNamesStation(flow, stationID) {
    return Boolean(flow) && (flow.from === stationID || flow.to === stationID);
  }

  // stationFlowCount gives the number of demand profile flows that start or
  // end at the station, in all profiles. deleteStation removes these flows.
  function stationFlowCount(config, stationID) {
    let count = 0;
    for (const profile of config.demandProfiles || []) {
      if (profile && Array.isArray(profile.flows)) count += profile.flows.filter((flow) => flowNamesStation(flow, stationID)).length;
    }
    return count;
  }

  function stationRailReferences(config, stationID) {
    return (config.railArrivals || []).reduce((count, arrival) => count + (arrival.station === stationID ? 1 : arrival.destinations.filter((destination) => destination.station === stationID).length), 0) + (config.railDepartures || []).reduce((count, departure) => count + (departure.station === stationID ? 1 : departure.origins.filter((origin) => origin.station === stationID).length), 0);
  }

  // fleetRows gives one Fleet row for each station, in station order: the
  // station ID and name, the number of initial pods at the station, and the
  // number of berths, which is the limit for the pod count.
  function fleetRows(config) {
    return config.network.Stations.map((station) => ({
      id: station.ID, name: station.Name, max: station.Berths.length,
      count: config.fleet.filter((pod) => pod.StationID === station.ID).length,
    }));
  }

  function fleetClassNotice(config) {
    return config.version === 3 ? "New pods use legacy class. Import a project to set vehicle classes and express services. Group and express pods cannot start yet." : "";
  }

  // selectionCard gives the data of the Selection panel for the selected
  // station, lane or junction. A station gives its name, its bearing in
  // whole degrees, the parking option, and its berths. The bearing is 0
  // when the entry or the exit node is missing. The card marks the berth
  // that the selection names. It lets you remove a berth only when the
  // station has two or more berths. A lane gives its end nodes, its speed
  // limit in whole km/h, its length in meters, and if it has a curve. A
  // junction gives its position. The function gives null when there is no
  // selection, or when the draft does not have the selected item.
  function selectionCard(config, selection) {
    if (!selection) return null;
    const find = (items) => items.find((item) => item.ID === selection.id);
    if (selection.type === "station") {
      const station = find(config.network.Stations);
      const entry = station && point(config, station.Entry); const exit = station && point(config, station.Exit);
      const bank = station && selectedBank(station, selection);
      return station ? {
        type: "station", id: station.ID, name: station.Name, bearing: entry && exit ? Math.round(stationBearing(entry, exit)) % 360 : 0,
        parkingOnly: Boolean(station.ParkingOnly), canRemove: station.Berths.length > 1,
        berths: station.Berths.filter((berth) => !station.Banks || bank?.BerthIDs.includes(berth.ID)).map((berth) => ({ id: berth.ID, selected: berth.ID === selection.berth })),
        ...(station.Banks ? { banks: station.Banks.map((bank) => ({ id: bank.ID })), bank: bank?.ID || "" } : {}),
      } : null;
    }
    if (selection.type === "lane") {
      const lane = find(config.network.Lanes);
      return lane ? { type: "lane", id: lane.ID, from: lane.From, to: lane.To, speed: Math.round(lane.SpeedLimit * 3.6), length: laneLength(config, lane), curved: Boolean(lane.Control) } : null;
    }
    const node = find(config.network.Nodes);
    return node ? { type: "node", id: node.ID, x: node.Position.X, y: node.Position.Y } : null;
  }

  // berthFocusID gives the berth row that gets the keyboard focus after a
  // berth remove from the keyboard. berthIDs holds the berths of the station
  // before the remove. The row is the next row after the removed berth, or
  // the previous row when the removed berth was the last row. The focus goes
  // to the Remove button of that row. With one berth left, the Remove button
  // is disabled, because selectionCard gives canRemove false. Then, and for
  // a berth that is not in berthIDs, the function gives an empty ID, and the
  // focus goes to Add physical berth.
  function berthFocusID(berthIDs, removedID) {
    const index = berthIDs.indexOf(removedID); const rest = berthIDs.filter((id) => id !== removedID);
    return index >= 0 && rest.length > 1 ? rest[Math.min(index, rest.length - 1)] : "";
  }

  // undoFocus gives the selection and the keyboard focus after an undo or a
  // redo. before and after are the drafts before and after the step, and
  // selection is the selection before the step. control is the Selection
  // panel button that had the focus, as its action and berth ID, or null
  // when the focus was not in the Selection panel. When after has the
  // selected item, the selection stays, and the focus is the same button.
  // When that button is Remove on a berth row, and the berth is gone or the
  // button is disabled, the focus moves as after a berth remove. It is the
  // Remove button of the row that berthFocusID gives, or Add physical berth.
  // When after does not have the item, the selection clears. When control
  // is not null, the focus is then "map", as after a delete. A null focus
  // does not move the focus.
  function undoFocus(step) {
    const { before, after, selection, control } = step; const card = selectionCard(after, selection);
    if (!card) return { selection: null, focus: control ? "map" : null };
    if (!control || control.action !== "remove-berth") return { selection, focus: control };
    const usable = (id) => card.canRemove && card.berths.some((berth) => berth.id === id);
    const berthIDs = selectionCard(before, selection)?.berths.map((berth) => berth.id) || [];
    const id = usable(control.id) ? control.id : berthFocusID(berthIDs, control.id);
    return { selection, focus: usable(id) ? { action: "remove-berth", id } : { action: "add-berth", id: "" } };
  }

  function laneLength(config, lane) {
    const start = point(config, lane.From);
    const end = point(config, lane.To);
    if (!start || !end) return 0;
    return curveLength({ from: start, to: end, control: lane.Control });
  }

  // curveLength gives the length of a lane from the positions of its nodes and
  // its optional control point. For a curve, it adds the lengths of 16 straight
  // parts of the curve.
  function curveLength(curve) {
    const { from, to, control } = curve;
    if (!control) return Math.hypot(to.X - from.X, to.Y - from.Y);
    let length = 0;
    let previous = from;
    for (let i = 1; i <= 16; i += 1) {
      const t = i / 16;
      const u = 1 - t;
      const current = {
        X: u * u * from.X + 2 * u * t * control.X + t * t * to.X,
        Y: u * u * from.Y + 2 * u * t * control.Y + t * t * to.Y,
      };
      length += Math.hypot(current.X - previous.X, current.Y - previous.Y);
      previous = current;
    }
    return length;
  }

  // checkSelector gives a function that gives the editor selection for a
  // check target of the scenario. A berth selects its station, and berth
  // marks the berth in the station panel. A node of a station selects the
  // station, as a click on the map does. The function gives null when the
  // scenario does not have the object. It finds the station nodes once, so
  // a long Checks list does not find them again for each item.
  function checkSelector(config) {
    const { Nodes, Lanes, Stations } = config.network;
    let owners = null;
    return (target) => {
      if (!target) return null;
      const has = (items) => items.some((item) => item && item.ID === target.id);
      if (target.type === "station" && has(Stations)) return { type: "station", id: target.id };
      if (target.type === "lane" && has(Lanes)) return { type: "lane", id: target.id };
      if (target.type === "berth") {
        const station = Stations.find((item) => item && Array.isArray(item.Berths) && has(item.Berths));
        return station ? { type: "station", id: station.ID, berth: target.id } : null;
      }
      if (target.type === "node" && has(Nodes)) {
        owners ??= stationNodeOwners(config);
        const stationID = owners.get(target.id);
        return stationID ? { type: "station", id: stationID } : { type: "node", id: target.id };
      }
      return null;
    };
  }

  // checkSelection gives the editor selection for one check target, as
  // checkSelector tells.
  function checkSelection(config, target) { return checkSelector(config)(target); }

  // selectionPoint gives the map point of a selected item: the position of a
  // junction, the middle of a lane, or the middle between the entry and
  // the exit of a station. It gives null when a node is missing.
  function selectionPoint(config, selection) {
    const find = (items, id) => items.find((item) => item && item.ID === id);
    const at = (id) => find(config.network.Nodes, id)?.Position || null;
    if (selection.type === "node") return at(selection.id);
    const lane = selection.type === "lane" ? find(config.network.Lanes, selection.id) : null;
    const station = selection.type === "station" ? find(config.network.Stations, selection.id) : null;
    const from = at(lane ? lane.From : station?.Entry); const to = at(lane ? lane.To : station?.Exit);
    if (!from || !to) return null;
    // The middle of a curved lane is the point of its curve at t = 0.5.
    const control = lane && lane.Control ? lane.Control : { X: (from.X + to.X) / 2, Y: (from.Y + to.Y) / 2 };
    return { X: (from.X + to.X) / 4 + control.X / 2, Y: (from.Y + to.Y) / 4 + control.Y / 2 };
  }

  // focusView gives the view that shows a point that a check selected. The
  // view does not change when the point is on the map, at least FIT_MARGIN / 2
  // pixels from its edge, and the scale shows junction labels. Otherwise
  // the view puts the point in the center of the map, at NODE_LABEL_SCALE or
  // at the current scale when it is larger.
  function focusView(focus) {
    const { view, point, size } = focus; const margin = FIT_MARGIN / 2;
    const x = view.x + point.X * view.scale; const y = view.y + point.Y * view.scale;
    const onMap = x >= margin && x <= size.width - margin && y >= margin && y <= size.height - margin;
    if (onMap && view.scale >= NODE_LABEL_SCALE) return view;
    const scale = Math.max(view.scale, NODE_LABEL_SCALE);
    return { scale, x: size.width / 2 - point.X * scale, y: size.height / 2 - point.Y * scale };
  }

  // problemCountText gives the text of the problem count beside the apply
  // button. Only errors are problems. The text is empty with no errors.
  function problemCountText(errorCount) {
    return errorCount ? `${errorCount} problem${errorCount === 1 ? "" : "s"}` : "";
  }

  // CHECK_DELAY is the time in milliseconds from the last draft change to the
  // checks. The checks take about 130 ms on the London project, so they do
  // not run after each change of a fast series.
  const CHECK_DELAY = 150;

  // createCheckTimer runs the checks after the draft changes. schedule
  // starts a wait of timer.delay milliseconds when the current task ends,
  // and the checks run when the wait ends. The wait thus starts after the
  // render of the change, which takes about 250 ms on the London project. A
  // schedule call during the wait starts the wait again, so a series of
  // changes gives one run, and runs do not stack. run runs the checks now,
  // cancels the wait, and gives the result of timer.run. timer.clock holds
  // setTimeout and clearTimeout, so a test can use a mock clock.
  function createCheckTimer(timer) {
    let waiting = null;
    const cancel = () => { if (waiting !== null) timer.clock.clearTimeout(waiting); waiting = null; };
    const fire = () => { waiting = null; (timer.scheduled || timer.run)(); };
    return {
      get waiting() { return waiting !== null; },
      schedule() { cancel(); waiting = timer.clock.setTimeout(() => { waiting = timer.clock.setTimeout(fire, timer.delay); }, 0); },
      run() { cancel(); return timer.run(); },
    };
  }

  // validationSummary gives the tone and the text of the Checks summary.
  // With no errors, the scenario is ready to apply, and the text gives the
  // number of warnings.
  function validationSummary(errors, warnings) {
    if (errors.length) return { tone: "bad", text: `${errors.length} problem${errors.length === 1 ? "" : "s"} must be fixed.` };
    if (warnings.length) return { tone: "warn", text: `The scenario is ready to apply. It has ${warnings.length} warning${warnings.length === 1 ? "" : "s"}.` };
    return { tone: "good", text: "The scenario is ready to apply." };
  }

  // checkFocusKey gives the link of the Checks list that gets the keyboard
  // focus after the checks run again. before and after hold the links
  // before and after the run, in list order. A link has text, its message,
  // and type and id, its check target. The key of a link is its text. The
  // text is unique in the list, because validateConfig and configWarnings
  // remove a repeated text. focused is the index in before of the link that
  // had the focus, or null when the focus was not in the list. Then the
  // function gives null, and the focus does not move.
  // The new link of an old link is the link of after with the same text.
  // Some texts change, for example the station counts of a cut-off station.
  // Thus when after does not have the text, the new link is a link with the
  // same target. A text that before does not have comes first. The
  // function gives the key of the new link of the focused link. When there
  // is none, it gives the new link of the nearest old link, the next old
  // link first. When no old link has a new link, it gives the link at the
  // same place, or the last link. With no links, it gives an empty key, and
  // the focus goes to the Checks heading.
  function checkFocusKey(focus) {
    const { before, focused, after } = focus;
    if (focused === null) return null;
    if (!after.length) return "";
    const oldTexts = new Set(before.map((link) => link.text));
    const texts = new Map(after.map((link) => [link.text, link]));
    const targets = new Map(); const target = (link) => `${link.type}\0${link.id}`;
    for (const link of after) { if (!targets.has(target(link))) targets.set(target(link), []); targets.get(target(link)).push(link); }
    const newLink = (link) => { const same = targets.get(target(link)) || []; return texts.get(link.text) || same.find((item) => !oldTexts.has(item.text)) || same[0]; };
    const nearest = [...before.keys()].sort((a, b) => Math.abs(a - focused) - Math.abs(b - focused) || b - a);
    for (const index of nearest) { const link = newLink(before[index]); if (link) return link.text; }
    return after[Math.min(focused, after.length - 1)].text;
  }

  // dataURLBytes gives the length of the longest data URL for a PNG or JPEG
  // image file of the given size.
  function dataURLBytes(bytes) {
    return "data:image/jpeg;base64,".length + Math.ceil(bytes / 3) * 4;
  }

  // serializeDocument gives the export file. It is compact JSON, as the
  // server project file is, so PROJECT_FILE_BYTES holds each export.
  // The file has the members of backgroundFields, so the image key of the
  // page does not go into the file. A background with an asset member, as
  // exportBackground gives it, also writes the frame state, the frame and
  // the license of the image in background.asset.
  function serializeDocument(config, background) {
    const document = { format: "podsim", version: 1, scenario: clone(config) };
    if (background && background.dataURL) {
      document.background = backgroundFields(background);
      if (background.asset) document.background.asset = { frameState: background.asset.frameState, frame: clone(background.asset.frame), license: clone(background.asset.license) };
    }
    return JSON.stringify(document);
  }

  // exportBackground gives the background of the export file for
  // background, a history entry, and image, its image descriptor. It makes
  // the data URL from the bytes in the same task, with no wait, so a later
  // change of the history cannot drop the image first. The asset member is
  // there when the image has a frame or a license. It gives null with no
  // background.
  function exportBackground(background, image) {
    if (!background || !image) return null;
    const { x, y, width, height, opacity } = background;
    const out = { dataURL: bytesToDataURL(image.bytes, image.mime), x, y, width, height, opacity };
    if (background.frameState !== "none" || image.license) out.asset = { frameState: background.frameState, frame: image.frame, license: image.license };
    return out;
  }

  // documentAsset gives the frame state, the frame and the license of the
  // asset member of an imported background. An absent asset is the state
  // "none" with no frame and no license. It throws an error for an asset
  // that assetError rejects. A missing alignment is not an error.
  function documentAsset(asset) {
    if (asset === undefined) return { frameState: "none", frame: null, license: null };
    if (asset === null || typeof asset !== "object" || Array.isArray(asset)) throw new Error("The background asset must be an object.");
    for (const key of Object.keys(asset)) if (!["frameState", "frame", "license"].includes(key)) throw new Error("The background asset has an unknown member.");
    const out = { frameState: asset.frameState, frame: asset.frame ?? null, license: asset.license ?? null };
    const error = assetError(out);
    if (error) throw new Error(error);
    return clone(out);
  }

  // unwrapDocument gets the scenario from an import file. A browser export has
  // a format field and wraps the scenario. A server project file, such as the
  // -project file or the scenario command output, is a bare scenario.
  function unwrapDocument(document) {
    const isObject = (value) => value !== null && typeof value === "object" && !Array.isArray(value);
    if (!isObject(document)) throw new Error("The file must contain a JSON object.");
    if ("format" in document) {
      if (document.format !== "podsim") throw new Error('The format field must be "podsim".');
      if (document.version !== 1) throw new Error("The version field must be 1.");
      if (!isObject(document.scenario)) throw new Error("The scenario field must be an object.");
      return { scenario: clone(document.scenario), background: document.background };
    }
    if (!("network" in document)) throw new Error("The file has no format field and no network field.");
    if (!isObject(document.network)) throw new Error("The network field must be an object.");
    if (![1, 2, 3].includes(document.version)) throw new Error("The version field must be 1, 2, or 3.");
    return { scenario: clone(document), background: null };
  }

  // parseDocument checks the text of an import file and gives the scenario
  // and the background fields. For a background, it also gives asset, the
  // frame state, the frame and the license of its asset member, as
  // documentAsset checks them.
  function parseDocument(text, { deferMetadata = false } = {}) {
    let document;
    try { document = JSON.parse(text); } catch (error) { throw new Error(`The file is not valid JSON. ${error.message}`); }
    const { scenario, background } = unwrapDocument(document);
    if (deferMetadata && background) {
      if (typeof background !== "object" || Array.isArray(background)) throw new Error("The background must be an object.");
      if (typeof background.dataURL !== "string" || !/^data:image\/(png|jpeg);base64,/.test(background.dataURL)) throw new Error("The background must be a PNG or JPEG data URL.");
      imageFacts(background.dataURL);
    }
    if (!deferMetadata) {
      if (typeof module !== "undefined" && module.exports) {
        const { hasServiceMetadata } = require("./editor-service-reference.cjs");
        if (hasServiceMetadata(scenario) && scenario.version !== 3) throw new Error("Vehicle and service fields require project version 3.");
      }
      const banked = (scenario.network?.Stations || []).filter((station) => station && Object.hasOwn(station, "Banks"));
      if (scenario.version === 1 && banked.length) throw new Error("Version 1 projects cannot contain station banks.");
      if (scenario.version === 2 && !banked.length) throw new Error("Version 2 projects need a banked station.");
      for (const station of banked) {
        if (!Array.isArray(station.Banks) || !station.Banks.length || station.Banks.length > 8) throw new Error("A station needs 1 to 8 banks.");
        for (const bank of station.Banks) if (!Array.isArray(bank?.BerthIDs) || !bank.BerthIDs.length || bank.BerthIDs.length > MAX_BERTHS) throw new Error("A bank needs 1 to 200 berth IDs.");
      }
      if (background) checkBackground(background);
      for (const pod of Array.isArray(scenario.fleet) ? scenario.fleet : []) {
        if (pod && !pod.BerthID) {
          const station = scenario.network?.Stations?.find((item) => item && item.ID === pod.StationID);
          pod.BerthID = station?.Berths?.[0]?.ID || "";
        }
      }
      if (scenario.demand && scenario.demand.pattern === "market") {
        scenario.demand.pattern = "destination";
        if (!scenario.demand.destination && scenario.network && Array.isArray(scenario.network.Stations)) {
          const market = scenario.network.Stations.find((station) => station && !station.ParkingOnly && station.ID === "market");
          const first = scenario.network.Stations.filter((station) => station && !station.ParkingOnly).at(-1);
          scenario.demand.destination = (market || first || {}).ID || "";
        }
      }
    }
    // Go checks the raw scenario before defaults can replace invalid values.
    const out = { scenario, background: background ? backgroundFields(background) : null };
    if (!deferMetadata && background) out.asset = documentAsset(background.asset);
    if (deferMetadata) out.metadata = { ...(background ? { placement: Object.fromEntries(["x", "y", "width", "height", "opacity"].map((key) => [key, background[key]])), ...(Object.hasOwn(background, "asset") ? { asset: background.asset } : {}) } : {}) };
    return out;
  }

  // checkBackground throws an error when item is not a valid background: a
  // PNG or JPEG data URL with image data that imageFacts accepts, a finite
  // position, a positive size, and an opacity from 0 to 1. The project
  // import and the restore of the stored background use it. It does not
  // decode the image, so parseDocument stays synchronous.
  function checkBackground(item) {
    if (item === null || typeof item !== "object" || Array.isArray(item)) throw new Error("The background must be an object.");
    if (typeof item.dataURL !== "string" || !/^data:image\/(png|jpeg);base64,/.test(item.dataURL)) throw new Error("The background must be a PNG or JPEG data URL.");
    checkPlacement(item);
    imageFacts(item.dataURL);
  }

  // checkPlacement throws an error when item does not have a finite
  // position, a positive size, and an opacity from 0 to 1.
  function checkPlacement(item) {
    for (const key of ["x", "y", "width", "height", "opacity"]) if (typeof item[key] !== "number" || !Number.isFinite(item[key])) throw new Error(`The background ${key} value is invalid.`);
    if (item.width <= 0 || item.height <= 0 || item.opacity < 0 || item.opacity > 1) throw new Error("The background dimensions or opacity are invalid.");
  }

  // IMAGE_KEY_PATTERN is the form of an image key: 128 random bits as 32
  // lower case hex digits. The key tells two images apart in the text of
  // the background keeper, also across tabs and reloads.
  const IMAGE_KEY_PATTERN = /^[0-9a-f]{32}$/;

  // newImageKey gives a new image key from random, an object with
  // getRandomValues, such as crypto.
  function newImageKey(random) {
    return [...random.getRandomValues(new Uint8Array(16))].map((byte) => byte.toString(16).padStart(2, "0")).join("");
  }

  // FRAME_STATES are the states of the frame of a background. "none" is an
  // image with no frame. "attached" is the intent to keep the placement on
  // the frame, and "detached" keeps the frame but not that intent.
  const FRAME_STATES = ["none", "attached", "detached"];

  // LICENSE_LIMITS gives the members of the license facts of an image, and
  // the largest number of characters of each one. Each member is a
  // string, and can be empty. licenseURL and copyrightURL are HTTPS URLs.
  // retrieved is an ISO 8601 time. method is the query that selected the
  // data, or "user supplied". notice is a text that the user must keep.
  const LICENSE_LIMITS = { source: 200, attribution: 500, license: 64, licenseURL: 2048, copyrightURL: 2048, retrieved: 40, method: 10000, notice: 2000 };

  // httpsURL tells if text is an HTTPS URL.
  function httpsURL(text) {
    try { return new URL(text).protocol === "https:"; } catch (_) { return false; }
  }

  // licenseError gives the error of license facts, or an empty text when
  // license is null or valid. A license has each member of LICENSE_LIMITS
  // and no other member.
  function licenseError(license) {
    if (license === null) return "";
    if (typeof license !== "object" || Array.isArray(license)) return "The license must be an object.";
    for (const key of Object.keys(license)) if (!(key in LICENSE_LIMITS)) return `The license has an unknown member ${JSON.stringify(key.slice(0, 40))}.`;
    for (const [key, limit] of Object.entries(LICENSE_LIMITS)) {
      const value = license[key];
      if (typeof value !== "string") return `The license ${key} must be text.`;
      if ([...value].length > limit) return `The license ${key} must have at most ${limit} characters.`;
      if ((key === "licenseURL" || key === "copyrightURL") && value && !httpsURL(value)) return `The license ${key} must be an HTTPS URL.`;
    }
    if (license.retrieved && !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(:\d{2}(\.\d+)?)?(Z|[+-]\d{2}:\d{2})$/.test(license.retrieved)) return "The license retrieved time must be an ISO 8601 time.";
    return "";
  }

  // assetError gives the error of the frame state, the frame and the
  // license of a background, or an empty text when they are valid. The
  // state "none" has no frame, and "attached" and "detached" have a frame
  // that frameError accepts. A missing alignment is not an error.
  function assetError(asset) {
    if (!FRAME_STATES.includes(asset.frameState)) return `The frame state must be ${FRAME_STATES.map((name) => `"${name}"`).join(", ")}.`;
    if (asset.frameState === "none" && asset.frame !== null) return 'A background with the frame state "none" cannot have a frame.';
    if (asset.frameState !== "none") {
      if (asset.frame === null) return `A background with the frame state "${asset.frameState}" needs a frame.`;
      const frame = frameError(asset.frame);
      if (frame) return frame;
    }
    return licenseError(asset.license);
  }

  // imageFacts checks the image data of dataURL, a PNG or JPEG data URL,
  // before the browser decodes it. It gives the file size in bytes, and the
  // width and the height in pixels from the PNG or JPEG header. It throws an
  // error when the base64 text is not valid, when the file is larger than
  // IMAGE_FILE_BYTES, when the data is not a PNG or a JPEG with a size in its
  // header, or when checkImageSize rejects the size. The data can be a PNG
  // or a JPEG for either media type, because the browser reads the data.
  // A header can be valid for data that the browser cannot decode. The
  // import and the restore therefore also decode the image, as
  // checkImageBytes does.
  function imageFacts(dataURL) {
    const base64 = dataURL.slice(dataURL.indexOf(",") + 1);
    if (base64.length % 4 !== 0 || !/^[A-Za-z0-9+/]*={0,2}$/.test(base64)) throw new Error("The background image data is not valid base64.");
    const bytes = (base64.length / 4) * 3 - (base64.endsWith("==") ? 2 : base64.endsWith("=") ? 1 : 0);
    if (bytes > IMAGE_FILE_BYTES) throw new Error(`The background image must be ${IMAGE_FILE_BYTES / MIB} MiB or smaller.`);
    return { bytes, ...headerFacts(binaryBytes(atob(base64))) };
  }

  // imageBytesFacts checks image bytes as imageFacts checks a data URL.
  // bytes is an ArrayBuffer. It gives the file size in bytes, the width and
  // the height in pixels, and mime, the media type of the signature.
  function imageBytesFacts(bytes) {
    if (!(bytes instanceof ArrayBuffer)) throw new Error("The background image data is not bytes.");
    if (bytes.byteLength > IMAGE_FILE_BYTES) throw new Error(`The background image must be ${IMAGE_FILE_BYTES / MIB} MiB or smaller.`);
    return { bytes: bytes.byteLength, ...headerFacts(new Uint8Array(bytes)) };
  }

  // headerFacts gives the width, the height and the media type in the
  // header of data, the bytes of a PNG or JPEG file as a Uint8Array. It
  // throws an error when data is not a PNG or a JPEG with a size in its
  // header, or when checkImageSize rejects the size.
  function headerFacts(data) {
    const starts = (signature) => signature.every((byte, index) => data[index] === byte);
    const png = starts([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]); const jpeg = !png && starts([0xff, 0xd8]);
    const size = png ? pngSize(data) : jpeg ? jpegSize(data) : null;
    if (!size) throw new Error("The background image is not a valid PNG or JPEG.");
    checkImageSize(size.width, size.height);
    return { width: size.width, height: size.height, mime: png ? "image/png" : "image/jpeg" };
  }

  // binaryBytes gives the bytes of a binary string, as atob gives it.
  function binaryBytes(text) {
    const bytes = new Uint8Array(text.length);
    for (let index = 0; index < text.length; index += 1) bytes[index] = text.charCodeAt(index);
    return bytes;
  }

  // dataURLToBytes gives the bytes of a base64 data URL as an ArrayBuffer.
  function dataURLToBytes(dataURL) {
    return binaryBytes(atob(dataURL.slice(dataURL.indexOf(",") + 1))).buffer;
  }

  // bytesToDataURL gives a base64 data URL of bytes, an ArrayBuffer, with
  // the media type mime. It converts the bytes in parts, because a spread
  // of a large array exceeds the stack.
  function bytesToDataURL(bytes, mime) {
    const view = new Uint8Array(bytes); const parts = [];
    for (let at = 0; at < view.length; at += 0x8000) parts.push(String.fromCharCode(...view.subarray(at, at + 0x8000)));
    return `data:${mime};base64,${btoa(parts.join(""))}`;
  }

  // checkImageSize throws an error when an image of width by height pixels
  // is empty or larger than IMAGE_MAX_SIDE or IMAGE_MAX_PIXELS.
  function checkImageSize(width, height) {
    if (!(width >= 1 && height >= 1)) throw new Error("The background image has no pixels.");
    if (width > IMAGE_MAX_SIDE || height > IMAGE_MAX_SIDE || width * height > IMAGE_MAX_PIXELS) {
      throw new Error(`The background image is ${width} by ${height} pixels. The limit is ${IMAGE_MAX_SIDE} pixels on each side and ${IMAGE_MAX_PIXELS} pixels in total.`);
    }
  }

  // pngSize gives the width and the height in the IHDR chunk of data, the
  // bytes of a PNG file as a Uint8Array, or null when data has no IHDR
  // chunk first.
  function pngSize(data) {
    if (data.length < 24 || String.fromCharCode(...data.subarray(12, 16)) !== "IHDR") return null;
    const word = (at) => ((data[at] << 24) | (data[at + 1] << 16) | (data[at + 2] << 8) | data[at + 3]) >>> 0;
    return { width: word(16), height: word(20) };
  }

  // jpegSize gives the width and the height in the first frame header of
  // data, the bytes of a JPEG file as a Uint8Array. It follows the
  // segment lengths from the start. It gives null when the data ends, or a
  // scan starts, before a frame header. The frame header markers are 0xC0
  // to 0xCF, but not 0xC4, 0xC8 and 0xCC.
  function jpegSize(data) {
    const half = (at) => (data[at] << 8) | data[at + 1];
    let at = 2;
    while (at + 4 <= data.length) {
      if (data[at] !== 0xff) return null;
      const marker = data[at + 1];
      if (marker === 0xff) { at += 1; continue; }
      if (marker === 0x01 || (marker >= 0xd0 && marker <= 0xd7)) { at += 2; continue; }
      if (marker === 0xd9 || marker === 0xda) return null;
      const length = half(at + 2);
      if (length < 2) return null;
      if (marker >= 0xc0 && marker <= 0xcf && ![0xc4, 0xc8, 0xcc].includes(marker)) return at + 9 <= data.length ? { width: half(at + 7), height: half(at + 5) } : null;
      at += 2 + length;
    }
    return null;
  }

  // checkDecodedSize throws an error when the decoded size of an image is
  // not the size of its header facts. The browser can turn a JPEG, as its
  // orientation tag tells, so the decoded size can have the width and the
  // height of the header in the other order.
  function checkDecodedSize(size, facts) {
    checkImageSize(size.width, size.height);
    const same = (size.width === facts.width && size.height === facts.height) || (size.width === facts.height && size.height === facts.width);
    if (!same) throw new Error("The decoded background image does not have the size in its header.");
  }

  // backgroundFields gives a copy of the background fields of item, without
  // other members. These are the fields of the export file.
  function backgroundFields(item) {
    return { dataURL: item.dataURL, x: item.x, y: item.y, width: item.width, height: item.height, opacity: item.opacity };
  }

  // FRAME_SOURCES are the projections of an image that the user gives with
  // its bounds. An "equirectangular" image is linear in longitude and
  // latitude. A "web-mercator" image is in EPSG:3857, north up, as the
  // OpenStreetMap tiles are. FRAME_MAX_LATITUDE is the largest absolute
  // latitude in degrees of a frame edge.
  const FRAME_SOURCES = ["equirectangular", "web-mercator"];
  const FRAME_MAX_LATITUDE = 80;
  // SCALE_TOLERANCE is the accuracy budget of a frame. The projection of
  // the project is exact north to south, and its east-west scale at
  // latitude lat is cos(lat0) / cos(lat) of the true scale. A frame edge
  // with a scale error of more than SCALE_TOLERANCE is not accepted.
  const SCALE_TOLERANCE = 0.005;
  // ALIGN_TOLERANCE is the largest difference in meters of each placement
  // value from the placement of the frame, for an aligned background.
  const ALIGN_TOLERANCE = 0.5;
  // ANCHOR_MIN_DISTANCE is the smallest distance in meters between the two
  // anchor nodes, and ANCHOR_MAX_RESIDUAL is the largest residual of the
  // second node as a part of that distance.
  const ANCHOR_MIN_DISTANCE = 100;
  const ANCHOR_MAX_RESIDUAL = 0.02;
  // RESAMPLE_MAX_SIDE is the largest side in pixels of a resampled image.
  const RESAMPLE_MAX_SIDE = 4096;
  const DEGREE = Math.PI / 180;

  // projectPoint gives the world position in meters of a latitude and a
  // longitude in degrees, with the projection of geo: x is R cos(lat0)
  // (lon - lon0) and y is -R (lat - lat0), with the angles in radians.
  function projectPoint(geo, latitude, longitude) {
    return { X: geo.radius * Math.cos(geo.latitude * DEGREE) * (longitude - geo.longitude) * DEGREE, Y: -geo.radius * (latitude - geo.latitude) * DEGREE };
  }

  // frameError gives the error of a frame record, or an empty text for a
  // valid frame. A frame has south, north, west and east in degrees, at
  // the outer edges of the edge pixels, and source, one of FRAME_SOURCES.
  // south is less than north, and both are at most FRAME_MAX_LATITUDE from
  // the equator. west is less than east, from -180 to 180, so the box does
  // not cross the antimeridian. A frame has no other members.
  function frameError(frame) {
    if (frame === null || typeof frame !== "object" || Array.isArray(frame)) return "The frame must be an object.";
    const keys = ["south", "north", "west", "east", "source"];
    if (Object.keys(frame).some((key) => !keys.includes(key))) return "The frame has an unknown member.";
    if (!FRAME_SOURCES.includes(frame.source)) return `The frame source must be ${FRAME_SOURCES.map((name) => `"${name}"`).join(" or ")}.`;
    if (!["south", "north", "west", "east"].every((key) => typeof frame[key] === "number" && Number.isFinite(frame[key]))) return "The frame bounds must be numbers.";
    if (Math.abs(frame.south) > FRAME_MAX_LATITUDE || Math.abs(frame.north) > FRAME_MAX_LATITUDE) return `The frame latitudes must be from -${FRAME_MAX_LATITUDE} to ${FRAME_MAX_LATITUDE} degrees.`;
    if (frame.south >= frame.north) return "The south edge of the frame must be south of the north edge.";
    if (frame.west < -180 || frame.east > 180) return "The frame longitudes must be from -180 to 180 degrees.";
    if (frame.west >= frame.east) return "The west edge of the frame must be west of the east edge. The frame cannot cross the antimeridian.";
    return "";
  }

  // framePlacement gives the placement of an image with frame in the
  // project projection of geo: the world position of the north-west
  // corner, and the width and the height in meters.
  function framePlacement(frame, geo) {
    const corner = projectPoint(geo, frame.north, frame.west); const far = projectPoint(geo, frame.south, frame.east);
    return { x: corner.X, y: corner.Y, width: far.X - corner.X, height: far.Y - corner.Y };
  }

  // frameAligned tells if the placement of background, with x, y, width
  // and height, is the placement of frame in geo, each value within
  // ALIGN_TOLERANCE meters. With no geo, a background is not aligned.
  function frameAligned(background, frame, geo) {
    if (!geo || !frame) return false;
    const place = framePlacement(frame, geo);
    return ["x", "y", "width", "height"].every((key) => Math.abs(background[key] - place[key]) <= ALIGN_TOLERANCE);
  }

  // resampleSize gives the pixel size of the resampled image of frame at
  // metersPerPixel meters for each pixel in geo. Each side is at least 1
  // pixel. It gives an error when the size is not a positive number or a
  // side is more than RESAMPLE_MAX_SIDE pixels.
  function resampleSize(frame, geo, metersPerPixel) {
    if (!(Number.isFinite(metersPerPixel) && metersPerPixel > 0)) return { width: 0, height: 0, error: "The meters for each pixel must be a positive number." };
    const place = framePlacement(frame, geo);
    const width = Math.max(1, Math.round(place.width / metersPerPixel)); const height = Math.max(1, Math.round(place.height / metersPerPixel));
    const error = width > RESAMPLE_MAX_SIDE || height > RESAMPLE_MAX_SIDE ? `The resampled image would be ${width} by ${height} pixels. The limit is ${RESAMPLE_MAX_SIDE} pixels on each side, so choose more meters for each pixel.` : "";
    return { width, height, error };
  }

  // mercatorY gives the Web Mercator y value of a latitude in degrees, on
  // a unit sphere. It increases to the north.
  function mercatorY(latitude) {
    return Math.log(Math.tan(Math.PI / 4 + latitude * DEGREE / 2));
  }

  // resampleRows gives the rows of a Web Mercator image with frame, for an
  // output image of height rows that is linear in latitude. sourceHeight is
  // the number of rows of the source image. Each item has latitude, the
  // latitude of the center of the output row, and source, the source row
  // coordinate of that latitude. A source row r has its center at r + 0.5,
  // so the draw of an output row copies a source rectangle of one row
  // from source - 0.5. Longitude is linear in both projections, so each
  // output row is a scaled copy of one source row.
  function resampleRows(frame, height, sourceHeight) {
    const top = mercatorY(frame.north); const span = top - mercatorY(frame.south);
    return Array.from({ length: height }, (_, row) => {
      const latitude = frame.north - (row + 0.5) * (frame.north - frame.south) / height;
      return { latitude, source: (top - mercatorY(latitude)) / span * sourceHeight };
    });
  }

  // networkBounds gives the box around the nodes and the background image. It
  // gives null when the map has nothing to show.
  function networkBounds(config, background) {
    const points = config.network.Nodes.map((node) => node.Position);
    if (background) points.push({ X: background.x, Y: background.y }, { X: background.x + background.width, Y: background.y + background.height });
    if (!points.length) return null;
    const xs = points.map((item) => item.X); const ys = points.map((item) => item.Y);
    return { minX: Math.min(...xs), minY: Math.min(...ys), maxX: Math.max(...xs), maxY: Math.max(...ys) };
  }

  // fitView gives the view that shows the bounds in the center of a map of
  // the given size. The scale has no lower limit, so a large network fits.
  function fitView(bounds, size) {
    if (!bounds) return { x: 0, y: 0, scale: 1 };
    const width = Math.max(80, bounds.maxX - bounds.minX); const height = Math.max(80, bounds.maxY - bounds.minY);
    const scale = Math.min(MAX_FIT_ZOOM, Math.max(1, size.width - FIT_MARGIN) / width, Math.max(1, size.height - FIT_MARGIN) / height);
    return { scale, x: size.width / 2 - ((bounds.minX + bounds.maxX) / 2) * scale, y: size.height / 2 - ((bounds.minY + bounds.maxY) / 2) * scale };
  }

  // zoomScale gives the scale after one zoom step. The lowest scale is
  // MIN_ZOOM, or the fit scale if the network needs a lower scale to fit. A
  // step out never increases the scale, also when the view is already below
  // that limit.
  function zoomScale(step) {
    const lowest = Math.min(MIN_ZOOM, step.fitScale, step.scale);
    return Math.min(MAX_ZOOM, Math.max(lowest, step.scale * step.factor));
  }

  // nodeLabelSize gives the font size in meters of the ID label of a junction,
  // or 0 when the label does not show. All labels show at NODE_LABEL_SCALE or
  // more. The label of the selected junction and of the start node of a new
  // guideway always shows. It is never smaller than NODE_LABEL_SIZE screen
  // pixels, so it stays readable when the map is zoomed out.
  function nodeLabelSize(label) {
    const selected = label.id === label.linkFrom || Boolean(label.selection && label.selection.type === "node" && label.selection.id === label.id);
    if (selected) return NODE_LABEL_SIZE / Math.min(1, label.scale);
    return label.scale >= NODE_LABEL_SCALE ? NODE_LABEL_SIZE : 0;
  }

  // pairedLaneIDs gives the IDs of the lanes that have a reverse lane, that is
  // a lane from the To node to the From node. The map draws these lanes with
  // an offset, so that both lanes of a pair show and each one can be selected.
  function pairedLaneIDs(config) {
    const key = (from, to) => `${from}\u0000${to}`;
    const directed = new Set(config.network.Lanes.map((lane) => key(lane.From, lane.To)));
    return new Set(config.network.Lanes.filter((lane) => lane.From !== lane.To && directed.has(key(lane.To, lane.From))).map((lane) => lane.ID));
  }

  // showsChevron reports whether a lane shows its direction chevron at a view
  // scale. The length is the length of the lane in meters, as curveLength
  // gives it. A lane that is shorter than CHEVRON_LANE_LENGTH screen pixels has
  // no chevron, so that the chevrons do not cover a dense map.
  function showsChevron(lane) {
    return lane.length * lane.scale >= CHEVRON_LANE_LENGTH;
  }

  // laneOffset gives the offset in meters of a drawn lane at a view scale. A
  // lane of a pair moves LANE_PAIR_OFFSET screen pixels at each scale. Other
  // lanes do not move.
  function laneOffset(lane) {
    return lane.paired ? LANE_PAIR_OFFSET / lane.scale : 0;
  }

  // laneCurve gives the drawn curve of a lane from the positions of its
  // nodes and its optional control point. The offset moves the curve that
  // number of meters to the right of the direction of travel, so the reverse
  // lane moves to the other side. Each end moves along the normal of the curve
  // at that end. The middle point, the point of the curve at t = 0.5, moves
  // along the normal of the line from end to end. The result has a control
  // point only for a curved lane.
  function laneCurve(lane) {
    const { from, to, control, offset = 0 } = lane;
    // normal gives the unit vector to the right of the direction from a to b.
    // The map Y axis points down. It gives null when a and b are at the same
    // point.
    const normal = (a, b) => { const dx = b.X - a.X; const dy = b.Y - a.Y; const length = Math.hypot(dx, dy); return length ? { X: -dy / length, Y: dx / length } : null; };
    const move = (at, by) => ({ X: at.X + by.X * offset, Y: at.Y + by.Y * offset });
    const chord = normal(from, to) || { X: 0, Y: 0 };
    const bend = control || { X: (from.X + to.X) / 2, Y: (from.Y + to.Y) / 2 };
    const middle = move({ X: (from.X + 2 * bend.X + to.X) / 4, Y: (from.Y + 2 * bend.Y + to.Y) / 4 }, chord);
    if (!control) return { from: move(from, chord), to: move(to, chord), middle };
    const start = move(from, normal(from, control) || chord); const end = move(to, normal(control, to) || chord);
    // This control point puts the moved curve through the moved middle point.
    return { from: start, control: { X: 2 * middle.X - (start.X + end.X) / 2, Y: 2 * middle.Y - (start.Y + end.Y) / 2 }, to: end, middle };
  }

  // lanePathData gives the SVG path data of a lane curve. The path has a
  // vertex at the middle point, where the direction chevron shows. A curved
  // lane is two quadratic curves that meet at the middle point.
  function lanePathData(curve) {
    const { from, to, control, middle } = curve;
    const at = (item) => `${item.X} ${item.Y}`;
    if (!control) return `M ${at(from)} L ${at(middle)} L ${at(to)}`;
    const half = (a, b) => ({ X: (a.X + b.X) / 2, Y: (a.Y + b.Y) / 2 });
    return `M ${at(from)} Q ${at(half(from, control))} ${at(middle)} Q ${at(half(control, to))} ${at(to)}`;
  }

  // A connection holds the values that the editor uses with the session API.
  // fetch sends one HTTP request, as window.fetch does. clientID and sequence
  // identify each command, and epoch is the session epoch. postCommand
  // increases sequence and keeps the epoch of the reply. onServerStart is
  // optional. readState gives it the server start ID of each live state
  // that it reads.

  // getJSON gets one JSON document. It throws the server error or the HTTP
  // status.
  async function getJSON(connection, url) {
    const response = await connection.fetch(url, { headers: { Accept: "application/json" } });
    let body = null; try { body = await response.json(); } catch (_) {}
    if (!response.ok || (body && (body.error || body.Error))) throw new Error((body && (body.error || body.Error)) || `HTTP ${response.status}`);
    return body;
  }

  // readState gets the live state from /api/state. It gives the server
  // start ID of the reply, or an empty string, to connection.onServerStart
  // when it is set. Each read of the live state uses readState, so the page
  // knows the server start ID of the latest read.
  async function readState(connection) {
    const live = await getJSON(connection, "/api/state");
    if (connection.onServerStart) connection.onServerStart((live && (live.serverStart || live.ServerStart)) || "");
    return live;
  }

  // mebibytes gives a size in MiB with at most two decimals. round is
  // Math.floor for a limit and Math.ceil for a size over a limit, so that
  // the two numbers are never equal.
  function mebibytes(bytes, round = Math.floor) {
    return `${round(bytes / MIB * 100) / 100} MiB`;
  }

  // jsonBytes gives the size of the JSON text of value in UTF-8 bytes.
  function jsonBytes(value) {
    return new TextEncoder().encode(JSON.stringify(value)).length;
  }

  // tooLargeError gives the error of a command that is larger than the
  // server accepts. It has status 413 and no errorCode, as the reply of
  // the server.
  function tooLargeError(message) {
    const error = new Error(message);
    error.status = 413; error.errorCode = ""; return error;
  }

  // SERVER_TOO_LARGE_TEXT is the message for a command that the server
  // refuses with HTTP 413.
  const SERVER_TOO_LARGE_TEXT = `The command is too large for the server. The server accepts at most ${mebibytes(SERVER_COMMAND_JSON_BYTES)} of JSON and ${mebibytes(SERVER_COMMAND_BYTES)} after compression.`;

  // commandRequest gives the body and the headers of a command request
  // for the JSON text. It compresses text of more than GZIP_COMMAND_BYTES
  // with gzip. It throws a tooLargeError before the request when the JSON
  // or the compressed body is larger than the server accepts.
  async function commandRequest(text) {
    const headers = { "Content-Type": "application/json", Accept: "application/json" };
    const json = new TextEncoder().encode(text);
    if (json.length <= GZIP_COMMAND_BYTES) return { body: text, headers };
    if (json.length > SERVER_COMMAND_JSON_BYTES) {
      throw tooLargeError(`The command has ${mebibytes(json.length, Math.ceil)} of JSON. The server accepts at most ${mebibytes(SERVER_COMMAND_JSON_BYTES)}.`);
    }
    const body = await new Response(new Blob([json]).stream().pipeThrough(new CompressionStream("gzip"))).arrayBuffer();
    if (body.byteLength > SERVER_COMMAND_BYTES) {
      throw tooLargeError(`The compressed command has ${mebibytes(body.byteLength, Math.ceil)}. The server accepts at most ${mebibytes(SERVER_COMMAND_BYTES)}.`);
    }
    return { body, headers: { ...headers, "Content-Encoding": "gzip" } };
  }

  // REPLY_TEXT_CHARS is the maximum number of characters of a plain text
  // error reply that the editor shows. REPLY_SCAN_CHARS is the maximum
  // number of UTF-16 units of the reply that replyText reads, so a large
  // reply does not use much memory.
  const REPLY_TEXT_CHARS = 200;
  const REPLY_SCAN_CHARS = 4096;

  // replyText gives the text of a plain text error reply on one line, with
  // at most REPLY_TEXT_CHARS characters and "..." when it is shorter than
  // the reply. When the text is empty, it gives the HTTP status, and for
  // HTTP 503 it also tells that the server is busy.
  function replyText(text, status) {
    const start = text.trimStart();
    const scan = start.slice(0, REPLY_SCAN_CHARS).replace(/\s+/g, " ").trimEnd();
    let out = ""; let count = 0;
    for (const char of scan) {
      if (count === REPLY_TEXT_CHARS) return `${out.trimEnd()}...`;
      out += char; count += 1;
    }
    if (start.length > REPLY_SCAN_CHARS) return `${out}...`;
    if (out) return out;
    return status === 503 ? "the server is busy (HTTP 503)" : `HTTP ${status}`;
  }

  // retryAfterSeconds gives the number of seconds in the Retry-After header
  // of a reply, or null when the header is not a number of seconds.
  function retryAfterSeconds(response) {
    const value = (response.headers && response.headers.get("Retry-After")) || "";
    return /^\d+$/.test(value.trim()) ? Number(value.trim()) : null;
  }

  // postCommand sends one command. A rejected command throws an error with
  // status, the HTTP status, and errorCode, the error code of the
  // acknowledgment. The message of the error is the error text of the
  // acknowledgment. A reply without an acknowledgment gives an empty
  // errorCode and the text of the reply as the message, as replyText
  // gives it. The error also has retryAfter, the seconds of the
  // Retry-After header or null. For example, the server replies with HTTP
  // 503, Retry-After and plain text when it is busy with other large
  // commands. HTTP 413 gives SERVER_TOO_LARGE_TEXT. A command that is too
  // large for the server throws a tooLargeError, and the editor does not
  // send it.
  async function postCommand(connection, command) {
    connection.sequence += 1;
    const { body: requestBody, headers } = await commandRequest(JSON.stringify({ client: connection.clientID, sequence: connection.sequence, epoch: connection.epoch, ...command }));
    const response = await connection.fetch("/api/command", { method: "POST", headers, body: requestBody });
    if (response.status === 413) throw tooLargeError(SERVER_TOO_LARGE_TEXT);
    let text = ""; try { text = await response.text(); } catch (_) {}
    let body = null; try { body = JSON.parse(text); } catch (_) {}
    if (!response.ok || (body && (body.error || body.Error))) {
      const error = new Error((body && (body.error || body.Error)) || replyText(text, response.status));
      error.status = response.status; error.errorCode = (body && body.errorCode) || ""; error.retryAfter = retryAfterSeconds(response); throw error;
    }
    const replyState = body && (body.state || body.State || body);
    if (replyState && (replyState.epoch || replyState.Epoch)) connection.epoch = replyState.epoch || replyState.Epoch;
    return body;
  }

  // simulationID identifies one simulation. A server restart gives a new
  // epoch. A reset, a demo, a rewind and a project apply give a new
  // generation.
  function simulationID(frame) {
    return `${frame.epoch || frame.Epoch || ""} ${frame.generation ?? frame.Generation ?? 0}`;
  }

  // SNAPSHOT_ATTEMPTS is the maximum number of read passes of readSnapshot.
  const SNAPSHOT_ATTEMPTS = 3;

  // snapshotConsistent tells if before, the first state read, project, the
  // project reply, and after, the second state read, show one server state.
  // Both state reads must have the same server start ID and epoch, and the
  // project revision must be the project revision of after.
  function snapshotConsistent(before, project, after) {
    const start = (state) => state.serverStart || state.ServerStart || "";
    const epoch = (state) => state.epoch || state.Epoch || "";
    const revision = project.revision ?? project.Revision;
    return start(before) === start(after) && epoch(before) === epoch(after) && revision !== undefined && Number(revision) === Number(after.projectRevision ?? after.ProjectRevision);
  }

  // draftBeforeRestart tells if a draft is from before a server restart.
  // draftStart is the server start ID of the state that the draft started
  // from, and liveStart is the server start ID of the live state. An empty
  // ID is not known, so then the function gives false. Pause and apply
  // uses the same check.
  function draftBeforeRestart(draftStart, liveStart) {
    return Boolean(draftStart && liveStart && draftStart !== liveStart);
  }

  // readSnapshot reads the live project and the live state as one
  // snapshot. It gives project, the project reply, and state, the state
  // that has the same server start ID, epoch and project revision. The
  // server gives the project and the state in two requests. A server
  // restart between them can restore a different project with the same
  // revision, so the old project could get the new server start ID. Thus
  // readSnapshot reads the state, the project, then the state again, and
  // accepts the reads only when snapshotConsistent is true. Else it reads
  // again, for a maximum of attempts passes, then throws an error.
  async function readSnapshot(connection, attempts = SNAPSHOT_ATTEMPTS) {
    for (let pass = 0; pass < attempts; pass += 1) {
      const before = await readState(connection);
      const project = await getJSON(connection, "/api/project");
      const state = await readState(connection);
      if (!project || !(project.project || project.Project)) throw new Error("The server returned no scenario.");
      if (before && state && snapshotConsistent(before, project, state)) return { project, state };
    }
    throw new Error("The live scenario changed during each read.");
  }

  // readLive gets the live project and the live state with readSnapshot. It
  // gives project, the normalized live project, revision, the live project
  // revision, epoch, the session epoch, and serverStart, the server start
  // ID or an empty string. All four are of the same server state. It does
  // not change the connection.
  async function readLive(connection, normalize) {
    const { project: projectReply, state: liveState } = await readSnapshot(connection);
    return {
      project: await normalize(projectReply.project || projectReply.Project),
      revision: Number(liveState.projectRevision ?? liveState.ProjectRevision),
      epoch: liveState.epoch || liveState.Epoch || "",
      serverStart: liveState.serverStart || liveState.ServerStart || "",
    };
  }

  // applyToServer applies a project to the live session. It gives revision,
  // the new project revision, stateSaved, the stateSaved member of the
  // reply, and serverStart, the server start ID that it read before the
  // pause, or an empty string. stateSaved is false when the server could
  // not save the session state before its reply, and undefined when the
  // reply does not have the member. The server applies a project only while
  // the simulation is paused, so applyToServer pauses the simulation first.
  // apply.revision is the live project revision that the draft started
  // from. apply.onApplying runs after the pause, when it is set.
  // apply.serverStart is the server start ID of the state that the draft
  // started from, or an empty string. The project command sends it when it
  // is not empty, so the server rejects a draft from before a server
  // restart.
  //
  // A failure error has status, the HTTP status of a rejected command,
  // errorCode, the error code of a rejected command, and pause, the state of
  // the simulation after the failure. Before the pause, the editor checks
  // that the JSON of the project has at most SERVER_PROJECT_BYTES. Else the
  // error is a tooLargeError. Then the editor reads the live project and
  // state with readSnapshot, and does the checks of the server in the same
  // order. When the live server start ID
  // is not apply.serverStart, the error has status 409 and errorCode
  // "session_changed". Else, when the live project revision is not
  // apply.revision, the error has status 409 and errorCode "stale_project".
  // The pause values are:
  // - "not-paused": the editor did not pause the simulation.
  // - "was-paused": the simulation was paused before the apply and stays paused.
  // - "resumed": the editor paused the simulation, then resumed it.
  // - "left-paused": the editor paused the simulation and could not resume it.
  // - "restarted": the simulation restarted after the pause. The editor did
  //   not resume it.
  async function applyToServer(apply) {
    const { connection } = apply;
    let step = "check";
    let wasPaused = false;
    let simulation = "";
    try {
      const projectBytes = jsonBytes(apply.project);
      if (projectBytes > SERVER_PROJECT_BYTES) {
        throw tooLargeError(`The scenario has ${mebibytes(projectBytes, Math.ceil)} of JSON. The server accepts at most ${mebibytes(SERVER_PROJECT_BYTES)}.`);
      }
      const { project: current, state: live } = await readSnapshot(connection);
      const liveStart = live.serverStart || live.ServerStart || "";
      if (draftBeforeRestart(apply.serverStart, liveStart)) {
        const error = new Error("The server session changed."); error.status = 409; error.errorCode = "session_changed"; throw error;
      }
      if (Number(current.revision ?? current.Revision ?? 0) !== apply.revision) {
        const error = new Error("The live scenario changed."); error.status = 409; error.errorCode = "stale_project"; throw error;
      }
      if (!connection.epoch) connection.epoch = live.epoch || live.Epoch || "";
      wasPaused = Boolean(live.simulation && live.simulation.Paused);
      simulation = simulationID(live);
      step = "pause";
      await postCommand(connection, { action: "pause", paused: true });
      step = "project";
      if (apply.onApplying) apply.onApplying();
      const reply = await postCommand(connection, { action: "project", projectRevision: apply.revision, project: apply.project, ...(apply.serverStart ? { serverStart: apply.serverStart } : {}) });
      const replyState = reply && (reply.state || reply.State || reply);
      const revision = Number(reply?.projectRevision ?? reply?.ProjectRevision ?? (replyState && (replyState.projectRevision ?? replyState.ProjectRevision)) ?? apply.revision + 1);
      return { revision, stateSaved: reply?.stateSaved, serverStart: liveStart };
    } catch (error) {
      error.pause = await pauseAfterFailure({ connection, error, step, wasPaused, simulation });
      throw error;
    }
  }

  // pauseAfterFailure gives the pause value of a failed apply. failure.step
  // is the step that failed: "check" before the pause command, "pause" for
  // the pause command, and "project" after it. The server did not apply a
  // command that it rejected with a 4xx status. A pause command without a
  // reply can be in effect, so the editor then resumes the simulation.
  async function pauseAfterFailure(failure) {
    if (failure.step === "check") return "not-paused";
    if (failure.wasPaused) return "was-paused";
    const rejected = failure.error.status >= 400 && failure.error.status < 500;
    if (failure.step === "pause" && rejected) return "not-paused";
    return resumeSimulation(failure.connection, failure.simulation);
  }

  // resumeSimulation resumes the simulation that the editor paused. It does
  // not resume a simulation that restarted after the pause. For example, a
  // project apply from another browser starts a new paused simulation. The
  // same occurs when the server applied this project and its reply was lost.
  async function resumeSimulation(connection, simulation) {
    try {
      const live = await readState(connection);
      if (simulationID(live) !== simulation) return "restarted";
      await postCommand(connection, { action: "pause", paused: false });
      return "resumed";
    } catch (_) { return "left-paused"; }
  }

  // APPLY_PAUSE_TEXT tells the state of the simulation after a failed apply.
  const APPLY_PAUSE_TEXT = {
    "not-paused": "The simulation was not paused.",
    "was-paused": "The simulation remains paused.",
    "resumed": "The editor resumed the simulation.",
    "left-paused": "The apply attempt paused the simulation and could not resume it.",
    "restarted": "The simulation restarted after the pause. The editor did not resume it.",
  };

  // APPLY_FAILURE gives the reason and the status line for the error codes
  // of a failed apply that need their own text. "stale_project" is a
  // conflict with the live project. "session_changed" is a server restart.
  // The page keeps the revision, the epoch and the server start ID that it
  // loaded, so each later apply also fails. After such a failure, the page
  // shows the apply conflict actions, as readConflict tells.
  const APPLY_FAILURE = new Map([
    ["stale_project", { reason: "The live scenario changed. Your draft is safe.", status: "Apply conflict. The live scenario changed." }],
    ["session_changed", { reason: "The server session changed. Your draft is safe.", status: "Apply conflict. The server session changed." }],
  ]);

  // APPLY_FAILED_STATUS is the status line for an error code that is not in
  // APPLY_FAILURE. The toast with the reason hides after 4 seconds. The
  // status line stays.
  const APPLY_FAILED_STATUS = "Apply failed. The draft stays on this page.";

  // retryText tells the user when to apply again after an HTTP 503 reply.
  // seconds is the Retry-After value of the reply, or null. The reason
  // before it tells that the server is busy.
  function retryText(seconds) {
    if (!seconds) return "Apply again later.";
    return `Wait ${seconds} ${seconds === 1 ? "second" : "seconds"}, then apply again.`;
  }

  // applyFailureText gives the message for an error from applyToServer. For
  // an error code that is not in APPLY_FAILURE, the reason is the error
  // text, for example the reason from the server or a network error. After
  // an HTTP 503 reply without an error code, retryText follows the
  // reason. note is empty when the browser keeps the draft. Else it tells
  // the user that the draft is lost when they leave the page.
  function applyFailureText(error, note = "") {
    const text = String(error.message).replace(/\.?$/, ".");
    const reason = APPLY_FAILURE.get(error.errorCode)?.reason ?? `Apply failed. ${text.charAt(0).toUpperCase()}${text.slice(1)}`;
    const retry = error.status === 503 && !error.errorCode ? retryText(error.retryAfter) : "";
    return [reason, retry, APPLY_PAUSE_TEXT[error.pause], note].filter(Boolean).join(" ");
  }

  // applyFailureStatus gives the status line for an error from
  // applyToServer. note is as applyFailureText uses it.
  function applyFailureStatus(error, note = "") {
    return [APPLY_FAILURE.get(error.errorCode)?.status ?? APPLY_FAILED_STATUS, note].filter(Boolean).join(" ");
  }

  // applyToast gives the arguments of toast for an applied project. applied
  // is the result of applyToServer. When the server could not save the
  // session state, the toast is a warning in the error style.
  function applyToast(applied) {
    if (applied.stateSaved === false) {
      return ["The project is applied, but the server could not save the session state. A server crash can undo this change.", true];
    }
    return ["The scenario was applied. The simulation remains paused.", false];
  }

  // readConflict gives the apply conflict that the page shows after error,
  // an error from applyToServer, or null. Only "stale_project" and
  // "session_changed" give a conflict. Then readConflict reads the live
  // state again. The conflict has code, the error code, revision, the live
  // project revision, and serverStart, the server start ID of the same
  // read. revision is null when the live state did not load. readConflict
  // does not change the epoch of the connection. Only the conflict actions
  // do, after the user agrees.
  async function readConflict(connection, error, normalize) {
    if (!APPLY_FAILURE.has(error.errorCode)) return null;
    try {
      const live = await readLive(connection, normalize);
      return { code: error.errorCode, revision: live.revision, serverStart: live.serverStart };
    } catch (_) { return { code: error.errorCode, revision: null, serverStart: "" }; }
  }

  // CONFLICT_UNLOADED_TEXT is the note of the status line when the live
  // state did not load after an apply conflict.
  const CONFLICT_UNLOADED_TEXT = "The live scenario did not load. Select Pause and apply to try again.";

  // conflictView gives the texts of the apply conflict panel for conflict,
  // as readConflict gives it with a revision: text, hint and applyLabel,
  // the label of the Apply over button.
  function conflictView(conflict) {
    const applyLabel = `Apply over revision ${conflict.revision}`;
    const reason = conflict.code === "session_changed" ? `The server session changed. The live scenario is now revision ${conflict.revision}.` : `The live scenario changed to revision ${conflict.revision}.`;
    return {
      text: `${reason} Your draft is not applied.`,
      hint: `Load live scenario replaces the changes in your draft. ${applyLabel} replaces the live scenario with your draft.`,
      applyLabel,
    };
  }

  // LOAD_LIVE_QUESTION asks the user before Load live scenario replaces
  // the changes in the draft.
  const LOAD_LIVE_QUESTION = "Load the live scenario? It replaces the changes in your draft.";

  // applyOverQuestion asks the user before the draft replaces live
  // revision revision.
  function applyOverQuestion(revision) {
    return `Apply your draft over live revision ${revision}? Your draft replaces the live scenario, and the shared simulation restarts paused.`;
  }

  // takeLive reads the live state for a conflict action. take.question is
  // the question for take.confirm, which asks the user as window.confirm
  // does. An empty question does not ask. takeLive gives null when the user
  // cancels. Else it gives the live state, as readLive gives it, and sets
  // the epoch of take.connection to the live epoch. Thus the next command
  // has the epoch of the current session, also after a server restart.
  async function takeLive(take) {
    if (take.question && !take.confirm(take.question)) return null;
    const live = await readLive(take.connection, take.normalize);
    take.connection.epoch = live.epoch;
    return live;
  }

  // loadLive starts Load live scenario. load.changed tells if the draft has
  // scenario changes. Only then loadLive asks the user, with load.confirm,
  // because the live scenario replaces them. It gives the live state, as
  // takeLive gives it, or null when the user cancels. The page then makes
  // the live project the draft and its base, as liveDraft gives.
  function loadLive(load) {
    return takeLive({ connection: load.connection, question: load.changed ? LOAD_LIVE_QUESTION : "", confirm: load.confirm, normalize: load.normalize });
  }

  // liveDraft gives the page values after Load live scenario. live is the
  // live state, as loadLive gives it. current has loaded, the loaded
  // project or null, and background, the background of the draft. The live
  // project becomes value, the new draft, and the scenario of loaded, the
  // base of Reset draft. Both keep their background, because the server
  // does not have it. loadedRevision, loadedStart and draftBase get the
  // live revision, epoch and server start ID.
  function liveDraft(live, current) {
    return {
      loadedRevision: live.revision, loadedStart: live.serverStart,
      draftBase: { revision: live.revision, epoch: live.epoch, serverStart: live.serverStart },
      loaded: { scenario: clone(live.project), background: current.loaded ? current.loaded.background : null },
      value: { scenario: clone(live.project), background: current.background },
    };
  }

  // applyOverBase starts Apply over revision N. over.revision is N and
  // over.serverStart is the server start ID of the conflict. applyOverBase
  // asks the user with over.confirm, then reads the live state. It gives
  // the base of the apply, or null when the user cancels. The base has
  // revision N and the server start ID of the conflict, and the epoch of
  // the live state. The page keeps the draft and applies it with this
  // base. When the live revision is not N any more, the apply fails with
  // "stale_project". When the server restarted again, it fails with
  // "session_changed". Then the page shows the new revision.
  async function applyOverBase(over) {
    const live = await takeLive({ connection: over.connection, question: applyOverQuestion(over.revision), confirm: over.confirm, normalize: over.normalize });
    return live && { revision: over.revision, epoch: live.epoch, serverStart: over.serverStart };
  }

  // DRAFT_SAVE_DELAY is the time in milliseconds from the last draft change
  // to the save of the draft in the browser.
  const DRAFT_SAVE_DELAY = 300;

  // DRAFT_STORE_TEXT tells the user if the browser keeps the draft, for each
  // status of createDraftKeeper.
  const DRAFT_STORE_TEXT = {
    ok: "This browser keeps the draft changes until you apply them.",
    off: "This browser cannot keep the draft. Export the draft before you leave the page.",
    failed: "This browser could not save the draft. Export the draft before you leave the page.",
    full: "The browser storage is full, so the draft is not saved. Export the draft before you leave the page.",
  };

  // DRAFT_UNSAVED_TEXT tells the user that the browser did not save the
  // last draft changes, for example while the saved draft offer shows.
  const DRAFT_UNSAVED_TEXT = "This browser did not save the last draft changes. Export the draft before you leave the page.";

  // DRAFT_DISPLACED_TEXT tells the user that another editor tab replaced
  // or deleted the saved draft, so the browser does not keep the draft of
  // this tab.
  const DRAFT_DISPLACED_TEXT = "Another editor tab replaced the saved draft. This browser saves the draft of this tab again after your next change.";

  // DRAFT_STORE and BACKGROUND_STORE name the IndexedDB database and the
  // object store of the saved draft and of the stored background. Each has
  // its own database at version 1. Thus a new store needs no version
  // upgrade, which an open editor tab of an older version can block.
  const DRAFT_STORE = { database: "podsim-editor", store: "drafts" };
  // The background database has the suffix -2, because its record holds
  // the image as bytes with an image key. The editor does not open the
  // older podsim-editor-backgrounds database, so an open tab of an older
  // editor cannot read or delete a record of this form.
  const BACKGROUND_STORE = { database: "podsim-editor-backgrounds-2", store: "backgrounds" };

  // openRecordStore gives a record store that keeps records in IndexedDB.
  // factory is the indexedDB object of the browser, and names is
  // DRAFT_STORE or BACKGROUND_STORE. get, put and delete give promises. A
  // write ends when its transaction completes, so a quota error at the
  // commit rejects the write. Without factory, the function gives null.
  // When the database cannot open, each call rejects.
  function openRecordStore(factory, names) {
    if (!factory) return null;
    let database = null;
    const open = () => database ??= new Promise((resolve, reject) => {
      const request = factory.open(names.database, 1);
      request.onupgradeneeded = () => request.result.createObjectStore(names.store);
      request.onsuccess = () => resolve(request.result);
      request.onerror = () => reject(request.error);
    });
    const run = async (mode, action) => {
      const db = await open();
      return new Promise((resolve, reject) => {
        const transaction = db.transaction(names.store, mode); const request = action(transaction.objectStore(names.store));
        transaction.oncomplete = () => resolve(request.result);
        transaction.onabort = () => reject(transaction.error || request.error || new Error("The IndexedDB transaction stopped."));
      });
    };
    return {
      get: (key) => run("readonly", (records) => records.get(key)),
      put: (key, record) => run("readwrite", (records) => records.put(record, key)),
      delete: (key) => run("readwrite", (records) => records.delete(key)),
    };
  }

  // createDraftKeeper saves the draft in a draft store, so that a reload or
  // a closed tab does not lose it. The page also uses it to keep the
  // background. keeper.store has get, put and delete, as openRecordStore
  // gives, or is null. keeper.key names the one record.
  // keeper.snapshot gives the record to keep, or null when the draft has no
  // changes. Then the keeper deletes the record. keeper.text, when given,
  // gives the text that the keeper compares for a record. The default is
  // the JSON text. schedule saves the draft keeper.delay milliseconds after
  // the last call, and keeper.clock holds setTimeout and clearTimeout.
  //
  // The queue holds at most one running write and one waiting write, and
  // the writes commit in the order of the calls. A newer write replaces
  // the waiting write, which has not started. The replaced write settles
  // at once with no store call: it does not set the stored text, change
  // the status or tell the other tabs. keeper.onQueued runs with the record
  // of each write when the keeper queues it, and keeper.onSettled runs once
  // with the same record when the write commits, fails or is replaced. Its
  // second argument is true only when the store committed the write. The
  // record of a delete is null. Thus the page can count the writes that
  // hold an image, and tell the size of the stored image.
  //
  // The keeper does not write before arm. Thus load can read the saved
  // record, and the editor can offer it before a new draft replaces it.
  // replace arms the keeper and writes the snapshot at once, in one put or
  // one delete, also when the store has the same text. Thus a discard or
  // an apply never deletes the record and then puts a new one in a second
  // write that can fail.
  //
  // With keeper.holdUntilChange set, arm keeps the text of the snapshot,
  // and the keeper does not write, and unsaved is false, until the text of
  // a snapshot differs from it. From that change on, the keeper works as
  // without the option, also when the snapshot gets the old text again.
  // The background keeper sets it, so a stored background that did not
  // restore, or that another tab wrote after the restore, stays in the
  // store until a background change of this page.
  //
  // unsaved tells if the draft has changes that are not in the store: a
  // write is still in the queue, or the snapshot is not the text of the
  // last write that the store committed. status is "ok", "off" without a
  // store or after a failed load, "failed" after a failed write, or "full"
  // after a write that the storage quota stopped. keeper.onStatus runs when
  // the status changes. After a failed load, the keeper does not write.
  //
  // For the draft, a snapshot of null is not an unsaved change, because
  // the saved draft can wait for an offer. With keeper.deletes set, a
  // snapshot of null after arm is an unsaved change until the delete of
  // the record commits. The background keeper sets it, so a removed
  // background that is still in the store keeps the unload question.
  //
  // Each editor tab of one server writes the same record. keeper.channel,
  // when given, tells the other tabs of each write. It is a BroadcastChannel,
  // which does not give a message to the channel that sent it. When another
  // tab writes or deletes the record, the draft of this tab is not in the
  // store. Then unsaved tells so, keeper.onDisplaced runs, and the next
  // flush writes again. The keeper does not write again at once, because
  // two tabs would then replace the record of each other without end.
  function createDraftKeeper(keeper) {
    let store = keeper.store || null; let status = store ? "ok" : "off";
    let armed = false; let waiting = null;
    // stored is the text of the record in the store, and queued is the
    // text of the last queued write. An empty text is no record. Only a
    // committed write sets stored. A failed write sets queued to null when
    // no newer write waits, so the next flush writes again. A write of
    // another tab sets stored and queued to null. pending counts the
    // writes that did not settle. next is the write that waits for the
    // running write, or null, and writes resolves when the last queued
    // write settles.
    let stored = ""; let queued = ""; let pending = 0; let next = null; let draining = false; let writes = Promise.resolve();
    // changed tells if the snapshot changed since arm, and baseline is the
    // text of the snapshot at arm. See keeper.holdUntilChange.
    let changed = !keeper.holdUntilChange; let baseline = "";
    const channel = keeper.channel || null;
    const textOf = (record) => (record ? (keeper.text ? keeper.text(record) : JSON.stringify(record)) : "");
    const hook = (name, record, committed) => { if (keeper[name]) keeper[name](record, committed); };
    const setStatus = (value) => { if (value === status) return; status = value; if (keeper.onStatus) keeper.onStatus(value); };
    const cancel = () => { if (waiting !== null) keeper.clock.clearTimeout(waiting); waiting = null; };
    // noteChange tells if the keeper can write a snapshot with the text.
    const noteChange = (text) => {
      if (!changed && armed && text !== baseline) changed = true;
      return changed;
    };
    const settle = (item, committed = false) => {
      pending -= 1; hook("onSettled", item.record, committed);
      item.record = null; item.resolve(); item.resolve = null;
    };
    const drain = async () => {
      while (next) {
        const item = next; next = null;
        let committed = false;
        try {
          await (item.record ? store.put(keeper.key, item.record) : store.delete(keeper.key));
          stored = item.text; committed = true; setStatus("ok"); if (channel) channel.postMessage({ key: keeper.key });
        } catch (error) {
          if (!next) queued = null;
          setStatus(error && error.name === "QuotaExceededError" ? "full" : "failed");
        }
        settle(item, committed);
      }
      draining = false;
    };
    const write = (text, record) => {
      queued = text; pending += 1; hook("onQueued", record);
      if (next) { const replaced = next; next = null; settle(replaced); }
      const item = { text, record }; const done = new Promise((resolve) => { item.resolve = resolve; });
      next = item;
      // One drain owns the queue. Replaced writes leave no promise callback.
      if (!draining) { draining = true; writes = Promise.resolve().then(drain); }
      return done;
    };
    if (channel) {
      channel.addEventListener("message", (event) => {
        const message = event.data;
        if (!message || message.key !== keeper.key) return;
        stored = null; queued = null;
        if (keeper.onDisplaced) keeper.onDisplaced();
      });
    }
    const flush = () => {
      cancel();
      if (!armed || !store) return writes;
      const record = keeper.snapshot(); const text = textOf(record);
      return !noteChange(text) || text === queued ? writes : write(text, record);
    };
    return {
      get status() { return status; },
      get unsaved() {
        if (pending > 0) return true;
        const record = keeper.snapshot(); const text = textOf(record);
        if (keeper.holdUntilChange && !noteChange(text)) return false;
        if (!record) return Boolean(keeper.deletes && armed && store) && stored !== "";
        return !store || text !== stored;
      },
      async load() {
        if (!store) return null;
        try {
          const record = await store.get(keeper.key);
          stored = textOf(record); queued = stored; return record || null;
        } catch (_) { store = null; setStatus("off"); return null; }
      },
      arm() {
        if (!armed && !changed) baseline = textOf(keeper.snapshot());
        armed = true; return flush();
      },
      schedule() {
        cancel();
        if (!armed || (!changed && !noteChange(textOf(keeper.snapshot())))) return;
        if (store) waiting = keeper.clock.setTimeout(flush, keeper.delay);
      },
      flush,
      replace() {
        cancel(); armed = true; changed = true;
        if (!store) return writes;
        const record = keeper.snapshot();
        return write(textOf(record), record);
      },
    };
  }

  // draftChanged tells if the scenario of draft differs from the live
  // baseline. live.scenario is the JSON text of the live scenario. Pause
  // and apply sends only the scenario, so it needs a scenario change. The
  // background is not part of the saved draft. The page keeps it apart, as
  // backgroundRecordFor gives it.
  function draftChanged(draft, live) {
    return JSON.stringify(draft.scenario) !== live.scenario;
  }

  // draftRecordFor gives the record that the keeper saves for draft: the
  // scenario, and the revision, the epoch and the server start ID of base,
  // the draft base. It gives null when live is null, which is before the
  // load ends, and when the draft scenario is the live scenario.
  function draftRecordFor(draft, live, base) {
    if (!live || !draftChanged(draft, live)) return null;
    return { scenario: draft.scenario, revision: base.revision, epoch: base.epoch, serverStart: base.serverStart };
  }

  // draftOffer gives the saved draft that the editor offers to restore, or
  // null. record is the record that createDraftKeeper saved, and live is
  // the live baseline, as draftChanged uses it. The editor offers a record
  // that has a scenario object that differs from the live scenario. It
  // ignores other members, such as the background that an older editor
  // saved in the draft. revision is the project
  // revision that the draft started from, epoch is the session epoch of
  // that revision, and serverStart is the server start ID of that state.
  // A record from an older editor has no serverStart, so it gets an empty
  // string.
  function draftOffer(record, live) {
    const isObject = (value) => value !== null && typeof value === "object" && !Array.isArray(value);
    if (!isObject(record) || !isObject(record.scenario)) return null;
    const draft = { scenario: record.scenario };
    if (!draftChanged(draft, live)) return null;
    const text = (value) => (typeof value === "string" ? value : "");
    return { draft, revision: Math.max(0, Math.floor(Number(record.revision) || 0)), epoch: text(record.epoch), serverStart: text(record.serverStart) };
  }

  // backgroundRecordFor gives the record that the background keeper saves
  // for background, the background of the page with its image key, its
  // placement, its opacity and its frame state. image has bytes, the image
  // as an ArrayBuffer, frame and license. The record holds the image and
  // the placement in one value, so one put writes both. It gives null when
  // the page has no background. Then the keeper deletes the record.
  function backgroundRecordFor(background, image) {
    if (!background) return null;
    const { imageKey, x, y, width, height, opacity } = background;
    return { background: { imageKey, x, y, width, height, opacity, frameState: background.frameState || "none", image: { bytes: image.bytes, frame: image.frame ?? null, license: image.license ?? null } } };
  }

  // backgroundRecordText gives the keeper text of a background record: the
  // JSON text of the record without its image. An ArrayBuffer has the JSON
  // text {}, so the image key stands for the image. A record that JSON
  // cannot write gives a text that no page record gives.
  function backgroundRecordText(record) {
    try {
      const background = record && typeof record === "object" ? record.background : undefined;
      if (!background || typeof background !== "object") return JSON.stringify(record) ?? "invalid";
      const { image: _, ...rest } = background;
      return JSON.stringify({ background: rest });
    } catch (_) { return "invalid"; }
  }

  // storedBackground gives the background and the image of record, a
  // record that the background keeper saved, or null when there is no
  // record. background has the image key, the placement, the opacity and
  // the frame state. image has bytes, mime, pixelWidth and pixelHeight from
  // the image header, frame and license. It throws an error when the
  // record is not valid.
  function storedBackground(record, { deferMetadata = false } = {}) {
    if (record === null || record === undefined) return null;
    if (typeof record !== "object" || Array.isArray(record)) throw new Error("The stored background is not a record.");
    const item = record.background;
    if (item === null || typeof item !== "object" || Array.isArray(item)) throw new Error("The background must be an object.");
    if (typeof item.imageKey !== "string" || !IMAGE_KEY_PATTERN.test(item.imageKey)) throw new Error("The stored background has no valid image key.");
    if (!deferMetadata) checkPlacement(item);
    const image = item.image;
    if (image === null || typeof image !== "object" || Array.isArray(image)) throw new Error("The stored background has no image.");
    const facts = imageBytesFacts(image.bytes);
    const asset = { frameState: item.frameState, frame: image.frame ?? null, license: image.license ?? null };
    if (!deferMetadata) { const error = assetError(asset); if (error) throw new Error(error); }
    const { imageKey, x, y, width, height, opacity } = item;
    return {
      background: { imageKey, x, y, width, height, opacity, frameState: asset.frameState },
      image: { bytes: image.bytes, mime: facts.mime, pixelWidth: facts.width, pixelHeight: facts.height, frame: asset.frame, license: asset.license },
    };
  }

  // STORED_BACKGROUND_KEPT_TEXT tells the user that the browser keeps a
  // stored background that is not valid, until a new background of this
  // page replaces it.
  const STORED_BACKGROUND_KEPT_TEXT = "The stored background stays in this browser until a new background replaces it.";

  // restoreStoredBackground puts the background of record, a record of the
  // background store, on the page. The page blocks input until the restore
  // ends, as startEditor does, so no background change of the user comes
  // before it. page.decode, when given, decodes the image of the restored
  // value, as storedBackground gives it, and rejects when the check of the
  // decoded image fails. page.install puts the restored value on the page,
  // and page.warn tells the user of a record that is not valid. The record
  // then stays in the store, because the background keeper does not write
  // before a background change of the page. It gives true when it installs
  // the background.
  async function restoreStoredBackground(record, page) {
    let restored = null;
    try {
      restored = storedBackground(record, { deferMetadata: Boolean(page.metadata) });
      if (restored && page.metadata) {
        const metadata = await page.metadata({ placement: Object.fromEntries(["x", "y", "width", "height", "opacity"].map((key) => [key, restored.background[key]])), asset: { frameState: restored.background.frameState, frame: restored.image.frame, license: restored.image.license } });
        restored.background = { imageKey: restored.background.imageKey, ...metadata.placement, frameState: metadata.asset.frameState };
        restored.image = { ...restored.image, frame: metadata.asset.frame, license: metadata.asset.license };
      }
      if (restored && page.decode) await page.decode(restored);
    } catch (error) { page.warn(`${error.message} ${STORED_BACKGROUND_KEPT_TEXT}`); return false; }
    if (!restored) return false;
    await page.install(restored);
    return true;
  }

  // IMAGE_TABLE_BYTES is the largest sum of the encoded bytes of the
  // distinct images that one editor tab holds. One image has at most
  // IMAGE_FILE_BYTES, so the table holds 16 images.
  const IMAGE_TABLE_BYTES = 128 * MIB;

  // DEFAULT_OPACITY is the opacity of a new background with no background
  // before it.
  const DEFAULT_OPACITY = 0.45;

  // freezeImage gives the frozen image descriptor of parts: key, bytes, an
  // ArrayBuffer, mime, pixelWidth, pixelHeight, frame and license. The
  // frame and the license are frozen copies, and no code writes to the
  // bytes, so no image changes under its key.
  function freezeImage(parts) {
    const frame = parts.frame ? Object.freeze({ ...parts.frame }) : null;
    const license = parts.license ? Object.freeze({ ...parts.license }) : null;
    return Object.freeze({ key: parts.key, bytes: parts.bytes, mime: parts.mime, pixelWidth: parts.pixelWidth, pixelHeight: parts.pixelHeight, frame, license });
  }

  // mibText gives a byte count in MiB with one decimal.
  function mibText(bytes) { return (bytes / MIB).toFixed(1); }

  // Go owns history. The browser owns image bytes, pins, and local previews.
  function createGoBackgroundModel(options) {
    const cap = options.cap ?? IMAGE_TABLE_BYTES;
    const images = new Map(), pins = new Map();
    let loaded = clone(options.initial), edits = 0, dragChanged = false;
    let gesture = null, own = false, ticket = null;
    const history = options.historyFactory(options.initial, (checksUnchanged) => {
      edits++;
      if (gesture && !own) gesture = { entry: history.snapshot, changed: false };
      prune(); options.onChange?.(checksUnchanged);
    }, () => { prune(); options.onAcknowledged?.(); });
    const keyOf = (background) => background?.imageKey || "";
    const sizeOf = (keys) => { let total = 0; for (const key of keys) total += images.get(key)?.bytes.byteLength || 0; return total; };
    const pinnedKeys = () => new Set([...pins.keys(), keyOf(history.background), keyOf(loaded.background)].filter(Boolean));
    function prune() {
      if (history.failed) return;
      const keys = history.keys();
      for (const key of pinnedKeys()) keys.add(key);
      for (const key of images.keys()) if (!keys.has(key)) images.delete(key);
    }
    const keySignature = (keys) => [...keys].sort().join("|");
    function imageBytes(keys, image) {
      let total = 0;
      for (const key of keys) {
        const descriptor = key === image?.key ? image : images.get(key);
        if (!descriptor) throw new Error("A retained history image is missing.");
        total += descriptor.bytes.byteLength;
      }
      return total;
    }
    function planImageAdmission(image, view, lookup) {
      const protectedKeys = pinnedKeys(), references = new Map();
      for (const id of view.retained) {
        const entry = lookup(id);
        if (!entry) throw new Error("A retained history snapshot is missing.");
        const key = keyOf(entry.background);
        if (key) references.set(key, (references.get(key) || 0) + 1);
      }
      const keys = new Set([...protectedKeys, ...references.keys(), image.key]);
      let total = imageBytes(keys, image), trim = 0;
      while (total > cap && view.retained[trim] !== view.head) {
        const key = keyOf(lookup(view.retained[trim++]).background);
        if (!key) continue;
        const remaining = references.get(key) - 1;
        references.set(key, remaining);
        if (!remaining && !protectedKeys.has(key) && key !== image.key) {
          keys.delete(key); total -= images.get(key).bytes.byteLength;
        }
      }
      if (total > cap) throw new Error("The image does not fit with the protected images of this tab.");
      return { trim, protectedKeys, signature: keySignature(protectedKeys), keys };
    }
    function installImage(image, view, protectedKeys) {
      const keys = new Set([...view.imageKeys, ...protectedKeys, image.key]);
      if (imageBytes(keys, image) > cap) throw new Error("The image admission exceeds the image limit of this tab.");
      for (const key of images.keys()) if (!keys.has(key)) images.delete(key);
      images.set(image.key, image);
    }
    function abort() { if (ticket) { const controller = ticket.controller; ticket = null; controller.abort(); } }
    function publishError(request, image) {
      if (!request || request !== ticket || request.signal.aborted) return "A newer action stopped the import.";
      if (edits !== request.edits) return "The draft changed during the import.";
      if (dragChanged || gesture?.changed) return "A drag or an opacity change was open at the end of the import. Finish it and import again.";
      if (image) {
        const keys = pinnedKeys(), held = sizeOf(keys);
        keys.add(image.key);
        if (imageBytes(keys, image) > cap) return `The image does not fit in the ${mibText(cap)} MiB image limit of this tab. The images that the tab must keep use ${mibText(held)} MiB, with ${mibText(sizeOf(pins.keys()))} MiB for writes to the browser store.`;
      }
      return "";
    }
    return {
      history, cap,
      get loaded() { return clone(loaded); },
      setLoaded(value) { loaded = clone(value); prune(); },
      image(key) { return images.get(key) || null; },
      keys() { return [...images.keys()]; },
      get bytes() { return sizeOf(images.keys()); },
      get keeperBytes() { return sizeOf(pins.keys()); },
      pinCount(key) { return pins.get(key) || 0; },
      pin(key) { pins.set(key, (pins.get(key) || 0) + 1); },
      unpin(key) { const count = (pins.get(key) || 0) - 1; if (count > 0) pins.set(key, count); else pins.delete(key); prune(); },
      get edits() { return edits; },
      moveDrag() { edits++; dragChanged = true; },
      endDrag() { dragChanged = false; },
      get gestureOpen() { return Boolean(gesture); },
      get gestureChanged() { return Boolean(gesture?.changed); },
      pressOpacity() { gesture = { entry: history.snapshot, changed: false }; },
      setOpacity(opacity) {
        const background = history.background;
        if (!background) return false;
        background.opacity = opacity;
        let changed;
        own = true;
        try { changed = history.preview({ scenario: history.snapshot.scenario, background }); } finally { own = false; }
        if (changed && gesture) gesture.changed = true;
        return changed;
      },
      endOpacity() {
        if (!gesture) return Promise.resolve(false);
        const before = gesture.entry; gesture = null;
        return history.commitFrom(before, history.snapshot);
      },
      takeOpacity() { const ended = gesture; gesture = null; return ended; },
      start() { abort(); const controller = new AbortController(); ticket = { signal: controller.signal, controller, edits }; return ticket; },
      abort,
      current(request) { return request === ticket && !request.signal.aborted; },
      publishError,
      async publish(request) {
        const error = publishError(request.ticket, request.image);
        if (error) { if (request.ticket === ticket) ticket = null; return { error, dropped: 0 }; }
        let published = false, admission = null;
        try {
          await history.replace(request.value, true, false, {
            current: () => !publishError(request.ticket, request.image) && (!admission || admission.signature === keySignature(pinnedKeys()) && imageBytes(admission.keys, request.image) <= cap),
            planTrim: request.image ? (view, lookup) => { admission = planImageAdmission(request.image, view, lookup); return admission.trim; } : undefined,
            beforePublish: (_changed, view) => {
              if (request.image) installImage(request.image, view, admission.protectedKeys);
              if (request.baseline) loaded = clone(request.value);
              ticket = null; published = true;
              request.beforePublish?.();
            },
          });
          if (!published) return { error: publishError(request.ticket, request.image) || "The draft changed during the import.", dropped: 0 };
          prune(); return { error: "", dropped: admission?.trim || 0 };
        } finally { if (request.ticket === ticket) ticket = null; }
      },
      async restore(image, background) {
        const protectedKeys = pinnedKeys(), signature = keySignature(protectedKeys);
        if (imageBytes(new Set([...protectedKeys, image.key]), image) > cap) throw new Error("The restored image does not fit with the protected images of this tab.");
        await history.reset({ scenario: history.snapshot.scenario, background }, {
          current: () => signature === keySignature(pinnedKeys()),
          beforePublish: (_changed, view) => {
            installImage(image, view, protectedKeys);
            loaded = { scenario: loaded.scenario, background: clone(background) };
          },
        });
      },
    };
  }

  // abortError gives the error of an aborted request.
  function abortError() { return new DOMException("The request was aborted.", "AbortError"); }

  // throwIfAborted throws abortError when signal is aborted.
  function throwIfAborted(signal) { if (signal && signal.aborted) throw abortError(); }

  // createDecoderSlot gives the decoder slot of an editor tab. A decode or
  // an encode of the browser has no abort, and a dropped promise still
  // uses its memory, so the tab runs one decoder operation at a time. The
  // slot has one running operation and at most one waiting request. run
  // queues work, a function of an AbortSignal that gives a promise, and
  // gives the promise of its result. A newer request replaces the waiting
  // request, which rejects at once with an AbortError, with no call of its
  // work. A request that is aborted when it gets the slot rejects, and
  // gives the slot back. A running operation keeps the slot until its
  // promise settles, also after an abort, so it can release its bitmaps
  // and its canvas first.
  function createDecoderSlot() {
    let running = false; let waiting = null;
    function next() {
      const request = waiting; waiting = null;
      if (!request) return;
      if (request.signal && request.signal.aborted) { request.reject(abortError()); next(); return; }
      running = true;
      Promise.resolve().then(() => request.work(request.signal)).then(request.resolve, request.reject).finally(() => { running = false; next(); });
    }
    return {
      get busy() { return running; },
      get waiting() { return waiting !== null; },
      run(work, signal) {
        return new Promise((resolve, reject) => {
          if (signal && signal.aborted) { reject(abortError()); return; }
          if (waiting) waiting.reject(abortError());
          waiting = { work, signal, resolve, reject };
          if (!running) next();
        });
      },
    };
  }

  // decodeBytes decodes image bytes with the facts of imageBytesFacts, and
  // checks the decoded size as checkDecodedSize does. deps.decode is
  // createImageBitmap or a test fake. The Blob of the decode lives only
  // until the decode settles. It gives the bitmap, and closes it when a
  // check fails.
  async function decodeBytes(bytes, facts, deps) {
    let blob = new Blob([bytes], { type: facts.mime });
    let bitmap;
    try { bitmap = await deps.decode(blob); } catch (_) { throw new Error("The browser cannot decode the background image."); } finally { blob = null; }
    try { checkDecodedSize({ width: bitmap.width, height: bitmap.height }, facts); } catch (error) { bitmap.close(); throw error; }
    return bitmap;
  }

  // checkImageBytes checks image bytes, an ArrayBuffer, with
  // imageBytesFacts, then decodes them and checks the decoded size. It
  // runs in the decoder slot, and closes the bitmap before it ends. It
  // gives the facts of imageBytesFacts.
  async function checkImageBytes(bytes, deps, signal) {
    const facts = imageBytesFacts(bytes);
    const bitmap = await decodeBytes(bytes, facts, deps);
    bitmap.close();
    throwIfAborted(signal);
    return facts;
  }

  // resampleImage makes a latitude and longitude grid image from a Web
  // Mercator image. request has bytes, the source image, frame, geo and
  // metersPerPixel, the output meters for each pixel in geo. It runs in
  // the decoder slot, in the steps of the design: decode and check the
  // source, draw each output row from its source row, close the source,
  // encode the canvas as a PNG, release the canvas, then check the PNG as
  // checkImageBytes does. deps has decode, canvas(width, height) and
  // encode(canvas), which gives a promise of a Blob. It gives bytes and
  // facts of the PNG.
  async function resampleImage(request, deps, signal) {
    const facts = imageBytesFacts(request.bytes);
    const size = resampleSize(request.frame, request.geo, request.metersPerPixel);
    if (size.error) throw new Error(size.error);
    let source = null; let canvas = null; let blob = null;
    try {
      source = await decodeBytes(request.bytes, facts, deps);
      throwIfAborted(signal);
      canvas = deps.canvas(size.width, size.height);
      const context = canvas.getContext("2d");
      context.imageSmoothingEnabled = true; context.imageSmoothingQuality = "high";
      const last = source.height - 1;
      for (const [row, item] of resampleRows(request.frame, size.height, source.height).entries()) {
        context.drawImage(source, 0, Math.min(Math.max(item.source - 0.5, 0), last), source.width, 1, 0, row, size.width, 1);
      }
      source.close(); source = null;
      blob = await deps.encode(canvas);
    } finally {
      if (source) source.close();
      if (canvas) { canvas.width = 0; canvas.height = 0; }
    }
    throwIfAborted(signal);
    if (!blob) throw new Error("The browser cannot encode the resampled image.");
    let bytes;
    try { bytes = await blob.arrayBuffer(); } finally { blob = null; }
    throwIfAborted(signal);
    return { bytes, facts: await checkImageBytes(bytes, deps, signal) };
  }

  // FRAME_WARNING_TEXT tells the user that an attached background is not
  // on its frame.
  const FRAME_WARNING_TEXT = "The background is not on its frame in the geographic reference of the project. Place it from the frame, or detach the frame.";

  // frameView gives the frame controls of the Background panel for value,
  // a history value, and image, its image descriptor or null. framed is
  // true for an image with a frame. warning is FRAME_WARNING_TEXT for an
  // attached background that is not aligned, computed at each render.
  // placeReason and calibrateReason are the reasons why Place from frame
  // and Calibrate scale are disabled, or empty texts.
  function frameView(value, image) {
    const background = value.background;
    const framed = Boolean(background && image && image.frame && background.frameState !== "none");
    if (!framed) return { framed, state: "none", warning: "", placeReason: "", calibrateReason: "" };
    const geo = value.scenario.geo || null;
    const attached = background.frameState === "attached";
    const warning = attached && !frameAligned(background, image.frame, geo) ? FRAME_WARNING_TEXT : "";
    const placeReason = !geo && value.scenario.network.Nodes.length ? "The project has nodes and no geographic reference. Choose an anchor or adopt the image center below." : "";
    const calibrateReason = attached ? "Detach the frame before you calibrate the scale." : "";
    return { framed, state: background.frameState, warning, placeReason, calibrateReason };
  }

  // attributionParts gives the parts of the attribution line of license:
  // the attribution text, then links to the copyright URL, and to the
  // license URL when it differs. A part has text and href, which is set
  // only for an HTTPS URL. It gives an empty list with no license.
  function attributionParts(license) {
    if (!license) return [];
    const parts = [];
    const text = license.attribution || license.source;
    if (text) parts.push({ text, href: "" });
    if (license.copyrightURL && httpsURL(license.copyrightURL)) parts.push({ text: "Copyright", href: license.copyrightURL });
    if (license.licenseURL && httpsURL(license.licenseURL) && license.licenseURL !== license.copyrightURL) parts.push({ text: license.license || "License", href: license.licenseURL });
    return parts;
  }

  // BACKGROUND_DURABLE_TEXT tells the user that the browser store is not
  // a durable copy of the background.
  const BACKGROUND_DURABLE_TEXT = "The browser can delete its stored background. The exported project file is the only durable copy.";

  // backgroundFacts gives the lines of the Background panel: the license
  // facts of image, the image bytes that the tab holds against the cap
  // with the share of the keeper writes, the size of the stored image,
  // and the notes. facts has image, held, keeperBytes, cap, storedBytes
  // and restoreFailed.
  function backgroundFacts(facts) {
    const lines = [];
    const license = facts.image && facts.image.license;
    if (license) {
      for (const [key, label] of [["source", "Source"], ["license", "License"], ["retrieved", "Retrieved"], ["method", "Method"], ["notice", "Notice"]]) if (license[key]) lines.push(`${label}: ${license[key]}`);
    }
    lines.push(`This tab holds ${mibText(facts.held)} MiB of images of its ${mibText(facts.cap)} MiB limit, with ${mibText(facts.keeperBytes)} MiB for writes to the browser store.`);
    if (facts.storedBytes > 0) lines.push(`The stored image has ${mibText(facts.storedBytes)} MiB.`);
    if (facts.image) lines.push(BACKGROUND_DURABLE_TEXT);
    if (facts.restoreFailed) lines.push(STORED_BACKGROUND_KEPT_TEXT);
    return lines;
  }

  // STARTUP_EVENTS are the input events that blockInput stops while the
  // editor starts.
  const STARTUP_EVENTS = ["click", "change", "input", "keydown", "pointerdown", "pointerup", "wheel", "contextmenu"];

  // createStartupGate gives the gate that holds the input of the user while
  // the editor starts. closed is true until the first release. ready is a
  // promise that resolves at the release. A second release does nothing.
  // guard stops an event while the gate is closed, and tells if it stopped
  // the event.
  function createStartupGate() {
    let closed = true; let open;
    const ready = new Promise((resolve) => { open = resolve; });
    return {
      ready,
      get closed() { return closed; },
      guard(event) {
        if (!closed) return false;
        event.stopImmediatePropagation(); event.preventDefault(); return true;
      },
      release() { if (!closed) return; closed = false; open(); },
    };
  }

  // blockInput stops the STARTUP_EVENTS of document while gate is closed.
  // Its listeners are in the capture phase of document, so they run before
  // the listeners of the page. A wheel listener of a document is passive
  // by default, so the listeners are not passive. exempt tells if an event
  // can pass. While the gate is closed, each element with the data-startup
  // attribute is inert, and the body has aria-busy. At the release,
  // blockInput removes the listeners, the inert attributes, the
  // data-startup attributes and aria-busy. It gives a promise that
  // resolves after the removal.
  function blockInput(document, gate, exempt) {
    const options = { capture: true, passive: false };
    const stop = (event) => { if (!exempt(event)) gate.guard(event); };
    const marked = [...document.querySelectorAll("[data-startup]")];
    for (const type of STARTUP_EVENTS) document.addEventListener(type, stop, options);
    for (const element of marked) element.setAttribute("inert", "");
    document.body.setAttribute("aria-busy", "true");
    return gate.ready.then(() => {
      for (const type of STARTUP_EVENTS) document.removeEventListener(type, stop, options);
      for (const element of marked) { element.removeAttribute("inert"); element.removeAttribute("data-startup"); }
      document.body.removeAttribute("aria-busy");
    });
  }

  // startupExempt tells if an input event can pass while the editor starts.
  // An event on the Simulation link passes, so the user can leave the page.
  // The Tab key passes, so the keyboard can get to the link.
  function startupExempt(event) {
    if (event.type === "keydown" && event.key === "Tab") return true;
    const target = event.target;
    return Boolean(target && typeof target.closest === "function" && target.closest("#simulationLink"));
  }

  // startEditor starts the editor page. page.readDraft reads the saved
  // draft and page.readBackground reads the stored background, while
  // page.loadLive loads the live scenario. Then page.restore puts the
  // stored background on the page. The page blocks input until the live
  // load and the restore end, so no change of the user can come before
  // the live scenario or the stored background. page.release then opens
  // the startup gate, also when a step fails, so the page does not stay
  // blocked. After the release, page.armBackground starts the background
  // keeper. The gate does not wait for the saved draft, so a draft store
  // that does not answer does not stop the editor. page.offerDraft offers
  // the saved draft when its read ends. The draft keeper does not save
  // until the offer ends, so an edit before the offer cannot replace the
  // saved draft. When a step fails, startEditor rejects with the error and
  // does not start the background keeper, so the keeper cannot delete the
  // stored background.
  async function startEditor(page) {
    const saved = page.readDraft(); const stored = page.readBackground();
    try {
      await page.loadLive();
      await page.restore(await stored);
    } finally { page.release(); }
    await Promise.all([page.armBackground(), saved.then(page.offerDraft)]);
  }

  // BACKGROUND_STORE_TEXT tells the user when the browser does not keep the
  // background, for each status of the background keeper that is not "ok".
  const BACKGROUND_STORE_TEXT = {
    off: "This browser cannot keep the background. Export the project to keep it.",
    failed: "This browser could not save the background. Export the project to keep it.",
    full: "The browser storage is full, so the background is not saved. Export the project to keep it.",
  };

  // DRAFT_RESTART_TEXT tells the user that the saved draft is from before
  // a server restart, and what Pause and apply then does.
  const DRAFT_RESTART_TEXT = "The draft was saved before the server restarted. After you restore it, Pause and apply stops with a conflict and does not change the live scenario. Then you can load the live scenario or apply the draft over it.";

  // draftOfferText gives the text of the saved draft offer. offer.revision
  // is the revision that the draft started from. offer.live is the live
  // project revision, or null when the live scenario did not load.
  // offer.serverStart is the server start ID of the draft, and
  // offer.liveStart is the live server start ID. When draftBeforeRestart
  // is true for them, the text also has DRAFT_RESTART_TEXT.
  function draftOfferText(offer) {
    const based = `This browser has a saved draft that is not applied. The draft is based on revision ${offer.revision}.`;
    const text = offer.live === null || offer.live === offer.revision ? based : `${based} The live scenario is now revision ${offer.live}.`;
    return draftBeforeRestart(offer.serverStart, offer.liveStart) ? `${text} ${DRAFT_RESTART_TEXT}` : text;
  }

  // RESTORED_RESTART_TEXT tells the user that the restored draft is from
  // before a server restart, and what Pause and apply then does.
  const RESTORED_RESTART_TEXT = "It was saved before the server restarted. Pause and apply stops with a conflict and does not change the live scenario. After the conflict, the Apply over revision button replaces the live scenario with the draft.";

  // restoreStatusText gives the status line after a restore. draft.revision
  // is the revision that the restored draft started from. draft.live is the
  // live project revision, or null when the live scenario did not load.
  // draft.serverStart is the server start ID of the draft, and
  // draft.liveStart is the live server start ID. When draftBeforeRestart
  // is true for them, Pause and apply stops with a conflict, so the text
  // tells that first. The revisions of two server runs do not compare, so
  // the text then does not name them. Else Pause and apply replaces the
  // live scenario, so the text names the live changes that an older draft
  // replaces.
  function restoreStatusText(draft) {
    if (draft.live === null) return "The restored draft is local.";
    const text = `Live revision ${draft.live}. The restored draft is not applied.`;
    if (draftBeforeRestart(draft.serverStart, draft.liveStart)) return `${text} ${RESTORED_RESTART_TEXT}`;
    return draft.live === draft.revision ? text : `${text} It is based on revision ${draft.revision}. Pause and apply replaces the live changes after revision ${draft.revision}.`;
  }

  // shellPage gives the parent window when the editor is a frame of the
  // shell page index.html. Otherwise it gives null. The shell page sets
  // podsimShell. The browser blocks a read from a parent on a different
  // origin, so that parent is not the shell page.
  function shellPage(win) {
    try { return win.parent !== win && win.parent.podsimShell === true ? win.parent : null; } catch (_) { return null; }
  }

  function metadataURLFacts(metadata) {
    const facts = {}, license = metadata.asset?.license;
    if (license && typeof license === "object" && !Array.isArray(license)) for (const key of ["licenseURL", "copyrightURL"]) {
      if (typeof license[key] === "string" && license[key]) facts[key] = { text: license[key], https: httpsURL(license[key]) };
    }
    return { ...metadata, urlFacts: facts };
  }

  const API = {

    MIN_LANE_LENGTH, MAX_PODS, MAX_STATIONS, MAX_NODES, MAX_LANES, MAX_NODE_LANES, MAX_FLOWS, MIN_ZOOM, NODE_LABEL_SCALE, NODE_LABEL_SIZE, LANE_PAIR_OFFSET, CHEVRON_LANE_LENGTH, BERTH_PITCH, STATION_PADDING, CLEARANCE, CHECK_DELAY, emptyConfig, fallbackConfig,
    stationBearing, stationShape, stationLayout, stationGeometryCommand, berthChain, stationFlowCount, stationRailReferences, fleetRows, fleetClassNotice, selectionCard, berthFocusID, undoFocus,
    laneLength, curveLength, stationNodeOwners, dragTargets, checkSelector, checkSelection, selectionPoint, focusView,
    GEO_PROJECTION, GEO_RADIUS, GEO_MAX_LATITUDE, MAX_COORDINATE, FRAME_SOURCES, SCALE_TOLERANCE, ALIGN_TOLERANCE, RESAMPLE_MAX_SIDE, projectPoint, frameError, framePlacement, frameAligned, resampleSize, mercatorY, resampleRows,
    problemCountText, createCheckTimer, validationSummary, checkFocusKey, IMAGE_FILE_BYTES, IMAGE_MAX_SIDE, IMAGE_MAX_PIXELS, imageFacts, imageBytesFacts, dataURLToBytes, bytesToDataURL, checkImageSize,
    metadataURLFacts, IMAGE_KEY_PATTERN, newImageKey, FRAME_STATES, LICENSE_LIMITS, licenseError, assetError, backgroundRecordText, STORED_BACKGROUND_KEPT_TEXT, SERVER_PROJECT_BYTES, PROJECT_FILE_BYTES, SERVER_COMMAND_BYTES, SERVER_COMMAND_JSON_BYTES, GZIP_COMMAND_BYTES, SERVER_TOO_LARGE_TEXT, postCommand, dataURLBytes, serializeDocument, parseDocument,
    networkBounds, fitView, zoomScale, nodeLabelSize, pairedLaneIDs, showsChevron, laneOffset, laneCurve, lanePathData, SNAPSHOT_ATTEMPTS, snapshotConsistent, draftBeforeRestart, readState, readSnapshot, applyToServer, applyFailureText, applyFailureStatus, applyToast,
    readLive, readConflict, CONFLICT_UNLOADED_TEXT, conflictView, LOAD_LIVE_QUESTION, applyOverQuestion, loadLive, liveDraft, applyOverBase,
    DRAFT_SAVE_DELAY, DRAFT_STORE_TEXT, DRAFT_UNSAVED_TEXT, DRAFT_DISPLACED_TEXT, DRAFT_STORE, BACKGROUND_STORE, openRecordStore, createDraftKeeper, draftChanged, draftRecordFor, draftOffer, backgroundRecordFor, storedBackground, restoreStoredBackground,
    IMAGE_TABLE_BYTES, DEFAULT_OPACITY, freezeImage, createGoBackgroundModel, createDecoderSlot, checkImageBytes, resampleImage, FRAME_WARNING_TEXT, frameView, attributionParts, BACKGROUND_DURABLE_TEXT, backgroundFacts, exportBackground, documentAsset, STARTUP_EVENTS, createStartupGate, blockInput, startupExempt, startEditor, BACKGROUND_STORE_TEXT, checkBackground, DRAFT_RESTART_TEXT, draftOfferText, RESTORED_RESTART_TEXT, restoreStatusText, shellPage,
  };
  if (typeof module !== "undefined" && module.exports) {
    const reference = require("./editor-model-reference.cjs")({
      ...API, ANCHOR_MAX_RESIDUAL, ANCHOR_MIN_DISTANCE, BERTH_PITCH, CLEARANCE, DEFAULT_OPACITY,
      DEFAULT_SPEED, DEGREE, GEO_MAX_LATITUDE, GEO_PROJECTION, GEO_RADIUS, MAX_BERTHS,
      MAX_COORDINATE, MAX_LANES, MAX_NODES, MAX_NODE_LANES, MAX_STATIONS, MIN_LANE_LENGTH,
      SCALE_TOLERANCE, Tiles, berthChain, clone, emptyConfig, flowNamesStation,
      frameError, framePlacement, frozenDrafts, laneLength, point, projectPoint,
      shiftNodes, stationAxes, stationBearing, stationLayout, stationNodeIDs, stationNodeOwners,
    });
    Object.assign(API, reference);
    Object.assign(API, require("./editor-checks-reference.cjs")(API));
    Object.assign(API, require("./editor-history-reference.cjs")({ ...API, clone, freezeDraft: reference.freezeDraft, ownDraft, editDraft: reference.editDraft, MIB, mibText }));
    module.exports = API;
  }
  root.PodsimEditorModel = API;

  if (typeof document === "undefined") return;

  const $ = (selector) => document.querySelector(selector);
  const svgNS = "http://www.w3.org/2000/svg";
  // checks runs the checks CHECK_DELAY milliseconds after the last draft
  // change. The history schedules it for each change.
  const checks = createCheckTimer({ delay: CHECK_DELAY, run: runValidation, scheduled: scheduleValidation, clock: root });
  // model keeps the draft history with the images of its backgrounds, the
  // Reset draft baseline, the edit counter, the open gestures and the open
  // acquisition. Each draft change schedules both keepers. Changes that can
  // affect validation also schedule the checks.
  const model = createGoBackgroundModel({
    initial: { scenario: emptyConfig(), background: null },
    historyFactory: (initial, onChange, onAcknowledged) => root.PodsimGoEditor.createHistory({
      initial, own: ownDraft, clone, onChange, onAcknowledged,
      call: (...args) => goModel.call(...args), accept: (config, token) => goModel.acceptHistory(config, token),
      captureGuard: (kind) => {
        const generation = model.edits;
        return () => generation === model.edits && (kind === "dropOldest" || !(state.drag && state.drag.type !== "pan") && !model.gestureOpen);
      },
      onFatal: (error) => {
        goModel.close(); model.abort(); renderHistoryButtons(); renderApply();
        showValidation(draft(), { errors: [{ text: error.message }], warnings: [], valid: false });
        updateStatus("The Go editor history stopped. Export the draft before reloading.");
        toast(`${error.message} The visible draft and images are kept for export. Reload the editor to continue.`, true);
      },
    }),
    onAcknowledged: () => { renderHistoryButtons(); renderApply(); },
    onChange: (checksUnchanged) => { state.background = state.history.background; if (!checksUnchanged) checks.schedule(); keeper.schedule(); backgroundKeeper.schedule(); },
  });
  // slot is the decoder slot of the tab. Each check decode, the restore and
  // each resample run in it. decoder gives the browser functions to the
  // slot operations.
  const slot = createDecoderSlot();
  const decoder = {
    decode: (blob) => root.createImageBitmap(blob),
    canvas: (width, height) => Object.assign(document.createElement("canvas"), { width, height }),
    encode: (canvas) => new Promise((resolve) => canvas.toBlob(resolve, "image/png")),
  };
  // display is the object URL of the image that the map shows, with its
  // image key. The page makes it from the bytes of the present image only.
  let display = { key: "", url: "" };
  // storedBytes is the size of the image in the browser store, after the
  // restore or the last committed write of this tab, or 0.
  let storedBytes = 0;
  // keeper saves the draft in this browser DRAFT_SAVE_DELAY milliseconds
  // after the last draft change. It keeps one record for each server origin,
  // and tells the other editor tabs of each write.
  const keeper = createDraftKeeper({
    store: openRecordStore(draftDatabase(), DRAFT_STORE), key: root.location.origin, delay: DRAFT_SAVE_DELAY, clock: root, snapshot: draftRecord,
    channel: draftChannel(), onStatus: showDraftStatus, onDisplaced: showDraftDisplaced,
  });
  // backgroundKeeper keeps the background of the page in this browser,
  // apart from the draft, so a reload after an apply still shows it. It
  // keeps one record for each server origin, as the draft keeper does. It
  // writes when the background changes, also after an undo, a redo, a
  // project import and Reset draft. A page with no background deletes the
  // record, and the unload question stays until the delete commits. All
  // editor tabs of one server share the record, and the last background
  // change of a tab replaces it. It has no channel. It writes nothing
  // until the background of this page changes after the restore, so a tab
  // does not replace the record of another tab, or a record that did not
  // restore, when the user did not change the background. Each queued
  // write pins its image in the model until it settles.
  const backgroundKeeper = createDraftKeeper({
    store: openRecordStore(draftDatabase(), BACKGROUND_STORE), key: root.location.origin, delay: DRAFT_SAVE_DELAY, clock: root,
    snapshot: backgroundRecord, text: backgroundRecordText, onStatus: showBackgroundStatus, deletes: true, holdUntilChange: true,
    onQueued: (record) => { if (record) model.pin(record.background.imageKey); },
    onSettled: (record, committed) => {
      if (record) model.unpin(record.background.imageKey);
      if (committed) { storedBytes = record ? record.background.image.bytes.byteLength : 0; state.restoreFailed = false; }
      renderBackgroundInfo();
    },
  });
  const state = {
    history: model.history,
    background: null,
    // restoreFailed is true after a stored background did not restore,
    // until a write of this tab replaces it.
    restoreFailed: false,
    // geoOpen is true while the georeferenced image panel shows.
    geoOpen: false,
    mapOpen: false,
    loadedRevision: 0,
    // loadedStart is the server start ID of the state that the page loaded
    // or last applied, or an empty string when the server does not send it.
    loadedStart: "",
    // liveStart is the server start ID of the latest live state that the
    // page read, or an empty string. The onServerStart function of the
    // connection sets it. It is not the draft base, and a read after a
    // server restart changes it.
    liveStart: "",
    // refreshing is true while the page reads the live state for
    // refreshLiveStart.
    refreshing: false,
    // live is the live baseline for draftChanged. It has the scenario text
    // after the load or the last apply, and revision,
    // the live project revision, or null when the live scenario did not
    // load. It is null until the load ends.
    live: null,
    // offer is the saved draft that the page offers to restore, as
    // draftOffer gives it, or null.
    offer: null,
    // draftBase is the project revision, the session epoch and the server
    // start ID that the draft started from. The load, a successful apply and
    // Reset draft set it to the loaded revision and loadedStart. Restore
    // draft sets it to the values of the saved draft. Import JSON sets its
    // server start ID to loadedStart. Load live scenario sets it to the live
    // revision, epoch and server start ID that it read. Pause and apply
    // always sends loadedRevision, and it sends the server start ID of
    // draftBase. Apply over revision N sends its own base.
    draftBase: { revision: 0, epoch: "", serverStart: "" },
    // conflict is the apply conflict that the page shows, as readConflict
    // gives it with a revision, or null. A successful apply, Load live
    // scenario and Reset draft set it to null.
    conflict: null,
    // applying is true while an apply or a conflict action runs.
    applying: false,
    connection: {
      fetch: (url, init) => root.fetch(url, init),
      clientID: root.crypto && root.crypto.randomUUID ? root.crypto.randomUUID() : `editor-${Date.now()}-${Math.random()}`,
      sequence: 0,
      epoch: "",
    },
    selection: null,
    tool: "select",
    linkFrom: "",
    view: { x: 0, y: 0, scale: 1 },
    // map holds the drawn map elements by ID, so a drag can move them in place.
    map: null,
    drag: null,
    calibrating: false,
    calibrationPoints: [],
    toastTimer: 0,
  };

  let tilePageActive = true;
  const tiles = Tiles.createLayer({ container: $(".map-panel"), onStatus: (text) => { $("#mapDiagnostics").textContent = text; } });
  function renderTiles() {
    const config = draft(), rect = $("#networkMap").getBoundingClientRect();
    const enabled = tilePageActive && !document.hidden && !root.frameElement?.inert && Tiles.validMap(config.map, config.geo);
    tiles.setView({ ...state.view, width: rect.width, height: rect.height, geo: config.geo, opacity: config.map?.opacity, enabled });
  }

  function draft() { return state.history.snapshot.scenario; }
  function setAttributes(element, attributes) {
    for (const [key, value] of Object.entries(attributes || {})) element.setAttribute(key, value);
    return element;
  }
  function svgElement(name, attributes) { return setAttributes(document.createElementNS(svgNS, name), attributes); }
  const drawingNodeIndexes = new WeakMap();
  function nodeFor(config, id) {
    const nodes = config.network.Nodes;
    let index = drawingNodeIndexes.get(nodes);
    if (!index) {
      index = new Map();
      // Draft node IDs are immutable. Drag copies change positions only.
      // Keep the first node when an unfinished draft repeats an ID.
      for (const node of nodes) if (!index.has(node.ID)) index.set(node.ID, node);
      drawingNodeIndexes.set(nodes, index);
    }
    return index.get(id);
  }
  function stationForNode(config, id) { const stationID = stationNodeOwners(config).get(id); return config.network.Stations.find((station) => station.ID === stationID); }
  function setDraft(next, record = true, current = () => true) {
    return state.history.replace({ scenario: next, background: state.background }, record, false, {
      current, beforePublish: (changed) => { if (changed) render(); },
    });
  }
  function toast(message, error) {
    const element = $("#toast"); element.textContent = message; element.className = error ? "show error" : "show";
    clearTimeout(state.toastTimer); state.toastTimer = setTimeout(() => { element.className = ""; }, 4000);
  }
  function updateStatus(message) { $("#serverStatus").textContent = message; }
  // reportStationDeletion shows the references removed by an accepted edit.
  function reportStationDeletion(config, stationID) {
    const flows = stationFlowCount(config, stationID);
    const rail = stationRailReferences(config, stationID);
    if (rail > 0) toast(`Station deleted. Rail arrival plans updated${flows ? ` and ${flows} demand flows removed` : ""}.`);
    else if (flows > 0) toast(`Station deleted. ${flows} demand ${flows === 1 ? "flow" : "flows"} removed.`);
  }

  function worldPoint(event) {
    const rect = $("#networkMap").getBoundingClientRect();
    return { X: (event.clientX - rect.left - state.view.x) / state.view.scale, Y: (event.clientY - rect.top - state.view.y) / state.view.scale };
  }
  function setView() {
    $("#viewport").setAttribute("transform", `translate(${state.view.x} ${state.view.y}) scale(${state.view.scale})`);
    if (state.map && state.map.laneScale !== state.view.scale) scaleLanes();
    if (state.map && state.map.labelScale !== state.view.scale) renderNodeLabels();
    renderTiles();
  }
  function zoomAt(factor, clientX, clientY) {
    const rect = $("#networkMap").getBoundingClientRect();
    const sx = clientX - rect.left; const sy = clientY - rect.top;
    const old = state.view.scale; const next = zoomScale({ scale: old, factor, fitScale: fitView(state.map.bounds, rect).scale });
    const wx = (sx - state.view.x) / old; const wy = (sy - state.view.y) / old;
    state.view.scale = next; state.view.x = sx - wx * next; state.view.y = sy - wy * next; setView();
  }

  // drawLane sets the path of a drawn lane at the lane scale of the map. A
  // lane of a pair has an offset of LANE_PAIR_OFFSET screen pixels. A lane
  // that is short on the screen hides its chevron. The map keeps the length
  // for the next scale change.
  function drawLane(path, config, lane, map) {
    const from = nodeFor(config, lane.From)?.Position; const to = nodeFor(config, lane.To)?.Position;
    const length = from && to ? curveLength({ from, to, control: lane.Control }) : 0;
    const offset = laneOffset({ paired: map.paired.has(lane.ID), scale: map.laneScale });
    path.setAttribute("d", from && to ? lanePathData(laneCurve({ from, to, control: lane.Control, offset })) : "");
    path.classList.toggle("short", !showsChevron({ length, scale: map.laneScale }));
    map.lengths.set(lane.ID, length);
  }

  // The place functions give the position attributes of the drawn items.
  // renderMap and a drag both use them, so a moved item matches a redrawn one.
  // A junction label has an offset of one font size from its node, so its
  // position does not change when its font size changes.
  function placeNode(position) { return { cx: position.X, cy: position.Y }; }
  function placeNodeLabel(position) { return { x: position.X, y: position.Y }; }
  // placeStation gives the position attributes of a station shape and its
  // label, and the angle of the shape. stationShape gives the shape. at
  // gives the position of a node, and nodeIDs holds the station nodes. The
  // label is 5 m above the highest corner of the shape.
  function placeStation(at, station, nodeIDs) {
    const entry = at(station.Entry) || at(station.Exit) || { X: 0, Y: 0 }; const exit = at(station.Exit) || entry;
    const place = stationShape({ entry, exit, points: nodeIDs.map(at).filter(Boolean) });
    const { center, width, height, angle } = place;
    return { angle, shape: { x: center.X - width / 2, y: center.Y - height / 2, width, height, transform: `rotate(${angle} ${center.X} ${center.Y})` }, label: { x: center.X, y: place.top - 5 } };
  }
  // STATION_MARKERS gives the path data of the entry and the exit markers,
  // for a node at 0, 0 and travel along the X axis. The entry is a square.
  // The exit is a triangle that points in the direction of travel.
  const STATION_MARKERS = { entry: "M -5 -5 H 5 V 5 H -5 Z", exit: "M 7 0 L -5 6.5 L -5 -6.5 Z" };
  // placeMarker gives the position attributes of an entry or exit marker.
  // The marker turns to the angle of its station shape.
  function placeMarker(position, angle) { return { transform: `translate(${position.X} ${position.Y}) rotate(${angle})` }; }
  function placeControl(config, lane) {
    const from = nodeFor(config, lane.From); const to = nodeFor(config, lane.To);
    return { line: { d: `M ${from.Position.X} ${from.Position.Y} L ${lane.Control.X} ${lane.Control.Y} L ${to.Position.X} ${to.Position.Y}` }, handle: { cx: lane.Control.X, cy: lane.Control.Y } };
  }

  function renderMap() {
    drawnNetwork = null;
    const config = draft();
    const backgroundLayer = $("#backgroundLayer"); const laneLayer = $("#laneLayer"); const stationLayer = $("#stationLayer"); const stationLabelLayer = $("#stationLabelLayer"); const nodeLayer = $("#nodeLayer"); const handleLayer = $("#handleLayer");
    backgroundLayer.replaceChildren(); laneLayer.replaceChildren(); stationLayer.replaceChildren(); stationLabelLayer.replaceChildren(); nodeLayer.replaceChildren(); handleLayer.replaceChildren();
    // stationNodes holds the station nodes of each station, and markers holds
    // the entry and exit nodes. A drag keeps them, because it moves nodes
    // but does not change the lanes.
    const map = { config, bounds: networkBounds(config, state.background), lanes: new Map(), lengths: new Map(), paired: pairedLaneIDs(config), laneScale: state.view.scale, stations: new Map(), stationNodes: new Map(), markers: new Map(), nodes: new Map(), junctions: [], labels: new Map(), labelScale: null, handles: null };
    const shown = display.url; const url = displayURL(state.background);
    if (state.background) {
      const image = svgElement("image", { class: "background-image", href: url, x: state.background.x, y: state.background.y, width: state.background.width, height: state.background.height, opacity: state.background.opacity, preserveAspectRatio: "none" });
      backgroundLayer.append(image);
    }
    if (shown && shown !== url) URL.revokeObjectURL(shown);
    for (const lane of config.network.Lanes) {
      const path = svgElement("path", { class: `lane${state.selection && state.selection.type === "lane" && state.selection.id === lane.ID ? " selected" : ""}`, "data-type": "lane", "data-id": lane.ID });
      drawLane(path, config, lane, map); laneLayer.append(path); map.lanes.set(lane.ID, path);
    }
    const component = stationNodeOwners(config);
    const positions = new Map(config.network.Nodes.map((node) => [node.ID, node.Position]));
    for (const station of config.network.Stations) map.stationNodes.set(station.ID, []);
    for (const [id, stationID] of component) map.stationNodes.get(stationID)?.push(id);
    const angles = new Map();
    for (const station of config.network.Stations) {
      const place = placeStation((id) => positions.get(id), station, map.stationNodes.get(station.ID));
      const shape = svgElement("rect", { class: `station-shape${state.selection && state.selection.type === "station" && state.selection.id === station.ID ? " selected" : ""}`, ...place.shape, rx: 8, "data-type": "station", "data-id": station.ID });
      const label = svgElement("text", { class: "station-label", ...place.label }); label.textContent = station.Name;
      stationLayer.append(shape); stationLabelLayer.append(label); map.stations.set(station.ID, { shape, label });
      map.markers.set(station.Entry, "entry"); map.markers.set(station.Exit, "exit"); angles.set(station.Entry, place.angle); angles.set(station.Exit, place.angle);
    }
    for (const node of config.network.Nodes) {
      const stationID = component.get(node.ID); const marker = map.markers.get(node.ID);
      const data = { "data-type": stationID ? "station-node" : "node", "data-id": node.ID, "data-station": stationID || "" };
      const element = marker
        ? svgElement("path", { class: `station-node station-${marker}`, d: STATION_MARKERS[marker], ...placeMarker(node.Position, angles.get(node.ID)), ...data })
        : svgElement("circle", { class: stationID ? "station-node" : `junction${state.selection && state.selection.type === "node" && state.selection.id === node.ID ? " selected" : ""}`, ...placeNode(node.Position), r: stationID ? 5 : 7, ...data });
      nodeLayer.append(element); map.nodes.set(node.ID, element);
      if (!stationID) map.junctions.push(node);
    }
    if (state.selection && state.selection.type === "lane") {
      const lane = config.network.Lanes.find((item) => item.ID === state.selection.id);
      if (lane && lane.Control) {
        const place = placeControl(config, lane);
        const line = svgElement("path", { class: "control-line", ...place.line });
        const handle = svgElement("circle", { class: "control-handle", ...place.handle, r: 7, "data-type": "control", "data-id": lane.ID });
        handleLayer.append(line, handle); map.handles = { laneID: lane.ID, line, handle };
      }
    }
    if (state.linkFrom) {
      const from = nodeFor(config, state.linkFrom);
      if (from) handleLayer.append(svgElement("circle", { class: "control-handle", cx: from.Position.X, cy: from.Position.Y, r: 10 }));
    }
    for (const calibration of state.calibrationPoints) handleLayer.append(svgElement("circle", { class: "calibration-point", cx: calibration.X, cy: calibration.Y, r: 7 }));
    state.map = map; renderNodeLabels(); setView();
  }

  // scaleLanes changes the drawn lanes for the current scale. setView calls
  // it when the scale changes. A lane that is short on the screen hides its
  // chevron, and each lane of a pair moves to the offset for the scale.
  // During a drag, the lanes take their positions from the working copy of
  // the draft.
  function scaleLanes() {
    const map = state.map; const before = map.laneScale; map.laneScale = state.view.scale;
    for (const [id, path] of map.lanes) {
      const length = map.lengths.get(id); const shows = showsChevron({ length, scale: map.laneScale });
      // Only a lane that crosses the limit changes. This keeps a zoom step
      // fast on a large network.
      if (shows !== showsChevron({ length, scale: before })) path.classList.toggle("short", !shows);
    }
    if (!map.paired.size) return;
    const config = state.drag && state.drag.working ? state.drag.working : map.config;
    for (const lane of config.network.Lanes) if (map.paired.has(lane.ID) && map.lanes.has(lane.ID)) drawLane(map.lanes.get(lane.ID), config, lane, map);
  }

  // renderNodeLabels draws the junction ID labels that nodeLabelSize allows at
  // the current scale. setView calls it again when the scale changes. If the
  // scale does not cross NODE_LABEL_SCALE, the same labels show, and only a
  // selected label can change its font size.
  function renderNodeLabels() {
    const map = state.map; const scale = state.view.scale;
    const size = (id) => nodeLabelSize({ scale, id, selection: state.selection, linkFrom: state.linkFrom });
    const sameLabels = map.labelScale !== null && (map.labelScale >= NODE_LABEL_SCALE) === (scale >= NODE_LABEL_SCALE);
    map.labelScale = scale;
    if (sameLabels) {
      for (const id of [state.linkFrom, state.selection && state.selection.id]) { const label = map.labels.get(id); if (label) label.setAttribute("font-size", size(id)); }
      return;
    }
    const layer = $("#labelLayer"); layer.replaceChildren(); map.labels.clear();
    for (const node of map.junctions) {
      const fontSize = size(node.ID);
      if (!fontSize) continue;
      const label = svgElement("text", { class: "node-label", ...placeNodeLabel(node.Position), dx: "1em", dy: "-1em", "font-size": fontSize }); label.textContent = node.ID;
      layer.append(label); map.labels.set(node.ID, label);
    }
    // A zoom during a drag draws the labels again. Put the moved labels at
    // their drag positions.
    if (state.drag && state.drag.working) drawDragTargets(state.drag.working, state.drag.targets);
  }

  // drawDragTargets moves the drawn targets of a drag to their positions in
  // the working copy of the draft. The other map items stay as they are. A
  // station target also turns its entry and exit markers to the new angle
  // of its shape.
  function drawDragTargets(config, targets) {
    const map = state.map;
    for (const id of targets.nodeIDs) {
      const node = nodeFor(config, id); const circle = map.nodes.get(id); const label = map.labels.get(id);
      if (node && circle && !map.markers.has(id)) setAttributes(circle, placeNode(node.Position));
      if (node && label) setAttributes(label, placeNodeLabel(node.Position));
    }
    for (const id of targets.laneIDs) {
      const lane = config.network.Lanes.find((item) => item.ID === id); const path = map.lanes.get(id);
      if (!lane) continue;
      if (path) drawLane(path, config, lane, map);
      if (map.handles && map.handles.laneID === id && lane.Control) {
        const place = placeControl(config, lane);
        setAttributes(map.handles.line, place.line); setAttributes(map.handles.handle, place.handle);
      }
    }
    for (const id of targets.stationIDs) {
      const station = config.network.Stations.find((item) => item.ID === id); const drawn = map.stations.get(id);
      if (!station || !drawn) continue;
      const at = (nodeID) => nodeFor(config, nodeID)?.Position;
      const place = placeStation(at, station, map.stationNodes.get(id) || []);
      setAttributes(drawn.shape, place.shape); setAttributes(drawn.label, place.label);
      for (const nodeID of [station.Entry, station.Exit]) {
        const position = at(nodeID); const marker = map.nodes.get(nodeID);
        if (position && marker && map.markers.has(nodeID)) setAttributes(marker, placeMarker(position, place.angle));
      }
    }
  }

  // SELECTION_FORMS gives the Selection panel fields for each type of item.
  // renderSelection puts the item data into the data-field elements and the
  // inputs.
  const SELECTION_FORMS = {
    station: '<label>Name<input data-edit="station-name" maxlength="80"></label><p class="id" data-field="id"></p><label class="check"><input data-edit="parking-only" type="checkbox"> Parking station</label><label>Bearing (degrees)<input data-edit="station-bearing" type="number" step="1"></label><p class="hint">The bearing is the direction from the entry to the exit, clockwise from up. The square marks the entry, and the triangle marks the exit. Drag the shape to move the station, or drag a node to move only that node.</p><label data-field="bank-selection" hidden>Bank<select data-edit="station-bank"></select></label><fieldset data-field="layout"><legend>Station layout</legend><label>Berth pitch (m)<input data-layout="pitch" type="number" min="25" step="1"></label><label>Entry/exit spacing (m)<input data-layout="spacing" type="number" min="48" step="1"></label><label>Approach setback (m)<input data-layout="setback" type="number" min="1" step="1"></label><label data-field="approach-length" hidden>Approach length (m)<input data-layout="approachLength" type="number" min="24" step="1"></label><label data-field="departure-length" hidden>Departure length (m)<input data-layout="departureLength" type="number" min="24" step="1"></label><p class="hint" data-field="layout-hint"></p><button data-action="station-layout" type="button">Preview in draft</button></fieldset><details><summary>Bank membership</summary><label>Complete bank array<textarea data-edit="station-banks" rows="5" spellcheck="false"></textarea></label><p class="hint">Import or edit the physical nodes and lanes first. Each bank needs distinct gates and its own berth paths.</p><button data-action="station-banks" type="button">Preview bank membership</button><button data-action="station-legacy" type="button">Use legacy station gates</button></details><div class="berth-list" data-field="berths"><strong>Physical berths</strong></div><button data-action="add-berth" type="button">Add physical berth</button><button data-action="delete-station" class="danger" type="button">Delete station and connections</button>',
    lane: '<p class="id" data-field="id"></p><p data-field="route"></p><label>Speed limit (km/h)<input data-edit="lane-speed" type="number" min="1" step="1"></label><p class="hint" data-field="length"></p><button data-action="toggle-curve" type="button"></button><button data-action="delete-lane" class="danger" type="button">Delete guideway</button>',
    node: '<p class="id" data-field="id"></p><p data-field="position"></p><button data-action="delete-node" class="danger" type="button">Delete junction and connections</button>',
  };

  // renderSelection shows the fields of the selected station, lane or
  // junction. An arrow key in Speed limit or a name edit changes the draft,
  // and each draft change renders the page. While the same item stays
  // selected, renderSelection changes the fields in place. Then a field
  // keeps the keyboard focus and its caret, and Tab after an edit goes to
  // the next field.
  function renderSelection() {
    const panel = $("#selectionContent"); const card = selectionCard(draft(), state.selection);
    if (!card) { clearSelectionInputs(panel); state.selection = null; delete panel.dataset.card; panel.className = "empty"; panel.textContent = "No item selected."; return; }
    const key = `${card.type} ${card.id}`;
    if (panel.dataset.card !== key) { clearSelectionInputs(panel); panel.dataset.card = key; panel.layoutNetwork = null; panel.className = "selection-card"; panel.innerHTML = SELECTION_FORMS[card.type]; }
    const field = (name) => panel.querySelector(`[data-field="${name}"]`); const input = (name) => panel.querySelector(`[data-edit="${name}"]`);
    // Set only a changed value. This keeps the caret in a focused field.
    const setValue = setControlValue;
    field("id").textContent = card.id;
    if (card.type === "station") {
      setValue(input("station-name"), card.name); setValue(input("station-bearing"), String(card.bearing)); input("parking-only").checked = card.parkingOnly; renderBerths(field("berths"), card); renderStationLayout(panel, draft(), card.id);
    } else if (card.type === "lane") {
      field("route").textContent = `${card.from} → ${card.to}`; setValue(input("lane-speed"), String(card.speed));
      field("length").textContent = `Length: ${card.length.toFixed(1)} m`;
      panel.querySelector('[data-action="toggle-curve"]').textContent = card.curved ? "Make straight" : "Add curve";
    } else field("position").textContent = `Junction at ${card.x.toFixed(1)}, ${card.y.toFixed(1)} m`;
    for (const control of panel.querySelectorAll("input, button, select, textarea")) { control.dataset.item = card.id; control.dataset.kind = card.type; }
  }
  function clearSelectionInputs(panel) {
    for (const input of panel.querySelectorAll("input, textarea")) { typingInputs.delete(input); pendingInputs.delete(input); }
  }

  function renderStationLayout(panel, config, stationID) {
    const station = config.network.Stations.find((item) => item.ID === stationID);
    const bankID = selectedBank(station, state.selection)?.ID || "";
    if (panel.layoutNetwork === config.network && panel.layoutBank === bankID && panel.layoutStation === station.ID) return;
    panel.layoutNetwork = config.network; panel.layoutBank = bankID;
    const select = panel.querySelector('[data-edit="station-bank"]');
    panel.querySelector('[data-field="bank-selection"]').hidden = !station.Banks;
    select.replaceChildren(...(station.Banks || []).map((bank) => { const option = document.createElement("option"); option.value = bank.ID; option.textContent = bank.ID; return option; }));
    select.value = bankID;
    const membership = panel.querySelector('[data-edit="station-banks"]');
    setControlValue(membership, JSON.stringify(station.Banks || [], null, 2));
    panel.querySelector('[data-action="station-legacy"]').disabled = !Object.hasOwn(station, "Banks");
    for (const field of ["approach-length", "departure-length"]) panel.querySelector(`[data-field="${field}"]`).hidden = !station.Banks;
    panel.layoutStation = stationID;
    for (const input of panel.querySelectorAll("input[data-layout]")) { input.disabled = true; setControlValue(input, ""); }
    panel.querySelector('[data-action="station-layout"]').disabled = true;
    panel.querySelector('[data-field="layout-hint"]').textContent = "Inspecting station layout.";
    const current = () => panel.isConnected && draft().network === config.network && state.selection?.id === stationID && (selectedBank(station, state.selection)?.ID || "") === bankID && panel.layoutNetwork === config.network && panel.layoutBank === bankID;
    goModel.call(config, "stationLayout", { stationID, ...(bankID ? { bankID } : {}) }).then((result) => {
      if (!current()) return;
      if (result.error) throw new Error(result.error);
      const reasons = new Set();
      for (const [key, field] of Object.entries(result.layout)) {
        const input = panel.querySelector(`[data-layout="${key}"]`);
        input.disabled = field.value === null;
        const value = input.disabled ? "" : String(Number(field.value.toFixed(3)));
        setControlValue(input, value); input.dataset.layoutValue = value;
        if (field.reason && (!["approachLength", "departureLength"].includes(key) || station.Banks)) reasons.add(field.reason);
      }
      panel.querySelector('[data-field="layout-hint"]').textContent = [...reasons].join(" ") || "Preview updates the draft map in one undo step. Apply the project to change the simulation.";
      panel.querySelector('[data-action="station-layout"]').disabled = [...panel.querySelectorAll("input[data-layout]")].every((input) => input.disabled);
    }).catch((error) => { if (current()) { panel.layoutNetwork = null; panel.querySelector('[data-field="layout-hint"]').textContent = error.message; } });
  }

  // renderBerths shows a row with a Remove button for each berth of the
  // selected station, and marks the berth that the selection names. When the
  // station has the same berths, it changes the rows in place.
  function renderBerths(list, card) {
    let rows = [...list.querySelectorAll(".berth-row")];
    if (rows.length !== card.berths.length || card.berths.some((berth, index) => rows[index].dataset.berth !== berth.id)) {
      rows.forEach((row) => row.remove());
      rows = card.berths.map((berth) => {
        const row = document.createElement("div"); row.className = "berth-row"; row.dataset.berth = berth.id;
        const name = document.createElement("span"); name.textContent = berth.id;
        const button = document.createElement("button"); button.type = "button"; button.dataset.action = "remove-berth"; button.dataset.id = berth.id; button.textContent = "Remove";
        row.append(name, button); return row;
      });
      list.append(...rows);
    }
    card.berths.forEach((berth, index) => { rows[index].classList.toggle("selected", berth.selected); rows[index].querySelector("button").disabled = !card.canRemove; });
  }

  // focusBerthControl gives the keyboard focus to the Remove button of the
  // berth row that berthFocusID gives. An empty ID gives the focus to Add
  // physical berth.
  function focusBerthControl(berthID) {
    const panel = $("#selectionContent"); const row = [...panel.querySelectorAll(".berth-row")].find((item) => item.dataset.berth === berthID);
    (row ? row.querySelector("button") : panel.querySelector('[data-action="add-berth"]'))?.focus();
  }

  // focusMap gives the keyboard focus to the map after the delete of a
  // station, lane or junction, and after an undo or a redo that removes the
  // selected item. A render does not replace the map element, and the
  // Delete, Escape and undo keys work there. In a narrow window the map is
  // above the Selection panel, so the page scrolls until the map is in view.
  function focusMap() {
    $("#networkMap").focus({ preventScroll: true }); $(".map-panel").scrollIntoView({ block: "nearest" });
  }

  // stepHistory does an undo, or a redo when redo is true. The selection
  // and the keyboard focus then change as undoFocus gives. A Selection panel
  // button that stays after the render keeps the focus. A button that the
  // render made again gets the focus back.
  function stepHistory(redo) {
    model.abort();
    editQueue.submit(async (current) => { if (current()) await stepHistoryNow(redo, current); return current(); }).catch((error) => toast(error.message, true));
  }
  async function stepHistoryNow(redo, current) {
    model.abort();
    const panel = $("#selectionContent"); const focused = document.activeElement; const before = draft();
    const control = panel.contains(focused) ? { action: focused.dataset.action || "", id: focused.dataset.id || "" } : null;
    const options = { current, beforePublish: (changed) => {
      if (!changed) return;
      const ownsFocus = document.activeElement === focused;
      const next = undoFocus({ before, after: draft(), selection: state.selection, control: ownsFocus ? control : null });
      state.selection = next.selection; render();
      if (!ownsFocus) return;
      if (next.focus === "map") focusMap();
      else if (next.focus) [...panel.querySelectorAll("button[data-action]")].find((button) => button.dataset.action === next.focus.action && (button.dataset.id || "") === next.focus.id)?.focus();
    } };
    await (redo ? state.history.redo(options) : state.history.undo(options));
  }

  // renderFleet shows a pod count field for each station. An arrow key in a
  // field changes the draft, and each draft change renders the page. When
  // the fields are for the same stations in the same order, renderFleet
  // changes them in place, so that the field keeps the keyboard focus.
  function renderFleet() {
    const rows = fleetRows(draft()); const parent = $("#fleetControls");
    if (!rows.length) { parent.innerHTML = '<p class="empty">Add a station to place pods.</p>'; return; }
    const current = [...parent.querySelectorAll(".fleet-row")];
    if (current.length !== rows.length || rows.some((row, index) => current[index].dataset.station !== row.id)) {
      parent.replaceChildren(...rows.map((row) => {
        const element = document.createElement("label"); element.className = "fleet-row"; element.dataset.station = row.id;
        const input = document.createElement("input"); input.type = "number"; input.min = "0"; input.dataset.station = row.id;
        element.append(document.createElement("span"), input); return element;
      }));
    }
    rows.forEach((row, index) => {
      const [name, input] = parent.children[index].children;
      name.textContent = row.name; input.max = String(row.max); input.setAttribute("aria-label", `Initial pods at ${row.name}`);
      // Set only a changed value. This keeps the caret in a focused field.
      setControlValue(input, row.count);
    });
    let notice = parent.querySelector("[data-fleet-class-notice]");
    const message = fleetClassNotice(draft());
    if (!message) { notice?.remove(); return; }
    if (!notice) { notice = document.createElement("p"); notice.className = "hint"; notice.dataset.fleetClassNotice = "true"; parent.append(notice); }
    notice.textContent = message;
  }

  const drawnRailPlans = new Map();
  const railEpochs = new Map();
  const observedRailLayouts = new Map();
  const railStructuralEdits = new Set();
  function railEpoch(kind) { return railEpochs.get(kind) || 0; }
  function railRowsID(kind) { return kind === "departure" ? "#railDepartureRows" : "#railArrivalRows"; }
  function railBusy(kind) { return [...railStructuralEdits].some((edit) => edit.kind === kind); }
  function renderRailLocks(kind) {
    const parent = $(railRowsID(kind)), busy = railBusy(kind);
    for (const control of parent.querySelectorAll("input, select, button")) {
      control.disabled = busy || control.dataset.railDisabled === "true";
    }
    const config = draft();
    $(kind === "departure" ? "#addRailDeparture" : "#addRailArrival").disabled = busy || (config.railArrivals || []).length + (config.railDepartures || []).length >= 256 || config.network.Stations.filter((station) => !station.ParkingOnly).length < 2;
  }
  function railSummary(event, passenger, departure) {
    const hub = passenger.find((station) => station.ID === event.station);
    return `${event.id}: ${hub?.Name || event.station}, ${event.passengers} passengers ${departure ? "departing" : "released"} at ${departure ? event.atSeconds : event.atSeconds + event.walkingSeconds}s`;
  }
  function renderRailArrivals(config, kind = "arrival") {
    const departure = kind === "departure", key = departure ? "railDepartures" : "railArrivals", choices = departure ? "origins" : "destinations";
    const rowsID = departure ? "#railDepartureRows" : "#railArrivalRows", addID = departure ? "#addRailDeparture" : "#addRailArrival";
    const parent = $(rowsID); const arrivals = config[key] || [];
    $(addID).disabled = (config.railArrivals || []).length + (config.railDepartures || []).length >= 256 || config.network.Stations.filter((station) => !station.ParkingOnly).length < 2;
    const drawn = drawnRailPlans.get(kind);
    const passenger = config.network.Stations.filter((station) => !station.ParkingOnly);
    const layout = JSON.stringify([passenger.map((station) => station.ID), arrivals.map((event) => [event.id, event[choices].length])]);
    const observed = observedRailLayouts.get(kind);
    if (observed !== undefined && observed !== layout) railEpochs.set(kind, railEpoch(kind) + 1);
    observedRailLayouts.set(kind, layout);
    if (drawn?.plan === config[key] && drawn?.stations === config.network.Stations && drawn?.epoch === railEpoch(kind)) { renderRailLocks(kind); return; }
    // Keep the same controls for field edits, including unreadable partial numbers.
    if (drawn?.layout === layout && drawn?.epoch === railEpoch(kind)) {
      for (const row of parent.children) {
        const arrival = arrivals.find((item) => item.id === row.dataset.railID);
        row.querySelector("summary").textContent = railSummary(arrival, passenger, departure);
        for (const control of row.querySelectorAll("[data-rail-field]")) {
          const { railField, railDestination } = control.dataset;
          const item = railDestination === undefined ? arrival : arrival[choices][Number(railDestination)];
          if (control.tagName === "SELECT") for (const option of control.options) option.textContent = passenger.find((station) => station.ID === option.value).Name;
          setControlValue(control, item[railField]);
          control.dataset.railEpoch = String(railEpoch(kind));
        }
        for (const control of row.querySelectorAll("[data-rail-action]")) control.dataset.railEpoch = String(railEpoch(kind));
      }
      drawnRailPlans.set(kind, { plan: config[key], stations: config.network.Stations, layout, epoch: railEpoch(kind) });
      renderRailLocks(kind); return;
    }
    if ([...typingInputs].some((input) => parent.contains(input))) { renderRailLocks(kind); return; }
    const active = parent.contains(document.activeElement) ? document.activeElement : null;
    const focus = active ? { ...active.dataset } : null;
    if (focus) delete focus.railEpoch;
    const opened = new Set([...parent.querySelectorAll("details[open]")].map((item) => item.dataset.railID));
    const field = (text, control) => { const label = document.createElement("label"); label.append(document.createTextNode(text), control); return label; };
    const stationSelect = (value, id, name, destination) => {
      const select = document.createElement("select"); select.dataset.railID = id; select.dataset.railField = name; select.dataset.railEpoch = String(railEpoch(kind));
      if (destination !== undefined) select.dataset.railDestination = String(destination);
      for (const station of passenger) { const option = document.createElement("option"); option.value = station.ID; option.textContent = station.Name; select.append(option); }
      select.value = value;
      return select;
    };
    const number = (value, id, name, min, max, destination) => {
      const input = document.createElement("input"); input.type = "number"; input.min = String(min); input.max = String(max); input.step = "1"; input.value = String(value); input.dataset.railID = id; input.dataset.railField = name; input.dataset.railEpoch = String(railEpoch(kind));
      if (destination !== undefined) input.dataset.railDestination = String(destination);
      return input;
    };
    const button = (text, action, id, destination) => {
      const item = document.createElement("button"); item.type = "button"; item.textContent = text; item.dataset.railAction = action; item.dataset.railID = id; item.dataset.railEpoch = String(railEpoch(kind));
      if (destination !== undefined) item.dataset.railDestination = String(destination);
      return item;
    };
    parent.replaceChildren();
    for (const arrival of arrivals) {
      const row = document.createElement("details"); row.dataset.railID = arrival.id; row.open = opened.has(arrival.id) || arrivals.length === 1;
      const title = document.createElement("summary"); title.textContent = railSummary(arrival, passenger, departure); row.append(title);
      row.append(field("Rail hub", stationSelect(arrival.station, arrival.id, "station")));
      row.append(field(`${departure ? "Departure" : "Arrival"} after reset (seconds)`, number(arrival.atSeconds, arrival.id, "atSeconds", departure ? 1 : 0, 86400)));
      if (departure) {
        row.append(field("Request window start (seconds)", number(arrival.requestFromSeconds, arrival.id, "requestFromSeconds", 0, 86399)));
        row.append(field("Request window end (seconds)", number(arrival.requestUntilSeconds, arrival.id, "requestUntilSeconds", 0, 86399)));
      }
      row.append(field(departure ? "Transfer after alighting (seconds)" : "Walking delay (seconds)", number(arrival.walkingSeconds, arrival.id, "walkingSeconds", 0, 3600)));
      row.append(field("Passengers", number(arrival.passengers, arrival.id, "passengers", 1, 200)));
      arrival[choices].forEach((destination, index) => {
        const group = document.createElement("div"); group.className = "subpanel";
        group.append(field(`${departure ? "Origin" : "Destination"} ${index + 1}`, stationSelect(destination.station, arrival.id, "station", index)));
        group.append(field("Weight", number(destination.weight, arrival.id, "weight", 1, 1000000, index)));
        const remove = button(departure ? "Remove origin" : "Remove destination", "remove-destination", arrival.id, index); remove.dataset.railDisabled = String(arrival[choices].length <= 1); group.append(remove); row.append(group);
      });
      const add = button(departure ? "Add origin" : "Add destination", "add-destination", arrival.id); add.dataset.railDisabled = String(arrival[choices].length >= 16); row.append(add, button(departure ? "Remove departure" : "Remove arrival", "remove-arrival", arrival.id));
      parent.append(row);
    }
    drawnRailPlans.set(kind, { plan: config[key], stations: config.network.Stations, layout, epoch: railEpoch(kind) });
    renderRailLocks(kind);
    if (focus) ([...parent.querySelectorAll("input, select, button")].find((item) => Object.keys(focus).every((key) => item.dataset[key] === focus[key])) || $(addID)).focus({ preventScroll: true });
  }

  function renderDemand() {
    const config = draft(); const demand = config.demand;
    $("#demandEnabled").checked = demand.enabled; setScalarValue("#demandRate", demand.perMinute); setScalarValue("#demandPattern", demand.pattern); setScalarValue("#demandSeed", demand.seed); $("#redistribution").checked = config.redistribution;
    const select = $("#demandDestination"); select.replaceChildren();
    const passenger = config.network.Stations.filter((station) => !station.ParkingOnly);
    for (const station of passenger) { const option = document.createElement("option"); option.value = station.ID; option.textContent = station.Name; select.append(option); }
    select.value = draft().demand.destination; $("#destinationLabel").hidden = demand.pattern !== "destination";
    const profiles = config.demandProfiles || []; const profileSelect = $("#demandProfile"); profileSelect.replaceChildren();
    for (const profile of profiles) { const option = document.createElement("option"); option.value = profile.id; option.textContent = profile.name; profileSelect.append(option); }
    profileSelect.value = demand.profile;
    const profile = profiles.find((item) => item.id === demand.profile); const bandSelect = $("#demandBand"); bandSelect.replaceChildren();
    for (const band of profile?.bands || []) { const option = document.createElement("option"); option.value = band.id; option.textContent = band.name; bandSelect.append(option); }
    bandSelect.value = demand.band; $("#profileLabel").hidden = !["profile", "profile-daily"].includes(demand.pattern); $("#bandLabel").hidden = demand.pattern !== "profile";
    $("#demandPattern").querySelector('option[value="profile"]').disabled = profiles.length === 0;
    $("#demandPattern").querySelector('option[value="profile-daily"]').disabled = profiles.length === 0;
    $("#dailyClockLabel").hidden = $("#dailyClockHint").hidden = demand.pattern !== "profile-daily";
    const dailyMinute = demand.dailyStartMinute || 0;
    setScalarValue("#dailyStartTime", `${String(Math.floor(dailyMinute / 60)).padStart(2, "0")}:${String(dailyMinute % 60).padStart(2, "0")}`);
    renderParkRide(config, passenger);
    $("#demandPattern").querySelector('option[value="rail-arrivals"]').disabled = !(config.railArrivals || []).length;
    $("#demandPattern").querySelector('option[value="rail-services"]').disabled = !(config.railArrivals || []).length && !(config.railDepartures || []).length;
    $("#demandRate").disabled = ["rail-arrivals", "rail-services"].includes(demand.pattern);
    setScalarValue("#sharedRidePartyLimit", config.sharedRidePartyLimit);
    setScalarValue("#sharedRideMode", config.sharedRideMode);
    setScalarValue("#sharedRideJoin", config.sharedRideJoin);
    setScalarValue("#sharedRideMaxStops", config.sharedRideMaxStops);
    $("#sharedRideMaxStopsLabel").hidden = config.sharedRideMode !== "drop-offs";
    setScalarValue("#platoonLimit", String(config.platoonLimit));
    $("#stationBuffers").checked = config.stationBuffers;
    setScalarValue("#stationQueueSpacing", config.stationQueueSpacing || "ordinary");
    $("#pickupReassignment").checked = config.pickupReassignment;
  }

  let parkRideStations = null, parkRideBusy = false;
  function renderParkRide(config, passenger) {
    $("#parkRideCreate").disabled = parkRideBusy || (config.demandProfiles || []).length >= 8;
    $("#parkRideAddDestination").disabled = passenger.length < 2;
    if (parkRideStations === config.network.Stations) return;
    parkRideStations = config.network.Stations;
    for (const selector of ["#parkRideHub", "#parkRideDestination"]) {
      const select = $(selector), value = select.value;
      select.replaceChildren(...passenger.filter((station) => selector !== "#parkRideDestination" || station.ID !== $("#parkRideHub").value).map((station) => Object.assign(document.createElement("option"), { value: station.ID, textContent: station.Name })));
      if ([...select.options].some((option) => option.value === value)) select.value = value;
    }
  }

  function addParkRideDestination() {
    const select = $("#parkRideDestination");
    const row = document.createElement("label"); row.className = "fleet-row"; row.dataset.station = select.value;
    const name = document.createElement("span"); name.textContent = select.selectedOptions[0]?.textContent || "Destination";
    const input = document.createElement("input"); input.type = "number"; input.min = "0"; input.step = "any"; input.value = "1"; input.required = true; input.setAttribute("aria-label", `Weight for ${name.textContent}`);
    const remove = document.createElement("button"); remove.type = "button"; remove.textContent = "Remove"; remove.addEventListener("click", () => row.remove());
    row.append(name, input, remove); $("#parkRideDestinations").append(row);
  }

  const inputMinute = (selector) => { const [hours, minutes] = $(selector).value.split(":").map(Number); return hours * 60 + minutes; };
  async function createParkRideProfile() {
    if (parkRideBusy) return;
    for (const input of $("#parkRideForm").querySelectorAll("input, select")) if (!input.reportValidity()) return;
    const inputRevision = editRevision(), status = $("#parkRideStatus");
    const band = (name) => ({ startMinute: inputMinute(`#parkRide${name}Start`), durationMinutes: $(`#parkRide${name}Duration`).valueAsNumber, perMinute: $(`#parkRide${name}Rate`).valueAsNumber });
    const plan = { name: $("#parkRideName").value, hub: $("#parkRideHub").value,
      destinations: [...$("#parkRideDestinations").children].map((row) => ({ station: row.dataset.station, weight: row.querySelector("input").valueAsNumber })),
      morning: band("Morning"), evening: band("Evening"), dailyStartMinute: inputMinute("#parkRideStartTime") };
    parkRideBusy = true; renderParkRide(draft(), draft().network.Stations.filter((station) => !station.ParkingOnly)); status.textContent = "Creating profile…";
    try {
      if (!await editQueue.flush() || inputRevision !== editRevision()) throw new Error("The settings changed. Create the profile again.");
      const config = draft(), editGeneration = model.edits;
      const result = await goModel.call(config, "park-ride", plan);
      if (result.error) throw new Error(result.error);
      if (config !== draft() || editGeneration !== model.edits || inputRevision !== editRevision() || state.drag && state.drag.type !== "pan" || model.gestureOpen) throw new Error("The draft changed or a gesture is open. Finish the gesture and create the profile again.");
      const current = () => config === draft() && editGeneration === model.edits && inputRevision === editRevision();
      if (!await setDraft({ ...config, demandProfiles: [...(config.demandProfiles || []), result.profile], demand: result.demand }, true, current)) throw new Error("The settings changed. Create the profile again.");
      status.textContent = `Created ${result.profile.name}. Pause and apply to use it.`;
    } catch (error) { status.textContent = error.message; toast(error.message, true); }
    finally { parkRideBusy = false; renderParkRide(draft(), draft().network.Stations.filter((station) => !station.ParkingOnly)); }
  }

  let drawnNetwork = null, drawnBackground = "", drawnSelection = "";
  function render() {
    // A render draws the draft from the history. It ends a drag of a working
    // copy, so that a pointer up cannot record that copy over a newer draft.
    if (state.drag && state.drag.working) { state.drag = null; model.endDrag(); drawnNetwork = null; }
    state.background = state.history.background;
    const config = draft(); setScalarValue("#scenarioName", config.name); $("#backgroundOpacity").value = pendingInputs.get($("#backgroundOpacity"))?.value ?? (state.background ? state.background.opacity : .45); $("#opacityValue").value = `${Math.round(Number($("#backgroundOpacity").value) * 100)}%`;
    renderHistoryButtons();
    $("#networkMap").dataset.tool = state.tool; $("#cancelLinkButton").hidden = !state.linkFrom;
    renderTools();
    const backgroundKey = JSON.stringify(state.background), selectionKey = JSON.stringify([state.selection, state.linkFrom, state.tool, state.calibrating, state.calibrationPoints]);
    if (config.network !== drawnNetwork || backgroundKey !== drawnBackground || selectionKey !== drawnSelection) {
      renderMap(); drawnNetwork = config.network; drawnBackground = backgroundKey; drawnSelection = selectionKey;
    } else { renderTiles(); }
    renderSelection(); renderFleet(); renderDemand(); renderRailArrivals(config); renderRailArrivals(config, "departure"); updatePrompt(); renderBackground(); renderApply();
    restorePendingInputs();
  }
  function restorePendingInputs() {
    for (const [input, pending] of pendingInputs) if (input.isConnected && !typingInputs.has(input)) {
      if (input.type === "checkbox") input.checked = pending.value;
      else if (input.value !== pending.value) input.value = pending.value;
    }
  }
  function setScalarValue(selector, value) {
    setControlValue($(selector), value);
  }
  function setControlValue(input, value) {
    // A number field can hold partial text that its value getter cannot read.
    if (!typingInputs.has(input) && input.value !== String(value)) input.value = String(value);
  }

  // displayURL gives the object URL of the image of background for the
  // map, or an empty text with no background. It makes a new Blob and URL
  // only when the image key changes. renderMap revokes the old URL after
  // the image element changes.
  function displayURL(background) {
    const key = background ? background.imageKey : "";
    if (key === display.key) return display.url;
    const image = key ? model.image(key) : null;
    display = { key, url: image ? URL.createObjectURL(new Blob([image.bytes], { type: image.mime })) : "" };
    return display.url;
  }

  // renderBackground shows the frame controls, the mismatch warning, the
  // reference choice, the calibration state, the attribution line and the
  // facts of the Background panel for the present background. The
  // warning is computed at each render, so no event sets or clears it.
  function renderBackground() {
    const value = { scenario: draft(), background: state.background };
    const image = state.background ? model.image(state.background.imageKey) : null;
    const view = frameView(value, image);
    $("#frameControls").hidden = !view.framed;
    $("#frameStateText").textContent = view.framed ? `The image has a frame from ${image.frame.south} to ${image.frame.north} degrees north and from ${image.frame.west} to ${image.frame.east} degrees east. The frame is ${view.state}.` : "";
    $("#frameWarning").hidden = !view.warning; $("#frameWarning").textContent = view.warning;
    const place = $("#placeFrameButton");
    place.disabled = Boolean(view.placeReason) && !$("#referenceMode").value; place.title = place.disabled ? view.placeReason : "";
    $("#detachFrameButton").disabled = view.state !== "attached";
    const calibrate = $("#calibrateButton"); calibrate.disabled = Boolean(view.calibrateReason); calibrate.title = view.calibrateReason;
    const unreferenced = !value.scenario.geo && value.scenario.network.Nodes.length > 0;
    $("#referencePanel").hidden = !(unreferenced && (state.geoOpen || state.mapOpen || view.framed));
    $("#referenceMode").querySelector('[value="adopt"]').textContent = state.mapOpen ? "Adopt the map origin" : "Adopt the image center";
    $("#adoptText").textContent = state.mapOpen ? "Use the entered map origin without moving the network. The map and network can fail to align." : "The image center becomes the reference, and the network does not move. The network and the image can then fail to align.";
    const mode = $("#referenceMode").value; $("#anchorFields").hidden = mode !== "anchor"; $("#adoptField").hidden = mode !== "adopt";
    $("#geoPanel").hidden = !state.geoOpen;
    $("#tilePanel").hidden = !state.mapOpen;
    $("#mapRemoveButton").disabled = !value.scenario.map;
    const mapOpacity = $("#mapOpacity");
    mapOpacity.value = pendingInputs.get(mapOpacity)?.value ?? value.scenario.map?.opacity ?? .45;
    $("#mapOpacityValue").value = `${Math.round(Number(mapOpacity.value) * 100)}%`;
    $("#mapOriginFields").hidden = !!value.scenario.geo;
    $("#mapReferenceText").textContent = value.scenario.geo ? `Map origin: ${value.scenario.geo.latitude}, ${value.scenario.geo.longitude}.` : "Set the latitude and longitude of world position 0, 0, or anchor two existing nodes.";
    const line = $("#mapAttribution"); const parts = attributionParts(image && image.license);
    line.replaceChildren(...parts.flatMap((part, index) => {
      const node = part.href ? Object.assign(document.createElement("a"), { href: part.href, target: "_blank", rel: "noopener noreferrer" }) : document.createElement("span");
      node.textContent = part.text;
      return index ? [document.createTextNode(" | "), node] : [node];
    }));
    line.hidden = !parts.length;
    renderBackgroundInfo();
    placeSearch.refresh();
  }

  // renderBackgroundInfo shows the facts of backgroundFacts in the
  // Background panel.
  function renderBackgroundInfo() {
    const image = state.background ? model.image(state.background.imageKey) : null;
    const lines = backgroundFacts({ image, held: model.bytes, keeperBytes: model.keeperBytes, cap: model.cap, storedBytes, restoreFailed: state.restoreFailed });
    $("#backgroundInfo").replaceChildren(...lines.map((text) => Object.assign(document.createElement("li"), { textContent: text })));
  }

  // renderApply enables Pause and apply only when the draft scenario is
  // not the live scenario and no apply runs. A change of only the
  // background does not enable it, because the server does not get the
  // background. It also disables the apply conflict actions while an
  // apply or a conflict action runs.
  function renderApply() {
    const changed = Boolean(state.live) && !sameDraft(draft(), state.live.snapshot);
    const button = $("#applyButton"); button.disabled = Boolean(state.history.failed) || state.applying || !changed && !editQueue.pending && !state.history.pending;
    button.title = changed || state.applying || editQueue.pending ? "" : "The draft has no changes to apply.";
    $("#loadLiveButton").disabled = state.applying || Boolean(state.history.failed); $("#applyOverButton").disabled = state.applying || Boolean(state.history.failed);
  }

  // setLive keeps the scenario of value as the live baseline, with the live
  // project revision.
  function setLive(value, revision) {
    state.live = { scenario: JSON.stringify(value.scenario), snapshot: ownDraft(value.scenario, draft()), revision };
    renderApply();
  }

  // draftDatabase gives the indexedDB object of the browser, or null. The
  // browser can block the access, for example on a sandboxed page.
  function draftDatabase() { try { return root.indexedDB || null; } catch (_) { return null; } }

  // draftChannel gives the channel that tells the other editor tabs of each
  // draft save, or null when the browser has no BroadcastChannel.
  function draftChannel() { try { return typeof root.BroadcastChannel === "function" ? new root.BroadcastChannel("podsim-editor-drafts") : null; } catch (_) { return null; } }

  // draftRecord gives the record that the keeper saves for the draft of
  // this page, as draftRecordFor gives it with state.draftBase.
  function draftRecord() { return draftRecordFor(state.history.value, state.live, state.draftBase); }

  // backgroundRecord gives the record that the background keeper saves for
  // the background of this page, as backgroundRecordFor gives it with the
  // image descriptor of the model. The record holds the bytes, so a later
  // prune of the model does not take them from a queued write.
  function backgroundRecord() {
    const background = model.history.background;
    return background ? backgroundRecordFor(background, model.image(background.imageKey)) : null;
  }

  // showDraftStatus tells the user when the status of the saved draft
  // changes. A failure also shows the error toast.
  function showDraftStatus(status) {
    const live = state.live && state.live.revision !== null ? `Live revision ${state.live.revision}. ` : "";
    updateStatus(`${live}${DRAFT_STORE_TEXT[status]}`);
    if (status !== "ok") toast(DRAFT_STORE_TEXT[status], true);
  }

  // showBackgroundStatus shows the error toast when the browser does not
  // keep the background.
  function showBackgroundStatus(status) {
    if (status !== "ok") toast(BACKGROUND_STORE_TEXT[status], true);
  }

  // restoreBackground puts the stored background of record on the page, as
  // restoreStoredBackground does. The decoder slot decodes the bytes that
  // the store gave, as checkImageBytes does. The image keeps its stored
  // key. The background is part of the loaded project, so Reset draft
  // keeps it. The undo history starts again from the draft with the
  // background, so no undo step removes it. A record that is not valid
  // shows the error toast, and stays in the store until a background
  // change of the page. The Background panel then says so.
  function restoreBackground(record) {
    return restoreStoredBackground(record, {
      warn: (message) => { state.restoreFailed = true; toast(message, true); renderBackgroundInfo(); },
      metadata: metadataWithGo,
      decode: (restored) => slot.run((signal) => checkImageBytes(restored.image.bytes, decoder, signal)),
      install: async (restored) => {
        const image = freezeImage({ key: restored.background.imageKey, ...restored.image });
        await model.restore(image, restored.background); storedBytes = image.bytes.byteLength;
        render(); fitNetwork();
        console.info("podsim editor: restored background", { key: image.key, bytes: image.bytes.byteLength, width: image.pixelWidth, height: image.pixelHeight, frameState: restored.background.frameState });
      },
    });
  }

  // showDraftDisplaced tells the user that another editor tab replaced the
  // saved draft, when the draft of this tab has changes. Then the browser
  // also asks before you leave the page.
  function showDraftDisplaced() {
    if (!keeper.unsaved) return;
    const live = state.live && state.live.revision !== null ? `Live revision ${state.live.revision}. ` : "";
    updateStatus(`${live}${DRAFT_DISPLACED_TEXT}`); toast(DRAFT_DISPLACED_TEXT, true);
  }

  // offerDraft shows the Restore draft and Discard draft offer when the
  // saved record differs from the live baseline. The keeper does not save
  // the draft until the user selects one. With no offer, the keeper starts
  // to save the draft, and deletes a record with no changes.
  function offerDraft(record) {
    state.offer = draftOffer(record, state.live);
    if (!state.offer) { keeper.arm(); return; }
    renderOffer();
  }

  // renderOffer shows the saved draft offer while state.offer is set. The
  // text names the revision of the saved draft and the live revision. It
  // also tells the user when the draft is from before a server restart.
  // state.liveStart is the server start ID of the latest live read, or an
  // empty string. Each live read renders the offer again.
  function renderOffer() {
    $("#draftOffer").hidden = !state.offer;
    if (state.offer) $("#draftOfferText").textContent = draftOfferText({ revision: state.offer.revision, live: state.live.revision, serverStart: state.offer.serverStart, liveStart: state.liveStart });
  }

  // observeServerStart keeps start, the server start ID of a live read, in
  // state.liveStart. While the saved draft offer shows, it renders the
  // offer again, so the offer tells of a server restart.
  function observeServerStart(start) {
    state.liveStart = start;
    if (state.offer) renderOffer();
  }

  // refreshLiveStart reads the live state once while the saved draft offer
  // shows. The editor window gets the focus when the shell shows the
  // editor, and when the user goes back to the browser tab or window. Thus
  // the offer tells of a server restart after the page loaded. The read
  // gives the server start ID to observeServerStart. A failed read changes
  // nothing.
  async function refreshLiveStart() {
    if (!state.offer || state.refreshing) return;
    state.refreshing = true;
    try { await readState(state.connection); } catch (_) {} finally { state.refreshing = false; }
  }

  // closeOffer hides the saved draft offer. After a choice from the
  // keyboard, the map gets the focus, because the focused button hides.
  function closeOffer(event) {
    state.offer = null; renderOffer();
    if (event && event.detail === 0) focusMap();
  }

  // restoreDraft puts the saved draft back and starts a new undo history.
  // The page keeps its background, because the draft does not have it.
  // The draft keeps the revision, the epoch and the server start ID that it
  // started from, so a later offer names them. Pause and apply sends the
  // live revision of this page, so it replaces the live scenario. It also
  // sends the server start ID of the draft, so the server rejects a draft
  // from before a server restart. The draft is not the live baseline, so it
  // counts as changed.
  async function restoreDraft(event) {
    if (!state.offer) return;
    const offer = state.offer;
    cancelPendingEdits();
    const ticket = model.start(), inputRevision = editRevision();
    try {
      const { draft: saved, revision, epoch, serverStart } = offer;
      const scenario = await normalizeWithGo(saved.scenario);
      if (!model.current(ticket) || state.offer !== offer) return;
      const stale = model.publishError(ticket, null);
      if (stale || inputRevision !== editRevision() || state.drag && state.drag.type !== "pan" || model.gestureOpen) throw new Error(stale || "The draft changed during restoration. Restore it again.");
      const restored = await state.history.reset({ scenario, background: state.background }, {
        current: () => model.current(ticket) && state.offer === offer && inputRevision === editRevision(),
        beforePublish: () => { model.abort(); closeOffer(event); state.draftBase = { revision, epoch, serverStart }; state.selection = null; },
      });
      if (!restored) return;
      if (!(state.drag && state.drag.type !== "pan") && !model.gestureOpen) { render(); fitNetwork(); }
      checks.run(); keeper.arm();
      updateStatus(restoreStatusText({ revision, live: state.live.revision, serverStart, liveStart: state.liveStart }));
      toast("The saved draft is restored.");
    } catch (error) {
      if (!model.current(ticket)) return;
      model.abort(); toast(`${error.message} The draft is unchanged.`, true);
    }
  }

  // discardDraft replaces the saved draft with the current draft in one
  // write: a delete, or a put when the draft has changes.
  function discardDraft(event) {
    model.abort(); closeOffer(event); keeper.replace(); toast("The saved draft is discarded.");
  }

  // renderTools marks the button of the active tool as pressed. The
  // aria-pressed value tells screen readers which tool is active, and the
  // style sheet uses the same value.
  function renderTools() {
    document.querySelectorAll(".tool").forEach((button) => button.setAttribute("aria-pressed", String(button.dataset.tool === state.tool)));
  }

  function updatePrompt() {
    let message = "Select an item to edit it.";
    if (state.calibrating) message = state.calibrationPoints.length ? "Select the second calibration point." : "Select the first calibration point.";
    else if (state.tool === "junction") message = "Select the map to add a junction.";
    else if (state.tool === "station") message = "Select the map to add a station with one physical berth.";
    else if (state.tool === "lane") message = state.linkFrom ? "Select an end node. Press Escape to cancel." : "Select a start node. Crossings do not connect.";
    $("#mapPrompt").textContent = message;
  }

  function setTool(tool) {
    state.tool = tool; state.linkFrom = "";
    render();
  }

  let geometrySelectionEpoch = 0;
  function selectItem(type, id) { geometrySelectionEpoch++; state.selection = { type, id }; render(); }

  function beginDrag(type, id, event) {
    const drag = { type, id, startClient: { x: event.clientX, y: event.clientY }, originalView: { ...state.view } };
    if (type !== "pan") {
      // Commit a changed form field first. Its change event renders, and a
      // render ends a drag of a working copy.
      const field = document.activeElement;
      if (field && field.matches("input, select, textarea")) field.blur();
      // The drag changes a working copy of the draft and redraws only its
      // targets. Go computes the committed edit after the pointer up.
      drag.source = draft(); drag.working = clone(drag.source); drag.targets = dragTargets(drag.working, { type, id }); drag.moved = new Set(drag.targets.nodeIDs); drag.last = worldPoint(event); drag.delta = { X: 0, Y: 0 };
    }
    state.drag = drag;
    $("#networkMap").setPointerCapture(event.pointerId);
  }

  function onPointerDown(event) {
    if (event.button !== 0) return;
    const selectionEpoch = ++geometrySelectionEpoch;
    const target = event.target; const type = target.dataset && target.dataset.type; const id = target.dataset && target.dataset.id;
    if (state.calibrating) {
      if (!state.background) return;
      if (state.calibrationPoints.length < 2) state.calibrationPoints.push(worldPoint(event));
      $("#finishCalibrationButton").disabled = state.calibrationPoints.length !== 2; renderMap(); updatePrompt();
      return;
    }
    if (state.tool === "lane" && (type === "node" || type === "station-node")) {
      if (!state.linkFrom) state.linkFrom = id;
      else if (state.linkFrom !== id) { queueGeometryEdit({ action: "addLane", id: state.linkFrom, to: id, paired: $("#pairedLanes").checked }); state.linkFrom = ""; }
      render(); return;
    }
    if (type === "lane") { selectItem("lane", id); return; }
    if (type === "station" || type === "station-node") {
      const stationID = type === "station" ? id : target.dataset.station;
      selectItem("station", stationID);
      // A drag of the shape moves the station. A drag of a station node
      // moves only that node, and the station stays selected.
      if (state.tool === "select") beginDrag(type === "station" ? "station" : "node", type === "station" ? stationID : id, event);
      return;
    }
    if (type === "node") {
      selectItem("node", id);
      if (state.tool === "select") beginDrag("node", id, event);
      return;
    }
    if (type === "control") { beginDrag("control", id, event); return; }
    const location = worldPoint(event);
    if (state.tool === "junction" || state.tool === "station") {
      const station = state.tool === "station";
      queueGeometryEdit({ action: station ? "addStation" : "addNode", point: location }, { accepted: () => {
        if (selectionEpoch === geometrySelectionEpoch) selectItem(station ? "station" : "node", draft().network[station ? "Stations" : "Nodes"].at(-1).ID);
      } });
      return;
    }
    state.selection = null; beginDrag("pan", "", event); render();
  }

  function onPointerMove(event) {
    const drag = state.drag;
    if (!drag) return;
    if (drag.type === "pan") {
      state.view.x = drag.originalView.x + event.clientX - drag.startClient.x; state.view.y = drag.originalView.y + event.clientY - drag.startClient.y; setView(); return;
    }
    const location = worldPoint(event); const config = drag.working;
    // A move of the working copy is an edit, so an acquisition that ends
    // during the drag does not publish over it.
    model.moveDrag();
    if (drag.type === "station") {
      const dx = location.X - drag.last.X, dy = location.Y - drag.last.Y;
      shiftNodes(config, { ids: drag.moved, dx, dy }); drag.last = location;
      drag.delta.X += dx; drag.delta.Y += dy;
      drag.command = { action: "moveStation", id: drag.id, delta: drag.delta };
    } else if (drag.type === "node") {
      const node = nodeFor(config, drag.id); if (node) node.Position = { X: location.X, Y: location.Y };
      drag.command = { action: "moveNode", id: drag.id, point: location };
    } else if (drag.type === "control") {
      const lane = config.network.Lanes.find((item) => item.ID === drag.id); if (lane) lane.Control = location;
      drag.command = { action: "moveControl", id: drag.id, point: location };
    }
    // The selection panel shows the new values after the pointer up. A panel
    // change during the drag would make the browser lay out the whole map again.
    drawDragTargets(config, drag.targets);
  }

  function onPointerUp(event) {
    const drag = state.drag;
    if (!drag) return;
    state.drag = null; model.endDrag();
    try { $("#networkMap").releasePointerCapture(event.pointerId); } catch (_) {}
    if (drag.type !== "pan") {
      // Redraw the committed network even when Go rejects the preview.
      drawnNetwork = null;
      if (drag.command) queueGeometryEdit(drag.command, { source: drag.source, sourceEdits: model.edits });
      else render();
    }
  }

  function fitNetwork() { state.view = fitView(state.map.bounds, $("#networkMap").getBoundingClientRect()); setView(); }

  const goModel = root.PodsimGoEditor.createClient({ makeWorker: () => new Worker("./editor-model.js") });
  async function metadataWithGo(metadata) {
    const result = await goModel.call(null, "backgroundMetadata", metadataURLFacts(metadata));
    if (result.error) throw new Error(result.error);
    return result.metadata;
  }

  function editorProposal(config, command) { return goModel.call(config, "edit", { ...command, editor: true }); }
  async function normalizeWithGo(config) {
    const result = await editorProposal(config, { field: "normalize", value: true });
    if (result.error) throw new Error(result.error);
    const patch = result.change?.patch;
    if (!patch || typeof patch !== "object" || Array.isArray(patch)) throw new Error("The Go editor model did not return a normalized draft.");
    return { ...config, ...patch };
  }
  const pendingInputs = new Map();
  const typingInputs = new Set();
  let typingRevision = 0;
  const editQueue = root.PodsimGoEditor.createEditQueue({ onChange: () => { renderHistoryButtons(); renderApply(); } });
  function editRevision() { return `${editQueue.revision}:${typingRevision}`; }
  function bindScalarInput(id, field = id) {
    const input = $("#" + id);
    input.addEventListener("input", () => {
      markTyping(input);
    });
    input.addEventListener("change", () => queueScalarEdit(field, input));
  }
  function markTyping(input) { typingRevision++; model.abort(); typingInputs.add(input); }
  function renderHistoryButtons() {
    $("#undoButton").disabled = Boolean(state.history.failed) || !state.history.canUndo && !editQueue.pending && !state.history.pending;
    $("#redoButton").disabled = Boolean(state.history.failed) || !state.history.canRedo;
  }
  function cancelPendingEdits() {
    editQueue.cancel(); state.history.cancel();
    for (const rollback of pendingBackgroundPreviews) rollback();
    pendingInputs.clear(); typingInputs.clear(); railStructuralEdits.clear();
    pendingMapEnables.clear();
    for (const kind of ["arrival", "departure"]) railEpochs.set(kind, railEpoch(kind) + 1);
  }
  function queueScalarEdit(field, input, target) {
    const pending = { value: input.type === "checkbox" ? input.checked : input.value };
    typingInputs.delete(input); pendingInputs.set(input, pending); model.abort();
    editQueue.submit(async (current) => {
      if (!current()) return false;
      const config = draft(), generation = model.edits;
      if (state.drag && state.drag.type !== "pan" || model.gestureOpen) throw new Error("Finish the open gesture before editing settings.");
      const command = { field, value: pending.value };
      if (target !== undefined) command.target = target;
      const result = await editorProposal(config, command);
      if (result.error) throw new Error(result.error);
      if (!current()) return false;
      if (config !== draft() || generation !== model.edits || state.drag && state.drag.type !== "pan" || model.gestureOpen) throw new Error("The draft changed during the edit. Enter the setting again.");
      await state.history.replace({ scenario: { ...config, ...result.change.patch }, background: state.background }, true, Boolean(result.change.flag), { current });
      return true;
    }).catch((error) => toast(error.message, true)).finally(() => {
      if (pendingInputs.get(input) === pending) pendingInputs.delete(input);
      // Settings replies must not end or redraw a newer map gesture.
      setScalarValue("#scenarioName", draft().name); renderDemand();
      if (field === "fleetCount") renderFleet();
      renderHistoryButtons(); renderApply(); restorePendingInputs();
    });
  }
  function queueRailEdit(kind, command, control) {
    const epoch = control?.dataset.railEpoch === undefined ? railEpoch(kind) : Number(control.dataset.railEpoch);
    const structural = command.action !== "set", lock = { kind };
    const pending = command.action === "set" ? { value: control.value } : null;
    if (pending) { typingInputs.delete(control); pendingInputs.set(control, pending); }
    if (structural) railStructuralEdits.add(lock);
    model.abort(); renderRailLocks(kind);
    editQueue.submit(async (current) => {
      if (!current()) return false;
      if (epoch !== railEpoch(kind)) throw new Error("The rail rows changed. Enter the edit again.");
      const config = draft(), generation = model.edits;
      if (state.drag && state.drag.type !== "pan" || model.gestureOpen) throw new Error("Finish the open gesture before editing rail plans.");
      const result = await editorProposal(config, { field: kind === "departure" ? "railDeparture" : "railArrival", value: command });
      if (result.error) throw new Error(result.error);
      if (!current()) return false;
      if (config !== draft() || generation !== model.edits || state.drag && state.drag.type !== "pan" || model.gestureOpen) throw new Error("The draft changed during the edit. Enter the rail setting again.");
      await state.history.replace({ scenario: { ...config, ...result.change.patch }, background: state.background }, true, false, {
        current, beforePublish: (changed) => { if (changed && structural && Object.keys(result.change.patch).length) railEpochs.set(kind, railEpoch(kind) + 1); },
      });
      return true;
    }).catch((error) => toast(error.message, true)).finally(() => {
      if (pendingInputs.get(control) === pending) pendingInputs.delete(control);
      railStructuralEdits.delete(lock);
      // Rail replies must not end a newer map gesture.
      renderRailArrivals(draft(), kind); renderDemand(); renderHistoryButtons(); renderApply(); restorePendingInputs();
    });
  }
  function queueGeometryEdit(command, { controls = [], accepted, source, sourceEdits } = {}) {
    const pending = controls.map((input) => {
      const value = { value: input.type === "checkbox" ? input.checked : input.value };
      typingInputs.delete(input); pendingInputs.set(input, value);
      return { input, value };
    });
    model.abort();
    return editQueue.submit(async (current) => {
      if (!current()) return false;
      const config = draft(), generation = model.edits;
      if (source && (source !== config || sourceEdits !== generation)) throw new Error("The draft changed after the drag. Move the item again.");
      if (state.drag && state.drag.type !== "pan" || model.gestureOpen) throw new Error("Finish the open gesture before editing geometry.");
      const result = await editorProposal(config, { field: "geometry", value: command });
      if (result.error) throw new Error(result.error);
      if (!current()) return false;
      if (config !== draft() || generation !== model.edits || state.drag && state.drag.type !== "pan" || model.gestureOpen) throw new Error("The draft changed during the edit. Enter the geometry change again.");
      await state.history.replace({ scenario: { ...config, ...result.change.patch }, background: state.background }, true, false, {
        current, beforePublish: (changed) => { if (changed && accepted) accepted(config); },
      });
      return true;
    }).catch((error) => { toast(error.message, true); return false; }).finally(() => {
      for (const { input, value } of pending) if (pendingInputs.get(input) === value) pendingInputs.delete(input);
      // A late geometry reply must preserve a newer drawing gesture.
      if (!(state.drag && state.drag.type !== "pan") && !model.gestureOpen) render();
      else { renderHistoryButtons(); renderApply(); restorePendingInputs(); }
    });
  }
  const pendingMapEnables = new Set();
  function queueMapEdit(command, control) {
    const pending = control ? { value: control.value } : null;
    if (pending) { typingInputs.delete(control); pendingInputs.set(control, pending); }
    if (command.action === "enable") pendingMapEnables.add(command);
    model.abort();
    return editQueue.submit(async (current) => {
      if (!current()) return false;
      const config = draft(), generation = model.edits;
      if (state.drag && state.drag.type !== "pan" || model.gestureOpen) throw new Error("Finish the open gesture before editing the map.");
      const result = await editorProposal(config, { field: "map", value: command });
      if (result.error) throw new Error(result.error);
      if (!current()) return false;
      if (config !== draft() || generation !== model.edits || state.drag && state.drag.type !== "pan" || model.gestureOpen) throw new Error("The draft changed during the edit. Enter the map change again.");
      const scenario = { ...config, ...result.change.patch };
      if (command.action === "remove") delete scenario.map;
      await state.history.replace({ scenario, background: state.background }, true, false, { current });
      if (command.action === "enable") toast("Live map enabled. Pause and apply to show it in the simulation.");
      return true;
    }, { key: command.action === "opacity" ? "mapOpacity" : undefined }).catch((error) => { toast(error.message, true); return false; }).finally(() => {
      pendingMapEnables.delete(command);
      if (pending && pendingInputs.get(control) === pending) pendingInputs.delete(control);
      if (!(state.drag && state.drag.type !== "pan") && !model.gestureOpen) render();
      else { renderBackground(); renderHistoryButtons(); renderApply(); restorePendingInputs(); }
    });
  }
  async function backgroundProposal(config, command) {
    const result = await editorProposal(config, { field: "background", value: command });
    if (result.error) throw new Error(result.error);
    if (!result.change?.background || !Object.hasOwn(result.change.background, "value")) throw new Error("The Go background proposal is missing.");
    return { value: { scenario: { ...config, ...result.change.patch }, background: result.change.background.value }, note: result.change.note };
  }
  const pendingBackgroundPreviews = new Set();
  function queueBackgroundEdit(command, { accepted, before, sourceEdits, imageKey, acceptIf = () => true, control } = {}) {
    const pending = control ? { value: control.value } : null;
    if (pending) pendingInputs.set(control, pending);
    model.abort();
    const previewSource = before ? draft() : null;
    let committed = false;
    const rollbackPreview = () => {
      if (!committed && before && previewSource === draft() && sourceEdits === model.edits && !model.gestureOpen && !(state.drag && state.drag.type !== "pan")) state.history.preview(before);
    };
    if (before) pendingBackgroundPreviews.add(rollbackPreview);
    return editQueue.submit(async (current) => {
      if (!current() || !acceptIf()) return false;
      const config = draft(), generation = model.edits;
      if (before && sourceEdits !== generation) throw new Error("The draft changed after the opacity gesture. Set the opacity again.");
      if (state.drag && state.drag.type !== "pan" || model.gestureOpen) throw new Error("Finish the open gesture before editing the background.");
      const background = state.history.background;
      if (imageKey && imageKey !== background?.imageKey) throw new Error("The background changed. Place the current image again.");
      const result = await backgroundProposal(config, { ...command, background });
      if (!current() || !acceptIf()) return false;
      if (config !== draft() || generation !== model.edits || state.drag && state.drag.type !== "pan" || model.gestureOpen) throw new Error("The draft changed during the edit. Enter the background change again.");
      const options = {
        current: () => current() && acceptIf(),
        beforePublish: () => {
          committed = true;
          if (accepted) { render(); accepted(); }
          if (result.note) updateStatus(result.note);
        },
      };
      if (before) await state.history.commitFrom(before, result.value, options);
      else await state.history.replace(result.value, true, false, options);
      return committed;
    }, { key: command.action === "opacity" && !before ? "backgroundOpacity" : undefined }).catch((error) => {
      toast(error.message, true); return false;
    }).finally(() => {
      // A rejected or canceled preview can roll back only its own draft.
      rollbackPreview(); pendingBackgroundPreviews.delete(rollbackPreview);
      if (pending && pendingInputs.get(control) === pending) pendingInputs.delete(control);
      if (!(state.drag && state.drag.type !== "pan") && !model.gestureOpen) render();
      else { renderHistoryButtons(); renderApply(); restorePendingInputs(); }
    });
  }
  function deleteSelectedItem(selection, { keyboard = false } = {}) {
    const focused = document.activeElement;
    const action = { station: "deleteStation", lane: "deleteLane", node: "deleteNode" }[selection.type];
    return queueGeometryEdit({ action, id: selection.id }, { accepted: (before) => {
      const ownsFocus = document.activeElement === focused;
      if (selection.type === "station") reportStationDeletion(before, selection.id);
      if (state.selection?.type === selection.type && state.selection.id === selection.id) state.selection = null;
      render();
      if (keyboard && ownsFocus && !focused.isConnected) focusMap();
    } });
  }
  let placeNavigationEpoch = 0;
  const placeSearch = root.PodsimPlaceSearch.create({
    form: $("#placeSearchForm"), input: $("#placeSearchQuery"), button: $("#placeSearchButton"), status: $("#placeSearchStatus"), results: $("#placeSearchResults"),
    fetch: (url, options) => root.fetch(url, options), hasReference: () => !!draft().geo,
    navigate: async (place) => {
      const geo = draft().geo, previousView = { ...state.view }, epoch = placeNavigationEpoch, rect = $("#networkMap").getBoundingClientRect();
      const result = await goModel.call({ geo }, "place-view", { latitude: place.latitude, longitude: place.longitude, bounds: place.bounds, width: rect.width, height: rect.height });
      if (result.error) throw new Error(result.error);
      const currentRect = $("#networkMap").getBoundingClientRect();
      if (geo !== draft().geo || ["x", "y", "scale"].some((key) => previousView[key] !== state.view[key]) || rect.width !== currentRect.width || rect.height !== currentRect.height || state.drag || epoch !== placeNavigationEpoch) throw new Error("The map view or geographic reference changed. Select the place again.");
      state.view = result.view; setView();
    },
  });
  root.addEventListener("pagehide", () => { placeNavigationEpoch++; placeSearch.suspend(); });
  root.addEventListener("pageshow", () => placeSearch.resume());
  let validationJob = null;
  function scheduleValidation() {
    if (validationJob) return;
    const config = draft();
    validationJob = runValidation(config, true).finally(() => {
      validationJob = null;
      if (config !== draft()) checks.schedule();
    });
  }

  // runValidation checks a draft off the main thread. A stale result cannot replace current checks.
  async function runValidation(config = draft(), background = false) {
    let results;
    try { results = await goModel.call(config, "validate", undefined, { background }); }
    catch (error) { if (error.name === "AbortError") return { errors: [], warnings: [], valid: false }; results = { errors: [{ text: error.message }], warnings: [], valid: false }; }
    if (!results || !Array.isArray(results.errors) || !Array.isArray(results.warnings) || ![...results.errors, ...results.warnings].every((row) => row && typeof row.text === "string" && row.text.length > 0) || results.valid !== true && !results.errors.length) results = { errors: [{ text: "The Go editor model did not return a valid verdict. Reload the editor." }], warnings: [], valid: false };
    if (config === draft()) showValidation(config, results);
    return results;
  }
  // showValidation shows the check results in the Checks section, and the
  // problem count beside the apply button. It lists the errors first, then
  // the warnings. A result that names an object of the scenario is a button
  // that selects the object. The new list removes the focused button, so
  // when a button of the list had the keyboard focus, the focus goes to the
  // button or heading that checkFocusKey gives.
  function showValidation(config, results) {
    const summary = $("#validationSummary"); const list = $("#validationList");
    const buttons = () => [...list.querySelectorAll(".check-link")];
    const links = () => buttons().map((button) => ({ text: button.textContent, type: button.dataset.type, id: button.dataset.id }));
    const index = buttons().indexOf(document.activeElement); const focused = index < 0 ? null : index; const before = links();
    list.replaceChildren();
    const { tone, text } = validationSummary(results.errors, results.warnings); summary.className = `validation ${tone}`; summary.textContent = text;
    const count = $("#problemCount"); count.textContent = problemCountText(results.errors.length); count.hidden = !results.errors.length;
    const rows = [...results.errors.map((result) => ({ result, text: result.text })), ...results.warnings.map((result) => ({ result, text: `Warning: ${result.text}`, warning: true }))];
    const select = checkSelector(config);
    for (const row of rows) {
      const item = document.createElement("li"); if (row.warning) item.className = "warning";
      if (select(row.result.target)) {
        const button = document.createElement("button"); button.type = "button"; button.className = "check-link"; button.textContent = row.text;
        button.dataset.type = row.result.target.type; button.dataset.id = row.result.target.id; item.append(button);
      } else item.textContent = row.text;
      list.append(item);
    }
    const key = checkFocusKey({ before, focused, after: links() });
    if (key !== null) (buttons().find((button) => button.textContent === key) || $("#checksHeading")).focus({ preventScroll: true });
  }
  // showChecks scrolls the Checks section into view. It moves the keyboard
  // focus to the first item that selects an object, or else to Run checks.
  function showChecks() {
    $("#checksPanel").scrollIntoView({ block: "start" });
    ($("#validationList button") || $("#validateButton")).focus({ preventScroll: true });
  }
  // selectCheck selects the object that a check result names. When the map
  // does not show the object well, the view moves to it, as focusView tells.
  // In a narrow window the map is above the Checks section, so the page
  // scrolls until the map is in view.
  function selectCheck(target) {
    const config = draft(); const selection = checkSelection(config, target);
    if (!selection) return;
    const point = selectionPoint(config, selection);
    if (point) state.view = focusView({ view: state.view, point, size: $("#networkMap").getBoundingClientRect() });
    state.selection = selection; render();
    $(".map-panel").scrollIntoView({ block: "nearest" });
  }

  // loadServerProject loads the live scenario as the draft, as startEditor
  // calls it. Go normalizes the live baseline before the first render.
  // When the live
  // scenario cannot load, the local fallback draft is the baseline. Then
  // the editor offers a saved draft that is different. readLive gives the
  // live scenario with the revision, the epoch and the server start ID of
  // the same server state. The load ignores a background in a saved draft
  // of an older editor.
  async function loadServerProject() {
    updateStatus("Loading the live scenario…");
    try {
      const live = await readLive(state.connection, normalizeWithGo);
      state.loadedRevision = live.revision; state.connection.epoch = live.epoch; state.loadedStart = live.serverStart;
      state.draftBase = { revision: live.revision, epoch: live.epoch, serverStart: live.serverStart };
      model.setLoaded({ scenario: clone(live.project), background: null });
      await state.history.reset(model.loaded); state.background = null; state.selection = null;
      render(); setLive(state.history.value, state.loadedRevision); fitNetwork();
      updateStatus(`Live revision ${state.loadedRevision}. ${DRAFT_STORE_TEXT[keeper.status]}`);
      // Keep a server scenario that fails the editor checks, and list the
      // problems. Only errors show the error toast.
      const { errors } = await checks.run();
      if (errors.length) toast(`The server scenario has ${errors.length} validation problem${errors.length === 1 ? "" : "s"}. See Checks.`, true);
    } catch (error) {
      if (state.history.failed) throw error;
      const fallback = { scenario: fallbackConfig(), background: null };
      await state.history.reset(fallback, { beforePublish: () => model.setLoaded(fallback) });
      updateStatus("The live scenario could not load. This draft is local."); render(); setLive(state.history.value, null); fitNetwork(); toast(`Load failed. ${error.message}`, true);
    }
  }

  // applyProject applies the draft to the live session. The keeper saves
  // the draft first, so a failed apply does not lose it. A successful apply
  // makes the draft the live baseline and deletes the saved draft, in one
  // replace write. The background keeper keeps the background. While the
  // saved draft offer shows, the keeper does not save. Then the apply
  // keeps the saved draft, and the offer names the new live revision.
  // After a conflict, the page shows the apply conflict actions. base has
  // the revision and the server start ID that the apply sends. Only a
  // successful apply changes the loaded revision and the draft base.
  async function applyProject(base = { revision: state.loadedRevision, serverStart: state.draftBase.serverStart }) {
    if (state.applying) return;
    const inputRevision = editRevision();
    const button = $("#applyButton"); state.applying = true; renderApply(); button.textContent = "Checking…";
    try {
      await state.history.flush();
      if (!await editQueue.flush() || inputRevision !== editRevision()) { toast("The settings changed or an edit failed. Apply the current draft again.", true); return; }
      const project = draft();
      if (!await readyToApply(project)) return;
      button.textContent = "Pausing…"; keeper.flush();
      const applied = await applyToServer({ connection: state.connection, revision: base.revision, serverStart: base.serverStart, project, onApplying: () => { button.textContent = "Applying…"; } });
      // A restored draft from an older editor has no server start ID. The
      // applied state then gets the ID that the apply read.
      const serverStart = applied.serverStart || base.serverStart;
      state.loadedRevision = applied.revision; state.loadedStart = serverStart; state.draftBase = { revision: applied.revision, epoch: state.connection.epoch, serverStart };
      model.setLoaded({ scenario: project, background: state.background ? clone(state.background) : null });
      setLive(model.loaded, applied.revision); renderOffer(); showConflict(null);
      if (!state.offer) keeper.replace();
      updateStatus(`Applied revision ${state.loadedRevision}. The simulation is paused.`); toast(...applyToast(applied));
    } catch (error) {
      // note tells the user when a reload loses the draft. Another failure
      // keeps the conflict that the page shows.
      const note = keeper.status !== "ok" ? DRAFT_STORE_TEXT[keeper.status] : keeper.unsaved ? DRAFT_UNSAVED_TEXT : "";
      const conflict = await readConflict(state.connection, error, normalizeWithGo);
      if (conflict) showConflict(conflict.revision === null ? null : conflict);
      const unloaded = conflict && conflict.revision === null ? CONFLICT_UNLOADED_TEXT : "";
      updateStatus([applyFailureStatus(error, note), unloaded].filter(Boolean).join(" ")); toast(applyFailureText(error, note), true);
    } finally { state.applying = false; renderApply(); button.textContent = "Pause and apply"; }
  }

  // readyToApply runs the checks. When the draft has errors, it shows them
  // and gives false.
  async function readyToApply(config = draft()) {
    const editGeneration = model.edits, inputRevision = editRevision();
    const { errors, valid } = await checks.run();
    if (state.history.failed || state.history.pending || config !== draft() || editGeneration !== model.edits || inputRevision !== editRevision() || editQueue.pending || state.drag && state.drag.type !== "pan" || model.gestureOpen) { toast("The draft changed during validation. Apply the current draft again.", true); return false; }
    if (errors.length) { showChecks(); toast("Fix the listed problems before you apply the scenario.", true); }
    return valid === true && !errors.length;
  }

  // showConflict shows the apply conflict actions for conflict, or hides
  // them when conflict is null.
  function showConflict(conflict) { state.conflict = conflict; renderConflict(); }

  // renderConflict shows the apply conflict panel while state.conflict is
  // set. renderApply disables its buttons.
  function renderConflict() {
    $("#applyConflict").hidden = !state.conflict;
    if (!state.conflict) return;
    const view = conflictView(state.conflict);
    $("#applyConflictText").textContent = view.text; $("#applyConflictHint").textContent = view.hint; $("#applyOverButton").textContent = view.applyLabel;
  }

  // runConflictAction runs action, a conflict action that reads the live
  // state, for event, the click on its button. It blocks the other actions
  // while it runs. A failed read shows the error toast and changes nothing.
  // After a choice from the keyboard, the map gets the focus when the
  // panel hides, as closeOffer does.
  async function runConflictAction(event, action) {
    if (!state.conflict || state.applying) return;
    state.applying = true; renderApply();
    let next = null;
    try { next = await action(); } catch (error) { toast(`The live scenario could not load. ${error.message}`, true); }
    state.applying = false; renderApply();
    try { if (next) await next(); } catch (error) { toast(`The live scenario could not load. ${error.message}`, true); }
    if (!state.conflict && event && event.detail === 0) focusMap();
  }

  // loadLiveScenario makes the live scenario the draft and the base of
  // Reset draft, with the live revision, epoch and server start ID. It
  // asks first when the draft has scenario changes. The draft keeps its
  // background, because the server does not have it. Undo brings back the
  // replaced draft.
  function loadLiveScenario(event) {
    return runConflictAction(event, async () => {
      const config = draft(), generation = model.edits, inputRevision = editRevision();
      const live = await loadLive({ connection: { ...state.connection }, changed: draftChanged(state.history.value, state.live), confirm: (text) => root.confirm(text), normalize: normalizeWithGo });
      if (live && (config !== draft() || generation !== model.edits || inputRevision !== editRevision() || state.drag && state.drag.type !== "pan" || model.gestureOpen)) throw new Error("The draft changed during loading. Load the live scenario again.");
      return live && (async () => {
        cancelPendingEdits(); model.abort();
        const publicationRevision = editRevision();
        const { value, loaded, ...page } = liveDraft(live, { loaded: model.loaded, background: state.history.background });
        let published = false;
        await state.history.replace(value, true, false, {
          current: () => publicationRevision === editRevision(),
          beforePublish: () => {
            Object.assign(state, page); state.connection.epoch = live.epoch; model.setLoaded(loaded); state.selection = null;
            setLive({ scenario: value.scenario }, live.revision); showConflict(null); renderOffer(); published = true;
          },
        });
        if (!published) return;
        if (!(state.drag && state.drag.type !== "pan") && !model.gestureOpen) { render(); fitNetwork(); }
        checks.run(); keeper.flush();
        updateStatus(`Live revision ${live.revision}. The draft is the live scenario.`); toast("The live scenario replaced the draft.");
      });
    });
  }

  // applyOver applies the draft over the live revision of the conflict. It
  // asks first. Pause and apply then runs as usual with the base that
  // applyOverBase gives.
  function applyOver(event) {
    if (!state.conflict || state.applying) return Promise.resolve();
    return runConflictAction(event, async () => {
      const config = draft(), generation = model.edits, inputRevision = editRevision();
      const base = await applyOverBase({ connection: { ...state.connection }, revision: state.conflict.revision, serverStart: state.conflict.serverStart, confirm: (text) => root.confirm(text), normalize: normalizeWithGo });
      if (base && (config !== draft() || generation !== model.edits || inputRevision !== editRevision() || state.drag && state.drag.type !== "pan" || model.gestureOpen)) throw new Error("The draft changed during loading. Apply the current draft again.");
      return base && (() => { state.connection.epoch = base.epoch; return applyProject(base); });
    });
  }

  // importProject replaces the draft with a project file and makes the file
  // the baseline of Reset draft. It is an import of the model, so it aborts
  // the open acquisition, and a newer action aborts it. parseDocument
  // checks the file, also the size and the header of the background image
  // and its asset. The page converts the data URL to bytes and drops the
  // file text, then the decoder slot decodes the bytes, as checkImageBytes
  // does. The publish fails when the draft changed or a changed gesture is
  // open, and then the draft does not change. The file has no draft base,
  // so the draft gets the server start ID of the loaded project. A file
  // that the user exported before a server restart then applies after a
  // reload, also after Restore draft.
  async function importProject(file) {
    if (!file) return;
    if (file.size > PROJECT_FILE_BYTES) { toast(`The project file must be ${PROJECT_FILE_BYTES / MIB} MiB or smaller.`, true); return; }
    cancelPendingEdits(); const ticket = model.start();
    try {
      let text = await file.text();
      if (!model.current(ticket)) return;
      const imported = parseDocument(text, { deferMetadata: true }); text = "";
      const compatibility = await goModel.call(imported.scenario, "importCompatibility");
      if (!model.current(ticket)) return;
      if (compatibility.error) throw new Error(compatibility.error);
      imported.scenario = ownDraft({ ...imported.scenario, ...compatibility.change.patch });
      const metadata = await metadataWithGo(imported.metadata);
      if (!model.current(ticket)) return;
      imported.asset = metadata.asset;
      if (imported.background) imported.background = { dataURL: imported.background.dataURL, ...metadata.placement };
      delete imported.metadata;
      const verdict = await goModel.call(imported.scenario);
      if (!model.current(ticket)) return;
      if (verdict.valid !== true || !Array.isArray(verdict.errors) || verdict.errors.length) {
        const errors = verdict.errors || [{ text: "The Go editor model could not validate the project." }];
        throw new Error(`The project has ${errors.length} error${errors.length === 1 ? "" : "s"}. ${errors.slice(0, 3).map((item) => item.text).join(" ")}`);
      }
      imported.scenario = await normalizeWithGo(imported.scenario);
      if (!model.current(ticket)) return;
      let image = null; let background = null;
      if (imported.background) {
        const { bytes, placement } = takeBackgroundBytes(imported);
        if (slot.busy) updateStatus("The project image waits for the decoder.");
        const facts = await slot.run((signal) => checkImageBytes(bytes, decoder, signal), ticket.signal);
        const asset = imported.asset || { frameState: "none", frame: null, license: null };
        image = freezeImage({ key: newImageKey(root.crypto), bytes, mime: facts.mime, pixelWidth: facts.width, pixelHeight: facts.height, frame: asset.frame, license: asset.license });
        background = { imageKey: image.key, ...placement, frameState: asset.frameState };
      }
      if (!model.current(ticket)) return;
      const result = await model.publish({ ticket, image, value: { scenario: imported.scenario, background }, baseline: true,
        beforePublish: () => { state.selection = null; state.draftBase = { ...state.draftBase, serverStart: state.loadedStart }; render(); fitNetwork(); },
      });
      if (result.error) { toast(`${result.error} The draft is unchanged.`, true); return; }
      checks.run(); toast("The project was imported into the draft.");
      reportPublish("project import", image, result.dropped);
    } catch (error) {
      if (error.name === "AbortError" || !model.current(ticket)) return;
      model.abort(); toast(`${error.message} The draft is unchanged.`, true);
    }
  }

  // takeBackgroundBytes gives the bytes of the data URL of the background
  // of imported, a result of parseDocument, and its placement fields. It
  // removes the background from imported, so the import holds only the
  // bytes while it waits for the decode.
  function takeBackgroundBytes(imported) {
    const { dataURL, ...placement } = imported.background; imported.background = null;
    return { bytes: dataURLToBytes(dataURL), placement };
  }

  // exportProject writes the draft and its background to a project file.
  // After pending edits finish, it takes the image and makes its data URL
  // in one task, so no later change drops the image first.
  async function exportProject() {
    await editQueue.flush(); await state.history.flush().catch(() => {});
    const snapshot = state.history.snapshot;
    const background = snapshot.background ? exportBackground(snapshot.background, model.image(snapshot.background.imageKey)) : null;
    const blob = new Blob([serializeDocument(snapshot.scenario, background)], { type: "application/json" });
    const url = URL.createObjectURL(blob); const link = document.createElement("a");
    const safe = (snapshot.scenario.name || "scenario").toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "") || "scenario";
    link.href = url; link.download = `${safe}.podsim.json`; document.body.append(link); link.click(); link.remove(); URL.revokeObjectURL(url);
  }

  // reportPublish tells the user of a published image, and of the undo
  // steps that the image limit dropped. It writes one console line with
  // the same values.
  function reportPublish(kind, image, dropped) {
    if (dropped) {
      const text = `The image limit of ${mibText(model.cap)} MiB removed the ${dropped} oldest undo step${dropped === 1 ? "" : "s"}.`;
      updateStatus(text); toast(text);
    }
    console.info(`podsim editor: ${kind}`, { key: image ? image.key : "", bytes: image ? image.bytes.byteLength : 0, width: image ? image.pixelWidth : 0, height: image ? image.pixelHeight : 0, held: model.bytes, dropped });
  }

  // keptToast gives the toast after a new background: the browser keeps
  // it, or the status of the background keeper.
  function keptToast() {
    return backgroundKeeper.status === "ok" ? ["This browser keeps the background for this server. Export the project to use it in another browser.", false] : [BACKGROUND_STORE_TEXT[backgroundKeeper.status], true];
  }

  // importBackground makes an image file the background. The file must be
  // a PNG or JPEG of at most IMAGE_FILE_BYTES. It is an acquisition of the
  // model, so it aborts the open one. checkImageBytes checks the header
  // size of the image before the decoder slot decodes it, and then the
  // decoded size. Before a calibration, one image pixel is one meter. The
  // image has no frame and no license.
  async function importBackground(file) {
    if (!file) return;
    if (!/^image\/(png|jpeg)$/.test(file.type)) { toast("Choose a PNG or JPEG image.", true); return; }
    if (file.size > IMAGE_FILE_BYTES) { toast(`The background image must be ${IMAGE_FILE_BYTES / MIB} MiB or smaller.`, true); return; }
    const ticket = model.start();
    try {
      const bytes = await file.arrayBuffer();
      if (!model.current(ticket)) return;
      if (slot.busy) updateStatus("The image waits for the decoder.");
      const facts = await slot.run((signal) => checkImageBytes(bytes, decoder, signal), ticket.signal);
      const image = freezeImage({ key: newImageKey(root.crypto), bytes, mime: facts.mime, pixelWidth: facts.width, pixelHeight: facts.height, frame: null, license: null });
      if (!model.current(ticket)) return;
      const proposed = await backgroundProposal(draft(), { action: "initialize", imageKey: image.key, width: facts.width, height: facts.height, background: state.history.background });
      await publishBackground(ticket, image, proposed.value, "image import");
    } catch (error) {
      if (error.name === "AbortError" || !model.current(ticket)) return;
      model.abort(); toast(`${error.message} The background is unchanged.`, true);
    }
  }

  // publishBackground publishes image with value for the ticket of an
  // acquisition, and tells the user of the result.
  async function publishBackground(ticket, image, value, kind) {
    if (!model.current(ticket)) return;
    const result = await model.publish({ ticket, image, value, beforePublish: () => { render(); fitNetwork(); } });
    if (result.error) { toast(`${result.error} The background is unchanged.`, true); return result; }
    toast(...keptToast());
    reportPublish(kind, image, result.dropped);
    return result;
  }

  // referenceChoice gives the reference choice of the reference panel for
  // referenceFor, or null when the user did not choose.
  function referenceChoice() {
    const mode = $("#referenceMode").value;
    const number = (selector) => { const text = $(selector).value.trim(); return text === "" ? NaN : Number(text); };
    if (mode === "adopt") return { mode, confirmed: $("#adoptConfirm").checked };
    if (mode === "anchor") {
      return {
        mode,
        a: { id: $("#anchorANode").value.trim(), latitude: number("#anchorALatitude"), longitude: number("#anchorALongitude") },
        b: { id: $("#anchorBNode").value.trim(), latitude: number("#anchorBLatitude"), longitude: number("#anchorBLongitude") },
      };
    }
    return null;
  }

  // closeGeoPanel hides the georeferenced image panel. It aborts the open
  // acquisition.
  function closeGeoPanel() { model.abort(); state.geoOpen = false; renderBackground(); }

  // importFramedImage imports the image of the georeferenced image panel
  // with its frame and license. The frame and the reference are checked
  // before any work. A Web Mercator image is resampled into a latitude and
  // longitude grid in the decoder slot. The publish puts the image, its
  // placement from the frame, the frame state "attached" and the geo
  // reference into one history step.
  async function importFramedImage() {
    model.abort();
    const file = $("#geoFile").files[0];
    if (!file) { toast("Choose a PNG or JPEG image.", true); return; }
    if (!/^image\/(png|jpeg)$/.test(file.type)) { toast("Choose a PNG or JPEG image.", true); return; }
    if (file.size > IMAGE_FILE_BYTES) { toast(`The background image must be ${IMAGE_FILE_BYTES / MIB} MiB or smaller.`, true); return; }
    const bound = (selector) => { const text = $(selector).value.trim(); return text === "" ? NaN : Number(text); };
    let frame = { south: bound("#geoSouth"), north: bound("#geoNorth"), west: bound("#geoWest"), east: bound("#geoEast"), source: $("#geoSource").value };
    let license = {
      source: $("#geoLicenseSource").value.trim(), attribution: $("#geoAttribution").value.trim(), license: $("#geoLicense").value.trim(),
      licenseURL: $("#geoLicenseURL").value.trim(), copyrightURL: $("#geoCopyrightURL").value.trim(), retrieved: new Date().toISOString(), method: "user supplied", notice: $("#geoNotice").value.trim(),
    };
    // A frame or a license that is not valid stops the new import before
    // any file read. The previous acquisition stays canceled.
    const ticket = model.start();
    try {
      const metadata = await metadataWithGo({ asset: { frameState: "attached", frame, license } });
      if (!model.current(ticket)) return;
      frame = metadata.asset.frame; license = metadata.asset.license;
      const key = newImageKey(root.crypto);
      const proposed = await backgroundProposal(draft(), { action: "place", imageKey: key, frame, choice: referenceChoice(), background: state.history.background });
      if (!model.current(ticket)) return;
      const changed = model.publishError(ticket, null);
      if (changed) throw new Error(changed);
      const bytes = await file.arrayBuffer();
      if (!model.current(ticket)) return;
      if (slot.busy) updateStatus("The image waits for the decoder.");
      const result = frame.source === "web-mercator"
        ? await slot.run((signal) => resampleImage({ bytes, frame, geo: proposed.value.scenario.geo, metersPerPixel: Number($("#geoMeters").value) }, decoder, signal), ticket.signal)
        : { bytes, facts: await slot.run((signal) => checkImageBytes(bytes, decoder, signal), ticket.signal) };
      const image = freezeImage({ key, bytes: result.bytes, mime: result.facts.mime, pixelWidth: result.facts.width, pixelHeight: result.facts.height, frame, license });
      const published = await publishBackground(ticket, image, proposed.value, "georeferenced image import");
      if (published && !published.error && proposed.note) updateStatus(proposed.note);
    } catch (error) {
      if (error.name === "AbortError" || !model.current(ticket)) return;
      model.abort();
      const hint = frame.source === "web-mercator" && /MiB or smaller/.test(error.message) ? " Choose more meters for each pixel." : "";
      toast(`${error.message}${hint} The background is unchanged.`, true);
    }
  }

  function enableTileMap() {
    const number = (id) => $(id).value.trim() === "" ? NaN : Number($(id).value);
    queueMapEdit({ action: "enable", latitude: number("#mapLatitude"), longitude: number("#mapLongitude"), opacity: Number($("#mapOpacity").value), choice: referenceChoice() });
  }

  // placeFromFrame places the background on its frame in the geo
  // reference of the draft, or in the reference of the reference choice,
  // and attaches the frame, in one history step.
  function placeFromFrame() {
    const image = state.background ? model.image(state.background.imageKey) : null;
    if (!image || !image.frame) return;
    queueBackgroundEdit({ action: "place", imageKey: image.key, frame: image.frame, choice: referenceChoice() }, { imageKey: image.key, accepted: fitNetwork });
  }

  function finishCalibration() {
    if (!state.background || state.calibrationPoints.length !== 2) return;
    const points = state.calibrationPoints, [a, b] = points;
    queueBackgroundEdit({ action: "calibrate", a, b, meters: Number($("#calibrationDistance").value) }, {
      acceptIf: () => state.calibrating && state.calibrationPoints === points,
      accepted: () => {
        state.calibrating = false; state.calibrationPoints = []; $("#calibrationPanel").hidden = true;
        fitNetwork(); toast("The background scale is set. New network geometry uses meters.");
      },
    });
  }

  async function resetDraft() {
    model.abort(); cancelPendingEdits();
    try {
      let published = false;
      await state.history.replace(model.loaded, true, false, {
        beforePublish: () => { state.draftBase = { revision: state.loadedRevision, epoch: state.connection.epoch, serverStart: state.loadedStart }; state.selection = null; showConflict(null); published = true; },
      });
      if (!published) return;
      if (!(state.drag && state.drag.type !== "pan") && !model.gestureOpen) { render(); fitNetwork(); }
      toast("The draft matches the last loaded project.");
    } catch (error) { toast(error.message, true); }
  }

  function bindEvents() {
    document.querySelectorAll(".tool").forEach((button) => button.addEventListener("click", () => setTool(button.dataset.tool)));
    $("#networkMap").addEventListener("pointerdown", onPointerDown); $("#networkMap").addEventListener("pointermove", onPointerMove); $("#networkMap").addEventListener("pointerup", onPointerUp); $("#networkMap").addEventListener("pointercancel", onPointerUp);
    $("#networkMap").addEventListener("contextmenu", (event) => { event.preventDefault(); state.linkFrom = ""; render(); });
    $("#networkMap").addEventListener("wheel", (event) => { event.preventDefault(); zoomAt(event.deltaY < 0 ? 1.12 : .89, event.clientX, event.clientY); }, { passive: false });
    $("#zoomInButton").addEventListener("click", () => { const rect = $("#networkMap").getBoundingClientRect(); zoomAt(1.25, rect.left + rect.width / 2, rect.top + rect.height / 2); });
    $("#zoomOutButton").addEventListener("click", () => { const rect = $("#networkMap").getBoundingClientRect(); zoomAt(.8, rect.left + rect.width / 2, rect.top + rect.height / 2); });
    $("#fitButton").addEventListener("click", fitNetwork); $("#cancelLinkButton").addEventListener("click", () => { state.linkFrom = ""; render(); });
    $("#undoButton").addEventListener("click", () => stepHistory(false)); $("#redoButton").addEventListener("click", () => stepHistory(true));
    $("#resetButton").addEventListener("click", resetDraft);
    $("#validateButton").addEventListener("click", async () => { if (await editQueue.flush()) checks.run(); }); $("#applyButton").addEventListener("click", () => applyProject());
    $("#restoreDraftButton").addEventListener("click", restoreDraft); $("#discardDraftButton").addEventListener("click", discardDraft);
    $("#loadLiveButton").addEventListener("click", loadLiveScenario); $("#applyOverButton").addEventListener("click", applyOver);
    // In the shell page, the return link asks the shell to show the game.
    // The game then keeps its map view and its selection. When the editor is
    // the top page, or a modifier key opens the link elsewhere, the link
    // opens index.html. After Enter on the link, the click detail is 0, and
    // the message tells the shell that the keyboard selected the link.
    $("#simulationLink").addEventListener("click", (event) => {
      const shell = shellPage(root); if (!shell || event.ctrlKey || event.metaKey || event.shiftKey || event.altKey) return;
      event.preventDefault(); shell.postMessage({ podsim: "show", view: "game", keyboard: event.detail === 0 }, root.location.origin);
    });
    // The browser asks before you leave or reload the page only when the
    // draft or the background has changes that are not in their store. A
    // waiting save starts now. It is not in the store yet, so the prompt still shows.
    state.connection.onServerStart = observeServerStart;
    root.addEventListener("focus", refreshLiveStart);
    root.addEventListener("beforeunload", (event) => { model.abort(); keeper.flush(); backgroundKeeper.flush(); if (editQueue.pending || typingInputs.size || keeper.unsaved || backgroundKeeper.unsaved) { event.preventDefault(); event.returnValue = ""; } });
    $("#problemCount").addEventListener("click", showChecks);
    $("#validationList").addEventListener("click", (event) => { const button = event.target.closest("button[data-type]"); if (button) selectCheck({ type: button.dataset.type, id: button.dataset.id }); });
    $("#exportButton").addEventListener("click", exportProject); $("#projectImport").addEventListener("change", (event) => { importProject(event.target.files[0]); event.target.value = ""; });
    $("#backgroundImport").addEventListener("change", (event) => { importBackground(event.target.files[0]); event.target.value = ""; });
    // The opacity gesture of the model runs from pointer down to pointer
    // up, pointercancel or a lost pointer capture. Each of the three ends
    // it in the same way, and records a change as one undo step.
    const endOpacity = () => {
      const ended = model.takeOpacity();
      if (ended?.changed) queueBackgroundEdit({ action: "opacity", opacity: state.history.background.opacity }, { before: ended.entry, sourceEdits: model.edits, control: $("#backgroundOpacity") });
      else render();
    };
    $("#backgroundOpacity").addEventListener("pointerdown", () => model.pressOpacity());
    $("#backgroundOpacity").addEventListener("input", (event) => {
      const value = Number(event.target.value); $("#opacityValue").value = `${Math.round(value * 100)}%`;
      if (model.gestureOpen) {
        if (model.setOpacity(value)) { state.background = state.history.background; renderMap(); }
      } else if (state.background) queueBackgroundEdit({ action: "opacity", opacity: value }, { control: event.target });
    });
    for (const name of ["pointerup", "pointercancel", "lostpointercapture"]) $("#backgroundOpacity").addEventListener(name, endOpacity);
    $("#removeBackgroundButton").addEventListener("click", () => queueBackgroundEdit({ action: "remove" }, { accepted: () => { state.calibrating = false; state.calibrationPoints = []; $("#calibrationPanel").hidden = true; } }));
    $("#mapOpenButton").addEventListener("click", () => { state.mapOpen = !state.mapOpen; if (!state.mapOpen) model.abort(); renderBackground(); });
    $("#mapEnableButton").addEventListener("click", enableTileMap);
    $("#mapRemoveButton").addEventListener("click", () => queueMapEdit({ action: "remove" }));
    $("#mapRetryButton").addEventListener("click", () => tiles.retry());
    $("#mapOpacity").addEventListener("input", () => {
      $("#mapOpacityValue").value = `${Math.round(Number($("#mapOpacity").value) * 100)}%`;
      if (draft().map || pendingMapEnables.size) queueMapEdit({ action: "opacity", opacity: Number($("#mapOpacity").value) }, $("#mapOpacity"));
    });
    new ResizeObserver(renderTiles).observe($("#networkMap"));
    document.addEventListener("visibilitychange", renderTiles);
    root.addEventListener("pagehide", () => { tilePageActive = false; renderTiles(); });
    root.addEventListener("pageshow", () => { tilePageActive = true; renderTiles(); });
    if (root.frameElement) new MutationObserver(renderTiles).observe(root.frameElement, { attributes: true, attributeFilter: ["inert"] });
    $("#geoImportButton").addEventListener("click", () => { state.geoOpen = true; renderBackground(); });
    $("#geoImportCloseButton").addEventListener("click", closeGeoPanel); $("#geoImportRunButton").addEventListener("click", importFramedImage);
    $("#geoFile").addEventListener("change", (event) => { const file = event.target.files[0]; $("#geoFileName").textContent = file ? file.name : "No image chosen."; });
    $("#referencePanel").addEventListener("change", renderBackground);
    $("#placeFrameButton").addEventListener("click", placeFromFrame);
    $("#detachFrameButton").addEventListener("click", () => queueBackgroundEdit({ action: "detach" }));
    $("#calibrateButton").addEventListener("click", () => {
      if (!state.background) { toast("Choose a background image first.", true); return; }
      if (state.background.frameState === "attached") { toast("Detach the frame before you calibrate the scale.", true); return; }
      state.calibrating = true; state.calibrationPoints = []; $("#calibrationPanel").hidden = false; $("#finishCalibrationButton").disabled = true; updatePrompt(); renderMap();
    });
    $("#finishCalibrationButton").addEventListener("click", finishCalibration); $("#cancelCalibrationButton").addEventListener("click", () => { state.calibrating = false; state.calibrationPoints = []; $("#calibrationPanel").hidden = true; render(); });
    $("#parkRideHub").addEventListener("change", () => { parkRideStations = null; renderParkRide(draft(), draft().network.Stations.filter((station) => !station.ParkingOnly)); });
    $("#parkRideAddDestination").addEventListener("click", addParkRideDestination);
    $("#parkRideCreate").addEventListener("click", createParkRideProfile);
    bindScalarInput("dailyStartTime");
    for (const departure of [false, true]) {
      const addID = departure ? "#addRailDeparture" : "#addRailArrival", rowsID = departure ? "#railDepartureRows" : "#railArrivalRows", choices = departure ? "origins" : "destinations";
      const kind = departure ? "departure" : "arrival", key = departure ? "railDepartures" : "railArrivals";
      $(addID).addEventListener("click", () => { if (!railBusy(kind)) queueRailEdit(kind, { action: "add" }); });
      $(rowsID).addEventListener("input", (event) => { if (event.target.dataset.railField) markTyping(event.target); });
      $(rowsID).addEventListener("change", (event) => {
        const { railID, railField, railDestination } = event.target.dataset;
        if (!railID || !railField) return;
        const command = { action: "set", id: railID, field: railField, value: event.target.value };
        if (railDestination !== undefined) {
          command.index = Number(railDestination);
          command.choiceCount = draft()[key]?.find((item) => item.id === railID)?.[choices].length || 0;
        }
        queueRailEdit(kind, command, event.target);
      });
      $(rowsID).addEventListener("click", (event) => {
        const control = event.target.closest("button[data-rail-action]"); if (!control) return;
        if (railBusy(kind)) return;
        const { railID, railAction, railDestination } = control.dataset;
        const command = { action: railAction === "remove-arrival" ? "remove" : railAction === "remove-destination" ? "removeChoice" : "addChoice", id: railID };
        if (railDestination !== undefined) {
          command.index = Number(railDestination);
          command.choiceCount = draft()[key]?.find((item) => item.id === railID)?.[choices].length || 0;
        }
        queueRailEdit(kind, command, control);
      });
    }
    for (const id of ["demandEnabled", "demandRate", "demandPattern", "demandDestination", "demandProfile", "demandBand", "sharedRidePartyLimit", "sharedRideMode", "sharedRideJoin", "sharedRideMaxStops", "platoonLimit", "demandSeed", "redistribution", "stationBuffers", "stationQueueSpacing", "pickupReassignment"]) bindScalarInput(id);
    bindScalarInput("scenarioName", "name");
    $("#fleetControls").addEventListener("input", (event) => { if (event.target.dataset.station) markTyping(event.target); });
    $("#fleetControls").addEventListener("change", (event) => { if (event.target.dataset.station) queueScalarEdit("fleetCount", event.target, event.target.dataset.station); });
    $("#selectionContent").addEventListener("input", (event) => {
      if (event.target.dataset.edit || event.target.dataset.layout) markTyping(event.target);
    });
    $("#selectionContent").addEventListener("change", (event) => {
      const control = event.target, id = control.dataset.item;
      if (control.dataset.edit === "station-bank" && state.selection?.id === id) {
        for (const input of $("#selectionContent").querySelectorAll("input[data-layout]")) { typingInputs.delete(input); pendingInputs.delete(input); }
        state.selection.bank = control.value; render(); return;
      }
      const action = { "station-name": "stationName", "parking-only": "stationParking", "station-bearing": "stationBearing", "lane-speed": "laneSpeed" }[control.dataset.edit];
      if (!action || !id) return;
      queueGeometryEdit({ action, id, value: control.type === "checkbox" ? control.checked : control.value }, { controls: [control] });
    });
    // Keyboard deletion keeps focus near the removed item after acceptance.
    $("#selectionContent").addEventListener("click", (event) => {
      const button = event.target.closest("button[data-action]");
      if (!button || !button.dataset.item) return;
      const action = button.dataset.action, id = button.dataset.item, keyboard = event.detail === 0;
      if (action.startsWith("delete-")) { deleteSelectedItem({ type: button.dataset.kind, id }, { keyboard }); return; }
      if (action === "station-banks") {
        const control = $("#selectionContent").querySelector('[data-edit="station-banks"]');
        let value;
        try { value = JSON.parse(control.value); } catch (_) { toast("Bank membership must be a JSON array.", true); return; }
        queueGeometryEdit({ action: "stationBanks", id, value }, { controls: [control] }); return;
      }
      if (action === "station-legacy") { queueGeometryEdit({ action: "stationLegacy", id }); return; }
      if (action === "station-layout") {
        const fields = [...$("#selectionContent").querySelectorAll("input[data-layout]")].filter((input) => !input.disabled && input.value !== input.dataset.layoutValue);
        if (fields.some((input) => !Number.isFinite(input.valueAsNumber))) { toast("Station dimensions must be finite numbers.", true); return; }
        const dimensions = Object.fromEntries(fields.map((input) => [input.dataset.layout, input.valueAsNumber]));
        if (!fields.length) return;
        queueGeometryEdit(stationGeometryCommand(draft(), id, "stationLayout", dimensions, $("#selectionContent").layoutBank), { controls: fields });
        return;
      }
      const command = stationGeometryCommand(draft(), id, { "add-berth": "addBerth", "remove-berth": "removeBerth", "toggle-curve": "toggleCurve" }[action], undefined, $("#selectionContent").layoutBank);
      if (!command.action) return;
      const berthIDs = action === "remove-berth" ? draft().network.Stations.find((station) => station.ID === id)?.Berths.map((berth) => berth.ID) || [] : [];
      if (action === "remove-berth") command.value = button.dataset.id;
      queueGeometryEdit(command, { accepted: () => {
        const ownsFocus = document.activeElement === button;
        render();
        if (keyboard && ownsFocus && !button.isConnected && state.selection?.id === id) {
          if (action === "remove-berth") focusBerthControl(berthFocusID(berthIDs, button.dataset.id));
          else focusMap();
        }
      } });
    });
    document.addEventListener("keydown", (event) => {
      const editing = /INPUT|SELECT|TEXTAREA/.test(document.activeElement.tagName);
      if (event.key === "Escape") { state.linkFrom = ""; state.calibrating = false; state.calibrationPoints = []; $("#calibrationPanel").hidden = true; render(); }
      if (!editing && (event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "z") { event.preventDefault(); stepHistory(event.shiftKey); }
      if (!editing && (event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "y") { event.preventDefault(); stepHistory(true); }
      // A delete of the selected item clears the Selection panel. When a
      // panel button had the focus, the map gets the focus.
      if (!editing && (event.key === "Delete" || event.key === "Backspace") && state.selection) {
        event.preventDefault(); deleteSelectedItem({ ...state.selection }, { keyboard: true });
      }
    });
  }

  // The startup gate blocks input until the live scenario and the stored
  // background are on the page. See startEditor. When a startup step
  // fails, the error toast tells the user to reload the page.
  const gate = createStartupGate();
  blockInput(document, gate, startupExempt);
  bindEvents(); render();
  startEditor({
    readDraft: () => keeper.load(), readBackground: () => backgroundKeeper.load(), loadLive: loadServerProject, restore: restoreBackground,
    release: gate.release, armBackground: () => backgroundKeeper.arm(), offerDraft,
  }).catch((error) => toast(`The editor could not start. ${error.message} Reload the page.`, true));
})(typeof window !== "undefined" ? window : globalThis);
