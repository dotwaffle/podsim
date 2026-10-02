package editormodel

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"reflect"
	"slices"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
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
