package sim

import (
	"fmt"
	"slices"
)

type bankTopology struct {
	station, bank int
	entry, exit   int
	arrival       map[int]bool
	departure     map[int]bool
	local         map[int]bool
}

type bankIndex struct {
	banks []bankTopology
	nodes []int
	lanes []int
	err   error
}

func (n Network) hasStationBanks() bool {
	return slices.ContainsFunc(n.Stations, func(s Station) bool { return s.Banks != nil })
}

// ValidateStationBanks checks bank membership and isolated local paths.
// It does not require routes between passenger stations.
func (n Network) ValidateStationBanks() error {
	if !n.hasStationBanks() {
		return nil
	}
	return newRouteGraph(n).banks.err
}

func indexStationBanks(n Network, graph routeGraph) bankIndex {
	index := bankIndex{}
	if !n.hasStationBanks() {
		return index
	}
	index.nodes, index.lanes = make([]int, len(n.Nodes)), make([]int, len(n.Lanes))
	for i := range index.nodes {
		index.nodes[i] = -1
	}
	for i := range index.lanes {
		index.lanes[i] = -1
	}
	index.err = index.build(n, graph)
	return index
}

func (index *bankIndex) build(n Network, graph routeGraph) error {
	gates, berths := make(map[string]string), make(map[string]bool)
	for _, station := range n.Stations {
		for _, berth := range station.Berths {
			berths[berth.Node] = true
		}
	}
	// Existing legacy gates can coincide. Bank gates cannot share any gate.
	for _, station := range n.Stations {
		if station.Banks == nil {
			gates[station.Entry], gates[station.Exit] = station.ID, station.ID
		}
	}
	for _, station := range n.Stations {
		if station.Banks == nil {
			continue
		}
		if len(station.Banks) == 0 || len(station.Banks) > MaxStationBanks {
			return fmt.Errorf("station %q needs 1 to %d banks", station.ID, MaxStationBanks)
		}
		for _, bank := range station.Banks {
			if bank.Entry == bank.Exit {
				return fmt.Errorf("station %q bank gates must differ", station.ID)
			}
			for _, gate := range []string{bank.Entry, bank.Exit} {
				_, exists := graph.nodes[gate]
				if !exists || berths[gate] || gates[gate] != "" {
					return fmt.Errorf("station %q has an invalid or shared bank gate %q", station.ID, gate)
				}
				gates[gate] = station.ID
			}
		}
	}
	for stationIndex, station := range n.Stations {
		if station.Banks == nil {
			continue
		}
		if station.Entry != station.Banks[0].Entry || station.Exit != station.Banks[0].Exit {
			return fmt.Errorf("station %q aliases must match its first bank", station.ID)
		}
		ids, membership := make(map[string]bool), make(map[string]bool)
		for bankNumber, bank := range station.Banks {
			if bank.ID == "" || len(bank.ID) > 64 || ids[bank.ID] || len(bank.BerthIDs) == 0 || len(bank.BerthIDs) > 200 {
				return fmt.Errorf("station %q has invalid bank %q", station.ID, bank.ID)
			}
			ids[bank.ID] = true
			topology := bankTopology{station: stationIndex, bank: bankNumber, entry: graph.nodes[bank.Entry], exit: graph.nodes[bank.Exit], arrival: make(map[int]bool), departure: make(map[int]bool), local: make(map[int]bool)}
			for _, id := range bank.BerthIDs {
				berth, exists := station.berth(id)
				if !exists || membership[id] {
					return fmt.Errorf("station %q has invalid bank berth %q", station.ID, id)
				}
				membership[id] = true
				for _, path := range []struct {
					from, to string
					role     StationLaneRole
					lanes    map[int]bool
				}{{bank.Entry, berth.Node, StationBerthAccessRole, topology.arrival}, {berth.Node, bank.Exit, StationDepartureRole, topology.departure}} {
					lanes, ok := bankPathLanes(n, graph, station.ID, path.from, path.to, path.role, gates, berths)
					if !ok {
						return fmt.Errorf("station %q bank %q lacks a local %s path for %q", station.ID, bank.ID, path.role, id)
					}
					for lane := range lanes {
						path.lanes[lane], topology.local[lane] = true, true
					}
				}
			}
			through := false
			for laneIndex, lane := range n.Lanes {
				if lane.StationID == station.ID && lane.StationRole == StationThroughRole && lane.From == bank.Entry && lane.To == bank.Exit {
					topology.local[laneIndex], through = true, true
				}
			}
			if !through {
				return fmt.Errorf("station %q bank %q needs a direct through lane", station.ID, bank.ID)
			}
			owner := len(index.banks)
			for lane := range topology.local {
				if index.lanes[lane] >= 0 {
					return fmt.Errorf("lane %q belongs to multiple banks", n.Lanes[lane].ID)
				}
				index.lanes[lane] = owner
				for _, node := range []int{graph.edges[lane].from, graph.edges[lane].to} {
					if index.nodes[node] >= 0 && index.nodes[node] != owner {
						return fmt.Errorf("node %q belongs to multiple banks", n.Nodes[node].ID)
					}
					index.nodes[node] = owner
				}
			}
			index.banks = append(index.banks, topology)
		}
		if len(membership) != len(station.Berths) {
			return fmt.Errorf("station %q must assign every berth to one bank", station.ID)
		}
	}
	for laneIndex, lane := range n.Lanes {
		station, exists := n.Station(lane.StationID)
		if exists && station.Banks != nil && bankLocalRole(lane.StationRole) && index.lanes[laneIndex] < 0 {
			return fmt.Errorf("station %q has orphan or cross-bank lane %q", station.ID, lane.ID)
		}
		if index.lanes[laneIndex] >= 0 {
			continue
		}
		for _, node := range []int{graph.edges[laneIndex].from, graph.edges[laneIndex].to} {
			owner := index.nodes[node]
			if owner < 0 {
				continue
			}
			bank := index.banks[owner]
			if node != bank.entry && node != bank.exit {
				return fmt.Errorf("lane %q connects an external road to a bank interior", lane.ID)
			}
			station := n.Stations[bank.station]
			if node == bank.entry && (lane.To != n.Nodes[node].ID || lane.StationID != station.ID || lane.StationRole != StationEntryRole) {
				return fmt.Errorf("lane %q must enter its bank gate as an entry lane", lane.ID)
			}
			if node == bank.exit && (lane.From != n.Nodes[node].ID || lane.StationID != station.ID || lane.StationRole != StationExitRole) {
				return fmt.Errorf("lane %q must leave its bank gate as an exit lane", lane.ID)
			}
		}
	}
	return nil
}

func bankLocalRole(role StationLaneRole) bool {
	return role == StationBerthAccessRole || role == StationDepartureRole || role == StationThroughRole
}

// bankPathLanes includes every edge on a legal local walk, including alternate paths.
func bankPathLanes(n Network, graph routeGraph, station, from, to string, role StationLaneRole, gates map[string]string, berths map[string]bool) (map[int]bool, bool) {
	start, startOK := graph.nodes[from]
	end, endOK := graph.nodes[to]
	if !startOK || !endOK {
		return nil, false
	}
	allowed := func(lane int) bool {
		l := n.Lanes[lane]
		if l.StationID != station || l.StationRole != role {
			return false
		}
		for _, node := range []string{l.From, l.To} {
			if node != from && node != to && (gates[node] != "" || berths[node]) {
				return false
			}
		}
		return l.To != from && l.From != to
	}
	reachable := func(origin int, reverse bool) []bool {
		seen := make([]bool, len(n.Nodes))
		seen[origin] = true
		queue := []int{origin}
		for head := 0; head < len(queue); head++ {
			lanes := graph.outgoing[queue[head]]
			if reverse {
				lanes = graph.incoming[queue[head]]
			}
			for _, lane := range lanes {
				if !allowed(lane) {
					continue
				}
				next := graph.edges[lane].to
				if reverse {
					next = graph.edges[lane].from
				}
				if !seen[next] {
					seen[next] = true
					queue = append(queue, next)
				}
			}
		}
		return seen
	}
	forward, backward := reachable(start, false), reachable(end, true)
	lanes := make(map[int]bool)
	for lane, edge := range graph.edges {
		if allowed(lane) && forward[edge.from] && backward[edge.to] {
			lanes[lane] = true
		}
	}
	return lanes, forward[end]
}

func (station Station) bankForBerth(id string) (StationBank, bool) {
	for _, bank := range station.Banks {
		if slices.Contains(bank.BerthIDs, id) {
			return bank, true
		}
	}
	return StationBank{}, false
}

func (station Station) isEntry(node string) bool {
	if station.Banks == nil {
		return node == station.Entry
	}
	return slices.ContainsFunc(station.Banks, func(b StationBank) bool { return b.Entry == node })
}

func (station Station) isExit(node string) bool {
	if station.Banks == nil {
		return node == station.Exit
	}
	return slices.ContainsFunc(station.Banks, func(b StationBank) bool { return b.Exit == node })
}

func (station Station) berthEntry(berth Berth) string {
	if bank, ok := station.bankForBerth(berth.ID); ok {
		return bank.Entry
	}
	return station.Entry
}

// routeEntry retains the gate commitment in full or truncated arrival routes.
func (station Station) routeEntry(route []Lane, berth Berth) string {
	for _, lane := range slices.Backward(route) {
		if station.isEntry(lane.To) {
			return lane.To
		}
		if station.isEntry(lane.From) {
			return lane.From
		}
	}
	if berth.ID != "" {
		return station.berthEntry(berth)
	}
	return station.Entry
}
