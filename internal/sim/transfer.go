package sim

import (
	"errors"
	"slices"
)

// transferRider moves the active rider at index from v to the queue with
// the leg origin station (incident contract, section 7.3). The party keeps
// its order identity, its request time, and its boarding time. A transfer
// sets LegFrom also when station is the order origin. It sets no
// exclusion, because a transferred party can use any pod.
//
// transferRider is a step of a composite operation. It does not change the
// stops, the distance, or the phase of v, so the pod is not valid until
// the composite settles it. It does not need a feasible continuation: a
// transfer without one makes a stranded order, which waits until a project
// change gives it a pod. See continuationFeasible.
//
// It returns an error and changes nothing when the rider at index is not
// active, or when station is not a passenger station or is the destination
// of the rider.
func (s *Simulation) transferRider(v *vehicle, index int, station string) error {
	if index < 0 || index >= len(v.Riders) || v.Riders[index].Completed {
		return errors.New("a transfer needs an active rider")
	}
	if len(v.Boardings) > 0 && len(v.Boardings) != len(v.Riders) {
		return errors.New("the boarding records of the pod do not align with its riders")
	}
	rider := v.Riders[index]
	if !s.passengerStation(station) || station == rider.To {
		return errors.New("a transfer needs a passenger station other than the destination")
	}
	v.Riders = slices.Delete(slices.Clone(v.Riders), index, index+1)
	if len(v.Boardings) > 0 {
		v.Boardings = slices.Delete(slices.Clone(v.Boardings), index, index+1)
	}
	trip := requeuedTrip(rider)
	trip.request.LegFrom = station
	// The queue is in order ID order, as restoreWaiting builds it.
	position := slices.IndexFunc(s.waiting, func(waiting waitingTrip) bool { return waiting.request.ID > trip.request.ID })
	if position < 0 {
		position = len(s.waiting)
	}
	s.waiting = slices.Insert(s.waiting, position, trip)
	return nil
}

// continuationFeasible reports whether some fleet pod fits request with the
// leg origin station. A policy can test it before it chooses the station
// of an unload. Stage 1 operations do not test it.
func (s *Simulation) continuationFeasible(request Request, station string) bool {
	request.LegFrom = station
	return s.hasFittingPod(request)
}
