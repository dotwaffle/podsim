package sim

import (
	"math"
	"slices"
	"testing"
)

// TestStationPickupBoundsIndex compares the prepared bounds of each station
// with a new reverse search. A network with banks uses the bank bounds. In
// the networks with banks, some bank bounds differ from the plain bounds.
func TestStationPickupBoundsIndex(t *testing.T) {
	t.Parallel()
	for name, network := range map[string]Network{"example": Example(), "bank entry cost": bankEntryCostNetwork(), "no candidate": missNetwork(8, 5, 16)} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p, err := PrepareNetwork(network)
			if err != nil {
				t.Fatal(err)
			}
			// The prepared network holds each bound before any read, so no
			// step or concurrent simulation writes the shared table.
			for _, station := range p.network.Stations {
				if p.pickupBounds[station.ID] == nil {
					t.Fatalf("%s: the prepared network has no bounds", station.ID)
				}
			}
			s := &Simulation{networkIndexes: &p.networkIndexes}
			banks, differ := len(s.graph.banks.banks) > 0, false
			if banks != (name != "example") {
				t.Fatalf("network has banks=%v", banks)
			}
			for _, station := range s.network.Stations {
				want := s.graph.berthTravelBounds(station.Berths)
				if banks {
					bank := s.bankTravelBounds(station)
					differ = differ || !slices.Equal(bank, want)
					want = bank
				}
				if got := s.stationPickupBounds(station.ID); !slices.Equal(got, want) {
					t.Fatalf("%s: the prepared bounds differ from the reverse search", station.ID)
				}
			}
			if banks && !differ {
				t.Fatal("the bank bounds equal the plain bounds")
			}
			if got := s.stationPickupBounds("missing"); got != nil || len(s.pickupBounds) != len(s.network.Stations) {
				t.Fatal("the index has bounds of an unknown station")
			}
		})
	}
}

// TestStationPickupBoundsShared checks that the simulations and clones of
// one prepared network read one table, and that a graph rebuild replaces it.
func TestStationPickupBoundsShared(t *testing.T) {
	t.Parallel()
	p, err := PrepareNetwork(Example())
	if err != nil {
		t.Fatal(err)
	}
	s, err := p.NewFleet([]Placement{{ID: "01", StationID: "harbor"}})
	if err != nil {
		t.Fatal(err)
	}
	other, err := p.NewFleet([]Placement{{ID: "01", StationID: "garden"}})
	if err != nil {
		t.Fatal(err)
	}
	original := s.stationPickupBounds("market")
	s.Reset()
	for name, sim := range map[string]*Simulation{"reset": s, "other fleet": other, "clone": s.Clone()} {
		if got := sim.stationPickupBounds("market"); &got[0] != &original[0] {
			t.Fatalf("%s: the bounds are not shared", name)
		}
	}
	clone := s.Clone()
	detachIndexes(clone)
	clone.network.Nodes = append(slices.Clone(clone.network.Nodes), Node{ID: "disconnected"})
	clone.ensureNetworkIndexes()
	rebuilt := clone.stationPickupBounds("market")
	if len(rebuilt) != len(original)+1 || !slices.Equal(rebuilt[:len(original)], original) || !math.IsInf(rebuilt[len(original)], 1) {
		t.Fatal("graph rebuild did not replace the bounds")
	}
	if got := s.stationPickupBounds("market"); &got[0] != &original[0] || len(got) != len(original) {
		t.Fatal("graph rebuild of a clone changed the bounds of its source")
	}
}

func BenchmarkStationPickupBounds(b *testing.B) {
	network := Example()
	graph := newRouteGraph(network)
	station := network.Stations[0]
	b.ReportAllocs()
	for b.Loop() {
		graph.berthTravelBounds(station.Berths)
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
