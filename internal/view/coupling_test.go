package view

import (
	"fmt"
	"math"
	"slices"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"

	"github.com/dotwaffle/podsim/internal/sim"
)

// couplingBox returns the corners of a rectangle along the x axis, in the
// server order: front left, rear left, rear right, front right.
func couplingBox(front, rear, halfWidth float64) sim.CouplingRectangle {
	return sim.CouplingRectangle{Corners: [4]sim.Point{{X: front, Y: halfWidth}, {X: rear, Y: halfWidth}, {X: rear, Y: -halfWidth}, {X: front, Y: -halfWidth}}}
}

// couplingFixture returns a train of cabins "a" at x 10 and "b" at x 5.5 in
// phase, with a virtual platoon of "c" and "d" behind it.
func couplingFixture(phase sim.CouplingPhase) sim.Snapshot {
	group := sim.CouplingGroupView{Bodies: [2]sim.CouplingRectangle{couplingBox(12, 8, 1), couplingBox(7.5, 3.5, 1)}}
	group.ID, group.Members, group.Phase = "pair", [2]string{"a", "b"}, phase
	switch phase {
	case sim.CouplingLatching, sim.CouplingConnected, sim.CouplingUnlatching:
		connector := couplingBox(8, 7.5, .15)
		group.Connector = &connector
	case sim.CouplingClosing, sim.CouplingOpening:
		envelope := couplingBox(8, 7.5, .15)
		group.ManeuverEnvelope = &envelope
	case sim.CouplingDraining:
	}
	return sim.Snapshot{
		CouplingContract: sim.CompactPairV1CouplingContract,
		CouplingGroups:   []sim.CouplingGroupView{group},
		Vehicles: []sim.Vehicle{
			{Pod: sim.Pod{ID: "c", Position: sim.Point{X: -10}}, PlatoonID: "d", PlatoonIndex: 2},
			{Pod: sim.Pod{ID: "a", Position: sim.Point{X: 10}}, CouplingID: "pair"},
			{Pod: sim.Pod{ID: "b", Position: sim.Point{X: 5.5}}, CouplingID: "pair"},
			{Pod: sim.Pod{ID: "d", Position: sim.Point{X: -4}}, PlatoonID: "d", PlatoonIndex: 1},
		},
	}
}

// couplingTestInput draws state at 10 pixels for each meter, with the map
// origin at (100, 50). With a unit of 1 pixel, the bodies are wider than a
// train mark. With a unit of 2 pixels, a train mark replaces them.
func couplingTestInput(state sim.Snapshot, unit float64) couplingDrawInput {
	return couplingDrawInput{
		groups: state.CouplingGroups, unit: unit,
		screen: func(p sim.Point) sim.Point { return sim.Point{X: 100 + 10*p.X, Y: 50 + 10*p.Y} },
	}
}

// outline returns the four screen sides of rectangle.
func outline(input couplingDrawInput, rectangle sim.CouplingRectangle, width float64) []couplingLine {
	return appendOutline(nil, input.worldRectangle(rectangle), width)
}

// TestCouplingLines checks the screen lines of a train in each phase. When
// the bodies are wide enough, bodies and envelopes are outlines and the
// connector joins the body pins. Otherwise a mark surrounds the train: a
// closed mark for a latched train, and only its corners for other phases.
// The connector has the accent color, and all other lines have the train
// color. The virtual platoon line joins only the virtual platoon.
func TestCouplingLines(t *testing.T) {
	t.Parallel()
	bodies := func(input couplingDrawInput) []couplingLine {
		group := input.groups[0]
		width := couplingBodyWidth * input.unit
		return append(outline(input, group.Bodies[0], width), outline(input, group.Bodies[1], width)...)
	}
	envelope := func(input couplingDrawInput) []couplingLine {
		return outline(input, *input.groups[0].ManeuverEnvelope, couplingEnvelopeWidth*input.unit)
	}
	// The connector is 3 pixels wide, wider than its smallest width.
	connector := couplingLine{from: sim.Point{X: 180, Y: 50}, to: sim.Point{X: 175, Y: 50}, width: 3, color: accent}
	// The bodies span x 135 to 220 and y 40 to 60. The cabin centers are at
	// x 155 and 200. The mark keeps 12 pixels from each center.
	mark := [4]sim.Point{{X: 220, Y: 62}, {X: 135, Y: 62}, {X: 135, Y: 38}, {X: 220, Y: 38}}
	tests := []struct {
		name  string
		state sim.Snapshot
		unit  float64
		want  func(couplingDrawInput) []couplingLine
	}{
		{name: "unmarked", state: sim.Snapshot{Vehicles: couplingFixture(sim.CouplingConnected).Vehicles}, unit: 1, want: func(couplingDrawInput) []couplingLine { return nil }},
		{name: "closing", state: couplingFixture(sim.CouplingClosing), unit: 1, want: func(input couplingDrawInput) []couplingLine {
			return append(envelope(input), bodies(input)...)
		}},
		{name: "latching", state: couplingFixture(sim.CouplingLatching), unit: 1, want: func(input couplingDrawInput) []couplingLine {
			return append(bodies(input), connector)
		}},
		{name: "connected", state: couplingFixture(sim.CouplingConnected), unit: 1, want: func(input couplingDrawInput) []couplingLine {
			return append(bodies(input), connector)
		}},
		{name: "unlatching", state: couplingFixture(sim.CouplingUnlatching), unit: 1, want: func(input couplingDrawInput) []couplingLine {
			return append(bodies(input), connector)
		}},
		{name: "opening", state: couplingFixture(sim.CouplingOpening), unit: 1, want: func(input couplingDrawInput) []couplingLine {
			return append(envelope(input), bodies(input)...)
		}},
		{name: "draining", state: couplingFixture(sim.CouplingDraining), unit: 1, want: bodies},
		{name: "narrow connector", state: couplingFixture(sim.CouplingConnected), unit: 1.6, want: func(input couplingDrawInput) []couplingLine {
			narrow := connector
			narrow.width = 3.2
			return append(bodies(input), narrow)
		}},
		{name: "connected mark", state: couplingFixture(sim.CouplingConnected), unit: 2, want: func(couplingDrawInput) []couplingLine {
			return appendOutline(nil, mark, 4)
		}},
		{name: "closing mark", state: couplingFixture(sim.CouplingClosing), unit: 2, want: func(couplingDrawInput) []couplingLine {
			return appendCorners(nil, mark, 4)
		}},
		{name: "draining mark", state: couplingFixture(sim.CouplingDraining), unit: 2, want: func(couplingDrawInput) []couplingLine {
			return appendCorners(nil, mark, 4)
		}},
		{name: "invalid body", state: func() sim.Snapshot {
			state := couplingFixture(sim.CouplingConnected)
			state.CouplingGroups[0].Bodies[1].Corners[2].Y = math.NaN()
			return state
		}(), unit: 1, want: func(couplingDrawInput) []couplingLine { return nil }},
		{name: "invalid connector", state: func() sim.Snapshot {
			state := couplingFixture(sim.CouplingConnected)
			state.CouplingGroups[0].Connector.Corners[0].X = math.Inf(1)
			return state
		}(), unit: 1, want: bodies},
		{name: "invalid envelope", state: func() sim.Snapshot {
			state := couplingFixture(sim.CouplingOpening)
			state.CouplingGroups[0].ManeuverEnvelope.Corners[3].Y = math.NaN()
			return state
		}(), unit: 1, want: bodies},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := couplingTestInput(test.state, test.unit)
			want := test.want(input)
			if got := couplingLines(input); !slices.Equal(got, want) {
				t.Fatalf("lines = %+v\nwant %+v", got, want)
			}
			// The virtual platoon line joins c to d only. Train members
			// have no virtual platoon fields.
			if got := platoonLinks(test.state.Vehicles); !slices.Equal(got, []platoonLink{{follower: 0, ahead: 3}}) {
				t.Fatalf("platoon links = %+v", got)
			}
		})
	}
}

// segmentDistance returns the distance from p to the segment from a to b.
func segmentDistance(p, a, b sim.Point) float64 {
	dx, dy := b.X-a.X, b.Y-a.Y
	t := 0.0
	if length := dx*dx + dy*dy; length > 0 {
		t = max(0, min(1, ((p.X-a.X)*dx+(p.Y-a.Y)*dy)/length))
	}
	return math.Hypot(p.X-a.X-t*dx, p.Y-a.Y-t*dy)
}

// floatBounds is a screen rectangle with float edges.
type floatBounds struct{ left, top, right, bottom float64 }

func (b floatBounds) Dx() float64 { return b.right - b.left }
func (b floatBounds) Dy() float64 { return b.bottom - b.top }

func (b floatBounds) contains(p sim.Point) bool {
	return p.X > b.left && p.X < b.right && p.Y > b.top && p.Y < b.bottom
}

// lineBounds returns the bounds of the ends of lines.
func lineBounds(lines []couplingLine) floatBounds {
	b := floatBounds{left: math.Inf(1), top: math.Inf(1), right: math.Inf(-1), bottom: math.Inf(-1)}
	for _, line := range lines {
		for _, p := range []sim.Point{line.from, line.to} {
			b = floatBounds{left: min(b.left, p.X), top: min(b.top, p.Y), right: max(b.right, p.X), bottom: max(b.bottom, p.Y)}
		}
	}
	return b
}

// moveGroup returns group moved by offset.
func moveGroup(group sim.CouplingGroupView, offset sim.Point) sim.CouplingGroupView {
	move := func(shape *sim.CouplingRectangle) {
		for i := range shape.Corners {
			shape.Corners[i].X += offset.X
			shape.Corners[i].Y += offset.Y
		}
	}
	for i := range group.Bodies {
		move(&group.Bodies[i])
	}
	for _, shape := range []**sim.CouplingRectangle{&group.Connector, &group.ManeuverEnvelope} {
		if *shape != nil {
			moved := **shape
			move(&moved)
			*shape = &moved
		}
	}
	return group
}

// TestCouplingMarkClearsPodSprites places a train on the London map at the
// fitted zoom and at the largest zoom. There a train is smaller than a pod
// sprite. Each train line must keep clear of the sprites of both cabins, so
// that no sprite can cover it, and the mark must have its smallest size.
// The game draws the lines after the pods as well.
func TestCouplingMarkClearsPodSprites(t *testing.T) {
	t.Parallel()
	for _, zoom := range []float64{1, mapMaxZoom} {
		for _, phase := range []sim.CouplingPhase{sim.CouplingClosing, sim.CouplingConnected, sim.CouplingDraining} {
			t.Run(fmt.Sprintf("zoom %g %s", zoom, phase), func(t *testing.T) {
				t.Parallel()
				game := londonPickGame(t)
				viewport := game.layout.mapViewport
				game.camera.zoomAt(sim.Point{X: float64(viewport.Min.X+viewport.Max.X) / 2, Y: float64(viewport.Min.Y+viewport.Max.Y) / 2}, zoom)
				game.syncCamera()
				// Put the train at the world point in the center of the map.
				target := game.camera.worldPoint(sim.Point{X: float64(viewport.Min.X+viewport.Max.X) / 2, Y: float64(viewport.Min.Y+viewport.Max.Y) / 2})
				group := moveGroup(couplingFixture(phase).CouplingGroups[0], target)
				t.Logf("map scale %g px/m, unit %g px", game.mapScale, game.layout.unit)
				if game.mapScale >= couplingMarkPadding*game.layout.unit {
					t.Fatalf("map scale %g px/m shows the real bodies, want a train mark", game.mapScale)
				}
				lines := game.mapCouplingLines(sim.Snapshot{CouplingGroups: []sim.CouplingGroupView{group}})
				if len(lines) == 0 {
					t.Fatal("no train lines")
				}
				centers := [2]sim.Point{game.mapPoint(center(group.Bodies[0])), game.mapPoint(center(group.Bodies[1]))}
				clearance := podRadius * game.layout.unit
				bounds := lineBounds(lines)
				for _, line := range lines {
					if line.color != couplingTrainColor || line.width < couplingMarkWidth*game.layout.unit {
						t.Fatalf("line %+v has the wrong color or width", line)
					}
					for _, c := range centers {
						if gap := segmentDistance(c, line.from, line.to) - line.width/2; gap < clearance {
							t.Fatalf("line %+v is %g px from a cabin center, want %g px or more", line, gap+line.width/2, clearance+line.width/2)
						}
					}
				}
				if side := 2 * couplingMarkPadding * game.layout.unit; bounds.Dx() < side || bounds.Dy() < side {
					t.Fatalf("mark is %g x %g px, want at least %g px on each side", bounds.Dx(), bounds.Dy(), side)
				}
			})
		}
	}
}

// TestCouplingRow checks the Train row of the pod inspector. It comes from
// the train registry, names the other cabin by its fleet number, and does
// not show for a virtual platoon.
// TestCouplingMarkTurnedBodies checks that turned draining bodies get the
// train mark when each body is narrower than the mark, even though the two
// bodies together are wider than the mark across the axis.
func TestCouplingMarkTurnedBodies(t *testing.T) {
	t.Parallel()
	// A 4 x 2 m body along x at the origin, and a 2 x 4 m body turned a
	// quarter turn, with its center 3 m along each axis.
	front := couplingBox(2, -2, 1)
	rear := sim.CouplingRectangle{Corners: [4]sim.Point{{X: 2, Y: 5}, {X: 2, Y: 1}, {X: 4, Y: 1}, {X: 4, Y: 5}}}
	group := sim.CouplingGroupView{Phase: sim.CouplingDraining, Bodies: [2]sim.CouplingRectangle{front, rear}}
	const scale = 3.0
	input := couplingDrawInput{screen: func(p sim.Point) sim.Point { return sim.Point{X: p.X * scale, Y: p.Y * scale} }, unit: 1}
	_, legible, ok := couplingMark(group, input)
	if !ok {
		t.Fatal("finite bodies were rejected")
	}
	if legible {
		t.Fatalf("bodies with %g px half widths show as real geometry, want a train mark (padding %g px)", input.halfWidth(front), float64(couplingMarkPadding))
	}
	wide := couplingDrawInput{screen: func(p sim.Point) sim.Point { return sim.Point{X: p.X * 10, Y: p.Y * 10} }, unit: 1}
	if _, legible, _ := couplingMark(group, wide); !legible {
		t.Fatal("bodies with 10 px half widths show as a train mark, want real geometry")
	}
}

func TestCouplingRow(t *testing.T) {
	t.Parallel()
	missing := couplingFixture(sim.CouplingConnected)
	missing.Vehicles = missing.Vehicles[:2]
	orphan := couplingFixture(sim.CouplingConnected)
	orphan.CouplingGroups = nil
	unnamed := couplingFixture(sim.CouplingConnected)
	unnamed.CouplingGroups[0].ID = ""
	tests := []struct {
		name  string
		state sim.Snapshot
		index int
		want  inspectionRow
		ok    bool
	}{
		{name: "front cabin", state: couplingFixture(sim.CouplingConnected), index: 1, want: inspectionRow{"Train", "Connected / 03"}, ok: true},
		{name: "rear cabin", state: couplingFixture(sim.CouplingUnlatching), index: 2, want: inspectionRow{"Train", "Unlatching / 02"}, ok: true},
		{name: "closing", state: couplingFixture(sim.CouplingClosing), index: 2, want: inspectionRow{"Train", "Closing / 02"}, ok: true},
		{name: "other cabin not in the state", state: missing, index: 1, want: inspectionRow{"Train", "Connected / b"}, ok: true},
		{name: "virtual platoon", state: couplingFixture(sim.CouplingConnected), index: 0},
		{name: "pod outside a group without ID", state: unnamed, index: 0},
		{name: "no registry record", state: orphan, index: 1},
		{name: "unmarked", state: sim.Snapshot{Vehicles: []sim.Vehicle{{Pod: sim.Pod{ID: "a"}}}}, index: 0},
		{name: "past the fleet", state: couplingFixture(sim.CouplingConnected), index: 4},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got, ok := couplingRow(test.state, test.index); got != test.want || ok != test.ok {
				t.Fatalf("row = %q, %v, want %q, %v", got, ok, test.want, test.ok)
			}
		})
	}
}

// TestCouplingInspectionRowsFit checks that the Train row comes before the
// station phase rows, that each Train value shows in full for the largest
// fleet, and that the most rows still end above the pod selector.
func TestCouplingInspectionRowsFit(t *testing.T) {
	t.Parallel()
	phases := []sim.CouplingPhase{sim.CouplingClosing, sim.CouplingLatching, sim.CouplingConnected, sim.CouplingUnlatching, sim.CouplingOpening, sim.CouplingDraining}
	inputs := map[string]layoutInput{"laptop 1366x617": {outsideWidth: 1366, outsideHeight: 617, deviceScale: 1}}
	for _, layout := range controlLayouts {
		inputs[layout.name] = layout.input
	}
	for name, input := range inputs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			game := controlTestGame(t, input)
			for _, phase := range phases {
				state := couplingFixture(phase)
				// The other cabin has the widest fleet number.
				state.Vehicles = append(make([]sim.Vehicle, 297), state.Vehicles...)
				state.Vehicles[298].Pod.Occupied, state.Vehicles[298].Riders = true, []sim.Request{{PartySize: 1}}
				state.Vehicles[298].Pod.StationPhase, state.Vehicles[298].Pod.ManeuverStationID = sim.ApproachingStation, "station-01"
				rows := game.podInspectionRows(state, 298)
				if len(rows) != 5 || rows[2].name != "Train" || rows[3].name != "Station phase" {
					t.Fatalf("rows = %q", rows)
				}
				if got := game.fitInspectionValue(rows[2]); got != rows[2].value {
					t.Errorf("row %q shows %q, want %q", rows[2].name, got, rows[2].value)
				}
				_, height := text.Measure("Station phase", game.textFace(14), 0)
				bottom := (inspectionRowsTop+float64(len(rows)-1)*inspectionRowSpacing)*game.layout.unit + height
				if top := podSelectorTop * game.layout.unit; bottom >= top {
					t.Fatalf("inspection rows end at %g, pod selector starts at %g", bottom, top)
				}
			}
		})
	}
}

// TestDrawCouplingState draws one frame of a train with a selected cabin.
// Draw must not panic.
func TestDrawCouplingState(t *testing.T) {
	t.Parallel()
	game := exampleTestGame(t)
	state := &game.state.Simulation
	group := couplingFixture(sim.CouplingConnected).CouplingGroups[0]
	group.Members = [2]string{state.Vehicles[0].Pod.ID, state.Vehicles[1].Pod.ID}
	state.CouplingContract, state.CouplingGroups = sim.CompactPairV1CouplingContract, []sim.CouplingGroupView{group}
	state.Vehicles[0].CouplingID, state.Vehicles[1].CouplingID = group.ID, group.ID
	if rows := game.podInspectionRows(*state, 0); rows[2] != (inspectionRow{"Train", "Connected / 02"}) {
		t.Fatalf("rows = %q", rows)
	}
	screen := ebiten.NewImage(game.layout.width, game.layout.height)
	defer screen.Deallocate()
	game.Draw(screen)
	// Draw fitted the camera. The train lines surround both cabin centers
	// at their camera positions.
	lines := game.mapCouplingLines(*state)
	bounds := lineBounds(lines)
	for _, body := range group.Bodies {
		if c := game.mapPoint(center(body)); len(lines) == 0 || !bounds.contains(c) {
			t.Fatalf("map lines %+v do not surround cabin center %v", lines, c)
		}
	}
}

// TestTrainLegendFits checks the Train entry of the map legend in each
// layout. It shows the closed train mark around two pods, clear of both pod
// sprites. The mark starts after the Selected label, and the Train label
// ends inside the map panel.
func TestTrainLegendFits(t *testing.T) {
	t.Parallel()
	for _, layout := range controlLayouts {
		t.Run(layout.name, func(t *testing.T) {
			t.Parallel()
			game := controlTestGame(t, layout.input)
			centers := game.trainLegendCenters()
			lines := trainLegendLines(centers, game.layout.unit)
			if len(lines) != 4 {
				t.Fatalf("legend lines = %+v, want a closed mark", lines)
			}
			for _, line := range lines {
				for _, c := range centers {
					if gap := segmentDistance(c, line.from, line.to) - line.width/2; gap < podRadius*game.layout.unit {
						t.Fatalf("legend line %+v covers a pod at %v", line, c)
					}
				}
			}
			bounds := lineBounds(lines)
			stroke := couplingMarkWidth * game.layout.unit / 2
			selected, _ := game.measureLabel(label{size: 10, value: "Selected", physical: true})
			if right := game.layout.x(619) + selected; bounds.left-stroke <= right {
				t.Fatalf("train mark starts at %g, Selected label ends at %g", bounds.left-stroke, right)
			}
			trainLabel := game.trainLegendLabel()
			width, _ := game.measureLabel(trainLabel)
			if trainLabel.x <= bounds.right+stroke {
				t.Fatalf("Train label starts at %g, mark ends at %g", trainLabel.x, bounds.right+stroke)
			}
			if right, panel := trainLabel.x+width, game.layout.x(24)+game.layout.x(748)+game.layout.extraX; right >= panel {
				t.Fatalf("Train label ends at %g, map panel ends at %g", right, panel)
			}
			if bottom, panel := bounds.bottom+stroke, game.layout.y(64)+game.layout.y(474)+game.layout.extraY; bottom >= panel {
				t.Fatalf("train mark ends at %g, map panel ends at %g", bottom, panel)
			}
		})
	}
}
