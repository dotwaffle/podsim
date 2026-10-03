package sim

import "math"

func (s *Simulation) plannedBankDetour(origin string, stops []string, start detourStart) float64 {
	return s.plannedRiderBankDetour(riderDetour{origin: origin}, stops, start)
}

func (s *Simulation) plannedRiderBankDetour(rider riderDetour, stops []string, start detourStart) float64 {
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
		from := start.from
		if from == "" {
			from = rider.origin
		}
		entry, err = s.stationBankEntryMatching(from, first, s.berthLoad, start.class, s.berthFilterForStops(start.class, stops[1:]))
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
		accept := s.berthFilterForStops(start.class, stops[index+1:])
		for gate, ridden := range states {
			for _, berth := range station.Berths {
				if rider.destination != "" && !berthAllows(station, berth, start.class) {
					continue
				}
				if accept != nil && !accept(berth) {
					continue
				}
				if station.berthEntry(berth) != gate || index == 0 && start.berth.ID != "" && berth.ID != start.berth.ID {
					continue
				}
				path, err := s.stationPathForClass(gate, berth.Node, start.class)
				if err != nil {
					continue
				}
				arrival := ridden
				if index != 0 || start.berth.ID == "" {
					arrival += s.lanesMeters(path)
				}
				direct := 0.0
				if rider.destination == "" {
					direct = s.directDistanceForClass(rider.origin, station.ID, berth, start.class)
					if direct <= 0 {
						return math.Inf(1)
					}
				}
				ratio := s.plannedArrivalDetour(rider, stop, berth, start.class, arrival, direct)
				largest, found = max(largest, ratio), true
				if index+1 < len(stops) {
					route, err := s.stationApproachForStops(berth.Node, stops[index+1:], start.class)
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
