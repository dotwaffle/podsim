package sim

import "testing"

// scanRelocationConflict is the full scan that passengerArrivals replaces.
// It reports whether a pod other than relocating goes to the destination
// berth of relocating with an active passenger or an assigned waiting trip.
func scanRelocationConflict(s *Simulation, relocating *vehicle) bool {
	for i := range s.vehicles {
		arrival := &s.vehicles[i]
		if arrival == relocating || arrival.destination.ID != relocating.destination.ID {
			continue
		}
		activePassenger := arrival.Request != nil && !arrival.Request.Completed &&
			(arrival.Pod.Activity == Boarding || arrival.Pod.Activity == Traveling)
		if activePassenger || s.assigned(arrival.Pod.ID) {
			return true
		}
	}
	return false
}

// scanYieldRelocationClaims is yieldRelocationClaims with a full scan for
// each relocating pod. It is the reference for the passenger arrivals.
func scanYieldRelocationClaims(s *Simulation) {
	for i := range s.vehicles {
		relocating := &s.vehicles[i]
		if relocating.RelocatingTo == "" || !scanRelocationConflict(s, relocating) || s.relocationDestinationAdmitted(relocating) {
			continue
		}
		yielded := false
		for _, claimed := range []resource{
			{kind: berthResource, id: relocating.destination.ID},
			{kind: nodeResource, id: relocating.destination.Node},
		} {
			if s.owners[claimed] == relocating.Pod.ID {
				s.releaseOwned(relocating, claimed)
				yielded = true
			}
		}
		if yielded && relocating.released {
			s.parkReleased(relocating)
		}
	}
}

// checkArrivalsMatchScan checks passengerArrivals against the full scan for
// each relocating pod. It returns the number of pods that conflict.
func checkArrivalsMatchScan(t *testing.T, s *Simulation) int {
	t.Helper()
	arrivals := s.passengerArrivals()
	conflicts := 0
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if v.RelocatingTo == "" {
			continue
		}
		want := scanRelocationConflict(s, v)
		if got := arrivals[v.destination.ID].conflictsWith(v); got != want {
			t.Fatalf("tick %d: pod %s conflicts %v, the full scan gives %v", s.tick, v.Pod.ID, got, want)
		}
		if want {
			conflicts++
		}
	}
	return conflicts
}

// checkYieldMatchesScan runs yieldRelocationClaims and the full scan on two
// clones of s. It checks that the two clones have the same state. It
// reports whether the full scan changed the state.
func checkYieldMatchesScan(t *testing.T, s *Simulation) bool {
	t.Helper()
	got, want := s.Clone(), s.Clone()
	got.yieldRelocationClaims()
	scanYieldRelocationClaims(want)
	if !sameState(got, want) {
		t.Fatalf("tick %d: yieldRelocationClaims and the full scan give different states", s.tick)
	}
	return !sameState(want, s)
}

// timedTrip is a trip request at a simulated second.
type timedTrip struct {
	second   int
	from, to string
}

func TestYieldRelocationClaimsMatchesScanInTraffic(t *testing.T) {
	t.Parallel()
	busyFleet := []Placement{
		{ID: "01", StationID: "parking", BerthID: "parking-1"},
		{ID: "02", StationID: "parking", BerthID: "parking-2"},
		{ID: "03", StationID: "garden"},
		{ID: "04", StationID: "harbor"},
		{ID: "05", StationID: "market", BerthID: "market-1"},
		{ID: "06", StationID: "market", BerthID: "market-2"},
	}
	var busyTrips []timedTrip
	pairs := [][2]string{
		{"market", "garden"}, {"garden", "market"}, {"harbor", "market"}, {"market", "harbor"},
		{"market", "garden"}, {"garden", "harbor"}, {"harbor", "garden"}, {"market", "harbor"},
	}
	for i := range 29 {
		pair := pairs[i%len(pairs)]
		busyTrips = append(busyTrips, timedTrip{second: 7 * i, from: pair[0], to: pair[1]})
	}
	claimFleet := []Placement{{ID: "01", StationID: "parking", BerthID: "parking-1"}, {ID: "02", StationID: "garden", BerthID: "garden-1"}}
	for _, tc := range []struct {
		name    string
		network Network
		fleet   []Placement
		trips   []timedTrip
		seconds int
		// yields is true when a pod must yield a claim in the run.
		yields bool
	}{
		// See TestRedistributionYieldsRemoteClaimToPickup.
		{name: "pickup", network: Example(), fleet: claimFleet, trips: []timedTrip{{45, "market", "garden"}}, seconds: 90, yields: true},
		// See TestRedistributionYieldsRemoteClaimToPassengerArrival.
		{name: "arrival", network: Example(), fleet: claimFleet, trips: []timedTrip{{0, "garden", "market"}}, seconds: 60, yields: true},
		{name: "busy", network: twoBerthMarket(), fleet: busyFleet, trips: busyTrips, seconds: 200, yields: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, err := NewFleet(tc.network, tc.fleet)
			if err != nil {
				t.Fatal(err)
			}
			s.SetRedistribution(true)
			if err := s.SetDemandWeights(map[string]float64{"market": 6, "harbor": 2, "garden": 2}); err != nil {
				t.Fatal(err)
			}
			if err := s.SetSharedRidePartyLimit(2); err != nil {
				t.Fatal(err)
			}
			trips := tc.trips
			conflicts, changes := 0, 0
			for tick := range tc.seconds * TicksPerSecond {
				for len(trips) > 0 && trips[0].second*TicksPerSecond == tick {
					if err := s.RequestTrip(trips[0].from, trips[0].to); err != nil {
						t.Fatal(err)
					}
					trips = trips[1:]
				}
				// The checks run on the state between two ticks. In Step,
				// yieldRelocationClaims runs after dispatch, so a pod that
				// dispatch releases in the same tick is tested only by
				// TestYieldRelocationClaimsReadsPodsAfterPark.
				conflicts += checkArrivalsMatchScan(t, s)
				if checkYieldMatchesScan(t, s) {
					changes++
				}
				s.Step()
			}
			if conflicts == 0 || tc.yields && changes == 0 {
				t.Fatalf("the run did not test a yield: %d conflicts, %d changes", conflicts, changes)
			}
		})
	}
}

// yieldFixture is a claim conflict simulation after one step. Pod 01 is on
// a redistribution move to Market. It holds the Market berth and its node.
// Pod 02 is idle at Garden.
func yieldFixture(t *testing.T) (s *Simulation, relocating, other *vehicle) {
	t.Helper()
	s = newClaimConflictSimulation(t)
	s.Step()
	relocating, other = s.findVehicle("01"), s.findVehicle("02")
	if !relocating.Rebalancing || relocating.destination.ID != "market-1" ||
		s.owners[resource{kind: berthResource, id: "market-1"}] != "01" ||
		s.owners[resource{kind: nodeResource, id: "market-berth"}] != "01" {
		t.Fatalf("pod 01 did not start redistribution to Market: %+v", s.Snapshot())
	}
	return s, relocating, other
}

// assign adds a waiting trip that names the pod.
func assign(s *Simulation, v *vehicle) {
	s.requestID++
	s.waiting = append(s.waiting, waitingTrip{request: Request{ID: s.requestID, From: "market", To: "garden", PartySize: 1, PodID: v.Pod.ID}})
}

func TestPassengerArrivalsMatchScan(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		// setup changes the fixture. It returns the expected conflict of pod 01.
		setup func(s *Simulation, relocating, other *vehicle)
		want  bool
	}{
		{name: "no arrival", setup: func(*Simulation, *vehicle, *vehicle) {}},
		{
			name: "assigned pickup pod", want: false,
			setup: func(s *Simulation, relocating, _ *vehicle) { assign(s, relocating) },
		},
		{
			name: "pod named by two trips", want: false,
			setup: func(s *Simulation, relocating, _ *vehicle) {
				assign(s, relocating)
				assign(s, relocating)
			},
		},
		{
			name: "assigned pod at the berth", want: true,
			setup: func(s *Simulation, relocating, other *vehicle) {
				other.destination = relocating.destination
				assign(s, other)
			},
		},
		{
			name: "assigned pod at another berth", want: false,
			setup: func(s *Simulation, _, other *vehicle) { assign(s, other) },
		},
		{
			name: "assigned pods at the berth", want: true,
			setup: func(s *Simulation, relocating, other *vehicle) {
				other.destination = relocating.destination
				assign(s, relocating)
				assign(s, other)
			},
		},
		{
			name: "active passenger", want: true,
			setup: func(_ *Simulation, relocating, other *vehicle) {
				other.destination = relocating.destination
				other.Pod.Activity = Traveling
				other.Request = &Request{ID: 1, From: "garden", To: "market"}
			},
		},
		{
			name: "boarding passenger", want: true,
			setup: func(_ *Simulation, relocating, other *vehicle) {
				other.destination = relocating.destination
				other.Pod.Activity = Boarding
				other.Request = &Request{ID: 1, From: "garden", To: "market"}
			},
		},
		{
			name: "completed passenger", want: false,
			setup: func(_ *Simulation, relocating, other *vehicle) {
				other.destination = relocating.destination
				other.Pod.Activity = Traveling
				other.Request = &Request{ID: 1, From: "garden", To: "market", Completed: true}
			},
		},
		{
			name: "idle pod with a passenger", want: false,
			setup: func(_ *Simulation, relocating, other *vehicle) {
				other.destination = relocating.destination
				other.Request = &Request{ID: 1, From: "garden", To: "market"}
			},
		},
		{
			name: "active and assigned pod", want: true,
			setup: func(s *Simulation, relocating, other *vehicle) {
				other.destination = relocating.destination
				other.Pod.Activity = Traveling
				other.Request = &Request{ID: 1, From: "garden", To: "market"}
				assign(s, other)
			},
		},
		{
			name: "no destination berth", want: true,
			setup: func(s *Simulation, relocating, other *vehicle) {
				relocating.destination = Berth{}
				other.destination = Berth{}
				assign(s, other)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, relocating, other := yieldFixture(t)
			tc.setup(s, relocating, other)
			if got := scanRelocationConflict(s, relocating); got != tc.want {
				t.Fatalf("the full scan gives %v, want %v", got, tc.want)
			}
			checkArrivalsMatchScan(t, s)
		})
	}
}

// TestYieldRelocationClaimsYieldsNodeClaim checks a pod that holds only the
// node of its destination berth. It must yield the node.
func TestYieldRelocationClaimsYieldsNodeClaim(t *testing.T) {
	t.Parallel()
	s, relocating, other := yieldFixture(t)
	other.destination = relocating.destination
	assign(s, other)
	delete(s.owners, resource{kind: berthResource, id: "market-1"})
	if !checkYieldMatchesScan(t, s) {
		t.Fatal("the full scan did not yield the node claim")
	}
	s.yieldRelocationClaims()
	if owner := s.owners[resource{kind: nodeResource, id: "market-berth"}]; owner != "" {
		t.Fatalf("pod %q holds the Market berth node", owner)
	}
}

// TestYieldRelocationClaimsReadsPodsAfterPark checks that the loop makes the
// passenger arrivals again after parkReleased. The state is not usual,
// because sendPickup clears the released flag of a pod that it assigns.
//
// Pod 01 is released and assigned, and it holds the Market berth. Pod 02
// goes to the same berth, is assigned, and holds the berth node. Pod 02
// makes pod 01 yield, and pod 01 then parks at another berth. After that,
// no pod other than pod 02 goes to the Market berth, so pod 02 keeps the
// node.
func TestYieldRelocationClaimsReadsPodsAfterPark(t *testing.T) {
	t.Parallel()
	s, released, pickup := yieldFixture(t)
	advance(s, 5*TicksPerSecond)
	if released.Pod.Activity != Traveling {
		t.Fatalf("pod 01 is not traveling: %+v", released.Pod)
	}
	released.Rebalancing, released.released = false, true
	assign(s, released)
	pickup.Pod.Activity, pickup.RelocatingTo = Traveling, "market"
	pickup.destination, pickup.destinationStation = released.destination, "market"
	assign(s, pickup)
	s.owners[resource{kind: nodeResource, id: "market-berth"}] = "02"

	if !checkYieldMatchesScan(t, s) {
		t.Fatal("the full scan did not change the state")
	}
	scanYieldRelocationClaims(s)
	if released.destination.ID == "market-1" {
		t.Fatal("pod 01 did not park at another berth")
	}
	if owner := s.owners[resource{kind: nodeResource, id: "market-berth"}]; owner != "02" {
		t.Fatalf("the Market berth node has owner %q, want 02", owner)
	}
}
