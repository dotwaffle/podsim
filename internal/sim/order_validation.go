package sim

import (
	"errors"
	"fmt"
	"slices"
	"unicode/utf8"
)

func validSavedOptionsWithOrderContract(request SavedRequest, contract OrderContract) bool {
	if contract == ExpressOrderContract && (!utf8.ValidString(request.From) || !utf8.ValidString(request.LegFrom) || !utf8.ValidString(request.To) || !utf8.ValidString(request.ServiceID) || !utf8.ValidString(request.PodID) || !utf8.ValidString(request.DispatchReason)) {
		return false
	}
	if ValidateOrderContract(contract) != nil {
		return false
	}
	if request.SharingConsent != PrivateConsent && request.SharingConsent != SharedConsent {
		return false
	}
	if request.PartySize > newPartyLimit(contract) {
		return false
	}
	if request.Service == OnDemandService {
		return request.ServiceID == ""
	}
	return request.Service == ExpressServiceChoice && request.SharingConsent == SharedConsent && validOrderID(request.ServiceID)
}

func checkSavedAdmissionWithOrderContract(pod SavedPod, active []SavedRequest, contract OrderContract) error {
	if err := ValidateVehicleClassProfileWithOrderContract(pod.Class, contract); err != nil {
		return err
	}
	if contract == ExpressOrderContract {
		profile, _ := LookupVehicleClassWithOrderContract(pod.Class, contract)
		for _, rider := range pod.Riders {
			if rider.PartySize > profile.MaxNewPartySize {
				return ErrPartyAdmission
			}
		}
	}
	if len(active) == 0 {
		return nil
	}
	facts := make([]PartyFacts, 0, len(active)-1)
	for _, rider := range active[:len(active)-1] {
		facts = append(facts, PartyFacts{TripOptions: Request(rider).options()})
	}
	last := Request(active[len(active)-1])
	limit := MaxSharedRideParties
	if last.Service == ExpressServiceChoice {
		limit = MaxExpressParties
	}
	return CheckPartyAdmissionWithOrderContract(PartyAdmissionInput{Class: pod.Class, Request: last.options(), Active: facts, PartyLimit: limit}, contract)
}

// checkSavedClasses prevents logical fallback from accepting an unapproved
// profile, a changed immutable class, or a declared incompatible endpoint.
func (s *Simulation) checkSavedClasses(state SavedState) error {
	for _, pod := range state.Pods {
		if err := ValidateVehicleClassProfileWithOrderContract(pod.Class, s.orderContract); err != nil {
			return err
		}
		if v := s.findVehicle(pod.ID); v != nil && effectiveClass(v.Pod.Class) != effectiveClass(pod.Class) {
			return errors.New("saved pod class differs from fleet class")
		}
		for _, id := range []string{pod.BerthID, pod.Origin, pod.Destination, pod.JourneyOrigin} {
			if id == "" {
				continue
			}
			if node, ok := s.graph.berthNodes[id]; ok && !s.graph.nodeAllows(node, pod.Class) {
				return fmt.Errorf("pod %s declares an incompatible berth", pod.ID)
			}
		}
		for _, id := range append(slices.Clone(pod.Stops), pod.DestinationStation, pod.RelocatingTo) {
			if station, ok := s.station(id); ok && !station.VehicleClasses.Allows(string(pod.Class)) {
				return fmt.Errorf("pod %s declares an incompatible station", pod.ID)
			}
		}
		for _, stop := range pod.Stops {
			station, ok := s.station(stop)
			if !ok {
				continue
			}
			arrival := false
			for _, berth := range station.Berths {
				if _, err := s.stationPathForClass(station.berthEntry(berth), berth.Node, pod.Class); err == nil {
					arrival = true
					break
				}
			}
			if !arrival {
				return fmt.Errorf("pod %s has an incompatible stop berth path", pod.ID)
			}
		}
		for index := 1; index < len(pod.Stops); index++ {
			from, _ := s.station(pod.Stops[index-1])
			to, _ := s.station(pod.Stops[index])
			if from.ID != "" && to.ID != "" && !s.stationsConnectedForClass(from, to, pod.Class) {
				return fmt.Errorf("pod %s has an incompatible later passenger leg", pod.ID)
			}
		}
		for _, lane := range pod.Route {
			if lane >= 0 && lane < len(s.network.Lanes) && !s.graph.laneAllows(lane, pod.Class) {
				return fmt.Errorf("pod %s route has an incompatible lane", pod.ID)
			}
		}
	}
	return nil
}
