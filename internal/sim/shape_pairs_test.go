package sim

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"math/rand/v2"
	"slices"
	"testing"
)

// The lane classes of the shape pair tests: a small lane, an Express lane,
// a lane with the union body, and a Group lane in place of Express.
var (
	shapeSmallClasses   = mustClassSet("compact")
	shapeExpressClasses = mustClassSet("express")
	shapeUnionClasses   = mustClassSet("compact", "express")
	shapeGroupClasses   = mustClassSet("compact", "group")
)

func mustClassSet(classes ...string) ClassSet {
	set, err := NewClassSet(classes...)
	if err != nil {
		panic(err)
	}
	return set
}

// shapeLane is a straight lane of a shape pair test: its start, its
// heading in degrees, and its length.
type shapeLane struct {
	id      string
	classes ClassSet
	start   Point
	degrees float64
	length  float64
}

// shapeNetwork returns a network of straight lanes with their own nodes, so
// that no two lanes share a node, and the field set as given. joins adds a
// lane from the end of the lane with each key, at the heading of the
// value, 50 m long.
func shapeNetwork(field bool, lanes []shapeLane, joins map[string]float64) Network {
	n := Network{ShapeClearance: field}
	add := func(lane shapeLane) {
		s, c := math.Sincos(lane.degrees * math.Pi / 180)
		end := Point{X: lane.start.X + lane.length*c, Y: lane.start.Y + lane.length*s}
		n.Nodes = append(n.Nodes, Node{ID: lane.id + "-from", Position: lane.start}, Node{ID: lane.id + "-to", Position: end})
		n.Lanes = append(n.Lanes, Lane{ID: lane.id, From: lane.id + "-from", To: lane.id + "-to", SpeedLimit: 14, VehicleClasses: lane.classes})
	}
	for _, lane := range lanes {
		add(lane)
	}
	for _, lane := range lanes {
		degrees, ok := joins[lane.id]
		if !ok {
			continue
		}
		s, c := math.Sincos(lane.degrees * math.Pi / 180)
		from := Point{X: lane.start.X + lane.length*c, Y: lane.start.Y + lane.length*s}
		s, c = math.Sincos(degrees * math.Pi / 180)
		id := lane.id + "-join"
		n.Nodes = append(n.Nodes, Node{ID: id + "-to", Position: Point{X: from.X + 50*c, Y: from.Y + 50*s}})
		n.Lanes = append(n.Lanes, Lane{ID: id, From: lane.id + "-to", To: id + "-to", SpeedLimit: 14, VehicleClasses: lane.classes})
	}
	return n
}

// shapeAudit runs the audit of a network with no maneuver group or depot,
// as ValidateBankGeometry does, and returns its shape pairs. certifier
// replaces the certifier of the audit when it is not nil.
func shapeAudit(n Network, certifier *shapeCertifier) (*depotLateral, error) {
	tails := indexGeometryTails(n)
	return n.auditBankGeometry(bankAudit{retention: newLaneRetention(n, tails), tails: tails, certifier: certifier})
}

// shapePair reports whether the lanes with two IDs form a pair of a table.
func shapePair(n Network, table *depotLateral, a, b string) (float64, bool) {
	index := func(id string) int {
		return slices.IndexFunc(n.Lanes, func(lane Lane) bool { return lane.ID == id })
	}
	return table.clearance(laneKey(index(a)), laneKey(index(b)))
}

// shapeAbove returns the start of a lane at a heading of 0 to 90 degrees
// whose swept body is gap above the swept body of a straight lane from the
// origin along X. The lowest corner of the swept body is the rear corner on
// the right side, and it is over the top side of the other body.
func shapeAbove(below, above shapeBody, degrees, gap float64) Point {
	s, c := math.Sincos(degrees * math.Pi / 180)
	return Point{X: 40, Y: below.halfWidth + gap + above.rear*s + above.halfWidth*c}
}

// TestShapeClassBodies checks the side-by-side distances and the disk
// bounds of section 3.2 of the shape clearance design.
func TestShapeClassBodies(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		first, second VehicleClass
		want          float64
	}{
		{CompactClass, LegacyClass, 3.1}, {ExpressClass, CompactClass, 3.55}, {GroupClass, LegacyClass, 3.55},
		{ExpressClass, ExpressClass, 4.0}, {GroupClass, ExpressClass, 4.0},
	} {
		if got := shapeClassClearance(test.first, test.second); math.Abs(got-test.want) > 1e-12 {
			t.Errorf("%s and %s: side-by-side distance %g, want %g", test.first, test.second, got, test.want)
		}
	}
	small, express, union := laneShapeBody(shapeSmallClasses), laneShapeBody(shapeExpressClasses), laneShapeBody(shapeUnionClasses)
	if union != (shapeBody{rear: 4, front: 4.2, halfWidth: 2}) || laneShapeBody(shapeGroupClasses) != union || laneShapeBody(0) != small {
		t.Errorf("union body %+v, small body %+v", union, small)
	}
	for _, test := range []struct {
		first, second shapeBody
		want          float64
	}{
		{small, small, 8.954}, {express, small, 8.950}, {express, express, 8.945},
		{union, small, 9.129}, {union, express, 9.125}, {union, union, 9.304},
	} {
		first, second := test.first.sweep(), test.second.sweep()
		bound := shapeDiskBound(first, second)
		if math.Abs(bound-test.want) > 1e-9 || bound <= first.reach+second.reach+sweepGuard {
			t.Errorf("bodies %+v and %+v: disk bound %g, want %g above %g", test.first, test.second, bound, test.want, first.reach+second.reach)
		}
	}
}

// TestShapePairsSideBySide checks two straight lanes that share no node,
// for each pairing of a small lane, an Express lane, and a lane with the
// union body, side by side and at 3, 15, 45, and 90 degrees. Where the
// swept bodies touch, the pair is not a shape pair, and the audit refuses
// it at 12 m or 20 m. 0.01 m further apart, it is a shape pair at the
// side-by-side distance of the two lane bodies, and the audit accepts it.
func TestShapePairsSideBySide(t *testing.T) {
	t.Parallel()
	classes := []ClassSet{shapeSmallClasses, shapeExpressClasses, shapeUnionClasses}
	for _, below := range classes {
		for _, above := range classes {
			for _, degrees := range []float64{0, 3, 15, 45, 90} {
				for _, gap := range []float64{0, 0.01} {
					name := fmt.Sprintf("%v below %v at %g degrees, %g m", below, above, degrees, gap)
					lower, upper := laneShapeBody(below), laneShapeBody(above)
					n := shapeNetwork(true, []shapeLane{
						{id: "below", classes: below, length: 100},
						{id: "above", classes: above, start: shapeAbove(lower, upper, degrees, gap), degrees: degrees, length: 100},
					}, nil)
					table, err := shapeAudit(n, nil)
					side, ok := shapePair(n, table, "below", "above")
					clearance := classSetPairClearance(below, above)
					if gap == 0 {
						want := planeGapText("below", "above", clearance)
						if ok || err == nil || err.Error() != want {
							t.Errorf("%s: shape pair %v, error %v, want no pair and %q", name, ok, err, want)
						}
						continue
					}
					if !ok || err != nil || math.Abs(side-lower.halfWidth-upper.halfWidth) > 1e-12 {
						t.Errorf("%s: shape pair %v at %g, error %v, want a pair at %g", name, ok, side, err, lower.halfWidth+upper.halfWidth)
					}
				}
			}
		}
	}
}

// TestShapePairsUnionBody checks the union body of a lane that admits a
// small class and a large class, against a lane of each class, in both
// lane orders, with Express and with Group. In each case the other lane is
// just inside the union body and just outside the body that the case names:
// only the small front reaches it ahead of the lane, only the large rear
// reaches it behind the lane, and only the large side reaches it beside the
// lane. A lane that admits only the class that does not reach is a shape
// pair with the same other lane.
func TestShapePairsUnionBody(t *testing.T) {
	t.Parallel()
	type reach struct {
		name string
		// clear is the classes of a lane whose body does not reach the
		// other lane.
		clear ClassSet
		other func(body shapeBody) shapeLane
	}
	reaches := []reach{
		{name: "small front", clear: shapeExpressClasses, other: func(body shapeBody) shapeLane {
			return shapeLane{start: Point{X: 100 + 4.1 + body.halfWidth, Y: -20}, degrees: 90, length: 100}
		}},
		{name: "large rear", clear: shapeSmallClasses, other: func(body shapeBody) shapeLane {
			return shapeLane{start: Point{X: -3.5 - body.halfWidth, Y: -20}, degrees: 90, length: 100}
		}},
		{name: "large side", clear: shapeSmallClasses, other: func(body shapeBody) shapeLane {
			return shapeLane{start: Point{X: 20, Y: 1.8 + body.halfWidth}, length: 60}
		}},
	}
	for _, large := range []string{"express", "group"} {
		mixed := mustClassSet("compact", large)
		for _, test := range reaches {
			for _, others := range []ClassSet{shapeSmallClasses, mustClassSet(large), mixed} {
				for _, swap := range []bool{false, true} {
					for _, classes := range []ClassSet{mixed, test.clear} {
						if large == "group" && classes == shapeExpressClasses {
							classes = mustClassSet("group")
						}
						other := test.other(laneShapeBody(others))
						other.id, other.classes = "other", others
						lanes := []shapeLane{{id: "mixed", classes: classes, length: 100}, other}
						if swap {
							lanes[0], lanes[1] = lanes[1], lanes[0]
						}
						// A lane that continues the other lane gives it more
						// steps than the mixed lane, so the sweep encloses the
						// steps of the mixed lane.
						for _, joins := range []map[string]float64{nil, {"other": other.degrees}} {
							if joins != nil && test.name == "large side" {
								continue
							}
							n := shapeNetwork(true, lanes, joins)
							table, _ := shapeAudit(n, nil)
							_, ok := shapePair(n, table, "mixed", "other")
							if want := classes != mixed; ok != want {
								t.Errorf("%s with %s: lane %v against %v, swapped %v, joined %v: shape pair %v, want %v", test.name, large, classes, others, swap, joins != nil, ok, want)
							}
						}
					}
				}
			}
		}
	}
}

// TestShapePairsJoinTurns checks that the sweep covers the join turn at the
// end of a lane. A left turn at the end of the first lane swings the rear
// corner of the body 3.38 m to the right of the lane, where the second lane
// is 2.5 m away from it, so the audit refuses the pair at 12 m. Without the
// joined lane, the two lanes are a shape pair.
func TestShapePairsJoinTurns(t *testing.T) {
	t.Parallel()
	for _, joined := range []bool{false, true} {
		joins := map[string]float64{}
		if joined {
			joins["first"] = 90
		}
		n := shapeNetwork(true, []shapeLane{
			{id: "first", classes: shapeSmallClasses, length: 100},
			{id: "second", classes: shapeSmallClasses, start: Point{X: 60, Y: -4.05}, length: 100},
		}, joins)
		// The joined lane is on another plane, so that the audit does not
		// refuse it with the second lane.
		for index := range n.Lanes {
			n.Lanes[index].SeparationGroup = "main"
		}
		if joined {
			n.Lanes[2].SeparationGroup = "join"
		}
		table, err := shapeAudit(n, nil)
		want := "<nil>"
		if joined {
			want = planeGapText("first", "second", Clearance)
		}
		if _, ok := shapePair(n, table, "first", "second"); ok == joined || fmt.Sprint(err) != want {
			t.Errorf("joined %v: shape pair %v, audit %v, want a pair %v and %s", joined, ok, err, !joined, want)
		}
	}
}

// TestShapePairsSweepResults checks that only a clear sweep makes a shape
// pair. A lane goes down from a point near the left turn at the end of the
// first lane, so that the corner of its body is about 0.5 m from the body
// in the turn. The sweep must halve the turn to clear the pair. With a
// tolerance of 1 km the sweep ends as potential, and with one visit as
// unresolved, and neither is a shape pair.
func TestShapePairsSweepResults(t *testing.T) {
	t.Parallel()
	n := shapeNetwork(true, []shapeLane{
		{id: "first", classes: shapeSmallClasses, length: 100},
		{id: "second", classes: shapeSmallClasses, start: Point{X: 102 + 1.55, Y: -3.2 - 3}, degrees: 270, length: 50},
	}, map[string]float64{"first": 90})
	for _, test := range []struct {
		options sweepOptions
		want    sweepStatus
	}{
		{shapeOptions, sweepClear},
		{sweepOptions{tolerance: 1e3, visits: shapeOptions.visits}, sweepPotential},
		{sweepOptions{tolerance: shapeOptions.tolerance, visits: 1}, sweepUnresolved},
	} {
		certifier := newShapeCertifier(n, test.options)
		if got := certifier.certify(0, 1, nil); got != test.want {
			t.Fatalf("options %+v: sweep %v, want %v", test.options, got, test.want)
		}
		table, _ := shapeAudit(n, newShapeCertifier(n, test.options))
		if _, ok := shapePair(n, table, "first", "second"); ok != (test.want == sweepClear) {
			t.Errorf("options %+v: shape pair %v after sweep %v", test.options, ok, test.want)
		}
	}
}

// TestShapeDiskBound checks that the disk bound gives the result of the
// sweep. For each pairing of the lane bodies, two lanes with join turns at
// both ends, with random headings, and with their closest points from the
// disk bound to 0.5 m beyond it, need no sweep. The sweep of the same pair
// is clear when the lanes are at least the sweep tolerance (1 mm) beyond the
// bound. Nearer to the bound, the bodies can be closer than the tolerance,
// and the sweep can end as potential, but it finds no collision. 0.5 m
// closer than the disk bound, the certifier sweeps.
func TestShapeDiskBound(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewPCG(5, 17))
	classes := []ClassSet{shapeSmallClasses, shapeExpressClasses, shapeUnionClasses}
	checked, swept := 0, 0
	for _, first := range classes {
		for _, second := range classes {
			bound := shapeDiskBound(laneShapeBody(first).sweep(), laneShapeBody(second).sweep())
			for trial := range 20 {
				heading := rng.Float64() * 360
				lanes := []shapeLane{{id: "first", classes: first, length: 60}, {id: "second", classes: second, degrees: heading, length: 60}}
				joins := map[string]float64{"first": rng.Float64() * 360, "second": rng.Float64() * 360}
				// Place the second lane at the distance along a random
				// direction, then move it back until the polylines are at
				// the distance.
				distance := bound + 0.5*rng.Float64()
				if trial%5 == 0 {
					distance = bound
				}
				direction := rng.Float64() * 2 * math.Pi
				lanes[1].start = Point{X: 60 + 200*math.Cos(direction), Y: 200 * math.Sin(direction)}
				for range 200 {
					n := shapeNetwork(true, lanes, joins)
					gap := polylineGap(n.Polyline(n.Lanes[0]), n.Polyline(n.Lanes[1]))
					if math.Abs(gap-distance) < 1e-9 {
						break
					}
					lanes[1].start.X -= (gap - distance) * math.Cos(direction)
					lanes[1].start.Y -= (gap - distance) * math.Sin(direction)
				}
				n := shapeNetwork(true, lanes, joins)
				gap := polylineGap(n.Polyline(n.Lanes[0]), n.Polyline(n.Lanes[1]))
				if gap < bound {
					// The search can stop a little short of the bound.
					continue
				}
				disk := newShapeCertifier(n, shapeOptions)
				sweep := newShapeCertifier(n, shapeOptions)
				sweep.sweepAll = true
				got, want := disk.certify(0, 1, nil), sweep.certify(0, 1, nil)
				if gap < bound+shapeOptions.tolerance && want == sweepPotential {
					want = sweepClear
				}
				if got != sweepClear || want != sweepClear || disk.sweeps != 0 || sweep.sweeps != 1 {
					t.Errorf("%v and %v %g m apart: disk bound %v after %d sweeps, sweep %v", first, second, gap, got, disk.sweeps, want)
				}
				checked++
				lanes[1].start.X -= 0.5 * math.Cos(direction)
				lanes[1].start.Y -= 0.5 * math.Sin(direction)
				closer := shapeNetwork(true, lanes, joins)
				near := newShapeCertifier(closer, shapeOptions)
				near.certify(0, 1, nil)
				if gap := polylineGap(closer.Polyline(closer.Lanes[0]), closer.Polyline(closer.Lanes[1])); gap < bound {
					if near.sweeps != 1 {
						t.Errorf("%v and %v %g m apart: %d sweeps, want 1", first, second, gap, near.sweeps)
					}
					swept++
				}
			}
		}
	}
	if checked < 100 || swept < 100 {
		t.Errorf("%d pairs at the bound and %d pairs closer, want at least 100 of each", checked, swept)
	}
}

// polylineGap returns the least distance between two polylines.
func polylineGap(first, second []Point) float64 {
	gap := math.Inf(1)
	for a := 1; a < len(first); a++ {
		for b := 1; b < len(second); b++ {
			gap = min(gap, bankSegmentGap(first[a-1], first[a], second[b-1], second[b], math.Inf(1)))
		}
	}
	return gap
}

// TestShapePairsKeepWindows checks that the retention rules come first. In
// a straight chain of slow lanes, the lanes before and after an 11 m lane
// share no node, and their bodies are disjoint, but a window of the
// retention index gives them reduced pieces. So they are not a shape pair,
// and the audit accepts them with the pieces of the window.
func TestShapePairsKeepWindows(t *testing.T) {
	t.Parallel()
	n := straightChain(chainSpec{30, 2}, chainSpec{30, 2}, chainSpec{11, 2}, chainSpec{30, 2}, chainSpec{30, 2})
	n.ShapeClearance = true
	r := newLaneRetention(n, indexGeometryTails(n))
	member := func(id string) auditMember { return auditMember{spans: r.laneSpans(id), length: 30, speed: 2} }
	if !slices.ContainsFunc(r.pairPieces(member("l1"), member("l3")), func(piece pairPiece) bool { return piece.clearance < Clearance }) {
		t.Fatal("the window gives no reduced piece")
	}
	table, err := shapeAudit(n, nil)
	if _, ok := shapePair(n, table, "l1", "l3"); ok || err != nil {
		t.Errorf("shape pair %v, audit %v", ok, err)
	}
}

// TestShapeClearanceKeepsMerges checks that the field keeps the lateral
// merge pairs of LateralMerges for small lanes: a 6 degree merge has the
// same pairs and extents with either field. When the side lane admits
// Express, it forms a merge pair with the field (decision D5), and no pair
// with LateralMerges alone.
func TestShapeClearanceKeepsMerges(t *testing.T) {
	t.Parallel()
	merges := func(network Network) *depotLateral {
		return newMergeLateral(network, indexGeometryTails(network), nil, nil)
	}
	lateral, shape := mergeCorridor(true, 6), mergeCorridor(true, 6)
	lateral.LateralMerges, shape.ShapeClearance = true, true
	want, got := merges(lateral), merges(shape)
	if got == nil {
		t.Fatal("no merge pairs with the field")
	}
	if !want.any() || !maps.Equal(got.pairs, want.pairs) || !maps.Equal(got.extents, want.extents) {
		t.Fatalf("%d merge pairs with the field, %d with LateralMerges", len(got.pairs), len(want.pairs))
	}
	main, side := slices.IndexFunc(shape.Lanes, func(lane Lane) bool { return lane.ID == "main" }), slices.IndexFunc(shape.Lanes, func(lane Lane) bool { return lane.ID == "side" })
	if !got.mergePartner(main, side) {
		t.Fatal("the main lane and the side lane are not a merge pair")
	}
	shape.Lanes[side].VehicleClasses, lateral.Lanes[side].VehicleClasses = shapeUnionClasses, shapeUnionClasses
	if !merges(shape).mergePartner(main, side) {
		t.Error("with the field, an Express side lane forms no merge pair")
	}
	if merges(lateral).mergePartner(main, side) {
		t.Error("with LateralMerges alone, an Express side lane forms a merge pair")
	}
}

// TestShapeAuditCoverage checks decision D12-A: a network that sets the
// field and has no bank, large class, maneuver group, or depot gets the
// audit of each pair. Two small lanes 3.0 m apart, closer than the
// side-by-side distance, are refused with the field and accepted without
// it. Two small lanes 5 m apart are a shape pair and accepted with the
// field.
func TestShapeAuditCoverage(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		gap   float64
		field bool
		want  string
	}{
		{3, true, planeGapText("first", "second", Clearance)},
		{3, false, ""},
		{5, true, ""},
		{5, false, ""},
	} {
		n := shapeNetwork(test.field, []shapeLane{
			{id: "first", classes: shapeSmallClasses, length: 100},
			{id: "second", classes: shapeSmallClasses, start: Point{Y: test.gap}, length: 100},
		}, nil)
		err := n.ValidateBankGeometry()
		if got := fmt.Sprint(err); test.want == "" && err != nil || test.want != "" && got != test.want {
			t.Errorf("%g m apart, field %v: audit %v, want %q", test.gap, test.field, err, test.want)
		}
	}
}

// shapeEnvelopeNetwork returns an Express lane after a small lane on
// another plane, and a small lane on the plane of the small lane that
// crosses the Express lane at a distance along it, at least 12 m from the
// small lane. The two crossing lanes
// are on different planes, so only the envelope of a large pod denies the
// plane exemption. extra adds a lane 10 m long, which preparation refuses
// after the audit.
func shapeEnvelopeNetwork(field bool, at float64, extra bool) Network {
	n := Network{
		ShapeClearance: field,
		Nodes: []Node{
			{ID: "behind", Position: Point{X: -100}}, {ID: "start"}, {ID: "end", Position: Point{X: 100}},
			{ID: "cross-from", Position: Point{X: at, Y: -50}}, {ID: "cross-to", Position: Point{X: at, Y: 50}},
		},
		Lanes: []Lane{
			{ID: "behind", From: "behind", To: "start", SpeedLimit: 14, SeparationGroup: "low"},
			{ID: "express", From: "start", To: "end", SpeedLimit: 14, SeparationGroup: "high", VehicleClasses: shapeExpressClasses},
			{ID: "cross", From: "cross-from", To: "cross-to", SpeedLimit: 14, SeparationGroup: "low"},
		},
	}
	if extra {
		n.Nodes = append(n.Nodes, Node{ID: "short-from", Position: Point{X: 500}}, Node{ID: "short-to", Position: Point{X: 510}})
		n.Lanes = append(n.Lanes, Lane{ID: "short", From: "short-from", To: "short-to", SpeedLimit: 14})
	}
	return n
}

// TestShapeEnvelopePlanes checks the planes of a large path by position
// (decision D1-B). A lane that crosses an Express lane 15 m after its start
// meets the envelope of a pod there, which holds the lane behind, so the
// audit refuses the pair with and without the field. A lane that crosses
// it 60 m after its start, outside the reach of the lanes at its ends, is
// refused only without the field, which takes the whole-lane reach.
// The refusal stays a PlaneReachError, so preparation reports a short lane
// first.
func TestShapeEnvelopePlanes(t *testing.T) {
	t.Parallel()
	want := planeGapText("express", "cross", largeClearance)
	for _, test := range []struct {
		at      float64
		field   bool
		refused bool
	}{
		{15, true, true}, {15, false, true}, {60, true, false}, {60, false, true},
	} {
		n := shapeEnvelopeNetwork(test.field, test.at, false)
		err := n.ValidateBankGeometry()
		var reach *PlaneReachError
		if test.refused != (err != nil) || err != nil && (!errors.As(err, &reach) || err.Error() != want) {
			t.Errorf("crossing at %g m, field %v: audit %v, want refused %v as %q", test.at, test.field, err, test.refused, want)
		}
		if err := ValidateNetwork(n); test.refused != (err != nil) || err != nil && err.Error() != want {
			t.Errorf("crossing at %g m, field %v: preparation %v, want refused %v as %q", test.at, test.field, err, test.refused, want)
		}
		if err := ValidateNetwork(shapeEnvelopeNetwork(test.field, test.at, true)); err == nil || err.Error() != `lane "short" must be at least 24 meters long` {
			t.Errorf("crossing at %g m, field %v, short lane: preparation %v", test.at, test.field, err)
		}
	}
}

// TestShapeFoldedTail checks a network that sets the field and has a
// large lane whose tail is not finite. A small lane on the plane of the
// lane behind the Express lane runs beside the Express lane, 5 m from it.
// With finite tails, the envelope reach denies the plane exemption only in
// the first 20 m, and the two lanes are a shape pair. With a folded large
// lane elsewhere, the audit builds no envelope reach. It takes the
// whole-lane reach, as without the field, and it certifies no pair with a
// large lane, so it refuses the pair. Preparation reports the folded lane
// first.
func TestShapeFoldedTail(t *testing.T) {
	t.Parallel()
	for _, folded := range []bool{false, true} {
		n := shapeEnvelopeNetwork(true, 500, false)
		n.Nodes = append(n.Nodes, Node{ID: "beside-from", Position: Point{X: 14, Y: 5}}, Node{ID: "beside-to", Position: Point{X: 90, Y: 5}})
		n.Lanes = append(n.Lanes, Lane{ID: "beside", From: "beside-from", To: "beside-to", SpeedLimit: 14, SeparationGroup: "low"})
		if folded {
			n.Nodes = append(n.Nodes, Node{ID: "fold-from", Position: Point{Y: 1000}}, Node{ID: "fold-to", Position: Point{X: 10, Y: 1000}})
			n.Lanes = append(n.Lanes, Lane{ID: "fold", From: "fold-from", To: "fold-to", SpeedLimit: 14, VehicleClasses: shapeExpressClasses, Control: &Point{X: 60, Y: 1000}})
			if tail := indexGeometryTails(n)["fold"].tail; finite(tail) {
				t.Fatalf("the folded lane has a finite tail %g", tail)
			}
		}
		err := n.ValidateBankGeometry()
		var reach *PlaneReachError
		if folded != (err != nil) || folded && (!errors.As(err, &reach) || err.Error() != planeGapText("express", "beside", largeClearance)) {
			t.Errorf("folded %v: audit %v", folded, err)
		}
		if !folded {
			continue
		}
		if err := ValidateNetwork(n); err == nil || err.Error() != `lane "fold" has an unsupported large-vehicle path shape` {
			t.Errorf("preparation %v", err)
		}
	}
}

// TestShapeRefusalOrder checks that the field keeps the order and the text
// of the refusals of the audit on a network that both rules audit: an
// Express lane makes the audit check each pair. A small lane crosses the
// Express lane, and two small lanes cross each other. None of the three
// pairs is a shape pair.
func TestShapeRefusalOrder(t *testing.T) {
	t.Parallel()
	results := make(map[bool]string)
	for _, field := range []bool{false, true} {
		n := shapeNetwork(field, []shapeLane{
			{id: "a-cross", classes: shapeSmallClasses, start: Point{X: 300, Y: -50}, degrees: 90, length: 100},
			{id: "b-cross", classes: shapeSmallClasses, start: Point{X: 250}, length: 100},
			{id: "express", classes: shapeExpressClasses, length: 100},
			{id: "small", classes: shapeSmallClasses, start: Point{X: 50, Y: -50}, degrees: 90, length: 100},
		}, nil)
		results[field] = fmt.Sprint(n.ValidateBankGeometry())
		table, _ := shapeAudit(n, nil)
		if table.any() {
			t.Errorf("field %v: shape pairs %v", field, table.pairs)
		}
	}
	if want := planeGapText("express", "small", largeClearance); results[false] != want || results[true] != want {
		t.Errorf("refusal %q without the field and %q with it, want %q", results[false], results[true], want)
	}
}

// TestShapeOracle checks the threshold of a pod pair on the lanes of a
// shape pair: the side-by-side distance of the classes of the two pods,
// less separationTolerance, for each pairing of classes. A following pair,
// on one lane or on two lanes through a node, keeps 12 m, or 20 m with an
// Express pod, also when the network has a shape pair.
func TestShapeOracle(t *testing.T) {
	t.Parallel()
	n := shapeNetwork(true, []shapeLane{
		{id: "first", classes: shapeUnionClasses, length: 100},
		{id: "second", classes: shapeUnionClasses, start: Point{X: 40, Y: 5}, length: 100},
	}, map[string]float64{"first": 0})
	table, err := shapeAudit(n, nil)
	if err != nil || !table.any() {
		t.Fatalf("audit %v, shape pairs %v", err, table)
	}
	first, second, join := laneKey(0), laneKey(1), laneKey(2)
	observe := func(a, b VehicleClass, keys [2]separationKey, gap float64) error {
		pods := []Pod{{ID: "a", Class: a}, {ID: "b", Class: b, Position: Point{Y: gap}}}
		o := SafetyObservation{Pods: pods, bound: slices.Clone(pods), keys: keys[:], shapes: table, OrderContract: ExpressOrderContract}
		_, err := o.Check()
		return err
	}
	for _, test := range []struct {
		a, b VehicleClass
		keys [2]separationKey
		want float64
	}{
		{CompactClass, CompactClass, [2]separationKey{first, second}, 3.1},
		{CompactClass, ExpressClass, [2]separationKey{first, second}, 3.55},
		{ExpressClass, CompactClass, [2]separationKey{second, first}, 3.55},
		{ExpressClass, ExpressClass, [2]separationKey{first, second}, 4.0},
		{CompactClass, CompactClass, [2]separationKey{first, first}, Clearance},
		{ExpressClass, ExpressClass, [2]separationKey{second, second}, largeClearance},
		{CompactClass, CompactClass, [2]separationKey{first, join}, Clearance},
		{CompactClass, ExpressClass, [2]separationKey{join, first}, largeClearance},
	} {
		at := test.want - separationTolerance
		if err := observe(test.a, test.b, test.keys, at); err != nil {
			t.Errorf("%s and %s on %v at %g m: %v", test.a, test.b, test.keys, at, err)
		}
		if err := observe(test.a, test.b, test.keys, test.want-0.01); err == nil {
			t.Errorf("%s and %s on %v at %g m: no error", test.a, test.b, test.keys, test.want-0.01)
		}
	}
}

// shapeDebrisNetwork returns a station with one berth and two lanes 5 m
// apart, far from the station. With the field, each lane admits Compact
// and Express, and the two lanes form a shape pair at 4.0 m. Only lanes of
// the station form merge pairs. Without it, the lanes are small lanes, which
// the audit of a plain network does not check.
func shapeDebrisNetwork(field bool) Network {
	classes := shapeSmallClasses
	if field {
		classes = shapeUnionClasses
	}
	n := Network{
		ShapeClearance: field,
		Nodes: []Node{
			{ID: "entry"}, {ID: "exit", Position: Point{X: 200}}, {ID: "berth", Position: Point{X: 100, Y: 40}},
		},
		Lanes: []Lane{
			{ID: "through", From: "entry", To: "exit", SpeedLimit: 14, VehicleClasses: classes},
			{ID: "in", From: "entry", To: "berth", SpeedLimit: 14, VehicleClasses: classes},
			{ID: "out", From: "berth", To: "exit", SpeedLimit: 14, VehicleClasses: classes},
		},
		Stations: []Station{{ID: "dest", Name: "Destination", Entry: "entry", Exit: "exit", Berths: []Berth{{ID: "dest-01", Node: "berth"}}}},
	}
	n.Nodes = append(n.Nodes,
		Node{ID: "upper-from", Position: Point{X: -1500, Y: -500}}, Node{ID: "upper-to", Position: Point{X: -1100, Y: -500}},
		Node{ID: "lower-from", Position: Point{X: -1400, Y: -505}}, Node{ID: "lower-to", Position: Point{X: -1000, Y: -505}})
	n.Lanes = append(n.Lanes,
		Lane{ID: "upper", From: "upper-from", To: "upper-to", SpeedLimit: 14, VehicleClasses: classes},
		Lane{ID: "lower", From: "lower-from", To: "lower-to", SpeedLimit: 14, VehicleClasses: classes})
	return n
}

// TestShapeDebrisBlocksPartner checks the debris footprint of a lane of a
// shape pair (section 7.2 of the incident suspension contract). With the
// field, the footprint of debris on the upper lane holds each cell of the
// lower lane that comes closer than Clearance to the debris, and no other
// cell of the lower lane. Without it, it holds no cell of the lower lane.
// The table holds the pair at the union distance, though the fleet holds
// only small pods.
func TestShapeDebrisBlocksPartner(t *testing.T) {
	t.Parallel()
	for _, field := range []bool{true, false} {
		network := shapeDebrisNetwork(field)
		s, err := NewFleet(network, []Placement{{ID: "p1", StationID: "dest", BerthID: "dest-01"}})
		if err != nil {
			t.Fatal(err)
		}
		upper, lower := s.graph.lanes["upper"], s.graph.lanes["lower"]
		if side, ok := s.shapes.clearance(laneKey(upper), laneKey(lower)); ok != field || field && side != 4 {
			t.Fatalf("field %v: shape pair %v at %g", field, ok, side)
		}
		if observation := s.SafetyObservation(); observation.shapes != s.shapes || field && (len(observation.keys) == 0 || len(s.merges.neighborLanes(upper)) > 0 || len(s.merges.neighborLanes(lower)) > 0) {
			t.Fatalf("field %v: the safety observation does not hold the shape pairs", field)
		}
		const from, to = 300, 303
		footprint := s.debrisFootprint(upper, from, to)
		debris := polylinePiece(network.Polyline(network.Lanes[upper]), from, to)
		cells, length := s.laneCells["lower"], s.graph.lengths[lower]
		near := 0
		for cell := range cells.count() {
			piece := polylinePiece(network.Polyline(network.Lanes[lower]), cellOffset(cell, cells.count(), length), cellOffset(cell+1, cells.count(), length))
			nearby := field && bankPathsClose(debris, piece, Clearance)
			for _, r := range cells.cell(cell) {
				if held := slices.Contains(footprint, r); held != nearby && r.kind == trackResource {
					t.Errorf("field %v: lower cell %d resource %v in the footprint %v, want %v", field, cell, r, held, nearby)
				}
			}
			if nearby {
				near++
			}
		}
		if field && near == 0 {
			t.Fatal("no cell of the lower lane is near the debris")
		}
	}
}
