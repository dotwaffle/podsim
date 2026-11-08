package main

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
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
		{"removed station buffers", add(`"stationBuffers":false`), false},
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
	// Validation refuses a name with a control character, and the error
	// quotes the name.
	config := project.Default()
	config.Name = "Line\n\x1b[31m"
	err := command([]string{"-project", projectFile(t, projectData(t, config))}, io.Discard)
	if err == nil || strings.ContainsAny(err.Error(), "\x1b") || !strings.Contains(err.Error(), `Line\n\x1b[31m`) {
		t.Fatalf("project name was not refused and quoted: %v", err)
	}
	// The summary quotes a name with a quote.
	config.Name = `Line "A"`
	var output bytes.Buffer
	if err := command([]string{"-project", projectFile(t, projectData(t, config))}, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"Line \"A\""`) {
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

func railHubAllClasses(t *testing.T) project.Config {
	t.Helper()
	classes, err := sim.NewClassSet("legacy", "compact", "group", "express")
	if err != nil {
		t.Fatal(err)
	}
	config := scenarios.RailHub()
	for index := range config.Network.Lanes {
		config.Network.Lanes[index].VehicleClasses = classes
	}
	return config
}

// withUTurns adds a U-turn loop to the middle of the first count links of
// a rail-hub project. Each loop leaves a link node and comes back to it. The U-turn has a
// chord of 10 m and a length of 101 m, so its tail exceeds its length.
func withUTurns(t *testing.T, config project.Config, count int) project.Config {
	t.Helper()
	classes, err := sim.NewClassSet("legacy", "compact", "group", "express")
	if err != nil {
		t.Fatal(err)
	}
	positions := make(map[string]sim.Point, len(config.Network.Nodes))
	for _, node := range config.Network.Nodes {
		positions[node.ID] = node.Position
	}
	loops := 0
	for index := range config.Network.Lanes {
		if loops == count {
			break
		}
		lane := &config.Network.Lanes[index]
		if !strings.HasPrefix(lane.ID, "link-") {
			continue
		}
		loops++
		from, to := positions[lane.From], positions[lane.To]
		middle := sim.Point{X: (from.X + to.X) / 2, Y: (from.Y + to.Y) / 2}
		radius := math.Hypot(middle.X, middle.Y)
		out := sim.Point{X: middle.X / radius, Y: middle.Y / radius}
		across := sim.Point{X: -out.Y, Y: out.X}
		at := func(outward, sideways float64) sim.Point {
			return sim.Point{X: middle.X + outward*out.X + sideways*across.X, Y: middle.Y + outward*out.Y + sideways*across.Y}
		}
		prefix := fmt.Sprintf("u%02d", loops)
		hub, in, back := prefix+"-hub", prefix+"-in", prefix+"-back"
		end := lane.To
		lane.To = hub
		speed, control := lane.SpeedLimit, at(200, 0)
		config.Network.Nodes = append(config.Network.Nodes,
			sim.Node{ID: hub, Position: middle}, sim.Node{ID: in, Position: at(100, 5)}, sim.Node{ID: back, Position: at(100, -5)})
		config.Network.Lanes = append(config.Network.Lanes,
			sim.Lane{ID: prefix + "-link", From: hub, To: end, SpeedLimit: speed, VehicleClasses: classes},
			sim.Lane{ID: prefix + "-out", From: hub, To: in, SpeedLimit: speed, VehicleClasses: classes},
			sim.Lane{ID: prefix + "-bend", From: in, To: back, SpeedLimit: speed, VehicleClasses: classes, Control: &control},
			sim.Lane{ID: prefix + "-return", From: back, To: hub, SpeedLimit: speed, VehicleClasses: classes})
	}
	return config
}

// TestProjectCheckTailWarnings checks that a warning does not change the
// exit of the command, and that the lines follow the summary.
func TestProjectCheckTailWarnings(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	if err := command([]string{"-project", projectFile(t, projectData(t, withUTurns(t, railHubAllClasses(t), project.MaxTailWarnings+2)))}, &output); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(output.String(), "\n"), "\n")
	if len(lines) != 2+project.MaxTailWarnings+1 {
		t.Fatalf("got %d lines: %s", len(lines), &output)
	}
	if want := "Warning: Lane u01-bend has a large-class tail of 101.5 m on a length of 101.0 m, so the lane holds one pod."; lines[2] != want {
		t.Fatalf("first warning = %q", lines[2])
	}
	if want := "Warning: 2 more lanes have a large-class tail longer than their length."; lines[len(lines)-1] != want {
		t.Fatalf("count line = %q", lines[len(lines)-1])
	}
	output.Reset()
	if err := command([]string{"-project", projectFile(t, projectData(t, scenarios.RailHub()))}, &output); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "Warning") {
		t.Fatalf("plain rail-hub: %s", &output)
	}
	output.Reset()
	if err := command([]string{"-project", projectFile(t, projectData(t, railHubAllClasses(t)))}, &output); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "Warning") {
		t.Fatalf("all-class rail-hub: %s", &output)
	}
}

// countedWriter fails on the write after the given count.
type countedWriter struct {
	writes int
	err    error
}

func (w *countedWriter) Write(data []byte) (int, error) {
	if w.writes == 0 {
		return 0, w.err
	}
	w.writes--
	return len(data), nil
}

func TestProjectCheckWarningWriteError(t *testing.T) {
	t.Parallel()
	failure := errors.New("write failed")
	err := command([]string{"-project", projectFile(t, projectData(t, withUTurns(t, railHubAllClasses(t), 1)))}, &countedWriter{writes: 1, err: failure})
	if !errors.Is(err, failure) || !strings.Contains(err.Error(), "write project warning") {
		t.Fatalf("error = %v", err)
	}
}

// TestProjectCheckParkingGaps limits the lanes into the parking station of
// the small preset to Compact, so that its Legacy pods have no route to
// parking. The check names the first berth and counts the others.
func TestProjectCheckParkingGaps(t *testing.T) {
	t.Parallel()
	config := scenarios.Small()
	compact, err := sim.NewClassSet("compact")
	if err != nil {
		t.Fatal(err)
	}
	for _, station := range config.Network.Stations {
		if !station.ParkingOnly {
			continue
		}
		for index := range config.Network.Lanes {
			if config.Network.Lanes[index].To == station.Entry {
				config.Network.Lanes[index].VehicleClasses = compact
			}
		}
	}
	if err = project.Validate(config); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err = command([]string{"-project", projectFile(t, projectData(t, config))}, &output)
	want := "parking routes: 16 berths have a gap, the first is berth station-01-01 of station station-01, with no legacy route to parking"
	if err == nil || err.Error() != want || output.Len() != 0 {
		t.Fatalf("err %v, output %q, want %q", err, &output, want)
	}
}
