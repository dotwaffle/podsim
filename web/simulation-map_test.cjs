"use strict";
const assert = require("node:assert/strict");
const test = require("node:test");
const fs = require("node:fs");
const map = require("./simulation-map.js");

const geo = { latitude: 51.5, longitude: -.1, projection: "equirectangular", radius: 6371000 };
function view() { return { source: { epoch: "a", serverStart: "one", projectRevision: "1", generation: "1" }, geo: { ...geo }, map: { provider: "osm", opacity: .45 }, hidden: false, scale: 2, x: 100, y: 200, width: 2200, height: 1456, clip: { x: 48, y: 208, width: 1496, height: 768 } }; }
function events(value = {}) {
  const handlers = new Map();
  return Object.assign(value, { addEventListener(name, fn) { handlers.set(name, fn); }, removeEventListener(name) { handlers.delete(name); }, fire(name, event = {}) { handlers.get(name)?.(event); } });
}
function fixture(saved) {
  const nodes = Object.fromEntries(["simulationMap", "simulationMapControls", "showMap", "simulationMapStatus", "retryMap"].map((id) => [id, events({ style: {}, hidden: true, textContent: "" })]));
  let credit;
  nodes.simulationMap.querySelector = () => credit;
  const canvas = { width: 2200, height: 1456, getContext: () => ({ drawingBufferWidth: 2200, drawingBufferHeight: 1456 }), getBoundingClientRect: () => ({ left: 0, top: 0, width: 1100, height: 728 }) };
  const document = events({ hidden: false, getElementById: (id) => nodes[id], querySelector: (selector) => { assert.equal(selector, "canvas:not(.tile-canvas)"); return canvas; }, body: { append(node) { assert.equal(node, credit); } } });
  let observer;
  const window = events({ innerWidth: 1100, innerHeight: 728, frameElement: { inert: false }, MutationObserver: class { constructor(fn) { observer = fn; } observe() {} disconnect() {} } });
  const calls = [], stored = [];
  const tiles = {
    validGeo: (value) => value?.projection === geo.projection && value.radius === geo.radius,
    validMap: (value, reference) => value?.provider === "osm" && Number.isFinite(value.opacity) && value.opacity >= 0 && value.opacity <= 1 && !!reference,
    createLayer({ container, onStatus }) {
      assert.equal(container, nodes.simulationMap); credit = { style: {} }; calls.push({ kind: "create" });
      return { setView(value) { calls.push({ kind: "view", ...value }); onStatus(value.enabled ? "Map tiles loaded." : ""); }, retry() { calls.push({ kind: "retry" }); }, destroy() { calls.push({ kind: "destroy" }); } };
    },
  };
  const controller = map.createController({ window, document, tiles, storage: { getItem() { return saved; }, setItem(...args) { stored.push(args); } } });
  return { controller, window, document, nodes, calls, stored, observer: () => observer(), credit: () => credit };
}

test("screen transform preserves DPR, cap stretching, and letterbox", () => {
  const initial = view();
  const normal = map.screenTransform(initial, { left: 3, top: 7, width: 1100, height: 728 }, { width: 2200, height: 1456 });
  assert.deepEqual(normal, { unit: .5, stretchX: 1, stretchY: 1, left: 3, top: 7, width: 1100, height: 728 });
  const capped = map.screenTransform({ width: 7524, height: 4409 }, { left: 0, top: 0, width: 4200, height: 2400 }, { width: 7524, height: 4409 });
  assert(Math.abs(capped.width * capped.stretchX - 4200) < 1e-9);
  assert(Math.abs(capped.height * capped.stretchY - 2400) < 1e-9);
  const letterbox = map.screenTransform({ width: 1100, height: 728 }, { left: 0, top: 0, width: 1000, height: 700 }, { width: 2000, height: 1400 });
  assert(letterbox.top > 0); assert(Math.abs(2 * letterbox.top + letterbox.height - 700) < 1e-9);
  assert.equal(map.screenTransform(initial, { width: 0, height: 728 }, { width: 2200, height: 1456 }), null);
});

test("map is opt-in per scenario with a remembered browser visibility choice", () => {
  const f = fixture(null);
  f.controller.receive({ ...view(), map: null }); assert.equal(f.calls.length, 0); assert.equal(f.window.podsimMapEnabled, false);
  f.controller.receive(view()); assert.equal(f.window.podsimMapEnabled, true); assert.equal(f.nodes.showMap.checked, true);
  const sent = f.calls.at(-1); assert.equal(sent.scale, 1); assert.equal(sent.x, 50); assert.equal(sent.y, 100); assert.deepEqual(sent.clip, { x: 24, y: 104, width: 748, height: 384 });
  assert.equal(f.credit().style.zIndex, "4"); assert.equal(f.credit().style.right, "328px"); assert.equal(f.credit().style.bottom, "240px");
  f.nodes.showMap.checked = false; f.nodes.showMap.fire("change"); assert.equal(f.window.podsimMapEnabled, false); assert.equal(f.calls.at(-1).enabled, false);
  assert.deepEqual(f.stored, [[map.STORAGE_KEY, "false"]]);
  const hidden = fixture("false"); hidden.controller.receive(view()); assert.equal(hidden.calls.length, 0);
  hidden.nodes.showMap.checked = true; hidden.nodes.showMap.fire("change"); assert.equal(hidden.window.podsimMapEnabled, true);
  hidden.window.fire("storage", { key: map.STORAGE_KEY, newValue: "false" }); assert.equal(hidden.window.podsimMapEnabled, false);
});

test("page and editor visibility stop tiles and wait for a current camera on return", () => {
  for (const condition of ["document", "editor", "pagehide"]) {
    const f = fixture(null); f.controller.receive(view());
    if (condition === "document") { f.document.hidden = true; f.document.fire("visibilitychange"); }
    if (condition === "editor") { f.window.frameElement.inert = true; f.observer(); }
    if (condition === "pagehide") f.window.fire("pagehide");
    assert.equal(f.window.podsimMapEnabled, false, condition); assert.equal(f.calls.at(-1).enabled, false);
    if (condition === "document") { f.document.hidden = false; f.document.fire("visibilitychange"); }
    if (condition === "editor") { f.window.frameElement.inert = false; f.observer(); }
    if (condition === "pagehide") f.window.fire("pageshow");
    assert.equal(f.window.podsimMapEnabled, false, "resume waits for Go");
    assert(f.window.podsimMapRefresh > 0);
    f.controller.receive(view()); assert.equal(f.window.podsimMapEnabled, true);
  }
});

test("zero opacity and invalid or removed metadata never enable tiles", () => {
  for (const change of [(v) => v.map.opacity = 0, (v) => v.map.provider = "custom", (v) => v.geo = null, (v) => v.map.opacity = NaN, (v) => v.hidden = true]) {
    const f = fixture(null), next = view(); change(next); f.controller.receive(next);
    assert.equal(f.window.podsimMapEnabled, false); assert.equal(f.calls.length, 0);
  }
  const f = fixture(null); f.controller.receive(view());
  f.controller.receive({ ...view(), map: null }); assert.equal(f.window.podsimMapEnabled, false); assert.equal(f.calls.at(-1).kind, "destroy");
});

test("source swaps remove stale tiles even when Go reuses its bridge objects", () => {
  const f = fixture(null), next = view(); f.controller.receive(next);
  next.source.serverStart = "two"; next.geo.latitude = 40; f.controller.receive(next);
  assert.deepEqual(f.calls.slice(-3).map((x) => x.kind), ["destroy", "create", "view"]);
  assert.equal(f.calls.at(-1).geo.latitude, 40);
  f.nodes.retryMap.fire("click"); assert.equal(f.calls.at(-1).kind, "retry");
  f.controller.destroy(); assert.equal(f.window.podsimMapEnabled, false); assert.equal(f.window.podsimMapView, undefined);
});

test("game loads tiles before its bridge and focuses only the game canvas", () => {
  const html = fs.readFileSync(`${__dirname}/game.html`, "utf8");
  assert(html.indexOf('id="simulationMap"') < html.indexOf('src="simulation-map.js"'));
  assert(html.indexOf('src="tiles.js"') < html.indexOf('src="simulation-map.js"'));
  assert(html.includes('querySelector("canvas:not(.tile-canvas)")'));
});
