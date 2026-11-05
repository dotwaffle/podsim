package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
)

// writeProjectFile writes config to a project file and returns its path.
func writeProjectFile(t *testing.T, config project.Config) string {
	t.Helper()
	data, err := jsonv2.Marshal(config, json.DefaultOptionsV1())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "project.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// compareOutput returns the SHA-256 of the report of a command line.
func compareOutput(t *testing.T, args []string) string {
	t.Helper()
	var output, failure bytes.Buffer
	if code := runCLI(cliInput{args: args, stdout: &output, stderr: &failure}); code != 0 {
		t.Fatalf("exit %d: %s", code, failure.String())
	}
	sum := sha256.Sum256(output.Bytes())
	return hex.EncodeToString(sum[:])
}

// TestNoRateOutputIsPinned checks section 5.1.5 of the incident stage 7
// contract. The output of each format has the SHA-256 of the commit before
// compare ran incident rates. A run is deterministic on one architecture
// (section 5.1.4), so the digests hold on amd64.
func TestNoRateOutputIsPinned(t *testing.T) {
	t.Parallel()
	skipLong(t)
	for _, test := range []struct {
		name   string
		config *project.Config
		args   []string
		want   map[string]string
	}{
		{"default", nil, []string{"-duration", "10m", "-arrivals-for", "5m", "-request-every", "20s", "-seeds", "1,2", "-patterns", "balanced,hotspot", "-stop-when-drained"},
			map[string]string{
				"json":  "99201e1e926a7dae433f23cd3a3342dc1d0998044c49f628a2888dbe1999b628",
				"csv":   "77b8bc6030d53f8878cdc64615d339ae86163f7836a4d042893cc939d0b61efb",
				"table": "0b5139947decd53c6ca494ff4fd5007471b8bc6cb709bd96854bd8802bea5aad",
			}},
		{"busy", new(scenarios.Busy()), []string{"-duration", "5m", "-arrivals-for", "3m", "-request-every", "5s", "-seed", "1", "-pattern", "hotspot"},
			map[string]string{
				"json":  "18ceb98a70a5e4afe5dcc437f6c954b6470a856f36f6fa3f95ad6474dc8390d4",
				"csv":   "8ec95e70580cf895f88f2ca08a8ba62092a01c1b4a68571a9b62917cec2d90f9",
				"table": "d0c3779883537b27f64eec6e86b6b0c9e0854b267a418b481c7234478e10124e",
			}},
		{"rail hub", new(scenarios.RailHub()), []string{"-duration", "10m", "-arrivals-for", "5m", "-request-every", "5s", "-burst-size", "12", "-seed", "1", "-pattern", "hub-burst"},
			map[string]string{
				"json":  "ae0acd74d4e93c55ae3183f3e3734478e884e62235c61953be5bd174b4511eb0",
				"csv":   "5c5b37be0355466afde8d2fc6d3e522d153ba0f6c25bc8d35d04d33433cc17e0",
				"table": "18131d83742e6033ff31e9c0b7d2c431fa6213773654d7de0f5a57ee78195b8a",
			}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			args := test.args
			if test.config != nil {
				args = slices.Concat(args, []string{"-project", writeProjectFile(t, *test.config)})
			}
			for format, want := range test.want {
				if got := compareOutput(t, slices.Concat(args, []string{"-format", format})); got != want {
					t.Errorf("%s: SHA-256 %s, want %s", format, got, want)
				}
			}
		})
	}
}

// TestNoRateFormsArePinned checks that each form of a rate of 0 gives the output
// of a project with no incident settings.
func TestNoRateFormsArePinned(t *testing.T) {
	t.Parallel()
	skipLong(t)
	const want = "8ec95e70580cf895f88f2ca08a8ba62092a01c1b4a68571a9b62917cec2d90f9"
	args := []string{"-duration", "5m", "-arrivals-for", "3m", "-request-every", "5s", "-seed", "1", "-pattern", "hotspot", "-format", "csv"}
	negativeZero := math.Copysign(0, -1)
	for name, test := range map[string]struct {
		faults      *project.FaultConfig
		emergencies *project.EmergencyConfig
	}{
		"absent":        {},
		"empty":         {&project.FaultConfig{}, &project.EmergencyConfig{}},
		"zero":          {&project.FaultConfig{PerHour: new(0.0)}, &project.EmergencyConfig{PerHour: new(0.0)}},
		"negative zero": {&project.FaultConfig{PerHour: &negativeZero}, &project.EmergencyConfig{PerHour: &negativeZero}},
		"settings":      {&project.FaultConfig{EvacuationSeconds: new(60), DebrisShare: new(0.5)}, nil},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			config := scenarios.Busy()
			config.Faults, config.Emergencies = test.faults, test.emergencies
			if got := compareOutput(t, slices.Concat(args, []string{"-project", writeProjectFile(t, config)})); got != want {
				t.Fatalf("SHA-256 %s, want %s", got, want)
			}
		})
	}
}

// TestNoRatePlanOutputIsPinned checks the two service paths, on the plans of
// their own tests.
func TestNoRatePlanOutputIsPinned(t *testing.T) {
	t.Parallel()
	rail, err := loadScenario("", "harbor")
	if err != nil {
		t.Fatal(err)
	}
	rail.railDepartures = serviceComparisonDepartures()
	rail.railArrivals = []project.RailArrival{{ID: "inbound", Station: "garden", Passengers: 1, Destinations: []project.RailDestination{{Station: "market", Weight: 1}}}}
	for _, test := range []struct {
		name     string
		scenario scenario
		args     []string
		want     string
	}{
		{"rail services", rail, []string{"-pattern", "rail-services", "-duration", "10m", "-arrivals-for", "3s", "-stop-when-drained", "-seeds", "7,19", "-redistribution-policies", "off"}, "0b717a7e61f0b18b8034f40a5ec3caba9e6145c0edb18a98f862c78ded2b724e"},
		{"daily", dailyComparisonScenario(t), []string{"-pattern", "profile-daily", "-duration", "4m", "-arrivals-for", "3m", "-seed", "1"}, "f12e561b114890f25516628fd8cfab8ca3d8fc5d8e81c4b2642e20278230203e"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			opts, err := parseOptions(test.args, &bytes.Buffer{})
			if err != nil {
				t.Fatal(err)
			}
			results, err := compare(opts, test.scenario)
			if err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			if err := writeReport(writeReportInput{output: &output, format: "json", results: results}); err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(output.Bytes())
			if got := hex.EncodeToString(sum[:]); got != test.want {
				t.Fatalf("SHA-256 %s, want %s", got, test.want)
			}
		})
	}
}
