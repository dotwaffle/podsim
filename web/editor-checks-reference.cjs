"use strict";

// This reference runs only in Node tests. The browser uses Go checks.
module.exports = function (editor) {
  const { serviceMetadataChecks, laneMinimumLength } = require("./editor-service-reference.cjs");
  const { MAX_NODE_LANES, MAX_STATIONS, MAX_NODES, MAX_LANES, MAX_PODS, MAX_FLOWS, laneLength, geoError } = editor;
  const Tiles = require("./tiles.js");
  const STATION_LANE_ROLES = new Set(["approach", "entry", "berth-access", "through", "departure", "exit"]);
  const sharedRideModes = ["drop-offs", "destination"], sharedRideJoins = ["unassigned", "reassign-existing"], platoonLimits = [0, 2, 3, 4];
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

  // railArrivalErrors checks the portable plan before import or apply.
  function railArrivalErrors(arrivals, passengerIDs) {
    const isRecord = (value) => value !== null && typeof value === "object" && !Array.isArray(value);
    if (arrivals === undefined || arrivals === null) return [];
    if (!Array.isArray(arrivals)) return ["Rail arrivals must be an array."];
    const validID = (id) => typeof id === "string" && id.length > 0 && new TextEncoder().encode(id).length <= 64;
    const errors = []; const ids = new Set(); const releases = new Map(); let total = 0;
    if (arrivals.length > 256) errors.push("The project must contain at most 256 rail arrivals.");
    for (const [index, arrival] of arrivals.entries()) {
      const row = `Rail arrival ${index + 1}`;
      if (!isRecord(arrival)) { errors.push(`${row} must be an object.`); continue; }
      if (!validID(arrival.id) || ids.has(arrival.id)) errors.push(`${row} has an invalid or duplicate ID.`);
      ids.add(arrival.id);
      if (!passengerIDs.has(arrival.station)) errors.push(`${row} needs a passenger hub.`);
      const timeValid = Number.isInteger(arrival.atSeconds) && arrival.atSeconds >= 0 && arrival.atSeconds <= 86400 && Number.isInteger(arrival.walkingSeconds) && arrival.walkingSeconds >= 0 && arrival.walkingSeconds <= 3600 && arrival.atSeconds + arrival.walkingSeconds <= 86400;
      if (!timeValid) errors.push(`${row} has an invalid arrival time or walking delay.`);
      if (!Number.isInteger(arrival.passengers) || arrival.passengers < 1 || arrival.passengers > 200) errors.push(`${row} must offer 1 to 200 passengers.`);
      else {
        total += arrival.passengers;
        if (timeValid) { const tick = Math.max(1, (arrival.atSeconds + arrival.walkingSeconds) * 60); releases.set(tick, (releases.get(tick) || 0) + arrival.passengers); }
      }
      if (!Array.isArray(arrival.destinations) || arrival.destinations.length < 1 || arrival.destinations.length > 16) { errors.push(`${row} needs 1 to 16 destinations.`); continue; }
      const destinations = new Set();
      for (const destination of arrival.destinations) {
        if (!isRecord(destination)) { errors.push(`${row} has an invalid destination.`); continue; }
        if (!passengerIDs.has(destination.station) || destination.station === arrival.station || destinations.has(destination.station)) errors.push(`${row} has an invalid or duplicate destination.`);
        destinations.add(destination.station);
        if (!Number.isInteger(destination.weight) || destination.weight < 1 || destination.weight > 1000000) errors.push(`${row} needs destination weights from 1 to 1000000.`);
      }
    }
    if (total > 10000) errors.push("Rail arrivals must offer at most 10000 passengers.");
    if ([...releases.values()].some((count) => count > 200)) errors.push("Rail arrivals must offer at most 200 passengers at one release tick.");
    return errors;
  }

  function railDepartureErrors(departures, arrivals, passengerIDs) {
    if (departures === undefined || departures === null) return [];
    if (!Array.isArray(departures)) return ["Rail departures must be an array."];
    const errors = [], ids = new Set(), releases = new Map(); let outbound = 0, total = 0;
    if (departures.length + (Array.isArray(arrivals) ? arrivals.length : 0) > 256) errors.push("Rail plans must contain at most 256 combined events.");
    for (const arrival of Array.isArray(arrivals) ? arrivals : []) {
      if (!arrival || !Number.isInteger(arrival.passengers)) continue;
      total += arrival.passengers;
      const tick = Math.max(1, (arrival.atSeconds + arrival.walkingSeconds) * 60);
      releases.set(tick, (releases.get(tick) || 0) + arrival.passengers);
    }
    for (const [index, departure] of departures.entries()) {
      const row = `Rail departure ${index + 1}`;
      if (!departure || typeof departure !== "object" || Array.isArray(departure)) { errors.push(`${row} must be an object.`); continue; }
      if (typeof departure.id !== "string" || !departure.id.length || new TextEncoder().encode(departure.id).length > 64 || ids.has(departure.id)) errors.push(`${row} has an invalid or duplicate ID.`);
      ids.add(departure.id);
      if (!passengerIDs.has(departure.station)) errors.push(`${row} needs a passenger hub.`);
      const timeValid = Number.isInteger(departure.atSeconds) && departure.atSeconds >= 1 && departure.atSeconds <= 86400 && Number.isInteger(departure.walkingSeconds) && departure.walkingSeconds >= 0 && departure.walkingSeconds <= 3600 && Number.isInteger(departure.requestFromSeconds) && departure.requestFromSeconds >= 0 && Number.isInteger(departure.requestUntilSeconds) && departure.requestUntilSeconds >= departure.requestFromSeconds && departure.requestUntilSeconds + departure.walkingSeconds < departure.atSeconds;
      if (!timeValid) errors.push(`${row} has an invalid request window or transfer time.`);
      if (!Number.isInteger(departure.passengers) || departure.passengers < 1 || departure.passengers > 200) errors.push(`${row} must offer 1 to 200 passengers.`);
      else {
        outbound += departure.passengers; total += departure.passengers;
        if (timeValid) for (let i = 0; i < departure.passengers; i += 1) {
          const tick = Math.max(1, departure.requestFromSeconds * 60 + (departure.passengers > 1 ? Math.floor(i * (departure.requestUntilSeconds - departure.requestFromSeconds) * 60 / (departure.passengers - 1)) : 0));
          releases.set(tick, (releases.get(tick) || 0) + 1);
        }
      }
      if (!Array.isArray(departure.origins) || departure.origins.length < 1 || departure.origins.length > 16) { errors.push(`${row} needs 1 to 16 origins.`); continue; }
      const origins = new Set();
      for (const origin of departure.origins) {
        if (!origin || typeof origin !== "object" || Array.isArray(origin)) { errors.push(`${row} has an invalid origin.`); continue; }
        if (!passengerIDs.has(origin.station) || origin.station === departure.station || origins.has(origin.station)) errors.push(`${row} has an invalid or duplicate origin.`);
        origins.add(origin.station);
        if (!Number.isInteger(origin.weight) || origin.weight < 1 || origin.weight > 1000000) errors.push(`${row} needs origin weights from 1 to 1000000.`);
      }
    }
    if (outbound > 3000) errors.push("Rail departures must offer at most 3000 passengers.");
    if (total > 10000) errors.push("Rail plans must offer at most 10000 combined passengers.");
    if ([...releases.values()].some((count) => count > 200)) errors.push("Rail plans must offer at most 200 passengers at one release tick.");
    return errors;
  }

  // validateConfig gives the errors for a scenario. An error blocks an apply
  // or an import. configWarnings gives the checks that do not block. When
  // targets is a Map, validateConfig adds the object that an error names to
  // it, with the error text as the key. checkResults uses these targets.
  function validateConfig(value, targets) {
    const errors = [];
    const report = (text, target) => { errors.push(text); if (targets && target && !targets.has(text)) targets.set(text, target); };
    if (!value || typeof value !== "object" || Array.isArray(value)) return ["The scenario must be a JSON object."];
    const banked = value.network?.Stations?.some((station) => station && Object.hasOwn(station, "Banks"));
    if (![1, 2, 3].includes(value.version)) errors.push("The scenario version must be 1, 2, or 3.");
    else if (value.version === 1 && banked || value.version === 2 && !banked) errors.push("The scenario version does not match its station banks.");
    serviceMetadataChecks(value, report);
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
      if (isRecord(lane) && nodeIDs.has(lane.From) && nodeIDs.has(lane.To) && laneLength(value, lane) < laneMinimumLength(lane)) report(`Lane ${lane.ID} is shorter than ${laneMinimumLength(lane)} m.`, laneTarget);
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
      for (const bank of station.Banks || []) { componentNodes.add(bank.Entry); componentNodes.add(bank.Exit); }
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
        const gates = station.Banks ? station.Banks.find((bank) => bank.BerthIDs?.includes(berth.ID)) : station;
        const entry = reachableAvoiding(directed, gates?.Entry, berth.Node, componentNodes);
        const exit = reachableAvoiding(directed, berth.Node, gates?.Exit, componentNodes);
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
    errors.push(...railArrivalErrors(value.railArrivals, passengerIDs), ...railDepartureErrors(value.railDepartures, value.railArrivals, passengerIDs));
    if (value.geo !== undefined && value.geo !== null) { const geo = geoError(value.geo); if (geo) errors.push(geo); }
    if (value.map !== undefined && value.map !== null && !Tiles.validMap(value.map, value.geo)) errors.push("The map needs provider osm, opacity from 0 to 1, and a geographic reference.");
    if (!demand || !["balanced", "destination", "market", "profile", "profile-daily", "rail-arrivals", "rail-services"].includes(demand.pattern)) errors.push("The passenger demand pattern is invalid.");
    if (isRecord(demand) && "destination" in demand && (typeof demand.destination !== "string" || new TextEncoder().encode(demand.destination).length > 64)) errors.push("The passenger demand destination is invalid.");
    if (demand && demand.pattern === "destination" && !passenger.some((station) => station.ID === demand.destination)) errors.push("Select a passenger destination.");
    if (demand && demand.pattern === "rail-arrivals" && (!Array.isArray(value.railArrivals) || !value.railArrivals.length)) errors.push("Rail-arrivals demand needs a nonempty arrival plan.");
    if (demand && demand.pattern === "rail-services" && !(value.railArrivals || []).length && !(value.railDepartures || []).length) errors.push("Rail-services demand needs a nonempty rail plan.");
    if (demand && ["profile", "profile-daily"].includes(demand.pattern)) {
      const profile = profiles.find((item) => isRecord(item) && item.id === demand.profile);
      if (!profiles.length) errors.push("The project has no demand profiles. Select another pattern.");
      else if (!profile) errors.push("Select a demand profile.");
      else if (demand.pattern === "profile" && !profile.bands.some((band) => isRecord(band) && band.id === demand.band)) errors.push("Select a demand time band.");

    }
    if (!demand || !Number.isSafeInteger(demand.seed) || demand.seed < 0) errors.push("The demand seed must be a nonnegative whole number.");
    if ("sharedRidePartyLimit" in value && (!Number.isInteger(value.sharedRidePartyLimit) || value.sharedRidePartyLimit < 0 || value.sharedRidePartyLimit > 8)) errors.push("The shared ride party limit must be 1 to 8.");
    if ("sharedRideMode" in value && value.sharedRideMode !== "" && !sharedRideModes.includes(value.sharedRideMode)) errors.push("The shared ride mode must be destination or drop-offs.");
    if ("sharedRideJoin" in value && value.sharedRideJoin !== "" && !sharedRideJoins.includes(value.sharedRideJoin)) errors.push("The shared ride join policy must be unassigned or reassign-existing.");
    if ("sharedRideMaxStops" in value && (!Number.isInteger(value.sharedRideMaxStops) || value.sharedRideMaxStops < 0 || value.sharedRideMaxStops > 7)) errors.push("The shared ride stop limit must be 1 to 7.");
    if ("platoonLimit" in value && !platoonLimits.includes(value.platoonLimit)) errors.push("The platoon limit must be 2 to 4, or 0 for no platoons.");
    if ("stationBuffers" in value && typeof value.stationBuffers !== "boolean") errors.push("The station buffer setting must be true or false.");
    if ("pickupReassignment" in value && typeof value.pickupReassignment !== "boolean") errors.push("The pickup reassignment setting must be true or false.");
    if (Object.hasOwn(value, "stationQueueSpacing")) {
      if (!["ordinary", "compact-v1"].includes(value.stationQueueSpacing)) errors.push("Station queue spacing must be ordinary or compact-v1.");
      else if (value.stationQueueSpacing === "compact-v1" && (value.stationBuffers !== true || ![2, 3, 4].includes(value.platoonLimit))) errors.push("Compact station queues require station buffers and a platoon limit from 2 to 4.");
    }
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

  const parse = editor.parseDocument;
  return { validateConfig, configWarnings, checkResults, cutOffStations,
    parseDocument(text, options) {
      const out = parse(text, options);
      if (options?.deferMetadata) return out;
      const errors = validateConfig(out.scenario);
      if (errors.length) throw new Error(`The project has ${errors.length} error${errors.length === 1 ? "" : "s"}. ${errors.slice(0, 3).join(" ")}`);
      return { ...out, scenario: editor.normalizeConfig(out.scenario) };
    },
  };
};
