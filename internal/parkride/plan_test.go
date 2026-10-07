package parkride

import (
	"encoding/json/v2"
	"math"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

func testPlan() Plan {
	return Plan{Lots: []Lot{{ID: "lot", Hub: "harbor", Capacity: 1}}, Itineraries: []Itinerary{{ID: "a", CarID: "car-a", Lot: "lot", Destination: "market", CarSeats: 2, PartySize: 2, OutwardRefusal: "drive-home", ReturnRefusal: "retain-car"}}}
}

func planJSON(t *testing.T) string {
	t.Helper()
	data, err := json.Marshal(testPlan())
	if err != nil {
		t.Fatal(err)
	}
	return strings.Replace(string(data), `,"sharingConsent":""`, "", 1)
}

func TestDecodePlanShape(t *testing.T) {
	t.Parallel()
	valid := planJSON(t)
	for _, tc := range []struct {
		name string
		data string
	}{
		{"missing-array", strings.Replace(valid, `"lots":`, `"wrong":`, 1)},
		{"null-array", `{"lots":null,"itineraries":[]}`},
		{"null-lot", `{"lots":[null],"itineraries":[]}`},
		{"null-itinerary", `{"lots":[],"itineraries":[null]}`},
		{"null-consent", strings.Replace(valid, `"partySize":2`, `"partySize":2,"sharingConsent": null `, 1)},
		{"empty-consent", strings.Replace(valid, `"partySize":2`, `"partySize":2,"sharingConsent":""`, 1)},
		{"unknown-consent", strings.Replace(valid, `"partySize":2`, `"partySize":2,"sharingConsent":"legacy-unknown"`, 1)},
		{"duplicate-member", strings.Replace(valid, `"capacity":1`, `"capacity":1,"capacity":2`, 1)},
		{"unknown-member", strings.Replace(valid, `"capacity":1`, `"capacity":1,"extra":0`, 1)},
		{"missing-time", strings.Replace(valid, `"retrievalSeconds":0,`, "", 1)},
		{"null-time", strings.Replace(valid, `"retrievalSeconds":0`, `"retrievalSeconds":null`, 1)},
		{"fraction-time", strings.Replace(valid, `"retrievalSeconds":0`, `"retrievalSeconds":0.5`, 1)},
		{"negative-time", strings.Replace(valid, `"retrievalSeconds":0`, `"retrievalSeconds":-1`, 1)},
		{"missing-policy", strings.Replace(valid, `,"returnRefusal":"retain-car"`, "", 1)},
		{"wrong-policy", strings.Replace(valid, "retain-car", "retry", 1)},
		{"trailing-value", valid + ` {}`},
		{"oversized-file", strings.Repeat(" ", MaxPlanBytes+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := DecodePlan([]byte(tc.data), sim.Example()); err == nil {
				t.Fatal("malformed plan accepted")
			}
		})
	}
	plan, err := DecodePlan([]byte(valid), sim.Example())
	if err != nil || plan.Itineraries[0].SharingConsent != sim.PrivateConsent {
		t.Fatalf("private default: %+v, %v", plan, err)
	}
	if _, err = DecodePlan([]byte(`{"lots":[],"itineraries":[]}`), sim.Example()); err != nil {
		t.Fatal(err)
	}
}

// TestDecodePlanInputBound pads a valid plan with whitespace to the plan
// bound of 10 MiB and to one byte more.
func TestDecodePlanInputBound(t *testing.T) {
	t.Parallel()
	valid := planJSON(t)
	padded := valid + strings.Repeat(" ", 10<<20-len(valid))
	if _, err := DecodePlan([]byte(padded), sim.Example()); err != nil {
		t.Fatal("refused a plan at the bound", err)
	}
	if _, err := DecodePlan([]byte(padded+" "), sim.Example()); err == nil || err.Error() != "plan exceeds the 10 MiB input bound" {
		t.Fatalf("plan over the bound: %v", err)
	}
}

func TestPlanAdmissionAndArithmetic(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		change func(*Plan)
	}{
		{"negative-capacity", func(p *Plan) { p.Lots[0].Capacity = -1 }},
		{"parking-hub", func(p *Plan) { p.Lots[0].Hub = "parking" }},
		{"unknown-hub", func(p *Plan) { p.Lots[0].Hub = "missing" }},
		{"duplicate-lot", func(p *Plan) { p.Lots = append(p.Lots, p.Lots[0]) }},
		{"empty-ID", func(p *Plan) { p.Itineraries[0].ID = "" }},
		{"long-ID", func(p *Plan) { p.Itineraries[0].CarID = strings.Repeat("x", 65) }},
		{"lot-ID-character", func(p *Plan) {
			p.Lots[0].ID = "harbor cars"
			for i := range p.Itineraries {
				p.Itineraries[i].Lot = p.Lots[0].ID
			}
		}},
		{"itinerary-ID-character", func(p *Plan) { p.Itineraries[0].ID = "party_1" }},
		{"car-ID-character", func(p *Plan) { p.Itineraries[0].CarID = "car\x01" }},
		{"duplicate-itinerary", func(p *Plan) {
			other := p.Itineraries[0]
			other.CarID = "other"
			p.Itineraries = append(p.Itineraries, other)
		}},
		{"duplicate-car", func(p *Plan) {
			other := p.Itineraries[0]
			other.ID = "other"
			p.Itineraries = append(p.Itineraries, other)
		}},
		{"unknown-lot", func(p *Plan) { p.Itineraries[0].Lot = "missing" }},
		{"parking-destination", func(p *Plan) { p.Itineraries[0].Destination = "parking" }},
		{"same-hub", func(p *Plan) { p.Itineraries[0].Destination = "harbor" }},
		{"no-car-seats", func(p *Plan) { p.Itineraries[0].CarSeats = 0 }},
		{"car-too-small", func(p *Plan) { p.Itineraries[0].CarSeats = 1 }},
		{"party-over-eight", func(p *Plan) { p.Itineraries[0].CarSeats = 9; p.Itineraries[0].PartySize = 9 }},
		{"unknown-consent", func(p *Plan) { p.Itineraries[0].SharingConsent = "unknown" }},
		{"negative-time", func(p *Plan) { p.Itineraries[0].DepartureSeconds = -1 }},
		{"conversion-overflow", func(p *Plan) { p.Itineraries[0].OutwardSeconds = math.MaxInt64 }},
		{"arrival-overflow", func(p *Plan) {
			p.Itineraries[0].DepartureSeconds = math.MaxInt64 / sim.TicksPerSecond
			p.Itineraries[0].OutwardSeconds = 1
		}},
		{"dynamic-overflow", func(p *Plan) {
			p.Itineraries[0].ActivitySeconds = math.MaxInt64 / sim.TicksPerSecond
			p.Itineraries[0].RetrievalSeconds = 1
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := testPlan()
			tc.change(&p)
			if _, err := normalizePlan(p, sim.Example()); err == nil {
				t.Fatal("invalid direct plan accepted")
			}
		})
	}
	if fitsStorage(1, storageLimit/itineraryBytes) || fitsStorage(-1, 0) || fitsStorage(0, -1) || !fitsStorage(0, storageLimit/itineraryBytes) {
		t.Fatal("storage bound arithmetic")
	}
	tooManyNulls := `{"lots":[],"itineraries":[` + strings.Repeat("null,", storageLimit/itineraryBytes) + `null]}`
	if err := scanStorage([]byte(tooManyNulls)); err == nil {
		t.Fatal("null elements bypassed preallocation storage bound")
	}
	if _, err := addTicks(-1, 0); err == nil {
		t.Fatal("negative tick accepted")
	}
	if _, err := addTicks(math.MaxInt64, 1); err == nil {
		t.Fatal("dynamic tick overflow accepted")
	}
}

func TestPlanHashOwnsEffectiveConsent(t *testing.T) {
	t.Parallel()
	p, err := normalizePlan(testPlan(), sim.Example())
	if err != nil {
		t.Fatal(err)
	}
	hash, err := canonicalHash(p)
	if err != nil {
		t.Fatal(err)
	}
	explicit := testPlan()
	explicit.Itineraries[0].SharingConsent = sim.PrivateConsent
	other, err := normalizePlan(explicit, sim.Example())
	if err != nil {
		t.Fatal(err)
	}
	otherHash, err := canonicalHash(other)
	if err != nil || hash != otherHash {
		t.Fatal("effective private consent changed hash")
	}
	explicit.Itineraries[0].SharingConsent = sim.SharedConsent
	if p.Itineraries[0].SharingConsent != sim.PrivateConsent {
		t.Fatal("normalization retained caller storage")
	}
	shared, err := normalizePlan(explicit, sim.Example())
	if err != nil {
		t.Fatal(err)
	}
	sharedHash, _ := canonicalHash(shared)
	if sharedHash == hash {
		t.Fatal("plan hash lost consent")
	}
}
