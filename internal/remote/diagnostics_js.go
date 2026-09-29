//go:build js && wasm

package remote

import "syscall/js"

// streamDiagnostic reports local measurements without changing the wire format.
func streamDiagnostic(event string, values ...any) {
	hook := js.Global().Get("podsimStreamDiagnostic")
	if hook.Type() != js.TypeFunction {
		return
	}
	args := append([]any{event}, values...)
	hook.Invoke(args...)
}
