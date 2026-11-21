package sim

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
)

// referenceCheckPartyAdmission is CheckPartyAdmissionWithOrderContract
// before the checks returned a refusal. Each refusal makes its error.
func referenceCheckPartyAdmission(input PartyAdmissionInput, contract OrderContract) error {
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
	parties, passengers := 0, 0
	for _, party := range input.Active {
		if party.Completed {
			continue
		}
		if err := referenceCheckActiveParty(request, party.TripOptions, profile, contract); err != nil {
			return err
		}
		parties++
		if parties >= input.PartyLimit || party.PartySize > profile.Capacity()-passengers {
			return fmt.Errorf("active parties exceed available capacity: %w", ErrPartyAdmission)
		}
		passengers += party.PartySize
	}
	if request.PartySize > profile.Capacity()-passengers {
		return fmt.Errorf("passenger seats are full: %w", ErrPartyAdmission)
	}
	return nil
}

// referenceCheckActiveParty is checkActiveParty before it returned a
// refusal.
func referenceCheckActiveParty(request, party TripOptions, profile VehicleClassSpec, contract OrderContract) error {
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

// admissionInputs makes random admission inputs. Most values are valid,
// so a check reaches the late refusals, and the other values make each
// early refusal.
type admissionInputs struct {
	random *rand.Rand
}

// pick returns valid with probability 3/4, and otherwise a value of all.
func pick[T any](g admissionInputs, valid, all []T) T {
	if g.random.IntN(4) > 0 {
		return valid[g.random.IntN(len(valid))]
	}
	return all[g.random.IntN(len(all))]
}

func (g admissionInputs) options() TripOptions {
	ids := []string{"waterloo", "bank"}
	badIDs := []string{"", "waterloo", "bank", "no space", "\xff", strings.Repeat("a", orderIDBytes+1)}
	options := TripOptions{
		From:           pick(g, ids, badIDs),
		To:             pick(g, ids, badIDs),
		PartySize:      pick(g, []int{0, 1, 2, 3, 4}, []int{-1, 0, 5, 6, 8, 9, 13, 20, 21}),
		SharingConsent: pick(g, []SharingConsent{SharedConsent}, []SharingConsent{"", PrivateConsent, SharedConsent, "maybe"}),
		Service:        pick(g, []ServiceChoice{"", OnDemandService, ExpressServiceChoice}, []ServiceChoice{"", OnDemandService, ExpressServiceChoice, "bus"}),
	}
	if options.Service == ExpressServiceChoice {
		options.ServiceID = pick(g, []string{"west", "east"}, badIDs)
	} else if g.random.IntN(8) == 0 {
		options.ServiceID = "west"
	}
	return options
}

func (g admissionInputs) input() (PartyAdmissionInput, OrderContract) {
	input := PartyAdmissionInput{
		Class:      pick(g, []VehicleClass{LegacyClass, CompactClass, GroupClass, ExpressClass}, []VehicleClass{"", "tram", ExpressClass}),
		Request:    g.options(),
		PartyLimit: pick(g, []int{1, 2, 3, 4, 8, 20}, []int{-1, 0, 4, 5, 20, 21}),
	}
	for range g.random.IntN(6) {
		input.Active = append(input.Active, PartyFacts{TripOptions: g.options(), Completed: g.random.IntN(5) == 0})
	}
	// An Express party often shares the pair and the service of the
	// request, so the check reaches the capacity refusals.
	if g.random.IntN(2) == 0 {
		for index := range input.Active {
			party := &input.Active[index]
			party.From, party.To, party.Service, party.ServiceID = input.Request.From, input.Request.To, input.Request.Service, input.Request.ServiceID
		}
	}
	contract := pick(g, []OrderContract{"", ExpressOrderContract}, []OrderContract{"", ExpressOrderContract, "v0"})
	return input, contract
}

// TestPartyAdmissionMatchesReference compares the error and the answer of
// the admission check with the reference on random inputs. The errors
// have the same text, the same type, and the same sentinels. Each refusal
// occurs.
func TestPartyAdmissionMatchesReference(t *testing.T) {
	t.Parallel()
	g := admissionInputs{random: rand.New(rand.NewPCG(7, 8))}
	sentinels := []error{ErrPartyAdmission, ErrInvalidTripOptions, ErrUnknownVehicleClass}
	seen := map[admissionRefusal]int{}
	trials := 400000
	if testing.Short() {
		trials = 200000
	}
	for range trials {
		input, contract := g.input()
		want := referenceCheckPartyAdmission(input, contract)
		got := CheckPartyAdmissionWithOrderContract(input, contract)
		seen[checkPartyAdmission(input, contract).refusal]++
		if admits := partyAdmits(input, contract); admits != (want == nil) {
			t.Fatalf("%+v %q: admits %v, reference error %v", input, contract, admits, want)
		}
		if (got == nil) != (want == nil) {
			t.Fatalf("%+v %q: error %v, reference %v", input, contract, got, want)
		}
		if want == nil {
			continue
		}
		if got.Error() != want.Error() || fmt.Sprintf("%T", got) != fmt.Sprintf("%T", want) {
			t.Fatalf("%+v %q: error %T %q, reference %T %q", input, contract, got, got, want, want)
		}
		for _, sentinel := range sentinels {
			if errors.Is(got, sentinel) != errors.Is(want, sentinel) {
				t.Fatalf("%+v %q: error %q is %v %v, reference %v", input, contract, got, sentinel, errors.Is(got, sentinel), errors.Is(want, sentinel))
			}
		}
	}
	t.Logf("refusals %v", seen)
	for refusal := admitted; refusal <= refusedSeatsFull; refusal++ {
		if seen[refusal] == 0 {
			t.Fatalf("refusal %d does not occur: %v", refusal, seen)
		}
	}
}
