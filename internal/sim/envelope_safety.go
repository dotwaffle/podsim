package sim

import "slices"

// envelopeLocationsPerPod is the capacity for envelope locations that
// largeSafety starts with, for each pod.
const envelopeLocationsPerPod = 4

// largeSafety adds the envelope of each pod when the fleet has a large pod.
// The envelopes share one backing array, and each envelope has no spare
// capacity, so an append to one envelope does not write into the next.
func (s *Simulation) largeSafety(observation *SafetyObservation) {
	if !slices.ContainsFunc(s.vehicles, func(v vehicle) bool { return largeVehicleClass(v.Pod.Class) }) {
		return
	}
	observation.envelopes = make([][]SafetyLocation, len(s.vehicles))
	locations := make([]SafetyLocation, 0, envelopeLocationsPerPod*len(s.vehicles))
	for index := range s.vehicles {
		v := &s.vehicles[index]
		start := len(locations)
		locations = s.appendVehicleSafetyLocations(locations, v, v.distance)
		observation.envelopes[index] = locations[start:len(locations):len(locations)]
	}
}

// appendVehicleSafetyLocations appends the planes that the body of a pod
// occupies at a route distance to locations, each plane one time. The rear
// bound includes retained resources, and the front bound encloses the body.
func (s *Simulation) appendVehicleSafetyLocations(locations []SafetyLocation, v *vehicle, distance float64) []SafetyLocation {
	if v.Pod.Activity != Traveling && v.Pod.LaneID == "" && v.Pod.BerthID != "" {
		// The envelope of a pod at a berth depends only on the berth. The
		// copy keeps the shared entry out of the observation.
		if envelope, ok := s.berthEnvelopes[v.Pod.BerthID]; ok {
			return append(locations, envelope...)
		}
	}
	start := len(locations)
	add := func(location SafetyLocation) {
		if !slices.Contains(locations[start:], location) {
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
	// A body at a berth occupies its incident planes, including an incoming
	// plane after arrival. Center-only locations cannot prove that it has left.
	node := v.origin.Node
	if berthNode, ok := s.berthNodes[v.Pod.BerthID]; ok && v.Pod.BerthID != "" {
		node = berthNode
	}
	for _, location := range s.nodeSafety[node] {
		add(location)
	}
	return locations
}

func envelopeLocationsSeparated(first, second []SafetyLocation) bool {
	if len(first) == 0 || len(second) == 0 {
		return false
	}
	for _, a := range first {
		for _, b := range second {
			if !safetyLocationsSeparated(a, b) {
				return false
			}
		}
	}
	return true
}
