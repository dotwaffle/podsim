package editormodel

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

func serviceEditorConfig(t *testing.T) project.Config {
	t.Helper()
	config := project.Default()
	classes, err := sim.NewClassSet("legacy", "compact", "group", "express")
	if err != nil {
		t.Fatal(err)
	}
	for i := range config.Network.Lanes {
		config.Network.Lanes[i].VehicleClasses = classes
	}
	for i := range config.Network.Stations {
		config.Network.Stations[i].VehicleClasses = classes
		for j := range config.Network.Stations[i].Berths {
			config.Network.Stations[i].Berths[j].VehicleClasses = classes
		}
	}
	config.Fleet[0].Class = sim.CompactClass
	config.ExpressServices = []sim.ExpressService{{ID: "express", From: config.Network.Stations[0].ID, To: config.Network.Stations[1].ID, Class: sim.ExpressClass, PartyLimit: 20}}
	if err := project.Validate(config); err != nil {
		t.Fatal(err)
	}
	return config
}

func serviceEditorDraft(t *testing.T) map[string]any {
	t.Helper()
	raw, err := json.Marshal(serviceEditorConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	var draft map[string]any
	if decodeErr := json.Unmarshal(raw, &draft); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	return draft
}

func TestServiceEditorBranchesHistoryAndOwnership(t *testing.T) {
	t.Parallel()
	config := serviceEditorConfig(t)
	model := new(engine)
	keys := synchronize(t, model, config)
	if !reflect.DeepEqual(config.ExpressServices, model.config.ExpressServices) || model.config.Fleet[0].Class != sim.CompactClass || model.config.Network.Lanes[0].VehicleClasses != config.Network.Lanes[0].VehicleClasses {
		t.Fatal("service branches lost metadata")
	}
	config.ExpressServices[0].PartyLimit = 1
	if model.config.ExpressServices[0].PartyLimit != 20 {
		t.Fatal("service branch shares caller data")
	}
	acceptedHistory(t, model, historyCommand{Kind: "reset", Background: jsontext.Value(`null`)})
	if _, err := model.sync(request{Keys: keys, Patch: jsontext.Value(`{"expressServices":[]}`)}); err != nil {
		t.Fatal(err)
	}
	acceptedHistory(t, model, historyCommand{Kind: "replace", Background: jsontext.Value(`null`)})
	acceptedHistory(t, model, historyCommand{Kind: "undo"})
	if len(model.config.ExpressServices) != 1 || model.config.ExpressServices[0].PartyLimit != 20 || model.config.Version != project.CurrentVersion || model.config.Fleet[0].Class != sim.CompactClass {
		t.Fatal("undo lost service metadata")
	}
	acceptedHistory(t, model, historyCommand{Kind: "redo"})
	if len(model.config.ExpressServices) != 0 {
		t.Fatal("redo retained registry")
	}
	keys = slices.DeleteFunc(keys, func(key string) bool { return key == "expressServices" })
	if _, err := model.sync(request{Keys: keys, Patch: jsontext.Value(`{}`)}); err != nil || model.config.ExpressServices != nil {
		t.Fatal("removed registry retained metadata", err)
	}
}

func TestServiceEditorEarlierVersionsAndRepair(t *testing.T) {
	t.Parallel()
	for _, field := range []string{
		`"expressServices":null`, `"expressServices":[]`,
		`"fleet":[{"Class":null}]`, `"fleet":[{"Class":"legacy"}]`,
		`"network":{"Stations":[{"VehicleClasses":null}]}`,
		`"network":{"Lanes":[{"VehicleClasses":["legacy"]}]}`,
		`"network":{"Stations":[{"Berths":[{"VehicleClasses":["compact"]}]}]}`,
	} {
		for _, version := range []string{"2", "3"} {
			var branches map[string]jsontext.Value
			if err := json.Unmarshal([]byte(`{"version":`+version+`,`+field+`}`), &branches); err != nil {
				t.Fatal(err)
			}
			model := new(engine)
			raw, _ := json.Marshal(branches)
			if _, err := model.sync(request{Keys: slices.Sorted(maps.Keys(branches)), Patch: raw}); err == nil || !model.ready {
				t.Fatalf("earlier version accepted or raw draft lost: %s %s", version, field)
			}
			if _, err := model.handle(`{"op":"validate"}`); err == nil {
				t.Fatal("invalid metadata became startable")
			}
			keys := synchronize(t, model, project.Default())
			if _, err := model.sync(request{Keys: keys, Patch: jsontext.Value(`{}`)}); err != nil {
				t.Fatal("repair failed", err)
			}
		}
	}
	model := new(engine)
	config := serviceEditorConfig(t)
	keys := synchronize(t, model, config)
	if _, err := model.sync(request{Keys: keys, Patch: jsontext.Value(`{"version":3}`)}); err == nil {
		t.Fatal("service metadata accepted project version 3")
	}
	if _, err := model.sync(request{Keys: keys, Patch: jsontext.Value(`{"version":1}`)}); err != nil {
		t.Fatal("version 1 repair failed", err)
	}
}

func TestServicePresenceSurvivesReleasedHistoryNetwork(t *testing.T) {
	t.Parallel()
	config := serviceEditorConfig(t)
	config.ExpressServices = nil
	for index := range config.Fleet {
		config.Fleet[index].Class = ""
	}
	model := new(engine)
	keys := synchronize(t, model, config)
	acceptedHistory(t, model, historyCommand{Kind: "reset", Background: jsontext.Value(`null`)})
	config.Network.Nodes[0].Position.X += 1
	patch, err := json.Marshal(map[string]any{"network": config.Network})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := model.sync(request{Keys: keys, Patch: patch}); err != nil {
		t.Fatal(err)
	}
	acceptedHistory(t, model, historyCommand{Kind: "replace", Background: jsontext.Value(`null`)})
	acceptedHistory(t, model, historyCommand{Kind: "undo"})
	if !model.branches["network"].needsValue || model.branches["network"].value != nil {
		t.Fatal("fixture did not release history network")
	}
	if _, err := model.sync(request{Keys: keys, Patch: jsontext.Value(`{"version":3}`)}); err == nil {
		t.Fatal("released network accepted project version 3")
	}
	if _, err := model.sync(request{Keys: keys, Patch: jsontext.Value(`{"version":1}`)}); err != nil {
		t.Fatal("version 1 repair failed", err)
	}
	if model.config.Network.Lanes[0].VehicleClasses != config.Network.Lanes[0].VehicleClasses {
		t.Fatal("released network lost raw service presence")
	}
}

func TestBanksSurviveReleasedHistoryNetwork(t *testing.T) {
	t.Parallel()
	draft := bankEditorFixture(1)
	model := new(engine)
	background := historyTarget(t, model, map[string]any{"scenario": draft, "background": nil})
	if model.err != nil {
		t.Fatal(model.err)
	}
	acceptedHistory(t, model, historyCommand{Kind: "reset", Background: background})
	object(member(items(member(draft["network"], "Nodes"))[0], "Position"))["X"] = float64(10)
	background = historyTarget(t, model, map[string]any{"scenario": draft, "background": nil})
	acceptedHistory(t, model, historyCommand{Kind: "replace", Background: background})
	acceptedHistory(t, model, historyCommand{Kind: "undo"})
	if !model.branches["network"].needsValue {
		t.Fatal("fixture did not release network")
	}
	keys := slices.Sorted(maps.Keys(model.branches))
	if _, err := model.sync(request{Keys: keys, Patch: jsontext.Value(`{"version":2}`)}); err == nil {
		t.Fatal("restored banks were admitted in project version 2")
	}
	if _, err := model.sync(request{Keys: keys, Patch: jsontext.Value(`{"version":1}`)}); err != nil {
		t.Fatal("version 1 repair failed", err)
	}
	if len(model.config.Network.Stations) != 1 || len(model.config.Network.Stations[0].Banks) != 2 {
		t.Fatal("released network lost its banks")
	}
}

func TestServiceChecksTrackSyncAndHistoryChanges(t *testing.T) {
	t.Parallel()
	model := new(engine)
	config := serviceEditorConfig(t)
	keys := synchronize(t, model, config)
	acceptedHistory(t, model, historyCommand{Kind: "reset", Background: jsontext.Value(`null`)})
	read := func(wantError bool) {
		t.Helper()
		report, err := model.draftChecks()
		if err != nil || (len(report.Errors) != 0) != wantError {
			t.Fatal("stale service checks", err, report.Errors)
		}
	}
	read(false)
	config.ExpressServices[0].PartyLimit = 0
	patch, err := json.Marshal(map[string]any{"expressServices": config.ExpressServices})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := model.sync(request{Keys: keys, Patch: patch}); err != nil {
		t.Fatal(err)
	}
	read(true)
	acceptedHistory(t, model, historyCommand{Kind: "replace", Background: jsontext.Value(`null`)})
	acceptedHistory(t, model, historyCommand{Kind: "undo"})
	read(false)
	acceptedHistory(t, model, historyCommand{Kind: "redo"})
	read(true)
}

func TestServiceNormalizationAndBankEditsPreserveVersion(t *testing.T) {
	t.Parallel()
	draft := serviceEditorDraft(t)
	before := cloneEditValue(draft)
	change, err := normalizeProject(draft)
	if err != nil {
		t.Fatal(err)
	}
	out := object(cloneEditValue(draft))
	maps.Copy(out, change.Patch)
	if number(out["version"]) != project.CurrentVersion || !reflect.DeepEqual(draft, before) || !reflect.DeepEqual(out["expressServices"], draft["expressServices"]) || !reflect.DeepEqual(member(items(out["fleet"])[0], "Class"), member(items(draft["fleet"])[0], "Class")) {
		t.Fatal("normalization downgraded or changed service metadata")
	}
	for _, version := range []float64{2, 3, 4, 5} {
		earlier := object(cloneEditValue(draft))
		earlier["version"] = version
		if _, err := normalizeProject(earlier); err == nil {
			t.Fatal("normalization accepted an earlier project version", version)
		}
	}
	banked := bankEditorFixture(1)
	station := items(member(banked["network"], "Stations"))[0]
	object(station)["VehicleClasses"] = []any{"legacy", "compact", "express"}
	for _, command := range []string{`{"action":"stationName","id":"station","value":"Renamed"}`, `{"action":"deleteStation","id":"station"}`} {
		edited := applyBankEdit(t, banked, command)
		if number(edited["version"]) != project.CurrentVersion {
			t.Fatal("bank edit changed the project version")
		}
		if len(items(member(edited["network"], "Stations"))) != 0 && !reflect.DeepEqual(member(items(member(edited["network"], "Stations"))[0], "VehicleClasses"), member(station, "VehicleClasses")) {
			t.Fatal("bank edit lost allowlist")
		}
	}
}

func TestServiceDraftChecksAndPhysicalGuards(t *testing.T) {
	t.Parallel()
	draft := serviceEditorDraft(t)
	if report := draftChecks(draft); len(report.Errors) != 0 {
		t.Fatal("valid service checks", report.Errors)
	}
	for _, class := range []string{"express", "unknown"} {
		bad := object(cloneEditValue(draft))
		object(items(bad["fleet"])[0])["Class"] = class
		if report := draftChecks(bad); len(report.Errors) == 0 {
			t.Fatalf("%s fleet profile accepted", class)
		}
		raw, _ := json.Marshal(bad)
		if result := Call(`{"op":"validate","project":` + string(raw) + `}`); !strings.Contains(result, `"error"`) {
			t.Fatal("unsupported class start accepted", result)
		}
	}
	bad := object(cloneEditValue(draft))
	bad["version"] = float64(3)
	if report := draftChecks(bad); len(report.Errors) == 0 {
		t.Fatal("service metadata accepted project version 3")
	}
	for _, replacement := range []any{nil, []any{}, []any{"compact", "compact"}, []any{"unknown"}, []any{float64(1)}} {
		bad := object(cloneEditValue(draft))
		object(items(member(bad["network"], "Lanes"))[0])["VehicleClasses"] = replacement
		if report := draftChecks(bad); len(report.Errors) == 0 {
			t.Fatal("invalid allowlist accepted", replacement)
		}
	}
}

func TestProjectPreservesOmittedClassesAndFleetCountDefaults(t *testing.T) {
	t.Parallel()
	config := project.Default()
	model := new(engine)
	synchronize(t, model, config)
	if result, err := model.handle(`{"op":"validate"}`); err != nil || !result.Valid {
		t.Fatal("a project needs no banks or service metadata", err)
	}
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	var draft map[string]any
	if decodeErr := json.Unmarshal(raw, &draft); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	change, err := normalizeProject(draft)
	if err != nil {
		t.Fatal(err)
	}
	out := object(cloneEditValue(draft))
	maps.Copy(out, change.Patch)
	if hasServiceMetadata(out) || number(out["version"]) != project.CurrentVersion {
		t.Fatal("normalization invented class or service defaults")
	}
	fleet := fleetDraft()
	object(items(fleet["fleet"])[0])["Class"] = "compact"
	changed, err := editProject(fleet, jsontext.Value(`{"field":"fleetCount","target":"alpha","value":2}`))
	if err != nil {
		t.Fatal(err)
	}
	var kept, added any
	for _, pod := range items(changed.Patch["fleet"]) {
		if member(pod, "ID") == "01" {
			kept = pod
		}
		if member(pod, "ID") == "03" {
			added = pod
		}
	}
	if member(kept, "Class") != "compact" || added == nil || has(added, "Class") {
		t.Fatal("count edit replaced compact metadata or invented a class")
	}
}

func TestServiceRequestContainerBounds(t *testing.T) {
	t.Parallel()
	for _, envelope := range []string{"project", "patch"} {
		for _, container := range []string{`"Lanes":[{"VehicleClasses":%s}]`, `"Stations":[{"VehicleClasses":%s}]`, `"Stations":[{"Berths":[{"VehicleClasses":%s}]}]`} {
			valid := strings.Replace(container, "%s", `["legacy","compact","group","express"]`, 1)
			if err := scanRequest([]byte(`{"` + envelope + `":{"network":{` + valid + `}}}`)); err != nil {
				t.Fatal("four classes rejected", err)
			}
			invalid := strings.Replace(container, "%s", `["legacy","compact","group","express","legacy"]`, 1)
			if err := scanRequest([]byte(`{"` + envelope + `":{"network":{` + invalid + `}}}`)); err == nil {
				t.Fatal("fifth class passed prescan")
			}
		}
		if err := scanRequest([]byte(`{"` + envelope + `":{"expressServices":[` + strings.Repeat(`{},`, 299) + `{}]}}`)); err != nil {
			t.Fatal("300 services rejected", err)
		}
		if err := scanRequest([]byte(`{"` + envelope + `":{"expressServices":[` + strings.Repeat(`{},`, 300) + `{}]}}`)); err == nil {
			t.Fatal("301 services passed prescan")
		}
	}
}
