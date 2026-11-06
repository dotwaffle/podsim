package sim

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"sync"
)

// bankRouteParts holds the searches that the routes from one origin share in
// one bankNearest call, with one value for each terminalBerthsOnly setting.
// The graph, the blocked lanes, and the class do not change during the call.
// Only the origin bank sets the prefix, and only the destination bank sets the
// middle part of a route to a node inside that bank.
//
// tree is one search with no target for all middle parts, which all start at
// one node. It gives each target the lanes of a search that stops at the target,
// for these reasons. The filters of a middle part do not read the target, so
// that search is the first part of the tree search, with the same pops and the
// same writes. A pop is never below the pop before it, because a lane adds a
// cost that is not negative. So after the first valid pop of a node, no later
// relaxation is below its distance. The relaxation is strict, so an equal cost
// does not write it, and its previous lane stays. The costs come from the lanes
// of the path, in route order, and not from the distances of the tree.
type bankRouteParts struct {
	prefix bankRoutePart
	middle []bankRoutePart
	tree   *routeSearchWork
}

// bankSearchWork keeps the arrays of the searches of bankNearest for the next
// call, because the arrays of a network at the project limits take about 200 KB
// each and most of their elements do not change. A search resets the nodes that
// the last search wrote, so no result depends on the earlier use of the work.
var bankSearchWork sync.Pool

// takeBankSearchWork returns a work value from bankSearchWork, or a new one.
func takeBankSearchWork() *routeSearchWork {
	if work, ok := bankSearchWork.Get().(*routeSearchWork); ok {
		return work
	}
	return new(routeSearchWork)
}

// release returns the tree of p, if it has one, to bankSearchWork.
func (p *bankRouteParts) release() {
	if p.tree != nil {
		bankSearchWork.Put(p.tree)
	}
}

// treePath adds the lanes from node from to node to, from the tree, to the path
// of work. A disallowed end gives the error of a search that stops at the target.
func (p *bankRouteParts) treePath(n Network, input networkRouteInput, from, to int, graph routeGraph, work *routeSearchWork) error {
	if !graph.nodeAllows(from, input.class) || !graph.nodeAllows(to, input.class) {
		return ErrUnreachable
	}
	if p.tree == nil {
		p.tree = takeBankSearchWork()
		n.searchFrom(input, from, -1, graph, p.tree)
	}
	p.tree.path = work.path
	err := p.tree.appendPath(graph, from, to)
	// The tree goes back to bankSearchWork, so it must not keep the buffer of work.
	work.path, p.tree.path = p.tree.path, nil
	return err
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
		if part.bankExternal && input.parts != nil {
			return input.parts.treePath(n, part, from, to, graph, work)
		}
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
			if lane.StationRole == StationDepartureRole && s.graph.banks.lanes[s.graph.lanes[lane.ID]] >= 0 {
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
	work := takeBankSearchWork()
	defer bankSearchWork.Put(work)
	// A reverse call has a different origin for each candidate, so its routes
	// share no prefix.
	var terminal, open *bankRouteParts
	if !input.reverse {
		terminal = &bankRouteParts{middle: make([]bankRoutePart, len(graph.banks.banks))}
		open = &bankRouteParts{middle: make([]bankRoutePart, len(graph.banks.banks))}
		defer terminal.release()
		defer open.release()
	}
	for node, rank := range input.rank {
		if rank < 0 {
			continue
		}
		from, to := origin, node
		if input.reverse {
			from, to = to, from
		}
		path, err := n.bankPathNodes(networkRouteInput{terminalBerthsOnly: true, class: input.class, parts: terminal}, from, to, graph, work)
		if errors.Is(err, ErrUnreachable) {
			path, err = n.bankPathNodes(networkRouteInput{class: input.class, parts: open}, from, to, graph, work)
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
