package session

import (
	"reflect"
	"slices"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

// TestApplyStreamRefusalOrder pins which refusal comes first when an
// envelope has two faults, and the refusal of each single fault. A refused
// envelope gives a zero frame.
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
	cases := []struct {
		name string
		e    StreamEnvelope
		want string
	}{
		{"identity before kind", StreamEnvelope{Kind: "other", Sequence: 1}, "invalid stream identity"},
		{"full shape before identity", full(nil), "invalid full envelope"},
		{"full identity", full(nil), "full identity mismatch"},
		{"unknown kind", StreamEnvelope{Kind: "other", Stream: "test", Sequence: 1, Source: sourceOf(base)}, "unknown state envelope"},
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
