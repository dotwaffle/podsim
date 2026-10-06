package sim

import "math"

// routeSearchWork belongs to one simulation. Search results have separate
// storage, so another search can reuse these arrays and the heap.
type routeSearchWork struct {
	distance []float64
	previous []int
	visited  []bool
	queue    routeQueue
	// searches counts the calls of reset, which is one for each
	// shortest-path search, and localFailed the same-bank local attempts
	// of bankRoute that found no path. searchRoute reads them for the
	// search counters.
	searches, localFailed int64
}

func (w *routeSearchWork) reset(nodes int) {
	w.searches++
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
	work := s.routeWork
	searches, localFailed := work.searches, work.localFailed
	lanes, err := s.network.routeIndexedWithWork(input, s.routingGraph(), work)
	s.searchCounters.countRoute(input, err, work.searches-searches, work.localFailed-localFailed)
	return lanes, err
}
