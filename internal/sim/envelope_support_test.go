package sim

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"testing"
)

// This file tests the envelope of a network with ShapeClearance: the
// reach bound of each lane, the local front, and the small-pod rules of
// section 3.1 of docs/design/shared-track-preset.md (section 7 of that
// design is the test plan). The containment tests use the containment
// driver of the shared-track study (see sharedContainment) on small
// networks. The study runs the same driver on the London networks.

// withShapeClearance returns a copy of a network with ShapeClearance set
// or clear.
func withShapeClearance(n Network, field bool) Network {
	n = n.clone()
	n.ShapeClearance = field
	return n
}

// supportBayNetwork returns the compact station of the design table with
// ShapeClearance. Its bays are maneuver group lanes shorter than the disk
// of the small body, so the support of a small pod at a berth reaches a
// second node through a group lane.
func supportBayNetwork() Network {
	return withShapeClearance(compactStationNetwork(9.5, 40), true)
}

// supportRun checks a containment run: no failure of the proposal, and at
// least the minimum of each count.
func supportRun(t *testing.T, c *sharedContainment, minimum map[string]int) {
	t.Helper()
	if f := c.failures(); f > 0 {
		for key, value := range c.counts {
			if sharedFailure(key) {
				t.Errorf("%s: %s: %d, for example %v", c.name, key, value, c.examples[key])
			}
		}
	}
	for key, want := range minimum {
		if got := c.counts[key]; got < want {
			t.Errorf("%s: %s: %d, want at least %d", c.name, key, got, want)
		}
	}
	if t.Failed() {
		c.log(t)
	}
}

// TestEnvelopeSupportTurnBack is the containment test of section 3.1 on
// the four turn-back shapes of the disk study, with small and with large
// lanes: oriented bodies along each lane and across each join, on routes
// that come back beside the first lane. A large body has a support of at
// most two lanes, and each support lane is in the local oracle and in the
// index, also at the route distances and the published positions shifted
// by two restoreTolerance steps each way.
func TestEnvelopeSupportTurnBack(t *testing.T) {
	t.Parallel()
	for shape := range diskShapes {
		for _, kind := range []diskStudyKind{diskSmall, diskLarge} {
			classes := ClassSet(0)
			if kind == diskLarge {
				classes = shapeStudyClasses
			}
			n, _, _ := diskStudyTurnBack(shape, classes)
			n.ShapeClearance = true
			c := newSharedContainment(t, fmt.Sprintf("turn-back %s, %s lanes", diskStudyShapeNames[shape], diskStudyKindNames[kind]), n)
			c.reference = true
			route := make([]int, len(n.Lanes))
			for index := range route {
				route[index] = index
			}
			minimum := map[string]int{}
			for _, k := range []diskStudyKind{diskSmall, diskLarge} {
				if k.allows(classes) {
					c.drive(route, k, 0, len(route))
					minimum["poses "+diskStudyKindNames[k]] = 1
				}
			}
			if kind == diskLarge {
				minimum["support of 2 lanes, gate"] = 1
				minimum["support of 2 lanes, large grown"] = 1
			}
			supportRun(t, c, minimum)
			if more := c.counts["support of more than 2 lanes with no maneuver group lane, gate"] + c.counts["support of more than 2 lanes with a maneuver group lane, gate"]; more > 0 {
				t.Errorf("%s: the support of the gate body meets more than one node at %d body poses", c.name, more)
			}
		}
	}
}

// TestEnvelopeSupportReturn checks the route of the loop shape that comes
// back beside its first lane on another plane. The support of a body on
// the first lane is the first lane only, and A1 declares the lane that
// comes back separated. So the local oracle and the oracle without
// ShapeClearance both exempt a pod on that lane. With the lane that comes
// back in the group of the first lane, they do not.
func TestEnvelopeSupportReturn(t *testing.T) {
	t.Parallel()
	for _, same := range []bool{false, true} {
		n, position, onBack := diskStudyTurnBack(diskLoop, shapeStudyClasses)
		back := len(n.Lanes) - 1
		if same {
			n.Lanes[back].SeparationGroup = n.Lanes[0].SeparationGroup
		}
		for _, field := range []bool{false, true} {
			c := newSharedContainment(t, fmt.Sprintf("loop, same group %v, shapeClearance %v", same, field), withShapeClearance(n, field))
			route := make([]int, len(n.Lanes))
			for index := range route {
				route[index] = index
			}
			lanes := make([]Lane, len(route))
			copy(lanes, c.s.network.Lanes)
			first := &vehicle{Pod: Pod{ID: "first", Class: ExpressClass, Activity: Traveling, LaneID: lanes[0].ID, LaneDistance: position}, originReleased: true}
			c.s.setVehicleRoute(first, lanes)
			other := &vehicle{Pod: Pod{ID: "back", Class: ExpressClass, Activity: Traveling, LaneID: lanes[back].ID, LaneDistance: onBack}, originReleased: true}
			c.s.setVehicleRoute(other, lanes[back:])
			a := c.s.appendVehicleSafetyLocations(nil, first, position)
			b := c.s.appendVehicleSafetyLocations(nil, other, onBack)
			reference, heading := c.pointAt(route[0], position)
			lo, hi, _, _ := c.support(route, 0, position, func(q Point) bool {
				dx, dy := q.X-reference.X, q.Y-reference.Y
				x, y := dx*math.Cos(heading)+dy*math.Sin(heading), -dx*math.Sin(heading)+dy*math.Cos(heading)
				body := classShapeBody(ExpressClass)
				return x >= -body.rear && x <= body.front && math.Abs(y) <= body.halfWidth
			})
			if lo != 0 || hi != 0 {
				t.Errorf("%s: the support of the body at %g m is lanes %d to %d, want the first lane only", c.name, position, lo, hi)
			}
			if got := envelopeLocationsSeparated(a, b); got == same {
				t.Errorf("%s: envelopes %v and %v separated %v", c.name, a, b, got)
			}
		}
	}
}

// TestEnvelopeSupportCurves is the containment test on the curves network
// of TestEnvelopeReachCurves with ShapeClearance. The index without the
// seed misses planes at the start of lane short, and the index of each
// lane misses planes of the oracle with the largest front of the route.
func TestEnvelopeSupportCurves(t *testing.T) {
	t.Parallel()
	curves := withShapeClearance(envelopeReachCurves(t), true)
	c := newSharedContainment(t, "curves", curves)
	c.reference = true
	for lane := range curves.Lanes {
		for _, kind := range []diskStudyKind{diskSmall, diskLarge} {
			if kind.allows(curves.Lanes[lane].VehicleClasses) {
				c.lane(lane, kind, c.behind+1, c.bounds[lane]+60, 4)
			}
		}
	}
	c.origins(diskLarge)
	c.stationary(diskLarge)
	supportRun(t, c, map[string]int{
		"poses small":                       1,
		"poses large":                       1,
		"poses with the origin held, large": 1,
		"stationary berths large":           1,
		"support of 2 lanes, gate":          1,
	})
}

// TestEnvelopeSupportMixedSmall is the containment test of the small pod
// of a mixed fleet. On the hook network, the support of a small body
// passes the end of a curved lane with no geometry tail more than 6 m
// ahead of the reference, and rule 1 holds the next lane. On the compact
// station, the support of a small body passes maneuver group lanes,
// traveling, before its origin release, and at a berth, and rule 2 holds
// the lanes after them. Without the rules, the oracle misses support lanes.
func TestEnvelopeSupportMixedSmall(t *testing.T) {
	t.Parallel()
	t.Run("hook", func(t *testing.T) {
		t.Parallel()
		c := newSharedContainment(t, "hook", sharedHookNetwork())
		c.reference = true
		c.drive([]int{0, 1, 2, 3}, diskSmall, 0, 4)
		supportRun(t, c, map[string]int{
			"poses small": 1,
			"control, support miss: oracle without the small-pod rules, small, curve":         1,
			"control, support miss: oracle without the small-pod rules, small grown, curve":   1,
			"control, support miss: oracle with the anchor front 6 m, small grown, curve":     1,
			"earlier oracle, support miss: oracle with the route maximum, small grown, curve": 1,
		})
	})
	t.Run("bays", func(t *testing.T) {
		t.Parallel()
		n := supportBayNetwork()
		c := newSharedContainment(t, "bays", n)
		c.reference = true
		for lane := range n.Lanes {
			c.lane(lane, diskSmall, c.behind+1, c.bounds[lane]+60, 2)
		}
		c.origins(diskSmall)
		c.stationary(diskSmall)
		supportRun(t, c, map[string]int{
			"poses small":                       1,
			"poses with the origin held, small": 1,
			"stationary berths small":           1,
			"support of more than 2 lanes with a maneuver group lane, small grown":                     1,
			"control, support miss: oracle without the small-pod rules, small grown, maneuver group":   1,
			"earlier oracle, support miss: oracle with the route maximum, small grown, maneuver group": 1,
			"control, support miss: oracle without the small-pod rules at a berth, small grown":        1,
			"earlier oracle, support miss: oracle at a berth without ShapeClearance, small grown":      1,
			"stationary support with more than one node, small grown":                                  1,
		})
	})
	// The shared lanes of london-full-shared-express, where Compact pods
	// and Express pods meet. The preset loads a network of 9,089 lanes, so
	// this case runs only in the long run, without -race.
	t.Run("london-full-shared-express", func(t *testing.T) {
		t.Parallel()
		if testing.Short() || raceEnabled {
			t.Skip("the preset network is large")
		}
		n := sharedExpressNetwork(t)
		c := newSharedContainment(t, "london-full-shared-express", n)
		c.reference = true
		for lane, l := range n.Lanes {
			if sharedExpressLinkLanes[l.ID] {
				c.lane(lane, diskSmall, c.behind+1, c.bounds[lane]+60, 2)
			}
		}
		supportRun(t, c, map[string]int{"poses small": 1})
		t.Logf("%v", c.counts)
	})
}

// TestEnvelopeSupportOneNode guards facts F1 and F2 of section 3.1: no lane
// that admits a large class fits in the disk of radius 5.404 m of the
// proof. Half the largest distance between two points of the lane is at
// most the radius of its smallest enclosing circle, so the test requires
// that half distance to be larger than the radius.
func TestEnvelopeSupportOneNode(t *testing.T) {
	t.Parallel()
	const radius = 5.404
	gate := shapeBody{rear: 5, front: 5, halfWidth: 1.25}
	for _, body := range []shapeBody{gate, classShapeBody(ExpressClass)} {
		if r := math.Hypot(max(body.rear, body.front), body.halfWidth) + sharedTolerance; r > radius {
			t.Fatalf("body %+v with the guideway tolerance has the radius %g m, over %g m", body, r, radius)
		}
	}
	express := expressNetworkFixture(t)
	networks := map[string]Network{"curves": envelopeReachCurves(t), "london-full-express": express, "london-full-shared-express": sharedExpressNetwork(t)}
	maps.Copy(networks, gateOffRefusedNetworks(t, express))
	maps.Copy(networks, scenarioNetworks(t))
	for shape := range diskShapes {
		n, _, _ := diskStudyTurnBack(shape, shapeStudyClasses)
		networks["turn-back "+diskStudyShapeNames[shape]] = n
	}
	large := 0
	for name, n := range networks {
		positions := indexNodePositions(n.Nodes)
		for _, lane := range n.Lanes {
			if !largeClassSet(lane.VehicleClasses) {
				continue
			}
			large++
			points := lanePointsBetween(lane, positions[lane.From], positions[lane.To], make([]Point, 0, 65))
			diameter := 0.0
			for i := range points {
				for j := i + 1; j < len(points); j++ {
					diameter = max(diameter, pointDistance(points[i], points[j]))
				}
			}
			if diameter/2 <= radius {
				t.Errorf("%s: large lane %q fits in a disk of radius %g m: its points are at most %g m apart", name, lane.ID, radius, diameter)
			}
		}
	}
	if large == 0 {
		t.Fatal("no large lane")
	}
}

// TestEnvelopeSupportChain guards the chain row of section 3.1: a lane
// with a reduced retention is longer than the radius 4.7269 m of the disk
// of the small body, and no such lane is wholly in that disk about a point
// of a lane next to it, within the longest arc of one lane in the disk.
func TestEnvelopeSupportChain(t *testing.T) {
	t.Parallel()
	express := expressNetworkFixture(t)
	networks := map[string]Network{"london-full-express": express, "london-full-shared-express": sharedExpressNetwork(t), "bays": supportBayNetwork(), "tall bays": tallBayNetwork(3, 4)}
	maps.Copy(networks, scenarioNetworks(t))
	arc := (2 + math.Sqrt2) * sharedSmallRadius
	checked := 0
	for name, n := range networks {
		tails := indexGeometryTails(n)
		retention := newLaneRetention(n, tails)
		positions := indexNodePositions(n.Nodes)
		points := make([][]Point, len(n.Lanes))
		for index, lane := range n.Lanes {
			points[index] = lanePointsBetween(lane, positions[lane.From], positions[lane.To], make([]Point, 0, 65))
		}
		for index, lane := range n.Lanes {
			if retention.of(lane.ID) <= 0 {
				continue
			}
			checked++
			if length := polylineLength(points[index]); length <= sharedSmallRadius {
				t.Errorf("%s: lane %q has a reduced retention and is %g m long", name, lane.ID, length)
			}
			for other, next := range n.Lanes {
				if other == index {
					continue
				}
				var node string
				switch {
				case next.To == lane.From:
					node = lane.From
				case next.From == lane.To:
					node = lane.To
				default:
					continue
				}
				// Points of the other lane within the longest arc of the
				// node, each 0.25 m.
				arcs := points[other]
				length := polylineLength(arcs)
				for along := 0.0; along <= length; along += 0.25 {
					at := polylinePoint(arcs, along)
					if pointDistance(at, positions[node]) > arc {
						continue
					}
					if !slices.ContainsFunc(points[index], func(p Point) bool { return pointDistance(p, at) > sharedSmallRadius }) {
						t.Errorf("%s: lane %q with a reduced retention is wholly in the disk about %v on lane %q", name, lane.ID, at, next.ID)
					}
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no lane with a reduced retention")
	}
}

// polylinePoint returns the point of a polyline at a distance from its
// start.
func polylinePoint(points []Point, distance float64) Point {
	for index := 1; index < len(points); index++ {
		a, b := points[index-1], points[index]
		length := pointDistance(a, b)
		if distance <= length || index == len(points)-1 {
			f := 0.0
			if length > 0 {
				f = min(1, max(0, distance/length))
			}
			return Point{X: a.X + f*(b.X-a.X), Y: a.Y + f*(b.Y-a.Y)}
		}
		distance -= length
	}
	return points[len(points)-1]
}

// TestEnvelopeFrontRuleOne pins the front of rule 1: a curved lane with no
// geometry tail gets min(6 / (c (1 - conflictSlack)), 16.2 m) when the
// network sets ShapeClearance, and the front of the tail index otherwise.
// The lane cells and the envelope reach take the same front.
func TestEnvelopeFrontRuleOne(t *testing.T) {
	t.Parallel()
	if want := math.Ceil((2+math.Sqrt2)*sharedSmallRadius*10) / 10; smallFrontCap != want || smallFrontCap != sharedSmallCap {
		t.Fatalf("smallFrontCap %g m, want %g m", smallFrontCap, want)
	}
	if body := classShapeBody(CompactClass); math.Abs(math.Hypot(max(body.rear, body.front), body.halfWidth)+sharedTolerance-sharedSmallRadius) > 1e-12 {
		t.Fatalf("the grown small body %+v is not in the disk of radius %g m", body, sharedSmallRadius)
	}
	hook := sharedHookNetwork()
	lane := func(id, from, to string, control *Point) Lane {
		return Lane{ID: id, From: from, To: to, SpeedLimit: 14, SeparationGroup: id, Control: control}
	}
	// Three curves from (0, 100) to (40, 100): c is about 0.70, 0.24, and
	// 0, and one straight lane.
	hook.Nodes = append(hook.Nodes, Node{ID: "p", Position: Point{Y: 100}}, Node{ID: "q", Position: Point{X: 40, Y: 100}})
	hook.Lanes = append(hook.Lanes,
		lane("gentle", "p", "q", &Point{X: 20, Y: 120}),
		lane("steep", "p", "q", &Point{X: 20, Y: 180}),
		lane("level", "p", "q", nil))
	for _, field := range []bool{false, true} {
		n := withShapeClearance(hook, field)
		s := diskStudyPrepare(t, n)
		reach := newEnvelopeReach(s.network, s.tails)
		positions := indexNodePositions(n.Nodes)
		for index, lane := range n.Lanes {
			c := sharedChordProjection(lanePointsBetween(lane, positions[lane.From], positions[lane.To], make([]Point, 0, 65)))
			want := 0.0
			if field && lane.Control != nil {
				want = smallFrontCap
				if c > 0 {
					want = min(largeEnvelopeRadius/(c*(1-conflictSlack)), smallFrontCap)
				}
			}
			if got := s.laneCells[lane.ID].front; got != want {
				t.Errorf("shapeClearance %v: lane %q with c %.2f: cells front %g m, want %g m", field, lane.ID, c, got, want)
			}
			if got := reach.graph.fronts[index]; got != want {
				t.Errorf("shapeClearance %v: lane %q with c %.2f: reach front %g m, want %g m", field, lane.ID, c, got, want)
			}
			// A Simulation literal has no lane cells, so routeBlocks makes
			// them.
			literal := &Simulation{networkIndexes: &networkIndexes{network: n}}
			if blocks, _ := literal.routeBlocks([]Lane{lane}); blocks.lanes[0].cells.front != want {
				t.Errorf("shapeClearance %v: lane %q with c %.2f: front of new cells %g m, want %g m", field, lane.ID, c, blocks.lanes[0].cells.front, want)
			}
		}
		if field {
			for id, want := range map[string]float64{"hook": smallFrontCap, "steep": smallFrontCap} {
				if got := s.laneCells[id].front; got != want {
					t.Errorf("lane %q: front %g m, want the cap %g m", id, got, want)
				}
			}
			if got := s.laneCells["gentle"].front; got <= largeEnvelopeRadius || got >= smallFrontCap {
				t.Errorf("lane gentle: front %g m, want between %g m and the cap", got, largeEnvelopeRadius)
			}
		}
	}
	// A lane with a geometry tail keeps the front of the tail index.
	curves := withShapeClearance(envelopeReachCurves(t), true)
	s := diskStudyPrepare(t, curves)
	if got, want := s.laneCells["bend"].front, s.tails["bend"].front; got != want {
		t.Errorf("lane bend: front %g m, want the front of the tail index %g m", got, want)
	}
}

// TestEnvelopeRuleTwoPairOrders checks the plane exemption of a mixed pair
// with rule 2, in both orders of the pods. A Compact pod at a bay berth of
// the compact station holds the cluster of the bay lanes, which includes
// lane leave at the receiving node. An Express pod on lane leave is then
// on a plane of that envelope, so the pair is not exempt. Without
// ShapeClearance, the envelope of the berth holds only the lanes at the
// berth node, and the pair is exempt.
func TestEnvelopeRuleTwoPairOrders(t *testing.T) {
	t.Parallel()
	for _, field := range []bool{false, true} {
		n := withShapeClearance(compactStationNetwork(9.5, 40), field)
		for index := range n.Lanes {
			n.Lanes[index].SeparationGroup = "g-" + n.Lanes[index].ID
		}
		for index := range n.Stations {
			for berth := range n.Stations[index].Berths {
				n.Stations[index].Berths[berth].SeparationGroup = "g-" + n.Stations[index].Berths[berth].ID
			}
		}
		s := diskStudyPrepare(t, n)
		small := &vehicle{Pod: Pod{ID: "small", Class: CompactClass, Activity: Idle, BerthID: "bay-1"}, originReleased: true}
		large := &vehicle{Pod: Pod{ID: "large", Class: ExpressClass, Activity: Traveling, LaneID: "leave", LaneDistance: 1, Position: Point{X: 1}}, originReleased: true}
		s.setVehicleRoute(large, []Lane{n.Lanes[networkLaneIndex(n, "leave")]})
		envelopes := map[string][]SafetyLocation{
			"small": s.appendVehicleSafetyLocations(nil, small, 0),
			"large": s.appendVehicleSafetyLocations(nil, large, 1),
		}
		if want := referenceVehicleSafetyLocations(s, small, 0); !slices.Equal(envelopes["small"], want) {
			t.Errorf("shapeClearance %v: berth envelope %v, reference %v", field, envelopes["small"], want)
		}
		locations := map[string]SafetyLocation{"small": s.berthSafety["bay-1"], "large": s.laneSafety["leave"]}
		for _, order := range [][2]*vehicle{{small, large}, {large, small}} {
			o := SafetyObservation{}
			for _, v := range order {
				o.Pods = append(o.Pods, v.Pod)
				o.Locations = append(o.Locations, locations[v.Pod.ID])
				o.envelopes = append(o.envelopes, envelopes[v.Pod.ID])
			}
			o.bound = slices.Clone(o.Pods)
			_, err := o.checkSeparation()
			if (err != nil) != field {
				t.Errorf("shapeClearance %v, pods %s and %s: %v", field, order[0].Pod.ID, order[1].Pod.ID, err)
			}
		}
	}
}

// reachBoundNetworks returns the networks of the tests of the reach bound,
// with ShapeClearance, and whether the test samples their lanes. The
// network with Express on the routes of Waterloo and Bank has a large
// front, so its bound of the whole network is more than the bound of most
// lanes.
func reachBoundNetworks(t *testing.T) (map[string]Network, map[string]bool) {
	t.Helper()
	express := expressNetworkFixture(t)
	networks := map[string]Network{
		"curves": envelopeReachCurves(t), "hook": sharedHookNetwork(), "bays": supportBayNetwork(),
		"curves with gap": envelopeReachSubSlackCurves(t, envelopeReachSlack/2), "london-full-express": express,
	}
	maps.Copy(networks, gateOffRefusedNetworks(t, express))
	maps.Copy(networks, scenarioNetworks(t))
	for shape := range diskShapes {
		n, _, _ := diskStudyTurnBack(shape, shapeStudyClasses)
		networks["turn-back "+diskStudyShapeNames[shape]] = n
	}
	sampled := make(map[string]bool)
	for name, n := range networks {
		networks[name] = withShapeClearance(n, true)
		sampled[name] = len(n.Lanes) > 1000
	}
	return networks, sampled
}

// spanSet returns the location numbers of a span as a set, or the numbers
// of closed when it is not nil.
func spanSet(span *reachSpan, closed []int) map[int]bool {
	if closed == nil {
		closed = span.locations
	}
	set := make(map[int]bool, len(closed))
	for _, number := range closed {
		set[number] = true
	}
	return set
}

// spanMiddles returns the middle of each part of a lane between the bounds
// of the spans of each index, so that each middle is inside one span of
// each index.
func spanMiddles(length float64, indexes ...[]reachSpan) []float64 {
	cuts := []float64{0, length}
	for _, spans := range indexes {
		for _, span := range spans {
			cuts = append(cuts, span.from, span.to)
		}
	}
	slices.Sort(cuts)
	cuts = slices.Compact(cuts)
	var middles []float64
	for index := 1; index < len(cuts); index++ {
		middles = append(middles, (cuts[index-1]+cuts[index])/2)
	}
	if len(middles) == 0 {
		middles = append(middles, 0)
	}
	return middles
}

// TestEnvelopeReachChangeFromBuilt compares the index of a network with
// ShapeClearance with the index of the same network without it, which is
// the index before slice 1. It lists the lanes with changes. The expected
// additions are the lanes of a cluster where the index holds a group lane
// (rule 2), the lanes ahead within the anchor front where that front is
// more than the earlier front (rule 1), the planes of the anchors on the
// next lanes within the slack of an end of the lane, and the locations of
// an item that ends within the slack after the end of the item of the
// built index. The expected
// removals are the lanes ahead past the anchor front. Each other change
// fails the test.
func TestEnvelopeReachChangeFromBuilt(t *testing.T) {
	t.Parallel()
	networks, sampled := reachBoundNetworks(t)
	every := 23
	if testing.Short() {
		every = 97
	}
	minimum := map[string][4]int{"curves": {0, 0, 1, 1}, "hook": {0, 1, 0, 0}, "bays": {1, 0, 0, 1}, "london-full-express": {1, 0, 1, 1}}
	for name, n := range networks {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			tails := indexGeometryTails(n)
			reach := newEnvelopeReach(n, tails)
			built := newEnvelopeReach(withShapeClearance(n, false), tails)
			var walk reachWalk
			// held reports whether an item holds a location at a position
			// that is not a bound of an item.
			held := func(items []reachItem, number int, position float64) bool {
				return slices.ContainsFunc(items, func(item reachItem) bool {
					return item.location == number && item.from < position && position < item.to
				})
			}
			var clusters, ahead, removed, near int
			for lane := range n.Lanes {
				if sampled[name] && lane%every != 0 {
					continue
				}
				front, length := reach.graph.anchorFront(lane), reach.graph.lengths[lane]
				base := reach.graph.anchorItems(&walk, lane, nil)
				old := built.graph.laneItems(&walk, lane, nil)
				var addedCluster, addedAhead, removedAhead, addedNear bool
				for _, position := range spanMiddles(length, reach.laneSpans(lane), built.laneSpans(lane)) {
					span := reach.laneAt(lane, position)
					a, b := spanSet(span, reach.closure(span.locations)), spanSet(built.laneAt(lane, position), nil)
					for number := range a {
						if b[number] {
							continue
						}
						switch {
						case held(base, number, position) && front > built.graph.front:
							addedAhead = true
						case !slices.Contains(span.locations, number):
							addedCluster = true
						case position <= envelopeReachSlack || position >= length-envelopeReachSlack:
							addedNear = true
						case held(base, number, position) && slices.ContainsFunc(old, func(item reachItem) bool {
							return item.location == number && item.from <= position+envelopeReachSlack && position-envelopeReachSlack <= item.to
						}):
							// The item of the index ends the slack after the
							// item of the built index.
							addedNear = true
						default:
							t.Fatalf("lane %q at %g m: unexpected addition %+v", n.Lanes[lane].ID, position, reach.locations[number])
						}
					}
					for number := range b {
						if a[number] {
							continue
						}
						// A removed location is held only by items of the
						// ahead walk, which end at +Inf.
						onlyAhead := !slices.ContainsFunc(old, func(item reachItem) bool {
							return item.location == number && item.from < position && position < item.to && !math.IsInf(item.to, 1)
						})
						if !onlyAhead || front >= built.graph.front {
							t.Fatalf("lane %q at %g m: unexpected removal %+v", n.Lanes[lane].ID, position, built.locations[number])
						}
						removedAhead = true
					}
				}
				clusters += boolInt(addedCluster)
				ahead += boolInt(addedAhead)
				removed += boolInt(removedAhead)
				near += boolInt(addedNear)
			}
			t.Logf("%s: lanes with additions from clusters %d, with additions ahead %d, with removals %d, with additions within the slack %d", name, clusters, ahead, removed, near)
			if want, ok := minimum[name]; ok && (clusters < want[0] || ahead < want[1] || removed < want[2] || near < want[3]) {
				t.Errorf("%s: changes %v, want at least %v", name, [4]int{clusters, ahead, removed, near}, want)
			}
		})
	}
}

// driveEnvelopes calls check with a pod of each class on a route through
// each sampled lane of a prepared network, at positions along the lane,
// with the origin held and released, and with a pod at each berth. The
// positions include the start and the end of the lane, a position within
// the slack of its start, and the positions at which a front bound of the
// route reaches the start of a route lane, each also shifted by two
// restoreTolerance steps each way.
func driveEnvelopes(s *Simulation, sampled func(index int, lane Lane) bool, check func(v *vehicle, distance float64)) {
	lanes := s.network.Lanes
	in, out := make(map[string][]int), make(map[string][]int)
	berths := make(map[string]Berth)
	for index, lane := range lanes {
		in[lane.To] = append(in[lane.To], index)
		out[lane.From] = append(out[lane.From], index)
	}
	for _, station := range s.network.Stations {
		for _, berth := range station.Berths {
			berths[berth.Node] = berth
			check(&vehicle{Pod: Pod{ID: "probe", Class: CompactClass, Activity: Idle, BerthID: berth.ID}, originReleased: true}, 0)
		}
	}
	for index, lane := range lanes {
		if !sampled(index, lane) {
			continue
		}
		route := []int{index}
		if previous := slices.DeleteFunc(slices.Clone(in[lane.From]), func(other int) bool { return other == index }); len(previous) > 0 {
			route = append([]int{previous[0]}, route...)
		}
		for len(route) < 6 {
			next := slices.DeleteFunc(slices.Clone(out[lanes[route[len(route)-1]].To]), func(other int) bool { return slices.Contains(route, other) })
			if len(next) == 0 {
				break
			}
			route = append(route, next[0])
		}
		at := slices.Index(route, index)
		path := make([]Lane, len(route))
		for k, lane := range route {
			path[k] = lanes[lane]
		}
		for _, class := range []VehicleClass{CompactClass, ExpressClass} {
			v := &vehicle{Pod: Pod{ID: "probe", Class: class, Activity: Traveling, LaneID: lane.ID}, origin: berths[path[0].From]}
			s.setVehicleRoute(v, path)
			start, end := v.blocks.lanes[at].start, v.blocks.lanes[at+1].start
			distances := []float64{start, start + envelopeReachSlack/2, (start + end) / 2, end}
			for k := range route {
				for _, front := range []float64{largeEnvelopeRadius, smallFrontCap, v.blocks.lanes[k].cells.front} {
					distances = append(distances, v.blocks.lanes[k].start-front)
				}
			}
			for _, distance := range distances {
				if distance < start || distance > end {
					continue
				}
				for _, shift := range []float64{0, -2 * restoreTolerance, 2 * restoreTolerance} {
					for _, released := range []bool{false, true} {
						v.originReleased = released || v.origin.ID == ""
						v.Pod.LaneDistance = max(0, distance+shift-start)
						check(v, distance+shift)
					}
				}
			}
		}
	}
}

// envelopeParityNetworks returns the networks of the parity tests. The
// small networks have curved lanes with no geometry tail and maneuver
// groups.
func envelopeParityNetworks(t *testing.T) map[string]Network {
	t.Helper()
	express := expressNetworkFixture(t)
	networks := map[string]Network{
		"curves": envelopeReachCurves(t), "hook": sharedHookNetwork(), "bays": supportBayNetwork(), "tall bays": tallBayNetwork(3, 4),
		"london-full-express": express, "london-full-shared-express": sharedExpressNetwork(t),
	}
	maps.Copy(networks, gateOffRefusedNetworks(t, express))
	maps.Copy(networks, scenarioNetworks(t))
	return networks
}

// TestEnvelopeFieldOffMatchesBuilt is the field-off test of section 7:
// without ShapeClearance, the oracle and the reference construction give
// the envelopes of the construction before slice 1, in the same order (see
// builtVehicleSafetyLocations). The networks include curved lanes with no
// geometry tail and maneuver groups, to which the small-pod rules would
// apply with the field.
func TestEnvelopeFieldOffMatchesBuilt(t *testing.T) {
	t.Parallel()
	every := 97
	if testing.Short() {
		every = 401
	}
	for name, n := range envelopeParityNetworks(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := diskStudyPrepare(t, withShapeClearance(n, false))
			count := 0
			driveEnvelopes(s, func(index int, lane Lane) bool {
				return len(n.Lanes) < 1000 || index%every == 0
			}, func(v *vehicle, distance float64) {
				count++
				got, want := s.appendVehicleSafetyLocations(nil, v, distance), builtVehicleSafetyLocations(s, v, distance)
				if !slices.Equal(got, want) {
					t.Fatalf("pod %s on %q at %g m: envelope %v, the construction before slice 1 %v", v.Pod.Class, v.Pod.LaneID+v.Pod.BerthID, distance, got, want)
				}
				if reference := referenceVehicleSafetyLocations(s, v, distance); !slices.Equal(reference, want) {
					t.Fatalf("pod %s on %q at %g m: reference %v, the construction before slice 1 %v", v.Pod.Class, v.Pod.LaneID+v.Pod.BerthID, distance, reference, want)
				}
			})
			if s.clusterSafety != nil {
				t.Fatal("the network has the planes of rule 2 without ShapeClearance")
			}
			t.Logf("%s: %d envelopes", name, count)
		})
	}
}

// TestEnvelopeMatchesReference is the parity of SX3: with ShapeClearance,
// the indexed construction of the envelope and the reference construction
// give the same locations in the same order.
func TestEnvelopeMatchesReference(t *testing.T) {
	t.Parallel()
	every := 197
	if testing.Short() {
		every = 797
	}
	for name, n := range envelopeParityNetworks(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := diskStudyPrepare(t, withShapeClearance(n, true))
			count := 0
			driveEnvelopes(s, func(index int, lane Lane) bool {
				// Each group lane, and each curved lane with no geometry
				// tail, of a large network is a lane of the small-pod
				// rules.
				small := lane.Control != nil && s.tails[lane.ID] == (geometryTails{}) || s.maneuvers.groupLane(lane.ID)
				return len(n.Lanes) < 1000 || index%every == 0 || small && index%(every/2) == 0
			}, func(v *vehicle, distance float64) {
				count++
				got, want := s.appendVehicleSafetyLocations(nil, v, distance), referenceVehicleSafetyLocations(s, v, distance)
				if !slices.Equal(got, want) {
					t.Fatalf("pod %s on %q at %g m: envelope %v, reference %v", v.Pod.Class, v.Pod.LaneID+v.Pod.BerthID, distance, got, want)
				}
			})
			t.Logf("%s: %d envelopes", name, count)
		})
	}
}
