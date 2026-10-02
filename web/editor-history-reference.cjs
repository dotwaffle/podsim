"use strict";

// This module keeps the former model for Node parity tests only.
module.exports = (helpers) => {
  const { clone, freezeDraft, ownDraft, editDraft, IMAGE_TABLE_BYTES, MIB, mibText } = helpers;
  // createHistory keeps the draft with undo and redo. onChange runs after
  // each change of the draft, also after an undo, a redo, and a reset. The
  // editor uses it to schedule the checks. Its argument is true only when
  // valid boolean flags change, so existing checks remain valid. A pending
  // check from an earlier edit must still run. background gives a copy of the
  // background of the draft only, with no copy of the scenario. The
  // background of an entry holds an image key, and no image data, so the
  // JSON copy of an entry copies only numbers and strings.
  function createHistory(initial, onChange) {
    let past = [];
    let present = freezeDraft(clone(initial));
    let future = [];
    const changed = (checksUnchanged = false) => { if (onChange) onChange(checksUnchanged); return true; };
    return {
      get value() { return clone(present); },
      get snapshot() { return present; },
      edit(change) {
        const scenario = editDraft(present.scenario, change);
        if (scenario === present.scenario) return false;
        past.push(present); present = freezeDraft({ ...present, scenario }); future = [];
        return changed();
      },
      get scenarioText() { return JSON.stringify(present.scenario); },
      get background() { return present.background ? clone(present.background) : null; },
      get canUndo() { return past.length > 0; },
      get canRedo() { return future.length > 0; },
      // Shared snapshots are deeply frozen. A boolean edit can
      // share the unchanged graph while keeping previous entries intact.
      setOperatingFlag(name, value) {
        if (!["demandEnabled", "redistribution", "stationBuffers", "pickupReassignment"].includes(name) || typeof value !== "boolean") throw new Error("Invalid operating flag.");
        const scenario = present.scenario;
        const enabled = name === "demandEnabled";
        const previous = enabled ? scenario.demand.enabled : scenario[name];
        if (previous === value) return false;
        past.push(present);
        present = freezeDraft({ ...present, scenario: enabled
          ? { ...scenario, demand: { ...scenario.demand, enabled: value } }
          : { ...scenario, [name]: value } });
        future = [];
        // Validation depends on the flag types, not their boolean values.
        return changed(typeof previous === "boolean");
      },
      replace(next, record, checksUnchanged = false) {
        const owned = ownDraft(next, present);
        if (owned === present) return false;
        if (record !== false) past.push(present);
        present = owned;
        future = [];
        return changed(checksUnchanged);
      },
      preview(next) { return this.replace(next, false); },
      commitFrom(before, next) {
        if (JSON.stringify(before) === JSON.stringify(next)) return false;
        past.push(ownDraft(before)); present = ownDraft(next, present); future = []; return changed();
      },
      undo() { if (!past.length) return false; future.push(present); present = past.pop(); return changed(); },
      // keys gives the image keys of the backgrounds of all entries: the
      // undo steps, the draft and the redo steps.
      keys() {
        const keys = new Set();
        for (const entry of [...past, present, ...future]) if (entry.background && entry.background.imageKey) keys.add(entry.background.imageKey);
        return keys;
      },
      // dropOldest removes the oldest undo step. It gives false when there
      // is no undo step. It is not a draft change, so onChange does not run.
      dropOldest() { if (!past.length) return false; past.shift(); return true; },
      redo() { if (!future.length) return false; past.push(present); present = future.pop(); return changed(); },
      reset(next) { past = []; present = freezeDraft(clone(next)); future = []; changed(); },
    };
  }

  // createBackgroundModel keeps the draft history of an editor tab with the
  // images of its backgrounds. options.initial is the first history value
  // and the Reset draft baseline, options.onChange runs after each change
  // of the draft, and options.cap is the byte limit of the image table,
  // IMAGE_TABLE_BYTES when not given.
  //
  // A history entry and the baseline hold the image key of a background,
  // and the table maps each key to its frozen descriptor. A key stays in
  // the table while an entry of the history, the baseline or a pin refers
  // to it. The background keeper pins the key of each queued write, so a
  // write keeps its image until it settles. Each history change and each
  // unpin removes the images with no reference.
  //
  // The model also keeps the edit counter, which each history change and
  // each move of a drag increments, the open drag and opacity gestures,
  // and the ticket of the one open acquisition or import. publish adds an
  // image only after the final check of publishError, in the same task.
  function createBackgroundModel(options) {
    const cap = options.cap ?? IMAGE_TABLE_BYTES;
    const images = new Map(); const pins = new Map();
    let loaded = clone(options.initial);
    // edits counts the draft changes, also the moves of a drag that the
    // history does not have yet. dragChanged tells if an open drag moved.
    let edits = 0; let dragChanged = false;
    // gesture is the open opacity gesture, with entry, the history value
    // at its start, and changed, set at its first opacity step. own is true
    // while the gesture changes the history.
    let gesture = null; let own = false;
    // ticket is the open acquisition or import, or null.
    let ticket = null;
    const history = createHistory(options.initial, (checksUnchanged) => {
      edits += 1;
      // A history change that the gesture did not make starts the gesture
      // again from the new value, so its pointer up cannot record an old
      // entry.
      if (gesture && !own) gesture = { entry: history.value, changed: false };
      prune();
      if (options.onChange) options.onChange(checksUnchanged);
    });
    const keyOf = (background) => (background && background.imageKey) || "";
    const sizeOf = (keys) => { let total = 0; for (const key of keys) { const image = images.get(key); if (image) total += image.bytes.byteLength; } return total; };
    // pinnedKeys gives the keys that the table must keep for a publish: the
    // present image, the baseline image and the pinned images.
    const pinnedKeys = () => new Set([...pins.keys(), keyOf(history.background), keyOf(loaded.background)].filter(Boolean));
    function prune() {
      const keys = history.keys();
      for (const key of pinnedKeys()) keys.add(key);
      for (const key of [...images.keys()]) if (!keys.has(key)) images.delete(key);
    }
    // bound drops the oldest undo steps until the table holds at most cap
    // bytes. It never drops the present image, the baseline image or a
    // pinned image. It gives the number of dropped steps.
    function bound() {
      let dropped = 0;
      while (sizeOf(images.keys()) > cap && history.dropOldest()) { dropped += 1; prune(); }
      return dropped;
    }
    // publishError gives the reason why a publish of image, or of no
    // image, for the ticket request cannot go on, or an empty text.
    // abort aborts the open acquisition or import.
    function abort() { if (ticket) { const { controller } = ticket; ticket = null; controller.abort(); } }
    function publishError(request, image) {
      if (!request || request !== ticket || request.signal.aborted) return "A newer action stopped the import.";
      if (edits !== request.edits) return "The draft changed during the import.";
      if (dragChanged || (gesture && gesture.changed)) return "A drag or an opacity change was open at the end of the import. Finish it and import again.";
      if (image) {
        const keys = pinnedKeys(); const held = sizeOf(keys);
        if (held + image.bytes.byteLength > cap) return `The image does not fit in the ${mibText(cap)} MiB image limit of this tab. The images that the tab must keep use ${mibText(held)} MiB, with ${mibText(sizeOf(pins.keys()))} MiB for writes to the browser store.`;
      }
      return "";
    }
    return {
      history,
      cap,
      get loaded() { return clone(loaded); },
      // setLoaded makes value the Reset draft baseline.
      setLoaded(value) { loaded = clone(value); prune(); },
      image(key) { return images.get(key) || null; },
      keys() { return [...images.keys()]; },
      get bytes() { return sizeOf(images.keys()); },
      get keeperBytes() { return sizeOf(pins.keys()); },
      pinCount(key) { return pins.get(key) || 0; },
      pin(key) { pins.set(key, (pins.get(key) || 0) + 1); },
      unpin(key) {
        const count = (pins.get(key) || 0) - 1;
        if (count > 0) pins.set(key, count); else pins.delete(key);
        prune();
      },
      get edits() { return edits; },
      // moveDrag tells the model that an open drag changed its working
      // copy, and endDrag that the drag ended.
      moveDrag() { edits += 1; dragChanged = true; },
      endDrag() { dragChanged = false; },
      get gestureOpen() { return Boolean(gesture); },
      get gestureChanged() { return Boolean(gesture && gesture.changed); },
      // pressOpacity starts an opacity gesture at the present value.
      pressOpacity() { gesture = { entry: history.value, changed: false }; },
      // setOpacity sets the opacity of the present background with no undo
      // step. It gives false when there is no background or no change.
      setOpacity(opacity) {
        const background = history.background;
        if (!background) return false;
        background.opacity = opacity;
        own = true;
        let changed = false;
        try { changed = history.replace({ scenario: history.snapshot.scenario, background }, false); } finally { own = false; }
        if (changed && gesture) gesture.changed = true;
        return changed;
      },
      // endOpacity ends the opacity gesture at a pointer up, a
      // pointercancel or a lost pointer capture. A change since the start
      // of the gesture becomes one undo step. With no open gesture it does
      // nothing, so a second end event changes nothing.
      endOpacity() {
        if (!gesture) return false;
        const { entry } = gesture; gesture = null;
        return history.commitFrom(entry, history.value);
      },
      // takeOpacity ends a preview and returns its entry for a Go commit.
      takeOpacity() { const ended = gesture; gesture = null; return ended; },
      // start opens a new acquisition or import, and aborts the open one.
      // The ticket has signal, an AbortSignal, and the edit counter.
      start() {
        abort();
        const controller = new AbortController();
        ticket = { signal: controller.signal, controller, edits };
        return ticket;
      },
      abort,
      current(request) { return request === ticket && !request.signal.aborted; },
      publishError,
      // publish runs the final check of publishError, and then, with no
      // wait, adds image to the table and makes value the draft in one
      // history step. With baseline set, value also becomes the Reset draft
      // baseline. Then it drops the oldest undo steps past the cap. It
      // gives error, the reason of a failed check, and dropped, the number
      // of dropped undo steps. The ticket closes in both cases.
      publish(request) {
        const error = publishError(request.ticket, request.image);
        if (request.ticket === ticket) ticket = null;
        if (error) return { error, dropped: 0 };
        if (request.image) images.set(request.image.key, request.image);
        if (request.baseline) loaded = clone(request.value);
        history.replace(request.value);
        prune();
        return { error: "", dropped: bound() };
      },
      // restore installs the image and the background of the stored
      // record, with no undo step, and makes the background part of the
      // baseline.
      restore(image, background) {
        images.set(image.key, image);
        loaded = { scenario: loaded.scenario, background: clone(background) };
        history.reset({ scenario: history.value.scenario, background });
      },
    };
  }

  return { createHistory, createBackgroundModel };
};
