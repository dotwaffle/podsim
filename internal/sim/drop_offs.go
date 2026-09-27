package sim

import "slices"

// stopKey identifies the free-flow route from a node to the entry of a
// station.
type stopKey struct{ from, station string }

// stationsOnRoute returns the passenger stations that the free-flow route
// from a node to a station passes, in route order. The route passes a
// station when it goes through the start of an approach lane of the
// station, so a pod can turn off to that station there. The list ends with
// the station of the route. It is nil when no route exists.
//
// The stop order is a plan that dispatch makes when a party boards, so it
// uses the free-flow route of route with each routing policy. A leg can
// then take a costed route. The result does not change during a run, so
// stationsOnRoute keeps it.
func (s *Simulation) stationsOnRoute(from, stationID string) []string {
	key := stopKey{from: from, station: stationID}
	if stations, ok := s.routeStations[key]; ok {
		return stations
	}
	if s.approachStations == nil {
		s.approachStations = indexApproachStations(s.network)
	}
	var stations []string
	if route, err := s.stationApproachRoute(from, stationID); err == nil {
		for _, lane := range route {
			for _, id := range s.approachStations[lane.From] {
				if !slices.Contains(stations, id) {
					stations = append(stations, id)
				}
			}
		}
		if !slices.Contains(stations, stationID) {
			stations = append(stations, stationID)
		}
	}
	if s.routeStations == nil {
		s.routeStations = make(map[stopKey][]string)
	}
	s.routeStations[key] = stations
	return stations
}

// indexApproachStations returns the passenger stations of the approach
// lanes that start at each node, in lane order.
func indexApproachStations(network Network) map[string][]string {
	index := make(map[string][]string)
	for _, lane := range network.Lanes {
		if lane.StationRole != StationApproachRole || slices.Contains(index[lane.From], lane.StationID) {
			continue
		}
		if station, ok := network.Station(lane.StationID); ok && !station.ParkingOnly {
			index[lane.From] = append(index[lane.From], lane.StationID)
		}
	}
	return index
}

// dropOffStops returns the stops of a boarding pod with a stop for a party
// that goes to the station to. It reports false when the pod cannot take
// the party.
//
// The pod takes the party when to is a stop of the pod. It also takes the
// party when the pod can add to as a stop:
//   - The pod must hold no track, and it must not have a berth at its next
//     stop, because a new first stop changes the route of the pod.
//   - The new stop must not make more intermediate stops than the limit.
//   - The free-flow route from the boarding berth to the last stop passes
//     to. Then to goes between the stops in route order.
//   - Or the free-flow route from the boarding berth to to passes each stop
//     in the same order. Then to becomes the last stop.
func (s *Simulation) dropOffStops(v *vehicle, to string) ([]string, bool) {
	if slices.Contains(v.Stops, to) {
		return v.Stops, true
	}
	if v.reservedThrough >= 0 || v.destination.ID != "" || len(v.Stops) == 0 || len(v.Stops) > s.sharedRideMaxStops {
		return nil, false
	}
	from := v.journeyOrigin.Node
	plan := s.stationsOnRoute(from, v.lastStop())
	if at := slices.Index(plan, to); at >= 0 {
		insert := slices.IndexFunc(v.Stops, func(stop string) bool {
			index := slices.Index(plan, stop)
			return index < 0 || index > at
		})
		if insert < 0 {
			insert = len(v.Stops)
		}
		return slices.Insert(slices.Clone(v.Stops), insert, to), true
	}
	if isSubsequence(v.Stops, s.stationsOnRoute(from, to)) {
		return append(slices.Clone(v.Stops), to), true
	}
	return nil, false
}

// isSubsequence reports whether each item of part is in whole, in the same
// order.
func isSubsequence(part, whole []string) bool {
	next := 0
	for _, item := range whole {
		if next < len(part) && part[next] == item {
			next++
		}
	}
	return next == len(part)
}

// setBoardingStops gives a boarding pod new stops. When the first stop
// changes, the pod gets the route of assignedApproachRoute to the new first
// stop. It reports false and does not change the pod when that route does
// not exist.
func (s *Simulation) setBoardingStops(v *vehicle, stops []string) bool {
	if stops[0] != v.Stops[0] {
		route, err := s.assignedApproachRoute(v, v.origin.Node, stops[0])
		if err != nil {
			return false
		}
		v.destinationStation = stops[0]
		s.setVehicleRoute(v, route)
	}
	v.Stops = stops
	return true
}
