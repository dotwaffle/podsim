package session

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// assertPlainStreamMaximum shows that the bounded scan and the decoder
// accept an encoder maximum without contract markers.
func assertPlainStreamMaximum(t *testing.T, raw []byte) {
	t.Helper()
	if raceEnabled {
		return
	}
	if err := prescanJSON(raw, streamLimits(contractMarkers{})); err != nil {
		t.Fatalf("plain maximum failed the bounded scan: %v", err)
	}
	assertExplicitArrayBounds(t, "plain stream maximum", raw, streamLimits(contractMarkers{}))
	if _, err := DecodeStreamJSON(raw); err != nil {
		t.Fatalf("plain maximum: %v", err)
	}
}

// The bounded scan runs before the token scans and the typed decode. A
// document deeper than the jsontext decoder's own cap fails with the scan
// error, and the other shapes fail with a limit error in place of a marker
// or member error.
func TestStreamDecodeBoundsBeforeTokenScans(t *testing.T) {
	t.Parallel()
	zeros := func(n int64) string { return "[" + strings.TrimSuffix(strings.Repeat("0,", int(n)), ",") + "]" }
	members := func(n int) string {
		var b strings.Builder
		for i := range n {
			fmt.Fprintf(&b, `,"m%d":0`, i)
		}
		return b.String()
	}
	deep := strings.Repeat("[", 20000) + strings.Repeat("]", 20000)
	for _, order := range []sim.OrderContract{"", sim.ExpressOrderContract} {
		markers := contractMarkers{order: order}
		pending, riders := markers.orderBounds()
		tests := []struct {
			name string
			raw  string
			want error
		}{
			{"deeper than the decoder cap", `{"x":` + deep + `}`, errJSONTooDeep},
			{"deeper than the decoder cap under the marker", `{"orderContract":` + deep + `}`, errJSONTooDeep},
			{"deeper than the decoder cap in a replacement group", `{"delta":{"groups":{"pending":` + deep + `}}}`, errJSONTooDeep},
			{"deeper than the stream limit", `{"x":` + strings.Repeat("[", 65) + strings.Repeat("]", 65) + `}`, errJSONTooDeep},
			{"array past the element limit", `{"x":` + zeros(65537) + `}`, errJSONArrayTooLong},
			{"object past the member limit", `{"x":0` + members(256) + `}`, errJSONObjectTooLong},
			{"replacement group past the member limit", `{"delta":{"groups":{"controls":{"x":0` + members(256) + `}}}}`, errJSONObjectTooLong},
			{"vehicles past the fleet bound", `{"full":{"state":{"simulation":{"vehicles":` + zeros(project.MaxPods+1) + `}}}}`, errJSONArrayTooLong},
			{"full pending past the order bound", `{"full":{"state":{"simulation":{"pending":` + zeros(pending+1) + `}}}}`, errJSONArrayTooLong},
			{"full riders past the order bound", `{"full":{"state":{"simulation":{"vehicles":[{"riders":` + zeros(riders+1) + `}]}}}}`, errJSONArrayTooLong},
			{"delta riders past the order bound", `{"delta":{"vehicles":[{"riders":{"value":` + zeros(riders+1) + `}}]}}`, errJSONArrayTooLong},
			{"full boardings past the order bound", `{"full":{"state":{"simulation":{"vehicles":[{"boardings":` + zeros(riders+1) + `}]}}}}`, errJSONArrayTooLong},
			{"delta boardings past the order bound", `{"delta":{"vehicles":[{"boardings":{"value":` + zeros(riders+1) + `}}]}}`, errJSONArrayTooLong},
			{"replacement pending past the order bound", `{"delta":{"groups":{"pending":` + zeros(pending+1) + `}}}`, errJSONArrayTooLong},
		}
		for _, test := range tests {
			raw := test.raw
			if order != "" {
				raw = `{"orderContract":"` + string(order) + `",` + raw[1:]
			}
			t.Run(fmt.Sprintf("order=%s/%s", order, test.name), func(t *testing.T) {
				t.Parallel()
				if _, err := DecodeStreamJSON([]byte(raw)); !errors.Is(err, test.want) {
					t.Fatalf("got %v, want %v", err, test.want)
				}
			})
		}
	}
}

// stateDecoders returns the stream decoder and the HTTP state decoder by
// name.
func stateDecoders() map[string]func([]byte) error {
	return map[string]func([]byte) error{
		"stream": func(raw []byte) error {
			_, err := DecodeStreamJSON(raw)
			return err
		},
		"http": func(raw []byte) error {
			_, err := DecodeStateJSON(raw)
			return err
		},
	}
}

// The bounded scan runs before the header decode, so a document deeper than
// the jsontext decoder's own cap fails with the scan error.
func TestDecodeBoundsBeforeHeader(t *testing.T) {
	t.Parallel()
	zeros := func(n int) string { return "[" + strings.TrimSuffix(strings.Repeat("0,", n), ",") + "]" }
	members := func(n int) string {
		var b strings.Builder
		for i := range n {
			fmt.Fprintf(&b, `,"m%d":0`, i)
		}
		return b.String()
	}
	packed := `"orderContract":"` + string(sim.ExpressOrderContract) + `",`
	tests := []struct {
		name string
		raw  string
		want error
	}{
		{"deeper than the decoder cap", `{"x":` + strings.Repeat("[", 20000) + strings.Repeat("]", 20000) + `}`, errJSONTooDeep},
		{"deeper than the decoder cap under the marker", `{"orderContract":` + strings.Repeat("[", 20000) + strings.Repeat("]", 20000) + `}`, errJSONTooDeep},
		{"deeper than the stream limit", `{"x":` + strings.Repeat("[", 65) + strings.Repeat("]", 65) + `}`, errJSONTooDeep},
		{"array past the element limit", `{"x":` + zeros(65537) + `}`, errJSONArrayTooLong},
		{"object past the member limit", `{"x":0` + members(256) + `}`, errJSONObjectTooLong},
		{"unpacked full pending past the unpacked bound", `{"full":{"state":{"simulation":{"pending":` + zeros(maxSavedTrips+1) + `}}}}`, errJSONArrayTooLong},
		{"unpacked frame pending past the unpacked bound", `{"frame":{"state":{"simulation":{"pending":` + zeros(maxSavedTrips+1) + `}}}}`, errJSONArrayTooLong},
		{"unpacked delta riders past the unpacked bound", `{"delta":{"vehicles":[{"riders":{"value":` + zeros(9) + `}}]}}`, errJSONArrayTooLong},
		{"packed full pending past the packed bound", `{` + packed + `"full":{"state":{"simulation":{"pending":` + zeros(sim.MaxExpressWaitingTrips+1) + `}}}}`, errJSONArrayTooLong},
		{"packed frame pending past the packed bound", `{` + packed + `"frame":{"state":{"simulation":{"pending":` + zeros(sim.MaxExpressWaitingTrips+1) + `}}}}`, errJSONArrayTooLong},
		{"folded marker pending past the packed bound", `{"ORDERCONTRACT":"` + string(sim.ExpressOrderContract) + `","full":{"state":{"simulation":{"pending":` + zeros(sim.MaxExpressWaitingTrips+1) + `}}}}`, errJSONArrayTooLong},
	}
	for name, decode := range stateDecoders() {
		for _, test := range tests {
			t.Run(name+"/"+test.name, func(t *testing.T) {
				t.Parallel()
				if err := decode([]byte(test.raw)); !errors.Is(err, test.want) {
					t.Fatalf("got %v, want %v", err, test.want)
				}
			})
		}
	}
}

// The Express limits, which bound the read of the root markers, contain
// the limits of every other marker.
func TestStreamLimitsPackedContainUnpacked(t *testing.T) {
	t.Parallel()
	wide, exact := streamLimits(contractMarkers{order: sim.ExpressOrderContract}), streamLimits(contractMarkers{})
	if wide.depth != exact.depth || wide.elements != exact.elements || wide.members != exact.members ||
		wide.stringBytes != exact.stringBytes || wide.allowInvalidUTF8 != exact.allowInvalidUTF8 {
		t.Fatal("packed and unpacked limits differ outside array bounds")
	}
	if !maps.Equal(maps.Collect(func(yield func(string, bool) bool) {
		for path := range wide.arrays {
			if !yield(path, true) {
				return
			}
		}
	}), maps.Collect(func(yield func(string, bool) bool) {
		for path := range exact.arrays {
			if !yield(path, true) {
				return
			}
		}
	})) {
		t.Fatal("packed and unpacked limits bound different paths")
	}
	for path, bound := range exact.arrays {
		if wide.arrays[path] < bound {
			t.Fatalf("packed bound %d of %s is below unpacked bound %d", wide.arrays[path], path, bound)
		}
	}
}

// scanRootMarkers reads the root markers with exact names. The last of
// duplicate markers selects the limits, and scanContractMarkers then
// refuses the duplicate.
func TestScanRootMarkers(t *testing.T) {
	t.Parallel()
	express := string(sim.ExpressOrderContract)
	tests := []struct {
		name string
		raw  string
		want contractMarkers
		err  bool
	}{
		{"absent", `{"kind":"full"}`, contractMarkers{}, false},
		{"express", `{"orderContract":"` + express + `"}`, contractMarkers{order: sim.ExpressOrderContract}, false},
		{"express last", `{"kind":"full","full":{},"orderContract":"` + express + `"}`, contractMarkers{order: sim.ExpressOrderContract}, false},
		{"folded express", `{"ORDERCONTRACT":"` + express + `"}`, contractMarkers{}, false},
		{"escaped express", `{"order\u0043ontract":"` + express + `"}`, contractMarkers{order: sim.ExpressOrderContract}, false},
		{"other contract", `{"orderContract":"other"}`, contractMarkers{order: "other"}, false},
		{"null", `{"orderContract":null}`, contractMarkers{}, false},
		{"nested", `{"full":{"orderContract":"` + express + `"}}`, contractMarkers{}, false},
		{"duplicate last wins", `{"orderContract":"` + express + `","orderContract":"other"}`, contractMarkers{order: "other"}, false},
		{"duplicate last wins express", `{"orderContract":"other","orderContract":"` + express + `"}`, contractMarkers{order: sim.ExpressOrderContract}, false},
		{"number", `{"orderContract":1}`, contractMarkers{}, true},
		{"array document", `[]`, contractMarkers{}, true},
		{"too deep", `{"a":` + strings.Repeat("[", 65) + strings.Repeat("]", 65) + `}`, contractMarkers{}, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			markers, err := scanRootMarkers([]byte(test.raw))
			if (err != nil) != test.err || !test.err && markers != test.want {
				t.Fatalf("got %+v err=%v, want %+v err=%t", markers, err, test.want, test.err)
			}
		})
	}
}

// decodeStreamJSONUnbounded is the decoder before the bounded scan moved
// to the start. It is the reference for decoded values.
func decodeStreamJSONUnbounded(data []byte, markers contractMarkers) (StreamEnvelope, error) {
	express := markers.order == sim.ExpressOrderContract
	if err := scanContractMarkers(data, express); err != nil {
		return StreamEnvelope{}, err
	}
	if err := scanPackedOrders(data); err != nil {
		return StreamEnvelope{}, err
	}
	if err := scanStreamBoardingMembers(data, markers); err != nil {
		return StreamEnvelope{}, err
	}
	if err := scanStreamServiceMembers(data, markers); err != nil {
		return StreamEnvelope{}, err
	}
	var envelope StreamEnvelope
	err := jsonv2.Unmarshal(data, &envelope, json.DefaultOptionsV1(), jsonv2.RejectUnknownMembers(true), packedDecodeOptions())
	return envelope, err
}

// streamFamilyDocuments returns a full and a delta publication without
// contract markers and with the Express marker.
func streamFamilyDocuments(t *testing.T) map[sim.OrderContract][][]byte {
	t.Helper()
	encode := func(e StreamEnvelope) []byte {
		raw, err := EncodeStreamJSON(e)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	publications := func(a, b StreamFrame, order sim.OrderContract) [][]byte {
		delta, err := makeDelta(a, b)
		if err != nil {
			t.Fatal(err)
		}
		full := StreamEnvelope{OrderContract: order, Kind: "full", Stream: "s", Sequence: 1, Source: sourceOf(a), Build: a.State.Build, Full: &a}
		changed := StreamEnvelope{OrderContract: order, Kind: "delta", Stream: "s", Sequence: 2, Base: 1, Source: sourceOf(b), Build: b.State.Build, Delta: &delta}
		return [][]byte{encode(full), encode(changed)}
	}

	s, a := streamFixture(t)
	s.Apply(Command{Client: "test", Sequence: 1, Epoch: a.State.Epoch, Action: "trip", Origin: "harbor", Destination: "market"})
	for range 20 {
		s.advance()
	}
	b, err := s.presentationFrame()
	if err != nil {
		t.Fatal(err)
	}
	foundation := publications(a, b, "")

	_, express := expressGuardFrame(t)
	next := ownStreamBoardings(express)
	next.State.Revision++
	next.State.Simulation.Tick++
	vehicle := &next.State.Simulation.Vehicles[0]
	vehicle.Riders, vehicle.Boardings = vehicle.Riders[1:], vehicle.Boardings[1:]
	return map[sim.OrderContract][][]byte{"": foundation, sim.ExpressOrderContract: publications(express, next, sim.ExpressOrderContract)}
}

func TestStreamDecodeMatchesUnboundedDecoder(t *testing.T) {
	t.Parallel()
	families := streamFamilyDocuments(t)
	for order, documents := range families {
		for _, raw := range documents {
			want, wantErr := decodeStreamJSONUnbounded(raw, contractMarkers{order: order})
			got, err := DecodeStreamJSON(raw)
			if err != nil || wantErr != nil {
				t.Fatalf("order %q: got %v, reference %v", order, err, wantErr)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("order %q %s publication decoded to a different value", order, got.Kind)
			}
			if got.Delta != nil && (len(got.Delta.Groups) == 0 || len(got.Delta.Vehicles) == 0) {
				t.Fatalf("order %q delta has no replacement groups or vehicles", order)
			}
		}
	}
}

// The HTTP state carries a stream frame with route windows, so its arrays
// have the bounds of the stream. TestStreamMaximumEncoding checks the
// widest HTTP state.
func TestStateJSONBounds(t *testing.T) {
	t.Parallel()
	zeros := func(n int) string { return "[" + strings.TrimSuffix(strings.Repeat("0,", n), ",") + "]" }
	tests := []struct {
		name string
		raw  string
		want error
	}{
		{"deeper than the stream limit", `{"x":` + strings.Repeat("[", 65) + strings.Repeat("]", 65) + `}`, errJSONTooDeep},
		{"vehicles past the fleet bound", `{"frame":{"state":{"simulation":{"vehicles":` + zeros(project.MaxPods+1) + `}}}}`, errJSONArrayTooLong},
		{"pending past the order bound", `{"frame":{"state":{"simulation":{"pending":` + zeros(maxSavedTrips+1) + `}}}}`, errJSONArrayTooLong},
		{"riders past the order bound", `{"frame":{"state":{"simulation":{"vehicles":[{"riders":` + zeros(sim.MaxSharedRideParties+1) + `}]}}}}`, errJSONArrayTooLong},
		{"complete route", `{"frame":{"state":{"simulation":{"vehicles":[{"routeLaneIDs":["l"]}]}}}}`, errJSONArrayTooLong},
		{"topology lanes past the lane bound", `{"topology":{"network":{"lanes":` + zeros(project.MaxLanes+1) + `}}}`, errJSONArrayTooLong},
		{"invalid UTF-8", "{\"frame\":{\"state\":{\"epoch\":\"\xff\"}}}", nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := DecodeStateJSON([]byte(test.raw))
			if test.want == nil {
				if err == nil || errors.Is(err, errJSONArrayTooLong) {
					t.Fatalf("got %v, want a refusal of the text", err)
				}
				return
			}
			if !errors.Is(err, test.want) {
				t.Fatalf("got %v, want %v", err, test.want)
			}
		})
	}
}
