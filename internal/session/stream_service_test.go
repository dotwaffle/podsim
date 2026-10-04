package session

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

func TestStreamServiceMemberVersions(t *testing.T) {
	t.Parallel()
	for _, member := range []string{
		`"Class":null`, `"Class":""`, `"Class":"unknown"`,
		`"SharingConsent":null`, `"SharingConsent":""`, `"SharingConsent":true`,
		`"Service":"unknown"`, `"ServiceID":""`, `"projectVersion":null`,
		`"projectVersion":"3"`, `"projectVersion":3.5`, `"projectVersion":4`,
	} {
		if err := scanStreamServiceMembers([]byte(`{`+member+`}`), 3); err == nil {
			t.Errorf("hello3 accepted %s", member)
		}
	}
}

func TestStreamServiceTopologyVersions(t *testing.T) {
	t.Parallel()
	shared, _ := streamFixture(t)
	topology := shared.Topology()
	if topology.ProjectVersion != project.Default().Version {
		t.Fatalf("project version lost: %d", topology.ProjectVersion)
	}
	if _, err := NewStreamAssemblerVersion(topology, 3); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{`null`, `0`, `"3"`, `3.5`} {
		var decoded TopologySnapshot
		if err := json.Unmarshal([]byte(`{"projectVersion":`+value+`}`), &decoded); err == nil {
			t.Errorf("accepted projectVersion %s", value)
		}
	}
	classes, err := sim.NewClassSet("compact")
	if err != nil {
		t.Fatal(err)
	}
	topology.Network.Lanes[0].VehicleClasses = classes
	if _, err := NewStreamAssemblerVersion(topology, 3); err == nil {
		t.Fatal("old project accepted class metadata")
	}
	topology.ProjectVersion = project.ServiceVersion
	if _, err := NewStreamAssemblerVersion(topology, 3); err != nil {
		t.Fatal(err)
	}
}

func TestStreamServiceOrdersDelta(t *testing.T) {
	shared, frame := streamFixture(t)
	assembler, err := NewStreamAssemblerVersion(shared.Topology(), 3)
	if err != nil {
		t.Fatal(err)
	}
	rider := sim.Request{ID: 1, From: "harbor", To: "market", PartySize: 1, SharingConsent: sim.SharedConsent, Service: sim.OnDemandService}
	frame.State.Simulation.Vehicles[0].Riders = []sim.Request{rider}
	if _, err = assembler.State(frame); err != nil {
		t.Fatal(err)
	}
	before, err := json.Marshal(frame)
	if err != nil {
		t.Fatal(err)
	}
	next := frame
	next.State.Simulation.Vehicles = append([]VehicleFrame(nil), frame.State.Simulation.Vehicles...)
	next.State.Simulation.Vehicles[0].Riders = nil
	next.State.Revision++
	delta, err := makeDelta(frame, next)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ApplyStream(frame, "service", 1, StreamEnvelope{Kind: "delta", Stream: "service", Sequence: 2, Base: 1, Source: sourceOf(next), Delta: &delta})
	if err != nil || !reflect.DeepEqual(got, next) {
		t.Fatalf("rider delta mismatch: %v", err)
	}
	after, err := json.Marshal(frame)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("delta changed predecessor")
	}
	for _, request := range []sim.Request{
		{From: "harbor", To: "market", PartySize: 1},
		{From: "harbor", To: "market", PartySize: 1, SharingConsent: "legacy-unknown", Service: sim.OnDemandService},
		{From: "harbor", To: "market", PartySize: 9, SharingConsent: sim.SharedConsent, Service: sim.OnDemandService},
	} {
		bad := next
		bad.State.Simulation.Pending = []sim.Request{request}
		if _, err := assembler.State(bad); err == nil {
			t.Errorf("accepted invalid pending: %+v", request)
		}
	}
	private := rider
	private.SharingConsent = sim.PrivateConsent
	bad := next
	bad.State.Simulation.Vehicles = append([]VehicleFrame(nil), next.State.Simulation.Vehicles...)
	bad.State.Simulation.Vehicles[0].Riders = []sim.Request{private, private}
	if _, err := assembler.State(bad); err == nil {
		t.Fatal("accepted pooled private parties")
	}
}

func TestStreamProjectVersionMatchesMetadata(t *testing.T) {
	t.Parallel()
	shared, frame := streamFixture(t)
	banked := shared.Topology()
	banked.Network = sim.BankExample()
	if _, err := NewStreamAssemblerVersion(banked, 3); err == nil {
		t.Fatal("project 1 topology accepted banks")
	}
	unbanked := shared.Topology()
	unbanked.ProjectVersion = project.BankVersion
	if _, err := NewStreamAssemblerVersion(unbanked, 3); err == nil {
		t.Fatal("project 2 topology omitted its required banks")
	}
	assembler, err := NewStreamAssemblerVersion(shared.Topology(), 3)
	if err != nil {
		t.Fatal(err)
	}
	frame.State.Simulation.Vehicles[0].Pod.Class = sim.CompactClass
	if _, err := assembler.State(frame); err == nil {
		t.Fatal("project 1 frame accepted compact fleet class")
	}
}

func TestStreamClassIsImmutableWithinProject(t *testing.T) {
	t.Parallel()
	shared, frame := streamFixture(t)
	topology := shared.Topology()
	topology.ProjectVersion = project.ServiceVersion
	assembler, err := NewStreamAssemblerVersion(topology, 3)
	if err != nil {
		t.Fatal(err)
	}
	if _, stateErr := assembler.State(frame); stateErr != nil {
		t.Fatal(stateErr)
	}
	// Omitted and explicit legacy name the same immutable class.
	frame.State.Simulation.Vehicles[0].Pod.Class = sim.LegacyClass
	frame.State.Revision++
	if _, stateErr := assembler.State(frame); stateErr != nil {
		t.Fatal(stateErr)
	}
	frame.State.Simulation.Vehicles[0].Pod.Class = sim.CompactClass
	frame.State.Revision++
	if _, stateErr := assembler.State(frame); stateErr == nil {
		t.Fatal("same pod changed class inside one project")
	}
	frame.State.Simulation.Vehicles[0].Pod.Class = sim.LegacyClass
	if _, stateErr := assembler.State(frame); stateErr != nil {
		t.Fatalf("rejection poisoned class identity: %v", stateErr)
	}
	// New project topology starts a new identity scope.
	topology.ProjectRevision++
	frame.State.ProjectRevision++
	frame.State.Simulation.Vehicles[0].Pod.Class = sim.CompactClass
	other, err := NewStreamAssemblerVersion(topology, 3)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.State(frame); err != nil {
		t.Fatalf("new project could not change authored class: %v", err)
	}
}

// TestStreamRejectsLegacyOrderMembers checks that hello 3 rejects the
// removed legacy order members in full frames and metadata deltas.
func TestStreamRejectsLegacyOrderMembers(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ wrapper, control string }{
		{`{"full":{"state":{"simulation":{"Vehicles":[{%s}]}}}}`, `"Rebalancing":true`},
		{`{"full":{"state":{"simulation":{"Vehicles":[{"Riders":[{%s}]}]}}}}`, `"ID":1`},
		{`{"full":{"state":{"simulation":{"Pending":[{%s}]}}}}`, `"ID":1`},
		{`{"delta":{"vehicles":[{"metadata":{"value":{%s}}}]}}`, `"Rebalancing":true`},
	} {
		control := []byte(strings.Replace(test.wrapper, "%s", test.control, 1))
		if _, err := DecodeStreamJSONVersion(control, 3); err != nil {
			t.Fatalf("control %s: %v", control, err)
		}
		for _, name := range []string{"LegacyCohort", "LegacyPartySize"} {
			raw := []byte(strings.Replace(test.wrapper, "%s", `"`+name+`":true`, 1))
			if _, err := DecodeStreamJSONVersion(raw, 3); err == nil || !strings.Contains(err.Error(), name) {
				t.Errorf("removed member %s: %v", raw, err)
			}
		}
	}
}
