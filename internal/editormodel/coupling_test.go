package editormodel

import (
	"bytes"
	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

// couplingEditorConfig adds the native coupling geometry fixture to a valid project.
func couplingEditorConfig(t *testing.T, express bool) project.Config {
	t.Helper()
	config := project.Default()
	if express {
		config = expressEditorConfig(t)
	}
	config.CouplingContract = sim.CompactPairV1CouplingContract
	config.CouplingEnabled = true
	compact, err := sim.NewClassSet("compact")
	if err != nil {
		t.Fatal(err)
	}
	config.Network.Nodes = append(config.Network.Nodes,
		sim.Node{ID: "coupling-a", Position: sim.Point{Y: 1000}},
		sim.Node{ID: "coupling-b", Position: sim.Point{X: 200, Y: 1000}},
		sim.Node{ID: "coupling-c", Position: sim.Point{X: 400, Y: 1000}},
	)
	config.Network.Lanes = append(config.Network.Lanes,
		sim.Lane{ID: "coupling-ab", From: "coupling-a", To: "coupling-b", SpeedLimit: 14, VehicleClasses: compact},
		sim.Lane{ID: "coupling-bc", From: "coupling-b", To: "coupling-c", SpeedLimit: 7, VehicleClasses: compact},
	)
	config.CouplingSites = []sim.CouplingSite{
		{ID: "split", LaneID: "coupling-bc", StartMeters: 50, EndMeters: 140, RearStagingMeters: 70, FrontStagingMeters: 82},
		{ID: "assembly", LaneID: "coupling-ab", StartMeters: 20, EndMeters: 100, RearStagingMeters: 40, FrontStagingMeters: 52},
	}
	config.CouplingCorridors = []sim.CouplingCorridor{{ID: "corridor", AssemblySiteID: "assembly", SplitSiteID: "split", LaneIDs: []string{"coupling-ab", "coupling-bc"}}}
	if err := project.Validate(config); err != nil {
		t.Fatal(err)
	}
	return config
}

func bankEditorConfig() project.Config {
	config := project.Default()
	config.Network = sim.BankExample()
	config.Fleet = []sim.Placement{{ID: "01", StationID: "origin", BerthID: "origin-1"}}
	return config
}

func configDraft(t *testing.T, config project.Config) map[string]any {
	t.Helper()
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	var draft map[string]any
	if err := json.Unmarshal(raw, &draft); err != nil {
		t.Fatal(err)
	}
	return draft
}

func couplingMembers(draft map[string]any) map[string]any {
	members := make(map[string]any)
	for _, key := range couplingKeys {
		if value, present := draft[key]; present {
			members[key] = value
		}
	}
	return members
}

// nativeVerdict decodes and validates a complete project with the server
// decoders. cmd/serve reads a project file with an encoding/json Decoder that
// disallows unknown fields. The session decodes the project command with
// session.Command. Both use encoding/json options, and the two must agree.
func nativeVerdict(t *testing.T, raw []byte) error {
	t.Helper()
	_, err := serverDecode(t, raw)
	return err
}

func serverDecode(t *testing.T, raw []byte) (project.Config, error) {
	t.Helper()
	file := jsonv1.NewDecoder(bytes.NewReader(raw))
	file.DisallowUnknownFields()
	var config project.Config
	fileErr := file.Decode(&config)
	if fileErr == nil {
		if file.Decode(new(any)) != io.EOF {
			fileErr = errors.New("expected one JSON value")
		} else {
			fileErr = project.Validate(config)
		}
	}
	body := jsonv1.NewDecoder(strings.NewReader(`{"action":"project","projectRevision":1,"project":` + string(raw) + `}`))
	body.DisallowUnknownFields()
	var command session.Command
	commandErr := body.Decode(&command)
	if commandErr == nil {
		commandErr = project.Validate(*command.Project)
	}
	if (fileErr == nil) != (commandErr == nil) || fileErr == nil && !reflect.DeepEqual(config, *command.Project) {
		t.Fatalf("server decoders disagree: file=%v command=%v", fileErr, commandErr)
	}
	return config, fileErr
}

// syncDraft sends a complete draft through the bounded worker request path.
func syncDraft(model *engine, raw []byte) error {
	var fields map[string]jsontext.Value
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	keys, err := json.Marshal(slices.Sorted(maps.Keys(fields)))
	if err != nil {
		return err
	}
	_, err = model.handle(`{"op":"sync","keys":` + string(keys) + `,"patch":` + string(raw) + `}`)
	return err
}

// engineVerdict synchronizes a complete draft and validates it as the editor worker does.
func engineVerdict(raw []byte) error {
	model := new(engine)
	if err := syncDraft(model, raw); err != nil {
		return err
	}
	_, err := model.handle(`{"op":"validate"}`)
	return err
}

func callVerdict(raw []byte) error {
	var result response
	if err := json.Unmarshal([]byte(Call(`{"op":"validate","project":`+string(raw)+`}`)), &result); err != nil {
		return err
	}
	if result.Error != "" {
		return fmt.Errorf("%s", result.Error)
	}
	return nil
}

// The web tests import this fixture, so it must stay a valid native project.
func TestCouplingEditorFixtureIsNative(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("testdata/coupling_project.json")
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.Marshal(couplingEditorConfig(t, false))
	if err != nil {
		t.Fatal(err)
	}
	fixture := jsontext.Value(slices.Clone(raw))
	if err := fixture.Compact(); err != nil || !bytes.Equal(fixture, want) {
		t.Fatal("coupling fixture differs from the native writer", err)
	}
	if err := nativeVerdict(t, raw); err != nil {
		t.Fatal(err)
	}
}

func TestCouplingEditorNormalizationPreservesMembers(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		express bool
		change  func(map[string]any)
	}{
		{"enabled", false, func(map[string]any) {}},
		{"explicit off", false, func(draft map[string]any) { draft["couplingEnabled"] = false }},
		{"omitted option", false, func(draft map[string]any) { delete(draft, "couplingEnabled") }},
		{"geometry free", false, func(draft map[string]any) { delete(draft, "couplingSites"); delete(draft, "couplingCorridors") }},
		{"express order", true, func(map[string]any) {}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			draft := configDraft(t, couplingEditorConfig(t, test.express))
			test.change(draft)
			before := cloneEditValue(draft)
			change, err := normalizeProject(draft)
			if err != nil {
				t.Fatal(err)
			}
			for _, key := range append([]string{"version", "orderContract"}, couplingKeys...) {
				if _, replaced := change.Patch[key]; replaced {
					t.Fatal("normalization replaced", key)
				}
			}
			out := object(cloneEditValue(draft))
			maps.Copy(out, change.Patch)
			if !reflect.DeepEqual(couplingMembers(out), couplingMembers(object(before))) || !reflect.DeepEqual(draft, before) {
				t.Fatal("normalization changed coupling members or its source")
			}
			raw, err := json.Marshal(draft)
			if err != nil {
				t.Fatal(err)
			}
			if err = engineVerdict(raw); err != nil {
				t.Fatal(err)
			}
			model := new(engine)
			if err = syncDraft(model, raw); err != nil {
				t.Fatal(err)
			}
			const command = `{"field":"normalize","value":true}`
			cached, err := model.handle(`{"op":"edit","edit":` + command + `}`)
			if err != nil || !reflect.DeepEqual(cached.Change.Patch, change.Patch) {
				t.Fatal("cached normalization differs", err)
			}
			var explicit response
			if err := json.Unmarshal([]byte(Call(`{"op":"edit","project":`+string(raw)+`,"edit":`+command+`}`)), &explicit); err != nil || explicit.Error != "" || !reflect.DeepEqual(explicit.Change.Patch, change.Patch) {
				t.Fatal("explicit normalization differs", err, explicit.Error)
			}
		})
	}
}

func TestCouplingEditorSynchronizationMatchesNative(t *testing.T) {
	t.Parallel()
	for _, express := range []bool{false, true} {
		config := couplingEditorConfig(t, express)
		raw, err := json.Marshal(config)
		if err != nil {
			t.Fatal(err)
		}
		model := new(engine)
		synchronize(t, model, config)
		var native project.Config
		if err := json.Unmarshal(raw, &native); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(model.config, native) {
			t.Fatal("synchronized project differs from native decoding", express)
		}
		if _, err := model.handle(`{"op":"validate"}`); err != nil {
			t.Fatal(err)
		}
		if err := callVerdict(raw); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCouplingEditorQueueEditsByMarker(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name              string
		express, coupling bool
		refused           bool
	}{
		{"no markers", false, false, false},
		{"coupling", false, true, false},
		// Native refuses station queue spacing with Express but without coupling.
		{"express", true, false, true},
		{"express and coupling", true, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			draft := configDraft(t, couplingEditorConfig(t, test.express))
			if !test.coupling {
				for _, key := range couplingKeys {
					delete(draft, key)
				}
			}
			before := cloneEditValue(draft)
			change, err := editProject(draft, jsontext.Value(`{"field":"stationQueueSpacing","value":"ordinary"}`))
			if test.refused {
				if err == nil {
					t.Fatal("queue edit accepted", change.Patch)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]any{"stationQueueSpacing": "ordinary"}
			if !reflect.DeepEqual(change.Patch, want) {
				t.Fatalf("queue edit patch %v", change.Patch)
			}
			if !reflect.DeepEqual(draft, before) {
				t.Fatal("queue edit changed its source")
			}
		})
	}
	model := new(engine)
	keys := synchronize(t, model, couplingEditorConfig(t, false))
	result, err := model.handle(`{"op":"edit","edit":{"field":"stationQueueSpacing","value":"ordinary"}}`)
	if err != nil || !reflect.DeepEqual(result.Change.Patch, map[string]any{"stationQueueSpacing": "ordinary"}) {
		t.Fatal("queue edit changed more than its setting", err)
	}
	patch, err := json.Marshal(result.Change.Patch)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := model.sync(request{Keys: append(keys, "stationQueueSpacing"), Patch: patch}); err != nil {
		t.Fatal(err)
	}
	if model.config.Version != project.CurrentVersion || model.config.CouplingContract != sim.CompactPairV1CouplingContract || len(model.config.CouplingSites) != 2 {
		t.Fatal("queue edit dropped the coupling project")
	}
}

func TestCouplingEditorBankEditsKeepVersion(t *testing.T) {
	t.Parallel()
	draft := bankEditorFixture(1)
	network := object(draft["network"])
	station := object(items(network["stations"])[0])
	network["stations"] = []any{station}
	station["berths"] = items(station["berths"])[:1]
	station["banks"] = items(station["banks"])[:1]
	network["nodes"] = slices.DeleteFunc(items(network["nodes"]), func(node any) bool { return len(text(member(node, "id"))) > 2 && text(member(node, "id"))[:2] == "b-" })
	network["lanes"] = slices.DeleteFunc(items(network["lanes"]), func(lane any) bool { return len(text(member(lane, "id"))) > 2 && text(member(lane, "id"))[:2] == "b-" })
	draft["version"] = float64(project.CurrentVersion)
	draft["couplingContract"] = string(sim.CompactPairV1CouplingContract)
	banks, err := json.Marshal(station["banks"])
	if err != nil {
		t.Fatal(err)
	}
	legacy := applyBankEdit(t, draft, `{"action":"stationLegacy","id":"station"}`)
	banked := applyBankEdit(t, legacy, `{"action":"stationBanks","id":"station","value":`+string(banks)+`}`)
	for _, edited := range []map[string]any{legacy, banked} {
		if edited["version"] != float64(project.CurrentVersion) || edited["couplingContract"] != draft["couplingContract"] {
			t.Fatal("bank edit changed the coupling project version")
		}
	}
}

func TestCouplingEditorTrainOption(t *testing.T) {
	t.Parallel()
	config := couplingEditorConfig(t, false)
	model := new(engine)
	keys := synchronize(t, model, config)
	sites, corridors := slices.Clone(model.branches["couplingSites"].raw), slices.Clone(model.branches["couplingCorridors"].raw)
	for _, enabled := range []bool{false, true, false} {
		result, err := model.handle(fmt.Sprintf(`{"op":"edit","edit":{"field":"couplingEnabled","value":%t}}`, enabled))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(result.Change.Patch, map[string]any{"couplingEnabled": enabled}) || result.Change.Flag != "" {
			t.Fatalf("train option patch %v", result.Change.Patch)
		}
		patch, err := json.Marshal(result.Change.Patch)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := model.sync(request{Keys: keys, Patch: patch}); err != nil {
			t.Fatal(err)
		}
		if model.config.CouplingEnabled != enabled || model.config.CouplingContract != sim.CompactPairV1CouplingContract ||
			!bytes.Equal(model.branches["couplingSites"].raw, sites) || !bytes.Equal(model.branches["couplingCorridors"].raw, corridors) {
			t.Fatal("train option changed more than its flag", enabled)
		}
		if _, err := model.handle(`{"op":"validate"}`); err != nil {
			t.Fatal(err)
		}
	}
	draft := configDraft(t, config)
	for _, value := range []string{`"false"`, `0`, `1`} {
		if _, err := editProject(draft, jsontext.Value(`{"field":"couplingEnabled","value":`+value+`}`)); err == nil {
			t.Fatal("train option accepted a non-Boolean value", value)
		}
	}
	for _, version := range []float64{1, 2, 3, 4, 6} {
		older := configDraft(t, project.Default())
		older["version"] = version
		if _, err := editProject(older, jsontext.Value(`{"field":"couplingEnabled","value":true}`)); err == nil {
			t.Fatal("train option migrated an older project", version)
		}
	}
}

func TestCouplingEditorChecksFollowCouplingBranches(t *testing.T) {
	t.Parallel()
	const problem = "The train setting must be true or false."
	model := new(engine)
	keys := synchronize(t, model, couplingEditorConfig(t, false))
	reported := func() bool {
		t.Helper()
		result, err := model.handle(`{"op":"checks"}`)
		if err != nil {
			t.Fatal(err)
		}
		return slices.ContainsFunc(result.Checks.Errors, func(item check) bool { return item.Text == problem })
	}
	acceptedHistory(t, model, historyCommand{Kind: "reset", Background: jsontext.Value(`null`)})
	if reported() {
		t.Fatal("valid train option reported")
	}
	if _, err := model.sync(request{Keys: keys, Patch: jsontext.Value(`{"couplingEnabled":null}`)}); err == nil {
		t.Fatal("synchronization accepted a null train option")
	}
	if !reported() {
		t.Fatal("cached checks missed the changed train option")
	}
	acceptedHistory(t, model, historyCommand{Kind: "replace", Background: jsontext.Value(`null`)})
	acceptedHistory(t, model, historyCommand{Kind: "undo"})
	if reported() {
		t.Fatal("cached checks kept the undone train option")
	}
}

func TestCouplingEditorHistoryPreservesMembers(t *testing.T) {
	t.Parallel()
	config := couplingEditorConfig(t, true)
	model := new(engine)
	keys := synchronize(t, model, config)
	original := make(map[string]jsontext.Value)
	for _, key := range couplingKeys {
		original[key] = slices.Clone(model.branches[key].raw)
	}
	acceptedHistory(t, model, historyCommand{Kind: "reset", Background: jsontext.Value(`null`)})
	queueKeys := append(slices.Clone(keys), "stationQueueSpacing")
	for _, edit := range []struct {
		keys  []string
		patch string
	}{{keys, `{"name":"Edited coupling project"}`}, {keys, `{"couplingEnabled":false}`}, {queueKeys, `{"stationQueueSpacing":"ordinary"}`}} {
		if _, err := model.sync(request{Keys: edit.keys, Patch: jsontext.Value(edit.patch)}); err != nil {
			t.Fatal(err)
		}
		acceptedHistory(t, model, historyCommand{Kind: "replace", Background: jsontext.Value(`null`)})
	}
	trim := uint64(1)
	view := historyRequest(t, model, historyCommand{Action: "prepare", Kind: "replace", Revision: strconv.FormatUint(model.timeline.revision, 10), Background: jsontext.Value(`{"imageKey":"a"}`), TrimOldest: &trim})
	historyRequest(t, model, historyCommand{Action: "accept", Token: view.Proposal})
	check := func(step string, enabled bool) {
		t.Helper()
		if model.config.Version != project.CurrentVersion || model.config.CouplingEnabled != enabled || model.config.OrderContract != sim.ExpressOrderContract ||
			!reflect.DeepEqual(model.config.CouplingSites, config.CouplingSites) || !reflect.DeepEqual(model.config.CouplingCorridors, config.CouplingCorridors) {
			t.Fatal("history changed the coupling project", step)
		}
		for _, key := range []string{"couplingContract", "couplingSites", "couplingCorridors"} {
			if !bytes.Equal(model.branches[key].raw, original[key]) {
				t.Fatal("history changed coupling bytes", step, key)
			}
		}
		if _, err := model.handle(`{"op":"validate"}`); err != nil {
			t.Fatal(step, err)
		}
	}
	check("trim", false)
	for _, step := range []struct {
		kind    string
		enabled bool
	}{{"undo", false}, {"undo", false}, {"undo", true}, {"redo", false}, {"redo", false}, {"redo", false}} {
		acceptedHistory(t, model, historyCommand{Kind: step.kind})
		check(step.kind, step.enabled)
	}
	if view := historyMetadata(model.timeline.state, model.timeline.revision, false); view.CanRedo || len(view.Retained) != 4 {
		t.Fatalf("trimmed history retained %v", view.Retained)
	}
}

// Each coupling member needs the coupling marker. The marker alone is valid.
func TestCouplingEditorRejectsMembersWithoutMarker(t *testing.T) {
	t.Parallel()
	bases := map[string]project.Config{"plain": project.Default(), "banks": bankEditorConfig(), "service": serviceEditorConfig(t), "express": expressEditorConfig(t)}
	members := []struct{ key, value string }{
		{"couplingContract", `"compact-pair-v1"`}, {"couplingContract", `null`}, {"couplingContract", `""`},
		{"couplingEnabled", `false`}, {"couplingEnabled", `true`}, {"couplingEnabled", `null`},
		{"couplingSites", `[]`}, {"couplingSites", `null`},
		{"couplingCorridors", `[]`}, {"couplingCorridors", `null`},
	}
	for name, config := range bases {
		base, err := json.Marshal(config)
		if err != nil {
			t.Fatal(err)
		}
		if err := nativeVerdict(t, base); err != nil {
			t.Fatal("base project is invalid", name, err)
		}
		for _, item := range members {
			t.Run(fmt.Sprintf("%s %s %s", name, item.key, item.value), func(t *testing.T) {
				t.Parallel()
				raw := append(bytes.TrimSuffix(slices.Clone(base), []byte("}")), []byte(`,"`+item.key+`":`+item.value+`}`)...)
				if item.key == "couplingContract" && item.value == `"compact-pair-v1"` {
					if err := errors.Join(nativeVerdict(t, raw), engineVerdict(raw), callVerdict(raw)); err != nil {
						t.Fatal("the marker alone was refused", err)
					}
					return
				}
				if nativeVerdict(t, raw) == nil || engineVerdict(raw) == nil || callVerdict(raw) == nil {
					t.Fatal("a project without the marker accepted a coupling member")
				}
				var draft map[string]any
				if err := json.Unmarshal(raw, &draft); err != nil {
					t.Fatal(err)
				}
				if _, err := normalizeProject(draft); err == nil {
					t.Fatal("normalization accepted a coupling member")
				}
				if checks := draftChecks(draft); !slices.ContainsFunc(checks.Errors, func(item check) bool { return item.Text == "Coupling fields require couplingContract compact-pair-v1." }) {
					t.Fatalf("checks missed the coupling member: %v", checks.Errors)
				}
			})
		}
	}
}

func TestCouplingEditorNativeParity(t *testing.T) {
	t.Parallel()
	valid := configDraft(t, couplingEditorConfig(t, false))
	express := configDraft(t, couplingEditorConfig(t, true))
	for _, test := range []struct {
		name   string
		base   map[string]any
		change func(map[string]any)
		valid  bool
	}{
		{"enabled", valid, func(map[string]any) {}, true},
		{"explicit off", valid, func(draft map[string]any) { draft["couplingEnabled"] = false }, true},
		{"omitted option", valid, func(draft map[string]any) { delete(draft, "couplingEnabled") }, true},
		{"geometry free", valid, func(draft map[string]any) { delete(draft, "couplingSites"); delete(draft, "couplingCorridors") }, true},
		{"empty registries", valid, func(draft map[string]any) { draft["couplingSites"], draft["couplingCorridors"] = []any{}, []any{} }, true},
		{"express order", express, func(map[string]any) {}, true},
		{"express order removed", express, func(draft map[string]any) { delete(draft, "orderContract") }, false},
		{"marker omitted", valid, func(draft map[string]any) { delete(draft, "couplingContract") }, false},
		{"marker unknown", valid, func(draft map[string]any) { draft["couplingContract"] = "compact-pair-v2" }, false},
		{"marker null", valid, func(draft map[string]any) { draft["couplingContract"] = nil }, false},
		{"option null", valid, func(draft map[string]any) { draft["couplingEnabled"] = nil }, false},
		{"option text", valid, func(draft map[string]any) { draft["couplingEnabled"] = "true" }, false},
		{"sites null", valid, func(draft map[string]any) { draft["couplingSites"] = nil }, false},
		{"corridors null", valid, func(draft map[string]any) { draft["couplingCorridors"] = nil }, false},
		{"one-sided sites", valid, func(draft map[string]any) { delete(draft, "couplingCorridors") }, false},
		{"unknown site lane", valid, func(draft map[string]any) { object(items(draft["couplingSites"])[0])["laneId"] = "missing" }, false},
		{"unknown corridor site", valid, func(draft map[string]any) { object(items(draft["couplingCorridors"])[0])["splitSiteId"] = "missing" }, false},
		{"unknown record member", valid, func(draft map[string]any) { object(items(draft["couplingSites"])[0])["extra"] = true }, false},
		{"empty order contract", valid, func(draft map[string]any) { draft["orderContract"] = "" }, false},
		{"null order contract", valid, func(draft map[string]any) { draft["orderContract"] = nil }, false},
		{"version 6", valid, func(draft map[string]any) { draft["version"] = float64(6) }, false},
		{"version 4", express, func(draft map[string]any) { draft["version"] = float64(4) }, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			draft := object(cloneEditValue(test.base))
			test.change(draft)
			raw, err := json.Marshal(draft)
			if err != nil {
				t.Fatal(err)
			}
			native, model, call := nativeVerdict(t, raw), engineVerdict(raw), callVerdict(raw)
			if (native == nil) != test.valid || (model == nil) != test.valid || (call == nil) != test.valid {
				t.Fatalf("verdicts differ: native=%v model=%v call=%v", native, model, call)
			}
		})
	}
}
