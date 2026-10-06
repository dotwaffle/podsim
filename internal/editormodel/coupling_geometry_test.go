package editormodel

import (
	"encoding/json/jsontext"
	"maps"
	"reflect"
	"testing"
)

func applyEdit(t *testing.T, draft map[string]any, command string) map[string]any {
	t.Helper()
	before := cloneEditValue(draft)
	change, err := editProject(draft, jsontext.Value(command))
	if err != nil {
		t.Fatal(command, err)
	}
	if !reflect.DeepEqual(draft, before) {
		t.Fatal("the proposal changed its source")
	}
	out := object(cloneEditValue(draft))
	maps.Copy(out, change.Patch)
	return out
}

// The draft check reports the native geometry error text, so the worker
// shows one message when native validation reports the same error.
func TestCouplingGeometryChecksMatchNative(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		change func(map[string]any)
	}{
		{"valid", func(map[string]any) {}},
		{"short site", func(draft map[string]any) { object(items(draft["couplingSites"])[0])["endMeters"] = float64(60) }},
		{"overlap", func(draft map[string]any) { object(items(draft["couplingSites"])[1])["laneId"] = "coupling-bc" }},
		{"unknown lane", func(draft map[string]any) { object(items(draft["couplingSites"])[0])["laneId"] = "missing" }},
		{"curved lane", func(draft map[string]any) {
			for _, lane := range items(member(draft["network"], "lanes")) {
				if member(lane, "id") == "coupling-ab" {
					object(lane)["control"] = map[string]any{"x": float64(100), "y": float64(1010)}
				}
			}
		}},
		{"one-sided", func(draft map[string]any) { draft["couplingCorridors"] = []any{} }},
		{"reversed path", func(draft map[string]any) {
			object(items(draft["couplingCorridors"])[0])["laneIds"] = []any{"coupling-bc", "coupling-ab"}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			draft := configDraft(t, couplingEditorConfig(t, false))
			test.change(draft)
			var report checkList
			checkCouplingGeometry(draft, &report)
			err := nativeVerdict(t, encodeDraft(t, draft))
			if (err == nil) != (len(report.items) == 0) || err != nil && report.items[0].Text != err.Error() {
				t.Fatalf("check %v native %v", report.items, err)
			}
		})
	}
}
