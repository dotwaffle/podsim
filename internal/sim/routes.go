package sim

const routeCacheLimit = 4096

type routeKey struct {
	from, to string
	station  bool
}
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
	s.cacheRoute(key, routeResult{lanes: lanes, err: err})
	return lanes, err
}

func (s *Simulation) stationPath(from, to string) ([]Lane, error) {
	key := routeKey{from: from, to: to, station: true}
	if cached, ok := s.routes[key]; ok {
		return cached.lanes, cached.err
	}
	lanes, err := s.network.stationPath(from, to)
	s.cacheRoute(key, routeResult{lanes: lanes, err: err})
	return lanes, err
}

func (s *Simulation) cacheRoute(key routeKey, result routeResult) {
	if s.routes == nil {
		s.routes = make(map[routeKey]routeResult)
	}
	if _, exists := s.routes[key]; exists {
		s.routes[key] = result
		return
	}
	if len(s.routes) >= routeCacheLimit {
		for len(s.routeOrder) > 0 {
			oldest := s.routeOrder[0]
			s.routeOrder = s.routeOrder[1:]
			if _, exists := s.routes[oldest]; exists {
				delete(s.routes, oldest)
				break
			}
		}
		if len(s.routes) >= routeCacheLimit {
			clear(s.routes)
			s.routeOrder = s.routeOrder[:0]
		}
	}
	s.routes[key] = result
	s.routeOrder = append(s.routeOrder, key)
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
