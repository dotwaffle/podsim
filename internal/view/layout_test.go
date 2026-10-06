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
	{name: "minimum", input: layoutInput{outsideWidth: 1100, outsideHeight: 728, deviceScale: 1}},
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
	{name: "fractional DPR", input: layoutInput{outsideWidth: 1100, outsideHeight: 728, deviceScale: 1.5}},
	{name: "tall desktop", input: layoutInput{outsideWidth: 1920, outsideHeight: 2160, deviceScale: 1}},
	{name: "full 4K", input: layoutInput{outsideWidth: 3840, outsideHeight: 2160, deviceScale: 1}},
}

// controlTestGame returns a connected game in the shell page with enough
// stations and pods to show the station and pod page arrows.
func controlTestGame(t *testing.T, input layoutInput) *Game {
	t.Helper()
	game := journeyTestGame(t, 20)
	game.shell = newFakeShell()
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
	width, height := text.Measure(value.value, g.labelFace(value), 0)
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
		{name: "baseline", input: layoutInput{outsideWidth: 1100, outsideHeight: 728, deviceScale: 1}, wantWidth: 1100, wantHeight: 728, wantUnit: 1, wantMap: image.Rect(24, 104, 772, 488)},
		{name: "tall half 4K DPR2", input: layoutInput{outsideWidth: 960, outsideHeight: 1040, deviceScale: 2}, wantWidth: 1920, wantHeight: 2080, wantUnit: 1920.0 / 1100, wantMap: image.Rect(42, 182, 1347, 1661)},
		{name: "tall desktop", input: layoutInput{outsideWidth: 1920, outsideHeight: 2160, deviceScale: 1}, wantWidth: 1920, wantHeight: 2160, wantUnit: 1, wantMap: image.Rect(24, 104, 1592, 1920)},
		{name: "full 4K", input: layoutInput{outsideWidth: 3840, outsideHeight: 2160, deviceScale: 1}, wantWidth: 3840, wantHeight: 2160, wantUnit: 1, wantMap: image.Rect(24, 104, 3512, 1920)},
		{name: "fractional DPR", input: layoutInput{outsideWidth: 1100, outsideHeight: 728, deviceScale: 1.5}, wantWidth: 1650, wantHeight: 1092, wantUnit: 1.5, wantMap: image.Rect(36, 156, 1158, 732)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := newDisplayLayout(test.input)
			if got.width != test.wantWidth || got.height != test.wantHeight || got.unit != test.wantUnit || got.mapViewport != test.wantMap {
				t.Fatalf("layout = %dx%d unit %g map %v, want %dx%d unit %g map %v", got.width, got.height, got.unit, got.mapViewport, test.wantWidth, test.wantHeight, test.wantUnit, test.wantMap)
			}
			if got.right(1076) > float64(got.width) || got.bottom(700) > float64(got.height) {
				t.Fatalf("anchored panel escaped layout: right=%g bottom=%g", got.right(1076), got.bottom(700))
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
		if got.width != minimumWidth || got.height != minimumHeight || got.unit != 1 || got.deviceScale != 1 {
			t.Fatalf("fallback layout = %+v", got)
		}
	}
}

// TestScreenScale checks the device scale that keeps the screen image within
// the image limit. The side is the longer side of the window in CSS pixels.
// A screen image that fits keeps the device scale. A larger one gets a scale
// that makes its longer side exactly the limit. The rows with a scale of 1000
// have sides where the division rounds up.
func TestScreenScale(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input screenScaleInput
		// wantSide is the longer side of the screen image in pixels.
		wantSide int
		clamped  bool
	}{
		{name: "2560 at 2", input: screenScaleInput{side: 2560, deviceScale: 2, limit: headlessImageLimit}, wantSide: 5120},
		{name: "widest at 2", input: screenScaleInput{side: 4095, deviceScale: 2, limit: headlessImageLimit}, wantSide: 8190},
		{name: "8192 pixels at 2", input: screenScaleInput{side: 4096, deviceScale: 2, limit: headlessImageLimit}, wantSide: 8191, clamped: true},
		{name: "4200 at 2", input: screenScaleInput{side: 4200, deviceScale: 2, limit: headlessImageLimit}, wantSide: 8191, clamped: true},
		{name: "4200 at 2 on a larger GPU", input: screenScaleInput{side: 4200, deviceScale: 2, limit: 16383}, wantSide: 8400},
		{name: "4200 at 2 unknown limit", input: screenScaleInput{side: 4200, deviceScale: 2}, wantSide: 8400},
		{name: "9000 at 1", input: screenScaleInput{side: 9000, deviceScale: 1, limit: headlessImageLimit}, wantSide: 8191, clamped: true},
		{name: "fractional scale", input: screenScaleInput{side: 3000, deviceScale: 2.75, limit: headlessImageLimit}, wantSide: 8191, clamped: true},
		{name: "rounding 25", input: screenScaleInput{side: 25, deviceScale: 1000, limit: 4095}, wantSide: 4095, clamped: true},
		{name: "rounding 50", input: screenScaleInput{side: 50, deviceScale: 1000, limit: 4095}, wantSide: 4095, clamped: true},
		{name: "rounding 53", input: screenScaleInput{side: 53, deviceScale: 1000, limit: 4095}, wantSide: 4095, clamped: true},
		{name: "rounding 59", input: screenScaleInput{side: 59, deviceScale: 1000, limit: 4095}, wantSide: 4095, clamped: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := screenScale(test.input)
			side := int(math.Ceil(float64(test.input.side) * got))
			if side != test.wantSide {
				t.Errorf("screenScale(%+v) = %g, screen side %d, want %d", test.input, got, side, test.wantSide)
			}
			if !test.clamped && got != test.input.deviceScale {
				t.Errorf("screenScale(%+v) = %g, want the device scale", test.input, got)
			}
			if want := float64(test.input.limit) / float64(test.input.side); test.clamped && !closeTo(got, want) {
				t.Errorf("screenScale(%+v) = %g, want about %g", test.input, got, want)
			}
		})
	}
}

// TestScreenScaleFitsEverySide checks that the screen image fits the limit,
// and that a clamped screen image uses the full limit, for each window side
// up to 20000 CSS pixels.
func TestScreenScaleFitsEverySide(t *testing.T) {
	t.Parallel()
	for _, limit := range []int{4095, headlessImageLimit, 16383} {
		for side := 1; side <= 20000; side++ {
			input := screenScaleInput{side: side, deviceScale: 3, limit: limit}
			got := int(math.Ceil(float64(side) * screenScale(input)))
			if got > limit || side*3 > limit && got != limit {
				t.Fatalf("screenScale(%+v) gives a screen side of %d pixels, want %d", input, got, min(side*3, limit))
			}
		}
	}
}

// TestDisplayLayoutFitsImageLimit checks the layout of windows that are too
// large for the image limit at their device scale. The screen gets a lower
// device scale, and the panels and the map stay inside the screen. The
// window below the minimum window also shrinks its unit with the window.
func TestDisplayLayoutFitsImageLimit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                  string
		input                 layoutInput
		wantWidth, wantHeight int
		wantScale, wantUnit   float64
	}{
		{name: "4200 by 2400 at 2", input: layoutInput{outsideWidth: 4200, outsideHeight: 2400, deviceScale: 2, imageLimit: headlessImageLimit}, wantWidth: 8191, wantHeight: 4681, wantScale: 8191.0 / 4200, wantUnit: 8191.0 / 4200},
		{name: "tall 1200 by 4500 at 2", input: layoutInput{outsideWidth: 1200, outsideHeight: 4500, deviceScale: 2, imageLimit: headlessImageLimit}, wantWidth: 2185, wantHeight: 8191, wantScale: 8191.0 / 4500, wantUnit: 8191.0 / 4500},
		{name: "9000 by 3000 at 1", input: layoutInput{outsideWidth: 9000, outsideHeight: 3000, deviceScale: 1, imageLimit: headlessImageLimit}, wantWidth: 8191, wantHeight: 2731, wantScale: 8191.0 / 9000, wantUnit: 8191.0 / 9000},
		{name: "below minimum at 12", input: layoutInput{outsideWidth: 800, outsideHeight: 560, deviceScale: 12, imageLimit: headlessImageLimit}, wantWidth: 8191, wantHeight: 5734, wantScale: 8191.0 / 800, wantUnit: 8191.0 / 1100},
		{name: "3840 by 2160 at 2 fits", input: layoutInput{outsideWidth: 3840, outsideHeight: 2160, deviceScale: 2, imageLimit: headlessImageLimit}, wantWidth: 7680, wantHeight: 4320, wantScale: 2, wantUnit: 2},
		{name: "4200 by 2400 at 2 on a larger GPU", input: layoutInput{outsideWidth: 4200, outsideHeight: 2400, deviceScale: 2, imageLimit: 16383}, wantWidth: 8400, wantHeight: 4800, wantScale: 2, wantUnit: 2},
		{name: "4200 by 2400 at 2 unknown limit", input: layoutInput{outsideWidth: 4200, outsideHeight: 2400, deviceScale: 2}, wantWidth: 8400, wantHeight: 4800, wantScale: 2, wantUnit: 2},
		{name: "4200 by 2400 at 2 capped buffer", input: layoutInput{outsideWidth: 4200, outsideHeight: 2400, deviceScale: 2, imageLimit: headlessImageLimit, buffer: image.Pt(7524, 4409), canvas: image.Pt(8400, 4800)}, wantWidth: 7524, wantHeight: 4409, wantScale: 7524.0 / 4200, wantUnit: 7524.0 / 4200},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := newDisplayLayout(test.input)
			if got.width != test.wantWidth || got.height != test.wantHeight || !closeTo(got.deviceScale, test.wantScale) || !closeTo(got.unit, test.wantUnit) {
				t.Fatalf("layout = %dx%d scale %g unit %g, want %dx%d scale %g unit %g", got.width, got.height, got.deviceScale, got.unit, test.wantWidth, test.wantHeight, test.wantScale, test.wantUnit)
			}
			screen := image.Rect(0, 0, got.width, got.height)
			if got.right(1076) > float64(got.width) || got.bottom(700) > float64(got.height) {
				t.Errorf("anchored panel escaped layout: right=%g bottom=%g", got.right(1076), got.bottom(700))
			}
			if !got.mapViewport.In(screen) {
				t.Errorf("map viewport %v is not inside the screen %v", got.mapViewport, screen)
			}
		})
	}
}

// TestScreenSize checks the size of the screen image. When Chrome makes the
// drawing buffer smaller than the canvas, the screen image gets the size of
// the drawing buffer within the image limit, so that Ebitengine adds no
// letterbox. The drawing buffer sizes are from headless Chrome with
// SwiftShader, which has an 8192 pixel limit.
func TestScreenSize(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		input     screenSizeInput
		want      image.Point
		wantScale float64
	}{
		{
			name:  "uncapped",
			input: screenSizeInput{outside: image.Pt(3840, 2160), deviceScale: 2, limit: headlessImageLimit, buffer: image.Pt(7680, 4320), canvas: image.Pt(7680, 4320)},
			want:  image.Pt(7680, 4320), wantScale: 2,
		},
		{
			name:  "uncapped fractional scale",
			input: screenSizeInput{outside: image.Pt(1367, 769), deviceScale: 1.25, limit: headlessImageLimit, buffer: image.Pt(1708, 961), canvas: image.Pt(1708, 961)},
			want:  image.Pt(1709, 962), wantScale: 1.25,
		},
		{
			name:  "desktop above limit",
			input: screenSizeInput{outside: image.Pt(4200, 2400), deviceScale: 2, limit: headlessImageLimit},
			want:  image.Pt(8191, 4681), wantScale: 8191.0 / 4200,
		},
		{
			name:  "width capped only",
			input: screenSizeInput{outside: image.Pt(4200, 1000), deviceScale: 2, limit: headlessImageLimit, buffer: image.Pt(8192, 2000), canvas: image.Pt(8400, 2000)},
			want:  image.Pt(8191, 2000), wantScale: 8191.0 / 4200,
		},
		{
			name:  "height capped only",
			input: screenSizeInput{outside: image.Pt(1500, 4200), deviceScale: 2, limit: headlessImageLimit, buffer: image.Pt(3000, 8192), canvas: image.Pt(3000, 8400)},
			want:  image.Pt(3000, 8191), wantScale: 8191.0 / 4200,
		},
		{
			name:  "area capped only",
			input: screenSizeInput{outside: image.Pt(4000, 2400), deviceScale: 2, limit: headlessImageLimit, buffer: image.Pt(7436, 4461), canvas: image.Pt(8000, 4800)},
			want:  image.Pt(7436, 4461), wantScale: 4461.0 / 2400,
		},
		{
			name:  "width and area capped",
			input: screenSizeInput{outside: image.Pt(4200, 2400), deviceScale: 2, limit: headlessImageLimit, buffer: image.Pt(7524, 4409), canvas: image.Pt(8400, 4800)},
			want:  image.Pt(7524, 4409), wantScale: 7524.0 / 4200,
		},
		{
			name:  "unknown limit",
			input: screenSizeInput{outside: image.Pt(4200, 2400), deviceScale: 2, buffer: image.Pt(7524, 4409), canvas: image.Pt(8400, 4800)},
			want:  image.Pt(8400, 4800), wantScale: 2,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, scale := screenSize(test.input)
			if got != test.want || !closeTo(scale, test.wantScale) {
				t.Fatalf("screenSize(%+v) = %v, %g, want %v, %g", test.input, got, scale, test.want, test.wantScale)
			}
		})
	}
}

// TestMapLabelSize checks the font size of map labels and of other text in
// physical pixels. A map label keeps its CSS size in a window smaller than
// the minimum window, and it is at least 10 CSS pixels. Other text follows
// the display unit.
func TestMapLabelSize(t *testing.T) {
	t.Parallel()
	minimum := layoutInput{outsideWidth: 1100, outsideHeight: 728, deviceScale: 1}
	laptop := layoutInput{outsideWidth: 1366, outsideHeight: 610, deviceScale: 1}
	tests := []struct {
		name  string
		input layoutInput
		size  float64
		// want is the size of a map label. wantOther is the size of
		// other text.
		want, wantOther float64
	}{
		{name: "minimum window name", input: minimum, size: 16, want: 16, wantOther: 16},
		{name: "minimum window queue line", input: minimum, size: 9, want: 10, wantOther: 9},
		{name: "short laptop name", input: laptop, size: 16, want: 16, wantOther: 16 * 610.0 / 728},
		{name: "short laptop queue line", input: laptop, size: 9, want: 10, wantOther: 9 * 610.0 / 728},
		{name: "short laptop DPR2 pod label", input: layoutInput{outsideWidth: 1366, outsideHeight: 610, deviceScale: 2}, size: 11, want: 22, wantOther: 22 * 610.0 / 728},
		{name: "fractional DPR queue line", input: layoutInput{outsideWidth: 1100, outsideHeight: 728, deviceScale: 1.5}, size: 9, want: 15, wantOther: 13.5},
		{name: "below minimum", input: layoutInput{outsideWidth: 800, outsideHeight: 560, deviceScale: 1}, size: 10, want: 10, wantOther: 10 * 800.0 / 1100},
		{name: "invalid device scale", input: layoutInput{outsideWidth: 1366, outsideHeight: 610, deviceScale: math.NaN()}, size: 9, want: 10, wantOther: 9 * 610.0 / 728},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game := &Game{layout: newDisplayLayout(test.input)}
			if got := game.labelFace(label{size: test.size, mapLabel: true}).Size; math.Abs(got-test.want) > 1e-9 {
				t.Errorf("map label size = %g, want %g", got, test.want)
			}
			if got := game.labelFace(label{size: test.size}).Size; math.Abs(got-test.wantOther) > 1e-9 {
				t.Errorf("other text size = %g, want %g", got, test.wantOther)
			}
		})
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
	oldEdge := image.Pt(771, 487)
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
			Pod:    sim.Pod{Occupied: true, StationPhase: sim.ApproachingStation, ManeuverStationID: "station-01"},
			Riders: []sim.Request{{PartySize: 1}},
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
					// The fault marker adds the fault button to the
					// inspector.
					game.state.Simulation.FaultContract = sim.FaultV1Contract
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
			findButton(t, controls, "checkpoint")
			findButton(t, controls, "rewind")
			for _, header := range game.headerLabels() {
				bounds := game.labelArea(header)
				for _, control := range controls {
					if bounds.overlaps(buttonArea(control)) {
						t.Errorf("header %q %+v overlaps control %q %+v", header.value, bounds, control.action, buttonArea(control))
					}
				}
				if limit := game.layout.y(headerHeight); bounds.bottom > limit {
					t.Errorf("header %q ends at %g, the panels start at %g", header.value, bounds.bottom, limit)
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
	return area{left: g.layout.right(796), top: g.layout.y(headerHeight), right: g.layout.right(1076), bottom: g.layout.y(538) + g.layout.extraY}
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
		{name: "1080 high window", extra: 352, want: 14},
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
