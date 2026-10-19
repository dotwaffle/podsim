package sim

import (
	"fmt"
	"slices"
)

// tallBayNetwork returns a station with one maneuver group: an aisle from
// the holding node "hold" to the receiving node "receive", and a closed
// bay chain to each berth, straight up from its branch node. bays sets the
// number of bays, 13 meters apart, so every pair of paths that the bank
// audit checks is at least 12 meters apart without certification. first is
// the length of the first piece of each bay. A loop with a second station,
// "stop", leads from the station exit back to its entry.
func tallBayNetwork(bays int, first float64) Network {
	network := Network{
		Nodes: []Node{
			{ID: "entry", Position: Point{-60, 0}}, {ID: "hold", Position: Point{-30, 0}},
			{ID: "receive", Position: Point{30 + 13*float64(bays-1), 0}}, {ID: "exit", Position: Point{60 + 13*float64(bays-1), 0}},
			{ID: "loop-east", Position: Point{60 + 13*float64(bays-1), -100}}, {ID: "stop-entry", Position: Point{30, -100}},
			{ID: "stop-berth", Position: Point{0, -140}}, {ID: "stop-exit", Position: Point{-30, -100}},
			{ID: "loop-west", Position: Point{-60, -100}},
		},
		Lanes: []Lane{
			{ID: "approach", From: "entry", To: "hold", SpeedLimit: 14},
			{ID: "through", From: "entry", To: "exit", SpeedLimit: 14, Control: &Point{X: 6.5 * float64(bays-1), Y: -60}},
			{ID: "leave", From: "receive", To: "exit", SpeedLimit: 14},
			{ID: "loop-down", From: "exit", To: "loop-east", SpeedLimit: 14},
			{ID: "loop-in", From: "loop-east", To: "stop-entry", SpeedLimit: 14},
			{ID: "stop-through", From: "stop-entry", To: "stop-exit", SpeedLimit: 14},
			{ID: "stop-in", From: "stop-entry", To: "stop-berth", SpeedLimit: 14},
			{ID: "stop-out", From: "stop-berth", To: "stop-exit", SpeedLimit: 14},
			{ID: "loop-out", From: "stop-exit", To: "loop-west", SpeedLimit: 14},
			{ID: "loop-up", From: "loop-west", To: "entry", SpeedLimit: 14},
		},
		Stations: []Station{
			{ID: "bays", Name: "Bays", Entry: "entry", Exit: "exit"},
			{ID: "stop", Name: "Stop", Entry: "stop-entry", Exit: "stop-exit", Berths: []Berth{{ID: "stop-1", Node: "stop-berth"}}},
		},
	}
	group := ManeuverGroup{ID: "bays-group", StationID: "bays"}
	aisle := func(id, from, to string) {
		network.Lanes = append(network.Lanes, Lane{ID: id, From: from, To: to, SpeedLimit: 2, VehicleClasses: compactClassSet})
		group.LaneIDs = append(group.LaneIDs, id)
	}
	previous := "hold"
	for bay := 1; bay <= bays; bay++ {
		x := 13 * float64(bay-1)
		branch, inner, berth := fmt.Sprint("branch-", bay), fmt.Sprint("inner-", bay), fmt.Sprint("berth-", bay)
		network.Nodes = append(network.Nodes, Node{ID: branch, Position: Point{x, 0}}, Node{ID: inner, Position: Point{x, first}}, Node{ID: berth, Position: Point{x, 14}})
		network.Stations[0].Berths = append(network.Stations[0].Berths, Berth{ID: fmt.Sprint("bay-", bay), Node: berth, VehicleClasses: compactClassSet})
		group.BerthIDs = append(group.BerthIDs, fmt.Sprint("bay-", bay))
		aisle(fmt.Sprint("aisle-", bay), previous, branch)
		previous = branch
		for _, piece := range [][2]string{{branch, inner}, {inner, berth}} {
			aisle(fmt.Sprintf("bay-%d-in-%s", bay, piece[1]), piece[0], piece[1])
		}
		for _, piece := range [][2]string{{berth, inner}, {inner, branch}} {
			id := fmt.Sprintf("bay-%d-out-%s", bay, piece[1])
			aisle(id, piece[0], piece[1])
			group.ReverseLaneIDs = append(group.ReverseLaneIDs, id)
		}
	}
	aisle("aisle-out", previous, "receive")
	network.ManeuverGroups = []ManeuverGroup{group}
	return network
}

// withoutLane removes a lane from the network and from its groups.
func withoutLane(network Network, id string) Network {
	network = network.clone()
	network.Lanes = slices.DeleteFunc(network.Lanes, func(lane Lane) bool { return lane.ID == id })
	for i := range network.ManeuverGroups {
		group := &network.ManeuverGroups[i]
		group.LaneIDs = slices.DeleteFunc(group.LaneIDs, func(lane string) bool { return lane == id })
		group.ReverseLaneIDs = slices.DeleteFunc(group.ReverseLaneIDs, func(lane string) bool { return lane == id })
	}
	return network
}

// networkLaneIndex returns the index of a lane by ID, or -1.
func networkLaneIndex(network Network, id string) int {
	return slices.IndexFunc(network.Lanes, func(lane Lane) bool { return lane.ID == id })
}
