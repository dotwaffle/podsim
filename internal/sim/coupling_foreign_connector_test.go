package sim

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"strings"
	"testing"
)

// couplingConnectorContext returns a train in the middle of its connected
// leg and the state one tick later.
func couplingConnectorContext(t *testing.T, id string) (*couplingMotionContext, couplingMotionState, couplingMotionState) {
	t.Helper()
	input := couplingMotionFixture(t, false, false)
	plan, err := planCouplingReservation(input)
	if err != nil {
		t.Fatal(err)
	}
	c, err := prepareCouplingMotionContext(couplingMotionContextInput{Reservation: plan, Current: input, GroupID: id})
	if err != nil {
		t.Fatal(err)
	}
	previous := remainingTestFind(t, c, couplingConnected, 1)
	next, err := c.stateAt(previous.Elapsed + 1)
	if err != nil || next.Phase != couplingConnected {
		t.Fatal("connected fixture has no connected next state", err)
	}
	return c, previous, next
}

func couplingConnectorMemberSegments(t *testing.T, c *couplingMotionContext, previous, next couplingMotionState) [2][]laneSegment {
	t.Helper()
	var members [2][]laneSegment
	for i := range members {
		var err error
		members[i], err = couplingMotionSegments(&c.reservation.routes[i], previous.Distances[i], next.Distances[i])
		if err != nil {
			t.Fatal(err)
		}
	}
	return members
}

func couplingConnectorBodyDistance(members [2][]laneSegment, foreign []laneSegment) float64 {
	nearest := math.Inf(1)
	for _, member := range members {
		for _, a := range member {
			for _, b := range foreign {
				nearest = min(nearest, couplingSegmentsDistance(a.from, a.to, b.from, b.to))
			}
		}
	}
	return nearest
}

// The connector sweep check refuses a foreign center inside its envelope and
// accepts one outside it. Each refused center is also inside the body
// clearance, so the body sweep check in checkForeignSweeps refuses it first.
func TestCouplingForeignConnectorSweepRefusal(t *testing.T) {
	t.Parallel()
	c, previous, next := couplingConnectorContext(t, "pair")
	footprint, err := c.connectedFootprint(previous)
	if err != nil {
		t.Fatal(err)
	}
	profile, _ := LookupCouplingProfile(c.reservation.network.contract)
	corridor := c.reservation.network.corridors[c.reservation.corridorID]
	direction := c.reservation.network.lanes[corridor.LaneIDs[0]].direction
	normal := Point{X: -direction.Y, Y: direction.X}
	center := Point{X: (footprint.Pins[0].X + footprint.Pins[1].X) / 2, Y: (footprint.Pins[0].Y + footprint.Pins[1].Y) / 2}
	members := couplingConnectorMemberSegments(t, c, previous, next)
	for _, class := range []VehicleClass{CompactClass, LegacyClass, ExpressClass} {
		radius := classPairClearance(CompactClass, class) / 2
		if largeVehicleClass(class) {
			radius = largeEnvelopeRadius
		}
		envelope := radius + profile.ConnectorWidthMeters/2
		for _, test := range []struct {
			name   string
			offset float64
			refuse bool
		}{{"crossing", 0, true}, {"inside_edge", envelope - 0.01, true}, {"outside_edge", envelope + 0.01, false}} {
			t.Run(string(class)+"/"+test.name, func(t *testing.T) {
				t.Parallel()
				point := couplingOffset(center, normal, test.offset)
				foreign := []laneSegment{{from: point, to: point}}
				err := c.checkConnectorSweep(previous, next, foreign, class)
				if !test.refuse {
					if err != nil {
						t.Fatal("connector sweep refused a foreign center outside its envelope", err)
					}
					return
				}
				if !errors.Is(err, errCouplingMotionInvariant) || !strings.Contains(err.Error(), "swept connector") {
					t.Fatal("connector sweep accepted a foreign center inside its envelope", err)
				}
				if gap := couplingConnectorBodyDistance(members, foreign); gap >= classPairClearance(CompactClass, class)-conflictSlack {
					t.Fatalf("connector refusal at %.3f m is outside the body clearance", gap)
				}
			})
		}
	}
}

// The pair connector check refuses another train whose connector box touches
// a member body bound and accepts a distant one. Each refused train has a
// member center inside the body clearance of this train.
func TestCouplingForeignPairConnectorRefusal(t *testing.T) {
	t.Parallel()
	c, previous, next := couplingConnectorContext(t, "pair")
	other, _, _ := couplingConnectorContext(t, "other")
	members := couplingConnectorMemberSegments(t, c, previous, next)
	far := couplingMotionState{}
	for elapsed := uint64(1); elapsed+1 < other.ticks; elapsed++ {
		state, err := other.stateAt(elapsed)
		if err != nil {
			t.Fatal(err)
		}
		if state.Phase == couplingConnected && math.Abs(state.Distances[0]-previous.Distances[0]) > 3*Clearance {
			far = state
			break
		}
	}
	if far.context == nil {
		t.Fatal("fixture has no distant connected state")
	}
	farNext, err := other.stateAt(far.Elapsed + 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name           string
		previous, next couplingMotionState
		refuse         bool
	}{{"same_place", previous, next, true}, {"distant", far, farNext, false}} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			proof := &nativeForeignPairProof{pair: couplingNativeForeignPair{context: other, previous: test.previous, next: test.next}}
			err := c.checkNativePairConnector(previous, next, proof, members)
			if !test.refuse {
				if err != nil {
					t.Fatal("pair connector check refused a distant train", err)
				}
				return
			}
			if !errors.Is(err, errCouplingMotionInvariant) || !strings.Contains(err.Error(), "member body bound") {
				t.Fatal("pair connector check accepted a touching train connector", err)
			}
			foreign := couplingConnectorMemberSegments(t, other, test.previous, test.next)
			gap := min(couplingConnectorBodyDistance(members, foreign[0]), couplingConnectorBodyDistance(members, foreign[1]))
			if gap >= Clearance-conflictSlack {
				t.Fatalf("pair connector refusal at %.3f m is outside the body clearance", gap)
			}
		})
	}
	proof := &nativeForeignPairProof{pair: couplingNativeForeignPair{context: c, previous: previous, next: next}}
	if err := c.checkNativePairConnector(previous, next, proof, members); !errors.Is(err, errCouplingMotionInvariant) {
		t.Fatal("pair connector check accepted its own context as another train", err)
	}
}

// The swept connector check (checkConnectorSweep) is dominated by the body
// sweep check for any travel. Both members and the connector lie on one
// straight corridor axis, so each point of the connector sweep is at most
// halfSpan from a member center sweep. A foreign sweep that the connector
// check refuses is then inside the body clearance. A change that breaks this
// test can make the swept connector check reachable at Step.
func TestCouplingForeignConnectorSweepBoundDominatedByBodyClearance(t *testing.T) {
	t.Parallel()
	profile, ok := LookupCouplingProfile(CompactPairV1CouplingContract)
	if !ok {
		t.Fatal("unknown profile")
	}
	halfSpan := profile.CenterSpacingMeters / 2
	halfWidth := profile.ConnectorWidthMeters / 2
	halfLength := (profile.CenterSpacingMeters - 2*profile.PinOffsetMeters) / 2
	if halfLength <= 0 || profile.PinOffsetMeters+halfLength != halfSpan {
		t.Fatal("connector geometry changed: the pins and the spacing no longer bound the connector")
	}
	for _, class := range []VehicleClass{CompactClass, LegacyClass, GroupClass, ExpressClass} {
		clearance := classPairClearance(CompactClass, class)
		radius := clearance / 2
		if largeVehicleClass(class) {
			radius = largeEnvelopeRadius
		}
		if bound := radius + halfWidth + halfSpan; bound >= clearance-conflictSlack {
			t.Fatalf("connector sweep bound %.3f m for %s reaches the body clearance %.3f m", bound, class, clearance)
		}
	}
}

// The pair connector check (checkNativePairConnector) is not dominated at
// any travel. It compares axis-aligned boxes of the other train's connector
// corners at the previous and next ticks. The connected leg has no profile
// speed cap (coupling_motion_context.go:82). On a diagonal corridor at 20 m
// per tick (1200 m/s), the box covers a member 12.5 m from the other train's
// center and connector sweeps, so the pair check refuses a pose that the
// body check accepts. This is why coupling geometry validation refuses a
// corridor lane faster than MaxCouplingCorridorSpeed.
func TestCouplingPairConnectorBoxNotDominatedAtHighTravel(t *testing.T) {
	t.Parallel()
	profile, _ := LookupCouplingProfile(CompactPairV1CouplingContract)
	direction := Point{X: math.Sqrt2 / 2, Y: math.Sqrt2 / 2}
	normal := Point{X: -direction.Y, Y: direction.X}
	halfSpan := profile.CenterSpacingMeters / 2
	connectorLength := profile.CenterSpacingMeters - 2*profile.PinOffsetMeters
	radius := math.Hypot(profile.BodyLengthMeters, profile.BodyWidthMeters) / 2
	for _, test := range []struct {
		name   string
		travel float64
		refuse bool
	}{{"high_travel", 20, true}, {"maneuver_travel", profile.ManeuverSpeed / TicksPerSecond, false}} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			before, after := Point{}, couplingOffset(Point{}, direction, test.travel)
			var other couplingBox
			for i, center := range []Point{before, after} {
				for j, corner := range couplingRectangle(center, direction, connectorLength, profile.ConnectorWidthMeters).Corners {
					if i == 0 && j == 0 {
						other = couplingSegmentBox(corner, corner, 0)
					} else {
						other = couplingUnionBox(other, couplingSegmentBox(corner, corner, 0))
					}
				}
			}
			member := couplingOffset(couplingOffset(before, direction, test.travel/2), normal, 12.5)
			sweeps := []laneSegment{{from: before, to: after},
				{from: couplingOffset(before, direction, -halfSpan), to: couplingOffset(after, direction, -halfSpan)},
				{from: couplingOffset(before, direction, halfSpan), to: couplingOffset(after, direction, halfSpan)}}
			for _, sweep := range sweeps {
				if gap := couplingSegmentsDistance(member, member, sweep.from, sweep.to); gap < Clearance {
					t.Fatalf("member is %.3f m from a sweep of the other train, inside the body clearance", gap)
				}
			}
			if refused := couplingSegmentBox(member, member, radius).intersects(other); refused != test.refuse {
				t.Fatalf("pair connector box refusal = %t at %.3f m per tick, want %t", refused, test.travel, test.refuse)
			}
		})
	}
}

// couplingPairBoxTrain models one train for the pair connector check: the
// box of its connector corners at two ticks and its member center sweeps.
type couplingPairBoxTrain struct {
	box     couplingBox
	members [2]laneSegment
}

// couplingPairBoxMotion is one tick of a train: the spacing at the previous
// and next ticks and the travel of the front member.
type couplingPairBoxMotion struct {
	spacing [2]float64
	travel  float64
}

// couplingPairBoxMotions lists a connected tick at the given travel, and
// closing and opening ticks at ManeuverSpeed from the connected spacing to
// Clearance. Closing moves the rear member and opening moves the front one.
func couplingPairBoxMotions(profile CouplingProfile, travel float64) []couplingPairBoxMotion {
	maneuver := profile.ManeuverSpeed / TicksPerSecond
	motions := []couplingPairBoxMotion{{spacing: [2]float64{profile.CenterSpacingMeters, profile.CenterSpacingMeters}, travel: travel}}
	for _, spacing := range []float64{profile.CenterSpacingMeters + maneuver, (profile.CenterSpacingMeters + Clearance) / 2, Clearance} {
		motions = append(motions, couplingPairBoxMotion{spacing: [2]float64{spacing, spacing - maneuver}},
			couplingPairBoxMotion{spacing: [2]float64{spacing - maneuver, spacing}, travel: maneuver})
	}
	return motions
}

func couplingPairBoxTrainAt(t *testing.T, direction Point, motion couplingPairBoxMotion) couplingPairBoxTrain {
	t.Helper()
	var train couplingPairBoxTrain
	var footprints [2]CouplingFootprint
	for i, front := range []Point{{}, couplingOffset(Point{}, direction, motion.travel)} {
		var err error
		footprints[i], err = CouplingFootprintAt(CouplingFootprintInput{Contract: CompactPairV1CouplingContract, Front: front, Direction: direction, SpacingMeters: motion.spacing[i]})
		if err != nil {
			t.Fatal(err)
		}
	}
	train.box = couplingSegmentBox(footprints[0].Connector.Corners[0], footprints[0].Connector.Corners[0], 0)
	for _, footprint := range &footprints {
		for _, corner := range footprint.Connector.Corners {
			train.box = couplingUnionBox(train.box, couplingSegmentBox(corner, corner, 0))
		}
	}
	for i := range train.members {
		train.members[i] = laneSegment{from: footprints[0].Centers[i], to: footprints[1].Centers[i]}
	}
	return train
}

// couplingPairBoxContacts returns offsets on the edges of the region where
// two boxes touch. The offsets move one nanometer inside the region, so the
// boxes intersect without a rounding error.
func couplingPairBoxContacts(region couplingBox) []Point {
	const steps = 16
	region.low = Point{X: region.low.X + 1e-9, Y: region.low.Y + 1e-9}
	region.high = Point{X: region.high.X - 1e-9, Y: region.high.Y - 1e-9}
	var offsets []Point
	for i := range steps + 1 {
		for j := range steps + 1 {
			if i == 0 || i == steps || j == 0 || j == steps {
				offsets = append(offsets, Point{X: region.low.X + (region.high.X-region.low.X)*float64(i)/steps, Y: region.low.Y + (region.high.Y-region.low.Y)*float64(j)/steps})
			}
		}
	}
	return offsets
}

// couplingPairBoxGap returns the body sweep gap between the sweeps moved by
// offset and the member sweeps of the other train.
func couplingPairBoxGap(sweeps []laneSegment, offset Point, other couplingPairBoxTrain) float64 {
	gap := math.Inf(1)
	for _, sweep := range sweeps {
		from, to := Point{X: sweep.from.X + offset.X, Y: sweep.from.Y + offset.Y}, Point{X: sweep.to.X + offset.X, Y: sweep.to.Y + offset.Y}
		for _, member := range other.members {
			gap = min(gap, couplingSegmentsDistance(from, to, member.from, member.to))
		}
	}
	return gap
}

// couplingPairBoxWorstGap returns the largest body sweep gap over the
// sampled poses that the pair connector check refuses, and a description of
// that pose. Each member and train travels at most the given distance in one
// tick. A member sweep can have any heading, because a drain leg can leave
// the corridor axis. The other train heads from 0 to 90 degrees; the
// reflections of the plane cover the other headings.
func couplingPairBoxWorstGap(t *testing.T, travel float64) (float64, string) {
	t.Helper()
	profile, _ := LookupCouplingProfile(CompactPairV1CouplingContract)
	radius := math.Hypot(profile.BodyLengthMeters, profile.BodyWidthMeters) / 2
	heading := func(degrees float64) Point {
		return Point{X: math.Cos(degrees * math.Pi / 180), Y: math.Sin(degrees * math.Pi / 180)}
	}
	worst, pose := 0.0, ""
	record := func(gap float64, format string, args ...any) {
		if gap > worst {
			worst, pose = gap, fmt.Sprintf(format, args...)
		}
	}
	motions := couplingPairBoxMotions(profile, travel)
	for otherDegrees := 0.0; otherDegrees <= 90; otherDegrees += 15 {
		for otherIndex, otherMotion := range motions {
			other := couplingPairBoxTrainAt(t, heading(otherDegrees), otherMotion)
			for degrees := 0.0; degrees < 360; degrees += 15 {
				// A member box against the other connector box.
				for _, length := range []float64{0, travel / 2, travel} {
					sweep := laneSegment{to: couplingOffset(Point{}, heading(degrees), length)}
					box := couplingSegmentBox(sweep.from, sweep.to, radius)
					region := couplingBox{low: Point{X: other.box.low.X - box.high.X, Y: other.box.low.Y - box.high.Y}, high: Point{X: other.box.high.X - box.low.X, Y: other.box.high.Y - box.low.Y}}
					for _, offset := range couplingPairBoxContacts(region) {
						moved := couplingSegmentBox(Point{X: sweep.from.X + offset.X, Y: sweep.from.Y + offset.Y}, Point{X: sweep.to.X + offset.X, Y: sweep.to.Y + offset.Y}, radius)
						if !moved.intersects(other.box) {
							t.Fatal("contact offset is outside the member box contact region")
						}
						record(couplingPairBoxGap([]laneSegment{sweep}, offset, other), "member %.0f m at %.0f degrees, other motion %d at %.0f degrees", length, degrees, otherIndex, otherDegrees)
					}
				}
				// The connector box of this train against the other connector box.
				for index, motion := range motions {
					own := couplingPairBoxTrainAt(t, heading(degrees), motion)
					region := couplingBox{low: Point{X: other.box.low.X - own.box.high.X, Y: other.box.low.Y - own.box.high.Y}, high: Point{X: other.box.high.X - own.box.low.X, Y: other.box.high.Y - own.box.low.Y}}
					for _, offset := range couplingPairBoxContacts(region) {
						moved := couplingBox{low: Point{X: own.box.low.X + offset.X, Y: own.box.low.Y + offset.Y}, high: Point{X: own.box.high.X + offset.X, Y: own.box.high.Y + offset.Y}}
						if !moved.intersects(other.box) {
							t.Fatal("contact offset is outside the connector box contact region")
						}
						record(couplingPairBoxGap(own.members[:], offset, other), "train motion %d at %.0f degrees, other motion %d at %.0f degrees", index, degrees, otherIndex, otherDegrees)
					}
				}
			}
		}
	}
	return worst, pose
}

// couplingPairBoxLimit returns the derived travel limit in one tick below
// which the pair connector check is dominated (see MaxCouplingCorridorSpeed).
func couplingPairBoxLimit() float64 {
	profile, _ := LookupCouplingProfile(CompactPairV1CouplingContract)
	radius := math.Hypot(profile.BodyLengthMeters, profile.BodyWidthMeters) / 2
	connector := (profile.CenterSpacingMeters - 2*profile.PinOffsetMeters + profile.ConnectorWidthMeters) / 2
	return Clearance - conflictSlack - math.Sqrt2*radius - connector
}

// At the travel of MaxCouplingCorridorSpeed, each pose that the pair
// connector check refuses is inside the body clearance, so the body sweep
// check refuses it too. The poses include the diagonal case of
// TestCouplingPairConnectorBoxNotDominatedAtHighTravel.
func TestCouplingPairConnectorDominatedUnderCorridorSpeedBound(t *testing.T) {
	t.Parallel()
	limit := couplingPairBoxLimit()
	travel := MaxCouplingCorridorSpeed / TicksPerSecond
	if gap, pose := couplingPairBoxWorstGap(t, travel); gap >= Clearance-conflictSlack {
		t.Errorf("at %.3f m per tick the pair connector check refuses %s, %.3f m from the other train", travel, pose, gap)
	}
	if travel >= limit {
		t.Errorf("bound travel %.3f m per tick is not below the derived limit %.3f m", travel, limit)
	}
}

// couplingPairBoxCornerGap returns the largest body sweep gap when a member
// box touches the connector box of the other train at a corner. Both trains
// are connected and move the given travel at 45 degrees, which is the worst
// case of the derivation. The boxes touch on the diagonal normal to the
// motion. The offsets move one picometer inside the contact, so the boxes
// intersect without a rounding error, and the gap changes much less than the
// steps of the threshold test.
func couplingPairBoxCornerGap(t *testing.T, travel float64) float64 {
	t.Helper()
	const inside = 1e-12
	profile, _ := LookupCouplingProfile(CompactPairV1CouplingContract)
	radius := math.Hypot(profile.BodyLengthMeters, profile.BodyWidthMeters) / 2
	direction := Point{X: math.Sqrt2 / 2, Y: math.Sqrt2 / 2}
	other := couplingPairBoxTrainAt(t, direction, couplingPairBoxMotions(profile, travel)[0])
	sweep := laneSegment{to: couplingOffset(Point{}, direction, travel)}
	box := couplingSegmentBox(sweep.from, sweep.to, radius)
	worst := 0.0
	for _, offset := range []Point{{X: other.box.high.X - box.low.X - inside, Y: other.box.low.Y - box.high.Y + inside}, {X: other.box.low.X - box.high.X + inside, Y: other.box.high.Y - box.low.Y - inside}} {
		moved := couplingSegmentBox(Point{X: sweep.from.X + offset.X, Y: sweep.from.Y + offset.Y}, Point{X: sweep.to.X + offset.X, Y: sweep.to.Y + offset.Y}, radius)
		if !moved.intersects(other.box) {
			t.Fatal("corner offset does not touch the connector box")
		}
		worst = max(worst, couplingPairBoxGap([]laneSegment{sweep}, offset, other))
	}
	return worst
}

// The derived limit is exact. Just below it, the body sweep check refuses
// the worst pose that the pair connector check refuses. Just above it, the
// body sweep check accepts that pose. The step is smaller than
// conflictSlack, so a limit without the slack fails.
func TestCouplingPairConnectorDominationThreshold(t *testing.T) {
	t.Parallel()
	const step = 1e-10
	limit := couplingPairBoxLimit()
	if gap := couplingPairBoxCornerGap(t, limit-step); gap >= Clearance-conflictSlack {
		t.Errorf("below the limit %.12f m the pair connector check refuses a pose %.12f m from the other train", limit, gap)
	}
	if gap := couplingPairBoxCornerGap(t, limit+step); gap < Clearance-conflictSlack {
		t.Errorf("above the limit %.12f m the worst refused pose is %.12f m from the other train, inside the body clearance", limit, gap)
	}
}

// couplingConnectorParkedSweep plans the connected fixture with one foreign
// berth for the given class at the given offset from the connector center,
// normal to the corridor. It returns the train, a connected tick, and a
// parked sweep at that berth. If network preparation or reservation
// planning refuses the berth, it returns that error.
func couplingConnectorParkedSweep(t *testing.T, class VehicleClass, offset float64) (*couplingMotionContext, couplingMotionState, couplingMotionState, couplingForeignSweep, error) {
	t.Helper()
	probe, at, _ := couplingConnectorContext(t, "probe")
	footprint, err := probe.connectedFootprint(at)
	if err != nil {
		t.Fatal(err)
	}
	corridor := probe.reservation.network.corridors[probe.reservation.corridorID]
	direction := probe.reservation.network.lanes[corridor.LaneIDs[0]].direction
	normal := Point{X: -direction.Y, Y: direction.X}
	center := couplingOffset(Point{X: (footprint.Pins[0].X + footprint.Pins[1].X) / 2, Y: (footprint.Pins[0].Y + footprint.Pins[1].Y) / 2}, normal, offset)

	input := couplingMotionFixture(t, false, false)
	network := input.Prepared.Network()
	classes := classBit(string(class))
	network.Nodes = append(network.Nodes, Node{ID: "connector-entry", Position: couplingOffset(center, normal, 80)},
		Node{ID: "connector-berth", Position: center}, Node{ID: "connector-exit", Position: couplingOffset(couplingOffset(center, normal, 80), direction, 40)})
	network.Stations = append(network.Stations, Station{ID: "connector-station", Name: "Connector", Entry: "connector-entry", Exit: "connector-exit", VehicleClasses: classes,
		Berths: []Berth{{ID: "connector-1", Node: "connector-berth", VehicleClasses: classes}}})
	for _, lane := range []Lane{{ID: "connector-in", From: "connector-entry", To: "connector-berth"}, {ID: "connector-out", From: "connector-berth", To: "connector-exit"},
		{ID: "connector-through", From: "connector-entry", To: "connector-exit"}} {
		lane.VehicleClasses, lane.SpeedLimit = classes, 7
		network.Lanes = append(network.Lanes, lane)
	}
	prepared, err := PrepareNetwork(network)
	if err != nil {
		return nil, couplingMotionState{}, couplingMotionState{}, couplingForeignSweep{}, err
	}
	geometry := CouplingGeometryInput{Contract: CompactPairV1CouplingContract, Network: prepared.Network(),
		Sites: []CouplingSite{input.Network.sites[corridor.AssemblySiteID], input.Network.sites[corridor.SplitSiteID]}, Corridors: []CouplingCorridor{corridor}}
	n, err := prepareCouplingReservations(prepared, geometry)
	if err != nil {
		return nil, couplingMotionState{}, couplingMotionState{}, couplingForeignSweep{}, err
	}
	input.Network, input.Prepared, input.OrderContract = n, prepared, ExpressOrderContract
	plan, err := planCouplingReservation(input)
	if err != nil {
		return nil, couplingMotionState{}, couplingMotionState{}, couplingForeignSweep{}, err
	}
	c, err := prepareCouplingMotionContext(couplingMotionContextInput{Reservation: plan, Current: input, GroupID: "pair", ForeignIDs: []string{"parked"}})
	if err != nil {
		t.Fatal(err)
	}
	previous := remainingTestFind(t, c, couplingConnected, 1)
	next, err := c.stateAt(previous.Elapsed + 1)
	if err != nil {
		t.Fatal(err)
	}
	path, err := prepareCouplingParkedForeign(n, input.OrderContract, "parked", class, "connector-station", "connector-1")
	if err != nil {
		t.Fatal(err)
	}
	owners := maps.Clone(input.Owners)
	for _, r := range berthResources(path.berth) {
		owners[r] = podResourceOwner("parked")
	}
	view, err := sealCouplingForeignOwners(path, 0, -1, owners)
	if err != nil {
		t.Fatal(err)
	}
	return c, previous, next, couplingForeignSweep{Path: path, Tick: previous.Tick, ReservedThrough: -1, Owners: view}, nil
}

// No prepared network can put a foreign berth at the connector center. For
// large classes the network audit refuses it. For the other classes
// reservation planning refuses a corridor cell that shares no exclusion
// resource with the nearby foreign path. A berth well outside the body
// clearance passes both and the actual foreign sweep check accepts it.
func TestCouplingForeignSweepAtConnectorCenter(t *testing.T) {
	t.Parallel()
	for _, class := range []VehicleClass{CompactClass, LegacyClass, ExpressClass} {
		for _, test := range []struct {
			name   string
			offset float64
			refuse bool
		}{{"center", 0, true}, {"clear", 3 * largeClearance, false}} {
			t.Run(string(class)+"/"+test.name, func(t *testing.T) {
				t.Parallel()
				c, previous, next, sweep, err := couplingConnectorParkedSweep(t, class, test.offset)
				if test.refuse {
					want := "lacks a shared exclusion resource"
					if largeVehicleClass(class) {
						want = "nonincident paths"
					}
					if err == nil || !strings.Contains(err.Error(), want) {
						t.Fatal("preparation accepted a foreign berth at the connector center", err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if err = c.checkForeignSweeps(previous, next, []couplingForeignSweep{sweep}); err != nil {
					t.Fatal("foreign sweep check refused a parked pod outside the body clearance", err)
				}
			})
		}
	}
}

// A stationary Compact pod on the rear member's route, offset along that
// route from the rear member's previous distance. The owner ledger is
// fabricated: the pod owns its own footprint cells even where the train
// owns them in the actual ledger. Step never builds such a ledger, so this
// shows only that the sweep checks are live.
func couplingConnectorRouteSweep(t *testing.T, offset float64) (*couplingMotionContext, couplingMotionState, couplingMotionState, couplingForeignSweep) {
	t.Helper()
	input := couplingMotionFixture(t, false, false)
	plan, err := planCouplingReservation(input)
	if err != nil {
		t.Fatal(err)
	}
	c, err := prepareCouplingMotionContext(couplingMotionContextInput{Reservation: plan, Current: input, GroupID: "pair", ForeignIDs: []string{"foreign"}})
	if err != nil {
		t.Fatal(err)
	}
	previous := remainingTestFind(t, c, couplingConnected, 1)
	next, err := c.stateAt(previous.Elapsed + 1)
	if err != nil {
		t.Fatal(err)
	}
	path, err := prepareCouplingForeignPath(input.Network, input.OrderContract, "foreign", CompactClass, c.reservation.routes[1].route)
	if err != nil {
		t.Fatal(err)
	}
	distance := previous.Distances[1] + offset
	if distance < 0 {
		t.Fatal("foreign offset leaves the route", distance)
	}
	through := -1
	for i := range path.blocks.len() {
		if path.blocks.at(i).end >= distance {
			through = i
			break
		}
	}
	owners := maps.Clone(input.Owners)
	for _, b := range path.blocks.span(0, through+1) {
		for _, r := range b.resources {
			if resourceReleaseDistance(b, r) > distance {
				owners[r] = podResourceOwner("foreign")
			}
		}
	}
	view, err := sealCouplingForeignOwners(path, distance, through, owners)
	if err != nil {
		t.Fatal(err)
	}
	sweep := couplingForeignSweep{Path: path, Tick: previous.Tick, Distance: distance, NextDistance: distance, ReservedThrough: through, Owners: view}
	return c, previous, next, sweep
}

// The actual foreign sweep check refuses a foreign body inside the body
// clearance of a connected train and accepts one outside it. Behind the
// rear member only the body check can refuse; at the connector center the
// body and connector checks both refuse.
func TestCouplingForeignSweepNearConnectedTrain(t *testing.T) {
	t.Parallel()
	profile, _ := LookupCouplingProfile(CompactPairV1CouplingContract)
	for _, test := range []struct {
		name   string
		offset float64
		want   string
	}{
		{"behind_rear", -5, "class-pair exclusion"},
		{"at_connector", profile.CenterSpacingMeters / 2, "couplingMotionInvariant"},
		{"clear", -3 * largeClearance, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c, previous, next, sweep := couplingConnectorRouteSweep(t, test.offset)
			err := c.checkForeignSweeps(previous, next, []couplingForeignSweep{sweep})
			switch test.want {
			case "":
				if err != nil {
					t.Fatal("foreign sweep check refused a body outside the clearance", err)
				}
			case "couplingMotionInvariant":
				if !errors.Is(err, errCouplingMotionInvariant) {
					t.Fatal("foreign sweep check accepted a body at the connector center", err)
				}
				t.Log("refusal:", err)
			default:
				if !errors.Is(err, errCouplingMotionInvariant) || !strings.Contains(err.Error(), test.want) {
					t.Fatal("foreign sweep check accepted a body behind the rear member", err)
				}
			}
		})
	}
}
