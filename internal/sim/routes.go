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
	s.ensureNetworkIndexes()
	key := routeKey{from: from, to: to}
	if cached, ok := s.routes[key]; ok {
		return cached.lanes, cached.err
	}
	lanes, err := s.network.routeIndexed(networkRouteInput{from: from, to: to}, s.graph)
	s.cacheRoute(key, routeResult{lanes: lanes, err: err})
	return lanes, err
}

func (s *Simulation) stationPath(from, to string) ([]Lane, error) {
	s.ensureNetworkIndexes()
	key := routeKey{from: from, to: to, station: true}
	if cached, ok := s.routes[key]; ok {
		return cached.lanes, cached.err
	}
	lanes, err := s.network.routeIndexed(networkRouteInput{from: from, to: to, forbidden: s.stationForbidden}, s.graph)
	s.cacheRoute(key, routeResult{lanes: lanes, err: err})
	return lanes, err
}

func (s *Simulation) ensureNetworkIndexes() {
	if len(s.graph.nodes) == len(s.network.Nodes) && len(s.graph.lengths) == len(s.network.Lanes) {
		return
	}
	s.graph = newRouteGraph(s.network)
	s.stationIndexes = indexStations(s.network)
	s.stationForbidden = s.network.stationForbidden()
	s.geometry = buildLaneGeometry(s.network)
	s.lengths = nil
	s.routes = nil
	s.routeOrder = nil
}

func indexStations(network Network) map[string]int {
	indexes := make(map[string]int, len(network.Stations))
	for index, station := range network.Stations {
		indexes[station.ID] = index
	}
	return indexes
}

func (s *Simulation) station(id string) (Station, bool) {
	index, ok := s.stationIndexes[id]
	if !ok || index >= len(s.network.Stations) || s.network.Stations[index].ID != id {
		return s.network.Station(id)
	}
	return s.network.Stations[index], true
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
