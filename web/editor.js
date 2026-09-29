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
  const STATION_LANE_ROLES = new Set(["approach", "entry", "berth-access", "through", "departure", "exit"]);
  // sharedRideModes mirrors the modes of sim.SharedRideMode. The first
  // mode is sim.DefaultSharedRideMode.
  const sharedRideModes = ["drop-offs", "destination"];
  const sharedRideJoins = ["unassigned", "reassign-existing"];
  // platoonLimits holds the platoon limits that project.Validate accepts. 0
  // turns platoons off.
  const platoonLimits = [0, 2, 3, 4];
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

  // freezeDraft makes history snapshots safe to share with read-only callers.
  function freezeDraft(value) {
    if (!value || typeof value !== "object" || frozenDrafts.has(value)) return value;
    for (const child of Object.values(value)) freezeDraft(child);
    frozenDrafts.add(value); return Object.freeze(value);
  }

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

  // emptyConfig gives a scenario with an empty network. Each setting has the
  // value that normalizeConfig gives, so that each settings field shows a
  // value.
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
      redistribution: false,
    };
  }

  // fallbackConfig gives the local draft that the editor opens when the live
  // scenario cannot load. It has two stations with one lane in each
  // direction and no pods.
  function fallbackConfig() {
    let config = addStation(addStation(emptyConfig(), 100, 120, { name: "Origin" }), 340, 120, { name: "Destination" });
    config = addLane(config, config.network.Stations[0].Exit, config.network.Stations[1].Entry, false);
    return addLane(config, config.network.Stations[1].Exit, config.network.Stations[0].Entry, false);
  }

  function normalizeConfig(input) {
    const config = clone(input || emptyConfig());
    config.version = 1;
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
    if (!["destination", "profile"].includes(config.demand.pattern)) config.demand.pattern = "balanced";
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

  function point(config, nodeID) {
    const node = config.network.Nodes.find((item) => item && item.ID === nodeID);
    return node && node.Position;
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
  function laneConflict(config, ids) {
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

  function stationCoreNodeIDs(station) {
    return new Set([station.Entry, station.Exit, ...(station.Berths || []).map((berth) => berth.Node)]);
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

  function moveStation(config, stationID, dx, dy) {
    const out = clone(config);
    const station = out.network.Stations.find((item) => item.ID === stationID);
    if (!station) return config;
    shiftNodes(out, { ids: stationNodeIDs(out, station), dx, dy });
    return out;
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
    return out;
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

  // fleetRows gives one Fleet row for each station, in station order: the
  // station ID and name, the number of initial pods at the station, and the
  // number of berths, which is the limit for the pod count.
  function fleetRows(config) {
    return config.network.Stations.map((station) => ({
      id: station.ID, name: station.Name, max: station.Berths.length,
      count: config.fleet.filter((pod) => pod.StationID === station.ID).length,
    }));
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
      return station ? {
        type: "station", id: station.ID, name: station.Name, bearing: entry && exit ? Math.round(stationBearing(entry, exit)) % 360 : 0,
        parkingOnly: Boolean(station.ParkingOnly), canRemove: station.Berths.length > 1,
        berths: station.Berths.map((berth) => ({ id: berth.ID, selected: berth.ID === selection.berth })),
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

  // setDemandPattern sets the passenger demand pattern. The profile pattern
  // selects the first demand profile and its first time band. A draft with no
  // demand profiles, or with a demandProfiles value that is not an array, gets
  // only the pattern. The checks then report the missing profile.
  function setDemandPattern(config, pattern) {
    const out = frozenDrafts.has(config) ? { ...config, demand: { ...config.demand } } : clone(config);
    out.demand.pattern = pattern;
    const [first] = Array.isArray(out.demandProfiles) ? out.demandProfiles : [];
    if (pattern === "profile" && first) { out.demand.profile = first.id; out.demand.band = first.bands?.[0]?.id || ""; }
    return out;
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

  function reachable(config, from, to) {
    const adjacency = new Map();
    for (const lane of config.network.Lanes) {
      if (!adjacency.has(lane.From)) adjacency.set(lane.From, []);
      adjacency.get(lane.From).push(lane.To);
    }
    return reachableFrom(adjacency, from).has(to);
  }

  // reachableAvoiding reports whether a lane route goes from one node to another.
  // The route cannot pass through a blocked node, but it can end at one.
  function reachableAvoiding(adjacency, from, to, blocked) {
    const seen = new Set([from]);
    const queue = [from];
    for (let index = 0; index < queue.length; index += 1) {
      for (const next of adjacency.get(queue[index]) || []) {
        if (next === to) return true;
        if (seen.has(next) || blocked.has(next)) continue;
        seen.add(next);
        queue.push(next);
      }
    }
    return false;
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

  // stationReach tells which stations can reach which other stations.
  // berthNodes holds the berth node IDs of each station. reach[from][to] is
  // true when a lane route goes from each berth of station from to each berth
  // of station to. As on the server, the route can use all lanes. One search
  // runs from each berth. The search uses node indexes, because the London
  // project has about 200 passenger berths.
  function stationReach(lanes, berthNodes) {
    const indexes = new Map();
    const indexOf = (node) => {
      if (!indexes.has(node)) indexes.set(node, indexes.size);
      return indexes.get(node);
    };
    const adjacency = [];
    for (const lane of lanes) (adjacency[indexOf(lane.From)] ||= []).push(indexOf(lane.To));
    const berths = berthNodes.map((nodes) => nodes.map(indexOf));
    const seen = new Uint32Array(indexes.size);
    let search = 0;
    return berths.map((starts) => {
      const reach = berths.map(() => true);
      for (const start of starts) {
        search += 1;
        seen[start] = search;
        const queue = [start];
        for (let index = 0; index < queue.length; index += 1) {
          for (const next of adjacency[queue[index]] || []) {
            if (seen[next] === search) continue;
            seen[next] = search;
            queue.push(next);
          }
        }
        berths.forEach((targets, to) => { if (!targets.every((node) => seen[node] === search)) reach[to] = false; });
      }
      return reach;
    });
  }

  // cutOffStations gives the stations that are cut off from the main group.
  // reach is the table from stationReach, and checked tells which stations
  // have berths to check. A group is a set of stations that can all reach
  // each other. All stations of a group have the same missing routes to and
  // from the other stations. The main group is the largest group. When two
  // or more groups have the largest size, the main group is the group with
  // the fewest missing routes, and then the group with the first station in
  // the project. Thus a station with no routes is not the main group when
  // another station has a route. Each station that is not in the main group
  // gives one item. out holds the indexes of the stations that it cannot
  // reach, and in holds the indexes of the stations that cannot reach it. A
  // station with no berths to check is not in a group, because its row and
  // column of reach are all true.
  function cutOffStations(reach, checked) {
    const stations = [...reach.keys()].filter((index) => checked[index]);
    const links = (index) => ({
      out: stations.filter((other) => other !== index && !reach[index][other]),
      in: stations.filter((other) => other !== index && !reach[other][index]),
    });
    const group = []; const groups = [];
    for (const seed of stations) {
      if (group[seed] !== undefined) continue;
      const members = stations.filter((index) => index === seed || (reach[seed][index] && reach[index][seed]));
      for (const index of members) group[index] = groups.length;
      const missing = links(seed);
      groups.push({ size: members.length, misses: missing.out.length + missing.in.length });
    }
    const better = (item, best) => item.size > best.size || (item.size === best.size && item.misses < best.misses);
    const main = groups.reduce((best, item, index) => (best < 0 || better(item, groups[best]) ? index : best), -1);
    return stations.filter((index) => group[index] !== main).map((index) => ({ index, ...links(index) }));
  }

  // cutOffText gives the error for a cut-off station. item has name, the
  // name of the station, out, the names of the stations that it cannot
  // reach, and in, the names of the stations that cannot reach it. A list of
  // one station gives its name. A longer list gives the number of stations.
  function cutOffText(item) {
    const list = (names) => names.length === 1 ? names[0] : `${names.length} passenger stations`;
    const parts = [];
    if (item.out.length) parts.push(`${item.name} cannot reach ${list(item.out)}`);
    if (item.in.length) parts.push(`${list(item.in)} cannot reach ${item.name}`);
    return `${parts.join(", and ")}.`;
  }

  // A check target is the object that a check message names. type is
  // "station", "berth", "lane" or "node", and id is the ID of the object.
  // CHECK_TARGET_KINDS gives the target type for each kind of ID that
  // validateConfig checks. A pod is not a target, because the map does not
  // show pods.
  const CHECK_TARGET_KINDS = { "A node": "node", "A lane": "lane", "A station": "station", "A berth": "berth" };

  // checkTarget gives a check target, or null when the type or the ID is
  // missing.
  function checkTarget(type, id) {
    return type && typeof id === "string" && id ? { type, id } : null;
  }

  // validateConfig gives the errors for a scenario. An error blocks an apply
  // or an import. configWarnings gives the checks that do not block. When
  // targets is a Map, validateConfig adds the object that an error names to
  // it, with the error text as the key. checkResults uses these targets.
  function validateConfig(value, targets) {
    const errors = [];
    const report = (text, target) => { errors.push(text); if (targets && target && !targets.has(text)) targets.set(text, target); };
    if (!value || typeof value !== "object" || Array.isArray(value)) return ["The scenario must be a JSON object."];
    if (value.version !== 1) errors.push("The scenario version must be 1.");
    if (typeof value.name !== "string" || !value.name.trim()) errors.push("The scenario needs a name.");
    if (typeof value.name === "string" && new TextEncoder().encode(value.name).length > 80) errors.push("The scenario name exceeds 80 bytes.");
    const network = value.network;
    if (!network || !Array.isArray(network.Nodes) || !Array.isArray(network.Lanes) || !Array.isArray(network.Stations)) {
      return [...errors, "The scenario needs Nodes, Lanes, and Stations arrays."];
    }
    const namespaces = new Map();
    function uniqueID(id, kind) {
      if (!namespaces.has(kind)) namespaces.set(kind, new Set());
      const idSet=namespaces.get(kind);
      const target = checkTarget(CHECK_TARGET_KINDS[kind], id);
      if (typeof id === "string" && new TextEncoder().encode(id).length > 64) report(`${kind} ID exceeds 64 bytes.`, target);
      if (typeof id !== "string" || !id.trim()) errors.push(`${kind} has no ID.`);
      else if (idSet.has(id)) report(`ID ${id} is used more than once.`, target);
      else idSet.add(id);
    }
    const isRecord = (item) => item && typeof item === "object" && !Array.isArray(item);
    const validID = (id) => typeof id === "string" && id.trim() && new TextEncoder().encode(id).length <= 64;
    const validLanes = network.Lanes.filter(isRecord);
    const validStations = network.Stations.filter(isRecord);
    const nodeIDs = new Set();
    for (const node of network.Nodes) {
      uniqueID(node && node.ID, "A node");
      if (isRecord(node) && typeof node.ID === "string") nodeIDs.add(node.ID);
      if (!isRecord(node) || !isRecord(node.Position) || !Number.isFinite(node.Position.X) || !Number.isFinite(node.Position.Y)) errors.push(`Node ${(node && node.ID) || "?"} has an invalid position.`);
    }
    const lanesByPair = new Set();
    const directed = new Map();
    const nodeLanes = new Map();
    const lanePaths = new Map();
    for (const lane of network.Lanes) {
      uniqueID(lane && lane.ID, "A lane");
      const laneTarget = checkTarget("lane", lane && lane.ID);
      if (!isRecord(lane) || !nodeIDs.has(lane.From) || !nodeIDs.has(lane.To) || lane.From === lane.To) report(`Lane ${(lane && lane.ID) || "?"} has invalid endpoints.`, laneTarget);
      if (!isRecord(lane) || !Number.isFinite(lane.SpeedLimit) || lane.SpeedLimit <= 0) report(`Lane ${(lane && lane.ID) || "?"} needs a positive speed limit.`, laneTarget);
      if (isRecord(lane) && lane.Control && (!isRecord(lane.Control) || !Number.isFinite(lane.Control.X) || !Number.isFinite(lane.Control.Y))) report(`Lane ${lane.ID} has an invalid control point.`, laneTarget);
      if (isRecord(lane) && nodeIDs.has(lane.From) && nodeIDs.has(lane.To) && laneLength(value, lane) < MIN_LANE_LENGTH) report(`Lane ${lane.ID} is shorter than ${MIN_LANE_LENGTH} m.`, laneTarget);
      if (isRecord(lane)) {
        const pair = `${lane.From}\u0000${lane.To}`;
        for (const node of [lane.From, lane.To]) nodeLanes.set(node, (nodeLanes.get(node) || 0) + 1);
        const path = isRecord(lane.Control) ? `${pair}\u0000${lane.Control.X}\u0000${lane.Control.Y}` : pair;
        if (lanePaths.has(path)) report(`Lanes ${lanePaths.get(path)} and ${lane.ID} have the same nodes and path.`, laneTarget);
        else lanePaths.set(path, lane.ID);
        lanesByPair.add(pair);
        if (!directed.has(lane.From)) directed.set(lane.From, []);
        directed.get(lane.From).push(lane.To);
      }
    }
    for (const [node, count] of nodeLanes) {
      if (nodeIDs.has(node) && count > MAX_NODE_LANES) report(`Node ${node} has ${count} lanes, more than ${MAX_NODE_LANES}.`, checkTarget("node", node));
    }
    const stationIDs = new Set();
    const berthIDs = new Set();
    const componentNodes = new Set();
    const berthNodes = new Set();
    for (const station of network.Stations) {
      uniqueID(station && station.ID, "A station");
      if (!isRecord(station)) {
        errors.push("A station has an invalid value.");
        continue;
      }
      stationIDs.add(station.ID);
      const stationTarget = checkTarget("station", station.ID);
      if ("ParkingOnly" in station && typeof station.ParkingOnly !== "boolean") report(`Station ${station.ID} has an invalid parking setting.`, stationTarget);
      if (typeof station.Name === "string" && new TextEncoder().encode(station.Name).length>80) report(`Station ${station.ID} name exceeds 80 bytes.`, stationTarget);
      if (Array.isArray(station.Berths) && station.Berths.length>200) report(`Station ${station.ID} exceeds 200 berths.`, stationTarget);
      if (typeof station.Name !== "string" || !station.Name.trim()) report(`Station ${station.ID} needs a name.`, stationTarget);
      if (!nodeIDs.has(station.Entry) || !nodeIDs.has(station.Exit) || station.Entry === station.Exit) report(`Station ${station.ID} has invalid entry or exit nodes.`, stationTarget);
      componentNodes.add(station.Entry); componentNodes.add(station.Exit);
      if (!Array.isArray(station.Berths) || station.Berths.length === 0) report(`Station ${station.ID} needs at least one berth.`, stationTarget);
      for (const berth of Array.isArray(station.Berths) ? station.Berths : []) {
        uniqueID(berth && berth.ID, "A berth");
        if (!isRecord(berth)) {
          report(`Station ${station.ID} has an invalid berth.`, stationTarget);
          continue;
        }
        berthIDs.add(berth.ID);
        componentNodes.add(berth.Node);
        if (!nodeIDs.has(berth.Node) || berth.Node === station.Entry || berth.Node === station.Exit) report(`Berth ${berth.ID} has an invalid node.`, checkTarget("berth", berth.ID));
        if (berthNodes.has(berth.Node)) report(`Berth node ${berth.Node} is used more than once.`, checkTarget("berth", berth.ID));
        berthNodes.add(berth.Node);
      }
      if (!lanesByPair.has(`${station.Entry}\u0000${station.Exit}`)) report(`Station ${station.ID} needs a through lane.`, stationTarget);
    }
    // As on the server, a berth route can use a chain of lanes. It cannot pass
    // through the entry, exit, or berth node of a station.
    const brokenBerths = new Set();
    for (const station of validStations) {
      for (const berth of Array.isArray(station.Berths) ? station.Berths.filter(isRecord) : []) {
        const entry = reachableAvoiding(directed, station.Entry, berth.Node, componentNodes);
        const exit = reachableAvoiding(directed, berth.Node, station.Exit, componentNodes);
        if (!entry) report(`Berth ${berth.ID} needs an entry lane.`, checkTarget("berth", berth.ID));
        if (!exit) report(`Berth ${berth.ID} needs an exit lane.`, checkTarget("berth", berth.ID));
        if (!entry || !exit) brokenBerths.add(berth.Node);
      }
    }
    for (const lane of validLanes) {
      const hasStation = typeof lane.StationID === "string" && lane.StationID.length > 0;
      const hasRole = typeof lane.StationRole === "string" && lane.StationRole.length > 0;
      if (hasStation !== hasRole || hasRole && !STATION_LANE_ROLES.has(lane.StationRole)) report(`Lane ${lane.ID} has an invalid station role.`, checkTarget("lane", lane.ID));
      else if (hasStation && !stationIDs.has(lane.StationID)) report(`Lane ${lane.ID} refers to an unknown station.`, checkTarget("lane", lane.ID));
    }
    if (!Array.isArray(value.fleet)) errors.push("The scenario needs a fleet array.");
    if (typeof value.redistribution !== "boolean") errors.push("The redistribution setting must be true or false.");
    const fleet = Array.isArray(value.fleet) ? value.fleet : [];
    const occupied = new Set();
    for (const pod of fleet) {
      uniqueID(pod && pod.ID, "A pod");
      const podStation = isRecord(pod) && validStations.find((station) => station.ID === pod.StationID);
      const stationBerths = podStation && Array.isArray(podStation.Berths) ? podStation.Berths : [];
      if (!isRecord(pod) || !podStation || !berthIDs.has(pod.BerthID) || !stationBerths.some((berth) => isRecord(berth) && berth.ID === pod.BerthID)) report(`Pod ${(pod && pod.ID) || "?"} has an invalid station or berth.`, checkTarget("station", pod && pod.StationID));
      if (isRecord(pod) && occupied.has(pod.BerthID)) report(`Berth ${pod.BerthID} has more than one pod.`, checkTarget("berth", pod.BerthID));
      if (isRecord(pod)) occupied.add(pod.BerthID);
    }
    const passenger = validStations.filter((station) => station.ParkingOnly !== true);
    if (passenger.length < 2) errors.push("The network needs at least two passenger stations.");
    if (network.Stations.length > MAX_STATIONS || network.Nodes.length > MAX_NODES || network.Lanes.length > MAX_LANES) errors.push("The network exceeds the supported size.");
    if (fleet.length < 1 || fleet.length > MAX_PODS) errors.push(`The fleet must contain 1 to ${MAX_PODS} pods.`);
    // As on the server, each berth of a passenger station must reach each
    // berth of the other passenger stations. The check skips a berth that has
    // a berth route error. That error already blocks, and the server stops at
    // it. Thus one broken berth gives one error, not one for each station pair.
    // A cut-off station also gives one error. See cutOffStations.
    const passengerBerths = passenger.map((station) => (Array.isArray(station.Berths) ? station.Berths.filter(isRecord) : [])
      .map((berth) => berth.Node).filter((node) => !brokenBerths.has(node)));
    const reach = stationReach(validLanes, passengerBerths);
    const names = (indexes) => indexes.map((index) => passenger[index].Name);
    for (const item of cutOffStations(reach, passengerBerths.map((nodes) => nodes.length > 0))) {
      const station = passenger[item.index];
      report(cutOffText({ name: station.Name, out: names(item.out), in: names(item.in) }), checkTarget("station", station.ID));
    }
    const demand = value.demand;
    if (isRecord(demand) && "enabled" in demand && typeof demand.enabled !== "boolean") errors.push("The passenger demand enabled setting must be true or false.");
    if (!demand || !Number.isInteger(demand.perMinute) || demand.perMinute < 1 || demand.perMinute > 120) errors.push("Passenger demand must be 1 to 120 trips per minute.");
    const profiles = Array.isArray(value.demandProfiles) ? value.demandProfiles : [];
    if (!Array.isArray(value.demandProfiles || [])) errors.push("Demand profiles must be an array.");
    if (profiles.length > 8) errors.push("The project has too many demand profiles.");
    const profileIDs = new Set();
    const passengerIDs = new Set(passenger.map((station) => station.ID));
    for (const profile of profiles) {
      if (!isRecord(profile) || !validID(profile.id) || profileIDs.has(profile.id)) { errors.push("A demand profile has an invalid or duplicate ID."); continue; }
      profileIDs.add(profile.id);
      if (typeof profile.name !== "string" || !profile.name.trim() || new TextEncoder().encode(profile.name).length > 80) errors.push(`Demand profile ${profile.id} has an invalid name.`);
      const bands = Array.isArray(profile.bands) ? profile.bands : [];
      const flows = Array.isArray(profile.flows) ? profile.flows : [];
      if (bands.length < 1 || bands.length > 24) errors.push(`Demand profile ${profile.id} must contain 1 to 24 bands.`);
      if (flows.length < 1 || flows.length > MAX_FLOWS) errors.push(`Demand profile ${profile.id} must contain 1 to ${MAX_FLOWS} flows.`);
      const bandIDs = new Set(); const totals = new Array(bands.length).fill(0); const pairs = new Set();
      for (const band of bands) {
        if (!isRecord(band) || !validID(band.id) || bandIDs.has(band.id) || typeof band.name !== "string" || !band.name.trim() || !Number.isInteger(band.startMinute) || band.startMinute < 0 || band.startMinute >= 1440 || !Number.isInteger(band.durationMinutes) || band.durationMinutes < 1 || band.durationMinutes > 1440) errors.push(`Demand profile ${profile.id} has an invalid band.`);
        else bandIDs.add(band.id);
      }
      for (const flow of flows) {
        const pair = isRecord(flow) ? `${flow.from}\0${flow.to}` : "";
        if (!isRecord(flow) || !passengerIDs.has(flow.from) || !passengerIDs.has(flow.to) || flow.from === flow.to || pairs.has(pair) || !Array.isArray(flow.weights) || flow.weights.length !== bands.length) { errors.push(`Demand profile ${profile.id} has an invalid flow.`); continue; }
        pairs.add(pair);
        flow.weights.forEach((weight, index) => { if (!Number.isFinite(weight) || weight < 0) errors.push(`Demand profile ${profile.id} has an invalid weight.`); else totals[index] += weight; });
      }
      if (totals.some((total) => !Number.isFinite(total) || total <= 0)) errors.push(`Demand profile ${profile.id} has an empty band.`);
    }
    if (value.geo !== undefined && value.geo !== null) { const geo = geoError(value.geo); if (geo) errors.push(geo); }
    if (value.map !== undefined && value.map !== null && !Tiles.validMap(value.map, value.geo)) errors.push("The map needs provider osm, opacity from 0 to 1, and a geographic reference.");
    if (!demand || !["balanced", "destination", "market", "profile"].includes(demand.pattern)) errors.push("The passenger demand pattern is invalid.");
    if (isRecord(demand) && "destination" in demand && (typeof demand.destination !== "string" || new TextEncoder().encode(demand.destination).length > 64)) errors.push("The passenger demand destination is invalid.");
    if (demand && demand.pattern === "destination" && !passenger.some((station) => station.ID === demand.destination)) errors.push("Select a passenger destination.");
    if (demand && demand.pattern === "profile") {
      const profile = profiles.find((item) => isRecord(item) && item.id === demand.profile);
      if (!profiles.length) errors.push("The project has no demand profiles. Select another pattern.");
      else if (!profile) errors.push("Select a demand profile.");
      else if (!profile.bands.some((band) => isRecord(band) && band.id === demand.band)) errors.push("Select a demand time band.");
    }
    if (!demand || !Number.isSafeInteger(demand.seed) || demand.seed < 0) errors.push("The demand seed must be a nonnegative whole number.");
    if ("sharedRidePartyLimit" in value && (!Number.isInteger(value.sharedRidePartyLimit) || value.sharedRidePartyLimit < 0 || value.sharedRidePartyLimit > 8)) errors.push("The shared ride party limit must be 1 to 8.");
    if ("sharedRideMode" in value && value.sharedRideMode !== "" && !sharedRideModes.includes(value.sharedRideMode)) errors.push("The shared ride mode must be destination or drop-offs.");
    if ("sharedRideJoin" in value && value.sharedRideJoin !== "" && !sharedRideJoins.includes(value.sharedRideJoin)) errors.push("The shared ride join policy must be unassigned or reassign-existing.");
    if ("sharedRideMaxStops" in value && (!Number.isInteger(value.sharedRideMaxStops) || value.sharedRideMaxStops < 0 || value.sharedRideMaxStops > 7)) errors.push("The shared ride stop limit must be 1 to 7.");
    if ("platoonLimit" in value && !platoonLimits.includes(value.platoonLimit)) errors.push("The platoon limit must be 2 to 4, or 0 for no platoons.");
    return [...new Set(errors)];
  }

  // configWarnings gives the warnings for a scenario. The server accepts a
  // scenario with warnings, so a warning does not block an apply or an
  // import. A junction with no lanes gives a warning. Two nodes that no chain
  // of lanes connects, in any lane direction, also give a warning. That check
  // skips a junction with no lanes, because the junction has its own warning.
  // targets works as in validateConfig.
  function configWarnings(value, targets) {
    const network = value && value.network;
    if (!network || !Array.isArray(network.Nodes) || !Array.isArray(network.Lanes) || !Array.isArray(network.Stations)) return [];
    const isRecord = (item) => item && typeof item === "object" && !Array.isArray(item);
    const stationNodes = new Set();
    for (const station of network.Stations.filter(isRecord)) {
      stationNodes.add(station.Entry); stationNodes.add(station.Exit);
      for (const berth of Array.isArray(station.Berths) ? station.Berths.filter(isRecord) : []) stationNodes.add(berth.Node);
    }
    const undirected = new Map();
    for (const lane of network.Lanes.filter(isRecord)) {
      for (const [from, to] of [[lane.From, lane.To], [lane.To, lane.From]]) {
        if (!undirected.has(from)) undirected.set(from, []);
        undirected.get(from).push(to);
      }
    }
    const warnings = [];
    const sectionNodes = [];
    for (const node of network.Nodes.filter(isRecord)) {
      if (stationNodes.has(node.ID) || undirected.has(node.ID)) sectionNodes.push(node);
      else {
        const text = `Junction ${node.ID} is disconnected.`;
        warnings.push(text); if (targets) targets.set(text, checkTarget("node", node.ID));
      }
    }
    if (sectionNodes.length) {
      const visited = reachableFrom(undirected, sectionNodes[0].ID);
      if (sectionNodes.some((node) => !visited.has(node.ID))) warnings.push("The network has disconnected sections.");
    }
    return [...new Set(warnings)];
  }

  // checkResults gives the errors and the warnings of a scenario for the
  // Checks list. Each result has text, the message, and target, the check
  // target that the message names, or null.
  function checkResults(value) {
    const errorTargets = new Map(); const warningTargets = new Map();
    const results = (texts, targets) => texts.map((text) => ({ text, target: targets.get(text) || null }));
    return { errors: results(validateConfig(value, errorTargets), errorTargets), warnings: results(configWarnings(value, warningTargets), warningTargets) };
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
    if (document.version !== 1) throw new Error("The version field must be 1.");
    return { scenario: clone(document), background: null };
  }

  // parseDocument checks the text of an import file and gives the scenario
  // and the background fields. For a background, it also gives asset, the
  // frame state, the frame and the license of its asset member, as
  // documentAsset checks them.
  function parseDocument(text) {
    let document;
    try { document = JSON.parse(text); } catch (error) { throw new Error(`The file is not valid JSON. ${error.message}`); }
    const { scenario, background } = unwrapDocument(document);
    if (background) checkBackground(background);
    const asset = background ? documentAsset(background.asset) : null;
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
    const errors = validateConfig(scenario);
    if (errors.length) throw new Error(`The project has ${errors.length} error${errors.length === 1 ? "" : "s"}. ${errors.slice(0, 3).join(" ")}`);
    // A server project file leaves out empty optional fields. An older or
    // edited browser export can also leave out a field. Add them as the live
    // server project load does. The checks run first, so the default values
    // do not replace an invalid value.
    const out = { scenario: normalizeConfig(scenario), background: background ? backgroundFields(background) : null };
    if (asset) out.asset = asset;
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

  // projectPoint gives the world position in meters of a latitude and a
  // longitude in degrees, with the projection of geo: x is R cos(lat0)
  // (lon - lon0) and y is -R (lat - lat0), with the angles in radians.
  function projectPoint(geo, latitude, longitude) {
    return { X: geo.radius * Math.cos(geo.latitude * DEGREE) * (longitude - geo.longitude) * DEGREE, Y: -geo.radius * (latitude - geo.latitude) * DEGREE };
  }

  // unprojectPoint gives the latitude and the longitude in degrees of a
  // world position, as the inverse of projectPoint.
  function unprojectPoint(geo, at) {
    return { latitude: geo.latitude - at.Y / (geo.radius * DEGREE), longitude: geo.longitude + at.X / (geo.radius * Math.cos(geo.latitude * DEGREE) * DEGREE) };
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

  // framePlacement gives the placement of an image with frame in the
  // project projection of geo: the world position of the north-west
  // corner, and the width and the height in meters.
  function framePlacement(frame, geo) {
    const corner = projectPoint(geo, frame.north, frame.west); const far = projectPoint(geo, frame.south, frame.east);
    return { x: corner.X, y: corner.Y, width: far.X - corner.X, height: far.Y - corner.Y };
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

  // frameAligned tells if the placement of background, with x, y, width
  // and height, is the placement of frame in geo, each value within
  // ALIGN_TOLERANCE meters. With no geo, a background is not aligned.
  function frameAligned(background, frame, geo) {
    if (!geo || !frame) return false;
    const place = framePlacement(frame, geo);
    return ["x", "y", "width", "height"].every((key) => Math.abs(background[key] - place[key]) <= ALIGN_TOLERANCE);
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

  // createHistory keeps the draft with undo and redo. onChange runs after
  // each change of the draft, also after an undo, a redo, and a reset. The
  // editor uses it to schedule the checks. Its argument is true only when
  // valid boolean flags change, so existing checks remain valid. A pending
  // check from an earlier edit must still run. background gives a copy of the
  // background of the draft only, with no copy of the scenario. The
  // background of an entry holds an image key, and no image data, so the
  // JSON copy of an entry copies only numbers and strings.
  function createHistory(initial, onChange) {
    let past = [];
    let present = freezeDraft(clone(initial));
    let future = [];
    const changed = (checksUnchanged = false) => { if (onChange) onChange(checksUnchanged); return true; };
    return {
      get value() { return clone(present); },
      get snapshot() { return present; },
      edit(change) {
        const scenario = editDraft(present.scenario, change);
        if (scenario === present.scenario) return false;
        past.push(present); present = freezeDraft({ ...present, scenario }); future = [];
        return changed();
      },
      get scenarioText() { return JSON.stringify(present.scenario); },
      get background() { return present.background ? clone(present.background) : null; },
      get canUndo() { return past.length > 0; },
      get canRedo() { return future.length > 0; },
      // Shared snapshots are deeply frozen. A boolean edit can
      // share the unchanged graph while keeping previous entries intact.
      setOperatingFlag(name, value) {
        if (!["demandEnabled", "redistribution"].includes(name) || typeof value !== "boolean") throw new Error("Invalid operating flag.");
        const scenario = present.scenario;
        const enabled = name === "demandEnabled";
        const previous = enabled ? scenario.demand.enabled : scenario.redistribution;
        if (previous === value) return false;
        past.push(present);
        present = freezeDraft({ ...present, scenario: enabled
          ? { ...scenario, demand: { ...scenario.demand, enabled: value } }
          : { ...scenario, redistribution: value } });
        future = [];
        // Validation depends on the flag types, not their boolean values.
        return changed(typeof previous === "boolean");
      },
      replace(next, record) {
        const owned = ownDraft(next, present);
        if (owned === present) return false;
        if (record !== false) past.push(present);
        present = owned;
        future = [];
        return changed();
      },
      commitFrom(before, next) {
        if (JSON.stringify(before) === JSON.stringify(next)) return false;
        past.push(ownDraft(before)); present = ownDraft(next, present); future = []; return changed();
      },
      undo() { if (!past.length) return false; future.push(present); present = past.pop(); return changed(); },
      // keys gives the image keys of the backgrounds of all entries: the
      // undo steps, the draft and the redo steps.
      keys() {
        const keys = new Set();
        for (const entry of [...past, present, ...future]) if (entry.background && entry.background.imageKey) keys.add(entry.background.imageKey);
        return keys;
      },
      // dropOldest removes the oldest undo step. It gives false when there
      // is no undo step. It is not a draft change, so onChange does not run.
      dropOldest() { if (!past.length) return false; past.shift(); return true; },
      redo() { if (!future.length) return false; past.push(present); present = future.pop(); return changed(); },
      reset(next) { past = []; present = freezeDraft(clone(next)); future = []; changed(); },
    };
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
  async function readLive(connection) {
    const { project: projectReply, state: liveState } = await readSnapshot(connection);
    return {
      project: normalizeConfig(projectReply.project || projectReply.Project),
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
  async function readConflict(connection, error) {
    if (!APPLY_FAILURE.has(error.errorCode)) return null;
    try {
      const live = await readLive(connection);
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
    const live = await readLive(take.connection);
    take.connection.epoch = live.epoch;
    return live;
  }

  // loadLive starts Load live scenario. load.changed tells if the draft has
  // scenario changes. Only then loadLive asks the user, with load.confirm,
  // because the live scenario replaces them. It gives the live state, as
  // takeLive gives it, or null when the user cancels. The page then makes
  // the live project the draft and its base, as liveDraft gives.
  function loadLive(load) {
    return takeLive({ connection: load.connection, question: load.changed ? LOAD_LIVE_QUESTION : "", confirm: load.confirm });
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
    const live = await takeLive({ connection: over.connection, question: applyOverQuestion(over.revision), confirm: over.confirm });
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
  function storedBackground(record) {
    if (record === null || record === undefined) return null;
    if (typeof record !== "object" || Array.isArray(record)) throw new Error("The stored background is not a record.");
    const item = record.background;
    if (item === null || typeof item !== "object" || Array.isArray(item)) throw new Error("The background must be an object.");
    if (typeof item.imageKey !== "string" || !IMAGE_KEY_PATTERN.test(item.imageKey)) throw new Error("The stored background has no valid image key.");
    checkPlacement(item);
    const image = item.image;
    if (image === null || typeof image !== "object" || Array.isArray(image)) throw new Error("The stored background has no image.");
    const facts = imageBytesFacts(image.bytes);
    const asset = { frameState: item.frameState, frame: image.frame ?? null, license: image.license ?? null };
    const error = assetError(asset);
    if (error) throw new Error(error);
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
      restored = storedBackground(record);
      if (restored && page.decode) await page.decode(restored);
    } catch (error) { page.warn(`${error.message} ${STORED_BACKGROUND_KEPT_TEXT}`); return false; }
    if (!restored) return false;
    page.install(restored);
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

  // createBackgroundModel keeps the draft history of an editor tab with the
  // images of its backgrounds. options.initial is the first history value
  // and the Reset draft baseline, options.onChange runs after each change
  // of the draft, and options.cap is the byte limit of the image table,
  // IMAGE_TABLE_BYTES when not given.
  //
  // A history entry and the baseline hold the image key of a background,
  // and the table maps each key to its frozen descriptor. A key stays in
  // the table while an entry of the history, the baseline or a pin refers
  // to it. The background keeper pins the key of each queued write, so a
  // write keeps its image until it settles. Each history change and each
  // unpin removes the images with no reference.
  //
  // The model also keeps the edit counter, which each history change and
  // each move of a drag increments, the open drag and opacity gestures,
  // and the ticket of the one open acquisition or import. publish adds an
  // image only after the final check of publishError, in the same task.
  function createBackgroundModel(options) {
    const cap = options.cap ?? IMAGE_TABLE_BYTES;
    const images = new Map(); const pins = new Map();
    let loaded = clone(options.initial);
    // edits counts the draft changes, also the moves of a drag that the
    // history does not have yet. dragChanged tells if an open drag moved.
    let edits = 0; let dragChanged = false;
    // gesture is the open opacity gesture, with entry, the history value
    // at its start, and changed, set at its first opacity step. own is true
    // while the gesture changes the history.
    let gesture = null; let own = false;
    // ticket is the open acquisition or import, or null.
    let ticket = null;
    const history = createHistory(options.initial, (checksUnchanged) => {
      edits += 1;
      // A history change that the gesture did not make starts the gesture
      // again from the new value, so its pointer up cannot record an old
      // entry.
      if (gesture && !own) gesture = { entry: history.value, changed: false };
      prune();
      if (options.onChange) options.onChange(checksUnchanged);
    });
    const keyOf = (background) => (background && background.imageKey) || "";
    const sizeOf = (keys) => { let total = 0; for (const key of keys) { const image = images.get(key); if (image) total += image.bytes.byteLength; } return total; };
    // pinnedKeys gives the keys that the table must keep for a publish: the
    // present image, the baseline image and the pinned images.
    const pinnedKeys = () => new Set([...pins.keys(), keyOf(history.background), keyOf(loaded.background)].filter(Boolean));
    function prune() {
      const keys = history.keys();
      for (const key of pinnedKeys()) keys.add(key);
      for (const key of [...images.keys()]) if (!keys.has(key)) images.delete(key);
    }
    // bound drops the oldest undo steps until the table holds at most cap
    // bytes. It never drops the present image, the baseline image or a
    // pinned image. It gives the number of dropped steps.
    function bound() {
      let dropped = 0;
      while (sizeOf(images.keys()) > cap && history.dropOldest()) { dropped += 1; prune(); }
      return dropped;
    }
    // publishError gives the reason why a publish of image, or of no
    // image, for the ticket request cannot go on, or an empty text.
    // abort aborts the open acquisition or import.
    function abort() { if (ticket) { const { controller } = ticket; ticket = null; controller.abort(); } }
    function publishError(request, image) {
      if (!request || request !== ticket || request.signal.aborted) return "A newer action stopped the import.";
      if (edits !== request.edits) return "The draft changed during the import.";
      if (dragChanged || (gesture && gesture.changed)) return "A drag or an opacity change was open at the end of the import. Finish it and import again.";
      if (image) {
        const keys = pinnedKeys(); const held = sizeOf(keys);
        if (held + image.bytes.byteLength > cap) return `The image does not fit in the ${mibText(cap)} MiB image limit of this tab. The images that the tab must keep use ${mibText(held)} MiB, with ${mibText(sizeOf(pins.keys()))} MiB for writes to the browser store.`;
      }
      return "";
    }
    return {
      history,
      cap,
      get loaded() { return clone(loaded); },
      // setLoaded makes value the Reset draft baseline.
      setLoaded(value) { loaded = clone(value); prune(); },
      image(key) { return images.get(key) || null; },
      keys() { return [...images.keys()]; },
      get bytes() { return sizeOf(images.keys()); },
      get keeperBytes() { return sizeOf(pins.keys()); },
      pinCount(key) { return pins.get(key) || 0; },
      pin(key) { pins.set(key, (pins.get(key) || 0) + 1); },
      unpin(key) {
        const count = (pins.get(key) || 0) - 1;
        if (count > 0) pins.set(key, count); else pins.delete(key);
        prune();
      },
      get edits() { return edits; },
      // moveDrag tells the model that an open drag changed its working
      // copy, and endDrag that the drag ended.
      moveDrag() { edits += 1; dragChanged = true; },
      endDrag() { dragChanged = false; },
      get gestureOpen() { return Boolean(gesture); },
      get gestureChanged() { return Boolean(gesture && gesture.changed); },
      // pressOpacity starts an opacity gesture at the present value.
      pressOpacity() { gesture = { entry: history.value, changed: false }; },
      // setOpacity sets the opacity of the present background with no undo
      // step. It gives false when there is no background or no change.
      setOpacity(opacity) {
        const background = history.background;
        if (!background) return false;
        background.opacity = opacity;
        own = true;
        let changed = false;
        try { changed = history.replace({ scenario: history.snapshot.scenario, background }, false); } finally { own = false; }
        if (changed && gesture) gesture.changed = true;
        return changed;
      },
      // endOpacity ends the opacity gesture at a pointer up, a
      // pointercancel or a lost pointer capture. A change since the start
      // of the gesture becomes one undo step. With no open gesture it does
      // nothing, so a second end event changes nothing.
      endOpacity() {
        if (!gesture) return false;
        const { entry } = gesture; gesture = null;
        return history.commitFrom(entry, history.value);
      },
      // start opens a new acquisition or import, and aborts the open one.
      // The ticket has signal, an AbortSignal, and the edit counter.
      start() {
        abort();
        const controller = new AbortController();
        ticket = { signal: controller.signal, controller, edits };
        return ticket;
      },
      abort,
      current(request) { return request === ticket && !request.signal.aborted; },
      publishError,
      // publish runs the final check of publishError, and then, with no
      // wait, adds image to the table and makes value the draft in one
      // history step. With baseline set, value also becomes the Reset draft
      // baseline. Then it drops the oldest undo steps past the cap. It
      // gives error, the reason of a failed check, and dropped, the number
      // of dropped undo steps. The ticket closes in both cases.
      publish(request) {
        const error = publishError(request.ticket, request.image);
        if (request.ticket === ticket) ticket = null;
        if (error) return { error, dropped: 0 };
        if (request.image) images.set(request.image.key, request.image);
        if (request.baseline) loaded = clone(request.value);
        history.replace(request.value);
        prune();
        return { error: "", dropped: bound() };
      },
      // restore installs the image and the background of the stored
      // record, with no undo step, and makes the background part of the
      // baseline.
      restore(image, background) {
        images.set(image.key, image);
        loaded = { scenario: loaded.scenario, background: clone(background) };
        history.reset({ scenario: history.value.scenario, background });
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

  // shellPage gives the parent window when the editor is a frame of the
  // shell page index.html. Otherwise it gives null. The shell page sets
  // podsimShell. The browser blocks a read from a parent on a different
  // origin, so that parent is not the shell page.
  function shellPage(win) {
    try { return win.parent !== win && win.parent.podsimShell === true ? win.parent : null; } catch (_) { return null; }
  }

  const API = {
    withTileMap,
    MIN_LANE_LENGTH, MAX_PODS, MAX_STATIONS, MAX_NODES, MAX_LANES, MAX_NODE_LANES, MAX_FLOWS, MIN_ZOOM, NODE_LABEL_SCALE, NODE_LABEL_SIZE, LANE_PAIR_OFFSET, CHEVRON_LANE_LENGTH, BERTH_PITCH, STATION_PADDING, CLEARANCE, CHECK_DELAY, emptyConfig, fallbackConfig, normalizeConfig, inferStationLanes, addLane, addJunction, addStation, addBerth,
    stationBearing, stationShape, rotateStation, setStationBearing, nextBerthPosition, berthChain, nextChainRow, lanePolyline, laneConflict, removeBerth, moveStation, moveNode, deleteNode, deleteLane, deleteStation, stationFlowCount, setFleetCount, fleetRows, selectionCard, berthFocusID, undoFocus, setDemandPattern,
    laneLength, curveLength, reachable, cutOffStations, stationNodeOwners, dragTargets, validateConfig, configWarnings, checkResults, checkSelector, checkSelection, selectionPoint, focusView,
    GEO_PROJECTION, GEO_RADIUS, GEO_MAX_LATITUDE, MAX_COORDINATE, FRAME_SOURCES, SCALE_TOLERANCE, ALIGN_TOLERANCE, RESAMPLE_MAX_SIDE, makeGeo, geoError, projectPoint, unprojectPoint, frameError, scaleError, framePlacement, placementError, frameAligned, anchorGeo, resampleSize, mercatorY, resampleRows,
    problemCountText, createCheckTimer, validationSummary, checkFocusKey, IMAGE_FILE_BYTES, IMAGE_MAX_SIDE, IMAGE_MAX_PIXELS, imageFacts, imageBytesFacts, dataURLToBytes, bytesToDataURL, checkImageSize,
    IMAGE_KEY_PATTERN, newImageKey, FRAME_STATES, LICENSE_LIMITS, licenseError, assetError, backgroundRecordText, STORED_BACKGROUND_KEPT_TEXT, SERVER_PROJECT_BYTES, PROJECT_FILE_BYTES, SERVER_COMMAND_BYTES, SERVER_COMMAND_JSON_BYTES, GZIP_COMMAND_BYTES, SERVER_TOO_LARGE_TEXT, postCommand, dataURLBytes, serializeDocument, parseDocument, createHistory,
    networkBounds, fitView, zoomScale, nodeLabelSize, pairedLaneIDs, showsChevron, laneOffset, laneCurve, lanePathData, SNAPSHOT_ATTEMPTS, snapshotConsistent, draftBeforeRestart, readState, readSnapshot, applyToServer, applyFailureText, applyFailureStatus, applyToast,
    readLive, readConflict, CONFLICT_UNLOADED_TEXT, conflictView, LOAD_LIVE_QUESTION, applyOverQuestion, loadLive, liveDraft, applyOverBase,
    DRAFT_SAVE_DELAY, DRAFT_STORE_TEXT, DRAFT_UNSAVED_TEXT, DRAFT_DISPLACED_TEXT, DRAFT_STORE, BACKGROUND_STORE, openRecordStore, createDraftKeeper, draftChanged, draftRecordFor, draftOffer, backgroundRecordFor, storedBackground, restoreStoredBackground,
    IMAGE_TABLE_BYTES, DEFAULT_OPACITY, freezeImage, createBackgroundModel, createDecoderSlot, checkImageBytes, resampleImage, frameCenterGeo, referenceFor, framedValue, detachFrame, FRAME_WARNING_TEXT, frameView, attributionParts, BACKGROUND_DURABLE_TEXT, backgroundFacts, exportBackground, documentAsset, STARTUP_EVENTS, createStartupGate, blockInput, startupExempt, startEditor, BACKGROUND_STORE_TEXT, checkBackground, DRAFT_RESTART_TEXT, draftOfferText, RESTORED_RESTART_TEXT, restoreStatusText, shellPage,
  };
  if (typeof module !== "undefined" && module.exports) module.exports = API;
  root.PodsimEditorModel = API;

  if (typeof document === "undefined") {
    if (typeof importScripts === "function") {
      let config = {};
      root.onmessage = ({ data }) => {
        config = Object.fromEntries(data.keys.map((key) => [key, Object.hasOwn(data.patch, key) ? data.patch[key] : config[key]]));
        root.postMessage({ id: data.id, results: checkResults(config) });
      };
    }
    return;
  }

  const $ = (selector) => document.querySelector(selector);
  const svgNS = "http://www.w3.org/2000/svg";
  // checks runs the checks CHECK_DELAY milliseconds after the last draft
  // change. The history schedules it for each change.
  const checks = createCheckTimer({ delay: CHECK_DELAY, run: runValidation, scheduled: scheduleValidation, clock: root });
  // model keeps the draft history with the images of its backgrounds, the
  // Reset draft baseline, the edit counter, the open gestures and the open
  // acquisition. Each draft change schedules both keepers. Changes that can
  // affect validation also schedule the checks.
  const model = createBackgroundModel({
    initial: { scenario: emptyConfig(), background: null },
    onChange: (checksUnchanged) => { if (!checksUnchanged) checks.schedule(); keeper.schedule(); backgroundKeeper.schedule(); },
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
  function nodeFor(config, id) { return config.network.Nodes.find((node) => node.ID === id); }
  function stationForNode(config, id) { const stationID = stationNodeOwners(config).get(id); return config.network.Stations.find((station) => station.ID === stationID); }
  function setDraft(next, record = true) { if (state.history.replace({ scenario: next, background: state.background }, record)) render(); }
  function setBackground(next, record = true) { if (state.history.replace({ scenario: draft(), background: next }, record)) render(); }
  function mutate(change) { if (state.history.edit(change)) render(); }
  function setOperatingFlag(name, value) {
    if (!state.history.setOperatingFlag(name, value)) return;
    if (state.drag && state.drag.working) { render(); return; }
    $("#undoButton").disabled = !state.history.canUndo;
    $("#redoButton").disabled = !state.history.canRedo;
    renderApply();
  }
  function toast(message, error) {
    const element = $("#toast"); element.textContent = message; element.className = error ? "show error" : "show";
    clearTimeout(state.toastTimer); state.toastTimer = setTimeout(() => { element.className = ""; }, 4000);
  }
  function updateStatus(message) { $("#serverStatus").textContent = message; }
  // removeStation deletes the station from the draft. When the delete removes
  // demand profile flows, a toast gives their number.
  function removeStation(stationID) {
    const flows = stationFlowCount(draft(), stationID);
    setDraft(deleteStation(draft(), stationID));
    if (flows > 0) toast(`Station deleted. ${flows} demand ${flows === 1 ? "flow" : "flows"} removed.`);
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
    station: '<label>Name<input data-edit="station-name" maxlength="80"></label><p class="id" data-field="id"></p><label class="check"><input data-edit="parking-only" type="checkbox"> Parking station</label><label>Bearing (degrees)<input data-edit="station-bearing" type="number" step="1"></label><p class="hint">The bearing is the direction from the entry to the exit, clockwise from up. The square marks the entry, and the triangle marks the exit. Drag the shape to move the station, or drag a node to move only that node.</p><div class="berth-list" data-field="berths"><strong>Physical berths</strong></div><button data-action="add-berth" type="button">Add physical berth</button><button data-action="delete-station" class="danger" type="button">Delete station and connections</button>',
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
    if (!card) { state.selection = null; delete panel.dataset.card; panel.className = "empty"; panel.textContent = "No item selected."; return; }
    const key = `${card.type} ${card.id}`;
    if (panel.dataset.card !== key) { panel.dataset.card = key; panel.className = "selection-card"; panel.innerHTML = SELECTION_FORMS[card.type]; }
    const field = (name) => panel.querySelector(`[data-field="${name}"]`); const input = (name) => panel.querySelector(`[data-edit="${name}"]`);
    // Set only a changed value. This keeps the caret in a focused field.
    const setValue = (element, value) => { if (element.value !== value) element.value = value; };
    field("id").textContent = card.id;
    if (card.type === "station") {
      setValue(input("station-name"), card.name); setValue(input("station-bearing"), String(card.bearing)); input("parking-only").checked = card.parkingOnly; renderBerths(field("berths"), card);
    } else if (card.type === "lane") {
      field("route").textContent = `${card.from} → ${card.to}`; setValue(input("lane-speed"), String(card.speed));
      field("length").textContent = `Length: ${card.length.toFixed(1)} m`;
      panel.querySelector('[data-action="toggle-curve"]').textContent = card.curved ? "Make straight" : "Add curve";
    } else field("position").textContent = `Junction at ${card.x.toFixed(1)}, ${card.y.toFixed(1)} m`;
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
    const panel = $("#selectionContent"); const focused = document.activeElement; const before = draft();
    const control = panel.contains(focused) ? { action: focused.dataset.action || "", id: focused.dataset.id || "" } : null;
    if (!(redo ? state.history.redo() : state.history.undo())) return;
    const next = undoFocus({ before, after: draft(), selection: state.selection, control });
    state.selection = next.selection; render();
    if (next.focus === "map") focusMap();
    else if (next.focus) [...panel.querySelectorAll("button[data-action]")].find((button) => button.dataset.action === next.focus.action && (button.dataset.id || "") === next.focus.id)?.focus();
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
      if (input.value !== String(row.count)) input.value = String(row.count);
    });
  }

  function renderDemand() {
    const config = draft(); const demand = config.demand;
    $("#demandEnabled").checked = demand.enabled; $("#demandRate").value = demand.perMinute; $("#demandPattern").value = demand.pattern; $("#demandSeed").value = demand.seed; $("#redistribution").checked = config.redistribution;
    const select = $("#demandDestination"); select.replaceChildren();
    const passenger = config.network.Stations.filter((station) => !station.ParkingOnly);
    for (const station of passenger) { const option = document.createElement("option"); option.value = station.ID; option.textContent = station.Name; select.append(option); }
    if (passenger.length && !passenger.some((station) => station.ID === demand.destination)) {
      const next = { ...config, demand: { ...demand, destination: passenger[0].ID } };
      state.history.replace({ scenario: next, background: state.background }, false);
    }
    select.value = draft().demand.destination; $("#destinationLabel").hidden = demand.pattern !== "destination";
    const profiles = config.demandProfiles || []; const profileSelect = $("#demandProfile"); profileSelect.replaceChildren();
    for (const profile of profiles) { const option = document.createElement("option"); option.value = profile.id; option.textContent = profile.name; profileSelect.append(option); }
    profileSelect.value = demand.profile;
    const profile = profiles.find((item) => item.id === demand.profile); const bandSelect = $("#demandBand"); bandSelect.replaceChildren();
    for (const band of profile?.bands || []) { const option = document.createElement("option"); option.value = band.id; option.textContent = band.name; bandSelect.append(option); }
    bandSelect.value = demand.band; $("#profileLabel").hidden = demand.pattern !== "profile"; $("#bandLabel").hidden = demand.pattern !== "profile";
    $("#demandPattern").querySelector('option[value="profile"]').disabled = profiles.length === 0;
    $("#sharedRidePartyLimit").value = config.sharedRidePartyLimit;
    $("#sharedRideMode").value = config.sharedRideMode;
    $("#sharedRideJoin").value = config.sharedRideJoin;
    $("#sharedRideMaxStops").value = config.sharedRideMaxStops;
    $("#sharedRideMaxStopsLabel").hidden = config.sharedRideMode !== "drop-offs";
    $("#platoonLimit").value = String(config.platoonLimit);
  }

  let drawnNetwork = null, drawnBackground = "", drawnSelection = "";
  function render() {
    // A render draws the draft from the history. It ends a drag of a working
    // copy, so that a pointer up cannot record that copy over a newer draft.
    if (state.drag && state.drag.working) { state.drag = null; model.endDrag(); drawnNetwork = null; }
    state.background = state.history.background;
    const config = draft(); $("#scenarioName").value = config.name; $("#backgroundOpacity").value = state.background ? state.background.opacity : .45; $("#opacityValue").value = `${Math.round(Number($("#backgroundOpacity").value) * 100)}%`;
    $("#undoButton").disabled = !state.history.canUndo; $("#redoButton").disabled = !state.history.canRedo;
    $("#networkMap").dataset.tool = state.tool; $("#cancelLinkButton").hidden = !state.linkFrom;
    renderTools();
    const backgroundKey = JSON.stringify(state.background), selectionKey = JSON.stringify([state.selection, state.linkFrom, state.tool, state.calibrating, state.calibrationPoints]);
    if (config.network !== drawnNetwork || backgroundKey !== drawnBackground || selectionKey !== drawnSelection) {
      renderMap(); drawnNetwork = config.network; drawnBackground = backgroundKey; drawnSelection = selectionKey;
    } else { renderTiles(); }
    renderSelection(); renderFleet(); renderDemand(); updatePrompt(); renderBackground(); renderApply();
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
    $("#mapOpacity").value = value.scenario.map?.opacity ?? .45;
    $("#mapOpacityValue").value = `${Math.round(Number($("#mapOpacity").value) * 100)}%`;
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
  // background. renderDemand can change the draft, so render calls
  // renderApply last. It also disables the apply conflict actions while an
  // apply or a conflict action runs.
  function renderApply() {
    const changed = Boolean(state.live) && !sameDraft(draft(), state.live.snapshot);
    const button = $("#applyButton"); button.disabled = state.applying || !changed;
    button.title = changed || state.applying ? "" : "The draft has no changes to apply.";
    $("#loadLiveButton").disabled = state.applying; $("#applyOverButton").disabled = state.applying;
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
      decode: (restored) => slot.run((signal) => checkImageBytes(restored.image.bytes, decoder, signal)),
      install: (restored) => {
        const image = freezeImage({ key: restored.background.imageKey, ...restored.image });
        model.restore(image, restored.background); storedBytes = image.bytes.byteLength;
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
  function restoreDraft(event) {
    if (!state.offer) return;
    model.abort();
    const { draft: saved, revision, epoch, serverStart } = state.offer; closeOffer(event);
    state.draftBase = { revision, epoch, serverStart };
    state.history.reset({ scenario: normalizeConfig(saved.scenario), background: state.background });
    state.selection = null; render(); fitNetwork(); checks.run(); keeper.arm();
    updateStatus(restoreStatusText({ revision, live: state.live.revision, serverStart, liveStart: state.liveStart }));
    toast("The saved draft is restored.");
  }

  // discardDraft replaces the saved draft with the current draft in one
  // write: a delete, or a put when the draft has changes.
  function discardDraft(event) {
    closeOffer(event); keeper.replace(); toast("The saved draft is discarded.");
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

  function selectItem(type, id) { state.selection = { type, id }; render(); }

  function beginDrag(type, id, event) {
    const drag = { type, id, startClient: { x: event.clientX, y: event.clientY }, originalView: { ...state.view } };
    if (type !== "pan") {
      // Commit a changed form field first. Its change event renders, and a
      // render ends a drag of a working copy.
      const field = document.activeElement;
      if (field && field.matches("input, select, textarea")) field.blur();
      // The drag changes a working copy of the draft and redraws only its
      // targets. The pointer up records the result in the history.
      drag.working = clone(draft()); drag.targets = dragTargets(drag.working, { type, id }); drag.moved = new Set(drag.targets.nodeIDs); drag.last = worldPoint(event);
    }
    state.drag = drag;
    $("#networkMap").setPointerCapture(event.pointerId);
  }

  function onPointerDown(event) {
    if (event.button !== 0) return;
    const target = event.target; const type = target.dataset && target.dataset.type; const id = target.dataset && target.dataset.id;
    if (state.calibrating) {
      if (!state.background) return;
      if (state.calibrationPoints.length < 2) state.calibrationPoints.push(worldPoint(event));
      $("#finishCalibrationButton").disabled = state.calibrationPoints.length !== 2; renderMap(); updatePrompt();
      return;
    }
    if (state.tool === "lane" && (type === "node" || type === "station-node")) {
      if (!state.linkFrom) state.linkFrom = id;
      else if (state.linkFrom !== id) { setDraft(addLane(draft(), state.linkFrom, id, $("#pairedLanes").checked)); state.linkFrom = ""; }
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
    if (state.tool === "junction") { const next = addJunction(draft(), location.X, location.Y); setDraft(next); selectItem("node", next.network.Nodes.at(-1).ID); return; }
    if (state.tool === "station") { const next = addStation(draft(), location.X, location.Y); setDraft(next); selectItem("station", next.network.Stations.at(-1).ID); return; }
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
    if (drag.type === "station") { shiftNodes(config, { ids: drag.moved, dx: location.X - drag.last.X, dy: location.Y - drag.last.Y }); drag.last = location; }
    else if (drag.type === "node") { const node = nodeFor(config, drag.id); if (node) node.Position = { X: location.X, Y: location.Y }; }
    else if (drag.type === "control") { const lane = config.network.Lanes.find((item) => item.ID === drag.id); if (lane) lane.Control = location; }
    // The selection panel shows the new values after the pointer up. A panel
    // change during the drag would make the browser lay out the whole map again.
    drawDragTargets(config, drag.targets);
  }

  function onPointerUp(event) {
    const drag = state.drag;
    if (!drag) return;
    state.drag = null; model.endDrag();
    try { $("#networkMap").releasePointerCapture(event.pointerId); } catch (_) {}
    if (drag.type !== "pan") setDraft(drag.working);
  }

  function fitNetwork() { state.view = fitView(state.map.bounds, $("#networkMap").getBoundingClientRect()); setView(); }

  let validationWorker = null, validationJob = null, validationID = 0, validationFailed = false, validationSent = {};
  function scheduleValidation() {
    if (validationFailed || typeof Worker === "undefined") { runValidation(); return; }
    // Keep at most one worker job. Its completion schedules the latest draft.
    if (validationJob) return;
    try {
      if (!validationWorker) {
        validationWorker = new Worker("./editor.js");
        validationWorker.onmessage = ({ data }) => {
          const job = validationJob; validationJob = null;
          if (job && job.id === data.id && job.config === draft()) showValidation(job.config, data.results);
          else checks.schedule();
        };
        validationWorker.onerror = () => {
          validationWorker.terminate(); validationWorker = null; validationJob = null; validationFailed = true; checks.schedule();
        };
      }
      validationJob = { id: ++validationID, config: draft() };
      const patch = {};
      for (const key of Object.keys(validationJob.config)) if (validationJob.config[key] !== validationSent[key]) patch[key] = validationJob.config[key];
      validationWorker.postMessage({ id: validationJob.id, keys: Object.keys(validationJob.config), patch });
      validationSent = validationJob.config;
    } catch (_) {
      validationWorker?.terminate(); validationWorker = null; validationJob = null; validationFailed = true; runValidation();
    }
  }

  // runValidation checks the draft and shows the results. It gives the
  // errors. Use checks.run to run it, so that a scheduled run is canceled.
  function runValidation() { const config = draft(); const results = checkResults(config); showValidation(config, results); return results.errors; }
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
  // calls it. The live baseline is the draft after the first render,
  // because renderDemand can set the demand destination. When the live
  // scenario cannot load, the local fallback draft is the baseline. Then
  // the editor offers a saved draft that is different. readLive gives the
  // live scenario with the revision, the epoch and the server start ID of
  // the same server state. The load ignores a background in a saved draft
  // of an older editor.
  async function loadServerProject() {
    updateStatus("Loading the live scenario…");
    try {
      const live = await readLive(state.connection);
      state.loadedRevision = live.revision; state.connection.epoch = live.epoch; state.loadedStart = live.serverStart;
      state.draftBase = { revision: live.revision, epoch: live.epoch, serverStart: live.serverStart };
      model.setLoaded({ scenario: clone(live.project), background: null });
      state.history.reset(model.loaded); state.background = null; state.selection = null;
      render(); setLive(state.history.value, state.loadedRevision); fitNetwork();
      updateStatus(`Live revision ${state.loadedRevision}. ${DRAFT_STORE_TEXT[keeper.status]}`);
      // Keep a server scenario that fails the editor checks, and list the
      // problems. Only errors show the error toast.
      const errors = checks.run();
      if (errors.length) toast(`The server scenario has ${errors.length} validation problem${errors.length === 1 ? "" : "s"}. See Checks.`, true);
    } catch (error) {
      model.setLoaded({ scenario: fallbackConfig(), background: null }); state.history.reset(model.loaded);
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
    if (!readyToApply()) return;
    const button = $("#applyButton"); state.applying = true; renderApply(); button.textContent = "Pausing…";
    keeper.flush();
    try {
      const project = draft();
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
      const conflict = await readConflict(state.connection, error);
      if (conflict) showConflict(conflict.revision === null ? null : conflict);
      const unloaded = conflict && conflict.revision === null ? CONFLICT_UNLOADED_TEXT : "";
      updateStatus([applyFailureStatus(error, note), unloaded].filter(Boolean).join(" ")); toast(applyFailureText(error, note), true);
    } finally { state.applying = false; renderApply(); button.textContent = "Pause and apply"; }
  }

  // readyToApply runs the checks. When the draft has errors, it shows them
  // and gives false.
  function readyToApply() {
    const errors = checks.run();
    if (errors.length) { showChecks(); toast("Fix the listed problems before you apply the scenario.", true); }
    return !errors.length;
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
    if (next) await next();
    if (!state.conflict && event && event.detail === 0) focusMap();
  }

  // loadLiveScenario makes the live scenario the draft and the base of
  // Reset draft, with the live revision, epoch and server start ID. It
  // asks first when the draft has scenario changes. The draft keeps its
  // background, because the server does not have it. Undo brings back the
  // replaced draft.
  function loadLiveScenario(event) {
    return runConflictAction(event, async () => {
      const live = await loadLive({ connection: state.connection, changed: draftChanged(state.history.value, state.live), confirm: (text) => root.confirm(text) });
      return live && (() => {
        model.abort();
        const { value, loaded, ...page } = liveDraft(live, { loaded: model.loaded, background: state.background }); Object.assign(state, page); model.setLoaded(loaded);
        state.history.replace(value); state.selection = null;
        render(); setLive({ scenario: draft() }, live.revision);
        showConflict(null); renderOffer(); fitNetwork(); checks.run(); keeper.flush();
        updateStatus(`Live revision ${live.revision}. The draft is the live scenario.`); toast("The live scenario replaced the draft.");
      });
    });
  }

  // applyOver applies the draft over the live revision of the conflict. It
  // asks first. Pause and apply then runs as usual with the base that
  // applyOverBase gives.
  function applyOver(event) {
    if (!state.conflict || state.applying || !readyToApply()) return Promise.resolve();
    return runConflictAction(event, async () => {
      const base = await applyOverBase({ connection: state.connection, revision: state.conflict.revision, serverStart: state.conflict.serverStart, confirm: (text) => root.confirm(text) });
      return base && (() => applyProject(base));
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
    const ticket = model.start();
    try {
      let text = await file.text();
      if (!model.current(ticket)) return;
      const imported = parseDocument(text); text = "";
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
      const result = model.publish({ ticket, image, value: { scenario: imported.scenario, background }, baseline: true });
      if (result.error) { toast(`${result.error} The draft is unchanged.`, true); return; }
      state.selection = null; state.draftBase = { ...state.draftBase, serverStart: state.loadedStart };
      render(); fitNetwork(); checks.run(); toast("The project was imported into the draft.");
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
  // It takes the image descriptor and makes the data URL in the same task,
  // with no wait, so no later change drops the image first.
  function exportProject() {
    const background = state.background ? exportBackground(state.background, model.image(state.background.imageKey)) : null;
    const blob = new Blob([serializeDocument(draft(), background)], { type: "application/json" });
    const url = URL.createObjectURL(blob); const link = document.createElement("a");
    const safe = (draft().name || "scenario").toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "") || "scenario";
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
      const opacity = state.background ? state.background.opacity : DEFAULT_OPACITY;
      publishBackground(ticket, image, { scenario: draft(), background: { imageKey: image.key, x: 0, y: 0, width: facts.width, height: facts.height, opacity, frameState: "none" } }, "image import");
    } catch (error) {
      if (error.name === "AbortError" || !model.current(ticket)) return;
      model.abort(); toast(`${error.message} The background is unchanged.`, true);
    }
  }

  // publishBackground publishes image with value for the ticket of an
  // acquisition, and tells the user of the result.
  function publishBackground(ticket, image, value, kind) {
    if (!model.current(ticket)) return;
    const result = model.publish({ ticket, image, value });
    if (result.error) { toast(`${result.error} The background is unchanged.`, true); return result; }
    render(); fitNetwork(); toast(...keptToast());
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
    const frame = { south: bound("#geoSouth"), north: bound("#geoNorth"), west: bound("#geoWest"), east: bound("#geoEast"), source: $("#geoSource").value };
    const license = {
      source: $("#geoLicenseSource").value.trim(), attribution: $("#geoAttribution").value.trim(), license: $("#geoLicense").value.trim(),
      licenseURL: $("#geoLicenseURL").value.trim(), copyrightURL: $("#geoCopyrightURL").value.trim(), retrieved: new Date().toISOString(), method: "user supplied", notice: $("#geoNotice").value.trim(),
    };
    // A frame or a license that is not valid stops the new import before
    // any file read. The previous acquisition stays canceled.
    const checked = frameError(frame) || licenseError(license);
    if (checked) { toast(`${checked} The background is unchanged.`, true); return; }
    const ticket = model.start();
    const reference = referenceFor({ config: draft(), frame, choice: referenceChoice() });
    if (reference.error) { model.abort(); toast(`${reference.error} The background is unchanged.`, true); return; }
    try {
      const bytes = await file.arrayBuffer();
      if (!model.current(ticket)) return;
      if (slot.busy) updateStatus("The image waits for the decoder.");
      const result = frame.source === "web-mercator"
        ? await slot.run((signal) => resampleImage({ bytes, frame, geo: reference.geo, metersPerPixel: Number($("#geoMeters").value) }, decoder, signal), ticket.signal)
        : { bytes, facts: await slot.run((signal) => checkImageBytes(bytes, decoder, signal), ticket.signal) };
      const image = freezeImage({ key: newImageKey(root.crypto), bytes: result.bytes, mime: result.facts.mime, pixelWidth: result.facts.width, pixelHeight: result.facts.height, frame, license });
      publishBackground(ticket, image, framedValue({ scenario: draft(), background: state.background }, image, reference.geo), "georeferenced image import");
      if (reference.note) updateStatus(reference.note);
    } catch (error) {
      if (error.name === "AbortError" || !model.current(ticket)) return;
      model.abort();
      const hint = frame.source === "web-mercator" && /MiB or smaller/.test(error.message) ? " Choose more meters for each pixel." : "";
      toast(`${error.message}${hint} The background is unchanged.`, true);
    }
  }

  function enableTileMap() {
    model.abort();
    const number = (id) => $(id).value.trim() === "" ? NaN : Number($(id).value);
    try {
      setDraft(withTileMap(draft(), { latitude: number("#mapLatitude"), longitude: number("#mapLongitude"), opacity: Number($("#mapOpacity").value), choice: referenceChoice() }));
      toast("Live map enabled. Pause and apply to show it in the simulation.");
    } catch (error) { toast(error.message, true); }
  }

  // placeFromFrame places the background on its frame in the geo
  // reference of the draft, or in the reference of the reference choice,
  // and attaches the frame, in one history step.
  function placeFromFrame() {
    const image = state.background ? model.image(state.background.imageKey) : null;
    if (!image || !image.frame) return;
    const reference = referenceFor({ config: draft(), frame: image.frame, choice: referenceChoice() });
    if (reference.error) { toast(reference.error, true); return; }
    state.history.replace(framedValue({ scenario: draft(), background: state.background }, image, reference.geo)); render(); fitNetwork();
    if (reference.note) updateStatus(reference.note);
  }

  function finishCalibration() {
    if (!state.background || state.calibrationPoints.length !== 2) return;
    const meters = Number($("#calibrationDistance").value); const [a, b] = state.calibrationPoints; const current = Math.hypot(b.X - a.X, b.Y - a.Y);
    if (!Number.isFinite(meters) || meters <= 0 || current <= 0) { toast("Enter a positive distance and select two different points.", true); return; }
    if (state.background.frameState === "attached") { toast("Detach the frame before you calibrate the scale.", true); return; }
    const factor = meters / current; const background = clone(state.background);
    background.x = a.X + (background.x - a.X) * factor; background.y = a.Y + (background.y - a.Y) * factor; background.width *= factor; background.height *= factor;
    state.calibrating = false; state.calibrationPoints = []; $("#calibrationPanel").hidden = true; setBackground(background); fitNetwork(); toast("The background scale is set. New network geometry uses meters.");
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
    $("#resetButton").addEventListener("click", () => { model.abort(); state.draftBase = { revision: state.loadedRevision, epoch: state.connection.epoch, serverStart: state.loadedStart }; state.history.replace(model.loaded); state.selection = null; render(); fitNetwork(); toast("The draft matches the last loaded project."); });
    $("#validateButton").addEventListener("click", () => checks.run()); $("#applyButton").addEventListener("click", () => applyProject());
    $("#restoreDraftButton").addEventListener("click", restoreDraft); $("#discardDraftButton").addEventListener("click", discardDraft);
    $("#loadLiveButton").addEventListener("click", loadLiveScenario); $("#applyOverButton").addEventListener("click", applyOver);
    // Reset draft also hides the apply conflict actions.
    $("#resetButton").addEventListener("click", () => showConflict(null));
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
    root.addEventListener("beforeunload", (event) => { model.abort(); keeper.flush(); backgroundKeeper.flush(); if (keeper.unsaved || backgroundKeeper.unsaved) { event.preventDefault(); event.returnValue = ""; } });
    $("#problemCount").addEventListener("click", showChecks);
    $("#validationList").addEventListener("click", (event) => { const button = event.target.closest("button[data-type]"); if (button) selectCheck({ type: button.dataset.type, id: button.dataset.id }); });
    $("#exportButton").addEventListener("click", exportProject); $("#projectImport").addEventListener("change", (event) => { importProject(event.target.files[0]); event.target.value = ""; });
    $("#backgroundImport").addEventListener("change", (event) => { importBackground(event.target.files[0]); event.target.value = ""; });
    // The opacity gesture of the model runs from pointer down to pointer
    // up, pointercancel or a lost pointer capture. Each of the three ends
    // it in the same way, and records a change as one undo step.
    const endOpacity = () => { model.endOpacity(); render(); };
    $("#backgroundOpacity").addEventListener("pointerdown", () => model.pressOpacity());
    $("#backgroundOpacity").addEventListener("input", (event) => { const value = Number(event.target.value); $("#opacityValue").value = `${Math.round(value * 100)}%`; if (model.setOpacity(value)) { state.background = state.history.background; renderMap(); } });
    for (const name of ["pointerup", "pointercancel", "lostpointercapture"]) $("#backgroundOpacity").addEventListener(name, endOpacity);
    $("#backgroundOpacity").addEventListener("change", (event) => { if (model.gestureOpen || !state.background) return; const background = clone(state.background); background.opacity = Number(event.target.value); setBackground(background); });
    $("#removeBackgroundButton").addEventListener("click", () => { model.abort(); setBackground(null); state.calibrating = false; state.calibrationPoints = []; $("#calibrationPanel").hidden = true; render(); });
    $("#mapOpenButton").addEventListener("click", () => { state.mapOpen = !state.mapOpen; if (!state.mapOpen) model.abort(); renderBackground(); });
    $("#mapEnableButton").addEventListener("click", enableTileMap);
    $("#mapRemoveButton").addEventListener("click", () => { const next = clone(draft()); delete next.map; setDraft(next); });
    $("#mapRetryButton").addEventListener("click", () => tiles.retry());
    $("#mapOpacity").addEventListener("input", () => {
      $("#mapOpacityValue").value = `${Math.round(Number($("#mapOpacity").value) * 100)}%`;
      if (draft().map) { const next = clone(draft()); next.map.opacity = Number($("#mapOpacity").value); setDraft(next); }
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
    $("#detachFrameButton").addEventListener("click", () => { state.history.replace(detachFrame({ scenario: draft(), background: state.background })); render(); });
    $("#calibrateButton").addEventListener("click", () => {
      if (!state.background) { toast("Choose a background image first.", true); return; }
      if (state.background.frameState === "attached") { toast("Detach the frame before you calibrate the scale.", true); return; }
      state.calibrating = true; state.calibrationPoints = []; $("#calibrationPanel").hidden = false; $("#finishCalibrationButton").disabled = true; updatePrompt(); renderMap();
    });
    $("#finishCalibrationButton").addEventListener("click", finishCalibration); $("#cancelCalibrationButton").addEventListener("click", () => { state.calibrating = false; state.calibrationPoints = []; $("#calibrationPanel").hidden = true; render(); });
    $("#scenarioName").addEventListener("change", (event) => mutate((config) => { config.name = event.target.value.trim(); return config; }));
    $("#demandEnabled").addEventListener("change", (event) => setOperatingFlag("demandEnabled", event.target.checked));
    $("#demandRate").addEventListener("change", (event) => mutate((config) => { config.demand.perMinute = Math.floor(Number(event.target.value)); return config; }));
    $("#demandPattern").addEventListener("change", (event) => setDraft(setDemandPattern(draft(), event.target.value)));
    $("#demandDestination").addEventListener("change", (event) => mutate((config) => { config.demand.destination = event.target.value; return config; }));
    $("#demandProfile").addEventListener("change", (event) => mutate((config) => { config.demand.profile = event.target.value; config.demand.band = config.demandProfiles.find((profile) => profile.id === event.target.value)?.bands?.[0]?.id || ""; return config; }));
    $("#demandBand").addEventListener("change", (event) => mutate((config) => { config.demand.band = event.target.value; return config; }));
    $("#sharedRidePartyLimit").addEventListener("change", (event) => mutate((config) => { config.sharedRidePartyLimit = Math.max(1, Math.min(8, Math.floor(Number(event.target.value) || 1))); return config; }));
    $("#sharedRideMode").addEventListener("change", (event) => mutate((config) => { config.sharedRideMode = sharedRideModes.includes(event.target.value) ? event.target.value : "drop-offs"; return config; }));
    $("#sharedRideJoin").addEventListener("change", (event) => mutate((config) => { config.sharedRideJoin = sharedRideJoins.includes(event.target.value) ? event.target.value : "unassigned"; return config; }));
    $("#sharedRideMaxStops").addEventListener("change", (event) => mutate((config) => { config.sharedRideMaxStops = Math.max(1, Math.min(7, Math.floor(Number(event.target.value) || 3))); return config; }));
    $("#platoonLimit").addEventListener("change", (event) => mutate((config) => { const limit = Number(event.target.value); config.platoonLimit = platoonLimits.includes(limit) ? limit : 0; return config; }));
    $("#demandSeed").addEventListener("change", (event) => mutate((config) => { config.demand.seed = Math.max(0, Math.floor(Number(event.target.value))); return config; }));
    $("#redistribution").addEventListener("change", (event) => setOperatingFlag("redistribution", event.target.checked));
    $("#fleetControls").addEventListener("change", (event) => { if (event.target.dataset.station) setDraft(setFleetCount(draft(), event.target.dataset.station, event.target.value)); });
    $("#selectionContent").addEventListener("change", (event) => {
      if (!state.selection) return;
      if (event.target.dataset.edit === "station-name") mutate((config) => { config.network.Stations.find((item) => item.ID === state.selection.id).Name = event.target.value.trim(); return config; });
      if (event.target.dataset.edit === "parking-only") mutate((config) => { config.network.Stations.find((item) => item.ID === state.selection.id).ParkingOnly = event.target.checked; return config; });
      // After a bearing change, the field shows the bearing from 0 to 359.
      // A value that is not a number does not turn the station.
      if (event.target.dataset.edit === "station-bearing") {
        const bearing = event.target.value === "" ? NaN : Number(event.target.value);
        if (Number.isFinite(bearing)) setDraft(setStationBearing(draft(), state.selection.id, bearing));
        renderSelection();
      }
      if (event.target.dataset.edit === "lane-speed") mutate((config) => { config.network.Lanes.find((item) => item.ID === state.selection.id).SpeedLimit = Number(event.target.value)/3.6; return config; });
    });
    // A delete removes the button that started it. Enter or Space on a
    // button gives a click with detail 0. After such a delete from the
    // keyboard, the focus goes to a nearby control. After a pointer click,
    // the editor does not move the focus.
    $("#selectionContent").addEventListener("click", (event) => {
      const button = event.target.closest("button[data-action]"); if (!button || !state.selection) return; const action = button.dataset.action; const config = draft();
      const berthIDs = action === "remove-berth" ? selectionCard(config, state.selection)?.berths.map((berth) => berth.id) || [] : [];
      if (action === "add-berth") { const result = addBerth(config, state.selection.id); if (result.error) toast(result.error, true); else setDraft(result.config); }
      else if (action === "remove-berth") { const result = removeBerth(config, state.selection.id, button.dataset.id); if (result.error) toast(result.error, true); else setDraft(result.config); }
      else if (action === "delete-station") { removeStation(state.selection.id); state.selection = null; render(); }
      else if (action === "delete-lane") { setDraft(deleteLane(config, state.selection.id)); state.selection = null; render(); }
      else if (action === "delete-node") { const result = deleteNode(config, state.selection.id); if (result.error) toast(result.error, true); else { setDraft(result.config); state.selection = null; render(); } }
      else if (action === "toggle-curve") mutate((next) => { const lane = next.network.Lanes.find((item) => item.ID === state.selection.id); if (lane.Control) delete lane.Control; else { const a = nodeFor(next, lane.From).Position; const b = nodeFor(next, lane.To).Position; lane.Control = { X: (a.X + b.X) / 2 - (b.Y - a.Y) * .25, Y: (a.Y + b.Y) / 2 + (b.X - a.X) * .25 }; } return next; });
      if (event.detail === 0 && !button.isConnected) { if (action === "remove-berth") focusBerthControl(berthFocusID(berthIDs, button.dataset.id)); else focusMap(); }
    });
    document.addEventListener("keydown", (event) => {
      const editing = /INPUT|SELECT|TEXTAREA/.test(document.activeElement.tagName);
      if (event.key === "Escape") { state.linkFrom = ""; state.calibrating = false; state.calibrationPoints = []; $("#calibrationPanel").hidden = true; render(); }
      if (!editing && (event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "z") { event.preventDefault(); stepHistory(event.shiftKey); }
      if (!editing && (event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "y") { event.preventDefault(); stepHistory(true); }
      // A delete of the selected item clears the Selection panel. When a
      // panel button had the focus, the map gets the focus.
      if (!editing && (event.key === "Delete" || event.key === "Backspace") && state.selection) {
        const focused = document.activeElement;
        if (state.selection.type === "lane") setDraft(deleteLane(draft(), state.selection.id));
        else if (state.selection.type === "station") removeStation(state.selection.id);
        else { const result = deleteNode(draft(), state.selection.id); if (result.error) toast(result.error, true); else setDraft(result.config); }
        state.selection = null; render();
        if (!focused.isConnected) focusMap();
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
