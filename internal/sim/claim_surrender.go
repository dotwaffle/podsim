package sim

import "math"

// incidentOutstanding reports whether an incident is outstanding (section
// 9.5 of the incident suspension contract, and section 9.4 of the incident
// emergency contract): faults are on, and an active fault record exists or
// a pod has the fault hold, or an emergency record exists, or a pod has
// the emergency hold. A fault recovery keeps the fault hold until its
// arrival clears purpose 3, so the gate stays on through the recovery
// after the last clear. The emergency clauses need no switch: a record
// exists only with emergencies on (E7), and the stage 1 test entry can set
// the emergency hold without the switch, which section 9.4 also counts.
func (s *Simulation) incidentOutstanding() bool {
	if len(s.emergencies) > 0 {
		return true
	}
	holds := emergencyHold
	if s.faultsOn {
		if len(s.faults) > 0 {
			return true
		}
		holds |= faultHold
	}
	for index := range s.vehicles {
		if s.vehicles[index].withdrawn&holds != 0 {
			return true
		}
	}
	return false
}

// surrenderWaitingClaims is the first phase of admit while an incident is
// outstanding (section 9.5 of the incident suspension contract). It runs
// after every claim producer of the tick. In pod ID order, each healthy
// pod in no group that owns an unused service claim on its destination
// berth and waits gives up that claim, so a pod that needs the berth can
// take it in the second phase. A withdrawn head makes no other pod yield,
// so only the owner gives up its claim. The pod keeps its route, its
// destination, its relocation, and every other claim and grant.
func (s *Simulation) surrenderWaitingClaims() {
	if !s.incidentOutstanding() {
		return
	}
	for index := range s.vehicles {
		v := &s.vehicles[index]
		if v.faulted || !s.ownsServiceClaim(v) || s.operationalMember(v) != nil || !s.admissionWaits(v) {
			continue
		}
		s.surrenderServiceClaims(v)
	}
}

// ownsServiceClaim reports whether v owns a resource of its destination
// berth that claimKind classifies as claimService. Only an empty pod with
// a relocation has such a claim.
func (s *Simulation) ownsServiceClaim(v *vehicle) bool {
	if v.destination.ID == "" {
		return false
	}
	for _, r := range berthResources(v.destination) {
		if s.owners[r].isPod(v.Pod.ID) && s.claimKind(v, r) == claimService {
			return true
		}
	}
	return false
}

// admissionWaits reports whether the second phase of admit would refuse
// the request of v: a resource of the span that v would request has an
// owner other than v. It builds the request as admit does, and it writes
// nothing. It needs no incident owner, so a pod behind a healthy pod that
// waits at a fault also waits.
//
// The caller passes only a pod with a destination berth, which is not in a
// group. For such a pod, assignTerminalBerth returns with no search, and
// grant does not use the buffer plan, so the terminal berth choice and the
// complete berth path of a buffer head cannot refuse it.
func (s *Simulation) admissionWaits(v *vehicle) bool {
	if ready := departs(v.Pod.Activity) && v.phaseTicks == 0; !ready && v.Pod.Activity != Traveling {
		return false
	}
	next, ok := s.admissionRequest(v)
	if !ok {
		return false
	}
	through := reservationEnd(&v.blocks, next)
	for resources := range v.blocks.spanResources(next, through+1) {
		for _, r := range resources {
			if owner := s.owners[r]; !owner.isZero() && !owner.isPod(v.Pod.ID) {
				return true
			}
		}
	}
	return false
}

// admissionRequest returns the first block that admit requests for v,
// after the terminal berth choice. It reports false when v is not ready to
// move, when its reservation reaches the end of its route, or when its
// reservation already covers the stopping distance at cruising speed plus
// the configured lookahead.
func (s *Simulation) admissionRequest(v *vehicle) (int, bool) {
	next := v.reservedThrough + 1
	if next >= v.blocks.len() {
		return 0, false
	}
	speed := math.Max(v.Pod.Speed, v.blocks.currentLane(v.blockIndex).SpeedLimit)
	horizon := speed*speed/(2*acceleration) + speed*s.reservationLookaheadSeconds
	if v.reservedThrough >= 0 && v.blocks.end(v.reservedThrough)-v.distance >= horizon && !v.link.compact {
		return 0, false
	}
	return next, true
}
