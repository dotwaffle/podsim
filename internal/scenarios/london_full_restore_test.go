package scenarios

import (
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

// TestLondonFullSafetyAndPhysicalRestore covers active traffic and a physical
// restore on the full network. Longer demand trials remain separate.
func TestLondonFullSafetyAndPhysicalRestore(t *testing.T) {
	t.Parallel()
	config := LondonFull()
	schedule := londonDemandSchedule(20260929, LondonFullDemand()[1], 40)
	for index := range schedule {
		schedule[index].tick = int64((index + 1) * 3 * sim.TicksPerSecond)
	}
	live := newSimulation(t, config)
	const restoreTick = 125 * sim.TicksPerSecond
	for tick := range restoreTick {
		if err := stepScheduled(live, scheduledStep{schedule: schedule, tick: int64(tick)}); err != nil {
			t.Fatal(err)
		}
		if tick%sim.TicksPerSecond == 0 {
			checkScaleSafety(t, live.SafetyObservation())
		}
	}
	before := live.Snapshot()
	if before.Submitted != len(schedule) || before.Completed == before.Submitted {
		t.Fatalf("restore fixture lacks active demand: submitted=%d completed=%d", before.Submitted, before.Completed)
	}
	restored, _ := checkLondonRestore(t, live, config)
	for range 30 * sim.TicksPerSecond {
		restored.Step()
		checkScaleSafety(t, restored.SafetyObservation())
	}
	after := restored.Snapshot()
	if after.Submitted != before.Submitted || after.Completed < before.Completed {
		t.Fatalf("restore lost request accounting: before=%d/%d after=%d/%d", before.Completed, before.Submitted, after.Completed, after.Submitted)
	}
	moved := 0
	for index, vehicle := range after.Vehicles {
		if vehicle.Pod.Position != before.Vehicles[index].Pod.Position {
			moved++
		}
	}
	if moved == 0 {
		t.Fatal("no pod moved after the physical restore")
	}
	t.Logf("submitted=%d completed=%d restored_tick=%d final_tick=%d", after.Submitted, after.Completed, before.Tick, after.Tick)
}
