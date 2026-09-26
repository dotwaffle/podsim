//go:build js && wasm

package view

import (
	"image"
	"syscall/js"
)

// gameCanvas holds the canvas of Ebitengine and its WebGL context after the
// first call of readDrawingBuffer that finds them. Only the game loop uses
// it.
var gameCanvas struct {
	canvas, context js.Value
}

// readDrawingBuffer returns the size of the WebGL drawing buffer and the
// size of the canvas, in pixels. See newPointerTransform and screenSize.
// Call it only from Update and LayoutF, which run in the game loop.
// Ebitengine adds the only canvas of game.html and makes its WebGL 2 context
// before the game loop starts. Thus getContext returns the context of
// Ebitengine and does not make a new one. When the canvas or its context is
// not found, both sizes are 0.
func readDrawingBuffer() (buffer, canvas image.Point) {
	if !gameCanvas.context.Truthy() {
		element := js.Global().Get("document").Call("querySelector", "canvas")
		if !element.Truthy() {
			return image.Point{}, image.Point{}
		}
		context := element.Call("getContext", "webgl2")
		if !context.Truthy() {
			return image.Point{}, image.Point{}
		}
		gameCanvas.canvas, gameCanvas.context = element, context
	}
	element, context := gameCanvas.canvas, gameCanvas.context
	buffer = image.Pt(context.Get("drawingBufferWidth").Int(), context.Get("drawingBufferHeight").Int())
	canvas = image.Pt(element.Get("width").Int(), element.Get("height").Int())
	return buffer, canvas
}
