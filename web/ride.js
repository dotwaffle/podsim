"use strict";

// The first-person ride page. See docs/design/first-person-ride.md.
// The pure functions at the top have node tests in ride_test.cjs. start()
// needs a browser, Babylon.js, and the ride WebAssembly module.
(function (root) {
  const POSE_FIELDS = 6;
  // The top of the guideway beam, in meters above the ground.
  const BEAM_TOP = 5;
  const BEAM_WIDTH = 1.4;
  const BEAM_DEPTH = 0.7;
  const PILLAR_SPACING = 60;
  // A beam with its bottom lower than this has no pillars.
  const PILLAR_MIN_HEIGHT = 1;
  // A straight ramp has a point at least every LEVEL_STEP meters.
  const LEVEL_STEP = 8;
  // The deck of a hub is a little lower than a beam top, so a station beam
  // that crosses it does not fight it in the depth buffer.
  const HUB_DECK_DROP = 0.05;
  // A hub is a group of mainline lanes that cross each other with no shared
  // node, with at least HUB_CROSSINGS crossings and HUB_LANES lanes. A lane
  // longer than HUB_LANE_LIMIT passes a hub, so it is not part of one.
  const HUB_CROSSINGS = 10;
  const HUB_LANES = 4;
  const HUB_LANE_LIMIT = 400;
  const HUB_CELL = 100;
  const MAX_PILLARS = 60000;
  // A curved lane gets one sample at most every CURVE_STEP meters.
  const CURVE_STEP = 4;
  const MAX_CURVE_SAMPLES = 33;
  const LANES_PER_CHUNK = 2000;
  // The pod body: height and width, in meters. The length comes from the class.
  const POD_HEIGHT = 2.1;
  const POD_WIDTH = 1.5;
  // The eye of the seat camera, above the top of the beam.
  const EYE_HEIGHT = 1.35;
  // Time constants of the camera turns, in seconds. A longer value makes a
  // sharp lane join turn the view more slowly.
  const SEAT_YAW_SECONDS = 0.35;
  const CHASE_YAW_SECONDS = 0.6;
  // The other pods turn at a lane join with this time constant.
  const POD_YAW_SECONDS = 0.2;
  // Mouse look returns to forward with this time constant after release.
  const LOOK_RETURN_SECONDS = 0.3;
  const LOOK_RADIANS_PER_PIXEL = 0.004;
  const LOOK_PITCH_LIMIT = 0.9;
  const SEAT_PITCH = 0.06;
  const CHASE_PITCH = 0.3;
  const CHASE_DISTANCE = 14;
  const CHASE_HEIGHT = 5;
  const HUD_INTERVAL_MS = 250;
  const RADIUS = 6371000;
  const DEGREE = Math.PI / 180;
  const MAX_TILES_PER_RING = 64;
  // New tiles to request in each update, so a jump does not start a burst.
  // The rings ask in the order of TILE_RINGS, so the near tiles come first.
  const TILE_REQUESTS_PER_UPDATE = 12;
  // The camera sees the ground at a low angle, so the tiles need anisotropic
  // filtering to stay sharp.
  const TILE_ANISOTROPY = 16;
  const TILE_UPDATE_MS = 1000;
  // Station signs: a fixed pool, given to the nearest stations.
  const SIGN_COUNT = 12;
  const SIGN_RADIUS = 900;
  const SHADOW_FRUSTUM = 260;
  // A 4,096-pixel shadow map over the frustum gives texels of about 6 cm.
  const SHADOW_MAP_SIZE = 4096;
  // The mini-map shows the network 1.5 km around the ridden pod, with north
  // up.
  const MINIMAP_SIZE = 240;
  const MINIMAP_RADIUS = 1500;
  const MINIMAP_CELL = 500;
  const MINIMAP_INTERVAL_MS = 100;
  const MINIMAP_LABEL_LENGTH = 14;
  const MINIMAP_LABELS = 16;
  // Auto mode fades to black, moves to the next pod, and fades back.
  const FADE_MS = 400;
  // The platform camera stands beside the berths of a station, above the
  // platform, and looks at their center.
  const PLATFORM_SIDE = 22;
  const PLATFORM_BACK = 10;
  const PLATFORM_EYE = 4;
  // After the party arrives, the platform camera shows the destination for
  // this time before the trip menu opens.
  const ARRIVAL_MS = 10000;
  // Ground layers draw first, in this order, and write no depth. Nothing in
  // the scene is below the ground, so the layers cannot fight in the depth
  // buffer, and every other mesh draws over them.
  const GROUND_LAYERS = { sky: 1, ground: 2, farTiles: 3, midTiles: 4, nearTiles: 5 };

  // groundLayerOrder sorts the opaque submeshes of a rendering group: the
  // ground layers in their order, then the other meshes in Babylon's order.
  function groundLayerOrder(a, b) {
    const layer = (subMesh) => subMesh.getMesh().metadata?.groundLayer ?? Infinity;
    // Two meshes that are not ground layers give Infinity - Infinity.
    const difference = layer(a) - layer(b);
    return Number.isNaN(difference) ? 0 : difference;
  }

  // asGroundLayer sets the ground layer of a mesh and its material.
  function asGroundLayer(mesh, layer) {
    mesh.metadata = { ...mesh.metadata, groundLayer: layer };
    mesh.material.disableDepthWrite = true;
  }
  const FOG_START = 600;
  const FOG_END = 3000;
  // Map tiles: rings around the ridden pod, from detailed near the pod to
  // coarse at the fog. A zoom 18 tile pixel is about 0.37 m at London.
  const TILE_RINGS = [
    { zoom: 18, radius: 300, layer: "nearTiles" },
    { zoom: 16, radius: 1200, layer: "midTiles" },
    { zoom: 14, radius: FOG_END, layer: "farTiles" },
  ];
  // The sky color at the horizon, which the fog also uses, and at the top.
  const SKY = [0.74, 0.82, 0.9];
  const SKY_TOP = [0.25, 0.45, 0.78];
  const CLASS_COLORS = [[0.86, 0.88, 0.9], [0.32, 0.62, 0.9], [0.95, 0.66, 0.22], [0.72, 0.36, 0.86]];
  const MODULE_GZIP_LIMIT = 16 * 1024 * 1024;
  const MODULE_LIMIT = 64 * 1024 * 1024;

  // toScene maps a world point to the scene ground plane. World y points
  // south, and the scene z points north.
  function toScene(x, y) { return [x, -y]; }

  // yawOf returns the Babylon rotation about the y axis that turns the
  // forward axis (+z) to the world heading. The heading is in radians, from
  // world +x toward world +y (south).
  function yawOf(heading) { return Math.atan2(Math.cos(heading), -Math.sin(heading)); }

  // wrapAngle returns the angle in the range (-pi, pi].
  function wrapAngle(angle) {
    let wrapped = angle % (2 * Math.PI);
    if (wrapped <= -Math.PI) wrapped += 2 * Math.PI;
    if (wrapped > Math.PI) wrapped -= 2 * Math.PI;
    return wrapped;
  }

  // newTurn returns the state of a smoothed angle with no value yet.
  function newTurn() { return { angle: NaN, rate: 0 }; }

  // dampAngle moves turn.angle toward target along the shorter way, as a
  // critically damped spring, and returns the new angle. A turn starts and
  // ends with a low rate. On a steady turn of the target, the angle lags by
  // tau seconds. The step is exact for a constant target, so the result does
  // not depend on the frame rate. A turn with no angle takes the target.
  function dampAngle(turn, target, seconds, tau) {
    if (!Number.isFinite(turn.angle) || !(tau > 0)) {
      turn.angle = target;
      turn.rate = 0;
      return target;
    }
    const omega = 2 / tau, t = Math.max(0, seconds), decay = Math.exp(-omega * t);
    const offset = wrapAngle(turn.angle - target), push = turn.rate + omega * offset;
    turn.angle = wrapAngle(target + (offset + push * t) * decay);
    turn.rate = (turn.rate - omega * push * t) * decay;
    return turn.angle;
  }

  // laneSamples returns the world points along a lane: the two end points of
  // a straight lane, or samples of a quadratic Bezier curve.
  function laneSamples(from, to, control) {
    if (!control) return [from, to];
    const approx = Math.hypot(control.x - from.x, control.y - from.y) + Math.hypot(to.x - control.x, to.y - control.y);
    const count = Math.max(3, Math.min(MAX_CURVE_SAMPLES, Math.ceil(approx / CURVE_STEP) + 1));
    const points = [];
    for (let i = 0; i < count; i++) {
      const t = i / (count - 1), u = 1 - t;
      points.push({ x: u * u * from.x + 2 * u * t * control.x + t * t * to.x, y: u * u * from.y + 2 * u * t * control.y + t * t * to.y });
    }
    return points;
  }

  // levelHeight returns the beam top at distance meters from the mainline,
  // with the levels of the topology. It is Levels.height of internal/ride.
  function levelHeight(distance, levels) {
    if (!levels) return BEAM_TOP;
    const f = Math.min(Math.max(distance / levels.rampMeters, 0), 1);
    return levels.mainTop - (levels.mainTop - levels.stationTop) * f * f * (3 - 2 * f);
  }

  // laneProfile returns the world points along a lane, each with the beam
  // top h, or null when a node is missing. The height of a point uses the
  // fraction of the lane length before it, as the pod heights of
  // ride.frame do. A straight lane with two node heights gets more points,
  // so the beam follows the ramp curve.
  function laneProfile(lane, nodes, levels) {
    const from = nodes.get(lane.from), to = nodes.get(lane.to);
    if (!from || !to) return null;
    const a = lane.fromDistance || 0, b = lane.toDistance || 0;
    let points = laneSamples(from, to, lane.control);
    if (!lane.control && levelHeight(a, levels) !== levelHeight(b, levels)) {
      const count = Math.max(2, Math.ceil(Math.hypot(to.x - from.x, to.y - from.y) / LEVEL_STEP) + 1);
      points = Array.from({ length: count }, (_, i) => ({ x: from.x + (to.x - from.x) * i / (count - 1), y: from.y + (to.y - from.y) * i / (count - 1) }));
    }
    const lengths = [0];
    for (let i = 1; i < points.length; i++) lengths.push(lengths[i - 1] + Math.hypot(points[i].x - points[i - 1].x, points[i].y - points[i - 1].y));
    const total = lengths[lengths.length - 1];
    return points.map((point, i) => ({ x: point.x, y: point.y, h: levelHeight(a + (b - a) * (total > 0 ? lengths[i] / total : 0), levels) }));
  }

  // uniqueLanes returns the lanes without a reverse twin of an earlier lane:
  // a lane on the same two nodes with the same control point.
  function uniqueLanes(lanes) {
    const seen = new Set();
    return lanes.filter((lane) => {
      const ends = lane.from < lane.to ? `${lane.from}\u0000${lane.to}` : `${lane.to}\u0000${lane.from}`;
      const key = lane.control ? `${ends}\u0000${lane.control.x}\u0000${lane.control.y}` : ends;
      if (seen.has(key)) return false;
      seen.add(key);
      return true;
    });
  }

  // segmentsCross reports whether the segments a-b and c-d cross at a point
  // inside both of them.
  function segmentsCross(a, b, c, d) {
    const rx = b.x - a.x, ry = b.y - a.y, sx = d.x - c.x, sy = d.y - c.y, den = rx * sy - ry * sx;
    if (den === 0) return false;
    const u = ((c.x - a.x) * sy - (c.y - a.y) * sx) / den, v = ((c.x - a.x) * ry - (c.y - a.y) * rx) / den;
    return u > 0 && u < 1 && v > 0 && v < 1;
  }

  // convexHull returns the convex hull of points, counterclockwise in world
  // coordinates, with no repeated point.
  function convexHull(points) {
    const sorted = [...points].sort((a, b) => a.x - b.x || a.y - b.y);
    const cross = (o, a, b) => (a.x - o.x) * (b.y - o.y) - (a.y - o.y) * (b.x - o.x);
    const half = (list) => {
      const out = [];
      for (const point of list) {
        while (out.length >= 2 && cross(out[out.length - 2], out[out.length - 1], point) <= 0) out.pop();
        out.push(point);
      }
      out.pop();
      return out;
    };
    if (sorted.length < 3) return sorted;
    return [...half(sorted), ...half(sorted.reverse())];
  }

  // findHubs returns the hubs of the mainline lanes: groups of lanes that
  // cross each other with no shared node. Each hub has the IDs of its lanes
  // and the convex hull of their end points. The simulation does not control
  // such crossings, so a pod passes through a pod on a crossing lane.
  function findHubs(lanes, nodes) {
    const segments = [], buckets = new Map();
    const candidates = lanes.filter((lane) => !lane.stationID && nodes.has(lane.from) && nodes.has(lane.to));
    candidates.forEach((lane, index) => {
      const points = laneSamples(nodes.get(lane.from), nodes.get(lane.to), lane.control);
      let length = 0;
      for (let i = 1; i < points.length; i++) length += Math.hypot(points[i].x - points[i - 1].x, points[i].y - points[i - 1].y);
      if (length > HUB_LANE_LIMIT) return;
      for (let i = 1; i < points.length; i++) {
        const a = points[i - 1], b = points[i], segment = segments.length;
        segments.push({ index, a, b });
        for (let cx = Math.floor(Math.min(a.x, b.x) / HUB_CELL); cx <= Math.floor(Math.max(a.x, b.x) / HUB_CELL); cx++) {
          for (let cy = Math.floor(Math.min(a.y, b.y) / HUB_CELL); cy <= Math.floor(Math.max(a.y, b.y) / HUB_CELL); cy++) {
            const key = `${cx},${cy}`;
            if (!buckets.has(key)) buckets.set(key, []);
            buckets.get(key).push(segment);
          }
        }
      }
    });
    // parent is a union-find forest over the candidate lanes.
    const parent = candidates.map((_, i) => i);
    const find = (i) => { while (parent[i] !== i) { parent[i] = parent[parent[i]]; i = parent[i]; } return i; };
    const crossings = new Map(), seen = new Set();
    for (const list of buckets.values()) {
      for (let i = 0; i < list.length; i++) {
        for (let j = i + 1; j < list.length; j++) {
          const p = segments[list[i]], q = segments[list[j]];
          if (p.index === q.index) continue;
          const a = candidates[p.index], b = candidates[q.index];
          if (a.from === b.from || a.from === b.to || a.to === b.from || a.to === b.to) continue;
          const pair = list[i] < list[j] ? `${list[i]},${list[j]}` : `${list[j]},${list[i]}`;
          if (seen.has(pair) || !segmentsCross(p.a, p.b, q.a, q.b)) continue;
          seen.add(pair);
          parent[find(p.index)] = find(q.index);
          crossings.set(p.index, (crossings.get(p.index) || 0) + 1);
        }
      }
    }
    const groups = new Map();
    for (const index of crossings.keys()) {
      const root = find(index);
      if (!groups.has(root)) groups.set(root, { lanes: new Set(), crossings: 0 });
    }
    candidates.forEach((lane, index) => {
      const group = groups.get(find(index));
      if (group) group.lanes.add(index);
    });
    for (const [index, count] of crossings) groups.get(find(index)).crossings += count;
    const hubs = [];
    for (const group of groups.values()) {
      if (group.crossings < HUB_CROSSINGS || group.lanes.size < HUB_LANES) continue;
      const members = [...group.lanes].map((index) => candidates[index]);
      const ends = members.flatMap((lane) => [nodes.get(lane.from), nodes.get(lane.to)]);
      hubs.push({ laneIDs: new Set(members.map((lane) => lane.id)), hull: convexHull(ends), crossings: group.crossings });
    }
    return hubs;
  }

  // deckGeometry returns the vertex data of a flat deck over the convex
  // polygon hull of world points, with its top at top and the depth
  // BEAM_DEPTH. It has a top face and side walls, with vertex colors as the
  // beams have.
  function deckGeometry(hull, top) {
    const positions = [], normals = [], colors = [], indices = [];
    const add = (point, height, normal, shade) => {
      const [x, z] = toScene(point.x, point.y);
      positions.push(x, height, z);
      normals.push(...normal);
      colors.push(shade, shade, shade * 0.97, 1);
      return positions.length / 3 - 1;
    };
    if (hull.length < 3) return { positions: new Float32Array(), normals: new Float32Array(), colors: new Float32Array(), indices: new Uint32Array() };
    // The beam material draws both sides, so the winding does not matter.
    const first = add(hull[0], top, [0, 1, 0], 0.42);
    const rim = hull.slice(1).map((point) => add(point, top, [0, 1, 0], 0.42));
    for (let i = 0; i + 1 < rim.length; i++) indices.push(first, rim[i], rim[i + 1]);
    for (let i = 0; i < hull.length; i++) {
      const a = hull[i], b = hull[(i + 1) % hull.length];
      const [ax, az] = toScene(a.x, a.y), [bx, bz] = toScene(b.x, b.y), length = Math.hypot(bx - ax, bz - az) || 1;
      // The hull is counterclockwise in world coordinates, so this normal
      // points out of the deck.
      const normal = [-(bz - az) / length, 0, (bx - ax) / length];
      const v = [add(a, top, normal, 0.82), add(b, top, normal, 0.82), add(a, top - BEAM_DEPTH, normal, 0.82), add(b, top - BEAM_DEPTH, normal, 0.82)];
      indices.push(v[0], v[2], v[1], v[1], v[2], v[3]);
    }
    return { positions: new Float32Array(positions), normals: new Float32Array(normals), colors: new Float32Array(colors), indices: new Uint32Array(indices) };
  }

  // beamGeometry returns the vertex data of a box beam along each lane. Each
  // face has its own vertices and normals, so the faces have flat shading.
  function beamGeometry(lanes, nodes, levels) {
    const positions = [], normals = [], colors = [], indices = [];
    const half = BEAM_WIDTH / 2;
    for (const lane of lanes) {
      const profile = laneProfile(lane, nodes, levels);
      if (!profile) continue;
      const points = profile.map((point) => toScene(point.x, point.y));
      const sides = [];
      for (let i = 0; i < points.length; i++) {
        const previous = points[Math.max(0, i - 1)], next = points[Math.min(points.length - 1, i + 1)];
        let dx = next[0] - previous[0], dz = next[1] - previous[1];
        const length = Math.hypot(dx, dz);
        if (length === 0) { dx = 1; dz = 0; } else { dx /= length; dz /= length; }
        // left is the horizontal unit vector to the left of travel.
        sides.push([-dz, dx]);
      }
      // Each face: two edges, given as [side sign, depth below the top],
      // and a normal. The running surface on top is darker than the
      // concrete sides.
      const top = 0, bottom = BEAM_DEPTH;
      const faces = [
        { edges: [[1, top], [-1, top]], normal: () => [0, 1, 0], shade: 0.42 },
        { edges: [[-1, top], [-1, bottom]], normal: (left) => [-left[0], 0, -left[1]], shade: 0.82 },
        { edges: [[-1, bottom], [1, bottom]], normal: () => [0, -1, 0], shade: 0.6 },
        { edges: [[1, bottom], [1, top]], normal: (left) => [left[0], 0, left[1]], shade: 0.82 },
      ];
      for (const face of faces) {
        const base = positions.length / 3;
        for (let i = 0; i < points.length; i++) {
          const left = sides[i], normal = face.normal(left);
          for (const [sign, depth] of face.edges) {
            positions.push(points[i][0] + left[0] * half * sign, profile[i].h - depth, points[i][1] + left[1] * half * sign);
            normals.push(normal[0], normal[1], normal[2]);
            colors.push(face.shade, face.shade, face.shade * 0.97, 1);
          }
        }
        for (let i = 0; i + 1 < points.length; i++) {
          const a = base + 2 * i;
          indices.push(a, a + 1, a + 2, a + 1, a + 3, a + 2);
        }
      }
    }
    return { positions: new Float32Array(positions), normals: new Float32Array(normals), colors: new Float32Array(colors), indices: new Uint32Array(indices) };
  }

  // pillarPoints returns scene points [x, z, top] under the lanes, about
  // one for each PILLAR_SPACING meters of lane, at most MAX_PILLARS. top is
  // the beam top. A point where the beam bottom is lower than
  // PILLAR_MIN_HEIGHT has no pillar.
  function pillarPoints(lanes, nodes, levels) {
    const result = [];
    for (const lane of lanes) {
      const points = laneProfile(lane, nodes, levels);
      if (!points) continue;
      let carried = PILLAR_SPACING / 2;
      for (let i = 0; i + 1 < points.length; i++) {
        const a = points[i], b = points[i + 1];
        const length = Math.hypot(b.x - a.x, b.y - a.y);
        let at = carried;
        while (at < length) {
          if (result.length >= MAX_PILLARS) return result;
          const f = at / length, top = a.h + (b.h - a.h) * f;
          if (top - BEAM_DEPTH >= PILLAR_MIN_HEIGHT) result.push([...toScene(a.x + (b.x - a.x) * f, a.y + (b.y - a.y) * f), top]);
          at += PILLAR_SPACING;
        }
        carried = at - length;
      }
    }
    return result;
  }

  // stationSlabs returns a platform rectangle for each station with berths:
  // the scene center, the width along x, and the depth along z.
  function stationSlabs(stations, margin = 6) {
    const slabs = [];
    for (const station of stations) {
      if (!station.berths || station.berths.length === 0) continue;
      let minX = Infinity, maxX = -Infinity, minZ = Infinity, maxZ = -Infinity;
      for (const berth of station.berths) {
        const [x, z] = toScene(berth.x, berth.y);
        minX = Math.min(minX, x); maxX = Math.max(maxX, x); minZ = Math.min(minZ, z); maxZ = Math.max(maxZ, z);
      }
      slabs.push({ id: station.id, name: station.name, height: station.height ?? BEAM_TOP, x: (minX + maxX) / 2, z: (minZ + maxZ) / 2, width: maxX - minX + 2 * margin, depth: maxZ - minZ + 2 * margin });
    }
    return slabs;
  }

  // sceneBounds returns the scene rectangle of the nodes.
  function sceneBounds(nodes) {
    let minX = Infinity, maxX = -Infinity, minZ = Infinity, maxZ = -Infinity;
    for (const node of nodes) {
      const [x, z] = toScene(node.position.x, node.position.y);
      minX = Math.min(minX, x); maxX = Math.max(maxX, x); minZ = Math.min(minZ, z); maxZ = Math.max(maxZ, z);
    }
    if (!Number.isFinite(minX)) return { minX: 0, maxX: 0, minZ: 0, maxZ: 0 };
    return { minX, maxX, minZ, maxZ };
  }

  // writePodMatrix writes the Babylon world matrix of a pod at scene (x, z)
  // with yaw into out at offset. Babylon matrices put the translation in
  // elements 12 to 14.
  function writePodMatrix(out, offset, x, top, z, yaw) {
    const c = Math.cos(yaw), s = Math.sin(yaw);
    out.set([c, 0, -s, 0, 0, 1, 0, 0, s, 0, c, 0, x, top + POD_HEIGHT / 2, z, 1], offset);
  }

  // choosePod returns the requested pod ID when the list has it, else the
  // first pod with riders, else the first pod.
  function choosePod(pods, requested) {
    if (pods.some((pod) => pod.id === requested)) return requested;
    const busy = pods.find((pod) => pod.riders > 0);
    return busy ? busy.id : (pods[0] ? pods[0].id : "");
  }

  function podLabel(pod) {
    const trip = pod.riders > 0 ? ` ${pod.from} to ${pod.to}` : "";
    return `${pod.id} (${pod.class}) ${pod.activity}${trip}`;
  }

  // worldToGeo returns the latitude and longitude in degrees of world point
  // (x, y) in meters. It is the inverse of the equirectangular projection of
  // the project geography. World y points south.
  function worldToGeo(geo, x, y) {
    return { lat: geo.latitude - y / RADIUS / DEGREE, lon: geo.longitude + x / (RADIUS * Math.cos(geo.latitude * DEGREE)) / DEGREE };
  }

  function geoToWorld(geo, lat, lon) {
    return { x: (lon - geo.longitude) * DEGREE * RADIUS * Math.cos(geo.latitude * DEGREE), y: (geo.latitude - lat) * DEGREE * RADIUS };
  }

  const tileXOf = (lon, count) => (lon + 180) / 360 * count;
  const tileYOf = (lat, count) => (1 - Math.asinh(Math.tan(Math.max(-80, Math.min(80, lat)) * DEGREE)) / Math.PI) / 2 * count;
  const tileLon = (x, count) => x / count * 360 - 180;
  const tileLat = (y, count) => Math.atan(Math.sinh(Math.PI * (1 - 2 * y / count))) / DEGREE;

  // groundTiles returns the map tiles at zoom within radius meters of world
  // point (x, y), nearest first, at most max. Each tile has its world
  // rectangle. The URL key wraps tile x across the date line.
  function groundTiles(geo, x, y, zoom, radius, max) {
    const count = 2 ** zoom;
    const northWest = worldToGeo(geo, x - radius, y - radius), southEast = worldToGeo(geo, x + radius, y + radius);
    const minX = Math.floor(tileXOf(northWest.lon, count)), maxX = Math.floor(tileXOf(southEast.lon, count));
    const minY = Math.max(0, Math.floor(tileYOf(northWest.lat, count))), maxY = Math.min(count - 1, Math.floor(tileYOf(southEast.lat, count)));
    const tiles = [];
    for (let ty = minY; ty <= maxY; ty++) {
      for (let tx = minX; tx <= maxX; tx++) {
        const west = geoToWorld(geo, tileLat(ty, count), tileLon(tx, count)), east = geoToWorld(geo, tileLat(ty + 1, count), tileLon(tx + 1, count));
        const rect = { minX: west.x, maxX: east.x, minY: west.y, maxY: east.y };
        const wrapped = ((tx % count) + count) % count;
        const distance = Math.hypot(Math.max(rect.minX - x, 0, x - rect.maxX), Math.max(rect.minY - y, 0, y - rect.maxY));
        if (distance <= radius) tiles.push({ key: `${zoom}/${wrapped}/${ty}`, z: zoom, x: wrapped, y: ty, rect, distance });
      }
    }
    tiles.sort((a, b) => a.distance - b.distance || a.key.localeCompare(b.key));
    return tiles.slice(0, max);
  }

  // tileQuad returns the vertex data of a flat map tile at height. The north
  // edge of the tile has v = 1, which is the top row of the image when the
  // texture inverts y, as Babylon does by default.
  function tileQuad(rect, height) {
    const [west, north] = toScene(rect.minX, rect.minY), [east, south] = toScene(rect.maxX, rect.maxY);
    return {
      positions: new Float32Array([west, height, south, east, height, south, east, height, north, west, height, north]),
      normals: new Float32Array([0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0]),
      uvs: new Float32Array([0, 0, 1, 0, 1, 1, 0, 1]),
      indices: new Uint32Array([0, 2, 1, 0, 3, 2]),
    };
  }

  // nearestSlabs returns the station slabs within radius of scene point
  // (x, z), nearest first, at most count.
  function nearestSlabs(slabs, x, z, count, radius) {
    return slabs.map((slab) => ({ slab, distance: Math.hypot(slab.x - x, slab.z - z) }))
      .filter((entry) => entry.distance <= radius)
      .sort((a, b) => a.distance - b.distance || a.slab.id.localeCompare(b.slab.id))
      .slice(0, count).map((entry) => entry.slab);
  }

  // springBack moves a look offset toward zero with time constant tau.
  function springBack(offset, seconds, tau) {
    return tau > 0 ? offset * Math.exp(-Math.max(0, seconds) / tau) : 0;
  }

  // chaseEye returns the scene position of the chase camera behind a pod at
  // world (x, y). yaw is the Babylon yaw of the camera.
  function chaseEye(x, y, yaw, top = BEAM_TOP) {
    const [sx, sz] = toScene(x, y);
    return [sx - Math.sin(yaw) * CHASE_DISTANCE, top + POD_HEIGHT + CHASE_HEIGHT, sz - Math.cos(yaw) * CHASE_DISTANCE];
  }

  // smoothPodYaws updates yaws, a map from pod ID to the turn of the drawn
  // yaw, toward the heading of each pod in poses.
  function smoothPodYaws(yaws, ids, poses, count, seconds, tau) {
    for (let i = 0; i < count && i < ids.length; i++) {
      if (!yaws.has(ids[i])) yaws.set(ids[i], newTurn());
      dampAngle(yaws.get(ids[i]), yawOf(poses[i * POSE_FIELDS + 2]), seconds, tau);
    }
    return yaws;
  }

  // segmentIndex returns the straight segments of the lanes in a grid of
  // square cells, so that a draw reads only the segments near a point. A
  // segment is in each cell of its bounding box.
  function segmentIndex(lanes, nodes, cell) {
    const coords = [], buckets = new Map();
    for (const lane of lanes) {
      const from = nodes.get(lane.from), to = nodes.get(lane.to);
      if (!from || !to) continue;
      const points = laneSamples(from, to, lane.control);
      for (let i = 1; i < points.length; i++) {
        const a = points[i - 1], b = points[i], segment = coords.length / 4;
        coords.push(a.x, a.y, b.x, b.y);
        for (let cx = Math.floor(Math.min(a.x, b.x) / cell); cx <= Math.floor(Math.max(a.x, b.x) / cell); cx++) {
          for (let cy = Math.floor(Math.min(a.y, b.y) / cell); cy <= Math.floor(Math.max(a.y, b.y) / cell); cy++) {
            const key = `${cx},${cy}`;
            if (!buckets.has(key)) buckets.set(key, []);
            buckets.get(key).push(segment);
          }
        }
      }
    }
    return { cell, coords: Float64Array.from(coords), buckets, marks: new Uint32Array(coords.length / 4), mark: 0 };
  }

  // segmentsNear returns each segment of index in the cells that the square
  // of half side radius around world (x, y) touches, once.
  function segmentsNear(index, x, y, radius) {
    const found = [], cell = index.cell;
    index.mark++;
    for (let cx = Math.floor((x - radius) / cell); cx <= Math.floor((x + radius) / cell); cx++) {
      for (let cy = Math.floor((y - radius) / cell); cy <= Math.floor((y + radius) / cell); cy++) {
        for (const segment of index.buckets.get(`${cx},${cy}`) || []) {
          if (index.marks[segment] === index.mark) continue;
          index.marks[segment] = index.mark;
          found.push(segment);
        }
      }
    }
    return found;
  }

  // miniMapArrow returns the corners of the arrow of the ridden pod at the
  // center of the map, which has a half size of half pixels. The arrow points
  // along heading, and its tip is size pixels from the center. The map has
  // north up, so canvas y points south, as world y does.
  function miniMapArrow(heading, half, size) {
    const cos = Math.cos(heading), sin = Math.sin(heading);
    // Each corner is (forward, right) in units of size.
    return [[1, 0], [-0.7, 0.7], [-0.3, 0], [-0.7, -0.7]].map(([forward, right]) =>
      [half + size * (forward * cos - right * sin), half + size * (forward * sin + right * cos)]);
  }

  // stopPoints returns the passenger stations that have berths, each at the
  // center of its berths.
  function stopPoints(stations) {
    return stations.filter((station) => !station.parkingOnly && station.berths.length > 0).map((station) => ({
      id: station.id,
      name: station.name || station.id,
      x: station.berths.reduce((sum, berth) => sum + berth.x, 0) / station.berths.length,
      y: station.berths.reduce((sum, berth) => sum + berth.y, 0) / station.berths.length,
    }));
  }

  // shortLabel returns a station name for the mini-map: the name without a
  // part in brackets, cut at a word boundary to at most limit characters.
  function shortLabel(name, limit) {
    const plain = name.replace(/\s*\(.*?\)\s*/g, " ").trim() || name;
    if (plain.length <= limit) return plain;
    const cut = plain.slice(0, limit), space = cut.lastIndexOf(" ");
    return `${(space > 0 ? cut.slice(0, space) : cut).trim()}…`;
  }

  // placeLabels returns the labels that fit with no overlap, at most max.
  // Each label has a box (x, y, width, height). A nearer label, with a lower
  // distance, comes first. A label that overlaps an earlier label or a box
  // of blocked is not in the result.
  function placeLabels(labels, blocked, max) {
    const overlaps = (a, b) => a.x < b.x + b.width && b.x < a.x + a.width && a.y < b.y + b.height && b.y < a.y + a.height;
    const placed = [];
    for (const label of [...labels].sort((a, b) => a.distance - b.distance)) {
      if (placed.length >= max) break;
      if (blocked.some((box) => overlaps(label, box)) || placed.some((box) => overlaps(label, box))) continue;
      placed.push(label);
    }
    return placed;
  }

  // nearestStop returns the stop of stops nearest to world (x, y), with its
  // distance, or null when there are no stops. Ties go to the first stop.
  function nearestStop(stops, x, y) {
    let best = null;
    for (const stop of stops) {
      const distance = Math.hypot(stop.x - x, stop.y - y);
      if (!best || distance < best.distance) best = { id: stop.id, name: stop.name, distance };
    }
    return best;
  }

  // snapShadowCenter returns the position p of a directional light with the
  // unit direction d, moved to whole shadow texels of size texel. Babylon
  // looks from the light position along d with up +y, so the snap uses the
  // x and y axes of that view. When the light follows the camera, a snapped
  // shadow map keeps each shadow edge on the same texels, and the edges do
  // not crawl. Vectors are [x, y, z] arrays.
  function snapShadowCenter(p, d, texel) {
    const cross = (a, b) => [a[1] * b[2] - a[2] * b[1], a[2] * b[0] - a[0] * b[2], a[0] * b[1] - a[1] * b[0]];
    const dot = (a, b) => a[0] * b[0] + a[1] * b[1] + a[2] * b[2];
    let xAxis = cross([0, 1, 0], d);
    const length = Math.hypot(...xAxis);
    xAxis = xAxis.map((value) => value / length);
    const yAxis = cross(d, xAxis);
    const x = Math.round(dot(p, xAxis) / texel) * texel, y = Math.round(dot(p, yAxis) / texel) * texel, z = dot(p, d);
    return [0, 1, 2].map((i) => x * xAxis[i] + y * yAxis[i] + z * d[i]);
  }

  // platformView returns the scene positions of the eye and the target of
  // the platform camera at a station with the world berth positions berths.
  // The eye is to the side of the line through the first and the last berth.
  function platformView(berths, top = BEAM_TOP) {
    const count = berths.length || 1;
    const center = { x: berths.reduce((sum, berth) => sum + berth.x, 0) / count, y: berths.reduce((sum, berth) => sum + berth.y, 0) / count };
    let ax = 1, ay = 0;
    if (berths.length >= 2) {
      const first = berths[0], last = berths[berths.length - 1], length = Math.hypot(last.x - first.x, last.y - first.y);
      if (length > 1) { ax = (last.x - first.x) / length; ay = (last.y - first.y) / length; }
    }
    const [ex, ez] = toScene(center.x - ay * PLATFORM_SIDE - ax * PLATFORM_BACK, center.y + ax * PLATFORM_SIDE - ay * PLATFORM_BACK);
    const [tx, tz] = toScene(center.x, center.y);
    return { center, eye: [ex, top + PLATFORM_EYE, ez], target: [tx, top + 1, tz] };
  }

  // tripView returns how the page shows the passenger flow p of
  // ride.passenger(), or null when there is no trip. names maps a station ID
  // to its name. camera is "platform" at station, "seat" in pod podID, or ""
  // to keep the camera.
  function tripView(p, names) {
    if (!p || !p.phase) return null;
    const name = (id) => names.get(id) || id;
    const order = p.orderID ? `Order #${p.orderID}` : "Trip";
    const pod = p.podID ? `pod ${p.podID}` : "a pod";
    switch (p.phase) {
      case "requested":
        return { text: p.orderID ? `${order}: ${name(p.origin)} to ${name(p.destination)}` : `Sending the trip: ${name(p.origin)} to ${name(p.destination)}`, camera: "platform", station: p.station, podID: "", active: true };
      case "waiting":
        return { text: `${order}: waiting at ${name(p.station)} for ${pod}`, camera: "platform", station: p.station, podID: p.podID || "", active: true };
      case "riding":
        return { text: `${order}: riding ${pod} to ${name(p.destination)}`, camera: "seat", station: "", podID: p.podID, active: true };
      case "arrived":
        return { text: `${order}: arrived at ${name(p.station)}`, camera: "platform", station: p.station, podID: "", active: false };
      default:
        return { text: `Trip stopped: ${p.message || "unknown reason"}`, camera: "", station: "", podID: "", active: false };
    }
  }

  // autoHop reports whether auto mode moves to another pod. It moves when
  // the last rider leaves, or at once when the user turned on auto mode on
  // an empty pod. riders is -1 when the session has no ridden pod.
  function autoHop(auto, armed, previousRiders, riders) {
    return auto && riders === 0 && (armed || previousRiders > 0);
  }

  function formatSpeed(metersPerSecond) { return `${Math.round(metersPerSecond * 3.6)} km/h`; }

  function formatDistance(meters) {
    return meters < 1000 ? `${Math.round(meters)} m` : `${(meters / 1000).toFixed(1)} km`;
  }

  function formatDuration(seconds) {
    const total = Math.max(0, Math.round(seconds));
    const hours = Math.floor(total / 3600), minutes = Math.floor(total % 3600 / 60), rest = total % 60;
    const pad = (value) => String(value).padStart(2, "0");
    return hours > 0 ? `${hours}:${pad(minutes)}:${pad(rest)}` : `${minutes}:${pad(rest)}`;
  }

  // hudView returns the texts of the HUD for the data of ride.hud().
  function hudView(hud) {
    const session = hud.session || {};
    const view = {
      speed: "", state: "Pod not in the session", wait: "", trip: [], next: "", route: "", progress: -1,
      session: `Session ${session.speed}x${session.paused ? ", paused" : ""}${session.connected ? "" : `, disconnected${session.error ? `: ${session.error}` : ""}`}`,
      message: session.message || "",
      rideAt1x: session.connected === true && session.speed !== 1,
    };
    if (!hud.found) return view;
    view.speed = formatSpeed(hud.speed) + (hud.speedLimit > 0 ? `, limit ${formatSpeed(hud.speedLimit)}` : "");
    view.state = `${hud.podID} (${hud.class}): ${hud.activity}`;
    if (hud.waitReason) view.wait = `Waiting: ${hud.waitReason}${hud.blockedBy ? ` (${hud.blockedBy})` : ""}`;
    view.trip = hud.riders.map((rider) => `${rider.partySize} ${rider.partySize === 1 ? "passenger" : "passengers"}: ${rider.from} to ${rider.to}`);
    view.next = hud.nextStop ? `Next stop: ${hud.nextStop}` : (hud.riders.length === 0 ? "No passengers" : "");
    if (hud.route) {
      // The time is at the speed limits with no waits. When the route goes
      // on after the window that the stream sends, the values are the part
      // in the window. The progress is the part of the route in the window
      // that is behind the pod.
      const { traveledMeters: traveled = 0, remainingMeters: remaining } = hud.route;
      const distance = formatDistance(remaining), time = formatDuration(hud.route.seconds);
      view.route = hud.route.after ? `Route: over ${distance}, over ${time}` : `Route: ${distance}, about ${time}`;
      view.progress = traveled + remaining > 0 ? traveled / (traveled + remaining) : 1;
    }
    return view;
  }

  // bounded fails the stream when it has more than limit bytes.
  function bounded(stream, limit, message) {
    let count = 0;
    return stream.pipeThrough(new TransformStream({ transform(chunk, controller) {
      count += chunk.byteLength;
      if (count > limit) throw new Error(message);
      controller.enqueue(chunk);
    } }));
  }

  async function loadModule(stopped) {
    if (typeof DecompressionStream !== "function" || typeof WebAssembly !== "object") throw new Error("This browser needs WebAssembly and native gzip decompression.");
    const go = new root.Go();
    const response = await fetch("ride.wasm.gz");
    if (!response.ok || !response.body) throw new Error("The ride module could not be downloaded.");
    const decoded = bounded(bounded(response.body, MODULE_GZIP_LIMIT, "The ride module is too large.").pipeThrough(new DecompressionStream("gzip")), MODULE_LIMIT, "The ride module is too large.");
    const { instance } = await WebAssembly.instantiateStreaming(new Response(decoded, { headers: { "Content-Type": "application/wasm" } }), go.importObject);
    go.run(instance).then(stopped, stopped);
    if (!root.podsimRide) throw new Error("The ride module did not start.");
    return root.podsimRide;
  }

  function color(B, rgb) { return new B.Color3(rgb[0], rgb[1], rgb[2]); }

  // buildPodMesh makes one pod body for a class: a rounded shell in the
  // class color, a band of dark glass, and a running gear on the beam. The
  // parts merge into one mesh with one submesh for each material, so thin
  // instances draw a class in a few calls.
  function buildPodMesh(B, scene, cls, rgb) {
    const length = cls.lengthMeters, radius = POD_WIDTH / 2;
    const shell = B.MeshBuilder.CreateCapsule(`shell-${cls.name}`, { orientation: new B.Vector3(0, 0, 1), height: length, radius, tessellation: 20, subdivisions: 4, capSubdivisions: 6 }, scene);
    shell.scaling.y = (POD_HEIGHT - 0.25) / POD_WIDTH;
    shell.position.y = 0.12;
    shell.bakeCurrentTransformIntoVertices();
    const shellMaterial = new B.StandardMaterial(`shell-${cls.name}`, scene);
    shellMaterial.diffuseColor = color(B, rgb);
    shellMaterial.specularColor = new B.Color3(0.55, 0.55, 0.55);
    shellMaterial.specularPower = 48;
    shell.material = shellMaterial;

    const glass = B.MeshBuilder.CreateBox(`glass-${cls.name}`, { width: POD_WIDTH + 0.06, height: 0.62, depth: Math.max(1, length - 0.9) }, scene);
    glass.position.y = 0.32;
    glass.bakeCurrentTransformIntoVertices();
    const glassMaterial = new B.StandardMaterial(`glass-${cls.name}`, scene);
    glassMaterial.diffuseColor = new B.Color3(0.05, 0.08, 0.12);
    glassMaterial.specularColor = new B.Color3(0.9, 0.95, 1);
    glassMaterial.specularPower = 96;
    glass.material = glassMaterial;

    const gear = B.MeshBuilder.CreateBox(`gear-${cls.name}`, { width: BEAM_WIDTH * 0.8, height: 0.3, depth: length * 0.7 }, scene);
    gear.position.y = -POD_HEIGHT / 2 + 0.15;
    gear.bakeCurrentTransformIntoVertices();
    const gearMaterial = new B.StandardMaterial(`gear-${cls.name}`, scene);
    gearMaterial.diffuseColor = new B.Color3(0.16, 0.17, 0.19);
    gearMaterial.specularColor = new B.Color3(0.1, 0.1, 0.1);
    gear.material = gearMaterial;

    const mesh = B.Mesh.MergeMeshes([shell, glass, gear], true, true, undefined, false, true);
    mesh.name = `pod-${cls.name}`;
    mesh.isPickable = false;
    // The instances move every frame and cover the whole network.
    mesh.alwaysSelectAsActiveMesh = true;
    return mesh;
  }

  // buildWorld makes the static meshes of a topology and the pod meshes.
  // casters cast shadows, and receivers show them.
  function buildWorld(B, scene, topology) {
    const meshes = [], casters = [], receivers = [];
    const nodes = new Map(topology.nodes.map((node) => [node.id, node.position]));
    const beamMaterial = new B.StandardMaterial("beam", scene);
    beamMaterial.diffuseColor = new B.Color3(1, 1, 1);
    beamMaterial.specularColor = new B.Color3(0.08, 0.08, 0.08);
    beamMaterial.backFaceCulling = false;
    // A reverse twin draws in the same place, so it draws once. A hub draws
    // as one deck in place of the beams of its crossing lanes.
    const hubs = findHubs(topology.lanes, nodes);
    const hubLanes = new Set(hubs.flatMap((hub) => [...hub.laneIDs]));
    const lanes = uniqueLanes(topology.lanes).filter((lane) => !hubLanes.has(lane.id)), levels = topology.levels;
    for (let start = 0; start < lanes.length; start += LANES_PER_CHUNK) {
      const geometry = beamGeometry(lanes.slice(start, start + LANES_PER_CHUNK), nodes, levels);
      if (geometry.indices.length === 0) continue;
      const mesh = new B.Mesh(`beams${start}`, scene);
      const data = new B.VertexData();
      data.positions = geometry.positions; data.normals = geometry.normals; data.colors = geometry.colors; data.indices = geometry.indices;
      data.applyToMesh(mesh);
      mesh.material = beamMaterial;
      mesh.isPickable = false;
      mesh.freezeWorldMatrix();
      meshes.push(mesh); casters.push(mesh); receivers.push(mesh);
    }
    for (const [index, hub] of hubs.entries()) {
      const geometry = deckGeometry(hub.hull, BEAM_TOP - HUB_DECK_DROP);
      if (geometry.indices.length === 0) continue;
      const mesh = new B.Mesh(`hub${index}`, scene);
      const data = new B.VertexData();
      data.positions = geometry.positions; data.normals = geometry.normals; data.colors = geometry.colors; data.indices = geometry.indices;
      data.applyToMesh(mesh);
      mesh.material = beamMaterial;
      mesh.isPickable = false;
      mesh.freezeWorldMatrix();
      meshes.push(mesh); casters.push(mesh); receivers.push(mesh);
    }

    const concrete = new B.StandardMaterial("pillar", scene);
    concrete.diffuseColor = new B.Color3(0.72, 0.72, 0.7);
    concrete.specularColor = new B.Color3(0.05, 0.05, 0.05);
    const pillar = B.MeshBuilder.CreateCylinder("pillar", { height: BEAM_TOP - BEAM_DEPTH, diameterTop: 0.55, diameterBottom: 0.75, tessellation: 12 }, scene);
    pillar.material = concrete;
    pillar.isPickable = false;
    // A pillar holds each corner of a hub deck.
    const points = [...pillarPoints(lanes, nodes, levels), ...hubs.flatMap((hub) => hub.hull.map((point) => [...toScene(point.x, point.y), BEAM_TOP - HUB_DECK_DROP]))];
    const pillarMatrices = new Float32Array(points.length * 16);
    // The cylinder is as tall as a mainline pillar, so a lower beam scales it.
    points.forEach(([x, z, top], i) => {
      const height = top - BEAM_DEPTH;
      pillarMatrices.set([1, 0, 0, 0, 0, height / (BEAM_TOP - BEAM_DEPTH), 0, 0, 0, 0, 1, 0, x, height / 2, z, 1], i * 16);
    });
    if (points.length > 0) pillar.thinInstanceSetBuffer("matrix", pillarMatrices, 16, true);
    else pillar.setEnabled(false);
    meshes.push(pillar); casters.push(pillar);

    const slabMaterial = new B.StandardMaterial("platform", scene);
    slabMaterial.diffuseColor = new B.Color3(0.6, 0.62, 0.66);
    slabMaterial.specularColor = new B.Color3(0.1, 0.1, 0.1);
    const slabs = stationSlabs(topology.stations);
    for (const slab of slabs) {
      const mesh = B.MeshBuilder.CreateBox(`station-${slab.id}`, { width: slab.width, depth: slab.depth, height: 0.4 }, scene);
      mesh.position.set(slab.x, slab.height - BEAM_DEPTH - 0.25, slab.z);
      mesh.material = slabMaterial;
      mesh.isPickable = false;
      mesh.freezeWorldMatrix();
      meshes.push(mesh); receivers.push(mesh);
    }

    const bounds = sceneBounds(topology.nodes);
    const size = Math.max(bounds.maxX - bounds.minX, bounds.maxZ - bounds.minZ) + 2 * FOG_END;
    const ground = B.MeshBuilder.CreateGround("ground", { width: size, height: size }, scene);
    ground.position.set((bounds.minX + bounds.maxX) / 2, 0, (bounds.minZ + bounds.maxZ) / 2);
    const groundTexture = new B.DynamicTexture("groundTexture", { width: 256, height: 256 }, scene, true);
    const context = groundTexture.getContext();
    context.fillStyle = "#6f8a5a"; context.fillRect(0, 0, 256, 256);
    context.fillStyle = "#67814f"; context.fillRect(0, 0, 128, 128); context.fillRect(128, 128, 128, 128);
    groundTexture.update();
    groundTexture.wrapU = groundTexture.wrapV = B.Texture.WRAP_ADDRESSMODE;
    // One texture repeat covers 40 m, so the ground shows speed.
    groundTexture.uScale = groundTexture.vScale = size / 40;
    const groundMaterial = new B.StandardMaterial("ground", scene);
    groundMaterial.diffuseTexture = groundTexture;
    groundMaterial.specularColor = new B.Color3(0, 0, 0);
    ground.material = groundMaterial;
    asGroundLayer(ground, GROUND_LAYERS.ground);
    ground.isPickable = false;
    ground.freezeWorldMatrix();
    meshes.push(ground); receivers.push(ground);

    const pods = topology.classes.map((cls, index) => {
      const mesh = buildPodMesh(B, scene, cls, CLASS_COLORS[index % CLASS_COLORS.length]);
      meshes.push(mesh); casters.push(mesh);
      return { mesh, length: cls.lengthMeters, matrices: new Float32Array(0), count: 0 };
    });
    return { meshes, casters, receivers, pods, slabs };
  }

  // buildCabin makes the window frame and the dashboard of the seat. They
  // move with the camera.
  function buildCabin(B, scene, camera) {
    const material = new B.StandardMaterial("cabin", scene);
    material.diffuseColor = new B.Color3(0.12, 0.13, 0.15);
    material.specularColor = new B.Color3(0.2, 0.2, 0.2);
    const parts = [
      B.MeshBuilder.CreateBox("dashboard", { width: 1.6, height: 0.25, depth: 0.5 }, scene),
      B.MeshBuilder.CreateBox("pillarLeft", { width: 0.08, height: 1.2, depth: 0.08 }, scene),
      B.MeshBuilder.CreateBox("pillarRight", { width: 0.08, height: 1.2, depth: 0.08 }, scene),
      B.MeshBuilder.CreateBox("roof", { width: 1.6, height: 0.1, depth: 0.5 }, scene),
    ];
    const offsets = [[0, -0.55, 0.75], [-0.78, 0.0, 0.9], [0.78, 0.0, 0.9], [0, 0.6, 0.75]];
    parts.forEach((part, i) => {
      part.parent = camera;
      part.position.set(offsets[i][0], offsets[i][1], offsets[i][2]);
      part.material = material;
      part.isPickable = false;
    });
    return parts;
  }

  // buildSky makes a sky dome with a vertical gradient in vertex colors.
  function buildSky(B, scene) {
    const sky = B.MeshBuilder.CreateSphere("sky", { diameter: 2 * FOG_END, segments: 24, sideOrientation: B.Mesh.BACKSIDE }, scene);
    const positions = sky.getVerticesData(B.VertexBuffer.PositionKind);
    const colors = new Float32Array(positions.length / 3 * 4);
    for (let i = 0; i < positions.length / 3; i++) {
      // 0 at the horizon and below, 1 at the top.
      const up = Math.max(0, positions[i * 3 + 1] / FOG_END), t = Math.sqrt(up);
      for (let c = 0; c < 3; c++) colors[i * 4 + c] = SKY[c] + (SKY_TOP[c] - SKY[c]) * t;
      colors[i * 4 + 3] = 1;
    }
    sky.setVerticesData(B.VertexBuffer.ColorKind, colors);
    const material = new B.StandardMaterial("sky", scene);
    // With lighting off, the output is the emissive color times the vertex
    // color, so the emissive color is white.
    material.disableLighting = true;
    material.emissiveColor = new B.Color3(1, 1, 1);
    material.diffuseColor = new B.Color3(1, 1, 1);
    material.fogEnabled = false;
    sky.material = material;
    asGroundLayer(sky, GROUND_LAYERS.sky);
    sky.infiniteDistance = true;
    sky.isPickable = false;
    return sky;
  }

  // createTileLayer drapes map tiles on the ground around the camera when the
  // project has a geographic reference. Unlike the 2D view, it does not need
  // a map background setting, but it refuses a map setting that is not valid.
  function createTileLayer(B, scene, topology, credit) {
    const tiles = root.PodsimTiles;
    const enabled = !!(tiles && tiles.validGeo(topology.geo) && (!topology.map || tiles.validMap(topology.map, topology.geo)));
    credit.hidden = !enabled;
    const meshes = new Map();
    function update(x, y) {
      if (!enabled) return;
      const wanted = new Map();
      // The far ring lies under the near ring.
      // A nearer ring draws after a farther ring, so it covers it.
      for (const ring of TILE_RINGS) {
        for (const tile of groundTiles(topology.geo, x, y, ring.zoom, ring.radius, MAX_TILES_PER_RING)) wanted.set(tile.key, { tile, layer: GROUND_LAYERS[ring.layer] });
      }
      for (const [key, mesh] of meshes) {
        if (!wanted.has(key)) { mesh.dispose(false, true); meshes.delete(key); }
      }
      let requests = 0;
      for (const [key, { tile, layer }] of wanted) {
        if (meshes.has(key) || requests >= TILE_REQUESTS_PER_UPDATE) continue;
        requests++;
        const mesh = new B.Mesh(`tile-${key}`, scene);
        const data = new B.VertexData();
        Object.assign(data, tileQuad(tile.rect, 0));
        data.applyToMesh(mesh);
        const material = new B.StandardMaterial(`tile-${key}`, scene);
        const url = tiles.URL_TEMPLATE.replace("{z}", tile.z).replace("{x}", tile.x).replace("{y}", tile.y);
        material.diffuseTexture = new B.Texture(url, scene, false, true);
        material.diffuseTexture.wrapU = material.diffuseTexture.wrapV = B.Texture.CLAMP_ADDRESSMODE;
        material.diffuseTexture.anisotropicFilteringLevel = TILE_ANISOTROPY;
        material.specularColor = new B.Color3(0, 0, 0);
        material.backFaceCulling = false;
        mesh.material = material;
        asGroundLayer(mesh, layer);
        mesh.receiveShadows = true;
        mesh.isPickable = false;
        mesh.freezeWorldMatrix();
        meshes.set(key, mesh);
      }
    }
    function dispose() {
      for (const mesh of meshes.values()) mesh.dispose(false, true);
      meshes.clear();
    }
    return { update, dispose, enabled };
  }

  // createSigns makes a pool of station name signs. update gives them to the
  // stations nearest the camera.
  function createSigns(B, scene) {
    const signs = [];
    for (let i = 0; i < SIGN_COUNT; i++) {
      const mesh = B.MeshBuilder.CreatePlane(`sign${i}`, { width: 9, height: 1.5 }, scene);
      const texture = new B.DynamicTexture(`sign${i}`, { width: 768, height: 128 }, scene, true);
      const material = new B.StandardMaterial(`sign${i}`, scene);
      material.diffuseTexture = texture;
      material.emissiveColor = new B.Color3(0.85, 0.85, 0.85);
      material.specularColor = new B.Color3(0, 0, 0);
      material.backFaceCulling = false;
      mesh.material = material;
      mesh.billboardMode = B.Mesh.BILLBOARDMODE_Y;
      mesh.isPickable = false;
      mesh.setEnabled(false);
      signs.push({ mesh, texture, station: "" });
    }
    function update(slabs, x, z) {
      const nearest = nearestSlabs(slabs, x, z, SIGN_COUNT, SIGN_RADIUS);
      signs.forEach((sign, i) => {
        const slab = nearest[i];
        sign.mesh.setEnabled(!!slab);
        if (!slab) { sign.station = ""; return; }
        sign.mesh.position.set(slab.x, slab.height + 4.2, slab.z);
        if (sign.station === slab.id) return;
        sign.station = slab.id;
        const context = sign.texture.getContext();
        context.fillStyle = "#123a5e"; context.fillRect(0, 0, 768, 128);
        context.fillStyle = "#ffffff"; context.font = "bold 64px system-ui, sans-serif";
        context.textAlign = "center"; context.textBaseline = "middle";
        context.fillText(slab.name || slab.id, 384, 66, 740);
        sign.texture.update();
      });
    }
    return { update };
  }

  // createMiniMap draws the lanes, the stations, and the pods around the
  // ridden pod on canvas.
  function createMiniMap(canvas, topology) {
    const nodes = new Map(topology.nodes.map((node) => [node.id, node.position]));
    const index = segmentIndex(topology.lanes, nodes, MINIMAP_CELL);
    const stations = stopPoints(topology.stations).map((station) => ({ ...station, label: shortLabel(station.name, MINIMAP_LABEL_LENGTH) }));
    // widths holds the width of each label text at the font of fontRatio.
    const widths = new Map();
    let fontRatio = 0;
    const classColors = CLASS_COLORS.map((rgb) => `rgb(${rgb.map((value) => Math.round(value * 255)).join(",")})`);
    const context = canvas.getContext("2d");

    // draw shows the map around world (x, y), with north up. With a finite
    // heading, an arrow at the center shows the ridden pod, and the pose
    // index ridden draws no dot. The pose index marked draws as a large
    // white dot. An index of -1 is no pod.
    function draw(x, y, heading, poses, count, ridden, marked) {
      const ratio = root.devicePixelRatio || 1, size = Math.round(MINIMAP_SIZE * ratio);
      if (canvas.width !== size) canvas.width = canvas.height = size;
      const half = size / 2, scale = half / MINIMAP_RADIUS;
      context.setTransform(1, 0, 0, 1, 0, 0);
      context.clearRect(0, 0, size, size);
      context.save();
      context.beginPath();
      context.arc(half, half, half, 0, 2 * Math.PI);
      context.clip();
      context.fillStyle = "#101d28d9";
      context.fill();
      // The lanes draw in world coordinates through the map transform.
      context.setTransform(scale, 0, 0, scale, half - scale * x, half - scale * y);
      context.beginPath();
      for (const segment of segmentsNear(index, x, y, MINIMAP_RADIUS)) {
        const at = segment * 4;
        context.moveTo(index.coords[at], index.coords[at + 1]);
        context.lineTo(index.coords[at + 2], index.coords[at + 3]);
      }
      context.setTransform(1, 0, 0, 1, 0, 0);
      context.lineWidth = 1.5 * ratio;
      context.strokeStyle = "#8195a6";
      context.stroke();
      const dot = (dx, dy, radius, style) => {
        context.beginPath();
        context.arc(half + scale * dx, half + scale * dy, radius * ratio, 0, 2 * Math.PI);
        context.fillStyle = style;
        context.fill();
      };
      context.font = `${Math.round(10 * ratio)}px system-ui, sans-serif`;
      if (fontRatio !== ratio) { widths.clear(); fontRatio = ratio; }
      const labels = [], lineHeight = 12 * ratio, gap = 5 * ratio;
      for (const station of stations) {
        const dx = station.x - x, dy = station.y - y;
        if (Math.abs(dx) > MINIMAP_RADIUS || Math.abs(dy) > MINIMAP_RADIUS) continue;
        dot(dx, dy, 3, "#f3c479");
        if (!widths.has(station.label)) widths.set(station.label, context.measureText(station.label).width);
        const width = widths.get(station.label), px = half + scale * dx, py = half + scale * dy;
        // A label must be inside the round map.
        if (Math.hypot(px + gap + width - half, py - half) > half - 2 * ratio) continue;
        labels.push({ text: station.label, x: px + gap, y: py - lineHeight / 2, width, height: lineHeight, distance: Math.hypot(dx, dy) });
      }
      // The labels do not cover the arrow of the ridden pod.
      const arrowBox = { x: half - 10 * ratio, y: half - 10 * ratio, width: 20 * ratio, height: 20 * ratio };
      context.textAlign = "left";
      context.textBaseline = "middle";
      context.lineWidth = 3 * ratio;
      context.strokeStyle = "#101d28";
      context.fillStyle = "#f5e2b8";
      for (const label of placeLabels(labels, [arrowBox], MINIMAP_LABELS)) {
        context.strokeText(label.text, label.x, label.y + lineHeight / 2);
        context.fillText(label.text, label.x, label.y + lineHeight / 2);
      }
      for (let i = 0; i < count; i++) {
        const at = i * POSE_FIELDS, dx = poses[at] - x, dy = poses[at + 1] - y;
        if (i === ridden || Math.abs(dx) > MINIMAP_RADIUS || Math.abs(dy) > MINIMAP_RADIUS) continue;
        if (i === marked) dot(dx, dy, 4.5, "#ffffff"); else dot(dx, dy, 2, classColors[poses[at + 4] | 0] || classColors[0]);
      }
      if (Number.isFinite(heading)) {
        context.beginPath();
        for (const [px, py] of miniMapArrow(heading, half, 8 * ratio)) context.lineTo(px, py);
        context.closePath();
        context.fillStyle = "#ffffff";
        context.fill();
      }
      // N marks the north edge of the map.
      const nx = half, ny = 14 * ratio;
      context.font = `bold ${Math.round(12 * ratio)}px system-ui, sans-serif`;
      context.textAlign = "center";
      context.textBaseline = "middle";
      context.fillStyle = "#e5edf5";
      context.fillText("N", nx, ny);
      context.restore();
    }
    return { draw };
  }

  async function start() {
    const status = document.getElementById("status");
    const show = (text) => { status.hidden = false; status.textContent = text; };
    const B = root.BABYLON;
    if (!B) { show("Babylon.js did not load from https://cdn.babylonjs.com. The 2D viewer still works."); return; }
    if (!B.Engine.isSupported()) { show("This browser has no WebGL, so the ride page cannot draw."); return; }
    let ride;
    try { ride = await loadModule(() => show("The ride module stopped. Reload the page.")); } catch (error) { show(error.message); return; }

    const canvas = document.getElementById("scene");
    // useLargeWorldRendering draws relative to the camera, so a far network
    // does not make the cabin jitter.
    const engine = new B.Engine(canvas, true, { useLargeWorldRendering: true });
    const scene = new B.Scene(engine);
    scene.setRenderingOrder(0, groundLayerOrder);
    scene.clearColor = new B.Color4(SKY[0], SKY[1], SKY[2], 1);
    scene.fogMode = B.Scene.FOGMODE_LINEAR;
    scene.fogColor = color(B, SKY);
    scene.fogStart = FOG_START;
    scene.fogEnd = FOG_END;
    scene.skipPointerMovePicking = true;
    const sunDirection = new B.Vector3(-0.5, -0.75, -0.35).normalize();
    const sun = new B.DirectionalLight("sun", sunDirection, scene);
    sun.intensity = 1.1;
    sun.diffuse = new B.Color3(1, 0.96, 0.88);
    // The shadow map covers a square that follows the camera.
    sun.autoUpdateExtends = false;
    sun.shadowFrustumSize = SHADOW_FRUSTUM;
    sun.shadowMinZ = 1;
    sun.shadowMaxZ = 800;
    const fill = new B.HemisphericLight("fill", new B.Vector3(0, 1, 0), scene);
    fill.intensity = 0.5;
    fill.diffuse = color(B, SKY);
    fill.groundColor = new B.Color3(0.32, 0.34, 0.3);
    buildSky(B, scene);
    const shadows = new B.ShadowGenerator(SHADOW_MAP_SIZE, sun);
    shadows.usePercentageCloserFiltering = true;
    shadows.filteringQuality = B.ShadowGenerator.QUALITY_HIGH;
    shadows.bias = 0.0008;
    const camera = new B.FreeCamera("seat", new B.Vector3(0, BEAM_TOP + EYE_HEIGHT, 0), scene);
    // The nearest cabin part is about 0.5 m from the eye. A larger near plane keeps
    // more depth precision for the far ground.
    camera.minZ = 0.3;
    camera.maxZ = FOG_END + 200;
    camera.fov = 1.1;
    camera.rotation.x = SEAT_PITCH;
    const cabin = buildCabin(B, scene, camera);
    const pipeline = new B.DefaultRenderingPipeline("look", true, scene, [camera]);
    pipeline.samples = 4;
    pipeline.fxaaEnabled = true;
    pipeline.imageProcessingEnabled = true;
    pipeline.imageProcessing.toneMappingEnabled = true;
    pipeline.imageProcessing.toneMappingType = B.ImageProcessingConfiguration.TONEMAPPING_ACES;
    pipeline.imageProcessing.exposure = 1.1;
    pipeline.imageProcessing.contrast = 1.15;
    pipeline.bloomEnabled = true;
    pipeline.bloomThreshold = 0.85;
    pipeline.bloomWeight = 0.2;
    const signs = createSigns(B, scene);
    const credit = document.getElementById("credit");
    let tileLayer = null, lastTiles = 0;
    const instrumentation = new B.SceneInstrumentation(scene);
    instrumentation.captureFrameTime = true;

    const picker = document.getElementById("pod");
    const diagnostics = document.getElementById("diagnostics");
    const params = new URLSearchParams(root.location.search);
    let requested = params.get("pod") || "", podList = [], ids = [], version = -1;
    let session = "", world = null, poses = new Float64Array(0), lastTime = performance.now();
    let bridgeTime = 0, connected = false, connectionError = "";
    let chase = false, lookYaw = 0, lookPitch = 0, looking = false;
    const podYaws = new Map();
    // The seat camera, the chase camera, and the mini-map arrow turn with
    // their own damping.
    const seatTurn = newTurn(), chaseTurn = newTurn(), mapTurn = newTurn();
    const resetTurns = () => { for (const turn of [seatTurn, chaseTurn, mapTurn]) turn.angle = NaN; };
    const autoBox = document.getElementById("auto"), fade = document.getElementById("fade");
    const miniMapCanvas = document.getElementById("minimap");
    // focus is the world point that the view shows: the ridden pod, or the
    // station of the platform camera.
    let miniMap = null, lastMap = 0, stops = [], focus = { x: 0, y: 0, known: false };
    // armed is true from the moment the user turns on auto mode until the
    // page reads the riders of the pod. lastRiders is -1 when not known.
    autoBox.checked = params.get("auto") === "1";
    let armed = autoBox.checked, lastRiders = -1, hopping = false;
    // The passenger flow. trip is the tripView of the last HUD update, and
    // platform is the station of the platform camera, or null.
    const tripButton = document.getElementById("tripButton"), tripMenu = document.getElementById("tripMenu");
    const tripElements = Object.fromEntries(["tripOrigin", "tripDestination", "tripParty", "tripShared", "tripMessage", "hudPassenger"].map((id) => [id, document.getElementById(id)]));
    let trip = null, tripPhase = "", arrivedAt = 0, platform = null, stationNames = new Map(), stationBerths = new Map(), partyLimit = 8;
    const hudElements = Object.fromEntries(["hudSpeed", "hudState", "hudWait", "hudTrip", "hudNext", "hudRoute", "hudProgress", "hudProgressBar", "hudNear", "stripSession", "stripMessage", "rideAt1x"].map((id) => [id, document.getElementById(id)]));

    // Mouse look: drag to turn the view. On release, the view returns to forward.
    canvas.addEventListener("pointerdown", (event) => { looking = true; canvas.setPointerCapture(event.pointerId); });
    canvas.addEventListener("pointermove", (event) => {
      if (!looking) return;
      lookYaw = wrapAngle(lookYaw + event.movementX * LOOK_RADIANS_PER_PIXEL);
      lookPitch = Math.max(-LOOK_PITCH_LIMIT, Math.min(LOOK_PITCH_LIMIT, lookPitch + event.movementY * LOOK_RADIANS_PER_PIXEL));
    });
    for (const name of ["pointerup", "pointercancel", "lostpointercapture"]) canvas.addEventListener(name, () => { looking = false; });
    root.addEventListener("keydown", (event) => {
      if (event.target instanceof root.HTMLSelectElement || event.target instanceof root.HTMLButtonElement || event.target instanceof root.HTMLInputElement) return;
      if (event.key === "c" || event.key === "C") {
        chase = !chase;
        chaseTurn.angle = NaN;
        showCabin();
      }
    });
    hudElements.rideAt1x.addEventListener("click", () => {
      const error = ride.setSpeed(1);
      if (error) hudElements.stripMessage.textContent = error;
    });

    function setParam(name, value) {
      const url = new URL(root.location.href);
      if (value === null) url.searchParams.delete(name); else url.searchParams.set(name, value);
      root.history.replaceState(null, "", url);
    }

    function selectPod(id) {
      requested = id;
      picker.value = id;
      setParam("pod", id);
      resetTurns();
      lastRiders = -1;
    }

    // The seat view draws the cabin frame. The chase and the platform views
    // do not.
    function showCabin() {
      for (const part of cabin) part.setEnabled(!chase && !platform);
    }

    // fadeTo fades to black, calls change, and fades back.
    function fadeTo(change) {
      hopping = true;
      fade.style.opacity = "1";
      root.setTimeout(() => {
        change();
        fade.style.opacity = "0";
        hopping = false;
      }, FADE_MS);
    }

    // hop fades to black, rides the pod id, and fades back.
    function hop(id) { fadeTo(() => selectPod(id)); }

    // openTripMenu fills the trip menu and shows it. The origin is the
    // nearest stop to the view.
    function openTripMenu() {
      const sorted = [...stops].sort((a, b) => a.name.localeCompare(b.name));
      const near = nearestStop(stops, focus.x, focus.y);
      const origin = near ? near.id : (sorted[0] ? sorted[0].id : "");
      const destination = (sorted.find((stop) => stop.id !== origin) || { id: "" }).id;
      tripElements.tripOrigin.replaceChildren(...sorted.map((stop) => new Option(stop.name, stop.id, false, stop.id === origin)));
      tripElements.tripDestination.replaceChildren(...sorted.map((stop) => new Option(stop.name, stop.id, false, stop.id === destination)));
      tripElements.tripParty.max = String(partyLimit);
      tripElements.tripParty.value = String(Math.min(Math.max(1, Number(tripElements.tripParty.value) || 1), partyLimit));
      tripElements.tripMessage.textContent = "";
      tripMenu.hidden = false;
    }

    tripMenu.addEventListener("submit", (event) => {
      event.preventDefault();
      const party = Math.min(Math.max(1, Math.round(Number(tripElements.tripParty.value) || 1)), partyLimit);
      const error = ride.requestTrip(tripElements.tripOrigin.value, tripElements.tripDestination.value, party, tripElements.tripShared.checked);
      if (error) { tripElements.tripMessage.textContent = error; return; }
      tripMenu.hidden = true;
      showTrip();
    });
    document.getElementById("tripClose").addEventListener("click", () => { tripMenu.hidden = true; });
    tripButton.addEventListener("click", () => {
      if (trip) {
        // The order stays in the session. The page stops following it.
        ride.endTrip();
        showTrip();
        return;
      }
      if (tripMenu.hidden) openTripMenu(); else tripMenu.hidden = true;
    });

    // showTrip reads the passenger flow and moves the camera to follow it.
    function showTrip() {
      const passenger = JSON.parse(ride.passenger() || "{}");
      if ((passenger.phase || "") !== tripPhase) {
        console.log("ride trip", passenger.phase || "none", passenger);
        tripPhase = passenger.phase || "";
        if (tripPhase === "arrived") arrivedAt = performance.now();
      }
      trip = tripView(passenger, stationNames);
      if (trip && tripPhase === "arrived" && performance.now() - arrivedAt >= ARRIVAL_MS) {
        ride.endTrip();
        trip = null;
        tripPhase = "";
        openTripMenu();
      }
      const station = trip && trip.camera === "platform" ? trip.station : (trip && trip.camera === "" ? (platform && platform.id) : null);
      const nextPlatform = station && stationBerths.has(station) ? station : null;
      const nextPod = trip && trip.podID ? trip.podID : requested;
      // A fade takes longer than one HUD update, so the next change waits.
      if (!hopping && ((platform && platform.id) !== nextPlatform || nextPod !== requested)) {
        const change = () => {
          platform = nextPlatform ? { id: nextPlatform, ...platformView(stationBerths.get(nextPlatform).berths, stationBerths.get(nextPlatform).height) } : null;
          if (nextPod !== requested) selectPod(nextPod);
          showCabin();
        };
        // A move of the camera fades. A new pod for the platform view does not.
        if ((platform && platform.id) !== nextPlatform || !platform) fadeTo(change); else change();
      }
      tripButton.textContent = trip ? (trip.active ? "Leave trip" : "Close trip") : "Trip…";
      picker.disabled = autoBox.disabled = Boolean(trip);
      tripElements.hudPassenger.textContent = trip ? trip.text : "";
    }

    picker.addEventListener("change", () => selectPod(picker.value));
    autoBox.addEventListener("change", () => {
      armed = autoBox.checked;
      setParam("auto", autoBox.checked ? "1" : null);
    });
    picker.addEventListener("focus", () => refreshPods());

    function refreshPods() {
      podList = JSON.parse(ride.pods() || "[]");
      const chosen = choosePod(podList, requested);
      if (chosen !== requested) { resetTurns(); lastRiders = -1; }
      requested = chosen;
      picker.replaceChildren(...podList.map((pod) => new Option(podLabel(pod), pod.id, false, pod.id === requested)));
    }

    function rebuild() {
      const text = ride.topology();
      if (!text) return false;
      const topology = JSON.parse(text);
      if (world) for (const mesh of world.meshes) mesh.dispose(false, true);
      if (tileLayer) tileLayer.dispose();
      world = buildWorld(B, scene, topology);
      shadows.getShadowMap().renderList = world.casters;
      for (const mesh of world.receivers) mesh.receiveShadows = true;
      tileLayer = createTileLayer(B, scene, topology, credit);
      miniMap = createMiniMap(miniMapCanvas, topology);
      stops = stopPoints(topology.stations);
      stationNames = new Map(topology.stations.map((station) => [station.id, station.name || station.id]));
      stationBerths = new Map(topology.stations.filter((station) => station.berths.length > 0).map((station) => [station.id, station]));
      partyLimit = topology.partyLimit || 8;
      platform = null;
      lastTiles = 0;
      session = `${topology.serverStart}/${topology.epoch}/${topology.generation}`;
      version = -1;
      status.hidden = true;
      return true;
    }

    function updatePods(count, seconds) {
      smoothPodYaws(podYaws, ids, poses, count, seconds, POD_YAW_SECONDS);
      // The seat camera is inside the ridden pod, so the seat view does not
      // draw it. The chase and the platform views do.
      const hidden = chase || platform ? "" : requested;
      const counts = world.pods.map(() => 0);
      for (let i = 0; i < count; i++) {
        if (ids[i] === hidden) continue;
        counts[poses[i * POSE_FIELDS + 4] | 0]++;
      }
      world.pods.forEach((pod, index) => {
        // A larger buffer replaces the GPU buffer. Otherwise the frame
        // updates the same buffer.
        if (pod.matrices.length < counts[index] * 16) {
          pod.matrices = new Float32Array(Math.max(16, counts[index] * 32));
          pod.mesh.thinInstanceSetBuffer("matrix", pod.matrices, 16, false);
        }
        pod.count = 0;
      });
      let ridden = -1;
      for (let i = 0; i < count; i++) {
        if (ids[i] === requested) ridden = i;
        if (ids[i] === hidden) continue;
        const at = i * POSE_FIELDS, pod = world.pods[poses[at + 4] | 0];
        const [x, z] = toScene(poses[at], poses[at + 1]);
        writePodMatrix(pod.matrices, pod.count * 16, x, poses[at + 5], z, podYaws.get(ids[i]).angle);
        pod.count++;
      }
      for (const pod of world.pods) {
        pod.mesh.thinInstanceCount = pod.count;
        if (pod.count > 0) pod.mesh.thinInstanceBufferUpdated("matrix");
        pod.mesh.setEnabled(pod.count > 0);
      }
      return ridden;
    }

    function moveCamera(index, seconds) {
      if (!looking) {
        lookYaw = springBack(lookYaw, seconds, LOOK_RETURN_SECONDS);
        lookPitch = springBack(lookPitch, seconds, LOOK_RETURN_SECONDS);
      }
      if (platform) {
        camera.position.fromArray(platform.eye);
        camera.setTarget(B.Vector3.FromArray(platform.target));
        camera.rotation.x += lookPitch;
        camera.rotation.y += lookYaw;
        return;
      }
      if (index < 0) return;
      const at = index * POSE_FIELDS;
      const length = world.pods[poses[at + 4] | 0].length;
      const heading = poses[at + 2];
      const yaw = dampAngle(seatTurn, yawOf(heading), seconds, SEAT_YAW_SECONDS);
      if (chase) {
        const chaseYaw = dampAngle(chaseTurn, yawOf(heading), seconds, CHASE_YAW_SECONDS);
        const [x, y, z] = chaseEye(poses[at], poses[at + 1], chaseYaw + lookYaw, poses[at + 5]);
        camera.position.set(x, y, z);
        camera.rotation.set(CHASE_PITCH + lookPitch, chaseYaw + lookYaw, 0);
        return;
      }
      // The eye is near the front of the pod.
      const forward = length / 2 - 0.6;
      const [x, z] = toScene(poses[at] + Math.cos(heading) * forward, poses[at + 1] + Math.sin(heading) * forward);
      camera.position.set(x, poses[at + 5] + EYE_HEIGHT, z);
      camera.rotation.set(SEAT_PITCH + lookPitch, yaw + lookYaw, 0);
    }

    function showHUD() {
      if (session === "") return;
      showTrip();
      // While the party waits for an assignment, the HUD shows no pod.
      const hud = JSON.parse(ride.hud(trip && !trip.podID && trip.active ? "" : requested) || "{}");
      const riders = hud.found ? hud.riders.length : -1;
      if (!trip && !hopping && autoHop(autoBox.checked, armed, lastRiders, riders)) {
        // With no pod to move to, the page stays on the pod.
        const next = ride.nextPod(requested);
        if (next) hop(next);
      }
      if (riders >= 0) { armed = false; lastRiders = riders; }
      const view = hudView(hud);
      if (!hud.found && trip) view.state = "No pod is assigned yet";
      hudElements.hudSpeed.textContent = view.speed;
      hudElements.hudState.textContent = view.state;
      hudElements.hudWait.textContent = view.wait;
      hudElements.hudTrip.replaceChildren(...view.trip.map((line) => Object.assign(document.createElement("div"), { textContent: line })));
      hudElements.hudNext.textContent = view.next;
      hudElements.hudRoute.textContent = view.route;
      hudElements.hudProgress.hidden = view.progress < 0;
      hudElements.hudProgressBar.style.width = `${(Math.max(0, view.progress) * 100).toFixed(1)}%`;
      hudElements.hudProgress.setAttribute("aria-valuenow", String(Math.round(Math.max(0, view.progress) * 100)));
      const near = focus.known ? nearestStop(stops, focus.x, focus.y) : null;
      hudElements.hudNear.textContent = near ? `Near ${near.name}, ${formatDistance(near.distance)}` : "";
      hudElements.stripSession.textContent = view.session;
      hudElements.stripMessage.textContent = view.message;
      hudElements.rideAt1x.disabled = !view.rideAt1x;
    }

    engine.runRenderLoop(() => {
      const now = performance.now(), seconds = (now - lastTime) / 1000;
      lastTime = now;
      if (session === "" && !rebuild()) { show("Waiting for the server…"); return; }
      const started = performance.now();
      let info = ride.frame(poses);
      if (info.count * POSE_FIELDS > poses.length) {
        poses = new Float64Array(info.count * POSE_FIELDS * 2);
        info = ride.frame(poses);
      }
      bridgeTime = performance.now() - started;
      connected = info.connected;
      connectionError = info.error;
      // A new server start, epoch, or generation can change the network.
      if (info.session !== session) { rebuild(); return; }
      if (info.version !== version) {
        version = info.version;
        // ids has the order of the poses. The pod list has the order of the
        // latest state, which can be newer.
        ids = JSON.parse(ride.ids() || "null") || [];
        for (const id of podYaws.keys()) if (!ids.includes(id)) podYaws.delete(id);
        refreshPods();
      }
      const ridden = updatePods(info.count, seconds);
      moveCamera(ridden, seconds);
      if (platform) {
        focus = { x: platform.center.x, y: platform.center.y, known: true };
      } else if (ridden >= 0) {
        focus = { x: poses[ridden * POSE_FIELDS], y: poses[ridden * POSE_FIELDS + 1], known: true };
      } else {
        focus.known = false;
      }
      const mapHeading = ridden >= 0 && !platform ? dampAngle(mapTurn, poses[ridden * POSE_FIELDS + 2], seconds, SEAT_YAW_SECONDS) : NaN;
      if (focus.known && now - lastMap >= MINIMAP_INTERVAL_MS) {
        lastMap = now;
        // The platform view marks the pod of the trip.
        miniMap.draw(focus.x, focus.y, mapHeading, poses, info.count, platform ? -1 : ridden, platform && trip && trip.podID ? ridden : -1);
      }
      // The light sits back along the sun direction from the camera, so the
      // shadow map covers the ground around the camera.
      const light = camera.position.subtract(sunDirection.scale(400));
      sun.position.fromArray(snapShadowCenter(light.asArray(), sunDirection.asArray(), SHADOW_FRUSTUM / SHADOW_MAP_SIZE));
      if (focus.known && now - lastTiles >= TILE_UPDATE_MS) {
        lastTiles = now;
        tileLayer.update(focus.x, focus.y);
        signs.update(world.slabs, camera.position.x, camera.position.z);
      }
      scene.render();
    });

    root.setInterval(() => {
      const draws = instrumentation.drawCallsCounter.lastSecAverage;
      diagnostics.textContent = `${engine.getFps().toFixed(0)} fps · bridge ${bridgeTime.toFixed(2)} ms · ${draws.toFixed(0)} draws · ${ids.length} pods · ${connected ? "connected" : `disconnected ${connectionError}`}`;
    }, 500);
    root.setInterval(showHUD, HUD_INTERVAL_MS);
    root.addEventListener("resize", () => engine.resize());
  }

  const api = { POSE_FIELDS, segmentIndex, segmentsNear, segmentsCross, convexHull, findHubs, deckGeometry, levelHeight, laneProfile, uniqueLanes, miniMapArrow, snapShadowCenter, stopPoints, shortLabel, placeLabels, platformView, tripView, nearestStop, autoHop, BEAM_TOP, GROUND_LAYERS, groundLayerOrder, toScene, worldToGeo, geoToWorld, groundTiles, tileQuad, nearestSlabs, yawOf, wrapAngle, newTurn, dampAngle, laneSamples, beamGeometry, pillarPoints, stationSlabs, sceneBounds, writePodMatrix, choosePod, podLabel, springBack, chaseEye, smoothPodYaws, formatSpeed, formatDistance, formatDuration, hudView, start };
  if (typeof module !== "undefined" && module.exports) module.exports = api;
  root.PodsimRide = api;
})(typeof globalThis !== "undefined" ? globalThis : this);
