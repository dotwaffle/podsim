//go:build js && wasm

package main

import "syscall/js"

func serverURL() string { return js.Global().Get("location").Get("origin").String() }
