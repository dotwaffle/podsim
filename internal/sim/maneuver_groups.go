package sim

import (
	"fmt"
	"slices"
)

// compactClassSet admits only Compact pods. Each lane and berth of a
// maneuver group has it.
const compactClassSet ClassSet = 1 << 1

// separationKey identifies a lane or a berth by its index: a lane by its
// index in the network lanes, and a berth by its index in the berths of all
// stations, in station order. The kind keeps a lane and a berth with one
// ID apart.
type separationKey struct {
	kind  separationKeyKind
	index int
}

type separationKeyKind uint8

// The kinds of a separationKey. The zero key names nothing.
const (
	noKeyKind separationKeyKind = iota
	laneKeyKind
	berthKeyKind
)

func laneKey(index int) separationKey  { return separationKey{kind: laneKeyKind, index: index} }
func berthKey(index int) separationKey { return separationKey{kind: berthKeyKind, index: index} }

// indexBerthKeys returns the index of each berth of a network in the
// berths of all stations, in station order. A repeated ID keeps its first
// index.
func indexBerthKeys(network Network) map[string]int {
	keys := make(map[string]int)
	for _, station := range network.Stations {
		for _, berth := range station.Berths {
			if _, ok := keys[berth.ID]; !ok {
				keys[berth.ID] = len(keys)
			}
		}
	}
	return keys
}

// maneuverIndex holds the maneuver groups of a validated network by lane
// and berth. prepareNetwork builds it, and no code writes to it. A network
// with no group has a nil index.
type maneuverIndex struct {
	groups []ManeuverGroup
	// laneGroups holds the index of the group of each group lane, and
	// berthGroups the index of the group of each group berth.
	laneGroups, berthGroups map[string]int
	// reverse holds the lanes on which a pod faces against its direction
	// of travel.
	reverse map[string]bool
	// chainBerths holds the berth of each lane of a closed bay chain.
	chainBerths map[string]string
	// inLanes holds the forward chain lane that ends at each group berth.
	inLanes map[string]string
}

// groupLane reports whether a lane is in a maneuver group.
func (m *maneuverIndex) groupLane(id string) bool {
	if m == nil {
		return false
	}
	_, ok := m.laneGroups[id]
	return ok
}

// laneGroup returns the group of a lane.
func (m *maneuverIndex) laneGroup(id string) (ManeuverGroup, bool) {
	if m == nil {
		return ManeuverGroup{}, false
	}
	group, ok := m.laneGroups[id]
	if !ok {
		return ManeuverGroup{}, false
	}
	return m.groups[group], true
}

// groupJunction returns the junction resource of the group of a lane.
func (m *maneuverIndex) groupJunction(id string) (resource, bool) {
	if m == nil {
		return resource{}, false
	}
	group, ok := m.laneGroups[id]
	if !ok {
		return resource{}, false
	}
	return resource{kind: junctionResource, id: m.groups[group].ID}, true
}

// groupResource reports whether r is the junction resource of a group.
func (m *maneuverIndex) groupResource(r resource) bool {
	return m != nil && r.kind == junctionResource && slices.ContainsFunc(m.groups, func(group ManeuverGroup) bool { return group.ID == r.id })
}

// chainBerth returns the berth of the closed bay chain of a lane.
func (m *maneuverIndex) chainBerth(id string) (string, bool) {
	if m == nil {
		return "", false
	}
	berth, ok := m.chainBerths[id]
	return berth, ok
}

// everyCell returns the resources that each cell of a lane holds: the
// junction of its group, and then the berth of its closed bay chain.
func (m *maneuverIndex) everyCell(id string) []resource {
	junction, ok := m.groupJunction(id)
	if !ok {
		return nil
	}
	every := []resource{junction}
	if berth, ok := m.chainBerth(id); ok {
		every = append(every, resource{kind: berthResource, id: berth})
	}
	return every
}

// auditSkips reports whether the bank audit skips a pair of paths: two
// lanes of one group, or a lane of a closed bay chain and its own berth.
func (m *maneuverIndex) auditSkips(first, second bankAuditPath) bool {
	if first.key.kind == berthKeyKind {
		first, second = second, first
	}
	if first.key.kind != laneKeyKind {
		return false
	}
	if second.key.kind == berthKeyKind {
		berth, ok := m.chainBerths[first.id]
		return ok && berth == second.id
	}
	a, aLane := m.laneGroups[first.id]
	b, bLane := m.laneGroups[second.id]
	return aLane && bLane && a == b
}

// validateManeuvers checks the maneuver groups, in group order. nodes holds
// the node positions. For each group, it refuses, in order: a duplicate ID
// or one equal to a node ID; an unknown station; an unknown or repeated
// lane; an unknown or repeated berth, or a berth of another station; a
// reverse lane outside the group; a lane or berth that admits a class other
// than Compact; a berth without a closed bay chain; and a reverse set that
// differs from the reverse lanes of the chains.
func (n Network) validateManeuvers(nodes map[string]Point) error {
	_, err := newManeuverIndex(n, nodes)
	return err
}

// newManeuverIndex checks the maneuver groups of a network, in the order
// of validateManeuvers, and returns their index. It returns nil when the
// network has no group.
func newManeuverIndex(n Network, nodes map[string]Point) (*maneuverIndex, error) {
	if len(n.ManeuverGroups) == 0 {
		return nil, nil
	}
	m := &maneuverIndex{
		groups: n.ManeuverGroups, laneGroups: make(map[string]int), berthGroups: make(map[string]int),
		reverse: make(map[string]bool), chainBerths: make(map[string]string), inLanes: make(map[string]string),
	}
	lanes := make(map[string]int, len(n.Lanes))
	incident := make(map[string][]int)
	for index, lane := range n.Lanes {
		lanes[lane.ID] = index
		incident[lane.From] = append(incident[lane.From], index)
		if lane.To != lane.From {
			incident[lane.To] = append(incident[lane.To], index)
		}
	}
	berths := make(map[string]Berth)
	berthStations := make(map[string]string)
	berthNodes := make(map[string]bool)
	for _, station := range n.Stations {
		for _, berth := range station.Berths {
			berths[berth.ID], berthStations[berth.ID] = berth, station.ID
			berthNodes[berth.Node] = true
		}
	}
	ids := make(map[string]bool)
	for index, group := range n.ManeuverGroups {
		if _, node := nodes[group.ID]; group.ID == "" || ids[group.ID] || node {
			return nil, fmt.Errorf("maneuver group %q has an invalid or duplicate ID", group.ID)
		}
		ids[group.ID] = true
		if !slices.ContainsFunc(n.Stations, func(station Station) bool { return station.ID == group.StationID }) {
			return nil, fmt.Errorf("maneuver group %q has unknown station %q", group.ID, group.StationID)
		}
		for _, id := range group.LaneIDs {
			if _, known := lanes[id]; !known || m.groupLane(id) {
				return nil, fmt.Errorf("maneuver group %q has an unknown or repeated lane %q", group.ID, id)
			}
			m.laneGroups[id] = index
		}
		for _, id := range group.BerthIDs {
			if _, repeated := m.berthGroups[id]; repeated || berthStations[id] != group.StationID {
				return nil, fmt.Errorf("maneuver group %q has an unknown, repeated, or foreign berth %q", group.ID, id)
			}
			m.berthGroups[id] = index
		}
		for _, id := range group.ReverseLaneIDs {
			if group, ok := m.laneGroups[id]; !ok || group != index || m.reverse[id] {
				return nil, fmt.Errorf("maneuver group %q reverse lane %q is not a unique lane of the group", n.ManeuverGroups[index].ID, id)
			}
			m.reverse[id] = true
		}
		for _, id := range group.LaneIDs {
			if n.Lanes[lanes[id]].VehicleClasses != compactClassSet {
				return nil, fmt.Errorf("maneuver group %q lane %q admits a class other than compact", group.ID, id)
			}
		}
		for _, id := range group.BerthIDs {
			if berths[id].VehicleClasses != compactClassSet {
				return nil, fmt.Errorf("maneuver group %q berth %q admits a class other than compact", group.ID, id)
			}
		}
		chains := maneuverChains{network: n, index: index, lanes: m, incident: incident, berths: berths, berthNodes: berthNodes}
		reverse, flagged := 0, 0
		for _, id := range group.BerthIDs {
			count, marked, ok := chains.add(id)
			if !ok {
				return nil, fmt.Errorf("maneuver group %q berth %q has no closed bay chain", group.ID, id)
			}
			reverse, flagged = reverse+count, flagged+marked
		}
		if reverse != len(group.ReverseLaneIDs) || flagged != reverse {
			return nil, fmt.Errorf("maneuver group %q reverse lanes differ from the reverse lanes of its bay chains", group.ID)
		}
	}
	return m, nil
}

// maneuverChains finds the closed bay chains of the group at index.
type maneuverChains struct {
	network  Network
	index    int
	lanes    *maneuverIndex
	incident map[string][]int
	berths   map[string]Berth
	// berthNodes holds the node of each berth of the network.
	berthNodes map[string]bool
}

// add walks the closed bay chain of a berth from the berth node and adds
// its lanes to chainBerths. It returns the number of reverse chain lanes
// and the number of them that are flagged as reverse lanes, and false when
// the berth has no closed bay chain.
//
// The berth node has exactly two lanes: the forward lane that ends at it
// and the reverse lane that starts at it, over the same two nodes. At each
// next node, the chain goes on when the node has exactly four lanes: the
// two chain lanes of the last step and a new pair of group lanes over the
// same two nodes. A node at which the chain does not go on is the branch
// node, but a node with a reverse lane of the group that starts at it must
// go on. Each chain lane holds the berth, so a reverse chain lane that is
// not flagged, or a flagged lane outside the chains, makes the reverse set
// differ. A chain that reaches a berth node has no closed bay chain. Then
// pair never gives a lane of chain or of another chain: a node of another
// chain has two more lanes, and each node of a chain has exactly four.
func (c maneuverChains) add(berthID string) (count, flagged int, ok bool) {
	node := c.berths[berthID].Node
	forward, reverse, ok := c.pair(node, nil)
	if !ok {
		return 0, 0, false
	}
	var chain []int
	for {
		chain = append(chain, forward, reverse)
		count++
		if c.lanes.reverse[c.network.Lanes[reverse].ID] {
			flagged++
		}
		node = c.network.Lanes[forward].From
		if c.berthNodes[node] {
			return 0, 0, false
		}
		next, back, ok := c.pair(node, chain)
		if !ok {
			if c.flaggedExit(node, chain) {
				return 0, 0, false
			}
			break
		}
		forward, reverse = next, back
	}
	for _, lane := range chain {
		c.lanes.chainBerths[c.network.Lanes[lane].ID] = berthID
	}
	c.lanes.inLanes[berthID] = c.network.Lanes[chain[0]].ID
	return count, flagged, true
}

// pair returns the lanes of a node outside chain when they are exactly one
// group lane that ends at the node and one group lane that starts at it,
// over the same two nodes. The lanes of chain at a node are the two lanes
// of the last step, so the node then has exactly four lanes, and a berth
// node, with an empty chain, has exactly two.
func (c maneuverChains) pair(node string, chain []int) (forward, reverse int, ok bool) {
	forward, reverse = -1, -1
	for _, index := range c.incident[node] {
		if slices.Contains(chain, index) {
			continue
		}
		lane := c.network.Lanes[index]
		if group, in := c.lanes.laneGroups[lane.ID]; !in || group != c.index || lane.From == lane.To {
			return 0, 0, false
		}
		switch {
		case lane.To == node && forward < 0:
			forward = index
		case lane.From == node && reverse < 0:
			reverse = index
		default:
			return 0, 0, false
		}
	}
	if forward < 0 || reverse < 0 || c.network.Lanes[forward].From != c.network.Lanes[reverse].To {
		return 0, 0, false
	}
	return forward, reverse, true
}

// flaggedExit reports whether a reverse lane of the group outside chain
// starts at node.
func (c maneuverChains) flaggedExit(node string, chain []int) bool {
	return slices.ContainsFunc(c.incident[node], func(index int) bool {
		lane := c.network.Lanes[index]
		return lane.From == node && !slices.Contains(chain, index) && c.lanes.reverse[lane.ID] && c.lanes.laneGroups[lane.ID] == c.index
	})
}
