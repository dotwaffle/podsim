package sim

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
	"testing"
)

// The tests of the merge and split pairs of a network (part 3 and decision
// D5 of docs/design/shape-clearance.md):
// a lane that admits a large class or has a geometry tail can form a pair,
// with the union body of the classes that it admits.

// withShapeClasses returns a copy of a network with the classes of the
// lanes that classes names.
func withShapeClasses(network Network, classes map[string]ClassSet) Network {
	network.Lanes = slices.Clone(network.Lanes)
	for index := range network.Lanes {
		if set, ok := classes[network.Lanes[index].ID]; ok {
			network.Lanes[index].VehicleClasses = set
		}
	}
	return network
}

// shapeMergeHasLarge reports whether a merge pair of a network has a lane
// that admits a large class.
func shapeMergeHasLarge(network Network) bool {
	positions := indexNodePositions(network.Nodes)
	maneuvers, _ := newManeuverIndex(network, positions)
	depots, _ := newDepotIndex(network, positions)
	merges := newMergeLateral(network, maneuvers, depots)
	if merges == nil {
		return false
	}
	for pair := range merges.pairs {
		if largeClassSet(network.Lanes[pair[0].index].VehicleClasses) || largeClassSet(network.Lanes[pair[1].index].VehicleClasses) {
			return true
		}
	}
	return false
}

// TestShapeMergesMatchBodyReference compares the junction conflict table
// of networks with large merge and split pairs with the brute-force body
// reference of referenceMergeConflicts, with the union body of each lane.
// The networks are the quadratic ramps of
// TestLateralMergeRampsMatchBodyReference and straight pairs at several
// angles, each with a lane that admits a large class, and the pair of
// TestLateralMergeExcludesLaneEndPairs with Express lanes, which clause 5
// excludes. The isotropic extent of each other pair has 20 m when a lane
// admits a large class.
func TestShapeMergesMatchBodyReference(t *testing.T) {
	t.Parallel()
	skipLong(t)
	combinations := []map[string]ClassSet{
		{"a": shapeUnionClasses, "b": shapeSmallClasses},
		{"a": shapeSmallClasses, "b": shapeExpressClasses},
		{"a": shapeGroupClasses, "b": shapeExpressClasses},
	}
	networks := make(map[string]Network)
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
			for combination, classes := range combinations {
				networks[fmt.Sprintf("ramp %d converging %v classes %d", index, converging, combination)] = withShapeClasses(rampNetwork(ramp.control, ramp.start, ramp.main, converging), classes)
			}
		}
	}
	for _, degrees := range []float64{3, 6, 15, 30, 90} {
		for _, converging := range []bool{true, false} {
			for combination, classes := range combinations {
				networks[fmt.Sprintf("%v degrees converging %v classes %d", degrees, converging, combination)] = withShapeClasses(lateralPairNetwork(degrees, 200, converging), classes)
			}
		}
	}
	for _, converging := range []bool{true, false} {
		networks[fmt.Sprintf("lane end converging %v", converging)] = withShapeClasses(laneEndNetwork(converging, 3, 24), map[string]ClassSet{"a": shapeExpressClasses, "b": shapeExpressClasses})
	}
	for _, name := range slices.Sorted(maps.Keys(networks)) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			network := networks[name]
			counts := checkMergeConflictsMatchReference(t, network)
			// Clause 5 excludes the Express pair of a lane end network, and
			// the lanes of its stations are small.
			if excluded := strings.HasPrefix(name, "lane end"); excluded {
				if counts.excluded == 0 || mergePairOf(network) {
					t.Fatalf("%d pairs excluded by clause 5, lanes a and b a merge pair %v", counts.excluded, mergePairOf(network))
				}
				return
			}
			if counts.lateral == 0 || !shapeMergeHasLarge(network) {
				t.Fatalf("%d merge pairs, none with a large lane, so the test does not check the union body", counts.lateral)
			}
		})
	}
}

// shapeGrownHalfWidth returns the grown half width of the reference body of
// a lane that admits classes (see mergeLaneShape).
func shapeGrownHalfWidth(classes ClassSet) float64 {
	return mergeLaneShape(Lane{VehicleClasses: classes}).halfWidth + 0.5
}

// shapeUnionReach is a case of TestShapeMergesUnionBody: a pair of
// straight lanes a and b at node "n", where only one part of the union
// body of lane a reaches lane b at the far end of a, by about 0.1 m or
// more. clear is the classes of a lane a whose body does not reach lane b
// there. network returns the lanes for the grown half width of lane b.
type shapeUnionReach struct {
	name    string
	clear   func(large string) ClassSet
	network func(half float64) Network
}

// shapeUnionReaches are the cases of TestShapeMergesUnionBody. Each lane a
// is one segment, and clause 5 tests the body over the sixteenth of the
// segment at the far end, which is at most conflictSampleStep long. So
// the far end of the body is at the distance d = 15L/16 from the node.
//
//   - Small front: a converging lane a comes in at 90 degrees to lane b,
//     which continues straight into "exit". The front of the body at d
//     meets the body of b when d is at most front + half. d is 4.1 m plus
//     the half width of b, so the front of 4.2 m of the small body reaches
//     b, and the front of 4.0 m of the large body does not.
//   - Large rear: the same, diverging, after lane "feed". The rear of the
//     body at d meets b when d is at most rear + half. d is 3.5 m plus the
//     half width of b, so the rear of 4.0 m of the large body reaches b,
//     and the rear of 3.0 m of the small body does not.
//   - Large side: a diverging lane a at 5 degrees to lane b. The rear
//     corner of the body on the side of b is (d - rear) sin(5) - side
//     cos(5) from the line of b. d is between the values where that equals
//     the half width of b for the large body and for the small body, more
//     than 3 m from each, so the side of the large body reaches b, and the
//     small body does not.
var shapeUnionReaches = []shapeUnionReach{
	{
		name:  "small front",
		clear: func(large string) ClassSet { return mustClassSet(large) },
		network: func(half float64) Network {
			length := (4.1 + half) * 16 / 15
			return Network{
				Nodes: []Node{{ID: "n"}, {ID: "a-start", Position: Point{X: -length}}, {ID: "b-start", Position: Point{Y: -300}}, {ID: "m", Position: Point{Y: 300}}},
				Lanes: []Lane{
					{ID: "a", From: "a-start", To: "n", SpeedLimit: 14},
					{ID: "b", From: "b-start", To: "n", SpeedLimit: 14},
					{ID: "exit", From: "n", To: "m", SpeedLimit: 14},
				},
			}
		},
	},
	{
		name:  "large rear",
		clear: func(string) ClassSet { return shapeSmallClasses },
		network: func(half float64) Network {
			length := (3.5 + half) * 16 / 15
			return Network{
				Nodes: []Node{{ID: "n"}, {ID: "a-end", Position: Point{X: length}}, {ID: "b-end", Position: Point{Y: 300}}, {ID: "feed-start", Position: Point{Y: -300}}},
				Lanes: []Lane{
					{ID: "a", From: "n", To: "a-end", SpeedLimit: 14},
					{ID: "b", From: "n", To: "b-end", SpeedLimit: 14},
					{ID: "feed", From: "feed-start", To: "n", SpeedLimit: 14},
				},
			}
		},
	},
	{
		name:  "large side",
		clear: func(string) ClassSet { return shapeSmallClasses },
		network: func(half float64) Network {
			s, c := math.Sincos(5 * math.Pi / 180)
			small, large := mergeSmallShape, mergeLargeShape
			reach := func(body mergeShape) float64 {
				return body.rear + 0.5 + (half+(body.halfWidth+0.5)*c)/s
			}
			length := (reach(small)+reach(large))/2 + conflictSampleStep/2
			return Network{
				Nodes: []Node{{ID: "n"}, {ID: "a-end", Position: Point{X: length * c, Y: -length * s}}, {ID: "b-end", Position: Point{X: 300}}, {ID: "feed-start", Position: Point{X: -300}}},
				Lanes: []Lane{
					{ID: "a", From: "n", To: "a-end", SpeedLimit: 14},
					{ID: "b", From: "n", To: "b-end", SpeedLimit: 14},
					{ID: "feed", From: "feed-start", To: "n", SpeedLimit: 14},
				},
			}
		},
	},
}

// TestShapeMergesUnionBody checks the union body of a lane of a merge or
// split pair that admits a small class and a large class (section 3.2 of
// the design), against a lane b of each class, with Express and with
// Group, in both lane orders. In each case of shapeUnionReaches, only one
// part of the union body reaches lane b at the far end of lane a, so
// clause 5 excludes the pair. A lane a that admits only the class whose
// body does not reach forms a merge pair with the same lane b.
func TestShapeMergesUnionBody(t *testing.T) {
	t.Parallel()
	for _, large := range []string{"express", "group"} {
		mixed := mustClassSet("compact", large)
		for _, test := range shapeUnionReaches {
			for _, others := range []ClassSet{shapeSmallClasses, mustClassSet(large), mixed} {
				for _, classes := range []ClassSet{mixed, test.clear(large)} {
					for _, swap := range []bool{false, true} {
						network := test.network(shapeGrownHalfWidth(others))
						network = withShapeClasses(network, map[string]ClassSet{"a": classes, "b": others})
						if swap {
							network.Lanes[0], network.Lanes[1] = network.Lanes[1], network.Lanes[0]
						}
						if got, want := mergePairOf(network), classes != mixed; got != want {
							t.Errorf("%s with %s: lane a %v against lane b %v, swapped %v: merge pair %v, want %v", test.name, large, classes, others, swap, got, want)
						}
					}
				}
			}
		}
	}
}

// TestShapeMergesExcludeLaneEndPairs checks clause 5 for large lanes: the
// 24 m lane a of TestLateralMergeExcludesLaneEndPairs meets lane b at 3
// degrees, so the body range of a reaches its far end. With Express lanes,
// the pair is not a merge pair, the lanes have no merge
// partner, and their extents are the isotropic extents of 20 m.
func TestShapeMergesExcludeLaneEndPairs(t *testing.T) {
	t.Parallel()
	for _, converging := range []bool{true, false} {
		t.Run(fmt.Sprintf("converging %v", converging), func(t *testing.T) {
			t.Parallel()
			network := withShapeClasses(laneEndNetwork(converging, 3, 24), map[string]ClassSet{"a": shapeExpressClasses, "b": shapeExpressClasses})
			if mergePairOf(network) {
				t.Error("lanes a and b form a merge pair")
			}
			on, off := junctionConflictsOf(network), isotropicConflictsOf(network)
			for _, lane := range []string{"a", "b"} {
				if fmt.Sprint(on[lane]) != fmt.Sprint(off[lane]) {
					t.Errorf("lane %s extents %v, want the isotropic extents %v", lane, on[lane], off[lane])
				}
			}
			// The same pair with a lane a of 300 m is a merge pair, so the
			// test fails when clause 5 does not apply.
			if !mergePairOf(withShapeClasses(laneEndNetwork(converging, 3, 300), map[string]ClassSet{"a": shapeExpressClasses, "b": shapeExpressClasses})) {
				t.Error("with a lane a of 300 m, lanes a and b do not form a merge pair")
			}
		})
	}
}

// TestShapeMergesKeepFollowingRules checks part 2 of the rule on
// largeOracleNetwork against the isotropic extents: the cells of each lane
// do not change, except for the junction resources of the lanes of the
// merge or split pair. Each extent
// of a pair that is not a merge or split pair is the same bit for bit, and
// each extent of the pair lies inside its isotropic extent of 20 m, and is
// shorter.
func TestShapeMergesKeepFollowingRules(t *testing.T) {
	t.Parallel()
	for _, converging := range []bool{true, false} {
		t.Run(fmt.Sprintf("converging %v", converging), func(t *testing.T) {
			t.Parallel()
			network := largeOracleNetwork(t, converging)
			on, err := PrepareNetwork(network)
			if err != nil {
				t.Fatal(err)
			}
			if on.merges == nil {
				t.Fatal("the network has no merge pair")
			}
			off := *on
			off.junctionConflicts = isotropicConflictsOf(on.network)
			off.laneCells = indexLaneCells(laneCellsIndexInput{network: on.network, tails: on.tails, geometry: on.geometry, conflicts: off.junctionConflicts, berths: on.berthResources, retention: on.retention, maneuvers: on.maneuvers, depots: on.depots})
			for _, lane := range network.Lanes {
				checkShapeMergeCells(t, lane.ID, on.laneCells[lane.ID], off.laneCells[lane.ID])
			}
			lanes := make(map[string]int, len(network.Lanes))
			for index, lane := range network.Lanes {
				lanes[lane.ID] = index
			}
			seen, shorter, merges := make(map[string]int), 0, 0
			forEachJunctionPair(network, func(node string, lane, other Lane) {
				index := seen[lane.ID]
				seen[lane.ID]++
				got, want := on.junctionConflicts[lane.ID][index], off.junctionConflicts[lane.ID][index]
				if got.junction != node || want.junction != node {
					t.Fatalf("lane %s extent %d at %s and %s, want %s", lane.ID, index, got.junction, want.junction, node)
				}
				if !on.merges.mergePartner(lanes[lane.ID], lanes[other.ID]) {
					if math.Float64bits(got.start) != math.Float64bits(want.start) || math.Float64bits(got.end) != math.Float64bits(want.end) {
						t.Errorf("lane %s against %s at %s: extent %+v, want %+v", lane.ID, other.ID, node, got, want)
					}
					return
				}
				merges++
				if got.start < want.start || got.end > want.end {
					t.Errorf("lane %s against %s at %s: extent %+v is not inside the isotropic extent %+v", lane.ID, other.ID, node, got, want)
				}
				if got.end-got.start < want.end-want.start {
					shorter++
				}
			})
			if merges == 0 || shorter != merges {
				t.Errorf("%d of %d extents of merge pairs are shorter than the isotropic extent", shorter, merges)
			}
		})
	}
}

// checkShapeMergeCells fails the test when the cells of a lane differ from
// the cells with the isotropic extents, other than by fewer junction
// resources.
func checkShapeMergeCells(t *testing.T, lane string, on, off *laneCells) {
	t.Helper()
	if on.tail != off.tail || on.fromTail != off.fromTail || on.front != off.front || on.retention != off.retention || on.count() != off.count() {
		t.Errorf("lane %s: cells %d with tails %v, %v, front %v, retention %v; want %d with %v, %v, %v, %v",
			lane, on.count(), on.tail, on.fromTail, on.front, on.retention, off.count(), off.tail, off.fromTail, off.front, off.retention)
		return
	}
	for cell := range on.count() {
		got, want := on.cell(cell), off.cell(cell)
		others := func(resources []resource) []resource {
			return slices.DeleteFunc(slices.Clone(resources), func(r resource) bool { return r.kind == junctionResource })
		}
		if !slices.Equal(others(got), others(want)) {
			t.Errorf("lane %s cell %d: resources %v, want %v other than junctions", lane, cell, got, want)
		}
		for _, r := range got {
			if r.kind == junctionResource && !slices.Contains(want, r) {
				t.Errorf("lane %s cell %d: junction %s that the cell does not hold with the isotropic extents", lane, cell, r.id)
			}
		}
	}
}

// TestShapeMergesCheck checks the threshold of a pod pair on the lanes of
// a merge pair of largeOracleNetwork: the side-by-side distance of the
// classes of the two pods, less separationTolerance, for each pairing of
// classes, in both orders, with Group in place of Express. A following
// pair on one lane, and a through pair, keep 12 m, or 20 m with a large
// pod.
func TestShapeMergesCheck(t *testing.T) {
	t.Parallel()
	network := largeOracleNetwork(t, true)
	prepared, err := PrepareNetwork(network)
	if err != nil {
		t.Fatal(err)
	}
	lane := func(id string) separationKey {
		return laneKey(slices.IndexFunc(network.Lanes, func(lane Lane) bool { return lane.ID == id }))
	}
	a, b, exit := lane("a"), lane("b"), lane("exit")
	observe := func(merges *depotLateral, first, second VehicleClass, keys [2]separationKey, gap float64) error {
		pods := []Pod{{ID: "p", Class: first}, {ID: "q", Class: second, Position: Point{Y: gap}}}
		o := SafetyObservation{Pods: pods, bound: slices.Clone(pods), keys: keys[:], merges: merges, OrderContract: ExpressOrderContract}
		_, failure := o.Check()
		return failure
	}
	for _, test := range []struct {
		first, second VehicleClass
		keys          [2]separationKey
		want          float64
	}{
		{CompactClass, LegacyClass, [2]separationKey{a, b}, 3.1},
		{CompactClass, ExpressClass, [2]separationKey{a, b}, 3.55},
		{ExpressClass, CompactClass, [2]separationKey{a, b}, 3.55},
		{LegacyClass, GroupClass, [2]separationKey{b, a}, 3.55},
		{GroupClass, CompactClass, [2]separationKey{b, a}, 3.55},
		{ExpressClass, ExpressClass, [2]separationKey{a, b}, 4.0},
		{GroupClass, ExpressClass, [2]separationKey{a, b}, 4.0},
		{ExpressClass, GroupClass, [2]separationKey{b, a}, 4.0},
		{CompactClass, CompactClass, [2]separationKey{a, a}, Clearance},
		{ExpressClass, ExpressClass, [2]separationKey{b, b}, largeClearance},
		{CompactClass, CompactClass, [2]separationKey{a, exit}, Clearance},
		{CompactClass, ExpressClass, [2]separationKey{exit, b}, largeClearance},
	} {
		at := test.want - separationTolerance
		if failure := observe(prepared.merges, test.first, test.second, test.keys, at); failure != nil {
			t.Errorf("%s and %s on %v at %g m: %v", test.first, test.second, test.keys, at, failure)
		}
		if failure := observe(prepared.merges, test.first, test.second, test.keys, test.want-0.01); failure == nil {
			t.Errorf("%s and %s on %v at %g m: no error", test.first, test.second, test.keys, test.want-0.01)
		}
	}
}

// TestShapeMergesDebrisBlocksPartner checks the debris footprint of a lane
// of a large merge pair (section 7.2 of the incident suspension contract),
// on the merge of largeOracleNetwork. Debris on lane a, 100 m before the
// node, is about 10.5 m from lane b, outside the junction zone of the
// pair. The footprint holds each cell of lane b that comes closer than
// Clearance to the debris, and no other cell of lane b.
func TestShapeMergesDebrisBlocksPartner(t *testing.T) {
	t.Parallel()
	network := largeOracleNetwork(t, true)
	s, err := NewFleet(network, []Placement{{ID: "p1", StationID: "dest", BerthID: "dest-01"}})
	if err != nil {
		t.Fatal(err)
	}
	const from, to = oracleLaneLength - 100, oracleLaneLength - 97
	a, b := s.graph.lanes["a"], s.graph.lanes["b"]
	if !s.merges.mergePartner(a, b) {
		t.Fatal("lanes a and b form no merge pair")
	}
	footprint := s.debrisFootprint(a, from, to)
	if slices.ContainsFunc(footprint, func(r resource) bool { return r.kind == junctionResource }) {
		t.Fatal("the footprint holds a junction, so the test does not need the partner")
	}
	debris := polylinePiece(network.Polyline(network.Lanes[a]), from, to)
	cells, length := s.laneCells["b"], s.graph.lengths[b]
	near := 0
	for cell := range cells.count() {
		piece := polylinePiece(network.Polyline(network.Lanes[b]), cellOffset(cell, cells.count(), length), cellOffset(cell+1, cells.count(), length))
		nearby := bankPathsClose(debris, piece, Clearance)
		for _, r := range cells.cell(cell) {
			if held := slices.Contains(footprint, r); held != nearby && r.kind == trackResource {
				t.Errorf("cell %d of lane b resource %v in the footprint %v, want %v", cell, r, held, nearby)
			}
		}
		if nearby {
			near++
		}
	}
	if near == 0 {
		t.Fatal("no cell of lane b is near the debris")
	}
}

// TestShapeMergesTwoStreams runs two streams into the merge of
// largeOracleNetwork for 180 s: three Express pods on one lane and three
// Compact pods on the other, each way round. At each tick, Check and the
// body-and-ownership oracle pass. The pods of the two lanes come closer
// than Clearance, which the isotropic rule of 20 m does not allow, and
// each pod leaves the lanes of the pair.
func TestShapeMergesTwoStreams(t *testing.T) {
	t.Parallel()
	for _, classes := range [][2]VehicleClass{{ExpressClass, CompactClass}, {CompactClass, ExpressClass}} {
		t.Run(fmt.Sprintf("%s on a then %s on b", classes[0], classes[1]), func(t *testing.T) {
			t.Parallel()
			l := oracleLayout{converging: true, large: true}
			pods := append(l.stream("a", 50, corridorQueueGap, classes[0]), l.stream("b", 60, corridorQueueGap, classes[1])...)
			s := restoreMergeOracle(t, largeOracleNetwork(t, true), pods)
			run := runMergeOracle(s, 180)
			if run.oracle != nil {
				t.Error(run.oracle)
			}
			if run.check != nil {
				t.Error(run.check)
			}
			t.Logf("closest pods of the pair %.3f m", run.closest)
			if !(run.closest < Clearance) {
				t.Errorf("closest pods of the pair %.3f m, want less than %v m, so that the run tests the rule", run.closest, Clearance)
			}
			for _, v := range s.vehicles {
				if v.Pod.LaneID == "a" || v.Pod.LaneID == "b" {
					t.Errorf("pod %s is still on lane %s", v.Pod.ID, v.Pod.LaneID)
				}
			}
		})
	}
}
