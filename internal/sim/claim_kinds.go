package sim

import "slices"

// claimKind classifies one resource that a pod owns or retains.
type claimKind uint8

const (
	claimNotHeld   claimKind = iota // v neither owns nor retains r
	claimCommitted                  // coupling or approach commitment
	claimOccupied                   // under the body of v
	claimStopping                   // granted track that v needs to stop
	claimRetained                   // tail retention of v
	claimLent                       // a follower of v retains r
	claimService                    // unused destination claim of an empty move
	claimOther                      // any other owned resource
)

// claimKind returns the kind of the claim of v on r. The tests run in the
// order of the constants, and the first match wins. Only claimService is
// revocable.
func (s *Simulation) claimKind(v *vehicle, r resource) claimKind {
	switch {
	case !s.owners[r].isPod(v.Pod.ID) && !v.retains(r):
		return claimNotHeld
	case s.claimCommitted(v, r):
		return claimCommitted
	case s.claimOccupied(v, r):
		return claimOccupied
	case v.claimStopping(r):
		return claimStopping
	case v.retainsPast(r):
		return claimRetained
	case s.claimLent(v, r):
		return claimLent
	case v.claimService(r):
		return claimService
	default:
		return claimOther
	}
}

// revocable reports whether a claim can be released without a physical
// transition.
func (s *Simulation) revocable(v *vehicle, r resource) bool {
	return s.claimKind(v, r) == claimService
}

// retains reports whether v.routeReleases has an entry for r.
func (v *vehicle) retains(r resource) bool {
	_, retained := v.routeReleases[r]
	return retained
}

// retainsPast reports whether v retains r past its distance.
func (v *vehicle) retainsPast(r resource) bool {
	releaseAt, retained := v.routeReleases[r]
	return retained && releaseAt > v.distance
}

// claimCommitted reports whether a train or an approach to a train binds r.
// A member of a train has a coupling ID. An approach member has none until
// adoption, but adoption needs the receiving claims of an empty member.
func (s *Simulation) claimCommitted(v *vehicle, r resource) bool {
	if v.couplingID != "" || s.couplingApproachMember(v.Pod.ID) || s.owners[r].kind == groupOwnerKind {
		return true
	}
	listed := func(claim couplingClaim) bool { return claim.Resource == r }
	for _, group := range s.couplingGroups {
		if slices.ContainsFunc(group.context.claims, listed) || slices.ContainsFunc(group.context.reservation.PreservedClaims, listed) {
			return true
		}
	}
	return false
}

// claimOccupied reports whether r is under the body of v: in its footprint
// at the current distance, at the berth where v is, or at its origin berth
// before v releases the origin.
func (s *Simulation) claimOccupied(v *vehicle, r resource) bool {
	if v.Pod.BerthID != "" {
		station, _ := s.station(v.Pod.StationID)
		berth, _ := station.berth(v.Pod.BerthID)
		if ofBerth(berth, r) {
			return true
		}
	}
	if v.Pod.Activity == Traveling && v.inFootprint(r, v.blockIndex, v.distance) {
		return true
	}
	return !v.originReleased && ofBerth(v.origin, r)
}

// inFootprint reports whether footprint(through, distance) contains r. It
// does not make the footprint.
func (v *vehicle) inFootprint(r resource, through int, distance float64) bool {
	for _, b := range v.blocks.span(0, through+1) {
		if slices.Contains(b.resources, r) && resourceReleaseDistance(b, r) > distance {
			return true
		}
	}
	return distance < v.originTail() && ofBerth(v.origin, r)
}

// claimStopping reports whether r is in the reserved span of v.
func (v *vehicle) claimStopping(r resource) bool {
	for resources := range v.blocks.spanResources(0, min(v.reservedThrough+1, v.blocks.len())) {
		if slices.Contains(resources, r) {
			return true
		}
	}
	return false
}

// claimLent reports whether a follower of v retains r past its distance.
func (s *Simulation) claimLent(v *vehicle, r resource) bool {
	for follower := v.follower; follower != 0; follower = s.vehicles[follower-1].follower {
		if s.vehicles[follower-1].retainsPast(r) {
			return true
		}
	}
	return false
}

// claimService reports whether r is a destination claim of an empty move
// that v does not use yet.
func (v *vehicle) claimService(r resource) bool {
	return v.RelocatingTo != "" && ofBerth(v.destination, r) &&
		!v.Pod.Occupied && !v.carriesPassengers() && v.Pod.BerthID != v.destination.ID
}

// ofBerth reports whether r is the berth or the berth node of berth.
func ofBerth(berth Berth, r resource) bool {
	claims := berthResources(berth)
	return r == claims[0] || r == claims[1]
}
