//go:build js && wasm

package view

import (
	"sync/atomic"
	"syscall/js"
)

// touchCancel is true when the browser canceled the touches after the last
// call of touchCanceled.
var touchCancel atomic.Bool

// init listens for the browser events that stop the touches without a
// touchend event.
// Ebitengine reads touchstart, touchmove, and touchend, but not touchcancel.
// Thus it keeps a canceled touch until the next touch event on the canvas.
// A hidden page can also stop the touches.
func init() {
	window := js.Global()
	document := window.Get("document")
	window.Call("addEventListener", "touchcancel", js.FuncOf(func(js.Value, []js.Value) any {
		touchCancel.Store(true)
		return nil
	}), true)
	document.Call("addEventListener", "visibilitychange", js.FuncOf(func(js.Value, []js.Value) any {
		if document.Get("hidden").Bool() {
			touchCancel.Store(true)
		}
		return nil
	}))
}

// touchCanceled reports whether the browser canceled the touches after the
// last call.
func touchCanceled() bool { return touchCancel.Swap(false) }
