package sim

import (
	"fmt"
	"maps"
	"slices"
	"testing"
)

// scaledBankNetwork is BankExample with each position times scale. The
// entry lanes of the hub banks are then long enough for station buffers.
func scaledBankNetwork(scale float64) Network {
	network := BankExample()
	for index := range network.Nodes {
		network.Nodes[index].Position.X *= scale
		network.Nodes[index].Position.Y *= scale
	}
	return network
}

// bankAccessFleet returns a simulation on scaledBankNetwork(4) with station
// buffers and faults on, under the order contract. Pod 01 is idle at the
// parking berth, and pod 02 at the origin berth. Pod 01 travels to its
// pickup for a waiting trip from the hub to the origin. Express needs the class restrictions of
// expressNetwork and the Express class.
func bankAccessFleet(t *testing.T, contract OrderContract) *Simulation {
	t.Helper()
	network := scaledBankNetwork(4)
	class := VehicleClass("")
	if contract == ExpressOrderContract {
		network, class = expressNetwork(network), ExpressClass
	}
	s, err := NewFleetWithOrderContract(network, []Placement{
		{ID: "01", Class: class, StationID: "parking", BerthID: "parking-1"},
		{ID: "02", Class: class, StationID: "origin", BerthID: "origin-1"},
	}, contract)
	if err != nil {
		t.Fatal(err)
	}
	s.incidentContract = IncidentV1Contract
	s.SetStationBuffers(true)
	if err := s.SetFaults(true, FaultSettings{EvacuationSeconds: 300}); err != nil {
		t.Fatal(err)
	}
	trip := newTrip(s, "hub", "origin")
	trip.deferUntil = 1_000_000
	trip.request.PodID = "01"
	s.waiting = []waitingTrip{trip}
	if err := s.sendPickupForRequest(s.findVehicle("01"), trip.request); err != nil {
		t.Fatal(err)
	}
	return s
}

// TestPickupAccessBankEntry runs, under each order contract, a pickup pod
// that is past the split on its way to the entry of bank a. Debris then
// blocks the only exit road of bank a, so no berth of bank a is a
// compatible pickup berth. The open route to the entry does not keep the
// trip: dispatch unbinds it with no exclusion, and pod 02, which reaches
// bank b, takes it. Pod 01 stays in service and never wins the trip
// again, so the binding changes once. The trip keeps its ID and deferral
// budget, and gets no exclusion.
func TestPickupAccessBankEntry(t *testing.T) {
	t.Parallel()
	for _, contract := range []OrderContract{"", ExpressOrderContract} {
		t.Run(string(contract), func(t *testing.T) {
			t.Parallel()
			s := bankAccessFleet(t, contract)
			first := s.findVehicle("01")
			for first.reservedThrough < 0 || first.blocks.at(first.reservedThrough).lane.ID != "a-approach" {
				if s.tick > 900*TicksPerSecond {
					t.Fatalf("pod 01 is not past the split: %+v", first.Pod)
				}
				s.Step()
			}
			if first.destination.ID != "" || first.Route[len(first.Route)-1].To != "bank-a-entry" || s.waiting[0].request.PodID != "01" {
				t.Fatalf("pod 01 goes to %q on %v for trip %+v", first.destination.ID, first.Route, s.waiting[0].request)
			}
			checkFaultsEachTick(t, s)
			length := s.graph.lengths[laneIndex(t, s, "a-merge")]
			startDebris(t, s, "a-merge", length/2, length/2+10, 0)
			if s.pickupAccess(first, s.waiting[0].request) {
				t.Fatal("pod 01 has access to the pickup")
			}
			id := s.waiting[0].request.ID
			changes, bound := 0, "01"
			for range 60 * TicksPerSecond {
				s.Step()
				if len(s.waiting) == 0 {
					break
				}
				if pod := s.waiting[0].request.PodID; pod != bound {
					changes, bound = changes+1, pod
				}
				if s.waiting[0].request.PodID == "01" {
					t.Fatalf("tick %d: pod 01 has the trip again", s.tick)
				}
			}
			if changes != 1 || bound != "02" {
				t.Fatalf("the binding changed %d times, to %q", changes, bound)
			}
			if trip := s.waiting[0]; trip.request.ID != id || trip.excludedPod != "" || trip.deferUntil != 1_000_000 {
				t.Fatalf("the trip changed: %+v", trip)
			}
			if !first.inService() {
				t.Fatal("pod 01 left service")
			}
		})
	}
}

// accessPickup returns a simulation on altLineFleet(-100) with pod 01 idle
// at the parking berth and pod 02 at s3. Pod 01 travels to its pickup at
// s1 for a trip from s1 to s3, which is the first waiting trip.
func accessPickup(t *testing.T) (*Simulation, *vehicle) {
	t.Helper()
	s := altLineFleet(t, -100, "p-1", "s3-1")
	trip := newTrip(s, "s1", "s3")
	trip.request.PodID = "01"
	s.waiting = []waitingTrip{trip}
	v := s.findVehicle("01")
	if err := s.sendPickupForRequest(v, trip.request); err != nil {
		t.Fatal(err)
	}
	if v.destination.ID != "s1-1" {
		t.Fatalf("pod 01 picks up at %q", v.destination.ID)
	}
	s.waiting[0].route, _ = s.stationApproachRouteForClass(v.destination.Node, "s3", v.Pod.Class)
	return s, v
}

// TestPickupAccessRules checks the rules of pickupAccess with direct calls.
func TestPickupAccessRules(t *testing.T) {
	t.Parallel()
	t.Run("empty blocked set", func(t *testing.T) {
		t.Parallel()
		s, v := accessPickup(t)
		request := s.waiting[0].request
		request.From, request.To = "s3", "unknown"
		if !s.pickupAccess(v, request) {
			t.Fatal("with an empty blocked set, the pod has no access")
		}
	})
	t.Run("idle at the origin", func(t *testing.T) {
		t.Parallel()
		s, _ := accessPickup(t)
		// The only road to s0 is blocked, so the onward leg has no route.
		length := s.graph.lengths[laneIndex(t, s, "return")]
		startDebris(t, s, "return", length/2, length/2+10, 0)
		if !s.pickupAccess(s.findVehicle("02"), newTrip(s, "s3", "s0").request) {
			t.Fatal("the pod idle at the origin has no access")
		}
	})
	t.Run("blocked onward exit", func(t *testing.T) {
		t.Parallel()
		s, v := accessPickup(t)
		startDebris(t, s, "s1-1-out", 40, 44, 0)
		if s.pickupAccess(v, s.waiting[0].request) {
			t.Fatal("the pod has access through a berth with a blocked exit")
		}
	})
	t.Run("occupied compatible berth", func(t *testing.T) {
		t.Parallel()
		s, v := accessPickup(t)
		length := s.graph.lengths[laneIndex(t, s, "return")]
		startDebris(t, s, "return", length/2, length/2+10, 0)
		s.owners[resource{kind: berthResource, id: "s1-1"}] = podResourceOwner("02")
		if !s.pickupAccess(v, s.waiting[0].request) {
			t.Fatal("an occupied compatible berth refuses access")
		}
	})
	t.Run("open route in the arrival chain", func(t *testing.T) {
		t.Parallel()
		s, v := accessPickup(t)
		stepUntil(t, s, "pod 01 holds its inlet", func() bool {
			return v.reservedThrough >= 0 && v.blocks.at(v.reservedThrough).lane.ID == "s1-1-in"
		})
		length := s.graph.lengths[laneIndex(t, s, "return")]
		startDebris(t, s, "return", length/2, length/2+10, 0)
		if _, _, ok := s.divertStart(v); ok {
			t.Fatal("the pod can divert")
		}
		if !s.pickupAccess(v, s.waiting[0].request) {
			t.Fatal("an open route in the arrival chain has no access")
		}
	})
	t.Run("blocked kept lane", func(t *testing.T) {
		t.Parallel()
		s, v := accessPickup(t)
		stepUntil(t, s, "pod 01 holds the start of s0-link", func() bool {
			if v.Pod.Activity != Traveling || v.reservedThrough < 0 {
				return false
			}
			committed := v.blocks.at(v.reservedThrough)
			return committed.lane.ID == "s0-link" && v.blocks.end(v.reservedThrough)-committed.laneStart <= 60
		})
		startDebris(t, s, "s0-link", 95, 100, 0)
		if s.pickupAccess(v, s.waiting[0].request) {
			t.Fatal("a trapped pod has access")
		}
	})
	t.Run("leg origin", func(t *testing.T) {
		t.Parallel()
		s, v := accessPickup(t)
		// The order origin s0 is cut off. The leg origin s1 is not.
		length := s.graph.lengths[laneIndex(t, s, "return")]
		startDebris(t, s, "return", length/2, length/2+10, 0)
		request := s.waiting[0].request
		request.From, request.LegFrom, request.To = "s0", "s1", "s3"
		cruiseOn(t, s, v, "p-link")
		if v.distance == 0 || v.reservedThrough < 0 || !s.pickupAccess(v, request) {
			t.Fatal("the pod has no access to the leg origin")
		}
	})
	t.Run("end berth of a busy pod", func(t *testing.T) {
		t.Parallel()
		// Pod 01 rides to s1 and chooses s1-1. Debris on the exit of s1-1
		// cuts the end berth off. s1-2 could reach the pickup, but the pod
		// becomes available at s1-1.
		s := altLineFleet(t, -100, "s0-1", "s3-1")
		v := s.findVehicle("01")
		if err := s.RequestJourney("01", "s1"); err != nil {
			t.Fatal(err)
		}
		stepUntil(t, s, "the terminal berth choice", func() bool { return v.destination.ID != "" })
		if v.destination.ID != "s1-1" {
			t.Fatalf("pod 01 chose %s", v.destination.ID)
		}
		request := newTrip(s, "s2", "s3").request
		if !s.pickupAccess(v, request) {
			t.Fatal("before the debris, the busy pod has no access")
		}
		startDebris(t, s, "s1-1-out", 40, 44, 0)
		if s.pickupAccess(v, request) {
			t.Fatal("the busy pod has access from a berth with a blocked exit")
		}
	})
}

// legPickup returns a simulation on altLineFleet(-100) with pod 01 idle at
// the parking berth, pod 02 at s3, and pod 03 at s2-1. Pod 01 travels to
// its pickup at s0 for a trip from s0 to s2.
func legPickup(t *testing.T) (*Simulation, *vehicle) {
	t.Helper()
	s := altLineFleet(t, -100, "p-1", "s3-1", "s2-1")
	trip := newTrip(s, "s0", "s2")
	trip.request.PodID = "01"
	s.waiting = []waitingTrip{trip}
	v := s.findVehicle("01")
	if err := s.sendPickupForRequest(v, trip.request); err != nil {
		t.Fatal(err)
	}
	s.waiting[0].route, _ = s.stationApproachRouteForClass(v.destination.Node, "s2", v.Pod.Class)
	return s, v
}

// TestPickupAccessContinuation checks the continuation of rule 4 for a pod
// with an assigned passenger leg from s0 to s2, for a new pickup at s3.
// With an empty blocked set, the leg ends at the node of finishEstimate.
// When the leg route is blocked, the cached trip route does not give
// access. When the first berth of s2 is blocked, the leg ends at the
// other berth, which gives access.
func TestPickupAccessContinuation(t *testing.T) {
	t.Parallel()
	request := func(s *Simulation) Request { return newTrip(s, "s3", "s0").request }
	t.Run("empty blocked set", func(t *testing.T) {
		t.Parallel()
		s, v := legPickup(t)
		node, ok := s.continuationNode(v)
		want, _, wantOK := s.finishEstimate(v)
		if node != want || ok != wantOK || !ok {
			t.Fatalf("the continuation ends at %q, %v, want %q, %v", node, ok, want, wantOK)
		}
	})
	t.Run("blocked leg", func(t *testing.T) {
		t.Parallel()
		s, v := legPickup(t)
		startDebris(t, s, "s0-link", 70, 80, 0)
		if !usesLane(s.waiting[0].route, "s0-link") {
			t.Fatalf("the cached leg route %v does not use s0-link", s.waiting[0].route)
		}
		if s.pickupAccess(v, request(s)) {
			t.Fatal("the pod has access after a blocked leg")
		}
	})
	t.Run("blocked first berth", func(t *testing.T) {
		t.Parallel()
		s, v := legPickup(t)
		startFault(t, s, s.findVehicle("03"), 0)
		node, ok := s.continuationNode(v)
		if !ok || node != "s2-2" || !s.pickupAccess(v, request(s)) {
			t.Fatalf("the continuation ends at %q, %v", node, ok)
		}
	})
}

// TestPickupAccessUnbinding traps pickup pod 01 behind debris on s0-link.
// Dispatch unbinds the trip, and no other pod can take it. The trip keeps
// its ID, its place, its deferral budget and its exclusion, and it loses
// its route and berth under each order contract. Pod 01 stays in service,
// and the same pass does not bind the trip to it again.
func TestPickupAccessUnbinding(t *testing.T) {
	t.Parallel()
	s, v := accessPickup(t)
	other := newTrip(s, "s3", "s0")
	s.waiting = append(s.waiting, other)
	s.waiting[0].excludedPod, s.waiting[0].deferUntil = "02", 1_000_000
	stepUntil(t, s, "pod 01 holds the start of s0-link", func() bool {
		if v.Pod.Activity != Traveling || v.reservedThrough < 0 {
			return false
		}
		committed := v.blocks.at(v.reservedThrough)
		return committed.lane.ID == "s0-link" && v.blocks.end(v.reservedThrough)-committed.laneStart <= 60
	})
	if s.waiting[0].request.PodID != "01" || s.waiting[0].route == nil {
		t.Fatalf("the trip lost pod 01 or its route: %+v", s.waiting[0])
	}
	checkFaultsEachTick(t, s)
	startDebris(t, s, "s0-link", 95, 100, 0)
	id := s.waiting[0].request.ID
	s.dispatch()
	trip := s.waiting[0]
	switch {
	case trip.request.ID != id || trip.request.PodID != "":
		t.Fatalf("the first trip is %+v", trip.request)
	case trip.route != nil || trip.destination != (Berth{}):
		t.Fatalf("the unbound trip keeps the route %v and the berth %q", trip.route, trip.destination.ID)
	case trip.excludedPod != "02" || trip.deferUntil != 1_000_000:
		t.Fatalf("the unbound trip has the exclusion %q and the deferral %d", trip.excludedPod, trip.deferUntil)
	case !v.inService() || !v.released:
		t.Fatalf("pod 01 has the holds %d and released %t", v.withdrawn, v.released)
	}
}

// TestPickupAccessBlockedPickupBerth blocks the pickup berth of pod 01.
// Dispatch unbinds the trip and sends the same pod to the other berth of
// the station, which is a compatible pickup berth.
func TestPickupAccessBlockedPickupBerth(t *testing.T) {
	t.Parallel()
	s, v := accessPickup(t)
	advance(s, TicksPerSecond)
	claims := berthResources(v.destination)
	s.setBlocked([]faultFootprint{{id: "i1.1", resources: claims[:]}})
	s.dispatch()
	if s.waiting[0].request.PodID != "01" || v.destination.ID != "s1-2" || s.waiting[0].excludedPod != "" {
		t.Fatalf("the trip %+v, pod 01 goes to %q", s.waiting[0], v.destination.ID)
	}
}

// TestPickupInstallationNeedsCompatibleBerth checks that the pickup
// installation takes a compatible pickup berth. Debris on the exit of s1-1
// cuts the onward route from that berth, so pod 01 goes to s1-2.
func TestPickupInstallationNeedsCompatibleBerth(t *testing.T) {
	t.Parallel()
	s := altLineFleet(t, -100, "p-1", "s3-1")
	startDebris(t, s, "s1-1-out", 40, 44, 0)
	trip := newTrip(s, "s1", "s3")
	trip.request.PodID = "01"
	s.waiting = []waitingTrip{trip}
	v := s.findVehicle("01")
	if err := s.sendPickupForRequest(v, trip.request); err != nil {
		t.Fatal(err)
	}
	if v.destination.ID != "s1-2" {
		t.Fatalf("pod 01 picks up at %q", v.destination.ID)
	}
}

// TestPickupAccessLocalPod checks the exception of rule 2 through dispatch.
// Debris cuts both roads out of s1, where pod 01 is idle. Dispatch binds
// a new trip from s1 to s3 to pod 01, which boards there, and the trip
// waits for destination access. Pod 02 at s3 has no compatible pickup
// berth at s1, so it does not take the trip.
func TestPickupAccessLocalPod(t *testing.T) {
	t.Parallel()
	s := altLineFleet(t, -100, "s1-1", "s3-1")
	startDebris(t, s, "s1-link", 70, 80, 0)
	startDebris(t, s, "alt-out", 70, 80, 0)
	if err := s.RequestTrip("s1", "s3"); err != nil {
		t.Fatal(err)
	}
	v := s.findVehicle("01")
	for range 5 * TicksPerSecond {
		s.Step()
		if len(s.waiting) != 1 {
			t.Fatalf("tick %d: %d waiting trips", s.tick, len(s.waiting))
		}
		if trip := s.waiting[0].request; trip.PodID != "01" || trip.DispatchReason != "Waiting for destination access" {
			t.Fatalf("tick %d: the trip has pod %q and the reason %q", s.tick, trip.PodID, trip.DispatchReason)
		}
		if v.Pod.Activity != Idle || v.Pod.StationID != "s1" {
			t.Fatalf("tick %d: pod 01 is %s at %q", s.tick, v.Pod.Activity, v.Pod.StationID)
		}
	}
}

// TestPickupPromotionNeedsCompatibleBerth checks assignedPickupFitsRequest
// in the promotion of a ready pickup pod. Pod 01 travels to s1-1 for the
// older trip, and pod 02 is idle at s1-2 for a later trip from s1. The
// promotion would give the later trip to pod 01. A blocked set that holds
// only the berth s1-1 leaves its exit open, so the berth passes
// pickupBerthFitsRequest but is not a compatible pickup berth, and the
// promotion does not run. With an empty blocked set, it runs.
func TestPickupPromotionNeedsCompatibleBerth(t *testing.T) {
	t.Parallel()
	for _, blocked := range []bool{false, true} {
		s := altLineFleet(t, -100, "p-1", "s1-2")
		first, later := newTrip(s, "s1", "s3"), newTrip(s, "s1", "s3")
		first.request.PodID, later.request.PodID = "01", "02"
		s.waiting = []waitingTrip{first, later}
		v := s.findVehicle("01")
		if err := s.sendPickupForRequest(v, first.request); err != nil || v.destination.ID != "s1-1" {
			t.Fatalf("pod 01 picks up at %q: %v", v.destination.ID, err)
		}
		if blocked {
			claims := berthResources(v.destination)
			s.setBlocked([]faultFootprint{{id: "i1.1", resources: claims[:1]}})
			if !s.pickupBerthFitsRequest(v, later.request, v.destination) {
				t.Fatal("the blocked berth fails pickupBerthFitsRequest")
			}
		}
		if promoted := s.promoteReadyPickup(0); promoted == blocked {
			t.Fatalf("blocked %v: promoted %v", blocked, promoted)
		}
	}
}

// TestPickupHoldNeedsAccess checks the finishing-pod hold. Pod 01 rides to
// s2 and finishes before pod 02 at the parking berth can reach s3, so a
// trip from s3 waits for pod 01. Debris beyond the grants of pod 01 traps
// it, so it has no access, and the trip waits for no pod.
func TestPickupHoldNeedsAccess(t *testing.T) {
	t.Parallel()
	s := altLineFleet(t, -100, "s0-1", "p-1")
	busy, idle := s.findVehicle("01"), s.findVehicle("02")
	if err := s.RequestJourney("01", "s2"); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, s, "pod 01 holds the start of s1-link", func() bool {
		if busy.Pod.Activity != Traveling || busy.reservedThrough < 0 {
			return false
		}
		committed := busy.blocks.at(busy.reservedThrough)
		return committed.lane.ID == "s1-link" && busy.blocks.end(busy.reservedThrough)-committed.laneStart <= 60
	})
	trip := newTrip(s, "s3", "s0")
	if !s.waitForFinishingPod(&trip, idle, map[string]bool{}) || trip.deferPodID != "01" {
		t.Fatalf("the trip does not wait for pod 01: %+v", trip)
	}
	startDebris(t, s, "s1-link", 95, 100, 0)
	if s.keepHold(&trip, new(dispatchPass)) {
		t.Fatal("the hold keeps a pod with no access")
	}
	if s.waitForFinishingPod(&trip, idle, map[string]bool{}) {
		t.Fatalf("the trip waits for a pod with no access: %+v", trip)
	}
	fresh := newTrip(s, "s3", "s0")
	if s.waitForFinishingPod(&fresh, idle, map[string]bool{}) {
		t.Fatalf("a new trip waits for a pod with no access: %+v", fresh)
	}
}

// TestPickupSwapNeedsAccess checks the swap and transfer gates. In the
// crossed fixture, pod 01 goes to s3 for a trip to s0, and pod 02 to s1.
// Debris on the return road cuts the leg of pod 01 off, so pod 01 has no
// access to the pickup at s1 after its leg, and the swap does not run.
// Debris on the exit of the pickup berth of pod 01 also cuts that leg,
// while each pod has a compatible berth for the other pickup. In
// the transfer fixture, debris on the inlet of the parking berth of pod 02
// makes its route not executable, so it does not take the trip.
func TestPickupSwapNeedsAccess(t *testing.T) {
	t.Parallel()
	t.Run("swap", func(t *testing.T) {
		t.Parallel()
		s := crossedPickupFixture(t)
		s.faultsOn = true
		length := s.graph.lengths[laneIndex(t, s, "return")]
		startDebris(t, s, "return", length/2, length/2+10, 0)
		s.SetPickupSwaps(true)
		s.swapPickups()
		if s.PickupSwapStats().Swaps != 0 || s.waiting[0].request.PodID != "01" {
			t.Fatalf("the swap ran: %+v", s.PickupSwapStats())
		}
	})
	t.Run("swap after the given-away leg", func(t *testing.T) {
		t.Parallel()
		s := crossedPickupFixture(t)
		s.faultsOn = true
		a, b := s.findVehicle("01"), s.findVehicle("02")
		startDebris(t, s, a.destination.ID+"-out", 40, 44, 0)
		// Both pods have a compatible berth for the other pickup, but
		// pod 01 has no access to the pickup of pod 02 after its own leg.
		if _, _, ok := s.candidateRouteForRequest(a, s.waiting[1].request, nil); !ok {
			t.Fatal("pod 01 has no candidate route to the pickup of pod 02")
		}
		if _, _, ok := s.candidateRouteForRequest(b, s.waiting[0].request, nil); !ok {
			t.Fatal("pod 02 has no candidate route to the pickup of pod 01")
		}
		if s.pickupAccess(a, s.waiting[1].request) {
			t.Fatal("pod 01 has access to the pickup of pod 02")
		}
		s.SetPickupSwaps(true)
		s.swapPickups()
		if s.PickupSwapStats().Swaps != 0 || s.waiting[0].request.PodID != "01" {
			t.Fatalf("the swap ran: %+v", s.PickupSwapStats())
		}
	})
	t.Run("transfer", func(t *testing.T) {
		t.Parallel()
		s := pickupTransferFixture(t, "parking")
		s.faultsOn = true
		v := s.findVehicle("02")
		startDebris(t, s, v.destination.ID+"-in", 40, 44, 0)
		s.SetPickupSwaps(true)
		s.swapPickups()
		if s.PickupSwapStats().Transfers != 0 || s.waiting[0].request.PodID != "01" {
			t.Fatalf("the transfer ran: %+v", s.PickupSwapStats())
		}
	})
}

// TestPickupBerthFilter checks the filter of pickup berths. With an empty
// blocked set, it is the stop filter. While the blocked set is not empty,
// it refuses a blocked berth and a berth with no onward route, also on a
// network without class restrictions.
func TestPickupBerthFilter(t *testing.T) {
	t.Parallel()
	s, v := accessPickup(t)
	request := s.waiting[0].request
	if s.pickupBerthFilter(v, request) != nil {
		t.Fatal("with an empty blocked set, the filter is not the stop filter")
	}
	s1, _ := s.station("s1")
	startDebris(t, s, "s1-1-out", 40, 44, 0)
	filter := s.pickupBerthFilter(v, request)
	if filter == nil || filter(s1.Berths[0]) || !filter(s1.Berths[1]) {
		t.Fatal("the filter accepts a berth with a blocked exit, or refuses a free berth")
	}
	claims := berthResources(s1.Berths[1])
	s.setBlocked(append(s.faultFootprints(), faultFootprint{id: "i9.9", resources: claims[:]}))
	if filter := s.pickupBerthFilter(v, request); filter(s1.Berths[1]) {
		t.Fatal("the filter accepts a blocked berth")
	}
	if got := s.berthFilterForVehicle(v); got == nil || got(s1.Berths[1]) || got(s1.Berths[0]) {
		t.Fatal("the pod filter is not the pickup filter")
	}
	if !slices.ContainsFunc(s1.Berths, func(b Berth) bool { return b.ID == v.destination.ID }) {
		t.Fatal("the fixture changed")
	}
}

// routingPolicies are the four routing policies.
var routingPolicies = []RoutingPolicy{FreeFlowRouting, CongestionRouting, QueueRouting, PredictiveRouting}

// TestIncidentQueriesWriteNothing checks that endpointRoute and
// pickupAccess write nothing (section 11 of the incident suspension
// contract), when they succeed and when they fail, under each routing
// policy. They write no routing-policy state and no route memo, also when
// a policy search comes before a detour refusal.
func TestIncidentQueriesWriteNothing(t *testing.T) {
	t.Parallel()
	unchanged := func(t *testing.T, s *Simulation, query func() bool, want bool) {
		t.Helper()
		before, memo := s.Clone(), slices.Collect(maps.Keys(s.routes))
		if got := query(); got != want {
			t.Fatalf("the query reports %v, want %v", got, want)
		}
		if !sameState(before, s) {
			t.Fatal("the query wrote state")
		}
		if len(s.routes) != len(memo) || slices.ContainsFunc(memo, func(key routeKey) bool { _, ok := s.routes[key]; return !ok }) {
			t.Fatalf("the query wrote the route memo: %d entries, want %d", len(s.routes), len(memo))
		}
	}
	setPolicy := func(t *testing.T, s *Simulation, policy RoutingPolicy) {
		t.Helper()
		if err := s.SetRoutingPolicy(policy); err != nil {
			t.Fatal(err)
		}
	}
	for _, policy := range routingPolicies {
		t.Run(fmt.Sprintf("policy %d", policy), func(t *testing.T) {
			t.Parallel()
			t.Run("endpoint route", func(t *testing.T) {
				t.Parallel()
				s := altLineFleet(t, -100, "s0-1", "s3-1")
				v := s.findVehicle("01")
				if err := s.RequestJourney("01", "s2"); err != nil {
					t.Fatal(err)
				}
				// The policy starts with no state, so a query that
				// updated it would write it.
				setPolicy(t, s, policy)
				var route []Lane
				query := func() bool {
					var ok bool
					route, ok = s.endpointRoute(v)
					return ok
				}
				startDebris(t, s, "s1-link", 70, 80, 0)
				unchanged(t, s, query, true)
				// The installation reads the same view, so it installs
				// the route of the query.
				if !s.rerouteToEndpoint(v) || !sameLanes(v.Route, route) {
					t.Fatalf("the installed route %v differs from %v", v.Route, route)
				}
				startDebris(t, s, "alt-in", 70, 80, 0)
				unchanged(t, s, query, false)
			})
			t.Run("detour refusal", func(t *testing.T) {
				t.Parallel()
				s, v := asymmetricBankFleet(t)
				if err := s.RequestJourney("01", "hub"); err != nil {
					t.Fatal(err)
				}
				setPolicy(t, s, policy)
				startDebris(t, s, "a-approach", 60, 70, 0)
				unchanged(t, s, func() bool { _, ok := s.endpointRoute(v); return ok }, false)
				unchanged(t, s, func() bool { return s.rerouteToEndpoint(v) }, false)
			})
			t.Run("pickup access", func(t *testing.T) {
				t.Parallel()
				s, v := legPickup(t)
				setPolicy(t, s, policy)
				request := newTrip(s, "s3", "s0").request
				access := func() bool { return s.pickupAccess(v, request) }
				startFault(t, s, s.findVehicle("03"), 0)
				unchanged(t, s, access, true)
				startDebris(t, s, "s0-link", 70, 80, 0)
				unchanged(t, s, access, false)
			})
		})
	}
}
