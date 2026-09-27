package sim

import "slices"

const (
	routeCacheLimit                 = 8192
	ownedTrackCongestionSeconds     = 6.0
	stoppedVehicleCongestionSeconds = 20.0
	congestionRouteRefreshTicks     = 5 * TicksPerSecond
)

type routeKey struct {
	from, to string
	station  bool
}
type routeResult struct {
	lanes []Lane
	err   error
	// seconds is the value of routeSeconds for lanes and a pod at rest. It
	// is valid only when timed is true. withSeconds sets both.
	seconds float64
	timed   bool
}

// withSeconds returns the result with the value of routeSeconds for its
// lanes and a pod at rest.
func (s *Simulation) withSeconds(result routeResult) routeResult {
	result.seconds, result.timed = s.routeSeconds(result.lanes, motionEstimate{}), true
	return result
}

// route returns the free-flow route. It shares read-only paths within this
// simulation's immutable network. Snapshots copy routes before they leave
// the simulation. Clones share the routes of pods and waiting trips, so code
// replaces a route whole and never writes into it in place.
//
// Estimates, candidate searches and the choices of dispatch, positioning
// and parking use route with each routing policy. Only a pod that starts
// a new route gets it from assignedRoute.
func (s *Simulation) route(from, to string) ([]Lane, error) {
	result := s.cachedRoute(from, to)
	return result.lanes, result.err
}

// cachedRoute returns the result that route returns. The result can also
// hold the travel time of the route.
func (s *Simulation) cachedRoute(from, to string) routeResult {
	s.ensureNetworkIndexes()
	key := routeKey{from: from, to: to}
	if cached, ok := s.routes[key]; ok {
		return cached
	}
	lanes, err := s.network.routeIndexed(networkRouteInput{from: from, to: to}, s.graph)
	return s.cacheRoute(key, routeResult{lanes: lanes, err: err})
}

// costedRouting reports whether assignedRoute can give a route that is not
// the free-flow route.
func (s *Simulation) costedRouting() bool {
	return s.routingPolicy != FreeFlowRouting
}

// assignedRoute returns the route for pod v when it starts a new route from
// node from to node to. It is the free-flow route of route, or the route of
// the routing policy. The callers are board, startEmptyMove, sendPickup
// and parkReleased.
func (s *Simulation) assignedRoute(v *vehicle, from, to string) ([]Lane, error) {
	switch s.routingPolicy {
	case CongestionRouting:
		s.ensureNetworkIndexes()
		result := s.congestionRoute(from, to)
		return result.lanes, result.err
	case QueueRouting:
		return s.queueRoute(v, from, to)
	default:
		return s.route(from, to)
	}
}

// congestionRoute returns the route with the lowest travel time plus
// congestion cost. The route does not go through the berths of a station
// other than the stations at from and to. A pod at a berth costs nothing, so
// without this rule a route could go through the berths of a station to
// avoid its through lane. When no such route exists, congestionRoute
// returns the free-flow route.
func (s *Simulation) congestionRoute(from, to string) routeResult {
	s.refreshCongestionCosts()
	key := routeKey{from: from, to: to}
	if cached, ok := s.congestionRoutes[key]; ok {
		return cached
	}
	lanes, err := s.network.routeIndexed(networkRouteInput{from: from, to: to, extraCost: s.congestionRouteCosts, ownBerthsOnly: true}, s.graph)
	if err != nil {
		lanes, err = s.network.routeIndexed(networkRouteInput{from: from, to: to}, s.graph)
	}
	result := s.withSeconds(routeResult{lanes: lanes, err: err})
	s.congestionRoutes[key] = result
	return result
}

// refreshCongestionCosts computes the congestion costs again when they are
// older than congestionRouteRefreshTicks. It then clears the congestion
// routes.
func (s *Simulation) refreshCongestionCosts() {
	if s.congestionRouteCosts == nil || s.tick >= s.nextCongestionRouteRefresh {
		s.congestionRouteCosts = s.congestionCosts()
		s.congestionRoutes = make(map[routeKey]routeResult)
		s.nextCongestionRouteRefresh = s.tick + congestionRouteRefreshTicks
	}
}

func (s *Simulation) congestionCosts() []float64 {
	costs := make([]float64, len(s.network.Lanes))
	for claimed, owner := range s.owners {
		if owner == "" || claimed.kind != trackResource {
			continue
		}
		if index, ok := s.graph.lanes[claimed.id]; ok {
			costs[index] += ownedTrackCongestionSeconds
		}
	}
	for index := range s.vehicles {
		pod := s.vehicles[index].Pod
		if pod.Activity != Traveling || pod.LaneID == "" || pod.WaitReason == NoWait {
			continue
		}
		if laneIndex, ok := s.graph.lanes[pod.LaneID]; ok {
			costs[laneIndex] += stoppedVehicleCongestionSeconds
		}
	}
	return costs
}

// cacheStationRoutes puts the routes from a node to each berth of a station
// in the route cache. One route search gives all the routes, so the
// route calls that follow do not search again. The cache holds only routes
// that route returns, so this changes no result.
func (s *Simulation) cacheStationRoutes(from string, berths []Berth) {
	if len(berths) < 2 {
		return
	}
	s.ensureNetworkIndexes()
	var targets []string
	for _, berth := range berths {
		if _, cached := s.routes[routeKey{from: from, to: berth.Node}]; !cached && !slices.Contains(targets, berth.Node) {
			targets = append(targets, berth.Node)
		}
	}
	if len(targets) < 2 {
		return
	}
	for index, result := range s.network.routesIndexed(from, targets, s.graph) {
		s.cacheRoute(routeKey{from: from, to: targets[index]}, result)
	}
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
	s.junctionConflicts = buildJunctionConflicts(s.network)
	s.berthResources = indexBerthResources(s.network)
	s.laneCells = indexLaneCells(laneCellsIndexInput{network: s.network, geometry: s.geometry, conflicts: s.junctionConflicts, berths: s.berthResources})
	s.lengths = nil
	s.routes = nil
	s.routeOrder = nil
	s.congestionRouteCosts = nil
	s.congestionRoutes = nil
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

// cacheRoute stores the result with its travel time and returns the stored
// value.
func (s *Simulation) cacheRoute(key routeKey, result routeResult) routeResult {
	result = s.withSeconds(result)
	if s.routes == nil {
		s.routes = make(map[routeKey]routeResult)
	}
	if _, exists := s.routes[key]; exists {
		s.routes[key] = result
		return result
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
	return result
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
