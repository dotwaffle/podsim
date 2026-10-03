package editormodel

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"

	"github.com/dotwaffle/podsim/internal/project"
)

type historyCommand struct {
	Action     string         `json:"action"`
	Kind       string         `json:"kind,omitempty"`
	Revision   string         `json:"revision,omitempty"`
	Token      string         `json:"token,omitempty"`
	Before     string         `json:"before,omitempty"`
	Record     *bool          `json:"record,omitempty"`
	TrimOldest *uint64        `json:"trimOldest,omitempty"`
	Background jsontext.Value `json:"background,omitzero"`
}

type historySnapshot struct {
	branches map[string]projectBranch
	config   project.Config
	err      error
	size     int
}

type historyEntry struct {
	id         string
	project    historySnapshot
	background jsontext.Value
	imageKey   string
}

type historyState struct {
	past    []*historyEntry
	present *historyEntry
	future  []*historyEntry
}

type historyProposal struct {
	state   historyState
	token   string
	changed bool
}

type historyTimeline struct {
	state    historyState
	pending  *historyProposal
	revision uint64
	nextID   uint64
	nextPlan uint64
}

type historyView struct {
	Head       string         `json:"head"`
	Revision   string         `json:"revision"`
	Proposal   string         `json:"proposal,omitempty"`
	Changed    bool           `json:"changed"`
	CanUndo    bool           `json:"canUndo"`
	CanRedo    bool           `json:"canRedo"`
	Retained   []string       `json:"retained"`
	ImageKeys  []string       `json:"imageKeys"`
	Background jsontext.Value `json:"background"`
}

func (e *engine) historyOperation(raw jsontext.Value) (response, error) {
	command, err := decodeHistoryCommand(raw)
	if err != nil {
		return response{}, err
	}
	if e.timeline == nil {
		e.timeline = new(historyTimeline)
	}
	h := e.timeline
	switch command.Action {
	case "prepare":
		if command.Token != "" {
			return response{}, errors.New("history preparation does not accept a proposal token")
		}
		view, err := h.prepare(e, command)
		return response{History: view}, err
	case "accept", "discard":
		if command.Kind != "" || command.Revision != "" || command.Before != "" || command.Record != nil || len(command.Background) != 0 {
			return response{}, errors.New("history completion accepts only a proposal token")
		}
		if command.Token == "" || h.pending == nil || command.Token != h.pending.token {
			return response{}, errors.New("the history proposal is no longer current")
		}
		if command.Action == "discard" {
			h.pending = nil
			return response{History: historyMetadata(h.state, h.revision, false)}, nil
		}
		if h.revision == ^uint64(0) {
			return response{}, errors.New("the history revision counter is exhausted")
		}
		proposal := h.pending
		h.state, h.pending, h.revision = proposal.state, nil, h.revision+1
		e.restoreHistory(h.state.present.project)
		return response{History: historyMetadata(h.state, h.revision, proposal.changed)}, nil
	default:
		return response{}, errors.New("unknown history action")
	}
}

func decodeHistoryCommand(raw jsontext.Value) (historyCommand, error) {
	var command historyCommand
	if raw.Kind() != '{' || json.Unmarshal(raw, &command, json.RejectUnknownMembers(true)) != nil {
		return command, errors.New("history needs a valid command object")
	}
	var fields map[string]jsontext.Value
	if err := json.Unmarshal(raw, &fields); err != nil {
		return command, err
	}
	allowed := map[string]bool{"action": true}
	switch command.Action {
	case "accept", "discard":
		allowed["token"] = true
	case "prepare":
		allowed["kind"], allowed["revision"] = true, true
		switch command.Kind {
		case "reset", "replace", "commitFrom":
			allowed["background"] = true
		}
		if command.Kind == "replace" {
			allowed["record"] = true
			allowed["trimOldest"] = true
			if value, exists := fields["trimOldest"]; exists && value.Kind() != '0' {
				return command, errors.New("the history trim count must be an unsigned integer")
			}
			if value, exists := fields["record"]; exists && value.Kind() != 't' && value.Kind() != 'f' {
				return command, errors.New("the history recording flag must be boolean")
			}
		}
		if command.Kind == "commitFrom" {
			allowed["before"] = true
		}
	}
	for key := range fields {
		if !allowed[key] {
			return command, fmt.Errorf("the history action does not accept %s", key)
		}
	}
	return command, nil
}

func (h *historyTimeline) prepare(e *engine, command historyCommand) (*historyView, error) {
	if command.Revision != strconv.FormatUint(h.revision, 10) {
		return nil, errors.New("the history revision is no longer current")
	}
	if h.nextPlan == ^uint64(0) || h.nextID == ^uint64(0) || h.revision == ^uint64(0) {
		return nil, errors.New("the history counter is exhausted")
	}
	if err := checkHistoryParameters(command); err != nil {
		return nil, err
	}
	if command.Kind != "reset" && h.state.present == nil {
		return nil, errors.New("the editor history is not initialized")
	}
	next, changed, err := h.transition(e, command)
	if err != nil {
		return nil, err
	}
	h.nextPlan++
	token := "p" + strconv.FormatUint(h.nextPlan, 10)
	h.pending = &historyProposal{state: next, token: token, changed: changed}
	view := historyMetadata(next, h.revision+1, changed)
	view.Proposal = token
	return view, nil
}

func checkHistoryParameters(command historyCommand) error {
	creates := command.Kind == "reset" || command.Kind == "replace" || command.Kind == "commitFrom"
	if creates {
		if command.Background.Kind() != '{' && command.Background.Kind() != 'n' {
			return errors.New("history replacement needs a background object or null")
		}
	} else if len(command.Background) != 0 {
		return errors.New("the history action does not accept a background")
	}
	if command.Kind != "replace" && command.Record != nil {
		return errors.New("the history action does not accept a recording flag")
	}
	if command.TrimOldest != nil && (command.Kind != "replace" || command.Record != nil && !*command.Record) {
		return errors.New("history trimming needs a recorded replacement")
	}
	if command.Kind == "commitFrom" {
		if command.Before == "" {
			return errors.New("a history gesture needs its starting snapshot")
		}
	} else if command.Before != "" {
		return errors.New("the history action does not accept a starting snapshot")
	}
	return nil
}

func (h *historyTimeline) transition(e *engine, command historyCommand) (historyState, bool, error) {
	next := h.state
	switch command.Kind {
	case "reset", "replace", "commitFrom":
		if !e.ready {
			return historyState{}, false, errors.New("the editor project is not synchronized")
		}
		if command.Kind == "commitFrom" && command.Before != next.present.id {
			return historyState{}, false, errors.New("the history gesture has a stale starting snapshot")
		}
		entry, err := h.capture(e, command.Background)
		if err != nil {
			return historyState{}, false, err
		}
		if command.Kind == "reset" {
			return historyState{present: entry}, true, nil
		}
		if sameHistoryEntry(next.present, entry) {
			if command.TrimOldest != nil && *command.TrimOldest > 0 {
				return historyState{}, false, errors.New("an unchanged history replacement cannot trim entries")
			}
			return next, false, nil
		}
		if command.Record == nil || *command.Record {
			next.past = append(slices.Clone(next.past), next.present)
		}
		next.present, next.future = entry, nil
		if command.TrimOldest != nil {
			if *command.TrimOldest > uint64(len(next.past)) {
				return historyState{}, false, errors.New("the history trim count exceeds the candidate past stack")
			}
			next.past = slices.Clone(next.past[*command.TrimOldest:])
		}
	case "undo":
		if len(next.past) == 0 {
			return next, false, nil
		}
		next.future = append(slices.Clone(next.future), next.present)
		next.present, next.past = next.past[len(next.past)-1], slices.Clone(next.past[:len(next.past)-1])
	case "redo":
		if len(next.future) == 0 {
			return next, false, nil
		}
		next.past = append(slices.Clone(next.past), next.present)
		next.present, next.future = next.future[len(next.future)-1], slices.Clone(next.future[:len(next.future)-1])
	case "dropOldest":
		if len(next.past) == 0 {
			return next, false, nil
		}
		// Copy the retained entries so the dropped pointer can be released.
		next.past = slices.Clone(next.past[1:])
	default:
		return historyState{}, false, errors.New("unknown history transition")
	}
	return next, true, nil
}

func (h *historyTimeline) capture(e *engine, background jsontext.Value) (*historyEntry, error) {
	var image struct {
		ImageKey string `json:"imageKey"`
	}
	if background.Kind() == '{' {
		if err := json.Unmarshal(background, &image); err != nil {
			return nil, fmt.Errorf("decode history background: %w", err)
		}
	}
	h.nextID++
	branches := maps.Clone(e.branches)
	if network, exists := branches["network"]; exists {
		// Save raw and typed geometry. Rebuild generic edit/check values on use.
		network.value, network.needsValue = nil, true
		branches["network"] = network
	}
	return &historyEntry{
		id:         "s" + strconv.FormatUint(h.nextID, 10),
		project:    historySnapshot{branches: branches, config: e.config, err: e.err, size: e.size},
		background: slices.Clone(background), imageKey: image.ImageKey,
	}, nil
}

func sameHistoryEntry(a, b *historyEntry) bool {
	if len(a.project.branches) != len(b.project.branches) || !sameHistoryJSON(a.background, b.background) {
		return false
	}
	for key, branch := range a.project.branches {
		other, exists := b.project.branches[key]
		if !exists || !sameHistoryJSON(branch.raw, other.raw) {
			return false
		}
	}
	return true
}

func sameHistoryJSON(a, b jsontext.Value) bool {
	if bytes.Equal(a, b) {
		return true
	}
	// Compare reordered objects without decoding full profile flow maps.
	left, right := slices.Clone(a), slices.Clone(b)
	return left.Canonicalize(jsontext.CanonicalizeRawInts(false)) == nil && right.Canonicalize(jsontext.CanonicalizeRawInts(false)) == nil && bytes.Equal(left, right)
}

func historyMetadata(state historyState, revision uint64, changed bool) *historyView {
	view := &historyView{Revision: strconv.FormatUint(revision, 10), Changed: changed, CanUndo: len(state.past) > 0, CanRedo: len(state.future) > 0, Background: jsontext.Value("null")}
	seen, imageKeys := make(map[string]bool), make(map[string]bool)
	retain := func(entry *historyEntry) {
		if entry == nil || seen[entry.id] {
			return
		}
		seen[entry.id] = true
		view.Retained = append(view.Retained, entry.id)
		if entry.imageKey != "" && !imageKeys[entry.imageKey] {
			imageKeys[entry.imageKey] = true
			view.ImageKeys = append(view.ImageKeys, entry.imageKey)
		}
	}
	for _, entry := range state.past {
		retain(entry)
	}
	retain(state.present)
	for _, entry := range state.future {
		retain(entry)
	}
	if state.present != nil {
		view.Head, view.Background = state.present.id, slices.Clone(state.present.background)
	}
	return view
}

func (e *engine) restoreHistory(state historySnapshot) {
	if !bytes.Equal(e.branches["network"].raw, state.branches["network"].raw) {
		e.checks = nil
	}
	if !bytes.Equal(e.branches["demandProfiles"].raw, state.branches["demandProfiles"].raw) {
		e.profiles = nil
		if e.checks != nil {
			e.checks.profiles, e.checks.profilesReady = nil, false
		}
	}
	if e.checks != nil {
		for _, key := range []string{"version", "orderContract", "fleet", "expressServices", "stationQueueSpacing", "onboardPickups", "sharedRidePartyLimit", "sharedRideMode"} {
			if !bytes.Equal(e.branches[key].raw, state.branches[key].raw) {
				e.checks.servicesReady = false
				break
			}
		}
	}
	branches := maps.Clone(state.branches)
	if network := branches["network"]; network.needsValue {
		previous := e.branches["network"]
		if !previous.needsValue && bytes.Equal(previous.raw, network.raw) {
			network.value, network.needsValue = previous.value, false
			branches["network"] = network
		}
	}
	e.branches, e.config, e.err, e.size, e.ready = branches, state.config, state.err, state.size, true
}

func (e *engine) ensureNetworkValue() error {
	network := e.branches["network"]
	if !network.needsValue {
		return nil
	}
	if err := json.Unmarshal(network.raw, &network.value); err != nil {
		return fmt.Errorf("decode restored editor network: %w", err)
	}
	network.needsValue = false
	e.branches["network"] = network
	return nil
}
