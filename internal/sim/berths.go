package sim

import "fmt"

// stationApproachRoute routes to the station boundary without choosing a berth.
func (s *Simulation) stationApproachRoute(fromNode, stationID string) ([]Lane, error) {
	return s.stationApproachRouteForClass(fromNode, stationID, LegacyClass)
}

func (s *Simulation) stationApproachRouteForClass(fromNode, stationID string, class VehicleClass) ([]Lane, error) {
	return s.stationApproachRouteMatching(fromNode, stationID, class, nil)
}

func (s *Simulation) stationApproachRouteMatching(fromNode, stationID string, class VehicleClass, accept func(Berth) bool) ([]Lane, error) {
	return s.stationApproachRouteOn(false, fromNode, stationID, class, accept)
}

// stationApproachRouteOn is stationApproachRouteMatching, on the static
// graph when static is true. See routeOn.
func (s *Simulation) stationApproachRouteOn(static bool, fromNode, stationID string, class VehicleClass, accept func(Berth) bool) ([]Lane, error) {
	station, ok := s.station(stationID)
	if !ok {
		return nil, fmt.Errorf("unknown station %q", stationID)
	}
	entry, err := s.stationBankEntryOn(static, fromNode, station, s.berthLoad, class, accept)
	if err != nil {
		return nil, err
	}
	route, err := s.routeOn(static, fromNode, entry, class)
	if err != nil {
		return nil, fmt.Errorf("route to %s: %w", stationID, err)
	}
	return route, nil
}

// assignedApproachRoute is stationApproachRoute for pod v when it starts
// the route. It uses assignedRoute.
func (s *Simulation) assignedApproachRoute(v *vehicle, fromNode, stationID string) ([]Lane, error) {
	return s.assignedApproachRouteMatching(v, fromNode, stationID, nil)
}

func (s *Simulation) assignedApproachRouteMatching(v *vehicle, fromNode, stationID string, accept func(Berth) bool) ([]Lane, error) {
	station, ok := s.station(stationID)
	if !ok {
		return nil, fmt.Errorf("unknown station %q", stationID)
	}
	entry, err := s.stationBankEntryMatching(fromNode, station, s.berthLoad, v.Pod.Class, accept)
	if err != nil {
		return nil, err
	}
	route, err := s.assignedRoute(v, fromNode, entry)
	if err != nil {
		return nil, fmt.Errorf("route to %s: %w", stationID, err)
	}
	return route, nil
}

// stationRoute selects a reachable berth with the least assigned demand.
// Berth order breaks equal-load ties.
func (s *Simulation) stationRoute(fromNode, stationID string) ([]Lane, Berth, error) {
	return s.stationRouteByLoad(stationRouteInput{from: fromNode, station: stationID})
}

// stationRouteInput is the input of stationRouteByLoad.
type stationRouteInput struct {
	class         VehicleClass
	from, station string
	// load gives the same value as berthLoad. When it is nil,
	// stationRouteByLoad uses berthLoad.
	load func(Berth) int
	// accept filters passenger continuation before choosing a berth or bank.
	accept func(Berth) bool
}

// stationRouteByLoad is stationRoute with a berth load function from the
// caller. A caller that finds routes to one station for many pods can give
// a function that computes each berth load one time.
func (s *Simulation) stationRouteByLoad(input stationRouteInput) ([]Lane, Berth, error) {
	station, ok := s.station(input.station)
	if !ok {
		return nil, Berth{}, fmt.Errorf("unknown station %q", input.station)
	}
	loadOf := s.berthLoad
	if input.load != nil {
		loadOf = input.load
	}
	entry, err := s.stationBankEntryMatching(input.from, station, loadOf, input.class, input.accept)
	if err != nil {
		return nil, Berth{}, err
	}
	s.cacheStationRoutesForClass(input.from, station.Berths, input.class)
	var bestRoute []Lane
	var bestBerth Berth
	bestLoad, found := 0, false
	for _, berth := range station.Berths {
		if !berthAllows(station, berth, input.class) || input.accept != nil && !input.accept(berth) {
			continue
		}
		if station.Banks != nil && station.berthEntry(berth) != entry {
			continue
		}
		if s.berthBlocked(berth) {
			continue
		}
		route, err := s.routeForClass(input.from, berth.Node, input.class)
		if station.isEntry(input.from) {
			route, err = s.stationPathForClass(input.from, berth.Node, input.class)
		}
		if err != nil {
			continue
		}
		load := loadOf(berth)
		if !found || load < bestLoad {
			bestRoute, bestBerth = route, berth
			bestLoad, found = load, true
		}
	}
	if found {
		return bestRoute, bestBerth, nil
	}
	return nil, Berth{}, fmt.Errorf("route to %s: %w", input.station, ErrUnreachable)
}

// berthLoads returns a function that gives berthLoad for each berth. It
// computes the load of each berth one time and then reuses it. The caller
// must not use the function after a change to the pods, the berth owners,
// or the waiting trips.
func (s *Simulation) berthLoads() func(Berth) int {
	loads := make(map[Berth]int)
	return func(berth Berth) int {
		load, ok := loads[berth]
		if !ok {
			load = s.berthLoad(berth)
			loads[berth] = load
		}
		return load
	}
}

func (s *Simulation) berthLoad(berth Berth) int {
	owners := [2]resourceOwner{
		s.owners[resource{kind: berthResource, id: berth.ID}],
		s.owners[resource{kind: nodeResource, id: berth.Node}],
	}
	load := 0
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if v.Pod.Activity == Idle || v.destination.ID != berth.ID {
			continue
		}
		load++
		for index := range owners {
			if owners[index] == podResourceOwner(v.Pod.ID) {
				owners[index] = resourceOwner{}
			}
		}
	}
	for _, trip := range s.waiting {
		if trip.destination.ID == berth.ID {
			load++
		}
	}
	for index, owner := range owners {
		if owner.isZero() {
			continue
		}
		load++
		for duplicate := index + 1; duplicate < len(owners); duplicate++ {
			if owners[duplicate] == owner {
				owners[duplicate] = resourceOwner{}
			}
		}
	}
	return load
}

func (s *Simulation) berthAvailable(berth Berth) bool {
	if !s.owners[resource{kind: berthResource, id: berth.ID}].isZero() ||
		!s.owners[resource{kind: nodeResource, id: berth.Node}].isZero() {
		return false
	}
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if v.Pod.Activity != Idle && v.destination.ID == berth.ID {
			return false
		}
	}
	return true
}
