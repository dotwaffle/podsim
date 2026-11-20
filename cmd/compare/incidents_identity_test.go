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
// contract. The policy flags preserve the settings from before compare ran
// incident rates. The digests include the explicit pickup policy metadata.
// A run is deterministic on one architecture (section 5.1.4), so the digests
// hold on amd64.
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
				"json":  "fbdedff808a6af36c82ad752158abc77669156eb045cc366784b6c0e7730cec1",
				"csv":   "f399d161357d7111316c0184262e8741d7b133ffd52fbd2d828b3ebe16b789fd",
				"table": "37f80d1b034f6c0716213a5a07430ec5c1fbab51465177ddfb32df1adf2458c7",
			}},
		{"busy", new(scenarios.Busy()), []string{"-duration", "5m", "-arrivals-for", "3m", "-request-every", "5s", "-seed", "1", "-pattern", "hotspot"},
			map[string]string{
				"json":  "675ae22017089f46633cb431f7681100842a5ddb8a746b27b83e7b458a96dc93",
				"csv":   "ce934ea37f249ab30152d27f2028196cbc260d857c10873d08672a1ed6e930fe",
				"table": "208650f500351d533f0f980a8f450dd4484876ebeb9bfd3a88dcda0b77f788c9",
			}},
		{"rail hub", new(scenarios.RailHub()), []string{"-duration", "10m", "-arrivals-for", "5m", "-request-every", "5s", "-burst-size", "12", "-seed", "1", "-pattern", "hub-burst"},
			map[string]string{
				"json":  "e180844b57296d1631445eb40e03bccae9a3f107a8dbf3e5d1995c040306b745",
				"csv":   "85fe6c77b586732c1e713d424bb32420a8cbe7539a056ec277f424445419f889",
				"table": "5d6499387c64bf9a2c8e92009e1b72d9d55312cba7e76180e6856a280015fd5b",
			}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			args := slices.Concat(test.args, []string{"-redistribution-policies", "off,on", "-pickup-reassignment", "off"})
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
	const want = "ce934ea37f249ab30152d27f2028196cbc260d857c10873d08672a1ed6e930fe"
	args := []string{"-redistribution-policies", "off,on", "-pickup-reassignment", "off", "-duration", "5m", "-arrivals-for", "3m", "-request-every", "5s", "-seed", "1", "-pattern", "hotspot", "-format", "csv"}
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
		{"rail services", rail, []string{"-pattern", "rail-services", "-duration", "10m", "-arrivals-for", "3s", "-stop-when-drained", "-seeds", "7,19", "-redistribution-policies", "off"}, "a1f4f4fcfedc7d32dde231a9a2548e78af8bcdcc22708fa2e3bbda2e85052b93"},
		{"daily", dailyComparisonScenario(t), []string{"-pattern", "profile-daily", "-duration", "4m", "-arrivals-for", "3m", "-seed", "1"}, "9fd4dea4f6cef295665203cb9a5f486a30af156d31affece2059c5075f1bb97e"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			args := slices.Concat(test.args, []string{"-pickup-reassignment", "off"})
			if !slices.Contains(args, "-redistribution-policies") {
				args = append(args, "-redistribution-policies", "off,on")
			}
			opts, err := parseOptions(args, &bytes.Buffer{})
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
