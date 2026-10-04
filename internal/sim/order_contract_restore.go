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
	fits := func(class VehicleClass, options TripOptions) bool {
		profile, ok := LookupVehicleClassWithOrderContract(class, input.OrderContract)
		if !ok || !profile.PhysicalSupported || options.PartySize > profile.MaxNewPartySize {
			return false
		}
		if options.Service == ExpressServiceChoice && class != ExpressClass {
			return false
		}
		key := contractRouteKey{class, options.From, options.To}
		if checked[key] {
			return known[key]
		}
		checked[key] = true
		from, fromOK := input.Network.Station(options.From)
		to, toOK := input.Network.Station(options.To)
		known[key] = fromOK && toOK && !from.ParkingOnly && !to.ParkingOnly && networkStationsConnected(input.Network, graph, from, to, class)
		return known[key]
	}
	for _, trip := range input.State.Waiting {
		options := Request(trip.Request).options()
		normalized, err := NormalizeTripOptionsWithOrderContract(options, input.OrderContract)
		if err != nil {
			return err
		}
		fit := false
		for _, class := range classes {
			if fits(class, normalized) {
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
			if !fits(class, Request(rider).options()) {
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
