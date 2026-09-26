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

test("showRequest accepts only a show message from the same origin", () => {
  const game = { view: "game", keyboard: false };
  for (const [name, event, want] of [
    ["game", { origin: ORIGIN, data: { podsim: "show", view: "game" } }, game],
    ["editor", { origin: ORIGIN, data: { podsim: "show", view: "editor" } }, { view: "editor", keyboard: false }],
    ["game with the keyboard", { origin: ORIGIN, data: { podsim: "show", view: "game", keyboard: true } }, { view: "game", keyboard: true }],
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
    ["other action", { origin: ORIGIN, data: { podsim: "hide", view: "game" } }, null],
    ["other message", { origin: ORIGIN, data: { view: "game" } }, null],
    ["text data", { origin: ORIGIN, data: "show game" }, null],
    ["null data", { origin: ORIGIN, data: null }, null],
    ["no event", undefined, null],
  ]) {
    assert.deepEqual(shell.showRequest(event, ORIGIN), want, name);
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

test("the shell page has the element that focusTarget names", () => {
  const html = fs.readFileSync(path.join(__dirname, "index.html"), "utf8");
  assert.match(html, /<a id="editorLink" href="#editor">Edit scenario<\/a>/);
});

test("the page titles match the pages that the shell shows", () => {
  for (const [name, file] of [["game", "index.html"], ["editor", "editor.html"]]) {
    const html = fs.readFileSync(path.join(__dirname, file), "utf8");
    assert.equal(html.match(/<title>(.*)<\/title>/)[1], shell.VIEWS[name].title, file);
  }
});
