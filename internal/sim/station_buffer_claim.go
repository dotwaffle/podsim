package sim

import "slices"

type bufferBerthClaim struct {
	resource resource
	owner    *vehicle
}

// bufferBerthClaims accepts free resources and remote empty claims that
// passenger traffic can make yield. Admitted or held resources stay protected.
// The caller releases accepted claims only inside a complete-path trial and
// restores them when that trial fails.
func (s *Simulation) bufferBerthClaims(head *vehicle, berth Berth) ([2]bufferBerthClaim, bool) {
	var claims [2]bufferBerthClaim
	for index, r := range berthResources(berth) {
		owner := s.owners[r]
		if owner.isZero() || owner.isPod(head.Pod.ID) {
			continue
		}
		remote := s.ownerVehicle(owner)
		if !s.bufferClaimCanYield(head, remote, berth, r) {
			return claims, false
		}
		claims[index] = bufferBerthClaim{resource: r, owner: remote}
	}
	return claims, true
}

func (s *Simulation) bufferClaimCanYield(head, remote *vehicle, berth Berth, r resource) bool {
	if !head.carriesPassengers() && !s.assigned(head.Pod.ID) {
		return false
	}
	// A coupled member's receiving claim belongs to the committed train.
	if remote == nil || remote.couplingID != "" || remote.RelocatingTo == "" || remote.destination.ID != berth.ID ||
		remote.Pod.Occupied || remote.carriesPassengers() || s.assigned(remote.Pod.ID) ||
		remote.Pod.BerthID == berth.ID || s.relocationDestinationAdmitted(remote) {
		return false
	}
	if releaseAt, held := remote.routeReleases[r]; held && releaseAt > remote.distance {
		return false
	}
	for resources := range remote.blocks.spanResources(0, min(remote.reservedThrough+1, remote.blocks.len())) {
		if slices.Contains(resources, r) {
			return false
		}
	}
	for follower := remote.follower; follower != 0; follower = s.vehicles[follower-1].follower {
		member := &s.vehicles[follower-1]
		if releaseAt, held := member.routeReleases[r]; held && releaseAt > member.distance {
			return false
		}
	}
	return true
}

// finishBufferClaimYield runs after the head owns its complete berth path.
// Released empty pods choose another destination in the next dispatch pass.
// Their routes must remain valid until all queued admission intents finish.
func (s *Simulation) finishBufferClaimYield(claims [2]bufferBerthClaim) {
	for _, claim := range claims {
		if claim.owner != nil {
			delete(claim.owner.routeReleases, claim.resource)
			claim.owner.nextRelease = 0
		}
	}
	for index, claim := range claims {
		remote := claim.owner
		if remote == nil || index > 0 && remote == claims[0].owner {
			continue
		}
		if s.positioning == PositioningGuarded && remote.Rebalancing {
			remote.Rebalancing, remote.released = false, true
		}
	}
}
