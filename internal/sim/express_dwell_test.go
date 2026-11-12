package sim

import (
	"strings"
	"testing"
)

// expressBoarding submits one private party to market, and it fails the test
// when the pod does not board it.
func expressBoarding(t *testing.T, s *Simulation, consent SharingConsent, party int) *vehicle {
	t.Helper()
	if _, err := s.SubmitTripOptions(TripOptions{From: "harbor", To: "market", PartySize: party, SharingConsent: consent}); err != nil {
		t.Fatal(err)
	}
	v := &s.vehicles[0]
	if v.Pod.Activity != Boarding {
		t.Fatalf("the pod does not board: %+v", v.Pod)
	}
	return v
}

// TestDwellValues pins the phases in seconds for each load of each class.
func TestDwellValues(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ passengers, boarding, unloading int }{
		{1, 3, 2}, {2, 4, 3}, {8, 10, 9}, {12, 14, 13}, {13, 15, 14}, {20, 22, 21}, {21, 22, 21},
	} {
		if got, want := boardingPhase(ExpressClass, test.passengers), test.boarding*TicksPerSecond; got != want {
			t.Errorf("Express boarding of %d: %d ticks, want %d", test.passengers, got, want)
		}
		if got, want := unloadingPhase(ExpressClass, test.passengers), test.unloading*TicksPerSecond; got != want {
			t.Errorf("Express unloading of %d: %d ticks, want %d", test.passengers, got, want)
		}
		for _, class := range seatClasses {
			if boardingPhase(class, test.passengers) != 3*TicksPerSecond || unloadingPhase(class, test.passengers) != 2*TicksPerSecond {
				t.Errorf("class %q changes its phase with a load of %d", class, test.passengers)
			}
		}
	}
	if got := unloadingPhase(ExpressClass, 0); got != 2*TicksPerSecond {
		t.Errorf("Express unloading with no passenger: %d ticks", got)
	}
}

// TestExpressJourneyDwell checks the time at the two berths of a whole journey.
func TestExpressJourneyDwell(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ party, boarding, unloading int }{{1, 3, 2}, {13, 15, 14}, {20, 22, 21}} {
		s, _, _ := expressTestFleet(t)
		v := expressBoarding(t, s, PrivateConsent, test.party)
		start := s.tick
		for v.Pod.Activity == Boarding && s.tick < 60*TicksPerSecond {
			s.Step()
		}
		if got, want := s.tick-start, int64(test.boarding*TicksPerSecond); got != want {
			t.Errorf("party %d boards for %d ticks, want %d", test.party, got, want)
		}
		for v.Pod.Activity != Unloading && s.tick < 600*TicksPerSecond {
			s.Step()
		}
		if got, want := v.phaseTicks, test.unloading*TicksPerSecond; got != want {
			t.Errorf("party %d unloads for %d ticks, want %d", test.party, got, want)
		}
		if err := s.CheckContract(); err != nil {
			t.Fatal(err)
		}
	}
}

// TestExpressJoinAddsPassengers checks that a join adds to the phase, and that
// it does not restart the phase.
func TestExpressJoinAddsPassengers(t *testing.T) {
	t.Parallel()
	s, _, _ := expressTestFleet(t)
	if err := s.SetSharedRidePartyLimit(3); err != nil {
		t.Fatal(err)
	}
	v := expressBoarding(t, s, SharedConsent, 4)
	if want := 6 * TicksPerSecond; v.phaseTicks != want {
		t.Fatalf("first party: phase %d, want %d", v.phaseTicks, want)
	}
	advance(s, TicksPerSecond)
	left := v.phaseTicks
	if _, err := s.SubmitTripOptions(TripOptions{From: "harbor", To: "market", PartySize: 3, SharingConsent: SharedConsent}); err != nil {
		t.Fatal(err)
	}
	if len(v.Riders) != 2 {
		t.Fatalf("the party did not join: %d riders", len(v.Riders))
	}
	if want := left + 3*TicksPerSecond; v.phaseTicks != want {
		t.Fatalf("after the join: phase %d, want %d", v.phaseTicks, want)
	}
}

// TestExpressJoinStaysInsideBound checks the cap of the largest phase.
func TestExpressJoinStaysInsideBound(t *testing.T) {
	t.Parallel()
	s, _, _ := expressTestFleet(t)
	if err := s.SetSharedRidePartyLimit(3); err != nil {
		t.Fatal(err)
	}
	v := expressBoarding(t, s, SharedConsent, 4)
	v.phaseTicks = 21 * TicksPerSecond
	if _, err := s.SubmitTripOptions(TripOptions{From: "harbor", To: "market", PartySize: 3, SharingConsent: SharedConsent}); err != nil {
		t.Fatal(err)
	}
	if want := 22 * TicksPerSecond; v.phaseTicks != want {
		t.Fatalf("phase %d, want the bound %d", v.phaseTicks, want)
	}
}

// TestExpressJoinEndsTrackWait checks a join into a pod that finished its phase
// and waits for track. It boards again, and its wait starts again later.
func TestExpressJoinEndsTrackWait(t *testing.T) {
	t.Parallel()
	s, _, _ := expressTestFleet(t)
	if err := s.SetSharedRidePartyLimit(3); err != nil {
		t.Fatal(err)
	}
	v := expressBoarding(t, s, SharedConsent, 4)
	v.phaseTicks, v.pending, v.waitSince = 0, 0, s.tick
	v.Pod.WaitReason, v.Pod.BlockedBy = TrackOccupied, "other"
	if _, err := s.SubmitTripOptions(TripOptions{From: "harbor", To: "market", PartySize: 2, SharingConsent: SharedConsent}); err != nil {
		t.Fatal(err)
	}
	if want := 2 * TicksPerSecond; v.phaseTicks != want || v.pending != -1 || v.Pod.WaitReason != NoWait || v.Pod.BlockedBy != "" {
		t.Fatalf("phase %d, want %d, pending %d, wait %v by %q", v.phaseTicks, want, v.pending, v.Pod.WaitReason, v.Pod.BlockedBy)
	}
	if err := s.CheckContract(); err != nil {
		t.Fatal(err)
	}
}

// expressOccupiedPickup returns a pod that took a party aboard at its second
// stop, with a party of 2 aboard and a party of 3 that joined there.
func expressOccupiedPickup(t *testing.T) (*Simulation, *vehicle) {
	t.Helper()
	s, _, _ := expressTestFleet(t)
	if err := s.SetSharedRidePartyLimit(4); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSharedRideMode(SharedRideDropOffs, 3); err != nil {
		t.Fatal(err)
	}
	if err := s.SetOnboardPickups(true); err != nil {
		t.Fatal(err)
	}
	for _, trip := range []TripOptions{
		{From: "harbor", To: "market", PartySize: 2, SharingConsent: SharedConsent},
		{From: "harbor", To: "garden", PartySize: 1, SharingConsent: SharedConsent},
		{From: "garden", To: "market", PartySize: 3, SharingConsent: SharedConsent},
	} {
		if _, err := s.SubmitTripOptions(trip); err != nil {
			t.Fatal(err)
		}
	}
	v := &s.vehicles[0]
	for range 300 * TicksPerSecond {
		s.Step()
		if v.Pod.Activity == Boarding && v.Pod.Occupied {
			return s, v
		}
	}
	t.Fatalf("no occupied pickup: %+v", v.Pod)
	return nil, nil
}

// TestExpressOccupiedPickupDwell checks the phase that an occupied pickup
// starts, and a further join into that phase.
func TestExpressOccupiedPickupDwell(t *testing.T) {
	t.Parallel()
	s, v := expressOccupiedPickup(t)
	if got, want := v.phaseTicks, 5*TicksPerSecond-1; got != want && got != want+1 {
		t.Fatalf("phase %d, want about %d", got, want)
	}
	advance(s, TicksPerSecond)
	left := v.phaseTicks
	if _, err := s.SubmitTripOptions(TripOptions{From: "garden", To: "market", PartySize: 2, SharingConsent: SharedConsent}); err != nil {
		t.Fatal(err)
	}
	if want := left + 2*TicksPerSecond; v.phaseTicks != want || v.RidersAboard() != 3 {
		t.Fatalf("after the join: phase %d, want %d, %d riders", v.phaseTicks, want, v.RidersAboard())
	}
	if err := s.CheckContract(); err != nil {
		t.Fatal(err)
	}
}

// TestExpressMultiStopUnloadDwell checks that each stop counts only the parties
// that alight there.
func TestExpressMultiStopUnloadDwell(t *testing.T) {
	t.Parallel()
	s, _, _ := expressTestFleet(t)
	if err := s.SetSharedRidePartyLimit(3); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSharedRideMode(SharedRideDropOffs, 3); err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"market": 5, "garden": 3}
	for to, party := range want {
		if _, err := s.SubmitTripOptions(TripOptions{From: "harbor", To: to, PartySize: party, SharingConsent: SharedConsent}); err != nil {
			t.Fatal(err)
		}
	}
	v := &s.vehicles[0]
	seen := make(map[string]int)
	for s.completed < 2 && s.tick < 900*TicksPerSecond {
		if before := v.Pod.Activity; before == Traveling {
			s.Step()
			if v.Pod.Activity == Unloading {
				seen[v.Pod.StationID] = v.phaseTicks
			}
			continue
		}
		s.Step()
	}
	if s.completed != 2 {
		t.Fatalf("completed %d", s.completed)
	}
	for to, party := range want {
		if got, phase := seen[to], (2+party-1)*TicksPerSecond; got != phase {
			t.Errorf("stop %s: phase %d, want %d", to, got, phase)
		}
	}
}

// TestExpressEmergencyUnloadDwell checks that each active party counts in an
// emergency unload, and that a pod that already unloads keeps its phase, as the
// emergency contract asks.
func TestExpressEmergencyUnloadDwell(t *testing.T) {
	t.Parallel()
	s, _, _ := expressTestFleet(t)
	v := expressBoarding(t, s, PrivateConsent, 13)
	if err := s.withdrawService(v, emergencyHold); err != nil {
		t.Fatal(err)
	}
	if err := s.startOperationalUnload(v, emergencyHold, 0); err != nil {
		t.Fatal(err)
	}
	if want := 14 * TicksPerSecond; v.phaseTicks != want {
		t.Fatalf("phase %d, want %d", v.phaseTicks, want)
	}
	advance(s, TicksPerSecond)
	left := v.phaseTicks
	if err := s.startOperationalUnload(v, emergencyHold, 0); err != nil {
		t.Fatal(err)
	}
	if v.phaseTicks != left {
		t.Fatalf("phase %d, want the phase %d that the pod has", v.phaseTicks, left)
	}
	for v.Pod.Activity == Unloading && s.tick < 60*TicksPerSecond {
		s.Step()
	}
	if v.Pod.Activity != Idle {
		t.Fatalf("the unload did not end: %+v", v.Pod)
	}
}

// TestExpressArrivalAtEmergencyStationDwell checks an emergency unload that
// starts when a pod arrives.
func TestExpressArrivalAtEmergencyStationDwell(t *testing.T) {
	t.Parallel()
	s, _, _ := expressTestFleet(t)
	v := expressBoarding(t, s, PrivateConsent, 9)
	for v.Pod.Activity != Traveling || v.destination.Node == "" {
		if s.tick > 300*TicksPerSecond {
			t.Fatalf("the pod did not depart: %+v", v.Pod)
		}
		s.Step()
	}
	v.op.purpose = opEmergencyUnload
	// The party goes to this stop, so the other rule also counts it. A second
	// stop for the party shows that the emergency counts it all the same.
	v.Stops = append([]string{v.destinationStation}, "garden")
	v.Riders[0].To = "garden"
	s.arrive(v)
	if want := 10 * TicksPerSecond; v.phaseTicks != want {
		t.Fatalf("phase %d, want %d", v.phaseTicks, want)
	}
}

// TestExpressRefugeHoldsWithNoPhase checks that an arrival at a refuge keeps
// its zero phase with a load.
func TestExpressRefugeHoldsWithNoPhase(t *testing.T) {
	t.Parallel()
	s, _, _ := expressTestFleet(t)
	v := expressBoarding(t, s, PrivateConsent, 9)
	for v.Pod.Activity != Traveling || v.destination.Node == "" {
		if s.tick > 300*TicksPerSecond {
			t.Fatalf("the pod did not depart: %+v", v.Pod)
		}
		s.Step()
	}
	v.op.purpose = opRefuge
	s.arrive(v)
	if v.phaseTicks != 0 {
		t.Fatalf("refuge phase %d", v.phaseTicks)
	}
}

// TestExpressEstimatesUseLoad checks each estimate of a dwell with a load.
func TestExpressEstimatesUseLoad(t *testing.T) {
	t.Parallel()
	express := motionEnvelopeFor(ExpressClass)
	t.Run("destination", func(t *testing.T) {
		t.Parallel()
		s, _, _ := expressTestFleet(t)
		v := expressBoarding(t, s, PrivateConsent, 13)
		for v.Pod.Activity != Traveling || v.destination.Node == "" {
			if s.tick > 300*TicksPerSecond {
				t.Fatalf("the pod did not depart: %+v", v.Pod)
			}
			s.Step()
		}
		_, got, ok := s.finishEstimate(v)
		if !ok {
			t.Fatal("no estimate")
		}
		requireSameBits(t, "finish", got, s.routeSecondsWith(express, v.Route, v.routeLengths, motionEstimate{distance: v.distance, speed: v.Pod.Speed})+14)
	})
	t.Run("later stops", func(t *testing.T) {
		t.Parallel()
		s, _, _ := expressTestFleet(t)
		v := &s.vehicles[0]
		v.Riders = []Request{
			{ID: 1, PartySize: 5, To: "market"},
			{ID: 2, PartySize: 3, To: "market", Completed: true},
		}
		_, one, ok := s.laterStops(v, "harbor-1", 0, []string{"market"})
		if !ok {
			t.Fatal("no estimate")
		}
		v.Riders = nil
		_, none, ok := s.laterStops(v, "harbor-1", 0, []string{"market"})
		if !ok {
			t.Fatal("no estimate")
		}
		if diff := one - none; diff != 4 {
			t.Fatalf("5 passengers add %g s, want 4", diff)
		}
	})
	t.Run("assigned trip", func(t *testing.T) {
		t.Parallel()
		s, _, _ := expressTestFleet(t)
		v := &s.vehicles[0]
		v.Pod.Activity = DepartingEmpty
		v.RelocatingTo = "harbor"
		v.destination, v.destinationStation = Berth{ID: "harbor-1", Node: "harbor-1"}, "harbor"
		route, err := s.routeForClass("harbor-1", "market-1", ExpressClass)
		if err != nil {
			t.Fatal(err)
		}
		s.setVehicleRoute(v, nil)
		trip := waitingTrip{request: Request{ID: 1, PodID: v.Pod.ID, To: "market", From: "harbor", PartySize: 6}, route: route}
		s.waiting = []waitingTrip{trip}
		_, six, ok := s.finishEstimate(v)
		if !ok {
			t.Fatal("no estimate")
		}
		s.waiting[0].request.PartySize = 1
		_, one, ok := s.finishEstimate(v)
		if !ok {
			t.Fatal("no estimate")
		}
		if diff := six - one; diff != 10 {
			t.Fatalf("5 more passengers add %g s, want 10", diff)
		}
	})
}

// TestExpressRestorePhaseBound restores a pod at the largest phase and one tick
// above it. The bound of a seat-only class stays, and each refusal keeps its
// text.
func TestExpressRestorePhaseBound(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		seconds int
		prepare func(t *testing.T, s *Simulation, v *vehicle)
	}{
		{"boarding", 22, func(*testing.T, *Simulation, *vehicle) {}},
		{"unloading", 21, func(t *testing.T, s *Simulation, v *vehicle) {
			t.Helper()
			for v.Pod.Activity != Unloading {
				if s.tick > 600*TicksPerSecond {
					t.Fatalf("the pod did not arrive: %+v", v.Pod)
				}
				s.Step()
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, network, fleet := expressTestFleet(t)
			v := expressBoarding(t, s, PrivateConsent, 20)
			test.prepare(t, s, v)
			largest := test.seconds * TicksPerSecond
			restore := func(phase int) (*Simulation, RestoreResult, error) {
				state := roundTripState(t, s.ExportState())
				state.Pods[0].PhaseTicks = phase
				return RestoreState(RestoreStateInput{OrderContract: ExpressOrderContract, Network: network, Fleet: fleet, State: state})
			}
			restored, receipt, err := restore(largest)
			if err != nil || !cleanRestore(receipt) {
				t.Fatalf("the largest phase: %+v, %v", receipt, err)
			}
			if got := restored.vehicles[0].phaseTicks; got != largest {
				t.Fatalf("restored phase %d, want %d", got, largest)
			}
			_, _, err = restore(largest + 1)
			if err == nil || !strings.Contains(err.Error(), "phase") || !strings.Contains(err.Error(), "is out of range") {
				t.Fatalf("one tick above the largest phase: %v", err)
			}
		})
	}
}

// TestExpressRestoreOccupiedPhaseBound checks the bound of an occupied pickup,
// which has its own phase rule.
func TestExpressRestoreOccupiedPhaseBound(t *testing.T) {
	t.Parallel()
	s, _ := expressOccupiedPickup(t)
	restore := func(phase int) (RestoreResult, error) {
		state := roundTripState(t, s.ExportState())
		state.Pods[0].PhaseTicks = phase
		_, receipt, err := RestoreState(RestoreStateInput{OrderContract: ExpressOrderContract, Network: s.network, Fleet: []Placement{{ID: "01", Class: ExpressClass, StationID: "harbor", BerthID: "harbor-1"}}, State: state})
		return receipt, err
	}
	largest := 22 * TicksPerSecond
	if receipt, err := restore(largest); err != nil || !cleanRestore(receipt) {
		t.Fatalf("the largest phase: %+v, %v", receipt, err)
	}
	if _, err := restore(largest + 1); err == nil || !strings.Contains(err.Error(), "is out of range") {
		t.Fatalf("one tick above the largest phase: %v", err)
	}
}

// TestExpressPhaseKeepsRefusalOrder checks that the class refusal of an Express
// pod with no contract comes before the phase refusal. Phase 661 is above each
// bound of each class, so a phase check that runs first refuses it with
// another text.
func TestExpressPhaseKeepsRefusalOrder(t *testing.T) {
	t.Parallel()
	s, network, fleet := expressTestFleet(t)
	expressBoarding(t, s, PrivateConsent, 20)
	const want = "pod 01 boarding records: class express: vehicle physical profile is not supported"
	for _, phase := range []int{1, 22*TicksPerSecond + 1} {
		state := roundTripState(t, s.ExportState())
		state.OrderContract = ""
		state.Pods[0].PhaseTicks = phase
		_, _, err := RestoreState(RestoreStateInput{Network: network, Fleet: fleet, State: state})
		if err == nil || err.Error() != want {
			t.Errorf("phase %d: refusal %v, want %q", phase, err, want)
		}
	}
}

// TestPhaseBoundFollowsClass pins the bound of each class in seconds. The
// physical tier reads the same bound as the contract.
func TestPhaseBoundFollowsClass(t *testing.T) {
	t.Parallel()
	for _, class := range seatClasses {
		if maxPhaseTicks(Boarding, class) != 3*TicksPerSecond || maxPhaseTicks(Unloading, class) != 2*TicksPerSecond {
			t.Errorf("class %q: bounds %d and %d", class, maxPhaseTicks(Boarding, class), maxPhaseTicks(Unloading, class))
		}
	}
	if maxPhaseTicks(Boarding, ExpressClass) != 22*TicksPerSecond || maxPhaseTicks(Unloading, ExpressClass) != 21*TicksPerSecond {
		t.Errorf("Express: bounds %d and %d", maxPhaseTicks(Boarding, ExpressClass), maxPhaseTicks(Unloading, ExpressClass))
	}
	if maxPhaseTicks(Idle, ExpressClass) != 0 {
		t.Error("an idle Express pod has a phase bound")
	}
}
