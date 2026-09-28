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
		"london":              "fa639a79b7b7659eb30b28faf030a57704bc4e9544e8a4394de32cee31d2010d",
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
		config, summary := generate(t, "-preset", "london", "-berths", "940GZZLUKSX=3", "-station-pods", "0", "-parking-pods", "12", "-parking-berths", "12")
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
		{name: "unknown London station", arguments: []string{"-preset", "london", "-berths", "940GZZLUXXX=3"}, err: `unknown London station ID "940GZZLUXXX"`},
		{name: "pods for a ring", arguments: []string{"-preset", "busy", "-station-pods", "2"}, err: "-station-pods applies only to the london preset"},
		{name: "Parking pods for the mesh", arguments: []string{"-preset", "scale100", "-parking-pods", "2"}, err: "-parking-pods applies only to the london preset"},
		{name: "pitch below the floor", arguments: []string{"-preset", "london", "-berth-pitch", "20"}, err: "berth pitch"},
		{name: "layout conflict", arguments: []string{"-preset", "london", "-berths", "940GZZLUEMB=3"}, err: "Embankment (940GZZLUEMB)"},
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
