(function (root) {
  "use strict";
  const MAX_ITEMS = 65536, MAX_COORDINATE = 100000;
  const object = (value) => value !== null && typeof value === "object" && !Array.isArray(value);
  const list = (value) => Array.isArray(value) && value.length <= MAX_ITEMS;
  const text = (value) => typeof value === "string" && value.length <= 4096;
  const id = (value) => text(value) && value.length > 0;
  const count = (value) => Number.isSafeInteger(value) && value >= 0;
  const position = (value) => object(value) && [value.x, value.y].every((n) => typeof n === "number" && Number.isFinite(n) && Math.abs(n) <= MAX_COORDINATE);
  const ORDER_ID_BYTES = 64, ORDER_TEXT_BYTES = 1024;
  const ORDER_FIELDS = new Set(["sharingConsent", "service", "serviceID", "id", "from", "legFrom", "to", "partySize", "podID", "completed", "requestedTick", "boardedTick", "dispatchReason"]);
  const BASE64 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";

  // HTTP order text is packed even though topology IDs and pod fields are not.
  // Validate padding and unused bits before allocation, then use fatal UTF-8.
  // ignoreBOM:true preserves a leading U+FEFF as actual text, as Go does.
  function unpackOrderText(packed, freeText = false) {
    const limit = freeText ? ORDER_TEXT_BYTES : ORDER_ID_BYTES;
    if (typeof packed !== "string" || packed.length > 4 * Math.ceil(limit / 3) ||
      !/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(packed)) throw new Error("Invalid canonical packed order text.");
    const padding = packed.endsWith("==") ? 2 : packed.endsWith("=") ? 1 : 0;
    const size = packed.length / 4 * 3 - padding;
    if (size > limit || padding === 2 && (BASE64.indexOf(packed.at(-3)) & 15) !== 0 || padding === 1 && (BASE64.indexOf(packed.at(-2)) & 3) !== 0) throw new Error("Invalid canonical packed order text.");
    const bytes = new Uint8Array(size);
    let offset = 0;
    for (let i = 0; i < packed.length; i += 4) {
      const a = BASE64.indexOf(packed[i]), b = BASE64.indexOf(packed[i + 1]);
      const c = packed[i + 2] === "=" ? 0 : BASE64.indexOf(packed[i + 2]);
      const d = packed[i + 3] === "=" ? 0 : BASE64.indexOf(packed[i + 3]);
      bytes[offset++] = (a << 2) | (b >> 4);
      if (offset < size) bytes[offset++] = ((b & 15) << 4) | (c >> 2);
      if (offset < size) bytes[offset++] = ((c & 3) << 6) | d;
    }
    let decoded;
    try { decoded = new TextDecoder("utf-8", { fatal: true, ignoreBOM: true }).decode(bytes); }
    catch (_) { throw new Error("Invalid packed order UTF-8."); }
    if (freeText ? /[\u0000-\u001f\u007f-\u009f]/.test(decoded) : !/^[A-Za-z0-9.+-]*$/.test(decoded)) throw new Error(freeText ? "Order text has a control character." : "Invalid order ID characters.");
    return decoded;
  }

  function unpackPendingRequest(request) {
    if (!object(request) || Object.keys(request).some((key) => !ORDER_FIELDS.has(key))) throw new Error("Unknown or invalid pending order fields.");
    if (!["sharingConsent", "service", "id", "from", "to", "partySize", "podID", "completed", "requestedTick", "dispatchReason"].every((key) => Object.hasOwn(request, key)) ||
      !count(request.id) || request.id === 0 || !count(request.partySize) || request.partySize === 0 ||
      !["private", "shared"].includes(request.sharingConsent) || !["on-demand", "express"].includes(request.service) ||
      request.completed !== false || !count(request.requestedTick) ||
      Object.hasOwn(request, "boardedTick") && !count(request.boardedTick)) throw new Error("Missing or invalid pending order data.");
    const next = { ...request };
    for (const key of ["from", "to", "podID", "dispatchReason", "serviceID", "legFrom"]) {
      // Only these two string fields have omitempty in sim.Request.
      const optional = key === "serviceID" || key === "legFrom";
      next[key] = optional && !Object.hasOwn(request, key) ? "" : unpackOrderText(request[key], key === "dispatchReason");
    }
    if (!next.from || !next.to) throw new Error("Pending order origin or destination is missing.");
    if (!Object.hasOwn(next, "boardedTick")) next.boardedTick = 0;
    return next;
  }
  function index(items) {
    if (!list(items)) return null;
    const map = new Map();
    for (const item of items) {
      if (!object(item) || !id(item.id) || map.has(item.id)) return null;
      map.set(item.id, item);
    }
    return map;
  }

  // The editor supplies its existing 16-segment quadratic length routine.
  // Spacings are straight chords in authored berth order, not swept clearance.
  function measurements(config, selection, curveLength, enabled = false) {
    const result = { enabled: Boolean(enabled), items: [], unavailable: [], scope: "Draft geometry in meters; not surveyed distances or safety clearances." };
    if (!enabled) return result;
    const network = config?.network, nodes = index(network?.nodes), lanes = index(network?.lanes), stations = index(network?.stations);
    if (!nodes || !lanes || !stations || typeof curveLength !== "function") { result.unavailable.push("Draft geometry unavailable."); return result; }
    const at = (nodeID) => position(nodes.get(nodeID)?.position) ? { ...nodes.get(nodeID).position } : null;
    function laneItem(lane) {
      const from = at(lane.from), to = at(lane.to), control = lane.control;
      if (!from || !to || control != null && !position(control)) { result.unavailable.push(`Lane ${lane.id}: invalid or missing geometry.`); return; }
      const curve = { from, to, ...(control ? { control: { ...control } } : {}) };
      const meters = curveLength(curve);
      if (!Number.isFinite(meters) || meters < 0 || meters > 4 * Math.SQRT2 * MAX_COORDINATE) { result.unavailable.push(`Lane ${lane.id}: length unavailable.`); return; }
      result.items.push({ kind: "lane", label: `Lane ${lane.id}`, ...curve, meters,
        method: control ? "curve approximation, 16 segments" : "straight centerline" });
    }
    if (selection?.type === "lane" && lanes.has(selection.id)) laneItem(lanes.get(selection.id));
    else if (selection?.type === "station" && stations.has(selection.id)) {
      const station = stations.get(selection.id);
      for (const lane of lanes.values()) if (lane.stationID === station.id) laneItem(lane);
      if (!list(station.berths)) result.unavailable.push("Berth center spacing unavailable.");
      else for (let i = 1; i < station.berths.length; i++) {
        const a = station.berths[i - 1], b = station.berths[i];
        const from = at(a?.node), to = at(b?.node);
        if (!id(a?.id) || !id(b?.id) || !from || !to) { result.unavailable.push(`Berth pair ${i}/${i + 1}: center spacing unavailable.`); continue; }
        result.items.push({ kind: "spacing", label: `Berths ${a.id} / ${b.id}`, from, to,
          meters: Math.hypot(to.x - from.x, to.y - from.y), method: "straight center chord, authored berth order" });
      }
    } else result.unavailable.push("Select a station or guideway to measure.");
    return result;
  }

  function identity(value) {
    return object(value) && id(value.epoch) && /^[A-Za-z0-9.+-]+$/.test(value.epoch) && id(value.serverStart) && /^[A-Za-z0-9.+-]+$/.test(value.serverStart) && count(value.projectRevision)
      ? { epoch: value.epoch, serverStart: value.serverStart, projectRevision: value.projectRevision } : null;
  }
  function sameIdentity(a, b) {
    const x = identity(a), y = identity(b);
    return Boolean(x && y && x.epoch === y.epoch && x.serverStart === y.serverStart && x.projectRevision === y.projectRevision);
  }
  function draftScope(report, base, unchanged) {
    if (!sameIdentity(report?.identity, base)) return "Live snapshot identity differs from the draft base or is unavailable. No live data is mapped onto the draft.";
    return unchanged ? "Snapshot matches the draft base identity. Observations still describe only the server snapshot."
      : "Unsaved draft differs from its live baseline. Observations describe only the server snapshot, not this draft.";
  }

  function stationLines(station) {
    if (!station) return ["Station data unavailable."];
    const number = (value) => count(value) ? String(value) : "Unavailable";
    const occupancy = station.occupancy;
    const lines = [occupancy ? `Berths: ${number(occupancy.occupied)} occupied, ${number(occupancy.reservedEmpty)} reserved-empty, ${number(occupancy.free)} free; capacity ${number(occupancy.capacity)}.` : "Berth occupancy: Unavailable.",
      `Associated pods: ${number(station.associatedPods)}. Ready (idle): ${number(station.idlePods)}. Boarding: ${number(station.boardingPods)}. Departing berth or exiting station: ${number(station.departingPods)}. Nonempty reported wait reason: ${number(station.blockedPods)}. Categories can overlap.`,
      `Pending origin requests (not started boarding): ${number(station.pendingOriginRequests)}. These are requests, not passenger counts.`,
    ];
    for (const [label, reasons] of [["Pod wait reasons", station.waitReasons], ["Pending dispatch reasons", station.dispatchReasons]]) {
      lines.push(reasons ? `${label}: ${reasons.length ? reasons.slice(0, 100).map((entry) => `${entry.reason || "No reason reported"}: ${entry.count}`).join("; ") : "none reported"}.${reasons.length > 100 ? " Only the first 100 reason groups are shown." : ""}` : `${label}: Unavailable.`);
    }
    if (!station.pods) lines.push("Pod details: Unavailable.");
    else {
      for (const pod of station.pods.slice(0, 100)) lines.push(`Pod ${pod.id}: activity ${pod.activity ?? "Unavailable"}; phase ${pod.phase ?? "Unavailable"}; wait ${pod.waitReason === "" ? "none reported" : pod.waitReason ?? "Unavailable"}; blockedBy ${pod.blockedBy === "" ? "none reported" : pod.blockedBy ?? "Unavailable"}.`);
      if (station.pods.length > 100) lines.push(`Only the first 100 of ${station.pods.length} associated pods are listed; counts include all associated pods.`);
    }
    return lines;
  }

  // Only fields actually supplied by the snapshot become counts. An absent or
  // malformed source makes its related metrics unavailable, never zero-filled.
  function observe(reply) {
    const state = reply?.frame?.state, topology = reply?.topology, sim = state?.simulation;
    const unavailable = (reason) => ({ status: "unavailable", reason, stations: [] });
    if (!sameIdentity(state, topology)) return unavailable("State and topology identity are missing or do not match.");
    if (!count(state.revision) || !count(sim?.tick)) return unavailable("Snapshot revision or simulated tick is unavailable.");
    const network = topology.network, stations = index(network?.stations), lanes = index(network?.lanes);
    if (!stations || !lanes || [...lanes.values()].some((lane) => lane.stationID !== undefined && (!text(lane.stationID) || lane.stationID && !stations.has(lane.stationID)))) return unavailable("Live topology station or lane definitions are unavailable.");
    const berthOwners = new Map();
    for (const station of stations.values()) {
      if (!list(station.berths)) return unavailable("Live topology berth definitions are unavailable.");
      for (const berth of station.berths) {
        if (!id(berth?.id) || berthOwners.has(berth.id)) return unavailable("Live topology has invalid or duplicate berth IDs.");
        berthOwners.set(berth.id, station.id);
      }
    }
    const berths = index(sim.berths);
    const vehicles = list(sim.vehicles) ? sim.vehicles : null;
    const pods = vehicles?.map((vehicle) => vehicle?.pod);
    const podMap = index(pods);
    const associationsValid = podMap && pods.every((pod) => text(pod.stationID) && text(pod.berthID) && text(pod.laneID) && (pod.maneuverStationID === undefined || text(pod.maneuverStationID)) &&
      (!pod.maneuverStationID || stations.has(pod.maneuverStationID)) && (!pod.berthID || berthOwners.has(pod.berthID)) && (!pod.laneID || lanes.has(pod.laneID)) && (!pod.stationID || stations.has(pod.stationID)));
    const requestIDs = new Set();
    let pending = null;
    try {
      // The HTTP codec writes a nil pending slice as explicit null.
      // Missing and malformed fields remain unavailable, unlike an empty queue.
      if (sim.pending === null || list(sim.pending)) {
        const decoded = Array.from(sim.pending ?? [], unpackPendingRequest);
        if (decoded.every((request) => !requestIDs.has(request.id) && requestIDs.add(request.id) && stations.has(request.from) && stations.has(request.to) &&
          (!request.legFrom || stations.has(request.legFrom)) && (!request.podID || podMap?.has(request.podID)))) pending = decoded;
      }
    } catch (_) { /* Invalid packed fields make pending observations unavailable. */ }
    const podsByStation = new Map(), pendingByStation = new Map();
    if (associationsValid) for (const pod of pods) {
      const owner = pod.maneuverStationID || berthOwners.get(pod.berthID) || lanes.get(pod.laneID)?.stationID || pod.stationID;
      if (!podsByStation.has(owner)) podsByStation.set(owner, []);
      podsByStation.get(owner).push(pod);
    }
    if (pending) for (const request of pending) {
      const owner = request.legFrom || request.from;
      if (!pendingByStation.has(owner)) pendingByStation.set(owner, []);
      pendingByStation.get(owner).push(request);
    }
    function frequencies(items, key, includeEmpty) {
      const frequencies = new Map();
      for (const item of items) if (includeEmpty || item[key]) frequencies.set(item[key], (frequencies.get(item[key]) || 0) + 1);
      return [...frequencies].sort(([a], [b]) => a < b ? -1 : a > b ? 1 : 0).map(([reason, count]) => ({ reason, count }));
    }
    const stationReports = [];
    for (const station of stations.values()) {
      const resources = station.berths.map((berth) => berths?.get(berth.id));
      const occupancyKnown = Boolean(berths) && resources.every((berth) => berth && text(berth.occupant) && text(berth.reservedBy));
      const occupancy = occupancyKnown ? { occupied: 0, reservedEmpty: 0, free: 0, capacity: resources.length } : null;
      if (occupancy) for (const berth of resources) occupancy[berth.occupant ? "occupied" : berth.reservedBy ? "reservedEmpty" : "free"]++;
      const associated = associationsValid ? podsByStation.get(station.id) || [] : null;
      const activitiesKnown = associated?.every((pod) => text(pod.activity));
      const waitsKnown = associated?.every((pod) => text(pod.waitReason));
      const phasesKnown = associated?.every((pod) => text(pod.stationPhase));
      const reasons = waitsKnown ? frequencies(associated, "waitReason", false) : null;
      const requests = pending ? pendingByStation.get(station.id) || [] : null;
      const dispatchKnown = requests?.every((request) => text(request.dispatchReason));
      stationReports.push({ id: station.id, name: text(station.name) ? station.name : station.id, occupancy,
        associatedPods: associated ? associated.length : null,
        idlePods: activitiesKnown ? associated.filter((pod) => pod.activity === "Ready").length : null,
        boardingPods: activitiesKnown ? associated.filter((pod) => pod.activity === "Boarding").length : null,
        departingPods: phasesKnown ? associated.filter((pod) => ["Departing berth", "Exiting station"].includes(pod.stationPhase)).length : null,
        blockedPods: waitsKnown ? associated.filter((pod) => pod.waitReason !== "").length : null,
        waitReasons: reasons,
        pods: associated ? associated.map((pod) => ({ id: pod.id, activity: text(pod.activity) ? pod.activity : null,
          phase: pod.stationPhase === undefined ? "Not reported" : text(pod.stationPhase) ? pod.stationPhase : null,
          waitReason: text(pod.waitReason) ? pod.waitReason : null, blockedBy: text(pod.blockedBy) ? pod.blockedBy : null })) : null,
        pendingOriginRequests: requests ? requests.length : null,
        dispatchReasons: dispatchKnown ? frequencies(requests, "dispatchReason", true) : null,
      });
    }
    return { status: "snapshot", identity: identity(state), revision: state.revision, tick: sim.tick, stations: stationReports,
      scope: "Manual live server snapshot, not streaming. May now be stale. Counts are observations, not causal bottleneck findings. Runtime throughput, p99 delay, and future capacity are NOT MEASURED.",
      association: "maneuverStationID, else berth owner, else station-owned lane, else reported stationID; association does not prove physical presence in a station footprint." };
  }

  // Cancellation invalidates publication immediately but retains the in-flight
  // slot until read settles, even when a transport ignores AbortSignal.
  function createManualReader({ read, onChange = () => {}, controllerFactory = () => new AbortController() }) {
    let serial = 0, running = null;
    let view = { busy: false, status: "not-read", result: null, error: "" };
    function publish(next) { view = next; onChange(view); }
    return {
      get view() { return view; },
      async refresh() {
        if (running) return false;
        const ticket = { serial: ++serial, controller: controllerFactory() }; running = ticket;
        publish({ busy: true, status: "loading", result: null, error: "" });
        try {
          const result = await read(ticket.controller.signal);
          if (ticket.serial === serial) publish({ busy: true, status: "snapshot", result, error: "" });
        } catch (error) {
          if (ticket.serial === serial) publish({ busy: true, status: "unavailable", result: null, error: error instanceof Error ? error.message : "Snapshot unavailable." });
        } finally {
          if (running === ticket) running = null;
          publish({ ...view, busy: false });
        }
        return true;
      },
      cancel() {
        serial++;
        if (running) running.controller.abort();
        publish({ busy: Boolean(running), status: "cancelled", result: null, error: "Canceled. No current snapshot is displayed." });
      },
    };
  }
  const api = { MAX_ITEMS, MAX_COORDINATE, ORDER_ID_BYTES, ORDER_TEXT_BYTES, unpackOrderText, unpackPendingRequest, measurements, identity, sameIdentity, draftScope, stationLines, observe, createManualReader };
  if (typeof module !== "undefined" && module.exports) module.exports = api;
  else root.PodsimStationObservations = Object.freeze(api);
})(typeof globalThis !== "undefined" ? globalThis : this);
