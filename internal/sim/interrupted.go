package sim

import (
	"errors"
	"fmt"
	"slices"
)

// interruptRider ends the order of the active rider at index as
// interrupted (incident contract, section 8.2). The rider and its aligned
// boarding record leave the pod. The order counts in interrupted, and its
// ID waits in undelivered until the session drains it.
//
// An interruption is not a completion. It writes no StepCompletion, and it
// adds nothing to completed, the journey totals, the rider distance, the
// direct distance, the detour ratio, or requestCompletions. It does not
// change riddenBase, the stops, or the phase of v: interruptRider is a step
// of a composite operation, and the composite freezes the distance and
// settles the pod. The caller checks that the rider at index is active
// and that the boarding records align.
func (s *Simulation) interruptRider(v *vehicle, index int) {
	rider := v.Riders[index]
	v.Riders = slices.Delete(slices.Clone(v.Riders), index, index+1)
	if len(v.Boardings) > 0 {
		v.Boardings = slices.Delete(slices.Clone(v.Boardings), index, index+1)
	}
	s.interrupted++
	s.interruptedPassengers += rider.PartySize
	s.undelivered = append(s.undelivered, rider.ID)
}

// DrainInterruptions returns the order IDs interrupted since the last call
// and clears them. The IDs are in the order of the interruptions. The
// session calls it before it releases its lock, so that rail sees each
// interruption before any save or publication. Step does not clear the
// IDs, so an interruption is never lost between ticks.
func (s *Simulation) DrainInterruptions() []int {
	drained := s.undelivered
	s.undelivered = nil
	return drained
}

// InterruptRider ends the active order orderID aboard pod podID as
// interrupted. It is the public entry of interruptRider, and it runs the
// monitor once after the change.
//
// Stage 1 has no policy that interrupts an order, so no production code
// calls it. The operational settlement of a pod comes with a later stage,
// so InterruptRider accepts only a rider whose destination another active
// rider of the pod shares. The stops and the phase of the pod then stay
// valid. It also refuses a simulation without the incident marker, a
// coupling, platoon, or Compact queue member, and a call during a dispatch
// pass, as the operations of the contract do. A refusal returns an error
// and changes nothing.
func (s *Simulation) InterruptRider(podID string, orderID int) error {
	if s.incidentContract != IncidentV1Contract {
		return errors.New("an interruption needs the incident contract")
	}
	v := s.findVehicle(podID)
	if v == nil {
		return fmt.Errorf("pod %s does not exist", podID)
	}
	if v.couplingID != "" || s.couplingApproachMember(v.Pod.ID) || v.coupled() || s.compactGroup(v) != nil {
		return fmt.Errorf("pod %s: interruption of a coupling, platoon, or Compact queue member", podID)
	}
	if s.pass != nil && s.pass.active {
		return fmt.Errorf("pod %s: interruption during a dispatch pass", podID)
	}
	if len(v.Boardings) > 0 && len(v.Boardings) != len(v.Riders) {
		return fmt.Errorf("pod %s: the boarding records do not align with the riders", podID)
	}
	index := slices.IndexFunc(v.Riders, func(rider Request) bool { return rider.ID == orderID && !rider.Completed })
	if index < 0 {
		return fmt.Errorf("pod %s has no active rider %d", podID, orderID)
	}
	to := v.Riders[index].To
	if !slices.ContainsFunc(v.Riders, func(rider Request) bool { return rider.ID != orderID && !rider.Completed && rider.To == to }) {
		return fmt.Errorf("pod %s: no other active rider goes to %s", podID, to)
	}
	s.interruptRider(v, index)
	s.observe()
	return nil
}

// FailStepForTest makes the Step that reaches tick fail a controller. At
// the end of that Step it runs before, and then it pauses the simulation
// with cause as the fault of the Compact queue controller, or of the
// coupling controller when compact is false. It acts once.
//
// It is a test entry (incident contract, section 13). The session tests
// use it to reach the fault returns of a step. No production code calls it.
func (s *Simulation) FailStepForTest(tick int64, compact bool, cause error, before func(*Simulation)) {
	previous := s.monitor
	s.monitor = func(s *Simulation) {
		if s.tick == tick {
			s.monitor = previous
			before(s)
			if compact {
				s.compactFault = cause
			} else {
				s.couplingFault = cause
			}
			s.paused = true
		}
		if previous != nil {
			previous(s)
		}
	}
}
