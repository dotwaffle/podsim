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

// MaxCouplingCorridorSpeed is the largest speed limit, in m/s, of a lane in a
// coupling site or corridor.
//
// The bound keeps the pair connector check (checkNativePairConnector)
// dominated by the body sweep check (checkForeignSweeps). The pair check
// refuses a member when the axis-aligned box of its tick sweep, grown by the
// body radius r = hypot(BodyLengthMeters, BodyWidthMeters)/2, touches the
// axis-aligned box of the other train's connector corners at the previous
// and next ticks. The body check refuses when a member sweep is closer than
// Clearance - conflictSlack to a member sweep of the other train.
//
// Let Ta be the travel in one tick of a member, and Tb the travel of the
// other train, with S = CenterSpacingMeters. A point in the box of a sweep of
// length Ta is at most Ta/2 from the sweep, with the worst case at a
// 45-degree heading. The box of the other connector corners is at most
// Tb/2 + (Lc+Wc)/2 from the connector center sweep, where
// Lc = CenterSpacingMeters - 2*PinOffsetMeters and Wc = ConnectorWidthMeters.
// When the member box touches the connector box, the member sweep is at most
// Ta/2 + Tb/2 + sqrt(2)*r + (Lc+Wc)/2 from the connector center sweep of a
// connected train. Each member center is S/2 from the connector center, so
// the member sweeps of the other train do not cover a part of the center
// sweep when Tb < S. Each point of that part is at most max(0, (S-Tb)/2) from
// a member sweep. The member sweep is thus at most
// Ta/2 + max(Tb, S)/2 + sqrt(2)*r + (Lc+Wc)/2 from a member sweep of the
// other train. Let T be the largest travel of a member in one tick. If
// T >= S, this is at most T + sqrt(2)*r + (Lc+Wc)/2. For compact-pair-v1
// the pair check is dominated while T < Clearance - conflictSlack - sqrt(10) - 0.4,
// about 8.44 m per tick or 506 m/s. This limit is larger than S, so a
// smaller T is also dominated. Two trains that move in parallel on 45-degree
// corridors reach this limit. During closing and opening the spacing can
// grow to Clearance, but those legs move at ManeuverSpeed and stay
// dominated. The second pair refusal, two connector boxes that touch, has a
// larger limit.
//
// Every tick with a connector, and every drain tick, has a speed cap from a
// corridor lane. The connected leg starts on the assembly lane, the drain
// legs start on the split lane, and legCap caps a leg by the lanes from
// where it starts. A leg that resumes after a restore keeps the cap of its
// original start. Closing and opening move at ManeuverSpeed. The bound of
// 360 m/s is 6 m per tick, 2.4 m per tick below the limit.
// TestCouplingPairConnectorDominatedUnderCorridorSpeedBound checks the
// domination at this travel, and
// TestCouplingPairConnectorDominationThreshold checks the limit.
const MaxCouplingCorridorSpeed = 360.0

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
