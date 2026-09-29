package observe

import "github.com/dotwaffle/podsim/internal/sim"

// NetworkMonitor stores owned topology indexes for all station summaries.
// Its summaries follow the station order supplied to NewNetworkMonitor.
type NetworkMonitor struct {
	monitor       StationMonitor
	stations      []stationIndex
	berthStations map[string][]int
	nodeStations  map[string][]int
}

type stationIndex struct {
	station    sim.Station
	berthCount int
	berthNodes map[string]bool
}

// NewNetworkMonitor builds static indexes without retaining network slices.
func NewNetworkMonitor(network sim.Network) NetworkMonitor {
	monitor := NetworkMonitor{
		monitor:       NewStationMonitor(network),
		stations:      make([]stationIndex, len(network.Stations)),
		berthStations: make(map[string][]int),
		nodeStations:  make(map[string][]int),
	}
	for index, station := range network.Stations {
		nodes := make(map[string]bool, len(station.Berths))
		ids := make(map[string]bool, len(station.Berths))
		for _, berth := range station.Berths {
			if !ids[berth.ID] {
				monitor.berthStations[berth.ID] = append(monitor.berthStations[berth.ID], index)
				ids[berth.ID] = true
			}
			if !nodes[berth.Node] {
				monitor.nodeStations[berth.Node] = append(monitor.nodeStations[berth.Node], index)
				nodes[berth.Node] = true
			}
		}
		monitor.stations[index] = stationIndex{
			station:    sim.Station{Entry: station.Entry, Exit: station.Exit},
			berthCount: len(station.Berths), berthNodes: nodes,
		}
	}
	return monitor
}

// Summarize returns independent counters for each indexed station.
// The monitor is read-only, so callers can summarize snapshots concurrently.
func (monitor NetworkMonitor) Summarize(state sim.Snapshot) []StationMetrics {
	metrics := make([]StationMetrics, len(monitor.stations))
	for _, berth := range state.Berths {
		for _, index := range monitor.berthStations[berth.ID] {
			if berth.Occupant != "" {
				metrics[index].Occupied++
			}
			if berth.ReservedBy != "" {
				metrics[index].TotalReserved++
				if berth.Occupant == "" {
					metrics[index].ReservedEmpty++
				}
			}
		}
	}
	for index, station := range monitor.stations {
		metrics[index].Free = station.berthCount - metrics[index].Occupied - metrics[index].ReservedEmpty
	}
	var seen []int
	for vehicleIndex, vehicle := range state.Vehicles {
		if !vehicleInTransit(vehicle) || len(vehicle.Route) == 0 {
			continue
		}
		stopped := podStopped(vehicle.Pod)
		for _, index := range monitor.nodeStations[vehicle.Route[len(vehicle.Route)-1].To] {
			metrics[index].Approaching++
			if stopped && monitor.monitor.onEntranceSegment(vehicle, monitor.stations[index].station) {
				metrics[index].EntranceStopped++
			}
		}
		if !stopped {
			continue
		}
		if seen == nil {
			seen = make([]int, len(monitor.stations))
		}
		for _, lane := range vehicle.Route {
			for _, index := range monitor.nodeStations[lane.From] {
				if seen[index] == vehicleIndex+1 {
					continue
				}
				seen[index] = vehicleIndex + 1
				station := monitor.stations[index]
				if monitor.monitor.onExitSegment(vehicle, station.station, station.berthNodes) {
					metrics[index].ExitStopped++
				}
			}
		}
	}
	return metrics
}
