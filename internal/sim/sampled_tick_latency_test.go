package sim

import (
	"fmt"
	"os"
	"runtime"
	"slices"
	"testing"
	"time"
)

// sampledTickRates returns the rates of the sampled-tick gate of section 5.1.9
// of the incident stage 7 contract, with the seed.
func sampledTickRates(seed uint64) IncidentRates {
	return IncidentRates{
		Faults:             FaultRate{PerHour: 10, DebrisShare: 0.25, DebrisMeters: 2, Duration: FaultDurationRule{Kind: "fixed", Seconds: 600}},
		EmergenciesPerHour: 10,
		FaultSeed:          [2]uint64{seed, 1},
		EmergencySeed:      [2]uint64{seed, 2},
	}
}

// sampledTick is the measurement of the sampled-tick gate.
type sampledTick struct {
	slowest, fastest time.Duration
	counts           searchCounters
}

// startDueSamplers makes both samplers of s due in the next step.
func startDueSamplers(t *testing.T, s *Simulation, seed uint64) {
	t.Helper()
	if err := s.SetFaults(true, FaultSettings{EvacuationSeconds: 300}); err != nil {
		t.Fatal(err)
	}
	if err := s.StartIncidentSampling(sampledTickRates(seed)); err != nil {
		t.Fatal(err)
	}
	s.faultSampler.nextTick, s.emergencySampler.nextTick = s.tick+1, s.tick+1
}

// measureSampledTick times a whole Step in which a fault event and an emergency
// event start, from copies of one state, with three seeds on which both events
// start and the emergency searches for a station. Another seed does not count,
// because such a step is cheaper.
func measureSampledTick(t *testing.T, fixture choiceTickFixture) sampledTick {
	t.Helper()
	var times []time.Duration
	var result sampledTick
	seed := uint64(0)
	for tries := 0; tries < 200 && len(times) < 3; tries++ {
		seed = nextOccupiedSeed(t, fixture.s, seed+1)
		s := copyWarm(fixture.s)
		if fixture.cold {
			if err := coldState(s); err != nil {
				t.Fatal(err)
			}
		}
		startDueSamplers(t, s, seed)
		before := s.searchCounters
		runtime.GC()
		start := time.Now()
		s.Step()
		elapsed := time.Since(start)
		if s.faultCounters.sampled != 1 || s.emergencyCounters.sampled != 1 {
			t.Fatalf("the step samples %d faults and %d emergencies, want one of each", s.faultCounters.sampled, s.emergencyCounters.sampled)
		}
		if s.faultCounters.skipped != 0 || s.emergencyCounters.skipped != 0 || s.searchCounters.choices == before.choices {
			continue
		}
		times = append(times, elapsed)
		result.counts = subtractCounters(s.searchCounters, before)
	}
	if len(times) < 3 {
		t.Fatalf("%d seeds start both events", len(times))
	}
	result.fastest, result.slowest = slices.Min(times), slices.Max(times)
	return result
}

// TestSampledTickLatency is the sampled-tick latency gate of section 5.1.9 of
// the incident stage 7 contract. A step holds the session lock, so the gate
// measures a whole step with the fault start, the reroute pass, the emergency
// start, and the station search. mise run qualify runs it.
func TestSampledTickLatency(t *testing.T) {
	if os.Getenv("PODSIM_QUALIFY") != "1" {
		t.Skip("the sampled-tick gate runs only in mise run qualify (PODSIM_QUALIFY=1)")
	}
	skipLong(t)
	if raceEnabled {
		t.Skip("the race detector slows the measured code")
	}
	networks := scenarioNetworks(t)
	for _, name := range []string{"London", "rail hub"} {
		for _, warm := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s, warm %t", name, warm), func(t *testing.T) {
				fixture := newChoiceTickFixture(t, choiceTickInput{network: networks[name], warm: warm})
				tick := measureSampledTick(t, fixture)
				t.Logf("slowest %v, fastest %v; searches %+v", tick.slowest, tick.fastest, tick.counts)
				if tick.slowest > choiceTickLimit {
					t.Errorf("the slowest sampled tick takes %v, more than %v", tick.slowest, choiceTickLimit)
				}
			})
		}
	}
}

// TestSampledTickNoCandidateLatency reports the sampled-tick time at the
// fixture of the no-candidate gate, against choiceTickLimit, and does not gate
// it (P23). A fault start replaces the blocked set of the fixture, so that the
// emergency then has candidates. So the report has two parts: a step with only
// the emergency event on the blocked set of the fixture, and a step with only
// the fault event. Their sum is the time of the sampled tick.
func TestSampledTickNoCandidateLatency(t *testing.T) {
	if os.Getenv("PODSIM_QUALIFY") != "1" {
		t.Skip("the full-limit timing report runs only in mise run qualify (PODSIM_QUALIFY=1)")
	}
	skipLong(t)
	if raceEnabled {
		t.Skip("the race detector slows the measured code")
	}
	network := missNetwork(600, 13, 1200)
	prepared, err := PrepareNetwork(network)
	if err != nil {
		t.Fatal(err)
	}
	for _, warm := range []bool{false, true} {
		t.Run(fmt.Sprintf("warm %t", warm), func(t *testing.T) {
			fixture := missFixture(t, choiceTickInput{network: network, prepared: prepared, warm: warm})
			emergency := timeSampledPart(t, fixture, func(s *Simulation) { s.faultSampler.nextTick = MaxCounter })
			fault := timeSampledPart(t, fixture, func(s *Simulation) { s.emergencySampler.nextTick = MaxCounter })
			t.Logf("reported, not gated: emergency %v and fault %v, sum %v, against %v", emergency, fault, emergency+fault, choiceTickLimit)
		})
	}
}

// timeSampledPart returns the time of one step. keep sets one sampler aside.
// The fixture is slow, so the report has one step for each part. The seed is
// the first one on which the emergency has an occupied pod.
func timeSampledPart(t *testing.T, fixture choiceTickFixture, keep func(*Simulation)) time.Duration {
	t.Helper()
	s := copyWarm(fixture.s)
	startDueSamplers(t, s, occupiedSeed(t, fixture.s))
	keep(s)
	runtime.GC()
	start := time.Now()
	s.Step()
	return time.Since(start)
}

// occupiedSeed returns the first seed whose first emergency event draws an
// occupied pod. A sampled emergency draws over all pods, and the fixtures have
// few occupied pods, so the search reads the draw and takes no step.
func occupiedSeed(t *testing.T, base *Simulation) uint64 {
	t.Helper()
	return nextOccupiedSeed(t, base, 1)
}

// nextOccupiedSeed returns the first seed from first on with this property.
func nextOccupiedSeed(t *testing.T, base *Simulation, first uint64) uint64 {
	t.Helper()
	probe := base.Clone()
	if err := probe.SetFaults(true, FaultSettings{}); err != nil {
		t.Fatal(err)
	}
	for seed := first; seed < first+100_000; seed++ {
		sampler := incidentSampler{}
		sampler.start(sampledTickRates(seed).EmergencySeed, 10, probe.tick)
		if probe.vehicles[int(sampler.uniform()*float64(len(probe.vehicles)))].carriesPassengers() {
			return seed
		}
	}
	t.Fatal("no seed draws an occupied pod")
	return 0
}

// TestStopIncidentSamplers checks the stop entry of the soak: no event comes
// due, and each sampler stays live, so its gated counters keep moving.
func TestStopIncidentSamplers(t *testing.T) {
	t.Parallel()
	s := faultLegFleet(t)
	s.emergenciesOn = true
	if err := s.StartIncidentSampling(IncidentRates{Faults: faultRateForTest(), EmergenciesPerHour: 60, FaultSeed: [2]uint64{1, 2}, EmergencySeed: [2]uint64{3, 4}}); err != nil {
		t.Fatal(err)
	}
	s.StopIncidentSamplersForTest()
	if !s.faultSampler.live || !s.emergencySampler.live {
		t.Fatal("the entry stops a sampler")
	}
	if s.faultSampler.nextTick != MaxCounter || s.emergencySampler.nextTick != MaxCounter {
		t.Fatalf("the next ticks are %d and %d", s.faultSampler.nextTick, s.emergencySampler.nextTick)
	}
	v := boardParties(t, s, "s1", "s2")
	v.Pod.WaitReason = BlockedByIncident
	s.faultStage()
	if s.faultCounters.delayedRiderTicks != 2 {
		t.Fatalf("the delayed riders are %d, want 2", s.faultCounters.delayedRiderTicks)
	}
	advance(s, incidentHourTicks/60*2)
	if s.faultCounters.sampled != 0 || s.emergencyCounters.sampled != 0 {
		t.Fatalf("the samplers start events: %+v, %+v", s.faultCounters, s.emergencyCounters)
	}
	if err := s.CheckContract(); err != nil {
		t.Fatal(err)
	}
}

// TestStopIncidentSamplersWithNone checks that the entry does not start a
// sampler, because the stop sets the next tick only of a live sampler.
func TestStopIncidentSamplersWithNone(t *testing.T) {
	t.Parallel()
	s := faultLegFleet(t)
	s.StopIncidentSamplersForTest()
	if err := s.CheckContract(); err != nil {
		t.Fatal(err)
	}
	if s.faultSampler.nextTick != 0 {
		t.Fatalf("the next tick is %d", s.faultSampler.nextTick)
	}
}

// TestFaultTransitionForTest checks that the entry compares the faulted pods.
func TestFaultTransitionForTest(t *testing.T) {
	t.Parallel()
	s := faultLegFleet(t)
	if _, err := s.Fault(FaultRequest{PodID: "02"}); err != nil {
		t.Fatal(err)
	}
	check := s.FaultTransitionForTest()
	advance(s, TicksPerSecond)
	if err := check(); err != nil {
		t.Fatal(err)
	}
	s.findVehicle("02").faultCap++
	if err := check(); err == nil {
		t.Fatal("the check accepts a change of the cap")
	}
}
