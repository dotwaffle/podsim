package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
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
		{name: "unknown sharing mode", args: []string{"-sharing-modes", "pickups"}, want: "unknown sharing mode"},
		{name: "duplicate sharing mode", args: []string{"-sharing-modes", "drop-offs,drop-offs"}, want: "more than once"},
		{name: "zero sharing stops", args: []string{"-sharing-max-stops", "0"}, want: "sharing-max-stops"},
		{name: "too many sharing stops", args: []string{"-sharing-max-stops", "8"}, want: "sharing-max-stops"},
		{name: "unknown routing policy", args: []string{"-routing-policies", "fast"}, want: "unknown routing policy"},
		{name: "duplicate routing policy", args: []string{"-routing-policies", "free-flow,free-flow"}, want: "more than once"},
		{name: "unknown redistribution policy", args: []string{"-redistribution-policies", "maybe"}, want: "unknown redistribution policy"},
		{name: "duplicate redistribution policy", args: []string{"-redistribution-policies", "off,off"}, want: "more than once"},
		{name: "duplicate on policy", args: []string{"-redistribution-policies", "on, off,on"}, want: "more than once"},
		{name: "retired guarded policy", args: []string{"-redistribution-policies", "off,guarded"}, want: "unknown redistribution policy"},
		{name: "unknown wait rule", args: []string{"-wait-rules", "lenient"}, want: "unknown wait rule"},
		{name: "empty wait rule", args: []string{"-wait-rules", "current,"}, want: "unknown wait rule"},
		{name: "duplicate wait rule", args: []string{"-wait-rules", "strict,strict"}, want: "more than once"},
		{name: "unknown platoon policy", args: []string{"-platoon-policies", "coupled"}, want: "unknown platoon policy"},
		{name: "duplicate platoon policy", args: []string{"-platoon-policies", "virtual,virtual"}, want: "more than once"},
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

// TestComparePairsSharingModes checks that a limit of 1 runs one time and
// that each other limit runs in each mode. In the example, the route from
// harbor to market passes garden, so drop-offs makes intermediate stops.
func TestComparePairsSharingModes(t *testing.T) {
	t.Parallel()
	opts, err := parseOptions([]string{
		"-duration", "20m", "-arrivals-for", "2m", "-request-every", "10s", "-pattern", "balanced", "-sharing-limits", "1,4",
		"-sharing-modes", "destination,drop-offs", "-sharing-max-stops", "2", "-redistribution-policies", "off",
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
	var arms []string
	for _, outcome := range results {
		arms = append(arms, strconv.Itoa(outcome.SharedRidePartyLimit)+" "+outcome.SharingMode)
		if outcome.SharingMode == string(sim.SharedRideDestination) && (outcome.IntermediateStops != 0 || outcome.DetourRatioMax > 1+1e-9) {
			t.Fatalf("destination mode made stops: %+v", outcome)
		}
	}
	if want := []string{"1 destination", "4 destination", "4 drop-offs"}; !slices.Equal(arms, want) {
		t.Fatalf("arms %v, want %v", arms, want)
	}
	if results[2].IntermediateStops == 0 || results[2].DetourRatioMax <= 1 {
		t.Fatalf("drop-offs made no intermediate stop: %+v", results[2])
	}
}

func TestComparePairsRoutingPolicies(t *testing.T) {
	t.Parallel()
	opts, err := parseOptions([]string{
		"-duration", "2m", "-request-every", "20s", "-routing-policies", "free-flow,congestion,queue",
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
	if len(results) != 6 {
		t.Fatalf("routing comparison has %d arms, want 6", len(results))
	}
	for index, want := range []string{"free-flow", "free-flow", "congestion", "congestion", "queue", "queue"} {
		if results[index].RoutingPolicy != want {
			t.Fatalf("routing comparison arm %d has policy %q, want %q", index, results[index].RoutingPolicy, want)
		}
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
	if decoded.SchemaVersion != 9 || !reflect.DeepEqual(decoded.Results, results) {
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
	kept := slices.ContainsFunc(state.Vehicles, func(vehicle sim.Vehicle) bool { return len(vehicle.Riders) > 0 })
	if !kept {
		t.Fatal("no pod keeps its completed request, so the test does not cover that case")
	}
	if got := state.WorkingVehicles(); got != 0 {
		t.Fatalf("WorkingVehicles() after the trip = %d, want 0", got)
	}
}

func TestParseRedistributionPolicies(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		args []string
		want []string
	}{
		{"flag not given", nil, []string{"off", "on"}},
		{"on only", []string{"-redistribution-policies", "on"}, []string{"on"}},
		{"all policies in order", []string{"-redistribution-policies", "on, off"}, []string{"on", "off"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts, err := parseOptions(tc.args, &bytes.Buffer{})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(opts.redistributionPolicies, tc.want) {
				t.Fatalf("redistribution policies = %#v, want %#v", opts.redistributionPolicies, tc.want)
			}
		})
	}
}

// policyArms runs the off and on policies with the given arguments, as
// the command does. It returns the results with the policy names cleared.
func policyArms(t *testing.T, args []string) (off, on result) {
	t.Helper()
	opts, err := parseOptions(append(slices.Clone(args), "-redistribution-policies", "off,on"), &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	caseStudy, err := loadScenario(opts.projectPath, opts.focus)
	if err != nil {
		t.Fatal(err)
	}
	results, err := compare(opts, caseStudy)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	for index, want := range []string{"off", "on"} {
		if results[index].Policy != want {
			t.Fatalf("result %d policy = %q, want %q", index, results[index].Policy, want)
		}
		results[index].Policy = ""
	}
	return results[0], results[1]
}

// TestOnPolicyMatchesOffAtHighRate runs each policy on the example fleet
// of two pods. A request each 20 s is above the guarded gate limit of one
// request per minute for each 20 pods. Thus the on arm must equal the off
// arm apart from its policy name.
func TestOnPolicyMatchesOffAtHighRate(t *testing.T) {
	t.Parallel()
	off, on := policyArms(t, []string{
		"-duration", "4m", "-arrivals-for", "3m", "-request-every", "20s", "-seed", "7", "-pattern", "balanced",
	})
	if !reflect.DeepEqual(on, off) {
		t.Fatalf("on arm differs from the off arm:\n%+v\n%+v", on, off)
	}
}

// smallProject writes the small qualification ring to a project file and
// returns its path. The ring has 12 pods and stations with four berths.
func smallProject(t *testing.T) string {
	t.Helper()
	data, err := json.Marshal(scenarios.Small())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "small.json")
	if err = os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestOnPolicyMovesPodsAtLowRate runs each policy on the small
// qualification ring. A request each 3 minutes is below the guarded gate
// limit, so the on arm must make positioning moves and differ from the off
// arm.
func TestOnPolicyMovesPodsAtLowRate(t *testing.T) {
	t.Parallel()
	off, on := policyArms(t, []string{
		"-project", smallProject(t), "-duration", "20m", "-request-every", "3m", "-seed", "1", "-pattern", "hotspot",
	})
	if on.PositioningMoveCount == 0 {
		t.Fatal("the on arm made no positioning moves")
	}
	if reflect.DeepEqual(on, off) {
		t.Fatalf("on arm equals the off arm:\n%+v", on)
	}
}

// TestOnPolicyStopsAtSkippedArrival runs each policy on the small
// qualification ring with a queue limit of one request. A request each 90 s
// is above the guarded gate limit of 0.6 requests per minute for 12 pods,
// but some arrivals are skipped, so the rate of the accepted requests is
// below the limit. The on arm must still equal the off arm apart from its
// policy name.
func TestOnPolicyStopsAtSkippedArrival(t *testing.T) {
	t.Parallel()
	off, on := policyArms(t, []string{
		"-project", smallProject(t), "-duration", "30m", "-arrivals-for", "20m", "-request-every", "90s", "-seed", "1", "-pattern", "hotspot", "-queue-limit", "1",
	})
	if off.Skipped == 0 {
		t.Fatal("the off arm skipped no arrival")
	}
	if !reflect.DeepEqual(on, off) {
		t.Fatalf("on arm differs from the off arm:\n%+v\n%+v", on, off)
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

func TestParsePlatoonPolicies(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		args []string
		want []string
	}{
		{"flag not given", nil, nil},
		{"default value given", []string{"-platoon-policies", "off"}, []string{"off"}},
		{"both policies in order", []string{"-platoon-policies", "virtual, off"}, []string{"virtual", "off"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts, err := parseOptions(tc.args, &bytes.Buffer{})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(opts.platoonPolicies, tc.want) || (opts.platoonPolicies == nil) != (tc.want == nil) {
				t.Fatalf("platoon policies = %#v, want %#v", opts.platoonPolicies, tc.want)
			}
		})
	}
}

// TestPlatoonColumnOnlyWhenRequested checks that the platoon policy column
// follows routing_policy and wait_rule only with -platoon-policies, and that
// coupled_time_percent is always the last CSV column.
func TestPlatoonColumnOnlyWhenRequested(t *testing.T) {
	t.Parallel()
	results := []result{{
		Pattern: "balanced", Policy: "off", RoutingPolicy: "free-flow", WaitRule: "strict", PlatoonPolicy: "virtual",
		WaitAverageSeconds: 12.34, CoupledTimePercent: 5.5,
	}}
	for _, tc := range []struct {
		name               string
		waitRule, platoon  bool
		wantHeader, wantAt string
		offset             int
	}{
		{"csv without columns", false, false, "shared_ride_party_limit", "0", 1},
		{"csv with the platoon column", false, true, "platoon_policy", "virtual", 1},
		{"csv with both columns", true, true, "platoon_policy", "virtual", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var output bytes.Buffer
			if err := writeReport(writeReportInput{output: &output, format: "csv", results: results, waitRuleColumn: tc.waitRule, platoonColumn: tc.platoon}); err != nil {
				t.Fatal(err)
			}
			records, err := csv.NewReader(&output).ReadAll()
			if err != nil {
				t.Fatal(err)
			}
			header, row := records[0], records[1]
			routing := slices.Index(header, "routing_policy")
			if routing < 0 || header[routing+tc.offset] != tc.wantHeader || row[routing+tc.offset] != tc.wantAt {
				t.Fatalf("column %d after routing_policy = %q, %q", tc.offset, header[routing+tc.offset], row[routing+tc.offset])
			}
			if len(header) != len(row) || header[len(header)-1] != "coupled_time_percent" || row[len(row)-1] != "5.5" {
				t.Fatalf("last column = %q, %q with %d and %d columns", header[len(header)-1], row[len(row)-1], len(header), len(row))
			}
		})
	}
	var table bytes.Buffer
	if err := writeReport(writeReportInput{output: &table, format: "table", results: results, waitRuleColumn: true, platoonColumn: true}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(table.String(), "\n")
	header, row := lines[1], lines[2]
	if strings.Index(header, "PLATOON") != strings.Index(row, "virtual") || strings.Index(header, "WAIT AVG") != strings.Index(row, "12.34") {
		t.Fatalf("PLATOON or WAIT AVG column is not aligned with its value:\n%s", table.String())
	}
}

// TestPlatoonPoliciesAddArms runs the busy ring at a load at which pods
// queue. The off arm must equal the arm without the option, and the
// virtual arm must couple pods.
func TestPlatoonPoliciesAddArms(t *testing.T) {
	t.Parallel()
	data, err := json.Marshal(scenarios.Busy())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "busy.json")
	if err = os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{
		"-project", path, "-duration", "10m", "-request-every", "4s", "-seed", "1",
		"-pattern", "balanced", "-redistribution-policies", "off",
	}
	caseStudy, err := loadScenario(path, "")
	if err != nil {
		t.Fatal(err)
	}
	defaults, err := parseOptions(args, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	withPolicies, err := parseOptions(append(slices.Clone(args), "-platoon-policies", "off,virtual"), &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	base, err := compare(defaults, caseStudy)
	if err != nil {
		t.Fatal(err)
	}
	arms, err := compare(withPolicies, caseStudy)
	if err != nil {
		t.Fatal(err)
	}
	if len(base) != 1 || len(arms) != 2 {
		t.Fatalf("got %d default and %d platoon results", len(base), len(arms))
	}
	off, virtual := arms[0], arms[1]
	if off.PlatoonPolicy != "off" || virtual.PlatoonPolicy != "virtual" || virtual.ScheduleID != off.ScheduleID {
		t.Fatalf("arms %q and %q with schedules %s and %s", off.PlatoonPolicy, virtual.PlatoonPolicy, off.ScheduleID, virtual.ScheduleID)
	}
	off.PlatoonPolicy = ""
	if !reflect.DeepEqual(off, base[0]) {
		t.Fatalf("the off arm differs from the default arm:\n%+v\n%+v", off, base[0])
	}
	if base[0].CoupledTimePercent != 0 || virtual.CoupledTimePercent <= 0 {
		t.Fatalf("coupled time %v without platoons and %v with them", base[0].CoupledTimePercent, virtual.CoupledTimePercent)
	}
}

// TestPlatoonPoliciesCountInMatrixLimit checks that each platoon policy
// counts as a separate arm in the matrix limit.
func TestPlatoonPoliciesCountInMatrixLimit(t *testing.T) {
	t.Parallel()
	values := make([]string, 100)
	for index := range values {
		values[index] = strconv.Itoa(index + 1)
	}
	seeds := strings.Join(values, ",")
	// 100 seeds and 6 loads give 600 comparisons for each platoon policy.
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"one policy stays under the limit", []string{"-platoon-policies", "virtual"}, ""},
		{"two policies go over the limit", []string{"-platoon-policies", "off,virtual"}, "at most"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseOptions(append([]string{"-seeds", seeds, "-loads", "10s,20s,30s,40s,50s,60s"}, tc.args...), &bytes.Buffer{})
			if (err == nil) != (tc.want == "") || err != nil && !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("parseOptions() error = %v, want text %q", err, tc.want)
			}
		})
	}
}
