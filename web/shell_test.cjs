"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const shell = require("./shell.js");

const ORIGIN = "http://127.0.0.1:8080";

test("viewForHash shows the editor only for #editor", () => {
  for (const [hash, want] of [
    ["", "game"],
    ["#", "game"],
    ["#editor", "editor"],
    ["#Editor", "game"],
    ["#editor2", "game"],
    ["#game", "game"],
  ]) {
    assert.equal(shell.viewForHash(hash), want, JSON.stringify(hash));
  }
});

test("each view has the hash that selects it", () => {
  const hashes = Object.fromEntries(Object.entries(shell.VIEWS).map(([name, view]) => [name, view.hash]));
  assert.deepEqual(hashes, { game: "", editor: "#editor" }, "the game has no hash");
  for (const [name, view] of Object.entries(shell.VIEWS)) {
    assert.equal(shell.viewForHash(view.hash), name);
  }
});

test("pageRequest accepts only a show, debug or reload message from the same origin", () => {
  const game = { action: "show", view: "game", keyboard: false };
  const debug = { action: "debug" };
  for (const [name, event, want] of [
    ["game", { origin: ORIGIN, data: { podsim: "show", view: "game" } }, game],
    ["editor", { origin: ORIGIN, data: { podsim: "show", view: "editor" } }, { action: "show", view: "editor", keyboard: false }],
    ["editor from the game header", { origin: ORIGIN, data: { podsim: "show", view: "editor", keyboard: false } }, { action: "show", view: "editor", keyboard: false }],
    ["game with the keyboard", { origin: ORIGIN, data: { podsim: "show", view: "game", keyboard: true } }, { action: "show", view: "game", keyboard: true }],
    ["game without the keyboard", { origin: ORIGIN, data: { podsim: "show", view: "game", keyboard: false } }, game],
    ["keyboard as text", { origin: ORIGIN, data: { podsim: "show", view: "game", keyboard: "true" } }, null],
    ["keyboard as a number", { origin: ORIGIN, data: { podsim: "show", view: "game", keyboard: 1 } }, null],
    ["keyboard as null", { origin: ORIGIN, data: { podsim: "show", view: "game", keyboard: null } }, null],
    ["keyboard in an object", { origin: ORIGIN, data: { podsim: "show", view: "game", keyboard: { value: true } } }, null],
    ["keyboard from other origin", { origin: "http://example.com", data: { podsim: "show", view: "game", keyboard: true } }, null],
    ["other origin", { origin: "http://example.com", data: { podsim: "show", view: "game" } }, null],
    ["other port", { origin: "http://127.0.0.1:8081", data: { podsim: "show", view: "game" } }, null],
    ["opaque origin", { origin: "null", data: { podsim: "show", view: "game" } }, null],
    ["unknown view", { origin: ORIGIN, data: { podsim: "show", view: "map" } }, null],
    ["inherited name", { origin: ORIGIN, data: { podsim: "show", view: "toString" } }, null],
    ["view in an array", { origin: ORIGIN, data: { podsim: "show", view: ["game"] } }, null],
    ["no view", { origin: ORIGIN, data: { podsim: "show" } }, null],
    ["debug", { origin: ORIGIN, data: { podsim: "debug" } }, debug],
    ["debug from other origin", { origin: "http://example.com", data: { podsim: "debug" } }, null],
    ["debug from an opaque origin", { origin: "null", data: { podsim: "debug" } }, null],
    ["debug as text", { origin: ORIGIN, data: "debug" }, null],
    ["debug in an array", { origin: ORIGIN, data: { podsim: ["debug"] } }, null],
    ["debug with other case", { origin: ORIGIN, data: { podsim: "Debug" } }, null],
    ["reload", { origin: ORIGIN, data: { podsim: "reload" } }, { action: "reload" }],
    ["reload from other origin", { origin: "http://example.com", data: { podsim: "reload" } }, null],
    ["reload as text", { origin: ORIGIN, data: "reload" }, null],
    ["notice to the game", { origin: ORIGIN, data: { podsim: "notice", text: "Debug state downloaded: tick 1.", error: false } }, null],
    ["other action", { origin: ORIGIN, data: { podsim: "hide", view: "game" } }, null],
    ["inherited action", { origin: ORIGIN, data: { podsim: "toString" } }, null],
    ["other message", { origin: ORIGIN, data: { view: "game" } }, null],
    ["text data", { origin: ORIGIN, data: "show game" }, null],
    ["null data", { origin: ORIGIN, data: null }, null],
    ["no event", undefined, null],
  ]) {
    assert.deepEqual(shell.pageRequest(event, ORIGIN), want, name);
  }
});

test("pageRequest accepts a ready message only from the game frame with a number version", () => {
  const game = { name: "game frame" };
  const editor = { name: "editor frame" };
  for (const [name, event, want] of [
    ["ready", { origin: ORIGIN, source: game, data: { podsim: "ready", version: 1 } }, { action: "ready", version: 1 }],
    ["other version", { origin: ORIGIN, source: game, data: { podsim: "ready", version: 2 } }, { action: "ready", version: 2 }],
    ["other origin", { origin: "http://example.com", source: game, data: { podsim: "ready", version: 1 } }, null],
    ["opaque origin", { origin: "null", source: game, data: { podsim: "ready", version: 1 } }, null],
    ["from the editor", { origin: ORIGIN, source: editor, data: { podsim: "ready", version: 1 } }, null],
    ["no source", { origin: ORIGIN, data: { podsim: "ready", version: 1 } }, null],
    ["version as text", { origin: ORIGIN, source: game, data: { podsim: "ready", version: "1" } }, null],
    ["version in an array", { origin: ORIGIN, source: game, data: { podsim: "ready", version: [1] } }, null],
    ["no version", { origin: ORIGIN, source: game, data: { podsim: "ready" } }, null],
    ["ready as text", { origin: ORIGIN, source: game, data: "ready" }, null],
  ]) {
    assert.deepEqual(shell.pageRequest(event, ORIGIN, game), want, name);
  }
  // Without the game window, the shell cannot check the source.
  assert.equal(shell.pageRequest({ origin: ORIGIN, data: { podsim: "ready", version: 1 } }, ORIGIN), null, "no game window");
  assert.equal(shell.pageRequest({ origin: ORIGIN, source: null, data: { podsim: "ready", version: 1 } }, ORIGIN, null), null, "null game window");
});

test("the game is ready only after the frame load sent the shell version", () => {
  const game = {};
  const ready = (version) => shell.pageRequest({ origin: ORIGIN, source: game, data: { podsim: "ready", version } }, ORIGIN, game);
  const cases = [
    ["first load", [], false],
    ["matching ready", [ready(shell.SHELL_VERSION)], true],
    ["other version", [ready(shell.SHELL_VERSION + 1)], false],
    ["older version", [ready(0)], false],
    ["NaN version", [ready(Number.NaN)], false],
    ["ignored message", [null], false],
    ["other request after ready", [ready(shell.SHELL_VERSION), { action: "debug" }, null, undefined], true],
    ["frame load after ready", [ready(shell.SHELL_VERSION), "load"], false],
    ["new ready after a frame load", [ready(shell.SHELL_VERSION), "load", ready(shell.SHELL_VERSION)], true],
    ["other version after a frame load", [ready(shell.SHELL_VERSION), "load", ready(shell.SHELL_VERSION + 1)], false],
  ];
  for (const [name, changes, want] of cases) {
    let state = false;
    for (const change of changes) state = shell.gameReady(state, change);
    assert.equal(state, want, name);
  }
});

test("the shell controls show without the focus only while the game shows and is not ready", () => {
  for (const [view, ready, want] of [
    ["game", false, true],
    ["game", true, false],
    ["editor", false, false],
    ["editor", true, false],
  ]) {
    assert.equal(shell.controlsShow(view, ready), want, `${view} ready ${ready}`);
  }
});

test("the shell and the game use the same message version", () => {
  const source = fs.readFileSync(path.join(__dirname, "..", "internal", "view", "shell.go"), "utf8");
  const match = source.match(/^const ShellVersion = (\d+)$/m);
  assert.ok(match, "internal/view/shell.go has no ShellVersion");
  assert.equal(Number(match[1]), shell.SHELL_VERSION);
});

test("the shell page shows its controls until the game frame is ready", () => {
  const html = fs.readFileSync(path.join(__dirname, "index.html"), "utf8");
  // The controls show before the first ready message.
  assert.match(html, /<nav aria-label="Scenario tools" class="fallback">/);
  assert.match(html, /nav\.classList\.toggle\("fallback", PodsimShell\.controlsShow\(view, gameReady\)\)/);
  assert.match(html, /PodsimShell\.pageRequest\(event, location\.origin, frames\.game\.contentWindow\)/);
  assert.match(html, /frames\.game\.addEventListener\("load", \(\) => updateControls\("load"\)\)/);
  // A frame reload shows the controls before the new game loads.
  assert.match(html, /updateControls\("load"\);\s*frames\.game\.contentWindow\.location\.reload\(\);/);
  assert.match(html, /case "ready":\s*updateControls\(request\);/);
});

test("reloadTarget keeps a loaded editor", () => {
  assert.equal(shell.reloadTarget(false), "page");
  assert.equal(shell.reloadTarget(true), "game");
});

test("noticeMessage gives the notice shape that the game accepts", () => {
  // cmd/podsim/server_js.go accepts only a string text and a boolean error.
  for (const [name, text, error, want] of [
    ["result", "Debug state downloaded: tick 1200.", false, { podsim: "notice", text: "Debug state downloaded: tick 1200.", error: false }],
    ["failure", "Capture failed. State HTTP 503. Try again.", true, { podsim: "notice", text: "Capture failed. State HTTP 503. Try again.", error: true }],
    ["no error value", "Done.", undefined, { podsim: "notice", text: "Done.", error: false }],
    ["error as text", "Done.", "true", { podsim: "notice", text: "Done.", error: false }],
    ["number text", 12, false, { podsim: "notice", text: "12", error: false }],
  ]) {
    const message = shell.noticeMessage(text, error);
    assert.deepEqual(message, want, name);
    assert.equal(typeof message.text, "string", name);
    assert.equal(typeof message.error, "boolean", name);
    assert.equal(shell.pageRequest({ origin: ORIGIN, data: message }, ORIGIN), null, `${name}: the shell ignores its own notice`);
  }
});

test("each view shows the shell controls that lead away from it", () => {
  assert.deepEqual(shell.VIEWS.game.controls, ["editorLink", "debugButton"]);
  assert.deepEqual(shell.VIEWS.editor.controls, ["gameButton", "debugButton"]);
  for (const [name, view] of Object.entries(shell.VIEWS)) {
    for (const id of view.controls) assert.ok(shell.CONTROLS.includes(id), `${name}: ${id}`);
  }
});

test("focusTarget keeps a keyboard user out of the game", () => {
  for (const [view, keyboard, want] of [
    ["game", false, "game"],
    ["game", true, "editorLink"],
    ["editor", false, "editor"],
    ["editor", true, "editor"],
  ]) {
    assert.equal(shell.focusTarget(view, keyboard), want, `${view} keyboard ${keyboard}`);
  }
});

test("a keyboard switch to the game focuses a shell control that shows", () => {
  const target = shell.focusTarget("game", true);
  assert.ok(shell.CONTROLS.includes(target), target);
  assert.ok(shell.VIEWS.game.controls.includes(target), target);
});

test("the shell controls come first in the page, in the order of CONTROLS", () => {
  const html = fs.readFileSync(path.join(__dirname, "index.html"), "utf8");
  const body = html.slice(html.indexOf("<body>"));
  // Tab from the start of the page reaches the controls before the frames.
  const ids = [...body.matchAll(/<(a|button|iframe)\b[^>]*\bid="([^"]+)"/g)].map((match) => match[2]);
  assert.deepEqual(ids, [...shell.CONTROLS, "gameFrame"]);
  assert.match(body, /<a id="editorLink" href="#editor">Edit scenario<\/a>/);
  assert.match(body, /<button id="gameButton" type="button" hidden>Simulation<\/button>/);
  assert.match(body, /<button id="debugButton" type="button"[^>]*>Download debug state<\/button>/);
});

test("the shell controls are hidden until they get the focus", () => {
  const html = fs.readFileSync(path.join(__dirname, "index.html"), "utf8");
  assert.match(html, /nav:not\(\.fallback\) a:not\(:focus\), nav:not\(\.fallback\) button:not\(:focus\), #debugStatus \{[^}]*clip-path: inset\(50%\)/);
  assert.match(html, /<span id="debugStatus" role="status"><\/span>/);
  assert.doesNotMatch(html, /<nav[^>]*\bhidden\b/, "a hidden nav removes the controls from the Tab order");
});

test("the page titles match the pages that the shell shows", () => {
  for (const [name, file] of [["game", "index.html"], ["editor", "editor.html"]]) {
    const html = fs.readFileSync(path.join(__dirname, file), "utf8");
    assert.equal(html.match(/<title>(.*)<\/title>/)[1], shell.VIEWS[name].title, file);
  }
});
