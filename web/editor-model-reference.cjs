"use strict";

// These former model helpers run only in Node parity tests.
module.exports = (helpers) => {
  const { hasServiceMetadata } = require("./editor-service-reference.cjs");
  const {
    ANCHOR_MAX_RESIDUAL, ANCHOR_MIN_DISTANCE, BERTH_PITCH, CLEARANCE, DEFAULT_OPACITY, DEFAULT_SPEED,
    DEGREE, GEO_MAX_LATITUDE, GEO_PROJECTION, GEO_RADIUS, MAX_BERTHS, MAX_COORDINATE,
    MAX_LANES, MAX_NODES, MAX_NODE_LANES, MAX_STATIONS, MIN_LANE_LENGTH, SCALE_TOLERANCE,
    Tiles, berthChain, clone, emptyConfig, flowNamesStation, frameError,
    framePlacement, frozenDrafts, laneLength, point, projectPoint, shiftNodes,
    stationAxes, stationBearing, stationLayout, stationNodeIDs, stationNodeOwners,
  } = helpers;
  const STATION_LANE_ROLES = new Set(["approach", "entry", "berth-access", "through", "departure", "exit"]);

  // sharedRideModes mirrors the modes of sim.SharedRideMode. The first
  // mode is sim.DefaultSharedRideMode.
  const sharedRideModes = ["drop-offs", "destination"];

  const sharedRideJoins = ["unassigned", "reassign-existing"];

  // platoonLimits holds the platoon limits that project.Validate accepts. 0
  // turns platoons off.
  const platoonLimits = [0, 2, 3, 4];

  // freezeDraft makes history snapshots safe to share with read-only callers.
  function freezeDraft(value) {
    if (!value || typeof value !== "object" || frozenDrafts.has(value)) return value;
    for (const child of Object.values(value)) freezeDraft(child);
    frozenDrafts.add(value); return Object.freeze(value);
  }

  // editDraft copies only the branches that a mutation changes. The callback
  // receives temporary proxies. No proxy enters a history snapshot.
  function editDraft(value, change) {
    const states = new WeakMap();
    function wrap(base) {
      const target = Array.isArray(base) ? base.slice() : { ...base };
      const children = new Map();
      const proxy = new Proxy(target, {
        get(object, key) {
          const child = object[key];
          if (!child || typeof child !== "object") return child;
          if (!children.has(key)) children.set(key, wrap(child));
          return children.get(key);
        },
        set(object, key, next) { children.delete(key); object[key] = next; return true; },
        deleteProperty(object, key) { children.delete(key); delete object[key]; return true; },
      });
      states.set(proxy, () => {
        for (const [key, child] of children) if (Object.hasOwn(target, key)) target[key] = finish(child);
        for (const key of Object.keys(target)) target[key] = finish(target[key]);
        const keys = Object.keys(target);
        return (!Array.isArray(base) || target.length === base.length) && keys.length === Object.keys(base).length && keys.every((key) => Object.hasOwn(base, key) && Object.is(target[key], base[key])) ? base : target;
      });
      return proxy;
    }
    function finish(item) {
      if (!item || typeof item !== "object" || frozenDrafts.has(item)) return item;
      if (states.has(item)) return states.get(item)();
      const out = Array.isArray(item) ? item.slice() : { ...item };
      for (const key of Object.keys(out)) out[key] = finish(out[key]);
      return out;
    }
    const proxy = wrap(value);
    return finish(change(proxy) ?? proxy);
  }

  function normalizeConfig(input) {
    const config = clone(input || emptyConfig());
    if (hasServiceMetadata(config) && config.version !== 3) throw new Error("Vehicle and service fields require project version 3.");
    if (config.version !== 3) config.version = config.network?.Stations?.some((station) => station && Object.hasOwn(station, "Banks")) ? 2 : 1;
    config.name = typeof config.name === "string" ? config.name : "Untitled scenario";
    config.network = config.network || {};
    config.network.Nodes = Array.isArray(config.network.Nodes) ? config.network.Nodes : [];
    config.network.Lanes = Array.isArray(config.network.Lanes) ? config.network.Lanes : [];
    config.network.Stations = Array.isArray(config.network.Stations) ? config.network.Stations : [];
    config.fleet = Array.isArray(config.fleet) ? config.fleet : [];
    config.demandProfiles = Array.isArray(config.demandProfiles) ? config.demandProfiles : [];
    if (config.map && config.map.opacity === undefined) config.map.opacity = 0;
    for (const pod of config.fleet) {
      if (pod && !pod.BerthID) {
        const station = config.network.Stations.find((item) => item && item.ID === pod.StationID);
        pod.BerthID = station?.Berths?.[0]?.ID || "";
      }
    }
    config.demand = config.demand || {};
    config.demand.enabled = Boolean(config.demand.enabled);
    config.demand.perMinute = Number(config.demand.perMinute) || 2;
    if (config.demand.pattern === "market") {
      config.demand.pattern = "destination";
      if (!config.demand.destination) {
        const market = config.network.Stations.find((station) => station && !station.ParkingOnly && station.ID === "market");
        const first = config.network.Stations.filter((station) => station && !station.ParkingOnly).at(-1);
        config.demand.destination = (market || first || {}).ID || "";
      }
    }
    if (!["destination", "profile", "profile-daily", "rail-arrivals", "rail-services"].includes(config.demand.pattern)) config.demand.pattern = "balanced";
    config.demand.destination = typeof config.demand.destination === "string" ? config.demand.destination : "";
    config.demand.profile = typeof config.demand.profile === "string" ? config.demand.profile : "";
    config.demand.band = typeof config.demand.band === "string" ? config.demand.band : "";
    if (config.demandProfiles.length && !config.demandProfiles.some((profile) => profile && profile.id === config.demand.profile)) config.demand.profile = config.demandProfiles[0].id;
    const demandProfile = config.demandProfiles.find((profile) => profile && profile.id === config.demand.profile);
    if (demandProfile && Array.isArray(demandProfile.bands) && !demandProfile.bands.some((band) => band && band.id === config.demand.band)) config.demand.band = demandProfile.bands[0]?.id || "";
    config.demand.seed = Math.max(0, Math.floor(Number(config.demand.seed) || 0));
    config.sharedRidePartyLimit = Math.max(1, Math.min(8, Math.floor(Number(config.sharedRidePartyLimit) || 1)));
    config.sharedRideMode = sharedRideModes.includes(config.sharedRideMode) ? config.sharedRideMode : "drop-offs";
    config.sharedRideJoin = sharedRideJoins.includes(config.sharedRideJoin) ? config.sharedRideJoin : "unassigned";
    config.sharedRideMaxStops = Math.max(1, Math.min(7, Math.floor(Number(config.sharedRideMaxStops) || 3)));
    config.platoonLimit = platoonLimits.includes(config.platoonLimit) ? config.platoonLimit : 0;
    config.stationBuffers = config.stationBuffers === true;
    config.pickupReassignment = config.pickupReassignment === true;
    config.redistribution = Boolean(config.redistribution);
    inferStationLanes(config.network);
    return config;
  }

  // inferStationLanes gives station lanes without StationID and StationRole
  // the values that sim.inferStationLaneRoles gives them. A legacy or a
  // hand-written project can have such lanes, and removeBerth accepts only
  // station lanes on a berth node. For each station, in station order, a
  // lane from the entry to the exit gets the through role. Then, for each
  // berth, the lanes of the route from the entry to the berth get the
  // berth-access role, and the lanes of the route from the berth to the
  // exit get the departure role. The first station and route that finds a
  // lane gives its values. A lane with one of the two fields keeps its
  // values. The simulation checks only StationRole, but in a valid network
  // a lane has both fields or neither. The function changes network in
  // place. It gives the number of lane roles that it kept for all stations.
  // Each station keeps at most one role for each lane.
  //
  // The simulation infers the roles only after it validates the network. A
  // restored draft can hold an unfinished network, so the function does
  // nothing when inferableStations or stationRouteGraph rejects the
  // network. Then the route costs are positive and finite, and the size of
  // the network is in the project limits. When a route search still gives
  // a broken route, the station gets no values.
  // TestStationLaneRolesGolden in internal/sim writes the roles that the
  // editor test compares with.
  function inferStationLanes(network) {
    const { Nodes, Lanes, Stations } = network;
    if (!inferableStations(network)) return 0;
    const graph = stationRouteGraph(network);
    if (!graph) return 0;
    const forbidden = new Set(Stations.flatMap((station) => [station.Entry, station.Exit, ...station.Berths.map((berth) => berth.Node)]));
    let kept = 0;
    for (const station of Stations) {
      if (Object.hasOwn(station, "Banks")) continue;
      const roles = new Map();
      const keep = (index, role) => { if (!roles.has(index)) roles.set(index, role); };
      Lanes.forEach((lane, index) => { if (lane.From === station.Entry && lane.To === station.Exit) keep(index, "through"); });
      let broken = false;
      for (const berth of station.Berths) {
        for (const [from, to, role] of [[station.Entry, berth.Node, "berth-access"], [berth.Node, station.Exit, "departure"]]) {
          const route = stationRoute(graph, from, to, forbidden);
          if (route === false) broken = true;
          for (const index of route || []) keep(index, role);
        }
      }
      kept += roles.size;
      if (broken) continue;
      for (const [index, role] of roles) {
        const lane = Lanes[index];
        if (lane.StationID || lane.StationRole) continue;
        lane.StationID = station.ID;
        lane.StationRole = role;
      }
    }
    return kept;
  }

  // inferableStations reports whether the network is in the project size
  // limits and has the station structure that sim.Network.validate needs.
  // Each item of the network is an object, and each node has a position
  // object. Each station has a unique ID that is not empty, an entry and
  // an exit that are different known nodes, and 1 to MAX_BERTHS berths.
  // Each berth has a unique ID that is not empty and a known node that no
  // other berth uses and that is not the entry or the exit. Thus the
  // inference runs at most one route search in each direction for each
  // berth node.
  function inferableStations(network) {
    const { Nodes, Lanes, Stations } = network;
    const record = (item) => item !== null && typeof item === "object";
    if (Nodes.length > MAX_NODES || Lanes.length > MAX_LANES || Stations.length > MAX_STATIONS) return false;
    if (![...Nodes, ...Lanes, ...Stations].every(record) || !Nodes.every((node) => record(node.Position))) return false;
    const nodeIDs = new Set(Nodes.map((node) => node.ID));
    const stationIDs = new Set(); const berthIDs = new Set(); const berthNodes = new Set();
    const validID = (id, ids) => typeof id === "string" && id !== "" && !ids.has(id) && Boolean(ids.add(id));
    for (const station of Stations) {
      if (!validID(station.ID, stationIDs) || !nodeIDs.has(station.Entry) || !nodeIDs.has(station.Exit) || station.Entry === station.Exit) return false;
      if (!Array.isArray(station.Berths) || station.Berths.length === 0 || station.Berths.length > MAX_BERTHS) return false;
      for (const berth of station.Berths) {
        if (!record(berth) || !validID(berth.ID, berthIDs) || !nodeIDs.has(berth.Node) || berthNodes.has(berth.Node)) return false;
        if (berth.Node === station.Entry || berth.Node === station.Exit) return false;
        berthNodes.add(berth.Node);
      }
    }
    return true;
  }

  // stationRouteGraph gives the route graph of sim.newRouteGraph, or null
  // when sim.Network.validate would not accept the nodes and the lanes. A
  // node must have a unique ID that is not empty and a finite position. A
  // lane must have a unique ID that is not empty, known end nodes, a finite
  // speed limit above 0, and a finite length above 0. nodes maps each node
  // ID to its index. outgoing holds the lane indexes out of each node.
  // edges holds, for each lane index, the node indexes of the lane ends and
  // the free-flow travel time. to holds the node ID at the end of each
  // lane.
  function stationRouteGraph(network) {
    const nodes = new Map(); const laneIDs = new Set();
    const finiteNumber = (value) => typeof value === "number" && Number.isFinite(value);
    for (const [index, node] of network.Nodes.entries()) {
      if (typeof node.ID !== "string" || !node.ID || nodes.has(node.ID) || !finiteNumber(node.Position.X) || !finiteNumber(node.Position.Y)) return null;
      nodes.set(node.ID, index);
    }
    const outgoing = network.Nodes.map(() => []);
    const edges = [];
    for (const [index, lane] of network.Lanes.entries()) {
      const from = nodes.get(lane.From); const to = nodes.get(lane.To);
      if (typeof lane.ID !== "string" || !lane.ID || laneIDs.has(lane.ID) || from === undefined || to === undefined) return null;
      const length = simLaneLength(network.Nodes[from].Position, network.Nodes[to].Position, lane.Control);
      if (!finiteNumber(lane.SpeedLimit) || lane.SpeedLimit <= 0 || !Number.isFinite(length) || length <= 0) return null;
      laneIDs.add(lane.ID);
      outgoing[from].push(index);
      edges.push({ from, to, seconds: length / lane.SpeedLimit });
    }
    return { nodes, outgoing, edges, to: network.Lanes.map((lane) => lane.To) };
  }

  // simLaneLength gives the length of a lane as sim.indexedLaneLength gives
  // it. A curve has 64 straight parts. curveLength uses 16 parts, so its
  // length is a little different, and a route search with it could choose
  // a different route.
  function simLaneLength(from, to, control) {
    if (!control) return goHypot(to.X - from.X, to.Y - from.Y);
    let length = 0;
    const points = lanePolyline(from, to, control);
    for (let i = 1; i < points.length; i += 1) length += goHypot(points[i].X - points[i - 1].X, points[i].Y - points[i - 1].Y);
    return length;
  }

  // goHypot gives the length of the vector (dx, dy) with the operations of
  // math.Hypot in Go. Math.hypot can give a different last bit, and then two
  // routes with the same cost in Go could have different costs here.
  function goHypot(dx, dy) {
    let p = Math.abs(dx); let q = Math.abs(dy);
    if (p < q) [p, q] = [q, p];
    if (p === 0) return 0;
    q /= p;
    return p * Math.sqrt(1 + q * q);
  }

  // stationRoute gives the lane indexes of the route from node fromID to node
  // to, as sim.Network.routeIndexed gives them with the forbidden set of
  // station nodes. fromID and toID are the node IDs. It gives null when
  // there is no route. It gives false when the route back from the end
  // node has more lanes than the graph has nodes, because then the route
  // goes around a loop. With the costs that stationRouteGraph accepts, this
  // does not occur. The route has the
  // lowest total travel time. The route cannot pass through a node in
  // forbidden, but it can start or end at one. The search takes the node
  // with the lowest cost from the queue, and the lowest node index between
  // equal costs. A lane replaces the route to its end node only when it
  // makes the cost lower, so between equal routes the first lane found
  // stays. Thus the route is the same as in Go.
  function stationRoute(graph, fromID, toID, forbidden) {
    const from = graph.nodes.get(fromID); const to = graph.nodes.get(toID);
    if (from === undefined || to === undefined) return null;
    const count = graph.outgoing.length;
    const distance = new Float64Array(count).fill(Infinity);
    const previous = new Int32Array(count).fill(-1);
    const visited = new Uint8Array(count);
    distance[from] = 0;
    const queue = createRouteQueue();
    queue.push(0, from);
    while (queue.size()) {
      const [cost, node] = queue.pop();
      if (visited[node] || cost !== distance[node]) continue;
      if (node === to) break;
      visited[node] = 1;
      for (const index of graph.outgoing[node]) {
        const edge = graph.edges[index];
        if (edge.to !== from && edge.to !== to && forbidden.has(graph.to[index])) continue;
        const candidate = cost + edge.seconds;
        if (candidate < distance[edge.to]) {
          distance[edge.to] = candidate; previous[edge.to] = index;
          queue.push(candidate, edge.to);
        }
      }
    }
    if (distance[to] === Infinity) return null;
    const route = [];
    for (let current = to; current !== from; current = graph.edges[route.at(-1)].from) {
      if (previous[current] < 0) return null;
      if (route.length >= count) return false;
      route.push(previous[current]);
    }
    return route.reverse();
  }

  // createRouteQueue gives a binary heap of [cost, node] items. pop gives
  // the item with the lowest cost, and the lowest node between equal costs.
  function createRouteQueue() {
    const items = [];
    const less = (a, b) => (a[0] === b[0] ? a[1] < b[1] : a[0] < b[0]);
    const swap = (i, j) => { [items[i], items[j]] = [items[j], items[i]]; };
    return {
      size: () => items.length,
      push(cost, node) {
        items.push([cost, node]);
        for (let child = items.length - 1; child > 0;) {
          const parent = (child - 1) >> 1;
          if (!less(items[child], items[parent])) break;
          swap(child, parent); child = parent;
        }
      },
      pop() {
        const root = items[0]; const last = items.pop();
        if (!items.length) return root;
        items[0] = last;
        for (let parent = 0; ;) {
          const left = parent * 2 + 1; const right = left + 1;
          if (left >= items.length) break;
          const child = right < items.length && less(items[right], items[left]) ? right : left;
          if (!less(items[child], items[parent])) break;
          swap(child, parent); parent = child;
        }
        return root;
      },
    };
  }

  function allIDs(config) {
    const ids = [];
    for (const node of config.network.Nodes) ids.push(node.ID);
    for (const lane of config.network.Lanes) ids.push(lane.ID);
    for (const station of config.network.Stations) {
      ids.push(station.ID);
      for (const berth of station.Berths || []) ids.push(berth.ID);
    }
    for (const pod of config.fleet) ids.push(pod.ID);
    return new Set(ids);
  }

  function nextID(config, prefix) {
    const ids = allIDs(config);
    for (let i = 1; ; i += 1) {
      const id = `${prefix}-${i}`;
      if (!ids.has(id)) return id;
    }
  }

  function addLane(config, from, to, paired, control) {
    if (!point(config, from) || !point(config, to) || from === to) return config;
    const out = clone(config);
    const exists = out.network.Lanes.some((lane) => lane.From === from && lane.To === to);
    if (!exists) {
      const lane = { ID: nextID(out, "lane"), From: from, To: to, SpeedLimit: DEFAULT_SPEED };
      if (control) lane.Control = { X: Number(control.X), Y: Number(control.Y) };
      out.network.Lanes.push(lane);
    }
    if (paired && !out.network.Lanes.some((lane) => lane.From === to && lane.To === from)) {
      const lane = { ID: nextID(out, "lane"), From: to, To: from, SpeedLimit: DEFAULT_SPEED };
      if (control) lane.Control = { X: Number(control.X), Y: Number(control.Y) };
      out.network.Lanes.push(lane);
    }
    return out;
  }

  function addStationLane(config, from, to, stationID, stationRole) {
    const out = addLane(config, from, to, false);
    const lane = out.network.Lanes.find((item) => item.From === from && item.To === to);
    if (lane) {
      lane.StationID = stationID;
      lane.StationRole = stationRole;
    }
    return out;
  }

  function addJunction(config, x, y) {
    const out = clone(config);
    out.network.Nodes.push({ ID: nextID(out, "node"), Position: { X: x, Y: y } });
    return out;
  }

  function addStation(config, x, y, options) {
    const out = clone(config);
    const stationID = nextID(out, "station");
    const entryID = nextID(out, `${stationID}-entry`);
    out.network.Nodes.push({ ID: entryID, Position: { X: x - 36, Y: y } });
    const exitID = nextID(out, `${stationID}-exit`);
    out.network.Nodes.push({ ID: exitID, Position: { X: x + 36, Y: y } });
    const berthID = nextID(out, `${stationID}-berth`);
    const berthNodeID = nextID(out, `${stationID}-berth-node`);
    out.network.Nodes.push({ ID: berthNodeID, Position: { X: x, Y: y + BERTH_PITCH } });
    const station = {
      ID: stationID,
      Name: (options && options.name) || `Station ${out.network.Stations.length + 1}`,
      Entry: entryID,
      Exit: exitID,
      Berths: [{ ID: berthID, Node: berthNodeID }],
      ParkingOnly: Boolean(options && options.parkingOnly),
    };
    out.network.Stations.push(station);
    const withInlet = addStationLane(out, entryID, berthNodeID, stationID, "berth-access");
    const withOutlet = addStationLane(withInlet, berthNodeID, exitID, stationID, "departure");
    return addStationLane(withOutlet, entryID, exitID, stationID, "through");
  }

  // rotateStation turns the station nodes around the station center by the
  // given number of degrees. A positive value turns them clockwise on the
  // map. The station nodes are the nodes that stationNodeIDs gives. The
  // curve control point of each lane between two station nodes turns too.
  // Each lane keeps its end nodes, so a lane from a station node to a
  // junction stays attached to both nodes.
  function rotateStation(config, stationID, degrees) {
    const out = clone(config);
    const station = out.network.Stations.find((item) => item.ID === stationID);
    const entry = station && point(out, station.Entry); const exit = station && point(out, station.Exit);
    if (!entry || !exit) return config;
    const center = stationAxes(entry, exit).origin; const ids = stationNodeIDs(out, station);
    const cos = Math.cos(degrees * Math.PI / 180); const sin = Math.sin(degrees * Math.PI / 180);
    const turn = (at) => { const x = at.X - center.X; const y = at.Y - center.Y; at.X = center.X + x * cos - y * sin; at.Y = center.Y + x * sin + y * cos; };
    for (const node of out.network.Nodes) if (ids.has(node.ID)) turn(node.Position);
    for (const lane of out.network.Lanes) if (lane.Control && ids.has(lane.From) && ids.has(lane.To)) turn(lane.Control);
    return out;
  }

  // setStationBearing turns the station around its center until its
  // bearing, as stationBearing gives it, is the given number of degrees. It
  // turns the station by at most 180 degrees. When the station already has
  // the bearing, the function gives the same config, so that the history
  // does not record a change.
  function setStationBearing(config, stationID, bearing) {
    const station = config.network.Stations.find((item) => item.ID === stationID);
    const entry = station && point(config, station.Entry); const exit = station && point(config, station.Exit);
    if (!entry || !exit || !Number.isFinite(bearing)) return config;
    const degrees = ((bearing - stationBearing(entry, exit)) % 360 + 540) % 360 - 180;
    return degrees ? rotateStation(config, stationID, degrees) : config;
  }

  // nextBerthPosition gives the position of a new berth node. station has
  // the positions of the entry, the exit, and the berth nodes. The station
  // axis goes across the entry-exit line, to the side of the mean berth
  // position. When the mean is on the line, or the station has no berths,
  // the axis points to the right of the direction of travel, as addStation
  // places the first berth. The last berth is the berth that is farthest
  // along the axis. The new berth is BERTH_PITCH meters past the last berth
  // along the axis. A station with no berths gets the new berth BERTH_PITCH
  // meters from its center.
  function nextBerthPosition(station) {
    const { origin, across } = stationAxes(station.entry, station.exit);
    const depth = (at) => (at.X - origin.X) * across.X + (at.Y - origin.Y) * across.Y;
    const side = station.berths.reduce((sum, at) => sum + depth(at), 0) < 0 ? -1 : 1;
    const start = station.berths.reduce((far, at) => (!far || depth(at) * side > depth(far) * side ? at : far), null) || origin;
    return { X: start.X + across.X * side * BERTH_PITCH, Y: start.Y + across.Y * side * BERTH_PITCH };
  }

  // addBerth adds a berth to the station. It gives the new config and an
  // empty error, or the same config and an error that names the station.
  // On a berth chain station, as berthChain finds it, the new berth is the
  // row that nextChainRow gives. When a lane of that row crosses another
  // lane, or comes nearer than CLEARANCE to it, the editor does not add the
  // berth. On another station, the new berth goes to the position that
  // nextBerthPosition gives. It gets a lane from the station entry and a
  // lane to the station exit.
  //
  // Each new lane has a new node at one end, so the berth adds no lane
  // with the nodes and the path of another lane. When a
  // node of the new lanes would have more than MAX_NODE_LANES lanes, the
  // editor does not add the berth. On a station that is not a chain, the
  // entry and the exit have a lane for each berth, so this limits the
  // berths of such a station.
  function addBerth(config, stationID) {
    const station = config.network.Stations.find((item) => item.ID === stationID);
    const rows = station && berthChain(config, station);
    if (rows) return addChainBerth(config, station, rows);
    const out = addStarBerth(config, stationID);
    const busy = out === config ? null : busyNode(config, out);
    return busy ? { config, error: busyText(station, busy) } : { config: out, error: "" };
  }

  function busyText(station, busy) {
    return `No space for another berth at ${station.Name || station.ID}. Node ${busy.node} would have ${busy.count} lanes, more than ${MAX_NODE_LANES}.`;
  }

  // busyNode gives the first node that has more than MAX_NODE_LANES lanes
  // in after, and more lanes than in before, with its lane count. It gives
  // null when there is no such node. A lane counts at its From node and at
  // its To node.
  function busyNode(before, after) {
    const counts = (config) => {
      const count = new Map();
      for (const lane of config.network.Lanes) for (const node of [lane.From, lane.To]) count.set(node, (count.get(node) || 0) + 1);
      return count;
    };
    const old = counts(before);
    for (const [node, count] of counts(after)) {
      if (count > MAX_NODE_LANES && count > (old.get(node) || 0)) return { node, count };
    }
    return null;
  }

  function addStarBerth(config, stationID) {
    const out = clone(config);
    const station = out.network.Stations.find((item) => item.ID === stationID);
    if (!station) return config;
    const entry = point(out, station.Entry);
    const exit = point(out, station.Exit);
    if (!entry || !exit) return config;
    const berthID = nextID(out, `${station.ID}-berth`);
    const nodeID = nextID(out, `${station.ID}-berth-node`);
    const berths = station.Berths.map((berth) => point(out, berth.Node)).filter(Boolean);
    out.network.Nodes.push({ ID: nodeID, Position: nextBerthPosition({ entry, exit, berths }) });
    station.Berths.push({ ID: berthID, Node: nodeID });
    return addStationLane(addStationLane(out, station.Entry, nodeID, stationID, "berth-access"), nodeID, station.Exit, stationID, "departure");
  }

  function addChainBerth(config, station, rows) {
    const row = nextChainRow(config, station, rows);
    const out = clone(config);
    out.network.Nodes.push(...row.nodes);
    out.network.Lanes.push(...row.lanes);
    out.network.Stations.find((item) => item.ID === station.ID).Berths.push(row.berth);
    const busy = busyNode(config, out);
    if (busy) return { config, error: busyText(station, busy) };
    const conflict = laneConflict(out, row.lanes.map((lane) => lane.ID));
    if (!conflict) return { config: out, error: "" };
    const problem = conflict.gap === 0 ? "cross" : `be nearer than ${CLEARANCE} m to`;
    return { config, error: `No space for another berth at ${station.Name || station.ID}. New lane ${conflict.lane} would ${problem} lane ${conflict.other}.` };
  }

  // A dimension edit moves only ordinary node coordinates. Rejected edits
  // retain the original draft, including fleet placement and lane metadata.
  function setStationLayout(config, stationID, dimensions) {
    const layout = stationLayout(config, stationID); const name = layout.station?.Name || stationID;
    const reject = (reason) => ({ config, error: `${name}: ${reason}` });
    if (layout.error) return reject(layout.error);
    const keys = Object.keys(dimensions);
    for (const key of keys) {
      if (!["pitch", "spacing", "setback"].includes(key)) return reject(`Unknown dimension ${key}.`);
      if (layout[key] === null) return reject(key === "pitch" ? "Berth pitch requires at least two rows." : "Approach setback requires a paired, aligned throat.");
      const minimum = key === "pitch" ? 25 : key === "spacing" ? 2 * MIN_LANE_LENGTH : Number.MIN_VALUE;
      if (!Number.isFinite(dimensions[key]) || dimensions[key] < minimum) return reject(`${key} must be a finite ${key === "setback" ? "positive value" : `value of at least ${minimum} m`}.`);
    }
    if (!keys.some((key) => Math.abs(dimensions[key] - layout[key]) > 1e-6)) return { config, error: "" };
    const out = clone(config); const moves = new Map();
    const add = (id, along, across) => {
      const old = moves.get(id) || { X: 0, Y: 0 };
      moves.set(id, { X: old.X + along * layout.frame.along.X + across * layout.frame.across.X, Y: old.Y + along * layout.frame.along.Y + across * layout.frame.across.Y });
    };
    const spacing = (dimensions.spacing ?? layout.spacing) - layout.spacing;
    if (spacing) {
      add(layout.station.Entry, -spacing / 2, 0); add(layout.station.Exit, spacing / 2, 0);
      for (const row of layout.rows) { add(row.arrival, -spacing / 2, 0); add(row.departure, spacing / 2, 0); }
    }
    const pitch = (dimensions.pitch ?? layout.pitch) - layout.pitch;
    if (pitch) layout.rows.forEach((row, index) => { for (const id of [row.arrival, row.berth.Node, row.departure]) add(id, 0, layout.side * index * pitch); });
    const setback = (dimensions.setback ?? layout.setback) - layout.setback;
    if (setback) for (const id of layout.body) add(id, 0, layout.side * setback);
    const moved = new Set();
    for (const node of out.network.Nodes) {
      const delta = moves.get(node.ID); if (!delta || (!delta.X && !delta.Y)) continue;
      node.Position.X += delta.X; node.Position.Y += delta.Y; moved.add(node.ID);
      if (![node.Position.X, node.Position.Y].every((value) => Number.isFinite(value) && Math.abs(value) <= MAX_COORDINATE)) return reject(`Node ${node.ID} exceeds the coordinate limit.`);
    }
    const lanes = out.network.Lanes.filter((lane) => moved.has(lane.From) || moved.has(lane.To));
    const short = lanes.find((lane) => laneLength(out, lane) < MIN_LANE_LENGTH);
    if (short) return reject(`Lane ${short.ID} would be shorter than ${MIN_LANE_LENGTH} m.`);
    const conflict = laneConflict(out, lanes.map((lane) => lane.ID), true);
    if (conflict) return reject(`Lane ${conflict.lane} would ${conflict.gap === 0 ? "cross" : `come within ${conflict.gap.toFixed(1)} m of`} lane ${conflict.other}.`);
    return { config: out, error: "" };
  }

  // nextChainRow gives the next row of a berth chain. rows is the chain that
  // berthChain gives. The row is a copy of the last row, moved by one pitch.
  // The pitch is the distance from the berth node of the row before the
  // last row to the last berth node. With one row, it is the distance from
  // the middle of the entry-exit line to the berth node. Thus the station
  // keeps the pitch and the direction of its chain. The copy keeps all
  // other values of the nodes, lanes and berth, such as the speed limit and
  // the separation group. A lane curve moves with the lane. The row has
  // three nodes, arrival, berth and departure, and four lanes, the arrival
  // link, the departure link, the lane in and the lane out, in the order
  // that the scenario generators use. Each new lane gets the station ID in
  // StationID. The arrival link and the lane in get the berth-access role,
  // and the departure link and the lane out get the departure role, as the
  // scenario generators give them. Thus removeBerth accepts the lanes of
  // the new row, also when the other chain lanes do not have these fields.
  // A new ID adds one to the last number in the ID of the copied item, with
  // the same number of digits. Thus a generated station gets the IDs that the generator
  // gives to one more berth. When that ID is in use, or has no number, the
  // item gets an editor ID. The function gives berth, nodes and lanes.
  function nextChainRow(config, station, rows) {
    const at = (id) => point(config, id);
    const last = rows.at(-1); const end = at(last.berth.Node);
    const start = rows.length > 1 ? at(rows.at(-2).berth.Node) : stationAxes(at(station.Entry), at(station.Exit)).origin;
    const shift = { X: end.X - start.X, Y: end.Y - start.Y };
    const moved = (from) => ({ X: from.X + shift.X, Y: from.Y + shift.Y });
    const ids = allIDs(config);
    const nodes = [last.arrival, last.berth.Node, last.departure].map((id, index) => {
      const node = clone(config.network.Nodes.find((item) => item.ID === id));
      return { ...node, ID: chainID(ids, id, index === 1 ? `${station.ID}-berth-node` : "node"), Position: moved(node.Position) };
    });
    const [arrival, berthNode, departure] = nodes.map((node) => node.ID);
    const lanes = [
      [last.arrivalLink, last.arrival, arrival, "berth-access"], [last.departureLink, departure, last.departure, "departure"],
      [last.inLane, arrival, berthNode, "berth-access"], [last.outLane, berthNode, departure, "departure"],
    ].map(([lane, from, to, role]) => {
      const copy = { ...clone(lane), ID: chainID(ids, lane.ID, "lane"), From: from, To: to, StationID: station.ID, StationRole: role };
      if (lane.Control) copy.Control = moved(lane.Control);
      return copy;
    });
    const berth = { ...clone(last.berth), ID: chainID(ids, last.berth.ID, `${station.ID}-berth`), Node: berthNode };
    return { berth, nodes, lanes };
  }

  // chainID gives a new ID for a copy of the item with the ID old. It adds
  // one to the last number in old and keeps the number of digits, so that
  // 940GZZLUKSX-02-node gives 940GZZLUKSX-03-node. When that ID is in ids,
  // or old has no number, it gives the first free editor ID with the prefix.
  // It adds the new ID to ids.
  function chainID(ids, old, prefix) {
    const match = /(\d+)(\D*)$/.exec(old);
    let id = match ? `${old.slice(0, match.index)}${String(Number(match[1]) + 1).padStart(match[1].length, "0")}${match[2]}` : "";
    for (let i = 1; !id || ids.has(id); i += 1) id = `${prefix}-${i}`;
    ids.add(id);
    return id;
  }

  // lanePolyline gives the path of a lane that a pod follows in the
  // simulation, as sim.Network.lanePoints gives it. A straight lane gives its
  // two end positions. A curved lane is a quadratic curve through its
  // control point, and gives 65 points, so that the path has 64 straight
  // parts.
  function lanePolyline(from, to, control) {
    if (!control) return [from, to];
    const points = [];
    for (let i = 0; i <= 64; i += 1) {
      const t = i / 64; const u = 1 - t;
      points.push({ X: u * u * from.X + 2 * u * t * control.X + t * t * to.X, Y: u * u * from.Y + 2 * u * t * control.Y + t * t * to.Y });
    }
    return points;
  }

  // pointGap gives the distance from at to the segment from a to b.
  function pointGap(at, a, b) {
    const dx = b.X - a.X; const dy = b.Y - a.Y; const size = dx * dx + dy * dy;
    const t = size ? Math.min(1, Math.max(0, ((at.X - a.X) * dx + (at.Y - a.Y) * dy) / size)) : 0;
    return Math.hypot(at.X - a.X - t * dx, at.Y - a.Y - t * dy);
  }

  // segmentGap gives the distance between the segments a-b and c-d. It is
  // 0 when the segments cross or touch.
  function segmentGap(a, b, c, d) {
    const side = (p, q, r) => Math.sign((q.X - p.X) * (r.Y - p.Y) - (q.Y - p.Y) * (r.X - p.X));
    if (side(a, b, c) * side(a, b, d) < 0 && side(c, d, a) * side(c, d, b) < 0) return 0;
    return Math.min(pointGap(a, c, d), pointGap(b, c, d), pointGap(c, a, b), pointGap(d, a, b));
  }

  // pathGap gives the distance between two lane paths from lanePolyline.
  function pathGap(first, second) {
    let gap = Infinity;
    for (let i = 1; i < first.length; i += 1) {
      for (let j = 1; j < second.length; j += 1) gap = Math.min(gap, segmentGap(first[i - 1], first[i], second[j - 1], second[j]));
    }
    return gap;
  }

  // laneConflict finds a lane that crosses one of the lanes in ids, or
  // comes nearer than CLEARANCE to it. It compares the lane paths that
  // lanePolyline gives. It compares each lane in ids with all other lanes,
  // also with the other lanes in ids. It does not compare two lanes that
  // share a node, because junction control holds the pods of such lanes
  // apart. It gives the first conflict as lane, the ID from ids, other, the
  // ID of the other lane, and gap, the distance in meters, which is 0 for a
  // crossing. It gives null when there is no conflict.
  function laneConflict(config, ids, separationGroups = false) {
    const paths = new Map();
    for (const lane of config.network.Lanes) {
      const from = point(config, lane.From); const to = point(config, lane.To);
      if (!from || !to) continue;
      const path = lanePolyline(from, to, lane.Control);
      const xs = path.map((at) => at.X); const ys = path.map((at) => at.Y);
      paths.set(lane.ID, { lane, path, low: { X: Math.min(...xs), Y: Math.min(...ys) }, high: { X: Math.max(...xs), Y: Math.max(...ys) } });
    }
    const apart = (a, b) => a.low.X - b.high.X >= CLEARANCE || b.low.X - a.high.X >= CLEARANCE || a.low.Y - b.high.Y >= CLEARANCE || b.low.Y - a.high.Y >= CLEARANCE;
    for (const id of ids) {
      const item = paths.get(id);
      if (!item) continue;
      for (const other of paths.values()) {
        if (other.lane.ID === id || apart(item, other)) continue;
        const ends = [other.lane.From, other.lane.To];
        if (ends.includes(item.lane.From) || ends.includes(item.lane.To)) continue;
        if (separationGroups && item.lane.SeparationGroup && other.lane.SeparationGroup && item.lane.SeparationGroup !== other.lane.SeparationGroup) continue;
        const gap = pathGap(item.path, other.path);
        if (gap < CLEARANCE) return { lane: id, other: other.lane.ID, gap };
      }
    }
    return null;
  }

  // removeBerth removes a berth, its node, the lanes of its node, and its
  // pods. It gives the new config and an empty error, or the same config
  // and an error that names the station. It does not remove the last berth
  // of a station. Each lane of the berth node must be a station lane, with
  // the station ID in StationID. addStation, addBerth and the scenario
  // generators make such lanes for each berth. normalizeConfig gives these
  // fields to the station lanes of a project without them, as the
  // simulation infers them. When a road lane or a lane
  // of a different station uses the berth node, the editor does not remove
  // the berth, and the error tells the user to delete these lanes first.
  // When the berth is the last row of a berth chain, it also removes the
  // arrival link, the departure link, the arrival node and the departure
  // node of the row. Thus it removes the row that addBerth adds. When a
  // lane that is not one of the four lanes of the row uses the arrival or
  // the departure node, the editor does not remove the berth, because the
  // removal would also cut that lane off.
  function removeBerth(config, stationID, berthID) {
    const station = config.network.Stations.find((item) => item.ID === stationID);
    const berth = station && station.Berths.length > 1 && station.Berths.find((item) => item.ID === berthID);
    if (!berth) return { config, error: "" };
    const foreign = config.network.Lanes.filter((lane) => (lane.From === berth.Node || lane.To === berth.Node) && lane.StationID !== stationID).map((lane) => lane.ID);
    if (foreign.length) {
      const lanes = foreign.length === 1 ? `lane ${foreign[0]} also uses` : `lanes ${foreign.join(", ")} also use`;
      return { config, error: `Berth ${berthID} at ${station.Name || station.ID} stays, because ${lanes} its node ${berth.Node}. Delete ${foreign.length === 1 ? "this lane" : "these lanes"} first.` };
    }
    const last = berthChain(config, station)?.at(-1);
    const row = last && last.berth.ID === berthID ? last : null;
    const rowLanes = new Set(row ? [row.arrivalLink.ID, row.departureLink.ID, row.inLane.ID, row.outLane.ID] : []);
    const rowNodes = new Set(row ? [row.arrival, row.departure] : []);
    const shared = config.network.Lanes.find((lane) => !rowLanes.has(lane.ID) && (rowNodes.has(lane.From) || rowNodes.has(lane.To)));
    if (shared) {
      const node = rowNodes.has(shared.From) ? shared.From : shared.To;
      return { config, error: `Berth ${berthID} at ${station.Name || station.ID} stays, because lane ${shared.ID} also uses node ${node}.` };
    }
    const out = clone(config);
    const target = out.network.Stations.find((item) => item.ID === stationID);
    target.Berths = target.Berths.filter((item) => item.ID !== berthID);
    out.network.Lanes = out.network.Lanes.filter((lane) => lane.From !== berth.Node && lane.To !== berth.Node && !rowLanes.has(lane.ID));
    const gone = new Set([berth.Node, ...rowNodes]);
    out.network.Nodes = out.network.Nodes.filter((node) => !gone.has(node.ID));
    out.fleet = out.fleet.filter((pod) => pod.BerthID !== berthID);
    return { config: out, error: "" };
  }

  function moveStation(config, stationID, dx, dy) {
    const out = clone(config);
    const station = out.network.Stations.find((item) => item.ID === stationID);
    if (!station) return config;
    shiftNodes(out, { ids: stationNodeIDs(out, station), dx, dy });
    return out;
  }

  function moveNode(config, nodeID, x, y) {
    const out = clone(config);
    const node = out.network.Nodes.find((item) => item.ID === nodeID);
    if (!node) return config;
    node.Position = { X: x, Y: y };
    return out;
  }

  function deleteNode(config, nodeID) {
    if (stationNodeOwners(config).has(nodeID)) return { config, error: "Delete the station or berth from its controls." };
    const out = clone(config);
    if (!out.network.Nodes.some((node) => node.ID === nodeID)) return { config, error: "The node does not exist." };
    out.network.Nodes = out.network.Nodes.filter((node) => node.ID !== nodeID);
    out.network.Lanes = out.network.Lanes.filter((lane) => lane.From !== nodeID && lane.To !== nodeID);
    return { config: out, error: "" };
  }

  function deleteLane(config, laneID) {
    const out = clone(config);
    out.network.Lanes = out.network.Lanes.filter((lane) => lane.ID !== laneID);
    return out;
  }

  // deleteStation removes the station, its nodes and lanes, and the pods at
  // the station. It clears the demand destination when it is the station. It
  // also removes each demand profile flow that starts or ends at the station.
  // A profile with no flows stays, and validation reports it.
  function deleteStation(config, stationID) {
    const out = clone(config);
    const station = out.network.Stations.find((item) => item.ID === stationID);
    if (!station) return config;
    const ids = stationNodeIDs(out, station);
    const berthIDs = new Set(station.Berths.map((berth) => berth.ID));
    out.network.Stations = out.network.Stations.filter((item) => item.ID !== stationID);
    out.network.Nodes = out.network.Nodes.filter((node) => !ids.has(node.ID));
    out.network.Lanes = out.network.Lanes.filter((lane) => !ids.has(lane.From) && !ids.has(lane.To));
    for (const lane of out.network.Lanes) {
      if (lane.StationID === stationID) {
        delete lane.StationID;
        delete lane.StationRole;
      }
    }
    out.fleet = out.fleet.filter((pod) => pod.StationID !== stationID && !berthIDs.has(pod.BerthID));
    if (out.demand.destination === stationID) out.demand.destination = "";
    for (const profile of out.demandProfiles || []) {
      if (profile && Array.isArray(profile.flows)) profile.flows = profile.flows.filter((flow) => !flowNamesStation(flow, stationID));
    }
    if (Array.isArray(out.railArrivals)) {
      out.railArrivals = out.railArrivals.filter((arrival) => arrival.station !== stationID).map((arrival) => ({ ...arrival, destinations: arrival.destinations.filter((destination) => destination.station !== stationID) })).filter((arrival) => arrival.destinations.length > 0);
    }
    if (Array.isArray(out.railDepartures)) {
      out.railDepartures = out.railDepartures.filter((departure) => departure.station !== stationID).map((departure) => ({ ...departure, origins: departure.origins.filter((origin) => origin.station !== stationID) })).filter((departure) => departure.origins.length > 0);
    }
    return out;
  }

  function withRailPlan(config, arrivals, key = "railArrivals") {
    const out = frozenDrafts.has(config) ? { ...config } : clone(config);
    out[key] = frozenDrafts.has(config) ? arrivals : clone(arrivals);
    return out;
  }

  function addRailArrival(config) {
    const passenger = config.network.Stations.filter((station) => !station.ParkingOnly);
    const arrivals = config.railArrivals || [];
    if (passenger.length < 2) return { config, error: "Rail arrivals need at least two passenger stations." };
    if (arrivals.length + (config.railDepartures || []).length >= 256) return { config, error: "The plan already has 256 rail events." };
    let number = 1;
    while (arrivals.some((arrival) => arrival.id === `train-${number}`)) number += 1;
    const atSeconds = arrivals.length ? Math.min(86400, Math.max(...arrivals.map((arrival) => arrival.atSeconds)) + 600) : 0;
    const arrival = { id: `train-${number}`, station: passenger[0].ID, atSeconds, walkingSeconds: 0, passengers: 120, destinations: [{ station: passenger[1].ID, weight: 1 }] };
    return { config: withRailPlan(config, [...arrivals, arrival]), error: "" };
  }

  function removeRailArrival(config, id) {
    const arrivals = config.railArrivals || [];
    if (!arrivals.some((arrival) => arrival.id === id)) return config;
    return withRailPlan(config, arrivals.filter((arrival) => arrival.id !== id));
  }

  function editRailArrival(config, id, change) {
    const arrivals = config.railArrivals || [];
    if (!arrivals.some((arrival) => arrival.id === id)) return config;
    return withRailPlan(config, arrivals.map((arrival) => arrival.id === id ? change(clone(arrival)) : arrival));
  }

  function addRailDestination(config, id) {
    const arrival = (config.railArrivals || []).find((item) => item.id === id);
    if (!arrival || arrival.destinations.length >= 16) return { config, error: "A rail arrival can have at most 16 destinations." };
    const station = config.network.Stations.find((item) => !item.ParkingOnly && item.ID !== arrival.station && !arrival.destinations.some((destination) => destination.station === item.ID));
    if (!station) return { config, error: "All other passenger stations are already destinations." };
    return { config: editRailArrival(config, id, (item) => { item.destinations.push({ station: station.ID, weight: 1 }); return item; }), error: "" };
  }

  function addRailDeparture(config) {
    const passenger = config.network.Stations.filter((station) => !station.ParkingOnly);
    const departures = config.railDepartures || [];
    if (passenger.length < 2) return { config, error: "Rail departures need at least two passenger stations." };
    if (departures.length + (config.railArrivals || []).length >= 256) return { config, error: "The plan already has 256 rail events." };
    if (departures.reduce((sum, item) => sum + item.passengers, 0) + 120 > 3000) return { config, error: "Rail departures can offer at most 3000 passengers." };
    let number = 1;
    while (departures.some((departure) => departure.id === `departure-${number}`)) number += 1;
    const atSeconds = departures.length ? Math.min(86400, Math.max(...departures.map((departure) => departure.atSeconds)) + 600) : 600;
    const departure = { id: `departure-${number}`, station: passenger[0].ID, atSeconds, walkingSeconds: 60, requestFromSeconds: atSeconds - 600, requestUntilSeconds: atSeconds - 300, passengers: 120, origins: [{ station: passenger[1].ID, weight: 1 }] };
    return { config: withRailPlan(config, [...departures, departure], "railDepartures"), error: "" };
  }

  function editRailDeparture(config, id, change) {
    const departures = config.railDepartures || [];
    if (!departures.some((departure) => departure.id === id)) return config;
    return withRailPlan(config, departures.map((departure) => departure.id === id ? change(clone(departure)) : departure), "railDepartures");
  }

  function removeRailDeparture(config, id) {
    const departures = config.railDepartures || [];
    if (!departures.some((departure) => departure.id === id)) return config;
    return withRailPlan(config, departures.filter((departure) => departure.id !== id), "railDepartures");
  }

  function addRailOrigin(config, id) {
    const departure = (config.railDepartures || []).find((item) => item.id === id);
    if (!departure || departure.origins.length >= 16) return { config, error: "A rail departure can have at most 16 origins." };
    const station = config.network.Stations.find((item) => !item.ParkingOnly && item.ID !== departure.station && !departure.origins.some((origin) => origin.station === item.ID));
    if (!station) return { config, error: "All other passenger stations are already origins." };
    return { config: editRailDeparture(config, id, (item) => { item.origins.push({ station: station.ID, weight: 1 }); return item; }), error: "" };
  }

  function setFleetCount(config, stationID, requested) {
    const out = frozenDrafts.has(config) ? { ...config } : clone(config);
    const station = out.network.Stations.find((item) => item.ID === stationID);
    if (!station) return config;
    const count = Math.max(0, Math.min(station.Berths.length, Math.floor(Number(requested) || 0)));
    const other = out.fleet.filter((pod) => pod.StationID !== stationID);
    const current = out.fleet.filter((pod) => pod.StationID === stationID && station.Berths.some((berth) => berth.ID === pod.BerthID));
    const kept = current.slice(0, count);
    const occupied = new Set([...other, ...kept].map((pod) => pod.BerthID));
    while (kept.length < count) {
      const berth = station.Berths.find((item) => !occupied.has(item.ID));
      if (!berth) break;
      const used = new Set([...other, ...kept].map((pod) => pod.ID));
      let number = 1;
      while (used.has(String(number).padStart(2, "0"))) number += 1;
      const pod = { ID: String(number).padStart(2, "0"), StationID: station.ID, BerthID: berth.ID };
      kept.push(pod);
      occupied.add(berth.ID);
    }
    out.fleet = [...other, ...kept];
    return out;
  }

  // setDemandPattern sets the passenger demand pattern. The profile pattern
  // selects the first demand profile and its first time band. A draft with no
  // demand profiles, or with a demandProfiles value that is not an array, gets
  // only the pattern. The checks then report the missing profile.
  function setDemandPattern(config, pattern) {
    const out = frozenDrafts.has(config) ? { ...config, demand: { ...config.demand } } : clone(config);
    out.demand.pattern = pattern;
    const [first] = Array.isArray(out.demandProfiles) ? out.demandProfiles : [];
    if (pattern === "profile" && first) { out.demand.profile = first.id; out.demand.band = first.bands?.[0]?.id || ""; }
    if (pattern === "profile-daily") { if (first) out.demand.profile = first.id; out.demand.band = ""; out.demand.dailyStartMinute ??= 0; }
    else delete out.demand.dailyStartMinute;
    return out;
  }

  function reachable(config, from, to) {
    const adjacency = new Map();
    for (const lane of config.network.Lanes) {
      if (!adjacency.has(lane.From)) adjacency.set(lane.From, []);
      adjacency.get(lane.From).push(lane.To);
    }
    return reachableFrom(adjacency, from).has(to);
  }

  function reachableFrom(adjacency, from) {
    const seen = new Set([from]);
    const queue = [from];
    for (let index = 0; index < queue.length; index += 1) {
      const current = queue[index];
      for (const next of adjacency.get(current) || []) {
        if (!seen.has(next)) {
          seen.add(next);
          queue.push(next);
        }
      }
    }
    return seen;
  }

  // makeGeo gives the geo reference of a project at a latitude and a
  // longitude in degrees.
  function makeGeo(latitude, longitude) {
    return { latitude, longitude, projection: GEO_PROJECTION, radius: GEO_RADIUS };
  }

  // geoError gives the error of a geo reference that project.Validate does
  // not accept, or an empty text for a valid reference.
  function geoError(geo) {
    if (geo === null || typeof geo !== "object" || Array.isArray(geo)) return "The geo reference must be an object.";
    if (typeof geo.latitude !== "number" || !(Math.abs(geo.latitude) <= GEO_MAX_LATITUDE)) return `The geo latitude must be from -${GEO_MAX_LATITUDE} to ${GEO_MAX_LATITUDE} degrees.`;
    if (typeof geo.longitude !== "number" || !(Math.abs(geo.longitude) <= 180)) return "The geo longitude must be from -180 to 180 degrees.";
    if (geo.projection !== GEO_PROJECTION) return `The geo projection must be "${GEO_PROJECTION}".`;
    if (geo.radius !== GEO_RADIUS) return `The geo radius must be ${GEO_RADIUS} meters.`;
    return "";
  }

  // unprojectPoint gives the latitude and the longitude in degrees of a
  // world position, as the inverse of projectPoint.
  function unprojectPoint(geo, at) {
    return { latitude: geo.latitude - at.Y / (geo.radius * DEGREE), longitude: geo.longitude + at.X / (geo.radius * Math.cos(geo.latitude * DEGREE) * DEGREE) };
  }

  // scaleError gives the error when the east-west scale of the projection
  // of geo, cos(lat0) / cos(lat), differs from 1 by more than
  // SCALE_TOLERANCE in the latitudes from south to north. The largest and
  // the smallest cos(lat) are at south, at north, and at 0 when the range
  // crosses the equator. It gives an empty text when the scale is in the
  // budget.
  function scaleError(geo, south, north) {
    const edges = south < 0 && north > 0 ? [south, north, 0] : [south, north];
    const worst = Math.max(...edges.map((latitude) => Math.abs(Math.cos(geo.latitude * DEGREE) / Math.cos(latitude * DEGREE) - 1)));
    if (worst <= SCALE_TOLERANCE) return "";
    return `The frame is too far north or south of the reference latitude ${geo.latitude}. The east-west scale differs by ${(worst * 100).toFixed(2)}%, and the limit is ${SCALE_TOLERANCE * 100}%.`;
  }

  // placementError gives the error when the editor cannot place an image
  // with frame in geo, or an empty text. The frame must be valid, geo must
  // be set, the frame must be in the accuracy budget of scaleError, and
  // each corner must be in the square of MAX_COORDINATE meters.
  function placementError(frame, geo) {
    const frameText = frameError(frame);
    if (frameText) return frameText;
    if (!geo) return "The project has no geographic reference.";
    const scale = scaleError(geo, frame.south, frame.north);
    if (scale) return scale;
    const place = framePlacement(frame, geo);
    const inside = [place.x, place.y, place.x + place.width, place.y + place.height].every((value) => Math.abs(value) <= MAX_COORDINATE);
    return inside ? "" : `The frame is more than ${MAX_COORDINATE} m from the reference of the project.`;
  }

  // anchorGeo gives the geo reference from two anchor nodes. a and b have X
  // and Y, the node position in meters, and latitude and longitude, the
  // place of the node in degrees. The reference puts a exactly at its node
  // position. The result has geo, the residual, the distance in meters
  // from the projection of b to its node, and distance, the distance in
  // meters between the two nodes. error is empty when the nodes are at
  // least ANCHOR_MIN_DISTANCE meters apart, the residual is at most
  // ANCHOR_MAX_RESIDUAL of the distance, and the reference is valid. The
  // frame of an image has no rotation, so a network that turns against the
  // map fails the residual check.
  function anchorGeo(anchor) {
    const { a, b } = anchor;
    const distance = Math.hypot(b.X - a.X, b.Y - a.Y);
    const latitude = a.latitude + a.Y / (GEO_RADIUS * DEGREE);
    const geo = makeGeo(latitude, a.longitude - a.X / (GEO_RADIUS * Math.cos(latitude * DEGREE) * DEGREE));
    const at = projectPoint(geo, b.latitude, b.longitude);
    const residual = Math.hypot(at.X - b.X, at.Y - b.Y);
    let error = geoError(geo);
    if (!error && distance < ANCHOR_MIN_DISTANCE) error = `The anchor nodes must be at least ${ANCHOR_MIN_DISTANCE} m apart.`;
    if (!error && residual > ANCHOR_MAX_RESIDUAL * distance) error = `The second anchor is ${residual.toFixed(1)} m from its node, more than ${ANCHOR_MAX_RESIDUAL * 100}% of the ${distance.toFixed(1)} m between the nodes.`;
    return { geo, residual, distance, error };
  }

  // frameCenterGeo gives the geo reference at the center of frame.
  function frameCenterGeo(frame) {
    return makeGeo((frame.south + frame.north) / 2, (frame.west + frame.east) / 2);
  }

  // referenceFor gives the geo reference for the placement of an image
  // with frame in config. A project with a reference keeps it, and a
  // project with no nodes adopts the center of the frame. A project with
  // nodes and no reference needs choice: mode "anchor" with a and b, each
  // with id, the node ID, and latitude and longitude, or mode "adopt" with
  // confirmed set. The result has geo and note, a text for the user, or
  // error. The frame must pass placementError in the reference.
  function referenceFor(request) {
    const { config, frame, choice } = request;
    let geo = config.geo || null; let note = "";
    if (!geo) {
      const mode = choice ? choice.mode : "";
      if (!config.network.Nodes.length || (mode === "adopt" && choice.confirmed)) geo = frameCenterGeo(frame);
      else if (mode === "adopt") return { error: "Confirm that the image center becomes the reference, and that the network does not move." };
      else if (mode === "anchor") {
        const point = (item) => {
          const node = config.network.Nodes.find((entry) => entry.ID === item.id);
          if (!node) throw new Error(`The anchor node "${String(item.id).slice(0, 40)}" is not in the project.`);
          if (![item.latitude, item.longitude].every((value) => typeof value === "number" && Number.isFinite(value))) throw new Error("Each anchor needs a latitude and a longitude in degrees.");
          return { X: node.Position.X, Y: node.Position.Y, latitude: item.latitude, longitude: item.longitude };
        };
        let anchor;
        try { anchor = anchorGeo({ a: point(choice.a), b: point(choice.b) }); } catch (error) { return { error: error.message }; }
        if (anchor.error) return { error: anchor.error };
        geo = anchor.geo; note = `The second anchor is ${anchor.residual.toFixed(1)} m from its node.`;
      } else return { error: "The project has nodes and no geographic reference. Anchor two nodes, or adopt the image center." };
    }
    const error = placementError(frame, geo);
    return error ? { error } : { geo: clone(geo), note };
  }

  // framedValue gives the history value with image placed from its frame
  // in geo, with the frame state "attached", and the scenario of value
  // with geo. The background keeps the opacity of the background of
  // value.
  function framedValue(value, image, geo) {
    const opacity = value.background ? value.background.opacity : DEFAULT_OPACITY;
    return { scenario: { ...value.scenario, geo: clone(geo) }, background: { imageKey: image.key, ...framePlacement(image.frame, geo), opacity, frameState: "attached" } };
  }

  // detachFrame gives the history value with the frame state "detached"
  // for an attached background. The image keeps its frame and its
  // license.
  function detachFrame(value) {
    if (!value.background || value.background.frameState !== "attached") return value;
    return { scenario: value.scenario, background: { ...value.background, frameState: "detached" } };
  }

  // withTileMap preserves coordinates and requires a reference before enabling tiles.
  function withTileMap(config, options) {
    const next = clone(config);
    if (!next.geo) {
      const choice = options.choice || {};
      if (!next.network.Nodes.length || (choice.mode === "adopt" && choice.confirmed)) {
        next.geo = makeGeo(options.latitude, options.longitude);
        const error = geoError(next.geo);
        if (error) throw new Error(error);
      } else if (choice.mode === "anchor") {
        const lat = choice.a.latitude, lon = choice.a.longitude;
        const frame = { south: Math.max(-80, lat - .00001), north: Math.min(80, lat + .00001), west: Math.max(-180, lon - .00001), east: Math.min(180, lon + .00001), source: "equirectangular" };
        const reference = referenceFor({ config: next, frame, choice });
        if (reference.error) throw new Error(reference.error);
        next.geo = reference.geo;
      } else throw new Error("Anchor two nodes, or confirm the map origin. Network positions will not change.");
    }
    next.map = { provider: "osm", opacity: options.opacity ?? .45 };
    if (!Tiles.validMap(next.map, next.geo)) throw new Error("The map settings are invalid.");
    return next;
  }

  return {
    normalizeConfig, inferStationLanes, inferableStations, stationRouteGraph, simLaneLength, goHypot, stationRoute, createRouteQueue, allIDs, nextID, addLane, addStationLane, addJunction, addStation, rotateStation, setStationBearing, nextBerthPosition, addBerth, busyText, busyNode, addStarBerth, addChainBerth, setStationLayout, nextChainRow, chainID, lanePolyline, pointGap, segmentGap, pathGap, laneConflict, removeBerth, moveStation, moveNode, deleteNode, deleteLane, deleteStation, withRailPlan, addRailArrival, removeRailArrival, editRailArrival, addRailDestination, addRailDeparture, editRailDeparture, removeRailDeparture, addRailOrigin, setFleetCount, setDemandPattern, reachable, reachableFrom, makeGeo, geoError, unprojectPoint, scaleError, placementError, anchorGeo, frameCenterGeo, referenceFor, framedValue, detachFrame, withTileMap,
    freezeDraft, editDraft,
    readLive: (connection, normalize = normalizeConfig) => helpers.readLive(connection, normalize),
    readConflict: (connection, error, normalize = normalizeConfig) => helpers.readConflict(connection, error, normalize),
    loadLive: (load) => helpers.loadLive({ ...load, normalize: load.normalize || normalizeConfig }),
    applyOverBase: (over) => helpers.applyOverBase({ ...over, normalize: over.normalize || normalizeConfig }),
  };
};
