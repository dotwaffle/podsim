package session

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
)

func TestPlaybackChoices(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ speed, next, lower int }{{1, 2, 1}, {2, 5, 1}, {5, 15, 2}, {15, 60, 5}, {60, 1, 15}} {
		if !validSpeed(tc.speed) || NextSpeed(tc.speed) != tc.next || lowerSpeed(tc.speed) != tc.lower {
			t.Errorf("invalid choices for %d", tc.speed)
		}
		s := newTestSession(t)
		c := commandFor(s, "speed")
		c.Speed = tc.speed
		if reply := s.Apply(c); reply.Error != "" {
			t.Fatal(reply.Error)
		}
		if s.State().Speed != tc.speed {
			t.Fatal("command did not set speed")
		}
	}
	for _, speed := range []int{-1, 0, 3, 4, 8, 16, 61} {
		if validSpeed(speed) {
			t.Errorf("accepted %d", speed)
		}
	}
	for _, speed := range []int{4, 8} {
		s := newTestSession(t)
		c := commandFor(s, "speed")
		c.Speed = speed
		want := fmt.Sprintf("speed %d is not supported; use 1, 2, 5, 15, or 60", speed)
		if reply := s.Apply(c); reply.Error != want || s.State().Speed != 1 {
			t.Errorf("speed %d: reply %q, speed %d", speed, reply.Error, s.State().Speed)
		}
	}
}

func TestSpeedMonitor(t *testing.T) {
	t.Parallel()
	start := time.Unix(100, 0)
	for _, tc := range []struct {
		name    string
		buckets []int
		want    int
	}{
		{"too soon", []int{0, 0, 0, 0}, 60},
		{"one stalled second", []int{3600, 0, 3600, 3600, 3600}, 60},
		{"two slow seconds", []int{3600, 3000, 3600, 3000, 3600}, 15},
		{"threshold", []int{3240, 3240, 3240, 3240, 3240}, 60},
		{"boundary stall recovered", []int{3600, 3200, 3600, 4000, 3600}, 60},
		{"boundary stall unrecovered", []int{3600, 3200, 3200, 3600, 3600}, 15},
		{"old stall expired", []int{0, 3600, 3600, 3600, 3600, 0}, 60},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := playbackClock{}
			c.observe(start, 60)
			got := 60
			for i, steps := range tc.buckets {
				c.steps = steps
				got, _ = c.observe(start.Add(time.Duration(i+1)*time.Second), 60)
			}
			if got != tc.want {
				t.Fatalf("got %d, want %d", got, tc.want)
			}
			c.reset()
			if next, _ := c.observe(start.Add(time.Hour), 60); next != 60 {
				t.Fatal("reset included inactive time")
			}
		})
	}
}

func TestClockCatchUp(t *testing.T) {
	t.Parallel()
	start := time.Unix(100, 0)
	c := playbackClock{paced: start}
	if n := c.due(start.Add(100*time.Millisecond), 60); n != 360 {
		t.Fatalf("missed wakes: %d", n)
	}
	c.debt -= 300
	if n := c.due(start.Add(150*time.Millisecond), 60); n != 240 {
		t.Fatalf("remaining debt: %d", n)
	}
	if n := c.due(start.Add(time.Hour), 60); n != 900 {
		t.Fatalf("unbounded catch-up: %d", n)
	}
	c.reset()
	if n := c.due(start.Add(time.Hour), 1); n > 1 {
		t.Fatalf("reset retained debt: %d", n)
	}
}

func TestLiveBatchesPreserveSimulation(t *testing.T) {
	t.Parallel()
	newActive := func() *Session {
		c := project.Default()
		c.Demand.Enabled = true
		c.Demand.PerMinute = 60
		s, err := NewWithProject(c)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	baseline := newActive()
	for range 600 {
		baseline.advance()
	}
	want := baseline.State()
	for _, speed := range []int{1, 2, 5, 15, 60} {
		s := newActive()
		s.speed = speed
		for range 600 / speed {
			remaining := speed
			for remaining > 0 {
				remaining -= s.liveBatch(s.clock.version, remaining)
			}
		}
		got := s.State()
		if !reflect.DeepEqual(got.Simulation, want.Simulation) || !reflect.DeepEqual(got.Demand, want.Demand) {
			t.Fatalf("%dx changed simulation", speed)
		}
	}
}

func TestClockControlCancelsOldBatch(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"pause", "speed", "reset", "demo", "project", "rewind"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			s := newTestSession(t)
			s.speed = 60
			s.clock.start = time.Now().Add(-time.Hour)
			s.clock.steps = 1
			version := s.clock.version
			c := commandFor(s, action)
			c.Speed = 5
			c.Paused = true
			s.Apply(c)
			if s.clock.version == version || !s.clock.start.IsZero() || s.clock.steps != 0 {
				t.Fatal("control did not reset clock")
			}
			tick := s.State().Simulation.Tick
			if s.liveBatch(version, 60) != 0 || s.State().Simulation.Tick != tick {
				t.Fatal("old wake continued after control")
			}
		})
	}
	s := newTestSession(t)
	if n := s.liveBatch(s.clock.version, 60); n < 1 || n > 8 {
		t.Fatalf("batch advanced %d steps", n)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tick := s.State().Simulation.Tick
	s.liveAdvance(ctx)
	if s.State().Simulation.Tick != tick {
		t.Fatal("canceled clock advanced")
	}
}

func TestAutomaticSpeedReduction(t *testing.T) {
	t.Parallel()
	s := newTestSession(t)
	s.speed = 60
	s.clock.start = time.Now().Add(-speedWindow)
	s.liveAdvance(context.Background())
	state := s.State()
	if state.Speed != 15 || state.SpeedReduction != (SpeedReduction{Sequence: 1, From: 60, To: 15}) {
		t.Fatalf("no reduction: %+v", state.SpeedReduction)
	}
	if s.Frame().SpeedReduction != state.SpeedReduction {
		t.Fatal("HTTP frame lost notice")
	}
	f, err := s.presentationFrame()
	if err != nil {
		t.Fatal(err)
	}
	if f.State.SpeedReduction != state.SpeedReduction {
		t.Fatal("stream lost notice")
	}
	before := s.speed
	s.liveAdvance(context.Background())
	if s.speed != before {
		t.Fatal("cooldown failed")
	}
	s.clock.reset()
	s.clock.start = time.Now().Add(-speedWindow)
	s.liveAdvance(context.Background())
	if s.speed != 5 || s.speedReduction.Sequence != 2 {
		t.Fatal("second reduction failed")
	}
	c := commandFor(s, "pause")
	c.Paused = true
	s.Apply(c)
	s.clock.start = time.Now().Add(-time.Hour)
	s.liveAdvance(context.Background())
	if s.speed != 5 || !s.clock.start.IsZero() {
		t.Fatal("pause counted as overload")
	}
}

func TestSavedPlaybackSpeeds(t *testing.T) {
	t.Parallel()
	for _, speed := range []int{1, 2, 5, 15, 60} {
		file := newTestStateFile(t)
		file.Speed = speed
		got, err := decodeCheckedState(encodeTestState(t, file))
		if err != nil {
			t.Fatalf("speed %d: %v", speed, err)
		}
		if got.Speed != speed {
			t.Fatalf("speed %d restored as %d", speed, got.Speed)
		}
	}
	for _, speed := range []int{4, 8} {
		file := newTestStateFile(t)
		file.Speed = speed
		_, err := decodeCheckedState(encodeTestState(t, file))
		if want := fmt.Sprintf("speed %d is not 1, 2, 5, 15 or 60", speed); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("speed %d restored: %v", speed, err)
		}
	}
}

// TestStateFramesRejectUnsupportedSpeed checks that a client rejects a
// playback speed that the server cannot select, in an HTTP state frame, a
// full stream frame, and a controls delta.
func TestStateFramesRejectUnsupportedSpeed(t *testing.T) {
	t.Parallel()
	shared, frame := streamFixture(t)
	topology := shared.Topology()
	assembler, err := NewStreamAssemblerVersion(topology, 3)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = assembler.State(frame); err != nil {
		t.Fatal("control", err)
	}
	for _, speed := range []int{0, 4, 8, 61} {
		want := fmt.Sprintf("state frame speed %d is not 1, 2, 5, 15 or 60", speed)
		next := frame
		next.State.Revision++
		next.State.Speed = speed
		if _, err := FrameState(topology, next.State); err == nil || err.Error() != want {
			t.Errorf("HTTP frame at speed %d: %v", speed, err)
		}
		if _, err := assembler.State(next); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("full frame at speed %d: %v", speed, err)
		}
		delta, err := makeDelta(frame, next)
		if err != nil {
			t.Fatal(err)
		}
		applied, err := ApplyStream(frame, "speed", 1, StreamEnvelope{Kind: "delta", Stream: "speed", Sequence: 2, Base: 1, Source: sourceOf(next), Delta: &delta})
		if err != nil || applied.State.Speed != speed {
			t.Fatalf("controls delta at speed %d: %v", speed, err)
		}
		if _, err := assembler.State(applied); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("delta at speed %d: %v", speed, err)
		}
	}
}
