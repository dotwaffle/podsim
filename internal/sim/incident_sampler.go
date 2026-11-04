package sim

import (
	"fmt"
	"math"
	"math/rand/v2"
)

// maxIncidentPerHour is the largest scenario rate of faults or of
// emergencies, and incidentHourTicks is the length of the rate unit.
const (
	maxIncidentPerHour = 60
	incidentHourTicks  = 3600 * TicksPerSecond
)

// The kinds of FaultDurationRule.
const (
	FaultDurationFixed       = "fixed"
	FaultDurationUniform     = "uniform"
	FaultDurationExponential = "exponential"
)

// FaultDurationRule is the duration distribution of sampled faults. A
// fixed rule has Seconds. A uniform rule has MinSeconds and MaxSeconds. An
// exponential rule has MinSeconds, MaxSeconds, and MeanSeconds.
type FaultDurationRule struct {
	Kind                                         string
	Seconds, MinSeconds, MaxSeconds, MeanSeconds int
}

// FaultRate holds the scenario fault settings of a project. PerHour counts
// attempts, so a skipped event lowers the real rate.
type FaultRate struct {
	PerHour, DebrisShare, DebrisMeters float64
	Duration                           FaultDurationRule
}

// IncidentRates holds the scenario rates of a project.
type IncidentRates struct {
	Faults             FaultRate
	EmergenciesPerHour float64
	FaultSeed          [2]uint64
	EmergencySeed      [2]uint64
}

// incidentSampler is the state of one scenario sampler (section 5.1.1 of
// the incident stage 7 contract). Its zero value is a stopped sampler. All
// members are values, so Clone copies the stream and a rewind replays the
// same draws.
type incidentSampler struct {
	live   bool
	stream rand.PCG
	// nextTick is the tick of the next event. MaxCounter is the terminal
	// value: it means no more events.
	nextTick int64
	perHour  float64
}

// start makes the sampler live with a new stream, and schedules the first
// event from tick.
func (m *incidentSampler) start(seed [2]uint64, perHour float64, tick int64) {
	*m = incidentSampler{live: true, stream: *rand.NewPCG(seed[0], seed[1]), perHour: perHour}
	m.schedule(tick)
}

// uniform returns the next draw of the stream, from 0 to below 1. The
// contract fixes this form, so a change of rand.Rand cannot change a run.
func (m *incidentSampler) uniform() float64 {
	return float64(m.stream.Uint64()<<11>>11) / (1 << 53)
}

// schedule draws the next interval and sets nextTick from tick.
func (m *incidentSampler) schedule(tick int64) {
	m.nextTick = nextIncidentTick(tick, m.uniform(), m.perHour)
}

// due reports whether an event comes due at tick. The terminal value never
// comes due, also when the tick reaches it.
func (m *incidentSampler) due(tick int64) bool {
	return m.live && m.nextTick != MaxCounter && tick >= m.nextTick
}

// check checks the state of the sampler at a boundary, for the invariant
// with the name: F13 or E9 of the incident stage 7 contract. A sampler is
// live only with its switch on and a rate above 0, and its next tick is
// the terminal value or after the tick.
func (m *incidentSampler) check(name string, on bool, tick int64) error {
	switch {
	case !m.live:
		return nil
	case !on:
		return fmt.Errorf("%s: a sampler is live with its switch off", name)
	case !(m.perHour > 0):
		return fmt.Errorf("%s: a sampler is live with the rate %g", name, m.perHour)
	case m.nextTick != MaxCounter && m.nextTick <= tick:
		return fmt.Errorf("%s: the next tick %d is not after the tick %d", name, m.nextTick, tick)
	}
	return nil
}

// nextIncidentTick returns the tick of the next event after tick, for the
// draw u and a rate above 0. The interval is at least one tick, so a
// sampler has at most one event in a tick. A result at or above MaxCounter,
// also +Inf, is the terminal value. The test is in float64, because a
// conversion of such a value to int64 is not defined.
func nextIncidentTick(tick int64, u, perHour float64) int64 {
	interval := math.Ceil(-math.Log(1-u) * incidentHourTicks / perHour)
	next := float64(tick) + max(1, interval)
	if next >= MaxCounter {
		return MaxCounter
	}
	return int64(next)
}

// StartIncidentSampling starts each sampler that has a rate above 0, its
// switch on, and no live sampler. Its first interval starts at the current
// tick. A live sampler keeps its stream, so a call after a restore changes
// nothing. It refuses a rate outside 0 to 60 before any change.
func (s *Simulation) StartIncidentSampling(rates IncidentRates) error {
	for _, rate := range []float64{rates.Faults.PerHour, rates.EmergenciesPerHour} {
		if !(rate >= 0 && rate <= maxIncidentPerHour) {
			return fmt.Errorf("incident rate %g is outside 0 to %d", rate, maxIncidentPerHour)
		}
	}
	if s.faultsOn && rates.Faults.PerHour > 0 && !s.faultSampler.live {
		s.faultRate = rates.Faults
		s.faultSampler.start(rates.FaultSeed, rates.Faults.PerHour, s.tick)
	}
	if s.emergenciesOn && rates.EmergenciesPerHour > 0 && !s.emergencySampler.live {
		s.emergencySampler.start(rates.EmergencySeed, rates.EmergenciesPerHour, s.tick)
	}
	return nil
}
