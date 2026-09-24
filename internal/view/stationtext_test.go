package view

import (
	"image"
	"math"
	"testing"

	"github.com/hajimehoshi/ebiten/v2/text/v2"

	"github.com/dotwaffle/podsim/internal/observe"
	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

func TestPlaceStationText(t *testing.T) {
	t.Parallel()
	viewport := image.Rect(0, 0, 200, 100)
	// ring is a berth ring in the middle of the viewport.
	ring := image.Rect(90, 40, 110, 60)
	size := image.Pt(40, 10)
	tests := []struct {
		name      string
		placement stationTextPlacement
		want      image.Rectangle
		wantOK    bool
	}{
		{
			name:      "below when the siding is above",
			placement: stationTextPlacement{item: ring, size: size, away: sim.Point{Y: 1}},
			want:      image.Rect(80, 62, 120, 72), wantOK: true,
		},
		{
			name:      "above when the siding is below",
			placement: stationTextPlacement{item: ring, size: size, away: sim.Point{Y: -1}},
			want:      image.Rect(80, 28, 120, 38), wantOK: true,
		},
		{
			name:      "left when the siding is to the right",
			placement: stationTextPlacement{item: ring, size: size, away: sim.Point{X: -1, Y: 0.1}},
			want:      image.Rect(48, 45, 88, 55), wantOK: true,
		},
		{
			name:      "right when the siding is to the left",
			placement: stationTextPlacement{item: ring, size: size, away: sim.Point{X: 1}},
			want:      image.Rect(112, 45, 152, 55), wantOK: true,
		},
		{
			name:      "no siding puts the text below",
			placement: stationTextPlacement{item: ring, size: size},
			want:      image.Rect(80, 62, 120, 72), wantOK: true,
		},
		{
			// The corner moves in by 3 pixels: 10*(1-sqrt(2)/2) rounded.
			name:      "corner moves in toward a round item",
			placement: stationTextPlacement{item: ring, size: size, away: sim.Point{X: 1, Y: 1}},
			want:      image.Rect(109, 59, 149, 69), wantOK: true,
		},
		{
			name:      "equal directions take the earlier place",
			placement: stationTextPlacement{item: ring, size: size, sides: []textSide{{-1, 0}, {1, 0}}, away: sim.Point{Y: 1}},
			want:      image.Rect(48, 45, 88, 55), wantOK: true,
		},
		{
			name:      "a place below slides left inside the viewport",
			placement: stationTextPlacement{item: image.Rect(180, 40, 200, 60), size: size, away: sim.Point{Y: 1}},
			want:      image.Rect(160, 62, 200, 72), wantOK: true,
		},
		{
			name:      "a place to the right slides down inside the viewport",
			placement: stationTextPlacement{item: image.Rect(90, 0, 110, 6), size: size, away: sim.Point{X: 1}},
			want:      image.Rect(112, 0, 152, 10), wantOK: true,
		},
		{
			name:      "no room above takes the next place",
			placement: stationTextPlacement{item: image.Rect(90, 2, 110, 22), size: size, sides: berthNumberSides, away: sim.Point{Y: -1}},
			want:      image.Rect(48, 7, 88, 17), wantOK: true,
		},
		{
			name: "an occupied place takes the next place",
			placement: stationTextPlacement{
				item: ring, size: size, sides: berthNumberSides, away: sim.Point{Y: 1},
				occupied: []image.Rectangle{image.Rect(95, 65, 105, 70)},
			},
			want: image.Rect(48, 45, 88, 55), wantOK: true,
		},
		{
			// The place to the left is outside the viewport, and the place
			// to the right is occupied. The place to the left moved into
			// the viewport is free.
			name: "no place fits moves the text into the viewport",
			placement: stationTextPlacement{
				item: image.Rect(-15, 40, 5, 60), size: size, sides: []textSide{{-1, 0}, {1, 0}}, away: sim.Point{X: -1},
				occupied: []image.Rectangle{image.Rect(41, 45, 47, 55)},
			},
			want: image.Rect(0, 45, 40, 55), wantOK: true,
		},
		{
			name: "all places occupied has no text",
			placement: stationTextPlacement{
				item: ring, size: size, sides: berthNumberSides, away: sim.Point{Y: 1},
				occupied: []image.Rectangle{viewport},
			},
		},
		{
			name:      "item outside the viewport has no text",
			placement: stationTextPlacement{item: image.Rect(210, 40, 230, 60), size: size, away: sim.Point{Y: 1}},
		},
		{
			name: "a lane below takes the next place",
			placement: stationTextPlacement{
				item: ring, size: size, away: sim.Point{Y: 1},
				lanes: []lanePath{{{X: 0, Y: 66}, {X: 200, Y: 66}}},
			},
			want: image.Rect(48, 45, 88, 55), wantOK: true,
		},
		{
			// The places below, left and right each cross a lane. The
			// place above crosses none.
			name: "fewest lanes wins over the away direction",
			placement: stationTextPlacement{
				item: ring, size: size, sides: berthNumberSides, away: sim.Point{Y: 1},
				lanes: []lanePath{
					{{X: 0, Y: 66}, {X: 200, Y: 66}},
					{{X: 60, Y: 0}, {X: 60, Y: 100}},
					{{X: 130, Y: 0}, {X: 130, Y: 100}},
				},
			},
			want: image.Rect(80, 28, 120, 38), wantOK: true,
		},
		{
			name: "equal lanes take the away direction",
			placement: stationTextPlacement{
				item: ring, size: size, sides: berthNumberSides, away: sim.Point{X: -1},
				lanes: []lanePath{{{X: 68, Y: 0}, {X: 68, Y: 100}}, {{X: 100, Y: 0}, {X: 100, Y: 100}}, {{X: 132, Y: 0}, {X: 132, Y: 100}}},
			},
			want: image.Rect(48, 45, 88, 55), wantOK: true,
		},
		{
			name: "a short cover keeps the away direction",
			placement: stationTextPlacement{
				item: ring, size: size, sides: berthNumberSides, away: sim.Point{Y: 1}, awayCost: 20,
				lanes: []lanePath{{{X: 100, Y: 60}, {X: 100, Y: 66}}},
			},
			want: image.Rect(80, 62, 120, 72), wantOK: true,
		},
		{
			name: "a long cover turns the text",
			placement: stationTextPlacement{
				item: ring, size: size, sides: berthNumberSides, away: sim.Point{Y: 1}, awayCost: 20,
				lanes: []lanePath{{{X: 0, Y: 66}, {X: 200, Y: 66}}},
			},
			want: image.Rect(48, 45, 88, 55), wantOK: true,
		},
		{
			name: "padding does not count as cover",
			placement: stationTextPlacement{
				item: ring, size: size, sides: berthNumberSides, away: sim.Point{Y: 1}, padding: 2,
				lanes: []lanePath{{{X: 0, Y: 71}, {X: 200, Y: 71}}},
			},
			want: image.Rect(80, 62, 120, 72), wantOK: true,
		},
		{
			name: "berth numbers take no corner",
			placement: stationTextPlacement{
				item: ring, size: size, sides: berthNumberSides, away: sim.Point{X: 1, Y: 1.1},
			},
			want: image.Rect(80, 62, 120, 72), wantOK: true,
		},
		{
			name:      "no places has no text",
			placement: stationTextPlacement{item: ring, size: size, sides: []textSide{}, away: sim.Point{Y: 1}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			placement := test.placement
			placement.gap = 2
			placement.viewport = viewport
			if placement.sides == nil {
				placement.sides = textSides
			}
			got, _, ok := placeStationText(placement)
			if got != test.want || ok != test.wantOK {
				t.Fatalf("placeStationText() = %v, %t, want %v, %t", got, ok, test.want, test.wantOK)
			}
		})
	}
}

func TestLanePathCovered(t *testing.T) {
	t.Parallel()
	area := image.Rect(10, 10, 20, 20)
	tests := []struct {
		name string
		path lanePath
		want float64
	}{
		{name: "through", path: lanePath{{X: 0, Y: 15}, {X: 30, Y: 15}}, want: 10},
		{name: "diagonal through", path: lanePath{{X: 0, Y: 0}, {X: 30, Y: 30}}, want: 10 * math.Sqrt2},
		{name: "inside", path: lanePath{{X: 12, Y: 15}, {X: 18, Y: 15}}, want: 6},
		{name: "ends inside", path: lanePath{{X: 0, Y: 15}, {X: 15, Y: 15}}, want: 5},
		{name: "two segments", path: lanePath{{X: 0, Y: 12}, {X: 15, Y: 12}, {X: 15, Y: 30}}, want: 13},
		{name: "above", path: lanePath{{X: 0, Y: 5}, {X: 30, Y: 5}}},
		{name: "stops short", path: lanePath{{X: 0, Y: 15}, {X: 8, Y: 15}}},
		{name: "diagonal past the corner", path: lanePath{{X: 0, Y: 5}, {X: 30, Y: -25}}},
		{name: "vertical to the right", path: lanePath{{X: 25, Y: 0}, {X: 25, Y: 30}}},
		{name: "one point", path: lanePath{{X: 15, Y: 15}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := test.path.covered(area); math.Abs(got-test.want) > 1e-9 {
				t.Fatalf("covered(%v) = %g, want %g", test.path, got, test.want)
			}
		})
	}
}

func TestLaneGeometryPathIn(t *testing.T) {
	t.Parallel()
	viewport := image.Rect(10, 10, 20, 20)
	tests := []struct {
		name   string
		points []sim.Point
		want   bool
	}{
		{name: "through", points: []sim.Point{{X: 0, Y: 15}, {X: 30, Y: 15}}, want: true},
		{name: "second segment in", points: []sim.Point{{X: 0, Y: 0}, {X: 30, Y: 0}, {X: 15, Y: 15}}, want: true},
		{name: "outside", points: []sim.Point{{X: 0, Y: 5}, {X: 30, Y: 5}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var geometry laneGeometry
			geometry.count = copy(geometry.points[:], test.points)
			path, ok := geometry.pathIn(viewport)
			if ok != test.want || ok && len(path) != len(test.points) {
				t.Fatalf("pathIn() = %v, %t, want %d points, %t", path, ok, len(test.points), test.want)
			}
		})
	}
}

func TestShiftInside(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                  string
		start, end, low, high int
		want                  int
	}{
		{name: "inside", start: 10, end: 20, low: 0, high: 100, want: 0},
		{name: "past the end", start: 90, end: 110, low: 0, high: 100, want: -10},
		{name: "before the start", start: -5, end: 5, low: 0, high: 100, want: 5},
		{name: "too long starts at low", start: -20, end: 120, low: 0, high: 100, want: 20},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := shiftInside(test.start, test.end, test.low, test.high); got != test.want {
				t.Fatalf("shiftInside(%d, %d, %d, %d) = %d, want %d", test.start, test.end, test.low, test.high, got, test.want)
			}
		})
	}
}

func TestStationTextDirection(t *testing.T) {
	t.Parallel()
	network := sim.Example()
	tests := []struct {
		station string
		want    sim.Point
	}{
		// The Harbor and Market berths are below their sidings, and the
		// Garden and Parking berths are above them.
		{station: "harbor", want: sim.Point{X: -10, Y: 90}},
		{station: "garden", want: sim.Point{X: -5, Y: -80}},
		{station: "market", want: sim.Point{X: -5, Y: 90}},
		{station: "parking", want: sim.Point{X: 0, Y: -67.5}},
		{station: "unknown", want: sim.Point{}},
	}
	for _, test := range tests {
		t.Run(test.station, func(t *testing.T) {
			t.Parallel()
			station, ok := network.Station(test.station)
			if !ok {
				station = sim.Station{ID: test.station, Entry: "missing", Berths: []sim.Berth{{Node: "harbor-berth"}}}
			}
			if got := stationTextDirection(network, station); got != test.want {
				t.Fatalf("stationTextDirection(%s) = %v, want %v", test.station, got, test.want)
			}
		})
	}
}

// testLanePaths returns the screen paths of the lanes in the map viewport,
// as drawBaseNetwork returns them.
func testLanePaths(game *Game) []lanePath {
	style := newNetworkStyle(networkStyleInput{network: game.network, markers: game.collapsedStationMarkers(), lineLanes: game.currentLineLanes(), scale: game.mapScale, unit: game.layout.unit})
	var paths []lanePath
	for _, lane := range game.network.Lanes {
		if path, ok := game.laneGeometry(lane, style.detailed).pathIn(game.layout.mapViewport); ok {
			paths = append(paths, path)
		}
	}
	return paths
}

// placedTestText returns the expanded station text of all stations of the
// game network that show their berths, the text at its place, and the lanes
// in the map viewport.
func placedTestText(game *Game) ([]expandedStationText, []placedStationText, []lanePath) {
	var expanded []expandedStationText
	markers := game.collapsedStationMarkers()
	for _, station := range game.network.Stations {
		if _, ok := markers[station.ID]; ok {
			continue
		}
		expanded = append(expanded, game.expandedStationText(expandedStationInput{station: station, status: observe.StationMetrics{Free: len(station.Berths)}}))
	}
	lanes := testLanePaths(game)
	return expanded, game.placeExpandedStationText(expanded, lanes), lanes
}

// checkStationText checks that each text block is inside the map viewport
// and covers no berth ring in the viewport and no other text block.
func checkStationText(t *testing.T, game *Game, expanded []expandedStationText, placed []placedStationText) {
	t.Helper()
	viewport := game.layout.mapViewport
	for index, text := range placed {
		if !text.area.In(viewport) {
			t.Errorf("text %q at %v is outside the map viewport %v", text.block.lines[0].value, text.area, viewport)
		}
		for _, station := range expanded {
			for _, berth := range station.berths {
				if berth.ring.Overlaps(viewport) && text.area.Overlaps(berth.ring) {
					t.Errorf("text %q at %v covers the berth ring at %v", text.block.lines[0].value, text.area, berth.ring)
				}
			}
		}
		for _, other := range placed[:index] {
			if text.area.Overlaps(other.area) {
				t.Errorf("text %q at %v covers text %q at %v", text.block.lines[0].value, text.area, other.block.lines[0].value, other.area)
			}
		}
	}
}

// TestStationTextOnExample checks the example network at Fit. Before, the
// Market text ended outside the map, and the Garden and Parking text
// crossed lanes.
func TestStationTextOnExample(t *testing.T) {
	t.Parallel()
	for _, size := range []struct {
		width, height int
		scale         float64
		// maxCover is the largest length of lanes in display units that a
		// text block covers. In the 1100x760 window, each place of the
		// Parking text covers a lane or other text.
		maxCover float64
		// below is true when the Harbor and Market text is below the ring,
		// away from the siding. The Garden text is never below the ring.
		below bool
	}{
		{1100, 760, 1, 190, true}, {1366, 610, 1, 70, false}, {1366, 610, 2, 70, false},
		{1920, 1080, 1, 20, true}, {1920, 1080, 2, 20, true}, {2560, 1440, 1, 20, true}, {2560, 1440, 2, 20, true},
	} {
		game := journeyNetworkGame(t, sim.Example())
		game.layoutFor(layoutInput{outsideWidth: size.width, outsideHeight: size.height, deviceScale: size.scale})
		game.fitNetwork()
		expanded, placed, lanes := placedTestText(game)
		// Three single-berth stations and Parking with two berth numbers
		// and its station text.
		if len(expanded) != 4 || len(placed) != 6 {
			t.Fatalf("window %dx%d: %d expanded stations and %d text blocks, want 4 and 6", size.width, size.height, len(expanded), len(placed))
		}
		checkStationText(t, game, expanded, placed)
		padding := int(stationTextPadding * game.layout.unit)
		for _, text := range placed {
			covered := 0.0
			for _, lane := range lanes {
				covered += lane.covered(text.area.Inset(padding))
			}
			if covered/game.layout.unit > size.maxCover {
				t.Errorf("window %dx%d@%g: text %q at %v covers %.0f units of lanes, want at most %g", size.width, size.height, size.scale, text.block.lines[0].value, text.area, covered/game.layout.unit, size.maxCover)
			}
		}
		areas := make(map[string]image.Rectangle)
		for _, text := range placed {
			areas[text.block.lines[0].value] = text.area
		}
		for index, name := range []string{"Harbor", "Garden", "Market"} {
			area, ring := areas[name], expanded[index].berths[0].ring
			below := area.Min.Y >= ring.Max.Y
			if wantBelow := name != "Garden"; (size.below || !wantBelow) && below != wantBelow {
				t.Errorf("window %dx%d@%g: %s text at %v is below the ring at %v: %t, want %t", size.width, size.height, size.scale, name, area, ring, below, wantBelow)
			}
		}
	}
}

// TestStationTextOnLondon checks London zoomed in on West London Parking and
// on Goodge Street, where the berths show. Before, the Parking berth numbers
// were on the rings of the next berths.
func TestStationTextOnLondon(t *testing.T) {
	t.Parallel()
	network := scenarios.London().Network
	for _, stationID := range []string{"parking-west", "940GZZLUGDG"} {
		t.Run(stationID, func(t *testing.T) {
			t.Parallel()
			game := journeyNetworkGame(t, network)
			game.state = session.State{Epoch: "london"}
			game.layoutFor(layoutInput{outsideWidth: 1100, outsideHeight: 760, deviceScale: 1})
			game.fitNetwork()
			anchor, ok := game.stationAnchors()[stationID]
			if !ok {
				t.Fatalf("station %s has no anchor", stationID)
			}
			game.camera.zoomAt(game.mapPoint(anchor), 20)
			game.syncCamera()
			expanded, placed, _ := placedTestText(game)
			found := false
			for _, station := range expanded {
				found = found || len(station.berths) > 0 && station.berths[0].text.lines[0].value == "1" && len(station.station.lines) > 0
			}
			if !found || len(placed) == 0 {
				t.Fatalf("%d expanded stations and %d text blocks, want berths that show", len(expanded), len(placed))
			}
			checkStationText(t, game, expanded, placed)
			checkBerthNumbers(t, expanded, placed)
			checkPodLabelsCleared(t, game, placed)
		})
	}
}

// checkBerthNumbers checks that each berth number is directly above, below,
// left or right of its ring, so that it does not look like the number of
// the next berth.
func checkBerthNumbers(t *testing.T, expanded []expandedStationText, placed []placedStationText) {
	t.Helper()
	areas := make(map[*label]image.Rectangle)
	for _, text := range placed {
		areas[&text.block.lines[0]] = text.area
	}
	for _, station := range expanded {
		if len(station.station.lines) == 0 {
			continue
		}
		for _, berth := range station.berths {
			area, ok := areas[&berth.text.lines[0]]
			if !ok {
				continue
			}
			beside := area.Min.X < berth.ring.Max.X && berth.ring.Min.X < area.Max.X ||
				area.Min.Y < berth.ring.Max.Y && berth.ring.Min.Y < area.Max.Y
			if !beside {
				t.Errorf("berth number %q at %v is at a corner of its ring at %v", berth.text.lines[0].value, area, berth.ring)
			}
		}
	}
}

// checkPodLabelsCleared checks that a pod label over station text does not
// show, and that the label of the selected pod shows.
func checkPodLabelsCleared(t *testing.T, game *Game, placed []placedStationText) {
	t.Helper()
	area := placed[len(placed)-1].area
	over := label{x: float64(area.Min.X), y: float64(area.Min.Y), size: 11, value: "P1", color: foreground}
	game.selected = 1
	got := game.clearPodLabels([]label{over, over}, appendStationTextAreas(nil, placed))
	if got[0].value != "" || got[1].value != "P1" {
		t.Errorf("clearPodLabels() = %q and %q, want the first label cleared and the selected label kept", got[0].value, got[1].value)
	}
}

func TestStationTextSize(t *testing.T) {
	t.Parallel()
	game := journeyNetworkGame(t, sim.Example())
	game.layoutFor(layoutInput{outsideWidth: 1920, outsideHeight: 1080, deviceScale: 2})
	block := stationTextBlock{lines: []label{{size: 16, value: "Parking"}}}
	width, height := text.Measure("Parking", game.textFace(16), 0)
	padding := 2 * stationTextPadding * game.layout.unit
	want := image.Pt(int(math.Ceil(width+padding)), int(math.Ceil(height+padding)))
	if got := game.stationTextSize(block); got != want {
		t.Fatalf("stationTextSize() = %v, want %v", got, want)
	}
}
