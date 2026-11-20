package session

import (
	"reflect"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
)

// TestProjectFleetTakesValidation checks that the fleet of a new session
// and of a project apply, which takes the network preparation of the
// validation, has the state of the fleet of startProject with no check,
// which prepares the network again, after 300 ticks.
func TestProjectFleetTakesValidation(t *testing.T) {
	t.Parallel()
	for name, config := range map[string]project.Config{"example": project.Default(), "busy": scenarios.Busy(), "rail-hub": scenarios.RailHubIncidents()} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			started, err := NewWithProject(config)
			if err != nil {
				t.Fatal(err)
			}
			applied, err := NewWithProject(scenarios.Small())
			if err != nil {
				t.Fatal(err)
			}
			applied.simulation.SetPaused(true)
			if changed, err := applied.applyProject(Command{ProjectRevision: applied.projectRevision, Project: &config}); err != nil || !changed {
				t.Fatalf("apply: changed %v, error %v", changed, err)
			}
			for _, s := range []*Session{started, applied} {
				reference := newSession(nil, nil)
				if err := reference.startProject(config, nil); err != nil {
					t.Fatal(err)
				}
				reference.simulation.SetIncidentGeneration(s.generation)
				for _, simulation := range []*Session{s, reference} {
					simulation.simulation.SetPaused(false)
					for range 300 {
						simulation.simulation.Step()
					}
				}
				if !reflect.DeepEqual(s.simulation.ExportState(), reference.simulation.ExportState()) {
					t.Errorf("generation %d: the state differs from the state of a fleet with no check", s.generation)
				}
			}
		})
	}
}

// TestProjectStartPreparesOneTime pins that a new session and a project
// apply prepare the network one time: the fleet takes the preparation of
// the validation. It does not run in parallel, because the count is for
// the process.
func TestProjectStartPreparesOneTime(t *testing.T) {
	started, config := scenarios.Small(), scenarios.Busy()
	before := sim.PreparationsForTest()
	s, err := NewWithProject(started)
	if err != nil {
		t.Fatal(err)
	}
	if got := sim.PreparationsForTest() - before; got != 1 {
		t.Errorf("a new session prepares the network %d times, want 1", got)
	}
	s.simulation.SetPaused(true)
	before = sim.PreparationsForTest()
	if changed, err := s.applyProject(Command{ProjectRevision: s.projectRevision, Project: &config}); err != nil || !changed {
		t.Fatalf("apply: changed %v, error %v", changed, err)
	}
	if got := sim.PreparationsForTest() - before; got != 1 {
		t.Errorf("a project apply prepares the network %d times, want 1", got)
	}
}

// TestCheckedProjectKeepsCopy checks that a checked project keeps a copy
// of the project, so that its checks stay those of its network, and that
// NewFromStore refuses a project and a checked project together.
func TestCheckedProjectKeepsCopy(t *testing.T) {
	t.Parallel()
	config := project.Default()
	checked, err := CheckProject(config)
	if err != nil {
		t.Fatal(err)
	}
	config.Network.Lanes[0].SpeedLimit++
	if reflect.DeepEqual(checked.Config(), config) || !reflect.DeepEqual(checked.Config(), project.Default()) {
		t.Fatal("a change of the project reaches the checked project")
	}
	_, err = NewFromStore(t.Context(), StoreInput{Store: &fakeStore{}, Project: &config, Checked: &checked})
	if err == nil || err.Error() != "start session: a project and a checked project" {
		t.Fatalf("error = %v, want the refusal of a project and a checked project", err)
	}
}
