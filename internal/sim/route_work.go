package sim

import "math"

// routeSearchWork belongs to one simulation. Search results have separate
// storage, so another search can reuse these arrays and the heap.
type routeSearchWork struct {
	distance []float64
	previous []int
	visited  []bool
	queue    routeQueue
}

func (w *routeSearchWork) reset(nodes int) {
	if cap(w.distance) < nodes {
		w.distance = make([]float64, nodes)
		w.previous = make([]int, nodes)
		w.visited = make([]bool, nodes)
	} else {
		w.distance = w.distance[:nodes]
		w.previous = w.previous[:nodes]
		w.visited = w.visited[:nodes]
	}
	for index := range nodes {
		w.distance[index], w.previous[index], w.visited[index] = math.Inf(1), -1, false
	}
	w.queue = w.queue[:0]
}

func (s *Simulation) searchRoute(input networkRouteInput) ([]Lane, error) {
	if s.routeWork == nil {
		s.routeWork = new(routeSearchWork)
	}
	return s.network.routeIndexedWithWork(input, s.graph, s.routeWork)
}
