package sim

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"testing"
)

// lateralPairNetwork returns two straight lanes of length meters that meet
// at node "n" at the given angle in degrees. With converging, lane "a" runs
// along the X axis into n, lane "b" comes in at the angle, and lane "c"
// leaves n along the X axis. Otherwise "a" and "b" leave n, and "c" comes
// into n along the X axis.
func lateralPairNetwork(degrees, length float64, converging bool) Network {
	radians := degrees * math.Pi / 180
	sign := 1.0
	if converging {
		sign = -1
	}
	network := Network{
		Nodes: []Node{
			{ID: "n"},
			{ID: "a-end", Position: Point{X: sign * length}},
			{ID: "b-end", Position: Point{X: sign * length * math.Cos(radians), Y: sign * length * math.Sin(radians)}},
			{ID: "c-end", Position: Point{X: -sign * length}},
		},
		LateralMerges: true,
	}
	if converging {
		network.Lanes = []Lane{
			{ID: "a", From: "a-end", To: "n", SpeedLimit: 14},
			{ID: "b", From: "b-end", To: "n", SpeedLimit: 14},
			{ID: "c", From: "n", To: "c-end", SpeedLimit: 14},
		}
	} else {
		network.Lanes = []Lane{
			{ID: "a", From: "n", To: "a-end", SpeedLimit: 14},
			{ID: "b", From: "n", To: "b-end", SpeedLimit: 14},
			{ID: "c", From: "c-end", To: "n", SpeedLimit: 14},
		}
	}
	return network
}

// conflictWith returns the first extent of a lane in a table. A lane has
// one extent for each other lane at each node, in forEachJunctionPair
// order, so the test networks name the lanes such that the first extent is
// the one against the wanted lane.
func conflictWith(t *testing.T, conflicts map[string][]laneConflict, lane string) laneConflict {
	t.Helper()
	if len(conflicts[lane]) == 0 {
		t.Fatalf("lane %s has no extent", lane)
	}
	return conflicts[lane][0]
}

// TestLateralMergeStraightExtents checks the extent of two straight lanes
// of a lateral merge pair against the closed form of the body range. The
// grown body of a pod at distance s before the node, on a lane at angle a
// to the other lane, meets the strip that the grown bodies of the other
// lane sweep when s is at most front + halfWidth*cot(a/2). After the node,
// the rear takes the place of the front. The extent also holds the node
// range and the sample pad, and it is never longer than the isotropic
// extent of the same pair.
func TestLateralMergeStraightExtents(t *testing.T) {
	t.Parallel()
	const length = 200.0
	grow := lateralClearance(0)/2 - sweepHalfWidth
	front, rear, halfWidth := sweepFront+grow, lateralRear+grow, sweepHalfWidth+grow
	for _, degrees := range []float64{3, 6, 10, 15, 21, 30, 60, 90} {
		for _, converging := range []bool{true, false} {
			t.Run(fmt.Sprintf("%v degrees converging %v", degrees, converging), func(t *testing.T) {
				t.Parallel()
				network := lateralPairNetwork(degrees, length, converging)
				on := junctionConflictsOf(network)
				network.LateralMerges = false
				off := junctionConflictsOf(network)
				cot := 1 / math.Tan(degrees*math.Pi/360)
				reach := rear + halfWidth*cot
				if converging {
					reach = front + halfWidth*cot
				}
				// Each lane runs from or to the node, so the body range has
				// the same length on both lanes.
				zone := max(reach, Clearance) + conflictSampleStep
				for index, lane := range []string{"a", "b"} {
					got, isotropic := conflictWith(t, on, lane), conflictWith(t, off, lane)
					laneLength := network.Length(network.Lanes[index])
					if got.junction != "n" {
						t.Fatalf("lane %s extent at %s, want n", lane, got.junction)
					}
					// The sweep halves a part to conflictSampleStep, so the
					// range can be longer by up to that step.
					near, far := got.end, laneLength-got.start
					if !converging {
						near, far = laneLength-got.start, got.end
					}
					if near != laneLength || far < zone-1e-9 || far > zone+conflictSampleStep+1e-9 {
						t.Errorf("lane %s extent [%v, %v], want a zone of %v to %v meters at the node", lane, got.start, got.end, zone, zone+conflictSampleStep)
					}
					if got.start < isotropic.start-1e-9 || got.end > isotropic.end+1e-9 {
						t.Errorf("lane %s extent [%v, %v] is not inside the isotropic extent [%v, %v]", lane, got.start, got.end, isotropic.start, isotropic.end)
					}
				}
			})
		}
	}
}

// mergeShape is a body of the lateral merge reference before the growth by
// half the margin: rear behind the reference, front ahead, and halfWidth
// to each side. It shares no code with the sweep.
type mergeShape struct {
	rear, front, halfWidth float64
}

// The bodies of section 3.2 of docs/design/shape-clearance.md: the small
// body, and the large body of Express and Group, a centered 6 m by 2.5 m
// body with a margin of 0.5 m at each end and 0.25 m at each side.
var (
	mergeSmallShape = mergeShape{rear: lateralRear, front: sweepFront, halfWidth: sweepHalfWidth}
	mergeLargeShape = mergeShape{rear: 3.5, front: 3.5, halfWidth: 1.5}
)

// mergeLaneShape returns the body of a lane in the reference: the small
// body for a network without ShapeClearance, and otherwise the union of
// the bodies of the classes that the lane admits.
func mergeLaneShape(network Network, lane Lane) mergeShape {
	if !network.ShapeClearance {
		return mergeSmallShape
	}
	var union mergeShape
	add := func(body mergeShape) {
		union = mergeShape{rear: max(union.rear, body.rear), front: max(union.front, body.front), halfWidth: max(union.halfWidth, body.halfWidth)}
	}
	if lane.VehicleClasses.Allows(string(LegacyClass)) || lane.VehicleClasses.Allows(string(CompactClass)) {
		add(mergeSmallShape)
	}
	if lane.VehicleClasses.Allows(string(GroupClass)) || lane.VehicleClasses.Allows(string(ExpressClass)) {
		add(mergeLargeShape)
	}
	return union
}

// grown returns the body grown by half the margin and by extra meters more
// on each side, as four corners, at a heading at a position.
func (b mergeShape) grown(position Point, heading, extra float64) [4]Point {
	grow := 0.5 + extra
	return mergeRectangle(position, heading, [2]float64{-b.rear - grow, b.front + grow}, [2]float64{-b.halfWidth - grow, b.halfWidth + grow})
}

// cornerReach returns the largest distance of a corner of the body, grown
// by half the margin and by extra, from its reference.
func (b mergeShape) cornerReach(extra float64) float64 {
	grow := 0.5 + extra
	return math.Hypot(max(b.front, b.rear)+grow, b.halfWidth+grow)
}

// mergeBody is the grown body of a small pod as four corners: lateralRear
// plus half the margin behind the reference, sweepFront plus half the
// margin ahead, and sweepHalfWidth plus half the margin to each side, at
// the heading of the lane at the reference. It shares no code with the
// sweep.
func mergeBody(position Point, heading float64) [4]Point {
	return mergeSmallShape.grown(position, heading, 0)
}

// mergeRectangle returns the four corners of a rectangle on the axes of a
// heading at a point: along is its range along the heading, and across its
// range to the left.
func mergeRectangle(at Point, heading float64, along, across [2]float64) [4]Point {
	s, c := math.Sincos(heading)
	var corners [4]Point
	for index, corner := range [4][2]float64{{along[0], across[0]}, {along[1], across[0]}, {along[1], across[1]}, {along[0], across[1]}} {
		corners[index] = Point{X: at.X + corner[0]*c - corner[1]*s, Y: at.Y + corner[0]*s + corner[1]*c}
	}
	return corners
}

// turnRectangle returns the rectangle that holds the grown body over a
// rotation in place by turn from heading, grown by extra on each side. It
// is on the axes of the middle heading, and its sides are the least and the
// greatest projections of the corners of the body over the rotation, as the
// sweep encloses a rotation.
func (b mergeShape) turnRectangle(at Point, heading, turn, extra float64) [4]Point {
	half := math.Abs(turn) / 2
	along, across := [2]float64{math.Inf(1), math.Inf(-1)}, [2]float64{math.Inf(1), math.Inf(-1)}
	grow := 0.5
	for _, corner := range [4][2]float64{{-b.rear - grow, -b.halfWidth - grow}, {b.front + grow, -b.halfWidth - grow}, {b.front + grow, b.halfWidth + grow}, {-b.rear - grow, b.halfWidth + grow}} {
		// At an angle t from the middle heading, the corner (x, y) is at
		// x cos t - y sin t along and x sin t + y cos t across.
		low, high := mergeTrigRange(corner[0], -corner[1], half)
		along = [2]float64{min(along[0], low), max(along[1], high)}
		low, high = mergeTrigRange(corner[1], corner[0], half)
		across = [2]float64{min(across[0], low), max(across[1], high)}
	}
	return mergeRectangle(at, heading+turn/2, [2]float64{along[0] - extra, along[1] + extra}, [2]float64{across[0] - extra, across[1] + extra})
}

// mergeTrigRange returns the least and the greatest value of
// a cos t + b sin t for t from -half to half. The value is
// r cos(t - p), with r = hypot(a, b) and p = atan2(b, a), so the range is
// that of the two ends, and r where t can be p, and -r where t can be
// p + pi.
func mergeTrigRange(a, b, half float64) (float64, float64) {
	r := math.Hypot(a, b)
	if half >= math.Pi {
		return -r, r
	}
	first, second := a*math.Cos(half)-b*math.Sin(half), a*math.Cos(half)+b*math.Sin(half)
	low, high := min(first, second), max(first, second)
	p := math.Atan2(b, a)
	if math.Abs(p) <= half {
		high = r
	}
	if math.Abs(math.Remainder(p+math.Pi, 2*math.Pi)) <= half {
		low = -r
	}
	return low, high
}

// mergeBodiesDisjoint reports whether two rectangles do not overlap, by
// the separating axis test on the edge normals of both.
func mergeBodiesDisjoint(first, second [4]Point) bool {
	for _, body := range [2][4]Point{first, second} {
		for index := range 2 {
			axis := Point{X: body[index+1].Y - body[index].Y, Y: body[index].X - body[index+1].X}
			project := func(corners [4]Point) (float64, float64) {
				low, high := math.Inf(1), math.Inf(-1)
				for _, corner := range corners {
					value := corner.X*axis.X + corner.Y*axis.Y
					low, high = min(low, value), max(high, value)
				}
				return low, high
			}
			firstLow, firstHigh := project(first)
			secondLow, secondHigh := project(second)
			if firstHigh <= secondLow || secondHigh <= firstLow {
				return true
			}
		}
	}
	return false
}

// laneHeading returns the heading of the segment of a polyline at a
// distance along it. A distance at a joint takes the segment before it.
func laneHeading(points []Point, distance float64) float64 {
	heading := 0.0
	for i := 1; i < len(points); i++ {
		length := pointDistance(points[i-1], points[i])
		if length == 0 {
			continue
		}
		heading = math.Atan2(points[i].Y-points[i-1].Y, points[i].X-points[i-1].X)
		if distance <= length {
			break
		}
		distance -= length
	}
	return heading
}

// checkMergeBodies fails the test when the grown bodies of two pods on the
// lanes of a lateral merge pair overlap. It returns the smallest distance
// between two such pods, or +Inf.
func checkMergeBodies(t *testing.T, s *Simulation) float64 {
	t.Helper()
	smallest := math.Inf(1)
	for i := range s.vehicles {
		first := s.vehicles[i].Pod
		a, ok := s.graph.lanes[first.LaneID]
		if first.LaneID == "" || !ok {
			continue
		}
		for j := i + 1; j < len(s.vehicles); j++ {
			second := s.vehicles[j].Pod
			b, ok := s.graph.lanes[second.LaneID]
			if second.LaneID == "" || !ok || !s.merges.mergePartner(a, b) {
				continue
			}
			smallest = min(smallest, pointDistance(first.Position, second.Position))
			firstBody := mergeBody(first.Position, laneHeading(s.network.Polyline(s.network.Lanes[a]), first.LaneDistance))
			secondBody := mergeBody(second.Position, laneHeading(s.network.Polyline(s.network.Lanes[b]), second.LaneDistance))
			if !mergeBodiesDisjoint(firstBody, secondBody) {
				t.Fatalf("tick %d: the bodies of pods %s on %s at %.3f m and %s on %s at %.3f m overlap", s.tick, first.ID, first.LaneID, first.LaneDistance, second.ID, second.LaneID, second.LaneDistance)
			}
		}
	}
	return smallest
}

// TestLateralMergeTwoStreams runs two stopped queues into a shallow merge
// with lateral merge pairs for 15 simulated minutes. At each tick, Check
// passes, and the grown bodies of the pods of the pair do not overlap. At
// 6.5 degrees, the body front is the difference between one and two
// junction cells on each feed lane.
func TestLateralMergeTwoStreams(t *testing.T) {
	t.Parallel()
	skipLong(t)
	for _, degrees := range []float64{6, 6.5} {
		t.Run(fmt.Sprintf("%v degrees", degrees), func(t *testing.T) {
			t.Parallel()
			got := runMergeCorridor(t, corridorCase{
				name: "two streams, shallow merge", side: true, sideDegrees: degrees, streams: []string{"main", "side"},
				queueHead: corridorFeedLength - 100, lateral: true, whole: true,
			})
			t.Logf("headway %.3f s, smallest lateral pair gap %.3f m", got.headway, got.lateralGap)
			if !(got.lateralGap < Clearance) {
				t.Errorf("smallest lateral pair gap %.3f m, want less than %v m, so that the run tests the rule", got.lateralGap, Clearance)
			}
		})
	}
}

// referencePose is a reference position on a lane, its distance along the
// lane, and a heading of the lane at it.
type referencePose struct {
	position Point
	distance float64
	heading  float64
}

// referencePoses samples a polyline every step meters on each segment, at
// the heading of the segment, and samples the turn at each inner point
// in eight parts.
func referencePoses(points []Point, step float64) []referencePose {
	var poses []referencePose
	distance, previous, started := 0.0, 0.0, false
	for i := 1; i < len(points); i++ {
		a, b := points[i-1], points[i]
		length := pointDistance(a, b)
		if length == 0 {
			continue
		}
		heading := math.Atan2(b.Y-a.Y, b.X-a.X)
		if started {
			turn := math.Remainder(heading-previous, 2*math.Pi)
			for part := range 9 {
				poses = append(poses, referencePose{position: a, distance: distance, heading: previous + turn*float64(part)/8})
			}
		}
		for offset := 0.0; offset < length; offset += step {
			poses = append(poses, referencePose{position: lanePoint(a, b, length, offset), distance: distance + offset, heading: heading})
		}
		distance += length
		poses = append(poses, referencePose{position: b, distance: distance, heading: heading})
		previous, started = heading, true
	}
	return poses
}

// referenceBodyRange returns the smallest and the largest distance of the
// sampled poses of a lane whose grown body overlaps the grown body at a
// sampled pose of the other lane, or +Inf and -Inf.
func referenceBodyRange(lane, other []referencePose) (float64, float64) {
	start, end := math.Inf(1), math.Inf(-1)
	// Two grown bodies can overlap only when their references are closer
	// than twice the largest distance of a corner from the reference.
	reach := 2 * math.Hypot(sweepFront+0.5, sweepHalfWidth+0.5)
	for _, pose := range lane {
		body := mergeBody(pose.position, pose.heading)
		for _, near := range other {
			if pointDistance(pose.position, near.position) > reach {
				continue
			}
			if !mergeBodiesDisjoint(body, mergeBody(near.position, near.heading)) {
				start, end = min(start, pose.distance), max(end, pose.distance)
				break
			}
		}
	}
	return start, end
}

// rampNetwork returns a straight main lane and a quadratic ramp that meet
// at node "n", as the ramps of the London junctions: the ramp leaves or
// joins the main lane at a shallow angle. control is the control point of
// the ramp and start the far end of the ramp, both for a converging ramp.
// A diverging network mirrors the X axis and reverses each lane. Lane "c"
// continues the main lane at the node.
func rampNetwork(control, start Point, mainControl *Point, converging bool) Network {
	mirror := func(p Point) Point {
		if converging {
			return p
		}
		return Point{X: -p.X, Y: p.Y}
	}
	network := Network{
		Nodes: []Node{
			{ID: "n"},
			{ID: "main-end", Position: mirror(Point{X: -300})},
			{ID: "ramp-end", Position: mirror(start)},
			{ID: "c-end", Position: mirror(Point{X: 300})},
		},
		LateralMerges: true,
	}
	main := Lane{ID: "a", From: "main-end", To: "n", SpeedLimit: 14}
	ramp := Lane{ID: "b", From: "ramp-end", To: "n", SpeedLimit: 10, Control: new(mirror(control))}
	if mainControl != nil {
		main.Control = new(mirror(*mainControl))
	}
	through := Lane{ID: "c", From: "n", To: "c-end", SpeedLimit: 14}
	if !converging {
		main.From, main.To = main.To, main.From
		ramp.From, ramp.To = ramp.To, ramp.From
		through.From, through.To = through.To, through.From
	}
	network.Lanes = []Lane{main, ramp, through}
	return network
}

// TestLateralMergeRampsMatchBodyReference compares the extents of quadratic
// ramps with a brute-force body reference. The reference samples both
// lanes every 0.05 m and each turn at a joint, and it tests the grown
// bodies with the separating axis test. The extent must hold the reference
// body range with the sample pad, and the node range. It can be longer
// only by the part length of the sweep and the sample step of the
// reference.
func TestLateralMergeRampsMatchBodyReference(t *testing.T) {
	t.Parallel()
	skipLong(t)
	const step = 0.05
	for index, ramp := range []struct {
		control, start Point
		main           *Point
	}{
		{control: Point{X: -80}, start: Point{X: -200, Y: -60}},
		{control: Point{X: -40}, start: Point{X: -150, Y: -25}},
		{control: Point{X: -120, Y: -3}, start: Point{X: -260, Y: -45}},
		{control: Point{X: -60, Y: 4}, start: Point{X: -220, Y: -90}, main: &Point{X: -150, Y: 20}},
		{control: Point{X: -100, Y: 30}, start: Point{X: -180, Y: 90}, main: &Point{X: -120, Y: -10}},
	} {
		for _, converging := range []bool{true, false} {
			t.Run(fmt.Sprintf("ramp %d converging %v", index, converging), func(t *testing.T) {
				t.Parallel()
				network := rampNetwork(ramp.control, ramp.start, ramp.main, converging)
				conflicts := junctionConflictsOf(network)
				poses := map[string][]referencePose{}
				for _, lane := range network.Lanes[:2] {
					poses[lane.ID] = referencePoses(network.Polyline(lane), step)
				}
				for laneIndex, pair := range [][2]string{{"a", "b"}, {"b", "a"}} {
					length := network.Length(network.Lanes[laneIndex])
					start, end := referenceBodyRange(poses[pair[0]], poses[pair[1]])
					if converging {
						start, end = min(start, length-Clearance), length
					} else {
						start, end = 0, max(end, Clearance)
					}
					start, end = max(0, start-conflictSampleStep), min(length, end+conflictSampleStep)
					got := conflictWith(t, conflicts, pair[0])
					if got.start > start || got.end < end {
						t.Errorf("lane %s extent [%.4f, %.4f] does not hold the reference [%.4f, %.4f]", pair[0], got.start, got.end, start, end)
					}
					if slack := conflictSampleStep + 2*step; got.start < start-slack || got.end > end+slack {
						t.Errorf("lane %s extent [%.4f, %.4f] is more than %v m longer than the reference [%.4f, %.4f]", pair[0], got.start, got.end, slack, start, end)
					}
				}
			})
		}
	}
}

// TestLateralMergeExcludedPairsKeepExtent checks that each pair that is
// not a lateral merge pair keeps the isotropic extent with LateralMerges
// set: a through pair, a loop, two lanes with the same endpoints, a pair
// with a large lane, a lane with a geometry tail, and the lanes of a
// maneuver group and of a depot.
func TestLateralMergeExcludedPairsKeepExtent(t *testing.T) {
	t.Parallel()
	radians := 6 * math.Pi / 180
	shallow := Point{X: -200 * math.Cos(radians), Y: -200 * math.Sin(radians)}
	group, err := NewClassSet(string(GroupClass))
	if err != nil {
		t.Fatal(err)
	}
	nodes := []Node{{ID: "n"}, {ID: "a-end", Position: Point{X: -200}}, {ID: "b-end", Position: shallow}, {ID: "m", Position: Point{X: -150, Y: 10}}, {ID: "far", Position: Point{X: -200, Y: -200}}}
	a := Lane{ID: "a", From: "a-end", To: "n", SpeedLimit: 14}
	for _, test := range []struct {
		name  string
		lanes []Lane
		// tail names a lane that must have a geometry tail.
		tail string
	}{
		{name: "through pair", lanes: []Lane{a, {ID: "c", From: "n", To: "b-end", SpeedLimit: 14}}},
		{name: "loop", lanes: []Lane{a, {ID: "loop", From: "n", To: "n", SpeedLimit: 14, Control: &Point{X: -100, Y: -8}}}},
		{name: "same endpoints", lanes: []Lane{{ID: "s1", From: "m", To: "n", SpeedLimit: 14}, {ID: "s2", From: "m", To: "n", SpeedLimit: 14, Control: &Point{X: -75, Y: 9}}}},
		{name: "large lane", lanes: []Lane{a, {ID: "b", From: "b-end", To: "n", SpeedLimit: 14, VehicleClasses: group}}},
		{name: "tail", tail: "a", lanes: []Lane{a, {ID: "b", From: "b-end", To: "n", SpeedLimit: 14}, {ID: "g", From: "far", To: "a-end", SpeedLimit: 14, VehicleClasses: group}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			network := Network{Nodes: nodes, Lanes: test.lanes, LateralMerges: true}
			if test.tail != "" && indexGeometryTails(network)[test.tail] == (geometryTails{}) {
				t.Fatalf("lane %s has no geometry tail", test.tail)
			}
			checkSameExtents(t, network, nil)
		})
	}
	for name, network := range map[string]Network{"maneuver group": compactStationNetwork(6, 28.40), "depot": testDepotNetwork()} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			positions := indexNodePositions(network.Nodes)
			maneuvers, _ := newManeuverIndex(network, positions)
			depots, _ := newDepotIndex(network, positions)
			excluded := func(lane Lane) bool { return maneuvers.groupLane(lane.ID) || depots.depotLane(lane.ID) }
			// Some lane of the group or the depot meets clauses 1 and 2
			// with another lane, so that the test can fail.
			candidates := 0
			forEachJunctionPair(network, func(node string, lane, other Lane) {
				if excluded(lane) && lateralMergePair(node, lane, other) {
					candidates++
				}
			})
			if candidates == 0 {
				t.Fatal("no lane of the network meets clauses 1 and 2")
			}
			network.LateralMerges = true
			checkSameExtents(t, network, excluded)
		})
	}
}

// checkSameExtents fails the test when the extents of a lane differ with
// and without LateralMerges, or when the lane is in a lateral merge pair.
// With a nil filter, it checks each lane.
func checkSameExtents(t *testing.T, network Network, filter func(Lane) bool) {
	t.Helper()
	positions := indexNodePositions(network.Nodes)
	maneuvers, _ := newManeuverIndex(network, positions)
	depots, _ := newDepotIndex(network, positions)
	merges := newMergeLateral(network, indexGeometryTails(network), maneuvers, depots)
	for index, lane := range network.Lanes {
		if (filter == nil || filter(lane)) && len(merges.neighborLanes(index)) > 0 {
			t.Errorf("lane %s is in a lateral merge pair with lanes %v", lane.ID, merges.neighborLanes(index))
		}
	}
	on := junctionConflictsOf(network)
	network.LateralMerges = false
	off := junctionConflictsOf(network)
	checked := 0
	for _, lane := range network.Lanes {
		if filter != nil && !filter(lane) {
			continue
		}
		checked++
		if fmt.Sprint(on[lane.ID]) != fmt.Sprint(off[lane.ID]) {
			t.Errorf("lane %s extents %v, want the isotropic extents %v", lane.ID, on[lane.ID], off[lane.ID])
		}
	}
	if checked == 0 || len(off) == 0 {
		t.Fatal("the test checks no extent")
	}
}

// TestLateralMergeCheck checks the separation of a lateral merge pair:
// two pods on the lanes of a 6 degree merge pass at lateralClearance(0)
// and fail just below it. A through pair at the same distance fails, and so
// does the merge pair without LateralMerges.
func TestLateralMergeCheck(t *testing.T) {
	t.Parallel()
	radians := 6 * math.Pi / 180
	// The pod on the side lane is 20 m before the merge node, and the pod
	// on the main lane is the given distance from it.
	side := Point{X: -20 * math.Cos(radians), Y: -20 * math.Sin(radians)}
	mainAt := func(gap float64) Point {
		return Point{X: side.X + math.Sqrt(gap*gap-side.Y*side.Y), Y: 0}
	}
	clearance := lateralClearance(0)
	for _, test := range []struct {
		name                  string
		lateral               bool
		first, second         Point
		firstLane, secondLane string
		fail                  bool
	}{
		{name: "lateral pair at the clearance", lateral: true, first: mainAt(clearance), second: side, firstLane: "main", secondLane: "side"},
		{name: "lateral pair below the clearance", lateral: true, first: mainAt(3.09), second: side, firstLane: "main", secondLane: "side", fail: true},
		{name: "through pair at the clearance", lateral: true, first: Point{X: -clearance / 2}, second: Point{X: clearance / 2}, firstLane: "main", secondLane: "exit", fail: true},
		{name: "lateral pair without the field", first: mainAt(clearance), second: side, firstLane: "main", secondLane: "side", fail: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			network := mergeCorridor(true, 6)
			network.LateralMerges = test.lateral
			s, err := NewFleet(network, []Placement{{ID: "p1", StationID: "dest", BerthID: "dest-01"}, {ID: "p2", StationID: "dest", BerthID: "dest-02"}})
			if err != nil {
				t.Fatal(err)
			}
			for index, place := range []struct {
				lane     string
				position Point
			}{{test.firstLane, test.first}, {test.secondLane, test.second}} {
				pod := &s.vehicles[index].Pod
				pod.Activity, pod.BerthID, pod.StationID, pod.LaneID, pod.Position = Traveling, "", "", place.lane, place.position
			}
			if gap := pointDistance(test.first, test.second); math.Abs(gap-3.09) > 1e-9 && math.Abs(gap-clearance) > 1e-9 {
				t.Fatalf("the pods are %v m apart, want 3.09 m or %v m", gap, clearance)
			}
			_, err = s.SafetyObservation().Check()
			if failed := err != nil; failed != test.fail {
				t.Fatalf("Check error %v, want an error %v", err, test.fail)
			}
		})
	}
}

// TestLateralMergeDebrisBlocksPartner checks the debris footprint of a lane
// of a lateral merge pair (section 7.2 of the incident suspension
// contract). Debris on the main lane of a 6 degree merge, 100 m before the
// merge node, is about 10.5 m from the side lane, outside the junction
// zones. The footprint holds each cell of the side lane that comes closer
// than Clearance to the debris, and no other cell of the side lane.
func TestLateralMergeDebrisBlocksPartner(t *testing.T) {
	t.Parallel()
	network := mergeCorridor(true, 6)
	network.LateralMerges = true
	s, err := NewFleet(network, []Placement{{ID: "p1", StationID: "dest", BerthID: "dest-01"}})
	if err != nil {
		t.Fatal(err)
	}
	const from, to = corridorFeedLength - 100, corridorFeedLength - 97
	main, side := s.graph.lanes["main"], s.graph.lanes["side"]
	footprint := s.debrisFootprint(main, from, to)
	for _, r := range footprint {
		if r.kind == junctionResource {
			t.Fatalf("the footprint holds junction %s, so the test does not need the partner", r.id)
		}
	}
	debris := polylinePiece(network.Polyline(network.Lanes[main]), from, to)
	cells, length := s.laneCells["side"], s.graph.lengths[side]
	near := 0
	for cell := range cells.count() {
		piece := polylinePiece(network.Polyline(network.Lanes[side]), cellOffset(cell, cells.count(), length), cellOffset(cell+1, cells.count(), length))
		nearby := false
		for _, point := range piece {
			nearby = nearby || pointToPolylineDistance(point, debris) < Clearance
		}
		for _, point := range debris {
			nearby = nearby || pointToPolylineDistance(point, piece) < Clearance
		}
		for _, r := range cells.cell(cell) {
			if held := slices.Contains(footprint, r); held != nearby && r.kind == trackResource {
				t.Errorf("side cell %d resource %v in the footprint %v, want %v", cell, r, held, nearby)
			}
		}
		if nearby {
			near++
		}
	}
	if near == 0 {
		t.Fatal("no cell of the side lane is near the debris")
	}
}

// TestLateralMergeRestoresInPlace restores two traveling pods 100 m before
// a 6 degree merge node, one on each lane of the lateral merge pair, about
// 10.5 m apart. With LateralMerges, the physical restore keeps both in
// place. Without it, the pair needs Clearance, and the restore demotes a
// pod.
func TestLateralMergeRestoresInPlace(t *testing.T) {
	t.Parallel()
	for _, lateral := range []bool{true, false} {
		network := mergeCorridor(true, 6)
		network.LateralMerges = lateral
		laneIndex := make(map[string]int, len(network.Lanes))
		for index, lane := range network.Lanes {
			laneIndex[lane.ID] = index
		}
		state := SavedState{SharedRidePartyLimit: 1}
		var fleet []Placement
		for index, feed := range []string{"main", "side"} {
			id := fmt.Sprintf("p%d", index+1)
			fleet = append(fleet, Placement{ID: id, StationID: "dest", BerthID: fmt.Sprintf("dest-%02d", index+1)})
			state.RequestID++
			state.Boarded++
			distance := corridorFeedLength - 100
			state.Pods = append(state.Pods, SavedPod{
				ID: id, Activity: "traveling", Occupied: true, Origin: "origin-1", DestinationStation: "dest",
				Riders:    []SavedRequest{{SharingConsent: SharedConsent, Service: OnDemandService, ID: state.RequestID, From: "origin", To: "dest", PartySize: 1, PodID: id}},
				Boardings: []RiderBoarding{{BerthID: "origin-1"}},
				Stops:     []string{"dest"},
				Route:     []int{laneIndex[feed], laneIndex["exit"], laneIndex["approach"]}, LaneID: feed, LaneDistance: distance, Distance: distance,
			})
		}
		_, result, err := RestoreState(RestoreStateInput{Network: network, Fleet: fleet, State: state})
		if err != nil {
			t.Fatal(err)
		}
		if inPlace := result.Tier == RestorePhysical && len(result.Demoted) == 0; inPlace != lateral {
			t.Errorf("LateralMerges %v: restore %+v, want in place %v", lateral, result, lateral)
		}
	}
}

// farEndPoses returns the reference poses of a lane at its far end from a
// node: the start when the lane ends at the node, or the end otherwise. The
// poses have the heading of the lane, and the headings of the turn in
// eight parts to each lane joined at that end.
func farEndPoses(network Network, lane Lane, node string) []referencePose {
	points := network.Polyline(lane)
	first, last, ok := lateralBuilder{}.headings(points)
	if !ok {
		return nil
	}
	at, heading, distance := points[len(points)-1], last, network.Length(lane)
	if lane.To == node {
		at, heading, distance = points[0], first, 0
	}
	poses := []referencePose{{position: at, distance: distance, heading: heading}}
	for _, joined := range network.Lanes {
		var other float64
		switch {
		case lane.To == node && joined.To == lane.From:
			_, other, ok = lateralBuilder{}.headings(network.Polyline(joined))
		case lane.To != node && joined.From == lane.To:
			other, _, ok = lateralBuilder{}.headings(network.Polyline(joined))
		default:
			continue
		}
		if !ok {
			continue
		}
		turn := math.Remainder(other-heading, 2*math.Pi)
		for part := range 9 {
			poses = append(poses, referencePose{position: at, distance: distance, heading: heading + turn*float64(part)/8})
		}
	}
	return poses
}

// mergePose is a reference pose of the lateral merge reference: a position,
// its distance along the lane, a heading, and turn, the angle of the
// rotation that the pose samples, or 0 for a pose on a segment.
type mergePose struct {
	position Point
	distance float64
	heading  float64
	turn     float64
}

// mergeTurnPoses samples a rotation by turn from heading at a point, so
// that a corner of a body whose corners are at most reach from its
// reference moves at most step from one pose to the next. The number of
// parts is even, so the middle heading is a pose.
func mergeTurnPoses(at Point, distance, heading, turn, step, reach float64) []mergePose {
	parts := max(2, 2*int(math.Ceil(math.Abs(turn)*reach/(2*step))))
	poses := make([]mergePose, 0, parts+1)
	for part := range parts + 1 {
		poses = append(poses, mergePose{position: at, distance: distance, heading: heading + turn*float64(part)/float64(parts), turn: turn})
	}
	return poses
}

// mergeHeadings returns the heading of the first and of the last segment
// with length of a polyline. ok is false for a polyline with no length.
func mergeHeadings(points []Point) (first, last float64, ok bool) {
	for i := 1; i < len(points); i++ {
		if pointDistance(points[i-1], points[i]) == 0 {
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

// mergeLanePoses returns reference poses of a polyline. On each segment
// with length, it samples the offsets in the range that span gives that
// are multiples of step, and the end of the segment when the range
// reaches it, at the heading of the segment. At each inner point where
// turn is true, it samples the turn from the segment before to the segment
// after with mergeTurnPoses, turnStep, and reach.
func mergeLanePoses(points []Point, step, turnStep, reach float64, span func(a, b Point, length, distance float64) (float64, float64), turn func(at Point, distance float64) bool) []mergePose {
	var poses []mergePose
	distance, previous, started := 0.0, 0.0, false
	for i := 1; i < len(points); i++ {
		a, b := points[i-1], points[i]
		length := pointDistance(a, b)
		if length == 0 {
			continue
		}
		heading := math.Atan2(b.Y-a.Y, b.X-a.X)
		if started && turn(a, distance) {
			poses = append(poses, mergeTurnPoses(a, distance, previous, math.Remainder(heading-previous, 2*math.Pi), turnStep, reach)...)
		}
		lo, hi := span(a, b, length, distance)
		for k := math.Ceil(max(0, lo) / step); k*step < length && k*step <= hi; k++ {
			poses = append(poses, mergePose{position: lanePoint(a, b, length, k*step), distance: distance + k*step, heading: heading})
		}
		if hi >= length && lo <= length {
			poses = append(poses, mergePose{position: b, distance: distance + length, heading: heading})
		}
		distance += length
		previous, started = heading, true
	}
	return poses
}

// mergeJoins holds the lanes that end at each node and the lanes that
// start at each node, in lane order.
type mergeJoins struct {
	incoming, outgoing map[string][]Lane
}

func newMergeJoins(network Network) mergeJoins {
	joins := mergeJoins{incoming: make(map[string][]Lane), outgoing: make(map[string][]Lane)}
	for _, lane := range network.Lanes {
		joins.incoming[lane.To] = append(joins.incoming[lane.To], lane)
		joins.outgoing[lane.From] = append(joins.outgoing[lane.From], lane)
	}
	return joins
}

// mergeTurn is a rotation in place by turn from heading, at a point at a
// distance along a lane.
type mergeTurn struct {
	at                      Point
	distance, heading, turn float64
}

// turns returns the turns at the start of a lane, from each lane that ends
// there, or at its end, to each lane that starts there. A joined lane with
// no length gives a full turn.
func (j mergeJoins) turns(network Network, lane Lane, atStart bool) []mergeTurn {
	points := network.Polyline(lane)
	first, last, _ := mergeHeadings(points)
	at, distance, joined := points[len(points)-1], network.Length(lane), j.outgoing[lane.To]
	if atStart {
		at, distance, joined = points[0], 0, j.incoming[lane.From]
	}
	var turns []mergeTurn
	for _, other := range joined {
		otherFirst, otherLast, ok := mergeHeadings(network.Polyline(other))
		switch {
		case !ok:
			turns = append(turns, mergeTurn{at: at, distance: distance, turn: 2 * math.Pi})
		case atStart:
			turns = append(turns, mergeTurn{at: at, distance: distance, heading: otherLast, turn: math.Remainder(first-otherLast, 2*math.Pi)})
		default:
			turns = append(turns, mergeTurn{at: at, distance: distance, heading: last, turn: math.Remainder(otherFirst-last, 2*math.Pi)})
		}
	}
	return turns
}

// poses samples the turns at the start or at the end of a lane with
// mergeTurnPoses, for a body whose corners are at most reach from its
// reference.
func (j mergeJoins) poses(network Network, lane Lane, atStart bool, step, reach float64) []mergePose {
	var poses []mergePose
	for _, turn := range j.turns(network, lane, atStart) {
		poses = append(poses, mergeTurnPoses(turn.at, turn.distance, turn.heading, turn.turn, step, reach)...)
	}
	return poses
}

// mergeBox is an axis-aligned box.
type mergeBox struct{ minX, minY, maxX, maxY float64 }

// mergeBoxOf returns the box of a polyline, grown by margin on each side.
func mergeBoxOf(points []Point, margin float64) mergeBox {
	box := mergeBox{minX: math.Inf(1), minY: math.Inf(1), maxX: math.Inf(-1), maxY: math.Inf(-1)}
	for _, p := range points {
		box = mergeBox{minX: min(box.minX, p.X), minY: min(box.minY, p.Y), maxX: max(box.maxX, p.X), maxY: max(box.maxY, p.Y)}
	}
	return mergeBox{minX: box.minX - margin, minY: box.minY - margin, maxX: box.maxX + margin, maxY: box.maxY + margin}
}

func (box mergeBox) holds(p Point) bool {
	return p.X >= box.minX && p.X <= box.maxX && p.Y >= box.minY && p.Y <= box.maxY
}

// clip returns the range of offsets of the segment from a to b that is in
// the box, or an empty range.
func (box mergeBox) clip(a, b Point, length float64) (float64, float64) {
	lo, hi := 0.0, 1.0
	for _, edge := range [4][2]float64{{a.X - b.X, a.X - box.minX}, {b.X - a.X, box.maxX - a.X}, {a.Y - b.Y, a.Y - box.minY}, {b.Y - a.Y, box.maxY - a.Y}} {
		p, q := edge[0], edge[1]
		switch {
		case p == 0 && q < 0:
			return 1, 0
		case p < 0:
			lo = max(lo, q/p)
		case p > 0:
			hi = min(hi, q/p)
		}
	}
	return lo * length, hi * length
}

// mergeObstacles holds the reference poses of the other lane of a
// candidate pair, with their exact bodies of shape, in groups in a grid of
// cells.
type mergeObstacles struct {
	shape  mergeShape
	poses  []mergePose
	bodies [][4]Point
	groups []mergeGroup
	cells  map[[2]int64][]int
	cell   float64
	// radius is the largest radius of a group.
	radius float64
}

// mergeGroup is a run of poses [first, last] on one segment, or one pose
// of a turn. The rectangle that the bodies of the run sweep, grown by
// mergeGroupPad, holds the body at each pose of the run, so a body that is
// clear of the rectangle is clear of each pose of the run. A group speeds
// up the reference and does not change its result.
type mergeGroup struct {
	first, last int
	rectangle   [4]Point
	center      Point
	radius      float64
	// ready reports that the bodies of the poses of the group are in
	// mergeObstacles.bodies.
	ready bool
}

// The number of poses in a group on a segment, and the growth of its
// rectangle for the rounding of the positions of the poses.
const (
	mergeGroupPoses = 32
	mergeGroupPad   = 1e-6
)

// newMergeObstacles samples a lane with the body shape as the sweep sees
// it: each segment every step meters, each inner turn, and the turns to the
// lanes joined at both ends. It keeps the poses in the box, and the poses
// on a segment one step beyond the box, so that each position of the lane
// in the box has a pose within step/2. A lane with no length is a full
// turn at its start.
func newMergeObstacles(network Network, joins mergeJoins, lane Lane, shape mergeShape, step float64, box mergeBox) *mergeObstacles {
	points := network.Polyline(lane)
	reach := shape.cornerReach(0)
	o := &mergeObstacles{shape: shape, cells: make(map[[2]int64][]int), cell: 2 * reach}
	if _, _, ok := mergeHeadings(points); !ok {
		o.poses = mergeTurnPoses(points[0], 0, 0, 2*math.Pi, step, reach)
	} else {
		o.poses = mergeLanePoses(points, step, step, reach, func(a, b Point, length, _ float64) (float64, float64) {
			lo, hi := box.clip(a, b, length)
			if lo > hi {
				return 1, 0
			}
			return lo - step, hi + step
		}, func(at Point, _ float64) bool { return box.holds(at) })
		for _, atStart := range []bool{true, false} {
			for _, pose := range joins.poses(network, lane, atStart, step, reach) {
				if box.holds(pose.position) {
					o.poses = append(o.poses, pose)
				}
			}
		}
	}
	o.bodies = make([][4]Point, len(o.poses))
	// A run continues while the poses are on a segment, at its heading, and
	// each pose is ahead of the pose before it.
	for first := 0; first < len(o.poses); {
		last := first
		for last+1 < len(o.poses) && last+1-first < mergeGroupPoses && o.poses[first].turn == 0 && o.poses[last+1].turn == 0 &&
			o.poses[last+1].heading == o.poses[first].heading && o.poses[last+1].distance > o.poses[last].distance {
			last++
		}
		rear := shape.grown(o.poses[first].position, o.poses[first].heading, mergeGroupPad)
		front := shape.grown(o.poses[last].position, o.poses[last].heading, mergeGroupPad)
		group := mergeGroup{first: first, last: last, rectangle: [4]Point{rear[0], front[1], front[2], rear[3]}}
		for _, corner := range group.rectangle {
			group.center = Point{X: group.center.X + corner.X/4, Y: group.center.Y + corner.Y/4}
		}
		for _, corner := range group.rectangle {
			group.radius = max(group.radius, pointDistance(group.center, corner))
		}
		o.radius = max(o.radius, group.radius)
		key := o.key(group.center)
		o.cells[key] = append(o.cells[key], len(o.groups))
		o.groups = append(o.groups, group)
		first = last + 1
	}
	return o
}

func (o *mergeObstacles) key(p Point) [2]int64 {
	return [2]int64{int64(math.Floor(p.X / o.cell)), int64(math.Floor(p.Y / o.cell))}
}

// meetsRectangle reports whether a rectangle overlaps the exact body at a
// pose of the obstacles. Each corner of the rectangle is within reach of
// center.
func (o *mergeObstacles) meetsRectangle(body [4]Point, center Point, reach float64) bool {
	low := o.key(Point{X: center.X - reach - o.radius, Y: center.Y - reach - o.radius})
	high := o.key(Point{X: center.X + reach + o.radius, Y: center.Y + reach + o.radius})
	for x := low[0]; x <= high[0]; x++ {
		for y := low[1]; y <= high[1]; y++ {
			for _, index := range o.cells[[2]int64{x, y}] {
				group := &o.groups[index]
				if pointDistance(center, group.center) > reach+group.radius || mergeBodiesDisjoint(body, group.rectangle) {
					continue
				}
				if !group.ready {
					for i := group.first; i <= group.last; i++ {
						o.bodies[i] = o.shape.grown(o.poses[i].position, o.poses[i].heading, 0)
					}
					group.ready = true
				}
				for i := group.first; i <= group.last; i++ {
					if !mergeBodiesDisjoint(body, o.bodies[i]) {
						return true
					}
				}
			}
		}
	}
	return false
}

// mergeSide is the lateral merge reference of one lane of a candidate pair
// against the other lane of the pair at node. shape is the body on the
// lane, and the obstacles have the body of the other lane (see
// mergeLaneShape).
//
// The sweep counts a part of a step as a meeting when the rectangle that
// holds the body over the part is not clear of the other lane, within
// resolution. On a segment, that rectangle is the body over the part. On a
// rotation by an angle a, it is mergeShape.turnRectangle, and the body at
// the middle heading grown by shape.cornerReach(0)*a/2 on each side holds
// it. The reference samples each lane every step meters, and each turn so
// that a corner moves at most step, so each pose of a lane is within
// step/2 of a sampled pose. In the body range, a body grown by step +
// resolution, and by shape.cornerReach(0)*a/2 at a pose of a rotation by
// a, then meets the sampled bodies of the other lane wherever the sweep can
// count a meeting. The exact body meets them only where the sweep must
// count one. Clause 5 uses the rectangles of the sweep (reachesFar).
type mergeSide struct {
	network    Network
	joins      mergeJoins
	lane       Lane
	other      Lane
	shape      mergeShape
	points     []Point
	length     float64
	converging bool
	step       float64
	// resolution bounds the distance from the other lane at which the
	// sweep can count a meeting, and grow is step + resolution.
	resolution, grow float64
	obstacles        *mergeObstacles
}

// newMergeSide samples the other lane in a box about lane.
func newMergeSide(network Network, joins mergeJoins, lane, other Lane, node string, step float64) *mergeSide {
	s := &mergeSide{network: network, joins: joins, lane: lane, other: other, shape: mergeLaneShape(network, lane), points: network.Polyline(lane), length: network.Length(lane), converging: lane.To == node, step: step}
	otherShape := mergeLaneShape(network, other)
	// The sweep bisects a step of the other lane to a corner travel of
	// 1e-3 m, or to a depth of 20 on a long step.
	otherPoints, longest := network.Polyline(other), 0.0
	for i := 1; i < len(otherPoints); i++ {
		longest = max(longest, pointDistance(otherPoints[i-1], otherPoints[i]))
	}
	s.resolution = max(1e-3, (longest+2*math.Pi*otherShape.cornerReach(0))/(1<<20)) + 1e-6
	s.grow = step + s.resolution
	// The box holds each body that a pose of the lane, grown by the most
	// that a pose of the lane grows, can meet. It also holds each
	// rectangle of reachesFar grown by s.grow, because the grown body at
	// the middle pose of a turn holds the rectangle of the turn.
	reach := s.shape.cornerReach(0)
	turns := mergeLanePoses(s.points, step, step, reach, func(_, _ Point, _, _ float64) (float64, float64) { return 1, 0 }, func(Point, float64) bool { return true })
	turn := 0.0
	for _, pose := range append(turns, joins.poses(network, lane, s.converging, step, reach)...) {
		turn = max(turn, math.Abs(pose.turn))
	}
	largest := s.grow + mergeCoarseStep/2 + reach*turn/2
	s.obstacles = newMergeObstacles(network, joins, other, otherShape, step, mergeBoxOf(s.points, s.shape.cornerReach(largest)+otherShape.cornerReach(0)+step))
	return s
}

// far returns the distance of a position on the lane from its far end: the
// start when converging, or the end otherwise.
func (s *mergeSide) far(distance float64) float64 {
	if s.converging {
		return distance
	}
	return s.length - distance
}

// extra returns the growth of the body at a pose for an upper bound.
func (s *mergeSide) extra(pose mergePose) float64 {
	return s.grow + s.shape.cornerReach(0)*math.Abs(pose.turn)/2
}

// meets reports whether the body of the lane at a pose, grown by extra,
// overlaps the exact body at a pose of the obstacles.
func (s *mergeSide) meets(obstacles *mergeObstacles, pose mergePose, extra float64) bool {
	return obstacles.meetsRectangle(s.shape.grown(pose.position, pose.heading, extra), pose.position, s.shape.cornerReach(extra))
}

// reachesFar checks clause 5 at the far end of the lane. It tests the
// rectangles that the sweep tests there: for the segment at the far end,
// the body over the part that the sweep tests last, which is the segment
// halved toward the far end until it is at most conflictSampleStep long;
// and for each turn to a lane joined at the far end, the rectangle of the
// whole turn (mergeShape.turnRectangle). The sweep does not halve a turn,
// because a turn has no length. A lane with no length is a full turn at
// its start. certain reports that a rectangle meets the other lane, so the
// sweep must count the far end as reached, and possible that the sweep can
// count it (decide).
func (s *mergeSide) reachesFar() (certain, possible bool) {
	first, last := -1, -1
	for i := 1; i < len(s.points); i++ {
		if pointDistance(s.points[i-1], s.points[i]) > 0 {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	turns := []mergeTurn{{at: s.points[0], turn: 2 * math.Pi}}
	if first >= 0 {
		turns = s.joins.turns(s.network, s.lane, s.converging)
		segment := first
		if !s.converging {
			segment = last
		}
		a, b := s.points[segment-1], s.points[segment]
		span := pointDistance(a, b)
		part := span
		for part > conflictSampleStep {
			part /= 2
		}
		from, to := 0.0, part
		if !s.converging {
			from, to = span-part, span
		}
		heading := math.Atan2(b.Y-a.Y, b.X-a.X)
		certain, possible = s.decide(func(extra float64) [4]Point {
			grow := 0.5 + extra
			return mergeRectangle(a, heading, [2]float64{from - s.shape.rear - grow, to + s.shape.front + grow}, [2]float64{-s.shape.halfWidth - grow, s.shape.halfWidth + grow})
		})
	}
	for _, turn := range turns {
		c, p := s.decide(func(extra float64) [4]Point {
			return s.shape.turnRectangle(turn.at, turn.heading, turn.turn, extra)
		})
		certain, possible = certain || c, possible || p
	}
	return certain, possible
}

// decide reports whether a rectangle on the lane meets the other lane,
// within the resolution of the sweep. rectangle returns it grown by extra
// on each side. certain reports that the rectangle meets the body at a
// sampled pose of the other lane, so the sweep must count a meeting.
// possible reports that the rectangle grown by the sample step plus the
// resolution meets such a body, so the sweep can count one.
//
// With a sample step d, each pose of the other lane is within d/2 of a
// sampled pose on a segment, and a corner moves at most d/2 to the nearest
// sampled pose of a turn. So the body at each pose of the other lane is
// within d/2 of a sampled body, and the rectangle grown by d + resolution
// meets a sampled body wherever the sweep can count a meeting. When only
// the grown rectangle meets, decide samples the other lane again, near the
// rectangle, at half the step, until one test decides or the step is less
// than the resolution.
func (s *mergeSide) decide(rectangle func(extra float64) [4]Point) (certain, possible bool) {
	obstacles, step := s.obstacles, s.step
	for {
		exact, grown := rectangle(0), rectangle(step+s.resolution)
		if obstacles.meetsRectangle(exact, mergeCenter(exact), mergeRadius(exact)) {
			return true, true
		}
		if !obstacles.meetsRectangle(grown, mergeCenter(grown), mergeRadius(grown)) {
			return false, false
		}
		if step/2 < s.resolution {
			return false, true
		}
		step /= 2
		obstacles = newMergeObstacles(s.network, s.joins, s.other, s.obstacles.shape, step, mergeBoxOf(grown[:], s.obstacles.shape.cornerReach(0)+step))
	}
}

// mergeCenter returns the center of a rectangle.
func mergeCenter(r [4]Point) Point {
	return Point{X: (r[0].X + r[2].X) / 2, Y: (r[0].Y + r[2].Y) / 2}
}

// mergeRadius returns the largest distance of a corner of a rectangle from
// its center, with a pad for the rounding of the corners.
func mergeRadius(r [4]Point) float64 {
	center, radius := mergeCenter(r), 0.0
	for _, corner := range r {
		radius = max(radius, pointDistance(center, corner))
	}
	return radius*(1+1e-9) + 1e-9
}

// mergeCoarseStep is the sample step in meters of the first pass of
// mergeSide.bodyRange.
const mergeCoarseStep = 1.0

// bodyRange returns the distance along the lane of the first pose from the
// far end whose exact body meets the other lane, inner, and of the first
// pose whose grown body meets it, outer. It stops after the far end of the
// node range plus the sample pads. inner and outer are +Inf when converging
// and -Inf otherwise when no pose meets.
//
// A first pass samples the segments every mergeCoarseStep meters, with the
// body grown by mergeCoarseStep/2 more. Each pose of the second pass is
// within mergeCoarseStep/2 of a pose of the first pass on the same
// segment, or is a pose of the first pass, so the grown body of the second
// pass meets the other lane only at mergeCoarseStep/2 or less before the
// first meeting of the first pass. The second pass starts there, and the
// first pass goes mergeCoarseStep/2 further.
func (s *mergeSide) bodyRange() (inner, outer float64) {
	none := math.Inf(1)
	if !s.converging {
		none = math.Inf(-1)
	}
	limit := s.length - Clearance + conflictSampleStep + 2*s.step
	coarse, _ := s.scan(mergeCoarseStep, mergeCoarseStep/2, math.Inf(-1), limit+mergeCoarseStep/2, false)
	if math.IsInf(coarse, 1) {
		return none, none
	}
	outer, inner = s.scan(s.step, 0, coarse-mergeCoarseStep/2, limit, true)
	if math.IsInf(outer, 1) {
		return none, none
	}
	outer = s.distance(outer)
	if math.IsInf(inner, 1) {
		return none, outer
	}
	return s.distance(inner), outer
}

// distance converts a distance from the far end to a distance along the
// lane.
func (s *mergeSide) distance(far float64) float64 {
	if s.converging {
		return far
	}
	return s.length - far
}

// scan samples the lane every step meters from the far end, from the
// distance from to the distance limit from the far end. It returns the
// distance from the far end of the first pose whose body, grown by
// s.extra and more, meets the other lane, and with exact, the first pose
// after it whose exact body meets the other lane. Each is +Inf when no
// pose meets.
func (s *mergeSide) scan(step, more, from, limit float64, exact bool) (grown, inner float64) {
	grown, inner = math.Inf(1), math.Inf(1)
	low, high := max(0, from), limit
	if !s.converging {
		low, high = s.length-limit, s.length-max(0, from)
	}
	window := func(distance float64) bool { return distance >= low && distance <= high }
	poses := mergeLanePoses(s.points, step, s.step, s.shape.cornerReach(0), func(_, _ Point, _, distance float64) (float64, float64) {
		return low - step - distance, high + step - distance
	}, func(_ Point, distance float64) bool { return window(distance) })
	// The poses are in the order of the lane.
	if !s.converging {
		slices.Reverse(poses)
	}
	for _, pose := range poses {
		if !window(pose.distance) {
			continue
		}
		if math.IsInf(grown, 1) && s.meets(s.obstacles, pose, s.extra(pose)+more) {
			grown = s.far(pose.distance)
			if !exact {
				return grown, inner
			}
		}
		if !math.IsInf(grown, 1) && s.meets(s.obstacles, pose, 0) {
			return grown, s.far(pose.distance)
		}
	}
	return grown, inner
}

// TestScenarioLateralMergesShortenExtents compares each network of
// testdata/scenario_networks.json.gz with LateralMerges set and with it
// cleared. With the field set, some extent is shorter, and each extent
// lies inside the isotropic extent of the same pair, up to the rounding of
// the samples of the isotropic extent. Each extent at a node where both
// lanes end or both start holds the node range. For each lateral merge
// pair, the grown body at the far end of each lane, and in the turn to each
// lane joined there, clears the grown body at each reference pose of the
// other lane (clause 5).
func TestScenarioLateralMergesShortenExtents(t *testing.T) {
	t.Parallel()
	skipLong(t)
	networks := scenarioNetworks(t)
	for _, name := range slices.Sorted(maps.Keys(networks)) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			network := networks[name]
			network.LateralMerges = false
			off := junctionConflictsOf(network)
			network.LateralMerges = true
			on := junctionConflictsOf(network)
			lengths := make(map[string]float64, len(network.Lanes))
			for _, lane := range network.Lanes {
				lengths[lane.ID] = network.Length(lane)
			}
			shorter := 0
			for _, lane := range network.Lanes {
				if len(on[lane.ID]) != len(off[lane.ID]) {
					t.Fatalf("lane %s has %d extents, want %d", lane.ID, len(on[lane.ID]), len(off[lane.ID]))
				}
				for index, got := range on[lane.ID] {
					want := off[lane.ID][index]
					if got.junction != want.junction || got.start < want.start-1e-9 || got.end > want.end+1e-9 {
						t.Fatalf("lane %s extent %+v is not inside the isotropic extent %+v", lane.ID, got, want)
					}
					if got != want {
						shorter++
						length := lengths[lane.ID]
						if lane.To == got.junction && (got.end != length || got.start > max(0, length-Clearance-conflictSampleStep)) ||
							lane.From == got.junction && (got.start != 0 || got.end < min(length, Clearance+conflictSampleStep)) {
							t.Fatalf("lane %s extent %+v of %v m does not hold the node range", lane.ID, got, length)
						}
					}
				}
			}
			positions := indexNodePositions(network.Nodes)
			maneuvers, _ := newManeuverIndex(network, positions)
			depots, _ := newDepotIndex(network, positions)
			merges := newMergeLateral(network, indexGeometryTails(network), maneuvers, depots)
			lanes := make(map[string]int, len(network.Lanes))
			for index, lane := range network.Lanes {
				lanes[lane.ID] = index
			}
			poses := make(map[string][]referencePose)
			sides := 0
			forEachJunctionPair(network, func(node string, lane, other Lane) {
				if !merges.mergePartner(lanes[lane.ID], lanes[other.ID]) {
					return
				}
				sides++
				if poses[other.ID] == nil {
					poses[other.ID] = referencePoses(network.Polyline(other), 0.25)
				}
				if start, end := referenceBodyRange(farEndPoses(network, lane, node), poses[other.ID]); start <= end {
					t.Errorf("lane %s at %s: the body at its far end meets the body on lane %s", lane.ID, node, other.ID)
				}
			})
			t.Logf("%d shorter extents, %d lateral merge pair sides", shorter, sides)
			if shorter == 0 {
				t.Error("no extent is shorter with LateralMerges set")
			}
		})
	}
}

// laneEndNetwork returns a candidate pair at node "n" whose lane "a" is
// short meters long on the X axis, against lane "b", 300 m long at the
// given angle in degrees. With converging, lane "p" runs into the start of
// a, a and b end at n, and lane "exit" leaves n. Otherwise lane "feed" runs
// into n, a and b start at n, lane "d" continues a to node "m", and lane
// "b2" joins b to m. A destination station is far after the pair, and a
// small origin station, not connected, gives the requests an origin.
func laneEndNetwork(converging bool, degrees, short float64) Network {
	radians := degrees * math.Pi / 180
	far := Point{X: 300 * math.Cos(radians), Y: 300 * math.Sin(radians)}
	const approach = 10000.0
	end := 1200.0
	network := Network{
		Nodes: []Node{
			{ID: "n"},
			{ID: "origin-entry", Position: Point{X: -3000, Y: 1000}},
			{ID: "origin-berth", Position: Point{X: -2900, Y: 1060}},
			{ID: "origin-exit", Position: Point{X: -2800, Y: 1000}},
		},
		Lanes: []Lane{
			{ID: "origin-through", From: "origin-entry", To: "origin-exit", SpeedLimit: 14},
			{ID: "origin-in", From: "origin-entry", To: "origin-berth", SpeedLimit: 14},
			{ID: "origin-out", From: "origin-berth", To: "origin-exit", SpeedLimit: 14},
		},
		LateralMerges: true,
	}
	if converging {
		network.Nodes = append(network.Nodes,
			Node{ID: "p-start", Position: Point{X: -3000}}, Node{ID: "a-start", Position: Point{X: -short}},
			Node{ID: "b-start", Position: Point{X: -far.X, Y: -far.Y}}, Node{ID: "m", Position: Point{X: end}})
		network.Lanes = append(network.Lanes,
			Lane{ID: "a", From: "a-start", To: "n", SpeedLimit: 14}, Lane{ID: "b", From: "b-start", To: "n", SpeedLimit: 14},
			Lane{ID: "p", From: "p-start", To: "a-start", SpeedLimit: 14}, Lane{ID: "exit", From: "n", To: "m", SpeedLimit: 14})
	} else {
		network.Nodes = append(network.Nodes,
			Node{ID: "feed-start", Position: Point{X: -3000}}, Node{ID: "a-end", Position: Point{X: short}},
			Node{ID: "b-end", Position: far}, Node{ID: "m", Position: Point{X: end}})
		network.Lanes = append(network.Lanes,
			Lane{ID: "a", From: "n", To: "a-end", SpeedLimit: 14}, Lane{ID: "b", From: "n", To: "b-end", SpeedLimit: 14},
			Lane{ID: "feed", From: "feed-start", To: "n", SpeedLimit: 14}, Lane{ID: "d", From: "a-end", To: "m", SpeedLimit: 14},
			Lane{ID: "b2", From: "b-end", To: "m", SpeedLimit: 14})
	}
	network.Nodes = append(network.Nodes,
		Node{ID: "dest-entry", Position: Point{X: end + approach}}, Node{ID: "dest-exit", Position: Point{X: end + approach + 200}})
	network.Lanes = append(network.Lanes,
		Lane{ID: "approach", From: "m", To: "dest-entry", SpeedLimit: 14}, Lane{ID: "dest-through", From: "dest-entry", To: "dest-exit", SpeedLimit: 14})
	network.Stations = []Station{
		{ID: "origin", Name: "Origin", Entry: "origin-entry", Exit: "origin-exit", Berths: []Berth{{ID: "origin-1", Node: "origin-berth"}}},
		{ID: "dest", Name: "Destination", Entry: "dest-entry", Exit: "dest-exit"},
	}
	for index := range 8 {
		node := fmt.Sprintf("dest-berth-%02d", index+1)
		network.Nodes = append(network.Nodes, Node{ID: node, Position: Point{X: end + approach + 100, Y: 40 + 25*float64(index)}})
		network.Lanes = append(network.Lanes,
			Lane{ID: node + "-in", From: "dest-entry", To: node, SpeedLimit: 14},
			Lane{ID: node + "-out", From: node, To: "dest-exit", SpeedLimit: 14})
		network.Stations[1].Berths = append(network.Stations[1].Berths, Berth{ID: fmt.Sprintf("dest-%02d", index+1), Node: node})
	}
	return network
}

// mergePairOf reports whether lanes a and b of a network form a lateral
// merge pair.
func mergePairOf(network Network) bool {
	positions := indexNodePositions(network.Nodes)
	maneuvers, _ := newManeuverIndex(network, positions)
	depots, _ := newDepotIndex(network, positions)
	merges := newMergeLateral(network, indexGeometryTails(network), maneuvers, depots)
	lanes := make(map[string]int, len(network.Lanes))
	for index, lane := range network.Lanes {
		lanes[lane.ID] = index
	}
	return merges.mergePartner(lanes["a"], lanes["b"])
}

// TestLateralMergeExcludesLaneEndPairs checks clause 5 on a 24 m lane a
// that meets lane b at 3 degrees: at the far end of a, the lanes are 1.26
// m apart, so the body range of a reaches that end. The pair is not a
// lateral merge pair. Its extents are the isotropic extents, two pods on a
// and b about 6 m apart fail Check, and debris on a does not block the
// cells of b.
func TestLateralMergeExcludesLaneEndPairs(t *testing.T) {
	t.Parallel()
	for _, converging := range []bool{true, false} {
		t.Run(fmt.Sprintf("converging %v", converging), func(t *testing.T) {
			t.Parallel()
			network := laneEndNetwork(converging, 3, 24)
			if mergePairOf(network) {
				t.Error("lanes a and b form a lateral merge pair")
			}
			checkSameExtents(t, network, func(lane Lane) bool { return lane.ID == "a" || lane.ID == "b" })
			s, err := NewFleet(network, []Placement{{ID: "p1", StationID: "dest", BerthID: "dest-01"}, {ID: "p2", StationID: "dest", BerthID: "dest-02"}})
			if err != nil {
				t.Fatal(err)
			}
			radians := 3 * math.Pi / 180
			first, second := Point{X: 4}, Point{X: 10 * math.Cos(radians), Y: 10 * math.Sin(radians)}
			if converging {
				first, second = Point{X: -first.X}, Point{X: -second.X, Y: -second.Y}
			}
			if gap := pointDistance(first, second); gap <= lateralClearance(0) || gap >= Clearance {
				t.Fatalf("the pods are %v m apart, want between %v and %v m", gap, lateralClearance(0), Clearance)
			}
			for index, place := range []struct {
				lane     string
				position Point
			}{{"a", first}, {"b", second}} {
				pod := &s.vehicles[index].Pod
				pod.Activity, pod.BerthID, pod.StationID, pod.LaneID, pod.Position = Traveling, "", "", place.lane, place.position
			}
			if _, err := s.SafetyObservation().Check(); err == nil {
				t.Error("Check passes two pods on a and b nearer than Clearance")
			}
			a, b := s.graph.lanes["a"], s.graph.lanes["b"]
			footprint := s.debrisFootprint(a, 10, 12)
			for cell := range s.laneCells["b"].count() {
				for _, r := range s.laneCells["b"].cell(cell) {
					if r.kind == trackResource && slices.Contains(footprint, r) {
						t.Errorf("debris on a blocks cell %d of b", cell)
					}
				}
			}
			if len(s.merges.neighborLanes(a)) != 0 || len(s.merges.neighborLanes(b)) != 0 {
				t.Errorf("lateral merge partners %v and %v, want none", s.merges.neighborLanes(a), s.merges.neighborLanes(b))
			}
		})
	}
}

// TestLateralMergeShortLaneStaysLateral checks that clause 5 tests the body
// range and not the node range. Lane a is 10 m long and meets lane b at 60
// degrees. The node range covers a, but the body range, about 7 m, does
// not reach the far end of a, so the pair is a lateral merge pair, and b
// gets an extent shorter than its isotropic extent.
func TestLateralMergeShortLaneStaysLateral(t *testing.T) {
	t.Parallel()
	for _, converging := range []bool{true, false} {
		t.Run(fmt.Sprintf("converging %v", converging), func(t *testing.T) {
			t.Parallel()
			network := laneEndNetwork(converging, 60, 10)
			if !mergePairOf(network) {
				t.Fatal("lanes a and b do not form a lateral merge pair")
			}
			on := junctionConflictsOf(network)
			network.LateralMerges = false
			off := junctionConflictsOf(network)
			index := slices.IndexFunc(network.Lanes, func(lane Lane) bool { return lane.ID == "a" })
			length := network.Length(network.Lanes[index])
			if got := conflictWith(t, on, "a"); got.start != 0 || got.end != length {
				t.Errorf("lane a extent %+v, want the node range [0, %v]", got, length)
			}
			if got, isotropic := conflictWith(t, on, "b"), conflictWith(t, off, "b"); got.end-got.start >= isotropic.end-isotropic.start {
				t.Errorf("lane b extent %+v, want shorter than the isotropic extent %+v", got, isotropic)
			}
		})
	}
}

// TestLateralMergeLaneEndJoin checks that a join step at the far end of a
// lane counts as part of the lane. Lane a is 40 m long and meets lane b at
// 7.8 degrees, so the lanes are 5.5 m apart at the far end of a, and the
// body range of a stops before that end. A lane d that turns 90 degrees
// toward b at the end of a turns the body at that end into b, so with d,
// the pair is not a lateral merge pair.
func TestLateralMergeLaneEndJoin(t *testing.T) {
	t.Parallel()
	radians := 7.8 * math.Pi / 180
	network := Network{
		Nodes: []Node{{ID: "n"}, {ID: "a-end", Position: Point{X: 40}}, {ID: "b-end", Position: Point{X: 200 * math.Cos(radians), Y: 200 * math.Sin(radians)}}, {ID: "d-end", Position: Point{X: 40, Y: 2}}},
		Lanes: []Lane{
			{ID: "a", From: "n", To: "a-end", SpeedLimit: 14},
			{ID: "b", From: "n", To: "b-end", SpeedLimit: 14},
		},
		LateralMerges: true,
	}
	if !mergePairOf(network) {
		t.Fatal("without d, lanes a and b do not form a lateral merge pair")
	}
	network.Lanes = append(network.Lanes, Lane{ID: "d", From: "a-end", To: "d-end", SpeedLimit: 14})
	if mergePairOf(network) {
		t.Error("with d, lanes a and b form a lateral merge pair")
	}
}

// laneEndBodies returns an error when the grown body of a pod on one of
// the lanes in first overlaps the grown body of a pod on one of the lanes
// in second. It also returns the smallest distance between two such pods,
// or +Inf.
func laneEndBodies(s *Simulation, first, second []string) (float64, error) {
	smallest := math.Inf(1)
	body := func(pod Pod) [4]Point {
		return mergeBody(pod.Position, laneHeading(s.network.Polyline(s.network.Lanes[s.graph.lanes[pod.LaneID]]), pod.LaneDistance))
	}
	for i := range s.vehicles {
		p := s.vehicles[i].Pod
		if !slices.Contains(first, p.LaneID) {
			continue
		}
		for j := range s.vehicles {
			q := s.vehicles[j].Pod
			if !slices.Contains(second, q.LaneID) {
				continue
			}
			smallest = min(smallest, pointDistance(p.Position, q.Position))
			if !mergeBodiesDisjoint(body(p), body(q)) {
				return smallest, fmt.Errorf("tick %d: the bodies of pods %s on %s at %.3f m and %s on %s at %.3f m overlap", s.tick, p.ID, p.LaneID, p.LaneDistance, q.ID, q.LaneID, q.LaneDistance)
			}
		}
	}
	return smallest, nil
}

// runLaneEndStreams restores a queue of six stopped pods on lane feed of a
// network of laneEndNetwork, in two streams: the pods take lane b and lane
// a in turn, from the head of the queue. It runs 300 s. At each tick, it
// runs Check and the body oracle of the pods on a or d against the pods on
// b. It returns the smallest distance between a pod on a or d and a pod on
// b, the number of pods that entered d, and the first error.
func runLaneEndStreams(t *testing.T, network Network) (float64, int, error) {
	t.Helper()
	laneIndex := make(map[string]int, len(network.Lanes))
	for index, lane := range network.Lanes {
		laneIndex[lane.ID] = index
	}
	var fleet []Placement
	state := SavedState{SharedRidePartyLimit: 1}
	for index := range laneEndPods {
		id := fmt.Sprintf("p%d", index+1)
		fleet = append(fleet, Placement{ID: id, StationID: "dest", BerthID: fmt.Sprintf("dest-%02d", index+1)})
		state.RequestID++
		state.Boarded++
		route := []int{laneIndex["feed"], laneIndex["b"], laneIndex["b2"], laneIndex["approach"]}
		if index%2 == 1 {
			route = []int{laneIndex["feed"], laneIndex["a"], laneIndex["d"], laneIndex["approach"]}
		}
		distance := 2900 - corridorQueueGap*float64(index)
		state.Pods = append(state.Pods, SavedPod{
			ID: id, Activity: "traveling", Occupied: true, Origin: "origin-1", DestinationStation: "dest",
			Riders:    []SavedRequest{{SharingConsent: SharedConsent, Service: OnDemandService, ID: state.RequestID, From: "origin", To: "dest", PartySize: 1, PodID: id}},
			Boardings: []RiderBoarding{{BerthID: "origin-1"}},
			Stops:     []string{"dest"},
			Route:     route, LaneID: "feed", LaneDistance: distance, Distance: distance,
		})
	}
	s, result, err := RestoreState(RestoreStateInput{Network: network, Fleet: fleet, State: state})
	if err != nil {
		t.Fatal(err)
	}
	if result.Tier != RestorePhysical || len(result.Demoted) != 0 {
		t.Fatalf("the queue did not restore in place: %+v", result)
	}
	smallest := math.Inf(1)
	onD := make(map[string]bool)
	for range 300 * TicksPerSecond {
		s.Step()
		if _, err := s.SafetyObservation().Check(); err != nil {
			return smallest, len(onD), fmt.Errorf("tick %d: %w", s.tick, err)
		}
		checkMergeBodies(t, s)
		gap, err := laneEndBodies(s, []string{"a", "d"}, []string{"b"})
		smallest = min(smallest, gap)
		if err != nil {
			return smallest, len(onD), err
		}
		for _, v := range s.vehicles {
			if v.Pod.LaneID == "d" {
				onD[v.Pod.ID] = true
			}
		}
	}
	return smallest, len(onD), nil
}

// laneEndPods is the number of pods of runLaneEndStreams.
const laneEndPods = 6

// TestLateralMergeLaneEndTwoStreams runs the two streams of
// runLaneEndStreams through the split of
// TestLateralMergeExcludesLaneEndPairs, with LateralMerges set. Lane b has
// a speed limit of 1 m/s, so that each pod on d passes a pod on b. Lanes a
// and d are in one separation group and lanes b and b2 in another, so
// Check does not compare a pod on d with a pod on b, and only the body
// oracle checks them. The run checks that the isotropic rule of the pair
// holds in a live run. The pair as a lateral merge pair gives the same run
// here, because the extents of b under both rules end in the same cell.
func TestLateralMergeLaneEndTwoStreams(t *testing.T) {
	t.Parallel()
	network := laneEndNetwork(false, 3, 24)
	for index := range network.Lanes {
		lane := &network.Lanes[index]
		switch lane.ID {
		case "a", "d":
			lane.SeparationGroup = "main"
		case "b":
			lane.SeparationGroup, lane.SpeedLimit = "ramp", 1
		case "b2":
			lane.SeparationGroup = "ramp"
		}
	}
	if mergePairOf(network) {
		t.Fatal("lanes a and b form a lateral merge pair")
	}
	smallest, onD, err := runLaneEndStreams(t, network)
	t.Logf("smallest distance between a pod on a or d and a pod on b: %.3f m", smallest)
	if err != nil {
		t.Fatal(err)
	}
	if onD != laneEndPods/2 || !(smallest < Clearance) {
		t.Errorf("%d pods entered d, and the smallest distance is %.3f m: want %d pods, and less than %v m, so that the run tests the rule", onD, smallest, laneEndPods/2, Clearance)
	}
}
