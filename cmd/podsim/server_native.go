//go:build !js

package main

import "flag"

func serverURL() string {
	address := flag.String("server", "http://127.0.0.1:8080", "Shared session server URL")
	flag.Parse()
	return *address
}

// pageReloader returns nil. The desktop client cannot load itself again, so
// the game shows a message when the server build changes.
func pageReloader() func() { return nil }
