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
  // the text tells of a failure. Then the text stays until the next action.
  function noticeMessage(text, error) {
    return { podsim: "notice", text: String(text), error: error === true };
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

  const API = { VIEWS, CONTROLS, SHELL_VERSION, viewForHash, pageRequest, gameReady, controlsShow, reloadTarget, noticeMessage, focusTarget };
  if (typeof module !== "undefined" && module.exports) module.exports = API;
  root.PodsimShell = API;
})(typeof window !== "undefined" ? window : globalThis);
