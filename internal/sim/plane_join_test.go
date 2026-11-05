package sim

import (
	"errors"
	"math"
	"testing"
)

// planeJoinCase describes a ring where the pod of station one rides "feed",
// "far", and "near" into the join with "main", and the pod of station two
// rides "cross".
type planeJoinCase struct {
	// cross is the Y offset of the cross lane from the join.
	cross float64
	// crossEnd is the X of the west end of the cross lane. The near lane is at
	// X 300. A value below 300 makes the cross lane pass over the near lane.
	crossEnd float64
	// approach is the plane of feed, far, and near.
	approach string
	// joined is the plane of main, which joins near.
	joined string
	// crossing is the plane of the cross lane and its feed and exit lanes.
	crossing string
	curved   bool
	// rear reverses every lane, so that near leaves the join.
	rear bool
	// classes overrides the classes of the lanes and the berths.
	classes []string
}

func planeJoinNetwork(t *testing.T, tc planeJoinCase) Network {
	t.Helper()
	allowed := tc.classes
	if allowed == nil {
		allowed = []string{"legacy", "compact", "group"}
	}
	classes := largeGeometryClasses(t, allowed...)
	entryOne, berthOne := Point{X: 160, Y: 500}, Point{X: 230, Y: 600}
	if tc.crossEnd > 300 {
		entryOne, berthOne = Point{X: 500, Y: 500}, Point{X: 400, Y: 600}
	}
	turn := Point{X: 160, Y: 440}
	if tc.crossEnd > 300 {
		turn = Point{X: tc.crossEnd + 9, Y: 440}
	}
	nodes := map[string]Point{
		"one-exit": {X: 300, Y: 500}, "top": {X: 300, Y: 300}, "mid": {X: 300, Y: 100}, "join": {X: 300},
		"east": {X: 700}, "two-entry": {X: 700, Y: 400}, "two-exit": {X: 440, Y: 400}, "two-berth": {X: 570, Y: 500},
		"cross-east": {X: 440, Y: tc.cross}, "cross-west": {X: tc.crossEnd, Y: tc.cross}, "turn": turn,
		"one-entry": entryOne, "one-berth": berthOne,
	}
	type row struct{ id, from, to, plane string }
	rows := []row{
		{"one-out", "one-berth", "one-exit", "station"}, {"feed", "one-exit", "top", tc.approach}, {"far", "top", "mid", tc.approach}, {"near", "mid", "join", tc.approach},
		{"main", "join", "east", tc.joined}, {"up", "east", "two-entry", "station"}, {"two-in", "two-entry", "two-berth", "station"}, {"two-out", "two-berth", "two-exit", "station"},
		{"cross-feed", "two-exit", "cross-east", tc.crossing}, {"cross", "cross-east", "cross-west", tc.crossing}, {"cross-exit", "cross-west", "turn", tc.crossing},
		{"turn-exit", "turn", "one-entry", "station"}, {"one-in", "one-entry", "one-berth", "station"},
		{"one-through", "one-entry", "one-exit", "station"}, {"two-through", "two-entry", "two-exit", "station"},
	}
	n := Network{Stations: []Station{
		{ID: "one", Entry: "one-entry", Exit: "one-exit", VehicleClasses: classes, Berths: []Berth{{ID: "one-1", Node: "one-berth", VehicleClasses: classes, SeparationGroup: "station"}}},
		{ID: "two", Entry: "two-entry", Exit: "two-exit", VehicleClasses: classes, Berths: []Berth{{ID: "two-1", Node: "two-berth", VehicleClasses: classes, SeparationGroup: "station"}}},
	}}
	for id, position := range nodes {
		n.Nodes = append(n.Nodes, Node{ID: id, Position: position})
	}
	for _, r := range rows {
		lane := Lane{ID: r.id, From: r.from, To: r.to, SpeedLimit: 14, VehicleClasses: classes, SeparationGroup: r.plane}
		if tc.curved && r.id == "near" {
			lane.Control = &Point{X: 340, Y: 50}
		}
		if tc.rear {
			lane.From, lane.To = lane.To, lane.From
		}
		n.Lanes = append(n.Lanes, lane)
	}
	if tc.rear {
		for i := range n.Stations {
			n.Stations[i].Entry, n.Stations[i].Exit = n.Stations[i].Exit, n.Stations[i].Entry
		}
	}
	return n
}

// planeBerthNetwork has a lane "leave" from a berth on plane Q that passes
// over the lane "cross" on plane Q, 25 meters from the berth. No other lane
// near them is on plane Q. rear reverses every lane, so that the lane arrives
// at the berth.
func planeBerthNetwork(t *testing.T, rear bool) Network {
	t.Helper()
	classes := largeGeometryClasses(t, "legacy", "compact", "group")
	nodes := map[string]Point{
		"berth": {}, "exit": {Y: 200}, "entry": {X: -100, Y: -150}, "two-entry": {X: 200, Y: 200}, "two-exit": {X: 200, Y: 25}, "two-berth": {X: 300, Y: 110},
		"cross-east": {X: 100, Y: 25}, "cross-west": {X: -40, Y: 25}, "back": {X: -100, Y: -60},
	}
	type row struct{ id, from, to, plane string }
	rows := []row{
		{"leave", "berth", "exit", "A"}, {"on", "exit", "two-entry", "A"}, {"two-in", "two-entry", "two-berth", "Q"}, {"two-out", "two-berth", "two-exit", "Q"}, {"two-through", "two-entry", "two-exit", "Q"},
		{"cross-feed", "two-exit", "cross-east", "Q"}, {"cross", "cross-east", "cross-west", "Q"}, {"cross-exit", "cross-west", "back", "Q"}, {"back-entry", "back", "entry", "A"},
		{"in", "entry", "berth", "A"}, {"through", "entry", "exit", "A"},
	}
	n := Network{Stations: []Station{
		{ID: "one", Entry: "entry", Exit: "exit", VehicleClasses: classes, Berths: []Berth{{ID: "one-1", Node: "berth", VehicleClasses: classes, SeparationGroup: "Q"}}},
		{ID: "two", Entry: "two-entry", Exit: "two-exit", VehicleClasses: classes, Berths: []Berth{{ID: "two-1", Node: "two-berth", VehicleClasses: classes, SeparationGroup: "Q"}}},
	}}
	for id, position := range nodes {
		n.Nodes = append(n.Nodes, Node{ID: id, Position: position})
	}
	for _, r := range rows {
		lane := Lane{ID: r.id, From: r.from, To: r.to, SpeedLimit: 14, VehicleClasses: classes, SeparationGroup: r.plane}
		if r.id == "through" {
			lane.Control = &Point{X: -300, Y: 100}
		}
		if rear {
			lane.From, lane.To = lane.To, lane.From
		}
		n.Lanes = append(n.Lanes, lane)
	}
	if rear {
		for i := range n.Stations {
			n.Stations[i].Entry, n.Stations[i].Exit = n.Stations[i].Exit, n.Stations[i].Entry
		}
	}
	return n
}

// planeUTurnNetwork has a lane "uturn" between "ahead" and "return", with the
// ends of "uturn" chord meters apart.
func planeUTurnNetwork(t *testing.T, chord float64, ahead, uturn, back string) Network {
	t.Helper()
	classes := largeGeometryClasses(t, "group")
	return Network{Nodes: []Node{{ID: "start", Position: Point{X: -200}}, {ID: "turn"}, {ID: "back", Position: Point{Y: chord}}, {ID: "end", Position: Point{X: -200, Y: chord}}},
		Lanes: []Lane{{ID: "ahead", From: "start", To: "turn", SpeedLimit: 14, VehicleClasses: classes, SeparationGroup: ahead},
			{ID: "uturn", From: "turn", To: "back", SpeedLimit: 14, VehicleClasses: classes, SeparationGroup: uturn, Control: &Point{X: 120, Y: chord / 2}},
			{ID: "return", From: "back", To: "end", SpeedLimit: 14, VehicleClasses: classes, SeparationGroup: back}}}
}

func TestLargeGeometryLaneReachAudit(t *testing.T) {
	t.Parallel()
	join := func(cross float64, curved, rear bool) func(*testing.T) Network {
		return func(t *testing.T) Network {
			t.Helper()
			return planeJoinNetwork(t, planeJoinCase{cross: cross, crossEnd: 160, approach: "A", joined: "Q", crossing: "Q", curved: curved, rear: rear})
		}
	}
	const nearRefusal = `nonincident paths "cross" and "near" are less than 20 meters apart`
	for _, tc := range []struct {
		name    string
		network func(*testing.T) Network
		want    string
	}{
		{"straight 24", join(24, false, false), nearRefusal},
		{"straight 20", join(20, false, false), nearRefusal},
		{"quadratic 24", join(24, true, false), nearRefusal},
		{"quadratic 20", join(20, true, false), nearRefusal},
		{"rear 34", join(34, false, true), nearRefusal},
		// The rule is for the whole lane, not only for its end.
		{"same lane far from the join", join(80, false, false), nearRefusal},
		{"control on one plane", func(t *testing.T) Network {
			t.Helper()
			return planeJoinNetwork(t, planeJoinCase{cross: 30, crossEnd: 321, approach: "Q", joined: "Q", crossing: "Q"})
		}, ""},
		{"flyover two lanes from a join", func(t *testing.T) Network {
			t.Helper()
			return planeJoinNetwork(t, planeJoinCase{cross: 200, crossEnd: 160, approach: "A", joined: "Q", crossing: "Q"})
		}, ""},
		{"small classes keep the lane plane", func(t *testing.T) Network {
			t.Helper()
			return planeJoinNetwork(t, planeJoinCase{cross: 12, crossEnd: 160, approach: "A", joined: "Q", crossing: "Q", classes: []string{"legacy", "compact"}})
		}, ""},
		{"berth at the From node", func(t *testing.T) Network { t.Helper(); return planeBerthNetwork(t, false) }, `nonincident paths "cross" and "leave" are less than 20 meters apart`},
		{"berth at the To node", func(t *testing.T) Network { t.Helper(); return planeBerthNetwork(t, true) }, ""},
		{"u-turn chord 4 one plane", func(t *testing.T) Network { t.Helper(); return planeUTurnNetwork(t, 4, "Q", "Q", "Q") }, `nonincident paths "ahead" and "return" are less than 20 meters apart`},
		{"u-turn chord 4 three planes", func(t *testing.T) Network { t.Helper(); return planeUTurnNetwork(t, 4, "A", "B", "Q") }, `nonincident paths "ahead" and "return" are less than 20 meters apart`},
		{"u-turn chord 25 three planes", func(t *testing.T) Network { t.Helper(); return planeUTurnNetwork(t, 25, "A", "B", "Q") }, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			n := tc.network(t)
			for name, err := range map[string]error{"audit": n.ValidateBankGeometry(), "preparation": prepareError(n)} {
				if tc.want == "" && err != nil {
					t.Errorf("%s refused: %v", name, err)
				}
				if tc.want != "" && (err == nil || err.Error() != tc.want) {
					t.Errorf("%s error = %v, want %s", name, err, tc.want)
				}
			}
		})
	}
}

// The banks make the audit read a small pair of a network with large lanes.
func TestLargeGeometryReachSmallPairKeepsLanePlane(t *testing.T) {
	t.Parallel()
	n := planeJoinNetwork(t, planeJoinCase{cross: 24, crossEnd: 160, approach: "A", joined: "Q", crossing: "Q", classes: []string{"legacy", "compact"}})
	n.Stations[0].Banks = []StationBank{{ID: "bank", Entry: n.Stations[0].Entry, Exit: n.Stations[0].Exit, BerthIDs: []string{"one-1"}}}
	n.Nodes = append(n.Nodes, Node{ID: "large-a", Position: Point{X: 2000}}, Node{ID: "large-b", Position: Point{X: 2100}})
	n.Lanes = append(n.Lanes, Lane{ID: "large", From: "large-a", To: "large-b", SpeedLimit: 14, VehicleClasses: largeGeometryClasses(t, "group")})
	if !n.hasStationBanks() || !n.hasLargeGeometry() {
		t.Fatal("fixture needs banks and a large lane")
	}
	if err := n.ValidateBankGeometry(); err != nil {
		t.Fatal(err)
	}
}

func prepareError(n Network) error {
	_, err := PrepareNetwork(n)
	return err
}

// A pair that only the reach refuses comes after a pair without a plane
// exemption, even when the scan reaches it first.
func TestLargeGeometryReachRefusalOrder(t *testing.T) {
	t.Parallel()
	n := planeJoinNetwork(t, planeJoinCase{cross: 24, crossEnd: 160, approach: "A", joined: "Q", crossing: "Q"})
	const reach = `nonincident paths "cross" and "near" are less than 20 meters apart`
	if err := n.ValidateBankGeometry(); err == nil || err.Error() != reach {
		t.Fatalf("reach refusal = %v", err)
	}
	classes := n.Lanes[0].VehicleClasses
	n.Nodes = append(n.Nodes, Node{ID: "late-a", Position: Point{X: 2000}}, Node{ID: "late-b", Position: Point{X: 2100}}, Node{ID: "late-c", Position: Point{X: 2000, Y: 10}}, Node{ID: "late-d", Position: Point{X: 2100, Y: 10}})
	n.Lanes = append(n.Lanes, Lane{ID: "late-first", From: "late-a", To: "late-b", SpeedLimit: 14, VehicleClasses: classes}, Lane{ID: "late-second", From: "late-c", To: "late-d", SpeedLimit: 14, VehicleClasses: classes})
	const plain = `nonincident paths "late-first" and "late-second" are less than 20 meters apart`
	if err := n.ValidateBankGeometry(); err == nil || err.Error() != plain {
		t.Fatalf("refusal = %v, want %s", err, plain)
	}
}

// planeJoinReports runs a pair of journeys, with pod a late by each offset in
// quarters of a second. It returns
// the first separation report and the smallest center gap of the runs.
func planeJoinReports(t *testing.T, p *PreparedNetwork, offsets []int) (report *SeparationError, smallest float64) {
	t.Helper()
	smallest = math.Inf(1)
	for _, offset := range offsets {
		s, err := p.NewFleet([]Placement{{ID: "a", Class: GroupClass, StationID: "one"}, {ID: "b", Class: GroupClass, StationID: "two"}})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.RequestJourney("b", "one"); err != nil {
			t.Fatal(err)
		}
		for tick := range 140 * TicksPerSecond {
			if tick == offset*TicksPerSecond/4 {
				// A pod that clears its berth for the other pod has a journey.
				if s.RequestJourney("a", "two") != nil {
					break
				}
			}
			s.Step()
			o := s.SafetyObservation()
			if _, err := o.Check(); err != nil {
				var separation *SeparationError
				if !errors.As(err, &separation) {
					t.Fatal(err)
				}
				if report == nil {
					report = separation
				}
			}
			smallest = min(smallest, pointDistance(o.Pods[0].Position, o.Pods[1].Position))
			if s.completed == 2 {
				break
			}
		}
	}
	return report, smallest
}

// The audit refuses the join because a real run breaks the 20 meter rule. The
// run uses a network that the audit accepts, with the observer planes of the
// refused network.
func TestLargeGeometryJoinRefusalMatchesObserver(t *testing.T) {
	t.Parallel()
	offsets := []int{14, 15, 16, 17, 18}
	p, err := PrepareNetwork(planeJoinNetwork(t, planeJoinCase{cross: 24, crossEnd: 160, approach: "A", joined: "Q", crossing: "Z"}))
	if err != nil {
		t.Fatal(err)
	}
	if report, _ := planeJoinReports(t, p, offsets); report != nil {
		t.Fatalf("accepted planes reported %v", report)
	}
	location := p.laneSafety["cross"]
	location.SeparationGroup = "Q"
	p.laneSafety["cross"] = location
	report, smallest := planeJoinReports(t, p, offsets)
	if report == nil {
		t.Fatal("the refused planes gave no report")
	}
	t.Logf("first report %v, smallest gap %.3f", report, smallest)
	control, err := PrepareNetwork(planeJoinNetwork(t, planeJoinCase{cross: 30, crossEnd: 321, approach: "Q", joined: "Q", crossing: "Q"}))
	if err != nil {
		t.Fatal(err)
	}
	report, smallest = planeJoinReports(t, control, offsets)
	if report != nil {
		t.Fatalf("control reported %v", report)
	}
	t.Logf("control smallest gap %.3f", smallest)
}

// planeJoinWithDefect returns the network that the reach refuses, with one
// more lane, far from the others, that adds another defect.
func planeJoinWithDefect(t *testing.T, lane Lane, end Point) Network {
	t.Helper()
	n := planeJoinNetwork(t, planeJoinCase{cross: 24, crossEnd: 160, approach: "A", joined: "Q", crossing: "Q"})
	start := Point{X: 2000}
	n.Nodes = append(n.Nodes, Node{ID: "defect-a", Position: start}, Node{ID: "defect-b", Position: Point{X: start.X + end.X, Y: end.Y}})
	lane.From, lane.To, lane.SpeedLimit = "defect-a", "defect-b", 14
	if lane.Control != nil {
		lane.Control = &Point{X: start.X + lane.Control.X, Y: lane.Control.Y}
	}
	n.Lanes = append(n.Lanes, lane)
	return n
}

// named gives the stations the names that the Express bounds need.
func named(n Network) Network {
	for index := range n.Stations {
		n.Stations[index].Name = n.Stations[index].ID
	}
	return n
}

// A refusal that the network had before the reach rule keeps its place: the
// reach refusal comes after every other check of preparation and of a fleet.
// The audit alone has no other check.
func TestLargeGeometryReachRefusalComesLast(t *testing.T) {
	t.Parallel()
	const reach = `nonincident paths "cross" and "near" are less than 20 meters apart`
	legacy, express := largeGeometryClasses(t, "legacy"), largeGeometryClasses(t, "express")
	placements := []Placement{{ID: "p", StationID: "one"}}
	entries := map[string]func(Network, []Placement) error{
		"PrepareNetwork":           func(n Network, _ []Placement) error { return prepareError(n) },
		"ValidateNetwork":          func(n Network, _ []Placement) error { return ValidateNetwork(n) },
		"ValidateNetworkClearance": func(n Network, _ []Placement) error { return ValidateNetworkClearance(n, NewLaneClearance(n)) },
		"NewFleet": func(n Network, p []Placement) error {
			_, err := NewFleet(n, p)
			return err
		},
		"ValidateFleet": ValidateFleet,
		"NewFleetWithOrderContract": func(n Network, p []Placement) error {
			_, err := NewFleetWithOrderContract(named(n), p, ExpressOrderContract)
			return err
		},
		"ValidateFleetWithOrderContract": func(n Network, p []Placement) error {
			return ValidateFleetWithOrderContract(named(n), p, ExpressOrderContract)
		},
	}
	for _, test := range []struct {
		name string
		lane Lane
		end  Point
		want string
	}{
		{"lane length", Lane{ID: "short", VehicleClasses: legacy}, Point{X: 10}, `lane "short" must be at least 24 meters long`},
		{"path shape", Lane{ID: "fold", VehicleClasses: express, Control: &Point{X: 100, Y: 100}}, Point{X: 10}, `lane "fold" has an unsupported large-vehicle path shape`},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			n := planeJoinWithDefect(t, test.lane, test.end)
			for name, entry := range entries {
				if err := entry(n, placements); err == nil || err.Error() != test.want {
					t.Errorf("%s: error = %v, want %s", name, err, test.want)
				}
			}
			if err := n.ValidateBankGeometry(); err == nil || err.Error() != reach {
				t.Errorf("audit error = %v, want %s", err, reach)
			}
		})
	}
	n := planeJoinNetwork(t, planeJoinCase{cross: 24, crossEnd: 160, approach: "A", joined: "Q", crossing: "Q"})
	for name, entry := range entries {
		if err := entry(n, placements); err == nil || err.Error() != reach {
			t.Errorf("%s: error = %v, want %s", name, err, reach)
		}
		var typed *PlaneReachError
		if err := entry(n, placements); !errors.As(err, &typed) {
			t.Errorf("%s: error %v has no PlaneReachError", name, err)
		}
	}
	// The checks of a fleet come before the reach refusal.
	bad := []Placement{{ID: "p", StationID: "nowhere"}}
	for _, name := range []string{"NewFleet", "ValidateFleet", "NewFleetWithOrderContract", "ValidateFleetWithOrderContract"} {
		if err := entries[name](n, bad); err == nil || err.Error() != `unknown start station "nowhere"` {
			t.Errorf("%s: fleet error = %v", name, err)
		}
	}
}
