package sim

import (
	"maps"
	"math"
	"reflect"
	"slices"
	"testing"
)

// altLineFleet returns a simulation with faults on, the incident marker, a
// party limit of 4 and drop-offs, on lineNetwork(lineStations(1, 1, 2, 2,
// 1)) with a second road from the exit of s1 to the entry of s2. The road
// passes the node alt, at altY meters from the line. The direct lane
// s1-link is 150 m long. A road at altY -100 is 250 m long, so it keeps a
// ride from s0 to s2 within the detour limit. A road at altY -400 is 814 m
// long, and it does not.
func altLineFleet(t *testing.T, altY float64, berths ...string) *Simulation {
	t.Helper()
	return altLineFleetClasses(t, altY, 0, berths...)
}

// altLineFleetClasses is altLineFleet with the vehicle classes of the lane
// alt-out. A nonzero set turns on the class restrictions of the network.
func altLineFleetClasses(t *testing.T, altY float64, classes ClassSet, berths ...string) *Simulation {
	t.Helper()
	network := lineNetwork(lineStations(1, 1, 2, 2, 1))
	network.Nodes = append(network.Nodes, Node{ID: "alt", Position: Point{X: 825, Y: altY}})
	network.Lanes = append(network.Lanes,
		Lane{ID: "alt-out", From: "s1-exit", To: "alt", SpeedLimit: 14, VehicleClasses: classes},
		Lane{ID: "alt-in", From: "alt", To: "s2-entry", SpeedLimit: 14},
	)
	s, err := NewFleet(network, place(berths...))
	if err != nil {
		t.Fatal(err)
	}
	s.incidentContract = IncidentV1Contract
	if err := s.SetSharedRidePartyLimit(4); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSharedRideMode(SharedRideDropOffs, DefaultSharedRideMaxStops); err != nil {
		t.Fatal(err)
	}
	if err := s.SetFaults(true, FaultSettings{EvacuationSeconds: 300}); err != nil {
		t.Fatal(err)
	}
	return s
}

// sameLanes reports whether two routes have the same lanes.
func sameLanes(a, b []Lane) bool {
	return slices.EqualFunc(a, b, func(x, y Lane) bool { return x.ID == y.ID })
}

// withoutRoute returns a copy of v without the route and the fields that
// each write of the route writes. The copy has its own retained resources
// and riders.
func withoutRoute(v *vehicle) vehicle {
	c := *v
	c.Route, c.routeVersion, c.stationPhase, c.blockStarts = nil, 0, stationPhaseCheck{}, nil
	c.terminal, c.routeLengths, c.blocks, c.pending = terminalCheck{}, nil, blockList{}, 0
	c.routeReleases = maps.Clone(v.routeReleases)
	c.Riders, c.Stops, c.Boardings = slices.Clone(v.Riders), slices.Clone(v.Stops), slices.Clone(v.Boardings)
	return c
}

// checkEndpointReroute runs the reroute pass and checks that it gives v a
// new route that keeps the kept lanes and the endpoint, and avoids the
// blocked set. Only the route, its derived fields and pending change. The
// resource owners and the waiting trips stay.
func checkEndpointReroute(t *testing.T, s *Simulation, v *vehicle, kept int) {
	t.Helper()
	before, route := withoutRoute(v), slices.Clone(v.Route)
	owners, waiting := maps.Clone(s.owners), slices.Clone(s.waiting)
	reroutes := s.faultCounters.reroutes
	s.rerouteDue = true
	s.reroutePass()
	if s.faultCounters.reroutes != reroutes+1 || sameLanes(v.Route, route) {
		t.Fatalf("pod %s is not rerouted: %d reroutes, route %v", v.Pod.ID, s.faultCounters.reroutes-reroutes, v.Route)
	}
	if !sameLanes(v.Route[:kept], route[:kept]) || v.Route[len(v.Route)-1].To != route[len(route)-1].To {
		t.Fatalf("the new route %v does not keep the %d lanes and the end of %v", v.Route, kept, route)
	}
	if s.routeBlocked(v.Route) || v.pending != -1 {
		t.Fatalf("the new route %v is blocked, or pending is %d", v.Route, v.pending)
	}
	if after := withoutRoute(v); !reflect.DeepEqual(after, before) {
		t.Fatalf("the reroute changed more than the route:\n%+v\n%+v", after, before)
	}
	if !maps.Equal(s.owners, owners) || !reflect.DeepEqual(s.waiting, waiting) {
		t.Fatal("the reroute changed the owners or the waiting trips")
	}
}

// TestEndpointRerouteOfBoardingPod blocks s1-link while pod 01 boards at
// s0 for s2. The reroute pass gives the pod a route to the entry of s2 on
// the other road. The pod keeps its riders and stops, and it unloads them
// at s2. It does not become an empty move.
func TestEndpointRerouteOfBoardingPod(t *testing.T) {
	t.Parallel()
	s := altLineFleet(t, -100, "s0-1", "s3-1")
	checkFaultsEachTick(t, s)
	v := s.findVehicle("01")
	if err := s.RequestJourney("01", "s2"); err != nil {
		t.Fatal(err)
	}
	if v.Pod.Activity != Boarding || v.reservedThrough >= 0 || !usesLane(v.Route, "s1-link") {
		t.Fatalf("pod 01 is %s with grants to %d on %v", v.Pod.Activity, v.reservedThrough, v.Route)
	}
	startDebris(t, s, "s1-link", 70, 80, 0)
	if !s.rerouteCandidate(v) {
		t.Fatal("the boarding pod is not a reroute candidate")
	}
	checkEndpointReroute(t, s, v, 0)
	if !usesLane(v.Route, "alt-in") || v.Route[len(v.Route)-1].To != "s2-entry" {
		t.Fatalf("the new route is %v", v.Route)
	}
	stepUntil(t, s, "the unload at s2", func() bool { return v.Pod.Activity == Unloading })
	if v.Pod.StationID != "s2" || v.RelocatingTo != "" || v.RidersAboard() != 1 {
		t.Fatalf("pod 01 unloads at %s with %d riders, relocating to %q", v.Pod.StationID, v.RidersAboard(), v.RelocatingTo)
	}
	stepUntil(t, s, "the rider completes", func() bool { return s.completed == 1 })
}

// TestEndpointRerouteOfContinuingPod blocks s1-link when pod 01 continues
// at s1 to s2, before it has a grant. The reroute pass gives it the other
// road.
func TestEndpointRerouteOfContinuingPod(t *testing.T) {
	t.Parallel()
	s := altLineFleet(t, -100, "s0-1", "s3-1")
	v := boardParties(t, s, "s1", "s2")
	stepUntil(t, s, "the unload at s1", func() bool { return v.Pod.Activity == Unloading })
	s.alight(v)
	s.continueJourney(v)
	if v.Pod.Activity != Continuing || v.reservedThrough >= 0 || !usesLane(v.Route, "s1-link") {
		t.Fatalf("pod 01 is %s with grants to %d on %v", v.Pod.Activity, v.reservedThrough, v.Route)
	}
	checkFaultsEachTick(t, s)
	startDebris(t, s, "s1-link", 70, 80, 0)
	checkEndpointReroute(t, s, v, 0)
	stepUntil(t, s, "the riders complete", func() bool { return s.completed == 2 })
}

// TestEndpointRerouteOfTravelingPod blocks s1-link ahead of pod 01, which
// travels on s0-link to s2. The pod keeps the lanes of its grants and
// takes the other road. The state at the end of the tick of the reroute
// saves in the physical format (section 16.5 of the incident suspension
// contract).
func TestEndpointRerouteOfTravelingPod(t *testing.T) {
	t.Parallel()
	s := altLineFleet(t, -100, "s0-1", "s3-1")
	checkFaultsEachTick(t, s)
	v := s.findVehicle("01")
	if err := s.RequestJourney("01", "s2"); err != nil {
		t.Fatal(err)
	}
	cruiseOn(t, s, v, "s0-link")
	startDebris(t, s, "s1-link", 70, 80, 0)
	prefix, _, ok := s.divertStart(v)
	if !ok || prefix == 0 {
		t.Fatalf("pod 01 cannot divert: prefix %d, %v", prefix, ok)
	}
	checkEndpointReroute(t, s, v, prefix)
	physicalSave(t, s, "end of a tick with an endpoint reroute")
	stepUntil(t, s, "the rider completes", func() bool { return s.completed == 1 })
}

// TestEndpointRerouteOfEmptyMove blocks s1-link while pod 02 departs empty
// from the parking berth to the berth s2-2. The new route ends at the same
// berth, and the pod becomes idle there.
func TestEndpointRerouteOfEmptyMove(t *testing.T) {
	t.Parallel()
	s := altLineFleet(t, -100, "p-1", "s3-1")
	checkFaultsEachTick(t, s)
	v := s.findVehicle("01")
	s2, _ := s.station("s2")
	berth, _ := s2.berth("s2-2")
	if err := s.startEmptyMove(v, emptyDestination{station: "s2", berth: berth, reserveBerth: true}); err != nil {
		t.Fatal(err)
	}
	if v.Pod.Activity != DepartingEmpty {
		t.Fatalf("pod 01 is %s", v.Pod.Activity)
	}
	startDebris(t, s, "s1-link", 70, 80, 0)
	checkEndpointReroute(t, s, v, 0)
	if v.Route[len(v.Route)-1].To != berth.Node || v.destination != berth {
		t.Fatalf("the new route %v ends away from the berth %s", v.Route, v.destination.ID)
	}
	stepUntil(t, s, "pod 01 is idle at s2-2", func() bool { return v.Pod.Activity == Idle && v.Pod.BerthID == "s2-2" })
}

// TestEndpointRerouteOverDetourLimit blocks s1-link while pod 01 boards
// for s2, and the other road takes the rider over the detour limit. The pod
// keeps its route and waits at the debris, blocked by the incident.
func TestEndpointRerouteOverDetourLimit(t *testing.T) {
	t.Parallel()
	s := altLineFleet(t, -400, "s0-1", "s3-1")
	checkFaultsEachTick(t, s)
	v := s.findVehicle("01")
	if err := s.RequestJourney("01", "s2"); err != nil {
		t.Fatal(err)
	}
	route := slices.Clone(v.Route)
	id := startDebris(t, s, "s1-link", 70, 80, 0)
	stepUntil(t, s, "pod 01 waits at the debris", func() bool { return v.Pod.Speed == 0 && v.Pod.WaitReason == blockedByIncident })
	// The terminal berth choice adds the inlet to the route.
	if s.faultCounters.reroutes != 0 || !sameLanes(v.Route[:len(route)], route) || v.Pod.BlockedBy != id {
		t.Fatalf("%d reroutes, route %v, blocked by %q", s.faultCounters.reroutes, v.Route, v.Pod.BlockedBy)
	}
}

// asymmetricBankFleet returns a simulation on BankExample with faults on,
// the incident marker, a party limit of 4 and drop-offs. Pod 01 is idle at
// the origin. The road to bank b has a bend of 480 m, so a pod from the
// origin goes to bank a. A detour of 750 m from the split to the approach
// of bank a goes around the approach lane a-approach. On the detour, a
// rider to the hub is over the detour limit at bank a, but within it at
// bank b, which is farther from the origin.
func asymmetricBankFleet(t *testing.T) (*Simulation, *vehicle) {
	t.Helper()
	network := BankExample()
	for index := range network.Lanes {
		if network.Lanes[index].ID == "b-approach" {
			network.Lanes[index].From = "b-bend"
		}
	}
	network.Nodes = append(network.Nodes, Node{ID: "a-detour", Position: Point{X: 360, Y: -310}}, Node{ID: "b-bend", Position: Point{X: 300, Y: 400}})
	network.Lanes = append(network.Lanes,
		Lane{ID: "b-bend", From: "split", To: "b-bend", SpeedLimit: 14},
		Lane{ID: "a-detour-out", From: "split", To: "a-detour", SpeedLimit: 14},
		Lane{ID: "a-detour-in", From: "a-detour", To: "a-approach", SpeedLimit: 14},
	)
	s, err := NewFleet(network, []Placement{{ID: "01", StationID: "origin", BerthID: "origin-1"}})
	if err != nil {
		t.Fatal(err)
	}
	s.incidentContract = IncidentV1Contract
	if err := s.SetSharedRidePartyLimit(4); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSharedRideMode(SharedRideDropOffs, DefaultSharedRideMaxStops); err != nil {
		t.Fatal(err)
	}
	if err := s.SetFaults(true, FaultSettings{EvacuationSeconds: 300}); err != nil {
		t.Fatal(err)
	}
	v := s.findVehicle("01")
	return s, v
}

// TestEndpointRerouteBankEntryDetour blocks a-approach while pod 01 boards
// for the hub with its route to the entry of bank a. The detour keeps the
// entry of bank a, where the rider is over the detour limit, so the pod
// keeps its route. The same detour passes the check at bank b, which the
// default bank choice gives, so the test catches a check that skips the
// entry of the route or does not run.
func TestEndpointRerouteBankEntryDetour(t *testing.T) {
	t.Parallel()
	s, v := asymmetricBankFleet(t)
	if err := s.RequestJourney("01", "hub"); err != nil {
		t.Fatal(err)
	}
	if v.Route[len(v.Route)-1].To != "bank-a-entry" || v.destination.ID != "" {
		t.Fatalf("pod 01 goes to %v, berth %q", v.Route, v.destination.ID)
	}
	startDebris(t, s, "a-approach", 60, 70, 0)
	detour, err := s.assignedRoute(v, v.origin.Node, "bank-a-entry")
	if err != nil || !usesLane(detour, "a-detour-in") {
		t.Fatalf("the detour is %v: %v", detour, err)
	}
	for entry, want := range map[string]bool{"bank-a-entry": false, "bank-b-entry": true, "": true} {
		start := detourStart{class: v.Pod.Class, from: v.origin.Node, ridden: v.riddenBase + s.lanesMeters(detour), entry: entry}
		if got := s.keepsRiderDetours(v, v.Stops, start); got != want {
			t.Fatalf("entry %q: the detour keeps the rider within the limit: %v", entry, got)
		}
	}
	if !s.rerouteCandidate(v) {
		t.Fatal("pod 01 is not a reroute candidate")
	}
	if route, ok := s.endpointRoute(v); ok {
		t.Fatalf("pod 01 has the endpoint route %v", route)
	}
}

// TestEndpointRerouteOfTrappedPod blocks s1-link beyond the grants of pod
// 01, which already holds the start of s1-link on its way to s3. A kept
// lane is blocked, so the pod keeps its route and waits.
func TestEndpointRerouteOfTrappedPod(t *testing.T) {
	t.Parallel()
	s := altLineFleet(t, -100, "s0-1", "p-1")
	checkFaultsEachTick(t, s)
	v := s.findVehicle("01")
	if err := s.RequestJourney("01", "s3"); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, s, "pod 01 holds the start of s1-link", func() bool {
		if v.Pod.Activity != Traveling || v.reservedThrough < 0 {
			return false
		}
		committed := v.blocks.at(v.reservedThrough)
		return committed.lane.ID == "s1-link" && v.blocks.end(v.reservedThrough)-committed.laneStart <= 60
	})
	route, version := slices.Clone(v.Route), v.routeVersion
	id := startDebris(t, s, "s1-link", 95, 100, 0)
	if !s.rerouteCandidate(v) {
		t.Fatal("the trapped pod is not a reroute candidate")
	}
	if _, ok := s.endpointRoute(v); ok {
		t.Fatal("the trapped pod has an endpoint route")
	}
	stepUntil(t, s, "pod 01 waits at the debris", func() bool { return v.Pod.Speed == 0 && v.Pod.WaitReason == blockedByIncident })
	if s.faultCounters.reroutes != 0 || v.routeVersion != version || !sameLanes(v.Route, route) || v.Pod.BlockedBy != id {
		t.Fatalf("%d reroutes, route %v, blocked by %q", s.faultCounters.reroutes, v.Route, v.Pod.BlockedBy)
	}
}

// TestEndpointRouteRefusals checks the cases where endpointRoute reports
// false with a blocked set that the test sets: a pod in its arrival
// chain, a blocked destination berth, and a pod that is not a candidate.
// The pass changes no route.
func TestEndpointRouteRefusals(t *testing.T) {
	t.Parallel()
	t.Run("arrival chain", func(t *testing.T) {
		t.Parallel()
		s := altLineFleet(t, -100, "p-1", "s3-1")
		v := s.findVehicle("01")
		s1, _ := s.station("s1")
		berth, _ := s1.berth("s1-2")
		if err := s.startEmptyMove(v, emptyDestination{station: "s1", berth: berth, reserveBerth: true}); err != nil {
			t.Fatal(err)
		}
		stepUntil(t, s, "pod 01 holds its inlet", func() bool {
			return v.reservedThrough >= 0 && v.blocks.at(v.reservedThrough).lane.ID == "s1-2-in"
		})
		blockLanes(t, s, "s1-2-out", "s1-2-in")
		s.blocked.lanes[s.graph.lanes["s1-2-out"]] = false
		if _, _, ok := s.divertStart(v); ok {
			t.Fatal("the pod in its arrival chain can divert")
		}
		version := v.routeVersion
		if !s.rerouteCandidate(v) {
			t.Fatal("the pod is not a reroute candidate")
		}
		if _, ok := s.endpointRoute(v); ok || s.rerouteToEndpoint(v) || v.routeVersion != version {
			t.Fatal("the pod in its arrival chain is rerouted")
		}
	})
	t.Run("blocked destination berth", func(t *testing.T) {
		t.Parallel()
		s := altLineFleet(t, -100, "p-1", "s3-1")
		v := s.findVehicle("01")
		s2, _ := s.station("s2")
		berth, _ := s2.berth("s2-2")
		if err := s.startEmptyMove(v, emptyDestination{station: "s2", berth: berth, reserveBerth: true}); err != nil {
			t.Fatal(err)
		}
		claims := berthResources(berth)
		s.setBlocked([]faultFootprint{{id: "i1.1", resources: claims[:]}})
		if !s.rerouteCandidate(v) {
			t.Fatal("the pod is not a reroute candidate")
		}
		if route, ok := s.endpointRoute(v); ok {
			t.Fatalf("the pod has the route %v to its blocked berth", route)
		}
	})
	t.Run("not a candidate", func(t *testing.T) {
		t.Parallel()
		s := altLineFleet(t, -100, "s0-1", "s3-1")
		v := s.findVehicle("01")
		if err := s.RequestJourney("01", "s2"); err != nil {
			t.Fatal(err)
		}
		blockLanes(t, s, "s1-link")
		for name, change := range map[string]func(){
			"faulted":         func() { v.faulted = true },
			"platoon member":  func() { v.follower = 2 },
			"coupling member": func() { v.couplingID = "pair" },
			"granted":         func() { v.reservedThrough = 0 },
			"unloading":       func() { v.Pod.Activity = Unloading },
		} {
			before := *v
			change()
			if s.rerouteCandidate(v) {
				t.Errorf("%s: the pod is a reroute candidate", name)
			}
			*v = before
		}
		if !s.rerouteCandidate(v) {
			t.Fatal("the boarding pod is not a reroute candidate")
		}
		s.setBlocked(nil)
		if s.rerouteCandidate(v) {
			t.Fatal("with an empty blocked set, the pod is a reroute candidate")
		}
	})
}

// TestReroutePassCadence checks when the reroute pass runs: at a new epoch,
// and every rerouteIntervalTicks while the blocked set is not empty.
func TestReroutePassCadence(t *testing.T) {
	t.Parallel()
	s := altLineFleet(t, -100, "s0-1", "s3-1")
	v := s.findVehicle("01")
	if err := s.RequestJourney("01", "s2"); err != nil {
		t.Fatal(err)
	}
	startDebris(t, s, "s1-link", 70, 80, 0)
	if !s.rerouteDue {
		t.Fatal("the new epoch asks for no reroute pass")
	}
	s.rerouteDue = false
	route := slices.Clone(v.Route)
	s.tick = 2*rerouteIntervalTicks - 1
	s.reroutePass()
	if !sameLanes(v.Route, route) {
		t.Fatal("the pass ran between the cadence ticks")
	}
	s.tick++
	s.reroutePass()
	if sameLanes(v.Route, route) || s.faultCounters.reroutes != 1 {
		t.Fatal("the pass did not run at the cadence tick")
	}
	s.rerouteDue = true
	s.reroutePass()
	if s.rerouteDue {
		t.Fatal("the pass did not end the request")
	}
}

// TestEndpointRerouteOfFaultRecovery evacuates a faulted pod on s0-link
// and clears the fault. The fault recovery continues the original route,
// and debris then blocks s1-link. The pass reroutes the recovery, which
// keeps its hold and its purpose, arrives, and returns to service.
func TestEndpointRerouteOfFaultRecovery(t *testing.T) {
	t.Parallel()
	s := altLineFleet(t, -100, "s0-1", "s3-1")
	if err := s.SetFaults(true, FaultSettings{}); err != nil {
		t.Fatal(err)
	}
	checkFaultsEachTick(t, s)
	v := s.findVehicle("01")
	if err := s.RequestJourney("01", "s2"); err != nil {
		t.Fatal(err)
	}
	cruiseOn(t, s, v, "s0-link")
	id := startFault(t, s, v, 0)
	stepUntil(t, s, "the evacuation", func() bool { return v.op.purpose == opEmptyRecovery })
	if err := s.clearFault(id); err != nil {
		t.Fatal(err)
	}
	startDebris(t, s, "s1-link", 70, 80, 0)
	s.Step()
	if s.faultCounters.reroutes != 1 || !usesLane(v.Route, "alt-in") || v.withdrawn != faultHold || v.op.purpose != opEmptyRecovery {
		t.Fatalf("%d reroutes, route %v, holds %d, purpose %d", s.faultCounters.reroutes, v.Route, v.withdrawn, v.op.purpose)
	}
	stepUntil(t, s, "the hold release", func() bool { return v.inService() })
	if v.Pod.Activity != Idle || v.Pod.StationID != "s2" {
		t.Fatalf("the recovery ends %s at %s", v.Pod.Activity, v.Pod.StationID)
	}
}

// bufferBypassNetwork is stationBufferNetwork(Example(), 4) with a second
// entry lane of Market, from the node bypass. Its speed limit makes it the
// free route from the bypass.
func bufferBypassNetwork() Network {
	network := stationBufferNetwork(Example(), 4)
	network.Lanes = append(network.Lanes, Lane{ID: "bypass-entry", From: "bypass", To: "market-entry", SpeedLimit: 20, StationID: "market", StationRole: StationEntryRole})
	return network
}

// TestEndpointRerouteOfBufferedPod blocks the entry lane of a buffered
// pod. The only route to the same entry ends with the other entry lane, so
// the pod keeps its route and its buffer membership, and waits.
func TestEndpointRerouteOfBufferedPod(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(bufferBypassNetwork(), []Placement{{ID: "01", StationID: "harbor"}})
	if err != nil {
		t.Fatal(err)
	}
	s.incidentContract = IncidentV1Contract
	s.faultsOn = true
	s.SetStationBuffers(true)
	if err := s.RequestJourney("01", "market"); err != nil {
		t.Fatal(err)
	}
	v := s.findVehicle("01")
	stepUntil(t, s, "pod 01 is buffered", func() bool { return v.buffered })
	if v.Route[len(v.Route)-1].ID != "bypass-entry" || v.Pod.Activity != Traveling {
		t.Fatalf("the pod is not buffered on bypass-entry: buffered %t, route %v", v.buffered, v.Route)
	}
	checkFaultsEachTick(t, s)
	route, bufferBerth := slices.Clone(v.Route), v.bufferBerth
	length := s.graph.lengths[laneIndex(t, s, "bypass-entry")]
	startDebris(t, s, "bypass-entry", math.Floor(length/2), math.Floor(length/2)+10, 0)
	if !s.rerouteCandidate(v) {
		t.Fatal("the buffered pod is not a reroute candidate")
	}
	s.Step()
	if s.faultCounters.reroutes != 0 || !sameLanes(v.Route, route) || !v.buffered || v.bufferBerth != bufferBerth {
		t.Fatalf("%d reroutes, route %v, buffered %t", s.faultCounters.reroutes, v.Route, v.buffered)
	}
}

// TestTerminalReevaluationDuringIncident runs full steps. Pod 01 rides to
// s1 and chooses the berth s1-1, where pod 02 is idle. A pod fault on pod
// 02 then blocks s1-1. When the berth s1-2 is free, the pod takes it and
// keeps its station, riders and stops. When a pod fault also blocks s1-2,
// the pod waits, blocked by the incident.
func TestTerminalReevaluationDuringIncident(t *testing.T) {
	t.Parallel()
	for _, both := range []bool{false, true} {
		s := altLineFleet(t, -100, "s0-1", "s1-1", "s1-2")
		v, first, second := s.findVehicle("01"), s.findVehicle("02"), s.findVehicle("03")
		if err := s.RequestJourney("01", "s1"); err != nil {
			t.Fatal(err)
		}
		stepUntil(t, s, "the terminal berth choice", func() bool { return v.destination.ID != "" })
		if v.destination.ID != "s1-1" {
			t.Fatalf("pod 01 chose %s", v.destination.ID)
		}
		id := startFault(t, s, first, 0)
		if both {
			startFault(t, s, second, 0)
		} else if err := s.RequestJourney("03", "s3"); err != nil {
			t.Fatal(err)
		}
		checkFaultsEachTick(t, s)
		riders, stops := slices.Clone(v.Riders), slices.Clone(v.Stops)
		if !both {
			stepUntil(t, s, "pod 01 takes s1-2", func() bool { return v.destination.ID == "s1-2" })
			if v.destinationStation != "s1" || !reflect.DeepEqual(v.Riders, riders) || !slices.Equal(v.Stops, stops) {
				t.Fatalf("pod 01 goes to %s with riders %v and stops %v", v.destinationStation, v.Riders, v.Stops)
			}
			stepUntil(t, s, "the rider completes", func() bool { return s.completed == 1 })
			continue
		}
		stepUntil(t, s, "pod 01 waits", func() bool { return v.Pod.Speed == 0 && v.Pod.WaitReason == blockedByIncident })
		if v.destination.ID != "s1-1" || v.Pod.BlockedBy != id {
			t.Fatalf("pod 01 waits for %s, blocked by %q", v.destination.ID, v.Pod.BlockedBy)
		}
	}
}
