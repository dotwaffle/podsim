"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const editor = require("./editor.js");
const frame = { south: 51.5, west: -0.14, north: 51.51, east: -0.12, source: "equirectangular" };
const point = (x, y) => ({ lon: -0.14 + x * 0.002, lat: 51.5 + y * 0.001 });
const way = (id, nodes, coords, tags = {}) => ({ type: "way", id, nodes, geometry: coords, tags });
const ring = (id, start, coords, tags = {}) => way(id, [...coords.slice(0, -1).map((_, i) => start + i), start], coords, tags);
const water = { natural: "water" };
const square = [point(1, 1), point(9, 1), point(9, 9), point(1, 9), point(1, 1)];
const smaller = [point(3, 3), point(7, 3), point(7, 7), point(3, 7), point(3, 3)];
const parse = (elements, limits) => editor.overpassGeometry({ elements }, frame, limits);

function clientFixture(body = { elements: [] }, options = {}) {
  const calls = []; const writes = []; const finishes = []; let admission = options.admission || { allowance: editor.OVERPASS_LIMITS.responseBytes };
  const store = { admit: async () => admission, finish: async (...args) => { finishes.push(args); }, put: async (...args) => { writes.push(args); } };
  const client = editor.createOverpassClient({ store, fetch: async (url, init) => { calls.push({ url, init }); return options.response || new Response(typeof body === "string" ? body : JSON.stringify(body), { status: options.status || 200, headers: { "content-type": options.type || "application/json" } }); }, token: () => "test-token", now: () => 1700000000000, ...options.dependencies });
  return { client, store, calls, writes, finishes, setAdmission(value) { admission = value; }, load: (signal = new AbortController().signal) => client.load("https://example.test/api/interpreter", frame, signal) };
}

test("Overpass endpoint and area tiers reject invalid inputs before fetch", () => {
  for (const endpoint of ["", "http://example.test", "https://user:pass@example.test", "https://example.test?a=b", "https://example.test#x", `https://example.test/${"x".repeat(200)}`]) assert.throws(() => editor.overpassEndpoint(endpoint));
  assert.equal(editor.overpassEndpoint("https://example.test"), "https://example.test/");
  const small = editor.overpassQuery(frame); assert.equal(small.tier, 25); assert.match(small.query, /way\(r\)/); assert.match(small.query, /leisure/);
  const medium = editor.overpassQuery({ ...frame, east: 0, north: 51.6 }); assert.equal(medium.tier, 400); assert.match(medium.query, /tertiary/); assert.doesNotMatch(medium.query, /leisure/);
  const large = editor.overpassQuery({ ...frame, east: 0.2, north: 51.7 }); assert.equal(large.tier, 2500); assert.doesNotMatch(large.query, /tertiary/);
  for (const change of [{ north: Infinity }, { west: NaN }, { east: -0.2 }, { south: 80 }, { east: 5, north: 55 }, { south: "51" }, { source: "bad" }]) assert.throws(() => editor.overpassQuery({ ...frame, ...change }));
});

test("Overpass preserves gaps and clips neighbors beyond the old margin", () => {
  const result = parse([way(1, [1, 2, 3, 4, 5], [point(1, 1), point(20, 1), null, point(1, 5), point(9, 5)])]);
  assert.equal(result.lines.length, 2); assert.equal(result.stats.missingCoordinates, 1);
  assert.equal(result.lines[0].points[1].lon, frame.east);
  assert.equal(result.lines[1].points[0].lat, point(1, 5).lat);
  assert.equal(editor.overpassClipLine(point(-5, -5), point(-2, -2), frame), null);
});

test("Overpass assembles identity rings and preserves holes before clipping", () => {
  const a = way(1, [10, 11, 12], square.slice(0, 3)); const b = way(2, [10, 13, 12], [square[0], square[3], square[2]]);
  const hole = ring(3, 20, smaller);
  const relation = { type: "relation", id: 9, tags: water, members: [1, 2, 3].map((ref) => ({ type: "way", ref, role: ref === 3 ? "inner" : "outer" })) };
  const result = parse([a, b, hole, relation]);
  assert.equal(result.polygons.length, 1); assert.equal(result.polygons[0].rings.length, 2); assert.equal(result.stats.omittedFills, 0);
  assert.equal(editor.overpassSafeRings([square], [smaller]), true);
  const outside = square.map((p) => ({ ...p, lon: p.lon + 1 }));
  assert.equal(editor.overpassSafeRings([square], [outside]), false);
  const clipped = editor.overpassClipRing([point(-3, -3), point(20, -3), point(20, 20), point(-3, 20), point(-3, -3)], frame);
  assert.equal(clipped.length, 4);
  for (const p of clipped) { assert.ok(p.lon >= frame.west && p.lon <= frame.east); assert.ok(p.lat >= frame.south && p.lat <= frame.north); }
});

test("Overpass omits open, missing, ambiguous and self-intersecting polygon fills", () => {
  const cases = [
    way(1, [1, 2, 3, 4], square.slice(0, 4), water),
    way(1, [1, 2, 3, 4, 1], [square[0], square[1], null, square[3], square[0]], water),
    way(1, undefined, square, water),
    ring(1, 1, [square[0], square[2], square[1], square[3], square[0]], water),
  ];
  for (const item of cases) { const result = parse([item]); assert.equal(result.polygons.length, 0); assert.equal(result.stats.omittedFills, 1); assert.ok(result.lines.length > 0); }
  const relation = { type: "relation", id: 1, tags: water, members: [{ type: "way", ref: 1, role: "outer", geometry: square }] };
  assert.equal(parse([relation]).stats.omittedFills, 1);
  assert.equal(parse([relation]).lines.length, 4);
  assert.equal(editor.overpassRings([way(1, [1, 2], square.slice(0, 2)), way(2, [2, 1], square.slice(0, 2).reverse()), way(3, [1, 3, 1], [square[0], square[2], square[0]])]), null);
  assert.equal(editor.overpassSafeRings([square], [smaller], { remaining: 0 }), false);
  assert.equal(editor.overpassSafeRings([square, smaller], []), false);
  assert.equal(editor.overpassSafeRings([square], [smaller, smaller]), false);
});

test("Overpass rejects malformed elements and all geometry caps", () => {
  const invalid = [null, {}, { elements: {} }, { elements: [], remark: "runtime" }, { elements: [{ type: "bad", id: 1, members: [] }] }, { elements: [{ type: "node", id: 1, lat: 91, lon: 0 }] }, { elements: [{ type: "node", id: 1, lat: 0, lon: "0" }] }];
  for (const body of invalid) assert.throws(() => editor.overpassGeometry(body, frame));
  for (const item of [way(1, [0, 2], square.slice(0, 2)), way(1, [1], square.slice(0, 2)), way(1, [1, 2], {}), { type: "relation", id: 1, members: {} }, { type: "relation", id: 1, members: [{ type: "way", ref: 1 }] }, way(1, [1, 2], square.slice(0, 2), { highway: 1 })]) assert.throws(() => parse([item]));
  for (const coords of [{ lat: NaN, lon: 0 }, { lat: 0, lon: Infinity }, { lat: 0, lon: 181 }, { lat: null, lon: 0 }]) assert.throws(() => parse([{ type: "node", id: 1, ...coords }]), /coordinate/);
  for (const member of [{ type: "bad", ref: 1, role: "outer" }, { type: "way", ref: 0, role: "outer" }, { type: "way", ref: 1, role: null }]) assert.throws(() => parse([{ type: "relation", id: 1, members: [member] }]), /member/);
  assert.throws(() => parse([way(0, [1, 2], square.slice(0, 2))]), /element/);
  assert.throws(() => parse([way(1, [1, 2], square.slice(0, 2), [])]), /tags/);
  assert.throws(() => parse([way(1, undefined, [null, null])], { ...editor.OVERPASS_LIMITS, vertices: 1 }), /raw vertex/);
  assert.throws(() => parse([way(1, [1, 2], square.slice(0, 2)), way(2, [1, 3], square.slice(1, 3))]), /conflicting/);
  assert.throws(() => parse([way(1, [1, 2], square.slice(0, 2)), way(1, [3, 4], square.slice(0, 2))]), /duplicate/);
  const limits = editor.OVERPASS_LIMITS;
  assert.throws(() => parse([way(1, undefined, square)], { ...limits, vertices: 4 }), /raw vertex/);
  assert.throws(() => parse([ring(1, 1, square)], { ...limits, vertices: 6 }), /clipped vertex/);
  assert.throws(() => parse([ring(1, 1, square)], { ...limits, ways: 0 }), /way limit/);
  assert.throws(() => parse([{ type: "relation", id: 1, members: [] }], { ...limits, relations: 0 }), /relation limit/);
  assert.throws(() => parse([{ type: "relation", id: 1, members: [{ type: "way", ref: 1, role: "outer" }] }], { ...limits, members: 0 }), /member limit/);
});

test("Overpass requests omit credentials and preserve source, query and notice", async () => {
  const f = clientFixture(); const result = await f.load();
  assert.equal(f.calls.length, 1); assert.equal(f.calls[0].init.credentials, "omit"); assert.equal(f.calls[0].init.mode, "cors"); assert.equal(f.calls[0].init.redirect, "error");
  assert.equal(f.calls[0].init.body.get("data"), result.plan.query);
  assert.equal(result.license.method, result.plan.query); assert.match(result.license.notice, /Mixed-source data is not automatically exempt/);
  assert.equal(editor.licenseError(result.license), ""); assert.equal(f.writes.length, 1); assert.equal(f.finishes.length, 1);
});

test("Overpass rejects HTTP, content type, runtime, truncated, and CORS failures without caching", async () => {
  for (const status of [429, 406, 504, 500, 204]) {
    const f = clientFixture({}, { response: new Response(status === 204 ? null : "{}", { status, headers: { "content-type": "application/json" } }) });
    await assert.rejects(f.load(), /HTTP/); assert.equal(f.writes.length, 0); assert.equal(f.calls.length, 1); assert.equal(f.finishes[0][2], [429, 406, 504].includes(status));
  }
  for (const f of [clientFixture({ elements: [] }, { type: "text/html" }), clientFixture({ elements: [], remark: "timeout" }), clientFixture('{"elements":['), clientFixture({}, { dependencies: { fetch: async () => { throw new TypeError("Failed to fetch"); } } })]) {
    await assert.rejects(f.load()); assert.equal(f.writes.length, 0); assert.equal(f.client.busy, false);
  }
});

test("Overpass stream overflow cancels the reader and charges consumed bytes", async () => {
  let canceled = false;
  const stream = new ReadableStream({ start(controller) { controller.enqueue(new Uint8Array(101)); setTimeout(() => { if (!canceled) controller.close(); }, 0); }, cancel() { canceled = true; } });
  const f = clientFixture({}, { response: new Response(stream, { headers: { "content-type": "application/json" } }), admission: { allowance: 100 } });
  await assert.rejects(f.load(), /byte limit/); assert.equal(canceled, true); assert.equal(f.finishes[0][1], 101); assert.equal(f.writes.length, 0);
});

test("Overpass cache hits make no network request and retain retrieval time", async () => {
  const text = JSON.stringify({ elements: [] }); const f = clientFixture({}, { admission: { cached: { text, bytes: text.length, created: 1600000000000 } } });
  const result = await f.load(); assert.equal(result.cached, true); assert.equal(result.license.retrieved, new Date(1600000000000).toISOString()); assert.equal(f.calls.length, 0); assert.equal(f.finishes.length, 0);
  f.setAdmission({ cached: { text, bytes: 1, created: 1600000000000 } }); await assert.rejects(f.load(), /byte count/);
});

test("Overpass cancellation, timeout, concurrent calls and stale ownership cannot cache", async () => {
  let release; let entered;
  const started = new Promise((resolve) => { entered = resolve; });
  const wait = new Promise((resolve) => { release = resolve; });
  const f = clientFixture({}, { dependencies: { fetch: async (_, { signal }) => { entered(); await wait; signal.throwIfAborted(); return new Response('{"elements":[]}', { headers: { "content-type": "application/json" } }); } } });
  const controller = new AbortController(); const pending = f.load(controller.signal); await started;
  await assert.rejects(f.load(), /already active/); controller.abort(); release(); await assert.rejects(pending, { name: "AbortError" }); assert.equal(f.writes.length, 0);
  let timeout;
  const g = clientFixture({}, { dependencies: { clock: { setTimeout(fn) { timeout = fn; return 1; }, clearTimeout() {} }, fetch: async (_, { signal }) => { timeout(); signal.throwIfAborted(); } } });
  await assert.rejects(g.load(), { name: "TimeoutError" }); assert.equal(g.writes.length, 0);
  const stale = clientFixture(); stale.store.finish = async () => { throw new Error("stale owner"); };
  await assert.rejects(stale.load(), /stale owner/); assert.equal(stale.writes.length, 0);
});

test("Overpass cache failures do not block import and low space skips the cache", async () => {
  const f = clientFixture({ elements: [] }, { dependencies: { estimate: async () => ({ quota: 1, usage: 0 }) } }); await f.load(); assert.equal(f.writes.length, 0);
  const g = clientFixture(); g.store.put = async () => { throw new Error("quota"); }; await g.load();
});

test("Overpass ledger rejects corruption and backward clock but carries active reservations across midnight", () => {
  const now = 1700000000000; const day = Math.floor(now / 86400000);
  assert.deepEqual(editor.overpassBudget(null, now), { day, requests: 0, bytes: 0, pauseUntil: 0, active: null });
  assert.throws(() => editor.overpassBudget({ day, requests: -1, bytes: 0, pauseUntil: 0 }, now), /invalid/);
  assert.throws(() => editor.overpassBudget({ day: day + 1, requests: 1, bytes: 0, pauseUntil: 0 }, now), /backward/);
  const active = { token: "old", allowance: 10, started: now - 100000, day: day - 1 };
  assert.deepEqual(editor.overpassBudget({ day: day - 1, requests: 1, bytes: 10, pauseUntil: 0, active }, now).active, active);
});

test("Overpass cache yields storage to draft and background writes once on quota failure", async () => {
  let writes = 0; let clears = 0;
  const quota = Object.assign(new Error("full"), { name: "QuotaExceededError" });
  const store = editor.overpassQuotaStore({ put: async () => { if (++writes === 1) throw quota; return true; }, get() {} }, { clear: async () => { clears++; } });
  assert.equal(await store.put("key", {}), true); assert.equal(writes, 2); assert.equal(clears, 1);
  assert.equal(editor.overpassQuotaStore(null, {}), null);
});

test("Overpass prepared geometry fixtures preserve spans and missing members", () => {
  const fixtures = require("./testdata/overpass/cases.json");
  for (const fixture of fixtures.cases.slice(0, 3)) {
    const result = editor.overpassGeometry(fixture.body, frame);
    if (fixture.name === "outside-adjacent-coordinate") assert.equal(result.lines.length, 1);
    if (fixture.name === "disconnected-way-spans") { assert.equal(result.lines.length, 2); assert.equal(result.stats.missingCoordinates, 1); }
    if (fixture.name === "incomplete-multipolygon") { assert.equal(result.polygons.length, 0); assert.equal(result.stats.omittedFills, 1); assert.ok(result.stats.missingMembers >= 1); }
  }
});

test("Overpass drawing caps output dimensions, image bytes, cancellation and releases the canvas", async () => {
  const geometry = parse([ring(1, 1, square, water)]); const geo = editor.frameCenterGeo(frame);
  const calls = []; const context = new Proxy({}, { get(_, name) { return (...args) => calls.push([name, ...args]); }, set() { return true; } });
  let canvas; let size;
  const decoder = { canvas(width, height) { size = { width, height }; canvas = { width, height, getContext: () => context }; return canvas; }, encode: async () => ({ size: editor.IMAGE_FILE_BYTES + 1 }) };
  await assert.rejects(editor.overpassDraw(geometry, frame, geo, decoder, new AbortController().signal), /8 MiB/);
  assert.ok(size.width <= 4096 && size.height <= 4096); assert.equal(canvas.width, 0); assert.equal(canvas.height, 0);
  assert.ok(calls.some(([name, rule]) => name === "fill" && rule === "evenodd")); assert.ok(calls.some(([name, text]) => name === "fillText" && text.includes("openstreetmap.org/copyright")));
  const controller = new AbortController(); decoder.encode = async () => { controller.abort(); return { size: 1 }; };
  await assert.rejects(editor.overpassDraw(geometry, frame, geo, decoder, controller.signal), { name: "AbortError" }); assert.equal(canvas.width, 0);
  decoder.canvas = () => ({ width: 1, height: 1, getContext: () => null });
  await assert.rejects(editor.overpassDraw(geometry, frame, geo, decoder, new AbortController().signal), /cannot draw/);
});

test("Overpass charges unknown transfers conservatively and refunds only before network start", async () => {
  const f = clientFixture({}, { admission: { allowance: 100 }, dependencies: { fetch: async () => { throw new TypeError("CORS failure"); } } });
  await assert.rejects(f.load()); assert.equal(f.finishes[0][1], 100);
  const controller = new AbortController(); const g = clientFixture();
  g.store.admit = async () => { controller.abort(); return { allowance: 100 }; };
  await assert.rejects(g.load(controller.signal), { name: "AbortError" }); assert.equal(g.calls.length, 0); assert.equal(g.finishes[0][1], 0);
  const h = clientFixture({}, { dependencies: { token: () => { throw new Error("random failed"); } } });
  await assert.rejects(h.load(), /random failed/); assert.equal(h.client.busy, false);
});

test("Overpass bounds repeated relation expansion before ring copies", () => {
  const shared = ring(1, 1, square);
  const relations = Array.from({ length: 5 }, (_, index) => ({ type: "relation", id: 10 + index, tags: water, members: [{ type: "way", ref: 1, role: "outer" }] }));
  assert.throws(() => parse([shared, ...relations], { ...editor.OVERPASS_LIMITS, vertices: 20 }), /expanded vertex/);
  const one = parse([shared, relations[0]]);
  assert.equal(one.stats.expandedVertices, square.length);
  assert.equal(one.stats.missingMembers, 0);
  assert.ok(one.lines.every((line) => line.layer === "water"));
});

test("Overpass does not draw an antimeridian crossing across unrelated bounds", () => {
  const a = { lat: 51.5, lon: 179 }; const b = { lat: 51.5, lon: -179 };
  assert.equal(editor.overpassClipLine(a, b, frame), null);
  const result = parse([ring(1, 1, [a, b, { ...b, lat: 51.51 }, { ...a, lat: 51.51 }, a], water)]);
  assert.equal(result.lines.length, 0); assert.equal(result.polygons.length, 0);
  assert.equal(result.stats.omittedSegments, 2); assert.equal(result.stats.omittedFills, 1);
});
