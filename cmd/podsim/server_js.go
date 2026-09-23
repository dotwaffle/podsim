//go:build js && wasm

package main

import "syscall/js"

func serverURL() string { return js.Global().Get("location").Get("origin").String() }

// pageReloader returns a func that loads the page again. game.html runs in an
// iframe of index.html, so the func reloads the top page. When the top page
// has a different origin, the browser blocks this with a SecurityError. Then
// the func reloads this frame only.
func pageReloader() func() {
	return func() {
		window := js.Global()
		defer func() {
			recovered := recover()
			if recovered == nil {
				return
			}
			if _, ok := recovered.(js.Error); !ok {
				panic(recovered)
			}
			window.Get("location").Call("reload")
		}()
		// A JavaScript exception in Value.Get stops the Go program.
		// Value.Call catches an exception, but after a failed call it reads
		// the method again with Get. So read the reload method with
		// Reflect.get in a call. A SecurityError then becomes a js.Error
		// panic.
		top := window.Get("top").Get("location")
		reload := window.Get("Reflect").Call("get", top, "reload")
		reload.Call("call", top)
	}
}
