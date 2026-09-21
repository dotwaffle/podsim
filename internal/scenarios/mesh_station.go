package scenarios

import (
	"fmt"
	"math"

	"github.com/dotwaffle/podsim/internal/sim"
)

type stationFrame struct {
	origin, along, outward sim.Point
}

func (frame stationFrame) position(along, outward float64) sim.Point {
	return add(frame.origin, add(scale(frame.along, along), scale(frame.outward, outward)))
}

// addMeshStation separates the road diverge and merge from the berth junctions.
func addMeshStation(network *sim.Network, parameters meshStationParameters) {
	frame := splitStationRoad(network, parameters)
	entry, exit := stationNodeID(parameters.index, "entry"), stationNodeID(parameters.index, "exit")
	network.Nodes = append(network.Nodes,
		sim.Node{ID: entry, Position: frame.position(400, 110)},
		sim.Node{ID: exit, Position: frame.position(800, 110)},
	)
	network.Lanes = append(network.Lanes,
		sim.Lane{ID: fmt.Sprintf("mesh-in-%02d", parameters.index+1), From: stationNodeID(parameters.index, "diverge"), To: entry, SpeedLimit: speedLimit},
		sim.Lane{ID: stationLaneID(parameters.index, "through"), From: entry, To: exit, SpeedLimit: speedLimit},
		sim.Lane{ID: fmt.Sprintf("mesh-out-%02d", parameters.index+1), From: exit, To: stationNodeID(parameters.index, "merge"), SpeedLimit: speedLimit},
	)
	station := sim.Station{
		ID: fmt.Sprintf("station-%02d", parameters.index+1), Name: fmt.Sprintf("Station %02d", parameters.index+1),
		Entry: entry, Exit: exit, ParkingOnly: parameters.parking,
	}
	if parameters.parking {
		station.ID, station.Name = "parking", "Parking"
	}
	previousArrival, previousDeparture := entry, exit
	for index := range parameters.berths {
		arrival := stationNodeID(parameters.index, fmt.Sprintf("arrival-%02d", index+1))
		departure := stationNodeID(parameters.index, fmt.Sprintf("departure-%02d", index+1))
		berth := sim.Berth{ID: fmt.Sprintf("%s-%02d", station.ID, index+1), Node: stationNodeID(parameters.index, fmt.Sprintf("berth-%02d", index+1))}
		// A 75-meter pitch leaves a free block between adjacent junction zones.
		depth := 185 + 75*float64(index)
		network.Nodes = append(network.Nodes,
			sim.Node{ID: arrival, Position: frame.position(400, depth)},
			sim.Node{ID: berth.Node, Position: frame.position(600, depth)},
			sim.Node{ID: departure, Position: frame.position(800, depth)},
		)
		network.Lanes = append(network.Lanes,
			sim.Lane{ID: stationLaneID(parameters.index, fmt.Sprintf("arrival-link-%02d", index+1)), From: previousArrival, To: arrival, SpeedLimit: speedLimit},
			sim.Lane{ID: stationLaneID(parameters.index, fmt.Sprintf("departure-link-%02d", index+1)), From: departure, To: previousDeparture, SpeedLimit: speedLimit},
			sim.Lane{ID: stationLaneID(parameters.index, fmt.Sprintf("in-%02d", index+1)), From: arrival, To: berth.Node, SpeedLimit: speedLimit},
			sim.Lane{ID: stationLaneID(parameters.index, fmt.Sprintf("out-%02d", index+1)), From: berth.Node, To: departure, SpeedLimit: speedLimit},
		)
		station.Berths = append(station.Berths, berth)
		previousArrival, previousDeparture = arrival, departure
	}
	network.Stations = append(network.Stations, station)
}

func splitStationRoad(network *sim.Network, parameters meshStationParameters) stationFrame {
	root := meshJunctionID(parameters.row, parameters.column)
	start, _ := network.Node(root)
	firstIndex := outgoingStationRoad(*network, root)
	first := network.Lanes[firstIndex]
	secondIndex := -1
	for index, lane := range network.Lanes {
		if lane.From == first.To {
			secondIndex = index
			break
		}
	}
	if secondIndex < 0 {
		panic(fmt.Sprintf("station road %q has no continuation", first.ID))
	}
	second := network.Lanes[secondIndex]
	end, _ := network.Node(second.To)
	frame := stationFrame{origin: start.Position, along: scale(add(end.Position, scale(start.Position, -1)), 1.0/1200)}
	frame.outward = sim.Point{X: -frame.along.Y, Y: frame.along.X}
	if frame.along.X == 0 || parameters.parking {
		frame.outward = scale(frame.outward, -1)
	}
	diverge, merge := stationNodeID(parameters.index, "diverge"), stationNodeID(parameters.index, "merge")
	network.Nodes = append(network.Nodes,
		sim.Node{ID: diverge, Position: network.Position(first, network.Length(first)/2)},
		sim.Node{ID: merge, Position: network.Position(second, network.Length(second)/2)},
	)
	network.Lanes[firstIndex].To = diverge
	network.Lanes[secondIndex].From = merge
	network.Lanes = append(network.Lanes,
		sim.Lane{ID: first.ID + "-access", From: diverge, To: first.To, SpeedLimit: speedLimit},
		sim.Lane{ID: second.ID + "-access", From: second.From, To: merge, SpeedLimit: speedLimit},
	)
	return frame
}

func outgoingStationRoad(network sim.Network, root string) int {
	start, _ := network.Node(root)
	result := -1
	for index, lane := range network.Lanes {
		if lane.From != root {
			continue
		}
		result = index
		end, _ := network.Node(lane.To)
		if math.Abs(end.Position.X-start.Position.X) > math.Abs(end.Position.Y-start.Position.Y) {
			return index
		}
	}
	if result < 0 {
		panic(fmt.Sprintf("station junction %q has no outgoing road", root))
	}
	return result
}
