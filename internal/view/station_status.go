package view

import "github.com/dotwaffle/podsim/internal/sim"

const stoppedSpeed = 0.01

type stationStatus struct {
	occupied        int
	reservedEmpty   int
	totalReserved   int
	free            int
	approaching     int
	entranceStopped int
	exitStopped     int
}

type summarizeStationInput struct {
	topology stationTopology
	station  sim.Station
	state    sim.Snapshot
}

type stationTopology struct {
	indegree  map[string]int
	outdegree map[string]int
}

func newStationTopology(network sim.Network) stationTopology {
	topology := stationTopology{
		indegree:  make(map[string]int, len(network.Nodes)),
		outdegree: make(map[string]int, len(network.Nodes)),
	}
	for _, lane := range network.Lanes {
		topology.indegree[lane.To]++
		topology.outdegree[lane.From]++
	}
	return topology
}

func summarizeStation(input summarizeStationInput) stationStatus {
	status := stationStatus{}
	berths := make(map[string]bool, len(input.station.Berths))
	berthNodes := make(map[string]bool, len(input.station.Berths))
	for _, berth := range input.station.Berths {
		berths[berth.ID] = true
		berthNodes[berth.Node] = true
	}
	for _, berth := range input.state.Berths {
		if !berths[berth.ID] {
			continue
		}
		if berth.Occupant != "" {
			status.occupied++
		}
		if berth.ReservedBy != "" {
			status.totalReserved++
			if berth.Occupant == "" {
				status.reservedEmpty++
			}
		}
	}
	status.free = len(input.station.Berths) - status.occupied - status.reservedEmpty
	for _, vehicle := range input.state.Vehicles {
		if !vehicleInTransit(vehicle) || len(vehicle.Route) == 0 {
			continue
		}
		if berthNodes[vehicle.Route[len(vehicle.Route)-1].To] {
			status.approaching++
			if podStopped(vehicle.Pod) && onEntranceSegment(entranceSegmentInput{vehicle: vehicle, station: input.station, outdegree: input.topology.outdegree}) {
				status.entranceStopped++
			}
		}
		if podStopped(vehicle.Pod) && onExitSegment(exitSegmentInput{vehicle: vehicle, station: input.station, berthNodes: berthNodes, indegree: input.topology.indegree}) {
			status.exitStopped++
		}
	}
	return status
}

func vehicleInTransit(vehicle sim.Vehicle) bool {
	return vehicle.Pod.Activity == sim.Traveling || vehicle.Pod.Activity == sim.DepartingEmpty
}

func podStopped(pod sim.Pod) bool {
	return pod.Speed < stoppedSpeed && pod.WaitReason != sim.NoWait && pod.LaneID != ""
}

type entranceSegmentInput struct {
	vehicle   sim.Vehicle
	station   sim.Station
	outdegree map[string]int
}

func onEntranceSegment(input entranceSegmentInput) bool {
	start := -1
	for i, lane := range input.vehicle.Route {
		if lane.To == input.station.Entry {
			start = i
			for start > 0 && input.outdegree[input.vehicle.Route[start].From] == 1 {
				start--
			}
			break
		}
		if lane.From == input.station.Entry {
			start = i
			break
		}
	}
	for i := max(0, start); start >= 0 && i < len(input.vehicle.Route); i++ {
		if input.vehicle.Route[i].ID == input.vehicle.Pod.LaneID {
			return true
		}
	}
	return false
}

type exitSegmentInput struct {
	vehicle    sim.Vehicle
	station    sim.Station
	berthNodes map[string]bool
	indegree   map[string]int
}

func onExitSegment(input exitSegmentInput) bool {
	start, end := -1, -1
	for i, lane := range input.vehicle.Route {
		if start < 0 && input.berthNodes[lane.From] {
			start = i
		}
		if start >= 0 && lane.From == input.station.Exit {
			end = i
			for end+1 < len(input.vehicle.Route) && input.indegree[input.vehicle.Route[end].To] == 1 {
				end++
			}
			break
		}
	}
	if start < 0 {
		return false
	}
	if end < start {
		end = start
		for end+1 < len(input.vehicle.Route) && input.vehicle.Route[end].To != input.station.Exit {
			end++
		}
	}
	for i := start; i <= end; i++ {
		if input.vehicle.Route[i].ID == input.vehicle.Pod.LaneID {
			return true
		}
	}
	return false
}
