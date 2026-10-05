package sim

import "errors"

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
		fit := false
		for _, class := range classes {
			if fits(class, normalized, trip.Request.legOrigin()) {
				fit = true
				break
			}
		}
		if !fit {
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

func contractRouteLanesFit(network Network, route []int, class VehicleClass) bool {
	for _, index := range route {
		if index < 0 || index >= len(network.Lanes) || !network.Lanes[index].VehicleClasses.Allows(string(class)) {
			return false
		}
	}
	return true
}
