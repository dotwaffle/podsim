package sim

import (
	"errors"
	"slices"
)

const (
	routeCacheLimit                 = 8192
	ownedTrackCongestionSeconds     = 6.0
	stoppedVehicleCongestionSeconds = 20.0
	congestionRouteRefreshTicks     = 5 * TicksPerSecond
)

type routeKey struct {
	from, to string
	station  bool
	class    VehicleClass
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

// route prefers a free-flow path without intermediate berths. A legacy
// layout without such a path keeps its unrestricted route. Paths are
// read-only within this simulation's immutable network. Snapshots copy them
// before they leave the simulation. Clones share routes of pods and trips.
// Code replaces a whole route and never writes into it in place.
//
// Estimates, candidate searches and the choices of dispatch, positioning
// and parking use route with each routing policy. Only a pod that starts
// a new route gets it from assignedRoute.
func (s *Simulation) route(from, to string) ([]Lane, error) {
	return s.routeForClass(from, to, LegacyClass)
}

func (s *Simulation) routeForClass(from, to string, class VehicleClass) ([]Lane, error) {
	result := s.cachedRouteForClass(from, to, class)
	return result.lanes, result.err
}

// cachedRoute returns the result that route returns. The result can also
// hold the travel time of the route.
func (s *Simulation) cachedRoute(from, to string) routeResult {
	return s.cachedRouteForClass(from, to, LegacyClass)
}

func (s *Simulation) cachedRouteForClass(from, to string, class VehicleClass) routeResult {
	s.ensureNetworkIndexes()
	key := routeKey{from: from, to: to, class: routeClass(class)}
	if cached, ok := s.routes[key]; ok {
		return cached
	}
	lanes, err := s.searchRoute(networkRouteInput{from: from, to: to, class: class, terminalBerthsOnly: true})
	if errors.Is(err, ErrUnreachable) {
		lanes, err = s.searchRoute(networkRouteInput{from: from, to: to, class: class})
	}
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
		result := s.congestionRouteForClass(from, to, podClass(v))
		return result.lanes, result.err
	case QueueRouting:
		return s.queueRoute(v, from, to)
	case PredictiveRouting:
		return s.predictiveRoute(v, from, to)
	default:
		return s.routeForClass(from, to, podClass(v))
	}
}

// congestionRoute minimizes travel time plus congestion cost without
// intermediate berths. If no such path exists, it returns the route
// selected by the free-flow preference and legacy fallback.
func (s *Simulation) congestionRouteForClass(from, to string, class VehicleClass) routeResult {
	s.refreshCongestionCosts()
	key := routeKey{from: from, to: to, class: routeClass(class)}
	if cached, ok := s.congestionRoutes[key]; ok {
		return cached
	}
	lanes, err := s.searchRoute(networkRouteInput{from: from, to: to, class: class, extraCost: s.congestionRouteCosts, terminalBerthsOnly: true})
	if err != nil {
		lanes, err = s.routeForClass(from, to, class)
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
		if owner.isZero() || claimed.kind != trackResource {
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
// in the route cache. A preferred search and an optional fallback give
// the routes. Later route calls do not search again. The cache holds only routes
// that route returns, so this changes no result.
func (s *Simulation) cacheStationRoutes(from string, berths []Berth) {
	s.cacheStationRoutesForClass(from, berths, LegacyClass)
}

func (s *Simulation) cacheStationRoutesForClass(from string, berths []Berth, class VehicleClass) {
	if len(berths) < 2 || s.network.hasStationBanks() {
		return
	}
	s.ensureNetworkIndexes()
	var targets []string
	for _, berth := range berths {
		if _, cached := s.routes[routeKey{from: from, to: berth.Node, class: routeClass(class)}]; !cached && !slices.Contains(targets, berth.Node) {
			targets = append(targets, berth.Node)
		}
	}
	if len(targets) < 2 {
		return
	}
	preferred := s.network.preferredTargets(routeTargetsInput{from: from, to: targets, class: class}, s.graph)
	for index, result := range s.network.routesFromTargets(preferred, s.graph) {
		s.cacheRoute(routeKey{from: from, to: targets[index], class: routeClass(class)}, result)
	}
}

func (s *Simulation) stationPath(from, to string) ([]Lane, error) {
	return s.stationPathForClass(from, to, LegacyClass)
}

func (s *Simulation) stationPathForClass(from, to string, class VehicleClass) ([]Lane, error) {
	s.ensureNetworkIndexes()
	key := routeKey{from: from, to: to, station: true, class: routeClass(class)}
	if cached, ok := s.routes[key]; ok {
		return cached.lanes, cached.err
	}
	lanes, err := s.searchRoute(networkRouteInput{from: from, to: to, class: class, forbidden: s.stationForbidden})
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
	s.pickupBounds = nil
	s.routeWork = nil
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
