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

// bankBuild holds the inputs that the passes of a bank index build share.
type bankBuild struct {
	index  *bankIndex
	n      Network
	graph  routeGraph
	gates  map[string]string // gate node ID to station ID
	berths map[string]bool   // berth node IDs
}

// build validates the station banks and assigns their local lanes and
// nodes. Each pass completes for every station before the next pass
// starts, so the pass order sets which kind of error comes first. Among
// several ownership conflicts, map order picks the one that is reported.
func (index *bankIndex) build(n Network, graph routeGraph) error {
	b := &bankBuild{index: index, n: n, graph: graph, gates: make(map[string]string), berths: make(map[string]bool)}
	for _, station := range n.Stations {
		for _, berth := range station.Berths {
			b.berths[berth.Node] = true
		}
	}
	// Existing legacy gates can coincide. Bank gates cannot share any gate.
	for _, station := range n.Stations {
		if station.Banks == nil {
			b.gates[station.Entry], b.gates[station.Exit] = station.ID, station.ID
		}
	}
	for _, station := range n.Stations {
		if station.Banks == nil {
			continue
		}
		if err := b.addStationGates(station); err != nil {
			return err
		}
	}
	for stationIndex, station := range n.Stations {
		if station.Banks == nil {
			continue
		}
		if err := b.addStation(stationIndex, station); err != nil {
			return err
		}
	}
	for laneIndex, lane := range n.Lanes {
		if err := b.checkLane(laneIndex, lane); err != nil {
			return err
		}
	}
	return nil
}

// addStationGates claims the entry and exit gates of each bank of a
// station. A bank gate must be a graph node that is not a berth or another
// gate.
func (b *bankBuild) addStationGates(station Station) error {
	if len(station.Banks) == 0 || len(station.Banks) > MaxStationBanks {
		return fmt.Errorf("station %q needs 1 to %d banks", station.ID, MaxStationBanks)
	}
	for _, bank := range station.Banks {
		if bank.Entry == bank.Exit {
			return fmt.Errorf("station %q bank gates must differ", station.ID)
		}
		for _, gate := range []string{bank.Entry, bank.Exit} {
			_, exists := b.graph.nodes[gate]
			if !exists || b.berths[gate] || b.gates[gate] != "" {
				return fmt.Errorf("station %q has an invalid or shared bank gate %q", station.ID, gate)
			}
			b.gates[gate] = station.ID
		}
	}
	return nil
}

// addStation indexes the banks of a station in order. Each berth of the
// station must belong to exactly one bank.
func (b *bankBuild) addStation(stationIndex int, station Station) error {
	if station.Entry != station.Banks[0].Entry || station.Exit != station.Banks[0].Exit {
		return fmt.Errorf("station %q aliases must match its first bank", station.ID)
	}
	ids, membership := make(map[string]bool), make(map[string]bool)
	for bankNumber, bank := range station.Banks {
		if err := b.addBank(stationIndex, bankNumber, station, bank, ids, membership); err != nil {
			return err
		}
	}
	if len(membership) != len(station.Berths) {
		return fmt.Errorf("station %q must assign every berth to one bank", station.ID)
	}
	return nil
}

// addBank collects the local lanes of one bank, which are its berth paths
// and its through lanes, and assigns them and their nodes to the bank.
func (b *bankBuild) addBank(stationIndex, bankNumber int, station Station, bank StationBank, ids, membership map[string]bool) error {
	if bank.ID == "" || len(bank.ID) > 64 || ids[bank.ID] || len(bank.BerthIDs) == 0 || len(bank.BerthIDs) > 200 {
		return fmt.Errorf("station %q has invalid bank %q", station.ID, bank.ID)
	}
	ids[bank.ID] = true
	topology := bankTopology{station: stationIndex, bank: bankNumber, entry: b.graph.nodes[bank.Entry], exit: b.graph.nodes[bank.Exit], arrival: make(map[int]bool), departure: make(map[int]bool), local: make(map[int]bool)}
	for _, id := range bank.BerthIDs {
		berth, exists := station.berth(id)
		if !exists || membership[id] {
			return fmt.Errorf("station %q has invalid bank berth %q", station.ID, id)
		}
		membership[id] = true
		if err := b.addBerthPaths(station.ID, bank, id, berth.Node, &topology); err != nil {
			return err
		}
	}
	if err := b.addThroughLanes(station.ID, bank, &topology); err != nil {
		return err
	}
	if err := b.claimLocal(&topology); err != nil {
		return err
	}
	b.index.banks = append(b.index.banks, topology)
	return nil
}

// addBerthPaths adds the arrival path from the bank entry to a berth and
// the departure path from the berth to the bank exit.
func (b *bankBuild) addBerthPaths(stationID string, bank StationBank, berthID, berthNode string, topology *bankTopology) error {
	for _, path := range []struct {
		from, to string
		role     StationLaneRole
		lanes    map[int]bool
	}{{bank.Entry, berthNode, StationBerthAccessRole, topology.arrival}, {berthNode, bank.Exit, StationDepartureRole, topology.departure}} {
		lanes, ok := bankPathLanes(b.n, b.graph, stationID, path.from, path.to, path.role, b.gates, b.berths)
		if !ok {
			return fmt.Errorf("station %q bank %q lacks a local %s path for %q", stationID, bank.ID, path.role, berthID)
		}
		for lane := range lanes {
			path.lanes[lane], topology.local[lane] = true, true
		}
	}
	return nil
}

// addThroughLanes adds every direct through lane from the bank entry to
// the bank exit. A bank needs at least one.
func (b *bankBuild) addThroughLanes(stationID string, bank StationBank, topology *bankTopology) error {
	through := false
	for laneIndex, lane := range b.n.Lanes {
		if lane.StationID == stationID && lane.StationRole == StationThroughRole && lane.From == bank.Entry && lane.To == bank.Exit {
			topology.local[laneIndex], through = true, true
		}
	}
	if !through {
		return fmt.Errorf("station %q bank %q needs a direct through lane", stationID, bank.ID)
	}
	return nil
}

// claimLocal assigns each local lane of the next bank, and the nodes at its
// ends, to that bank. Banks cannot share a local lane or a node.
func (b *bankBuild) claimLocal(topology *bankTopology) error {
	index := b.index
	owner := len(index.banks)
	for lane := range topology.local {
		if index.lanes[lane] >= 0 {
			return fmt.Errorf("lane %q belongs to multiple banks", b.n.Lanes[lane].ID)
		}
		index.lanes[lane] = owner
		for _, node := range []int{b.graph.edges[lane].from, b.graph.edges[lane].to} {
			if index.nodes[node] >= 0 && index.nodes[node] != owner {
				return fmt.Errorf("node %q belongs to multiple banks", b.n.Nodes[node].ID)
			}
			index.nodes[node] = owner
		}
	}
	return nil
}

// checkLane rejects a local lane of a banked station that no bank owns.
// A lane outside every bank can touch a bank only at its gates.
func (b *bankBuild) checkLane(laneIndex int, lane Lane) error {
	station, exists := b.n.Station(lane.StationID)
	if exists && station.Banks != nil && bankLocalRole(lane.StationRole) && b.index.lanes[laneIndex] < 0 {
		return fmt.Errorf("station %q has orphan or cross-bank lane %q", station.ID, lane.ID)
	}
	if b.index.lanes[laneIndex] >= 0 {
		return nil
	}
	for _, node := range []int{b.graph.edges[laneIndex].from, b.graph.edges[laneIndex].to} {
		if err := b.checkExternalEnd(node, lane); err != nil {
			return err
		}
	}
	return nil
}

// checkExternalEnd checks one end node of a lane outside every bank. At a
// bank node, the lane must be an entry lane into the bank entry or an exit
// lane out of the bank exit.
func (b *bankBuild) checkExternalEnd(node int, lane Lane) error {
	owner := b.index.nodes[node]
	if owner < 0 {
		return nil
	}
	bank := b.index.banks[owner]
	if node != bank.entry && node != bank.exit {
		return fmt.Errorf("lane %q connects an external road to a bank interior", lane.ID)
	}
	station := b.n.Stations[bank.station]
	if node == bank.entry && (lane.To != b.n.Nodes[node].ID || lane.StationID != station.ID || lane.StationRole != StationEntryRole) {
		return fmt.Errorf("lane %q must enter its bank gate as an entry lane", lane.ID)
	}
	if node == bank.exit && (lane.From != b.n.Nodes[node].ID || lane.StationID != station.ID || lane.StationRole != StationExitRole) {
		return fmt.Errorf("lane %q must leave its bank gate as an exit lane", lane.ID)
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
