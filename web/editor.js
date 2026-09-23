(function (root) {
  "use strict";

  const MIN_LANE_LENGTH = 24;
  const DEFAULT_SPEED = 12;
  const STATION_LANE_ROLES = new Set(["approach", "entry", "berth-access", "through", "departure", "exit"]);

  function clone(value) {
    return JSON.parse(JSON.stringify(value));
  }

  function emptyConfig() {
    return {
      version: 1,
      name: "Untitled scenario",
      network: { Nodes: [], Lanes: [], Stations: [] },
      fleet: [],
      demand: { enabled: false, perMinute: 2, pattern: "balanced", destination: "", seed: 1 },
      demandProfiles: [],
      redistribution: false,
    };
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
    config.redistribution = Boolean(config.redistribution);
    return config;
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
    out.network.Nodes.push({ ID: berthNodeID, Position: { X: x, Y: y + 30 } });
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

  function addBerth(config, stationID) {
    const out = clone(config);
    const station = out.network.Stations.find((item) => item.ID === stationID);
    if (!station) return config;
    const entry = point(out, station.Entry);
    const exit = point(out, station.Exit);
    if (!entry || !exit) return config;
    const berthID = nextID(out, `${station.ID}-berth`);
    const nodeID = nextID(out, `${station.ID}-berth-node`);
    const count = station.Berths.length;
    const direction = count % 2 === 0 ? 1 : -1;
    const row = Math.floor((count + 1) / 2) + 1;
    const x = (entry.X + exit.X) / 2;
    const y = (entry.Y + exit.Y) / 2 + direction * row * 30;
    out.network.Nodes.push({ ID: nodeID, Position: { X: x, Y: y } });
    station.Berths.push({ ID: berthID, Node: nodeID });
    return addStationLane(addStationLane(out, station.Entry, nodeID, stationID, "berth-access"), nodeID, station.Exit, stationID, "departure");
  }

  function removeBerth(config, stationID, berthID) {
    const out = clone(config);
    const station = out.network.Stations.find((item) => item.ID === stationID);
    if (!station || station.Berths.length <= 1) return config;
    const berth = station.Berths.find((item) => item.ID === berthID);
    if (!berth) return config;
    station.Berths = station.Berths.filter((item) => item.ID !== berthID);
    out.network.Nodes = out.network.Nodes.filter((node) => node.ID !== berth.Node);
    out.network.Lanes = out.network.Lanes.filter((lane) => lane.From !== berth.Node && lane.To !== berth.Node);
    out.fleet = out.fleet.filter((pod) => pod.BerthID !== berthID);
    return out;
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

  function moveStation(config, stationID, dx, dy) {
    const out = clone(config);
    const station = out.network.Stations.find((item) => item.ID === stationID);
    if (!station) return config;
    const ids = stationNodeIDs(out, station);
    for (const node of out.network.Nodes) {
      if (ids.has(node.ID)) {
        node.Position.X += dx;
        node.Position.Y += dy;
      }
    }
    for (const lane of out.network.Lanes) {
      if (lane.Control && ids.has(lane.From) && ids.has(lane.To)) {
        lane.Control.X += dx;
        lane.Control.Y += dy;
      }
    }
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
    return out;
  }

  function setFleetCount(config, stationID, requested) {
    const out = clone(config);
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

  function laneLength(config, lane) {
    const start = point(config, lane.From);
    const end = point(config, lane.To);
    if (!start || !end) return 0;
    if (!lane.Control) return Math.hypot(end.X - start.X, end.Y - start.Y);
    let length = 0;
    let previous = start;
    for (let i = 1; i <= 16; i += 1) {
      const t = i / 16;
      const u = 1 - t;
      const current = {
        X: u * u * start.X + 2 * u * t * lane.Control.X + t * t * end.X,
        Y: u * u * start.Y + 2 * u * t * lane.Control.Y + t * t * end.Y,
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

  function validateConfig(value) {
    const errors = [];
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
      if (typeof id === "string" && new TextEncoder().encode(id).length > 64) errors.push(`${kind} ID exceeds 64 bytes.`);
      if (typeof id !== "string" || !id.trim()) errors.push(`${kind} has no ID.`);
      else if (idSet.has(id)) errors.push(`ID ${id} is used more than once.`);
      else idSet.add(id);
    }
    const isRecord = (item) => item && typeof item === "object" && !Array.isArray(item);
    const validID = (id) => typeof id === "string" && id.trim() && new TextEncoder().encode(id).length <= 64;
    const validNodes = network.Nodes.filter(isRecord);
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
    const undirected = new Map();
    for (const lane of network.Lanes) {
      uniqueID(lane && lane.ID, "A lane");
      if (!isRecord(lane) || !nodeIDs.has(lane.From) || !nodeIDs.has(lane.To) || lane.From === lane.To) errors.push(`Lane ${(lane && lane.ID) || "?"} has invalid endpoints.`);
      if (!isRecord(lane) || !Number.isFinite(lane.SpeedLimit) || lane.SpeedLimit <= 0) errors.push(`Lane ${(lane && lane.ID) || "?"} needs a positive speed limit.`);
      if (isRecord(lane) && lane.Control && (!isRecord(lane.Control) || !Number.isFinite(lane.Control.X) || !Number.isFinite(lane.Control.Y))) errors.push(`Lane ${lane.ID} has an invalid control point.`);
      if (isRecord(lane) && nodeIDs.has(lane.From) && nodeIDs.has(lane.To) && laneLength(value, lane) < MIN_LANE_LENGTH) errors.push(`Lane ${lane.ID} is shorter than ${MIN_LANE_LENGTH} m.`);
      if (isRecord(lane)) {
        const pair = `${lane.From}\u0000${lane.To}`;
        lanesByPair.add(pair);
        if (!directed.has(lane.From)) directed.set(lane.From, []);
        directed.get(lane.From).push(lane.To);
        for (const [from, to] of [[lane.From, lane.To], [lane.To, lane.From]]) {
          if (!undirected.has(from)) undirected.set(from, []);
          undirected.get(from).push(to);
        }
      }
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
      if ("ParkingOnly" in station && typeof station.ParkingOnly !== "boolean") errors.push(`Station ${station.ID} has an invalid parking setting.`);
      if (typeof station.Name === "string" && new TextEncoder().encode(station.Name).length>80) errors.push(`Station ${station.ID} name exceeds 80 bytes.`);
      if (Array.isArray(station.Berths) && station.Berths.length>200) errors.push(`Station ${station.ID} exceeds 200 berths.`);
      if (typeof station.Name !== "string" || !station.Name.trim()) errors.push(`Station ${station.ID} needs a name.`);
      if (!nodeIDs.has(station.Entry) || !nodeIDs.has(station.Exit) || station.Entry === station.Exit) errors.push(`Station ${station.ID} has invalid entry or exit nodes.`);
      componentNodes.add(station.Entry); componentNodes.add(station.Exit);
      if (!Array.isArray(station.Berths) || station.Berths.length === 0) errors.push(`Station ${station.ID} needs at least one berth.`);
      for (const berth of Array.isArray(station.Berths) ? station.Berths : []) {
        uniqueID(berth && berth.ID, "A berth");
        if (!isRecord(berth)) {
          errors.push(`Station ${station.ID} has an invalid berth.`);
          continue;
        }
        berthIDs.add(berth.ID);
        componentNodes.add(berth.Node);
        if (!nodeIDs.has(berth.Node) || berth.Node === station.Entry || berth.Node === station.Exit) errors.push(`Berth ${berth.ID} has an invalid node.`);
        if (berthNodes.has(berth.Node)) errors.push(`Berth node ${berth.Node} is used more than once.`);
        berthNodes.add(berth.Node);
      }
      if (!lanesByPair.has(`${station.Entry}\u0000${station.Exit}`)) errors.push(`Station ${station.ID} needs a through lane.`);
    }
    // As on the server, a berth route can use a chain of lanes. It cannot pass
    // through the entry, exit, or berth node of a station.
    for (const station of validStations) {
      for (const berth of Array.isArray(station.Berths) ? station.Berths.filter(isRecord) : []) {
        if (!reachableAvoiding(directed, station.Entry, berth.Node, componentNodes)) errors.push(`Berth ${berth.ID} needs an entry lane.`);
        if (!reachableAvoiding(directed, berth.Node, station.Exit, componentNodes)) errors.push(`Berth ${berth.ID} needs an exit lane.`);
      }
    }
    for (const lane of validLanes) {
      const hasStation = typeof lane.StationID === "string" && lane.StationID.length > 0;
      const hasRole = typeof lane.StationRole === "string" && lane.StationRole.length > 0;
      if (hasStation !== hasRole || hasRole && !STATION_LANE_ROLES.has(lane.StationRole)) errors.push(`Lane ${lane.ID} has an invalid station role.`);
      else if (hasStation && !stationIDs.has(lane.StationID)) errors.push(`Lane ${lane.ID} refers to an unknown station.`);
    }
    for (const node of validNodes) {
      if (componentNodes.has(node.ID)) continue;
      const connected = validLanes.some((lane) => lane.From === node.ID || lane.To === node.ID);
      if (!connected) errors.push(`Junction ${node.ID} is disconnected.`);
    }
    if (validNodes.length) {
      const visited = reachableFrom(undirected, validNodes[0].ID);
      if (validNodes.some((node) => !visited.has(node.ID))) errors.push("The network has disconnected sections.");
    }
    if (!Array.isArray(value.fleet)) errors.push("The scenario needs a fleet array.");
    if (typeof value.redistribution !== "boolean") errors.push("The redistribution setting must be true or false.");
    const fleet = Array.isArray(value.fleet) ? value.fleet : [];
    const occupied = new Set();
    for (const pod of fleet) {
      uniqueID(pod && pod.ID, "A pod");
      const podStation = isRecord(pod) && validStations.find((station) => station.ID === pod.StationID);
      const stationBerths = podStation && Array.isArray(podStation.Berths) ? podStation.Berths : [];
      if (!isRecord(pod) || !podStation || !berthIDs.has(pod.BerthID) || !stationBerths.some((berth) => isRecord(berth) && berth.ID === pod.BerthID)) errors.push(`Pod ${(pod && pod.ID) || "?"} has an invalid station or berth.`);
      if (isRecord(pod) && occupied.has(pod.BerthID)) errors.push(`Berth ${pod.BerthID} has more than one pod.`);
      if (isRecord(pod)) occupied.add(pod.BerthID);
    }
    const passenger = validStations.filter((station) => station.ParkingOnly !== true);
    if (passenger.length < 2) errors.push("The network needs at least two passenger stations.");
    if (network.Stations.length > 100 || network.Nodes.length > 2000 || network.Lanes.length > 4000) errors.push("The network exceeds the supported size.");
    if (fleet.length < 1 || fleet.length > 200) errors.push("The fleet must contain 1 to 200 pods.");
    const reachability = new Map();
    for (const origin of passenger) reachability.set(origin.ID, reachableFrom(directed, origin.Exit));
    for (const origin of passenger) {
      for (const destination of passenger) {
        if (origin.ID !== destination.ID && !reachability.get(origin.ID).has(destination.Entry)) errors.push(`${origin.Name} cannot reach ${destination.Name}.`);
      }
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
      if (flows.length < 1 || flows.length > 20000) errors.push(`Demand profile ${profile.id} must contain 1 to 20000 flows.`);
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
    if (!demand || !["balanced", "destination", "market", "profile"].includes(demand.pattern)) errors.push("The passenger demand pattern is invalid.");
    if (isRecord(demand) && "destination" in demand && (typeof demand.destination !== "string" || new TextEncoder().encode(demand.destination).length > 64)) errors.push("The passenger demand destination is invalid.");
    if (demand && demand.pattern === "destination" && !passenger.some((station) => station.ID === demand.destination)) errors.push("Select a passenger destination.");
    if (demand && demand.pattern === "profile") {
      const profile = profiles.find((item) => isRecord(item) && item.id === demand.profile);
      if (!profile) errors.push("Select a demand profile.");
      else if (!profile.bands.some((band) => isRecord(band) && band.id === demand.band)) errors.push("Select a demand time band.");
    }
    if (!demand || !Number.isSafeInteger(demand.seed) || demand.seed < 0) errors.push("The demand seed must be a nonnegative whole number.");
    if ("sharedRidePartyLimit" in value && (!Number.isInteger(value.sharedRidePartyLimit) || value.sharedRidePartyLimit < 0 || value.sharedRidePartyLimit > 8)) errors.push("The shared ride party limit must be 1 to 8.");
    return [...new Set(errors)];
  }

  function serializeDocument(config, background) {
    const document = { format: "podsim", version: 1, scenario: clone(config) };
    if (background && background.dataURL) document.background = clone(background);
    return JSON.stringify(document, null, 2);
  }

  function parseDocument(text) {
    let document;
    try { document = JSON.parse(text); } catch (error) { throw new Error(`The file is not valid JSON. ${error.message}`); }
    if (!document || document.format !== "podsim" || document.version !== 1 || !document.scenario) throw new Error("The file must be a Podsim version 1 project.");
    if (document.background) {
      const item = document.background;
      if (typeof item.dataURL !== "string" || !/^data:image\/(png|jpeg);base64,/.test(item.dataURL)) throw new Error("The background must be a PNG or JPEG data URL.");
      for (const key of ["x", "y", "width", "height", "opacity"]) if (!Number.isFinite(item[key])) throw new Error(`The background ${key} value is invalid.`);
      if (item.width <= 0 || item.height <= 0 || item.opacity < 0 || item.opacity > 1) throw new Error("The background dimensions or opacity are invalid.");
    }
    const scenario = clone(document.scenario);
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
    return { scenario, background: document.background ? clone(document.background) : null };
  }

  function createHistory(initial) {
    let past = [];
    let present = clone(initial);
    let future = [];
    return {
      get value() { return clone(present); },
      get canUndo() { return past.length > 0; },
      get canRedo() { return future.length > 0; },
      replace(next, record) {
        const serialized = JSON.stringify(next);
        if (serialized === JSON.stringify(present)) return false;
        if (record !== false) past.push(clone(present));
        present = clone(next);
        future = [];
        return true;
      },
      commitFrom(before, next) {
        if (JSON.stringify(before) === JSON.stringify(next)) return false;
        past.push(clone(before)); present = clone(next); future = []; return true;
      },
      undo() { if (!past.length) return false; future.push(clone(present)); present = past.pop(); return true; },
      redo() { if (!future.length) return false; past.push(clone(present)); present = future.pop(); return true; },
      reset(next) { past = []; present = clone(next); future = []; },
    };
  }

  const API = {
    MIN_LANE_LENGTH, emptyConfig, normalizeConfig, addLane, addJunction, addStation, addBerth,
    removeBerth, moveStation, moveNode, deleteNode, deleteLane, deleteStation, setFleetCount,
    laneLength, reachable, stationNodeOwners, validateConfig, serializeDocument, parseDocument, createHistory,
  };
  if (typeof module !== "undefined" && module.exports) module.exports = API;
  root.PodsimEditorModel = API;

  if (typeof document === "undefined") return;

  const $ = (selector) => document.querySelector(selector);
  const svgNS = "http://www.w3.org/2000/svg";
  const state = {
    history: createHistory({ scenario: emptyConfig(), background: null }),
    background: null,
    loaded: null,
    loadedRevision: 0,
    epoch: "",
    sequence: 0,
    clientID: root.crypto && root.crypto.randomUUID ? root.crypto.randomUUID() : `editor-${Date.now()}-${Math.random()}`,
    selection: null,
    tool: "select",
    linkFrom: "",
    view: { x: 0, y: 0, scale: 1 },
    drag: null,
    calibrating: false,
    calibrationPoints: [],
    toastTimer: 0,
  };

  function draft() { return state.history.value.scenario; }
  function svgElement(name, attributes) {
    const element = document.createElementNS(svgNS, name);
    for (const [key, value] of Object.entries(attributes || {})) element.setAttribute(key, value);
    return element;
  }
  function nodeFor(config, id) { return config.network.Nodes.find((node) => node.ID === id); }
  function stationForNode(config, id) { const stationID = stationNodeOwners(config).get(id); return config.network.Stations.find((station) => station.ID === stationID); }
  function stationCenter(config, station) {
    const entry = nodeFor(config, station.Entry); const exit = nodeFor(config, station.Exit);
    return entry && exit ? { X: (entry.Position.X + exit.Position.X) / 2, Y: (entry.Position.Y + exit.Position.Y) / 2 } : { X: 0, Y: 0 };
  }
  function esc(value) { return String(value).replace(/[&<>"']/g, (char) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[char])); }
  function setDraft(next, record = true) { if (state.history.replace({ scenario: next, background: state.background }, record)) render(); }
  function setBackground(next, record = true) { if (state.history.replace({ scenario: draft(), background: next }, record)) render(); }
  function mutate(change) { setDraft(change(draft())); }
  function toast(message, error) {
    const element = $("#toast"); element.textContent = message; element.className = error ? "show error" : "show";
    clearTimeout(state.toastTimer); state.toastTimer = setTimeout(() => { element.className = ""; }, 4000);
  }
  function updateStatus(message) { $("#serverStatus").textContent = message; }

  function worldPoint(event) {
    const rect = $("#networkMap").getBoundingClientRect();
    return { X: (event.clientX - rect.left - state.view.x) / state.view.scale, Y: (event.clientY - rect.top - state.view.y) / state.view.scale };
  }
  function setView() { $("#viewport").setAttribute("transform", `translate(${state.view.x} ${state.view.y}) scale(${state.view.scale})`); }
  function zoomAt(factor, clientX, clientY) {
    const rect = $("#networkMap").getBoundingClientRect();
    const sx = clientX - rect.left; const sy = clientY - rect.top;
    const old = state.view.scale; const next = Math.max(.15, Math.min(5, old * factor));
    const wx = (sx - state.view.x) / old; const wy = (sy - state.view.y) / old;
    state.view.scale = next; state.view.x = sx - wx * next; state.view.y = sy - wy * next; setView();
  }

  function lanePath(config, lane) {
    const from = nodeFor(config, lane.From); const to = nodeFor(config, lane.To);
    if (!from || !to) return "";
    if (lane.Control) return `M ${from.Position.X} ${from.Position.Y} Q ${lane.Control.X} ${lane.Control.Y} ${to.Position.X} ${to.Position.Y}`;
    return `M ${from.Position.X} ${from.Position.Y} L ${to.Position.X} ${to.Position.Y}`;
  }

  function renderMap() {
    const config = draft();
    const backgroundLayer = $("#backgroundLayer"); const laneLayer = $("#laneLayer"); const stationLayer = $("#stationLayer"); const nodeLayer = $("#nodeLayer"); const handleLayer = $("#handleLayer");
    backgroundLayer.replaceChildren(); laneLayer.replaceChildren(); stationLayer.replaceChildren(); nodeLayer.replaceChildren(); handleLayer.replaceChildren();
    if (state.background) {
      const image = svgElement("image", { class: "background-image", href: state.background.dataURL, x: state.background.x, y: state.background.y, width: state.background.width, height: state.background.height, opacity: state.background.opacity, preserveAspectRatio: "none" });
      backgroundLayer.append(image);
    }
    for (const lane of config.network.Lanes) {
      const path = svgElement("path", { class: `lane${state.selection && state.selection.type === "lane" && state.selection.id === lane.ID ? " selected" : ""}`, d: lanePath(config, lane), "data-type": "lane", "data-id": lane.ID });
      laneLayer.append(path);
    }
    for (const station of config.network.Stations) {
      const center = stationCenter(config, station);
      const shape = svgElement("rect", { class: `station-shape${state.selection && state.selection.type === "station" && state.selection.id === station.ID ? " selected" : ""}`, x: center.X - 34, y: center.Y - 16, width: 68, height: 32, rx: 8, "data-type": "station", "data-id": station.ID });
      stationLayer.append(shape);
      const label = svgElement("text", { class: "station-label", x: center.X, y: center.Y - 21 }); label.textContent = station.Name; stationLayer.append(label);
    }
    const component = stationNodeOwners(config);
    for (const node of config.network.Nodes) {
      const stationID = component.get(node.ID);
      const circle = svgElement("circle", { class: stationID ? "station-node" : `junction${state.selection && state.selection.type === "node" && state.selection.id === node.ID ? " selected" : ""}`, cx: node.Position.X, cy: node.Position.Y, r: stationID ? 5 : 7, "data-type": stationID ? "station-node" : "node", "data-id": node.ID, "data-station": stationID || "" });
      nodeLayer.append(circle);
      if (!stationID) { const label = svgElement("text", { class: "node-label", x: node.Position.X + 9, y: node.Position.Y - 9 }); label.textContent = node.ID; nodeLayer.append(label); }
    }
    if (state.selection && state.selection.type === "lane") {
      const lane = config.network.Lanes.find((item) => item.ID === state.selection.id);
      if (lane && lane.Control) {
        const from = nodeFor(config, lane.From); const to = nodeFor(config, lane.To);
        handleLayer.append(svgElement("path", { class: "control-line", d: `M ${from.Position.X} ${from.Position.Y} L ${lane.Control.X} ${lane.Control.Y} L ${to.Position.X} ${to.Position.Y}` }));
        handleLayer.append(svgElement("circle", { class: "control-handle", cx: lane.Control.X, cy: lane.Control.Y, r: 7, "data-type": "control", "data-id": lane.ID }));
      }
    }
    if (state.linkFrom) {
      const from = nodeFor(config, state.linkFrom);
      if (from) handleLayer.append(svgElement("circle", { class: "control-handle", cx: from.Position.X, cy: from.Position.Y, r: 10 }));
    }
    for (const calibration of state.calibrationPoints) handleLayer.append(svgElement("circle", { class: "calibration-point", cx: calibration.X, cy: calibration.Y, r: 7 }));
    setView();
  }

  function renderSelection() {
    const panel = $("#selectionContent"); const config = draft();
    if (!state.selection) { panel.className = "empty"; panel.textContent = "No item selected."; return; }
    panel.className = "selection-card";
    if (state.selection.type === "station") {
      const station = config.network.Stations.find((item) => item.ID === state.selection.id);
      if (!station) { state.selection = null; renderSelection(); return; }
      panel.innerHTML = `<label>Name<input data-edit="station-name" maxlength="80" value="${esc(station.Name)}"></label><p class="id">${esc(station.ID)}</p><label class="check"><input data-edit="parking-only" type="checkbox" ${station.ParkingOnly ? "checked" : ""}> Parking station</label><div class="berth-list"><strong>Physical berths</strong>${station.Berths.map((berth) => `<div class="berth-row"><span>${esc(berth.ID)}</span><button data-action="remove-berth" data-id="${esc(berth.ID)}" type="button" ${station.Berths.length <= 1 ? "disabled" : ""}>Remove</button></div>`).join("")}</div><button data-action="add-berth" type="button">Add physical berth</button><button data-action="delete-station" class="danger" type="button">Delete station and connections</button>`;
    } else if (state.selection.type === "lane") {
      const lane = config.network.Lanes.find((item) => item.ID === state.selection.id);
      if (!lane) { state.selection = null; renderSelection(); return; }
      panel.innerHTML = `<p class="id">${esc(lane.ID)}</p><p>${esc(lane.From)} → ${esc(lane.To)}</p><label>Speed limit (km/h)<input data-edit="lane-speed" type="number" min="1" step="1" value="${Math.round(lane.SpeedLimit*3.6)}"></label><p class="hint">Length: ${laneLength(config, lane).toFixed(1)} m</p><button data-action="toggle-curve" type="button">${lane.Control ? "Make straight" : "Add curve"}</button><button data-action="delete-lane" class="danger" type="button">Delete guideway</button>`;
    } else {
      const node = config.network.Nodes.find((item) => item.ID === state.selection.id);
      if (!node) { state.selection = null; renderSelection(); return; }
      panel.innerHTML = `<p class="id">${esc(node.ID)}</p><p>Junction at ${node.Position.X.toFixed(1)}, ${node.Position.Y.toFixed(1)} m</p><button data-action="delete-node" class="danger" type="button">Delete junction and connections</button>`;
    }
  }

  function renderFleet() {
    const config = draft(); const parent = $("#fleetControls"); parent.replaceChildren();
    if (!config.network.Stations.length) { parent.innerHTML = '<p class="empty">Add a station to place pods.</p>'; return; }
    for (const station of config.network.Stations) {
      const count = config.fleet.filter((pod) => pod.StationID === station.ID).length;
      const row = document.createElement("label"); row.className = "fleet-row";
      const name = document.createElement("span"); name.textContent = station.Name;
      const input = document.createElement("input"); input.type = "number"; input.min = "0"; input.max = String(station.Berths.length); input.value = String(count); input.dataset.station = station.ID; input.setAttribute("aria-label", `Initial pods at ${station.Name}`);
      row.append(name, input); parent.append(row);
    }
  }

  function renderDemand() {
    const config = draft(); const demand = config.demand;
    $("#demandEnabled").checked = demand.enabled; $("#demandRate").value = demand.perMinute; $("#demandPattern").value = demand.pattern; $("#demandSeed").value = demand.seed; $("#redistribution").checked = config.redistribution;
    const select = $("#demandDestination"); select.replaceChildren();
    const passenger = config.network.Stations.filter((station) => !station.ParkingOnly);
    for (const station of passenger) { const option = document.createElement("option"); option.value = station.ID; option.textContent = station.Name; select.append(option); }
    if (passenger.length && !passenger.some((station) => station.ID === demand.destination)) {
      config.demand.destination = passenger[0].ID; state.history.replace({ scenario: config, background: state.background }, false);
    }
    select.value = config.demand.destination; $("#destinationLabel").hidden = demand.pattern !== "destination";
    const profiles = config.demandProfiles || []; const profileSelect = $("#demandProfile"); profileSelect.replaceChildren();
    for (const profile of profiles) { const option = document.createElement("option"); option.value = profile.id; option.textContent = profile.name; profileSelect.append(option); }
    profileSelect.value = demand.profile;
    const profile = profiles.find((item) => item.id === demand.profile); const bandSelect = $("#demandBand"); bandSelect.replaceChildren();
    for (const band of profile?.bands || []) { const option = document.createElement("option"); option.value = band.id; option.textContent = band.name; bandSelect.append(option); }
    bandSelect.value = demand.band; $("#profileLabel").hidden = demand.pattern !== "profile"; $("#bandLabel").hidden = demand.pattern !== "profile";
    $("#demandPattern").querySelector('option[value="profile"]').disabled = profiles.length === 0;
    $("#sharedRidePartyLimit").value = config.sharedRidePartyLimit;
  }

  function render() {
    state.background = state.history.value.background;
    const config = draft(); $("#scenarioName").value = config.name; $("#backgroundOpacity").value = state.background ? state.background.opacity : .45; $("#opacityValue").value = `${Math.round(Number($("#backgroundOpacity").value) * 100)}%`;
    $("#undoButton").disabled = !state.history.canUndo; $("#redoButton").disabled = !state.history.canRedo;
    $("#networkMap").dataset.tool = state.tool; $("#cancelLinkButton").hidden = !state.linkFrom;
    renderMap(); renderSelection(); renderFleet(); renderDemand(); updatePrompt();
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
    document.querySelectorAll(".tool").forEach((button) => button.classList.toggle("active", button.dataset.tool === tool));
    render();
  }

  function selectItem(type, id) { state.selection = { type, id }; render(); }

  function beginDrag(type, id, event) {
    const config = draft(); const position = worldPoint(event);
    state.drag = { type, id, before: state.history.value, last: position, startClient: { x: event.clientX, y: event.clientY }, originalView: { ...state.view } };
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
      if (state.tool === "select" && type === "station") beginDrag("station", stationID, event);
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
    if (!state.drag) return;
    if (state.drag.type === "pan") {
      state.view.x = state.drag.originalView.x + event.clientX - state.drag.startClient.x; state.view.y = state.drag.originalView.y + event.clientY - state.drag.startClient.y; setView(); return;
    }
    const location = worldPoint(event); let config = draft();
    if (state.drag.type === "station") { config = moveStation(config, state.drag.id, location.X - state.drag.last.X, location.Y - state.drag.last.Y); state.drag.last = location; }
    else if (state.drag.type === "node") config = moveNode(config, state.drag.id, location.X, location.Y);
    else if (state.drag.type === "control") { config = clone(config); const lane = config.network.Lanes.find((item) => item.ID === state.drag.id); if (lane) lane.Control = location; }
    state.history.replace({ scenario: config, background: state.background }, false); renderMap(); renderSelection();
  }

  function onPointerUp(event) {
    if (!state.drag) return;
    const drag = state.drag; state.drag = null;
    try { $("#networkMap").releasePointerCapture(event.pointerId); } catch (_) {}
    if (drag.type !== "pan") state.history.commitFrom(drag.before, state.history.value);
    render();
  }

  function fitNetwork() {
    const config = draft(); const points = config.network.Nodes.map((node) => node.Position);
    if (state.background) points.push({ X: state.background.x, Y: state.background.y }, { X: state.background.x + state.background.width, Y: state.background.y + state.background.height });
    if (!points.length) { state.view = { x: 0, y: 0, scale: 1 }; setView(); return; }
    const xs = points.map((item) => item.X); const ys = points.map((item) => item.Y); const minX = Math.min(...xs); const maxX = Math.max(...xs); const minY = Math.min(...ys); const maxY = Math.max(...ys);
    const rect = $("#networkMap").getBoundingClientRect(); const scale = Math.max(.15, Math.min(3, Math.min((rect.width - 100) / Math.max(80, maxX - minX), (rect.height - 100) / Math.max(80, maxY - minY))));
    state.view = { scale, x: rect.width / 2 - ((minX + maxX) / 2) * scale, y: rect.height / 2 - ((minY + maxY) / 2) * scale }; setView();
  }

  function runValidation() { return showValidation(validateConfig(draft())); }
  function showValidation(errors) {
    const summary = $("#validationSummary"); const list = $("#validationList"); list.replaceChildren();
    if (!errors.length) { summary.className = "validation good"; summary.textContent = "The scenario is ready to apply."; return errors; }
    summary.className = "validation bad"; summary.textContent = `${errors.length} problem${errors.length === 1 ? "" : "s"} must be fixed.`;
    for (const error of errors) { const item = document.createElement("li"); item.textContent = error; list.append(item); }
    return errors;
  }

  async function getJSON(url) {
    const response = await fetch(url, { headers: { Accept: "application/json" } });
    let body = null; try { body = await response.json(); } catch (_) {}
    if (!response.ok || (body && (body.error || body.Error))) throw new Error((body && (body.error || body.Error)) || `HTTP ${response.status}`);
    return body;
  }
  async function postCommand(command) {
    state.sequence += 1;
    const response = await fetch("/api/command", { method: "POST", headers: { "Content-Type": "application/json", Accept: "application/json" }, body: JSON.stringify({ client: state.clientID, sequence: state.sequence, epoch: state.epoch, ...command }) });
    let body = null; try { body = await response.json(); } catch (_) {}
    if (!response.ok || (body && (body.error || body.Error))) {
      const error = new Error((body && (body.error || body.Error)) || `HTTP ${response.status}`); error.status = response.status; throw error;
    }
    const replyState = body && (body.state || body.State || body);
    if (replyState && (replyState.epoch || replyState.Epoch)) state.epoch = replyState.epoch || replyState.Epoch;
    return body;
  }

  async function loadServerProject() {
    updateStatus("Loading the live scenario…");
    try {
      const [projectReply, liveState] = await Promise.all([getJSON("/api/project"), getJSON("/api/state")]);
      if (!projectReply || !(projectReply.project || projectReply.Project)) throw new Error("The server returned no scenario.");
      const project = normalizeConfig(projectReply.project || projectReply.Project);
      state.loadedRevision = Number(projectReply.revision ?? projectReply.Revision ?? liveState.projectRevision ?? liveState.ProjectRevision ?? 0);
      state.epoch = liveState.epoch || liveState.Epoch || "";
      state.loaded = { scenario: clone(project), background: null };
      state.history.reset(state.loaded); state.background = null; state.selection = null;
      updateStatus(`Live revision ${state.loadedRevision}. Draft changes stay in this browser.`); render(); fitNetwork();
      // Keep a server scenario that fails the editor checks, and list the problems.
      const errors = validateConfig(project);
      if (errors.length) { showValidation(errors); toast(`The server scenario has ${errors.length} validation problem${errors.length === 1 ? "" : "s"}. See Checks.`, true); }
    } catch (error) {
      let fallback = addStation(addStation(emptyConfig(), 100, 120, { name: "Origin" }), 340, 120, { name: "Destination" });
      fallback = addLane(fallback, fallback.network.Stations[0].Exit, fallback.network.Stations[1].Entry, false);
      fallback = addLane(fallback, fallback.network.Stations[1].Exit, fallback.network.Stations[0].Entry, false);
      state.loaded = { scenario: fallback, background: null }; state.history.reset(state.loaded);
      updateStatus("The live scenario could not load. This draft is local."); render(); fitNetwork(); toast(`Load failed. ${error.message}`, true);
    }
  }

  async function runExampleSequence() {
    const button = $("#demoButton"); button.disabled = true;
    try {
      await postCommand({ action: "demo" });
      toast("The example sequence started. Return to the simulation to view it.");
    } catch (error) {
      toast(`The example sequence could not start. ${error.message}`, true);
    } finally { button.disabled = false; }
  }

  async function applyProject() {
    const errors = runValidation(); if (errors.length) { toast("Fix the listed problems before you apply the scenario.", true); return; }
    const button = $("#applyButton"); button.disabled = true; button.textContent = "Pausing…";
    let pausedForApply = false;
    try {
      const current = await getJSON("/api/project");
      const currentRevision = Number(current.revision ?? current.Revision ?? 0);
      if (currentRevision !== state.loadedRevision) {
        const error = new Error("The live scenario changed."); error.status = 409; throw error;
      }
      if (!state.epoch) { const live = await getJSON("/api/state"); state.epoch = live.epoch || live.Epoch || ""; }
      await postCommand({ action: "pause", paused: true });
      pausedForApply = true;
      button.textContent = "Applying…";
      const reply = await postCommand({ action: "project", projectRevision: state.loadedRevision, project: draft() });
      const replyState = reply && (reply.state || reply.State || reply);
      state.loadedRevision = Number(reply.projectRevision ?? reply.ProjectRevision ?? (replyState && (replyState.projectRevision ?? replyState.ProjectRevision)) ?? state.loadedRevision + 1);
      state.loaded = { scenario: draft(), background: state.background ? clone(state.background) : null };
      updateStatus(`Applied revision ${state.loadedRevision}. The simulation is paused.`); toast("The scenario was applied. The simulation remains paused.");
    } catch (error) {
      if (error.status === 409) updateStatus("Apply conflict. Reload the page to get the current live scenario.");
      const conflict = pausedForApply
        ? "The live scenario changed. Your draft is safe. The apply attempt paused the simulation."
        : "The live scenario changed. Your draft is safe and the simulation was not paused.";
      toast(error.status === 409 ? conflict : `Apply failed. ${error.message}`, true);
    } finally { button.disabled = false; button.textContent = "Pause and apply"; }
  }

  function importProject(file) {
    if (!file) return;
    if (file.size > 10 * 1024 * 1024) { toast("The project file must be 10 MB or smaller.", true); return; }
    const reader = new FileReader();
    reader.onerror = () => toast("The project file could not be read. The draft is unchanged.", true);
    reader.onload = () => {
      try {
        const imported = parseDocument(String(reader.result));
        state.history.replace(imported); state.background = imported.background; state.loaded = clone(imported); state.selection = null;
        render(); fitNetwork(); runValidation(); toast("The project was imported into the draft.");
      } catch (error) { toast(`${error.message} The draft is unchanged.`, true); }
    };
    reader.readAsText(file);
  }

  function exportProject() {
    const blob = new Blob([serializeDocument(draft(), state.background)], { type: "application/json" });
    const url = URL.createObjectURL(blob); const link = document.createElement("a");
    const safe = (draft().name || "scenario").toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "") || "scenario";
    link.href = url; link.download = `${safe}.podsim.json`; document.body.append(link); link.click(); link.remove(); URL.revokeObjectURL(url);
  }

  function importBackground(file) {
    if (!file) return;
    if (!/^image\/(png|jpeg)$/.test(file.type)) { toast("Choose a PNG or JPEG image.", true); return; }
    if (file.size > 8 * 1024 * 1024) { toast("The background image must be 8 MB or smaller.", true); return; }
    const reader = new FileReader(); reader.onerror = () => toast("The image could not be read.", true);
    reader.onload = () => {
      const image = new Image(); image.onerror = () => toast("The image is not a valid PNG or JPEG.", true);
      image.onload = () => { setBackground({ dataURL: String(reader.result), x: 0, y: 0, width: image.naturalWidth, height: image.naturalHeight, opacity: .45 }); fitNetwork(); toast("The background stays in this browser until you export the project."); };
      image.src = String(reader.result);
    };
    reader.readAsDataURL(file);
  }

  function finishCalibration() {
    if (!state.background || state.calibrationPoints.length !== 2) return;
    const meters = Number($("#calibrationDistance").value); const [a, b] = state.calibrationPoints; const current = Math.hypot(b.X - a.X, b.Y - a.Y);
    if (!Number.isFinite(meters) || meters <= 0 || current <= 0) { toast("Enter a positive distance and select two different points.", true); return; }
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
    $("#undoButton").addEventListener("click", () => { if (state.history.undo()) { state.selection = null; render(); } });
    $("#redoButton").addEventListener("click", () => { if (state.history.redo()) { state.selection = null; render(); } });
    $("#resetButton").addEventListener("click", () => { if (!state.loaded) return; state.history.replace(state.loaded); state.background = state.loaded.background ? clone(state.loaded.background) : null; state.selection = null; render(); fitNetwork(); toast("The draft matches the last loaded project."); });
    $("#demoButton").addEventListener("click", runExampleSequence);
    $("#validateButton").addEventListener("click", runValidation); $("#applyButton").addEventListener("click", applyProject);
    $("#exportButton").addEventListener("click", exportProject); $("#projectImport").addEventListener("change", (event) => { importProject(event.target.files[0]); event.target.value = ""; });
    $("#backgroundImport").addEventListener("change", (event) => { importBackground(event.target.files[0]); event.target.value = ""; });
    let opacityBefore = null;
    $("#backgroundOpacity").addEventListener("pointerdown", () => { opacityBefore = state.history.value; });
    $("#backgroundOpacity").addEventListener("input", (event) => { const value = Number(event.target.value); $("#opacityValue").value = `${Math.round(value * 100)}%`; if (state.background) { const background = clone(state.background); background.opacity = value; state.history.replace({ scenario: draft(), background }, false); state.background = background; renderMap(); } });
    $("#backgroundOpacity").addEventListener("pointerup", () => { if (opacityBefore) state.history.commitFrom(opacityBefore, state.history.value); opacityBefore = null; render(); });
    $("#backgroundOpacity").addEventListener("change", (event) => { if (opacityBefore || !state.background) return; const background = clone(state.background); background.opacity = Number(event.target.value); setBackground(background); });
    $("#removeBackgroundButton").addEventListener("click", () => { setBackground(null); state.calibrating = false; state.calibrationPoints = []; $("#calibrationPanel").hidden = true; render(); });
    $("#calibrateButton").addEventListener("click", () => {
      if (!state.background) { toast("Choose a background image first.", true); return; }
      
      state.calibrating = true; state.calibrationPoints = []; $("#calibrationPanel").hidden = false; $("#finishCalibrationButton").disabled = true; updatePrompt(); renderMap();
    });
    $("#finishCalibrationButton").addEventListener("click", finishCalibration); $("#cancelCalibrationButton").addEventListener("click", () => { state.calibrating = false; state.calibrationPoints = []; $("#calibrationPanel").hidden = true; render(); });
    $("#scenarioName").addEventListener("change", (event) => mutate((config) => { config.name = event.target.value.trim(); return config; }));
    $("#demandEnabled").addEventListener("change", (event) => mutate((config) => { config.demand.enabled = event.target.checked; return config; }));
    $("#demandRate").addEventListener("change", (event) => mutate((config) => { config.demand.perMinute = Math.floor(Number(event.target.value)); return config; }));
    $("#demandPattern").addEventListener("change", (event) => mutate((config) => { config.demand.pattern = event.target.value; if (event.target.value === "profile" && config.demandProfiles.length) { config.demand.profile = config.demandProfiles[0].id; config.demand.band = config.demandProfiles[0].bands?.[0]?.id || ""; } return config; }));
    $("#demandDestination").addEventListener("change", (event) => mutate((config) => { config.demand.destination = event.target.value; return config; }));
    $("#demandProfile").addEventListener("change", (event) => mutate((config) => { config.demand.profile = event.target.value; config.demand.band = config.demandProfiles.find((profile) => profile.id === event.target.value)?.bands?.[0]?.id || ""; return config; }));
    $("#demandBand").addEventListener("change", (event) => mutate((config) => { config.demand.band = event.target.value; return config; }));
    $("#sharedRidePartyLimit").addEventListener("change", (event) => mutate((config) => { config.sharedRidePartyLimit = Math.max(1, Math.min(8, Math.floor(Number(event.target.value) || 1))); return config; }));
    $("#demandSeed").addEventListener("change", (event) => mutate((config) => { config.demand.seed = Math.max(0, Math.floor(Number(event.target.value))); return config; }));
    $("#redistribution").addEventListener("change", (event) => mutate((config) => { config.redistribution = event.target.checked; return config; }));
    $("#fleetControls").addEventListener("change", (event) => { if (event.target.dataset.station) setDraft(setFleetCount(draft(), event.target.dataset.station, event.target.value)); });
    $("#selectionContent").addEventListener("change", (event) => {
      if (!state.selection) return;
      if (event.target.dataset.edit === "station-name") mutate((config) => { config.network.Stations.find((item) => item.ID === state.selection.id).Name = event.target.value.trim(); return config; });
      if (event.target.dataset.edit === "parking-only") mutate((config) => { config.network.Stations.find((item) => item.ID === state.selection.id).ParkingOnly = event.target.checked; return config; });
      if (event.target.dataset.edit === "lane-speed") mutate((config) => { config.network.Lanes.find((item) => item.ID === state.selection.id).SpeedLimit = Number(event.target.value)/3.6; return config; });
    });
    $("#selectionContent").addEventListener("click", (event) => {
      const button = event.target.closest("button[data-action]"); if (!button || !state.selection) return; const action = button.dataset.action; const config = draft();
      if (action === "add-berth") setDraft(addBerth(config, state.selection.id));
      else if (action === "remove-berth") setDraft(removeBerth(config, state.selection.id, button.dataset.id));
      else if (action === "delete-station") { setDraft(deleteStation(config, state.selection.id)); state.selection = null; render(); }
      else if (action === "delete-lane") { setDraft(deleteLane(config, state.selection.id)); state.selection = null; render(); }
      else if (action === "delete-node") { const result = deleteNode(config, state.selection.id); if (result.error) toast(result.error, true); else { setDraft(result.config); state.selection = null; render(); } }
      else if (action === "toggle-curve") mutate((next) => { const lane = next.network.Lanes.find((item) => item.ID === state.selection.id); if (lane.Control) delete lane.Control; else { const a = nodeFor(next, lane.From).Position; const b = nodeFor(next, lane.To).Position; lane.Control = { X: (a.X + b.X) / 2 - (b.Y - a.Y) * .25, Y: (a.Y + b.Y) / 2 + (b.X - a.X) * .25 }; } return next; });
    });
    document.addEventListener("keydown", (event) => {
      const editing = /INPUT|SELECT|TEXTAREA/.test(document.activeElement.tagName);
      if (event.key === "Escape") { state.linkFrom = ""; state.calibrating = false; state.calibrationPoints = []; $("#calibrationPanel").hidden = true; render(); }
      if (!editing && (event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "z") { event.preventDefault(); if (event.shiftKey ? state.history.redo() : state.history.undo()) { state.selection = null; render(); } }
      if (!editing && (event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "y") { event.preventDefault(); if (state.history.redo()) { state.selection = null; render(); } }
      if (!editing && (event.key === "Delete" || event.key === "Backspace") && state.selection) {
        if (state.selection.type === "lane") setDraft(deleteLane(draft(), state.selection.id));
        else if (state.selection.type === "station") setDraft(deleteStation(draft(), state.selection.id));
        else { const result = deleteNode(draft(), state.selection.id); if (result.error) toast(result.error, true); else setDraft(result.config); }
        state.selection = null; render();
      }
    });
  }

  bindEvents(); render(); loadServerProject();
})(typeof window !== "undefined" ? window : globalThis);
