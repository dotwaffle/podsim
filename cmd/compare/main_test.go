package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

func TestParseOptionsRejectsInvalidBounds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "zero duration", args: []string{"-duration", "0s"}, want: "duration must"},
		{name: "long duration", args: []string{"-duration", "25h"}, want: "duration must"},
		{name: "subtick duration", args: []string{"-duration", "1ms"}, want: "simulation tick"},
		{name: "long arrival window", args: []string{"-duration", "1m", "-arrivals-for", "2m"}, want: "arrivals-for"},
		{name: "zero load", args: []string{"-request-every", "0s"}, want: "each load"},
		{name: "load after window", args: []string{"-duration", "1m", "-request-every", "2m"}, want: "each load"},
		{name: "load at arrival end", args: []string{"-duration", "2m", "-arrivals-for", "1m", "-request-every", "1m"}, want: "each load"},
		{name: "subtick load", args: []string{"-request-every", "1ms"}, want: "simulation tick"},
		{name: "duplicate seed", args: []string{"-seeds", "1,1"}, want: "more than once"},
		{name: "invalid seed", args: []string{"-seeds", "one"}, want: "parse seed"},
		{name: "duplicate load", args: []string{"-loads", "30s,30s"}, want: "more than once"},
		{name: "unknown pattern", args: []string{"-pattern", "future"}, want: "unknown demand pattern"},
		{name: "duplicate pattern", args: []string{"-patterns", "balanced,balanced"}, want: "more than once"},
		{name: "unknown format", args: []string{"-format", "yaml"}, want: "format must"},
		{name: "zero queue", args: []string{"-queue-limit", "0"}, want: "queue-limit"},
		{name: "zero burst", args: []string{"-burst-size", "0"}, want: "burst-size"},
		{name: "zero workers", args: []string{"-workers", "0"}, want: "workers"},
		{name: "zero sharing limit", args: []string{"-sharing-limits", "0"}, want: "sharing limits"},
		{name: "duplicate sharing limit", args: []string{"-sharing-limits", "2,2"}, want: "more than once"},
		{name: "unknown routing policy", args: []string{"-routing-policies", "fast"}, want: "unknown routing policy"},
		{name: "duplicate routing policy", args: []string{"-routing-policies", "free-flow,free-flow"}, want: "more than once"},
		{name: "unknown redistribution policy", args: []string{"-redistribution-policies", "maybe"}, want: "unknown redistribution policy"},
		{name: "duplicate redistribution policy", args: []string{"-redistribution-policies", "off,off"}, want: "more than once"},
		{name: "unknown wait rule", args: []string{"-wait-rules", "lenient"}, want: "unknown wait rule"},
		{name: "empty wait rule", args: []string{"-wait-rules", "current,"}, want: "unknown wait rule"},
		{name: "duplicate wait rule", args: []string{"-wait-rules", "strict,strict"}, want: "more than once"},
		{name: "adaptive limit without drain stop", args: []string{"-adaptive-limit"}, want: "adaptive-limit requires -stop-when-drained"},
		{name: "negative past limit", args: []string{"-adaptive-limit", "-stop-when-drained", "-past-limit", "-1"}, want: "past-limit must be at least 0"},
		{name: "past limit without adaptive limit", args: []string{"-stop-when-drained", "-past-limit", "2"}, want: "past-limit requires -adaptive-limit"},
		{name: "positional argument", args: []string{"extra"}, want: "unexpected positional"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseOptions(test.args, &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("parseOptions() error = %v, want text %q", err, test.want)
			}
		})
	}
}

func TestCompareIsRepeatableAndPairsSchedules(t *testing.T) {
	t.Parallel()
	opts, err := parseOptions([]string{
		"-duration", "2m", "-loads", "20s,30s", "-seeds", "7,19", "-patterns", "balanced,bursty-hotspot",
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	scenario, err := loadScenario("", "")
	if err != nil {
		t.Fatal(err)
	}
	first, err := compare(opts, scenario)
	if err != nil {
		t.Fatal(err)
	}
	opts.workers = 4
	second, err := compare(opts, scenario)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("identical comparisons produced different results")
	}
	if len(first) != 16 {
		t.Fatalf("result count = %d, want 16", len(first))
	}
	for i := 0; i < len(first); i += 2 {
		off, on := first[i], first[i+1]
		if off.Policy != "off" || on.Policy != "on" || off.ScheduleID != on.ScheduleID || off.Scheduled != on.Scheduled {
			t.Fatalf("comparison pair does not share a schedule: off=%+v on=%+v", off, on)
		}
	}
}

func TestComparePairsSharedRideLimits(t *testing.T) {
	t.Parallel()
	opts, err := parseOptions([]string{
		"-duration", "2m", "-request-every", "10s", "-pattern", "hub-burst", "-burst-size", "3", "-sharing-limits", "1,3",
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	caseStudy, err := loadScenario("", "market")
	if err != nil {
		t.Fatal(err)
	}
	caseStudy.fleet = append(caseStudy.fleet, sim.Placement{ID: "03", StationID: "market", BerthID: "market-1"})
	results, err := compare(opts, caseStudy)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 4 || results[0].SharedRidePartyLimit != 1 || results[1].SharedRidePartyLimit != 1 ||
		results[2].SharedRidePartyLimit != 3 || results[3].SharedRidePartyLimit != 3 {
		t.Fatalf("sharing comparison arms = %+v", results)
	}
	if results[2].SharedParties == 0 || results[3].SharedParties == 0 {
		t.Fatalf("sharing comparison did not combine parties: %+v", results)
	}
}

func TestComparePairsRoutingPolicies(t *testing.T) {
	t.Parallel()
	opts, err := parseOptions([]string{
		"-duration", "2m", "-request-every", "20s", "-routing-policies", "free-flow,congestion",
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	caseStudy, err := loadScenario("", "")
	if err != nil {
		t.Fatal(err)
	}
	results, err := compare(opts, caseStudy)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 4 || results[0].RoutingPolicy != "free-flow" || results[1].RoutingPolicy != "free-flow" ||
		results[2].RoutingPolicy != "congestion" || results[3].RoutingPolicy != "congestion" {
		t.Fatalf("routing comparison arms = %+v", results)
	}
}

func TestCLIOutputIsRepeatable(t *testing.T) {
	t.Parallel()
	args := []string{
		"-duration", "2m", "-request-every", "30s", "-seeds", "7,19", "-patterns", "balanced,hotspot", "-format", "json",
	}
	var firstOutput, secondOutput bytes.Buffer
	var firstError, secondError bytes.Buffer
	if code := runCLI(cliInput{args: args, stdout: &firstOutput, stderr: &firstError}); code != 0 {
		t.Fatalf("first run exit = %d, error = %s", code, firstError.String())
	}
	if code := runCLI(cliInput{args: args, stdout: &secondOutput, stderr: &secondError}); code != 0 {
		t.Fatalf("second run exit = %d, error = %s", code, secondError.String())
	}
	if !bytes.Equal(firstOutput.Bytes(), secondOutput.Bytes()) {
		t.Fatal("identical CLI runs produced different reports")
	}
}

func TestDemandSchedulePatterns(t *testing.T) {
	t.Parallel()
	base := scheduleInput{
		seed: 7, durationTicks: durationTicks(10 * time.Minute), intervalTicks: durationTicks(time.Minute),
		burstSize: 3, passengers: []string{"harbor", "garden", "market"}, focus: "market",
	}
	for _, pattern := range syntheticPatterns {
		input := base
		input.pattern = pattern
		first, second := demandSchedule(input), demandSchedule(input)
		if !reflect.DeepEqual(first, second) || len(first) != 9 {
			t.Fatalf("pattern %s is not repeatable or has wrong count: %d", pattern, len(first))
		}
		for _, request := range first {
			if request.origin == request.destination {
				t.Fatalf("pattern %s generated same-station request: %+v", pattern, request)
			}
			if pattern == "destination" && request.destination != "market" {
				t.Fatalf("destination pattern generated %+v", request)
			}
		}
		if pattern == "bursty-hotspot" && (first[0].tick != first[1].tick || first[1].tick != first[2].tick) {
			t.Fatalf("bursty pattern did not group arrivals: %+v", first[:3])
		}
		if pattern == "hub-burst" {
			if first[0].tick != first[1].tick || first[1].tick != first[2].tick {
				t.Fatalf("hub burst did not group arrivals: %+v", first[:3])
			}
			for _, request := range first {
				if request.origin != "market" {
					t.Fatalf("hub burst generated %+v", request)
				}
			}
		}
	}
}

func TestProfileDemandBandsAreRepeatable(t *testing.T) {
	t.Parallel()
	caseStudy, err := loadScenario("", "")
	if err != nil {
		t.Fatal(err)
	}
	caseStudy.demand = project.DemandConfig{Pattern: "profile", Profile: "test", Band: "peak"}
	caseStudy.demandProfiles = []project.DemandProfile{{
		ID: "test", Name: "Test profile",
		Bands: []project.DemandBand{{ID: "peak", Name: "Peak"}, {ID: "quiet", Name: "Quiet"}},
		Flows: []project.DemandFlow{
			{From: "harbor", To: "market", Weights: []float64{9, 1}},
			{From: "garden", To: "harbor", Weights: []float64{1, 9}},
		},
	}}
	opts, err := parseOptions([]string{
		"-duration", "2m", "-arrivals-for", "1m", "-request-every", "10s", "-pattern", "profile", "-bands", "all",
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	arms, err := demandArms(opts, caseStudy)
	if err != nil {
		t.Fatal(err)
	}
	if len(arms) != 2 || arms[0].band != "peak" || arms[1].band != "quiet" {
		t.Fatalf("profile arms = %+v", arms)
	}
	weights, err := demandWeights("profile", "peak", caseStudy)
	if err != nil {
		t.Fatal(err)
	}
	if weights["harbor"] != 9 || weights["garden"] != 1 {
		t.Fatalf("profile pickup weights = %+v", weights)
	}
	input := scheduleInput{
		seed: 7, durationTicks: durationTicks(time.Minute), intervalTicks: durationTicks(10 * time.Second),
		pattern: "profile", profileFlows: arms[0].flows,
	}
	first, second := demandSchedule(input), demandSchedule(input)
	if !reflect.DeepEqual(first, second) || len(first) != 5 {
		t.Fatalf("profile schedule is not repeatable: first=%+v second=%+v", first, second)
	}
	for _, request := range first {
		if request.origin == request.destination {
			t.Fatalf("profile generated same-station request: %+v", request)
		}
	}
}

func TestReportFormatsAreMachineReadable(t *testing.T) {
	t.Parallel()
	results := []result{{Pattern: "balanced", Policy: "off", ScheduleID: "abc", Scheduled: 1}}
	var jsonOutput bytes.Buffer
	if err := writeReport(writeReportInput{output: &jsonOutput, format: "json", results: results}); err != nil {
		t.Fatal(err)
	}
	var decoded report
	if err := json.Unmarshal(jsonOutput.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.SchemaVersion != 5 || !reflect.DeepEqual(decoded.Results, results) {
		t.Fatalf("JSON report changed values: %+v", decoded)
	}

	var csvOutput bytes.Buffer
	if err := writeReport(writeReportInput{output: &csvOutput, format: "csv", results: results}); err != nil {
		t.Fatal(err)
	}
	records, err := csv.NewReader(&csvOutput).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[0][0] != "pattern" || records[1][0] != "balanced" {
		t.Fatalf("unexpected CSV report: %+v", records)
	}
}

func TestArrivalWindowLeavesDrainTime(t *testing.T) {
	t.Parallel()
	opts, err := parseOptions([]string{
		"-duration", "4m", "-arrivals-for", "1m", "-request-every", "5s", "-burst-size", "6", "-pattern", "hub-burst",
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	caseStudy, err := loadScenario("", "market")
	if err != nil {
		t.Fatal(err)
	}
	results, err := compare(opts, caseStudy)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("result count = %d, want 2", len(results))
	}
	for _, outcome := range results {
		if outcome.ArrivalEndSeconds >= outcome.WindowEndSeconds || outcome.BurstSize != 6 || outcome.FocusStation != "market" {
			t.Fatalf("experiment window = %+v", outcome)
		}
		if outcome.PeakPending == 0 || outcome.PeakFocusOccupiedBerths == 0 {
			t.Fatalf("experiment did not record demand and berth use: %+v", outcome)
		}
		if outcome.PassengerDistanceMeters <= 0 || outcome.LoadedDistancePercent <= 0 || outcome.LoadedDistancePercent >= 100 {
			t.Fatalf("experiment did not record loaded distance: %+v", outcome)
		}
	}
}

func TestLoadedDistancePercent(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name             string
		passenger, empty float64
		want             float64
	}{
		{name: "no travel", want: 0},
		{name: "all passenger", passenger: 25, want: 100},
		{name: "quarter loaded", passenger: 25, empty: 75, want: 25},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := loadedDistancePercent(test.passenger, test.empty); got != test.want {
				t.Fatalf("loadedDistancePercent(%v, %v) = %v, want %v", test.passenger, test.empty, got, test.want)
			}
		})
	}
}

func TestQueueLimitCountsSkippedArrivals(t *testing.T) {
	t.Parallel()
	opts, err := parseOptions([]string{
		"-duration", "3m", "-request-every", "5s", "-seed", "3", "-pattern", "bursty-hotspot", "-queue-limit", "1",
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	scenario, err := loadScenario("", "")
	if err != nil {
		t.Fatal(err)
	}
	results, err := compare(opts, scenario)
	if err != nil {
		t.Fatal(err)
	}
	for _, outcome := range results {
		if outcome.Skipped == 0 || outcome.Scheduled != outcome.Served+outcome.Remaining+outcome.Skipped {
			t.Fatalf("incorrect queue accounting: %+v", outcome)
		}
	}
}

// TestWorkingVehiclesAfterTrip runs one trip to completion. The pod keeps its
// completed Request when it goes idle, and the working count must drop to zero.
func TestWorkingVehiclesAfterTrip(t *testing.T) {
	t.Parallel()
	caseStudy, err := loadScenario("", "")
	if err != nil {
		t.Fatal(err)
	}
	simulation, err := sim.NewFleet(caseStudy.network, caseStudy.fleet)
	if err != nil {
		t.Fatal(err)
	}
	if err := simulation.RequestTrip(caseStudy.passengers[0], caseStudy.passengers[1]); err != nil {
		t.Fatal(err)
	}
	peakWorking, peakPassenger := 0, 0
	limit := int64(10 * time.Minute * sim.TicksPerSecond / time.Second)
	state := simulation.Snapshot()
	for state.Completed == 0 && state.Tick < limit {
		working := state.WorkingVehicles()
		passenger := 0
		for _, vehicle := range state.Vehicles {
			if vehicle.Pod.Occupied {
				passenger++
			}
		}
		if passenger > working {
			t.Fatalf("tick %d: %d pods carry passengers but only %d have work", state.Tick, passenger, working)
		}
		peakWorking, peakPassenger = max(peakWorking, working), max(peakPassenger, passenger)
		simulation.Step()
		state = simulation.Snapshot()
	}
	if state.Completed != 1 {
		t.Fatalf("completed = %d at tick %d, want 1", state.Completed, state.Tick)
	}
	if peakWorking != 1 || peakPassenger != 1 {
		t.Fatalf("peak working = %d, peak passenger = %d, want 1 and 1", peakWorking, peakPassenger)
	}
	kept := slices.ContainsFunc(state.Vehicles, func(vehicle sim.Vehicle) bool { return vehicle.Request != nil })
	if !kept {
		t.Fatal("no pod keeps its completed request, so the test does not cover that case")
	}
	if got := state.WorkingVehicles(); got != 0 {
		t.Fatalf("WorkingVehicles() after the trip = %d, want 0", got)
	}
}

func TestParseWaitRules(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		args []string
		want []string
	}{
		{"flag not given", nil, nil},
		{"default value given", []string{"-wait-rules", "current"}, []string{"current"}},
		{"all rules in order", []string{"-wait-rules", "none, strict,current"}, []string{"none", "strict", "current"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts, err := parseOptions(tc.args, &bytes.Buffer{})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(opts.waitRules, tc.want) || (opts.waitRules == nil) != (tc.want == nil) {
				t.Fatalf("wait rules = %#v, want %#v", opts.waitRules, tc.want)
			}
		})
	}
}

func TestWaitRuleColumnOnlyWhenRequested(t *testing.T) {
	t.Parallel()
	results := []result{{Pattern: "balanced", Policy: "off", RoutingPolicy: "free-flow", WaitRule: "strict", WaitAverageSeconds: 12.34}}
	for _, tc := range []struct {
		name       string
		format     string
		column     bool
		wantHeader string
		wantValue  string
	}{
		{"csv without column", "csv", false, "shared_ride_party_limit", "0"},
		{"csv with column", "csv", true, "wait_rule", "strict"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var output bytes.Buffer
			if err := writeReport(writeReportInput{output: &output, format: tc.format, results: results, waitRuleColumn: tc.column}); err != nil {
				t.Fatal(err)
			}
			records, err := csv.NewReader(&output).ReadAll()
			if err != nil {
				t.Fatal(err)
			}
			routing := slices.Index(records[0], "routing_policy")
			if routing < 0 || records[0][routing+1] != tc.wantHeader || records[1][routing+1] != tc.wantValue {
				t.Fatalf("column after routing_policy = %q, %q", records[0][routing+1], records[1][routing+1])
			}
			if len(records[0]) != len(records[1]) {
				t.Fatalf("header has %d columns, row has %d", len(records[0]), len(records[1]))
			}
		})
	}
	for _, tc := range []struct {
		name       string
		column     bool
		wantHeader bool
	}{
		{"table without column", false, false},
		{"table with column", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var table bytes.Buffer
			if err := writeReport(writeReportInput{output: &table, format: "table", results: results, waitRuleColumn: tc.column}); err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(table.String(), "\n")
			header, row := lines[1], lines[2]
			if got := strings.Contains(header, "WAIT RULE"); got != tc.wantHeader {
				t.Fatalf("header has WAIT RULE = %t, want %t:\n%s", got, tc.wantHeader, table.String())
			}
			// The table is aligned, so each value starts at the column of
			// its header. A header column with no row value moves the
			// later values out of line.
			if strings.Index(header, "WAIT AVG") != strings.Index(row, "12.34") {
				t.Fatalf("WAIT AVG column is not aligned with its value:\n%s", table.String())
			}
			if tc.column && strings.Index(header, "WAIT RULE") != strings.Index(row, "strict") {
				t.Fatalf("WAIT RULE column is not aligned with its value:\n%s", table.String())
			}
		})
	}
}

// TestWaitRulesAddArms runs two schedules on which the wait rules give
// different results. With seed 5 the current rule differs from strict and
// none. With seed 9 the none rule differs from current and strict. So each
// rule must reach the simulation.
func TestWaitRulesAddArms(t *testing.T) {
	t.Parallel()
	args := []string{
		"-duration", "3m", "-arrivals-for", "2m", "-request-every", "20s", "-seeds", "5,9",
		"-pattern", "balanced", "-redistribution-policies", "off",
	}
	caseStudy, err := loadScenario("", "")
	if err != nil {
		t.Fatal(err)
	}
	defaults, err := parseOptions(args, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	withRules, err := parseOptions(append(slices.Clone(args), "-wait-rules", "current,strict,none"), &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	base, err := compare(defaults, caseStudy)
	if err != nil {
		t.Fatal(err)
	}
	arms, err := compare(withRules, caseStudy)
	if err != nil {
		t.Fatal(err)
	}
	if len(base) != 2 || len(arms) != 6 {
		t.Fatalf("got %d default and %d wait rule results", len(base), len(arms))
	}
	rules := []string{"current", "strict", "none"}
	for index, arm := range arms {
		seedArm := base[index/len(rules)]
		if arm.WaitRule != rules[index%len(rules)] || arm.ScheduleID != seedArm.ScheduleID {
			t.Fatalf("arm %d = %q with schedule %s", index, arm.WaitRule, arm.ScheduleID)
		}
		arm.WaitRule = ""
		arms[index] = arm
	}
	for _, tc := range []struct {
		name  string
		left  result
		right result
		equal bool
	}{
		{"seed 5 current equals the default", arms[0], base[0], true},
		{"seed 5 strict differs from current", arms[1], arms[0], false},
		{"seed 5 none equals strict", arms[2], arms[1], true},
		{"seed 9 current equals the default", arms[3], base[1], true},
		{"seed 9 strict equals current", arms[4], arms[3], true},
		{"seed 9 none differs from strict", arms[5], arms[4], false},
	} {
		if got := reflect.DeepEqual(tc.left, tc.right); got != tc.equal {
			t.Errorf("%s: equal = %t, want %t:\n%+v\n%+v", tc.name, got, tc.equal, tc.left, tc.right)
		}
	}
}

// TestWaitRulesCountInMatrixLimit checks that each wait rule counts as a
// separate arm in the matrix limit.
func TestWaitRulesCountInMatrixLimit(t *testing.T) {
	t.Parallel()
	seeds := func(count int) string {
		values := make([]string, count)
		for index := range values {
			values[index] = strconv.Itoa(index + 1)
		}
		return strings.Join(values, ",")
	}
	// 100 seeds and 4 loads give 400 comparisons for each wait rule.
	const loads = "10s,20s,30s,40s"
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"one rule stays under the limit", []string{"-seeds", seeds(100), "-loads", loads, "-wait-rules", "current"}, ""},
		{"three rules go over the limit", []string{"-seeds", seeds(100), "-loads", loads, "-wait-rules", "current,strict,none"}, "at most"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseOptions(tc.args, &bytes.Buffer{})
			if (err == nil) != (tc.want == "") || err != nil && !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("parseOptions() error = %v, want text %q", err, tc.want)
			}
		})
	}
	// Two profile bands double the arms after parsing, so only compare()
	// can find that the matrix is too large.
	caseStudy, err := loadScenario("", "")
	if err != nil {
		t.Fatal(err)
	}
	caseStudy.demand = project.DemandConfig{Pattern: "profile", Profile: "test", Band: "peak"}
	caseStudy.demandProfiles = []project.DemandProfile{{
		ID: "test", Name: "Test profile",
		Bands: []project.DemandBand{{ID: "peak", Name: "Peak"}, {ID: "quiet", Name: "Quiet"}},
		Flows: []project.DemandFlow{{From: "harbor", To: "market", Weights: []float64{1, 1}}},
	}}
	opts, err := parseOptions([]string{
		"-duration", "1m", "-request-every", "30s", "-pattern", "profile", "-bands", "all",
		"-loads", "10s,20s", "-redistribution-policies", "off", "-seeds", seeds(100), "-wait-rules", "current,strict,none",
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compare(opts, caseStudy); err == nil || !strings.Contains(err.Error(), "expanded matrix") {
		t.Fatalf("compare() error = %v, want the expanded matrix limit", err)
	}
}
