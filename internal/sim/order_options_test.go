package sim

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestNormalizeTripOptions(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		input TripOptions
		want  TripOptions
	}{
		{"omitted defaults", TripOptions{From: "harbor", To: "market"}, privateOrder(1)},
		{"explicit private group", privateOrder(8), privateOrder(8)},
		{"explicit shared group", sharedOrder(8), sharedOrder(8)},
		{"express", expressOrder(1), expressOrder(1)},
		{"express group", expressOrder(8), expressOrder(8)},
		{"ID characters", TripOptions{From: "a.B", To: "0+-", SharingConsent: SharedConsent, Service: ExpressServiceChoice, ServiceID: "e.1+x-Y"}, TripOptions{From: "a.B", To: "0+-", PartySize: 1, SharingConsent: SharedConsent, Service: ExpressServiceChoice, ServiceID: "e.1+x-Y"}},
		{"bounded express ID", TripOptions{From: "harbor", To: "market", PartySize: 1, SharingConsent: SharedConsent, Service: ExpressServiceChoice, ServiceID: strings.Repeat("e", 64)}, TripOptions{From: "harbor", To: "market", PartySize: 1, SharingConsent: SharedConsent, Service: ExpressServiceChoice, ServiceID: strings.Repeat("e", 64)}},
		{"bounded IDs", TripOptions{From: strings.Repeat("a", 64), To: strings.Repeat("b", 64)}, TripOptions{From: strings.Repeat("a", 64), To: strings.Repeat("b", 64), PartySize: 1, SharingConsent: PrivateConsent, Service: OnDemandService}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			before := test.input
			got, err := NormalizeTripOptions(test.input)
			if err != nil || got != test.want {
				t.Fatalf("NormalizeTripOptions = %+v, %v, want %+v", got, err, test.want)
			}
			if test.input != before {
				t.Fatalf("normalization changed input: %+v", test.input)
			}
		})
	}
}

func TestNormalizeTripOptionsRejectsInvalid(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		edit func(*TripOptions)
	}{
		{"missing origin", func(o *TripOptions) { o.From = "" }},
		{"long origin", func(o *TripOptions) { o.From = strings.Repeat("a", 65) }},
		{"missing destination", func(o *TripOptions) { o.To = "" }},
		{"long destination", func(o *TripOptions) { o.To = strings.Repeat("a", 65) }},
		{"space in origin", func(o *TripOptions) { o.From = " " }},
		{"control in destination", func(o *TripOptions) { o.To = "\t" }},
		{"express service ID character", func(o *TripOptions) {
			o.Service, o.ServiceID, o.SharingConsent = ExpressServiceChoice, "express_1", SharedConsent
		}},
		{"same station", func(o *TripOptions) { o.To = o.From }},
		{"negative party", func(o *TripOptions) { o.PartySize = -1 }},
		{"oversized party", func(o *TripOptions) { o.PartySize = 9 }},
		{"overflow party", func(o *TripOptions) { o.PartySize = int(^uint(0) >> 1) }},
		{"unknown consent", func(o *TripOptions) { o.SharingConsent = "yes" }},
		{"legacy consent", func(o *TripOptions) { o.SharingConsent = "legacy-unknown" }},
		{"unknown service", func(o *TripOptions) { o.Service = "hub" }},
		{"on-demand service ID", func(o *TripOptions) { o.ServiceID = "express-1" }},
		{"express without service ID", func(o *TripOptions) { o.Service, o.SharingConsent = ExpressServiceChoice, SharedConsent }},
		{"express long service ID", func(o *TripOptions) {
			o.Service, o.ServiceID, o.SharingConsent = ExpressServiceChoice, strings.Repeat("a", 65), SharedConsent
		}},
		{"private express", func(o *TripOptions) { o.Service, o.ServiceID = ExpressServiceChoice, "express-1" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := privateOrder(1)
			test.edit(&input)
			before := input
			got, err := NormalizeTripOptions(input)
			if !errors.Is(err, ErrInvalidTripOptions) || got != (TripOptions{}) {
				t.Fatalf("invalid options returned %+v, %v", got, err)
			}
			if input != before {
				t.Fatalf("rejection changed input: %+v", input)
			}
		})
	}
}

func TestCheckPartyAdmission(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		class   VehicleClass
		request TripOptions
		active  []PartyFacts
		limit   int
		want    error
	}{
		{"private singleton default", "", privateOrder(1), nil, 1, nil},
		{"omitted request defaults", LegacyClass, TripOptions{From: "harbor", To: "market"}, nil, 8, nil},
		{"private eight cannot use legacy", LegacyClass, privateOrder(8), nil, 8, ErrPartyAdmission},
		{"private two cannot use legacy", LegacyClass, privateOrder(2), nil, 8, ErrPartyAdmission},
		{"shared two cannot use legacy", LegacyClass, sharedOrder(2), nil, 8, ErrPartyAdmission},
		{"private four compact", CompactClass, privateOrder(4), nil, 1, nil},
		{"private five cannot use compact", CompactClass, privateOrder(5), nil, 8, ErrPartyAdmission},
		{"private eight group metadata", GroupClass, privateOrder(8), nil, 1, nil},
		{"private eight express metadata", ExpressClass, privateOrder(8), nil, 1, nil},
		{"shared legacy singleton", LegacyClass, sharedOrder(1), facts(sharedOrder(1)), 8, nil},
		{"eighth legacy singleton fits", LegacyClass, sharedOrder(1), repeatedFacts(sharedOrder(1), 7), 8, nil},
		{"ninth legacy singleton refused", LegacyClass, sharedOrder(1), repeatedFacts(sharedOrder(1), 8), 8, ErrPartyAdmission},
		{"sharing disabled", LegacyClass, sharedOrder(1), facts(sharedOrder(1)), 1, ErrPartyAdmission},
		{"policy cannot infer new consent", LegacyClass, privateOrder(1), facts(sharedOrder(1)), 8, ErrPartyAdmission},
		{"policy cannot infer host consent", LegacyClass, sharedOrder(1), facts(privateOrder(1)), 8, ErrPartyAdmission},
		{"omitted host consent is not inferred", LegacyClass, sharedOrder(1), facts(TripOptions{From: "harbor", To: "market", PartySize: 1}), 8, ErrPartyAdmission},
		{"later private host blocks join", LegacyClass, sharedOrder(1), facts(sharedOrder(1), privateOrder(1)), 8, ErrPartyAdmission},
		{"completed private history ignored", LegacyClass, sharedOrder(1), []PartyFacts{{TripOptions: privateOrder(8), Completed: true}}, 1, nil},
		{"completed malformed history ignored", CompactClass, privateOrder(4), []PartyFacts{{Completed: true}}, 1, nil},
		{"two groups fit compact seats", CompactClass, sharedOrder(2), facts(sharedOrder(2)), 8, nil},
		{"party count does not measure seats", CompactClass, sharedOrder(2), facts(sharedOrder(3)), 8, ErrPartyAdmission},
		{"full compact group", CompactClass, sharedOrder(1), facts(sharedOrder(4)), 8, ErrPartyAdmission},
		{"exact group seats", GroupClass, sharedOrder(5), facts(sharedOrder(3)), 8, nil},
		{"group seats exceeded", GroupClass, sharedOrder(6), facts(sharedOrder(3)), 8, ErrPartyAdmission},
		{"zero active party invalid", LegacyClass, sharedOrder(1), facts(sharedOrder(0)), 8, ErrInvalidTripOptions},
		{"invalid active endpoint", LegacyClass, sharedOrder(1), facts(TripOptions{To: "market", PartySize: 1, SharingConsent: SharedConsent}), 8, ErrInvalidTripOptions},
		{"oversized active party invalid", ExpressClass, sharedOrder(1), facts(sharedOrder(int(^uint(0) >> 1))), 8, ErrInvalidTripOptions},
		{"active group cannot use legacy", LegacyClass, sharedOrder(1), facts(sharedOrder(2)), 8, ErrPartyAdmission},
		{"unknown class", "large", privateOrder(1), nil, 1, ErrUnknownVehicleClass},
		{"zero pooling limit", LegacyClass, privateOrder(1), nil, 0, ErrPartyAdmission},
		{"negative pooling limit", LegacyClass, privateOrder(1), nil, -1, ErrPartyAdmission},
		{"on-demand cannot increase party cap", ExpressClass, sharedOrder(1), nil, 20, ErrPartyAdmission},
		{"legacy cannot serve express", LegacyClass, expressOrder(1), nil, 20, ErrPartyAdmission},
		{"compact cannot serve express", CompactClass, expressOrder(1), nil, 20, ErrPartyAdmission},
		{"group cannot serve express", GroupClass, expressOrder(1), nil, 20, ErrPartyAdmission},
		{"express cap above twenty", ExpressClass, expressOrder(1), nil, 21, ErrPartyAdmission},
		{"express same pair", ExpressClass, expressOrder(1), facts(expressOrder(1)), 20, nil},
		{"express different service", ExpressClass, expressOrder(1), facts(TripOptions{From: "harbor", To: "market", PartySize: 1, SharingConsent: SharedConsent, Service: ExpressServiceChoice, ServiceID: "express-2"}), 20, ErrPartyAdmission},
		{"express different origin", ExpressClass, expressOrder(1), facts(TripOptions{From: "garden", To: "market", PartySize: 1, SharingConsent: SharedConsent, Service: ExpressServiceChoice, ServiceID: "express-1"}), 20, ErrPartyAdmission},
		{"express different destination", ExpressClass, expressOrder(1), facts(TripOptions{From: "harbor", To: "garden", PartySize: 1, SharingConsent: SharedConsent, Service: ExpressServiceChoice, ServiceID: "express-1"}), 20, ErrPartyAdmission},
		{"express cannot join on-demand", ExpressClass, expressOrder(1), facts(sharedOrder(1)), 20, ErrPartyAdmission},
		{"on-demand cannot join express", ExpressClass, sharedOrder(1), facts(expressOrder(1)), 8, ErrPartyAdmission},
		{"on-demand routes checked by caller", CompactClass, sharedOrder(1), facts(TripOptions{From: "garden", To: "harbor", PartySize: 1, SharingConsent: SharedConsent, Service: OnDemandService}), 8, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := PartyAdmissionInput{Class: test.class, Request: test.request, Active: slices.Clone(test.active), PartyLimit: test.limit}
			before := input
			before.Active = slices.Clone(input.Active)
			if err := CheckPartyAdmission(input); !errors.Is(err, test.want) {
				t.Fatalf("CheckPartyAdmission = %v, want %v", err, test.want)
			}
			if !reflect.DeepEqual(input, before) {
				t.Fatalf("admission changed input: %+v", input)
			}
		})
	}
}

func TestExpressAdmissionCountsSeatsAndParties(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		active []PartyFacts
		want   error
	}{
		{"twentieth singleton fits", repeatedFacts(expressOrder(1), 19), nil},
		{"twenty-first singleton refused", repeatedFacts(expressOrder(1), 20), ErrPartyAdmission},
		{"two groups and four singletons fill seats", append(facts(expressOrder(8), expressOrder(8)), repeatedFacts(expressOrder(1), 3)...), nil},
		{"groups cannot exceed seats below party cap", append(facts(expressOrder(8), expressOrder(8)), repeatedFacts(expressOrder(1), 4)...), ErrPartyAdmission},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := PartyAdmissionInput{Class: ExpressClass, Request: expressOrder(1), Active: test.active, PartyLimit: 20}
			if err := CheckPartyAdmission(input); !errors.Is(err, test.want) {
				t.Fatalf("express admission = %v, want %v", err, test.want)
			}
			if err := ValidateVehicleClassProfile(ExpressClass); !errors.Is(err, ErrUnsupportedVehicleProfile) {
				t.Fatalf("logical capacity enabled physical express operation: %v", err)
			}
		})
	}
}

func privateOrder(size int) TripOptions {
	return TripOptions{From: "harbor", To: "market", PartySize: size, SharingConsent: PrivateConsent, Service: OnDemandService}
}

func sharedOrder(size int) TripOptions {
	options := privateOrder(size)
	options.SharingConsent = SharedConsent
	return options
}

func expressOrder(size int) TripOptions {
	options := sharedOrder(size)
	options.Service, options.ServiceID = ExpressServiceChoice, "express-1"
	return options
}

func facts(options ...TripOptions) []PartyFacts {
	parties := make([]PartyFacts, len(options))
	for index, option := range options {
		parties[index] = PartyFacts{TripOptions: option}
	}
	return parties
}

func repeatedFacts(options TripOptions, count int) []PartyFacts {
	parties := make([]PartyFacts, count)
	for index := range parties {
		parties[index] = PartyFacts{TripOptions: options}
	}
	return parties
}

// NormalizeTripOptions validates and defaults a copy of a new order.
// Wire decoders must reject explicit null and empty values before this call.
// This function does not check station existence or express registry membership.
func NormalizeTripOptions(options TripOptions) (TripOptions, error) {
	return NormalizeTripOptionsWithOrderContract(options, "")
}

// CheckPartyAdmission checks logical fit without changing any input.
// Callers must also validate physical approval, route, berth, and service registry.
// On-demand route and stop policies remain the caller's responsibility.
func CheckPartyAdmission(input PartyAdmissionInput) error {
	return CheckPartyAdmissionWithOrderContract(input, "")
}
