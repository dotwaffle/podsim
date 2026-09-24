//go:build js && wasm

package view

import "syscall/js"

// pointerShift reports whether Shift was pressed at the last pointer event.
// game.html keeps this state, because Ebitengine reads Shift only from key
// events on its canvas.
func pointerShift() bool {
	value := js.Global().Get("podsimPointerShift")
	return value.Type() == js.TypeBoolean && value.Bool()
}
