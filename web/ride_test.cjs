"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const ride = require("./ride.js");

const near = (actual, expected, message) => assert.ok(Math.abs(actual - expected) < 1e-9, `${message}: ${actual} != ${expected}`);

// forwardOf turns the forward axis (+z) by a Babylon yaw. It uses the same
// matrix as writePodMatrix.
function forwardOf(yaw) {
  const matrix = new Float32Array(16);
  ride.writePodMatrix(matrix, 0, 0, 0, 0, yaw);
  // A row vector (0, 0, 1) times the matrix gives row 2.
  return [matrix[8], matrix[10]];
}

test("world y points south and scene z points north", () => {
  assert.deepEqual(ride.toScene(10, 20), [10, -20]);
});

test("yaw turns the pod and the camera to the world heading", () => {
  const cases = [
    { name: "east", heading: 0, forward: [1, 0] },
    { name: "south", heading: Math.PI / 2, forward: [0, -1] },
    { name: "west", heading: Math.PI, forward: [-1, 0] },
    { name: "north", heading: -Math.PI / 2, forward: [0, 1] },
  ];
  for (const { name, heading, forward } of cases) {
    const [x, z] = forwardOf(ride.yawOf(heading));
    near(x, forward[0], `${name} x`);
    near(z, forward[1], `${name} z`);
    // The scene direction of the heading matches the world direction.
    const [wx, wz] = ride.toScene(Math.cos(heading), Math.sin(heading));
    near(x, wx, `${name} world x`);
    near(z, wz, `${name} world z`);
  }
});

test("dampAngle takes the shorter way and converges", () => {
  near(ride.wrapAngle(3 * Math.PI), Math.PI, "wrap");
  near(ride.wrapAngle(-Math.PI), Math.PI, "wrap lower bound");
  // From just below pi to just above -pi is a short step across the seam.
  const seam = { angle: Math.PI - 0.1, rate: 0 };
  const step = ride.dampAngle(seam, -Math.PI + 0.1, 0.15, 0.15);
  assert.ok(Math.abs(ride.wrapAngle(step - Math.PI)) < 0.1, `step ${step} went the long way`);
  // A reset turn keeps its old rate until the next step.
  const first = { angle: NaN, rate: 5 };
  near(ride.dampAngle(first, 1, 0.016, 0.15), 1, "first value");
  near(first.rate, 0, "first rate");
  near(ride.dampAngle({ angle: 0, rate: 2 }, 1, -0.5, 0.15), 0, "negative time");
  const long = { angle: 0, rate: 0 };
  near(ride.dampAngle(long, 1, 30, 0.15), 1, "long time");
  near(long.rate, 0, "rate after a long time");
  near(ride.dampAngle({ angle: 0, rate: 0 }, 1, 0, 0.15), 0, "no time");
  near(ride.dampAngle({ angle: 0, rate: 3 }, 1, 0.1, 0), 1, "no damping");
});

test("dampAngle starts and ends a turn slowly", () => {
  // The target turns by 1 rad at once. The turn speeds up, then slows down.
  const turn = { angle: 0, rate: 0 }, steps = [];
  let last = 0;
  for (let i = 0; i < 60; i++) {
    const angle = ride.dampAngle(turn, 1, 1 / 60, 0.35);
    steps.push(angle - last);
    last = angle;
  }
  const peak = steps.indexOf(Math.max(...steps));
  assert.ok(peak >= 5 && peak <= 15, `the fastest step is ${peak}`);
  for (let i = 1; i <= peak; i++) assert.ok(steps[i] > steps[i - 1], `step ${i} speeds up`);
  for (let i = peak + 1; i < steps.length; i++) assert.ok(steps[i] < steps[i - 1], `step ${i} slows down`);
  // A first-order filter would move 4.6% in the first frame.
  assert.ok(steps[0] < 0.005, `first step ${steps[0]}`);
});

test("dampAngle does not depend on the frame rate", () => {
  const once = { angle: 0, rate: 0.5 }, often = { angle: 0, rate: 0.5 };
  ride.dampAngle(once, 1, 0.3, 0.35);
  for (let i = 0; i < 30; i++) ride.dampAngle(often, 1, 0.01, 0.35);
  near(once.angle, often.angle, "angle");
  near(once.rate, often.rate, "rate");
});

test("dampAngle lags a steady turn by tau", () => {
  const turn = { angle: 0, rate: 0 };
  let target = 0;
  for (let i = 0; i < 500; i++) {
    target += 0.005;
    ride.dampAngle(turn, target, 0.01, 0.35);
  }
  // The target moves in steps, so it is half a step ahead of a smooth turn.
  const rate = 0.5, lag = rate * 0.35 - rate * 0.01 / 2;
  assert.ok(Math.abs(target - turn.angle - lag) < 1e-6, `lag ${target - turn.angle}`);
});

test("laneSamples keeps straight lanes and samples curves", () => {
  const from = { x: 0, y: 0 }, to = { x: 100, y: 100 };
  assert.deepEqual(ride.laneSamples(from, to, null), [from, to]);
  const curve = ride.laneSamples(from, to, { x: 100, y: 0 });
  assert.ok(curve.length >= 3 && curve.length <= 33, `${curve.length} samples`);
  assert.deepEqual(curve[0], from);
  near(curve.at(-1).x, 100, "end x");
  near(curve.at(-1).y, 100, "end y");
});

test("beamGeometry gives four faces with outward normals", () => {
  const nodes = new Map([["a", { x: 0, y: 0 }], ["b", { x: 10, y: 0 }]]);
  const geometry = ride.beamGeometry([{ from: "a", to: "b" }, { from: "a", to: "missing" }], nodes);
  // One straight lane: 4 faces, 2 samples, 2 edges each, and 2 triangles per face.
  assert.equal(geometry.positions.length, 4 * 2 * 2 * 3);
  assert.equal(geometry.normals.length, geometry.positions.length);
  assert.equal(geometry.colors.length, geometry.positions.length / 3 * 4);
  // The top face comes first, and it is darker than a side.
  assert.ok(geometry.colors[0] < geometry.colors[4 * 4], "top face is darker");
  assert.equal(geometry.indices.length, 4 * 6);
  for (let i = 0; i < geometry.positions.length; i += 3) {
    const [x, y, z] = geometry.positions.subarray(i, i + 3);
    const [nx, ny, nz] = geometry.normals.subarray(i, i + 3);
    // The beam center line is at scene z = 0, between the top and the bottom.
    const center = [x, ride.BEAM_TOP - 0.35, 0];
    assert.ok(nx * 0 + ny * (y - center[1]) + nz * (z - center[2]) > 0, `normal ${[nx, ny, nz]} at ${[x, y, z]} points inward`);
  }
  for (const index of geometry.indices) assert.ok(index < geometry.positions.length / 3);
});

test("pillarPoints spaces pillars along lanes", () => {
  const nodes = new Map([["a", { x: 0, y: 0 }], ["b", { x: 200, y: 0 }]]);
  const points = ride.pillarPoints([{ from: "a", to: "b" }], nodes);
  assert.deepEqual(points.map(([x]) => x), [30, 90, 150]);
});

test("stationSlabs covers the berths", () => {
  const slabs = ride.stationSlabs([
    { id: "s", name: "Stop", berths: [{ x: 0, y: 0 }, { x: 20, y: 10 }] },
    { id: "empty", name: "Empty", berths: [] },
  ], 5);
  assert.deepEqual(slabs, [{ id: "s", name: "Stop", height: ride.BEAM_TOP, x: 10, z: -5, width: 30, depth: 20 }]);
  assert.equal(ride.stationSlabs([{ id: "low", berths: [{ x: 0, y: 0 }], height: 1 }])[0].height, 1);
});

test("choosePod keeps the request, then prefers a pod with riders", () => {
  const pods = [{ id: "01", riders: 0 }, { id: "02", riders: 1 }];
  assert.equal(ride.choosePod(pods, "01"), "01");
  assert.equal(ride.choosePod(pods, "99"), "02");
  assert.equal(ride.choosePod([{ id: "01", riders: 0 }], ""), "01");
  assert.equal(ride.choosePod([], "01"), "");
});

test("springBack returns a look offset toward zero", () => {
  near(ride.springBack(1, 0, 0.3), 1, "no time");
  near(ride.springBack(1, 0.3, 0.3), Math.exp(-1), "one time constant");
  near(ride.springBack(1, -1, 0.3), 1, "negative time");
  near(ride.springBack(1, 0.1, 0), 0, "no time constant");
});

test("chaseEye is behind and above the pod", () => {
  // A pod at world (0, 0) heading east has the yaw of east.
  const [x, y, z] = ride.chaseEye(0, 0, ride.yawOf(0));
  assert.ok(x < -10 && Math.abs(z) < 1e-9, `eye ${[x, y, z]} is not west of the pod`);
  assert.ok(y > ride.BEAM_TOP + 2, `eye height ${y}`);
  // On a lower beam, the eye is lower by the same amount.
  near(ride.chaseEye(0, 0, ride.yawOf(0), ride.BEAM_TOP - 4)[1], y - 4, "eye over a station beam");
});

test("writePodMatrix puts the pod on its beam", () => {
  const matrix = new Float32Array(16);
  ride.writePodMatrix(matrix, 0, 7, 3, -2, 0);
  near(matrix[12], 7, "x");
  near(matrix[14], -2, "z");
  assert.ok(Math.abs(matrix[13] - (3 + 1.05)) < 1e-6, `pod center ${matrix[13]}`);
});

test("smoothPodYaws starts at the heading and then follows it", () => {
  const poses = new Float64Array([0, 0, 0, 0, 0, 5, 5, 5, Math.PI / 2, 0, 0, 5]);
  const yaws = ride.smoothPodYaws(new Map(), ["a", "b"], poses, 2, 0.016, 0.2);
  near(yaws.get("a").angle, ride.yawOf(0), "first pod");
  near(yaws.get("b").angle, ride.yawOf(Math.PI / 2), "second pod");
  poses[2] = Math.PI / 2;
  ride.smoothPodYaws(yaws, ["a", "b"], poses, 2, 0.2, 0.2);
  const expected = ride.dampAngle({ angle: ride.yawOf(0), rate: 0 }, ride.yawOf(Math.PI / 2), 0.2, 0.2);
  near(yaws.get("a").angle, expected, "turning pod");
  near(yaws.get("b").angle, ride.yawOf(Math.PI / 2), "steady pod");
  // A count larger than the ID list does not add an undefined key.
  ride.smoothPodYaws(yaws, ["a"], poses, 2, 0.1, 0.2);
  assert.ok(!yaws.has(undefined));
});

test("formats speed, distance, and duration", () => {
  assert.equal(ride.formatSpeed(14), "50 km/h");
  assert.equal(ride.formatDistance(999.4), "999 m");
  assert.equal(ride.formatDistance(1250), "1.3 km");
  assert.equal(ride.formatDuration(65.4), "1:05");
  assert.equal(ride.formatDuration(3725), "1:02:05");
  assert.equal(ride.formatDuration(-3), "0:00");
});

test("hudView shows the pod, the trip, and the session", () => {
  const hud = {
    found: true, podID: "01", class: "compact", activity: "Traveling", speed: 10, speedLimit: 14,
    waitReason: "Pod ahead", blockedBy: "02",
    riders: [{ partySize: 1, from: "Bank", to: "Oval" }, { partySize: 3, from: "Bank", to: "Kennington" }],
    nextStop: "Oval", route: { traveledMeters: 500, remainingMeters: 1500, seconds: 125, before: false, after: true },
    session: { speed: 15, paused: false, connected: true, message: "" },
  };
  const view = ride.hudView(hud);
  assert.equal(view.speed, "36 km/h, limit 50 km/h");
  assert.equal(view.state, "01 (compact): Traveling");
  assert.equal(view.wait, "Waiting: Pod ahead (02)");
  assert.deepEqual(view.trip, ["1 passenger: Bank to Oval", "3 passengers: Bank to Kennington"]);
  assert.equal(view.next, "Next stop: Oval");
  assert.equal(view.route, "Route: over 1.5 km, over 2:05");
  near(view.progress, 0.25, "progress");
  const whole = ride.hudView({ ...hud, route: { traveledMeters: 0, remainingMeters: 400, seconds: 30, before: false, after: false } });
  assert.equal(whole.route, "Route: 400 m, about 0:30");
  near(whole.progress, 0, "progress at the start");
  near(ride.hudView({ ...hud, route: { traveledMeters: 0, remainingMeters: 0, seconds: 0 } }).progress, 1, "progress of an empty route");
  assert.equal(view.session, "Session 15x");
  assert.equal(view.rideAt1x, true);

  const empty = ride.hudView({ ...hud, riders: [], nextStop: "", waitReason: "", route: null, speedLimit: 0, session: { speed: 1, paused: true, connected: false, error: "lost" } });
  assert.equal(empty.next, "No passengers");
  assert.equal(empty.wait, "");
  assert.equal(empty.route, "");
  assert.equal(empty.progress, -1);
  assert.equal(empty.speed, "36 km/h");
  assert.equal(empty.session, "Session 1x, paused, disconnected: lost");
  assert.equal(empty.rideAt1x, false);

  const missing = ride.hudView({ found: false, riders: [], session: { speed: 2, connected: true, message: "the session changed" } });
  assert.equal(missing.state, "Pod not in the session");
  assert.equal(missing.message, "the session changed");
  assert.equal(missing.rideAt1x, true);
});

const london = { latitude: 51.5074, longitude: -0.1278, radius: 6371000, projection: "equirectangular" };

test("worldToGeo and geoToWorld are inverse, with world y to the south", () => {
  const point = ride.worldToGeo(london, 1000, 2000);
  assert.ok(point.lat < london.latitude && point.lon > london.longitude, "south-east of the origin");
  const back = ride.geoToWorld(london, point.lat, point.lon);
  assert.ok(Math.abs(back.x - 1000) < 1e-6 && Math.abs(back.y - 2000) < 1e-6, `${back.x}, ${back.y}`);
});

test("groundTiles covers the point, nearest first, with touching rectangles", () => {
  const tiles = ride.groundTiles(london, 0, 0, 17, 700, 64);
  assert.ok(tiles.length > 9 && tiles.length <= 64, `${tiles.length} tiles`);
  const first = tiles[0];
  assert.equal(first.distance, 0);
  assert.ok(first.rect.minX <= 0 && first.rect.maxX >= 0 && first.rect.minY <= 0 && first.rect.maxY >= 0, "first tile holds the origin");
  // A zoom-17 tile at London is about 190 m wide.
  const width = first.rect.maxX - first.rect.minX;
  assert.ok(width > 150 && width < 230, `width ${width}`);
  for (let i = 1; i < tiles.length; i++) assert.ok(tiles[i].distance >= tiles[i - 1].distance);
  const east = tiles.find((tile) => tile.x === first.x + 1 && tile.y === first.y);
  assert.ok(Math.abs(east.rect.minX - first.rect.maxX) < 1e-6, "east neighbor touches");
  const south = tiles.find((tile) => tile.x === first.x && tile.y === first.y + 1);
  assert.ok(Math.abs(south.rect.minY - first.rect.maxY) < 1e-6, "south neighbor touches");
  assert.equal(new Set(tiles.map((tile) => tile.key)).size, tiles.length);
  assert.equal(ride.groundTiles(london, 0, 0, 17, 700, 5).length, 5);
  // Tile x wraps across the date line.
  const dateLine = { ...london, longitude: 179.999 };
  assert.ok(ride.groundTiles(dateLine, 0, 0, 10, 2000, 64).every((tile) => tile.x >= 0 && tile.x < 1024));
});

test("tileQuad puts the north edge of the tile at v = 1", () => {
  const quad = ride.tileQuad({ minX: 0, maxX: 10, minY: 0, maxY: 20 }, 0.5);
  for (let i = 0; i < 4; i++) {
    const [x, y, z] = quad.positions.subarray(i * 3, i * 3 + 3), [u, v] = quad.uvs.subarray(i * 2, i * 2 + 2);
    assert.equal(y, 0.5);
    // World y 0 is the north edge, which is scene z 0. World y 20 is z -20.
    assert.equal(v, z === 0 ? 1 : 0, `v at z ${z}`);
    assert.equal(u, x === 0 ? 0 : 1, `u at x ${x}`);
  }
  // The material draws both sides, so only the two triangles must agree.
  const winding = (t) => {
    const [a, b, c] = [...quad.indices.subarray(t * 3, t * 3 + 3)].map((index) => [quad.positions[index * 3], quad.positions[index * 3 + 2]]);
    return Math.sign((b[0] - a[0]) * (c[1] - a[1]) - (b[1] - a[1]) * (c[0] - a[0]));
  };
  assert.ok(winding(0) !== 0 && winding(0) === winding(1));
});

test("nearestSlabs keeps the nearest stations within the radius", () => {
  const slabs = [{ id: "far", x: 1000, z: 0 }, { id: "b", x: 10, z: 0 }, { id: "a", x: 0, z: 10 }, { id: "near", x: 1, z: 1 }];
  assert.deepEqual(ride.nearestSlabs(slabs, 0, 0, 3, 500).map((slab) => slab.id), ["near", "a", "b"]);
  assert.deepEqual(ride.nearestSlabs(slabs, 0, 0, 2, 500).map((slab) => slab.id), ["near", "a"]);
  assert.deepEqual(ride.nearestSlabs([], 0, 0, 3, 500), []);
});

test("groundLayerOrder draws the ground layers first, in their order", () => {
  const sub = (name, layer) => ({ name, getMesh: () => ({ metadata: layer === undefined ? null : { groundLayer: layer } }) });
  const L = ride.GROUND_LAYERS;
  const list = [sub("pod"), sub("near", L.nearTiles), sub("beam"), sub("ground", L.ground), sub("far", L.farTiles), sub("sky", L.sky)];
  const sorted = [...list].sort(ride.groundLayerOrder).map((entry) => entry.name);
  assert.deepEqual(sorted, ["sky", "ground", "far", "near", "pod", "beam"]);
  assert.ok(L.farTiles < L.midTiles && L.midTiles < L.nearTiles, "a nearer ring draws later");
  assert.equal(ride.groundLayerOrder(sub("pod"), sub("beam")), 0);
});

test("segmentsNear returns the segments near a point once", () => {
  const nodes = new Map([["a", { x: 0, y: 0 }], ["b", { x: 1000, y: 0 }], ["c", { x: 5000, y: 5000 }], ["d", { x: 5010, y: 5000 }]]);
  const lanes = [{ id: "long", from: "a", to: "b" }, { id: "far", from: "c", to: "d" }, { id: "broken", from: "a", to: "x" }];
  const index = ride.segmentIndex(lanes, nodes, 250);
  assert.equal(index.coords.length, 8, "a lane with a missing node has no segment");
  // The long segment is in 5 cells, and the query touches 3 of them.
  assert.deepEqual(ride.segmentsNear(index, 500, 0, 300), [0]);
  assert.deepEqual(ride.segmentsNear(index, 5005, 5000, 10), [1]);
  assert.deepEqual(ride.segmentsNear(index, 2500, 2500, 100), []);
  // A curved lane gives one segment for each pair of samples.
  const curved = ride.segmentIndex([{ id: "curve", from: "a", to: "b", control: { x: 500, y: 500 } }], nodes, 250);
  assert.ok(curved.coords.length / 4 > 2);
  assert.equal(ride.segmentsNear(curved, 500, 250, 1000).length, curved.coords.length / 4);
});

test("miniMapArrow points along the heading on a north-up map", () => {
  const cases = [
    { name: "east", heading: 0, tip: [110, 100], backRight: [93, 107] },
    { name: "south", heading: Math.PI / 2, tip: [100, 110], backRight: [93, 93] },
    { name: "north", heading: -Math.PI / 2, tip: [100, 90], backRight: [107, 107] },
  ];
  for (const { name, heading, tip, backRight } of cases) {
    const corners = ride.miniMapArrow(heading, 100, 10);
    near(corners[0][0], tip[0], `${name} tip x`);
    near(corners[0][1], tip[1], `${name} tip y`);
    near(corners[1][0], backRight[0], `${name} back right x`);
    near(corners[1][1], backRight[1], `${name} back right y`);
  }
});

test("autoHop moves after the last rider leaves", () => {
  const cases = [
    { name: "last rider left", args: [true, false, 2, 0], want: true },
    { name: "auto off", args: [false, false, 2, 0], want: false },
    { name: "riders aboard", args: [true, false, 2, 1], want: false },
    { name: "still empty", args: [true, false, 0, 0], want: false },
    { name: "first read after a move", args: [true, false, -1, 0], want: false },
    { name: "turned on with an empty pod", args: [true, true, -1, 0], want: true },
    { name: "turned on with riders", args: [true, true, -1, 3], want: false },
    { name: "no ridden pod", args: [true, true, 2, -1], want: false },
  ];
  for (const { name, args, want } of cases) assert.equal(ride.autoHop(...args), want, name);
});

test("nearestStop finds the nearest passenger station", () => {
  const stations = [
    { id: "a", name: "Alpha", berths: [{ x: 0, y: 0 }, { x: 20, y: 0 }] },
    { id: "depot", name: "Depot", parkingOnly: true, berths: [{ x: 101, y: 0 }] },
    { id: "b", name: "", berths: [{ x: 100, y: 0 }] },
    { id: "empty", name: "Empty", berths: [] },
  ];
  const stops = ride.stopPoints(stations);
  assert.deepEqual(stops, [{ id: "a", name: "Alpha", x: 10, y: 0 }, { id: "b", name: "b", x: 100, y: 0 }]);
  assert.deepEqual(ride.nearestStop(stops, 90, 0), { id: "b", name: "b", distance: 10 });
  assert.deepEqual(ride.nearestStop(stops, 10, 30), { id: "a", name: "Alpha", distance: 30 });
  // A tie goes to the first stop.
  assert.equal(ride.nearestStop(stops, 55, 0).name, "Alpha");
  assert.equal(ride.nearestStop([], 0, 0), null);
});

test("shadowFocus moves ahead along the horizontal view direction", () => {
  assert.deepEqual(ride.shadowFocus([10, 6, -5], [0.3, -0.9, 0.4], 100), [70, 6, 75]);
  assert.deepEqual(ride.shadowFocus([10, 6, -5], [0, -1, 0], 100), [10, 6, -5]);
});

test("snapShadowCenter moves the light to whole texels across its view", () => {
  const d = [-0.5, -0.75, -0.35], length = Math.hypot(...d), unit = d.map((value) => value / length);
  const texel = 260 / 4096;
  // The x and y axes of a Babylon left-handed look-at with up +y.
  const xAxis = [unit[2], 0, -unit[0]].map((value) => value / Math.hypot(unit[2], unit[0]));
  const yAxis = [unit[1] * xAxis[2] - unit[2] * xAxis[1], unit[2] * xAxis[0] - unit[0] * xAxis[2], unit[0] * xAxis[1] - unit[1] * xAxis[0]];
  const dot = (a, b) => a[0] * b[0] + a[1] * b[1] + a[2] * b[2];
  for (const p of [[0, 0, 0], [1234.567, 300.1, -9876.54], [-50000.3, 400, 20000.9]]) {
    const snapped = ride.snapShadowCenter(p, unit, texel);
    for (const [name, axis] of [["x", xAxis], ["y", yAxis]]) {
      const steps = dot(snapped, axis) / texel;
      assert.ok(Math.abs(steps - Math.round(steps)) < 1e-6, `${name} is ${steps} texels`);
      assert.ok(Math.abs(dot(snapped, axis) - dot(p, axis)) <= texel / 2 + 1e-9, `${name} moved more than half a texel`);
    }
    near(dot(snapped, unit), dot(p, unit), "depth along the light");
  }
});

test("shortLabel shortens station names for the mini-map", () => {
  const cases = [
    ["Bank", "Bank"],
    ["Custom House (for ExCel)", "Custom House"],
    ["Cutty Sark (for Maritime Greenwich)", "Cutty Sark"],
    ["Battersea Power Station", "Battersea…"],
    ["Walthamstowcentralstation", "Walthamstowcen…"],
    ["(brackets)", "(brackets)"],
  ];
  for (const [name, want] of cases) assert.equal(ride.shortLabel(name, 14), want, name);
});

test("placeLabels keeps the nearest labels that do not overlap", () => {
  const box = (text, x, y, distance) => ({ text, x, y, width: 40, height: 10, distance });
  const labels = [box("far", 0, 0, 300), box("near", 20, 5, 100), box("apart", 100, 100, 500), box("blocked", 200, 200, 50)];
  const blocked = [{ x: 210, y: 198, width: 5, height: 10 }];
  assert.deepEqual(ride.placeLabels(labels, blocked, 10).map((label) => label.text), ["near", "apart"]);
  assert.deepEqual(ride.placeLabels(labels, blocked, 1).map((label) => label.text), ["near"]);
  // Boxes that only touch do not overlap.
  assert.equal(ride.placeLabels([box("a", 0, 0, 1), box("b", 40, 0, 2)], [], 10).length, 2);
});

test("platformView stands beside the berths and looks at their center", () => {
  // Berths along world x: the eye is 22 m to the south (world +y) and 10 m back.
  const along = ride.platformView([{ x: 0, y: 0 }, { x: 40, y: 0 }]);
  assert.deepEqual(along.center, { x: 20, y: 0 });
  near(along.eye[0], 10, "eye x");
  near(along.eye[2], -22, "eye scene z");
  near(along.target[0], 20, "target x");
  near(along.target[2], 0, "target scene z");
  assert.ok(along.eye[1] > along.target[1], "the eye is above the target");
  // Berths along world y: the side turns with them.
  const across = ride.platformView([{ x: 0, y: 0 }, { x: 0, y: 40 }]);
  near(across.eye[0], -22, "turned eye x");
  near(across.eye[2], -10, "turned eye scene z");
  // One berth uses world x as the line.
  const low = ride.platformView([{ x: 0, y: 0 }, { x: 40, y: 0 }], 1);
  near(along.eye[1] - low.eye[1], ride.BEAM_TOP - 1, "eye over a station platform");
  near(along.target[1] - low.target[1], ride.BEAM_TOP - 1, "target on a station platform");
  const one = ride.platformView([{ x: 5, y: 5 }]);
  near(one.eye[0], -5, "single eye x");
  near(one.eye[2], -27, "single eye scene z");
});

test("tripView shows each phase of the passenger flow", () => {
  const names = new Map([["s", "Bank"], ["t", "Oval"], ["x", "Kennington"]]);
  const base = { origin: "s", destination: "t", station: "s" };
  const cases = [
    { p: { ...base, phase: "requested" }, text: "Sending the trip: Bank to Oval", camera: "platform", station: "s", podID: "", active: true },
    { p: { ...base, phase: "requested", orderID: 7 }, text: "Order #7: Bank to Oval", camera: "platform", station: "s", podID: "", active: true },
    { p: { ...base, phase: "waiting", orderID: 7 }, text: "Order #7: waiting at Bank for a pod", camera: "platform", station: "s", podID: "", active: true },
    { p: { ...base, phase: "waiting", orderID: 7, station: "x", podID: "p2" }, text: "Order #7: waiting at Kennington for pod p2", camera: "platform", station: "x", podID: "p2", active: true },
    { p: { ...base, phase: "riding", orderID: 7, podID: "p2" }, text: "Order #7: riding pod p2 to Oval", camera: "seat", station: "", podID: "p2", active: true },
    { p: { ...base, phase: "arrived", orderID: 7, station: "t", podID: "p2" }, text: "Order #7: arrived at Oval", camera: "platform", station: "t", podID: "", active: false },
    { p: { ...base, phase: "stopped", message: "the queue is full" }, text: "Trip stopped: the queue is full", camera: "", station: "", podID: "", active: false },
  ];
  for (const { p, ...want } of cases) assert.deepEqual(ride.tripView(p, names), want, p.phase);
  assert.equal(ride.tripView({ phase: "" }, names), null);
  assert.equal(ride.tripView(null, names), null);
  assert.equal(ride.tripView({ ...base, phase: "waiting", station: "zz" }, names).text, "Trip: waiting at zz for a pod");
});

test("levelHeight matches the height curve of internal/ride", () => {
  const fixture = JSON.parse(require("node:fs").readFileSync(require("node:path").join(__dirname, "../internal/ride/testdata/levels.json"), "utf8"));
  assert.equal(fixture.levels.mainTop, ride.BEAM_TOP, "the mainline is at BEAM_TOP");
  for (const { distance, height } of fixture.cases) near(ride.levelHeight(distance, fixture.levels), height, `height at ${distance} m`);
  assert.equal(ride.levelHeight(60, undefined), ride.BEAM_TOP, "no levels");
});

const LEVELS = { mainTop: 5, stationTop: 1, rampMeters: 120 };

test("laneProfile follows a ramp and keeps flat lanes short", () => {
  const nodes = new Map([["m", { x: 0, y: 0 }], ["s", { x: 120, y: 0 }], ["t", { x: 240, y: 0 }]]);
  const ramp = ride.laneProfile({ from: "m", to: "s", fromDistance: 0, toDistance: 120 }, nodes, LEVELS);
  // 120 m at one point each 8 m: 16 steps.
  assert.equal(ramp.length, 16);
  near(ramp[0].h, 5, "top of the ramp");
  near(ramp.at(-1).h, 1, "bottom of the ramp");
  near(ramp[8].x, 64, "point 8 x");
  near(ramp[8].h, ride.levelHeight(64, LEVELS), "point 8 height");
  for (let i = 1; i < ramp.length; i++) assert.ok(ramp[i].h <= ramp[i - 1].h, `point ${i} goes up`);
  // An up ramp is the same ramp the other way.
  const up = ride.laneProfile({ from: "s", to: "m", fromDistance: 120, toDistance: 0 }, nodes, LEVELS);
  near(up[3].h, ramp[ramp.length - 4].h, "up ramp");
  const flat = ride.laneProfile({ from: "s", to: "t", fromDistance: 120, toDistance: 120 }, nodes, LEVELS);
  assert.deepEqual(flat, [{ x: 120, y: 0, h: 1 }, { x: 240, y: 0, h: 1 }]);
  const main = ride.laneProfile({ from: "m", to: "t" }, nodes, LEVELS);
  assert.deepEqual(main.map((point) => point.h), [5, 5]);
  assert.equal(ride.laneProfile({ from: "m", to: "missing" }, nodes, LEVELS), null);
  // A curve keeps its samples and gets the height by the length before each.
  const curve = ride.laneProfile({ from: "m", to: "t", control: { x: 120, y: 200 }, fromDistance: 0, toDistance: 120 }, nodes, LEVELS);
  near(curve[0].h, 5, "curve start");
  near(curve.at(-1).h, 1, "curve end");
  const middle = curve[(curve.length - 1) / 2];
  near(middle.x, 120, "the middle sample is on the axis of symmetry");
  near(middle.h, 3, "the middle of a symmetric curve is half way down");
  // On a lopsided curve, the samples are not evenly spaced, and the height
  // follows the length before each sample, as a pod height does.
  const lopsided = ride.laneProfile({ from: "m", to: "t", control: { x: 230, y: 150 }, fromDistance: 0, toDistance: 120 }, nodes, LEVELS);
  let before = 0;
  const total = lopsided.slice(1).reduce((sum, point, i) => sum + Math.hypot(point.x - lopsided[i].x, point.y - lopsided[i].y), 0);
  lopsided.forEach((point, i) => {
    if (i > 0) before += Math.hypot(point.x - lopsided[i - 1].x, point.y - lopsided[i - 1].y);
    near(point.h, ride.levelHeight(120 * before / total, LEVELS), `lopsided point ${i}`);
  });
});

test("uniqueLanes draws a reverse twin once", () => {
  const lanes = [
    { id: "ab", from: "a", to: "b" }, { id: "ba", from: "b", to: "a" },
    { id: "curve", from: "a", to: "b", control: { x: 1, y: 2 } }, { id: "back", from: "b", to: "a", control: { x: 1, y: 2 } },
    { id: "other", from: "b", to: "a", control: { x: 1, y: 3 } }, { id: "bc", from: "b", to: "c" },
  ];
  assert.deepEqual(ride.uniqueLanes(lanes).map((lane) => lane.id), ["ab", "curve", "other", "bc"]);
});

test("beamGeometry and pillarPoints follow the levels", () => {
  const nodes = new Map([["m", { x: 0, y: 0 }], ["s", { x: 120, y: 0 }], ["t", { x: 600, y: 0 }]]);
  const lanes = [{ from: "m", to: "s", fromDistance: 0, toDistance: 120 }, { from: "s", to: "t", fromDistance: 120, toDistance: 120 }];
  const geometry = ride.beamGeometry(lanes.slice(0, 1), nodes, LEVELS);
  // The top face of the first point is at 5 m, and of the last point at 1 m.
  const tops = [];
  for (let i = 0; i < geometry.positions.length / 3; i++) if (geometry.normals[i * 3 + 1] === 1) tops.push(geometry.positions[i * 3 + 1]);
  assert.ok(Math.abs(Math.max(...tops) - 5) < 1e-6 && Math.abs(Math.min(...tops) - 1) < 1e-6, `tops ${Math.min(...tops)} to ${Math.max(...tops)}`);
  const pillars = ride.pillarPoints(lanes, nodes, LEVELS);
  // On the ramp, the pillar at 30 m holds the beam, and the one at 90 m is
  // too low. The station lane has none. The beam is straight between the
  // ramp points at 24 m and 32 m, so the pillar meets it there.
  assert.equal(pillars.length, 1);
  near(pillars[0][0], 30, "pillar x");
  near(pillars[0][2], ride.levelHeight(24, LEVELS) + (ride.levelHeight(32, LEVELS) - ride.levelHeight(24, LEVELS)) * 0.75, "pillar top");
});

test("convexHull keeps the outer points counterclockwise", () => {
  const points = [{ x: 0, y: 0 }, { x: 10, y: 0 }, { x: 10, y: 10 }, { x: 0, y: 10 }, { x: 5, y: 5 }, { x: 5, y: 0 }, { x: 0, y: 0 }];
  assert.deepEqual(ride.convexHull(points), [{ x: 0, y: 0 }, { x: 10, y: 0 }, { x: 10, y: 10 }, { x: 0, y: 10 }]);
  assert.deepEqual(ride.convexHull([{ x: 1, y: 1 }, { x: 2, y: 2 }]), [{ x: 1, y: 1 }, { x: 2, y: 2 }]);
});

test("findHubs groups crossing mainline lanes", () => {
  // A ring of 8 nodes. Each node joins the node across the ring, so the 4
  // chords cross in the middle, and each joins the node 3 steps on, which
  // adds more crossings.
  const nodes = new Map(), lanes = [];
  for (let i = 0; i < 8; i++) nodes.set(`r${i}`, { x: 100 * Math.cos(i * Math.PI / 4), y: 100 * Math.sin(i * Math.PI / 4) });
  for (let i = 0; i < 8; i++) lanes.push({ id: `c${i}`, from: `r${i}`, to: `r${(i + 3) % 8}` });
  for (let i = 0; i < 4; i++) lanes.push({ id: `d${i}`, from: `r${i}`, to: `r${i + 4}` });
  // An approach lane ends on the ring and crosses nothing.
  nodes.set("far", { x: 1000, y: 0 });
  lanes.push({ id: "approach", from: "far", to: "r0" });
  // A pair of lanes far away cross once, which is not a hub.
  for (const [id, x, y] of [["p", 5000, 0], ["q", 5100, 0], ["u", 5050, -50], ["v", 5050, 50]]) nodes.set(id, { x, y });
  lanes.push({ id: "pq", from: "p", to: "q" }, { id: "uv", from: "u", to: "v" });
  // A station lane through the ring and a long lane over it are not part of it.
  nodes.set("west", { x: -2000, y: 1 }); nodes.set("east", { x: 2000, y: 1 });
  lanes.push({ id: "long", from: "west", to: "east" });
  lanes.push({ id: "station", from: "r2", to: "r6", stationID: "s" });
  const hubs = ride.findHubs(lanes, nodes);
  assert.equal(hubs.length, 1);
  assert.deepEqual([...hubs[0].laneIDs].sort(), ["c0", "c1", "c2", "c3", "c4", "c5", "c6", "c7", "d0", "d1", "d2", "d3"]);
  assert.equal(hubs[0].hull.length, 8);
  assert.ok(hubs[0].crossings >= 10, `crossings ${hubs[0].crossings}`);
});

test("deckGeometry has a top at the deck height and outward walls", () => {
  const hull = [{ x: 0, y: 0 }, { x: 10, y: 0 }, { x: 10, y: 10 }, { x: 0, y: 10 }];
  const deck = ride.deckGeometry(hull, 5);
  // A fan of 2 triangles on top, and 2 triangles on each of 4 walls.
  assert.equal(deck.indices.length, (2 + 8) * 3);
  const count = deck.positions.length / 3;
  for (let i = 0; i < count; i++) {
    const [x, y, z] = deck.positions.subarray(i * 3, i * 3 + 3), [nx, ny, nz] = deck.normals.subarray(i * 3, i * 3 + 3);
    assert.ok(y === 5 || Math.abs(y - (5 - 0.7)) < 1e-6, `height ${y}`);
    if (ny === 1) continue;
    // The deck center is at scene (5, -5). A wall normal points away from it.
    assert.ok(nx * (x - 5) + nz * (z + 5) > 0, `normal ${[nx, ny, nz]} at ${[x, z]} points inward`);
  }
  for (const index of deck.indices) assert.ok(index < count);
  assert.equal(ride.deckGeometry(hull.slice(0, 2), 5).indices.length, 0);
});

test("segmentsCross counts only a crossing inside both segments", () => {
  const p = (x, y) => ({ x, y });
  assert.equal(ride.segmentsCross(p(-1, 0), p(1, 0), p(0, -1), p(0, 1)), true, "X");
  assert.equal(ride.segmentsCross(p(-1, 0), p(1, 0), p(0, -1), p(0, 0)), false, "T: one end on the other segment");
  assert.equal(ride.segmentsCross(p(-1, 0), p(1, 0), p(-1, 1), p(1, 1)), false, "parallel");
  assert.equal(ride.segmentsCross(p(-1, 0), p(1, 0), p(2, -1), p(2, 1)), false, "apart");
});

test("findHubs leaves lanes that share a node at grade", () => {
  // Straight lanes fan out from node o. Curves leave o below the fan and
  // sweep up across all of it, so each curve crosses each straight lane.
  // The simulation controls these crossings at the shared junction o, so
  // they are not a hub.
  const nodes = new Map([["o", { x: 0, y: 0 }]]), lanes = [];
  for (let i = 0; i < 6; i++) {
    nodes.set(`a${i}`, { x: 100, y: 20 * i }); nodes.set(`b${i}`, { x: 100, y: 130 + 10 * i });
    lanes.push({ id: `a${i}`, from: "o", to: `a${i}` }, { id: `b${i}`, from: "o", to: `b${i}`, control: { x: 50, y: -150 } });
  }
  assert.deepEqual(ride.findHubs(lanes, nodes), []);
});

// podModelBuffer reads web/pod.glb into its own ArrayBuffer.
function podModelBuffer() {
  const bytes = require("node:fs").readFileSync(require("node:path").join(__dirname, "pod.glb"));
  return bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength);
}

// windings counts the triangles of a part whose edge cross product points
// along and against the vertex normal of the first corner.
function windings(positions, normals, indices) {
  let along = 0, against = 0;
  for (let t = 0; t < indices.length; t += 3) {
    const [a, b, c] = [indices[t], indices[t + 1], indices[t + 2]].map((i) => [positions[3 * i], positions[3 * i + 1], positions[3 * i + 2]]);
    const u = [b[0] - a[0], b[1] - a[1], b[2] - a[2]], v = [c[0] - a[0], c[1] - a[1], c[2] - a[2]];
    const cross = [u[1] * v[2] - u[2] * v[1], u[2] * v[0] - u[0] * v[2], u[0] * v[1] - u[1] * v[0]];
    const i = indices[t];
    if (cross[0] * normals[3 * i] + cross[1] * normals[3 * i + 1] + cross[2] * normals[3 * i + 2] > 0) along++; else against++;
  }
  return { along, against };
}

test("parsePodModel reads the parts and the eye of web/pod.glb", () => {
  const model = ride.parsePodModel(podModelBuffer());
  assert.deepEqual(Object.keys(model.parts).sort(), ["cabin", "gear", "glass", "seat", "shell"]);
  assert.deepEqual(Object.keys(model.screens), ["mapScreen", "speedScreen", "routeScreen", "banner"]);
  for (const [name, part] of Object.entries(model.parts)) {
    // MergeMeshes needs the same attributes in each part, so each part has
    // texture coordinates.
    assert.equal(part.uvs.length / 2, part.positions.length / 3, `${name} texture coordinates`);
    assert.ok(part.uvs.every(Number.isFinite), `${name} texture coordinates are finite`);
  }
  near(model.length, 4, "model length");
  assert.deepEqual(model.eye.map((value) => Math.round(value * 100) / 100 + 0), [0, 0.3, 0.5], "eye");
  near(model.parts.glass.color[0], 0.05, "glass color");
  for (const [name, part] of Object.entries(model.parts)) {
    // In Babylon's left-handed scene, the edge cross product of a front
    // face points against its normal. A headless check of MeshBuilder boxes
    // and spheres in Babylon 9.36 gave this sign for every triangle. The
    // model is symmetric in x, so this check also fails when x does not
    // change sign in both the positions and the normals.
    const { along, against } = windings(part.positions, part.normals, part.indices);
    assert.equal(along, 0, `${name}: ${along} of ${along + against} triangles wind as back faces`);
  }
});

test("parsePodModel refuses a damaged file", () => {
  const buffer = podModelBuffer();
  assert.throws(() => ride.parsePodModel(buffer.slice(0, buffer.byteLength - 4)), /not a GLB file/);
  const wrongMagic = buffer.slice(0);
  new DataView(wrongMagic).setUint32(0, 0, true);
  assert.throws(() => ride.parsePodModel(wrongMagic), /not a GLB file/);
});

test("stretchPart keeps the normals at right angles to the surface", () => {
  // The triangle slopes along z behind the cut, so a stretch in z turns its
  // normal.
  const a = [0, 0, -2], b = [1, 0, -1], c = [0, 1, -2];
  const normal = [-1, 0, 1].map((value) => value / Math.sqrt(2));
  const part = { positions: Float32Array.from([...a, ...b, ...c]), normals: Float32Array.from([...normal, ...normal, ...normal]) };
  const { positions, normals } = ride.stretchPart(part, 1.5, 4);
  near(positions[2], -3, "the rear end");
  assert.ok(Math.abs(positions[5] - ride.stretchZ(-1, 1.5, 4)[0]) < 1e-6, `stretched z ${positions[5]}`);
  assert.equal(part.positions[5], -1, "the part does not change");
  for (const corner of [1, 2]) {
    const edge = [0, 1, 2].map((k) => positions[3 * corner + k] - positions[k]);
    const dot = edge[0] * normals[0] + edge[1] * normals[1] + edge[2] * normals[2];
    assert.ok(Math.abs(dot) < 1e-6, `normal and edge ${corner}: dot ${dot}`);
  }
  assert.ok(Math.abs(Math.hypot(normals[0], normals[1], normals[2]) - 1) < 1e-6, "unit normal");
});

test("seatEye places the eye as writePodMatrix places the pod", () => {
  const eye = [0.2, 0.3, 0.5], scale = 1.5, yaw = 0.7, x = 10, top = 5, z = -4;
  const matrix = new Float32Array(16);
  ride.writePodMatrix(matrix, 0, x, top, z, yaw);
  const local = [eye[0], eye[1], ride.stretchZ(eye[2], scale, 4)[0]];
  near(local[2], 1.5, "the eye stays 1.5 m from the front");
  const want = [0, 1, 2].map((k) => local[0] * matrix[k] + local[1] * matrix[4 + k] + local[2] * matrix[8 + k] + matrix[12 + k]);
  const got = ride.seatEye(x, top, z, yaw, eye, scale, 4);
  for (let k = 0; k < 3; k++) assert.ok(Math.abs(got[k] - want[k]) < 1e-5, `axis ${k}: ${got[k]} != ${want[k]}`);
});

// fakeContext is a canvas 2D context that records its calls. A character
// of text is 0.6 of the font size wide.
function fakeContext() {
  const calls = [], state = { font: "10px sans-serif", textAlign: "left", fillStyle: "", globalCompositeOperation: "source-over" };
  const fontSize = () => Number(/([\d.]+)px/.exec(state.font)[1]);
  return new Proxy({}, {
    get(_, name) {
      if (name === "calls") return calls;
      if (name === "measureText") return (text) => ({ width: text.length * fontSize() * 0.6 });
      if (name in state) return state[name];
      return (...args) => { calls.push({ name, args, font: state.font, size: fontSize(), align: state.textAlign, style: state.fillStyle }); };
    },
    set(_, name, value) { state[name] = value; return true; },
  });
}

// textsInside checks that each text that context drew is inside a canvas of
// width by height and that no two texts overlap, and returns the texts. A
// text box reaches 0.75 of the font size above the baseline and 0.2 below.
function textsInside(context, width, height) {
  const boxes = [];
  for (const call of context.calls.filter((c) => c.name === "fillText")) {
    const [text, x, y] = call.args, w = text.length * call.size * 0.6;
    const left = call.align === "right" ? x - w : (call.align === "center" ? x - w / 2 : x);
    assert.ok(left >= -0.01 && left + w <= width + 0.01, `"${text}" spans ${left} to ${left + w}, outside 0 to ${width}`);
    assert.ok(y - 0.75 * call.size >= -0.01 && y <= height + 0.01, `"${text}" at y ${y} is outside 0 to ${height}`);
    const box = { text, left, right: left + w, top: y - 0.75 * call.size, bottom: y + 0.2 * call.size };
    for (const other of boxes) {
      const apart = box.right <= other.left || other.right <= box.left || box.bottom <= other.top || other.bottom <= box.top;
      assert.ok(apart, `"${text}" overlaps "${other.text}"`);
    }
    boxes.push(box);
  }
  return boxes.map((box) => box.text);
}

const longHud = {
  found: true, podID: "0123", class: "compact", activity: "Traveling to the next station of the long route", speed: 20, speedLimit: 22,
  waitReason: "Pod ahead on the merge at the junction", blockedBy: "0456",
  riders: [{ partySize: 2, from: "Custom House for ExCel", to: "Cutty Sark for Maritime Greenwich" }, { partySize: 1, from: "Bank", to: "Battersea Power Station" }],
  nextStop: "Cutty Sark for Maritime Greenwich", route: { traveledMeters: 300, remainingMeters: 900, seconds: 75, before: false, after: true },
  session: { speed: 1, paused: false, connected: true },
};

test("hudView gives the fields of the racing layout", () => {
  const view = ride.hudView(longHud);
  assert.equal(view.speedKmh, 72);
  assert.equal(view.limitKmh, 79);
  assert.equal(view.podName, "0123 (compact)");
  assert.equal(view.nextName, "Cutty Sark for Maritime Greenwich");
  assert.equal(view.eta, "over 1:15");
  assert.equal(view.time, "1:15");
  assert.equal(view.over, true);
  assert.equal(view.distance, "over 900 m");
  assert.equal(view.aboard, 3);
  const inside = ride.hudView({ ...longHud, speedLimit: 0, route: { ...longHud.route, after: false } });
  assert.equal(inside.eta, "1:15");
  assert.equal(inside.distance, "900 m");
  assert.equal(inside.limitKmh, 0);
  const missing = ride.hudView({ found: false, riders: [], session: {} });
  assert.equal(missing.nextName, "");
  assert.equal(missing.aboard, 0);
});

test("fitText cuts a text at a word boundary to fit a width", () => {
  const measure = (text) => text.length * 10;
  assert.equal(ride.fitText("Bank", 40, measure), "Bank");
  assert.equal(ride.fitText("Battersea Power Station", 200, measure), "Battersea Power…");
  assert.equal(ride.fitText("Battersea Power Station", 150, measure), "Battersea…");
  // A first word that is too wide loses characters.
  assert.equal(ride.fitText("Walthamstowcentralstation", 60, measure), "Walth…");
  for (const width of [20, 55, 95, 130]) assert.ok(measure(ride.fitText("Cutty Sark for Maritime Greenwich", width, measure)) <= width, `width ${width}`);
});

test("waitText cuts blockedBy before the wait reason", () => {
  const measure = (text) => text.length * 10;
  assert.equal(ride.waitText("Pod ahead", "", 200, measure), "Pod ahead");
  assert.equal(ride.waitText("Pod ahead", "0456", 200, measure), "Pod ahead (0456)");
  assert.equal(ride.waitText("Blocked by incident", "debris-0123456789", 300, measure), "Blocked by incident (debris-…)");
  assert.equal(ride.waitText("Blocked by incident", "debris-0123456789", 200, measure), "Blocked by incident");
  assert.equal(ride.waitText("Blocked by incident", "debris-0123456789", 150, measure), "Blocked by…");
});

test("stopLadder puts the stops behind the pod, the next stop, and the later stops in order", () => {
  const ladder = ride.stopLadder(longHud, 0.25);
  assert.deepEqual(ladder.stops, [
    { name: "Custom House for ExCel", state: "done" },
    { name: "Bank", state: "done" },
    { name: "Cutty Sark for Maritime Greenwich", state: "next" },
    { name: "Battersea Power Station", state: "later" },
  ]);
  assert.equal(ladder.oneLeg, false);
  const items = ride.ladderItems(ladder);
  assert.deepEqual(items.map((item) => (item.bar ? `bar ${item.progress}` : item.state)), ["done", "done", "bar 0.25", "next", "later"]);

  // One party: a bar from its origin to the next stop.
  const one = ride.stopLadder({ ...longHud, riders: [longHud.riders[1]], nextStop: "Battersea Power Station" }, 0.5);
  assert.equal(one.oneLeg, true);
  assert.deepEqual(one.stops.map((stop) => stop.state), ["done", "next"]);
  // An empty pod on its way to a pickup has a next stop and no origin.
  const empty = ride.stopLadder({ ...longHud, riders: [], nextStop: "Oval" }, -1);
  assert.deepEqual(empty.stops, [{ name: "Oval", state: "next" }]);
  assert.equal(empty.oneLeg, true);
  assert.deepEqual(ride.ladderItems(empty).map((item) => (item.bar ? "bar" : item.name)), ["bar", "Oval"]);
  assert.deepEqual(ride.stopLadder({ found: false }, 0.5), { stops: [], progress: -1, oneLeg: true });
  // At most three stops come after the next stop, and a stop shows once.
  const many = { ...longHud, nextStop: "A", riders: ["A", "B", "C", "D", "E", "B"].map((to) => ({ partySize: 1, from: "Bank", to })) };
  assert.deepEqual(ride.stopLadder(many, 0).stops.map((stop) => stop.name), ["Bank", "A", "B", "C", "D"]);
});

test("gaugeView scales the gauge above the speed and the limit", () => {
  const gauge = ride.gaugeView(50 / 3.6, 80 / 3.6);
  assert.equal(gauge.top, 100);
  near(gauge.speed, 0.5, "speed fraction");
  near(gauge.limit, 0.8, "limit fraction");
  assert.equal(gauge.over, false);
  assert.equal(ride.gaugeView(0, 0).top, 40);
  assert.equal(ride.gaugeView(0, 0).limit, -1);
  const over = ride.gaugeView(130 / 3.6, 100 / 3.6);
  assert.equal(over.top, 140);
  assert.equal(over.over, true);
});

test("hudMode puts the readouts in the cabin only in the seat view of a pod with screens", () => {
  assert.equal(ride.hudMode(false, false, true, true), "cabin");
  assert.equal(ride.hudMode(true, false, true, true), "overlay", "chase view");
  assert.equal(ride.hudMode(false, true, true, true), "overlay", "platform view");
  assert.equal(ride.hudMode(false, false, false, true), "overlay", "no pod model");
  assert.equal(ride.hudMode(false, false, true, false), "overlay", "no ridden pod");
});

test("the cabin screens draw each text inside the screen", () => {
  const model = ride.parsePodModel(podModelBuffer());
  const size = (name) => model.screens[name].scale.slice(0, 2).map((value) => Math.round(value * 2000));
  const view = ride.hudView(longHud), ladder = ride.stopLadder(longHud, view.progress);
  const near = { name: "Cutty Sark (for Maritime Greenwich)", distance: "1.2 km" };
  const draws = {
    speedScreen: (context, w, h) => ride.drawSpeedScreen(context, w, h, view),
    routeScreen: (context, w, h) => ride.drawRouteScreen(context, w, h, view, ladder, near),
    banner: (context, w, h) => ride.drawBanner(context, w, h, view),
  };
  const texts = {};
  for (const [name, draw] of Object.entries(draws)) {
    const [w, h] = size(name), context = fakeContext();
    draw(context, w, h);
    texts[name] = textsInside(context, w, h);
  }
  assert.ok(texts.speedScreen.includes("72"), texts.speedScreen.join(" | "));
  assert.ok(texts.speedScreen.includes("79 km/h"), "limit");
  assert.ok(texts.banner.includes("1:15") && texts.banner.includes("ETA, OVER"), texts.banner.join(" | "));
  assert.ok(texts.banner.some((text) => text.startsWith("Cutty Sark")), texts.banner.join(" | "));
  // The long wait reason does not fit, so it is cut at a word boundary.
  const wait = texts.routeScreen.find((text) => text.startsWith("Pod ahead"));
  assert.ok(wait && wait.endsWith("…") && "Pod ahead on the merge at the junction".startsWith(wait.slice(0, -1)), wait);
  assert.ok(texts.routeScreen.includes("3 aboard"), texts.routeScreen.join(" | "));
  // A station name loses the part in brackets before a word cut.
  assert.ok(texts.routeScreen.includes("Near Cutty Sark, 1.2 km"), texts.routeScreen.join(" | "));

  // The longest wait reason keeps a full pod ID on the route screen.
  {
    const hud = { ...longHud, waitReason: "No parking available", blockedBy: "0456" }, context = fakeContext();
    const [w, h] = size("routeScreen"), longView = ride.hudView(hud);
    ride.drawRouteScreen(context, w, h, longView, ride.stopLadder(hud, longView.progress), near);
    assert.ok(textsInside(context, w, h).includes("No parking available (0456)"));
  }
  // A ladder of seven rows draws each row apart.
  {
    const hud = { ...longHud, nextStop: "A", riders: ["A", "B", "C", "D"].map((to, i) => ({ partySize: 1, from: `Origin ${i % 2}`, to })) };
    const context = fakeContext(), [w, h] = size("routeScreen"), manyView = ride.hudView(hud), ladder7 = ride.stopLadder(hud, 0.5);
    assert.equal(ride.ladderItems(ladder7).length, 7);
    ride.drawRouteScreen(context, w, h, manyView, ladder7, near);
    textsInside(context, w, h);
  }

  // One leg draws a bar with the origin and the next stop, and no stop
  // ahead draws a message.
  const [w, h] = size("routeScreen");
  for (const hud of [{ ...longHud, riders: [{ partySize: 1, from: "Bank", to: "Oval" }], nextStop: "Oval" }, { ...longHud, riders: [], nextStop: "" }]) {
    const context = fakeContext(), oneView = ride.hudView(hud);
    ride.drawRouteScreen(context, w, h, oneView, ride.stopLadder(hud, oneView.progress), null);
    const drawn = textsInside(context, w, h);
    for (const want of hud.nextStop ? ["Bank", "Oval"] : ["No stop ahead"]) assert.ok(drawn.includes(want), drawn.join(" | "));
  }
});

test("screenQuad faces the eye and reads left to right and top up", () => {
  const model = ride.parsePodModel(podModelBuffer());
  for (const stretch of [1, 1.5]) {
    const eye = [model.eye[0], model.eye[1], ride.stretchZ(model.eye[2], stretch, model.length)[0]];
    for (const [name, screen] of Object.entries(model.screens)) {
      const quad = ride.screenQuad(screen, stretch, model.length), p = (i) => [0, 1, 2].map((k) => quad.positions[3 * i + k]);
      // In the scene, +x is to the right of the rider and +y is up. u grows
      // from corner 0 to corner 1, and v from corner 1 to corner 2.
      const right = p(1).map((value, k) => value - p(0)[k]), up = p(2).map((value, k) => value - p(1)[k]);
      assert.ok(right[0] > 0.5 * Math.hypot(...right), `${name}: u grows to the left`);
      assert.ok(up[1] > 0.8 * Math.hypot(...up), `${name}: v = 1 is not the top`);
      assert.deepEqual([...quad.uvs], [0, 0, 1, 0, 1, 1, 0, 1]);
      const center = [0, 1, 2].map((k) => (p(0)[k] + p(2)[k]) / 2), toEye = eye.map((value, k) => value - center[k]);
      const normal = [0, 1, 2].map((k) => quad.normals[k]), dot = normal.reduce((sum, value, k) => sum + value * toEye[k], 0);
      assert.ok(dot > 0.999 * Math.hypot(...toEye), `${name} faces away from the eye`);
      const { along } = windings(quad.positions, quad.normals, quad.indices);
      assert.equal(along, 0, `${name} winds as a back face`);
    }
  }
});

test("stretchZ matches the stretch of internal/cmd/podmodel", () => {
  const fixture = JSON.parse(require("node:fs").readFileSync(require("node:path").join(__dirname, "../internal/cmd/podmodel/testdata/stretch.json"), "utf8"));
  assert.equal(ride.STRETCH_CUT, fixture.cut, "the cut");
  for (const { scale, z, want, slope } of fixture.cases) {
    const [got, gotSlope] = ride.stretchZ(z, scale, fixture.modelLength);
    near(got, want, `z ${z} at scale ${scale}`);
    near(gotSlope, slope, `slope at z ${z} and scale ${scale}`);
  }
});

test("a longer class keeps the cabin front and stretches behind the rider seat", () => {
  const model = ride.parsePodModel(podModelBuffer());
  const scale = 1.5, shift = (scale - 1) * model.length / 2;
  // From the front of the pod, the eye and the screens do not move.
  const eye = (stretch) => ride.seatEye(0, 0, 0, 0, model.eye, stretch, model.length);
  near(eye(scale)[2] - shift, eye(1)[2], "eye z from the front");
  for (const [name, screen] of Object.entries(model.screens)) {
    const short = ride.screenQuad(screen, 1, model.length), long = ride.screenQuad(screen, scale, model.length);
    for (let i = 0; i < short.positions.length; i++) {
      const moved = i % 3 === 2 ? shift : 0;
      assert.ok(Math.abs(long.positions[i] - moved - short.positions[i]) < 1e-6, `${name} position ${i}: ${long.positions[i]} and ${short.positions[i]}`);
      assert.ok(Math.abs(long.normals[i] - short.normals[i]) < 1e-6, `${name} normal ${i}`);
    }
  }
  for (const [name, part] of Object.entries(model.parts)) {
    // The 4 m class keeps the model positions.
    assert.deepEqual(ride.stretchPart(part, 1, model.length).positions, part.positions, `${name} at scale 1`);
    const { positions, normals } = ride.stretchPart(part, scale, model.length);
    let behind = 0;
    for (let i = 2; i < positions.length; i += 3) {
      const z = part.positions[i];
      if (z >= ride.STRETCH_CUT) {
        assert.ok(Math.abs(positions[i] - shift - z) < 1e-6, `${name} vertex ${(i - 2) / 3} ahead of the cut moves by ${positions[i] - z}`);
        for (const k of [i - 2, i - 1, i]) assert.ok(Math.abs(normals[k] - part.normals[k]) < 1e-6, `${name} normal ${k} ahead of the cut`);
      } else {
        behind++;
        // Behind the cut, the part stretches, so each point moves back from
        // where a plain shift would put it.
        assert.ok(positions[i] - shift < z - 1e-6 || z > ride.STRETCH_CUT - 1e-6, `${name} vertex ${(i - 2) / 3} behind the cut does not stretch`);
      }
    }
    assert.ok(behind > 0, `${name} has no vertex behind the cut`);
    const { along } = windings(positions, normals, part.indices);
    assert.equal(along, 0, `${name}: ${along} triangles wind as back faces when stretched`);
  }
  // The body reaches the ends of the class length.
  const zs = Object.values(model.parts).flatMap((part) => [...ride.stretchPart(part, scale, model.length).positions.filter((_, i) => i % 3 === 2)]);
  assert.ok(Math.abs(Math.min(...zs) + scale * model.length / 2) < 1e-6, `rear end ${Math.min(...zs)}`);
  assert.ok(Math.abs(Math.max(...zs) - scale * model.length / 2) < 1e-6, `front end ${Math.max(...zs)}`);
});

test("attachTexture keeps the flat color when a texture does not load", () => {
  const made = [];
  class Texture {
    constructor(url, scene, noMipmap, invertY, sampling, onLoad, onError) { Object.assign(this, { url, onLoad, onError, disposed: false }); made.push(this); }
    dispose() { this.disposed = true; }
  }
  Texture.TRILINEAR_SAMPLINGMODE = 3;
  Texture.WRAP_ADDRESSMODE = 1;
  const B = { Texture }, warn = console.warn;
  const loads = { diffuseColor: "white" }, fails = { diffuseColor: "grey" };
  ride.attachTexture(B, null, "textures/concrete.png", [loads]);
  assert.equal(loads.diffuseTexture, undefined, "no texture before it loads");
  made[0].onLoad();
  assert.equal(loads.diffuseTexture, made[0]);
  assert.equal(made[0].wrapU, 1);
  ride.attachTexture(B, null, "textures/missing.png", [fails]);
  console.warn = () => {};
  try { made[1].onError("404"); } finally { console.warn = warn; }
  assert.equal(fails.diffuseTexture, undefined);
  assert.equal(fails.diffuseColor, "grey");
  assert.equal(made[1].disposed, true);
});

test("the concrete has texture coordinates in meters", () => {
  const nodes = new Map([["a", { x: 0, y: 0 }], ["b", { x: 40, y: 0 }]]);
  const beam = ride.beamGeometry([{ id: "l", from: "a", to: "b" }], nodes, null);
  assert.equal(beam.uvs.length / 2, beam.positions.length / 3);
  // The end of the 40 m beam is 10 texture repeats along.
  assert.equal(Math.max(...beam.uvs.filter((_, i) => i % 2 === 0)), 10);
  const deck = ride.deckGeometry([{ x: 30000, y: 0 }, { x: 30040, y: 0 }, { x: 30040, y: 40 }], 5);
  assert.equal(deck.uvs.length / 2, deck.positions.length / 3);
  // The deck coordinates start at its first point, so they stay small far
  // from the origin.
  assert.ok(deck.uvs.every((value) => Math.abs(value) <= 21), "deck coordinates are small");
  const uvs = ride.boxUVs([2, 0, 4, 2, 0, 4], [0, 1, 0, 1, 0, 0], 4);
  assert.deepEqual([...uvs], [0.5, 1, 1, 0]);
});

test("the mini-map draws in the square that the caller gives", () => {
  const topology = { nodes: [{ id: "a", position: { x: 0, y: 0 } }, { id: "b", position: { x: 100, y: 0 } }], lanes: [{ id: "l", from: "a", to: "b" }], stations: [] };
  const map = ride.createMiniMap(topology);
  for (const size of [240, 360]) {
    const context = fakeContext();
    map.draw(context, size, size / 240, 0, 0, 0, new Float64Array(0), 0, -1, -1);
    const clip = context.calls.find((call) => call.name === "arc");
    assert.deepEqual(clip.args.slice(0, 3), [size / 2, size / 2, size / 2]);
    const north = context.calls.find((call) => call.name === "fillText" && call.args[0] === "N");
    assert.equal(north.args[1], size / 2);
  }
});

test("the overlay has the racing layout", () => {
  const html = require("node:fs").readFileSync(require("node:path").join(__dirname, "ride.html"), "utf8");
  const rule = (selector) => {
    const match = new RegExp(`(?:^|\\n)\\s*${selector.replace(/[.#]/g, "\\$&")}\\s*\\{([^}]*)\\}`).exec(html);
    assert.ok(match, `no rule for ${selector}`);
    return Object.fromEntries(match[1].split(";").map((part) => part.split(":").map((value) => value.trim())).filter(([key]) => key));
  };
  const centered = (style) => style.left === "50%" && style.transform === "translateX(-50%)";
  // The speed gauge is at the bottom center, the map at the bottom left,
  // the next stop and the ladder at the top center, and the status at the
  // bottom right.
  assert.ok(centered(rule("#hudGauge")) && rule("#hudGauge").bottom, "gauge");
  assert.ok(rule("#minimap").left && rule("#minimap").bottom && rule("#minimap")["border-radius"] === "50%", "map");
  assert.ok(centered(rule("#hudBanner")) && rule("#hudBanner").top, "banner");
  assert.ok(centered(rule("#hudLadder")) && parseFloat(rule("#hudLadder").top) > parseFloat(rule("#hudBanner").top), "ladder under the banner");
  assert.ok(rule("#hudStatus").right && rule("#hudStatus").bottom, "status");
  assert.ok(rule("#panel").left && rule("#panel").top && rule("#strip").right && rule("#strip").top, "controls and strip");
  // In the seat view, the cabin screens show the readouts.
  assert.equal(rule("body.cabin #hud, body.cabin #minimap").display, "none");
  // The page code finds these elements.
  for (const id of ["hudSpeed", "hudLimit", "hudState", "hudWait", "hudTrip", "hudNext", "hudEta", "hudRoute", "hudStops", "hudProgress", "hudProgressBar", "hudNear", "gauge", "stripSession", "stripMessage", "rideAt1x", "hudPassenger", "minimap"]) {
    assert.ok(html.includes(`id="${id}"`), id);
  }
});

// cornerTopology is the corner of internal/ride/fillet_test.go: a lane
// east from a to b and a lane south from b to c, with the trims and the
// piece that the topology gives.
function cornerTopology() {
  const nodes = new Map([["a", { x: 0, y: 0 }], ["b", { x: 100, y: 0 }], ["c", { x: 100, y: 100 }]]);
  const lanes = [{ id: "east", from: "a", to: "b", trimEnd: 8 }, { id: "south", from: "b", to: "c", trimStart: 8 }];
  const pieces = [{ in: 0, out: 1, control: [100, 0] }];
  return { nodes, lanes, pieces };
}

test("beamGeometry of a 90 degree join draws the trimmed lanes and one curved piece", () => {
  const { nodes, lanes, pieces } = cornerTopology();
  const plan = ride.filletPlan(lanes, pieces, new Set(), nodes);
  assert.equal(plan.pieces.length, 1);
  const lanesOnly = ride.beamGeometry(plan.lanes, nodes, null);
  const both = ride.beamGeometry(plan.lanes, nodes, null, plan.pieces);
  const profile = ride.pieceProfile(plan.pieces[0], nodes, null);
  assert.equal(profile.length, 17, "a sharp turn gets the most segments");
  // Each beam has 4 faces, with 2 vertices for each point of each face.
  assert.equal(lanesOnly.positions.length / 3, 2 * 2 * 4 * 2);
  assert.equal(both.positions.length / 3 - lanesOnly.positions.length / 3, profile.length * 4 * 2);
  // The lane beams stop at the trims, and the piece meets them.
  const east = ride.laneProfile(plan.lanes[0], nodes, null), south = ride.laneProfile(plan.lanes[1], nodes, null);
  assert.equal(east.length, 2);
  near(east.at(-1).x, 92, "east end");
  near(south[0].y, 8, "south start");
  near(south.at(-1).y, 100, "south end");
  near(profile[0].x, 92, "piece start");
  near(profile.at(-1).y, 8, "piece end");
  // Half way, the piece is inside the corner, 2 * sqrt(2) m from the node.
  near(profile[8].x, 98, "piece middle x");
  near(profile[8].y, 2, "piece middle y");
  // The lane beams have pillars only between the trims.
  const long = new Map([["a", { x: 0, y: 0 }], ["b", { x: 200, y: 0 }]]);
  assert.deepEqual(ride.pillarPoints([{ from: "a", to: "b", trimStart: 8 }], long).map(([x]) => x), [38, 98, 158]);
});

test("filletPlan draws no piece for a hub lane, and the other lane reaches the node", () => {
  const { nodes, lanes, pieces } = cornerTopology();
  const plan = ride.filletPlan(lanes, pieces, new Set(["south"]), nodes);
  assert.equal(plan.pieces.length, 0);
  assert.equal(plan.lanes[0].trimEnd, 0);
  assert.equal(lanes[0].trimEnd, 8, "the topology lane does not change");
  near(ride.laneProfile(plan.lanes[0], nodes, null).at(-1).x, 100, "east end");
  assert.equal(ride.filletPlan(lanes, pieces, new Set(["east"]), nodes).pieces.length, 0);
});

test("a straight continuation gets a near-straight piece", () => {
  const turn = 2 * Math.PI / 180;
  const nodes = new Map([["a", { x: 0, y: 0 }], ["b", { x: 100, y: 0 }], ["c", { x: 100 + 100 * Math.cos(turn), y: 100 * Math.sin(turn) }]]);
  const lanes = [{ id: "in", from: "a", to: "b", trimEnd: 8 }, { id: "out", from: "b", to: "c", trimStart: 8 }];
  const end = [100 + 8 * Math.cos(turn), 8 * Math.sin(turn)];
  const plan = ride.filletPlan(lanes, [{ in: 0, out: 1, control: [100, 0] }], new Set(), nodes);
  const profile = ride.pieceProfile(plan.pieces[0], nodes, null);
  assert.ok(profile.length >= 2 && profile.length <= 4, `${profile.length} points`);
  // Each point is within 10 cm of the line from the start to the end.
  for (const point of profile) {
    const gap = Math.abs((point.x - 92) * end[1] - point.y * (end[0] - 92)) / Math.hypot(end[0] - 92, end[1]);
    assert.ok(gap < 0.1, `point ${point.x}, ${point.y} is ${gap} m off the line`);
  }
  // A join with no turn is one segment.
  const line = new Map([...nodes, ["c", { x: 200, y: 0 }]]);
  const straight = ride.filletPlan(lanes, [{ in: 0, out: 1, control: [100, 0] }], new Set(), line);
  assert.equal(ride.pieceProfile(straight.pieces[0], line, null).length, 2);
});

test("a piece follows the height of the lane parts that it replaces", () => {
  const nodes = new Map([["m", { x: 0, y: 0 }], ["s", { x: 100, y: 0 }], ["t", { x: 100, y: 100 }]]);
  const lanes = [
    { id: "down", from: "m", to: "s", fromDistance: 0, toDistance: 100, trimEnd: 8 },
    { id: "turn", from: "s", to: "t", fromDistance: 100, toDistance: 120, trimStart: 8 },
  ];
  const plan = ride.filletPlan(lanes, [{ in: 0, out: 1, control: [100, 0] }], new Set(), nodes);
  const profile = ride.pieceProfile(plan.pieces[0], nodes, LEVELS);
  near(profile[0].h, ride.laneProfile(lanes[0], nodes, LEVELS).at(-1).h, "start height");
  near(profile[0].h, ride.levelHeight(92, LEVELS), "start height on the curve");
  near(profile.at(-1).h, ride.laneProfile(lanes[1], nodes, LEVELS)[0].h, "end height");
  near(profile.at(-1).h, ride.levelHeight(101.6, LEVELS), "end height on the curve");
  // Half way along the piece is half way along the two lane parts: the node.
  near(profile[(profile.length - 1) / 2].h, ride.levelHeight(100, LEVELS), "middle height");
});

test("the beams of the lanes meet the pieces of internal/ride", () => {
  const fixture = JSON.parse(require("node:fs").readFileSync(require("node:path").join(__dirname, "../internal/ride/testdata/fillets.json"), "utf8"));
  const nodes = new Map(fixture.nodes.map((node) => [node.id, node.position]));
  const plan = ride.filletPlan(fixture.lanes, fixture.pieces, new Set(), nodes);
  assert.equal(plan.pieces.length, fixture.ends.length);
  plan.pieces.forEach((piece, i) => {
    const name = `${piece.from.id} to ${piece.to.id}`;
    // The page finds the ends of the bridge pieces to float precision.
    const [x0, y0, x1, y1] = fixture.ends[i];
    assert.ok(Math.hypot(piece.start.x - x0, piece.start.y - y0) < 1e-9 && Math.hypot(piece.end.x - x1, piece.end.y - y1) < 1e-9, `${name}: ends ${JSON.stringify(piece)}`);
    const before = ride.laneProfile(piece.from, nodes, fixture.levels).at(-1), after = ride.laneProfile(piece.to, nodes, fixture.levels)[0];
    const profile = ride.pieceProfile(piece, nodes, fixture.levels);
    for (const [got, want, end] of [[profile[0], before, "start"], [profile.at(-1), after, "end"]]) {
      assert.ok(Math.hypot(got.x - want.x, got.y - want.y) < 1e-9, `${name} ${end}: ${got.x}, ${got.y} is not at ${want.x}, ${want.y}`);
      assert.ok(Math.abs(got.h - want.h) < 1e-9, `${name} ${end} height: ${got.h} and ${want.h}`);
    }
  });
  // The 10.0787 m lane has a trim of 40%, rounded to 1 mm.
  assert.equal(fixture.lanes.find((lane) => lane.id === "odd").trimStart, 4.031);
});

test("lookOptions turns each part of the polish off by name or all by polish=0", () => {
  assert.deepEqual(ride.lookOptions(""), { tint: true, rail: true, taper: true, sky: true, gloss: true, lights: true });
  assert.deepEqual(ride.lookOptions("?pod=01&tint=0"), { tint: false, rail: true, taper: true, sky: true, gloss: true, lights: true });
  // Only 0 turns a switch off.
  assert.deepEqual(ride.lookOptions("?tint=1"), { tint: true, rail: true, taper: true, sky: true, gloss: true, lights: true });
  assert.deepEqual(ride.lookOptions("?polish=0&tint=1"), { tint: false, rail: false, taper: false, sky: false, gloss: false, lights: false });
});

test("railGeometry runs a strip along the middle of each beam top", () => {
  // A lane to a fork at b: one branch goes on, and a curve turns away.
  const nodes = new Map([["a", { x: 0, y: 0 }], ["b", { x: 40, y: 0 }], ["c", { x: 80, y: 0 }], ["d", { x: 80, y: -30 }]]);
  const lanes = [{ id: "ab", from: "a", to: "b" }, { id: "bc", from: "b", to: "c" }, { id: "bd", from: "b", to: "d", control: { x: 60, y: 0 } }, { id: "lost", from: "a", to: "missing" }];
  const profiles = ride.guidewayProfiles(lanes, nodes, null);
  const rail = ride.railGeometry(profiles);
  const drawn = profiles.filter(Boolean), points = drawn.flat();
  assert.equal(rail.positions.length / 3, 2 * points.length);
  assert.equal(rail.indices.length, 6 * (points.length - drawn.length));
  points.forEach((point, i) => {
    const a = rail.positions.subarray(6 * i, 6 * i + 3), b = rail.positions.subarray(6 * i + 3, 6 * i + 6);
    const [x, z] = ride.toScene(point.x, point.y);
    // The strip is centered on the beam, narrower than its top, and a
    // little above it.
    // The positions are float32.
    assert.ok(Math.abs((a[0] + b[0]) / 2 - x) < 1e-5 && Math.abs((a[2] + b[2]) / 2 - z) < 1e-5, `center ${i}`);
    assert.ok(Math.abs(Math.hypot(a[0] - b[0], a[2] - b[2]) - 0.12) < 1e-4, `width ${i}`);
    assert.ok(Math.abs(a[1] - (ride.BEAM_TOP + 0.05)) < 1e-5 && a[1] === b[1], `height ${i}`);
  });
  // The rails of the two branches start together at the fork. The first
  // side of a curve follows its first chord, so only the centers match.
  const center = (profile) => {
    const at = 6 * drawn.slice(0, drawn.indexOf(profile)).reduce((sum, p) => sum + p.length, 0), p = rail.positions;
    return [(p[at] + p[at + 3]) / 2, (p[at + 2] + p[at + 5]) / 2];
  };
  const [, onward, turn] = drawn;
  assert.deepEqual(center(onward), center(turn));
  for (const index of rail.indices) assert.ok(index < rail.positions.length / 3);
});

test("a tapered beam has a narrow bottom and sides that face out and down", () => {
  const nodes = new Map([["a", { x: 0, y: 0 }], ["b", { x: 10, y: 0 }]]);
  const box = ride.beamGeometry([{ from: "a", to: "b" }], nodes, null);
  const tapered = ride.beamGeometry([{ from: "a", to: "b" }], nodes, null, [], 0.9);
  assert.equal(tapered.positions.length, box.positions.length);
  assert.deepEqual(tapered.indices, box.indices);
  for (let i = 0; i < tapered.positions.length; i += 3) {
    // The beam runs along x, so z is the offset from its middle.
    const y = tapered.positions[i + 1], z = tapered.positions[i + 2], [nx, ny, nz] = tapered.normals.subarray(i, i + 3);
    // The positions and the normals are float32.
    const bottom = Math.abs(y - (ride.BEAM_TOP - 0.7)) < 1e-5;
    assert.ok(Math.abs(Math.abs(z) - (bottom ? 0.45 : 0.7)) < 1e-6, `offset ${z} at height ${y}`);
    assert.ok(Math.abs(Math.hypot(nx, ny, nz) - 1) < 1e-6, "unit normal");
    if (Math.abs(nz) > 0.5) {
      // A side: out from the middle, and down, square to the slope.
      assert.ok(nz * z > 0 && ny < 0, `side normal ${[nx, ny, nz]}`);
      assert.ok(Math.abs(Math.abs(nz) * 0.25 - Math.abs(ny) * 0.7) < 1e-6, "normal square to the side");
    }
  }
});

test("skyColor keeps the fog color at the horizon and glows toward the sun", () => {
  const close = (a, b, message) => a.forEach((value, i) => near(value, b[i], `${message} ${i}`));
  const toSun = [0.6, 0.8, 0];
  const polished = ride.skyLook({ sky: true }), plain = ride.skyLook({ sky: false });
  for (const sky of [polished, plain]) {
    // The horizon is the fog color in each direction, also toward the sun.
    for (const direction of [[1, 0, 0], [0, 0, 1], [-1, 0, 0]]) close(ride.skyColor(direction, sky, toSun), sky.horizon, "horizon");
    // The sky dome below the horizon has the fog color.
    close(ride.skyColor([0.6, -0.8, 0], sky, toSun), sky.horizon, "below");
  }
  // Without the polish, the sky is the gradient from before.
  close(ride.skyColor([0.8, 0.6, 0], plain, toSun), [0.74, 0.82, 0.9].map((value, c) => value + ([0.25, 0.45, 0.78][c] - value) * Math.sqrt(0.6)), "plain gradient");
  // At one height, the sky is brighter toward the sun than away from it.
  const toward = ride.skyColor([0.6, 0.8, 0], polished, toSun), away = ride.skyColor([-0.6, 0.8, 0], polished, toSun);
  assert.ok(toward[0] > away[0] + 0.2, `glow ${toward} against ${away}`);
  // Just above the horizon, the glow is still faint, so the sky meets the
  // fog without a band.
  const low = [Math.sqrt(1 - 0.02 * 0.02), 0.02, 0], lowGlow = ride.skyColor(low, polished, toSun)[0] - ride.skyColor(low, { ...polished, glow: null }, toSun)[0];
  assert.ok(lowGlow < 0.005, `glow ${lowGlow} near the horizon`);
  // At 90 degrees from the sun, the glow ends.
  close(ride.skyColor([-0.8, 0.6, 0], polished, toSun), ride.skyColor([-0.8, 0.6, 0], { ...polished, glow: null }, toSun), "no glow away from the sun");
  // A below color shows under the horizon.
  close(ride.skyColor([0, -0.5, Math.sqrt(0.75)], { ...polished, below: [0.3, 0.3, 0.3] }, toSun), [0.3, 0.3, 0.3], "ground");
  assert.ok(toward.every((value) => value <= 1), "the glow stays in range");
});

// cubeLookup is the cube map lookup of the WebGL specification: the face
// and the s, t of a direction.
function cubeLookup([x, y, z]) {
  const ax = Math.abs(x), ay = Math.abs(y), az = Math.abs(z);
  let face, sc, tc, ma;
  if (ax >= ay && ax >= az) [face, sc, tc, ma] = x > 0 ? [0, -z, -y, ax] : [1, z, -y, ax];
  else if (ay >= az) [face, sc, tc, ma] = y > 0 ? [2, x, z, ay] : [3, x, -z, ay];
  else [face, sc, tc, ma] = z > 0 ? [4, x, -y, az] : [5, -x, -y, az];
  return [face, (sc / ma + 1) / 2, (tc / ma + 1) / 2];
}

test("cubeDirection is the inverse of the WebGL cube lookup", () => {
  for (let face = 0; face < 6; face++) {
    for (const [s, t] of [[0.5, 0.5], [0.1, 0.8], [0.9, 0.3], [0.25, 0.05]]) {
      const direction = ride.cubeDirection(face, s, t);
      near(Math.hypot(...direction), 1, "unit");
      const [back, bs, bt] = cubeLookup(direction);
      assert.equal(back, face, `face of ${direction}`);
      near(bs, s, `s on face ${face}`);
      near(bt, t, `t on face ${face}`);
    }
  }
});

test("skyCubeFaces puts the sky up and the ground down", () => {
  const size = 8, sky = ride.skyLook({ sky: true }), faces = ride.skyCubeFaces(size, sky, [0.6, 0.8, 0]);
  assert.equal(faces.length, 6);
  const texel = (face, s, t) => [...faces[face].subarray((t * size + s) * 4, (t * size + s) * 4 + 4)];
  for (const face of faces) assert.equal(face.length, size * size * 4);
  // The bottom face is the ground everywhere, and the top face is sky.
  const ground = [0.3, 0.32, 0.29].map((value) => Math.round(value * 255));
  for (let i = 0; i < size; i++) assert.deepEqual(texel(3, i, size - 1 - i), [...ground, 255]);
  assert.ok(texel(2, 4, 4)[2] > texel(2, 4, 4)[0], "the top is blue");
  // On each side face, the first row is the sky and the last row is the
  // ground.
  for (const face of [0, 1, 4, 5]) {
    assert.ok(texel(face, 4, 0)[2] > 200, `sky at the top of face ${face}`);
    assert.deepEqual(texel(face, 4, size - 1), [...ground, 255], `ground at the bottom of face ${face}`);
  }
});

test("podLightTexels lights the noses and a side line in the livery stripe", () => {
  const width = 64, height = 512, data = ride.podLightTexels(width, height);
  assert.equal(data.length, width * height * 4);
  // texel reads the color at z of the model and dy above the middle of the
  // stripe, as the shell texture coordinates and the texture scale give it.
  const texel = (z, dy) => {
    const u = z / 4 + 0.5, v = 0.5 + dy / 2.1;
    const c = Math.min(width - 1, Math.floor(u * width)), r = Math.min(height - 1, Math.floor(v * height));
    return [...data.subarray((r * width + c) * 4, (r * width + c) * 4 + 3)];
  };
  assert.deepEqual(texel(1.9, 0), [235, 247, 255], "head light");
  assert.deepEqual(texel(-1.9, 0.03), [255, 31, 20], "tail light");
  assert.deepEqual(texel(0.5, 0), [26, 99, 128], "side line");
  for (const [z, dy] of [[1.9, 0.08], [-1.9, -0.08], [0.5, 0.03], [1.2, 0.4], [0, -0.3]]) assert.deepEqual(texel(z, dy), [0, 0, 0], `dark at ${z}, ${dy}`);
  // The rows are symmetric, so the direction of v does not matter.
  for (let r = 0; r < height; r++) assert.deepEqual(data.subarray(r * width * 4, (r + 1) * width * 4), data.subarray((height - 1 - r) * width * 4, (height - r) * width * 4), `row ${r}`);
  // The shell of the pod model has u = z, inside the span of the texture.
  const glb = require("node:fs").readFileSync(require("node:path").join(__dirname, "pod.glb"));
  const shell = ride.parsePodModel(glb.buffer.slice(glb.byteOffset, glb.byteOffset + glb.length)).parts.shell;
  // v is 0.5 at the middle of the stripe, 0.45 m below the body middle.
  for (let i = 0; i < shell.uvs.length / 2; i++) {
    near(shell.uvs[2 * i], shell.positions[3 * i + 2], `u of vertex ${i}`);
    assert.ok(Math.abs(shell.uvs[2 * i + 1] - (0.5 + (shell.positions[3 * i + 1] + 0.45) / 2.1)) < 1e-6, `v of vertex ${i}`);
  }
  assert.ok(shell.uvs.every((value, i) => i % 2 === 1 || Math.abs(value) < 2), "u inside the span");
});
