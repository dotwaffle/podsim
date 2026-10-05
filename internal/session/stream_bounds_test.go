package session

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// assertUnpackedStreamMaximum shows that the bounded scan accepts an
// encoder maximum of family 3.
func assertUnpackedStreamMaximum(t *testing.T, raw []byte) {
	t.Helper()
	if raceEnabled {
		return
	}
	if err := prescanJSON(raw, unpackedStreamLimits()); err != nil {
		t.Fatalf("version %d maximum failed the bounded scan: %v", FoundationStreamVersion, err)
	}
	if _, err := DecodeStreamJSONVersion(raw, FoundationStreamVersion); err != nil {
		t.Fatalf("version %d maximum: %v", FoundationStreamVersion, err)
	}
}

// The bounded scan runs before the token scans and the typed decode. A
// document deeper than the jsontext decoder's own cap fails with the scan
// error, and the other shapes fail with a limit error in place of a marker
// or member error.
func TestStreamDecodeBoundsBeforeTokenScans(t *testing.T) {
	t.Parallel()
	zeros := func(n int) string { return "[" + strings.TrimSuffix(strings.Repeat("0,", n), ",") + "]" }
	members := func(n int) string {
		var b strings.Builder
		for i := range n {
			fmt.Fprintf(&b, `,"m%d":0`, i)
		}
		return b.String()
	}
	deep := strings.Repeat("[", 20000) + strings.Repeat("]", 20000)
	for version := FoundationStreamVersion; version <= ExpressStreamVersion; version++ {
		pending, riders := maxSavedTrips, sim.MaxSharedRideParties
		if version == ExpressStreamVersion {
			pending, riders = sim.MaxExpressWaitingTrips, sim.MaxExpressParties
		}
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
			t.Run(fmt.Sprintf("version %d/%s", version, test.name), func(t *testing.T) {
				t.Parallel()
				if _, err := DecodeStreamJSONVersion([]byte(test.raw), version); !errors.Is(err, test.want) {
					t.Fatalf("got %v, want %v", err, test.want)
				}
			})
		}
	}
}

// decodeStreamJSONVersionUnbounded is the decoder before the bounded scan
// moved to the start. It is the reference for decoded values.
func decodeStreamJSONVersionUnbounded(data []byte, version int) (StreamEnvelope, error) {
	if err := scanContractMarkers(data, version == ExpressStreamVersion, version == ExpressStreamVersion); err != nil {
		return StreamEnvelope{}, err
	}
	if version == ExpressStreamVersion {
		if err := prescanJSON(data, expressStreamLimits()); err != nil {
			return StreamEnvelope{}, err
		}
		if err := scanPackedOrders(data); err != nil {
			return StreamEnvelope{}, err
		}
	}
	if err := scanStreamBoardingMembers(data, version); err != nil {
		return StreamEnvelope{}, err
	}
	if err := scanStreamServiceMembers(data, version); err != nil {
		return StreamEnvelope{}, err
	}
	var envelope StreamEnvelope
	var err error
	if version == ExpressStreamVersion {
		err = jsonv2.Unmarshal(data, &envelope, json.DefaultOptionsV1(), jsonv2.RejectUnknownMembers(true), packedDecodeOptions())
	} else {
		err = decodeStreamJSON(data, &envelope)
	}
	return envelope, err
}

// streamFamilyDocuments returns a full and a delta publication of
// families 3 and 4.
func streamFamilyDocuments(t *testing.T) map[int][][]byte {
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
		marker := streamTextEncoding(order)
		full := StreamEnvelope{OrderContract: order, TextEncoding: marker, Kind: "full", Stream: "s", Sequence: 1, Source: sourceOf(a), Build: a.State.Build, Full: &a}
		changed := StreamEnvelope{OrderContract: order, TextEncoding: marker, Kind: "delta", Stream: "s", Sequence: 2, Base: 1, Source: sourceOf(b), Build: b.State.Build, Delta: &delta}
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
	return map[int][][]byte{FoundationStreamVersion: foundation, ExpressStreamVersion: publications(express, next, sim.ExpressOrderContract)}
}

func TestStreamDecodeMatchesUnboundedDecoder(t *testing.T) {
	t.Parallel()
	families := streamFamilyDocuments(t)
	for version, documents := range families {
		for _, raw := range documents {
			want, wantErr := decodeStreamJSONVersionUnbounded(raw, version)
			got, err := DecodeStreamJSONVersion(raw, version)
			if err != nil || wantErr != nil {
				t.Fatalf("version %d: got %v, reference %v", version, err, wantErr)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("version %d %s publication decoded to a different value", version, got.Kind)
			}
			if got.Delta != nil && (len(got.Delta.Groups) == 0 || len(got.Delta.Vehicles) == 0) {
				t.Fatalf("version %d delta has no replacement groups or vehicles", version)
			}
		}
	}
}

// The plain HTTP state endpoint sends the full state frame with complete
// routes. Its widest shape passes the bounded scan.
func TestPrescanStateFrameJSONAcceptsServerMaximum(t *testing.T) {
	if raceEnabled {
		t.Skip("maximum state frame proof runs without the race detector")
	}
	route := project.MaxLanes + project.MaxNodes
	frame := maximumStreamFrame(t).State
	for i := range frame.Simulation.Vehicles {
		v := &frame.Simulation.Vehicles[i]
		v.Boardings = slices.Repeat([]sim.RiderBoarding{{BerthID: "b"}}, sim.MaxSharedRideParties)
		v.RouteLaneIDs = slices.Repeat([]string{"l"}, route)
	}
	raw, err := json.Marshal(frame)
	if err != nil {
		t.Fatal(err)
	}
	if err = PrescanStateFrameJSON(append(raw, '\n')); err != nil {
		t.Fatal("widest state frame failed the bounded scan", err)
	}
	t.Logf("widest state frame with one-byte lane IDs: %d bytes", len(raw)+1)
}

func TestPrescanStateFrameJSONBounds(t *testing.T) {
	t.Parallel()
	zeros := func(n int) string { return "[" + strings.TrimSuffix(strings.Repeat("0,", n), ",") + "]" }
	tests := []struct {
		name string
		raw  string
		want error
	}{
		{"deeper than the stream limit", `{"x":` + strings.Repeat("[", 65) + strings.Repeat("]", 65) + `}`, errJSONTooDeep},
		{"vehicles past the fleet bound", `{"simulation":{"vehicles":` + zeros(project.MaxPods+1) + `}}`, errJSONArrayTooLong},
		{"pending past the order bound", `{"simulation":{"pending":` + zeros(maxSavedTrips+1) + `}}`, errJSONArrayTooLong},
		{"riders past the order bound", `{"simulation":{"vehicles":[{"riders":` + zeros(sim.MaxSharedRideParties+1) + `}]}}`, errJSONArrayTooLong},
		{"route past the element limit", `{"simulation":{"vehicles":[{"routeLaneIDs":` + zeros(65537) + `}]}}`, errJSONArrayTooLong},
		{"invalid UTF-8", "{\"epoch\":\"\xff\"}", nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := PrescanStateFrameJSON([]byte(test.raw)); !errors.Is(err, test.want) {
				t.Fatalf("got %v, want %v", err, test.want)
			}
		})
	}
}
