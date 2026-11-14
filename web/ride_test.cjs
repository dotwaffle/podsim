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
  assert.deepEqual(Object.keys(model.parts).sort(), ["cabin", "gear", "glass", "shell"]);
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
  // The triangle slopes along z, so a stretch in z turns its normal.
  const a = [0, 0, 0], b = [1, 0, 1], c = [0, 1, 0];
  const normal = [-1, 0, 1].map((value) => value / Math.sqrt(2));
  const part = { positions: Float32Array.from([...a, ...b, ...c]), normals: Float32Array.from([...normal, ...normal, ...normal]) };
  const { positions, normals } = ride.stretchPart(part, 1.5);
  near(positions[5], 1.5, "stretched z");
  assert.equal(part.positions[5], 1, "the part does not change");
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
  const local = [eye[0], eye[1], eye[2] * scale];
  const want = [0, 1, 2].map((k) => local[0] * matrix[k] + local[1] * matrix[4 + k] + local[2] * matrix[8 + k] + matrix[12 + k]);
  const got = ride.seatEye(x, top, z, yaw, eye, scale);
  for (let k = 0; k < 3; k++) assert.ok(Math.abs(got[k] - want[k]) < 1e-5, `axis ${k}: ${got[k]} != ${want[k]}`);
});
