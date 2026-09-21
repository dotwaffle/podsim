package scenarios

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"math/rand/v2"
	"reflect"
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

func TestScale100SampledSafetyAndProgress(t *testing.T) {
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
		checkScaleSafety(t, simulation.Snapshot())
	}
	state := simulation.Snapshot()
	t.Logf("preset=scale100 dense_safety_ticks=%d submitted=%d completed=%d remaining=%d", denseWindowTicks, state.Submitted, state.Completed, state.Submitted-state.Completed)
}

func TestBusyScenarioRepeatability(t *testing.T) {
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

type qualificationInput struct {
	config      project.Config
	schedule    []scheduledRequest
	checkSafety bool
}

func runQualification(t *testing.T, input qualificationInput) qualificationResult {
	t.Helper()
	simulation := newSimulation(t, input.config)
	next := 0
	for tick := range qualificationTicks {
		for next < len(input.schedule) && input.schedule[next].tick == int64(tick) {
			request := input.schedule[next]
			if err := simulation.RequestTrip(request.origin, request.destination); err != nil {
				t.Fatalf("request %s to %s at tick %d: %v", request.origin, request.destination, tick, err)
			}
			next++
		}
		simulation.Step()
		if tick%sim.TicksPerSecond == 0 {
			state := simulation.Snapshot()
			if input.checkSafety {
				checkScaleSafety(t, state)
			}
			if next == len(input.schedule) && state.Completed == len(input.schedule) {
				return qualificationResult{state: state, fingerprint: scheduleFingerprint(input.schedule)}
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

func checkScaleSafety(t *testing.T, state sim.Snapshot) {
	t.Helper()
	for index, first := range state.Vehicles {
		if math.IsNaN(first.Pod.Position.X) || math.IsNaN(first.Pod.Position.Y) || first.Pod.Speed < 0 || first.Pod.Speed > 14.000001 {
			t.Fatalf("invalid pod at tick %d: %+v", state.Tick, first.Pod)
		}
		for _, second := range state.Vehicles[index+1:] {
			gap := math.Hypot(first.Pod.Position.X-second.Pod.Position.X, first.Pod.Position.Y-second.Pod.Position.Y)
			if gap < sim.Clearance-1e-6 {
				t.Fatalf("tick %d: pods %s and %s are %.5f meters apart: %+v %+v", state.Tick, first.Pod.ID, second.Pod.ID, gap, first.Pod, second.Pod)
			}
		}
	}
	for _, berth := range state.Berths {
		occupants := 0
		for _, vehicle := range state.Vehicles {
			if vehicle.Pod.BerthID == berth.ID {
				occupants++
				if berth.Occupant != vehicle.Pod.ID || berth.ReservedBy != vehicle.Pod.ID {
					t.Fatalf("invalid berth state at tick %d: %+v", state.Tick, berth)
				}
			}
		}
		if occupants > 1 {
			t.Fatalf("berth capacity exceeded at tick %d: %+v", state.Tick, berth)
		}
	}
}

// Check every tick, including the final empty moves after passenger delivery.
func TestScale100Station19QueueDrainsSafely(t *testing.T) {
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
		state := simulation.Snapshot()
		checkScaleSafety(t, state)
		if state.Completed != orders || len(state.Pending) != 0 {
			continue
		}
		settled := true
		for _, v := range state.Vehicles {
			if v.Pod.Activity != sim.Idle {
				settled = false
			}
		}
		if settled {
			t.Logf("%d Station 19 orders complete, %d pods idle after %.1fs", orders, len(state.Vehicles), float64(state.Tick)/sim.TicksPerSecond)
			return
		}
	}
	state := simulation.Snapshot()
	t.Fatalf("Station 19 queue did not drain: completed=%d submitted=%d", state.Completed, submitted)
}
