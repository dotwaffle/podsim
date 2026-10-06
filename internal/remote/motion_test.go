package remote

import (
	"fmt"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

func motionState(tick int64, x float64) session.State {
	route := []sim.Lane{{ID: "ab", From: "a", To: "b", SpeedLimit: 14}, {ID: "bc", From: "b", To: "c", SpeedLimit: 14}}
	return session.State{Epoch: "one", Revision: uint64(tick + 1), Speed: 1, Network: sim.Network{Nodes: []sim.Node{{ID: "a", Position: sim.Point{}}, {ID: "b", Position: sim.Point{X: 10}}, {ID: "c", Position: sim.Point{X: 10, Y: 10}}}, Lanes: route}, Simulation: sim.Snapshot{Tick: tick, Vehicles: []sim.Vehicle{{Pod: sim.Pod{ID: "01", LaneID: "ab", LaneDistance: x, Position: sim.Point{X: x}, Activity: sim.Traveling}, Route: route}}}}
}

func TestMotionSmoothsSnapshotsWithoutChangingState(t *testing.T) {
	t.Parallel()
	var motion Motion
	start := time.Unix(100, 0)
	first, last := motionState(0, 0), motionState(6, 1.4)
	motion.Observe(first, start)
	motion.Observe(last, start.Add(100*time.Millisecond))
	for _, tc := range []struct {
		ms   int
		want float64
	}{{150, 0}, {175, .35}, {200, .7}, {225, 1.05}, {250, 1.4}, {1000, 1.4}} {
		got := motion.Sample(start.Add(time.Duration(tc.ms) * time.Millisecond))
		if math.Abs(got.Vehicles[0].Pod.Position.X-tc.want) > 1e-9 {
			t.Fatalf("%dms: %v, want%v", tc.ms, got.Vehicles[0].Pod.Position, tc.want)
		}
	}
	if !reflect.DeepEqual(first, motionState(0, 0)) || !reflect.DeepEqual(last, motionState(6, 1.4)) {
		t.Fatal("interpolation mutated authoritative state")
	}
}

func TestMotionFollowsCornersAndBerthTransitions(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"corner", "arrival", "departure", "changed route", "impossible teleport"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			a, b := motionState(0, 9), motionState(12, 9)
			b.Simulation.Vehicles[0].Pod.LaneID = "bc"
			b.Simulation.Vehicles[0].Pod.LaneDistance = 1
			b.Simulation.Vehicles[0].Pod.Position = sim.Point{X: 10, Y: 1}
			want := sim.Point{X: 10}
			switch name {
			case "arrival":
				a.Simulation.Vehicles[0].Pod.LaneID = "bc"
				a.Simulation.Vehicles[0].Pod.LaneDistance = 8
				a.Simulation.Vehicles[0].Pod.Position = sim.Point{X: 10, Y: 8}
				b.Simulation.Vehicles[0].Pod.LaneID = ""
				b.Simulation.Vehicles[0].Pod.Position = sim.Point{X: 10, Y: 10}
				b.Simulation.Vehicles[0].Pod.Activity = sim.Unloading
				want = sim.Point{X: 10, Y: 9}
			case "departure":
				a = motionState(0, 0)
				a.Simulation.Vehicles[0].Pod.LaneID = ""
				a.Simulation.Vehicles[0].Route = nil
				b = motionState(12, 2)
				want = sim.Point{X: 1}
			case "changed route":
				a.Simulation.Vehicles[0].Route = nil
			case "impossible teleport":
				b.Simulation.Vehicles[0].Pod.LaneDistance = 9
				b.Simulation.Vehicles[0].Pod.Position = sim.Point{X: 10, Y: 9}
				want = sim.Point{X: 9}
			}
			got := interpolate(a, b).Vehicles[0].Pod.Position
			if got != want {
				t.Fatalf("position%v, want%v", got, want)
			}
		})
	}
}

func TestMotionDiscontinuitiesSnap(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"pause", "resume", "reset", "epoch", "speed", "gap", "demo", "generation"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var motion Motion
			start := time.Unix(100, 0)
			a, b := motionState(30, 5), motionState(36, 6)
			at := start.Add(100 * time.Millisecond)
			switch name {
			case "generation":
				b.Generation = a.Generation + 1
			case "pause":
				b.Simulation.Paused = true
			case "resume":
				a.Simulation.Paused = true
			case "reset":
				b.Simulation.Tick = 0
			case "epoch":
				b.Epoch = "two"
			case "speed":
				b.Speed = 5
			case "gap":
				at = start.Add(time.Second)
			case "demo":
				b.Simulation.Demo = true
			}
			motion.Observe(a, start)
			motion.Observe(b, at)
			if got := motion.Sample(at).Vehicles[0].Pod.Position.X; got != 6 {
				t.Fatalf("discontinuity interpolated: %v", got)
			}
		})
	}
}

func TestMotionIgnoresDuplicateAndOldRevisions(t *testing.T) {
	t.Parallel()
	var motion Motion
	start := time.Unix(100, 0)
	state := motionState(0, 0)
	motion.Observe(state, start)
	motion.Observe(state, start.Add(time.Second))
	if len(motion.frames) != 1 {
		t.Fatal("duplicate added frame")
	}
	for i := 1; i <= 100; i++ {
		motion.Observe(motionState(int64(i), float64(i)*.1), start.Add(time.Duration(i)*50*time.Millisecond))
	}
	if len(motion.frames) != maxMotionFrames {
		t.Fatalf("motion buffer has %d frames, want %d", len(motion.frames), maxMotionFrames)
	}
	if got := motion.Sample(start.Add(5 * time.Second)).Vehicles[0].Pod.Position.X; math.Abs(got-9.7) > 1e-9 {
		t.Fatalf("motion after buffer rollover = %v, want 9.7", got)
	}
	motion.Observe(state, start.Add(6*time.Second))
	if motion.frames[len(motion.frames)-1].state.Simulation.Tick != 100 {
		t.Fatal("old revision replaced latest")
	}
}

func TestMotionFollowsCurvedLane(t *testing.T) {
	a, b := motionState(0, 0), motionState(600, 0)
	lane := sim.Lane{ID: "curve", From: "a", To: "b", SpeedLimit: 14, Control: &sim.Point{X: 5, Y: 10}}
	a.Network.Lanes = []sim.Lane{lane}
	b.Network = a.Network
	length := a.Network.Length(lane)
	for i, state := range []*session.State{&a, &b} {
		v := &state.Simulation.Vehicles[0]
		v.Route = []sim.Lane{lane}
		v.Pod.LaneID = lane.ID
		v.Pod.LaneDistance = float64(i) * length
		v.Pod.Position = state.Network.Position(lane, v.Pod.LaneDistance)
	}
	got := interpolate(a, b).Vehicles[0].Pod.Position
	if math.Abs(got.X-5) > 0.001 || math.Abs(got.Y-5) > 0.001 {
		t.Fatalf("cut across curve: %+v", got)
	}
}

// TestMotionNewServerStartRestarts checks that a restored older save from a
// new server process replaces the buffered motion, although it keeps the
// epoch and has a lower revision.
func TestMotionNewServerStartRestarts(t *testing.T) {
	t.Parallel()
	var motion Motion
	start := time.Unix(100, 0)
	for i := int64(10); i <= 12; i++ {
		state := motionState(i, float64(i)*.1)
		state.ServerStart = "a"
		motion.Observe(state, start.Add(time.Duration(i)*50*time.Millisecond))
	}
	restored := motionState(2, .2)
	restored.ServerStart = "b"
	restored.Simulation.Paused = true
	motion.Observe(restored, start.Add(time.Second))
	if len(motion.frames) != 1 || motion.frames[0].state.ServerStart != "b" {
		t.Fatalf("motion keeps %d frames after a restart, want only the restored one", len(motion.frames))
	}
	if got := motion.Sample(start.Add(2 * time.Second)).Tick; got != 2 {
		t.Fatalf("map tick %d after a restart, want 2", got)
	}
	old := motionState(12, 1.2)
	old.ServerStart = "b"
	old.Revision = 1
	motion.Observe(old, start.Add(2*time.Second))
	if len(motion.frames) != 1 {
		t.Fatal("an older revision from the new process was added")
	}
}

func TestMotionBoundedRouteWindows(t *testing.T) {
	for _, name := range []string{"corner", "replacement", "gap", "repeated occurrence", "shift"} {
		t.Run(name, func(t *testing.T) {
			a, b := motionState(0, 9), motionState(12, 9)
			before, after := &a.Simulation.Vehicles[0], &b.Simulation.Vehicles[0]
			after.Pod.LaneID = "bc"
			after.Pod.LaneDistance = 1
			after.Pod.Position = sim.Point{X: 10, Y: 1}
			before.Presentation = &sim.RoutePresentation{Identity: 1, Current: 0, Motion: before.Route}
			after.Presentation = &sim.RoutePresentation{Identity: 1, Current: 1, Motion: after.Route}
			want := sim.Point{X: 10}
			switch name {
			case "replacement":
				after.Presentation.Identity = 2
				want = before.Pod.Position
			case "gap":
				before.Presentation.Motion = before.Route[:1]
				before.Presentation.After = true
				after.Presentation.Start = 1
				after.Presentation.Motion = after.Route[1:]
				want = before.Pod.Position
			case "repeated occurrence":
				route := append(append([]sim.Lane{}, before.Route...), before.Route...)
				before.Presentation.Motion = route
				after.Presentation.Motion = route
				before.Presentation.Current = 2
				after.Presentation.Current = 3
			case "shift":
				before.Presentation.Start = 100
				before.Presentation.Current = 100
				after.Presentation.Start = 101
				after.Presentation.Current = 101
				after.Presentation.Motion = after.Route[1:]
			}
			if got := interpolate(a, b).Vehicles[0].Pod.Position; got != want {
				t.Fatalf("position=%v want=%v", got, want)
			}
		})
	}
}

// platoonState returns a state with a platoon of two pods, 1 m apart, and
// the lead pod at x.
func platoonState(tick int64, x float64) session.State {
	state := motionState(tick, x)
	lead := state.Simulation.Vehicles[0]
	lead.Pod.ID, lead.PlatoonID, lead.PlatoonIndex = "02", "02", 1
	lead.Pod.LaneDistance++
	lead.Pod.Position.X++
	state.Simulation.Vehicles[0].PlatoonID, state.Simulation.Vehicles[0].PlatoonIndex = "02", 2
	state.Simulation.Vehicles = append(state.Simulation.Vehicles, lead)
	return state
}

// TestMotionPlatoonAndDepartingPod checks that the pods of a platoon each
// follow their own lane position, and that a pod that leaves between two
// frames keeps its position in the earlier frame.
func TestMotionPlatoonAndDepartingPod(t *testing.T) {
	t.Parallel()
	a, b := platoonState(0, 0), platoonState(6, 1.4)
	got := interpolate(a, b)
	if len(got.Vehicles) != 2 {
		t.Fatalf("got %d pods, want 2", len(got.Vehicles))
	}
	for i, want := range []float64{.7, 1.7} {
		if x := got.Vehicles[i].Pod.Position.X; math.Abs(x-want) > 1e-9 {
			t.Fatalf("pod %s at %v, want %v", got.Vehicles[i].Pod.ID, x, want)
		}
		if got.Vehicles[i].PlatoonID != "02" || got.Vehicles[i].PlatoonIndex != a.Simulation.Vehicles[i].PlatoonIndex {
			t.Fatalf("pod %s lost its platoon place", got.Vehicles[i].Pod.ID)
		}
	}
	b.Simulation.Vehicles = b.Simulation.Vehicles[:1]
	got = interpolate(a, b)
	if len(got.Vehicles) != 2 || math.Abs(got.Vehicles[0].Pod.Position.X-.7) > 1e-9 || !reflect.DeepEqual(got.Vehicles[1], a.Simulation.Vehicles[1]) {
		t.Fatalf("departing pod changed: %+v", got.Vehicles)
	}
}

// TestMotionSampleAllocations reports the allocations of one map sample with
// the largest fleet. A sample between frames allocates only its vehicle
// slice, and a sample after the latest frame allocates nothing.
func TestMotionSampleAllocations(t *testing.T) {
	start := time.Unix(100, 0)
	frame := func(tick int64, x float64) session.State {
		state := motionState(tick, x)
		pod := state.Simulation.Vehicles[0]
		state.Simulation.Vehicles = nil
		for i := range project.MaxPods {
			pod.Pod.ID = fmt.Sprintf("%03d", i)
			state.Simulation.Vehicles = append(state.Simulation.Vehicles, pod)
		}
		return state
	}
	var motion Motion
	motion.Observe(frame(0, 0), start)
	motion.Observe(frame(6, 1.4), start.Add(100*time.Millisecond))
	at := start.Add(200 * time.Millisecond)
	sample := motion.Sample(at)
	if len(sample.Vehicles) != project.MaxPods || sample.Vehicles[0].Pod.Position.X == 0 || sample.Vehicles[0].Pod.Position.X == 1.4 {
		t.Fatalf("sample has %d pods at %v, want %d between the frames", len(sample.Vehicles), sample.Vehicles[0].Pod.Position, project.MaxPods)
	}
	if allocations := testing.AllocsPerRun(100, func() { motion.Sample(at) }); allocations != 1 {
		t.Fatalf("sample between frames has %g allocations, want 1", allocations)
	}
	if allocations := testing.AllocsPerRun(100, func() { motion.Sample(at.Add(time.Second)) }); allocations != 0 {
		t.Fatalf("sample of the latest frame has %g allocations, want 0", allocations)
	}
}

// interpolate returns the midpoint of two states.
func interpolate(a, b session.State) sim.Snapshot {
	return interpolateFrames(newMotionFrame(a, time.Time{}), newMotionFrame(b, time.Time{}), .5, newMotionGeometry(a))
}
