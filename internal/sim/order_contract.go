package sim

import (
	"errors"
	"fmt"
	"unicode/utf8"
)

// OrderContract selects explicit immutable order and storage semantics.
type OrderContract string

// ExpressOrderContract permits the approved Express profile and order bounds.
const ExpressOrderContract OrderContract = "express-v1"

// MaxExpressWaitingTrips bounds both pending and outstanding Express orders.
const MaxExpressWaitingTrips = 8600

// ValidateOrderContract rejects unknown opt-in contracts.
func ValidateOrderContract(contract OrderContract) error {
	if contract != "" && contract != ExpressOrderContract {
		return errors.New("unknown order contract")
	}
	return nil
}

// MaxWaitingTripsForOrderContract returns the pending bound for a valid contract.
func MaxWaitingTripsForOrderContract(contract OrderContract) int {
	if contract == ExpressOrderContract {
		return MaxExpressWaitingTrips
	}
	return MaxSavedWaitingTrips
}

// MaxStoredRidersForOrderContract includes active riders and completed history.
func MaxStoredRidersForOrderContract(class VehicleClass, contract OrderContract) int {
	if contract == ExpressOrderContract && class == ExpressClass {
		return MaxExpressParties
	}
	return MaxSharedRideParties
}

// LookupVehicleClassWithOrderContract returns the explicitly approved profile.
func LookupVehicleClassWithOrderContract(class VehicleClass, contract OrderContract) (VehicleClassSpec, bool) {
	if ValidateOrderContract(contract) != nil {
		return VehicleClassSpec{}, false
	}
	profile, ok := LookupVehicleClass(class)
	if ok && class == ExpressClass && contract == ExpressOrderContract {
		profile.MaxNewPartySize = 20
		profile.BodyLengthMeters = 10
		profile.PhysicalSupported = true
	}
	return profile, ok
}

// ValidateVehicleClassProfileWithOrderContract checks effective physical approval.
func ValidateVehicleClassProfileWithOrderContract(class VehicleClass, contract OrderContract) error {
	if err := ValidateOrderContract(contract); err != nil {
		return err
	}
	p, ok := LookupVehicleClassWithOrderContract(class, contract)
	if !ok {
		return ErrUnknownVehicleClass
	}
	if !p.PhysicalSupported {
		return fmt.Errorf("class %s: %w", p.Class, ErrUnsupportedVehicleProfile)
	}
	return nil
}

// OrderContract returns the simulation's immutable operating contract.
func (s *Simulation) OrderContract() OrderContract { return s.orderContract }

func newPartyLimit(contract OrderContract) int {
	if contract == ExpressOrderContract {
		return 20
	}
	return MaxNewPartySize
}

func (s *Simulation) outstandingOrders() int {
	count := len(s.waiting)
	for _, v := range s.vehicles {
		for _, r := range v.Riders {
			if !r.Completed {
				count++
			}
		}
	}
	return count
}

func (s *Simulation) clearUnboundWaitingRoutes() {
	if s.orderContract != ExpressOrderContract {
		return
	}
	for i := range s.waiting {
		trip := &s.waiting[i]
		if trip.request.PodID == "" {
			trip.route = nil
			trip.destination = Berth{}
		}
	}
}

func (state SavedState) checkContractRoutes() error {
	if state.OrderContract != ExpressOrderContract {
		return nil
	}
	pods := make(map[string]bool, len(state.Pods))
	for _, p := range state.Pods {
		pods[p.ID] = true
	}
	bound := make(map[string]bool)
	for _, trip := range state.Waiting {
		if !utf8.ValidString(trip.DeferPodID) || len(trip.DeferPodID) > 64 {
			return errors.New("the Express deferral pod ID needs bounded UTF-8")
		}
		if len(trip.Route) == 0 {
			continue
		}
		id := trip.Request.PodID
		if id == "" || !pods[id] || bound[id] {
			return errors.New("the Express waiting route needs a unique existing pod")
		}
		bound[id] = true
	}
	return nil
}

// NewFleetWithOrderContract creates a fleet with an explicit immutable contract.
func NewFleetWithOrderContract(network Network, placements []Placement, contract OrderContract) (*Simulation, error) {
	if contract == "" {
		return NewFleet(network, placements)
	}
	if err := validateContractFleetBounds(network, placements, contract); err != nil {
		return nil, err
	}
	p, err := PrepareNetwork(network)
	if err != nil {
		return nil, err
	}
	return p.NewFleetWithOrderContract(placements, contract)
}

// NewFleetWithOrderContract validates placements under the explicit contract.
func (p *PreparedNetwork) NewFleetWithOrderContract(placements []Placement, contract OrderContract) (*Simulation, error) {
	if err := p.check(); err != nil {
		return nil, err
	}
	if err := validatePlacementsWithOrderContract(p.network, placements, contract); err != nil {
		return nil, err
	}
	s := p.newFleet(placements)
	s.orderContract = contract
	return s, nil
}

// ValidateFleetWithOrderContract checks startup without creating mutable state.
func ValidateFleetWithOrderContract(network Network, placements []Placement, contract OrderContract) error {
	if contract == "" {
		return ValidateFleet(network, placements)
	}
	if err := validateContractFleetBounds(network, placements, contract); err != nil {
		return err
	}
	p, err := PrepareNetwork(network)
	if err != nil {
		return err
	}
	if err := p.check(); err != nil {
		return err
	}
	return validatePlacementsWithOrderContract(p.network, placements, contract)
}

func checkSavedServiceLimits(input RestoreStateInput) error {
	if input.OrderContract != ExpressOrderContract {
		return nil
	}
	for _, service := range input.ExpressServices {
		if !boundedContractID(service.ID) || !boundedContractID(service.From) || !boundedContractID(service.To) {
			return errors.New("the Express service IDs need valid UTF-8")
		}
	}
	registry, err := validatedExpressServices(input.Network, newRouteGraph(input.Network), input.ExpressServices)
	if err != nil {
		return err
	}
	for _, pod := range input.State.Pods {
		active, _ := savedRiders(pod)
		if len(active) == 0 {
			continue
		}
		last := Request(active[len(active)-1])
		if last.Service != ExpressServiceChoice {
			continue
		}
		service, ok := registry[last.ServiceID]
		if !ok {
			return errors.New("unknown saved Express service")
		}
		facts := make([]PartyFacts, 0, len(active)-1)
		for _, r := range active[:len(active)-1] {
			facts = append(facts, PartyFacts{TripOptions: Request(r).options()})
		}
		if err := CheckPartyAdmissionWithOrderContract(PartyAdmissionInput{Class: pod.Class, Request: last.options(), Active: facts, PartyLimit: service.PartyLimit}, input.OrderContract); err != nil {
			return err
		}
	}
	return nil
}
