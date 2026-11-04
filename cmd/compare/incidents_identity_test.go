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
				"json":  "2d836815aa8c4a5644a66ea9283ff5dd63dcdd6f3026ff746bebee3e0be66dfa",
				"csv":   "44315a44da313f19f5aa163a7e8992b5e428ce5575f4cca52e222f6fd4cb4b9c",
				"table": "0b5139947decd53c6ca494ff4fd5007471b8bc6cb709bd96854bd8802bea5aad",
			}},
		{"busy", new(scenarios.Busy()), []string{"-duration", "5m", "-arrivals-for", "3m", "-request-every", "5s", "-seed", "1", "-pattern", "hotspot"},
			map[string]string{
				"json":  "75821b3bdd10f0d8b00217e25b9567e7b16f06b65380c05482f9ca725f07fb3d",
				"csv":   "2a712881992f9aadfd3f1441f591e99d5a534b5bb24493ef4ef33c5281a34fe2",
				"table": "d0c3779883537b27f64eec6e86b6b0c9e0854b267a418b481c7234478e10124e",
			}},
		{"rail hub", new(scenarios.RailHub()), []string{"-duration", "10m", "-arrivals-for", "5m", "-request-every", "5s", "-burst-size", "12", "-seed", "1", "-pattern", "hub-burst"},
			map[string]string{
				"json":  "6246652060a02312a27cccbaaabcc7fcf4b45d9c9306528ab5b1854d370c1ce2",
				"csv":   "f42ee5caa3589b12f8579da8296b1b577c63ed1242de91a6bc819bcf4858f5c7",
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
	const want = "2a712881992f9aadfd3f1441f591e99d5a534b5bb24493ef4ef33c5281a34fe2"
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
		{"rail services", rail, []string{"-pattern", "rail-services", "-duration", "10m", "-arrivals-for", "3s", "-stop-when-drained", "-seeds", "7,19", "-redistribution-policies", "off", "-rail-forecast"}, "01afd10b9e3237839fc46e4d9a44ff173214e82450bb485b8e95186346f04602"},
		{"daily", dailyComparisonScenario(t), []string{"-pattern", "profile-daily", "-duration", "4m", "-arrivals-for", "3m", "-seed", "1"}, "e9f68e6333b84fc70b637800b27c19ce9c0849e9127484412e3ff1624e88d4ac"},
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
