package sim

import (
	"errors"
	"slices"
)

// SetOnboardPickups controls new occupied pickups at passenger berths.
// Disabling the policy preserves accepted riders and boarding intervals.
func (s *Simulation) SetOnboardPickups(enabled bool) error {
	if enabled && !s.cappedDetours() {
		return errors.New("onboard pickups require drop-off sharing above one party")
	}
	s.onboardPickups = enabled
	return nil
}

// joinOnboardPickup commits a complete candidate at an owned passenger berth.
// The trip does not join the pod that it excludes.
func (s *Simulation) joinOnboardPickup(trip *waitingTrip) bool {
	if !s.onboardPickups || !s.cappedDetours() {
		return false
	}
	for index := range s.vehicles {
		v := &s.vehicles[index]
		if trip.excludes(v.Pod.ID) || !s.onboardPickupReady(v, trip.request) {
			continue
		}
		candidate, ok := s.onboardPickupCandidate(v, trip.request)
		if !ok {
			continue
		}
		host := 0
		for _, rider := range candidate.Riders {
			if !rider.Completed {
				host = rider.ID
				break
			}
		}
		last := len(candidate.Riders) - 1
		candidate.Riders[last] = s.boardingRider(*trip, v, host)
		v.Riders, v.Boardings, v.Stops = candidate.Riders, candidate.Boardings, candidate.Stops
		v.destinationStation = candidate.destinationStation
		s.setVehicleRoute(v, candidate.Route)
		if v.Pod.Activity == Continuing {
			v.Pod.Activity, v.phaseTicks = Boarding, boardingPhase(v.Pod.Class, trip.request.PartySize)
		} else {
			v.addJoinDwell(trip.request.PartySize)
		}
		v.Pod.WaitReason, v.Pod.BlockedBy = NoWait, ""
		if !trip.boarded {
			s.sharedParties++
		}
		return true
	}
	return false
}

func (s *Simulation) onboardPickupReady(v *vehicle, request Request) bool {
	if !v.inService() || !v.Pod.Occupied || v.Pod.Speed != 0 || v.RidersAboard() == 0 || v.Pod.StationID != request.legOrigin() ||
		v.originReleased || v.reservedThrough >= 0 || v.destination.ID != "" || v.distance != 0 || v.pending >= 0 {
		return false
	}
	if v.Pod.Activity != Continuing && (v.Pod.Activity != Boarding || v.phaseTicks <= 0) {
		return false
	}
	station, ok := s.station(request.legOrigin())
	if !ok || station.ParkingOnly || !s.canJoin(v, request) {
		return false
	}
	berth, ok := station.berth(v.Pod.BerthID)
	if !ok || berth != v.origin || !s.pickupBerthFitsRequest(v, request, berth) {
		return false
	}
	return s.owners[resource{kind: berthResource, id: berth.ID}] == podResourceOwner(v.Pod.ID) &&
		s.owners[resource{kind: nodeResource, id: berth.Node}] == podResourceOwner(v.Pod.ID)
}

func (s *Simulation) onboardPickupCandidate(v *vehicle, request Request) (vehicle, bool) {
	candidate := *v
	candidate.Riders, candidate.Stops = slices.Clone(v.Riders), slices.Clone(v.Stops)
	var ok bool
	candidate.Boardings, ok = s.pickupBoardingRecords(v)
	if !ok || !finite(v.riddenMeters()) || v.riddenMeters() < 0 {
		return vehicle{}, false
	}
	if len(candidate.Riders) >= MaxStoredRidersForOrderContract(v.Pod.Class, s.orderContract) {
		oldest := slices.IndexFunc(candidate.Riders, func(rider Request) bool { return rider.Completed })
		if oldest < 0 {
			return vehicle{}, false
		}
		candidate.Riders = slices.Delete(candidate.Riders, oldest, oldest+1)
		candidate.Boardings = slices.Delete(candidate.Boardings, oldest, oldest+1)
	}
	candidate.Riders = append(candidate.Riders, request)
	candidate.Boardings = append(candidate.Boardings, RiderBoarding{BerthID: v.origin.ID, MetersAtBoarding: v.riddenMeters()})
	if !slices.Contains(candidate.Stops, request.To) {
		candidate.Stops, ok = s.addedStops(&candidate, request.To)
		if !ok {
			return vehicle{}, false
		}
	}
	route, err := s.stationApproachForStops(v.origin.Node, candidate.Stops, v.Pod.Class)
	if err != nil {
		return vehicle{}, false
	}
	station, _ := s.station(candidate.Stops[0])
	start := detourStart{class: v.Pod.Class, from: v.origin.Node, ridden: v.riddenMeters() + s.lanesMeters(route), entry: station.routeEntry(route, Berth{})}
	if !s.keepsRiderDetours(&candidate, candidate.Stops, start) {
		return vehicle{}, false
	}
	candidate.Route, candidate.destinationStation = route, candidate.Stops[0]
	return candidate, true
}

// pickupBoardingRecords returns a copy of the boarding records of pod v for
// an onboard pickup. It reports false when the records do not align with
// the riders, or when a rider cannot share the pod: an active rider needs
// shared consent, and a completed rider needs shared or private consent.
func (s *Simulation) pickupBoardingRecords(v *vehicle) ([]RiderBoarding, bool) {
	if len(v.Boardings) != len(v.Riders) {
		return nil, false
	}
	for _, rider := range v.Riders {
		if rider.SharingConsent != SharedConsent && (!rider.Completed || rider.SharingConsent != PrivateConsent) {
			return nil, false
		}
	}
	return slices.Clone(v.Boardings), true
}
