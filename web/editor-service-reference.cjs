"use strict";

// This reference runs only in Node tests. The browser uses Go metadata checks.
const classes = ["legacy", "compact", "group", "express"];
const record = (value) => value !== null && typeof value === "object" && !Array.isArray(value);
const rows = (value) => Array.isArray(value) ? value : [];
const has = (value, key) => record(value) && Object.keys(value).some((name) => name.toLowerCase() === key.toLowerCase());
const topology = (draft) => [...rows(draft?.network?.Lanes), ...rows(draft?.network?.Stations), ...rows(draft?.network?.Stations).flatMap((station) => rows(station?.Berths))];

function hasServiceMetadata(draft) {
  return has(draft, "expressServices") || has(draft, "stationQueueSpacing") || rows(draft?.fleet).some((pod) => has(pod, "Class")) || topology(draft).some((item) => has(item, "VehicleClasses"));
}

function classSet(item) {
  if (!record(item) || !Object.hasOwn(item, "VehicleClasses")) return ["legacy", "compact"];
  const list = item.VehicleClasses;
  return Array.isArray(list) && list.length >= 1 && list.length <= 4 && new Set(list).size === list.length && list.every((name) => classes.includes(name)) ? list : null;
}

function serviceMetadataChecks(draft, report) {
  if (!hasServiceMetadata(draft)) return;
  if (draft.version !== 3) report("Vehicle and service fields require project version 3.");
  if (topology(draft).some((item) => classSet(item) === null)) report("VehicleClasses must contain 1 to 4 distinct known classes.");
  for (const pod of rows(draft.fleet)) {
    const id = typeof pod?.ID === "string" && pod.ID || "?";
    const target = typeof pod?.StationID === "string" && pod.StationID ? { type: "station", id: pod.StationID } : null;
    const classID = Object.hasOwn(pod || {}, "Class") ? pod.Class : "legacy";
    if (!classes.includes(classID)) { report(`Pod ${id} has an invalid vehicle class.`, target); continue; }
    if (["group", "express"].includes(classID)) { report(`Pod ${id} has no approved physical profile.`, target); continue; }
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

module.exports = { hasServiceMetadata, serviceMetadataChecks };
