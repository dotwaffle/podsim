package sim

import (
	"errors"
	"maps"
	"slices"
)

// blockedSet holds the lanes and berths that meet an active fault
// footprint. A rebuild replaces each member whole, and no code writes into
// a member in place, so a clone can share them.
type blockedSet struct {
	// lanes holds, by lane index, the blocked lanes. It is nil when no lane
	// is blocked.
	lanes []bool
	// berths holds the berth resource of each blocked berth. It is nil when
	// no berth is blocked.
	berths map[resource]bool
	// by gives the fault ID for each footprint resource.
	by map[resource]string
}

// faultFootprint holds the resources that one fault blocks.
type faultFootprint struct {
	id        string
	resources []resource
}

// blockedActive reports whether the blocked set is not empty.
func (s *Simulation) blockedActive() bool {
	return s.blocked.lanes != nil || s.blocked.berths != nil
}

// berthBlocked reports whether an active fault blocks the berth.
func (s *Simulation) berthBlocked(berth Berth) bool {
	return s.blocked.berths[resource{kind: berthResource, id: berth.ID}]
}

// routingGraph returns the graph of the operational route searches: s.graph
// with the blocked lanes. With an empty blocked set, it is s.graph. It does
// not write s.graph, because a prepared network can share it.
func (s *Simulation) routingGraph() routeGraph {
	graph := s.graph
	graph.blocked = s.blocked.lanes
	return graph
}

// setBlocked replaces the blocked set with the set of the footprints, which
// are in record order. Each operation that changes a footprint calls it
// before it returns. When the blocked lanes or berths change, a new epoch
// starts in the same call. Thus no reader sees a cached route, or a cached
// failure, of an earlier epoch.
func (s *Simulation) setBlocked(footprints []faultFootprint) {
	s.ensureNetworkIndexes()
	next := s.blockedFrom(footprints)
	changed := !slices.Equal(next.lanes, s.blocked.lanes) || !maps.Equal(next.berths, s.blocked.berths)
	s.blocked = next
	if changed {
		s.startRouteEpoch()
	}
}

// blockedFrom returns the blocked set of the footprints. A lane is blocked
// when a cell of the lane holds a footprint resource. A berth is blocked
// when its berth resource or its node resource is in a footprint. When two
// footprints hold a resource, the first gives its fault ID.
func (s *Simulation) blockedFrom(footprints []faultFootprint) blockedSet {
	var next blockedSet
	index := s.resourceLanes
	for _, footprint := range footprints {
		for _, r := range footprint.resources {
			if _, ok := next.by[r]; ok {
				continue
			}
			if next.by == nil {
				next.by = make(map[resource]string)
			}
			next.by[r] = footprint.id
			for _, lane := range index[r] {
				if next.lanes == nil {
					next.lanes = make([]bool, len(s.network.Lanes))
				}
				next.lanes[lane] = true
			}
		}
	}
	if next.by == nil {
		return next
	}
	for _, station := range s.network.Stations {
		for _, berth := range station.Berths {
			key := resource{kind: berthResource, id: berth.ID}
			_, own := next.by[key]
			_, node := next.by[resource{kind: nodeResource, id: berth.Node}]
			if !own && !node {
				continue
			}
			if next.berths == nil {
				next.berths = make(map[resource]bool)
			}
			next.berths[key] = true
		}
	}
	return next
}

// startRouteEpoch clears the caches that hold results of routingGraph,
// successes and failures alike, and asks for a reroute pass. The static
// caches and pickupBounds stay, because they depend only on the network.
func (s *Simulation) startRouteEpoch() {
	s.routes, s.routeOrder = nil, nil
	s.congestionRoutes, s.congestionRouteCosts = nil, nil
	s.routeStations = nil
	s.rerouteDue = true
}

// indexResourceLanes returns, for each resource, the indexes of the lanes
// whose cells hold it, in lane order. It is an index of the network, and
// the other network indexes give its lane cells.
func indexResourceLanes(network Network, cells map[string]*laneCells) map[resource][]int {
	index := make(map[resource][]int)
	for laneIndex, lane := range network.Lanes {
		held := cells[lane.ID]
		if held == nil {
			continue
		}
		for _, r := range held.resources {
			if lanes := index[r]; len(lanes) == 0 || lanes[len(lanes)-1] != laneIndex {
				index[r] = append(lanes, laneIndex)
			}
		}
	}
	return index
}

// searchStaticRoute is searchRoute on the static graph s.graph.
func (s *Simulation) searchStaticRoute(input networkRouteInput) ([]Lane, error) {
	if s.routeWork == nil {
		s.routeWork = new(routeSearchWork)
	}
	return s.network.routeIndexedWithWork(input, s.graph, s.routeWork)
}

// staticSearch makes the searches of cachedRouteForClass, or of
// stationPathForClass when station is true, on the static graph.
func (s *Simulation) staticSearch(from, to string, station bool, class VehicleClass) ([]Lane, error) {
	if station {
		return s.searchStaticRoute(networkRouteInput{from: from, to: to, class: class, forbidden: s.stationForbidden})
	}
	lanes, err := s.searchStaticRoute(networkRouteInput{from: from, to: to, class: class, terminalBerthsOnly: true})
	if errors.Is(err, ErrUnreachable) {
		lanes, err = s.searchStaticRoute(networkRouteInput{from: from, to: to, class: class})
	}
	return lanes, err
}

// staticRoute returns the route that routeForClass, or stationPathForClass
// when station is true, gives on the static graph. staticRoutes caches it.
func (s *Simulation) staticRoute(from, to string, station bool, class VehicleClass) ([]Lane, error) {
	s.ensureNetworkIndexes()
	key := routeKey{from: from, to: to, station: station, class: routeClass(class)}
	if cached, ok := s.staticRoutes[key]; ok {
		return cached.lanes, cached.err
	}
	lanes, err := s.staticSearch(from, to, station, class)
	s.staticRoutes = cacheStatic(s.staticRoutes, key, routeResult{lanes: lanes, err: err})
	return lanes, err
}

// staticConnection reports whether staticRoute gives a route.
// staticConnected caches the answer.
func (s *Simulation) staticConnection(from, to string, station bool, class VehicleClass) bool {
	s.ensureNetworkIndexes()
	key := routeKey{from: from, to: to, station: station, class: routeClass(class)}
	if connected, ok := s.staticConnected[key]; ok {
		return connected
	}
	_, err := s.staticSearch(from, to, station, class)
	s.staticConnected = cacheStatic(s.staticConnected, key, err == nil)
	return err == nil
}

// cacheStatic stores a value in a static cache and returns the cache. It
// clears a full cache first. The cache holds only values of the network,
// so the clear changes no result.
func cacheStatic[V any](cache map[routeKey]V, key routeKey, value V) map[routeKey]V {
	if cache == nil {
		cache = make(map[routeKey]V)
	} else if len(cache) >= routeCacheLimit {
		clear(cache)
	}
	cache[key] = value
	return cache
}

// routeOn is routeForClass, on the static graph when static is true.
// Structural checks and detour baselines set static while the blocked set
// is not empty. With an empty blocked set, both graphs give the same
// route, and the cached operational route is the faster lookup.
func (s *Simulation) routeOn(static bool, from, to string, class VehicleClass) ([]Lane, error) {
	if static {
		return s.staticRoute(from, to, false, class)
	}
	return s.routeForClass(from, to, class)
}

// stationPathOn is stationPathForClass, on the static graph when static is
// true. See routeOn.
func (s *Simulation) stationPathOn(static bool, from, to string, class VehicleClass) ([]Lane, error) {
	if static {
		return s.staticRoute(from, to, true, class)
	}
	return s.stationPathForClass(from, to, class)
}
