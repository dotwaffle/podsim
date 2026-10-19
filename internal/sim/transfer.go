package sim

import (
	"slices"
)

// queueTransfer is the effect of transferRider. The caller checks its
// preconditions.
func (s *Simulation) queueTransfer(v *vehicle, index int, station string) {
	rider := v.Riders[index]
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
}
