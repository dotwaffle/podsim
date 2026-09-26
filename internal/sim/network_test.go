package sim

import (
	"fmt"
	"math"
	"reflect"
	"slices"
	"testing"
)

// routeSearchNetworks returns networks for the route search tests. The grid
// has many routes with the same cost, so the tests reach the tie rules. The
// duplicate network is not valid. Network.Route does not validate, so the
// search must give the same result for it.
func routeSearchNetworks() map[string]Network {
	duplicate := Network{
		Nodes: []Node{
			{ID: "a", Position: Point{0, 0}},
			{ID: "b", Position: Point{100, 0}},
			{ID: "a", Position: Point{0, 100}},
			{ID: "c", Position: Point{100, 100}},
		},
		Lanes: []Lane{
			{ID: "ab", From: "a", To: "b", SpeedLimit: 10},
			{ID: "bc", From: "b", To: "c", SpeedLimit: 10},
			{ID: "ca", From: "c", To: "a", SpeedLimit: 10},
			{ID: "cb", From: "c", To: "b", SpeedLimit: 5},
			{ID: "bx", From: "b", To: "missing", SpeedLimit: 10},
		},
	}
	return map[string]Network{
		"example":   Example(),
		"ladder":    ladderNetwork(),
		"grid":      gridNetwork(6),
		"duplicate": duplicate,
	}
}

// gridNetwork returns size by size nodes with lanes in both directions
// between neighbors. All lanes have the same length.
func gridNetwork(size int) Network {
	var network Network
	id := func(x, y int) string { return fmt.Sprintf("n-%d-%d", x, y) }
	for y := range size {
		for x := range size {
			network.Nodes = append(network.Nodes, Node{ID: id(x, y), Position: Point{float64(x) * 100, float64(y) * 100}})
		}
	}
	link := func(from, to string, speed float64) {
		network.Lanes = append(network.Lanes, Lane{ID: from + "-" + to, From: from, To: to, SpeedLimit: speed})
	}
	for y := range size {
		for x := range size {
			speed := []float64{14, 14, 10}[(x+y)%3]
			if x+1 < size {
				link(id(x, y), id(x+1, y), speed)
				link(id(x+1, y), id(x, y), speed)
			}
			if y+1 < size {
				link(id(x, y), id(x, y+1), speed)
				link(id(x, y+1), id(x, y), speed)
			}
		}
	}
	return network
}

// referenceRoute is the route search that looks up the node ID of each lane
// end. routeIndexed must give the same result.
func referenceRoute(n Network, input networkRouteInput, graph routeGraph) ([]Lane, error) {
	from, ok := graph.nodes[input.from]
	if !ok {
		return nil, fmt.Errorf("unknown origin %q", input.from)
	}
	to, ok := graph.nodes[input.to]
	if !ok {
		return nil, fmt.Errorf("unknown destination %q", input.to)
	}
	distance := make([]float64, len(n.Nodes))
	previous := make([]int, len(n.Nodes))
	visited := make([]bool, len(n.Nodes))
	for i := range distance {
		distance[i], previous[i] = math.Inf(1), -1
	}
	distance[from] = 0
	queue := routeQueue{{node: from}}
	for len(queue) > 0 {
		item := queue.pop()
		if visited[item.node] || item.distance != distance[item.node] {
			continue
		}
		if item.node == to {
			break
		}
		visited[item.node] = true
		for _, laneIndex := range graph.outgoing[item.node] {
			lane := n.Lanes[laneIndex]
			if lane.To != input.from && lane.To != input.to && input.forbidden[lane.To] {
				continue
			}
			next := graph.nodes[lane.To]
			extra := 0.0
			if laneIndex < len(input.extraCost) {
				extra = input.extraCost[laneIndex]
			}
			candidate := item.distance + graph.lengths[laneIndex]/lane.SpeedLimit + extra
			if candidate < distance[next] {
				distance[next], previous[next] = candidate, laneIndex
				queue.push(routeQueueItem{node: next, distance: candidate})
			}
		}
	}
	if math.IsInf(distance[to], 1) {
		return nil, ErrUnreachable
	}
	var route []Lane
	for current := to; current != from; {
		laneIndex := previous[current]
		if laneIndex < 0 {
			return nil, ErrUnreachable
		}
		lane := n.Lanes[laneIndex]
		route = append(route, lane)
		current = graph.nodes[lane.From]
	}
	slices.Reverse(route)
	return route, nil
}

// referenceNearest is the nearest goal search that looks up the node ID of
// each lane end. nearestIndexed must give the same result.
func referenceNearest(n Network, input nearestInput, graph routeGraph) (int, bool) {
	from, ok := graph.nodes[input.from]
	if !ok {
		return 0, false
	}
	distance := make([]float64, len(n.Nodes))
	visited := make([]bool, len(n.Nodes))
	for i := range distance {
		distance[i] = math.Inf(1)
	}
	distance[from] = 0
	best, bestDistance := -1, math.Inf(1)
	queue := routeQueue{{node: from}}
	for len(queue) > 0 {
		item := queue.pop()
		if item.distance > bestDistance {
			break
		}
		if visited[item.node] || item.distance != distance[item.node] {
			continue
		}
		visited[item.node] = true
		if rank := input.rank[item.node]; rank >= 0 {
			if best < 0 || rank < input.rank[best] {
				best, bestDistance = item.node, item.distance
			}
			continue
		}
		for _, laneIndex := range graph.outgoing[item.node] {
			next := graph.nodes[n.Lanes[laneIndex].To]
			extra := 0.0
			if laneIndex < len(input.extraCost) {
				extra = input.extraCost[laneIndex]
			}
			candidate := item.distance + graph.lengths[laneIndex]/n.Lanes[laneIndex].SpeedLimit + extra
			if candidate < distance[next] {
				distance[next] = candidate
				queue.push(routeQueueItem{node: next, distance: candidate})
			}
		}
	}
	return best, best >= 0
}

// routeSearchCosts returns extra lane costs for the route search tests. Some
// costs are equal, and the short list leaves the last lanes without a cost.
func routeSearchCosts(n Network) map[string][]float64 {
	full := make([]float64, len(n.Lanes))
	for index := range full {
		full[index] = float64(index%3) * 2
	}
	return map[string][]float64{"none": nil, "full": full, "short": full[:len(full)/2]}
}

func TestRouteGraphEdgesMatchLaneEnds(t *testing.T) {
	t.Parallel()
	for name, network := range routeSearchNetworks() {
		graph := newRouteGraph(network)
		if len(graph.edges) != len(network.Lanes) {
			t.Fatalf("%s: %d edges for %d lanes", name, len(graph.edges), len(network.Lanes))
		}
		for from, lanes := range graph.outgoing {
			for _, index := range lanes {
				lane := network.Lanes[index]
				want := routeEdge{from: graph.nodes[lane.From], to: graph.nodes[lane.To], seconds: graph.lengths[index] / lane.SpeedLimit}
				if graph.edges[index] != want || want.from != from {
					t.Fatalf("%s: lane %s edge = %+v, want %+v from node %d", name, lane.ID, graph.edges[index], want, from)
				}
			}
		}
	}
}

func TestRouteSearchMatchesNodeIDLookups(t *testing.T) {
	t.Parallel()
	for name, network := range routeSearchNetworks() {
		graph := newRouteGraph(network)
		ids := []string{"unknown"}
		for _, node := range network.Nodes {
			ids = append(ids, node.ID)
		}
		forbidden := map[string]map[string]bool{"none": nil, "stations": network.stationForbidden(), "grid": {"n-1-1": true, "n-2-3": true, "b": true}}
		routes := 0
		for forbiddenName, forbid := range forbidden {
			for costName, costs := range routeSearchCosts(network) {
				for _, from := range ids {
					for _, to := range ids {
						input := networkRouteInput{from: from, to: to, forbidden: forbid, extraCost: costs}
						want, wantErr := referenceRoute(network, input, graph)
						got, gotErr := network.routeIndexed(input, graph)
						if !reflect.DeepEqual(got, want) || fmt.Sprint(gotErr) != fmt.Sprint(wantErr) {
							t.Fatalf("%s %s %s: route %s to %s = %v, %v, want %v, %v", name, forbiddenName, costName, from, to, got, gotErr, want, wantErr)
						}
						if len(want) > 1 {
							routes++
						}
					}
				}
			}
		}
		if routes == 0 {
			t.Fatalf("%s: no route has more than one lane", name)
		}
	}
}

func TestNearestSearchMatchesNodeIDLookups(t *testing.T) {
	t.Parallel()
	for name, network := range routeSearchNetworks() {
		graph := newRouteGraph(network)
		for costName, costs := range routeSearchCosts(network) {
			for step := 2; step <= 5; step++ {
				rank := make([]int, len(network.Nodes))
				for index := range rank {
					rank[index] = -1
					if index%step == 0 {
						rank[index] = index % 3
					}
				}
				for _, node := range append(slices.Clone(network.Nodes), Node{ID: "unknown"}) {
					input := nearestInput{from: node.ID, rank: rank, extraCost: costs}
					wantNode, wantOK := referenceNearest(network, input, graph)
					gotNode, gotOK := network.nearestIndexed(input, graph)
					if gotNode != wantNode || gotOK != wantOK {
						t.Fatalf("%s %s step %d: nearest from %s = %d, %v, want %d, %v", name, costName, step, node.ID, gotNode, gotOK, wantNode, wantOK)
					}
				}
			}
		}
	}
}
