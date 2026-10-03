package session

import (
	"errors"
	"slices"

	"github.com/dotwaffle/podsim/internal/sim"
)

func orderOptions(r sim.Request) sim.TripOptions {
	return sim.TripOptions{From: r.From, To: r.To, PartySize: r.PartySize, SharingConsent: r.SharingConsent, Service: r.Service, ServiceID: r.ServiceID}
}

func validStreamOrderContract(r sim.Request, contract sim.OrderContract) bool {
	if contract == "" {
		return validStreamOrder(r)
	}
	if r.LegacyPartySize || r.SharingConsent == sim.LegacyUnknownConsent {
		return validStreamOrder(r)
	}
	options, err := sim.NormalizeTripOptionsWithOrderContract(orderOptions(r), contract)
	return err == nil && options == orderOptions(r)
}

func (a *StreamAssembler) expressService(r sim.Request) (int, error) {
	if r.Service != sim.ExpressServiceChoice {
		return sim.MaxSharedRideParties, nil
	}
	for _, service := range a.topology.ExpressServices {
		if service.ID == r.ServiceID && service.From == r.From && service.To == r.To {
			return service.PartyLimit, nil
		}
	}
	return 0, errors.New("unknown Express service or directed pair")
}

func (a *StreamAssembler) expressOrders(frame StreamFrame) error {
	if frame.State.Simulation.OrderContract != a.topology.OrderContract {
		return errors.New("state contract does not match topology")
	}
	outstanding := len(frame.State.Simulation.Pending)
	for _, r := range frame.State.Simulation.Pending {
		if !validStreamOrderContract(r, a.topology.OrderContract) || r.LegacyPartySize || r.SharingConsent == sim.LegacyUnknownConsent {
			return errors.New("invalid Express pending order")
		}
		if _, err := a.expressService(r); err != nil {
			return err
		}
		// A pending order must have a compatible certified vehicle and passenger path.
		compatible := false
		for _, v := range frame.State.Simulation.Vehicles {
			limit, _ := a.expressService(r)
			if sim.CheckPartyAdmissionWithOrderContract(sim.PartyAdmissionInput{Class: v.Pod.Class, Request: orderOptions(r), PartyLimit: limit}, a.topology.OrderContract) == nil && a.passengerPath(r, v.Pod.Class) {
				compatible = true
				break
			}
		}
		if !compatible {
			return errors.New("pending order has no compatible class and passenger path")
		}
	}
	for _, v := range frame.State.Simulation.Vehicles {
		profile, known := sim.LookupVehicleClassWithOrderContract(v.Pod.Class, a.topology.OrderContract)
		if !known || sim.ValidateVehicleClassProfileWithOrderContract(v.Pod.Class, a.topology.OrderContract) != nil {
			return errors.New("unsupported Express stream vehicle")
		}
		if old, exists := a.classes[v.Pod.ID]; exists && old != profile.Class {
			return errors.New("stream vehicle class changed within project")
		}
		if v.LegacyCohort && (profile.Class != sim.LegacyClass || len(v.Riders) == 0) {
			return errors.New("invalid closed legacy cohort")
		}
		if len(v.Riders) > sim.MaxStoredRidersForOrderContract(v.Pod.Class, a.topology.OrderContract) {
			return errors.New("too many stored riders")
		}
		facts := []sim.PartyFacts{}
		for _, r := range v.Riders {
			if !validStreamOrderContract(r, a.topology.OrderContract) || (r.SharingConsent == sim.LegacyUnknownConsent) != v.LegacyCohort {
				return errors.New("invalid Express rider order")
			}
			if _, err := a.expressService(r); err != nil {
				return err
			}
			if r.Completed {
				if r.LegacyPartySize || r.SharingConsent == sim.LegacyUnknownConsent {
					if profile.Class != sim.LegacyClass {
						return errors.New("historical order requires legacy class")
					}
				} else {
					limit, err := a.expressService(r)
					if err != nil {
						return err
					}
					if err := sim.CheckPartyAdmissionWithOrderContract(sim.PartyAdmissionInput{Class: profile.Class, Request: orderOptions(r), PartyLimit: limit}, a.topology.OrderContract); err != nil {
						return err
					}
				}
				continue
			}
			outstanding++
			if v.LegacyCohort {
				continue
			}
			if r.LegacyPartySize || !a.passengerPath(r, profile.Class) {
				return errors.New("active rider has incompatible profile or passenger path")
			}
			limit, err := a.expressService(r)
			if err != nil {
				return err
			}
			if err := sim.CheckPartyAdmissionWithOrderContract(sim.PartyAdmissionInput{Class: profile.Class, Request: orderOptions(r), Active: facts, PartyLimit: limit}, a.topology.OrderContract); err != nil {
				return err
			}
			facts = append(facts, sim.PartyFacts{TripOptions: orderOptions(r)})
		}
	}
	if outstanding > sim.MaxExpressWaitingTrips {
		return errors.New("express outstanding orders exceed supported limit")
	}
	return nil
}

type passengerPathKey struct {
	from, to string
	class    sim.VehicleClass
}

func (a *StreamAssembler) passengerPath(r sim.Request, class sim.VehicleClass) bool {
	if !a.stations[r.From] || !a.stations[r.To] {
		return false
	}
	key := passengerPathKey{r.From, r.To, class}
	if result, known := a.passengerPaths[key]; known {
		return result
	}
	result := a.findPassengerPath(r, class)
	a.passengerPaths[key] = result
	return result
}

func (a *StreamAssembler) findPassengerPath(r sim.Request, class sim.VehicleClass) bool {
	from, ok := a.topology.Network.Station(r.From)
	if !ok || from.ParkingOnly || !from.VehicleClasses.Allows(string(class)) {
		return false
	}
	to, ok := a.topology.Network.Station(r.To)
	if !ok || to.ParkingOnly || !to.VehicleClasses.Allows(string(class)) {
		return false
	}
	for _, origin := range from.Berths {
		if !origin.VehicleClasses.Allows(string(class)) {
			continue
		}
		for _, destination := range to.Berths {
			if !destination.VehicleClasses.Allows(string(class)) {
				continue
			}
			if _, err := a.topology.Network.RouteForClass(origin.Node, destination.Node, class); err == nil {
				return true
			}
		}
	}
	return false
}

func (a *StreamAssembler) indexClassLanes() {
	a.classLanes = map[sim.VehicleClass]map[string]bool{}
	for _, class := range []sim.VehicleClass{sim.LegacyClass, sim.CompactClass, sim.GroupClass, sim.ExpressClass} {
		nodes := map[string]bool{}
		stations := map[string]bool{}
		for _, station := range a.topology.Network.Stations {
			stations[station.ID] = station.VehicleClasses.Allows(string(class))
			for _, berth := range station.Berths {
				allowed := stations[station.ID] && berth.VehicleClasses.Allows(string(class))
				if previous, known := nodes[berth.Node]; known {
					allowed = allowed && previous
				}
				nodes[berth.Node] = allowed
			}
		}
		lanes := make(map[string]bool, len(a.topology.Network.Lanes))
		for _, lane := range a.topology.Network.Lanes {
			allowed := lane.VehicleClasses.Allows(string(class))
			if from, known := nodes[lane.From]; known {
				allowed = allowed && from
			}
			if to, known := nodes[lane.To]; known {
				allowed = allowed && to
			}
			if lane.StationID != "" {
				allowed = allowed && stations[lane.StationID]
			}
			lanes[lane.ID] = allowed
		}
		a.classLanes[class] = lanes
	}
}

func (a *StreamAssembler) laneAdmits(index int, class sim.VehicleClass) bool {
	return index >= 0 && index < len(a.topology.Network.Lanes) && a.classLanes[class][a.topology.Network.Lanes[index].ID]
}

func (a *StreamAssembler) expressBindings(frame StreamFrame) error {
	classes := map[string]sim.VehicleClass{}
	for _, v := range frame.State.Simulation.Vehicles {
		classes[v.Pod.ID] = v.Pod.Class
	}
	large := func(class sim.VehicleClass) bool { return class == sim.GroupClass || class == sim.ExpressClass }
	for i, v := range frame.State.Simulation.Vehicles {
		class := v.Pod.Class
		if class == "" {
			class = sim.LegacyClass
		}
		if large(class) && v.PlatoonIndex != 0 || v.PlatoonID != "" && (large(class) || large(classes[v.PlatoonID])) {
			return errors.New("large vehicles cannot have platoon links")
		}
		for _, id := range []string{v.Pod.StationID, v.Pod.ManeuverStationID, v.RelocatingTo} {
			if id != "" {
				station, ok := a.topology.Network.Station(id)
				if !ok || !station.VehicleClasses.Allows(string(class)) {
					return errors.New("vehicle station does not admit its class")
				}
			}
		}
		if v.Pod.BerthID != "" {
			berth, ok := a.boardingBerths[v.Pod.BerthID]
			if !ok || v.Pod.StationID != "" && berth.station != v.Pod.StationID || !berth.stationClasses.Allows(string(class)) || !berth.classes.Allows(string(class)) {
				return errors.New("vehicle berth does not admit its class")
			}
		}
		if v.Pod.LaneID != "" {
			index := slices.IndexFunc(a.topology.Network.Lanes, func(l sim.Lane) bool { return l.ID == v.Pod.LaneID })
			if !a.laneAdmits(index, class) {
				return errors.New("vehicle lane does not admit its class")
			}
		}
		for _, indexes := range [][]int{frame.Routes[i].Display, frame.Routes[i].Lanes} {
			for _, index := range indexes {
				if !a.laneAdmits(index, class) {
					return errors.New("route lane does not admit vehicle class")
				}
			}
		}
	}
	return nil
}

// ownAssemblerState keeps mutable presentation containers private to the cache.
// Geometry and expanded routes retain their existing immutable ownership.
func ownAssemblerState(state State) State {
	state.Simulation.Pending = slices.Clone(state.Simulation.Pending)
	state.Simulation.Vehicles = slices.Clone(state.Simulation.Vehicles)
	for i := range state.Simulation.Vehicles {
		vehicle := &state.Simulation.Vehicles[i]
		vehicle.Riders = slices.Clone(vehicle.Riders)
		vehicle.Boardings = slices.Clone(vehicle.Boardings)
		vehicle.Stops = slices.Clone(vehicle.Stops)
	}
	state.Checkpoints = slices.Clone(state.Checkpoints)
	return state
}
