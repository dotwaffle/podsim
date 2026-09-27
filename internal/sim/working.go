package sim

// WorkingVehicles counts the pods with assigned work. A pod has assigned
// work when it holds a trip that is not complete, or when a pending request
// names it as the pickup pod. The first case includes boarding, travel with
// passengers, and unloading. The second case includes a pod on its way to
// the pickup station and a pod that waits there to board.
//
// An empty move without a pending request, such as redistribution or a move
// to parking, is not work. A pod keeps its riders after the trip, so the
// count ignores a pod whose riders all left it. Every pod with passengers
// aboard has a rider that did not leave, so the count of pods with
// passengers aboard is never more than this count.
func (s Snapshot) WorkingVehicles() int {
	pickups := make(map[string]bool, len(s.Pending))
	for _, request := range s.Pending {
		if request.PodID != "" {
			pickups[request.PodID] = true
		}
	}
	working := 0
	for _, vehicle := range s.Vehicles {
		if vehicle.RidersAboard() > 0 || pickups[vehicle.Pod.ID] {
			working++
		}
	}
	return working
}
