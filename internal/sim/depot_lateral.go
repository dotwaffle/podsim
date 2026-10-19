package sim

import (
	"math"
	"slices"
)

// The lateral certification of depot lanes. Two members of different lane
// paths of one depot, each a lane of the path or a slot, can be closer
// than Clearance when a body sweep clears them. The basis is a pod that its
// guideway holds: its reference point stays within 0.25 m of the lane
// polyline, its heading is the heading of the polyline at the reference,
// and a fault stops it on its lane. The body of a small pod is 4.0 m by
// 1.6 m, with an end margin of 0.5 m and a side margin of 0.25 m. It
// extends lateralRear behind the reference, so that it also holds a body
// whose center is the reference. Each side of each body grows by half of
// 1+2u, with u the stop distance at the higher speed limit of the pair.
// Thus two disjoint bodies keep their reference points at least
// lateralClearance apart.
const (
	// lateralRear is the body length behind the reference point.
	lateralRear = 2.5
)

// lateralOptions are the resolution and the work allowance of the sweep of
// one pair.
var lateralOptions = sweepOptions{tolerance: 1e-3, visits: 1 << 16}

// lateralClearance returns the center distance in meters that two small
// pods of a lateral pair need when the higher speed limit of the pair is
// speed: the body width, the tracking allowance of each pod, the margin of
// layer A, and twice the stop distance.
func lateralClearance(speed float64) float64 {
	return 2*sweepHalfWidth + 1 + 2*stopDistance(speed)
}

// lateralBody returns the body of the sweep at a speed. Each side grows
// so that the width of the body is lateralClearance.
func lateralBody(speed float64) sweepBody {
	grow := lateralClearance(speed)/2 - sweepHalfWidth
	return newSweepBody(lateralRear+grow, sweepFront+grow, sweepHalfWidth+grow)
}

// depotLateral holds the lateral pairs of the depots of a network.
// prepareNetwork builds it, and no code writes to it. A nil index has no
// pair.
type depotLateral struct {
	// pairs holds the clearance of each lateral pair, by the keys of the
	// pair with the lower key first.
	pairs map[[2]separationKey]float64
	// neighbors holds the lanes that form a lateral pair with each lane,
	// in network lane order, by lane index. points holds the polyline of
	// each lane that has a neighbor.
	neighbors map[int][]int
	points    map[int][]Point
	// visits and unresolved count the sweep work and the pairs whose
	// sweep ran out of work.
	visits, unresolved int
}

// lateralMember is a lane or a slot of a depot lane path. A slot has the
// same node at both ends, speed 0, and one step with no motion.
type lateralMember struct {
	key         separationKey
	depot, path int
	from, to    string
	speed       float64
	// small is false for a lane that admits a large class or has a
	// geometry tail, and for a large slot.
	small    bool
	points   []Point
	min, max Point
	steps    []sweepStep
}

// lateralCandidate reports whether two members meet clauses 1 to 4 of a
// lateral pair: lane paths of one depot, no shared node, small members,
// and slow lanes.
func lateralCandidate(a, b *lateralMember) bool {
	if a.depot != b.depot || a.path == b.path {
		return false
	}
	if a.from == b.from || a.from == b.to || a.to == b.from || a.to == b.to {
		return false
	}
	if !a.small || !b.small {
		return false
	}
	return lateralSlow(a.speed) && lateralSlow(b.speed)
}

// lateralSlow reports whether a member speed is below the gate of layer A.
// A slot has speed 0.
func lateralSlow(speed float64) bool {
	return speed >= 0 && speed < reducedClearanceSpeed
}

// lateralNear reports whether the boxes of two members are less than
// Clearance apart, as the bank audit tests them. Other pairs keep
// Clearance with no certification.
func lateralNear(a, b *lateralMember) bool {
	return b.min.X-a.max.X < Clearance && a.min.X-b.max.X < Clearance && b.min.Y-a.max.Y < Clearance && a.min.Y-b.max.Y < Clearance
}

// newDepotLateral returns the lateral pairs of the depots of a network,
// in depot, lane path and member order. depots is the depot index of the
// network, tails its geometry tails and berthKeys its berth keys. The
// network does not need to be valid. A pair that the sweep does not clear
// is not a lateral pair, so the certification never refuses a network.
func newDepotLateral(network Network, depots *depotIndex, tails map[string]geometryTails, berthKeys map[string]int) *depotLateral {
	if depots == nil {
		return nil
	}
	b := newLateralBuilder(network)
	l := &depotLateral{pairs: make(map[[2]separationKey]float64), neighbors: make(map[int][]int), points: make(map[int][]Point)}
	for index, depot := range depots.depots {
		members := b.members(index, depot, tails, berthKeys)
		for i := range members {
			for j := i + 1; j < len(members); j++ {
				first, second := &members[i], &members[j]
				if !lateralCandidate(first, second) || !lateralNear(first, second) {
					continue
				}
				speed := max(first.speed, second.speed)
				// The second steps are enclosed one by one, so the member
				// with fewer steps goes second.
				moving, enclosed := first.steps, second.steps
				if len(enclosed) > len(moving) {
					moving, enclosed = enclosed, moving
				}
				visits := 0
				status := sweepPair(moving, enclosed, lateralBody(speed), lateralOptions, &visits)
				l.visits += visits
				if status != sweepClear {
					if status == sweepUnresolved {
						l.unresolved++
					}
					continue
				}
				l.add(first, second, lateralClearance(speed))
			}
		}
	}
	for lane, neighbors := range l.neighbors {
		slices.Sort(neighbors)
		l.neighbors[lane] = slices.Compact(neighbors)
	}
	return l
}

func (l *depotLateral) add(a, b *lateralMember, clearance float64) {
	l.pairs[keyPair(a.key, b.key)] = clearance
	if a.key.kind != laneKeyKind || b.key.kind != laneKeyKind {
		return
	}
	l.neighbors[a.key.index] = append(l.neighbors[a.key.index], b.key.index)
	l.neighbors[b.key.index] = append(l.neighbors[b.key.index], a.key.index)
	l.points[a.key.index], l.points[b.key.index] = a.points, b.points
}

// clearance returns the clearance of two keys that form a lateral pair.
func (l *depotLateral) clearance(a, b separationKey) (float64, bool) {
	if l == nil {
		return 0, false
	}
	clearance, ok := l.pairs[keyPair(a, b)]
	return clearance, ok
}

// neighborLanes returns the lanes that form a lateral pair with a lane, in
// network lane order.
func (l *depotLateral) neighborLanes(lane int) []int {
	if l == nil {
		return nil
	}
	return l.neighbors[lane]
}

// lateralBuilder holds the network data of the members.
type lateralBuilder struct {
	network            Network
	positions          map[string]Point
	lanes              map[string]int
	incoming, outgoing map[string][]int
}

func newLateralBuilder(network Network) lateralBuilder {
	b := lateralBuilder{network: network, positions: indexNodePositions(network.Nodes), lanes: make(map[string]int),
		incoming: make(map[string][]int), outgoing: make(map[string][]int)}
	for index, lane := range network.Lanes {
		if _, ok := b.lanes[lane.ID]; !ok {
			b.lanes[lane.ID] = index
		}
		b.incoming[lane.To] = append(b.incoming[lane.To], index)
		b.outgoing[lane.From] = append(b.outgoing[lane.From], index)
	}
	return b
}

// members returns the members of a depot: for each lane path in order,
// its lanes from the entry to the exit and then its slots from the head.
func (b lateralBuilder) members(depot int, info depotInfo, tails map[string]geometryTails, berthKeys map[string]int) []lateralMember {
	var members []lateralMember
	for path, lanePath := range info.lanes {
		for _, lane := range lanePath.lanes() {
			index, ok := b.lanes[lane.ID]
			if !ok {
				continue
			}
			points := b.points(lane)
			members = append(members, lateralMember{
				key: laneKey(index), depot: depot, path: path, from: lane.From, to: lane.To, speed: lane.SpeedLimit,
				small:  !largeClassSet(lane.VehicleClasses) && tails[lane.ID] == (geometryTails{}),
				points: points, steps: b.steps(lane, points),
			})
		}
		for slot, berth := range lanePath.slots {
			key, ok := berthKeys[berth.ID]
			if !ok {
				continue
			}
			into := lanePath.leadIn
			if slot < len(lanePath.chain) {
				into = lanePath.chain[slot]
			}
			position := b.positions[berth.Node]
			_, heading, _ := b.headings(b.points(into))
			members = append(members, lateralMember{
				key: berthKey(key), depot: depot, path: path, from: berth.Node, to: berth.Node,
				small:  !largeBerth(info.station, berth),
				points: []Point{position, position},
				steps:  []sweepStep{{start: sweepPose{x: position.X, y: position.Y, heading: heading}}},
			})
		}
	}
	for index := range members {
		member := &members[index]
		member.min, member.max = member.points[0], member.points[0]
		for _, point := range member.points {
			member.min.X, member.min.Y = min(member.min.X, point.X), min(member.min.Y, point.Y)
			member.max.X, member.max.Y = max(member.max.X, point.X), max(member.max.Y, point.Y)
		}
	}
	return members
}

func (b lateralBuilder) points(lane Lane) []Point {
	return lanePointsBetween(lane, b.positions[lane.From], b.positions[lane.To], make([]Point, 0, 65))
}

// headings returns the heading of the first and of the last segment of a
// polyline. ok is false for a polyline with no length.
func (b lateralBuilder) headings(points []Point) (first, last float64, ok bool) {
	for i := 1; i < len(points); i++ {
		if points[i] == points[i-1] {
			continue
		}
		heading := math.Atan2(points[i].Y-points[i-1].Y, points[i].X-points[i-1].X)
		if !ok {
			first = heading
		}
		last, ok = heading, true
	}
	return first, last, ok
}

// steps returns the steps of a lane: a segment step along each segment, a
// vertex step at each inner point, and a join step at each end with each
// lane that ends at its start or starts at its end. A lane or a joined
// lane with no length gives a full turn at the node.
func (b lateralBuilder) steps(lane Lane, points []Point) []sweepStep {
	var steps []sweepStep
	heading, started := 0.0, false
	for i := 1; i < len(points); i++ {
		p, q := points[i-1], points[i]
		if p == q {
			continue
		}
		next := math.Atan2(q.Y-p.Y, q.X-p.X)
		if started {
			steps = append(steps, rotation(p, heading, next))
		}
		steps = append(steps, sweepStep{start: sweepPose{x: p.X, y: p.Y, heading: next}, dx: q.X - p.X, dy: q.Y - p.Y})
		heading, started = next, true
	}
	from, to := points[0], points[len(points)-1]
	first, last, ok := b.headings(points)
	if !ok {
		return []sweepStep{{start: sweepPose{x: from.X, y: from.Y}, sweep: 2 * math.Pi}}
	}
	for _, index := range b.incoming[lane.From] {
		if _, before, ok := b.headings(b.points(b.network.Lanes[index])); ok {
			steps = append(steps, rotation(from, before, first))
		} else {
			steps = append(steps, sweepStep{start: sweepPose{x: from.X, y: from.Y}, sweep: 2 * math.Pi})
		}
	}
	for _, index := range b.outgoing[lane.To] {
		if after, _, ok := b.headings(b.points(b.network.Lanes[index])); ok {
			steps = append(steps, rotation(to, last, after))
		} else {
			steps = append(steps, sweepStep{start: sweepPose{x: to.X, y: to.Y}, sweep: 2 * math.Pi})
		}
	}
	return steps
}

// sweepPair returns the most severe result of a body on the first steps
// against the same body on the second steps. Only sweepClear certifies the
// pair. The steps of the pair share the work allowance.
func sweepPair(first, second []sweepStep, body sweepBody, options sweepOptions, visits *int) sweepStatus {
	status := sweepClear
	for _, step := range second {
		for _, fraction := range [2]float64{0, 1} {
			if result := sweepEnclosed(first, step, body, fraction, fraction, 0, options, visits); result != sweepClear {
				return result
			}
		}
		status = max(status, sweepEnclosed(first, step, body, 0, 1, 0, options, visits))
		if status >= sweepUnresolved {
			return status
		}
	}
	return status
}

// sweepEnclosed sweeps the first steps against a rectangle that holds the
// body over the part [lo, hi] of a second step. For one pose the rectangle
// is the body, so a collision is a collision of the two bodies. For a part
// of a step, it halves the part while the rectangle is not clear, until a
// pose collides, the part reaches the tolerance or the depth limit, or the
// work allowance ends. Each half sweeps only the steps that were not clear.
func sweepEnclosed(first []sweepStep, step sweepStep, body sweepBody, lo, hi float64, depth int, options sweepOptions, visits *int) sweepStatus {
	if *visits >= options.visits {
		return sweepUnresolved
	}
	*visits++
	obstacle := sweepEnclosure(step, body, lo, hi)
	axes := sweepAxes(obstacle.heading)
	status := sweepClear
	var open []sweepStep
	for _, other := range first {
		if sweepFar(other, body, obstacle, axes) {
			continue
		}
		if result := sweepOne(other, body, obstacle, options, visits); result != sweepClear {
			status = max(status, result)
			open = append(open, other)
		}
	}
	pose := lo == hi || step.dx == 0 && step.dy == 0 && step.sweep == 0
	if status == sweepClear || pose || status == sweepUnresolved {
		return status
	}
	if step.travel(body)*(hi-lo) <= options.tolerance || depth >= sweepDepth {
		return sweepPotential
	}
	mid := (lo + hi) / 2
	if result := sweepEnclosed(open, step, body, mid, mid, depth+1, options, visits); result != sweepClear {
		return result
	}
	status = sweepEnclosed(open, step, body, lo, mid, depth+1, options, visits)
	if status >= sweepUnresolved {
		return status
	}
	return max(status, sweepEnclosed(open, step, body, mid, hi, depth+1, options, visits))
}

// sweepFar reports whether a body over a whole step stays clear of a
// rectangle with these axes. Each corner of the body is within body.reach
// of the point on the path, and that point stays on the segment of a
// translation, so a disk about the middle of the step holds the body. The
// body is clear when the disk is outside the rectangle on one of its axes
// by more than a guard.
func sweepFar(step sweepStep, body sweepBody, r sweepRectangle, axes [2]Point) bool {
	offset := Point{X: step.start.x + step.dx/2 - r.x, Y: step.start.y + step.dy/2 - r.y}
	radius := (math.Hypot(step.dx, step.dy)/2+body.reach)*(1+1e-9) + sweepGuard
	return math.Abs(dotPoint(offset, axes[0])) > r.halfLength*(1+1e-9)+radius ||
		math.Abs(dotPoint(offset, axes[1])) > r.halfWidth*(1+1e-9)+radius
}

// sweepEnclosure returns a rectangle that holds a body over the part [lo,
// hi] of a step: the bounds of the body on the axes of its heading at the
// middle of the part.
func sweepEnclosure(step sweepStep, body sweepBody, lo, hi float64) sweepRectangle {
	heading := step.at((lo + hi) / 2).heading
	axes := sweepAxes(heading)
	along, across := movingProjection(step, body, axes[0], lo, hi), movingProjection(step, body, axes[1], lo, hi)
	a, c := (along.low+along.high)/2, (across.low+across.high)/2
	return sweepRectangle{
		x: a*axes[0].X + c*axes[1].X, y: a*axes[0].Y + c*axes[1].Y, heading: heading,
		halfLength: (along.high - along.low) / 2, halfWidth: (across.high - across.low) / 2,
	}
}
