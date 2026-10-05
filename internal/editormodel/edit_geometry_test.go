package editormodel

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"maps"
	"math"
	"os"
	"reflect"
	"slices"
	"strconv"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
)

func TestGeometryMatchesExistingEditor(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/geometry.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name    string         `json:"name"`
		Before  map[string]any `json:"before"`
		Command jsontext.Value `json:"command"`
		After   map[string]any `json:"after"`
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			t.Parallel()
			before := cloneEditValue(fixture.Before)
			command, err := json.Marshal(map[string]any{"field": "geometry", "value": fixture.Command})
			if err != nil {
				t.Fatal(err)
			}
			change, err := editProject(fixture.Before, command)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(fixture.Before, before) {
				t.Fatal("geometry proposal changed caller state")
			}
			actual := object(cloneEditValue(fixture.Before))
			maps.Copy(actual, change.Patch)
			if !sameGeometry(actual, fixture.After) {
				t.Fatalf("geometry differs from the existing editor: actual=%v expected=%v", actual, fixture.After)
			}
			model := new(engine)
			keys := slices.Sorted(maps.Keys(fixture.Before))
			sync, err := json.Marshal(map[string]any{"op": "sync", "keys": keys, "patch": fixture.Before})
			if err != nil {
				t.Fatal(err)
			}
			if _, syncErr := model.handle(string(sync)); syncErr != nil {
				t.Fatal(syncErr)
			}
			cached, err := model.handle(`{"op":"edit","edit":` + string(command) + `}`)
			if err != nil || !reflect.DeepEqual(cached.Change, &change) {
				t.Fatal("cached geometry response differs", err)
			}
			projectData, err := json.Marshal(fixture.Before)
			if err != nil {
				t.Fatal(err)
			}
			var explicit response
			if err := json.Unmarshal([]byte(Call(`{"op":"edit","project":`+string(projectData)+`,"edit":`+string(command)+`}`)), &explicit); err != nil || explicit.Error != "" || !reflect.DeepEqual(explicit.Change, &change) {
				t.Fatal("stateless geometry response differs", err, explicit.Error)
			}
			if network := object(change.Patch["network"]); network != nil {
				object(items(network["nodes"])[0])["id"] = "mutated"
				if !reflect.DeepEqual(fixture.Before, before) {
					t.Fatal("geometry response shares source nodes")
				}
				again, err := model.handle(`{"op":"edit","edit":` + string(command) + `}`)
				if err != nil || !sameGeometry(object(again.Change.Patch["network"]), member(fixture.After, "network")) {
					t.Fatal("discarded geometry proposal changed cached state", err)
				}
			}
		})
	}
}

func sameGeometry(actual, expected any) bool {
	switch left := actual.(type) {
	case float64:
		right, ok := expected.(float64)
		return ok && math.Abs(left-right) <= 1e-9
	case map[string]any:
		right, ok := expected.(map[string]any)
		if !ok || len(left) != len(right) {
			return false
		}
		for key, value := range left {
			if !sameGeometry(value, right[key]) {
				return false
			}
		}
		return true
	case []any:
		right, ok := expected.([]any)
		if !ok || len(left) != len(right) {
			return false
		}
		for index, value := range left {
			if !sameGeometry(value, right[index]) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(actual, expected)
	}
}

func TestGeometryRejectsInvalidAndStaleCommands(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		`{}`, `{"action":"unknown"}`, `{"action":"addNode"}`,
		`{"action":"addNode","point":{"x":1}}`,
		`{"action":"addNode","point":{"x":"1","y":2}}`,
		`{"action":"addNode","point":{"x":100001,"y":2}}`,
		`{"action":"addNode","point":{"x":1,"y":2},"id":""}`,
		`{"action":"addNode","point":{"x":1,"y":2},"paired":null}`,
		`{"action":"addStation","point":{"x":99999,"y":2}}`,
		`{"action":"moveNode","id":"missing","point":{"x":1,"y":2}}`,
		`{"action":"moveStation","id":"alpha","delta":{"x":1}}`,
		`{"action":"stationName","id":"alpha","value":true}`,
		`{"action":"stationParking","id":"alpha","value":"true"}`,
		`{"action":"stationBearing","id":"alpha","value":""}`,
		`{"action":"addLane","id":"same","to":"same"}`,
	} {
		draft := fleetDraft()
		before := cloneEditValue(draft)
		if _, err := editGeometry(draft, jsontext.Value(raw)); err == nil || !reflect.DeepEqual(draft, before) {
			t.Errorf("invalid geometry accepted or changed its source: %s", raw)
		}
	}
}

func TestMalformedStationMovementRetainsWorkerState(t *testing.T) {
	t.Parallel()
	const draft = `{"network":{"stations":[{"id":"s"}],"nodes":[null,{"position":{"x":0,"y":0}}]}}`
	const command = `{"field":"geometry","value":{"action":"moveStation","id":"s","delta":{"x":1,"y":1}}}`
	model := new(engine)
	if _, err := model.handle(`{"op":"sync","keys":["network"],"patch":` + draft + `}`); err != nil {
		t.Fatal("the unfinished draft could not synchronize", err)
	}
	before := model.branches["network"].raw.Clone()
	if _, err := model.handle(`{"op":"edit","edit":` + command + `}`); err == nil {
		t.Fatal("malformed cached station accepted movement")
	}
	var explicit response
	if err := json.Unmarshal([]byte(Call(`{"op":"edit","project":`+draft+`,"edit":`+command+`}`)), &explicit); err != nil || explicit.Error == "" {
		t.Fatal("malformed explicit station accepted movement", err)
	}
	if !reflect.DeepEqual(model.branches["network"].raw, before) {
		t.Fatal("failed movement changed synchronized state")
	}
	result, err := model.handle(`{"op":"edit","edit":{"field":"geometry","value":{"action":"stationName","id":"s","value":"Repaired name"}}}`)
	if err != nil || member(items(member(result.Change.Patch["network"], "stations"))[0], "name") != "Repaired name" {
		t.Fatal("rejected movement prevented subsequent editing", err)
	}
}

func TestGeometryRetainsExistingRejections(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/geometry_rejections.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name    string         `json:"name"`
		Before  map[string]any `json:"before"`
		Command jsontext.Value `json:"command"`
		Error   string         `json:"error"`
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			t.Parallel()
			before := cloneEditValue(fixture.Before)
			command, err := json.Marshal(map[string]any{"field": "geometry", "value": fixture.Command})
			if err != nil {
				t.Fatal(err)
			}
			if _, editErr := editProject(fixture.Before, command); editErr == nil {
				t.Fatal("geometry accepted a rejected legacy edit", fixture.Error)
			}
			if !reflect.DeepEqual(fixture.Before, before) {
				t.Fatal("rejected edit changed caller state")
			}
			model := new(engine)
			sync, err := json.Marshal(map[string]any{"op": "sync", "keys": slices.Sorted(maps.Keys(fixture.Before)), "patch": fixture.Before})
			if err != nil {
				t.Fatal(err)
			}
			if _, syncErr := model.handle(string(sync)); syncErr != nil {
				t.Fatal(syncErr)
			}
			original := model.branches["network"].raw.Clone()
			if _, editErr := model.handle(`{"op":"edit","edit":` + string(command) + `}`); editErr == nil {
				t.Fatal("cached edit accepted a rejected legacy edit")
			}
			if !reflect.DeepEqual(original, model.branches["network"].raw) {
				t.Fatal("rejected edit changed synchronized state")
			}
			encoded, err := json.Marshal(fixture.Before)
			if err != nil {
				t.Fatal(err)
			}
			var explicit response
			if err := json.Unmarshal([]byte(Call(`{"op":"edit","project":`+string(encoded)+`,"edit":`+string(command)+`}`)), &explicit); err != nil || explicit.Error == "" {
				t.Fatal("stateless edit accepted a rejected legacy edit", err)
			}
		})
	}
}

func TestGeometryPreservesProjectCountCaps(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		name, key, command string
		limit              int
	}{
		{"nodes", "nodes", `{"action":"addNode","point":{"x":1,"y":2}}`, project.MaxNodes},
		{"stations", "stations", `{"action":"addStation","point":{"x":1,"y":2}}`, project.MaxStations},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			entries := make([]any, row.limit)
			for i := range entries {
				entries[i] = map[string]any{"id": row.name + "-" + strconv.Itoa(i)}
			}
			draft := map[string]any{"network": map[string]any{row.key: entries}}
			before := cloneEditValue(draft)
			if _, err := editGeometry(draft, jsontext.Value(row.command)); err == nil || !reflect.DeepEqual(draft, before) {
				t.Fatal("count cap did not reject growth without changing the draft")
			}
		})
	}
}
