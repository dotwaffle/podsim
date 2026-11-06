package sim

import (
	"math"
	"slices"
)

// routeSearchWork belongs to one simulation or to one call of bankNearest.
// Search results have separate storage, so another search can reuse these arrays
// and the heap.
type routeSearchWork struct {
	distance []float64
	previous []int
	visited  []bool
	queue    routeQueue
	// touched holds each node that the search wrote since the last reset,
	// and reset restores only those nodes. routeTargets does not add to it,
	// because it uses its work value for one search only.
	touched []int
	// path holds the lane indexes of the bank route of the last call of
	// bankPathNodes.
	path []int
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
		for index := range nodes {
			w.distance[index], w.previous[index], w.visited[index] = math.Inf(1), -1, false
		}
	} else {
		// Each element up to the capacity keeps its start value, so a later
		// search on more nodes can extend the arrays.
		distance, previous, visited := w.distance[:cap(w.distance)], w.previous[:cap(w.previous)], w.visited[:cap(w.visited)]
		for _, node := range w.touched {
			distance[node], previous[node], visited[node] = math.Inf(1), -1, false
		}
		w.distance, w.previous, w.visited = distance[:nodes], previous[:nodes], visited[:nodes]
	}
	w.touched = w.touched[:0]
	w.queue = w.queue[:0]
}

// appendPath adds the lane indexes of the route that the last search found, in
// route order, to path. On an error, it adds nothing.
func (w *routeSearchWork) appendPath(graph routeGraph, from, to int) error {
	if math.IsInf(w.distance[to], 1) {
		return ErrUnreachable
	}
	start := len(w.path)
	for current := to; current != from; {
		lane := w.previous[current]
		if lane < 0 {
			w.path = w.path[:start]
			return ErrUnreachable
		}
		w.path = append(w.path, lane)
		current = graph.edges[lane].from
	}
	slices.Reverse(w.path[start:])
	return nil
}

func (s *Simulation) searchRoute(input networkRouteInput) ([]Lane, error) {
	if s.routeWork == nil {
		s.routeWork = new(routeSearchWork)
	}
	work := s.routeWork
	searches, localFailed := work.searches, work.localFailed
	lanes, err := s.network.routeIndexedWithWork(input, s.routingGraph(), work)
	s.searchCounters.countRoute(err, work.searches-searches, work.localFailed-localFailed)
	return lanes, err
}
