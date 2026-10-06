package sim

import (
	"errors"
	"maps"
	"slices"
)

type couplingNativeTick struct {
	frame      *nativeForeignTick
	steps      []couplingMotionStep
	writes     []couplingOwnerWrite
	approaches []couplingApproachTransition
	adoptions  []couplingNativeGroup
	adopted    []bool
}

// couplingTickWork holds the buffers of the native tick of one simulation.
// Each planned tick clears and fills them again. The frame and the ledger
// of a tick are read only until that tick ends, so no later tick can
// change them while a reader uses them.
type couplingTickWork struct {
	owners    map[resource]resourceOwner
	proofs    map[string]*nativeForeignProof
	facts     []nativeForeignFact
	retained  []map[resource]float64
	views     []map[resource]resourceOwner
	claims    [][]couplingClaim
	ledger    map[resource]resourceOwner
	touched   map[resource]bool
	newClaims map[resource]bool
}

func (s *Simulation) nativeCouplingWork() *couplingTickWork {
	if s.couplingWork == nil {
		s.couplingWork = &couplingTickWork{owners: make(map[resource]resourceOwner), proofs: make(map[string]*nativeForeignProof),
			ledger: make(map[resource]resourceOwner), touched: make(map[resource]bool), newClaims: make(map[resource]bool)}
	}
	return s.couplingWork
}

// Planning freezes all controllers before any pod leaves its berth or moves.
func (s *Simulation) planNativeCouplingTick() (*couplingNativeTick, error) {
	if len(s.couplingGroups) == 0 && len(s.couplingApproaches) == 0 {
		s.couplingFleet = nil
		return nil, nil
	}
	contexts := make([]*couplingMotionContext, len(s.couplingGroups))
	pairs := make([]couplingNativeForeignPair, len(s.couplingGroups))
	for i, group := range s.couplingGroups {
		c := group.context
		if c == nil || c.reservation.network != s.couplingNetwork || group.formationTick != c.formationTick || group.state.Tick != s.tick-1 {
			return nil, couplingMotionInvariant("native registry has a stale context or clock")
		}
		for _, member := range &c.reservation.members {
			v := s.findVehicle(member.Vehicle.Pod.ID)
			if v == nil || v.couplingID != c.owner.id {
				return nil, couplingMotionInvariant("native registry does not own its actual members")
			}
		}
		next, err := c.stateAt(group.state.Elapsed + 1)
		if err != nil {
			return nil, err
		}
		contexts[i] = c
		pairs[i] = couplingNativeForeignPair{context: c, previous: group.state, next: next}
	}

	if !s.couplingFleetMatches(contexts) {
		fleet, err := prepareNativeForeignFleetBound(s, s.couplingNetwork, s.orderContract, contexts...)
		if err != nil {
			return nil, err
		}
		s.couplingFleet = fleet
	}
	approaches, err := s.planCouplingApproaches()
	if err != nil {
		return nil, err
	}
	work := s.nativeCouplingWork()
	frame, err := buildNativeForeignApproachTick(s, s.couplingFleet, work, approaches, pairs...)
	if err != nil {
		return nil, err
	}
	if len(frame.proofs) != len(s.vehicles) {
		return nil, couplingMotionInvariant("native tick does not certify the complete fleet")
	}
	for _, fact := range frame.facts {
		if frame.proofs[fact.pod.ID] == nil {
			return nil, couplingMotionInvariant("native tick omits an actual pod")
		}
	}
	for _, approach := range approaches {
		if err := frame.checkApproachForeign(approach); err != nil {
			return nil, err
		}
	}
	tick := &couplingNativeTick{frame: frame, steps: make([]couplingMotionStep, len(contexts)), approaches: approaches, adoptions: make([]couplingNativeGroup, len(approaches)), adopted: make([]bool, len(approaches))}
	ledger, touched := work.ledger, work.touched
	clear(ledger)
	clear(touched)
	maps.Copy(ledger, frame.owners)
	for i, c := range contexts {
		indices := s.couplingFleet.pairs[c]
		step, err := planCouplingMotion(couplingMotionInput{Context: c, Previous: pairs[i].previous,
			Owners: frame.pairMembers[indices[0]].proof.owners, Foreign: frame.sweepsFor(c)})
		if err != nil {
			return nil, err
		}
		tick.steps[i] = step
		for _, write := range step.Writes {
			if touched[write.Resource] || ledger[write.Resource] != write.Expected {
				return nil, couplingMotionInvariant("native pair writes conflict before movement")
			}
			touched[write.Resource] = true
			putCouplingOwner(ledger, write.Resource, write.Next)
			tick.writes = append(tick.writes, write)
		}
	}
	for i, step := range tick.steps {
		if !step.State.Finished {
			continue
		}
		writes, err := s.planCouplingRetirement(contexts[i], step, ledger)
		if err != nil {
			return nil, err
		}
		for _, write := range writes {
			putCouplingOwner(ledger, write.Resource, write.Next)
			tick.writes = append(tick.writes, write)
		}
	}

	accepted := make([][2]string, 0, len(approaches))
	newClaims := work.newClaims
	clear(newClaims)
	for i, approach := range approaches {
		if !approach.step.Ready {
			continue
		}
		members := [2]string{approach.context.members[0].id, approach.context.members[1].id}
		proposed := slices.Concat(accepted, [][2]string{members})
		if err := s.checkCouplingCheckpointBatchWork(proposed); err != nil {
			if errors.Is(err, errCouplingReservationDenied) {
				continue
			}
			return nil, err
		}
		group, initial, err := s.prepareCouplingAdoption(approach, i)
		if err != nil {
			if errors.Is(err, errCouplingReservationDenied) {
				continue
			}
			return nil, err
		}
		fits := true
		for _, write := range initial.Writes {
			if !touched[write.Resource] && ledger[write.Resource] == write.Expected {
				continue
			}
			if newClaims[write.Resource] {
				fits = false
				break
			}
			return nil, couplingMotionInvariant("native initial pair writes differ from committed or frozen ownership")
		}
		if !fits {
			continue
		}
		for _, write := range initial.Writes {
			touched[write.Resource], newClaims[write.Resource] = true, true
			putCouplingOwner(ledger, write.Resource, write.Next)
			tick.writes = append(tick.writes, write)
		}
		accepted = proposed
		tick.adoptions[i], tick.adopted[i] = group, true
	}
	return tick, nil
}

func (s *Simulation) couplingFleetMatches(contexts []*couplingMotionContext) bool {
	f := s.couplingFleet
	if f == nil || f.source != s || f.context != nil || f.network != s.couplingNetwork || f.orderContract != s.orderContract || len(f.entries) != len(s.vehicles) || len(f.pairs) != len(contexts) {
		return false
	}
	for _, c := range contexts {
		if _, ok := f.pairs[c]; !ok {
			return false
		}
	}
	for i := range s.vehicles {
		v, entry := &s.vehicles[i], &f.entries[i]
		if v.Pod.ID != entry.id || v.Pod.Class != entry.class || v.routeVersion != entry.routeVersion || !nativeForeignSameRoute(v.Route, entry.route) {
			return false
		}
		if v.Pod.Activity != Traveling && (entry.parked == nil || entry.parked.berth.ID != v.Pod.BerthID || entry.parked.position != v.Pod.Position) {
			return false
		}
	}
	return true
}

func (frame *nativeForeignTick) sweepsFor(c *couplingMotionContext) []couplingForeignSweep {
	result := make([]couplingForeignSweep, 0, len(c.foreignIDs))
	for _, id := range c.foreignIDs {
		proof := frame.proofs[id]
		if proof == nil {
			return nil
		}
		sweep := proof.raw
		sweep.native = proof
		result = append(result, sweep)
	}
	return result
}

// Retirement checks individual retention against the proposed final ledger.
func (s *Simulation) planCouplingRetirement(c *couplingMotionContext, step couplingMotionStep, ledger map[resource]resourceOwner) ([]couplingOwnerWrite, error) {
	expected := make(map[resource]resourceOwner)
	members := make(map[string]bool, 2)
	for i, member := range &c.reservation.members {
		v := *s.findVehicle(member.Vehicle.Pod.ID)
		v.Pod = step.Members[i].Pod
		v.distance, v.blockIndex, v.reservedThrough = step.State.Distances[i], step.State.Cells[i], c.through[i]
		s.addRouteOwners(expected, &v)
		members[v.Pod.ID] = true
		if v.RelocatingTo != "" {
			for _, r := range []resource{{kind: berthResource, id: v.destination.ID}, {kind: nodeResource, id: v.destination.Node}} {
				if ledger[r] == podResourceOwner(v.Pod.ID) {
					expected[r] = ledger[r]
				}
			}
		}
	}
	for r, owner := range expected {
		if ledger[r] != owner {
			return nil, couplingMotionInvariant("native retirement lacks individual retained ownership")
		}
	}
	var writes []couplingOwnerWrite
	for r, owner := range ledger {
		if owner == c.owner {
			return nil, couplingMotionInvariant("native retirement retains a group resource")
		}
		if owner.kind != podOwnerKind || !members[owner.id] || expected[r] == owner {
			continue
		}
		// Occupied trips need no remote receiving claims after the pair retires.
		preserved := slices.ContainsFunc(c.reservation.PreservedClaims, func(claim couplingClaim) bool { return claim.Resource == r && claim.Expected == owner })
		if !preserved {
			return nil, couplingMotionInvariant("native retirement has an unexplained individual resource")
		}
		writes = append(writes, couplingOwnerWrite{Resource: r, Expected: owner, Role: couplingReleasedOwner})
	}
	slices.SortFunc(writes, func(a, b couplingOwnerWrite) int { return compareCouplingResource(a.Resource, b.Resource) })
	return writes, nil
}

func putCouplingOwner(owners map[resource]resourceOwner, r resource, owner resourceOwner) {
	if owner.isZero() {
		delete(owners, r)
	} else {
		owners[r] = owner
	}
}

func (s *Simulation) moveNativeCoupling(tick *couplingNativeTick, index int) {
	group, step := &s.couplingGroups[index], &tick.steps[index]
	for i, member := range &group.context.reservation.members {
		v := s.findVehicle(member.Vehicle.Pod.ID)
		entered := v.blocks.routeLane(v.blockIndex) + 1
		if v.Pod.LaneID == "" {
			entered--
		}
		s.recordLaneEntries(v, entered, v.blocks.routeLane(step.State.Cells[i]))
		v.Pod = step.Members[i].Pod
		v.distance, v.blockIndex, v.reservedThrough = step.State.Distances[i], step.State.Cells[i], group.context.through[i]
		s.recordMotion(step.Samples[i])
		if v.Pod.Occupied {
			s.passengerDistanceMeters += step.Samples[i].DistanceMeters
		} else {
			s.emptyDistanceMeters += step.Samples[i].DistanceMeters
		}
	}
}

// The actual complete tick must match its proof before any owner transfer.
func (s *Simulation) finishNativeCoupling(tick *couplingNativeTick) error {
	if tick == nil {
		return nil
	}
	if err := tick.frame.checkApplied(s); err != nil {
		return err
	}
	proposed := make(map[resource]resourceOwner, len(tick.writes))
	for _, write := range tick.writes {
		actual, written := proposed[write.Resource]
		if !written {
			actual = s.owners[write.Resource]
		}
		if actual != write.Expected {
			return couplingMotionInvariant("native owner transfer changed after motion")
		}
		proposed[write.Resource] = write.Next
	}
	for _, write := range tick.writes {
		putCouplingOwner(s.owners, write.Resource, write.Next)
	}
	groups := s.couplingGroups[:0]
	for i, group := range s.couplingGroups {
		group.state = tick.steps[i].State
		if !group.state.Finished {
			groups = append(groups, group)
			continue
		}
		for _, member := range &group.context.reservation.members {
			v := s.findVehicle(member.Vehicle.Pod.ID)
			v.couplingID = ""
			v.routeReleases = nil
			v.nextRelease = 0
			for _, b := range v.blocks.span(0, v.reservedThrough+1) {
				for _, r := range b.resources {
					release := resourceReleaseDistance(b, r)
					if release > v.distance && s.owners[r] == podResourceOwner(v.Pod.ID) {
						v.retainRouteResource(r, release)
					}
				}
			}
			v.originReleased = v.distance >= v.originTail()
		}
	}
	clear(s.couplingGroups[len(groups):])
	s.couplingGroups = groups
	if len(groups) != len(tick.steps) {
		s.couplingFleet = nil
	}
	s.finishCouplingApproaches(tick)
	if len(s.couplingGroups) == 0 {
		s.couplingGroups = nil
		if len(s.couplingApproaches) == 0 {
			s.couplingFleet = nil
		}
	}
	return nil
}
