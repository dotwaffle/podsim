"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const ride = require("./ride.js");

const near = (actual, expected, message) => assert.ok(Math.abs(actual - expected) < 1e-9, `${message}: ${actual} != ${expected}`);

// forwardOf turns the forward axis (+z) by a Babylon yaw. It uses the same
// matrix as writePodMatrix.
function forwardOf(yaw) {
  const matrix = new Float32Array(16);
  ride.writePodMatrix(matrix, 0, 0, 0, yaw);
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
  assert.deepEqual(slabs, [{ id: "s", name: "Stop", x: 10, z: -5, width: 30, depth: 20 }]);
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
});

test("smoothPodYaws starts at the heading and then follows it", () => {
  const poses = new Float64Array([0, 0, 0, 0, 0, 5, 5, Math.PI / 2, 0, 0]);
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
  assert.deepEqual(stops, [{ name: "Alpha", x: 10, y: 0 }, { name: "b", x: 100, y: 0 }]);
  assert.deepEqual(ride.nearestStop(stops, 90, 0), { name: "b", distance: 10 });
  assert.deepEqual(ride.nearestStop(stops, 10, 30), { name: "Alpha", distance: 30 });
  // A tie goes to the first stop.
  assert.equal(ride.nearestStop(stops, 55, 0).name, "Alpha");
  assert.equal(ride.nearestStop([], 0, 0), null);
});
