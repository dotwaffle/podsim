package parkride

import (
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

// conservationPayload returns a small checkpoint payload at tick 100 that
// validateConservation accepts. Car a waits for its return pod, car b
// completed its journey, and car c rides its outward pod.
func conservationPayload() checkpointPayload {
	itinerary := func(id string) Itinerary {
		return Itinerary{ID: id, CarID: id, Lot: "lot", Destination: "market", PartySize: 1, SharingConsent: sim.PrivateConsent}
	}
	request := func(id int, from, to string, requested, boarded int64) sim.SavedRequest {
		return sim.SavedRequest{ID: id, From: from, To: to, PartySize: 1, SharingConsent: sim.PrivateConsent, Service: sim.OnDemandService, RequestedTick: requested, BoardedTick: boarded}
	}
	refused := checkpointLeg{OfferedTick: -1, BoardedTick: -1, AlightedTick: -1}
	var p checkpointPayload
	p.Tick = 100
	p.Origin.Plan = Plan{
		Lots:        []Lot{{ID: "lot", Hub: "harbor", Capacity: 2}},
		Itineraries: []Itinerary{itinerary("a"), itinerary("b"), itinerary("c")},
	}
	p.Ledger.Records = []checkpointRecord{
		{Stage: "return-pod", Held: true, CarArrivalTick: 5, ReturnEligibleTick: 35, CarReleaseTick: -1, HomeArrivalTick: -1, DoorToDoorTicks: -1,
			Outward: checkpointLeg{RequestID: 1, OfferedTick: 10, BoardedTick: 20, AlightedTick: 30},
			Return:  checkpointLeg{RequestID: 2, OfferedTick: 40, BoardedTick: -1, AlightedTick: -1}},
		{Stage: "terminal", Outcome: "completed", CarArrivalTick: 5, ReturnEligibleTick: 28, CarReleaseTick: 50, HomeArrivalTick: 60, DoorToDoorTicks: 55,
			Outward: checkpointLeg{RequestID: 3, OfferedTick: 10, BoardedTick: 15, AlightedTick: 25},
			Return:  checkpointLeg{RequestID: 4, OfferedTick: 30, BoardedTick: 35, AlightedTick: 45}},
		{Stage: "outward-pod", Held: true, CarArrivalTick: 5, ReturnEligibleTick: -1, CarReleaseTick: -1, HomeArrivalTick: -1, DoorToDoorTicks: -1,
			Outward: checkpointLeg{RequestID: 5, OfferedTick: 10, BoardedTick: 20, AlightedTick: -1},
			Return:  refused},
	}
	p.Ledger.Lots = []checkpointLot{{Occupancy: 2, Peak: 2}}
	p.Native.RequestID, p.Native.Completed, p.Native.Boarded = 5, 3, 4
	p.Native.Waiting = []sim.SavedTrip{{Request: request(2, "market", "harbor", 40, -1)}}
	p.Native.Pods = []sim.SavedPod{{Riders: []sim.SavedRequest{request(5, "harbor", "market", 10, 20)}}}
	return p
}

// TestValidateConservationRefusalOrder pins each refusal of
// validateConservation and validateLeg, and the order of the passes:
// the records, the held slots, the native counters, and then the native
// requests.
func TestValidateConservationRefusalOrder(t *testing.T) {
	t.Parallel()
	if err := validateConservation(conservationPayload()); err != nil {
		t.Fatalf("base payload refused: %v", err)
	}
	for _, tc := range []struct {
		name, want string
		mutate     func(*checkpointPayload)
	}{
		{"stage", "invalid checkpoint car stage or outcome", func(p *checkpointPayload) { p.Ledger.Records[0].Stage = "parked" }},
		{"terminal", "checkpoint terminal stage mismatch", func(p *checkpointPayload) { p.Ledger.Records[0].Stage = "terminal" }},
		{"lot", "checkpoint itinerary has no lot", func(p *checkpointPayload) { p.Origin.Plan.Itineraries[0].Lot = "other" }},
		{"leg tick", "invalid checkpoint pod leg tick", func(p *checkpointPayload) { p.Ledger.Records[0].Outward.OfferedTick = 101 }},
		{"unaccepted receipts", "unaccepted checkpoint leg has native receipts", func(p *checkpointPayload) { p.Ledger.Records[2].Return.BoardedTick = 5 }},
		{"offer refusal", "invalid checkpoint offer refusal", func(p *checkpointPayload) { p.Ledger.Records[2].Return.Reason = "other" }},
		{"accepted leg", "invalid checkpoint accepted leg", func(p *checkpointPayload) { p.Ledger.Records[0].Outward.Reason = "queue-limit" }},
		{"duplicate binding", "duplicate checkpoint request binding", func(p *checkpointPayload) { p.Ledger.Records[1].Outward.RequestID = 1 }},
		{"car tick", "invalid checkpoint car tick", func(p *checkpointPayload) { p.Ledger.Records[0].ReturnEligibleTick = -2 }},
		{"future", "checkpoint car event lies in the future", func(p *checkpointPayload) { p.Ledger.Records[0].CarArrivalTick = 101 }},
		{"full lot", "full-lot checkpoint contains an offer or held slot", func(p *checkpointPayload) { p.Ledger.Records[1].Outcome = "full-lot" }},
		{"stranded", "stranded checkpoint lost its held car", func(p *checkpointPayload) { p.Ledger.Records[1].Outcome = "stranded" }},
		{"completed", "completed checkpoint lacks whole journey receipts", func(p *checkpointPayload) { p.Ledger.Records[1].DoorToDoorTicks = -1 }},
		{"recovered refusal", "recovered refusal checkpoint lacks recovery receipts", func(p *checkpointPayload) { p.Ledger.Records[1].Outcome = "recovered-refusal" }},
		{"release", "checkpoint release contradicts held slot", func(p *checkpointPayload) { p.Ledger.Records[1].CarReleaseTick = 4 }},
		{"held slots", "checkpoint held-slot conservation mismatch", func(p *checkpointPayload) { p.Ledger.Lots[0].Occupancy = 1 }},
		{"counters", "checkpoint native order counters mismatch", func(p *checkpointPayload) { p.Native.RequestID = 6 }},
		{"requeue", "checkpoint contains historical requeue", func(p *checkpointPayload) { p.Native.Waiting[0].Boarded = true }},
		{"immutable request", "checkpoint immutable native request mismatch", func(p *checkpointPayload) { p.Native.Waiting[0].Request.To = "garden" }},
		{"boarding receipt", "checkpoint boarding receipt mismatch", func(p *checkpointPayload) { p.Native.Pods[0].Riders[0].BoardedTick = 21 }},
		{"native class", "checkpoint contains unsupported native class", func(p *checkpointPayload) { p.Native.Pods[0].Class = sim.ExpressClass }},
		{"lost party", "checkpoint lost an accepted party", func(p *checkpointPayload) { p.Native.Waiting = nil }},
		// Two faults: the refusal of the earlier pass comes first.
		{"record before held slots", "invalid checkpoint car stage or outcome", func(p *checkpointPayload) {
			p.Ledger.Records[0].Stage = "parked"
			p.Ledger.Lots[0].Occupancy = 1
		}},
		{"held slots before counters", "checkpoint held-slot conservation mismatch", func(p *checkpointPayload) {
			p.Ledger.Lots[0].Occupancy = 1
			p.Native.RequestID = 6
		}},
		{"counters before native requests", "checkpoint native order counters mismatch", func(p *checkpointPayload) {
			p.Native.RequestID = 6
			p.Native.Waiting[0].Boarded = true
		}},
		{"waiting before pods", "checkpoint contains historical requeue", func(p *checkpointPayload) {
			p.Native.Waiting[0].Boarded = true
			p.Native.Pods[0].Class = sim.ExpressClass
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := conservationPayload()
			tc.mutate(&p)
			if err := validateConservation(p); err == nil || err.Error() != tc.want {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}
