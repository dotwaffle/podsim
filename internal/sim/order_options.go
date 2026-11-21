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
	return checkPartyAdmission(input, contract).error()
}

// partyAdmits reports whether CheckPartyAdmissionWithOrderContract returns
// nil. It makes no error for a refusal.
func partyAdmits(input PartyAdmissionInput, contract OrderContract) bool {
	return checkPartyAdmission(input, contract).refusal == admitted
}

// admissionRefusal identifies the refusal of a party admission check. Each
// refusal has one error of CheckPartyAdmissionWithOrderContract.
type admissionRefusal uint8

// The refusals of a party admission check.
const (
	admitted              admissionRefusal = iota
	refusedRequest                         // the request options are invalid
	refusedUnknownClass                    // the class has no profile
	refusedClassFit                        // the party does not fit the class
	refusedExpressClass                    // express service needs the express class
	refusedPartyLimit                      // the pooling party limit is out of range
	refusedActiveConsent                   // an active party does not consent to sharing
	refusedActiveSize                      // an active party size is invalid
	refusedActiveOptions                   // the options of an active party are invalid
	refusedActiveClassFit                  // an active party does not fit the class
	refusedActiveService                   // an active party has a different service
	refusedActivePair                      // an active party has a different express pair
	refusedActiveCapacity                  // the active parties exceed the capacity
	refusedSeatsFull                       // the passenger seats are full
)

// admissionResult is the result of checkPartyAdmission. It holds the values
// that the error of the refusal needs.
type admissionResult struct {
	refusal admissionRefusal
	// class is the class of refusedClassFit and refusedActiveClassFit.
	class VehicleClass
	// limit is the party limit of refusedPartyLimit.
	limit int
	// err is the options error of refusedRequest and refusedActiveOptions.
	err error
}

// error returns the error of the refusal, or nil when the check admits the
// party.
func (r admissionResult) error() error {
	switch r.refusal {
	case admitted:
		return nil
	case refusedRequest:
		return r.err
	case refusedUnknownClass:
		return ErrUnknownVehicleClass
	case refusedClassFit:
		return fmt.Errorf("party does not fit class %s: %w", r.class, ErrPartyAdmission)
	case refusedExpressClass:
		return fmt.Errorf("express service needs the express class: %w", ErrPartyAdmission)
	case refusedPartyLimit:
		return fmt.Errorf("pooling party limit must be 1 to %d: %w", r.limit, ErrPartyAdmission)
	case refusedActiveConsent:
		return fmt.Errorf("all active parties must consent to sharing: %w", ErrPartyAdmission)
	case refusedActiveSize:
		return fmt.Errorf("active party size is invalid: %w", ErrInvalidTripOptions)
	case refusedActiveOptions:
		return fmt.Errorf("active party: %w", r.err)
	case refusedActiveClassFit:
		return fmt.Errorf("active party does not fit class %s: %w", r.class, ErrPartyAdmission)
	case refusedActiveService:
		return fmt.Errorf("active party has a different service: %w", ErrPartyAdmission)
	case refusedActivePair:
		return fmt.Errorf("active party has a different express pair: %w", ErrPartyAdmission)
	case refusedActiveCapacity:
		return fmt.Errorf("active parties exceed available capacity: %w", ErrPartyAdmission)
	default:
		return fmt.Errorf("passenger seats are full: %w", ErrPartyAdmission)
	}
}

// checkPartyAdmission runs the checks of
// CheckPartyAdmissionWithOrderContract in its order. It makes no error, so
// a caller that needs only the answer pays no formatting.
func checkPartyAdmission(input PartyAdmissionInput, contract OrderContract) admissionResult {
	request, err := NormalizeTripOptionsWithOrderContract(input.Request, contract)
	if err != nil {
		return admissionResult{refusal: refusedRequest, err: err}
	}
	profile, ok := LookupVehicleClassWithOrderContract(input.Class, contract)
	if !ok {
		return admissionResult{refusal: refusedUnknownClass}
	}
	if request.PartySize > profile.MaxNewPartySize {
		return admissionResult{refusal: refusedClassFit, class: profile.Class}
	}
	limit := MaxSharedRideParties
	if request.Service == ExpressServiceChoice {
		if profile.Class != ExpressClass {
			return admissionResult{refusal: refusedExpressClass}
		}
		limit = MaxExpressParties
	}
	if input.PartyLimit < 1 || input.PartyLimit > limit {
		return admissionResult{refusal: refusedPartyLimit, limit: limit}
	}
	parties, passengers := 0, 0
	for _, party := range input.Active {
		if party.Completed {
			continue
		}
		if result := checkActiveParty(request, party.TripOptions, profile, contract); result.refusal != admitted {
			return result
		}
		parties++
		if parties >= input.PartyLimit || party.PartySize > profile.Capacity()-passengers {
			return admissionResult{refusal: refusedActiveCapacity}
		}
		passengers += party.PartySize
	}
	if request.PartySize > profile.Capacity()-passengers {
		return admissionResult{refusal: refusedSeatsFull}
	}
	return admissionResult{}
}

func checkActiveParty(request, party TripOptions, profile VehicleClassSpec, contract OrderContract) admissionResult {
	if request.SharingConsent != SharedConsent || party.SharingConsent != SharedConsent {
		return admissionResult{refusal: refusedActiveConsent}
	}
	if party.PartySize < 1 || party.PartySize > newPartyLimit(contract) {
		return admissionResult{refusal: refusedActiveSize}
	}
	effective, err := NormalizeTripOptionsWithOrderContract(party, contract)
	if err != nil {
		return admissionResult{refusal: refusedActiveOptions, err: err}
	}
	if effective.PartySize > profile.MaxNewPartySize {
		return admissionResult{refusal: refusedActiveClassFit, class: profile.Class}
	}
	if effective.Service != request.Service || effective.ServiceID != request.ServiceID {
		return admissionResult{refusal: refusedActiveService}
	}
	if request.Service == ExpressServiceChoice && (effective.From != request.From || effective.To != request.To) {
		return admissionResult{refusal: refusedActivePair}
	}
	return admissionResult{}
}
