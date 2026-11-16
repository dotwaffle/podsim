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
// plane for at least each position at which a route holds it. The index
// does not change after newEnvelopeReach.
type envelopeReach struct {
	// lanes holds the spans of each lane in lane order. The spans of a
	// lane follow each other from 0 to the length of the lane.
	lanes [][]reachSpan
	// berths holds the span of each berth in station and berth order.
	berths []reachSpan
	// locations holds the plane of each location number: the lanes in
	// lane order, then the berths in station and berth order.
	locations []SafetyLocation
}

// reachSpan is a part of a lane, from from up to to, and the planes that
// an envelope at a position in it can hold.
type reachSpan struct {
	from, to float64
	// locations holds the sorted location numbers of the planes (see
	// envelopeReach.locations).
	locations []int
	// part holds the groups and the nodes of the planes, for the plane
	// test.
	part reachPart
}

// separated reports whether each plane of two spans is on a different
// plane from each plane of the other (see safetyLocationsSeparated).
func (s *reachSpan) separated(other *reachSpan) bool {
	return !s.part.blank && !other.part.blank && !s.part.overlaps(&other.part)
}

// laneAt returns the span of a lane at a position. A position at the
// boundary of two spans is in the second span. Each bound includes the
// slack, so the second span holds each plane that the envelope can hold at
// the boundary.
func (r *envelopeReach) laneAt(lane int, position float64) *reachSpan {
	spans := r.lanes[lane]
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

// newEnvelopeReach builds the envelope reach of a network. tails is the
// tail index of the network (see indexGeometryTails). A bound that is not
// finite makes each walk cover each node that it reaches, which is slow
// and large on a big network. Preparation refuses such a network, but only
// after the bank audit, so a caller in the audit must check the bounds
// first.
func newEnvelopeReach(n Network, tails map[string]geometryTails) *envelopeReach {
	g := newReachGraph(n, tails)
	reach := &envelopeReach{lanes: make([][]reachSpan, len(n.Lanes)), locations: make([]SafetyLocation, 0, len(n.Lanes)+len(g.berthNode))}
	for _, lane := range n.Lanes {
		reach.locations = append(reach.locations, SafetyLocation{SeparationGroup: lane.SeparationGroup, From: lane.From, To: lane.To})
	}
	for _, station := range n.Stations {
		for _, berth := range station.Berths {
			reach.locations = append(reach.locations, SafetyLocation{SeparationGroup: berth.SeparationGroup, From: berth.Node, To: berth.Node})
		}
	}
	members := newReachMembers()
	var walk reachWalk
	var items []reachItem
	for index := range n.Lanes {
		items = g.laneItems(&walk, index, items[:0])
		reach.lanes[index] = reachSpans(items, g.lengths[index], reach.locations, members)
	}
	for _, node := range g.berthNode {
		// The berths at the node include the berth.
		items = g.nodeItems(items[:0], node, math.Inf(1))
		reach.berths = append(reach.berths, reachSpans(items, 0, reach.locations, members)...)
	}
	count := members.count()
	for index := range reach.lanes {
		for span := range reach.lanes[index] {
			reach.lanes[index][span].part.seal(count)
		}
	}
	for index := range reach.berths {
		reach.berths[index].part.seal(count)
	}
	return reach
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

// reachSpans divides [0, length] at the bounds of the items, and returns
// the spans with the locations of the items that each span meets. Two
// spans next to each other with the same locations become one.
func reachSpans(items []reachItem, length float64, locations []SafetyLocation, members *reachMembers) []reachSpan {
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
	spans := make([]reachSpan, 0, len(cuts))
	for index, from := range cuts {
		to := length
		if index+1 < len(cuts) {
			to = cuts[index+1]
		}
		var numbers []int
		for _, item := range items {
			if item.from < to && item.to > from {
				numbers = append(numbers, item.location)
			}
		}
		slices.Sort(numbers)
		numbers = slices.Clip(slices.Compact(numbers))
		if last := len(spans) - 1; last >= 0 && slices.Equal(spans[last].locations, numbers) {
			spans[last].to = to
			continue
		}
		spans = append(spans, reachSpan{from: from, to: to, locations: numbers, part: members.part(numbers, locations)})
	}
	return spans
}

// reachMembers numbers the groups and the nodes of the planes of an
// envelope reach, as newPlaneReach does.
type reachMembers struct {
	numbers map[reachMember]int
}

type reachMember struct {
	id    string
	group bool
}

func newReachMembers() *reachMembers {
	return &reachMembers{numbers: make(map[reachMember]int)}
}

func (m *reachMembers) count() int {
	return len(m.numbers)
}

func (m *reachMembers) number(member reachMember) int {
	value, found := m.numbers[member]
	if !found {
		value = len(m.numbers)
		m.numbers[member] = value
	}
	return value
}

// part returns the groups and the nodes of some locations. The caller
// seals it when the numbers are complete.
func (m *reachMembers) part(numbers []int, locations []SafetyLocation) reachPart {
	var part reachPart
	for _, number := range numbers {
		location := locations[number]
		part.blank = part.blank || location.SeparationGroup == ""
		part.members = append(part.members, m.number(reachMember{location.SeparationGroup, true}))
		for _, node := range []string{location.From, location.To} {
			// A location with no node shares no node with another.
			if node != "" {
				part.members = append(part.members, m.number(reachMember{node, false}))
			}
		}
	}
	return part
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
