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

func groupStreamFixture(t *testing.T) (TopologySnapshot, StreamFrame) {
	t.Helper()
	shared, frame := streamFixture(t)
	t.Cleanup(shared.Close)
	topology := shared.Topology()
	topology.ProjectVersion = project.ServiceVersion
	classes, err := sim.NewClassSet("legacy", "group")
	if err != nil {
		t.Fatal(err)
	}
	for i := range topology.Network.Lanes {
		topology.Network.Lanes[i].VehicleClasses = classes
	}
	for i := range topology.Network.Stations {
		topology.Network.Stations[i].VehicleClasses = classes
		for j := range topology.Network.Stations[i].Berths {
			topology.Network.Stations[i].Berths[j].VehicleClasses = classes
		}
	}
	frame.State.Simulation.Vehicles[0].Pod.Class = sim.GroupClass
	return topology, frame
}

func TestGroupStreamSourceBindings(t *testing.T) {
	for _, test := range []struct {
		name    string
		mutate  func(*TopologySnapshot, *StreamFrame)
		invalid bool
	}{
		{"compatible", func(*TopologySnapshot, *StreamFrame) {}, false},
		{"station default", func(topology *TopologySnapshot, frame *StreamFrame) {
			for i := range topology.Network.Stations {
				if topology.Network.Stations[i].ID == frame.State.Simulation.Vehicles[0].Pod.StationID {
					topology.Network.Stations[i].VehicleClasses = 0
				}
			}
		}, true},
		{"berth default", func(topology *TopologySnapshot, frame *StreamFrame) {
			for i := range topology.Network.Stations {
				for j := range topology.Network.Stations[i].Berths {
					if topology.Network.Stations[i].Berths[j].ID == frame.State.Simulation.Vehicles[0].Pod.BerthID {
						topology.Network.Stations[i].Berths[j].VehicleClasses = 0
					}
				}
			}
		}, true},
		{"foreign berth", func(topology *TopologySnapshot, frame *StreamFrame) {
			frame.State.Simulation.Vehicles[0].Pod.BerthID = topology.Network.Stations[1].Berths[0].ID
		}, true},
		{"berthless station", func(_ *TopologySnapshot, frame *StreamFrame) { frame.State.Simulation.Vehicles[0].Pod.BerthID = "" }, true},
		{"default lane", func(topology *TopologySnapshot, frame *StreamFrame) {
			pod := &frame.State.Simulation.Vehicles[0].Pod
			pod.StationID, pod.BerthID = "", ""
			pod.LaneID = topology.Network.Lanes[0].ID
			topology.Network.Lanes[0].VehicleClasses = 0
		}, true},
		{"route lane default", func(topology *TopologySnapshot, frame *StreamFrame) {
			frame.Routes[0].Display = []int{0}
			topology.Network.Lanes[0].VehicleClasses = 0
		}, true},
		{"motion lane default", func(topology *TopologySnapshot, frame *StreamFrame) {
			frame.Routes[0].Lanes = []int{0}
			topology.Network.Lanes[0].VehicleClasses = 0
		}, true},
		{"lane station default", func(topology *TopologySnapshot, frame *StreamFrame) {
			topology.Network.Lanes[0].StationID = topology.Network.Stations[1].ID
			topology.Network.Stations[1].VehicleClasses = 0
			frame.Routes[0].Display = []int{0}
		}, true},
		{"group index without leader", func(_ *TopologySnapshot, frame *StreamFrame) { frame.State.Simulation.Vehicles[0].PlatoonIndex = 1 }, true},
		{"group follower", func(_ *TopologySnapshot, frame *StreamFrame) {
			frame.State.Simulation.Vehicles[0].PlatoonID = frame.State.Simulation.Vehicles[1].Pod.ID
		}, true},
		{"group leader", func(_ *TopologySnapshot, frame *StreamFrame) {
			frame.State.Simulation.Vehicles[1].PlatoonID = frame.State.Simulation.Vehicles[0].Pod.ID
		}, true},
		{"compact group link", func(_ *TopologySnapshot, frame *StreamFrame) {
			frame.State.Simulation.Vehicles[1].Pod.Class = sim.CompactClass
			frame.State.Simulation.Vehicles[1].PlatoonID = frame.State.Simulation.Vehicles[0].Pod.ID
		}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			topology, frame := groupStreamFixture(t)
			test.mutate(&topology, &frame)
			assembler, err := NewStreamAssemblerVersion(topology, 3)
			if err != nil {
				t.Fatal(err)
			}
			if err := assembler.references(frame); (err != nil) != test.invalid {
				t.Fatalf("references error = %v", err)
			}
			if assembler.classes != nil || !reflect.DeepEqual(assembler.previous, StreamFrame{}) {
				t.Fatal("reference check retained candidate")
			}
		})
	}
}

func TestGroupStreamRetentionRollback(t *testing.T) {
	shared, err := NewWithProject(groupConsumerProject(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shared.Close)
	topology := shared.Topology()
	frame, err := shared.presentationFrame()
	if err != nil {
		t.Fatal(err)
	}
	assembler, err := NewStreamAssemblerVersion(topology, 3)
	if err != nil {
		t.Fatal(err)
	}
	if _, stateErr := assembler.State(frame); stateErr != nil {
		t.Fatal("compatible group rejected", stateErr)
	}
	before := streamJSON(t, assembler.previous)
	bad := ownStreamBoardings(frame)
	bad.State.Simulation.Vehicles[0].Pod.BerthID = topology.Network.Stations[1].Berths[0].ID
	if _, stateErr := assembler.State(bad); stateErr == nil {
		t.Fatal("foreign group berth accepted")
	}
	if !bytes.Equal(before, streamJSON(t, assembler.previous)) {
		t.Fatal("rejected candidate replaced retained frame")
	}
	if assembler.classes[frame.State.Simulation.Vehicles[0].Pod.ID] != sim.GroupClass {
		t.Fatal("rejected candidate changed class binding")
	}
	if _, stateErr := assembler.State(frame); stateErr != nil {
		t.Fatal("rollback lost compatible predecessor", stateErr)
	}
	fresh, err := NewStreamAssemblerVersion(topology, 3)
	if err != nil {
		t.Fatal(err)
	}
	if _, stateErr := fresh.State(bad); stateErr == nil {
		t.Fatal("fresh assembler accepted foreign berth")
	}
	if len(fresh.classes) != 0 {
		t.Fatal("rejected first state retained classes")
	}
	ordinary := ownStreamBoardings(frame)
	ordinary.State.Simulation.Vehicles[0].Pod.Class = sim.LegacyClass
	if _, stateErr := fresh.State(ordinary); stateErr != nil {
		t.Fatal("rejected state poisoned class admission", stateErr)
	}
}

func TestGroupStreamActualFullAndDelta(t *testing.T) {
	shared, err := NewWithProject(groupConsumerProject(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shared.Close)
	topology := shared.Topology()
	frame, err := shared.presentationFrame()
	if err != nil {
		t.Fatal(err)
	}
	for _, recorded := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "recorded"}[recorded], func(t *testing.T) {
			candidate := ownStreamBoardings(frame)
			vehicle := &candidate.State.Simulation.Vehicles[0]
			vehicle.Riders = []sim.Request{{ID: 1, From: "harbor", To: "market", PodID: vehicle.Pod.ID, PartySize: 8, SharingConsent: sim.SharedConsent, Service: sim.OnDemandService}}
			if recorded {
				vehicle.Boardings = []sim.RiderBoarding{{BerthID: "harbor-1", MetersAtBoarding: 0}}
				vehicle.RiddenMeters = 10
			}
			assembler, err := NewStreamAssemblerVersion(topology, 3)
			if err != nil {
				t.Fatal(err)
			}
			full := StreamEnvelope{Kind: "full", Stream: "group", Sequence: 1, Source: sourceOf(candidate), Full: &candidate}
			encoded, err := encodeStream(full)
			if err != nil {
				t.Fatal(err)
			}
			inflated, err := InflateStream(encoded)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := DecodeStreamJSONVersion(inflated, 3)
			if err != nil {
				t.Fatal(err)
			}
			accepted, err := ApplyStream(StreamFrame{}, "", 0, decoded)
			if err != nil {
				t.Fatal(err)
			}
			if _, stateErr := assembler.State(accepted); stateErr != nil {
				t.Fatal("group full rejected", stateErr)
			}
			bad := ownStreamBoardings(candidate)
			bad.State.Revision++
			bad.State.Simulation.Vehicles[0].Pod.BerthID = "garden-1"
			delta, err := makeDelta(candidate, bad)
			if err != nil {
				t.Fatal(err)
			}
			envelope := StreamEnvelope{Kind: "delta", Stream: "group", Sequence: 2, Base: 1, Source: sourceOf(bad), Delta: &delta}
			reconstructed, err := ApplyStream(candidate, "group", 1, envelope)
			if err != nil {
				t.Fatal(err)
			}
			if _, stateErr := assembler.State(reconstructed); stateErr == nil {
				t.Fatal("foreign group berth delta accepted")
			}
			repaired := ownStreamBoardings(candidate)
			repaired.State.Revision++
			repaired.State.Simulation.Vehicles[0].Riders[0].PartySize = 9
			repaired.State.Simulation.Vehicles[0].Riders[0].LegacyPartySize = true
			if _, stateErr := assembler.State(repaired); stateErr == nil {
				t.Fatal("group party limit widened")
			}
			repaired.State.Simulation.Vehicles[0].Riders[0].PartySize = 8
			repaired.State.Simulation.Vehicles[0].Riders[0].LegacyPartySize = false
			pooled := ownStreamBoardings(repaired)
			pooled.State.Simulation.Vehicles[0].Riders[0].PartySize = 5
			pooled.State.Simulation.Vehicles[0].Riders = append(pooled.State.Simulation.Vehicles[0].Riders, sim.Request{ID: 2, From: "harbor", To: "market", PartySize: 4, SharingConsent: sim.SharedConsent, Service: sim.OnDemandService})
			if recorded {
				pooled.State.Simulation.Vehicles[0].Boardings = append(pooled.State.Simulation.Vehicles[0].Boardings, sim.RiderBoarding{BerthID: "harbor-1"})
			}
			if _, stateErr := assembler.State(pooled); stateErr == nil {
				t.Fatal("group aggregate seat limit widened")
			}
			if _, stateErr := assembler.State(repaired); stateErr != nil {
				t.Fatal("rejected delta poisoned class or predecessor", stateErr)
			}
		})
	}
}

func TestGroupStreamMaximumEncoding(t *testing.T) {
	frame := maximumStreamFrame(t)
	for i := range frame.State.Simulation.Vehicles {
		vehicle := &frame.State.Simulation.Vehicles[i]
		vehicle.Pod.Class, vehicle.LegacyCohort = sim.GroupClass, false
		vehicle.PlatoonID, vehicle.PlatoonIndex = "", 0
		vehicle.RiddenMeters = 0.0000010000000000000002
		vehicle.Boardings = make([]sim.RiderBoarding, sim.MaxSharedRideParties)
		for j := range vehicle.Riders {
			vehicle.Riders[j].PartySize, vehicle.Riders[j].LegacyPartySize = 1, false
			vehicle.Riders[j].SharingConsent, vehicle.Riders[j].Service = sim.SharedConsent, sim.OnDemandService
			vehicle.Riders[j].ServiceID = ""
			vehicle.Boardings[j] = sim.RiderBoarding{BerthID: strings.Repeat("\x01", 64), MetersAtBoarding: vehicle.RiddenMeters}
		}
	}
	if len(frame.State.Simulation.Vehicles) != project.MaxPods {
		t.Fatal("group byte fixture changed fleet limit")
	}
	full := StreamEnvelope{Kind: "full", Stream: "group-max", Sequence: 1, Source: sourceOf(frame), Full: &frame}
	_, empty := streamFixture(t)
	empty.State.Simulation.Vehicles = make([]VehicleFrame, project.MaxPods)
	empty.Routes = make([]sim.RoutePresentation, project.MaxPods)
	empty.State.Simulation.Berths = make([]sim.BerthState, project.MaxNodes)
	delta, err := makeDelta(empty, frame)
	if err != nil {
		t.Fatal(err)
	}
	changed := StreamEnvelope{Kind: "delta", Stream: "group-max", Sequence: 2, Base: 1, Source: sourceOf(frame), Delta: &delta}
	for _, envelope := range []StreamEnvelope{full, changed} {
		raw, err := json.Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}
		if len(raw) > MaxStreamJSON {
			t.Fatal("typed group maximum exceeds unchanged byte cap", len(raw))
		}
		compressed, err := encodeStream(envelope)
		if err != nil {
			t.Fatal(err)
		}
		inflated, err := InflateStream(compressed)
		if err != nil || !bytes.Equal(raw, inflated) {
			t.Fatal("typed group maximum gzip round trip", err)
		}
		if scanErr := prescanJSON(inflated, unpackedStreamLimits()); scanErr != nil {
			t.Fatal("typed group maximum failed the bounded scan", scanErr)
		}
		decoded, err := DecodeStreamJSONVersion(inflated, 3)
		if err != nil {
			t.Fatal(err)
		}
		if decoded.Full != nil && len(decoded.Full.State.Simulation.Vehicles) != project.MaxPods || decoded.Delta != nil && len(decoded.Delta.Vehicles) != project.MaxPods {
			t.Fatal("typed group maximum lost vehicle records")
		}
		t.Logf("typed group %s maximum: raw=%d gzip=%d cap=%d", envelope.Kind, len(raw), len(compressed), MaxStreamJSON)
	}
}
