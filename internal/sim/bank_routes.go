package sim

import (
	"errors"
	"fmt"
	"math"
	"slices"
)

// bankRouteParts holds the searches that the routes from one origin share in
// one bankNearest call, with one value for each terminalBerthsOnly setting.
// The graph, the blocked lanes, and the class do not change during the call.
// Only the origin bank sets the prefix, and only the destination bank sets the
// middle part of a route to a node inside that bank.
type bankRouteParts struct {
	prefix bankRoutePart
	middle []bankRoutePart
}

// bankRoutePart is the result of one search, as lane indexes.
type bankRoutePart struct {
	lanes []int
	err   error
	done  bool
}

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
	return n.bankRouteNodes(input, from, to, graph, work)
}

// bankRouteNodes is bankRoute between node indexes, on a graph with no bank
// error. It does not read input.from or input.to.
func (n Network) bankRouteNodes(input networkRouteInput, from, to int, graph routeGraph, work *routeSearchWork) ([]Lane, error) {
	if from == to {
		return nil, nil
	}
	path, err := n.bankPathNodes(input, from, to, graph, work)
	if err != nil {
		return nil, err
	}
	route := make([]Lane, len(path))
	for index, lane := range path {
		route[index] = n.Lanes[lane]
	}
	return route, nil
}

// bankPathNodes returns the lane indexes of the route of bankRouteNodes. The
// result stays valid only until the next search with work. The parts of a route
// go in one buffer, so the route needs one allocation.
func (n Network) bankPathNodes(input networkRouteInput, from, to int, graph routeGraph, work *routeSearchWork) ([]int, error) {
	work.path = work.path[:0]
	if from == to {
		return work.path, nil
	}
	source, destination := graph.banks.nodes[from], graph.banks.nodes[to]
	search := func(from, to int, allowed map[int]bool) error {
		part := input
		part.bankRaw, part.allowedLanes = true, allowed
		part.bankExternal = allowed == nil
		if err := n.searchNodes(part, from, to, graph, work); err != nil {
			return err
		}
		return work.appendPath(graph, from, to)
	}
	// once adds the part in slot, and searches only for the first call. A
	// call with no slot always searches.
	once := func(slot *bankRoutePart, from, to int, allowed map[int]bool) error {
		if slot == nil {
			return search(from, to, allowed)
		}
		if !slot.done {
			start := len(work.path)
			slot.err = search(from, to, allowed)
			slot.lanes = slices.Clone(work.path[start:])
			slot.done = true
			return slot.err
		}
		work.path = append(work.path, slot.lanes...)
		return slot.err
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
			if err := search(from, to, allowed); err == nil {
				return work.path, nil
			}
			work.localFailed++
			// A local arrival must not escape and enter another gate.
			if input.forbidden != nil || from == bank.entry || bankArrivalNode(bank, graph, from) {
				return nil, ErrUnreachable
			}
		}
	}
	if source >= 0 {
		bank := graph.banks.banks[source]
		if from != bank.entry && from != bank.exit {
			var slot *bankRoutePart
			if input.parts != nil {
				slot = &input.parts.prefix
			}
			if err := once(slot, from, bank.exit, bank.departure); err != nil {
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
	if input.forbidden != nil && (len(work.path) > 0 || suffixFrom >= 0) {
		return nil, ErrUnreachable
	}
	var slot *bankRoutePart
	if input.parts != nil && suffixFrom >= 0 {
		slot = &input.parts.middle[destination]
	}
	if err := once(slot, from, to, nil); err != nil {
		return nil, err
	}
	if suffixFrom >= 0 {
		if err := search(suffixFrom, suffixTo, graph.banks.banks[destination].arrival); err != nil {
			return nil, err
		}
	}
	return work.path, nil
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
	origin, ok := graph.nodes[input.from]
	if !ok || graph.banks.err != nil {
		return -1, false
	}
	best, seconds := -1, math.Inf(1)
	// A search reads none of the work of the search before it, and the
	// counters of this work reach no simulation.
	var work routeSearchWork
	// A reverse call has a different origin for each candidate, so its routes
	// share no prefix.
	var terminal, open *bankRouteParts
	if !input.reverse {
		terminal = &bankRouteParts{middle: make([]bankRoutePart, len(graph.banks.banks))}
		open = &bankRouteParts{middle: make([]bankRoutePart, len(graph.banks.banks))}
	}
	for node, rank := range input.rank {
		if rank < 0 {
			continue
		}
		from, to := origin, node
		if input.reverse {
			from, to = to, from
		}
		path, err := n.bankPathNodes(networkRouteInput{terminalBerthsOnly: true, class: input.class, parts: terminal}, from, to, graph, &work)
		if errors.Is(err, ErrUnreachable) {
			path, err = n.bankPathNodes(networkRouteInput{class: input.class, parts: open}, from, to, graph, &work)
		}
		if err != nil {
			continue
		}
		cost := 0.0
		for _, lane := range path {
			cost += graph.edges[lane].seconds
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
