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

func TestBackgroundEditsMatchExistingEditor(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/background_edits.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name    string         `json:"name"`
		Before  map[string]any `json:"before"`
		Command jsontext.Value `json:"command"`
		After   any            `json:"after"`
		Note    string         `json:"note"`
		Error   string         `json:"error"`
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			t.Parallel()
			before := cloneEditValue(fixture.Before)
			command, err := json.Marshal(map[string]any{"field": "background", "value": fixture.Command})
			if err != nil {
				t.Fatal(err)
			}
			change, err := editProject(fixture.Before, command)
			if !reflect.DeepEqual(before, fixture.Before) {
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
			if change.Background == nil {
				t.Fatal("missing background proposal")
			}
			actual := object(cloneEditValue(fixture.Before))
			maps.Copy(actual, change.Patch)
			if !sameGeometry(map[string]any{"scenario": actual, "background": change.Background.Value}, fixture.After) || change.Note != fixture.Note {
				t.Fatalf("proposal = %+v, want %v, note %q", change, fixture.After, fixture.Note)
			}
			model := new(engine)
			project, err := json.Marshal(fixture.Before)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = model.sync(request{Keys: slices.Sorted(maps.Keys(fixture.Before)), Patch: project})
			if !model.ready {
				t.Fatal("the fixture did not synchronize")
			}
			cached, err := model.edit(command)
			if err != nil || !sameBackgroundChange(*cached.Change, change) {
				t.Fatal("cached proposal differs", err)
			}
			var stateless response
			if decodeErr := json.Unmarshal([]byte(Call(`{"op":"edit","project":`+string(project)+`,"edit":`+string(command)+`}`)), &stateless); decodeErr != nil || stateless.Error != "" || !sameBackgroundChange(*stateless.Change, change) {
				t.Fatal("stateless proposal differs", decodeErr, stateless.Error)
			}
			if background := object(change.Background.Value); background != nil {
				background["x"] = 999
				if extra := object(background["extra"]); extra != nil {
					extra["value"] = nil
				}
			}
			if geo := object(change.Patch["geo"]); geo != nil {
				geo["latitude"] = 0
			}
			if !reflect.DeepEqual(before, fixture.Before) {
				t.Fatal("returned metadata shares source state")
			}
			again, err := model.edit(command)
			if err != nil || !sameBackgroundChange(*again.Change, *cached.Change) {
				t.Fatal("a discarded proposal changed the model", err)
			}
		})
	}
}

func TestBackgroundEditsRejectInvalidCommands(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		`null`, `[]`, `{}`, `{"action":"unknown"}`, `{"action":"remove","imageKey":"a"}`,
		`{"action":"initialize","imageKey":"abc","width":1,"height":1}`,
		`{"action":"initialize","imageKey":"0123456789abcdef0123456789abcdef","width":1.5,"height":1}`,
		`{"action":"initialize","imageKey":"0123456789abcdef0123456789abcdef","width":16385,"height":1}`,
		`{"action":"initialize","imageKey":"0123456789abcdef0123456789abcdef","width":16384,"height":16384}`,
		`{"action":"opacity","background":{"x":0,"y":0,"width":1,"height":1,"opacity":1},"opacity":null}`,
		`{"action":"opacity","background":{"x":0,"y":0,"width":1,"height":1,"opacity":1},"opacity":2}`,
		`{"action":"calibrate","background":{"x":0,"y":0,"width":1e308,"height":1,"opacity":1},"a":{"X":0,"Y":0},"b":{"X":1,"Y":0},"meters":1e308}`,
	} {
		if _, err := editBackground(map[string]any{}, jsontext.Value(raw)); err == nil {
			t.Errorf("accepted invalid command %s", raw)
		}
	}
}

func sameBackgroundChange(a, b projectChange) bool {
	return a.Background != nil && b.Background != nil && sameGeometry(a.Patch, b.Patch) && sameGeometry(a.Background.Value, b.Background.Value) && a.Note == b.Note
}
