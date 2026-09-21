package view

import (
	"image"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

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

func pointFromImage(point image.Point) sim.Point {
	return sim.Point{X: float64(point.X), Y: float64(point.Y)}
}
