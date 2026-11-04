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

// ticks returns the duration of a fault for the draw u, rounded up to whole
// ticks. A rule with no kind gives 0.
func (r FaultDurationRule) ticks(u float64) int64 {
	var seconds float64
	low, high := float64(r.MinSeconds), float64(r.MaxSeconds)
	switch r.Kind {
	case FaultDurationFixed:
		seconds = float64(r.Seconds)
	case FaultDurationUniform:
		seconds = low + u*(high-low)
	case FaultDurationExponential:
		seconds = min(high, max(low, -math.Log(1-u)*float64(r.MeanSeconds)))
	}
	return int64(math.Ceil(seconds * TicksPerSecond))
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

// sampleFaults starts the fault event that is due. An event uses four draws
// and then the draw of the next interval, whatever its kind and its outcome,
// so later events do not depend on outcomes. A refused start is a skip, and
// it changes no state, because each start checks before its first write.
func (s *Simulation) sampleFaults() {
	sampler := &s.faultSampler
	if !sampler.due(s.tick) {
		return
	}
	kind := sampler.uniform()
	target := sampler.uniform()
	position := sampler.uniform()
	duration := sampler.uniform()
	countFault(&s.faultCounters.sampled)
	if s.startSampledFault(kind, target, position, duration) != nil {
		countFault(&s.faultCounters.skipped)
	}
	sampler.schedule(s.tick)
}

// startSampledFault starts the fault of one event, from its four draws:
// debris when kind is below the debris share, or else a pod fault. A sampled
// fault always has an end, so a duration of 0 ticks is refused. A segment on
// a lane shorter than the debris has a negative start, and the debris start
// refuses it.
func (s *Simulation) startSampledFault(kind, target, position, duration float64) error {
	rate := &s.faultRate
	ticks := rate.Duration.ticks(duration)
	if ticks < 1 {
		return errFaultDuration
	}
	if kind >= rate.DebrisShare {
		_, err := s.startPodFaultTicks(s.sampledPod(target), ticks)
		return err
	}
	lane := s.sampledLane(target)
	if lane < 0 {
		return errUnknownLane
	}
	from := position * (s.graph.lengths[lane] - rate.DebrisMeters)
	_, err := s.startDebrisTicks(lane, from, from+rate.DebrisMeters, ticks)
	return err
}

// sampledPod returns the pod at the index floor(u x n) over the n pods
// without a fault, or nil with no such pod. s.vehicles is in pod ID order
// (rule G11 of the incident stage 7 contract).
func (s *Simulation) sampledPod(u float64) *vehicle {
	free := 0
	for index := range s.vehicles {
		if !s.vehicles[index].faulted {
			free++
		}
	}
	target := int(u * float64(free))
	for index := range s.vehicles {
		v := &s.vehicles[index]
		if v.faulted {
			continue
		}
		if target == 0 {
			return v
		}
		target--
	}
	return nil
}

// sampledLane returns the index of the first lane in network order whose
// running sum of lengths is above u x the total length, or -1 with no lane.
func (s *Simulation) sampledLane(u float64) int {
	s.ensureNetworkIndexes()
	var total float64
	for _, length := range s.graph.lengths {
		total += length
	}
	var sum float64
	for lane, length := range s.graph.lengths {
		sum += length
		if sum > u*total {
			return lane
		}
	}
	return -1
}
