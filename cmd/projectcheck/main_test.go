package main

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

// sessionVerdict decodes raw as the session project command does, which is
// the second server path that reads a project.
func sessionVerdict(raw []byte) error {
	var command session.Command
	if err := command.UnmarshalJSON([]byte(`{"action":"project","projectRevision":1,"project":` + string(raw) + `}`)); err != nil {
		return err
	}
	if command.Project == nil {
		return errors.New("the command has no project")
	}
	return project.Validate(*command.Project)
}

func projectData(t *testing.T, config project.Config) []byte {
	t.Helper()
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func projectFile(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "project.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func groupProject(t *testing.T) project.Config {
	t.Helper()
	data, err := os.ReadFile("../../internal/project/testdata/group_public.json")
	if err != nil {
		t.Fatal(err)
	}
	config, err := decodeProject(data)
	if err != nil {
		t.Fatal(err)
	}
	return config
}

func TestProjectCompatibility(t *testing.T) {
	t.Parallel()
	banked := project.Default()
	banked.Network = sim.BankExample()
	banked.Fleet = []sim.Placement{{ID: "01", StationID: "origin", BerthID: "origin-1"}}
	compact := project.Default()
	compact.Fleet[0].Class = sim.CompactClass
	for _, test := range []struct {
		name   string
		config project.Config
	}{
		{"default project", project.Default()},
		{"station banks", banked},
		{"compact class", compact},
		{"group class", groupProject(t)},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := project.Validate(test.config); err != nil {
				t.Fatal("invalid compatibility fixture", err)
			}
			data := projectData(t, test.config)
			path := projectFile(t, data)
			var output bytes.Buffer
			if err := command([]string{"-project", path}, &output); err != nil {
				t.Fatal(err)
			}
			for _, text := range []string{"Valid project", test.config.Name, "stations", "lanes", "pods", "Static project checks only. Simulation not advanced."} {
				if !strings.Contains(output.String(), text) {
					t.Fatalf("summary omitted %q: %s", text, &output)
				}
			}
			unchanged, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(unchanged, data) {
				t.Fatal("validation changed input", err)
			}
		})
	}
}

func TestNativeValidationCaller(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		config project.Config
		change func(*project.Config)
	}{
		{"empty fleet", project.Default(), func(config *project.Config) { config.Fleet = nil }},
		{"unsupported physical class", groupProject(t), func(config *project.Config) { config.Fleet[0].Class = sim.ExpressClass }},
		{"large short lane", groupProject(t), func(config *project.Config) {
			config.Network.Nodes = append(config.Network.Nodes,
				sim.Node{ID: "short-a", Position: sim.Point{X: 2000, Y: 2000}},
				sim.Node{ID: "short-b", Position: sim.Point{X: 2039.999, Y: 2000}})
			config.Network.Lanes = append(config.Network.Lanes, sim.Lane{ID: "short", From: "short-a", To: "short-b", SpeedLimit: 14, VehicleClasses: config.Network.Lanes[0].VehicleClasses})
		}},
		{"unreachable passenger route", project.Default(), func(config *project.Config) {
			config.Network.Lanes = slices.DeleteFunc(config.Network.Lanes, func(lane sim.Lane) bool { return lane.From == config.Network.Stations[0].Exit })
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config := project.Clone(test.config)
			test.change(&config)
			data := projectData(t, config)
			decoded, err := decodeProject(data)
			if err != nil {
				t.Fatal("fixture must reach native validation", err)
			}
			if project.Validate(decoded) == nil {
				t.Fatal("native validator accepted invalid fixture")
			}
			var output bytes.Buffer
			err = command([]string{"-project", projectFile(t, data)}, &output)
			if err == nil || !strings.Contains(err.Error(), "validate project:") {
				t.Fatalf("invalid project bypassed native validation: output=%q error=%v", &output, err)
			}
			if output.Len() != 0 {
				t.Fatalf("invalid project produced summary: %q", &output)
			}
		})
	}
}

func TestRawProjectRules(t *testing.T) {
	t.Parallel()
	current := project.Default()
	base := string(projectData(t, current))
	fleetData, err := json.Marshal(current.Fleet)
	if err != nil {
		t.Fatal(err)
	}
	add := func(member string) string { return strings.TrimSuffix(base, "}") + "," + member + "}" }
	for _, test := range []struct {
		name, raw string
		valid     bool
	}{
		{"unknown root member", add(`"unknown":1`), false},
		{"duplicate name", add(`"name":"other"`), false},
		{"duplicate escaped name", add(`"\u006eame":"other"`), false},
		{"unknown nested member", strings.Replace(base, `"network":{`, `"network":{"unknown":1,`, 1), false},
		{"refused version 2", strings.Replace(base, `"version":1`, `"version":2`, 1), false},
		{"refused version 3", strings.Replace(base, `"version":1`, `"version":3`, 1), false},
		{"refused version 4", strings.Replace(base, `"version":1`, `"version":4`, 1), false},
		{"refused version 5", strings.Replace(base, `"version":1`, `"version":5`, 1), false},
		{"unsupported version", strings.Replace(base, `"version":1`, `"version":6`, 1), false},
		{"missing version", strings.Replace(base, `"version":1,`, "", 1), false},
		{"null root", "null", false},
		{"null fleet", strings.Replace(base, `"fleet":`+string(fleetData), `"fleet":null`, 1), false},
		{"null class", strings.Replace(base, `"id":"01"`, `"id":"01","class":null`, 1), false},
		{"empty class", strings.Replace(base, `"id":"01"`, `"id":"01","class":""`, 1), false},
		{"unknown class", strings.Replace(base, `"id":"01"`, `"id":"01","class":"future"`, 1), false},
		{"refused version new class", strings.Replace(strings.Replace(base, `"version":1`, `"version":3`, 1), `"id":"01"`, `"id":"01","class":"compact"`, 1), false},
		{"current version new class", strings.Replace(base, `"id":"01"`, `"id":"01","class":"compact"`, 1), true},
		{"null station classes", strings.Replace(base, `"name":"Harbor"`, `"name":"Harbor","vehicleClasses":null`, 1), false},
		{"null registry", add(`"expressServices":null`), false},
		{"null buffers", add(`"stationBuffers":null`), false},
		{"null onboard pickups", add(`"onboardPickups":null`), false},
		{"optional null", add(`"geo":null`), true},
		{"trailing value", base + " {}", false},
		{"invalid UTF-8", strings.Replace(base, current.Name, string([]byte{0xff}), 1), false},
		{"upper version name", strings.Replace(base, `"version":1`, `"VERSION":1`, 1), false},
		{"long s stations name", strings.Replace(base, `"stations":`, "\"Station\u017f\":", 1), false},
		{"Cyrillic letter is not case", strings.Replace(base, `"fleet":`, "\"flee\u0442\":", 1), false},
		{"Kelvin sign network name", strings.Replace(base, `"network":`, "\"networ\u212a\":", 1), false},
		{"case pair is unknown", strings.Replace(base, `"version":1`, `"Version":3,"version":1`, 1), false},
		{"case pair after version", strings.Replace(base, `"version":1`, `"version":1,"Version":6`, 1), false},
		{"case variant null", strings.Replace(base, `"version":1`, `"version":1,"Version":null`, 1), false},
		{"delimiter is not case", strings.Replace(base, `"version":1`, `"version":1,"ver_sion":1`, 1), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			nativeErr := sessionVerdict([]byte(test.raw))
			if (nativeErr == nil) != test.valid {
				t.Fatalf("raw fixture differs from intended server rule: %v", nativeErr)
			}
			err := command([]string{"-project", projectFile(t, []byte(test.raw))}, io.Discard)
			if (err == nil) != test.valid {
				t.Fatalf("helper differs from server rule: %v", err)
			}
		})
	}
}

func TestHelpAndArguments(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	if err := command([]string{"-h", "-project", "missing.json"}, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "-project") || strings.Contains(output.String(), "Valid project") {
		t.Fatalf("unexpected help: %s", &output)
	}
	for _, args := range [][]string{nil, {"-project"}, {"-project", ""}, {"-project", "missing.json", "extra"}, {"extra", "-project", "missing.json"}, {"-output", "out.json"}, {"-unknown"}} {
		if err := command(args, io.Discard); err == nil {
			t.Fatal("invalid arguments accepted", args)
		}
	}
}

type failedWriter struct{ err error }

func (writer failedWriter) Write([]byte) (int, error) { return 0, writer.err }

func TestInputAndOutputFailures(t *testing.T) {
	t.Parallel()
	for _, path := range []string{filepath.Join(t.TempDir(), "missing.json"), t.TempDir()} {
		if err := command([]string{"-project", path}, io.Discard); err == nil || !strings.Contains(err.Error(), "read project:") {
			t.Fatalf("read failure lost: %v", err)
		}
	}
	path := projectFile(t, projectData(t, project.Default()))
	writeErr := errors.New("summary writer failed")
	if err := command([]string{"-project", path}, failedWriter{err: writeErr}); !errors.Is(err, writeErr) {
		t.Fatalf("write failure lost: %v", err)
	}
	config := project.Default()
	config.Name = "Line\n\x1b[31m"
	var output bytes.Buffer
	if err := command([]string{"-project", projectFile(t, projectData(t, config))}, &output); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(output.String(), "\x1b") || !strings.Contains(output.String(), `Line\n\x1b[31m`) {
		t.Fatalf("project name was not quoted: %q", &output)
	}
}

func TestInputByteBound(t *testing.T) {
	t.Parallel()
	base := projectData(t, project.Default())
	for _, extra := range []int{0, 1} {
		data := make([]byte, project.MaxFileBytes+extra)
		copy(data, base)
		for index := len(base); index < len(data); index++ {
			data[index] = ' '
		}
		err := command([]string{"-project", projectFile(t, data)}, io.Discard)
		if (err == nil) != (extra == 0) {
			t.Fatalf("input bytes=%d error=%v", len(data), err)
		}
		if extra == 1 && !strings.Contains(err.Error(), "read project:") {
			t.Fatalf("oversized input reached decoding: %v", err)
		}
	}
}

// The editor decoder table holds server verdicts for member names that
// differ in case and for repeated names. The command must give the same
// verdicts.
func TestServerDecoderParity(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../../internal/editormodel/testdata/decoder_parity.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Bases map[string]string `json:"bases"`
		Cases []struct {
			Name    string `json:"name"`
			Base    string `json:"base"`
			Find    string `json:"find"`
			Replace string `json:"replace"`
			Valid   bool   `json:"valid"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, item := range fixture.Cases {
		t.Run(item.Name, func(t *testing.T) {
			t.Parallel()
			base := fixture.Bases[item.Base]
			if base == "" || !strings.Contains(base, item.Find) {
				t.Fatal("fixture text is missing")
			}
			data := []byte(strings.Replace(base, item.Find, item.Replace, 1))
			sessionErr := sessionVerdict(data)
			err := command([]string{"-project", projectFile(t, data)}, io.Discard)
			if (err == nil) != item.Valid || (sessionErr == nil) != item.Valid {
				t.Fatalf("valid=%v command=%v session=%v", item.Valid, err, sessionErr)
			}
		})
	}
}
