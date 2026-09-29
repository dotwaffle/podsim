(function (root) {
  "use strict";
  const RADIUS = 6371000;
  const DEGREE = Math.PI / 180;
  const TILE_SIZE = 256;
  const MAX_TILES = 96;
  const CACHE_TILES = 128;
  const CONCURRENCY = 4;
  const URL_TEMPLATE = "https://tile.openstreetmap.org/{z}/{x}/{y}.png";
  const clamp = (x, low, high) => Math.max(low, Math.min(high, x));
  const latitude = (y, count) => Math.atan(Math.sinh(Math.PI * (1 - 2 * y / count))) / DEGREE;
  const tileY = (lat, count) => (1 - Math.asinh(Math.tan(clamp(lat, -80, 80) * DEGREE)) / Math.PI) / 2 * count;

  function validGeo(geo) {
    return geo && geo.projection === "equirectangular" && geo.radius === RADIUS &&
      Number.isFinite(geo.latitude) && Math.abs(geo.latitude) <= 80 && Number.isFinite(geo.longitude) && Math.abs(geo.longitude) <= 180;
  }

  function validMap(map, geo) {
    return map && typeof map === "object" && !Array.isArray(map) && Object.keys(map).every((key) => ["provider", "opacity"].includes(key)) &&
      map.provider === "osm" && (map.opacity === undefined || (Number.isFinite(map.opacity) && map.opacity >= 0 && map.opacity <= 1)) && validGeo(geo);
  }

  // plan chooses only tiles that intersect the visible geographic area.
  // Tile X can cross the date line. The URL wraps X, but placement does not.
  function plan(view) {
    if (!view || !view.enabled || !validGeo(view.geo) || ![view.scale, view.x, view.y, view.width, view.height].every(Number.isFinite) || view.scale <= 0 || view.width <= 0 || view.height <= 0) return [];
    const clip = view.clip || { x: 0, y: 0, width: view.width, height: view.height };
    if (![clip.x, clip.y, clip.width, clip.height].every(Number.isFinite) || clip.width <= 0 || clip.height <= 0) return [];
    const left = Math.max(0, clip.x), right = Math.min(view.width, clip.x + clip.width);
    const top = Math.max(0, clip.y), bottom = Math.min(view.height, clip.y + clip.height);
    if (right <= left || bottom <= top) return [];
    const longitudeScale = RADIUS * Math.cos(view.geo.latitude * DEGREE) * DEGREE;
    const west = view.geo.longitude + (left - view.x) / view.scale / longitudeScale;
    const east = Math.min(west + 360, view.geo.longitude + (right - view.x) / view.scale / longitudeScale);
    const north = Math.min(80, view.geo.latitude - (top - view.y) / view.scale / RADIUS / DEGREE);
    const south = Math.max(-80, view.geo.latitude - (bottom - view.y) / view.scale / RADIUS / DEGREE);
    if (![west, east, north, south].every(Number.isFinite) || south >= north) return [];
    let zoom = clamp(Math.round(Math.log2(view.scale * longitudeScale * 360 / TILE_SIZE)), 0, 19);
    let bounds;
    for (;;) {
      const count = 2 ** zoom;
      bounds = { count, x0: Math.floor((west + 180) / 360 * count), x1: Math.ceil((east + 180) / 360 * count) - 1,
        y0: Math.floor(tileY(north, count)), y1: Math.ceil(tileY(south, count)) - 1 };
      if (![bounds.x0, bounds.x1, bounds.y0, bounds.y1].every(Number.isSafeInteger)) return [];
      if ((bounds.x1 - bounds.x0 + 1) * (bounds.y1 - bounds.y0 + 1) <= MAX_TILES || zoom === 0) break;
      zoom--;
    }
    const out = [];
    for (let y = bounds.y0; y <= bounds.y1; y++) for (let x = bounds.x0; x <= bounds.x1; x++) {
      const wrappedX = ((x % bounds.count) + bounds.count) % bounds.count;
      const key = `${zoom}/${wrappedX}/${y}`;
      out.push({ key, z: zoom, x, y, count: bounds.count, url: URL_TEMPLATE.replace("{z}", zoom).replace("{x}", wrappedX).replace("{y}", y) });
    }
    return out;
  }

  // strips reprojects each visible horizontal band into local meters.
  // No image pixels are read or exported. A band is at most four screen pixels.
  function strips(tile, view) {
    const geo = view.geo, clip = view.clip || { x: 0, y: 0, width: view.width, height: view.height };
    const scaleY = RADIUS * DEGREE * view.scale;
    const screenY = (row) => view.y - (latitude(tile.y + row / TILE_SIZE, tile.count) - geo.latitude) * scaleY;
    const y0 = Math.max(screenY(0), view.y - (80 - geo.latitude) * scaleY, 0, clip.y);
    const y1 = Math.min(screenY(TILE_SIZE), view.y - (-80 - geo.latitude) * scaleY, view.height, clip.y + clip.height);
    const scaleX = RADIUS * Math.cos(geo.latitude * DEGREE) * DEGREE * view.scale;
    const x = view.x + (tile.x / tile.count * 360 - 180 - geo.longitude) * scaleX;
    const width = 360 / tile.count * scaleX;
    const sourceY = (screen) => (tileY(geo.latitude - (screen - view.y) / scaleY, tile.count) - tile.y) * TILE_SIZE;
    const out = [];
    for (let y = y0; y < y1;) {
      const end = Math.min(Math.floor(y) + 4, y1), sy = clamp(sourceY(y), 0, TILE_SIZE), ey = clamp(sourceY(end), 0, TILE_SIZE);
      if (ey > sy) out.push({ sx: 0, sy, sw: TILE_SIZE, sh: ey - sy, x, y, width, height: end - y });
      y = end;
    }
    return out;
  }

  function createLayer({ container, onStatus = () => {} }) {
    const doc = container.ownerDocument;
    const canvas = doc.createElement("canvas");
    canvas.className = "tile-canvas"; canvas.setAttribute("aria-hidden", "true");
    Object.assign(canvas.style, { position: "absolute", inset: "0", pointerEvents: "none" });
    const credit = doc.createElement("a");
    credit.textContent = "© OpenStreetMap contributors"; credit.href = "https://www.openstreetmap.org/copyright";
    credit.target = "_blank"; credit.rel = "noopener"; credit.className = "tile-attribution";
    Object.assign(credit.style, { position: "absolute", zIndex: "2", font: "11px system-ui", padding: "2px 5px", color: "#16252c", background: "#ffffffeb", pointerEvents: "auto" });
    container.prepend(canvas); container.append(credit);
    let view = null, visible = [], timer = 0, raf = 0, destroyed = false, lastStatus = "";
    const cache = new Map();
    function report() {
      const entries = visible.map((tile) => cache.get(tile.key));
      const failed = entries.filter((entry) => entry?.state === "error").length;
      const loaded = entries.filter((entry) => entry?.state === "loaded").length;
      const status = !view?.enabled ? "" : failed ? `Map tiles unavailable (${failed}). The network remains usable. Retry to request them again.` : loaded < visible.length ? "Loading map tiles…" : "Map tiles loaded.";
      if (status !== lastStatus) { lastStatus = status; onStatus(status); }
    }
    function draw() {
      raf = 0;
      const ctx = canvas.getContext("2d");
      ctx.clearRect(0, 0, canvas.width, canvas.height);
      if (!view?.enabled) return;
      const clip = view.clip || { x: 0, y: 0, width: view.width, height: view.height };
      ctx.save(); ctx.beginPath(); ctx.rect(clip.x, clip.y, clip.width, clip.height); ctx.clip();
      ctx.globalAlpha = clamp(view.opacity ?? .45, 0, 1);
      for (const tile of visible) {
        const entry = cache.get(tile.key);
        if (entry?.state !== "loaded") continue;
        for (const band of strips(tile, view)) ctx.drawImage(entry.image, band.sx, band.sy, band.sw, band.sh, band.x, band.y, band.width, band.height);
      }
      ctx.restore(); report();
    }
    function scheduleDraw() { if (!raf && !destroyed) raf = root.requestAnimationFrame(draw); }
    function prune() {
      const keep = new Set(visible.map((tile) => tile.key));
      for (const [key, entry] of cache) {
        if (cache.size <= CACHE_TILES) break;
        if (!keep.has(key) && entry.state !== "loading") cache.delete(key);
      }
    }
    function pump() {
      timer = 0;
      if (destroyed || !view?.enabled) return;
      let active = [...cache.values()].filter((entry) => entry.state === "loading").length;
      for (const tile of visible) {
        if (active >= CONCURRENCY) break;
        if (cache.has(tile.key)) continue;
        const image = new root.Image();
        const entry = { image, state: "loading" }; cache.set(tile.key, entry); active++;
        const finish = (state) => {
          if (destroyed || cache.get(tile.key) !== entry) return;
          entry.state = state; root.clearTimeout(entry.timer); image.onload = image.onerror = null;
          scheduleDraw(); prune(); if (!timer) pump();
        };
        image.onload = () => finish(image.naturalWidth === TILE_SIZE && image.naturalHeight === TILE_SIZE ? "loaded" : "error");
        image.onerror = () => finish("error");
        image.referrerPolicy = "strict-origin-when-cross-origin";
        entry.timer = root.setTimeout(() => { finish("error"); image.removeAttribute("src"); }, 15000);
        image.src = tile.url;
      }
      prune(); report();
    }
    function stop() {
      root.clearTimeout(timer); timer = 0;
      for (const [key, entry] of cache) if (entry.state === "loading") {
        root.clearTimeout(entry.timer); entry.image.onload = entry.image.onerror = null; entry.image.removeAttribute("src"); cache.delete(key);
      }
    }
    const layer = {
      setView(next) {
        if (destroyed) return;
        const sizeOK = next && [next.width, next.height].every((size) => Number.isFinite(size) && size > 0 && size <= 8192) && next.width * next.height <= 32 * 1024 * 1024;
        view = { ...next, enabled: !!next?.enabled && sizeOK && validGeo(next.geo) && next.opacity > 0 };
        visible = plan(view);
        canvas.hidden = credit.hidden = !view.enabled;
        if (!view.enabled) { stop(); scheduleDraw(); report(); return; }
        const width = Math.ceil(view.width), height = Math.ceil(view.height);
        if (canvas.width !== width) canvas.width = width;
        if (canvas.height !== height) canvas.height = height;
        canvas.style.width = `${view.width}px`; canvas.style.height = `${view.height}px`;
        const clip = view.clip || { x: 0, y: 0, width: view.width, height: view.height };
        credit.style.right = `${Math.max(0, view.width - clip.x - clip.width)}px`;
        credit.style.bottom = `${Math.max(0, view.height - clip.y - clip.height)}px`;
        for (const tile of visible) if (cache.has(tile.key)) { const entry = cache.get(tile.key); cache.delete(tile.key); cache.set(tile.key, entry); }
        scheduleDraw();
        if (!timer) timer = root.setTimeout(pump, 120);
      },
      retry() { for (const [key, entry] of cache) if (entry.state === "error") cache.delete(key); pump(); },
      destroy() { destroyed = true; stop(); root.cancelAnimationFrame(raf); cache.clear(); canvas.remove(); credit.remove(); },
    };
    return layer;
  }
  const api = { RADIUS, TILE_SIZE, MAX_TILES, CACHE_TILES, CONCURRENCY, URL_TEMPLATE, validGeo, validMap, latitude, tileY, plan, strips, createLayer };
  if (typeof module !== "undefined" && module.exports) module.exports = api;
  root.PodsimTiles = api;
})(typeof window !== "undefined" ? window : globalThis);
