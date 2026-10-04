package sim

import (
	"math"
	"slices"
)

type nativeApproachProof struct {
	frame      *nativeForeignTick
	index      int
	transition couplingApproachTransition
	sealed     couplingApproachTransition
}

func cloneApproachTransition(a couplingApproachTransition) couplingApproachTransition {
	if a.step.Front != nil {
		a.step.Front = new(*a.step.Front)
	}
	return a
}

func (frame *nativeForeignTick) prepareApproaches(transitions []couplingApproachTransition) error {
	if len(transitions) != len(frame.fleet.source.couplingApproaches) {
		return couplingMotionInvariant("native approaches omit an actual controller")
	}
	frame.approachFronts = make(map[int]*nativeApproachProof, len(transitions))
	used := make(map[string]bool, len(transitions)*2)
	for i, a := range transitions {
		actual := frame.fleet.source.couplingApproaches[i]
		if a.context != actual.context || a.previous != actual.state || a.step.State.Tick != frame.tick {
			return couplingMotionInvariant("native approach has another producer or clock")
		}
		expected, err := planCouplingApproach(couplingApproachInput{Context: actual.context, Previous: actual.state, Simulation: frame.fleet.source, Enabled: frame.fleet.source.couplingEnabled})
		if err != nil || !sameApproachStep(expected, a.step) {
			return couplingMotionInvariant("native approach differs from its actual planned producer")
		}
		for _, m := range &a.context.members {
			if used[m.id] {
				return couplingMotionInvariant("native approaches share a member")
			}
			used[m.id] = true
		}
		if a.step.Front == nil && !a.step.HoldFront {
			continue
		}
		index := slices.IndexFunc(frame.facts, func(f nativeForeignFact) bool { return f.pod.ID == a.context.members[0].id })
		if index < 0 || index >= len(frame.facts) || frame.facts[index].pod.ID != a.context.members[0].id || frame.pairMembers[index].proof != nil || frame.facts[index].compact.planned {
			return couplingMotionInvariant("native approach front has another controller")
		}
		frame.approachFronts[index] = &nativeApproachProof{frame: frame, index: index, transition: cloneApproachTransition(a), sealed: cloneApproachTransition(a)}
	}
	return nil
}

func (frame *nativeForeignTick) approachProof(index int, a *nativeApproachProof) (*nativeForeignProof, error) {
	if err := frame.checkOrdinaryPose(index); err != nil {
		return nil, err
	}
	fact, entry := frame.facts[index], frame.fleet.entries[index]
	if entry.path == nil {
		return nil, couplingMotionInvariant("native approach lacks an actual path")
	}
	owners, err := frame.sealOwners(index, entry.path)
	if err != nil {
		return nil, err
	}
	nextDistance, nextSpeed := fact.distance, fact.pod.Speed
	if a.transition.step.Front != nil {
		nextDistance, nextSpeed = a.transition.step.Front.Distance, a.transition.step.Front.Speed
	}
	position, err := frame.ordinaryPosition(index, nextDistance)
	if err != nil {
		return nil, err
	}
	proof := &nativeForeignProof{frame: frame, index: index, approach: a, nextPosition: position, nextRouteDistance: nextDistance,
		raw: couplingForeignSweep{Path: entry.path, Tick: frame.tick - 1, Distance: fact.distance, Speed: fact.pod.Speed, NextDistance: nextDistance, NextSpeed: nextSpeed, ReservedThrough: fact.through, Owners: owners}}
	if err := a.check(proof); err != nil {
		return nil, err
	}
	return proof.freezeClaims(), nil
}

func (a *nativeApproachProof) check(proof *nativeForeignProof) error {
	if a == nil || proof == nil || a.frame != proof.frame || a.index != proof.index || a.frame.approachFronts[a.index] != a || a.transition.context != a.sealed.context || a.transition.previous != a.sealed.previous || !sameApproachStep(a.transition.step, a.sealed.step) {
		return couplingMotionInvariant("native approach certificate changed after production")
	}
	t, fact := a.transition, a.frame.facts[a.index]
	raw := proof.raw
	if t.previous.Tick != raw.Tick || t.step.State.Tick != a.frame.tick || t.context != t.previous.context || t.context != t.step.State.context || raw.Distance != fact.distance || raw.Speed != fact.pod.Speed {
		return couplingMotionInvariant("native approach certificate has a stale clock or pose")
	}
	if command := t.step.Front; command != nil {
		if command.ID != fact.pod.ID || command.Tick != a.frame.tick || command.PreviousDistance != fact.distance || command.PreviousSpeed != fact.pod.Speed || raw.NextDistance != command.Distance || raw.NextSpeed != command.Speed {
			return couplingMotionInvariant("native approach sweep differs from its commanded motion")
		}
		if t.step.State.Phase == couplingApproachAborting {
			speed := max(0, fact.pod.Speed-acceleration/TicksPerSecond)
			if raw.NextSpeed != speed || raw.NextDistance != fact.distance+speed/TicksPerSecond {
				return couplingMotionInvariant("native approach abort differs from actual bounded braking")
			}
		} else {
			distance, speed, err := t.context.schedule.sample(t.step.State.Cursor)
			if err != nil || t.step.State.Cursor != t.previous.Cursor+1 || raw.NextDistance != distance || raw.NextSpeed != speed {
				return couplingMotionInvariant("native approach motion differs from its immutable schedule")
			}
		}
	} else if !t.step.HoldFront || fact.distance != t.context.target || fact.pod.Speed != 0 || raw.NextDistance != fact.distance || raw.NextSpeed != 0 || t.step.State.WaitTick < 0 || t.step.State.Tick > t.step.State.DeadlineTick {
		return couplingMotionInvariant("native held approach lacks an unexpired actual arrival")
	}
	return nil
}

func (frame *nativeForeignTick) checkApproachForeign(a couplingApproachTransition) error {
	var members [2][]laneSegment
	for i, m := range &a.context.members {
		proof := frame.proofs[m.id]
		if proof == nil {
			return couplingMotionInvariant("native approach omits its actual member")
		}
		sweep := proof.raw
		sweep.native = proof
		if err := couplingCheckForeignMotion(sweep); err != nil {
			return err
		}
		segments, err := couplingForeignSegments(sweep)
		if err != nil {
			return err
		}
		members[i] = segments
	}
	for _, x := range members[0] {
		for _, y := range members[1] {
			if couplingSegmentsDistance(x.from, x.to, y.from, y.to) < Clearance-conflictSlack {
				return couplingMotionInvariant("native approach member sweeps overlap")
			}
		}
	}
	for id, proof := range frame.proofs {
		if id == a.context.members[0].id || id == a.context.members[1].id {
			continue
		}
		sweep := proof.raw
		sweep.native = proof
		if err := couplingCheckForeignMotion(sweep); err != nil {
			return err
		}
		foreign, err := couplingForeignSegments(sweep)
		if err != nil {
			return err
		}
		clearance := classPairClearance(CompactClass, sweep.Path.class)
		for _, segments := range members {
			for _, x := range segments {
				for _, y := range foreign {
					if couplingSegmentsDistance(x.from, x.to, y.from, y.to) < clearance-conflictSlack {
						return couplingMotionInvariant("native approach foreign body sweep is unsafe")
					}
				}
			}
		}
		if proof.pair.proof != nil && proof.pair.member == 0 {
			pair := proof.pair.proof.pair
			connector, exists, err := nativePairConnectorBox(pair.context, pair.previous, pair.next)
			if err != nil {
				return err
			}
			if exists {
				for _, segments := range members {
					for _, segment := range segments {
						profile, _ := LookupCouplingProfile(a.context.network.contract)
						radius := math.Hypot(profile.BodyLengthMeters, profile.BodyWidthMeters) / 2
						if couplingSegmentBox(segment.from, segment.to, radius).intersects(connector) {
							return couplingMotionInvariant("native approach touches a foreign connector sweep")
						}
					}
				}
			}
		}
	}
	return nil
}

func sameApproachStep(a, b couplingApproachStep) bool {
	if (a.Front == nil) != (b.Front == nil) {
		return false
	}
	if a.Front != nil && *a.Front != *b.Front {
		return false
	}
	a.Front, b.Front = nil, nil
	return a == b
}
