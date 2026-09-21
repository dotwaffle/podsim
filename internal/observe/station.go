// Package observe derives display and experiment metrics from simulation snapshots.
package observe

import "github.com/dotwaffle/podsim/internal/sim"

const stoppedSpeed = 0.01

// StationMetrics summarizes berth use and stopped traffic at one station.
type StationMetrics struct {
	Occupied        int
	ReservedEmpty   int
	TotalReserved   int
	Free            int
	Approaching     int
	EntranceStopped int
	ExitStopped     int
}

// StationMonitor stores network topology used by station observations.
type StationMonitor struct {
	indegree  map[string]int
	outdegree map[string]int
}

// NewStationMonitor builds reusable topology indexes for a network.
func NewStationMonitor(network sim.Network) StationMonitor {
	monitor := StationMonitor{
		indegree:  make(map[string]int, len(network.Nodes)),
		outdegree: make(map[string]int, len(network.Nodes)),
	}
	for _, lane := range network.Lanes {
		monitor.indegree[lane.To]++
		monitor.outdegree[lane.From]++
	}
	return monitor
}

// Summarize returns berth use and local queues for one station snapshot.
func (monitor StationMonitor) Summarize(station sim.Station, state sim.Snapshot) StationMetrics {
	metrics := StationMetrics{}
	berths := make(map[string]bool, len(station.Berths))
	berthNodes := make(map[string]bool, len(station.Berths))
	for _, berth := range station.Berths {
		berths[berth.ID] = true
		berthNodes[berth.Node] = true
	}
	for _, berth := range state.Berths {
		if !berths[berth.ID] {
			continue
		}
		if berth.Occupant != "" {
			metrics.Occupied++
		}
		if berth.ReservedBy != "" {
			metrics.TotalReserved++
			if berth.Occupant == "" {
				metrics.ReservedEmpty++
			}
		}
	}
	metrics.Free = len(station.Berths) - metrics.Occupied - metrics.ReservedEmpty
	for _, vehicle := range state.Vehicles {
		if !vehicleInTransit(vehicle) || len(vehicle.Route) == 0 {
			continue
		}
		if berthNodes[vehicle.Route[len(vehicle.Route)-1].To] {
			metrics.Approaching++
			if podStopped(vehicle.Pod) && monitor.onEntranceSegment(vehicle, station) {
				metrics.EntranceStopped++
			}
		}
		if podStopped(vehicle.Pod) && monitor.onExitSegment(vehicle, station, berthNodes) {
			metrics.ExitStopped++
		}
	}
	return metrics
}

func vehicleInTransit(vehicle sim.Vehicle) bool {
	return vehicle.Pod.Activity == sim.Traveling || vehicle.Pod.Activity == sim.DepartingEmpty
}

func podStopped(pod sim.Pod) bool {
	return pod.Speed < stoppedSpeed && pod.WaitReason != sim.NoWait && pod.LaneID != ""
}

func (monitor StationMonitor) onEntranceSegment(vehicle sim.Vehicle, station sim.Station) bool {
	start := -1
	for i, lane := range vehicle.Route {
		if lane.To == station.Entry {
			start = i
			for start > 0 && monitor.outdegree[vehicle.Route[start].From] == 1 {
				start--
			}
			break
		}
		if lane.From == station.Entry {
			start = i
			break
		}
	}
	for i := max(0, start); start >= 0 && i < len(vehicle.Route); i++ {
		if vehicle.Route[i].ID == vehicle.Pod.LaneID {
			return true
		}
	}
	return false
}

func (monitor StationMonitor) onExitSegment(vehicle sim.Vehicle, station sim.Station, berthNodes map[string]bool) bool {
	start, end := -1, -1
	for i, lane := range vehicle.Route {
		if start < 0 && berthNodes[lane.From] {
			start = i
		}
		if start >= 0 && lane.From == station.Exit {
			end = i
			for end+1 < len(vehicle.Route) && monitor.indegree[vehicle.Route[end].To] == 1 {
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
		for end+1 < len(vehicle.Route) && vehicle.Route[end].To != station.Exit {
			end++
		}
	}
	for i := start; i <= end; i++ {
		if vehicle.Route[i].ID == vehicle.Pod.LaneID {
			return true
		}
	}
	return false
}
