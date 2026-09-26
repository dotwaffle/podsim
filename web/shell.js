(function (root) {
  "use strict";

  // VIEWS has the frames that the shell page can show. hash is the URL hash
  // that selects the frame. title is the page title while the frame shows.
  // controls has the IDs of the shell controls that the page shows with the
  // frame. The controls are hidden until they get the keyboard focus.
  const VIEWS = {
    game: { hash: "", title: "Podsim: Network playground", controls: ["editorLink", "debugButton"] },
    editor: { hash: "#editor", title: "Podsim scenario editor", controls: ["gameButton", "debugButton"] },
  };

  // CONTROLS has the IDs of all shell controls, in their page order.
  const CONTROLS = ["editorLink", "gameButton", "debugButton"];

  // viewForHash gives the view that the URL hash selects. #editor selects the
  // editor. Each other hash selects the game.
  function viewForHash(hash) {
    return hash === VIEWS.editor.hash ? "editor" : "game";
  }

  // pageRequest gives the request of a message event to the shell page. It
  // gives null when the shell must ignore the message. The shell accepts
  // only a message from its own origin with one of these data shapes:
  //
  //   { podsim: "show", view, keyboard } asks the shell to show view. view
  //   is a name in VIEWS. keyboard is absent or a boolean. It is true when a
  //   keyboard caused the request. The request is { action: "show", view,
  //   keyboard }.
  //
  //   { podsim: "debug" } asks the shell to download a debug capture. The
  //   game sends it from its Download debug state button. The request is
  //   { action: "debug" }.
  function pageRequest(event, origin) {
    const data = event?.data;
    if (event?.origin !== origin) return null;
    switch (data?.podsim) {
      case "show":
        if (!Object.keys(VIEWS).includes(data.view)) return null;
        if (data.keyboard !== undefined && typeof data.keyboard !== "boolean") return null;
        return { action: "show", view: data.view, keyboard: data.keyboard === true };
      case "debug":
        return { action: "debug" };
      default:
        return null;
    }
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

  const API = { VIEWS, CONTROLS, viewForHash, pageRequest, noticeMessage, focusTarget };
  if (typeof module !== "undefined" && module.exports) module.exports = API;
  root.PodsimShell = API;
})(typeof window !== "undefined" ? window : globalThis);
