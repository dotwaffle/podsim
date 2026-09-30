package sim

import "math"

// stationPickupBounds caches each station's free-flow bounds. These bounds
// depend only on the owned network, so Reset can retain them. Clone drops
// the cache, and a graph rebuild clears it. Callers must not change the slice.
func (s *Simulation) stationPickupBounds(stationID string) []float64 {
	s.ensureNetworkIndexes()
	if bounds, ok := s.pickupBounds[stationID]; ok {
		return bounds
	}
	station, ok := s.station(stationID)
	if !ok {
		return nil
	}
	bounds := s.graph.berthTravelBounds(station.Berths)
	if s.pickupBounds == nil {
		s.pickupBounds = make(map[string][]float64)
	}
	s.pickupBounds[stationID] = bounds
	return bounds
}

// berthTravelBounds gives the minimum free-flow lane time from each node
// to any berth. One reverse search covers every pickup candidate. The
// bound ignores berth load, acceleration, and braking, so it does not
// exceed the full estimate of a feasible pickup.
func (g routeGraph) berthTravelBounds(berths []Berth) []float64 {
	distance := make([]float64, len(g.outgoing))
	for i := range distance {
		distance[i] = math.Inf(1)
	}
	var queue routeQueue
	for _, berth := range berths {
		if node, ok := g.nodes[berth.Node]; ok && distance[node] != 0 {
			distance[node] = 0
			queue.push(routeQueueItem{node: node})
		}
	}
	for len(queue) > 0 {
		item := queue.pop()
		if item.distance != distance[item.node] {
			continue
		}
		for _, lane := range g.incoming[item.node] {
			edge := g.edges[lane]
			candidate := item.distance + edge.seconds
			if candidate < distance[edge.from] {
				distance[edge.from] = candidate
				queue.push(routeQueueItem{node: edge.from, distance: candidate})
			}
		}
	}
	return distance
}

// pickupBound includes the untraveled part of a moving pod's committed
// prefix. Its suffix can end at any berth, even a busy one. Zero disables
// pruning when a route boundary cannot be identified.
func (s *Simulation) pickupBound(v *vehicle, bounds []float64) float64 {
	var from string
	seconds := 0.0
	if v.Pod.Activity == Idle {
		station, _ := s.station(v.Pod.StationID)
		berth, _ := station.berth(v.Pod.BerthID)
		from = berth.Node
	} else {
		prefix, node, ok := s.divertStart(v)
		if !ok {
			return math.Inf(1)
		}
		from = node
		remaining := v.distance
		for _, lane := range v.Route[:prefix] {
			length := s.laneLength(lane)
			if remaining >= length {
				remaining -= length
				continue
			}
			seconds += (length - remaining) / lane.SpeedLimit
			remaining = 0
		}
		if remaining > 0 {
			return 0
		}
	}
	node, ok := s.graph.nodes[from]
	if !ok || node >= len(bounds) {
		return 0
	}
	return seconds + bounds[node]
}

func pickupCannotImprove(bound, best float64) bool {
	// Reverse and forward searches add lane times in different orders.
	// Leave a relative margin, and evaluate ties with the original route
	// search and fleet order.
	return bound > best+1e-6*(1+best)
}
