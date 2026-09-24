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

func TestNewScaleBar(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		scale  float64
		unit   float64
		meters float64
		label  string
	}{
		{name: "whole country", scale: 80.0 / 3_000_000, unit: 1, meters: 2_000_000, label: "2000 km"},
		{name: "London at fit", scale: 0.0244, unit: 1, meters: 2000, label: "2 km"},
		{name: "exactly 1 km", scale: 0.08, unit: 1, meters: 1000, label: "1 km"},
		{name: "rounding below 1 km", scale: 0.14, unit: 1.75, meters: 1000, label: "1 km"},
		{name: "rounding below 500 m", scale: 0.28, unit: 1.75, meters: 500, label: "500 m"},
		{name: "just below 1 km", scale: 0.0801, unit: 1, meters: 500, label: "500 m"},
		{name: "exactly 5 km", scale: 0.016, unit: 1, meters: 5000, label: "5 km"},
		{name: "10 km", scale: 0.005, unit: 1, meters: 10000, label: "10 km"},
		{name: "example network", scale: 0.29, unit: 1, meters: 200, label: "200 m"},
		{name: "high density display", scale: 0.58, unit: 2, meters: 200, label: "200 m"},
		{name: "zoomed in", scale: 3.2, unit: 1, meters: 20, label: "20 m"},
		{name: "exactly 1 m", scale: 80, unit: 1, meters: 1, label: "1 m"},
		{name: "below 1 m", scale: 150, unit: 1, meters: 0.5, label: "0.5 m"},
		{name: "far below 1 m", scale: 5000, unit: 1, meters: 0.01, label: "0.01 m"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			bar, ok := newScaleBar(test.scale, test.unit)
			if !ok {
				t.Fatalf("newScaleBar(%v, %v) returned false", test.scale, test.unit)
			}
			if bar.meters != test.meters || bar.label != test.label {
				t.Fatalf("newScaleBar(%v, %v) = %v (%q), want %v (%q)", test.scale, test.unit, bar.meters, bar.label, test.meters, test.label)
			}
			if want := test.meters * test.scale / test.unit; math.Abs(bar.length-want) > 1e-9 {
				t.Fatalf("length = %v, want %v", bar.length, want)
			}
			if bar.length > scaleBarMaximum+1e-6 || bar.length <= scaleBarMaximum/2.5 {
				t.Fatalf("length = %v units, want more than %v and at most %v", bar.length, scaleBarMaximum/2.5, scaleBarMaximum)
			}
		})
	}
}

func TestNewScaleBarSteps(t *testing.T) {
	t.Parallel()
	for exponent := -6; exponent <= 3; exponent++ {
		for step := range 100 {
			scale := math.Pow(10, float64(exponent)) * (1 + float64(step)/10)
			bar, ok := newScaleBar(scale, 1.5)
			if !ok {
				t.Fatalf("newScaleBar(%v, 1.5) returned false", scale)
			}
			if bar.length > scaleBarMaximum+1e-6 || bar.length <= scaleBarMaximum/2.5 {
				t.Fatalf("newScaleBar(%v, 1.5) length = %v units, want more than %v and at most %v", scale, bar.length, scaleBarMaximum/2.5, scaleBarMaximum)
			}
			power := math.Pow(10, math.Floor(math.Log10(bar.meters)+1e-9))
			if lead := math.Round(bar.meters / power); lead != 1 && lead != 2 && lead != 5 || math.Abs(bar.meters/power-lead) > 1e-9 {
				t.Fatalf("newScaleBar(%v, 1.5) meters = %v, want 1, 2 or 5 times a power of ten", scale, bar.meters)
			}
		}
	}
}

func TestNewScaleBarInvalid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		scale float64
		unit  float64
	}{
		{name: "zero scale", scale: 0, unit: 1},
		{name: "negative scale", scale: -1, unit: 1},
		{name: "infinite scale", scale: math.Inf(1), unit: 1},
		{name: "NaN scale", scale: math.NaN(), unit: 1},
		{name: "zero unit", scale: 1, unit: 0},
		{name: "infinite unit", scale: 1, unit: math.Inf(1)},
		{name: "distance underflow", scale: 1e300, unit: 1e-300},
		{name: "distance overflow", scale: 1e-320, unit: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if bar, ok := newScaleBar(test.scale, test.unit); ok {
				t.Fatalf("newScaleBar(%v, %v) = %+v, true, want false", test.scale, test.unit, bar)
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
		name         string
		scale        float64
		unit         float64
		collapsed    []string
		marker       float64
		lane         float64
		station      laneStroke
		stationArrow uint32
		nodeDots     bool
		shortArrow   float64
	}{
		{
			name: "overview", scale: 0.01, unit: 1, collapsed: []string{"a", "b"},
			marker: 3, lane: 2, station: laneStroke{width: 1, color: collapsedStationTrack}, stationArrow: track, nodeDots: false, shortArrow: 24,
		},
		{
			name: "overview in units", scale: 0.01, unit: 2, collapsed: []string{"a", "b"},
			marker: 6, lane: 4, station: laneStroke{width: 2, color: collapsedStationTrack}, stationArrow: track, nodeDots: false, shortArrow: 48,
		},
		{
			name: "lanes between limits", scale: 0.12, unit: 1, collapsed: []string{"a", "b"},
			marker: 10, lane: 3.6, station: laneStroke{width: 1.8, color: collapsedStationTrack}, stationArrow: track, nodeDots: false, shortArrow: 24,
		},
		{
			name: "one station expanded", scale: 0.2, unit: 1, collapsed: []string{"b"},
			marker: 10, lane: 5, station: laneStroke{width: 5, color: track}, stationArrow: muted, nodeDots: true, shortArrow: 24,
		},
		{
			name: "all stations expanded", scale: 1, unit: 1,
			marker: 10, lane: 5, station: laneStroke{width: 5, color: track}, stationArrow: muted, nodeDots: true, shortArrow: 24,
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
			if got := style.laneArrowColor(stationLane); got != test.stationArrow {
				t.Errorf("station lane arrow color = %#06x, want %#06x", got, test.stationArrow)
			}
			for _, lane := range []sim.Lane{throughLane, roadLane} {
				if got := style.laneStroke(lane); !closeTo(float64(got.width), test.lane) || got.color != track || got.antialias {
					t.Errorf("lane %s stroke = %+v, want width %v in track", lane.ID, got, test.lane)
				}
				if got := style.laneArrowColor(lane); got != muted {
					t.Errorf("lane %s arrow color = %#06x, want muted %#06x", lane.ID, got, muted)
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
		name         string
		lanes        int
		detailed     bool
		marker       float64
		station      laneStroke
		stationArrow uint32
		nodeDots     bool
		arrow        bool
	}{
		{
			name: "at the limit", lanes: detailedLanes, detailed: true,
			marker: 10, station: laneStroke{width: 5, color: track, antialias: true}, stationArrow: muted, nodeDots: true, arrow: true,
		},
		{
			name: "above the limit", lanes: detailedLanes + 1, detailed: false,
			marker: 3, station: laneStroke{width: 1, color: collapsedStationTrack}, stationArrow: track, nodeDots: false, arrow: false,
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
			if got := style.laneArrowColor(network.Lanes[0]); got != test.stationArrow {
				t.Errorf("station lane arrow color = %#06x, want %#06x", got, test.stationArrow)
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
// dots do not scale with the zoom. All its direction arrows are muted, and
// no arrow crowds another.
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
					if got := style.laneArrowColor(lane); got != muted {
						t.Errorf("lane %s arrow color = %#06x, want muted %#06x", lane.ID, got, muted)
					}
				}
				arrows := baseLaneArrows(game, style)
				if spaced := style.spacedArrows(arrows); len(spaced) != len(game.network.Lanes) {
					t.Errorf("%d of %d direction arrows stay after spacing, want all", len(spaced), len(game.network.Lanes))
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

func TestArrowLegEnds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		arrow arrow
		ends  [2]sim.Point
		ok    bool
	}{
		{
			name:  "lane arrow to the right",
			arrow: arrow{tip: sim.Point{X: 6}, direction: sim.Point{X: 10}, size: laneArrowSize, unit: 1},
			ends:  [2]sim.Point{{X: 2, Y: 2}, {X: 2, Y: -2}}, ok: true,
		},
		{
			name:  "lane arrow down the screen",
			arrow: arrow{tip: sim.Point{Y: 6}, direction: sim.Point{Y: 0.5}, size: laneArrowSize, unit: 1},
			ends:  [2]sim.Point{{X: -2, Y: 2}, {X: 2, Y: 2}}, ok: true,
		},
		{
			name:  "diagonal lane arrow",
			arrow: arrow{tip: sim.Point{X: 1.8, Y: 2.4}, direction: sim.Point{X: 3, Y: 4}, size: laneArrowSize, unit: 1},
			ends:  [2]sim.Point{{X: -2.2, Y: 0.4}, {X: 1, Y: -2}}, ok: true,
		},
		{
			name:  "route arrow to the left in units",
			arrow: arrow{tip: sim.Point{X: 4}, direction: sim.Point{X: -10}, size: routeArrowSize, unit: 2},
			ends:  [2]sim.Point{{X: 18, Y: -8}, {X: 18, Y: 8}}, ok: true,
		},
		{
			name:  "no direction",
			arrow: arrow{tip: sim.Point{X: 5, Y: 5}, size: laneArrowSize, unit: 1},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ends, ok := test.arrow.legEnds()
			if ok != test.ok {
				t.Fatalf("legEnds ok = %t, want %t", ok, test.ok)
			}
			if ok && (!closeToPoint(ends[0], test.ends[0]) || !closeToPoint(ends[1], test.ends[1])) {
				t.Fatalf("legEnds = %v, want %v", ends, test.ends)
			}
		})
	}
}

func TestLaneGeometryAlong(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		points    []sim.Point
		fraction  float64
		point     sim.Point
		direction sim.Point
	}{
		{name: "straight lane", points: []sim.Point{{}, {X: 100}}, fraction: .61, point: sim.Point{X: 61}, direction: sim.Point{X: 100}},
		{name: "second segment", points: []sim.Point{{}, {X: 10}, {X: 10, Y: 10}}, fraction: .75, point: sim.Point{X: 10, Y: 5}, direction: sim.Point{Y: 10}},
		{name: "segment end", points: []sim.Point{{}, {X: 10}, {X: 10, Y: 10}}, fraction: .5, point: sim.Point{X: 10}, direction: sim.Point{X: 10}},
		{name: "empty first segment", points: []sim.Point{{}, {}, {Y: 20}}, fraction: .5, point: sim.Point{Y: 10}, direction: sim.Point{Y: 20}},
		{name: "no screen length", points: []sim.Point{{X: 5, Y: 5}, {X: 5, Y: 5}}, fraction: .61, point: sim.Point{X: 5, Y: 5}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			geometry := laneGeometry{count: len(test.points)}
			copy(geometry.points[:], test.points)
			point, direction := geometry.along(test.fraction)
			if !closeToPoint(point, test.point) || !closeToPoint(direction, test.direction) {
				t.Fatalf("along(%v) = %v, %v, want %v, %v", test.fraction, point, direction, test.point, test.direction)
			}
		})
	}
}

// TestLaneArrowColors checks that each direction arrow in the network base
// layer is lighter than its lane. On a lane in the line style, the contrast
// is at least 3:1.
func TestLaneArrowColors(t *testing.T) {
	t.Parallel()
	example := sim.Example()
	exampleStyle := newNetworkStyle(networkStyleInput{network: example, scale: 1, unit: 1})
	dense := denseStyleNetwork()
	markers := map[string]sim.Point{"a": {}, "b": {}}
	denseStyle := newNetworkStyle(networkStyleInput{network: dense, markers: markers, lineLanes: stationLineLanes(dense), scale: 0.01, unit: 1})
	tests := []struct {
		name        string
		style       networkStyle
		lane        sim.Lane
		want        uint32
		minContrast float64
	}{
		{name: "example lane", style: exampleStyle, lane: example.Lanes[0], want: muted, minContrast: 3},
		{name: "dense road lane", style: denseStyle, lane: dense.Lanes[len(dense.Lanes)-1], want: muted, minContrast: 3},
		{name: "dense station lane in a line", style: denseStyle, lane: dense.Lanes[1], want: muted, minContrast: 3},
		{name: "collapsed station lane", style: denseStyle, lane: dense.Lanes[0], want: track, minContrast: 1.3},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			lane := test.style.laneStroke(test.lane).color
			a, ok := test.style.laneArrow(test.lane, straightGeometry(100))
			if !ok {
				t.Fatal("a 100-pixel lane has no direction arrow")
			}
			if a.size != laneArrowSize {
				t.Fatalf("arrow size = %+v, want %+v", a.size, laneArrowSize)
			}
			got := a.color
			if got != test.want {
				t.Fatalf("arrow color = %#06x, want %#06x", got, test.want)
			}
			if relativeLuminance(got) <= relativeLuminance(lane) {
				t.Fatalf("arrow %#06x is not lighter than lane %#06x", got, lane)
			}
			if ratio := contrastRatio(got, lane); ratio < test.minContrast {
				t.Fatalf("contrast of arrow %#06x on lane %#06x = %.2f:1, want at least %v:1", got, lane, ratio, test.minContrast)
			}
		})
	}
}

// TestLaneArrowsStayOnLanes checks that the tip and the leg ends of each
// direction arrow are on its 5-unit lane at all zoom levels, also on a curved
// lane.
func TestLaneArrowsStayOnLanes(t *testing.T) {
	t.Parallel()
	network := sim.Example()
	network.Lanes = append(network.Lanes, sim.Lane{ID: "curve", From: "harbor-exit", To: "branch", Control: &sim.Point{X: 250, Y: 200}})
	layouts := []layoutInput{
		{outsideWidth: minimumWidth, outsideHeight: minimumHeight, deviceScale: 1},
		{outsideWidth: 1920, outsideHeight: 930, deviceScale: 1.5},
	}
	for _, input := range layouts {
		for _, zoom := range []float64{1, 4, 12, mapMaxZoom} {
			t.Run(fmt.Sprintf("%dx%d at %gx zoom", input.outsideWidth, input.outsideHeight, zoom), func(t *testing.T) {
				t.Parallel()
				game := zoomedGame(network, input, zoom)
				unit := game.layout.unit
				style := newNetworkStyle(networkStyleInput{network: network, markers: game.collapsedStationMarkers(), lineLanes: game.currentLineLanes(), scale: game.mapScale, unit: unit})
				for _, lane := range network.Lanes {
					geometry := game.laneGeometry(lane, style.detailed)
					a, ok := style.laneArrow(lane, geometry)
					if !ok {
						t.Fatalf("lane %s has no direction arrow", lane.ID)
					}
					if a.color != muted || a.size != laneArrowSize || a.unit != unit || !a.antialias {
						t.Errorf("lane %s arrow = %+v, want muted %#06x, size %+v, unit %v, and antialias", lane.ID, a, muted, laneArrowSize, unit)
					}
					// On a straight lane, the tip is at 61 percent of the lane.
					if want := game.mapPoint(network.Position(lane, .61*network.Length(lane))); lane.Control == nil && !closeToPoint(a.tip, want) {
						t.Errorf("lane %s arrow tip = %v, want %v", lane.ID, a.tip, want)
					}
					ends, ok := a.legEnds()
					if !ok {
						t.Fatalf("lane %s arrow has no direction", lane.ID)
					}
					halfWidth := float64(style.laneStroke(lane).width) / 2
					for _, point := range []sim.Point{a.tip, ends[0], ends[1]} {
						if distance := polylineDistance(point, geometry); distance > halfWidth {
							t.Errorf("lane %s arrow point %v is %.2f pixels from the lane center, want at most %v", lane.ID, point, distance, halfWidth)
						}
					}
				}
			})
		}
	}
}

func TestSpacedArrows(t *testing.T) {
	t.Parallel()
	right, down := sim.Point{X: 1}, sim.Point{Y: 1}
	at := func(x, y float64, direction sim.Point) arrow {
		return arrow{tip: sim.Point{X: x, Y: y}, direction: direction}
	}
	tests := []struct {
		name   string
		unit   float64
		arrows []arrow
		want   []arrow
	}{
		{
			name:   "parallel arrows closer than the gap",
			unit:   1,
			arrows: []arrow{at(0, 0, right), at(0, 5.9, right)},
			want:   []arrow{at(0, 0, right)},
		},
		{
			name:   "parallel arrows at the gap",
			unit:   1,
			arrows: []arrow{at(0, 0, right), at(0, 6, right)},
			want:   []arrow{at(0, 0, right), at(0, 6, right)},
		},
		{
			name:   "gap in units",
			unit:   2,
			arrows: []arrow{at(0, 0, right), at(11.9, 0, right), at(24, 0, right)},
			want:   []arrow{at(0, 0, right), at(24, 0, right)},
		},
		{
			name:   "opposite arrows",
			unit:   1,
			arrows: []arrow{at(0, 0, right), at(1, 0, sim.Point{X: -1})},
			want:   []arrow{at(0, 0, right), at(1, 0, sim.Point{X: -1})},
		},
		{
			name:   "crossing arrows",
			unit:   1,
			arrows: []arrow{at(0, 0, right), at(1, 1, down)},
			want:   []arrow{at(0, 0, right), at(1, 1, down)},
		},
		{
			name:   "arrows 40 degrees apart",
			unit:   1,
			arrows: []arrow{at(0, 0, right), at(1, 1, sim.Point{X: math.Cos(40 * math.Pi / 180), Y: math.Sin(40 * math.Pi / 180)})},
			want:   []arrow{at(0, 0, right)},
		},
		{
			name:   "arrows 50 degrees apart",
			unit:   1,
			arrows: []arrow{at(0, 0, right), at(1, 1, sim.Point{X: math.Cos(50 * math.Pi / 180), Y: math.Sin(50 * math.Pi / 180)})},
			want:   []arrow{at(0, 0, right), at(1, 1, sim.Point{X: math.Cos(50 * math.Pi / 180), Y: math.Sin(50 * math.Pi / 180)})},
		},
		{
			name:   "neighbor cells at negative coordinates",
			unit:   1,
			arrows: []arrow{at(-0.5, -0.5, down), at(0.5, 0.5, down), at(-6.4, -0.5, down), at(-0.5, 5.4, down)},
			want:   []arrow{at(-0.5, -0.5, down)},
		},
		{
			name:   "only kept arrows crowd",
			unit:   1,
			arrows: []arrow{at(0, 0, right), at(4, 0, right), at(8, 0, right)},
			want:   []arrow{at(0, 0, right), at(8, 0, right)},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			style := networkStyle{unit: test.unit}
			got := style.spacedArrows(test.arrows)
			if len(got) != len(test.want) {
				t.Fatalf("spacedArrows = %v, want %v", got, test.want)
			}
			for i := range got {
				if !closeToPoint(got[i].tip, test.want[i].tip) || !closeToPoint(got[i].direction, test.want[i].direction) {
					t.Fatalf("spacedArrows = %v, want %v", got, test.want)
				}
			}
		})
	}
}

// TestSpacedArrowsOnPresets checks the spaced direction arrows of generated
// networks at Fit. The berth lanes of a collapsed station lie side by side,
// so some of their arrows go. No two arrows that stay crowd each other.
func TestSpacedArrowsOnPresets(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		network sim.Network
	}{
		{name: "small", network: scenarios.Small().Network},
		{name: "parking-constrained", network: scenarios.ParkingConstrained().Network},
		{name: "busy", network: scenarios.Busy().Network},
	}
	input := layoutInput{outsideWidth: minimumWidth, outsideHeight: minimumHeight, deviceScale: 1}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game := zoomedGame(test.network, input, 1)
			style := newNetworkStyle(networkStyleInput{network: test.network, markers: game.collapsedStationMarkers(), lineLanes: game.currentLineLanes(), scale: game.mapScale, unit: game.layout.unit})
			arrows := baseLaneArrows(game, style)
			spaced := style.spacedArrows(arrows)
			if len(spaced) == len(arrows) {
				t.Fatalf("all %d direction arrows stay at Fit, want fewer", len(arrows))
			}
			gap := laneArrowGap * style.unit
			for i, a := range spaced {
				for _, other := range spaced[i+1:] {
					if a.crowds(other, gap) {
						t.Fatalf("arrow at %v crowds arrow at %v", other.tip, a.tip)
					}
				}
			}
		})
	}
}

// baseLaneArrows returns the direction arrows of the network base layer
// before spacing, in lane order.
func baseLaneArrows(game *Game, style networkStyle) []arrow {
	var arrows []arrow
	for _, lane := range game.network.Lanes {
		if a, ok := style.laneArrow(lane, game.laneGeometry(lane, style.detailed)); ok {
			arrows = append(arrows, a)
		}
	}
	return arrows
}

// polylineDistance returns the distance from a point to the nearest segment
// of the lane geometry.
func polylineDistance(point sim.Point, geometry laneGeometry) float64 {
	nearest := math.Inf(1)
	for i := 1; i < geometry.count; i++ {
		from, to := geometry.points[i-1], geometry.points[i]
		dx, dy := to.X-from.X, to.Y-from.Y
		t := 0.0
		if squared := dx*dx + dy*dy; squared > 0 {
			t = min(1, max(0, ((point.X-from.X)*dx+(point.Y-from.Y)*dy)/squared))
		}
		nearest = min(nearest, math.Hypot(point.X-from.X-t*dx, point.Y-from.Y-t*dy))
	}
	return nearest
}

// contrastRatio returns the WCAG contrast ratio of two colors.
func contrastRatio(a, b uint32) float64 {
	lighter, darker := max(relativeLuminance(a), relativeLuminance(b)), min(relativeLuminance(a), relativeLuminance(b))
	return (lighter + 0.05) / (darker + 0.05)
}

// relativeLuminance returns the WCAG relative luminance of a color.
func relativeLuminance(color uint32) float64 {
	linear := linearRGB(color)
	return 0.2126*linear[0] + 0.7152*linear[1] + 0.0722*linear[2]
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

func closeToPoint(a, b sim.Point) bool {
	return closeTo(a.X, b.X) && closeTo(a.Y, b.Y)
}
