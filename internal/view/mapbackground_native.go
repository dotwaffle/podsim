//go:build !js

package view

type mapPublisher struct{}

// publish leaves the native viewport opaque and does not request tiles.
func (*mapPublisher) publish(mapView) bool { return false }
