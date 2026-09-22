package scenarios

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

const (
	lookaheadExperimentOrders = 100
	lookaheadExperimentTicks  = 3600 * sim.TicksPerSecond
)

type lookaheadResult struct {
	firstComplete, lastComplete, allIdle int64
	peakStopped                          int
	entryBlocks, exitBlocks              int
	entryStops, exitStops                int
	entryBlockedTicks, exitBlockedTicks  int64
	minimumGap                           float64
	berthSpread                          int
}

type boundaryState struct {
	entryBlocked, exitBlocked bool
	entryStopped, exitStopped bool
	activity                  sim.Activity
}

// BenchmarkReservationLookahead compares station flow without extending the
// normal scenario test suite. Run it with -benchtime=1x.
func BenchmarkReservationLookahead(b *testing.B) {
	for _, seconds := range []float64{2.0 / sim.TicksPerSecond, 0.25, 0.5, 1, 2} {
		b.Run(fmt.Sprintf("%.3fs", seconds), func(b *testing.B) {
			for b.Loop() {
				result := runLookaheadExperiment(b, seconds)
				reportLookaheadResult(b, result)
			}
		})
	}
}

func runLookaheadExperiment(tb testing.TB, seconds float64) lookaheadResult {
	tb.Helper()
	config := Scale100()
	simulation := newSimulation(tb, config)
	if err := simulation.SetReservationLookahead(seconds); err != nil {
		tb.Fatal(err)
	}
	var origins []string
	for _, station := range config.Network.Stations {
		if !station.ParkingOnly && station.ID != "station-19" {
			origins = append(origins, station.ID)
		}
	}

	result := lookaheadResult{minimumGap: math.Inf(1)}
	previous := make(map[string]boundaryState, len(config.Fleet))
	berthUses := make(map[string]int)
	submitted, previousCompleted := 0, 0
	for tick := range lookaheadExperimentTicks {
		if submitted < lookaheadExperimentOrders && tick%(5*sim.TicksPerSecond) == 0 {
			if err := simulation.RequestTrip(origins[submitted%len(origins)], "station-19"); err != nil {
				tb.Fatal(err)
			}
			submitted++
		}
		simulation.Step()
		state := simulation.SafetyObservation()
		gap, err := scaleSafety(state)
		if err != nil {
			tb.Fatal(err)
		}
		result.minimumGap = min(result.minimumGap, gap)
		active, stopped := observeLookaheadTick(state, previous, berthUses, &result)
		result.peakStopped = max(result.peakStopped, stopped)
		if state.Completed != previousCompleted {
			if result.firstComplete == 0 {
				result.firstComplete = state.Tick
			}
			result.lastComplete = state.Tick
			previousCompleted = state.Completed
		}
		if state.Completed == lookaheadExperimentOrders && state.Pending == 0 && active == 0 {
			result.allIdle = state.Tick
			result.berthSpread = useSpread(config.Network.Stations, berthUses)
			return result
		}
	}
	state := simulation.Snapshot()
	tb.Fatalf("lookahead %.3fs did not drain: completed=%d submitted=%d pending=%d", seconds, state.Completed, submitted, len(state.Pending))
	return result
}

func observeLookaheadTick(state sim.SafetyObservation, previous map[string]boundaryState, berthUses map[string]int, result *lookaheadResult) (int, int) {
	active, stopped := 0, 0
	for _, pod := range state.Pods {
		if pod.Activity != sim.Idle {
			active++
		}
		if pod.Activity == sim.Traveling && pod.Speed < 0.01 {
			stopped++
		}
		prior := previous[pod.ID]
		entry := pod.Activity == sim.Traveling && entryBoundaryLane(pod.LaneID) && pod.WaitReason != sim.NoWait
		exit := pod.Activity == sim.Traveling && exitBoundaryLane(pod.LaneID) && pod.WaitReason != sim.NoWait
		entryStopped := entry && pod.Speed < 0.01
		exitStopped := exit && pod.Speed < 0.01
		if entry && !prior.entryBlocked {
			result.entryBlocks++
		}
		if exit && !prior.exitBlocked {
			result.exitBlocks++
		}
		if entryStopped && !prior.entryStopped {
			result.entryStops++
		}
		if exitStopped && !prior.exitStopped {
			result.exitStops++
		}
		if entry {
			result.entryBlockedTicks++
		}
		if exit {
			result.exitBlockedTicks++
		}
		if pod.Activity == sim.Unloading && prior.activity != sim.Unloading && pod.StationID == "station-19" {
			berthUses[pod.BerthID]++
		}
		previous[pod.ID] = boundaryState{
			entryBlocked: entry, exitBlocked: exit,
			entryStopped: entryStopped, exitStopped: exitStopped,
			activity: pod.Activity,
		}
	}
	return active, stopped
}

func entryBoundaryLane(lane string) bool {
	return lane == "mesh-in-19" || strings.HasPrefix(lane, "s19-arrival-") || strings.HasPrefix(lane, "s19-in-")
}

func exitBoundaryLane(lane string) bool {
	return lane == "mesh-out-19" || strings.HasPrefix(lane, "s19-departure-") || strings.HasPrefix(lane, "s19-out-")
}

func useSpread(stations []sim.Station, uses map[string]int) int {
	minimum, maximum := math.MaxInt, 0
	for _, station := range stations {
		if station.ID != "station-19" {
			continue
		}
		for _, berth := range station.Berths {
			minimum = min(minimum, uses[berth.ID])
			maximum = max(maximum, uses[berth.ID])
		}
	}
	return maximum - minimum
}

func reportLookaheadResult(b *testing.B, result lookaheadResult) {
	b.Helper()
	b.ReportMetric(float64(result.firstComplete)/sim.TicksPerSecond, "first_s")
	b.ReportMetric(float64(result.lastComplete)/sim.TicksPerSecond, "last_s")
	b.ReportMetric(float64(result.allIdle)/sim.TicksPerSecond, "idle_s")
	b.ReportMetric(float64(result.peakStopped), "peak_stopped")
	b.ReportMetric(float64(result.entryBlocks), "entry_blocks")
	b.ReportMetric(float64(result.exitBlocks), "exit_blocks")
	b.ReportMetric(float64(result.entryStops), "entry_stops")
	b.ReportMetric(float64(result.exitStops), "exit_stops")
	b.ReportMetric(float64(result.entryBlockedTicks)/sim.TicksPerSecond, "entry_block_s")
	b.ReportMetric(float64(result.exitBlockedTicks)/sim.TicksPerSecond, "exit_block_s")
	b.ReportMetric(result.minimumGap, "min_gap_m")
	b.ReportMetric(float64(result.berthSpread), "berth_spread")
}
