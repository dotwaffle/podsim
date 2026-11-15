package sim

import (
	"math"
	"slices"
)

// The lateral merge pairs of docs/design/lateral-merge-zones.md. When a
// network sets LateralMerges, two lanes form a lateral merge pair at a node
// when both end at the node or both start at it, neither is a loop, they do
// not share both endpoints, both are small with no geometry tail, neither
// is in a maneuver group or a depot, and on neither lane does the body
// range reach the far end of the lane. The junction extent of such a pair
// is the body range and the node range, and its pods keep
// lateralClearance(0). The junction resource of the node orders the two
// pods, so a pod in its extent owns the junction, and a pod outside its
// extent has a body that clears each body position of the other lane.
//
// A pod releases the junction at the end of its extent, and the extent
// stops at the lane end. Where the body range reaches the far end of a
// lane, the body of a pod that left the lane can still meet the other
// body, so such a pair keeps the isotropic rule.

// mergeOptions are the resolution and the work allowance of the sweep of
// one other step against one enclosure.
var mergeOptions = sweepOptions{tolerance: 1e-3, visits: 1 << 12}

// newMergeLateral returns the lateral merge pairs of a network, with
// lateralClearance(0) for each pair and the extent of each lane of a pair,
// or nil when the network does not set LateralMerges or has no pair.
// tails, maneuvers and depots are the indexes of the network. Only lane
// keys are in the result, and each lane with a partner has its polyline in
// points. The junction conflicts, the safety observation, the restore, and
// the debris footprint all read this one table.
func newMergeLateral(network Network, tails map[string]geometryTails, maneuvers *maneuverIndex, depots *depotIndex) *depotLateral {
	if !network.LateralMerges {
		return nil
	}
	lanes := newMergeLanes(network)
	// eligible checks clauses 3 and 4: a small lane with no geometry tail,
	// outside the maneuver groups and the depots.
	eligible := func(lane Lane) bool {
		return !largeClassSet(lane.VehicleClasses) && tails[lane.ID] == (geometryTails{}) &&
			!maneuvers.groupLane(lane.ID) && !depots.depotLane(lane.ID)
	}
	// Each candidate pair gives both of its sides at its node, in
	// forEachJunctionPair order. reaches holds the candidate pairs that fail
	// clause 5 on either side.
	type mergeSide struct {
		a, b       int
		start, end float64
	}
	var sides []mergeSide
	reaches := make(map[[2]separationKey]bool)
	forEachJunctionPair(network, func(node string, lane, other Lane) {
		if !lateralMergePair(node, lane, other) || !eligible(lane) || !eligible(other) {
			return
		}
		a, b := lanes.builder.lanes[lane.ID], lanes.builder.lanes[other.ID]
		start, end, far := lanes.extent(a, b, node)
		if far {
			reaches[keyPair(laneKey(a), laneKey(b))] = true
		}
		sides = append(sides, mergeSide{a: a, b: b, start: start, end: end})
	})
	l := &depotLateral{pairs: make(map[[2]separationKey]float64), neighbors: make(map[int][]int), points: make(map[int][]Point), extents: make(map[[2]int][2]float64)}
	for _, side := range sides {
		key := keyPair(laneKey(side.a), laneKey(side.b))
		if reaches[key] {
			continue
		}
		l.pairs[key] = lateralClearance(0)
		l.neighbors[side.a] = append(l.neighbors[side.a], side.b)
		l.points[side.a] = lanes.points[side.a]
		l.extents[[2]int{side.a, side.b}] = [2]float64{side.start, side.end}
	}
	if len(l.pairs) == 0 {
		return nil
	}
	for lane, neighbors := range l.neighbors {
		slices.Sort(neighbors)
		l.neighbors[lane] = slices.Compact(neighbors)
	}
	return l
}

// mergeExtent returns the extent of lane a against lane b of a lateral
// merge pair, by the lane indexes.
func (l *depotLateral) mergeExtent(a, b int) (float64, float64, bool) {
	if l == nil {
		return 0, 0, false
	}
	extent, ok := l.extents[[2]int{a, b}]
	return extent[0], extent[1], ok
}

// mergeLanes holds the polylines, the steps, and the lengths of the lanes
// of the candidate pairs, so that each lane builds its steps once.
type mergeLanes struct {
	builder lateralBuilder
	points  map[int][]Point
	steps   map[int][]mergeStep
	lengths map[int]float64
}

func newMergeLanes(network Network) *mergeLanes {
	return &mergeLanes{builder: newLateralBuilder(network), points: make(map[int][]Point), steps: make(map[int][]mergeStep), lengths: make(map[int]float64)}
}

// extent returns the extent of lane a against lane b of a candidate pair
// at node, and whether the body range of a reaches the far end of a (see
// lateralMergeExtent).
func (m *mergeLanes) extent(a, b int, node string) (float64, float64, bool) {
	others := make([]sweepStep, 0, len(m.laneSteps(b)))
	for _, step := range m.laneSteps(b) {
		others = append(others, step.step)
	}
	return lateralMergeExtent(m.laneSteps(a), others, m.lengths[a], m.builder.network.Lanes[a].To == node)
}

// laneSteps returns the steps of a lane, and keeps its polyline and its
// length.
func (m *mergeLanes) laneSteps(index int) []mergeStep {
	if steps, ok := m.steps[index]; ok {
		return steps
	}
	lane := m.builder.network.Lanes[index]
	points := m.builder.points(lane)
	steps := mergeSteps(m.builder, lane, points)
	length := 0.0
	for i := 1; i < len(points); i++ {
		length += pointDistance(points[i-1], points[i])
	}
	m.points[index], m.steps[index], m.lengths[index] = points, steps, length
	return steps
}

// lateralMergePair reports whether two lanes at a node meet clauses 1 and 2
// of a lateral merge pair: both end at the node or both start at it,
// neither is a loop, and they do not share both endpoints.
func lateralMergePair(node string, lane, other Lane) bool {
	if lane.From == lane.To || other.From == other.To || sharedEndpoints(lane, other) {
		return false
	}
	return lane.To == node && other.To == node || lane.From == node && other.From == node
}

// mergePartner reports whether the lanes with these indexes form a lateral
// merge pair.
func (l *depotLateral) mergePartner(a, b int) bool {
	_, ok := l.clearance(laneKey(a), laneKey(b))
	return ok
}

// mergeStep is a sweep step of a lane of a lateral merge pair, with the
// distances along the lane at its start and at its end. A rotation has the
// same distance at both ends.
type mergeStep struct {
	step     sweepStep
	from, to float64
}

// mergeSteps returns the steps of a lane, with their distances along the
// lane: a segment step along each segment, a vertex step at each inner
// point, and a join step at each end with each lane that ends at its start
// or starts at its end, as lateralBuilder.steps. A lane or a joined lane
// with no length gives a full turn at the node.
func mergeSteps(b lateralBuilder, lane Lane, points []Point) []mergeStep {
	var steps []mergeStep
	heading, started, distance := 0.0, false, 0.0
	for i := 1; i < len(points); i++ {
		p, q := points[i-1], points[i]
		if p == q {
			continue
		}
		next := math.Atan2(q.Y-p.Y, q.X-p.X)
		if started {
			steps = append(steps, mergeStep{step: rotation(p, heading, next), from: distance, to: distance})
		}
		length := pointDistance(p, q)
		steps = append(steps, mergeStep{step: sweepStep{start: sweepPose{x: p.X, y: p.Y, heading: next}, dx: q.X - p.X, dy: q.Y - p.Y}, from: distance, to: distance + length})
		heading, started, distance = next, true, distance+length
	}
	from, to := points[0], points[len(points)-1]
	first, last, ok := b.headings(points)
	turn := func(at Point, distance float64) mergeStep {
		return mergeStep{step: sweepStep{start: sweepPose{x: at.X, y: at.Y}, sweep: 2 * math.Pi}, from: distance, to: distance}
	}
	if !ok {
		return []mergeStep{turn(from, 0)}
	}
	for _, index := range b.incoming[lane.From] {
		if _, before, ok := b.headings(b.points(b.network.Lanes[index])); ok {
			steps = append(steps, mergeStep{step: rotation(from, before, first)})
		} else {
			steps = append(steps, turn(from, 0))
		}
	}
	for _, index := range b.outgoing[lane.To] {
		if after, _, ok := b.headings(b.points(b.network.Lanes[index])); ok {
			steps = append(steps, mergeStep{step: rotation(to, last, after), from: distance, to: distance})
		} else {
			steps = append(steps, turn(to, distance))
		}
	}
	return steps
}

// mergeSweep finds the end of the body range of a lane, against the other
// lane of a lateral merge pair, that is far from the node.
type mergeSweep struct {
	body   sweepBody
	others []sweepStep
	// bound is the far end of the range so far: the least start of a
	// part before the node, or the greatest end of a part after it.
	bound float64
}

// lateralMergeExtent returns the extent of a lane against the other lane of
// a lateral merge pair: the body range, and the last Clearance meters of the
// lane when both lanes end at the node, or the first Clearance meters when
// both start at it. The range gets the sample pad of
// conflictExtentWithClearance. length is the length of the lane. far
// reports whether the body range reaches the far end of the lane (see
// reachesFar), which clause 5 does not allow.
//
// The node range holds the end of the lane at the node, so only the far end
// of the body range can make the extent longer. The sweep starts with the
// far end of the node range, and it skips each part that cannot pass it.
func lateralMergeExtent(steps []mergeStep, others []sweepStep, length float64, converging bool) (start, end float64, far bool) {
	sweep := mergeSweep{body: lateralBody(0), others: others}
	far = sweep.reachesFar(steps, length, converging)
	if converging {
		sweep.bound = length - Clearance
		for _, step := range steps {
			sweep.before(step, 0, 1)
		}
		return max(0, sweep.bound-conflictSampleStep), length, far
	}
	sweep.bound = Clearance
	for _, step := range slices.Backward(steps) {
		sweep.after(step, 0, 1)
	}
	return 0, min(length, sweep.bound+conflictSampleStep), far
}

// reachesFar reports whether the body range of a lane reaches the far end
// of the lane: the start when converging, or the end otherwise. The steps
// at the far end are the segment that starts or ends there and the join
// steps there. For each of them, it halves the part toward the far end, as
// before and after do, so it gives the same parts as the sweep without the
// node range. The body range reaches the far end when such a part of at
// most conflictSampleStep is not clear.
func (m *mergeSweep) reachesFar(steps []mergeStep, length float64, converging bool) bool {
	for _, step := range steps {
		if converging && step.from != 0 || !converging && step.to != length {
			continue
		}
		span := step.to - step.from
		lo, hi := 0.0, 1.0
		for !m.clear(step.step, lo, hi) {
			if span*(hi-lo) <= conflictSampleStep {
				return true
			}
			if converging {
				hi = (lo + hi) / 2
			} else {
				lo = (lo + hi) / 2
			}
		}
	}
	return false
}

// before lowers the bound to the start of the part [lo, hi] of a step when
// the body over the part can meet the body on a step of the other lane. It
// halves a part that is not clear, the first half first, until the part is
// at most conflictSampleStep long. Each result other than sweepClear counts
// as a meeting, so the body at each distance before the bound clears the
// other lane.
func (m *mergeSweep) before(step mergeStep, lo, hi float64) {
	span := step.to - step.from
	if step.from+span*lo >= m.bound || m.clear(step.step, lo, hi) {
		return
	}
	if span*(hi-lo) <= conflictSampleStep {
		m.bound = step.from + span*lo
		return
	}
	mid := (lo + hi) / 2
	m.before(step, lo, mid)
	m.before(step, mid, hi)
}

// after is before for a part after the node: it raises the bound to the
// end of the part, and it takes the second half first.
func (m *mergeSweep) after(step mergeStep, lo, hi float64) {
	span := step.to - step.from
	if step.from+span*hi <= m.bound || m.clear(step.step, lo, hi) {
		return
	}
	if span*(hi-lo) <= conflictSampleStep {
		m.bound = step.from + span*hi
		return
	}
	mid := (lo + hi) / 2
	m.after(step, mid, hi)
	m.after(step, lo, mid)
}

// clear reports whether the enclosure of the body over the part [lo, hi]
// of a step is clear of the body on each step of the other lane.
func (m *mergeSweep) clear(step sweepStep, lo, hi float64) bool {
	enclosure := sweepEnclosure(step, m.body, lo, hi)
	shape := newSweepObstacle(enclosure)
	for _, other := range m.others {
		if sweepFar(other, m.body, enclosure, shape.axes) {
			continue
		}
		visits := 0
		if sweepOne(other, m.body, &shape, mergeOptions, &visits) != sweepClear {
			return false
		}
	}
	return true
}
