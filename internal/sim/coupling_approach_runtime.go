package sim

import (
	"fmt"
	"reflect"
	"slices"
)

type couplingNativeApproach struct {
	context *couplingApproachContext
	state   couplingApproachState
}

type couplingApproachAttempt struct {
	context *couplingApproachContext
}

type couplingApproachTransition struct {
	context  *couplingApproachContext
	previous couplingApproachState
	step     couplingApproachStep
}

// Discovery reads a completed native boundary before the next tick increment.
func (s *Simulation) discoverCouplingApproaches() {
	// A new ordinary berth departure starts a new physical journey epoch.
	// Pending pickups and intermediate occupied departures retain the attempt.
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if departs(v.Pod.Activity) && !v.Pod.Occupied && v.distance == 0 && !v.originReleased && v.Pod.BerthID != "" && v.Pod.BerthID == v.origin.ID {
			delete(s.couplingAttempts, v.Pod.ID)
		}
	}
	if !s.couplingEnabled || s.couplingNetwork == nil || s.platooning != PlatooningVirtual {
		return
	}
	ids := make([]string, 0, len(s.vehicles))
	for _, v := range s.vehicles {
		ids = append(ids, v.Pod.ID)
	}
	slices.Sort(ids)
	corridors := make([]string, 0, len(s.couplingNetwork.corridors))
	for id := range s.couplingNetwork.corridors {
		corridors = append(corridors, id)
	}
	slices.Sort(corridors)
	for _, id := range ids {
		front := s.findVehicle(id)
		if front.couplingID != "" || s.couplingApproachMember(id) || front.follower <= 0 || front.follower > len(s.vehicles) {
			continue
		}
		rear := &s.vehicles[front.follower-1]
		if rear.couplingID != "" || s.couplingApproachMember(rear.Pod.ID) {
			continue
		}
		if old := s.couplingAttempts[id]; old.context != nil {
			continue
		}
		for _, corridor := range corridors {
			c, state, err := prepareCouplingApproach(couplingApproachPrepareInput{Simulation: s, Network: s.couplingNetwork, Prepared: s.couplingNetwork.prepared, CorridorID: corridor, Members: [2]string{id, rear.Pod.ID}, Enabled: true})
			if err != nil {
				continue
			}
			if s.couplingAttempts == nil {
				s.couplingAttempts = make(map[string]couplingApproachAttempt)
			}
			s.couplingAttempts[id] = couplingApproachAttempt{context: c}
			s.couplingApproaches = append(s.couplingApproaches, couplingNativeApproach{context: c, state: state})
			break
		}
	}
}

func (s *Simulation) couplingApproachMember(id string) bool {
	for _, a := range s.couplingApproaches {
		if a.context.members[0].id == id || a.context.members[1].id == id {
			return true
		}
	}
	return false
}

// This check runs after the whole conflict closure and before any grant write.
func (s *Simulation) couplingApproachGrant(v *vehicle, through int) bool {
	for _, a := range s.couplingApproaches {
		if a.context.members[1].id != v.Pod.ID {
			continue
		}
		if v.routeVersion != a.context.members[1].routeVersion || !reflect.DeepEqual(v.Route, a.context.members[1].route) || through > a.context.ceiling {
			v.Pod.WaitReason, v.Pod.BlockedBy = TrackOccupied, a.context.members[0].id
			return false
		}
	}
	return true
}

func (s *Simulation) planCouplingApproaches() ([]couplingApproachTransition, error) {
	transitions := make([]couplingApproachTransition, 0, len(s.couplingApproaches))
	for _, a := range s.couplingApproaches {
		step, err := planCouplingApproach(couplingApproachInput{Context: a.context, Previous: a.state, Simulation: s, Enabled: s.couplingEnabled})
		if err != nil {
			return nil, err
		}
		transitions = append(transitions, couplingApproachTransition{context: a.context, previous: a.state, step: step})
	}
	return transitions, nil
}

func (s *Simulation) prepareCouplingAdoption(a couplingApproachTransition, index int) (couplingNativeGroup, couplingMotionStep, error) {
	members := [2]string{a.context.members[0].id, a.context.members[1].id}
	if err := s.checkCouplingCheckpointWork(members); err != nil {
		return couplingNativeGroup{}, couplingMotionStep{}, err
	}
	input := a.context.formationInput(s, s.findVehicle(members[0]), s.findVehicle(members[1]))
	reservation, err := planCouplingReservation(input)
	if err != nil {
		return couplingNativeGroup{}, couplingMotionStep{}, err
	}
	foreign := make([]string, 0, len(s.vehicles)-2)
	for _, v := range s.vehicles {
		if v.Pod.ID != members[0] && v.Pod.ID != members[1] {
			foreign = append(foreign, v.Pod.ID)
		}
	}
	id := s.couplingAdoptionID(index)
	c, err := prepareCouplingMotionContext(couplingMotionContextInput{Reservation: reservation, Current: input, GroupID: id, ForeignIDs: foreign})
	if err != nil {
		return couplingNativeGroup{}, couplingMotionStep{}, err
	}
	initial, err := initialCouplingMotion(c, input)
	if err != nil {
		return couplingNativeGroup{}, couplingMotionStep{}, err
	}
	return couplingNativeGroup{context: c, state: initial.State, formationTick: s.tick}, initial, nil
}

func (s *Simulation) couplingAdoptionID(index int) string {
	for suffix := 0; suffix <= len(s.couplingGroups); suffix++ {
		id := fmt.Sprintf("pair-%d-%d-%d", s.tick, index, suffix)
		if !slices.ContainsFunc(s.couplingGroups, func(g couplingNativeGroup) bool { return g.context.owner.id == id }) {
			return id
		}
	}
	panic("bounded group identity search exhausted")
}

// Only the producer-certified front replaces ordinary movement.
func (s *Simulation) moveCouplingApproach(tick *couplingNativeTick, v *vehicle) bool {
	if tick == nil {
		return false
	}
	for _, a := range tick.approaches {
		if a.context.members[0].id != v.Pod.ID {
			continue
		}
		step := a.step
		if step.Front == nil {
			return step.HoldFront
		}
		command := *step.Front
		before, speed, occupied := v.distance, v.Pod.Speed, v.Pod.Occupied
		s.publishVehicleTravel(v, command.Distance, command.Speed)
		travel := v.distance - before
		s.recordMotion(MotionSample{ID: v.Pod.ID, Class: v.Pod.Class, DistanceMeters: travel, StartSpeed: speed, EndSpeed: v.Pod.Speed})
		if occupied {
			s.passengerDistanceMeters += travel
		} else {
			s.emptyDistanceMeters += travel
		}
		return true
	}
	return false
}

func (s *Simulation) finishCouplingApproaches(tick *couplingNativeTick) {
	retained := s.couplingApproaches[:0]
	for i, a := range s.couplingApproaches {
		a.state = tick.approaches[i].step.State
		if tick.adopted[i] {
			s.couplingGroups = append(s.couplingGroups, tick.adoptions[i])
			for _, m := range &tick.adoptions[i].context.reservation.members {
				v := s.findVehicle(m.Vehicle.Pod.ID)
				v.couplingID = tick.adoptions[i].context.owner.id
				// The checked mechanical owner now replaces ordinary admission.
				v.pending = -1
				member := 0
				if m.Vehicle.Pod.ID == tick.adoptions[i].context.reservation.members[1].Vehicle.Pod.ID {
					member = 1
				}
				v.blockIndex, v.reservedThrough = tick.adoptions[i].state.Cells[member], tick.adoptions[i].context.through[member]
			}
			s.couplingFleet = nil
			continue
		}
		if tick.approaches[i].step.Ready {
			a.state.Phase = couplingApproachWaiting
		}
		if a.state.Phase != couplingApproachFinished {
			retained = append(retained, a)
		}
	}
	clear(s.couplingApproaches[len(retained):])
	s.couplingApproaches = retained
	if len(retained) == 0 {
		s.couplingApproaches = nil
	}
}

// Ordinary release has finished before the per-link retirement check.
func (s *Simulation) finishCouplingApproachLinks() {
	for _, a := range s.couplingApproaches {
		if a.state.Phase != couplingApproachWaiting && a.state.Phase != couplingApproachAborting {
			continue
		}
		rear := s.findVehicle(a.context.members[1].id)
		if rear.link.leader == 0 {
			continue
		}
		rear.link.draining = true
		if !s.holdsPending(rear) {
			s.unlink(rear)
		}
	}
}
