package sim

import (
	"math"
	"slices"
	"sync"
)

// The shape pairs of docs/design/shape-clearance.md, part 4. When a network
// sets ShapeClearance, two lanes that share no node form a shape pair when
// the grown body of each lane, swept over the lane and over the join turns
// at both of its ends, stays clear of the grown body of the other lane at
// each pair of positions that are not separated. Two pods on a shape pair
// keep the side-by-side distance of their classes (shapeClassClearance).
// No resource orders them: the sweep proves that their bodies are disjoint
// at each pair of positions, under the guideway-bound assumption of the
// lateral pairs (see depot_lateral.go).
//
// The grown body of a lane is the union of the bodies of the classes that
// the lane admits. A lane that admits a small class and a large class has
// the union body, which reaches 0.2 m further ahead than the large body and
// 1.0 m further behind and 0.45 m further to each side than the small body.

// The bodies of the classes about the reference of a pod, with their
// margins. The small body is 4.0 m by 1.6 m (see lateralRear, sweepFront,
// and sweepHalfWidth). The large body of Express and Group is a centered
// 6 m by 2.5 m body with the same margins.
const (
	largeBodyRear, largeBodyFront, largeBodyHalfWidth = 3.5, 3.5, 1.5
	// shapeGrowth moves each face of a body out by half of the margin of
	// 1 m of the shape rule.
	shapeGrowth = 0.5
)

// shapeOptions are the resolution and the work allowance of the sweep of
// one shape pair.
var shapeOptions = sweepOptions{tolerance: 1e-3, visits: 1 << 16}

// shapeBody is a grown body about the reference of a pod.
type shapeBody struct {
	rear, front, halfWidth float64
}

// classShapeBody returns the grown body of a class.
func classShapeBody(class VehicleClass) shapeBody {
	if largeVehicleClass(class) {
		return shapeBody{rear: largeBodyRear + shapeGrowth, front: largeBodyFront + shapeGrowth, halfWidth: largeBodyHalfWidth + shapeGrowth}
	}
	return shapeBody{rear: lateralRear + shapeGrowth, front: sweepFront + shapeGrowth, halfWidth: sweepHalfWidth + shapeGrowth}
}

// laneShapeBody returns the union of the grown bodies of the classes that a
// lane admits.
func laneShapeBody(classes ClassSet) shapeBody {
	var union shapeBody
	for _, class := range []VehicleClass{LegacyClass, CompactClass, GroupClass, ExpressClass} {
		if !classes.Allows(string(class)) {
			continue
		}
		body := classShapeBody(class)
		union = shapeBody{rear: max(union.rear, body.rear), front: max(union.front, body.front), halfWidth: max(union.halfWidth, body.halfWidth)}
	}
	return union
}

func (b shapeBody) sweep() sweepBody {
	return newSweepBody(b.rear, b.front, b.halfWidth)
}

// shapeClassClearance returns the center distance that two pods of a merge,
// split, or shape pair keep: the sum of the grown half widths of their
// classes. Two disjoint grown bodies keep their references at least this
// far apart.
func shapeClassClearance(first, second VehicleClass) float64 {
	return classShapeBody(first).halfWidth + classShapeBody(second).halfWidth
}

// shapeDiskBound returns the disk bound of two bodies: the sum of their
// corner radii, rounded up to 0.001 m. Two references at the bound or more
// apart have disjoint bodies at each heading.
func shapeDiskBound(first, second sweepBody) float64 {
	return math.Ceil((first.reach+second.reach)*1000) / 1000
}

// pairRange is a part of each path of a pair, as distance ranges from the
// start of each path.
type pairRange struct {
	first, second [2]float64
}

// shapeCertifier sweeps the lanes of candidate pairs. It builds the steps
// of each lane once.
type shapeCertifier struct {
	lanes   *mergeLanes
	options sweepOptions
	// sweepAll turns off the disk bound, so that each part is swept. Only
	// tests set it.
	sweepAll bool
	// visits counts the sweep work, sweeps the pairs that needed a sweep,
	// and unresolved the pairs whose sweep ran out of work.
	visits, sweeps, unresolved int
}

func newShapeCertifier(network Network, options sweepOptions) *shapeCertifier {
	return &shapeCertifier{lanes: newMergeLanes(network), options: options}
}

// certify returns the result of the sweep of lanes a and b over the parts
// of ranges, or over the whole lanes when ranges is nil. A part whose
// polylines stay at or beyond the disk bound needs no sweep. Only
// sweepClear certifies the pair.
func (c *shapeCertifier) certify(a, b int, ranges []pairRange) sweepStatus {
	lanes := c.lanes.builder.network.Lanes
	bodyA, bodyB := laneShapeBody(lanes[a].VehicleClasses).sweep(), laneShapeBody(lanes[b].VehicleClasses).sweep()
	stepsA, stepsB := c.lanes.laneSteps(a), c.lanes.laneSteps(b)
	pointsA, pointsB := c.lanes.points[a], c.lanes.points[b]
	if ranges == nil {
		ranges = []pairRange{{first: [2]float64{0, c.lanes.lengths[a]}, second: [2]float64{0, c.lanes.lengths[b]}}}
	}
	bound := shapeDiskBound(bodyA, bodyB)
	visits, swept := 0, false
	defer func() {
		c.visits += visits
		if swept {
			c.sweeps++
		}
	}()
	for _, part := range ranges {
		if !c.sweepAll && !bankPathsClose(rangePiece(pointsA, part.first), rangePiece(pointsB, part.second), bound+separationTolerance) {
			continue
		}
		swept = true
		moving, enclosed := clipShapeSteps(stepsA, part.first), clipShapeSteps(stepsB, part.second)
		bodies := sweepPairBodies{first: bodyA, second: bodyB}
		// The second steps are enclosed one by one, so the lane with fewer
		// steps goes second.
		if len(enclosed) > len(moving) {
			moving, enclosed = enclosed, moving
			bodies.first, bodies.second = bodies.second, bodies.first
		}
		if status := sweepBodies(moving, enclosed, bodies, c.options, &visits); status != sweepClear {
			if status == sweepUnresolved {
				c.unresolved++
			}
			return status
		}
	}
	return sweepClear
}

// rangePiece returns the part of a polyline in a distance range, with the
// slack of the envelope reach on each side.
func rangePiece(points []Point, bounds [2]float64) []Point {
	return polylinePiece(points, bounds[0]-envelopeReachSlack, bounds[1]+envelopeReachSlack)
}

// clipShapeSteps returns the steps of a lane in a distance range, with the
// slack of the envelope reach on each side. A segment step keeps the part
// in the range. A turn keeps its whole sweep when its distance is in the
// range, so the join turns at an end of the lane are in each range that
// holds the end.
func clipShapeSteps(steps []mergeStep, bounds [2]float64) []sweepStep {
	lo, hi := bounds[0]-envelopeReachSlack, bounds[1]+envelopeReachSlack
	clipped := make([]sweepStep, 0, len(steps))
	for _, step := range steps {
		if step.from == step.to {
			if step.from >= lo && step.from <= hi {
				clipped = append(clipped, step.step)
			}
			continue
		}
		from, to := max(step.from, lo), min(step.to, hi)
		if from > to {
			continue
		}
		if from == step.from && to == step.to {
			clipped = append(clipped, step.step)
			continue
		}
		span := step.to - step.from
		start, end := (from-step.from)/span, (to-step.from)/span
		clipped = append(clipped, sweepStep{start: step.step.at(start), dx: step.step.dx * (end - start), dy: step.step.dy * (end - start)})
	}
	return clipped
}

// shapePairs returns the shape pairs of a network that sets
// ShapeClearance, or nil when the network does not set it. paths are the
// paths of the audit, sorted by the least X. The table holds the pairs
// that the audit would otherwise test: two lanes that share no node, come
// closer than the clearance of their classes at positions that are not
// separated, and pass the sweep. Each other pair of lanes never comes that
// close, so it keeps its rule with no change. The value of each pair is
// the side-by-side distance of the bodies of its two lanes, which the
// audit then requires.
//
// The rules that come first stay: a lane of a maneuver group or a depot is
// in no shape pair, and nor is a pair whose window pieces have a reduced
// clearance. A pair with a large lane needs the envelope reach, so a
// network whose geometry tails are not finite has no such pair.
func (audit bankAudit) shapePairs(n Network, paths []bankAuditPath, scanClearance float64) *depotLateral {
	if !n.ShapeClearance {
		return nil
	}
	shapes := &depotLateral{pairs: make(map[[2]separationKey]float64), neighbors: make(map[int][]int), points: make(map[int][]Point)}
	certifier := audit.certifier
	for i, first := range paths {
		for _, second := range paths[i+1:] {
			if second.min.X-first.max.X >= scanClearance {
				break
			}
			if !audit.shapeCandidate(first, second) {
				continue
			}
			clearance := Clearance
			if first.large || second.large {
				clearance = largeClearance
			}
			if first.min.Y-second.max.Y >= clearance || second.min.Y-first.max.Y >= clearance || first.min.X-second.max.X >= clearance || second.min.X-first.max.X >= clearance {
				continue
			}
			planes := audit.planes(first, second)
			if planes.exempt || (first.large || second.large) && planes.held && audit.envelope.get() == nil {
				continue
			}
			if !planes.close(first.points, second.points, clearance) {
				continue
			}
			if slices.ContainsFunc(audit.retention.pairPieces(first.member, second.member), func(piece pairPiece) bool { return piece.clearance != clearance }) {
				continue
			}
			if certifier == nil {
				certifier = newShapeCertifier(n, shapeOptions)
			}
			if certifier.certify(first.key.index, second.key.index, planes.ranges) != sweepClear {
				continue
			}
			a, b := first.key.index, second.key.index
			side := laneShapeBody(n.Lanes[a].VehicleClasses).halfWidth + laneShapeBody(n.Lanes[b].VehicleClasses).halfWidth
			shapes.add(&lateralMember{key: first.key, points: first.points}, &lateralMember{key: second.key, points: second.points}, side)
		}
	}
	if certifier != nil {
		shapes.visits, shapes.unresolved = certifier.visits, certifier.unresolved
	}
	for lane, neighbors := range shapes.neighbors {
		slices.Sort(neighbors)
		shapes.neighbors[lane] = slices.Compact(neighbors)
	}
	return shapes
}

// shapeCandidate reports whether two paths can form a shape pair: two
// lanes that share no node, neither in a maneuver group or a depot.
func (audit bankAudit) shapeCandidate(first, second bankAuditPath) bool {
	if first.key.kind != laneKeyKind || second.key.kind != laneKeyKind || safetyLocationsShareNode(first.location, second.location) {
		return false
	}
	for _, path := range []bankAuditPath{first, second} {
		if audit.maneuvers.groupLane(path.id) || audit.depots.depotLane(path.id) {
			return false
		}
	}
	return true
}

// pairPlanes is the plane result of a pair of paths in the audit. An exempt
// pair is separated at each pair of positions. Otherwise ranges holds the
// parts that are not separated, or nil for the whole paths. held is true
// when the planes of the two paths are separated, so that only the reach
// of a large path denies the exemption (see PlaneReachError).
type pairPlanes struct {
	exempt, held bool
	ranges       []pairRange
}

// planes returns the plane result of a pair of paths. Two paths whose own
// planes are not separated are not separated at any position. Without
// ShapeClearance, or when the envelope reach cannot be built, a large pair
// on separated planes is exempt only when the whole-lane plane reach is
// separated. With ShapeClearance, it is not separated at the parts where
// the envelope reach of the two paths overlap.
func (audit bankAudit) planes(first, second bankAuditPath) pairPlanes {
	if !first.separated(second) {
		return pairPlanes{}
	}
	if !first.large && !second.large {
		return pairPlanes{exempt: true}
	}
	if reach := audit.envelope.get(); reach != nil {
		ranges := reach.unseparated(first.key, second.key)
		return pairPlanes{exempt: len(ranges) == 0, held: true, ranges: ranges}
	}
	return pairPlanes{exempt: first.reachSeparated(second, audit.reach), held: true}
}

// cut returns the parts of a piece that are not separated.
func (p pairPlanes) cut(piece pairPiece) []pairPiece {
	if p.ranges == nil {
		return []pairPiece{piece}
	}
	var parts []pairPiece
	for _, part := range p.ranges {
		first := [2]float64{max(piece.first[0], part.first[0]), min(piece.first[1], part.first[1])}
		second := [2]float64{max(piece.second[0], part.second[0]), min(piece.second[1], part.second[1])}
		if first[0] <= first[1] && second[0] <= second[1] {
			parts = append(parts, pairPiece{first: first, second: second, clearance: piece.clearance})
		}
	}
	return parts
}

// close reports whether two polylines come closer than clearance at a part
// that is not separated.
func (p pairPlanes) close(first, second []Point, clearance float64) bool {
	if p.ranges == nil {
		return bankPathsClose(first, second, clearance)
	}
	for _, part := range p.ranges {
		if bankPathsClose(polylinePiece(first, part.first[0], part.first[1]), polylinePiece(second, part.second[0], part.second[1]), clearance) {
			return true
		}
	}
	return false
}

// unseparated returns the parts of two paths at which the envelope reach of
// the first and the envelope reach of the second are not separated. For
// each span of the first path, it joins the spans of the second path that
// follow each other into one part.
func (r *envelopeReach) unseparated(a, b separationKey) []pairRange {
	first, second := r.pathSpans(a), r.pathSpans(b)
	var ranges []pairRange
	for i := range first {
		open := false
		for j := range second {
			if first[i].separated(&second[j]) {
				open = false
				continue
			}
			if open {
				ranges[len(ranges)-1].second[1] = second[j].to
				continue
			}
			ranges = append(ranges, pairRange{first: [2]float64{first[i].from, first[i].to}, second: [2]float64{second[j].from, second[j].to}})
			open = true
		}
	}
	return ranges
}

// pathSpans returns the spans of a lane or a berth.
func (r *envelopeReach) pathSpans(key separationKey) []reachSpan {
	if key.kind == berthKeyKind {
		return r.berths[key.index : key.index+1]
	}
	return r.lanes[key.index]
}

// lazyEnvelopeReach builds the envelope reach of a network that sets
// ShapeClearance when a pair first needs it. It builds no index when a
// bound of the tails is not finite: preparation refuses such a network
// after the audit (see prepareNetworkTails), and a walk with no bound is
// slow and large. A nil value has no index.
type lazyEnvelopeReach struct {
	once    sync.Once
	network Network
	tails   map[string]geometryTails
	index   *envelopeReach
}

func newLazyEnvelopeReach(n Network, tails map[string]geometryTails) *lazyEnvelopeReach {
	if !n.ShapeClearance {
		return nil
	}
	return &lazyEnvelopeReach{network: n, tails: tails}
}

func (l *lazyEnvelopeReach) get() *envelopeReach {
	if l == nil {
		return nil
	}
	l.once.Do(func() {
		for _, lane := range l.network.Lanes {
			if bounds := l.tails[lane.ID]; !finite(bounds.tail) || !finite(bounds.fromTail) || !finite(bounds.front) {
				return
			}
		}
		l.index = newEnvelopeReach(l.network, l.tails)
	})
	return l.index
}
