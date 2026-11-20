package sim

import (
	"cmp"
	"math"
	"slices"
)

// envelopeReachSlack widens each bound of the envelope reach. The slack is
// the half width u of the interval of the possible anchors of a pod about
// its published lane position (see localItems). The route distance of a
// pod can differ from that position by two restoreTolerance steps after a
// restore (see restoredDistance), and by the rounding of the route sums.
// The slack is much larger than both.
const envelopeReachSlack = 1e-3

// envelopeReach holds the planes that the envelope of a pod can hold (see
// appendVehicleSafetyLocations), by position on each lane. It is the
// envelope reach of decision D1-B of the shape clearance design.
//
// The spans are the envelope itself: the oracle selects its planes from
// the span at the published lane position of the pod (see
// appendSpanEnvelope). Each lane takes its own anchor front (see
// anchorItems), and a position also holds the envelopes of the possible
// anchors on the lanes next to it (see localItems).
// Each item of an origin keeps the node of its origin, so that the oracle
// can select the origin of the pod. Rule 2 adds the clusters of the
// maneuver group lanes of each span (see closure).
//
// A berth has its own plane and the planes of the lanes at its node.
//
// Each walk along the network takes the shortest distance, so it holds a
// plane for at least each position at which a route holds it. The spans of
// a lane depend only on the network. The audit builds the spans of a lane
// when it first asks for the lane (see laneSpans), so that the cost grows
// with the lanes that the audit asks for. A prepared network builds the
// spans of each lane one time (see compileEnvelopeReach), and then no code
// writes to them. An index with lanes that it did not build yet is not safe
// for concurrent use.
type envelopeReach struct {
	graph reachGraph
	// lanes holds the spans of each lane in lane order, or nil for a lane
	// that laneSpans did not build yet. The spans of a lane follow each
	// other from 0 to the length of the lane.
	lanes [][]reachSpan
	// berths holds the span of each berth in station and berth order.
	berths []reachSpan
	// locations holds the plane of each location number: the lanes in
	// lane order, then the berths in station and berth order. members
	// holds their groups and nodes. berthLocations holds the location
	// number of each berth by ID.
	locations      []SafetyLocation
	members        reachMembers
	berthLocations map[string]int
	// keys numbers the items of an origin, which are not the items of the
	// same location without an origin: a key is a location number, or
	// len(locations) plus the index of a pair in origins. origins holds
	// the location and the node of the origin of each such pair, and
	// originKeys their keys.
	origins    [][2]int
	originKeys map[[2]int]int
	// clusterOf holds the cluster of rule 2 of each location number whose
	// plane is the plane of a maneuver group lane, and -1 for each other
	// location. clusterLanes holds the lanes of each cluster (see
	// maneuverClusters). clusterOf is nil when the network has no group.
	clusterOf    []int
	clusterLanes [][]int
	// walk, near, between, marks, items, anchor, sorted, window, and
	// closed are the storage of the lane builds. built counts the lanes that laneSpans
	// built.
	walk                  reachWalk
	near                  []reachVisit
	between               []int
	marks                 []bool
	items, anchor, sorted []reachItem
	window                reachWindow
	closed                []int
	built                 int
}

// reachSpan is a part of a lane, from from up to to, and the planes that
// an envelope at a position in it can hold.
type reachSpan struct {
	from, to float64
	// locations holds the location numbers of the planes (see
	// envelopeReach.locations), in no order, and a number can occur more
	// than one time. keys holds the key of each (see envelopeReach.keys).
	// The spans of a lane share one array of each. The planes of rule 2
	// are not in locations (see closure).
	locations, keys []int
	// planes holds the groups and the nodes of the planes and of their
	// clusters, for the plane test.
	planes reachSet
}

// separated reports whether each plane of two spans is on a different
// plane from each plane of the other (see safetyLocationsSeparated).
func (s *reachSpan) separated(other *reachSpan) bool {
	return !s.planes.blank && !other.planes.blank && !s.planes.overlaps(&other.planes)
}

// reachSet holds the groups and the nodes of the planes of a span, as
// member numbers (see reachMembers). A set with at most reachBitsMembers
// members holds them in order. A larger set holds only bits, so that it
// uses one bit for each member of the network, and not one number for
// each member of each plane.
type reachSet struct {
	members []int
	bits    []uint64
	// blank is true when a plane has no group. Such a plane is not
	// separated from any other.
	blank bool
}

// overlaps reports whether two sets have a member in common.
func (s *reachSet) overlaps(other *reachSet) bool {
	if s.bits != nil && other.bits != nil {
		for index, word := range s.bits {
			if word&other.bits[index] != 0 {
				return true
			}
		}
		return false
	}
	// Look up the members of the smaller set in the other.
	if s.bits != nil || other.bits == nil && len(s.members) > len(other.members) {
		s, other = other, s
	}
	return slices.ContainsFunc(s.members, other.has)
}

// has reports whether a set holds a member.
func (s *reachSet) has(member int) bool {
	if s.bits != nil {
		return s.bits[member/64]&(1<<(member%64)) != 0
	}
	_, found := slices.BinarySearch(s.members, member)
	return found
}

// laneAt returns the span of a lane at a position. A position at the
// boundary of two spans is in the second span. Each bound includes the
// slack, so the second span holds each plane that the envelope can hold at
// the boundary. A position before 0 is in the first span, and a position
// after the length of the lane is in the last span.
func (r *envelopeReach) laneAt(lane int, position float64) *reachSpan {
	spans := r.laneSpans(lane)
	index, _ := slices.BinarySearchFunc(spans, position, func(span reachSpan, position float64) int {
		return cmp.Compare(span.from, position)
	})
	if index == len(spans) || spans[index].from > position {
		index--
	}
	return &spans[max(0, index)]
}

// reachGraph holds the lanes and the berths of a network by node number.
// Lane numbers are lane indexes, and berth numbers follow the lanes.
type reachGraph struct {
	// from, to, lengths, tails, and fronts hold the end nodes, the length,
	// the retained tail, and the front bound for the envelope (see
	// envelopeFront) of each lane.
	from, to               []int
	lengths, tails, fronts []float64
	// in, out, and incident hold the lanes into, out of, and at each node,
	// as nodeSafety holds them. berthsAt holds the berths at each node.
	in, out, incident, berthsAt [][]int
	// originAt holds the longest origin tail of the lanes out of each
	// node. berthNode holds the node of each berth.
	originAt  []float64
	berthNode []int
	// nodes holds the number of each node by ID.
	nodes map[string]int
	// behind is the longest distance behind a lane at which a route can
	// hold a lane or an origin.
	behind float64
}

// newReachGraph numbers the nodes of a network and indexes its lanes and
// berths by node.
func newReachGraph(n Network, tails map[string]geometryTails) reachGraph {
	g := reachGraph{nodes: make(map[string]int, len(n.Nodes))}
	number := func(id string) int {
		value, found := g.nodes[id]
		if !found {
			value = len(g.nodes)
			g.nodes[id] = value
		}
		return value
	}
	positions := indexNodePositions(n.Nodes)
	points := make([]Point, 0, 65)
	for _, lane := range n.Lanes {
		g.from, g.to = append(g.from, number(lane.From)), append(g.to, number(lane.To))
		points = lanePointsBetween(lane, positions[lane.From], positions[lane.To], points[:0])
		length := 0.0
		for index := 1; index < len(points); index++ {
			length += pointDistance(points[index-1], points[index])
		}
		g.lengths = append(g.lengths, length)
		g.tails = append(g.tails, max(largeClearance, tails[lane.ID].tail))
		g.fronts = append(g.fronts, envelopeFront(lane, tails[lane.ID], func() []Point { return points }))
	}
	for _, station := range n.Stations {
		for _, berth := range station.Berths {
			g.berthNode = append(g.berthNode, number(berth.Node))
		}
	}
	count := len(g.nodes)
	g.in, g.out, g.incident = make([][]int, count), make([][]int, count), make([][]int, count)
	g.berthsAt, g.originAt = make([][]int, count), make([]float64, count)
	for index, lane := range n.Lanes {
		from, to := g.from[index], g.to[index]
		g.out[from] = append(g.out[from], index)
		g.in[to] = append(g.in[to], index)
		g.incident[from] = append(g.incident[from], index)
		if to != from {
			g.incident[to] = append(g.incident[to], index)
		}
		g.originAt[from] = max(g.originAt[from], largeClearance, tails[lane.ID].fromTail)
	}
	for index, node := range g.berthNode {
		g.berthsAt[node] = append(g.berthsAt[node], len(n.Lanes)+index)
	}
	g.behind = largeClearance
	for _, bound := range g.tails {
		g.behind = max(g.behind, bound)
	}
	for _, node := range g.berthNode {
		g.behind = max(g.behind, g.originAt[node])
	}
	return g
}

// anchorFront returns the anchor front B(L) of a lane L:
// max(largeEnvelopeRadius, the front bound of L). It takes no front of
// another lane.
func (g reachGraph) anchorFront(lane int) float64 {
	return max(largeEnvelopeRadius, g.fronts[lane])
}

// newEnvelopeReach returns the envelope reach of a network. tails is the
// tail index of the network (see indexGeometryTails). It builds the spans of
// the berths, and the spans of a lane when laneSpans first asks for them. A
// bound that is not finite makes each walk cover each node that it reaches,
// which is slow and large on a big network. Preparation refuses such a
// network, but only after the bank audit, so a caller in the audit must
// check the bounds first (see finiteReachTails).
func newEnvelopeReach(n Network, tails map[string]geometryTails) *envelopeReach {
	g := newReachGraph(n, tails)
	reach := &envelopeReach{graph: g, lanes: make([][]reachSpan, len(n.Lanes)), locations: make([]SafetyLocation, 0, len(n.Lanes)+len(g.berthNode)), berthLocations: make(map[string]int)}
	for _, lane := range n.Lanes {
		reach.locations = append(reach.locations, SafetyLocation{SeparationGroup: lane.SeparationGroup, From: lane.From, To: lane.To})
	}
	for _, station := range n.Stations {
		for _, berth := range station.Berths {
			reach.berthLocations[berth.ID] = len(reach.locations)
			reach.locations = append(reach.locations, SafetyLocation{SeparationGroup: berth.SeparationGroup, From: berth.Node, To: berth.Node})
		}
	}
	reach.clusterOf, reach.clusterLanes = locationClusters(n, reach.locations)
	reach.originKeys = make(map[[2]int]int)
	for node, berths := range g.berthsAt {
		for _, location := range slices.Concat(berths, g.incident[node]) {
			pair := [2]int{location, node}
			if _, found := reach.originKeys[pair]; !found {
				reach.originKeys[pair] = len(reach.locations) + len(reach.origins)
				reach.origins = append(reach.origins, pair)
			}
		}
	}
	reach.members = newReachMembers(reach.locations)
	reach.window.count = make([]int, len(reach.locations)+len(reach.origins))
	for _, node := range g.berthNode {
		// The berths at the node include the berth.
		reach.items = g.nodeItems(reach.items[:0], node, math.Inf(1))
		reach.berths = append(reach.berths, reach.spans(reach.items, 0)...)
	}
	return reach
}

// compileEnvelopeReach returns the envelope reach of a network, with the
// spans of each lane. No code writes to it after
// that, so it is safe for concurrent use. It returns nil when a bound of
// the tails is not finite (see finiteReachTails): preparation refuses
// such a network.
func compileEnvelopeReach(n Network, tails map[string]geometryTails) *envelopeReach {
	if !finiteReachTails(n, tails) {
		return nil
	}
	reach := newEnvelopeReach(n, tails)
	for lane := range n.Lanes {
		reach.laneSpans(lane)
	}
	reach.walk, reach.near, reach.items, reach.anchor, reach.sorted = reachWalk{}, nil, nil, nil, nil
	reach.window, reach.closed, reach.between, reach.marks = reachWindow{}, nil, nil, nil
	return reach
}

// finiteReachTails reports whether each bound of the tail index of a
// network is finite.
func finiteReachTails(n Network, tails map[string]geometryTails) bool {
	for _, lane := range n.Lanes {
		if bounds := tails[lane.ID]; !finite(bounds.tail) || !finite(bounds.fromTail) || !finite(bounds.front) {
			return false
		}
	}
	return true
}

// locationClusters returns the cluster of rule 2 of each location number
// whose plane is the plane of a maneuver group lane, and -1 for each other
// location, and the lanes of each cluster. It returns nil when the network
// has no group. The oracle applies rule 2 by plane (see appendClusters),
// so the index does too.
func locationClusters(n Network, locations []SafetyLocation) ([]int, [][]int) {
	planes := make(map[SafetyLocation]int)
	var lanes [][]int
	for lane, cluster := range maneuverClusters(n) {
		if cluster == nil {
			continue
		}
		// The lanes of one cluster share one slice.
		id := slices.IndexFunc(lanes, func(other []int) bool { return &other[0] == &cluster[0] })
		if id < 0 {
			id = len(lanes)
			lanes = append(lanes, cluster)
		}
		planes[locations[lane]] = id
	}
	if len(lanes) == 0 {
		return nil, nil
	}
	clusters := make([]int, len(locations))
	for number, location := range locations {
		id, ok := planes[location]
		if !ok {
			id = -1
		}
		clusters[number] = id
	}
	return clusters, lanes
}

// key returns the key of an item (see envelopeReach.keys).
func (r *envelopeReach) key(item reachItem) int {
	if item.origin < 0 {
		return item.location
	}
	return r.originKeys[[2]int{item.location, item.origin}]
}

// keyOrigin returns the node of the origin of a key, or -1 for a key with
// no origin.
func (r *envelopeReach) keyOrigin(key int) int {
	if key < len(r.locations) {
		return -1
	}
	return r.origins[key-len(r.locations)][1]
}

// closure returns the location numbers of a span and the lanes that rule 2
// adds to them: each lane of the cluster of each location of a maneuver
// group plane. It returns the locations of the span when the network has
// no group. The result can share storage
// with the next call.
func (r *envelopeReach) closure(numbers []int) []int {
	if r.clusterOf == nil {
		return numbers
	}
	closed := r.closed[:0]
	closed = append(closed, numbers...)
	var applied []int
	for _, number := range numbers {
		if id := r.clusterOf[number]; id >= 0 && !slices.Contains(applied, id) {
			applied = append(applied, id)
			closed = append(closed, r.clusterLanes[id]...)
		}
	}
	r.closed = closed
	return closed
}

// laneSpans returns the spans of a lane. It builds them when it first gets
// the lane, and keeps them.
func (r *envelopeReach) laneSpans(lane int) []reachSpan {
	if spans := r.lanes[lane]; spans != nil {
		return spans
	}
	r.items = r.localItems(lane)
	r.lanes[lane] = r.spans(r.items, r.graph.lengths[lane])
	r.built++
	return r.lanes[lane]
}

// reachItem is a location and the positions of a lane at which an
// envelope can hold it, from from to to. The bounds include the slack.
// to is the next float64 after the last position of the item, so that the
// item holds that position (see anchorItems). origin is the node of the
// origin of an item of nodeItems, and -1 for each other item.
type reachItem struct {
	location int
	from, to float64
	origin   int
}

// inclusiveLimit returns the walk limit of a budget: the walk reaches each node
// at budget plus the slack or less.
func inclusiveLimit(budget float64) float64 {
	return math.Nextafter(budget+envelopeReachSlack, math.Inf(1))
}

// anchorItems appends to items the items of the envelope of an anchor on a
// lane A. An item holds the
// positions p of A, from 0 to the length of A, at which an anchor within
// the slack u of p has the plane:
//   - A, at each position.
//   - A lane P into a node at the walk distance d behind the From node of
//     A, while p <= (tail(P) - d) + u.
//   - The berths and the lanes at a node of a berth at the walk distance d
//     behind the From node, while p <= (originAt - d) + u, with the node as
//     their origin.
//   - A lane M out of a node at the walk distance d ahead of the To node of
//     A, while p >= ((length(A) + d) - B(A)) - u, where B(A) is the anchor
//     front of A.
//
// Each walk distance is the least sum along lanes, added from the start of
// the walk, and each walk reaches each node at its budget plus u or less
// (see inclusiveLimit). The budget behind is behind, and ahead it is B(A).
func (g reachGraph) anchorItems(walk *reachWalk, lane int, items []reachItem) []reachItem {
	items = append(items, reachItem{location: lane, from: math.Inf(-1), to: math.Inf(1), origin: -1})
	items = g.behindItems(walk, g.from[lane], 0, items)
	return g.aheadItems(walk, lane, g.to[lane], 0, items)
}

// behindItems appends the items behind an anchor on a lane A: a walk back
// from the node start, where the walk distance d of a node is base plus
// the least sum from start, added in that order. A lane P into a node is
// held while p <= (tail(P) - d) + u, and the berths and the lanes at a node
// of a berth while p <= (originAt - d) + u.
func (g reachGraph) behindItems(walk *reachWalk, start int, base float64, items []reachItem) []reachItem {
	walk.runTo(start, inclusiveLimit(g.behind), g.in, g.from, g.lengths)
	for _, node := range walk.reached {
		distance := base + walk.distance[node]
		for _, previous := range g.in[node] {
			if to := g.tails[previous] - distance + envelopeReachSlack; to >= 0 {
				items = append(items, reachItem{location: previous, from: math.Inf(-1), to: math.Nextafter(to, math.Inf(1)), origin: -1})
			}
		}
		if len(g.berthsAt[node]) > 0 {
			if to := g.originAt[node] - distance + envelopeReachSlack; to >= 0 {
				items = g.nodeItems(items, node, math.Nextafter(to, math.Inf(1)))
			}
		}
	}
	return items
}

// aheadItems appends the items ahead of an anchor on a lane A: a walk
// ahead from the node start, where the walk distance d of a node is base
// plus the least sum from start, added in that order. A lane M out of a
// node is held while p >= ((length(A) + d) - B(A)) - u.
func (g reachGraph) aheadItems(walk *reachWalk, lane, start int, base float64, items []reachItem) []reachItem {
	length, front := g.lengths[lane], g.anchorFront(lane)
	walk.runTo(start, inclusiveLimit(front), g.out, g.to, g.lengths)
	for _, node := range walk.reached {
		from := length + (base + walk.distance[node]) - front - envelopeReachSlack
		for _, next := range g.out[node] {
			items = append(items, reachItem{location: next, from: from, to: math.Inf(1), origin: -1})
		}
	}
	return items
}

// throughItems appends the items of an anchor on a lane A next to the lane
// L of a published position, along the paths through L only. The anchor is
// within the slack of the published position, so the route of the pod
// goes from A through the short lanes between A and L to L, or from L
// through them to A. On the side of L, the items come from the walk through
// L, and not from the walk at the end of A, so a lane of a diverge or a
// merge next to these lanes is not an item. near holds the nodes within the
// slack of L on the side of A, between the lanes from a node of near to a
// node of near, and distance the walk distance d between A and L.
//   - A, L, and each lane of between.
//   - When A is before L: the items behind A (see behindItems), and the
//     items ahead of A from the walk ahead of the To node of L, with the
//     walk distance (d + length(L)) + w, where w is the least sum from that
//     node.
//   - When A is after L: the items ahead of A (see aheadItems), the
//     berths and the lanes at each node of near, and the items behind A from
//     the walk back from the From node of L, with the walk distance
//     (d + length(L)) + w.
func (g reachGraph) throughItems(walk *reachWalk, anchor, lane int, distance float64, near []reachVisit, between []int, before bool, items []reachItem) []reachItem {
	items = append(items, reachItem{location: anchor, from: math.Inf(-1), to: math.Inf(1), origin: -1}, reachItem{location: lane, from: math.Inf(-1), to: math.Inf(1), origin: -1})
	for _, other := range between {
		items = append(items, reachItem{location: other, from: math.Inf(-1), to: math.Inf(1), origin: -1})
	}
	base := distance + g.lengths[lane]
	if before {
		items = g.behindItems(walk, g.from[anchor], 0, items)
		return g.aheadItems(walk, anchor, g.to[lane], base, items)
	}
	for _, visit := range near {
		if len(g.berthsAt[visit.node]) > 0 {
			items = g.nodeItems(items, visit.node, math.Inf(1))
		}
	}
	items = g.aheadItems(walk, anchor, g.to[anchor], 0, items)
	return g.behindItems(walk, g.from[lane], base, items)
}

// betweenLanes returns the lanes from a node of near to a node of near.
// marks has one entry for each node, each false, and it is false again on
// return.
func (g reachGraph) betweenLanes(marks []bool, near []reachVisit, between []int) []int {
	for _, visit := range near {
		marks[visit.node] = true
	}
	for _, visit := range near {
		for _, other := range g.in[visit.node] {
			if marks[g.from[other]] {
				between = append(between, other)
			}
		}
	}
	for _, visit := range near {
		marks[visit.node] = false
	}
	return between
}

// localItems returns the items of a lane L. A pod at the published position p
// of L can have its anchor at each position within the slack u of p along
// lanes, also on the lanes before and after L, through any number of short
// lanes. The items are the union of the envelopes of these anchors, each with
// the anchor front and the distances of its own lane:
//   - The items of an anchor on L (see anchorItems).
//   - For each lane A into a node at the walk distance d <= u behind the
//     From node of L: while p <= u - d, each item of A along the paths
//     through L (see throughItems) that holds a position of A from
//     max(0, length(A) - u) to length(A).
//   - For each lane A out of a node at the walk distance d <= u ahead of
//     the To node of L: while p >= (length(L) + d) - u, each item of A
//     along the paths through L that holds a position of A from 0 to
//     min(u, length(A)).
//
// The items of A keep their origins.
func (r *envelopeReach) localItems(lane int) []reachItem {
	g := &r.graph
	if len(r.marks) < len(g.in) {
		r.marks = make([]bool, len(g.in))
	}
	items := g.anchorItems(&r.walk, lane, r.items[:0])
	r.walk.runTo(g.from[lane], inclusiveLimit(0), g.in, g.from, g.lengths)
	r.near = r.near[:0]
	for _, node := range r.walk.reached {
		r.near = append(r.near, reachVisit{node: node, distance: r.walk.distance[node]})
	}
	r.between = g.betweenLanes(r.marks, r.near, r.between[:0])
	for _, visit := range r.near {
		to := math.Nextafter(envelopeReachSlack-visit.distance, math.Inf(1))
		for _, anchor := range g.in[visit.node] {
			length := g.lengths[anchor]
			r.anchor = g.throughItems(&r.walk, anchor, lane, visit.distance, r.near, r.between, true, r.anchor[:0])
			items = r.neighborItems(items, max(0, length-envelopeReachSlack), length, math.Inf(-1), to)
		}
	}
	r.walk.runTo(g.to[lane], inclusiveLimit(0), g.out, g.to, g.lengths)
	r.near = r.near[:0]
	for _, node := range r.walk.reached {
		r.near = append(r.near, reachVisit{node: node, distance: r.walk.distance[node]})
	}
	r.between = g.betweenLanes(r.marks, r.near, r.between[:0])
	for _, visit := range r.near {
		from := g.lengths[lane] + visit.distance - envelopeReachSlack
		for _, anchor := range g.out[visit.node] {
			r.anchor = g.throughItems(&r.walk, anchor, lane, visit.distance, r.near, r.between, false, r.anchor[:0])
			items = r.neighborItems(items, 0, min(envelopeReachSlack, g.lengths[anchor]), from, math.Inf(1))
		}
	}
	return items
}

// neighborItems appends, over the positions from from to to of a lane, the
// locations of each item of r.anchor, the items of an anchor on another
// lane (see throughItems), that holds a position of that lane from lo to
// hi.
func (r *envelopeReach) neighborItems(items []reachItem, lo, hi, from, to float64) []reachItem {
	for _, item := range r.anchor {
		if item.from <= hi && item.to > lo {
			items = append(items, reachItem{location: item.location, from: from, to: to, origin: item.origin})
		}
	}
	return items
}

// nodeItems appends the berths and the lanes at a node, which the envelope
// holds up to the position to. The node is their origin.
func (g reachGraph) nodeItems(items []reachItem, node int, to float64) []reachItem {
	origin := node
	for _, berth := range g.berthsAt[node] {
		items = append(items, reachItem{location: berth, from: math.Inf(-1), to: to, origin: origin})
	}
	for _, lane := range g.incident[node] {
		items = append(items, reachItem{location: lane, from: math.Inf(-1), to: to, origin: origin})
	}
	return items
}

// spans divides [0, length] at the bounds of the items, and returns the
// spans with the locations of the items that each span meets. Two spans
// next to each other with the same keys become one.
//
// Each item starts at -Inf (the lane, a lane behind, or a plane of an
// origin) or ends at +Inf (a lane ahead). So the items that a span meets
// are the items that start at -Inf and end after the start of the span,
// and the other items that start before its end. With the items that start
// at -Inf in order of their ends, then the other items in order of their
// starts, these items are one window of the order. From one span to the
// next, both ends of the window move forward. Each span holds its window
// of one array of location numbers, so the spans of a lane use storage in
// proportion to the items, and not to the spans times the items.
//
// An item holds the closed interval from its start up to the float64 before its
// end. A bound at the length of the lane is then a cut too, and the last span
// is the one position of the length when an item starts there. Its window holds
// each item that starts at the length or before.
func (r *envelopeReach) spans(items []reachItem, length float64) []reachSpan {
	cuts := []float64{0}
	for _, item := range items {
		for _, bound := range []float64{item.from, item.to} {
			if bound > 0 && bound <= length {
				cuts = append(cuts, bound)
			}
		}
	}
	slices.Sort(cuts)
	cuts = slices.Compact(cuts)
	sorted := r.sorted[:0]
	for _, item := range items {
		if math.IsInf(item.from, -1) {
			sorted = append(sorted, item)
		}
	}
	open := len(sorted)
	for _, item := range items {
		if !math.IsInf(item.from, -1) {
			sorted = append(sorted, item)
		}
	}
	slices.SortFunc(sorted[:open], func(a, b reachItem) int { return cmp.Compare(a.to, b.to) })
	slices.SortFunc(sorted[open:], func(a, b reachItem) int { return cmp.Compare(a.from, b.from) })
	r.sorted = sorted
	numbers, keys := make([]int, len(sorted)), make([]int, len(sorted))
	for index, item := range sorted {
		numbers[index], keys[index] = item.location, r.key(item)
	}
	spans := make([]reachSpan, 0, len(cuts))
	lo, hi := 0, 0
	for index, from := range cuts {
		to, join := length, math.Nextafter(length, math.Inf(1))
		if index+1 < len(cuts) {
			to, join = cuts[index+1], cuts[index+1]
		}
		// An item joins the window when it starts before join. An item
		// that starts at -Inf leaves it when it ends at or before from.
		start, end := lo, hi
		for end < len(sorted) && sorted[end].from < join {
			end++
		}
		for start < open && sorted[start].to <= from {
			start++
		}
		changed := r.window.move(keys, lo, hi, start, end)
		lo, hi = start, end
		if last := len(spans) - 1; last >= 0 && !changed {
			spans[last].to = to
			continue
		}
		window := numbers[lo:hi:hi]
		spans = append(spans, reachSpan{from: from, to: to, locations: window, keys: keys[lo:hi:hi], planes: r.members.set(r.closure(window))})
	}
	r.window.clear(keys[lo:hi])
	return spans
}

// reachWindow counts the keys in a window of an array.
type reachWindow struct {
	// count holds the count of each key in the window.
	count []int
}

// move moves the window [lo, hi) of numbers to [start, end), where start is
// at least lo, end at least hi, and start at most end. numbers holds keys.
// It reports whether a key joins or leaves the window.
func (w *reachWindow) move(numbers []int, lo, hi, start, end int) bool {
	changed := false
	// Add first, so that a number that leaves at one place and joins at
	// another stays in the window.
	for _, number := range numbers[hi:end] {
		changed = changed || w.count[number] == 0
		w.count[number]++
	}
	for _, number := range numbers[lo:start] {
		w.count[number]--
		changed = changed || w.count[number] == 0
	}
	return changed
}

// clear empties the window.
func (w *reachWindow) clear(numbers []int) {
	for _, number := range numbers {
		w.count[number] = 0
	}
}

// reachMembers numbers the groups and the nodes of the planes of an
// envelope reach, as newPlaneReach does. It numbers them for each location
// at the start, so that a set is complete when it is made.
type reachMembers struct {
	// of holds the member numbers of each location number, and blank
	// whether the location has no group.
	of    [][]int
	blank []bool
	count int
	// seen and list are the storage of set.
	seen []uint64
	list []int
}

type reachMember struct {
	id    string
	group bool
}

func newReachMembers(locations []SafetyLocation) reachMembers {
	numbers := make(map[reachMember]int)
	number := func(member reachMember) int {
		value, found := numbers[member]
		if !found {
			value = len(numbers)
			numbers[member] = value
		}
		return value
	}
	m := reachMembers{of: make([][]int, len(locations)), blank: make([]bool, len(locations))}
	for index, location := range locations {
		m.blank[index] = location.SeparationGroup == ""
		of := []int{number(reachMember{location.SeparationGroup, true})}
		for _, node := range []string{location.From, location.To} {
			// A location with no node shares no node with another.
			if node != "" {
				of = append(of, number(reachMember{node, false}))
			}
		}
		m.of[index] = of
	}
	m.count = len(numbers)
	m.seen = make([]uint64, (m.count+63)/64)
	return m
}

// set returns the groups and the nodes of some locations.
func (m *reachMembers) set(numbers []int) reachSet {
	var set reachSet
	list := m.list[:0]
	for _, number := range numbers {
		set.blank = set.blank || m.blank[number]
		for _, member := range m.of[number] {
			if word, bit := member/64, uint64(1)<<(member%64); m.seen[word]&bit == 0 {
				m.seen[word] |= bit
				list = append(list, member)
			}
		}
	}
	if len(list) > reachBitsMembers {
		set.bits = slices.Clone(m.seen)
	} else {
		set.members = slices.Sorted(slices.Values(list))
	}
	for _, member := range list {
		m.seen[member/64] = 0
	}
	m.list = list
	return set
}

// reachWalk finds the shortest distance from a node to each node within a
// budget, along lanes in one direction. It keeps its storage between walks.
type reachWalk struct {
	distance []float64
	settled  []bool
	// touched holds the nodes with a finite distance. reached holds the
	// nodes within the budget, in the order in which the walk settles them.
	touched, reached []int
	queue            reachQueue
}

// runTo walks from start along the lanes of each node in lanes, to the node
// that next gives for each lane. It reaches each node whose distance is
// less than limit. The distance of a node is the least sum of lane lengths
// along a walk to it, added from the start.
func (w *reachWalk) runTo(start int, limit float64, lanes [][]int, next []int, lengths []float64) {
	if len(w.distance) < len(lanes) {
		w.distance, w.settled = make([]float64, len(lanes)), make([]bool, len(lanes))
		for index := range w.distance {
			w.distance[index] = math.Inf(1)
		}
	}
	for _, node := range w.touched {
		w.distance[node], w.settled[node] = math.Inf(1), false
	}
	w.touched, w.reached, w.queue = w.touched[:0], w.reached[:0], w.queue[:0]
	w.relax(start, 0)
	for len(w.queue) > 0 {
		visit := w.queue.pop()
		if w.settled[visit.node] {
			continue
		}
		w.settled[visit.node] = true
		w.reached = append(w.reached, visit.node)
		for _, lane := range lanes[visit.node] {
			if distance := visit.distance + lengths[lane]; distance < limit {
				w.relax(next[lane], distance)
			}
		}
	}
}

// relax gives a node a distance when it is shorter than the distance that
// the node has.
func (w *reachWalk) relax(node int, distance float64) {
	if distance >= w.distance[node] {
		return
	}
	if math.IsInf(w.distance[node], 1) {
		w.touched = append(w.touched, node)
	}
	w.distance[node] = distance
	w.queue.push(reachVisit{node: node, distance: distance})
}

// reachVisit is a node and a distance to it.
type reachVisit struct {
	node     int
	distance float64
}

// reachQueue is a binary heap of visits, the shortest distance first.
type reachQueue []reachVisit

// push adds a visit.
func (q *reachQueue) push(visit reachVisit) {
	*q = append(*q, visit)
	heap := *q
	for child := len(heap) - 1; child > 0; {
		parent := (child - 1) / 2
		if heap[parent].distance <= heap[child].distance {
			break
		}
		heap[parent], heap[child] = heap[child], heap[parent]
		child = parent
	}
}

// pop removes and returns the visit with the shortest distance.
func (q *reachQueue) pop() reachVisit {
	heap := *q
	first, last := heap[0], len(heap)-1
	heap[0] = heap[last]
	heap = heap[:last]
	for parent := 0; ; {
		least := parent
		for _, child := range []int{2*parent + 1, 2*parent + 2} {
			if child < len(heap) && heap[child].distance < heap[least].distance {
				least = child
			}
		}
		if least == parent {
			break
		}
		heap[parent], heap[least] = heap[least], heap[parent]
		parent = least
	}
	*q = heap
	return first
}
