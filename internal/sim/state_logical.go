package sim

import (
	"cmp"
	"fmt"
	"slices"
)

// logicalTrip is a trip for the queue of a logical restore.
type logicalTrip struct {
	trip   waitingTrip
	source tripSource
}

// tripSource tells where a trip of a logical restore comes from.
type tripSource int

const (
	// fromQueue is a trip of the saved queue.
	fromQueue tripSource = iota
	// fromPod is a rider of a pod that boards or carries parties. The
	// restore puts it back in the queue.
	fromPod
	// fromUnloadingPod is a rider that leaves an unloading pod at its
	// station, because the pod has no later stop for it. The restore counts
	// it as completed and does not queue it.
	fromUnloadingPod
)

// restoreLogical rebuilds a running simulation with each fleet pod idle at
// its initial berth. It keeps the clock and the counters of the saved state.
// The riders that leave an unloading pod at its station count as completed.
// These are the riders with no stop in the pod.
// Their journeys do not count in the journey totals. Each other rider of a
// pod goes back to the queue as one trip, and its boarding stays recorded.
// The queued trips lose their pod bindings. As after Reset,
// the traffic demo stops and its parked pods are gone. restoreLogical fails
// when the saved state is not valid or when the result fails a check.
func restoreLogical(input RestoreStateInput) (*Simulation, RestoreResult, error) {
	state := input.State
	gap, err := validateSavedState(state)
	if err != nil {
		return nil, RestoreResult{}, err
	}
	s, err := NewFleet(input.Network, input.Fleet)
	if err != nil {
		return nil, RestoreResult{}, fmt.Errorf("create the fleet: %w", err)
	}
	if err := checkSavedPodIDs(s.initial, state); err != nil {
		return nil, RestoreResult{}, err
	}
	s.setSavedCounters(state)
	trips := make([]logicalTrip, 0, len(state.Pods)+len(state.Waiting))
	for _, pod := range state.Pods {
		if !pod.carriesPassengers() {
			continue
		}
		for _, rider := range pod.Riders {
			if rider.Completed {
				continue
			}
			entry := logicalTrip{trip: requeuedTrip(Request(rider)), source: fromPod}
			if pod.Activity == activityCode(Unloading) && !slices.Contains(pod.Stops, rider.To) {
				s.completed++
				entry.source = fromUnloadingPod
			}
			trips = append(trips, entry)
		}
	}
	for _, saved := range state.Waiting {
		trips = append(trips, logicalTrip{trip: s.unboundTrip(saved), source: fromQueue})
	}
	result := s.queueTrips(state, trips)
	result.Tier = RestoreLogical
	if err := s.verifyRestore(gap + result.DroppedParties); err != nil {
		return nil, RestoreResult{}, err
	}
	return s, result, nil
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
// order. The trips from the pods come before the saved trips, so of two trips
// with one ID, the saved trip is the duplicate. queueTrips drops a trip with
// a duplicate ID or with a request that is not valid. The request of an
// unloading pod that leaves at its station does not go in the queue, but it
// keeps its ID in use. It
// returns the requeued and the dropped requests.
func (s *Simulation) queueTrips(state SavedState, trips []logicalTrip) RestoreResult {
	slices.SortStableFunc(trips, func(a, b logicalTrip) int { return cmp.Compare(a.trip.request.ID, b.trip.request.ID) })
	var result RestoreResult
	used := make(map[int]bool, len(trips))
	for _, entry := range trips {
		request := entry.trip.request
		switch {
		case entry.source == fromUnloadingPod:
			// restoreLogical counted the rider as completed.
		case used[request.ID] || !state.validTrip(SavedRequest(request), entry.trip.boarded) ||
			!s.passengerStation(request.From) || !s.passengerStation(request.To):
			result.Dropped = append(result.Dropped, request.ID)
			result.DroppedParties++
			continue
		default:
			s.waiting = append(s.waiting, entry.trip)
			if entry.source == fromPod {
				result.Requeued = append(result.Requeued, request.ID)
			}
		}
		used[request.ID] = true
	}
	return result
}
