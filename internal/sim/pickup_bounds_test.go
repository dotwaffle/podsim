package sim

import (
	"math"
	"testing"
)

func TestBerthTravelBoundsMatchForwardSearch(t *testing.T) {
	t.Parallel()
	network := Example()
	network.Nodes = append(network.Nodes, Node{ID: "disconnected"})
	graph := newRouteGraph(network)
	for _, station := range network.Stations {
		t.Run(station.ID, func(t *testing.T) {
			t.Parallel()
			bounds := graph.berthTravelBounds(station.Berths)
			for index, node := range network.Nodes {
				want := math.Inf(1)
				for _, berth := range station.Berths {
					route, err := network.routeIndexed(networkRouteInput{from: node.ID, to: berth.Node}, graph)
					if err != nil {
						continue
					}
					seconds := 0.0
					for _, lane := range route {
						seconds += graph.edges[graph.lanes[lane.ID]].seconds
					}
					want = min(want, seconds)
				}
				if bounds[index] != want && math.Abs(bounds[index]-want) > 1e-9 {
					t.Fatalf("%s to %s: bound=%g forward=%g", node.ID, station.ID, bounds[index], want)
				}
			}
		})
	}
}

func TestPickupBoundNeverExcludesBetterCandidate(t *testing.T) {
	t.Parallel()
	var idle, moving int
	holdScenario{rule: FinishingPodWaitCurrent}.run(t, func(s *Simulation) {
		assigned := waitingAssignments(s)
		for _, station := range s.network.Stations {
			bounds := s.graph.berthTravelBounds(station.Berths)
			for i := range s.vehicles {
				v := &s.vehicles[i]
				route, _, ok := s.pickupRouteWithAssignments(pickupRouteInput{pod: v, station: station.ID, assigned: assigned})
				if !ok {
					continue
				}
				bound, exact := s.pickupBound(v, bounds), s.pickupSeconds(v, route)
				if pickupCannotImprove(bound, exact) {
					t.Fatalf("tick %d pod %s: bound %g exceeds pickup estimate %g", s.tick, v.Pod.ID, bound, exact)
				}
				if v.Pod.Activity == Idle {
					idle++
				} else {
					moving++
				}
			}
		}
	})
	if idle == 0 || moving == 0 {
		t.Fatalf("missing bound coverage: idle=%d moving=%d", idle, moving)
	}
}

func TestPickupBoundRetainsTiesAndRoundingMargin(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		bound, best float64
		prune       bool
	}{
		{name: "tie", bound: 100, best: 100},
		{name: "rounding", bound: 100.0000001, best: 100},
		{name: "better", bound: 99, best: 100},
		{name: "slower", bound: 101, best: 100, prune: true},
		{name: "unreachable", bound: math.Inf(1), best: 100, prune: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := pickupCannotImprove(tc.bound, tc.best); got != tc.prune {
				t.Fatalf("prune=%v want %v", got, tc.prune)
			}
		})
	}
}
