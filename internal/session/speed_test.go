package session

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
)

func TestPlaybackChoices(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ speed, next, lower int }{{1, 2, 1}, {2, 5, 1}, {4, 5, 2}, {5, 15, 2}, {8, 15, 5}, {15, 60, 5}, {60, 1, 15}} {
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
	for _, speed := range []int{-1, 0, 3, 16, 61} {
		if validSpeed(speed) {
			t.Errorf("accepted %d", speed)
		}
	}
}

func TestSpeedMonitor(t *testing.T) {
	t.Parallel()
	start := time.Unix(100, 0)
	for _, tc := range []struct {
		name               string
		elapsed            time.Duration
		steps, speed, want int
	}{
		{"too soon", 3*time.Second - time.Nanosecond, 0, 60, 60},
		{"overloaded", 3 * time.Second, 9000, 60, 15},
		{"threshold", 3 * time.Second, 9720, 60, 60},
		{"brief stall", 3 * time.Second, 10440, 60, 60},
		{"late observation", 4 * time.Second, 10000, 60, 15},
		{"minimum", 3 * time.Second, 0, 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := playbackClock{}
			c.observe(start, tc.speed)
			c.steps = tc.steps
			got, _ := c.observe(start.Add(tc.elapsed), tc.speed)
			if got != tc.want {
				t.Fatalf("got %d, want %d", got, tc.want)
			}
			if tc.elapsed >= speedWindow {
				if next, _ := c.observe(start.Add(tc.elapsed+time.Second), got); next != got {
					t.Fatal("no cooldown")
				}
			}
		})
	}
	c := playbackClock{}
	c.observe(start, 60)
	c.reset()
	if next, _ := c.observe(start.Add(time.Hour), 60); next != 60 {
		t.Fatal("reset included inactive time")
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
			s.liveAdvance(context.Background())
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
	s.clock.start = time.Now().Add(-speedWindow)
	s.clock.steps = 0
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
	for _, speed := range []int{1, 2, 4, 5, 8, 15, 60} {
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
}
