package scenarios

import (
	"math"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// stationOffsets returns the position of point in the frame of shape: the
// distance along the station axis and the distance out from the center.
func stationOffsets(shape londonStationShape, point sim.Point) (along, out float64) {
	frame := shape.frame()
	relative := sim.Point{X: point.X - frame.origin.X, Y: point.Y - frame.origin.Y}
	return relative.X*frame.along.X + relative.Y*frame.along.Y, relative.X*frame.outward.X + relative.Y*frame.outward.Y
}

func TestLondonStationScale(t *testing.T) {
	type offset struct{ along, out float64 }
	tests := []struct {
		name  string
		shape londonStationShape
		// want holds the diverge, merge, entry, exit, and each berth row as
		// arrival, berth, and departure.
		want []offset
		area [2]float64 // the near and far distance of the area
	}{
		{
			name:  "scale 1",
			shape: londonStationShape{berths: 2, pitch: 75, scale: 1, side: 100},
			want:  []offset{{-30, 80}, {30, 80}, {-100, 200}, {100, 200}, {-100, 290}, {0, 290}, {100, 290}, {-100, 365}, {0, 365}, {100, 365}},
			area:  [2]float64{80, 385},
		},
		{
			name:  "zero scale and side are scale 1",
			shape: londonStationShape{berths: 2, pitch: 75},
			want:  []offset{{-30, 80}, {30, 80}, {-100, 200}, {100, 200}, {-100, 290}, {0, 290}, {100, 290}, {-100, 365}, {0, 365}, {100, 365}},
			area:  [2]float64{80, 385},
		},
		{
			name:  "half scale keeps the throat",
			shape: londonStationShape{berths: 2, pitch: 75, scale: 0.5, side: 50},
			want:  []offset{{-30, 80}, {30, 80}, {-50, 140}, {50, 140}, {-50, 185}, {0, 185}, {50, 185}, {-50, 222.5}, {0, 222.5}, {50, 222.5}},
			area:  [2]float64{80, 242.5},
		},
		{
			name:  "zero side follows the scale",
			shape: londonStationShape{berths: 1, pitch: 75, scale: 0.4},
			want:  []offset{{-30, 80}, {30, 80}, {-40, 128}, {40, 128}, {-40, 164}, {0, 164}, {40, 164}},
			area:  [2]float64{80, 184},
		},
		{
			name:  "tight ignores the pitch, the scale, and the side",
			shape: londonStationShape{berths: 2, pitch: 75, scale: 0.5, side: 50, tight: true},
			want:  []offset{{-30, 80}, {30, 80}, {-25, 105}, {25, 105}, {-25, 130}, {0, 130}, {25, 130}, {-25, 155}, {0, 155}, {25, 155}},
			area:  [2]float64{80, 175},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.shape.center, test.shape.direction = sim.Point{X: 300, Y: -200}, 0.7
			nodes := test.shape.nodes()
			points := []sim.Point{nodes.diverge, nodes.merge, nodes.entry, nodes.exit}
			for _, berth := range nodes.berths {
				points = append(points, berth.arrival, berth.berth, berth.departure)
			}
			if len(points) != len(test.want) {
				t.Fatalf("got %d points, want %d", len(points), len(test.want))
			}
			for i, point := range points {
				along, out := stationOffsets(test.shape, point)
				if math.Abs(along-test.want[i].along) > 1e-9 || math.Abs(out-test.want[i].out) > 1e-9 {
					t.Fatalf("point %d at (%v, %v), want %+v", i, along, out, test.want[i])
				}
			}
			area := test.shape.area()
			_, near := stationOffsets(test.shape, area[0])
			_, far := stationOffsets(test.shape, area[2])
			side, _ := stationOffsets(test.shape, area[1])
			if math.Abs(near-test.area[0]) > 1e-9 || math.Abs(far-test.area[1]) > 1e-9 || math.Abs(side-test.want[3].along) > 1e-9 {
				t.Fatalf("area from %v to %v, side %v; want %v, side %v", near, far, side, test.area, test.want[3].along)
			}
			if reach := test.shape.reach(); math.Abs(reach-math.Hypot(test.area[1], test.want[3].along)) > 1e-9 {
				t.Fatalf("reach = %v", reach)
			}
		})
	}
}

// The arrival and departure lanes of a Compact station stay outside the
// Compact bays, whose entry and exit are londonCompactSide from their
// middle.
func TestLondonCompactSide(t *testing.T) {
	capacity := londonCapacity{scale: 0.4, compact: []bool{false, true}}
	if side := capacity.side(0); math.Abs(side-40) > 1e-9 {
		t.Fatalf("side of a station = %v, want 40", side)
	}
	if side := capacity.side(1); side != londonCompactSide {
		t.Fatalf("side of a Compact station = %v, want %v", side, londonCompactSide)
	}
	layout := compactStation(nil, "", "station")
	if half := (layout.nodes[3].Position.X - layout.nodes[0].Position.X) / 2; math.Abs(half-londonCompactSide) > 1e-9 {
		t.Fatalf("Compact entry and exit are %v from their middle, want %v", half, londonCompactSide)
	}
}

func TestLondonStationScaleOptions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		scale float64
		err   string
	}{
		{"zero", 0, "station scale must be greater than 0 and at most 1"},
		{"above 1", 1.5, "station scale must be greater than 0 and at most 1"},
		{"not a number", math.NaN(), "station scale must be greater than 0 and at most 1"},
		{"scaled pitch below the floor", 0.3, "with the station scale, the berth pitch must be a finite distance of at least 25 meters"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			options := DefaultLondonCentralOptions()
			options.StationScale = test.scale
			if _, err := LondonCentralWith(options); err == nil || err.Error() != test.err {
				t.Fatalf("LondonCentralWith error = %v, want %q", err, test.err)
			}
		})
	}
	t.Run("scale 1 is a custom capacity", func(t *testing.T) {
		t.Parallel()
		options := DefaultLondonCentralOptions()
		options.StationScale = 1
		config, err := LondonCentralWith(options)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(config.Name, CustomCapacitySuffix) {
			t.Fatalf("project name %q has no custom capacity suffix", config.Name)
		}
	})
	t.Run("default shapes", func(t *testing.T) {
		t.Parallel()
		defaults := DefaultLondonCentralOptions()
		if defaults.StationScale != 0.5 || defaults.WideStations {
			t.Fatalf("default station scale = %v, wide stations %v, want 0.5 and tight stations", defaults.StationScale, defaults.WideStations)
		}
		wideOptions := DefaultLondonCentralOptions()
		wideOptions.WideStations = true
		wide, err := LondonCentralWith(wideOptions)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(wide.Name, CustomCapacitySuffix) {
			t.Fatalf("project name %q has no custom capacity suffix", wide.Name)
		}
		distance := func(config project.Config, from, to string) float64 {
			t.Helper()
			a, aOK := config.Network.Node(from)
			b, bOK := config.Network.Node(to)
			if !aOK || !bOK {
				t.Fatalf("no node %s or %s", from, to)
			}
			return math.Hypot(b.Position.X-a.Position.X, b.Position.Y-a.Position.Y)
		}
		for _, test := range []struct {
			name   string
			config project.Config
			// entry, first, and pitch are the distances from the diverge to
			// the entry, from the entry to the first berth, and from the
			// first berth to the second berth of Baker Street, which has
			// berth rows.
			entry, first, pitch float64
		}{
			// The tight entry is 25 m past the throat and 5 m nearer to the
			// axis, and its first berth is 25 m past the entry and 25 m
			// beside it.
			{name: "tight", config: LondonCentral(), entry: math.Hypot(5, 25), first: math.Hypot(25, 25), pitch: 25},
			// The wide entry is 60 m past the throat and 20 m farther from
			// the axis, and its first berth is 45 m past the entry and 50 m
			// beside it.
			{name: "wide", config: wide, entry: math.Hypot(20, 60), first: math.Hypot(50, 45), pitch: 37.5},
		} {
			got := []float64{
				distance(test.config, "940GZZLUBST-diverge", "940GZZLUBST-entry"),
				distance(test.config, "940GZZLUBST-entry", "940GZZLUBST-01-node"),
				distance(test.config, "940GZZLUBST-01-node", "940GZZLUBST-02-node"),
			}
			for index, want := range []float64{test.entry, test.first, test.pitch} {
				if math.Abs(got[index]-want) > 1e-6 {
					t.Fatalf("%s: Baker Street distances %v m, want %v, %v, and %v", test.name, got, test.entry, test.first, test.pitch)
				}
			}
			// Regent's Park has Compact bays. Their entry is 60 m past the
			// throat, londonCompactSide from the axis, with both shapes.
			want := math.Hypot(londonCompactSide-londonStationThroatOffset, 60)
			if got := distance(test.config, "940GZZLURGP-diverge", "940GZZLURGP-entry"); math.Abs(got-want) > 1e-6 {
				t.Fatalf("%s: Compact diverge to entry = %v m, want %v", test.name, got, want)
			}
		}
	})
}
