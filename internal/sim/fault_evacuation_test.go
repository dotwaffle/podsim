package sim

import (
	"math"
	"testing"
)

// TestFaultEvacuationTiming faults a cruising pod with riders for each
// evacuation delay. The fault stage evacuates the pod in the first tick
// with tick >= evacuateTick in which the pod was at rest before the tick:
// with a delay of 0 or 1 s the pod still brakes at its evacuation tick,
// and with 300 s it rests long before. Every active rider is interrupted,
// and the pod stays faulted and withdrawn.
func TestFaultEvacuationTiming(t *testing.T) {
	t.Parallel()
	for _, delay := range []int{0, 1, 300} {
		s := faultLegFleet(t)
		if err := s.SetFaults(true, FaultSettings{EvacuationSeconds: delay}); err != nil {
			t.Fatal(err)
		}
		v := boardParties(t, s, "s2", "s2")
		cruiseOn(t, s, v, "s0-link")
		checkFaultsEachTick(t, s)
		startFault(t, s, v, 0)
		evacuateTick := s.faults[0].start + int64(delay)*TicksPerSecond
		aboard, interrupted := v.RidersAboard(), s.interrupted
		rest := int64(-1)
		for v.RidersAboard() > 0 {
			if s.tick > evacuateTick+60*TicksPerSecond {
				t.Fatalf("delay %d: no evacuation", delay)
			}
			s.Step()
			if rest < 0 && v.Pod.Speed == 0 {
				rest = s.tick
			}
		}
		want := max(evacuateTick, rest+1)
		if s.tick != want || s.interrupted != interrupted+aboard || s.faultCounters.evacuations != 1 {
			t.Fatalf("delay %d: evacuation at tick %d, want %d; interrupted %d", delay, s.tick, want, s.interrupted-interrupted)
		}
		if delay < 300 && rest < evacuateTick {
			t.Fatalf("delay %d: the pod was at rest at tick %d, before its evacuation tick %d", delay, rest, evacuateTick)
		}
		if !v.faulted || v.withdrawn != faultHold || v.op != (operationalDestination{purpose: opEmptyRecovery, owner: faultHold}) {
			t.Fatalf("delay %d: faulted %t, withdrawn %d, purpose %+v", delay, v.faulted, v.withdrawn, v.op)
		}
		for range 10 * TicksPerSecond {
			s.Step()
		}
		if s.faultCounters.evacuations != 1 {
			t.Fatalf("delay %d: %d evacuations", delay, s.faultCounters.evacuations)
		}
	}
}

// TestFaultClearBeforeEvacuation gives a fault the same end as its
// evacuation tick. The clear runs first in the fault stage, so the pod is
// never evacuated, and it delivers its riders.
func TestFaultClearBeforeEvacuation(t *testing.T) {
	t.Parallel()
	s := faultLegFleet(t)
	if err := s.SetFaults(true, FaultSettings{EvacuationSeconds: 20}); err != nil {
		t.Fatal(err)
	}
	v := boardParties(t, s, "s2", "s2")
	cruiseOn(t, s, v, "s0-link")
	checkFaultsEachTick(t, s)
	startFault(t, s, v, 20)
	if record := s.faults[0]; record.end != s.evacuateTick(record) {
		t.Fatalf("end %d, evacuation tick %d", record.end, s.evacuateTick(record))
	}
	completed := s.completed
	stepUntil(t, s, "riders complete", func() bool {
		if s.interrupted != 0 || s.faultCounters.evacuations != 0 {
			t.Fatalf("tick %d: the pod was evacuated", s.tick)
		}
		return s.completed == completed+2
	})
}

// TestFaultEvacuationAtBerth faults a pod that brakes into its destination
// berth with riders for that station. The pod arrives and stays faulted,
// and the fault stage evacuates it at the berth: each rider is
// interrupted, also at its destination, and settleIdleAtBerth sets its
// distance to 0 while the transition rules hold. After the clear, the pod
// is idle in service.
func TestFaultEvacuationAtBerth(t *testing.T) {
	t.Parallel()
	s := faultLegFleet(t)
	if err := s.SetFaults(true, FaultSettings{EvacuationSeconds: 1}); err != nil {
		t.Fatal(err)
	}
	v := boardParties(t, s, "s1", "s1")
	stepUntil(t, s, "pod 01 near its berth", func() bool {
		return v.Pod.Activity == Traveling && v.Pod.Speed > 0 && v.reservedThrough == v.blocks.len()-1 &&
			v.distance+stoppingDistance(v.Pod.Speed) >= v.blocks.end(v.reservedThrough)
	})
	checkFaultsEachTick(t, s)
	id := startFault(t, s, v, 0)
	stepUntil(t, s, "arrival", func() bool { return v.Pod.Activity != Traveling })
	if v.distance == 0 || v.RidersAboard() != 2 {
		t.Fatalf("arrival at %g m with %d riders", v.distance, v.RidersAboard())
	}
	completed, interrupted := s.completed, s.interrupted
	stepUntil(t, s, "evacuation", func() bool { return v.RidersAboard() == 0 })
	if s.completed != completed || s.interrupted != interrupted+2 || v.Pod.Activity != Idle || v.distance != 0 || !v.faulted {
		t.Fatalf("completed %d, interrupted %d, pod %+v at %g m", s.completed-completed, s.interrupted-interrupted, v.Pod, v.distance)
	}
	for range 10 * TicksPerSecond {
		s.Step()
	}
	if s.faultCounters.evacuations != 1 {
		t.Fatalf("%d evacuations, want 1", s.faultCounters.evacuations)
	}
	if err := s.clearFault(id); err != nil {
		t.Fatal(err)
	}
	if v.withdrawn != 0 || v.Pod.Activity != Idle {
		t.Fatalf("withdrawn %d, activity %s after the clear", v.withdrawn, v.Pod.Activity)
	}
}

// laneRecoveryShapes returns the route shapes of a lane evacuation: a
// route that ends at the destination berth, and a route that ends at a
// station entry. Each returns a simulation with faults on and no
// evacuation delay, pod 01 with riders, and a function that frees its
// berth.
func laneRecoveryShapes() []struct {
	name    string
	prepare func(t *testing.T) (*Simulation, *vehicle, func())
} {
	cruise := func(lane string) func(t *testing.T) (*Simulation, *vehicle, func()) {
		return func(t *testing.T) (*Simulation, *vehicle, func()) {
			t.Helper()
			s := faultLegFleet(t)
			if err := s.SetFaults(true, FaultSettings{}); err != nil {
				t.Fatal(err)
			}
			v := boardParties(t, s, "s2", "s2")
			cruiseOn(t, s, v, lane)
			return s, v, func() {}
		}
	}
	return []struct {
		name    string
		prepare func(t *testing.T) (*Simulation, *vehicle, func())
	}{
		{"berth end", cruise("s1-link")},
		{"entry end", cruise("s0-link")},
	}
}

// TestFaultLaneRecovery evacuates a faulted pod on a lane on each route
// shape. The pod becomes an empty recovery that the fault hold owns, on its
// original route and with its destination: it searches no free berth. The
// clear removes the record at once, and the hold stays while the recovery
// travels (F12). The arrival clears the purpose, and the next fault stage
// releases the hold.
func TestFaultLaneRecovery(t *testing.T) {
	t.Parallel()
	for _, shape := range laneRecoveryShapes() {
		t.Run(shape.name, func(t *testing.T) {
			t.Parallel()
			s, v, release := shape.prepare(t)
			checkFaultsEachTick(t, s)
			id := startFault(t, s, v, 0)
			route, destination, station := v.Route, v.destination, v.destinationStation
			if (destination.ID != "") != (shape.name == "berth end") {
				t.Fatalf("destination %q", destination.ID)
			}
			stepUntil(t, s, "evacuation", func() bool { return v.RidersAboard() == 0 })
			if v.op != (operationalDestination{purpose: opEmptyRecovery, owner: faultHold}) || !sameRouteSlice(v.Route, route) ||
				v.destination != destination || v.RelocatingTo != station || v.Pod.Speed != 0 {
				t.Fatalf("pod after the evacuation: %+v, purpose %+v", v.Pod, v.op)
			}
			release()
			if err := s.clearFault(id); err != nil {
				t.Fatal(err)
			}
			if len(s.faults) != 0 || v.faulted || v.withdrawn != faultHold || v.op.owner != faultHold {
				t.Fatalf("after the clear: records %v, faulted %t, withdrawn %d, purpose %+v", faultIDs(s), v.faulted, v.withdrawn, v.op)
			}
			stepUntil(t, s, "recovery arrival", func() bool {
				if v.withdrawn != faultHold {
					t.Fatalf("tick %d: the hold ended before the arrival", s.tick)
				}
				// A route to a station entry gets its berth as a passenger
				// route does. Otherwise the route and the berth stay.
				if v.op.purpose == opEmptyRecovery && v.Pod.Activity == Traveling &&
					(v.destinationStation != station || destination.ID != "" && (v.destination != destination || !sameRouteSlice(v.Route, route))) {
					t.Fatalf("tick %d: the recovery changed its route or destination", s.tick)
				}
				return v.Pod.Activity == Idle
			})
			if v.op != (operationalDestination{}) || v.Pod.StationID != station || v.withdrawn != faultHold {
				t.Fatalf("pod after the arrival: %+v, purpose %+v, withdrawn %d", v.Pod, v.op, v.withdrawn)
			}
			s.Step()
			if v.withdrawn != 0 {
				t.Fatalf("the next fault stage kept the hold %d", v.withdrawn)
			}
		})
	}
}

// TestFaultDuringRecovery faults a pod while its fault recovery travels.
// The fault reuses the hold and keeps the purpose. Its clear keeps the
// hold for the recovery, and the recovery ends as usual.
func TestFaultDuringRecovery(t *testing.T) {
	t.Parallel()
	s, v, _ := laneRecoveryShapes()[0].prepare(t)
	checkFaultsEachTick(t, s)
	first := startFault(t, s, v, 0)
	stepUntil(t, s, "evacuation", func() bool { return v.RidersAboard() == 0 })
	if err := s.clearFault(first); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, s, "recovery moves", func() bool { return v.Pod.Speed > 0 })
	recovery := v.op
	second := startFault(t, s, v, 0)
	if v.withdrawn != faultHold || v.op != recovery || s.faultCounters.evacuations != 1 {
		t.Fatalf("withdrawn %d, purpose %+v, evacuations %d", v.withdrawn, v.op, s.faultCounters.evacuations)
	}
	for range 30 * TicksPerSecond {
		s.Step()
	}
	if s.faultCounters.evacuations != 1 || v.Pod.Speed != 0 {
		t.Fatalf("evacuations %d, speed %g", s.faultCounters.evacuations, v.Pod.Speed)
	}
	if err := s.clearFault(second); err != nil {
		t.Fatal(err)
	}
	if v.withdrawn != faultHold || v.op != recovery {
		t.Fatalf("after the second clear: withdrawn %d, purpose %+v", v.withdrawn, v.op)
	}
	stepUntil(t, s, "recovery arrival", func() bool { return v.Pod.Activity == Idle })
	s.Step()
	if v.withdrawn != 0 {
		t.Fatalf("the hold %d stays after the arrival", v.withdrawn)
	}
}

// TestFaultHoldRelease checks the hold release rule in the fault stage. A
// pod with the fault hold and no record returns to service. The hold stays
// while the pod is faulted and while the hold owns a purpose.
func TestFaultHoldRelease(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		cause func(s *Simulation, v *vehicle) (undo func())
	}{
		{"no cause", func(*Simulation, *vehicle) func() { return nil }},
		{"faulted", func(s *Simulation, v *vehicle) func() {
			id := startFault(t, s, v, 0)
			return func() {
				if err := s.clearFault(id); err != nil {
					t.Fatal(err)
				}
			}
		}},
		{"purpose", func(_ *Simulation, v *vehicle) func() {
			v.op = operationalDestination{purpose: opEmptyRecovery, owner: faultHold}
			return func() { v.op = operationalDestination{} }
		}},
	} {
		s := faultLegFleet(t)
		v := s.findVehicle("02")
		if err := s.withdrawService(v, faultHold); err != nil {
			t.Fatal(err)
		}
		undo := test.cause(s, v)
		if undo == nil {
			s.faultStage()
			if v.withdrawn != 0 {
				t.Fatalf("%s: the hold stays", test.name)
			}
			continue
		}
		s.faultStage()
		if v.withdrawn != faultHold {
			t.Fatalf("%s: the hold ended", test.name)
		}
		undo()
		s.faultStage()
		if v.withdrawn != 0 {
			t.Fatalf("%s: the hold stays after the cause ended", test.name)
		}
	}
}

// TestFaultEvacuationCounterSaturates checks that an evacuation completes
// with the evacuation counter at math.MaxInt64, and that the counter stays
// there.
func TestFaultEvacuationCounterSaturates(t *testing.T) {
	t.Parallel()
	s, v, _ := laneRecoveryShapes()[1].prepare(t)
	s.faultCounters.evacuations = math.MaxInt64
	startFault(t, s, v, 0)
	stepUntil(t, s, "evacuation", func() bool { return v.RidersAboard() == 0 })
	if s.faultCounters.evacuations != math.MaxInt64 || v.op.purpose != opEmptyRecovery {
		t.Fatalf("evacuations %d, purpose %+v", s.faultCounters.evacuations, v.op)
	}
}

// checkRecoverySave checks a restored pod 01 in its fault recovery: it has
// the fault hold and the recovery purpose, and no record names it.
func checkRecoverySave(t *testing.T, restored *Simulation) {
	t.Helper()
	v := restored.findVehicle("01")
	if v.withdrawn&faultHold == 0 || v.op.purpose != opEmptyRecovery || len(restored.faults) != 0 || v.faulted {
		t.Fatalf("restored recovery: hold %d, purpose %+v, records %d", v.withdrawn, v.op, len(restored.faults))
	}
}

// TestFaultEvacuationPhysicalSave saves, in the physical format, at the
// end of the tick of an evacuation on a lane and at a berth (section 16.5
// of the incident suspension contract), and replays 600 ticks from a
// checkpoint through the evacuation, the clear and the recovery. It also
// saves the fault recovery after the clear. See physicalSave.
func TestFaultEvacuationPhysicalSave(t *testing.T) {
	t.Parallel()
	s, v, _ := laneRecoveryShapes()[0].prepare(t)
	id := startFault(t, s, v, 0)
	checkpoint := s.Clone()
	run := func(s *Simulation, save bool) {
		v := s.findVehicle("01")
		for tick := range 600 {
			aboard := v.RidersAboard()
			s.Step()
			if save && aboard > 0 && v.RidersAboard() == 0 {
				physicalSave(t, s, "evacuation on a lane")
			}
			if tick == 540 {
				if err := s.clearFault(id); err != nil {
					t.Fatal(err)
				}
				if save {
					checkRecoverySave(t, physicalSave(t, s, "fault recovery after the clear"))
				}
			}
		}
	}
	run(s, true)
	run(checkpoint, false)
	if !sameState(checkpoint, s) || s.faultCounters.evacuations != 1 {
		t.Fatalf("the replay differs from the source, or no evacuation: %d", s.faultCounters.evacuations)
	}
	berth := faultLegFleet(t)
	if err := berth.SetFaults(true, FaultSettings{}); err != nil {
		t.Fatal(err)
	}
	boarding := boardParties(t, berth, "s1")
	startFault(t, berth, boarding, 0)
	berth.Step()
	if boarding.RidersAboard() != 0 || boarding.Pod.Activity != Idle {
		t.Fatalf("the boarding pod was not evacuated: %+v", boarding.Pod)
	}
	physicalSave(t, berth, "evacuation at a berth")
}
