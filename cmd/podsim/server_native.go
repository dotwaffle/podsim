//go:build !js

package main

import (
	"flag"

	"github.com/dotwaffle/podsim/internal/view"
)

func serverURL() string {
	address := flag.String("server", "http://127.0.0.1:8080", "Shared session server URL")
	flag.Parse()
	return *address
}

// pageReloader returns nil. The desktop client cannot load itself again, so
// the game shows a message when the server build changes.
func pageReloader(view.Shell) func() { return nil }

// shellLink returns nil. The desktop client has no shell page, so the header
// does not show Edit scenario and Download debug state.
func shellLink() view.Shell { return nil }
