package sim

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"
	"testing"
)

// intermediateBerthNetwork has a fast shortcut through berth b. The
// direct route to c is slower, and every route to d crosses another berth.
func intermediateBerthNetwork() Network {
	return Network{
		Nodes: []Node{
			{ID: "a", Position: Point{}},
			{ID: "b", Position: Point{X: 1}},
			{ID: "c", Position: Point{X: 2}},
			{ID: "d", Position: Point{X: 3}},
		},
		Lanes: []Lane{
			{ID: "ab", From: "a", To: "b", SpeedLimit: 1},
			{ID: "bc", From: "b", To: "c", SpeedLimit: 1},
			{ID: "ac", From: "a", To: "c", SpeedLimit: 0.2},
			{ID: "bd", From: "b", To: "d", SpeedLimit: 1},
			{ID: "cd", From: "c", To: "d", SpeedLimit: 2},
		},
		Stations: []Station{
			{ID: "origin", Berths: []Berth{{ID: "a", Node: "a"}, {ID: "b", Node: "b"}}},
			{ID: "destination", Berths: []Berth{{ID: "c", Node: "c"}, {ID: "d", Node: "d"}}},
		},
	}
}

// referencePreferredBerthRoute removes incoming lanes to every
// intermediate berth, then uses the public unrestricted route search.
func referencePreferredBerthRoute(n Network, from, to string) ([]Lane, error) {
	forbidden := make(map[string]bool)
	for _, station := range n.Stations {
		for _, berth := range station.Berths {
			if berth.Node != from && berth.Node != to {
				forbidden[berth.Node] = true
			}
		}
	}
	filtered := n
	filtered.Lanes = slices.DeleteFunc(slices.Clone(n.Lanes), func(lane Lane) bool { return forbidden[lane.To] })
	route, err := filtered.Route(from, to)
	if errors.Is(err, ErrUnreachable) {
		return n.Route(from, to)
	}
	return route, err
}

func TestSimulationAvoidsIntermediateSiblingBerths(t *testing.T) {
	t.Parallel()
	for _, owner := range []string{"origin", "destination", "third"} {
		t.Run(owner, func(t *testing.T) {
			t.Parallel()
			network := intermediateBerthNetwork()
			b := network.Stations[0].Berths[1]
			network.Stations[0].Berths = network.Stations[0].Berths[:1]
			if owner == "third" {
				network.Stations = append(network.Stations, Station{ID: owner, Berths: []Berth{b}})
			} else {
				for index := range network.Stations {
					if network.Stations[index].ID == owner {
						network.Stations[index].Berths = append(network.Stations[index].Berths, b)
					}
				}
			}
			s := &Simulation{network: network}
			route, err := s.route("a", "c")
			if err != nil || len(route) != 1 || route[0].ID != "ac" {
				t.Fatalf("preferred route = %v, %v, want ac", route, err)
			}
			legacy, err := network.Route("a", "c")
			if err != nil || len(legacy) != 2 || legacy[0].ID != "ab" {
				t.Fatalf("public shortest route changed: %v, %v", legacy, err)
			}
		})
	}
}

func TestPreferredDirectAndBatchedRoutesAgree(t *testing.T) {
	t.Parallel()
	for name, network := range map[string]Network{
		"shortcuts":  intermediateBerthNetwork(),
		"legacy":     ladderNetwork(),
		"equal-cost": gridNetwork(4),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var targets []Berth
			for _, node := range network.Nodes {
				targets = append(targets, Berth{ID: node.ID, Node: node.ID})
			}
			targets = append(targets, targets[0], Berth{ID: "missing", Node: "missing"})
			for _, from := range append(slices.Clone(network.Nodes), Node{ID: "missing"}) {
				for _, batched := range []bool{false, true} {
					s := &Simulation{network: network}
					if batched {
						s.cacheStationRoutes(from.ID, targets)
					}
					for _, target := range targets {
						want, wantErr := referencePreferredBerthRoute(network, from.ID, target.Node)
						for range 2 {
							got, gotErr := s.route(from.ID, target.Node)
							if !reflect.DeepEqual(got, want) || fmt.Sprint(gotErr) != fmt.Sprint(wantErr) {
								t.Fatalf("batched %v, %s to %s: got %v, %v, want %v, %v", batched, from.ID, target.Node, got, gotErr, want, wantErr)
							}
						}
					}
				}
			}
		})
	}
}

func TestNearestFreeBerthUsesPerDestinationRouteCost(t *testing.T) {
	t.Parallel()
	s := &Simulation{
		network: intermediateBerthNetwork(),
		owners:  map[resource]string{{kind: berthResource, id: "b"}: "other"},
	}
	v := &vehicle{Pod: Pod{ID: "moving", Activity: Traveling}}
	berth, station, ok := s.nearestFreeBerth(v, "a")
	// c has a preferred cost of ten seconds. d has no preferred path, so
	// its legacy cost of 2.5 seconds wins despite passing through c.
	if !ok || berth.ID != "d" || station != "destination" {
		t.Fatalf("nearest berth = %v, %s, %v, want d", berth, station, ok)
	}
}

func TestPreferredNearestCostsMatchPerTargetRoutes(t *testing.T) {
	t.Parallel()
	for name, network := range map[string]Network{
		"shortcuts": intermediateBerthNetwork(),
		"legacy":    ladderNetwork(),
		"ties":      gridNetwork(4),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			graph := newRouteGraph(network)
			for _, from := range append(slices.Clone(network.Nodes), Node{ID: "missing"}) {
				for _, stride := range []int{1, 2, len(network.Nodes) + 1} {
					rank := make([]int, len(network.Nodes))
					want, wantSeconds := -1, 0.0
					for node := range rank {
						rank[node] = -1
						if (node+1)%stride != 0 {
							continue
						}
						// Reverse rank makes equal-cost choices independent of
						// network order, including the grid's equal-cost paths.
						rank[node] = len(rank) - node
						route, err := referencePreferredBerthRoute(network, from.ID, network.Nodes[node].ID)
						if err != nil {
							continue
						}
						seconds := 0.0
						for _, lane := range route {
							seconds += graph.edges[graph.lanes[lane.ID]].seconds
						}
						if want < 0 || seconds < wantSeconds || seconds == wantSeconds && rank[node] < rank[want] {
							want, wantSeconds = node, seconds
						}
					}
					got, ok := network.preferredNearestIndexed(preferredNearestInput{from: from.ID, rank: rank}, graph)
					if ok != (want >= 0) || ok && got != want {
						t.Fatalf("from %s stride %d: nearest %d, %v, want %d", from.ID, stride, got, ok, want)
					}
				}
			}
		})
	}
}

func TestPreferredRoutingKeepsLegacyBerthCrossingAndRestore(t *testing.T) {
	t.Parallel()
	network := Example()
	for index := range network.Lanes {
		if network.Lanes[index].ID == "approach-branch" {
			// The authored road reaches Garden through its berth. Garden
			// still has valid local arrival, departure, and through lanes.
			network.Lanes[index].To = "garden-berth"
		}
	}
	fleet := []Placement{{ID: "01", StationID: "harbor", BerthID: "harbor-1"}}
	s, err := NewFleet(network, fleet)
	if err != nil {
		t.Fatal(err)
	}
	want, err := s.network.Route("harbor-berth", "market-berth")
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.route("harbor-berth", "market-berth")
	if err != nil || !slices.Equal(got, want) || !slices.ContainsFunc(got, func(lane Lane) bool { return lane.To == "garden-berth" }) {
		t.Fatalf("legacy path was lost or changed: %v, %v", got, err)
	}
	if journeyErr := s.RequestJourney("01", "market"); journeyErr != nil {
		t.Fatal(journeyErr)
	}
	stepUntil(t, s, "pod enters the legacy berth crossing", func() bool {
		return s.findVehicle("01").Pod.LaneID == "approach-branch"
	})
	saved := s.ExportState()
	var result RestoreResult
	s, result, err = RestoreState(RestoreStateInput{Network: network, Fleet: fleet, State: saved})
	if err != nil || result.Tier != RestorePhysical || len(result.Demoted) != 0 {
		t.Fatalf("physical restore: result %+v, error %v", result, err)
	}
	restored := s.ExportState()
	if !reflect.DeepEqual(saved.Pods, restored.Pods) {
		t.Fatal("physical restore changed the saved legacy route or assignment")
	}
	stepUntil(t, s, "legacy journey completes", func() bool { return s.completed == 1 })
}

func TestPreferredRoutingRestoresExistingIntermediateBerthRoute(t *testing.T) {
	t.Parallel()
	network := Example()
	for index := range network.Lanes {
		switch network.Lanes[index].ID {
		case "garden-through", "bypass-merge":
			network.Lanes[index].SpeedLimit = 0.1
		}
	}
	fleet := []Placement{{ID: "01", StationID: "harbor", BerthID: "harbor-1"}}
	s, err := NewFleet(network, fleet)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := s.network.Route("harbor-berth", "market-entry")
	if err != nil || !slices.ContainsFunc(legacy, func(lane Lane) bool { return lane.To == "garden-berth" }) {
		t.Fatalf("old shortest path has no intermediate berth: %v, %v", legacy, err)
	}
	preferred, err := s.route("harbor-berth", "market-entry")
	if err != nil || slices.ContainsFunc(preferred, func(lane Lane) bool { return lane.To == "garden-berth" }) {
		t.Fatalf("new preferred path crosses a berth: %v, %v", preferred, err)
	}
	if journeyErr := s.RequestJourney("01", "market"); journeyErr != nil {
		t.Fatal(journeyErr)
	}
	v := s.findVehicle("01")
	// Install the valid route an earlier build would assign, before the
	// pod starts moving or reserves any of it.
	s.setVehicleRoute(v, legacy)
	stepUntil(t, s, "pod enters its old intermediate-berth route", func() bool {
		return v.Pod.LaneID == "garden-out"
	})
	saved := s.ExportState()
	var result RestoreResult
	s, result, err = RestoreState(RestoreStateInput{Network: network, Fleet: fleet, State: saved})
	if err != nil || result.Tier != RestorePhysical || len(result.Demoted) != 0 {
		t.Fatalf("old physical route restore: result %+v, error %v", result, err)
	}
	if restored := s.ExportState(); !reflect.DeepEqual(saved.Pods, restored.Pods) {
		t.Fatal("restore rerouted or changed an existing physical journey")
	}
	stepUntil(t, s, "old routed journey completes", func() bool { return s.completed == 1 })
}

func TestGuardedRefillUsesPreferredReach(t *testing.T) {
	t.Parallel()
	for _, alternative := range []bool{false, true} {
		t.Run(strconv.FormatBool(alternative), func(t *testing.T) {
			t.Parallel()
			network := lineNetwork(lineStations(2, 2, 2, 2))
			for index := range network.Lanes {
				if network.Lanes[index].ID == "s0-through" {
					network.Lanes[index].SpeedLimit = 0.1
				}
			}
			// The second source has a longer unrestricted path but a preferred
			// path within the reach limit. The first source exceeds that limit.
			network.Lanes = append(network.Lanes, Lane{ID: "alternative", From: "s2-exit", To: "s1-entry", SpeedLimit: 5})
			fleet := place("p-1")
			if alternative {
				fleet = place("p-1", "s2-1", "s2-2")
			}
			s, err := NewFleet(network, fleet)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.SetPositioning(PositioningGuarded); err != nil {
				t.Fatal(err)
			}
			if err := s.SetDemandWeights(map[string]float64{"s1": 1}); err != nil {
				t.Fatal(err)
			}
			openGate(s)
			s.positionGuarded()
			want := []string(nil)
			if alternative {
				want = []string{"02"}
			}
			if got := moved(s); !slices.Equal(got, want) {
				t.Fatalf("moved %v, want %v", got, want)
			}
		})
	}
}

func TestGuardedBumpUsesPreferredReach(t *testing.T) {
	t.Parallel()
	network := lineNetwork(lineStations(2, 2, 2, 2))
	for index := range network.Lanes {
		if network.Lanes[index].ID == "s0-through" {
			network.Lanes[index].SpeedLimit = 0.1
		}
	}
	network.Lanes = append(network.Lanes, Lane{ID: "alternative", From: "p-exit", To: "s2-entry", SpeedLimit: 6})
	s, err := NewFleet(network, place("p-1", "s0-1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetPositioning(PositioningGuarded); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDemandWeights(map[string]float64{"s1": 1, "s2": 1}); err != nil {
		t.Fatal(err)
	}
	openGate(s)
	blocker := s.findVehicle("01")
	if !s.guardedBumpToDeficit(guardedBump{blocker: blocker, from: "p-1", gate: s.guardedGate()}) || blocker.destination.ID != "s2-1" {
		t.Fatalf("bumped to %s, want reachable s2-1", blocker.destination.ID)
	}
}

func TestGuardedSourcesMatchPreferredRouteCosts(t *testing.T) {
	t.Parallel()
	for name, network := range map[string]Network{"shortcuts": intermediateBerthNetwork(), "ties": gridNetwork(4), "legacy": ladderNetwork()} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := &Simulation{network: network}
			s.ensureNetworkIndexes()
			rank := make([]int, len(network.Nodes))
			for node := range rank {
				rank[node] = len(rank) - node
			}
			for _, goal := range append(slices.Clone(network.Nodes), Node{ID: "missing"}) {
				best, seconds := -1, 0.0
				for from := range rank {
					route, err := referencePreferredBerthRoute(network, network.Nodes[from].ID, goal.ID)
					if err != nil {
						continue
					}
					cost := 0.0
					for _, lane := range route {
						cost += s.graph.edges[s.graph.lanes[lane.ID]].seconds
					}
					if cost > guardedReachSeconds {
						continue
					}
					if best < 0 || cost < seconds || cost == seconds && rank[from] < rank[best] {
						best, seconds = from, cost
					}
				}
				got, ok := network.preferredNearestIndexed(preferredNearestInput{from: goal.ID, rank: rank, limit: guardedReachSeconds, reverse: true}, s.graph)
				if ok != (best >= 0) || ok && got != best {
					t.Fatalf("to %s: got %d, %v, want %d", goal.ID, got, ok, best)
				}
			}
		})
	}
}

func BenchmarkPreferredNearest(b *testing.B) {
	berths := make([]int, 100)
	for index := range berths {
		berths[index] = 4
	}
	network := lineNetwork(lineStations(0, berths...))
	graph := newRouteGraph(network)
	rank := make([]int, len(network.Nodes))
	for node := range rank {
		rank[node] = -1
		if graph.berthStations[node] >= 0 {
			rank[node] = node
		}
	}
	for _, preferred := range []bool{false, true} {
		b.Run(strconv.FormatBool(preferred), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if preferred {
					network.preferredNearestIndexed(preferredNearestInput{from: "s50-exit", rank: rank}, graph)
				} else {
					network.nearestIndexed(nearestInput{from: "s50-exit", rank: rank}, graph)
				}
			}
		})
	}
}

func BenchmarkGuardedPreferredSource(b *testing.B) {
	berths := make([]int, 100)
	for index := range berths {
		berths[index] = 4
	}
	network := lineNetwork(lineStations(0, berths...))
	graph := newRouteGraph(network)
	rank := make([]int, len(network.Nodes))
	for node := range rank {
		rank[node] = -1
		if graph.berthStations[node] >= 0 {
			rank[node] = node
		}
	}
	b.ReportAllocs()
	for b.Loop() {
		network.preferredNearestIndexed(preferredNearestInput{from: "s50-1", rank: rank, limit: guardedReachSeconds, reverse: true}, graph)
	}
}

func TestPreferredReverseTargetsKeepDirectedCosts(t *testing.T) {
	t.Parallel()
	for name, network := range map[string]Network{"shortcuts": intermediateBerthNetwork(), "legacy": ladderNetwork(), "ties": gridNetwork(4)} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			graph := newRouteGraph(network)
			var sources []string
			for _, node := range network.Nodes {
				sources = append(sources, node.ID)
			}
			sources = append(sources, "missing")
			for _, goal := range network.Nodes {
				targets := network.preferredTargets(routeTargetsInput{from: goal.ID, to: sources, reverse: true}, graph)
				routes := network.routesFromTargets(targets, graph)
				for index, source := range sources {
					want, err := referencePreferredBerthRoute(network, source, goal.ID)
					got := routes[index]
					if fmt.Sprint(got.err) != fmt.Sprint(err) {
						// Unknown endpoints have direction-specific error text.
						if source != "missing" || got.err == nil || err == nil {
							t.Fatalf("%s to %s: got %v, want %v", source, goal.ID, got.err, err)
						}
					}
					if err != nil {
						continue
					}
					cost, wantCost, at := 0.0, 0.0, source
					for _, lane := range got.lanes {
						if lane.From != at {
							t.Fatalf("reverse tree has disconnected travel route: %v", got.lanes)
						}
						at = lane.To
						cost += graph.edges[graph.lanes[lane.ID]].seconds
					}
					for _, lane := range want {
						wantCost += graph.edges[graph.lanes[lane.ID]].seconds
					}
					if at != goal.ID || math.Abs(cost-wantCost) > 1e-9 {
						t.Fatalf("%s to %s: got cost %g ends at %s, want %g", source, goal.ID, cost, at, wantCost)
					}
				}
			}
		})
	}
}

func TestPreferredSourcePreservesForwardRounding(t *testing.T) {
	t.Parallel()
	network := Network{}
	for index := range 7 {
		network.Nodes = append(network.Nodes, Node{ID: strconv.Itoa(index), Position: Point{X: float64(index)}})
	}
	for branch, costs := range [][]float64{{180, 1e-14, 1e-14}, {1e-14, 1e-14, 180}} {
		for step, cost := range costs {
			from, to := branch*3+step, branch*3+step+1
			if step == 2 {
				to = 6
			}
			network.Lanes = append(network.Lanes, Lane{ID: fmt.Sprintf("%d-%d", from, to), From: strconv.Itoa(from), To: strconv.Itoa(to), SpeedLimit: float64(to-from) / cost})
		}
	}
	graph := newRouteGraph(network)
	rank := []int{1, -1, -1, 0, -1, -1, -1}
	// Forward addition gives source 0 exactly 180 seconds and source 3
	// slightly more. Reverse addition gives the opposite order.
	for _, limit := range []float64{0, 180, math.Nextafter(180, 0)} {
		got, ok := network.preferredNearestIndexed(preferredNearestInput{from: "6", rank: rank, limit: limit, reverse: true}, graph)
		want := 0
		if limit > 0 && limit < 180 {
			want = -1
		}
		if ok != (want >= 0) || ok && got != want {
			t.Fatalf("limit %g: got %d, %v, want %d", limit, got, ok, want)
		}
	}
}
