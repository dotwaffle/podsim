package sim

import (
	"slices"
	"sync"
)

// reachBitsMembers is the number of members above which a reach part also
// keeps a bit set. A part of a node within the project limit of lanes at a
// node has at most 129 members.
const reachBitsMembers = 129

// planeReach answers the plane test of the locations that a pod on a path
// can occupy. For a lane these are the lane, its incident lanes, which the
// front and the rear of the body reach, and each berth at its From node,
// which a departing pod holds until origin release. A berth at its To node
// is not one of them, because a pod holds a berth only when it stops there.
// For a berth they are the berth and the lanes at its node.
//
// Two reaches are separated when every cross-pair of their locations is
// separated (see safetyLocationsSeparated). That is true exactly when each
// location has a group and the two reaches have no group and no node in
// common, so the index keeps these members by node and not locations by
// lane. Its size grows with the lanes and not with the lanes at a node
// squared. It does not change after newPlaneReach.
type planeReach struct {
	lanes, berths []reachParts
	// laneIDs holds the index of the first lane of each ID.
	laneIDs map[string]int
}

// reachParts holds the parts of the reach of one path. A nil part is empty.
type reachParts [3]*reachPart

// reachPart holds the groups and the nodes of some locations, as sorted
// member numbers. bits holds the same set for a part with many members.
type reachPart struct {
	members []int
	bits    []uint64
	// blank is true when a location has no group. Such a location is not
	// separated from any other.
	blank bool
}

func newPlaneReach(n Network) *planeReach {
	type member struct {
		id    string
		group bool
	}
	// A part holds the lanes at a node, the berths at a node, or one berth,
	// which a group tells from the other berths of its node.
	type partKey struct {
		node, berthGroup string
		lanes, oneBerth  bool
	}
	numbers := make(map[member]int)
	parts := make(map[partKey]*reachPart)
	number := func(m member) int {
		value, found := numbers[m]
		if !found {
			value = len(numbers)
			numbers[m] = value
		}
		return value
	}
	add := func(key partKey, group string, nodes ...string) *reachPart {
		part := parts[key]
		if part == nil {
			part = &reachPart{}
			parts[key] = part
		}
		part.blank = part.blank || group == ""
		part.members = append(part.members, number(member{group, true}))
		for _, node := range nodes {
			// A location with no node shares no node with another.
			if node != "" {
				part.members = append(part.members, number(member{node, false}))
			}
		}
		return part
	}
	reach := &planeReach{laneIDs: make(map[string]int, len(n.Lanes))}
	atNode := func(node string) partKey { return partKey{node: node, lanes: true} }
	for index, lane := range n.Lanes {
		if _, repeated := reach.laneIDs[lane.ID]; !repeated {
			reach.laneIDs[lane.ID] = index
		}
		for _, node := range []string{lane.From, lane.To} {
			add(atNode(node), lane.SeparationGroup, lane.From, lane.To)
		}
	}
	for _, station := range n.Stations {
		for _, berth := range station.Berths {
			add(partKey{node: berth.Node}, berth.SeparationGroup, berth.Node)
			own := add(partKey{node: berth.Node, berthGroup: berth.SeparationGroup, oneBerth: true}, berth.SeparationGroup, berth.Node)
			reach.berths = append(reach.berths, reachParts{parts[atNode(berth.Node)], own})
		}
	}
	for _, lane := range n.Lanes {
		reach.lanes = append(reach.lanes, reachParts{parts[atNode(lane.From)], parts[atNode(lane.To)], parts[partKey{node: lane.From}]})
	}
	for _, part := range parts {
		part.seal(len(numbers))
	}
	return reach
}

func (p *reachPart) seal(count int) {
	slices.Sort(p.members)
	p.members = slices.Clip(slices.Compact(p.members))
	if len(p.members) > reachBitsMembers {
		p.bits = make([]uint64, (count+63)/64)
		for _, member := range p.members {
			p.bits[member/64] |= 1 << (member % 64)
		}
	}
}

// overlaps reports whether two parts have a member in common.
func (p *reachPart) overlaps(other *reachPart) bool {
	if p.bits != nil && other.bits != nil {
		for index, word := range p.bits {
			if word&other.bits[index] != 0 {
				return true
			}
		}
		return false
	}
	if len(p.members) > len(other.members) {
		p, other = other, p
	}
	for _, member := range p.members {
		if _, found := slices.BinarySearch(other.members, member); found {
			return true
		}
	}
	return false
}

// separated reports whether every cross-pair of the locations of two
// reaches is on different planes.
func (r reachParts) separated(other reachParts) bool {
	for _, first := range r {
		if first == nil {
			continue
		}
		if first.blank {
			return false
		}
		for _, second := range other {
			if second != nil && (second.blank || first.overlaps(second)) {
				return false
			}
		}
	}
	return true
}

// path returns the parts of the reach of a lane or a berth of the network.
func (r *planeReach) path(key separationKey) reachParts {
	if key.kind == berthKeyKind {
		return r.berths[key.index]
	}
	return r.lanes[key.index]
}

// lazyPlaneReach builds the index of a network when a pair first needs it.
// A network with no large pair on different planes never does.
type lazyPlaneReach struct {
	once    sync.Once
	network Network
	index   *planeReach
}

func (l *lazyPlaneReach) get() *planeReach {
	l.once.Do(func() { l.index = newPlaneReach(l.network) })
	return l.index
}
