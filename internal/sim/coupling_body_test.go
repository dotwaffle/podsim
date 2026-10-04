package sim

import (
	"errors"
	"math"
	"testing"
)

func TestCouplingFootprintOracle(t *testing.T) {
	t.Parallel()
	for _, angle := range []float64{0, math.Pi / 2, math.Pi, -math.Pi / 3} {
		for _, spacing := range []float64{4.5, 8, 12} {
			input := CouplingFootprintInput{Contract: CompactPairV1CouplingContract, Front: Point{X: 30, Y: -10},
				Direction: Point{X: math.Cos(angle), Y: math.Sin(angle)}, SpacingMeters: spacing}
			before := input
			got, err := CouplingFootprintAt(input)
			if err != nil {
				t.Fatal(err)
			}
			if input != before {
				t.Fatal("footprint changed caller state")
			}
			// Project corners directly onto the authored axes. This oracle does
			// not call the production rectangle or offset helpers.
			for index, body := range got.Bodies {
				centerOffset := -float64(index) * spacing
				wantLong := [4]float64{centerOffset + 2, centerOffset - 2, centerOffset - 2, centerOffset + 2}
				wantSide := [4]float64{1, 1, -1, -1}
				couplingCheckProjected(t, body, input, wantLong, wantSide)
			}
			couplingCheckProjected(t, got.Connector, input,
				[4]float64{-2, -spacing + 2, -spacing + 2, -2}, [4]float64{0.15, 0.15, -0.15, -0.15})
			frontPin, rearPin := got.Pins[0], got.Pins[1]
			if math.Abs(math.Hypot(frontPin.X-rearPin.X, frontPin.Y-rearPin.Y)-(spacing-4)) > 1e-12 {
				t.Fatal("pin gap differs from body-end gap")
			}
			if math.Abs(math.Hypot(got.Centers[0].X-got.Centers[1].X, got.Centers[0].Y-got.Centers[1].Y)-spacing) > 1e-12 {
				t.Fatal("member spacing changed")
			}
		}
	}
}

func couplingCheckProjected(t *testing.T, shape CouplingRectangle, input CouplingFootprintInput, wantLong, wantSide [4]float64) {
	t.Helper()
	for index, point := range shape.Corners {
		x, y := point.X-input.Front.X, point.Y-input.Front.Y
		long := x*input.Direction.X + y*input.Direction.Y
		side := -x*input.Direction.Y + y*input.Direction.X
		if math.Abs(long-wantLong[index]) > 1e-12 || math.Abs(side-wantSide[index]) > 1e-12 {
			t.Fatalf("corner %d axes = (%g,%g), want (%g,%g)", index, long, side, wantLong[index], wantSide[index])
		}
	}
}

func TestCouplingFootprintInputGuards(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		change func(*CouplingFootprintInput)
		want   error
	}{
		{"marker", func(i *CouplingFootprintInput) { i.Contract = "" }, ErrUnknownCouplingContract},
		{"position", func(i *CouplingFootprintInput) { i.Front.X = math.NaN() }, ErrInvalidCouplingGeometry},
		{"coordinate cap", func(i *CouplingFootprintInput) { i.Front.X = 100001 }, ErrInvalidCouplingGeometry},
		{"zero direction", func(i *CouplingFootprintInput) { i.Direction = Point{} }, ErrInvalidCouplingGeometry},
		{"scaled direction", func(i *CouplingFootprintInput) { i.Direction.X = 2 }, ErrInvalidCouplingGeometry},
		{"nonfinite direction", func(i *CouplingFootprintInput) { i.Direction.X = math.Inf(1) }, ErrInvalidCouplingGeometry},
		{"short spacing", func(i *CouplingFootprintInput) { i.SpacingMeters = math.Nextafter(4.5, 0) }, ErrInvalidCouplingGeometry},
		{"long spacing", func(i *CouplingFootprintInput) { i.SpacingMeters = math.Nextafter(12, 13) }, ErrInvalidCouplingGeometry},
		{"nonfinite spacing", func(i *CouplingFootprintInput) { i.SpacingMeters = math.NaN() }, ErrInvalidCouplingGeometry},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			input := CouplingFootprintInput{Contract: CompactPairV1CouplingContract, Direction: Point{X: 1}, SpacingMeters: 4.5}
			tc.change(&input)
			got, err := CouplingFootprintAt(input)
			if !errors.Is(err, tc.want) || got != (CouplingFootprint{}) {
				t.Fatalf("invalid footprint accepted: %+v %v", got, err)
			}
		})
	}
}

func TestCouplingSiteSweptBodyRoom(t *testing.T) {
	t.Parallel()
	room, err := CouplingSiteRoom(CompactPairV1CouplingContract)
	if err != nil {
		t.Fatal(err)
	}
	// The rear starts at the exact longitudinal margin. Closing keeps the
	// front fixed. Opening holds the rear and advances the front by 7.5 m.
	rearStart := room.BoundaryMarginMeters
	frontStart := rearStart + 12
	for _, opening := range []bool{false, true} {
		for _, travel := range []float64{0, 3.75, 7.5} {
			front, spacing := frontStart, 12-travel
			if opening {
				front, spacing = frontStart+travel, 4.5+travel
			}
			shape, err := CouplingFootprintAt(CouplingFootprintInput{Contract: CompactPairV1CouplingContract,
				Front: Point{X: front}, Direction: Point{X: 1}, SpacingMeters: spacing})
			if err != nil {
				t.Fatal(err)
			}
			for _, rectangle := range []CouplingRectangle{shape.Bodies[0], shape.Bodies[1], shape.Connector} {
				for _, corner := range rectangle.Corners {
					// Independent boundary arithmetic includes ordinary clearance,
					// braking at 0.5 m/s, and one full 60 Hz motion tick.
					minimum := 12.0 + 0.25 + 0.5/60
					if corner.X < minimum-1e-12 || room.RequiredLengthMeters-corner.X < minimum-1e-12 || math.Abs(corner.Y) > 1 {
						t.Fatalf("maneuver body exceeds protected room: opening=%t travel=%g corner=%+v", opening, travel, corner)
					}
				}
			}
		}
	}
}
