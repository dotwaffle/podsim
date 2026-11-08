package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"io"
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
)

func TestRunWritesRawProjectConfig(t *testing.T) {
	t.Parallel()
	var first bytes.Buffer
	if err := run([]string{"-preset", "scale100"}, &first, io.Discard); err != nil {
		t.Fatal(err)
	}
	var config project.Config
	if err := jsonv2.Unmarshal(first.Bytes(), &config, json.DefaultOptionsV1()); err != nil {
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
	if err := jsonv2.Unmarshal(output.Bytes(), &config, json.DefaultOptionsV1()); err != nil {
		t.Fatal(err)
	}
	if config.Version != project.CurrentVersion || len(config.Network.Stations[1].Banks) != 2 {
		t.Fatal("fixture lost independent banks")
	}
	if err := run([]string{"-preset", "independent-banks", "-station-berths", "4"}, &bytes.Buffer{}, io.Discard); err == nil {
		t.Fatal("fixture accepted a whole-station generation flag")
	}
}

func TestRunWritesCompactStationPreset(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	if err := run([]string{"-preset", "compact-station"}, &output, io.Discard); err != nil {
		t.Fatal(err)
	}
	var config project.Config
	if err := jsonv2.Unmarshal(output.Bytes(), &config, json.DefaultOptionsV1()); err != nil {
		t.Fatal(err)
	}
	if err := project.Validate(config); err != nil {
		t.Fatal(err)
	}
	if len(config.Network.ManeuverGroups) != 1 || len(config.Network.ManeuverGroups[0].BerthIDs) != 4 || len(config.Fleet) != 6 {
		t.Fatal("preset lost its maneuver group or pods")
	}
	if err := run([]string{"-preset", "compact-station", "-berth-pitch", "40"}, &bytes.Buffer{}, io.Discard); err == nil {
		t.Fatal("preset accepted a whole-station generation flag")
	}
}

func TestRunWritesRailHubPreset(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	if err := run([]string{"-preset", "rail-hub"}, &output, io.Discard); err != nil {
		t.Fatal(err)
	}
	var config project.Config
	if err := jsonv2.Unmarshal(output.Bytes(), &config, json.DefaultOptionsV1()); err != nil {
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
		"small":               "c6cef78005b07bc1a58f277bb30655ffee1b6a0424c00cb2598b50a270b7a172",
		"busy":                "a3b8cc1b9397fdead60159bb2f36d83e191ef4a23aace9446279fdb9f137a921",
		"parking-constrained": "006dffebdd023cad179272c051dbe7f4f24fb0dbd89931f829e86c25d7aa5adc",
		"rail-hub":            "622c7d2a904691008a9d0309c46bdd62f1e7058b3a8f48cf0189ea3e9ab2a796",
		"rail-hub-incidents":  "7d3d1c4309452899e666eee478b96923f39b03c32ac7903aaf16591d90167ca3",
		"scale100":            "b8e952d7a1ae2f0fa1cc9f2022742b54cb101ffff78771d1a96cbec4552eac0f",
		"london-central":      "ee6e663927d20d78b55b648141be18c1eacec8580e869e9dd03eb3b9c0da2b2e",
		"london-full":         "95dd5f6d466a9fc0721a9e3d1338bf94dde147f787240abf7e8b8e72f4ab579c",
		"london-full-express": "9032f3535806ec1edbce9d2048efdac59191d5c0d79e8e5c6fac9b4eba95d369",
	}
	// networks holds the network SHA-256 of the summary line of the London
	// presets.
	networks := map[string]string{
		"london-central":      "d1fa6044728d60392e26001c48892e8abf5e0ebf6bdf690153e6be8e3d2c4ad6",
		"london-full":         "4a7efa573c6af08d96de0ceeb60287176422f2bf93bc4ea2e5e2a19a21f83374",
		"london-full-express": "3aa0a2eb9e99eab4ea80ecfad481c7718b57cd38e0b35876b44b5ab5dc1d22e5",
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
			if network, ok := networks[preset]; ok && !strings.Contains(line, " network_sha256="+network+" ") {
				t.Fatalf("summary = %q, want network SHA-256 %s", line, network)
			}
		})
	}
}

// TestRunLondonOptOut pins the network SHA-256 of the London presets with
// the flags that turn off the depots and the Compact stations. Both flags
// give the networks of the presets without depots and Compact stations.
// Without -parking-berths, -parking-lanes 0 gives the ordinary facilities
// their own berth count.
// TestRunPresetsTurnOnPolicies pins redistribution and pickup reassignment in
// the output of each preset name, also the fixtures of this command.
func TestRunPresetsTurnOnPolicies(t *testing.T) {
	t.Parallel()
	for _, preset := range presets {
		t.Run(preset, func(t *testing.T) {
			t.Parallel()
			config, _ := generate(t, "-preset", preset)
			if !config.Redistribution || !bool(config.PickupReassignment) {
				t.Fatalf("redistribution %t, pickup reassignment %t, want on", config.Redistribution, config.PickupReassignment)
			}
		})
	}
}

func TestRunLondonOptOut(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, preset, network string
		flags                 []string
		groups, depots        int
	}{
		{name: "LondonCentral", preset: "london-central", network: "b5ffdaec92145c98d487cdb31a8c9b286d7ebc3052cd186c1364438c4c7a272c",
			flags: []string{"-parking-lanes", "0", "-compact-stations", "none"}},
		{name: "LondonCentral without depots", preset: "london-central", network: "dfe2594ffd97f4cff417513f459498db63a1f69002ce1fc21ce2691c8b6e5163",
			flags: []string{"-parking-lanes", "0"}, groups: 7},
		{name: "LondonCentral without Compact stations", preset: "london-central", network: "1e9031e223a8166fd889a876313e0d5d983cb4f36820add35bbea19b773d6833",
			flags: []string{"-compact-stations", "none"}, depots: 3},
		{name: "LondonFull", preset: "london-full", network: "4c65870bff2d41c1b849d6b11b2c6bfb3b0a6cb4c8e686136221f137c2bab8f6",
			flags: []string{"-parking-lanes", "0", "-compact-stations", "none"}},
		{name: "LondonFull without depots", preset: "london-full", network: "f287ee6a0bdddc0431a7c6b55de268469f28506e851403890e3ab1c88204fc93",
			flags: []string{"-parking-lanes", "0"}, groups: 8},
		{name: "LondonFull without Compact stations", preset: "london-full", network: "e44e6d07c57daea8e7f15d7f8829e792f95b3ce73167cd92ff270b92e3806ba6",
			flags: []string{"-compact-stations", "none"}, depots: 3},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config, summary := generate(t, append([]string{"-preset", test.preset}, test.flags...)...)
			if !strings.Contains(summary, " network_sha256="+test.network+" ") {
				t.Fatalf("summary = %q, want network SHA-256 %s", summary, test.network)
			}
			if len(config.Network.ManeuverGroups) != test.groups || len(config.Network.Depots) != test.depots {
				t.Fatalf("%d maneuver groups and %d depots, want %d and %d", len(config.Network.ManeuverGroups), len(config.Network.Depots), test.groups, test.depots)
			}
		})
	}
}

// TestRunKeepsDepotRing pins the output of a ring with a depot.
func TestRunKeepsDepotRing(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	if err := run([]string{"-preset", "small", "-parking-lanes", "3", "-parking-berths", "5"}, &output, io.Discard); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(output.Bytes())
	if got, want := hex.EncodeToString(sum[:]), "ea1067554d432759306bd5601fe267fbccdfccdce99439e504422a7cdcbb0c5f"; got != want {
		t.Fatalf("output SHA-256 = %s, want %s", got, want)
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
	if err := jsonv2.Unmarshal(output.Bytes(), &config, json.DefaultOptionsV1()); err != nil {
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
	t.Run("Parking lanes", func(t *testing.T) {
		t.Parallel()
		config, summary := generate(t, "-preset", "rail-hub", "-parking-lanes", "3", "-parking-berths", "4")
		if len(config.Network.Depots) != 1 || len(config.Network.Depots[0].Lanes) != 3 || len(config.Network.Depots[0].Lanes[2].BerthIDs) != 4 {
			t.Fatalf("depots %+v", config.Network.Depots)
		}
		if !strings.Contains(summary, " parking_berths=12 pods=30 ") {
			t.Fatalf("summary = %q", summary)
		}
	})
	t.Run("London Parking lanes", func(t *testing.T) {
		t.Parallel()
		config, summary := generate(t, "-preset", "london-central", "-parking-lanes", "2", "-parking-berths", "6", "-berths", "parking-north=9")
		if len(config.Network.Depots) != 3 || len(config.Network.Depots[1].Lanes) != 2 || len(config.Network.Depots[1].Lanes[0].BerthIDs) != 9 {
			t.Fatalf("depots %+v", config.Network.Depots)
		}
		if !strings.Contains(summary, " parking_berths=42 pods=114 ") || config.Name != "LondonCentral (custom capacity)" {
			t.Fatalf("summary %q, name %q", summary, config.Name)
		}
	})
	t.Run("London Compact stations", func(t *testing.T) {
		t.Parallel()
		// The default stations and depots on the untouched LondonFull
		// defaults, which give each station a berth count.
		ids := "940GZZLURGP, 940GZZLULBN,940GZZLULGT,940GZZLUERB,940GZZLUBWT,940GZZLUHPC,940GZZLUBOR,940GZZDLTWG"
		config, _ := generate(t, "-preset", "london-full", "-compact-stations", ids, "-parking-lanes", "2", "-parking-berths", "6")
		if len(config.Network.ManeuverGroups) != 8 || len(config.Network.Depots) != 3 || berthCount(t, config, "940GZZLURGP") != 4 {
			t.Fatalf("%d groups, %d depots, %d Regent's Park berths", len(config.Network.ManeuverGroups), len(config.Network.Depots), berthCount(t, config, "940GZZLURGP"))
		}
		for _, pod := range config.Fleet {
			if pod.Class != sim.CompactClass {
				t.Fatalf("pod %s has the class %q", pod.ID, pod.Class)
			}
		}
		if config.Name != "LondonFull" {
			t.Fatalf("name %q", config.Name)
		}
	})
	t.Run("berths of a London station without Compact stations", func(t *testing.T) {
		t.Parallel()
		config, _ := generate(t, "-preset", "london-central", "-compact-stations", "none", "-berths", "940GZZLURGP=3")
		if berthCount(t, config, "940GZZLURGP") != 3 || len(config.Network.ManeuverGroups) != 0 {
			t.Fatalf("%d Regent's Park berths, %d groups", berthCount(t, config, "940GZZLURGP"), len(config.Network.ManeuverGroups))
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
		if !strings.Contains(summary, "pods=36 nodes=1946/") || !strings.Contains(summary, " soft_conflicts=1\n") {
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
		{name: "negative Parking lanes for London", arguments: []string{"-preset", "london-central", "-parking-lanes", "-1"}, err: "parking lanes must not be negative"},
		{name: "London depot too long", arguments: []string{"-preset", "london-full", "-parking-lanes", "6", "-parking-berths", "12"}, err: "West London Parking (parking-west): the depot of 6 lanes of 12 slots is 202.03 meters long, more than 200 meters"},
		{name: "Parking lanes for the mesh", arguments: []string{"-preset", "scale100", "-parking-lanes", "2"}, err: "parking lanes apply only to a ring"},
		{name: "Parking lanes for a fixture", arguments: []string{"-preset", "compact-station", "-parking-lanes", "2"}, err: "-parking-lanes does not apply to the compact station fixture"},
		{name: "pods for the Express preset", arguments: []string{"-preset", "london-full-express", "-station-pods", "2"}, err: "-station-pods does not apply to the LondonFull Express preset"},
		{name: "Compact stations for a ring", arguments: []string{"-preset", "busy", "-compact-stations", "s01"}, err: "-compact-stations applies only to the London presets"},
		{name: "Compact stations for a fixture", arguments: []string{"-preset", "compact-station", "-compact-stations", "compact"}, err: "-compact-stations does not apply to the compact station fixture"},
		{name: "empty Compact station", arguments: []string{"-preset", "london-central", "-compact-stations", "940GZZLURGP,,940GZZLUBOR"}, err: `-compact-stations item "" has no station ID`},
		{name: "berths of a Compact station", arguments: []string{"-preset", "london-full", "-compact-stations", "940GZZLUBOR,940GZZLURGP", "-berths", "940GZZLURGP=2"}, err: `-berths names Compact station "940GZZLURGP", which has the 4 bays of the compact station; use -compact-stations none or a list without it`},
		{name: "berths of a default Compact station", arguments: []string{"-preset", "london-central", "-berths", "940GZZLUBOR=3"}, err: `-berths names Compact station "940GZZLUBOR", which has the 4 bays of the compact station; use -compact-stations none or a list without it`},
		{name: "Parking as a Compact station", arguments: []string{"-preset", "london-central", "-compact-stations", "parking-west"}, err: "West London Parking (parking-west) is a Parking facility, not a passenger station for Compact bays"},
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

// TestRunIncidentFlags checks that each flag writes its member and no other.
// A flag works on each kind of preset, and over a preset value.
func TestRunIncidentFlags(t *testing.T) {
	t.Parallel()
	rate := []string{"-fault-rate", "5", "-fault-duration", "fixed:600"}
	base := func() *project.FaultConfig {
		return &project.FaultConfig{PerHour: new(5.0), Duration: &project.FaultDuration{Kind: "fixed", Seconds: new(600)}}
	}
	edited := func(edit func(*project.FaultConfig)) *project.FaultConfig {
		faults := base()
		edit(faults)
		return faults
	}
	preset := scenarios.RailHubIncidents()
	for _, test := range []struct {
		name        string
		preset      string
		flags       []string
		faults      *project.FaultConfig
		emergencies *project.EmergencyConfig
	}{
		{name: "no flag", preset: "small"},
		{name: "fault rate and fixed duration", preset: "small", flags: rate, faults: base()},
		{name: "uniform duration", preset: "small", flags: []string{"-fault-rate", "5", "-fault-duration", "uniform:60:600"},
			faults: &project.FaultConfig{PerHour: new(5.0), Duration: &project.FaultDuration{Kind: "uniform", MinSeconds: new(60), MaxSeconds: new(600)}}},
		{name: "exponential duration", preset: "small", flags: []string{"-fault-rate", "5", "-fault-duration", "exponential:60:3600:600"},
			faults: &project.FaultConfig{PerHour: new(5.0), Duration: &project.FaultDuration{Kind: "exponential", MinSeconds: new(60), MaxSeconds: new(3600), MeanSeconds: new(600)}}},
		{name: "debris share", preset: "small", flags: append([]string{"-debris-share", "0.5"}, rate...),
			faults: edited(func(faults *project.FaultConfig) { faults.DebrisShare = new(0.5) })},
		{name: "debris meters", preset: "small", flags: append([]string{"-debris-meters", "3"}, rate...),
			faults: edited(func(faults *project.FaultConfig) { faults.DebrisMeters = new(3.0) })},
		{name: "evacuation seconds", preset: "small", flags: []string{"-evacuation-seconds", "30"}, faults: &project.FaultConfig{EvacuationSeconds: new(30)}},
		{name: "evacuation seconds of 0", preset: "small", flags: []string{"-evacuation-seconds", "0"}, faults: &project.FaultConfig{EvacuationSeconds: new(0)}},
		{name: "emergency rate", preset: "small", flags: []string{"-emergency-rate", "7"}, emergencies: &project.EmergencyConfig{PerHour: new(7.0)}},
		{name: "rates of 0", preset: "small", flags: []string{"-fault-rate", "0", "-emergency-rate", "0"},
			faults: &project.FaultConfig{PerHour: new(0.0)}, emergencies: &project.EmergencyConfig{PerHour: new(0.0)}},
		{name: "a fixture", preset: "compact-station", flags: []string{"-emergency-rate", "7"}, emergencies: &project.EmergencyConfig{PerHour: new(7.0)}},
		{name: "the mesh", preset: "scale100", flags: rate, faults: base()},
		{name: "over a preset fault rate", preset: "rail-hub-incidents", flags: []string{"-fault-rate", "0"}, emergencies: preset.Emergencies,
			faults: &project.FaultConfig{PerHour: new(0.0), DebrisShare: preset.Faults.DebrisShare, DebrisMeters: preset.Faults.DebrisMeters, Duration: preset.Faults.Duration}},
		{name: "over a preset emergency rate", preset: "rail-hub-incidents", flags: []string{"-emergency-rate", "1"}, faults: preset.Faults, emergencies: &project.EmergencyConfig{PerHour: new(1.0)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config, _ := generate(t, append([]string{"-preset", test.preset}, test.flags...)...)
			if !reflect.DeepEqual(config.Faults, test.faults) {
				t.Errorf("faults = %+v, want %+v", config.Faults, test.faults)
			}
			if !reflect.DeepEqual(config.Emergencies, test.emergencies) {
				t.Errorf("emergencies = %+v, want %+v", config.Emergencies, test.emergencies)
			}
			if err := project.Validate(config); err != nil {
				t.Error(err)
			}
		})
	}
}

// TestRunRejectsBadIncidentFlags checks the refusals of the flag parser and
// their order, and that project.Validate gives each other refusal.
func TestRunRejectsBadIncidentFlags(t *testing.T) {
	t.Parallel()
	const (
		form = "-fault-duration must be fixed:S, uniform:MIN:MAX, or exponential:MIN:MAX:MEAN"
		need = "-debris-share, -debris-meters, and -fault-duration need -fault-rate"
	)
	tests := []struct {
		name  string
		flags []string
		want  string
	}{
		{"form before the rate", []string{"-fault-duration", "soon", "-debris-share", "0.5"}, form},
		{"debris share needs the rate", []string{"-debris-share", "0.5"}, need},
		{"debris meters needs the rate", []string{"-debris-meters", "3"}, need},
		{"duration needs the rate", []string{"-fault-duration", "fixed:600"}, need},
		{"the preset rate is not the flag", []string{"-preset", "rail-hub-incidents", "-debris-share", "0.5"}, need},
		{"parser before the preset", []string{"-preset", "missing", "-debris-share", "0.5"}, need},
		{"rate above the limit", []string{"-fault-rate", "61"}, "faults perHour must be 0 to 60"},
		{"NaN rate", []string{"-emergency-rate", "NaN"}, "emergencies perHour must be 0 to 60"},
		{"rate with no duration", []string{"-fault-rate", "5"}, "faults perHour above 0 needs a duration"},
		{"share above 1", []string{"-fault-rate", "5", "-fault-duration", "fixed:600", "-debris-share", "2"}, "faults debrisShare must be 0 to 1"},
		{"evacuation above the limit", []string{"-evacuation-seconds", "3601"}, "faults evacuationSeconds must be 0 to 3600"},
		{"uniform out of order", []string{"-fault-rate", "5", "-fault-duration", "uniform:600:60"}, "faults duration"},
	}
	for _, text := range []string{"", "fixed", "fixed:", "fixed:a", "fixed:1.5", "fixed:1:2", "uniform:1", "uniform:1:2:3", "exponential:1:2", "exponential:1:2:3:4", "other:1"} {
		tests = append(tests, struct {
			name  string
			flags []string
			want  string
		}{"form " + text, []string{"-fault-rate", "5", "-fault-duration", text}, form})
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var output bytes.Buffer
			err := run(append([]string{"-preset", "small"}, test.flags...), &output, io.Discard)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("run error = %v, want an error with %q", err, test.want)
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
	pretty, err := jsonv2.Marshal(config, json.DefaultOptionsV1(), jsontext.WithIndent("  "))
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
			if decodeErr := jsonv2.Unmarshal(data, &decoded, json.DefaultOptionsV1()); decodeErr != nil {
				t.Fatal(decodeErr)
			}
			want, err := jsonv2.Marshal(config, json.DefaultOptionsV1())
			if err != nil {
				t.Fatal(err)
			}
			got, err := jsonv2.Marshal(decoded, json.DefaultOptionsV1())
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
	if output.Len() > project.MaxFileBytes {
		t.Fatalf("full output has %d bytes, more than %d", output.Len(), project.MaxFileBytes)
	}
	var config project.Config
	if err := jsonv2.Unmarshal(output.Bytes(), &config, json.DefaultOptionsV1()); err != nil {
		t.Fatal(err)
	}
	if err := project.Validate(config); err != nil {
		t.Fatal(err)
	}
	if config.Name != "LondonFull" || len(config.Network.Stations) != 312 || len(config.Fleet) != 327 ||
		len(config.DemandProfiles) != 1 || len(config.DemandProfiles[0].Flows) != 76567 || len(config.DemandProfiles[0].Bands) != 6 {
		t.Fatal("full export lost its preset identity, network, fleet, or demand")
	}
	if !strings.Contains(summary.String(), "soft_conflicts=4") {
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
