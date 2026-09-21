package remote

import (
	"math"
	"reflect"
	"testing"
	"time"

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
			got := interpolate(a, b, .5).Vehicles[0].Pod.Position
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
				b.Speed = 8
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
	got := interpolate(a, b, .5).Vehicles[0].Pod.Position
	if math.Abs(got.X-5) > 0.001 || math.Abs(got.Y-5) > 0.001 {
		t.Fatalf("cut across curve: %+v", got)
	}
}
