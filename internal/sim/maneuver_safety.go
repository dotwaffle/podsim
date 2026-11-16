package sim

// maneuverSafety adds the separation key of each pod, the certified
// pairs, the lateral pairs, the lateral merge pairs, and the shape pairs to
// an observation, when the network has certified, lateral, lateral merge,
// or shape pairs. A pod
// on a lane has the key of the lane, and a pod at a berth the key of the
// berth. A traveling pod with no lane at route distance 0 has the key of
// its origin berth when it is at the berth node. Every other pod has the
// zero key, which names nothing.
func (s *Simulation) maneuverSafety(observation *SafetyObservation) {
	if len(s.certified) == 0 && !s.lateral.any() && !s.merges.any() && !s.shapes.any() {
		return
	}
	observation.keys = make([]separationKey, len(s.vehicles))
	observation.certified, observation.lateral, observation.merges, observation.shapes = s.certified, s.lateral, s.merges, s.shapes
	for index := range s.vehicles {
		if key, ok := s.separationKey(&s.vehicles[index]); ok {
			observation.keys[index] = key
		}
	}
}

func (s *Simulation) separationKey(v *vehicle) (separationKey, bool) {
	pod := v.Pod
	switch {
	case pod.LaneID != "":
		index, ok := s.graph.lanes[pod.LaneID]
		return laneKey(index), ok
	case pod.BerthID != "":
		index, ok := s.berthKeys[pod.BerthID]
		return berthKey(index), ok
	}
	return separationKey{}, false
}
