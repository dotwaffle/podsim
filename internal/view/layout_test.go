package view

import (
	"cmp"
	"fmt"
	"image"
	"maps"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/hajimehoshi/ebiten/v2/text/v2"

	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

// controlLayouts are the window sizes for the control overlap and text fit
// tests. They include short laptop windows, fractional device scales, and a
// window below the minimum size.
var controlLayouts = []struct {
	name  string
	input layoutInput
}{
	{name: "minimum", input: layoutInput{outsideWidth: 1100, outsideHeight: 760, deviceScale: 1}},
	{name: "below minimum", input: layoutInput{outsideWidth: 800, outsideHeight: 560, deviceScale: 1}},
	{name: "short minimum width", input: layoutInput{outsideWidth: 1100, outsideHeight: 600, deviceScale: 1}},
	{name: "laptop 1366", input: layoutInput{outsideWidth: 1366, outsideHeight: 728, deviceScale: 1}},
	{name: "laptop 1280", input: layoutInput{outsideWidth: 1280, outsideHeight: 680, deviceScale: 1}},
	{name: "laptop 1440 DPR2", input: layoutInput{outsideWidth: 1440, outsideHeight: 750, deviceScale: 2}},
	{name: "tall half 4K DPR2", input: layoutInput{outsideWidth: 960, outsideHeight: 1040, deviceScale: 2}},
	{name: "desktop 1600", input: layoutInput{outsideWidth: 1600, outsideHeight: 1000, deviceScale: 1}},
	{name: "desktop 1600 DPR2", input: layoutInput{outsideWidth: 1600, outsideHeight: 1000, deviceScale: 2}},
	{name: "desktop 1920 DPR1.25", input: layoutInput{outsideWidth: 1920, outsideHeight: 1000, deviceScale: 1.25}},
	{name: "desktop 1920 DPR1.5", input: layoutInput{outsideWidth: 1920, outsideHeight: 1000, deviceScale: 1.5}},
	{name: "fractional DPR", input: layoutInput{outsideWidth: 1100, outsideHeight: 760, deviceScale: 1.5}},
	{name: "tall desktop", input: layoutInput{outsideWidth: 1920, outsideHeight: 2160, deviceScale: 1}},
	{name: "full 4K", input: layoutInput{outsideWidth: 3840, outsideHeight: 2160, deviceScale: 1}},
}

// controlTestGame returns a connected game with enough stations and pods to
// show the station and pod page arrows.
func controlTestGame(t *testing.T, input layoutInput) *Game {
	t.Helper()
	game := journeyTestGame(t, 20)
	game.state.Simulation.Vehicles = make([]sim.Vehicle, 8)
	for i := range game.state.Simulation.Vehicles {
		game.state.Simulation.Vehicles[i].Pod.ID = fleetPodLabel(i)
	}
	game.layoutFor(input)
	return game
}

// area is a screen rectangle in physical pixels.
type area struct{ left, top, right, bottom float64 }

func buttonArea(control button) area {
	return area{left: control.x, top: control.y, right: control.x + control.w, bottom: control.y + control.h}
}

// labelArea returns the measured text box of a label at its drawn position.
func (g *Game) labelArea(value label) area {
	x, y := value.x, value.y
	if !value.physical {
		x, y = g.layout.labelPosition(value.x, value.y)
	}
	width, height := text.Measure(value.value, g.textFace(value.size), 0)
	return area{left: x, top: y, right: x + width, bottom: y + height}
}

func (a area) overlaps(b area) bool {
	return a.left < b.right && b.left < a.right && a.top < b.bottom && b.top < a.bottom
}

func TestDisplayLayoutDimensionsAndAnchors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                  string
		input                 layoutInput
		wantWidth, wantHeight int
		wantUnit              float64
		wantMap               image.Rectangle
	}{
		{name: "baseline", input: layoutInput{outsideWidth: 1100, outsideHeight: 760, deviceScale: 1}, wantWidth: 1100, wantHeight: 760, wantUnit: 1, wantMap: image.Rect(24, 136, 772, 520)},
		{name: "tall half 4K DPR2", input: layoutInput{outsideWidth: 960, outsideHeight: 1040, deviceScale: 2}, wantWidth: 1920, wantHeight: 2080, wantUnit: 1920.0 / 1100, wantMap: image.Rect(42, 237, 1347, 1661)},
		{name: "tall desktop", input: layoutInput{outsideWidth: 1920, outsideHeight: 2160, deviceScale: 1}, wantWidth: 1920, wantHeight: 2160, wantUnit: 1, wantMap: image.Rect(24, 136, 1592, 1920)},
		{name: "full 4K", input: layoutInput{outsideWidth: 3840, outsideHeight: 2160, deviceScale: 1}, wantWidth: 3840, wantHeight: 2160, wantUnit: 1, wantMap: image.Rect(24, 136, 3512, 1920)},
		{name: "fractional DPR", input: layoutInput{outsideWidth: 1100, outsideHeight: 760, deviceScale: 1.5}, wantWidth: 1650, wantHeight: 1140, wantUnit: 1.5, wantMap: image.Rect(36, 204, 1158, 780)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := newDisplayLayout(test.input)
			if got.width != test.wantWidth || got.height != test.wantHeight || got.unit != test.wantUnit || got.mapViewport != test.wantMap {
				t.Fatalf("layout = %dx%d unit %g map %v, want %dx%d unit %g map %v", got.width, got.height, got.unit, got.mapViewport, test.wantWidth, test.wantHeight, test.wantUnit, test.wantMap)
			}
			if got.right(1076) > float64(got.width) || got.bottom(732) > float64(got.height) {
				t.Fatalf("anchored panel escaped layout: right=%g bottom=%g", got.right(1076), got.bottom(732))
			}
		})
	}
}

func TestDisplayLayoutInvalidInputFallsBack(t *testing.T) {
	t.Parallel()
	tests := []layoutInput{
		{},
		{outsideWidth: -1, outsideHeight: -1, deviceScale: -1},
	}
	for _, input := range tests {
		got := newDisplayLayout(input)
		if got.width != minimumWidth || got.height != minimumHeight || got.unit != 1 {
			t.Fatalf("fallback layout = %+v", got)
		}
	}
}

func TestGameLayoutMovesControlsAndInvalidatesCamera(t *testing.T) {
	t.Parallel()
	game := cameraTestGame()
	game.layoutFor(layoutInput{outsideWidth: 1920, outsideHeight: 2160, deviceScale: 1})
	if game.camera.initialized {
		t.Fatal("resize kept camera fitted to stale viewport")
	}
	game.fitNetwork()
	if game.camera.viewport != game.layout.mapViewport {
		t.Fatalf("camera viewport = %v, want %v", game.camera.viewport, game.layout.mapViewport)
	}
	oldEdge := image.Pt(771, 519)
	newArea := image.Pt(1200, 900)
	if !oldEdge.In(game.camera.viewport) || !newArea.In(game.camera.viewport) {
		t.Fatalf("resized map does not contain old edge %v and expanded area %v: %v", oldEdge, newArea, game.camera.viewport)
	}
	if !game.camera.contains(pointFromImage(newArea)) {
		t.Fatal("camera input clipping rejected expanded map area")
	}
	for _, control := range game.buttons() {
		if control.x < 0 || control.y < 0 || control.x+control.w > float64(game.layout.width) || control.y+control.h > float64(game.layout.height) {
			t.Fatalf("control %q outside layout: %+v", control.action, control)
		}
	}
}

func TestInspectionRowsClearPodSelector(t *testing.T) {
	t.Parallel()
	game := journeyTestGame(t, 2)
	for _, input := range []layoutInput{
		{outsideWidth: minimumWidth, outsideHeight: minimumHeight, deviceScale: 1},
		{outsideWidth: 1600, outsideHeight: 1000, deviceScale: 1},
		{outsideWidth: 1600, outsideHeight: 1000, deviceScale: 2},
	} {
		game.layoutFor(input)
		// An occupied pod in a station maneuver has the most rows.
		rows := game.inspectionRows(sim.Vehicle{
			Pod:     sim.Pod{Occupied: true, StationPhase: sim.ApproachingStation, ManeuverStationID: "station-01"},
			Request: &sim.Request{PartySize: 1},
		})
		if len(rows) != 4 {
			t.Fatalf("inspection rows = %q, want 4 rows", rows)
		}
		last := len(rows) - 1
		_, height := text.Measure("Station phase", game.textFace(14), 0)
		lastRowBottom := (inspectionRowsTop+float64(last)*inspectionRowSpacing)*game.layout.unit + height
		selectorTop := podSelectorTop * game.layout.unit
		if lastRowBottom >= selectorTop {
			t.Fatalf("inspection rows end at %g, pod selector starts at %g for %+v", lastRowBottom, selectorTop, input)
		}
	}
}

func TestInspectorAndButtonTextFitAvailableWidth(t *testing.T) {
	t.Parallel()
	game := journeyTestGame(t, 2)
	game.layoutFor(layoutInput{outsideWidth: 1600, outsideHeight: 1000, deviceScale: 1})
	tests := []struct {
		name, value string
		size, width float64
		button      bool
	}{
		{name: "pod heading", value: "POD 01 / london-waterloo-parking-pod-001", size: 12, width: 140},
		{name: "status", value: "Waiting for destination access at Waterloo Underground Station", size: 13, width: inspectionRight - inspectionLeft},
		{name: "journey", value: "Heathrow Terminal 5 > King's Cross St Pancras", size: 17, width: inspectionRight - inspectionLeft},
		{name: "demand pattern", value: "Pattern: weekday-am-peak / london-weekday", size: 14, width: 250, button: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			limit := test.width * game.layout.unit
			got := game.fitText(test.value, test.size, test.width)
			if test.button {
				got = game.fitButtonText(test.value, test.size, limit)
				limit -= 12 * game.layout.unit
			}
			width, _ := text.Measure(got, game.textFace(test.size), 0)
			if width > limit {
				t.Fatalf("fitted text %q width %g exceeds %g", got, width, limit)
			}
			if got == test.value {
				t.Fatalf("long text %q was not shortened", test.value)
			}
		})
	}
}

func TestControlsDoNotOverlap(t *testing.T) {
	t.Parallel()
	savePoints := []session.Checkpoint{{ID: 1, Tick: 600}, {ID: 2, Tick: 1200, RestoresProject: true}}
	for _, layout := range controlLayouts {
		for _, showDemand := range []bool{false, true} {
			for _, checkpoints := range [][]session.Checkpoint{nil, savePoints} {
				name := fmt.Sprintf("%s demand %t save points %d", layout.name, showDemand, len(checkpoints))
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					game := controlTestGame(t, layout.input)
					game.showDemand = showDemand
					game.state.Checkpoints = checkpoints
					controls := game.buttons()
					findButton(t, controls, "checkpoint")
					findButton(t, controls, "rewind")
					bounds := area{right: float64(game.layout.width), bottom: float64(game.layout.height)}
					for i, control := range controls {
						got := buttonArea(control)
						if got.left < bounds.left || got.top < bounds.top || got.right > bounds.right || got.bottom > bounds.bottom {
							t.Errorf("control %q outside layout: %+v", control.action, control)
						}
						for _, other := range controls[i+1:] {
							if got.overlaps(buttonArea(other)) {
								t.Errorf("control %q %+v overlaps %q %+v", control.action, got, other.action, buttonArea(other))
							}
						}
					}
				})
			}
		}
	}
}

func TestSavePointLabelsFit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, action string
		checkpoint   session.Checkpoint
		want         string
	}{
		{name: "save point", action: "checkpoint", want: "Save point"},
		{name: "London AM peak end", action: "rewind", checkpoint: session.Checkpoint{ID: 1, Tick: 10800 * sim.TicksPerSecond}, want: "Rewind 10800.0 s"},
		{name: "long run", action: "rewind", checkpoint: session.Checkpoint{ID: 1, Tick: 999999 * sim.TicksPerSecond / 10}, want: "Rewind 99999.9 s"},
		{name: "project restore", action: "rewind", checkpoint: session.Checkpoint{ID: 1, Tick: 999999 * sim.TicksPerSecond / 10, RestoresProject: true}, want: "Rewind + project"},
	}
	for _, layout := range controlLayouts {
		for _, test := range tests {
			t.Run(layout.name+" "+test.name, func(t *testing.T) {
				t.Parallel()
				game := controlTestGame(t, layout.input)
				game.state.Checkpoints = []session.Checkpoint{test.checkpoint}
				control := findButton(t, game.buttons(), test.action)
				if control.label != test.want {
					t.Fatalf("label = %q, want %q", control.label, test.want)
				}
				if got := game.fitButtonText(control.label, cmp.Or(control.fontSize, 14), control.w); got != control.label {
					t.Fatalf("label %q shortened to %q in width %g", control.label, got, control.w)
				}
			})
		}
	}
}

func TestHeaderTextClearsControls(t *testing.T) {
	t.Parallel()
	for _, layout := range controlLayouts {
		t.Run(layout.name, func(t *testing.T) {
			t.Parallel()
			game := controlTestGame(t, layout.input)
			// The header counts stations and pods after the first state frame.
			game.state.Epoch = "test"
			game.state.Checkpoints = []session.Checkpoint{{ID: 1, Tick: 600}}
			controls := game.buttons()
			savePointRow := []button{findButton(t, controls, "checkpoint"), findButton(t, controls, "rewind")}
			for _, header := range game.headerLabels() {
				bounds := game.labelArea(header)
				for _, control := range controls {
					if bounds.overlaps(buttonArea(control)) {
						t.Errorf("header %q %+v overlaps control %q %+v", header.value, bounds, control.action, buttonArea(control))
					}
				}
				// The header stats sit in the right column above the save point row.
				if header.x < 796 {
					continue
				}
				for _, control := range savePointRow {
					if bounds.bottom >= control.y {
						t.Errorf("header %q ends at %g, control %q starts at %g", header.value, bounds.bottom, control.action, control.y)
					}
				}
			}
		})
	}
}

func TestConnectionLabelFitsLayout(t *testing.T) {
	t.Parallel()
	for _, layout := range controlLayouts {
		t.Run(layout.name, func(t *testing.T) {
			t.Parallel()
			game := controlTestGame(t, layout.input)
			for _, focused := range []bool{true, false} {
				footer := game.connectionFooter(focused)
				got := game.labelArea(footer)
				if got.right > float64(game.layout.width) || got.bottom > float64(game.layout.height) {
					t.Errorf("connection label %q %+v escapes layout %dx%d", footer.value, got, game.layout.width, game.layout.height)
				}
			}
		})
	}
}

// TestConnectionFooter checks the text and color of the line below the
// panels for each connection state, with and without the keyboard focus.
func TestConnectionFooter(t *testing.T) {
	t.Parallel()
	const (
		lost      = "Connection lost or connecting. Controls resume when the server is available."
		pending   = "Shared session / waiting for command confirmation"
		connected = "Shared session / connected. Playback, orders, demand, and save points are shared across all browsers. Pod inspection stays local."
	)
	tests := []struct {
		name                        string
		connected, pending, focused bool
		wantValue                   string
		wantColor                   uint32
	}{
		{name: "connected with focus", connected: true, focused: true, wantValue: connected, wantColor: muted},
		{name: "connected without focus", connected: true, wantValue: focusHint, wantColor: amber},
		{name: "pending with focus", connected: true, pending: true, focused: true, wantValue: pending, wantColor: muted},
		{name: "pending without focus", connected: true, pending: true, wantValue: pending, wantColor: muted},
		{name: "lost with focus", focused: true, wantValue: lost, wantColor: muted},
		{name: "lost without focus", wantValue: lost, wantColor: muted},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game := journeyTestGame(t, 2)
			game.connected, game.pending = test.connected, test.pending
			got := game.connectionFooter(test.focused)
			if got.value != test.wantValue || got.color != test.wantColor {
				t.Errorf("footer = %q color %#06x, want %q color %#06x", got.value, got.color, test.wantValue, test.wantColor)
			}
		})
	}
}

// rightPanelArea returns the right panel in physical pixels, as Draw fills
// it.
func (g *Game) rightPanelArea() area {
	return area{left: g.layout.right(796), top: g.layout.y(96), right: g.layout.right(1076), bottom: g.layout.y(570) + g.layout.extraY}
}

// rightBottomGroup reports if the control with action is in the group at
// the bottom of the right panel.
func rightBottomGroup(action string) bool {
	switch action {
	case "pause", "speed", "reset", "orders", "demand", "pods-prev", "pods-next":
		return true
	}
	return strings.HasPrefix(action, "pod/")
}

// TestRightPanelContentStaysInside checks that the controls, the fleet
// statistics and a full Orders panel stay inside the right panel, and that
// the order rows end above the pod selector.
func TestRightPanelContentStaysInside(t *testing.T) {
	t.Parallel()
	for _, layout := range controlLayouts {
		t.Run(layout.name, func(t *testing.T) {
			t.Parallel()
			game := controlTestGame(t, layout.input)
			game.showOrders = true
			for i := range 40 {
				game.state.Simulation.Pending = append(game.state.Simulation.Pending, sim.Request{ID: i + 1, From: "station-01", To: "station-02"})
			}
			panel := game.rightPanelArea()
			inside := func(got area) bool {
				return got.left >= panel.left && got.top >= panel.top && got.right <= panel.right && got.bottom <= panel.bottom
			}
			selectorTop := panel.bottom
			for _, control := range game.buttons() {
				if !rightBottomGroup(control.action) {
					continue
				}
				if got := buttonArea(control); !inside(got) {
					t.Errorf("control %q %+v escapes right panel %+v", control.action, got, panel)
				}
				selectorTop = min(selectorTop, control.y)
			}
			for _, value := range fleetStatLabels(game.state.Simulation) {
				if got := game.labelArea(value); !inside(got) {
					t.Errorf("label %q %+v escapes right panel %+v", value.value, got, panel)
				}
			}
			for _, value := range game.orderLabels(game.state.Simulation) {
				got := game.labelArea(value)
				if got.top < panel.top || got.bottom >= selectorTop {
					t.Errorf("order label %q %+v is not between panel top %g and pod selector %g", value.value, got, panel.top, selectorTop)
				}
			}
		})
	}
}

// TestRightBottomGroupMovesAsOneGroup checks that the pod selector, Pause,
// Speed, Reset, Orders and Demand keep the same distance to the bottom of
// the right panel in every window. So no gap opens between them on a tall
// window.
func TestRightBottomGroupMovesAsOneGroup(t *testing.T) {
	t.Parallel()
	offsets := func(game *Game) map[string]float64 {
		got := map[string]float64{}
		bottom := game.rightPanelArea().bottom
		for _, control := range game.buttons() {
			if rightBottomGroup(control.action) {
				got[control.action] = (bottom - control.y) / game.layout.unit
			}
		}
		return got
	}
	want := offsets(controlTestGame(t, controlLayouts[0].input))
	for _, layout := range controlLayouts {
		got := offsets(controlTestGame(t, layout.input))
		for action, offset := range want {
			if math.Abs(got[action]-offset) > 1e-9 {
				t.Errorf("%s: control %q is %g units above the panel bottom, want %g", layout.name, action, got[action], offset)
			}
		}
	}
}

// TestRightBottomGroupHasNoGap checks that each row of the bottom group
// starts at most 6 units below the row above it. So no fixed gap opens
// between the pod selector, Pause, Speed and Reset, and Orders and Demand.
func TestRightBottomGroupHasNoGap(t *testing.T) {
	t.Parallel()
	game := controlTestGame(t, controlLayouts[0].input)
	rows := map[float64]float64{}
	for _, control := range game.buttons() {
		if rightBottomGroup(control.action) {
			rows[control.y] = max(rows[control.y], control.y+control.h)
		}
	}
	tops := slices.Sorted(maps.Keys(rows))
	if len(tops) != 4 {
		t.Fatalf("bottom group has %d rows, want 4", len(tops))
	}
	for i := 1; i < len(tops); i++ {
		if gap := (tops[i] - rows[tops[i-1]]) / game.layout.unit; gap > 6+1e-9 {
			t.Errorf("row at y %g starts %g units below the row above it, want at most 6", tops[i], gap)
		}
	}
}

func TestOrderRowLimit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		extra float64
		want  int
	}{
		{name: "minimum", extra: 0, want: 5},
		{name: "negative", extra: -100, want: 5},
		{name: "almost one more row", extra: 28, want: 5},
		{name: "one more row", extra: 29, want: 6},
		{name: "1080 high window", extra: 320, want: 13},
	}
	for _, test := range tests {
		if got := orderRowLimit(test.extra); got != test.want {
			t.Errorf("%s: orderRowLimit(%g) = %d, want %d", test.name, test.extra, got, test.want)
		}
	}
}

func pointFromImage(point image.Point) sim.Point {
	return sim.Point{X: float64(point.X), Y: float64(point.Y)}
}
