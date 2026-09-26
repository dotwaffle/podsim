package sim

import (
	"maps"
	"testing"
)

func TestRedistributionYieldsRemoteClaimToPickup(t *testing.T) {
	t.Parallel()
	s := newClaimConflictSimulation(t)
	advance(s, 45*TicksPerSecond)
	if err := s.RequestTrip("market", "garden"); err != nil {
		t.Fatal(err)
	}
	for range 5 * 60 * TicksPerSecond {
		s.Step()
		checkTraffic(t, s.Snapshot())
		if s.Snapshot().Completed == 1 {
			break
		}
	}
	if state := s.Snapshot(); state.Completed != 1 {
		t.Fatalf("passenger pickup and redistribution did not make progress: %+v", state)
	}
}

func TestRedistributionYieldsRemoteClaimToPassengerArrival(t *testing.T) {
	t.Parallel()
	s := newClaimConflictSimulation(t)
	if err := s.RequestTrip("garden", "market"); err != nil {
		t.Fatal(err)
	}
	for range 5 * 60 * TicksPerSecond {
		s.Step()
		checkTraffic(t, s.Snapshot())
		if s.Snapshot().Completed == 1 {
			break
		}
	}
	if state := s.Snapshot(); state.Completed != 1 {
		t.Fatalf("passenger arrival and redistribution did not make progress: %+v", state)
	}
	if err := s.RequestTrip("market", "harbor"); err != nil {
		t.Fatal(err)
	}
	for range 5 * 60 * TicksPerSecond {
		s.Step()
		checkTraffic(t, s.Snapshot())
		if s.Snapshot().Completed == 2 {
			break
		}
	}
	if state := s.Snapshot(); state.Completed != 2 {
		t.Fatalf("following passenger did not clear the yielded arrival: %+v", state)
	}
}

func TestRedistributionKeepsClaimWhenPassengerUsesAnotherBerth(t *testing.T) {
	t.Parallel()
	s := newClaimConflictSimulation(t)
	addMarketBerth(s)
	s.Step()
	rebalancing := s.findVehicle("01")
	claimed := resource{kind: berthResource, id: rebalancing.destination.ID}
	if rebalancing.destination.ID != "market-1" || s.owners[claimed] != "01" {
		t.Fatalf("unexpected redistribution destination: %+v", rebalancing.Vehicle)
	}
	if err := s.RequestTrip("market", "garden"); err != nil {
		t.Fatal(err)
	}
	s.Step()
	pickup := s.findVehicle(s.waiting[0].request.PodID)
	if pickup == nil || pickup.destination.ID != "market-2" {
		t.Fatalf("pickup did not use the free berth: %+v", s.Snapshot())
	}
	if s.owners[claimed] != "01" {
		t.Fatal("redistribution yielded a claim that did not conflict")
	}
}

func TestRedistributionNeverYieldsAdmittedDestination(t *testing.T) {
	t.Parallel()
	s := newClaimConflictSimulation(t)
	s.Step()
	rebalancing := s.findVehicle("01")
	if !rebalancing.Rebalancing {
		t.Fatal("pod 01 did not start redistribution")
	}
	before := maps.Clone(s.owners)
	rebalancing.reservedThrough = len(rebalancing.blocks) - 1
	if !s.relocationDestinationAdmitted(rebalancing) {
		t.Fatal("test did not admit the destination block")
	}
	// Mark another pod as assigned to the same pickup berth.
	pickup := s.findVehicle("02")
	pickup.destination = rebalancing.destination
	s.waiting = append(s.waiting, waitingTrip{request: Request{ID: 1, From: "market", To: "garden", PodID: "02"}})
	s.yieldRelocationClaims()
	for claimed, owner := range before {
		if s.owners[claimed] != owner {
			t.Fatalf("admitted resource %+v changed owner from %q to %q", claimed, owner, s.owners[claimed])
		}
	}
}

func TestRedistributionKeepsClaimForCompletedPassenger(t *testing.T) {
	t.Parallel()
	s := newClaimConflictSimulation(t)
	s.Step()
	rebalancing := s.findVehicle("01")
	claimed := resource{kind: berthResource, id: rebalancing.destination.ID}
	completed := s.findVehicle("02")
	completed.destination = rebalancing.destination
	completed.Request = &Request{ID: 1, From: "garden", To: "market", Completed: true}
	s.yieldRelocationClaims()
	if s.owners[claimed] != "01" {
		t.Fatal("completed passenger caused a remote redistribution claim to yield")
	}
}

func TestRedistributionDoesNotDeleteAnotherPodsClaim(t *testing.T) {
	t.Parallel()
	s := newClaimConflictSimulation(t)
	s.Step()
	rebalancing := s.findVehicle("01")
	pickup := s.findVehicle("02")
	pickup.destination = rebalancing.destination
	s.waiting = append(s.waiting, waitingTrip{request: Request{ID: 1, From: "market", To: "garden", PodID: "02"}})
	for _, claimed := range []resource{
		{kind: berthResource, id: rebalancing.destination.ID},
		{kind: nodeResource, id: rebalancing.destination.Node},
	} {
		s.owners[claimed] = "02"
	}
	s.yieldRelocationClaims()
	if s.owners[resource{kind: berthResource, id: rebalancing.destination.ID}] != "02" ||
		s.owners[resource{kind: nodeResource, id: rebalancing.destination.Node}] != "02" {
		t.Fatal("redistribution deleted another pod's destination claim")
	}
}

// newClaimConflictSimulation returns the example network with pod 01 in
// parking and pod 02 idle at Garden. Pod 01 makes a rebalancing move to
// Market-1, as a positioning move does, in off mode.
func newClaimConflictSimulation(t *testing.T) *Simulation {
	t.Helper()
	s, err := NewFleet(Example(), []Placement{
		{ID: "01", StationID: "parking", BerthID: "parking-1"},
		{ID: "02", StationID: "garden", BerthID: "garden-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !rebalanceToMarket(s) {
		t.Fatal("pod 01 did not start a rebalancing move to Market")
	}
	return s
}

// rebalanceToMarket starts a rebalancing move to the first free berth of
// Market, as a positioning move does. The pod is the first idle empty pod
// in parking that no waiting trip names and that has no rebalance cooldown.
// A berth is free when no pod holds the berth or its node. The move
// reserves the berth. It reports whether a move started.
func rebalanceToMarket(s *Simulation) bool {
	station, ok := s.station("market")
	if !ok {
		return false
	}
	for _, berth := range station.Berths {
		if s.owners[resource{kind: berthResource, id: berth.ID}] != "" || s.owners[resource{kind: nodeResource, id: berth.Node}] != "" {
			continue
		}
		for index := range s.vehicles {
			v := &s.vehicles[index]
			from, _ := s.station(v.Pod.StationID)
			if v.Pod.Activity != Idle || v.Pod.Occupied || !from.ParkingOnly || s.assigned(v.Pod.ID) || v.rebalanceAfter > s.tick {
				continue
			}
			return s.startEmptyMove(v, emptyDestination{station: station.ID, berth: berth, reserveBerth: true, rebalance: true}) == nil
		}
		return false
	}
	return false
}

// rebalancing reports whether a pod makes a rebalancing move.
func rebalancing(s *Simulation) bool {
	for index := range s.vehicles {
		if s.vehicles[index].Rebalancing {
			return true
		}
	}
	return false
}
