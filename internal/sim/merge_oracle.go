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

// The bodies of the oracle, from the table of section 3.2 of the design,
// about the reference of a pod. The oracle keeps its own copy, so that an
// error in the bodies of the certification (classShapeBody) does not also
// shrink the bodies of the oracle. TestOracleBodiesMatchClassBodies keeps
// the two equal.
const (
	oracleSmallRear, oracleSmallFront, oracleSmallHalfWidth = 2.5, 3.7, 1.05
	oracleLargeRear, oracleLargeFront, oracleLargeHalfWidth = 3.5, 3.5, 1.5
	// oracleMargin is the margin of the shape rule. Each face of a body
	// moves out by half of it.
	oracleMargin = 1.0
	// oraclePadLimit is the pad in meters below which the refinement of a
	// turn stops and reports an overlap (see oraclePosesDisjoint).
	oraclePadLimit = 1e-6
	// oracleGap is the gap in meters that a separating axis needs, the
	// value of sweepGuard. Two bodies that touch are not disjoint.
	oracleGap = 1e-7
)

// LateralBodyCheckForTest runs lateralBodyCheck. It is a test entry: the
// session tests run the D6 oracle on the restored copies of their saves.
// No production code calls it.
func (s *Simulation) LateralBodyCheckForTest() error {
	return s.lateralBodyCheck()
}

// JoinTurnPodsForTest returns the IDs of the traveling pods at a join turn:
// at a lane end, where the oracle turns the body toward a lane that joins
// there with another heading (see podPoses). It is a test entry: the
// session tests save at such a state. No production code calls it.
func (s *Simulation) JoinTurnPodsForTest() []string {
	var pods []string
	for i := range s.vehicles {
		v := &s.vehicles[i]
		lane, ok := s.graph.lanes[v.Pod.LaneID]
		if v.Pod.Activity != Traveling || !ok {
			continue
		}
		this := s.network.Lanes[lane]
		points := s.network.Polyline(this)
		first, last, ok := polylineEndHeadings(points)
		if !ok {
			continue
		}
		length := 0.0
		for k := 1; k < len(points); k++ {
			length += pointDistance(points[k-1], points[k])
		}
		distance := v.Pod.LaneDistance
		atStart, atEnd := distance <= mergeOracleNear, distance >= length-mergeOracleNear
		if !atStart && !atEnd {
			continue
		}
		for _, other := range s.network.Lanes {
			otherFirst, otherLast, otherOK := polylineEndHeadings(s.network.Polyline(other))
			if other.From == other.To || !otherOK {
				continue
			}
			if atStart && other.To == this.From && math.Abs(math.Remainder(first-otherLast, 2*math.Pi)) > 0 ||
				atEnd && other.From == this.To && math.Abs(math.Remainder(otherFirst-last, 2*math.Pi)) > 0 {
				pods = append(pods, v.Pod.ID)
				break
			}
		}
	}
	return pods
}

// lateralBodyCheck is the oriented body oracle of the lateral pairs of
// docs/design/shape-clearance.md. It is independent of Check: it does not
// use pair or the clearance of a pair. It reads the merge table only for
// the pairs and their extents, and the shape table only for the pairs.
//
// For each two traveling pods on the two lanes of a merge, split, or shape
// pair, it places the grown body of the class of each pod (see
// oracleClassBody) at the position of the pod and at the heading of its
// lane, and at each heading of a turn at a vertex or an end of the lane
// (see podPoses). When the bodies are not disjoint by its own separating
// axis test, and the pods are not on different planes (see
// oraclePlanesSeparated), it returns an error that names the tick and the
// pair, and for each pod its lane, its body, and its poses. For a merge or split pair, the error also names the node, the
// owner of the junction, and for each pod its extent, whether it is outside
// the extent, and whether it holds the junction (decision D6).
//
// Each overlap is an error. The extents use the union body of the lanes,
// which holds each class body, so a pod outside its extent clears each body
// position of the other lane. Thus an overlap on a merge or split pair
// means that both pods are in their extents, and the junction did not
// order them, or that the extent table is wrong. Both are failures. The
// design also lets a pair pass when one pod holds the junction and the
// other is outside its extent, but with correct extents such a pair cannot
// overlap, and with a wrong table that rule hides the error. The sweep
// certifies a shape pair as disjoint at each pair of positions, so an
// overlap on a shape pair means that the shape table holds the pair in
// error. Only the tests call the oracle.
func (s *Simulation) lateralBodyCheck() error {
	if s.merges == nil && s.shapes == nil {
		return nil
	}
	var pods []*vehicle
	for i := range s.vehicles {
		v := &s.vehicles[i]
		lane, ok := s.graph.lanes[v.Pod.LaneID]
		if ok && v.Pod.Activity == Traveling && (len(s.merges.neighborLanes(lane)) > 0 || len(s.shapes.neighborLanes(lane)) > 0) {
			pods = append(pods, v)
		}
	}
	for i, first := range pods {
		for _, second := range pods[i+1:] {
			if err := s.checkMergePair(first, second); err != nil {
				return err
			}
			if err := s.checkShapePair(first, second); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkShapePair applies the oracle to two pods. It returns nil when the
// lanes of the pods do not form a shape pair.
func (s *Simulation) checkShapePair(first, second *vehicle) error {
	a, b := s.graph.lanes[first.Pod.LaneID], s.graph.lanes[second.Pod.LaneID]
	if _, ok := s.shapes.clearance(laneKey(a), laneKey(b)); !ok {
		return nil
	}
	pods := [2]lateralPod{s.newLateralPod(first, a), s.newLateralPod(second, b)}
	if oracleDisjoint(pods[0], pods[1]) || s.oraclePlanesSeparated(pods[0], pods[1]) {
		return nil
	}
	var text strings.Builder
	fmt.Fprintf(&text, "tick %d: the bodies of a shape pair of lanes %s and %s are not disjoint", s.tick, first.Pod.LaneID, second.Pod.LaneID)
	for _, pod := range pods {
		p := oraclePodText(&text, pod.v)
		fmt.Fprintf(&text, " at %.3f m, body", p.LaneDistance)
		pod.writeBody(&text)
	}
	return errors.New(text.String())
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
	pods := [2]lateralPod{s.newLateralPod(first, a), s.newLateralPod(second, b)}
	extents := [2][2]float64{{startA, endA}, {startB, endB}}
	if oracleDisjoint(pods[0], pods[1]) || s.oraclePlanesSeparated(pods[0], pods[1]) {
		return nil
	}
	node := s.network.Lanes[a].From
	if s.network.Lanes[a].To == s.network.Lanes[b].To {
		node = s.network.Lanes[a].To
	}
	junction := resource{kind: junctionResource, id: node}
	var text strings.Builder
	fmt.Fprintf(&text, "tick %d: the bodies of a merge pair at node %s are not disjoint: junction owner %q", s.tick, node, s.owners[junction].String())
	for index, pod := range pods {
		p := oraclePodText(&text, pod.v)
		extent := extents[index]
		fmt.Fprintf(&text, " at %.3f m, extent [%.3f, %.3f], outside %v, holds the junction %v, body",
			p.LaneDistance, extent[0], extent[1], p.LaneDistance < extent[0] || p.LaneDistance > extent[1], s.holdsResource(pod.v, junction))
		pod.writeBody(&text)
	}
	return errors.New(text.String())
}

// oraclePodText writes the ID, the class, and the lane of pod v to the
// text of an oracle error, and returns the pod.
func oraclePodText(text *strings.Builder, v *vehicle) Pod {
	p := v.Pod
	if p.Class == "" {
		p.Class = LegacyClass
	}
	fmt.Fprintf(text, "; pod %s (%s) on lane %s", p.ID, p.Class, p.LaneID)
	return p
}

// oracleBody is a grown body of the oracle about the reference of a pod.
type oracleBody struct {
	rear, front, halfWidth float64
}

// oracleClassBody returns the grown body of a class: the large body for
// Express and Group, and the small body for each other class.
func oracleClassBody(class VehicleClass) oracleBody {
	body := oracleBody{rear: oracleSmallRear, front: oracleSmallFront, halfWidth: oracleSmallHalfWidth}
	if class == ExpressClass || class == GroupClass {
		body = oracleBody{rear: oracleLargeRear, front: oracleLargeFront, halfWidth: oracleLargeHalfWidth}
	}
	grow := oracleMargin / 2
	return oracleBody{rear: body.rear + grow, front: body.front + grow, halfWidth: body.halfWidth + grow}
}

// radius returns the largest distance from the reference to a point of the
// body: the distance to a corner at the larger end.
func (b oracleBody) radius() float64 {
	return math.Hypot(max(b.rear, b.front), b.halfWidth)
}

// corners returns the corners of the body at a pose, with each face moved
// out by pad, in order around the body: rear right, front right, front
// left, and rear left.
func (b oracleBody) corners(x, y, heading, pad float64) [4]Point {
	s, c := math.Sincos(heading)
	at := func(along, side float64) Point {
		return Point{X: x + along*c - side*s, Y: y + along*s + side*c}
	}
	rear, front, half := -b.rear-pad, b.front+pad, b.halfWidth+pad
	return [4]Point{at(rear, -half), at(front, -half), at(front, half), at(rear, half)}
}

// pad returns the pad of the body at the middle heading of a pose that
// holds the body at each heading of the pose. A point of the body at a
// heading within |turn|/2 of the middle heading is within r*|turn|/2 of the
// same point at the middle heading, where r is the radius of the body. A
// pose with no turn has no pad.
func (b oracleBody) pad(pose oraclePose) float64 {
	return b.radius() * math.Abs(pose.turn) / 2
}

// enclosure returns the body at the middle heading of a pose, with the pad
// of the pose. It holds the body at each heading of the pose.
func (b oracleBody) enclosure(pose oraclePose) [4]Point {
	return b.corners(pose.x, pose.y, pose.heading+pose.turn/2, b.pad(pose))
}

// middle returns the body at the middle heading of a pose, with no pad.
func (b oracleBody) middle(pose oraclePose) [4]Point {
	return b.corners(pose.x, pose.y, pose.heading+pose.turn/2, 0)
}

// oraclePose is a pose of the body of a pod: its reference at (x, y), and
// its heading. A turn that is not zero rotates the body in place from the
// heading by turn radians.
type oraclePose struct {
	x, y, heading, turn float64
}

// halves returns the two halves of the turn of a pose.
func (pose oraclePose) halves() [2]oraclePose {
	half := pose.turn / 2
	return [2]oraclePose{
		{x: pose.x, y: pose.y, heading: pose.heading, turn: half},
		{x: pose.x, y: pose.y, heading: pose.heading + half, turn: half},
	}
}

// lateralPod is a pod of the oracle with its lane, its body, and its poses
// on the lane.
type lateralPod struct {
	v     *vehicle
	lane  int
	body  oracleBody
	poses []oraclePose
}

// newLateralPod returns the oracle data of pod v on a lane.
func (s *Simulation) newLateralPod(v *vehicle, lane int) lateralPod {
	return lateralPod{v: v, lane: lane, body: oracleClassBody(v.Pod.Class), poses: s.podPoses(v.Pod.Position, lane, v.Pod.LaneDistance)}
}

// writeBody writes the size of the body and its poses to the text of an
// oracle error.
func (pod lateralPod) writeBody(text *strings.Builder) {
	fmt.Fprintf(text, " %.3f by %.3f m", pod.body.rear+pod.body.front, 2*pod.body.halfWidth)
	for _, pose := range pod.poses {
		fmt.Fprintf(text, ", pose (%.3f, %.3f) heading %.4f rad", pose.x, pose.y, pose.heading)
		if pose.turn != 0 {
			fmt.Fprintf(text, " turning %.4f rad", pose.turn)
		}
	}
}

// oracleDisjoint reports whether the body of one pod at each of its poses
// and the body of the other pod at each of its poses are disjoint.
func oracleDisjoint(first, second lateralPod) bool {
	for _, a := range first.poses {
		for _, b := range second.poses {
			if !oraclePosesDisjoint(first.body, a, second.body, b) {
				return false
			}
		}
	}
	return true
}

// oraclePosesDisjoint reports whether body a at each heading of pose
// poseA and body b at each heading of pose poseB are disjoint. When the
// enclosures of the two poses are disjoint, the bodies are. When the
// bodies at the middle headings overlap, the bodies overlap. Otherwise it
// halves the turn of the pose with the larger pad and tests each half.
// When both pads are below oraclePadLimit and no decision is made, it
// reports an overlap.
func oraclePosesDisjoint(a oracleBody, poseA oraclePose, b oracleBody, poseB oraclePose) bool {
	if oracleSeparated(a.enclosure(poseA), b.enclosure(poseB)) {
		return true
	}
	if !oracleSeparated(a.middle(poseA), b.middle(poseB)) {
		return false
	}
	padA, padB := a.pad(poseA), b.pad(poseB)
	if max(padA, padB) < oraclePadLimit {
		return false
	}
	if padA >= padB {
		halves := poseA.halves()
		return oraclePosesDisjoint(a, halves[0], b, poseB) && oraclePosesDisjoint(a, halves[1], b, poseB)
	}
	halves := poseB.halves()
	return oraclePosesDisjoint(a, poseA, b, halves[0]) && oraclePosesDisjoint(a, poseA, b, halves[1])
}

// oracleSeparated reports whether two rectangles are disjoint by the
// separating axis test: the projections of their corners on the normal of
// an edge of one of them have a gap of more than oracleGap.
func oracleSeparated(a, b [4]Point) bool {
	for _, corners := range [][4]Point{a, b} {
		for i := range 2 {
			edge := Point{X: corners[i+1].X - corners[i].X, Y: corners[i+1].Y - corners[i].Y}
			length := math.Hypot(edge.X, edge.Y)
			if length == 0 {
				continue
			}
			axis := Point{X: -edge.Y / length, Y: edge.X / length}
			lowA, highA := oracleProjection(a, axis)
			lowB, highB := oracleProjection(b, axis)
			if highA < lowB-oracleGap || highB < lowA-oracleGap {
				return true
			}
		}
	}
	return false
}

// oracleProjection returns the least and the greatest projection of the
// corners of a rectangle on an axis.
func oracleProjection(corners [4]Point, axis Point) (low, high float64) {
	low, high = math.Inf(1), math.Inf(-1)
	for _, corner := range corners {
		projection := corner.X*axis.X + corner.Y*axis.Y
		low, high = min(low, projection), max(high, projection)
	}
	return low, high
}

// oraclePlanesSeparated reports whether the bodies of two pods are on
// different planes, from the separation groups of the network: the lanes
// of the pods share no node, and the planes of one pod and the planes of
// the other have no plane in common (see oraclePlanes). The empty group is
// not a separate plane, so a pod with it is not separated from any pod.
func (s *Simulation) oraclePlanesSeparated(first, second lateralPod) bool {
	a, b := s.network.Lanes[first.lane], s.network.Lanes[second.lane]
	if a.From == b.From || a.From == b.To || a.To == b.From || a.To == b.To {
		return false
	}
	planesA, okA := s.oraclePlanes(first)
	planesB, okB := s.oraclePlanes(second)
	if !okA || !okB {
		return false
	}
	for plane := range planesA {
		if planesB[plane] {
			return false
		}
	}
	return true
}

// oraclePlanes returns the planes of a pod. ok is false when a plane is
// the empty group.
//
// The oracle checks the bodies of the pods at their poses at each tick, so
// a pod is on the planes that its body touches: the group of its lane, and
// the groups of the lanes and the berths at each node within the radius of
// its body. The radius is the distance from the reference to the farthest
// corner of the grown body of its class, at the larger end, and it is
// measured from the position of the pod, so it holds the body at each
// heading, the join turns at a lane end included. On purpose, the oracle
// does not copy the envelope reach of the certification: that reach is a
// conservative bound that also covers motion and reservations, such as the
// retained tail and the unreleased origin berth, and not the body at one
// pose.
func (s *Simulation) oraclePlanes(pod lateralPod) (planes map[string]bool, ok bool) {
	planes = map[string]bool{s.network.Lanes[pod.lane].SeparationGroup: true}
	reach := pod.body.radius()
	for _, node := range s.network.Nodes {
		if pointDistance(node.Position, pod.v.Pod.Position) > reach {
			continue
		}
		for _, other := range s.network.Lanes {
			if other.From == node.ID || other.To == node.ID {
				planes[other.SeparationGroup] = true
			}
		}
		for _, station := range s.network.Stations {
			for _, berth := range station.Berths {
				if berth.Node == node.ID {
					planes[berth.SeparationGroup] = true
				}
			}
		}
	}
	return planes, !planes[""]
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
// along a lane. Inside a segment, the body has the heading
// of the segment. At an inner point of the polyline, the body turns from
// one segment to the next. At an end of the lane, the body also turns to
// or from each lane that joins there, as mergeSteps does. A lane with no
// length gives a full turn.
func (s *Simulation) podPoses(at Point, lane int, distance float64) []oraclePose {
	pose := func(heading float64) oraclePose {
		return oraclePose{x: at.X, y: at.Y, heading: heading}
	}
	// rotation turns the body at the point from one heading to another by
	// the smaller angle.
	rotation := func(from, to float64) oraclePose {
		return oraclePose{x: at.X, y: at.Y, heading: from, turn: math.Remainder(to-from, 2*math.Pi)}
	}
	turn := oraclePose{x: at.X, y: at.Y, turn: 2 * math.Pi}
	this := s.network.Lanes[lane]
	points := s.network.Polyline(this)
	first, last, ok := polylineEndHeadings(points)
	if !ok {
		return []oraclePose{turn}
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
	steps := []oraclePose{pose(headings[0])}
	for k := 1; k < len(headings); k++ {
		steps = append(steps, rotation(headings[k-1], headings[k]))
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
			steps = append(steps, rotation(otherLast, first))
		case distance >= length-mergeOracleNear && other.From == this.To:
			steps = append(steps, rotation(last, otherFirst))
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
