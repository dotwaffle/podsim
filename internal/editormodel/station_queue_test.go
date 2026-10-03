package editormodel

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"maps"
	"reflect"
	"slices"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

func TestStationQueueEditorLegacyPresence(t *testing.T) {
	t.Parallel()
	for _, value := range []string{`null`, `"ordinary"`, `"compact-v1"`} {
		for _, version := range []string{"1", "2"} {
			var draft map[string]any
			if err := json.Unmarshal([]byte(`{"version":`+version+`,"stationQueueSpacing":`+value+`}`), &draft); err != nil {
				t.Fatal(err)
			}
			if _, err := normalizeProject(draft); err == nil {
				t.Fatal("normalization granted compact contract to a legacy import")
			}
			raw, _ := json.Marshal(draft)
			model := new(engine)
			if _, err := model.sync(request{Keys: slices.Sorted(maps.Keys(draft)), Patch: raw}); err == nil {
				t.Fatal("legacy queue presence escaped branch validation")
			}
		}
	}
}

func TestStationQueueEditorSelectionAndHistory(t *testing.T) {
	t.Parallel()
	config := project.Default()
	model := new(engine)
	keys := synchronize(t, model, config)
	acceptedHistory(t, model, historyCommand{Kind: "reset", Background: jsontext.Value(`null`)})
	var draft map[string]any
	raw, _ := json.Marshal(config)
	if err := json.Unmarshal(raw, &draft); err != nil {
		t.Fatal(err)
	}
	before := cloneEditValue(draft)
	change, err := editProject(draft, jsontext.Value(`{"field":"stationQueueSpacing","value":"compact-v1"}`))
	if err != nil || change.Patch["version"] != float64(3) || change.Patch["stationQueueSpacing"] != "compact-v1" || change.Flag != "" {
		t.Fatal("explicit queue selection did not grant only the requested contract", err, change)
	}
	if !reflect.DeepEqual(before, draft) || has(change.Patch, "platoonLimit") || has(change.Patch, "stationBuffers") || has(change.Patch, "network") {
		t.Fatal("queue selection changed input, dependencies, or lane speeds")
	}
	keys = append(keys, "stationQueueSpacing")
	patch, _ := json.Marshal(change.Patch)
	if _, err := model.sync(request{Keys: keys, Patch: patch}); err != nil {
		t.Fatal(err)
	}
	if err := project.Validate(model.config); err == nil {
		t.Fatal("compact selection with buffers and platoons off became startable")
	}
	acceptedHistory(t, model, historyCommand{Kind: "replace", Background: jsontext.Value(`null`)})
	acceptedHistory(t, model, historyCommand{Kind: "undo"})
	if model.config.Version != 1 || model.config.StationQueueSpacing != "" {
		t.Fatal("undo retained the new policy or version")
	}
	acceptedHistory(t, model, historyCommand{Kind: "redo"})
	if model.config.Version != 3 || model.config.StationQueueSpacing != sim.StationQueueCompactV1 {
		t.Fatal("redo lost compact metadata")
	}
}

func TestStationQueueEditorChecksAndFlagInvalidation(t *testing.T) {
	t.Parallel()
	for _, value := range []any{nil, "", "other", true, "ordinary", "compact-v1"} {
		for _, buffers := range []bool{false, true} {
			for _, limit := range []float64{0, 2, 3, 4} {
				draft := map[string]any{"stationQueueSpacing": value, "stationBuffers": buffers, "platoonLimit": limit}
				var errors checkList
				checkStationQueueSetting(draft, &errors)
				valid := value == "ordinary" || value == "compact-v1" && buffers && limit >= 2
				if (len(errors.items) == 0) != valid {
					t.Fatalf("setting %v buffers %v limit %v: %+v", value, buffers, limit, errors.items)
				}
			}
		}
	}
	draft := serviceEditorDraft(t)
	draft["stationQueueSpacing"], draft["stationBuffers"], draft["platoonLimit"] = "compact-v1", true, float64(4)
	for _, flag := range []string{"stationBuffers", "redistribution"} {
		raw, _ := json.Marshal(map[string]any{"field": flag, "value": false})
		change, err := editProject(draft, raw)
		if err != nil || (change.Flag == "") != (flag == "stationBuffers") {
			t.Fatal("dependent buffer edit reused stale validation", err, change)
		}
	}
}
