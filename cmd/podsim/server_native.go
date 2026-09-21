//go:build !js

package main

import "flag"

func serverURL() string {
	address := flag.String("server", "http://127.0.0.1:8080", "Shared session server URL")
	flag.Parse()
	return *address
}
