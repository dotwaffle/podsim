package editormodel

import (
	"bytes"
	jsonv1 "encoding/json"
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
	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

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

// nativeVerdict decodes and validates a complete project with the server
// decoders. cmd/serve reads a project file with a json/v2 decoder that
// uses the v1 options and refuses unknown members. The session decodes the
// project command with session.Command. Both use the same options, and the
// two must agree.
func nativeVerdict(t *testing.T, raw []byte) error {
	t.Helper()
	_, err := serverDecode(t, raw)
	return err
}

func serverDecode(t *testing.T, raw []byte) (project.Config, error) {
	t.Helper()
	var config project.Config
	fileErr := json.UnmarshalRead(bytes.NewReader(raw), &config, jsonv1.DefaultOptionsV1(), json.MatchCaseInsensitiveNames(false), json.RejectUnknownMembers(true))
	if fileErr == nil {
		fileErr = project.Validate(config)
	}
	var command session.Command
	commandErr := json.UnmarshalRead(strings.NewReader(`{"action":"project","projectRevision":1,"project":`+string(raw)+`}`), &command, jsonv1.DefaultOptionsV1(), json.MatchCaseInsensitiveNames(false), json.RejectUnknownMembers(true))
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

func encodeDraft(t *testing.T, draft any) []byte {
	t.Helper()
	raw, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	return raw
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
	bases := map[string]any{"plain": project.Default(), "banks": bankEditorConfig(), "express": expressEditorConfig(t)}
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

// The editor model, the Call entry point, and the server decoders agree on
// each project, including the Express order marker.
func TestProjectVerdictsMatchNative(t *testing.T) {
	t.Parallel()
	plain := configDraft(t, project.Default())
	express := configDraft(t, expressEditorConfig(t))
	for _, test := range []struct {
		name   string
		base   map[string]any
		change func(map[string]any)
		valid  bool
	}{
		{"plain", plain, func(map[string]any) {}, true},
		{"banks", configDraft(t, bankEditorConfig()), func(map[string]any) {}, true},
		{"express order", express, func(map[string]any) {}, true},
		{"express order removed", express, func(draft map[string]any) { delete(draft, "orderContract") }, false},
		{"empty order contract", plain, func(draft map[string]any) { draft["orderContract"] = "" }, false},
		{"null order contract", plain, func(draft map[string]any) { draft["orderContract"] = nil }, false},
		{"unknown order contract", express, func(draft map[string]any) { draft["orderContract"] = "express-v2" }, false},
		{"unknown member", plain, func(draft map[string]any) { draft["extra"] = true }, false},
		{"version 6", plain, func(draft map[string]any) { draft["version"] = float64(6) }, false},
		{"version 4", express, func(draft map[string]any) { draft["version"] = float64(4) }, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			draft := object(cloneEditValue(test.base))
			test.change(draft)
			raw := encodeDraft(t, draft)
			native, model, call := nativeVerdict(t, raw), engineVerdict(raw), callVerdict(raw)
			if (native == nil) != test.valid || (model == nil) != test.valid || (call == nil) != test.valid {
				t.Fatalf("verdicts differ: native=%v model=%v call=%v", native, model, call)
			}
		})
	}
}

// A synchronized project has the fields of native decoding.
func TestSynchronizationMatchesNativeDecoding(t *testing.T) {
	t.Parallel()
	for name, config := range map[string]project.Config{"plain": project.Default(), "banks": bankEditorConfig(), "express": expressEditorConfig(t)} {
		raw := encodeDraft(t, config)
		model := new(engine)
		synchronize(t, model, config)
		var native project.Config
		if err := json.Unmarshal(raw, &native); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(model.config, native) {
			t.Fatal("synchronized project differs from native decoding", name)
		}
		if _, err := model.handle(`{"op":"validate"}`); err != nil {
			t.Fatal(name, err)
		}
		if err := callVerdict(raw); err != nil {
			t.Fatal(name, err)
		}
	}
}

// The lane class edit stores the classes in native order and refuses a
// malformed command.
func TestLaneClassesEdit(t *testing.T) {
	t.Parallel()
	draft := configDraft(t, project.Default())
	classes := func(draft map[string]any, id string) any {
		for _, lane := range items(member(draft["network"], "lanes")) {
			if member(lane, "id") == id {
				return member(lane, "vehicleClasses")
			}
		}
		return nil
	}
	edited := applyEdit(t, draft, `{"field":"geometry","value":{"action":"laneClasses","id":"approach-branch","value":["compact"]}}`)
	if !reflect.DeepEqual(classes(edited, "approach-branch"), []any{"compact"}) {
		t.Fatal("lane classes", classes(edited, "approach-branch"))
	}
	if err := nativeVerdict(t, encodeDraft(t, edited)); err != nil {
		t.Fatal(err)
	}
	reordered := applyEdit(t, draft, `{"field":"geometry","value":{"action":"laneClasses","id":"approach-branch","value":["express","legacy"]}}`)
	if !reflect.DeepEqual(classes(reordered, "approach-branch"), []any{"legacy", "express"}) {
		t.Fatal("lane class order", classes(reordered, "approach-branch"))
	}
	for _, value := range []string{`[]`, `["compact","compact"]`, `["future"]`, `"compact"`, `[1]`, `["legacy","compact","group","express","legacy"]`} {
		if _, err := editProject(draft, jsontext.Value(`{"field":"geometry","value":{"action":"laneClasses","id":"approach-branch","value":`+value+`}}`)); err == nil {
			t.Fatal("accepted lane classes", value)
		}
	}
	for _, command := range []string{
		`{"action":"laneClasses","id":"approach-branch","value":null}`,
		`{"action":"laneClasses","id":"approach-branch"}`,
		`{"action":"laneClasses","id":"missing","value":["compact"]}`,
	} {
		if _, err := editProject(draft, jsontext.Value(`{"field":"geometry","value":`+command+`}`)); err == nil {
			t.Fatal("accepted", command)
		}
	}
}

// applyEdit returns draft with the patch of command. It fails the test
// when the edit changes draft.
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
