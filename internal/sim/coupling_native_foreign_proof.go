package sim

import (
	"maps"
	"math"
	"slices"
)

type nativeForeignProof struct {
	frame             *nativeForeignTick
	index             int
	raw               couplingForeignSweep
	ordinary          ordinaryMoveResult
	limit             float64
	lane              int
	compact           nativeCompactMember
	approach          *nativeApproachProof
	pair              nativeForeignPairMember
	nextPosition      Point
	nextRouteDistance float64
	claims            []couplingClaim
	// faulted and faultCap bind the fault members of the fact. A changed
	// cap at rest, or a changed fault at a stop at the grant end, can
	// leave the step the same, so the checks compare the members too.
	faulted  bool
	faultCap float64
}

func (frame *nativeForeignTick) prepareProof(index int, compact nativeCompactMember) (*nativeForeignProof, error) {
	fact := &frame.facts[index]
	entry := &frame.fleet.entries[index]
	if member, exists := frame.pairMembers[index]; exists {
		return frame.pairProof(index, member)
	}
	if approach := frame.approachFronts[index]; approach != nil {
		return frame.approachProof(index, approach)
	}
	proof := &nativeForeignProof{frame: frame, index: index, compact: compact, nextPosition: fact.pod.Position, nextRouteDistance: fact.distance}
	if fact.pod.Activity != Traveling {
		if entry.parked == nil || fact.pod.StationID == "" || fact.pod.BerthID != entry.parked.berth.ID || fact.pod.Position != entry.parked.position || fact.pod.Speed != 0 {
			return nil, couplingMotionInvariant("native stationary pose is not its actual canonical berth")
		}
		switch fact.pod.Activity {
		case Idle, Boarding, Unloading, Continuing, DepartingEmpty:
		default:
			return nil, couplingMotionInvariant("native stationary activity is unsupported")
		}
		owners, err := frame.sealOwners(index, entry.parked)
		if err != nil {
			return nil, err
		}
		proof.raw = couplingForeignSweep{Path: entry.parked, Tick: frame.tick - 1, ReservedThrough: -1, Owners: owners}
		return proof.freezeClaims(), nil
	}
	if entry.path == nil {
		return nil, couplingMotionInvariant("native moving pose has no canonical route")
	}
	blocks := &entry.path.blocks
	if compact.group == nil {
		if err := frame.checkOrdinaryPose(index); err != nil {
			return nil, err
		}
	}
	owners, err := frame.sealOwners(index, entry.path)
	if err != nil {
		return nil, err
	}
	proof.raw = couplingForeignSweep{Path: entry.path, Tick: frame.tick - 1, Distance: fact.distance, Speed: fact.pod.Speed, ReservedThrough: fact.through, Owners: owners}
	if compact.group != nil {
		planned := compact.group.planned[compact.member]
		lane := len(blocks.route) - 1
		proof.nextRouteDistance = blocks.lanes[lane].start + planned.position
		proof.raw.NextDistance, proof.raw.NextSpeed = proof.nextRouteDistance, planned.speed
		proof.nextPosition = frame.fleet.source.lanePosition(blocks.lanes[lane].geometry, &blocks.route[lane], planned.position)
		return proof.freezeClaims(), nil
	}
	proof.limit, err = nativeForeignLimit(*fact, blocks)
	if err != nil {
		return nil, err
	}
	if capErr := frame.checkPredecessorCap(index); capErr != nil {
		return nil, capErr
	}
	if capErr := checkFaultCap(fact, proof.limit); capErr != nil {
		return nil, capErr
	}
	proof.lane = blocks.locate(fact.blockIndex, 0)
	proof.ordinary = nativeForeignStep(fact, blocks, proof.lane, proof.limit)
	if !finite(proof.ordinary.distance) || !finite(proof.ordinary.speed) || !finite(proof.ordinary.commandedSpeed) || proof.ordinary.distance < fact.distance || proof.ordinary.distance > proof.limit || proof.ordinary.speed < 0 {
		return nil, couplingMotionInvariant("native ordinary kernel output is invalid")
	}
	proof.raw.NextDistance, proof.raw.NextSpeed = proof.ordinary.distance, proof.ordinary.speed
	proof.nextRouteDistance = proof.ordinary.distance
	proof.nextPosition, err = frame.ordinaryPosition(index, proof.ordinary.distance)
	if err != nil {
		return nil, err
	}
	return proof.freezeClaims(), nil
}

// This repeats no motion formula. It binds the actual pre-movement cap producer.
func (frame *nativeForeignTick) checkPredecessorCap(index int) error {
	fact := &frame.facts[index]
	if fact.link.leader == 0 || fact.link.compact {
		return nil
	}
	ahead := fact.link.leader - 1
	if ahead < 0 || ahead >= len(frame.facts) || frame.facts[ahead].follower != index+1 {
		return couplingMotionInvariant("native cap predecessor is not reciprocal")
	}
	predecessor := &frame.facts[ahead]
	if predecessor.pod.Activity != Traveling {
		if !math.IsInf(fact.cap, 1) {
			return couplingMotionInvariant("native stopped predecessor cap differs from its producer")
		}
		return nil
	}
	followerPath := frame.fleet.entries[index].path
	leaderPath := frame.fleet.entries[ahead].path
	if followerPath == nil || leaderPath == nil || fact.link.lane < 0 || fact.link.leaderLane < 0 || fact.link.lanes < 1 || fact.link.lane+fact.link.lanes > len(followerPath.blocks.route) || fact.link.leaderLane+fact.link.lanes > len(leaderPath.blocks.route) || predecessor.blockIndex < 0 || predecessor.blockIndex >= leaderPath.blocks.len() {
		return couplingMotionInvariant("native cap has an invalid shared route")
	}
	current := leaderPath.blocks.locate(predecessor.blockIndex, 0)
	leaderLane := min(max(current, fact.link.leaderLane), fact.link.leaderLane+fact.link.lanes-1)
	lane := fact.link.lane + leaderLane - fact.link.leaderLane
	if followerPath.blocks.route[lane].ID != leaderPath.blocks.route[leaderLane].ID {
		return couplingMotionInvariant("native cap maps another canonical lane")
	}
	position := followerPath.blocks.lanes[lane].start + predecessor.distance - leaderPath.blocks.lanes[leaderLane].start
	limit := position + stoppingDistance(predecessor.pod.Speed) - fact.link.clearance
	own := fact.distance + stoppingDistance(fact.pod.Speed)
	expected := max(min(own, limit), limit-fact.pod.Speed*platoonReactionSeconds)
	if fact.link.buffer {
		blocks := followerPath.blocks
		geometry, _ := linkEnds(&blocks, fact.link)
		expected = min(expected, geometry)
	}
	if !nativeForeignSameFloat(fact.cap, expected) {
		return couplingMotionInvariant("native predecessor cap differs from its frozen producer facts")
	}
	return nil
}

// checkFaultCap checks the fault members of a moving fact as
// checkPredecessorCap checks a follower cap. Fault start fixes the cap,
// so no frame fact can produce it again. A faulted pod keeps its cap
// between its distance and its grant end (F3), and a pod without a fault
// has no cap.
func checkFaultCap(fact *nativeForeignFact, limit float64) error {
	if !fact.faulted {
		if fact.faultCap != 0 {
			return couplingMotionInvariant("native pod without a fault has a fault cap")
		}
		return nil
	}
	if !finite(fact.faultCap) || fact.faultCap < fact.distance || fact.faultCap > limit {
		return couplingMotionInvariant("native fault cap is outside its pose and grants")
	}
	return nil
}

func (frame *nativeForeignTick) ordinaryPosition(index int, distance float64) (Point, error) {
	fact := &frame.facts[index]
	blocks := &frame.fleet.entries[index].path.blocks
	cursor := fact.blockIndex
	for distance >= blocks.at(cursor).end {
		if cursor+1 == blocks.len() {
			if fact.destination.ID == "" {
				return fact.pod.Position, nil
			}
			node, exists := frame.fleet.entries[index].path.network.prepared.network.Node(fact.destination.Node)
			if !exists {
				return Point{}, couplingMotionInvariant("native arrival has no actual destination node")
			}
			return node.Position, nil
		}
		if cursor == fact.through {
			break
		}
		cursor++
	}
	lane := blocks.locate(cursor, 0)
	local := distance - blocks.lanes[lane].start
	return frame.fleet.source.lanePosition(blocks.lanes[lane].geometry, &blocks.route[lane], local), nil
}

func checkNativeForeignSweep(sweep couplingForeignSweep) error {
	proof := sweep.native
	if sweep.Path == nil || proof == nil || proof.frame == nil || proof.frame.fleet == nil || proof.index < 0 || proof.index >= len(proof.frame.facts) || proof.frame.proofs[sweep.Path.id] != proof {
		return couplingMotionInvariant("native foreign certificate has no complete frozen producer")
	}
	raw := proof.raw
	if raw.Owners == nil || len(raw.Owners.owners) != len(proof.claims) {
		return couplingMotionInvariant("native owner certificate changed after capture")
	}
	for _, claim := range proof.claims {
		if raw.Owners.owners[claim.Resource] != claim.Expected || proof.frame.owners[claim.Resource] != claim.Expected {
			return couplingMotionInvariant("native owner certificate changed after capture")
		}
	}
	if sweep.Path != raw.Path || sweep.Owners != raw.Owners || sweep.Tick != raw.Tick || sweep.ReservedThrough != raw.ReservedThrough || !nativeForeignSameFloat(sweep.Distance, raw.Distance) || !nativeForeignSameFloat(sweep.Speed, raw.Speed) || !nativeForeignSameFloat(sweep.NextDistance, raw.NextDistance) || !nativeForeignSameFloat(sweep.NextSpeed, raw.NextSpeed) {
		return couplingMotionInvariant("native foreign sweep differs from its exact frozen command")
	}
	if fact := &proof.frame.facts[proof.index]; fact.faulted != proof.faulted || !nativeForeignSameFloat(fact.faultCap, proof.faultCap) {
		return couplingMotionInvariant("native fault members changed after certification")
	}
	if proof.approach != nil {
		return proof.approach.check(proof)
	}
	if proof.pair.proof != nil {
		return proof.pair.proof.checkStates()
	}
	if proof.compact.group == nil && !raw.Path.parked {
		fact := proof.frame.facts[proof.index]
		expected := nativeForeignStep(&fact, &raw.Path.blocks, proof.lane, proof.limit)
		if expected != proof.ordinary {
			return couplingMotionInvariant("native ordinary command changed after certification")
		}
	}
	return nil
}

// A later Step adapter checks actual publications before end-of-tick release.
// This helper compares values, not a caller success flag or revision.
func (frame *nativeForeignTick) checkApplied(s *Simulation) error {
	if frame == nil || frame.fleet == nil || frame.fleet.source != s || frame.tick != s.tick || len(s.vehicles) != len(frame.facts) {
		return couplingMotionInvariant("native application belongs to another fleet or tick")
	}
	if !maps.Equal(s.owners, frame.owners) {
		return couplingMotionInvariant("native ledger changed inside the frozen movement stage")
	}
	for index, fact := range frame.facts {
		proof := frame.proofs[fact.pod.ID]
		if proof == nil {
			continue
		}
		v := &s.vehicles[index]
		if v.Pod.ID != fact.pod.ID || v.Pod.Class != fact.pod.Class || !nativeForeignSameFloat(v.distance, proof.nextRouteDistance) || !nativeForeignSameFloat(v.Pod.Speed, proof.raw.NextSpeed) || v.Pod.Position != proof.nextPosition {
			return couplingMotionInvariant("native applied motion differs from the frozen complete tick")
		}
		if v.faulted != proof.faulted || !nativeForeignSameFloat(v.faultCap, proof.faultCap) {
			return couplingMotionInvariant("native fault members changed inside the frozen movement stage")
		}
	}
	return nil
}

func (proof *nativeForeignProof) freezeClaims() *nativeForeignProof {
	size := len(proof.raw.Owners.owners)
	if work := proof.frame.work; work != nil {
		// The appends below stay in the array that Grow gives.
		proof.claims = slices.Grow(work.claims[proof.index][:0], size)
		work.claims[proof.index] = proof.claims
	} else {
		proof.claims = make([]couplingClaim, 0, size)
	}
	for r, owner := range proof.raw.Owners.owners {
		proof.claims = append(proof.claims, couplingClaim{Resource: r, Expected: owner})
	}
	slices.SortFunc(proof.claims, func(a, b couplingClaim) int { return compareCouplingResource(a.Resource, b.Resource) })
	return proof
}

func (frame *nativeForeignTick) checkOrdinaryPose(index int) error {
	fact := &frame.facts[index]
	path := frame.fleet.entries[index].path
	if fact.blockIndex < 0 || fact.blockIndex >= path.blocks.len() {
		return couplingMotionInvariant("native ordinary current cell is invalid")
	}
	lane := path.blocks.locate(fact.blockIndex, 0)
	current := &path.blocks.route[lane]
	local := fact.distance - path.blocks.lanes[lane].start
	if fact.pod.LaneID == "" {
		if fact.distance != 0 || fact.pod.Speed != 0 {
			return couplingMotionInvariant("native departure has nonzero motion before lane entry")
		}
		local = 0
	} else if fact.pod.LaneID != current.ID || !nativeForeignSameFloat(fact.pod.LaneDistance, local) {
		return couplingMotionInvariant("native current lane pose is not its exact ordinary publication")
	}
	expected := frame.fleet.source.lanePosition(path.blocks.lanes[lane].geometry, current, local)
	if fact.pod.Position != expected || fact.pod.Speed > current.SpeedLimit {
		return couplingMotionInvariant("native ordinary current geometry or lane speed is invalid")
	}
	return nil
}
