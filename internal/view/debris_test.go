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

// horizontalTarget returns a lane target from x 0 to x 100 at the height y.
func horizontalTarget(id string, y float64) laneTarget {
	return laneTarget{lane: sim.Lane{ID: id}, path: lanePath{{X: 0, Y: y}, {X: 100, Y: y}}}
}

// TestPickLane checks the nearest lane, the first lane on a tie, the reach,
// and the place of the click on the lane.
func TestPickLane(t *testing.T) {
	t.Parallel()
	lanes := []laneTarget{horizontalTarget("first", 0), horizontalTarget("tied", 10), horizontalTarget("far", 40), {lane: sim.Lane{ID: "empty"}}}
	tests := []struct {
		name         string
		point        sim.Point
		want         string
		wantFraction float64
		wantOK       bool
	}{
		{name: "on a lane", point: sim.Point{X: 25, Y: 40}, want: "far", wantFraction: 0.25, wantOK: true},
		{name: "nearest", point: sim.Point{X: 50, Y: 8}, want: "tied", wantFraction: 0.5, wantOK: true},
		{name: "tie", point: sim.Point{X: 50, Y: 5}, want: "first", wantFraction: 0.5, wantOK: true},
		{name: "at the reach", point: sim.Point{X: 50, Y: 52}, want: "far", wantFraction: 0.5, wantOK: true},
		{name: "past the reach", point: sim.Point{X: 50, Y: 52.5}},
		{name: "past the lane end", point: sim.Point{X: 110, Y: 40}, want: "far", wantFraction: 1, wantOK: true},
		{name: "before the lane start", point: sim.Point{X: -5, Y: 40}, want: "far", wantFraction: 0, wantOK: true},
	}
	for _, test := range tests {
		got, ok := pickLane(test.point, lanes, 12)
		if ok != test.wantOK {
			t.Errorf("%s: ok = %t, want %t", test.name, ok, test.wantOK)
			continue
		}
		if ok && (got.lane.ID != test.want || !closeTo(got.fraction, test.wantFraction)) {
			t.Errorf("%s: pick = %s at %g, want %s at %g", test.name, got.lane.ID, got.fraction, test.want, test.wantFraction)
		}
	}
	if _, ok := pickLane(sim.Point{}, nil, 12); ok {
		t.Error("a pick with no lanes found a lane")
	}
}

// TestPickLanePolyline checks the place of the click on a path of two parts
// with different lengths.
func TestPickLanePolyline(t *testing.T) {
	t.Parallel()
	path := laneTarget{lane: sim.Lane{ID: "bent"}, path: lanePath{{X: 0, Y: 0}, {X: 30, Y: 0}, {X: 30, Y: 90}}}
	got, ok := pickLane(sim.Point{X: 32, Y: 60}, []laneTarget{path}, 12)
	if !ok || !closeTo(got.fraction, 0.75) {
		t.Fatalf("pick = %+v %t, want the fraction 0.75", got, ok)
	}
}

// TestDebrisSegment checks the 2 m segment in the middle and at each end of
// a lane, and on a lane shorter than 2 m.
func TestDebrisSegment(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name                     string
		center, length, from, to float64
	}{
		{name: "middle", center: 50, length: 100, from: 49, to: 51},
		{name: "lane start", center: 0.25, length: 100, from: 0, to: 2},
		{name: "lane end", center: 99.5, length: 100, from: 98, to: 100},
		{name: "exact fit", center: 1, length: 2, from: 0, to: 2},
		{name: "short lane", center: 0.5, length: 1.5, from: 0, to: 1.5},
	} {
		if from, to := debrisSegment(test.center, test.length); from != test.from || to != test.to {
			t.Errorf("%s: segment = %g to %g, want %g to %g", test.name, from, to, test.from, test.to)
		}
	}
}

// TestLaneTargetsSkipHiddenLanes checks that the thin lanes of a collapsed
// station are not targets, that the through lane of the station is, and that
// a lane out of the viewport is not.
func TestLaneTargetsSkipHiddenLanes(t *testing.T) {
	t.Parallel()
	network := denseStyleNetwork()
	network.Nodes = append(network.Nodes, sim.Node{ID: "out", Position: sim.Point{X: 5000, Y: 5000}}, sim.Node{ID: "out2", Position: sim.Point{X: 5100, Y: 5000}})
	network.Lanes = append(network.Lanes, sim.Lane{ID: "away", From: "out", To: "out2"})
	game := zoomedGame(network, layoutInput{outsideWidth: minimumWidth, outsideHeight: minimumHeight, deviceScale: 1}, 1)
	ids := func() []string {
		var ids []string
		for _, target := range game.laneTargets() {
			ids = append(ids, target.lane.ID)
		}
		return ids
	}
	got := ids()
	if slices.Contains(got, "a-berths") {
		t.Error("a hidden lane of a collapsed station is a target")
	}
	if !slices.Contains(got, "a-through") {
		t.Error("the through lane of a collapsed station is not a target")
	}
	if len(got) < 2 || got[0] != "a-through" {
		t.Errorf("targets start %q, want network order", got[:min(2, len(got))])
	}
	game.camera.centerOn(sim.Point{})
	game.camera.zoomAt(game.mapPoint(sim.Point{}), mapMaxZoom)
	game.syncCamera()
	if slices.Contains(ids(), "away") {
		t.Error("a lane out of the viewport is a target")
	}
}

// armedTestGame returns a game on a session, with the Incidents panel open
// and a debris click armed.
func armedTestGame(t *testing.T) *Game {
	t.Helper()
	game := sharedProjectGame(t, faultProject())
	game.fitNetwork()
	game.press(incidentsAction)
	game.press(placeDebrisAction)
	if !game.debrisArmed {
		t.Fatal("press does not arm")
	}
	return game
}

// TestArmedClickSendsFault clicks a lane while armed. The click sends a fault
// on the lane with a centered segment and no duration, selects nothing, and
// disarms.
func TestArmedClickSendsFault(t *testing.T) {
	t.Parallel()
	game := armedTestGame(t)
	selected, origin, destination := game.selected, game.origin, game.destination
	target := game.laneTargets()[0]
	point, _ := game.laneGeometry(target.lane, game.currentNetworkStyle(game.collapsedStationMarkers()).detailed).along(0.5)
	pick, ok := pickLane(point, game.laneTargets(), lanePickRadius*game.layout.unit)
	if !ok {
		t.Fatal("no lane at the click")
	}
	game.pickOnMap(point, false)
	if game.debrisArmed {
		t.Error("the click leaves debris armed")
	}
	command := clickResult(t, game).Command
	length := game.network.Length(pick.lane)
	wantFrom, wantTo := debrisSegment(pick.fraction*length, length)
	if command.Action != "fault" || command.LaneID != pick.lane.ID || command.PodID != "" || command.DurationSeconds != nil {
		t.Fatalf("command = %+v, want a fault on %s with no duration", command, pick.lane.ID)
	}
	if command.FromMeters == nil || command.ToMeters == nil || *command.FromMeters != wantFrom || *command.ToMeters != wantTo {
		t.Fatalf("segment = %v to %v, want %g to %g", command.FromMeters, command.ToMeters, wantFrom, wantTo)
	}
	if game.selected != selected || game.origin != origin || game.destination != destination {
		t.Error("the click changed a selection")
	}
}

// TestArmedClickOnPodSelectsNothing clicks a pod while armed. The click must
// not select the pod, which would also close the panel.
func TestArmedClickOnPodSelectsNothing(t *testing.T) {
	t.Parallel()
	game := armedTestGame(t)
	vehicles := game.state.Simulation.Vehicles
	if len(vehicles) < 2 {
		t.Fatalf("project has %d pods, want two", len(vehicles))
	}
	game.selected = 0
	point := game.mapPoint(vehicles[1].Pod.Position)
	if target, ok := pickMapTarget(game.mapPickTargets(point)); !ok || target.pod != 1 {
		t.Fatalf("target = %+v %t, want the second pod", target, ok)
	}
	game.pickOnMap(point, false)
	if game.selected != 0 || !game.showIncidents {
		t.Fatalf("selected %d panel %t, want no selection", game.selected, game.showIncidents)
	}
}

// TestArmedClickPlacesDebris runs an armed click through the session on a
// lane where the server accepts it, and shows the mark and the row.
func TestArmedClickPlacesDebris(t *testing.T) {
	t.Parallel()
	game := armedTestGame(t)
	var placed bool
	for _, target := range game.laneTargets() {
		game.debrisArmed = true
		point, _ := game.laneGeometry(target.lane, game.currentNetworkStyle(game.collapsedStationMarkers()).detailed).along(0.5)
		game.pickOnMap(point, false)
		result := clickResult(t, game)
		game.handleResult(result)
		syncGame(t, game, func() bool { return true })
		if result.Reply.Error != "" {
			if game.message != result.Reply.Error {
				t.Fatalf("message = %q, want the server error %q", game.message, result.Reply.Error)
			}
			continue
		}
		placed = true
		break
	}
	if !placed {
		t.Fatal("the server refused debris on every lane")
	}
	syncGame(t, game, func() bool { return len(game.state.Simulation.Faults.Active) == 1 })
	if got := game.debrisMarks(game.state.Simulation.Faults, true); len(got) != 1 {
		t.Errorf("marks = %+v, want one", got)
	}
	if got := actionsWithPrefix(game.buttons(), incidentClearPrefix); len(got) != 1 {
		t.Errorf("Clear buttons = %q, want one", got)
	}
}

// TestArmedClickMiss checks that a click with no lane in reach sends nothing,
// selects nothing, shows a message, and stays armed.
func TestArmedClickMiss(t *testing.T) {
	t.Parallel()
	game := debrisTestGame(t)
	game.state.Simulation.Vehicles = []sim.Vehicle{{Pod: sim.Pod{ID: "pod-a", Position: sim.Point{}}}, {Pod: sim.Pod{ID: "pod-b", Position: sim.Point{X: 100}}}}
	game.showIncidents, game.debrisArmed = true, true
	viewport := game.layout.mapViewport
	game.pickOnMap(sim.Point{X: float64(viewport.Min.X + 2), Y: float64(viewport.Max.Y - 2)}, false)
	if !game.debrisArmed || game.message != noLaneMessage || game.pending {
		t.Fatalf("armed %t message %q pending %t", game.debrisArmed, game.message, game.pending)
	}
}

// TestPlaceDebrisButton checks the arming press, the disarming press, and
// the state of the button. A closed panel ends an armed click. While a
// command waits, the button cannot arm, but it can disarm.
func TestPlaceDebrisButton(t *testing.T) {
	t.Parallel()
	game := faultTestGame(t)
	game.showIncidents = true
	if got := findButton(t, game.buttons(), placeDebrisAction); got.label != "Place debris" || got.selected || got.disabled {
		t.Fatalf("button = %+v", got)
	}
	game.press(placeDebrisAction)
	if got := findButton(t, game.buttons(), placeDebrisAction); !game.debrisArmed || got.label != "Click a lane" || !got.selected {
		t.Fatalf("armed %t, button = %+v", game.debrisArmed, got)
	}
	game.pending = true
	if got := findButton(t, game.buttons(), placeDebrisAction); got.disabled {
		t.Error("the armed button cannot disarm while a command waits")
	}
	game.press(placeDebrisAction)
	if game.debrisArmed {
		t.Error("a second press does not disarm")
	}
	if got := findButton(t, game.buttons(), placeDebrisAction); !got.disabled {
		t.Error("the button can arm while a command waits")
	}
	game.pending = false
	game.press(placeDebrisAction)
	game.press(incidentsAction)
	if game.debrisArmed {
		t.Error("closing the panel leaves debris armed")
	}
	game.press(incidentsAction)
	game.press(placeDebrisAction)
	game.press("orders")
	if game.debrisArmed {
		t.Error("another panel leaves debris armed")
	}
}

// TestPlaceDebrisButtonFits checks both labels in each layout.
func TestPlaceDebrisButtonFits(t *testing.T) {
	t.Parallel()
	for _, layout := range controlLayouts {
		t.Run(layout.name, func(t *testing.T) {
			t.Parallel()
			game := controlTestGame(t, layout.input)
			game.showIncidents = true
			for _, armed := range []bool{false, true} {
				game.debrisArmed = armed
				control := findButton(t, game.buttons(), placeDebrisAction)
				if got := game.fitButtonText(control.label, control.fontSize, control.w); got != control.label {
					t.Errorf("label %q shortened to %q", control.label, got)
				}
				for _, value := range game.incidentLabels(game.state.Simulation) {
					if game.labelArea(value).overlaps(buttonArea(control)) {
						t.Errorf("label %q overlaps the button", value.value)
					}
				}
			}
		})
	}
}
