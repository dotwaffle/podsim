package sim

import (
	"math"
	"slices"
	"testing"
)

func TestStationPickupBoundsCache(t *testing.T) {
	t.Parallel()
	s := newTraffic(t)
	for _, station := range s.network.Stations {
		got := s.stationPickupBounds(station.ID)
		want := s.graph.berthTravelBounds(station.Berths)
		if !slices.Equal(got, want) {
			t.Fatalf("%s: cached bounds %v, want %v", station.ID, got, want)
		}
		again := s.stationPickupBounds(station.ID)
		if &again[0] != &got[0] {
			t.Fatalf("%s: repeated lookup rebuilt the bounds", station.ID)
		}
	}
	if got := s.stationPickupBounds("missing"); got != nil || len(s.pickupBounds) != len(s.network.Stations) {
		t.Fatal("unknown station added a cache entry")
	}
}

func TestStationPickupBoundsLifecycle(t *testing.T) {
	t.Parallel()
	s := newTraffic(t)
	original := s.stationPickupBounds("market")
	s.Reset()
	if got := s.stationPickupBounds("market"); &got[0] != &original[0] {
		t.Fatal("Reset rebuilt fixed-network bounds")
	}
	clone := s.Clone()
	if clone.pickupBounds != nil {
		t.Fatal("Clone retained a mutable cache")
	}
	copied := clone.stationPickupBounds("market")
	if !slices.Equal(copied, original) || &copied[0] == &original[0] {
		t.Fatal("Clone did not rebuild independent, equal bounds")
	}
	copied[0] = -1
	if s.stationPickupBounds("market")[0] != original[0] || original[0] < 0 {
		t.Fatal("the clone changed its source's bounds")
	}
	clone.network.Nodes = append(slices.Clone(clone.network.Nodes), Node{ID: "disconnected"})
	clone.ensureNetworkIndexes()
	if clone.pickupBounds != nil {
		t.Fatal("graph rebuild retained old bounds")
	}
	rebuilt := clone.stationPickupBounds("market")
	if len(rebuilt) != len(original)+1 || !slices.Equal(rebuilt[:len(original)], original) || !math.IsInf(rebuilt[len(original)], 1) {
		t.Fatal("graph rebuild did not replace the cached bounds")
	}
}

func BenchmarkStationPickupBounds(b *testing.B) {
	for _, cached := range []bool{false, true} {
		name := "fresh"
		if cached {
			name = "cached"
		}
		b.Run(name, func(b *testing.B) {
			s := &Simulation{network: Example()}
			s.ensureNetworkIndexes()
			station, _ := s.station("market")
			s.stationPickupBounds(station.ID)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if cached {
					s.stationPickupBounds(station.ID)
				} else {
					s.graph.berthTravelBounds(station.Berths)
				}
			}
		})
	}
}

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
