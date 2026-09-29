"use strict";

(function (root) {
  const STORAGE_KEY = "podsim.showMap";
  const finite = (value) => typeof value === "number" && Number.isFinite(value);

  // screenTransform includes the letterbox inside a capped WebGL buffer.
  // The wrapper carries any nonuniform stretch applied by the browser.
  function screenTransform(view, rect, buffer) {
    if (![view.width, view.height, rect.width, rect.height, buffer.width, buffer.height].every((value) => finite(value) && value > 0)) return null;
    const drawScale = Math.min(buffer.width / view.width, buffer.height / view.height);
    const sx = drawScale * rect.width / buffer.width;
    const sy = drawScale * rect.height / buffer.height;
    const unit = Math.min(sx, sy);
    return {
      unit, stretchX: sx / unit, stretchY: sy / unit,
      left: rect.left + (buffer.width - view.width * drawScale) / 2 * rect.width / buffer.width,
      top: rect.top + (buffer.height - view.height * drawScale) / 2 * rect.height / buffer.height,
      width: view.width * unit, height: view.height * unit,
    };
  }

  function sourceKey(view) {
    const source = view.source || {};
    return JSON.stringify([source.serverStart, source.epoch, source.projectRevision, source.generation, view.geo, view.map]);
  }

  function createController({ window, document, tiles, storage }) {
    const container = document.getElementById("simulationMap");
    const controls = document.getElementById("simulationMapControls");
    const toggle = document.getElementById("showMap");
    const status = document.getElementById("simulationMapStatus");
    const retry = document.getElementById("retryMap");
    let view = null, layer = null, credit = null, identity = "", suspended = false, awaitingView = true;
    let preference = null;
    try { const saved = storage?.getItem(STORAGE_KEY); if (saved === "true" || saved === "false") preference = saved === "true"; } catch { /* Storage can be disabled by the browser. */ }
    window.podsimMapEnabled = false;
    window.podsimMapRefresh = 0;

    function active() { return !suspended && !document.hidden && !window.frameElement?.inert && !view?.hidden; }
    function available() { return !!view && tiles.validGeo(view.geo) && tiles.validMap(view.map, view.geo); }
    function clearLayer() { layer?.destroy(); layer = credit = null; }
    function report(text) {
      status.textContent = text;
      status.hidden = !text || text === "Map tiles loaded.";
      retry.hidden = !text.includes("unavailable");
    }
    function refresh() {
      const hasMap = available();
      controls.hidden = !hasMap || !active();
      toggle.checked = preference ?? hasMap;
      window.podsimMapEnabled = false;
      const canvas = document.querySelector("canvas:not(.tile-canvas)");
      const context = canvas?.getContext("webgl2");
      const transform = canvas && view && screenTransform(view, canvas.getBoundingClientRect(), {
        width: context?.drawingBufferWidth || canvas.width,
        height: context?.drawingBufferHeight || canvas.height,
      });
      const enabled = hasMap && active() && !awaitingView && toggle.checked && view.map.opacity > 0 && !!transform;
      if (!hasMap || !transform) { clearLayer(); report(""); return; }
      if (!layer && enabled) {
        layer = tiles.createLayer({ container, onStatus: report });
        credit = container.querySelector(".tile-attribution");
        // Keep the shared attribution above the game canvas and outside
        // the raster transform so its text and link remain usable.
        if (credit) { document.body.append(credit); credit.style.zIndex = "4"; }
      }
      const unit = transform.unit;
      const clip = { x: view.clip.x * unit, y: view.clip.y * unit, width: view.clip.width * unit, height: view.clip.height * unit };
      Object.assign(container.style, { left: `${transform.left}px`, top: `${transform.top}px`, width: `${transform.width}px`, height: `${transform.height}px`, transform: `scale(${transform.stretchX}, ${transform.stretchY})` });
      const left = transform.left + clip.x * transform.stretchX;
      const top = transform.top + clip.y * transform.stretchY;
      controls.style.left = `${left + 8}px`; controls.style.top = `${top + 8}px`;
      layer?.setView({ geo: view.geo, opacity: view.map.opacity, enabled, scale: view.scale * unit, x: view.x * unit, y: view.y * unit, width: transform.width, height: transform.height, clip });
      if (credit) {
        credit.style.right = `${Math.max(0, window.innerWidth - left - clip.width * transform.stretchX)}px`;
        credit.style.bottom = `${Math.max(0, window.innerHeight - top - clip.height * transform.stretchY)}px`;
      }
      window.podsimMapEnabled = enabled;
    }
    function receive(next) {
      const nextIdentity = sourceKey(next);
      if (nextIdentity !== identity) { clearLayer(); identity = nextIdentity; }
      // Go reuses its bridge objects. Keep a snapshot until the next call.
      view = { ...next, geo: next.geo && { ...next.geo }, map: next.map && { ...next.map }, clip: { ...next.clip } };
      awaitingView = false;
      refresh();
    }
    function invalidate() { awaitingView = true; window.podsimMapRefresh++; refresh(); }
    function change() {
      preference = toggle.checked;
      try { storage?.setItem(STORAGE_KEY, String(preference)); } catch { /* The preference still applies to this page. */ }
      refresh();
    }
    function stored(event) {
      if (event.key !== STORAGE_KEY) return;
      preference = event.newValue === "true" ? true : event.newValue === "false" ? false : null;
      refresh();
    }
    function hide() { suspended = true; invalidate(); }
    function show() { suspended = false; invalidate(); }
    function retryTiles() { if (window.podsimMapEnabled) layer?.retry(); }
    toggle.addEventListener("change", change); retry.addEventListener("click", retryTiles);
    document.addEventListener("visibilitychange", invalidate);
    window.addEventListener("resize", invalidate); window.addEventListener("pagehide", hide); window.addEventListener("pageshow", show); window.addEventListener("storage", stored);
    const observer = window.frameElement ? new window.MutationObserver(invalidate) : null;
    observer?.observe(window.frameElement, { attributes: true, attributeFilter: ["inert", "hidden"] });
    window.podsimMapView = receive;
    return {
      receive,
      destroy() {
        clearLayer(); observer?.disconnect(); window.podsimMapEnabled = false; delete window.podsimMapView;
        toggle.removeEventListener("change", change); retry.removeEventListener("click", retryTiles);
        document.removeEventListener("visibilitychange", invalidate);
        window.removeEventListener("resize", invalidate); window.removeEventListener("pagehide", hide); window.removeEventListener("pageshow", show); window.removeEventListener("storage", stored);
      },
    };
  }

  const api = { STORAGE_KEY, screenTransform, sourceKey, createController };
  if (typeof module !== "undefined" && module.exports) module.exports = api;
  if (root.document && root.PodsimTiles) {
    let storage;
    try { storage = root.localStorage; } catch { /* Storage is optional. */ }
    root.PodsimSimulationMap = createController({ window: root, document: root.document, tiles: root.PodsimTiles, storage });
  }
})(typeof window !== "undefined" ? window : globalThis);
