//go:build !js

package view

// pointerShift reports whether Shift was pressed at the last pointer event.
// Only the browser build keeps this state. The desktop build reads Shift
// from the keyboard.
func pointerShift() bool { return false }
