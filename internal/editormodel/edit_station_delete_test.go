package editormodel

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"maps"
	"reflect"
	"slices"
	"testing"
)

func TestStationDeletionPreservesMalformedReferencesAndOwnsProposals(t *testing.T) {
	t.Parallel()
	draft := fleetDraft()
	draft["demandProfiles"] = []any{nil, map[string]any{
		"id": "profile", "custom": map[string]any{"keep": true},
		"stations": []any{"alpha", "beta", "gamma"},
		"flows":    []any{nil, []any{float64(0), float64(1)}, []any{float64(1), float64(2), float64(2)}},
	}}
	draft["railArrivals"] = []any{nil, map[string]any{"id": "unfinished"}, map[string]any{
		"id": "arrival", "station": "beta", "destinations": []any{nil, map[string]any{"station": "alpha"}, map[string]any{"station": "gamma", "weight": float64(1)}},
	}}
	before := cloneEditValue(draft)
	model := new(engine)
	command, err := json.Marshal(map[string]any{"op": "sync", "keys": slices.Sorted(maps.Keys(draft)), "patch": draft})
	if err != nil {
		t.Fatal(err)
	}
	// Unknown profile fields keep the typed operation barrier active. Draft
	// edits must still own and preserve fields that this edit does not change.
	if _, syncErr := model.handle(string(command)); syncErr == nil {
		t.Fatal("the malformed profile did not retain the typed operation barrier")
	}
	const edit = `{"field":"geometry","value":{"action":"deleteStation","id":"alpha"}}`
	result, err := model.handle(`{"op":"edit","edit":` + edit + `}`)
	if err != nil {
		t.Fatal(err)
	}
	var explicit response
	data, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	if decodeErr := json.Unmarshal([]byte(Call(`{"op":"edit","project":`+string(data)+`,"edit":`+edit+`}`)), &explicit); decodeErr != nil || explicit.Error != "" || !sameGeometry(result.Change.Patch, explicit.Change.Patch) {
		t.Fatal("malformed cached and explicit deletion differ", decodeErr, explicit.Error)
	}
	profiles := items(result.Change.Patch["demandProfiles"])
	profile := object(profiles[1])
	if len(items(profile["flows"])) != 2 || member(profile["custom"], "keep") != true {
		t.Fatal("deletion lost unrelated or malformed profile fields")
	}
	// The kept flow gets the indexes of the new station list.
	if !reflect.DeepEqual(profile["stations"], []any{"beta", "gamma"}) || !reflect.DeepEqual(items(profile["flows"])[1], []any{float64(0), float64(1), float64(2)}) {
		t.Fatal("deletion did not list the stations of the kept flows again", profile["stations"], profile["flows"])
	}
	rail := items(result.Change.Patch["railArrivals"])
	if len(rail) != 3 || len(items(member(rail[2], "destinations"))) != 2 {
		t.Fatal("deletion lost unfinished rail events or unrelated choices")
	}
	expected := cloneEditValue(result.Change.Patch)
	object(profile["custom"])["keep"] = false
	items(items(profile["flows"])[1])[0] = "mutated"
	object(items(member(rail[2], "destinations"))[1])["station"] = "mutated"
	object(items(result.Change.Patch["fleet"])[0])["id"] = "mutated"
	if !reflect.DeepEqual(draft, before) || model.branches["demandProfiles"].value != nil || member(items(model.profiles)[1], "flows") != nil {
		t.Fatal("deletion changed source state or retained full generic flows")
	}
	again, err := model.handle(`{"op":"edit","edit":` + edit + `}`)
	if err != nil || !sameGeometry(again.Change.Patch, expected) {
		t.Fatal("discarded deletion changed the synchronized project", err)
	}
	if _, err := model.handle(`{"op":"edit","edit":{"field":"geometry","value":{"action":"deleteStation","id":"missing"}}}`); err == nil {
		t.Fatal("deletion accepted a stale station ID")
	}
}

func TestStationDeletionNeedsNoProfileBranch(t *testing.T) {
	t.Parallel()
	change, err := editProject(fleetDraft(), jsontext.Value(`{"field":"geometry","value":{"action":"deleteStation","id":"alpha"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, found := change.Patch["demandProfiles"]; found {
		t.Fatal("deletion added an absent profile branch")
	}
}
