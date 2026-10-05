package sim

import (
	"errors"
	"fmt"
)

// ExpressService declares one directed hub pair and its pooling party cap.
type ExpressService struct {
	ID         string       `json:"id"`
	From       string       `json:"from"`
	To         string       `json:"to"`
	Class      VehicleClass `json:"class"`
	PartyLimit int          `json:"partyLimit"`
}

// MaxExpressServices bounds an authored service registry.
const MaxExpressServices = 300

// SetExpressServices validates and replaces an owned registry atomically.
// Declaring future class capacity does not approve that class's physical profile.
func (s *Simulation) SetExpressServices(services []ExpressService) error {
	if len(services) > MaxExpressServices {
		return errors.New("too many express services")
	}
	s.ensureNetworkIndexes()
	next, err := validatedExpressServices(s.network, s.graph, services)
	if err != nil {
		return err
	}
	// Accepted requests retain their service identity and pair.
	for _, trip := range s.waiting {
		if err := serviceMatches(next, trip.request.options()); err != nil {
			return err
		}
	}
	for _, v := range s.vehicles {
		for _, rider := range v.Riders {
			if !rider.Completed {
				if err := serviceMatches(next, rider.options()); err != nil {
					return err
				}
			}
		}
	}
	if s.orderContract == ExpressOrderContract {
		if err := checkSavedServiceLimits(RestoreStateInput{OrderContract: s.orderContract, Network: s.network, State: s.ExportState(), ExpressServices: services}); err != nil {
			return err
		}
	}
	s.expressServices = next
	return nil
}

// ValidateExpressServices checks bounded service records and compatible paths.
func ValidateExpressServices(network Network, services []ExpressService) error {
	_, err := validatedExpressServices(network, newRouteGraph(network), services)
	return err
}

func validatedExpressServices(network Network, graph routeGraph, services []ExpressService) (map[string]ExpressService, error) {
	if len(services) > MaxExpressServices {
		return nil, errors.New("too many express services")
	}
	next := make(map[string]ExpressService, len(services))
	for _, service := range services {
		if !validOrderID(service.ID) || !validOrderID(service.From) || !validOrderID(service.To) || service.From == service.To || service.Class != ExpressClass || service.PartyLimit < 1 || service.PartyLimit > MaxExpressParties {
			return nil, errors.New("invalid express service")
		}
		if _, ok := next[service.ID]; ok {
			return nil, errors.New("duplicate express service")
		}
		from, fromOK := network.Station(service.From)
		to, toOK := network.Station(service.To)
		if !fromOK || !toOK || from.ParkingOnly || to.ParkingOnly || !networkStationsConnected(network, graph, from, to, service.Class) {
			return nil, fmt.Errorf("express service %s has incompatible endpoints or paths", service.ID)
		}
		next[service.ID] = service
	}
	return next, nil
}

func networkStationsConnected(network Network, graph routeGraph, from, to Station, class VehicleClass) bool {
	for _, origin := range from.Berths {
		if !berthAllows(from, origin, class) {
			continue
		}
		for _, destination := range to.Berths {
			if !berthAllows(to, destination, class) {
				continue
			}
			input := networkRouteInput{from: origin.Node, to: destination.Node, class: class, terminalBerthsOnly: true}
			if _, err := network.routeIndexed(input, graph); err == nil {
				return true
			}
			input.terminalBerthsOnly = false
			if _, err := network.routeIndexed(input, graph); err == nil {
				return true
			}
		}
	}
	return false
}

func serviceMatches(registry map[string]ExpressService, options TripOptions) error {
	if options.Service != ExpressServiceChoice {
		return nil
	}
	service, ok := registry[options.ServiceID]
	if !ok || service.From != options.From || service.To != options.To {
		return errors.New("unknown express service or directed pair")
	}
	return nil
}

func (s *Simulation) validateTripOptions(options TripOptions) (TripOptions, error) {
	if options.From == options.To && options.From != "" {
		return TripOptions{}, ErrSameStation
	}
	options, err := NormalizeTripOptionsWithOrderContract(options, s.orderContract)
	if err != nil {
		return TripOptions{}, err
	}
	if s.orderContract == ExpressOrderContract && (len(s.waiting) >= MaxExpressWaitingTrips || s.outstandingOrders() >= MaxExpressWaitingTrips) {
		return TripOptions{}, ErrPartyAdmission
	}
	from, ok := s.station(options.From)
	if !ok || from.ParkingOnly {
		return TripOptions{}, errors.New("choose a passenger pickup station")
	}
	to, ok := s.station(options.To)
	if !ok || to.ParkingOnly {
		return TripOptions{}, errors.New("choose a passenger destination")
	}
	if err := serviceMatches(s.expressServices, options); err != nil {
		return TripOptions{}, err
	}
	request := requestFromOptions(options, 0, s.tick)
	for i := range s.vehicles {
		if s.podFitsRequest(&s.vehicles[i], request) {
			return options, nil
		}
	}
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if ValidateVehicleClassProfileWithOrderContract(v.Pod.Class, s.orderContract) == nil && CheckPartyAdmissionWithOrderContract(PartyAdmissionInput{Class: v.Pod.Class, Request: options, PartyLimit: s.partyLimit(request)}, s.orderContract) == nil {
			return TripOptions{}, fmt.Errorf("passenger route %s to %s: %w", options.From, options.To, ErrUnreachable)
		}
	}
	return TripOptions{}, fmt.Errorf("no certified compatible vehicle and passenger path: %w", ErrPartyAdmission)
}

func requestFromOptions(options TripOptions, id int, tick int64) Request {
	return Request{ID: id, From: options.From, To: options.To, PartySize: options.PartySize, SharingConsent: options.SharingConsent, Service: options.Service, ServiceID: options.ServiceID, RequestedTick: tick}
}

func (request Request) options() TripOptions {
	return TripOptions{From: request.From, To: request.To, PartySize: request.PartySize, SharingConsent: request.SharingConsent, Service: request.Service, ServiceID: request.ServiceID}
}

func (s *Simulation) partyLimit(request Request) int {
	if request.Service == ExpressServiceChoice {
		if service, ok := s.expressServices[request.ServiceID]; ok {
			return service.PartyLimit
		}
		return 1
	}
	return s.sharedRidePartyLimit
}

func (s *Simulation) podFitsRequest(v *vehicle, request Request) bool {
	if ValidateVehicleClassProfileWithOrderContract(v.Pod.Class, s.orderContract) != nil {
		return false
	}
	if err := serviceMatches(s.expressServices, request.options()); err != nil {
		return false
	}
	if err := CheckPartyAdmissionWithOrderContract(PartyAdmissionInput{Class: v.Pod.Class, Request: request.options(), PartyLimit: s.partyLimit(request)}, s.orderContract); err != nil {
		return false
	}
	from, fromOK := s.station(request.From)
	to, toOK := s.station(request.To)
	return fromOK && toOK && !from.ParkingOnly && !to.ParkingOnly && s.stationsConnectedForClass(from, to, v.Pod.Class)
}

// pickupBerthFitsRequest checks the passenger leg from the selected pickup berth.
func (s *Simulation) pickupBerthFitsRequest(v *vehicle, request Request, berth Berth) bool {
	station, ok := s.station(request.From)
	if !ok || !berthAllows(station, berth, v.Pod.Class) {
		return false
	}
	selected, ok := station.berth(berth.ID)
	if !ok || selected.Node != berth.Node {
		return false
	}
	_, err := s.stationApproachRouteForClass(berth.Node, request.To, v.Pod.Class)
	return err == nil
}

// assignedPickupFitsRequest checks the current or reachable pickup berth.
func (s *Simulation) assignedPickupFitsRequest(v *vehicle, request Request) bool {
	station, ok := s.station(request.From)
	if !ok {
		return false
	}
	berth := v.destination
	switch {
	case v.Pod.Activity == Idle:
		if v.Pod.StationID != request.From {
			return false
		}
		berth, _ = station.berth(v.Pod.BerthID)
	case v.destinationStation != request.From:
		var reachable bool
		_, berth, reachable = s.candidateRouteForRequest(v, request, nil)
		if !reachable {
			return false
		}
	case berth.Node == "":
		var err error
		_, berth, err = s.stationRouteByLoad(stationRouteInput{class: v.Pod.Class, from: station.routeEntry(v.Route, berth), station: station.ID, accept: s.berthFilterForStops(v.Pod.Class, []string{request.To})})
		if err != nil {
			return false
		}
	}
	return s.pickupBerthFitsRequest(v, request, berth)
}

func (s *Simulation) canJoin(v *vehicle, request Request) bool {
	if v.couplingID != "" {
		return false
	}
	if !s.podFitsRequest(v, request) {
		return false
	}
	active := make([]PartyFacts, 0, len(v.Riders))
	for _, rider := range v.Riders {
		active = append(active, PartyFacts{TripOptions: rider.options(), Completed: rider.Completed})
	}
	return CheckPartyAdmissionWithOrderContract(PartyAdmissionInput{Class: v.Pod.Class, Request: request.options(), Active: active, PartyLimit: s.partyLimit(request)}, s.orderContract) == nil
}

func (s *Simulation) consentCompatible(v *vehicle, request Request) bool {
	if request.SharingConsent != SharedConsent {
		return false
	}
	for _, rider := range v.Riders {
		if !rider.Completed && (rider.SharingConsent != SharedConsent || rider.Service != request.Service || rider.ServiceID != request.ServiceID) {
			return false
		}
	}
	return s.podFitsRequest(v, request)
}

func (s *Simulation) hasFittingPod(request Request) bool {
	for i := range s.vehicles {
		if s.podFitsRequest(&s.vehicles[i], request) {
			return true
		}
	}
	return false
}

// ValidateExpressServicesWithOrderContract checks the registry and explicit contract.
func ValidateExpressServicesWithOrderContract(network Network, services []ExpressService, contract OrderContract) error {
	if err := ValidateOrderContract(contract); err != nil {
		return err
	}
	if contract == ExpressOrderContract {
		for _, service := range services {
			if !boundedContractID(service.ID) || !boundedContractID(service.From) || !boundedContractID(service.To) {
				return errors.New("the Express service IDs need bounded UTF-8")
			}
		}
	}
	return ValidateExpressServices(network, services)
}
