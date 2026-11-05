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
	soakReplayTicks   = 10 * sim.TicksPerSecond
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
	// everyTick runs the full check at each tick, and not at the stride.
	everyTick bool
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
	// emergencyInPlatoon counts the records that pass the limit on a platoon
	// member, and emergencyLong the records that end longer than the limit in
	// total time. emergencyLongest is the longest time not blocked by a fault
	// of any record, in ticks.
	emergencyInPlatoon, emergencyLong int
	emergencyLongest                  int64
	settleTicks                       int64
	misses                            []string
	steps                             []time.Duration
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
	// events holds the incident numbers of the last check. fullChecks counts
	// the full checks, and lastFull is the simulation tick of the last one.
	events     sim.IncidentEvents
	fullChecks int
	lastFull   int64
	// fail reports a failed gate. It stops the test, and only a test of the
	// driver replaces it. breakExport, when set, changes the state that the
	// full check reads, so such a test can make a violation.
	fail        func(message string)
	breakExport func(*sim.SavedState)
	// nextFault, nextEmergency, nextSave, and nextRewind are the ticks of the
	// next driver events. clears holds the command faults, by clear tick.
	nextFault, nextEmergency, nextSave, nextRewind int64
	clears                                         []soakClear
	// open holds the reading state of each active record.
	open   map[string]*soakOpenRecord
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
	d := &soakDriver{t: t, input: input, config: config, s: s, client: newTestClient(s, "soak"), rng: rng, open: map[string]*soakOpenRecord{},
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
	d.events = d.s.simulation.IncidentEventsForTest()
}

// failf stops the test with the run and the tick.
func (d *soakDriver) failf(format string, args ...any) {
	d.t.Helper()
	message := fmt.Sprintf("%s %s seed %d tick %d: %s", d.input.preset, d.input.arm.name, d.input.seed, d.s.simulation.Tick(), fmt.Sprintf(format, args...))
	if d.fail != nil {
		d.fail(message)
		return
	}
	d.t.Fatal(message)
}

// check runs the checks of a tick or a command, by the stride of amendment A10.
// The cheap checks run each time: the fault and emergency records, the
// transition rules, and the Safety observation. The full check runs at each
// second and at each event. An event is a command of the driver, or a change of
// the incident numbers of the simulation, which a sampler can make with no
// command.
func (d *soakDriver) check(command bool) {
	d.t.Helper()
	if !d.checks {
		return
	}
	simulation := d.s.simulation
	if err := simulation.CheckIncidentRecordsForTest(); err != nil {
		d.failf("the contract: %v", err)
	}
	if err := d.transition(); err != nil {
		d.failf("the transition rules: %v", err)
	}
	if _, err := simulation.SafetyObservation().Check(); err != nil {
		d.failf("the safety observation: %v", err)
	}
	events := simulation.IncidentEventsForTest()
	if command || events != d.events || d.input.everyTick || simulation.Tick()%soakViewTicks == 0 {
		d.fullCheck()
	}
	d.events = events
}

// fullCheck is the check of the saved state and the Conservation row, from one
// export.
func (d *soakDriver) fullCheck() {
	d.t.Helper()
	d.fullChecks++
	d.lastFull = d.s.simulation.Tick()
	state, err := d.s.simulation.CheckedExportForTest()
	if err != nil {
		d.failf("the contract: %v", err)
	}
	if d.breakExport != nil {
		d.breakExport(&state)
	}
	if err := soakOrderBalance(state); err != nil {
		d.failf("%v", err)
	}
}

// soakOrderBalance is the Conservation row. CheckedExportForTest does not prove
// it: that check compares the unaccounted orders of the state with the count
// of the simulation, and a restore that drops orders makes that count above 0.
func soakOrderBalance(state sim.SavedState) error {
	queued, aboard := len(state.Waiting), 0
	for index := range state.Pods {
		for rider := range state.Pods[index].Riders {
			if !state.Pods[index].Riders[rider].Completed {
				aboard++
			}
		}
	}
	if state.RequestID != state.Completed+state.Interrupted+queued+aboard {
		return fmt.Errorf("the order balance: %d orders, %d completed, %d interrupted, %d queued, %d aboard", state.RequestID, state.Completed, state.Interrupted, queued, aboard)
	}
	return nil
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
	d.check(true)
	return reply
}

// mustApply sends a command that must succeed, and runs the checks of a command.
func (d *soakDriver) mustApply(command Command) Reply {
	d.t.Helper()
	reply := d.client.mustApply(d.t, command)
	d.check(true)
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
	d.check(false)
}

// checkRewind is the Persistence gate for a rewind: a save point, a window with
// no command, and a rewind must replay the window exactly.
func (d *soakDriver) checkRewind() {
	d.t.Helper()
	generation, serial := d.s.generation, d.s.simulation.ExportState().IncidentSerial
	id := d.mustApply(Command{Action: "checkpoint"}).Checkpoint
	for range soakReplayTicks {
		d.step()
	}
	want := d.s.simulation.ExportState()
	d.client.mustApply(d.t, Command{Action: "rewind", Checkpoint: id})
	d.transition = d.s.simulation.FaultTransitionForTest()
	d.mustApply(Command{Action: "pause", Paused: false})
	for range soakReplayTicks {
		d.step()
	}
	soakRenewGeneration(&want, serial, generation, d.s.generation)
	if got := d.s.simulation.ExportState(); !reflect.DeepEqual(got, want) {
		d.failf("the rewind does not replay")
	}
	d.result.rewinds++
}

// soakRenewGeneration gives a record of the window, with a serial above serial,
// the generation of the replay. A rewind starts a new generation, and that
// changes only the IDs of the records that the replay makes. A record from
// before the save point keeps its generation, and the comparison checks it.
func soakRenewGeneration(state *sim.SavedState, serial, from, to uint64) {
	if state.Faults != nil {
		for index := range state.Faults.Records {
			if record := &state.Faults.Records[index]; record.Serial > serial && record.Generation == from {
				record.Generation = to
			}
		}
	}
	if state.Emergencies != nil {
		for index := range state.Emergencies.Records {
			if record := &state.Emergencies.Records[index]; record.Serial > serial && record.Generation == from {
				record.Generation = to
			}
		}
	}
}

// soakReading holds one reading of the state, for the classifier of amendment
// A8 of the incident stage 7 contract.
type soakReading struct {
	pods                map[string]sim.Pod
	faultIDs, faultPods map[string]bool
	// legBlocked reports whether an idle pod has an order that cannot board
	// only because of the active faults.
	legBlocked func(podID string) bool
}

func newSoakReading(state sim.Snapshot, legBlocked func(string) bool) soakReading {
	reading := soakReading{pods: make(map[string]sim.Pod, len(state.Vehicles)), faultIDs: map[string]bool{}, faultPods: map[string]bool{}, legBlocked: legBlocked}
	for index := range state.Vehicles {
		pod := state.Vehicles[index].Pod
		reading.pods[pod.ID] = pod
	}
	for _, fault := range state.Faults.Active {
		reading.faultIDs[fault.ID] = true
		if fault.PodID != "" {
			reading.faultPods[fault.PodID] = true
		}
	}
	return reading
}

// blockedByFault reports whether the time of the pod is blocked by a fault
// under rule A of amendment A8, and it names the end of the BlockedBy chain.
// Each case that the rule does not name stays not blocked, because a fault
// named in error hides a real wait. With no active fault, nothing is blocked,
// so with faults off the whole record counts.
//
// A pod whose terminal berth choice fails while the blocked set is not empty
// reports "No forward route" or "Blocked by incident" at each tick (see
// reportBlockedBerths), so such a stop needs no search here.
func (r soakReading) blockedByFault(podID string) (bool, string) {
	blocked, end := r.walkChain(podID)
	return blocked && len(r.faultIDs) > 0, end
}

// walkChain follows the BlockedBy chain from the pod. Each pod enters the walk
// once, so a cycle ends it.
func (r soakReading) walkChain(podID string) (bool, string) {
	seen := make(map[string]bool, 4)
	for id := podID; ; {
		pod, ok := r.pods[id]
		switch {
		case !ok:
			return false, "unknown blocker " + id
		case seen[id]:
			return false, "cycle at pod " + id
		case r.faultPods[id]:
			return true, fmt.Sprintf("pod %s with a fault", id)
		case pod.WaitReason == sim.BlockedByIncident || pod.WaitReason == sim.NoForwardRoute:
			return true, fmt.Sprintf("pod %s, wait %q, blocked by %q", id, pod.WaitReason, pod.BlockedBy)
		case r.faultIDs[pod.BlockedBy]:
			return true, "fault " + pod.BlockedBy
		case pod.BlockedBy == "":
			return r.legBlocked(id), fmt.Sprintf("pod %s, %s, wait %q", id, pod.Activity, pod.WaitReason)
		}
		seen[id] = true
		id = pod.BlockedBy
	}
}

// soakOpenRecord is the reading state of one active record.
type soakOpenRecord struct {
	view sim.EmergencyView
	// last is the tick of the last reading.
	last int64
	// unblocked sums the ticks not blocked by a fault. A reading gives its
	// class to the ticks since the last reading. That is 1 second, or the
	// length of the replay after a rewind.
	unblocked int64
	noted     bool
}

// read adds the ticks since the last reading. It reports true once, at the
// first reading at which the sum passes the limit of the gate.
func (r *soakOpenRecord) read(tick int64, blocked bool) bool {
	if !blocked {
		r.unblocked += tick - r.last
	}
	r.last = tick
	if r.noted || r.unblocked <= soakEmergencyTime {
		return false
	}
	r.noted = true
	return true
}

// watchEmergencies is the Progress gate of the emergency contract, with rule A
// of amendment A8. It reads the records once a second. A record that a late
// start of the checks finds counts from that start.
func (d *soakDriver) watchEmergencies() {
	tick := d.s.simulation.Tick()
	state := d.s.simulation.Snapshot()
	reading := newSoakReading(state, d.s.simulation.BoardingLegBlockedForTest)
	seen := make(map[string]bool, len(state.Emergencies.Active))
	for _, view := range state.Emergencies.Active {
		seen[view.ID] = true
		record, ok := d.open[view.ID]
		if !ok {
			record = &soakOpenRecord{last: max(view.StartTick, d.input.checkFrom)}
			d.open[view.ID] = record
			d.result.emergencies++
		}
		record.view = view
		blocked, end := reading.blockedByFault(view.PodID)
		if record.read(tick, blocked) {
			d.noteLateEmergency(state, view, end)
		}
		d.result.emergencyLongest = max(d.result.emergencyLongest, record.unblocked)
	}
	for id, record := range d.open {
		if !seen[id] {
			if record.last-record.view.StartTick > soakEmergencyTime {
				d.result.emergencyLong++
			}
			delete(d.open, id)
		}
	}
}

// noteLateEmergency records a record of an ordinary pod that passes the limit
// of the gate, with its phase, wait reason, and the end of its chain. A platoon
// member waits for its link to drain, so it is not an ordinary pod, and the
// driver only counts it.
func (d *soakDriver) noteLateEmergency(state sim.Snapshot, record sim.EmergencyView, end string) {
	for index := range state.Vehicles {
		vehicle := &state.Vehicles[index]
		if vehicle.Pod.ID != record.PodID {
			continue
		}
		if vehicle.PlatoonID != "" {
			d.result.emergencyInPlatoon++
			return
		}
		d.result.emergencyLate++
		d.result.misses = append(d.result.misses, fmt.Sprintf("emergency %s on %s: phase %s, wait %q, blocked by %q, chain end %s, start %d", record.ID, record.PodID, record.Phase, vehicle.Pod.WaitReason, vehicle.Pod.BlockedBy, end, record.StartTick))
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
		d.mustApply(Command{Action: "clearFault", FaultID: fault.ID})
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
		d.result.misses = append(d.result.misses, fmt.Sprintf("emergency %s on %s does not end: phase %s, start %d", record.view.ID, record.view.PodID, record.view.Phase, record.view.StartTick))
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
	t.Logf("preset=%s arm=%s seed=%d hours=%g checks=%t ticks=%d wall=%.1fs cpu=%.1fs ticks/s=%.0f commands=%d refused=%d saves=%d demoted=%d rewinds=%d rail=%d completed=%d interrupted=%d queue=%d..%d settle=%ds waitAvg=%.1fs waitMax=%.1fs faults=%+v emergencies=%+v seen=%d late=%d inPlatoon=%d long=%d longestUnblocked=%.1fs fullChecks=%d rates=%v",
		input.preset, input.arm.name, input.seed, input.hours, input.checks, d.result.ticks, d.result.wall.Seconds(), d.result.cpu, float64(d.result.ticks)/d.result.wall.Seconds(),
		d.result.commands, d.result.refused, d.result.saves, d.result.demoted, d.result.rewinds, d.result.railRecords, state.Completed, state.Interrupted, d.result.queue.low, d.result.queue.high,
		d.result.settleTicks/sim.TicksPerSecond, state.Wait.AverageSeconds, state.Wait.MaxSeconds, state.Faults.Counters, state.Emergencies.Counters, d.result.emergencies, d.result.emergencyLate, d.result.emergencyInPlatoon, d.result.emergencyLong, float64(d.result.emergencyLongest)/sim.TicksPerSecond, d.fullChecks, d.result.rates)
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
	everyTick := os.Getenv("PODSIM_SOAK_EVERY_TICK") == "1"
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
							runSoak(t, soakInput{preset: preset, arm: soakArms[name], seed: seed, hours: hours, checks: checks, queue: queue, settle: settle, checkFrom: checkFrom, everyTick: everyTick})
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
	d.nextFault, d.nextEmergency, d.nextSave, d.nextRewind = 5*sim.TicksPerSecond/3, 100*sim.TicksPerSecond/3, 125*sim.TicksPerSecond/3, 50*sim.TicksPerSecond
	reply := d.client.mustApply(t, Command{Action: "fault", PodID: d.config.Fleet[0].ID})
	d.clears = append(d.clears, soakClear{reply.FaultID, 50 * sim.TicksPerSecond / 3})
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

// soakStrideInput is a short run with the sampler and with no driver command.
func soakStrideInput(everyTick bool) soakInput {
	return soakInput{preset: "rail-hub", arm: soakArms["ci"], seed: 1, checks: true, everyTick: everyTick}
}

// TestSoakStrideEvents steps a run through a sampler fault, a sampler
// emergency, their ends, and driver commands. The full check must run at each
// tick where the incident numbers change, at each second, and at each command,
// also one that changes no such number, and at no other tick.
func TestSoakStrideEvents(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("about 50 simulated minutes")
	}
	d := newSoakDriver(t, soakStrideInput(false))
	limit := 3600 * sim.TicksPerSecond
	offSecond, want := 0, sim.IncidentEvents{}
	for range limit {
		before := d.s.simulation.IncidentEventsForTest()
		d.step()
		tick := d.s.simulation.Tick()
		events := d.s.simulation.IncidentEventsForTest()
		wantFull := events != before || tick%soakViewTicks == 0
		if got := d.lastFull == tick; got != wantFull {
			t.Fatalf("tick %d: a full check %t, want %t, with %+v then %+v", tick, got, wantFull, before, events)
		}
		if events != before && tick%soakViewTicks != 0 {
			offSecond++
		}
		want = events
		if want.FaultSampled > 0 && want.EmergencySampled > 0 && want.FaultCleared > 0 && want.EmergencyEnded > 0 && offSecond > 0 {
			break
		}
	}
	if want.FaultSampled == 0 || want.EmergencySampled == 0 || want.FaultCleared == 0 || want.EmergencyEnded == 0 || offSecond == 0 {
		t.Fatalf("the run does not see each kind of event: %+v, %d between seconds", want, offSecond)
	}
	// A command between two seconds gets its own full check.
	for d.s.simulation.Tick()%soakViewTicks != 1 {
		d.step()
	}
	full := d.fullChecks
	d.apply(Command{Action: "pause", Paused: false})
	if d.fullChecks != full+1 {
		t.Fatalf("the command makes %d full checks", d.fullChecks-full)
	}
}

// TestSoakCatchesViolation makes a violation of the order balance at a chosen
// tick. With no event, the next full check at a second finds it. At an event,
// the check of that event finds it. In the every-tick mode, the same tick does.
func TestSoakCatchesViolation(t *testing.T) {
	t.Parallel()
	const from = 7*soakViewTicks + 1
	tests := []struct {
		name      string
		everyTick bool
		event     bool
		want      int64
	}{
		{name: "no event", want: 8 * soakViewTicks},
		{name: "a command", event: true, want: from},
		{name: "every tick", everyTick: true, want: from},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			d := newSoakDriver(t, soakStrideInput(test.everyTick))
			var caught int64 = -1
			d.fail = func(string) {
				if caught < 0 {
					caught = d.s.simulation.Tick()
				}
			}
			d.breakExport = func(state *sim.SavedState) {
				if d.s.simulation.Tick() >= from {
					state.RequestID++
				}
			}
			for d.s.simulation.Tick() < from-1 {
				d.step()
			}
			if caught >= 0 {
				t.Fatalf("a check at tick %d sees a violation before tick %d", caught, from)
			}
			if test.event {
				d.step()
				if caught < 0 {
					d.apply(Command{Action: "pause", Paused: false})
				}
			}
			for caught < 0 && d.s.simulation.Tick() < 10*soakViewTicks {
				d.step()
			}
			if caught != test.want {
				t.Fatalf("the check finds the violation at tick %d, want %d", caught, test.want)
			}
		})
	}
}

// TestSoakStrideKeepsRun checks that the stride changes no state: the every-tick
// mode gives the same run, with a full check at each tick.
func TestSoakStrideKeepsRun(t *testing.T) {
	t.Parallel()
	run := func(everyTick bool) (sim.SavedState, *soakDriver) {
		input := soakStrideInput(everyTick)
		input.arm = soakArms["incidents"]
		input.hours = 0.02
		d := newSoakDriver(t, input)
		d.runHours()
		return d.s.simulation.ExportState(), d
	}
	stride, strideDriver := run(false)
	every, everyDriver := run(true)
	if !reflect.DeepEqual(stride, every) {
		t.Fatal("the stride changes the run")
	}
	if ticks := int(0.02 * soakTicksPerHour); everyDriver.fullChecks < ticks || strideDriver.fullChecks >= ticks/2 {
		t.Fatalf("%d ticks have %d full checks with the stride, and %d in every-tick mode", ticks, strideDriver.fullChecks, everyDriver.fullChecks)
	}
}

// TestSoakOrderBalance checks each term of the Conservation row on a state that
// balances, and on states that do not.
func TestSoakOrderBalance(t *testing.T) {
	t.Parallel()
	balanced := func() sim.SavedState {
		return sim.SavedState{RequestID: 7, Completed: 2, Interrupted: 1, Waiting: make([]sim.SavedTrip, 1), Pods: []sim.SavedPod{
			{Riders: []sim.SavedRequest{{Completed: true}, {}, {}}},
			{Riders: []sim.SavedRequest{{}}},
		}}
	}
	tests := []struct {
		name string
		edit func(*sim.SavedState)
		ok   bool
	}{
		{name: "balanced", edit: func(*sim.SavedState) {}, ok: true},
		{name: "an order too many", edit: func(s *sim.SavedState) { s.RequestID++ }},
		{name: "a completed order lost", edit: func(s *sim.SavedState) { s.Completed-- }},
		{name: "an interrupted order lost", edit: func(s *sim.SavedState) { s.Interrupted-- }},
		{name: "a queued order lost", edit: func(s *sim.SavedState) { s.Waiting = nil }},
		{name: "a rider lost", edit: func(s *sim.SavedState) { s.Pods[1].Riders = nil }},
		{name: "a completed rider counts as aboard", edit: func(s *sim.SavedState) { s.Pods[0].Riders[0].Completed = false }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			state := balanced()
			test.edit(&state)
			if err := soakOrderBalance(state); (err == nil) != test.ok {
				t.Fatalf("the check gives %v, want ok %t", err, test.ok)
			}
		})
	}
}

// TestSoakBlockedByFault checks the classifier of rule A of amendment A8. The
// cases that must stay not blocked guard against a fault named in error, which
// hides a real wait.
func TestSoakBlockedByFault(t *testing.T) {
	t.Parallel()
	pod := func(id string, reason sim.WaitReason, by string) sim.Vehicle {
		return sim.Vehicle{Pod: sim.Pod{ID: id, Activity: sim.Traveling, WaitReason: reason, BlockedBy: by}}
	}
	idle := func(id string) sim.Vehicle {
		return sim.Vehicle{Pod: sim.Pod{ID: id, Activity: sim.Idle}}
	}
	debris := []sim.FaultView{{ID: "i1.1", Kind: "debris"}}
	tests := []struct {
		name       string
		pods       []sim.Vehicle
		faults     []sim.FaultView
		legBlocked []string
		want       bool
		end        string
	}{
		{name: "own fault", pods: []sim.Vehicle{pod("a", sim.TrackOccupied, "b"), pod("b", sim.NoWait, "")},
			faults: []sim.FaultView{{ID: "i1.1", PodID: "a"}}, want: true, end: "pod a with a fault"},
		{name: "reports blocked by incident", pods: []sim.Vehicle{pod("a", sim.BlockedByIncident, "i1.1")}, faults: debris, want: true, end: `pod a, wait "Blocked by incident"`},
		{name: "reports blocked by incident with no active fault ID", pods: []sim.Vehicle{pod("a", sim.BlockedByIncident, "")}, faults: debris, want: true, end: `pod a, wait "Blocked by incident"`},
		{name: "reports no forward route", pods: []sim.Vehicle{pod("a", sim.NoForwardRoute, "")}, faults: debris, want: true, end: `pod a, wait "No forward route"`},
		{name: "chain to a pod with a fault", pods: []sim.Vehicle{pod("a", sim.TrackOccupied, "b"), pod("b", sim.TrackOccupied, "c"), pod("c", sim.FaultStopped, "")},
			faults: []sim.FaultView{{ID: "i1.1", PodID: "c"}}, want: true, end: "pod c with a fault"},
		{name: "chain to blocked by incident", pods: []sim.Vehicle{pod("a", sim.TrackOccupied, "b"), pod("b", sim.BlockedByIncident, "i1.1")}, faults: debris, want: true, end: "pod b"},
		{name: "chain to no forward route", pods: []sim.Vehicle{pod("a", sim.BerthOccupied, "b"), pod("b", sim.NoForwardRoute, "")}, faults: debris, want: true, end: "pod b"},
		{name: "chain to a fault ID", pods: []sim.Vehicle{pod("a", sim.TrackOccupied, "b"), pod("b", sim.TrackOccupied, "i1.1")}, faults: debris, want: true, end: "fault i1.1"},
		{name: "chain to an idle pod that cannot board", pods: []sim.Vehicle{pod("a", sim.BerthOccupied, "b"), idle("b")}, faults: debris, legBlocked: []string{"b"}, want: true, end: "pod b, Ready"},
		{name: "chain to a healthy pod with an ordinary wait", pods: []sim.Vehicle{pod("a", sim.TrackOccupied, "b"), pod("b", sim.BerthOccupied, "c"), pod("c", sim.NoWait, "")}, faults: debris, end: "pod c, Traveling"},
		{name: "moving pod", pods: []sim.Vehicle{pod("a", sim.NoWait, "")}, faults: debris, end: "pod a, Traveling"},
		{name: "cycle", pods: []sim.Vehicle{pod("a", sim.TrackOccupied, "b"), pod("b", sim.TrackOccupied, "c"), pod("c", sim.TrackOccupied, "b")}, faults: debris, end: "cycle at pod b"},
		{name: "cycle on itself", pods: []sim.Vehicle{pod("a", sim.TrackOccupied, "a")}, faults: debris, end: "cycle at pod a"},
		{name: "idle pod whose leg also fails with an empty blocked set", pods: []sim.Vehicle{pod("a", sim.BerthOccupied, "b"), idle("b")}, faults: debris, end: "pod b, Ready"},
		{name: "a blocker that is no pod and no active fault", pods: []sim.Vehicle{pod("a", sim.TrackOccupied, "i9.9")}, faults: debris, end: "unknown blocker i9.9"},
		{name: "faults off, ordinary wait", pods: []sim.Vehicle{pod("a", sim.TrackOccupied, "b"), idle("b")}, end: "pod b, Ready"},
		{name: "faults off, stale reports", pods: []sim.Vehicle{pod("a", sim.NoForwardRoute, "")}, end: `pod a, wait "No forward route"`},
		{name: "faults off, idle leg", pods: []sim.Vehicle{pod("a", sim.BerthOccupied, "b"), idle("b")}, legBlocked: []string{"b"}, end: "pod b, Ready"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			state := sim.Snapshot{Vehicles: test.pods, Faults: sim.FaultsView{Active: test.faults}}
			reading := newSoakReading(state, func(id string) bool { return slices.Contains(test.legBlocked, id) })
			blocked, end := reading.blockedByFault("a")
			if blocked != test.want || !strings.Contains(end, test.end) {
				t.Fatalf("got %t, end %q, want %t, end %q", blocked, end, test.want, test.end)
			}
		})
	}
}

// TestSoakOpenRecordRead checks the sum of the gate: only ticks not blocked
// count, the sum must pass the limit, and a record passes once.
func TestSoakOpenRecordRead(t *testing.T) {
	t.Parallel()
	const start = 100
	record := soakOpenRecord{last: start}
	if record.read(start+soakEmergencyTime, true) || record.unblocked != 0 {
		t.Fatalf("blocked time counts: %+v", record)
	}
	if record.read(start+2*soakEmergencyTime, false) || record.unblocked != soakEmergencyTime {
		t.Fatalf("a sum at the limit passes: %+v", record)
	}
	if record.read(start+3*soakEmergencyTime, true) || record.unblocked != soakEmergencyTime {
		t.Fatalf("blocked time after the limit counts: %+v", record)
	}
	if !record.read(start+3*soakEmergencyTime+1, false) || record.unblocked != soakEmergencyTime+1 {
		t.Fatalf("a sum past the limit does not pass: %+v", record)
	}
	if record.read(start+3*soakEmergencyTime+1+soakViewTicks, false) || record.unblocked != soakEmergencyTime+1+soakViewTicks {
		t.Fatalf("the record passes twice: %+v", record)
	}
}

// soakEmergencyOnPod runs a driver until a pod carries riders, and starts an
// emergency on it. It returns the driver and the ID of the record, after the
// first reading. The samplers do not run, so the only incidents are those of
// the test.
func soakEmergencyOnPod(t *testing.T, input soakInput) (*soakDriver, string) {
	t.Helper()
	d := newSoakDriver(t, input)
	d.s.simulation.StopIncidentSamplersForTest()
	for range soakTicksPerHour {
		d.s.advance()
		state := d.s.simulation.Snapshot()
		for index := range state.Vehicles {
			if vehicle := &state.Vehicles[index]; vehicle.Pod.Occupied && vehicle.Pod.Activity == sim.Traveling {
				id := d.client.mustApply(t, Command{Action: "emergency", PodID: vehicle.Pod.ID}).EmergencyID
				d.watchEmergencies()
				if d.open[id] == nil {
					t.Fatalf("the driver does not read the record %q: %+v", id, d.open)
				}
				return d, id
			}
		}
	}
	t.Fatal("no pod carries riders")
	return nil, ""
}

// TestSoakWatchSumsUnblockedTime checks that the watch gives the classifier's
// answer to the sum, and that it notes a late record once with its chain end.
func TestSoakWatchSumsUnblockedTime(t *testing.T) {
	t.Parallel()
	input := soakInput{preset: "rail-hub", arm: soakArms["ci"], seed: 1}
	advance := func(d *soakDriver) {
		for range soakViewTicks {
			d.s.advance()
		}
		d.watchEmergencies()
	}
	t.Run("not blocked", func(t *testing.T) {
		t.Parallel()
		d, id := soakEmergencyOnPod(t, input)
		d.open[id].unblocked = soakEmergencyTime
		advance(d)
		advance(d)
		if d.result.emergencyLate != 1 || len(d.result.misses) != 1 {
			t.Fatalf("late %d, misses %q, want one", d.result.emergencyLate, d.result.misses)
		}
		for _, part := range []string{"phase ", "wait ", "chain end pod "} {
			if !strings.Contains(d.result.misses[0], part) {
				t.Fatalf("the miss %q has no %q", d.result.misses[0], part)
			}
		}
		if d.result.emergencyLongest != d.open[id].unblocked {
			t.Fatalf("the longest is %d, want %d", d.result.emergencyLongest, d.open[id].unblocked)
		}
	})
	t.Run("blocked by a fault", func(t *testing.T) {
		t.Parallel()
		d, id := soakEmergencyOnPod(t, input)
		d.client.mustApply(t, Command{Action: "fault", PodID: d.open[id].view.PodID})
		d.open[id].unblocked = soakEmergencyTime
		advance(d)
		advance(d)
		if d.result.emergencyLate != 0 || d.open[id].unblocked != soakEmergencyTime {
			t.Fatalf("late %d, sum %d, misses %q", d.result.emergencyLate, d.open[id].unblocked, d.result.misses)
		}
	})
	t.Run("platoon member", func(t *testing.T) {
		t.Parallel()
		d := soakDriver{}
		state := sim.Snapshot{Vehicles: []sim.Vehicle{{Pod: sim.Pod{ID: "a"}, PlatoonID: "a"}}}
		d.noteLateEmergency(state, sim.EmergencyView{ID: "i1.1", PodID: "a"}, "pod a")
		if d.result.emergencyLate != 0 || d.result.emergencyInPlatoon != 1 {
			t.Fatalf("late %d, in a platoon %d", d.result.emergencyLate, d.result.emergencyInPlatoon)
		}
	})
	t.Run("late start of the checks", func(t *testing.T) {
		t.Parallel()
		d, id := soakEmergencyOnPod(t, input)
		start := d.open[id].view.StartTick
		delete(d.open, id)
		for range soakViewTicks {
			d.s.advance()
		}
		d.input.checkFrom = d.s.simulation.Tick()
		d.watchEmergencies()
		if got := d.open[id]; got == nil || got.unblocked != 0 {
			t.Fatalf("a record of tick %d read at tick %d is %+v", start, d.input.checkFrom, got)
		}
	})
	t.Run("record does not end", func(t *testing.T) {
		t.Parallel()
		// One view interval after the clear is too short for the unload.
		input := input
		input.settle = soakViewTicks
		d, id := soakEmergencyOnPod(t, input)
		d.checks = true
		d.startChecks()
		d.clearAndSettle()
		if !slices.ContainsFunc(d.result.misses, func(miss string) bool {
			return strings.Contains(miss, id+" on ") && strings.Contains(miss, "does not end")
		}) {
			t.Fatalf("the misses are %q", d.result.misses)
		}
	})
}

// TestIncidentSoakRewindOverRecord runs the rewind check over a window in which
// a sampler makes a record. The replay gives the record a new generation, and
// the check must accept only that difference. A command fault from before the
// save point must keep its generation.
func TestIncidentSoakRewindOverRecord(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		seed uint64
		next func(sim.SavedState) int64
		// records returns the number of records of the sampler of the case.
		records func(sim.SavedState) int
	}{
		{"fault", 2, func(state sim.SavedState) int64 { return *state.Faults.NextTick }, func(state sim.SavedState) int { return len(state.Faults.Records) }},
		{"emergency", 6, func(state sim.SavedState) int64 { return *state.Emergencies.NextTick }, func(state sim.SavedState) int { return len(state.Emergencies.Records) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			d := newSoakDriver(t, soakInput{preset: "rail-hub", arm: soakArms["ci"], seed: test.seed})
			next := test.next(d.s.simulation.ExportState())
			for d.s.simulation.Tick() < next-soakReplayTicks/2 {
				d.s.advance()
			}
			d.client.mustApply(t, Command{Action: "fault", PodID: d.config.Fleet[0].ID})
			start := d.s.simulation.ExportState()
			faults, generation := len(start.Faults.Records), d.s.generation
			d.checkRewind()
			state := d.s.simulation.ExportState()
			if before, after := test.records(start), test.records(state); after <= before {
				t.Fatalf("the window has no new record: %d records, then %d", before, after)
			}
			if got := state.Faults.Records[faults-1].Generation; got != generation {
				t.Fatalf("the command fault has generation %d, want %d", got, generation)
			}
			if d.s.generation != generation+1 {
				t.Fatalf("the generation is %d, want %d", d.s.generation, generation+1)
			}
		})
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
	const warmTicks, measuredTicks = 60 * sim.TicksPerSecond, 10 * sim.TicksPerSecond
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
