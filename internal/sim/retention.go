package sim

import (
	"math"
	"slices"
)

// Speed-dependent clearance, layer A. A pod at speed v needs a gap of
// min(compactGap, 1+2u) behind it, with u = v²/2b, so a slow pod needs less
// than Clearance. Only qualifying lanes use this rule (see
// newLaneRetention). Large classes keep their clearance.
const (
	// compactBody and compactGap are the body length and the full gap in
	// meters of a small pod. Clearance is their sum.
	compactBody = 4.0
	compactGap  = 8.0
)

// reducedClearanceSpeed is the speed in meters per second at which the
// speed-dependent gap reaches compactGap. A lane at this limit or more keeps
// Clearance.
var reducedClearanceSpeed = math.Sqrt(acceleration * (compactGap - 1))

// stopDistance returns the braking distance in meters at a speed.
func stopDistance(speed float64) float64 {
	return float64(speed * speed / (2 * acceleration))
}

// compactClearance returns the center distance in meters that a small
// follower at a speed needs behind the pod ahead of it.
func compactClearance(speed float64) float64 {
	return compactBody + min(compactGap, 1+float64(2*stopDistance(speed)))
}

// laneRetention holds the reduced retention of each qualifying lane and the
// window that certifies it. prepareNetwork builds it one time for a
// network. The lane lengths, the lane cells, the bank audit and the safety
// observation read it, and NewLaneClearance gives it to the editor. A nil
// index has no qualifying lane.
type laneRetention struct {
	// retention holds the reduced retention in meters of each qualifying
	// lane, by lane ID. A lane that does not qualify has none.
	retention map[string]float64
	windows   []retentionWindow
	// lanes and nodes hold the spans of each lane and of each inner node
	// of the windows, by ID. A node span has no length.
	lanes, nodes map[string][]windowSpan
}

// retentionWindow is the window of one qualifying lane. Offsets are path
// distances from the start of the lane. end is the offset of the end of the
// lane, and limit is end plus 2*Clearance.
type retentionWindow struct {
	end, limit float64
}

// windowSpan is the offset range of a lane or a node in a window.
type windowSpan struct {
	window     int
	start, end float64
}

// retentionLane holds the lane data that newLaneRetention reads.
type retentionLane struct {
	length, stop float64
	shape        laneShape
	// usable is true for a lane with known end nodes, a unique ID, a
	// finite positive length and speed limit, and no geometry tail. A
	// window holds only usable lanes. A lane that admits a large class has
	// a tail, and so has each lane at a node of a large berth.
	usable bool
}

type retentionBuilder struct {
	network            Network
	lanes              []retentionLane
	incoming, outgoing map[string][]int
}

// windowLane is a lane of a window and the offset of its start.
type windowLane struct {
	lane   int
	offset float64
}

// windowNode is an inner node of a window and its offset.
type windowNode struct {
	id     string
	offset float64
}

// newLaneRetention returns the retention index of a network, in network lane
// order. The network does not need to be valid: a lane that the index cannot
// certify does not qualify. tails are the geometry tails of the network.
//
// The upstream bound u_up of a lane L is the largest stop distance at the
// limit of L and of each upstream lane P that a reverse walk includes. The
// walk includes P when the path distance from the end of P to the start of
// L is at most the stop distance of P. It continues past P only when the
// distance from the start of P to the start of L is at most that stop
// distance, because speedBeforeLane brings a pod to the limit of P at the
// start of P. The window of L holds the lanes from u_up before the start of
// L to 2*Clearance after its end, and T is the total turn along them. L
// qualifies when all of these are true:
//  1. L is straight.
//  2. The limit of L is less than reducedClearanceSpeed.
//  3. No lane of the window admits a large class or has a geometry tail.
//  4. Each node in the window has one incoming and one outgoing lane, and
//     no lane occurs twice.
//  5. T is at most π/2.
//  6. R = (compactBody+1+2u*)/cos(T/2) - u*, with u* = min(u_up,
//     (compactGap-1)/2), is less than Clearance.
func newLaneRetention(network Network, tails map[string]geometryTails) *laneRetention {
	if !slices.ContainsFunc(network.Lanes, func(lane Lane) bool {
		return lane.Control == nil && lane.SpeedLimit > 0 && lane.SpeedLimit < reducedClearanceSpeed
	}) {
		return nil
	}
	b := retentionBuilder{network: network, lanes: make([]retentionLane, len(network.Lanes)),
		incoming: make(map[string][]int), outgoing: make(map[string][]int)}
	positions := indexNodePositions(network.Nodes)
	ids := make(map[string]int, len(network.Lanes))
	for _, lane := range network.Lanes {
		ids[lane.ID]++
	}
	for index, lane := range network.Lanes {
		b.incoming[lane.To] = append(b.incoming[lane.To], index)
		b.outgoing[lane.From] = append(b.outgoing[lane.From], index)
		from, fromOK := positions[lane.From]
		to, toOK := positions[lane.To]
		points := lanePointsBetween(lane, from, to, make([]Point, 0, 65))
		length := indexedLaneLength(lane, from, to)
		b.lanes[index] = retentionLane{
			length: length, stop: stopDistance(lane.SpeedLimit), shape: newLaneShape(points),
			usable: fromOK && toOK && ids[lane.ID] == 1 && finite(length) && length > 0 &&
				finite(lane.SpeedLimit) && lane.SpeedLimit > 0 &&
				tails[lane.ID] == (geometryTails{}),
		}
	}
	r := &laneRetention{retention: make(map[string]float64), lanes: make(map[string][]windowSpan), nodes: make(map[string][]windowSpan)}
	for index, lane := range network.Lanes {
		retention, lanes, nodes, ok := b.window(index)
		if !ok {
			continue
		}
		window := len(r.windows)
		r.retention[lane.ID] = retention
		r.windows = append(r.windows, retentionWindow{end: b.lanes[index].length, limit: b.lanes[index].length + 2*Clearance})
		for _, entry := range lanes {
			id := network.Lanes[entry.lane].ID
			r.lanes[id] = append(r.lanes[id], windowSpan{window: window, start: entry.offset, end: entry.offset + b.lanes[entry.lane].length})
		}
		for _, node := range nodes {
			r.nodes[node.id] = append(r.nodes[node.id], windowSpan{window: window, start: node.offset, end: node.offset})
		}
	}
	return r
}

// window returns the reduced retention of a lane, the lanes of its window
// in path order, and the inner nodes of the window. ok is false when the
// lane does not qualify.
func (b *retentionBuilder) window(index int) (retention float64, lanes []windowLane, nodes []windowNode, ok bool) {
	lane, data := b.network.Lanes[index], b.lanes[index]
	if lane.Control != nil || !(lane.SpeedLimit < reducedClearanceSpeed) || !data.usable {
		return 0, nil, nil, false
	}
	// The upstream walk. Each node at an offset of at least -up is in the
	// window, and up grows only with a lane that the walk includes.
	up, offset, node, walking := data.stop, 0.0, lane.From, true
	for offset >= -up {
		previous, ok := b.chainLane(node, b.incoming, lanes, index)
		if !ok {
			return 0, nil, nil, false
		}
		nodes = append(nodes, windowNode{id: node, offset: offset})
		before := b.lanes[previous]
		if walking && -offset <= before.stop {
			up = max(up, before.stop)
		}
		walking = walking && -offset+before.length <= before.stop
		offset -= before.length
		lanes = append(lanes, windowLane{lane: previous, offset: offset})
		node = b.network.Lanes[previous].From
	}
	slices.Reverse(lanes)
	lanes = append(lanes, windowLane{lane: index})
	limit := data.length + 2*Clearance
	for offset, node = data.length, lane.To; offset <= limit; {
		next, ok := b.chainLane(node, b.outgoing, lanes, index)
		if !ok {
			return 0, nil, nil, false
		}
		nodes = append(nodes, windowNode{id: node, offset: offset})
		lanes = append(lanes, windowLane{lane: next, offset: offset})
		offset += b.lanes[next].length
		node = b.network.Lanes[next].To
	}
	var turn turnSum
	for _, entry := range lanes {
		turn = turn.add(b.lanes[entry.lane].shape)
	}
	if turn.turn > math.Pi/2 {
		return 0, nil, nil, false
	}
	retention = reducedRetention(up, turn.turn)
	if !(retention < Clearance) {
		return 0, nil, nil, false
	}
	return retention, lanes, nodes, true
}

// chainLane returns the one lane of next at a window node. The node needs
// one incoming and one outgoing lane, and the lane must be usable and not
// already in the window.
func (b *retentionBuilder) chainLane(node string, next map[string][]int, lanes []windowLane, index int) (int, bool) {
	if len(b.incoming[node]) != 1 || len(b.outgoing[node]) != 1 {
		return 0, false
	}
	lane := next[node][0]
	if lane == index || !b.lanes[lane].usable || slices.ContainsFunc(lanes, func(entry windowLane) bool { return entry.lane == lane }) {
		return 0, false
	}
	return lane, true
}

// reducedRetention returns R for an upstream bound and a total turn.
func reducedRetention(up, turn float64) float64 {
	bound := min(up, (compactGap-1)/2)
	return (compactBody+1+float64(2*bound))/math.Cos(turn/2) - bound
}

// of returns the reduced retention of a lane, or 0 when the lane does not
// qualify.
func (r *laneRetention) of(id string) float64 {
	if r == nil {
		return 0
	}
	return r.retention[id]
}

// laneMinimum returns the minimum length of a lane: twice its reduced
// retention when it qualifies, and LaneMinimumLength when it does not.
func (r *laneRetention) laneMinimum(lane Lane) float64 {
	if retention := r.of(lane.ID); retention > 0 {
		return 2 * retention
	}
	return LaneMinimumLength(lane)
}

func (r *laneRetention) laneSpans(id string) []windowSpan {
	if r == nil {
		return nil
	}
	return r.lanes[id]
}

func (r *laneRetention) nodeSpans(id string) []windowSpan {
	if r == nil {
		return nil
	}
	return r.nodes[id]
}

// auditMember holds the data of a path that pairPieces reads. A berth has
// length 0 and speed 0.
type auditMember struct {
	spans         []windowSpan
	length, speed float64
	large         bool
}

// pairPiece is a part of a pair of paths and the clearance that it needs.
// first and second are path distance ranges from the start of each path.
type pairPiece struct {
	first, second [2]float64
	clearance     float64
}

// pairPieces divides two nonincident paths into pieces with one clearance
// each, as the safety observation judges two pods at these positions. A
// pair with a large path keeps largeClearance. In a window that holds both
// paths, the path with the smaller start holds the follower, which must be
// at or before the end of the qualifying lane, and the other pod must be at
// or before the limit of the window. A piece in which a window certifies
// each pair of positions uses compactClearance at the higher speed limit of
// the two paths, and every other piece keeps Clearance.
func (r *laneRetention) pairPieces(first, second auditMember) []pairPiece {
	whole := pairPiece{first: [2]float64{0, first.length}, second: [2]float64{0, second.length}, clearance: Clearance}
	if first.large || second.large {
		whole.clearance = largeClearance
		return []pairPiece{whole}
	}
	// Each window certifies the positions up to a bound on each path.
	var bounds [][2]float64
	for _, a := range first.spans {
		for _, b := range second.spans {
			if a.window != b.window {
				continue
			}
			window := r.windows[a.window]
			bound := [2]float64{window.limit - a.start, window.limit - b.start}
			if a.start <= b.start {
				bound[0] = window.end - a.start
			}
			if b.start <= a.start {
				bound[1] = window.end - b.start
			}
			if bound[0] >= 0 && bound[1] >= 0 {
				bounds = append(bounds, bound)
			}
		}
	}
	if len(bounds) == 0 {
		return []pairPiece{whole}
	}
	cuts := func(side int, length float64) []float64 {
		values := []float64{0, length}
		for _, bound := range bounds {
			if bound[side] > 0 && bound[side] < length {
				values = append(values, bound[side])
			}
		}
		slices.Sort(values)
		return slices.Compact(values)
	}
	firstCuts, secondCuts := cuts(0, first.length), cuts(1, second.length)
	reduced := compactClearance(max(first.speed, second.speed))
	var pieces []pairPiece
	for i := range max(1, len(firstCuts)-1) {
		for j := range max(1, len(secondCuts)-1) {
			piece := pairPiece{first: [2]float64{firstCuts[i], firstCuts[min(i+1, len(firstCuts)-1)]},
				second: [2]float64{secondCuts[j], secondCuts[min(j+1, len(secondCuts)-1)]}, clearance: Clearance}
			if slices.ContainsFunc(bounds, func(bound [2]float64) bool {
				return piece.first[1] <= bound[0] && piece.second[1] <= bound[1]
			}) {
				piece.clearance = reduced
			}
			pieces = append(pieces, piece)
		}
	}
	return pieces
}

// polylinePiece returns the part of a polyline between two path distances.
func polylinePiece(points []Point, from, to float64) []Point {
	piece := make([]Point, 0, len(points)+1)
	distance := 0.0
	for index := 1; index < len(points); index++ {
		a, b := points[index-1], points[index]
		length := pointDistance(a, b)
		at := func(target float64) Point {
			if length == 0 {
				return a
			}
			t := min(1, max(0, (target-distance)/length))
			return Point{X: a.X + t*(b.X-a.X), Y: a.Y + t*(b.Y-a.Y)}
		}
		if len(piece) == 0 && from <= distance+length {
			piece = append(piece, at(from))
		}
		if len(piece) > 0 {
			if to <= distance+length {
				return append(piece, at(to))
			}
			piece = append(piece, b)
		}
		distance += length
	}
	if len(piece) == 0 {
		piece = append(piece, points[len(points)-1])
	}
	return append(piece, points[len(points)-1])
}

// chainPosition is the offset of a pod in one window.
type chainPosition struct {
	window     int
	offset     float64
	end, limit float64
}

// safetyChain binds the window positions to an unchanged observation pod.
type safetyChain struct {
	pod       Pod
	positions []chainPosition
	speed     float64
}

// chainPositions returns the window positions of a small pod on a lane at a
// distance, or at a berth on a node.
func (r *laneRetention) chainPositions(pod Pod, berthNode string) []chainPosition {
	var spans []windowSpan
	distance := 0.0
	if pod.LaneID != "" {
		spans, distance = r.laneSpans(pod.LaneID), pod.LaneDistance
	} else if berthNode != "" {
		spans = r.nodeSpans(berthNode)
	}
	if len(spans) == 0 {
		return nil
	}
	positions := make([]chainPosition, len(spans))
	for index, span := range spans {
		window := r.windows[span.window]
		positions[index] = chainPosition{window: span.window, offset: span.start + distance, end: window.end, limit: window.limit}
	}
	return positions
}

// certifiedClearance returns the clearance of two small pods with these
// window positions and speeds, and whether a window certifies the pair. In
// a window, the pod with the smaller offset is the follower. It must be at
// or before the end of the qualifying lane, and the other pod at or before
// the limit of the window. The clearance is compactClearance at the speed
// of the follower, the larger result when more than one window certifies
// the pair.
func certifiedClearance(first, second safetyChain) (float64, bool) {
	clearance, ok := 0.0, false
	for _, a := range first.positions {
		for _, b := range second.positions {
			if a.window != b.window {
				continue
			}
			follower, leader := min(a.offset, b.offset), max(a.offset, b.offset)
			if follower > a.end || leader > a.limit {
				continue
			}
			speed := first.speed
			switch {
			case b.offset < a.offset:
				speed = second.speed
			case a.offset == b.offset:
				speed = max(first.speed, second.speed)
			}
			clearance, ok = max(clearance, compactClearance(speed)), true
		}
	}
	return clearance, ok
}

// LaneClearance holds the reduced retention and the certified windows of a
// network, so that the editor applies the rules of PrepareNetwork. The
// zero value has no qualifying lane.
type LaneClearance struct {
	retention *laneRetention
	// depots holds only the chain lanes of the depots.
	depots *depotIndex
}

// NewLaneClearance returns the index of a network. The network does not
// need to be valid. A lane that the index cannot certify keeps the class
// bounds. The chain lanes of the index are the chain lanes as the depots
// list them (see depotChainLanes), so that for a depot that is not valid
// the editor reports the depot error of the server and not a short chain
// lane.
func NewLaneClearance(network Network) LaneClearance {
	return LaneClearance{retention: newLaneRetention(network, indexGeometryTails(network)), depots: depotChainLanes(network)}
}

// depotChainLanes returns an index with only the chain lanes of the
// depots as they are listed: each lane from the node of a listed slot to
// the node of the slot before it in the same depot lane. For a valid
// network these are the chain lanes of newDepotIndex.
func depotChainLanes(network Network) *depotIndex {
	if len(network.Depots) == 0 {
		return nil
	}
	lanes := make(map[[2]string][]string)
	for _, lane := range network.Lanes {
		lanes[[2]string{lane.From, lane.To}] = append(lanes[[2]string{lane.From, lane.To}], lane.ID)
	}
	d := &depotIndex{chains: make(map[string]Berth)}
	for _, depot := range network.Depots {
		station, _ := network.Station(depot.Station)
		for _, lane := range depot.Lanes {
			for i := 1; i < len(lane.BerthIDs); i++ {
				from, fromOK := station.berth(lane.BerthIDs[i])
				to, toOK := station.berth(lane.BerthIDs[i-1])
				if !fromOK || !toOK {
					continue
				}
				for _, id := range lanes[[2]string{from.Node, to.Node}] {
					d.chains[id] = to
				}
			}
		}
	}
	return d
}

// LaneMinimum returns the minimum length of a lane, found by its ID: its
// reduced retention for a qualifying chain lane of a depot, twice its
// reduced retention for any other qualifying lane, and LaneMinimumLength
// when it does not qualify.
func (c LaneClearance) LaneMinimum(lane Lane) float64 {
	return c.depots.chainMinimum(lane, c.retention)
}

// PairPiece is a part of a pair of nonincident lanes and the clearance in
// meters that it needs. First and Second are path distance ranges from the
// start of each lane.
type PairPiece struct {
	First, Second [2]float64
	Clearance     float64
}

// PairPieces divides two nonincident lanes with the given lengths into
// pieces, as the bank audit of PrepareNetwork does.
func (c LaneClearance) PairPieces(first, second Lane, firstLength, secondLength float64) []PairPiece {
	member := func(lane Lane, length float64) auditMember {
		return auditMember{spans: c.retention.laneSpans(lane.ID), length: length, speed: lane.SpeedLimit, large: largeClassSet(lane.VehicleClasses)}
	}
	pieces := c.retention.pairPieces(member(first, firstLength), member(second, secondLength))
	result := make([]PairPiece, len(pieces))
	for index, piece := range pieces {
		result[index] = PairPiece{First: piece.first, Second: piece.second, Clearance: piece.clearance}
	}
	return result
}
