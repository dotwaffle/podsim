package sim

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
	"time"
)

// The tests of the oriented body oracle on shape pairs (see
// lateralBodyCheck), from section 7 of docs/design/shape-clearance.md.

// shapeStreamGap is the distance in meters between the two parallel lanes
// of the stream test: 0.01 m more than the side-by-side distance of two
// lanes with the union body.
const shapeStreamGap = 4.01

// shapeStreamLength is the length in meters of each of the two parallel
// lanes.
const shapeStreamLength = 600.0

// shapeStreamRing returns the lanes, nodes, and stations of one ring of
// shapeStreamNetwork. The ring goes east on its parallel lane from the
// origin to (shapeStreamLength, 0), turns north on a curve that leaves the
// lane at a small angle, goes north to the entry of its east station,
// west through its east station and its west station, and south and east
// on a curve back to the start of the parallel lane. With flip, the ring
// is reflected on the line halfway between the two parallel lanes, and its
// parallel lane is gap meters south of the other. Each lane, station, and
// berth admits Compact and Express.
//
// The two curves are quadratic, with a long arm at the parallel lane and a
// short arm at the far end, so that each curve turns slowly near the other
// ring. Each station has three berths 200 m from its entry, at 12 degrees
// on each side of its axis and at 24 degrees, and its exit 1200 m beyond
// its entry, as largeOracleNetwork has, so that the audit of the field
// accepts it.
func shapeStreamRing(prefix, lane string, flip bool, gap float64) ([]Node, []Lane, []Station) {
	at := func(x, y float64) Point {
		if flip {
			return Point{X: x, Y: -gap - y}
		}
		return Point{X: x, Y: y}
	}
	node := func(id string) string { return prefix + "-" + id }
	const (
		arm, short, turn = 1000.0, 150.0, 30 * math.Pi / 180
		top              = 600.0
	)
	s, c := math.Sincos(turn)
	outEnd := Point{X: shapeStreamLength + arm + short*c, Y: short * s}
	inStart := Point{X: -arm - short*c, Y: short * s}
	nodes := []Node{
		{ID: node("start"), Position: at(0, 0)},
		{ID: node("end"), Position: at(shapeStreamLength, 0)},
		{ID: node("out"), Position: at(outEnd.X, outEnd.Y)},
		{ID: node("in"), Position: at(inStart.X, inStart.Y)},
	}
	outControl, inControl := at(shapeStreamLength+arm, 0), at(-arm, 0)
	lanes := []Lane{
		{ID: lane, From: node("start"), To: node("end")},
		{ID: node("out"), From: node("end"), To: node("out"), Control: &outControl},
		{ID: node("in"), From: node("in"), To: node("start"), Control: &inControl},
	}
	var stations []Station
	for index, entry := range []Point{{X: 2000, Y: top}, {X: -400, Y: top}} {
		id := fmt.Sprintf("%s%d", prefix, 2-index)
		station := Station{ID: id, Name: id, Entry: id + "-entry", Exit: id + "-exit", VehicleClasses: shapeUnionClasses}
		nodes = append(nodes, Node{ID: station.Entry, Position: at(entry.X, entry.Y)}, Node{ID: station.Exit, Position: at(entry.X-1200, entry.Y)})
		lanes = append(lanes, Lane{ID: id + "-through", From: station.Entry, To: station.Exit})
		for berth, degrees := range []float64{12, -12, 24} {
			s, c := math.Sincos((180 + degrees) * math.Pi / 180)
			berthNode := fmt.Sprintf("%s-berth-%d", id, berth+1)
			nodes = append(nodes, Node{ID: berthNode, Position: at(entry.X+200*c, entry.Y+200*s)})
			lanes = append(lanes, Lane{ID: berthNode + "-in", From: station.Entry, To: berthNode}, Lane{ID: berthNode + "-out", From: berthNode, To: station.Exit})
			station.Berths = append(station.Berths, Berth{ID: fmt.Sprintf("%s-%d", id, berth+1), Node: berthNode, VehicleClasses: shapeUnionClasses})
		}
		stations = append(stations, station)
	}
	lanes = append(lanes,
		Lane{ID: node("up"), From: node("out"), To: prefix + "2-entry"},
		Lane{ID: node("mid"), From: prefix + "2-exit", To: prefix + "1-entry"},
		Lane{ID: node("down"), From: prefix + "1-exit", To: node("in")})
	for index := range lanes {
		lanes[index].SpeedLimit, lanes[index].VehicleClasses = 14, shapeUnionClasses
	}
	return nodes, lanes, stations
}

// shapeStreamNetwork returns two rings of shapeStreamRing with the field:
// ring a with the parallel lane "upper" and the stations a1 and a2, and
// ring b, gap meters to the south, with the parallel lane "lower" and the
// stations b1 and b2. A pod on its way from station 1 to station 2 of its
// ring passes its parallel lane.
func shapeStreamNetwork(gap float64) Network {
	n := Network{ShapeClearance: true}
	for _, ring := range []struct {
		prefix, lane string
		flip         bool
	}{{"a", "upper", false}, {"b", "lower", true}} {
		nodes, lanes, stations := shapeStreamRing(ring.prefix, ring.lane, ring.flip, gap)
		n.Nodes, n.Lanes, n.Stations = append(n.Nodes, nodes...), append(n.Lanes, lanes...), append(n.Stations, stations...)
	}
	return n
}

// shapeStreamFleet returns three Express pods at the berths of station a1
// and three Compact pods at the berths of station b1.
func shapeStreamFleet() []Placement {
	var fleet []Placement
	for index := range 3 {
		fleet = append(fleet,
			Placement{ID: fmt.Sprintf("x%d", index+1), Class: ExpressClass, StationID: "a1", BerthID: fmt.Sprintf("a1-%d", index+1)},
			Placement{ID: fmt.Sprintf("c%d", index+1), Class: CompactClass, StationID: "b1", BerthID: fmt.Sprintf("b1-%d", index+1)})
	}
	return fleet
}

// shapeStreamRun is the result of runShapeStreams: the first error of the
// oracle and of Check, the smallest distance between two pods on the lanes
// of a shape pair, and the number of times that a pod came onto a
// parallel lane.
type shapeStreamRun struct {
	oracle, check error
	closest       float64
	passes        int
}

// shapeStreamInterval is the time in ticks between two departures of a
// stream from station 1 of its ring. It is longer than the time that the
// station exit needs between two Express pods.
const shapeStreamInterval = 25 * TicksPerSecond

// shapeStreamLag returns the time in ticks that the lower acceleration of
// Express costs a pod that starts from rest to line speed, against a
// Compact pod: v/2 times the difference of the inverse rates. A Compact pod
// that leaves this much later than an Express pod passes the parallel
// lanes abreast of it.
func shapeStreamLag() int64 {
	express, compact := motionEnvelopeFor(ExpressClass).acceleration, motionEnvelopeFor(CompactClass).acceleration
	return int64(math.Round(TicksPerSecond * 14 / 2 * (1/express - 1/compact)))
}

// runShapeStreams steps s for the given seconds. Every shapeStreamInterval
// ticks, it sends an idle Express pod from station a1 to station a2, and
// shapeStreamLag ticks later an idle Compact pod from station b1 to station
// b2, so that the two pods pass the parallel lanes abreast. It sends each
// idle pod at station 2 of a ring back to station 1 at once. After each
// step, it runs the oracle and Check, and it keeps the first error of
// each.
func runShapeStreams(t *testing.T, s *Simulation, seconds int) shapeStreamRun {
	t.Helper()
	run := shapeStreamRun{closest: math.Inf(1)}
	lag := shapeStreamLag()
	compactDue := int64(-1)
	send := func(station, to string) bool {
		for _, v := range s.vehicles {
			if v.Pod.Activity != Idle || v.Pod.StationID != station || s.assigned(v.Pod.ID) {
				continue
			}
			if err := s.RequestJourney(v.Pod.ID, to); err != nil {
				t.Fatalf("tick %d: journey of pod %s to %s: %v", s.tick, v.Pod.ID, to, err)
			}
			return true
		}
		return false
	}
	lanes := make([]string, len(s.vehicles))
	for range seconds * TicksPerSecond {
		for send("a2", "a1") || send("b2", "b1") {
		}
		if s.tick%shapeStreamInterval == 0 && send("a1", "a2") {
			compactDue = s.tick + lag
		}
		if s.tick == compactDue {
			send("b1", "b2")
		}
		s.Step()
		if err := s.lateralBodyCheck(); err != nil && run.oracle == nil {
			run.oracle = err
		}
		if _, err := s.SafetyObservation().Check(); err != nil && run.check == nil {
			run.check = err
		}
		for i := range s.vehicles {
			lane := s.vehicles[i].Pod.LaneID
			if lane != lanes[i] && (lane == "upper" || lane == "lower") {
				run.passes++
			}
			lanes[i] = lane
		}
		run.closest = min(run.closest, shapePairClosest(s))
	}
	return run
}

// shapePairClosest returns the smallest distance between two pods on the
// lanes of a shape pair, or +Inf.
func shapePairClosest(s *Simulation) float64 {
	closest := math.Inf(1)
	for i := range s.vehicles {
		a, ok := s.graph.lanes[s.vehicles[i].Pod.LaneID]
		if !ok {
			continue
		}
		for j := i + 1; j < len(s.vehicles); j++ {
			b, ok := s.graph.lanes[s.vehicles[j].Pod.LaneID]
			if _, pair := s.shapes.clearance(laneKey(a), laneKey(b)); ok && pair {
				closest = min(closest, pointDistance(s.vehicles[i].Pod.Position, s.vehicles[j].Pod.Position))
			}
		}
	}
	return closest
}

// TestShapeOracleTwoStreams is the stream test of section 7 of the
// design: two streams on parallel lanes 4.01 m apart, each lane admitting
// Compact and Express, one stream of Express pods and one of Compact pods,
// for 15 simulated minutes, with Check and the oriented body oracle at each
// tick. The two lanes form a shape pair at the side-by-side distance of
// the union body, 4.0 m. Each pod passes its parallel lane twice, abreast
// of a pod of the other stream, so the closest pods are about 4.01 m
// apart.
func TestShapeOracleTwoStreams(t *testing.T) {
	t.Parallel()
	fleet := shapeStreamFleet()
	s, err := NewFleetWithOrderContract(shapeStreamNetwork(shapeStreamGap), fleet, ExpressOrderContract)
	if err != nil {
		t.Fatal(err)
	}
	upper, lower := s.graph.lanes["upper"], s.graph.lanes["lower"]
	if side, ok := s.shapes.clearance(laneKey(upper), laneKey(lower)); !ok || side != 4 {
		t.Fatalf("lanes upper and lower: shape pair %v at %g m, want a shape pair at 4 m", ok, side)
	}
	start := time.Now()
	run := runShapeStreams(t, s, 15*60)
	t.Logf("closest pods of a shape pair %.4f m, %d passes of a parallel lane, run time %v", run.closest, run.passes, time.Since(start))
	if run.oracle != nil {
		t.Error(run.oracle)
	}
	if run.check != nil {
		t.Error(run.check)
	}
	if run.passes < 2*len(fleet) {
		t.Errorf("%d passes of a parallel lane, want at least %d", run.passes, 2*len(fleet))
	}
	if !(run.closest < shapeStreamGap+0.5) {
		t.Errorf("closest pods of a shape pair %.3f m, want less than %.2f m, so that the pods pass abreast", run.closest, shapeStreamGap+0.5)
	}
}

// shapeProbeY is the Y coordinate of lane first of shapeProbeNetwork, far
// from the ring.
const shapeProbeY = -5000.0

// shapeProbeNetwork returns ring a of shapeStreamRing with the field, and
// two lanes that share no node with it or with each other: lane first,
// 400 m east from (0, shapeProbeY), and lane second from one point to
// another. Each lane admits Compact and Express.
func shapeProbeNetwork(from, to Point) Network {
	nodes, lanes, stations := shapeStreamRing("a", "upper", false, shapeStreamGap)
	n := Network{ShapeClearance: true, Nodes: nodes, Lanes: lanes, Stations: stations}
	n.Nodes = append(n.Nodes,
		Node{ID: "first-from", Position: Point{Y: shapeProbeY}}, Node{ID: "first-to", Position: Point{X: 400, Y: shapeProbeY}},
		Node{ID: "second-from", Position: from}, Node{ID: "second-to", Position: to})
	n.Lanes = append(n.Lanes,
		Lane{ID: "first", From: "first-from", To: "first-to", SpeedLimit: 14, VehicleClasses: shapeUnionClasses},
		Lane{ID: "second", From: "second-from", To: "second-to", SpeedLimit: 14, VehicleClasses: shapeUnionClasses})
	return n
}

// shapeProbe is a lane second of shapeProbeNetwork.
// gap is the smallest distance between the two lanes, and near the
// distance along lane second of its point closest to (200, shapeProbeY).
type shapeProbe struct {
	name      string
	from, to  Point
	gap, near float64
}

// shapeParallelProbe returns lane second 400 m long, parallel to lane first
// and gap meters to the south of it, from X = 100.
func shapeParallelProbe(gap float64) shapeProbe {
	return shapeProbe{name: fmt.Sprintf("parallel at %g m", gap), from: Point{X: 100, Y: shapeProbeY - gap}, to: Point{X: 500, Y: shapeProbeY - gap}, gap: gap, near: 100}
}

// shapeEndProbe returns lane second 101 m long at a heading of degrees
// from the east, that ends gap meters south of lane first at X = 200.
func shapeEndProbe(degrees, gap float64) shapeProbe {
	s, c := math.Sincos(degrees * math.Pi / 180)
	end := Point{X: 200, Y: shapeProbeY - gap}
	return shapeProbe{name: fmt.Sprintf("end at %g degrees %g m away", degrees, gap), from: Point{X: end.X - 101*c, Y: end.Y - 101*s}, to: end, gap: gap, near: 101}
}

// newShapeProbe returns a simulation of a network of shapeProbeNetwork
// with pod p1 of one class and pod p2 of another, at berths of station a1.
func newShapeProbe(t *testing.T, probe shapeProbe, classes [2]VehicleClass) (*Simulation, error) {
	t.Helper()
	fleet := []Placement{{ID: "p1", Class: classes[0], StationID: "a1", BerthID: "a1-1"}, {ID: "p2", Class: classes[1], StationID: "a1", BerthID: "a1-2"}}
	return NewFleetWithOrderContract(shapeProbeNetwork(probe.from, probe.to), fleet, ExpressOrderContract)
}

// shapeProbeClasses holds the class pairs of the probe tests.
var shapeProbeClasses = [][2]VehicleClass{
	{ExpressClass, ExpressClass}, {ExpressClass, CompactClass}, {CompactClass, ExpressClass}, {CompactClass, CompactClass},
}

// TestShapeOracleControls places two pods by hand on two lanes that the
// shape table holds as a shape pair, although their bodies can overlap,
// and checks that the oracle fails where Check passes. The table is the
// table of a network whose lanes the audit certifies, and a copy of the
// network moves lane second closer to lane first, so that the table holds
// the pair in error. The audit refuses the network of the copy. Check
// takes the side-by-side distance of the classes of the pods from the
// table, and the pods are further apart than that, so Check passes. The
// controls where the bodies do not overlap pass the oracle.
func TestShapeOracleControls(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name             string
		certified, moved shapeProbe
		classes          [2]VehicleClass
		first, second    float64
		overlap          bool
	}{
		// Lane second is 3.5 m south, and p2 is 5 m east of p1: the
		// pods are 6.1 m apart.
		{name: "Express pods", certified: shapeParallelProbe(5), moved: shapeParallelProbe(3.5), classes: [2]VehicleClass{ExpressClass, ExpressClass}, first: 200, second: 105, overlap: true},
		{name: "Express and Compact pods", certified: shapeParallelProbe(5), moved: shapeParallelProbe(3.5), classes: [2]VehicleClass{ExpressClass, CompactClass}, first: 200, second: 105, overlap: true},
		{name: "Compact pods", certified: shapeParallelProbe(5), moved: shapeParallelProbe(3.5), classes: [2]VehicleClass{CompactClass, CompactClass}, first: 200, second: 105},
		{name: "Express pods 9 m apart along the lanes", certified: shapeParallelProbe(5), moved: shapeParallelProbe(3.5), classes: [2]VehicleClass{ExpressClass, ExpressClass}, first: 200, second: 109},
		// Lane second comes from the south and ends 4.5 m south of lane
		// first, and p2 is at its end, 4.5 m from p1.
		{name: "Express pods at a lane end", certified: shapeEndProbe(90, 10), moved: shapeEndProbe(90, 4.5), classes: [2]VehicleClass{ExpressClass, ExpressClass}, first: 200, second: 101, overlap: true},
		{name: "Compact pod at a lane end", certified: shapeEndProbe(90, 10), moved: shapeEndProbe(90, 4.5), classes: [2]VehicleClass{ExpressClass, CompactClass}, first: 200, second: 101, overlap: true},
		{name: "Express pods 3 m before a lane end", certified: shapeEndProbe(90, 10), moved: shapeEndProbe(90, 4.5), classes: [2]VehicleClass{ExpressClass, ExpressClass}, first: 200, second: 98},
	} {
		t.Run(fmt.Sprintf("%s on %s", test.name, test.moved.name), func(t *testing.T) {
			t.Parallel()
			if _, err := newShapeProbe(t, test.moved, test.classes); err == nil || !strings.Contains(err.Error(), "are less than") {
				t.Fatalf("audit of the moved lanes: %v, want a refusal", err)
			}
			s, err := newShapeProbe(t, test.certified, test.classes)
			if err != nil {
				t.Fatal(err)
			}
			first, second := s.graph.lanes["first"], s.graph.lanes["second"]
			side, ok := s.shapes.clearance(laneKey(first), laneKey(second))
			if !ok {
				t.Fatal("lanes first and second do not form a shape pair")
			}
			indexes := *s.networkIndexes
			indexes.network.Nodes = slices.Clone(indexes.network.Nodes)
			for index := range indexes.network.Nodes {
				switch node := &indexes.network.Nodes[index]; node.ID {
				case "second-from":
					node.Position = test.moved.from
				case "second-to":
					node.Position = test.moved.to
				}
			}
			s.networkIndexes = &indexes
			oraclePlace(s, 0, "first", test.first)
			oraclePlace(s, 1, "second", test.second)
			distance := pointDistance(s.vehicles[0].Pod.Position, s.vehicles[1].Pod.Position)
			if minimum := shapeClassClearance(test.classes[0], test.classes[1]); distance < minimum {
				t.Fatalf("the pods are %.3f m apart, less than %g m, so Check fails", distance, minimum)
			}
			if _, err = s.SafetyObservation().Check(); err != nil {
				t.Fatalf("Check: %v", err)
			}
			err = s.lateralBodyCheck()
			t.Logf("pair at %g m, pods %.3f m apart, oracle %v", side, distance, err)
			if failed := err != nil; failed != test.overlap {
				t.Fatalf("oracle error %v, want an error %v", err, test.overlap)
			}
			if err == nil {
				return
			}
			for _, want := range []string{
				fmt.Sprintf("tick %d: the bodies of a shape pair of lanes first and second are not disjoint", s.tick),
				fmt.Sprintf("; pod p1 (%s) on lane first at %.3f m, body", test.classes[0], test.first),
				fmt.Sprintf("; pod p2 (%s) on lane second at %.3f m, body", test.classes[1], test.second),
				oracleBodyText(test.classes[0]), oracleBodyText(test.classes[1]),
			} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("oracle error %q does not contain %q", err, want)
				}
			}
		})
	}
}

// TestShapeOracleAuditedPairs checks the shape pairs of the audit with the
// oracle. For lane second at each probe, parallel to lane first and
// ending south of it at three headings, at distances on each side of the
// limit of the sweep, the test builds the network. When the audit accepts
// it and lanes first and second form a shape pair, the oracle passes at
// each placement of two pods near the closest points of the two lanes, for
// each pair of classes, and the closest placement is at the gap of the
// lanes. A probe inside the limit of the sweep and outside
// the side-by-side distance is the case that Check alone does not detect:
// if the table held it, the oracle would fail.
func TestShapeOracleAuditedPairs(t *testing.T) {
	t.Parallel()
	probes := []shapeProbe{shapeParallelProbe(3.9), shapeParallelProbe(4.01), shapeParallelProbe(5)}
	for _, degrees := range []float64{15, 45, 90} {
		for _, gap := range []float64{4.5, 5.5, 6.5, 8} {
			probes = append(probes, shapeEndProbe(degrees, gap))
		}
	}
	pairs := 0
	for _, probe := range probes {
		for _, classes := range shapeProbeClasses {
			s, err := newShapeProbe(t, probe, classes)
			if err != nil {
				if !strings.Contains(err.Error(), "are less than") {
					t.Fatalf("%s: %v", probe.name, err)
				}
				continue
			}
			first, second := s.graph.lanes["first"], s.graph.lanes["second"]
			if _, ok := s.shapes.clearance(laneKey(first), laneKey(second)); !ok {
				continue
			}
			pairs++
			length, closest := s.graph.lengths[second], math.Inf(1)
			// Each probe comes closest to lane first at X = 200: at the end
			// of lane second, or at the point of a parallel lane second at
			// the same X. The samples cover 10 m of each lane on each side
			// of these points.
			for along := -10.0; along <= 10; along += 0.25 {
				for offset := -10.0; offset <= 10; offset += 0.25 {
					oraclePlace(s, 0, "first", 200+along)
					oraclePlace(s, 1, "second", min(max(probe.near+offset, 0), length))
					closest = min(closest, pointDistance(s.vehicles[0].Pod.Position, s.vehicles[1].Pod.Position))
					if err := s.lateralBodyCheck(); err != nil {
						t.Fatalf("%s, %s then %s: %v", probe.name, classes[0], classes[1], err)
					}
				}
			}
			if math.Abs(closest-probe.gap) > 1e-6 {
				t.Fatalf("%s: closest pods %.6f m apart, want the gap of the lanes", probe.name, closest)
			}
		}
	}
	if pairs == 0 {
		t.Fatal("the audit made no shape pair of a probe, so the test does not test the table")
	}
	t.Logf("%d shape pairs of the probes pass the oracle", pairs)
}

// TestOracleBodiesMatchClassBodies checks that the literal bodies of the
// oracle equal the grown class bodies of the certification, so that a
// change to one of them shows.
func TestOracleBodiesMatchClassBodies(t *testing.T) {
	t.Parallel()
	for _, class := range []VehicleClass{"", LegacyClass, CompactClass, GroupClass, ExpressClass} {
		oracle, certified := oracleClassBody(class), classShapeBody(class)
		if math.Abs(oracle.rear-certified.rear) > 1e-12 || math.Abs(oracle.front-certified.front) > 1e-12 || math.Abs(oracle.halfWidth-certified.halfWidth) > 1e-12 {
			t.Errorf("class %q: oracle body %+v, class body %+v", class, oracle, certified)
		}
	}
}

// TestOracleTurns checks the refinement of a turn of the oracle. An
// Express body at the origin turns from heading 0 to 90 degrees. Its
// corners reach at most r = hypot(4, 2) = 4.472 m in X, near 26.6 degrees.
// An Express body that heads north at X = x has its west face at x - 2.
// At x = 7.2, and at x = r + 2.01, where the turn clears the face by
// 0.01 m, the bodies are disjoint. One rectangle around the whole turn, or
// a padded sample near 26.6 degrees, would reach past the face. At
// x = r + 1.99 and at x = 6.3, the turn meets the face by 0.01 m and more,
// but the body at 0 and at 90 degrees clears it, so only the turn fails
// the oracle.
func TestOracleTurns(t *testing.T) {
	t.Parallel()
	body := oracleClassBody(ExpressClass)
	reach := math.Hypot(4, 2)
	turning := oraclePose{turn: math.Pi / 2}
	other := func(x float64) oraclePose { return oraclePose{x: x, heading: math.Pi / 2} }
	for _, test := range []struct {
		x       float64
		overlap bool
	}{
		{x: 7.2}, {x: reach + 2.01}, {x: reach + 1.99, overlap: true}, {x: 6.3, overlap: true},
	} {
		if disjoint := oraclePosesDisjoint(body, turning, body, other(test.x)); disjoint == test.overlap {
			t.Errorf("the turn and the body at X = %.6f: disjoint %v, want %v", test.x, disjoint, !test.overlap)
		}
		if !test.overlap {
			continue
		}
		for _, heading := range []float64{0, math.Pi / 2} {
			if !oraclePosesDisjoint(body, oraclePose{heading: heading}, body, other(test.x)) {
				t.Errorf("the body at heading %g and the body at X = %.6f are not disjoint", heading, test.x)
			}
		}
	}
}

// shapeCrossingNetwork returns shapeProbeNetwork with lane first on plane
// "deck" and a curved lane second on plane "tunnel". Lane second comes
// from the south, passes under lane first, and turns east to end 10 m
// north of it, where lane exit on plane "deck" goes on east. Near the end
// of lane second, the envelope of a pod there holds the plane of lane exit,
// so the planes of the two lanes are not separated there, and the sweep
// certifies that part at 10 m. At the crossing, the planes are separated.
func shapeCrossingNetwork() Network {
	n := shapeProbeNetwork(Point{X: 150, Y: shapeProbeY - 80}, Point{X: 300, Y: shapeProbeY + 10})
	for index := range n.Lanes {
		switch lane := &n.Lanes[index]; lane.ID {
		case "first":
			lane.SeparationGroup = "deck"
		case "second":
			lane.SeparationGroup, lane.Control = "tunnel", &Point{X: 150, Y: shapeProbeY + 10}
		}
	}
	n.Nodes = append(n.Nodes, Node{ID: "exit-to", Position: Point{X: 400, Y: shapeProbeY + 10}})
	n.Lanes = append(n.Lanes, Lane{ID: "exit", From: "second-to", To: "exit-to", SpeedLimit: 14, VehicleClasses: shapeUnionClasses, SeparationGroup: "deck"})
	return n
}

// TestShapeOracleSeparatePlanes places a Compact pod p2 on lane second
// where lane second of shapeCrossingNetwork passes under lane first, and an
// Express pod p1 on lane first 5 m east of p2. The two lanes form a shape
// pair, and the bodies of the pods overlap in plan, but the pods are on
// different planes, so the oracle and Check pass.
//
// Each other case changes a copy of the network that the oracle reads. A
// portal node with two lanes, and in one case a berth, is due south of p2,
// at a distance from p2 near the radius of the Compact body, 4.477 m, and
// more than the radius of the Express body from p1. The radius of the
// Compact body is at its front. The rear corners are at 3.377 m. A
// portal within the radius puts p2 on its planes as well: with a lane on
// the plane of p1, or a lane or a berth with no plane, the oracle fails.
// A portal just beyond the radius does not touch the body of p2, so p2 is
// wholly on the plane of its lane, as the network defines the planes, and
// the oracle passes. With lane second on no plane, the oracle fails.
func TestShapeOracleSeparatePlanes(t *testing.T) {
	t.Parallel()
	fleet := []Placement{{ID: "p1", Class: ExpressClass, StationID: "a1", BerthID: "a1-1"}, {ID: "p2", Class: CompactClass, StationID: "a1", BerthID: "a1-2"}}
	s, err := NewFleetWithOrderContract(shapeCrossingNetwork(), fleet, ExpressOrderContract)
	if err != nil {
		t.Fatal(err)
	}
	first, second := s.graph.lanes["first"], s.graph.lanes["second"]
	if _, ok := s.shapes.clearance(laneKey(first), laneKey(second)); !ok {
		t.Fatal("lanes first and second do not form a shape pair")
	}
	points := s.network.Polyline(s.network.Lanes[second])
	crossing, best := 0.0, math.Inf(1)
	for distance := 0.0; distance <= s.graph.lengths[second]; distance += 0.01 {
		if gap := math.Abs(pointAlong(points, distance).Y - shapeProbeY); gap < best {
			crossing, best = distance, gap
		}
	}
	oraclePlace(s, 1, "second", crossing)
	oraclePlace(s, 0, "first", s.vehicles[1].Pod.Position.X+5)
	if oracleDisjoint(s.newLateralPod(&s.vehicles[0], first), s.newLateralPod(&s.vehicles[1], second)) {
		t.Fatal("the bodies at the crossing are disjoint in plan, so the case does not test the planes")
	}
	if _, err := s.SafetyObservation().Check(); err != nil {
		t.Fatalf("Check: %v", err)
	}
	base := s.networkIndexes
	radius := math.Hypot(4.2, 1.55)
	at := s.vehicles[1].Pod.Position
	for _, test := range []struct {
		name string
		// distance is the distance from p2 to the portal, or 0 for no
		// portal. groups are the planes of the two portal lanes, and berth
		// adds a berth with no plane at the portal.
		distance float64
		groups   [2]string
		berth    bool
		// unplaned leaves lane second on no plane.
		unplaned bool
		fail     bool
	}{
		{name: "no portal"},
		{name: "portal with the plane of p1 within the radius", distance: radius - 0.05, groups: [2]string{"tunnel", "deck"}, fail: true},
		{name: "portal with a lane on no plane within the radius", distance: radius - 0.05, groups: [2]string{"tunnel", ""}, fail: true},
		{name: "portal with a berth on no plane within the radius", distance: radius - 0.05, groups: [2]string{"tunnel", "tunnel"}, berth: true, fail: true},
		{name: "portal with the plane of p1 beyond the radius", distance: radius + 0.01, groups: [2]string{"tunnel", "deck"}},
		{name: "lane second on no plane", unplaned: true, fail: true},
	} {
		indexes := *base
		network := &indexes.network
		network.Nodes, network.Lanes, network.Stations = slices.Clone(network.Nodes), slices.Clone(network.Lanes), slices.Clone(network.Stations)
		if test.unplaned {
			network.Lanes[second].SeparationGroup = ""
		}
		if test.distance != 0 {
			portal := Point{X: at.X, Y: at.Y - test.distance}
			if pointDistance(portal, s.vehicles[0].Pod.Position) <= math.Hypot(4, 2)+0.1 {
				t.Fatalf("%s: the portal is within the radius of p1, so the case does not test the planes of p2", test.name)
			}
			network.Nodes = append(network.Nodes,
				Node{ID: "portal", Position: portal}, Node{ID: "portal-west", Position: Point{X: portal.X - 1000, Y: portal.Y - 1000}}, Node{ID: "portal-east", Position: Point{X: portal.X + 1000, Y: portal.Y - 1000}})
			network.Lanes = append(network.Lanes,
				Lane{ID: "portal-in", From: "portal-west", To: "portal", SpeedLimit: 14, SeparationGroup: test.groups[0]},
				Lane{ID: "portal-out", From: "portal", To: "portal-east", SpeedLimit: 14, SeparationGroup: test.groups[1]})
			if test.berth {
				network.Stations[0].Berths = append(slices.Clone(network.Stations[0].Berths), Berth{ID: "portal-berth", Node: "portal"})
			}
		}
		s.networkIndexes = &indexes
		if err := s.lateralBodyCheck(); (err != nil) != test.fail {
			t.Errorf("%s: oracle error %v, want an error %v", test.name, err, test.fail)
		}
	}
}
