package session

import (
	"bytes"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

func expressGuardFrame(t *testing.T) (TopologySnapshot, StreamFrame) {
	t.Helper()
	shared := expressSession(t)
	frame, err := shared.presentationFrame()
	if err != nil {
		t.Fatal(err)
	}
	request := sim.Request{ID: 1, From: "harbor", To: "market", PartySize: 20, SharingConsent: sim.SharedConsent, Service: sim.ExpressServiceChoice, ServiceID: "harbor-market", Completed: true}
	vehicle := &frame.State.Simulation.Vehicles[0]
	for i := range 20 {
		r := request
		r.ID = i + 1
		vehicle.Riders = append(vehicle.Riders, r)
		vehicle.Boardings = append(vehicle.Boardings, sim.RiderBoarding{BerthID: vehicle.Pod.BerthID})
	}
	return shared.Topology(), frame
}

func TestExpressPublicOrderGuards(t *testing.T) {
	topology, frame := expressGuardFrame(t)
	tests := []struct {
		name   string
		mutate func(*StreamFrame)
	}{
		{"stored-21", func(f *StreamFrame) {
			v := &f.State.Simulation.Vehicles[0]
			v.Riders = append(v.Riders, v.Riders[0])
			v.Boardings = append(v.Boardings, v.Boardings[0])
		}},
		{"whole-party-seats", func(f *StreamFrame) {
			v := &f.State.Simulation.Vehicles[0]
			v.Riders[0].Completed = false
			v.Riders[0].PartySize = 12
			v.Riders[1].Completed = false
			v.Riders[1].PartySize = 9
		}},
		{"private-mixed", func(f *StreamFrame) {
			v := &f.State.Simulation.Vehicles[0]
			v.Boardings = nil
			v.Riders[0].Completed = false
			v.Riders[0].PartySize = 12
			v.Riders[0].SharingConsent = sim.PrivateConsent
			v.Riders[1].Completed = false
			v.Riders[1].PartySize = 8
		}},
		{"registry-pair", func(f *StreamFrame) { f.State.Simulation.Vehicles[0].Riders[0].To = "harbor" }},
		{"registry-id", func(f *StreamFrame) { f.State.Simulation.Vehicles[0].Riders[0].ServiceID = "unknown" }},
		{"boarding-source", func(f *StreamFrame) {
			f.State.Simulation.Vehicles[0].Boardings[0].BerthID = topology.Network.Stations[1].Berths[0].ID
		}},
		{"large-virtual", func(f *StreamFrame) { f.State.Simulation.Vehicles[0].PlatoonIndex = 1 }},
		{"outstanding-8601", func(f *StreamFrame) {
			v := &f.State.Simulation.Vehicles[0]
			r := v.Riders[0]
			r.Completed = false
			r.PartySize = 1
			f.State.Simulation.Pending = slices.Repeat([]sim.Request{r}, sim.MaxExpressWaitingTrips)
			v.Riders[0] = r
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assembler, err := NewStreamAssemblerVersion(topology, 4)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = assembler.State(frame); err != nil {
				t.Fatal("valid twenty-record seed", err)
			}
			before := assembler.previous
			bad := ownStreamBoardings(frame)
			test.mutate(&bad)
			envelope := StreamEnvelope{OrderContract: sim.ExpressOrderContract, TextEncoding: ExpressTextEncoding, Kind: "full", Stream: "guard", Sequence: 1, Source: sourceOf(bad), Full: &bad}
			raw, err := EncodeStreamJSON(envelope)
			if err != nil {
				t.Fatal(err)
			}
			decoded, decodeErr := DecodeStreamJSONVersion(raw, 4)
			rejected := decodeErr != nil
			if !rejected {
				candidate, applyErr := ApplyStream(StreamFrame{}, "", 0, decoded)
				rejected = applyErr != nil
				if !rejected {
					_, stateErr := assembler.State(candidate)
					rejected = stateErr != nil
				}
			}
			if !rejected {
				t.Fatal("accepted invalid candidate")
			}
			if !reflect.DeepEqual(before, assembler.previous) {
				t.Fatal("rejection mutated prior accepted frame")
			}
		})
	}
}

func TestExpressPublicTextAndShapeGuards(t *testing.T) {
	shared := expressSession(t)
	frame, err := shared.presentationFrame()
	if err != nil {
		t.Fatal(err)
	}
	frame.State.Simulation.Pending = []sim.Request{{ID: math.MaxInt, From: "harbor", To: "market", PartySize: 20, SharingConsent: sim.SharedConsent, Service: sim.ExpressServiceChoice, ServiceID: "harbor-market"}}
	envelope := StreamEnvelope{OrderContract: sim.ExpressOrderContract, TextEncoding: ExpressTextEncoding, Kind: "full", Stream: "guard", Sequence: 1, Source: sourceOf(frame), Full: &frame}
	raw, err := EncodeStreamJSON(envelope)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct{ name, old, new string }{
		{"escaped-canonical", "\"From\":\"aGFyYm9y\"", "\"From\":\"\\u0061GFyYm9y\""},
		{"raw-fallback", "\"From\":\"aGFyYm9y\"", "\"From\":\"harbor\""},
		{"decoded-id-65", "\"From\":\"aGFyYm9y\"", "\"From\":\"" + strings.Repeat("eHh4", 21) + "eHg=\""},
		{"duplicate-field", "\"From\":\"aGFyYm9y\"", "\"From\":\"aGFyYm9y\",\"From\":\"aGFyYm9y\""},
		{"duplicate-alias", "\"From\":\"aGFyYm9y\"", "\"From\":\"aGFyYm9y\",\"from\":\"bWFya2V0\""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mutated := bytes.Replace(raw, []byte(test.old), []byte(test.new), 1)
			if bytes.Equal(mutated, raw) {
				t.Fatal("mutation missed field")
			}
			if _, decodeErr := DecodeStreamJSONVersion(mutated, 4); decodeErr == nil {
				t.Fatal("accepted invalid packed field")
			}
		})
	}
	frame.State.Simulation.Pending = slices.Repeat(frame.State.Simulation.Pending, sim.MaxExpressWaitingTrips+1)
	envelope.Full = &frame
	raw, err = EncodeStreamJSON(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if _, decodeErr := DecodeStreamJSONVersion(raw, 4); decodeErr == nil {
		t.Fatal("accepted 8601 pending records")
	}
}

func TestExpressPublicHTTPBoardingPresence(t *testing.T) {
	topology, frame := expressGuardFrame(t)
	raw, err := EncodeExpressStateJSON(topology, frame)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"null", "[]", "{}"} {
		mutated := slices.Clone(raw)
		// Locate the array without parsing the numeric values through another adapter.
		start := bytes.Index(mutated, []byte(`"Boardings":`))
		if start < 0 {
			t.Fatal("missing boarding field")
		}
		start += len(`"Boardings":`)
		end := start + bytes.IndexByte(mutated[start:], ']') + 1
		if end <= start {
			t.Fatal("missing boarding array")
		}
		mutated = append(append(slices.Clone(mutated[:start]), value...), mutated[end:]...)
		if _, decodeErr := DecodeExpressStateJSON(mutated); decodeErr == nil {
			t.Fatal("accepted boarding shape", value)
		}
	}
}

func TestExpressPublisherRequiresNewHello(t *testing.T) {
	p := statePublisher{sequence: 1, frame: StreamFrame{State: StateFrame{Simulation: SimulationFrame{OrderContract: sim.ExpressOrderContract}}}}
	subscriber := streamSubscriber{}
	if err := p.sendAvailable(t.Context(), &subscriber); err == nil {
		t.Fatal("foundation negotiation crossed Express boundary")
	}
	if subscriber.bytes != 0 || len(subscriber.sent) != 0 {
		t.Fatal("rejected publication acquired credit")
	}
}

func TestExpressPublicClassBindings(t *testing.T) {
	topology, frame := expressGuardFrame(t)
	group, err := sim.NewClassSet("group")
	if err != nil {
		t.Fatal(err)
	}
	topology.Network.Stations = append(slices.Clone(topology.Network.Stations), sim.Station{ID: "group-only", VehicleClasses: group})
	topology.Network.Lanes = append(slices.Clone(topology.Network.Lanes), sim.Lane{ID: "group-only-link", From: topology.Network.Nodes[0].ID, To: topology.Network.Nodes[1].ID, VehicleClasses: group})
	for _, binding := range []string{"station", "route"} {
		t.Run(binding, func(t *testing.T) {
			assembler, createErr := NewStreamAssemblerVersion(topology, 4)
			if createErr != nil {
				t.Fatal(createErr)
			}
			if _, stateErr := assembler.State(frame); stateErr != nil {
				t.Fatal(stateErr)
			}
			before := assembler.previous
			bad := ownStreamBoardings(frame)
			if binding == "station" {
				bad.State.Simulation.Vehicles[0].Pod.ManeuverStationID = "group-only"
			} else {
				bad.Routes = slices.Clone(bad.Routes)
				index := len(topology.Network.Lanes) - 1
				bad.Routes[0] = sim.RoutePresentation{Origin: 0, Display: []int{index}, Lanes: []int{index}}
			}
			if _, stateErr := assembler.State(bad); stateErr == nil {
				t.Fatal("accepted class-incompatible binding")
			}
			if !reflect.DeepEqual(before, assembler.previous) {
				t.Fatal("class rejection changed prior frame")
			}
		})
	}
}

func TestExpressNegotiatedVersions(t *testing.T) {
	topology, _ := expressGuardFrame(t)
	if _, err := NewStreamAssemblerVersion(topology, FoundationStreamVersion); err == nil {
		t.Fatal("foundation negotiation accepted Express topology")
	}
	for _, raw := range []string{
		`{"kind":"hello","version":4,"serverStart":"source"}`,
		`{"kind":"hello","version":3,"serverStart":"source","orderContract":null}`,
		`{"kind":"hello","version":3,"serverStart":"source","textEncoding":""}`,
		`{"kind":"hello","version":4,"serverStart":"source","orderContract":"express-v1","textEncoding":"order-text-base64-v1","TextEncoding":"order-text-base64-v1"}`,
	} {
		if _, err := DecodeStreamHello([]byte(raw)); err == nil {
			t.Fatal("accepted contradictory hello", raw)
		}
	}
}
