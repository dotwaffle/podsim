package editormodel

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"maps"
	"os"
	"reflect"
	"slices"
	"strconv"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
)

func historyRequest(t *testing.T, model *engine, command historyCommand) *historyView {
	t.Helper()
	raw, err := json.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	result, err := model.historyOperation(raw)
	if err != nil {
		t.Fatal(err)
	}
	return result.History
}

func historyTarget(t *testing.T, model *engine, target any) jsontext.Value {
	t.Helper()
	var value struct {
		Scenario   map[string]jsontext.Value `json:"scenario"`
		Background jsontext.Value            `json:"background"`
	}
	encoded, err := json.Marshal(target)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(encoded, &value); err != nil {
		t.Fatal(err)
	}
	patch, err := json.Marshal(value.Scenario)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = model.sync(request{Keys: slices.Sorted(maps.Keys(value.Scenario)), Patch: patch})
	if !model.ready {
		t.Fatal("history source did not synchronize")
	}
	return value.Background
}

func acceptedHistory(t *testing.T, model *engine, command historyCommand) *historyView {
	t.Helper()
	command.Action = "prepare"
	if model.timeline == nil {
		command.Revision = "0"
	} else {
		command.Revision = strconv.FormatUint(model.timeline.revision, 10)
	}
	prepared := historyRequest(t, model, command)
	accepted := historyRequest(t, model, historyCommand{Action: "accept", Token: prepared.Proposal})
	prepared.Proposal = ""
	if !reflect.DeepEqual(prepared, accepted) {
		t.Fatal("accepted metadata differs from the proposal")
	}
	return accepted
}

func historyValue(t *testing.T, model *engine, background jsontext.Value) map[string]any {
	t.Helper()
	branches := make(map[string]jsontext.Value, len(model.branches))
	for key, branch := range model.branches {
		branches[key] = branch.raw
	}
	encoded, err := json.Marshal(map[string]any{"scenario": branches, "background": background})
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(encoded, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

//nolint:tparallel // Trace steps share one timeline and must run in order.
func TestHistoryMatchesExistingTrace(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/history_trace.json")
	if err != nil {
		t.Fatal(err)
	}
	var events []struct {
		Kind      string   `json:"kind"`
		Target    any      `json:"target"`
		Record    *bool    `json:"record"`
		Changed   bool     `json:"changed"`
		Value     any      `json:"value"`
		CanUndo   bool     `json:"canUndo"`
		CanRedo   bool     `json:"canRedo"`
		ImageKeys []string `json:"imageKeys"`
	}
	if err := json.Unmarshal(data, &events); err != nil {
		t.Fatal(err)
	}
	model := new(engine)
	for index, event := range events {
		ok := t.Run(strconv.Itoa(index)+"-"+event.Kind, func(t *testing.T) {
			command := historyCommand{Kind: event.Kind, Record: event.Record}
			if event.Target != nil {
				command.Background = historyTarget(t, model, event.Target)
			}
			if event.Kind == "commitFrom" {
				command.Before = model.timeline.state.present.id
			}
			view := acceptedHistory(t, model, command)
			if view.Changed != event.Changed || view.CanUndo != event.CanUndo || view.CanRedo != event.CanRedo || !slices.Equal(slices.Sorted(slices.Values(view.ImageKeys)), event.ImageKeys) {
				t.Fatalf("metadata differs: %+v, expected %+v", view, event)
			}
			if !sameGeometry(historyValue(t, model, view.Background), event.Value) {
				t.Fatal("restored project/background differs from the reference")
			}
			// Modifying a reply must not change saved background metadata.
			if len(view.Background) > 0 {
				view.Background[0] = 'x'
			}
		})
		if !ok {
			t.FailNow()
		}
	}
}

func TestHistoryProposalsAreDiscardableAndRejectStaleTokens(t *testing.T) {
	t.Parallel()
	model := new(engine)
	synchronize(t, model, project.Default())
	initial := acceptedHistory(t, model, historyCommand{Kind: "reset", Background: jsontext.Value("null")})
	keys := slices.Sorted(maps.Keys(model.branches))
	if _, err := model.sync(request{Keys: keys, Patch: jsontext.Value(`{"name":"candidate"}`)}); err != nil {
		t.Fatal(err)
	}
	first := historyRequest(t, model, historyCommand{Action: "prepare", Kind: "replace", Revision: initial.Revision, Background: jsontext.Value("null")})
	if model.timeline.state.present.id != initial.Head {
		t.Fatal("preparation changed the committed head")
	}
	second := historyRequest(t, model, historyCommand{Action: "prepare", Kind: "replace", Revision: initial.Revision, Background: jsontext.Value("null")})
	for _, raw := range []string{`{"action":"accept","token":"` + first.Proposal + `"}`, `{"action":"prepare","kind":"unknown","revision":"` + initial.Revision + `"}`, `{"action":"prepare","kind":"replace","revision":"0","background":null}`} {
		if _, err := model.historyOperation(jsontext.Value(raw)); err == nil {
			t.Fatal("accepted stale/invalid history command", raw)
		}
	}
	if model.timeline.pending.token != second.Proposal {
		t.Fatal("a failed operation replaced the pending proposal")
	}
	historyRequest(t, model, historyCommand{Action: "discard", Token: second.Proposal})
	if model.timeline.pending != nil || model.timeline.state.present.id != initial.Head {
		t.Fatal("discard changed committed history")
	}
	if _, err := model.historyOperation(jsontext.Value(`{"action":"accept","token":"` + second.Proposal + `"}`)); err == nil {
		t.Fatal("accepted a discarded proposal")
	}
}

func TestHistorySharesBranchesAndRestoresInvalidBarriers(t *testing.T) {
	t.Parallel()
	model := new(engine)
	config := project.Default()
	profile, demand, err := makeParkRide(config, validPlan())
	if err != nil {
		t.Fatal(err)
	}
	config.DemandProfiles, config.Demand = []project.DemandProfile{profile}, demand
	keys := synchronize(t, model, config)
	initial := acceptedHistory(t, model, historyCommand{Kind: "reset", Background: jsontext.Value("null")})
	original := model.timeline.state.present
	if _, err := model.sync(request{Keys: keys, Patch: jsontext.Value(`{"name":false}`)}); err == nil {
		t.Fatal("invalid typed name did not set its barrier")
	}
	invalid := acceptedHistory(t, model, historyCommand{Kind: "replace", Background: jsontext.Value("null")})
	if model.timeline.state.present.project.branches["demandProfiles"].value != nil {
		t.Fatal("history decoded generic full profile flows")
	}
	if &original.project.config.Network.Nodes[0] != &model.timeline.state.present.project.config.Network.Nodes[0] || &original.project.config.DemandProfiles[0] != &model.timeline.state.present.project.config.DemandProfiles[0] {
		t.Fatal("unchanged typed branches were copied")
	}
	if original.project.config.Name != config.Name || model.err == nil {
		t.Fatal("a later edit changed the saved source/barrier")
	}
	if _, err := model.handle(`{"op":"validate"}`); err == nil {
		t.Fatal("history cleared the typed operation barrier")
	}
	undo := acceptedHistory(t, model, historyCommand{Kind: "undo"})
	if undo.Head != initial.Head || model.err != nil {
		t.Fatal("undo did not restore the valid snapshot")
	}
	redo := acceptedHistory(t, model, historyCommand{Kind: "redo"})
	if redo.Head != invalid.Head || model.err == nil {
		t.Fatal("redo did not restore the invalid barrier")
	}
	if !bytes.Equal(model.branches["demandProfiles"].raw, original.project.branches["demandProfiles"].raw) {
		t.Fatal("profile data changed during history navigation")
	}
}

func TestHistoryRejectsUnrelatedAndMalformedRequests(t *testing.T) {
	t.Parallel()
	call := NewCall()
	for _, input := range []string{
		`{"op":"history","history":null}`, `{"op":"history","history":[]}`,
		`{"op":"history","project":{},"history":{"action":"prepare"}}`,
		`{"op":"sync","keys":[],"patch":{},"history":{"action":"prepare"}}`,
		`{"op":"checks","history":{"action":"prepare"}}`,
		`{"op":"history","history":{"action":"accept","token":"p1","background":null}}`,
	} {
		var result response
		if err := json.Unmarshal([]byte(call(input)), &result); err != nil || result.Error == "" {
			t.Fatal("accepted malformed/unrelated request", input, err)
		}
	}
	var result response
	if err := json.Unmarshal([]byte(Call(`{"op":"history","history":{"action":"prepare"}}`)), &result); err != nil || result.Error == "" {
		t.Fatal("stateless call accepted history", err)
	}
}

func TestHistoryStrictParametersAndGestureOwnership(t *testing.T) {
	t.Parallel()
	model := new(engine)
	synchronize(t, model, project.Default())
	initial := acceptedHistory(t, model, historyCommand{Kind: "reset", Background: jsontext.Value("null")})
	for _, raw := range []string{
		`{"action":"prepare","kind":"replace","revision":"1","background":null,"record":null}`,
		`{"action":"prepare","kind":"undo","revision":"1","background":null}`,
		`{"action":"prepare","kind":"undo","revision":"1","before":""}`,
		`{"action":"prepare","kind":"replace","revision":"1","background":null,"token":""}`,
		`{"action":"prepare","kind":"reset","revision":"1","background":null,"record":true}`,
		`{"action":"prepare","kind":"replace","revision":"1","background":[]}`,
		`{"action":"prepare","kind":"commitFrom","revision":"1","background":null}`,
		`{"action":"prepare","kind":"commitFrom","revision":"1","before":"missing","background":null}`,
	} {
		if _, err := model.historyOperation(jsontext.Value(raw)); err == nil {
			t.Fatal("accepted inappropriate history fields", raw)
		}
		if model.timeline.state.present.id != initial.Head || model.timeline.pending != nil {
			t.Fatal("a rejected command changed history")
		}
	}
	prepared := historyRequest(t, model, historyCommand{Action: "prepare", Kind: "replace", Revision: "1", Background: jsontext.Value("null")})
	for _, raw := range []string{
		`{"action":"accept","token":"` + prepared.Proposal + `","kind":""}`,
		`{"action":"discard","token":"` + prepared.Proposal + `","record":null}`,
	} {
		if _, err := model.historyOperation(jsontext.Value(raw)); err == nil {
			t.Fatal("accepted inappropriate completion fields", raw)
		}
	}
	historyRequest(t, model, historyCommand{Action: "accept", Token: prepared.Proposal})
}

func TestHistoryComparisonPreservesIntegerPrecision(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		a, b  string
		equal bool
	}{
		{`{"a":1,"b":2}`, `{"b":2,"a":1}`, true},
		{`{"a":9007199254740992}`, `{"a":9007199254740993}`, false},
		{`{"a":null}`, `{}`, false},
	} {
		if sameHistoryJSON(jsontext.Value(test.a), jsontext.Value(test.b)) != test.equal {
			t.Fatal("incorrect history equivalence", test)
		}
	}
}

func TestHistoryTokensUseExactStringsBeyondBrowserPrecision(t *testing.T) {
	t.Parallel()
	model := new(engine)
	synchronize(t, model, project.Default())
	model.timeline = &historyTimeline{revision: 9007199254740993, nextID: 9007199254740993, nextPlan: 9007199254740993}
	view := acceptedHistory(t, model, historyCommand{Kind: "reset", Background: jsontext.Value("null")})
	if view.Revision != "9007199254740994" || view.Head != "s9007199254740994" {
		t.Fatal("history tokens lost precision", view)
	}
}

func TestHistoryNavigationReleasesDiscardedSnapshots(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"undo", "redo"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			model := new(engine)
			keys := synchronize(t, model, project.Default())
			acceptedHistory(t, model, historyCommand{Kind: "reset", Background: jsontext.Value("null")})
			for _, name := range []string{"first", "second", "third", "fourth"} {
				patch, err := json.Marshal(map[string]string{"name": name})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := model.sync(request{Keys: keys, Patch: patch}); err != nil {
					t.Fatal(err)
				}
				acceptedHistory(t, model, historyCommand{Kind: "replace", Background: jsontext.Value("null")})
			}
			if kind == "redo" {
				for range 3 {
					acceptedHistory(t, model, historyCommand{Kind: "undo"})
				}
			}
			for range 2 {
				before := model.timeline.state
				before.past, before.future = slices.Clone(before.past), slices.Clone(before.future)
				prepared := historyRequest(t, model, historyCommand{Action: "prepare", Kind: kind, Revision: strconv.FormatUint(model.timeline.revision, 10)})
				committed := model.timeline.state
				if before.present != committed.present || !slices.Equal(before.past, committed.past) || !slices.Equal(before.future, committed.future) {
					t.Fatal("preparation modified committed stack storage")
				}
				historyRequest(t, model, historyCommand{Action: "accept", Token: prepared.Proposal})
			}
			if kind == "undo" {
				if _, err := model.sync(request{Keys: keys, Patch: jsontext.Value(`{"name":"unrecorded"}`)}); err != nil {
					t.Fatal(err)
				}
				record := false
				acceptedHistory(t, model, historyCommand{Kind: "replace", Background: jsontext.Value("null"), Record: &record})
				if len(model.timeline.state.future) != 0 {
					t.Fatal("unrecorded replacement retained redo")
				}
			}
			for _, stack := range [][]*historyEntry{model.timeline.state.past, model.timeline.state.future} {
				for _, entry := range stack[len(stack):cap(stack)] {
					if entry != nil {
						t.Fatal("a popped snapshot remains in stack backing storage", entry.id)
					}
				}
			}
		})
	}
}

func TestHistoryRebuildsGenericNetworkForEditsAndChecks(t *testing.T) {
	t.Parallel()
	for _, op := range []string{"edit", "checks"} {
		t.Run(op, func(t *testing.T) {
			t.Parallel()
			model := new(engine)
			keys := synchronize(t, model, project.Default())
			acceptedHistory(t, model, historyCommand{Kind: "reset", Background: jsontext.Value("null")})
			before := model.timeline.state.present
			if before.project.branches["network"].value != nil {
				t.Fatal("history retains generic network objects")
			}
			network := model.config.Network
			network.Nodes = slices.Clone(network.Nodes)
			network.Nodes[0].Position.X += 1
			patch, err := json.Marshal(map[string]any{"network": network})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = model.sync(request{Keys: keys, Patch: patch}); err != nil {
				t.Fatal(err)
			}
			acceptedHistory(t, model, historyCommand{Kind: "replace", Background: jsontext.Value("null")})
			acceptedHistory(t, model, historyCommand{Kind: "undo"})
			if !model.branches["network"].needsValue {
				t.Fatal("undo did not release the old generic network")
			}
			switch op {
			case "edit":
				result, err := model.edit(jsontext.Value(`{"field":"geometry","value":{"action":"moveNode","id":"market-entry","point":{"X":1,"Y":1}}}`))
				if err != nil || result.Change == nil {
					t.Fatal("restored geometry cannot be edited", err)
				}
			case "checks":
				actual, err := model.draftChecks()
				if err != nil {
					t.Fatal(err)
				}
				var draft any
				raw, err := json.Marshal(project.Default())
				if err != nil {
					t.Fatal(err)
				}
				if err = json.Unmarshal(raw, &draft); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(actual, draftChecks(draft)) {
					t.Fatal("restored checks differ from stateless checks")
				}
			}
			if model.branches["network"].needsValue || model.branches["network"].value == nil || before.project.branches["network"].value != nil {
				t.Fatal("lazy reconstruction changed the saved entry")
			}
		})
	}
}
