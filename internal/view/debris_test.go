package view

import (
	"fmt"
	"log/slog"
	"math"
	"slices"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/remote"
	"github.com/dotwaffle/podsim/internal/session"
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
		// Each point of the stroke is on the curve between the meters of
		// the ends, to within the chord error.
		if len(mark.path) < 3 {
			t.Errorf("detailed %t: path has %d points, want points between the ends", detailed, len(mark.path))
		}
		segment := make(lanePath, 1001)
		for i := range segment {
			segment[i] = game.mapPoint(game.network.Position(lane, from+(to-from)*float64(i)/1000))
		}
		for _, point := range mark.path {
			if d := pathToPoint(point, segment); d > 0.5 {
				t.Errorf("detailed %t: point %+v is %g pixels from the segment", detailed, point, d)
			}
		}
		for name, check := range map[string]struct {
			got    sim.Point
			meters float64
		}{"from": {mark.from, from}, "to": {mark.to, to}, "center": {mark.center, length / 2}} {
			if want := game.mapPoint(game.network.Position(lane, check.meters)); !near(check.got, want, 0.5) {
				t.Errorf("detailed %t: %s = %+v, want %+v", detailed, name, check.got, want)
			}
		}
	}
}

// asymmetricCurve holds a curved lane whose distance does not grow evenly
// with the curve parameter, and whose drawn points differ from the points of
// the simulation.
var asymmetricCurve = struct{ from, control, to sim.Point }{
	from: sim.Point{}, control: sim.Point{X: 1000, Y: 1000}, to: sim.Point{X: 1000},
}

// curveMeters measures asymmetricCurve to the curve parameter vertex/64 on 64
// chords, as the simulation does. It uses no code of the view or of the
// simulation.
func curveMeters(vertex int) float64 {
	at := func(i int) (x, y float64) {
		t := float64(i) / 64
		u := 1 - t
		c := asymmetricCurve
		return u*u*c.from.X + 2*u*t*c.control.X + t*t*c.to.X, u*u*c.from.Y + 2*u*t*c.control.Y + t*t*c.to.Y
	}
	meters := 0.0
	for i := 1; i <= vertex; i++ {
		ax, ay := at(i - 1)
		bx, by := at(i)
		meters += math.Hypot(bx-ax, by-ay)
	}
	return meters
}

// denseCurveGame returns a game on a session that shows asymmetricCurve with
// four drawn segments. The network is local to the game, so the server
// refuses the lane, and the commands of the game show what the view sends.
func denseCurveGame(t *testing.T) (*Game, sim.Lane) {
	t.Helper()
	game := sharedProjectGame(t, faultProject())
	curve := sim.Lane{ID: "curve", From: "start", To: "end", Control: new(asymmetricCurve.control)}
	network := sim.Network{
		Nodes: []sim.Node{
			{ID: "start", Position: asymmetricCurve.from}, {ID: "end", Position: asymmetricCurve.to},
			{ID: "far", Position: sim.Point{X: 60000}}, {ID: "farther", Position: sim.Point{X: 60100}},
		},
		Lanes: []sim.Lane{curve},
	}
	for len(network.Lanes) <= detailedLanes {
		network.Lanes = append(network.Lanes, sim.Lane{ID: fmt.Sprintf("road-%d", len(network.Lanes)), From: "far", To: "farther"})
	}
	game.network, game.index = network, nil
	game.state.Simulation.Vehicles = nil
	game.fitNetwork()
	if geometry := game.laneGeometry(curve, false); game.currentNetworkStyle(game.collapsedStationMarkers()).detailed || geometry.count != 5 {
		t.Fatalf("the curve has %d points, want 5 on the dense map", geometry.count)
	}
	return game, curve
}

// TestLaneMetersDenseCurve checks the distances of a dense curve against the
// value of each drawn point that the simulation geometry gives.
func TestLaneMetersDenseCurve(t *testing.T) {
	t.Parallel()
	game, curve := denseCurveGame(t)
	got := game.laneMeters(curve, 5, false)
	for i, vertex := range []int{0, 16, 32, 48, 64} {
		if !closeTo(got[i], curveMeters(vertex)) {
			t.Errorf("point %d = %g m, want %g m", i, got[i], curveMeters(vertex))
		}
	}
	if length := game.network.Length(curve); got[4] != length {
		t.Errorf("last point = %v m, want the lane length %v m", got[4], length)
	}
	// A count that does not divide the polyline lands between its vertices.
	if between := game.laneMeters(curve, 6, false); between[1] <= curveMeters(12) || between[1] >= curveMeters(13) {
		t.Errorf("point of 5 segments = %g m, want between %g and %g", between[1], curveMeters(12), curveMeters(13))
	}
}

// TestDenseCurveClickSendsSimulationMeters clicks a point of a curve with four
// drawn segments and reads the command. A fraction of the drawn length gives
// 925.960 m at the middle point, and the simulation has 918.111 m. The mark
// of the command is at the clicked point.
func TestDenseCurveClickSendsSimulationMeters(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		vertex   int
		from, to func(length float64) float64
	}{
		{
			name: "middle", vertex: 32,
			from: func(float64) float64 { return curveMeters(32) - 1 },
			to:   func(float64) float64 { return curveMeters(32) + 1 },
		},
		{
			name: "lane end", vertex: 64,
			from: func(length float64) float64 { return length - 2 },
			to:   func(length float64) float64 { return length },
		},
		{
			name: "lane start", vertex: 0,
			from: func(float64) float64 { return 0 },
			to:   func(float64) float64 { return 2 },
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game, curve := denseCurveGame(t)
			if math.Abs(curveMeters(32)-918.111) > 0.001 {
				t.Fatalf("the middle of the curve is at %g m, want 918.111 m", curveMeters(32))
			}
			length := curveMeters(64)
			clicked := game.laneGeometry(curve, false).points[test.vertex/16]
			game.debrisArmed = true
			game.pickOnMap(clicked, false)
			command := clickResult(t, game).Command
			if command.LaneID != curve.ID || command.FromMeters == nil || command.ToMeters == nil {
				t.Fatalf("command = %+v, want a segment of the curve", command)
			}
			from, to := *command.FromMeters, *command.ToMeters
			if !closeTo(from, test.from(length)) || !closeTo(to, test.to(length)) {
				t.Fatalf("segment = %g to %g, want %g to %g", from, to, test.from(length), test.to(length))
			}
			if to > game.network.Length(curve) {
				t.Errorf("segment ends at %v, past the lane length %v", to, game.network.Length(curve))
			}
			marks := game.debrisMarks(sim.FaultsView{Active: []sim.FaultView{debrisFault(curve.ID, from, to)}}, false)
			if len(marks) != 1 {
				t.Fatalf("marks = %+v, want one", marks)
			}
			if d := pathToPoint(clicked, marks[0].path); d > 1e-6 {
				t.Errorf("the mark is %g pixels from the click", d)
			}
			if test.vertex != 32 {
				return
			}
			if !near(marks[0].center, clicked, 1e-6) {
				t.Errorf("center = %+v, want the click %+v", marks[0].center, clicked)
			}
			// 1 m before the click, on the drawn segment that ends at the click.
			before := game.laneGeometry(curve, false).points[1]
			fraction := 1 - 1/(curveMeters(32)-curveMeters(16))
			want := sim.Point{X: before.X + (clicked.X-before.X)*fraction, Y: before.Y + (clicked.Y-before.Y)*fraction}
			if !near(marks[0].from, want, 1e-6) {
				t.Errorf("from = %+v, want %+v", marks[0].from, want)
			}
		})
	}
}

// pathToPoint returns the distance in pixels from point to a stroke.
func pathToPoint(point sim.Point, path []sim.Point) float64 {
	distance, _, _ := nearestOnPath(point, path)
	return distance
}

// TestCurveEndClickReachesTheServer clicks near the end of a curved lane in a
// session, so the next lane does not take the click. The segment ends at the
// length that the view measures. The server checks that end against its own
// length of the lane, and the stream checks it again.
func TestCurveEndClickReachesTheServer(t *testing.T) {
	t.Parallel()
	config := faultProject()
	index := slices.IndexFunc(config.Network.Lanes, func(lane sim.Lane) bool { return lane.ID == "bypass-in" })
	config.Network.Lanes[index].Control = &sim.Point{X: 463.7, Y: 261.3}
	game := sharedProjectGame(t, config)
	game.fitNetwork()
	game.press(incidentsAction)
	game.press(placeDebrisAction)
	lane := game.network.Lanes[index]
	game.pickOnMap(game.mapPoint(game.network.Position(lane, game.network.Length(lane)-0.5)), false)
	result := clickResult(t, game)
	if result.Reply.Error != "" {
		t.Fatalf("the server refused the segment: %s", result.Reply.Error)
	}
	command := result.Command
	if command.LaneID != lane.ID || *command.ToMeters != game.network.Length(lane) {
		t.Fatalf("command on %s ends at %v, want %s at %v", command.LaneID, *command.ToMeters, lane.ID, game.network.Length(lane))
	}
	game.handleResult(result)
	syncGame(t, game, func() bool { return len(game.state.Simulation.Faults.Active) == 1 })
	if active := game.state.Simulation.Faults.Active[0]; *active.ToMeters != *command.ToMeters {
		t.Errorf("the session holds the end %v, want %v", *active.ToMeters, *command.ToMeters)
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

// TestPodCollapsed checks which pods the map hides under the marker of a
// collapsed station.
func TestPodCollapsed(t *testing.T) {
	t.Parallel()
	collapsed := map[string]bool{"collapsed": true}
	for _, test := range []struct {
		name               string
		activity           sim.Activity
		station            string
		selected, incident bool
		want               bool
	}{
		{name: "idle at a collapsed station", activity: sim.Idle, station: "collapsed", want: true},
		{name: "selected", activity: sim.Idle, station: "collapsed", selected: true},
		{name: "incident", activity: sim.Idle, station: "collapsed", incident: true},
		{name: "moving", activity: sim.Traveling, station: "collapsed"},
		{name: "idle at an open station", activity: sim.Idle, station: "open"},
	} {
		incidents := map[string]bool{}
		if test.incident {
			incidents["pod"] = true
		}
		pod := sim.Pod{ID: "pod", Activity: test.activity, StationID: test.station}
		if got := podCollapsed(pod, test.selected, collapsed, incidents); got != test.want {
			t.Errorf("%s: collapsed = %t, want %t", test.name, got, test.want)
		}
	}
}

// TestFaultedParkedPodStaysOnMap faults an idle pod in a berth through a
// session. The pod stays idle, so only the incident keeps it on the map, and
// its label shows.
func TestFaultedParkedPodStaysOnMap(t *testing.T) {
	t.Parallel()
	game := sharedProjectGame(t, faultProject())
	game.selected = 1
	clickFault(t, game, faultActionPrefix+game.state.Simulation.Vehicles[1].Pod.ID)
	syncGame(t, game, func() bool { return len(game.state.Simulation.Faults.Active) == 1 })
	game.selected = 0
	state := game.state.Simulation
	faulted := state.Vehicles[1].Pod
	if faulted.Activity != sim.Idle || faulted.StationID == "" {
		t.Fatalf("faulted pod = %+v, want an idle pod at a station", faulted)
	}
	collapsed := map[string]bool{faulted.StationID: true}
	incidents := incidentPods(state.Faults, state.Emergencies)
	if !podCollapsed(faulted, false, collapsed, nil) || podCollapsed(faulted, false, collapsed, incidents) {
		t.Error("only the incident must keep the faulted pod on the map")
	}
	if got := game.podMapLabels(state.Vehicles, collapsed, incidents)[1]; got.value == "" {
		t.Error("the faulted pod has no label")
	}
	if got := game.podMapLabels(state.Vehicles, collapsed, nil)[1]; got.value != "" {
		t.Errorf("a parked pod has the label %q", got.value)
	}
}

// TestFaultedParkedPodCanBeClicked checks that a click can select an idle pod
// at a station whose berths do not separate only when the pod has an
// incident.
func TestFaultedParkedPodCanBeClicked(t *testing.T) {
	t.Parallel()
	game := zoomedGame(denseStyleNetwork(), layoutInput{outsideWidth: minimumWidth, outsideHeight: minimumHeight, deviceScale: 1}, 1)
	game.camera.scale = 0.1
	station, _ := game.network.Station("a")
	if game.showStationBerths(station) {
		t.Fatal("the berths of the station separate")
	}
	vehicle := sim.Vehicle{Pod: sim.Pod{ID: "pod", Activity: sim.Idle, StationID: "a"}}
	game.state.Simulation.Vehicles = []sim.Vehicle{{}, vehicle}
	targets := func() int { return len(game.mapPickTargets(sim.Point{}).pods) }
	if got := targets(); got != 1 {
		t.Fatalf("%d pods are targets with no incident, want only the selected pod", got)
	}
	game.state.Simulation.Faults.Active = []sim.FaultView{{ID: "i1.1", Kind: sim.FaultKindPod, PodID: "pod"}}
	if got := targets(); got != 2 {
		t.Fatalf("%d pods are targets with a faulted pod, want 2", got)
	}
}

// horizontalTarget returns a lane target from x 0 to x 100 at the height y.
// The lane has 400 m.
func horizontalTarget(id string, y float64) laneTarget {
	return laneTarget{lane: sim.Lane{ID: id}, path: lanePath{{X: 0, Y: y}, {X: 100, Y: y}}, meters: []float64{0, 400}}
}

// TestPickLane checks the nearest lane, the first lane on a tie, the reach,
// and the place of the click on the lane.
func TestPickLane(t *testing.T) {
	t.Parallel()
	lanes := []laneTarget{horizontalTarget("first", 0), horizontalTarget("tied", 10), horizontalTarget("far", 40), {lane: sim.Lane{ID: "empty"}}}
	tests := []struct {
		name       string
		point      sim.Point
		want       string
		wantMeters float64
		wantOK     bool
	}{
		{name: "on a lane", point: sim.Point{X: 25, Y: 40}, want: "far", wantMeters: 100, wantOK: true},
		{name: "nearest", point: sim.Point{X: 50, Y: 8}, want: "tied", wantMeters: 200, wantOK: true},
		{name: "tie", point: sim.Point{X: 50, Y: 5}, want: "first", wantMeters: 200, wantOK: true},
		{name: "at the reach", point: sim.Point{X: 50, Y: 52}, want: "far", wantMeters: 200, wantOK: true},
		{name: "past the reach", point: sim.Point{X: 50, Y: 52.5}},
		{name: "past the lane end", point: sim.Point{X: 110, Y: 40}, want: "far", wantMeters: 400, wantOK: true},
		{name: "before the lane start", point: sim.Point{X: -5, Y: 40}, want: "far", wantMeters: 0, wantOK: true},
	}
	for _, test := range tests {
		got, ok := pickLane(test.point, lanes, 12)
		if ok != test.wantOK {
			t.Errorf("%s: ok = %t, want %t", test.name, ok, test.wantOK)
			continue
		}
		if ok && (got.lane.ID != test.want || !closeTo(got.meters, test.wantMeters)) {
			t.Errorf("%s: pick = %s at %g, want %s at %g", test.name, got.lane.ID, got.meters, test.want, test.wantMeters)
		}
	}
	if _, ok := pickLane(sim.Point{}, nil, 12); ok {
		t.Error("a pick with no lanes found a lane")
	}
}

// TestPickLanePolyline checks the place of the click on a path of two parts.
// The distances of the path decide the place, and not the screen lengths: the
// click is at 75 percent of the screen length, and at 70 m of 100 m.
func TestPickLanePolyline(t *testing.T) {
	t.Parallel()
	path := laneTarget{lane: sim.Lane{ID: "bent"}, path: lanePath{{X: 0, Y: 0}, {X: 30, Y: 0}, {X: 30, Y: 90}}, meters: []float64{0, 10, 100}}
	got, ok := pickLane(sim.Point{X: 32, Y: 60}, []laneTarget{path}, 12)
	if !ok || !closeTo(got.meters, 70) {
		t.Fatalf("pick = %+v %t, want 70 m", got, ok)
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
	wantFrom, wantTo := debrisSegment(pick.meters, game.network.Length(pick.lane))
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
// selects nothing, shows a message, and ends the arming, because a press arms
// one click. The next click at a pod selects the pod.
func TestArmedClickMiss(t *testing.T) {
	t.Parallel()
	game := debrisTestGame(t)
	game.state.Simulation.Vehicles = []sim.Vehicle{{Pod: sim.Pod{ID: "pod-a", Position: sim.Point{}}}, {Pod: sim.Pod{ID: "pod-b", Position: sim.Point{X: 100}}}}
	game.showIncidents, game.debrisArmed = true, true
	viewport := game.layout.mapViewport
	game.pickOnMap(sim.Point{X: float64(viewport.Min.X + 2), Y: float64(viewport.Max.Y - 2)}, false)
	if game.debrisArmed || game.message != noLaneMessage || game.pending || game.selected != 0 {
		t.Fatalf("armed %t message %q pending %t selected %d", game.debrisArmed, game.message, game.pending, game.selected)
	}
	game.pickOnMap(game.mapPoint(sim.Point{X: 100}), false)
	if game.selected != 1 || game.pending {
		t.Fatalf("selected %d pending %t, want a selection of the second pod", game.selected, game.pending)
	}
}

// TestSessionChangeEndsArming changes the session while a debris click is
// armed. The next click must select, and not place debris on the new
// network. A new revision of the same session keeps the arming.
func TestSessionChangeEndsArming(t *testing.T) {
	t.Parallel()
	apply := func(t *testing.T, shared *session.Session, command session.Command) {
		t.Helper()
		command.Client, command.Sequence, command.Epoch = "other-"+command.Action, 1, shared.State().Epoch
		if reply := shared.Apply(command); reply.Error != "" {
			t.Fatalf("%s: %s", command.Action, reply.Error)
		}
	}
	for _, test := range []struct {
		name   string
		change func(*testing.T, changeEnv)
	}{
		{name: "project apply", change: func(t *testing.T, env changeEnv) {
			t.Helper()
			next := project.Default()
			next.Name = "Replacement"
			apply(t, env.shared, session.Command{Action: "project", Project: &next, ProjectRevision: env.shared.Project().Revision})
		}},
		{name: "reset", change: func(t *testing.T, env changeEnv) {
			t.Helper()
			apply(t, env.shared, session.Command{Action: "reset"})
		}},
		{name: "reconnect to a new session", change: func(t *testing.T, env changeEnv) {
			t.Helper()
			replacement, err := session.NewWithProject(project.Default(), session.WithLogger(slog.New(slog.DiscardHandler)))
			if err != nil {
				t.Fatalf("create session: %v", err)
			}
			env.server.use(replacement)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			env := newChangeEnv(t)
			game := env.game
			game.fitNetwork()
			game.press(incidentsAction)
			game.press(placeDebrisAction)
			before := remote.SessionOf(game.state)

			apply(t, env.shared, session.Command{Action: "pause", Paused: true})
			syncGame(t, game, func() bool { return game.state.Simulation.Paused })
			if !game.debrisArmed {
				t.Fatal("a new revision of the session ended the arming")
			}

			test.change(t, env)
			syncGame(t, game, func() bool { return remote.SessionOf(game.state) != before })
			if game.state.Simulation.OperationsOff || game.debrisArmed {
				t.Fatalf("operations off %t, armed %t, want operations on and no arming", game.state.Simulation.OperationsOff, game.debrisArmed)
			}
			if !game.showIncidents {
				t.Error("the session change closed the panel")
			}
			game.selected = 0
			game.pickOnMap(game.mapPoint(game.state.Simulation.Vehicles[1].Pod.Position), false)
			if game.pending || game.selected != 1 {
				t.Fatalf("pending %t selected %d, want a selection and no command", game.pending, game.selected)
			}
		})
	}
}

// TestArmedClickRefusedAfterUnreadSessionChange clicks while the client
// follows a new session that the game has not read. The lane and the meters
// come from the old network, so the click sends nothing.
func TestArmedClickRefusedAfterUnreadSessionChange(t *testing.T) {
	t.Parallel()
	env := newChangeEnv(t)
	game := env.game
	game.fitNetwork()
	game.press(incidentsAction)
	game.press(placeDebrisAction)
	before := game.state.Epoch
	replacement, err := session.NewWithProject(project.Default(), session.WithLogger(slog.New(slog.DiscardHandler)))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	env.server.use(replacement)
	waitFor(t, func() bool {
		state, connected, _ := game.client.View()
		return connected && state.Epoch != before
	})
	lane := game.network.Lanes[0]
	game.pickOnMap(game.mapPoint(game.network.Position(lane, game.network.Length(lane)/2)), false)
	if game.pending || game.debrisArmed || game.message != remote.ErrSessionChanged.Error() {
		t.Fatalf("pending %t armed %t message %q, want a refusal", game.pending, game.debrisArmed, game.message)
	}
	if got := len(replacement.State().Simulation.Faults.Active); got != 0 {
		t.Fatalf("the new session has %d faults", got)
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
