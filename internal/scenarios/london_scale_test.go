package scenarios

import (
	"math"
	"strings"
	"testing"

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
	t.Run("half scale", func(t *testing.T) {
		t.Parallel()
		options := DefaultLondonCentralOptions()
		options.StationScale = 0.5
		config, err := LondonCentralWith(options)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(config.Name, customCapacitySuffix) {
			t.Fatalf("project name %q has no custom capacity suffix", config.Name)
		}
		// Baker Street has berth rows. Its entry is 60 m past the throat
		// and 20 m farther from the axis, and its first berth is 45 m past
		// the entry and 50 m beside it.
		node := func(id string) sim.Point {
			n, ok := config.Network.Node(id)
			if !ok {
				t.Fatalf("no node %s", id)
			}
			return n.Position
		}
		distance := func(a, b sim.Point) float64 { return math.Hypot(b.X-a.X, b.Y-a.Y) }
		if got := distance(node("940GZZLUBST-diverge"), node("940GZZLUBST-entry")); math.Abs(got-math.Hypot(20, 60)) > 1e-6 {
			t.Fatalf("diverge to entry = %v m, want %v", got, math.Hypot(20, 60))
		}
		if got := distance(node("940GZZLUBST-entry"), node("940GZZLUBST-01-node")); math.Abs(got-math.Hypot(50, 45)) > 1e-6 {
			t.Fatalf("entry to first berth = %v m, want %v", got, math.Hypot(50, 45))
		}
		// Regent's Park has Compact bays. Their entry is 60 m past the
		// throat, londonCompactSide from the axis.
		want := math.Hypot(londonCompactSide-londonStationThroatOffset, 60)
		if got := distance(node("940GZZLURGP-diverge"), node("940GZZLURGP-entry")); math.Abs(got-want) > 1e-6 {
			t.Fatalf("Compact diverge to entry = %v m, want %v", got, want)
		}
	})
}
