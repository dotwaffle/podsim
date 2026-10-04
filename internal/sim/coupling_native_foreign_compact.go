package sim

import "slices"

type nativeCompactProof struct {
	members           []int
	previous, planned []compactQueueState
	bounds            compactQueueBounds
	recovery          compactQueueRecovery
	lane              string
}

type nativeCompactMember struct {
	group  *nativeCompactProof
	member int
}

func (frame *nativeForeignTick) compactCertificates(previous, next []*compactBufferGroup) (map[int]nativeCompactMember, error) {
	result := make(map[int]nativeCompactMember)
	if len(previous) != len(next) {
		return nil, couplingMotionInvariant("native compact group plan is incomplete")
	}
	for g, group := range previous {
		following := next[g]
		if group == nil || following == nil || len(group.members) < 1 || len(group.members) > compactQueueMaxMembers || !slices.Equal(group.members, following.members) || group.lane != following.lane || group.bounds != following.bounds {
			return nil, couplingMotionInvariant("native compact planned membership changed")
		}
		proof := &nativeCompactProof{members: slices.Clone(group.members), bounds: group.bounds, lane: group.lane, recovery: following.recovery}
		proof.recovery.targets = slices.Clone(following.recovery.targets)
		proof.recovery.landingSpeeds = slices.Clone(following.recovery.landingSpeeds)
		for member, index := range group.members {
			if index < 0 || index >= len(frame.facts) {
				return nil, couplingMotionInvariant("native compact member is outside the complete fleet")
			}
			if _, exists := result[index]; exists {
				return nil, couplingMotionInvariant("native compact member belongs to two groups")
			}
			fact := &frame.facts[index]
			path := frame.fleet.entries[index].path
			if path == nil || fact.pod.Activity != Traveling || fact.pod.LaneID != group.lane || !fact.compact.planned || path.blocks.route[len(path.blocks.route)-1].ID != group.lane || fact.through < 0 || fact.through >= path.blocks.len() {
				return nil, couplingMotionInvariant("native compact plan lacks its actual member or route")
			}
			if member == 0 && fact.link.leader != 0 || member > 0 && (fact.link.leader != group.members[member-1]+1 || !fact.link.compact) || member+1 < len(group.members) && fact.follower != group.members[member+1]+1 || member+1 == len(group.members) && fact.follower != 0 {
				return nil, couplingMotionInvariant("native compact plan disagrees with actual reciprocal links")
			}
			start := path.blocks.lanes[len(path.blocks.route)-1].start
			state := compactQueueState{position: fact.pod.LaneDistance, speed: fact.pod.Speed, stopBoundary: path.blocks.at(fact.through).end - start}
			planned := fact.compact.state
			lo, hi := compactQueueSpeedRange(state.speed)
			if planned.speed < lo || planned.speed > hi || planned.stopBoundary != state.stopBoundary || !nativeForeignSameFloat(planned.position, compactQueueStep(state, planned.speed).position) {
				return nil, couplingMotionInvariant("native compact command differs from its local exact step")
			}
			proof.previous = append(proof.previous, state)
			proof.planned = append(proof.planned, planned)
			result[index] = nativeCompactMember{group: proof, member: member}
		}
		if err := compactQueueRecoveryValidate(proof.planned, proof.recovery); err != nil {
			return nil, couplingMotionInvariant("native compact planned recovery is invalid")
		}
		for i, state := range proof.previous {
			var leader *compactQueueState
			if i > 0 {
				leader = &proof.previous[i-1]
			}
			if !compactQueueFits(state, proof.bounds, leader) {
				return nil, couplingMotionInvariant("native compact previous envelope is invalid")
			}
		}
	}
	for i, fact := range frame.facts {
		if _, exists := result[i]; fact.compact.planned && !exists {
			return nil, couplingMotionInvariant("native compact command has no whole-group proof")
		}
	}
	return result, nil
}
