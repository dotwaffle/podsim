"use strict";
const test = require("node:test"), assert = require("node:assert/strict");
const { create } = require("./place-search.js");

function fixture(response = { results: [] }) {
  class Element {
    constructor() { this.children = []; this.listeners = new Map(); this.textContent = ""; this.ownerDocument = document; }
    addEventListener(name, callback) { this.listeners.set(name, callback); }
    removeEventListener(name) { this.listeners.delete(name); }
    append(item) { this.children.push(item); }
    replaceChildren() { this.children = []; }
    querySelectorAll() { return this.children.flatMap((item) => item.children); }
    reportValidity() { return true; }
  }
  const document = { createElement: () => new Element() };
  const form = new Element(), input = new Element(), button = new Element(), status = new Element(), results = new Element();
  const calls = [], selected = [];
  let reference = true, fetcher = async () => ({ ok: true, status: 200, json: async () => response });
  input.value = "London";
  const controller = create({ form, input, button, status, results, hasReference: () => reference,
    fetch: async (...args) => { calls.push(args); return fetcher(...args); }, navigate: async (place) => selected.push(place) });
  return { form, input, button, status, results, calls, selected, controller,
    set reference(value) { reference = value; controller.refresh(); },
    set fetcher(value) { fetcher = value; },
    submit: () => form.listeners.get("submit")({ preventDefault() {} }),
  };
}

test("place search sends explicit searches, displays names as text, and delegates navigation", async () => {
  const place = { name: "<script>London</script>", latitude: 51.5, longitude: -.12 };
  const f = fixture({ results: [place] });
  f.input.value = "  London & rail  ";
  assert.equal(f.input.listeners.size, 0);
  assert.equal(f.calls.length, 0);
  await f.submit();
  assert.equal(f.calls[0][0], "/api/places?q=London%20%26%20rail");
  assert.equal(f.calls[0][1].cache, "no-store");
  const control = f.results.children[0].children[0];
  assert.equal(control.textContent, place.name);
  assert.deepEqual(f.selected, []);
  await control.listeners.get("click")();
  assert.deepEqual(f.selected, [place]);
  assert.equal(f.status.textContent, "Map view updated.");
});

test("place search does not retry failures or search without a reference", async (t) => {
  for (const [code, message] of [[429, /Wait 2 seconds/], [404, /native Podsim/], [503, /disabled/], [504, /timed out/], [502, /unavailable/], [400, /UTF-8/]]) await t.test(String(code), async () => {
    const f = fixture();
    f.reference = false;
    assert.equal(f.button.disabled, true);
    await f.submit(); assert.equal(f.calls.length, 0);
    f.reference = true;
    f.fetcher = async () => ({ ok: false, status: code, headers: { get: () => "2" } });
    await f.submit();
    assert.equal(f.calls.length, 1);
    assert.match(f.status.textContent, message);
    assert.equal(f.button.disabled, false);
    assert.equal(f.results.children.length, 0);
  });
});

test("busy place search blocks another request and close cancels without publishing results", async () => {
  const f = fixture();
  let finish;
  f.fetcher = () => new Promise((resolve) => { finish = resolve; });
  const pending = f.submit();
  assert.equal(f.button.disabled, true);
  await f.submit(); assert.equal(f.calls.length, 1);
  f.controller.close();
  assert.equal(f.calls[0][1].signal.aborted, true);
  finish({ ok: true, status: 200, json: async () => ({ results: [{ name: "Late", latitude: 0, longitude: 0 }] }) });
  await pending;
  assert.equal(f.results.children.length, 0);
  assert.equal(f.form.listeners.size, 0);
});

test("place search resumes after page hiding and discards the previous response", async () => {
  const f = fixture();
  let finish;
  f.fetcher = () => new Promise((resolve) => { finish = resolve; });
  const pending = f.submit();
  f.controller.suspend(); f.controller.resume();
  finish({ ok: true, status: 200, json: async () => ({ results: [{ name: "Stale", latitude: 0, longitude: 0 }] }) });
  await pending;
  assert.equal(f.results.children.length, 0);
  assert.equal(f.button.disabled, false);
  f.fetcher = async () => ({ ok: true, status: 200, json: async () => ({ results: [] }) });
  await f.submit();
  assert.equal(f.calls.length, 2);
  assert.equal(f.status.textContent, "No places found.");
});
