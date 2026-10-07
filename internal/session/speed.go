package session

import (
	"context"
	"log/slog"
	"runtime"
	"time"

	"github.com/dotwaffle/podsim/internal/sim"
)

const (
	speedWindow  = 5 * time.Second
	catchUpLimit = 250 * time.Millisecond
	wakeBudget   = 20 * time.Millisecond
)

// NextSpeed cycles the playback choices.
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
	case 1, 2, 5, 15, 60:
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
	buckets [5]int
	count   int
	cursor  int
	paced   time.Time
	debt    float64
}

func (c *playbackClock) reset() {
	*c = playbackClock{version: c.version + 1}
}

// closeBuckets records completed seconds. A long suspension retains only
// the last five seconds, so it cannot cause an unbounded accounting loop.
func (c *playbackClock) closeBuckets(now time.Time) {
	if c.start.IsZero() {
		c.start = now
		return
	}
	elapsed := int(now.Sub(c.start) / time.Second)
	if elapsed > len(c.buckets) {
		c.buckets = [5]int{}
		c.count = len(c.buckets)
		c.steps = 0
		c.start = c.start.Add(time.Duration(elapsed) * time.Second)
		return
	}
	for range elapsed {
		c.buckets[c.cursor] = c.steps
		c.cursor = (c.cursor + 1) % len(c.buckets)
		c.count = min(c.count+1, len(c.buckets))
		c.steps = 0
		c.start = c.start.Add(time.Second)
	}
}

func (c *playbackClock) observe(now time.Time, speed int) (int, float64) {
	c.closeBuckets(now)
	if c.count < len(c.buckets) {
		return speed, 0
	}
	slow, total := 0, 0
	for _, steps := range c.buckets {
		total += steps
		if float64(steps) < float64(speed*sim.TicksPerSecond)*.9 {
			slow++
		}
	}
	achieved := float64(total) / sim.TicksPerSecond / float64(len(c.buckets))
	if slow >= 2 {
		return lowerSpeed(speed), achieved
	}
	return speed, achieved
}

// due retains missed timer wakes as step debt, capped at 250 ms of wall
// time. Excess wall time is discarded, never an individual physics step.
func (c *playbackClock) due(now time.Time, speed int) int {
	if c.paced.IsZero() {
		c.paced = now
		c.debt = float64(speed)
	}
	elapsed := max(0, now.Sub(c.paced).Seconds())
	c.paced = now
	rate := float64(speed * sim.TicksPerSecond)
	c.debt = min(c.debt+elapsed*rate, catchUpLimit.Seconds()*rate)
	return int(c.debt + 1e-6)
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
	now := time.Now()
	next, achieved := s.clock.observe(now, from)
	var reduction SpeedReduction
	if next != from {
		s.speed = next
		s.clock.reset()
		s.clock.observe(now, next)
		s.speedReduction = SpeedReduction{Sequence: s.speedReduction.Sequence + 1, From: from, To: next}
		reduction = s.speedReduction
		s.revision++
	}
	remaining, version := s.clock.due(now, s.speed), s.clock.version
	s.mu.Unlock()
	if reduction.Sequence != 0 {
		s.logger.Warn("Reduced playback speed", slog.Int("from", from), slog.Int("to", next), slog.Float64("achieved", achieved))
	}
	deadline := time.Now().Add(wakeBudget)
	for remaining > 0 && ctx.Err() == nil {
		completed := s.liveBatch(version, remaining)
		if completed == 0 {
			return
		}
		remaining -= completed
		if time.Now().After(deadline) {
			return
		}
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
	if completed == 0 {
		return 0
	}
	s.clock.closeBuckets(time.Now())
	s.clock.steps += completed
	s.clock.debt = max(0, s.clock.debt-float64(completed))
	s.revision++
	return completed
}

// step advances physics and demand together. The caller holds mu.
func (s *Session) step() {
	dailyChanged := s.demand.activateDaily(s.simulation.Tick() + 1)
	if dailyChanged {
		s.configureRedistribution()
	}
	wasDemo := s.simulation.DemoRunning()
	s.simulation.Step()
	// Deliver before demand.step. Advance there scores the departures of
	// this tick, and an interruption must reach rail first.
	s.deliverInterruptions()
	if wasDemo && !s.simulation.DemoRunning() {
		s.configureRedistribution()
	}
	s.demand.step(s.simulation)
}
