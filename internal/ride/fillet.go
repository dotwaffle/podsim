package ride

import (
	"math"
	"slices"

	"github.com/dotwaffle/podsim/internal/sim"
)

// The ride page rounds the joins of lanes at a node. The change is for
// display only: the simulation, the protocol, and the 2D view keep the
// sharp joins.
const (
	// filletTrim is the largest length in meters that a join takes from
	// the end of a lane.
	filletTrim = 8
	// filletShare is the largest fraction of a lane that a join takes from
	// one end, so the two ends of a lane do not overlap.
	filletShare = 0.4
	// filletMinTurn and filletMaxTurn are the turns in radians that make a
	// node round. A node with a turn above filletMaxTurn, as at a reversal
	// or a bay end, keeps its sharp joins.
	filletMinTurn = 3 * math.Pi / 180
	filletMaxTurn = 150 * math.Pi / 180
	// pieceSamples is the number of points of the arc length table of a
	// piece.
	pieceSamples = 33
	// parallelSine is the sine of the angle below which two tangent lines
	// are parallel.
	parallelSine = 1e-9
	// onLine is the distance in meters within which a node is on a tangent
	// line.
	onLine = 1e-6
	// pieceTangent is the largest angle in radians between a piece and the
	// direction of its lane at either end.
	pieceTangent = 1e-6
)

// pieceKey identifies the piece that joins the end of lane in to the start
// of lane out.
type pieceKey struct{ in, out string }

// piece is a quadratic Bezier curve that joins two lanes at a node. It
// starts on lane in at the trim of its end, and it stops on lane out at the
// trim of its start, with the tangents of the two lanes there.
type piece struct {
	key                 pieceKey
	start, control, end sim.Point
	// lengths holds the arc length at each sample of the curve.
	lengths []float64
}

// addFillets sets the trims of the lanes of t at each round node of network
// and returns the pieces, in node order and then in lane order. See
// roundNode and nodePieces for the rules.
func (t laneTable) addFillets(network sim.Network) []piece {
	berths := make(map[string]bool)
	for _, station := range network.Stations {
		for _, berth := range station.Berths {
			berths[berth.Node] = true
		}
	}
	ins, outs := make(map[string][]string), make(map[string][]string)
	for _, lane := range network.Lanes {
		ins[lane.To] = append(ins[lane.To], lane.ID)
		outs[lane.From] = append(outs[lane.From], lane.ID)
	}
	var pieces []piece
	for _, node := range network.Nodes {
		in, out := ins[node.ID], outs[node.ID]
		if berths[node.ID] || !t.roundNode(in, out) {
			continue
		}
		joins, ok := t.nodePieces(in, out, node.Position)
		if !ok {
			continue
		}
		for _, id := range in {
			shape := t[id]
			shape.trimEnd = trimOf(t.length(id))
			t[id] = shape
		}
		for _, id := range out {
			shape := t[id]
			shape.trimStart = trimOf(t.length(id))
			t[id] = shape
		}
		pieces = append(pieces, joins...)
	}
	return pieces
}

// nodePieces returns the pieces of each pair of an arriving lane of in and a
// leaving lane of out at node. It returns false when one of them does not
// continue the direction of its two lanes, and the node then keeps its sharp
// joins.
func (t laneTable) nodePieces(in, out []string, node sim.Point) ([]piece, bool) {
	joins := make([]piece, 0, len(in)*len(out))
	for _, a := range in {
		for _, b := range out {
			p, ok := t.newPiece(a, b, trimOf(t.length(a)), trimOf(t.length(b)), node)
			if !ok {
				return nil, false
			}
			joins = append(joins, p)
		}
	}
	return joins, true
}

// roundNode reports whether the node with the arriving lanes in and the
// leaving lanes out gets round joins: at least one pair of an arriving and a
// leaving lane turns by filletMinTurn or more, no pair turns by more than
// filletMaxTurn, and no lane is a reverse lane. A lane with no direction at
// the node keeps the joins sharp. The caller excludes the berth nodes.
func (t laneTable) roundNode(in, out []string) bool {
	if len(in) == 0 || len(out) == 0 {
		return false
	}
	for _, id := range slices.Concat(in, out) {
		if t[id].reverse {
			return false
		}
	}
	turns := false
	for _, a := range in {
		end, ok := t.tangent(a, math.Inf(1))
		if !ok {
			return false
		}
		for _, b := range out {
			start, ok := t.tangent(b, 0)
			if !ok {
				return false
			}
			turn := math.Abs(wrap(start - end))
			if turn > filletMaxTurn {
				return false
			}
			turns = turns || turn >= filletMinTurn
		}
	}
	return turns
}

// trimOf returns the trim of one end of a lane of length meters, in whole
// millimeters. The topology gives the trims, and the page cuts the lanes and
// finds the ends of the pieces at the same trims.
func trimOf(length float64) float64 { return millimeters(min(filletTrim, filletShare*length)) }

// newPiece returns the piece from lane in to lane out, which meet at node,
// with the trims before at the end of in and after at the start of out.
// The control point is where the tangent lines at the two ends meet, ahead
// of the start, behind the end, and at most twice the trims from either end.
// When the lines are the same line through node, node is the control point.
// When the lines are parallel, the control point is the middle of the two
// ends. It returns false when the piece does not continue the direction of
// both lanes.
func (t laneTable) newPiece(in, out string, before, after float64, node sim.Point) (piece, bool) {
	a, b := t[in], t[out]
	start, startDirection := a.at(t.length(in) - before)
	end, endDirection := b.at(after)
	p := piece{key: pieceKey{in, out}, start: start, end: end}
	p.control = sim.Point{X: (start.X + end.X) / 2, Y: (start.Y + end.Y) / 2}
	reach := 2 * (before + after)
	cross := startDirection.X*endDirection.Y - startDirection.Y*endDirection.X
	dx, dy := end.X-start.X, end.Y-start.Y
	if math.Abs(cross) < parallelSine {
		if lineGap(start, startDirection, node) < onLine && lineGap(end, endDirection, node) < onLine {
			p.control = node
		}
	} else {
		// start + ahead*startDirection = end + behind*endDirection.
		ahead := (dx*endDirection.Y - dy*endDirection.X) / cross
		behind := (dx*startDirection.Y - dy*startDirection.X) / cross
		if ahead > 0 && behind < 0 && ahead <= reach && -behind <= reach {
			p.control = sim.Point{X: start.X + ahead*startDirection.X, Y: start.Y + ahead*startDirection.Y}
		}
	}
	if !tangentTo(start, p.control, startDirection) || !tangentTo(p.control, end, endDirection) {
		return piece{}, false
	}
	p.lengths = make([]float64, pieceSamples)
	last := p.start
	for i := 1; i < pieceSamples; i++ {
		point := p.point(float64(i) / (pieceSamples - 1))
		p.lengths[i] = p.lengths[i-1] + math.Hypot(point.X-last.X, point.Y-last.Y)
		last = point
	}
	return p, true
}

// tangentTo reports whether the line from from to to has the direction,
// within pieceTangent.
func tangentTo(from, to, direction sim.Point) bool {
	dx, dy := to.X-from.X, to.Y-from.Y
	if dx == 0 && dy == 0 || direction.X == 0 && direction.Y == 0 {
		return false
	}
	return math.Abs(wrap(math.Atan2(dy, dx)-math.Atan2(direction.Y, direction.X))) <= pieceTangent
}

// lineGap returns the distance of point from the line through origin with
// the unit direction.
func lineGap(origin, direction, point sim.Point) float64 {
	return math.Abs((point.X-origin.X)*direction.Y - (point.Y-origin.Y)*direction.X)
}

func (p piece) point(t float64) sim.Point {
	u := 1 - t
	return sim.Point{X: u*u*p.start.X + 2*u*t*p.control.X + t*t*p.end.X, Y: u*u*p.start.Y + 2*u*t*p.control.Y + t*t*p.end.Y}
}

// at returns the point of p at the fraction of its arc length, and the
// direction of p there.
func (p piece) at(fraction float64) (sim.Point, float64) {
	t := arcParam(p.lengths, min(max(fraction, 0), 1)*p.lengths[len(p.lengths)-1])
	u := 1 - t
	dx := 2*u*(p.control.X-p.start.X) + 2*t*(p.end.X-p.control.X)
	dy := 2*u*(p.control.Y-p.start.Y) + 2*t*(p.end.Y-p.control.Y)
	return p.point(t), math.Atan2(dy, dx)
}

// arcParam returns the curve parameter at distance along a curve with the
// arc length table lengths, which has evenly spaced parameters.
func arcParam(lengths []float64, distance float64) float64 {
	i, _ := slices.BinarySearch(lengths, distance)
	switch {
	case i == 0:
		return 0
	case i >= len(lengths):
		return 1
	}
	fraction := 0.0
	if span := lengths[i] - lengths[i-1]; span > 0 {
		fraction = (distance - lengths[i-1]) / span
	}
	return (float64(i-1) + fraction) / float64(len(lengths)-1)
}

// at returns the point of s at distance along it, on the sample line that
// the simulation moves a pod on, and the unit direction of the lane there.
// The direction of a curve is the exact derivative.
func (s laneShape) at(distance float64) (sim.Point, sim.Point) {
	if s.control == nil {
		dx, dy := s.to.X-s.from.X, s.to.Y-s.from.Y
		length := math.Hypot(dx, dy)
		if length == 0 {
			return s.from, sim.Point{}
		}
		f := min(max(distance/length, 0), 1)
		return sim.Point{X: s.from.X + dx*f, Y: s.from.Y + dy*f}, sim.Point{X: dx / length, Y: dy / length}
	}
	param := arcParam(s.lengths, distance)
	scaled := param * (curveSamples - 1)
	i := min(int(scaled), curveSamples-2)
	f := scaled - float64(i)
	a, b := s.points[i], s.points[i+1]
	direction := s.direction(param)
	if length := math.Hypot(direction.X, direction.Y); length > 0 {
		direction = sim.Point{X: direction.X / length, Y: direction.Y / length}
	}
	return sim.Point{X: a.X + (b.X-a.X)*f, Y: a.Y + (b.Y-a.Y)*f}, direction
}

// direction returns the derivative of the curve s at param. Where the
// derivative is zero, at an end with the control point on it, it returns
// the limit of its direction there: the line from the start to the end.
func (s laneShape) direction(param float64) sim.Point {
	c := *s.control
	u := 1 - param
	d := sim.Point{X: 2*u*(c.X-s.from.X) + 2*param*(s.to.X-c.X), Y: 2*u*(c.Y-s.from.Y) + 2*param*(s.to.Y-c.Y)}
	if d.X == 0 && d.Y == 0 {
		return sim.Point{X: s.to.X - s.from.X, Y: s.to.Y - s.from.Y}
	}
	return d
}

// project returns the distance along s of the point of its sample line
// nearest to point, at least from, and the gap between the two points.
func (s laneShape) project(point sim.Point, from float64) (float64, float64) {
	points, lengths := s.points, s.lengths
	if s.control == nil {
		points = []sim.Point{s.from, s.to}
		lengths = []float64{0, math.Hypot(s.to.X-s.from.X, s.to.Y-s.from.Y)}
	}
	best, gap := from, math.Inf(1)
	for i := 1; i < len(points); i++ {
		if lengths[i] < from {
			continue
		}
		a, b := points[i-1], points[i]
		span := lengths[i] - lengths[i-1]
		f := 0.0
		if span > 0 {
			f = ((point.X-a.X)*(b.X-a.X) + (point.Y-a.Y)*(b.Y-a.Y)) / (span * span)
			f = min(max(f, (from-lengths[i-1])/span, 0), 1)
		}
		x, y := a.X+(b.X-a.X)*f, a.Y+(b.Y-a.Y)*f
		if d := math.Hypot(point.X-x, point.Y-y); d < gap {
			best, gap = lengths[i-1]+span*f, d
		}
	}
	return best, gap
}

// locateGap is the largest gap in meters between the sampled position of a
// pod and its route. The motion buffer moves a pod on the sample lines that
// project uses, so the gap is a rounding error.
const locateGap = 1e-3

// sameSpot is the largest route distance in meters between two matches of
// locate that are the same place, as the end of a lane and the start of the
// next lane.
const sameSpot = 0.01

// locateReach is the farthest route distance in meters past the lane of the
// pod that locate searches. The motion buffer moves a pod at most a few
// lane speeds times half a second between two frames.
const locateReach = 100

// place is the lane and the distance along it of a pod, with the lanes
// before and after it on its route. An empty lane name is not known.
type place struct {
	lane, previous, next string
	distance             float64
}

// locate returns the place of the sampled position of vehicle on its route.
// remote.Motion moves the position along the route, but it keeps the lane
// and the lane distance of the earlier frame. The search starts at that
// lane and distance and goes forward. It returns false when the position
// is not on the route, or when it is at two places of the route, as on a
// route that comes back to a lane.
func (t laneTable) locate(vehicle sim.Vehicle) (place, bool) {
	pod := vehicle.Pod
	route, index := routeAt(vehicle)
	if index < 0 {
		return place{}, false
	}
	if route == nil {
		route = []sim.Lane{{ID: pod.LaneID}}
	}
	var found place
	matched, foundAt := false, 0.0
	// laneStart is the route distance of the start of lane i from the
	// start of the pod lane.
	laneStart := 0.0
	for i := index; i < len(route) && laneStart-pod.LaneDistance <= locateReach; i++ {
		id := route[i].ID
		if _, ok := t[id]; !ok {
			break
		}
		from := 0.0
		if i == index {
			from = pod.LaneDistance
		}
		distance, gap := t[id].project(pod.Position, from)
		if gap <= locateGap {
			if matched {
				if laneStart+distance-foundAt > sameSpot {
					return place{}, false
				}
			} else {
				matched, foundAt = true, laneStart+distance
				found = place{lane: id, distance: distance}
				if i > 0 {
					found.previous = route[i-1].ID
				}
				if i+1 < len(route) {
					found.next = route[i+1].ID
				}
			}
		}
		laneStart += t.length(id)
	}
	return found, matched
}

// routeAt returns the lanes of the route of vehicle in travel order and the
// index of the lane of the pod. It returns no lanes and the index 0 when
// the route does not hold the pod lane. It returns the index -1 for a pod
// with no lane, and for a Route that holds the pod lane more than once, as
// the bridge cannot tell which one the pod is on. A stream view gives the
// route window in Presentation, with the index of the pod lane, and Route
// then holds the lanes in network order.
func routeAt(vehicle sim.Vehicle) ([]sim.Lane, int) {
	pod := vehicle.Pod
	if pod.LaneID == "" {
		return nil, -1
	}
	if window := vehicle.Presentation; window != nil {
		if window.Current < window.Start || window.Current-window.Start >= uint64(len(window.Motion)) {
			return nil, 0
		}
		index := int(window.Current - window.Start) // #nosec G115 -- less than len(window.Motion).
		if window.Motion[index].ID != pod.LaneID {
			return nil, 0
		}
		return window.Motion, index
	}
	onLane := func(lane sim.Lane) bool { return lane.ID == pod.LaneID }
	index := slices.IndexFunc(vehicle.Route, onLane)
	if index < 0 {
		return nil, 0
	}
	if slices.ContainsFunc(vehicle.Route[index+1:], onLane) {
		return nil, -1
	}
	return vehicle.Route, index
}

// trimJoin returns the node position of the trim that at is in, or false
// when at is not in a trim.
func (t laneTable) trimJoin(at place) (sim.Point, bool) {
	shape := t[at.lane]
	length := t.length(at.lane)
	switch {
	case shape.trimEnd > 0 && at.distance > length-shape.trimEnd:
		return shape.to, true
	case shape.trimStart > 0 && at.distance < shape.trimStart:
		return shape.from, true
	}
	return sim.Point{}, false
}

// filletPosition returns the position of a pod at at on its piece, and the
// direction of the piece there. A pod in the trim at the end of its lane
// uses the piece to the next lane, and a pod in the trim at the start of its
// lane uses the piece from the previous lane. The progress along the two
// lane parts maps to the same fraction of the arc length of the piece. It
// returns false when the pod is not in a trim, or the piece is not known.
func (b *Bridge) filletPosition(at place) (sim.Point, float64, bool) {
	shape := b.lanes[at.lane]
	length := b.lanes.length(at.lane)
	if shape.trimEnd > 0 && at.distance > length-shape.trimEnd && at.next != "" {
		if p, ok := b.pieces[pieceKey{at.lane, at.next}]; ok {
			done := at.distance - (length - shape.trimEnd)
			point, angle := p.at(done / (shape.trimEnd + b.lanes[at.next].trimStart))
			return point, angle, true
		}
	}
	if shape.trimStart > 0 && at.distance < shape.trimStart && at.previous != "" {
		if p, ok := b.pieces[pieceKey{at.previous, at.lane}]; ok {
			before := b.lanes[at.previous].trimEnd
			point, angle := p.at((before + max(at.distance, 0)) / (before + shape.trimStart))
			return point, angle, true
		}
	}
	return sim.Point{}, 0, false
}

// pieceIndex returns pieces by their key.
func pieceIndex(pieces []piece) map[pieceKey]piece {
	index := make(map[pieceKey]piece, len(pieces))
	for _, p := range pieces {
		index[p.key] = p
	}
	return index
}
