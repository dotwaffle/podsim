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

test("smoothAngle takes the shorter way and converges", () => {
  near(ride.wrapAngle(3 * Math.PI), Math.PI, "wrap");
  near(ride.wrapAngle(-Math.PI), Math.PI, "wrap lower bound");
  // From just below pi to just above -pi is a short step across the seam.
  const step = ride.smoothAngle(Math.PI - 0.1, -Math.PI + 0.1, 0.15, 0.15);
  assert.ok(Math.abs(ride.wrapAngle(step - Math.PI)) < 0.1, `step ${step} went the long way`);
  near(ride.smoothAngle(NaN, 1, 0.016, 0.15), 1, "first value");
  near(ride.smoothAngle(0, 1, 10, 0.15), 1, "long time");
  near(ride.smoothAngle(0, 1, 0, 0.15), 0, "no time");
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
