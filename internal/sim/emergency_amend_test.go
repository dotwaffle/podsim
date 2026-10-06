package sim

import (
	"slices"
	"testing"
)

// bindTo makes the bound pod v of the only record go to the berth of the
// station, with the interrupt set of its purpose. A test uses it to put
// the pod at a berth that the station choice would not take.
func bindTo(t *testing.T, s *Simulation, v *vehicle, station, berth string) {
	t.Helper()
	if err := s.setOperationalDestination(v, operationalTarget{purpose: opEmergencyUnload, owner: emergencyHold, interrupt: v.op.interrupt, station: station, berth: berth}); err != nil {
		t.Fatal(err)
	}
}

// TestEmergencyBerthFilter checks the berth filter of an emergency unload
// (section 9.2 of the incident emergency contract). Without class
// restrictions, it is nil. With them, it tests only the class of the pod:
// it accepts a berth of the emergency station also when the stops of the
// riders cannot follow it.
func TestEmergencyBerthFilter(t *testing.T) {
	t.Parallel()
	for _, restricted := range []bool{false, true} {
		var classes ClassSet
		if restricted {
			classes, _ = NewClassSet("legacy", "express")
		}
		s := altLineFleetClasses(t, -100, classes, "s0-1", "s3-1")
		s.emergenciesOn = true
		v := boardParties(t, s, "s1", "s2")
		travelOn(t, s, v, "s0-link")
		startEmergency(t, s, v, 0)
		if v.op.purpose != opEmergencyUnload || v.destinationStation != "s1" {
			t.Fatalf("the pod has %+v at %s, want s1", v.op, v.destinationStation)
		}
		stops := v.Stops
		v.Stops = []string{"s1", "nowhere"}
		accept := s.berthFilterForVehicle(v)
		v.Stops = stops
		if restricted != (accept != nil) {
			t.Fatalf("class restrictions %t: the filter is set: %t", restricted, accept != nil)
		}
		if accept != nil && (!accept(v.destination) || !accept(Berth{ID: "s1-2", Node: "s1-2"})) {
			t.Fatal("the filter refuses a berth of the emergency station")
		}
	}
}

// TestEmergencyTerminalBerth binds a pod to the berth s1-2, where pod 02
// is idle. The rider detour test refuses each other berth, because the
// pod has ridden far. Terminal reevaluation still moves the bound pod to
// the free berth s1-1, where it unloads.
func TestEmergencyTerminalBerth(t *testing.T) {
	t.Parallel()
	s := newLegFleet(t, "s0-1", "s1-2")
	s.incidentContract, s.emergenciesOn = IncidentV1Contract, true
	if err := s.SetSharedRidePartyLimit(4); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSharedRideMode(SharedRideDropOffs, DefaultSharedRideMaxStops); err != nil {
		t.Fatal(err)
	}
	v := boardParties(t, s, "s1", "s2")
	travelOn(t, s, v, "s0-link")
	checkEmergenciesEachTick(t, s)
	startEmergency(t, s, v, 1)
	bindTo(t, s, v, "s1", "s1-2")
	v.riddenBase = 1e7
	if route, _ := s.stationPathForClass("s1-entry", "s1-1", v.Pod.Class); s.rerouteKeepsDetours(v, append(slices.Clone(v.Route[:len(v.Route)-1]), route...), Berth{ID: "s1-1", Node: "s1-1"}) {
		t.Fatal("the detour test accepts s1-1")
	}
	stepUntil(t, s, "the unload", func() bool { return v.Pod.Activity == Unloading || v.Pod.WaitReason != NoWait && v.Pod.Speed == 0 })
	if v.Pod.Activity != Unloading || v.Pod.BerthID != "s1-1" {
		t.Fatalf("the pod is %s at %q, waits for %s, want the unload at s1-1", v.Pod.Activity, v.Pod.BerthID, v.Pod.WaitReason)
	}
}

// TestEmergencyEndpointReroute binds a pod that rides to s2 to the
// emergency berth s2-1, and debris then blocks s1-link. The road through
// alt takes the rider over the detour limit, but the endpoint reroute
// skips the detour test for an emergency unload, so the pod keeps its
// emergency berth on the other road.
func TestEmergencyEndpointReroute(t *testing.T) {
	t.Parallel()
	s := altLineFleet(t, -400, "s0-1", "s3-1")
	s.emergenciesOn = true
	checkEmergenciesEachTick(t, s)
	v := s.findVehicle("01")
	if err := s.RequestJourney("01", "s2"); err != nil {
		t.Fatal(err)
	}
	travelOn(t, s, v, "s0-link")
	startEmergency(t, s, v, 0)
	bindTo(t, s, v, "s2", "s2-1")
	startDebris(t, s, "s1-link", 70, 80, 0)
	stepUntil(t, s, "the reroute", func() bool {
		return s.faultCounters.reroutes > 0 || v.Pod.Speed == 0 && v.Pod.WaitReason == BlockedByIncident
	})
	if s.faultCounters.reroutes != 1 || !usesLane(v.Route, "alt-in") || v.destination.ID != "s2-1" || v.op.purpose != opEmergencyUnload {
		t.Fatalf("%d reroutes, route %v to %s, purpose %+v", s.faultCounters.reroutes, laneIDs(v.Route), v.destination.ID, v.op)
	}
}

// TestEmergencyBankSearches makes a route search from the departure path
// of bank a to the berth of bank a. The local search fails, and the bank
// route then makes the departure, middle, and arrival searches. The same
// search on the static graph counts as static searches only. Under each routing
// policy, one assignedRoute call makes at most 12 graph searches.
func TestEmergencyBankSearches(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(BankExample(), []Placement{{ID: "01", StationID: "origin", BerthID: "origin-1"}})
	if err != nil {
		t.Fatal(err)
	}
	s.ensureNetworkIndexes()
	before := s.searchCounters
	if _, err := s.searchRoute(networkRouteInput{from: "bank-a-departure", to: "bank-a-berth", terminalBerthsOnly: true}); err != nil {
		t.Fatal(err)
	}
	after := s.searchCounters
	if after.graph-before.graph != 4 || after.localFailed-before.localFailed != 1 || after.routes-before.routes != 1 || after.failed != before.failed {
		t.Fatalf("counters %+v, then %+v, want 4 graph searches with 1 failed local attempt in 1 route search", before, after)
	}
	_, _ = s.searchStaticRoute(networkRouteInput{from: "bank-a-departure", to: "bank-a-berth"})
	if static := s.searchCounters; static.static != after.static+4 || static.graph != after.graph || static.routes != after.routes {
		t.Fatalf("counters %+v, then %+v, want the 4 searches of the bank route as static searches", after, static)
	}
	for _, policy := range []RoutingPolicy{FreeFlowRouting, CongestionRouting, QueueRouting, PredictiveRouting} {
		c := s.Clone()
		if err := c.SetRoutingPolicy(policy); err != nil {
			t.Fatal(err)
		}
		before := c.searchCounters.graph
		if _, err := c.assignedRoute(&c.vehicles[0], "bank-a-departure", "bank-a-berth"); err != nil {
			t.Fatal(err)
		}
		if searches := c.searchCounters.graph - before; searches < 4 || searches > 12 {
			t.Fatalf("policy %d: %d graph searches in one call", policy, searches)
		}
	}
}
