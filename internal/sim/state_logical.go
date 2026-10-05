package sim

import (
	"cmp"
	"fmt"
	"slices"
)

// logicalTrip is a trip for the queue of a logical restore. fromPod marks
// the rider of a pod that the restore puts back in the queue.
type logicalTrip struct {
	trip    waitingTrip
	fromPod bool
}

// restoreLogical rebuilds a running simulation with each fleet pod idle at
// its initial berth. It keeps the clock and the counters of the saved state.
// An active rider of an unloading pod completes when it goes to the station
// of the pod, and the station and the berth of the pod are a passenger
// station and one of its berths in the network. These journeys do not count
// in the journey totals. Each other active rider of a pod goes back to the
// queue as one trip, and its boarding stays recorded.
// The queued trips lose their pod bindings. As after Reset,
// the traffic demo stops and its parked pods are gone. restoreLogical fails
// when the saved state is not valid or when the result fails a check.
func restoreLogical(input RestoreStateInput, newFleet func() (*Simulation, error)) (*Simulation, RestoreResult, error) {
	state := input.State
	unaccounted, err := validateSavedState(state)
	if err != nil {
		return nil, RestoreResult{}, err
	}
	s, err := newFleet()
	if err != nil {
		return nil, RestoreResult{}, fmt.Errorf("create the fleet: %w", err)
	}
	if err := s.SetExpressServices(input.ExpressServices); err != nil {
		return nil, RestoreResult{}, err
	}
	if err := s.checkSavedClasses(state); err != nil {
		return nil, RestoreResult{}, err
	}
	if err := checkSavedPodIDs(s.initial, state); err != nil {
		return nil, RestoreResult{}, err
	}
	s.setSavedCounters(state)
	if err := s.SetOnboardPickups(input.OnboardPickups); err != nil {
		return nil, RestoreResult{}, err
	}
	var completed []int
	trips := make([]logicalTrip, 0, len(state.Pods)+len(state.Waiting))
	for _, pod := range state.Pods {
		active, _ := savedRiders(pod)
		arrived := pod.Activity == activityCode(Unloading) && s.passengerBerth(pod.StationID, pod.BerthID)
		for _, rider := range active {
			if arrived && rider.To == pod.StationID {
				s.completed++
				completed = append(completed, rider.ID)
				continue
			}
			trips = append(trips, logicalTrip{trip: requeuedTrip(Request(rider)), fromPod: true})
		}
	}
	for _, saved := range state.Waiting {
		trips = append(trips, logicalTrip{trip: s.unboundTrip(saved)})
	}
	result := s.queueTrips(state, trips)
	result.Tier, result.Unaccounted = RestoreLogical, unaccounted
	result.LogicalCompleted = completed
	if err := s.verifyRestore(state, completed, result.Interrupted, result.Dropped, unaccounted); err != nil {
		return nil, RestoreResult{}, err
	}
	return s, result, nil
}

// passengerBerth reports whether a berth is a berth of a passenger station
// in the network.
func (s *Simulation) passengerBerth(stationID, berthID string) bool {
	station, ok := s.station(stationID)
	return ok && !station.ParkingOnly &&
		slices.ContainsFunc(station.Berths, func(berth Berth) bool { return berth.ID == berthID })
}

// unboundTrip returns a saved trip without its pod bindings: the pod, the
// route and the deferral check. It keeps the deferral deadline when the
// deadline is in range.
func (s *Simulation) unboundTrip(saved SavedTrip) waitingTrip {
	trip := waitingTrip{request: Request(saved.Request), boarded: saved.Boarded}
	trip.request.PodID = ""
	if s.deferralInRange(saved.DeferUntil) {
		trip.deferUntil = saved.DeferUntil
	}
	return trip
}

// queueTrips puts the trips of a logical restore in the queue in request ID
// order. It drops a trip with a request that is not valid. It returns the
// requeued and the dropped requests.
func (s *Simulation) queueTrips(state SavedState, trips []logicalTrip) RestoreResult {
	slices.SortStableFunc(trips, func(a, b logicalTrip) int { return cmp.Compare(a.trip.request.ID, b.trip.request.ID) })
	var result RestoreResult
	for _, entry := range trips {
		request := entry.trip.request
		if !state.validTrip(SavedRequest(request), entry.trip.boarded) ||
			!s.passengerStation(request.From) || !s.passengerStation(request.To) || !s.optionalPassengerStation(request.LegFrom) {
			result.Dropped = append(result.Dropped, request.ID)
			result.DroppedParties++
			continue
		}
		s.waiting = append(s.waiting, entry.trip)
		if entry.fromPod {
			result.Requeued = append(result.Requeued, request.ID)
		}
	}
	return result
}
