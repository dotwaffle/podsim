package session

import (
	"bytes"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
)

// TestEmergenciesOnWithoutEmergenciesOnPresets runs LondonCentral and the
// rail-hub preset with demand in two sessions, one with the operations off
// and one with the operations on and emergency settings, and no emergency
// (section 12 of the incident emergency contract). At every 600th tick of
// 36,000 ticks, the exported states, the demand states and the demand
// random streams must be equal. At the end, an emergency command proves
// that only the second session has emergencies on.
func TestEmergenciesOnWithoutEmergenciesOnPresets(t *testing.T) {
	t.Parallel()
	for name, config := range map[string]project.Config{"london-central": scenarios.LondonCentral(), "rail-hub": scenarios.RailHub()} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			config.Demand.Enabled = true
			if config.Demand.PerMinute == 0 {
				config.Demand.PerMinute = 12
			}
			off, err := NewWithProject(config)
			if err != nil {
				t.Fatal(err)
			}
			if err = project.ConfigureExperiments(off.simulation, config, false); err != nil {
				t.Fatal(err)
			}
			config.Emergencies = &project.EmergencyConfig{}
			on, err := NewWithProject(config)
			if err != nil {
				t.Fatal(err)
			}
			for tick := 1; tick <= 600*sim.TicksPerSecond; tick++ {
				off.step()
				on.step()
				if tick%(10*sim.TicksPerSecond) == 0 && !bytes.Equal(paritySnapshot(t, off), paritySnapshot(t, on)) {
					t.Fatalf("tick %d: the sessions differ", tick)
				}
			}
			if off.simulation.Snapshot().Completed == 0 {
				t.Fatal("no trip completed")
			}
			pod := config.Fleet[0].ID
			if _, err := off.simulation.Emergency(pod, 0); err == nil || err.Error() != "emergencies are not enabled" {
				t.Fatalf("the session with the operations off: %v", err)
			}
			if _, err := on.simulation.Emergency(pod, 0); err != nil && err.Error() == "emergencies are not enabled" {
				t.Fatal("the session with the operations on has emergencies off")
			}
		})
	}
}
