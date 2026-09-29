package session

import (
	"context"
	"log/slog"
	"runtime"
	"time"

	"github.com/dotwaffle/podsim/internal/sim"
)

const speedWindow = 3 * time.Second

// NextSpeed cycles the visible playback choices. Legacy speeds move to the
// next higher choice. Commands and saved sessions still accept 4x and 8x.
func NextSpeed(speed int) int {
	for _, next := range [...]int{1, 2, 5, 15, 60} {
		if next > speed {
			return next
		}
	}
	return 1
}

func lowerSpeed(speed int) int {
	for _, next := range [...]int{60, 15, 5, 2, 1} {
		if next < speed {
			return next
		}
	}
	return 1
}

func validSpeed(speed int) bool {
	switch speed {
	case 1, 2, 4, 5, 8, 15, 60:
		return true
	}
	return false
}

// SpeedReduction identifies the latest automatic reduction in this process.
// Sequence lets all clients show the notice once, including after reconnects.
// It is transient and is not part of a saved simulation or checkpoint.
type SpeedReduction struct {
	Sequence uint64 `json:"sequence"`
	From     int    `json:"from"`
	To       int    `json:"to"`
}

// playbackClock measures completed steps over wall time. All fields use mu.
// version invalidates unfinished batches after clock-control commands.
type playbackClock struct {
	version uint64
	start   time.Time
	steps   int
}

func (c *playbackClock) reset() {
	c.version++
	c.start = time.Time{}
	c.steps = 0
}

func (c *playbackClock) observe(now time.Time, speed int) (int, float64) {
	if c.start.IsZero() {
		c.start = now
		return speed, 0
	}
	elapsed := now.Sub(c.start)
	if elapsed < speedWindow {
		return speed, 0
	}
	achieved := float64(c.steps) / sim.TicksPerSecond / elapsed.Seconds()
	c.start, c.steps = now, 0
	if achieved < float64(speed)*.9 {
		return lowerSpeed(speed), achieved
	}
	return speed, achieved
}

// liveAdvance advances one clock wake. Each lock hold ends after eight steps
// or four milliseconds, whichever comes first. One step is indivisible.
// Clock-control commands cancel the remaining steps of the old wake.
func (s *Session) liveAdvance(ctx context.Context) {
	s.mu.Lock()
	if s.closed.Load() || s.simulation.Paused() || ctx.Err() != nil {
		s.clock.reset()
		s.mu.Unlock()
		return
	}
	from := s.speed
	next, achieved := s.clock.observe(time.Now(), from)
	var reduction SpeedReduction
	if next != from {
		s.speed = next
		s.speedReduction = SpeedReduction{Sequence: s.speedReduction.Sequence + 1, From: from, To: next}
		reduction = s.speedReduction
		s.revision++
	}
	remaining, version := s.speed, s.clock.version
	s.mu.Unlock()
	if reduction.Sequence != 0 {
		s.logger.Warn("Reduced playback speed", slog.Int("from", from), slog.Int("to", next), slog.Float64("achieved", achieved))
	}
	for remaining > 0 && ctx.Err() == nil {
		completed := s.liveBatch(version, remaining)
		if completed == 0 {
			return
		}
		remaining -= completed
		if remaining > 0 {
			runtime.Gosched()
		}
	}
}

func (s *Session) liveBatch(version uint64, remaining int) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed.Load() || s.simulation.Paused() || version != s.clock.version {
		return 0
	}
	started := time.Now()
	completed := 0
	for completed < min(remaining, 8) {
		s.step()
		completed++
		if time.Since(started) >= 4*time.Millisecond {
			break
		}
	}
	s.clock.steps += completed
	s.revision++
	return completed
}

// step advances physics and demand together. The caller holds mu.
func (s *Session) step() {
	wasDemo := s.simulation.DemoRunning()
	s.simulation.Step()
	if wasDemo && !s.simulation.DemoRunning() {
		s.configureRedistribution()
	}
	s.demand.step(s.simulation)
}
