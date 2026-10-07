package sim

import (
	"errors"
	"fmt"
	"unicode/utf8"
)

// SharingConsent records one party's order-time sharing choice.
type SharingConsent string

// The sharing consents of an order.
const (
	PrivateConsent SharingConsent = "private"
	SharedConsent  SharingConsent = "shared"
)

// ServiceChoice distinguishes service from the vehicle's physical class.
type ServiceChoice string

// Service choices select ordinary demand or an explicit directed express pair.
const (
	OnDemandService      ServiceChoice = "on-demand"
	ExpressServiceChoice ServiceChoice = "express"
)

const (
	// MaxNewPartySize bounds new orders without splitting their party.
	MaxNewPartySize = 8
	// MaxExpressParties bounds singleton parties in one express service pod.
	MaxExpressParties = 20
	orderIDBytes      = 64
)

var (
	// ErrInvalidTripOptions means the order's effective values are invalid.
	ErrInvalidTripOptions = errors.New("invalid trip options")
	// ErrPartyAdmission means capacity, consent, or service refuses admission.
	ErrPartyAdmission = errors.New("party admission refused")
)

// TripOptions describes a new party independently of its eventual pod.
type TripOptions struct {
	From           string
	To             string
	PartySize      int
	SharingConsent SharingConsent
	Service        ServiceChoice
	ServiceID      string
}

// NormalizeTripOptionsWithOrderContract validates options under a valid explicit contract.
func NormalizeTripOptionsWithOrderContract(options TripOptions, contract OrderContract) (TripOptions, error) {
	if err := ValidateOrderContract(contract); err != nil {
		return TripOptions{}, err
	}
	if contract == ExpressOrderContract && (!utf8.ValidString(options.From) || !utf8.ValidString(options.To) || !utf8.ValidString(options.ServiceID)) {
		return TripOptions{}, fmt.Errorf("order IDs need valid UTF-8: %w", ErrInvalidTripOptions)
	}
	if options.PartySize == 0 {
		options.PartySize = 1
	}
	if options.SharingConsent == "" {
		options.SharingConsent = PrivateConsent
	}
	if options.Service == "" {
		options.Service = OnDemandService
	}
	if !validOrderID(options.From) || !validOrderID(options.To) {
		return TripOptions{}, fmt.Errorf("passenger endpoints need bounded IDs: %w", ErrInvalidTripOptions)
	}
	if options.From == options.To {
		return TripOptions{}, fmt.Errorf("trip endpoints must differ: %w", ErrInvalidTripOptions)
	}
	if options.PartySize < 1 || options.PartySize > newPartyLimit(contract) {
		return TripOptions{}, fmt.Errorf("party size must be 1 to %d: %w", newPartyLimit(contract), ErrInvalidTripOptions)
	}
	if options.SharingConsent != PrivateConsent && options.SharingConsent != SharedConsent {
		return TripOptions{}, fmt.Errorf("new orders need private or shared consent: %w", ErrInvalidTripOptions)
	}
	switch options.Service {
	case OnDemandService:
		if options.ServiceID != "" {
			return TripOptions{}, fmt.Errorf("on-demand orders cannot name an express service: %w", ErrInvalidTripOptions)
		}
	case ExpressServiceChoice:
		if options.SharingConsent != SharedConsent || !validOrderID(options.ServiceID) {
			return TripOptions{}, fmt.Errorf("express needs shared consent and a bounded service ID: %w", ErrInvalidTripOptions)
		}
	default:
		return TripOptions{}, fmt.Errorf("service must be on-demand or express: %w", ErrInvalidTripOptions)
	}
	return options, nil
}

// validOrderID reports whether id has 1 to orderIDBytes bytes, each one
// of IDCharacters.
func validOrderID(id string) bool {
	return id != "" && len(id) <= orderIDBytes && ValidIDText(id)
}

// PartyFacts supplies one active or completed rider's effective order values.
type PartyFacts struct {
	TripOptions
	Completed bool
}

// PartyAdmissionInput supplies logical capacity, consent, and service facts.
// PartyLimit is an explicit pooling policy, not evidence of sharing consent.
type PartyAdmissionInput struct {
	Class      VehicleClass
	Request    TripOptions
	Active     []PartyFacts
	PartyLimit int
}

// CheckPartyAdmissionWithOrderContract checks whole-party fit for the explicit contract.
func CheckPartyAdmissionWithOrderContract(input PartyAdmissionInput, contract OrderContract) error {
	request, err := NormalizeTripOptionsWithOrderContract(input.Request, contract)
	if err != nil {
		return err
	}
	profile, ok := LookupVehicleClassWithOrderContract(input.Class, contract)
	if !ok {
		return ErrUnknownVehicleClass
	}
	if request.PartySize > profile.MaxNewPartySize {
		return fmt.Errorf("party does not fit class %s: %w", profile.Class, ErrPartyAdmission)
	}
	limit := MaxSharedRideParties
	if request.Service == ExpressServiceChoice {
		if profile.Class != ExpressClass {
			return fmt.Errorf("express service needs the express class: %w", ErrPartyAdmission)
		}
		limit = MaxExpressParties
	}
	if input.PartyLimit < 1 || input.PartyLimit > limit {
		return fmt.Errorf("pooling party limit must be 1 to %d: %w", limit, ErrPartyAdmission)
	}
	parties, seats := 0, 0
	for _, party := range input.Active {
		if party.Completed {
			continue
		}
		if err := checkActiveParty(request, party.TripOptions, profile, contract); err != nil {
			return err
		}
		parties++
		if parties >= input.PartyLimit || party.PartySize > profile.Seats-seats {
			return fmt.Errorf("active parties exceed available capacity: %w", ErrPartyAdmission)
		}
		seats += party.PartySize
	}
	if request.PartySize > profile.Seats-seats {
		return fmt.Errorf("passenger seats are full: %w", ErrPartyAdmission)
	}
	return nil
}

func checkActiveParty(request, party TripOptions, profile VehicleClassSpec, contract OrderContract) error {
	if request.SharingConsent != SharedConsent || party.SharingConsent != SharedConsent {
		return fmt.Errorf("all active parties must consent to sharing: %w", ErrPartyAdmission)
	}
	if party.PartySize < 1 || party.PartySize > newPartyLimit(contract) {
		return fmt.Errorf("active party size is invalid: %w", ErrInvalidTripOptions)
	}
	effective, err := NormalizeTripOptionsWithOrderContract(party, contract)
	if err != nil {
		return fmt.Errorf("active party: %w", err)
	}
	if effective.PartySize > profile.MaxNewPartySize {
		return fmt.Errorf("active party does not fit class %s: %w", profile.Class, ErrPartyAdmission)
	}
	if effective.Service != request.Service || effective.ServiceID != request.ServiceID {
		return fmt.Errorf("active party has a different service: %w", ErrPartyAdmission)
	}
	if request.Service == ExpressServiceChoice && (effective.From != request.From || effective.To != request.To) {
		return fmt.Errorf("active party has a different express pair: %w", ErrPartyAdmission)
	}
	return nil
}
