package sim

import "math"

// bankTravelBounds joins arrival, external, and departure lower bounds at gates.
func (s *Simulation) bankTravelBounds(station Station) []float64 {
	graph := s.graph
	arrival := make([]float64, len(s.network.Nodes))
	for i := range arrival {
		arrival[i] = math.Inf(1)
	}
	for _, berth := range station.Berths {
		arrival[graph.nodes[berth.Node]] = 0
	}
	bankReverseBounds(arrival, graph, func(lane int) bool {
		owner := graph.banks.lanes[lane]
		return owner >= 0 && s.network.Stations[graph.banks.banks[owner].station].ID == station.ID && graph.banks.banks[owner].arrival[lane]
	})
	external := make([]float64, len(arrival))
	for i := range external {
		external[i] = math.Inf(1)
	}
	if station.Banks == nil {
		// A legacy destination retains its terminal local geometry.
		return graph.berthTravelBounds(station.Berths)
	}
	for _, bank := range station.Banks {
		node := graph.nodes[bank.Entry]
		external[node] = arrival[node]
	}
	bankReverseBounds(external, graph, func(lane int) bool {
		return graph.banks.lanes[lane] < 0 || s.network.Lanes[lane].StationRole == StationThroughRole
	})
	departure := make([]float64, len(arrival))
	for i := range departure {
		departure[i] = math.Inf(1)
	}
	for _, bank := range graph.banks.banks {
		departure[bank.exit] = external[bank.exit]
	}
	bankReverseBounds(departure, graph, func(lane int) bool {
		return s.network.Lanes[lane].StationRole == StationDepartureRole && graph.banks.lanes[lane] >= 0
	})
	for i := range external {
		external[i] = min(external[i], arrival[i], departure[i])
	}
	return external
}

func bankReverseBounds(distance []float64, graph routeGraph, allowed func(int) bool) {
	var queue routeQueue
	for node, cost := range distance {
		if !math.IsInf(cost, 1) {
			queue.push(routeQueueItem{node: node, distance: cost})
		}
	}
	for len(queue) > 0 {
		item := queue.pop()
		if item.distance != distance[item.node] {
			continue
		}
		for _, lane := range graph.incoming[item.node] {
			if !allowed(lane) {
				continue
			}
			edge := graph.edges[lane]
			cost := item.distance + edge.seconds
			if cost < distance[edge.from] {
				distance[edge.from] = cost
				queue.push(routeQueueItem{node: edge.from, distance: cost})
			}
		}
	}
}
