package scenarios

import (
	"math"
	"reflect"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

const (
	// The London restore test requests AM peak journeys at 20 per minute.
	// After a 70 s warmup, it restores the simulation every 5 s for 20 s.
	// Then it runs the last restored copy for 30 s, which includes the first
	// completion at about 100 s. The cost of Step grows with the active pods,
	// and each second of London after 90 s costs more than 0.1 s with the
	// race detector. A 2-minute restore window and a 1-minute continuation
	// take about 40 s, so the test uses shorter ones.
	londonRestoreInterval          = 3 * sim.TicksPerSecond
	londonRestoreWarmupTicks       = 70 * sim.TicksPerSecond
	londonRestorePeriodTicks       = 5 * sim.TicksPerSecond
	londonRestoreCount             = 5
	londonRestoreContinuationTicks = 30 * sim.TicksPerSecond
)

// londonRestoreSchedule requests AM peak journeys at 20 per minute. The last
// request comes before the tick until.
func londonRestoreSchedule(until int) []scheduledRequest {
	schedule := londonDemandSchedule(londonCloneSeed, LondonDemand()[2], (until-1)/londonRestoreInterval)
	for index := range schedule {
		schedule[index].tick = int64((index + 1) * londonRestoreInterval)
	}
	return schedule
}

// restoreWork records the largest saved routes and the largest restore cost
// of the states that a run saves. The cost and the budget follow the block
// rule of the physical restore: a lane of length l has max(2, ceil(l/30))
// blocks.
type restoreWork struct {
	podRoute, tripRoute, cost int
}

func laneBlocks(network sim.Network, lane sim.Lane) int {
	return max(2, int(math.Ceil(network.Length(lane)/30)))
}

func (w *restoreWork) add(network sim.Network, state sim.SavedState) {
	cost := 0
	for _, pod := range state.Pods {
		w.podRoute = max(w.podRoute, len(pod.Route))
		for _, lane := range pod.Route {
			cost += laneBlocks(network, network.Lanes[lane])
		}
	}
	for _, trip := range state.Waiting {
		w.tripRoute = max(w.tripRoute, len(trip.Route))
		cost += len(trip.Route)
	}
	w.cost = max(w.cost, cost)
}

// check fails when a saved route uses a quarter of its limit or more, or when
// the restore cost is more than a quarter of the block budget.
func (w *restoreWork) check(t *testing.T, network sim.Network) {
	t.Helper()
	blocks := 0
	for _, lane := range network.Lanes {
		blocks += laneBlocks(network, lane)
	}
	budget := 32*blocks + 4*len(network.Lanes)
	podLimit, tripLimit := len(network.Lanes)+len(network.Nodes), len(network.Nodes)
	t.Logf("largest pod route %d of %d, largest trip route %d of %d, largest cost %d of %d",
		w.podRoute, podLimit, w.tripRoute, tripLimit, w.cost, budget)
	if 4*w.podRoute >= podLimit || 4*w.tripRoute >= tripLimit {
		t.Errorf("a saved route uses a quarter of its limit or more")
	}
	if 4*w.cost > budget {
		t.Errorf("the restore cost is more than a quarter of the budget")
	}
}

// checkLondonRestore restores the state of a live simulation and compares
// the result with the live simulation. A restored pod is at rest, so its
// speed and its wait reason can differ. It returns the restored simulation
// and the saved state.
func checkLondonRestore(t *testing.T, live *sim.Simulation, config project.Config) (*sim.Simulation, sim.SavedState) {
	t.Helper()
	state := live.ExportState()
	restored, result, err := sim.RestoreState(sim.RestoreStateInput{Network: config.Network, Fleet: config.Fleet, State: state})
	if err != nil {
		t.Fatalf("tick %d: %v", state.Tick, err)
	}
	if result.Tier != sim.RestorePhysical || len(result.Demoted) > 0 {
		t.Fatalf("tick %d: result %+v", state.Tick, result)
	}
	want, got := live.Snapshot(), restored.Snapshot()
	for index := range want.Vehicles {
		wantPod, gotPod := want.Vehicles[index].Pod, got.Vehicles[index].Pod
		wantPod.Speed, wantPod.WaitReason, wantPod.BlockedBy = 0, sim.NoWait, ""
		gotPod.Speed, gotPod.WaitReason, gotPod.BlockedBy = 0, sim.NoWait, ""
		if gotPod != wantPod {
			t.Fatalf("tick %d: pod\n got %+v\nwant %+v", state.Tick, gotPod, wantPod)
		}
	}
	saved := restored.ExportState()
	if !reflect.DeepEqual(saved.Waiting, state.Waiting) {
		t.Fatalf("tick %d: the restored queue differs", state.Tick)
	}
	for index, pod := range saved.Pods {
		wantPod := state.Pods[index]
		if pod.Waiting != wantPod.Waiting || pod.WaitSince != wantPod.WaitSince ||
			pod.RouteIndex != wantPod.RouteIndex || pod.LaneDistance != wantPod.LaneDistance {
			t.Fatalf("tick %d: saved pod\n got %+v\nwant %+v", state.Tick, pod, wantPod)
		}
	}
	return restored, state
}

// checkLondonLogicalRestore restores the state of a live simulation with the
// logical tier only. Each fleet pod must wait empty at its initial berth. The
// parties in an unloading pod count as completed, and the parties in each
// other pod go back to the queue.
func checkLondonLogicalRestore(t *testing.T, live *sim.Simulation, config project.Config) {
	t.Helper()
	state := live.ExportState()
	restored, result, err := sim.RestoreState(sim.RestoreStateInput{
		Network: config.Network, Fleet: config.Fleet, State: state, LogicalOnly: true,
	})
	if err != nil {
		t.Fatalf("tick %d: %v", state.Tick, err)
	}
	if _, err := restored.SafetyObservation().Check(); err != nil {
		t.Fatalf("tick %d: %v", state.Tick, err)
	}
	want, got := live.Snapshot(), restored.Snapshot()
	requeued, completed := 0, want.Completed
	for _, v := range want.Vehicles {
		if v.Request == nil || v.Request.Completed || (v.Pod.Activity != sim.Boarding && !v.Pod.Occupied) {
			continue
		}
		if v.Pod.Activity == sim.Unloading {
			completed += max(1, v.Parties)
		} else {
			requeued++
		}
	}
	if result.Tier != sim.RestoreLogical || result.PhysicalError != nil || len(result.Dropped) > 0 ||
		len(result.Requeued) != requeued || requeued == 0 {
		t.Fatalf("tick %d: result %+v, want %d requeued requests", state.Tick, result, requeued)
	}
	if got.Submitted != want.Submitted || got.Completed != completed || len(got.Pending) != len(want.Pending)+requeued {
		t.Fatalf("tick %d: submitted %d, completed %d, pending %d, want %d, %d, %d", state.Tick,
			got.Submitted, got.Completed, len(got.Pending), want.Submitted, completed, len(want.Pending)+requeued)
	}
	for index, v := range got.Vehicles {
		placement := config.Fleet[index]
		if v.Pod.ID != placement.ID || v.Pod.Activity != sim.Idle || v.Pod.BerthID != placement.BerthID || v.Request != nil {
			t.Fatalf("tick %d: pod %+v, want pod %s idle at berth %s", state.Tick, v.Pod, placement.ID, placement.BerthID)
		}
	}
	t.Logf("tick %d: logical restore requeued %d requests and completed %d parties", state.Tick, requeued, completed-want.Completed)
}

func TestRestorePhysicalLondon(t *testing.T) {
	if testing.Short() {
		t.Skip("the London restore test runs for about 15 s with the race detector")
	}
	t.Parallel()
	config := London()
	last := int64(londonRestoreWarmupTicks + (londonRestoreCount-1)*londonRestorePeriodTicks)
	end := last + londonRestoreContinuationTicks
	schedule := londonRestoreSchedule(int(end))
	live := newSimulation(t, config)
	live.SetRedistribution(true)
	var work restoreWork
	var restored *sim.Simulation
	for tick := range last {
		if err := stepScheduled(live, scheduledStep{schedule: schedule, tick: tick}); err != nil {
			t.Fatal(err)
		}
		if elapsed := tick + 1; elapsed >= londonRestoreWarmupTicks && (elapsed-londonRestoreWarmupTicks)%londonRestorePeriodTicks == 0 {
			var state sim.SavedState
			restored, state = checkLondonRestore(t, live, config)
			work.add(config.Network, state)
		}
	}
	work.check(t, config.Network)
	checkLondonLogicalRestore(t, live, config)
	restored.SetRedistribution(true)
	start := restored.Snapshot()
	runLondon(t, restored, londonRun{
		schedule: schedule, start: last, end: end,
		observe: func(observation londonObservation) {
			if _, err := observation.safety.Check(); err != nil {
				t.Fatal(err)
			}
		},
	})
	final := restored.Snapshot()
	if final.Completed <= start.Completed {
		t.Fatalf("the restored simulation completed no order in %d ticks", londonRestoreContinuationTicks)
	}
	t.Logf("preset=london band=am_peak seed=%d restores=%d submitted=%d..%d completed=%d..%d",
		londonCloneSeed, londonRestoreCount, start.Submitted, final.Submitted, start.Completed, final.Completed)
}
