package sim

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

// bufferClaimCanYield reports whether head can take the claim of remote on
// r, a resource of berth. Passenger traffic in service takes only a
// revocable claim of a remote pod in service that goes to the same berth and
// has no assigned trip.
func (s *Simulation) bufferClaimCanYield(head, remote *vehicle, berth Berth, r resource) bool {
	if !head.inService() || !head.carriesPassengers() && !s.assigned(head.Pod.ID) {
		return false
	}
	if remote == nil || !remote.inService() || remote.destination.ID != berth.ID || s.assigned(remote.Pod.ID) {
		return false
	}
	return s.revocable(remote, r)
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
