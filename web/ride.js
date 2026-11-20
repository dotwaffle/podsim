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
  // The bottom of a tapered beam.
  const BEAM_BOTTOM_WIDTH = 0.9;
  // The light rail along the middle of the beam top. It is a little above
  // the top, so it does not fight the top in the depth buffer.
  const RAIL_WIDTH = 0.12;
  const RAIL_RISE = 0.05;
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
  // A piece of a round join gets enough beam segments that each is at most
  // PIECE_SAG meters from the curve, and at most PIECE_SEGMENTS segments.
  const PIECE_SAG = 0.01;
  const PIECE_SEGMENTS = 16;
  // BRIDGE_CURVE_SAMPLES is curveSamples of internal/ride: the bridge and
  // the simulation move a pod on this many samples of a curve.
  const BRIDGE_CURVE_SAMPLES = 65;
  const LANES_PER_CHUNK = 2000;
  // The pod body: height and width, in meters. The length comes from the class.
  const POD_HEIGHT = 2.1;
  const POD_WIDTH = 1.5;
  // The eye of the seat camera, above the top of the beam.
  const EYE_HEIGHT = 1.35;
  // STRETCH_CUT is a z of the pod model between the back of the rider seat
  // and the rear bench. A class that is longer than the model stretches
  // only behind it, so the eye, the dashboard, and the screens stay at the
  // same place from the front of the pod in each class.
  // internal/cmd/podmodel has the same value and tests the stretch.
  const STRETCH_CUT = -0.2;
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
  // The vertical field of view of the cameras, in radians. With the fov
  // switch, the seat view widens by up to FOV_WIDEN at FOV_SPEED meters
  // per second and more, and it follows a change of speed with the time
  // constant FOV_SECONDS. A narrower view would cut off the cabin screens.
  const CAMERA_FOV = 1.1;
  const FOV_WIDEN = 0.08;
  const FOV_SPEED = 20;
  const FOV_SECONDS = 0.5;
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
  // One 4,096-pixel shadow map covers a square of SHADOW_FRUSTUM meters,
  // with texels of about 8 cm. The square is centered SHADOW_AHEAD meters in
  // front of the camera, so shadows reach about 280 m ahead and 40 m behind.
  const SHADOW_FRUSTUM = 320;
  const SHADOW_AHEAD = 120;
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
  // The sky of the visual polish: a lighter haze at the horizon, which the
  // fog also uses, a deeper blue at the top, and a warm glow around the
  // sun. The fog thickens with distance as exp(-(d * HAZE_DENSITY)^2), so
  // 4% of a color stays at FOG_END.
  const HAZE = [0.8, 0.86, 0.92];
  const HAZE_TOP = [0.22, 0.42, 0.78];
  const SUN_GLOW = [1, 0.84, 0.6];
  const HAZE_DENSITY = 6e-4;
  // The sky at dusk: a warm haze, a deep blue top, and a strong glow.
  const DUSK_HAZE = [0.84, 0.62, 0.52];
  const DUSK_TOP = [0.16, 0.22, 0.42];
  const DUSK_GLOW = [1, 0.55, 0.25];
  // The pods reflect a cube texture of the sky, SKY_CUBE_SIZE texels on a
  // side. Below the horizon, they reflect GROUND_SHINE.
  const SKY_CUBE_SIZE = 32;
  const GROUND_SHINE = [0.3, 0.32, 0.29];
  // The lights of the pod model: a band across each nose and a thin
  // accent line along each side, in the dark livery stripe. The shell
  // texture coordinates of internal/cmd/podmodel have u = z in meters and
  // the middle of the stripe at v = 0.5, with one repeat of v for the
  // model height, POD_HEIGHT. The light texture spans POD_LIGHT_SPAN
  // meters of z, the model length, so it does not repeat.
  const POD_LIGHT_SPAN = 4;
  const POD_LIGHT_NOSE = 1.55;
  const POD_LIGHT_BAND = 0.05;
  const POD_LIGHT_LINE = 0.012;
  const HEAD_LIGHT = [0.92, 0.97, 1];
  const TAIL_LIGHT = [1, 0.12, 0.08];
  const ACCENT_LIGHT = [0.1, 0.39, 0.5];
  // The image effects of the visual polish. With the default vignette
  // field of view of Babylon, a weight of 0.5 darkens the corners of a
  // 16:9 view by about 21% and the cabin screens by about 8%. The
  // saturation is on the scale of Babylon color curves, from -100 to 100.
  const FX_VIGNETTE = 0.5;
  const FX_SATURATION = 12;
  const CLASS_COLORS = [[0.86, 0.88, 0.9], [0.32, 0.62, 0.9], [0.95, 0.66, 0.22], [0.72, 0.36, 0.86]];
  // Textures: web/textures, which internal/cmd/texgen writes. One repeat of
  // the concrete covers CONCRETE_TILE meters.
  const CONCRETE_TILE = 4;
  const TEXTURE_ANISOTROPY = 8;
  // The cabin screens have this many canvas pixels for each meter of the
  // screen size in the pod model.
  const SCREEN_PIXELS_PER_METER = 2000;
  // The ladder of the route screen shows at most this many stops after the
  // next stop.
  const LADDER_LATER = 3;
  const MODULE_GZIP_LIMIT = 16 * 1024 * 1024;
  const MODULE_LIMIT = 64 * 1024 * 1024;
  // LOOK_SWITCHES are the parts of the visual polish. lookOptions gives
  // each one, so the maintainer can compare the look with and without it.
  const LOOK_SWITCHES = ["tint", "rail", "taper", "sky", "gloss", "lights", "signs", "fx", "fov"];
  // The map tiles get this color, so the guideway stands out over the
  // bright map.
  const TILE_TINT = [0.78, 0.81, 0.85];
  // The light rail has the accent color of the HUD.
  const RAIL_COLOR = [0.21, 0.78, 1];

  // lookOptions reads the switches of the visual polish from a URL query.
  // Each switch is on, unless the query has name=0. polish=0 turns all of
  // them off and gives the look from before the polish. time is "dusk"
  // when the query has time=dusk and the polished sky is on, and "day"
  // otherwise.
  function lookOptions(search) {
    const params = new URLSearchParams(search);
    const all = params.get("polish") !== "0";
    const look = Object.fromEntries(LOOK_SWITCHES.map((name) => [name, all && params.get(name) !== "0"]));
    look.time = look.sky && params.get("time") === "dusk" ? "dusk" : "day";
    return look;
  }

  // lightLook returns the sun and the fill light of a time of lookOptions:
  // the direction of the sunlight, the sun color and intensity, and the
  // sky and ground colors and the intensity of the hemispheric fill. The
  // day is the light from before the visual polish. At dusk, a warm sun
  // 15 degrees above the horizon in the west-southwest throws long
  // shadows, and a cool fill lights the shade.
  function lightLook(time) {
    if (time === "dusk") {
      // The azimuth turns from scene +z (north) toward +x (east).
      const elevation = 15 * DEGREE, azimuth = 245 * DEGREE;
      const toSun = [Math.cos(elevation) * Math.sin(azimuth), Math.sin(elevation), Math.cos(elevation) * Math.cos(azimuth)];
      return { fromSun: toSun.map((value) => -value), sun: [1, 0.64, 0.38], sunIntensity: 1, fill: [0.42, 0.46, 0.66], fillGround: [0.2, 0.18, 0.18], fillIntensity: 0.45 };
    }
    const fromSun = [-0.5, -0.75, -0.35], length = Math.hypot(...fromSun);
    return { fromSun: fromSun.map((value) => value / length), sun: [1, 0.96, 0.88], sunIntensity: 1.1, fill: SKY, fillGround: [0.32, 0.34, 0.3], fillIntensity: 0.5 };
  }

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
  // a straight lane, or samples of a quadratic Bezier curve at evenly spaced
  // parameters. count sets the number of samples of a curve.
  function laneSamples(from, to, control, count) {
    if (!control) return [from, to];
    const approx = Math.hypot(control.x - from.x, control.y - from.y) + Math.hypot(to.x - control.x, to.y - control.y);
    count ??= Math.max(3, Math.min(MAX_CURVE_SAMPLES, Math.ceil(approx / CURVE_STEP) + 1));
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
  // so the beam follows the ramp curve. The profile starts trimStart
  // meters after the start of the lane and stops trimEnd meters before its
  // end, where the pieces of the round joins take over. Each cut is on the
  // line that the bridge moves a pod on, so it meets the end of its piece.
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
    const profile = points.map((point, i) => ({ x: point.x, y: point.y, h: levelHeight(a + (b - a) * (total > 0 ? lengths[i] / total : 0), levels) }));
    const start = lane.trimStart || 0, end = lane.trimEnd || 0;
    if (start <= 0 && end <= 0) return profile;
    // The parameter of a profile point is its length fraction on a straight
    // lane, and its curve parameter on a curve.
    const params = lane.control ? points.map((_, i) => i / (points.length - 1)) : lengths.map((length) => (total > 0 ? length / total : 0));
    const line = laneLine(lane, nodes);
    const cut = (at) => ({ ...line.at(at), h: levelHeight(a + (b - a) * (line.length > 0 ? at / line.length : 0), levels) });
    const first = cut(start), last = cut(line.length - end);
    const inside = profile.filter((_, i) => params[i] > first.param && params[i] < last.param);
    return [first, ...inside, last].map(({ x, y, h }) => ({ x, y, h }));
  }

  // laneLine returns the line that the bridge moves a pod on along a lane,
  // or null when a node is missing: the length of the line, and at, which
  // gives the point at a distance along it with its parameter. The
  // parameter is the length fraction on a straight lane, and the curve
  // parameter on a curve. It is laneShape.at of internal/ride.
  function laneLine(lane, nodes) {
    const from = nodes.get(lane.from), to = nodes.get(lane.to);
    if (!from || !to) return null;
    if (!lane.control) {
      const length = Math.hypot(to.x - from.x, to.y - from.y);
      return {
        length,
        at: (distance) => {
          const f = length > 0 ? Math.min(Math.max(distance / length, 0), 1) : 0;
          return { x: from.x + (to.x - from.x) * f, y: from.y + (to.y - from.y) * f, param: f };
        },
      };
    }
    const line = laneSamples(from, to, lane.control, BRIDGE_CURVE_SAMPLES);
    const along = [0];
    for (let i = 1; i < line.length; i++) along.push(along[i - 1] + Math.hypot(line[i].x - line[i - 1].x, line[i].y - line[i - 1].y));
    return {
      length: along[along.length - 1],
      at: (distance) => {
        let i = 1;
        while (i < along.length - 1 && along[i] < distance) i++;
        const span = along[i] - along[i - 1], f = span > 0 ? Math.min(Math.max((distance - along[i - 1]) / span, 0), 1) : 0;
        const p = line[i - 1], q = line[i];
        return { x: p.x + (q.x - p.x) * f, y: p.y + (q.y - p.y) * f, param: (i - 1 + f) / (line.length - 1) };
      },
    };
  }

  // filletPlan returns the lanes with the trims that the page draws, and
  // the pieces of the round joins as objects with their two lanes and their
  // start, control, and end points. pieces comes from the topology, with
  // lane indexes into lanes. A piece starts and stops where laneProfile
  // cuts its two lanes, so the beams meet. A hub draws as a deck, so a
  // piece to or from a hub lane does not draw, and the other lane keeps
  // that end at full length, so its beam meets the deck.
  function filletPlan(lanes, pieces, hubLanes, nodes) {
    const fullEnd = new Set(), fullStart = new Set(), drawn = [];
    for (const piece of pieces || []) {
      const from = lanes[piece.in], to = lanes[piece.out];
      if (!from || !to) continue;
      if (hubLanes.has(from.id) || hubLanes.has(to.id)) {
        fullEnd.add(piece.in);
        fullStart.add(piece.out);
        continue;
      }
      const before = laneLine(from, nodes), after = laneLine(to, nodes);
      if (!before || !after) continue;
      const { x: x0, y: y0 } = before.at(before.length - (from.trimEnd || 0)), { x: x1, y: y1 } = after.at(to.trimStart || 0);
      drawn.push({ from, to, start: { x: x0, y: y0 }, control: { x: piece.control[0], y: piece.control[1] }, end: { x: x1, y: y1 } });
    }
    const planned = lanes.map((lane, i) => {
      if (!fullEnd.has(i) && !fullStart.has(i)) return lane;
      return { ...lane, trimStart: fullStart.has(i) ? 0 : lane.trimStart, trimEnd: fullEnd.has(i) ? 0 : lane.trimEnd };
    });
    return { lanes: planned, pieces: drawn };
  }

  // pieceProfile returns the world points along a piece of filletPlan,
  // each with the beam top h, or null when a node is missing. A point at a
  // fraction of the arc length of the piece has the height of the point at
  // the same fraction of the two lane parts that the piece replaces, as
  // the pod heights of ride.frame do.
  function pieceProfile(piece, nodes, levels) {
    const { from, to, start, control, end } = piece;
    const fromLine = laneLine(from, nodes), toLine = laneLine(to, nodes);
    if (!fromLine || !toLine) return null;
    const fromLength = fromLine.length, toLength = toLine.length;
    const before = from.trimEnd || 0, after = to.trimStart || 0;
    // A segment of length l that turns by a is about l * a / 8 from the
    // curve, so n segments over a length L that turns by A are L * A /
    // (8 * n * n) from it.
    const span = Math.hypot(control.x - start.x, control.y - start.y) + Math.hypot(end.x - control.x, end.y - control.y);
    const turn = span > 0 ? Math.abs(wrapAngle(Math.atan2(end.y - control.y, end.x - control.x) - Math.atan2(control.y - start.y, control.x - start.x))) : 0;
    const count = Math.min(PIECE_SEGMENTS, Math.max(1, Math.ceil(Math.sqrt(span * turn / (8 * PIECE_SAG))))) + 1;
    const points = [];
    for (let i = 0; i < count; i++) {
      const t = i / (count - 1), u = 1 - t;
      points.push({ x: u * u * start.x + 2 * u * t * control.x + t * t * end.x, y: u * u * start.y + 2 * u * t * control.y + t * t * end.y });
    }
    const lengths = [0];
    for (let i = 1; i < count; i++) lengths.push(lengths[i - 1] + Math.hypot(points[i].x - points[i - 1].x, points[i].y - points[i - 1].y));
    const total = lengths[count - 1];
    const distance = (lane, at, length) => (lane.fromDistance || 0) + ((lane.toDistance || 0) - (lane.fromDistance || 0)) * (length > 0 ? at / length : 0);
    return points.map((point, i) => {
      const done = (total > 0 ? lengths[i] / total : 0) * (before + after);
      const d = done <= before ? distance(from, fromLength - before + done, fromLength) : distance(to, done - before, toLength);
      return { x: point.x, y: point.y, h: levelHeight(d, levels) };
    });
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
    const positions = [], normals = [], colors = [], uvs = [], indices = [];
    if (hull.length < 3) return { positions: new Float32Array(), normals: new Float32Array(), colors: new Float32Array(), uvs: new Float32Array(), indices: new Uint32Array() };
    // The texture coordinates are meters from the first hull point over
    // CONCRETE_TILE, so they stay small far from the origin.
    const [ox, oz] = toScene(hull[0].x, hull[0].y);
    const add = (point, height, normal, shade) => {
      const [x, z] = toScene(point.x, point.y);
      positions.push(x, height, z);
      normals.push(...normal);
      colors.push(shade, shade, shade * 0.97, 1);
      // The top uses x and z. A wall uses its horizontal place and the
      // height.
      if (normal[1] > 0.5) uvs.push((x - ox) / CONCRETE_TILE, (z - oz) / CONCRETE_TILE);
      else uvs.push((x - ox + z - oz) / CONCRETE_TILE, (height - top) / CONCRETE_TILE);
      return positions.length / 3 - 1;
    };
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
    return { positions: new Float32Array(positions), normals: new Float32Array(normals), colors: new Float32Array(colors), uvs: new Float32Array(uvs), indices: new Uint32Array(indices) };
  }

  // guidewayProfiles returns the profiles of laneProfile for each lane and
  // of pieceProfile for each piece of filletPlan, with null for a lane or
  // a piece with a missing node.
  function guidewayProfiles(lanes, nodes, levels, pieces = []) {
    return [...lanes.map((lane) => laneProfile(lane, nodes, levels)), ...pieces.map((piece) => pieceProfile(piece, nodes, levels))];
  }

  // profileSides returns the horizontal unit vector to the left of travel
  // at each scene point [x, z] of a profile.
  function profileSides(points) {
    const sides = [];
    for (let i = 0; i < points.length; i++) {
      const previous = points[Math.max(0, i - 1)], next = points[Math.min(points.length - 1, i + 1)];
      let dx = next[0] - previous[0], dz = next[1] - previous[1];
      const length = Math.hypot(dx, dz);
      if (length === 0) { dx = 1; dz = 0; } else { dx /= length; dz /= length; }
      sides.push([-dz, dx]);
    }
    return sides;
  }

  // beamGeometry returns the vertex data of a beam along each lane and
  // each piece of filletPlan, as profileBeams does.
  function beamGeometry(lanes, nodes, levels, pieces = [], bottomWidth = BEAM_WIDTH) {
    return profileBeams(guidewayProfiles(lanes, nodes, levels, pieces), bottomWidth);
  }

  // profileBeams returns the vertex data of a beam along each profile of
  // guidewayProfiles. The top is BEAM_WIDTH wide and the bottom is
  // bottomWidth wide, so a narrower bottom gives sides that slope in. Each
  // face has its own vertices and normals, so the faces have flat shading.
  // The texture coordinate u is the distance along the beam from its
  // start, and v is the distance across the face, both over CONCRETE_TILE.
  function profileBeams(profiles, bottomWidth = BEAM_WIDTH) {
    const positions = [], normals = [], colors = [], uvs = [], indices = [];
    const half = BEAM_WIDTH / 2, bottomHalf = bottomWidth / 2;
    // A side normal points out and, when the sides slope in, down.
    const slope = Math.hypot(BEAM_DEPTH, half - bottomHalf), out = BEAM_DEPTH / slope, down = (half - bottomHalf) / slope;
    for (const profile of profiles) {
      if (!profile) continue;
      const points = profile.map((point) => toScene(point.x, point.y));
      const sides = profileSides(points);
      const along = [0];
      for (let i = 1; i < points.length; i++) along.push(along[i - 1] + Math.hypot(points[i][0] - points[i - 1][0], points[i][1] - points[i - 1][1]));
      // Each face: two edges, given as [side sign, depth below the top],
      // and a normal. The running surface on top is darker than the
      // concrete sides.
      const top = 0, bottom = BEAM_DEPTH;
      const faces = [
        { edges: [[1, top], [-1, top]], normal: () => [0, 1, 0], shade: 0.42 },
        { edges: [[-1, top], [-1, bottom]], normal: (left) => [-left[0] * out, -down, -left[1] * out], shade: 0.82 },
        { edges: [[-1, bottom], [1, bottom]], normal: () => [0, -1, 0], shade: 0.6 },
        { edges: [[1, bottom], [1, top]], normal: (left) => [left[0] * out, -down, left[1] * out], shade: 0.82 },
      ];
      for (const face of faces) {
        const base = positions.length / 3;
        for (let i = 0; i < points.length; i++) {
          const left = sides[i], normal = face.normal(left);
          for (const [sign, depth] of face.edges) {
            const offset = (depth === top ? half : bottomHalf) * sign;
            positions.push(points[i][0] + left[0] * offset, profile[i].h - depth, points[i][1] + left[1] * offset);
            normals.push(normal[0], normal[1], normal[2]);
            colors.push(face.shade, face.shade, face.shade * 0.97, 1);
            uvs.push(along[i] / CONCRETE_TILE, (sign * half + depth) / CONCRETE_TILE);
          }
        }
        for (let i = 0; i + 1 < points.length; i++) {
          const a = base + 2 * i;
          indices.push(a, a + 1, a + 2, a + 1, a + 3, a + 2);
        }
      }
    }
    return { positions: new Float32Array(positions), normals: new Float32Array(normals), colors: new Float32Array(colors), uvs: new Float32Array(uvs), indices: new Uint32Array(indices) };
  }

  // railGeometry returns the vertex data of the light rail along the
  // middle of each beam top: a flat strip RAIL_WIDTH wide, RAIL_RISE above
  // the top of each profile of guidewayProfiles. At a fork, the rails of
  // the two beams split as the beams do, so a rail does not cross the
  // running surface of the other beam. The rail is unlit, so it has no
  // normals.
  function railGeometry(profiles) {
    const positions = [], indices = [];
    const half = RAIL_WIDTH / 2;
    for (const profile of profiles) {
      if (!profile) continue;
      const points = profile.map((point) => toScene(point.x, point.y));
      const sides = profileSides(points), base = positions.length / 3;
      points.forEach(([x, z], i) => {
        const [lx, lz] = sides[i], y = profile[i].h + RAIL_RISE;
        positions.push(x + lx * half, y, z + lz * half, x - lx * half, y, z - lz * half);
      });
      for (let i = 0; i + 1 < points.length; i++) {
        const a = base + 2 * i;
        indices.push(a, a + 1, a + 2, a + 1, a + 3, a + 2);
      }
    }
    return { positions: new Float32Array(positions), indices: new Uint32Array(indices) };
  }

  // boxUVs returns texture coordinates for a mesh by a box projection: each
  // vertex uses the plane of the main axis of its normal, with one texture
  // repeat for each tile meters. The station slabs and the pillars use it.
  function boxUVs(positions, normals, tile) {
    const uvs = new Float32Array(positions.length / 3 * 2);
    for (let i = 0; i < positions.length / 3; i++) {
      const [x, y, z] = [positions[3 * i], positions[3 * i + 1], positions[3 * i + 2]];
      const [nx, ny, nz] = [Math.abs(normals[3 * i]), Math.abs(normals[3 * i + 1]), Math.abs(normals[3 * i + 2])];
      const [u, v] = nx >= ny && nx >= nz ? [z, y] : (ny >= nz ? [x, z] : [x, y]);
      uvs[2 * i] = u / tile;
      uvs[2 * i + 1] = v / tile;
    }
    return uvs;
  }

  // pillarPoints returns scene points [x, z, top] under the lanes, about
  // one for each PILLAR_SPACING meters of lane, at most MAX_PILLARS. They
  // follow the beams of laneProfile, which stop at the trims, so the pieces
  // of the round joins have no pillars. top is
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

  // parsePodModel reads web/pod.glb, which internal/cmd/podmodel writes. It
  // returns the geometry, the texture coordinates, and the base color of
  // each part by node name, the seat eye, the screens, and the model
  // length. The scene is left-handed and glTF is right-handed, so x changes
  // sign. With the same index order, the triangles then wind as Babylon's
  // own meshes do. A screen keeps the glTF translation, rotation, and scale
  // of its node, and screenQuad changes them to the scene.
  function parsePodModel(buffer) {
    const view = new DataView(buffer);
    const word = (at) => view.getUint32(at, true);
    if (buffer.byteLength < 28 || word(0) !== 0x46546c67 || word(4) !== 2 || word(8) !== buffer.byteLength) throw new Error("The pod model is not a GLB file.");
    const jsonLength = word(12);
    if (word(16) !== 0x4e4f534a || 28 + jsonLength > buffer.byteLength || word(24 + jsonLength) !== 0x004e4942) throw new Error("The pod model chunks are not valid.");
    const doc = JSON.parse(new TextDecoder().decode(new Uint8Array(buffer, 20, jsonLength)));
    const bin = 28 + jsonLength;
    const accessor = (index, Type, size) => {
      const a = doc.accessors[index], v = doc.bufferViews[a.bufferView];
      const bytes = a.count * size * Type.BYTES_PER_ELEMENT, start = bin + v.byteOffset;
      if (bytes > v.byteLength || start + v.byteLength > buffer.byteLength) throw new Error("The pod model accessors are not valid.");
      return new Type(buffer.slice(start, start + bytes));
    };
    const parts = {}, screens = {};
    let eye = null, half = 0;
    for (const node of doc.nodes) {
      if (node.mesh === undefined) {
        if (node.name === "eye") eye = [-node.translation[0], node.translation[1], node.translation[2]];
        else if (node.translation && node.rotation && node.scale) screens[node.name] = { translation: node.translation, rotation: node.rotation, scale: node.scale };
        continue;
      }
      const primitive = doc.meshes[node.mesh].primitives[0];
      const positions = accessor(primitive.attributes.POSITION, Float32Array, 3);
      const normals = accessor(primitive.attributes.NORMAL, Float32Array, 3);
      const indices = accessor(primitive.indices, Uint16Array, 1);
      const uvs = primitive.attributes.TEXCOORD_0 === undefined ? null : accessor(primitive.attributes.TEXCOORD_0, Float32Array, 2);
      if (uvs && uvs.length / 2 !== positions.length / 3) throw new Error("The pod model texture coordinates are not valid.");
      for (let i = 0; i < positions.length; i += 3) {
        positions[i] = -positions[i];
        normals[i] = -normals[i];
        half = Math.max(half, Math.abs(positions[i + 2]));
      }
      for (const index of indices) if (index >= positions.length / 3) throw new Error("The pod model indices are not valid.");
      const factor = doc.materials[primitive.material].pbrMetallicRoughness.baseColorFactor;
      parts[node.name] = { positions, normals, uvs, indices, color: factor.slice(0, 3) };
    }
    for (const name of ["shell", "glass", "gear", "cabin"]) if (!parts[name]) throw new Error(`The pod model has no ${name} part.`);
    if (!eye) throw new Error("The pod model has no eye.");
    return { parts, eye, screens, length: 2 * half };
  }

  // stretchZ returns the z in a pod of scale times the model length of the
  // point at z of the pod model, which is length meters long, and the
  // stretch of z there. The part ahead of STRETCH_CUT moves to the new
  // front, and the part behind it stretches to the new rear. At scale 1,
  // the z does not change.
  function stretchZ(z, scale, length) {
    const shift = (scale - 1) * length / 2;
    if (z >= STRETCH_CUT) return [z + shift, 1];
    const grow = 2 * shift / (STRETCH_CUT + length / 2);
    return [z + shift + (z - STRETCH_CUT) * grow, 1 + grow];
  }

  // screenQuad returns the vertex data of a screen of the pod model in the
  // frame of the cabin mesh, stretched to scale times the model length, as
  // stretchPart does. length is the model length.
  // In the glTF frame of its node, the screen is the unit square from -0.5
  // to 0.5 in x and y, scaled by the node scale, with its face toward -z.
  // There, +x is the left of the rider, because glTF is right-handed. u
  // grows to the right of the rider, and v = 1 is the top edge, which is
  // the top row of the canvas when the texture inverts y, as tileQuad
  // says.
  function screenQuad(screen, scale, length) {
    const [qx, qy, qz, qw] = screen.rotation, [tx, ty, tz] = screen.translation, [sx, sy] = screen.scale;
    // rotate turns v by the quaternion.
    const rotate = ([x, y, z]) => {
      const cx = 2 * (qy * z - qz * y), cy = 2 * (qz * x - qx * z), cz = 2 * (qx * y - qy * x);
      return [x + qw * cx + qy * cz - qz * cy, y + qw * cy + qz * cx - qx * cz, z + qw * cz + qx * cy - qy * cx];
    };
    // The corners from the bottom left of the rider, counterclockwise as
    // the rider sees them.
    const corners = [[0.5, -0.5], [-0.5, -0.5], [-0.5, 0.5], [0.5, 0.5]];
    const positions = new Float32Array(12), normals = new Float32Array(12);
    const face = rotate([0, 0, -1]);
    corners.forEach(([cx, cy], i) => {
      const [x, y, z] = rotate([cx * sx, cy * sy, 0]);
      const [stretched, slope] = stretchZ(tz + z, scale, length);
      positions.set([-(tx + x), ty + y, stretched], 3 * i);
      const normal = [-face[0], face[1], face[2] / slope], size = Math.hypot(...normal);
      normals.set(normal.map((value) => value / size), 3 * i);
    });
    return { positions, normals, uvs: new Float32Array([0, 0, 1, 0, 1, 1, 0, 1]), indices: new Uint16Array([0, 1, 2, 0, 2, 3]) };
  }

  // stretchPart returns the positions and the normals of part, a part of
  // the pod model of length meters, stretched by stretchZ to scale times
  // that length. The z of a normal divides by the stretch, so the normal
  // stays at right angles to the surface.
  function stretchPart(part, scale, length) {
    const positions = Float32Array.from(part.positions), normals = Float32Array.from(part.normals);
    for (let i = 0; i < positions.length; i += 3) {
      const [z, slope] = stretchZ(positions[i + 2], scale, length);
      positions[i + 2] = z;
      normals[i + 2] /= slope;
      const size = Math.hypot(normals[i], normals[i + 1], normals[i + 2]);
      normals[i] /= size; normals[i + 1] /= size; normals[i + 2] /= size;
    }
    return { positions, normals };
  }

  // seatEye returns the scene position of the eye for a pod at scene (x, z)
  // with yaw, as writePodMatrix places the pod. eye is the model eye, scale
  // is the class length over the model length, and length is the model
  // length.
  function seatEye(x, top, z, yaw, eye, scale, length) {
    const c = Math.cos(yaw), s = Math.sin(yaw), ex = eye[0], [ez] = stretchZ(eye[2], scale, length);
    return [x + c * ex + s * ez, top + POD_HEIGHT / 2 + eye[1], z - s * ex + c * ez];
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

  // seatFov returns the field of view of the seat camera at speed meters
  // per second: CAMERA_FOV when the pod stands, and wider with speed, to
  // CAMERA_FOV + FOV_WIDEN at FOV_SPEED.
  function seatFov(speed) {
    return CAMERA_FOV + FOV_WIDEN * Math.min(1, Math.max(0, speed) / FOV_SPEED);
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

  // cutAtWord returns the start of a text that does not fit, cut is that
  // start, cut back to the last word boundary in it, with an ellipsis. A
  // start with no word boundary stays whole.
  function cutAtWord(cut) {
    const space = cut.lastIndexOf(" ");
    return `${(space > 0 ? cut.slice(0, space) : cut).trim()}…`;
  }

  // withoutBrackets returns text without its parts in brackets, or text
  // when nothing else is left.
  function withoutBrackets(text) {
    return text.replace(/\s*\(.*?\)\s*/g, " ").trim() || text;
  }

  // shortLabel returns a station name for the mini-map: the name without a
  // part in brackets, cut at a word boundary to at most limit characters.
  function shortLabel(name, limit) {
    const plain = withoutBrackets(name);
    if (plain.length <= limit) return plain;
    return cutAtWord(plain.slice(0, limit));
  }

  // fitText returns text, or else text cut at a word boundary, as
  // shortLabel cuts, so that it is at most width wide. measure returns the
  // width of a text. A first word that is too wide loses characters, down
  // to one character.
  function fitText(text, width, measure) {
    if (measure(text) <= width) return text;
    let end = text.length;
    while (end > 1 && measure(cutAtWord(text.slice(0, end))) > width) end--;
    return cutAtWord(text.slice(0, end));
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

  // shadowFocus returns the point ahead meters from position along the
  // horizontal part of the view direction forward. A view straight up or
  // down has no horizontal part, so the point is position. Vectors are
  // [x, y, z] arrays.
  function shadowFocus(position, forward, ahead) {
    const length = Math.hypot(forward[0], forward[2]);
    if (length < 1e-6) return [...position];
    return [position[0] + forward[0] / length * ahead, position[1], position[2] + forward[2] / length * ahead];
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

  // createFader fades element to black, calls a change after ms, and fades
  // back. timers gives setTimeout and clearTimeout. busy is true from the
  // start of a fade to its end. cancel ends a pending fade without its
  // change, because a rebuild of the scene can remove what the change
  // reads. A callback of a cancelled fade that still runs does nothing.
  function createFader(timers, element, ms) {
    let generation = 0, timer = null, busy = false;
    function end() {
      element.style.opacity = "0";
      busy = false;
      timer = null;
    }
    return {
      busy: () => busy,
      fadeTo(change) {
        const current = ++generation;
        busy = true;
        element.style.opacity = "1";
        timer = timers.setTimeout(() => {
          if (current !== generation) return;
          // A change that throws must not leave the view black.
          try { change(); } finally { end(); }
        }, ms);
      },
      cancel() {
        generation++;
        if (timer !== null) timers.clearTimeout(timer);
        end();
      },
    };
  }

  // createAutoMode keeps the state of auto mode. armed is true from the
  // moment the user turns on auto mode until the page reads the riders of
  // the pod. lastRiders is -1 when not known. A hop of auto mode that a
  // rebuild cancels arms auto mode again, so that the next update chooses
  // a pod of the new scene.
  function createAutoMode(fader, armed) {
    let lastRiders = -1, pending = false;
    return {
      arm(on) { armed = on; },
      // forget clears the riders of the last pod, after a change of pod.
      forget() { lastRiders = -1; },
      // update reads the riders of the pod, -1 when not known. When auto
      // mode moves, choose gives the next pod, and select rides it after
      // the fade. With no next pod, the page stays on the pod.
      update(auto, riders, choose, select) {
        if (!fader.busy() && autoHop(auto, armed, lastRiders, riders)) {
          const next = choose();
          if (next) {
            pending = true;
            fader.fadeTo(() => {
              pending = false;
              select(next);
            });
          }
        }
        if (riders >= 0) {
          armed = false;
          lastRiders = riders;
        }
      },
      // cancel ends a pending fade when the scene rebuilds.
      cancel() {
        if (pending) armed = true;
        pending = false;
        fader.cancel();
      },
    };
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
      // The fields of the racing layout: the cabin screens and the overlay.
      podName: "", activity: "", waitReason: "", blockedBy: "", speedKmh: 0, limitKmh: 0, nextName: "", eta: "", time: "", over: false, distance: "", aboard: 0,
      session: `Session ${session.speed}x${session.paused ? ", paused" : ""}${session.connected ? "" : `, disconnected${session.error ? `: ${session.error}` : ""}`}`,
      message: session.message || "",
      rideAt1x: session.connected === true && session.speed !== 1,
    };
    if (!hud.found) return view;
    view.speed = formatSpeed(hud.speed) + (hud.speedLimit > 0 ? `, limit ${formatSpeed(hud.speedLimit)}` : "");
    view.state = `${hud.podID} (${hud.class}): ${hud.activity}`;
    view.podName = `${hud.podID} (${hud.class})`;
    view.activity = hud.activity || "";
    view.speedKmh = Math.round(hud.speed * 3.6);
    view.limitKmh = hud.speedLimit > 0 ? Math.round(hud.speedLimit * 3.6) : 0;
    view.nextName = hud.nextStop || "";
    view.aboard = hud.riders.reduce((sum, rider) => sum + rider.partySize, 0);
    if (hud.waitReason) view.wait = `Waiting: ${hud.waitReason}${hud.blockedBy ? ` (${hud.blockedBy})` : ""}`;
    view.waitReason = hud.waitReason || "";
    view.blockedBy = hud.blockedBy || "";
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
      view.distance = hud.route.after ? `over ${distance}` : distance;
      view.eta = hud.route.after ? `over ${time}` : time;
      view.time = time;
      view.over = hud.route.after;
      view.progress = traveled + remaining > 0 ? traveled / (traveled + remaining) : 1;
    }
    return view;
  }

  // stopLadder returns the stops of the ladder of the ridden pod for the
  // data of ride.hud(), with the route progress of hudView. The bridge
  // sends the next stop and the origin and the destination of each party
  // aboard, not the stops of the route window, so the ladder uses them:
  // the stops behind the pod are the origins of the parties aboard, at
  // most two, then the next stop, then the other destinations of the
  // parties aboard, at most LADDER_LATER. The stops keep the order of the
  // riders. A ladder with at most one stop behind and none after the next
  // stop is one leg, and the page shows it as a bar from the origin to the
  // next stop.
  function stopLadder(hud, progress) {
    if (!hud.found) return { stops: [], progress: -1, oneLeg: true };
    const unique = (names) => [...new Set(names.filter(Boolean))];
    const next = hud.nextStop || "";
    const ahead = unique([next, ...hud.riders.map((rider) => rider.to)]);
    const behind = unique(hud.riders.map((rider) => rider.from)).slice(-2);
    const later = ahead.filter((name) => name !== next).slice(0, LADDER_LATER);
    const stops = [
      ...behind.map((name) => ({ name, state: "done" })),
      ...(next ? [{ name: next, state: "next" }] : []),
      ...later.map((name) => ({ name, state: "later" })),
    ];
    return { stops, progress, oneLeg: behind.length <= 1 && later.length === 0 };
  }

  // ladderItems returns the stops of a ladder with the pod in place: a bar
  // with the route progress after the stops behind the pod. The bar is
  // empty when the progress is not known (-1).
  function ladderItems(ladder) {
    const behind = ladder.stops.filter((stop) => stop.state === "done").length;
    const bar = { bar: true, progress: ladder.progress };
    return [...ladder.stops.slice(0, behind), bar, ...ladder.stops.slice(behind)];
  }

  // The speed gauge is an arc of 270 degrees with its gap at the bottom.
  // Canvas angles turn clockwise from +x.
  const GAUGE_START = 0.75 * Math.PI;
  const GAUGE_SWEEP = 1.5 * Math.PI;

  // gaugeView returns the scale of the speed gauge in km/h for the speed
  // and the lane limit in meters per second: the top of the scale, a
  // multiple of 20 km/h above the speed and 20% above the limit, and the
  // fractions of the scale at the speed and at the limit. limit is -1 when
  // the lane has no limit.
  function gaugeView(speed, limit) {
    const kmh = Math.max(0, speed * 3.6), limitKmh = Math.max(0, limit * 3.6);
    const top = Math.max(40, Math.ceil(Math.max(kmh, limitKmh * 1.2) / 20) * 20);
    return { top, speed: kmh / top, limit: limitKmh > 0 ? limitKmh / top : -1, over: limitKmh > 0 && kmh > limitKmh + 0.5 };
  }

  // hudMode returns where the page shows the readouts: "cabin" on the
  // screens of the cabin in the seat view of a ridden pod, when the pod
  // model has screens, and "overlay" on the screen overlay otherwise.
  function hudMode(chase, platform, hasScreens, ridden) {
    return !chase && !platform && hasScreens && ridden ? "cabin" : "overlay";
  }

  // The look of the readouts: dark panels, a cyan accent, amber for a wait
  // and the limit, and red for a speed over the limit.
  const HUD_FONT = "system-ui, sans-serif";
  const HUD_COLORS = { panel: "#0a141d", track: "#1e3242", accent: "#35c8ff", text: "#eaf3fa", dim: "#8ca3b6", warn: "#ffb547", over: "#ff5d50", done: "#55697c" };

  // nameChoices returns the texts of a station name for drawText: the name,
  // then the name without the part in brackets after the place.
  function nameChoices(name) { return [name, withoutBrackets(name)]; }

  // waitText returns the wait reason with blockedBy in brackets, at most
  // width wide. When the whole text does not fit, blockedBy is cut first,
  // so the reason stays whole as long as it can.
  function waitText(reason, blockedBy, width, measure) {
    if (!blockedBy) return fitText(reason, width, measure);
    const whole = `${reason} (${blockedBy})`;
    if (measure(whole) <= width) return whole;
    const room = width - measure(`${reason} ()`);
    if (room < measure("00…")) return fitText(reason, width, measure);
    return `${reason} (${fitText(blockedBy, room, measure)})`;
  }

  // drawText draws text at (x, y) on context, cut to fit width. text is one
  // text, a list of texts from the longest to the shortest, of which the
  // first that fits draws, or a function of the measure and the width that
  // returns the text. align is the canvas text alignment at x. It returns
  // the text that it drew.
  function drawText(context, text, x, y, width, size, color, align = "left", weight = 600) {
    context.font = `${weight} ${Math.round(size)}px ${HUD_FONT}`;
    context.fillStyle = color;
    context.textAlign = align;
    context.textBaseline = "alphabetic";
    const measure = (value) => context.measureText(value).width;
    const choices = [text].flat();
    const chosen = typeof text === "function" ? text(measure, width) : (choices.find((choice) => measure(choice) <= width) ?? choices[choices.length - 1]);
    const fitted = fitText(chosen, width, measure);
    if (fitted) context.fillText(fitted, x, y);
    return fitted;
  }

  // drawPanel fills the rectangle of a screen with the panel color and
  // draws an accent line along its top.
  function drawPanel(context, width, height) {
    context.setTransform(1, 0, 0, 1, 0, 0);
    context.clearRect(0, 0, width, height);
    context.fillStyle = HUD_COLORS.panel;
    context.fillRect(0, 0, width, height);
    context.fillStyle = HUD_COLORS.accent;
    context.fillRect(0, 0, width, Math.max(2, height * 0.012));
  }

  // drawGauge draws the speed arc of gauge, which gaugeView returns, around
  // (x, y) with the radius radius: a track, the speed, a tick at each
  // 20 km/h, and a mark at the limit.
  function drawGauge(context, x, y, radius, gauge) {
    const width = radius * 0.16, angle = (fraction) => GAUGE_START + GAUGE_SWEEP * Math.min(1, Math.max(0, fraction));
    context.lineCap = "butt";
    context.lineWidth = width;
    context.strokeStyle = HUD_COLORS.track;
    context.beginPath();
    context.arc(x, y, radius, GAUGE_START, GAUGE_START + GAUGE_SWEEP);
    context.stroke();
    context.strokeStyle = gauge.over ? HUD_COLORS.over : HUD_COLORS.accent;
    context.beginPath();
    context.arc(x, y, radius, GAUGE_START, angle(gauge.speed));
    context.stroke();
    const tick = (fraction, inner, outer, color, lineWidth) => {
      const a = angle(fraction);
      context.strokeStyle = color;
      context.lineWidth = lineWidth;
      context.beginPath();
      context.moveTo(x + Math.cos(a) * inner, y + Math.sin(a) * inner);
      context.lineTo(x + Math.cos(a) * outer, y + Math.sin(a) * outer);
      context.stroke();
    };
    for (let kmh = 0; kmh <= gauge.top; kmh += 20) tick(kmh / gauge.top, radius - width * 1.3, radius - width * 0.7, HUD_COLORS.dim, Math.max(1, width * 0.12));
    if (gauge.limit >= 0) tick(gauge.limit, radius - width * 0.8, radius + width * 0.8, HUD_COLORS.warn, Math.max(2, width * 0.3));
  }

  // drawSpeedScreen draws the middle cabin screen: the speed gauge on the
  // left, the speed in its middle, and the limit, the activity, and the pod
  // on the right.
  function drawSpeedScreen(context, width, height, view) {
    drawPanel(context, width, height);
    const radius = height * 0.36, x = height * 0.48, y = height * 0.55, pad = height * 0.06;
    drawGauge(context, x, y, radius, gaugeView(view.speedKmh / 3.6, view.limitKmh / 3.6));
    drawText(context, String(view.speedKmh), x, y + height * 0.08, radius * 1.5, height * 0.3, HUD_COLORS.text, "center", 800);
    drawText(context, "km/h", x, y + height * 0.22, radius, height * 0.08, HUD_COLORS.dim, "center");
    const left = x + radius + height * 0.14, right = width - pad, column = right - left;
    drawText(context, "LIMIT", left, height * 0.24, column, height * 0.07, HUD_COLORS.dim);
    drawText(context, view.limitKmh > 0 ? `${view.limitKmh} km/h` : "None", left, height * 0.38, column, height * 0.13, HUD_COLORS.warn, "left", 700);
    drawText(context, view.activity || view.state, left, height * 0.62, column, height * 0.1, HUD_COLORS.text);
    drawText(context, view.podName, left, height * 0.78, column, height * 0.08, HUD_COLORS.dim);
  }

  // drawLadder draws the stop ladder in the rectangle at (x, y) of width by
  // height. One leg is a bar from the origin to the next stop. A longer
  // ladder is a column of stops, with the bar of the pod between the last
  // stop behind it and the next stop.
  function drawLadder(context, x, y, width, height, ladder, size) {
    const bar = (bx, by, length, progress, vertical) => {
      const thickness = size * 0.35;
      context.fillStyle = HUD_COLORS.track;
      if (vertical) context.fillRect(bx - thickness / 2, by, thickness, length);
      else context.fillRect(bx, by - thickness / 2, length, thickness);
      if (progress < 0) return;
      context.fillStyle = HUD_COLORS.accent;
      if (vertical) context.fillRect(bx - thickness / 2, by, thickness, length * progress);
      else context.fillRect(bx, by - thickness / 2, length * progress, thickness);
    };
    const dot = (dx, dy, state, radius = size) => {
      context.beginPath();
      context.arc(dx, dy, radius * (state === "next" ? 0.42 : 0.3), 0, 2 * Math.PI);
      context.fillStyle = state === "done" ? HUD_COLORS.done : (state === "next" ? HUD_COLORS.accent : HUD_COLORS.dim);
      context.fill();
    };
    if (ladder.stops.length === 0) {
      drawText(context, "No stop ahead", x, y + size, width, size, HUD_COLORS.dim);
      return;
    }
    if (ladder.oneLeg) {
      const origin = ladder.stops.find((stop) => stop.state === "done"), next = ladder.stops.find((stop) => stop.state === "next");
      const middle = y + height * 0.42;
      bar(x + size * 0.4, middle, width - size * 0.8, ladder.progress, false);
      if (origin) dot(x + size * 0.4, middle, "done");
      if (next) dot(x + width - size * 0.4, middle, "next");
      drawText(context, origin ? nameChoices(origin.name) : "", x, middle + size * 1.6, width * 0.48, size * 0.85, HUD_COLORS.dim);
      drawText(context, next ? nameChoices(next.name) : "", x + width, middle + size * 1.6, width * 0.5, size, HUD_COLORS.text, "right", 700);
      return;
    }
    // With many stops, the rows get smaller, so they do not overlap.
    const items = ladderItems(ladder), step = height / items.length, track = x + size * 0.5, row = Math.min(size, step * 0.7);
    // A thin line joins the stops.
    context.fillStyle = HUD_COLORS.track;
    context.fillRect(track - size * 0.06, y + step / 2, size * 0.12, height - step);
    items.forEach((item, i) => {
      const top = y + step * i, middle = top + step / 2;
      if (item.bar) { bar(track, top, step, item.progress, true); return; }
      dot(track, middle, item.state, row);
      const color = item.state === "next" ? HUD_COLORS.text : HUD_COLORS.dim;
      drawText(context, nameChoices(item.name), x + size * 1.3, middle + row * 0.35, width - size * 1.3, row * (item.state === "next" ? 1 : 0.85), color, "left", item.state === "next" ? 700 : 500);
    });
  }

  // drawRouteScreen draws the right cabin screen: the stop ladder at the
  // top, then the wait reason with blockedBy, the riders aboard, and the
  // nearest stop. near is the name and the distance text of the nearest
  // stop, or null.
  function drawRouteScreen(context, width, height, view, ladder, near) {
    drawPanel(context, width, height);
    const pad = height * 0.07, size = height * 0.085, inner = width - 2 * pad;
    drawText(context, "ROUTE", pad, pad + size * 0.8, inner * 0.4, size * 0.75, HUD_COLORS.dim);
    drawText(context, view.distance, width - pad, pad + size * 0.8, inner * 0.58, size * 0.75, HUD_COLORS.dim, "right");
    drawLadder(context, pad, pad + size * 1.4, inner, height * 0.44, ladder, size);
    const lines = [
      view.waitReason ? [(measure, room) => waitText(view.waitReason, view.blockedBy, room, measure), HUD_COLORS.warn] : null,
      [view.aboard > 0 ? `${view.aboard} aboard` : "No riders aboard", HUD_COLORS.text],
      near ? [nameChoices(near.name).map((name) => `Near ${name}, ${near.distance}`), HUD_COLORS.dim] : null,
    ].filter(Boolean);
    lines.forEach(([text, color], i) => drawText(context, text, pad, height * 0.7 + i * size * 1.2, inner, size * 0.9, color));
  }

  // drawBanner draws the head-up banner on the windscreen: the next stop on
  // the left and the time to it on the right, on a dark band that lets the
  // view show through. The banner texture has alpha.
  function drawBanner(context, width, height, view) {
    context.setTransform(1, 0, 0, 1, 0, 0);
    context.clearRect(0, 0, width, height);
    const slant = height * 0.35;
    context.fillStyle = "rgba(10, 20, 29, 0.62)";
    context.beginPath();
    context.moveTo(slant, 0); context.lineTo(width, 0); context.lineTo(width - slant, height); context.lineTo(0, height);
    context.closePath();
    context.fill();
    context.fillStyle = HUD_COLORS.accent;
    context.fillRect(slant, 0, width - slant, height * 0.05);
    const pad = slant + height * 0.08, size = height * 0.3, split = width * 0.68;
    drawText(context, view.nextName ? "NEXT STOP" : "", pad, height * 0.32, split - pad, height * 0.16, HUD_COLORS.dim);
    drawText(context, view.nextName ? nameChoices(view.nextName) : (view.next || view.state), pad, height * 0.76, split - pad, size, HUD_COLORS.text, "left", 700);
    const right = width - slant - height * 0.08;
    // Past the route window, the time is the part in the window.
    drawText(context, view.time ? (view.over ? "ETA, OVER" : "ETA") : "", right, height * 0.32, width - split - slant, height * 0.16, HUD_COLORS.dim, "right");
    drawText(context, view.time, right, height * 0.76, width - split - slant, size, HUD_COLORS.accent, "right", 800);
  }

  // drawStationSign draws the station sign of the visual polish on a
  // canvas of width by height: the panel of the cabin screens, an accent
  // bar along the bottom, a stop mark, and the name. A name that does not
  // fit loses the part in brackets, then words at its end.
  function drawStationSign(context, width, height, name) {
    drawPanel(context, width, height);
    context.fillStyle = HUD_COLORS.accent;
    context.fillRect(0, height * 0.9, width, height * 0.1);
    const radius = height * 0.24;
    context.beginPath();
    context.arc(height * 0.5, height * 0.45, radius, 0, 2 * Math.PI);
    context.lineWidth = radius * 0.4;
    context.strokeStyle = HUD_COLORS.accent;
    context.stroke();
    drawText(context, nameChoices(name), height, height * 0.64, width - height * 1.15, height * 0.48, HUD_COLORS.text, "left", 700);
  }

  // attachTexture loads the image at url as a tiling texture and gives it
  // to each material when it loads. Until then, and for good when it does
  // not load, the materials keep their flat colors.
  function attachTexture(B, scene, url, materials) {
    // Babylon can call onLoad before the constructor returns, so the
    // materials get the texture when both have happened.
    let texture = null, loaded = false;
    const attach = () => {
      if (!texture || !loaded) return;
      for (const material of materials) material.diffuseTexture = texture;
    };
    texture = new B.Texture(url, scene, false, true, B.Texture.TRILINEAR_SAMPLINGMODE, () => { loaded = true; attach(); }, (message) => {
      console.warn(`The texture ${url} did not load, so the surfaces keep their flat colors.`, message);
      loaded = false;
      if (texture) texture.dispose();
    });
    texture.wrapU = texture.wrapV = B.Texture.WRAP_ADDRESSMODE;
    texture.anisotropicFilteringLevel = TEXTURE_ANISOTROPY;
    attach();
    return texture;
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

  // loadPodModel fetches and reads web/pod.glb. It returns null when it
  // cannot, and the page then draws procedural pods.
  async function loadPodModel() {
    try {
      const response = await fetch("pod.glb");
      if (!response.ok) throw new Error(`HTTP status ${response.status}`);
      return parsePodModel(await response.arrayBuffer());
    } catch (error) {
      console.warn("The pod model did not load, so the pods are procedural.", error);
      return null;
    }
  }

  function color(B, rgb) { return new B.Color3(rgb[0], rgb[1], rgb[2]); }

  // modelMesh makes a mesh from a part of the pod model, stretched to the
  // class length.
  function modelMesh(B, scene, name, part, cls, model) {
    const mesh = new B.Mesh(name, scene);
    const data = new B.VertexData();
    const { positions, normals } = stretchPart(part, cls.lengthMeters / model.length, model.length);
    data.positions = positions; data.normals = normals; data.indices = part.indices;
    // The texture stretches with the part.
    if (part.uvs) data.uvs = part.uvs;
    data.applyToMesh(mesh);
    return mesh;
  }

  // proceduralPodParts makes the shell, the glass, and the gear of a class
  // when the page has no pod model.
  function proceduralPodParts(B, scene, cls) {
    const length = cls.lengthMeters, radius = POD_WIDTH / 2;
    const shell = B.MeshBuilder.CreateCapsule(`shell-${cls.name}`, { orientation: new B.Vector3(0, 0, 1), height: length, radius, tessellation: 20, subdivisions: 4, capSubdivisions: 6 }, scene);
    shell.scaling.y = (POD_HEIGHT - 0.25) / POD_WIDTH;
    shell.position.y = 0.12;
    shell.bakeCurrentTransformIntoVertices();
    const glass = B.MeshBuilder.CreateBox(`glass-${cls.name}`, { width: POD_WIDTH + 0.06, height: 0.62, depth: Math.max(1, length - 0.9) }, scene);
    glass.position.y = 0.32;
    glass.bakeCurrentTransformIntoVertices();
    const gear = B.MeshBuilder.CreateBox(`gear-${cls.name}`, { width: BEAM_WIDTH * 0.8, height: 0.3, depth: length * 0.7 }, scene);
    gear.position.y = -POD_HEIGHT / 2 + 0.15;
    gear.bakeCurrentTransformIntoVertices();
    return [shell, glass, gear];
  }

  // buildPodMesh makes one pod body for a class: a shell in the class
  // color, dark glass, and a running gear on the beam. With a pod model, the
  // parts come from the model, stretched to the class length. The parts
  // merge into one mesh with one submesh for each material, so thin
  // instances draw a class in a few calls. With a sky cube texture, the
  // shell and the glass reflect the sky, most at a low angle, as paint and
  // glass do. With a light texture of podLightTexels, the shell of the
  // pod model shows its lights.
  function buildPodMesh(B, scene, cls, rgb, model, skyCube, lights) {
    const [shell, glass, gear] = model
      ? ["shell", "glass", "gear"].map((name) => modelMesh(B, scene, `${name}-${cls.name}`, model.parts[name], cls, model))
      : proceduralPodParts(B, scene, cls);
    const shellMaterial = new B.StandardMaterial(`shell-${cls.name}`, scene);
    shellMaterial.diffuseColor = color(B, rgb);
    shellMaterial.specularColor = new B.Color3(0.55, 0.55, 0.55);
    shellMaterial.specularPower = 48;
    shell.material = shellMaterial;

    const glassMaterial = new B.StandardMaterial(`glass-${cls.name}`, scene);
    glassMaterial.diffuseColor = color(B, model ? model.parts.glass.color : [0.05, 0.08, 0.12]);
    glassMaterial.specularColor = new B.Color3(0.9, 0.95, 1);
    glassMaterial.specularPower = 96;
    glass.material = glassMaterial;
    if (skyCube) {
      // The reflection adds to the lit color, and the result is clipped at
      // white, so the light shells reflect little and only at a low angle.
      shellMaterial.reflectionTexture = skyCube;
      shellMaterial.reflectionFresnelParameters = fresnel(B, 0.3, 0.1);
      glassMaterial.reflectionTexture = skyCube;
      glassMaterial.reflectionFresnelParameters = fresnel(B, 0.85, 0.2);
    }
    if (model && lights) {
      // The lights add to the lit color, so shade and the dark stripe do
      // not dim them.
      shellMaterial.emissiveTexture = lights;
      shellMaterial.useEmissiveAsIllumination = true;
    }

    const gearMaterial = new B.StandardMaterial(`gear-${cls.name}`, scene);
    gearMaterial.diffuseColor = color(B, model ? model.parts.gear.color : [0.16, 0.17, 0.19]);
    gearMaterial.specularColor = new B.Color3(0.1, 0.1, 0.1);
    gear.material = gearMaterial;

    const mesh = B.Mesh.MergeMeshes([shell, glass, gear], true, true, undefined, false, true);
    mesh.name = `pod-${cls.name}`;
    mesh.isPickable = false;
    // The instances move every frame and cover the whole network.
    mesh.alwaysSelectAsActiveMesh = true;
    return { mesh, shellMaterial };
  }

  // fresnel returns the Fresnel parameters of a reflection that is level
  // strong at a grazing angle and facing times as strong head on. Babylon
  // mixes the two with the weight cos^power of the angle to the normal, so
  // with power 0.5, the reflection falls fast from a grazing angle: to
  // half its range at 75 degrees from the normal.
  function fresnel(B, level, facing) {
    const parameters = new B.FresnelParameters();
    parameters.leftColor = new B.Color3(level, level, level);
    parameters.rightColor = new B.Color3(level * facing, level * facing, level * facing);
    parameters.bias = 0;
    parameters.power = 0.5;
    return parameters;
  }

  // buildModelCabin makes the inside of a pod of a class from the pod
  // model: the cabin and the seats, merged into one mesh with one submesh
  // for each material. The seat view places it at the ridden pod. It
  // returns the mesh and the materials by part name.
  function buildModelCabin(B, scene, cls, model) {
    const names = ["cabin", "seat"].filter((name) => model.parts[name]), materials = {};
    const meshes = names.map((name) => {
      const mesh = modelMesh(B, scene, `${name}-${cls.name}`, model.parts[name], cls, model);
      const material = new B.StandardMaterial(`${name}-${cls.name}`, scene);
      material.diffuseColor = color(B, model.parts[name].color);
      // The windows light the cabin. The emissive color stands in for that
      // light, because the sun does not light the cabin.
      material.emissiveColor = color(B, model.parts[name].color.map((value) => value * 0.8));
      material.specularColor = new B.Color3(0.1, 0.1, 0.1);
      mesh.material = material;
      materials[name] = material;
      return mesh;
    });
    const mesh = meshes.length > 1 ? B.Mesh.MergeMeshes(meshes, true, true, undefined, false, true) : meshes[0];
    mesh.name = `cabin-${cls.name}`;
    mesh.isPickable = false;
    mesh.setEnabled(false);
    return { mesh, materials };
  }

  // createCabinScreens makes a mesh with a canvas texture for each screen of
  // the pod model. The screens are emissive and unlit, so they stay
  // readable in shade and at night. The banner texture has alpha, so the
  // view shows through it.
  function createCabinScreens(B, scene, model) {
    const screens = new Map();
    for (const [name, node] of Object.entries(model.screens)) {
      const width = Math.max(64, Math.round(node.scale[0] * SCREEN_PIXELS_PER_METER));
      const height = Math.max(64, Math.round(node.scale[1] * SCREEN_PIXELS_PER_METER));
      const texture = new B.DynamicTexture(`screen-${name}`, { width, height }, scene, true);
      const material = new B.StandardMaterial(`screen-${name}`, scene);
      texture.hasAlpha = name === "banner";
      material.diffuseTexture = texture;
      material.useAlphaFromDiffuseTexture = texture.hasAlpha;
      // With lighting off, the output is the emissive color times the
      // texture.
      material.disableLighting = true;
      material.emissiveColor = new B.Color3(1, 1, 1);
      material.specularColor = new B.Color3(0, 0, 0);
      material.fogEnabled = false;
      const mesh = new B.Mesh(`screen-${name}`, scene);
      mesh.material = material;
      mesh.isPickable = false;
      mesh.setEnabled(false);
      screens.set(name, { node, mesh, texture, width, height, stretch: NaN });
    }
    // place shows the screens in cabin, a cabin mesh stretched to stretch
    // times the model length. The screens are not children of the cabin,
    // so a rebuild disposes each mesh once.
    function place(cabin, stretch) {
      for (const screen of screens.values()) {
        if (screen.stretch !== stretch) {
          const data = new B.VertexData();
          Object.assign(data, screenQuad(screen.node, stretch, model.length));
          data.applyToMesh(screen.mesh);
          screen.stretch = stretch;
        }
        screen.mesh.position.copyFrom(cabin.position);
        screen.mesh.rotation.y = cabin.rotation.y;
        screen.mesh.setEnabled(true);
      }
    }
    function hide() {
      for (const screen of screens.values()) screen.mesh.setEnabled(false);
    }
    // draw calls paint(context, width, height) on the canvas of the screen
    // name and shows the result.
    function draw(name, paint) {
      const screen = screens.get(name);
      if (!screen) return;
      paint(screen.texture.getContext(), screen.width, screen.height);
      screen.texture.update();
    }
    return { place, hide, draw, meshes: [...screens.values()].map((screen) => screen.mesh) };
  }

  // buildWorld makes the static meshes of a topology and the pod meshes.
  // casters cast shadows, and receivers show them. With a pod model, each
  // class also gets a cabin, and the world gets the cabin screens. The
  // textures load in the background, and the surfaces keep their flat
  // colors until then. look holds the switches of lookOptions, sky the
  // colors of skyLook, and toSun the unit vector toward the sun.
  function buildWorld(B, scene, topology, model, look, sky, toSun) {
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
    // The round joins replace the lane ends at their nodes.
    const plan = filletPlan(topology.lanes, topology.pieces, hubLanes, nodes);
    const lanes = uniqueLanes(plan.lanes).filter((lane) => !hubLanes.has(lane.id)), levels = topology.levels;
    const chunks = [];
    for (let start = 0; start < lanes.length; start += LANES_PER_CHUNK) chunks.push([`beams${start}`, lanes.slice(start, start + LANES_PER_CHUNK), []]);
    for (let start = 0; start < plan.pieces.length; start += LANES_PER_CHUNK) chunks.push([`joins${start}`, [], plan.pieces.slice(start, start + LANES_PER_CHUNK)]);
    const railProfiles = [];
    for (const [name, chunkLanes, chunkPieces] of chunks) {
      const profiles = guidewayProfiles(chunkLanes, nodes, levels, chunkPieces);
      if (look.rail) railProfiles.push(...profiles);
      const geometry = profileBeams(profiles, look.taper ? BEAM_BOTTOM_WIDTH : BEAM_WIDTH);
      if (geometry.indices.length === 0) continue;
      const mesh = new B.Mesh(name, scene);
      const data = new B.VertexData();
      data.positions = geometry.positions; data.normals = geometry.normals; data.colors = geometry.colors; data.uvs = geometry.uvs; data.indices = geometry.indices;
      data.applyToMesh(mesh);
      mesh.material = beamMaterial;
      mesh.isPickable = false;
      mesh.freezeWorldMatrix();
      meshes.push(mesh); casters.push(mesh); receivers.push(mesh);
    }
    // The rails of all beams are one mesh, so they add one draw call. The
    // rail is unlit, and it does not cast or receive shadows.
    const rail = railGeometry(railProfiles);
    if (rail.indices.length > 0) {
      const mesh = new B.Mesh("rail", scene);
      const data = new B.VertexData();
      data.positions = rail.positions; data.indices = rail.indices;
      data.applyToMesh(mesh);
      const material = new B.StandardMaterial("rail", scene);
      material.disableLighting = true;
      material.emissiveColor = color(B, RAIL_COLOR);
      material.specularColor = new B.Color3(0, 0, 0);
      // Far from the camera, the depth buffer has less precision than the
      // rise of the rail, so the rail moves a little toward the camera.
      material.zOffset = -1;
      mesh.material = material;
      mesh.isPickable = false;
      mesh.freezeWorldMatrix();
      meshes.push(mesh);
    }
    for (const [index, hub] of hubs.entries()) {
      const geometry = deckGeometry(hub.hull, BEAM_TOP - HUB_DECK_DROP);
      if (geometry.indices.length === 0) continue;
      const mesh = new B.Mesh(`hub${index}`, scene);
      const data = new B.VertexData();
      data.positions = geometry.positions; data.normals = geometry.normals; data.colors = geometry.colors; data.uvs = geometry.uvs; data.indices = geometry.indices;
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
    // The cylinder texture goes once around a pillar, so it has no seam.
    // The height gets the same scale as the mean circumference, so the
    // concrete is not stretched, at about 2 m for each repeat.
    const pillarUVs = pillar.getVerticesData(B.VertexBuffer.UVKind);
    for (let i = 1; i < pillarUVs.length; i += 2) pillarUVs[i] *= (BEAM_TOP - BEAM_DEPTH) / (Math.PI * 0.65);
    pillar.setVerticesData(B.VertexBuffer.UVKind, pillarUVs);
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
      setConcreteUVs(B, mesh);
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

    const textured = { shell: [], cabin: [], seat: [] };
    const skyCube = look.gloss ? new B.RawCubeTexture(scene, skyCubeFaces(SKY_CUBE_SIZE, sky, toSun), SKY_CUBE_SIZE, B.Engine.TEXTUREFORMAT_RGBA, B.Engine.TEXTURETYPE_UNSIGNED_BYTE, true, false, B.Texture.TRILINEAR_SAMPLINGMODE) : null;
    if (skyCube) skyCube.coordinatesMode = B.Texture.CUBIC_MODE;
    const lights = look.lights ? new B.RawTexture(podLightTexels(64, 512), 64, 512, B.Engine.TEXTUREFORMAT_RGBA, scene, true, false, B.Texture.TRILINEAR_SAMPLINGMODE) : null;
    if (lights) {
      // u = z of the model, from -POD_LIGHT_SPAN / 2 to POD_LIGHT_SPAN / 2,
      // goes to 0 to 1.
      lights.uScale = 1 / POD_LIGHT_SPAN;
      lights.uOffset = 0.5;
      lights.wrapU = lights.wrapV = B.Texture.CLAMP_ADDRESSMODE;
    }
    const pods = topology.classes.map((cls, index) => {
      const { mesh, shellMaterial } = buildPodMesh(B, scene, cls, CLASS_COLORS[index % CLASS_COLORS.length], model, skyCube, lights);
      meshes.push(mesh); casters.push(mesh);
      const cabin = model ? buildModelCabin(B, scene, cls, model) : null;
      if (cabin) {
        meshes.push(cabin.mesh);
        textured.shell.push(shellMaterial);
        for (const [name, material] of Object.entries(cabin.materials)) textured[name].push(material);
      }
      return { mesh, cabin: cabin ? cabin.mesh : null, length: cls.lengthMeters, matrices: new Float32Array(0), count: 0 };
    });
    const screens = model && Object.keys(model.screens).length > 0 ? createCabinScreens(B, scene, model) : null;
    if (screens) meshes.push(...screens.meshes);
    // The procedural pods have no texture coordinates of the pod model, so
    // only the model gets the pod textures.
    const textures = [
      attachTexture(B, scene, "textures/concrete.png", [beamMaterial, concrete, slabMaterial]),
      attachTexture(B, scene, "textures/panel.png", textured.shell),
      attachTexture(B, scene, "textures/dash.png", textured.cabin),
      attachTexture(B, scene, "textures/fabric.png", textured.seat),
      ...[skyCube, lights].filter(Boolean),
    ];
    return { meshes, casters, receivers, pods, slabs, screens, textures };
  }

  // setConcreteUVs gives mesh texture coordinates in meters for the
  // concrete texture, by a box projection of its local positions.
  function setConcreteUVs(B, mesh) {
    const positions = mesh.getVerticesData(B.VertexBuffer.PositionKind), normals = mesh.getVerticesData(B.VertexBuffer.NormalKind);
    mesh.setVerticesData(B.VertexBuffer.UVKind, boxUVs(positions, normals, CONCRETE_TILE));
  }

  // buildCabin makes the window frame and the dashboard of the seat when
  // the page has no pod model. They move with the camera.
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

  // skyLook returns the colors of the sky: the horizon, which is also the
  // fog color, the top, the glow around the sun or null, and the color
  // below the horizon. With the sky switch off, it is the sky from before
  // the visual polish.
  function skyLook(polish) {
    if (!polish.sky) return { horizon: SKY, top: SKY_TOP, glow: null, below: SKY };
    if (polish.time === "dusk") return { horizon: DUSK_HAZE, top: DUSK_TOP, glow: DUSK_GLOW, below: DUSK_HAZE };
    return { horizon: HAZE, top: HAZE_TOP, glow: SUN_GLOW, below: HAZE };
  }

  // skyColor returns the color of the sky in a scene direction, a unit
  // vector [x, y, z]. sky comes from skyLook, and toSun is the unit vector
  // toward the sun. Above the horizon, the color goes from the horizon
  // color to the top color, with the glow added near the sun. The glow
  // fades in above the horizon, so the horizon keeps the fog color. Below
  // the horizon, the color goes to the below color within about 7 degrees.
  function skyColor(direction, sky, toSun) {
    const [x, y, z] = direction;
    if (y <= 0) {
      const f = Math.min(1, -y * 8);
      return sky.horizon.map((value, c) => value + (sky.below[c] - value) * f);
    }
    const t = Math.sqrt(Math.min(1, y));
    const base = sky.horizon.map((value, c) => value + (sky.top[c] - value) * t);
    if (!sky.glow) return base;
    const cos = Math.max(0, x * toSun[0] + y * toSun[1] + z * toSun[2]);
    const lift = Math.min(1, y / 0.1), fade = lift * lift * (3 - 2 * lift);
    const glow = (0.35 * cos ** 32 + 0.12 * cos ** 4) * fade;
    return base.map((value, c) => Math.min(1, value + sky.glow[c] * glow));
  }

  // cubeDirection returns the unit scene direction through the point s, t
  // of a face of a cube texture, with s and t from 0 to 1. The faces are
  // in the order +x, -x, +y, -y, +z, -z. s grows along a row and t from
  // the first row to the last, with the orientation of WebGL, which
  // Babylon also uses to read a cube face.
  function cubeDirection(face, s, t) {
    const a = 2 * s - 1, b = 2 * t - 1;
    const direction = [[1, -b, -a], [-1, -b, a], [a, 1, b], [a, -1, -b], [a, -b, 1], [-a, -b, -1]][face];
    const length = Math.hypot(...direction);
    return direction.map((value) => value / length);
  }

  // skyCubeFaces returns the six faces of a cube texture of the sky of
  // skyColor, in the order of cubeDirection: size by size RGBA bytes, the
  // first row first. Below the horizon, the sky goes to GROUND_SHINE, so a
  // pod reflects the ground there.
  function skyCubeFaces(size, sky, toSun) {
    const look = { ...sky, below: GROUND_SHINE };
    return Array.from({ length: 6 }, (_, face) => {
      const data = new Uint8Array(size * size * 4);
      for (let t = 0; t < size; t++) {
        for (let s = 0; s < size; s++) {
          const rgb = skyColor(cubeDirection(face, (s + 0.5) / size, (t + 0.5) / size), look, toSun);
          data.set([...rgb.map((value) => Math.round(value * 255)), 255], (t * size + s) * 4);
        }
      }
      return data;
    });
  }

  // podLightTexels returns the RGBA bytes of the light texture of the pod
  // shell, width by height, the first row first. Column c is at z = (c +
  // 0.5) / width * POD_LIGHT_SPAN - POD_LIGHT_SPAN / 2 of the model, and
  // row r is (r + 0.5) / height * POD_HEIGHT - POD_HEIGHT / 2 above the
  // middle of the livery stripe. The front nose has a white band, the rear
  // nose a red band, and the sides a thin accent line between them. The
  // rows are symmetric, so the texture does not depend on the direction of
  // v.
  function podLightTexels(width, height) {
    const data = new Uint8Array(width * height * 4);
    for (let r = 0; r < height; r++) {
      const dy = Math.abs((r + 0.5) / height - 0.5) * POD_HEIGHT;
      for (let c = 0; c < width; c++) {
        const z = ((c + 0.5) / width - 0.5) * POD_LIGHT_SPAN;
        let rgb = [0, 0, 0];
        if (z >= POD_LIGHT_NOSE && dy <= POD_LIGHT_BAND) rgb = HEAD_LIGHT;
        else if (z <= -POD_LIGHT_NOSE && dy <= POD_LIGHT_BAND) rgb = TAIL_LIGHT;
        else if (Math.abs(z) < POD_LIGHT_NOSE && dy <= POD_LIGHT_LINE) rgb = ACCENT_LIGHT;
        data.set([...rgb.map((value) => Math.round(value * 255)), 255], (r * width + c) * 4);
      }
    }
    return data;
  }

  // buildSky makes a sky dome with the colors of skyColor in vertex
  // colors.
  function buildSky(B, scene, sky, toSun) {
    // The glow needs more vertices than the plain gradient.
    const dome = B.MeshBuilder.CreateSphere("sky", { diameter: 2 * FOG_END, segments: sky.glow ? 48 : 24, sideOrientation: B.Mesh.BACKSIDE }, scene);
    const positions = dome.getVerticesData(B.VertexBuffer.PositionKind);
    const colors = new Float32Array(positions.length / 3 * 4);
    for (let i = 0; i < positions.length / 3; i++) {
      const x = positions[i * 3], y = positions[i * 3 + 1], z = positions[i * 3 + 2], length = Math.hypot(x, y, z) || 1;
      colors.set([...skyColor([x / length, y / length, z / length], sky, toSun), 1], i * 4);
    }
    dome.setVerticesData(B.VertexBuffer.ColorKind, colors);
    const material = new B.StandardMaterial("sky", scene);
    // With lighting off, the output is the emissive color times the vertex
    // color, so the emissive color is white.
    material.disableLighting = true;
    material.emissiveColor = new B.Color3(1, 1, 1);
    material.diffuseColor = new B.Color3(1, 1, 1);
    material.fogEnabled = false;
    dome.material = material;
    asGroundLayer(dome, GROUND_LAYERS.sky);
    dome.infiniteDistance = true;
    dome.isPickable = false;
    return dome;
  }

  // createTileLayer drapes map tiles on the ground around the camera when the
  // project has a geographic reference. Unlike the 2D view, it does not need
  // a map background setting, but it refuses a map setting that is not valid.
  // With look.tint, the tiles get TILE_TINT.
  function createTileLayer(B, scene, topology, credit, look) {
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
        if (look.tint) material.diffuseColor = color(B, TILE_TINT);
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
  // stations nearest the camera. With polished, the signs have the style of
  // drawStationSign. A sign draws its texture again only when the name that
  // it shows changes. A project apply can rename a station and keep its ID.
  function createSigns(B, scene, polished) {
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
      signs.push({ mesh, texture, name: "" });
    }
    function update(slabs, x, z) {
      const nearest = nearestSlabs(slabs, x, z, SIGN_COUNT, SIGN_RADIUS);
      signs.forEach((sign, i) => {
        const slab = nearest[i];
        sign.mesh.setEnabled(!!slab);
        if (!slab) { sign.name = ""; return; }
        sign.mesh.position.set(slab.x, slab.height + 4.2, slab.z);
        const name = slab.name || slab.id;
        if (sign.name === name) return;
        sign.name = name;
        const context = sign.texture.getContext();
        if (polished) {
          drawStationSign(context, 768, 128, name);
          sign.texture.update();
          return;
        }
        context.fillStyle = "#123a5e"; context.fillRect(0, 0, 768, 128);
        context.fillStyle = "#ffffff"; context.font = "bold 64px system-ui, sans-serif";
        context.textAlign = "center"; context.textBaseline = "middle";
        context.fillText(name, 384, 66, 740);
        sign.texture.update();
      });
    }
    return { update };
  }

  // createMiniMap draws the lanes, the stations, and the pods around the
  // ridden pod on a canvas: the round map of the overlay or the map screen
  // of the cabin.
  function createMiniMap(topology) {
    const nodes = new Map(topology.nodes.map((node) => [node.id, node.position]));
    const index = segmentIndex(topology.lanes, nodes, MINIMAP_CELL);
    const stations = stopPoints(topology.stations).map((station) => ({ ...station, label: shortLabel(station.name, MINIMAP_LABEL_LENGTH) }));
    // widths holds the width of each label text at the font of fontRatio.
    const widths = new Map();
    let fontRatio = 0;
    const classColors = CLASS_COLORS.map((rgb) => `rgb(${rgb.map((value) => Math.round(value * 255)).join(",")})`);

    // draw shows the map around world (x, y), with north up, on context, in
    // a square of size pixels. ratio scales the lines, the dots, and the
    // text: 1 is the size of the overlay map of MINIMAP_SIZE CSS pixels.
    // With a finite heading, an arrow at the center shows the ridden pod,
    // and the pose index ridden draws no dot. The pose index marked draws
    // as a large white dot. An index of -1 is no pod.
    function draw(context, size, ratio, x, y, heading, poses, count, ridden, marked) {
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
    const modelLoad = loadPodModel();
    let ride;
    try { ride = await loadModule(() => show("The ride module stopped. Reload the page.")); } catch (error) { show(error.message); return; }

    const canvas = document.getElementById("scene");
    // useLargeWorldRendering draws relative to the camera, so a far network
    // does not make the cabin jitter.
    const engine = new B.Engine(canvas, true, { useLargeWorldRendering: true });
    const scene = new B.Scene(engine);
    scene.setRenderingOrder(0, groundLayerOrder);
    const look = lookOptions(root.location.search), sky = skyLook(look);
    scene.clearColor = new B.Color4(sky.horizon[0], sky.horizon[1], sky.horizon[2], 1);
    scene.fogColor = color(B, sky.horizon);
    if (look.sky) {
      scene.fogMode = B.Scene.FOGMODE_EXP2;
      scene.fogDensity = HAZE_DENSITY;
    } else {
      scene.fogMode = B.Scene.FOGMODE_LINEAR;
      scene.fogStart = FOG_START;
      scene.fogEnd = FOG_END;
    }
    scene.skipPointerMovePicking = true;
    const light = lightLook(look.time);
    const sunDirection = B.Vector3.FromArray(light.fromSun);
    const sun = new B.DirectionalLight("sun", sunDirection, scene);
    sun.intensity = light.sunIntensity;
    sun.diffuse = color(B, light.sun);
    const fill = new B.HemisphericLight("fill", new B.Vector3(0, 1, 0), scene);
    fill.intensity = light.fillIntensity;
    fill.diffuse = color(B, light.fill);
    fill.groundColor = color(B, light.fillGround);
    const toSun = sunDirection.negate().asArray();
    buildSky(B, scene, sky, toSun);
    const camera = new B.FreeCamera("seat", new B.Vector3(0, BEAM_TOP + EYE_HEIGHT, 0), scene);
    // The nearest cabin part is about 0.5 m from the eye. A larger near plane keeps
    // more depth precision for the far ground.
    camera.minZ = 0.3;
    camera.maxZ = FOG_END + 200;
    // The shadow map covers a square that follows the camera.
    sun.autoUpdateExtends = false;
    sun.shadowFrustumSize = SHADOW_FRUSTUM;
    sun.shadowMinZ = 1;
    sun.shadowMaxZ = 800;
    const shadows = new B.ShadowGenerator(SHADOW_MAP_SIZE, sun);
    shadows.usePercentageCloserFiltering = true;
    shadows.filteringQuality = B.ShadowGenerator.QUALITY_HIGH;
    shadows.bias = 0.0008;
    camera.fov = CAMERA_FOV;
    camera.rotation.x = SEAT_PITCH;
    // Without the pod model, the seat view draws a frame that moves with
    // the camera.
    const podModel = await modelLoad;
    const cabin = podModel ? [] : buildCabin(B, scene, camera);
    const pipeline = new B.DefaultRenderingPipeline("look", true, scene, [camera]);
    pipeline.samples = 4;
    pipeline.fxaaEnabled = true;
    pipeline.imageProcessingEnabled = true;
    pipeline.imageProcessing.toneMappingEnabled = true;
    pipeline.imageProcessing.toneMappingType = B.ImageProcessingConfiguration.TONEMAPPING_ACES;
    pipeline.imageProcessing.exposure = 1.1;
    pipeline.imageProcessing.contrast = 1.15;
    // Bloom spread the bright map tiles into soft blotches on the dark cabin
    // pillars, so the pipeline has no bloom.
    pipeline.bloomEnabled = false;
    if (look.fx) {
      // The vignette and the color curves are part of the image processing
      // pass that the pipeline has already, so they add no pass.
      pipeline.imageProcessing.vignetteEnabled = true;
      pipeline.imageProcessing.vignetteWeight = FX_VIGNETTE;
      const curves = new B.ColorCurves();
      curves.globalSaturation = FX_SATURATION;
      pipeline.imageProcessing.colorCurvesEnabled = true;
      pipeline.imageProcessing.colorCurves = curves;
    }
    const signs = createSigns(B, scene, look.signs);
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
    const miniMapCanvas = document.getElementById("minimap"), miniMapContext = miniMapCanvas.getContext("2d");
    // mode is where the readouts show, from hudMode. screenKey is the data
    // of the last draw of the readouts, so a draw happens only on a change.
    let mode = "overlay", screenKey = "";
    // focus is the world point that the view shows: the ridden pod, or the
    // station of the platform camera.
    let miniMap = null, lastMap = 0, stops = [], focus = { x: 0, y: 0, known: false };
    autoBox.checked = params.get("auto") === "1";
    const fader = createFader(root, fade, FADE_MS), autoMode = createAutoMode(fader, autoBox.checked);
    // The passenger flow. trip is the tripView of the last HUD update, and
    // platform is the station of the platform camera, or null.
    const tripButton = document.getElementById("tripButton"), tripMenu = document.getElementById("tripMenu");
    const tripElements = Object.fromEntries(["tripOrigin", "tripDestination", "tripParty", "tripShared", "tripMessage", "hudPassenger"].map((id) => [id, document.getElementById(id)]));
    let trip = null, tripPhase = "", arrivedAt = 0, platform = null, stationNames = new Map(), stationBerths = new Map(), partyLimit = 8;
    const hudElements = Object.fromEntries(["hudSpeed", "hudLimit", "hudState", "hudWait", "hudTrip", "hudNext", "hudEta", "hudRoute", "hudStops", "hudProgress", "hudProgressBar", "hudNear", "gauge", "stripSession", "stripMessage", "rideAt1x"].map((id) => [id, document.getElementById(id)]));

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
      autoMode.forget();
    }

    // The seat view draws the cabin frame of the procedural pods. The chase
    // and the platform views do not.
    function showCabin() {
      for (const part of cabin) part.setEnabled(!chase && !platform);
    }

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
      if (!fader.busy() && ((platform && platform.id) !== nextPlatform || nextPod !== requested)) {
        const change = () => {
          platform = nextPlatform ? { id: nextPlatform, ...platformView(stationBerths.get(nextPlatform).berths, stationBerths.get(nextPlatform).height) } : null;
          if (nextPod !== requested) selectPod(nextPod);
          showCabin();
        };
        // A move of the camera fades. A new pod for the platform view does not.
        if ((platform && platform.id) !== nextPlatform || !platform) fader.fadeTo(change); else change();
      }
      tripButton.textContent = trip ? (trip.active ? "Leave trip" : "Close trip") : "Trip…";
      picker.disabled = autoBox.disabled = Boolean(trip);
      tripElements.hudPassenger.textContent = trip ? trip.text : "";
    }

    picker.addEventListener("change", () => selectPod(picker.value));
    autoBox.addEventListener("change", () => {
      autoMode.arm(autoBox.checked);
      setParam("auto", autoBox.checked ? "1" : null);
    });
    picker.addEventListener("focus", () => refreshPods());

    function refreshPods() {
      podList = JSON.parse(ride.pods() || "[]");
      const chosen = choosePod(podList, requested);
      if (chosen !== requested) { resetTurns(); autoMode.forget(); }
      requested = chosen;
      picker.replaceChildren(...podList.map((pod) => new Option(podLabel(pod), pod.id, false, pod.id === requested)));
    }

    function rebuild() {
      const text = ride.topology();
      if (!text) return false;
      const topology = JSON.parse(text);
      if (world) {
        for (const mesh of world.meshes) mesh.dispose(false, true);
        for (const texture of world.textures) texture.dispose();
      }
      if (tileLayer) tileLayer.dispose();
      world = buildWorld(B, scene, topology, podModel, look, sky, toSun);
      // The sun does not light a cabin, because no roof shadows the floor.
      sun.excludedMeshes = world.pods.flatMap((pod) => (pod.cabin ? [pod.cabin] : []));
      shadows.getShadowMap().renderList = world.casters;
      for (const mesh of world.receivers) mesh.receiveShadows = true;
      tileLayer = createTileLayer(B, scene, topology, credit, look);
      miniMap = createMiniMap(topology);
      screenKey = "";
      stops = stopPoints(topology.stations);
      stationNames = new Map(topology.stations.map((station) => [station.id, station.name || station.id]));
      stationBerths = new Map(topology.stations.filter((station) => station.berths.length > 0).map((station) => [station.id, station]));
      partyLimit = topology.partyLimit || 8;
      // A pending fade can read a station of the old topology.
      autoMode.cancel();
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
      // The seat view draws the cabin of the ridden pod, and in the cabin
      // mode, its screens.
      const seatClass = !platform && !chase && index >= 0 ? poses[index * POSE_FIELDS + 4] | 0 : -1;
      world.pods.forEach((pod, i) => { if (pod.cabin) pod.cabin.setEnabled(i === seatClass); });
      if (world.screens && mode !== "cabin") world.screens.hide();
      if (!looking) {
        lookYaw = springBack(lookYaw, seconds, LOOK_RETURN_SECONDS);
        lookPitch = springBack(lookPitch, seconds, LOOK_RETURN_SECONDS);
      }
      // The seat view widens with speed. The chase and the platform views
      // keep CAMERA_FOV.
      const fov = look.fov && seatClass >= 0 ? seatFov(poses[index * POSE_FIELDS + 3]) : CAMERA_FOV;
      camera.fov = fov + springBack(camera.fov - fov, seconds, FOV_SECONDS);
      if (platform) {
        camera.position.fromArray(platform.eye);
        camera.setTarget(B.Vector3.FromArray(platform.target));
        camera.rotation.x += lookPitch;
        camera.rotation.y += lookYaw;
        return;
      }
      if (index < 0) return;
      const at = index * POSE_FIELDS, pod = world.pods[poses[at + 4] | 0];
      const heading = poses[at + 2];
      const yaw = dampAngle(seatTurn, yawOf(heading), seconds, SEAT_YAW_SECONDS);
      if (chase) {
        const chaseYaw = dampAngle(chaseTurn, yawOf(heading), seconds, CHASE_YAW_SECONDS);
        const [x, y, z] = chaseEye(poses[at], poses[at + 1], chaseYaw + lookYaw, poses[at + 5]);
        camera.position.set(x, y, z);
        camera.rotation.set(CHASE_PITCH + lookPitch, chaseYaw + lookYaw, 0);
        return;
      }
      camera.rotation.set(SEAT_PITCH + lookPitch, yaw + lookYaw, 0);
      if (pod.cabin) {
        // The cabin and the eye turn with the seat yaw, so the cabin does
        // not move in the view.
        const [x, z] = toScene(poses[at], poses[at + 1]);
        pod.cabin.position.set(x, poses[at + 5] + POD_HEIGHT / 2, z);
        pod.cabin.rotation.y = yaw;
        if (mode === "cabin") world.screens.place(pod.cabin, pod.length / podModel.length);
        camera.position.fromArray(seatEye(x, poses[at + 5], z, yaw, podModel.eye, pod.length / podModel.length, podModel.length));
        return;
      }
      // The eye is near the front of the pod.
      const forward = pod.length / 2 - 0.6;
      const [x, z] = toScene(poses[at] + Math.cos(heading) * forward, poses[at + 1] + Math.sin(heading) * forward);
      camera.position.set(x, poses[at + 5] + EYE_HEIGHT, z);
    }

    function showHUD() {
      if (session === "") return;
      showTrip();
      // While the party waits for an assignment, the HUD shows no pod.
      const hud = JSON.parse(ride.hud(trip && !trip.podID && trip.active ? "" : requested) || "{}");
      const riders = hud.found ? hud.riders.length : -1;
      autoMode.update(autoBox.checked && !trip, riders, () => ride.nextPod(requested), selectPod);
      const view = hudView(hud);
      if (!hud.found && trip) view.state = "No pod is assigned yet";
      const ladder = stopLadder(hud, view.progress);
      const nearest = focus.known ? nearestStop(stops, focus.x, focus.y) : null;
      const near = nearest ? { name: nearest.name, distance: formatDistance(nearest.distance) } : null;
      hudElements.stripSession.textContent = view.session;
      hudElements.stripMessage.textContent = view.message;
      hudElements.rideAt1x.disabled = !view.rideAt1x;
      // The readouts draw only when their data or the mode changes.
      const key = JSON.stringify([mode, view, ladder, near]);
      if (key === screenKey) return;
      screenKey = key;
      if (mode === "cabin") {
        world.screens.draw("speedScreen", (context, width, height) => drawSpeedScreen(context, width, height, view));
        world.screens.draw("routeScreen", (context, width, height) => drawRouteScreen(context, width, height, view, ladder, near));
        world.screens.draw("banner", (context, width, height) => drawBanner(context, width, height, view));
        return;
      }
      showOverlay(view, ladder, near);
    }

    // showOverlay shows the readouts on the screen overlay of the chase and
    // the platform views.
    function showOverlay(view, ladder, near) {
      hudElements.hudSpeed.textContent = view.speed ? String(view.speedKmh) : "";
      hudElements.hudLimit.textContent = view.limitKmh > 0 ? `Limit ${view.limitKmh} km/h` : "";
      hudElements.hudState.textContent = view.state;
      hudElements.hudWait.textContent = view.wait;
      hudElements.hudTrip.replaceChildren(...view.trip.map((line) => Object.assign(document.createElement("div"), { textContent: line })));
      hudElements.hudNext.textContent = view.nextName || view.next;
      hudElements.hudNext.classList.toggle("noStop", !view.nextName);
      hudElements.hudEta.textContent = view.eta;
      hudElements.hudRoute.textContent = view.distance;
      // The progress bar of the route stands between the stops behind the
      // pod and the next stop. With no stops, the ladder is empty and hides.
      hudElements.hudStops.replaceChildren(...(ladder.stops.length === 0 ? [] : ladderItems(ladder)).map((item) => {
        if (item.bar) return hudElements.hudProgress;
        return Object.assign(document.createElement("span"), { textContent: withoutBrackets(item.name), title: item.name, className: item.state });
      }));
      hudElements.hudStops.classList.toggle("oneLeg", ladder.oneLeg);
      hudElements.hudProgress.hidden = ladder.stops.length === 0;
      hudElements.hudProgressBar.style.width = `${(Math.max(0, view.progress) * 100).toFixed(1)}%`;
      hudElements.hudProgress.setAttribute("aria-valuenow", String(Math.round(Math.max(0, view.progress) * 100)));
      hudElements.hudNear.textContent = near ? `Near ${near.name}, ${near.distance}` : "";
      const ratio = root.devicePixelRatio || 1, canvas = hudElements.gauge, size = Math.round(canvas.clientWidth * ratio) || 1;
      if (canvas.width !== size) canvas.width = canvas.height = size;
      const context = canvas.getContext("2d");
      context.clearRect(0, 0, size, size);
      if (view.speed) drawGauge(context, size / 2, size / 2, size * 0.42, gaugeView(view.speedKmh / 3.6, view.limitKmh / 3.6));
    }

    // setMode moves the readouts between the cabin screens and the screen
    // overlay. The overlay and its mini-map hide in the cabin mode.
    function setMode(next) {
      if (next === mode) return;
      mode = next;
      document.body.classList.toggle("cabin", mode === "cabin");
      screenKey = "";
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
      setMode(hudMode(chase, Boolean(platform), Boolean(world.screens), ridden >= 0));
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
        const draw = (context, size, ratio) => miniMap.draw(context, size, ratio, focus.x, focus.y, mapHeading, poses, info.count, platform ? -1 : ridden, platform && trip && trip.podID ? ridden : -1);
        if (mode === "cabin") {
          world.screens.draw("mapScreen", (context, width, height) => {
            draw(context, Math.min(width, height), Math.min(width, height) / MINIMAP_SIZE);
            // The panel fills the corners around the round map.
            context.globalCompositeOperation = "destination-over";
            context.fillStyle = HUD_COLORS.panel;
            context.fillRect(0, 0, width, height);
            context.globalCompositeOperation = "source-over";
          });
        } else {
          const ratio = root.devicePixelRatio || 1, size = Math.round(MINIMAP_SIZE * ratio);
          if (miniMapCanvas.width !== size) miniMapCanvas.width = miniMapCanvas.height = size;
          draw(miniMapContext, size, ratio);
        }
      }
      // The light sits back along the sun direction from a point ahead of
      // the camera, so the shadow map covers the ground that the camera sees.
      const shadowCenter = B.Vector3.FromArray(shadowFocus(camera.position.asArray(), camera.getDirection(B.Axis.Z).asArray(), SHADOW_AHEAD));
      const light = shadowCenter.subtract(sunDirection.scale(400));
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

  const api = { POSE_FIELDS, lookOptions, lightLook, segmentIndex, segmentsNear, segmentsCross, convexHull, findHubs, deckGeometry, levelHeight, laneProfile, filletPlan, pieceProfile, uniqueLanes, miniMapArrow, snapShadowCenter, shadowFocus, stopPoints, shortLabel, placeLabels, skyLook, skyColor, cubeDirection, skyCubeFaces, podLightTexels, platformView, tripView, nearestStop, autoHop, createFader, createAutoMode, BEAM_TOP, GROUND_LAYERS, groundLayerOrder, toScene, worldToGeo, geoToWorld, groundTiles, tileQuad, nearestSlabs, yawOf, wrapAngle, newTurn, dampAngle, laneSamples, beamGeometry, guidewayProfiles, railGeometry, pillarPoints, stationSlabs, sceneBounds, writePodMatrix, parsePodModel, STRETCH_CUT, stretchZ, stretchPart, seatEye, choosePod, podLabel, springBack, seatFov, chaseEye, smoothPodYaws, formatSpeed, formatDistance, formatDuration, hudView, boxUVs, screenQuad, fitText, stopLadder, ladderItems, gaugeView, hudMode, waitText, drawText, drawGauge, drawLadder, drawSpeedScreen, drawRouteScreen, drawBanner, drawStationSign, attachTexture, createMiniMap, createSigns, start };
  if (typeof module !== "undefined" && module.exports) module.exports = api;
  root.PodsimRide = api;
})(typeof globalThis !== "undefined" ? globalThis : this);
