package sim

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"testing"
)

// This file tests the envelope of section 3.1 of
// docs/design/shared-track-preset.md at its bounds: the anchor front of
// each lane, the anchors within the slack of the published position, the
// bounds of the items and the float64 values next to them, the selection
// of the planes of a pod, and rule 2 after the selection. Each check also
// requires the envelope of the code to equal the reference construction
// (see referenceVehicleSafetyLocations), and the index to hold it.

// anchorPod returns a traveling pod on a route of lanes, after its origin
// release, at a published position of the lane at a route index.
func anchorPod(t *testing.T, s *Simulation, ids []string, at int, position float64) *vehicle {
	t.Helper()
	route := make([]Lane, len(ids))
	for index, id := range ids {
		lane, ok := s.graph.lanes[id]
		if !ok {
			t.Fatalf("no lane %q", id)
		}
		route[index] = s.network.Lanes[lane]
	}
	v := &vehicle{Pod: Pod{ID: "anchor", Class: CompactClass, Activity: Traveling}, originReleased: true}
	s.setVehicleRoute(v, route)
	anchorMove(v, at, position)
	return v
}

// anchorMove publishes a pod at a position of the lane at a route index.
func anchorMove(v *vehicle, at int, position float64) {
	v.Pod.LaneID, v.Pod.LaneDistance = v.Route[at].ID, position
	v.distance = v.blocks.lanes[at].start + position
}

// anchorEnvelope returns the envelope of a pod. It requires the envelope
// to equal the reference construction, and the span of the index at the
// published position to hold each of its planes.
func anchorEnvelope(t *testing.T, s *Simulation, v *vehicle) []SafetyLocation {
	t.Helper()
	got := s.appendVehicleSafetyLocations(nil, v)
	if want := referenceVehicleSafetyLocations(s, v); !slices.Equal(got, want) {
		t.Errorf("pod on %s at %v m: envelope %v, reference %v", v.Pod.LaneID, v.Pod.LaneDistance, got, want)
	}
	r := s.envelopes
	var span *reachSpan
	if v.Pod.Activity == Traveling {
		span = r.laneAt(s.graph.lanes[v.Pod.LaneID], v.Pod.LaneDistance)
	} else {
		span = &r.berths[r.berthLocations[v.Pod.BerthID]-len(r.lanes)]
	}
	closed := r.closure(span.locations)
	for _, location := range got {
		if !slices.ContainsFunc(closed, func(number int) bool { return r.locations[number] == location }) {
			t.Errorf("pod on %s at %v m: the index does not hold plane %+v", v.Pod.LaneID, v.Pod.LaneDistance, location)
		}
	}
	return got
}

// anchorHolds requires the envelope of a pod at each published position
// to hold a lane exactly when want says so.
func anchorHolds(t *testing.T, s *Simulation, v *vehicle, at int, lane string, positions []float64, want func(float64) bool) {
	t.Helper()
	for _, position := range positions {
		anchorMove(v, at, position)
		got := slices.Contains(anchorEnvelope(t, s, v), s.laneSafety[lane])
		if got != want(position) {
			t.Errorf("pod on %s at %v m: holds lane %s %v, want %v", v.Pod.LaneID, position, lane, got, want(position))
		}
	}
}

// around returns a value and the float64 values next to it.
func around(value float64) []float64 {
	return []float64{math.Nextafter(value, math.Inf(-1)), value, math.Nextafter(value, math.Inf(1))}
}

// TestEnvelopeAnchorFront pins the anchor front on the curves network. Lane
// bend has its own front, and each other lane 6 m. A pod on lane short
// takes the front of short, and not the front of bend behind it, so it
// does not hold lane short2 24 m ahead. Within the slack of the start of
// short, an anchor on bend is possible, so the envelope holds the planes of
// that anchor, short2 among them.
func TestEnvelopeAnchorFront(t *testing.T) {
	t.Parallel()
	s := diskStudyPrepare(t, envelopeReachCurves(t))
	g := &s.envelopes.graph
	bend := s.tails["bend"].front
	if bend <= 24+envelopeReachSlack {
		t.Fatalf("lane bend has the front %g m, not more than the 24 m of lane short", bend)
	}
	for id, want := range map[string]float64{"out": largeEnvelopeRadius, "bend": bend, "short": largeEnvelopeRadius, "short2": largeEnvelopeRadius, "far": largeEnvelopeRadius} {
		if got := g.anchorFront(s.graph.lanes[id]); got != want {
			t.Errorf("lane %s: anchor front %g m, want %g m", id, got, want)
		}
	}
	v := anchorPod(t, s, []string{"bend", "short", "short2", "far"}, 1, 0)
	anchorHolds(t, s, v, 1, "short2", append(around(envelopeReachSlack), 0, 2*envelopeReachSlack, 1, 10), func(p float64) bool { return p <= envelopeReachSlack })
	// The front of short holds short2 from 6 m and the slack before its
	// start.
	start := (g.lengths[s.graph.lanes["short"]] + 0 - largeEnvelopeRadius) - envelopeReachSlack
	anchorHolds(t, s, v, 1, "short2", around(start), func(p float64) bool { return p <= envelopeReachSlack || p >= start })
	// A pod on bend 20 m before its end holds short with the front of bend.
	length := g.lengths[s.graph.lanes["bend"]]
	anchorHolds(t, s, v, 0, "short", []float64{length - 20}, func(float64) bool { return true })
	anchorHolds(t, s, v, 0, "far", []float64{length - 20}, func(float64) bool { return false })
}

// envelopeReachSubSlackCurves returns the curves network with
// ShapeClearance and a group lane gap of the given length from node b,
// where lane bend ends, to the start of lane short, and a lane spur out
// of the end of gap that no route of the tests uses. This is the first
// construction of the code review of slice 1: lane bend has a front bound
// over 24 m, lanes gap and short have the front bound 6 m, and lane short2
// starts 24 m past the start of short.
func envelopeReachSubSlackCurves(t *testing.T, length float64) Network {
	t.Helper()
	n := envelopeReachCurves(t)
	n.Nodes = append(n.Nodes, Node{ID: "b2", Position: Point{X: 90}})
	n.Nodes[slices.IndexFunc(n.Nodes, func(node Node) bool { return node.ID == "b" })].Position.X -= length
	short := n.Lanes[networkLaneIndex(n, "short")]
	gap := short
	gap.ID, gap.To, gap.SeparationGroup = "gap", "b2", "g-gap"
	gap.VehicleClasses = largeGeometryClasses(t, "compact")
	short.From = "b2"
	spur := short
	spur.ID, spur.To, spur.SeparationGroup = "spur", "e", "g-spur"
	n.Lanes[networkLaneIndex(n, "short")] = short
	n.Lanes = append(n.Lanes, gap, spur)
	n.ManeuverGroups = append(n.ManeuverGroups, ManeuverGroup{ID: "gap-group", StationID: "station", LaneIDs: []string{"gap"}})
	return n
}

// TestEnvelopeAnchorSubSlackChain is the first construction of the code
// review of slice 1: lane bend, then a group lane gap shorter than the
// slack, then lane short. At the start of short, an anchor at the end of
// bend is within the slack, through gap, so the envelope holds lane short2
// by the front of bend while p <= u - length(gap), and not after that. With
// a gap longer than the slack, no anchor on bend is possible at the start
// of short.
func TestEnvelopeAnchorSubSlackChain(t *testing.T) {
	t.Parallel()
	for _, length := range []float64{envelopeReachSlack / 2, envelopeReachSlack + restoreTolerance} {
		t.Run(fmt.Sprint(length), func(t *testing.T) {
			t.Parallel()
			s := diskStudyPrepare(t, envelopeReachSubSlackCurves(t, length))
			gap := s.envelopes.graph.lengths[s.graph.lanes["gap"]]
			if math.Abs(gap-length) > 1e-9 {
				t.Fatalf("lane gap is %g m", gap)
			}
			v := anchorPod(t, s, []string{"bend", "gap", "short", "short2", "far"}, 2, 0)
			bound := envelopeReachSlack - gap
			anchorHolds(t, s, v, 2, "short2", append(around(max(0, bound)), 0, envelopeReachSlack, 1), func(p float64) bool { return p <= bound })
			// On gap, the envelope holds short2 by the front of bend
			// within the slack of the start of gap.
			anchorHolds(t, s, v, 1, "short2", []float64{0}, func(float64) bool { return true })
		})
	}
}

// anchorStraight returns a network of straight lanes along the x axis,
// each lane from the node at one x to the node at the next, with its own
// plane, and lanes named by names.
func anchorStraight(names []string, xs []float64) Network {
	var n Network
	n.ShapeClearance = true
	for index, x := range xs {
		n.Nodes = append(n.Nodes, Node{ID: fmt.Sprintf("n%d", index), Position: Point{X: x}})
	}
	for index, name := range names {
		n.Lanes = append(n.Lanes, Lane{ID: name, From: fmt.Sprintf("n%d", index), To: fmt.Sprintf("n%d", index+1), SpeedLimit: 14, SeparationGroup: "g-" + name})
	}
	return n
}

// TestEnvelopeAnchorExactBounds is the second construction of the code
// review of slice 1: lanes P, L, Q, M, and N, where N starts exactly the
// anchor front of L plus the slack after the end of L, by the walk sum. The
// walk then reaches the start of N, and the envelope at each bound of an
// item holds the item, and at the float64 next to the bound outside the
// item does not. The bounds are those of the behind item of P, the ahead
// item of N, and the anchors within the slack of each end of L. With N one
// float64 further, the walk of L does not reach it. The anchors at the end
// of L hold N in both cases, so TestEnvelopeAnchorWalkLimit tests the walk
// limit by itself.
func TestEnvelopeAnchorExactBounds(t *testing.T) {
	t.Parallel()
	d := largeEnvelopeRadius + envelopeReachSlack
	for _, end := range []float64{d, math.Nextafter(d, math.Inf(1))} {
		t.Run(fmt.Sprint(end), func(t *testing.T) {
			t.Parallel()
			n := anchorStraight([]string{"P", "L", "Q", "M", "N"}, []float64{-80, -40, 0, 6, end, end + 40})
			s := diskStudyPrepare(t, n)
			g := &s.envelopes.graph
			if walked := g.lengths[2] + g.lengths[3]; walked != end {
				t.Fatalf("the walk sum to the start of N is %v, want %v", walked, end)
			}
			v := anchorPod(t, s, []string{"P", "L", "Q", "M", "N"}, 1, 0)
			length := g.lengths[1]
			// The behind item of P.
			behind := g.tails[0] - 0 + envelopeReachSlack
			anchorHolds(t, s, v, 1, "P", around(behind), func(p float64) bool { return p <= behind })
			// The ahead item of N from the anchor on L, and the anchors on Q
			// within the slack of the end of L, whose front reaches N.
			reached := end <= d
			from := length + end - largeEnvelopeRadius - envelopeReachSlack
			near := length + 0 - envelopeReachSlack
			positions := slices.Concat(around(from), around(near), []float64{length, math.Nextafter(length, math.Inf(-1))})
			anchorHolds(t, s, v, 1, "N", positions, func(p float64) bool { return reached && p >= from || p >= near })
		})
	}
}

// TestEnvelopeAnchorShortChain checks a chain of many short lanes between
// lane bend and lane short of the curves network. When the chain is
// shorter than the slack, an anchor at the end of bend is possible at the
// start of short, so the envelope holds short2. When it is longer, it does
// not.
func TestEnvelopeAnchorShortChain(t *testing.T) {
	t.Parallel()
	for _, count := range []int{50, 150} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			t.Parallel()
			const step = 1e-5
			n := envelopeReachCurves(t)
			b := slices.IndexFunc(n.Nodes, func(node Node) bool { return node.ID == "b" })
			n.Nodes[b].Position.X -= float64(count) * step
			short := networkLaneIndex(n, "short")
			ids := []string{"bend"}
			from := "b"
			for index := range count {
				to := fmt.Sprintf("k%d", index)
				n.Nodes = append(n.Nodes, Node{ID: to, Position: Point{X: n.Nodes[b].Position.X + float64(index+1)*step}})
				id := fmt.Sprintf("chain%d", index)
				n.Lanes = append(n.Lanes, Lane{ID: id, From: from, To: to, SpeedLimit: 14, SeparationGroup: "g-" + id})
				ids, from = append(ids, id), to
			}
			n.Lanes[short].From = from
			s := diskStudyPrepare(t, n)
			ids = append(ids, "short", "short2", "far")
			at := slices.Index(ids, "short")
			walk := newEnvelopeReference(s).walk(from, false, 1)
			bound := envelopeReachSlack - walk["b"]
			v := anchorPod(t, s, ids, at, 0)
			anchorHolds(t, s, v, at, "short2", append(around(max(0, bound)), 0), func(p float64) bool { return p <= bound })
		})
	}
}

// TestEnvelopeAnchorRepeatedLane drives a route that uses each lane of a
// loop two times. The envelope selects lanes by identity, so a pod at the
// same published position has the same envelope at each use of the lane.
func TestEnvelopeAnchorRepeatedLane(t *testing.T) {
	t.Parallel()
	n := Network{ShapeClearance: true, Nodes: []Node{{ID: "a"}, {ID: "b", Position: Point{X: 100}}, {ID: "c", Position: Point{X: 100, Y: 100}}, {ID: "d", Position: Point{Y: 100}}}}
	for _, lane := range [][2]string{{"a", "b"}, {"b", "c"}, {"c", "d"}, {"d", "a"}} {
		n.Lanes = append(n.Lanes, Lane{ID: lane[0] + lane[1], From: lane[0], To: lane[1], SpeedLimit: 14, SeparationGroup: "g-" + lane[0] + lane[1]})
	}
	s := diskStudyPrepare(t, n)
	ids := []string{"ab", "bc", "cd", "da", "ab", "bc", "cd", "da"}
	v := anchorPod(t, s, ids, 0, 0)
	for _, position := range []float64{0, envelopeReachSlack, 5, 50, 95, 100} {
		for at := range 4 {
			anchorMove(v, at, position)
			first := anchorEnvelope(t, s, v)
			anchorMove(v, at+4, position)
			if second := anchorEnvelope(t, s, v); !slices.Equal(first, second) {
				t.Errorf("lane %s at %g m: envelope %v at the first use, %v at the second", ids[at], position, first, second)
			}
		}
	}
}

// TestEnvelopeAnchorRestoreNode checks a pod that a restore publishes at
// the start of lane short while its route distance is two
// restoreTolerance steps before it, at the end of lane bend. The envelope
// reads the published position, so it does not depend on the route
// distance, and it holds the planes of an anchor at the end of bend, short2
// among them. The anchor at the end of bend is also within the slack of the
// published position.
func TestEnvelopeAnchorRestoreNode(t *testing.T) {
	t.Parallel()
	s := diskStudyPrepare(t, envelopeReachCurves(t))
	v := anchorPod(t, s, []string{"out", "bend", "short", "short2", "far"}, 2, 0)
	start := v.blocks.lanes[2].start
	want := anchorEnvelope(t, s, v)
	for _, distance := range []float64{start - 2*restoreTolerance, math.Nextafter(start, math.Inf(-1)), start + 2*restoreTolerance} {
		v.distance = distance
		if got := anchorEnvelope(t, s, v); !slices.Equal(got, want) {
			t.Errorf("route distance %v: envelope %v, want %v", distance, got, want)
		}
	}
	for _, lane := range []string{"bend", "short2"} {
		if !slices.Contains(want, s.laneSafety[lane]) {
			t.Errorf("the envelope at the start of short does not hold lane %s: %v", lane, want)
		}
	}
}

// TestEnvelopeAnchorRoutePrefix checks that the envelope does not depend on
// the route distance of the start of the lane. The case is the
// counterexample of the review of the shapes: the start of lane L at a
// route distance of 2^44 m, L 40 m long, and a pod at 33.9985 m on L, where
// a sum of route distances rounds to the start of the next lane. The
// envelope holds the next lane only from 40 - 6 - 0.001 m, with any prefix.
func TestEnvelopeAnchorRoutePrefix(t *testing.T) {
	t.Parallel()
	s := diskStudyPrepare(t, anchorStraight([]string{"L", "N"}, []float64{0, 40, 80}))
	from := s.envelopes.graph.lengths[0] + 0 - largeEnvelopeRadius - envelopeReachSlack
	for _, prefix := range []float64{0, math.Ldexp(1, 44)} {
		v := anchorPod(t, s, []string{"L", "N"}, 0, 0)
		for index := range v.blocks.lanes {
			v.blocks.lanes[index].start += prefix
		}
		anchorHolds(t, s, v, 0, "N", append(around(from), 33.9985), func(p float64) bool { return p >= from })
	}
}

// TestEnvelopeAnchorSelection checks the selection of the planes of a pod
// on the curves network. A pod that leaves the berth before its origin
// release selects the unit of its origin node, which holds the lane arrive
// off its route and the berth. A pod at the same position after the release,
// or with no origin, does not. Lane through,
// off the route, is not in the envelope. A pod at the berth selects the
// berth and the lanes at its node.
func TestEnvelopeAnchorSelection(t *testing.T) {
	t.Parallel()
	s := diskStudyPrepare(t, envelopeReachCurves(t))
	berth := s.berthSafety["berth"]
	for _, origin := range []bool{true, false} {
		v := anchorPod(t, s, []string{"out", "bend"}, 0, 5)
		if origin {
			v.origin, v.originReleased = s.network.Stations[0].Berths[0], false
		}
		got := anchorEnvelope(t, s, v)
		for plane, want := range map[SafetyLocation]bool{berth: origin, s.laneSafety["arrive"]: origin, s.laneSafety["out"]: true, s.laneSafety["through"]: false} {
			if slices.Contains(got, plane) != want {
				t.Errorf("origin %v: envelope %v, want plane %+v %v", origin, got, plane, want)
			}
		}
		// Past the origin tail of the node, no origin plane.
		anchorMove(v, 0, s.envelopes.graph.originAt[s.envelopes.graph.nodes["berth"]]+2*envelopeReachSlack)
		if got := anchorEnvelope(t, s, v); slices.Contains(got, berth) || slices.Contains(got, s.laneSafety["arrive"]) {
			t.Errorf("origin %v: past the origin tail, envelope %v holds an origin plane", origin, got)
		}
	}
	// After the origin release, the pod selects no origin unit.
	v := anchorPod(t, s, []string{"out", "bend"}, 0, 5)
	v.origin = s.network.Stations[0].Berths[0]
	if got := anchorEnvelope(t, s, v); slices.Contains(got, berth) || slices.Contains(got, s.laneSafety["arrive"]) {
		t.Errorf("after the release, envelope %v holds an origin plane", got)
	}
	parked := &vehicle{Pod: Pod{ID: "parked", Class: CompactClass, Activity: Idle, BerthID: "berth"}, originReleased: true}
	want := []SafetyLocation{s.laneSafety["arrive"], s.laneSafety["out"], berth}
	if got := anchorEnvelope(t, s, parked); !slices.Equal(got, want) {
		t.Errorf("pod at the berth: envelope %v, want %v", got, want)
	}
}

// TestEnvelopeAnchorRuleTwoAfterSelection checks that rule 2 applies to the
// selected planes, and keeps the lanes of the cluster off the route. Lane
// gap is a group lane, and lane spur leaves its end off the route. A pod on
// gap, and a pod on short with gap behind it, hold spur by rule 2. A pod on
// far, with no group lane in its envelope, does not.
func TestEnvelopeAnchorRuleTwoAfterSelection(t *testing.T) {
	t.Parallel()
	s := diskStudyPrepare(t, envelopeReachSubSlackCurves(t, 2))
	ids := []string{"bend", "gap", "short", "short2", "far"}
	v := anchorPod(t, s, ids, 1, 1)
	for _, tc := range []struct {
		at       int
		position float64
		want     bool
	}{{1, 1, true}, {2, 10, true}, {4, 50, false}} {
		anchorMove(v, tc.at, tc.position)
		if got := anchorEnvelope(t, s, v); slices.Contains(got, s.laneSafety["spur"]) != tc.want {
			t.Errorf("pod on %s at %g m: envelope %v, want lane spur %v", ids[tc.at], tc.position, got, tc.want)
		}
	}
}

// TestEnvelopeAnchorWalkLimit checks the walk limit of the ahead walk at
// its exact bound. Lane L turns back, and it has no geometry tail, so rule
// 1 gives it the anchor front smallFrontCap. Lanes Q and M follow, 1 m and
// about 15.2 m long, so that the walk sum to the start of lane N is the
// anchor front plus the slack, as a float64. The anchor front of Q is 6 m,
// so no anchor within the slack of the end of L holds N. Only the item of
// L holds N, from its start to the end of L. With N one float64 further,
// no position of L holds N.
func TestEnvelopeAnchorWalkLimit(t *testing.T) {
	t.Parallel()
	limit := smallFrontCap + envelopeReachSlack
	for _, beyond := range []bool{false, true} {
		t.Run(strconv.FormatBool(beyond), func(t *testing.T) {
			t.Parallel()
			// limit - 1 is exact, so lane M is limit - 1 long, and the walk
			// sum 1 + (limit - 1) is limit.
			end := limit
			if beyond {
				end = math.Nextafter(end, math.Inf(1))
			}
			for start := -30.0; start > -32; start -= 1.0 / 64 {
				n := anchorStraight([]string{"L", "Q", "M", "N"}, []float64{start, 0, 1, end, end + 40})
				n.Lanes[0].Control = &Point{X: 20, Y: 30}
				s := diskStudyPrepare(t, n)
				g := &s.envelopes.graph
				if front := g.anchorFront(0); front != smallFrontCap {
					t.Fatalf("lane L has the anchor front %v, want %v", front, smallFrontCap)
				}
				walked := g.lengths[1] + g.lengths[2]
				if beyond == (walked == limit) {
					t.Fatalf("the walk sum to the start of N is %v, the limit %v", walked, limit)
				}
				length := g.lengths[0]
				from := ((length + limit) - smallFrontCap) - envelopeReachSlack
				if from > length || from < length-envelopeReachSlack {
					continue
				}
				v := anchorPod(t, s, []string{"L", "Q", "M", "N"}, 0, 0)
				positions := slices.DeleteFunc(slices.Concat(around(from), []float64{length, length - 2*envelopeReachSlack}), func(p float64) bool { return p > length })
				anchorHolds(t, s, v, 0, "N", positions, func(p float64) bool { return !beyond && p >= from })
				return
			}
			t.Fatal("no start of lane L gives an item of N that starts on L")
		})
	}
}

// anchorNetwork returns a network with ShapeClearance from nodes and
// lanes. Each lane is its ID, From node, and To node, and it has its own
// plane.
func anchorNetwork(nodes map[string]Point, lanes [][3]string) Network {
	n := Network{ShapeClearance: true}
	for _, id := range slices.Sorted(maps.Keys(nodes)) {
		n.Nodes = append(n.Nodes, Node{ID: id, Position: nodes[id]})
	}
	for _, lane := range lanes {
		n.Lanes = append(n.Lanes, Lane{ID: lane[0], From: lane[1], To: lane[2], SpeedLimit: 14, SeparationGroup: "g-" + lane[0]})
	}
	return n
}

// anchorSpanHolds reports whether the span of the index at a published
// position holds a lane.
func anchorSpanHolds(s *Simulation, lane string, position float64, other string) bool {
	r := s.envelopes
	span := r.laneAt(s.graph.lanes[lane], position)
	return slices.ContainsFunc(r.closure(span.locations), func(number int) bool { return r.locations[number] == s.laneSafety[other] })
}

// TestEnvelopeAnchorDiverge checks a diverge at node x: lane P into x, and
// lanes L and M out of x. An anchor at the end of P is within the slack of
// the start of L, and the walk ahead of P reaches M. Along the paths
// through L, the span at the start of L does not hold M, so neither the
// oracle nor the audit takes it. A pod on P, whose span holds both, selects
// the lane of its route. The loop of the route of the last case comes back
// to x and takes M: the pod at the start of L does not hold M, and the pod
// at the end of the lane before M does.
func TestEnvelopeAnchorDiverge(t *testing.T) {
	t.Parallel()
	n := anchorNetwork(map[string]Point{"s": {X: -40}, "x": {}, "y": {X: 40}, "z": {X: 40, Y: 40}, "w": {Y: 40}, "m": {Y: -40}},
		[][3]string{{"P", "s", "x"}, {"L", "x", "y"}, {"N", "y", "z"}, {"R", "z", "w"}, {"Q", "w", "x"}, {"M", "x", "m"}})
	s := diskStudyPrepare(t, n)
	for _, p := range []float64{0, envelopeReachSlack} {
		if anchorSpanHolds(s, "L", p, "M") {
			t.Errorf("the span at %g m of L holds M", p)
		}
		if !anchorSpanHolds(s, "L", p, "P") {
			t.Errorf("the span at %g m of L does not hold P", p)
		}
	}
	if !anchorSpanHolds(s, "P", 40, "M") || !anchorSpanHolds(s, "P", 40, "L") {
		t.Error("the span at the end of P does not hold both lanes out of x")
	}
	holds := func(v *vehicle, lane string) bool {
		return slices.Contains(anchorEnvelope(t, s, v), s.laneSafety[lane])
	}
	v := anchorPod(t, s, []string{"P", "L", "N"}, 1, 0)
	if holds(v, "M") || !holds(v, "P") {
		t.Errorf("route P L N at the start of L: envelope %v", anchorEnvelope(t, s, v))
	}
	anchorMove(v, 0, 40)
	if holds(v, "M") || !holds(v, "L") {
		t.Errorf("route P L N at the end of P: envelope %v", anchorEnvelope(t, s, v))
	}
	v = anchorPod(t, s, []string{"P", "M"}, 0, 40)
	if !holds(v, "M") || holds(v, "L") {
		t.Errorf("route P M at the end of P: envelope %v", anchorEnvelope(t, s, v))
	}
	anchorMove(v, 1, 0)
	if !holds(v, "M") || !holds(v, "P") || holds(v, "L") {
		t.Errorf("route P M at the start of M: envelope %v", anchorEnvelope(t, s, v))
	}
	v = anchorPod(t, s, []string{"P", "L", "N", "R", "Q", "M"}, 1, 0)
	if holds(v, "M") {
		t.Errorf("loop route at the start of L: envelope %v holds M", anchorEnvelope(t, s, v))
	}
	anchorMove(v, 4, 40)
	if !holds(v, "M") {
		t.Errorf("loop route at the end of Q: envelope %v does not hold M", anchorEnvelope(t, s, v))
	}
	// A restore publishes the start of L with the route distance at the end
	// of P.
	v = anchorPod(t, s, []string{"P", "L", "N"}, 1, 0)
	want := anchorEnvelope(t, s, v)
	v.distance = v.blocks.lanes[1].start - 2*restoreTolerance
	if got := anchorEnvelope(t, s, v); !slices.Equal(got, want) || holds(v, "M") {
		t.Errorf("restored before the node: envelope %v, want %v without M", got, want)
	}
}

// TestEnvelopeAnchorMerge checks a merge at node y: lanes L and K into y,
// and lane N out of y. An anchor at the start of N is within the slack of
// the end of L, and the walk behind N reaches K. Along the paths through
// L, the span at the end of L does not hold K. A pod on N selects the lane
// of its route behind it.
func TestEnvelopeAnchorMerge(t *testing.T) {
	t.Parallel()
	n := anchorNetwork(map[string]Point{"a": {X: -40}, "y": {}, "k": {Y: -40}, "b": {X: 40}},
		[][3]string{{"L", "a", "y"}, {"K", "k", "y"}, {"N", "y", "b"}})
	s := diskStudyPrepare(t, n)
	for _, p := range []float64{40, 40 - envelopeReachSlack} {
		if anchorSpanHolds(s, "L", p, "K") {
			t.Errorf("the span at %g m of L holds K", p)
		}
		if !anchorSpanHolds(s, "L", p, "N") {
			t.Errorf("the span at %g m of L does not hold N", p)
		}
	}
	holds := func(v *vehicle, lane string) bool {
		return slices.Contains(anchorEnvelope(t, s, v), s.laneSafety[lane])
	}
	v := anchorPod(t, s, []string{"L", "N"}, 0, 40)
	if holds(v, "K") || !holds(v, "N") {
		t.Errorf("route L N at the end of L: envelope %v", anchorEnvelope(t, s, v))
	}
	v = anchorPod(t, s, []string{"K", "N"}, 1, 0)
	if !holds(v, "K") || holds(v, "L") {
		t.Errorf("route K N at the start of N: envelope %v", anchorEnvelope(t, s, v))
	}
}

// TestEnvelopeAnchorChainSiblings checks a chain of two short lanes C1 and
// C2 between lane P and lane L, with a lane S out of the node between them
// and a lane M out of the From node of L. Along the paths through L, the
// span at the start of L holds P, C1, and C2, and not S or M.
func TestEnvelopeAnchorChainSiblings(t *testing.T) {
	t.Parallel()
	n := anchorNetwork(map[string]Point{"s": {X: -40}, "c": {X: -0.0004}, "d": {X: -0.0002}, "x": {}, "y": {X: 40}, "e": {X: -0.0002, Y: -40}, "m": {Y: 40}},
		[][3]string{{"P", "s", "c"}, {"C1", "c", "d"}, {"C2", "d", "x"}, {"L", "x", "y"}, {"S", "d", "e"}, {"M", "x", "m"}})
	s := diskStudyPrepare(t, n)
	for lane, want := range map[string]bool{"P": true, "C1": true, "C2": true, "S": false, "M": false} {
		if got := anchorSpanHolds(s, "L", 0, lane); got != want {
			t.Errorf("the span at the start of L holds %s %v, want %v", lane, got, want)
		}
	}
	v := anchorPod(t, s, []string{"P", "C1", "C2", "L"}, 3, 0)
	if got := anchorEnvelope(t, s, v); slices.Contains(got, s.laneSafety["S"]) || slices.Contains(got, s.laneSafety["M"]) || !slices.Contains(got, s.laneSafety["P"]) {
		t.Errorf("route P C1 C2 L at the start of L: envelope %v", got)
	}
}

// routeExpected returns the lanes of a route that the envelope of an anchor
// at the route distance s must hold, measured along the route only: the
// lane of the anchor; each lane that ends at most its retained tail plus
// the slack behind s; and each lane that starts at most the anchor front of
// the lane of the anchor plus the slack ahead of s. It does not use the
// walks of the index or of the reference.
func routeExpected(s *Simulation, v *vehicle, distance float64) []string {
	g := &s.envelopes.graph
	var anchors []int
	for index := range v.Route {
		if b := v.blocks.lanes[index]; b.start <= distance && distance <= b.start+g.lengths[s.graph.lanes[v.Route[index].ID]] {
			anchors = append(anchors, index)
		}
	}
	var held []string
	for index, lane := range v.Route {
		number := s.graph.lanes[lane.ID]
		start := v.blocks.lanes[index].start
		end := start + g.lengths[number]
		hold := slices.Contains(anchors, index) || (end <= distance && distance-end <= g.tails[number]+envelopeReachSlack/2)
		for _, anchor := range anchors {
			hold = hold || (start >= distance && start-distance <= g.anchorFront(s.graph.lanes[v.Route[anchor].ID])+envelopeReachSlack/2)
		}
		if hold {
			held = append(held, lane.ID)
		}
	}
	return held
}

// anchorRuleOneChain returns lanes S of 40 m, A, L of a length, and M of
// 40 m in a line. A is a curved lane of about 5.9 m with the anchor front
// smallFrontCap of rule 1, more than the 6 m of the other lanes.
func anchorRuleOneChain(t *testing.T, length float64) Network {
	t.Helper()
	n := anchorStraight([]string{"S", "A", "L", "M"}, []float64{-40, 0, 2, 2 + length, 42 + length})
	n.Lanes[1].Control = &Point{X: 1, Y: 5}
	return n
}

// anchorRuleOneLength returns the length of lane A of anchorRuleOneChain,
// and requires its anchor front to be smallFrontCap.
func anchorRuleOneLength(t *testing.T) float64 {
	t.Helper()
	s := diskStudyPrepare(t, anchorRuleOneChain(t, 10))
	g := &s.envelopes.graph
	if front := g.anchorFront(1); front != smallFrontCap {
		t.Fatalf("lane A has the anchor front %v, want %v", front, smallFrontCap)
	}
	return g.lengths[1]
}

// TestEnvelopeAnchorRouteCoverage checks the index against a construction
// that does not use its walks. For each route, at route distances near each
// node and in each lane, a pod is published on each route lane within half
// the slack of its anchor. Its envelope must hold each lane of the route
// that routeExpected gives. The diverge network has a branch M of 4 m next
// to a path L L2 of 5.66 m from x to y, so a walk through M is shorter than
// the route through L. The chain network has short lanes and a sibling of
// the chain. On the curves network, the anchor front of bend is more than
// the 24 m of short, so an anchor at the end of bend holds short2 from the
// start of short. On the rule 1 chains, lane A has the anchor front
// 16.2 m, and lane M starts 15.7 m after an end of A: after the end of A
// through a lane L of 15.7 m, for an anchor before L, and after the start
// of A through A and L, for an anchor after lane S. The anchor front of 6
// m of S and L does not reach M, so only the walks of A through the
// published lane hold M, within 1 m of their bound.
func TestEnvelopeAnchorRouteCoverage(t *testing.T) {
	t.Parallel()
	diverge := anchorNetwork(map[string]Point{"s": {X: -30}, "x": {}, "w": {X: 2, Y: 2}, "y": {X: 4}, "z": {X: 34}, "k": {X: 4, Y: -30}},
		[][3]string{{"P", "s", "x"}, {"L", "x", "w"}, {"L2", "w", "y"}, {"M", "x", "y"}, {"K", "k", "y"}, {"N", "y", "z"}})
	chain := anchorNetwork(map[string]Point{"s": {X: -40}, "c": {X: -0.0004}, "d": {X: -0.0002}, "x": {}, "y": {X: 40}, "e": {X: -0.0002, Y: -40}, "m": {Y: 40}},
		[][3]string{{"P", "s", "c"}, {"C1", "c", "d"}, {"C2", "d", "x"}, {"L", "x", "y"}, {"S", "d", "e"}, {"M", "x", "m"}})
	loop := anchorNetwork(map[string]Point{"s": {X: -40}, "x": {}, "y": {X: 40}, "z": {X: 40, Y: 40}, "w": {Y: 40}, "m": {Y: -40}},
		[][3]string{{"P", "s", "x"}, {"L", "x", "y"}, {"N", "y", "z"}, {"R", "z", "w"}, {"Q", "w", "x"}, {"M", "x", "m"}})
	for _, c := range []struct {
		name   string
		n      Network
		routes [][]string
	}{
		{"diverge", diverge, [][]string{{"P", "L", "L2", "N"}, {"P", "M", "N"}, {"K", "N"}}},
		{"chain", chain, [][]string{{"P", "C1", "C2", "L"}, {"P", "C1", "S"}, {"P", "C1", "C2", "M"}}},
		{"loop", loop, [][]string{{"P", "L", "N", "R", "Q", "M"}, {"P", "L", "N", "R", "Q", "L"}}},
		{"curves", envelopeReachCurves(t), [][]string{{"out", "bend", "short", "short2", "far", "leave"}}},
		{"rule 1 before", anchorRuleOneChain(t, 15.7), [][]string{{"S", "A", "L", "M"}}},
		{"rule 1 after", anchorRuleOneChain(t, 15.7-anchorRuleOneLength(t)), [][]string{{"S", "A", "L", "M"}}},
	} {
		s := diskStudyPrepare(t, c.n)
		for _, ids := range c.routes {
			v := anchorPod(t, s, ids, 0, 0)
			var distances []float64
			for index := range v.Route {
				start := v.blocks.lanes[index].start
				length := s.envelopes.graph.lengths[s.graph.lanes[ids[index]]]
				for _, offset := range []float64{-envelopeReachSlack / 2, -1e-6, 0, 1e-6, envelopeReachSlack / 2, length / 2, length - 1} {
					if d := start + offset; d >= 0 {
						distances = append(distances, d)
					}
				}
			}
			for _, distance := range distances {
				for at := range v.Route {
					start := v.blocks.lanes[at].start
					position := min(max(distance-start, 0), s.envelopes.graph.lengths[s.graph.lanes[ids[at]]])
					if math.Abs(start+position-distance) > envelopeReachSlack/2 {
						continue
					}
					v.Pod.LaneID, v.Pod.LaneDistance, v.distance = ids[at], position, distance
					got := anchorEnvelope(t, s, v)
					for _, lane := range routeExpected(s, v, distance) {
						if !slices.Contains(got, s.laneSafety[lane]) {
							t.Errorf("%s, route %v, anchor %g m, published on %s at %g m: envelope does not hold %s", c.name, ids, distance, ids[at], position, lane)
						}
					}
				}
			}
		}
	}
}

// anchorAuditNetwork returns an Express lane from node x, after the lane
// behind on plane low, and a small sibling lane from x on plane side. A
// small lane cross on plane other ends 3 m from x, after a lane feed whose
// plane is feed. The span of the end of cross holds feed.
func anchorAuditNetwork(feed string) Network {
	return Network{ShapeClearance: true,
		Nodes: []Node{{ID: "behind", Position: Point{X: -100}}, {ID: "x"}, {ID: "end", Position: Point{X: 100}}, {ID: "side", Position: Point{X: 50, Y: 50}},
			{ID: "ff", Position: Point{X: 2, Y: -60}}, {ID: "cf", Position: Point{X: 2, Y: -15}}, {ID: "ct", Position: Point{X: 2, Y: -3}}},
		Lanes: []Lane{
			{ID: "behind", From: "behind", To: "x", SpeedLimit: 14, SeparationGroup: "low"},
			{ID: "express", From: "x", To: "end", SpeedLimit: 14, SeparationGroup: "high", VehicleClasses: shapeExpressClasses},
			{ID: "sibling", From: "x", To: "side", SpeedLimit: 14, SeparationGroup: "side"},
			{ID: "feed", From: "ff", To: "cf", SpeedLimit: 14, SeparationGroup: feed},
			{ID: "cross", From: "cf", To: "ct", SpeedLimit: 14, SeparationGroup: "other"},
		}}
}

// TestEnvelopeAnchorAuditSibling checks the bank audit at a diverge. An
// anchor at the end of lane behind is within the slack of the start of the
// Express lane, and the walk from the end of behind reaches the sibling
// lane. Along the paths through the Express lane, the span at its start
// does not hold the plane side of the sibling, so the audit accepts the
// network when feed is on plane side. When feed is on plane low, the plane
// of the lane behind, the span holds it, and the audit refuses the pair.
func TestEnvelopeAnchorAuditSibling(t *testing.T) {
	t.Parallel()
	if err := anchorAuditNetwork("side").ValidateBankGeometry(); err != nil {
		t.Errorf("feed on plane side: audit %v, want no refusal", err)
	}
	want := `nonincident paths "express" and "cross" are less than 20 meters apart`
	if err := anchorAuditNetwork("low").ValidateBankGeometry(); err == nil || err.Error() != want {
		t.Errorf("feed on plane low: audit %v, want %q", err, want)
	}
}

// TestEnvelopeAnchorRouteEnd drives a pod with no destination through move
// to the end of its route, where the last lane F is shorter than the last
// step. publishVehicleTravel then returns at the route end before it
// publishes, so the published position stays on lane S before F, behind
// the route distance by at most routeEndGap. F is curved, with an anchor
// front of rule 1 of more than 7 m. The route starts with lane E at node
// z1, 7 m after the To node of F along lane Z, which is not on the route.
// The anchor front of 6 m of S does not reach E, so only the anchor on F,
// within the slack of the published position, holds E. At each tick, the envelope must equal the reference
// construction and hold the lanes of routeExpected at the route distance.
func TestEnvelopeAnchorRouteEnd(t *testing.T) {
	t.Parallel()
	n := anchorNetwork(map[string]Point{"z": {}, "z1": {X: 7}, "w": {X: -40, Y: 40}, "a": {X: -2e-5}},
		[][3]string{{"Z", "z", "z1"}, {"E", "z1", "w"}, {"S", "w", "a"}, {"F", "a", "z"}})
	n.Lanes[3].Control = &Point{X: -1e-5, Y: 1e-5}
	s := diskStudyPrepare(t, n)
	g := &s.envelopes.graph
	if front := g.anchorFront(3); front <= 7+envelopeReachSlack {
		t.Fatalf("lane F has the anchor front %v, not more than 7 m", front)
	}
	ids := []string{"E", "S", "F"}
	stale := 0
	for start := 0.5; start < 3; start += 0.05 {
		v := anchorPod(t, s, ids, 1, g.lengths[2]-start)
		v.reservedThrough = v.blocks.len() - 1
		v.Pod.Speed = 1
		end := v.blocks.end(v.blocks.len() - 1)
		for range 30 * TicksPerSecond {
			for v.blockIndex+1 < v.blocks.len() && v.blocks.end(v.blockIndex) <= v.distance {
				v.blockIndex++
			}
			s.move(v)
			published := v.blocks.lanes[slices.Index(ids, v.Pod.LaneID)].start + v.Pod.LaneDistance
			if gap := v.distance - published; gap < 0 || gap > routeEndGap {
				t.Fatalf("start %g m: route distance %v, published %s at %v m", start, v.distance, v.Pod.LaneID, v.Pod.LaneDistance)
			}
			got := anchorEnvelope(t, s, v)
			for _, lane := range routeExpected(s, v, v.distance) {
				if !slices.Contains(got, s.laneSafety[lane]) {
					t.Errorf("start %g m: route distance %v, published on %s: envelope does not hold %s", start, v.distance, v.Pod.LaneID, lane)
				}
			}
			if v.distance == end {
				break
			}
		}
		if v.distance != end {
			t.Fatalf("start %g m: the pod stops at %v, not at the route end %v", start, v.distance, end)
		}
		if v.Pod.LaneID == "S" {
			stale++
			if !slices.Contains(anchorEnvelope(t, s, v), s.laneSafety["E"]) {
				t.Errorf("start %g m: the envelope at the route end does not hold E", start)
			}
		}
	}
	if stale == 0 {
		t.Fatal("no start ends with the published position on S")
	}
}

// routeEndGap is the largest last step of a pod that the endpoint snap of
// moveStep brings to its limit: the step a with a - safe(a) dt = 1e-5 m is
// about 2.21e-4 m at the highest maximum braking, 2 m/s^2, and 1.70e-4 m
// at 1.15 m/s^2 of Express. The bound is half the slack.
const routeEndGap = envelopeReachSlack / 2
