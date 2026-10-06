package sim

import (
	"fmt"
	"slices"
	"testing"
)

// legTrip returns a transferred trip of one party with the order origin
// from, the leg origin leg, and the destination to (incident contract,
// section 7.3).
func legTrip(s *Simulation, from, leg, to string) waitingTrip {
	trip := newTrip(s, from, to)
	trip.request.LegFrom, trip.boarded = leg, true
	return trip
}

// TestLegOriginDispatch checks that dispatch picks up each transferred
// party at its leg origin with each pickup policy. Each trip has a leg
// origin that differs from its order origin, and some plain trips start at
// the leg origin of a transferred trip. Each party must board at its leg
// origin, and the state contract must hold at each observation.
func TestLegOriginDispatch(t *testing.T) {
	t.Parallel()
	skipLong(t)
	tests := []struct {
		name  string
		setup func(*Simulation) error
	}{
		{name: "defaults", setup: func(*Simulation) error { return nil }},
		{name: "destination mode without holds", setup: func(s *Simulation) error {
			if err := s.SetSharedRideMode(SharedRideDestination, 4); err != nil {
				return err
			}
			return s.SetFinishingPodWait(FinishingPodWaitNone)
		}},
		{name: "reassignment, onboard pickups, swaps, and the seat screen", setup: func(s *Simulation) error {
			s.SetExperimentRecords(true)
			s.SetPickupSwaps(true)
			if err := s.SetSharedRidePartyLimit(4); err != nil {
				return err
			}
			if err := s.SetSharedRideJoin(SharedRideJoinReassignExisting); err != nil {
				return err
			}
			return s.SetOnboardPickups(true)
		}},
		{name: "strict holds", setup: func(s *Simulation) error {
			return s.SetFinishingPodWait(FinishingPodWaitStrict)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, err := NewFleet(lineNetwork(lineStations(0, 2, 2, 2, 2)), place("s0-1", "s1-1", "s2-1", "s3-1"))
			if err != nil {
				t.Fatal(err)
			}
			if err := test.setup(s); err != nil {
				t.Fatal(err)
			}
			station := func(i int) string { return fmt.Sprintf("s%d", i%4) }
			legs := map[int]string{}
			for k := range 16 {
				// Each fourth trip is a plain trip from the leg origin of
				// the trip before it.
				trip := legTrip(s, station(k), station(k+1), station(k+2))
				if k%4 == 3 {
					s.requestID--
					trip = newTrip(s, station(k), station(k+2))
				}
				legs[trip.request.ID] = trip.request.legOrigin()
				s.waiting = append(s.waiting, trip)
			}
			seen := map[int]bool{}
			s.monitor = func(s *Simulation) {
				t.Helper()
				if err := s.CheckContract(); err != nil {
					t.Fatalf("tick %d: %v", s.tick, err)
				}
				for i := range s.vehicles {
					v := &s.vehicles[i]
					for _, rider := range v.Riders {
						if seen[rider.ID] {
							continue
						}
						seen[rider.ID] = true
						if v.Pod.StationID != legs[rider.ID] || rider.legOrigin() != legs[rider.ID] {
							t.Fatalf("tick %d: order %d boards pod %s at %q, want its leg origin %s", s.tick, rider.ID, v.Pod.ID, v.Pod.StationID, legs[rider.ID])
						}
					}
				}
			}
			s.dispatch()
			s.observe()
			for s.completed < 16 && s.tick < 30*60*TicksPerSecond {
				s.Step()
			}
			if s.completed != 16 || len(seen) != 16 {
				t.Fatalf("%d of 16 orders completed and %d boarded at tick %d: %+v", s.completed, len(seen), s.tick, s.waiting)
			}
		})
	}
}

// newLegFleet returns a simulation on a line of the parking station p and
// the passenger stations s0, s1, and s2, with pods 01, 02, and so on at the
// berths. Station s1 has two berths. The leg tests use orders from s0 to
// s2 with the leg origin s1.
func newLegFleet(t *testing.T, berths ...string) *Simulation {
	t.Helper()
	s, err := NewFleet(lineNetwork(lineStations(1, 1, 2, 1)), place(berths...))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// newPass returns a dispatch pass for the queue of s.
func newPass(s *Simulation) *dispatchPass {
	pass := new(dispatchPass)
	pass.begin(s.waiting)
	return pass
}

// TestLegOriginPickupSelection checks the pickup readers of section 7.4
// that select or send a pod: each uses the leg origin. Pod 01 is idle at
// the order origin s0, and pod 02 is idle at the leg origin s1.
func TestLegOriginPickupSelection(t *testing.T) {
	t.Parallel()
	setup := func(t *testing.T) (*Simulation, waitingTrip, *vehicle, *vehicle) {
		t.Helper()
		s := newLegFleet(t, "s0-1", "s1-1")
		origin, leg := s.findVehicle("01"), s.findVehicle("02")
		return s, legTrip(s, "s0", "s1", "s2"), origin, leg
	}
	t.Run("localPickupForRequest", func(t *testing.T) {
		t.Parallel()
		s, trip, _, leg := setup(t)
		if got := s.localPickupForRequest(trip.request, "", newPass(s)); got != leg {
			t.Fatalf("local pickup %v, want pod 02 at the leg origin", got)
		}
	})
	t.Run("pickupPodForRequest", func(t *testing.T) {
		t.Parallel()
		s, trip, _, leg := setup(t)
		if got := s.pickupPodForRequest(trip.request, "", newPass(s)); got != leg {
			t.Fatalf("pickup pod %v, want pod 02 at the leg origin", got)
		}
	})
	t.Run("pickupBerthFitsRequest and assignedPickupFitsRequest", func(t *testing.T) {
		t.Parallel()
		s, trip, _, leg := setup(t)
		berth, _ := s.network.Station("s1")
		if !s.pickupBerthFitsRequest(leg, trip.request, berth.Berths[0]) {
			t.Error("the berth at the leg origin does not fit")
		}
		if !s.assignedPickupFitsRequest(leg, trip.request) {
			t.Error("the idle pod at the leg origin does not fit")
		}
	})
	t.Run("candidateRouteForRequest and sendPickupForRequest", func(t *testing.T) {
		t.Parallel()
		s, trip, origin, _ := setup(t)
		if _, berth, ok := s.candidateRouteForRequest(origin, trip.request, nil); !ok || berth.ID != "s1-2" {
			t.Errorf("candidate berth %q, %t, want the free berth s1-2 at the leg origin", berth.ID, ok)
		}
		if err := s.sendPickupForRequest(origin, trip.request); err != nil || origin.RelocatingTo != "s1" || origin.destinationStation != "s1" {
			t.Errorf("pod 01 goes to %q, %q, %v, want the leg origin", origin.RelocatingTo, origin.destinationStation, err)
		}
	})
	t.Run("board", func(t *testing.T) {
		t.Parallel()
		s, trip, _, leg := setup(t)
		if err := s.board(leg, trip); err != nil {
			t.Fatal(err)
		}
		if leg.origin.ID != "s1-1" || leg.Riders[0].LegFrom != "s1" || leg.Riders[0].From != "s0" {
			t.Fatalf("pod 02 boards at %q with %+v", leg.origin.ID, leg.Riders[0])
		}
		if err := s.CheckContract(); err != nil {
			t.Fatal(err)
		}
	})
}

// TestLegOriginDispatchPass checks the dispatch readers of section 7.4. In
// each case, dispatch gives the transferred trip the pod at its leg origin
// s1 at once.
func TestLegOriginDispatchPass(t *testing.T) {
	t.Parallel()
	// boards checks that pod v boards the trip with ID id.
	boards := func(t *testing.T, v *vehicle, id int) {
		t.Helper()
		if v.Pod.Activity != Boarding || len(v.Riders) == 0 || v.Riders[0].ID != id {
			t.Fatalf("pod %s is %s with %+v, want it to board order %d", v.Pod.ID, v.Pod.Activity, v.Riders, id)
		}
	}
	t.Run("free pod at the leg origin", func(t *testing.T) {
		t.Parallel()
		s := newLegFleet(t, "s0-1", "s1-1")
		s.waiting = []waitingTrip{legTrip(s, "s0", "s1", "s2")}
		s.dispatch()
		boards(t, s.findVehicle("02"), 1)
	})
	t.Run("local pickup replaces a bound pod", func(t *testing.T) {
		t.Parallel()
		s := newLegFleet(t, "s0-1", "s1-1")
		trip := legTrip(s, "s0", "s1", "s2")
		trip.request.PodID = "01"
		s.waiting = []waitingTrip{trip}
		s.dispatch()
		boards(t, s.findVehicle("02"), 1)
	})
	t.Run("promotion of a later pickup", func(t *testing.T) {
		t.Parallel()
		// Pod 02 is idle at s1 for the plain trip from s1. No pod is idle
		// at s0. The older transferred trip takes pod 02.
		s := newLegFleet(t, "s2-1", "s1-1")
		later := newTrip(s, "s1", "s2")
		later.request.ID = 2
		later.request.PodID = "02"
		s.waiting = []waitingTrip{legTrip(s, "s0", "s1", "s2"), later}
		s.waiting[0].request.ID = 1
		s.dispatch()
		boards(t, s.findVehicle("02"), 1)
	})
	t.Run("no promotion for a pod idle at the leg origin", func(t *testing.T) {
		t.Parallel()
		// The transferred trip has pod 02 at its leg origin, so it keeps
		// it, and the later trip keeps pod 03.
		s := newLegFleet(t, "s0-1", "s1-1", "s1-2")
		trip := legTrip(s, "s0", "s1", "s2")
		trip.request.PodID = "02"
		later := newTrip(s, "s1", "s2")
		later.request.PodID = "03"
		s.waiting = []waitingTrip{trip, later}
		if s.promoteReadyPickup(0) {
			t.Fatalf("promotion swapped the pods: %+v", s.waiting)
		}
	})
	t.Run("join a boarding pod at the leg origin", func(t *testing.T) {
		t.Parallel()
		s := newLegFleet(t, "s0-1", "s1-1")
		if err := s.SetSharedRidePartyLimit(4); err != nil {
			t.Fatal(err)
		}
		host := s.findVehicle("02")
		if err := s.board(host, newTrip(s, "s1", "s2")); err != nil {
			t.Fatal(err)
		}
		trip := legTrip(s, "s0", "s1", "s2")
		if !s.joinSharedRide(&trip, newPass(s)) || len(host.Riders) != 2 || host.Riders[1].ID != trip.request.ID {
			t.Fatalf("the trip did not join pod 02: %+v", host.Riders)
		}
		if err := s.CheckContract(); err != nil {
			t.Fatal(err)
		}
	})
}

// newStrandedFleet returns an Express simulation on the line harbor,
// garden, market, where an Express pod can stop at garden but cannot leave
// its berth there. So the Express class has a passenger path from harbor
// to market and none from garden. Pod 01 is at harbor.
func newStrandedFleet(t *testing.T) (*Simulation, Network, []Placement) {
	t.Helper()
	network := expressNetwork(largeRestoreNetwork())
	for i := range network.Lanes {
		if network.Lanes[i].ID == "garden-1-out" {
			network.Lanes[i].VehicleClasses = classBit(string(GroupClass))
		}
	}
	fleet := []Placement{{ID: "01", Class: ExpressClass, StationID: "harbor", BerthID: "harbor-1"}}
	s, err := NewFleetWithOrderContract(network, fleet, ExpressOrderContract)
	if err != nil {
		t.Fatal(err)
	}
	return s, network, fleet
}

// TestLegOriginConnectivity checks the readers of section 7.4 that test a
// passenger path: each tests the path from the leg origin.
func TestLegOriginConnectivity(t *testing.T) {
	t.Parallel()
	t.Run("podFitsRequest", func(t *testing.T) {
		t.Parallel()
		s, _, _ := newStrandedFleet(t)
		v := s.findVehicle("01")
		plain, leg := newTrip(s, "harbor", "market"), legTrip(s, "harbor", "garden", "market")
		if !s.podFitsRequest(v, plain.request) || s.podFitsRequest(v, leg.request) {
			t.Fatal("the pod fits the order without the path from its leg origin")
		}
	})
	t.Run("cache key", func(t *testing.T) {
		t.Parallel()
		// The trips have equal options. No pod fits the first trip, and
		// pod 01 fits the second one.
		s, _, _ := newStrandedFleet(t)
		s.waiting = []waitingTrip{legTrip(s, "harbor", "garden", "market"), newTrip(s, "harbor", "market")}
		if s.waiting[0].request.options() != s.waiting[1].request.options() {
			t.Fatal("the options differ")
		}
		s.dispatch()
		if v := s.findVehicle("01"); len(v.Riders) != 1 || v.Riders[0].ID != 2 {
			t.Fatalf("pod 01 has %+v, want order 2", v.Riders)
		}
		if reason := s.waiting[0].request.DispatchReason; reason != "Waiting for a certified vehicle that fits this party and route" {
			t.Fatalf("the transferred trip has the reason %q", reason)
		}
	})
	t.Run("berthFilterForVehicle", func(t *testing.T) {
		t.Parallel()
		s, _, _ := newStrandedFleet(t)
		v := s.findVehicle("01")
		trip := legTrip(s, "harbor", "garden", "market")
		trip.request.PodID = "01"
		s.waiting = []waitingTrip{trip}
		v.destinationStation = "garden"
		if s.berthFilterForVehicle(v) == nil {
			t.Fatal("the pod to the leg origin has no berth filter for the destination")
		}
	})
	t.Run("Express restore of a waiting trip", func(t *testing.T) {
		t.Parallel()
		// A trip that never boarded cannot use the exception for stranded
		// trips.
		s, network, fleet := newStrandedFleet(t)
		state := s.ExportState()
		trip := legTrip(s, "harbor", "garden", "market")
		state.RequestID = s.requestID
		state.Waiting = []SavedTrip{{Request: SavedRequest(trip.request), Boarded: false}}
		err := checkContractRestoreSemantics(RestoreStateInput{OrderContract: ExpressOrderContract, Network: network, Fleet: fleet, State: state})
		if err == nil || err.Error() != "saved Express order has no compatible vehicle path" {
			t.Fatalf("restore error %v", err)
		}
	})
	t.Run("Express restore of a rider", func(t *testing.T) {
		t.Parallel()
		s, network, fleet := newStrandedFleet(t)
		state := s.ExportState()
		rider := legTrip(s, "harbor", "garden", "market").request
		rider.PodID = "01"
		state.RequestID = s.requestID
		state.Pods[0].Riders = []SavedRequest{SavedRequest(rider)}
		err := checkContractRestoreSemantics(RestoreStateInput{OrderContract: ExpressOrderContract, Network: network, Fleet: fleet, State: state})
		if err == nil || err.Error() != "saved Express rider has incompatible endpoints or path" {
			t.Fatalf("restore error %v", err)
		}
	})
}

// boardLeg returns the simulation of newLegFleet with pod 02 boarding a
// transferred party at its leg origin s1.
func boardLeg(t *testing.T) (*Simulation, *vehicle) {
	t.Helper()
	s := newLegFleet(t, "s0-1", "s1-1", "s1-2")
	v := s.findVehicle("02")
	if err := s.board(v, legTrip(s, "s0", "s1", "s2")); err != nil {
		t.Fatal(err)
	}
	return s, v
}

// TestLegOriginRiderReaders checks the readers of section 7.4 that read
// the origin of a rider: each uses the leg origin.
func TestLegOriginRiderReaders(t *testing.T) {
	t.Parallel()
	t.Run("boardingStation", func(t *testing.T) {
		t.Parallel()
		_, v := boardLeg(t)
		if got := v.boardingStation(); got != "s1" {
			t.Fatalf("boarding station %q", got)
		}
	})
	t.Run("riderOrigin", func(t *testing.T) {
		t.Parallel()
		s, v := boardLeg(t)
		v.Boardings = []RiderBoarding{{BerthID: "s1-1"}}
		if got := s.riderOrigin(v, 0); got != "s1-1" {
			t.Fatalf("rider origin %q", got)
		}
	})
	t.Run("pickupBoardingRecords", func(t *testing.T) {
		t.Parallel()
		s, v := boardLeg(t)
		records, ok := s.pickupBoardingRecords(v)
		if !ok || len(records) != 1 || records[0].BerthID != "s1-1" {
			t.Fatalf("records %+v, %t", records, ok)
		}
	})
	t.Run("legacyBoardingRecords", func(t *testing.T) {
		t.Parallel()
		// Two parties boarded at s1: the transferred party and a plain
		// party from s1.
		s, v := boardLeg(t)
		plain := newTrip(s, "s1", "s2").request
		plain.PodID = v.Pod.ID
		v.Riders = append(v.Riders, plain)
		v.Boardings = []RiderBoarding{{BerthID: "s1-1"}, {BerthID: "s1-1"}}
		if !v.legacyBoardingRecords() {
			t.Fatal("the records of two parties from one leg origin are not legacy records")
		}
	})
	t.Run("checkBoardingBerths", func(t *testing.T) {
		t.Parallel()
		s, v := boardLeg(t)
		pod := s.exportPod(v, newRouteLimits(s.network))
		pod.Boardings = []RiderBoarding{{BerthID: "s1-1"}}
		if err := checkBoardingBerths(s.network, pod); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("passengerRiders", func(t *testing.T) {
		t.Parallel()
		s, v := boardLeg(t)
		r := &physicalRestore{s: s}
		if !r.passengerRiders(v) {
			t.Fatal("a rider with a passenger leg origin is refused")
		}
		v.Riders[0].LegFrom = "p"
		if r.passengerRiders(v) {
			t.Fatal("a rider with a parking leg origin is accepted")
		}
	})
}

// TestLegOriginSavedChecks checks the validators of section 7.4 that read
// LegFrom: a leg origin differs from the destination, names a passenger
// station, and is valid UTF-8.
func TestLegOriginSavedChecks(t *testing.T) {
	t.Parallel()
	t.Run("validTrip", func(t *testing.T) {
		t.Parallel()
		s := newLegFleet(t, "s0-1")
		trip := legTrip(s, "s0", "s1", "s2")
		state := s.ExportState()
		if !state.validTrip(SavedRequest(trip.request), true) {
			t.Fatal("a valid transferred trip is refused")
		}
		trip.request.LegFrom = "s2"
		if state.validTrip(SavedRequest(trip.request), true) {
			t.Fatal("a trip with its leg origin at its destination is accepted")
		}
	})
	t.Run("rider leg origin at the destination", func(t *testing.T) {
		t.Parallel()
		s, v := boardLeg(t)
		for s.tick < 60*TicksPerSecond && v.Pod.Activity != Traveling {
			s.Step()
		}
		if v.Pod.Activity != Traveling {
			t.Fatalf("pod 02 is %s", v.Pod.Activity)
		}
		state := s.ExportState()
		if _, err := state.checkContract(); err != nil {
			t.Fatal(err)
		}
		state.Pods[1].Riders[0].LegFrom = "s2"
		if _, err := state.checkContract(); err == nil {
			t.Fatal("a rider with its leg origin at its destination is accepted")
		}
	})
	t.Run("order text", func(t *testing.T) {
		t.Parallel()
		request := SavedRequest{SharingConsent: SharedConsent, Service: OnDemandService, ID: 1, From: "s0", LegFrom: "s1", To: "s2", PartySize: 1}
		if !validSavedOptionsWithOrderContract(request, ExpressOrderContract) {
			t.Fatal("valid order text is refused")
		}
		request.LegFrom = "\xff"
		if validSavedOptionsWithOrderContract(request, ExpressOrderContract) {
			t.Fatal("a leg origin that is not UTF-8 is accepted")
		}
	})
	t.Run("restore of a parking leg origin", func(t *testing.T) {
		t.Parallel()
		// Each restore tier drops a queued order whose leg origin is not a
		// passenger station, and keeps a valid one.
		s := newLegFleet(t, "s0-1")
		valid, parking := legTrip(s, "s0", "s1", "s2"), legTrip(s, "s0", "p", "s2")
		state := s.ExportState()
		state.RequestID = s.requestID
		state.Waiting = []SavedTrip{{Request: SavedRequest(valid.request), Boarded: true}, {Request: SavedRequest(parking.request), Boarded: true}}
		for _, logical := range []bool{false, true} {
			restored, result, err := RestoreState(RestoreStateInput{Network: s.network, Fleet: place("s0-1"), State: state, LogicalOnly: logical})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(result.Dropped, []int{parking.request.ID}) || len(restored.waiting) != 1 || restored.waiting[0].request != valid.request {
				t.Errorf("logical %t: dropped %v, queue %+v", logical, result.Dropped, restored.waiting)
			}
		}
	})
}

// TestLegOriginHoldsAndCensus checks the finishing-pod hold, the seat
// screen, and guarded supply readers of section 7.4: each reads the leg
// origin.
func TestLegOriginHoldsAndCensus(t *testing.T) {
	t.Parallel()
	for _, busy := range []string{"s0-1", "s1-1"} {
		t.Run("waitForFinishingPod for a pod at "+busy, func(t *testing.T) {
			t.Parallel()
			// Pod 01 is idle at p, before s0. Pod 02 unloads at s0 or at
			// s1 for 2 s, and then reaches the leg origin s1 sooner than
			// pod 01. From s0, pod 01 would be sooner. From s1, pod 02
			// would need the whole loop.
			s := newLegFleet(t, "p-1", busy)
			advance(s, TicksPerSecond)
			unloadFor(s.findVehicle("02"), 2*TicksPerSecond)
			trip := legTrip(s, "s0", "s1", "s2")
			if !s.waitForFinishingPod(&trip, s.findVehicle("01"), map[string]bool{}) || trip.deferPodID != "02" {
				t.Fatalf("the trip does not wait for pod 02: %+v", trip)
			}
		})
	}
	t.Run("keepHold", func(t *testing.T) {
		t.Parallel()
		// No Express berth is at the order origin harbor, so only the leg
		// origin garden has an Express pickup.
		network := expressNetwork(largeRestoreNetwork())
		network.Stations[0].Berths[0].VehicleClasses = classBit(string(GroupClass))
		s, err := NewFleetWithOrderContract(network, []Placement{{ID: "01", Class: ExpressClass, StationID: "market", BerthID: "market-1"}}, ExpressOrderContract)
		if err != nil {
			t.Fatal(err)
		}
		trip := legTrip(s, "harbor", "garden", "market")
		trip.deferUntil, trip.deferCheck, trip.deferPodID = s.tick+maxDispatchDeferral, s.tick+TicksPerSecond, "01"
		s.waiting = []waitingTrip{trip}
		if !s.keepHold(&s.waiting[0], newPass(s)) || s.waiting[0].request.DispatchReason != "Waiting for pod 01 to finish" {
			t.Fatalf("the hold has the reason %q", s.waiting[0].request.DispatchReason)
		}
	})
	t.Run("recordDeparture", func(t *testing.T) {
		t.Parallel()
		s := newLegFleet(t, "s0-1", "s1-1")
		s.SetExperimentRecords(true)
		if err := s.SetSharedRidePartyLimit(4); err != nil {
			t.Fatal(err)
		}
		host := s.findVehicle("02")
		if err := s.board(host, newTrip(s, "s1", "s2")); err != nil {
			t.Fatal(err)
		}
		s.waiting = []waitingTrip{legTrip(s, "s0", "s1", "s2")}
		s.recordDeparture(host)
		if s.seatScreen.DepartureBacklog != 1 {
			t.Fatalf("departure backlog %d, want the transferred party", s.seatScreen.DepartureBacklog)
		}
	})
	t.Run("recordJoinEligible", func(t *testing.T) {
		t.Parallel()
		// The census skips a boarded trip, so this trip has a leg origin
		// and never boarded. Pod 01 leaves p for its leg origin s1, where
		// pod 02 boards for its destination.
		s := newLegFleet(t, "p-1", "s1-1")
		s.SetExperimentRecords(true)
		if err := s.SetSharedRidePartyLimit(4); err != nil {
			t.Fatal(err)
		}
		host := s.findVehicle("02")
		if err := s.board(host, newTrip(s, "s1", "s2")); err != nil {
			t.Fatal(err)
		}
		trip := legTrip(s, "s0", "s1", "s2")
		trip.boarded = false
		v := s.findVehicle("01")
		if err := s.sendPickupForRequest(v, trip.request); err != nil || !releasable(v) {
			t.Fatalf("pod 01 is not on its way: %v", err)
		}
		trip.request.PodID = "01"
		s.waiting = []waitingTrip{trip}
		s.recordJoinEligible(&s.waiting[0], v, newPass(s))
		if s.seatScreen.JoinEligibleExistingStop != 1 {
			t.Fatalf("join census %+v, want the trip", s.seatScreen)
		}
	})
	t.Run("guardedSupply", func(t *testing.T) {
		t.Parallel()
		s := newLegFleet(t, "p-1")
		s.waiting = []waitingTrip{legTrip(s, "s0", "s1", "s2")}
		if _, supplied, _ := guardedSupplyOf(s, "s1", ""); !supplied {
			t.Fatal("the leg origin has no supply")
		}
		if _, supplied, _ := guardedSupplyOf(s, "s0", ""); supplied {
			t.Fatal("the order origin has supply")
		}
	})
}

// TestLegOriginPickupState checks the readers of section 7.4 that test a
// pod on its way to a pickup: the assigned pickup in the arrival chain, the
// buffer membership of a restore, and an onboard pickup.
func TestLegOriginPickupState(t *testing.T) {
	t.Parallel()
	t.Run("assignedPickupFitsRequest in the arrival chain", func(t *testing.T) {
		t.Parallel()
		// Pod 01 is on the berth access lane of the leg origin, so it can
		// no longer divert.
		s := newLegFleet(t, "s0-1")
		trip := legTrip(s, "s2", "s1", "s0")
		v := s.findVehicle("01")
		if err := s.sendPickupForRequest(v, trip.request); err != nil {
			t.Fatal(err)
		}
		trip.request.PodID = "01"
		s.waiting = []waitingTrip{trip}
		for s.tick < 120*TicksPerSecond && (v.Pod.LaneID != v.destination.ID+"-in" || v.Pod.Activity != Traveling) {
			s.Step()
		}
		if v.Pod.LaneID != v.destination.ID+"-in" {
			t.Fatalf("pod 01 is on lane %q", v.Pod.LaneID)
		}
		if !s.assignedPickupFitsRequest(v, s.waiting[0].request) {
			t.Fatal("the pod in the arrival chain of the leg origin does not fit")
		}
	})
	t.Run("restore of a buffered pickup", func(t *testing.T) {
		t.Parallel()
		network := lineNetwork(lineStations(0, 2, 2, 2, 2))
		for index := range network.Lanes {
			lane := &network.Lanes[index]
			for _, station := range network.Stations {
				if lane.To == station.Entry {
					lane.StationID, lane.StationRole = station.ID, StationEntryRole
				}
			}
		}
		s, err := NewFleet(network, place("s0-1"))
		if err != nil {
			t.Fatal(err)
		}
		s.SetStationBuffers(true)
		trip := legTrip(s, "s1", "s3", "s0")
		trip.request.PodID = "01"
		if sendErr := s.sendPickupForRequest(s.findVehicle("01"), trip.request); sendErr != nil {
			t.Fatal(sendErr)
		}
		s.waiting = []waitingTrip{trip}
		advance(s, 5*TicksPerSecond)
		if !s.vehicles[0].buffered {
			t.Fatal("pod 01 is not buffered")
		}
		_, result, err := RestoreState(RestoreStateInput{Network: s.network, Fleet: s.initial, State: s.ExportState()})
		if err != nil || !cleanRestore(result) {
			t.Fatalf("the buffered pickup did not restore physically: %+v %v", result, err)
		}
	})
	t.Run("onboard pickup at the leg origin", func(t *testing.T) {
		t.Parallel()
		// As newOccupiedPickupRide, but the party that joins at garden is
		// a transferred party from harbor.
		s, err := NewFleet(Example(), []Placement{{ID: "01", StationID: "harbor", BerthID: "harbor-1"}})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.SetSharedRidePartyLimit(4); err != nil {
			t.Fatal(err)
		}
		if err := s.SetOnboardPickups(true); err != nil {
			t.Fatal(err)
		}
		for _, pair := range [][2]string{{"harbor", "market"}, {"harbor", "garden"}} {
			if err := submitSharedTrip(s, pair[0], pair[1]); err != nil {
				t.Fatal(err)
			}
		}
		s.waiting = append(s.waiting, legTrip(s, "harbor", "garden", "market"))
		monitorContractEachTick(t, s)
		v := &s.vehicles[0]
		for s.tick < 300*TicksPerSecond && (v.Pod.Activity != Boarding || !v.Pod.Occupied) {
			s.Step()
		}
		if len(v.Riders) != 3 || v.Pod.StationID != "garden" || v.Riders[2].LegFrom != "garden" || len(v.Boardings) != 3 || v.Boardings[2].BerthID != "garden-1" {
			t.Fatalf("no onboard pickup at the leg origin: %+v, records %+v", v.Riders, v.Boardings)
		}
	})
}

// TestLegOriginReassignment checks the pickup swap and transfer readers
// of section 7.4: each compares and sends to the leg origins.
func TestLegOriginReassignment(t *testing.T) {
	t.Parallel()
	// legPickups returns a simulation on the loop s0 to s3 with the
	// transferred trips, each bound to the pod on its way to its leg
	// origin, after 5 s.
	legPickups := func(t *testing.T, berths []string, trips ...[3]string) *Simulation {
		t.Helper()
		s, err := NewFleet(lineNetwork(lineStations(0, 2, 2, 2, 2)), place(berths...))
		if err != nil {
			t.Fatal(err)
		}
		for index, stations := range trips {
			trip := legTrip(s, stations[0], stations[1], stations[2])
			v := &s.vehicles[index]
			if err := s.sendPickupForRequest(v, trip.request); err != nil {
				t.Fatal(err)
			}
			trip.request.PodID = v.Pod.ID
			s.waiting = append(s.waiting, trip)
		}
		s.SetExperimentRecords(true)
		advance(s, 5*TicksPerSecond)
		return s
	}
	// checkRelocations checks that each pod goes to the leg origin of its
	// trip, and the state contract.
	checkRelocations := func(t *testing.T, s *Simulation) {
		t.Helper()
		for _, trip := range s.waiting {
			if v := s.findVehicle(trip.request.PodID); v.Pod.Activity != Idle && v.RelocatingTo != trip.request.LegFrom {
				t.Errorf("pod %s goes to %q for order %d from %s", v.Pod.ID, v.RelocatingTo, trip.request.ID, trip.request.LegFrom)
			}
		}
		if err := s.CheckContract(); err != nil {
			t.Error(err)
		}
	}
	t.Run("swap", func(t *testing.T) {
		t.Parallel()
		// As crossedPickupFixture, with the leg origins s3 and s1.
		s := legPickups(t, []string{"s0-1", "s2-1"}, [3]string{"s2", "s3", "s0"}, [3]string{"s0", "s1", "s2"})
		s.SetPickupSwaps(true)
		s.swapPickups()
		if s.PickupSwapStats().Swaps != 1 || s.waiting[0].request.PodID != "02" {
			t.Fatalf("no swap: %+v %+v", s.PickupSwapStats(), s.waiting)
		}
		checkRelocations(t, s)
	})
	t.Run("same leg origin", func(t *testing.T) {
		t.Parallel()
		s := legPickups(t, []string{"s0-1", "s2-1"}, [3]string{"s2", "s3", "s0"}, [3]string{"s0", "s3", "s1"})
		s.SetPickupSwaps(true)
		s.swapPickups()
		if stats := s.PickupSwapStats(); stats.SameOriginPairs == 0 || stats.Swaps != 0 {
			t.Fatalf("pickup swap stats %+v", stats)
		}
	})
	t.Run("transfer to an idle pod", func(t *testing.T) {
		t.Parallel()
		s := legPickups(t, []string{"s0-1", "s2-1"}, [3]string{"s1", "s3", "s0"})
		s.SetPickupSwaps(true)
		s.swapPickups()
		if s.PickupSwapStats().Transfers != 1 || s.waiting[0].request.PodID != "02" {
			t.Fatalf("no transfer: %+v %+v", s.PickupSwapStats(), s.waiting)
		}
		checkRelocations(t, s)
	})
	t.Run("transfer to an idle pod at the leg origin", func(t *testing.T) {
		t.Parallel()
		// Pod 01 leaves s0 for the leg origin s3, where pod 02 is idle.
		// Dispatch has not run yet.
		s, err := NewFleet(lineNetwork(lineStations(0, 2, 2, 2, 2)), place("s0-1", "s3-1"))
		if err != nil {
			t.Fatal(err)
		}
		trip := legTrip(s, "s1", "s3", "s0")
		if sendErr := s.sendPickupForRequest(s.findVehicle("01"), trip.request); sendErr != nil {
			t.Fatal(sendErr)
		}
		trip.request.PodID = "01"
		s.waiting = []waitingTrip{trip}
		s.SetPickupSwaps(true)
		v := s.findVehicle("02")
		if !s.tryPickupTransfer(0, v) || s.waiting[0].request.PodID != "02" || v.Pod.Activity != Idle || v.RelocatingTo != "" {
			t.Fatalf("no transfer to the idle pod: %+v %+v", s.PickupSwapStats(), v.Pod)
		}
	})
	t.Run("transfer to a moving pod", func(t *testing.T) {
		t.Parallel()
		s, err := NewFleet(lineNetwork(lineStations(0, 2, 2, 2, 2)), place("s0-1", "s2-1"))
		if err != nil {
			t.Fatal(err)
		}
		trip := legTrip(s, "s1", "s3", "s0")
		if err := s.sendPickupForRequest(s.findVehicle("01"), trip.request); err != nil {
			t.Fatal(err)
		}
		trip.request.PodID = "01"
		s.waiting = []waitingTrip{trip}
		station, _ := s.station("s0")
		if err := s.startEmptyMove(s.findVehicle("02"), emptyDestination{station: station.ID, berth: station.Berths[0], rebalance: true}); err != nil {
			t.Fatal(err)
		}
		s.SetExperimentRecords(true)
		advance(s, 5*TicksPerSecond)
		s.SetPickupSwaps(true)
		s.swapPickups()
		if s.PickupSwapStats().Transfers != 1 || s.waiting[0].request.PodID != "02" {
			t.Fatalf("no transfer: %+v %+v", s.PickupSwapStats(), s.waiting)
		}
		checkRelocations(t, s)
	})
}
