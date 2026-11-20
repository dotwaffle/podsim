package sim

import (
	"math"
	"math/rand/v2"
	"testing"
)

// bankPathsCloseReference is bankPathsClose with no skip: the full double
// loop over the segments.
func bankPathsCloseReference(first, second []Point, clearance float64) bool {
	for a := 1; a < len(first); a++ {
		for b := 1; b < len(second); b++ {
			if bankSegmentGap(first[a-1], first[a], second[b-1], second[b], clearance) < clearance-separationTolerance {
				return true
			}
		}
	}
	return false
}

// TestBankPathsCloseMatchesReference compares bankPathsClose with the full
// double loop on random polylines and on cases at the edge of the skip:
// boxes that touch, collinear and zero-length segments, distances at the
// clearance, coordinates where the sum of a coordinate and the clearance
// rounds, and NaN and infinite values.
func TestBankPathsCloseMatchesReference(t *testing.T) {
	t.Parallel()
	random := rand.New(rand.NewPCG(20261120, 1))
	clearances := []float64{0, 1, Clearance, largeClearance, 12.5, -1, math.Inf(1), math.Inf(-1), math.NaN()}
	verdicts := map[bool]int{}
	check := func(name string, first, second []Point, clearance float64) {
		t.Helper()
		want := bankPathsCloseReference(first, second, clearance)
		if got := bankPathsClose(first, second, clearance); got != want {
			t.Fatalf("%s: bankPathsClose(%v, %v, %v) = %v, the reference gives %v", name, first, second, clearance, got, want)
		}
		verdicts[want]++
	}
	both := func(name string, first, second []Point) {
		t.Helper()
		for _, clearance := range clearances {
			check(name, first, second, clearance)
			check(name, second, first, clearance)
		}
	}
	// A polyline on a small integer grid, so that equal coordinates,
	// collinear segments, and zero-length segments are common.
	grid := func(count, span int) []Point {
		points := make([]Point, count)
		for index := range points {
			points[index] = Point{X: float64(random.IntN(span)), Y: float64(random.IntN(span))}
		}
		return points
	}
	for range 3000 {
		both("grid", grid(1+random.IntN(6), 30), grid(1+random.IntN(6), 30))
	}
	// A smooth curve, as a curved lane has, at an offset that puts the two
	// boxes near each other.
	curve := func(origin Point, radius float64, count int) []Point {
		points := make([]Point, count)
		for index := range points {
			angle := float64(index) / float64(count-1) * math.Pi / 2
			points[index] = Point{X: origin.X + radius*math.Cos(angle), Y: origin.Y + radius*math.Sin(angle)}
		}
		return points
	}
	for range 400 {
		offset := Point{X: random.Float64()*80 - 40, Y: random.Float64()*80 - 40}
		both("curve", curve(Point{}, 30, 65), curve(offset, 20+random.Float64()*20, 2+random.IntN(64)))
	}
	// Boxes that touch: the second polyline starts at a gap of exactly the
	// clearance from the first on one axis, or just inside or outside it.
	for _, clearance := range []float64{1, Clearance, largeClearance} {
		for _, delta := range []float64{0, math.SmallestNonzeroFloat64, -separationTolerance, separationTolerance, -2 * separationTolerance, 1e-9, -1e-9} {
			first := []Point{{X: 0, Y: 0}, {X: 5, Y: 3}, {X: 5, Y: 3}, {X: 10, Y: 0}}
			gap := clearance + delta
			for _, second := range [][]Point{
				{{X: 10 + gap, Y: -4}, {X: 10 + gap, Y: 4}},
				{{X: -gap, Y: 0}, {X: -gap - 3, Y: 0}},
				{{X: 2, Y: 3 + gap}, {X: 8, Y: 3 + gap}, {X: 8, Y: 3 + gap}},
				{{X: 2, Y: -gap}, {X: 20, Y: -gap}},
				{{X: 10 + gap, Y: 0}, {X: 20 + gap, Y: 0}},
				{{X: 10 + gap, Y: 3 + gap}, {X: 10 + gap, Y: 3 + gap}},
			} {
				check("touching", first, second, clearance)
				check("touching", second, first, clearance)
			}
		}
	}
	// Large coordinates, where the sum of a coordinate and the clearance
	// rounds by more than separationTolerance. The second polyline starts at
	// that sum, so the skip test is an equality, and the true gap can be
	// less than the clearance.
	for _, magnitude := range []float64{1e11, 3e12, 7e13, 1e15, -1e11, -3e12} {
		for _, clearance := range []float64{Clearance, largeClearance, 12.3456789} {
			for step := range 64 {
				x := magnitude + float64(step)*0.123456789*math.Abs(magnitude)*1e-12
				edge := x + clearance
				first := []Point{{X: x - 5, Y: 0}, {X: x, Y: 0}}
				second := []Point{{X: edge, Y: -1}, {X: edge, Y: 1}, {X: edge + 4, Y: 2}}
				check("rounding", first, second, clearance)
				check("rounding", second, first, clearance)
				vertical := []Point{{X: 0, Y: x - 5}, {X: 0, Y: x}}
				above := []Point{{X: -1, Y: edge}, {X: 1, Y: edge}}
				check("rounding", vertical, above, clearance)
				check("rounding", above, vertical, clearance)
			}
		}
	}
	// NaN and infinite coordinates in either polyline.
	specials := []float64{math.NaN(), math.Inf(1), math.Inf(-1)}
	for _, special := range specials {
		for index := range 4 {
			for _, axis := range []int{0, 1} {
				base := []Point{{X: 0, Y: 0}, {X: 5, Y: 5}, {X: 10, Y: 0}, {X: 15, Y: 5}}
				if axis == 0 {
					base[index].X = special
				} else {
					base[index].Y = special
				}
				for _, other := range [][]Point{
					{{X: 0, Y: 30}, {X: 15, Y: 30}},
					{{X: 40, Y: 0}, {X: 40, Y: 5}},
					{{X: 2, Y: 1}, {X: 3, Y: 2}},
					{{X: -40, Y: -40}, {X: -30, Y: -40}},
				} {
					both("special", base, other)
				}
			}
		}
	}
	// Polylines with fewer than two points.
	for _, short := range [][]Point{nil, {}, {{X: 1, Y: 1}}} {
		both("short", short, []Point{{X: 0, Y: 0}, {X: 1, Y: 1}})
		both("short", short, short)
	}
	if verdicts[true] == 0 || verdicts[false] == 0 {
		t.Fatalf("the cases give only one verdict: %v", verdicts)
	}
}

// BenchmarkBankPathsClose measures one curved in/out lane pair of a ring
// station: two curves that share no node and are not close.
func BenchmarkBankPathsClose(b *testing.B) {
	positions := map[string]Point{"entry": {X: 0, Y: 0}, "berth-1": {X: 20, Y: 30}, "berth-2": {X: 50, Y: 30}, "exit": {X: 70, Y: 0}}
	in := lanePointsBetween(Lane{From: "entry", To: "berth-1", Control: &Point{X: 0, Y: 30}}, positions["entry"], positions["berth-1"], nil)
	out := lanePointsBetween(Lane{From: "berth-2", To: "exit", Control: &Point{X: 70, Y: 30}}, positions["berth-2"], positions["exit"], nil)
	for b.Loop() {
		bankPathsClose(in, out, Clearance)
	}
}
