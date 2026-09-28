package sim

import "slices"

// SeatScreen holds the counters of the seat screen. They show whether the
// parties that a pod could take are more than its seats. The simulation
// counts them only while the experiment records are on and the party limit
// is more than 1. They change no decision, and they are not in the
// snapshot or in a saved state. Reset and a restore start them from 0.
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
// when the screen did not count the trip before. It does not change the
// pod.
func (s *Simulation) refusedByFullPod(trip *waitingTrip, v *vehicle) bool {
	if !s.recordExperiments || trip.boarded || trip.fullPodRefused {
		return false
	}
	if s.sharedRideMode != SharedRideDropOffs {
		return v.destinationStation == trip.request.To
	}
	_, ok := s.dropOffStops(v, trip.request.To)
	return ok
}

// recordDeparture counts a boarding pod that departs now in the seat
// screen. The pod is still at its origin berth.
func (s *Simulation) recordDeparture(v *vehicle) {
	aboard, backlog := len(v.Riders), 0
	for index := range s.waiting {
		if request := s.waiting[index].request; request.From == v.Pod.StationID && s.backlogParty(v, request.To) {
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
