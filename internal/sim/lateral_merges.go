package sim

import (
	"math"
	"slices"
)

// The lateral merge pairs of docs/design/lateral-merge-zones.md. When a
// network sets LateralMerges, two lanes form a lateral merge pair at a node
// when both end at the node or both start at it, neither is a loop, they do
// not share both endpoints, both are small with no geometry tail, and
// neither is in a maneuver group or a depot. The junction extent of such a
// pair is the body range and the node range, and its pods keep
// lateralClearance(0). The junction resource of the node orders the two
// pods, so a pod in its extent owns the junction, and a pod outside its
// extent has a body that clears each body position of the other lane.

// mergeOptions are the resolution and the work allowance of the sweep of
// one other step against one enclosure.
var mergeOptions = sweepOptions{tolerance: 1e-3, visits: 1 << 12}

// newMergeLateral returns the lateral merge pairs of a network, with
// lateralClearance(0) for each pair, or nil when the network does not set
// LateralMerges. tails, maneuvers and depots are the indexes of the
// network. Only lane keys are in the result, and each lane with a partner
// has its polyline in points.
func newMergeLateral(network Network, tails map[string]geometryTails, maneuvers *maneuverIndex, depots *depotIndex) *depotLateral {
	if !network.LateralMerges {
		return nil
	}
	lanes := make(map[string]int, len(network.Lanes))
	for index, lane := range network.Lanes {
		if _, ok := lanes[lane.ID]; !ok {
			lanes[lane.ID] = index
		}
	}
	// eligible checks clauses 3 and 4: a small lane with no geometry tail,
	// outside the maneuver groups and the depots.
	eligible := func(lane Lane) bool {
		return !largeClassSet(lane.VehicleClasses) && tails[lane.ID] == (geometryTails{}) &&
			!maneuvers.groupLane(lane.ID) && !depots.depotLane(lane.ID)
	}
	positions := indexNodePositions(network.Nodes)
	l := &depotLateral{pairs: make(map[[2]separationKey]float64), neighbors: make(map[int][]int), points: make(map[int][]Point)}
	forEachJunctionPair(network, func(node string, lane, other Lane) {
		if !lateralMergePair(node, lane, other) || !eligible(lane) || !eligible(other) {
			return
		}
		a, b := lanes[lane.ID], lanes[other.ID]
		l.pairs[keyPair(laneKey(a), laneKey(b))] = lateralClearance(0)
		l.neighbors[a] = append(l.neighbors[a], b)
		if l.points[a] == nil {
			l.points[a] = lanePointsBetween(lane, positions[lane.From], positions[lane.To], make([]Point, 0, 65))
		}
	})
	if len(l.pairs) == 0 {
		return nil
	}
	for lane, neighbors := range l.neighbors {
		slices.Sort(neighbors)
		l.neighbors[lane] = slices.Compact(neighbors)
	}
	return l
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
// conflictExtentWithClearance. length is the length of the lane.
//
// The node range holds the end of the lane at the node, so only the far end
// of the body range can make the extent longer. The sweep starts with the
// far end of the node range, and it skips each part that cannot pass it.
func lateralMergeExtent(steps []mergeStep, others []sweepStep, length float64, converging bool) (float64, float64) {
	sweep := mergeSweep{body: lateralBody(0), others: others}
	if converging {
		sweep.bound = length - Clearance
		for _, step := range steps {
			sweep.before(step, 0, 1)
		}
		return max(0, sweep.bound-conflictSampleStep), length
	}
	sweep.bound = Clearance
	for _, step := range slices.Backward(steps) {
		sweep.after(step, 0, 1)
	}
	return 0, min(length, sweep.bound+conflictSampleStep)
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
