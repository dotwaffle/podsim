package main

import (
	"bytes"
	randv2 "math/rand/v2"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
)

// TestProfileScheduleSmallestWeight checks a valid profile with one flow of
// the smallest positive weight. Without a scale, a draw above 0.5 times
// that weight rounds to the total, no cumulative weight is above it, and
// the search runs past the flows.
func TestProfileScheduleSmallestWeight(t *testing.T) {
	t.Parallel()
	const weight = 5e-324
	config := project.Default()
	base, err := newScenario(config, "")
	if err != nil {
		t.Fatal(err)
	}
	from, to := base.passengers[0], base.passengers[1]
	profile := project.DemandProfile{ID: "tiny", Name: "Tiny",
		Bands: []project.DemandBand{{ID: "b", Name: "B", DurationMinutes: 60}},
		Flows: []project.DemandFlow{{From: from, To: to, Weights: []float64{weight}}}}
	config.DemandProfiles = []project.DemandProfile{profile}
	if err := project.Validate(config); err != nil {
		t.Fatalf("the profile is not valid: %v", err)
	}
	// Find a seed whose first draw rounds up to the total, as the schedule
	// draws it.
	var seed int64
	for candidate := int64(1); seed == 0; candidate++ {
		bits := uint64(candidate)
		if draw := randv2.New(randv2.NewPCG(bits, ^bits)).Float64(); draw > 0.5 && draw*weight == weight {
			seed = candidate
		}
	}
	flows := weightedProfileFlows(profile, 0)
	if len(flows) != 1 || flows[0].cumulative != 0.5 {
		t.Fatalf("flows %+v", flows)
	}
	requests := profileDemandSchedule(scheduleInput{seed: seed, durationTicks: 20, intervalTicks: 10, pattern: "profile", profileFlows: flows})
	if len(requests) != 1 || requests[0].origin != from || requests[0].destination != to {
		t.Fatalf("seed %d: requests %+v", seed, requests)
	}
}

// TestProfileFlowSubnormalShares checks the shares of flows with subnormal
// weights over a uniform grid of draws. Without a scale, the products of
// the draws round to a few values, and two equal weights get 25% and 75%
// of the draws.
func TestProfileFlowSubnormalShares(t *testing.T) {
	t.Parallel()
	const draws = 100_000
	for _, test := range []struct {
		weights []float64
		want    []int
	}{
		{[]float64{5e-324, 5e-324}, []int{50_000, 50_000}},
		{[]float64{5e-324, 1.5e-323}, []int{25_000, 75_000}},
		{[]float64{1e-310, 3e-310}, []int{25_000, 75_000}},
	} {
		profile := project.DemandProfile{Flows: []project.DemandFlow{{From: "a", To: "b", Weights: test.weights[:1]}, {From: "b", To: "a", Weights: test.weights[1:]}}}
		flows := weightedProfileFlows(profile, 0)
		got := make([]int, len(flows))
		for index := range draws {
			flow := profileFlowAt(flows, (float64(index)+0.5)/draws)
			if flow.from == "a" {
				got[0]++
			} else {
				got[1]++
			}
		}
		if got[0] != test.want[0] || got[1] != test.want[1] {
			t.Errorf("weights %v: %v draws, want %v", test.weights, got, test.want)
		}
	}
}

// TestProfileScheduleOrdinaryTotal pins the profile schedules of
// LondonFull. A total at or above minProfileDrawTotal has no scale, so its
// schedules are the same as before the scale.
func TestProfileScheduleOrdinaryTotal(t *testing.T) {
	t.Parallel()
	source := scenarios.LondonFull()
	base, err := newScenario(source, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, pin := range []struct {
		band int
		seed int64
		id   string
	}{
		{0, 1, "71781bad4f84ecd4"}, {0, 7, "b17ff887d25a642c"}, {2, 1, "64ba1f6bd26c7b6d"}, {2, 7, "d4fad3101614389c"},
	} {
		requests := demandSchedule(scheduleInput{seed: pin.seed, durationTicks: 3600 * 30, intervalTicks: 30, pattern: "profile",
			passengers: base.passengers, focus: base.focus, profileFlows: weightedProfileFlows(source.DemandProfiles[0], pin.band)})
		if id := scheduleID(requests); len(requests) != 3599 || id != pin.id {
			t.Errorf("band %d seed %d: %d requests, ID %s, want 3599, %s", pin.band, pin.seed, len(requests), id, pin.id)
		}
	}
}

// TestScheduleRequestBytes checks the size that the schedule bound charges
// for each request.
func TestScheduleRequestBytes(t *testing.T) {
	t.Parallel()
	if size := reflect.TypeFor[scheduledRequest]().Size(); size != scheduleRequestBytes {
		t.Fatalf("a scheduled request has %d bytes, and the bound charges %d", size, scheduleRequestBytes)
	}
}

// TestIntervalRequestCount checks that the bound counts the requests that
// the interval schedules hold.
func TestIntervalRequestCount(t *testing.T) {
	t.Parallel()
	base, err := newScenario(project.Default(), "")
	if err != nil {
		t.Fatal(err)
	}
	flows := []weightedDemandFlow{{from: base.passengers[0], to: base.passengers[1], cumulative: 1}}
	for _, pattern := range []string{"balanced", "bursty-hotspot", "profile"} {
		for _, window := range [][2]int64{{1, 1}, {10, 1}, {10, 3}, {12, 3}, {13, 3}, {1800, 7}} {
			requests := demandSchedule(scheduleInput{seed: 1, durationTicks: window[0], intervalTicks: window[1], burstSize: 3, pattern: pattern,
				passengers: base.passengers, focus: base.focus, profileFlows: flows})
			if got := intervalRequestCount(window[0], window[1]); int64(len(requests)) != got {
				t.Errorf("%s, window %v: %d requests, bound counts %d", pattern, window, len(requests), got)
			}
		}
	}
}

// seedList returns the seeds 1 to n as a -seeds value.
func seedList(n int) string {
	seeds := make([]string, n)
	for index := range seeds {
		seeds[index] = strconv.Itoa(index + 1)
	}
	return strings.Join(seeds, ",")
}

// TestScheduleBytesBound checks the bound at its limit for each count that
// it multiplies. One seed of 24h at one request a second holds 86,399
// requests, so 32 seeds fit in 256 MiB and 33 do not. A load of 34ms is
// one tick, and one such schedule holds 2,591,999 requests.
func TestScheduleBytesBound(t *testing.T) {
	t.Parallel()
	const refusal = "the request schedules must fit in 256 MiB"
	for _, test := range []struct {
		name   string
		args   []string
		refuse bool
	}{
		{"32 seeds", []string{"-request-every", "1s", "-seeds", seedList(32)}, false},
		{"33 seeds", []string{"-request-every", "1s", "-seeds", seedList(33)}, true},
		{"one load", []string{"-loads", "34ms"}, false},
		{"two loads", []string{"-loads", "34ms,40ms"}, true},
		{"one pattern", []string{"-request-every", "34ms", "-patterns", "balanced"}, false},
		{"two patterns", []string{"-request-every", "34ms", "-patterns", "balanced,hotspot"}, true},
		{"a shorter arrival window", []string{"-request-every", "34ms", "-arrivals-for", "12h", "-patterns", "balanced,hotspot"}, false},
		{"a daily pattern", []string{"-pattern", "profile-daily", "-seeds", seedList(100)}, false},
		{"a rail pattern", []string{"-pattern", "rail-services", "-seeds", seedList(100)}, false},
	} {
		_, err := parseOptions(append([]string{"-duration", "24h"}, test.args...), &bytes.Buffer{})
		if refused := err != nil && strings.HasPrefix(err.Error(), refusal); refused != test.refuse || err != nil && !refused {
			t.Errorf("%s: %v", test.name, err)
		}
	}
}

// TestScheduleBytesRefusal checks the matrix of the audit, about 23 GiB of
// requests, which the command refuses as a flag error before it allocates
// a schedule.
func TestScheduleBytesRefusal(t *testing.T) {
	t.Parallel()
	var stderr bytes.Buffer
	code := runCLI(cliInput{args: []string{"-duration", "24h", "-request-every", "34ms", "-seeds", seedList(100)}, stdout: &bytes.Buffer{}, stderr: &stderr})
	const want = "the request schedules must fit in 256 MiB, and this matrix needs 23731 MiB"
	if code != 2 || !strings.HasPrefix(stderr.String(), want) {
		t.Fatalf("exit %d, %q; want 2, %q", code, stderr.String(), want)
	}
}

// TestScheduleBytesProfileBands checks that compare counts each profile
// band. Before the project loads, the profile pattern counts as one arm.
func TestScheduleBytesProfileBands(t *testing.T) {
	t.Parallel()
	opts, err := parseOptions([]string{"-pattern", "profile", "-bands", "am-peak,pm-peak", "-duration", "24h", "-request-every", "1s", "-seeds", seedList(32), "-workers", "1"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	london, err := newScenario(scenarios.LondonFull(), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compare(opts, london); err == nil || !strings.HasPrefix(err.Error(), "the request schedules must fit in 256 MiB, and this matrix needs 507 MiB") {
		t.Fatalf("two bands of 32 seeds: %v", err)
	}
}
