package sim

import (
	"slices"
	"strconv"
	"testing"
)

// accessOK reports pickupAccess of pod 01 for a new trip to s3.
func accessOK(s *Simulation, from string) bool {
	return s.pickupAccess(s.findVehicle("01"), newTrip(s, from, "s3").request)
}

// accessFailure returns a fleet whose pod has no pickup access from its berth,
// and a memo that holds the failed search.
func accessFailure(t *testing.T) *Simulation {
	t.Helper()
	s := altLineFleet(t, -100, "s1-1")
	blockLanes(t, s, "s1-1-out")
	if accessOK(s, "s2") {
		t.Fatal("the pod has access through the blocked lane")
	}
	if len(s.accessRoutes) == 0 {
		t.Fatal("the failed access stored no search")
	}
	return s
}

// TestAccessRoutesEndWithEpoch checks each path that changes the routing graph.
// A search that stays over one of them refuses an access that exists.
func TestAccessRoutesEndWithEpoch(t *testing.T) {
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
		if s.accessRoutes != nil {
			t.Errorf("the memo stays with no blocked set: %d entries", len(s.accessRoutes))
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
		if s.accessRoutes != nil {
			t.Errorf("the memo stays after a reset: %d entries", len(s.accessRoutes))
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
		if s.accessRoutes != nil {
			t.Errorf("the memo stays after a network rebuild: %d entries", len(s.accessRoutes))
		}
	})
	t.Run("fault clear", func(t *testing.T) {
		t.Parallel()
		s := altLineFleet(t, -100, "s1-1")
		length := s.graph.lengths[laneIndex(t, s, "s1-1-out")]
		failing := startDebris(t, s, "s1-1-out", length/2, length/2+1, 0)
		through := s.graph.lengths[laneIndex(t, s, "s3-through")]
		other := startDebris(t, s, "s3-through", through/2, through/2+1, 0)
		if accessOK(s, "s2") || len(s.accessRoutes) == 0 {
			t.Fatalf("no stored failure: %d entries", len(s.accessRoutes))
		}
		if err := s.ClearFault(failing); err != nil {
			t.Fatal(err)
		}
		if !s.blockedActive() || !accessOK(s, "s2") {
			t.Errorf("the failure stays after the clear, with fault %s active", other)
		}
	})
}

// TestAccessRoutesCopies checks that a clone and a restore start with no memo,
// and that a clone does not write the memo of its source.
func TestAccessRoutesCopies(t *testing.T) {
	t.Parallel()
	s := altLineFleet(t, -100, "s1-1")
	length := s.graph.lengths[laneIndex(t, s, "s1-1-out")]
	startDebris(t, s, "s1-1-out", length/2, length/2+1, 0)
	if accessOK(s, "s2") || len(s.accessRoutes) == 0 {
		t.Fatalf("no stored failure: %d entries", len(s.accessRoutes))
	}
	stored := len(s.accessRoutes)
	clone := s.Clone()
	if clone.accessRoutes != nil {
		t.Fatalf("the clone has the memo with %d entries", len(clone.accessRoutes))
	}
	if accessOK(clone, "s0") || len(clone.accessRoutes) == 0 {
		t.Fatalf("the clone has access, or stored nothing: %d entries", len(clone.accessRoutes))
	}
	if len(s.accessRoutes) != stored {
		t.Errorf("the clone wrote the memo of its source: %d entries, want %d", len(s.accessRoutes), stored)
	}
	restored, _, err := RestoreState(RestoreStateInput{FaultsOn: true, Faults: FaultSettings{EvacuationSeconds: 300}, Network: s.network, Fleet: place("s1-1"), State: s.ExportState()})
	if err != nil {
		t.Fatal(err)
	}
	if restored.accessRoutes != nil {
		t.Fatalf("the restore has the memo with %d entries", len(restored.accessRoutes))
	}
	if accessOK(restored, "s2") {
		t.Error("the restore has lost the blocked set")
	}
}

// TestAccessRoutesHideRepeat checks that a second query does no search, for the
// free-flow searches and for the station paths of a network with class
// restrictions. It also checks that the query leaves the route memo as it was.
func TestAccessRoutesHideRepeat(t *testing.T) {
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
	if s.routes != nil || s.routeOrder != nil {
		t.Errorf("the query wrote the route memo: %d entries", len(s.routes))
	}
	station := false
	for key := range s.accessRoutes {
		station = station || key.station
	}
	if !station {
		t.Fatal("the fixture stores no station path")
	}
	searches := s.routeWork.searches
	if !accessOK(s, "s2") {
		t.Fatal("the pod has no access on the repeat")
	}
	if made := s.routeWork.searches - searches; made != 0 {
		t.Errorf("the repeat made %d searches", made)
	}
}

// TestAccessRoutesOnlyForAccess checks that no other query reads or writes the
// memo: not a query outside a view, and not a query under another view, which
// the refusal oracle of the emergency station choice covers.
func TestAccessRoutesOnlyForAccess(t *testing.T) {
	t.Parallel()
	poisoned := func(t *testing.T) (*Simulation, routeKey) {
		t.Helper()
		s := altLineFleet(t, -100, "s1-1")
		blockLanes(t, s, "s3-through")
		key := routeKey{from: "s1-1", to: "s2-entry", class: routeClass(LegacyClass)}
		s.accessRoutes = map[routeKey]routeResult{key: {err: ErrUnreachable}}
		return s, key
	}
	check := func(t *testing.T, s *Simulation, key routeKey) {
		t.Helper()
		if _, err := s.routeForClass(key.from, key.to, LegacyClass); err != nil {
			t.Errorf("the query read the memo: %v", err)
		}
		if _, err := s.routeForClass("s1-1", "s3-entry", LegacyClass); err != nil {
			t.Fatal(err)
		}
		if len(s.accessRoutes) != 1 {
			t.Errorf("the query wrote the memo: %d entries", len(s.accessRoutes))
		}
	}
	t.Run("no view", func(t *testing.T) {
		t.Parallel()
		s, key := poisoned(t)
		check(t, s, key)
	})
	t.Run("another view", func(t *testing.T) {
		t.Parallel()
		s, key := poisoned(t)
		defer s.leaveRouteView(s.enterRouteView(s.findVehicle("01")))
		check(t, s, key)
	})
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
		if s.routeView != outer || outer.access {
			t.Error("the nested query changed the outer view")
		}
		if s.accessRoutes != nil {
			t.Errorf("a query under the outer view wrote the memo: %d entries", len(s.accessRoutes))
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

// TestAccessRoutesNoBatch checks that the view of pickupAccess makes no batch of
// a station with more than one berth. The batch of a view is lost, and it costs
// more than the single searches that follow it.
func TestAccessRoutesNoBatch(t *testing.T) {
	t.Parallel()
	s := altLineFleet(t, -100, "s1-1")
	blockLanes(t, s, "s3-through")
	v := s.findVehicle("01")
	berths := []Berth{{ID: "a", Node: "s2-entry"}, {ID: "b", Node: "s3-entry"}}
	previous := s.enterRouteView(v)
	s.routeView.access = true
	s.cacheStationRoutesForClass("s1-1", berths, LegacyClass)
	s.leaveRouteView(previous)
	if s.accessRoutes != nil {
		t.Errorf("the batch stored %d entries", len(s.accessRoutes))
	}
}

// TestAccessRoutesLimit checks that a full memo starts again.
func TestAccessRoutesLimit(t *testing.T) {
	t.Parallel()
	s := altLineFleet(t, -100, "s1-1")
	for index := range routeCacheLimit {
		s.noteAccessRoute(routeKey{from: strconv.Itoa(index)}, routeResult{})
	}
	if len(s.accessRoutes) != routeCacheLimit {
		t.Fatalf("the memo has %d entries", len(s.accessRoutes))
	}
	s.noteAccessRoute(routeKey{from: "last"}, routeResult{})
	if len(s.accessRoutes) != 1 {
		t.Errorf("a full memo has %d entries after one more", len(s.accessRoutes))
	}
}
