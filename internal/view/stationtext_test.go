package view

import (
	"fmt"
	"image"
	"maps"
	"math"
	"slices"
	"strings"
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

// TestPlaceStationTextForm checks the rule that picks the long or the short
// form of station text. The long form is 80x20 pixels and the short form is
// 40x10 pixels. A form can go below the ring or left of it. A high away cost
// keeps a form below the ring when that place is free.
func TestPlaceStationTextForm(t *testing.T) {
	t.Parallel()
	viewport := image.Rect(0, 0, 200, 100)
	ring := image.Rect(90, 40, 110, 60)
	long, short := image.Pt(80, 20), image.Pt(40, 10)
	// wide is a horizontal lane at y across the viewport, and tall is a
	// vertical lane at x.
	wide := func(y float64) lanePath { return lanePath{{X: 0, Y: y}, {X: 200, Y: y}} }
	tall := func(x float64) lanePath { return lanePath{{X: x, Y: 0}, {X: x, Y: 100}} }
	// below blocks the place of the long form below the ring.
	below := image.Rect(125, 62, 130, 82)
	tests := []struct {
		name     string
		sizes    []image.Point
		lanes    []lanePath
		padding  int
		slack    float64
		occupied []image.Rectangle
		want     textFormPlace
		wantOK   bool
	}{
		{
			name:  "long form without lanes",
			sizes: []image.Point{long, short},
			want:  textFormPlace{form: 0, area: image.Rect(60, 62, 140, 82)}, wantOK: true,
		},
		{
			name:  "long form clear of a lane beside it",
			sizes: []image.Point{long, short}, lanes: []lanePath{wide(95)},
			want: textFormPlace{form: 0, area: image.Rect(60, 62, 140, 82)}, wantOK: true,
		},
		{
			name:  "long form crosses a lane and the short form does not",
			sizes: []image.Point{long, short}, lanes: []lanePath{wide(78)},
			want: textFormPlace{form: 1, area: image.Rect(80, 62, 120, 72)}, wantOK: true,
		},
		{
			name:  "both forms cross a lane",
			sizes: []image.Point{long, short}, lanes: []lanePath{wide(66)},
			want: textFormPlace{form: 1, area: image.Rect(80, 62, 120, 72), cost: 40}, wantOK: true,
		},
		{
			name:  "a lane in the padding does not count",
			sizes: []image.Point{long, short}, lanes: []lanePath{wide(81)}, padding: 2,
			want: textFormPlace{form: 0, area: image.Rect(60, 62, 140, 82)}, wantOK: true,
		},
		{
			name:  "long form without a free place",
			sizes: []image.Point{long, short}, occupied: []image.Rectangle{below, image.Rect(0, 40, 10, 60)},
			want: textFormPlace{form: 1, area: image.Rect(80, 62, 120, 72)}, wantOK: true,
		},
		{
			name:  "long form covers less of the lanes than the short form",
			sizes: []image.Point{long, short}, lanes: []lanePath{wide(66), tall(20)}, occupied: []image.Rectangle{below},
			want: textFormPlace{form: 0, area: image.Rect(8, 40, 88, 60), cost: 1020}, wantOK: true,
		},
		{
			name:  "long form crosses a lane within the slack",
			sizes: []image.Point{long, short}, lanes: []lanePath{tall(130)}, slack: 25,
			want: textFormPlace{form: 0, area: image.Rect(60, 62, 140, 82), cost: 20}, wantOK: true,
		},
		{
			name:  "long form crosses a lane past the slack",
			sizes: []image.Point{long, short}, lanes: []lanePath{tall(130)}, slack: 15,
			want: textFormPlace{form: 1, area: image.Rect(80, 62, 120, 72)}, wantOK: true,
		},
		{
			name:  "one form crosses a lane",
			sizes: []image.Point{short}, lanes: []lanePath{wide(66)},
			want: textFormPlace{form: 0, area: image.Rect(80, 62, 120, 72), cost: 40}, wantOK: true,
		},
		{
			name:  "no form has a free place",
			sizes: []image.Point{long, short}, occupied: []image.Rectangle{viewport},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, ok := placeStationTextForm(textFormPlacement{
				placement: stationTextPlacement{
					item: ring, sides: []textSide{{0, 1}, {-1, 0}}, away: sim.Point{Y: 1}, gap: 2, padding: test.padding,
					viewport: viewport, occupied: test.occupied, lanes: test.lanes, awayCost: 1000,
				},
				sizes: test.sizes, slack: test.slack,
			})
			want := test.want
			if got.form != want.form || got.area != want.area || math.Abs(got.cost-want.cost) > 1e-9 || ok != test.wantOK {
				t.Fatalf("placeStationTextForm() = %+v, %t, want %+v, %t", got, ok, want, test.wantOK)
			}
		})
	}
}

// TestExpandedStationTextForms checks the lines of the long and the short
// form of the text of a single-berth station and of a station with two
// berths. The short form has a queue line only when pods stop at the
// entrance or the exit. Its size always holds a queue line and the widest
// occupancy line.
func TestExpandedStationTextForms(t *testing.T) {
	t.Parallel()
	// The longest pod ID gives the widest occupancy line of a berth.
	state := sim.Snapshot{Vehicles: []sim.Vehicle{{Pod: sim.Pod{ID: "01"}}, {Pod: sim.Pod{ID: "pod-003"}}}}
	tests := []struct {
		name      string
		station   string
		status    observe.StationMetrics
		wantLong  []string
		wantShort []string
		// wantWidest is the widest occupancy line of the short form.
		wantWidest string
	}{
		{
			name: "single berth", station: "harbor", status: observe.StationMetrics{Free: 1},
			wantLong:  []string{"Harbor", "BERTH 0/1", "0 occupied · 0 reserved empty · 1 free", "In 0 stopped / 0 approaching · Out 0 stopped"},
			wantShort: []string{"Harbor", "BERTH 0/1"}, wantWidest: "DEPARTING pod-003",
		},
		{
			name: "single berth with a queue", station: "harbor", status: observe.StationMetrics{ReservedEmpty: 1, EntranceStopped: 2, ExitStopped: 1},
			wantLong:  []string{"Harbor", "BERTH 0/1", "0 occupied · 1 reserved empty · 0 free", "In 2 stopped / 0 approaching · Out 1 stopped"},
			wantShort: []string{"Harbor", "BERTH 0/1", "In 2 · Out 1"}, wantWidest: "DEPARTING pod-003",
		},
		{
			name: "single berth with an entrance queue", station: "harbor", status: observe.StationMetrics{Free: 1, EntranceStopped: 1},
			wantLong:  []string{"Harbor", "BERTH 0/1", "0 occupied · 0 reserved empty · 1 free", "In 1 stopped / 0 approaching · Out 0 stopped"},
			wantShort: []string{"Harbor", "BERTH 0/1", "In 1 · Out 0"}, wantWidest: "DEPARTING pod-003",
		},
		{
			name: "two berths", station: "parking", status: observe.StationMetrics{Occupied: 1, ReservedEmpty: 1},
			wantLong:  []string{"Parking", "1/2 occupied · 1 reserved empty · 0 free", "In 0 stopped / 0 approaching · Out 0 stopped"},
			wantShort: []string{"Parking", "1/2 occupied"}, wantWidest: "2/2 occupied",
		},
		{
			name: "two berths with a queue", station: "parking", status: observe.StationMetrics{Occupied: 1, Free: 1, EntranceStopped: 2, Approaching: 3, ExitStopped: 1},
			wantLong:  []string{"Parking", "1/2 occupied · 0 reserved empty · 1 free", "In 2 stopped / 3 approaching · Out 1 stopped"},
			wantShort: []string{"Parking", "1/2 occupied", "In 2 · Out 1"}, wantWidest: "2/2 occupied",
		},
		{
			name: "two berths with an exit queue", station: "parking", status: observe.StationMetrics{Free: 2, ExitStopped: 1},
			wantLong:  []string{"Parking", "0/2 occupied · 0 reserved empty · 2 free", "In 0 stopped / 0 approaching · Out 1 stopped"},
			wantShort: []string{"Parking", "0/2 occupied", "In 0 · Out 1"}, wantWidest: "2/2 occupied",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game := journeyNetworkGame(t, sim.Example())
			station, ok := game.network.Station(test.station)
			if !ok {
				t.Fatalf("station %s is not in the example", test.station)
			}
			expanded := game.expandedStationText(expandedStationInput{station: station, status: test.status, state: state})
			block := expanded.station
			if len(station.Berths) == 1 {
				block = expanded.berths[0].text
			}
			if got := lineValues(block.lines); !slices.Equal(got, test.wantLong) {
				t.Errorf("long form = %q, want %q", got, test.wantLong)
			}
			short := block.short
			if got := lineValues(short.lines); !slices.Equal(got, test.wantShort) {
				t.Errorf("short form = %q, want %q", got, test.wantShort)
			}
			if len(short.lines) == 3 && short.lines[2].color != amber {
				t.Errorf("queue line color = %06x, want amber %06x", short.lines[2].color, amber)
			}
			wantSizing := append(slices.Clone(test.wantShort), test.wantWidest, "In 99 · Out 99")
			if got := lineValues(short.sizing); !slices.Equal(got, wantSizing) {
				t.Errorf("short form sizing = %q, want %q", got, wantSizing)
			}
		})
	}
}

// lineValues returns the text of each line.
func lineValues(lines []label) []string {
	var values []string
	for _, line := range lines {
		values = append(values, line.value)
	}
	return values
}

// isShortForm reports whether lines are the short form of station text.
// Each long form has a line with the reserved-empty berths, and a berth
// number has one line.
func isShortForm(lines []label) bool {
	return len(lines) > 1 && !slices.ContainsFunc(lines, func(line label) bool {
		return strings.Contains(line.value, "reserved empty")
	})
}

// TestStationTextKeepsItsPlace checks that the station text of the example
// at Fit keeps its place when pods stop at the entrance or the exit, and
// when a pod arrives at a berth or occupies it. Some text shows its short
// form, whose drawn lines change with these states.
func TestStationTextKeepsItsPlace(t *testing.T) {
	t.Parallel()
	vehicles := []sim.Vehicle{{Pod: sim.Pod{ID: "01"}}, {Pod: sim.Pod{ID: "02"}}}
	states := []struct {
		name   string
		status observe.StationMetrics
		berths []sim.BerthState
	}{
		{name: "no queue"},
		{name: "entrance queue", status: observe.StationMetrics{EntranceStopped: 1}},
		{name: "exit queue", status: observe.StationMetrics{ExitStopped: 1}},
		{name: "arriving pods", status: observe.StationMetrics{ReservedEmpty: 1}, berths: []sim.BerthState{
			{ID: "harbor-1", ReservedBy: "01"}, {ID: "garden-1", ReservedBy: "02"}, {ID: "market-1", ReservedBy: "01"}, {ID: "parking-1", ReservedBy: "02"},
		}},
		{name: "occupied berths", status: observe.StationMetrics{Occupied: 1}, berths: []sim.BerthState{
			{ID: "harbor-1", Occupant: "01"}, {ID: "garden-1", Occupant: "02"}, {ID: "market-1", Occupant: "01"}, {ID: "parking-1", Occupant: "02"},
		}},
	}
	for _, input := range []layoutInput{
		{outsideWidth: 1100, outsideHeight: 760, deviceScale: 1},
		{outsideWidth: 1366, outsideHeight: 610, deviceScale: 1},
	} {
		t.Run(fmt.Sprintf("%dx%d", input.outsideWidth, input.outsideHeight), func(t *testing.T) {
			t.Parallel()
			game := journeyNetworkGame(t, sim.Example())
			game.layoutFor(input)
			game.fitNetwork()
			var want map[string]image.Rectangle
			for _, state := range states {
				_, placed, _ := placedTestTextWith(game, func(station sim.Station) expandedStationInput {
					status := state.status
					status.Free = len(station.Berths) - status.Occupied - status.ReservedEmpty
					return expandedStationInput{station: station, status: status, state: sim.Snapshot{Vehicles: vehicles, Berths: state.berths}}
				})
				areas := make(map[string]image.Rectangle)
				short := 0
				for _, text := range placed {
					areas[text.block.lines[0].value] = text.area
					if isShortForm(text.block.lines) {
						short++
					}
				}
				if short == 0 {
					t.Errorf("%s: no text shows its short form", state.name)
				}
				if want == nil {
					want = areas
					continue
				}
				if !maps.Equal(areas, want) {
					t.Errorf("%s: text areas %v, want %v", state.name, areas, want)
				}
			}
		})
	}
}

// TestStationTextFormFollowsZoom checks that the Parking text of the example
// shows its short form at Fit, where the long form covers the bypass lanes,
// and its long form after a zoom in on Parking.
func TestStationTextFormFollowsZoom(t *testing.T) {
	t.Parallel()
	for _, input := range []layoutInput{
		{outsideWidth: 1100, outsideHeight: 760, deviceScale: 1},
		{outsideWidth: 1366, outsideHeight: 610, deviceScale: 1},
	} {
		t.Run(fmt.Sprintf("%dx%d", input.outsideWidth, input.outsideHeight), func(t *testing.T) {
			t.Parallel()
			for _, zoom := range []struct {
				factor float64
				lines  int
			}{{1, 2}, {3, 3}} {
				game := journeyNetworkGame(t, sim.Example())
				game.layoutFor(input)
				game.fitNetwork()
				game.camera.zoomAt(game.mapPoint(game.stationAnchors()["parking"]), zoom.factor)
				game.syncCamera()
				_, placed, _ := placedTestText(game)
				lines := 0
				for _, text := range placed {
					if text.block.lines[0].value == "Parking" {
						lines = len(text.block.lines)
					}
				}
				if lines != zoom.lines {
					t.Errorf("zoom %g: Parking text has %d lines, want %d", zoom.factor, lines, zoom.lines)
				}
			}
		})
	}
}

// TestStationTextFormOnLondon checks London zoomed in on three stations. At
// 20 times the Fit scale, the long form of most station text covers lanes,
// so the short form shows. At the largest zoom, the long form shows, so the
// full counts of each station stay available.
func TestStationTextFormOnLondon(t *testing.T) {
	t.Parallel()
	network := scenarios.London().Network
	for _, input := range []layoutInput{
		{outsideWidth: 1100, outsideHeight: 760, deviceScale: 1},
		{outsideWidth: 1366, outsideHeight: 610, deviceScale: 1},
	} {
		t.Run(fmt.Sprintf("%dx%d", input.outsideWidth, input.outsideHeight), func(t *testing.T) {
			t.Parallel()
			for _, zoom := range []float64{20, mapMaxZoom} {
				long, short := 0, 0
				for _, stationID := range []string{"parking-west", "940GZZLUGDG", "940GZZLUALD"} {
					game := journeyNetworkGame(t, network)
					game.state = session.State{Epoch: "london"}
					game.layoutFor(input)
					game.fitNetwork()
					game.camera.zoomAt(game.mapPoint(game.stationAnchors()[stationID]), zoom)
					game.syncCamera()
					_, placed, _ := placedTestText(game)
					for _, text := range placed {
						// The station text has three or four lines in the
						// long form and two in the short form. A berth
						// number has one line.
						switch lines := len(text.block.lines); {
						case lines >= 3:
							long++
						case lines == 2:
							short++
						}
					}
				}
				if zoom < mapMaxZoom && short == 0 {
					t.Errorf("zoom %g: %d long and %d short station text blocks, want short blocks", zoom, long, short)
				}
				if zoom == mapMaxZoom && (long == 0 || short > 0) {
					t.Errorf("zoom %g: %d long and %d short station text blocks, want only long blocks", zoom, long, short)
				}
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
	positions := newNetworkIndex(network).positions
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
			if got := stationTextDirection(positions, station); got != test.want {
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
	return placedTestTextWith(game, func(station sim.Station) expandedStationInput {
		return expandedStationInput{station: station, status: observe.StationMetrics{Free: len(station.Berths)}}
	})
}

// placedTestTextWith is placedTestText with the station state that input
// returns for each station.
func placedTestTextWith(game *Game, input func(station sim.Station) expandedStationInput) ([]expandedStationText, []placedStationText, []lanePath) {
	var expanded []expandedStationText
	markers := game.collapsedStationMarkers()
	for _, station := range game.network.Stations {
		if _, ok := markers[station.ID]; ok {
			continue
		}
		expanded = append(expanded, game.expandedStationText(input(station)))
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
		// text block covers. Each limit is the largest cover measured in
		// the window, rounded up to the next 10 units. In the 1100x760
		// window, each place of the Parking text covers a lane or other
		// text, also in the short form. Map labels keep their CSS size, so
		// in the short 1366x610 window the text takes a larger part of the
		// map. There, the short form of the Parking text goes above its
		// berths and crosses the bypass lanes. In the larger windows, a
		// lane crosses a corner of the long form of the Garden text. This
		// length is in the slack of placeStationTextForm.
		maxCover float64
		// below is true when the Harbor and Market text is below the ring,
		// away from the siding. The Garden text is never below the ring.
		below bool
	}{
		{1100, 760, 1, 70, true}, {1366, 610, 1, 90, false}, {1366, 610, 2, 90, false},
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
		// The Parking text is above its berths in each window.
		var parkingRings image.Rectangle
		for _, berth := range expanded[3].berths {
			parkingRings = parkingRings.Union(berth.ring)
		}
		if area := areas["Parking"]; area.Max.Y > parkingRings.Min.Y {
			t.Errorf("window %dx%d@%g: Parking text at %v is not above its berths at %v", size.width, size.height, size.scale, area, parkingRings)
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
	over := label{x: float64(area.Min.X), y: float64(area.Min.Y), size: 11, value: "P1", color: foreground, mapLabel: true}
	game.selected = 1
	got := game.clearPodLabels([]label{over, over}, appendStationTextAreas(nil, placed))
	if got[0].value != "" || got[1].value != "P1" {
		t.Errorf("clearPodLabels() = %q and %q, want the first label cleared and the selected label kept", got[0].value, got[1].value)
	}
}

// TestStationTextSize checks the size of a text block in a window of normal
// size and in a short window. The lines keep their CSS size in the short
// window, and the 9 pixel queue line is 10 CSS pixels. The padding follows
// the display unit.
func TestStationTextSize(t *testing.T) {
	t.Parallel()
	for _, input := range []layoutInput{
		{outsideWidth: 1920, outsideHeight: 1080, deviceScale: 2},
		{outsideWidth: 1366, outsideHeight: 610, deviceScale: 1},
		{outsideWidth: 1366, outsideHeight: 610, deviceScale: 2},
	} {
		t.Run(fmt.Sprintf("%dx%d@%g", input.outsideWidth, input.outsideHeight, input.deviceScale), func(t *testing.T) {
			t.Parallel()
			game := journeyNetworkGame(t, sim.Example())
			game.layoutFor(input)
			scale := input.deviceScale
			queue := "In 0 stopped / 0 approaching · Out 0 stopped"
			block := stationTextBlock{lines: []label{
				{size: 16, value: "Parking", mapLabel: true},
				{y: 23 * scale, size: 9, value: queue, mapLabel: true},
			}}
			nameWidth, _ := text.Measure("Parking", &text.GoTextFace{Source: game.font, Size: 16 * scale}, 0)
			queueWidth, queueHeight := text.Measure(queue, &text.GoTextFace{Source: game.font, Size: 10 * scale}, 0)
			padding := 2 * stationTextPadding * game.layout.unit
			want := image.Pt(int(math.Ceil(max(nameWidth, queueWidth)+padding)), int(math.Ceil(23*scale+queueHeight+padding)))
			if got := game.stationTextSize(block.lines); got != want {
				t.Fatalf("stationTextSize() = %v, want %v", got, want)
			}
		})
	}
}

// TestStationTextLinesDoNotOverlap checks that the lines of the long and the
// short form of the station text do not overlap in a short window and in a
// window with a high device scale. The line spacing and the map label size
// are both in CSS pixels. It also checks that each line has the map label
// size, at least 10 CSS pixels. Pods stop at each entrance and exit, so the
// short form has its queue line.
func TestStationTextLinesDoNotOverlap(t *testing.T) {
	t.Parallel()
	for _, input := range []layoutInput{
		{outsideWidth: 1100, outsideHeight: 760, deviceScale: 1},
		{outsideWidth: 1366, outsideHeight: 610, deviceScale: 1},
		{outsideWidth: 1366, outsideHeight: 610, deviceScale: 2},
		{outsideWidth: 800, outsideHeight: 560, deviceScale: 1.5},
	} {
		t.Run(fmt.Sprintf("%dx%d@%g", input.outsideWidth, input.outsideHeight, input.deviceScale), func(t *testing.T) {
			t.Parallel()
			game := journeyNetworkGame(t, sim.Example())
			game.layoutFor(input)
			game.fitNetwork()
			var forms []textForm
			for _, station := range game.network.Stations {
				expanded := game.expandedStationText(expandedStationInput{
					station: station, status: observe.StationMetrics{Free: len(station.Berths), EntranceStopped: 2, ExitStopped: 1},
				})
				forms = append(forms, expanded.station.forms()...)
				for _, berth := range expanded.berths {
					forms = append(forms, berth.text.forms()...)
				}
			}
			checked := 0
			for _, form := range forms {
				lines := form.lines
				for _, line := range lines {
					if got, want := game.labelFace(line).Size, max(line.size, 10)*input.deviceScale; math.Abs(got-want) > 1e-9 {
						t.Errorf("line %q has size %g, want %g", line.value, got, want)
					}
				}
				for index := 1; index < len(lines); index++ {
					above, line := lines[index-1], lines[index]
					_, height := text.Measure(above.value, game.labelFace(above), 0)
					if above.y+height > line.y {
						t.Errorf("line %q ends at %g, below the top of line %q at %g", above.value, above.y+height, line.value, line.y)
					}
					checked++
				}
			}
			// Three single-berth stations with four lines in the long
			// form, and Parking with three lines. Each short form has
			// three lines.
			if want := 3*3 + 2 + 4*2; checked != want {
				t.Fatalf("checked %d pairs of lines, want %d", checked, want)
			}
		})
	}
}
