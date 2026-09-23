package session

import (
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// testClient sends commands from one client with increasing sequences.
type testClient struct {
	session  *Session
	name     string
	epoch    string
	sequence uint64
}

func newTestClient(s *Session, name string) *testClient {
	return &testClient{session: s, name: name, epoch: s.Frame().Epoch}
}

// next sets the client, the next sequence, and the epoch on command.
func (c *testClient) next(command Command) Command {
	c.sequence++
	command.Client, command.Sequence, command.Epoch = c.name, c.sequence, c.epoch
	return command
}

// mustApply applies command with the next sequence. It stops the test when
// the session rejects the command.
func (c *testClient) mustApply(t *testing.T, command Command) Reply {
	t.Helper()
	reply := c.session.Apply(c.next(command))
	if reply.Error != "" {
		t.Fatalf("%s: %s", command.Action, reply.Error)
	}
	return reply
}

func advanceTicks(s *Session, ticks int) {
	for range ticks {
		s.advance()
	}
}

func checkpointIDs(list []Checkpoint) []uint64 {
	ids := make([]uint64, len(list))
	for i, entry := range list {
		ids[i] = entry.ID
	}
	return ids
}

func TestCheckpointAndRewindCounters(t *testing.T) {
	t.Parallel()
	s := newTestSession(t)
	client := newTestClient(s, "test")
	client.mustApply(t, Command{Action: "speed", Speed: 4})
	saved := []Checkpoint{{ID: 1, Tick: 120}}
	// Each step runs in order on the same session.
	steps := []struct {
		name string
		// advance is the number of clock updates before the command.
		advance         int
		command         Command
		generationDelta uint64
		checkpoint      uint64
		paused          bool
		tick            int64
	}{
		{name: "checkpoint", advance: 30, command: Command{Action: "checkpoint"}, checkpoint: 1, tick: 120},
		{name: "rewind", advance: 30, command: Command{Action: "rewind", Checkpoint: 1}, generationDelta: 1, paused: true, tick: 120},
	}
	for _, step := range steps {
		advanceTicks(s, step.advance)
		before := s.State()
		reply := client.mustApply(t, step.command)
		after := s.State()
		if reply.Revision != before.Revision+1 || after.Revision != reply.Revision {
			t.Errorf("%s: revision %d, reply %d, want %d", step.name, after.Revision, reply.Revision, before.Revision+1)
		}
		if after.Generation != before.Generation+step.generationDelta || reply.Generation != after.Generation {
			t.Errorf("%s: generation %d, reply %d, want %d", step.name, after.Generation, reply.Generation, before.Generation+step.generationDelta)
		}
		if after.Epoch != before.Epoch || after.ProjectRevision != before.ProjectRevision || reply.ProjectRevision != before.ProjectRevision {
			t.Errorf("%s: epoch or project revision changed", step.name)
		}
		if reply.Checkpoint != step.checkpoint {
			t.Errorf("%s: reply checkpoint %d, want %d", step.name, reply.Checkpoint, step.checkpoint)
		}
		if after.Simulation.Paused != step.paused || after.Speed != 4 || after.Simulation.Tick != step.tick {
			t.Errorf("%s: paused %t, speed %d, tick %d; want %t, 4, %d",
				step.name, after.Simulation.Paused, after.Speed, after.Simulation.Tick, step.paused, step.tick)
		}
		if !reflect.DeepEqual(after.Checkpoints, saved) {
			t.Errorf("%s: checkpoints %+v, want %+v", step.name, after.Checkpoints, saved)
		}
	}
}

// replayObservation holds the session state at the end of one simulated
// second.
type replayObservation struct {
	state  State
	safety sim.SafetyObservation
	demand demandRun
}

// observeReplay reads the state that a rewind must restore exactly. It sets
// Revision and Generation to zero, because a rewind changes them.
func observeReplay(s *Session) replayObservation {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.stateWithoutNetwork()
	state.Revision, state.Generation = 0, 0
	return replayObservation{state: state, safety: s.simulation.SafetyObservation(), demand: s.demand.clone()}
}

// replayDiff names the first part of got that differs from want. It returns
// an empty string when they are equal. Clone drops route caches, so compare
// observations and never the simulations.
func replayDiff(got, want replayObservation) string {
	switch {
	case !reflect.DeepEqual(got.state, want.state):
		return "state"
	case !reflect.DeepEqual(got.safety, want.safety):
		return "safety observation"
	case !reflect.DeepEqual(got.demand, want.demand):
		return "demand stream"
	default:
		return ""
	}
}

type replayRun struct {
	session *Session
	seconds int
	// observe gets the state after each simulated second.
	observe func(replayObservation)
}

// runSeconds advances the session and observes it once each simulated
// second. At speeds that do not divide a second, it observes at the first
// clock update that reaches the second.
func runSeconds(t *testing.T, run replayRun) {
	t.Helper()
	tick := observeReplay(run.session).state.Simulation.Tick
	start := tick
	for second := range run.seconds {
		target := start + int64(second+1)*sim.TicksPerSecond
		for tick < target {
			run.session.advance()
			tick += int64(run.session.speed)
		}
		observation := observeReplay(run.session)
		if observation.state.Simulation.Tick != tick {
			t.Fatalf("the clock is at tick %d, want %d. Is the session paused?", observation.state.Simulation.Tick, tick)
		}
		run.observe(observation)
	}
}

type replayCheck struct {
	client  *testClient
	seconds int
}

// replayResult holds the observations at the save point and at the end of
// the reference run.
type replayResult struct {
	start, end replayObservation
	checkpoint uint64
	reference  []replayObservation
}

// checkReplays saves a checkpoint and records the reference run from it.
// Then it rewinds twice, and checks that each second of both replays is
// equal to the reference. The second rewind restores a clone of a clone.
// The reference runs first. If the save point shares state with the live
// run, the replay starts from a changed state and the check fails.
func checkReplays(t *testing.T, check replayCheck) replayResult {
	t.Helper()
	s := check.client.session
	result := replayResult{checkpoint: check.client.mustApply(t, Command{Action: "checkpoint"}).Checkpoint}
	result.start = observeReplay(s)
	runSeconds(t, replayRun{session: s, seconds: check.seconds, observe: func(observation replayObservation) {
		result.reference = append(result.reference, observation)
	}})
	result.end = result.reference[len(result.reference)-1]
	for replay := 1; replay <= 2; replay++ {
		if diff := replayFrom(t, replayInput{client: check.client, result: result}); diff != "" {
			t.Fatalf("replay %d: %s", replay, diff)
		}
	}
	return result
}

type replayInput struct {
	client *testClient
	result replayResult
	// change runs after the resume and before the first second. It can be nil.
	change func()
}

// replayFrom rewinds to the save point, resumes, and runs as long as the
// reference. It describes the first difference from the reference, or
// returns an empty string when every second is equal.
func replayFrom(t *testing.T, input replayInput) string {
	t.Helper()
	s := input.client.session
	input.client.mustApply(t, Command{Action: "rewind", Checkpoint: input.result.checkpoint})
	rewound := observeReplay(s)
	rewound.state.Simulation.Paused = input.result.start.state.Simulation.Paused
	if diff := replayDiff(rewound, input.result.start); diff != "" {
		return "the rewind restores a different " + diff
	}
	input.client.mustApply(t, Command{Action: "pause", Paused: false})
	if input.change != nil {
		input.change()
	}
	var diff string
	second := 0
	runSeconds(t, replayRun{session: s, seconds: len(input.result.reference), observe: func(got replayObservation) {
		want := input.result.reference[second]
		second++
		if diff == "" {
			if part := replayDiff(got, want); part != "" {
				diff = "the " + part + " differs at tick " + strconv.FormatInt(want.state.Simulation.Tick, 10)
			}
		}
	}})
	return diff
}

func TestRewindReplaysExactly(t *testing.T) {
	t.Parallel()
	balanced := project.Default()
	balanced.Demand = DemandConfig{Enabled: true, PerMinute: 12, Pattern: "balanced", Seed: 7}
	balanced.Redistribution = true
	// The demo check compares only the network and the fleet, so the demo
	// starts with redistribution in the project.
	demo := project.Default()
	demo.Redistribution = true
	tests := []struct {
		name   string
		config project.Config
		// prepare runs before the save point.
		prepare func(*testing.T, *testClient)
		seconds int
		// extraTrip is true when the continuation accepts a journey request.
		// The test then also checks that a branch with one more journey
		// differs from the reference.
		extraTrip bool
		// ran reports why the case did not use its path, or returns an empty
		// string.
		ran func(replayResult) string
	}{
		{
			name: "balanced demand", config: balanced, seconds: 30, extraTrip: true,
			prepare: func(_ *testing.T, client *testClient) { advanceTicks(client.session, 10*sim.TicksPerSecond) },
			ran: func(result replayResult) string {
				if result.end.state.Demand.Generated <= result.start.state.Demand.Generated {
					return "no generated journeys"
				}
				return ""
			},
		},
		{
			name: "profile demand", config: profileDemandProject(), seconds: 30, extraTrip: true,
			prepare: func(_ *testing.T, client *testClient) { advanceTicks(client.session, 10*sim.TicksPerSecond) },
			ran: func(result replayResult) string {
				if len(result.start.demand.profileFlows) == 0 {
					return "no profile flows"
				}
				if result.end.state.Demand.Generated <= result.start.state.Demand.Generated {
					return "no generated journeys"
				}
				return ""
			},
		},
		{
			// The demo ends at about 332 s. The advance hook then configures
			// redistribution again. During the demo, the save point has
			// redistribution off and the project has it on. A rewind that
			// configures redistribution turns it on during the demo, and the
			// replay then differs.
			name: "demo ends", config: demo, seconds: 40,
			prepare: func(t *testing.T, client *testClient) {
				t.Helper()
				client.mustApply(t, Command{Action: "demo"})
				advanceTicks(client.session, 310*sim.TicksPerSecond)
			},
			ran: func(result replayResult) string {
				if !result.start.state.Simulation.Demo || result.end.state.Simulation.Demo {
					return "the demo did not end in the continuation"
				}
				if result.start.state.Redistribution || !result.end.state.Redistribution {
					return "redistribution did not start at the end of the demo"
				}
				return ""
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, err := NewWithProject(test.config)
			if err != nil {
				t.Fatal(err)
			}
			client := newTestClient(s, "test")
			test.prepare(t, client)
			result := checkReplays(t, replayCheck{client: client, seconds: test.seconds})
			if reason := test.ran(result); reason != "" {
				t.Fatalf("the case did not use its path: %s", reason)
			}
			if !test.extraTrip {
				return
			}
			other := newTestClient(s, "other")
			diff := replayFrom(t, replayInput{client: client, result: result, change: func() {
				other.mustApply(t, Command{Action: "trip", Origin: "harbor", Destination: "garden"})
			}})
			if diff == "" {
				t.Fatal("a branch with an extra journey is equal to the reference")
			}
		})
	}
}

func TestCheckpointRejectionsPreserveState(t *testing.T) {
	t.Parallel()
	save := func(t *testing.T, client *testClient) {
		t.Helper()
		advanceTicks(client.session, 1)
		client.mustApply(t, Command{Action: "checkpoint"})
	}
	tests := []struct {
		name string
		// prepare returns the rewind target.
		prepare func(*testing.T, *testClient) uint64
		message string
	}{
		{"zero ID", func(t *testing.T, client *testClient) uint64 {
			t.Helper()
			save(t, client)
			return 0
		}, "rewind requires a save point"},
		{"unknown ID", func(t *testing.T, client *testClient) uint64 {
			t.Helper()
			save(t, client)
			return 2
		}, "save point #2 is no longer available"},
		{"evicted ID", func(t *testing.T, client *testClient) uint64 {
			t.Helper()
			for range checkpointLimit + 1 {
				save(t, client)
			}
			return 1
		}, "save point #1 is no longer available"},
		{"demand change", func(t *testing.T, client *testClient) uint64 {
			t.Helper()
			save(t, client)
			client.mustApply(t, Command{Action: "demand", Demand: DemandConfig{Enabled: true, PerMinute: 6, Pattern: "balanced", Seed: 3}})
			return 1
		}, "the project or demand settings changed after save point #1, so a rewind to it is not available"},
		{"project change", func(t *testing.T, client *testClient) uint64 {
			t.Helper()
			save(t, client)
			config := customProject()
			client.mustApply(t, Command{Action: "pause", Paused: true})
			client.mustApply(t, Command{Action: "project", ProjectRevision: client.session.Project().Revision, Project: &config})
			return 1
		}, "the project or demand settings changed after save point #1, so a rewind to it is not available"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s := newTestSession(t)
			client := newTestClient(s, "test")
			target := test.prepare(t, client)
			beforeState, beforeProject := s.State(), s.Project()
			command := client.next(Command{Action: "rewind", Checkpoint: target})
			reply := s.Apply(command)
			if reply.ErrorCode != CommandRejected || reply.Error != test.message {
				t.Fatalf("reply = %+v, want %s: %q", reply, CommandRejected, test.message)
			}
			if !reflect.DeepEqual(beforeState, s.State()) || !reflect.DeepEqual(beforeProject, s.Project()) {
				t.Fatal("the rejected rewind changed the session")
			}
			if retry := s.Apply(command); !reflect.DeepEqual(retry, reply) {
				t.Fatalf("retry = %+v, want the stored rejection %+v", retry, reply)
			}
			if !reflect.DeepEqual(beforeState, s.State()) {
				t.Fatal("the retried rejection changed the session")
			}
		})
	}
}

func TestCheckpointEvictionAndIDs(t *testing.T) {
	t.Parallel()
	s := newTestSession(t)
	client := newTestClient(s, "test")
	var issued []uint64
	save := func(name string) {
		t.Helper()
		advanceTicks(s, 1)
		id := client.mustApply(t, Command{Action: "checkpoint"}).Checkpoint
		if want := uint64(len(issued) + 1); id != want {
			t.Fatalf("%s: checkpoint ID %d, want %d", name, id, want)
		}
		issued = append(issued, id)
		want := issued[max(0, len(issued)-checkpointLimit):]
		if got := checkpointIDs(s.State().Checkpoints); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: retained IDs %v, want %v", name, got, want)
		}
	}
	for i := range checkpointLimit + 2 {
		save("save " + strconv.Itoa(i+1))
	}
	list := s.State().Checkpoints
	for i := 1; i < len(list); i++ {
		if list[i].Tick <= list[i-1].Tick {
			t.Fatalf("checkpoints are not oldest first: %+v", list)
		}
	}
	if list[0].ID != 3 || list[len(list)-1].ID != checkpointLimit+2 {
		t.Fatalf("retained IDs %v, want 3 to %d", checkpointIDs(list), checkpointLimit+2)
	}
	// Each command runs in order. A save point after each one gets the next ID.
	for _, step := range []struct {
		name    string
		command Command
	}{
		{"reset", Command{Action: "reset"}},
		{"demo", Command{Action: "demo"}},
		{"rewind", Command{Action: "rewind", Checkpoint: checkpointLimit + 3}},
		{"rewind to an older save point", Command{Action: "rewind", Checkpoint: checkpointLimit}},
	} {
		client.mustApply(t, step.command)
		if step.command.Action == "rewind" {
			client.mustApply(t, Command{Action: "pause", Paused: false})
		}
		save("after " + step.name)
	}
}

func TestCheckpointRetriesAreIdempotent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		// prepare returns the command to send and retry.
		prepare func(*testing.T, *testClient) Command
		// between changes the session before the retry.
		between func(*testing.T, *Session)
	}{
		{
			name: "checkpoint",
			prepare: func(_ *testing.T, client *testClient) Command {
				return client.next(Command{Action: "checkpoint"})
			},
			between: func(*testing.T, *Session) {},
		},
		{
			name: "rewind after resume",
			prepare: func(t *testing.T, client *testClient) Command {
				t.Helper()
				id := client.mustApply(t, Command{Action: "checkpoint"}).Checkpoint
				advanceTicks(client.session, 30)
				return client.next(Command{Action: "rewind", Checkpoint: id})
			},
			between: func(t *testing.T, s *Session) {
				t.Helper()
				newTestClient(s, "other").mustApply(t, Command{Action: "pause", Paused: false})
				advanceTicks(s, 30)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s := newTestSession(t)
			advanceTicks(s, 30)
			command := test.prepare(t, newTestClient(s, "test"))
			first := s.Apply(command)
			if first.Error != "" {
				t.Fatal(first.Error)
			}
			test.between(t, s)
			before := s.State()
			if retry := s.Apply(command); !reflect.DeepEqual(retry, first) {
				t.Fatalf("retry = %+v, want %+v", retry, first)
			}
			if after := s.State(); !reflect.DeepEqual(after, before) {
				t.Fatalf("the retry changed the session: tick %d to %d, generation %d to %d, checkpoints %v to %v",
					before.Simulation.Tick, after.Simulation.Tick, before.Generation, after.Generation,
					checkpointIDs(before.Checkpoints), checkpointIDs(after.Checkpoints))
			}
			if len(before.Checkpoints) != 1 {
				t.Fatalf("checkpoints = %v, want one", checkpointIDs(before.Checkpoints))
			}
		})
	}
}

func TestCheckpointConcurrentWithClock(t *testing.T) {
	t.Parallel()
	s := newTestSession(t)
	const clients, rounds = 2, 30
	stop := make(chan struct{})
	var background, workers sync.WaitGroup
	var rewinds atomic.Uint64
	background.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
				s.advance()
			}
		}
	})
	type counters struct{ revision, generation uint64 }
	readers := map[string]func() counters{
		"State": func() counters { state := s.State(); return counters{state.Revision, state.Generation} },
		"Frame": func() counters { frame := s.Frame(); return counters{frame.Revision, frame.Generation} },
	}
	for name, read := range readers {
		background.Go(func() {
			var last counters
			for {
				select {
				case <-stop:
					return
				default:
				}
				got := read()
				if got.revision < last.revision || got.generation < last.generation {
					t.Errorf("%s went from %+v to %+v", name, last, got)
					return
				}
				last = got
			}
		})
	}
	for i := range clients {
		workers.Go(func() {
			client := newTestClient(s, "client-"+strconv.Itoa(i))
			for range rounds {
				saved := s.Apply(client.next(Command{Action: "checkpoint"}))
				resumed := s.Apply(client.next(Command{Action: "pause", Paused: false}))
				rewound := s.Apply(client.next(Command{Action: "rewind", Checkpoint: saved.Checkpoint}))
				paused := s.Apply(client.next(Command{Action: "pause", Paused: i == 0}))
				for _, reply := range []Reply{saved, resumed, paused} {
					if reply.Error != "" {
						t.Errorf("client %d: %+v", i, reply)
					}
				}
				switch {
				case rewound.Error == "":
					rewinds.Add(1)
				// The other client can save enough checkpoints to evict this one.
				case !strings.HasSuffix(rewound.Error, "is no longer available"):
					t.Errorf("client %d: rewind %+v", i, rewound)
				}
			}
		})
	}
	workers.Wait()
	close(stop)
	background.Wait()
	state := s.State()
	if state.Generation != 1+rewinds.Load() {
		t.Fatalf("generation %d after %d rewinds", state.Generation, rewinds.Load())
	}
	if ids := checkpointIDs(state.Checkpoints); len(ids) != checkpointLimit || ids[len(ids)-1] != clients*rounds {
		t.Fatalf("retained IDs %v, want the last %d of %d", ids, checkpointLimit, clients*rounds)
	}
}

func TestDemandCloneIsIndependent(t *testing.T) {
	t.Parallel()
	profiled := profileDemandProject()
	input := func(config DemandConfig) demandInput {
		return demandInput{config: config, network: profiled.Network, profiles: profiled.DemandProfiles}
	}
	tests := []struct {
		name  string
		input demandInput
	}{
		{"balanced", input(DemandConfig{Enabled: true, PerMinute: 12, Pattern: "balanced", Seed: 7})},
		{"market", input(DemandConfig{Enabled: true, PerMinute: 12, Pattern: "market", Seed: 3})},
		{"profile", input(profiled.Demand)},
		{"disabled", input(DemandConfig{PerMinute: 12, Pattern: "balanced", Seed: 7})},
	}
	draw := func(run *demandRun) []string {
		pairs := make([]string, 20)
		for i := range pairs {
			from, to := run.nextPair()
			pairs[i] = from + ">" + to
		}
		return pairs
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			original, twin := newDemand(test.input), newDemand(test.input)
			draw(&original)
			draw(&twin)
			clone := original.clone()
			if clone.pcg == original.pcg || clone.rng == original.rng {
				t.Fatal("the clone shares its random source")
			}
			fromClone := draw(&clone)
			fromOriginal := draw(&original)
			want := draw(&twin)
			if !reflect.DeepEqual(fromClone, want) {
				t.Fatalf("clone draws %v, want %v", fromClone, want)
			}
			if !reflect.DeepEqual(fromOriginal, want) {
				t.Fatalf("draws from the clone changed the original: %v, want %v", fromOriginal, want)
			}
			if !reflect.DeepEqual(clone, original) {
				t.Fatal("the clone and the original differ after equal draws")
			}
		})
	}
	t.Run("zero", func(t *testing.T) {
		t.Parallel()
		var zero demandRun
		clone := zero.clone()
		if !reflect.DeepEqual(clone, zero) {
			t.Fatalf("clone of the zero stream = %+v", clone)
		}
	})
}

type rewindRule int

const (
	// rewindRestore marks state that a rewind takes from the save point. The
	// checkpoint struct holds a field with the same name and type.
	rewindRestore rewindRule = iota + 1
	// rewindKeep marks state that a rewind does not change.
	rewindKeep
	// rewindBump marks a counter that goes up by one on a rewind.
	rewindBump
	// rewindInfrastructure marks locks, shutdown, and I/O. They are not
	// simulation state.
	rewindInfrastructure
)

// sessionRewindRules gives a rule for each Session field.
var sessionRewindRules = map[string]rewindRule{
	"closed": rewindInfrastructure, "mu": rewindInfrastructure, "saveProject": rewindInfrastructure,
	"simulation": rewindRestore, "demand": rewindRestore,
	// A rewind across a project change is rejected, so the live project is
	// also the project of the save point.
	"project": rewindKeep, "projectRevision": rewindKeep,
	"epoch": rewindKeep, "speed": rewindKeep, "receipts": rewindKeep,
	"checkpoints": rewindKeep, "lastCheckpoint": rewindKeep,
	"revision": rewindBump, "generation": rewindBump,
}

type demandCloneRule int

const (
	// demandShare marks storage that code replaces whole after newDemand.
	demandShare demandCloneRule = iota + 1
	// demandCopy marks storage that a draw changes. clone gives it new storage.
	demandCopy
)

// demandRunRules gives a rule for each demandRun field that holds references.
var demandRunRules = map[string]demandCloneRule{
	"pcg": demandCopy, "rng": demandCopy,
	"passenger": demandShare, "profileFlows": demandShare, "pickupWeights": demandShare,
}

// holdsReferences reports whether a value copy of t shares storage with the
// original. Strings cannot change, so they count as plain values.
func holdsReferences(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Map, reflect.Slice, reflect.Pointer, reflect.Interface, reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return true
	case reflect.Array:
		return holdsReferences(t.Elem())
	case reflect.Struct:
		for field := range t.Fields() {
			if holdsReferences(field.Type) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func TestSessionFieldsHaveRewindRules(t *testing.T) {
	t.Parallel()
	session, saved := reflect.TypeFor[Session](), reflect.TypeFor[checkpoint]()
	for field := range session.Fields() {
		rule, ok := sessionRewindRules[field.Name]
		if !ok {
			t.Errorf("Session.%s has no rewind rule", field.Name)
			continue
		}
		if rule != rewindRestore {
			continue
		}
		if stored, ok := saved.FieldByName(field.Name); !ok || stored.Type != field.Type {
			t.Errorf("a rewind restores Session.%s, but checkpoint has no %s field of type %s", field.Name, field.Name, field.Type)
		}
	}
	for name := range sessionRewindRules {
		if _, ok := session.FieldByName(name); !ok {
			t.Errorf("the rewind rule for Session.%s names a missing field", name)
		}
	}
}

func TestDemandRunFieldsHaveCloneRules(t *testing.T) {
	t.Parallel()
	typ := reflect.TypeFor[demandRun]()
	for field := range typ.Fields() {
		if _, ok := demandRunRules[field.Name]; !ok && holdsReferences(field.Type) {
			t.Errorf("demandRun.%s holds references but has no clone rule", field.Name)
		}
	}
	config := profileDemandProject()
	original := newDemand(demandInput{config: config.Demand, network: config.Network, profiles: config.DemandProfiles})
	clone := original.clone()
	source, copied := reflect.ValueOf(original), reflect.ValueOf(clone)
	for name, rule := range demandRunRules {
		field, ok := typ.FieldByName(name)
		if !ok {
			t.Errorf("the clone rule for demandRun.%s names a missing field", name)
			continue
		}
		from, to := source.FieldByIndex(field.Index), copied.FieldByIndex(field.Index)
		if from.IsNil() {
			t.Errorf("the profile fixture has no data in demandRun.%s", name)
			continue
		}
		if shared := from.Pointer() == to.Pointer(); shared != (rule == demandShare) {
			t.Errorf("demandRun.%s: shared storage is %t, want %t", name, shared, rule == demandShare)
		}
	}
}
