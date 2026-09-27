package sim

import "testing"

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
