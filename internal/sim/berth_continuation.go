package sim

// berthFilterForStops admits a berth only when the remaining stops have a path.
// Unrestricted networks keep the existing selection and avoid another search.
func (s *Simulation) berthFilterForStops(class VehicleClass, stops []string) func(Berth) bool {
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
			if _, err := s.routeForClass(from, berth.Node, class); err == nil && onward(berth.Node, index+1) {
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
// without class restrictions.
func (s *Simulation) berthFilterForVehicle(v *vehicle) func(Berth) bool {
	s.ensureNetworkIndexes()
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
	return s.candidateRouteMatching(v, request.legOrigin(), load, s.pickupBerthFilter(v, request))
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
