package sim

import (
	"errors"
	"fmt"
	"math"
)

// bankRoute keeps local paths inside their bank and joins them at the gates.
func (n Network) bankRoute(input networkRouteInput, graph routeGraph, work *routeSearchWork) ([]Lane, error) {
	if graph.banks.err != nil {
		return nil, graph.banks.err
	}
	from, fromOK := graph.nodes[input.from]
	to, toOK := graph.nodes[input.to]
	if !fromOK || !toOK {
		return nil, fmt.Errorf("unknown route endpoint %q or %q", input.from, input.to)
	}
	if from == to {
		return nil, nil
	}
	source, destination := graph.banks.nodes[from], graph.banks.nodes[to]
	offset := input.startCost
	search := func(from, to int, allowed map[int]bool) ([]Lane, error) {
		part := input
		part.from, part.to = n.Nodes[from].ID, n.Nodes[to].ID
		part.bankRaw, part.allowedLanes, part.startCost = true, allowed, offset
		part.bankExternal = allowed == nil
		if len(input.discharge) == 0 && len(input.forecasts) == 0 {
			part.startCost = 0
		}
		route, err := n.routeIndexedWithWork(part, graph, work)
		if err == nil {
			offset = work.distance[to]
		}
		return route, err
	}
	// Arrival reroutes and berth departures use only their local lane set.
	if source >= 0 && source == destination {
		bank := graph.banks.banks[source]
		if to == bank.exit || from == bank.entry || graph.berthStations[from] < 0 && to != bank.entry {
			allowed := bank.arrival
			if to == bank.exit {
				allowed = bank.departure
				if from == bank.entry {
					allowed = make(map[int]bool)
					for lane := range bank.local {
						if n.Lanes[lane].StationRole == StationThroughRole {
							allowed[lane] = true
						}
					}
				}
			}
			if route, err := search(from, to, allowed); err == nil {
				return route, nil
			}
			work.localFailed++
			// A local arrival must not escape and enter another gate.
			if input.forbidden != nil || from == bank.entry || bankArrivalNode(bank, graph, from) {
				return nil, ErrUnreachable
			}
		}
	}
	var prefix, suffix []Lane
	var err error
	if source >= 0 {
		bank := graph.banks.banks[source]
		if from != bank.entry && from != bank.exit {
			prefix, err = search(from, bank.exit, bank.departure)
			if err != nil {
				return nil, err
			}
			from = bank.exit
		}
	}
	suffixFrom, suffixTo := -1, to
	if destination >= 0 {
		bank := graph.banks.banks[destination]
		if to != bank.entry && to != bank.exit {
			suffixFrom, to = bank.entry, bank.entry
		}
	}
	if input.forbidden != nil && (len(prefix) > 0 || suffixFrom >= 0) {
		return nil, ErrUnreachable
	}
	middle, err := search(from, to, nil)
	if err != nil {
		return nil, err
	}
	if suffixFrom >= 0 {
		suffix, err = search(suffixFrom, suffixTo, graph.banks.banks[destination].arrival)
		if err != nil {
			return nil, err
		}
	}
	route := make([]Lane, 0, len(prefix)+len(middle)+len(suffix))
	route = append(route, prefix...)
	route = append(route, middle...)
	return append(route, suffix...), nil
}

// stationBankEntryMatching selects free-flow approach cost, then minimum
// berth load.
func (s *Simulation) stationBankEntryMatching(from string, station Station, load func(Berth) int, class VehicleClass, accept func(Berth) bool) (string, error) {
	return s.stationBankEntryOn(false, from, station, load, class, accept)
}

// stationBankEntryOn is stationBankEntryMatching, on the static graph when
// static is true. See routeOn.
func (s *Simulation) stationBankEntryOn(static bool, from string, station Station, load func(Berth) int, class VehicleClass, accept func(Berth) bool) (string, error) {
	if !station.VehicleClasses.Allows(string(class)) {
		return "", ErrUnreachable
	}
	s.ensureNetworkIndexes()
	if station.Banks == nil {
		if (s.graph.classRestrictions || accept != nil) && !s.entryHasMatchingBerthOn(static, station, station.Entry, class, accept) {
			return "", ErrUnreachable
		}
		return station.Entry, nil
	}
	if node, ok := s.graph.nodes[from]; ok {
		if owner := s.graph.banks.nodes[node]; owner >= 0 {
			bank := s.graph.banks.banks[owner]
			if s.network.Stations[bank.station].ID == station.ID && node != bank.exit && s.graph.berthStations[node] < 0 {
				entry := station.Banks[bank.bank].Entry
				if accept != nil && !s.entryHasMatchingBerthOn(static, station, entry, class, accept) {
					return "", ErrUnreachable
				}
				return entry, nil
			}
		}
	}
	if node, ok := s.graph.nodes[from]; ok {
		if owner := s.graph.banks.nodes[node]; owner >= 0 && s.graph.berthStations[node] >= 0 {
			from = s.network.Nodes[s.graph.banks.banks[owner].exit].ID
		}
	}
	bestEntry, bestCost, bestLoad := "", 0.0, 0
	for _, bank := range station.Banks {
		route, err := s.routeOn(static, from, bank.Entry, class)
		if err != nil {
			continue
		}
		cost := 0.0
		for _, lane := range route {
			// Departure within the origin bank does not change approach order.
			if owner := s.graph.banks.lanes[s.graph.lanes[lane.ID]]; owner >= 0 && lane.StationRole == StationDepartureRole {
				continue
			}
			cost += s.laneLength(lane) / lane.SpeedLimit
		}
		minimum := int(^uint(0) >> 1)
		for _, id := range bank.BerthIDs {
			berth, _ := station.berth(id)
			if !berthAllows(station, berth, class) || accept != nil && !accept(berth) {
				continue
			}
			if _, err := s.stationPathOn(static, bank.Entry, berth.Node, class); err != nil {
				continue
			}
			minimum = min(minimum, load(berth))
		}
		if minimum == int(^uint(0)>>1) {
			continue
		}
		if bestEntry == "" || cost < bestCost || cost == bestCost && minimum < bestLoad {
			bestEntry, bestCost, bestLoad = bank.Entry, cost, minimum
		}
	}
	if bestEntry == "" {
		return "", ErrUnreachable
	}
	return bestEntry, nil
}

func (s *Simulation) entryHasMatchingBerth(station Station, entry string, class VehicleClass, accept func(Berth) bool) bool {
	return s.entryHasMatchingBerthOn(false, station, entry, class, accept)
}

// entryHasMatchingBerthOn is entryHasMatchingBerth, on the static graph
// when static is true. See routeOn.
func (s *Simulation) entryHasMatchingBerthOn(static bool, station Station, entry string, class VehicleClass, accept func(Berth) bool) bool {
	for _, berth := range station.Berths {
		if station.berthEntry(berth) != entry || !berthAllows(station, berth, class) || accept != nil && !accept(berth) {
			continue
		}
		if _, err := s.stationPathOn(static, entry, berth.Node, class); err == nil {
			return true
		}
	}
	return false
}

func bankArrivalNode(bank bankTopology, graph routeGraph, node int) bool {
	for lane := range bank.arrival {
		if graph.edges[lane].from == node || graph.edges[lane].to == node {
			return true
		}
	}
	return false
}

func (n Network) bankNearest(input preferredNearestInput, graph routeGraph) (int, bool) {
	best, seconds := -1, math.Inf(1)
	for node, rank := range input.rank {
		if rank < 0 {
			continue
		}
		from, to := input.from, n.Nodes[node].ID
		if input.reverse {
			from, to = to, from
		}
		route, err := n.routeIndexed(networkRouteInput{from: from, to: to, terminalBerthsOnly: true, class: input.class}, graph)
		if errors.Is(err, ErrUnreachable) {
			route, err = n.routeIndexed(networkRouteInput{from: from, to: to, class: input.class}, graph)
		}
		if err != nil {
			continue
		}
		cost := 0.0
		for _, lane := range route {
			cost += graph.edges[graph.lanes[lane.ID]].seconds
		}
		if input.limit > 0 && cost > input.limit {
			continue
		}
		if best < 0 || cost < seconds || cost == seconds && rank < input.rank[best] {
			best, seconds = node, cost
		}
	}
	return best, best >= 0
}
