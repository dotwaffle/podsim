(function (root) {
  "use strict";

  // VIEWS has the frames that the shell page can show. hash is the URL hash
  // that selects the frame. title is the page title while the frame shows.
  const VIEWS = {
    game: { hash: "", title: "Podsim: Network playground" },
    editor: { hash: "#editor", title: "Podsim scenario editor" },
  };

  // viewForHash gives the view that the URL hash selects. #editor selects the
  // editor. Each other hash selects the game.
  function viewForHash(hash) {
    return hash === VIEWS.editor.hash ? "editor" : "game";
  }

  // showRequest gives the view that a message event asks the shell page to
  // show, and tells if a keyboard caused the request. It gives null when the
  // shell must ignore the message. The shell accepts only a message from its
  // own origin, with the data { podsim: "show", view, keyboard }, a view in
  // VIEWS, and no keyboard or a boolean keyboard.
  function showRequest(event, origin) {
    const data = event?.data;
    if (event?.origin !== origin || data?.podsim !== "show") return null;
    if (!Object.keys(VIEWS).includes(data.view)) return null;
    if (data.keyboard !== undefined && typeof data.keyboard !== "boolean") return null;
    return { view: data.view, keyboard: data.keyboard === true };
  }

  // focusTarget gives the element that gets the keyboard focus after the
  // shell shows view. It gives "editorLink" for the Edit scenario link, or
  // the name of the frame. The game keeps the Tab key, so a switch to the
  // game with the keyboard does not move the focus into the game. The focus
  // goes to the Edit scenario link, and Tab still moves through the page.
  function focusTarget(view, keyboard) {
    return view === "game" && keyboard ? "editorLink" : view;
  }

  const API = { VIEWS, viewForHash, showRequest, focusTarget };
  if (typeof module !== "undefined" && module.exports) module.exports = API;
  root.PodsimShell = API;
})(typeof window !== "undefined" ? window : globalThis);
