(function (root) {
  "use strict";

  // The debug capture in shell.js and the editor in editor.js read the same
  // state reply, so they share these checks.

  // MAX_STATE_DEPTH and MAX_STATE_ELEMENTS are the depth limit and the
  // element limit of each array of a stream document of the server. See
  // streamLimits in internal/session. A Go test checks the mirror.
  const MAX_STATE_DEPTH = 64;
  const MAX_STATE_ELEMENTS = 65536;

  // isObject is true for a JSON object that is not an array.
  function isObject(value) { return value !== null && typeof value === "object" && !Array.isArray(value); }

  // CONTRACT_MARKERS gives each contract marker of a state reply and the
  // one value that the server sends.
  const CONTRACT_MARKERS = [["orderContract", "express-v1"]];

  // REMOVED_MEMBERS are members that earlier servers sent and the server
  // no longer writes: textEncoding, the couplingContract marker of the
  // removed physical coupling feature, and the removed incident, fault, and
  // emergency markers. The native decoders refuse them as unknown members.
  const REMOVED_MEMBERS = ["textEncoding", "couplingContract", "incidentContract", "faultContract", "emergencyContract"];

  // plainStateTree is true when value has at most MAX_STATE_DEPTH levels
  // of arrays and objects, no array with more than MAX_STATE_ELEMENTS
  // elements, and no object with a member in REMOVED_MEMBERS. The presence
  // of such a member refuses the reply, whatever its value.
  // The walk uses a stack, so a deep reply cannot overflow the call stack.
  function plainStateTree(value) {
    const stack = [[value, 1]];
    while (stack.length > 0) {
      const [node, depth] = stack.pop();
      if (node === null || typeof node !== "object") continue;
      if (depth > MAX_STATE_DEPTH) return false;
      if (Array.isArray(node) ? node.length > MAX_STATE_ELEMENTS : REMOVED_MEMBERS.some((name) => Object.hasOwn(node, name))) return false;
      for (const child of Object.values(node)) stack.push([child, depth + 1]);
    }
    return true;
  }

  // markersAgree is true when the reply has a topology object, and the
  // reply, its topology and the simulation of state have the same contract
  // markers. Each marker that is present must have the value in
  // CONTRACT_MARKERS. The server decoder also refuses a reply without a
  // topology object.
  function markersAgree(reply, state) {
    if (!isObject(reply.topology)) return false;
    const holders = [reply, reply.topology, state.simulation];
    return CONTRACT_MARKERS.every(([name, allowed]) => {
      const values = holders.map((holder) => Object.hasOwn(holder, name) ? holder[name] : undefined);
      return values.every((marker) => marker === values[0] && (marker === undefined || marker === allowed));
    });
  }

  const api = { MAX_STATE_DEPTH, MAX_STATE_ELEMENTS, isObject, plainStateTree, markersAgree };
  if (typeof module !== "undefined" && module.exports) module.exports = api;
  root.PodsimStateCheck = api;
})(typeof window !== "undefined" ? window : globalThis);
