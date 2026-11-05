package sim

import (
	"math"
	"testing"
)

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
	// The through lane is slow, so a free search prefers the berth of b.
	for index := range network.Lanes {
		if network.Lanes[index].ID == "b-through" {
			network.Lanes[index].SpeedLimit = 0.1
		}
	}
	graph := newRouteGraph(network)
	unguarded, err := network.routeIndexed(networkRouteInput{from: "a-berth", to: "c-berth"}, graph)
	if err != nil {
		t.Fatal(err)
	}
	guarded, err := network.routeIndexed(networkRouteInput{from: "a-berth", to: "c-berth", ownBerthsOnly: true}, graph)
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

func routeIDs(route []Lane) []string {
	ids := make([]string, len(route))
	for index, lane := range route {
		ids[index] = lane.ID
	}
	return ids
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
