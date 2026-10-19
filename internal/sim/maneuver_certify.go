package sim

import (
	"fmt"
	"math"
)

// The maneuver certification sweeps a rectangle that encloses a small pod
// along the lane polylines of each maneuver group, and checks that it stays
// clear of the parked envelopes of the other berths of the group. It is
// the float64 interval bisection of the offline maneuver checker. It
// covers the poses of its steps only, and makes no claim about a smooth
// physical path.
const (
	// The moving body about the rear axle: body 4 m, wheelbase 2.4 m, end
	// margin 0.5 m and side margin 0.25 m.
	sweepRear, sweepFront, sweepHalfWidth = 1.3, 3.7, 1.05
	// A parked envelope is 5.0 m by 2.1 m, centered 1.2 m ahead of the
	// berth node.
	envelopeLength, envelopeWidth, envelopeAhead = 5.0, 2.1, 1.2
	// sweepGuard is the gap that a separating axis needs, sweepPad widens
	// each projection, and sweepAnglePad widens each angle range.
	sweepGuard, sweepPad, sweepAnglePad = 1e-7, 1e-8, 1e-12
	// sweepDepth bounds the bisection.
	sweepDepth = 20
)

// sweepOptions are the resolution and the work allowance of the sweep of
// one lane against one berth.
type sweepOptions struct {
	tolerance float64
	visits    int
}

// certifyOptions are the options of the certification.
var certifyOptions = sweepOptions{tolerance: 1e-3, visits: 8192}

// sweepStatus is the result of a sweep. Only sweepClear certifies a pair.
type sweepStatus int

// The results of a sweep, in increasing severity.
const (
	sweepClear sweepStatus = iota
	sweepPotential
	sweepUnresolved
	sweepCollision
)

// certifiedPairs holds the pairs of separation keys that the maneuver
// certification clears: a group lane and a berth of its group, and two
// berths of one group. The lower key is first.
type certifiedPairs map[[2]separationKey]bool

// certified reports whether two keys form a certified pair.
func (c certifiedPairs) certified(a, b separationKey) bool {
	if c == nil {
		return false
	}
	if b.kind < a.kind || b.kind == a.kind && b.index < a.index {
		a, b = b, a
	}
	return c[[2]separationKey{a, b}]
}

func (c certifiedPairs) add(a, b separationKey) {
	if b.kind < a.kind || b.kind == a.kind && b.index < a.index {
		a, b = b, a
	}
	c[[2]separationKey{a, b}] = true
}

// sweepPose is the rear axle position and the heading of the body.
type sweepPose struct {
	x, y, heading float64
}

// sweepStep moves the body from start by a translation (dx, dy) at its
// heading, or rotates it in place by sweep.
type sweepStep struct {
	start         sweepPose
	dx, dy, sweep float64
}

func (step sweepStep) at(fraction float64) sweepPose {
	return sweepPose{x: step.start.x + step.dx*fraction, y: step.start.y + step.dy*fraction, heading: step.start.heading + step.sweep*fraction}
}

// travel bounds the distance that a corner of the body moves in the step.
func (step sweepStep) travel() float64 {
	return math.Hypot(step.dx, step.dy) + math.Hypot(sweepFront, sweepHalfWidth)*math.Abs(step.sweep)
}

// sweepRectangle is a parked envelope.
type sweepRectangle struct {
	x, y, heading float64
}

// certifyManeuvers certifies the maneuver groups, in group order. Two
// berths of a group need disjoint envelopes. Then each group lane, in
// lane order, must be clear of each envelope of its group except the one
// of the berth of its closed bay chain. It returns the certified pairs.
func certifyManeuvers(network Network, maneuvers *maneuverIndex) (certifiedPairs, error) {
	if maneuvers == nil {
		return nil, nil
	}
	return newManeuverCertifier(network, maneuvers).certify(certifyOptions)
}

type maneuverCertifier struct {
	network   Network
	maneuvers *maneuverIndex
	positions map[string]Point
	lanes     map[string]int
	// envelopes holds the parked envelope of each group berth.
	envelopes map[string]sweepRectangle
	// lanePoses holds the heading of the first and the last segment of
	// each group lane, as the body faces on the lane.
	first, last map[string]float64
}

func newManeuverCertifier(network Network, maneuvers *maneuverIndex) *maneuverCertifier {
	c := &maneuverCertifier{
		network: network, maneuvers: maneuvers, positions: indexNodePositions(network.Nodes), lanes: make(map[string]int),
		envelopes: make(map[string]sweepRectangle), first: make(map[string]float64), last: make(map[string]float64),
	}
	for index, lane := range network.Lanes {
		c.lanes[lane.ID] = index
	}
	for _, group := range maneuvers.groups {
		for _, id := range group.LaneIDs {
			headings := c.headings(network.Lanes[c.lanes[id]])
			c.first[id], c.last[id] = headings[0], headings[len(headings)-1]
		}
		for _, id := range group.BerthIDs {
			lane := network.Lanes[c.lanes[maneuvers.inLanes[id]]]
			from := c.positions[lane.From]
			if lane.Control != nil {
				from = *lane.Control
			}
			to := c.positions[lane.To]
			heading := math.Atan2(to.Y-from.Y, to.X-from.X)
			c.envelopes[id] = sweepRectangle{x: to.X + envelopeAhead*math.Cos(heading), y: to.Y + envelopeAhead*math.Sin(heading), heading: heading}
		}
	}
	return c
}

func (c *maneuverCertifier) certify(options sweepOptions) (certifiedPairs, error) {
	pairs := make(certifiedPairs)
	for _, group := range c.maneuvers.groups {
		for i, first := range group.BerthIDs {
			for _, second := range group.BerthIDs[i+1:] {
				if !rectanglesSeparated(c.envelopes[first], c.envelopes[second]) {
					return nil, fmt.Errorf("maneuver group %q berths %q and %q overlap", group.ID, first, second)
				}
				pairs.add(berthKey(c.maneuvers.berthKeys[first]), berthKey(c.maneuvers.berthKeys[second]))
			}
		}
		for _, id := range group.LaneIDs {
			steps := c.steps(id)
			own, _ := c.maneuvers.chainBerth(id)
			for _, berth := range group.BerthIDs {
				if berth == own {
					continue
				}
				if status := sweepSteps(steps, c.envelopes[berth], options); status != sweepClear {
					return nil, fmt.Errorf("maneuver group %q lane %q is not clear of berth %q", group.ID, id, berth)
				}
				pairs.add(laneKey(c.lanes[id]), berthKey(c.maneuvers.berthKeys[berth]))
			}
		}
	}
	return pairs, nil
}

// headings returns the heading of each segment of a lane with a length, as
// the body faces: against the segment on a reverse lane.
func (c *maneuverCertifier) headings(lane Lane) []float64 {
	points := lanePointsBetween(lane, c.positions[lane.From], c.positions[lane.To], make([]Point, 0, 65))
	var headings []float64
	for i := 1; i < len(points); i++ {
		if points[i] == points[i-1] {
			continue
		}
		heading := math.Atan2(points[i].Y-points[i-1].Y, points[i].X-points[i-1].X)
		if c.maneuvers.reverse[lane.ID] {
			heading += math.Pi
		}
		headings = append(headings, heading)
	}
	return headings
}

// steps returns the steps of a group lane: a segment step along each
// segment, a vertex step at each inner point, a join step at each end with
// each group lane before or after it, and a berth step at a berth node.
func (c *maneuverCertifier) steps(id string) []sweepStep {
	lane := c.network.Lanes[c.lanes[id]]
	points := lanePointsBetween(lane, c.positions[lane.From], c.positions[lane.To], make([]Point, 0, 65))
	var steps []sweepStep
	heading, started := 0.0, false
	for i := 1; i < len(points); i++ {
		a, b := points[i-1], points[i]
		if a == b {
			continue
		}
		next := math.Atan2(b.Y-a.Y, b.X-a.X)
		if c.maneuvers.reverse[id] {
			next += math.Pi
		}
		if started {
			steps = append(steps, rotation(a, heading, next))
		}
		steps = append(steps, sweepStep{start: sweepPose{x: a.X, y: a.Y, heading: next}, dx: b.X - a.X, dy: b.Y - a.Y})
		heading, started = next, true
	}
	group, _ := c.maneuvers.laneGroup(id)
	for _, other := range group.LaneIDs {
		if other == id {
			continue
		}
		next := c.network.Lanes[c.lanes[other]]
		if next.To == lane.From {
			steps = append(steps, rotation(c.positions[lane.From], c.last[other], c.first[id]))
		}
		if next.From == lane.To {
			steps = append(steps, rotation(c.positions[lane.To], c.last[id], c.first[other]))
		}
	}
	for _, berth := range group.BerthIDs {
		envelope := c.envelopes[berth]
		node := c.network.Lanes[c.lanes[c.maneuvers.inLanes[berth]]].To
		if lane.To == node {
			steps = append(steps, rotation(c.positions[node], c.last[id], envelope.heading))
		}
		if lane.From == node {
			steps = append(steps, rotation(c.positions[node], envelope.heading, c.first[id]))
		}
	}
	return steps
}

// rotation returns the step that rotates the body in place at a point
// from one heading to another, by the smaller angle.
func rotation(at Point, from, to float64) sweepStep {
	sweep := math.Remainder(to-from, 2*math.Pi)
	return sweepStep{start: sweepPose{x: at.X, y: at.Y, heading: from}, sweep: sweep}
}

// sweepSteps returns the most severe result of the steps against one
// envelope. The steps share the work allowance.
func sweepSteps(steps []sweepStep, obstacle sweepRectangle, options sweepOptions) sweepStatus {
	visits := 0
	for _, step := range steps {
		if status := sweepOne(step, obstacle, options, &visits); status != sweepClear {
			return status
		}
	}
	return sweepClear
}

// sweepOne checks the poses at both ends of a step, and then bisects the
// step until each interval is separated, a pose touches the obstacle, an
// interval reaches the tolerance or the depth limit, or the work allowance
// ends.
func sweepOne(step sweepStep, obstacle sweepRectangle, options sweepOptions, visits *int) sweepStatus {
	for _, fraction := range [2]float64{0, 1} {
		if !sweepSeparated(step, obstacle, fraction, fraction) {
			return sweepCollision
		}
	}
	if step.dx == 0 && step.dy == 0 && step.sweep == 0 {
		return sweepClear
	}
	type interval struct {
		lo, hi float64
		depth  int
	}
	var stack [sweepDepth + 1]interval
	stack[0] = interval{lo: 0, hi: 1}
	size, status, travel := 1, sweepClear, step.travel()
	for size > 0 {
		size--
		current := stack[size]
		if *visits >= options.visits {
			return max(status, sweepUnresolved)
		}
		*visits++
		mid := (current.lo + current.hi) / 2
		if !sweepSeparated(step, obstacle, mid, mid) {
			return sweepCollision
		}
		if sweepSeparated(step, obstacle, current.lo, current.hi) {
			continue
		}
		if travel*(current.hi-current.lo) <= options.tolerance || current.depth >= sweepDepth {
			status = sweepPotential
			continue
		}
		stack[size] = interval{lo: mid, hi: current.hi, depth: current.depth + 1}
		stack[size+1] = interval{lo: current.lo, hi: mid, depth: current.depth + 1}
		size += 2
	}
	return status
}

// sweepCorners are the corners of the moving body about the rear axle.
var sweepCorners = [4]Point{{-sweepRear, -sweepHalfWidth}, {sweepFront, -sweepHalfWidth}, {sweepFront, sweepHalfWidth}, {-sweepRear, sweepHalfWidth}}

// sweepSeparated reports whether a separating axis of the body at the
// middle of the interval or of the obstacle separates the body over the
// interval from the obstacle.
func sweepSeparated(step sweepStep, obstacle sweepRectangle, lo, hi float64) bool {
	mid := step.at((lo + hi) / 2)
	for _, heading := range [2]float64{mid.heading, obstacle.heading} {
		for _, axis := range sweepAxes(heading) {
			if intervalsDisjoint(movingProjection(step, axis, lo, hi), staticProjection(obstacle, axis)) {
				return true
			}
		}
	}
	return false
}

// rectanglesSeparated reports whether two envelopes are disjoint, by a
// separating axis of either one.
func rectanglesSeparated(a, b sweepRectangle) bool {
	for _, heading := range [2]float64{a.heading, b.heading} {
		for _, axis := range sweepAxes(heading) {
			if intervalsDisjoint(staticProjection(a, axis), staticProjection(b, axis)) {
				return true
			}
		}
	}
	return false
}

func sweepAxes(heading float64) [2]Point {
	s, c := math.Sincos(heading)
	norm := math.Hypot(s, c)
	return [2]Point{{c / norm, s / norm}, {-s / norm, c / norm}}
}

func dotPoint(p, axis Point) float64 { return p.X*axis.X + p.Y*axis.Y }

func worldCorner(p sweepPose, corner Point) Point {
	s, c := math.Sincos(p.heading)
	return Point{X: p.x + corner.X*c - corner.Y*s, Y: p.y + corner.X*s + corner.Y*c}
}

// projection is a closed range on an axis.
type projection struct{ low, high float64 }

// padded widens a range by sweepPad and one more rounding step. A range
// that is not finite covers the whole axis.
func padded(low, high float64) projection {
	if !finite(low) || !finite(high) || high < low {
		return projection{math.Inf(-1), math.Inf(1)}
	}
	return projection{math.Nextafter(low-sweepPad, math.Inf(-1)), math.Nextafter(high+sweepPad, math.Inf(1))}
}

func intervalsDisjoint(a, b projection) bool {
	return a.high < b.low-sweepGuard || b.high < a.low-sweepGuard
}

func staticProjection(r sweepRectangle, axis Point) projection {
	low, high := math.Inf(1), math.Inf(-1)
	pose := sweepPose(r)
	for _, corner := range [4]Point{{-envelopeLength / 2, -envelopeWidth / 2}, {envelopeLength / 2, -envelopeWidth / 2}, {envelopeLength / 2, envelopeWidth / 2}, {-envelopeLength / 2, envelopeWidth / 2}} {
		v := dotPoint(worldCorner(pose, corner), axis)
		low, high = min(low, v), max(high, v)
	}
	return padded(low, high)
}

// movingProjection bounds the projection of the body over a part of a
// step. A translation moves each corner along a line, so the range of the
// corners at the two ends holds it. A rotation moves each corner on a
// circle about the rear axle.
func movingProjection(step sweepStep, axis Point, lo, hi float64) projection {
	low, high := math.Inf(1), math.Inf(-1)
	for _, corner := range sweepCorners {
		if step.sweep == 0 {
			v0, v1 := dotPoint(worldCorner(step.at(lo), corner), axis), dotPoint(worldCorner(step.at(hi), corner), axis)
			low, high = min(low, v0, v1), max(high, v0, v1)
			continue
		}
		a := axis.X*corner.X + axis.Y*corner.Y
		b := axis.Y*corner.X - axis.X*corner.Y
		span := trigRange(a, b, step.start.heading+step.sweep*lo, step.start.heading+step.sweep*hi)
		constant := dotPoint(Point{X: step.start.x, Y: step.start.y}, axis)
		low, high = min(low, constant+span.low), max(high, constant+span.high)
	}
	return padded(low, high)
}

// trigRange returns the range of a cos h + b sin h for h between low and
// high.
func trigRange(a, b, low, high float64) projection {
	if low > high {
		low, high = high, low
	}
	amplitude := math.Hypot(a, b)
	if high-low >= 2*math.Pi-sweepAnglePad {
		return projection{-amplitude, amplitude}
	}
	f := func(h float64) float64 { return a*math.Cos(h) + b*math.Sin(h) }
	v0, v1 := f(low), f(high)
	result := projection{min(v0, v1), max(v0, v1)}
	phase := math.Atan2(b, a)
	first := math.Ceil((low - sweepAnglePad - phase) / math.Pi)
	last := math.Floor((high + sweepAnglePad - phase) / math.Pi)
	for k := first; k <= last; k++ {
		value := amplitude
		if int(k)%2 != 0 {
			value = -amplitude
		}
		result.low, result.high = min(result.low, value), max(result.high, value)
	}
	return result
}
