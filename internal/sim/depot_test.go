package sim

import (
	"errors"
	"slices"
	"testing"
)

// stepDepot steps s up to ticks times and checks the safety observation
// after each step. It stops when done reports true, and fails the test
// when the ticks run out first.
func stepDepot(t *testing.T, s *Simulation, ticks int, done func() bool) {
	t.Helper()
	for range ticks {
		s.Step()
		if _, err := s.SafetyObservation().Check(); err != nil {
			t.Fatalf("tick %d: %v", s.tick, err)
		}
		if done() {
			return
		}
	}
	t.Fatalf("not done after %d ticks", ticks)
}

// sendToDepot starts the depot form of an empty move for a pod.
func sendToDepot(t *testing.T, s *Simulation, id string) *vehicle {
	t.Helper()
	v := s.findVehicle(id)
	if err := s.startEmptyMove(v, emptyDestination{station: "parking", depot: true}); err != nil {
		t.Fatal(err)
	}
	return v
}

// binding records the pods in the order in which they bind, with the slot
// that each one binds.
type binding struct {
	pod, slot string
}

// watchBindings returns a function for stepDepot that records each new
// binding of the pods. It checks that a pod that binds owns a resource of
// its slot only when its reservation holds it, because binding writes no
// owner. It reports true when each pod is idle.
func watchBindings(t *testing.T, s *Simulation, pods []*vehicle, got *[]binding) func() bool {
	t.Helper()
	bound := make(map[string]bool)
	return func() bool {
		idle := true
		for _, v := range pods {
			if _, slot := s.depots.slot(v.destination.ID); slot && !bound[v.Pod.ID] {
				bound[v.Pod.ID] = true
				*got = append(*got, binding{pod: v.Pod.ID, slot: v.destination.ID})
				var reserved []resource
				for resources := range v.blocks.spanResources(0, v.reservedThrough+1) {
					reserved = append(reserved, resources...)
				}
				for _, r := range berthResources(v.destination) {
					if s.owners[r].isPod(v.Pod.ID) && !slices.Contains(reserved, r) {
						t.Fatalf("tick %d: %s owns %+v with no grant when it binds", s.tick, v.Pod.ID, r)
					}
				}
			}
			idle = idle && v.Pod.Activity == Idle
		}
		return idle
	}
}

// TestDepotLaneView checks the lane view, the lane choice, and the soft
// gate of depotGoal: a faulted or withdrawn pod stops its lane, the lane
// with the fewest pods wins, and each unbound pod takes one free slot.
func TestDepotLaneView(t *testing.T) {
	t.Parallel()
	network := shortDepotNetwork(2, 3)
	s, err := NewFleet(network, depotPlacements(network, 3))
	if err != nil {
		t.Fatal(err)
	}
	depot := s.depots.stations["parking"]
	station, _ := s.station("parking")
	market := s.findVehicle("market-pod")
	view := s.depotLaneView(depot, nil)
	if view[0] != (depotLaneState{rear: 1, pods: 2}) || view[1] != (depotLaneState{rear: 0, pods: 1}) {
		t.Fatalf("view %+v", view)
	}
	if _, slot, ok := s.depotChoice(market, depot); !ok || slot.ID != depotSlotID(2, 2) {
		t.Fatalf("choice %q %v", slot.ID, ok)
	}
	lane2 := s.findVehicle("depot-pod-02")
	lane2.faulted = true
	if _, slot, ok := s.depotChoice(market, depot); !ok || slot.ID != depotSlotID(1, 3) {
		t.Fatalf("choice with a faulted pod %q %v", slot.ID, ok)
	}
	if !s.depotGoal(market, station) {
		t.Fatal("no goal with one free slot")
	}
	// An unbound pod that goes to the entry takes the last free slot.
	garden := sendToDepot(t, s, "garden-pod")
	if s.depotGoal(market, station) || !s.depotGoal(garden, station) {
		t.Fatal("the unbound pod does not take the free slot")
	}
	lane2.faulted, lane2.withdrawn = false, faultHold
	if view := s.depotLaneView(depot, nil); !view[1].stopped || view[0].stopped {
		t.Fatalf("view with a withdrawn pod %+v", view)
	}
	lane2.withdrawn = 0
	if !s.depotGoal(market, station) {
		t.Fatal("no goal with two free slots and one unbound pod")
	}
}

// TestDepotBindingOrder checks that the pods bind in the order in which
// they reach the entry, each to the lane with the fewest pods and then the
// lowest index, that they park at those slots, and that a pod owns no slot
// resource when it binds.
func TestDepotBindingOrder(t *testing.T) {
	t.Parallel()
	network := shortDepotNetwork(2, 3)
	s, err := NewFleet(network, depotPlacements(network, 0))
	if err != nil {
		t.Fatal(err)
	}
	pods := []*vehicle{sendToDepot(t, s, "market-pod"), sendToDepot(t, s, "garden-pod"), sendToDepot(t, s, "harbor-pod")}
	var got []binding
	stepDepot(t, s, 300*TicksPerSecond, watchBindings(t, s, pods, &got))
	want := []binding{{"market-pod", depotSlotID(1, 1)}, {"garden-pod", depotSlotID(2, 1)}, {"harbor-pod", depotSlotID(1, 2)}}
	if len(got) != len(want) {
		t.Fatalf("bindings %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("bindings %+v, want %+v", got, want)
		}
	}
	for _, b := range want {
		if v := s.findVehicle(b.pod); v.Pod.BerthID != b.slot || v.Pod.StationID != "parking" {
			t.Fatalf("%s idle at %s %s", b.pod, v.Pod.StationID, v.Pod.BerthID)
		}
	}
}

// TestDepotChainBinding checks the case of two pods that bind one lane on
// a 60 m fan: A binds slot 1 and B binds slot 2 behind it before A reaches
// its slot. A crosses slot 2 and releases it, and the grants of B follow.
func TestDepotChainBinding(t *testing.T) {
	t.Parallel()
	network := depotNetwork(depotLayout{lanes: 1, slots: 3, pitch: 6, width: 12, fan: 60})
	s, err := NewFleet(network, depotPlacements(network, 0))
	if err != nil {
		t.Fatal(err)
	}
	a, b := sendToDepot(t, s, "market-pod"), sendToDepot(t, s, "garden-pod")
	var got []binding
	both := false
	watch := watchBindings(t, s, []*vehicle{a, b}, &got)
	stepDepot(t, s, 300*TicksPerSecond, func() bool {
		done := watch()
		both = both || len(got) == 2 && a.Pod.Activity != Idle
		return done
	})
	if !both || len(got) != 2 || got[0] != (binding{"market-pod", depotSlotID(1, 1)}) || got[1] != (binding{"garden-pod", depotSlotID(1, 2)}) {
		t.Fatalf("bindings %+v, both bound before A parked: %v", got, both)
	}
	if a.Pod.BerthID != depotSlotID(1, 1) || b.Pod.BerthID != depotSlotID(1, 2) {
		t.Fatalf("A at %s, B at %s", a.Pod.BerthID, b.Pod.BerthID)
	}
}

// TestDepotEntryWait checks a pod that reaches the entry with no eligible
// lane and no free berth elsewhere: it waits at the entry and reports
// ParkingUnavailable, and it binds when a lane becomes eligible.
func TestDepotEntryWait(t *testing.T) {
	t.Parallel()
	network := shortDepotNetwork(2, 3)
	// Lane 1 is full, the head of lane 2 is the mover, and a withdrawn pod
	// at slot 2 of lane 2 stops that lane.
	placements := depotPlacements(network, 0)
	for _, slot := range []string{depotSlotID(1, 1), depotSlotID(1, 2), depotSlotID(1, 3), depotSlotID(2, 1), depotSlotID(2, 2)} {
		placements = append(placements, Placement{ID: "pod-" + slot, StationID: "parking", BerthID: slot})
	}
	s, err := NewFleet(network, placements)
	if err != nil {
		t.Fatal(err)
	}
	stopper := s.findVehicle("pod-" + depotSlotID(2, 2))
	stopper.withdrawn = faultHold
	mover := sendToDepot(t, s, "pod-"+depotSlotID(2, 1))
	stepDepot(t, s, 300*TicksPerSecond, func() bool {
		return mover.Pod.WaitReason == ParkingUnavailable
	})
	if !s.depotEntryWait(mover) || mover.destination.ID != "" || mover.Pod.Speed != 0 {
		t.Fatalf("mover %+v destination %q", mover.Pod, mover.destination.ID)
	}
	stopper.withdrawn = 0
	stepDepot(t, s, 120*TicksPerSecond, func() bool { return mover.Pod.Activity == Idle })
	if mover.Pod.BerthID != depotSlotID(2, 3) {
		t.Fatalf("mover idle at %s", mover.Pod.BerthID)
	}
}

// TestDepotEntryRedirect checks a pod that reaches a full depot: it does
// not bind, and parkReleased sends it to a free berth elsewhere.
func TestDepotEntryRedirect(t *testing.T) {
	t.Parallel()
	network := shortDepotNetwork(1, 2)
	s, err := NewFleet(network, depotPlacements(network, 2))
	if err != nil {
		t.Fatal(err)
	}
	mover := sendToDepot(t, s, "market-pod")
	stepDepot(t, s, 300*TicksPerSecond, func() bool { return mover.Pod.Activity == Idle })
	if mover.Pod.BerthID != "market-1" {
		t.Fatalf("mover idle at %s %s", mover.Pod.StationID, mover.Pod.BerthID)
	}
}

// TestDepotDiversion checks that an unbound pod can divert and a bound pod
// cannot.
func TestDepotDiversion(t *testing.T) {
	t.Parallel()
	network := shortDepotNetwork(1, 3)
	s, err := NewFleet(network, depotPlacements(network, 0))
	if err != nil {
		t.Fatal(err)
	}
	v := sendToDepot(t, s, "market-pod")
	diverted := false
	stepDepot(t, s, 120*TicksPerSecond, func() bool {
		_, _, ok := s.divertStart(v)
		if v.destination.ID == "" {
			diverted = diverted || ok
			return false
		}
		if ok {
			t.Fatalf("tick %d: the bound pod can divert", s.tick)
		}
		return v.Pod.Activity == Idle
	})
	if !diverted {
		t.Fatal("the unbound pod never could divert")
	}
}

// TestDepotGuardedBump checks that guardedBumpToParking sends a blocker to
// the depot entry with no claim, and that the blocker binds there.
func TestDepotGuardedBump(t *testing.T) {
	t.Parallel()
	network := shortDepotNetwork(2, 3)
	s, err := NewFleet(network, depotPlacements(network, 1))
	if err != nil {
		t.Fatal(err)
	}
	blocker := s.findVehicle("market-pod")
	if !s.guardedBumpToParking(guardedBump{blocker: blocker, from: "market-berth"}) {
		t.Fatal("no bump")
	}
	if blocker.destinationStation != "parking" || blocker.destination.ID != "" || blocker.RelocatingTo != "parking" {
		t.Fatalf("bump to %s %q", blocker.destinationStation, blocker.destination.ID)
	}
	stepDepot(t, s, 120*TicksPerSecond, func() bool { return blocker.Pod.Activity == Idle })
	if blocker.Pod.BerthID != depotSlotID(2, 1) {
		t.Fatalf("blocker idle at %s", blocker.Pod.BerthID)
	}
}

// TestDepotReleasedPark checks that parkReleased sends a released pod to
// the depot entry, that the pod is no longer released, so it is not sent
// again in each tick, and that it binds and parks.
func TestDepotReleasedPark(t *testing.T) {
	t.Parallel()
	network := shortDepotNetwork(1, 3)
	s, err := NewFleet(network, depotPlacements(network, 0))
	if err != nil {
		t.Fatal(err)
	}
	// The pods at market and garden trade berths, so each passenger berth
	// is held or named.
	v, other := s.findVehicle("market-pod"), s.findVehicle("garden-pod")
	if err := s.startEmptyMove(v, emptyDestination{station: "garden", berth: Berth{ID: "garden-1", Node: "garden-berth"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.startEmptyMove(other, emptyDestination{station: "market", berth: Berth{ID: "market-1", Node: "market-berth"}}); err != nil {
		t.Fatal(err)
	}
	v.released = true
	s.parkUnclaimedReleased()
	if v.destinationStation != "parking" || v.destination.ID != "" || v.released {
		t.Fatalf("released pod goes to %s %q, released %v", v.destinationStation, v.destination.ID, v.released)
	}
	stepDepot(t, s, 120*TicksPerSecond, func() bool { return v.Pod.Activity == Idle })
	if v.Pod.BerthID != depotSlotID(1, 1) {
		t.Fatalf("pod idle at %s", v.Pod.BerthID)
	}
}

// TestDepotClassEligibility checks that depotGoal and the lane choice are
// specific to the class of the pod: a lane whose fan or slot a class cannot
// use is not eligible for it, and a depot with no eligible lane is no goal
// for that class.
func TestDepotClassEligibility(t *testing.T) {
	t.Parallel()
	compact, err := NewClassSet(string(CompactClass))
	if err != nil {
		t.Fatal(err)
	}
	fleet := func(network Network, lane2 int) (*Simulation, *vehicle, *vehicle) {
		t.Helper()
		placements := depotPlacements(network, 0)
		placements[2].Class = CompactClass
		for slot := 1; slot <= lane2; slot++ {
			placements = append(placements, Placement{ID: depotSlotID(2, slot) + "-pod", StationID: "parking", BerthID: depotSlotID(2, slot), Class: CompactClass})
		}
		s, err := NewFleet(network, placements)
		if err != nil {
			t.Fatal(err)
		}
		legacy, small := s.findVehicle("harbor-pod"), s.findVehicle("market-pod")
		return s, legacy, small
	}
	network := shortDepotNetwork(2, 2)
	findLane(&network, "parking-fan-1").VehicleClasses = compact
	s, legacy, small := fleet(network, 1)
	depot := s.depots.stations["parking"]
	station, _ := s.station("parking")
	if _, slot, ok := s.depotChoice(legacy, depot); !ok || slot.ID != depotSlotID(2, 2) {
		t.Fatalf("legacy choice %q %v", slot.ID, ok)
	}
	if _, slot, ok := s.depotChoice(small, depot); !ok || slot.ID != depotSlotID(1, 1) {
		t.Fatalf("compact choice %q %v", slot.ID, ok)
	}
	s, legacy, small = fleet(network, 2)
	if s.depotGoal(legacy, station) || !s.depotGoal(small, station) {
		t.Fatal("the goal with a full lane 2 is not specific to the class")
	}
	network = shortDepotNetwork(2, 2)
	for i := range network.Stations {
		if network.Stations[i].ID == "parking" {
			for j := range network.Stations[i].Berths {
				network.Stations[i].Berths[j].VehicleClasses = compact
			}
		}
	}
	s, legacy, small = fleet(network, 0)
	station, _ = s.station("parking")
	if s.depotGoal(legacy, station) || !s.depotGoal(small, station) {
		t.Fatal("the goal with compact slots is not specific to the class")
	}
	if s.park(legacy) {
		t.Fatal("a legacy pod parks at compact slots")
	}
}

// TestDepotClassPath checks that a lane is not eligible for a class that a
// lane of its lane path ahead of the slot does not admit. A legacy pod
// that bound slot 2 behind a compact chain lane could never move up, and
// the lane would keep that slot from dispatch. The pod goes to a free
// berth elsewhere, and a compact pod still binds slot 2.
func TestDepotClassPath(t *testing.T) {
	t.Parallel()
	compact, err := NewClassSet(string(CompactClass))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"parking-chain-1-1", "parking-out-1", "parking-throat-1"} {
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			network := shortDepotNetwork(1, 2)
			findLane(&network, id).VehicleClasses = compact
			placements := depotPlacements(network, 0)
			placements[2].Class = CompactClass
			placements = append(placements, Placement{ID: "head", StationID: "parking", BerthID: depotSlotID(1, 1), Class: CompactClass})
			s, err := NewFleet(network, placements)
			if err != nil {
				t.Fatal(err)
			}
			legacy, small := s.findVehicle("harbor-pod"), s.findVehicle("market-pod")
			depot := s.depots.stations["parking"]
			station, _ := s.station("parking")
			if _, slot, ok := s.depotChoice(legacy, depot); ok || s.depotGoal(legacy, station) {
				t.Fatalf("legacy choice %q", slot.ID)
			}
			if _, slot, ok := s.depotChoice(small, depot); !ok || slot.ID != depotSlotID(1, 2) || !s.depotGoal(small, station) {
				t.Fatalf("compact choice %q %v", slot.ID, ok)
			}
			sendToDepot(t, s, legacy.Pod.ID)
			stepDepot(t, s, 300*TicksPerSecond, func() bool { return legacy.Pod.Activity == Idle })
			if legacy.Pod.StationID == "parking" {
				t.Fatalf("legacy pod idle at %s", legacy.Pod.BerthID)
			}
		})
	}
}

// headFleet returns a depot of 2 lanes and 2 slots with a legacy head in
// lane 1, a compact head in lane 2, and a compact pod at slot 2 of lane 1.
// The only other pod is a compact pod at market.
func headFleet(t *testing.T) *Simulation {
	t.Helper()
	return headFleetOn(t, shortDepotNetwork(2, 2))
}

// headFleetOn is headFleet on a network.
func headFleetOn(t *testing.T, network Network) *Simulation {
	t.Helper()
	s, err := NewFleet(network, []Placement{
		{ID: "market-pod", StationID: "market", Class: CompactClass},
		{ID: "lane-1-head", StationID: "parking", BerthID: depotSlotID(1, 1)},
		{ID: "lane-2-head", StationID: "parking", BerthID: depotSlotID(2, 1), Class: CompactClass},
		{ID: "lane-1-rear", StationID: "parking", BerthID: depotSlotID(1, 2), Class: CompactClass},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// TestDepotOldestEligibleHead checks that a pickup gets the oldest depot
// head that can serve it: a head that does not fit the party, or that the
// trip excludes, does not hide the head of another lane, and a pod behind
// a head is never a candidate.
func TestDepotOldestEligibleHead(t *testing.T) {
	t.Parallel()
	s := headFleet(t)
	group := newTrip(s, "harbor", "garden")
	group.request.PartySize = 2
	pickup := func(request Request, excluded string, assigned ...string) string {
		pass := &dispatchPass{assigned: map[string]bool{}}
		for _, id := range assigned {
			pass.assigned[id] = true
		}
		if v := s.pickupPodForRequest(request, excluded, pass); v != nil {
			return v.Pod.ID
		}
		return ""
	}
	if got := pickup(group.request, ""); got != "lane-2-head" {
		t.Fatalf("pickup for a party of 2: %q", got)
	}
	single := newTrip(s, "harbor", "garden")
	if got := pickup(single.request, ""); got != "lane-1-head" {
		t.Fatalf("pickup for a party of 1: %q", got)
	}
	if got := pickup(single.request, "lane-1-head"); got != "lane-2-head" {
		t.Fatalf("pickup that excludes the lane 1 head: %q", got)
	}
	if got := pickup(single.request, "", "lane-1-head"); got != "lane-2-head" {
		t.Fatalf("pickup with an assigned lane 1 head: %q", got)
	}
	if !s.pickupAvailable("harbor", "lane-1-head", &dispatchPass{assigned: map[string]bool{"market-pod": true}}) {
		t.Fatal("the lane 2 head is not available when the trip excludes the lane 1 head")
	}
	if s.pickupAvailable("harbor", "lane-1-head", &dispatchPass{assigned: map[string]bool{"lane-2-head": true, "market-pod": true}}) {
		t.Fatal("the pod behind an excluded head is available")
	}
	if !s.pickupAvailable("harbor", "", &dispatchPass{assigned: map[string]bool{"lane-1-head": true, "market-pod": true}}) {
		t.Fatal("the lane 2 head is not available when the lane 1 head is assigned")
	}
	// A legacy pod cannot leave lane 1, so its head has no pickup route.
	network := shortDepotNetwork(2, 2)
	compact, err := NewClassSet(string(CompactClass))
	if err != nil {
		t.Fatal(err)
	}
	findLane(&network, "parking-throat-1").VehicleClasses = compact
	s = headFleetOn(t, network)
	if got := pickup(single.request, ""); got != "lane-2-head" {
		t.Fatalf("pickup with no route from the lane 1 head: %q", got)
	}
}

// TestDepotPickupTransfer checks that pickup reassignment moves a pickup
// only to the oldest depot head that can take it: a head that does not fit
// the party, that has no route to the pickup, that the trip excludes, or
// that another trip names does not hide the head of another lane, and a
// pod behind a head never takes a pickup.
func TestDepotPickupTransfer(t *testing.T) {
	t.Parallel()
	compact, err := NewClassSet(string(CompactClass))
	if err != nil {
		t.Fatal(err)
	}
	noExit := shortDepotNetwork(2, 2)
	findLane(&noExit, "parking-throat-1").VehicleClasses = compact
	for _, test := range []struct {
		name     string
		network  Network
		party    int
		excluded string
		assigned bool
	}{
		{name: "party", network: shortDepotNetwork(2, 2), party: 2},
		{name: "route", network: noExit, party: 1},
		{name: "exclusion", network: shortDepotNetwork(2, 2), party: 1, excluded: "lane-1-head"},
		{name: "assigned", network: shortDepotNetwork(2, 2), party: 1, assigned: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s := headFleetOn(t, test.network)
			trip := newTrip(s, "harbor", "garden")
			trip.request.PartySize, trip.excludedPod = test.party, test.excluded
			if err := s.sendPickupForRequest(s.findVehicle("market-pod"), trip.request); err != nil {
				t.Fatal(err)
			}
			trip.request.PodID = "market-pod"
			s.waiting = []waitingTrip{trip}
			if test.assigned {
				other := newTrip(s, "market", "garden")
				other.request.PodID = "lane-1-head"
				s.waiting = append(s.waiting, other)
			}
			s.SetPickupSwaps(true)
			if s.tryPickupTransfer(0, s.findVehicle("lane-1-rear")) {
				t.Fatal("the pod behind the lane 1 head takes the pickup")
			}
			if !s.tryPickupTransfer(0, s.findVehicle("lane-2-head")) || s.waiting[0].request.PodID != "lane-2-head" {
				t.Fatalf("the lane 2 head does not take the pickup: %+v", s.waiting[0].request)
			}
		})
	}
}

// TestDepotGuardedCandidates checks that positioning offers only the
// oldest depot head that is a candidate: a head in a rebalance cooldown
// does not hide the head of another lane.
func TestDepotGuardedCandidates(t *testing.T) {
	t.Parallel()
	s := headFleet(t)
	node := func(id string) int { return s.graph.nodes[id] }
	rank, ok := s.guardedCandidates(guardedView{assigned: map[string]bool{}, weights: make([]float64, len(s.network.Stations)), idle: make([]int, len(s.network.Stations))})
	if !ok || rank[node(depotSlotID(1, 1))] < 0 || rank[node(depotSlotID(2, 1))] >= 0 || rank[node(depotSlotID(1, 2))] >= 0 {
		t.Fatalf("candidates %v", rank)
	}
	s.findVehicle("lane-1-head").rebalanceAfter = s.tick + 1
	rank, ok = s.guardedCandidates(guardedView{assigned: map[string]bool{}, weights: make([]float64, len(s.network.Stations)), idle: make([]int, len(s.network.Stations))})
	if !ok || rank[node(depotSlotID(2, 1))] < 0 || rank[node(depotSlotID(1, 2))] >= 0 {
		t.Fatalf("candidates with a cooldown %v", rank)
	}
}

// TestDepotOperationalTarget checks that no operational purpose can name a
// depot station.
func TestDepotOperationalTarget(t *testing.T) {
	t.Parallel()
	s := headFleet(t)
	v := s.findVehicle("market-pod")
	v.withdrawn = faultHold
	err := s.checkOperationalTarget(v, operationalTarget{purpose: opEmptyRecovery, owner: faultHold, station: "parking"})
	if err == nil || err.Error() != "station parking is a depot" {
		t.Fatalf("got %v", err)
	}
	if err := s.checkOperationalTarget(v, operationalTarget{purpose: opEmptyRecovery, owner: faultHold, station: "harbor"}); err != nil {
		t.Fatal(err)
	}
}

// TestDepotBlockedLanes checks the blocked lanes and the faults that change
// a depot lane: a blocked fan lane makes its lane ineligible, a blocked
// lead-out keeps the head of its lane from a pickup, and so does a fault
// of the head itself. The test sets the blocked lanes directly, because
// the footprint of debris on a fan holds the junction at the entry, and
// the footprint of debris on a lead-out holds a slot node.
func TestDepotBlockedLanes(t *testing.T) {
	t.Parallel()
	block := func(s *Simulation, lane string, blocked bool) {
		if s.blocked.lanes == nil {
			s.blocked.lanes = make([]bool, len(s.network.Lanes))
		}
		s.blocked.lanes[s.graph.lanes[lane]] = blocked
	}
	pickup := func(s *Simulation) string {
		trip := newTrip(s, "harbor", "garden")
		if v := s.pickupPodForRequest(trip.request, "", &dispatchPass{assigned: map[string]bool{}}); v != nil {
			return v.Pod.ID
		}
		return ""
	}
	t.Run("fan", func(t *testing.T) {
		t.Parallel()
		s := headFleet(t)
		depot := s.depots.stations["parking"]
		v := s.findVehicle("market-pod")
		if _, slot, ok := s.depotChoice(v, depot); !ok || slot.ID != depotSlotID(2, 2) {
			t.Fatalf("choice %q %v", slot.ID, ok)
		}
		block(s, "parking-fan-2", true)
		if _, _, ok := s.depotChoice(v, depot); ok {
			t.Fatal("a lane with a blocked fan is eligible")
		}
	})
	t.Run("lead-out", func(t *testing.T) {
		t.Parallel()
		s := headFleet(t)
		block(s, "parking-out-1", true)
		if got := pickup(s); got != "lane-2-head" {
			t.Fatalf("pickup with a blocked lead-out %q", got)
		}
		block(s, "parking-out-1", false)
		block(s, "parking-throat-1", true)
		if got := pickup(s); got != "lane-2-head" {
			t.Fatalf("pickup with a blocked throat %q", got)
		}
		block(s, "parking-throat-1", false)
		if got := pickup(s); got != "lane-1-head" {
			t.Fatalf("pickup with open lanes %q", got)
		}
	})
	t.Run("head", func(t *testing.T) {
		t.Parallel()
		s := headFleet(t)
		s.findVehicle("lane-1-head").faulted = true
		if got := pickup(s); got != "lane-2-head" {
			t.Fatalf("pickup with a faulted head %q", got)
		}
	})
}

// TestDepotSlotReservation checks that no empty move can reserve a depot
// slot.
func TestDepotSlotReservation(t *testing.T) {
	t.Parallel()
	network := shortDepotNetwork(1, 2)
	s, err := NewFleet(network, depotPlacements(network, 0))
	if err != nil {
		t.Fatal(err)
	}
	station, _ := s.station("parking")
	slot, _ := station.berth(depotSlotID(1, 1))
	if err := s.startEmptyMove(s.findVehicle("market-pod"), emptyDestination{station: "parking", berth: slot, reserveBerth: true}); !errors.Is(err, ErrBerthUnavailable) {
		t.Fatalf("got %v", err)
	}
}

// TestDepotFaultedArrival checks that a pod that faults or is withdrawn
// when it owns the last cell of the incoming lane does not bind while it
// is out of service, and that it parks after it returns.
func TestDepotFaultedArrival(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"fault", "withdrawal"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			network := shortDepotNetwork(1, 3)
			s, err := NewFleet(network, depotPlacements(network, 0))
			if err != nil {
				t.Fatal(err)
			}
			if err := s.SetFaults(true, FaultSettings{EvacuationSeconds: 300}); err != nil {
				t.Fatal(err)
			}
			v := sendToDepot(t, s, "market-pod")
			last := resource{kind: trackResource, id: "return-to-parking", cell: s.laneCells["return-to-parking"].count() - 1}
			stepDepot(t, s, 120*TicksPerSecond, func() bool { return s.owners[last].isPod(v.Pod.ID) })
			if v.destination.ID != "" {
				t.Fatalf("the pod binds %s first", v.destination.ID)
			}
			if name == "fault" {
				seconds := int64(10)
				if _, err := s.Fault(FaultRequest{PodID: v.Pod.ID, DurationSeconds: &seconds}); err != nil {
					t.Fatal(err)
				}
			} else {
				v.withdrawn = emergencyHold
			}
			returns := s.tick + 10*TicksPerSecond
			stepDepot(t, s, 300*TicksPerSecond, func() bool {
				if _, slot := s.depots.slot(v.destination.ID); slot && (v.faulted || !v.inService()) {
					t.Fatalf("tick %d: the pod binds %s out of service", s.tick, v.destination.ID)
				}
				if name != "fault" && s.tick == returns {
					v.withdrawn = 0
				}
				return v.Pod.Activity == Idle
			})
			if v.Pod.BerthID != depotSlotID(1, 1) {
				t.Fatalf("pod idle at %s %s", v.Pod.StationID, v.Pod.BerthID)
			}
		})
	}
}
