package editormodel

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"maps"
	"os"
	"reflect"
	"slices"
	"testing"
)

func TestMapEditsMatchExistingEditor(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/map_edits.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name    string         `json:"name"`
		Before  map[string]any `json:"before"`
		Command jsontext.Value `json:"command"`
		After   map[string]any `json:"after"`
		Error   string         `json:"error"`
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			t.Parallel()
			before := cloneEditValue(fixture.Before)
			command, err := json.Marshal(map[string]any{"field": "map", "value": fixture.Command})
			if err != nil {
				t.Fatal(err)
			}
			change, err := editProject(fixture.Before, command)
			if !reflect.DeepEqual(fixture.Before, before) {
				t.Fatal("the proposal changed its source")
			}
			if fixture.Error != "" {
				if err == nil || err.Error() != fixture.Error {
					t.Fatalf("error = %v, want %q", err, fixture.Error)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			actual := object(cloneEditValue(fixture.Before))
			for key, value := range change.Patch {
				if key == "map" && value == nil {
					delete(actual, key)
				} else {
					actual[key] = value
				}
			}
			if !sameGeometry(actual, fixture.After) {
				t.Fatalf("proposal = %v, want %v", actual, fixture.After)
			}
			model := new(engine)
			project, err := json.Marshal(fixture.Before)
			if err != nil {
				t.Fatal(err)
			}
			// Unknown metadata can remain in a draft until the user repairs it.
			// The raw branches must synchronize before the proposal runs.
			_, _ = model.sync(request{Keys: slices.Sorted(maps.Keys(fixture.Before)), Patch: project})
			if !model.ready {
				t.Fatal("the map fixture did not synchronize")
			}
			cached, err := model.edit(command)
			if err != nil || !sameGeometry(cached.Change.Patch, change.Patch) {
				t.Fatal("cached proposal differs", err)
			}
			var stateless response
			if decodeErr := json.Unmarshal([]byte(Call(`{"op":"edit","project":`+string(project)+`,"edit":`+string(command)+`}`)), &stateless); decodeErr != nil || stateless.Error != "" || !sameGeometry(stateless.Change.Patch, change.Patch) {
				t.Fatal("stateless proposal differs", decodeErr, stateless.Error)
			}
			if background := object(change.Patch["map"]); background != nil {
				background["opacity"] = .9
				if extra := object(background["extra"]); extra != nil {
					extra["value"] = nil
				}
			}
			if !reflect.DeepEqual(fixture.Before, before) {
				t.Fatal("returned metadata shares source state")
			}
			again, err := model.edit(command)
			if err != nil || !sameGeometry(again.Change.Patch, cached.Change.Patch) {
				t.Fatal("discarded proposal changed the model", err)
			}
		})
	}
}

func TestMapEditsRejectInvalidCommands(t *testing.T) {
	t.Parallel()
	draft := map[string]any{"network": map[string]any{"nodes": []any{}}}
	for _, raw := range []string{
		`null`, `[]`, `{}`, `{"action":"unknown"}`,
		`{"action":"remove","latitude":0}`, `{"action":"opacity"}`,
		`{"action":"enable","latitude":null,"longitude":0}`,
		`{"action":"enable","latitude":0,"longitude":null}`,
		`{"action":"enable","latitude":0,"longitude":0,"opacity":"0.5"}`,
		`{"action":"enable","latitude":0,"longitude":0,"opacity":-1}`,
	} {
		if _, err := editMap(draft, jsontext.Value(raw)); err == nil {
			t.Errorf("accepted invalid command %s", raw)
		}
	}
}
