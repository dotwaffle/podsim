package session

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

// TestPackedDocumentsRefuseCaseVariants covers the packed Express decodes,
// which do not go through decodeStreamJSON. Each
// document must refuse a member name that differs only in case, and an
// order with an unknown member.
func TestPackedDocumentsRefuseCaseVariants(t *testing.T) {
	t.Parallel()
	expressTopology, expressFrame := expressGuardFrame(t)
	expressBase := expressFrame
	expressFrame.State.Revision++
	expressFrame.State.Simulation.Pending = []sim.Request{{ID: 1, From: "harbor", To: "market", PartySize: 2, SharingConsent: sim.SharedConsent, Service: sim.ExpressServiceChoice, ServiceID: "harbor-market"}}
	documents := []struct {
		name   string
		encode func() ([]byte, error)
		decode func([]byte) error
	}{
		{"express stream", func() ([]byte, error) {
			return EncodeStreamJSON(StreamEnvelope{OrderContract: sim.ExpressOrderContract, Kind: "full", Stream: "case", Sequence: 1,
				Source: sourceOf(expressFrame), Full: &expressFrame})
		}, func(raw []byte) error { _, err := DecodeStreamJSON(raw); return err }},
		{"express pending delta", func() ([]byte, error) {
			delta, err := makeDelta(expressBase, expressFrame)
			if err != nil {
				return nil, err
			}
			if _, found := delta.Groups["pending"]; !found {
				return nil, errors.New("delta does not replace the pending orders")
			}
			return EncodeStreamJSON(StreamEnvelope{OrderContract: sim.ExpressOrderContract, Kind: "delta", Stream: "case", Sequence: 2, Base: 1,
				Source: sourceOf(expressFrame), Delta: &delta})
		}, func(raw []byte) error {
			// A delta carries the pending orders as a raw group, and
			// ApplyStream decodes the group.
			decoded, err := DecodeStreamJSON(raw)
			if err != nil {
				return err
			}
			_, err = ApplyStream(expressBase, "case", 1, decoded)
			return err
		}},
		{"express state", func() ([]byte, error) { return EncodeStateJSON(expressTopology, expressFrame) },
			func(raw []byte) error { _, err := DecodeStateJSON(raw); return err }},
	}
	for _, document := range documents {
		raw, err := document.encode()
		if err != nil {
			t.Fatal(document.name, err)
		}
		if err := document.decode(raw); err != nil {
			t.Fatal(document.name, "exact document refused:", err)
		}
		type variant struct {
			from, to string
			// unknown is the member that the decode error must name. The
			// decoders use the error text of encoding/json, which does
			// not wrap jsonv2.ErrUnknownName.
			unknown string
		}
		variants := []variant{{`"vehicles":`, `"Vehicles":`, ""}}
		if bytes.Contains(raw, []byte(`"full":`)) || bytes.Contains(raw, []byte(`"frame":`)) {
			// A delta without vehicle changes has no motion values.
			variants = append(variants, variant{`"speed":`, `"Speed":`, ""})
		}
		// All packed documents share the order record decode, which
		// reports the unknown member and not the state of the enclosing
		// decoder.
		variants = append(variants, variant{`"partySize":`, `"PartySize":`, "PartySize"}, variant{`"partySize":`, `"unknownMember":1,"partySize":`, "unknownMember"})
		for _, variant := range variants {
			t.Run(document.name+"/"+variant.to, func(t *testing.T) {
				t.Parallel()
				if !bytes.Contains(raw, []byte(variant.from)) {
					t.Fatal("document has no", variant.from)
				}
				err := document.decode(bytes.Replace(raw, []byte(variant.from), []byte(variant.to), 1))
				if err == nil {
					t.Fatalf("accepted %s", variant.to)
				}
				if want := fmt.Sprintf("json: unknown field %q", variant.unknown); variant.unknown != "" && err.Error() != want {
					t.Fatalf("got %v, want %s", err, want)
				}
			})
		}
	}
}
