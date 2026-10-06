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

test("debugResult keeps a failure and gives other text the notice time of the game", () => {
  const source = fs.readFileSync(path.join(__dirname, "..", "internal", "view", "shared.go"), "utf8");
  const match = source.match(/^const noticeDuration = (\d+) \* sim\.TicksPerSecond$/m);
  assert.ok(match, "internal/view/shared.go has no noticeDuration in seconds");
  assert.equal(shell.NOTICE_MS, Number(match[1]) * 1000);
  for (const [name, text, error, want] of [
    ["result", "Debug state downloaded: tick 42.", false, { text: "Debug state downloaded: tick 42.", error: false, expiresAt: 1000 + shell.NOTICE_MS, sent: false }],
    ["no error value", "Done.", undefined, { text: "Done.", error: false, expiresAt: 1000 + shell.NOTICE_MS, sent: false }],
    ["error as text", "Done.", "true", { text: "Done.", error: false, expiresAt: 1000 + shell.NOTICE_MS, sent: false }],
    ["failure", "Capture failed. State HTTP 503. Try again.", true, { text: "Capture failed. State HTTP 503. Try again.", error: true, expiresAt: null, sent: false }],
  ]) {
    assert.deepEqual(shell.debugResult(text, error, 1000), want, name);
  }
});

test("reconcileResult sends an active result once to each ready game", () => {
  const success = shell.debugResult("Debug state downloaded: tick 42.", false, 0);
  const failure = shell.debugResult("Capture failed. State HTTP 503. Try again.", true, 0);
  const late = 60 * 60 * 1000;
  // steps has [ready, now] for each change. want has the sent notices, in
  // order, and if a result stays at the end.
  for (const [name, start, steps, want] of [
    ["no result", null, [[true, 0], [false, 1], [true, 2]], { notices: [], stays: false }],
    ["success while ready", success, [[true, 0]], { notices: [success], stays: true }],
    ["failure while ready", failure, [[true, 0]], { notices: [failure], stays: true }],
    ["success while not ready", success, [[false, 0]], { notices: [], stays: true }],
    ["failure stays while not ready", failure, [[false, 0], [false, late]], { notices: [], stays: true }],
    ["failure then ready", failure, [[false, 0], [true, late]], { notices: [failure], stays: true }],
    ["success then ready before it expires", success, [[false, 0], [true, shell.NOTICE_MS - 1]], { notices: [success], stays: true }],
    ["success then ready after it expires", success, [[false, 0], [true, shell.NOTICE_MS]], { notices: [], stays: false }],
    ["success expires while not ready", success, [[false, 0], [false, shell.NOTICE_MS]], { notices: [], stays: false }],
    ["repeated ready", failure, [[false, 0], [true, 1], [true, 2], [true, 3]], { notices: [failure], stays: true }],
    ["repeated ready with a success", success, [[true, 0], [true, 1], [true, 2]], { notices: [success], stays: true }],
    ["ready then reload", failure, [[true, 0], [false, 1]], { notices: [failure], stays: true }],
    ["ready, reload and ready", failure, [[true, 0], [false, 1], [true, 2]], { notices: [failure, failure], stays: true }],
    ["success sent then expires", success, [[true, 0], [false, shell.NOTICE_MS], [true, shell.NOTICE_MS + 1]], { notices: [success], stays: false }],
  ]) {
    let result = start;
    const notices = [];
    for (const [ready, now] of steps) {
      const change = shell.reconcileResult(result, ready, now);
      result = change.result;
      if (change.notice) notices.push(change.notice);
      if (!ready) assert.equal(change.notice, null, `${name}: a notice to a game that is not ready`);
    }
    assert.deepEqual(notices, want.notices.map((entry) => shell.noticeMessage(entry.text, entry.error)), name);
    assert.equal(result !== null, want.stays, name);
    if (result !== null) assert.equal(result.text, start.text, name);
  }
});

test("the shell page shows the debug result next to its controls", () => {
  const html = fs.readFileSync(path.join(__dirname, "index.html"), "utf8");
  // The status is hidden only while the nav has no fallback class or the
  // status has no text.
  assert.match(html, /#debugStatus \{[^}]*padding: 6px 12px;[^}]*color: #9fe4eb;/);
  assert.doesNotMatch(html, /[{}]\s*#debugStatus \{[^}]*clip-path/, "the status is always hidden");
  // A failure shows in the amber of the game.
  assert.match(html, /#debugStatus\.error \{ color: #f3c479; \}/);
  const game = fs.readFileSync(path.join(__dirname, "..", "internal", "view", "game.go"), "utf8");
  assert.match(game, /^\s*amber\s*= 0xf3c479$/m, "the amber of the game changed");
  // A new status stops the timer of the status before it. A result without
  // a failure goes at its expiresAt.
  assert.match(html, /status\.classList\.toggle\("error", error\);/);
  assert.match(html, /function showStatus\(text, result, error = false\) \{\s*clearTimeout\(statusTimer\);/);
  assert.match(html, /debugResult = result \? PodsimShell\.debugResult\(text, error, Date\.now\(\)\) : null;/);
  assert.match(html, /if \(debugResult\?\.expiresAt != null\) \{\s*statusTimer = setTimeout\(/);
  // The shell sends the result only after reconcileResult, and it
  // reconciles after each change of the result or of the game.
  assert.match(html, /const \{ result, notice \} = PodsimShell\.reconcileResult\(debugResult, gameReady, Date\.now\(\)\);\s*debugResult = result;\s*if \(notice\) frames\.game\.contentWindow\?\.postMessage\(notice, location\.origin\);/);
  assert.match(html, /nav\.classList\.toggle\("fallback", PodsimShell\.controlsShow\(view, gameReady\)\);\s*reconcileStatus\(\);/);
  assert.match(html, /\}, debugResult\.expiresAt - Date\.now\(\)\);\s*\}\s*reconcileStatus\(\);/);
  assert.equal(html.match(/postMessage\(/g).length, 1, "the shell sends a notice only from reconcileStatus");
  // The success and failure texts that the status shows.
  assert.match(html, /showStatus\(`Debug state downloaded: tick \$\{state\.simulation\.tick\}\.`, true\);/);
  assert.match(html, /showStatus\(`Capture failed\. \$\{error\.message\}\. Try again\.`, true, true\);/);
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

// nested gives levels objects, each in the member next of the one
// before. A reply with nested(n) as its topology has n + 1 levels.
function nested(levels) {
  let value = {};
  for (let level = 1; level < levels; level++) value = { next: value };
  return value;
}

// stateReplyRefusals gives the state replies that the debug capture and
// the editor refuse because of their contract markers, their topology, a
// textEncoding member or their size. envelope(markers) gives a valid reply with those
// contract markers at the root, in the topology and in the simulation. The
// incident, fault and emergency markers are only in the topology and in the
// simulation.
// editor_test.cjs has the same table.
function stateReplyRefusals(envelope) {
  const plain = envelope();
  const express = envelope({ orderContract: "express-v1" });
  const simulation = (reply, change) => ({ ...reply, frame: { ...reply.frame, state: { ...reply.frame.state, simulation: { ...reply.frame.state.simulation, ...change } } } });
  // incidentReply gives a plain reply with the incident marker value in
  // the topology and in the simulation, where the server puts it.
  const incidentReply = (value) => ({ ...simulation(plain, { incidentContract: value }), topology: { incidentContract: value } });
  const incident = incidentReply("incident-v1");
  // faultReply gives an incident reply with the fault marker value in the
  // topology and in the simulation, where the server puts it.
  const faultReply = (value) => ({ ...simulation(incident, { faultContract: value }), topology: { incidentContract: "incident-v1", faultContract: value } });
  const fault = faultReply("fault-v1");
  // emergencyReply gives an incident reply with the emergency marker value
  // in the topology and in the simulation, where the server puts it.
  const emergencyReply = (value) => ({ ...simulation(incident, { emergencyContract: value }), topology: { incidentContract: "incident-v1", emergencyContract: value } });
  const emergency = emergencyReply("emergency-v1");
  const without = (value, name) => Object.fromEntries(Object.entries(value).filter(([key]) => key !== name));
  const cases = [];
  // Earlier servers sent a textEncoding member. Its presence anywhere in
  // the reply refuses the reply, whatever its value.
  for (const [kind, value] of [["", "order-text-base64-v1"], ["an empty ", ""], ["a null ", null]]) {
    cases.push(
      [`${kind}textEncoding member at the root`, { ...express, textEncoding: value }],
      [`${kind}textEncoding member in the frame`, { ...plain, frame: { ...plain.frame, textEncoding: value } }],
      [`${kind}textEncoding member in the state`, { ...plain, frame: { ...plain.frame, state: { ...plain.frame.state, textEncoding: value } } }],
      [`${kind}textEncoding member in the topology`, { ...express, topology: { ...express.topology, textEncoding: value } }],
      [`${kind}textEncoding member in the simulation`, simulation(express, { textEncoding: value })],
      [`${kind}textEncoding member in a route`, { ...plain, frame: { ...plain.frame, routes: [{ lanes: [], textEncoding: value }] } }],
    );
  }
  cases.push(
    // The limits of a stream document of the server.
    ["a reply over the depth limit", { ...plain, topology: nested(64) }],
    ["a reply with an array over the element limit", { ...plain, topology: { lanes: new Array(65537).fill(0) } }],
    // The root, the topology and the simulation have the same markers.
    ["an Express root with an unmarked topology and simulation", { ...plain, orderContract: "express-v1" }],
    ["an unmarked root with an Express topology and simulation", without(express, "orderContract")],
    ["an Express root and topology with an unmarked simulation", { ...plain, orderContract: "express-v1", topology: { orderContract: "express-v1" } }],
    ["an unmarked Express topology", { ...express, topology: {} }],
    // The server decoder refuses a reply without a topology object.
    ["a reply without a topology", without(plain, "topology")],
    ["a reply with a null topology", { ...plain, topology: null }],
    ["a reply with a topology array", { ...plain, topology: [] }],
    ["a reply with a topology that is text", { ...plain, topology: "topology" }],
    // A marker has the one value that the server sends.
    ["an unknown order contract", envelope({ orderContract: "express-v2" })],
    ["an empty order contract", envelope({ orderContract: "" })],
    ["a null order contract", envelope({ orderContract: null })],
    ["an unknown order contract at the root only", { ...plain, orderContract: "express-v2" }],
    // The topology and the simulation carry the incident marker, and the
    // root does not. They have the same marker, with the one value that
    // the server sends.
    ["an incident marker at the root", { ...incident, incidentContract: "incident-v1" }],
    ["an incident marker at the root only", { ...plain, incidentContract: "incident-v1" }],
    ["an incident topology with an unmarked simulation", { ...plain, topology: { incidentContract: "incident-v1" } }],
    ["an incident simulation with an unmarked topology", simulation(plain, { incidentContract: "incident-v1" })],
  );
  for (const [kind, value] of [["an unknown", "incident-v2"], ["an empty", ""], ["a null", null]]) {
    cases.push(
      [`${kind} incident contract`, incidentReply(value)],
      [`${kind} incident contract in the simulation`, simulation(incident, { incidentContract: value })],
      [`${kind} incident contract in the topology`, { ...incident, topology: { incidentContract: value } }],
    );
  }
  cases.push(
    // The topology and the simulation carry the fault marker, and the root
    // does not. They have the same marker, with the one value that the
    // server sends, and the fault marker needs the incident marker.
    ["a fault marker at the root", { ...fault, faultContract: "fault-v1" }],
    ["a fault marker at the root only", { ...incident, faultContract: "fault-v1" }],
    ["a fault topology with an unmarked simulation", { ...incident, topology: { incidentContract: "incident-v1", faultContract: "fault-v1" } }],
    ["a fault simulation with an unmarked topology", simulation(incident, { faultContract: "fault-v1" })],
    ["a fault marker without the incident marker", { ...simulation(plain, { faultContract: "fault-v1" }), topology: { faultContract: "fault-v1" } }],
  );
  for (const [kind, value] of [["an unknown", "fault-v2"], ["an empty", ""], ["a null", null]]) {
    cases.push(
      [`${kind} fault contract`, faultReply(value)],
      [`${kind} fault contract in the simulation`, simulation(fault, { faultContract: value })],
      [`${kind} fault contract in the topology`, { ...fault, topology: { incidentContract: "incident-v1", faultContract: value } }],
      [`${kind} fault contract in the topology of an unmarked reply`, { ...incident, topology: { incidentContract: "incident-v1", faultContract: value } }],
      [`${kind} fault contract in the simulation of an unmarked reply`, simulation(incident, { faultContract: value })],
    );
  }
  cases.push(
    // The emergency marker has the rules of the fault marker.
    ["an emergency marker at the root", { ...emergency, emergencyContract: "emergency-v1" }],
    ["an emergency marker at the root only", { ...incident, emergencyContract: "emergency-v1" }],
    ["an emergency topology with an unmarked simulation", { ...incident, topology: { incidentContract: "incident-v1", emergencyContract: "emergency-v1" } }],
    ["an emergency simulation with an unmarked topology", simulation(incident, { emergencyContract: "emergency-v1" })],
    ["an emergency marker without the incident marker", { ...simulation(plain, { emergencyContract: "emergency-v1" }), topology: { emergencyContract: "emergency-v1" } }],
  );
  for (const [kind, value] of [["an unknown", "emergency-v2"], ["an empty", ""], ["a null", null]]) {
    cases.push(
      [`${kind} emergency contract`, emergencyReply(value)],
      [`${kind} emergency contract in the simulation`, simulation(emergency, { emergencyContract: value })],
      [`${kind} emergency contract in the topology`, { ...emergency, topology: { incidentContract: "incident-v1", emergencyContract: value } }],
      [`${kind} emergency contract in the topology of an unmarked reply`, { ...incident, topology: { incidentContract: "incident-v1", emergencyContract: value } }],
      [`${kind} emergency contract in the simulation of an unmarked reply`, simulation(incident, { emergencyContract: value })],
    );
  }
  return cases;
}

test("the debug capture and the editor have the same state reply limits", () => {
  const editor = require("./editor.js");
  assert.equal(shell.MAX_STATE_DEPTH, 64);
  assert.equal(shell.MAX_STATE_ELEMENTS, 65536);
  assert.equal(editor.MAX_STATE_DEPTH, shell.MAX_STATE_DEPTH);
  assert.equal(editor.MAX_STATE_ELEMENTS, shell.MAX_STATE_ELEMENTS);
});

test("the debug capture accepts the state reply of each project kind", () => {
  const state = { epoch: "epoch-1", projectRevision: 3, simulation: { tick: 7 }, orders: "packed" };
  // envelope gives a reply with markers at the root, in the topology and
  // in the simulation, as the server sends it.
  const envelope = (markers = {}) => ({ ...markers, topology: { ...markers }, frame: { state: { ...state, simulation: { ...state.simulation, ...markers } }, routes: [] } });
  const valid = [
    ["a plain project", envelope()],
    ["an Express project", envelope({ orderContract: "express-v1" })],
    ["an incident project", { ...envelope(), topology: { incidentContract: "incident-v1" }, frame: { state: { ...state, simulation: { ...state.simulation, incidentContract: "incident-v1" } }, routes: [] } }],
    ["a fault project", { ...envelope(), topology: { incidentContract: "incident-v1", faultContract: "fault-v1" }, frame: { state: { ...state, simulation: { ...state.simulation, incidentContract: "incident-v1", faultContract: "fault-v1" } }, routes: [] } }],
    ["an emergency project", { ...envelope(), topology: { incidentContract: "incident-v1", emergencyContract: "emergency-v1" }, frame: { state: { ...state, simulation: { ...state.simulation, incidentContract: "incident-v1", emergencyContract: "emergency-v1", emergencies: {} } }, routes: [] } }],
    ["a fault and emergency project", { ...envelope(), topology: { incidentContract: "incident-v1", faultContract: "fault-v1", emergencyContract: "emergency-v1" }, frame: { state: { ...state, simulation: { ...state.simulation, incidentContract: "incident-v1", faultContract: "fault-v1", emergencyContract: "emergency-v1" } }, routes: [] } }],
    ["a reply of the depth limit", { ...envelope(), topology: nested(shell.MAX_STATE_DEPTH - 1) }],
    ["a reply with an array of the element limit", { ...envelope(), topology: { lanes: new Array(shell.MAX_STATE_ELEMENTS).fill(0) } }],
  ];
  for (const [name, reply] of valid) assert.equal(shell.captureState(reply), reply.frame.state, name);
  for (const [name, reply] of stateReplyRefusals(envelope)) assert.throws(() => shell.captureState(reply), { message: "Invalid server state reply" }, name);
  const malformed = [
    ["a plain state without the envelope", state],
    ["an envelope with a null frame", { orderContract: "express-v1", frame: null }],
    ["an envelope with a frame array", { orderContract: "express-v1", frame: [state] }],
    ["an envelope without a state", { orderContract: "express-v1", frame: { routes: [] } }],
    ["an envelope with a state array", { orderContract: "express-v1", frame: { state: [] } }],
    ["an envelope with a revision only", { orderContract: "express-v1", frame: { state: { projectRevision: 3 } } }],
    ["an envelope with a nested error", { orderContract: "express-v1", frame: { state: { error: "bad" } } }],
    ["a reply that is not an object", null],
    ["no epoch", { topology: {}, frame: { state: { ...state, epoch: undefined } } }],
    ["an empty epoch", { topology: {}, frame: { state: { ...state, epoch: "" } } }],
    ["a revision that is text", { topology: {}, frame: { state: { ...state, projectRevision: "3" } } }],
    ["no simulation", { topology: {}, frame: { state: { ...state, simulation: undefined } } }],
    ["a null simulation", { topology: {}, frame: { state: { ...state, simulation: null } } }],
    ["a tick that is text", { topology: {}, frame: { state: { ...state, simulation: { tick: "7" } } } }],
  ];
  for (const [name, reply] of malformed) assert.throws(() => shell.captureState(reply), { message: "Invalid server state reply" }, name);
  // The editor reads the state with the same Accept header.
  const editor = require("./editor.js");
  assert.equal(shell.STATE_ACCEPT, editor.LIVE_STATE_ACCEPT);
  assert.equal(shell.STATE_ACCEPT, "application/vnd.podsim.state-6+json");
  const html = fs.readFileSync(path.join(__dirname, "index.html"), "utf8");
  assert.match(html, /fetch\("\/api\/state", \{[^}]*headers: \{ Accept: PodsimShell\.STATE_ACCEPT \}/);
  assert.match(html, /const frame = await PodsimShell\.readCaptureState\(stateResponse\);/);
});

test("the debug capture reads only a reply of the state media type", async () => {
  const state = { epoch: "epoch-1", projectRevision: 3, simulation: { tick: 7 } };
  const envelope = { topology: {}, frame: { state, routes: [] } };
  // reply gives a response to the capture. It records each body read, so a
  // test can check that a refused reply was not read.
  const reads = [];
  const reply = (name, body, headers, status = 200) => {
    const response = new Response(JSON.stringify(body), { status, headers });
    const json = response.json.bind(response);
    response.json = () => { reads.push(name); return json(); };
    return response;
  };
  const media = "Unsupported server state media type";
  const cases = [
    { name: "the state media type", body: envelope, headers: { "Content-Type": shell.STATE_ACCEPT }, want: state },
    { name: "the state media type with a parameter", body: envelope, headers: { "Content-Type": "Application/VND.podsim.state-6+json ; charset=utf-8" }, want: state },
    { name: "plain JSON", body: envelope, headers: { "Content-Type": "application/json" }, wantError: media },
    { name: "the Express media type", body: { ...envelope, orderContract: "express-v1", textEncoding: "order-text-base64-v1" }, headers: { "Content-Type": "application/vnd.podsim.express-v1+json" }, wantError: media },
    { name: "an earlier state media type", body: envelope, headers: { "Content-Type": "application/vnd.podsim.state-5+json" }, wantError: media },
    { name: "no media type", body: envelope, headers: {}, wantError: media },
    { name: "a textEncoding member", body: { ...envelope, textEncoding: "order-text-base64-v1" }, headers: { "Content-Type": shell.STATE_ACCEPT }, wantError: "Invalid server state reply" },
    { name: "HTTP 406", body: { error: "use the state media type" }, headers: { "Content-Type": "text/plain; charset=utf-8" }, status: 406, wantError: "State HTTP 406" },
  ];
  for (const item of cases) {
    reads.length = 0;
    const response = reply(item.name, item.body, item.headers, item.status);
    if (item.wantError) {
      await assert.rejects(shell.readCaptureState(response), { message: item.wantError }, item.name);
      if (item.wantError !== "Invalid server state reply") assert.deepEqual(reads, [], `${item.name}: the body was read`);
    } else {
      assert.deepEqual(await shell.readCaptureState(response), item.want, item.name);
    }
  }
});

test("the shell controls are hidden until they get the focus", () => {
  const html = fs.readFileSync(path.join(__dirname, "index.html"), "utf8");
  assert.match(html, /nav:not\(\.fallback\) a:not\(:focus\), nav:not\(\.fallback\) button:not\(:focus\), nav:not\(\.fallback\) #debugStatus, #debugStatus:empty \{[^}]*clip-path: inset\(50%\)/);
  assert.match(html, /<span id="debugStatus" role="status"><\/span>/);
  assert.doesNotMatch(html, /<nav[^>]*\bhidden\b/, "a hidden nav removes the controls from the Tab order");
});

test("the page titles match the pages that the shell shows", () => {
  for (const [name, file] of [["game", "index.html"], ["editor", "editor.html"]]) {
    const html = fs.readFileSync(path.join(__dirname, file), "utf8");
    assert.equal(html.match(/<title>(.*)<\/title>/)[1], shell.VIEWS[name].title, file);
  }
});
