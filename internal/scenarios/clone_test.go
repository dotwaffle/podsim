package scenarios

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

const (
	// The first London trip for this seed completes at 105 s. The 80 s
	// warmup puts that completion inside the replay window, so the test also
	// compares unloading and completion. Of seeds 1 to 64, only this seed
	// completes a trip before 120 s. With the race detector, the test takes
	// about 8 s. A 2-minute warmup takes about 14 s and has no completion in
	// the window.
	londonCloneSeed        = uint64(18)
	londonCloneWarmupTicks = 80 * sim.TicksPerSecond
	londonCloneReplayTicks = 60 * sim.TicksPerSecond
	// The benchmark clones a busier fleet.
	londonBenchmarkWarmupTicks = 2 * 60 * sim.TicksPerSecond
)

var benchmarkClone *sim.Simulation

// londonCloneSchedule requests AM peak journeys at 12 per minute. The last
// request comes before the tick until.
func londonCloneSchedule(until int) []scheduledRequest {
	const interval = 5 * sim.TicksPerSecond
	return londonDemandSchedule(londonCloneSeed, LondonDemand()[2], (until-1)/interval)
}

type scheduledStep struct {
	schedule []scheduledRequest
	tick     int64
}

// stepScheduled submits the requests due at the tick and then advances the
// simulation one tick.
func stepScheduled(simulation *sim.Simulation, step scheduledStep) error {
	for _, request := range step.schedule {
		if request.tick != step.tick {
			continue
		}
		if err := simulation.RequestTrip(request.origin, request.destination); err != nil {
			return fmt.Errorf("request %s to %s at tick %d: %w", request.origin, request.destination, request.tick, err)
		}
	}
	simulation.Step()
	return nil
}

type londonWarmup struct {
	schedule []scheduledRequest
	ticks    int64
}

// warmLondon runs London from the first tick. It fails when no pod is on a
// route at the end.
func warmLondon(tb testing.TB, warmup londonWarmup) *sim.Simulation {
	tb.Helper()
	simulation := newSimulation(tb, London())
	for tick := range warmup.ticks {
		if err := stepScheduled(simulation, scheduledStep{schedule: warmup.schedule, tick: tick}); err != nil {
			tb.Fatal(err)
		}
	}
	for _, vehicle := range simulation.Snapshot().Vehicles {
		if len(vehicle.Route) > 0 {
			return simulation
		}
	}
	tb.Fatal("the London warmup has no active routes")
	return nil
}

// londonObservation holds the exported state at the end of a simulated
// second.
type londonObservation struct {
	snapshot sim.Snapshot
	safety   sim.SafetyObservation
}

type londonRun struct {
	schedule   []scheduledRequest
	start, end int64
	// observe gets the state after each simulated second.
	observe func(londonObservation)
}

// runLondon steps the simulation from the start tick to the end tick.
func runLondon(tb testing.TB, simulation *sim.Simulation, run londonRun) {
	tb.Helper()
	for tick := run.start; tick < run.end; tick++ {
		if err := stepScheduled(simulation, scheduledStep{schedule: run.schedule, tick: tick}); err != nil {
			tb.Fatal(err)
		}
		if (tick+1)%sim.TicksPerSecond == 0 {
			run.observe(londonObservation{snapshot: simulation.Snapshot(), safety: simulation.SafetyObservation()})
		}
	}
}

func TestLondonCloneContinuesExactly(t *testing.T) {
	t.Parallel()
	end := int64(londonCloneWarmupTicks + londonCloneReplayTicks)
	schedule := londonCloneSchedule(int(end))
	source := warmLondon(t, londonWarmup{schedule: schedule, ticks: londonCloneWarmupTicks})
	clone := source.Clone()
	start := source.Snapshot()
	// Run the source to the end before the clone starts. If the clone shares
	// mutable state with its source, the clone then starts from a changed
	// state and the comparison fails.
	var want []londonObservation
	runLondon(t, source, londonRun{
		schedule: schedule, start: londonCloneWarmupTicks, end: end,
		observe: func(observation londonObservation) { want = append(want, observation) },
	})
	second := 0
	runLondon(t, clone, londonRun{
		schedule: schedule, start: londonCloneWarmupTicks, end: end,
		observe: func(got londonObservation) {
			if !reflect.DeepEqual(got.snapshot, want[second].snapshot) {
				t.Fatalf("the clone snapshot differs at tick %d", want[second].snapshot.Tick)
			}
			if !reflect.DeepEqual(got.safety, want[second].safety) {
				t.Fatalf("the clone safety observation differs at tick %d", want[second].snapshot.Tick)
			}
			second++
		},
	})
	if second != len(want) {
		t.Fatalf("the clone ran %d seconds, want %d", second, len(want))
	}
	last := want[len(want)-1].snapshot
	if last.Submitted <= start.Submitted || last.Completed <= start.Completed {
		t.Fatalf("the replay window has no new requests or completions: submitted %d to %d, completed %d to %d",
			start.Submitted, last.Submitted, start.Completed, last.Completed)
	}
	t.Logf("preset=london band=am_peak seed=%d warmup_ticks=%d replay_ticks=%d submitted=%d..%d completed=%d..%d",
		londonCloneSeed, londonCloneWarmupTicks, londonCloneReplayTicks, start.Submitted, last.Submitted,
		start.Completed, last.Completed)
}

func BenchmarkCloneLondonActive(b *testing.B) {
	simulation := warmLondon(b, londonWarmup{
		schedule: londonCloneSchedule(londonBenchmarkWarmupTicks), ticks: londonBenchmarkWarmupTicks,
	})
	b.ReportAllocs()
	for b.Loop() {
		benchmarkClone = simulation.Clone()
	}
}
