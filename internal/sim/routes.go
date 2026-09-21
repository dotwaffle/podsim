package sim

const routeCacheLimit = 4096

type routeKey struct{ from, to string }
type routeResult struct {
	lanes []Lane
	err   error
}

// route shares read-only paths within this simulation's immutable network.
// Snapshots copy routes before they leave the simulation.
func (s *Simulation) route(from, to string) ([]Lane, error) {
	key := routeKey{from: from, to: to}
	if cached, ok := s.routes[key]; ok {
		return cached.lanes, cached.err
	}
	lanes, err := s.network.Route(from, to)
	if s.routes == nil {
		s.routes = make(map[routeKey]routeResult)
	}
	if len(s.routes) >= routeCacheLimit {
		clear(s.routes)
	}
	s.routes[key] = routeResult{lanes: lanes, err: err}
	return lanes, err
}

// laneLength reuses geometry measurements for the fixed network.
func (s *Simulation) laneLength(lane Lane) float64 {
	if length, ok := s.lengths[lane.ID]; ok {
		return length
	}
	length := s.network.Length(lane)
	if s.lengths == nil {
		s.lengths = make(map[string]float64)
	}
	s.lengths[lane.ID] = length
	return length
}
