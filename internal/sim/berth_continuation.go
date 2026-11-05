package sim

// berthFilterForStops admits a berth only when the remaining stops have a path.
// Unrestricted networks keep the existing selection and avoid another search.
func (s *Simulation) berthFilterForStops(class VehicleClass, stops []string) func(Berth) bool {
	return s.berthFilterForStopsOn(false, class, stops)
}

// berthFilterForStopsOn is berthFilterForStops, with its searches on the
// static graph when static is true. See routeOn.
func (s *Simulation) berthFilterForStopsOn(static bool, class VehicleClass, stops []string) func(Berth) bool {
	s.ensureNetworkIndexes()
	if len(stops) == 0 || !s.graph.classRestrictions {
		return nil
	}
	type continuationKey struct {
		node string
		stop int
	}
	known, fits := make(map[continuationKey]bool), make(map[continuationKey]bool)
	var onward func(string, int) bool
	onward = func(from string, index int) bool {
		if index == len(stops) {
			return true
		}
		key := continuationKey{node: from, stop: index}
		if known[key] {
			return fits[key]
		}
		known[key] = true
		station, ok := s.station(stops[index])
		if !ok {
			return false
		}
		for _, berth := range station.Berths {
			if !berthAllows(station, berth, class) {
				continue
			}
			if _, err := s.routeOn(static, from, berth.Node, class); err == nil && onward(berth.Node, index+1) {
				fits[key] = true
				return true
			}
		}
		return false
	}
	return func(berth Berth) bool { return onward(berth.Node, 0) }
}

// berthFilterForVehicle returns the berth filter of the current work of v.
// An assigned pickup pod uses pickupBerthFilter, so while the blocked set
// is not empty it accepts only compatible pickup berths, also on a network
// without class restrictions. An emergency unload has no onward stop, so
// its filter tests only the class of the pod (section 9.2 of the incident
// emergency contract).
func (s *Simulation) berthFilterForVehicle(v *vehicle) func(Berth) bool {
	s.ensureNetworkIndexes()
	if v.op.purpose == opEmergencyUnload {
		if !s.graph.classRestrictions {
			return nil
		}
		station, _ := s.station(v.destinationStation)
		return func(berth Berth) bool { return berthAllows(station, berth, v.Pod.Class) }
	}
	if !s.graph.classRestrictions && !s.blockedActive() {
		return nil
	}
	if v.carriesPassengers() && len(v.Stops) > 0 && v.Stops[0] == v.destinationStation {
		return s.berthFilterForStops(v.Pod.Class, v.Stops[1:])
	}
	for _, trip := range s.waiting {
		if trip.request.PodID == v.Pod.ID && trip.request.legOrigin() == v.destinationStation {
			return s.pickupBerthFilter(v, trip.request)
		}
	}
	return nil
}

func (s *Simulation) candidateRouteForRequest(v *vehicle, request Request, load func(Berth) int) ([]Lane, Berth, bool) {
	key, memoized := s.pickupMissFor(v, request)
	if memoized && s.pickupMisses[key] {
		return nil, Berth{}, false
	}
	route, berth, ok := s.candidateRouteMatching(v, request.legOrigin(), load, s.pickupBerthFilter(v, request))
	if !ok && memoized {
		s.notePickupMiss(key)
	}
	return route, berth, ok
}

// pickupMiss selects a memoized failure of candidateRouteForRequest. A pod
// that is idle at another station starts at the node of its berth. Berth IDs
// are unique in a network, so the berth gives the station and the node. The
// search then reads only this key, the network and the routing graph. It reads
// the raw class, because a station allows a class by its name.
type pickupMiss struct {
	berth, origin, to string
	class             VehicleClass
}

// pickupMissFor returns the key of the failure, and false when the memo does
// not apply. While a fault cuts the ring, each dispatch pass repeats the same
// failed searches for each pickup key, idle candidate, and berth.
//
// The memo applies only while the blocked set is not empty, only to a pod that
// is idle at another station, and only to a leg origin with no banks:
//
//   - A moving pod starts from a node that depends on its route, and
//     keptLaneBlocked reads its own lanes. A pod idle at the origin gets
//     another berth filter. See pickupBerthFilter.
//   - At a station with banks, the load selects a bank, and a berth of that
//     bank must then have a route. Without banks, the load chooses only among
//     the berths that have a route, so a failure does not depend on the load.
func (s *Simulation) pickupMissFor(v *vehicle, request Request) (pickupMiss, bool) {
	s.ensureNetworkIndexes()
	origin := request.legOrigin()
	if !s.blockedActive() || v.Pod.Activity != Idle || v.Pod.StationID == origin {
		return pickupMiss{}, false
	}
	if station, ok := s.station(origin); !ok || station.Banks != nil {
		return pickupMiss{}, false
	}
	return pickupMiss{berth: v.Pod.BerthID, origin: origin, to: request.To, class: v.Pod.Class}, true
}

// notePickupMiss stores a failure. It clears a full memo first, which changes
// no result. Under a routing view, it stores nothing. See cacheRoute.
func (s *Simulation) notePickupMiss(key pickupMiss) {
	if s.routeView != nil {
		return
	}
	if s.pickupMisses == nil {
		s.pickupMisses = make(map[pickupMiss]bool)
	} else if len(s.pickupMisses) >= routeCacheLimit {
		clear(s.pickupMisses)
	}
	s.pickupMisses[key] = true
}

func (s *Simulation) stationApproachForStops(from string, stops []string, class VehicleClass) ([]Lane, error) {
	return s.stationApproachRouteMatching(from, stops[0], class, s.berthFilterForStops(class, stops[1:]))
}

// boardingRouteNeedsChange keeps the current bank only while it admits the new stops.
func (s *Simulation) boardingRouteNeedsChange(v *vehicle, stops []string) bool {
	if stops[0] != v.Stops[0] {
		return true
	}
	accept := s.berthFilterForStops(v.Pod.Class, stops[1:])
	if accept == nil {
		return false
	}
	station, _ := s.station(stops[0])
	return !s.entryHasMatchingBerth(station, station.routeEntry(v.Route, v.destination), v.Pod.Class, accept)
}
