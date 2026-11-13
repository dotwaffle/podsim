package sim

import (
	"math"
	"slices"
)

// stopKey identifies the free-flow route from a node to the entry of a
// station.
type stopKey struct {
	from, station string
	class         VehicleClass
}

// stationsOnRouteForClass returns the passenger stations that the
// free-flow route from a node to a station passes, in route order. The
// route passes a station when it goes through the start of an approach
// lane of the station, so a pod can turn off to that station there. The
// list ends with the station of the route. It is nil when no route exists.
//
// The stop order is a plan that dispatch makes when a party boards, so it
// uses the free-flow route of route. The result does not change during a
// run, so stationsOnRouteForClass keeps it.
func (s *Simulation) stationsOnRouteForClass(from, stationID string, class VehicleClass) []string {
	key := stopKey{from: from, station: stationID, class: routeClass(class)}
	if stations, ok := s.routeStations[key]; ok {
		return stations
	}
	if s.approachStations == nil {
		s.approachStations = indexApproachStations(s.network)
	}
	var stations []string
	if route, err := s.stationApproachRouteForClass(from, stationID, class); err == nil {
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
//   - With the new stops, the planned detour ratio of each rider, new or
//     aboard, must be at most maxSharedRideDetour. See cappedStops.
//
// addedStops gives the last three rules.
func (s *Simulation) dropOffStops(v *vehicle, to string) ([]string, bool) {
	if slices.Contains(v.Stops, to) {
		return v.Stops, true
	}
	if v.reservedThrough >= 0 || v.destination.ID != "" {
		return nil, false
	}
	return s.addedStops(v, to)
}

// addedStops returns the stops of pod v with to added as a stop, with the
// stop limit, the route rules and the detour cap of dropOffStops. It
// reports false when these rules refuse the stop. It does not read the
// track or the berth of the pod, and it does not change the pod.
func (s *Simulation) addedStops(v *vehicle, to string) ([]string, bool) {
	if len(v.Stops) == 0 || len(v.Stops) > s.sharedRideMaxStops {
		return nil, false
	}
	from := v.origin.Node
	plan := s.stationsOnRouteForClass(from, v.lastStop(), v.Pod.Class)
	if at := slices.Index(plan, to); at >= 0 {
		insert := slices.IndexFunc(v.Stops, func(stop string) bool {
			index := slices.Index(plan, stop)
			return index < 0 || index > at
		})
		if insert < 0 {
			insert = len(v.Stops)
		}
		return s.cappedStops(v, slices.Insert(slices.Clone(v.Stops), insert, to))
	}
	if isSubsequence(v.Stops, s.stationsOnRouteForClass(from, to, v.Pod.Class)) {
		return s.cappedStops(v, append(slices.Clone(v.Stops), to))
	}
	return nil, false
}

// cappedStops returns stops and true when no rider of pod v goes over
// maxSharedRideDetour with these stops. When the first stop does not
// change and its bank admits the continuation, the pod keeps its route.
// Otherwise the plan uses the free-flow route to a compatible first bank,
// and legRoute gives the pod a route that keeps each rider within the cap.
func (s *Simulation) cappedStops(v *vehicle, stops []string) ([]string, bool) {
	ridden, ok := s.lanesMeters(v.Route), true
	if s.boardingRouteNeedsChange(v, stops) {
		route, err := s.stationApproachForStops(v.origin.Node, stops, v.Pod.Class)
		ridden, ok = s.lanesMeters(route), err == nil
	}
	entry := ""
	if ok && s.network.hasStationBanks() {
		station, _ := s.station(stops[0])
		route := v.Route
		if s.boardingRouteNeedsChange(v, stops) {
			route, _ = s.stationApproachForStops(v.origin.Node, stops, v.Pod.Class)
		}
		entry = station.routeEntry(route, Berth{})
	}
	ridden += v.riddenBase
	if !ok || !s.keepsRiderDetours(v, stops, detourStart{class: v.Pod.Class, from: v.origin.Node, ridden: ridden, entry: entry}) {
		return nil, false
	}
	return stops, true
}

// cappedDetours reports whether the routes of the pods with riders must
// keep each rider within maxSharedRideDetour. This is so only in drop-offs
// mode with sharing on.
func (s *Simulation) cappedDetours() bool {
	return s.sharedRideMode == SharedRideDropOffs && s.sharedRidePartyLimit > 1
}

// leg is the input of legRoute. The riders of a pod rode the distance
// ridden to the node from. stops holds the stops that remain.
type leg struct {
	from   string
	stops  []string
	ridden float64
}

// legRoute returns the route of a pod with riders from leg.from to the
// entry of its next stop. It is the route of assignedApproachRouteMatching.
// legRoute does not check the rider detours of that route. The checks of
// cappedStops, continueJourney and reevaluateTerminalBerth plan with
// free-flow routes.
func (s *Simulation) legRoute(v *vehicle, next leg) ([]Lane, error) {
	return s.assignedApproachRouteMatching(v, next.from, next.stops[0], s.berthFilterForStops(v.Pod.Class, next.stops[1:]))
}

// rerouteKeepsDetours reports whether pod v can take a new route to a
// berth at its next stop. The route starts where the current leg starts.
// When cappedDetours is true and the pod has riders, each rider must stay
// within maxSharedRideDetour with the new route and free-flow routes after
// it. When the pod refuses the berth, it keeps its route, which an earlier
// check planned.
func (s *Simulation) rerouteKeepsDetours(v *vehicle, route []Lane, berth Berth) bool {
	if !s.cappedDetours() || v.RidersAboard() == 0 {
		return true
	}
	start := detourStart{class: v.Pod.Class, from: v.origin.Node, ridden: v.riddenBase + s.lanesMeters(route), berth: berth}
	return s.keepsRiderDetours(v, v.Stops, start)
}

// detourStart is the start of a plan of plannedRiderDetour. ridden is the
// distance that the riders ride to the entry of the first stop. When berth
// is set, the pod goes to that berth at the first stop, and ridden is the
// distance to that berth. from is the actual source node for bank selection.
type detourStart struct {
	class  VehicleClass
	from   string
	ridden float64
	berth  Berth
	entry  string
}

// plannedBerthDetour returns the planned detour ratio of rider at a station
// without banks, with the stops that remain. The ratio is the distance that
// the rider rides to the berth where it leaves the pod, less its baseline,
// over the direct distance of directDistanceForClass to that berth. After
// the first stop, the plan uses free-flow routes, as
// directDistanceForClass does.
//
// The berth at a stop is not known before the pod arrives. Thus the plan
// takes the berth that gives the largest ratio at the stop of the rider,
// and the berth with the longest distance to the next stop at the other
// stops. The distance from the station entry to a berth is the station
// path, as for a route of assignTerminalBerth. It returns +Inf when a route
// that the plan needs does not exist.
func (s *Simulation) plannedBerthDetour(rider riderDetour, stops []string, start detourStart) float64 {
	largest, ridden := 1.0, start.ridden
	for index, stop := range stops {
		station, _ := s.station(stop)
		berths, known := station.Berths, index == 0 && start.berth.ID != ""
		if known {
			berths = []Berth{start.berth}
		}
		// next is the largest planned distance to the entry of the next
		// stop.
		next, found := math.Inf(-1), false
		accept := s.berthFilterForStops(start.class, stops[index+1:])
		for _, berth := range berths {
			if !berthAllows(station, berth, start.class) {
				continue
			}
			if accept != nil && !accept(berth) {
				continue
			}
			path, err := s.stationPathForClass(station.berthEntry(berth), berth.Node, start.class)
			if err != nil {
				continue
			}
			arrival := ridden + s.lanesMeters(path)
			if known {
				arrival = ridden
			}
			ratio := s.plannedArrivalDetour(rider, stop, berth, start.class, arrival)
			largest, found = max(largest, ratio), true
			next = max(next, s.nextStopMeters(berth, stops[index+1:], start.class, arrival))
		}
		if !found || index+1 < len(stops) && math.IsInf(next, -1) {
			return math.Inf(1)
		}
		ridden = next
	}
	return largest
}

// nextStopMeters returns the planned distance to the entry of the first of
// stops when the pod leaves berth after arrival meters. It returns -Inf
// when stops is empty or the route does not exist.
func (s *Simulation) nextStopMeters(berth Berth, stops []string, class VehicleClass, arrival float64) float64 {
	if len(stops) == 0 {
		return math.Inf(-1)
	}
	onward, err := s.stationApproachForStops(berth.Node, stops, class)
	if err != nil {
		return math.Inf(-1)
	}
	return arrival + s.lanesMeters(onward)
}

// lanesMeters returns the length of a list of lanes.
func (s *Simulation) lanesMeters(lanes []Lane) float64 {
	meters := 0.0
	for _, lane := range lanes {
		meters += s.laneLength(lane)
	}
	return meters
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
// changes, the pod gets the route of legRoute to the new first stop. It
// reports false and does not change the pod when that route does not exist.
func (s *Simulation) setBoardingStops(v *vehicle, stops []string) bool {
	if s.boardingRouteNeedsChange(v, stops) {
		route, err := s.legRoute(v, leg{from: v.origin.Node, stops: stops, ridden: v.riddenBase})
		if err != nil {
			return false
		}
		v.destinationStation = stops[0]
		s.setVehicleRoute(v, route)
	}
	v.Stops = stops
	return true
}
