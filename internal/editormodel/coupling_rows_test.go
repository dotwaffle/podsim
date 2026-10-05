package editormodel

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"maps"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
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

func encodeDraft(t *testing.T, draft any) []byte {
	t.Helper()
	raw, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// The conversion keeps every existing member. Each converted family passes
// the server decoders, project validation, and the session fleet constructor.
func TestConvertToTrainsKeepsTheProject(t *testing.T) {
	t.Parallel()
	queue := serviceEditorConfig(t)
	queue.StationQueueSpacing, queue.StationBuffers, queue.PlatoonLimit = sim.StationQueueOrdinary, true, 2
	for name, config := range map[string]project.Config{"plain": project.Default(), "banks": bankEditorConfig(), "service": serviceEditorConfig(t), "queue": queue, "express": expressEditorConfig(t)} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			draft := configDraft(t, config)
			converted := applyEdit(t, draft, `{"field":"convertToTrains","value":true}`)
			want := map[string]any{"version": float64(project.CurrentVersion), "couplingContract": "compact-pair-v1", "couplingEnabled": false, "couplingSites": []any{}, "couplingCorridors": []any{}}
			for key, value := range draft {
				if !reflect.DeepEqual(converted[key], value) {
					t.Fatal("conversion changed", key)
				}
			}
			for key, value := range want {
				if !reflect.DeepEqual(converted[key], value) {
					t.Fatal("conversion set", key, converted[key])
				}
			}
			native, err := serverDecode(t, encodeDraft(t, converted))
			if err != nil {
				t.Fatal(err)
			}
			if native.OrderContract != config.OrderContract || !reflect.DeepEqual(native.Network, config.Network) || !reflect.DeepEqual(native.Fleet, config.Fleet) || native.StationQueueSpacing != config.StationQueueSpacing {
				t.Fatal("conversion changed native fields")
			}
			contracts := sim.FleetContracts{OrderContract: native.OrderContract, CouplingContract: native.CouplingContract, CouplingEnabled: native.CouplingEnabled, CouplingSites: native.CouplingSites, CouplingCorridors: native.CouplingCorridors}
			if _, err := sim.NewFleetWithContracts(native.Network, native.Fleet, contracts); err != nil {
				t.Fatal(err)
			}
			// The native writer keeps the empty registries and omits the false option.
			written := string(encodeDraft(t, native))
			if !strings.Contains(written, `"couplingSites":[],"couplingCorridors":[]`) || strings.Contains(written, "couplingEnabled") {
				t.Fatal("native writer changed the converted members", written[:120])
			}
		})
	}
}

func TestConvertToTrainsRefusals(t *testing.T) {
	t.Parallel()
	unnamed := configDraft(t, project.Default())
	unnamed["name"] = ""
	member := configDraft(t, project.Default())
	member["couplingSites"] = []any{}
	for _, test := range []struct {
		name    string
		draft   map[string]any
		command string
		want    string
	}{
		{"invalid project", unnamed, `true`, "fix the project before it converts to trains"},
		{"coupling member", member, `true`, "only a project without coupling fields"},
		{"coupling project", configDraft(t, couplingEditorConfig(t, false)), `true`, "only a project without coupling fields"},
		{"false value", configDraft(t, project.Default()), `false`, "requires a true value"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := editProject(test.draft, jsontext.Value(`{"field":"convertToTrains","value":`+test.command+`}`))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("conversion error %v", err)
			}
		})
	}
	for _, version := range []float64{0, 2, 5, 6, 2.5} {
		draft := configDraft(t, project.Default())
		draft["version"] = version
		if _, err := editProject(draft, jsontext.Value(`{"field":"convertToTrains","value":true}`)); err == nil {
			t.Fatal("conversion accepted version", version)
		}
	}
}

// One undo restores the exact branches and bytes before the conversion. The
// worker validates the full demand profiles, not its cached selections.
func TestConvertToTrainsIsOneUndoStep(t *testing.T) {
	t.Parallel()
	config := expressEditorConfig(t)
	profile, demand, err := makeParkRide(config, validPlan())
	if err != nil {
		t.Fatal(err)
	}
	config.DemandProfiles, config.Demand = []project.DemandProfile{profile}, demand
	model := new(engine)
	keys := synchronize(t, model, config)
	before := maps.Clone(model.branches)
	acceptedHistory(t, model, historyCommand{Kind: "reset", Background: jsontext.Value(`null`)})
	if _, err = model.handle(`{"op":"checks"}`); err != nil {
		t.Fatal(err)
	}
	result, err := model.handle(`{"op":"edit","edit":{"field":"convertToTrains","value":true}}`)
	if err != nil {
		t.Fatal(err)
	}
	next := append(slices.Clone(keys), "couplingContract", "couplingEnabled", "couplingSites", "couplingCorridors")
	if _, err := model.sync(request{Keys: next, Patch: encodeDraft(t, result.Change.Patch)}); err != nil {
		t.Fatal(err)
	}
	acceptedHistory(t, model, historyCommand{Kind: "replace", Background: jsontext.Value(`null`)})
	if _, err := model.handle(`{"op":"validate"}`); err != nil || model.config.Version != project.CurrentVersion || model.config.OrderContract != sim.ExpressOrderContract {
		t.Fatal("converted project is not valid", err)
	}
	acceptedHistory(t, model, historyCommand{Kind: "undo"})
	if len(model.branches) != len(before) {
		t.Fatal("undo kept coupling branches")
	}
	for key, branch := range before {
		if !bytes.Equal(model.branches[key].raw, branch.raw) {
			t.Fatal("undo changed branch", key)
		}
	}
	acceptedHistory(t, model, historyCommand{Kind: "redo"})
	if model.config.Version != project.CurrentVersion || model.config.CouplingSites == nil {
		t.Fatal("redo lost the conversion")
	}
}

// The rows build the native coupling fixture from a converted project.
func TestCouplingRowsBuildValidGeometry(t *testing.T) {
	t.Parallel()
	draft := configDraft(t, couplingEditorConfig(t, false))
	delete(draft, "couplingSites")
	delete(draft, "couplingCorridors")
	report := func(draft map[string]any) []string {
		var texts []string
		for _, item := range draftChecks(draft).Errors {
			texts = append(texts, item.Text)
		}
		return texts
	}
	if checks := report(draft); len(checks) != 0 {
		t.Fatal("geometry-free project has errors", checks)
	}
	draft = applyEdit(t, draft, `{"field":"coupling","value":{"action":"addSite","laneId":"coupling-ab"}}`)
	room, err := sim.CouplingSiteRoom(sim.CompactPairV1CouplingContract)
	if err != nil {
		t.Fatal(err)
	}
	if _, present := draft["couplingCorridors"]; present {
		t.Fatal("a site edit added the corridor registry")
	}
	site := object(items(draft["couplingSites"])[0])
	if site["id"] != "site-1" || site["laneId"] != "coupling-ab" {
		t.Fatalf("site defaults %v", site)
	}
	// The fixture case runs through the Go and the JavaScript checks, and
	// both accept its site as native geometry.
	want := defaultSiteFixture(t)
	for _, field := range []string{"startMeters", "endMeters", "rearStagingMeters", "frontStagingMeters"} {
		if site[field] != want[field] {
			t.Fatalf("site default %s is %v, the fixture has %v", field, site[field], want[field])
		}
	}
	if end, ok := site["endMeters"].(float64); !ok || end-room.RequiredLengthMeters >= 0.01 {
		t.Fatalf("site defaults %v are longer than the room needs", site)
	}
	if checks := report(draft); len(checks) != 1 || !strings.Contains(checks[0], "invalid coupling geometry") {
		t.Fatal("one site did not report native geometry", checks)
	}
	draft = applyEdit(t, draft, `{"field":"coupling","value":{"action":"addSite","laneId":"coupling-ab"}}`)
	draft = applyEdit(t, draft, `{"field":"coupling","value":{"action":"setSiteLane","id":"site-2","laneId":"coupling-bc"}}`)
	for field, value := range map[string]string{"startMeters": `"50"`, "endMeters": "140", "rearStagingMeters": "70", "frontStagingMeters": " 82 "} {
		if field == "frontStagingMeters" {
			value = `"` + value + `"`
		}
		draft = applyEdit(t, draft, `{"field":"coupling","value":{"action":"setSite","id":"site-2","field":"`+field+`","value":`+value+`}}`)
	}
	draft = applyEdit(t, draft, `{"field":"coupling","value":{"action":"addCorridor"}}`)
	draft = applyEdit(t, draft, `{"field":"coupling","value":{"action":"addCorridorLane","id":"corridor-1","laneId":"coupling-bc"}}`)
	if checks := report(draft); len(checks) != 0 {
		t.Fatal("authored geometry has errors", checks)
	}
	if err := nativeVerdict(t, encodeDraft(t, draft)); err != nil {
		t.Fatal(err)
	}
	corridor := object(items(draft["couplingCorridors"])[0])
	if !reflect.DeepEqual(corridor, map[string]any{"id": "corridor-1", "assemblySiteId": "site-1", "splitSiteId": "site-2", "laneIds": []any{"coupling-ab", "coupling-bc"}}) {
		t.Fatalf("corridor %v", corridor)
	}
	if _, err := editProject(draft, jsontext.Value(`{"field":"coupling","value":{"action":"removeSite","id":"site-1"}}`)); err == nil {
		t.Fatal("removed a site that a corridor uses")
	}
	if _, err := editProject(draft, jsontext.Value(`{"field":"coupling","value":{"action":"removeCorridorLane","id":"corridor-1","index":1,"count":3,"laneId":"coupling-bc"}}`)); err == nil {
		t.Fatal("stale path edit accepted")
	}
	// A row that shows a different guideway at the index is stale.
	if _, err := editProject(draft, jsontext.Value(`{"field":"coupling","value":{"action":"removeCorridorLane","id":"corridor-1","index":1,"count":2,"laneId":"coupling-ab"}}`)); err == nil {
		t.Fatal("removed a guideway that the row does not show")
	}
	shorter := applyEdit(t, draft, `{"field":"coupling","value":{"action":"removeCorridorLane","id":"corridor-1","index":1,"count":2,"laneId":"coupling-bc"}}`)
	if checks := report(shorter); len(checks) != 1 {
		t.Fatal("a path that misses its split site has no error", checks)
	}
	draft = applyEdit(t, draft, `{"field":"coupling","value":{"action":"setCorridor","id":"corridor-1","field":"splitSiteId","value":"site-1"}}`)
	if checks := report(draft); len(checks) != 1 || nativeVerdict(t, encodeDraft(t, draft)) == nil {
		t.Fatal("a corridor with one site has no error", checks)
	}
	draft = applyEdit(t, draft, `{"field":"coupling","value":{"action":"removeCorridor","id":"corridor-1"}}`)
	draft = applyEdit(t, draft, `{"field":"coupling","value":{"action":"removeSite","id":"site-1"}}`)
	draft = applyEdit(t, draft, `{"field":"coupling","value":{"action":"removeSite","id":"site-2"}}`)
	if !reflect.DeepEqual(draft["couplingSites"], []any{}) || !reflect.DeepEqual(draft["couplingCorridors"], []any{}) || len(report(draft)) != 0 {
		t.Fatal("removing all rows did not return to a geometry-free project")
	}
}

func TestCouplingRowEditsRejectInvalidCommands(t *testing.T) {
	t.Parallel()
	draft := configDraft(t, couplingEditorConfig(t, false))
	older := configDraft(t, project.Default())
	for _, test := range []struct {
		name, command string
		draft         map[string]any
	}{
		{"version 1", `{"action":"addSite","laneId":"approach-branch"}`, older},
		{"unknown action", `{"action":"renameSite","id":"split"}`, draft},
		{"null field", `{"action":"removeSite","id":null}`, draft},
		{"missing field", `{"action":"setSite","id":"split","field":"startMeters"}`, draft},
		{"extra field", `{"action":"addCorridor","id":"x"}`, draft},
		{"unknown lane", `{"action":"addSite","laneId":"missing"}`, draft},
		{"unknown site", `{"action":"setSite","id":"missing","field":"startMeters","value":1}`, draft},
		{"unknown site field", `{"action":"setSite","id":"split","field":"laneId","value":"coupling-ab"}`, draft},
		{"empty position", `{"action":"setSite","id":"split","field":"startMeters","value":" "}`, draft},
		{"text position", `{"action":"setSite","id":"split","field":"startMeters","value":"far"}`, draft},
		{"unknown corridor site", `{"action":"setCorridor","id":"corridor","field":"splitSiteId","value":"missing"}`, draft},
		{"unknown corridor field", `{"action":"setCorridor","id":"corridor","field":"laneIds","value":"coupling-ab"}`, draft},
		{"negative lane index", `{"action":"removeCorridorLane","id":"corridor","index":-1,"count":2,"laneId":"coupling-ab"}`, draft},
		{"null lane index", `{"action":"removeCorridorLane","id":"corridor","index":null,"count":2,"laneId":"coupling-ab"}`, draft},
		{"missing lane index", `{"action":"removeCorridorLane","id":"corridor","count":2,"laneId":"coupling-ab"}`, draft},
		{"missing shown lane", `{"action":"removeCorridorLane","id":"corridor","index":0,"count":2}`, draft},
		{"not an object", `[]`, draft},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := editProject(test.draft, jsontext.Value(`{"field":"coupling","value":`+test.command+`}`)); err == nil {
				t.Fatal("accepted", test.command)
			}
		})
	}
	malformed := configDraft(t, couplingEditorConfig(t, false))
	malformed["couplingSites"] = nil
	if _, err := editProject(malformed, jsontext.Value(`{"field":"coupling","value":{"action":"addSite","laneId":"coupling-ab"}}`)); err == nil {
		t.Fatal("edited a null site registry")
	}
}

func TestCouplingRowCaps(t *testing.T) {
	t.Parallel()
	draft := configDraft(t, couplingEditorConfig(t, false))
	full := make([]any, sim.MaxCouplingSites)
	for i := range full {
		full[i] = map[string]any{"id": fmt.Sprint("s", i), "laneId": "coupling-ab"}
	}
	sites := object(cloneEditValue(draft))
	sites["couplingSites"] = full
	if _, err := editProject(sites, jsontext.Value(`{"field":"coupling","value":{"action":"addSite","laneId":"coupling-ab"}}`)); err == nil {
		t.Fatal("site cap")
	}
	corridors := object(cloneEditValue(draft))
	corridors["couplingCorridors"] = slices.Repeat([]any{map[string]any{"id": "c"}}, sim.MaxCouplingCorridors)
	if _, err := editProject(corridors, jsontext.Value(`{"field":"coupling","value":{"action":"addCorridor"}}`)); err == nil {
		t.Fatal("corridor cap")
	}
	path := object(cloneEditValue(draft))
	object(items(path["couplingCorridors"])[0])["laneIds"] = slices.Repeat([]any{"coupling-ab"}, project.MaxLanes)
	if _, err := editProject(path, jsontext.Value(`{"field":"coupling","value":{"action":"addCorridorLane","id":"corridor","laneId":"coupling-bc"}}`)); err == nil {
		t.Fatal("path cap")
	}
	one := object(cloneEditValue(draft))
	one["couplingSites"] = items(one["couplingSites"])[:1]
	if _, err := editProject(one, jsontext.Value(`{"field":"coupling","value":{"action":"addCorridor"}}`)); err == nil {
		t.Fatal("corridor without two sites")
	}
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

type decoderParityFixture struct {
	Bases map[string]string `json:"bases"`
	Cases []struct {
		Name      string `json:"name"`
		Base      string `json:"base"`
		Find      string `json:"find"`
		Replace   string `json:"replace"`
		Duplicate bool   `json:"duplicate"`
		Valid     bool   `json:"valid"`
	} `json:"cases"`
}

// The editor import accepts and decodes a project as the server decoders do.
// The browser rejects repeated names, which the Node tests check.
func TestImportMatchesServerDecoder(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("testdata/decoder_parity.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture decoderParityFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	bases := map[string]any{"plain": project.Default(), "banks": bankEditorConfig(), "express": expressEditorConfig(t), "coupling": couplingEditorConfig(t, false)}
	for name, config := range bases {
		if fixture.Bases[name] != string(encodeDraft(t, config)) {
			t.Fatal("decoder fixture base differs from the native writer", name)
		}
	}
	for _, item := range fixture.Cases {
		t.Run(item.Name, func(t *testing.T) {
			t.Parallel()
			base := fixture.Bases[item.Base]
			if !strings.Contains(base, item.Find) {
				t.Fatal("fixture text is missing")
			}
			text := strings.Replace(base, item.Find, item.Replace, 1)
			native, nativeErr := serverDecode(t, []byte(text))
			if (nativeErr == nil) != item.Valid {
				t.Fatalf("native verdict %v", nativeErr)
			}
			if item.Duplicate {
				if nativeErr == nil || !strings.Contains(nativeErr.Error(), "duplicate") {
					t.Fatalf("native duplicate verdict %v", nativeErr)
				}
				return
			}
			scenario := importedDraft(t, text)
			editorErr := engineVerdict(encodeDraft(t, scenario))
			if editorErr == nil {
				model := new(engine)
				if err := syncDraft(model, encodeDraft(t, scenario)); err != nil {
					t.Fatal(err)
				}
				if result, err := model.handle(`{"op":"checks"}`); err != nil || len(result.Checks.Errors) != 0 {
					editorErr = fmt.Errorf("checks %v: %w", result.Checks, err)
				}
			}
			if (editorErr == nil) != item.Valid {
				t.Fatalf("editor verdict %v, native %v", editorErr, nativeErr)
			}
			if item.Valid {
				var decoded project.Config
				if err := json.Unmarshal(encodeDraft(t, scenario), &decoded, json.RejectUnknownMembers(true)); err != nil || !reflect.DeepEqual(decoded, native) {
					t.Fatal("editor project differs from the server project", err)
				}
			}
		})
	}
}

// importedDraft follows the browser import, which keeps the names of the
// file as JSON.parse gives them.
func importedDraft(t *testing.T, text string) map[string]any {
	t.Helper()
	var draft map[string]any
	if err := json.Unmarshal([]byte(text), &draft); err != nil {
		t.Fatal(err)
	}
	return draft
}

// The server decoder matches names exactly, so the request scanner bounds
// the arrays of the exact names only. An array at a name with another case
// has no limit, so the scanner refuses it.
func TestRequestLimitsMatchExactProjectNames(t *testing.T) {
	t.Parallel()
	within := `{"op":"validate","project":{"network":{"nodes":[` + strings.Repeat(`{},`, project.MaxNodes-1) + `{}]}}}`
	if err := scanRequest([]byte(within)); err != nil {
		t.Fatal(err)
	}
	beyond := `{"op":"validate","project":{"network":{"nodes":[` + strings.Repeat(`{},`, project.MaxNodes) + `{}]}}}`
	if err := scanRequest([]byte(beyond)); err == nil {
		t.Fatal("accepted too many nodes")
	}
	for _, path := range []string{"network", "Network", "NETWORK", "networ\u212a"} {
		for _, nodes := range []string{"nodes", "Nodes", "NODES", "node\u017f", "No_des"} {
			if path == "network" && nodes == "nodes" {
				continue
			}
			if err := scanRequest([]byte(`{"op":"validate","project":{"` + path + `":{"` + nodes + `":[{}]}}}`)); err == nil {
				t.Fatal("bounded a name with another case", path, nodes)
			}
		}
	}
	if err := scanRequest([]byte(`{"op":"edit","edit":{"value":{"Value":[{}]}}}`)); err == nil {
		t.Fatal("bounded an editor command path with another case")
	}
}

// An Express file without the coupling marker can have station queue
// spacing. Native and the import verdict accept it.
func TestExpressQueueSpacingPassesTheVerdict(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"ordinary", "compact-v1"} {
		draft := configDraft(t, expressEditorConfig(t))
		draft["stationQueueSpacing"] = value
		draft["stationBuffers"], draft["platoonLimit"] = true, 2.0
		raw := encodeDraft(t, draft)
		if _, err := serverDecode(t, raw); err != nil {
			t.Fatal("native refused queue spacing with Express", value, err)
		}
		if err := engineVerdict(raw); err != nil {
			t.Fatal("editor verdict refused queue spacing with Express", value, err)
		}
	}
}

// A lane that the editor draws has no vehicle classes. The lane class edit
// makes it Compact-only, so the coupling rows can use it.
func TestLaneClassesMakeCouplingLanes(t *testing.T) {
	t.Parallel()
	draft := configDraft(t, couplingEditorConfig(t, false))
	delete(draft, "couplingSites")
	delete(draft, "couplingCorridors")
	for _, lane := range items(member(draft["network"], "lanes")) {
		delete(object(lane), "vehicleClasses")
	}
	classes := func(draft map[string]any, id string) any {
		for _, lane := range items(member(draft["network"], "lanes")) {
			if member(lane, "id") == id {
				return member(lane, "vehicleClasses")
			}
		}
		return nil
	}
	for _, command := range []string{
		`{"action":"addSite","laneId":"coupling-ab"}`,
		`{"action":"addSite","laneId":"coupling-bc"}`,
		`{"action":"setSite","id":"site-2","field":"startMeters","value":50}`,
		`{"action":"setSite","id":"site-2","field":"endMeters","value":140}`,
		`{"action":"setSite","id":"site-2","field":"rearStagingMeters","value":70}`,
		`{"action":"setSite","id":"site-2","field":"frontStagingMeters","value":82}`,
		`{"action":"addCorridor"}`,
		`{"action":"addCorridorLane","id":"corridor-1","laneId":"coupling-bc"}`,
	} {
		draft = applyEdit(t, draft, `{"field":"coupling","value":`+command+`}`)
	}
	if checks := draftChecks(draft).Errors; len(checks) != 1 || !strings.Contains(checks[0].Text, "explicitly Compact-only") {
		t.Fatal("sites on unclassed lanes have no class error", checks)
	}
	for _, id := range []string{"coupling-ab", "coupling-bc"} {
		draft = applyEdit(t, draft, `{"field":"geometry","value":{"action":"laneClasses","id":"`+id+`","value":["compact"]}}`)
		if !reflect.DeepEqual(classes(draft, id), []any{"compact"}) {
			t.Fatal("lane classes", classes(draft, id))
		}
	}
	if checks := draftChecks(draft).Errors; len(checks) != 0 {
		t.Fatal("Compact-only lanes still have errors", checks)
	}
	if err := nativeVerdict(t, encodeDraft(t, draft)); err != nil {
		t.Fatal(err)
	}
	// The lane gets the classes in native order.
	reordered := applyEdit(t, draft, `{"field":"geometry","value":{"action":"laneClasses","id":"approach-branch","value":["express","legacy"]}}`)
	if !reflect.DeepEqual(classes(reordered, "approach-branch"), []any{"legacy", "express"}) {
		t.Fatal("lane class order", classes(reordered, "approach-branch"))
	}
	for _, value := range []string{`[]`, `["compact","compact"]`, `["future"]`, `"compact"`, `[1]`, `["legacy","compact","group","express","legacy"]`} {
		if _, err := editProject(draft, jsontext.Value(`{"field":"geometry","value":{"action":"laneClasses","id":"coupling-ab","value":`+value+`}}`)); err == nil {
			t.Fatal("accepted lane classes", value)
		}
	}
	for _, command := range []string{
		`{"action":"laneClasses","id":"coupling-ab","value":null}`,
		`{"action":"laneClasses","id":"coupling-ab"}`,
		`{"action":"laneClasses","id":"missing","value":["compact"]}`,
	} {
		if _, err := editProject(draft, jsontext.Value(`{"field":"geometry","value":`+command+`}`)); err == nil {
			t.Fatal("accepted", command)
		}
	}
}

// Native validation accepts lane classes on each project. The edit does not
// change the version.
func TestLaneClassesKeepVersion(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		draft map[string]any
		lane  string
	}{
		{configDraft(t, project.Default()), "approach-branch"},
		{configDraft(t, bankEditorConfig()), "a-merge"},
		{configDraft(t, expressEditorConfig(t)), "approach-branch"},
	} {
		change, err := editProject(test.draft, jsontext.Value(`{"field":"geometry","value":{"action":"laneClasses","id":"`+test.lane+`","value":["compact"]}}`))
		if err != nil {
			t.Fatal(test.lane, err)
		}
		if _, changed := change.Patch["version"]; changed || len(change.Patch) != 1 {
			t.Fatal("lane class edit changed more than the network", change.Patch)
		}
	}
}

// Two clicks on one stale Remove button send the same command. The second
// command has the count of the path that the row showed, so Go refuses it.
func TestCorridorLaneRemoveRefusesAStaleRow(t *testing.T) {
	t.Parallel()
	draft := configDraft(t, couplingEditorConfig(t, false))
	corridor := object(items(draft["couplingCorridors"])[0])
	corridor["laneIds"] = []any{"coupling-ab", "coupling-ab", "coupling-bc"}
	command := `{"field":"coupling","value":{"action":"removeCorridorLane","id":"corridor","index":0,"count":3,"laneId":"coupling-ab"}}`
	once := applyEdit(t, draft, command)
	if got := member(items(once["couplingCorridors"])[0], "laneIds"); !reflect.DeepEqual(got, []any{"coupling-ab", "coupling-bc"}) {
		t.Fatal("path after one remove", got)
	}
	if _, err := editProject(once, jsontext.Value(command)); err == nil {
		t.Fatal("the second command from a stale row removed a guideway")
	}
}

// defaultSiteFixture gives the first site of the "coupling geometry default
// site" case in testdata/service_checks.json, which has the positions of a
// new site.
func defaultSiteFixture(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile("testdata/service_checks.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture serviceCheckFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, item := range fixture.Cases {
		if item.Name != "coupling geometry default site" {
			continue
		}
		if len(item.Checks.Errors) != 0 {
			t.Fatal("the default site fixture has errors", item.Checks.Errors)
		}
		for _, edit := range item.Changes {
			if slices.Equal(edit.Path, []string{"couplingSites"}) {
				return object(items(edit.Value)[0])
			}
		}
	}
	t.Fatal("no default site fixture")
	return nil
}
