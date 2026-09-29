package view

import (
	"image"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// mapView contains the camera in screen-image pixels. The browser bridge
// maps these pixels through the drawing buffer into CSS coordinates.
type mapView struct {
	source            networkIndexKey
	geo               project.Geo
	background        project.MapBackground
	available, hidden bool
	scale             float64
	origin            sim.Point
	size              image.Point
	clip              image.Rectangle
}

func (g *Game) currentMapView() mapView {
	view := mapView{source: g.currentNetworkIndexKey(), hidden: g.hidden,
		scale: g.camera.scale, origin: g.camera.origin,
		size: image.Pt(g.layout.width, g.layout.height), clip: g.layout.mapViewport}
	if g.state.Geo != nil && g.state.Map != nil {
		view.geo, view.background = *g.state.Geo, *g.state.Map
		view.available = g.state.Epoch != "" && view.background.Provider == "osm"
	}
	return view
}
