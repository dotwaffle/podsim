//go:build !js

package view

// touchCanceled reports whether the browser canceled the touches after the
// last call.
// Only the browser build gets this signal.
// The desktop build gets no touches.
func touchCanceled() bool { return false }
