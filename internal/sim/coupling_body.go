package sim

import (
	"fmt"
	"math"
)

// CouplingRectangle lists four corners in perimeter order on the XY plane.
type CouplingRectangle struct{ Corners [4]Point }

// CouplingFootprint contains separate cabins and the pin-to-pin connector.
type CouplingFootprint struct {
	Bodies    [2]CouplingRectangle
	Connector CouplingRectangle
	Centers   [2]Point
	Pins      [2]Point
}

// CouplingFootprintInput describes a front-to-rear pair during a site maneuver.
// Direction must be a unit vector. Spacing ranges from connected to ordinary.
type CouplingFootprintInput struct {
	Contract      CouplingContract
	Front         Point
	Direction     Point
	SpacingMeters float64
}

// CouplingFootprintAt computes body and connector corners without route state.
// An opening connector envelope bounds the maneuver. It does not imply a latch.
func CouplingFootprintAt(input CouplingFootprintInput) (CouplingFootprint, error) {
	p, ok := LookupCouplingProfile(input.Contract)
	if !ok {
		return CouplingFootprint{}, ErrUnknownCouplingContract
	}
	if err := couplingFootprintInputs(input, p); err != nil {
		return CouplingFootprint{}, err
	}
	rear := couplingOffset(input.Front, input.Direction, -input.SpacingMeters)
	frontPin := couplingOffset(input.Front, input.Direction, -p.PinOffsetMeters)
	rearPin := couplingOffset(rear, input.Direction, p.PinOffsetMeters)
	connectorCenter := Point{X: (frontPin.X + rearPin.X) / 2, Y: (frontPin.Y + rearPin.Y) / 2}
	result := CouplingFootprint{
		Centers: [2]Point{input.Front, rear}, Pins: [2]Point{frontPin, rearPin},
		Bodies: [2]CouplingRectangle{
			couplingRectangle(input.Front, input.Direction, p.BodyLengthMeters, p.BodyWidthMeters),
			couplingRectangle(rear, input.Direction, p.BodyLengthMeters, p.BodyWidthMeters),
		},
		Connector: couplingRectangle(connectorCenter, input.Direction, input.SpacingMeters-2*p.PinOffsetMeters, p.ConnectorWidthMeters),
	}
	for _, shape := range []CouplingRectangle{result.Bodies[0], result.Bodies[1], result.Connector} {
		for _, corner := range shape.Corners {
			if !finite(corner.X) || !finite(corner.Y) {
				return CouplingFootprint{}, fmt.Errorf("nonfinite body corner: %w", ErrInvalidCouplingGeometry)
			}
		}
	}
	return result, nil
}

func couplingFootprintInputs(input CouplingFootprintInput, p CouplingProfile) error {
	length := math.Hypot(input.Direction.X, input.Direction.Y)
	if !contractPointFits(input.Front) || !finite(length) || math.Abs(length-1) > conflictSlack ||
		!finite(input.SpacingMeters) || input.SpacingMeters < p.CenterSpacingMeters || input.SpacingMeters > Clearance {
		return fmt.Errorf("invalid pair position, direction, or spacing: %w", ErrInvalidCouplingGeometry)
	}
	return nil
}

func couplingOffset(point, direction Point, meters float64) Point {
	return Point{X: point.X + direction.X*meters, Y: point.Y + direction.Y*meters}
}

func couplingRectangle(center, direction Point, length, width float64) CouplingRectangle {
	normal := Point{X: -direction.Y, Y: direction.X}
	front, rear := couplingOffset(center, direction, length/2), couplingOffset(center, direction, -length/2)
	return CouplingRectangle{Corners: [4]Point{
		couplingOffset(front, normal, width/2), couplingOffset(rear, normal, width/2),
		couplingOffset(rear, normal, -width/2), couplingOffset(front, normal, -width/2),
	}}
}
