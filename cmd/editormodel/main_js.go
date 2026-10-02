//go:build js && wasm

// Command editormodel runs the Go project model in an editor worker.
package main

import (
	"syscall/js"

	"github.com/dotwaffle/podsim/internal/editormodel"
)

func main() {
	call := js.FuncOf(func(_ js.Value, args []js.Value) (result any) {
		defer func() {
			if recover() != nil {
				result = `{"fatal":"The editor model operation failed."}`
			}
		}()
		if len(args) != 1 || args[0].Type() != js.TypeString {
			return `{"error":"The editor model needs one JSON request."}`
		}
		return editormodel.Call(args[0].String())
	})
	defer call.Release()
	js.Global().Set("podsimEditorCall", call)
	select {}
}
