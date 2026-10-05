package editormodel

import (
	"encoding/json/v2"
	"maps"
	"os"
	"reflect"
	"slices"
	"testing"
)

func TestNormalizationMatchesExistingEditor(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/normalization.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name   string         `json:"name"`
		Before map[string]any `json:"before"`
		After  map[string]any `json:"after"`
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			t.Parallel()
			before := cloneEditValue(fixture.Before)
			change, err := normalizeProject(fixture.Before)
			if err != nil {
				t.Fatal(err)
			}
			actual := object(cloneEditValue(fixture.Before))
			maps.Copy(actual, change.Patch)
			if !sameGeometry(actual, fixture.After) || !reflect.DeepEqual(fixture.Before, before) {
				t.Fatalf("normalization differs or changes source: actual=%v expected=%v", actual, fixture.After)
			}
			model := new(engine)
			sync, err := json.Marshal(map[string]any{"op": "sync", "keys": slices.Sorted(maps.Keys(fixture.Before)), "patch": fixture.Before})
			if err != nil {
				t.Fatal(err)
			}
			// Normalization can repair decoded-invalid drafts. Structural
			// synchronization must still succeed and retain the raw branches.
			_, _ = model.handle(string(sync))
			if !model.ready {
				t.Fatal("normalization fixture did not synchronize")
			}
			const command = `{"field":"normalize","value":true}`
			cached, err := model.handle(`{"op":"edit","edit":` + command + `}`)
			if err != nil || !sameGeometry(cached.Change.Patch, change.Patch) {
				t.Fatal("cached normalization differs", err)
			}
			projectData, err := json.Marshal(fixture.Before)
			if err != nil {
				t.Fatal(err)
			}
			var explicit response
			if decodeErr := json.Unmarshal([]byte(Call(`{"op":"edit","project":`+string(projectData)+`,"edit":`+command+`}`)), &explicit); decodeErr != nil || explicit.Error != "" || !sameGeometry(explicit.Change.Patch, change.Patch) {
				t.Fatal("explicit normalization differs", decodeErr, explicit.Error)
			}
			if network := object(change.Patch["network"]); network != nil {
				network["stations"] = []any{}
			}
			if fleet := items(change.Patch["fleet"]); len(fleet) != 0 && object(fleet[0]) != nil {
				object(fleet[0])["id"] = "mutated"
			}
			again, err := model.handle(`{"op":"edit","edit":` + command + `}`)
			if err != nil || !sameGeometry(again.Change.Patch, cached.Change.Patch) || !reflect.DeepEqual(fixture.Before, before) || model.branches["demandProfiles"].value != nil {
				t.Fatal("discarded normalization changed or retained generic source", err)
			}
		})
	}
}

func TestNormalizationRejectsMalformedBranchesWithoutPoisoningWorker(t *testing.T) {
	t.Parallel()
	for _, draft := range []string{
		`{"network":"invalid"}`, `{"demand":"invalid"}`,
		`{"demandProfiles":[null]}`, `{"fleet":["invalid"]}`,
	} {
		model := new(engine)
		var branches map[string]any
		if err := json.Unmarshal([]byte(draft), &branches); err != nil {
			t.Fatal(err)
		}
		sync, err := json.Marshal(map[string]any{"op": "sync", "keys": slices.Sorted(maps.Keys(branches)), "patch": branches})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = model.handle(string(sync))
		if !model.ready {
			t.Fatal("the malformed draft could not synchronize")
		}
		if _, err := model.handle(`{"op":"edit","edit":{"field":"normalize","value":true}}`); err == nil {
			t.Fatal("malformed normalization did not return an error", draft)
		}
		if _, err := model.handle(`{"op":"edit","edit":{"field":"name","value":"Repair"}}`); err != nil {
			t.Fatal("malformed normalization poisoned the worker", err)
		}
	}
}

func TestNormalizationOwnsMalformedProfileAndBandIDs(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"profile", "band"} {
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			id := map[string]any{"nested": map[string]any{"keep": true}}
			profile := map[string]any{"id": "profile", "bands": []any{map[string]any{"id": id}}}
			if key == "profile" {
				profile["id"], profile["bands"] = id, []any{}
			}
			draft := map[string]any{"demandProfiles": []any{profile}}
			before := cloneEditValue(draft)
			change, err := normalizeProject(draft)
			if err != nil {
				t.Fatal(err)
			}
			returned := member(change.Patch["demand"], key)
			if object(returned) == nil {
				t.Fatal("the malformed ID was not retained")
			}
			object(member(returned, "nested"))["keep"] = false
			if !reflect.DeepEqual(draft, before) {
				t.Fatal("normalization shares a malformed ID with its caller")
			}
		})
	}
}
