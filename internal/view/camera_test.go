package view

import (
	"image"
	"math"
	"slices"
	"testing"

	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

func TestCollapsedStationLabelsPreferSelectedContext(t *testing.T) {
	t.Parallel()
	labels := []boundedStationLabel{
		{stationID: "first", bounds: image.Rect(0, 0, 60, 20)},
		{stationID: "preferred", bounds: image.Rect(20, 0, 80, 20)},
		{stationID: "separate", bounds: image.Rect(100, 0, 160, 20)},
	}
	visible := selectCollapsedStationLabels(labels, map[string]bool{"preferred": true}, true)
	if visible[0] || !visible[1] || !visible[2] {
		t.Fatalf("visible labels = %v, want [false true true]", visible)
	}
	if all := selectCollapsedStationLabels(labels, nil, false); !all[0] || !all[1] || !all[2] {
		t.Fatalf("small-network labels = %v, want all visible", all)
	}
}

func TestDenseOverviewHidesUnselectedPodLabels(t *testing.T) {
	t.Parallel()
	game := Game{
		network:  sim.Network{Stations: make([]sim.Station, 31)},
		selected: 2,
		mapScale: 1,
		camera:   mapCamera{minScale: 1},
	}
	if game.showPodMapLabel(1) || !game.showPodMapLabel(2) {
		t.Fatal("dense overview did not keep only the selected pod label")
	}
	game.mapScale = 4
	if !game.showPodMapLabel(1) {
		t.Fatal("dense zoom did not restore pod labels")
	}
	game.network.Stations = game.network.Stations[:3]
	game.mapScale = 1
	if !game.showPodMapLabel(1) {
		t.Fatal("small network hid a pod label")
	}
}

func TestMapCameraZoomKeepsCursorWorldPoint(t *testing.T) {
	t.Parallel()
	camera := fittedTestCamera()
	cursor := sim.Point{X: 311, Y: 287}
	want := camera.worldPoint(cursor)

	if !camera.zoomAt(cursor, 2) {
		t.Fatal("zoom did not change camera")
	}
	got := camera.worldPoint(cursor)
	if !closePoint(got, want) {
		t.Fatalf("world point under cursor moved: got %+v, want %+v", got, want)
	}
}

func TestMapCameraCentersOnWorldPoint(t *testing.T) {
	t.Parallel()
	camera := fittedTestCamera()
	center := sim.Point{X: float64(camera.viewport.Min.X+camera.viewport.Max.X) / 2, Y: float64(camera.viewport.Min.Y+camera.viewport.Max.Y) / 2}
	target := sim.Point{X: 500, Y: 300}
	camera.zoomAt(center, 3)

	if !camera.centerOn(target) {
		t.Fatal("camera did not move")
	}
	if got := camera.screenPoint(target); !closePoint(got, center) {
		t.Fatalf("target screen point = %+v, want %+v", got, center)
	}
}

func TestMapCameraMutationGuards(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		run  func(*mapCamera) bool
	}{
		{name: "minimum zoom", run: func(camera *mapCamera) bool {
			camera.zoomAt(sim.Point{X: 300, Y: 300}, 0.0001)
			return camera.scale == camera.minScale
		}},
		{name: "maximum zoom", run: func(camera *mapCamera) bool {
			camera.zoomAt(sim.Point{X: 300, Y: 300}, 1000)
			return camera.scale == camera.maxScale
		}},
		{name: "horizontal pan bound", run: func(camera *mapCamera) bool {
			camera.zoomAt(sim.Point{X: 300, Y: 300}, 4)
			camera.pan(sim.Point{X: 1e6})
			return camera.screenPoint(sim.Point{X: camera.world.left}).X == float64(camera.viewport.Min.X)+camera.panMargin
		}},
		{name: "vertical pan bound", run: func(camera *mapCamera) bool {
			camera.zoomAt(sim.Point{X: 300, Y: 300}, 8)
			camera.pan(sim.Point{Y: -1e6})
			return camera.screenPoint(sim.Point{Y: camera.world.bottom}).Y == float64(camera.viewport.Max.Y)-camera.panMargin
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			camera := fittedTestCamera()
			if !test.run(&camera) {
				t.Fatal("camera invariant failed")
			}
		})
	}
}

func TestMapCameraDragThresholdSuppressesClick(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		move  sim.Point
		click bool
	}{
		{name: "stationary click", move: sim.Point{X: 102, Y: 102}, click: true},
		{name: "small jitter", move: sim.Point{X: 104, Y: 102}, click: true},
		{name: "drag", move: sim.Point{X: 108, Y: 102}, click: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			camera := fittedTestCamera()
			camera.beginDrag(sim.Point{X: 102, Y: 102})
			camera.drag(test.move)
			if got := camera.endDrag(); got != test.click {
				t.Fatalf("click = %t, want %t", got, test.click)
			}
		})
	}
}

func TestGameCameraWiring(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		run  func(*testing.T)
	}{
		{name: "camera change invalidates base cache", run: func(t *testing.T) {
			t.Helper()
			game := cameraTestGame()
			game.networkBaseValid = true
			game.camera.zoomAt(sim.Point{X: 300, Y: 300}, 2)
			game.syncCamera()
			if game.networkBaseValid {
				t.Fatal("camera change kept stale base cache")
			}
		}},
		{name: "outside map cannot select pod", run: func(t *testing.T) {
			t.Helper()
			game := cameraTestGame()
			target := game.state.Simulation.Vehicles[1].Pod.Position
			game.camera.origin = sim.Point{X: 10 - target.X*game.camera.scale, Y: 10 - target.Y*game.camera.scale}
			game.syncCamera()
			game.click(sim.Point{X: 10, Y: 10})
			if game.selected != 0 {
				t.Fatalf("selected pod %d outside map", game.selected)
			}
		}},
		{name: "selection follows zoom and pan", run: func(t *testing.T) {
			t.Helper()
			game := cameraTestGame()
			game.camera.zoomAt(sim.Point{X: 300, Y: 300}, 3)
			game.camera.pan(sim.Point{X: -80, Y: 30})
			game.syncCamera()
			game.click(game.mapPoint(game.state.Simulation.Vehicles[1].Pod.Position))
			if game.selected != 1 {
				t.Fatalf("selected pod %d, want 1", game.selected)
			}
		}},
		{name: "berths reveal with screen spacing", run: func(t *testing.T) {
			t.Helper()
			game := cameraTestGame()
			station := game.network.Stations[0]
			game.mapScale = 1
			if game.showStationBerths(station) {
				t.Fatal("showed overlapping berth details")
			}
			game.mapScale = 4
			if !game.showStationBerths(station) {
				t.Fatal("kept spaced berth details collapsed")
			}
		}},
		{name: "follow centers selected pod and invalidates cache", run: func(t *testing.T) {
			t.Helper()
			game := cameraTestGame()
			game.selected = 1
			center := sim.Point{X: float64(game.camera.viewport.Min.X+game.camera.viewport.Max.X) / 2, Y: float64(game.camera.viewport.Min.Y+game.camera.viewport.Max.Y) / 2}
			game.camera.zoomAt(center, 3)
			game.syncCamera()
			game.networkBaseValid = true
			game.toggleFollow()
			if got := game.mapPoint(game.state.Simulation.Vehicles[1].Pod.Position); !closePoint(got, center) {
				t.Fatalf("followed pod screen point = %+v, want %+v", got, center)
			}
			if !game.followSelected || game.networkBaseValid {
				t.Fatal("follow state or map cache not updated")
			}
		}},
		{name: "fit stops follow", run: func(t *testing.T) {
			t.Helper()
			game := cameraTestGame()
			game.followSelected = true
			game.click(centerOfButton(findButton(t, game.buttons(), "map-fit")))
			if game.followSelected {
				t.Fatal("fit kept pod follow enabled")
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, test.run)
	}
}

// TestGameCameraAcrossStateChanges checks when a new state refits the map.
// Changes on the same network keep the zoom, pan, and follow state of the
// user. New network bounds, a new epoch, an unfitted camera, and a layout
// change fit the whole network again.
func TestGameCameraAcrossStateChanges(t *testing.T) {
	t.Parallel()
	fullNetwork := worldBounds{left: 100, top: 100, right: 800, bottom: 500}
	tests := []struct {
		name string
		// startup fits the placeholder state before the first server state.
		// That state has no epoch.
		startup bool
		change  func(*Game)
		keep    bool
		// world is the expected camera bounds after a refit.
		world worldBounds
	}{
		{name: "rewind or reset on the same network", keep: true, change: func(g *Game) {
			g.state.Generation++
		}},
		{name: "demand edit", keep: true, change: func(g *Game) {
			g.state.ProjectRevision++
		}},
		{name: "project restore with the same bounds", keep: true, change: func(g *Game) {
			g.state.Generation++
			g.state.ProjectRevision++
			g.network.Nodes = slices.Clone(g.network.Nodes)
			g.network.Nodes[1].Position = sim.Point{X: 300, Y: 200}
		}},
		{name: "project with different bounds", world: worldBounds{left: 100, top: 100, right: 1200, bottom: 700}, change: func(g *Game) {
			g.state.Generation++
			g.state.ProjectRevision++
			g.network.Nodes = slices.Clone(g.network.Nodes)
			g.network.Nodes[2].Position = sim.Point{X: 1200, Y: 700}
		}},
		{name: "lane control point outside the nodes", world: worldBounds{left: 100, top: 50, right: 900, bottom: 500}, change: func(g *Game) {
			g.state.Generation++
			g.network.Lanes = []sim.Lane{{ID: "curve", From: "b1", To: "far", Control: new(sim.Point{X: 900, Y: 50})}}
		}},
		{name: "new epoch", world: fullNetwork, change: func(g *Game) {
			g.state.Epoch, g.state.Generation = "restarted", 1
		}},
		{name: "first server state", startup: true, world: fullNetwork, change: func(g *Game) {
			g.state.Epoch = "server"
		}},
		{name: "unfitted camera at startup", startup: true, world: fullNetwork, change: func(g *Game) {
			g.camera, g.cameraKey = mapCamera{}, cameraFitKey{}
		}},
		{name: "layout change", world: fullNetwork, change: func(g *Game) {
			g.layoutFor(layoutInput{outsideWidth: 1600, outsideHeight: 1000, deviceScale: 1})
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game := cameraTestGame()
			game.state.Generation = 1
			if !test.startup {
				game.state.Epoch = "server"
			}
			game.fitNetwork()
			center := sim.Point{X: float64(game.camera.viewport.Min.X+game.camera.viewport.Max.X) / 2, Y: float64(game.camera.viewport.Min.Y+game.camera.viewport.Max.Y) / 2}
			game.camera.zoomAt(center, 3)
			game.camera.pan(sim.Point{X: -80, Y: 30})
			game.syncCamera()
			game.followSelected = true
			before := game.camera

			test.change(game)
			game.fitNetwork()

			if test.keep {
				if game.camera != before || game.mapScale != before.scale || game.mapOrigin != before.origin || !game.followSelected {
					t.Fatalf("camera = %+v, follow %t, want kept camera %+v and follow", game.camera, game.followSelected, before)
				}
				return
			}
			var want mapCamera
			want.fit(cameraFit{bounds: test.world, viewport: game.layout.mapViewport, unit: game.layout.unit})
			got := game.camera
			if got.world != want.world || got.viewport != want.viewport || got.scale != want.scale || got.origin != want.origin {
				t.Fatalf("camera world %+v, viewport %v, scale %g, origin %+v, want refit to %+v, %v, %g, %+v", got.world, got.viewport, got.scale, got.origin, want.world, want.viewport, want.scale, want.origin)
			}
			if game.mapScale != want.scale || game.mapOrigin != want.origin {
				t.Fatalf("map scale %g and origin %+v not synced with refit camera", game.mapScale, game.mapOrigin)
			}
		})
	}
}

func cameraTestGame() *Game {
	network := sim.Network{
		Nodes: []sim.Node{
			{ID: "b1", Position: sim.Point{X: 100, Y: 100}},
			{ID: "b2", Position: sim.Point{X: 110, Y: 100}},
			{ID: "far", Position: sim.Point{X: 800, Y: 500}},
		},
		Stations: []sim.Station{{ID: "parking", Name: "Parking", ParkingOnly: true, Berths: []sim.Berth{{ID: "1", Node: "b1"}, {ID: "2", Node: "b2"}}}},
	}
	game := &Game{
		network: network,
		state: session.State{Simulation: sim.Snapshot{Vehicles: []sim.Vehicle{
			{Pod: sim.Pod{ID: "01", Position: sim.Point{X: 100, Y: 100}}},
			{Pod: sim.Pod{ID: "02", Position: sim.Point{X: 500, Y: 300}, Activity: sim.Traveling}},
		}}},
	}
	game.ensureLayout()
	game.camera.fit(cameraFit{bounds: worldBounds{left: 100, top: 100, right: 800, bottom: 500}, viewport: game.layout.mapViewport, unit: game.layout.unit})
	game.syncCamera()
	return game
}

func fittedTestCamera() mapCamera {
	var camera mapCamera
	layout := newDisplayLayout(layoutInput{outsideWidth: minimumWidth, outsideHeight: minimumHeight, deviceScale: 1})
	camera.fit(cameraFit{bounds: worldBounds{left: -100, top: -50, right: 900, bottom: 550}, viewport: layout.mapViewport, unit: layout.unit})
	return camera
}

func closePoint(a, b sim.Point) bool {
	return math.Abs(a.X-b.X) < 1e-9 && math.Abs(a.Y-b.Y) < 1e-9
}

func TestWheelZoomFactor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		delta  float64
		pixels bool
		steps  float64
	}{
		{name: "native step", delta: 1, steps: 1},
		{name: "browser step", delta: 100, pixels: true, steps: 1},
		{name: "browser trackpad", delta: 10, pixels: true, steps: .1},
		{name: "large scroll", delta: 600, pixels: true, steps: 3},
		{name: "reverse scroll", delta: -600, pixels: true, steps: -3},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := wheelZoomFactor(test.delta, test.pixels)
			if want := math.Pow(mapZoomStep, test.steps); math.Abs(got-want) > 1e-9 {
				t.Fatalf("factor %g, want %g", got, want)
			}
		})
	}
}
