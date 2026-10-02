package main

import (
	"bytes"
	"math"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

func TestPercentile95(t *testing.T) {
	t.Parallel()
	sequence := func(n int) []int64 {
		values := make([]int64, n)
		for index := range values {
			values[index] = int64(index + 1)
		}
		return values
	}
	for _, test := range []struct {
		name   string
		values []int64
		want   int64
	}{
		{name: "empty", want: 0},
		{name: "one value", values: []int64{7}, want: 7},
		// The nearest rank of 20 values is 19.
		{name: "twenty values", values: sequence(20), want: 19},
		// The nearest rank of 21 values is 20, because 0.95 * 21 is 19.95.
		{name: "twenty-one values", values: sequence(21), want: 20},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := percentile95(test.values); got != test.want {
				t.Fatalf("percentile95 = %d, want %d", got, test.want)
			}
		})
	}
}

// TestRequestTimeStats uses these parties, with ticks at 60 each second:
//
//	request 1: wait 1 s, journey 10 s.
//	request 2: wait 2 s, journey 20 s.
//	request 3 joins the pod of request 2: wait 1 s, journey 19 s.
//	request 4: wait 5 s, not complete.
//	request 5: pending at 30 s since 20 s, so its wait is 10 s.
//
// The five waits sorted are 1, 1, 2, 5 and 10 s, and ceil(0.95 * 5) = 5.
// The journeys sorted are 10, 19 and 20 s, and ceil(0.95 * 3) = 3.
// The pod journeys of requests 1 and 2 are 500 m and 1,000 m, and three
// parties rode them, so the occupancy is (500 + 1,000 + 1,000) / 1,500.
func TestRequestTimeStats(t *testing.T) {
	t.Parallel()
	timings := []sim.RequestTiming{
		{RequestID: 1, RequestedTick: 0, BoardedTick: 60, CompletedTick: 600, RiddenMeters: 500},
		{RequestID: 2, RequestedTick: 120, BoardedTick: 240, CompletedTick: 1320, RiddenMeters: 1000},
		{RequestID: 3, RequestedTick: 180, BoardedTick: 240, CompletedTick: 1320, RiddenMeters: 1000, SharedWith: 2},
		{RequestID: 4, RequestedTick: 600, BoardedTick: 900, CompletedTick: -1},
	}
	state := sim.Snapshot{Tick: 1800, Pending: []sim.Request{{ID: 5, RequestedTick: 1200}}}
	got := requestTimeStats(timings, state)
	want := requestStats{waitP95: 10, journeyAverage: 49.0 / 3, journeyP95: 20, journeyMaximum: 20, occupancy: 2500.0 / 1500}
	if got != want {
		t.Fatalf("requestTimeStats = %+v, want %+v", got, want)
	}
	if empty := requestTimeStats(nil, sim.Snapshot{}); empty != (requestStats{}) {
		t.Fatalf("stats without requests = %+v", empty)
	}
}

func TestJourneyColumnsInRun(t *testing.T) {
	t.Parallel()
	opts, err := parseOptions([]string{
		"-duration", "4m", "-arrivals-for", "1m", "-request-every", "5s", "-burst-size", "6", "-pattern", "hub-burst",
		"-sharing-consent", "shared", "-sharing-limits", "1,2", "-redistribution-policies", "off",
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
	for _, outcome := range results {
		if outcome.Served == 0 || outcome.JourneyAverageSeconds <= 0 || outcome.JourneyP95Seconds > outcome.JourneyMaximumSeconds ||
			outcome.JourneyAverageSeconds > outcome.JourneyMaximumSeconds {
			t.Fatalf("journey columns = %+v", outcome)
		}
		if outcome.WaitP95Seconds > outcome.WaitMaximumSeconds || outcome.WaitP95Seconds < 0 {
			t.Fatalf("wait columns = %+v", outcome)
		}
	}
}

// TestWaitSetMatchesSimulation checks that the p95 wait uses the set of
// waits of the simulation average: each boarded party and each pending
// request.
func TestWaitSetMatchesSimulation(t *testing.T) {
	t.Parallel()
	caseStudy, err := loadScenario("", "market")
	if err != nil {
		t.Fatal(err)
	}
	simulation, err := sim.NewFleet(caseStudy.network, caseStudy.fleet)
	if err != nil {
		t.Fatal(err)
	}
	simulation.SetExperimentRecords(true)
	if err := simulation.SetSharedRidePartyLimit(2); err != nil {
		t.Fatal(err)
	}
	for second := range 90 {
		if second%5 == 0 {
			if _, err := simulation.SubmitTripOptions(sim.TripOptions{From: "market", To: caseStudy.passengers[second/5%2], SharingConsent: sim.SharedConsent}); err != nil {
				t.Fatal(err)
			}
		}
		for range sim.TicksPerSecond {
			simulation.Step()
		}
	}
	state := simulation.Snapshot()
	timings := simulation.RequestTimings()
	if len(state.Pending) == 0 || len(timings) == 0 || state.SharedParties == 0 {
		t.Fatalf("the fixture needs boarded, shared and pending requests: timings=%d %+v", len(timings), state)
	}
	total := int64(0)
	for _, timing := range timings {
		total += timing.BoardedTick - timing.RequestedTick
	}
	for _, request := range state.Pending {
		total += state.Tick - request.RequestedTick
	}
	average := tickSeconds(total) / float64(len(timings)+len(state.Pending))
	if math.Abs(average-state.Wait.AverageSeconds) > 1e-9 {
		t.Fatalf("wait average from the timings = %v, simulation = %v", average, state.Wait.AverageSeconds)
	}
}

// TestOccupancyInRun checks the occupancy of a hub burst on the small ring.
// Without sharing, each pod journey has one party. With a limit of 4, the
// parties that join a pod raise the occupancy above 1, but not above 4.
func TestOccupancyInRun(t *testing.T) {
	t.Parallel()
	input := smallBurstInput(t)
	input.sharingConsent = sim.SharedConsent
	single, err := run(input)
	if err != nil {
		t.Fatal(err)
	}
	input.sharingLimit = 4
	shared, err := run(input)
	if err != nil {
		t.Fatal(err)
	}
	if single.Occupancy != 1 || shared.SharedParties == 0 || shared.Occupancy <= 1 || shared.Occupancy > 4 {
		t.Fatalf("occupancy without sharing = %v, with sharing = %v and %d shared parties", single.Occupancy, shared.Occupancy, shared.SharedParties)
	}
	t.Logf("occupancy with sharing = %v", shared.Occupancy)
}

// TestIntermediateStops checks the count of the stops before the last stop
// of each pod journey.
func TestIntermediateStops(t *testing.T) {
	t.Parallel()
	timings := []sim.RequestTiming{
		// A pod journey with stops at ticks 100 and 200 that ended.
		{RequestID: 1, CompletedTick: 200, RiddenMeters: 20},
		{RequestID: 2, SharedWith: 1, CompletedTick: 100, RiddenMeters: 10},
		{RequestID: 3, SharedWith: 1, CompletedTick: 200, RiddenMeters: 20},
		// A pod journey with one stop and a party still aboard.
		{RequestID: 4, CompletedTick: -1},
		{RequestID: 5, SharedWith: 4, CompletedTick: 150, RiddenMeters: 5},
		// A pod journey without sharing.
		{RequestID: 6, CompletedTick: 300, RiddenMeters: 30},
	}
	if got := requestTimeStats(timings, sim.Snapshot{}).intermediateStops; got != 2 {
		t.Fatalf("intermediate stops = %d, want 2", got)
	}
}
