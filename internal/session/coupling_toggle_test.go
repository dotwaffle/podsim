package session

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"reflect"
	"slices"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// connectedPhase is a saved fixture with a committed group in the
// connected phase and with passengers in both cabins.
const connectedPhase = "occupied-true-phase-3-leg-1.json"

// toggleSession is a paused version 5 session with a state store and a
// project saver that records each saved project.
type toggleSession struct {
	s       *Session
	store   *fakeStore
	client  *testClient
	saves   []project.Config
	saveErr error
}

// couplingExample returns the example project as a version 5 project with
// no coupling sites and no coupling corridors.
func couplingExample() project.Config {
	config := project.Default()
	config.Version, config.CouplingContract = project.CouplingVersion, sim.CompactPairV1CouplingContract
	return config
}

// newToggleSession starts a session from the saved fixture phase. With an
// empty phase, it starts couplingExample, which has no coupling groups.
func newToggleSession(t *testing.T, phase string) *toggleSession {
	t.Helper()
	if phase == "" {
		return newProjectSession(t, couplingExample())
	}
	data := couplingPhaseFixtures(t)
	index := slices.IndexFunc(data.Frames, func(frame couplingPhaseFrame) bool { return frame.Name == phase })
	if index < 0 {
		t.Fatal("unknown fixture phase", phase)
	}
	return startToggleSession(t, &fakeStore{data: encodeTestState(t, couplingPhaseFile(t, couplingPhaseInput(t, data, data.Frames[index])))}, nil)
}

// newProjectSession starts a new session of config with an empty state
// store.
func newProjectSession(t *testing.T, config project.Config) *toggleSession {
	t.Helper()
	return startToggleSession(t, &fakeStore{}, &config)
}

func startToggleSession(t *testing.T, store *fakeStore, config *project.Config) *toggleSession {
	t.Helper()
	ts := &toggleSession{store: store}
	saver := WithProjectSaver(func(config project.Config) error {
		if ts.saveErr != nil {
			return ts.saveErr
		}
		ts.saves = append(ts.saves, config)
		return nil
	})
	s, err := NewFromStore(t.Context(), StoreInput{Store: store, Project: config, Options: []Option{saver}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	ts.s, ts.client = s, newTestClient(s, "toggle")
	ts.client.mustApply(t, Command{Action: "pause", Paused: true})
	return ts
}

// apply sends the current project with CouplingEnabled inverted. edit, when
// not nil, changes the project before the command.
func (ts *toggleSession) apply(edit func(*project.Config)) Reply {
	current := ts.s.Project()
	config := current.Project
	config.CouplingEnabled = !config.CouplingEnabled
	if edit != nil {
		edit(&config)
	}
	return ts.s.Apply(ts.client.next(Command{Action: "project", Project: &config, ProjectRevision: current.Revision}))
}

// toggleKept holds the session values that a change in place keeps. The
// simulation and the demand random source must be the same values.
type toggleKept struct {
	simulation *sim.Simulation
	random     *rand.PCG
	values     toggleValues
}

type toggleValues struct {
	saved      sim.SavedState
	groups     []sim.CouplingGroupView
	tick       int64
	generation uint64
	speed      int
	demand     DemandState
	restore    RestoreInfo
	epoch      string
	checks     []Checkpoint
}

func keptValues(s *Session) toggleKept {
	s.mu.Lock()
	defer s.mu.Unlock()
	// A failed view leaves groups nil, and the group count checks fail.
	snapshot, _ := s.simulation.CheckedSnapshot()
	return toggleKept{simulation: s.simulation, random: s.demand.pcg, values: toggleValues{
		saved: s.simulation.ExportState(), groups: snapshot.CouplingGroups, tick: s.simulation.Tick(),
		generation: s.generation, speed: s.speed, demand: s.demand.state, restore: s.restore, epoch: s.epoch,
		checks: s.checkpointList(),
	}}
}

// same reports whether k and other have the same simulation, demand
// random source, and values.
func (k toggleKept) same(other toggleKept) bool {
	return k.simulation == other.simulation && k.random == other.random && reflect.DeepEqual(k.values, other.values)
}

// TestCouplingToggleInPlace checks that a project that changes only
// CouplingEnabled keeps the simulation, its groups, and the session
// counters, and saves the new project. It toggles twice at each saved phase
// and with no groups. The policy then drains the committed group without a
// fault.
func TestCouplingToggleInPlace(t *testing.T) {
	t.Parallel()
	phases := []string{""}
	for _, frame := range couplingPhaseFixtures(t).Frames {
		phases = append(phases, frame.Name)
	}
	for _, phase := range phases {
		t.Run("phase="+phase, func(t *testing.T) {
			t.Parallel()
			ts := newToggleSession(t, phase)
			ts.client.mustApply(t, Command{Action: "speed", Speed: 5})
			before := keptValues(ts.s)
			if phase != "" && len(before.values.groups) != 1 {
				t.Fatal("fixture has no committed group")
			}
			for step := range 2 {
				revision := ts.s.Project().Revision
				reply := ts.apply(nil)
				if reply.Error != "" {
					t.Fatal(reply.Error)
				}
				enabled := step == 0
				if after := keptValues(ts.s); !after.same(before) {
					t.Fatalf("toggle changed kept state:\nbefore %+v\nafter  %+v", before.values, after.values)
				}
				if reply.ProjectRevision != revision+1 || reply.Generation != before.values.generation {
					t.Fatalf("reply revisions %d/%d, want project revision %d and generation %d",
						reply.ProjectRevision, reply.Generation, revision+1, before.values.generation)
				}
				if ts.s.simulation.CouplingEnabled() != enabled || ts.s.Project().Project.CouplingEnabled != enabled {
					t.Fatal("toggle did not change the recruitment policy")
				}
				if len(ts.saves) != step+1 || ts.saves[step].CouplingEnabled != enabled {
					t.Fatal("toggle did not save the project")
				}
				if reply.StateSaved == nil || !*reply.StateSaved || ts.store.lastWrite(t).Project.CouplingEnabled != enabled {
					t.Fatal("toggle did not save the session state")
				}
			}
			ts.client.mustApply(t, Command{Action: "pause", Paused: false})
			for range 50 {
				ts.s.advance()
			}
			if err := ts.s.CouplingError(); err != nil {
				t.Fatal("policy change faulted the controller", err)
			}
		})
	}
}

// TestCouplingToggleFullReplace checks that each project change other than
// a toggle of CouplingEnabled alone still replaces the simulation and
// increases the generation.
func TestCouplingToggleFullReplace(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		phase   string
		prepare func(*testing.T, *Session)
		edit    func(*project.Config)
	}{
		{"toggle and rename", connectedPhase, nil, func(c *project.Config) { c.Name += " renamed" }},
		{"toggle and redistribution", connectedPhase, nil, func(c *project.Config) { c.Redistribution = !c.Redistribution }},
		{"toggle and demand", connectedPhase, nil, func(c *project.Config) { c.Demand.Seed++ }},
		{"toggle during demo", "", func(t *testing.T, s *Session) {
			t.Helper()
			newTestClient(s, "demo").mustApply(t, Command{Action: "demo"})
			s.mu.Lock()
			s.simulation.SetPaused(true)
			s.mu.Unlock()
		}, nil},
		{"toggle during coupling fault", connectedPhase, setViewFault, nil},
		{"same project during coupling fault", connectedPhase, setViewFault, func(c *project.Config) { c.CouplingEnabled = !c.CouplingEnabled }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ts := newToggleSession(t, test.phase)
			if test.prepare != nil {
				test.prepare(t, ts.s)
			}
			before := keptValues(ts.s)
			reply := ts.apply(test.edit)
			if reply.Error != "" {
				t.Fatal(reply.Error)
			}
			after := keptValues(ts.s)
			if after.simulation == before.simulation || after.values.generation != before.values.generation+1 || after.values.restore != (RestoreInfo{}) {
				t.Fatal("project change did not replace the simulation")
			}
			if test.phase != "" && len(after.values.groups) != 0 {
				t.Fatal("full replace kept the committed group")
			}
			if err := ts.s.CouplingError(); err != nil {
				t.Fatal("full replace kept the coupling fault", err)
			}
		})
	}
}

// setViewFault retains a coupling observation fault.
func setViewFault(_ *testing.T, s *Session) {
	s.mu.Lock()
	s.couplingViewError = errors.New("test observation fault")
	s.mu.Unlock()
}

// TestProjectApplySameProjectIsNoOp checks that an apply of the current
// project of each version changes nothing and saves nothing. A nil and an
// empty list of coupling sites are the same project.
func TestProjectApplySameProjectIsNoOp(t *testing.T) {
	t.Parallel()
	versioned := func(version int) func(*testing.T) *toggleSession {
		return func(t *testing.T) *toggleSession {
			t.Helper()
			config := project.Default()
			config.Version = version
			return newProjectSession(t, config)
		}
	}
	emptySites := func(t *testing.T) *toggleSession {
		t.Helper()
		config := couplingExample()
		config.CouplingSites = []sim.CouplingSite{}
		return newProjectSession(t, config)
	}
	tests := []struct {
		name  string
		start func(*testing.T) *toggleSession
		edit  func(*project.Config)
	}{
		{"version 1", versioned(1), nil},
		{"version 2", func(t *testing.T) *toggleSession {
			t.Helper()
			config := project.Default()
			config.Version, config.Network = project.BankVersion, sim.BankExample()
			config.Fleet = []sim.Placement{{ID: "01", StationID: "origin", BerthID: "origin-1"}}
			return newProjectSession(t, config)
		}, nil},
		{"version 3", versioned(project.ServiceVersion), nil},
		{"version 4", func(t *testing.T) *toggleSession { t.Helper(); return newProjectSession(t, expressConsumerProject(t)) }, nil},
		{"version 5 without groups", func(t *testing.T) *toggleSession { t.Helper(); return newToggleSession(t, "") }, nil},
		{"version 5 with a group", func(t *testing.T) *toggleSession { t.Helper(); return newToggleSession(t, connectedPhase) }, nil},
		{"nil sites to empty sites", func(t *testing.T) *toggleSession { t.Helper(); return newToggleSession(t, "") },
			func(c *project.Config) { c.CouplingSites = []sim.CouplingSite{} }},
		{"empty sites to nil sites", emptySites, func(c *project.Config) { c.CouplingSites = nil }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ts := test.start(t)
			current := ts.s.Project()
			config := project.Clone(current.Project)
			if test.edit != nil {
				test.edit(&config)
			}
			before, calls := keptValues(ts.s), len(ts.store.callList())
			ts.s.mu.Lock()
			origin := ts.s.projectOrigin
			ts.s.mu.Unlock()
			reply := ts.client.mustApply(t, Command{Action: "project", Project: &config, ProjectRevision: current.Revision})
			if !keptValues(ts.s).same(before) || reply.Generation != before.values.generation {
				t.Fatal("same project changed the simulation")
			}
			if reply.ProjectRevision != current.Revision || reply.StateSaved != nil || len(ts.saves) != 0 || len(ts.store.callList()) != calls {
				t.Fatalf("same project saved or changed the revision: %+v", reply)
			}
			if !reflect.DeepEqual(ts.s.Project(), current) || ts.s.projectOrigin != origin {
				t.Fatal("same project replaced the project")
			}
		})
	}
}

// TestCouplingToggleEmptyLists checks that a toggle with a nil list where
// the current project has an empty list, and the reverse, is a change in
// place, and that the session installs the project as sent.
func TestCouplingToggleEmptyLists(t *testing.T) {
	t.Parallel()
	for _, toEmpty := range []bool{false, true} {
		t.Run(fmt.Sprintf("to empty=%t", toEmpty), func(t *testing.T) {
			t.Parallel()
			start := couplingExample()
			if !toEmpty {
				start.CouplingSites = []sim.CouplingSite{}
			}
			ts := newProjectSession(t, start)
			before := keptValues(ts.s)
			reply := ts.apply(func(c *project.Config) {
				c.CouplingSites = nil
				if toEmpty {
					c.CouplingSites = []sim.CouplingSite{}
				}
			})
			if reply.Error != "" || !keptValues(ts.s).same(before) || !ts.s.simulation.CouplingEnabled() {
				t.Fatal("toggle with another empty list replaced the simulation", reply.Error)
			}
			if sites := ts.s.Project().Project.CouplingSites; (sites != nil) != toEmpty || len(ts.saves) != 1 || (ts.saves[0].CouplingSites != nil) != toEmpty {
				t.Fatal("toggle did not install the project as sent")
			}
		})
	}
}

// TestSameExceptCouplingEnabledEmptyLists checks that nil and empty
// coupling lists compare as the same in both directions, that other values
// compare as different, and that the comparison does not change its
// arguments.
func TestSameExceptCouplingEnabledEmptyLists(t *testing.T) {
	t.Parallel()
	base := couplingExample()
	base.CouplingCorridors = []sim.CouplingCorridor{{ID: "corridor", LaneIDs: nil}}
	empty := project.Clone(base)
	empty.CouplingSites, empty.CouplingEnabled = []sim.CouplingSite{}, true
	empty.CouplingCorridors[0].LaneIDs = []string{}
	noCorridors, emptyCorridors := couplingExample(), couplingExample()
	emptyCorridors.CouplingCorridors = []sim.CouplingCorridor{}
	lane := project.Clone(base)
	lane.CouplingCorridors[0].LaneIDs = []string{"lane"}
	tests := []struct {
		name          string
		first, second project.Config
		want          bool
	}{
		{"nil to empty", base, empty, true},
		{"empty to nil", empty, base, true},
		{"nil corridors to empty corridors", noCorridors, emptyCorridors, true},
		{"empty corridors to nil corridors", emptyCorridors, noCorridors, true},
		{"empty lanes to a lane", empty, lane, false},
		{"no corridors to a corridor", noCorridors, base, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			first, second := project.Clone(test.first), project.Clone(test.second)
			if got := sameExceptCouplingEnabled(first, second); got != test.want {
				t.Fatalf("sameExceptCouplingEnabled = %t, want %t", got, test.want)
			}
			if !reflect.DeepEqual(first, test.first) || !reflect.DeepEqual(second, test.second) {
				t.Fatal("comparison changed its arguments")
			}
		})
	}
}

// TestCouplingToggleRejectionsPreserveState checks that a rejected toggle
// changes neither the session nor the policy of the simulation.
func TestCouplingToggleRejectionsPreserveState(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		prepare func(*toggleSession, *Command)
		want    CommandErrorCode
	}{
		{"failed project save", func(ts *toggleSession, _ *Command) { ts.saveErr = errors.New("disk full") }, CommandRejected},
		{"stale revision", func(_ *toggleSession, command *Command) { command.ProjectRevision-- }, StaleProject},
		{"running", func(ts *toggleSession, _ *Command) {
			ts.s.mu.Lock()
			ts.s.simulation.SetPaused(false)
			ts.s.mu.Unlock()
		}, CommandRejected},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ts := newToggleSession(t, connectedPhase)
			current := ts.s.Project()
			config := current.Project
			config.CouplingEnabled = !config.CouplingEnabled
			command := Command{Action: "project", Project: &config, ProjectRevision: current.Revision}
			test.prepare(ts, &command)
			beforeState, beforeProject, before := ts.s.State(), ts.s.Project(), keptValues(ts.s)
			reply := ts.s.Apply(ts.client.next(command))
			if reply.Error == "" || reply.ErrorCode != test.want {
				t.Fatalf("reply = %+v, want error code %s", reply, test.want)
			}
			if !keptValues(ts.s).same(before) || ts.s.simulation.CouplingEnabled() != current.Project.CouplingEnabled {
				t.Fatal("rejected toggle changed the simulation")
			}
			if !reflect.DeepEqual(beforeState, ts.s.State()) || !reflect.DeepEqual(beforeProject, ts.s.Project()) || len(ts.saves) != 0 {
				t.Fatal("rejected toggle changed the session")
			}
		})
	}
}

// TestCouplingToggleStream checks that the publication after a toggle is a
// full frame of a new stream at the new project revision and the same
// generation, and that the topology of that revision accepts it.
func TestCouplingToggleStream(t *testing.T) {
	t.Parallel()
	ts := newToggleSession(t, connectedPhase)
	p := &statePublisher{session: ts.s, clients: map[*streamSubscriber]bool{}}
	if err := p.publish(t.Context(), true, true); err != nil {
		t.Fatal(err)
	}
	stream, source := p.stream, sourceOf(p.frame)
	if reply := ts.apply(nil); reply.Error != "" {
		t.Fatal(reply.Error)
	}
	if err := p.publish(t.Context(), false, true); err != nil {
		t.Fatal(err)
	}
	if p.full == nil || len(p.history) != 0 || p.stream == stream {
		t.Fatal("toggle did not start a new stream with a full frame")
	}
	inflated, err := InflateStream(p.full.data)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := DecodeStreamJSONVersion(inflated, CouplingStreamVersion)
	if err != nil {
		t.Fatal(err)
	}
	if envelope.Kind != "full" || envelope.Source.ProjectRevision != source.ProjectRevision+1 || envelope.Source.Generation != source.Generation {
		t.Fatalf("source %+v after %+v", envelope.Source, source)
	}
	frame, err := ApplyStream(StreamFrame{}, "", 0, envelope)
	if err != nil || !frame.State.Simulation.CouplingEnabled || len(frame.State.Simulation.CouplingGroups) != 1 {
		t.Fatal("full frame lost the policy or the group", err)
	}
	assembler, err := NewStreamAssemblerVersion(ts.s.Topology(), CouplingStreamVersion)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := assembler.State(frame); err != nil {
		t.Fatal("topology of the new revision rejected the frame", err)
	}
}

// TestCouplingToggleRestart checks that a restart after a toggle restores
// the new policy with the committed group, from the saved project and from
// a project file.
func TestCouplingToggleRestart(t *testing.T) {
	t.Parallel()
	for _, projectFile := range []bool{false, true} {
		name := "saved project"
		if projectFile {
			name = "project file"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ts := newToggleSession(t, connectedPhase)
			if reply := ts.apply(nil); reply.Error != "" {
				t.Fatal(reply.Error)
			}
			if err := ts.s.SaveState(t.Context(), SavePeriodic); err != nil {
				t.Fatal(err)
			}
			input := StoreInput{Store: ts.store}
			if projectFile {
				input.Project = &ts.saves[0]
			}
			restored, err := NewFromStore(t.Context(), input)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(restored.Close)
			if !restored.project.CouplingEnabled || !restored.simulation.CouplingEnabled() ||
				len(restored.simulation.ExportState().CouplingGroups) != 1 || restored.restore.Tier != "physical" {
				t.Fatal("restart lost the policy or the group")
			}
		})
	}
}

// TestCouplingToggleRewind checks that a save point from before a toggle
// restores the earlier project and policy, and that a save point from after
// it keeps the new ones.
func TestCouplingToggleRewind(t *testing.T) {
	t.Parallel()
	ts := newToggleSession(t, connectedPhase)
	early := ts.client.mustApply(t, Command{Action: "checkpoint"}).Checkpoint
	if reply := ts.apply(nil); reply.Error != "" {
		t.Fatal(reply.Error)
	}
	late := ts.client.mustApply(t, Command{Action: "checkpoint"}).Checkpoint
	if checkpoints := ts.s.State().Checkpoints; !checkpoints[0].RestoresProject || checkpoints[1].RestoresProject {
		t.Fatal("save points do not follow the project origin", checkpoints)
	}
	for _, test := range []struct {
		id       uint64
		enabled  bool
		restored bool
	}{{late, true, false}, {early, false, true}, {late, true, true}} {
		reply := ts.client.mustApply(t, Command{Action: "rewind", Checkpoint: test.id})
		if reply.ProjectRestored != test.restored || ts.s.Project().Project.CouplingEnabled != test.enabled ||
			ts.s.simulation.CouplingEnabled() != test.enabled || len(ts.s.simulation.ExportState().CouplingGroups) != 1 {
			t.Fatalf("rewind to %d: project restored %t, policy %t", test.id, reply.ProjectRestored, ts.s.simulation.CouplingEnabled())
		}
	}
}
