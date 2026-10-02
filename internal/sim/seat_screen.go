package sim

import "slices"

// SeatScreen holds the counters of the seat screen. They show whether the
// parties that a pod could take are more than its seats. The join census
// members count the parties with a pod on its way that a boarding pod
// could take. The simulation counts them only while the experiment
// records are on and the party limit is more than 1. They change no
// decision, and they are not in the snapshot or in a saved state. Reset
// and a restore start them from 0.
type SeatScreen struct {
	// FullPodRefusals counts the new parties that found a full boarding pod
	// at their origin that could take them with a free seat, and that
	// joined no pod in that dispatch pass. Each party counts one time. A
	// rider that a restore queues again does not count.
	FullPodRefusals int
	// FullDepartures counts the boarding pods that departed with as many
	// parties as the limit.
	FullDepartures int
	// DepartureBacklog is the sum, over the departures of boarding pods, of
	// the waiting parties at the origin that the pod could take with a
	// free seat. See backlogParty.
	DepartureBacklog int
	// Aboard counts the departures of boarding pods by the parties aboard.
	// Index n is for n parties.
	Aboard [MaxSharedRideParties + 1]int
	// Demand counts the departures of boarding pods by the parties aboard
	// plus the departure backlog. Index n is for n parties. The last index
	// is for MaxSharedRideParties parties or more.
	Demand [MaxSharedRideParties + 1]int
	// JoinEligibleAssigned counts the parties with a pod on its way that a
	// boarding pod at their origin could take with a free seat at one or
	// more dispatch passes. See recordJoinEligible. Each party counts one
	// time. A rider that a restore queues again does not count.
	JoinEligibleAssigned int
	// JoinEligibleExistingStop counts the parties of JoinEligibleAssigned
	// that a boarding pod could take with no new stop, because the pod
	// already stops at their destination. In destination mode, each party
	// of JoinEligibleAssigned is in this member. Each party counts one time,
	// so the member is at most JoinEligibleAssigned.
	JoinEligibleExistingStop int
	// ReassignedParties counts the parties that joined a boarding pod while
	// they had a pod on its way. Dispatch released that pod. See
	// SharedRideJoinReassignExisting. A rider that a restore queues again
	// does not count.
	ReassignedParties int
}

// SeatScreen returns the counters of the seat screen. It does not change
// the simulation.
func (s *Simulation) SeatScreen() SeatScreen {
	return s.seatScreen
}

// screensSeats reports whether the simulation counts the seat screen.
func (s *Simulation) screensSeats() bool {
	return s.recordExperiments && s.sharedRidePartyLimit > 1
}

// refusedByFullPod reports whether the seat screen must count a refusal
// of the trip by the full boarding pod v. This is so for a new party that
// the pod could take with a free seat, with the rules of joinSharedRide,
// when the screen did not count the trip before. For a trip with a pod on
// its way, these rules accept only a stop of the pod. It does not change
// the pod.
func (s *Simulation) refusedByFullPod(trip *waitingTrip, v *vehicle) bool {
	if !s.recordExperiments || trip.boarded || trip.fullPodRefused || !s.consentCompatible(v, trip.request) {
		return false
	}
	if s.sharedRideMode != SharedRideDropOffs {
		return v.destinationStation == trip.request.To
	}
	if trip.request.PodID != "" {
		return slices.Contains(v.Stops, trip.request.To)
	}
	_, ok := s.dropOffStops(v, trip.request.To)
	return ok
}

// recordDeparture counts a boarding pod that departs now in the seat
// screen. The pod is still at its origin berth.
func (s *Simulation) recordDeparture(v *vehicle) {
	aboard, backlog := len(v.Riders), 0
	for index := range s.waiting {
		if request := s.waiting[index].request; request.From == v.Pod.StationID && s.consentCompatible(v, request) && s.backlogParty(v, request.To) {
			backlog++
		}
	}
	screen := &s.seatScreen
	if aboard >= s.sharedRidePartyLimit {
		screen.FullDepartures++
	}
	screen.DepartureBacklog += backlog
	screen.Aboard[min(aboard, MaxSharedRideParties)]++
	screen.Demand[min(aboard+backlog, MaxSharedRideParties)]++
}

// backlogParty reports whether a departing boarding pod v could take a
// party to the station to with a free seat. The rules are the rules of
// joinSharedRide, but the pod reserved track to depart, and dropOffStops
// refuses a new stop for such a pod. Thus in drop-offs mode, backlogParty
// uses the rules of addedStops, which do not read the track or the berth
// of the pod. It does not change the pod.
func (s *Simulation) backlogParty(v *vehicle, to string) bool {
	if s.sharedRideMode != SharedRideDropOffs {
		return v.destinationStation == to
	}
	if slices.Contains(v.Stops, to) {
		return true
	}
	_, ok := s.addedStops(v, to)
	return ok
}

// recordJoinEligible counts the trip in the join census when its pod v is
// on its way and a boarding pod at its origin could take the party with a
// free seat. The rules are the rules of joinSharedRide, but the census
// does not call setBoardingStops, because it changes the pod. Thus it also
// counts a party whose new first stop has no route. The census counts each
// party one time in each member, and it does not count a rider that a
// restore queues again. It does not change the pods.
func (s *Simulation) recordJoinEligible(trip *waitingTrip, v *vehicle, pass *dispatchPass) {
	if trip.boarded || trip.joinEligibleExistingStop || !releasable(v) {
		return
	}
	to := trip.request.To
	for _, host := range s.boardingPods(pass)[trip.request.From] {
		if !s.canJoin(host, trip.request) {
			continue
		}
		existing := host.destinationStation == to
		if s.sharedRideMode == SharedRideDropOffs {
			existing = slices.Contains(host.Stops, to)
		}
		eligible := existing
		if !existing && !trip.joinEligibleAssigned && s.sharedRideMode == SharedRideDropOffs {
			_, eligible = s.dropOffStops(host, to)
		}
		if eligible && !trip.joinEligibleAssigned {
			trip.joinEligibleAssigned = true
			s.seatScreen.JoinEligibleAssigned++
		}
		if existing {
			trip.joinEligibleExistingStop = true
			s.seatScreen.JoinEligibleExistingStop++
			return
		}
	}
}
