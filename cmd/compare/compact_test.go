package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dotwaffle/podsim/internal/sim"
)

func TestStationQueueSpacingOptions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"empty", []string{"-station-queue-spacing", ""}},
		{"unknown", []string{"-station-queue-spacing", "compact"}},
		{"duplicate", []string{"-station-queue-spacing", "ordinary,ordinary"}},
		{"trailing", []string{"-station-queue-spacing", "ordinary,"}},
		{"buffers omitted", []string{"-station-queue-spacing", "compact-v1", "-platoon-policies", "virtual"}},
		{"buffers off", []string{"-station-queue-spacing", "compact-v1", "-station-buffers", "off", "-platoon-policies", "virtual"}},
		{"mixed buffers", []string{"-station-queue-spacing", "ordinary,compact-v1", "-station-buffers", "on,off", "-platoon-policies", "virtual"}},
		{"platoons omitted", []string{"-station-queue-spacing", "compact-v1", "-station-buffers", "on"}},
		{"platoons off", []string{"-station-queue-spacing", "compact-v1", "-station-buffers", "on", "-platoon-policies", "off"}},
		{"mixed platoons", []string{"-station-queue-spacing", "ordinary,compact-v1", "-station-buffers", "on", "-platoon-policies", "virtual,off"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			args := append(slices.Clone(tc.args), "-project", "/does/not/exist")
			if code := runCLI(cliInput{args: args, stdout: &stdout, stderr: &stderr}); code != 2 || stdout.Len() != 0 || strings.Contains(stderr.String(), "open ") {
				t.Fatalf("invalid matrix reached scenario load: code=%d stdout=%s stderr=%s", code, &stdout, &stderr)
			}
		})
	}
	for _, value := range []string{"ordinary", "compact-v1", " compact-v1, ordinary "} {
		opts, err := parseOptions([]string{"-station-queue-spacing", value, "-station-buffers", "on", "-platoon-policies", "virtual"}, &bytes.Buffer{})
		if err != nil || len(opts.stationQueueSpacing) != len(strings.Split(value, ",")) {
			t.Fatalf("valid selector %q rejected: %v", value, err)
		}
	}
	if _, err := compare(options{stationQueueSpacing: []string{"compact-v1"}}, scenario{}); err == nil || !strings.Contains(err.Error(), "station-buffers") {
		t.Fatalf("direct matrix did not fail before demand: %v", err)
	}
}

func TestStationQueueSpacingCLIReports(t *testing.T) {
	t.Parallel()
	args := []string{"-duration", "2m", "-arrivals-for", "1m", "-request-every", "10s", "-redistribution-policies", "off", "-seed", "7", "-station-buffers", "on", "-platoon-policies", "virtual"}
	for _, format := range []string{"json", "csv", "table"} {
		var base, explicit, stderr bytes.Buffer
		baseArgs := append(slices.Clone(args), "-format", format)
		if code := runCLI(cliInput{args: baseArgs, stdout: &base, stderr: &stderr}); code != 0 {
			t.Fatalf("default CLI failed: %s", &stderr)
		}
		if strings.Contains(base.String(), "station_queue_spacing") || strings.Contains(base.String(), "QUEUE SPACING") {
			t.Fatalf("default report gained spacing metadata: %s", &base)
		}
		explicitArgs := append(slices.Clone(baseArgs), "-station-queue-spacing", "ordinary,compact-v1")
		if code := runCLI(cliInput{args: explicitArgs, stdout: &explicit, stderr: &stderr}); code != 0 {
			t.Fatalf("compact CLI failed: %s", &stderr)
		}
		switch format {
		case "json":
			var baseline, arms report
			if err := json.Unmarshal(base.Bytes(), &baseline); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(explicit.Bytes(), &arms); err != nil {
				t.Fatal(err)
			}
			if baseline.SchemaVersion != 13 || arms.SchemaVersion != 13 || len(arms.Results) != 2 {
				t.Fatalf("report version or matrix changed: %+v", arms)
			}
			for i, spacing := range []string{"ordinary", "compact-v1"} {
				arm := arms.Results[i]
				if arm.StationQueueSpacing != spacing || arm.ScheduleID != baseline.Results[0].ScheduleID || arm.Scheduled != baseline.Results[0].Scheduled || arm.Served+arm.Remaining+arm.Skipped != arm.Scheduled || arm.ActualEndSeconds > 120 || arm.ArrivalEndSeconds != baseline.Results[0].ArrivalEndSeconds || arm.ArrivalWindowSeconds != 60 {
					t.Fatalf("unmatched offers, incomplete trips, or wrong bounds: %+v", arm)
				}
			}
			ordinary := arms.Results[0]
			ordinary.StationQueueSpacing = ""
			if !reflect.DeepEqual(ordinary, baseline.Results[0]) {
				t.Fatal("explicit ordinary changed outcome")
			}
		case "csv":
			rows, err := csv.NewReader(&explicit).ReadAll()
			if err != nil {
				t.Fatal(err)
			}
			column := slices.Index(rows[0], "station_queue_spacing")
			if column < 0 || len(rows) != 3 || rows[1][column] != "ordinary" || rows[2][column] != "compact-v1" {
				t.Fatalf("CSV spacing missing: %+v", rows)
			}
		case "table":
			if !strings.Contains(explicit.String(), "QUEUE SPACING") || !strings.Contains(explicit.String(), "compact-v1") {
				t.Fatal("table spacing missing")
			}
		}
	}
	var output, stderr bytes.Buffer
	if code := runCLI(cliInput{args: []string{"-duration", "1m", "-format", "json", "-redistribution-policies", "off"}, stdout: &output, stderr: &stderr}); code != 0 || !strings.Contains(output.String(), `"schema_version": 12`) {
		t.Fatalf("default report version changed: %s %s", &output, &stderr)
	}
}

func TestStationQueueSpacingConfiguration(t *testing.T) {
	t.Parallel()
	caseStudy, err := loadScenario("", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, limit := range []int{2, 3, 4} {
		t.Run(strconv.Itoa(limit), func(t *testing.T) {
			t.Parallel()
			simulation, err := sim.NewFleet(caseStudy.network, caseStudy.fleet)
			if err != nil {
				t.Fatal(err)
			}
			input := runInput{stationBuffers: "on", platoonPolicy: "virtual", stationQueueSpacing: "compact-v1"}
			if err := configureExperimentalPolicies(simulation, input); err == nil {
				t.Fatal("compact policy accepted disabled simulation platoons")
			}
			if err := simulation.SetPlatooning(sim.PlatooningVirtual); err != nil {
				t.Fatal(err)
			}
			if err := simulation.SetPlatoonLimit(limit); err != nil {
				t.Fatal(err)
			}
			if err := configureExperimentalPolicies(simulation, input); err != nil || simulation.StationQueueSpacing() != sim.StationQueueCompactV1 || !simulation.NeedsBufferState() {
				t.Fatalf("compact policy not configured: %v", err)
			}
			input.stationQueueSpacing = ""
			if err := configureExperimentalPolicies(simulation, input); err != nil || simulation.StationQueueSpacing() != sim.StationQueueOrdinary {
				t.Fatalf("omitted selector inherited compact policy: %v", err)
			}
			input.stationQueueSpacing = "unknown"
			if err := configureExperimentalPolicies(simulation, input); err == nil {
				t.Fatal("unknown direct arm accepted")
			}
		})
	}
}

func TestStationQueueSpacingAdaptiveGroupsAndMatrixLimit(t *testing.T) {
	t.Parallel()
	inputs := experimentalInputs([]runInput{{requestEvery: time.Second}}, options{stationQueueSpacing: []string{"ordinary", "compact-v1"}})
	if scheduler := newArmScheduler(inputs, 0); len(scheduler.groups) != 2 || len(scheduler.start()) != 2 {
		t.Fatal("spacing arms shared adaptive group")
	}
	seeds := make([]string, 100)
	for i := range seeds {
		seeds[i] = strconv.Itoa(i + 1)
	}
	_, err := parseOptions([]string{"-seeds", strings.Join(seeds, ","), "-loads", "5s,10s,15s", "-station-buffers", "on", "-platoon-policies", "virtual", "-station-queue-spacing", "ordinary,compact-v1", "-pickup-reassignment", "on,off"}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "matrix") {
		t.Fatalf("spacing arms escaped matrix limit: %v", err)
	}
}

type faultStepper struct {
	steps         int
	fault         error
	couplingFault error
	onStep        func()
}

func (s *faultStepper) Step() {
	s.steps++
	if s.onStep != nil {
		s.onStep()
	}
}
func (s *faultStepper) CompactQueueError() error { return s.fault }
func (s *faultStepper) CouplingError() error     { return s.couplingFault }

func TestComparisonStepFault(t *testing.T) {
	t.Parallel()
	fault := errors.New("retained physical fault")
	s := &faultStepper{fault: fault}
	if err := stepComparison(s); !errors.Is(err, fault) || !strings.Contains(err.Error(), "compact station queue controller") || s.steps != 1 {
		t.Fatalf("fault lost: steps=%d error=%v", s.steps, err)
	}
	s.fault = nil
	if err := stepComparison(s); err != nil || s.steps != 2 {
		t.Fatalf("healthy step failed: steps=%d error=%v", s.steps, err)
	}
}

func TestComparisonCouplingFault(t *testing.T) {
	t.Parallel()
	for _, duringStep := range []bool{false, true} {
		t.Run(map[bool]string{false: "retained", true: "during step"}[duringStep], func(t *testing.T) {
			t.Parallel()
			fault := errors.New("unproved physical tick")
			s := &faultStepper{}
			if duringStep {
				s.onStep = func() { s.couplingFault = fault }
			} else {
				s.couplingFault = fault
			}
			if err := stepComparison(s); !errors.Is(err, fault) || !strings.Contains(err.Error(), "physical coupling controller") {
				t.Fatal("comparison accepted a coupling fault", err)
			}
			want := 0
			if duringStep {
				want = 1
			}
			if s.steps != want {
				t.Fatal("comparison advanced a retained coupling fault", s.steps)
			}
		})
	}
}

func TestStationQueueSpacingDirectValidation(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"unknown", "ordinary,ordinary"} {
		opts := options{stationQueueSpacingText: value}
		if err := parseExperimentalOptions(&opts, map[string]bool{"station-queue-spacing": true}); err == nil {
			t.Fatalf("direct parser accepted %q", value)
		}
	}
	if err := validateStationQueueOptions(options{stationQueueSpacing: []string{"unknown"}}); err == nil {
		t.Fatal("direct matrix accepted unknown spacing")
	}
	caseStudy, err := loadScenario("", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []runInput{
		{stationBuffers: "off", platoonPolicy: "virtual", stationQueueSpacing: "compact-v1"},
		{stationBuffers: "on", platoonPolicy: "off", stationQueueSpacing: "compact-v1"},
	} {
		simulation, err := sim.NewFleet(caseStudy.network, caseStudy.fleet)
		if err != nil {
			t.Fatal(err)
		}
		if err := simulation.SetPlatooning(sim.PlatooningVirtual); err != nil {
			t.Fatal(err)
		}
		if err := configureExperimentalPolicies(simulation, input); err == nil || !strings.Contains(err.Error(), "compact-v1 requires") {
			t.Fatalf("invalid direct policy accepted: %+v", input)
		}
	}
}

func TestStationQueueSpacingServiceFutureCensoring(t *testing.T) {
	t.Parallel()
	opts, err := parseOptions([]string{"-pattern", "rail-services", "-duration", "2m", "-arrivals-for", "3s", "-stop-when-drained", "-station-buffers", "on", "-platoon-policies", "virtual", "-station-queue-spacing", "ordinary,compact-v1", "-redistribution-policies", "off"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	caseStudy, err := loadScenario("", "harbor")
	if err != nil {
		t.Fatal(err)
	}
	caseStudy.railDepartures = serviceComparisonDepartures()
	rows, err := compare(opts, caseStudy)
	if err != nil || len(rows) != 2 {
		t.Fatalf("service compact arms: %v", err)
	}
	for _, row := range rows {
		connections := row.RailConnections
		if row.ScheduleID != rows[0].ScheduleID || row.Scheduled != 5 || row.Served+row.Remaining+row.Skipped != 5 || row.ActualEndSeconds > 120 || connections == nil || connections.Unresolved != 3 || connections.Made+connections.Missed+connections.Unserved+connections.Unresolved != 5 {
			t.Fatalf("service bounds or future censoring changed: %+v", row)
		}
	}
}

func TestStationQueueSpacingOnlyReport(t *testing.T) {
	t.Parallel()
	var output, stderr bytes.Buffer
	code := runCLI(cliInput{args: []string{"-duration", "1m", "-station-queue-spacing", "ordinary", "-redistribution-policies", "off", "-format", "json"}, stdout: &output, stderr: &stderr})
	var decoded report
	if code != 0 {
		t.Fatalf("ordinary CLI failed: %s", &stderr)
	}
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil || decoded.SchemaVersion != 13 || len(decoded.Results) != 1 || decoded.Results[0].StationQueueSpacing != "ordinary" || decoded.Results[0].StationBuffers != "" || decoded.Results[0].PlatoonPolicy != "" {
		t.Fatalf("spacing-only provenance lost: %v %+v", err, decoded)
	}
}
