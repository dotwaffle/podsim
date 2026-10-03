package sim

import (
	"math"
	"reflect"
	"testing"
)

func TestLargeProfileCellBoundsSurviveConstruction(t *testing.T) {
	t.Parallel()
	for _, input := range []laneCellsInput{
		{lane: Lane{ID: "ordinary", From: "a", To: "b"}, length: 40},
		{lane: Lane{ID: "neighbor", From: "a", To: "b"}, length: 40, tail: 20, fromTail: 20},
		{lane: Lane{ID: "distant", From: "a", To: "b"}, length: 40, tail: 20},
		{lane: Lane{ID: "large-curve", From: "a", To: "b", VehicleClasses: largeGeometryClasses(t, "group")}, length: 40, tail: 40, fromTail: 40},
	} {
		cells := newLaneCells(input)
		if cells.tail != input.tail || cells.fromTail != input.fromTail {
			t.Fatalf("constructor dropped immutable bounds: %+v", cells)
		}
		blocks := blockList{route: []Lane{input.lane}, lanes: []routeLaneCells{{cells: cells, length: 40}, {first: cells.count(), start: 40}}, blocks: cells.count()}
		b := blocks.at(0)
		if b.tail != input.tail || b.fromTail != input.fromTail {
			t.Fatal("route block dropped immutable bounds")
		}
		v := vehicle{blocks: blocks}
		if input.lane.VehicleClasses.Allows(string(GroupClass)) {
			v.Pod.Class = GroupClass
		}
		if v.originTail() != max(12, input.fromTail) || blockTail(b) != max(12, input.tail) {
			t.Fatal("origin and route tail bounds were conflated")
		}
		if got := resourceReleaseDistance(b, resource{kind: nodeResource, id: "a"}); got != max(12, input.fromTail) {
			t.Fatalf("origin node release %g", got)
		}
		if got := resourceReleaseDistance(b, resource{kind: trackResource, id: input.lane.ID}); got != b.end+max(12, input.tail) {
			t.Fatalf("track release %g", got)
		}
	}
}

func TestLargeProfileNodeExtent(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		length, tail float64
		toCells      int
	}{{24, 0, 1}, {24, 20, 2}, {100, 20, 1}, {100, 40, 2}} {
		cells := newLaneCells(laneCellsInput{lane: Lane{ID: "incoming", From: "a", To: "berth"}, length: tc.length, tail: tc.tail})
		count := 0
		for cell := range cells.count() {
			for _, r := range cells.cell(cell) {
				if r == (resource{kind: nodeResource, id: "berth"}) {
					count++
				}
			}
		}
		if count != tc.toCells {
			t.Fatalf("length %g tail %g: node in %d cells, want %d", tc.length, tc.tail, count, tc.toCells)
		}
	}
}

func TestLargeProfileProjectedCurveTail(t *testing.T) {
	t.Parallel()
	points := []Point{{}, {X: 50, Y: 40}, {X: 100}}
	tail := largePathTail(points)
	if tail <= 20 || !finite(tail) {
		t.Fatalf("curve retained the straight-only distance: %g", tail)
	}
	length := 2 * math.Hypot(50, 40)
	point := func(distance float64) Point {
		segment := 0
		part := math.Hypot(50, 40)
		if distance > part {
			segment, distance = 1, distance-part
		}
		a, b := points[segment], points[segment+1]
		fraction := distance / part
		return Point{X: a.X + fraction*(b.X-a.X), Y: a.Y + fraction*(b.Y-a.Y)}
	}
	for first := 0.0; first < length-tail; first += .25 {
		for second := first + tail; second < length; second += .25 {
			a, b := point(first), point(second)
			if math.Hypot(a.X-b.X, a.Y-b.Y) < 20-1e-8 {
				t.Fatal("derived curve retention failed the independent center bound")
			}
		}
	}
	if got := largePathTail([]Point{{}, {X: 100}, {X: 1}}); !math.IsInf(got, 1) {
		t.Fatal("folded path gained a finite projection proof")
	}
}

func TestLargeProfileLinkExclusions(t *testing.T) {
	t.Parallel()
	for _, classes := range [][2]VehicleClass{{GroupClass, LegacyClass}, {LegacyClass, GroupClass}, {GroupClass, GroupClass}, {ExpressClass, CompactClass}} {
		s := &Simulation{vehicles: make([]vehicle, 2)}
		s.vehicles[0].Pod.Class, s.vehicles[1].Pod.Class = classes[0], classes[1]
		v, leader := &s.vehicles[0], &s.vehicles[1]
		if largeVehicleClass(v.Pod.Class) && s.canLink(v) {
			t.Fatal("large pod can create an ordinary link")
		}
		s.tryLink(0, 1)
		if v.link.leader != 0 || leader.follower != 0 {
			t.Fatal("large or mixed link was created")
		}
		if _, ok := s.planLink(linkPlan{v: v, leader: leader}); ok {
			t.Fatal("ordinary planner admitted a large or mixed link")
		}
		if _, ok := s.planBufferLink(linkPlan{v: v, leader: leader}); ok {
			t.Fatal("buffer planner admitted a large or mixed link")
		}
	}
}

func TestLargeProfileBufferCellBound(t *testing.T) {
	t.Parallel()
	s := stationBufferFixture(t)
	v := &s.vehicles[0]
	entry := v.Route[len(v.Route)-1]
	original := s.laneCells[entry.ID]
	copyCells := *original
	copyCells.tail = 20
	// Authored preparation prevents this shape; the buffer planner must also
	// reject a stale or reconstructed holding region with sub-20-meter cells.
	copyCells.ends = make([]int, max(4, int(math.Ceil(s.laneLength(entry)/19))))
	s.laneCells[entry.ID] = &copyCells
	if _, ok := s.bufferPlan(v); ok {
		t.Fatal("buffer accepted holding cells below the shared geometry bound")
	}
}

func TestLargeProfileOraclePairBounds(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		classes [2]VehicleClass
		gap     float64
		valid   bool
	}{
		{[2]VehicleClass{LegacyClass, CompactClass}, 12, true},
		{[2]VehicleClass{GroupClass, LegacyClass}, 19.99, false},
		{[2]VehicleClass{LegacyClass, GroupClass}, 19.99, false},
		{[2]VehicleClass{GroupClass, GroupClass}, 20, true},
	} {
		o := SafetyObservation{Pods: []Pod{{ID: "a", Class: tc.classes[0]}, {ID: "b", Class: tc.classes[1], Position: Point{X: tc.gap}}}}
		// A private compact exception cannot reduce a large-pair bound.
		o.compactPairs = map[[2]string]compactSafetyPair{{"a", "b"}: {first: o.Pods[0], second: o.Pods[1], minimum: 6}}
		if tc.classes[0] == LegacyClass {
			o.compactPairs = nil
		}
		_, err := o.checkSeparation()
		if (err == nil) != tc.valid {
			t.Fatalf("class pair %v at %g: %v", tc.classes, tc.gap, err)
		}
	}
}

func TestLargeProfileOraclePlaneProof(t *testing.T) {
	t.Parallel()
	upper := SafetyLocation{SeparationGroup: "upper", From: "a", To: "b"}
	lower := SafetyLocation{SeparationGroup: "lower", From: "c", To: "d"}
	o := SafetyObservation{Pods: []Pod{{ID: "group", Class: GroupClass}, {ID: "small", Position: Point{X: 1}}}, Locations: map[string]SafetyLocation{"group": lower, "small": upper}}
	if _, err := o.checkSeparation(); err == nil {
		t.Fatal("center-only locations granted a large plane exemption")
	}
	o.envelopes = map[string]safetyEnvelope{
		"group": {pod: o.Pods[0], locations: []SafetyLocation{lower}},
		"small": {pod: o.Pods[1], locations: []SafetyLocation{upper}},
	}
	if _, err := o.checkSeparation(); err != nil {
		t.Fatal(err)
	}
	envelope := o.envelopes["group"]
	envelope.locations = append(envelope.locations, upper)
	o.envelopes["group"] = envelope
	if _, err := o.checkSeparation(); err == nil {
		t.Fatal("retained incoming plane was ignored")
	}
	envelope.locations = []SafetyLocation{lower}
	o.envelopes["group"] = envelope
	o.Pods[0].Position.Y = 1
	if _, err := o.checkSeparation(); err == nil {
		t.Fatal("changed pod reused a stale plane proof")
	}
}

func TestLargeProfileEnvelopeRouteAndBerth(t *testing.T) {
	t.Parallel()
	n := Network{Nodes: []Node{{ID: "a"}, {ID: "b", Position: Point{X: 100}}, {ID: "c", Position: Point{X: 200}}},
		Lanes:    []Lane{{ID: "upper", From: "a", To: "b", SeparationGroup: "upper"}, {ID: "lower", From: "b", To: "c", SeparationGroup: "lower"}},
		Stations: []Station{{ID: "end", Berths: []Berth{{ID: "end-1", Node: "c", SeparationGroup: "lower"}}}}}
	s := &Simulation{network: n, laneSafety: map[string]SafetyLocation{"upper": {SeparationGroup: "upper", From: "a", To: "b"}, "lower": {SeparationGroup: "lower", From: "b", To: "c"}}, berthSafety: map[string]SafetyLocation{"end-1": {SeparationGroup: "lower", From: "c", To: "c"}}}
	v := vehicle{originReleased: true, blocks: blockList{lanes: []routeLaneCells{{start: 0}, {start: 100}, {start: 200}}}}
	v.Pod, v.Route = Pod{ID: "group", Class: GroupClass, Activity: Traveling, LaneID: "lower"}, n.Lanes
	before := v
	if got := s.vehicleSafetyLocations(&v, 116); len(got) != 2 {
		t.Fatalf("retained plane missing: %+v", got)
	}
	if got := s.vehicleSafetyLocations(&v, 120.001); len(got) != 1 || got[0].SeparationGroup != "lower" {
		t.Fatalf("passed incoming plane retained indefinitely: %+v", got)
	}
	v.Pod.LaneID = "upper"
	if got := s.vehicleSafetyLocations(&v, 94); len(got) != 2 {
		t.Fatalf("front envelope failed to reach next plane: %+v", got)
	}
	v = before
	v.Pod.Activity, v.Pod.LaneID, v.Pod.BerthID = Idle, "", "end-1"
	if got := s.vehicleSafetyLocations(&v, 0); len(got) != 2 {
		t.Fatalf("stationary berth lost its incoming plane: %+v", got)
	}
	v = before
	_ = s.vehicleSafetyLocations(&v, 116)
	if !reflect.DeepEqual(v, before) {
		t.Fatal("oracle changed live vehicle state")
	}
	v.distance = 116
	s.vehicles = []vehicle{v}
	observation := s.SafetyObservation()
	if envelope := observation.envelopes[v.Pod.ID]; envelope.pod != v.Pod || len(envelope.locations) != 2 {
		t.Fatal("native observation omitted its body-plane proof")
	}
	observation.envelopes[v.Pod.ID].locations[0].SeparationGroup = "changed"
	if s.SafetyObservation().envelopes[v.Pod.ID].locations[0].SeparationGroup == "changed" {
		t.Fatal("body-plane evidence shares mutable simulation storage")
	}
}
