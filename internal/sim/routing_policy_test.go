package sim

import (
	"fmt"
	"math"
	"slices"
	"testing"
)

func TestOccupiedTrackCostSelectsDeterministicAlternative(t *testing.T) {
	t.Parallel()
	network := Network{
		Nodes: []Node{
			{ID: "a", Position: Point{}},
			{ID: "b", Position: Point{X: 100}},
			{ID: "c", Position: Point{X: 100, Y: 80}},
			{ID: "d", Position: Point{X: 200}},
		},
		Lanes: []Lane{
			{ID: "fast-1", From: "a", To: "b", SpeedLimit: 10},
			{ID: "fast-2", From: "b", To: "d", SpeedLimit: 10},
			{ID: "alternate-1", From: "a", To: "c", SpeedLimit: 10},
			{ID: "alternate-2", From: "c", To: "d", SpeedLimit: 10},
		},
	}
	graph := newRouteGraph(network)
	free, err := network.routeIndexed(networkRouteInput{from: "a", to: "d"}, graph)
	if err != nil {
		t.Fatal(err)
	}
	costs := make([]float64, len(network.Lanes))
	costs[graph.lanes["fast-1"]] = ownedTrackCongestionSeconds
	congested, err := network.routeIndexed(networkRouteInput{from: "a", to: "d", extraCost: costs}, graph)
	if err != nil {
		t.Fatal(err)
	}
	if free[0].ID != "fast-1" || congested[0].ID != "alternate-1" {
		t.Fatalf("free route = %+v, congested route = %+v", free, congested)
	}
}

func TestCongestionCostsTrackClaimsAndStoppedPods(t *testing.T) {
	t.Parallel()
	s := newSharingSimulation(t)
	lane := s.network.Lanes[0]
	s.owners[resource{kind: trackResource, id: lane.ID, cell: 0}] = "01"
	s.vehicles[0].Pod = Pod{ID: "01", Activity: Traveling, LaneID: lane.ID, WaitReason: TrackOccupied}
	costs := s.congestionCosts()
	if got := costs[s.graph.lanes[lane.ID]]; got != ownedTrackCongestionSeconds+stoppedVehicleCongestionSeconds {
		t.Fatalf("congestion cost = %v", got)
	}
}

// berthGuardNetwork has three stations in a line. Station b is between a
// and c, and a route from a to c can go through b's through lane or
// through b's berth.
func berthGuardNetwork() Network {
	return Network{
		Nodes: []Node{
			{ID: "a-berth", Position: Point{}},
			{ID: "b-entry", Position: Point{X: 100}},
			{ID: "b-berth", Position: Point{X: 150, Y: 20}},
			{ID: "b-exit", Position: Point{X: 200}},
			{ID: "c-berth", Position: Point{X: 300}},
		},
		Lanes: []Lane{
			{ID: "a-out", From: "a-berth", To: "b-entry", SpeedLimit: 10},
			{ID: "b-through", From: "b-entry", To: "b-exit", SpeedLimit: 10},
			{ID: "b-in", From: "b-entry", To: "b-berth", SpeedLimit: 10},
			{ID: "b-out", From: "b-berth", To: "b-exit", SpeedLimit: 10},
			{ID: "c-in", From: "b-exit", To: "c-berth", SpeedLimit: 10},
		},
		Stations: []Station{
			{ID: "a", Berths: []Berth{{ID: "a-1", Node: "a-berth"}}},
			{ID: "b", Entry: "b-entry", Exit: "b-exit", Berths: []Berth{{ID: "b-1", Node: "b-berth"}}},
			{ID: "c", Berths: []Berth{{ID: "c-1", Node: "c-berth"}}},
		},
	}
}

func TestOwnBerthsOnlyAvoidsThirdStationBerths(t *testing.T) {
	t.Parallel()
	network := berthGuardNetwork()
	graph := newRouteGraph(network)
	costs := make([]float64, len(network.Lanes))
	costs[graph.lanes["b-through"]] = 100
	unguarded, err := network.routeIndexed(networkRouteInput{from: "a-berth", to: "c-berth", extraCost: costs}, graph)
	if err != nil {
		t.Fatal(err)
	}
	guarded, err := network.routeIndexed(networkRouteInput{from: "a-berth", to: "c-berth", extraCost: costs, ownBerthsOnly: true}, graph)
	if err != nil {
		t.Fatal(err)
	}
	if unguarded[1].ID != "b-in" || guarded[1].ID != "b-through" {
		t.Fatalf("unguarded route = %+v, guarded route = %+v", unguarded, guarded)
	}
	// The berths of the stations at the route ends stay open.
	for _, input := range []networkRouteInput{
		{from: "a-berth", to: "b-berth", ownBerthsOnly: true},
		{from: "b-berth", to: "c-berth", ownBerthsOnly: true},
	} {
		if _, err := network.routeIndexed(input, graph); err != nil {
			t.Fatalf("route %s to %s: %v", input.from, input.to, err)
		}
	}
}

func TestCongestionRouteAvoidsThirdStationBerths(t *testing.T) {
	t.Parallel()
	s := &Simulation{network: berthGuardNetwork()}
	s.SetCongestionRouting(true)
	s.ensureNetworkIndexes()
	s.owners = map[resource]string{{kind: trackResource, id: "b-through"}: "01"}
	route, err := s.route("a-berth", "c-berth")
	if err != nil {
		t.Fatal(err)
	}
	for _, lane := range route {
		if lane.To == "b-berth" {
			t.Fatalf("congestion route %+v goes through the berth of station b", route)
		}
	}
}

// queueNetwork has a fast route a, b, d and an alternate route a, c, d.
// Each lane of the fast route takes 10 s. The alternate route is longer
// by a share that depends on detourY.
func queueNetwork(detourY float64) Network {
	return Network{
		Nodes: []Node{
			{ID: "a", Position: Point{}},
			{ID: "b", Position: Point{X: 100}},
			{ID: "c", Position: Point{X: 100, Y: detourY}},
			{ID: "d", Position: Point{X: 200}},
		},
		Lanes: []Lane{
			{ID: "fast-1", From: "a", To: "b", SpeedLimit: 10},
			{ID: "fast-2", From: "b", To: "d", SpeedLimit: 10},
			{ID: "alternate-1", From: "a", To: "c", SpeedLimit: 10},
			{ID: "alternate-2", From: "c", To: "d", SpeedLimit: 10},
		},
	}
}

// queueSimulation returns a simulation on the network with count stopped
// pods on the lane, and one moving pod.
func queueSimulation(network Network, lane string, count int) *Simulation {
	s := &Simulation{network: network}
	if err := s.SetRoutingPolicy(QueueRouting); err != nil {
		panic(err)
	}
	for index := range count {
		var v vehicle
		v.Pod = Pod{ID: fmt.Sprintf("q%02d", index), Activity: Traveling, LaneID: lane, WaitReason: TrackOccupied}
		s.vehicles = append(s.vehicles, v)
	}
	var moving vehicle
	moving.Pod = Pod{ID: "moving", Activity: Traveling, LaneID: lane, WaitReason: TrackOccupied, Speed: 5}
	s.vehicles = append(s.vehicles, moving)
	return s
}

func routeIDs(route []Lane) []string {
	ids := make([]string, len(route))
	for index, lane := range route {
		ids[index] = lane.ID
	}
	return ids
}

func TestQueueRouteChoice(t *testing.T) {
	t.Parallel()
	fast, alternate := []string{"fast-1", "fast-2"}, []string{"alternate-1", "alternate-2"}
	for _, tc := range []struct {
		name    string
		detourY float64
		lane    string
		count   int
		want    []string
	}{
		// The alternate route takes 20.88 s and the fast route 20 s.
		{name: "no queue", detourY: 30, lane: "fast-2", count: 0, want: fast},
		// The queue clears after 27 s, and the pod gets to it after 10 s.
		// The delay of 17 s saves 16.12 s on the alternate route.
		{name: "queue on the route", detourY: 30, lane: "fast-2", count: 9, want: alternate},
		// A delay of 14 s saves 13.12 s, which is less than 15 s.
		{name: "saving too small", detourY: 30, lane: "fast-2", count: 8, want: fast},
		// The queue clears after 9 s, before the pod gets to it.
		{name: "far queue", detourY: 30, lane: "fast-2", count: 3, want: fast},
		// The alternate route takes 25.61 s, more than 1.2 times 20 s.
		{name: "detour too long", detourY: 80, lane: "fast-2", count: 20, want: fast},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := queueSimulation(queueNetwork(tc.detourY), tc.lane, tc.count)
			route, err := s.assignedRoute(nil, "a", "d")
			if err != nil {
				t.Fatal(err)
			}
			if got := routeIDs(route); !slices.Equal(got, tc.want) {
				t.Fatalf("route = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestQueueRouteExcludesItsPod(t *testing.T) {
	t.Parallel()
	s := queueSimulation(queueNetwork(30), "fast-2", 9)
	route, err := s.assignedRoute(&s.vehicles[0], "a", "d")
	if err != nil {
		t.Fatal(err)
	}
	// Without pod q00, the queue has 8 pods, so the saving is too small.
	if got := routeIDs(route); got[0] != "fast-1" {
		t.Fatalf("route = %v, want the fast route", got)
	}
}

func TestFarQueueCostsNothing(t *testing.T) {
	t.Parallel()
	s := queueSimulation(queueNetwork(30), "fast-2", 3)
	s.ensureNetworkIndexes()
	route, _ := s.route("a", "d")
	seconds, cost, delayed := s.queueCost(route, s.queueDischarge(nil))
	if delayed || cost != seconds {
		t.Fatalf("far queue: cost %v, free-flow time %v, delayed %v", cost, seconds, delayed)
	}
	near := queueSimulation(queueNetwork(30), "fast-1", 3)
	near.ensureNetworkIndexes()
	if seconds, cost, delayed := near.queueCost(route, near.queueDischarge(nil)); !delayed || cost != seconds+9 {
		t.Fatalf("near queue: cost %v, free-flow time %v, delayed %v", cost, seconds, delayed)
	}
}

func TestQueueRouteAvoidsThirdStationBerths(t *testing.T) {
	t.Parallel()
	s := queueSimulation(berthGuardNetwork(), "b-through", 20)
	route, err := s.assignedRoute(nil, "a-berth", "c-berth")
	if err != nil {
		t.Fatal(err)
	}
	if got := routeIDs(route); slices.Contains(got, "b-in") {
		t.Fatalf("queue route %v goes through the berth of station b", got)
	}
	// Without the guard, the search goes through the berth.
	unguarded, err := s.network.routeIndexed(networkRouteInput{from: "a-berth", to: "c-berth", discharge: s.queueDischarge(nil)}, s.graph)
	if err != nil || !slices.Contains(routeIDs(unguarded), "b-in") {
		t.Fatalf("unguarded route = %v, %v", routeIDs(unguarded), err)
	}
}

// TestQueueRouteWithoutQueuesIsFreeFlow checks each pair of nodes of the
// example networks. No pod is stopped, so each assigned route is the
// free-flow route.
func TestQueueRouteWithoutQueuesIsFreeFlow(t *testing.T) {
	t.Parallel()
	for _, network := range []Network{Example(), twoBerthMarket(), ladderNetwork()} {
		s := &Simulation{network: network}
		if err := s.SetRoutingPolicy(QueueRouting); err != nil {
			t.Fatal(err)
		}
		var moving vehicle
		moving.Pod = Pod{ID: "moving", Activity: Traveling, LaneID: network.Lanes[0].ID, WaitReason: TrackOccupied, Speed: 1}
		s.vehicles = []vehicle{moving}
		for _, from := range network.Nodes {
			for _, to := range network.Nodes {
				got, gotErr := s.assignedRoute(nil, from.ID, to.ID)
				want, wantErr := s.route(from.ID, to.ID)
				if !slices.Equal(routeIDs(got), routeIDs(want)) || fmt.Sprint(gotErr) != fmt.Sprint(wantErr) {
					t.Fatalf("route %s to %s = %v, %v, want %v, %v", from.ID, to.ID, routeIDs(got), gotErr, routeIDs(want), wantErr)
				}
			}
		}
	}
}

// TestQueueDelayIsFIFO checks that a later start of a lane never gives an
// earlier end. The route search needs this to find the lowest cost.
func TestQueueDelayIsFIFO(t *testing.T) {
	t.Parallel()
	for _, discharge := range []float64{0, 3, 12.5, 90} {
		previous := math.Inf(-1)
		for step := range 2000 {
			at := float64(step) * 0.07
			end := at + queueDelay(discharge, at)
			if end < previous || end < discharge {
				t.Fatalf("discharge %v: start %v ends at %v, after %v", discharge, at, end, previous)
			}
			previous = end
		}
	}
}

// TestQueueSearchFindsLowestCost compares the route search with queue
// delays to each path of a grid. The grid has queues on many lanes.
func TestQueueSearchFindsLowestCost(t *testing.T) {
	t.Parallel()
	const size = 4
	var network Network
	id := func(x, y int) string { return fmt.Sprintf("n%d%d", x, y) }
	for x := range size {
		for y := range size {
			network.Nodes = append(network.Nodes, Node{ID: id(x, y), Position: Point{X: float64(x) * 100, Y: float64(y) * 100}})
			if x+1 < size {
				network.Lanes = append(network.Lanes, Lane{ID: id(x, y) + "-e", From: id(x, y), To: id(x+1, y), SpeedLimit: float64(5 + (x+2*y)%4)})
			}
			if y+1 < size {
				network.Lanes = append(network.Lanes, Lane{ID: id(x, y) + "-n", From: id(x, y), To: id(x, y+1), SpeedLimit: float64(5 + (2*x+y)%3)})
			}
		}
	}
	graph := newRouteGraph(network)
	for seed := range 20 {
		discharge := make([]float64, len(network.Lanes))
		for index := range discharge {
			discharge[index] = float64((index*7+seed*13)%11) * 9
		}
		route, err := network.routeIndexed(networkRouteInput{from: id(0, 0), to: id(size-1, size-1), discharge: discharge}, graph)
		if err != nil {
			t.Fatal(err)
		}
		cost := func(path []int) float64 {
			at := 0.0
			for _, lane := range path {
				at += graph.edges[lane].seconds + queueDelay(discharge[lane], at)
			}
			return at
		}
		var got []int
		for _, lane := range route {
			got = append(got, graph.lanes[lane.ID])
		}
		best := math.Inf(1)
		var walk func(node int, path []int)
		walk = func(node int, path []int) {
			if node == graph.nodes[id(size-1, size-1)] {
				best = math.Min(best, cost(path))
				return
			}
			for _, lane := range graph.outgoing[node] {
				walk(graph.edges[lane].to, append(path, lane))
			}
		}
		walk(graph.nodes[id(0, 0)], nil)
		if math.Abs(cost(got)-best) > 1e-9 {
			t.Fatalf("seed %d: search cost %v, lowest path cost %v", seed, cost(got), best)
		}
	}
}

// TestQueueRouteIsDeterministic checks that the fleet order does not change
// the queue delays or the route. TestCloneIsIndependentAndExact also runs
// two simulations with queue routing from the same inputs.
func TestQueueRouteIsDeterministic(t *testing.T) {
	t.Parallel()
	s := queueSimulation(queueNetwork(30), "fast-2", 9)
	for index := range 4 {
		var v vehicle
		v.Pod = Pod{ID: fmt.Sprintf("r%02d", index), Activity: Traveling, LaneID: "alternate-1", WaitReason: BerthOccupied}
		s.vehicles = append(s.vehicles, v)
	}
	route, err := s.assignedRoute(nil, "a", "d")
	if err != nil {
		t.Fatal(err)
	}
	discharge := s.queueDischarge(nil)
	slices.Reverse(s.vehicles)
	reversed, err := s.assignedRoute(nil, "a", "d")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(routeIDs(route), routeIDs(reversed)) || !slices.Equal(discharge, s.queueDischarge(nil)) {
		t.Fatalf("fleet order changed the route %v to %v", routeIDs(route), routeIDs(reversed))
	}
}
