package sim

import (
	"bytes"
	"encoding/json/v2"
	"maps"
	"math"
	"reflect"
	"testing"
)

func TestMotionActualJourneyParity(t *testing.T) {
	t.Parallel()
	off := newTraffic(t)
	if err := off.StartDemo(); err != nil {
		t.Fatal(err)
	}
	on := off.Clone()
	if _, ok := off.MotionFrame(); ok || off.motion != nil {
		t.Fatal("recording enabled by default")
	}
	on.SetMotionRecording(true)
	departures, arrivals, moved := 0, 0, false
	for range demoTickLimit {
		before := on.Snapshot()
		off.Step()
		on.Step()
		after := on.Snapshot()
		frame, ok := on.MotionFrame()
		if !ok || frame.Tick != on.Tick() {
			t.Fatal("missing advanced frame")
		}
		byID := make(map[string]MotionSample)
		distance := 0.0
		for _, sample := range frame.Samples {
			if _, duplicate := byID[sample.ID]; duplicate {
				t.Fatal("duplicate sample")
			}
			byID[sample.ID] = sample
			distance += sample.DistanceMeters
			moved = moved || sample.DistanceMeters > 0
		}
		want := after.PassengerDistanceMeters + after.EmptyDistanceMeters - before.PassengerDistanceMeters - before.EmptyDistanceMeters
		if math.Abs(distance-want) > 1e-8 {
			t.Fatalf("recorded distance %g, counters %g", distance, want)
		}
		for i, current := range after.Vehicles {
			old := before.Vehicles[i].Pod
			sample, found := byID[current.Pod.ID]
			if found {
				profile, _ := LookupVehicleClass(current.Pod.Class)
				if sample.StartSpeed != old.Speed || sample.EndSpeed != current.Pod.Speed || sample.Class != profile.Class {
					t.Fatalf("speed/class lost across tick: %+v", sample)
				}
			}
			if departs(old.Activity) && current.Pod.Activity == Traveling {
				departures++
				if found {
					t.Fatal("departure tick invented movement")
				}
			}
			if old.Activity == Traveling && current.Pod.Activity == Unloading {
				arrivals++
				if !found || sample.StartSpeed <= 0 || sample.EndSpeed != 0 || sample.DistanceMeters <= 0 {
					t.Fatalf("arrival lost braking: %+v", sample)
				}
			}
		}
		if !reflect.DeepEqual(after, off.Snapshot()) || !maps.Equal(on.owners, off.owners) {
			t.Fatal("recording changed motion or ownership")
		}
		left, err := json.Marshal(on.ExportState(), json.Deterministic(true))
		if err != nil {
			t.Fatal(err)
		}
		right, err := json.Marshal(off.ExportState(), json.Deterministic(true))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(left, right) {
			t.Fatal("recording changed saved bytes")
		}
		if after.Completed >= 3 {
			break
		}
	}
	if departures == 0 || arrivals == 0 || !moved || on.completed < 3 {
		t.Fatalf("missing journey phases: departures=%d arrivals=%d completed=%d", departures, arrivals, on.completed)
	}
}

func TestMotionLifecycle(t *testing.T) {
	t.Parallel()
	s := newTraffic(t)
	s.SetMotionRecording(true)
	s.Step()
	frame, ok := s.MotionFrame()
	if !ok || frame.Tick != 1 || len(frame.Samples) != 0 {
		t.Fatal("idle tick has no clock")
	}
	s.SetMotionRecording(true)
	if next, _ := s.MotionFrame(); !reflect.DeepEqual(frame, next) {
		t.Fatal("repeated enable cleared frame")
	}
	s.SetPaused(true)
	s.Step()
	if next, _ := s.MotionFrame(); !reflect.DeepEqual(frame, next) {
		t.Fatal("pause changed frame")
	}
	s.SetPaused(false)
	s.SetMotionRecording(false)
	if _, ok := s.MotionFrame(); ok || s.motion != nil {
		t.Fatal("disable kept recorder")
	}
	s.SetMotionRecording(true)
	if next, _ := s.MotionFrame(); next.Tick != s.Tick() || len(next.Samples) != 0 {
		t.Fatal("reenable did not set baseline")
	}
	if err := s.RequestJourney("01", "market"); err != nil {
		t.Fatal(err)
	}
	for range 30 * TicksPerSecond {
		s.Step()
		frame, _ = s.MotionFrame()
		if len(frame.Samples) > 0 {
			break
		}
	}
	if len(frame.Samples) == 0 {
		t.Fatal("fixture did not move")
	}
	// Both buffers contain samples before cloning, so the check covers each storage boundary.
	s.Step()
	if len(s.motion.frame.Samples) == 0 || len(s.motion.pending) == 0 {
		t.Fatal("clone fixture needs both frame buffers")
	}
	clone := s.Clone()
	if &clone.motion.pending[0] == &s.motion.pending[0] {
		t.Fatal("clone shared existing pending storage")
	}
	if clone.motion == s.motion || &clone.motion.frame.Samples[0] == &s.motion.frame.Samples[0] {
		t.Fatal("clone shared recording storage")
	}
	frame.Samples[0].DistanceMeters = -1
	if next, _ := s.MotionFrame(); next.Samples[0].DistanceMeters < 0 {
		t.Fatal("accessor shared storage")
	}
	for range 3 {
		s.Step()
		clone.Step()
	}
	if !reflect.DeepEqual(s.motion.frame, clone.motion.frame) {
		t.Fatal("clone recording diverged")
	}
	if len(s.motion.pending) == 0 || &s.motion.pending[0] == &clone.motion.pending[0] {
		t.Fatal("clone shared pending storage")
	}
	for _, logical := range []bool{false, true} {
		restored, _, err := RestoreState(RestoreStateInput{Network: s.network, Fleet: s.initial, State: s.ExportState(), LogicalOnly: logical})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := restored.MotionFrame(); ok || restored.motion != nil {
			t.Fatal("restore enabled recording")
		}
		restored.SetMotionRecording(true)
		if baseline, _ := restored.MotionFrame(); baseline.Tick != restored.Tick() || len(baseline.Samples) != 0 {
			t.Fatal("restore invented history")
		}
	}
	s.Reset()
	if next, ok := s.MotionFrame(); !ok || next.Tick != 0 || len(next.Samples) != 0 || len(s.motion.pending) != 0 {
		t.Fatal("reset retained history")
	}
}

func TestMotionZeroDistanceBraking(t *testing.T) {
	t.Parallel()
	s := newTraffic(t)
	s.SetMotionRecording(true)
	s.beginMotionFrame()
	s.recordMotion(MotionSample{ID: "brake", StartSpeed: 1})
	s.publishMotionFrame()
	frame, _ := s.MotionFrame()
	if len(frame.Samples) != 1 || frame.Samples[0].StartSpeed != 1 || frame.Samples[0].EndSpeed != 0 {
		t.Fatal("zero-distance braking omitted")
	}
}

func TestMotionNativeClasses(t *testing.T) {
	t.Parallel()
	for _, class := range []VehicleClass{"", LegacyClass, CompactClass, GroupClass} {
		t.Run(string(class), func(t *testing.T) {
			t.Parallel()
			s, err := NewFleet(largeMotionNetwork(true), []Placement{{ID: "sample", Class: class, StationID: "a", BerthID: "a-1"}})
			if err != nil {
				t.Fatal(err)
			}
			if err := s.RequestJourney("sample", "b"); err != nil {
				t.Fatal(err)
			}
			s.SetMotionRecording(true)
			profile, _ := LookupVehicleClass(class)
			arrived := false
			for range 300 * TicksPerSecond {
				s.Step()
				frame, _ := s.MotionFrame()
				for _, sample := range frame.Samples {
					if sample.Class != profile.Class {
						t.Fatalf("class %q became %q", class, sample.Class)
					}
				}
				if s.vehicles[0].Pod.Activity == Unloading {
					if len(frame.Samples) != 1 || frame.Samples[0].EndSpeed != 0 {
						t.Fatal("class arrival sample missing")
					}
					arrived = true
					break
				}
			}
			if !arrived {
				t.Fatal("class fixture did not arrive")
			}
		})
	}
}
