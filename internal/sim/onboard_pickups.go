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
func (s *Simulation) joinOnboardPickup(trip *waitingTrip) bool {
	if !s.onboardPickups || !s.cappedDetours() {
		return false
	}
	for index := range s.vehicles {
		v := &s.vehicles[index]
		if !s.onboardPickupReady(v, trip.request) {
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
			v.Pod.Activity, v.phaseTicks = Boarding, boardingTicks
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
	if !v.Pod.Occupied || v.Pod.Speed != 0 || v.RidersAboard() == 0 || v.Pod.StationID != request.From ||
		v.originReleased || v.reservedThrough >= 0 || v.destination.ID != "" || v.distance != 0 || v.pending >= 0 {
		return false
	}
	if v.Pod.Activity != Continuing && (v.Pod.Activity != Boarding || v.phaseTicks <= 0) {
		return false
	}
	station, ok := s.station(request.From)
	if !ok || station.ParkingOnly || !s.canJoin(v, request) {
		return false
	}
	berth, ok := station.berth(v.Pod.BerthID)
	if !ok || berth != v.origin || !s.pickupBerthFitsRequest(v, request, berth) {
		return false
	}
	return s.owners[resource{kind: berthResource, id: berth.ID}] == v.Pod.ID &&
		s.owners[resource{kind: nodeResource, id: berth.Node}] == v.Pod.ID
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
		if request.PodID != "" {
			return vehicle{}, false
		}
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

func (s *Simulation) pickupBoardingRecords(v *vehicle) ([]RiderBoarding, bool) {
	if len(v.Boardings) > 0 {
		return slices.Clone(v.Boardings), len(v.Boardings) == len(v.Riders)
	}
	if v.LegacyCohort || v.journeyOrigin.ID == "" || v.RidersAboard() == 0 {
		return nil, false
	}
	records := make([]RiderBoarding, len(v.Riders))
	for index, rider := range v.Riders {
		if rider.LegacyPartySize || rider.SharingConsent != SharedConsent && (!rider.Completed || rider.SharingConsent != PrivateConsent) {
			return nil, false
		}
		station, ok := s.station(rider.From)
		if !ok || station.ParkingOnly {
			return nil, false
		}
		berth, ok := station.berth(v.journeyOrigin.ID)
		if !ok || berth != v.journeyOrigin || !berthAllows(station, berth, v.Pod.Class) {
			return nil, false
		}
		records[index] = RiderBoarding{BerthID: berth.ID}
	}
	return records, true
}
