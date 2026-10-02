package sim

import "math"

func (s *Simulation) plannedBankDetour(origin string, stops []string, start detourStart) float64 {
	if len(stops) == 0 {
		return 1
	}
	first, _ := s.station(stops[0])
	entry := start.entry
	if start.berth.ID != "" {
		entry = first.berthEntry(start.berth)
	}
	if entry == "" {
		var err error
		entry, err = s.stationBankEntry(origin, first, s.berthLoad)
		if err != nil {
			return math.Inf(1)
		}
	}
	states := map[string]float64{entry: start.ridden}
	largest := 1.0
	for index, stop := range stops {
		station, _ := s.station(stop)
		next := make(map[string]float64)
		found := false
		for gate, ridden := range states {
			for _, berth := range station.Berths {
				if station.berthEntry(berth) != gate || index == 0 && start.berth.ID != "" && berth.ID != start.berth.ID {
					continue
				}
				path, err := s.stationPath(gate, berth.Node)
				if err != nil {
					continue
				}
				arrival := ridden
				if index != 0 || start.berth.ID == "" {
					arrival += s.lanesMeters(path)
				}
				direct := s.directDistance(origin, station.ID, berth)
				if direct <= 0 {
					return math.Inf(1)
				}
				largest, found = max(largest, arrival/direct), true
				if index+1 < len(stops) {
					route, err := s.stationApproachRoute(berth.Node, stops[index+1])
					if err != nil {
						return math.Inf(1)
					}
					onward, _ := s.station(stops[index+1])
					gate := onward.routeEntry(route, Berth{})
					next[gate] = max(next[gate], arrival+s.lanesMeters(route))
				}
			}
		}
		if !found {
			return math.Inf(1)
		}
		states = next
	}
	return largest
}
