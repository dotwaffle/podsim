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

test("pageRequest accepts only a show or debug message from the same origin", () => {
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
  assert.match(html, /nav a:not\(:focus\), nav button:not\(:focus\), #debugStatus \{[^}]*clip-path: inset\(50%\)/);
  assert.match(html, /<span id="debugStatus" role="status"><\/span>/);
  assert.doesNotMatch(html, /<nav[^>]*\bhidden\b/, "a hidden nav removes the controls from the Tab order");
});

test("the page titles match the pages that the shell shows", () => {
  for (const [name, file] of [["game", "index.html"], ["editor", "editor.html"]]) {
    const html = fs.readFileSync(path.join(__dirname, file), "utf8");
    assert.equal(html.match(/<title>(.*)<\/title>/)[1], shell.VIEWS[name].title, file);
  }
});
