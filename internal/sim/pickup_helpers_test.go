package sim

// localPickup returns the first idle pod at the station that is not in
// assigned.
func (s *Simulation) localPickup(stationID string, pass *dispatchPass) *vehicle {
	for _, v := range s.freePods(pass) {
		if v.Pod.Activity == Idle && v.Pod.StationID == stationID && !pass.assigned[v.Pod.ID] {
			return v
		}
	}
	return nil
}

// pickupPod chooses the fastest available idle or divertible parking pod.
// Fleet order breaks ties.
// An idle pod at the pickup station has an estimate of zero, so it wins
// over each pod that must travel.
// It does not change the pods, the berth owners, or the waiting trips, so it
// computes each berth load one time for all pods.
//
// pickupRouteWithAssignments rejects each pod that pickupCandidate rejects,
// and it does not change the simulation when it does so. Thus pickupPod
// reads only the pickup candidates of the pass. They are in fleet order, so
// the full route search retains fleet-order ties. A reverse-search lower
// bound skips candidates that cannot improve the best pickup time. With
// no candidate, pickupPod does not compute the berth loads.
func (s *Simulation) pickupPod(stationID string, pass *dispatchPass) *vehicle {
	return s.pickupPodMatching(stationID, pass, nil, "")
}

// pickupRoute includes the track already committed by a pod moving toward parking.
func (s *Simulation) pickupRoute(v *vehicle, stationID string) ([]Lane, Berth, bool) {
	return s.pickupRouteWithAssignments(pickupRouteInput{pod: v, station: stationID})
}

func (s *Simulation) sendPickup(v *vehicle, stationID string) error {
	return s.sendPickupMatching(v, stationID, nil)
}
