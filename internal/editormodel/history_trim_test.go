package editormodel

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"reflect"
	"slices"
	"strconv"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

func TestHistoryReplacementTrimsOnlyItsCandidate(t *testing.T) {
	t.Parallel()
	model := new(engine)
	synchronize(t, model, project.Default())
	var views []*historyView
	for index, key := range []string{"a", "x", "b"} {
		kind := "replace"
		if index == 0 {
			kind = "reset"
		}
		views = append(views, acceptedHistory(t, model, historyCommand{Kind: kind, Background: jsontext.Value(`{"imageKey":"` + key + `"}`)}))
	}
	before := historyMetadata(model.timeline.state, model.timeline.revision, false)
	trim := uint64(2)
	command := historyCommand{Action: "prepare", Kind: "replace", Revision: before.Revision, Background: jsontext.Value(`{"imageKey":"c"}`), TrimOldest: &trim}
	proposal := historyRequest(t, model, command)
	if !slices.Equal(proposal.Retained, []string{views[2].Head, proposal.Head}) || !slices.Equal(proposal.ImageKeys, []string{"b", "c"}) {
		t.Fatalf("trimmed candidate: %+v", proposal)
	}
	if after := historyMetadata(model.timeline.state, model.timeline.revision, false); !reflect.DeepEqual(before, after) {
		t.Fatal("preparation changed the committed history")
	}
	historyRequest(t, model, historyCommand{Action: "discard", Token: proposal.Proposal})
	if after := historyMetadata(model.timeline.state, model.timeline.revision, false); !reflect.DeepEqual(before, after) {
		t.Fatal("discard changed the committed history")
	}
	proposal = historyRequest(t, model, command)
	historyRequest(t, model, historyCommand{Action: "accept", Token: proposal.Proposal})
	undo := acceptedHistory(t, model, historyCommand{Kind: "undo"})
	if undo.Head != views[2].Head || undo.CanUndo || !undo.CanRedo {
		t.Fatalf("undo retained a trimmed entry: %+v", undo)
	}
}

func TestHistoryTrimRejectsInvalidParameters(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"null", `"1"`, "-1", "1.5", "18446744073709551616", "true", "[]", "{}"} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			if _, err := decodeHistoryCommand(jsontext.Value(`{"action":"prepare","kind":"replace","revision":"1","background":null,"trimOldest":` + value + `}`)); err == nil {
				t.Fatal("accepted an invalid trim count")
			}
		})
	}
	for _, kind := range []string{"reset", "commitFrom", "undo", "redo", "dropOldest", "accept", "discard"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			if _, err := decodeHistoryCommand(jsontext.Value(`{"action":"prepare","kind":"` + kind + `","trimOldest":0}`)); err == nil {
				t.Fatal("accepted trimming for a different action")
			}
		})
	}
}

func TestHistoryRejectedTrimPreservesPendingProposal(t *testing.T) {
	t.Parallel()
	model := new(engine)
	synchronize(t, model, project.Default())
	initial := acceptedHistory(t, model, historyCommand{Kind: "reset", Background: jsontext.Value("null")})
	valid := historyRequest(t, model, historyCommand{Action: "prepare", Kind: "replace", Revision: initial.Revision, Background: jsontext.Value(`{"imageKey":"b"}`)})
	for _, fixture := range []struct {
		name       string
		background string
		trim       uint64
		record     *bool
	}{
		{name: "oversized", background: `{"imageKey":"b"}`, trim: 2},
		{name: "unchanged", background: "null", trim: 1},
		{name: "unrecorded", background: `{"imageKey":"b"}`, record: new(false)},
	} {
		command := historyCommand{Action: "prepare", Kind: "replace", Revision: initial.Revision, Background: jsontext.Value(fixture.background), TrimOldest: &fixture.trim, Record: fixture.record}
		raw, err := json.Marshal(command)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := model.historyOperation(raw); err == nil {
			t.Fatalf("accepted %s trimming", fixture.name)
		}
		if model.timeline.pending.token != valid.Proposal || model.timeline.state.present.id != initial.Head {
			t.Fatalf("rejected %s trimming changed history", fixture.name)
		}
	}
	accepted := historyRequest(t, model, historyCommand{Action: "accept", Token: valid.Proposal})
	if accepted.Head != valid.Head {
		t.Fatal("rejected trimming replaced the valid proposal")
	}
}

// Edits, a trim, and undo and redo keep the version and the Express members
// of the project, and keep their bytes.
func TestHistoryTrimKeepsExpressMembers(t *testing.T) {
	t.Parallel()
	config := expressEditorConfig(t)
	model := new(engine)
	keys := synchronize(t, model, config)
	members := []string{"orderContract", "expressServices"}
	original := make(map[string]jsontext.Value)
	for _, key := range members {
		original[key] = slices.Clone(model.branches[key].raw)
	}
	acceptedHistory(t, model, historyCommand{Kind: "reset", Background: jsontext.Value(`null`)})
	queueKeys := append(slices.Clone(keys), "stationQueueSpacing")
	for _, edit := range []struct {
		keys  []string
		patch string
	}{{keys, `{"name":"Edited Express project"}`}, {keys, `{"sharedRidePartyLimit":2}`}, {queueKeys, `{"stationQueueSpacing":"ordinary"}`}} {
		if _, err := model.sync(request{Keys: edit.keys, Patch: jsontext.Value(edit.patch)}); err != nil {
			t.Fatal(err)
		}
		acceptedHistory(t, model, historyCommand{Kind: "replace", Background: jsontext.Value(`null`)})
	}
	trim := uint64(1)
	view := historyRequest(t, model, historyCommand{Action: "prepare", Kind: "replace", Revision: strconv.FormatUint(model.timeline.revision, 10), Background: jsontext.Value(`{"imageKey":"a"}`), TrimOldest: &trim})
	historyRequest(t, model, historyCommand{Action: "accept", Token: view.Proposal})
	check := func(step string) {
		t.Helper()
		if model.config.Version != project.CurrentVersion || model.config.OrderContract != sim.ExpressOrderContract ||
			!reflect.DeepEqual(model.config.ExpressServices, config.ExpressServices) {
			t.Fatal("history changed the Express project", step)
		}
		for _, key := range members {
			if !bytes.Equal(model.branches[key].raw, original[key]) {
				t.Fatal("history changed Express bytes", step, key)
			}
		}
		if _, err := model.handle(`{"op":"validate"}`); err != nil {
			t.Fatal(step, err)
		}
	}
	check("trim")
	for _, kind := range []string{"undo", "undo", "undo", "redo", "redo", "redo"} {
		acceptedHistory(t, model, historyCommand{Kind: kind})
		check(kind)
	}
}
