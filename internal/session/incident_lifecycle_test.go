package session

import (
	"bytes"
	"encoding/json/v2"
	"math"
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
)

// rateProject returns the example project with both scenario rates and no
// generated demand. A rate of 0 keeps its member, with no sampler.
func rateProject(faults, emergencies float64) project.Config {
	config := project.Default()
	addRates(&config, faults, emergencies)
	return config
}

// addRates sets the scenario rates of config. The debris share gives both
// kinds of fault, and the duration keeps a record for 90 seconds.
func addRates(config *project.Config, faults, emergencies float64) {
	config.Faults = &project.FaultConfig{PerHour: new(faults), DebrisShare: new(0.25), DebrisMeters: new(3.0),
		Duration: &project.FaultDuration{Kind: "fixed", Seconds: new(90)}}
	config.Emergencies = &project.EmergencyConfig{PerHour: new(emergencies)}
}

// rateSession returns a new session of config.
func rateSession(t *testing.T, config project.Config) *Session {
	t.Helper()
	s, err := NewWithProject(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

// restoredRateSession returns the session of a real start from the save file
// with attempts as its restore count: 0 allows the physical tier, and 1
// allows only the logical tier.
func restoredRateSession(t *testing.T, file stateFile, attempts int, want sim.RestoreTier) *Session {
	t.Helper()
	file.RestoreAttempts = attempts
	s := startFromStore(t, StoreInput{Store: &fakeStore{data: encodeTestState(t, file)}})
	t.Cleanup(s.Close)
	if s.restore.Tier != string(want) {
		t.Fatalf("restore %+v, want the tier %s", s.restore, want)
	}
	return s
}

// samplerMembers are the saved stream and next tick of one sampler.
type samplerMembers struct {
	random   []byte
	nextTick int64
}

// firstMembers returns the members of a sampler that starts at tick with
// seed, computed by the text of section 5.1.1 of the incident stage 7
// contract.
func firstMembers(t *testing.T, seed [2]uint64, perHour float64, tick int64) samplerMembers {
	t.Helper()
	stream := rand.NewPCG(seed[0], seed[1])
	u := float64(stream.Uint64()<<11>>11) / (1 << 53)
	random, err := stream.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	return samplerMembers{random, tick + max(1, int64(math.Ceil(-math.Log(1-u)*216000/perHour)))}
}

// checkFirstMembers checks that s has a sampler of each rate of config that
// is above 0, started at tick from the seed of config, and no member for a
// rate of 0.
func checkFirstMembers(t *testing.T, s *Session, config project.Config, tick int64) {
	t.Helper()
	rates := project.EffectiveIncidentRates(config)
	state := s.simulation.ExportState()
	if state.Tick != tick {
		t.Fatalf("tick %d, want %d", state.Tick, tick)
	}
	var faultRandom, emergencyRandom []byte
	var faultNext, emergencyNext *int64
	if state.Faults != nil {
		faultRandom, faultNext = state.Faults.Random, state.Faults.NextTick
	}
	if state.Emergencies != nil {
		emergencyRandom, emergencyNext = state.Emergencies.Random, state.Emergencies.NextTick
	}
	checkOne := func(name string, seed [2]uint64, perHour float64, random []byte, next *int64) {
		t.Helper()
		if perHour == 0 {
			if random != nil || next != nil {
				t.Errorf("%s: a rate of 0 has the members %x and %v", name, random, next)
			}
			return
		}
		want := firstMembers(t, seed, perHour, tick)
		if next == nil || *next != want.nextTick || !bytes.Equal(random, want.random) {
			t.Errorf("%s: the members are %x and %v, want %x and %d", name, random, next, want.random, want.nextTick)
		}
	}
	checkOne("faults", rates.FaultSeed, rates.Faults.PerHour, faultRandom, faultNext)
	checkOne("emergencies", rates.EmergencySeed, rates.EmergenciesPerHour, emergencyRandom, emergencyNext)
}

// samplerTrace is the state of both samplers and of their counters, with no
// record: a new generation changes the record IDs.
type samplerTrace struct {
	faultRandom, emergencyRandom []byte
	faultNext, emergencyNext     *int64
	faults                       sim.FaultCounters
	emergencies                  sim.EmergencyCounters
}

// traceOf returns the sampler trace of s.
func traceOf(s *Session) samplerTrace {
	state := s.simulation.ExportState()
	var trace samplerTrace
	if faults := state.Faults; faults != nil {
		trace.faultRandom, trace.faultNext, trace.faults = faults.Random, faults.NextTick, faults.Counters
	}
	if emergencies := state.Emergencies; emergencies != nil {
		trace.emergencyRandom, trace.emergencyNext, trace.emergencies = emergencies.Random, emergencies.NextTick, emergencies.Counters
	}
	return trace
}

// advanceUntilSampled advances s until each sampler has its count of events,
// or stops the test after 200,000 ticks.
func advanceUntilSampled(t *testing.T, s *Session, faults, emergencies int64) {
	t.Helper()
	const limit = 200_000
	for range limit {
		trace := traceOf(s)
		if trace.faults.Sampled >= faults && trace.emergencies.Sampled >= emergencies {
			return
		}
		s.advance()
	}
	t.Fatalf("no %d and %d events in %d ticks: %+v", faults, emergencies, limit, traceOf(s))
}

// TestSamplerSessionStarts checks each start path of section 5.1.3 and the
// session events of section 5.1.4: session start, reset, and project apply
// start new samplers from tick 0. A demand command and an apply of the same
// project keep the stream, and the next reset uses the new seed.
func TestSamplerSessionStarts(t *testing.T) {
	t.Parallel()
	t.Run("start", func(t *testing.T) {
		t.Parallel()
		config := rateProject(60, 30)
		checkFirstMembers(t, rateSession(t, config), config, 0)
	})
	t.Run("each rate alone", func(t *testing.T) {
		t.Parallel()
		for _, config := range []project.Config{rateProject(60, 0), rateProject(0, 30)} {
			checkFirstMembers(t, rateSession(t, config), config, 0)
		}
	})
	t.Run("reset", func(t *testing.T) {
		t.Parallel()
		config := rateProject(60, 30)
		s := rateSession(t, config)
		advanceUntilSampled(t, s, 1, 1)
		newTestClient(s, "test").mustApply(t, Command{Action: "reset"})
		checkFirstMembers(t, s, config, 0)
	})
	t.Run("project apply", func(t *testing.T) {
		t.Parallel()
		config := rateProject(60, 30)
		s := rateSession(t, config)
		client := newTestClient(s, "test")
		advanceUntilSampled(t, s, 1, 1)
		client.mustApply(t, Command{Action: "pause", Paused: true})
		before := traceOf(s)
		client.mustApply(t, Command{Action: "project", ProjectRevision: s.Project().Revision, Project: &config})
		if after := traceOf(s); !reflect.DeepEqual(after, before) {
			t.Fatalf("an apply of the same project moves the samplers: %+v, want %+v", after, before)
		}
		other := rateProject(60, 30)
		other.Name = "another project"
		other.Demand.Seed = config.Demand.Seed + 1
		client.mustApply(t, Command{Action: "project", ProjectRevision: s.Project().Revision, Project: &other})
		checkFirstMembers(t, s, other, 0)
	})
	t.Run("demand keeps the stream", func(t *testing.T) {
		t.Parallel()
		config := rateProject(60, 30)
		s := rateSession(t, config)
		client := newTestClient(s, "test")
		advanceTicks(s, 600)
		before := traceOf(s)
		demand := config.Demand
		demand.Seed++
		client.mustApply(t, Command{Action: "demand", Demand: demand})
		if after := traceOf(s); !reflect.DeepEqual(after, before) {
			t.Fatalf("the demand command moves the samplers: %+v, want %+v", after, before)
		}
		client.mustApply(t, Command{Action: "reset"})
		config.Demand = demand
		checkFirstMembers(t, s, config, 0)
	})
}

// TestSamplerSessionRestoreTrace checks the restore trace of section 5.1.8.
// Each pod is at its berth and there is no demand, so the physical tier keeps
// the state as it is. The next 20 events of the restored session and their
// records are the same as in the run that did not stop.
func TestSamplerSessionRestoreTrace(t *testing.T) {
	t.Parallel()
	config := rateProject(60, 60)
	config.Demand.Enabled = false
	s := rateSession(t, config)
	advanceTicks(s, 120)
	restored := restoredRateSession(t, sessionStateFile(t, s), 0, sim.RestorePhysical)
	if result := restored.restore; result.Demoted != 0 || result.Requeued != 0 || result.Dropped != 0 {
		t.Fatalf("the fixture is not physically unchanged: %+v", result)
	}
	const events = 20
	for tick := 0; ; tick++ {
		want, got := s.simulation.ExportState(), restored.simulation.ExportState()
		// The restore gives a new generation, which changes only the record IDs.
		sameGeneration(want.Faults, want.Emergencies)
		sameGeneration(got.Faults, got.Emergencies)
		if !reflect.DeepEqual(got.Faults, want.Faults) || !reflect.DeepEqual(got.Emergencies, want.Emergencies) {
			t.Fatalf("tick %d: the groups are %+v and %+v, want %+v and %+v", want.Tick, got.Faults, got.Emergencies, want.Faults, want.Emergencies)
		}
		trace := traceOf(s)
		if trace.faults.Sampled >= events && trace.emergencies.Sampled >= events {
			return
		}
		if tick > 500_000 {
			t.Fatalf("too few events: %+v", trace)
		}
		s.advance()
		restored.advance()
	}
}

// sameGeneration clears the generation of each record of the groups.
func sameGeneration(faults *sim.SavedFaults, emergencies *sim.SavedEmergencies) {
	if faults != nil {
		for index := range faults.Records {
			faults.Records[index].Generation = 0
		}
	}
	if emergencies != nil {
		for index := range emergencies.Records {
			emergencies.Records[index].Generation = 0
		}
	}
}

// TestSamplerSaveAtRandomTick checks the save of section 5.1.8 on a live
// preset, at a tick with moving pods. The outcomes can differ after a
// restore, so the test compares the next 100 due ticks and draw states, 50 of
// each sampler, and not the outcomes.
func TestSamplerSaveAtRandomTick(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("runs for about 200,000 ticks in each of two sessions")
	}
	config := scenarios.Small()
	config.Demand.Enabled = true
	addRates(&config, 60, 60)
	s := rateSession(t, config)
	advanceTicks(s, 3607)
	restored := restoredRateSession(t, sessionStateFile(t, s), 0, sim.RestorePhysical)
	const events = 50
	want, got := dueSequence(t, s, events), dueSequence(t, restored, events)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("the restored samplers diverge: %v, want %v", got, want)
	}
}

// dueSequence advances s until each sampler has count more events. It returns
// the draw states and the next ticks at each 300 ticks: an export of each tick
// costs too much, and a stream that differs once differs in each later entry.
func dueSequence(t *testing.T, s *Session, count int64) []samplerTrace {
	t.Helper()
	start := traceOf(s)
	var list []samplerTrace
	for range 20_000 {
		trace := dueMembersOf(s)
		list = append(list, trace)
		if trace.faults.Sampled >= start.faults.Sampled+count && trace.emergencies.Sampled >= start.emergencies.Sampled+count {
			return list
		}
		advanceTicks(s, 300)
	}
	t.Fatalf("too few events: %+v", traceOf(s))
	return nil
}

// TestSamplerDemoLifecycle checks the sequence of section 5.1.3, for each
// rate alone: the logical tier of a demo save starts the samplers from the
// seed of the saved project and the restored tick. Two restores of one file
// give one stream, and a later save restores and continues it.
func TestSamplerDemoLifecycle(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name                string
		faults, emergencies float64
	}{
		{"both rates", 60, 60},
		{"fault rate", 60, 0},
		{"emergency rate", 0, 60},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config := rateProject(test.faults, test.emergencies)
			s := rateSession(t, config)
			newTestClient(s, "test").mustApply(t, Command{Action: "demo"})
			advanceTicks(s, 300)
			file := sessionStateFile(t, s)
			tick := file.Simulation.Tick
			if file.Simulation.Demo == nil || file.Simulation.Faults != nil || file.Simulation.Emergencies != nil {
				t.Fatalf("the demo save has the groups %+v and %+v", file.Simulation.Faults, file.Simulation.Emergencies)
			}

			// Step 1: an ordinary fleet at the tick of the save, also in a
			// second restore of the same file.
			first := restoredRateSession(t, file, 1, sim.RestoreLogical)
			second := restoredRateSession(t, file, 1, sim.RestoreLogical)
			for _, restored := range []*Session{first, second} {
				checkFirstMembers(t, restored, config, tick)
				if state := restored.simulation.ExportState(); len(state.Pods) != len(config.Fleet) || state.Demo != nil {
					t.Fatalf("the fleet after the tier has %d pods, demo %+v", len(state.Pods), state.Demo)
				}
			}

			// Step 2: events come due and the counters move.
			var faults, emergencies int64
			if test.faults > 0 {
				faults = 2
			}
			if test.emergencies > 0 {
				emergencies = 2
			}
			advanceUntilSampled(t, first, faults, emergencies)
			frame := first.State().Simulation
			trace := traceOf(first)
			if frame.Faults.Counters != trace.faults || frame.Emergencies.Counters != trace.emergencies {
				t.Fatalf("the frame counters are %+v and %+v, the saved counters %+v and %+v", frame.Faults.Counters, frame.Emergencies.Counters, trace.faults, trace.emergencies)
			}

			// Step 3: the save is not a demo save, and it has the members.
			again := sessionStateFile(t, first)
			if again.Simulation.Demo != nil || len(again.Simulation.Pods) != len(config.Fleet) {
				t.Fatalf("the second save is a demo save: demo %+v, %d pods", again.Simulation.Demo, len(again.Simulation.Pods))
			}
			if test.faults > 0 && (again.Simulation.Faults == nil || again.Simulation.Faults.Random == nil || again.Simulation.Faults.NextTick == nil) {
				t.Fatalf("the second save has the faults %+v", again.Simulation.Faults)
			}
			if test.emergencies > 0 && (again.Simulation.Emergencies == nil || again.Simulation.Emergencies.Random == nil || again.Simulation.Emergencies.NextTick == nil) {
				t.Fatalf("the second save has the emergencies %+v", again.Simulation.Emergencies)
			}

			// Steps 4 and 5: each tier installs the stream and keeps the
			// counters, and the same events come due. Outcomes can differ,
			// because no tier promises the same motion of a moving fleet.
			physical := restoredRateSession(t, again, 0, sim.RestorePhysical)
			logical := restoredRateSession(t, again, 1, sim.RestoreLogical)
			if got, want := traceOf(physical), traceOf(first); !reflect.DeepEqual(got, want) {
				t.Fatalf("the physical tier gives %+v, want %+v", got, want)
			}
			if got, want := traceOf(logical), traceOf(first); !reflect.DeepEqual(got, want) {
				t.Fatalf("the logical tier gives %+v, want %+v", got, want)
			}
			for range 8000 {
				first.advance()
				physical.advance()
				logical.advance()
				if got, want := dueMembersOf(physical), dueMembersOf(first); !reflect.DeepEqual(got, want) {
					t.Fatalf("tick %d: the physical tier gives %+v, want %+v", first.simulation.Tick(), got, want)
				}
				if got, want := dueMembersOf(logical), dueMembersOf(first); !reflect.DeepEqual(got, want) {
					t.Fatalf("tick %d: the logical tier gives %+v, want %+v", first.simulation.Tick(), got, want)
				}
			}
		})
	}
}

// dueMembersOf returns what does not depend on the state of the fleet: the
// draw states, the next ticks, and the counts of events.
func dueMembersOf(s *Session) samplerTrace {
	trace := traceOf(s)
	trace.faults = sim.FaultCounters{Sampled: trace.faults.Sampled}
	trace.emergencies = sim.EmergencyCounters{Sampled: trace.emergencies.Sampled}
	return trace
}

// TestSamplerDemoProjectDemand checks the rule of section 5.1.3 for a demand
// seed of a project file: the sampler of a demo save uses the seed of the
// saved project, and the next reset uses the seed of the project file.
func TestSamplerDemoProjectDemand(t *testing.T) {
	t.Parallel()
	config := rateProject(60, 60)
	s := rateSession(t, config)
	newTestClient(s, "test").mustApply(t, Command{Action: "demo"})
	advanceTicks(s, 300)
	file := sessionStateFile(t, s)
	file.RestoreAttempts = 1
	other := rateProject(60, 60)
	other.Demand.Seed = config.Demand.Seed + 1
	restored := startFromStore(t, StoreInput{Store: &fakeStore{data: encodeTestState(t, file)}, Project: &other})
	t.Cleanup(restored.Close)
	if restored.restore.Tier != string(sim.RestoreLogical) || restored.project.Demand.Seed != other.Demand.Seed {
		t.Fatalf("restore %+v, demand seed %d", restored.restore, restored.project.Demand.Seed)
	}
	checkFirstMembers(t, restored, config, file.Simulation.Tick)
	newTestClient(restored, "after").mustApply(t, Command{Action: "reset"})
	checkFirstMembers(t, restored, other, 0)
}

// TestSamplerDemoPhysical checks the physical tier of a demo save with a rate:
// the demo stays, with no sampler and no member. The next save restores, and a
// reset starts a sampler. The demo command also stops a live sampler.
func TestSamplerDemoPhysical(t *testing.T) {
	t.Parallel()
	config := rateProject(60, 60)
	s := rateSession(t, config)
	if state := s.simulation.ExportState(); state.Faults == nil || state.Emergencies == nil {
		t.Fatal("no live sampler before the demo")
	}
	newTestClient(s, "test").mustApply(t, Command{Action: "demo"})
	checkNoMembers := func(s *Session) {
		t.Helper()
		if state := s.simulation.ExportState(); state.Faults != nil || state.Emergencies != nil {
			t.Fatalf("the demo fleet has the groups %+v and %+v", state.Faults, state.Emergencies)
		}
	}
	checkNoMembers(s)
	advanceTicks(s, 300)
	restored := restoredRateSession(t, sessionStateFile(t, s), 0, sim.RestorePhysical)
	checkNoMembers(restored)
	advanceTicks(restored, 300)
	checkNoMembers(restored)
	again := restoredRateSession(t, sessionStateFile(t, restored), 0, sim.RestorePhysical)
	checkNoMembers(again)
	newTestClient(again, "after").mustApply(t, Command{Action: "reset"})
	checkFirstMembers(t, again, config, 0)
}

// TestSamplerExpressSmoke runs both rates on the Express order contract, which
// has no byte proof, then saves and restores.
func TestSamplerExpressSmoke(t *testing.T) {
	t.Parallel()
	config := expressConsumerProject(t)
	addRates(&config, 60, 60)
	s := rateSession(t, config)
	advanceUntilSampled(t, s, 3, 3)
	raw, err := json.Marshal(s.Frame())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"sampled":`)) {
		t.Errorf("the frame has no sampled counter: %.200s", raw)
	}
	restored := restoredRateSession(t, sessionStateFile(t, s), 0, sim.RestorePhysical)
	if got, want := traceOf(restored), traceOf(s); !reflect.DeepEqual(got, want) {
		t.Fatalf("the restored samplers are %+v, want %+v", got, want)
	}
	for range 3000 {
		s.advance()
		restored.advance()
	}
	if got, want := dueMembersOf(restored), dueMembersOf(s); !reflect.DeepEqual(got, want) {
		t.Fatalf("the restored samplers are %+v, want %+v", got, want)
	}
}

// TestSamplerSessionLogicalTier checks the logical tier with a live sampler:
// the stream stays, the records end, and the counters stay.
func TestSamplerSessionLogicalTier(t *testing.T) {
	t.Parallel()
	config := rateProject(60, 60)
	s := rateSession(t, config)
	for tick := 0; ; tick++ {
		if state := s.simulation.ExportState(); state.Faults != nil && len(state.Faults.Records) > 0 {
			break
		}
		if tick > 200_000 {
			t.Fatal("no fault record")
		}
		s.advance()
	}
	want := traceOf(s)
	restored := restoredRateSession(t, sessionStateFile(t, s), 1, sim.RestoreLogical)
	state := restored.simulation.ExportState()
	if len(state.Faults.Records) != 0 {
		t.Fatalf("the logical tier keeps the records %+v", state.Faults.Records)
	}
	if got := traceOf(restored); !reflect.DeepEqual(got, want) {
		t.Fatalf("the logical tier gives %+v, want %+v", got, want)
	}
	for range 4000 {
		s.advance()
		restored.advance()
	}
	if got, want := dueMembersOf(restored), dueMembersOf(s); !reflect.DeepEqual(got, want) {
		t.Fatalf("after the restore, the draws are %+v, want %+v", got, want)
	}
}

// TestSamplerRefusalsAtLoad checks the refusal tables and the explicit-zero
// table of section 4.3 through the whole load, with a project rate. Each save
// is invalid_state before either tier. It moves aside, and the new session
// has new samplers from tick 0.
func TestSamplerRefusalsAtLoad(t *testing.T) {
	t.Parallel()
	config := rateProject(60, 60)
	s := rateSession(t, config)
	advanceUntilSampled(t, s, 1, 1)
	base := sessionStateFile(t, s)
	tick := base.Simulation.Tick
	valid := *base.Simulation.Faults.NextTick
	validEmergency := *base.Simulation.Emergencies.NextTick
	random := samplerRandom(t, 5)
	for _, test := range []struct {
		name string
		edit func(*sim.SavedState)
		want string
	}{
		{"fault row 1", func(state *sim.SavedState) { state.Faults = nil }, "invalid saved fault: a rate above 0 needs the sampler members"},
		{"fault row 5", func(state *sim.SavedState) { state.Faults.Random = nil }, "invalid saved fault: random and nextTick need each other"},
		{"fault row 7", func(state *sim.SavedState) { state.Faults.Random, state.Faults.NextTick = nil, nil }, "invalid saved fault: a rate above 0 needs the sampler members"},
		{"fault row 8", func(state *sim.SavedState) { state.Faults.Random = []byte{} }, "invalid saved fault: the random source is not valid"},
		{"fault row 8, a wrong size", func(state *sim.SavedState) { state.Faults.Random = random[:19] }, "invalid saved fault: the random source is not valid"},
		{"fault row 8 before row 9", func(state *sim.SavedState) { state.Faults.Random, state.Faults.NextTick = []byte{}, new(int64) },
			"invalid saved fault: the random source is not valid"},
		{"fault row 9, 0", func(state *sim.SavedState) { state.Faults.NextTick = new(int64) }, "invalid saved fault: nextTick 0 is outside "},
		{"fault row 9, the saved tick", func(state *sim.SavedState) { state.Faults.NextTick = new(tick) }, "invalid saved fault: nextTick "},
		{"emergency row 2", func(state *sim.SavedState) { state.Emergencies.NextTick = nil }, "invalid saved emergency: random and nextTick need each other"},
		{"emergency row 4", func(state *sim.SavedState) { state.Emergencies = nil }, "invalid saved emergency: a rate above 0 needs the sampler members"},
		{"emergency row 4, no sampler member", func(state *sim.SavedState) { state.Emergencies.Random, state.Emergencies.NextTick = nil, nil },
			"invalid saved emergency: a rate above 0 needs the sampler members"},
		{"emergency row 5", func(state *sim.SavedState) { state.Emergencies.Random = []byte{} }, "invalid saved emergency: the random source is not valid"},
		{"emergency row 6, 0", func(state *sim.SavedState) { state.Emergencies.NextTick = new(int64) }, "invalid saved emergency: nextTick 0 is outside "},
		{"emergency row 6, the saved tick", func(state *sim.SavedState) { state.Emergencies.NextTick = new(tick) }, "invalid saved emergency: nextTick "},
		{"fault rows before emergency rows", func(state *sim.SavedState) { state.Faults, state.Emergencies = nil, nil }, "invalid saved fault: a rate above 0 needs the sampler members"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			for _, attempts := range []int{0, 1} {
				file := decodeTestState(t, encodeTestState(t, base))
				file.RestoreAttempts = attempts
				test.edit(&file.Simulation)
				data := encodeTestState(t, file)
				_, err := newTestSession(t).loadState(loadInput{data: data, steps: realRestoreSteps()})
				if stateReason(err) != reasonInvalidState || !strings.Contains(err.Error(), test.want) {
					t.Fatalf("attempts %d: %v, want %q", attempts, err, test.want)
				}
				store := &fakeStore{data: data}
				moved, err := NewFromStore(t.Context(), StoreInput{Store: store, Project: &config})
				assertMovedAside(t, moved, err, store, reasonInvalidState)
				checkFirstMembers(t, moved, config, 0)
			}
		})
	}
	// The control: each edit starts from a save that loads, also with each
	// valid form of nextTick.
	for name, edit := range map[string]func(*sim.SavedState){
		"no edit":        func(*sim.SavedState) {},
		"terminal":       func(state *sim.SavedState) { state.Faults.NextTick = new(int64(sim.MaxCounter)) },
		"terminal other": func(state *sim.SavedState) { state.Emergencies.NextTick = new(int64(sim.MaxCounter)) },
		"first tick":     func(state *sim.SavedState) { state.Faults.NextTick = new(tick + 1) },
		"same value": func(state *sim.SavedState) {
			state.Faults.NextTick, state.Emergencies.NextTick = new(valid), new(validEmergency)
		},
	} {
		file := decodeTestState(t, encodeTestState(t, base))
		edit(&file.Simulation)
		if _, err := newTestSession(t).loadState(loadInput{data: encodeTestState(t, file), steps: realRestoreSteps()}); err != nil {
			t.Errorf("control %s: %v", name, err)
		}
	}
}

// TestSamplerExplicitZeroAtLoad checks the rows that need a project with a
// rate of 0, and each explicit-zero case of section 4.3.
func TestSamplerExplicitZeroAtLoad(t *testing.T) {
	t.Parallel()
	random := samplerRandom(t, 5)
	counter := sim.FaultCounters{Started: 1, Cleared: 1}
	emergencyCounter := sim.EmergencyCounters{Started: 1, Ended: 1}
	for _, test := range []struct {
		name                string
		faults, emergencies float64
		edit                func(*sim.SavedState)
		want                string
	}{
		{"fault rate 0, a counter, and only nextTick 0", 0, 60, func(state *sim.SavedState) {
			state.Faults = &sim.SavedFaults{Counters: counter, NextTick: new(int64)}
		}, "invalid saved fault: random and nextTick need each other"},
		{"fault rate 0, a valid random, and nextTick 0", 0, 60, func(state *sim.SavedState) {
			state.Faults = &sim.SavedFaults{Random: random, NextTick: new(int64)}
		}, "invalid saved fault: the sampler members need a rate above 0"},
		{"fault rate above 0, a valid random, and nextTick 0", 60, 60, func(state *sim.SavedState) {
			state.Faults.Random, state.Faults.NextTick = random, new(int64)
		}, "invalid saved fault: nextTick 0 is outside "},
		{`fault rate above 0, "random":"", and a valid nextTick`, 60, 60, func(state *sim.SavedState) {
			state.Faults.Random = []byte{}
		}, "invalid saved fault: the random source is not valid"},
		{"emergency rate 0, a counter, and only nextTick 0", 60, 0, func(state *sim.SavedState) {
			state.Emergencies = &sim.SavedEmergencies{Counters: emergencyCounter, NextTick: new(int64)}
		}, "invalid saved emergency: random and nextTick need each other"},
		{"emergency rate 0, a valid random, and nextTick 0", 60, 0, func(state *sim.SavedState) {
			state.Emergencies = &sim.SavedEmergencies{Random: random, NextTick: new(int64)}
		}, "invalid saved emergency: the sampler members need a rate above 0"},
		{"emergency rate above 0, a valid random, and nextTick 0", 60, 60, func(state *sim.SavedState) {
			state.Emergencies.Random, state.Emergencies.NextTick = random, new(int64)
		}, "invalid saved emergency: nextTick 0 is outside "},
		{`emergency rate above 0, "random":"", and a valid nextTick`, 60, 60, func(state *sim.SavedState) {
			state.Emergencies.Random = []byte{}
		}, "invalid saved emergency: the random source is not valid"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s := rateSession(t, rateProject(test.faults, test.emergencies))
			advanceTicks(s, 10)
			file := sessionStateFile(t, s)
			file.RestoreAttempts = 0
			test.edit(&file.Simulation)
			_, err := newTestSession(t).loadState(loadInput{data: encodeTestState(t, file), steps: realRestoreSteps()})
			if stateReason(err) != reasonInvalidState || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("%v, want %q", err, test.want)
			}
		})
	}
}

// TestSamplerDemandPairs checks the demand pair sequence with the samplers on
// and off. The streams are separate, and a demand pair is drawn before the
// offer reads the state, so the two runs draw the same pairs.
func TestSamplerDemandPairs(t *testing.T) {
	t.Parallel()
	config := project.Default()
	config.Demand.Enabled = true
	withRates := rateProject(60, 60)
	withRates.Demand = config.Demand
	off, on := rateSession(t, config), rateSession(t, withRates)
	sampled := func() int64 { return traceOf(on).faults.Sampled }
	for tick := range 12_000 {
		off.advance()
		on.advance()
		offRandom, err := off.demand.pcg.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		onRandom, err := on.demand.pcg.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(offRandom, onRandom) {
			t.Fatalf("tick %d: the demand streams differ", tick)
		}
		if got, want := on.demand.state.Generated+on.demand.state.Skipped, off.demand.state.Generated+off.demand.state.Skipped; got != want {
			t.Fatalf("tick %d: %d offers, want %d", tick, got, want)
		}
	}
	if sampled() == 0 || off.demand.state.Generated == 0 {
		t.Fatalf("the fixture has %d events and %d generated orders", sampled(), off.demand.state.Generated)
	}
}

// TestSamplerRewindAndReset checks section 5.1.4: a rewind replays the same
// draws, and a reset replays the run from tick 0.
func TestSamplerRewindAndReset(t *testing.T) {
	t.Parallel()
	const ticks = 12_000
	run := func(t *testing.T, s *Session) []samplerTrace {
		t.Helper()
		list := make([]samplerTrace, 0, ticks/100)
		for tick := range ticks {
			s.advance()
			if tick%100 == 0 {
				list = append(list, traceOf(s))
			}
		}
		if last := list[len(list)-1]; last.faults.Sampled == 0 || last.emergencies.Sampled == 0 {
			t.Fatalf("the fixture has too few events: %+v", last)
		}
		return list
	}
	t.Run("rewind", func(t *testing.T) {
		t.Parallel()
		config := rateProject(60, 60)
		config.Demand.Enabled = false
		s := rateSession(t, config)
		client := newTestClient(s, "test")
		advanceTicks(s, 300)
		id := client.mustApply(t, Command{Action: "checkpoint"}).Checkpoint
		want := run(t, s)
		client.mustApply(t, Command{Action: "rewind", Checkpoint: id})
		client.mustApply(t, Command{Action: "pause", Paused: false})
		if got := run(t, s); !reflect.DeepEqual(got, want) {
			t.Fatalf("the rewind gives other draws: %+v, want %+v", got[0], want[0])
		}
	})
	t.Run("reset", func(t *testing.T) {
		t.Parallel()
		config := rateProject(60, 60)
		config.Demand.Enabled = false
		s := rateSession(t, config)
		want := run(t, s)
		newTestClient(s, "test").mustApply(t, Command{Action: "reset"})
		if got := run(t, s); !reflect.DeepEqual(got, want) {
			t.Fatalf("the reset gives other draws: %+v, want %+v", got[0], want[0])
		}
	})
}

// TestSamplerLiveFrames checks that the frames of a live run carry the sampler
// counters and no sampler member.
func TestSamplerLiveFrames(t *testing.T) {
	t.Parallel()
	s := rateSession(t, rateProject(60, 60))
	advanceUntilSampled(t, s, 2, 2)
	frame := s.State().Simulation
	if want := traceOf(s); frame.Faults.Counters != want.faults || frame.Emergencies.Counters != want.emergencies {
		t.Fatalf("the frame has %+v and %+v, the save %+v and %+v", frame.Faults.Counters, frame.Emergencies.Counters, want.faults, want.emergencies)
	}
	raw, err := json.Marshal(frame)
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range []string{`"random"`, `"nextTick"`} {
		if bytes.Contains(raw, []byte(member)) {
			t.Errorf("the frame has the member %s", member)
		}
	}
	if !bytes.Contains(raw, []byte(`"sampled":2`)) {
		t.Errorf("the frame has no sampled counter: %.200s", raw)
	}
}

// TestManualEmergencyEndCounters checks the counter gate of section 5.1.5 for
// the end of an emergency. The script of the identity digests ends no
// emergency in its ticks, so the digests cannot show this gate. With each form
// of a rate of 0, a manual emergency writes neither counter. With a live
// sampler, it counts as a sampled emergency does. The live rate is so low that
// the sampler has no event.
func TestManualEmergencyEndCounters(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		emergencies project.EmergencyConfig
		live        bool
	}{
		"empty":         {emergencies: project.EmergencyConfig{}},
		"zero":          {emergencies: project.EmergencyConfig{PerHour: new(0.0)}},
		"negative zero": {emergencies: project.EmergencyConfig{PerHour: new(math.Copysign(0, -1))}},
		"live":          {emergencies: project.EmergencyConfig{PerHour: new(1e-300)}, live: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			client := newFaultClient(t, emergencySessionProject(test.emergencies))
			s := client.session
			client.boardOrders(t, 1)
			client.mustApply(t, Command{Action: "emergency", PodID: "01"})
			client.mustApply(t, Command{Action: "pause", Paused: false})
			for tick := 0; s.State().Simulation.Emergencies.Counters.Ended == 0; tick++ {
				if tick > 100_000 {
					t.Fatal("the emergency did not end")
				}
				s.advance()
			}
			counters := s.State().Simulation.Emergencies.Counters
			if test.live {
				if counters.Sampled != 0 || counters.EndedTicks == 0 || counters.MaxTicks != counters.EndedTicks {
					t.Fatalf("a live sampler gives the counters %+v", counters)
				}
				return
			}
			if counters.EndedTicks != 0 || counters.MaxTicks != 0 {
				t.Fatalf("a rate of 0 gives the counters %+v", counters)
			}
			for where, value := range map[string]any{"save": s.simulation.ExportState().Emergencies, "frame": s.State().Simulation.Emergencies} {
				raw, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				checkIdentityGroup(t, where, raw)
			}
		})
	}
}
