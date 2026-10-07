package session

import (
	"reflect"
	"slices"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

// TestApplyStreamRefusalOrder pins which refusal comes first when an
// envelope has two faults, and the refusal of each single fault. The stage
// presence checks come after the identity, in stage order, and before the
// delta base. A refused envelope gives a zero frame.
func TestApplyStreamRefusalOrder(t *testing.T) {
	t.Parallel()
	_, base := streamFixture(t)
	if len(base.State.Simulation.Vehicles) < 2 || len(base.State.Simulation.Berths) < 2 {
		t.Fatal("fixture needs two vehicles and two berths")
	}
	pod := base.State.Simulation.Vehicles[0].Pod
	other := base.State.Simulation.Vehicles[1].Pod.ID
	berth := base.State.Simulation.Berths[0]
	full := func(edit func(*StreamFrame)) StreamEnvelope {
		f := base
		f.State.Simulation.Vehicles = slices.Clone(base.State.Simulation.Vehicles)
		f.Routes = slices.Clone(base.Routes)
		if edit != nil {
			edit(&f)
		}
		return StreamEnvelope{Kind: "full", Stream: "test", Sequence: 1, Source: sourceOf(f), Build: f.State.Build, Full: &f}
	}
	delta := func(d StreamDelta, edit func(*StreamEnvelope)) StreamEnvelope {
		e := StreamEnvelope{Kind: "delta", Stream: "test", Sequence: 2, Base: 1, Source: sourceOf(base), Build: base.State.Build, Delta: &d}
		if edit != nil {
			edit(&e)
		}
		return e
	}
	renamed := pod
	renamed.ID = "renamed"
	unknownBerth := sim.BerthState{ID: "unknown"}
	crossEpoch := func(e *StreamEnvelope) { e.Source.Epoch = "other" }
	// members marks the stage members that a decode found in an envelope
	// without the stage markers.
	members := func(incident, fault, emergency bool) func(*StreamEnvelope) {
		return func(e *StreamEnvelope) {
			e.incidentMembers, e.faultMembers, e.emergencyMembers = incident, fault, emergency
		}
	}
	cases := []struct {
		name string
		e    StreamEnvelope
		want string
	}{
		{"identity before kind", StreamEnvelope{Kind: "other", Sequence: 1}, "invalid stream identity"},
		{"full shape before identity", full(nil), "invalid full envelope"},
		{"full identity", full(nil), "full identity mismatch"},
		{"unknown kind", StreamEnvelope{Kind: "other", Stream: "test", Sequence: 1, Source: sourceOf(base)}, "unknown state envelope"},
		{"sequence range before kind", StreamEnvelope{Kind: "other", Stream: "test", Sequence: sim.MaxCounter + 1, Source: sourceOf(base)}, "invalid stream identity"},
		{"delta base", delta(StreamDelta{}, func(e *StreamEnvelope) { e.Base = 2; e.Source.Epoch = "other" }), "delta base mismatch"},
		{"vehicle before berth", delta(StreamDelta{Vehicles: []VehicleDelta{{ID: "unknown"}}, Berths: []sim.BerthState{unknownBerth}}, nil), "invalid delta vehicle"},
		{"duplicate vehicle", delta(StreamDelta{Vehicles: []VehicleDelta{{ID: other}, {ID: other}}}, nil), "invalid delta vehicle"},
		{"pairs before pod ID", delta(StreamDelta{Vehicles: []VehicleDelta{{ID: pod.ID, Pod: &Replacement[sim.Pod]{renamed},
			Boardings: &Replacement[[]sim.RiderBoarding]{[]sim.RiderBoarding{{BerthID: berth.ID}}}}}}, nil), "boarding records and riders need paired replacements"},
		{"boarding array before pod ID", delta(StreamDelta{Vehicles: []VehicleDelta{{ID: pod.ID, Pod: &Replacement[sim.Pod]{renamed},
			Boardings: &Replacement[[]sim.RiderBoarding]{}}}}, nil), "boarding replacement needs an array"},
		{"pod ID before berth", delta(StreamDelta{Vehicles: []VehicleDelta{{ID: pod.ID, Pod: &Replacement[sim.Pod]{renamed}}}, Berths: []sim.BerthState{unknownBerth}}, nil), "changed pod ID"},
		{"berth before source", delta(StreamDelta{Berths: []sim.BerthState{unknownBerth}}, crossEpoch), "invalid delta berth"},
		{"duplicate berth", delta(StreamDelta{Berths: []sim.BerthState{berth, berth}}, nil), "invalid delta berth"},
		{"source boundary", delta(StreamDelta{}, crossEpoch), "delta crosses source boundary"},
		{"counts before routes", full(func(f *StreamFrame) {
			f.Routes = f.Routes[:len(f.Routes)-1]
			f.State.Simulation.Vehicles[0].RouteLaneIDs = []string{"lane"}
		}), "invalid presentation counts"},
		{"unbounded route", full(func(f *StreamFrame) { f.State.Simulation.Vehicles[0].RouteLaneIDs = []string{"lane"} }), "unbounded stream route"},
		{"unbounded route before identity range", full(func(f *StreamFrame) {
			f.State.Simulation.Vehicles[0].RouteLaneIDs = []string{"lane"}
			f.Routes[0].Identity = sim.MaxCounter + 1
		}), "unbounded stream route"},
		{"route identity range", full(func(f *StreamFrame) { f.Routes[0].Identity = sim.MaxCounter + 1 }), "stream route counter is out of range"},
		{"route start range", full(func(f *StreamFrame) { f.Routes[0].Start = sim.MaxCounter + 1 }), "stream route counter is out of range"},
		{"route current range", full(func(f *StreamFrame) { f.Routes[0].Current = sim.MaxCounter + 1 }), "stream route counter is out of range"},
		{"route range before frame IDs", full(func(f *StreamFrame) {
			f.Routes[0].Current = sim.MaxCounter + 1
			f.State.Simulation.Vehicles[0].Pod.ID = "pod 1"
		}), "stream route counter is out of range"},
		{"pod ID characters", full(func(f *StreamFrame) { f.State.Simulation.Vehicles[0].Pod.ID = "pod 1" }), errFrameIDText.Error()},
		{"stop characters", full(func(f *StreamFrame) { f.State.Simulation.Vehicles[0].Stops = []string{"a_b"} }), errFrameIDText.Error()},
		{"platoon characters", full(func(f *StreamFrame) { f.State.Simulation.Vehicles[0].PlatoonID = "b\x01" }), errFrameIDText.Error()},
		{"berth characters", full(func(f *StreamFrame) {
			f.State.Simulation.Berths = slices.Clone(f.State.Simulation.Berths)
			f.State.Simulation.Berths[0].ReservedBy = "p&q"
		}), errFrameIDText.Error()},
		{"demand reference characters", full(func(f *StreamFrame) { f.State.Demand.Config.Destination = "harbor<" }), errFrameIDText.Error()},
		{"stream ID characters before kind", StreamEnvelope{Kind: "other", Stream: "a b", Sequence: 1, Source: sourceOf(base)}, "invalid stream identity"},
		{"epoch characters before kind", StreamEnvelope{Kind: "other", Stream: "test", Sequence: 1, Source: func() StreamSource {
			source := sourceOf(base)
			source.Epoch = "e_1"
			return source
		}()}, "invalid stream identity"},
		{"identity before presence", delta(StreamDelta{}, func(e *StreamEnvelope) { members(true, true, true)(e); e.Stream = "" }),
			"invalid stream identity"},
		{"incident before fault presence", delta(StreamDelta{}, members(true, true, true)), errIncidentStreamUnmarked.Error()},
		{"fault before emergency presence", delta(StreamDelta{}, members(false, true, true)), errFaultStreamUnmarked.Error()},
		{"emergency presence before delta base", delta(StreamDelta{}, func(e *StreamEnvelope) { members(false, false, true)(e); e.Base = 2 }),
			errEmergencyStreamUnmarked.Error()},
	}
	cases[1].e.Delta = &StreamDelta{}
	cases[1].e.Source.Revision++
	cases[2].e.Source.Revision++
	for _, c := range cases {
		previous, stream, sequence := base, "test", uint64(1)
		if c.e.Kind != "delta" {
			previous, stream, sequence = StreamFrame{}, "", 0
		}
		f, err := ApplyStream(previous, stream, sequence, c.e)
		if err == nil || err.Error() != c.want {
			t.Errorf("%s: got %v, want %q", c.name, err, c.want)
		}
		if !reflect.DeepEqual(f, StreamFrame{}) {
			t.Errorf("%s: refused envelope gave a frame", c.name)
		}
	}
}
