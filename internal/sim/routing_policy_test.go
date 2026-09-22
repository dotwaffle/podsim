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
