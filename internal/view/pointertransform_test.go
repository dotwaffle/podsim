package view

import (
	"image"
	"math"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

// TestBufferRatio checks the factor on one axis. Only a drawing buffer side
// smaller than the canvas side gives a factor below 1. Headless Chrome gives
// a 7436 by 4461 drawing buffer to the 8000 by 4800 canvas of a 4000 by 2400
// window at a device pixel ratio of 2.
func TestBufferRatio(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		buffer, canvas int
		want           float64
	}{
		{name: "full side", buffer: 8000, canvas: 8000, want: 1},
		{name: "area limit width", buffer: 7436, canvas: 8000, want: 7436.0 / 8000},
		{name: "area limit height", buffer: 4461, canvas: 4800, want: 4461.0 / 4800},
		{name: "side limit", buffer: 8192, canvas: 8400, want: 8192.0 / 8400},
		{name: "larger buffer", buffer: 8001, canvas: 8000, want: 1},
		{name: "no buffer", buffer: 0, canvas: 8000, want: 1},
		{name: "before start", buffer: 0, canvas: 0, want: 1},
		{name: "no canvas side", buffer: 300, canvas: 0, want: 1},
		{name: "negative buffer", buffer: -1, canvas: 8000, want: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := bufferRatio(test.buffer, test.canvas); got != test.want {
				t.Errorf("bufferRatio(%d, %d) = %g, want %g", test.buffer, test.canvas, got, test.want)
			}
		})
	}
}

// TestNewPointerTransform checks the factor and the letterbox correction on
// each axis. The sizes give exact results. The x and y factors are
// different, so that an exchange of the axes fails.
func TestNewPointerTransform(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input pointerTransformInput
		want  pointerTransform
	}{
		{name: "desktop", input: pointerTransformInput{screen: image.Pt(1600, 1000)}, want: identityPointer},
		{name: "full buffer", input: pointerTransformInput{buffer: image.Pt(1600, 1000), canvas: image.Pt(1600, 1000), screen: image.Pt(1600, 1000)}, want: identityPointer},
		{name: "full buffer with letterbox", input: pointerTransformInput{buffer: image.Pt(8400, 4800), canvas: image.Pt(8400, 4800), screen: image.Pt(8191, 4681)}, want: identityPointer},
		{name: "no screen", input: pointerTransformInput{buffer: image.Pt(400, 300), canvas: image.Pt(800, 400)}, want: identityPointer},
		{name: "no buffer width", input: pointerTransformInput{buffer: image.Pt(0, 300), canvas: image.Pt(800, 400), screen: image.Pt(800, 400)}, want: identityPointer},
		{name: "width limit", input: pointerTransformInput{buffer: image.Pt(600, 400), canvas: image.Pt(800, 400), screen: image.Pt(800, 400)}, want: pointerTransform{scale: sim.Point{X: 0.75, Y: 1}}},
		{name: "letterbox on y", input: pointerTransformInput{buffer: image.Pt(400, 300), canvas: image.Pt(800, 400), screen: image.Pt(800, 400)}, want: pointerTransform{scale: sim.Point{X: 0.5, Y: 0.75}, offset: sim.Point{Y: -25}}},
		{name: "letterbox on x", input: pointerTransformInput{buffer: image.Pt(300, 400), canvas: image.Pt(400, 800), screen: image.Pt(400, 800)}, want: pointerTransform{scale: sim.Point{X: 0.75, Y: 0.5}, offset: sim.Point{X: -25}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := newPointerTransform(test.input); got != test.want {
				t.Errorf("newPointerTransform(%+v) = %+v, want %+v", test.input, got, test.want)
			}
		})
	}
}

// TestPointerTransformApply checks that apply multiplies each axis by its
// factor and then adds the offset, and that identityPointer keeps the point
// exactly.
func TestPointerTransformApply(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		point     sim.Point
		transform pointerTransform
		want      sim.Point
	}{
		{name: "identity", point: sim.Point{X: 1234.5, Y: 678.25}, transform: identityPointer, want: sim.Point{X: 1234.5, Y: 678.25}},
		{name: "scale", point: sim.Point{X: 8000, Y: 4800}, transform: pointerTransform{scale: sim.Point{X: 0.5, Y: 0.25}}, want: sim.Point{X: 4000, Y: 1200}},
		{name: "scale and offset", point: sim.Point{X: 8000, Y: 4800}, transform: pointerTransform{scale: sim.Point{X: 0.5, Y: 0.25}, offset: sim.Point{X: -25, Y: 10}}, want: sim.Point{X: 3975, Y: 1210}},
		{name: "origin", point: sim.Point{}, transform: pointerTransform{scale: sim.Point{X: 0.9, Y: 0.9}, offset: sim.Point{Y: -3}}, want: sim.Point{Y: -3}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := test.transform.apply(test.point); got != test.want {
				t.Errorf("%+v.apply(%+v) = %+v, want %+v", test.transform, test.point, got, test.want)
			}
		})
	}
}

// TestPointerTransformHitsButton checks a click where the center of the
// Speed button shows in the browser window. The drawing buffer sizes are
// those of headless Chrome, which has an 8192 pixel limit on a side and a
// limit of about 33.2 million pixels. With a smaller drawing buffer, the
// cursor position from Ebitengine misses the button. The transform moves it
// back to less than one pixel from the center of the button. In the 4200 and
// 5000 pixel wide windows, Chrome limits the width and then the area, and
// the letterbox correction is necessary.
func TestPointerTransformHitsButton(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		window      image.Point
		deviceScale float64
		buffer      image.Point
	}{
		{name: "full buffer", window: image.Pt(1600, 1000), deviceScale: 1, buffer: image.Pt(1600, 1000)},
		{name: "full buffer at 2", window: image.Pt(2560, 1440), deviceScale: 2, buffer: image.Pt(5120, 2880)},
		{name: "area limit", window: image.Pt(4000, 2400), deviceScale: 2, buffer: image.Pt(7436, 4461)},
		{name: "side limit", window: image.Pt(4200, 1500), deviceScale: 2, buffer: image.Pt(8192, 3000)},
		{name: "side and area limit", window: image.Pt(4200, 2400), deviceScale: 2, buffer: image.Pt(7524, 4409)},
		{name: "wide side and area limit", window: image.Pt(5000, 2400), deviceScale: 2, buffer: image.Pt(7524, 4409)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game := &Game{}
			game.layoutFor(layoutInput{outsideWidth: test.window.X, outsideHeight: test.window.Y, deviceScale: test.deviceScale, imageLimit: headlessImageLimit})
			speed := findButton(t, game.buttons(), "speed")
			target := centerOfButton(speed)
			screen := image.Pt(game.layout.width, game.layout.height)
			browser := ebitenBrowser{window: test.window, deviceScale: test.deviceScale, buffer: test.buffer, screen: screen}
			cursor := browser.cursor(browser.client(target))
			// Ebitengine makes the canvas the window size times the
			// device scale.
			canvas := image.Pt(int(float64(test.window.X)*test.deviceScale), int(float64(test.window.Y)*test.deviceScale))
			pointer := newPointerTransform(pointerTransformInput{buffer: test.buffer, canvas: canvas, screen: screen})
			got := pointer.apply(cursor)
			if math.Abs(got.X-target.X) > 1 || math.Abs(got.Y-target.Y) > 1 || !onButton(got, speed) {
				t.Errorf("corrected cursor %+v is not within 1 pixel of the Speed button center %+v", got, target)
			}
			if full := test.buffer == canvas; onButton(cursor, speed) != full {
				t.Errorf("cursor %+v on the Speed button %+v = %t, want %t", cursor, speed, !full, full)
			}
		})
	}
}

// ebitenBrowser is a model of Ebitengine 2.10 in the browser. Ebitengine
// draws the screen image into the drawing buffer at the largest scale that
// fits, in the center. The browser then stretches the drawing buffer to the
// window. Ebitengine maps a click to the screen image with the device scale
// and not with the drawing buffer size.
type ebitenBrowser struct {
	// window is the size of the window and the canvas in CSS pixels.
	window      image.Point
	deviceScale float64
	// buffer is the size of the drawing buffer in pixels.
	buffer image.Point
	// screen is the size of the screen image from Layout in pixels.
	screen image.Point
}

// letterbox returns the scale and the offset of the screen image in the
// drawing buffer.
func (b ebitenBrowser) letterbox() (scale float64, offset sim.Point) {
	scale = min(float64(b.buffer.X)/float64(b.screen.X), float64(b.buffer.Y)/float64(b.screen.Y))
	offset = sim.Point{X: (float64(b.buffer.X) - float64(b.screen.X)*scale) / 2, Y: (float64(b.buffer.Y) - float64(b.screen.Y)*scale) / 2}
	return scale, offset
}

// client returns the point in CSS pixels where point on the screen image
// shows in the window.
func (b ebitenBrowser) client(point sim.Point) sim.Point {
	scale, offset := b.letterbox()
	return sim.Point{
		X: (point.X*scale + offset.X) * float64(b.window.X) / float64(b.buffer.X),
		Y: (point.Y*scale + offset.Y) * float64(b.window.Y) / float64(b.buffer.Y),
	}
}

// cursor returns the cursor position that Ebitengine reports for a click at
// client in CSS pixels. CursorPosition truncates the position to whole
// pixels.
func (b ebitenBrowser) cursor(client sim.Point) sim.Point {
	scale, offset := b.letterbox()
	return sim.Point{
		X: math.Trunc((client.X*b.deviceScale - offset.X) / scale),
		Y: math.Trunc((client.Y*b.deviceScale - offset.Y) / scale),
	}
}

// onButton reports whether point is on the button, with the rule of click.
func onButton(point sim.Point, control button) bool {
	return point.X >= control.x && point.X < control.x+control.w && point.Y >= control.y && point.Y < control.y+control.h
}
