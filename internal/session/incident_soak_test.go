package session

import (
	"cmp"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
)

// The values of the soak of section 5.1.9 of the incident stage 7 contract.
// The soak sets them itself, and it does not read the rail-hub-incidents
// preset, which has faults of 120 seconds. The status line under decision 7
// of the contract keeps the soak at 10 an hour and 600 seconds. Section 5.1.9
// names no duration, and the 600 seconds come from that line.
const (
	soakPerHour       = 10.0
	soakFaultSeconds  = 600
	soakDebrisShare   = 0.25
	soakDebrisMeters  = 2.0
	soakPlatoonLimit  = 4
	soakTicksPerHour  = 3600 * sim.TicksPerSecond
	soakClearTicks    = 30 * 60 * sim.TicksPerSecond
	soakEmergencyTime = 10 * 60 * sim.TicksPerSecond
	soakReplayTicks   = 600
	soakViewTicks     = sim.TicksPerSecond
	soakRateWindow    = 5 * 60 * sim.TicksPerSecond
)

// soakArm is one column of the soak matrix of section 5.1.9.
type soakArm struct {
	name string
	// faults and emergencies give the sampler rates. commands adds the
	// command driver at the same rates for each kind with a rate (A2).
	faults, emergencies, commands bool
	platoons                      bool
}

// soakArms holds the arms of section 5.1.9. "ci" is the CI soak, with the
// sampler only.
var soakArms = map[string]soakArm{
	"incidents":          {name: "incidents", faults: true, emergencies: true, commands: true},
	"emergencies":        {name: "emergencies", emergencies: true, commands: true},
	"platoons":           {name: "platoons", faults: true, emergencies: true, commands: true, platoons: true},
	"reference":          {name: "reference"},
	"reference-platoons": {name: "reference-platoons", platoons: true},
	"ci":                 {name: "ci", faults: true, emergencies: true},
}

// soakPreset returns the preset of the name, with demand on.
func soakPreset(t *testing.T, name string) project.Config {
	t.Helper()
	var config project.Config
	switch name {
	case "rail-hub":
		config = scenarios.RailHub()
	case "london-central":
		config = scenarios.LondonCentral()
	case "fleet-300", "fleet-1000":
		// No preset has the fleet of the worst-case rows of section 5.1.9.
		pods, err := strconv.Atoi(strings.TrimPrefix(name, "fleet-"))
		if err != nil {
			t.Fatal(err)
		}
		built, err := scenarios.Config(scenarios.Parameters{Name: name, Stations: pods * 2 / 25, Pods: pods, PassengerBerths: 13, ParkingBerths: 20, InitialParkingPods: 20, DemandPerMinute: 12, DemandSeed: 41})
		if err != nil {
			t.Fatal(err)
		}
		config = built
	default:
		t.Fatalf("unknown soak preset %q", name)
	}
	config.Demand.Enabled = true
	return config
}

// soakProject returns the project of one run. The seed selects the demand and
// the sampler streams.
func soakProject(t *testing.T, preset string, arm soakArm, seed uint64) project.Config {
	t.Helper()
	config := soakPreset(t, preset)
	config.Demand.Seed += seed
	if arm.faults {
		config.Faults = &project.FaultConfig{PerHour: new(soakPerHour), DebrisShare: new(soakDebrisShare), DebrisMeters: new(soakDebrisMeters),
			Duration: &project.FaultDuration{Kind: "fixed", Seconds: new(soakFaultSeconds)}}
	}
	if arm.emergencies {
		config.Emergencies = &project.EmergencyConfig{PerHour: new(soakPerHour)}
	}
	if arm.platoons {
		config.PlatoonLimit = soakPlatoonLimit
	}
	if err := project.Validate(config); err != nil {
		t.Fatal(err)
	}
	return config
}

// soakInput selects one run.
type soakInput struct {
	preset string
	arm    soakArm
	seed   uint64
	hours  float64
	// checks runs the every-tick checks, the persistence checks, and the
	// progress checks. A timed run or a reference run has none.
	checks bool
	// queue is the waiting queue range of the reference run, or nil.
	queue *soakRange
	// timed records the time of each step.
	timed bool
	// settle is the length of the Progress window after the clear, or 0 for
	// the 30 minutes of the gate. Only a timing run shortens it.
	settle int64
	// checkFrom is the tick at which the checks start. Only a timing run sets
	// it, to reach the full queue before it measures the checks.
	checkFrom int64
}

// soakRange is the range of a queue size.
type soakRange struct{ low, high int }

func (r *soakRange) add(value int) {
	r.low, r.high = min(r.low, value), max(r.high, value)
}

// soakResult is the result of one run. A gate that fails stops the test, so
// the result holds only numbers to report.
type soakResult struct {
	ticks                                int64
	wall                                 time.Duration
	cpu                                  float64
	queue                                soakRange
	state                                sim.Snapshot
	commands, refused                    int
	saves, demoted, rewinds, railRecords int
	emergencies, emergencyLate           int
	settleTicks                          int64
	misses                               []string
	steps                                []time.Duration
	// rates holds the ticks for each CPU second in each simulated 5 minutes
	// of the run before the clear, to see the drift of the rate.
	rates []int
}

// soakDriver holds the state of one run.
type soakDriver struct {
	t      *testing.T
	input  soakInput
	config project.Config
	s      *Session
	client *testClient
	rng    *rand.Rand
	// checks is true while the driver runs the checks.
	checks bool
	// transition compares the faulted pods of the current simulation. A
	// rewind replaces the simulation, and then this check.
	transition func() error
	// nextFault, nextEmergency, nextSave, and nextRewind are the ticks of the
	// next driver events. clears holds the command faults, by clear tick.
	nextFault, nextEmergency, nextSave, nextRewind int64
	clears                                         []soakClear
	// open holds the first tick at which the driver saw each record.
	open   map[string]sim.EmergencyView
	result soakResult
}

type soakClear struct {
	id   string
	tick int64
}

// soakInterval returns a random interval for the mean rate of the soak.
func soakInterval(rng *rand.Rand) int64 {
	return max(1, int64(rng.ExpFloat64()*soakTicksPerHour/soakPerHour))
}

func newSoakDriver(t *testing.T, input soakInput) *soakDriver {
	t.Helper()
	config := soakProject(t, input.preset, input.arm, input.seed)
	s, err := NewWithProject(config, WithLogger(slog.New(slog.DiscardHandler)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	rng := rand.New(rand.NewPCG(input.seed, 0x50414b))
	d := &soakDriver{t: t, input: input, config: config, s: s, client: newTestClient(s, "soak"), rng: rng, open: map[string]sim.EmergencyView{},
		nextFault: math.MaxInt64, nextEmergency: math.MaxInt64, nextSave: math.MaxInt64, nextRewind: math.MaxInt64}
	d.result.queue = soakRange{low: math.MaxInt, high: 0}
	if input.arm.commands {
		if input.arm.faults {
			d.nextFault = soakInterval(rng)
		}
		if input.arm.emergencies {
			d.nextEmergency = soakInterval(rng)
		}
	}
	d.checks = input.checks && input.checkFrom == 0
	if d.checks {
		d.startChecks()
	}
	return d
}

// startChecks starts the checks at the current tick.
func (d *soakDriver) startChecks() {
	tick := d.s.simulation.Tick()
	d.checks = true
	d.nextSave = tick + d.rng.Int64N(soakTicksPerHour) + 1
	d.nextRewind = tick + d.rng.Int64N(soakTicksPerHour) + 1
	d.transition = d.s.simulation.FaultTransitionForTest()
}

// failf stops the test with the run and the tick.
func (d *soakDriver) failf(format string, args ...any) {
	d.t.Helper()
	d.t.Fatalf("%s %s seed %d tick %d: %s", d.input.preset, d.input.arm.name, d.input.seed, d.s.simulation.Tick(), fmt.Sprintf(format, args...))
}

// check runs the checks of each boundary: the Safety and Contract gates, with
// each invariant in CheckContract, the transition rules, and Conservation.
func (d *soakDriver) check() {
	d.t.Helper()
	if !d.checks {
		return
	}
	simulation := d.s.simulation
	if err := simulation.CheckContract(); err != nil {
		d.failf("the contract: %v", err)
	}
	if err := d.transition(); err != nil {
		d.failf("the transition rules: %v", err)
	}
	if _, err := simulation.SafetyObservation().Check(); err != nil {
		d.failf("the safety observation: %v", err)
	}
	state := simulation.ExportState()
	queued, aboard := len(state.Waiting), 0
	for index := range state.Pods {
		for rider := range state.Pods[index].Riders {
			if !state.Pods[index].Riders[rider].Completed {
				aboard++
			}
		}
	}
	if state.RequestID != state.Completed+state.Interrupted+queued+aboard {
		d.failf("the order balance: %d orders, %d completed, %d interrupted, %d queued, %d aboard", state.RequestID, state.Completed, state.Interrupted, queued, aboard)
	}
}

// apply sends a command that the driver chose at random. A refusal changes
// nothing, and the driver counts it.
func (d *soakDriver) apply(command Command) Reply {
	d.t.Helper()
	reply := d.s.Apply(d.client.next(command))
	d.result.commands++
	if reply.Error != "" {
		d.result.refused++
	}
	d.check()
	return reply
}

// startFault starts a command fault with no duration, and the driver clears it
// after the soak duration. A quarter are debris, as in the sampler.
func (d *soakDriver) startFault(tick int64) {
	d.t.Helper()
	command := Command{Action: "fault"}
	if d.rng.Float64() < soakDebrisShare {
		lane := d.config.Network.Lanes[d.rng.IntN(len(d.config.Network.Lanes))]
		length := d.config.Network.Length(lane)
		if length <= soakDebrisMeters {
			return
		}
		from := d.rng.Float64() * (length - soakDebrisMeters)
		command.LaneID, command.FromMeters, command.ToMeters = lane.ID, new(from), new(from+soakDebrisMeters)
	} else {
		vehicles := d.s.simulation.Snapshot().Vehicles
		command.PodID = vehicles[d.rng.IntN(len(vehicles))].Pod.ID
	}
	if reply := d.apply(command); reply.Error == "" {
		d.clears = append(d.clears, soakClear{reply.FaultID, tick + soakFaultSeconds*sim.TicksPerSecond})
	}
}

// startEmergency starts an emergency on a random occupied pod.
func (d *soakDriver) startEmergency() {
	d.t.Helper()
	var occupied []string
	for _, vehicle := range d.s.simulation.Snapshot().Vehicles {
		if vehicle.Pod.Occupied {
			occupied = append(occupied, vehicle.Pod.ID)
		}
	}
	if len(occupied) > 0 {
		d.apply(Command{Action: "emergency", PodID: occupied[d.rng.IntN(len(occupied))]})
	}
}

// runEvents sends the commands that are due.
func (d *soakDriver) runEvents(tick int64) {
	d.t.Helper()
	for tick >= d.nextFault {
		d.nextFault += soakInterval(d.rng)
		d.startFault(tick)
	}
	for tick >= d.nextEmergency {
		d.nextEmergency += soakInterval(d.rng)
		d.startEmergency()
	}
	for len(d.clears) > 0 && tick >= d.clears[0].tick {
		id := d.clears[0].id
		d.clears = d.clears[1:]
		d.apply(Command{Action: "clearFault", FaultID: id})
	}
}

// checkSave is the Persistence gate for a save: it restores a save in a new
// session, and the run continues in the old one.
func (d *soakDriver) checkSave() {
	d.t.Helper()
	file := sessionStateFile(d.t, d.s)
	file.RestoreAttempts = 0
	restored, err := NewFromStore(d.t.Context(), StoreInput{Store: &fakeStore{data: encodeTestState(d.t, file)},
		Options: []Option{WithLogger(slog.New(slog.DiscardHandler))}})
	if err != nil {
		d.failf("the restore: %v", err)
	}
	defer restored.Close()
	d.result.saves++
	switch restored.restore.Tier {
	case string(sim.RestorePhysical):
	case string(sim.RestoreLogical):
		d.result.demoted++
		if restored.restore.Reason == "" {
			d.failf("the save demotes with no reason")
		}
	default:
		d.failf("the save starts a new session: %+v", restored.restore)
	}
	if err := restored.simulation.CheckContract(); err != nil {
		d.failf("the restored contract: %v", err)
	}
}

// step advances the session one tick, and runs the checks.
func (d *soakDriver) step() {
	d.t.Helper()
	if d.input.timed {
		start := time.Now()
		d.s.advance()
		d.result.steps = append(d.result.steps, time.Since(start))
		return
	}
	d.s.advance()
	d.check()
}

// checkRewind is the Persistence gate for a rewind: a save point, a window with
// no command, and a rewind must replay the window exactly.
func (d *soakDriver) checkRewind() {
	d.t.Helper()
	id := d.client.mustApply(d.t, Command{Action: "checkpoint"}).Checkpoint
	for range soakReplayTicks {
		d.step()
	}
	want := d.s.simulation.ExportState()
	d.client.mustApply(d.t, Command{Action: "rewind", Checkpoint: id})
	d.client.mustApply(d.t, Command{Action: "pause", Paused: false})
	d.transition = d.s.simulation.FaultTransitionForTest()
	for range soakReplayTicks {
		d.step()
	}
	if got := d.s.simulation.ExportState(); !reflect.DeepEqual(got, want) {
		d.failf("the rewind does not replay")
	}
	d.result.rewinds++
}

// watchEmergencies is the Progress gate of the emergency contract. It reads
// the records once a second, so a length has an error of at most 1 second.
func (d *soakDriver) watchEmergencies() {
	tick := d.s.simulation.Tick()
	active := d.s.simulation.Snapshot().Emergencies.Active
	seen := make(map[string]bool, len(active))
	for _, record := range active {
		seen[record.ID] = true
		if _, ok := d.open[record.ID]; !ok {
			d.open[record.ID] = record
			d.result.emergencies++
		}
		if tick-record.StartTick > soakEmergencyTime && tick-record.StartTick <= soakEmergencyTime+soakViewTicks {
			d.noteLateEmergency(record)
		}
	}
	for id := range d.open {
		if !seen[id] {
			delete(d.open, id)
		}
	}
}

// noteLateEmergency records a record of an ordinary pod that passes the limit
// of the gate, with its phase and wait reason. A platoon member waits for its
// link to drain, so it is not an ordinary pod.
func (d *soakDriver) noteLateEmergency(record sim.EmergencyView) {
	for _, vehicle := range d.s.simulation.Snapshot().Vehicles {
		if vehicle.Pod.ID != record.PodID {
			continue
		}
		if vehicle.PlatoonID != "" {
			return
		}
		d.result.emergencyLate++
		d.result.misses = append(d.result.misses, fmt.Sprintf("emergency %s on %s: phase %s, wait %q, blocked by %q, start %d", record.ID, record.PodID, record.Phase, vehicle.Pod.WaitReason, vehicle.Pod.BlockedBy, record.StartTick))
	}
}

// runHours runs the soak until its last tick.
func (d *soakDriver) runHours() {
	d.t.Helper()
	last := int64(d.input.hours * soakTicksPerHour)
	window := soakCPUSeconds()
	for tick := int64(1); tick <= last; tick++ {
		if tick%soakRateWindow == 0 {
			cpu := soakCPUSeconds()
			rate := int(float64(soakRateWindow) / max(cpu-window, 0.01))
			d.result.rates = append(d.result.rates, rate)
			d.t.Logf("window %s %s seed=%d simulated_minutes=%d checks=%t queue=%d ticks/cpu_s=%d", d.input.preset, d.input.arm.name, d.input.seed, tick/(60*sim.TicksPerSecond), d.checks, d.s.simulation.PendingCount(), rate)
			window = cpu
		}
		if d.input.checks && !d.checks && tick >= d.input.checkFrom {
			d.startChecks()
		}
		d.runEvents(tick)
		d.step()
		d.result.queue.add(d.s.simulation.PendingCount())
		if !d.checks {
			continue
		}
		if tick%soakViewTicks == 0 {
			d.watchEmergencies()
		}
		if tick >= d.nextSave {
			d.nextSave += soakTicksPerHour
			d.checkSave()
		}
		if tick >= d.nextRewind {
			d.nextRewind += soakTicksPerHour
			d.checkRewind()
		}
	}
	d.result.ticks = last
}

// soakHoldsClear reports whether the state meets the Progress gate after the
// clear: no pod with a fault hold or an incident wait, and a waiting queue in
// the range of the run without faults. It lists each remaining wait.
func (d *soakDriver) soakHoldsClear() (bool, []string) {
	state := d.s.simulation.Snapshot()
	var waits []string
	for _, vehicle := range state.Vehicles {
		pod := vehicle.Pod
		if vehicle.Withdrawn&1 != 0 || pod.WaitReason == sim.BlockedByIncident || pod.WaitReason == sim.NoForwardRoute {
			waits = append(waits, fmt.Sprintf("pod %s: hold %d, wait %q, blocked by %q", pod.ID, vehicle.Withdrawn, pod.WaitReason, pod.BlockedBy))
		}
	}
	if d.input.queue != nil && len(state.Pending) > d.input.queue.high {
		waits = append(waits, fmt.Sprintf("the queue has %d orders, more than the %d of the reference run", len(state.Pending), d.input.queue.high))
	}
	return len(waits) == 0, waits
}

// clearAndSettle is the Progress gate after the clear. The stop entry keeps
// each sampler live with no new event, the driver clears each active fault by
// command, and the 30 minutes start at the last clear, with demand running.
func (d *soakDriver) clearAndSettle() {
	d.t.Helper()
	d.s.simulation.StopIncidentSamplersForTest()
	d.nextFault, d.nextEmergency, d.nextSave, d.nextRewind = math.MaxInt64, math.MaxInt64, math.MaxInt64, math.MaxInt64
	d.clears = nil
	for _, fault := range d.s.simulation.Snapshot().Faults.Active {
		d.client.mustApply(d.t, Command{Action: "clearFault", FaultID: fault.ID})
		d.check()
	}
	settled := false
	var waits []string
	settle := cmp.Or(d.input.settle, soakClearTicks)
	for tick := int64(1); tick <= settle; tick++ {
		d.step()
		d.result.queue.add(d.s.simulation.PendingCount())
		if !d.checks || tick%soakViewTicks != 0 {
			continue
		}
		d.watchEmergencies()
		if !settled {
			if ok, remaining := d.soakHoldsClear(); ok {
				settled, d.result.settleTicks = true, tick
			} else {
				waits = remaining
			}
		}
	}
	d.result.ticks += settle
	if !d.checks {
		return
	}
	if !settled {
		d.result.misses = append(d.result.misses, waits...)
		d.result.misses = append(d.result.misses, "progress after the clear: the gate holds at no time in 30 minutes")
	}
	for _, record := range d.open {
		d.result.misses = append(d.result.misses, fmt.Sprintf("emergency %s on %s does not end: phase %s, start %d", record.ID, record.PodID, record.Phase, record.StartTick))
	}
}

// checkRail is the Rail gate. It reads the records of the demand run. Neither
// soak preset has a rail service, so this gate has no record on them, and
// railRecords reports that.
func (d *soakDriver) checkRail() {
	d.t.Helper()
	for _, record := range d.s.demand.connectionRecords() {
		d.result.railRecords++
		if record.Reason == "interrupted" && record.Outcome != "unserved" {
			d.failf("the rail record %+v", record)
		}
	}
}

// runSoak runs one soak and returns its numbers. A gate that holds for each
// boundary stops the test at the first failure. A miss of a Progress gate
// stops it at the end of the run, with the list.
func runSoak(t *testing.T, input soakInput) soakResult {
	t.Helper()
	d := newSoakDriver(t, input)
	start, startCPU := time.Now(), soakCPUSeconds()
	d.runHours()
	d.clearAndSettle()
	d.result.wall, d.result.cpu = time.Since(start), soakCPUSeconds()-startCPU
	d.checkRail()
	d.result.state = d.s.simulation.Snapshot()
	state := d.result.state
	t.Logf("preset=%s arm=%s seed=%d hours=%g checks=%t ticks=%d wall=%.1fs cpu=%.1fs ticks/s=%.0f commands=%d refused=%d saves=%d demoted=%d rewinds=%d rail=%d completed=%d interrupted=%d queue=%d..%d settle=%ds waitAvg=%.1fs waitMax=%.1fs faults=%+v emergencies=%+v seen=%d late=%d rates=%v",
		input.preset, input.arm.name, input.seed, input.hours, input.checks, d.result.ticks, d.result.wall.Seconds(), d.result.cpu, float64(d.result.ticks)/d.result.wall.Seconds(),
		d.result.commands, d.result.refused, d.result.saves, d.result.demoted, d.result.rewinds, d.result.railRecords, state.Completed, state.Interrupted, d.result.queue.low, d.result.queue.high,
		d.result.settleTicks/sim.TicksPerSecond, state.Wait.AverageSeconds, state.Wait.MaxSeconds, state.Faults.Counters, state.Emergencies.Counters, d.result.emergencies, d.result.emergencyLate, d.result.rates)
	if len(d.result.misses) > 0 {
		t.Errorf("preset %s arm %s seed %d misses a Progress gate:\n%s", input.preset, input.arm.name, input.seed, strings.Join(d.result.misses, "\n"))
	}
	return d.result
}

// soakCPUSeconds returns the CPU time of the process, in seconds, or 0 with no
// /proc file. A busy host changes it less than it changes the wall time.
func soakCPUSeconds() float64 {
	raw, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return 0
	}
	_, fields, ok := strings.Cut(string(raw), ") ")
	if !ok {
		return 0
	}
	// The fields after the process name start with the state, field 3 of the
	// format, and utime and stime are fields 14 and 15.
	list := strings.Fields(fields)
	if len(list) < 13 {
		return 0
	}
	user, _ := strconv.ParseFloat(list[11], 64)
	system, _ := strconv.ParseFloat(list[12], 64)
	return (user + system) / 100
}

// soakEnv returns an environment value with a default.
func soakEnv(name, fallback string) string {
	return cmp.Or(os.Getenv(name), fallback)
}

// soakGroups pairs each incident arm with the reference run that its Progress
// gate compares with.
var soakGroups = []struct {
	reference string
	arms      []string
}{
	{"reference", []string{"incidents", "emergencies"}},
	{"reference-platoons", []string{"platoons"}},
}

// TestIncidentSoak is the soak of section 5.1.9 of the incident stage 7
// contract. It runs only by request, because the full matrix takes many CPU
// hours. Decision 16 asks for a timing run first, and the maintainer chooses
// the size. These variables select the run, and their defaults give the
// matrix of the contract: PODSIM_SOAK=1, PODSIM_SOAK_PRESETS, PODSIM_SOAK_ARMS,
// PODSIM_SOAK_SEEDS, PODSIM_SOAK_FIRST_SEED, and PODSIM_SOAK_HOURS. Use -parallel
// to set the number of runs at one time. For a timing run only,
// PODSIM_SOAK_CHECKS=0 turns the every-tick checks off, and
// PODSIM_SOAK_SETTLE_MINUTES shortens the window after the clear, and
// PODSIM_SOAK_CHECK_FROM_HOURS starts the checks late, at the full queue.
func TestIncidentSoak(t *testing.T) {
	t.Parallel()
	if os.Getenv("PODSIM_SOAK") != "1" {
		t.Skip("the soak runs only with PODSIM_SOAK=1")
	}
	presets := strings.Split(soakEnv("PODSIM_SOAK_PRESETS", "rail-hub,london-central"), ",")
	arms := strings.Split(soakEnv("PODSIM_SOAK_ARMS", "incidents,emergencies,platoons,reference,reference-platoons"), ",")
	seeds, err := strconv.ParseUint(soakEnv("PODSIM_SOAK_SEEDS", "20"), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	first, err := strconv.ParseUint(soakEnv("PODSIM_SOAK_FIRST_SEED", "1"), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	hours, err := strconv.ParseFloat(soakEnv("PODSIM_SOAK_HOURS", "24"), 64)
	if err != nil {
		t.Fatal(err)
	}
	checks := os.Getenv("PODSIM_SOAK_CHECKS") != "0"
	settleMinutes, err := strconv.ParseInt(soakEnv("PODSIM_SOAK_SETTLE_MINUTES", "30"), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	settle := settleMinutes * 60 * sim.TicksPerSecond
	checkFromHours, err := strconv.ParseFloat(soakEnv("PODSIM_SOAK_CHECK_FROM_HOURS", "0"), 64)
	if err != nil {
		t.Fatal(err)
	}
	checkFrom := int64(checkFromHours * soakTicksPerHour)
	for _, preset := range presets {
		for seed := first; seed < first+seeds; seed++ {
			for _, group := range soakGroups {
				t.Run(fmt.Sprintf("%s/seed-%d/%s", preset, seed, group.reference), func(t *testing.T) {
					t.Parallel()
					var queue *soakRange
					if slices.Contains(arms, group.reference) {
						result := runSoak(t, soakInput{preset: preset, arm: soakArms[group.reference], seed: seed, hours: hours, settle: settle})
						queue = &result.queue
					}
					for _, name := range group.arms {
						if slices.Contains(arms, name) {
							runSoak(t, soakInput{preset: preset, arm: soakArms[name], seed: seed, hours: hours, checks: checks, queue: queue, settle: settle, checkFrom: checkFrom})
						}
					}
				})
			}
		}
	}
}

// TestIncidentSoakCI is the CI soak of section 5.1.9: 1 seed for 1 simulated
// hour, with the sampler only, on the rail-hub preset. The row names no preset,
// and a London run takes an estimated 20 to 30 CPU minutes
// (docs/studies/incident-soak.md), too long for one job of the qualify task.
// mise run qualify runs this test.
func TestIncidentSoakCI(t *testing.T) {
	if os.Getenv("PODSIM_QUALIFY") != "1" {
		t.Skip("the CI soak runs only in mise run qualify (PODSIM_QUALIFY=1)")
	}
	if testing.Short() || raceEnabled {
		t.Skip("one simulation on one goroutine, so the race detector finds nothing, and a long test")
	}
	const preset = "rail-hub"
	reference := runSoak(t, soakInput{preset: preset, arm: soakArms["reference"], seed: 1, hours: 1})
	run := runSoak(t, soakInput{preset: preset, arm: soakArms["ci"], seed: 1, hours: 1, checks: true, queue: &reference.queue})
	if counters := run.state.Faults.Counters; counters.Sampled == 0 || run.state.Emergencies.Counters.Sampled == 0 {
		t.Errorf("the soak samples no event: %+v, %+v", counters, run.state.Emergencies.Counters)
	}
}

// TestIncidentSoakDriver runs a short soak in the ordinary test run, so that the
// driver stays in use: a pod fault that ends, a command fault, a command
// emergency, a save, a rewind, and the clear.
func TestIncidentSoakDriver(t *testing.T) {
	t.Parallel()
	if raceEnabled {
		t.Skip("one simulation on one goroutine, so the race detector finds nothing")
	}
	input := soakInput{preset: "rail-hub", arm: soakArms["incidents"], seed: 1, hours: 0.02, checks: true, settle: 60 * sim.TicksPerSecond}
	d := newSoakDriver(t, input)
	d.nextFault, d.nextEmergency, d.nextSave, d.nextRewind = 100, 2000, 2500, 3000
	reply := d.client.mustApply(t, Command{Action: "fault", PodID: d.config.Fleet[0].ID})
	d.clears = append(d.clears, soakClear{reply.FaultID, 1000})
	d.runHours()
	d.clearAndSettle()
	d.checkRail()
	if r := d.result; r.commands == 0 || r.saves != 1 || r.rewinds != 1 || r.emergencies == 0 || r.settleTicks == 0 || len(r.misses) != 0 {
		t.Fatalf("the driver result is %+v", r)
	}
	state := d.s.simulation.Snapshot()
	if len(state.Faults.Active) != 0 || state.Faults.Counters.Started == 0 {
		t.Fatalf("the faults are %+v", state.Faults)
	}
}

// soakTimes is the summary of the step times of one run.
type soakTimes struct {
	median, slowest time.Duration
}

func summarizeSteps(steps []time.Duration) soakTimes {
	sorted := slices.Clone(steps)
	slices.Sort(sorted)
	return soakTimes{sorted[len(sorted)/2], sorted[len(sorted)-1]}
}

// checkSoakTimes reports the pass rule of the Performance gates, and enforces
// it with PODSIM_SOAK_GATE=1. Use that only on a quiet machine (A3).
func checkSoakTimes(t *testing.T, name string, incident, reference soakTimes) {
	t.Helper()
	t.Logf("%s: median %v against %v, slowest %v against %v", name, incident.median, reference.median, incident.slowest, reference.slowest)
	if os.Getenv("PODSIM_SOAK_GATE") != "1" {
		return
	}
	if float64(incident.median) > 1.1*float64(reference.median) {
		t.Errorf("%s: the median step %v is more than 10 percent above %v", name, incident.median, reference.median)
	}
	if incident.slowest > 2*reference.slowest {
		t.Errorf("%s: the slowest step %v is more than twice %v", name, incident.slowest, reference.slowest)
	}
}

// TestIncidentSoakTimed is the Performance gate with A3: 3 seeds on each preset
// with the every-tick checks off, against 3 reference runs, one after another.
// PODSIM_SOAK_TIMED sets the simulated hours of each run. PODSIM_SOAK_PRESETS
// and PODSIM_SOAK_SEEDS select a smaller run.
func TestIncidentSoakTimed(t *testing.T) {
	value := os.Getenv("PODSIM_SOAK_TIMED")
	if value == "" {
		t.Skip("the timed runs run only with PODSIM_SOAK_TIMED=<hours>, on a quiet machine")
	}
	hours, err := strconv.ParseFloat(value, 64)
	if err != nil {
		t.Fatal(err)
	}
	seeds, err := strconv.ParseUint(soakEnv("PODSIM_SOAK_SEEDS", "3"), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	for preset := range strings.SplitSeq(soakEnv("PODSIM_SOAK_PRESETS", "rail-hub,london-central"), ",") {
		for seed := uint64(1); seed <= seeds; seed++ {
			reference := runSoak(t, soakInput{preset: preset, arm: soakArms["reference"], seed: seed, hours: hours, timed: true})
			run := runSoak(t, soakInput{preset: preset, arm: soakArms["incidents"], seed: seed, hours: hours, timed: true})
			checkSoakTimes(t, fmt.Sprintf("%s seed %d", preset, seed), summarizeSteps(run.steps), summarizeSteps(reference.steps))
		}
	}
}

// soakWorstCase places worst-case records on a session: up to debris debris
// faults, then pod faults on up to pods pods, then up to emergencies
// emergencies. It returns the counts of the records.
func soakWorstCase(t *testing.T, d *soakDriver, debris, pods, emergencies int) (int, int, int) {
	t.Helper()
	placedDebris, placedPods, placedEmergencies := 0, 0, 0
	lanes := d.config.Network.Lanes
	for _, index := range d.rng.Perm(len(lanes)) {
		if placedDebris == debris {
			break
		}
		lane := lanes[index]
		length := d.config.Network.Length(lane)
		if length <= soakDebrisMeters {
			continue
		}
		from := length/2 - soakDebrisMeters/2
		reply := d.s.Apply(d.client.next(Command{Action: "fault", LaneID: lane.ID, FromMeters: new(from), ToMeters: new(from + soakDebrisMeters)}))
		if reply.Error == "" {
			placedDebris++
		}
	}
	for _, vehicle := range d.s.simulation.Snapshot().Vehicles {
		if placedEmergencies < emergencies && vehicle.Pod.Occupied {
			if reply := d.s.Apply(d.client.next(Command{Action: "emergency", PodID: vehicle.Pod.ID})); reply.Error == "" {
				placedEmergencies++
				continue
			}
		}
		if placedPods < pods {
			if reply := d.s.Apply(d.client.next(Command{Action: "fault", PodID: vehicle.Pod.ID})); reply.Error == "" {
				placedPods++
			}
		}
	}
	return placedDebris, placedPods, placedEmergencies
}

// TestIncidentWorstCaseStepTime gives the worst-case rows of the Performance
// gates and the fleet-limit report of section 5.1.9. Each row compares 600
// steps with the records and 600 steps of the same state with none.
// PODSIM_SOAK_WORST=1 runs it.
func TestIncidentWorstCaseStepTime(t *testing.T) {
	if os.Getenv("PODSIM_SOAK_WORST") != "1" {
		t.Skip("the worst-case times run only with PODSIM_SOAK_WORST=1, on a quiet machine")
	}
	const warmTicks, measuredTicks = 3600, 600
	rows := []struct {
		name                      string
		preset                    string
		debris, pods, emergencies int
	}{
		{name: "suspension: 64 debris and 300 pod faults", preset: "fleet-300", debris: 64, pods: 300},
		{name: "emergency: MaxEmergencies records", preset: "rail-hub", emergencies: sim.MaxEmergencies},
		{name: "emergency: MaxEmergencies records", preset: "london-central", emergencies: sim.MaxEmergencies},
		{name: "fleet limit: 64 debris and 1,000 pod faults", preset: "fleet-1000", debris: 64, pods: 1000},
	}
	for _, row := range rows {
		t.Run(row.preset+"/"+row.name, func(t *testing.T) {
			measure := func(apply bool) soakTimes {
				d := newSoakDriver(t, soakInput{preset: row.preset, arm: soakArm{name: "worst", faults: true, emergencies: true}, seed: 1, timed: true})
				for range warmTicks {
					d.s.advance()
				}
				if apply {
					debris, pods, emergencies := soakWorstCase(t, d, row.debris, row.pods, row.emergencies)
					t.Logf("records: %d debris, %d pod faults, %d emergencies, of %d pods", debris, pods, emergencies, len(d.config.Fleet))
				}
				for range measuredTicks {
					d.step()
				}
				return summarizeSteps(d.result.steps)
			}
			reference := measure(false)
			checkSoakTimes(t, row.name, measure(true), reference)
		})
	}
}
