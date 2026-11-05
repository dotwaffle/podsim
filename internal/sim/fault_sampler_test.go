package sim

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// pcgDraws returns the next n draws of a copy of stream, in the form of the
// contract.
func pcgDraws(stream rand.PCG, n int) []float64 {
	draws := make([]float64, n)
	for index := range draws {
		draws[index] = float64(stream.Uint64()<<11>>11) / (1 << 53)
	}
	return draws
}

// streamWith returns the first PCG, by seed, whose first n draws meet accept.
// A test uses it to choose the target of an event without a fixed value of
// the seed.
func streamWith(t *testing.T, n int, accept func(draws []float64) bool) rand.PCG {
	t.Helper()
	for seed := range uint64(100_000) {
		stream := *rand.NewPCG(seed, ^seed)
		if accept(pcgDraws(stream, n)) {
			return stream
		}
	}
	t.Fatal("no seed meets the draws")
	return rand.PCG{}
}

// faultRateForTest is a pod-only rate at 60 events an hour with a fixed
// duration of 600 seconds.
func faultRateForTest() FaultRate {
	return FaultRate{PerHour: 60, DebrisMeters: 2, Duration: FaultDurationRule{Kind: FaultDurationFixed, Seconds: 600}}
}

// startFaultSampler starts the fault sampler of s with the rate and the seed
// words 5 and 6.
func startFaultSampler(t *testing.T, s *Simulation, rate FaultRate) {
	t.Helper()
	if err := s.StartIncidentSampling(IncidentRates{Faults: rate, FaultSeed: [2]uint64{5, 6}}); err != nil {
		t.Fatal(err)
	}
}

// traceFleet returns the five pods of the example network with both switches
// on, for the event trace fixtures. submitTraceTrips gives them demand.
func traceFleet(t *testing.T) *Simulation {
	t.Helper()
	s, err := NewFleet(Example(), []Placement{
		{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"}, {ID: "03", StationID: "market"},
		{ID: "04", StationID: "parking", BerthID: "parking-1"}, {ID: "05", StationID: "parking", BerthID: "parking-2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetFaults(true, FaultSettings{EvacuationSeconds: 300}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEmergencies(true); err != nil {
		t.Fatal(err)
	}
	return s
}

// submitTraceTrips adds a trip every 60 ticks while the queue is short, in a
// fixed rotation of the passenger stations.
func submitTraceTrips(t *testing.T, s *Simulation) {
	t.Helper()
	if s.tick%TicksPerSecond != 0 || len(s.waiting) >= 4 {
		return
	}
	stations := []string{"harbor", "garden", "market"}
	from := int(s.tick/TicksPerSecond) % len(stations)
	if err := s.RequestTrip(stations[from], stations[(from+1)%len(stations)]); err != nil {
		t.Fatal(err)
	}
}

// TestFaultSamplerTrace pins 20 events of the fault sampler: the role and the
// value of each draw, then the target, the segment, the end tick, the outcome,
// and the next tick (section 5.1.8 of the incident stage 7 contract). Faults
// overlap in the run, so the count of free pods differs from the fleet.
//
// Each target is checked against its draws on each CPU type. A run is
// deterministic on one architecture only (section 5.1.4), so the fixture
// shape and the pinned text hold on amd64.
func TestFaultSamplerTrace(t *testing.T) {
	t.Parallel()
	s := traceFleet(t)
	checkFaultsEachTick(t, s)
	rate := FaultRate{PerHour: 60, DebrisShare: 0.5, DebrisMeters: 3,
		Duration: FaultDurationRule{Kind: FaultDurationUniform, MinSeconds: 60, MaxSeconds: 900}}
	startFaultSampler(t, s, rate)
	var trace []string
	starts, skips, overlaps := 0, 0, 0
	for len(trace) < 20 {
		submitTraceTrips(t, s)
		before := s.faultSampler
		started := s.faultCounters.started
		var freePods []string
		for index := range s.vehicles {
			if !s.vehicles[index].faulted {
				freePods = append(freePods, s.vehicles[index].Pod.ID)
			}
		}
		free := len(freePods)
		s.Step()
		if !before.due(s.tick) {
			continue
		}
		draws := pcgDraws(before.stream, 5)
		outcome := "skip"
		if s.faultCounters.started > started {
			starts++
			if free < len(s.vehicles) {
				overlaps++
			}
			record := s.faults[len(s.faults)-1]
			if record.kind == debrisFault {
				if want := laneByRunningSum(s.graph.lengths, draws[1]); record.lane != want {
					t.Fatalf("tick %d: debris on lane %d, want %d for the draw %v", s.tick, record.lane, want, draws[1])
				}
				outcome = fmt.Sprintf("debris %s from=%.6f to=%.6f end=%d", s.network.Lanes[record.lane].ID, record.from, record.to, record.end)
			} else {
				id := s.vehicles[record.pod].Pod.ID
				if want := freePods[int(draws[1]*float64(free))]; id != want {
					t.Fatalf("tick %d: fault on pod %s, want %s for the draw %v over %v", s.tick, id, want, draws[1], freePods)
				}
				outcome = fmt.Sprintf("pod %s end=%d", id, record.end)
			}
		} else {
			skips++
		}
		trace = append(trace, fmt.Sprintf("tick=%d free=%d kind=%.6f target=%.6f position=%.6f duration=%.6f interval=%.6f %s next=%d",
			s.tick, free, draws[0], draws[1], draws[2], draws[3], draws[4], outcome, s.faultSampler.nextTick))
	}
	if s.faultCounters.sampled != 20 || s.faultCounters.skipped != int64(skips) {
		t.Fatalf("counters %+v, want 20 sampled and %d skipped", s.faultCounters, skips)
	}
	if !incidentTraceGolden() {
		return
	}
	if starts == 0 || skips == 0 || overlaps == 0 {
		t.Fatalf("the fixture has %d starts, %d skips, and %d overlaps", starts, skips, overlaps)
	}
	const want = `tick=6587 free=5 kind=0.553975 target=0.893192 position=0.537084 duration=0.789673 interval=0.019215 pod 05 end=49987 next=6657
tick=6657 free=4 kind=0.924038 target=0.195145 position=0.551245 duration=0.464577 interval=0.687805 pod 01 end=33672 next=10848
tick=10848 free=3 kind=0.080000 target=0.090548 position=0.189359 duration=0.696930 interval=0.022296 debris approach-branch from=18.367843 to=21.367843 end=49574 next=10930
tick=10930 free=3 kind=0.411985 target=0.916454 position=0.086347 duration=0.894826 interval=0.964040 debris return from=23.918063 to=26.918063 end=59630 next=22902
tick=22902 free=3 kind=0.855144 target=0.753646 position=0.938026 duration=0.165921 interval=0.977885 pod 04 end=34865 next=36624
tick=36624 free=4 kind=0.257143 target=0.234172 position=0.534402 duration=0.532883 interval=0.593885 debris garden-out from=62.747324 to=65.747324 end=67082 next=39869
tick=39869 free=4 kind=0.131899 target=0.566708 position=0.848016 duration=0.168286 interval=0.894391 debris return-to-parking from=234.900366 to=237.900366 end=51951 next=47962
tick=47962 free=4 kind=0.503362 target=0.307491 position=0.358393 duration=0.691445 interval=0.181130 pod 02 end=86411 next=48682
tick=48682 free=3 kind=0.128632 target=0.650296 position=0.319114 duration=0.099028 interval=0.404094 debris parking-through from=88.394529 to=91.394529 end=57274 next=50546
tick=50546 free=4 kind=0.571862 target=0.250865 position=0.831895 duration=0.394908 interval=0.865815 pod 03 end=74050 next=57777
tick=57777 free=3 kind=0.756820 target=0.265206 position=0.180082 duration=0.491928 interval=0.869852 pod 01 end=86171 next=65118
tick=65118 free=2 kind=0.737910 target=0.347605 position=0.853577 duration=0.324552 interval=0.903906 pod 04 end=85076 next=73551
tick=73551 free=1 kind=0.819879 target=0.163806 position=0.683002 duration=0.178643 interval=0.663118 pod 05 end=86155 next=77468
tick=77468 free=1 kind=0.638251 target=0.300790 position=0.281310 duration=0.310183 interval=0.066742 pod 03 end=96702 next=77717
tick=77717 free=0 kind=0.329926 target=0.896021 position=0.020651 duration=0.009713 interval=0.021877 debris return from=5.720322 to=8.720322 end=81807 next=77797
tick=77797 free=0 kind=0.810364 target=0.596337 position=0.004412 duration=0.137870 interval=0.461090 skip next=80023
tick=80023 free=0 kind=0.391073 target=0.351302 position=0.291167 duration=0.496677 interval=0.573916 debris bypass-merge from=58.513059 to=61.513059 end=108656 next=83095
tick=83095 free=0 kind=0.887578 target=0.336118 position=0.569863 duration=0.025121 interval=0.338099 skip next=84581
tick=84581 free=0 kind=0.558665 target=0.895222 position=0.769671 duration=0.113241 interval=0.050356 skip next=84768
tick=84768 free=0 kind=0.425550 target=0.842589 position=0.490569 duration=0.804680 interval=0.594018 debris parking-out-2 from=81.527288 to=84.527288 end=128924 next=88014`
	if got := strings.Join(trace, "\n"); got != want {
		t.Fatalf("the trace differs:\n%s", got)
	}
}

// withoutSamplerMoves returns state without the members that a skipped event
// moves: the sampler members and the sampler counters.
func withoutSamplerMoves(state SavedState) SavedState {
	if state.Faults != nil {
		faults := *state.Faults
		faults.Random, faults.NextTick, faults.Counters.Sampled, faults.Counters.Skipped = nil, nil, 0, 0
		state.Faults = &faults
	}
	if state.Emergencies != nil {
		emergencies := *state.Emergencies
		emergencies.Random, emergencies.NextTick, emergencies.Counters.Sampled, emergencies.Counters.Skipped = nil, nil, 0, 0
		state.Emergencies = &emergencies
	}
	return state
}

// incidentTraceGolden reports whether the pinned traces hold on this CPU type
// (section 5.1.4 of the incident stage 7 contract).
func incidentTraceGolden() bool {
	return runtime.GOARCH == "amd64"
}

// laneByRunningSum returns the first lane whose running sum of lengths is
// above u x the total length.
func laneByRunningSum(lengths []float64, u float64) int {
	var total, sum float64
	for _, length := range lengths {
		total += length
	}
	for lane, length := range lengths {
		sum += length
		if sum > u*total {
			return lane
		}
	}
	return -1
}

// TestSampledTargetOrder checks each target walk on each CPU type: the pod
// index counts only pods without a fault, in s.vehicles order, and the lane
// is the first in network order whose running sum is above the draw (rule G11
// and section 5.1.1 of the incident stage 7 contract).
func TestSampledTargetOrder(t *testing.T) {
	t.Parallel()
	t.Run("pods", func(t *testing.T) {
		t.Parallel()
		s := traceFleet(t)
		s.vehicles[1].faulted, s.vehicles[3].faulted = true, true
		for _, test := range []struct {
			u    float64
			want string
		}{{0, "01"}, {0.33, "01"}, {0.34, "03"}, {0.66, "03"}, {0.67, "05"}, {0.99, "05"}} {
			got := s.sampledPod(test.u)
			if got == nil {
				t.Errorf("sampledPod(%v) has no pod, want pod %s", test.u, test.want)
			} else if got.Pod.ID != test.want {
				t.Errorf("sampledPod(%v) = pod %s, want pod %s", test.u, got.Pod.ID, test.want)
			}
		}
		for index := range s.vehicles {
			s.vehicles[index].faulted = true
		}
		if got := s.sampledPod(0.5); got != nil {
			t.Errorf("sampledPod with no free pod = %v", got.Pod.ID)
		}
	})
	t.Run("lanes", func(t *testing.T) {
		t.Parallel()
		s := traceFleet(t)
		s.ensureNetworkIndexes()
		lengths := s.graph.lengths
		var total, sum float64
		for _, length := range lengths {
			total += length
		}
		if got := s.sampledLane(0); got != 0 {
			t.Errorf("sampledLane(0) = %d", got)
		}
		for lane, length := range lengths[:len(lengths)-1] {
			sum += length
			u := sum / total
			// The running sum must be above the draw, so the draw at the end
			// of a lane selects the next one.
			if got := s.sampledLane(u); got != lane+1 {
				t.Errorf("sampledLane(%v) = %d, want lane %d", u, got, lane+1)
			}
		}
	})
}

// TestFaultDurationTicks checks each duration rule and the rounding up to
// whole ticks.
func TestFaultDurationTicks(t *testing.T) {
	t.Parallel()
	uniform := FaultDurationRule{Kind: FaultDurationUniform, MinSeconds: 1, MaxSeconds: 2}
	exponential := FaultDurationRule{Kind: FaultDurationExponential, MinSeconds: 10, MaxSeconds: 200, MeanSeconds: 100}
	for _, test := range []struct {
		name string
		rule FaultDurationRule
		u    float64
		want int64
	}{
		{"fixed", FaultDurationRule{Kind: FaultDurationFixed, Seconds: 600}, 0.7, 600 * TicksPerSecond},
		{"fixed at the limit", FaultDurationRule{Kind: FaultDurationFixed, Seconds: maxFaultSeconds}, 0.7, maxFaultSeconds * TicksPerSecond},
		{"uniform low", uniform, 0, TicksPerSecond},
		{"uniform rounds up", uniform, 0.123, int64(math.Ceil(1.123 * TicksPerSecond))},
		{"exponential at its minimum", exponential, 0, 10 * TicksPerSecond},
		{"exponential rounds up", exponential, 0.5, int64(math.Ceil(100 * math.Ln2 * TicksPerSecond))},
		{"exponential at its maximum", exponential, 0.99, 200 * TicksPerSecond},
		{"no kind", FaultDurationRule{}, 0.5, 0},
	} {
		if got := test.rule.ticks(test.u); got != test.want {
			t.Errorf("%s: %d ticks, want %d", test.name, got, test.want)
		}
	}
}

// TestFaultStartInTicks checks the inner starts and rule G3 of the incident
// stage 7 contract: the seconds forms keep the order of their checks, and a
// sampled duration need not be a whole second.
func TestFaultStartInTicks(t *testing.T) {
	t.Parallel()
	s := samplerFleet(t)
	v := s.findVehicle("01")
	if _, err := s.startPodFaultTicks(v, maxFaultSeconds*TicksPerSecond+1); !errors.Is(err, errFaultDuration) {
		t.Fatalf("long pod fault: %v", err)
	}
	if _, err := s.startDebrisTicks(0, 1, 2, -1); !errors.Is(err, errFaultDuration) {
		t.Fatalf("negative debris duration: %v", err)
	}
	if _, err := s.startPodFault(v, 1<<62); !errors.Is(err, errFaultDuration) {
		t.Fatalf("a duration that would overflow: %v", err)
	}
	s.tick = 100
	if _, err := s.startPodFaultTicks(v, 61); err != nil {
		t.Fatal(err)
	}
	if end := s.faults[0].end; end != 161 {
		t.Fatalf("end tick %d, want 161", end)
	}
	// The limit: a tick count that fits and one that does not. The delay is
	// 0, so only the tick count decides.
	s = samplerFleet(t)
	if err := s.SetFaults(true, FaultSettings{}); err != nil {
		t.Fatal(err)
	}
	v = s.findVehicle("01")
	const fits = 2*TicksPerSecond + 1
	s.tick = MaxCounter - fits
	if _, err := s.startPodFaultTicks(v, fits+1); !errors.Is(err, errIncidentLimit) {
		t.Fatalf("a fault past the limit: %v", err)
	}
	if _, err := s.startPodFault(v, 3); !errors.Is(err, errIncidentLimit) {
		t.Fatalf("a fault in seconds past the limit: %v", err)
	}
	if _, err := s.startPodFaultTicks(v, fits); err != nil {
		t.Fatalf("a fault that ends at the limit: %v", err)
	}
	// The delay also decides: its evacuation tick must not pass the limit.
	s = samplerFleet(t)
	if err := s.SetFaults(true, FaultSettings{EvacuationSeconds: 100}); err != nil {
		t.Fatal(err)
	}
	s.tick = MaxCounter - 121
	if _, err := s.startDebris(0, 1, 2, 1); !errors.Is(err, errIncidentLimit) {
		t.Fatalf("debris with a delay past the limit: %v", err)
	}
	// Faults off comes before the duration.
	if err := s.SetFaults(false, FaultSettings{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.startPodFault(s.findVehicle("01"), -1); !errors.Is(err, errFaultsOff) {
		t.Fatalf("faults off and a bad duration: %v", err)
	}
}

// TestFaultSamplerDrawCount checks that an event uses five draws for each
// outcome, so later events do not depend on outcomes, and that a refused
// start changes no state and counts as a skip (sections 5.1.1 and 5.1.7 of
// the incident stage 7 contract).
func TestFaultSamplerDrawCount(t *testing.T) {
	t.Parallel()
	debris := FaultRate{PerHour: 60, DebrisShare: 1, DebrisMeters: 2, Duration: FaultDurationRule{Kind: FaultDurationFixed, Seconds: 600}}
	blockSegment := func(s *Simulation, draws []float64) {
		lane := s.sampledLane(draws[1])
		from := draws[2] * (s.graph.lengths[lane] - 2)
		for _, r := range s.debrisFootprint(lane, from, from+2) {
			s.owners[r] = podResourceOwner("03")
		}
	}
	for _, test := range []struct {
		name    string
		rate    FaultRate
		prepare func(t *testing.T, s *Simulation, draws []float64)
		want    error
	}{
		{"pod accepted", faultRateForTest(), nil, nil},
		{"debris accepted", debris, nil, nil},
		{"pod target not supported", faultRateForTest(), func(_ *testing.T, s *Simulation, _ []float64) {
			for index := range s.vehicles {
				s.vehicles[index].follower = 1
			}
		}, errFaultTarget},
		{"incident limit", faultRateForTest(), func(_ *testing.T, s *Simulation, _ []float64) { s.incidentSerial = MaxCounter }, errIncidentLimit},
		{"tick limit", faultRateForTest(), func(_ *testing.T, s *Simulation, _ []float64) {
			s.tick = MaxCounter - 1000
			s.faultSampler.nextTick = s.tick
		}, errIncidentLimit},
		{"no free pod", faultRateForTest(), func(t *testing.T, s *Simulation, _ []float64) {
			t.Helper()
			for index := range s.vehicles {
				if _, err := s.startPodFault(&s.vehicles[index], 0); err != nil {
					t.Fatal(err)
				}
			}
		}, errUnknownPod},
		{"long duration", FaultRate{PerHour: 60, Duration: FaultDurationRule{Kind: FaultDurationFixed, Seconds: maxFaultSeconds + 1}}, nil, errFaultDuration},
		{"no duration", FaultRate{PerHour: 60}, nil, errFaultDuration},
		{"lane shorter than the debris", FaultRate{PerHour: 60, DebrisShare: 1, DebrisMeters: 1e6, Duration: debris.Duration}, nil, errDebrisSegment},
		{"debris limit", debris, func(_ *testing.T, s *Simulation, _ []float64) {
			for serial := range uint64(maxDebrisFaults) {
				s.faults = append(s.faults, faultRecord{serial: serial + 1, kind: debrisFault, lane: 0, from: 0, to: 1})
			}
			s.incidentSerial = maxDebrisFaults
		}, errDebrisLimit},
		{"debris on a claim", debris, func(_ *testing.T, s *Simulation, draws []float64) { blockSegment(s, draws) }, errDebrisClaim},
		{"debris on debris", debris, func(t *testing.T, s *Simulation, draws []float64) {
			t.Helper()
			lane := s.sampledLane(draws[1])
			from := draws[2] * (s.graph.lengths[lane] - 2)
			if _, err := s.startDebris(lane, from, from+2, 0); err != nil {
				t.Fatal(err)
			}
		}, errDebrisOverlap},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s := samplerFleet(t)
			startFaultSampler(t, s, test.rate)
			s.faultSampler.stream = streamWith(t, 3, func(draws []float64) bool {
				return draws[1] > 0.45 && draws[1] < 0.55 && draws[2] > 0.3 && draws[2] < 0.7
			})
			before := s.faultSampler.stream
			draws := pcgDraws(before, 5)
			s.ensureNetworkIndexes()
			s.faultSampler.nextTick = s.tick
			if test.prepare != nil {
				test.prepare(t, s, draws)
			}
			// The reason of the outcome comes from a clone, because the
			// sampler does not report it.
			err := s.Clone().startSampledFault(draws[0], draws[1], draws[2], draws[3])
			if !errors.Is(err, test.want) {
				t.Fatalf("the start gives %v, want %v", err, test.want)
			}
			state, faults, serial := s.ExportState(), len(s.faults), s.incidentSerial
			started := s.faultCounters.started
			s.sampleFaults()
			after := before
			for range 5 {
				after.Uint64()
			}
			if s.faultSampler.stream != after {
				t.Fatal("the event did not use five draws")
			}
			if want := nextIncidentTick(s.tick, draws[4], test.rate.PerHour); s.faultSampler.nextTick != want {
				t.Fatalf("next tick %d, want %d", s.faultSampler.nextTick, want)
			}
			if s.faultCounters.sampled != 1 {
				t.Fatalf("sampled %d", s.faultCounters.sampled)
			}
			if test.want == nil {
				if s.faultCounters.skipped != 0 || s.faultCounters.started != started+1 || len(s.faults) != faults+1 {
					t.Fatalf("an accepted event has the counters %+v and %d records", s.faultCounters, len(s.faults))
				}
				return
			}
			if s.faultCounters.skipped != 1 || s.faultCounters.started != started {
				t.Fatalf("a refused event has the counters %+v", s.faultCounters)
			}
			if !reflect.DeepEqual(withoutSamplerMoves(s.ExportState()), withoutSamplerMoves(state)) || len(s.faults) != faults || s.incidentSerial != serial {
				t.Fatal("a refused start changed the state")
			}
		})
	}
}

// faultBoundaryFixture returns a fleet in which, in one tick T, pod 01 ends
// its unload, the fault on pod 02 ends, and the fault sampler comes due. The
// sampler picks the free pod at the index floor(u x n) with u above 0.5. In
// the right place, pod 02 is still faulted and pod 01 has alighted, so
// the only target is pod 01 at rest with no rider.
func faultBoundaryFixture(t *testing.T) (s *Simulation, unloading, cleared *vehicle, due int64) {
	t.Helper()
	s = faultLegFleet(t)
	unloading, cleared = boardParties(t, s, "s1"), s.findVehicle("02")
	if _, err := s.startPodFault(cleared, 0); err != nil {
		t.Fatal(err)
	}
	rehearsal := s.Clone()
	stepUntil(t, rehearsal, "the unload", func() bool { return rehearsal.findVehicle("01").RidersAboard() == 0 })
	due = rehearsal.tick
	s.faults[0].end = due
	startFaultSampler(t, s, faultRateForTest())
	s.faultSampler.nextTick = due
	s.faultSampler.stream = streamWith(t, 2, func(draws []float64) bool { return draws[1] > 0.5 })
	return s, unloading, cleared, due
}

// TestFaultSamplerBoundary checks the place of the fault sampler in its tick
// (section 5.1.8 of the incident stage 7 contract): after the unloading loop
// and before the clears.
func TestFaultSamplerBoundary(t *testing.T) {
	t.Parallel()
	s, unloading, cleared, due := faultBoundaryFixture(t)
	checkFaultsEachTick(t, s)
	for s.tick < due {
		s.Step()
	}
	if s.faultCounters.sampled != 1 || s.faultCounters.skipped != 0 {
		t.Fatalf("counters %+v", s.faultCounters)
	}
	if !unloading.faulted || cleared.faulted {
		t.Fatalf("pod 01 faulted %v, pod 02 faulted %v, want the fault on pod 01", unloading.faulted, cleared.faulted)
	}
	if unloading.RidersAboard() != 0 || unloading.Pod.Activity != Idle {
		t.Fatalf("the unload did not end before the fault: %d riders, activity %s", unloading.RidersAboard(), unloading.Pod.Activity)
	}
}

// TestFaultSamplerTerminal runs a tick to the limit with the terminal value:
// no event starts, and F13 holds (the terminal rule of section 5.1.1 of the
// incident stage 7 contract). A fault near the limit is a skip.
func TestFaultSamplerTerminal(t *testing.T) {
	t.Parallel()
	s := samplerFleet(t)
	checkFaultsEachTick(t, s)
	startFaultSampler(t, s, faultRateForTest())
	s.tick, s.faultSampler.nextTick = MaxCounter-1, MaxCounter
	stream := s.faultSampler.stream
	s.Step()
	if s.tick != MaxCounter || s.faultCounters.sampled != 0 || s.faultSampler.stream != stream || s.faultSampler.nextTick != MaxCounter {
		t.Fatalf("tick %d, counters %+v, sampler %+v", s.tick, s.faultCounters, s.faultSampler)
	}
	if err := s.CheckContract(); err != nil {
		t.Fatal(err)
	}
	checkTerminalRestore(t, s, IncidentRates{Faults: faultRateForTest()})
	s = samplerFleet(t)
	checkFaultsEachTick(t, s)
	startFaultSampler(t, s, faultRateForTest())
	s.tick = MaxCounter - 1000
	s.faultSampler.nextTick = s.tick + 1
	s.Step()
	if s.faultCounters.sampled != 1 || s.faultCounters.skipped != 1 || len(s.faults) != 0 {
		t.Fatalf("a fault near the limit: counters %+v, %d records", s.faultCounters, len(s.faults))
	}
	if next := s.faultSampler.nextTick; next != MaxCounter && next <= s.tick {
		t.Fatalf("next tick %d at tick %d", next, s.tick)
	}
}

// checkTerminalRestore saves s at the tick limit with the terminal value of
// each live sampler, and restores it in each tier. The restore keeps the
// terminal value, which the range check accepts at each tick, and the state
// contract holds.
func checkTerminalRestore(t *testing.T, s *Simulation, rates IncidentRates) {
	t.Helper()
	for _, logical := range []bool{false, true} {
		input := samplerRestoreInput(s, s.ExportState(), rates)
		input.LogicalOnly = logical
		restored, _, err := RestoreState(input)
		if err != nil {
			t.Fatalf("logical %t: %v", logical, err)
		}
		if restored.tick != MaxCounter || restored.faultSampler != s.faultSampler || restored.emergencySampler != s.emergencySampler {
			t.Fatalf("logical %t: tick %d, samplers %+v and %+v", logical, restored.tick, restored.faultSampler, restored.emergencySampler)
		}
		if err := restored.CheckContract(); err != nil {
			t.Fatalf("logical %t: %v", logical, err)
		}
	}
}

// TestFaultCountersGate checks the counter gate of section 5.1.5 of the
// incident stage 7 contract and the meaning of each gated counter. With no
// sampler, a manual fault, its clear, and a wait change neither counter. With
// a live sampler, a manual record counts as a sampled one does.
func TestFaultCountersGate(t *testing.T) {
	t.Parallel()
	run := func(t *testing.T, live bool, counters faultCounters) (*Simulation, int64) {
		t.Helper()
		s := faultLegFleet(t)
		if live {
			startFaultSampler(t, s, faultRateForTest())
			s.faultSampler.nextTick = MaxCounter
		}
		s.faultCounters = counters
		// Two riders wait for an incident in one fault stage.
		v := boardParties(t, s, "s1", "s2")
		v.Pod.WaitReason = BlockedByIncident
		before := s.faultCounters.delayedRiderTicks
		s.faultStage()
		delayed := s.faultCounters.delayedRiderTicks - before
		// A command clears one fault, and a duration ends the other.
		id, err := s.Fault(FaultRequest{PodID: "02"})
		if err != nil {
			t.Fatal(err)
		}
		advance(s, 100)
		if err := s.ClearFault(id); err != nil {
			t.Fatal(err)
		}
		if _, err := s.startPodFault(s.findVehicle("02"), 1); err != nil {
			t.Fatal(err)
		}
		advance(s, TicksPerSecond)
		if len(s.faults) != 0 {
			t.Fatalf("%d records stay", len(s.faults))
		}
		return s, delayed
	}
	t.Run("no sampler", func(t *testing.T) {
		t.Parallel()
		s, delayed := run(t, false, faultCounters{})
		if c := s.faultCounters; c.clearedTicks != 0 || c.delayedRiderTicks != 0 || c.sampled != 0 || c.skipped != 0 || delayed != 0 {
			t.Fatalf("counters %+v", c)
		}
		if s.faultCounters.cleared != 2 || s.faultCounters.faultWaitTicks == 0 {
			t.Fatalf("the fixture has the counters %+v", s.faultCounters)
		}
	})
	t.Run("live sampler", func(t *testing.T) {
		t.Parallel()
		s, delayed := run(t, true, faultCounters{})
		if c := s.faultCounters; c.clearedTicks != 100+TicksPerSecond || delayed != 2 {
			t.Fatalf("counters %+v, %d delayed riders in the stage", c, delayed)
		}
		if s.faultCounters.sampled != 0 {
			t.Fatalf("a manual fault changed the sampled count: %+v", s.faultCounters)
		}
	})
	t.Run("saturation", func(t *testing.T) {
		t.Parallel()
		s, _ := run(t, true, faultCounters{clearedTicks: MaxCounter - 1, delayedRiderTicks: MaxCounter})
		if c := s.faultCounters; c.clearedTicks != MaxCounter || c.delayedRiderTicks != MaxCounter {
			t.Fatalf("counters %+v", c)
		}
	})
}

// TestFaultSamplerCounterInvariant checks the counter clause of F13.
func TestFaultSamplerCounterInvariant(t *testing.T) {
	t.Parallel()
	s := samplerFleet(t)
	s.faultCounters.skipped = 1
	if err := s.CheckContract(); err == nil || !strings.HasPrefix(err.Error(), "F13: 1 events skipped") {
		t.Fatalf("error %v", err)
	}
	s.faultCounters.sampled = 1
	if err := s.CheckContract(); err != nil {
		t.Fatal(err)
	}
}

// TestFaultSamplerReplay checks that a clone replays the events of the
// original, and that a reset and a new start replay the same first events
// from tick 0 (section 5.1.4 of the incident stage 7 contract).
func TestFaultSamplerReplay(t *testing.T) {
	t.Parallel()
	s := traceFleet(t)
	rate := FaultRate{PerHour: 60, DebrisShare: 0.5, DebrisMeters: 3, Duration: FaultDurationRule{Kind: FaultDurationFixed, Seconds: 30}}
	startFaultSampler(t, s, rate)
	first := s.faultSampler
	for s.faultCounters.sampled < 2 {
		s.Step()
	}
	clone := s.Clone()
	for range 10_000 {
		s.Step()
		clone.Step()
		if s.faultSampler != clone.faultSampler || s.faultCounters != clone.faultCounters {
			t.Fatalf("tick %d: sampler %+v, clone %+v", s.tick, s.faultSampler, clone.faultSampler)
		}
	}
	if s.faultCounters.sampled == 2 {
		t.Fatal("no event came due after the clone")
	}
	s.Reset()
	if s.faultSampler.live {
		t.Fatal("the reset kept the sampler")
	}
	startFaultSampler(t, s, rate)
	if s.faultSampler != first {
		t.Fatalf("a start after the reset has %+v, want %+v", s.faultSampler, first)
	}
}
