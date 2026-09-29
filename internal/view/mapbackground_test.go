package view

import (
	"image"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

func TestMapViewTracksCameraAndSource(t *testing.T) {
	t.Parallel()
	game := &Game{state: session.State{Epoch: "one", ServerStart: "server", ProjectRevision: 3, Generation: 4,
		Geo: &project.Geo{Latitude: 51.5, Longitude: -.1, Projection: project.GeoProjection, Radius: project.GeoRadius},
		Map: &project.MapBackground{Provider: "osm", Opacity: .45}},
		layout: displayLayout{width: 2200, height: 1456, mapViewport: image.Rect(48, 208, 1544, 976)},
		camera: mapCamera{scale: 2, origin: sim.Point{X: 100, Y: 200}}}
	original := game.currentMapView()
	if !original.available || original.scale != 2 || original.origin != game.camera.origin || original.clip != game.layout.mapViewport || original.size != image.Pt(2200, 1456) {
		t.Fatalf("map view: %+v", original)
	}
	if again := game.currentMapView(); again != original {
		t.Fatal("unchanged view was not comparable")
	}
	for _, test := range []struct {
		name   string
		change func(*Game)
	}{
		{"pan", func(g *Game) { g.camera.origin.X++ }},
		{"zoom", func(g *Game) { g.camera.scale++ }},
		{"resize", func(g *Game) { g.layout.width++ }},
		{"epoch", func(g *Game) { g.state.Epoch = "two" }},
		{"generation", func(g *Game) { g.state.Generation++ }},
		{"project", func(g *Game) { g.state.ProjectRevision++ }},
		{"restart", func(g *Game) { g.state.ServerStart = "other" }},
		{"hidden", func(g *Game) { g.hidden = true }},
	} {
		clone := *game
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			test.change(&clone)
			if clone.currentMapView() == original {
				t.Fatal("change did not invalidate map view")
			}
		})
	}
	game.state.Map = nil
	if got := game.currentMapView(); got.available || got.background.Provider != "" || got.geo.Projection != "" {
		t.Fatal("removed map kept stale metadata")
	}
	game.state.Map = &project.MapBackground{Provider: "osm", Opacity: .45}
	game.state.Geo = nil
	if game.currentMapView().available {
		t.Fatal("map without geo was available")
	}
}
