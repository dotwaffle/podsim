package view

import (
	"fmt"
	"math"
	"testing"

	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
)

func TestMapSizePixels(t *testing.T) {
	t.Parallel()
	size := mapSize{meters: 150, minimum: 3, maximum: 10}
	tests := []struct {
		name  string
		scale float64
		unit  float64
		want  float64
	}{
		{name: "below minimum", scale: 0.01, unit: 1, want: 3},
		{name: "at minimum", scale: 0.02, unit: 1, want: 3},
		{name: "between limits", scale: 0.04, unit: 1, want: 6},
		{name: "at maximum", scale: 10.0 / 150, unit: 1, want: 10},
		{name: "above maximum", scale: 1, unit: 1, want: 10},
		{name: "minimum in units", scale: 0.01, unit: 2, want: 6},
		{name: "between limits in units", scale: 0.1, unit: 2, want: 15},
		{name: "maximum in units", scale: 1, unit: 1.5, want: 15},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := size.pixels(test.scale, test.unit); math.Abs(got-test.want) > 1e-9 {
				t.Fatalf("pixels(%v, %v) = %v, want %v", test.scale, test.unit, got, test.want)
			}
		})
	}
}

func TestDenseMapSizes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		size  mapSize
		scale float64
		want  float64
	}{
		{name: "marker at overview", size: denseMarkerRadius, scale: 0.01, want: 3},
		{name: "marker at 150 m", size: denseMarkerRadius, scale: 0.05, want: 7.5},
		{name: "marker when zoomed in", size: denseMarkerRadius, scale: 0.5, want: 10},
		{name: "lane at overview", size: denseLaneWidth, scale: 0.01, want: 2},
		{name: "lane at 30 m", size: denseLaneWidth, scale: 0.1, want: 3},
		{name: "lane when zoomed in", size: denseLaneWidth, scale: 0.5, want: 5},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := test.size.pixels(test.scale, 1); math.Abs(got-test.want) > 1e-9 {
				t.Fatalf("pixels(%v, 1) = %v, want %v", test.scale, got, test.want)
			}
		})
	}
}

func TestNetworkStyleOnDenseMaps(t *testing.T) {
	t.Parallel()
	network := denseStyleNetwork()
	stationLane, throughLane, roadLane := network.Lanes[0], network.Lanes[1], network.Lanes[len(network.Lanes)-1]
	lineLanes := stationLineLanes(network)
	tests := []struct {
		name       string
		scale      float64
		unit       float64
		collapsed  []string
		marker     float64
		lane       float64
		station    laneStroke
		nodeDots   bool
		shortArrow float64
	}{
		{
			name: "overview", scale: 0.01, unit: 1, collapsed: []string{"a", "b"},
			marker: 3, lane: 2, station: laneStroke{width: 1, color: collapsedStationTrack}, nodeDots: false, shortArrow: 24,
		},
		{
			name: "overview in units", scale: 0.01, unit: 2, collapsed: []string{"a", "b"},
			marker: 6, lane: 4, station: laneStroke{width: 2, color: collapsedStationTrack}, nodeDots: false, shortArrow: 48,
		},
		{
			name: "lanes between limits", scale: 0.12, unit: 1, collapsed: []string{"a", "b"},
			marker: 10, lane: 3.6, station: laneStroke{width: 1.8, color: collapsedStationTrack}, nodeDots: false, shortArrow: 24,
		},
		{
			name: "one station expanded", scale: 0.2, unit: 1, collapsed: []string{"b"},
			marker: 10, lane: 5, station: laneStroke{width: 5, color: track}, nodeDots: true, shortArrow: 24,
		},
		{
			name: "all stations expanded", scale: 1, unit: 1,
			marker: 10, lane: 5, station: laneStroke{width: 5, color: track}, nodeDots: true, shortArrow: 24,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			markers := make(map[string]sim.Point)
			for _, id := range test.collapsed {
				markers[id] = sim.Point{}
			}
			style := newNetworkStyle(networkStyleInput{network: network, markers: markers, lineLanes: lineLanes, scale: test.scale, unit: test.unit})
			if style.detailed {
				t.Fatal("style is detailed, want dense")
			}
			if !closeTo(style.markerRadius, test.marker) || !closeTo(style.laneWidth, test.lane) {
				t.Errorf("marker radius, lane width = %v, %v, want %v, %v", style.markerRadius, style.laneWidth, test.marker, test.lane)
			}
			if style.nodeDots != test.nodeDots {
				t.Errorf("node dots = %t, want %t", style.nodeDots, test.nodeDots)
			}
			if got := style.laneStroke(stationLane); !closeTo(float64(got.width), float64(test.station.width)) || got.color != test.station.color || got.antialias {
				t.Errorf("station lane stroke = %+v, want %+v", got, test.station)
			}
			for _, lane := range []sim.Lane{throughLane, roadLane} {
				if got := style.laneStroke(lane); !closeTo(float64(got.width), test.lane) || got.color != track || got.antialias {
					t.Errorf("lane %s stroke = %+v, want width %v in track", lane.ID, got, test.lane)
				}
			}
			if style.showArrow(straightGeometry(test.shortArrow-0.01)) || !style.showArrow(straightGeometry(test.shortArrow)) {
				t.Errorf("direction arrows do not start at a lane length of %v pixels", test.shortArrow)
			}
		})
	}
}

func TestNetworkStyleAtDetailedLimit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		lanes    int
		detailed bool
		marker   float64
		station  laneStroke
		nodeDots bool
		arrow    bool
	}{
		{
			name: "at the limit", lanes: detailedLanes, detailed: true,
			marker: 10, station: laneStroke{width: 5, color: track, antialias: true}, nodeDots: true, arrow: true,
		},
		{
			name: "above the limit", lanes: detailedLanes + 1, detailed: false,
			marker: 3, station: laneStroke{width: 1, color: collapsedStationTrack}, nodeDots: false, arrow: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			network := denseStyleNetwork()
			network.Lanes = network.Lanes[:test.lanes]
			markers := map[string]sim.Point{"a": {}, "b": {}}
			style := newNetworkStyle(networkStyleInput{network: network, markers: markers, lineLanes: stationLineLanes(network), scale: 0.01, unit: 1})
			if style.detailed != test.detailed {
				t.Fatalf("detailed = %t, want %t", style.detailed, test.detailed)
			}
			if style.markerRadius != test.marker || style.nodeDots != test.nodeDots {
				t.Errorf("marker radius, node dots = %v, %t, want %v, %t", style.markerRadius, style.nodeDots, test.marker, test.nodeDots)
			}
			if got := style.laneStroke(network.Lanes[0]); got != test.station {
				t.Errorf("station lane stroke = %+v, want %+v", got, test.station)
			}
			if got := style.showArrow(straightGeometry(1)); got != test.arrow {
				t.Errorf("arrow on a 1-pixel lane = %t, want %t", got, test.arrow)
			}
		})
	}
}

func TestStationLineLanes(t *testing.T) {
	t.Parallel()
	lane := func(id, from, to, stationID string) sim.Lane {
		return sim.Lane{ID: id, From: from, To: to, StationID: stationID}
	}
	tests := []struct {
		name  string
		lanes []sim.Lane
		want  []string
	}{
		{
			name: "ring through lane",
			lanes: []sim.Lane{
				lane("link-in", "previous-exit", "entry", ""),
				lane("through", "entry", "exit", "s"),
				lane("link-out", "exit", "next-entry", ""),
				lane("in", "entry", "berth", "s"),
				lane("out", "berth", "exit", "s"),
			},
			want: []string{"through"},
		},
		{
			name: "junction siding",
			lanes: []sim.Lane{
				lane("arrival", "far", "portal-in", ""),
				lane("road-in", "portal-in", "diverge", "s"),
				lane("access-in", "diverge", "entry", "s"),
				lane("through", "entry", "exit", "s"),
				lane("access-out", "exit", "merge", "s"),
				lane("road-out", "merge", "portal-out", "s"),
				lane("departure", "portal-out", "far", ""),
			},
		},
		{
			name: "line end only",
			lanes: []sim.Lane{
				lane("link-in", "previous", "entry", ""),
				lane("through", "entry", "exit", "s"),
			},
		},
		{
			name: "no station lanes",
			lanes: []sim.Lane{
				lane("ab", "a", "b", ""),
				lane("bc", "b", "c", ""),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := stationLineLanes(sim.Network{Lanes: test.lanes})
			if len(got) != len(test.want) {
				t.Fatalf("stationLineLanes = %v, want %v", got, test.want)
			}
			for _, id := range test.want {
				if !got[id] {
					t.Fatalf("stationLineLanes = %v, want %v", got, test.want)
				}
			}
		})
	}
}

// TestShowArrowOnBentLanes checks that the arrow rule uses the length along
// all segments of a lane, not the distance between its ends.
func TestShowArrowOnBentLanes(t *testing.T) {
	t.Parallel()
	style := newNetworkStyle(networkStyleInput{network: denseStyleNetwork(), scale: 0.01, unit: 1})
	tests := []struct {
		name   string
		points []sim.Point
		want   bool
	}{
		{name: "path at the limit", points: []sim.Point{{}, {X: 12}, {X: 12, Y: 12}}, want: true},
		{name: "path below the limit", points: []sim.Point{{}, {X: 12}, {X: 12, Y: 11.9}}, want: false},
		{name: "many segments", points: []sim.Point{{}, {X: 6}, {X: 6, Y: 6}, {X: 12, Y: 6}, {X: 12, Y: 12}}, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			geometry := laneGeometry{count: len(test.points)}
			copy(geometry.points[:], test.points)
			if got := style.showArrow(geometry); got != test.want {
				t.Fatalf("showArrow = %t for a %v-pixel path, want %t", got, geometry.screenLength(), test.want)
			}
		})
	}
}

func TestBerthsExpanded(t *testing.T) {
	t.Parallel()
	twoBerths := func(id string) sim.Station {
		return sim.Station{ID: id, Berths: []sim.Berth{{ID: id + "-1"}, {ID: id + "-2"}}}
	}
	oneBerth := sim.Station{ID: "single", Berths: []sim.Berth{{ID: "single-1"}}}
	tests := []struct {
		name      string
		stations  []sim.Station
		collapsed []string
		want      bool
	}{
		{name: "all collapsed", stations: []sim.Station{twoBerths("a"), twoBerths("b")}, collapsed: []string{"a", "b"}, want: false},
		{name: "one expanded", stations: []sim.Station{twoBerths("a"), twoBerths("b")}, collapsed: []string{"a"}, want: true},
		{name: "single berth stations only", stations: []sim.Station{oneBerth}, want: true},
		{name: "single berth beside collapsed", stations: []sim.Station{oneBerth, twoBerths("a")}, collapsed: []string{"a"}, want: false},
		{name: "no stations", want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			markers := make(map[string]sim.Point)
			for _, id := range test.collapsed {
				markers[id] = sim.Point{}
			}
			if got := berthsExpanded(test.stations, markers); got != test.want {
				t.Fatalf("berthsExpanded = %t, want %t", got, test.want)
			}
		})
	}
}

// TestNetworkStyleKeepsExampleLook checks that the example network draws as
// before at all zoom levels. Its markers, lanes, direction arrows, and node
// dots do not scale with the zoom.
func TestNetworkStyleKeepsExampleLook(t *testing.T) {
	t.Parallel()
	layouts := []layoutInput{
		{outsideWidth: minimumWidth, outsideHeight: minimumHeight, deviceScale: 1},
		{outsideWidth: 1920, outsideHeight: 930, deviceScale: 1.5},
	}
	for _, input := range layouts {
		for _, zoom := range []float64{1, 2, 4, 8, mapMaxZoom} {
			t.Run(fmt.Sprintf("%dx%d at %gx zoom", input.outsideWidth, input.outsideHeight, zoom), func(t *testing.T) {
				t.Parallel()
				game := zoomedGame(sim.Example(), input, zoom)
				unit := game.layout.unit
				style := newNetworkStyle(networkStyleInput{network: game.network, markers: game.collapsedStationMarkers(), lineLanes: game.currentLineLanes(), scale: game.mapScale, unit: unit})
				if !style.detailed || style.markerRadius != 10*unit || style.laneWidth != 5*unit || !style.nodeDots {
					t.Fatalf("style = %+v, want detailed with a 10-unit marker, 5-unit lanes and node dots", style)
				}
				want := laneStroke{width: float32(5 * unit), color: track, antialias: true}
				for _, lane := range game.network.Lanes {
					if got := style.laneStroke(lane); got != want {
						t.Errorf("lane %s stroke = %+v, want %+v", lane.ID, got, want)
					}
					if !style.showArrow(game.laneGeometry(lane, true)) {
						t.Errorf("lane %s has no direction arrow", lane.ID)
					}
				}
				// A lane of any length keeps its direction arrow.
				if !style.showArrow(straightGeometry(1)) {
					t.Error("a 1-pixel lane has no direction arrow")
				}
			})
		}
	}
}

// TestNetworkStyleOnLondon checks the London overview and the berth expansion
// scale through the game state.
func TestNetworkStyleOnLondon(t *testing.T) {
	t.Parallel()
	network := scenarios.London().Network
	input := layoutInput{outsideWidth: minimumWidth, outsideHeight: minimumHeight, deviceScale: 1}

	fit := zoomedGame(network, input, 1)
	markers := fit.collapsedStationMarkers()
	lineLanes := fit.currentLineLanes()
	style := newNetworkStyle(networkStyleInput{network: network, markers: markers, lineLanes: lineLanes, scale: fit.mapScale, unit: fit.layout.unit})
	if len(markers) != len(network.Stations) {
		t.Fatalf("%d of %d stations are collapsed at Fit, want all", len(markers), len(network.Stations))
	}
	// All station lanes are in the station lattice, so all of them dim.
	if len(lineLanes) != 0 {
		t.Fatalf("station line lanes = %v, want none", lineLanes)
	}
	if style.detailed || style.nodeDots {
		t.Fatalf("detailed, node dots = %t, %t at Fit, want false, false", style.detailed, style.nodeDots)
	}
	if want := 150 * fit.mapScale; want < 3 || want > 10 || !closeTo(style.markerRadius, want) {
		t.Fatalf("marker radius = %v, want 150 m on the screen (%v) between 3 and 10 units", style.markerRadius, want)
	}
	if style.laneWidth != 2 || style.collapsedLaneWidth != 1 {
		t.Fatalf("lane width, collapsed lane width = %v, %v at Fit, want 2, 1", style.laneWidth, style.collapsedLaneWidth)
	}

	zoomed := zoomedGame(network, input, mapMaxZoom)
	markers = zoomed.collapsedStationMarkers()
	style = newNetworkStyle(networkStyleInput{network: network, markers: markers, lineLanes: zoomed.currentLineLanes(), scale: zoomed.mapScale, unit: zoomed.layout.unit})
	if len(markers) != 0 || !style.nodeDots {
		t.Fatalf("%d collapsed stations and node dots %t at maximum zoom, want 0 and true", len(markers), style.nodeDots)
	}
	if style.markerRadius != 10 || style.laneWidth != 5 {
		t.Fatalf("marker radius, lane width = %v, %v at maximum zoom, want 10, 5", style.markerRadius, style.laneWidth)
	}
}

// denseStyleNetwork returns a network with more than detailedLanes lanes and
// two stations with two berths each. The first lane joins the berths of
// station a. The second lane is the through lane of station a, and it
// continues the road lanes as in a generated ring. The last lane is a road
// lane.
func denseStyleNetwork() sim.Network {
	network := sim.Network{
		Nodes: []sim.Node{
			{ID: "a1", Position: sim.Point{X: 0, Y: 0}},
			{ID: "a2", Position: sim.Point{X: 75, Y: 0}},
			{ID: "a-entry", Position: sim.Point{X: 0, Y: -100}},
			{ID: "a-exit", Position: sim.Point{X: 75, Y: -100}},
			{ID: "b1", Position: sim.Point{X: 1000, Y: 0}},
			{ID: "b2", Position: sim.Point{X: 1000, Y: 75}},
		},
		Stations: []sim.Station{
			{ID: "a", Berths: []sim.Berth{{ID: "a-1", Node: "a1"}, {ID: "a-2", Node: "a2"}}},
			{ID: "b", Berths: []sim.Berth{{ID: "b-1", Node: "b1"}, {ID: "b-2", Node: "b2"}}},
		},
		Lanes: []sim.Lane{
			{ID: "a-berths", From: "a1", To: "a2", StationID: "a"},
			{ID: "a-through", From: "a-entry", To: "a-exit", StationID: "a", StationRole: sim.StationThroughRole},
		},
	}
	for len(network.Lanes) <= detailedLanes {
		network.Lanes = append(network.Lanes, sim.Lane{ID: fmt.Sprintf("road-%d", len(network.Lanes)), From: "a-exit", To: "a-entry"})
	}
	return network
}

// zoomedGame returns a game that shows the network at the zoom factor over
// the Fit scale, centered on the map viewport.
func zoomedGame(network sim.Network, input layoutInput, zoom float64) *Game {
	game := &Game{network: network, layout: newDisplayLayout(input)}
	game.fitNetwork()
	game.zoomMap(zoom)
	return game
}

// straightGeometry returns a horizontal lane with the screen length.
func straightGeometry(length float64) laneGeometry {
	geometry := laneGeometry{count: 2}
	geometry.points[1] = sim.Point{X: length}
	return geometry
}

func closeTo(a, b float64) bool {
	return math.Abs(a-b) < 1e-6
}
