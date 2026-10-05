package sim

import (
	"cmp"
	"errors"
	"maps"
	"reflect"
	"slices"
	"testing"
)

// withdrawalMotion is the part of a pod that a withdrawal gate must not
// change: the holds (W1), and the route, physical destination, speed, and
// owners (W4).
type withdrawalMotion struct {
	withdrawn                        serviceHold
	activity                         Activity
	station, berth, lane             string
	speed                            float64
	route                            []string
	destination                      Berth
	destinationStation, relocatingTo string
	rebalancing, released            bool
	owned                            []resource
}

func motionOf(s *Simulation, v *vehicle) withdrawalMotion {
	m := withdrawalMotion{
		withdrawn: v.withdrawn, activity: v.Pod.Activity, station: v.Pod.StationID, berth: v.Pod.BerthID, lane: v.Pod.LaneID,
		speed: v.Pod.Speed, destination: v.destination, destinationStation: v.destinationStation,
		relocatingTo: v.RelocatingTo, rebalancing: v.Rebalancing, released: v.released,
	}
	for _, lane := range v.Route {
		m.route = append(m.route, lane.ID)
	}
	for r, owner := range s.owners {
		if owner == podResourceOwner(v.Pod.ID) {
			m.owned = append(m.owned, r)
		}
	}
	slices.SortFunc(m.owned, func(a, b resource) int {
		return cmp.Or(cmp.Compare(a.kind, b.kind), cmp.Compare(a.id, b.id), cmp.Compare(a.cell, b.cell))
	})
	return m
}

// passengerAt makes pod v board a passenger at its station for the berth
// to.
func passengerAt(s *Simulation, v *vehicle, to Berth) {
	s.requestID++
	v.Pod.Activity, v.Pod.Occupied, v.destination = Boarding, true, to
	v.Riders = []Request{{SharingConsent: SharedConsent, Service: OnDemandService, ID: s.requestID, From: v.Pod.StationID, To: "market", PartySize: 1, PodID: v.Pod.ID}}
}

// withdrawalGateCase is one row of the supply path table of the incident
// contract, section 4.3. build returns a state in which the path selects,
// moves, counts, or yields for v. selects runs the path and reports
// whether it did so for v.
type withdrawalGateCase struct {
	name    string
	build   func(t *testing.T) (*Simulation, *vehicle)
	selects func(t *testing.T, s *Simulation, v *vehicle) bool
}

// finishingPodFixture is a trip at Market and an idle pod 01 at Harbor.
// Pod 02 unloads at Market for 1 s more, so a trip from Market waits for
// pod 02. When check is true, the trip is already on hold for pod 02
// until the next check.
func finishingPodFixture(check bool) func(t *testing.T) (*Simulation, *vehicle) {
	return func(t *testing.T) (*Simulation, *vehicle) {
		t.Helper()
		s, err := NewFleet(Example(), []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "market"}})
		if err != nil {
			t.Fatal(err)
		}
		busy := &s.vehicles[1]
		busy.Pod.Activity, busy.Pod.Occupied, busy.phaseTicks = Unloading, true, TicksPerSecond
		s.requestID = 1
		s.waiting = []waitingTrip{{request: Request{SharingConsent: SharedConsent, Service: OnDemandService, ID: 1, From: "market", To: "harbor", PartySize: 1}}}
		if check {
			trip := &s.waiting[0]
			trip.deferUntil, trip.deferCheck, trip.deferPodID = s.tick+maxDispatchDeferral, s.tick+TicksPerSecond, busy.Pod.ID
		}
		return s, busy
	}
}

// guardedSupplyOf runs guardedSupply with demand at the station. It
// reports the idle count and the supply of the station, and whether the
// berth is busy.
func guardedSupplyOf(s *Simulation, station, berth string) (idle int, supplied, busy bool) {
	n := len(s.network.Stations)
	view := guardedView{weights: make([]float64, n), idle: make([]int, n), assigned: s.assignedPods()}
	demand := make([]bool, n)
	index, _ := s.stationIndex(station)
	demand[index] = true
	supply, busyBerths := s.guardedSupply(guardedSupplyInput{view: &view, demand: demand})
	return view.idle[index], supply[index], busyBerths[berth]
}

func withdrawalGateCases() []withdrawalGateCase {
	harborPod := func(t *testing.T) (*Simulation, *vehicle) {
		t.Helper()
		s := newTraffic(t)
		v := s.findVehicle("01")
		return s, v
	}
	// claimPod returns claimKindFixture with pod 02 boarding a passenger
	// for the Market berth of pod 01, and returns pod 01 or pod 02.
	claimPod := func(relocatingPod bool) func(t *testing.T) (*Simulation, *vehicle) {
		return func(t *testing.T) (*Simulation, *vehicle) {
			t.Helper()
			s, relocating, other := claimKindFixture(t)
			passengerAt(s, other, relocating.destination)
			if relocatingPod {
				return s, relocating
			}
			return s, other
		}
	}
	marketClaimed := func(s *Simulation) bool {
		return s.owners[resource{kind: berthResource, id: "market-1"}] == podResourceOwner("01")
	}
	return []withdrawalGateCase{
		{
			name: "pickup candidate", build: harborPod,
			selects: func(_ *testing.T, s *Simulation, v *vehicle) bool { return s.pickupCandidate(v, nil) },
		},
		{
			name: "local pickup", build: harborPod,
			selects: func(_ *testing.T, s *Simulation, v *vehicle) bool {
				return s.localPickup("harbor", &dispatchPass{}) == v
			},
		},
		{
			name: "shared-ride join",
			build: func(t *testing.T) (*Simulation, *vehicle) {
				t.Helper()
				s, v := harborPod(t)
				if err := s.RequestJourney("01", "garden"); err != nil {
					t.Fatal(err)
				}
				return s, v
			},
			selects: func(_ *testing.T, s *Simulation, v *vehicle) bool {
				return slices.Contains(s.boardingPods(&dispatchPass{})["harbor"], v)
			},
		},
		{
			name: "onboard pickup",
			build: func(t *testing.T) (*Simulation, *vehicle) {
				t.Helper()
				s := newOccupiedPickupRide(t)
				advance(s, 37)
				return s, &s.vehicles[0]
			},
			selects: func(_ *testing.T, s *Simulation, v *vehicle) bool {
				return s.onboardPickupReady(v, Request{SharingConsent: SharedConsent, Service: OnDemandService, ID: s.requestID + 1, From: v.Pod.StationID, To: "market", PartySize: 1})
			},
		},
		{
			// Pod 01 goes to Market for the first trip. Pod 02 is ready there
			// for a later trip, so promotion swaps the pods of the two trips.
			name: "promotion",
			build: func(t *testing.T) (*Simulation, *vehicle) {
				t.Helper()
				s, err := NewFleet(Example(), []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "market"}})
				if err != nil {
					t.Fatal(err)
				}
				if err := s.sendPickup(s.findVehicle("01"), "market"); err != nil {
					t.Fatal(err)
				}
				for _, pod := range []string{"01", "02"} {
					s.requestID++
					s.waiting = append(s.waiting, waitingTrip{request: Request{SharingConsent: SharedConsent, Service: OnDemandService, ID: s.requestID, From: "market", To: "harbor", PartySize: 1, PodID: pod}})
				}
				v := s.findVehicle("02")
				return s, v
			},
			selects: func(_ *testing.T, s *Simulation, v *vehicle) bool {
				return s.promoteReadyPickup(0) && s.waiting[0].request.PodID == v.Pod.ID
			},
		},
		{
			name: "finishing-pod hold", build: finishingPodFixture(false),
			selects: func(_ *testing.T, s *Simulation, v *vehicle) bool {
				return s.waitForFinishingPod(&s.waiting[0], &s.vehicles[0], nil) && s.waiting[0].deferPodID == v.Pod.ID
			},
		},
		{
			name: "finishing-pod hold check", build: finishingPodFixture(true),
			selects: func(_ *testing.T, s *Simulation, _ *vehicle) bool {
				return s.waitForFinishingPod(&s.waiting[0], &s.vehicles[0], nil)
			},
		},
		{
			name: "hold refresh", build: finishingPodFixture(true),
			selects: func(_ *testing.T, s *Simulation, _ *vehicle) bool {
				return s.keepHold(&s.waiting[0], &dispatchPass{})
			},
		},
		{
			name: "pickup swap",
			build: func(t *testing.T) (*Simulation, *vehicle) {
				t.Helper()
				s := crossedPickupFixture(t)
				v := s.findVehicle(s.waiting[0].request.PodID)
				return s, v
			},
			selects: func(_ *testing.T, s *Simulation, v *vehicle) bool { return s.swapEligible(v, &s.waiting[0]) },
		},
		{
			name: "manual journey", build: harborPod,
			selects: func(t *testing.T, s *Simulation, v *vehicle) bool {
				t.Helper()
				err := s.RequestJourneyOptions(v.Pod.ID, TripOptions{To: "garden"})
				if err != nil && !errors.Is(err, ErrBusy) {
					t.Fatalf("RequestJourneyOptions: %v, want nil or ErrBusy", err)
				}
				return err == nil
			},
		},
		{
			name: "guarded idle supply",
			build: func(t *testing.T) (*Simulation, *vehicle) {
				t.Helper()
				s := newLineSimulation(t, lineStations(2, 3, 3), place("s0-1", "p-1"))
				v := s.findVehicle("01")
				return s, v
			},
			selects: func(t *testing.T, s *Simulation, _ *vehicle) bool {
				t.Helper()
				// The pod counts as idle and as supply together, or as
				// neither, so a partial leak fails here.
				idle, supplied, _ := guardedSupplyOf(s, "s0", "")
				if idle > 1 || (idle == 1) != supplied {
					t.Fatalf("guarded supply has %d idle pods and supply %t, want (1, true) or (0, false)", idle, supplied)
				}
				return supplied
			},
		},
		{
			name: "guarded relocating supply",
			build: func(t *testing.T) (*Simulation, *vehicle) {
				t.Helper()
				s := newLineSimulation(t, lineStations(2, 3, 3), place("p-1", "p-2"))
				v := s.findVehicle("01")
				s1, _ := s.station("s1")
				if err := s.startEmptyMove(v, emptyDestination{station: "s1", berth: s1.Berths[0], reserveBerth: true}); err != nil {
					t.Fatal(err)
				}
				return s, v
			},
			selects: func(t *testing.T, s *Simulation, v *vehicle) bool {
				t.Helper()
				_, supplied, busy := guardedSupplyOf(s, "s1", v.destination.ID)
				if !busy {
					t.Fatalf("the destination berth %s of pod %s is not busy", v.destination.ID, v.Pod.ID)
				}
				return supplied
			},
		},
		{
			name: "guarded inbound supply",
			build: func(t *testing.T) (*Simulation, *vehicle) {
				t.Helper()
				s := newLineSimulation(t, lineStations(2, 3, 3, 3, 3), place("s2-1", "p-1"))
				if err := submitSharedTrip(s, "s2", "s3"); err != nil {
					t.Fatal(err)
				}
				v := s.findVehicle("01")
				stepUntil(t, s, "pod 01 travels", func() bool { return v.Pod.Activity == Traveling })
				return s, v
			},
			selects: func(_ *testing.T, s *Simulation, v *vehicle) bool {
				_, supplied, _ := guardedSupplyOf(s, "s3", v.destination.ID)
				return supplied
			},
		},
		{
			name: "guarded candidate",
			build: func(t *testing.T) (*Simulation, *vehicle) {
				t.Helper()
				s := newLineSimulation(t, lineStations(2, 3), place("p-1"))
				v := s.findVehicle("01")
				return s, v
			},
			selects: func(_ *testing.T, s *Simulation, v *vehicle) bool {
				n := len(s.network.Stations)
				rank, found := s.guardedCandidates(guardedView{weights: make([]float64, n), idle: make([]int, n), assigned: s.assignedPods()})
				return found && rank[s.graph.nodes[v.Pod.BerthID]] == vehicleIndex(s, v)
			},
		},
		{
			// Pod 01 makes a rebalancing move to the target. It is the
			// supply of the target, so the forecast starts no move.
			name: "forecast supply",
			build: func(t *testing.T) (*Simulation, *vehicle) {
				t.Helper()
				s := forecastFixture(t, true, 30, 6)
				v := &s.vehicles[0]
				target, _ := s.station("target")
				if err := s.startEmptyMove(v, emptyDestination{station: "target", berth: target.Berths[0], reserveBerth: true, rebalance: true}); err != nil {
					t.Fatal(err)
				}
				return s, v
			},
			selects: func(t *testing.T, s *Simulation, _ *vehicle) bool {
				t.Helper()
				r, err := s.PositionForForecast([]ForecastTarget{{Station: "target", ReleaseTick: 300 * TicksPerSecond, Passengers: 1}})
				if err != nil {
					t.Fatal(err)
				}
				return r.Pod == ""
			},
		},
		{
			// Pod 01 takes a passenger to Market, where pod 02 is idle.
			name: "berth clearing",
			build: func(t *testing.T) (*Simulation, *vehicle) {
				t.Helper()
				s, err := NewFleet(Example(), []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "market"}})
				if err != nil {
					t.Fatal(err)
				}
				if err := s.RequestJourney("01", "market"); err != nil {
					t.Fatal(err)
				}
				v := s.findVehicle("02")
				return s, v
			},
			selects: func(t *testing.T, s *Simulation, v *vehicle) bool {
				t.Helper()
				arrival := s.findVehicle("01")
				blocked := false
				for range 120 * TicksPerSecond {
					s.Step()
					if v.Pod.Activity != Idle || v.RelocatingTo != "" {
						return true
					}
					blocked = blocked || arrival.Pod.WaitReason == BerthOccupied && arrival.Pod.BlockedBy == v.Pod.ID
				}
				if !blocked || arrival.Pod.WaitReason != BerthOccupied || arrival.Pod.BlockedBy != v.Pod.ID {
					t.Fatalf("pod 01 does not wait for pod 02: %+v", arrival.Pod)
				}
				return false
			},
		},
		{
			// Pod 01 is released and holds no claim.
			name: "released parking",
			build: func(t *testing.T) (*Simulation, *vehicle) {
				t.Helper()
				s, relocating, _ := claimKindFixture(t)
				relocating.Rebalancing, relocating.released = false, true
				for _, r := range berthResources(relocating.destination) {
					s.releaseOwned(relocating, r)
				}
				return s, relocating
			},
			selects: func(_ *testing.T, s *Simulation, v *vehicle) bool {
				before := motionOf(s, v)
				s.parkUnclaimedReleased()
				return !reflect.DeepEqual(before, motionOf(s, v))
			},
		},
		{
			name: "relocation yield", build: claimPod(true),
			selects: func(_ *testing.T, s *Simulation, _ *vehicle) bool {
				s.yieldRelocationClaims()
				return !marketClaimed(s)
			},
		},
		{
			name: "passenger arrival", build: claimPod(false),
			selects: func(_ *testing.T, s *Simulation, _ *vehicle) bool {
				s.yieldRelocationClaims()
				return !marketClaimed(s)
			},
		},
		{
			name: "buffer head", build: claimPod(false),
			selects: func(_ *testing.T, s *Simulation, v *vehicle) bool {
				_, ok := s.bufferBerthClaims(v, s.findVehicle("01").destination)
				return ok
			},
		},
		{
			name: "buffer remote", build: claimPod(true),
			selects: func(_ *testing.T, s *Simulation, v *vehicle) bool {
				_, ok := s.bufferBerthClaims(s.findVehicle("02"), v.destination)
				return ok
			},
		},
	}
}

// TestWithdrawalGates checks each supply path of the incident contract,
// section 4.3. In service, the path selects, moves, counts, or yields for
// the pod. With each hold, and with both holds, the path skips the pod
// (W3). The gate changes neither the holds (W1) nor the route, physical
// destination, speed, and owners of the pod (W4).
func TestWithdrawalGates(t *testing.T) {
	t.Parallel()
	for _, test := range withdrawalGateCases() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, v := test.build(t)
			if !test.selects(t, s, v) {
				t.Fatal("control: the path skips the pod in service")
			}
			for _, hold := range []serviceHold{faultHold, emergencyHold, knownServiceHolds} {
				s, v := test.build(t)
				v.withdrawn = hold
				before := motionOf(s, v)
				if test.selects(t, s, v) {
					t.Fatalf("hold %d: the path selects the withdrawn pod", hold)
				}
				if after := motionOf(s, v); !reflect.DeepEqual(before, after) {
					t.Fatalf("hold %d: the path changed the withdrawn pod\nbefore %+v\nafter  %+v", hold, before, after)
				}
				if v.withdrawn&^knownServiceHolds != 0 {
					t.Fatalf("hold %d: unknown hold bits %d", hold, v.withdrawn)
				}
			}
		})
	}
}

// TestWithdrawalPodFitsRequestIsStatic checks that admission ignores the
// holds. podFitsRequest also decides order admission, and a withdrawal
// never refuses a new order.
func TestWithdrawalPodFitsRequestIsStatic(t *testing.T) {
	t.Parallel()
	s := newTraffic(t)
	request := Request{SharingConsent: SharedConsent, Service: OnDemandService, ID: 1, From: "harbor", To: "garden", PartySize: 1}
	for i := range s.vehicles {
		s.vehicles[i].withdrawn = faultHold
	}
	if !s.podFitsRequest(&s.vehicles[0], request) {
		t.Fatal("podFitsRequest refuses a withdrawn pod")
	}
	if err := s.RequestTrip("harbor", "garden"); err != nil {
		t.Fatalf("admission refuses an order when every pod is withdrawn: %v", err)
	}
}

func TestCloneCopiesWithdrawal(t *testing.T) {
	t.Parallel()
	s := newTraffic(t)
	s.vehicles[0].withdrawn = faultHold | emergencyHold
	clone := s.Clone()
	if clone.vehicles[0].withdrawn != knownServiceHolds || !sameState(s, clone) {
		t.Fatalf("clone holds %d", clone.vehicles[0].withdrawn)
	}
	s.vehicles[0].withdrawn = 0
	if clone.vehicles[0].withdrawn != knownServiceHolds {
		t.Fatal("a change of the source changed the clone")
	}
	if !maps.Equal(s.owners, clone.owners) {
		t.Fatal("the clone owners differ")
	}
}
