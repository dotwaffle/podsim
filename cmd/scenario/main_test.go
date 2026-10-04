package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
)

func TestRunWritesRawProjectConfig(t *testing.T) {
	t.Parallel()
	var first bytes.Buffer
	if err := run([]string{"-preset", "scale100"}, &first, io.Discard); err != nil {
		t.Fatal(err)
	}
	var config project.Config
	if err := json.Unmarshal(first.Bytes(), &config); err != nil {
		t.Fatal(err)
	}
	if err := project.Validate(config); err != nil {
		t.Fatal(err)
	}
	if len(config.Network.Stations) != 20 || len(config.Fleet) != 100 {
		t.Fatalf("got %d stations and %d pods", len(config.Network.Stations), len(config.Fleet))
	}
	var second bytes.Buffer
	if err := run([]string{"-preset", "scale100"}, &second, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("successive runs produced different JSON")
	}
}

func TestRunRejectsUnknownPreset(t *testing.T) {
	t.Parallel()
	if err := run([]string{"-preset", "missing"}, &bytes.Buffer{}, io.Discard); err == nil {
		t.Fatal("accepted an unknown preset")
	}
	if err := run([]string{"extra"}, &bytes.Buffer{}, io.Discard); err == nil {
		t.Fatal("accepted a positional argument")
	}
}

func TestRunWritesIndependentBankFixture(t *testing.T) {
	var output bytes.Buffer
	if err := run([]string{"-preset", "independent-banks"}, &output, io.Discard); err != nil {
		t.Fatal(err)
	}
	var config project.Config
	if err := json.Unmarshal(output.Bytes(), &config); err != nil {
		t.Fatal(err)
	}
	if config.Version != project.CurrentVersion || len(config.Network.Stations[1].Banks) != 2 {
		t.Fatal("fixture lost independent banks")
	}
	if err := run([]string{"-preset", "independent-banks", "-station-berths", "4"}, &bytes.Buffer{}, io.Discard); err == nil {
		t.Fatal("fixture accepted a whole-station generation flag")
	}
}

func TestRunWritesRailHubPreset(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	if err := run([]string{"-preset", "rail-hub"}, &output, io.Discard); err != nil {
		t.Fatal(err)
	}
	var config project.Config
	if err := json.Unmarshal(output.Bytes(), &config); err != nil {
		t.Fatal(err)
	}
	hub, ok := config.Network.Station(config.Demand.Destination)
	if !ok || hub.Name != "Rail Hub" || len(config.Fleet) != 30 {
		t.Fatalf("rail-hub preset = %+v, found = %t", config, ok)
	}
}

// TestRunWithoutFlagsKeepsPresets pins the output of each preset without
// capacity flags.
func TestRunWithoutFlagsKeepsPresets(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"small":               "10de2ae8734fc8b3a3895e2b43a23f135862e366dabd1c3a7426b6c99eaf39ae",
		"busy":                "c949cbcf24cdac407990c8ff97d84beb0d2b69575ac5782bf80b0ad10d2db0b0",
		"parking-constrained": "690cd3857d2bc4cef4cf7758b6703f3e02877e7bf13a47301f41b0210b9be1d1",
		"rail-hub":            "e492e0a3fd738f5121779bc2d4880a4c5c14733d330eb57f20b0acded3b73917",
		"scale100":            "896f1784d0f1e13cf9f6637d24de7970db99edbbd4c10adf9ce60f17388e1abc",
		"london-central":      "e616e7f2d73d8ca19cbd1ec6775dd7193bcda091248144538098ee18deaaafe5",
	}
	for preset, want := range tests {
		t.Run(preset, func(t *testing.T) {
			t.Parallel()
			var output, summary bytes.Buffer
			if err := run([]string{"-preset", preset}, &output, &summary); err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(output.Bytes())
			if got := hex.EncodeToString(sum[:]); got != want {
				t.Fatalf("output SHA-256 = %s, want %s", got, want)
			}
			line := summary.String()
			if !strings.HasPrefix(line, "preset="+preset+" ") || strings.Count(line, "\n") != 1 || !strings.Contains(line, " bytes="+strconv.Itoa(output.Len())+" ") {
				t.Fatalf("summary = %q", line)
			}
		})
	}
}

// generate runs the command and decodes the project.
func generate(t *testing.T, arguments ...string) (project.Config, string) {
	t.Helper()
	var output, summary bytes.Buffer
	if err := run(arguments, &output, &summary); err != nil {
		t.Fatal(err)
	}
	var config project.Config
	if err := json.Unmarshal(output.Bytes(), &config); err != nil {
		t.Fatal(err)
	}
	return config, summary.String()
}

// berthCount returns the berth count of a station.
func berthCount(t *testing.T, config project.Config, id string) int {
	t.Helper()
	station, ok := config.Network.Station(id)
	if !ok {
		t.Fatalf("no station %q", id)
	}
	return len(station.Berths)
}

func TestRunCapacityFlags(t *testing.T) {
	t.Parallel()
	t.Run("station berths", func(t *testing.T) {
		t.Parallel()
		config, summary := generate(t, "-preset", "small", "-station-berths", "6")
		if berthCount(t, config, "station-01") != 6 || berthCount(t, config, "parking") != 4 {
			t.Fatalf("got %d and %d berths", berthCount(t, config, "station-01"), berthCount(t, config, "parking"))
		}
		if !strings.Contains(summary, "passenger_berths=24 parking_berths=4 pods=12 ") || !strings.HasSuffix(config.Name, " (custom capacity)") {
			t.Fatalf("summary %q, name %q", summary, config.Name)
		}
	})
	t.Run("Parking berths", func(t *testing.T) {
		t.Parallel()
		config, _ := generate(t, "-preset", "rail-hub", "-parking-berths", "20")
		if berthCount(t, config, "parking") != 20 {
			t.Fatalf("got %d Parking berths", berthCount(t, config, "parking"))
		}
	})
	t.Run("berths of single stations", func(t *testing.T) {
		t.Parallel()
		config, _ := generate(t, "-preset", "scale100", "-berths", "station-19=8, station-02=2", "-berth-pitch", "68")
		if berthCount(t, config, "station-19") != 8 || berthCount(t, config, "station-02") != 2 || berthCount(t, config, "station-01") != 6 {
			t.Fatal("the -berths flag did not set the berth counts")
		}
	})
	t.Run("berth pitch", func(t *testing.T) {
		t.Parallel()
		config, _ := generate(t, "-preset", "busy", "-berth-pitch", "40")
		first, _ := config.Network.Node("s01-berth-01")
		second, _ := config.Network.Node("s01-berth-02")
		if distance := math.Hypot(second.Position.X-first.Position.X, second.Position.Y-first.Position.Y); math.Abs(distance-40) > 1e-9 {
			t.Fatalf("berth pitch = %v meters, want 40", distance)
		}
	})
	t.Run("London", func(t *testing.T) {
		t.Parallel()
		config, summary := generate(t, "-preset", "london-central", "-berths", "940GZZLUKSX=3", "-station-pods", "0", "-parking-pods", "12", "-parking-berths", "12")
		if berthCount(t, config, "940GZZLUKSX") != 3 || len(config.Fleet) != 36 {
			t.Fatalf("got %d King's Cross berths and %d pods", berthCount(t, config, "940GZZLUKSX"), len(config.Fleet))
		}
		if !strings.Contains(summary, "pods=36 nodes=1845/") || !strings.Contains(summary, " soft_conflicts=1\n") {
			t.Fatalf("summary = %q", summary)
		}
	})
}

func TestRunRejectsBadCapacityFlags(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		arguments []string
		err       string
	}{
		{name: "item without a count", arguments: []string{"-preset", "small", "-berths", "station-01"}, err: `-berths item "station-01" is not ID=N`},
		{name: "empty list", arguments: []string{"-preset", "small", "-berths", ""}, err: `-berths item "" is not ID=N`},
		{name: "count not a number", arguments: []string{"-preset", "small", "-berths", "station-01=x"}, err: "has no whole berth count"},
		{name: "station twice", arguments: []string{"-preset", "small", "-berths", "station-01=2,station-01=3"}, err: "more than once"},
		{name: "unknown station", arguments: []string{"-preset", "small", "-berths", "station-09=2"}, err: `unknown station ID "station-09"`},
		{name: "unknown London station", arguments: []string{"-preset", "london-central", "-berths", "940GZZLUXXX=3"}, err: `unknown London station ID "940GZZLUXXX"`},
		{name: "pods for a ring", arguments: []string{"-preset", "busy", "-station-pods", "2"}, err: "-station-pods applies only to the London presets"},
		{name: "Parking pods for the mesh", arguments: []string{"-preset", "scale100", "-parking-pods", "2"}, err: "-parking-pods applies only to the London presets"},
		{name: "pitch below the floor", arguments: []string{"-preset", "london-central", "-berth-pitch", "20"}, err: "berth pitch"},
		{name: "layout conflict", arguments: []string{"-preset", "london-central", "-berths", "940GZZLUEMB=3"}, err: "Embankment (940GZZLUEMB)"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var output bytes.Buffer
			err := run(test.arguments, &output, io.Discard)
			if err == nil || !strings.Contains(err.Error(), test.err) {
				t.Fatalf("run error = %v, want an error with %q", err, test.err)
			}
			if output.Len() != 0 {
				t.Fatalf("run wrote %d bytes after an error", output.Len())
			}
		})
	}
}

func TestScenarioJSONFitsFileLimit(t *testing.T) {
	t.Parallel()
	config := project.Default()
	pretty, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name                    string
		limit                   int
		wantIndented, wantError bool
	}{
		{name: "exact indented bound", limit: len(pretty) + 1, wantIndented: true},
		{name: "compact fallback", limit: len(pretty)},
		{name: "cannot fit", limit: 1, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			data, err := scenarioJSON(config, test.limit)
			if test.wantError {
				if err == nil {
					t.Fatal("accepted oversized output")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(data) > test.limit || !bytes.HasSuffix(data, []byte{'\n'}) {
				t.Fatal("output exceeds the limit or lacks its final newline")
			}
			if got := bytes.Contains(data, []byte("\n  ")); got != test.wantIndented {
				t.Fatalf("indented = %t, want %t", got, test.wantIndented)
			}
			var decoded project.Config
			if decodeErr := json.Unmarshal(data, &decoded); decodeErr != nil {
				t.Fatal(decodeErr)
			}
			want, err := json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			got, err := json.Marshal(decoded)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatal("output changed the project")
			}
		})
	}
}

func TestRunWritesImportableLondonFull(t *testing.T) {
	t.Parallel()
	var output, summary bytes.Buffer
	if err := run([]string{"-preset", "london-full"}, &output, &summary); err != nil {
		t.Fatal(err)
	}
	if output.Len() > project.MaxFileBytes || bytes.Count(output.Bytes(), []byte{'\n'}) != 1 {
		t.Fatalf("full output has %d bytes or is not compact", output.Len())
	}
	var config project.Config
	if err := json.Unmarshal(output.Bytes(), &config); err != nil {
		t.Fatal(err)
	}
	if err := project.Validate(config); err != nil {
		t.Fatal(err)
	}
	if config.Name != "LondonFull" || len(config.Network.Stations) != 272 || len(config.Fleet) != 287 ||
		len(config.DemandProfiles) != 1 || len(config.DemandProfiles[0].Flows) != 60996 || len(config.DemandProfiles[0].Bands) != 6 {
		t.Fatal("full export lost its preset identity, network, fleet, or demand")
	}
	if !strings.Contains(summary.String(), "soft_conflicts=3") {
		t.Fatalf("full summary = %s", summary.String())
	}
}

func TestLondonFullCapacityFlagsPreserveDefaults(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name                 string
		flags                []string
		heathrow, kingsCross int
	}{
		{name: "single override", flags: []string{"-berths", "940GZZLUHRC=1"}, heathrow: 1, kingsCross: 5},
		{name: "uniform override", flags: []string{"-station-berths", "2"}, heathrow: 2, kingsCross: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config, _ := generate(t, append([]string{"-preset", "london-full"}, test.flags...)...)
			for id, count := range map[string]int{"940GZZLUHRC": test.heathrow, "940GZZLUKSX": test.kingsCross} {
				station, ok := config.Network.Station(id)
				if !ok || len(station.Berths) != count {
					t.Fatalf("station %s has %d berths, want %d", id, len(station.Berths), count)
				}
			}
		})
	}
}
