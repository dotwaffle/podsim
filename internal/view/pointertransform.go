package view

import (
	"image"

	"github.com/dotwaffle/podsim/internal/sim"
)

// pointerTransform corrects the cursor and touch positions from Ebitengine
// in the browser. The browser can make the WebGL drawing buffer smaller than
// the canvas. For example, Chrome keeps each side of the drawing buffer
// within the GPU limit, and the drawing buffer within about 33.2 million
// pixels. Ebitengine 2.10 then draws the screen image into the smaller
// drawing buffer, but it maps the positions to the screen image with the
// full canvas size. The positions then go past the controls.
//
// Ebitengine draws the screen image at the center of the drawing buffer, at
// the largest scale that fits. When the drawing buffer does not have the
// proportions of the screen image, Ebitengine adds a letterbox offset.
// LayoutF gives the screen image the proportions of a smaller drawing
// buffer, so this offset is less than one pixel. See screenSize. The
// transform multiplies each position by the drawing buffer size divided by
// the canvas size on each axis. Then it adds the correction for the
// letterbox offset. See
// newPointerTransform. The result is less than one pixel from the true
// position, because Ebitengine truncates the positions to whole pixels.
//
// Ebitengine can also move the screen image up for a virtual keyboard. The
// view has no text field, so that shift is always 0.
type pointerTransform struct {
	// scale is the factor on each axis.
	scale sim.Point
	// offset is the value in screen image pixels that apply adds after the
	// scale.
	offset sim.Point
}

// identityPointer is the transform of a drawing buffer that is as large as
// the canvas. It does not change the positions.
var identityPointer = pointerTransform{scale: sim.Point{X: 1, Y: 1}}

// pointerTransformInput holds the sizes in pixels for newPointerTransform.
type pointerTransformInput struct {
	// buffer is the size of the WebGL drawing buffer. Ebitengine uses it as
	// the size of its screen.
	buffer image.Point
	// canvas is the size of the canvas. Ebitengine sets it to the CSS size
	// times the device scale.
	canvas image.Point
	// screen is the size of the screen image that Layout returns.
	screen image.Point
}

// newPointerTransform returns the transform for the sizes in input. It
// returns identityPointer when the drawing buffer is as large as the canvas,
// or when a size is not known, for example in the desktop build.
//
// For a client position p in CSS pixels, Ebitengine reports the position
// c = (p*d - o) / s. d is the device scale, s is the scale of the screen
// image in the drawing buffer, and o is its letterbox offset. The browser
// stretches the drawing buffer to the window, so the true position is
// (p*d*r - o) / s, where r is the drawing buffer size divided by the canvas
// size. That is c*r + o*(r-1)/s. On an axis where r is 1, the offset term is
// 0.
func newPointerTransform(input pointerTransformInput) pointerTransform {
	ratio := sim.Point{X: bufferRatio(input.buffer.X, input.canvas.X), Y: bufferRatio(input.buffer.Y, input.canvas.Y)}
	if ratio == identityPointer.scale || input.buffer.X <= 0 || input.buffer.Y <= 0 || input.screen.X <= 0 || input.screen.Y <= 0 {
		return identityPointer
	}
	buffer := sim.Point{X: float64(input.buffer.X), Y: float64(input.buffer.Y)}
	screen := sim.Point{X: float64(input.screen.X), Y: float64(input.screen.Y)}
	scale := min(buffer.X/screen.X, buffer.Y/screen.Y)
	letterbox := sim.Point{X: (buffer.X - screen.X*scale) / 2, Y: (buffer.Y - screen.Y*scale) / 2}
	return pointerTransform{
		scale:  ratio,
		offset: sim.Point{X: letterbox.X * (ratio.X - 1) / scale, Y: letterbox.Y * (ratio.Y - 1) / scale},
	}
}

// bufferRatio returns the factor on one axis for a drawing buffer side of
// buffer pixels and a canvas side of canvas pixels. The factor is buffer
// divided by canvas when the drawing buffer is smaller than the canvas. In
// all other cases it is 1, for example before the game starts, when both
// sides are 0.
func bufferRatio(buffer, canvas int) float64 {
	if buffer <= 0 || buffer >= canvas {
		return 1
	}
	return float64(buffer) / float64(canvas)
}

// apply returns point corrected by the transform. With identityPointer, it
// returns point.
func (t pointerTransform) apply(point sim.Point) sim.Point {
	return sim.Point{X: point.X*t.scale.X + t.offset.X, Y: point.Y*t.scale.Y + t.offset.Y}
}
