package sim

import "fmt"

// stationRoute selects a reachable berth and prefers one without an owner.
func (s *Simulation) stationRoute(fromNode, stationID string) ([]Lane, Berth, error) {
	station, ok := s.network.Station(stationID)
	if !ok {
		return nil, Berth{}, fmt.Errorf("unknown station %q", stationID)
	}
	for _, requireFree := range []bool{true, false} {
		for _, berth := range station.Berths {
			if requireFree && !s.berthAvailable(berth) {
				continue
			}
			route, err := s.network.Route(fromNode, berth.Node)
			if err == nil {
				return route, berth, nil
			}
		}
	}
	return nil, Berth{}, fmt.Errorf("route to %s: %w", stationID, ErrUnreachable)
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
			if _, err := s.network.Route(origin.Node, destination.Node); err == nil {
				return true
			}
		}
	}
	return false
}
