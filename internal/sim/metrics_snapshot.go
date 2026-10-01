package sim

// MetricsSnapshot copies state for comparison and station metrics.
// Routes contain only ID, From, and To. Moving or unblocked pods retain
// only the terminal lane. Stopped waiting pods retain the full topology.
// Use Snapshot for geometry, rendering, routing, saves, and safety checks.
// The caller owns synchronization, as for Snapshot. Returned storage is owned.
func (s *Simulation) MetricsSnapshot() Snapshot {
	state := s.snapshot(false)
	for index, vehicle := range s.vehicles {
		state.Vehicles[index].Route = metricsRouteContext(vehicle.Pod, vehicle.Route)
	}
	return state
}

func metricsRouteContext(pod Pod, route []Lane) []Lane {
	if len(route) == 0 {
		return nil
	}
	if pod.Speed >= 0.01 || pod.WaitReason == NoWait || pod.LaneID == "" {
		route = route[len(route)-1:]
	}
	context := make([]Lane, len(route))
	for index, lane := range route {
		context[index] = Lane{ID: lane.ID, From: lane.From, To: lane.To}
	}
	return context
}
