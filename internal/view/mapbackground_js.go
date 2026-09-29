//go:build js && wasm

package view

import (
	"strconv"
	"syscall/js"
)

// mapPublisher reuses its JavaScript objects and only publishes a changed
// camera or source. The local toggle is read on every draw.
type mapPublisher struct {
	last                                 mapView
	sent                                 bool
	refresh                              int
	value, geo, background, source, clip js.Value
}

func (p *mapPublisher) publish(view mapView) bool {
	hook := js.Global().Get("podsimMapView")
	if hook.Type() != js.TypeFunction {
		return false
	}
	refresh := js.Global().Get("podsimMapRefresh")
	requested := 0
	if refresh.Type() == js.TypeNumber {
		requested = refresh.Int()
	}
	if !p.sent || p.last != view || p.refresh != requested {
		if !p.sent {
			object := js.Global().Get("Object")
			p.value, p.geo, p.background = object.New(), object.New(), object.New()
			p.source, p.clip = object.New(), object.New()
			p.value.Set("source", p.source)
			p.value.Set("clip", p.clip)
		}
		p.source.Set("serverStart", view.source.serverStart)
		p.source.Set("epoch", view.source.epoch)
		// String counters preserve the full uint64 source identity in JavaScript.
		p.source.Set("projectRevision", strconv.FormatUint(view.source.projectRevision, 10))
		p.source.Set("generation", strconv.FormatUint(view.source.generation, 10))
		p.value.Set("geo", js.Null())
		p.value.Set("map", js.Null())
		if view.available {
			p.geo.Set("latitude", view.geo.Latitude)
			p.geo.Set("longitude", view.geo.Longitude)
			p.geo.Set("projection", view.geo.Projection)
			p.geo.Set("radius", view.geo.Radius)
			p.background.Set("provider", view.background.Provider)
			p.background.Set("opacity", view.background.Opacity)
			p.value.Set("geo", p.geo)
			p.value.Set("map", p.background)
		}
		p.value.Set("hidden", view.hidden)
		p.value.Set("scale", view.scale)
		p.value.Set("x", view.origin.X)
		p.value.Set("y", view.origin.Y)
		p.value.Set("width", view.size.X)
		p.value.Set("height", view.size.Y)
		p.clip.Set("x", view.clip.Min.X)
		p.clip.Set("y", view.clip.Min.Y)
		p.clip.Set("width", view.clip.Dx())
		p.clip.Set("height", view.clip.Dy())
		hook.Invoke(p.value)
		p.last, p.sent, p.refresh = view, true, requested
	}
	enabled := js.Global().Get("podsimMapEnabled")
	return view.available && !view.hidden && enabled.Type() == js.TypeBoolean && enabled.Bool()
}
