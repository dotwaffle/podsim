package scenarios

import (
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// TestLondonFilletCurve checks the rule for one road: the control point,
// the speed of decisions FL-D2 and FL-D3, and each case that keeps the
// road. The tangent check comes before the check of the rays behind the
// ends.
func TestLondonFilletCurve(t *testing.T) {
	t.Parallel()
	east, north, west := sim.Point{X: 1}, sim.Point{Y: 1}, sim.Point{X: -1}
	tests := []struct {
		name    string
		lane    sim.Lane
		a, b    sim.Point
		u, v    sim.Point
		want    londonFilletOutcome
		control sim.Point
		// speed is the speed limit of the curve. Zero checks that the
		// speed is below londonRampMinimumSpeed.
		speed float64
	}{
		{name: "quarter turn", b: sim.Point{X: 100, Y: 100}, u: east, v: north, want: londonFilletCurved, control: sim.Point{X: 100}, speed: math.Sqrt(londonRampAcceleration * 100 / math.Sqrt2)},
		{name: "wide turn at the line speed", b: sim.Point{X: 1000, Y: 1000}, u: east, v: north, want: londonFilletCurved, control: sim.Point{X: 1000}, speed: speedLimit},
		// The parabola from (-5, 0) to (5, 0) with control (0, -2) has a
		// smallest radius of 12.5 m.
		{name: "floor", a: sim.Point{X: -5}, b: sim.Point{X: 5}, u: londonUnit(sim.Point{X: 5, Y: -2}), v: londonUnit(sim.Point{X: 5, Y: 2}), want: londonFilletCurved, control: sim.Point{Y: -2}, speed: londonRampMinimumSpeed},
		{name: "below the floor", a: sim.Point{X: -5}, b: sim.Point{X: 5}, u: londonUnit(sim.Point{X: 5, Y: -2.1}), v: londonUnit(sim.Point{X: 5, Y: 2.1}), want: londonFilletSlow, control: sim.Point{Y: -2.1}},
		{name: "control point", lane: sim.Lane{Control: &sim.Point{X: 100}}, b: sim.Point{X: 100, Y: 100}, u: east, v: north, want: londonFilletControl},
		{name: "parallel", b: sim.Point{X: 100, Y: 10}, u: east, v: east, want: londonFilletParallelRays},
		{name: "opposite", b: sim.Point{X: 100, Y: 10}, u: east, v: west, want: londonFilletParallelRays},
		{name: "tangent at the start", b: sim.Point{X: 100, Y: 1}, u: east, v: north, want: londonFilletTangentEnd},
		{name: "tangent at the end", b: sim.Point{X: 100, Y: 1}, u: north, v: east, want: londonFilletTangentEnd},
		{name: "tangent with the rays behind", b: sim.Point{X: 100, Y: -1}, u: east, v: north, want: londonFilletTangentEnd},
		{name: "behind the start", b: sim.Point{X: -50, Y: 100}, u: east, v: north, want: londonFilletRaysBehind},
		{name: "behind the end", b: sim.Point{X: 100, Y: -50}, u: east, v: north, want: londonFilletRaysBehind},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			outcome, control, speed := londonFilletCurve(test.lane, test.a, test.b, test.u, test.v)
			if outcome != test.want {
				t.Fatalf("outcome %d, want %d", outcome, test.want)
			}
			if test.want != londonFilletCurved && test.want != londonFilletSlow {
				return
			}
			if math.Hypot(control.X-test.control.X, control.Y-test.control.Y) > 1e-9 {
				t.Fatalf("control %v, want %v", control, test.control)
			}
			// The analytic curve leaves a along u and reaches b along v.
			for _, end := range []struct{ tangent, want sim.Point }{
				{sim.Point{X: control.X - test.a.X, Y: control.Y - test.a.Y}, test.u},
				{sim.Point{X: test.b.X - control.X, Y: test.b.Y - control.Y}, test.v},
			} {
				if londonAngle(londonUnit(end.tangent), end.want) > 1e-9 {
					t.Fatalf("tangent %v, want the direction %v", end.tangent, end.want)
				}
			}
			if ramp := londonRampSpeed(test.a, control, test.b); speed != ramp {
				t.Fatalf("speed %v, want the ramp speed %v", speed, ramp)
			}
			if test.speed == 0 && !(speed < londonRampMinimumSpeed) || test.speed != 0 && math.Abs(speed-test.speed) > 1e-9 {
				t.Fatalf("speed %v, want %v", speed, test.speed)
			}
		})
	}
}

// TestLondonRoadFillets checks the neighbors of the roads on a small
// network: the end tangent of a curved link lane, the start tangent of a
// curved access lane, a control point at the end of a lane, a portal with
// two link lanes, a station without an access lane, the lanes that are
// not roads, and a road-out between a curved access lane and a curved link
// lane.
func TestLondonRoadFillets(t *testing.T) {
	t.Parallel()
	node := func(id string, x, y float64) sim.Node { return sim.Node{ID: id, Position: sim.Point{X: x, Y: y}} }
	lane := func(id, from, to, station string, control *sim.Point) sim.Lane {
		return sim.Lane{ID: id, From: from, To: to, SpeedLimit: speedLimit, StationID: station, Control: control}
	}
	network := sim.Network{
		Nodes: []sim.Node{
			node("link-start", -100, -50), node("arrival", 0, 0), node("s-diverge", 40, 40), node("s-entry", 100, 100),
			node("s-exit", -40, 100), node("s-merge", -40, 40), node("departure", -80, 0), node("link-end", -180, 0),
			node("other-start", -100, 50), node("other-arrival", 0, 200), node("t-diverge", 50, 250),
			node("r-exit", 1900, 300), node("r-merge", 1900, 0), node("r-departure", 1600, -300), node("r-link-end", 1300, -900),
		},
		Lanes: []sim.Lane{
			// The end tangent of the link lane is east, and its chord is not.
			lane("london-link-001-ab-2", "link-start", "arrival", "", &sim.Point{X: -50}),
			// The start tangent of the access lane is north, and its chord is
			// parallel to the chord of the road.
			lane("s-access-in", "s-diverge", "s-entry", "s", &sim.Point{X: 40, Y: 100}),
			lane("s-road-in-01", "arrival", "s-diverge", "s", nil),
			// The control point is at the end, so the end tangent is the
			// chord, south.
			lane("s-access-out", "s-exit", "s-merge", "s", &sim.Point{X: -40, Y: 40}),
			lane("s-road-out-01", "s-merge", "departure", "s", nil),
			lane("london-link-001-ba-1", "departure", "link-end", "", nil),
			// Two link lanes end at the portal of this road.
			lane("london-link-002-ab-2", "link-start", "other-arrival", "", nil),
			lane("london-link-003-ab-2", "other-start", "other-arrival", "", nil),
			lane("s-road-in-02", "other-arrival", "s-diverge", "s", nil),
			// Station t has no access lane.
			lane("t-road-in-01", "arrival", "t-diverge", "t", nil),
			// Not roads: no station, and not a road ID.
			lane("x-road-in-01", "arrival", "s-diverge", "", nil),
			lane("s-through", "s-entry", "s-exit", "s", nil),
			// The end tangent of the access lane is (-2, -1), and its chord
			// is south. The start tangent of the link lane is south, and its
			// chord is not. The rays meet at (1600, -150).
			lane("r-access-out", "r-exit", "r-merge", "r", &sim.Point{X: 2200, Y: 150}),
			lane("r-road-out-01", "r-merge", "r-departure", "r", nil),
			lane("london-link-004-ba-1", "r-departure", "r-link-end", "", &sim.Point{X: 1600, Y: -600}),
		},
	}
	fillets := londonRoadFillets(network)
	got := make(map[string]londonFillet)
	for _, fillet := range fillets {
		got[network.Lanes[fillet.lane].ID] = fillet
	}
	want := map[string]londonFilletOutcome{
		"s-road-in-01": londonFilletCurved, "s-road-out-01": londonFilletCurved,
		"s-road-in-02": londonFilletNoNeighbor, "t-road-in-01": londonFilletNoNeighbor,
		"r-road-out-01": londonFilletCurved,
	}
	if len(got) != len(want) {
		t.Fatalf("%d roads, want %d: %+v", len(got), len(want), fillets)
	}
	for id, outcome := range want {
		if got[id].outcome != outcome {
			t.Fatalf("%s: outcome %d, want %d", id, got[id].outcome, outcome)
		}
	}
	for id, control := range map[string]sim.Point{"s-road-in-01": {X: 40}, "s-road-out-01": {X: -40}, "r-road-out-01": {X: 1600, Y: -150}} {
		if c := got[id].control; math.Hypot(c.X-control.X, c.Y-control.Y) > 1e-9 {
			t.Fatalf("%s: control %v, want %v", id, c, control)
		}
	}
	before := project.CloneNetwork(network)
	filletLondonRoads(&network)
	for index, lane := range network.Lanes {
		fillet, ok := got[lane.ID]
		if ok && fillet.outcome == londonFilletCurved {
			if lane.Control == nil || *lane.Control != fillet.control || lane.SpeedLimit != fillet.speed {
				t.Fatalf("%s: control %v and speed %v, want %v and %v", lane.ID, lane.Control, lane.SpeedLimit, fillet.control, fillet.speed)
			}
			continue
		}
		if !reflect.DeepEqual(lane, before.Lanes[index]) {
			t.Fatalf("lane %+v is %+v", before.Lanes[index], lane)
		}
	}
}

// TestLondonRoadFilletsOnLondonFull checks the rule on the roads of
// LondonFull with straight roads, the base of the shared-track preset: the
// count of each outcome, and the network of LondonFull, which sets the
// option. Each curved road leaves the link lane before it and joins the
// lane after it at their headings, with the speed of its curve. Each other
// road keeps its chord at 14 m/s, and each other lane and node does not
// change.
func TestLondonRoadFilletsOnLondonFull(t *testing.T) {
	t.Parallel()
	base := sharedBase(t).Network
	fillets := londonRoadFillets(base)
	counts := make(map[londonFilletOutcome]int)
	for _, fillet := range fillets {
		counts[fillet.outcome]++
	}
	// The tangent check comes first, so the 15 tangent roads with rays
	// behind an end count as tangent.
	want := map[londonFilletOutcome]int{londonFilletCurved: 676, londonFilletSlow: 631, londonFilletTangentEnd: 15, londonFilletRaysBehind: 134}
	if !reflect.DeepEqual(counts, want) {
		t.Fatalf("outcomes %v, want %v", counts, want)
	}
	n := LondonFull().Network
	if !reflect.DeepEqual(n.Nodes, base.Nodes) || len(n.Lanes) != len(base.Lanes) {
		t.Fatal("the option changes the nodes or the lane count")
	}
	positions := make(map[string]sim.Point, len(n.Nodes))
	for _, node := range n.Nodes {
		positions[node.ID] = node.Position
	}
	lanes := make(map[string]sim.Lane, len(n.Lanes))
	ends, starts := make(map[string]sim.Lane), make(map[string]sim.Lane)
	for _, lane := range n.Lanes {
		lanes[lane.ID] = lane
		if strings.HasPrefix(lane.ID, "london-link-") {
			ends[lane.To], starts[lane.From] = lane, lane
		}
	}
	// The London lanes before and after a road are straight.
	chord := func(lane sim.Lane) sim.Point {
		from, to := positions[lane.From], positions[lane.To]
		return londonUnit(sim.Point{X: to.X - from.X, Y: to.Y - from.Y})
	}
	curved := make(map[int]londonFillet)
	for _, fillet := range fillets {
		if fillet.outcome == londonFilletCurved {
			curved[fillet.lane] = fillet
		}
	}
	for index, lane := range n.Lanes {
		fillet, ok := curved[index]
		if !ok {
			if !reflect.DeepEqual(lane, base.Lanes[index]) {
				t.Fatalf("lane %+v is %+v", base.Lanes[index], lane)
			}
			continue
		}
		if lane.Control == nil || *lane.Control != fillet.control || lane.SpeedLimit != fillet.speed || lane.SpeedLimit < londonRampMinimumSpeed || lane.SpeedLimit > speedLimit {
			t.Fatalf("%s: control %v and speed %v", lane.ID, lane.Control, lane.SpeedLimit)
		}
		before, after := ends[lane.From], lanes[lane.StationID+"-access-in"]
		if strings.Contains(lane.ID, "-road-out-") {
			before, after = lanes[lane.StationID+"-access-out"], starts[lane.To]
		}
		from, to, control := positions[lane.From], positions[lane.To], *lane.Control
		if londonAngle(londonUnit(sim.Point{X: control.X - from.X, Y: control.Y - from.Y}), chord(before)) > 1e-9 ||
			londonAngle(londonUnit(sim.Point{X: to.X - control.X, Y: to.Y - control.Y}), chord(after)) > 1e-9 {
			t.Fatalf("%s is not tangent to %s and %s", lane.ID, before.ID, after.ID)
		}
	}
}

// TestLondonRoadFilletsOption checks that LondonFull sets the option and
// LondonCentral and the base of D do not, that londonCapacity.equal
// compares it, and that it has no effect with ramps.
func TestLondonRoadFilletsOption(t *testing.T) {
	t.Parallel()
	express := DefaultLondonFullOptions()
	expressBaseOptions(&express)
	if DefaultLondonCentralOptions().RoadFillets || !DefaultLondonFullOptions().RoadFillets || express.RoadFillets {
		t.Fatal("the road fillets are not set for LondonFull only")
	}
	var source londonSource
	if err := decodeLondonSource(&source); err != nil {
		t.Fatal(err)
	}
	options := DefaultLondonCentralOptions()
	straight, err := options.resolve(source)
	if err != nil {
		t.Fatal(err)
	}
	options.RoadFillets = true
	fillets, err := options.resolve(source)
	if err != nil {
		t.Fatal(err)
	}
	if straight.equal(fillets) || fillets.equal(straight) || !fillets.equal(fillets) {
		t.Fatal("equal does not compare the road fillets")
	}
	options.Junctions = LondonRamps
	ramps, err := LondonCentralWith(options)
	if err != nil {
		t.Fatal(err)
	}
	build := londonRampsPresets()["LondonCentral"]
	if build.err != nil {
		t.Fatal(build.err)
	}
	if !reflect.DeepEqual(ramps.Network, build.config.Network) {
		t.Fatal("the road fillets change the network with ramps")
	}
}
