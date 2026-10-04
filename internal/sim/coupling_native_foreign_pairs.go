package sim

import (
	"maps"
	"math"
	"reflect"
	"slices"
)

// The later Step adapter derives these values from its actual group registry.
// No caller supplies a safety exemption or a replacement owner tag.
type couplingNativeForeignPair struct {
	context        *couplingMotionContext
	previous, next couplingMotionState
}

type nativeForeignPairProof struct {
	frame   *nativeForeignTick
	pair    couplingNativeForeignPair
	indices [2]int
	owners  *couplingMotionOwnerView
}

type nativeForeignPairMember struct {
	proof  *nativeForeignPairProof
	member int
}

// Route content is checked once, before immutable native route caching.
func (f *nativeForeignFleet) preparePairRoutes(contexts []*couplingMotionContext) error {
	f.pairs = make(map[*couplingMotionContext][2]int, len(contexts))
	used := make(map[string]bool, len(contexts)*2)
	groups := make(map[resourceOwner]bool, len(contexts))
	if len(contexts) > len(f.entries)/2 {
		return couplingMotionInvariant("native pair registry exceeds the actual fleet")
	}
	for _, c := range contexts {
		if c == nil || c.reservation.network != f.network || c.reservation.orderContract != f.orderContract || groups[c.owner] || c.owner.kind != groupOwnerKind || !boundedContractID(c.owner.id) {
			return couplingMotionInvariant("native pair registry has an invalid context or owner")
		}
		groups[c.owner] = true
		indices := [2]int{-1, -1}
		for member, m := range &c.reservation.members {
			if used[m.Vehicle.Pod.ID] {
				return couplingMotionInvariant("native pair registries share a pod")
			}
			used[m.Vehicle.Pod.ID] = true
			for index, entry := range f.entries {
				if entry.id != m.Vehicle.Pod.ID {
					continue
				}
				if entry.class != CompactClass || entry.routeVersion != m.RouteVersion || !reflect.DeepEqual(entry.route, c.reservation.routes[member].route) || !nativeForeignRouteCells(&f.source.vehicles[index], &c.reservation.routes[member]) {
					return couplingMotionInvariant("native pair route differs from its immutable context")
				}
				indices[member] = index
			}
			if indices[member] < 0 {
				return couplingMotionInvariant("native pair member is absent from the complete fleet")
			}
		}
		f.pairs[c] = indices
	}
	return nil
}

func (frame *nativeForeignTick) preparePairs(pairs []couplingNativeForeignPair) error {
	if len(pairs) != len(frame.fleet.pairs) {
		return couplingMotionInvariant("native pair snapshots omit a registered group")
	}
	frame.pairMembers = make(map[int]nativeForeignPairMember, len(pairs)*2)
	seen := make(map[*couplingMotionContext]bool, len(pairs))
	for _, pair := range pairs {
		indices, exists := frame.fleet.pairs[pair.context]
		if !exists || seen[pair.context] {
			return couplingMotionInvariant("native pair snapshot is unknown or duplicate")
		}
		seen[pair.context] = true
		proof := &nativeForeignPairProof{frame: frame, pair: pair, indices: indices}
		if err := proof.checkStates(); err != nil {
			return err
		}
		if pair.previous.Tick != frame.tick-1 || pair.next.Tick != frame.tick {
			return couplingMotionInvariant("native pair snapshot belongs to another transition")
		}
		view, err := sealCouplingMotionOwners(pair.context, pair.previous, frame.owners)
		if err != nil {
			return err
		}
		proof.owners = view
		expected := pair.context.membersAt(pair.previous)
		for member, index := range indices {
			fact := &frame.facts[index]
			if !nativeForeignPodEqual(fact.pod, expected[member].Pod) || fact.cabin.Pod != fact.pod || fact.distance != pair.previous.Distances[member] || fact.blockIndex != pair.previous.Cells[member] || fact.through != pair.context.through[member] || fact.link.leader != 0 || fact.follower != 0 || fact.compact.planned || !nativeForeignCabinEqual(fact.cabin, expected[member]) {
				return couplingMotionInvariant("native pair cabin or current pose differs from the actual fleet")
			}
			frame.pairMembers[index] = nativeForeignPairMember{proof: proof, member: member}
		}
	}
	return nil
}

func (proof *nativeForeignPairProof) checkStates() error {
	c := proof.pair.context
	if c == nil || proof.pair.previous.context != c || proof.pair.next.context != c || proof.pair.previous.Finished || proof.pair.previous.Elapsed >= c.ticks {
		return couplingMotionInvariant("native pair has an invalid previous context")
	}
	previous, err := c.stateAt(proof.pair.previous.Elapsed)
	if err != nil || previous != proof.pair.previous {
		return couplingMotionInvariant("native pair previous state is not its exact certificate")
	}
	next, err := c.stateAt(previous.Elapsed + 1)
	if err != nil || next != proof.pair.next {
		return couplingMotionInvariant("native pair next state is not its exact certificate")
	}
	return c.checkTickMotion(previous, next)
}

// The native station controller owns these two presentation fields.
// Every other Pod field remains an exact physical or cabin binding.
func nativeForeignPodEqual(actual, expected Pod) bool {
	expected.StationPhase = actual.StationPhase
	expected.ManeuverStationID = actual.ManeuverStationID
	return actual == expected
}

func nativeForeignCabinEqual(a, b couplingCabinMotion) bool {
	return nativeForeignPodEqual(a.Pod, b.Pod) && a.RouteVersion == b.RouteVersion && nativeForeignSameFloat(a.RiddenMeters, b.RiddenMeters) && a.RelocatingTo == b.RelocatingTo && slices.Equal(a.Riders, b.Riders) && slices.Equal(a.Stops, b.Stops) && slices.Equal(a.Boardings, b.Boardings)
}

func (frame *nativeForeignTick) pairProof(index int, member nativeForeignPairMember) (*nativeForeignProof, error) {
	pair := member.proof
	if pair == nil || pair.frame != frame || member.member < 0 || member.member >= 2 || pair.indices[member.member] != index {
		return nil, couplingMotionInvariant("native pair membership lacks its frozen proof")
	}
	path := frame.fleet.entries[index].path
	if path == nil {
		return nil, couplingMotionInvariant("native pair member has no canonical route")
	}
	before, next := pair.pair.previous, pair.pair.next
	view := &couplingForeignOwnerView{path: path, through: frame.facts[index].through, distance: before.Distances[member.member], owners: maps.Clone(pair.owners.owners)}
	raw := couplingForeignSweep{Path: path, Tick: frame.tick - 1, Distance: before.Distances[member.member], Speed: before.Speeds[member.member], NextDistance: next.Distances[member.member], NextSpeed: next.Speeds[member.member], ReservedThrough: frame.facts[index].through, Owners: view}
	proof := &nativeForeignProof{frame: frame, index: index, raw: raw, pair: member, nextPosition: next.Positions[member.member], nextRouteDistance: next.Distances[member.member]}
	return proof.freezeClaims(), nil
}

// Axis-aligned bounds conservatively cover the other certified connector sweep.
// They can deny a clear rotated case, but they cannot waive a body interaction.
func (c *couplingMotionContext) checkNativePairConnector(previous, next couplingMotionState, pair *nativeForeignPairProof, members [2][]laneSegment) error {
	if pair == nil || pair.pair.context == c {
		return couplingMotionInvariant("foreign pair connector has no other certified context")
	}
	other, exists, err := nativePairConnectorBox(pair.pair.context, pair.pair.previous, pair.pair.next)
	if err != nil || !exists {
		return err
	}
	profile, _ := LookupCouplingProfile(c.reservation.network.contract)
	radius := math.Hypot(profile.BodyLengthMeters, profile.BodyWidthMeters) / 2
	for _, segments := range members {
		for _, segment := range segments {
			if couplingSegmentBox(segment.from, segment.to, radius).intersects(other) {
				return couplingMotionInvariant("foreign train connector sweep touches a member body bound")
			}
		}
	}
	own, ownExists, err := nativePairConnectorBox(c, previous, next)
	if err != nil {
		return err
	}
	if ownExists && own.intersects(other) {
		return couplingMotionInvariant("certified train connector sweeps overlap")
	}
	return nil
}

func nativePairConnectorBox(c *couplingMotionContext, previous, next couplingMotionState) (couplingBox, bool, error) {
	var result couplingBox
	exists := false
	for _, state := range [2]couplingMotionState{previous, next} {
		_, connector, err := c.motionBodiesAt(state)
		if err != nil {
			return result, false, err
		}
		if connector == nil {
			continue
		}
		for _, corner := range connector.Corners {
			point := couplingSegmentBox(corner, corner, 0)
			if !exists {
				result, exists = point, true
			} else {
				result = couplingUnionBox(result, point)
			}
		}
	}
	return result, exists, nil
}
