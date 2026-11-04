package view

import (
	"math"
	"slices"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

// debrisTestGame returns a game with a straight lane of 100 m and a curved
// lane.
func debrisTestGame(t *testing.T) *Game {
	t.Helper()
	game := journeyNetworkGame(t, sim.Network{
		Stations: []sim.Station{{ID: "s1", Name: "One"}, {ID: "s2", Name: "Two"}},
		Nodes:    []sim.Node{{ID: "a", Position: sim.Point{}}, {ID: "b", Position: sim.Point{X: 100}}, {ID: "c", Position: sim.Point{X: 100, Y: 100}}},
		Lanes: []sim.Lane{
			{ID: "straight", From: "a", To: "b"},
			{ID: "curved", From: "b", To: "c", Control: &sim.Point{X: 180, Y: 50}},
		},
	})
	game.fitNetwork()
	return game
}

func debrisFault(lane string, from, to float64) sim.FaultView {
	return sim.FaultView{ID: "i1." + lane, Kind: sim.FaultKindDebris, LaneID: lane, FromMeters: new(from), ToMeters: new(to)}
}

// near reports whether two screen points are within tolerance pixels.
func near(a, b sim.Point, tolerance float64) bool {
	return math.Hypot(a.X-b.X, a.Y-b.Y) <= tolerance
}

// TestDebrisMarksStraightLane checks that the ends of a mark are at their
// meters on the lane, and that the stroke is one line.
func TestDebrisMarksStraightLane(t *testing.T) {
	t.Parallel()
	game := debrisTestGame(t)
	marks := game.debrisMarks(sim.FaultsView{Active: []sim.FaultView{debrisFault("straight", 20, 40)}}, true)
	if len(marks) != 1 {
		t.Fatalf("marks = %+v, want one", marks)
	}
	mark := marks[0]
	for name, check := range map[string]struct{ got, want sim.Point }{
		"from":   {mark.from, game.mapPoint(sim.Point{X: 20})},
		"to":     {mark.to, game.mapPoint(sim.Point{X: 40})},
		"center": {mark.center, game.mapPoint(sim.Point{X: 30})},
	} {
		if !near(check.got, check.want, 1e-6) {
			t.Errorf("%s = %+v, want %+v", name, check.got, check.want)
		}
	}
	if !slices.Equal(mark.path, []sim.Point{mark.from, mark.to}) {
		t.Errorf("path = %+v, want the two ends", mark.path)
	}
}

// TestDebrisMarksCurvedLane checks that the stroke follows a curved lane
// through the lane points between its ends, on both forms of the map.
func TestDebrisMarksCurvedLane(t *testing.T) {
	t.Parallel()
	for _, detailed := range []bool{true, false} {
		game := debrisTestGame(t)
		lane := game.network.Lanes[1]
		length := game.network.Length(lane)
		from, to := length/4, 3*length/4
		marks := game.debrisMarks(sim.FaultsView{Active: []sim.FaultView{debrisFault("curved", from, to)}}, detailed)
		if len(marks) != 1 {
			t.Fatalf("detailed %t: marks = %+v, want one", detailed, marks)
		}
		mark := marks[0]
		geometry := game.laneGeometry(lane, detailed)
		want := 0
		walked, total := 0.0, geometry.screenLength()
		for i := 1; i < geometry.count; i++ {
			walked += math.Hypot(geometry.points[i].X-geometry.points[i-1].X, geometry.points[i].Y-geometry.points[i-1].Y)
			if total/4 < walked && walked < 3*total/4 {
				want++
			}
		}
		if want < 1 || len(mark.path) != want+2 {
			t.Errorf("detailed %t: path has %d points, want %d between the ends", detailed, len(mark.path), want+2)
		}
		// The ends are at their meters also on the curve. A lane on the
		// detailed map is split by meters, so its points are exact to
		// within the chord error.
		if detailed {
			if wantFrom := game.mapPoint(game.network.Position(lane, from)); !near(mark.from, wantFrom, 0.5) {
				t.Errorf("from = %+v, want %+v", mark.from, wantFrom)
			}
			if wantTo := game.mapPoint(game.network.Position(lane, to)); !near(mark.to, wantTo, 0.5) {
				t.Errorf("to = %+v, want %+v", mark.to, wantTo)
			}
			if wantCenter := game.mapPoint(game.network.Position(lane, length/2)); !near(mark.center, wantCenter, 0.5) {
				t.Errorf("center = %+v, want %+v", mark.center, wantCenter)
			}
		}
	}
}

// TestDebrisMarksFilter checks that only debris on a known lane has a mark,
// in record order.
func TestDebrisMarksFilter(t *testing.T) {
	t.Parallel()
	game := debrisTestGame(t)
	faults := sim.FaultsView{Active: []sim.FaultView{
		{ID: "i1.1", Kind: sim.FaultKindPod, PodID: "pod-a", LaneID: "straight", FromMeters: new(1.0), ToMeters: new(2.0)},
		debrisFault("unknown", 1, 3),
		debrisFault("curved", 1, 3),
		debrisFault("straight", 10, 12),
	}}
	marks := game.debrisMarks(faults, true)
	if len(marks) != 2 {
		t.Fatalf("marks = %+v, want two", marks)
	}
	if want := game.mapPoint(sim.Point{X: 10}); !near(marks[1].from, want, 1e-6) {
		t.Errorf("second mark from = %+v, want %+v on the straight lane", marks[1].from, want)
	}
	if got := game.debrisMarks(sim.FaultsView{}, true); got != nil {
		t.Errorf("marks with no fault = %+v", got)
	}
}

// TestDebrisMarkOnCollapsedStationLane checks that a lane of a collapsed
// station keeps its mark.
func TestDebrisMarkOnCollapsedStationLane(t *testing.T) {
	t.Parallel()
	game := debrisTestGame(t)
	game.network.Lanes[0].StationID = "s1"
	game.index = nil
	if marks := game.debrisMarks(sim.FaultsView{Active: []sim.FaultView{debrisFault("straight", 1, 3)}}, false); len(marks) != 1 {
		t.Fatalf("marks = %+v, want one", marks)
	}
}

// TestIncidentPods checks that pod faults and emergencies name their pods,
// and that debris names none.
func TestIncidentPods(t *testing.T) {
	t.Parallel()
	got := incidentPods(
		sim.FaultsView{Active: []sim.FaultView{{Kind: sim.FaultKindPod, PodID: "faulted"}, {Kind: sim.FaultKindDebris, PodID: "debris", LaneID: "l"}}},
		sim.EmergenciesView{Active: []sim.EmergencyView{{PodID: "emergency"}}},
	)
	if len(got) != 2 || !got["faulted"] || !got["emergency"] {
		t.Fatalf("incident pods = %v", got)
	}
	if got := incidentPods(sim.FaultsView{}, sim.EmergenciesView{}); got != nil {
		t.Fatalf("incident pods with no record = %v", got)
	}
}

// TestPodRingColor checks that an incident has a danger ring in place of
// the amber ring, and that danger differs from amber.
func TestPodRingColor(t *testing.T) {
	t.Parallel()
	if danger == amber {
		t.Fatal("danger is amber")
	}
	for _, test := range []struct {
		name      string
		wait      sim.WaitReason
		incident  bool
		wantShade uint32
		wantRing  bool
	}{
		{name: "healthy"},
		{name: "wait", wait: sim.TrackOccupied, wantShade: amber, wantRing: true},
		{name: "incident", incident: true, wantShade: danger, wantRing: true},
		{name: "incident and wait", wait: sim.TrackOccupied, incident: true, wantShade: danger, wantRing: true},
	} {
		shade, ring := podRingColor(sim.Pod{WaitReason: test.wait}, test.incident)
		if shade != test.wantShade || ring != test.wantRing {
			t.Errorf("%s: ring %#x %t, want %#x %t", test.name, shade, ring, test.wantShade, test.wantRing)
		}
	}
}
