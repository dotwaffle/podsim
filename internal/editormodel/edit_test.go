package editormodel

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"reflect"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
)

func editFixture() map[string]any {
	return map[string]any{
		"name": "Draft", "redistribution": false, "pickupReassignment": false,
		"demand":         map[string]any{"enabled": false, "perMinute": float64(12), "seed": float64(1), "pattern": "balanced", "profile": "old", "band": "old", "dailyStartMinute": float64(300)},
		"demandProfiles": []any{map[string]any{"id": "first", "bands": []any{map[string]any{"id": "morning"}}}, map[string]any{"id": "second", "bands": []any{map[string]any{"id": "evening"}}}},
	}
}

func TestNearCapEditsRejectGrowthAndPermitShrink(t *testing.T) {
	t.Parallel()
	// The third station ID pads the project to the size.
	head := func(padding int) string {
		return `{"name":"Draft","demandProfiles":[{"id":"p","name":"P","bands":[],"stations":["a","b","x` +
			strings.Repeat("z", padding) + `"],"flows":[`
	}
	const suffix = `]}]}`
	const weight = "0.0000010000000000000002"
	flow := `[0,1,` + strings.Repeat(weight+",", project.MaxBands-1) + weight + `]`
	const last = `[2,0,1]`
	wantSize := project.MaxFileBytes - 10
	count := (wantSize - len(head(0)) - len(suffix) - len(last)) / (len(flow) + 1)
	if count >= project.MaxFlows {
		t.Fatal("near-cap fixture exceeds the flow count")
	}
	padding := wantSize - len(head(0)) - len(suffix) - count*(len(flow)+1) - len(last)
	data := head(padding) + strings.Repeat(flow+",", count) + last + suffix
	if len(data) != wantSize {
		t.Fatal("fixture is not near the byte limit")
	}
	model := new(engine)
	keys := []string{"name", "demandProfiles"}
	if _, err := model.handle(`{"op":"sync","keys":["name","demandProfiles"],"patch":` + data + `}`); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{strings.Repeat("N", 80), ""} {
		command := `{"field":"name","value":"` + value + `"}`
		cached, editErr := model.handle(`{"op":"edit","edit":` + command + `}`)
		var explicit response
		if err := json.Unmarshal([]byte(Call(`{"op":"edit","project":`+data+`,"edit":`+command+`}`)), &explicit); err != nil {
			t.Fatal(err)
		}
		if value != "" {
			if editErr == nil || explicit.Error == "" {
				t.Fatal("growth beyond the byte limit was accepted")
			}
			continue
		}
		if editErr != nil || explicit.Error != "" || !reflect.DeepEqual(cached.Change, explicit.Change) {
			t.Fatal("shrinking edit was rejected", editErr, explicit.Error)
		}
		patch, err := json.Marshal(cached.Change.Patch)
		if err != nil {
			t.Fatal(err)
		}
		if _, syncErr := model.sync(request{Keys: keys, Patch: patch}); syncErr != nil || model.config.Name != "" {
			t.Fatal("accepted shrink failed synchronization", syncErr)
		}
	}
}

func TestDraftProfileSelectionParity(t *testing.T) {
	t.Parallel()
	model := new(engine)
	for _, profiles := range []string{`[{"bands":[{}],"flows":[]}]`, `[{"id":"p","bands":[{"id":"b"}],"flows":[]}]`} {
		data := `{"demand":{"pattern":"balanced"},"demandProfiles":` + profiles + `}`
		if _, err := model.handle(`{"op":"sync","keys":["demand","demandProfiles"],"patch":` + data + `}`); err != nil {
			t.Fatal(err)
		}
		command := `{"field":"demandPattern","value":"profile"}`
		cached, editErr := model.handle(`{"op":"edit","edit":` + command + `}`)
		var explicit response
		if err := json.Unmarshal([]byte(Call(`{"op":"edit","project":`+data+`,"edit":`+command+`}`)), &explicit); err != nil || editErr != nil || !reflect.DeepEqual(cached.Change, explicit.Change) {
			t.Fatal("raw profile selections differ", err, editErr)
		}
	}
}

func TestScalarEdits(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		name, field, value, key string
		want                    any
	}{
		{"trim name", "name", `" \ufeffNetwork\u2009 "`, "name", "Network"},
		{"keep non-ECMAScript space", "name", `"\u0085Network\u0085"`, "name", "\u0085Network\u0085"},
		{"keep empty name for checks", "name", `" "`, "name", ""},
		{"floor rate", "demandRate", `"17.9"`, "perMinute", float64(17)},
		{"empty rate", "demandRate", `""`, "perMinute", float64(0)},
		{"negative seed", "demandSeed", `-1`, "seed", float64(0)},
		{"exponent seed", "demandSeed", `"2e3"`, "seed", float64(2000)},
		{"zero sharing fallback", "sharedRidePartyLimit", `"0"`, "sharedRidePartyLimit", float64(1)},
		{"sharing cap", "sharedRidePartyLimit", `"90"`, "sharedRidePartyLimit", float64(8)},
		{"sharing floor", "sharedRidePartyLimit", `"3.8"`, "sharedRidePartyLimit", float64(3)},
		{"stop fallback", "sharedRideMaxStops", `""`, "sharedRideMaxStops", float64(3)},
		{"stop cap", "sharedRideMaxStops", `"10"`, "sharedRideMaxStops", float64(7)},
		{"mode", "sharedRideMode", `"destination"`, "sharedRideMode", "destination"},
		{"mode fallback", "sharedRideMode", `"unknown"`, "sharedRideMode", "drop-offs"},
		{"join", "sharedRideJoin", `"reassign-existing"`, "sharedRideJoin", "reassign-existing"},
		{"join fallback", "sharedRideJoin", `"unknown"`, "sharedRideJoin", "unassigned"},
		{"platoon", "platoonLimit", `"4"`, "platoonLimit", float64(4)},
		{"invalid platoon", "platoonLimit", `"5"`, "platoonLimit", float64(0)},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			draft := editFixture()
			before := cloneEditValue(draft)
			change, err := editProject(draft, jsontext.Value(`{"field":"`+row.field+`","value":`+row.value+`}`))
			if err != nil {
				t.Fatal(err)
			}
			got := change.Patch[row.key]
			if row.field == "demandRate" || row.field == "demandSeed" {
				got = member(change.Patch["demand"], row.key)
			}
			if !reflect.DeepEqual(got, row.want) || !reflect.DeepEqual(draft, before) {
				t.Fatalf("value = %#v, want %#v, changed input = %v", got, row.want, !reflect.DeepEqual(draft, before))
			}
		})
	}
}

func TestDemandSelectionEdits(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		field, selected, profile, band string
		daily                          bool
	}{
		{"demandDestination", "garden", "old", "old", true},
		{"demandBand", "chosen", "old", "chosen", true},
		{"demandProfile", "second", "second", "evening", true},
		{"demandProfile", "missing", "missing", "", true},
		{"demandPattern", "profile", "first", "morning", false},
		{"demandPattern", "profile-daily", "first", "", true},
		{"demandPattern", "balanced", "old", "old", false},
		{"demandPattern", "rail-services", "old", "old", false},
	} {
		t.Run(row.field+"/"+row.selected, func(t *testing.T) {
			t.Parallel()
			draft := editFixture()
			change, err := editProject(draft, jsontext.Value(`{"field":"`+row.field+`","value":"`+row.selected+`"}`))
			if err != nil {
				t.Fatal(err)
			}
			demand := object(change.Patch["demand"])
			if demand == nil || demand["profile"] != row.profile || demand["band"] != row.band || (demand["dailyStartMinute"] != nil) != row.daily {
				t.Fatalf("demand = %#v", demand)
			}
			if row.field == "demandDestination" && demand["destination"] != "garden" {
				t.Fatal("destination was not updated")
			}
		})
	}
	for _, profiles := range []any{nil, []any{}} {
		draft := editFixture()
		draft["demandProfiles"] = profiles
		delete(object(draft["demand"]), "dailyStartMinute")
		change, err := editProject(draft, jsontext.Value(`{"field":"demandPattern","value":"profile-daily"}`))
		if err != nil || member(change.Patch["demand"], "profile") != "old" || member(change.Patch["demand"], "dailyStartMinute") != float64(0) {
			t.Fatal("daily selection changed missing-profile behavior")
		}
	}
}

func TestEditFlagsNoopAndOwnership(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"demandEnabled", "redistribution", "pickupReassignment"} {
		draft := editFixture()
		change, err := editProject(draft, jsontext.Value(`{"field":"`+field+`","value":true}`))
		if err != nil || change.Flag != field || len(change.Patch) != 1 {
			t.Fatalf("flag = %+v, error = %v", change, err)
		}
	}
	draft := editFixture()
	change, err := editProject(draft, jsontext.Value(`{"field":"name","value":"Draft"}`))
	if err != nil || change.Patch == nil || len(change.Patch) != 0 {
		t.Fatal("unchanged edit needs an empty patch object")
	}
	// Malformed nested demand must neither panic during comparison nor alias.
	demand := object(draft["demand"])
	demand["unknown"] = map[string]any{"nested": []any{"original"}}
	change, err = editProject(draft, jsontext.Value(`{"field":"demandPattern","value":"profile"}`))
	if err != nil {
		t.Fatal(err)
	}
	nested := items(member(member(change.Patch["demand"], "unknown"), "nested"))
	nested[0] = "changed"
	if items(member(demand["unknown"], "nested"))[0] != "original" {
		t.Fatal("replacement demand shares nested input")
	}
	demand["enabled"] = "invalid"
	change, err = editProject(draft, jsontext.Value(`{"field":"demandEnabled","value":true}`))
	if err != nil || change.Flag != "" {
		t.Fatal("repairing a malformed flag must rerun checks")
	}
}

func TestRejectedEditCommands(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		`null`, `{}`, `{"field":"name"}`, `{"field":"name","value":null}`,
		`{"field":"name","value":{}}`, `{"field":"name","value":[]}`,
		`{"field":"name","value":true}`, `{"field":"unknown","value":1}`,
		`{"field":"name","value":"x","extra":true}`,
		`{"field":"demandEnabled","value":1}`, `{"field":"demandRate","value":"NaN"}`,
		`{"field":"demandSeed","value":"Infinity"}`, `{"field":"demandRate","value":false}`,
		`{"field":"demandPattern","value":"unknown"}`, `{"field":"demandProfile","value":1}`,
		`{"field":"sharedRideJoin","value":1}`,
	} {
		if _, err := editProject(editFixture(), jsontext.Value(raw)); err == nil {
			t.Errorf("invalid edit accepted: %s", raw)
		}
	}
	if _, err := editProject(map[string]any{}, jsontext.Value(`{"field":"demandRate","value":2}`)); err == nil {
		t.Fatal("missing demand settings accepted")
	}
}

func TestEditProtocolAndDiscardedProposal(t *testing.T) {
	t.Parallel()
	config := project.Default()
	profile, demand, err := makeParkRide(config, validPlan())
	if err != nil {
		t.Fatal(err)
	}
	config.DemandProfiles, config.Demand = []project.DemandProfile{profile}, demand
	model := new(engine)
	keys := synchronize(t, model, config)
	if _, checkErr := model.handle(`{"op":"checks"}`); checkErr != nil {
		t.Fatal(checkErr)
	}
	before := project.Clone(model.config)
	prepared := model.checks
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{
		`{"field":"name","value":"New name"}`,
		`{"field":"demandPattern","value":"profile"}`,
		`{"field":"pickupReassignment","value":true}`,
		`{"field":"fleetCount","target":"harbor","value":"0"}`,
		`{"field":"dailyStartTime","value":"23:59"}`,
		`{"field":"railArrival","value":{"action":"add"}}`,
		`{"field":"railDeparture","value":{"action":"add"}}`,
	} {
		cached, editErr := model.handle(`{"op":"edit","edit":` + command + `}`)
		if editErr != nil || cached.Change == nil {
			t.Fatal("cached edit failed", editErr)
		}
		var stateless response
		if decodeErr := json.Unmarshal([]byte(Call(`{"op":"edit","project":`+string(data)+`,"edit":`+command+`}`)), &stateless); decodeErr != nil || !reflect.DeepEqual(cached.Change, stateless.Change) {
			t.Fatal("cached and explicit edit responses differ", decodeErr)
		}
		if !reflect.DeepEqual(model.config, before) || prepared != model.checks {
			t.Fatal("discarded edit changed worker state or its cache")
		}
	}
	for _, command := range []string{
		`{"op":"edit"}`, `{"op":"edit","edit":{},"keys":[]}`, `{"op":"edit","edit":{},"view":{}}`,
		`{"op":"edit","edit":{},"parkRide":{}}`, `{"op":"validate","edit":{}}`,
		`{"op":"checks","edit":{}}`, `{"op":"sync","edit":{},"keys":[],"patch":{}}`,
	} {
		if _, commandErr := model.handle(command); commandErr == nil {
			t.Errorf("unrelated edit parameters accepted: %s", command)
		}
	}
	if _, syncErr := model.sync(request{Keys: keys, Patch: jsontext.Value(`{"name":false}`)}); syncErr == nil {
		t.Fatal("malformed name passed synchronization")
	}
	result, err := model.handle(`{"op":"edit","edit":{"field":"name","value":"Repaired"}}`)
	if err != nil || result.Change.Patch["name"] != "Repaired" {
		t.Fatal("edit could not propose a repair to invalid synchronized state", err)
	}
	if _, validateErr := model.handle(`{"op":"validate"}`); validateErr == nil {
		t.Fatal("uncommitted repair cleared the invalid-state barrier")
	}
	patch, err := json.Marshal(result.Change.Patch)
	if err != nil {
		t.Fatal(err)
	}
	if _, syncErr := model.sync(request{Keys: keys, Patch: patch}); syncErr != nil {
		t.Fatal("accepted repair failed synchronization", syncErr)
	}
	if validated, validateErr := model.handle(`{"op":"validate"}`); validateErr != nil || !validated.Valid {
		t.Fatal("accepted repair did not recover valid project", validateErr)
	}
}
