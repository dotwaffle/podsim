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

// conflictWith returns the extent at an index of the extents of a lane in
// a table. A lane has one extent for each other lane at each node, in
// forEachJunctionPair order, so the test networks name the lanes such that
// the first extent is the one against the wanted lane.
func conflictWith(t *testing.T, conflicts map[string][]laneConflict, lane string, index int) laneConflict {
	t.Helper()
	if index >= len(conflicts[lane]) {
		t.Fatalf("lane %s has %d extents, want more than %d", lane, len(conflicts[lane]), index)
	}
	return conflicts[lane][index]
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
					got, isotropic := conflictWith(t, on, lane, 0), conflictWith(t, off, lane, 0)
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

// mergeBody is the grown body of a small pod as four corners: lateralRear
// plus half the margin behind the reference, sweepFront plus half the
// margin ahead, and sweepHalfWidth plus half the margin to each side, at
// the heading of the lane at the reference. It shares no code with the
// sweep.
func mergeBody(position Point, heading float64) [4]Point {
	const grow = 0.5
	s, c := math.Sincos(heading)
	var corners [4]Point
	for index, corner := range [4][2]float64{{-lateralRear - grow, -sweepHalfWidth - grow}, {sweepFront + grow, -sweepHalfWidth - grow}, {sweepFront + grow, sweepHalfWidth + grow}, {-lateralRear - grow, sweepHalfWidth + grow}} {
		corners[index] = Point{X: position.X + corner[0]*c - corner[1]*s, Y: position.Y + corner[0]*s + corner[1]*c}
	}
	return corners
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
					got := conflictWith(t, conflicts, pair[0], 0)
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

// TestScenarioLateralMergesShortenExtents sets LateralMerges on each
// network of testdata/scenario_networks.json.gz. Each extent lies inside
// the isotropic extent of the same pair, up to the rounding of the
// samples of the isotropic extent, and each extent at a node where both
// lanes end or both start holds the node range.
func TestScenarioLateralMergesShortenExtents(t *testing.T) {
	t.Parallel()
	skipLong(t)
	networks := scenarioNetworks(t)
	for _, name := range slices.Sorted(maps.Keys(networks)) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			network := networks[name]
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
			t.Logf("%d shorter extents", shorter)
		})
	}
}
