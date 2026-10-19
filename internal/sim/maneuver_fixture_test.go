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

// compactY is the aisle axis of the compact station of the design table.
const compactY = -4.008168830740232

// compactStationNetwork returns the four-bay compact station of the design
// table, with bays 2 to 4 pitch meters apart, and the loop and the stop of
// tallBayNetwork. The node and control coordinates are the canonical
// values of the table, and the preset generator must give the same
// geometry. receive is the x coordinate of the receiving node.
func compactStationNetwork(pitch, receive float64) Network {
	y := compactY
	network := Network{
		Nodes: []Node{
			{ID: "entry", Position: Point{-49.14, y}}, {ID: "hold", Position: Point{-19.14, y}},
			{ID: "receive", Position: Point{receive, y}}, {ID: "exit", Position: Point{58.40, y}},
			{ID: "loop-east", Position: Point{58.40, y - 100}}, {ID: "stop-entry", Position: Point{30, y - 100}},
			{ID: "stop-berth", Position: Point{0, y - 140}}, {ID: "stop-exit", Position: Point{-20, y - 100}},
			{ID: "loop-west", Position: Point{-49.14, y - 100}},
		},
		Lanes: []Lane{
			{ID: "approach", From: "entry", To: "hold", SpeedLimit: 14},
			{ID: "through", From: "entry", To: "exit", SpeedLimit: 14, Control: &Point{X: 4.63, Y: y - 32}},
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
	group := ManeuverGroup{ID: "compact", StationID: "bays"}
	add := func(id, from, to string, control *Point, reverse bool) {
		network.Lanes = append(network.Lanes, Lane{ID: id, From: from, To: to, SpeedLimit: 2, VehicleClasses: compactClassSet, Control: control})
		group.LaneIDs = append(group.LaneIDs, id)
		if reverse {
			group.ReverseLaneIDs = append(group.ReverseLaneIDs, id)
		}
	}
	type piece struct {
		node    Point
		control *Point
	}
	shift := func(p Point, dx float64) Point { return Point{p.X + dx, p.Y} }
	bay1 := []piece{
		{Point{-5.180584, -3.620733}, &Point{-6.116867, y}}, {Point{-3.527301, -2.517305}, &Point{-4.244301, -3.233297}},
		{Point{-2.513211, -1.504641}, nil}, {Point{-1.843937, -0.943654}, &Point{-2.203065, -1.194929}},
		{Point{-1.087569, -0.507142}, &Point{-1.484809, -0.692378}},
	}
	bay2 := []piece{
		{Point{-3.058138, -3.900422}, &Point{-3.570193, y}}, {Point{-2.067188, -3.581799}, &Point{-2.546083, -3.792675}},
		{Point{4.814302, -0.551620}, nil}, {Point{4.912431, -0.507142}, &Point{4.863606, -0.529909}},
	}
	previous := "hold"
	for bay := 1; bay <= 4; bay++ {
		pieces, branch := bay1, Point{-7.130146, y}
		if bay > 1 {
			dx := pitch * float64(bay-2)
			pieces, branch = nil, Point{-4.093461 + dx, y}
			for _, p := range bay2 {
				moved := piece{node: shift(p.node, dx)}
				if p.control != nil {
					moved.control = new(shift(*p.control, dx))
				}
				pieces = append(pieces, moved)
			}
		}
		branchID, berthID := fmt.Sprint("branch-", bay), fmt.Sprint("bay-", bay)
		network.Nodes = append(network.Nodes, Node{ID: branchID, Position: branch})
		add(fmt.Sprint("aisle-", bay), previous, branchID, nil, false)
		previous = branchID
		nodes := []string{branchID}
		for k, p := range pieces {
			id := fmt.Sprintf("bay-%d-node-%d", bay, k+1)
			if k == len(pieces)-1 {
				id = fmt.Sprint("berth-", bay)
			}
			network.Nodes = append(network.Nodes, Node{ID: id, Position: p.node})
			nodes = append(nodes, id)
		}
		for k, p := range pieces {
			add(fmt.Sprintf("bay-%d-in-%d", bay, k+1), nodes[k], nodes[k+1], p.control, false)
		}
		for k, piece := range slices.Backward(pieces) {
			add(fmt.Sprintf("bay-%d-out-%d", bay, k+1), nodes[k+1], nodes[k], piece.control, true)
		}
		network.Stations[0].Berths = append(network.Stations[0].Berths, Berth{ID: berthID, Node: fmt.Sprint("berth-", bay), VehicleClasses: compactClassSet})
		group.BerthIDs = append(group.BerthIDs, berthID)
	}
	add("aisle-out", previous, "receive", nil, false)
	network.ManeuverGroups = []ManeuverGroup{group}
	return network
}
