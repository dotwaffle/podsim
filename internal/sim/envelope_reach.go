package sim

import (
	"cmp"
	"math"
	"slices"
)

// envelopeReachSlack widens each bound of the envelope reach. The oracle
// compares route distances that it sums from the start of the route. The
// index sums the same lane lengths in a different order. A restore can also
// move the route distance of a pod from its published lane position by up to
// two restoreTolerance steps (see restoredDistance), once to the saved
// distance and once to a block end. The slack is much larger than both.
const envelopeReachSlack = 1e-3

// envelopeReach holds the planes that the envelope of a pod can hold (see
// appendVehicleSafetyLocations), by position on each lane, for each route
// through the lane. It holds each plane of such an envelope, and it can
// hold more. This is the envelope reach of decision D1-B of the shape
// clearance design.
//
// A position on a lane has these planes:
//   - The lane.
//   - Each lane behind it on a route, up to the retained tail of that
//     lane: max(largeClearance, its geometry tail).
//   - Each lane ahead of it on a route, up to the front bound. The oracle
//     takes the largest front bound of the lanes of the whole route, and a
//     route can hold each lane that connects to the lane. So the index
//     takes the largest front bound of the network (see reachFront).
//   - Before the origin release, each berth from which a route can start
//     behind the position, and the lanes at the node of the berth. The
//     origin tail is max(largeClearance, the from tail of the first lane).
//
// A berth has its own plane and the planes of the lanes at its node.
//
// Each walk along the network takes the shortest distance, so it holds a
// plane for at least each position at which a route holds it. The spans of
// a lane depend only on the network, so the index builds them when the
// audit first asks for the lane (see laneSpans). The cost then grows with
// the lanes that the audit asks for, and not with each lane of the
// network. The index is not safe for concurrent use.
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
	// holds their groups and nodes.
	locations []SafetyLocation
	members   reachMembers
	// walk, items, sorted, and window are the storage of the lane builds.
	// built counts the lanes that laneSpans built.
	walk          reachWalk
	items, sorted []reachItem
	window        reachWindow
	built         int
}

// reachSpan is a part of a lane, from from up to to, and the planes that
// an envelope at a position in it can hold.
type reachSpan struct {
	from, to float64
	// locations holds the location numbers of the planes (see
	// envelopeReach.locations), in no order, and a number can occur more
	// than one time. The spans of a lane share one array.
	locations []int
	// planes holds the groups and the nodes of the planes, for the plane
	// test.
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
// the boundary.
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
	// from, to, lengths, and tails hold the end nodes, the length, and
	// the retained tail of each lane.
	from, to       []int
	lengths, tails []float64
	// in, out, and incident hold the lanes into, out of, and at each node,
	// as nodeSafety holds them. berthsAt holds the berths at each node.
	in, out, incident, berthsAt [][]int
	// originAt holds the longest origin tail of the lanes out of each
	// node. berthNode holds the node of each berth.
	originAt  []float64
	berthNode []int
	// front is the front bound of reachFront. behind is the longest
	// distance behind a lane at which a route can hold a lane or an origin.
	front, behind float64
}

// newReachGraph numbers the nodes of a network and indexes its lanes and
// berths by node.
func newReachGraph(n Network, tails map[string]geometryTails) reachGraph {
	nodes := make(map[string]int, len(n.Nodes))
	number := func(id string) int {
		value, found := nodes[id]
		if !found {
			value = len(nodes)
			nodes[id] = value
		}
		return value
	}
	positions := indexNodePositions(n.Nodes)
	g := reachGraph{front: reachFront(n, tails)}
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
	}
	for _, station := range n.Stations {
		for _, berth := range station.Berths {
			g.berthNode = append(g.berthNode, number(berth.Node))
		}
	}
	count := len(nodes)
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

// reachFront returns the largest front bound of the lanes of a network,
// and at least largeEnvelopeRadius. The oracle takes the largest front
// bound of the lanes of the route. A route through a lane can hold each
// lane that the lane reaches or that reaches the lane, which in a strongly
// connected network is each lane.
func reachFront(n Network, tails map[string]geometryTails) float64 {
	front := largeEnvelopeRadius
	for _, lane := range n.Lanes {
		front = max(front, tails[lane.ID].front)
	}
	return front
}

// newEnvelopeReach returns the envelope reach of a network. tails is the
// tail index of the network (see indexGeometryTails). It builds the spans of
// the berths, and the spans of a lane when laneSpans first asks for them. A
// bound that is not finite makes each walk cover each node that it reaches,
// which is slow and large on a big network. Preparation refuses such a
// network, but only after the bank audit, so a caller in the audit must
// check the bounds first.
func newEnvelopeReach(n Network, tails map[string]geometryTails) *envelopeReach {
	g := newReachGraph(n, tails)
	reach := &envelopeReach{graph: g, lanes: make([][]reachSpan, len(n.Lanes)), locations: make([]SafetyLocation, 0, len(n.Lanes)+len(g.berthNode))}
	for _, lane := range n.Lanes {
		reach.locations = append(reach.locations, SafetyLocation{SeparationGroup: lane.SeparationGroup, From: lane.From, To: lane.To})
	}
	for _, station := range n.Stations {
		for _, berth := range station.Berths {
			reach.locations = append(reach.locations, SafetyLocation{SeparationGroup: berth.SeparationGroup, From: berth.Node, To: berth.Node})
		}
	}
	reach.members = newReachMembers(reach.locations)
	reach.window.count = make([]int, len(reach.locations))
	for _, node := range g.berthNode {
		// The berths at the node include the berth.
		reach.items = g.nodeItems(reach.items[:0], node, math.Inf(1))
		reach.berths = append(reach.berths, reach.spans(reach.items, 0)...)
	}
	return reach
}

// laneSpans returns the spans of a lane. It builds them when it first gets
// the lane, and keeps them.
func (r *envelopeReach) laneSpans(lane int) []reachSpan {
	if spans := r.lanes[lane]; spans != nil {
		return spans
	}
	r.items = r.graph.laneItems(&r.walk, lane, r.items[:0])
	r.lanes[lane] = r.spans(r.items, r.graph.lengths[lane])
	r.built++
	return r.lanes[lane]
}

// reachItem is a location and the positions of a lane at which an
// envelope can hold it, from from to to. The bounds include the slack.
type reachItem struct {
	location int
	from, to float64
}

// laneItems appends the items of a lane to items.
func (g reachGraph) laneItems(walk *reachWalk, lane int, items []reachItem) []reachItem {
	length := g.lengths[lane]
	items = append(items, reachItem{location: lane, from: math.Inf(-1), to: math.Inf(1)})
	// Behind: a lane that ends a distance before the From node is in the
	// envelope while the pod is within its tail of that end.
	walk.run(g.from[lane], g.behind, g.in, g.from, g.lengths)
	for _, node := range walk.reached {
		distance := walk.distance[node]
		for _, previous := range g.in[node] {
			if to := g.tails[previous] - distance + envelopeReachSlack; to > 0 {
				items = append(items, reachItem{location: previous, from: math.Inf(-1), to: to})
			}
		}
		if len(g.berthsAt[node]) > 0 {
			if to := g.originAt[node] - distance + envelopeReachSlack; to > 0 {
				items = g.nodeItems(items, node, to)
			}
		}
	}
	// Ahead: a lane that starts a distance after the To node is in the
	// envelope from the front bound before its start.
	walk.run(g.to[lane], g.front, g.out, g.to, g.lengths)
	for _, node := range walk.reached {
		from := length + walk.distance[node] - g.front - envelopeReachSlack
		for _, next := range g.out[node] {
			items = append(items, reachItem{location: next, from: from, to: math.Inf(1)})
		}
	}
	return items
}

// nodeItems appends the berths and the lanes at a node, which the envelope
// holds up to the position to.
func (g reachGraph) nodeItems(items []reachItem, node int, to float64) []reachItem {
	for _, berth := range g.berthsAt[node] {
		items = append(items, reachItem{location: berth, from: math.Inf(-1), to: to})
	}
	for _, lane := range g.incident[node] {
		items = append(items, reachItem{location: lane, from: math.Inf(-1), to: to})
	}
	return items
}

// spans divides [0, length] at the bounds of the items, and returns the
// spans with the locations of the items that each span meets. Two spans
// next to each other with the same locations become one.
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
func (r *envelopeReach) spans(items []reachItem, length float64) []reachSpan {
	cuts := []float64{0}
	for _, item := range items {
		for _, bound := range []float64{item.from, item.to} {
			if bound > 0 && bound < length {
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
	numbers := make([]int, len(sorted))
	for index, item := range sorted {
		numbers[index] = item.location
	}
	spans := make([]reachSpan, 0, len(cuts))
	lo, hi := 0, 0
	for index, from := range cuts {
		to := length
		if index+1 < len(cuts) {
			to = cuts[index+1]
		}
		// An item joins the window when it starts before to. An item that
		// starts at -Inf leaves it when it ends at or before from.
		start, end := lo, hi
		for end < len(sorted) && sorted[end].from < to {
			end++
		}
		for start < open && sorted[start].to <= from {
			start++
		}
		changed := r.window.move(numbers, lo, hi, start, end)
		lo, hi = start, end
		if last := len(spans) - 1; last >= 0 && !changed {
			spans[last].to = to
			continue
		}
		window := numbers[lo:hi:hi]
		spans = append(spans, reachSpan{from: from, to: to, locations: window, planes: r.members.set(window)})
	}
	r.window.clear(numbers[lo:hi])
	return spans
}

// reachWindow counts the location numbers in a window of an array.
type reachWindow struct {
	// count holds the count of each location number in the window.
	count []int
}

// move moves the window [lo, hi) of numbers to [start, end), where start is
// at least lo, end at least hi, and start at most end. It reports whether
// a location number joins or leaves the window.
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

// run walks from start along the lanes of each node in lanes, to the node
// that next gives for each lane. It reaches each node whose distance is
// less than budget plus the slack.
func (w *reachWalk) run(start int, budget float64, lanes [][]int, next []int, lengths []float64) {
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
	limit := budget + envelopeReachSlack
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
