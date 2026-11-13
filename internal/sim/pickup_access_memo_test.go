package sim

import (
	"slices"
	"testing"
)

// accessOK reports pickupAccess of pod 01 for a new trip to s3.
func accessOK(s *Simulation, from string) bool {
	return s.pickupAccess(s.findVehicle("01"), newTrip(s, from, "s3").request)
}

// storesFailure reports whether the route memo holds a failed search.
func storesFailure(s *Simulation) bool {
	for _, result := range s.routes {
		if result.err != nil {
			return true
		}
	}
	return false
}

// accessFailure returns a fleet whose pod has no pickup access from its berth,
// and a route memo that holds the failed search.
func accessFailure(t *testing.T) *Simulation {
	t.Helper()
	s := altLineFleet(t, -100, "s1-1")
	blockLanes(t, s, "s1-1-out")
	if accessOK(s, "s2") {
		t.Fatal("the pod has access through the blocked lane")
	}
	if !storesFailure(s) {
		t.Fatal("the failed access stored no search")
	}
	return s
}

// TestPickupAccessMemoEndsWithEpoch checks each path that changes the routing
// graph. A search of pickupAccess that stays in the route memo over one of
// them refuses an access that exists.
func TestPickupAccessMemoEndsWithEpoch(t *testing.T) {
	t.Parallel()
	t.Run("another footprint", func(t *testing.T) {
		t.Parallel()
		s := accessFailure(t)
		blockLanes(t, s, "s3-through")
		if !accessOK(s, "s2") {
			t.Error("the failure stays after the blocked set changes")
		}
	})
	t.Run("empty set", func(t *testing.T) {
		t.Parallel()
		s := accessFailure(t)
		s.setBlocked(nil)
		if s.routes != nil {
			t.Errorf("the memo stays with no blocked set: %d entries", len(s.routes))
		}
		blockLanes(t, s, "s3-through")
		if !accessOK(s, "s2") {
			t.Error("the failure stays after an empty blocked set")
		}
	})
	t.Run("reset", func(t *testing.T) {
		t.Parallel()
		s := accessFailure(t)
		s.Reset()
		if s.routes != nil {
			t.Errorf("the memo stays after a reset: %d entries", len(s.routes))
		}
	})
	t.Run("network rebuild", func(t *testing.T) {
		t.Parallel()
		s := accessFailure(t)
		// Only a change of the network size makes a rebuild.
		indexes := *s.networkIndexes
		indexes.network.Nodes = append(slices.Clip(indexes.network.Nodes), Node{ID: "extra"})
		s.networkIndexes = &indexes
		s.ensureNetworkIndexes()
		if s.routes != nil {
			t.Errorf("the memo stays after a network rebuild: %d entries", len(s.routes))
		}
	})
	t.Run("fault clear", func(t *testing.T) {
		t.Parallel()
		s := altLineFleet(t, -100, "s1-1")
		length := s.graph.lengths[laneIndex(t, s, "s1-1-out")]
		failing := startDebris(t, s, "s1-1-out", length/2, length/2+1, 0)
		through := s.graph.lengths[laneIndex(t, s, "s3-through")]
		other := startDebris(t, s, "s3-through", through/2, through/2+1, 0)
		if accessOK(s, "s2") || !storesFailure(s) {
			t.Fatalf("no stored failure: %d entries", len(s.routes))
		}
		if err := s.ClearFault(failing); err != nil {
			t.Fatal(err)
		}
		if !s.blockedActive() || !accessOK(s, "s2") {
			t.Errorf("the failure stays after the clear, with fault %s active", other)
		}
	})
}

// TestPickupAccessMemoCopies checks that a clone and a restore start with no
// route memo, and that a query on a clone does not write the memo of its
// source.
func TestPickupAccessMemoCopies(t *testing.T) {
	t.Parallel()
	s := altLineFleet(t, -100, "s1-1")
	length := s.graph.lengths[laneIndex(t, s, "s1-1-out")]
	startDebris(t, s, "s1-1-out", length/2, length/2+1, 0)
	if accessOK(s, "s2") || !storesFailure(s) {
		t.Fatalf("no stored failure: %d entries", len(s.routes))
	}
	stored := len(s.routes)
	clone := s.Clone()
	if clone.routes != nil {
		t.Fatalf("the clone has the memo with %d entries", len(clone.routes))
	}
	if accessOK(clone, "s0") || !storesFailure(clone) {
		t.Fatalf("the clone has access, or stored nothing: %d entries", len(clone.routes))
	}
	if len(s.routes) != stored {
		t.Errorf("the clone wrote the memo of its source: %d entries, want %d", len(s.routes), stored)
	}
	restored, _, err := RestoreState(RestoreStateInput{FaultsOn: true, Faults: FaultSettings{EvacuationSeconds: 300}, Network: s.network, Fleet: place("s1-1"), State: s.ExportState()})
	if err != nil {
		t.Fatal(err)
	}
	if restored.routes != nil {
		t.Fatalf("the restore has the memo with %d entries", len(restored.routes))
	}
	if accessOK(restored, "s2") {
		t.Error("the restore has lost the blocked set")
	}
}

// TestPickupAccessHidesRepeat checks that a second query does no search, for
// the free-flow searches and for the station paths of a network with class
// restrictions. The route memo serves the repeat, and each of its entries
// equals a new search.
func TestPickupAccessHidesRepeat(t *testing.T) {
	t.Parallel()
	compact, err := NewClassSet("compact", "legacy")
	if err != nil {
		t.Fatal(err)
	}
	s := altLineFleetClasses(t, -100, compact, "s1-1")
	blockLanes(t, s, "s3-through")
	if !accessOK(s, "s2") {
		t.Fatal("the pod has no access")
	}
	station := false
	for key := range s.routes {
		station = station || key.station
	}
	if !station {
		t.Fatal("the fixture stores no station path")
	}
	checkRouteMemo(t, s)
	searches := s.routeWork.searches
	if !accessOK(s, "s2") {
		t.Fatal("the pod has no access on the repeat")
	}
	if made := s.routeWork.searches - searches; made != 0 {
		t.Errorf("the repeat made %d searches", made)
	}
}

// TestPickupAccessView checks that the routing view of pickupAccess ends with
// the query, and that a query nested in the view of the same pod keeps the
// outer view.
func TestPickupAccessView(t *testing.T) {
	t.Parallel()
	t.Run("nested in the view of the same pod", func(t *testing.T) {
		t.Parallel()
		s := altLineFleet(t, -100, "s1-1")
		blockLanes(t, s, "s3-through")
		v := s.findVehicle("01")
		previous := s.enterRouteView(v)
		outer := s.routeView
		if !s.pickupAccess(v, newTrip(s, "s2", "s3").request) {
			t.Fatal("the pod has no access")
		}
		if s.routeView != outer {
			t.Error("the nested query changed the outer view")
		}
		s.leaveRouteView(previous)
	})
	t.Run("the view ends", func(t *testing.T) {
		t.Parallel()
		s := altLineFleet(t, -100, "s1-1")
		blockLanes(t, s, "s3-through")
		accessOK(s, "s2")
		if s.routeView != nil {
			t.Error("the view stays after the query")
		}
	})
}
