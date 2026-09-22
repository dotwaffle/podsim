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
	station, ok := s.station(stationID)
	if !ok {
		return nil, Berth{}, fmt.Errorf("unknown station %q", stationID)
	}
	var bestRoute []Lane
	var bestBerth Berth
	bestLoad, found := 0, false
	for _, berth := range station.Berths {
		route, err := s.route(fromNode, berth.Node)
		if fromNode == station.Entry {
			route, err = s.stationPath(fromNode, berth.Node)
		}
		if err != nil {
			continue
		}
		load := s.berthLoad(berth)
		if !found || load < bestLoad {
			bestRoute, bestBerth = route, berth
			bestLoad, found = load, true
		}
	}
	if found {
		return bestRoute, bestBerth, nil
	}
	return nil, Berth{}, fmt.Errorf("route to %s: %w", stationID, ErrUnreachable)
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
