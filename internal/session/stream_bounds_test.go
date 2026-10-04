package session

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// foundationOnlyMembers are the members that families 1 and 2 reject.
var foundationOnlyMembers = []string{"class", "sharingconsent", "service", "serviceid", "legacycohort", "legacypartysize", "boardings", "riddenmeters"}

// stripStreamMembers removes the object members with the given lowercase
// names. The result is the document that an encoder without those members
// writes.
func stripStreamMembers(t *testing.T, raw []byte, names []string) []byte {
	t.Helper()
	decoder := jsontext.NewDecoder(bytes.NewReader(raw))
	var output bytes.Buffer
	encoder := jsontext.NewEncoder(&output)
	for {
		token, err := decoder.ReadToken()
		if errors.Is(err, io.EOF) {
			return output.Bytes()
		}
		if err != nil {
			t.Fatal(err)
		}
		kind, length := decoder.StackIndex(decoder.StackDepth())
		if token.Kind() == jsontext.KindString && kind == jsontext.KindBeginObject && length%2 == 1 && slices.Contains(names, strings.ToLower(token.String())) {
			if err := decoder.SkipValue(); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := encoder.WriteToken(token); err != nil {
			t.Fatal(err)
		}
	}
}

// assertUnpackedStreamMaximum shows that the bounded scan accepts an
// encoder maximum of families 1 to 3. Families 1 and 2 have no version 3
// members, so their maximum is the historical document without them.
func assertUnpackedStreamMaximum(t *testing.T, raw []byte, historical bool) {
	t.Helper()
	if raceEnabled {
		return
	}
	documents := map[int][]byte{FoundationStreamVersion: raw}
	if historical {
		stripped := stripStreamMembers(t, raw, foundationOnlyMembers)
		documents[1], documents[2] = stripped, stripped
	}
	for version, document := range documents {
		if err := prescanJSON(document, unpackedStreamLimits()); err != nil {
			t.Fatalf("version %d maximum failed the bounded scan: %v", version, err)
		}
		if _, err := DecodeStreamJSONVersion(document, version); err != nil {
			t.Fatalf("version %d maximum: %v", version, err)
		}
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
	for version := 1; version <= ExpressStreamVersion; version++ {
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
			{"vehicles past the fleet bound", `{"full":{"state":{"simulation":{"Vehicles":` + zeros(project.MaxPods+1) + `}}}}`, errJSONArrayTooLong},
			{"full pending past the order bound", `{"full":{"state":{"simulation":{"Pending":` + zeros(pending+1) + `}}}}`, errJSONArrayTooLong},
			{"folded full riders past the order bound", `{"FULL":{"State":{"SIMULATION":{"vehicles":[{"RIDERS":` + zeros(riders+1) + `}]}}}}`, errJSONArrayTooLong},
			{"delta riders past the order bound", `{"delta":{"vehicles":[{"riders":{"value":` + zeros(riders+1) + `}}]}}`, errJSONArrayTooLong},
			{"full boardings past the order bound", `{"full":{"state":{"simulation":{"Vehicles":[{"Boardings":` + zeros(riders+1) + `}]}}}}`, errJSONArrayTooLong},
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

// streamFamilyDocuments returns a full and a delta publication of each
// family before version 5.
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
	var legacy [][]byte
	for _, raw := range foundation {
		legacy = append(legacy, stripStreamMembers(t, raw, foundationOnlyMembers))
	}

	_, express := expressGuardFrame(t)
	next := ownStreamBoardings(express)
	next.State.Revision++
	next.State.Simulation.Tick++
	vehicle := &next.State.Simulation.Vehicles[0]
	vehicle.Riders, vehicle.Boardings = vehicle.Riders[1:], vehicle.Boardings[1:]
	return map[int][][]byte{1: legacy, 2: legacy, FoundationStreamVersion: foundation, ExpressStreamVersion: publications(express, next, sim.ExpressOrderContract)}
}

func TestStreamDecodeMatchesUnboundedDecoder(t *testing.T) {
	t.Parallel()
	families := streamFamilyDocuments(t)
	// The current encoder writes version 3 members, so the documents of
	// families 1 and 2 leave them out.
	if _, err := DecodeStreamJSONVersion(families[FoundationStreamVersion][1], 1); err == nil {
		t.Fatal("family 1 accepted version 3 members")
	}
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
// routes. Its widest shape passes the bounded scan. Servers before the
// topology endpoint sent the network and lane routes in the state, and
// their widest shape passes too.
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

	classes, err := sim.NewClassSet("legacy", "compact", "group", "express")
	if err != nil {
		t.Fatal(err)
	}
	state := State{Simulation: sim.Snapshot{Vehicles: make([]sim.Vehicle, project.MaxPods), Pending: make([]sim.Request, maxSavedTrips)}}
	state.Network.Nodes = make([]sim.Node, project.MaxNodes)
	state.Network.Lanes = make([]sim.Lane, project.MaxLanes)
	state.Network.Lanes[0].VehicleClasses = classes
	state.Network.Stations = make([]sim.Station, project.MaxStations)
	station := &state.Network.Stations[0]
	station.VehicleClasses = classes
	station.Berths = make([]sim.Berth, project.MaxBerths)
	station.Berths[0].VehicleClasses = classes
	station.Banks = make([]sim.StationBank, sim.MaxStationBanks)
	station.Banks[0].BerthIDs = make([]string, project.MaxBerths)
	state.Simulation.Vehicles[0].Route = make([]sim.Lane, route)
	state.Simulation.Vehicles[0].Riders = make([]sim.Request, sim.MaxSharedRideParties)
	state.Simulation.Vehicles[0].Stops = make([]string, sim.MaxSharedRideParties)
	raw, err = json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err = PrescanStateFrameJSON(append(raw, '\n')); err != nil {
		t.Fatal("widest earlier state failed the bounded scan", err)
	}
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
		{"vehicles past the fleet bound", `{"simulation":{"Vehicles":` + zeros(project.MaxPods+1) + `}}`, errJSONArrayTooLong},
		{"folded pending past the order bound", `{"SIMULATION":{"pending":` + zeros(maxSavedTrips+1) + `}}`, errJSONArrayTooLong},
		{"riders past the order bound", `{"simulation":{"Vehicles":[{"Riders":` + zeros(sim.MaxSharedRideParties+1) + `}]}}`, errJSONArrayTooLong},
		{"route past the element limit", `{"simulation":{"Vehicles":[{"RouteLaneIDs":` + zeros(65537) + `}]}}`, errJSONArrayTooLong},
		{"lanes past the network bound", `{"network":{"Lanes":` + zeros(project.MaxLanes+1) + `}}`, errJSONArrayTooLong},
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
