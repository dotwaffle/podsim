package sim

import (
	"maps"
	"slices"
	"testing"
)

// viewRoute returns the route of assignedRoute for pod v from the exit of
// s1 to the entry of s2 under a routing view of v.
func viewRoute(s *Simulation, v *vehicle) []Lane {
	defer s.leaveRouteView(s.enterRouteView(v))
	route, _ := s.assignedRoute(v, "s1-exit", "s2-entry")
	return route
}

// TestRouteViewCongestion checks the congestion costs of a routing view.
// Stored costs apply as they are, also when the refresh is due, and the
// view reads the congestion memo with them. Without stored costs, the view
// computes the costs and stores nothing.
func TestRouteViewCongestion(t *testing.T) {
	t.Parallel()
	s := altLineFleet(t, -100, "s0-1", "s3-1")
	if err := s.SetRoutingPolicy(CongestionRouting); err != nil {
		t.Fatal(err)
	}
	v := s.findVehicle("01")
	if route := viewRoute(s, v); !usesLane(route, "s1-link") || s.congestionRouteCosts != nil || len(s.congestionRoutes) != 0 {
		t.Fatalf("with computed costs, the route is %v, and the view stored %d costs", route, len(s.congestionRouteCosts))
	}
	costs := make([]float64, len(s.network.Lanes))
	costs[laneIndex(t, s, "s1-link")] = 1000
	s.congestionRouteCosts, s.congestionRoutes, s.nextCongestionRouteRefresh = costs, map[routeKey]routeResult{}, 0
	advance(s, 1)
	if route := viewRoute(s, v); !usesLane(route, "alt-in") || len(s.congestionRoutes) != 0 || s.nextCongestionRouteRefresh != 0 {
		t.Fatalf("with stored costs, the route is %v, the memo has %d entries, the refresh is at %d", route, len(s.congestionRoutes), s.nextCongestionRouteRefresh)
	}
	memo := routeResult{lanes: []Lane{s.network.Lanes[laneIndex(t, s, "s1-link")]}}
	s.congestionRoutes[routeKey{from: "s1-exit", to: "s2-entry", class: routeClass(v.Pod.Class)}] = memo
	if route := viewRoute(s, v); !sameLanes(route, memo.lanes) {
		t.Fatalf("with stored costs, the view does not read the memo: %v", route)
	}
}

// TestRouteViewPredictive checks the predictive state of a routing view.
// The history of predictiveQueues without the history of the pod gives
// the forecast, with no decay to the current tick. With no history, the
// forecast has zero history and the current sample.
func TestRouteViewPredictive(t *testing.T) {
	t.Parallel()
	s := altLineFleet(t, -40, "s0-1", "s3-1")
	if err := s.SetRoutingPolicy(PredictiveRouting); err != nil {
		t.Fatal(err)
	}
	v := s.findVehicle("01")
	if route := viewRoute(s, v); !usesLane(route, "s1-link") || s.predictiveQueues != nil {
		t.Fatalf("with no history, the route is %v, and the view stored the history", route)
	}
	link := laneIndex(t, s, "s1-link")
	queues := make([]float64, len(s.network.Lanes))
	queues[link] = 300
	history := func(id string) map[string]podQueueHistory {
		return map[string]podQueueHistory{id: {lanes: map[int]float64{link: 300}}}
	}
	for _, test := range []struct {
		owner string
		lane  string
	}{{"01", "s1-link"}, {"02", "alt-in"}} {
		s.predictiveQueues, s.predictivePodQueues, s.predictiveQueueTick = slices.Clone(queues), history(test.owner), 0
		advance(s, 10*TicksPerSecond)
		before := maps.Clone(s.predictivePodQueues[test.owner].lanes)
		if route := viewRoute(s, v); !usesLane(route, test.lane) {
			t.Fatalf("history of pod %s: the route is %v, want %s", test.owner, route, test.lane)
		}
		if s.predictiveQueueTick != 0 || s.predictiveQueues[link] != 300 || !maps.Equal(before, s.predictivePodQueues[test.owner].lanes) {
			t.Fatalf("history of pod %s: the view decayed the history", test.owner)
		}
	}
}
