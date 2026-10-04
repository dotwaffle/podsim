package sim

import "errors"

// CouplingContract selects an immutable mechanical coupling model.
type CouplingContract string

// CompactPairV1CouplingContract permits the approved two-Compact model.
const CompactPairV1CouplingContract CouplingContract = "compact-pair-v1"

const (
	// MaxCouplingSites bounds authored protected sites by the existing fleet cap.
	MaxCouplingSites = expressMaxPods
	// MaxCouplingCorridors bounds authored paths by the existing fleet cap.
	MaxCouplingCorridors = expressMaxPods
)

var (
	// ErrUnknownCouplingContract means no approved coupling profile exists.
	ErrUnknownCouplingContract = errors.New("unknown coupling contract")
	// ErrInvalidCouplingGeometry means the authored geometry cannot fit the model.
	ErrInvalidCouplingGeometry = errors.New("invalid coupling geometry")
)

// CouplingProfile is a value copy of the approved simulation model.
// These numbers do not certify hardware or establish an energy model.
type CouplingProfile struct {
	Contract             CouplingContract
	Class                VehicleClass
	Members              int
	SeatsPerMember       int
	BodyLengthMeters     float64
	BodyWidthMeters      float64
	PinOffsetMeters      float64
	FreeGapMeters        float64
	ConnectorWidthMeters float64
	CenterSpacingMeters  float64
	OccupiedLengthMeters float64
	Acceleration         float64
	Braking              float64
	ManeuverSpeed        float64
	ManeuverAcceleration float64
	ManeuverBraking      float64
	LatchTicks           int
	UnlatchTicks         int
	PartnerWaitTicks     int
}

// LookupCouplingProfile returns an independent value for the explicit marker.
// Omission selects ordinary operation and does not return a train profile.
func LookupCouplingProfile(contract CouplingContract) (CouplingProfile, bool) {
	if contract != CompactPairV1CouplingContract {
		return CouplingProfile{}, false
	}
	return CouplingProfile{
		Contract: contract, Class: CompactClass, Members: 2, SeatsPerMember: 4,
		BodyLengthMeters: 4, BodyWidthMeters: 2, PinOffsetMeters: 2,
		FreeGapMeters: 0.5, ConnectorWidthMeters: 0.3,
		CenterSpacingMeters: 4.5, OccupiedLengthMeters: 8.5,
		Acceleration: 2, Braking: 2,
		ManeuverSpeed: 0.5, ManeuverAcceleration: 0.5, ManeuverBraking: 0.5,
		LatchTicks: 2 * TicksPerSecond, UnlatchTicks: 2 * TicksPerSecond,
		PartnerWaitTicks: 5 * TicksPerSecond,
	}, true
}

// CouplingRoom separates longitudinal room from the body's lateral width.
type CouplingRoom struct {
	StagingSpacingMeters float64
	OpeningTravelMeters  float64
	StoppingMeters       float64
	BoundaryMarginMeters float64
	RequiredLengthMeters float64
	BodyWidthMeters      float64
}

// CouplingSiteRoom bounds closing and opening in one protected site.
// Each boundary retains a body half-length, ordinary clearance, stopping room,
// and one tick of maneuver motion. External traffic still needs ownership proof.
func CouplingSiteRoom(contract CouplingContract) (CouplingRoom, error) {
	p, ok := LookupCouplingProfile(contract)
	if !ok {
		return CouplingRoom{}, ErrUnknownCouplingContract
	}
	stopping := p.ManeuverSpeed*p.ManeuverSpeed/(2*p.ManeuverBraking) + p.ManeuverSpeed/TicksPerSecond
	margin := p.BodyLengthMeters/2 + Clearance + stopping
	opening := Clearance - p.CenterSpacingMeters
	return CouplingRoom{
		StagingSpacingMeters: Clearance, OpeningTravelMeters: opening,
		StoppingMeters: stopping, BoundaryMarginMeters: margin,
		RequiredLengthMeters: Clearance + opening + 2*margin,
		BodyWidthMeters:      p.BodyWidthMeters,
	}, nil
}
