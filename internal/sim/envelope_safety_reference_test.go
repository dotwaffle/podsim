package sim

import (
	"slices"
	"testing"
)

// referenceVehicleSafetyLocations is the construction of
// vehicleSafetyLocations before the berth and node indexes. It scans every
// station berth and every network lane.
func referenceVehicleSafetyLocations(s *Simulation, v *vehicle, distance float64) []SafetyLocation {
	var locations []SafetyLocation
	add := func(location SafetyLocation) {
		if !slices.Contains(locations, location) {
			locations = append(locations, location)
		}
	}
	if v.Pod.LaneID != "" {
		add(s.laneSafety[v.Pod.LaneID])
	}
	if v.Pod.BerthID != "" {
		add(s.berthSafety[v.Pod.BerthID])
	}
	if v.Pod.Activity == Traveling {
		front := largeEnvelopeRadius
		for _, entry := range v.blocks.lanes {
			if entry.cells != nil {
				front = max(front, entry.cells.front)
			}
		}
		for index, lane := range v.Route {
			start, end := v.blocks.lanes[index].start, v.blocks.lanes[index+1].start
			if start > distance+front {
				break
			}
			tail := largeClearance
			if cells := v.blocks.lanes[index].cells; cells != nil {
				tail = max(tail, cells.tail)
			}
			if end >= distance-tail {
				add(s.laneSafety[lane.ID])
			}
		}
		if v.originReleased {
			return locations
		}
		if v.origin.ID != "" {
			add(s.berthSafety[v.origin.ID])
		}
	}
	node := v.origin.Node
	if v.Pod.BerthID != "" {
		for _, station := range s.network.Stations {
			for _, berth := range station.Berths {
				if berth.ID == v.Pod.BerthID {
					node = berth.Node
				}
			}
		}
	}
	for _, lane := range s.network.Lanes {
		if lane.From == node || lane.To == node {
			add(s.laneSafety[lane.ID])
		}
	}
	return locations
}

// checkSafetyLocationParity requires the indexed envelope locations of
// each pod to equal the reference construction, in the same order.
func checkSafetyLocationParity(t *testing.T, s *Simulation) {
	t.Helper()
	for index := range s.vehicles {
		v := &s.vehicles[index]
		got, want := s.vehicleSafetyLocations(v, v.distance), referenceVehicleSafetyLocations(s, v, v.distance)
		if !slices.Equal(got, want) {
			t.Fatalf("tick %d pod %s safety locations %v, reference %v", s.tick, v.Pod.ID, got, want)
		}
	}
}
