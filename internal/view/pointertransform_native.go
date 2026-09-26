//go:build !js

package view

import "image"

// readDrawingBuffer returns the size of the WebGL drawing buffer and the
// size of the canvas, in pixels. See newPointerTransform. Only the browser
// build has a canvas. The desktop build returns 0 for both sizes, so it
// does not change the positions.
func readDrawingBuffer() (buffer, canvas image.Point) { return image.Point{}, image.Point{} }
