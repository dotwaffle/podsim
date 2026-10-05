package session

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"slices"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

// strandedSession returns expressSession, where an Express pod can stop at
// garden but cannot leave its berth there. So the Express class has a
// passenger path from harbor to market and none from garden.
func strandedSession(t *testing.T) *Session {
	t.Helper()
	config := expressConsumerProject(t)
	classes, err := sim.NewClassSet("group")
	if err != nil {
		t.Fatal(err)
	}
	for i := range config.Network.Lanes {
		if config.Network.Lanes[i].ID == "garden-1-out" {
			config.Network.Lanes[i].VehicleClasses = classes
		}
	}
	shared, err := NewWithProject(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shared.Close)
	return shared
}

// legRequest returns an active Express order from harbor to market with the
// leg origin leg.
func legRequest(leg string) sim.Request {
	return sim.Request{
		ID: 1, From: "harbor", LegFrom: leg, To: "market", PartySize: 1, SharingConsent: sim.SharedConsent,
		Service: sim.ExpressServiceChoice, ServiceID: "harbor-market",
	}
}

// TestLegOriginStreamReaders checks the stream readers of section 7.4 of
// the incident contract: the passenger path, the station references, and
// the boarding berths use the leg origin.
func TestLegOriginStreamReaders(t *testing.T) {
	t.Parallel()
	topology := strandedSession(t).Topology()
	assembler := func(t *testing.T) *StreamAssembler {
		t.Helper()
		a, err := NewStreamAssembler(topology)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	t.Run("passengerPath", func(t *testing.T) {
		t.Parallel()
		a := assembler(t)
		if a.passengerPath(legRequest("garden"), sim.ExpressClass) {
			t.Error("a path from the leg origin garden is accepted")
		}
		if !a.passengerPath(legRequest(""), sim.ExpressClass) || !a.passengerPath(legRequest("harbor"), sim.ExpressClass) {
			t.Error("the path from harbor is refused")
		}
	})
	t.Run("references", func(t *testing.T) {
		t.Parallel()
		a := assembler(t)
		frame := StreamFrame{}
		frame.State.Simulation.Pending = []sim.Request{legRequest("garden")}
		if err := a.references(frame); err != nil {
			t.Fatal(err)
		}
		frame.State.Simulation.Pending[0].LegFrom = "nowhere"
		if err := a.references(frame); err == nil {
			t.Fatal("an unknown leg origin is accepted")
		}
	})
	t.Run("vehicleBoardings", func(t *testing.T) {
		t.Parallel()
		a := assembler(t)
		rider := legRequest("garden")
		rider.PodID, rider.Completed = "01", true
		v := VehicleFrame{}
		v.Pod.ID, v.Pod.Class = "01", sim.ExpressClass
		v.Riders = []sim.Request{rider}
		v.Boardings = []sim.RiderBoarding{{BerthID: "garden-1"}}
		if err := a.vehicleBoardings(v); err != nil {
			t.Fatal(err)
		}
	})
}

// TestLegOriginBoardingTuples checks that the boarding tuples of a saved
// pod resolve the berth index against the leg origin of the rider, in
// both directions.
func TestLegOriginBoardingTuples(t *testing.T) {
	t.Parallel()
	config := expressConsumerProject(t)
	rider := sim.SavedRequest(legRequest("garden"))
	rider.PodID, rider.Completed = "01", true
	pod := sim.SavedPod{ID: "01", Class: sim.ExpressClass, Activity: "idle", StationID: "market", BerthID: "market-1", Riders: []sim.SavedRequest{rider}}
	pod.Boardings = []sim.RiderBoarding{{BerthID: "garden-1"}}
	var out bytes.Buffer
	if err := bindBoardingSource(config).encodePodContract(jsontext.NewEncoder(&out), pod, sim.ExpressOrderContract); err != nil {
		t.Fatal(err)
	}
	pod.Boardings = nil
	file := &stateFile{OrderContract: sim.ExpressOrderContract, Project: config, boardingTuples: [][]boardingTuple{{{}}}}
	file.Simulation.Pods = []sim.SavedPod{pod}
	if err := file.resolveBoardings(); err != nil {
		t.Fatal(err)
	}
	if got := file.Simulation.Pods[0].Boardings; len(got) != 1 || got[0].BerthID != "garden-1" {
		t.Fatalf("the tuple resolves to %+v", got)
	}
}

// TestLegFromHasNoWireMember checks that no stream or save order has a
// legFrom member before the format patch of the incident contract adds
// it: an encoder leaves LegFrom out, and a decoder refuses the member.
func TestLegFromHasNoWireMember(t *testing.T) {
	t.Parallel()
	stream, err := json.Marshal(legRequest("garden"), packedRequestOptions())
	if err != nil {
		t.Fatal(err)
	}
	saved, err := json.Marshal(sim.SavedRequest(legRequest("garden")), json.WithMarshalers(json.MarshalToFunc(encodePackedSavedRequest)))
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range [][]byte{stream, saved} {
		if bytes.Contains(raw, []byte("legFrom")) {
			t.Fatalf("encoded order %s has a legFrom member", raw)
		}
	}
	member := []byte(`{"legFrom":"Z2FyZGVu",`)
	var request sim.Request
	if err := json.Unmarshal(bytes.Replace(stream, []byte("{"), member, 1), &request, packedDecodeOptions()); err == nil {
		t.Error("the stream decoder accepts a legFrom member")
	}
	var savedRequest sim.SavedRequest
	if err := json.Unmarshal(bytes.Replace(saved, []byte("{"), member, 1), &savedRequest, json.WithUnmarshalers(json.UnmarshalFromFunc(decodePackedSavedRequest))); err == nil {
		t.Error("the save decoder accepts a legFrom member")
	}
}

// TestStrandedStreamOrder checks the stream exception for a stranded
// transferred order (incident contract, section 7.6): a pending Express
// order with a leg origin and no path from it is accepted only with no
// pod, known stations, its service pair, and a vehicle that admits the
// party.
func TestStrandedStreamOrder(t *testing.T) {
	t.Parallel()
	shared := strandedSession(t)
	frame, err := shared.presentationFrame()
	if err != nil {
		t.Fatal(err)
	}
	frame.State.Simulation.Pending = []sim.Request{legRequest("garden")}
	onDemand := func(r *sim.Request) { r.Service, r.ServiceID = sim.OnDemandService, "" }
	tests := []struct {
		name   string
		change func(*sim.Request)
		valid  bool
	}{
		{name: "stranded", change: func(*sim.Request) {}, valid: true},
		{name: "pod", change: func(r *sim.Request) { r.PodID = "01" }},
		{name: "unknown leg origin", change: func(r *sim.Request) { r.LegFrom = "nowhere" }},
		{name: "service pair", change: func(r *sim.Request) { r.ServiceID = "unknown" }},
		{name: "on-demand", change: onDemand, valid: true},
		{name: "on-demand to an unknown station", change: func(r *sim.Request) { onDemand(r); r.To = "nowhere" }},
		{name: "party admission", change: func(r *sim.Request) { r.PartySize = 21 }},
	}
	newAssembler := func(t *testing.T) *StreamAssembler {
		t.Helper()
		a, err := NewStreamAssembler(shared.Topology())
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			a := newAssembler(t)
			candidate := ownStreamBoardings(frame)
			test.change(&candidate.State.Simulation.Pending[0])
			if err := a.expressOrders(candidate); (err == nil) != test.valid {
				t.Fatalf("expressOrders: %v", err)
			}
			if _, err := a.State(candidate); (err == nil) != test.valid {
				t.Fatalf("State: %v", err)
			}
			// A Group pod cannot take an Express order. A new assembler has
			// no earlier class of the pod.
			candidate.State.Simulation.Vehicles = slices.Clone(candidate.State.Simulation.Vehicles)
			candidate.State.Simulation.Vehicles[0].Pod.Class = sim.GroupClass
			if err := newAssembler(t).expressOrders(candidate); err == nil && candidate.State.Simulation.Pending[0].Service == sim.ExpressServiceChoice {
				t.Fatal("expressOrders accepts the order without an Express pod")
			}
		})
	}
}
