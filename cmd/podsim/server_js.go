//go:build js && wasm

package main

import (
	"syscall/js"

	"github.com/dotwaffle/podsim/internal/view"
)

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
		top := window.Get("top").Get("location")
		readProperty(top, "reload").Call("call", top)
	}
}

// readProperty reads the property name of object. A JavaScript exception in
// Value.Get stops the Go program. Value.Call catches an exception, but after
// a failed call it reads the method again with Get. So readProperty reads
// the property with Reflect.get in a call. A SecurityError from an object of
// a different origin then becomes a js.Error panic.
func readProperty(object js.Value, name string) js.Value {
	return js.Global().Get("Reflect").Call("get", object, name)
}

// shellPage links the game to the shell page index.html. See view.Shell.
type shellPage struct {
	// parent is the window of the shell page, and origin is the origin of
	// this page and of the shell page.
	parent  js.Value
	origin  string
	notices chan view.ShellNotice
}

// shellLink returns the link to the shell page when game.html runs in a
// frame of it. Otherwise it returns nil. The shell page sets podsimShell to
// true. The browser blocks a read from a parent on a different origin, so
// that parent is not the shell page.
func shellLink() (link view.Shell) {
	window := js.Global()
	parent := window.Get("parent")
	if parent.Equal(window) {
		return nil
	}
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		if _, ok := recovered.(js.Error); !ok {
			panic(recovered)
		}
		link = nil
	}()
	if flag := readProperty(parent, "podsimShell"); flag.Type() != js.TypeBoolean || !flag.Bool() {
		return nil
	}
	shell := &shellPage{parent: parent, origin: window.Get("location").Get("origin").String(), notices: make(chan view.ShellNotice, 4)}
	// The func stays for the life of the page, so it is never released.
	window.Call("addEventListener", "message", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if notice, ok := shell.notice(args[0]); ok {
			select {
			case shell.notices <- notice:
			default:
			}
		}
		return nil
	}))
	return shell
}

// notice gives the status text of a message event from the shell page. The
// game accepts only a message from the shell page window with the origin
// of the shell page, with the data { podsim: "notice", text, error }. text
// is a string and error is a boolean.
func (s *shellPage) notice(event js.Value) (view.ShellNotice, bool) {
	if !event.Get("source").Equal(s.parent) || event.Get("origin").String() != s.origin {
		return view.ShellNotice{}, false
	}
	data := event.Get("data")
	if data.Type() != js.TypeObject {
		return view.ShellNotice{}, false
	}
	kind, text, failed := data.Get("podsim"), data.Get("text"), data.Get("error")
	if kind.Type() != js.TypeString || kind.String() != "notice" || text.Type() != js.TypeString || failed.Type() != js.TypeBoolean {
		return view.ShellNotice{}, false
	}
	return view.ShellNotice{Text: text.String(), Error: failed.Bool()}, true
}

// Send posts request to the shell page. See showRequest and debugRequest in
// web/shell.js.
func (s *shellPage) Send(request view.ShellRequest) {
	var message map[string]any
	switch request {
	case view.ShowEditor:
		message = map[string]any{"podsim": "show", "view": "editor", "keyboard": false}
	case view.CaptureDebugState:
		message = map[string]any{"podsim": "debug"}
	default:
		return
	}
	s.parent.Call("postMessage", message, s.origin)
}

// Notices gives the status texts from the shell page.
func (s *shellPage) Notices() <-chan view.ShellNotice { return s.notices }
