package sim

import "slices"

// safetyEnvelope binds the occupied planes to an unchanged observation pod.
// The rear bound includes retained resources; the front bound encloses the body.
type safetyEnvelope struct {
	pod       Pod
	locations []SafetyLocation
}

func (s *Simulation) largeSafety(observation *SafetyObservation) {
	if !slices.ContainsFunc(s.vehicles, func(v vehicle) bool { return largeVehicleClass(v.Pod.Class) }) {
		return
	}
	observation.envelopes = make(map[string]safetyEnvelope, len(s.vehicles))
	for index := range s.vehicles {
		v := &s.vehicles[index]
		observation.envelopes[v.Pod.ID] = safetyEnvelope{pod: v.Pod, locations: s.vehicleSafetyLocations(v, v.distance)}
	}
}

func (s *Simulation) vehicleSafetyLocations(v *vehicle, distance float64) []SafetyLocation {
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
				front = max(front, largeEnvelopeRadius*entry.cells.tail/largeClearance)
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
