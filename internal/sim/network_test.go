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

func TestRoutesSearchMatchesOneRouteSearch(t *testing.T) {
	t.Parallel()
	for name, network := range routeSearchNetworks() {
		graph := newRouteGraph(network)
		ids := []string{"unknown"}
		for _, node := range network.Nodes {
			ids = append(ids, node.ID)
		}
		// The lists take the node IDs with different steps. Thus they have
		// destinations in different orders, the origin, an unknown ID, and
		// the same ID two times.
		lists := [][]string{ids}
		for size := 1; size <= 4; size++ {
			for start := range ids {
				list := make([]string, size)
				for i := range list {
					list[i] = ids[(start+i*(start+1))%len(ids)]
				}
				lists = append(lists, list)
			}
		}
		for _, from := range ids {
			for _, to := range lists {
				got := network.routesIndexed(from, to, graph)
				if len(got) != len(to) {
					t.Fatalf("%s: %d routes for %d destinations", name, len(got), len(to))
				}
				for index, target := range to {
					want, wantErr := network.routeIndexed(networkRouteInput{from: from, to: target}, graph)
					if !reflect.DeepEqual(got[index].lanes, want) || fmt.Sprint(got[index].err) != fmt.Sprint(wantErr) {
						t.Fatalf("%s: route %s to %s in %v = %v, %v, want %v, %v", name, from, target, to, got[index].lanes, got[index].err, want, wantErr)
					}
				}
			}
		}
	}
}

func TestRouteGraphIncomingMatchesOutgoing(t *testing.T) {
	t.Parallel()
	for name, network := range routeSearchNetworks() {
		graph := newRouteGraph(network)
		if len(graph.incoming) != len(network.Nodes) {
			t.Fatalf("%s: incoming lanes for %d nodes, want %d", name, len(graph.incoming), len(network.Nodes))
		}
		outgoing, incoming := 0, 0
		for from, lanes := range graph.outgoing {
			outgoing += len(lanes)
			for _, index := range lanes {
				if to := graph.edges[index].to; slices.Index(graph.incoming[to], index) < 0 {
					t.Fatalf("%s: lane %d from node %d is not an incoming lane of node %d", name, index, from, to)
				}
			}
		}
		for to, lanes := range graph.incoming {
			incoming += len(lanes)
			for _, index := range lanes {
				if graph.edges[index].to != to {
					t.Fatalf("%s: incoming lane %d of node %d ends at node %d", name, index, to, graph.edges[index].to)
				}
			}
		}
		if incoming != outgoing {
			t.Fatalf("%s: %d incoming lanes, %d outgoing lanes", name, incoming, outgoing)
		}
	}
}

// routeCost adds the lane costs of a route in route order, or in reverse
// order. It adds each lane cost as the route search does.
func routeCost(route []Lane, costs []float64, graph routeGraph, reverse bool) float64 {
	total := 0.0
	for index := range route {
		if reverse {
			index = len(route) - 1 - index
		}
		laneIndex := graph.lanes[route[index].ID]
		extra := 0.0
		if laneIndex < len(costs) {
			extra = costs[laneIndex]
		}
		total = total + graph.edges[laneIndex].seconds + extra
	}
	return total
}

func TestNearestWithinForwardMatchesNearestIndexed(t *testing.T) {
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
					wantNode, wantOK := network.nearestIndexed(input, graph)
					gotNode, _, gotOK := network.nearestWithin(nearestWithinInput{nearestInput: input, limit: math.Inf(1)}, graph)
					if gotOK != wantOK || gotOK && gotNode != wantNode {
						t.Fatalf("%s %s step %d: nearest within from %s = %d, %v, want %d, %v", name, costName, step, node.ID, gotNode, gotOK, wantNode, wantOK)
					}
				}
			}
		}
	}
}

// TestNearestWithinCostsMatchRoutes checks the cost of one goal against the
// route between the nodes. A forward search adds the lane costs in route
// order, as the route search does, so the cost is equal. A reverse search
// adds them in the opposite order, so the cost can differ by a rounding
// error. The test also checks the limit at the cost and just below it.
func TestNearestWithinCostsMatchRoutes(t *testing.T) {
	t.Parallel()
	for name, network := range routeSearchNetworks() {
		graph := newRouteGraph(network)
		for costName, costs := range routeSearchCosts(network) {
			routes := 0
			for _, from := range network.Nodes {
				for _, to := range network.Nodes {
					route, err := network.routeIndexed(networkRouteInput{from: from.ID, to: to.ID, extraCost: costs}, graph)
					if err != nil || len(route) == 0 {
						continue
					}
					routes++
					forward, reverse := routeCost(route, costs, graph, false), routeCost(route, costs, graph, true)
					for _, search := range []struct {
						reverse    bool
						start, end string
						want       float64
					}{
						{start: from.ID, end: to.ID, want: forward},
						{reverse: true, start: to.ID, end: from.ID, want: reverse},
					} {
						rank := make([]int, len(network.Nodes))
						for index := range rank {
							rank[index] = -1
						}
						rank[graph.nodes[search.end]] = 0
						input := nearestWithinInput{from: search.start, rank: rank, extraCost: costs, reverse: search.reverse}
						input.limit = math.Inf(1)
						node, cost, ok := network.nearestWithin(input, graph)
						if !ok || node != graph.nodes[search.end] || math.Abs(cost-search.want) > 1e-9*max(1, search.want) ||
							!search.reverse && cost != search.want {
							t.Fatalf("%s %s: reverse %v from %s to %s = %d, %v, %v, want node %d cost %v",
								name, costName, search.reverse, search.start, search.end, node, cost, ok, graph.nodes[search.end], search.want)
						}
						input.limit = cost
						if _, _, ok := network.nearestWithin(input, graph); !ok {
							t.Fatalf("%s %s: reverse %v from %s to %s: no goal at a limit equal to cost %v", name, costName, search.reverse, search.start, search.end, cost)
						}
						input.limit = math.Nextafter(cost, 0)
						if _, _, ok := network.nearestWithin(input, graph); ok {
							t.Fatalf("%s %s: reverse %v from %s to %s: a goal past the limit %v", name, costName, search.reverse, search.start, search.end, input.limit)
						}
					}
				}
			}
			if routes == 0 {
				t.Fatalf("%s %s: no routes", name, costName)
			}
		}
	}
}

// routesIndexed returns the routes from node from to each node in to. Each
// route and error is the same as from routeIndexed with no forbidden nodes
// and no extra cost. One search gives all the routes. The lane costs must
// not be negative, as in a valid network.
//
// The search takes nodes from the queue in the same order as routeIndexed.
// It continues past each destination until it takes the last one. A lane
// cost that is not negative cannot make the route to a node shorter after
// the search takes the node. Thus the route to a destination does not
// change after the search takes the destination.
func (n Network) routesIndexed(from string, to []string, graph routeGraph) []routeResult {
	return n.routesFromTargets(n.routeTargets(routeTargetsInput{from: from, to: to}, graph), graph)
}

// nearestIndexed returns the index of the goal node with the lowest route
// cost from input.from. The cost is the same as in routeIndexed without
// forbidden nodes. It reports false when no goal is reachable.
func (n Network) nearestIndexed(input nearestInput, graph routeGraph) (int, bool) {
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
			edge := graph.edges[laneIndex]
			extra := 0.0
			if laneIndex < len(input.extraCost) {
				extra = input.extraCost[laneIndex]
			}
			candidate := item.distance + edge.seconds + extra
			if candidate < distance[edge.to] {
				distance[edge.to] = candidate
				queue.push(routeQueueItem{node: edge.to, distance: candidate})
			}
		}
	}
	return best, best >= 0
}

// nearestWithin returns the index of the goal node with the lowest route
// cost, and that cost. The cost is the same as in nearestIndexed. Without
// reverse, it is the route cost from input.from to the goal. With reverse,
// it is the route cost from the goal to input.from. The search does not go
// past input.limit. It reports false when no goal has a cost of input.limit
// or less. Between goals with the same cost, the lower rank wins.
func (n Network) nearestWithin(input nearestWithinInput, graph routeGraph) (int, float64, bool) {
	start, ok := graph.nodes[input.from]
	if !ok {
		return 0, 0, false
	}
	adjacent := graph.outgoing
	if input.reverse {
		adjacent = graph.incoming
	}
	distance := make([]float64, len(n.Nodes))
	visited := make([]bool, len(n.Nodes))
	for i := range distance {
		distance[i] = math.Inf(1)
	}
	distance[start] = 0
	best, bestDistance := -1, math.Inf(1)
	queue := routeQueue{{node: start}}
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
		for _, laneIndex := range adjacent[item.node] {
			edge := graph.edges[laneIndex]
			next := edge.to
			if input.reverse {
				next = edge.from
			}
			extra := 0.0
			if laneIndex < len(input.extraCost) {
				extra = input.extraCost[laneIndex]
			}
			candidate := item.distance + edge.seconds + extra
			if candidate <= input.limit && candidate < distance[next] {
				distance[next] = candidate
				queue.push(routeQueueItem{node: next, distance: candidate})
			}
		}
	}
	if best < 0 {
		return 0, 0, false
	}
	return best, bestDistance, true
}

// nearestInput is the input of nearestIndexed.
type nearestInput struct {
	from string
	// rank holds a rank for each node index. It is -1 for a node that is not
	// a goal. Between goals with the same route cost, the lower rank wins.
	rank      []int
	extraCost []float64
}

// nearestWithinInput is the input of nearestWithin.
type nearestWithinInput struct {
	nearestInput
	// limit is the highest route cost that a goal can have.
	limit float64
	// reverse makes the search follow each lane from its end to its start.
	// The cost of a goal is then the route cost from the goal to from.
	reverse bool
}

// TestNetworkValidateRefusalOrder pins the error that a network with two
// faults gets. Network.validate checks the nodes, the lanes, each station
// with its berths, the lane stations, the banks, and then the station
// routes. Each case breaks two adjacent checks, and the earlier check gives
// the refusal.
func TestNetworkValidateRefusalOrder(t *testing.T) {
	t.Parallel()
	laneIndex := func(n Network, id string) int {
		return slices.IndexFunc(n.Lanes, func(lane Lane) bool { return lane.ID == id })
	}
	if laneIndex(Example(), "harbor-through") < 0 || laneIndex(BankExample(), "origin-through") < 0 {
		t.Fatal("the fixtures do not have the lanes that the cases change")
	}
	breakBanks := func(n *Network) { n.Stations[1].Banks = []StationBank{} }
	deleteLane := func(n *Network, id string) {
		i := laneIndex(*n, id)
		n.Lanes = slices.Delete(n.Lanes, i, i+1)
	}
	for _, test := range []struct {
		name string
		base func() Network
		edit func(*Network)
		want string
	}{
		{"node_before_lane", Example, func(n *Network) {
			n.Nodes[1].ID = n.Nodes[0].ID
			n.Lanes[1].ID = n.Lanes[0].ID
		}, fmt.Sprintf("invalid or duplicate node %q", Example().Nodes[0].ID)},
		{"lane_before_station", Example, func(n *Network) {
			n.Lanes[1].ID = n.Lanes[0].ID
			n.Stations[1].ID = n.Stations[0].ID
		}, fmt.Sprintf("invalid or duplicate lane %q", Example().Lanes[0].ID)},
		{"station_before_its_berth", Example, func(n *Network) {
			n.Stations[0].Entry = n.Stations[0].Exit
			n.Stations[0].Berths[0].ID = ""
		}, `station "harbor" needs valid entry, exit, and berth capacity`},
		{"berth_before_next_station", Example, func(n *Network) {
			n.Stations[0].Berths[0].ID = ""
			n.Stations[1].Entry = n.Stations[1].Exit
		}, `station "harbor" has an invalid or duplicate berth`},
		{"station_before_lane_station", Example, func(n *Network) {
			n.Stations[1].ID = n.Stations[0].ID
			n.Lanes[laneIndex(*n, "harbor-through")].StationID = "missing"
		}, `station "harbor" needs valid entry, exit, and berth capacity`},
		{"lane_station_before_banks", BankExample, func(n *Network) {
			n.Lanes[laneIndex(*n, "origin-through")].StationID = "missing"
			breakBanks(n)
		}, `lane "origin-through" has unknown station "missing"`},
		{"banks_before_routes", BankExample, func(n *Network) {
			breakBanks(n)
			deleteLane(n, "origin-through")
		}, fmt.Sprintf("station %q needs 1 to %d banks", "hub", MaxStationBanks)},
		{"routes", BankExample, func(n *Network) {
			deleteLane(n, "origin-through")
		}, `station "origin" needs entry, exit, and through lanes`},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			n := test.base().clone()
			test.edit(&n)
			if err := n.validate(); err == nil || err.Error() != test.want {
				t.Fatalf("got %v, want %q", err, test.want)
			}
		})
	}
}
