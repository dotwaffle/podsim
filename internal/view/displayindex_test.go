package view

import (
	"image"
	"math"
	"testing"

	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

// TestNetworkCacheKeyCoversNetworkChanges checks that each state change that
// can bring a new network changes the display index key and the base layer
// key, and that the index then holds the new network. A pan keeps both
// keys, and a zoom changes only the base layer key.
func TestNetworkCacheKeyCoversNetworkChanges(t *testing.T) {
	t.Parallel()
	first := session.State{Epoch: "first", ProjectRevision: 1, Generation: 1}
	tests := []struct {
		name  string
		state session.State
		// newNetwork is true when the state brings a moved network.
		newNetwork bool
		pan, zoom  bool
		// indexChange and baseChange tell which keys must change.
		indexChange, baseChange bool
	}{
		{name: "same state", state: first},
		{name: "pan", state: first, pan: true},
		{name: "zoom", state: first, zoom: true, baseChange: true},
		{name: "new epoch", state: session.State{Epoch: "second", ProjectRevision: 1, Generation: 1}, newNetwork: true, indexChange: true, baseChange: true},
		{name: "new generation with a different network", state: session.State{Epoch: "first", ProjectRevision: 1, Generation: 2}, newNetwork: true, indexChange: true, baseChange: true},
		{name: "project apply", state: session.State{Epoch: "first", ProjectRevision: 2, Generation: 2}, newNetwork: true, indexChange: true, baseChange: true},
		{name: "restored save from a new server process", state: session.State{Epoch: "first", ProjectRevision: 1, Generation: 1, ServerStart: "restarted"}, newNetwork: true, indexChange: true, baseChange: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game := &Game{network: sim.Example(), state: first}
			game.ensureLayout()
			game.fitNetwork()
			indexKey, baseKey := game.currentNetworkIndexKey(), game.currentNetworkCacheKey()
			node := game.network.Nodes[0]
			if got := game.nodePosition(node.ID); got != node.Position {
				t.Fatalf("first node position = %v, want %v", got, node.Position)
			}
			want := node.Position
			if test.newNetwork {
				moved := sim.Example()
				for index := range moved.Nodes {
					moved.Nodes[index].Position.X += 1000
				}
				game.network = moved
				want = moved.Nodes[0].Position
			}
			game.state = test.state
			if test.pan {
				game.camera.origin.X += 5
				game.syncCamera()
			}
			if test.zoom {
				game.zoomMap(mapZoomStep)
			}
			if got := game.currentNetworkIndexKey() != indexKey; got != test.indexChange {
				t.Errorf("index key changed = %t, want %t", got, test.indexChange)
			}
			if got := game.currentNetworkCacheKey() != baseKey; got != test.baseChange {
				t.Errorf("base layer key changed = %t, want %t", got, test.baseChange)
			}
			if got := game.nodePosition(node.ID); got != want {
				t.Errorf("node position = %v, want %v", got, want)
			}
		})
	}
}

// TestBaseLayerShift checks when the cached base layer moves and when the
// cache draws it again.
func TestBaseLayerShift(t *testing.T) {
	t.Parallel()
	key := networkCacheKey{network: networkIndexKey{epoch: "first", generation: 1, projectRevision: 1}, scale: 0.1, unit: 1, viewport: image.Rect(24, 136, 772, 520)}
	changed := func(change func(*networkCacheKey)) networkCacheKey {
		next := key
		change(&next)
		return next
	}
	origin := sim.Point{X: 100, Y: 200}
	tests := []struct {
		name    string
		valid   bool
		current networkCacheKey
		origin  sim.Point
		want    sim.Point
		ok      bool
	}{
		{name: "same origin", valid: true, current: key, origin: origin, ok: true},
		{name: "pan in margin", valid: true, current: key, origin: sim.Point{X: 130.5, Y: 104}, want: sim.Point{X: 30.5, Y: -96}, ok: true},
		{name: "pan to margin", valid: true, current: key, origin: sim.Point{X: 4, Y: 296}, want: sim.Point{X: -96, Y: 96}, ok: true},
		{name: "pan past margin", valid: true, current: key, origin: sim.Point{X: 100, Y: 296.5}},
		{name: "no layer", current: key, origin: origin},
		{name: "zoom", valid: true, current: changed(func(k *networkCacheKey) { k.scale = 0.125 }), origin: origin},
		{name: "new unit", valid: true, current: changed(func(k *networkCacheKey) { k.unit = 2 }), origin: origin},
		{name: "new viewport", valid: true, current: changed(func(k *networkCacheKey) { k.viewport.Max.X++ }), origin: origin},
		{name: "new epoch", valid: true, current: changed(func(k *networkCacheKey) { k.network.epoch = "second" }), origin: origin},
		{name: "new generation", valid: true, current: changed(func(k *networkCacheKey) { k.network.generation++ }), origin: origin},
		{name: "new project revision", valid: true, current: changed(func(k *networkCacheKey) { k.network.projectRevision++ }), origin: origin},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, ok := baseLayerShift(baseLayerInput{valid: test.valid, cached: key, current: test.current, drawnOrigin: origin, origin: test.origin, margin: 96})
			if got != test.want || ok != test.ok {
				t.Fatalf("baseLayerShift = %v, %t, want %v, %t", got, ok, test.want, test.ok)
			}
		})
	}
}

func TestBaseLayerMargin(t *testing.T) {
	t.Parallel()
	tests := []struct {
		viewport image.Rectangle
		want     int
	}{
		{viewport: image.Rect(24, 136, 772, 520), want: 96},
		{viewport: image.Rect(0, 0, 400, 1000), want: 100},
		{viewport: image.Rect(0, 0, 0, 0), want: 0},
	}
	for _, test := range tests {
		if got := baseLayerMargin(test.viewport); got != test.want {
			t.Errorf("baseLayerMargin(%v) = %d, want %d", test.viewport, got, test.want)
		}
	}
}

func TestBerthSpacings(t *testing.T) {
	t.Parallel()
	positions := map[string]sim.Point{"a": {X: 0, Y: 0}, "b": {X: 30, Y: 40}, "c": {X: 0, Y: 20}}
	berths := func(nodes ...string) []sim.Berth {
		var list []sim.Berth
		for _, node := range nodes {
			list = append(list, sim.Berth{ID: node, Node: node})
		}
		return list
	}
	stations := []sim.Station{
		{ID: "one", Berths: berths("a")},
		{ID: "two", Berths: berths("a", "b")},
		{ID: "three", Berths: berths("a", "b", "c")},
		{ID: "unknown", Berths: berths("a", "missing")},
	}
	got := berthSpacings(stations, positions)
	want := map[string]float64{"two": 50, "three": 20}
	if len(got) != len(want) {
		t.Fatalf("berthSpacings = %v, want %v", got, want)
	}
	for id, spacing := range want {
		if got[id] != spacing {
			t.Errorf("spacing of %s = %v, want %v", id, got[id], spacing)
		}
	}
}

// TestMovedLanePaths checks that the lane paths of a moved base layer move
// with it, and that no move returns the same paths.
func TestMovedLanePaths(t *testing.T) {
	t.Parallel()
	paths := []lanePath{{{X: 1, Y: 2}, {X: 3, Y: 4}}}
	if got := movedLanePaths(paths, sim.Point{}); &got[0][0] != &paths[0][0] {
		t.Fatal("no move copied the paths")
	}
	got := movedLanePaths(paths, sim.Point{X: 10, Y: -1})
	want := lanePath{{X: 11, Y: 1}, {X: 13, Y: 3}}
	if got[0][0] != want[0] || got[0][1] != want[1] {
		t.Fatalf("moved path = %v, want %v", got[0], want)
	}
	if paths[0][0] != (sim.Point{X: 1, Y: 2}) {
		t.Fatal("move changed the source paths")
	}
}

// TestPlanBaseLayer checks the cache state that planBaseLayer keeps from
// frame to frame. A pan inside the margin moves the cached layer. A pan past
// the margin draws the layer again, and a later pan is measured from the new
// layer. A zoom, a new unit and an invalid cache draw the layer again. The
// test does not draw.
func TestPlanBaseLayer(t *testing.T) {
	t.Parallel()
	game := &Game{network: sim.Example(), state: session.State{Epoch: "first", ProjectRevision: 1, Generation: 1}}
	game.ensureLayout()
	game.fitNetwork()
	viewport := game.layout.mapViewport
	margin := float64(baseLayerMargin(viewport))
	pan := func(dx float64) func() {
		return func() {
			game.camera.origin.X += dx
			game.syncCamera()
		}
	}
	steps := []struct {
		name   string
		change func()
		redraw bool
		shift  sim.Point
	}{
		{name: "first frame", change: func() {}, redraw: true},
		{name: "same frame", change: func() {}},
		{name: "pan inside the margin", change: pan(10), shift: sim.Point{X: 10}},
		{name: "pan past the margin", change: pan(margin), redraw: true},
		{name: "pan from the new layer", change: pan(-10), shift: sim.Point{X: -10}},
		{name: "zoom", change: func() { game.zoomMap(mapZoomStep) }, redraw: true},
		{name: "new unit", change: func() { game.layout.unit *= 2 }, redraw: true},
		{name: "invalid cache", change: func() { game.networkBaseValid = false }, redraw: true},
	}
	for _, step := range steps {
		step.change()
		drawnOrigin := game.networkBaseOrigin
		plan := game.planBaseLayer()
		if plan.redraw != step.redraw || plan.shift != step.shift {
			t.Fatalf("%s: plan redraw %t shift %v, want %t %v", step.name, plan.redraw, plan.shift, step.redraw, step.shift)
		}
		if want := baseLayerArea(game.layout.mapViewport); plan.area != want {
			t.Fatalf("%s: plan area %v, want %v", step.name, plan.area, want)
		}
		if want := (sim.Point{X: -float64(plan.area.Min.X), Y: -float64(plan.area.Min.Y)}); plan.imageOffset() != want {
			t.Fatalf("%s: image offset %v, want %v", step.name, plan.imageOffset(), want)
		}
		if step.redraw {
			drawnOrigin = game.mapOrigin
		}
		if game.networkBaseOrigin != drawnOrigin || !game.networkBaseValid || game.networkBaseKey != game.currentNetworkCacheKey() {
			t.Fatalf("%s: cache origin %v valid %t, want origin %v valid with the current key", step.name, game.networkBaseOrigin, game.networkBaseValid, drawnOrigin)
		}
	}
}

// TestMovedBaseLanesMatchPan checks that the lane paths of the cached layer,
// moved by the planned shift, are the lane paths at the current camera.
func TestMovedBaseLanesMatchPan(t *testing.T) {
	t.Parallel()
	game := &Game{network: sim.Example()}
	game.ensureLayout()
	game.fitNetwork()
	paths := func(area image.Rectangle) []lanePath {
		var list []lanePath
		for _, lane := range game.network.Lanes {
			if _, path, ok := game.baseLane(lane, false, area); ok {
				list = append(list, path)
			}
		}
		return list
	}
	first := game.planBaseLayer()
	cached := paths(first.area)
	game.camera.origin.X += 7
	game.camera.origin.Y -= 5
	game.syncCamera()
	plan := game.planBaseLayer()
	if plan.redraw {
		t.Fatal("small pan draws the layer again")
	}
	got := movedLanePaths(cached, plan.shift)
	want := paths(first.area.Add(image.Pt(7, -5)))
	if len(got) != len(want) || len(got) == 0 {
		t.Fatalf("moved paths %d, want %d", len(got), len(want))
	}
	for i := range got {
		if len(got[i]) != len(want[i]) {
			t.Fatalf("path %d has %d points, want %d", i, len(got[i]), len(want[i]))
		}
		for j := range got[i] {
			if math.Abs(got[i][j].X-want[i][j].X) > 1e-9 || math.Abs(got[i][j].Y-want[i][j].Y) > 1e-9 {
				t.Fatalf("path %d point %d = %v, want %v", i, j, got[i][j], want[i][j])
			}
		}
	}
}
