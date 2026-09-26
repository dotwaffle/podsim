//go:build !js

package view

import "image"

// readDrawingBuffer returns the size of the WebGL drawing buffer and the
// size of the canvas, in pixels. See newPointerTransform and screenSize.
// Only the browser build has a canvas. The desktop build returns 0 for both
// sizes, so it does not change the positions or the screen size.
func readDrawingBuffer() (buffer, canvas image.Point) { return image.Point{}, image.Point{} }
