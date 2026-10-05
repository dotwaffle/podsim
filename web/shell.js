(function (root) {
  "use strict";

  // VIEWS has the frames that the shell page can show. hash is the URL hash
  // that selects the frame. title is the page title while the frame shows.
  // controls has the IDs of the shell controls that the page shows with the
  // frame. The controls are hidden until they get the keyboard focus, but see
  // controlsShow.
  const VIEWS = {
    game: { hash: "", title: "Podsim: Network playground", controls: ["editorLink", "debugButton"] },
    editor: { hash: "#editor", title: "Podsim scenario editor", controls: ["gameButton", "debugButton"] },
  };

  // CONTROLS has the IDs of all shell controls, in their page order.
  const CONTROLS = ["editorLink", "gameButton", "debugButton"];

  // SHELL_VERSION is the version of the messages between the game and the
  // shell page. The game sends its version in a ready message. See
  // ShellVersion in internal/view/shell.go.
  const SHELL_VERSION = 1;

  // viewForHash gives the view that the URL hash selects. #editor selects the
  // editor. Each other hash selects the game.
  function viewForHash(hash) {
    return hash === VIEWS.editor.hash ? "editor" : "game";
  }

  // pageRequest gives the request of a message event to the shell page. It
  // gives null when the shell must ignore the message. game is the window of
  // the game frame. The shell accepts only a message from its own origin
  // with one of these data shapes:
  //
  //   { podsim: "show", view, keyboard } asks the shell to show view. view
  //   is a name in VIEWS. keyboard is absent or a boolean. It is true when a
  //   keyboard caused the request. The request is { action: "show", view,
  //   keyboard }.
  //
  //   { podsim: "debug" } asks the shell to download a debug capture. The
  //   game sends it from its Download debug state button. The request is
  //   { action: "debug" }.
  //
  //   { podsim: "reload" } asks the shell to reload after a change of the
  //   server build. The game sends it. The request is { action: "reload" }.
  //   See reloadTarget.
  //
  //   { podsim: "ready", version } tells the shell that the game runs. The
  //   game sends it once when it starts. version is a number. It is the
  //   version of the messages that the game uses. The shell accepts it only
  //   from the game frame. The request is { action: "ready", version }. See
  //   gameReady.
  function pageRequest(event, origin, game) {
    const data = event?.data;
    if (event?.origin !== origin) return null;
    switch (data?.podsim) {
      case "show":
        if (!Object.keys(VIEWS).includes(data.view)) return null;
        if (data.keyboard !== undefined && typeof data.keyboard !== "boolean") return null;
        return { action: "show", view: data.view, keyboard: data.keyboard === true };
      case "debug":
        return { action: "debug" };
      case "reload":
        return { action: "reload" };
      case "ready":
        if (game == null || event.source !== game || typeof data.version !== "number") return null;
        return { action: "ready", version: data.version };
      default:
        return null;
    }
  }

  // reloadTarget gives what the shell reloads after a change of the server
  // build. Before the editor loads, the shell reloads the full page, so the
  // shell and the game get the new files. After the editor loads, the shell
  // reloads only the game frame. The editor then keeps its state and its
  // old files until the user reloads the page.
  function reloadTarget(editorLoaded) {
    return editorLoaded ? "game" : "page";
  }

  // gameReady gives if the game in the game frame is ready after change.
  // ready is the value before change. change is "load" when the game frame
  // loads again, or a request from pageRequest. The game is ready only after
  // the game frame load sent a ready message with SHELL_VERSION. A game of a
  // different build can use other messages, or it can have no Edit scenario
  // and Download debug state buttons.
  function gameReady(ready, change) {
    if (change === "load") return false;
    if (change?.action === "ready") return change.version === SHELL_VERSION;
    return ready;
  }

  // controlsShow gives if the shell controls show without the keyboard
  // focus. While the game shows and it is not ready, the controls show, so a
  // mouse or touch user can use them. Otherwise, they are hidden until they
  // get the focus. The editor has its own return link.
  function controlsShow(view, ready) {
    return view === "game" && !ready;
  }

  // noticeMessage gives the message that sends a status text to the game.
  // The game shows the text below its journey controls. error is true when
  // the text tells of a failure. Then the text stays until the next action
  // in the game or the next capture result.
  function noticeMessage(text, error) {
    return { podsim: "notice", text: String(text), error: error === true };
  }

  // NOTICE_MS is the time in milliseconds that the shell shows the result of
  // a debug capture that did not fail. The game shows its notices for the
  // same time. See noticeDuration in internal/view/shared.go.
  const NOTICE_MS = 3000;

  // debugResult gives the active result of a debug capture that the shell
  // got at the time now in milliseconds. error is true when text tells of a
  // failure. A failure stays until the next capture, so its expiresAt is
  // null. Other text goes at expiresAt, after NOTICE_MS. sent is true after
  // the shell sent the result to the game. See reconcileResult.
  function debugResult(text, error, now) {
    const failure = error === true;
    return { text: String(text), error: failure, expiresAt: failure ? null : now + NOTICE_MS, sent: false };
  }

  // STATE_ACCEPT is the Accept header of a debug capture read of /api/state.
  // The server replies only to session.StateMediaType, with the envelope of
  // the state. LIVE_STATE_ACCEPT in editor.js has the same value. A Go test
  // in internal/session checks the media type.
  const STATE_ACCEPT = "application/vnd.podsim.state-6+json";

  // MAX_STATE_DEPTH and MAX_STATE_ELEMENTS are the depth limit and the
  // element limit of each array of a stream document of the server. See
  // streamLimits in internal/session. A Go test checks the mirror, and
  // editor.js has the same values.
  const MAX_STATE_DEPTH = 64;
  const MAX_STATE_ELEMENTS = 65536;

  // isObject is true for a JSON object that is not an array. editor.js
  // has the same function.
  function isObject(value) { return value !== null && typeof value === "object" && !Array.isArray(value); }

  // CONTRACT_MARKERS gives each contract marker of a state reply and the
  // one value that the server sends.
  const CONTRACT_MARKERS = [["orderContract", "express-v1"], ["couplingContract", "compact-pair-v1"]];

  // plainStateTree is true when value has at most MAX_STATE_DEPTH levels
  // of arrays and objects, no array with more than MAX_STATE_ELEMENTS
  // elements, and no object with a textEncoding member. Earlier servers
  // sent textEncoding. Its presence refuses the reply, whatever its value.
  // The walk uses a stack, so a deep reply cannot overflow the call stack.
  // editor.js has the same function.
  function plainStateTree(value) {
    const stack = [[value, 1]];
    while (stack.length > 0) {
      const [node, depth] = stack.pop();
      if (node === null || typeof node !== "object") continue;
      if (depth > MAX_STATE_DEPTH) return false;
      if (Array.isArray(node) ? node.length > MAX_STATE_ELEMENTS : Object.hasOwn(node, "textEncoding")) return false;
      for (const child of Object.values(node)) stack.push([child, depth + 1]);
    }
    return true;
  }

  // markersAgree is true when the reply has a topology object, and the
  // reply, its topology and the simulation of state have the same contract
  // markers. Each marker that is present must have the value in
  // CONTRACT_MARKERS. The server decoder also refuses a reply without a
  // topology object. editor.js has the same function.
  function markersAgree(reply, state) {
    if (!isObject(reply.topology)) return false;
    const holders = [reply, reply.topology, state.simulation];
    return CONTRACT_MARKERS.every(([name, allowed]) => {
      const values = holders.map((holder) => Object.hasOwn(holder, name) ? holder[name] : undefined);
      return values.every((marker) => marker === values[0] && (marker === undefined || marker === allowed));
    });
  }

  // captureState gives the state of a reply to STATE_ACCEPT. The reply is
  // the envelope of the state: an object with a frame object whose state
  // member is the state. The state must have the values that the capture
  // reads, with their types: epoch, projectRevision and simulation.tick.
  // The capture keeps the other values as the server sent them, and does
  // not unpack or check the orders. The reply must pass plainStateTree
  // and markersAgree.
  function captureState(reply) {
    const state = isObject(reply) && isObject(reply.frame) ? reply.frame.state : null;
    if (!isObject(state) || !plainStateTree(reply) || !isObject(state.simulation) || !markersAgree(reply, state) ||
      typeof state.epoch !== "string" || state.epoch === "" || !Number.isSafeInteger(state.projectRevision) ||
      !Number.isSafeInteger(state.simulation?.tick)) {
      throw new Error("Invalid server state reply");
    }
    return state;
  }

  // readCaptureState gives the state of a response to STATE_ACCEPT. See
  // captureState. A server of another version replies with another media
  // type, and readCaptureState refuses that reply before it reads the body.
  async function readCaptureState(response) {
    if (!response.ok) throw new Error(`State HTTP ${response.status}`);
    const media = (response.headers.get("Content-Type") ?? "").split(";")[0].trim().toLowerCase();
    if (media !== STATE_ACCEPT) throw new Error("Unsupported server state media type");
    return captureState(await response.json());
  }

  // reconcileResult gives the active result after a change of the result or
  // of the game, and the notice that the shell sends to the game. The notice
  // is null when the shell sends nothing. result is from debugResult, or null
  // when there is no result. ready is true when the game is ready. See
  // gameReady. now is the time in milliseconds.
  //
  // A result that expired goes. While the game is not ready, it can be of a
  // different build that cannot show a notice. Then the debug status shows
  // the result next to the shell controls, and the result is not sent. When
  // the game is ready, the shell sends a result that it did not send to this
  // game. The game shows a notice for NOTICE_MS and cannot show it for a
  // shorter time, so a result can show for longer in the game than in the
  // shell. A frame load makes the game not ready, so the shell sends the
  // result again to the new game when it is ready.
  function reconcileResult(result, ready, now) {
    if (result == null || (result.expiresAt !== null && now >= result.expiresAt)) return { result: null, notice: null };
    if (!ready) return { result: { ...result, sent: false }, notice: null };
    if (result.sent) return { result, notice: null };
    return { result: { ...result, sent: true }, notice: noticeMessage(result.text, result.error) };
  }

  // focusTarget gives the element that gets the keyboard focus after the
  // shell shows view. It gives "editorLink" for the Edit scenario control,
  // or the name of the frame. The game keeps the Tab key, so a switch to the
  // game with the keyboard does not move the focus into the game. The focus
  // goes to the Edit scenario control, and Tab still moves through the
  // page.
  function focusTarget(view, keyboard) {
    return view === "game" && keyboard ? "editorLink" : view;
  }

  const API = { VIEWS, CONTROLS, SHELL_VERSION, NOTICE_MS, viewForHash, pageRequest, gameReady, controlsShow, reloadTarget, noticeMessage, debugResult, STATE_ACCEPT, MAX_STATE_DEPTH, MAX_STATE_ELEMENTS, captureState, readCaptureState, reconcileResult, focusTarget };
  if (typeof module !== "undefined" && module.exports) module.exports = API;
  root.PodsimShell = API;
})(typeof window !== "undefined" ? window : globalThis);
