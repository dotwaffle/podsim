// Command compare runs the same demand schedule with redistribution off and on.
package main

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"text/tabwriter"
	"time"

	"github.com/dotwaffle/podsim/internal/sim"
)

const maxDuration = 24 * time.Hour

type scheduledRequest struct {
	tick        int64
	origin      string
	destination string
}

type result struct {
	policy         string
	waitAverage    float64
	waitMaximum    float64
	served         int
	remaining      int
	emptyDistance  float64
	rebalanceMoves int
}

func main() {
	duration := flag.Duration("duration", 30*time.Minute, "simulated comparison duration")
	requestEvery := flag.Duration("request-every", 45*time.Second, "simulated time between requests")
	seed := flag.Int64("seed", 1, "demand schedule seed")
	flag.Parse()
	if *duration <= 0 || *duration > maxDuration {
		fmt.Fprintf(os.Stderr, "duration must be between 0 and %s\n", maxDuration)
		os.Exit(2)
	}
	if *requestEvery <= 0 || *requestEvery > *duration {
		fmt.Fprintln(os.Stderr, "request-every must be positive and no longer than duration")
		os.Exit(2)
	}
	ticks := durationTicks(*duration)
	intervalTicks := durationTicks(*requestEvery)
	if ticks < 1 || intervalTicks < 1 {
		fmt.Fprintln(os.Stderr, "duration and request-every must be at least one simulation tick")
		os.Exit(2)
	}
	schedule := demandSchedule(*seed, ticks, intervalTicks)
	results := make([]result, 0, 2)
	for _, enabled := range []bool{false, true} {
		outcome, err := run(enabled, ticks, schedule)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		results = append(results, outcome)
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "POLICY\tWAIT AVG (S)\tWAIT MAX (S)\tSERVED\tREMAINING\tEMPTY DISTANCE (M)\tREBALANCE MOVES")
	for _, outcome := range results {
		_, _ = fmt.Fprintf(w, "%s\t%.2f\t%.2f\t%d\t%d\t%.1f\t%d\n",
			outcome.policy, outcome.waitAverage, outcome.waitMaximum, outcome.served,
			outcome.remaining, outcome.emptyDistance, outcome.rebalanceMoves)
	}
	if err := w.Flush(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func durationTicks(duration time.Duration) int64 {
	return int64(duration * sim.TicksPerSecond / time.Second)
}

func demandSchedule(seed, durationTicks, intervalTicks int64) []scheduledRequest {
	rng := rand.New(rand.NewSource(seed))
	origins := []string{"market", "market", "market", "market", "market", "market", "harbor", "harbor", "garden", "garden"}
	destinations := map[string][]string{
		"harbor": {"garden", "market"},
		"garden": {"harbor", "market"},
		"market": {"harbor", "garden"},
	}
	var requests []scheduledRequest
	for tick := intervalTicks; tick < durationTicks; tick += intervalTicks {
		origin := origins[rng.Intn(len(origins))]
		choices := destinations[origin]
		requests = append(requests, scheduledRequest{tick: tick, origin: origin, destination: choices[rng.Intn(len(choices))]})
	}
	return requests
}

func run(enabled bool, durationTicks int64, schedule []scheduledRequest) (result, error) {
	simulation, err := sim.NewFleet(sim.Example(), []sim.Placement{
		{ID: "01", StationID: "parking", BerthID: "parking-1"},
		{ID: "02", StationID: "garden"},
	})
	if err != nil {
		return result{}, fmt.Errorf("create comparison: %w", err)
	}
	if err := simulation.SetDemandWeights(map[string]float64{"market": 6, "harbor": 2, "garden": 2}); err != nil {
		return result{}, fmt.Errorf("set demand weights: %w", err)
	}
	simulation.SetRedistribution(enabled)
	next := 0
	for tick := range durationTicks {
		for next < len(schedule) && schedule[next].tick == tick {
			request := schedule[next]
			if err := simulation.RequestTrip(request.origin, request.destination); err != nil {
				return result{}, fmt.Errorf("request %s to %s: %w", request.origin, request.destination, err)
			}
			next++
		}
		simulation.Step()
	}
	state := simulation.Snapshot()
	policy := "off"
	if enabled {
		policy = "on"
	}
	return result{
		policy: policy, waitAverage: state.Wait.AverageSeconds, waitMaximum: state.Wait.MaxSeconds,
		served: state.Completed, remaining: state.Submitted - state.Completed,
		emptyDistance: state.EmptyDistanceMeters, rebalanceMoves: state.RebalanceMoves,
	}, nil
}
