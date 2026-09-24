package sim

import "fmt"

// stationApproachRoute routes to the station boundary without choosing a berth.
func (s *Simulation) stationApproachRoute(fromNode, stationID string) ([]Lane, error) {
	station, ok := s.station(stationID)
	if !ok {
		return nil, fmt.Errorf("unknown station %q", stationID)
	}
	route, err := s.route(fromNode, station.Entry)
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
	from, station string
	// load gives the same value as berthLoad. When it is nil,
	// stationRouteByLoad uses berthLoad.
	load func(Berth) int
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
	var bestRoute []Lane
	var bestBerth Berth
	bestLoad, found := 0, false
	for _, berth := range station.Berths {
		route, err := s.route(input.from, berth.Node)
		if input.from == station.Entry {
			route, err = s.stationPath(input.from, berth.Node)
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
	owners := [2]string{
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
			if owners[index] == v.Pod.ID {
				owners[index] = ""
			}
		}
	}
	for _, trip := range s.waiting {
		if trip.destination.ID == berth.ID {
			load++
		}
	}
	for index, owner := range owners {
		if owner == "" {
			continue
		}
		load++
		for duplicate := index + 1; duplicate < len(owners); duplicate++ {
			if owners[duplicate] == owner {
				owners[duplicate] = ""
			}
		}
	}
	return load
}

func (s *Simulation) berthAvailable(berth Berth) bool {
	if s.owners[resource{kind: berthResource, id: berth.ID}] != "" ||
		s.owners[resource{kind: nodeResource, id: berth.Node}] != "" {
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

func (s *Simulation) stationsConnected(from, to Station) bool {
	for _, origin := range from.Berths {
		for _, destination := range to.Berths {
			if _, err := s.route(origin.Node, destination.Node); err == nil {
				return true
			}
		}
	}
	return false
}
