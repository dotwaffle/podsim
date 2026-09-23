package scenarios

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/rand/v2"
	"reflect"
	"slices"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

const qualificationTicks = 20 * 60 * sim.TicksPerSecond

type scheduledRequest struct {
	tick        int64
	origin      string
	destination string
}

type qualificationResult struct {
	state       sim.Snapshot
	fingerprint string
}

var (
	benchmarkSnapshot    sim.Snapshot
	benchmarkObservation sim.SafetyObservation
)

func TestScale100SampledSafetyAndProgress(t *testing.T) {
	t.Parallel()
	config := Scale100()
	const (
		seed         = uint64(20260921)
		requestCount = 20
		requestEvery = 10 * sim.TicksPerSecond
	)
	schedule := requestSchedule(config, scheduleParameters{seed: seed, count: requestCount, interval: requestEvery})
	first := runQualification(t, qualificationInput{config: config, schedule: schedule, checkSafety: true})
	if first.state.Completed != requestCount || len(first.state.Pending) != 0 {
		t.Fatalf("feasible demand did not finish: completed=%d remaining=%d", first.state.Completed, first.state.Submitted-first.state.Completed)
	}
	t.Logf("preset=scale100 seed=%d interval_ticks=%d requests=%d schedule_sha256=%s safety_sample_ticks=%d completed=%d remaining=%d wait_average_seconds=%.3f wait_max_seconds=%.3f ticks=%d",
		seed, requestEvery, requestCount, first.fingerprint, sim.TicksPerSecond, first.state.Completed,
		first.state.Submitted-first.state.Completed, first.state.Wait.AverageSeconds,
		first.state.Wait.MaxSeconds, first.state.Tick)
}

func TestScale100DenseSafetyWindow(t *testing.T) {
	t.Parallel()
	config := Scale100()
	simulation := newSimulation(t, config)
	passenger := project.PassengerStations(config.Network)
	for requestIndex := range 50 {
		origin := passenger[requestIndex%len(passenger)].ID
		destination := passenger[(requestIndex%len(passenger)+2)%len(passenger)].ID
		if err := simulation.RequestTrip(origin, destination); err != nil {
			t.Fatal(err)
		}
	}
	const denseWindowTicks = 180 * sim.TicksPerSecond
	for range denseWindowTicks {
		simulation.Step()
		checkScaleSafety(t, simulation.SafetyObservation())
	}
	state := simulation.Snapshot()
	t.Logf("preset=scale100 dense_safety_ticks=%d submitted=%d completed=%d remaining=%d", denseWindowTicks, state.Submitted, state.Completed, state.Submitted-state.Completed)
}

func TestBusyScenarioRepeatability(t *testing.T) {
	t.Parallel()
	config := Busy()
	const (
		seed         = uint64(20260923)
		requestCount = 10
		requestEvery = 10 * sim.TicksPerSecond
	)
	schedule := requestSchedule(config, scheduleParameters{seed: seed, count: requestCount, interval: requestEvery})
	first := runQualification(t, qualificationInput{config: config, schedule: schedule})
	second := runQualification(t, qualificationInput{config: config, schedule: schedule})
	if !reflect.DeepEqual(first.state, second.state) {
		t.Fatal("identical scenario and request schedule produced different results")
	}
	if first.state.Completed != requestCount {
		t.Fatalf("repeatability fixture did not finish: completed=%d remaining=%d", first.state.Completed, first.state.Submitted-first.state.Completed)
	}
	t.Logf("preset=busy seed=%d interval_ticks=%d requests=%d schedule_sha256=%s completed=%d remaining=%d wait_average_seconds=%.3f wait_max_seconds=%.3f ticks=%d",
		seed, requestEvery, requestCount, first.fingerprint, first.state.Completed,
		first.state.Submitted-first.state.Completed, first.state.Wait.AverageSeconds,
		first.state.Wait.MaxSeconds, first.state.Tick)
}

func TestParkingConstrainedSafetyAndAccounting(t *testing.T) {
	t.Parallel()
	config := ParkingConstrained()
	const (
		seed         = uint64(20260922)
		requestCount = 40
		requestEvery = 3 * sim.TicksPerSecond
	)
	schedule := requestSchedule(config, scheduleParameters{seed: seed, count: requestCount, interval: requestEvery})
	result := runQualification(t, qualificationInput{config: config, schedule: schedule, checkSafety: true})
	if result.state.Completed > result.state.Submitted {
		t.Fatalf("completed %d of %d submitted requests", result.state.Completed, result.state.Submitted)
	}
	t.Logf("preset=parking-constrained seed=%d interval_ticks=%d requests=%d schedule_sha256=%s completed=%d remaining=%d wait_average_seconds=%.3f wait_max_seconds=%.3f ticks=%d",
		seed, requestEvery, requestCount, result.fingerprint, result.state.Completed,
		result.state.Submitted-result.state.Completed, result.state.Wait.AverageSeconds,
		result.state.Wait.MaxSeconds, result.state.Tick)
}

func BenchmarkScale100StepActiveTraffic(b *testing.B) {
	config := scale100Ring()
	passenger := project.PassengerStations(config.Network)
	simulation := newSimulation(b, config)
	requestIndex := 0
	for range 50 {
		origin := passenger[requestIndex%len(passenger)].ID
		destination := passenger[(requestIndex+len(passenger)/2)%len(passenger)].ID
		if err := simulation.RequestTrip(origin, destination); err != nil {
			b.Fatal(err)
		}
		requestIndex++
	}
	b.ReportAllocs()
	b.ResetTimer()
	tick := 0
	for b.Loop() {
		if tick > 0 && tick%(30*60*sim.TicksPerSecond) == 0 {
			simulation = newSimulation(b, config)
			tick = 0
		}
		if tick > 0 && tick%(10*sim.TicksPerSecond) == 0 {
			origin := passenger[requestIndex%len(passenger)].ID
			destination := passenger[(requestIndex*7+3)%len(passenger)].ID
			if origin == destination {
				destination = passenger[(requestIndex*7+4)%len(passenger)].ID
			}
			if err := simulation.RequestTrip(origin, destination); err != nil {
				b.Fatal(err)
			}
			requestIndex++
		}
		simulation.Step()
		tick++
	}
}

func BenchmarkScale100SafetyState(b *testing.B) {
	config := Scale100()

	b.Run("snapshot", func(b *testing.B) {
		simulation := activeSafetyBenchmarkSimulation(b, config)
		b.ReportAllocs()
		for b.Loop() {
			benchmarkSnapshot = simulation.Snapshot()
		}
	})

	b.Run("observation", func(b *testing.B) {
		simulation := activeSafetyBenchmarkSimulation(b, config)
		b.ReportAllocs()
		for b.Loop() {
			benchmarkObservation = simulation.SafetyObservation()
		}
	})
}

func activeSafetyBenchmarkSimulation(b *testing.B, config project.Config) *sim.Simulation {
	b.Helper()
	simulation := newSimulation(b, config)
	passenger := project.PassengerStations(config.Network)
	for requestIndex := range 50 {
		origin := passenger[requestIndex%len(passenger)].ID
		destination := passenger[(requestIndex%len(passenger)+2)%len(passenger)].ID
		if err := simulation.RequestTrip(origin, destination); err != nil {
			b.Fatal(err)
		}
	}
	state := simulation.Snapshot()
	activeRoutes := 0
	for _, vehicle := range state.Vehicles {
		if len(vehicle.Route) > 0 {
			activeRoutes++
		}
	}
	if activeRoutes == 0 {
		b.Fatal("benchmark has no active routes")
	}
	return simulation
}

type qualificationInput struct {
	config      project.Config
	schedule    []scheduledRequest
	checkSafety bool
	ticks       int
}

func runQualification(t *testing.T, input qualificationInput) qualificationResult {
	t.Helper()
	simulation := newSimulation(t, input.config)
	next := 0
	ticks := input.ticks
	if ticks == 0 {
		ticks = qualificationTicks
	}
	for tick := range ticks {
		for next < len(input.schedule) && input.schedule[next].tick == int64(tick) {
			request := input.schedule[next]
			if err := simulation.RequestTrip(request.origin, request.destination); err != nil {
				t.Fatalf("request %s to %s at tick %d: %v", request.origin, request.destination, tick, err)
			}
			next++
		}
		simulation.Step()
		if tick%sim.TicksPerSecond == 0 {
			observation := simulation.SafetyObservation()
			if input.checkSafety {
				checkScaleSafety(t, observation)
			}
			if next == len(input.schedule) && observation.Completed == len(input.schedule) {
				return qualificationResult{state: simulation.Snapshot(), fingerprint: scheduleFingerprint(input.schedule)}
			}
		}
	}
	return qualificationResult{state: simulation.Snapshot(), fingerprint: scheduleFingerprint(input.schedule)}
}

func newSimulation(tb testing.TB, config project.Config) *sim.Simulation {
	tb.Helper()
	simulation, err := sim.NewFleet(config.Network, config.Fleet)
	if err != nil {
		tb.Fatal(err)
	}
	return simulation
}

type scheduleParameters struct {
	seed            uint64
	count, interval int
}

func requestSchedule(config project.Config, parameters scheduleParameters) []scheduledRequest {
	passenger := project.PassengerStations(config.Network)
	rng := rand.New(rand.NewPCG(parameters.seed, ^parameters.seed))
	schedule := make([]scheduledRequest, parameters.count)
	for index := range schedule {
		originIndex := rng.IntN(len(passenger))
		destinationIndex := (originIndex + 1 + rng.IntN(min(3, len(passenger)-1))) % len(passenger)
		schedule[index] = scheduledRequest{
			tick: int64((index + 1) * parameters.interval), origin: passenger[originIndex].ID,
			destination: passenger[destinationIndex].ID,
		}
	}
	return schedule
}

func scheduleFingerprint(schedule []scheduledRequest) string {
	hash := sha256.New()
	for _, request := range schedule {
		fmt.Fprintf(hash, "%d:%s:%s\n", request.tick, request.origin, request.destination)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// maxObservedSpeed is the lane speed limit of the scenario presets plus a
// rounding tolerance. SafetyObservation.Check does not apply a speed limit,
// because each lane has its own limit.
const maxObservedSpeed = speedLimit + 1e-6

// checkScaleSafety fails the test when state is not safe or when a pod moves
// faster than the preset speed limit. It returns the smallest gap between two
// pods on one plane.
func checkScaleSafety(tb testing.TB, state sim.SafetyObservation) float64 {
	tb.Helper()
	for _, pod := range state.Pods {
		if pod.Speed > maxObservedSpeed {
			tb.Fatalf("pod faster than the speed limit at tick %d: %+v", state.Tick, pod)
		}
	}
	gap, err := state.Check()
	if separation, ok := errors.AsType[*sim.SeparationError](err); ok {
		tb.Fatalf("%v: %+v %+v", err, observedPod(state, separation.First), observedPod(state, separation.Second))
	}
	if err != nil {
		tb.Fatal(err)
	}
	return gap
}

func observedPod(state sim.SafetyObservation, id string) sim.Pod {
	index := slices.IndexFunc(state.Pods, func(pod sim.Pod) bool { return pod.ID == id })
	return state.Pods[index]
}

// Check every tick, including the final empty moves after passenger delivery.
func TestScale100Station19QueueDrainsSafely(t *testing.T) {
	t.Parallel()
	config := Scale100()
	simulation := newSimulation(t, config)
	var origins []string
	for _, station := range config.Network.Stations {
		if !station.ParkingOnly && station.ID != "station-19" {
			origins = append(origins, station.ID)
		}
	}
	const orders = 40
	submitted := 0
	for tick := range 2400 * sim.TicksPerSecond {
		if submitted < orders && tick%(15*sim.TicksPerSecond) == 0 {
			if err := simulation.RequestTrip(origins[submitted%len(origins)], "station-19"); err != nil {
				t.Fatal(err)
			}
			submitted++
		}
		simulation.Step()
		state := simulation.SafetyObservation()
		checkScaleSafety(t, state)
		if state.Completed != orders || state.Pending != 0 {
			continue
		}
		settled := true
		for _, pod := range state.Pods {
			if pod.Activity != sim.Idle {
				settled = false
			}
		}
		if settled {
			final := simulation.Snapshot()
			t.Logf("%d Station 19 orders complete, %d pods idle after %.1fs", orders, len(final.Vehicles), float64(final.Tick)/sim.TicksPerSecond)
			return
		}
	}
	state := simulation.Snapshot()
	t.Fatalf("Station 19 queue did not drain: completed=%d submitted=%d", state.Completed, submitted)
}
