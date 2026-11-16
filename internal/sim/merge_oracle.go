package sim

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

// mergeOracleNear is the distance in meters from a vertex or an end of a
// polyline within which the body of a pod can have the heading of either
// side.
const mergeOracleNear = 1e-6

// mergeBodyCheck is the body-and-ownership oracle of decision D6 of
// docs/design/shape-clearance.md. It is independent of Check: it does not
// use pair or the clearance of a pair. It reads the merge table only for
// the pairs and their extents.
//
// For each two traveling pods on the two lanes of a merge or split pair,
// it places the grown body of the class of each pod at the position of the
// pod and at the heading of its lane (see podPoses). When the bodies are
// not disjoint by the separating axis test with sweepGuard, it returns an
// error that names the tick, the node, the owner of the junction, and for
// each pod its lane, its extent, whether it is outside the extent, whether
// it holds the junction, and its bodies.
//
// Each overlap is an error. The extents use the union body of the lanes,
// which holds each class body, so a pod outside its extent clears each body
// position of the other lane. Thus an overlap means that both pods are in
// their extents, and the junction did not order them, or that the extent
// table is wrong. Both are failures. The design also lets a pair pass when
// one pod holds the junction and the other is outside its extent, but with
// correct extents such a pair cannot overlap, and with a wrong table that
// rule hides the error. Only the tests call the oracle.
func (s *Simulation) mergeBodyCheck() error {
	if s.merges == nil {
		return nil
	}
	var pods []*vehicle
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if lane, ok := s.graph.lanes[v.Pod.LaneID]; ok && v.Pod.Activity == Traveling && len(s.merges.neighbors[lane]) > 0 {
			pods = append(pods, v)
		}
	}
	for i, first := range pods {
		for _, second := range pods[i+1:] {
			if err := s.checkMergePair(first, second); err != nil {
				return err
			}
		}
	}
	return nil
}

// mergeOraclePod is a pod of the oracle with the extent of its lane
// against the other lane, the enclosures of its body poses, and whether it
// is outside the extent.
type mergeOraclePod struct {
	v       *vehicle
	extent  [2]float64
	bodies  []sweepRectangle
	outside bool
}

// checkMergePair applies the oracle to two pods. It returns nil when the
// lanes of the pods do not form a merge or split pair.
func (s *Simulation) checkMergePair(first, second *vehicle) error {
	a, b := s.graph.lanes[first.Pod.LaneID], s.graph.lanes[second.Pod.LaneID]
	startA, endA, ok := s.merges.mergeExtent(a, b)
	if !ok {
		return nil
	}
	startB, endB, _ := s.merges.mergeExtent(b, a)
	pods := [2]mergeOraclePod{
		s.mergeOraclePod(first, a, startA, endA),
		s.mergeOraclePod(second, b, startB, endB),
	}
	if mergeBodiesSeparated(pods[0].bodies, pods[1].bodies) {
		return nil
	}
	node := s.network.Lanes[a].From
	if s.network.Lanes[a].To == s.network.Lanes[b].To {
		node = s.network.Lanes[a].To
	}
	junction := resource{kind: junctionResource, id: node}
	var text strings.Builder
	fmt.Fprintf(&text, "tick %d: the bodies of a merge pair at node %s are not disjoint: junction owner %q", s.tick, node, s.owners[junction].String())
	for _, pod := range pods {
		p := pod.v.Pod
		if p.Class == "" {
			p.Class = LegacyClass
		}
		fmt.Fprintf(&text, "; pod %s (%s) on lane %s at %.3f m, extent [%.3f, %.3f], outside %v, holds the junction %v, body",
			p.ID, p.Class, p.LaneID, p.LaneDistance, pod.extent[0], pod.extent[1], pod.outside, s.holdsResource(pod.v, junction))
		for _, body := range pod.bodies {
			fmt.Fprintf(&text, " center (%.3f, %.3f) heading %.4f rad %.3f by %.3f m", body.x, body.y, body.heading, 2*body.halfLength, 2*body.halfWidth)
		}
	}
	return errors.New(text.String())
}

// mergeOraclePod returns the oracle data of pod v on a lane with the
// extent from start to end.
func (s *Simulation) mergeOraclePod(v *vehicle, lane int, start, end float64) mergeOraclePod {
	body := classShapeBody(v.Pod.Class).sweep()
	pod := mergeOraclePod{v: v, extent: [2]float64{start, end}, outside: v.Pod.LaneDistance < start || v.Pod.LaneDistance > end}
	for _, step := range s.podPoses(v.Pod.Position, lane, v.Pod.LaneDistance) {
		pod.bodies = append(pod.bodies, sweepEnclosure(step, body, 0, 1))
	}
	return pod
}

// mergeBodiesSeparated reports whether each body of one pod and each body
// of the other pod are disjoint.
func mergeBodiesSeparated(first, second []sweepRectangle) bool {
	for _, a := range first {
		for _, b := range second {
			if !rectanglesSeparated(a, b) {
				return false
			}
		}
	}
	return true
}

// holdsResource reports whether pod v holds r: v owns r, or v keeps r and a
// pod ahead of v in its platoon owns r, as grant permits.
func (s *Simulation) holdsResource(v *vehicle, r resource) bool {
	owner := s.owners[r]
	if owner.isPod(v.Pod.ID) {
		return true
	}
	_, kept := v.routeReleases[r]
	return kept && s.ownerAheadInPlatoon(v, owner)
}

// podPoses returns the poses of the body of a pod at point at, a distance
// along a lane, as sweep steps. Inside a segment, the body has the heading
// of the segment. At an inner point of the polyline, the body turns from
// one segment to the next. At an end of the lane, the body also turns to
// or from each lane that joins there, as mergeSteps does. A lane with no
// length gives a full turn.
func (s *Simulation) podPoses(at Point, lane int, distance float64) []sweepStep {
	pose := func(heading float64) sweepStep {
		return sweepStep{start: sweepPose{x: at.X, y: at.Y, heading: heading}}
	}
	turn := sweepStep{start: sweepPose{x: at.X, y: at.Y}, sweep: 2 * math.Pi}
	this := s.network.Lanes[lane]
	points := s.network.Polyline(this)
	first, last, ok := polylineEndHeadings(points)
	if !ok {
		return []sweepStep{turn}
	}
	// headings holds the heading of each segment within mergeOracleNear of
	// the distance.
	var headings []float64
	length := 0.0
	for i := 1; i < len(points); i++ {
		p, q := points[i-1], points[i]
		if p == q {
			continue
		}
		end := length + pointDistance(p, q)
		if distance >= length-mergeOracleNear && distance <= end+mergeOracleNear {
			headings = append(headings, math.Atan2(q.Y-p.Y, q.X-p.X))
		}
		length = end
	}
	if len(headings) == 0 {
		headings = []float64{first}
		if distance > length {
			headings = []float64{last}
		}
	}
	steps := []sweepStep{pose(headings[0])}
	for k := 1; k < len(headings); k++ {
		steps = append(steps, rotation(at, headings[k-1], headings[k]))
	}
	if distance > mergeOracleNear && distance < length-mergeOracleNear {
		return steps
	}
	for _, other := range s.network.Lanes {
		if other.From == other.To {
			continue
		}
		otherFirst, otherLast, otherOK := polylineEndHeadings(s.network.Polyline(other))
		switch {
		case distance <= mergeOracleNear && other.To == this.From && !otherOK,
			distance >= length-mergeOracleNear && other.From == this.To && !otherOK:
			steps = append(steps, turn)
		case distance <= mergeOracleNear && other.To == this.From:
			steps = append(steps, rotation(at, otherLast, first))
		case distance >= length-mergeOracleNear && other.From == this.To:
			steps = append(steps, rotation(at, last, otherFirst))
		}
	}
	return steps
}

// polylineEndHeadings returns the heading of the first and of the last
// segment of a polyline that has length. ok is false when no segment has
// length.
func polylineEndHeadings(points []Point) (first, last float64, ok bool) {
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
