package sim

import (
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// incidentLegFleet returns a simulation on lineNetwork(lineStations(1, 1,
// 2, 1)) with the incident marker, a party limit of 4, and drop-offs. Pod 01 is idle
// at s0-1, and pod 02 is idle at the parking berth p-1.
func incidentLegFleet(t *testing.T) *Simulation {
	t.Helper()
	s := newLegFleet(t, "s0-1", "p-1")
	s.incidentContract = IncidentV1Contract
	if err := s.SetSharedRidePartyLimit(4); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSharedRideMode(SharedRideDropOffs, DefaultSharedRideMaxStops); err != nil {
		t.Fatal(err)
	}
	return s
}

// boardParties makes pod 01 board one party from s0 to each destination,
// and returns the pod. The first party boards, and the others join the
// pod with their destinations as later stops. Drop-offs cannot add s1 to
// a ride to s2, because the route to s2 stops at no berth of s1.
func boardParties(t *testing.T, s *Simulation, to ...string) *vehicle {
	t.Helper()
	v := s.findVehicle("01")
	if err := s.board(v, newTrip(s, "s0", to[0])); err != nil {
		t.Fatal(err)
	}
	for _, destination := range to[1:] {
		v.Riders = append(v.Riders, s.boardingRider(newTrip(s, "s0", destination), v, v.Riders[0].ID))
		if !slices.Contains(v.Stops, destination) {
			v.Stops = append(v.Stops, destination)
		}
	}
	return v
}

// checkEachTick makes s check the state contract and the order balance
// after each tick and each public command.
func checkEachTick(t *testing.T, s *Simulation) {
	t.Helper()
	s.monitor = func(s *Simulation) {
		if err := s.CheckContract(); err != nil {
			t.Fatalf("tick %d: %v", s.tick, err)
		}
		if err := checkOrderBalance(s); err != nil {
			t.Fatalf("tick %d: %v", s.tick, err)
		}
	}
}

// checkNow checks the state contract and the order balance of s.
func checkNow(t *testing.T, s *Simulation) {
	t.Helper()
	if err := s.CheckContract(); err != nil {
		t.Fatal(err)
	}
	if err := checkOrderBalance(s); err != nil {
		t.Fatal(err)
	}
}

// travelOn steps s until pod v travels on the lane.
func travelOn(t *testing.T, s *Simulation, v *vehicle, lane string) {
	t.Helper()
	stepUntil(t, s, "pod "+v.Pod.ID+" on "+lane, func() bool { return v.Pod.Activity == Traveling && v.Pod.LaneID == lane })
}

// emergencyAt withdraws pod v with the emergency hold and gives it an
// emergency unload at s1-2 that the hold owns.
func emergencyAt(t *testing.T, s *Simulation, v *vehicle, interrupt uint32) {
	t.Helper()
	if err := s.withdrawService(v, emergencyHold); err != nil {
		t.Fatal(err)
	}
	if err := s.setOperationalDestination(v, operationalTarget{purpose: opEmergencyUnload, owner: emergencyHold, interrupt: interrupt, station: "s1", berth: "s1-2"}); err != nil {
		t.Fatal(err)
	}
}

// riderIDs returns the order IDs of the riders, with the completed riders
// in completed.
func riderIDs(riders []Request) (active, completed []int) {
	for _, rider := range riders {
		if rider.Completed {
			completed = append(completed, rider.ID)
		} else {
			active = append(active, rider.ID)
		}
	}
	return active, completed
}

// queueIDs returns the order IDs of the queue.
func queueIDs(s *Simulation) []int {
	ids := []int{}
	for _, trip := range s.waiting {
		ids = append(ids, trip.request.ID)
	}
	return ids
}

// TestOperationalOwnerHold checks the owner rules of section 4.2 of the
// incident contract: restoreService refuses the owner hold of a purpose,
// also when the pod has another hold, and rebindOperationalOwner moves the
// purpose to another held hold. W5 holds after each call.
func TestOperationalOwnerHold(t *testing.T) {
	t.Parallel()
	s := incidentLegFleet(t)
	v := boardParties(t, s, "s2", "s2")
	travelOn(t, s, v, "s0-link")
	checkEachTick(t, s)
	emergencyAt(t, s, v, 0)
	if err := s.withdrawService(v, faultHold); err != nil {
		t.Fatal(err)
	}
	checkNow(t, s)
	before := s.ExportState()
	if err := s.restoreService(v, emergencyHold); err == nil {
		t.Fatal("restoreService removed the owner hold")
	}
	if !reflect.DeepEqual(s.ExportState(), before) {
		t.Fatal("a refused restore changed the state")
	}
	// Each refused rebind changes nothing.
	for _, to := range []serviceHold{emergencyHold, 0, faultHold | emergencyHold, 1 << 5} {
		if err := s.rebindOperationalOwner(v, to); err == nil || !reflect.DeepEqual(s.ExportState(), before) {
			t.Fatalf("rebind to %#x: %v", to, err)
		}
	}
	if err := s.rebindOperationalOwner(v, faultHold); err != nil {
		t.Fatal(err)
	}
	checkNow(t, s)
	if err := s.restoreService(v, emergencyHold); err != nil {
		t.Fatal(err)
	}
	checkNow(t, s)
	if v.withdrawn != faultHold || v.op.owner != faultHold || v.op.purpose != opEmergencyUnload {
		t.Fatalf("holds %#x, purpose %+v", v.withdrawn, v.op)
	}
	if err := s.restoreService(v, faultHold); err == nil {
		t.Fatal("restoreService removed the last owner hold")
	}
	// A pod in service has no purpose to rebind.
	other := s.findVehicle("02")
	if err := s.withdrawService(other, faultHold); err != nil {
		t.Fatal(err)
	}
	if err := s.withdrawService(other, emergencyHold); err != nil {
		t.Fatal(err)
	}
	if err := s.rebindOperationalOwner(other, emergencyHold); err == nil {
		t.Fatal("rebind of a pod without a purpose")
	}
}

// TestOperationalHoldMaskRejection checks that restoreService refuses a
// mask of two holds, zero, and an unknown bit for a pod whose purpose the
// emergency hold owns. The pod, its holds, and its purpose do not change,
// and the contract holds after each call.
func TestOperationalHoldMaskRejection(t *testing.T) {
	t.Parallel()
	s := incidentLegFleet(t)
	v := boardParties(t, s, "s2")
	travelOn(t, s, v, "s0-link")
	emergencyAt(t, s, v, 0)
	if err := s.withdrawService(v, faultHold); err != nil {
		t.Fatal(err)
	}
	before, op := s.ExportState(), v.op
	for _, hold := range []serviceHold{faultHold | emergencyHold, 0, 1 << 6} {
		if err := s.restoreService(v, hold); err == nil {
			t.Fatalf("restoreService accepted %#x", hold)
		}
		if !reflect.DeepEqual(s.ExportState(), before) || v.withdrawn != faultHold|emergencyHold || v.op != op {
			t.Fatalf("restoreService of %#x changed the pod", hold)
		}
		checkNow(t, s)
	}
}

// TestOperationalPurposes installs each purpose on a traveling pod, steps
// to the arrival, and checks the action of section 9.5. The state contract
// holds after each tick. The purpose stays set while the pod travels, and
// only the call that ends it clears it.
func TestOperationalPurposes(t *testing.T) {
	t.Parallel()
	t.Run("emergency unload", func(t *testing.T) {
		t.Parallel()
		s := incidentLegFleet(t)
		v := boardParties(t, s, "s1", "s2")
		travelOn(t, s, v, "s0-link")
		checkEachTick(t, s)
		emergencyAt(t, s, v, 0)
		if v.RelocatingTo != "" || !slices.Equal(v.Stops, []string{"s1", "s2"}) || v.destination.ID != "s1-2" {
			t.Fatalf("relocating %q, stops %v, destination %q", v.RelocatingTo, v.Stops, v.destination.ID)
		}
		stepUntil(t, s, "arrival", func() bool {
			if v.Pod.Activity == Traveling && v.op.purpose != opEmergencyUnload {
				t.Fatal("the purpose changed before the arrival")
			}
			return v.Pod.Activity == Unloading
		})
		if v.op.purpose != opEmergencyUnload || v.phaseTicks != unloadingTicks-0 || !slices.Equal(v.Stops, []string{"s2"}) || v.Pod.BerthID != "s1-2" {
			t.Fatalf("arrival: purpose %+v, phase %d, stops %v, berth %q", v.op, v.phaseTicks, v.Stops, v.Pod.BerthID)
		}
		stepUntil(t, s, "unload", func() bool { return v.Pod.Activity == Idle })
		if v.op != (operationalDestination{}) || v.withdrawn != emergencyHold || s.completed != 1 || v.RidersAboard() != 0 {
			t.Fatalf("after the unload: purpose %+v, holds %#x, completed %d", v.op, v.withdrawn, s.completed)
		}
	})
	t.Run("emergency unload at a later stop", func(t *testing.T) {
		t.Parallel()
		s := incidentLegFleet(t)
		v := boardParties(t, s, "s1", "s2")
		travelOn(t, s, v, "s0-link")
		checkEachTick(t, s)
		if err := s.withdrawService(v, emergencyHold); err != nil {
			t.Fatal(err)
		}
		if err := s.setOperationalDestination(v, operationalTarget{purpose: opEmergencyUnload, owner: emergencyHold, station: "s2", berth: "s2-1"}); err != nil {
			t.Fatal(err)
		}
		stepUntil(t, s, "arrival", func() bool { return v.Pod.Activity == Unloading })
		if v.Pod.StationID != "s2" || !slices.Equal(v.Stops, []string{"s1"}) {
			t.Fatalf("arrival at %q with stops %v", v.Pod.StationID, v.Stops)
		}
		stepUntil(t, s, "unload", func() bool { return v.Pod.Activity == Idle })
		if s.completed != 1 || s.interrupted != 0 {
			t.Fatalf("completed %d, interrupted %d", s.completed, s.interrupted)
		}
	})
	t.Run("refuge", func(t *testing.T) {
		t.Parallel()
		s := incidentLegFleet(t)
		v := boardParties(t, s, "s2")
		travelOn(t, s, v, "s0-link")
		checkEachTick(t, s)
		if err := s.withdrawService(v, faultHold); err != nil {
			t.Fatal(err)
		}
		if err := s.setOperationalDestination(v, operationalTarget{purpose: opRefuge, owner: faultHold, station: "s1", berth: "s1-1"}); err != nil {
			t.Fatal(err)
		}
		stepUntil(t, s, "arrival", func() bool { return v.Pod.Activity == Unloading })
		for range 3 * unloadingTicks {
			s.Step()
		}
		if v.Pod.Activity != Unloading || v.phaseTicks != 0 || v.Pod.WaitReason != refugeHolding || v.RidersAboard() != 1 || !slices.Equal(v.Stops, []string{"s2"}) {
			t.Fatalf("holding: %s, phase %d, wait %q, stops %v", v.Pod.Activity, v.phaseTicks, v.Pod.WaitReason, v.Stops)
		}
		if err := s.resumeFromRefuge(v); err != nil {
			t.Fatal(err)
		}
		checkNow(t, s)
		if v.Pod.Activity != Continuing || v.op != (operationalDestination{}) || v.destinationStation != "s2" {
			t.Fatalf("resume: %s, purpose %+v, destination %q", v.Pod.Activity, v.op, v.destinationStation)
		}
		stepUntil(t, s, "completion", func() bool { return s.completed == 1 })
		if v.withdrawn != faultHold {
			t.Fatalf("holds %#x", v.withdrawn)
		}
	})
	t.Run("empty recovery", func(t *testing.T) {
		t.Parallel()
		s := incidentLegFleet(t)
		v := s.findVehicle("02")
		if err := s.startEmptyMove(v, emptyDestination{station: "s2", berth: Berth{ID: "s2-1", Node: "s2-1"}, reserveBerth: true}); err != nil {
			t.Fatal(err)
		}
		travelOn(t, s, v, "p-link")
		if !s.owners[resource{kind: berthResource, id: "s2-1"}].isPod("02") {
			t.Fatal("the empty move does not claim its berth")
		}
		checkEachTick(t, s)
		if err := s.withdrawService(v, emergencyHold); err != nil {
			t.Fatal(err)
		}
		if err := s.setOperationalDestination(v, operationalTarget{purpose: opEmptyRecovery, owner: emergencyHold, station: "s1", berth: "s1-2"}); err != nil {
			t.Fatal(err)
		}
		if v.RelocatingTo != "s1" || s.owners[resource{kind: berthResource, id: "s1-2"}] != podResourceOwner("02") ||
			!s.owners[resource{kind: berthResource, id: "s2-1"}].isZero() {
			t.Fatalf("relocating %q, owners %v", v.RelocatingTo, s.owners)
		}
		stepUntil(t, s, "arrival", func() bool {
			if v.Pod.Activity == Traveling && v.op.purpose != opEmptyRecovery {
				t.Fatal("the purpose changed before the arrival")
			}
			return v.Pod.Activity == Idle
		})
		if v.op != (operationalDestination{}) || v.withdrawn != emergencyHold || v.Pod.BerthID != "s1-2" {
			t.Fatalf("arrival: purpose %+v, holds %#x, berth %q", v.op, v.withdrawn, v.Pod.BerthID)
		}
		// The idle pod stays out of service until the policy restores it.
		for range 10 * TicksPerSecond {
			s.Step()
		}
		if v.Pod.BerthID != "s1-2" || v.Pod.Activity != Idle {
			t.Fatalf("withdrawn idle pod moved: %s at %q", v.Pod.Activity, v.Pod.BerthID)
		}
		if err := s.restoreService(v, emergencyHold); err != nil {
			t.Fatal(err)
		}
	})
}

// TestFinishOperationalUnloadOutcomes checks the outcomes of an emergency
// unload: a marked rider is interrupted, also at its destination, an
// unmarked rider at its destination completes, and each other rider is
// transferred to the queue at its order ID position, boarded, with the
// station as its leg origin and no exclusion.
func TestFinishOperationalUnloadOutcomes(t *testing.T) {
	t.Parallel()
	s := incidentLegFleet(t)
	first := newTrip(s, "s2", "s0")
	v := boardParties(t, s, "s1", "s1")
	between := newTrip(s, "s2", "s0")
	for range 2 {
		v.Riders = append(v.Riders, s.boardingRider(newTrip(s, "s0", "s2"), v, v.Riders[0].ID))
	}
	v.Stops = []string{"s1", "s2"}
	last := newTrip(s, "s2", "s0")
	// The queued orders 1, 4, and 7 join the queue after the travel.
	s.unaccountedOrders = 3
	travelOn(t, s, v, "s0-link")
	// Order 2, at index 0, is marked. Order 3 goes to s1 unmarked.
	emergencyAt(t, s, v, 1)
	stepUntil(t, s, "arrival", func() bool { return v.Pod.Activity == Unloading })
	s.waiting, s.unaccountedOrders = []waitingTrip{first, between, last}, 0
	s.finishOperationalUnload(v)
	checkNow(t, s)
	if !slices.Equal(s.undelivered, []int{2}) || s.interrupted != 1 || s.completed != 1 ||
		!slices.ContainsFunc(s.stepCompletions, func(c StepCompletion) bool { return c.RequestID == 3 }) {
		t.Fatalf("undelivered %v, interrupted %d, completed %d, completions %v", s.undelivered, s.interrupted, s.completed, s.stepCompletions)
	}
	if got := queueIDs(s); !slices.Equal(got, []int{1, 4, 5, 6, 7}) {
		t.Fatalf("queue %v", got)
	}
	for _, trip := range s.waiting[2:4] {
		if !trip.boarded || trip.request.LegFrom != "s1" || trip.excludedPod != "" || trip.request.PodID != "" {
			t.Fatalf("transferred trip %+v", trip)
		}
	}
	if active, completed := riderIDs(v.Riders); len(active) != 0 || !slices.Equal(completed, []int{3}) {
		t.Fatalf("riders %v and %v", active, completed)
	}
	if v.Pod.Activity != Idle || v.op != (operationalDestination{}) || v.withdrawn != emergencyHold {
		t.Fatalf("pod %s, purpose %+v, holds %#x", v.Pod.Activity, v.op, v.withdrawn)
	}
}

// TestOperationalDistanceFreeze checks the distance freeze of section 8.2
// with recorded riders. After the last outcome the cumulative distance of
// the pod equals the value before the first outcome, whatever the last
// outcome is, and each retained boarding baseline is at or below it.
func TestOperationalDistanceFreeze(t *testing.T) {
	t.Parallel()
	// recorded returns a pod with three recorded riders with the baselines
	// 0, 50, and 70 that travels on s1-through. Order 1 goes to s1, and the
	// others go to s2.
	recorded := func(t *testing.T, hold serviceHold) (*Simulation, *vehicle) {
		t.Helper()
		s := incidentLegFleet(t)
		v := boardParties(t, s, "s1", "s2", "s2")
		travelOn(t, s, v, "s0-link")
		v.Boardings = []RiderBoarding{{BerthID: "s0-1", MetersAtBoarding: 50}, {BerthID: "s0-1"}, {BerthID: "s0-1", MetersAtBoarding: 70}}
		v.riddenBase = 100
		if err := s.withdrawService(v, hold); err != nil {
			t.Fatal(err)
		}
		checkEachTick(t, s)
		return s, v
	}
	check := func(t *testing.T, s *Simulation, v *vehicle, ridden float64) {
		t.Helper()
		checkNow(t, s)
		if got := v.riddenMeters(); got != ridden {
			t.Fatalf("ridden %v, want %v", got, ridden)
		}
		for _, record := range v.Boardings {
			if record.MetersAtBoarding > ridden {
				t.Fatalf("baseline %v above %v", record.MetersAtBoarding, ridden)
			}
		}
	}
	t.Run("mixed unload", func(t *testing.T) {
		t.Parallel()
		s, v := recorded(t, emergencyHold)
		// Order 2, at index 1, is marked. Order 3, the last rider and the
		// largest baseline, transfers last.
		if err := s.setOperationalDestination(v, operationalTarget{purpose: opEmergencyUnload, owner: emergencyHold, interrupt: 2, station: "s1", berth: "s1-2"}); err != nil {
			t.Fatal(err)
		}
		stepUntil(t, s, "arrival", func() bool { return v.Pod.Activity == Unloading })
		ridden := v.riddenMeters()
		s.finishOperationalUnload(v)
		if s.interrupted != 1 || s.completed != 1 || len(s.waiting) != 1 || s.waiting[0].request.ID != 3 {
			t.Fatalf("interrupted %d, completed %d, queue %v", s.interrupted, s.completed, queueIDs(s))
		}
		check(t, s, v, ridden)
	})
	t.Run("berth evacuation", func(t *testing.T) {
		t.Parallel()
		s, v := recorded(t, faultHold)
		stepUntil(t, s, "intermediate unload", func() bool { return v.Pod.Activity == Unloading })
		// Order 1 completes at s1, so its record stays as history.
		s.completeRider(v, 0, v.riddenMeters())
		ridden := v.riddenMeters()
		if err := s.evacuate(v); err != nil {
			t.Fatal(err)
		}
		if s.interrupted != 2 || len(v.Boardings) != 1 {
			t.Fatalf("interrupted %d, records %v", s.interrupted, v.Boardings)
		}
		check(t, s, v, ridden)
	})
	t.Run("lane evacuation", func(t *testing.T) {
		t.Parallel()
		s, v := recorded(t, faultHold)
		// Order 1 completes at s1, so its record stays as history.
		travelOn(t, s, v, "s1-link")
		v.Pod.Speed = 0
		ridden := v.riddenMeters()
		if err := s.evacuate(v); err != nil {
			t.Fatal(err)
		}
		if s.interrupted != 2 || len(v.Boardings) != 1 || v.Riders[0].ID != 1 {
			t.Fatalf("interrupted %d, riders %v", s.interrupted, v.Riders)
		}
		check(t, s, v, ridden)
		stepUntil(t, s, "recovery", func() bool { return v.Pod.Activity == Idle })
		check(t, s, v, ridden)
	})
}

// TestBerthEvacuation evacuates pods at berths in each activity that
// evacuate accepts. Every active rider is interrupted, also a rider at
// its destination, and no rider completes. The pod gets every field of
// settleIdleAtBerth, keeps its owners, and frees each other grant at the
// release boundary.
func TestBerthEvacuation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		// prepare brings pod 01 to its state at the berth, after the fault
		// hold.
		prepare func(t *testing.T, s *Simulation, v *vehicle)
	}{
		{name: "boarding with a grant", prepare: func(t *testing.T, s *Simulation, v *vehicle) {
			t.Helper()
			v.phaseTicks = 0
			s.admit()
			if v.reservedThrough < 0 {
				t.Fatal("the boarding pod has no grant")
			}
			v.phaseTicks = boardingTicks / 2
		}},
		{name: "continuing", prepare: func(t *testing.T, s *Simulation, v *vehicle) {
			t.Helper()
			// A continuing pod departs in the tick that it gets track, so
			// the pod continues at once, as in livePhaseSamples.
			stepUntil(t, s, "intermediate unload", func() bool { return v.Pod.Activity == Unloading })
			s.alight(v)
			s.continueJourney(v)
			if v.Pod.Activity != Continuing {
				t.Fatal("the pod does not continue")
			}
		}},
		{name: "unloading", prepare: func(t *testing.T, s *Simulation, v *vehicle) {
			t.Helper()
			stepUntil(t, s, "intermediate unload", func() bool { return v.Pod.Activity == Unloading && v.phaseTicks > 1 })
		}},
		{name: "emergency unload", prepare: func(t *testing.T, s *Simulation, v *vehicle) {
			t.Helper()
			travelOn(t, s, v, "s0-link")
			if err := s.setOperationalDestination(v, operationalTarget{purpose: opEmergencyUnload, owner: faultHold, interrupt: 2, station: "s1", berth: "s1-2"}); err != nil {
				t.Fatal(err)
			}
			stepUntil(t, s, "emergency unload", func() bool { return v.Pod.Activity == Unloading && v.phaseTicks > 1 })
		}},
		{name: "refuge holding", prepare: func(t *testing.T, s *Simulation, v *vehicle) {
			t.Helper()
			travelOn(t, s, v, "s0-link")
			v.Stops = []string{"s2"}
			v.Riders[0].To = "s2"
			if err := s.setOperationalDestination(v, operationalTarget{purpose: opRefuge, owner: faultHold, station: "s1", berth: "s1-2"}); err != nil {
				t.Fatal(err)
			}
			stepUntil(t, s, "refuge", func() bool { return v.Pod.Activity == Unloading })
			s.Step()
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s := incidentLegFleet(t)
			v := boardParties(t, s, "s1", "s2")
			if err := s.withdrawService(v, faultHold); err != nil {
				t.Fatal(err)
			}
			test.prepare(t, s, v)
			checkNow(t, s)
			checkEachTick(t, s)
			station, berth := v.Pod.StationID, v.Pod.BerthID
			aboard, completed := v.RidersAboard(), s.completed
			completions := len(s.stepCompletions)
			owned := maps.Clone(s.owners)
			if err := s.evacuate(v); err != nil {
				t.Fatal(err)
			}
			checkNow(t, s)
			if s.interrupted != aboard || s.completed != completed || len(s.stepCompletions) != completions || v.RidersAboard() != 0 {
				t.Fatalf("interrupted %d of %d, completed %d", s.interrupted, aboard, s.completed)
			}
			want := Pod{ID: "01", Position: v.Pod.Position, Activity: Idle, StationID: station, BerthID: berth, StationPhase: AtBerth, ManeuverStationID: station}
			if v.Pod != want || v.phaseTicks != 0 || v.blockIndex != 0 || v.distance != 0 || v.reservedThrough != -1 || v.pending != -1 ||
				v.Stops != nil || v.op != (operationalDestination{}) || v.buffered || v.bufferBerth != "" || v.RelocatingTo != "" ||
				v.Rebalancing || v.released || v.origin != (Berth{}) || v.destination.ID != berth || v.destinationStation != station ||
				v.Route != nil || v.blocks.len() != 0 || len(v.Riders) == 0 && (v.Riders != nil || v.Boardings != nil) || v.withdrawn != faultHold {
				t.Fatalf("pod after evacuation: %+v", v)
			}
			if !maps.Equal(s.owners, owned) {
				t.Fatal("the evacuation changed the owners")
			}
			s.releaseCleared()
			for r, owner := range s.owners {
				if owner.isPod("01") && !ofBerth(v.destination, r) {
					t.Fatalf("pod 01 still owns %v after the release boundary", r)
				}
			}
			if !s.owners[resource{kind: berthResource, id: berth}].isPod("01") {
				t.Fatal("pod 01 lost its berth")
			}
		})
	}
}

// TestLaneEvacuation evacuates a stopped pod on a lane on each route shape
// of section 9.5: a route that ends at the destination berth, a route
// that ends at a station entry, and a buffered route that ends at a
// station entry. Every active rider is interrupted. The pod continues as
// an empty recovery, reaches a berth, and becomes idle. The purpose clears
// at the arrival, and the fault hold stays.
func TestLaneEvacuation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		// prepare returns a simulation with pod 01 stopped on a lane with
		// riders, and a function that lets it arrive.
		prepare            func(t *testing.T) (*Simulation, func())
		berthEnd, buffered bool
	}{
		{name: "berth end", berthEnd: true, prepare: func(t *testing.T) (*Simulation, func()) {
			t.Helper()
			s := incidentLegFleet(t)
			v := boardParties(t, s, "s2", "s2")
			travelOn(t, s, v, "s1-link")
			return s, func() {}
		}},
		{name: "entry end", prepare: func(t *testing.T) (*Simulation, func()) {
			t.Helper()
			s := incidentLegFleet(t)
			v := boardParties(t, s, "s2", "s2")
			travelOn(t, s, v, "s0-link")
			return s, func() {}
		}},
		{name: "buffered entry end", buffered: true, prepare: func(t *testing.T) (*Simulation, func()) {
			t.Helper()
			s, err := NewFleet(stationBufferNetwork(Example(), 4), []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"}})
			if err != nil {
				t.Fatal(err)
			}
			s.incidentContract = IncidentV1Contract
			s.SetStationBuffers(true)
			if err := s.RequestJourney("01", "market"); err != nil {
				t.Fatal(err)
			}
			barrier := resource{kind: berthResource, id: "market-1"}
			s.owners[barrier] = podResourceOwner("external")
			stepUntil(t, s, "passenger head at frontier", func() bool {
				v := s.findVehicle("01")
				plan, ok := s.bufferPlan(v)
				return ok && v.Pod.Speed == 0 && v.distance == v.blocks.end(plan.frontier)
			})
			return s, func() { delete(s.owners, barrier) }
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, release := test.prepare(t)
			v := s.findVehicle("01")
			if (v.destination.ID != "") != test.berthEnd || v.buffered != test.buffered {
				t.Fatalf("destination %q, buffered %t", v.destination.ID, v.buffered)
			}
			if err := s.withdrawService(v, faultHold); err != nil {
				t.Fatal(err)
			}
			v.Pod.Speed = 0
			checkEachTick(t, s)
			aboard, station := v.RidersAboard(), v.destinationStation
			distance, route := v.distance, slices.Clone(v.Route)
			if err := s.evacuate(v); err != nil {
				t.Fatal(err)
			}
			checkNow(t, s)
			if s.interrupted != aboard || v.RidersAboard() != 0 || v.Pod.Occupied || v.Stops != nil || v.RelocatingTo != station ||
				v.op != (operationalDestination{purpose: opEmptyRecovery, owner: faultHold}) ||
				v.distance != distance || !slices.Equal(v.Route, route) || v.buffered != test.buffered {
				t.Fatalf("pod after evacuation: %+v", v)
			}
			release()
			stepUntil(t, s, "recovery arrival", func() bool {
				if v.Pod.Activity == Traveling && v.op.purpose != opEmptyRecovery {
					t.Fatal("the purpose changed before the arrival")
				}
				return v.Pod.Activity == Idle
			})
			if v.op != (operationalDestination{}) || v.withdrawn != faultHold || v.Pod.StationID != station || v.Pod.Occupied {
				t.Fatalf("pod after the arrival: %+v", v)
			}
		})
	}
}

// TestLaneEvacuationRestore checks that the physical tier keeps an empty
// recovery on each route shape of section 9.5, with its buffer membership,
// and that the logical tier clears the purpose and keeps the hold.
func TestLaneEvacuationRestore(t *testing.T) {
	t.Parallel()
	for _, lane := range []string{"s1-link", "s0-link", ""} {
		s := incidentLegFleet(t)
		var v *vehicle
		if lane != "" {
			v = boardParties(t, s, "s2", "s2")
			travelOn(t, s, v, lane)
		} else {
			var err error
			s, err = NewFleet(stationBufferNetwork(Example(), 4), []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"}})
			if err != nil {
				t.Fatal(err)
			}
			s.incidentContract = IncidentV1Contract
			s.SetStationBuffers(true)
			if err := s.RequestJourney("01", "market"); err != nil {
				t.Fatal(err)
			}
			s.owners[resource{kind: berthResource, id: "market-1"}] = podResourceOwner("external")
			v = s.findVehicle("01")
			stepUntil(t, s, "buffered head", func() bool { return v.buffered && v.Pod.Speed == 0 })
			delete(s.owners, resource{kind: berthResource, id: "market-1"})
		}
		if err := s.withdrawService(v, faultHold); err != nil {
			t.Fatal(err)
		}
		v.Pod.Speed = 0
		if err := s.evacuate(v); err != nil {
			t.Fatal(err)
		}
		state := s.ExportState()
		input := RestoreStateInput{Network: s.network, Fleet: s.initial, State: state, IncidentContract: IncidentV1Contract}
		if lane == "" {
			input.StationQueueSpacing = s.StationQueueSpacing()
		}
		restored, result, err := RestoreState(input)
		if err != nil || !cleanRestore(result) || !reflect.DeepEqual(restored.ExportState(), state) {
			t.Fatalf("%q: physical restore %v, %+v", lane, err, result)
		}
		if got := restored.findVehicle("01"); got.op != v.op || got.buffered != v.buffered {
			t.Fatalf("%q: restored purpose %+v, buffered %t", lane, got.op, got.buffered)
		}
		input.LogicalOnly = true
		restored, _, err = RestoreState(input)
		if got := restored.findVehicle("01"); err != nil || got.op != (operationalDestination{}) || got.withdrawn != faultHold {
			t.Fatalf("%q: logical restore %v", lane, err)
		}
	}
}

// refugeNetwork returns lineNetwork(lineStations(1, 1, 2, 1)). When
// blocked is true, only compact pods can use the lane out of the parking
// berth p-1, so a legacy pod at p-1 has no route to a stop.
func refugeNetwork(t *testing.T, blocked bool) Network {
	t.Helper()
	network := lineNetwork(lineStations(1, 1, 2, 1))
	if blocked {
		compact, err := NewClassSet(string(CompactClass))
		if err != nil {
			t.Fatal(err)
		}
		network.Lanes[slices.IndexFunc(network.Lanes, func(lane Lane) bool { return lane.ID == "p-1-out" })].VehicleClasses = compact
	}
	return network
}

// parkingRefuge returns a simulation where pod 01 holds at the parking
// refuge p-1 with two recorded riders to s2.
func parkingRefuge(t *testing.T, blocked bool) (*Simulation, *vehicle) {
	t.Helper()
	s, err := NewFleet(refugeNetwork(t, blocked), place("s0-1", "s1-1"))
	if err != nil {
		t.Fatal(err)
	}
	s.incidentContract = IncidentV1Contract
	if err := s.SetSharedRidePartyLimit(4); err != nil {
		t.Fatal(err)
	}
	v := boardParties(t, s, "s2", "s2")
	travelOn(t, s, v, "s0-link")
	v.Boardings = []RiderBoarding{{BerthID: "s0-1", MetersAtBoarding: 5}, {BerthID: "s0-1", MetersAtBoarding: 10}}
	v.riddenBase = 20
	if err := s.withdrawService(v, faultHold); err != nil {
		t.Fatal(err)
	}
	checkEachTick(t, s)
	if err := s.setOperationalDestination(v, operationalTarget{purpose: opRefuge, owner: faultHold, station: "p", berth: "p-1"}); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, s, "refuge", func() bool { return v.Pod.Activity == Unloading })
	s.Step()
	if v.Pod.BerthID != "p-1" || v.op.purpose != opRefuge || v.RidersAboard() != 2 || v.Pod.WaitReason != refugeHolding {
		t.Fatalf("pod at the refuge: %+v", v)
	}
	return s, v
}

// TestParkingRefuge checks a refuge at a parking berth with recorded
// riders. The live check passes, the physical tier keeps the pod, and the
// logical tier requeues every active rider and keeps the hold.
func TestParkingRefuge(t *testing.T) {
	t.Parallel()
	s, _ := parkingRefuge(t, false)
	state := s.ExportState()
	input := RestoreStateInput{Network: s.network, Fleet: s.initial, State: state, IncidentContract: IncidentV1Contract}
	restored, result, err := RestoreState(input)
	if err != nil || !cleanRestore(result) {
		t.Fatalf("physical restore: %v, %+v", err, result)
	}
	if !reflect.DeepEqual(restored.ExportState(), state) || restored.findVehicle("01").Pod.WaitReason != refugeHolding {
		t.Fatal("the physical restore changed the refuge")
	}
	input.LogicalOnly = true
	restored, result, err = RestoreState(input)
	if err != nil || !slices.Equal(result.Requeued, []int{1, 2}) || len(result.LogicalCompleted)+len(result.Interrupted) != 0 {
		t.Fatalf("logical restore: %v, %+v", err, result)
	}
	if v := restored.findVehicle("01"); v.withdrawn != faultHold || v.op != (operationalDestination{}) {
		t.Fatalf("logical pod: holds %#x, purpose %+v", v.withdrawn, v.op)
	}
}

// TestResumeFromRefuge checks both outcomes of resumeFromRefuge. With a
// route the pod continues in service. Without a route it keeps its purpose
// and holds, and the call returns an error.
func TestResumeFromRefuge(t *testing.T) {
	t.Parallel()
	s, v := parkingRefuge(t, false)
	if err := s.resumeFromRefuge(v); err != nil {
		t.Fatal(err)
	}
	checkNow(t, s)
	if v.Pod.Activity != Continuing || v.op != (operationalDestination{}) || v.Pod.WaitReason != NoWait {
		t.Fatalf("resumed pod: %+v", v)
	}
	s, v = parkingRefuge(t, true)
	before := s.ExportState()
	if err := s.resumeFromRefuge(v); err == nil {
		t.Fatal("the resume without a route succeeded")
	}
	if !reflect.DeepEqual(s.ExportState(), before) || v.Pod.WaitReason != refugeHolding {
		t.Fatal("a failed resume changed the pod")
	}
	if err := s.resumeFromRefuge(s.findVehicle("02")); err == nil {
		t.Fatal("resume of a pod without a refuge")
	}
}

// TestStartOperationalUnload starts an emergency unload at the berth of a
// boarding pod with a grant, a continuing pod, and an unloading pod, each
// with buffer flags where the phase allows them. The pod gets the arrival
// fields of section 9.3, keeps only its berth after the release boundary,
// and the unload ends with the outcomes of section 9.5.
func TestStartOperationalUnload(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		prepare func(t *testing.T, s *Simulation, v *vehicle)
		phase   int
	}{
		{name: "boarding", phase: unloadingTicks, prepare: func(t *testing.T, s *Simulation, v *vehicle) {
			t.Helper()
			v.phaseTicks = 0
			s.admit()
			if v.reservedThrough < 0 {
				t.Fatal("the boarding pod has no grant")
			}
			v.phaseTicks = boardingTicks / 2
			v.buffered, v.bufferBerth = true, "s2-1"
		}},
		{name: "continuing", phase: unloadingTicks, prepare: func(t *testing.T, s *Simulation, v *vehicle) {
			t.Helper()
			stepUntil(t, s, "intermediate unload", func() bool { return v.Pod.Activity == Unloading })
			s.alight(v)
			s.continueJourney(v)
			v.phaseTicks = 0
			s.admit()
			if v.Pod.Activity != Continuing || v.reservedThrough < 0 {
				t.Fatal("the pod does not continue with a grant")
			}
			v.buffered, v.bufferBerth = true, "s2-1"
		}},
		{name: "unloading", phase: 7, prepare: func(t *testing.T, s *Simulation, v *vehicle) {
			t.Helper()
			stepUntil(t, s, "intermediate unload", func() bool { return v.Pod.Activity == Unloading && v.phaseTicks == 7 })
		}},
		{name: "unloading at phase 0", phase: unloadingTicks, prepare: func(t *testing.T, s *Simulation, v *vehicle) {
			t.Helper()
			stepUntil(t, s, "intermediate unload", func() bool { return v.Pod.Activity == Unloading })
			v.phaseTicks = 0
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s := incidentLegFleet(t)
			v := boardParties(t, s, "s1", "s2", "s2")
			if err := s.withdrawService(v, emergencyHold); err != nil {
				t.Fatal(err)
			}
			test.prepare(t, s, v)
			station, berth := v.Pod.StationID, v.Pod.BerthID
			if err := s.startOperationalUnload(v, emergencyHold, 0); err != nil {
				t.Fatal(err)
			}
			checkNow(t, s)
			if v.Pod.Activity != Unloading || !v.Pod.Occupied || v.Pod.StationPhase != AtBerth || v.destination.ID != berth ||
				v.destinationStation != station || v.buffered || v.bufferBerth != "" || v.reservedThrough != -1 || v.pending != -1 ||
				slices.Contains(v.Stops, station) || v.phaseTicks != test.phase || v.op.purpose != opEmergencyUnload {
				t.Fatalf("pod after the start: %+v", v)
			}
			// The save at the command boundary restores in both tiers.
			state := s.ExportState()
			input := RestoreStateInput{Network: s.network, Fleet: s.initial, State: state, IncidentContract: IncidentV1Contract}
			restored, result, err := RestoreState(input)
			if err != nil || !cleanRestore(result) || !reflect.DeepEqual(restored.ExportState(), state) {
				t.Fatalf("physical restore: %v, %+v", err, result)
			}
			input.LogicalOnly = true
			if _, result, err := RestoreState(input); err != nil || len(result.Interrupted) != 0 {
				t.Fatalf("logical restore: %v, %+v", err, result)
			}
			s.releaseCleared()
			for r, owner := range s.owners {
				if owner.isPod("01") && !ofBerth(v.destination, r) {
					t.Fatalf("pod 01 still owns %v after the release boundary", r)
				}
			}
			checkEachTick(t, s)
			stepUntil(t, s, "unload", func() bool { return v.Pod.Activity == Idle })
			if s.interrupted != 0 || v.RidersAboard() != 0 || v.op != (operationalDestination{}) || v.withdrawn != emergencyHold {
				t.Fatalf("pod after the unload: %+v", v)
			}
		})
	}
}

// TestOperationalAtomicity checks that each operation of sections 8.2 and
// 9.3 refuses each failed precondition and changes no pod, queue, owner,
// or counter.
func TestOperationalAtomicity(t *testing.T) {
	t.Parallel()
	// traveling returns pod 01 on s0-link with riders to s1 and s2 and
	// both holds.
	traveling := func(t *testing.T) (*Simulation, *vehicle) {
		t.Helper()
		s := incidentLegFleet(t)
		v := boardParties(t, s, "s1", "s2")
		travelOn(t, s, v, "s0-link")
		for _, hold := range []serviceHold{emergencyHold, faultHold} {
			if err := s.withdrawService(v, hold); err != nil {
				t.Fatal(err)
			}
		}
		return s, v
	}
	target := func(purpose opPurpose, owner serviceHold, interrupt uint32, station, berth string) operationalTarget {
		return operationalTarget{purpose: purpose, owner: owner, interrupt: interrupt, station: station, berth: berth}
	}
	tests := []struct {
		name string
		call func(t *testing.T) (*Simulation, func() error)
	}{
		{"destination of an idle pod", func(t *testing.T) (*Simulation, func() error) {
			t.Helper()
			s := incidentLegFleet(t)
			v := s.findVehicle("02")
			if err := s.withdrawService(v, faultHold); err != nil {
				t.Fatal(err)
			}
			return s, func() error {
				return s.setOperationalDestination(v, target(opEmptyRecovery, faultHold, 0, "s1", "s1-2"))
			}
		}},
	}
	destination := func(name string, change func(v *vehicle), to operationalTarget) {
		tests = append(tests, struct {
			name string
			call func(t *testing.T) (*Simulation, func() error)
		}{"destination with " + name, func(t *testing.T) (*Simulation, func() error) {
			t.Helper()
			s, v := traveling(t)
			if change != nil {
				change(v)
			}
			return s, func() error { return s.setOperationalDestination(v, to) }
		}})
	}
	destination("service purpose", nil, target(opService, faultHold, 0, "s1", "s1-2"))
	destination("unknown purpose", nil, target(opEmptyRecovery+1, faultHold, 0, "s1", "s1-2"))
	destination("an owner that is not held", func(v *vehicle) { v.withdrawn = emergencyHold }, target(opEmergencyUnload, faultHold, 0, "s1", "s1-2"))
	destination("two owner bits", nil, target(opEmergencyUnload, faultHold|emergencyHold, 0, "s1", "s1-2"))
	destination("an empty recovery of riders", nil, target(opEmptyRecovery, faultHold, 0, "s1", "s1-2"))
	destination("an emergency unload at parking", nil, target(opEmergencyUnload, faultHold, 0, "p", "p-1"))
	destination("an interrupt set of a refuge", nil, target(opRefuge, faultHold, 1, "p", "p-1"))
	destination("an interrupt bit with no rider", nil, target(opEmergencyUnload, faultHold, 4, "s1", "s1-2"))
	destination("a refuge at a stop", nil, target(opRefuge, faultHold, 0, "s2", "s2-1"))
	destination("an unknown station", nil, target(opEmergencyUnload, faultHold, 0, "s9", "s9-1"))
	destination("a berth of another station", nil, target(opEmergencyUnload, faultHold, 0, "s1", "s2-1"))
	destination("a coupling member", func(v *vehicle) { v.couplingID = "c1" }, target(opEmergencyUnload, faultHold, 0, "s1", "s1-2"))
	tests = append(tests, []struct {
		name string
		call func(t *testing.T) (*Simulation, func() error)
	}{
		{"destination inside the arrival chain", func(t *testing.T) (*Simulation, func() error) {
			t.Helper()
			s, v := traveling(t)
			travelOn(t, s, v, "s1-1-in")
			return s, func() error {
				return s.setOperationalDestination(v, target(opEmergencyUnload, faultHold, 0, "s2", "s2-1"))
			}
		}},
		{"emergency unload of an empty pod", func(t *testing.T) (*Simulation, func() error) {
			t.Helper()
			s := incidentLegFleet(t)
			v := s.findVehicle("02")
			if err := s.startEmptyMove(v, emptyDestination{station: "s2", berth: Berth{ID: "s2-1", Node: "s2-1"}}); err != nil {
				t.Fatal(err)
			}
			travelOn(t, s, v, "p-link")
			if err := s.withdrawService(v, faultHold); err != nil {
				t.Fatal(err)
			}
			return s, func() error {
				return s.setOperationalDestination(v, target(opEmergencyUnload, faultHold, 0, "s1", "s1-2"))
			}
		}},
		{"unload of a traveling pod", func(t *testing.T) (*Simulation, func() error) {
			t.Helper()
			s, v := traveling(t)
			return s, func() error { return s.startOperationalUnload(v, faultHold, 0) }
		}},
		{"unload of an empty idle pod", func(t *testing.T) (*Simulation, func() error) {
			t.Helper()
			s := incidentLegFleet(t)
			v := s.findVehicle("02")
			if err := s.withdrawService(v, faultHold); err != nil {
				t.Fatal(err)
			}
			return s, func() error { return s.startOperationalUnload(v, faultHold, 0) }
		}},
		{"unload at a parking refuge", func(t *testing.T) (*Simulation, func() error) {
			t.Helper()
			s, v := parkingRefuge(t, false)
			return s, func() error { return s.startOperationalUnload(v, faultHold, 0) }
		}},
		{"unload with an owner that is not held", func(t *testing.T) (*Simulation, func() error) {
			t.Helper()
			s := incidentLegFleet(t)
			v := boardParties(t, s, "s2")
			if err := s.withdrawService(v, faultHold); err != nil {
				t.Fatal(err)
			}
			return s, func() error { return s.startOperationalUnload(v, emergencyHold, 0) }
		}},
		{"unload with a mark of a completed rider", func(t *testing.T) (*Simulation, func() error) {
			t.Helper()
			s := incidentLegFleet(t)
			v := boardParties(t, s, "s1", "s2")
			stepUntil(t, s, "intermediate unload", func() bool { return v.Pod.Activity == Unloading })
			s.completeRider(v, 0, v.riddenMeters())
			if err := s.withdrawService(v, faultHold); err != nil {
				t.Fatal(err)
			}
			return s, func() error { return s.startOperationalUnload(v, faultHold, 1) }
		}},
		{"unload of a coupling member", func(t *testing.T) (*Simulation, func() error) {
			t.Helper()
			s := incidentLegFleet(t)
			v := boardParties(t, s, "s2")
			if err := s.withdrawService(v, faultHold); err != nil {
				t.Fatal(err)
			}
			v.couplingID = "c1"
			return s, func() error { return s.startOperationalUnload(v, faultHold, 0) }
		}},
		{"evacuation without a fault hold", func(t *testing.T) (*Simulation, func() error) {
			t.Helper()
			s, v := traveling(t)
			v.withdrawn, v.Pod.Speed = emergencyHold, 0
			return s, func() error { return s.evacuate(v) }
		}},
		{"evacuation of a moving pod", func(t *testing.T) (*Simulation, func() error) {
			t.Helper()
			s, v := traveling(t)
			return s, func() error { return s.evacuate(v) }
		}},
		{"evacuation of a coupling member", func(t *testing.T) (*Simulation, func() error) {
			t.Helper()
			s, v := traveling(t)
			v.Pod.Speed, v.couplingID = 0, "c1"
			return s, func() error { return s.evacuate(v) }
		}},
		{"evacuation of an empty traveling pod", func(t *testing.T) (*Simulation, func() error) {
			t.Helper()
			s := incidentLegFleet(t)
			v := s.findVehicle("02")
			if err := s.startEmptyMove(v, emptyDestination{station: "s2", berth: Berth{ID: "s2-1", Node: "s2-1"}}); err != nil {
				t.Fatal(err)
			}
			travelOn(t, s, v, "p-link")
			if err := s.withdrawService(v, faultHold); err != nil {
				t.Fatal(err)
			}
			v.Pod.Speed = 0
			return s, func() error { return s.evacuate(v) }
		}},
		{"evacuation of a departing pod", func(t *testing.T) (*Simulation, func() error) {
			t.Helper()
			s := incidentLegFleet(t)
			v := s.findVehicle("02")
			if err := s.startEmptyMove(v, emptyDestination{station: "s2", berth: Berth{ID: "s2-1", Node: "s2-1"}}); err != nil {
				t.Fatal(err)
			}
			if err := s.withdrawService(v, faultHold); err != nil {
				t.Fatal(err)
			}
			return s, func() error { return s.evacuate(v) }
		}},
		{"evacuation with misaligned records", func(t *testing.T) (*Simulation, func() error) {
			t.Helper()
			s, v := traveling(t)
			v.Pod.Speed, v.Boardings = 0, []RiderBoarding{{BerthID: "s0-1"}}
			return s, func() error { return s.evacuate(v) }
		}},
	}...)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, call := test.call(t)
			before, owners, waiting := s.ExportState(), maps.Clone(s.owners), slices.Clone(s.waiting)
			ops := make([]operationalDestination, len(s.vehicles))
			for index := range s.vehicles {
				ops[index] = s.vehicles[index].op
			}
			if err := call(); err == nil {
				t.Fatal("the operation is accepted")
			}
			if !reflect.DeepEqual(s.ExportState(), before) || !maps.Equal(s.owners, owners) || !reflect.DeepEqual(s.waiting, waiting) ||
				s.interrupted != 0 || s.undelivered != nil {
				t.Fatal("a refused operation changed the state")
			}
			for index := range s.vehicles {
				if s.vehicles[index].op != ops[index] {
					t.Fatal("a refused operation changed a purpose")
				}
			}
		})
	}
}

// emergencyStates returns saved states with an emergency unload of pod
// 01, which carries order 1 to s1, order 2 to s1, and order 3 to s2. The
// interrupt set marks order 1. The first state is traveling on s0-link,
// and the second is unloading at s1-2.
func emergencyStates(t *testing.T) (traveling, unloading SavedState, s *Simulation) {
	t.Helper()
	s = incidentLegFleet(t)
	v := boardParties(t, s, "s1", "s1", "s2")
	travelOn(t, s, v, "s0-link")
	emergencyAt(t, s, v, 1)
	checkEachTick(t, s)
	traveling = s.ExportState()
	stepUntil(t, s, "emergency unload", func() bool { return v.Pod.Activity == Unloading })
	unloading = s.ExportState()
	return traveling, unloading, s
}

// TestOperationalRestoreTiers checks the restore table of section 9.6 for
// emergency unloads and refuges: the order IDs and the counters of each
// tier, and that a physical restore keeps each purpose in place.
func TestOperationalRestoreTiers(t *testing.T) {
	t.Parallel()
	traveling, unloading, s := emergencyStates(t)
	restore := func(t *testing.T, state SavedState, logical bool) (*Simulation, RestoreResult) {
		t.Helper()
		restored, result, err := RestoreState(RestoreStateInput{Network: s.network, Fleet: s.initial, State: state, IncidentContract: IncidentV1Contract, LogicalOnly: logical})
		if err != nil {
			t.Fatal(err)
		}
		if err := checkOrderBalance(restored); err != nil {
			t.Fatal(err)
		}
		if restored.findVehicle("01").withdrawn != emergencyHold {
			t.Fatal("the restore lost the hold")
		}
		return restored, result
	}
	for name, state := range map[string]SavedState{"traveling": traveling, "unloading": unloading} {
		restored, result := restore(t, state, false)
		if !cleanRestore(result) || !reflect.DeepEqual(restored.ExportState(), state) {
			t.Fatalf("%s: physical restore %+v", name, result)
		}
	}
	// A demoted traveling pod interrupts the marked rider and requeues the
	// others. It never boards again.
	demoted := traveling
	demoted.Pods = slices.Clone(demoted.Pods)
	demoted.Pods[0].Route = nil
	restored, result := restore(t, demoted, false)
	v := restored.findVehicle("01")
	if result.Tier != RestorePhysical || !slices.Equal(result.Demoted, []string{"01"}) || !slices.Equal(result.Interrupted, []int{1}) ||
		!slices.Equal(result.Requeued, []int{2, 3}) || restored.interrupted != 1 || restored.interruptedPassengers != 1 ||
		restored.undelivered != nil || v.op != (operationalDestination{}) || v.Pod.Activity != Idle || len(v.Riders) != 0 {
		t.Fatalf("demoted restore: %+v, pod %+v", result, v)
	}
	restored, result = restore(t, traveling, true)
	if !slices.Equal(result.Interrupted, []int{1}) || !slices.Equal(result.Requeued, []int{2, 3}) || len(result.LogicalCompleted) != 0 ||
		restored.interrupted != 1 || restored.undelivered != nil {
		t.Fatalf("logical restore of the traveling pod: %+v", result)
	}
	// At the station, the marked rider is interrupted also at its
	// destination, and the unmarked rider at its destination completes.
	restored, result = restore(t, unloading, true)
	if !slices.Equal(result.Interrupted, []int{1}) || !slices.Equal(result.LogicalCompleted, []int{2}) || !slices.Equal(result.Requeued, []int{3}) ||
		restored.interrupted != 1 || restored.completed != 1 {
		t.Fatalf("logical restore of the unloading pod: %+v", result)
	}
	// Each state needs the incident marker.
	if _, _, err := RestoreState(RestoreStateInput{Network: s.network, Fleet: s.initial, State: traveling}); err == nil {
		t.Fatal("a restore without the incident marker accepted an operational pod")
	}
}

// TestRefugeRestoreTiers checks the refuge rows of section 9.6 for a pod
// that travels to its refuge: the physical tier keeps it, a demotion and
// the logical tier clear the purpose, and no rider is interrupted.
func TestRefugeRestoreTiers(t *testing.T) {
	t.Parallel()
	s := incidentLegFleet(t)
	v := boardParties(t, s, "s2", "s2")
	travelOn(t, s, v, "s0-link")
	if err := s.withdrawService(v, faultHold); err != nil {
		t.Fatal(err)
	}
	if err := s.setOperationalDestination(v, operationalTarget{purpose: opRefuge, owner: faultHold, station: "s1", berth: "s1-2"}); err != nil {
		t.Fatal(err)
	}
	state := s.ExportState()
	input := RestoreStateInput{Network: s.network, Fleet: s.initial, State: state, IncidentContract: IncidentV1Contract}
	restored, result, err := RestoreState(input)
	if err != nil || !cleanRestore(result) || !reflect.DeepEqual(restored.ExportState(), state) {
		t.Fatalf("physical restore: %v, %+v", err, result)
	}
	input.State.Pods = slices.Clone(state.Pods)
	input.State.Pods[0].Route = nil
	restored, result, err = RestoreState(input)
	if v := restored.findVehicle("01"); err != nil || len(result.Interrupted) != 0 || v.op != (operationalDestination{}) || v.withdrawn != faultHold {
		t.Fatalf("demoted restore: %v, %+v", err, result)
	}
	input.State, input.LogicalOnly = state, true
	restored, result, err = RestoreState(input)
	if err != nil || len(result.Interrupted) != 0 || !slices.Equal(result.Requeued, []int{1, 2}) || restored.findVehicle("01").withdrawn != faultHold {
		t.Fatalf("logical restore: %v, %+v", err, result)
	}
}

// TestOperationalPhaseMutations takes a saved state for each phase of an
// operational purpose. Each change to a field that the phase or the
// purpose needs or forbids must make the contract and both restore tiers
// reject the state.
func TestOperationalPhaseMutations(t *testing.T) {
	t.Parallel()
	traveling, unloading, s := emergencyStates(t)
	refuge, _ := parkingRefuge(t, false)
	samples := []struct {
		name    string
		state   SavedState
		network Network
		fleet   []Placement
	}{
		{"emergency traveling", traveling, s.network, s.initial},
		{"emergency unloading", unloading, s.network, s.initial},
		{"refuge holding", refuge.ExportState(), refuge.network, refuge.initial},
	}
	lane := incidentLegFleet(t)
	v := boardParties(t, lane, "s2")
	travelOn(t, lane, v, "s0-link")
	if err := lane.withdrawService(v, faultHold); err != nil {
		t.Fatal(err)
	}
	v.Pod.Speed = 0
	if err := lane.evacuate(v); err != nil {
		t.Fatal(err)
	}
	samples = append(samples, struct {
		name    string
		state   SavedState
		network Network
		fleet   []Placement
	}{"empty recovery", lane.ExportState(), lane.network, lane.initial})
	for _, sample := range samples {
		pod := sample.state.Pods[0]
		phase, err := phaseOf(pod)
		if err != nil {
			t.Fatal(err)
		}
		mutations := contractMutations(ruleForPod(pod, phase), pod)
		if len(pod.Boardings) > 0 {
			// Boarding records prove the leg origin of each rider.
			mutations = slices.DeleteFunc(mutations, func(mutation podMutation) bool {
				return mutation.name == "riders from two stations" || mutation.name == "no journey origin"
			})
		}
		add := func(name string, edit func(state *SavedState, pod *SavedPod)) {
			mutations = append(mutations, podMutation{name, edit})
		}
		add("unknown hold", func(_ *SavedState, pod *SavedPod) { pod.Withdrawn |= 1 << 4 })
		add("owner not held", func(_ *SavedState, pod *SavedPod) { pod.Withdrawn &^= pod.Owner })
		add("two owner bits", func(_ *SavedState, pod *SavedPod) { pod.Withdrawn, pod.Owner = 3, 3 })
		add("no owner", func(_ *SavedState, pod *SavedPod) { pod.Owner = 0 })
		add("unknown purpose", func(_ *SavedState, pod *SavedPod) { pod.Purpose = 4 })
		add("interrupt bit with no rider", func(_ *SavedState, pod *SavedPod) { pod.Purpose, pod.Interrupt = 1, 1<<len(pod.Riders) })
		add("purpose at rest", func(_ *SavedState, pod *SavedPod) {
			pod.Activity, pod.Occupied, pod.PhaseTicks, pod.Riders, pod.Stops = "idle", false, 0, nil, nil
			pod.StationID, pod.BerthID, pod.Route, pod.Distance, pod.RouteIndex = "s1", "s1-1", nil, 0, 0
		})
		switch opPurpose(pod.Purpose) {
		case opEmergencyUnload:
			add("interrupt of a completed rider", func(_ *SavedState, pod *SavedPod) { pod.Riders[0].Completed = true })
			add("refuge purpose with an interrupt set", func(_ *SavedState, pod *SavedPod) { pod.Purpose = uint8(opRefuge) })
			add("empty recovery of riders", func(_ *SavedState, pod *SavedPod) { pod.Purpose, pod.Interrupt = uint8(opEmptyRecovery), 0 })
			if phase == phaseTravelingOccupied {
				add("no stops", func(_ *SavedState, pod *SavedPod) { pod.Stops = nil })
				add("a stop for no rider", func(_ *SavedState, pod *SavedPod) { pod.Stops = append(pod.Stops, "p") })
				add("a station buffer", func(_ *SavedState, pod *SavedPod) { pod.StationBuffered, pod.Destination = true, "" })
			}
		case opRefuge:
			add("refuge at a stop", func(_ *SavedState, pod *SavedPod) { pod.Stops = append(pod.Stops, pod.DestinationStation) })
			add("interrupt set", func(_ *SavedState, pod *SavedPod) { pod.Interrupt = 1 })
		case opEmptyRecovery:
			add("rebalancing", func(_ *SavedState, pod *SavedPod) { pod.Rebalancing = true })
			add("released", func(_ *SavedState, pod *SavedPod) { pod.Released = true })
			add("interrupt set", func(_ *SavedState, pod *SavedPod) { pod.Interrupt = 1 })
		default:
		}
		for _, mutation := range mutations {
			state := sample.state
			state.Pods = slices.Clone(state.Pods)
			mutated := state.Pods[0]
			mutated.Riders, mutated.Stops = slices.Clone(mutated.Riders), slices.Clone(mutated.Stops)
			mutation.edit(&state, &mutated)
			state.Pods[0] = mutated
			if _, err := state.checkContract(); err == nil {
				t.Errorf("%s, %s: the contract accepts %+v", sample.name, mutation.name, mutated)
				continue
			}
			for _, logical := range []bool{false, true} {
				if _, _, err := RestoreState(RestoreStateInput{Network: sample.network, Fleet: sample.fleet, State: state, IncidentContract: IncidentV1Contract, LogicalOnly: logical}); err == nil {
					t.Errorf("%s, %s: logical only %t restores", sample.name, mutation.name, logical)
				}
			}
		}
	}
	// An emergency unload at a parking station passes the network-free
	// contract, but the restore and the live check reject it.
	state := traveling
	state.Pods = slices.Clone(state.Pods)
	state.Pods[0].DestinationStation, state.Pods[0].Destination = "p", "p-1"
	if _, _, err := RestoreState(RestoreStateInput{Network: s.network, Fleet: s.initial, State: state, IncidentContract: IncidentV1Contract}); err == nil {
		t.Fatal("an emergency unload at a parking station restores")
	}
}

// TestStrandedOperationalUnload unloads an Express pod for an emergency at
// garden, where no Express pod has a path to the destination of its
// parties. Each party is transferred and waits as a stranded order. No
// party is interrupted, because the transfer needs no feasible
// continuation (incident contract, section 9.5).
func TestStrandedOperationalUnload(t *testing.T) {
	t.Parallel()
	s, _, _ := newStrandedFleet(t)
	s.incidentContract = IncidentV1Contract
	if err := s.SetExpressServices([]ExpressService{{ID: "harbor-market", Class: ExpressClass, From: "harbor", To: "market", PartyLimit: 20}}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := s.SubmitTripOptions(TripOptions{From: "harbor", To: "market", PartySize: 1, SharingConsent: SharedConsent, Service: ExpressServiceChoice, ServiceID: "harbor-market"}); err != nil {
			t.Fatal(err)
		}
	}
	v := s.findVehicle("01")
	if err := s.withdrawService(v, emergencyHold); err != nil {
		t.Fatal(err)
	}
	checkEachTick(t, s)
	stepUntil(t, s, "divertable travel", func() bool {
		_, _, ok := s.divertStart(v)
		return v.Pod.Activity == Traveling && v.Pod.Speed > 0 && ok
	})
	station, _ := s.station("garden")
	if err := s.setOperationalDestination(v, operationalTarget{purpose: opEmergencyUnload, owner: emergencyHold, station: "garden", berth: station.Berths[0].ID}); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, s, "emergency unload", func() bool { return v.Pod.Activity == Unloading })
	if s.continuationFeasible(v.Riders[0], "garden") {
		t.Fatal("the continuation from garden is feasible")
	}
	s.finishOperationalUnload(v)
	checkNow(t, s)
	if s.interrupted != 0 || len(s.waiting) != 2 || s.waiting[0].request.LegFrom != "garden" || s.waiting[1].request.LegFrom != "garden" {
		t.Fatalf("interrupted %d, queue %+v", s.interrupted, s.waiting)
	}
}

// TestEmptyRecoveryTakenBerth checks that an empty recovery whose target
// berth is taken chooses another free berth of the station, as a passenger
// route does. Pod 03 holds s1-1 idle with a fault hold, so no path moves
// it, and s1-2 is free. The recovery must not wait for s1-1.
func TestEmptyRecoveryTakenBerth(t *testing.T) {
	t.Parallel()
	s := newLegFleet(t, "s0-1", "p-1", "s1-1")
	s.incidentContract = IncidentV1Contract
	blocker, v := s.findVehicle("03"), s.findVehicle("02")
	if err := s.withdrawService(blocker, faultHold); err != nil {
		t.Fatal(err)
	}
	if err := s.startEmptyMove(v, emptyDestination{station: "s2", berth: Berth{ID: "s2-1", Node: "s2-1"}}); err != nil {
		t.Fatal(err)
	}
	travelOn(t, s, v, "p-link")
	checkEachTick(t, s)
	if err := s.withdrawService(v, emergencyHold); err != nil {
		t.Fatal(err)
	}
	if err := s.setOperationalDestination(v, operationalTarget{purpose: opEmptyRecovery, owner: emergencyHold, station: "s1", berth: "s1-1"}); err != nil {
		t.Fatal(err)
	}
	if s.owners[resource{kind: berthResource, id: "s1-1"}].isPod("02") {
		t.Fatal("the recovery claimed the taken berth")
	}
	stepUntil(t, s, "recovery arrival", func() bool { return v.Pod.Activity == Idle })
	if v.Pod.BerthID != "s1-2" || v.op != (operationalDestination{}) || v.withdrawn != emergencyHold || blocker.Pod.BerthID != "s1-1" {
		t.Fatalf("recovery at %q with purpose %+v, blocker at %q", v.Pod.BerthID, v.op, blocker.Pod.BerthID)
	}
}

// TestWithdrawnBindingRejected checks invariant W2 through the saved form:
// a queued order that is bound to a withdrawn pod, or that holds for one,
// fails the live contract and both restore tiers.
func TestWithdrawnBindingRejected(t *testing.T) {
	t.Parallel()
	for _, binding := range []string{"pod", "hold"} {
		t.Run(binding, func(t *testing.T) {
			t.Parallel()
			s := incidentLegFleet(t)
			trip := newTrip(s, "s1", "s2")
			s.waiting = []waitingTrip{trip}
			checkNow(t, s)
			v := s.findVehicle("02")
			if err := s.withdrawService(v, faultHold); err != nil {
				t.Fatal(err)
			}
			switch binding {
			case "pod":
				s.waiting[0].request.PodID = "02"
			default:
				s.waiting[0].deferPodID, s.waiting[0].deferUntil = "02", s.tick+TicksPerSecond
			}
			if err := s.CheckContract(); err == nil || !strings.Contains(err.Error(), "W2") {
				t.Fatalf("the live contract: %v", err)
			}
			for _, logical := range []bool{false, true} {
				_, _, err := RestoreState(RestoreStateInput{Network: s.network, Fleet: s.initial, State: s.ExportState(), IncidentContract: IncidentV1Contract, LogicalOnly: logical})
				if err == nil || !strings.Contains(err.Error(), "W2") {
					t.Fatalf("logical only %t: restore %v", logical, err)
				}
			}
		})
	}
}

// TestArrivePurposes calls arrive directly for each purpose and checks the
// complete postcondition of section 9.5 before any other operation runs.
// Each transition happens inside arrive, so the state contract holds at
// its return. No later pass of Step repairs the pod.
func TestArrivePurposes(t *testing.T) {
	t.Parallel()
	// lastBlock steps s until pod v travels on the last block of its route.
	lastBlock := func(t *testing.T, s *Simulation, v *vehicle) {
		t.Helper()
		stepUntil(t, s, "last block", func() bool { return v.Pod.Activity == Traveling && v.blockIndex+1 == v.blocks.len() })
	}
	t.Run("emergency unload", func(t *testing.T) {
		t.Parallel()
		s := incidentLegFleet(t)
		v := boardParties(t, s, "s2", "s1")
		travelOn(t, s, v, "s0-link")
		v.Stops = []string{"s2", "s1"}
		if err := s.withdrawService(v, emergencyHold); err != nil {
			t.Fatal(err)
		}
		if err := s.setOperationalDestination(v, operationalTarget{purpose: opEmergencyUnload, owner: emergencyHold, interrupt: 1, station: "s1", berth: "s1-2"}); err != nil {
			t.Fatal(err)
		}
		checkNow(t, s)
		lastBlock(t, s, v)
		s.arrive(v)
		checkNow(t, s)
		if v.Pod.Activity != Unloading || !v.Pod.Occupied || v.phaseTicks != unloadingTicks || v.Pod.StationID != "s1" || v.Pod.BerthID != "s1-2" ||
			!slices.Equal(v.Stops, []string{"s2"}) || v.RelocatingTo != "" || v.buffered ||
			v.op != (operationalDestination{purpose: opEmergencyUnload, owner: emergencyHold, interrupt: 1}) || v.RidersAboard() != 2 {
			t.Fatalf("pod after arrive: %+v", v)
		}
	})
	t.Run("refuge", func(t *testing.T) {
		t.Parallel()
		s := incidentLegFleet(t)
		v := boardParties(t, s, "s2")
		travelOn(t, s, v, "s0-link")
		if err := s.withdrawService(v, faultHold); err != nil {
			t.Fatal(err)
		}
		if err := s.setOperationalDestination(v, operationalTarget{purpose: opRefuge, owner: faultHold, station: "s1", berth: "s1-2"}); err != nil {
			t.Fatal(err)
		}
		lastBlock(t, s, v)
		s.arrive(v)
		checkNow(t, s)
		if v.Pod.Activity != Unloading || !v.Pod.Occupied || v.phaseTicks != 0 || v.Pod.WaitReason != refugeHolding || v.Pod.BerthID != "s1-2" ||
			!slices.Equal(v.Stops, []string{"s2"}) || v.op != (operationalDestination{purpose: opRefuge, owner: faultHold}) || v.RidersAboard() != 1 {
			t.Fatalf("pod after arrive: %+v", v)
		}
	})
	t.Run("empty recovery", func(t *testing.T) {
		t.Parallel()
		s := incidentLegFleet(t)
		v := s.findVehicle("02")
		if err := s.startEmptyMove(v, emptyDestination{station: "s2", berth: Berth{ID: "s2-1", Node: "s2-1"}}); err != nil {
			t.Fatal(err)
		}
		travelOn(t, s, v, "p-link")
		if err := s.withdrawService(v, emergencyHold); err != nil {
			t.Fatal(err)
		}
		if err := s.setOperationalDestination(v, operationalTarget{purpose: opEmptyRecovery, owner: emergencyHold, station: "s1", berth: "s1-2"}); err != nil {
			t.Fatal(err)
		}
		lastBlock(t, s, v)
		s.arrive(v)
		checkNow(t, s)
		if v.Pod.Activity != Idle || v.Pod.Occupied || v.phaseTicks != 0 || v.Pod.BerthID != "s1-2" || v.RelocatingTo != "" || v.released ||
			v.Rebalancing || v.op != (operationalDestination{}) || v.withdrawn != emergencyHold {
			t.Fatalf("pod after arrive: %+v", v)
		}
	})
}

// TestStartOperationalUnloadLaterStop starts an emergency unload of a
// continuing pod at s1 whose stops are s2 and then s1 again. The pod has
// aligned boarding records, and one active rider goes to s1, so the state
// is valid and a physical restore keeps it. The start removes s1 from the
// stops wherever it is, so the unloading pod does not stop at its station
// again.
func TestStartOperationalUnloadLaterStop(t *testing.T) {
	t.Parallel()
	s := incidentLegFleet(t)
	v := boardParties(t, s, "s1", "s2")
	v.Boardings = []RiderBoarding{{BerthID: "s0-1"}, {BerthID: "s0-1", MetersAtBoarding: 1}}
	stepUntil(t, s, "intermediate unload", func() bool { return v.Pod.Activity == Unloading })
	v.Stops = []string{"s2", "s1"}
	s.continueJourney(v)
	if v.Pod.Activity != Continuing || v.Pod.StationID != "s1" || !slices.Equal(v.Stops, []string{"s2", "s1"}) || v.RidersAboard() != 2 {
		t.Fatalf("the pod does not continue at s1: %+v", v)
	}
	if err := s.withdrawService(v, emergencyHold); err != nil {
		t.Fatal(err)
	}
	checkNow(t, s)
	state := s.ExportState()
	restored, result, err := RestoreState(RestoreStateInput{Network: s.network, Fleet: s.initial, State: state, IncidentContract: IncidentV1Contract})
	if err != nil || !cleanRestore(result) || !reflect.DeepEqual(restored.ExportState(), state) {
		t.Fatalf("physical restore: %v, %+v", err, result)
	}
	for _, sim := range []*Simulation{s, restored} {
		pod := sim.findVehicle("01")
		if err := sim.startOperationalUnload(pod, emergencyHold, 0); err != nil {
			t.Fatal(err)
		}
		checkNow(t, sim)
		if !slices.Equal(pod.Stops, []string{"s2"}) {
			t.Fatalf("stops %v after the start", pod.Stops)
		}
	}
}
