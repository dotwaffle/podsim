package sim

import (
	"errors"
	"slices"
)

type contractRouteKey struct {
	class    VehicleClass
	from, to string
}

func checkContractRestoreSemantics(input RestoreStateInput) error {
	if input.OrderContract != ExpressOrderContract {
		return nil
	}
	if err := validateContractFleetBounds(input.Network, input.Fleet, input.OrderContract); err != nil {
		return err
	}
	if err := input.Network.validate(); err != nil {
		return err
	}
	graph := newRouteGraph(input.Network)
	classes := make(map[string]VehicleClass, len(input.Fleet))
	for _, pod := range input.Fleet {
		if err := ValidateVehicleClassProfileWithOrderContract(pod.Class, input.OrderContract); err != nil {
			return err
		}
		classes[pod.ID] = effectiveClass(pod.Class)
	}
	known := make(map[contractRouteKey]bool)
	checked := make(map[contractRouteKey]bool)
	// admits checks the profile, party, and Express-class tests of a class
	// for the order options.
	admits := func(class VehicleClass, options TripOptions) bool {
		profile, ok := LookupVehicleClassWithOrderContract(class, input.OrderContract)
		if !ok || !profile.PhysicalSupported || options.PartySize > profile.MaxNewPartySize {
			return false
		}
		return options.Service != ExpressServiceChoice || class == ExpressClass
	}
	// pathFits checks for a certified passenger path of a class from the
	// leg origin to the destination.
	pathFits := func(class VehicleClass, from, to string) bool {
		key := contractRouteKey{class, from, to}
		if checked[key] {
			return known[key]
		}
		checked[key] = true
		fromStation, fromOK := input.Network.Station(from)
		toStation, toOK := input.Network.Station(to)
		known[key] = fromOK && toOK && !fromStation.ParkingOnly && !toStation.ParkingOnly && networkStationsConnected(input.Network, graph, fromStation, toStation, class)
		return known[key]
	}
	fits := func(class VehicleClass, options TripOptions, from string) bool {
		return admits(class, options) && pathFits(class, from, options.To)
	}
	for _, trip := range input.State.Waiting {
		options := Request(trip.Request).options()
		normalized, err := NormalizeTripOptionsWithOrderContract(options, input.OrderContract)
		if err != nil {
			return err
		}
		admitted, fit := false, false
		for _, class := range classes {
			if admits(class, normalized) {
				admitted = true
				if pathFits(class, trip.Request.legOrigin(), normalized.To) {
					fit = true
					break
				}
			}
		}
		// A transfer can leave a party where no admitting class has a path
		// to its destination. Only such a stranded trip waits without one.
		if !fit && (!admitted || !strandedTrip(input, trip)) {
			return errors.New("saved Express order has no compatible vehicle path")
		}
		if len(trip.Route) > 0 {
			class, ok := classes[trip.Request.PodID]
			if !ok || !contractRouteLanesFit(input.Network, trip.Route, class) {
				return errors.New("saved Express waiting route is incompatible")
			}
		}
	}
	for _, pod := range input.State.Pods {
		class, ok := classes[pod.ID]
		if !ok || class != effectiveClass(pod.Class) {
			return errors.New("saved Express pod class differs from fleet")
		}
		if !contractRouteLanesFit(input.Network, pod.Route, class) {
			return errors.New("saved Express pod route is incompatible")
		}
		for _, rider := range pod.Riders {
			if !fits(class, Request(rider).options(), rider.legOrigin()) {
				return errors.New("saved Express rider has incompatible endpoints or path")
			}
		}
	}
	return nil
}

// strandedTrip reports whether a saved trip that no admitting class can
// serve has the shape of a stranded transferred order (incident contract,
// section 7.6). S1: the trip boarded before, and its leg origin and
// destination are passenger stations. S2: it has no pod, route, hold, or
// exclusion. A saved trip keeps no destination berth apart from its
// route. S3: its order origin is a passenger station, and an Express order
// has its service pair. The caller checks that some fleet class admits the
// party.
func strandedTrip(input RestoreStateInput, trip SavedTrip) bool {
	request := trip.Request
	passenger := func(id string) bool {
		station, ok := input.Network.Station(id)
		return ok && !station.ParkingOnly
	}
	if !trip.Boarded || request.LegFrom == "" || !passenger(request.LegFrom) || !passenger(request.To) {
		return false
	}
	if request.PodID != "" || len(trip.Route) > 0 || trip.DeferPodID != "" || trip.DeferCheck != 0 || trip.ExcludedPod != "" {
		return false
	}
	if !passenger(request.From) {
		return false
	}
	return request.Service != ExpressServiceChoice || slices.ContainsFunc(input.ExpressServices, func(service ExpressService) bool {
		return service.ID == request.ServiceID && service.From == request.From && service.To == request.To
	})
}

func contractRouteLanesFit(network Network, route []int, class VehicleClass) bool {
	for _, index := range route {
		if index < 0 || index >= len(network.Lanes) || !network.Lanes[index].VehicleClasses.Allows(string(class)) {
			return false
		}
	}
	return true
}
