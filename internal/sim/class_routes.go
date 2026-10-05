package sim

import "slices"

const topologyClass VehicleClass = "*"

// effectiveClass keeps omitted legacy class metadata unchanged in public state.
func effectiveClass(class VehicleClass) VehicleClass {
	if class == "" {
		return LegacyClass
	}
	return class
}

func classMask(set ClassSet) uint8 {
	if set == 0 {
		return 3
	}
	if set & ^allClassBits != 0 {
		return 0
	}
	return uint8(set)
}

func indexRouteClasses(g *routeGraph, network Network) {
	g.nodeClasses = make([]uint8, len(network.Nodes))
	g.laneClasses = make([]uint8, len(network.Lanes))
	for i := range g.nodeClasses {
		g.nodeClasses[i] = 15
	}
	stations := make(map[string]uint8, len(network.Stations))
	for _, station := range network.Stations {
		if station.VehicleClasses != 0 {
			g.classRestrictions = true
		}
		mask := classMask(station.VehicleClasses)
		stations[station.ID] = mask
		gates := []string{station.Entry, station.Exit}
		for _, bank := range station.Banks {
			gates = append(gates, bank.Entry, bank.Exit)
		}
		for _, id := range gates {
			if node, ok := g.nodes[id]; ok {
				g.nodeClasses[node] &= mask
			}
		}
		for _, berth := range station.Berths {
			if berth.VehicleClasses != 0 {
				g.classRestrictions = true
			}
			if node, ok := g.nodes[berth.Node]; ok {
				g.nodeClasses[node] &= mask & classMask(berth.VehicleClasses)
			}
		}
	}
	for i, lane := range network.Lanes {
		if lane.VehicleClasses != 0 {
			g.classRestrictions = true
		}
		if _, ok := g.nodes[lane.From]; !ok {
			continue
		}
		if _, ok := g.nodes[lane.To]; !ok {
			continue
		}
		mask := classMask(lane.VehicleClasses)
		if station, ok := stations[lane.StationID]; ok {
			mask &= station
		}
		g.laneClasses[i] = mask & g.nodeClasses[g.edges[i].from] & g.nodeClasses[g.edges[i].to]
	}
}

func (g routeGraph) nodeAllows(node int, class VehicleClass) bool {
	if class == topologyClass || !g.classRestrictions && (class == "" || class == LegacyClass || class == CompactClass) {
		return true
	}
	return g.nodeClasses[node]&uint8(classBit(string(effectiveClass(class)))) != 0
}

func (g routeGraph) laneAllows(lane int, class VehicleClass) bool {
	if class == topologyClass || !g.classRestrictions && (class == "" || class == LegacyClass || class == CompactClass) {
		return true
	}
	return g.laneClasses[lane]&uint8(classBit(string(effectiveClass(class)))) != 0
}

func berthAllows(station Station, berth Berth, class VehicleClass) bool {
	return station.VehicleClasses.Allows(string(class)) && berth.VehicleClasses.Allows(string(class))
}

// stationsConnectedForClass reports whether a route connects a berth of
// from to a berth of to. Order admission and dispatch use it. While the
// blocked set is not empty, it asks the static graph, so a fault does not
// refuse an order or unbind a trip. The two answers are equal when no lane
// is blocked.
func (s *Simulation) stationsConnectedForClass(from, to Station, class VehicleClass) bool {
	if !from.VehicleClasses.Allows(string(class)) || !to.VehicleClasses.Allows(string(class)) {
		return false
	}
	static := s.blockedActive()
	for _, origin := range from.Berths {
		if !berthAllows(from, origin, class) {
			continue
		}
		for _, destination := range to.Berths {
			if !berthAllows(to, destination, class) {
				continue
			}
			if static {
				if s.staticConnection(origin.Node, destination.Node, false, class) {
					return true
				}
				continue
			}
			if _, err := s.routeForClass(origin.Node, destination.Node, class); err == nil {
				return true
			}
		}
	}
	return false
}

func podClass(v *vehicle) VehicleClass {
	if v == nil {
		return LegacyClass
	}
	return v.Pod.Class
}

func routeClass(class VehicleClass) VehicleClass {
	if effectiveClass(class) == LegacyClass {
		return ""
	}
	return class
}

// preferredFleetSource searches each immutable class separately. A class-free
// reverse tree cannot select a source pod when allowlists differ by class.
func (s *Simulation) preferredFleetSource(input preferredNearestInput) (int, bool) {
	var classes []VehicleClass
	for _, index := range input.rank {
		if index >= 0 {
			class := effectiveClass(s.vehicles[index].Pod.Class)
			found := slices.Contains(classes, class)
			if !found {
				classes = append(classes, class)
			}
		}
	}
	if len(classes) == 1 {
		input.class = classes[0]
		return s.network.preferredNearestIndexed(input, s.routingGraph())
	}
	best := -1
	bestCost := 0.0
	for _, class := range classes {
		rank := make([]int, len(input.rank))
		copy(rank, input.rank)
		for node, index := range rank {
			if index >= 0 && effectiveClass(s.vehicles[index].Pod.Class) != class {
				rank[node] = -1
			}
		}
		search := input
		search.rank, search.class = rank, class
		node, ok := s.network.preferredNearestIndexed(search, s.routingGraph())
		if !ok {
			continue
		}
		result := s.cachedRouteForClass(s.network.Nodes[node].ID, input.from, class)
		if result.err != nil {
			continue
		}
		if best < 0 || result.seconds < bestCost || result.seconds == bestCost && input.rank[node] < input.rank[best] {
			best, bestCost = node, result.seconds
		}
	}
	return best, best >= 0
}
