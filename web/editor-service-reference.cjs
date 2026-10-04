"use strict";

// This reference runs only in Node tests. The browser uses Go metadata checks.
const classes = ["legacy", "compact", "group", "express"];
const record = (value) => value !== null && typeof value === "object" && !Array.isArray(value);
const rows = (value) => Array.isArray(value) ? value : [];
// fold matches Go strings.EqualFold for an ASCII key. Only U+017F and U+212A
// fold to an ASCII letter.
const fold = (name) => name.replace(/[a-z\u017f\u212a]/g, (letter) => ({ "\u017f": "S", "\u212a": "K" })[letter] || letter.toUpperCase());
const has = (value, key) => record(value) && Object.keys(value).some((name) => fold(name) === fold(key));
const topology = (draft) => [...rows(draft?.network?.Lanes), ...rows(draft?.network?.Stations), ...rows(draft?.network?.Stations).flatMap((station) => rows(station?.Berths))];

function hasServiceMetadata(draft) {
  return has(draft, "orderContract") || has(draft, "expressServices") || has(draft, "stationQueueSpacing") || rows(draft?.fleet).some((pod) => has(pod, "Class")) || topology(draft).some((item) => has(item, "VehicleClasses"));
}

// Coupling presence includes null and empty values, as native project decoding counts them.
const couplingKeys = ["couplingContract", "couplingEnabled", "couplingSites", "couplingCorridors"];

function couplingContractError(draft) {
  if (draft?.version !== 5) return couplingKeys.some((key) => has(draft, key)) ? "Coupling fields require project version 5." : "";
  if (draft.couplingContract !== "compact-pair-v1") return "Project version 5 requires couplingContract compact-pair-v1.";
  if (Object.hasOwn(draft, "couplingEnabled") && typeof draft.couplingEnabled !== "boolean") return "The train setting must be true or false.";
  if (["couplingSites", "couplingCorridors"].some((key) => Object.hasOwn(draft, key) && !Array.isArray(draft[key]))) return "Coupling sites and corridors must be arrays.";
  return "";
}

function serviceContractError(draft) {
  const couplingError = couplingContractError(draft);
  if (couplingError) return couplingError;
  // Version 5 accepts service metadata. Its order contract stays optional.
  if (draft?.version === 5) return has(draft, "orderContract") && draft.orderContract !== "express-v1" ? "Project version 5 accepts only orderContract express-v1." : "";
  if (draft?.version === 4) return draft.orderContract === "express-v1" ? "" : "Project version 4 requires orderContract express-v1.";
  if (has(draft, "orderContract")) return "The order contract requires project version 4.";
  return hasServiceMetadata(draft) && draft?.version !== 3 ? "Vehicle and service fields require project version 3." : "";
}

function classSet(item) {
  if (!record(item) || !Object.hasOwn(item, "VehicleClasses")) return ["legacy", "compact"];
  const list = item.VehicleClasses;
  return Array.isArray(list) && list.length >= 1 && list.length <= 4 && new Set(list).size === list.length && list.every((name) => classes.includes(name)) ? list : null;
}

function serviceMetadataChecks(draft, report) {
  const contractError = serviceContractError(draft);
  if (contractError) report(contractError);
  couplingGeometryChecks(draft, report);
  if (!hasServiceMetadata(draft)) return;
  if (topology(draft).some((item) => classSet(item) === null)) report("VehicleClasses must contain 1 to 4 distinct known classes.");
  for (const pod of rows(draft.fleet)) {
    const id = typeof pod?.ID === "string" && pod.ID || "?";
    const target = typeof pod?.StationID === "string" && pod.StationID ? { type: "station", id: pod.StationID } : null;
    const classID = Object.hasOwn(pod || {}, "Class") ? pod.Class : "legacy";
    if (!classes.includes(classID)) { report(`Pod ${id} has an invalid vehicle class.`, target); continue; }
    if (classID === "express" && (![4, 5].includes(draft.version) || draft.orderContract !== "express-v1")) { report(`Pod ${id} has no approved physical profile.`, target); continue; }
    const station = rows(draft.network?.Stations).find((item) => item?.ID === pod?.StationID);
    const berth = rows(station?.Berths).find((item) => item?.ID === pod?.BerthID);
    if (berth && classSet(station) && classSet(berth) && (!classSet(station).includes(classID) || !classSet(berth).includes(classID))) report(`Pod ${id} has an incompatible station or berth.`, target);
  }
  if (Object.hasOwn(draft, "expressServices")) checkRegistry(draft, report);
}

function checkRegistry(draft, report) {
  const services = draft.expressServices;
  if (!Array.isArray(services) || services.length > 300) { report("Express services must be an array of at most 300 records."); return; }
  if (services.length && topology(draft).some((item) => classSet(item) === null)) { report("Express services or their network have invalid fields."); return; }
  const validID = (id) => typeof id === "string" && id.length > 0 && new TextEncoder().encode(id).length <= 64;
  const ids = new Set();
  for (const service of services) {
    if (service === null) { report("Express services: invalid express service."); return; }
    if (!record(service) || Object.keys(service).some((key) => !["ID", "From", "To", "Class", "PartyLimit"].includes(key))) { report("Express services or their network have invalid fields."); return; }
    if (["ID", "From", "To", "Class"].some((key) => service[key] != null && typeof service[key] !== "string") || service.PartyLimit != null && !Number.isInteger(service.PartyLimit)) { report("Express services or their network have invalid fields."); return; }
    if (!validID(service.ID) || !validID(service.From) || !validID(service.To) || service.From === service.To || service.Class !== "express" || !Number.isInteger(service.PartyLimit) || service.PartyLimit < 1 || service.PartyLimit > 20) { report("Express services: invalid express service."); return; }
    if (ids.has(service.ID)) { report("Express services: duplicate express service."); return; }
    ids.add(service.ID);
    const from = rows(draft.network?.Stations).find((station) => station?.ID === service.From);
    const to = rows(draft.network?.Stations).find((station) => station?.ID === service.To);
    if (!from || !to || from.ParkingOnly || to.ParkingOnly || !expressPath(draft.network, from, to)) { report(`Express services: express service ${service.ID} has incompatible endpoints or paths.`); return; }
  }
}

function expressPath(network, from, to) {
  const allows = (item) => classSet(item)?.includes("express");
  if (!allows(from) || !allows(to)) return false;
  const blocked = new Set();
  for (const station of rows(network?.Stations)) {
    for (const berth of rows(station?.Berths)) if (!allows(station) || !allows(berth)) blocked.add(berth?.Node);
    if (!allows(station)) for (const node of [station?.Entry, station?.Exit, ...rows(station?.Banks).flatMap((bank) => [bank?.Entry, bank?.Exit])]) blocked.add(node);
  }
  const adjacency = new Map();
  const nodeIDs = new Set(rows(network?.Nodes).map((node) => node?.ID));
  const stationAllows = new Map(rows(network?.Stations).map((station) => [station?.ID, allows(station)]));
  for (const lane of rows(network?.Lanes)) if (allows(lane) && stationAllows.get(lane?.StationID) !== false && nodeIDs.has(lane?.From) && nodeIDs.has(lane?.To) && !blocked.has(lane?.From) && !blocked.has(lane?.To)) {
    if (!adjacency.has(lane.From)) adjacency.set(lane.From, []);
    adjacency.get(lane.From).push(lane.To);
  }
  const destinations = new Set(rows(to.Berths).filter(allows).map((berth) => berth.Node));
  const queue = rows(from.Berths).filter(allows).map((berth) => berth.Node), seen = new Set(queue);
  for (let i = 0; i < queue.length; i++) {
    if (destinations.has(queue[i])) return true;
    for (const next of adjacency.get(queue[i]) || []) if (!seen.has(next)) { seen.add(next); queue.push(next); }
  }
  return false;
}

// COUPLING_ROOM is sim.CouplingSiteRoom of compact-pair-v1, in meters.
const COUPLING_ROOM = { spacing: 12, opening: 7.5, margin: 14.258333333333333, required: 48.016666666666666 };
const COUPLING_SLACK = 1e-9;
const invalidGeometry = (text) => `${text}: invalid coupling geometry`;

// couplingRecords gives the sites or corridors as Go decodes them into
// their records, or null when a record has an unknown member or a value of
// the wrong type. Null gives the zero value.
function couplingRecords(list, fields) {
  const out = [];
  for (const item of list) {
    if (item === null) { out.push(Object.fromEntries(Object.entries(fields).map(([key, kind]) => [key, kind === "number" ? 0 : kind === "string" ? "" : []]))); continue; }
    if (!record(item) || Object.keys(item).some((key) => !Object.hasOwn(fields, key))) return null;
    const decoded = {};
    for (const [key, kind] of Object.entries(fields)) {
      const value = item[key];
      if (kind === "list") {
        if (value != null && (!Array.isArray(value) || value.some((id) => id !== null && typeof id !== "string"))) return null;
        decoded[key] = rows(value).map((id) => id ?? "");
      } else if (value == null) decoded[key] = kind === "number" ? 0 : "";
      else if (typeof value !== kind) return null;
      else decoded[key] = value;
    }
    out.push(decoded);
  }
  return out;
}

// couplingGeometryChecks follows sim.ValidateCouplingGeometry for a version
// 5 draft. It does not repeat the native network record checks, which
// the other checks report.
function couplingGeometryChecks(draft, report) {
  if (draft?.version !== 5 || couplingContractError(draft)) return;
  if (!rows(draft.couplingSites).length && !rows(draft.couplingCorridors).length) return;
  const sites = couplingRecords(rows(draft.couplingSites), { id: "string", laneId: "string", startMeters: "number", endMeters: "number", frontStagingMeters: "number", rearStagingMeters: "number" });
  const corridors = couplingRecords(rows(draft.couplingCorridors), { id: "string", assemblySiteId: "string", splitSiteId: "string", laneIds: "list" });
  if (!sites || !corridors) { report("Coupling sites, corridors, or their network have invalid fields."); return; }
  const problem = couplingGeometryError(draft.network, sites, corridors);
  if (problem) report(problem);
}

function couplingGeometryError(network, sites, corridors) {
  const nodes = rows(network?.Nodes), lanes = rows(network?.Lanes);
  if (sites.length < 2 || sites.length > 300 || corridors.length < 1 || corridors.length > 300 || nodes.length < 1 || nodes.length > 5000 || lanes.length < 1 || lanes.length > 8000 || rows(network?.Stations).length > 300) return invalidGeometry("coupling collections exceed bounds");
  const positions = new Map(nodes.map((node) => [node?.ID, node?.Position]));
  const geometry = new Map();
  for (const lane of lanes) {
    const from = positions.get(lane?.From) || { X: 0, Y: 0 }, to = positions.get(lane?.To) || { X: 0, Y: 0 };
    const length = Math.hypot(to.X - from.X, to.Y - from.Y);
    geometry.set(lane?.ID, { lane, from, to, length, direction: length > 0 ? { X: (to.X - from.X) / length, Y: (to.Y - from.Y) / length } : { X: 0, Y: 0 } });
  }
  const bounded = (id) => id !== "" && new TextEncoder().encode(id).length <= 64;
  const laneError = (lane) => lane.Control != null || JSON.stringify(classSet(lane)) !== '["compact"]' ? invalidGeometry(`lane ${JSON.stringify(lane.ID)} must be straight and explicitly Compact-only`) : "";
  const placed = new Map();
  for (const site of sites) {
    if (!bounded(site.id) || !bounded(site.laneId)) return invalidGeometry("invalid site or lane ID");
    if (placed.has(site.id)) return invalidGeometry("invalid or duplicate site ID");
    const lane = geometry.get(site.laneId);
    if (!lane) return invalidGeometry("unknown or invalid site lane ID");
    const error = laneError(lane.lane);
    if (error) return error;
    const values = [site.startMeters, site.endMeters, site.frontStagingMeters, site.rearStagingMeters];
    if (values.some((value) => !Number.isFinite(value))) return invalidGeometry(`nonfinite site ${JSON.stringify(site.id)}`);
    if (site.startMeters < 0 || site.endMeters > lane.length || site.endMeters - site.startMeters < COUPLING_ROOM.required ||
      Math.abs(site.frontStagingMeters - site.rearStagingMeters - COUPLING_ROOM.spacing) > COUPLING_SLACK ||
      site.rearStagingMeters - COUPLING_ROOM.margin < site.startMeters ||
      site.frontStagingMeters + COUPLING_ROOM.opening + COUPLING_ROOM.margin > site.endMeters) return invalidGeometry(`site ${JSON.stringify(site.id)} lacks staging, maneuver, or stopping room`);
    for (const previous of placed.values()) {
      if (site.laneId === previous.laneId && site.startMeters < previous.endMeters && previous.startMeters < site.endMeters) return invalidGeometry(`protected sites overlap on ${JSON.stringify(site.laneId)}`);
    }
    placed.set(site.id, site);
  }
  const seen = new Set(), used = new Set();
  for (const corridor of corridors) {
    const path = corridor.laneIds;
    if (!bounded(corridor.id) || seen.has(corridor.id) || path.length < 1 || path.length > 8000) return invalidGeometry("invalid corridor identity or path");
    seen.add(corridor.id);
    if (!bounded(corridor.assemblySiteId) || !bounded(corridor.splitSiteId)) return invalidGeometry("invalid endpoint site ID");
    const assembly = placed.get(corridor.assemblySiteId), split = placed.get(corridor.splitSiteId);
    if (!assembly || !split || assembly.id === split.id || path[0] !== assembly.laneId || path.at(-1) !== split.laneId) return invalidGeometry(`corridor ${JSON.stringify(corridor.id)} does not bind distinct endpoint sites`);
    if (path.length === 1 && assembly.endMeters > split.startMeters) return invalidGeometry(`corridor ${JSON.stringify(corridor.id)} reverses its site order`);
    const error = couplingPathError(corridor, geometry, bounded, laneError);
    if (error) return error;
    used.add(assembly.id); used.add(split.id);
  }
  for (const id of placed.keys()) if (!used.has(id)) return invalidGeometry(`site ${JSON.stringify(id)} has no corridor`);
  return "";
}

function couplingPathError(corridor, geometry, bounded, laneError) {
  const seen = new Set();
  let previous = null, direction = null, origin = null;
  for (const id of corridor.laneIds) {
    if (!bounded(id)) return invalidGeometry("invalid corridor lane ID");
    const lane = geometry.get(id);
    if (!lane || seen.has(id)) return invalidGeometry("invalid corridor lane ID");
    seen.add(id);
    const error = laneError(lane.lane);
    if (error) return error;
    if (!Number.isFinite(lane.length) || lane.length <= 0) return invalidGeometry("zero or nonfinite corridor segment");
    if (!previous) { direction = lane.direction; origin = lane.from; }
    else if (previous.To !== lane.lane.From || direction.X * lane.direction.X + direction.Y * lane.direction.Y <= 0) return invalidGeometry(`corridor ${JSON.stringify(corridor.id)} is disconnected or reversed`);
    for (const point of [lane.from, lane.to]) {
      const distance = Math.abs((point.X - origin.X) * direction.Y - (point.Y - origin.Y) * direction.X);
      if (!Number.isFinite(distance) || distance > COUPLING_SLACK) return invalidGeometry("corridor endpoint leaves its fixed XY axis");
    }
    previous = lane.lane;
  }
  return "";
}

function laneMinimumLength(lane) {
  const allowed = classSet(lane);
  return allowed && (allowed.includes("group") || allowed.includes("express")) ? 40 : 24;
}

module.exports = { hasServiceMetadata, serviceContractError, serviceMetadataChecks, laneMinimumLength };
