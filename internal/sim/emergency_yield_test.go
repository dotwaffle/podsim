package sim

import (
	"reflect"
	"testing"
)

// emergencyYieldFixture returns a simulation in which pod 01 makes an empty
// move with a remote service claim on market-1, and pod 02 travels past
// Market with a passenger. An emergency on pod 02 chooses market-1.
func emergencyYieldFixture(t *testing.T) (s *Simulation, claimant, arrival *vehicle) {
	t.Helper()
	s = newClaimConflictSimulation(t)
	s.emergenciesOn, s.faultsOn = true, true
	if err := s.RequestTrip("garden", "harbor"); err != nil {
		t.Fatal(err)
	}
	claimant, arrival = s.findVehicle("01"), s.findVehicle("02")
	stepUntil(t, s, "a passenger on the road", func() bool { return arrival.Pod.Activity == Traveling && arrival.RidersAboard() > 0 })
	checkRemoteClaim(t, s, claimant)
	return s, claimant, arrival
}

// checkRemoteClaim checks that the pod owns both resources of market-1 only
// as a service claim.
func checkRemoteClaim(t *testing.T, s *Simulation, v *vehicle) {
	t.Helper()
	if v.destination.ID != "market-1" {
		t.Fatalf("pod %s goes to %q, want market-1", v.Pod.ID, v.destination.ID)
	}
	for _, r := range berthResources(v.destination) {
		if s.claimKind(v, r) != claimService {
			t.Fatalf("pod %s holds %v as kind %d", v.Pod.ID, r, s.claimKind(v, r))
		}
	}
}

// startBoundEmergency starts an emergency on the pod and checks that it is
// bound to market-1.
func startBoundEmergency(t *testing.T, s *Simulation, v *vehicle) {
	t.Helper()
	startEmergency(t, s, v, 0)
	if v.op.purpose != opEmergencyUnload || v.destination.ID != "market-1" {
		t.Fatalf("the emergency is bound to %q with %+v, want market-1", v.destination.ID, v.op)
	}
}

// ownsMarketBerth reports whether the pod owns a resource of market-1.
func ownsMarketBerth(s *Simulation, v *vehicle) bool {
	station, _ := s.station("market")
	berth, _ := station.berth("market-1")
	for _, r := range berthResources(berth) {
		if s.owners[r].isPod(v.Pod.ID) {
			return true
		}
	}
	return false
}

// checkNoWaitOn checks that the emergency pod does not wait for a pod that
// is still far from the berth.
func checkNoWaitOn(t *testing.T, s *Simulation, v, claimant *vehicle) {
	t.Helper()
	if v.Pod.BlockedBy == claimant.Pod.ID && claimant.Pod.Activity == Traveling {
		t.Fatalf("tick %d: pod %s waits for pod %s at %q", s.tick, v.Pod.ID, claimant.Pod.ID, v.Pod.WaitReason)
	}
}

// TestEmergencyYieldsRemoteClaim checks that a far empty pod gives up its
// claim on the berth of an emergency unload, as it does for a passenger. The
// emergency pod then unloads while the claimant is still on the road, and
// the claimant also ends in a berth. It covers each kind of claimant.
func TestEmergencyYieldsRemoteClaim(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		setup func(*Simulation, *vehicle)
		// released is the state of the claimant after the yield.
		released bool
	}{
		{"plain", func(_ *Simulation, v *vehicle) { v.Rebalancing = false }, false},
		{"rebalancing", func(s *Simulation, _ *vehicle) { s.positioning = PositioningGuarded }, true},
		{"released", func(_ *Simulation, v *vehicle) { v.Rebalancing, v.released = false, true }, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, claimant, arrival := emergencyYieldFixture(t)
			test.setup(s, claimant)
			checkEmergenciesEachTick(t, s)
			startBoundEmergency(t, s, arrival)
			checkRemoteClaim(t, s, claimant)
			s.Step()
			if ownsMarketBerth(s, claimant) {
				t.Fatal("the claimant keeps its claim")
			}
			if claimant.released != test.released || claimant.Rebalancing {
				t.Fatalf("after the yield, the claimant is released %t, rebalancing %t", claimant.released, claimant.Rebalancing)
			}
			checkIncrementalOwners(t, s)
			if err := s.CheckContract(); err != nil {
				t.Fatal(err)
			}
			unloading := false
			stepUntil(t, s, "the end of the emergency and of the empty move", func() bool {
				checkNoWaitOn(t, s, arrival, claimant)
				if arrival.Pod.Activity == Unloading && !unloading {
					unloading = true
					if arrival.Pod.BerthID != "market-1" || claimant.Pod.Activity != Traveling {
						t.Fatalf("pod 02 unloads at %q while pod 01 is %s", arrival.Pod.BerthID, claimant.Pod.Activity)
					}
				}
				return len(s.emergencies) == 0 && claimant.Pod.Activity == Idle
			})
			if !unloading {
				t.Fatal("pod 02 did not unload")
			}
		})
	}
}

// TestEmergencyYieldLimits checks the pods that make no claim yield, and the
// claims that stay.
func TestEmergencyYieldLimits(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		setup func(t *testing.T, s *Simulation, claimant, arrival *vehicle)
	}{
		{"deferred record", func(_ *testing.T, _ *Simulation, _, v *vehicle) { v.op = operationalDestination{} }},
		{"refuge", func(_ *testing.T, _ *Simulation, _, v *vehicle) { v.op.purpose = opRefuge }},
		{"empty recovery", func(_ *testing.T, _ *Simulation, _, v *vehicle) { v.op.purpose = opEmptyRecovery }},
		{"fault hold on the emergency pod", func(_ *testing.T, _ *Simulation, _, v *vehicle) { v.withdrawn |= faultHold }},
		{"withdrawn claimant", func(_ *testing.T, _ *Simulation, v, _ *vehicle) { v.withdrawn = faultHold }},
		{"another berth", func(_ *testing.T, _ *Simulation, _, v *vehicle) {
			v.destination = Berth{ID: "harbor-1", Node: "harbor-berth"}
		}},
		{"idle emergency pod", func(_ *testing.T, _ *Simulation, _, v *vehicle) { v.Pod.Activity = Idle }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, claimant, arrival := emergencyYieldFixture(t)
			startBoundEmergency(t, s, arrival)
			test.setup(t, s, claimant, arrival)
			before := len(s.owners)
			s.yieldRelocationClaims()
			if !ownsMarketBerth(s, claimant) || before != len(s.owners) {
				t.Fatal("the claimant gave up its claim")
			}
		})
	}
}

// TestEmergencyYieldKeepsRetainedNode checks a claimant that retains the
// berth node. Only the berth is revocable, and the node keeps its owner.
func TestEmergencyYieldKeepsRetainedNode(t *testing.T) {
	t.Parallel()
	s, claimant, arrival := emergencyYieldFixture(t)
	startBoundEmergency(t, s, arrival)
	berth := berthResources(claimant.destination)
	claimant.routeReleases[berth[1]] = claimant.distance + 1
	if kind := s.claimKind(claimant, berth[1]); kind != claimRetained {
		t.Fatalf("the node has kind %d", kind)
	}
	s.yieldRelocationClaims()
	if !s.owners[berth[0]].isZero() || !s.owners[berth[1]].isPod(claimant.Pod.ID) {
		t.Fatalf("after the yield, the berth has owner %q and the node has owner %q", s.owners[berth[0]], s.owners[berth[1]])
	}
}

// TestEmergencyYieldKeepsStoppingNode checks a claimant whose granted track
// holds the berth node. See partialYieldFixture.
func TestEmergencyYieldKeepsStoppingNode(t *testing.T) {
	t.Parallel()
	s, claimant := partialYieldFixture(t)
	s.waiting = nil
	arrival := s.findVehicle("02")
	arrival.Riders = []Request{{ID: 9, From: "garden", To: "harbor", PartySize: 1}}
	arrival.Pod.Activity, arrival.Pod.Occupied, arrival.destination = Traveling, true, claimant.destination
	arrival.op.purpose, arrival.withdrawn = opEmergencyUnload, emergencyHold
	berth := berthResources(claimant.destination)
	s.yieldRelocationClaims()
	if !s.owners[berth[0]].isZero() || !s.owners[berth[1]].isPod(claimant.Pod.ID) || s.claimKind(claimant, berth[1]) != claimStopping {
		t.Fatalf("after the yield, the berth has owner %q, and the node has owner %q and kind %d", s.owners[berth[0]], s.owners[berth[1]], s.claimKind(claimant, berth[1]))
	}
}

// TestEmergencyYieldLinkedClaimant checks a claimant that is in a platoon.
// It gives up its claim, and it keeps its route, because a linked pod cannot
// divert.
func TestEmergencyYieldLinkedClaimant(t *testing.T) {
	t.Parallel()
	s, claimant, arrival := emergencyYieldFixture(t)
	claimant.Rebalancing, claimant.released = false, true
	startBoundEmergency(t, s, arrival)
	claimant.follower = s.vehicleIndex(arrival) + 1
	route := len(claimant.Route)
	s.yieldRelocationClaims()
	if ownsMarketBerth(s, claimant) || len(claimant.Route) != route || claimant.destination.ID != "market-1" {
		t.Fatalf("the linked claimant goes to %q with %d lanes", claimant.destination.ID, len(claimant.Route))
	}
}

// TestEmergencyYieldTwoBerths is the arrangement of the HX11 record: a far
// empty pod claims market-1, and an idle pod stands in market-2.
func TestEmergencyYieldTwoBerths(t *testing.T) {
	t.Parallel()
	network := Example()
	network.Nodes = append(network.Nodes, Node{ID: "market-berth-2", Position: Point{X: 810, Y: 420}})
	network.Lanes = append(network.Lanes,
		Lane{ID: "market-in-2", From: "market-entry", To: "market-berth-2", SpeedLimit: 14, StationID: "market", StationRole: StationBerthAccessRole},
		Lane{ID: "market-out-2", From: "market-berth-2", To: "market-exit", SpeedLimit: 14, StationID: "market", StationRole: StationDepartureRole},
	)
	for i := range network.Stations {
		if network.Stations[i].ID == "market" {
			network.Stations[i].Berths = append(network.Stations[i].Berths, Berth{ID: "market-2", Node: "market-berth-2"})
		}
	}
	s, err := NewFleet(network, []Placement{
		{ID: "01", StationID: "parking", BerthID: "parking-1"},
		{ID: "02", StationID: "garden", BerthID: "garden-1"},
		{ID: "03", StationID: "market", BerthID: "market-2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	s.emergenciesOn = true
	if !rebalanceToMarket(s) {
		t.Fatal("pod 01 did not start a rebalancing move to Market")
	}
	if err := s.RequestTrip("garden", "harbor"); err != nil {
		t.Fatal(err)
	}
	claimant, arrival := s.findVehicle("01"), s.findVehicle("02")
	stepUntil(t, s, "a passenger on the road", func() bool { return arrival.Pod.Activity == Traveling && arrival.RidersAboard() > 0 })
	checkRemoteClaim(t, s, claimant)
	checkEmergenciesEachTick(t, s)
	startBoundEmergency(t, s, arrival)
	stepUntil(t, s, "the unload", func() bool {
		checkNoWaitOn(t, s, arrival, claimant)
		return arrival.Pod.Activity == Unloading
	})
	if claimant.Pod.Activity != Traveling || arrival.Pod.BerthID != "market-1" {
		t.Fatalf("pod 02 unloads at %q while pod 01 is %s", arrival.Pod.BerthID, claimant.Pod.Activity)
	}
	stepUntil(t, s, "the end of both moves", func() bool { return len(s.emergencies) == 0 && claimant.Pod.Activity == Idle })
}

// TestEmergencyYieldCompetingPickup adds a trip from Market, whose pickup
// goes to the same berth. The emergency, the claimant, and the trip all
// finish.
func TestEmergencyYieldCompetingPickup(t *testing.T) {
	t.Parallel()
	s, claimant, arrival := emergencyYieldFixture(t)
	checkEmergenciesEachTick(t, s)
	startBoundEmergency(t, s, arrival)
	if err := s.RequestTrip("market", "garden"); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, s, "the end of the emergency and of the trip", func() bool {
		return len(s.emergencies) == 0 && s.Snapshot().Completed >= 1 && len(s.waiting) == 0 && claimant.Pod.Activity == Idle
	})
}

// TestEmergencyYieldTwoEmergencies binds two emergencies to market-1. The
// record of pod 03 starts first, so the serial order is not the pod order.
// Both unload, and the claimant ends in a berth.
func TestEmergencyYieldTwoEmergencies(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(Example(), []Placement{
		{ID: "01", StationID: "parking", BerthID: "parking-1"},
		{ID: "02", StationID: "garden", BerthID: "garden-1"},
		{ID: "03", StationID: "harbor", BerthID: "harbor-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	s.emergenciesOn = true
	if !rebalanceToMarket(s) {
		t.Fatal("pod 01 did not start a rebalancing move to Market")
	}
	for _, trip := range [][2]string{{"garden", "harbor"}, {"harbor", "market"}} {
		if err := s.RequestTrip(trip[0], trip[1]); err != nil {
			t.Fatal(err)
		}
	}
	claimant, first, second := s.findVehicle("01"), s.findVehicle("02"), s.findVehicle("03")
	stepUntil(t, s, "two passengers on the road", func() bool {
		return first.Pod.Activity == Traveling && first.RidersAboard() > 0 && second.Pod.Activity == Traveling && second.RidersAboard() > 0
	})
	checkRemoteClaim(t, s, claimant)
	checkEmergenciesEachTick(t, s)
	startEmergency(t, s, second, 0)
	startEmergency(t, s, first, 0)
	for _, v := range []*vehicle{first, second} {
		if v.destination.ID != "market-1" {
			bindTo(t, s, v, "market", "market-1")
		}
	}
	s.Step()
	if ownsMarketBerth(s, claimant) {
		t.Fatal("the claimant keeps its claim")
	}
	stepUntil(t, s, "the end of both emergencies", func() bool { return len(s.emergencies) == 0 && claimant.Pod.Activity == Idle })
}

// TestEmergencyYieldNoParking blocks each other berth that the claimant can
// reach. It gives up the claim and keeps its route, and it gets a place
// after the debris is cleared.
func TestEmergencyYieldNoParking(t *testing.T) {
	t.Parallel()
	s, claimant, arrival := emergencyYieldFixture(t)
	claimant.Rebalancing, claimant.released = false, true
	startBoundEmergency(t, s, arrival)
	var ids []string
	for _, lane := range []string{"parking-in-1", "parking-in-2", "garden-in", "harbor-in"} {
		length := s.laneLength(s.network.Lanes[laneIndex(t, s, lane)])
		ids = append(ids, startDebris(t, s, lane, length/2, length/2+2, 0))
	}
	s.Step()
	if ownsMarketBerth(s, claimant) || claimant.destination.ID != "market-1" {
		t.Fatalf("the claimant goes to %q, owns the berth: %t", claimant.destination.ID, ownsMarketBerth(s, claimant))
	}
	stepUntil(t, s, "the end of the emergency", func() bool { return len(s.emergencies) == 0 })
	for _, id := range ids {
		if err := s.clearFault(id); err != nil {
			t.Fatal(err)
		}
	}
	stepUntil(t, s, "a place for the claimant", func() bool { return claimant.Pod.Activity == Idle })
}

// TestEmergencyYieldFaultClear checks the fault lifecycle. A fault on the
// emergency pod keeps the claim, and the clear lets it yield.
func TestEmergencyYieldFaultClear(t *testing.T) {
	t.Parallel()
	s, claimant, arrival := emergencyYieldFixture(t)
	s.faultSettings.evacuationSeconds = 300
	startBoundEmergency(t, s, arrival)
	fault := startFault(t, s, arrival, 0)
	s.Step()
	if !ownsMarketBerth(s, claimant) {
		t.Fatal("a faulted emergency pod made the claimant yield")
	}
	if err := s.clearFault(fault); err != nil {
		t.Fatal(err)
	}
	s.Step()
	if ownsMarketBerth(s, claimant) {
		t.Fatal("the claimant keeps its claim after the clear")
	}
}

// TestEmergencyYieldRestore saves and restores a run right before and right
// after the yield, with a claimant that is released or not. The restored run then makes the same states to the end.
func TestEmergencyYieldRestore(t *testing.T) {
	t.Parallel()
	for _, after := range []bool{false, true} {
		for _, released := range []bool{false, true} {
			s, claimant, arrival := emergencyYieldFixture(t)
			if released {
				claimant.Rebalancing, claimant.released = false, true
			}
			startBoundEmergency(t, s, arrival)
			if after {
				s.Step()
			}
			if ownsMarketBerth(s, claimant) == after {
				t.Fatalf("after %t: the claimant owns the berth: %t", after, !after)
			}
			restored, result, err := RestoreState(emergencyRestoreInput(s, s.ExportState()))
			if err != nil || !cleanRestore(result) {
				t.Fatalf("after %t: %+v, error %v", after, result, err)
			}
			for range 120 * TicksPerSecond {
				if got, want := restored.ExportState(), s.ExportState(); !reflect.DeepEqual(got, want) {
					t.Fatalf("after %t, released %t: tick %d: the states differ", after, released, s.tick)
				}
				s.Step()
				restored.Step()
			}
			if len(s.emergencies) != 0 || claimant.Pod.Activity != Idle {
				t.Fatalf("after %t, released %t: the emergency or the empty move did not end", after, released)
			}
		}
	}
}
