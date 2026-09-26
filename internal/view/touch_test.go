package view

import (
	"image"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/dotwaffle/podsim/internal/sim"
)

// touchAt returns a touch with id at x, y.
func touchAt(id ebiten.TouchID, x, y float64) touchPoint {
	return touchPoint{id: id, point: sim.Point{X: x, Y: y}}
}

// TestTouchGestures sends touch frames to the gestures and checks the
// actions of each frame. The map is the square from 0, 0 to 100, 100, and a
// touch can move less than 10 pixels and still be a tap.
func TestTouchGestures(t *testing.T) {
	t.Parallel()
	type step struct {
		touches  []touchPoint
		canceled bool
		want     touchActions
	}
	tests := []struct {
		name  string
		steps []step
	}{
		{name: "tap on the map", steps: []step{
			{touches: []touchPoint{touchAt(1, 50, 50)}, want: touchActions{mapPressed: true}},
			{touches: []touchPoint{touchAt(1, 53, 52)}},
			{want: touchActions{tap: true, tapPoint: sim.Point{X: 53, Y: 52}}},
		}},
		{name: "tap on the panel", steps: []step{
			{touches: []touchPoint{touchAt(1, 150, 50)}},
			{want: touchActions{tap: true, tapPoint: sim.Point{X: 150, Y: 50}}},
		}},
		{name: "one finger pans after the tap distance", steps: []step{
			{touches: []touchPoint{touchAt(1, 50, 50)}, want: touchActions{mapPressed: true}},
			{touches: []touchPoint{touchAt(1, 55, 50)}},
			{touches: []touchPoint{touchAt(1, 65, 50)}, want: touchActions{pan: sim.Point{X: 10}}},
			{touches: []touchPoint{touchAt(1, 70, 55)}, want: touchActions{pan: sim.Point{X: 5, Y: 5}}},
			{touches: []touchPoint{touchAt(1, 62, 55)}, want: touchActions{pan: sim.Point{X: -8}}},
			{},
		}},
		{name: "drag from the panel does nothing", steps: []step{
			{touches: []touchPoint{touchAt(1, 150, 50)}},
			{touches: []touchPoint{touchAt(1, 120, 50)}},
			{touches: []touchPoint{touchAt(1, 60, 50)}},
			{},
		}},
		{name: "pinch zooms and pans, then one finger pans", steps: []step{
			{touches: []touchPoint{touchAt(1, 40, 50)}, want: touchActions{mapPressed: true}},
			{touches: []touchPoint{touchAt(1, 40, 50), touchAt(2, 60, 50)}, want: touchActions{mapPressed: true}},
			{
				touches: []touchPoint{touchAt(1, 30, 50), touchAt(2, 70, 50)},
				want:    touchActions{zoom: 2, zoomCenter: sim.Point{X: 50, Y: 50}},
			},
			{
				touches: []touchPoint{touchAt(1, 35, 60), touchAt(2, 75, 60)},
				want:    touchActions{pan: sim.Point{X: 5, Y: 10}, zoom: 1, zoomCenter: sim.Point{X: 55, Y: 60}},
			},
			{touches: []touchPoint{touchAt(2, 75, 60)}},
			{touches: []touchPoint{touchAt(2, 78, 62)}, want: touchActions{pan: sim.Point{X: 3, Y: 2}}},
			{},
		}},
		{name: "two touches at one point do not zoom", steps: []step{
			{touches: []touchPoint{touchAt(1, 50, 50), touchAt(2, 50, 50)}, want: touchActions{mapPressed: true}},
			{touches: []touchPoint{touchAt(1, 40, 50), touchAt(2, 60, 50)}, want: touchActions{zoomCenter: sim.Point{X: 50, Y: 50}}},
			{},
		}},
		{name: "two short touches are not a tap", steps: []step{
			{touches: []touchPoint{touchAt(1, 50, 50)}, want: touchActions{mapPressed: true}},
			{touches: []touchPoint{touchAt(1, 50, 50), touchAt(2, 150, 50)}},
			{touches: []touchPoint{touchAt(2, 150, 50)}},
			{},
		}},
		{name: "a panel touch does not zoom", steps: []step{
			{touches: []touchPoint{touchAt(1, 50, 50), touchAt(2, 150, 50)}, want: touchActions{mapPressed: true}},
			{touches: []touchPoint{touchAt(1, 52, 50), touchAt(2, 150, 80)}, want: touchActions{pan: sim.Point{X: 2}}},
		}},
		// Ebitengine keeps a canceled touch until the next touch event. The
		// next touch then replaces it in the same frame.
		{name: "a canceled touch is not a tap", steps: []step{
			{touches: []touchPoint{touchAt(1, 150, 50)}},
			{touches: []touchPoint{touchAt(1, 150, 50)}, canceled: true},
			{touches: []touchPoint{touchAt(1, 150, 50)}},
			{touches: []touchPoint{touchAt(2, 50, 50)}, want: touchActions{mapPressed: true}},
			{want: touchActions{tap: true, tapPoint: sim.Point{X: 50, Y: 50}}},
		}},
		{name: "a touch that ends with the cancel is not a tap", steps: []step{
			{touches: []touchPoint{touchAt(1, 150, 50)}},
			{canceled: true},
			{},
		}},
		{name: "a touch that starts with the cancel is not a tap", steps: []step{
			{touches: []touchPoint{touchAt(1, 150, 50)}, canceled: true},
			{},
		}},
		{name: "a canceled touch at the same point is ignored", steps: []step{
			{touches: []touchPoint{touchAt(0, 50, 50)}, want: touchActions{mapPressed: true}},
			{touches: []touchPoint{touchAt(0, 50, 50)}, canceled: true},
			{touches: []touchPoint{touchAt(0, 50, 50)}},
			{touches: []touchPoint{touchAt(0, 50, 50)}},
			{},
		}},
		// Android often gives each new first finger ID 0.
		{name: "a new touch with the ID of a canceled touch is a tap", steps: []step{
			{touches: []touchPoint{touchAt(0, 150, 50)}},
			{touches: []touchPoint{touchAt(0, 150, 50)}, canceled: true},
			{touches: []touchPoint{touchAt(0, 150, 50)}},
			{touches: []touchPoint{touchAt(0, 50, 50)}, want: touchActions{mapPressed: true}},
			{want: touchActions{tap: true, tapPoint: sim.Point{X: 50, Y: 50}}},
		}},
		{name: "a new touch at the point of an ended canceled touch is a tap", steps: []step{
			{touches: []touchPoint{touchAt(0, 150, 50)}},
			{touches: []touchPoint{touchAt(0, 150, 50)}, canceled: true},
			{},
			{touches: []touchPoint{touchAt(0, 150, 50)}},
			{want: touchActions{tap: true, tapPoint: sim.Point{X: 150, Y: 50}}},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var gestures touchGestures
			for index, step := range test.steps {
				got := gestures.update(touchFrame{touches: step.touches, mapArea: image.Rect(0, 0, 100, 100), tapSlop: 10, canceled: step.canceled})
				if got != step.want {
					t.Errorf("step %d: actions = %+v, want %+v", index, got, step.want)
				}
			}
		})
	}
}

// touchGame sends touch frames to the gestures of game and applies the
// actions.
func touchGame(game *Game, frames [][]touchPoint, shift bool) {
	for _, touches := range frames {
		actions := game.touch.update(touchFrame{touches: touches, mapArea: game.camera.viewport, tapSlop: touchTapSlop * game.layout.unit})
		game.applyTouch(actions, shift)
	}
}

// TestTouchInput checks that a tap acts as a left click on buttons, station
// chips, and the map, and that touches on the map pan and zoom the camera.
func TestTouchInput(t *testing.T) {
	t.Parallel()
	tap := func(point sim.Point) [][]touchPoint {
		return [][]touchPoint{{touchPoint{id: 1, point: point}}, nil}
	}
	tests := []struct {
		name string
		run  func(*testing.T, *Game)
	}{
		{name: "tap on a button", run: func(t *testing.T, game *Game) {
			t.Helper()
			touchGame(game, tap(centerOfButton(findButton(t, game.buttons(), "stations-next"))), false)
			if game.stationPage != 1 {
				t.Errorf("station page = %d, want 1", game.stationPage)
			}
		}},
		{name: "tap on a station chip", run: func(t *testing.T, game *Game) {
			t.Helper()
			want := game.stationPages()[0][2].station.ID
			touchGame(game, tap(centerOfButton(findButton(t, game.buttons(), "to/"+want))), false)
			if game.destination != want {
				t.Errorf("To = %q, want %q", game.destination, want)
			}
		}},
		{name: "tap on a map station", run: func(t *testing.T, game *Game) {
			t.Helper()
			pages := game.stationPages()
			target := pages[len(pages)-1][0].station.ID
			touchGame(game, tap(game.mapPoint(game.stationAnchors()[target])), false)
			if game.origin != target || game.stationPage != len(pages)-1 {
				t.Errorf("From %q, station page %d, want %q, %d", game.origin, game.stationPage, target, len(pages)-1)
			}
		}},
		{name: "tap on a map station with Shift", run: func(t *testing.T, game *Game) {
			t.Helper()
			pages := game.stationPages()
			target := pages[len(pages)-1][0].station.ID
			touchGame(game, tap(game.mapPoint(game.stationAnchors()[target])), true)
			if game.destination != target {
				t.Errorf("To = %q, want %q", game.destination, target)
			}
		}},
		{name: "touch on the map stops follow", run: func(t *testing.T, game *Game) {
			t.Helper()
			game.followSelected = true
			center := game.camera.viewport.Min.Add(game.camera.viewport.Max).Div(2)
			touchGame(game, [][]touchPoint{{touchAt(1, float64(center.X), float64(center.Y))}}, false)
			if game.followSelected {
				t.Error("touch on the map kept follow")
			}
		}},
		{name: "one finger pans the map", run: func(t *testing.T, game *Game) {
			t.Helper()
			game.camera.zoomAt(sim.Point{X: 400, Y: 300}, 4)
			game.syncCamera()
			before := game.mapOrigin
			touchGame(game, [][]touchPoint{
				{touchAt(1, 400, 300)}, {touchAt(1, 405, 300)}, {touchAt(1, 430, 300)}, {touchAt(1, 450, 300)}, nil,
			}, false)
			if want := (sim.Point{X: before.X + 45, Y: before.Y}); !closePoint(game.mapOrigin, want) {
				t.Errorf("map origin = %+v, want %+v", game.mapOrigin, want)
			}
		}},
		{name: "drag from the panel keeps the map", run: func(t *testing.T, game *Game) {
			t.Helper()
			game.camera.zoomAt(sim.Point{X: 400, Y: 300}, 4)
			game.syncCamera()
			before, x := game.mapOrigin, float64(game.camera.viewport.Max.X+40)
			touchGame(game, [][]touchPoint{
				{touchAt(1, x, 300)}, {touchAt(1, x-200, 300)}, {touchAt(1, x-300, 320)}, nil,
			}, false)
			if game.mapOrigin != before {
				t.Errorf("map origin = %+v, want %+v", game.mapOrigin, before)
			}
		}},
		{name: "pinch zooms around the midpoint", run: func(t *testing.T, game *Game) {
			t.Helper()
			scale, world := game.camera.scale, game.camera.worldPoint(sim.Point{X: 400, Y: 300})
			touchGame(game, [][]touchPoint{
				{touchAt(1, 380, 300), touchAt(2, 420, 300)},
				{touchAt(1, 360, 300), touchAt(2, 440, 300)},
			}, false)
			if game.camera.scale != 2*scale {
				t.Errorf("scale = %v, want %v", game.camera.scale, 2*scale)
			}
			if got := game.mapPoint(world); !closePoint(got, sim.Point{X: 400, Y: 300}) {
				t.Errorf("world point under the midpoint moved to %+v", got)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			test.run(t, londonPickGame(t))
		})
	}
}

// TestCanceledTouchOnRewind checks that a touch on Rewind that the browser
// cancels does not rewind the shared session when the next touch starts.
// Without the cancel, the same touches rewind.
func TestCanceledTouchOnRewind(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		canceled   bool
		wantRewind bool
	}{
		{name: "canceled", canceled: true},
		{name: "not canceled", wantRewind: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game := sharedTestGame(t)
			clickCommand(t, game, "checkpoint")
			syncGame(t, game, func() bool { return len(game.state.Checkpoints) == 1 })
			rewind := touchPoint{id: 1, point: centerOfButton(findButton(t, game.buttons(), "rewind"))}
			center := game.camera.viewport.Min.Add(game.camera.viewport.Max).Div(2)
			frames := []touchFrame{
				{touches: []touchPoint{rewind}},
				{touches: []touchPoint{rewind}, canceled: test.canceled},
				{touches: []touchPoint{touchAt(2, float64(center.X), float64(center.Y))}},
			}
			for _, frame := range frames {
				frame.mapArea, frame.tapSlop = game.camera.viewport, touchTapSlop*game.layout.unit
				game.applyTouch(game.touch.update(frame), false)
			}
			if game.pending != test.wantRewind {
				t.Fatalf("command sent = %t, want %t", game.pending, test.wantRewind)
			}
			if !test.wantRewind {
				return
			}
			if result := commandResult(t, game); result.Command.Action != "rewind" {
				t.Fatalf("sent command %q, want rewind", result.Command.Action)
			}
		})
	}
}
