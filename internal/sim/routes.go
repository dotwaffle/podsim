package sim

import (
	"errors"
	"slices"
)

const routeCacheLimit = 8192

type routeKey struct {
	from, to string
	station  bool
	class    VehicleClass
}
type routeResult struct {
	lanes []Lane
	err   error
	// seconds is the value of routeSeconds for lanes and a pod of the key
	// class at rest. It is valid only when timed is true. withSeconds sets
	// both.
	seconds float64
	timed   bool
}

// withSeconds returns the result with the value of routeSeconds for its
// lanes and a pod of the class at rest.
func (s *Simulation) withSeconds(result routeResult, class VehicleClass) routeResult {
	result.seconds, result.timed = s.routeSeconds(motionEnvelopeFor(class), result.lanes, motionEstimate{}), true
	return result
}

// route prefers a free-flow path without intermediate berths. A legacy
// layout without such a path keeps its unrestricted route. Paths are
// read-only within this simulation's immutable network. Snapshots copy them
// before they leave the simulation. Clones share routes of pods and trips.
// Code replaces a whole route and never writes into it in place.
//
// Estimates, candidate searches and the choices of dispatch, positioning
// and parking use route. A pod that starts a new route gets it from
// assignedRoute.
func (s *Simulation) route(from, to string) ([]Lane, error) {
	return s.routeForClass(from, to, LegacyClass)
}

func (s *Simulation) routeForClass(from, to string, class VehicleClass) ([]Lane, error) {
	result := s.cachedRouteForClass(from, to, class)
	return result.lanes, result.err
}

// cachedRouteForClass returns the result that route returns. The result
// can also hold the travel time of the route.
func (s *Simulation) cachedRouteForClass(from, to string, class VehicleClass) routeResult {
	key := routeKey{from: from, to: to, class: routeClass(class)}
	if cached, ok := s.routes[key]; ok {
		return cached
	}
	if cached, ok := s.accessRoute(key); ok {
		return cached
	}
	lanes, err := s.searchRoute(networkRouteInput{from: from, to: to, class: class, terminalBerthsOnly: true})
	if errors.Is(err, ErrUnreachable) {
		lanes, err = s.searchRoute(networkRouteInput{from: from, to: to, class: class})
	}
	return s.cacheRoute(key, routeResult{lanes: lanes, err: err})
}

// assignedRoute returns the route for pod v when it starts a new route from
// node from to node to. Pod v can be nil.
func (s *Simulation) assignedRoute(v *vehicle, from, to string) ([]Lane, error) {
	return s.routeForClass(from, to, podClass(v))
}

// cacheStationRoutesForClass puts the routes from a node to each berth of
// a station in the route cache. A preferred search and an optional
// fallback give the routes. Later route calls do not search again. The
// cache holds only routes that route returns, so this changes no result.
func (s *Simulation) cacheStationRoutesForClass(from string, berths []Berth, class VehicleClass) {
	if len(berths) < 2 {
		return
	}
	// A view stores no batch in the route memo. accessRoutes must hold what a
	// single search gives, so it takes no batch.
	if s.routeView != nil && s.routeView.access {
		return
	}
	var targets []string
	for _, berth := range berths {
		if _, cached := s.routes[routeKey{from: from, to: berth.Node, class: routeClass(class)}]; !cached && !slices.Contains(targets, berth.Node) {
			targets = append(targets, berth.Node)
		}
	}
	if len(targets) < 2 {
		return
	}
	graph := s.routingGraph()
	trees := s.batchTrees()
	var results []routeResult
	if s.network.hasStationBanks() {
		results = s.bankStationRoutes(from, targets, class, graph, trees)
	} else {
		results = s.network.treeTargets(routeTargetsInput{from: from, to: targets, class: class}, graph, trees)
	}
	for index, result := range results {
		s.cacheRoute(routeKey{from: from, to: targets[index], class: routeClass(class)}, result)
	}
}

// bankStationRoutes returns the routes of cacheStationRoutesForClass on a
// network with station banks, or nil when an end is not a node of the graph
// or the graph has a bank error. Then each route keeps its own search, which
// gives the error. trees is the tree memo of bankRoutesFrom, or nil.
func (s *Simulation) bankStationRoutes(from string, targets []string, class VehicleClass, graph routeGraph, trees *routeTrees) []routeResult {
	origin, ok := graph.nodes[from]
	if !ok || graph.banks.err != nil {
		return nil
	}
	nodes := make([]int, len(targets))
	for index, target := range targets {
		if nodes[index], ok = graph.nodes[target]; !ok {
			return nil
		}
	}
	return s.network.bankRoutesFrom(networkRouteInput{class: class}, origin, nodes, graph, trees)
}

func (s *Simulation) stationPathForClass(from, to string, class VehicleClass) ([]Lane, error) {
	key := routeKey{from: from, to: to, station: true, class: routeClass(class)}
	if cached, ok := s.routes[key]; ok {
		return cached.lanes, cached.err
	}
	if cached, ok := s.accessRoute(key); ok {
		return cached.lanes, cached.err
	}
	lanes, err := s.searchRoute(networkRouteInput{from: from, to: to, class: class, forbidden: s.stationForbidden})
	s.cacheRoute(key, routeResult{lanes: lanes, err: err})
	return lanes, err
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

// accessRoute returns a free-flow search of an earlier pickupAccess of this
// route epoch. pickupAccess repeats the same searches for each bound trip at
// each dispatch pass, and its view stores nothing in the route memo. Such a
// search reads only the key, the network and the routing graph, so its result
// is the result of a new search. No other query reads accessRoutes, so s.routes,
// s.routeOrder, and each other query stay as they are without it.
func (s *Simulation) accessRoute(key routeKey) (routeResult, bool) {
	if s.routeView == nil || !s.routeView.access {
		return routeResult{}, false
	}
	cached, ok := s.accessRoutes[key]
	return cached, ok
}

// noteAccessRoute stores a result. It clears a full memo first, which changes
// no result.
func (s *Simulation) noteAccessRoute(key routeKey, result routeResult) {
	if s.accessRoutes == nil {
		s.accessRoutes = make(map[routeKey]routeResult)
	} else if len(s.accessRoutes) >= routeCacheLimit {
		clear(s.accessRoutes)
	}
	s.accessRoutes[key] = result
}

// cacheRoute stores the result with its travel time and returns the stored
// value. Under a routing view, it returns the value and stores nothing in the
// route memo.
func (s *Simulation) cacheRoute(key routeKey, result routeResult) routeResult {
	result = s.withSeconds(result, key.class)
	if s.routeView != nil {
		if s.routeView.access {
			s.noteAccessRoute(key, result)
		}
		return result
	}
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

// laneLength reads the length of a lane of the network from the route
// graph, which holds the bits of Network.Length and needs no scan of the
// nodes. It measures a lane outside the graph.
func (s *Simulation) laneLength(lane Lane) float64 {
	if index, ok := s.graph.lanes[lane.ID]; ok {
		return s.graph.lengths[index]
	}
	return s.network.Length(lane)
}
