package sim

import (
	"errors"
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

// savedMembers returns the state of the stream and the next tick of a live
// sampler, or nil and nil for a sampler that is not live.
func (m *incidentSampler) savedMembers() ([]byte, *int64) {
	if !m.live {
		return nil, nil
	}
	// MarshalBinary of a PCG cannot fail.
	random, _ := m.stream.MarshalBinary()
	return random, new(m.nextTick)
}

// restore makes the sampler live with a saved stream and a saved next tick.
func (m *incidentSampler) restore(random []byte, nextTick int64, perHour float64) error {
	*m = incidentSampler{live: true, nextTick: nextTick, perHour: perHour}
	return m.stream.UnmarshalBinary(random)
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

// errSamplerMissing refuses an ordinary save of a project with a rate and no
// sampler members.
var errSamplerMissing = errors.New("a rate above 0 needs the sampler members")

// checkSavedSampler checks the sampler members of one group of a saved state
// against the project rate (section 4.3 of the incident stage 7 contract).
// Presence does not depend on value, so an empty random and a next tick of 0
// are present. A demo save has no live sampler, so it needs no members. The
// next tick is the terminal value or after the saved tick. The order of the
// checks is part of the format.
func checkSavedSampler(input RestoreStateInput, random []byte, nextTick *int64, perHour float64) error {
	switch {
	case (random == nil) != (nextTick == nil):
		return errors.New("random and nextTick need each other")
	case nextTick != nil && !(perHour > 0):
		return errors.New("the sampler members need a rate above 0")
	case nextTick == nil && perHour > 0 && !demoSave(input):
		return errSamplerMissing
	case nextTick == nil:
		return nil
	}
	if err := new(rand.PCG).UnmarshalBinary(random); err != nil {
		return errors.New("the random source is not valid")
	}
	tick := input.State.Tick
	if next := *nextTick; next != MaxCounter && (next <= tick || next > MaxCounter) {
		return fmt.Errorf("nextTick %d is outside %d to %d", next, tick+1, MaxCounter)
	}
	return nil
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

// sampleEmergencies starts the emergency event that is due. An event uses the
// pod and the party draws and then the draw of the next interval, whatever
// its outcome, so later events do not depend on outcomes. A refused start is
// a skip, and it changes no state.
func (s *Simulation) sampleEmergencies() {
	sampler := &s.emergencySampler
	if !sampler.due(s.tick) {
		return
	}
	pod := sampler.uniform()
	party := sampler.uniform()
	addCount(&s.emergencyCounters.sampled, 1)
	if s.startSampledEmergency(pod, party) != nil {
		addCount(&s.emergencyCounters.skipped, 1)
	}
	sampler.schedule(s.tick)
}

// startSampledEmergency starts the emergency of one event: the pod at the
// index floor(u x n) over all pods, in pod ID order, and the party at the
// index floor(u x k) over its k active riders, in Riders order. A fleet with
// no pod or a pod with no active rider has no target.
func (s *Simulation) startSampledEmergency(pod, party float64) error {
	if len(s.vehicles) == 0 {
		return errUnknownPod
	}
	v := &s.vehicles[int(pod*float64(len(s.vehicles)))]
	target := int(party * float64(v.RidersAboard()))
	for index := range v.Riders {
		if v.Riders[index].Completed {
			continue
		}
		if target == 0 {
			_, err := s.startEmergency(v.Pod.ID, v.Riders[index].ID)
			return err
		}
		target--
	}
	return errNoPassenger
}
