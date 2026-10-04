package session

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

func expressConsumerProject(t *testing.T) project.Config {
	t.Helper()
	config := groupConsumerProject(t)
	config.OrderContract = sim.ExpressOrderContract
	classes, err := sim.NewClassSet("group", "express")
	if err != nil {
		t.Fatal(err)
	}
	for i := range config.Network.Lanes {
		config.Network.Lanes[i].VehicleClasses = classes
	}
	for i := range config.Network.Stations {
		config.Network.Stations[i].VehicleClasses = classes
		for j := range config.Network.Stations[i].Berths {
			config.Network.Stations[i].Berths[j].VehicleClasses = classes
		}
	}
	config.Fleet[0].Class = sim.ExpressClass
	config.ExpressServices = []sim.ExpressService{{ID: "harbor-market", From: "harbor", To: "market", Class: sim.ExpressClass, PartyLimit: 20}}
	return config
}

func expressSession(t *testing.T) *Session {
	t.Helper()
	shared, err := NewWithProject(expressConsumerProject(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shared.Close)
	return shared
}

func TestExpressSaveStreamHTTPRoundTrip(t *testing.T) {
	shared := expressSession(t)
	shared.advance()
	command := Command{Client: "express-test", Sequence: 1, Epoch: shared.State().Epoch, Action: "trip", Origin: "harbor", Destination: "market", PartySize: 20, SharingConsent: sim.SharedConsent, Service: sim.ExpressServiceChoice, ServiceID: "harbor-market", OrderContract: sim.ExpressOrderContract}
	reply := shared.Apply(command)
	if reply.ErrorCode != "" {
		t.Fatal(reply)
	}
	assembler, err := NewStreamAssemblerVersion(shared.Topology(), ExpressStreamVersion)
	if err != nil {
		t.Fatal(err)
	}
	sawBoarding := false
	for range 120 * sim.TicksPerSecond {
		shared.advance()
		frame, err := shared.presentationFrame()
		if err != nil {
			t.Fatal(err)
		}
		native, err := assembler.State(frame)
		if err != nil {
			t.Fatal(err)
		}
		if native.Simulation.OrderContract != sim.ExpressOrderContract {
			t.Fatal("lost marker")
		}
		vehicle := frame.State.Simulation.Vehicles[0]
		if len(vehicle.Riders) == 0 || vehicle.Pod.Activity != sim.Boarding {
			continue
		}
		sawBoarding = true
		file := sessionStateFile(t, shared)
		file.Version, file.OrderContract, file.TextEncoding = expressStateVersion, sim.ExpressOrderContract, ExpressTextEncoding
		file.RestoreAttempts = 0
		data := encodeTestState(t, file)
		decoded, err := decodeCheckedState(data)
		if err != nil {
			t.Fatal(err)
		}
		if err = decoded.resolveBoardings(); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(file.Simulation, decoded.Simulation) {
			t.Fatal("saved native facts changed")
		}
		loaded, err := shared.loadState(loadInput{data: data, project: &file.Project, steps: realRestoreSteps()})
		if err != nil || loaded.result.Tier != sim.RestorePhysical {
			t.Fatalf("physical restore: %v %+v", err, loaded.result)
		}
		e := StreamEnvelope{OrderContract: sim.ExpressOrderContract, TextEncoding: ExpressTextEncoding, Kind: "full", Stream: "test", Sequence: 1, Source: sourceOf(frame), Build: frame.State.Build, Full: &frame}
		raw, err := EncodeStreamJSON(e)
		if err != nil {
			t.Fatal(err)
		}
		got, err := DecodeStreamJSONVersion(raw, 4)
		if err != nil {
			t.Fatal(err)
		}
		accepted, err := ApplyStream(StreamFrame{}, "", 0, got)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(frame, accepted) {
			t.Fatal("stream semantic round trip changed")
		}
		httpRaw, err := EncodeExpressStateJSON(shared.Topology(), frame)
		if err != nil {
			t.Fatal(err)
		}
		state, err := DecodeExpressStateJSON(httpRaw)
		if err != nil || state.Simulation.Vehicles[0].Riders[0].PartySize != 20 {
			t.Fatalf("HTTP: %v", err)
		}
		if dir := os.Getenv("PODSIM_EXPRESS_SAMPLE_DIR"); dir != "" {
			for name, value := range map[string][]byte{"full.json": raw, "http.json": httpRaw, "save.json.gz": data} {
				if err := os.WriteFile(dir+"/"+name, value, 0o600); err != nil {
					t.Fatal(err)
				}
			}
		}
		break
	}
	if !sawBoarding {
		t.Fatal("Express party never boarded")
	}
}

func TestExpressMarkersAndAtomicAssembly(t *testing.T) {
	shared := expressSession(t)
	frame, err := shared.presentationFrame()
	if err != nil {
		t.Fatal(err)
	}
	e := StreamEnvelope{OrderContract: sim.ExpressOrderContract, TextEncoding: ExpressTextEncoding, Kind: "full", Stream: "test", Sequence: math.MaxUint64, Source: sourceOf(frame), Full: &frame}
	raw, err := EncodeStreamJSON(e)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func([]byte) []byte{
		func(b []byte) []byte { return bytes.Replace(b, []byte(`"orderContract":"express-v1",`), nil, 1) },
		func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"textEncoding":"order-text-base64-v1"`), []byte(`"textEncoding":null`), 1)
		},
		func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"orderContract":"express-v1"`), []byte(`"orderContract":"other"`), 1)
		},
		func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"textEncoding":"order-text-base64-v1"`), []byte(`"textEncoding":"order-text-base64-v1","textEncoding":"order-text-base64-v1"`), 1)
		},
	} {
		if _, decodeErr := DecodeStreamJSONVersion(mutate(raw), 4); decodeErr == nil {
			t.Fatal("accepted bad markers")
		}
	}
	if _, decodeErr := DecodeStreamJSONVersion(raw, FoundationStreamVersion); decodeErr == nil {
		t.Fatal("foundation family accepted Express")
	}
	assembler, err := NewStreamAssemblerVersion(shared.Topology(), 4)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = assembler.State(frame); err != nil {
		t.Fatal(err)
	}
	before := assembler.previous
	bad := ownStreamBoardings(frame)
	bad.State.Simulation.Vehicles[0].Pod.Class = sim.LegacyClass
	if _, err = assembler.State(bad); err == nil {
		t.Fatal("accepted changed class")
	}
	if !reflect.DeepEqual(before, assembler.previous) {
		t.Fatal("invalid candidate mutated prior frame")
	}
	if _, err = assembler.State(frame); err != nil {
		t.Fatal(err)
	}
}

func TestExpressHTTPNegotiationAndTripMarker(t *testing.T) {
	shared := expressSession(t)
	handler := shared.Handler("missing")
	for _, accept := range []string{"", ExpressMediaType} {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/state", http.NoBody)
		r.Header.Set("Accept", accept)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if accept == "" {
			if w.Code != http.StatusNotAcceptable {
				t.Fatal(w.Code)
			}
		} else {
			if w.Code != 200 || w.Header().Get("Content-Type") != ExpressMediaType {
				t.Fatal(w.Code, w.Body.String())
			}
			if _, err := DecodeExpressStateJSON(w.Body.Bytes()); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, marker := range []string{``, `,"orderContract":"express-v1"`, `,"orderContract":null`, `,"orderContract":"other"`} {
		raw := `{"action":"trip","origin":"harbor","destination":"market","partySize":20` + marker + `}`
		var command Command
		err := json.Unmarshal([]byte(raw), &command)
		if (err == nil) != (marker == `,"orderContract":"express-v1"`) {
			t.Fatal(raw, err)
		}
	}
	foundation, _ := NewWithProject(project.Default())
	t.Cleanup(foundation.Close)
	w := httptest.NewRecorder()
	foundation.Handler("missing").ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/state", http.NoBody))
	if w.Code != 200 || strings.Contains(w.Body.String(), "orderContract") {
		t.Fatal("foundation HTTP changed")
	}
}

// This public adapter test also runs in Go/WASM without browser APIs or HTTP.
func TestExpressPublicNumericRoundTrip(t *testing.T) {
	shared := expressSession(t)
	frame, err := shared.presentationFrame()
	if err != nil {
		t.Fatal(err)
	}
	request := sim.Request{ID: 9007199254740993, From: "harbor", To: "market", PartySize: 20, SharingConsent: sim.SharedConsent, Service: sim.ExpressServiceChoice, ServiceID: "harbor-market", RequestedTick: 9007199254740995, DispatchReason: strings.Repeat("\x01", 1000) + "é中\"\\"}
	frame.State.Simulation.Pending = []sim.Request{request}
	frame.State.Simulation.Tick = 9007199254740997
	frame.State.Simulation.PassengerDistanceMeters = 0.0000010000000000000002
	frame.State.Simulation.EmptyDistanceMeters = math.MaxFloat64
	frame.State.Simulation.DirectDistanceMeters = math.SmallestNonzeroFloat64
	e := StreamEnvelope{OrderContract: sim.ExpressOrderContract, TextEncoding: ExpressTextEncoding, Kind: "full", Stream: "numeric", Sequence: math.MaxUint64, Source: sourceOf(frame), Build: frame.State.Build, Full: &frame}
	raw, err := EncodeStreamJSON(e)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeStreamJSONVersion(raw, 4)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := ApplyStream(StreamFrame{}, "", 0, decoded)
	if err != nil {
		t.Fatal(err)
	}
	assembler, err := NewStreamAssemblerVersion(shared.Topology(), 4)
	if err != nil {
		t.Fatal(err)
	}
	native, err := assembler.State(candidate)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(native.Simulation.Pending, []sim.Request{request}) || native.Simulation.Tick != frame.State.Simulation.Tick || native.Simulation.EmptyDistanceMeters != math.MaxFloat64 || native.Simulation.PassengerDistanceMeters != frame.State.Simulation.PassengerDistanceMeters || native.Simulation.DirectDistanceMeters != math.SmallestNonzeroFloat64 {
		t.Fatal("public stream numeric or text precision changed")
	}
	httpRaw, err := EncodeExpressStateJSON(shared.Topology(), frame)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := DecodeExpressStateJSON(httpRaw)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restored.Simulation.Pending, native.Simulation.Pending) || restored.Simulation.Tick != native.Simulation.Tick || restored.Simulation.EmptyDistanceMeters != native.Simulation.EmptyDistanceMeters || restored.Simulation.DirectDistanceMeters != native.Simulation.DirectDistanceMeters {
		t.Fatal("public HTTP numeric precision changed")
	}
}

// The external assets contain independent bounded wire fields, not native motion.
// Retain the first accepted state while decoding its successor in native or WASM.
func TestExpressPublicAssetRetention(t *testing.T) {
	dir := os.Getenv("PODSIM_EXPRESS_PUBLIC_ASSET_DIR")
	if dir == "" {
		t.Skip("external widest reference-shape assets are not requested")
	}
	topologyBytes, err := os.ReadFile(dir + "/topology.json")
	if err != nil {
		t.Fatal(err)
	}
	var topology TopologySnapshot
	if err = json.Unmarshal(topologyBytes, &topology); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(dir + "/reference-full.json")
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeStreamJSONVersion(raw, 4)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := ApplyStream(StreamFrame{}, "", 0, decoded)
	if err != nil {
		t.Fatal(err)
	}
	assembler, err := NewStreamAssemblerVersion(topology, 4)
	if err != nil {
		t.Fatal(err)
	}
	first, err := assembler.State(candidate)
	if err != nil {
		t.Fatal(err)
	}
	nativeRequest := first.Simulation.Pending[0]
	successor := expressReferenceSuccessor(candidate)
	delta, err := makeDelta(candidate, successor)
	if err != nil {
		t.Fatal(err)
	}
	envelope := StreamEnvelope{OrderContract: sim.ExpressOrderContract, TextEncoding: ExpressTextEncoding, Kind: "delta", Stream: decoded.Stream, Sequence: decoded.Sequence + 1, Base: decoded.Sequence, Source: sourceOf(successor), Build: successor.State.Build, Delta: &delta}
	deltaRaw, err := EncodeStreamJSON(envelope)
	if err != nil {
		t.Fatal(err)
	}
	deltaDecoded, err := DecodeStreamJSONVersion(deltaRaw, 4)
	if err != nil {
		t.Fatal(err)
	}
	applied, err := ApplyStream(candidate, decoded.Stream, decoded.Sequence, deltaDecoded)
	if err != nil {
		t.Fatal(err)
	}
	second, err := assembler.State(applied)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Simulation.Vehicles) != 300 || len(second.Simulation.Pending) != 8599 || !reflect.DeepEqual(first.Simulation.Pending[0], nativeRequest) || first.Simulation.Vehicles[0].RiddenMeters != candidate.State.Simulation.Vehicles[0].RiddenMeters {
		t.Fatal("replacement delta changed retained predecessor")
	}
	beforePending := assembler.previous.State.Simulation.Pending[0]
	beforeBoarding := assembler.previous.State.Simulation.Vehicles[0].Boardings[0]
	second.Simulation.Pending[0].From = "mutated"
	second.Simulation.Vehicles[0].Riders[0].PartySize = 999
	second.Simulation.Vehicles[0].Boardings[0].MetersAtBoarding = 999
	second.Simulation.Vehicles[0].Pod.Class = sim.LegacyClass
	if !reflect.DeepEqual(assembler.previous.State.Simulation.Pending[0], beforePending) || assembler.previous.State.Simulation.Vehicles[0].Boardings[0] != beforeBoarding || !reflect.DeepEqual(first.Simulation.Pending[0], nativeRequest) || first.Simulation.Vehicles[0].Riders[0].PartySize == 999 {
		t.Fatal("returned owned containers alias accepted state")
	}
	if _, stateErr := assembler.State(applied); stateErr != nil {
		t.Fatal("returned mutation changed assembler", stateErr)
	}
	httpRaw, err := os.ReadFile(dir + "/http.json")
	if err != nil {
		t.Fatal(err)
	}
	httpState, err := DecodeExpressStateJSON(httpRaw)
	if err != nil {
		t.Fatal(err)
	}
	if len(httpState.Simulation.Vehicles) != 300 || !reflect.DeepEqual(first.Simulation.Pending[0], nativeRequest) {
		t.Fatal("widest HTTP decode changed retained stream")
	}
	t.Logf("public assets full=%d http=%d vehicles=%d pending=%d retained=true", len(raw), len(httpRaw), len(second.Simulation.Vehicles), len(second.Simulation.Pending))
}

func expressReferenceSuccessor(frame StreamFrame) StreamFrame {
	next := ownStreamBoardings(frame)
	next.State.Revision++
	next.State.Simulation.Tick++
	picked := next.State.Simulation.Pending[0]
	next.State.Simulation.Pending = slices.Clone(next.State.Simulation.Pending[1:])
	next.State.Simulation.Pending[0].DispatchReason = strings.Repeat("\x01", 1019) + "after"
	vehicle := &next.State.Simulation.Vehicles[0]
	picked.PodID = vehicle.Pod.ID
	picked.Completed = false
	vehicle.RiddenMeters *= 2
	vehicle.Riders = append(slices.Clone(vehicle.Riders[1:]), picked)
	vehicle.Boardings = append(slices.Clone(vehicle.Boardings[1:]), sim.RiderBoarding{BerthID: vehicle.Boardings[0].BerthID, MetersAtBoarding: vehicle.RiddenMeters})
	return next
}
