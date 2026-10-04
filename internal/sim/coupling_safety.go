package sim

import "fmt"

type couplingSafetyPair struct {
	pods    [2]Pod
	context *couplingMotionContext
	state   couplingMotionState
}

func (s *Simulation) couplingSafety(observation *SafetyObservation) {
	if len(s.couplingGroups) == 0 {
		return
	}
	observation.couplingPairs = make(map[[2]string]couplingSafetyPair, len(s.couplingGroups))
	for _, group := range s.couplingGroups {
		if group.context == nil || group.state.Finished {
			observation.couplingError = couplingMotionInvariant("stable frame holds an invalid or finished group")
			return
		}
		var pods [2]Pod
		for i, member := range &group.context.reservation.members {
			v := s.findVehicle(member.Vehicle.Pod.ID)
			if v == nil {
				observation.couplingError = couplingMotionInvariant("stable frame lacks a coupling member")
				return
			}
			pods[i] = v.Pod
		}
		key := [2]string{pods[0].ID, pods[1].ID}
		observation.couplingPairs[key] = couplingSafetyPair{pods: pods, context: group.context, state: group.state}
	}
}

func (o SafetyObservation) certifiedCouplingPair(first, second Pod) bool {
	if pair, ok := o.couplingPairs[[2]string{first.ID, second.ID}]; ok {
		return pair.pods == [2]Pod{first, second}
	}
	if pair, ok := o.couplingPairs[[2]string{second.ID, first.ID}]; ok {
		return pair.pods == [2]Pod{second, first}
	}
	return false
}

func (o SafetyObservation) checkCouplingSafety() error {
	if o.couplingError != nil {
		return o.couplingError
	}
	if len(o.couplingPairs) == 0 {
		return nil
	}
	pods := make(map[string]Pod, len(o.Pods))
	for _, pod := range o.Pods {
		pods[pod.ID] = pod
	}
	for key, pair := range o.couplingPairs {
		c, state := pair.context, pair.state
		if c == nil || state.context != c || state.Finished || state.Tick != o.Tick {
			return couplingMotionInvariant("observation has an invalid committed context")
		}
		canonical, err := c.stateAt(state.Elapsed)
		if err != nil || canonical != state {
			return couplingMotionInvariant("observation progress differs from its motion certificate")
		}
		members := c.membersAt(state)
		for i, id := range key {
			pod, ok := pods[id]
			certified := members[i].Pod
			// Native approach presentation changes as a draining member nears a station.
			// The observation still binds every physical and cabin field exactly.
			certified.StationPhase, certified.ManeuverStationID = pod.StationPhase, pod.ManeuverStationID
			if !ok || pod != pair.pods[i] || pod != certified {
				return couplingMotionInvariant("observation member differs from its certified cabin pose")
			}
		}
		if _, _, err := c.motionBodiesAt(state); err != nil {
			return err
		}
		if state.Phase == couplingDraining && pointDistance(pair.pods[0].Position, pair.pods[1].Position) < Clearance-conflictSlack {
			return couplingMotionInvariant("draining members lack ordinary separation")
		}
		for _, foreign := range o.Pods {
			if foreign.ID == key[0] || foreign.ID == key[1] {
				continue
			}
			clearance := classPairClearance(CompactClass, foreign.Class)
			for _, pod := range pair.pods {
				if pointDistance(pod.Position, foreign.Position) < clearance-conflictSlack {
					return &SeparationError{Tick: o.Tick, First: pod.ID, Second: foreign.ID, Gap: pointDistance(pod.Position, foreign.Position)}
				}
			}
			if state.Phase != couplingDraining {
				segments := []laneSegment{{from: foreign.Position, to: foreign.Position}}
				if err := c.checkConnectorSweep(state, state, segments, foreign.Class); err != nil {
					return fmt.Errorf("check coupling connector against pod %s: %w", foreign.ID, err)
				}
			}
		}
	}
	return nil
}
