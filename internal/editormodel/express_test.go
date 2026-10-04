package editormodel

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"maps"
	"reflect"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

func expressEditorConfig(t *testing.T) project.Config {
	t.Helper()
	config := serviceEditorConfig(t)
	config.OrderContract = sim.ExpressOrderContract
	config.Fleet[0].Class = sim.ExpressClass
	if err := project.Validate(config); err != nil {
		t.Fatal(err)
	}
	return config
}

func TestExpressEditorOwnershipAndHistory(t *testing.T) {
	t.Parallel()
	config := expressEditorConfig(t)
	model := new(engine)
	keys := synchronize(t, model, config)
	acceptedHistory(t, model, historyCommand{Kind: "reset", Background: jsontext.Value(`null`)})
	if _, err := model.sync(request{Keys: keys, Patch: jsontext.Value(`{"name":"Edited Express project"}`)}); err != nil {
		t.Fatal(err)
	}
	acceptedHistory(t, model, historyCommand{Kind: "replace", Background: jsontext.Value(`null`)})
	for _, kind := range []string{"undo", "redo"} {
		acceptedHistory(t, model, historyCommand{Kind: kind})
		if model.config.Version != project.CurrentVersion || model.config.OrderContract != sim.ExpressOrderContract || model.config.Fleet[0].Class != sim.ExpressClass {
			t.Fatal("history changed the Express contract or class", kind)
		}
		if !reflect.DeepEqual(model.config.ExpressServices, config.ExpressServices) {
			t.Fatal("history changed service identities", kind)
		}
	}
	config.ExpressServices[0].PartyLimit = 1
	if model.config.ExpressServices[0].PartyLimit != 20 {
		t.Fatal("editor retains caller-owned service data")
	}
	if _, err := model.sync(request{Keys: keys, Patch: jsontext.Value(`{"version":4}`)}); err == nil {
		t.Fatal("project version 4 accepted an Express contract")
	}
	if _, err := model.sync(request{Keys: keys, Patch: jsontext.Value(`{"version":1}`)}); err != nil {
		t.Fatal("version 1 repair failed", err)
	}
}

func TestExpressEditorNormalizationAndQueueEdits(t *testing.T) {
	t.Parallel()
	raw, err := json.Marshal(expressEditorConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	var draft map[string]any
	if err = json.Unmarshal(raw, &draft); err != nil {
		t.Fatal(err)
	}
	before := cloneEditValue(draft)
	change, err := normalizeProject(draft)
	if err != nil {
		t.Fatal(err)
	}
	out := object(cloneEditValue(draft))
	maps.Copy(out, change.Patch)
	if number(out["version"]) != project.CurrentVersion || out["orderContract"] != "express-v1" || !reflect.DeepEqual(out["fleet"], draft["fleet"]) || !reflect.DeepEqual(out["expressServices"], draft["expressServices"]) {
		t.Fatal("normalization changed the contract, fleet, or registry")
	}
	// Native refuses station queue spacing with Express but without the
	// coupling marker, so the editor refuses the edit.
	if _, err = editProject(draft, jsontext.Value(`{"field":"stationQueueSpacing","value":"ordinary"}`)); err == nil || !strings.Contains(err.Error(), "requires couplingContract compact-pair-v1") {
		t.Fatal("queue edit accepted with Express but without coupling", err)
	}
	if !reflect.DeepEqual(draft, before) {
		t.Fatal("edit changed its owned input")
	}
}

func TestExpressEditorRejectsContractPresenceAndValues(t *testing.T) {
	t.Parallel()
	for _, version := range []float64{1, 2, 3, 4} {
		for _, value := range []any{nil, "", "future", "express-v1"} {
			if version == project.CurrentVersion && value == "express-v1" {
				continue
			}
			t.Run(fmt.Sprintf("version-%d-%v", int(version), value), func(t *testing.T) {
				t.Parallel()
				draft := map[string]any{"version": version, "orderContract": value}
				if _, err := normalizeProject(draft); err == nil {
					t.Fatal("normalization accepted an invalid contract")
				}
				if checks := draftChecks(draft); len(checks.Errors) == 0 {
					t.Fatal("checks accepted an invalid contract")
				}
			})
		}
	}
	for _, draft := range []map[string]any{{"version": float64(4)}, {"version": float64(1), "ORDERCONTRACT": nil}} {
		if _, err := normalizeProject(draft); err == nil {
			t.Fatal("normalization manufactured or discarded contract presence")
		}
	}
}
