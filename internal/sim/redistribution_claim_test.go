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
	if !s.redistributionDestinationAdmitted(rebalancing) {
		t.Fatal("test did not admit the destination block")
	}
	// Mark another pod as assigned to the same pickup berth.
	pickup := s.findVehicle("02")
	pickup.destination = rebalancing.destination
	s.waiting = append(s.waiting, waitingTrip{request: Request{ID: 1, From: "market", To: "garden", PodID: "02"}})
	s.yieldRedistributionClaims()
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
	s.yieldRedistributionClaims()
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
	s.yieldRedistributionClaims()
	if s.owners[resource{kind: berthResource, id: rebalancing.destination.ID}] != "02" ||
		s.owners[resource{kind: nodeResource, id: rebalancing.destination.Node}] != "02" {
		t.Fatal("redistribution deleted another pod's destination claim")
	}
}

func newClaimConflictSimulation(t *testing.T) *Simulation {
	t.Helper()
	s, err := NewFleet(Example(), []Placement{
		{ID: "01", StationID: "parking", BerthID: "parking-1"},
		{ID: "02", StationID: "garden", BerthID: "garden-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetDemandWeights(map[string]float64{"market": 6, "harbor": 2, "garden": 2}); err != nil {
		t.Fatal(err)
	}
	s.SetRedistribution(true)
	return s
}
