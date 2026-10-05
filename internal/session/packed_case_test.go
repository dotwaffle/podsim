package session

import (
	"bytes"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

// TestPackedDocumentsRefuseCaseVariants covers the packed Express and
// coupling decodes, which do not go through decodeStreamJSON. Each
// document must refuse a member name that differs only in case.
func TestPackedDocumentsRefuseCaseVariants(t *testing.T) {
	t.Parallel()
	expressTopology, expressFrame := expressGuardFrame(t)
	expressFrame.State.Simulation.Pending = []sim.Request{{ID: 1, From: "harbor", To: "market", PartySize: 2, SharingConsent: sim.SharedConsent, Service: sim.ExpressServiceChoice, ServiceID: "harbor-market"}}
	_, couplingTopology, couplingFrame := couplingStreamFixture(t, couplingPhaseFixtures(t).Frames[0], sim.ExpressOrderContract)
	documents := []struct {
		name    string
		encode  func() ([]byte, error)
		decode  func([]byte) error
		records bool
	}{
		{"express stream", func() ([]byte, error) {
			return EncodeStreamJSON(StreamEnvelope{OrderContract: sim.ExpressOrderContract, TextEncoding: ExpressTextEncoding, Kind: "full", Stream: "case", Sequence: 1,
				Source: sourceOf(expressFrame), Full: &expressFrame})
		}, func(raw []byte) error { _, err := DecodeStreamJSONVersion(raw, ExpressStreamVersion); return err }, true},
		{"express state", func() ([]byte, error) { return EncodeExpressStateJSON(expressTopology, expressFrame) },
			func(raw []byte) error { _, err := DecodeExpressStateJSON(raw); return err }, true},
		{"coupling stream", func() ([]byte, error) { return EncodeStreamJSON(couplingFullEnvelope(couplingFrame)) },
			func(raw []byte) error { _, err := DecodeStreamJSONVersion(raw, CouplingStreamVersion); return err }, false},
		{"coupling state", func() ([]byte, error) { return EncodeCouplingStateJSON(couplingTopology, couplingFrame) },
			func(raw []byte) error { _, err := DecodeCouplingStateJSON(raw); return err }, false},
	}
	for _, document := range documents {
		raw, err := document.encode()
		if err != nil {
			t.Fatal(document.name, err)
		}
		if err := document.decode(raw); err != nil {
			t.Fatal(document.name, "exact document refused:", err)
		}
		variants := []struct{ from, to string }{{`"speed":`, `"Speed":`}, {`"vehicles":`, `"Vehicles":`}}
		if document.records {
			// The coupling fixture has no order records. All packed
			// documents share the order record decode.
			variants = append(variants, struct{ from, to string }{`"partySize":`, `"PartySize":`})
		}
		for _, variant := range variants {
			t.Run(document.name+"/"+variant.to, func(t *testing.T) {
				t.Parallel()
				if !bytes.Contains(raw, []byte(variant.from)) {
					t.Fatal("document has no", variant.from)
				}
				if document.decode(bytes.Replace(raw, []byte(variant.from), []byte(variant.to), 1)) == nil {
					t.Fatalf("accepted %s", variant.to)
				}
			})
		}
	}
}
