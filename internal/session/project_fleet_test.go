package session

import (
	"reflect"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
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
