(function (root) {
  "use strict";

  // create connects explicit browser searches to the native, shared provider proxy.
  function create({ form, input, button, status, results, fetch, navigate, hasReference }) {
    let searching = false, selecting = false, controller = null, stopped = false, suspended = false, generation = 0;
    const document = form.ownerDocument;
    function refresh() {
      const available = hasReference();
      input.disabled = button.disabled = stopped || suspended || searching || selecting || !available;
      for (const item of results.querySelectorAll("button")) item.disabled = stopped || suspended || searching || selecting || !available;
      if (!available) status.textContent = "Set a geographic reference before searching for a place.";
    }
    async function select(place) {
      if (selecting || stopped || suspended || !hasReference()) return;
      const ticket = generation;
      selecting = true; refresh();
      try { await navigate(place); if (!stopped && ticket === generation) status.textContent = "Map view updated."; }
      catch (error) { if (!stopped && ticket === generation) status.textContent = error.message; }
      finally { selecting = false; refresh(); }
    }
    async function search(event) {
      event.preventDefault();
      if (searching || selecting || stopped || suspended || !hasReference() || !input.reportValidity()) return;
      const ticket = generation;
      const query = input.value.trim();
      if (!query) { status.textContent = "Enter a place name."; return; }
      searching = true; controller = new AbortController(); results.replaceChildren(); refresh();
      status.textContent = "Searching…";
      try {
        const response = await fetch(`/api/places?q=${encodeURIComponent(query)}`, { signal: controller.signal, cache: "no-store", credentials: "same-origin" });
        if (response.status === 429) {
          const retry = Number(response.headers.get("Retry-After"));
          throw new Error(Number.isInteger(retry) && retry > 0 && retry <= 60 ? `Search is busy. Wait ${retry} seconds, then press Search again.` : "Search is busy. Try again later.");
        }
        if (response.status === 404 || response.status === 405) throw new Error("Place search needs the native Podsim server.");
        if (response.status === 503) throw new Error("Place search is disabled on this server.");
        if (response.status === 504) throw new Error("Place search timed out. Press Search to try again.");
        if (response.status === 400) throw new Error("Use a place name with 1 to 200 UTF-8 bytes.");
        if (!response.ok) throw new Error("Place search is unavailable. Press Search to try again.");
        const data = await response.json();
        if (!Array.isArray(data?.results) || data.results.length > 5 || !data.results.every((place) => place && typeof place.name === "string" && place.name.length && Number.isFinite(place.latitude) && Number.isFinite(place.longitude))) throw new Error("Place search returned invalid results.");
        if (stopped || ticket !== generation) return;
        for (const place of data.results) {
          const item = document.createElement("li"), control = document.createElement("button");
          control.type = "button"; control.textContent = place.name;
          control.addEventListener("click", () => select(place)); item.append(control); results.append(item);
        }
        status.textContent = data.results.length ? "Select a result to move the map view." : "No places found.";
      } catch (error) {
        if (!stopped && ticket === generation) status.textContent = error.name === "AbortError" ? "Search canceled." : error instanceof TypeError ? "Place search needs a reachable native Podsim server." : error.message;
      } finally { searching = false; controller = null; refresh(); }
    }
    form.addEventListener("submit", search); refresh();
    return {
      refresh,
      suspend() { suspended = true; generation++; controller?.abort(); if (searching || selecting) status.textContent = "Search canceled."; refresh(); },
      resume() { suspended = false; refresh(); },
      close() { stopped = true; generation++; controller?.abort(); form.removeEventListener("submit", search); refresh(); },
    };
  }
  const api = { create };
  if (typeof module !== "undefined" && module.exports) module.exports = api;
  root.PodsimPlaceSearch = api;
})(globalThis);
