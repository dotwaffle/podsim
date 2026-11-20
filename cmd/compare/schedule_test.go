package main

import (
	randv2 "math/rand/v2"
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
