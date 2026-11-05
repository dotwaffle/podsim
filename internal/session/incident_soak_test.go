package session

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
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
	// rewindEvery is the time between rewinds, or 0 for 1 simulated hour.
	rewindEvery int64
	// warm is the warm save of the run, or nil for a new session. branch
	// selects the stream of the driver, and 0 is the stream of a new session.
	warm   *soakWarm
	branch uint64
	// observe, when set, receives the result of the run. Only a test sets it.
	observe func(soakInput, soakResult)
}

// soakRange is the range of a queue size.
type soakRange struct{ low, high int }

func (r *soakRange) add(value int) {
	r.low, r.high = min(r.low, value), max(r.high, value)
}

// soakResult is the result of one run. A gate that fails stops the test, so
// the result holds only numbers to report.
type soakResult struct {
	ticks int64
	wall  time.Duration
	// threadCPU is the CPU of this run alone, and processCPU that of the whole
	// process in the same time, with each parallel run.
	threadCPU, processCPU       float64
	queue                       soakRange
	state                       sim.Snapshot
	commands, refused           int
	saves, rewinds, railRecords int
	// demoted counts the saves that fail the physical tier by the demotion
	// rule, and podsDemoted the pods that a physical restore of a save moves to
	// a berth.
	demoted, podsDemoted       int
	emergencies, emergencyLate int
	// emergencyInPlatoon counts the records that pass the limit on a platoon
	// member, and emergencyLong the records that end longer than the limit in
	// total time. emergencyLongest is the longest time not blocked by a fault
	// of any record, in ticks.
	emergencyInPlatoon, emergencyLong int
	emergencyLongest                  int64
	settleTicks                       int64
	// fullChecks counts the full checks of the run.
	fullChecks int
	// paths counts the readings of each path of the classifier of rule A, and
	// blockedRecords holds the records that ended. Only the log reads them.
	paths          soakPathCounts
	blockedRecords []soakOpenRecord
	misses         []string
	steps          []time.Duration
	// rates holds the ticks for each CPU second of this run's thread in each
	// simulated 5 minutes before the clear, to see the drift of the rate.
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
	// breakSave, when set, changes the file of each save, so a test of the driver
	// can make each restore result.
	breakSave func(*stateFile)
	// nextFault, nextEmergency, nextSave, and nextRewind are the ticks of the
	// next driver events. clears holds the command faults, by clear tick.
	nextFault, nextEmergency, nextSave, nextRewind int64
	clears                                         []soakClear
	// loopTick is the tick of the driver loop, and the driver events use it. Each
	// rewind adds its replay to the simulation tick and not to this one. A
	// branch continues the loop of its warm save.
	loopTick int64
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
	s, err := input.warm.session(t, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if input.warm != nil {
		soakReseed(s, input.seed, input.branch)
	}
	rng := rand.New(rand.NewPCG(input.seed, 0x50414b+input.branch))
	client := "soak"
	if input.warm != nil {
		client = fmt.Sprintf("soak-branch-%d", input.branch)
	}
	d := &soakDriver{t: t, input: input, config: config, s: s, client: newTestClient(s, client), rng: rng, open: map[string]*soakOpenRecord{},
		nextFault: math.MaxInt64, nextEmergency: math.MaxInt64, nextSave: math.MaxInt64, nextRewind: math.MaxInt64}
	d.result.queue = soakRange{low: math.MaxInt, high: 0}
	input.warm.resume(d)
	if input.arm.commands {
		if input.arm.faults {
			d.nextFault = d.loopTick + soakInterval(rng)
		}
		if input.arm.emergencies {
			d.nextEmergency = d.loopTick + soakInterval(rng)
		}
	}
	d.checks = input.checks && input.checkFrom == 0
	if d.checks {
		d.startChecks()
	}
	return d
}

// soakReseed gives a restored session new streams for the fault sampler, the
// emergency sampler, and demand. Each stream derives from the seed and the branch,
// so the same selection gives the same run, and branches of one warm save share
// no future sampler or demand event by the streams. The driver has its own stream.
func soakReseed(s *Session, seed, branch uint64) {
	source := rand.New(rand.NewPCG(seed, 0x42524e43+branch))
	fault := [2]uint64{source.Uint64(), source.Uint64()}
	emergency := [2]uint64{source.Uint64(), source.Uint64()}
	s.simulation.ReseedIncidentSamplersForTest(fault, emergency)
	if s.demand.pcg != nil {
		s.demand.pcg.Seed(source.Uint64(), source.Uint64())
	}
}

// soakWarm is a warm save: the encoded state of a run at the end of its warm
// time, and the part of the driver that the state does not hold.
type soakWarm struct {
	data     []byte
	loopTick int64
	clears   []soakClear
	open     map[string]soakOpenRecord
}

// soakWarmUp runs a new session for the ticks, and saves it through the save
// path. With no ticks, it returns nil, and the run starts with a new session.
func soakWarmUp(t *testing.T, input soakInput, ticks int64) *soakWarm {
	t.Helper()
	if ticks == 0 {
		return nil
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	input.hours = float64(ticks) / soakTicksPerHour
	d := newSoakDriver(t, input)
	defer d.s.Close()
	d.runHours()
	file := sessionStateFile(t, d.s)
	file.RestoreAttempts = 0
	warm := &soakWarm{data: encodeTestState(t, file), loopTick: d.loopTick, clears: d.clears, open: make(map[string]soakOpenRecord, len(d.open))}
	for id, record := range d.open {
		warm.open[id] = *record
	}
	t.Logf("warm preset=%s arm=%s seed=%d ticks=%d bytes=%d queue=%d active faults=%d emergencies=%d", input.preset, input.arm.name, input.seed, ticks, len(warm.data), d.s.simulation.PendingCount(), len(d.clears), len(d.open))
	return warm
}

// session returns a new session of the config with no warm save. With one, it
// restores the save through the restore path in a new session, and it must get
// the physical tier, or the branch does not continue the state of the warm run.
// The branch needs no later save, so it turns off the persistence, as a new
// session has it.
func (w *soakWarm) session(t *testing.T, config project.Config) (*Session, error) {
	t.Helper()
	options := []Option{WithLogger(slog.New(slog.DiscardHandler))}
	if w == nil {
		return NewWithProject(config, options...)
	}
	s, err := NewFromStore(t.Context(), StoreInput{Store: &fakeStore{data: w.data}, Options: options})
	if err != nil {
		return nil, err
	}
	s.persist.enabled.Store(false)
	if s.restore.Tier != string(sim.RestorePhysical) {
		s.Close()
		return nil, fmt.Errorf("the warm save restores in tier %q: %s", s.restore.Tier, s.restore.Reason)
	}
	return s, nil
}

// resume gives the driver the loop tick, the command faults to clear, and the
// open records of the warm save.
func (w *soakWarm) resume(d *soakDriver) {
	if w == nil {
		return
	}
	d.loopTick, d.clears = w.loopTick, slices.Clone(w.clears)
	for id, record := range w.open {
		d.open[id] = new(record)
	}
	d.result.emergencies = len(d.open)
}

// startChecks starts the checks at the current tick.
func (d *soakDriver) startChecks() {
	d.checks = true
	d.nextSave = d.loopTick + d.rng.Int64N(soakTicksPerHour) + 1
	d.nextRewind = d.loopTick + d.rng.Int64N(d.rewindEvery()) + 1
	d.transition = d.s.simulation.FaultTransitionForTest()
	if d.input.warm != nil && d.loopTick == d.input.warm.loopTick {
		// The first publish after the restore rounds the pose of a resting pod.
		d.transition = d.s.simulation.RestoredFaultTransitionForTest()
	}
	d.events = d.s.simulation.IncidentEventsForTest()
}

// rewindEvery returns the time between rewinds.
func (d *soakDriver) rewindEvery() int64 {
	return cmp.Or(d.input.rewindEvery, soakTicksPerHour)
}

// failf stops the test with the run and the tick.
func (d *soakDriver) failf(format string, args ...any) {
	d.t.Helper()
	message := fmt.Sprintf("%s %s seed %d branch %d tick %d: %s", d.input.preset, d.input.arm.name, d.input.seed, d.input.branch, d.s.simulation.Tick(), fmt.Sprintf(format, args...))
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
	if d.breakSave != nil {
		d.breakSave(&file)
	}
	var log soakRestoreLog
	restored, err := NewFromStore(d.t.Context(), StoreInput{Store: &fakeStore{data: encodeTestState(d.t, file)},
		Options: []Option{WithLogger(slog.New(&log))}})
	if err != nil {
		d.failf("the restore: %v", err)
		return
	}
	defer restored.Close()
	d.result.saves++
	switch restored.restore.Tier {
	case string(sim.RestorePhysical):
		d.result.podsDemoted += restored.restore.Demoted
	case string(sim.RestoreLogical):
		physicalError := log.physicalError()
		if restored.restore.Reason != reasonPhysicalFailed || !soakDemotionRule(physicalError) {
			d.failf("the save fails the physical tier by another rule: reason %q, error %q", restored.restore.Reason, physicalError)
			return
		}
		d.result.demoted++
		d.t.Logf("%s %s seed %d branch %d tick %d: the save demotes: %s", d.input.preset, d.input.arm.name, d.input.seed, d.input.branch, d.s.simulation.Tick(), physicalError)
	default:
		d.failf("the save starts a new session: %+v", restored.restore)
	}
	if err := restored.simulation.CheckContract(); err != nil {
		d.failf("the restored contract: %v", err)
	}
}

// soakDemotionRule reports whether the error of a physical restore is the
// demotion rule of a record: section 10.7 of the incident emergency contract, and
// section 12.7 of the incident suspension contract. The Persistence row allows
// only this failure of the tier.
func soakDemotionRule(physicalError string) bool {
	rest, ok := strings.CutPrefix(physicalError, "the restore would demote pod ")
	return ok && (strings.Contains(rest, ", which has emergency ") || strings.Contains(rest, ", which has fault "))
}

// soakRestoreLog keeps the physical error of the "Restored session" log record.
// The session does not keep the error, and only the log gives it.
type soakRestoreLog struct {
	mu    sync.Mutex
	error string
}

func (l *soakRestoreLog) Enabled(context.Context, slog.Level) bool { return true }

func (l *soakRestoreLog) Handle(_ context.Context, record slog.Record) error {
	if record.Message != "Restored session" {
		return nil
	}
	record.Attrs(func(attr slog.Attr) bool {
		if attr.Key != "physicalError" {
			return true
		}
		l.mu.Lock()
		defer l.mu.Unlock()
		l.error = attr.Value.String()
		return false
	})
	return nil
}

func (l *soakRestoreLog) WithAttrs([]slog.Attr) slog.Handler { return l }

func (l *soakRestoreLog) WithGroup(string) slog.Handler { return l }

func (l *soakRestoreLog) physicalError() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.error
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
	path, end := r.classify(podID)
	return path.blocked(), end
}

// classify returns the path of the classifier for the pod, and the end of its
// chain.
func (r soakReading) classify(podID string) (soakPath, string) {
	path, end := r.walkChain(podID)
	if len(r.faultIDs) == 0 {
		path = soakPathNotBlocked
	}
	return path, end
}

// soakPath is one path of the classifier of rule A.
type soakPath int

const (
	// soakPathOwnFault is a pod with a fault of its own.
	soakPathOwnFault soakPath = iota
	// soakPathIncident and soakPathNoRoute are the wait reasons on the chain.
	soakPathIncident
	soakPathNoRoute
	// soakPathFaultEnd is a chain that ends at a fault ID, or at another pod that
	// has a fault.
	soakPathFaultEnd
	// soakPathLegBlocked and soakPathLegFree are a pod at the end of the chain,
	// with no blocker, that is idle: its leg is blocked, or it is not.
	soakPathLegBlocked
	soakPathLegFree
	// soakPathNotBlocked is each other reading.
	soakPathNotBlocked
	soakPathCount
)

// blocked reports whether the path gives its time to the fault.
func (p soakPath) blocked() bool {
	return p != soakPathLegFree && p != soakPathNotBlocked
}

// soakPathCounts counts the readings of each path. Its text is for the log.
type soakPathCounts [soakPathCount]int

func (c soakPathCounts) String() string {
	return fmt.Sprintf("ownFault=%d incident=%d noForwardRoute=%d faultAtChainEnd=%d legBlocked=%d legFree=%d notBlocked=%d",
		c[soakPathOwnFault], c[soakPathIncident], c[soakPathNoRoute], c[soakPathFaultEnd], c[soakPathLegBlocked], c[soakPathLegFree], c[soakPathNotBlocked])
}

// walkChain follows the BlockedBy chain from the pod. Each pod enters the walk
// once, so a cycle ends it.
func (r soakReading) walkChain(podID string) (soakPath, string) {
	seen := make(map[string]bool, 4)
	for id := podID; ; {
		pod, ok := r.pods[id]
		switch {
		case !ok:
			return soakPathNotBlocked, "unknown blocker " + id
		case seen[id]:
			return soakPathNotBlocked, "cycle at pod " + id
		case r.faultPods[id] && id == podID:
			return soakPathOwnFault, fmt.Sprintf("pod %s with a fault", id)
		case r.faultPods[id]:
			return soakPathFaultEnd, fmt.Sprintf("pod %s with a fault", id)
		case pod.WaitReason == sim.BlockedByIncident:
			return soakPathIncident, fmt.Sprintf("pod %s, wait %q, blocked by %q", id, pod.WaitReason, pod.BlockedBy)
		case pod.WaitReason == sim.NoForwardRoute:
			return soakPathNoRoute, fmt.Sprintf("pod %s, wait %q, blocked by %q", id, pod.WaitReason, pod.BlockedBy)
		case r.faultIDs[pod.BlockedBy]:
			return soakPathFaultEnd, "fault " + pod.BlockedBy
		case pod.BlockedBy == "":
			end := fmt.Sprintf("pod %s, %s, wait %q", id, pod.Activity, pod.WaitReason)
			switch {
			case r.legBlocked(id):
				return soakPathLegBlocked, end
			case pod.Activity == sim.Idle:
				return soakPathLegFree, end
			}
			return soakPathNotBlocked, end
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
	// blocked sums the other ticks, and end is the chain end of the last blocked
	// reading. Only the log reads them.
	blocked int64
	end     string
}

// read adds the ticks since the last reading. It reports true once, at the
// first reading at which the sum passes the limit of the gate.
func (r *soakOpenRecord) read(tick int64, blocked bool) bool {
	if blocked {
		r.blocked += tick - r.last
	} else {
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
		path, end := reading.classify(view.PodID)
		d.result.paths[path]++
		blocked := path.blocked()
		if blocked {
			record.end = end
		}
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
			d.result.blockedRecords = append(d.result.blockedRecords, *record)
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
	ticks := int64(d.input.hours * soakTicksPerHour)
	first := d.loopTick + 1
	window := soakWindow{cpu: soakThreadCPUSeconds(), wall: time.Now()}
	for tick := first; tick < first+ticks; tick++ {
		d.runLoopTick(tick, &window)
		d.loopTick = tick
	}
	d.result.ticks = ticks
}

// soakWindow holds the start of a rate window.
type soakWindow struct {
	cpu  float64
	wall time.Time
}

// runLoopTick runs one tick of the driver loop, with its events and its checks.
func (d *soakDriver) runLoopTick(tick int64, window *soakWindow) {
	d.t.Helper()
	if tick%soakRateWindow == 0 {
		cpu, wall := soakThreadCPUSeconds(), time.Now()
		rate := int(float64(soakRateWindow) / max(cpu-window.cpu, 0.01))
		d.result.rates = append(d.result.rates, rate)
		d.t.Logf("window %s %s seed=%d branch=%d simulated_minutes=%d checks=%t queue=%d ticks/thread_cpu_s=%d ticks/wall_s=%.0f", d.input.preset, d.input.arm.name, d.input.seed, d.input.branch, tick/(60*sim.TicksPerSecond), d.checks, d.s.simulation.PendingCount(), rate, float64(soakRateWindow)/max(wall.Sub(window.wall).Seconds(), 0.01))
		window.cpu, window.wall = cpu, wall
	}
	if d.input.checks && !d.checks && tick >= d.input.checkFrom {
		d.startChecks()
	}
	d.runEvents(tick)
	d.step()
	d.result.queue.add(d.s.simulation.PendingCount())
	if !d.checks {
		return
	}
	if tick%soakViewTicks == 0 {
		d.watchEmergencies()
	}
	if tick >= d.nextSave {
		d.nextSave += soakTicksPerHour
		d.checkSave()
	}
	if tick >= d.nextRewind {
		d.nextRewind += d.rewindEvery()
		d.checkRewind()
	}
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
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	d := newSoakDriver(t, input)
	start, startThread, startProcess := time.Now(), soakThreadCPUSeconds(), soakProcessCPUSeconds()
	d.runHours()
	d.clearAndSettle()
	d.result.wall, d.result.threadCPU, d.result.processCPU = time.Since(start), soakThreadCPUSeconds()-startThread, soakProcessCPUSeconds()-startProcess
	d.checkRail()
	d.result.fullChecks = d.fullChecks
	d.result.state = d.s.simulation.Snapshot()
	state := d.result.state
	t.Logf("preset=%s arm=%s seed=%d branch=%d hours=%g checks=%t ticks=%d wall=%.1fs threadCPU=%.1fs processCPU=%.1fs ticks/s=%.0f commands=%d refused=%d saves=%d demoted=%d podsDemoted=%d rewinds=%d rail=%d completed=%d interrupted=%d queue=%d..%d settle=%ds waitAvg=%.1fs waitMax=%.1fs faults=%+v emergencies=%+v seen=%d late=%d inPlatoon=%d long=%d longestUnblocked=%.1fs fullChecks=%d rates=%v",
		input.preset, input.arm.name, input.seed, input.branch, input.hours, input.checks, d.result.ticks, d.result.wall.Seconds(), d.result.threadCPU, d.result.processCPU, float64(d.result.ticks)/d.result.wall.Seconds(),
		d.result.commands, d.result.refused, d.result.saves, d.result.demoted, d.result.podsDemoted, d.result.rewinds, d.result.railRecords, state.Completed, state.Interrupted, d.result.queue.low, d.result.queue.high,
		d.result.settleTicks/sim.TicksPerSecond, state.Wait.AverageSeconds, state.Wait.MaxSeconds, state.Faults.Counters, state.Emergencies.Counters, d.result.emergencies, d.result.emergencyLate, d.result.emergencyInPlatoon, d.result.emergencyLong, float64(d.result.emergencyLongest)/sim.TicksPerSecond, d.fullChecks, d.result.rates)
	t.Logf("rule A preset=%s arm=%s seed=%d branch=%d readings %s longest blocked: %s", input.preset, input.arm.name, input.seed, input.branch, d.result.paths, soakLongestBlocked(d.result.blockedRecords, d.open))
	if len(d.result.misses) > 0 {
		t.Errorf("preset %s arm %s seed %d branch %d misses a Progress gate:\n%s", input.preset, input.arm.name, input.seed, input.branch, strings.Join(d.result.misses, "\n"))
	}
	if input.observe != nil {
		input.observe(input, d.result)
	}
	return d.result
}

// soakLongestBlockedRecords is the number of records of a branch in the log line
// of rule A.
const soakLongestBlockedRecords = 5

// soakLongestBlocked returns the text of the records, ended or still open, with
// the most blocked time, with their chain ends. The ID breaks a tie, so the same
// run gives the same text.
func soakLongestBlocked(ended []soakOpenRecord, open map[string]*soakOpenRecord) string {
	records := slices.Clone(ended)
	for _, record := range open {
		records = append(records, *record)
	}
	slices.SortFunc(records, func(a, b soakOpenRecord) int {
		return cmp.Or(cmp.Compare(b.blocked, a.blocked), cmp.Compare(a.view.ID, b.view.ID))
	})
	var parts []string
	for _, record := range records[:min(len(records), soakLongestBlockedRecords)] {
		if record.blocked > 0 {
			parts = append(parts, fmt.Sprintf("%s on %s for %.1fs, end %s", record.view.ID, record.view.PodID, float64(record.blocked)/sim.TicksPerSecond, record.end))
		}
	}
	return strings.Join(parts, "; ")
}

// soakThreadCPUSeconds returns the CPU time of the current thread, in seconds,
// or 0 with no /proc file. A run locks its goroutine to its thread, so this is
// the CPU of that run alone, with no garbage collection, which other threads
// do.
func soakThreadCPUSeconds() float64 {
	return soakStatFile("/proc/thread-self/stat")
}

// soakProcessCPUSeconds returns the CPU time of the process, in seconds, or 0
// with no /proc file. It includes the garbage collection and each parallel run.
func soakProcessCPUSeconds() float64 {
	return soakStatFile("/proc/self/stat")
}

func soakStatFile(path string) float64 {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	return soakStatCPU(string(raw))
}

// soakStatCPU returns the CPU seconds of the text of a /proc stat file, or 0 for
// other text.
func soakStatCPU(raw string) float64 {
	_, fields, ok := strings.Cut(raw, ") ")
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

// soakShape is the size of a soak on one preset.
type soakShape struct {
	// seeds is the number of seeds. warmTicks is the warm time of a seed, and
	// each of its branches restores the one warm save. With no warm time, a
	// run starts with a new session.
	seeds     uint64
	warmTicks int64
	branches  int
	// hours is the length of a branch, before the clear.
	hours       float64
	rewindEvery int64
	arms        []string
}

// soakAllArms lists each arm of soakGroups.
var soakAllArms = []string{"incidents", "emergencies", "platoons", "reference", "reference-platoons"}

// soakShapeOf returns the shape of the name for a preset. "branches" is the
// routine shape of amendment A10. "seeds" is the shape of amendment A6, which
// A10 keeps for the same triggers as the every-tick mode.
func soakShapeOf(name, preset string) (soakShape, error) {
	switch name {
	case "branches":
		const warm, rewind = 30 * 60 * sim.TicksPerSecond, 10 * 60 * sim.TicksPerSecond
		if preset == "london-central" {
			return soakShape{seeds: 1, warmTicks: warm, branches: 3, hours: 1, rewindEvery: rewind, arms: []string{"incidents", "emergencies", "reference"}}, nil
		}
		return soakShape{seeds: 1, warmTicks: warm, branches: 10, hours: 1.5, rewindEvery: rewind, arms: soakAllArms}, nil
	case "seeds":
		return soakShape{seeds: 5, branches: 1, hours: 6, rewindEvery: soakTicksPerHour, arms: soakAllArms}, nil
	}
	return soakShape{}, fmt.Errorf("unknown soak shape %q", name)
}

// soakEnvNumber returns the value of an environment number, or fallback when
// the name is not set.
func soakEnvNumber[N uint64 | int64 | float64](t *testing.T, name string, fallback N) N {
	t.Helper()
	text := os.Getenv(name)
	if text == "" {
		return fallback
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return N(value)
}

// TestIncidentSoak is the soak of section 5.1.9 of the incident stage 7
// contract, with the size of amendment A10. It runs only by request, because it
// takes many CPU hours. Use mise run soak, which sets the memory cap, the time
// limit, and the lock. docs/operations.md has the details.
//
// In the routine shape, each arm has one warm save at 30 simulated minutes, and
// each branch restores it in a new session and reseeds the driver, the samplers,
// and demand. A reference arm has no driver event: it runs one branch, and each
// branch of the arms of its group reads its queue range. In
// the shape of amendment A6, each arm has one new session for each seed. Each
// branch is a subtest, so a failure stops only that branch, and its log line
// holds its Progress and rule A numbers.
//
// A run that the wall limit stops counts only its completed seeds. With
// PODSIM_SOAK_FINISHED set, the test adds a line to that file for each finished
// seed, at once, so the file survives the stop.
//
// These variables select the run: PODSIM_SOAK=1, PODSIM_SOAK_SHAPE ("branches"
// or "seeds"), PODSIM_SOAK_EVERY_TICK=1, PODSIM_SOAK_PRESETS, PODSIM_SOAK_ARMS,
// PODSIM_SOAK_SEEDS, PODSIM_SOAK_FIRST_SEED, PODSIM_SOAK_BRANCHES, and
// PODSIM_SOAK_HOURS, which is the length of a branch before the clear. Use
// -parallel to set the number of runs at one time. For a timing run only,
// PODSIM_SOAK_CHECKS=0 turns the checks off, and PODSIM_SOAK_SETTLE_MINUTES
// shortens the window after the clear, and PODSIM_SOAK_CHECK_FROM_HOURS starts
// the checks late, at the full queue.
func TestIncidentSoak(t *testing.T) {
	t.Parallel()
	if os.Getenv("PODSIM_SOAK") != "1" {
		t.Skip("the soak runs only with PODSIM_SOAK=1")
	}
	first := soakEnvNumber(t, "PODSIM_SOAK_FIRST_SEED", uint64(1))
	base := soakInput{checks: os.Getenv("PODSIM_SOAK_CHECKS") != "0", everyTick: os.Getenv("PODSIM_SOAK_EVERY_TICK") == "1",
		settle:    soakEnvNumber(t, "PODSIM_SOAK_SETTLE_MINUTES", int64(30)) * 60 * sim.TicksPerSecond,
		checkFrom: int64(soakEnvNumber(t, "PODSIM_SOAK_CHECK_FROM_HOURS", 0.0) * soakTicksPerHour)}
	for preset := range strings.SplitSeq(soakEnv("PODSIM_SOAK_PRESETS", "rail-hub,london-central"), ",") {
		shape, err := soakShapeOf(soakEnv("PODSIM_SOAK_SHAPE", "branches"), preset)
		if err != nil {
			t.Fatal(err)
		}
		shape.seeds = soakEnvNumber(t, "PODSIM_SOAK_SEEDS", shape.seeds)
		shape.branches = int(soakEnvNumber(t, "PODSIM_SOAK_BRANCHES", int64(shape.branches)))
		shape.hours = soakEnvNumber(t, "PODSIM_SOAK_HOURS", shape.hours)
		if text := os.Getenv("PODSIM_SOAK_ARMS"); text != "" {
			shape.arms = strings.Split(text, ",")
		}
		for seed := first; seed < first+shape.seeds; seed++ {
			input := base
			input.preset, input.seed, input.hours, input.rewindEvery = preset, seed, shape.hours, shape.rewindEvery
			finished := newSoakSeedRecord(os.Getenv("PODSIM_SOAK_FINISHED"), preset, seed, len(soakGroups))
			for _, group := range soakGroups {
				t.Run(fmt.Sprintf("%s/seed-%d/%s", preset, seed, group.reference), func(t *testing.T) {
					t.Parallel()
					t.Cleanup(func() {
						if err := finished.groupDone(t.Failed()); err != nil {
							t.Error(err)
						}
					})
					runSoakGroup(t, input, shape, group.reference, group.arms)
				})
			}
		}
	}
}

// soakSeedRecord adds one line to a file when each group of a seed is done.
type soakSeedRecord struct {
	path, preset string
	seed         uint64
	mu           sync.Mutex
	groups       int
	failed       bool
}

func newSoakSeedRecord(path, preset string, seed uint64, groups int) *soakSeedRecord {
	return &soakSeedRecord{path: path, preset: preset, seed: seed, groups: groups}
}

// groupDone records the end of a group. After the last group of the seed, it adds
// the line of the seed. A seed with a failed group is finished, and its line says
// that it failed.
func (r *soakSeedRecord) groupDone(failed bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failed = r.failed || failed
	r.groups--
	if r.groups != 0 || r.path == "" {
		return nil
	}
	file, err := os.OpenFile(r.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("record the seed: %w", err)
	}
	defer file.Close()
	if _, err := fmt.Fprintf(file, "preset=%s seed=%d failed=%t\n", r.preset, r.seed, r.failed); err != nil {
		return fmt.Errorf("record the seed: %w", err)
	}
	return nil
}

// runSoakGroup runs the reference of a group, and then a subtest for each arm.
func runSoakGroup(t *testing.T, input soakInput, shape soakShape, reference string, arms []string) {
	t.Helper()
	if slices.Contains(shape.arms, reference) {
		run := input
		run.arm, run.checks, run.everyTick, run.checkFrom = soakArms[reference], false, false, 0
		run.warm = soakWarmUp(t, run, shape.warmTicks)
		if run.warm != nil {
			run.branch = 1
		}
		result := runSoak(t, run)
		input.queue = &result.queue
	}
	for _, name := range arms {
		if !slices.Contains(shape.arms, name) {
			continue
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			run := input
			run.arm = soakArms[name]
			run.warm = soakWarmUp(t, run, shape.warmTicks)
			for index := range shape.branches {
				branch := run
				branch.branch = uint64(index)
				if branch.warm != nil {
					branch.branch++
				}
				t.Run(fmt.Sprintf("branch-%d", branch.branch), func(t *testing.T) {
					t.Parallel()
					runSoak(t, branch)
				})
			}
		})
	}
}

// TestIncidentSoakCI is the CI soak of section 5.1.9: 1 seed for 1 simulated
// hour, with the sampler only, on the rail-hub preset, and with the full check at
// each tick. The row names no preset,
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
	run := runSoak(t, soakInput{preset: preset, arm: soakArms["ci"], seed: 1, hours: 1, checks: true, everyTick: true, queue: &reference.queue})
	if ticks := int(run.ticks); run.fullChecks < ticks {
		t.Errorf("%d ticks have %d full checks", ticks, run.fullChecks)
	}
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

// TestSoakBranchesRestoreWarmSave runs a warm save with a command fault and an
// open record, and two branches. A branch starts from the state of the warm
// save, through the physical restore, with the driver state of the save. The
// reseed makes the branches differ.
func TestSoakBranchesRestoreWarmSave(t *testing.T) {
	t.Parallel()
	const warmTicks = 300 * sim.TicksPerSecond
	input := soakInput{preset: "rail-hub", arm: soakArms["incidents"], seed: 1, hours: 0.02, checks: true, rewindEvery: 60 * sim.TicksPerSecond, settle: 60 * sim.TicksPerSecond}
	warmRun := input
	warmRun.hours = float64(warmTicks) / soakTicksPerHour
	source := newSoakDriver(t, warmRun)
	source.nextFault = math.MaxInt64
	source.runHours()
	reply := source.mustApply(Command{Action: "fault", PodID: source.config.Fleet[0].ID})
	clearTick := source.loopTick + 30*sim.TicksPerSecond
	source.clears = append(source.clears, soakClear{reply.FaultID, clearTick})
	file := sessionStateFile(t, source.s)
	file.RestoreAttempts = 0
	warm := &soakWarm{data: encodeTestState(t, file), loopTick: source.loopTick, clears: source.clears, open: map[string]soakOpenRecord{"i9.9": {unblocked: 5, view: sim.EmergencyView{ID: "i9.9"}}}}
	want := source.s.simulation.ExportState()

	var states []sim.SavedState
	for branch := uint64(1); branch <= 2; branch++ {
		run := input
		run.warm, run.branch = warm, branch
		d := newSoakDriver(t, run)
		got := d.s.simulation.ExportState()
		if got.Tick != want.Tick || got.RequestID != want.RequestID || got.Completed != want.Completed || !reflect.DeepEqual(got.Waiting, want.Waiting) {
			t.Fatalf("branch %d does not start from the warm save: tick %d, want %d", branch, got.Tick, want.Tick)
		}
		// A restore rebuilds the route distance from the lane distance, so that
		// field can differ by a rounding error.
		for index := range got.Pods {
			pod, wantPod := got.Pods[index], want.Pods[index]
			if math.Abs(pod.Distance-wantPod.Distance) > 1e-9 {
				t.Fatalf("branch %d: pod %s has the distance %g, want %g", branch, pod.ID, pod.Distance, wantPod.Distance)
			}
			pod.Distance = wantPod.Distance
			if !reflect.DeepEqual(pod, wantPod) {
				t.Fatalf("branch %d: pod %s is %+v, want %+v", branch, pod.ID, pod, wantPod)
			}
		}
		if d.s.restore.Tier != string(sim.RestorePhysical) || d.s.persist.enabled.Load() {
			t.Fatalf("branch %d has the tier %q and persistence %t", branch, d.s.restore.Tier, d.s.persist.enabled.Load())
		}
		if d.loopTick != warm.loopTick || d.open["i9.9"] == nil || d.open["i9.9"].unblocked != 5 || d.result.emergencies != 1 {
			t.Fatalf("branch %d does not resume the driver: loop %d, open %+v, seen %d", branch, d.loopTick, d.open, d.result.emergencies)
		}
		if d.nextRewind > d.loopTick+run.rewindEvery || d.nextFault <= d.loopTick || d.nextEmergency <= d.loopTick {
			t.Fatalf("branch %d has the next events %d, %d, and %d from %d", branch, d.nextRewind, d.nextFault, d.nextEmergency, d.loopTick)
		}
		d.nextFault = d.loopTick + 10*sim.TicksPerSecond
		d.runHours()
		if d.result.rewinds == 0 {
			t.Fatalf("branch %d has no rewind", branch)
		}
		for _, fault := range d.s.simulation.Snapshot().Faults.Active {
			if fault.ID == reply.FaultID {
				t.Fatalf("branch %d keeps the command fault %s of the warm save past its clear", branch, fault.ID)
			}
		}
		delete(d.open, "i9.9")
		d.clearAndSettle()
		if len(d.result.misses) != 0 {
			t.Fatalf("branch %d misses: %q", branch, d.result.misses)
		}
		states = append(states, d.s.simulation.ExportState())
	}
	if reflect.DeepEqual(states[0].Pods, states[1].Pods) {
		t.Fatal("the two branches give the same run")
	}
}

// TestSoakBranchRestingPodRounding starts a branch from a warm save in which the
// saved lane distance of a faulted pod at rest differs from its route distance
// by a rounding error, as in a save of a pod with a route that the save cuts.
// The first tick publishes the rounded value, and the branch must pass.
// TestRestoredFaultTransition checks that a real move still fails.
func TestSoakBranchRestingPodRounding(t *testing.T) {
	t.Parallel()
	input := soakInput{preset: "rail-hub", arm: soakArms["incidents"], seed: 1, hours: 0.01, checks: true, rewindEvery: 3600 * sim.TicksPerSecond}
	warmRun := input
	warmRun.hours = float64(120*sim.TicksPerSecond) / soakTicksPerHour
	source := newSoakDriver(t, warmRun)
	source.nextFault, source.nextEmergency = math.MaxInt64, math.MaxInt64
	source.runHours()
	var podID string
	for _, vehicle := range source.s.simulation.Snapshot().Vehicles {
		if vehicle.Pod.Activity == sim.Traveling && vehicle.Pod.LaneID != "" && vehicle.PlatoonID == "" {
			podID = vehicle.Pod.ID
			break
		}
	}
	if podID == "" {
		t.Fatal("no pod travels")
	}
	source.mustApply(Command{Action: "fault", PodID: podID})
	for range 30 * sim.TicksPerSecond {
		source.step()
	}
	file := sessionStateFile(t, source.s)
	file.RestoreAttempts = 0
	index := slices.IndexFunc(file.Simulation.Pods, func(pod sim.SavedPod) bool { return pod.ID == podID })
	file.Simulation.Pods[index].LaneDistance += 2e-9
	warm := &soakWarm{data: encodeTestState(t, file), loopTick: source.loopTick, clears: source.clears, open: map[string]soakOpenRecord{}}
	run := input
	run.warm, run.branch = warm, 1
	branch := newSoakDriver(t, run)
	var failures []string
	branch.fail = func(message string) { failures = append(failures, message) }
	branch.check(true)
	for range 3 {
		branch.step()
	}
	if len(failures) != 0 {
		t.Fatalf("the first ticks fail: %q", failures)
	}
}

// TestSoakGroupBranches runs a small group through the test tree. The reference
// runs one branch, and each branch of the arm reads its queue range. A warm save
// that restores in another tier stops with an error.
func TestSoakGroupBranches(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	runs := map[string]soakResult{}
	queues := map[string]*soakRange{}
	input := soakInput{preset: "rail-hub", seed: 1, hours: 0.01, checks: true, rewindEvery: 10 * sim.TicksPerSecond, settle: 120 * sim.TicksPerSecond, observe: func(run soakInput, result soakResult) {
		mu.Lock()
		defer mu.Unlock()
		key := fmt.Sprintf("%s-%d", run.arm.name, run.branch)
		runs[key], queues[key] = result, run.queue
	}}
	shape := soakShape{warmTicks: 20 * sim.TicksPerSecond, branches: 2, hours: input.hours, rewindEvery: input.rewindEvery, arms: []string{"reference", "incidents"}}
	t.Cleanup(func() {
		if len(runs) != 3 {
			t.Errorf("the runs are %v", slices.Collect(maps.Keys(runs)))
			return
		}
		reference := runs["reference-1"]
		for _, key := range []string{"incidents-1", "incidents-2"} {
			if queues[key] == nil || *queues[key] != reference.queue {
				t.Errorf("%s has the queue range %v, want %v", key, queues[key], reference.queue)
			}
			if runs[key].rewinds == 0 {
				t.Errorf("%s has no rewind", key)
			}
		}
		if queues["reference-1"] != nil {
			t.Errorf("the reference has the queue range %v", queues["reference-1"])
		}
	})
	t.Run("group", func(t *testing.T) {
		t.Parallel()
		runSoakGroup(t, input, shape, "reference", []string{"incidents", "emergencies"})
	})
}

// TestSoakBranchReseed checks the streams of the branches of one warm save. The
// same selection gives the same run. Another branch has another stream for each
// sampler and for demand, and no branch continues the streams of the warm run.
func TestSoakBranchReseed(t *testing.T) {
	t.Parallel()
	input := soakInput{preset: "rail-hub", arm: soakArms["incidents"], seed: 1, hours: 0.01, checks: true, rewindEvery: 3600 * sim.TicksPerSecond}
	warm := soakWarmUp(t, input, 60*sim.TicksPerSecond)
	source, err := NewFromStore(t.Context(), StoreInput{Store: &fakeStore{data: warm.data}, Options: []Option{WithLogger(slog.New(slog.DiscardHandler))}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(source.Close)
	streams := func(s *Session) [3]string {
		state := s.simulation.ExportState()
		demand, err := s.demand.pcg.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		return [3]string{string(state.Faults.Random), string(state.Emergencies.Random), string(demand)}
	}
	run := func(branch uint64) (*soakDriver, [3]string) {
		selection := input
		selection.warm, selection.branch = warm, branch
		d := newSoakDriver(t, selection)
		at := streams(d.s)
		d.runHours()
		return d, at
	}
	first, firstStreams := run(1)
	again, againStreams := run(1)
	other, otherStreams := run(2)
	saved := streams(source)
	if firstStreams != againStreams {
		t.Fatal("the same selection has other streams")
	}
	for index, name := range []string{"fault", "emergency", "demand"} {
		if firstStreams[index] == otherStreams[index] {
			t.Errorf("two branches have the same %s stream", name)
		}
		if firstStreams[index] == saved[index] {
			t.Errorf("a branch continues the %s stream of the warm run", name)
		}
	}
	if !reflect.DeepEqual(first.s.simulation.ExportState(), again.s.simulation.ExportState()) {
		t.Fatal("the same selection gives another run")
	}
	if reflect.DeepEqual(first.s.simulation.ExportState(), other.s.simulation.ExportState()) {
		t.Fatal("two branches give the same run")
	}
}

// TestSoakSeedRecord checks that a seed has its line only after its last group,
// and that the line tells of a failure in any group.
func TestSoakSeedRecord(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "finished")
	read := func() string {
		data, err := os.ReadFile(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		return string(data)
	}
	first, second := newSoakSeedRecord(path, "rail-hub", 2, 2), newSoakSeedRecord(path, "rail-hub", 3, 2)
	for _, step := range []struct {
		record *soakSeedRecord
		failed bool
		want   string
	}{
		{first, true, ""},
		{second, false, ""},
		{second, false, "preset=rail-hub seed=3 failed=false\n"},
		{first, false, "preset=rail-hub seed=3 failed=false\npreset=rail-hub seed=2 failed=true\n"},
	} {
		if err := step.record.groupDone(step.failed); err != nil {
			t.Fatal(err)
		}
		if got := read(); got != step.want {
			t.Fatalf("the file holds %q, want %q", got, step.want)
		}
	}
}

// TestSoakReadingPaths checks each path of the classifier of rule A: the count
// of each reading, and the result that the gate reads.
func TestSoakReadingPaths(t *testing.T) {
	t.Parallel()
	pods := map[string]sim.Pod{
		"own":      {ID: "own"},
		"incident": {ID: "incident", WaitReason: sim.BlockedByIncident, BlockedBy: "f1"},
		"noRoute":  {ID: "noRoute", WaitReason: sim.NoForwardRoute},
		"faultID":  {ID: "faultID", BlockedBy: "f1"},
		"chain":    {ID: "chain", BlockedBy: "own"},
		"legOut":   {ID: "legOut", Activity: sim.Idle},
		"legOK":    {ID: "legOK", Activity: sim.Idle},
		"moving":   {ID: "moving", Activity: sim.Traveling},
		"unknown":  {ID: "unknown", BlockedBy: "gone"},
		"loopA":    {ID: "loopA", BlockedBy: "loopB"},
		"loopB":    {ID: "loopB", BlockedBy: "loopA"},
	}
	reading := soakReading{pods: pods, faultIDs: map[string]bool{"f1": true}, faultPods: map[string]bool{"own": true},
		legBlocked: func(id string) bool { return id == "legOut" }}
	tests := []struct {
		pod     string
		path    soakPath
		blocked bool
	}{
		{"own", soakPathOwnFault, true},
		{"incident", soakPathIncident, true},
		{"noRoute", soakPathNoRoute, true},
		{"faultID", soakPathFaultEnd, true},
		{"chain", soakPathFaultEnd, true},
		{"legOut", soakPathLegBlocked, true},
		{"legOK", soakPathLegFree, false},
		{"moving", soakPathNotBlocked, false},
		{"unknown", soakPathNotBlocked, false},
		{"loopA", soakPathNotBlocked, false},
	}
	var counts soakPathCounts
	for _, test := range tests {
		path, _ := reading.classify(test.pod)
		counts[path]++
		blocked, _ := reading.blockedByFault(test.pod)
		if path != test.path || blocked != test.blocked {
			t.Errorf("pod %s has the path %d, blocked %t, want %d, %t", test.pod, path, blocked, test.path, test.blocked)
		}
	}
	const want = "ownFault=1 incident=1 noForwardRoute=1 faultAtChainEnd=2 legBlocked=1 legFree=1 notBlocked=3"
	if got := counts.String(); got != want {
		t.Errorf("the counts are %s, want %s", got, want)
	}
	// With no active fault, nothing is blocked, and the path says so.
	reading.faultIDs = map[string]bool{}
	if path, _ := reading.classify("noRoute"); path != soakPathNotBlocked {
		t.Errorf("with no fault, the path is %d", path)
	}
}

// TestSoakLongestBlocked checks the text of the records: the most blocked time
// first, a fixed number of records, and no record with no blocked time.
func TestSoakLongestBlocked(t *testing.T) {
	t.Parallel()
	record := func(id string, seconds int64) soakOpenRecord {
		return soakOpenRecord{view: sim.EmergencyView{ID: id, PodID: "p" + id}, blocked: seconds * sim.TicksPerSecond, end: "end " + id}
	}
	ended := []soakOpenRecord{record("a", 1), record("b", 3), record("c", 0), record("d", 2), record("e", 2), record("f", 5)}
	open := record("g", 4)
	got := soakLongestBlocked(ended, map[string]*soakOpenRecord{"g": &open})
	want := "f on pf for 5.0s, end end f; g on pg for 4.0s, end end g; b on pb for 3.0s, end end b; d on pd for 2.0s, end end d; e on pe for 2.0s, end end e"
	if got != want {
		t.Errorf("the text is %q, want %q", got, want)
	}
}

// TestSoakCountsReadings runs the readings of a record on a pod with a fault of
// its own, through the driver. Each reading counts one time, on its path.
func TestSoakCountsReadings(t *testing.T) {
	t.Parallel()
	d := newSoakDriver(t, soakInput{preset: "rail-hub", arm: soakArms["incidents"], seed: 1, checks: true})
	d.nextFault, d.nextEmergency, d.nextSave, d.nextRewind = math.MaxInt64, math.MaxInt64, math.MaxInt64, math.MaxInt64
	var podID string
	for podID == "" {
		d.step()
		for _, vehicle := range d.s.simulation.Snapshot().Vehicles {
			if vehicle.Pod.Occupied && vehicle.Pod.Activity == sim.Traveling {
				podID = vehicle.Pod.ID
				break
			}
		}
	}
	d.mustApply(Command{Action: "emergency", PodID: podID})
	d.mustApply(Command{Action: "fault", PodID: podID})
	before := d.result.paths
	for range 4 {
		for range soakViewTicks {
			d.step()
		}
		d.watchEmergencies()
	}
	if got := d.result.paths[soakPathOwnFault] - before[soakPathOwnFault]; got < 4 {
		t.Fatalf("4 readings give %d of the own fault path: %s", got, d.result.paths)
	}
	id := d.s.simulation.Snapshot().Emergencies.Active[0].ID
	if record := d.open[id]; record == nil || record.blocked == 0 || !strings.Contains(record.end, "with a fault") {
		t.Fatalf("the record is %+v", record)
	}
}

// TestSoakFaultAtDeparture starts a fault in the tick after a pod leaves its
// berth, when it has no lane and no berth. The transition check runs at each
// tick, also after a restore, and the pod departs after the clear.
func TestSoakFaultAtDeparture(t *testing.T) {
	t.Parallel()
	d := newSoakDriver(t, soakInput{preset: "rail-hub", arm: soakArms["incidents"], seed: 1, checks: true})
	d.nextFault, d.nextEmergency, d.nextSave, d.nextRewind = math.MaxInt64, math.MaxInt64, math.MaxInt64, math.MaxInt64
	d.s.simulation.StopIncidentSamplersForTest()
	pod := func(d *soakDriver, id string) sim.Pod {
		for _, vehicle := range d.s.simulation.Snapshot().Vehicles {
			if vehicle.Pod.ID == id {
				return vehicle.Pod
			}
		}
		t.Fatalf("no pod %s", id)
		return sim.Pod{}
	}
	id := ""
	for range soakTicksPerHour {
		d.step()
		for _, vehicle := range d.s.simulation.Snapshot().Vehicles {
			if p := vehicle.Pod; p.Activity == sim.Traveling && p.LaneID == "" && p.BerthID == "" {
				id = p.ID
			}
		}
		if id != "" {
			break
		}
	}
	if id == "" {
		t.Fatal("no pod departs")
	}
	before := pod(d, id)
	fault := d.mustApply(Command{Action: "fault", PodID: id}).FaultID
	check := func(d *soakDriver) {
		t.Helper()
		for range 2 * soakViewTicks {
			d.step()
			after := pod(d, id)
			if after.LaneID != "" || after.BerthID != "" || after.LaneDistance != 0 || after.Speed != 0 || after.Position != before.Position {
				t.Fatalf("the pod moves: %+v, then %+v", before, after)
			}
		}
	}
	check(d)
	file := sessionStateFile(t, d.s)
	file.RestoreAttempts = 0
	warm := &soakWarm{data: encodeTestState(t, file), loopTick: d.loopTick, open: map[string]soakOpenRecord{}}
	restored := newSoakDriver(t, soakInput{preset: "rail-hub", arm: soakArms["incidents"], seed: 1, checks: true, warm: warm})
	restored.nextFault, restored.nextEmergency, restored.nextSave, restored.nextRewind = math.MaxInt64, math.MaxInt64, math.MaxInt64, math.MaxInt64
	restored.s.simulation.StopIncidentSamplersForTest()
	check(restored)
	restored.mustApply(Command{Action: "clearFault", FaultID: fault})
	for range 30 * soakViewTicks {
		restored.step()
		if after := pod(restored, id); after.LaneID != "" && after.Speed > 0 {
			return
		}
	}
	t.Fatalf("the pod does not depart after the clear: %+v", pod(restored, id))
}

// TestSoakCheckSave checks each result of a save. A physical restore can move an
// ordinary pod, and the driver counts the pods. A pod with a record that would
// move fails the tier by the demotion rule, and the driver counts the save and
// goes on. Each other failure of the physical tier is a miss.
func TestSoakCheckSave(t *testing.T) {
	t.Parallel()
	// loseRoute takes the route of the first traveling pod that matches.
	loseRoute := func(match func(pod *sim.SavedPod, file *stateFile) bool) func(*stateFile) {
		return func(file *stateFile) {
			pods := slices.Clone(file.Simulation.Pods)
			for index := range pods {
				if pods[index].Activity == "traveling" && match(&pods[index], file) {
					pods[index].Route = nil
					file.Simulation.Pods = pods
					return
				}
			}
			t.Fatal("no traveling pod matches")
		}
	}
	hasEmergency := func(pod *sim.SavedPod, file *stateFile) bool { return pod.Withdrawn&2 != 0 }
	ordinary := func(pod *sim.SavedPod, file *stateFile) bool { return pod.Withdrawn == 0 }
	tests := []struct {
		name                 string
		breakSave            func(*stateFile)
		demoted, podsDemoted int
		want                 string
	}{
		{name: "physical"},
		{name: "ordinary pod moves", breakSave: loseRoute(ordinary), podsDemoted: 1},
		{name: "record pod moves", breakSave: loseRoute(hasEmergency), demoted: 1},
		{name: "another physical failure", breakSave: func(file *stateFile) {
			pods := slices.Clone(file.Simulation.Pods)
			index := slices.IndexFunc(pods, func(pod sim.SavedPod) bool { return pod.Activity != "traveling" && pod.BerthID != "" })
			other := slices.IndexFunc(pods, func(pod sim.SavedPod) bool { return pod.Activity == "traveling" && pod.Withdrawn == 0 })
			pods[index].Platoon = &sim.SavedPlatoonLink{Leader: pods[other].ID, Lanes: 1}
			file.Simulation.Pods = pods
		}, want: "another rule"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := soakInput{preset: "rail-hub", arm: soakArms["incidents"], seed: 1, checks: true}
			d := newSoakDriver(t, input)
			d.nextFault, d.nextEmergency, d.nextSave, d.nextRewind = math.MaxInt64, math.MaxInt64, math.MaxInt64, math.MaxInt64
			var podID string
			for tick := int64(1); podID == "" || tick < 90*sim.TicksPerSecond; tick++ {
				d.step()
				if podID != "" {
					continue
				}
				for _, vehicle := range d.s.simulation.Snapshot().Vehicles {
					if vehicle.Pod.Occupied && vehicle.Pod.Activity == sim.Traveling {
						podID = vehicle.Pod.ID
						break
					}
				}
			}
			d.mustApply(Command{Action: "emergency", PodID: podID})
			var failure string
			d.fail = func(message string) { failure = message }
			d.breakSave = test.breakSave
			d.checkSave()
			if test.want == "" && failure != "" || test.want != "" && !strings.Contains(failure, test.want) {
				t.Fatalf("the failure is %q, want %q", failure, test.want)
			}
			if d.result.saves != 1 || d.result.demoted != test.demoted || d.result.podsDemoted != test.podsDemoted {
				t.Fatalf("the result is %+v", d.result)
			}
		})
	}
}

// TestSoakWarmNeedsPhysicalRestore refuses a warm save that demotes: a branch of
// it does not continue the state of the warm run.
func TestSoakWarmNeedsPhysicalRestore(t *testing.T) {
	t.Parallel()
	input := soakInput{preset: "rail-hub", arm: soakArms["ci"], seed: 1}
	d := newSoakDriver(t, input)
	file := sessionStateFile(t, d.s)
	logicalOnly(&file)
	warm := &soakWarm{data: encodeTestState(t, file)}
	if _, err := warm.session(t, d.config); err == nil || !strings.Contains(err.Error(), "logical") {
		t.Fatalf("the error is %v", err)
	}
}

// TestSoakStatCPU reads the two time fields of a stat file.
func TestSoakStatCPU(t *testing.T) {
	t.Parallel()
	tests := []struct {
		raw  string
		want float64
	}{
		{"12 (session.test) R 1 2 3 4 5 6 7 8 9 10 150 250 13 14 15", 4},
		{"12 (a b) R 1 2 3 4 5 6 7 8 9 10 100 0 13", 1},
		{"12 (short) R 1 2", 0},
		{"no name", 0},
	}
	for _, test := range tests {
		if got := soakStatCPU(test.raw); got != test.want {
			t.Errorf("%q gives %g, want %g", test.raw, got, test.want)
		}
	}
}

// TestSoakShape checks the sizes of amendment A10, and the size of A6 for the
// same triggers as the every-tick mode. A branch of 1.5 h and the clear of 30
// minutes is 216,000 ticks at 30 Hz.
func TestSoakShape(t *testing.T) {
	t.Parallel()
	const minute = 60 * sim.TicksPerSecond
	tests := []struct {
		name, preset string
		want         soakShape
	}{
		{"branches", "rail-hub", soakShape{seeds: 1, warmTicks: 30 * minute, branches: 10, hours: 1.5, rewindEvery: 10 * minute, arms: soakAllArms}},
		{"branches", "london-central", soakShape{seeds: 1, warmTicks: 30 * minute, branches: 3, hours: 1, rewindEvery: 10 * minute, arms: []string{"incidents", "emergencies", "reference"}}},
		{"seeds", "rail-hub", soakShape{seeds: 5, branches: 1, hours: 6, rewindEvery: 60 * minute, arms: soakAllArms}},
		{"seeds", "london-central", soakShape{seeds: 5, branches: 1, hours: 6, rewindEvery: 60 * minute, arms: soakAllArms}},
	}
	for _, test := range tests {
		got, err := soakShapeOf(test.name, test.preset)
		if err != nil || !reflect.DeepEqual(got, test.want) {
			t.Errorf("%s %s: got %+v, %v", test.name, test.preset, got, err)
		}
	}
	if _, err := soakShapeOf("other", "rail-hub"); err == nil {
		t.Error("an unknown shape gives no error")
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
