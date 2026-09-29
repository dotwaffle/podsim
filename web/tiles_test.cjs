"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const vm = require("node:vm");
const tiles = require("./tiles.js");
const editor = require("./editor.js");
const geo = (latitude = 51.5074, longitude = -.1278) => ({ latitude, longitude, radius: 6371000, projection: "equirectangular" });
const view = (changes = {}) => ({ enabled: true, geo: geo(), opacity: .45, scale: .1, x: 400, y: 300, width: 800, height: 600, ...changes });

test("zoom chooses progressively detailed tiles with bounded viewport requests", () => {
  let previous = -1;
  for (const scale of [.00001, .0001, .001, .01, .1, 1, 5, 100]) {
    const list = tiles.plan(view({ scale }));
    assert.ok(list.length > 0 && list.length <= tiles.MAX_TILES);
    assert.ok(list[0].z >= previous && list[0].z <= 19);
    previous = list[0].z;
    for (const tile of list) {
      assert.match(tile.url, /^https:\/\/tile\.openstreetmap\.org\/\d+\/\d+\/\d+\.png$/);
      assert.ok(tile.y >= 0 && tile.y < 2 ** tile.z);
    }
  }
  assert.ok(tiles.plan(view({ scale: 1 }))[0].z > tiles.plan(view({ scale: .01 }))[0].z);
});

test("only visible tiles, clipped viewport and no request for disabled or invalid views", () => {
  for (const changes of [{ enabled: false }, { geo: null }, { scale: 0 }, { scale: NaN }, { width: 0 }, { x: Infinity }, { x: Number.MAX_VALUE, y: 300, scale: Number.MIN_VALUE }, { x: 1e25 }, { geo: geo(81) }, { clip: { x: 0, y: 0, width: -1, height: 1 } }]) assert.deepEqual(tiles.plan(view(changes)), []);
  const full = tiles.plan(view());
  const clipped = tiles.plan(view({ clip: { x: 200, y: 100, width: 100, height: 100 } }));
  assert.ok(clipped.length < full.length);
  for (const tile of clipped) assert.ok(full.some((item) => item.key === tile.key));
  for (const tile of full) {
    const bands = tiles.strips(tile, view());
    assert.ok(bands.length > 0);
    assert.ok(bands[0].x < 800 && bands[0].x + bands[0].width > 0);
  }
});

test("date line wraps requests without moving geographic placement", () => {
  for (const longitude of [-180, 180]) {
    const v = view({ geo: geo(0, longitude), scale: .001 });
    const list = tiles.plan(v);
    assert.ok(list.some((tile) => longitude < 0 ? tile.x < 0 : tile.x >= tile.count));
    for (const tile of list) {
      const x = Number(tile.url.split("/").at(-2));
      assert.ok(x >= 0 && x < tile.count);
      assert.ok(tiles.strips(tile, v)[0].x < v.width);
    }
  }
});

test("Mercator row reprojection matches independent geographic positions", () => {
  for (const latitude of [-80, -33.86, 0, 51.5074, 80]) for (const scale of [.0001, .01, .15, 5]) {
    const v = view({ geo: geo(latitude), scale });
    for (const tile of tiles.plan(v)) for (const band of tiles.strips(tile, v)) {
      assert.ok(band.height > 0 && band.height <= 4 + 1e-9);
      assert.ok(band.sy >= 0 && band.sy + band.sh <= 256 + 1e-8);
      for (const fraction of [0, .25, .5, .75, 1]) {
        const pixel = band.sy + band.sh * fraction;
        const n = Math.PI - 2 * Math.PI * (tile.y + pixel / 256) / (2 ** tile.z);
        const lat = 180 / Math.PI * (2 * Math.atan(Math.exp(n)) - Math.PI / 2);
        const expected = v.y - (lat - latitude) * Math.PI / 180 * 6371000 * scale;
        assert.ok(Math.abs(expected - (band.y + fraction * band.height)) < .25, `alignment latitude ${latitude} scale ${scale}: ${expected - band.y - fraction * band.height}`);
      }
    }
  }
});

test("map metadata round trips, does not embed tiles, and undo preserves images", () => {
  let original = editor.fallbackConfig();
  const [a, b] = original.network.Stations;
  original = editor.addLane(original, b.Exit, a.Entry, false);
  original = editor.setFleetCount(original, a.ID, 1);
  const config = editor.withTileMap(original, { latitude: 51.5, longitude: -.1, opacity: .6, choice: { mode: "adopt", confirmed: true } });
  assert.equal(original.geo, undefined);
  assert.deepEqual(config.geo, geo(51.5, -.1));
  const restored = editor.parseDocument(editor.serializeDocument(config, null));
  assert.deepEqual(restored.scenario.map, { provider: "osm", opacity: .6 });
  assert.equal(restored.background, null);
  assert.ok(!editor.serializeDocument(config, null).includes("data:image"));
  const image = { imageKey: "abc", width: 1, height: 1, x: 0, y: 0, opacity: .5 };
  const history = editor.createHistory({ scenario: original, background: image });
  history.replace({ scenario: config, background: image }); history.undo();
  assert.equal(history.value.scenario.map, undefined);
  assert.deepEqual(history.background, image);
  history.redo(); assert.deepEqual(history.value.scenario.map, config.map);
});

test("map validation rejects missing reference, unknown providers and unsafe opacity", () => {
  const config = editor.emptyConfig(); config.geo = geo();
  for (const map of [{ provider: "other", opacity: .4 }, { provider: "osm", opacity: -1 }, { provider: "osm", opacity: 2 }, { provider: "osm", opacity: NaN }, { provider: "osm", opacity: "0.4" }, { provider: "osm", opacity: .4, url: "https://example.com" }]) {
    assert.ok(editor.validateConfig({ ...config, map }).some((s) => s.includes("map needs")));
  }
  assert.ok(!tiles.validMap({ provider: "osm", opacity: .4 }, null));
  assert.ok(tiles.validMap({ provider: "osm", opacity: 0 }, geo()));
  assert.ok(tiles.validMap({ provider: "osm" }, geo()));
  assert.equal(editor.normalizeConfig({ ...config, map: { provider: "osm" } }).map.opacity, 0);
});

test("existing network requires explicit reference and keeps its coordinates", () => {
  const config = editor.fallbackConfig();
  assert.throws(() => editor.withTileMap(config, { latitude: 1, longitude: 2 }), /Anchor two nodes/);
  assert.throws(() => editor.withTileMap(config, { latitude: 1, longitude: 2, choice: { mode: "adopt", confirmed: false } }), /confirm/);
  const adopted = editor.withTileMap(config, { latitude: 1, longitude: 2, choice: { mode: "adopt", confirmed: true } });
  assert.deepEqual(adopted.network, config.network);
  const again = editor.withTileMap(adopted, { latitude: 70, longitude: 80 });
  assert.deepEqual(again.geo, adopted.geo);
});

function fixture() {
  const timers = new Map(), frames = new Map(), requests = [], draws = [], statuses = [];
  let id = 0, now = 0;
  const context = { clearRect() {}, save() {}, restore() {}, beginPath() {}, rect() {}, clip() {}, drawImage(...args) { draws.push(args); } };
  const element = () => ({ style: {}, hidden: false, setAttribute() {}, remove() {}, getContext: () => context });
  class Image {
    naturalWidth = 256; naturalHeight = 256;
    set src(value) { this.url = value; requests.push(this); }
    removeAttribute() { this.canceled = true; }
  }
  const root = { Image, setTimeout(fn, delay) { timers.set(++id, { fn, at: now + delay }); return id; }, clearTimeout(id) { timers.delete(id); }, requestAnimationFrame(fn) { frames.set(++id, fn); return id; }, cancelAnimationFrame(id) { frames.delete(id); } };
  const sandbox = { window: root }; vm.runInNewContext(fs.readFileSync(require.resolve("./tiles.js"), "utf8"), sandbox);
  const container = { ownerDocument: { createElement: element }, prepend() {}, append() {} };
  const layer = root.PodsimTiles.createLayer({ container, onStatus: (s) => statuses.push(s) });
  const flush = (queue) => { const current = [...queue.values()]; queue.clear(); current.forEach((fn) => fn()); };
  return { layer, requests, draws, statuses, tick: (ms = 120) => { now += ms; for (const [id, timer] of [...timers]) if (timer.at <= now) { timers.delete(id); timer.fn(); } }, paint: () => flush(frames) };
}

test("loader bounds concurrency, uses image URLs, and stops while hidden", () => {
  const f = fixture(); f.layer.setView(view()); f.tick();
  assert.equal(f.requests.length, tiles.CONCURRENCY);
  assert.equal(f.requests[0].referrerPolicy, "strict-origin-when-cross-origin");
  f.requests[0].onload(); assert.equal(f.requests.length, tiles.CONCURRENCY + 1);
  f.paint(); assert.ok(f.draws.length > 0);
  f.layer.setView(view({ enabled: false })); f.tick();
  assert.ok(f.requests.slice(1).every((img) => img.canceled));
  const count = f.requests.length; f.tick(); f.paint(); assert.equal(f.requests.length, count);
  f.layer.destroy(); f.layer.setView(view()); f.tick(); assert.equal(f.requests.length, count);
});

test("failed tiles do not loop and retry is explicit", () => {
  const f = fixture(); f.layer.setView(view()); f.tick();
  for (let i = 0; i < f.requests.length; i++) f.requests[i].onerror();
  const count = f.requests.length;
  assert.equal(count, tiles.plan(view()).length);
  f.layer.setView(view()); f.tick(); assert.equal(f.requests.length, count);
  assert.ok(f.statuses.at(-1).includes("unavailable"));
  f.layer.retry(); assert.equal(f.requests.length, count + tiles.CONCURRENCY);
});

test("moving view discards queued requests and stale completions cannot restore a hidden map", () => {
  const f = fixture(); f.layer.setView(view()); f.tick();
  const stale = f.requests[0].onload;
  f.layer.setView(view({ x: -100000 })); f.tick();
  f.requests[0].onload();
  const newest = f.requests.at(-1).url;
  assert.ok(tiles.plan(view({ x: -100000 })).some((tile) => tile.url === newest));
  f.layer.setView(view({ enabled: false })); stale(); f.tick(); f.paint();
  assert.equal(f.statuses.at(-1), "");
});

test("stalled requests time out without an automatic retry", () => {
  const f = fixture(); f.layer.setView(view()); f.tick();
  const first = f.requests[0]; f.tick(15000);
  assert.equal(first.canceled, true);
  assert.equal(first.onload, null);
  assert.ok(f.statuses.at(-1).includes("unavailable"));
  assert.equal(f.requests.filter((image) => image.url === first.url).length, 1);
});
