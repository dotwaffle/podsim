package sim

import (
	"slices"
	"strconv"
	"testing"
)

// candidateOK reports whether the pod has a pickup route for the trip.
func candidateOK(s *Simulation, pod, from, to string) bool {
	_, _, ok := s.candidateRouteForRequest(s.findVehicle(pod), newTrip(s, from, to).request, nil)
	return ok
}

// TestPickupMissKey checks that a failure hides only the searches with the
// same key. For each member of the key, the same query with another value
// has a route. The fixture stations are in a ring: p, s0, s1, s2, s3.
func TestPickupMissKey(t *testing.T) {
	t.Parallel()
	t.Run("berth", func(t *testing.T) {
		t.Parallel()
		s := altLineFleet(t, -100, "s1-1", "s1-2")
		blockLanes(t, s, "s1-1-out")
		if candidateOK(s, "01", "s2", "s3") || !candidateOK(s, "02", "s2", "s3") {
			t.Error("a failure from one berth hides the route from another berth")
		}
	})
	t.Run("origin", func(t *testing.T) {
		t.Parallel()
		s := altLineFleet(t, -100, "s0-1")
		blockLanes(t, s, "s3-1-in")
		if candidateOK(s, "01", "s3", "s0") || !candidateOK(s, "01", "s2", "s0") {
			t.Error("a failure for one origin hides the route for another origin")
		}
	})
	t.Run("destination", func(t *testing.T) {
		t.Parallel()
		s := altLineFleet(t, -100, "s0-1")
		blockLanes(t, s, "s2-link")
		if candidateOK(s, "01", "s1", "s3") || !candidateOK(s, "01", "s1", "s2") {
			t.Error("a failure for one destination hides the route for another destination")
		}
	})
	t.Run("class", func(t *testing.T) {
		t.Parallel()
		compact, err := NewClassSet("compact")
		if err != nil {
			t.Fatal(err)
		}
		s := altLineFleetClasses(t, -100, compact, "s1-1")
		blockLanes(t, s, "s1-link")
		if candidateOK(s, "01", "s2", "s3") {
			t.Fatal("a legacy pod has a route on the Compact lane")
		}
		s.findVehicle("01").Pod.Class = CompactClass
		if !candidateOK(s, "01", "s2", "s3") {
			t.Error("a failure for one class hides the route for another class")
		}
	})
}

// TestPickupMissHidesRepeat checks that a second query with the key of a
// failure does no search. A key that a query has not failed has a search.
func TestPickupMissHidesRepeat(t *testing.T) {
	t.Parallel()
	s := altLineFleet(t, -100, "s1-1")
	blockLanes(t, s, "s1-1-out")
	if candidateOK(s, "01", "s2", "s3") {
		t.Fatal("the pod has a route")
	}
	if len(s.pickupMisses) != 1 {
		t.Fatalf("the memo has %d entries, want 1", len(s.pickupMisses))
	}
	s.routes, s.routeOrder = nil, nil
	if candidateOK(s, "01", "s2", "s3") {
		t.Fatal("the pod has a route")
	}
	if len(s.routes) != 0 {
		t.Errorf("the repeat made %d route lookups", len(s.routes))
	}
}

// TestPickupMissEndsWithBlockedSet checks each path that changes the blocked
// set: another footprint, an empty set, and a clear through the command. A
// memo that stays over one of them refuses a route that exists.
func TestPickupMissEndsWithBlockedSet(t *testing.T) {
	t.Parallel()
	fail := func(t *testing.T) *Simulation {
		t.Helper()
		s := altLineFleet(t, -100, "s1-1")
		blockLanes(t, s, "s1-1-out")
		if candidateOK(s, "01", "s2", "s3") || len(s.pickupMisses) != 1 {
			t.Fatalf("no memoized failure: %v", s.pickupMisses)
		}
		return s
	}
	t.Run("another footprint", func(t *testing.T) {
		t.Parallel()
		s := fail(t)
		blockLanes(t, s, "s3-through")
		if !candidateOK(s, "01", "s2", "s3") {
			t.Error("the failure stays after the blocked set changes")
		}
	})
	t.Run("empty set", func(t *testing.T) {
		t.Parallel()
		s := fail(t)
		s.setBlocked(nil)
		if s.pickupMisses != nil || !candidateOK(s, "01", "s2", "s3") {
			t.Errorf("the failure stays with no blocked set: %v", s.pickupMisses)
		}
	})
	t.Run("reset", func(t *testing.T) {
		t.Parallel()
		s := fail(t)
		s.Reset()
		if s.pickupMisses != nil {
			t.Errorf("the failure stays after a reset: %v", s.pickupMisses)
		}
	})
	t.Run("network rebuild", func(t *testing.T) {
		t.Parallel()
		s := fail(t)
		// Only a change of the network size makes a rebuild.
		indexes := *s.networkIndexes
		indexes.network.Nodes = append(slices.Clip(indexes.network.Nodes), Node{ID: "extra"})
		s.networkIndexes = &indexes
		s.ensureNetworkIndexes()
		if s.pickupMisses != nil {
			t.Errorf("the failure stays after a network rebuild: %v", s.pickupMisses)
		}
	})
	t.Run("fault clear", func(t *testing.T) {
		t.Parallel()
		s := altLineFleet(t, -100, "s1-1")
		length := s.graph.lengths[laneIndex(t, s, "s1-1-out")]
		id := startDebris(t, s, "s1-1-out", length/2, length/2+1, 0)
		trip := newTrip(s, "s2", "s3")
		s.waiting = []waitingTrip{trip}
		s.dispatch()
		if s.waiting[0].request.PodID != "" || len(s.pickupMisses) == 0 {
			t.Fatalf("with the debris, the trip has pod %q, memo %v", s.waiting[0].request.PodID, s.pickupMisses)
		}
		if err := s.ClearFault(id); err != nil {
			t.Fatal(err)
		}
		s.dispatch()
		if s.waiting[0].request.PodID != "01" {
			t.Errorf("after the clear, the trip has pod %q", s.waiting[0].request.PodID)
		}
	})
}

// TestPickupMissSkipsOtherQueries checks each query that the memo does not
// apply to. A false entry with the key of the query must not refuse it.
func TestPickupMissSkipsOtherQueries(t *testing.T) {
	t.Parallel()
	poison := func(s *Simulation, berth, origin, to string, class VehicleClass) {
		s.pickupMisses = map[pickupMiss]bool{{berth: berth, origin: origin, to: to, class: class}: true}
	}
	t.Run("empty blocked set", func(t *testing.T) {
		t.Parallel()
		s := altLineFleet(t, -100, "s1-1")
		poison(s, "s1-1", "s2", "s3", s.findVehicle("01").Pod.Class)
		if !candidateOK(s, "01", "s2", "s3") {
			t.Error("the memo refuses a query with an empty blocked set")
		}
	})
	t.Run("pod idle at the origin", func(t *testing.T) {
		t.Parallel()
		s := altLineFleet(t, -100, "s1-1")
		blockLanes(t, s, "return")
		poison(s, "s1-1", "s1", "s2", s.findVehicle("01").Pod.Class)
		if !candidateOK(s, "01", "s1", "s2") {
			t.Error("the memo refuses a pod idle at the origin")
		}
	})
	t.Run("moving pod", func(t *testing.T) {
		t.Parallel()
		s := altLineFleet(t, -100, "s0-1")
		v := s.findVehicle("01")
		if err := s.sendPickup(v, "p"); err != nil {
			t.Fatal(err)
		}
		v.released = true
		blockLanes(t, s, "s3-1-in")
		poison(s, v.Pod.BerthID, "s1", "s2", v.Pod.Class)
		if !candidateOK(s, "01", "s1", "s2") {
			t.Error("the memo refuses a moving pod")
		}
		if candidateOK(s, "01", "s3", "s2") || len(s.pickupMisses) != 1 {
			t.Errorf("a failure of a moving pod is in the memo: %v", s.pickupMisses)
		}
	})
	t.Run("origin with banks", func(t *testing.T) {
		t.Parallel()
		s, err := NewFleet(BankExample(), []Placement{{ID: "01", StationID: "origin", BerthID: "origin-1"}})
		if err != nil {
			t.Fatal(err)
		}
		// The block leaves a path to the origin of the query.
		blockLanes(t, s, "bank-a-out")
		poison(s, "origin-1", "hub", "origin", s.findVehicle("01").Pod.Class)
		if !candidateOK(s, "01", "hub", "origin") {
			t.Error("the memo refuses an origin with banks")
		}
	})
}

// TestPickupMissCopies checks that a clone and a restore start with no memo
// and give the result of a new search, and that a clone does not write the
// memo of its source.
func TestPickupMissCopies(t *testing.T) {
	t.Parallel()
	s := altLineFleet(t, -100, "s1-1")
	length := s.graph.lengths[laneIndex(t, s, "s1-1-out")]
	startDebris(t, s, "s1-1-out", length/2, length/2+1, 0)
	if candidateOK(s, "01", "s2", "s3") || len(s.pickupMisses) != 1 {
		t.Fatalf("no memoized failure: %v", s.pickupMisses)
	}
	clone := s.Clone()
	if clone.pickupMisses != nil {
		t.Fatalf("the clone has the memo %v", clone.pickupMisses)
	}
	if candidateOK(clone, "01", "s0", "s3") {
		t.Fatal("the clone has a route")
	}
	if len(s.pickupMisses) != 1 || len(clone.pickupMisses) != 1 {
		t.Errorf("source memo %v, clone memo %v", s.pickupMisses, clone.pickupMisses)
	}
	restored, _, err := RestoreState(RestoreStateInput{FaultsOn: true, Faults: FaultSettings{EvacuationSeconds: 300}, Network: s.network, Fleet: place("s1-1"), State: s.ExportState()})
	if err != nil {
		t.Fatal(err)
	}
	if restored.pickupMisses != nil {
		t.Fatalf("the restore has the memo %v", restored.pickupMisses)
	}
	if candidateOK(restored, "01", "s2", "s3") || len(restored.pickupMisses) != 1 {
		t.Errorf("the restore has lost the blocked set: memo %v", restored.pickupMisses)
	}
}

// TestPickupMissUnderRouteView checks that a query under a routing view
// stores nothing, as for the route memo.
func TestPickupMissUnderRouteView(t *testing.T) {
	t.Parallel()
	s := altLineFleet(t, -100, "s1-1")
	blockLanes(t, s, "s1-1-out")
	v := s.findVehicle("01")
	previous := s.enterRouteView(v)
	if candidateOK(s, "01", "s2", "s3") {
		t.Fatal("the pod has a route")
	}
	s.leaveRouteView(previous)
	if s.pickupMisses != nil {
		t.Errorf("the query under a view wrote the memo %v", s.pickupMisses)
	}
}

// TestPickupMissLimit checks that a full memo starts again.
func TestPickupMissLimit(t *testing.T) {
	t.Parallel()
	s := altLineFleet(t, -100, "s1-1")
	for index := range routeCacheLimit {
		s.notePickupMiss(pickupMiss{berth: strconv.Itoa(index)})
	}
	if len(s.pickupMisses) != routeCacheLimit {
		t.Fatalf("the memo has %d entries", len(s.pickupMisses))
	}
	s.notePickupMiss(pickupMiss{berth: "last"})
	if len(s.pickupMisses) != 1 {
		t.Errorf("a full memo has %d entries after one more", len(s.pickupMisses))
	}
}

// TestPickupMissStoresOnlyFailures checks that a query with a route stays
// out of the memo.
func TestPickupMissStoresOnlyFailures(t *testing.T) {
	t.Parallel()
	s := altLineFleet(t, -100, "s1-2")
	blockLanes(t, s, "s1-1-out")
	for range 2 {
		if !candidateOK(s, "01", "s2", "s3") {
			t.Fatal("the pod has no route")
		}
	}
	if s.pickupMisses != nil {
		t.Errorf("the memo holds a query with a route: %v", s.pickupMisses)
	}
}
