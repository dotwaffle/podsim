package editormodel

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

func onboardEditorConfig() project.Config {
	config := project.Default()
	config.Version = project.ServiceVersion
	config.SharedRidePartyLimit = 2
	config.SharedRideMode = sim.SharedRideDropOffs
	config.OnboardPickups = true
	return config
}

func onboardEditorDraft(t *testing.T) map[string]any {
	t.Helper()
	raw, err := json.Marshal(onboardEditorConfig())
	if err != nil {
		t.Fatal(err)
	}
	var draft map[string]any
	if decodeErr := json.Unmarshal(raw, &draft); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	return draft
}

func syncOnboardDraft(t *testing.T, model *engine, draft map[string]any) error {
	t.Helper()
	raw, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	_, err = model.sync(request{Keys: slices.Sorted(maps.Keys(draft)), Patch: raw})
	return err
}

func TestOnboardEditorPreservation(t *testing.T) {
	t.Parallel()
	draft := onboardEditorDraft(t)
	before := cloneEditValue(draft)
	for _, repair := range []func(any) (projectChange, error){importCompatibility, normalizeProject} {
		change, err := repair(draft)
		if err != nil {
			t.Fatal(err)
		}
		out := object(cloneEditValue(draft))
		maps.Copy(out, change.Patch)
		if out["onboardPickups"] != true || number(out["version"]) != 3 || !reflect.DeepEqual(draft, before) {
			t.Fatal("repair changed the opt-in or its source")
		}
		model := new(engine)
		if syncErr := syncOnboardDraft(t, model, out); syncErr != nil {
			t.Fatal(syncErr)
		}
		if !model.config.OnboardPickups {
			t.Fatal("sync lost the opt-in")
		}
		if _, validateErr := model.handle(`{"op":"validate"}`); validateErr != nil {
			t.Fatal(validateErr)
		}
		if report, checkErr := model.draftChecks(); checkErr != nil || len(report.Errors) != 0 {
			t.Fatal("valid draft failed checks", checkErr, report.Errors)
		}
		exported, err := json.Marshal(model.config)
		if err != nil {
			t.Fatal(err)
		}
		var config project.Config
		if decodeErr := json.Unmarshal(exported, &config); decodeErr != nil || !config.OnboardPickups {
			t.Fatal("export lost the opt-in", decodeErr)
		}
	}
}

func TestOnboardEditorPresence(t *testing.T) {
	t.Parallel()
	for _, version := range []float64{1, 2, 3} {
		for _, value := range []any{true, false, nil, "true", float64(1), map[string]any{}} {
			t.Run(fmt.Sprintf("version%g/%v", version, value), func(t *testing.T) {
				t.Parallel()
				draft := onboardEditorDraft(t)
				draft["version"], draft["onboardPickups"] = version, value
				_, boolean := value.(bool)
				valid := version == 3 && boolean
				model := new(engine)
				if err := syncOnboardDraft(t, model, draft); (err == nil) != valid || !model.ready {
					t.Fatal("sync presence validation or raw draft retention failed", err)
				}
				before := cloneEditValue(draft)
				if _, err := normalizeProject(draft); (err == nil) != valid || !reflect.DeepEqual(before, draft) {
					t.Fatal("normalization coerced or upgraded the flag", err)
				}
				if report := draftChecks(draft); (len(report.Errors) == 0) != valid {
					t.Fatal("draft checks accepted invalid presence", report.Errors)
				}
				if _, err := model.handle(`{"op":"validate"}`); (err == nil) != valid {
					t.Fatal("cached validation accepted invalid presence", err)
				}
			})
		}
	}
	for _, version := range []float64{1, 2} {
		draft := onboardEditorDraft(t)
		draft["version"], draft["ONBOARDPICKUPS"] = version, false
		delete(draft, "onboardPickups")
		if _, err := normalizeProject(draft); err == nil {
			t.Fatal("normalization upgraded case-variant legacy presence")
		}
	}
	model := new(engine)
	keys := synchronize(t, model, onboardEditorConfig())
	if _, err := model.sync(request{Keys: keys, Patch: jsontext.Value(`{"version":1}`)}); err == nil {
		t.Fatal("unchanged flag branch allowed a version downgrade")
	}
}

func TestOnboardEditorSharingContract(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name        string
		flag        bool
		limit, mode any
		valid       bool
	}{
		{"off-one", false, float64(1), "destination", true},
		{"on-two", true, float64(2), "drop-offs", true},
		{"on-default-mode", true, float64(2), "", true},
		{"on-one", true, float64(1), "drop-offs", false},
		{"on-zero", true, float64(0), "drop-offs", false},
		{"on-fraction", true, float64(2.5), "drop-offs", false},
		{"on-null-limit", true, nil, "drop-offs", false},
		{"on-destination", true, float64(2), "destination", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			draft := onboardEditorDraft(t)
			draft["onboardPickups"], draft["sharedRidePartyLimit"], draft["sharedRideMode"] = test.flag, test.limit, test.mode
			if report := draftChecks(draft); (len(report.Errors) == 0) != test.valid {
				t.Fatal("draft checks missed sharing contract", report.Errors)
			}
			if _, err := normalizeProject(draft); (err == nil) != test.valid {
				t.Fatal("normalization repaired an invalid opt-in contract", err)
			}
			model := new(engine)
			_ = syncOnboardDraft(t, model, draft)
			if _, err := model.handle(`{"op":"validate"}`); (err == nil) != test.valid {
				t.Fatal("cached validation missed sharing contract", err)
			}
		})
	}
	draft := onboardEditorDraft(t)
	draft["sharedRideMode"] = nil
	if report := draftChecks(draft); len(report.Errors) == 0 {
		t.Fatal("draft checks accepted a null sharing mode")
	}
	if _, err := normalizeProject(draft); err == nil {
		t.Fatal("normalization coerced a null sharing mode with an opt-in")
	}
}

func compareOnboardChecks(t *testing.T, model *engine, wantError bool) {
	t.Helper()
	cached, err := model.draftChecks()
	if err != nil {
		t.Fatal(err)
	}
	draft := make(map[string]any, len(model.branches))
	for key, branch := range model.branches {
		var value any
		if decodeErr := json.Unmarshal(branch.raw, &value); decodeErr != nil {
			t.Fatal(decodeErr)
		}
		draft[key] = value
	}
	fresh := draftChecks(draft)
	if !reflect.DeepEqual(cached, fresh) || (len(cached.Errors) != 0) != wantError {
		t.Fatal("cached and fresh checks differ", cached, fresh)
	}
}

func TestOnboardEditorCacheDependencies(t *testing.T) {
	t.Parallel()
	model := new(engine)
	keys := synchronize(t, model, onboardEditorConfig())
	wantError := false
	for _, test := range []struct {
		patch     string
		wantError bool
	}{
		{`{"onboardPickups":false}`, false},
		{`{"sharedRidePartyLimit":1}`, false},
		{`{"onboardPickups":true}`, true},
		{`{"sharedRidePartyLimit":2}`, false},
		{`{"sharedRideMode":"destination"}`, true},
		{`{"sharedRideMode":"drop-offs"}`, false},
	} {
		compareOnboardChecks(t, model, wantError)
		if _, err := model.sync(request{Keys: keys, Patch: jsontext.Value(test.patch)}); err != nil {
			t.Fatal(err)
		}
		if model.checks.servicesReady {
			t.Fatal("sync retained service checks after a dependency changed")
		}
		compareOnboardChecks(t, model, test.wantError)
		wantError = test.wantError
	}
	config := onboardEditorConfig()
	synchronize(t, model, config)
	acceptedHistory(t, model, historyCommand{Kind: "reset", Background: jsontext.Value(`null`)})
	config.OnboardPickups = false
	synchronize(t, model, config)
	acceptedHistory(t, model, historyCommand{Kind: "replace", Background: jsontext.Value(`null`)})
	for _, kind := range []string{"undo", "redo"} {
		compareOnboardChecks(t, model, false)
		acceptedHistory(t, model, historyCommand{Kind: kind})
		if model.checks.servicesReady {
			t.Fatal("history retained service checks after a dependency changed")
		}
		compareOnboardChecks(t, model, false)
		if model.config.OnboardPickups != (kind == "undo") {
			t.Fatal("history lost the opt-in")
		}
	}
}

func TestOnboardEditorOrdinaryOmission(t *testing.T) {
	t.Parallel()
	config := project.Default()
	want, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	model := new(engine)
	synchronize(t, model, config)
	got, err := json.Marshal(model.config)
	if err != nil || !bytes.Equal(got, want) || bytes.Contains(got, []byte("onboardPickups")) {
		t.Fatal("ordinary editor export bytes changed", err)
	}
	var draft map[string]any
	if decodeErr := json.Unmarshal(want, &draft); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	change, err := normalizeProject(draft)
	if err != nil || has(change.Patch, "onboardPickups") {
		t.Fatal("ordinary normalization introduced an opt-in", err)
	}
}
