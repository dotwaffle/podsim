package sim

// podKey binds the separation key of a pod to the pod value of the
// observation, as safetyEnvelope does. A changed pod has no key.
type podKey struct {
	pod Pod
	key separationKey
}

// maneuverSafety adds the separation key of each pod, the certified
// pairs, and the lateral pairs to an observation, when the network has
// certified or lateral pairs. A pod on a lane has the key of the lane, and
// a pod at a berth the key of the berth. A traveling pod with no lane at
// route distance 0 has the key of its origin berth when it is at the
// berth node. Every other pod has no key.
func (s *Simulation) maneuverSafety(observation *SafetyObservation) {
	if len(s.certified) == 0 && !s.lateral.any() {
		return
	}
	observation.keys = make(map[string]podKey, len(observation.Pods))
	observation.certified, observation.lateral = s.certified, s.lateral
	for index, pod := range observation.Pods {
		key, ok := s.separationKey(&s.vehicles[index])
		if ok {
			observation.keys[pod.ID] = podKey{pod: pod, key: key}
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
	case pod.Activity == Traveling && v.distance == 0 && v.origin.ID != "":
		node, ok := s.network.Node(v.origin.Node)
		index, known := s.berthKeys[v.origin.ID]
		return berthKey(index), ok && known && pod.Position == node.Position
	}
	return separationKey{}, false
}
