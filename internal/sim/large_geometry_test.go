package sim

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
)

func largeGeometryClasses(t *testing.T, classes ...string) ClassSet {
	t.Helper()
	set, err := NewClassSet(classes...)
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func TestLargeGeometryLaneLengthAndCells(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		class  string
		length float64
		valid  bool
	}{
		{"legacy minimum", "legacy", 24, true}, {"compact minimum", "compact", 24, true},
		{"group short", "group", 39.99, false}, {"group minimum", "group", 40, true},
		{"express metadata short", "express", 39.99, false}, {"express metadata minimum", "express", 40, true},
		{"group next cell", "group", 60.001, true}, {"group long", "group", 1000, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			n := Network{Nodes: []Node{{ID: "a"}, {ID: "b", Position: Point{X: tc.length}}},
				Lanes: []Lane{{ID: "lane", From: "a", To: "b", SpeedLimit: 14, VehicleClasses: largeGeometryClasses(t, tc.class)}}}
			prepared, err := PrepareNetwork(n)
			if !tc.valid {
				if err == nil || !strings.Contains(err.Error(), "40 meters long") {
					t.Fatalf("large lane length guard: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			cells := prepared.laneCells["lane"]
			if cells.count() != n.LaneBlocks()[0] {
				t.Fatal("preparation changed the authored block count")
			}
			if tc.class == "group" || tc.class == "express" {
				for cell := range cells.count() {
					if cellOffset(cell+1, cells.count(), tc.length)-cellOffset(cell, cells.count(), tc.length) < 20 {
						t.Fatal("large lane has an actual cell below 20 meters")
					}
				}
			}
		})
	}
	curve := Network{Nodes: []Node{{ID: "a"}, {ID: "b", Position: Point{X: 30}}},
		Lanes: []Lane{{ID: "curve", From: "a", To: "b", SpeedLimit: 14, Control: &Point{X: 15, Y: 60}, VehicleClasses: largeGeometryClasses(t, "group")}}}
	if curve.Length(curve.Lanes[0]) < 40 {
		t.Fatal("curve fixture has no long arc")
	}
	if _, err := PrepareNetwork(curve); err != nil {
		t.Fatalf("preparation used chord length instead of actual arc length: %v", err)
	}
}

func TestLargeGeometryActualCellGuard(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		length float64
		count  int
		valid  bool
	}{{40, 2, true}, {40, 3, false}, {60.001, 3, true}, {60.001, 4, false}} {
		err := validateLargeLaneCells("actual", tc.length, tc.count)
		if (err == nil) != tc.valid {
			t.Fatalf("actual cell guard length %g count %d: %v", tc.length, tc.count, err)
		}
	}
}

func TestLargeGeometryNonincidentLaneAudit(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                    string
		firstLarge, secondLarge bool
		vertical                bool
		gap                     float64
		firstPlane, secondPlane string
		valid                   bool
	}{
		{"old ordinary overlap", false, false, false, 1, "", "", true},
		{"large first near", true, false, false, 15, "", "", false},
		{"large second near", false, true, false, 15, "", "", false},
		{"large vertical near", true, false, true, 15, "", "", false},
		{"large boundary", true, true, false, 20, "", "", true},
		{"large separate planes", true, false, false, 1, "upper", "lower", true},
		{"large unknown plane", true, false, false, 15, "upper", "", false},
		{"large same plane", true, false, false, 15, "upper", "upper", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			n := Network{Nodes: []Node{{ID: "a"}, {ID: "b", Position: Point{X: 100}}, {ID: "c", Position: Point{Y: tc.gap}}, {ID: "d", Position: Point{X: 100, Y: tc.gap}}},
				Lanes: []Lane{{ID: "first", From: "a", To: "b", SpeedLimit: 14, SeparationGroup: tc.firstPlane}, {ID: "second", From: "c", To: "d", SpeedLimit: 14, SeparationGroup: tc.secondPlane}}}
			if tc.vertical {
				n.Nodes[1].Position, n.Nodes[2].Position, n.Nodes[3].Position = Point{Y: 100}, Point{X: tc.gap}, Point{X: tc.gap, Y: 100}
			}
			for i, large := range []bool{tc.firstLarge, tc.secondLarge} {
				if large {
					n.Lanes[i].VehicleClasses = largeGeometryClasses(t, "group")
				}
			}
			err := n.ValidateBankGeometry()
			if (err == nil) != tc.valid {
				t.Fatalf("nonincident large lane audit: %v", err)
			}
			_, prepareErr := PrepareNetwork(n)
			if (prepareErr == nil) != tc.valid {
				t.Fatalf("preparation bypassed nonincident geometry: %v", prepareErr)
			}
		})
	}
	// A small pair cannot end the X scan before a later large path is checked.
	n := Network{Nodes: []Node{{ID: "a"}, {ID: "b", Position: Point{Y: 100}}, {ID: "c", Position: Point{X: 13}}, {ID: "d", Position: Point{X: 13, Y: 100}}, {ID: "e", Position: Point{X: 15}}, {ID: "f", Position: Point{X: 15, Y: 100}}},
		Lanes: []Lane{{ID: "first", From: "a", To: "b", SpeedLimit: 14}, {ID: "middle", From: "c", To: "d", SpeedLimit: 14}, {ID: "large", From: "e", To: "f", SpeedLimit: 14, VehicleClasses: largeGeometryClasses(t, "group")}}}
	n.Lanes[1].SeparationGroup, n.Lanes[2].SeparationGroup = "upper", "lower"
	if err := n.ValidateBankGeometry(); err == nil {
		t.Fatal("X scan skipped a later large interaction")
	}
}

func largeGeometryOrdinaryStation(t *testing.T) Network {
	t.Helper()
	large := largeGeometryClasses(t, "legacy", "group")
	return Network{Nodes: []Node{{ID: "entry", Position: Point{X: -50}}, {ID: "exit", Position: Point{X: 50}}, {ID: "berth", Position: Point{Y: 50}}},
		Lanes:    []Lane{{ID: "through", From: "entry", To: "exit", SpeedLimit: 14}, {ID: "in", From: "entry", To: "berth", SpeedLimit: 14}, {ID: "out", From: "berth", To: "exit", SpeedLimit: 14}},
		Stations: []Station{{ID: "station", Entry: "entry", Exit: "exit", VehicleClasses: large, Berths: []Berth{{ID: "berth", Node: "berth", VehicleClasses: large}}}}}
}

func TestLargeGeometryOrdinaryBerthAudit(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                     string
		gap                      float64
		stationClass, berthClass string
		plane, incoming          string
		valid                    bool
	}{
		{"near admitted berth", 15, "group", "group", "", "", false},
		{"boundary", 20, "group", "group", "", "", true},
		{"station excludes group", 15, "legacy", "group", "", "", true},
		{"berth excludes group", 15, "group", "legacy", "", "", true},
		{"distinct planes", 1, "group", "group", "upper", "upper", true},
		{"falsely distinct incoming plane", 15, "group", "group", "upper", "lower", false},
		{"excluded incoming plane", 15, "legacy", "group", "upper", "lower", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			n := largeGeometryOrdinaryStation(t)
			n.Stations[0].VehicleClasses = largeGeometryClasses(t, tc.stationClass)
			n.Stations[0].Berths[0].VehicleClasses = largeGeometryClasses(t, tc.berthClass)
			n.Stations[0].Berths[0].SeparationGroup = tc.plane
			for i := range n.Lanes {
				n.Lanes[i].SeparationGroup = tc.incoming
			}
			n.Nodes = append(n.Nodes, Node{ID: "near-a", Position: Point{X: -50, Y: 50 + tc.gap}}, Node{ID: "near-b", Position: Point{X: 50, Y: 50 + tc.gap}})
			n.Lanes = append(n.Lanes, Lane{ID: "near", From: "near-a", To: "near-b", SpeedLimit: 14, SeparationGroup: "lower"})
			if n.hasStationBanks() {
				t.Fatal("ordinary fixture acquired station banks")
			}
			_, err := PrepareNetwork(n)
			if (err == nil) != tc.valid {
				t.Fatalf("ordinary berth audit: %v", err)
			}
		})
	}
	n := largeGeometryOrdinaryStation(t)
	n.Nodes = append(n.Nodes, Node{ID: "near-berth", Position: Point{X: 15, Y: 50}})
	n.Stations[0].Berths = append(n.Stations[0].Berths, Berth{ID: "near-berth", Node: "near-berth"})
	if err := n.ValidateBankGeometry(); err == nil {
		t.Fatal("large berth and small berth were not audited")
	}
	if _, err := NewFleet(largeGeometryOrdinaryStation(t), []Placement{{ID: "express", Class: ExpressClass, StationID: "station", BerthID: "berth"}}); !errors.Is(err, ErrUnsupportedVehicleProfile) {
		t.Fatalf("geometry preparation enabled express operation: %v", err)
	}
}

func TestLargeGeometryInitialPlacementSeparation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                                string
		first, second                       VehicleClass
		gap                                 float64
		firstPlane, secondPlane, secondNode string
		valid                               bool
	}{
		{"group leader", GroupClass, LegacyClass, 15, "", "", "second", false},
		{"group follower", CompactClass, GroupClass, 15, "", "", "second", false},
		{"express metadata", ExpressClass, LegacyClass, 15, "", "", "second", false},
		{"large boundary", GroupClass, GroupClass, 20, "", "", "second", true},
		{"old overlap unchanged", LegacyClass, CompactClass, 1, "", "", "second", true},
		{"distinct planes", GroupClass, CompactClass, 1, "upper", "lower", "second", true},
		{"unknown plane", GroupClass, CompactClass, 15, "upper", "", "second", false},
		{"shared node planes", GroupClass, CompactClass, 0, "upper", "lower", "first", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			placements := []initialPlacementGeometry{{id: "one", class: tc.first, locations: []SafetyLocation{{SeparationGroup: tc.firstPlane, From: "first", To: "first"}}},
				{id: "two", class: tc.second, position: Point{X: tc.gap}, locations: []SafetyLocation{{SeparationGroup: tc.secondPlane, From: tc.secondNode, To: tc.secondNode}}}}
			if err := validateInitialSeparation(placements); (err == nil) != tc.valid {
				t.Fatalf("initial large gap guard: %v", err)
			}
		})
	}
}

func TestLargeGeometryJunctionBoundAndSkip(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		first, second string
		clearance     float64
	}{
		{"old pair", "legacy", "compact", 12}, {"group first", "group", "legacy", 20}, {"group second", "legacy", "group", 20}, {"express metadata", "express", "legacy", 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			n := Network{Nodes: []Node{{ID: "a"}, {ID: "junction", Position: Point{X: 1000}}, {ID: "b", Position: Point{X: 1000, Y: 100}}},
				Lanes: []Lane{{ID: "approach", From: "a", To: "junction", SpeedLimit: 14, VehicleClasses: largeGeometryClasses(t, tc.first)}, {ID: "exit", From: "junction", To: "b", SpeedLimit: 14, VehicleClasses: largeGeometryClasses(t, tc.second)}}}
			conflicts := buildJunctionConflicts(n)
			if len(conflicts["approach"]) != 1 || len(conflicts["exit"]) != 1 || !reflect.DeepEqual(n.JunctionPairs(), []int{0, 2, 0}) {
				t.Fatalf("junction lost ordered pairs: %+v", conflicts)
			}
			approach, exit := conflicts["approach"][0], conflicts["exit"][0]
			if approach.start > 1000-tc.clearance || approach.start < 1000-tc.clearance-1.001 || approach.end != 1000 || exit.start != 0 || exit.end < tc.clearance || exit.end > tc.clearance+1.001 {
				t.Fatalf("junction bound/skip omitted or doubled clearance %g: approach %+v, exit %+v", tc.clearance, approach, exit)
			}
		})
	}
	points := []Point{{}, {X: 1000}}
	other := []Point{{X: 1000}, {X: 1000, Y: 100}}
	start, end := conflictExtentWithClearance(newConflictPolyline(points), newConflictPolyline(other), 20)
	if start > 980 || end != 1000 {
		t.Fatal("large conflict sample skip missed the 20-meter boundary")
	}
}

func TestLargeGeometryJunctionCurvesCoverIndependentSamples(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		points, other []Point
	}{
		{"short bend", []Point{{X: 40}, {X: 19.9}, {X: 40, Y: 10}}, []Point{{Y: -1}, {Y: 1}}},
		{"long approach", []Point{{X: -1000}, {X: -15}, {X: 15}, {X: 1000}}, []Point{{Y: -100}, {Y: 100}}},
		{"separated", []Point{{X: 21}, {X: 40}}, []Point{{Y: -1}, {Y: 1}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			start, end := conflictExtentWithClearance(newConflictPolyline(tc.points), newConflictPolyline(tc.other), 20)
			distance, conflicts := 0.0, 0
			for i := 1; i < len(tc.points); i++ {
				a, b := tc.points[i-1], tc.points[i]
				length := pointDistance(a, b)
				for offset := 0.0; ; offset = min(length, offset+0.125) {
					point := Point{X: a.X + (b.X-a.X)*offset/length, Y: a.Y + (b.Y-a.Y)*offset/length}
					if referencePointToPolylineDistance(point, tc.other) <= 20 {
						conflicts++
						if distance+offset < start || distance+offset > end {
							t.Fatal("large conflict extent omitted an independent close sample")
						}
					}
					if offset == length {
						break
					}
				}
				distance += length
			}
			if tc.name == "separated" && !math.IsInf(start, 1) || tc.name != "separated" && conflicts == 0 {
				t.Fatal("junction sample fixture did not reach its intended phase")
			}
		})
	}
}

func TestLargeGeometryNeighborTails(t *testing.T) {
	t.Parallel()
	n := Network{Nodes: []Node{{ID: "z", Position: Point{X: -100}}, {ID: "a"}, {ID: "shared", Position: Point{X: 100}}, {ID: "b", Position: Point{X: 200}}, {ID: "c", Position: Point{Y: 100}}, {ID: "d", Position: Point{X: 100, Y: 100}}}, Lanes: []Lane{
		{ID: "large", From: "a", To: "shared", VehicleClasses: largeGeometryClasses(t, "group")},
		{ID: "neighbor", From: "shared", To: "b"},
		{ID: "arrival", From: "z", To: "a"},
		{ID: "unrelated", From: "c", To: "d"},
	}}
	tails := indexGeometryTails(n)
	for _, tc := range []struct {
		id             string
		tail, fromTail float64
	}{{"large", 20, 20}, {"neighbor", 20, 20}, {"arrival", 20, 0}, {"unrelated", 0, 0}} {
		if got := tails[tc.id]; got.tail != tc.tail || got.fromTail != tc.fromTail {
			t.Fatalf("large neighbor geometry tail %s: %+v", tc.id, got)
		}
	}
	ordinary := indexGeometryTails(Example())
	for id, tail := range ordinary {
		if tail != (geometryTails{}) {
			t.Fatalf("ordinary geometry gained a tail at %s", id)
		}
	}
	curve := Network{Nodes: []Node{{ID: "a"}, {ID: "b", Position: Point{X: 100}}, {ID: "c", Position: Point{Y: 35}}, {ID: "d", Position: Point{X: 100, Y: 35}}},
		Lanes: []Lane{{ID: "curve", From: "a", To: "b", SpeedLimit: 14, Control: &Point{X: 50, Y: 40}, VehicleClasses: largeGeometryClasses(t, "group")}, {ID: "neighbor", From: "c", To: "d", SpeedLimit: 14}}}
	if _, err := PrepareNetwork(curve); err == nil {
		t.Fatal("curved nonincident large path escaped the audit")
	}
}

func TestLargeGeometryInitialIncidentPlanes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, firstIncident, secondIncident, firstGate, secondGate string
		oldOnly, valid                                             bool
	}{
		{"coincident incoming plane", "ground", "ground", "gate-a", "gate-b", false, false},
		{"small incoming meets large berth", "upper", "upper", "gate-a", "gate-b", false, false},
		{"disjoint incoming planes", "track-upper", "track-lower", "gate-a", "gate-b", false, true},
		{"unknown incoming plane", "", "track-lower", "gate-a", "gate-b", false, false},
		{"shared incoming node", "track-upper", "track-lower", "shared", "shared", false, false},
		{"old incoming plane unchanged", "ground", "ground", "gate-a", "gate-b", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			firstClass := GroupClass
			if tc.oldOnly {
				firstClass = LegacyClass
			}
			first := berthGeometryLocations(Berth{ID: "one", Node: "first", SeparationGroup: "upper"}, []Lane{{From: tc.firstGate, To: "first", SeparationGroup: tc.firstIncident}})
			second := berthGeometryLocations(Berth{ID: "two", Node: "second", SeparationGroup: "lower"}, []Lane{{From: tc.secondGate, To: "second", SeparationGroup: tc.secondIncident}})
			if len(first) != 2 || len(second) != 2 {
				t.Fatal("initial berth binding lost an incident plane")
			}
			placements := []initialPlacementGeometry{{id: "one", class: firstClass, locations: first}, {id: "two", class: CompactClass, position: Point{X: 15}, locations: second}}
			if err := validateInitialSeparation(placements); (err == nil) != tc.valid {
				t.Fatalf("initial incident plane proof: %v", err)
			}
		})
	}
}

func TestLargeGeometryBerthIncidentAudit(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, firstIncoming, secondIncoming string
		excluded, valid                     bool
	}{
		{"false raw berth planes", "ground", "ground", false, false},
		{"small incoming meets large berth", "upper", "upper", false, false},
		{"true disjoint planes", "upper", "lower", false, true},
		{"effective mask exclusion", "ground", "ground", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			n, other := largeGeometryOrdinaryStation(t), largeGeometryOrdinaryStation(t)
			n.Stations[0].Berths[0].SeparationGroup = "upper"
			if tc.excluded {
				n.Stations[0].VehicleClasses = largeGeometryClasses(t, "legacy")
			}
			for i := range n.Lanes {
				n.Lanes[i].SeparationGroup = tc.firstIncoming
			}
			for i := range other.Nodes {
				other.Nodes[i].ID = "second-" + other.Nodes[i].ID
				other.Nodes[i].Position.X += 15
			}
			for i := range other.Lanes {
				lane := &other.Lanes[i]
				lane.ID, lane.From, lane.To = "second-"+lane.ID, "second-"+lane.From, "second-"+lane.To
				lane.SeparationGroup = tc.secondIncoming
			}
			station := &other.Stations[0]
			station.ID, station.Entry, station.Exit = "second-"+station.ID, "second-"+station.Entry, "second-"+station.Exit
			station.VehicleClasses = largeGeometryClasses(t, "legacy")
			station.Berths[0].ID, station.Berths[0].Node = "second-berth", "second-berth"
			station.Berths[0].VehicleClasses, station.Berths[0].SeparationGroup = largeGeometryClasses(t, "legacy"), "lower"
			n.Nodes, n.Lanes, n.Stations = append(n.Nodes, other.Nodes...), append(n.Lanes, other.Lanes...), append(n.Stations, other.Stations...)
			if _, err := PrepareNetwork(n); (err == nil) != tc.valid {
				t.Fatalf("large/small berth incident plane audit: %v", err)
			}
		})
	}
}

func TestLargeGeometryPathProjection(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name         string
		end, control Point
		class        string
		valid        bool
	}{
		{"straight", Point{X: 100}, Point{X: 50}, "group", true},
		{"gentle curve", Point{X: 200}, Point{X: 100, Y: 40}, "group", true},
		{"narrow monotone arc", Point{X: 10}, Point{X: 5, Y: 100}, "group", true},
		{"folded large arc", Point{X: 10}, Point{X: 100, Y: 100}, "group", false},
		{"folded express metadata", Point{X: 10}, Point{X: 100, Y: 100}, "express", false},
		{"old folded arc unchanged", Point{X: 10}, Point{X: 100, Y: 100}, "legacy", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			n := Network{Nodes: []Node{{ID: "a"}, {ID: "b", Position: tc.end}}, Lanes: []Lane{{ID: "curve", From: "a", To: "b", SpeedLimit: 14, Control: &tc.control, VehicleClasses: largeGeometryClasses(t, tc.class)}}}
			lane := n.Lanes[0]
			if n.Length(lane) < 40 {
				t.Fatal("projection fixture lacks a long authored arc")
			}
			_, err := PrepareNetwork(n)
			if (err == nil) != tc.valid {
				t.Fatalf("large path projection admission: %v", err)
			}
			if !tc.valid && !strings.Contains(err.Error(), "unsupported large-vehicle path shape") {
				t.Fatalf("folded path rejected for another reason: %v", err)
			}
			if tc.class == "legacy" || !tc.valid {
				return
			}
			tail := indexGeometryTails(n)[lane.ID].tail
			if !finite(tail) || tail < 20 {
				t.Fatalf("invalid derived path retention %g", tail)
			}
			if tc.name == "straight" && tail != 20 {
				t.Fatalf("straight path changed exact retention: %g", tail)
			}
			if tc.name != "straight" && tail <= 20 {
				t.Fatal("curved path retained only 20 arc meters")
			}
			// Check the same interpolation that movement uses, including segment interiors.
			length := n.Length(lane)
			for first := 0.0; first+tail <= length; first += 0.125 {
				if gap := pointDistance(n.Position(lane, first), n.Position(lane, first+tail)); gap < 20-1e-8 {
					t.Fatalf("derived retention has physical gap %g at arc %g", gap, first)
				}
			}
		})
	}
}

func TestLargeGeometryFoldedIncidentLane(t *testing.T) {
	t.Parallel()
	for _, relevant := range []bool{false, true} {
		n := Network{Nodes: []Node{{ID: "a"}, {ID: "b", Position: Point{X: 10}}, {ID: "c", Position: Point{X: -100}}}, Lanes: []Lane{
			{ID: "old-fold", From: "a", To: "b", SpeedLimit: 14, Control: &Point{X: 100, Y: 100}},
			{ID: "incident", From: "c", To: "a", SpeedLimit: 14},
		}}
		if relevant {
			n.Lanes[1].VehicleClasses = largeGeometryClasses(t, "group")
		}
		_, err := PrepareNetwork(n)
		if relevant {
			if err == nil || !strings.Contains(err.Error(), `lane "old-fold" has an unsupported large-vehicle path shape`) {
				t.Fatalf("large-node neighbor folded admission: %v", err)
			}
		} else if err != nil {
			t.Fatalf("old-only folded lane changed: %v", err)
		}
	}
}
