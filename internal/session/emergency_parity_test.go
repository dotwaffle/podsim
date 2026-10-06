package session

import (
	"bytes"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
)

// TestEmergenciesOnWithoutEmergenciesOnPresets runs LondonCentral and the
// rail-hub preset with demand in two sessions, one with the incident
// marker only and one with the incident marker, the emergency marker and
// emergencies, and no emergency (section 12 of the incident emergency
// contract). At every 600th tick of 36,000 ticks, the exported states,
// the demand states and the demand random streams must be equal. At the
// end, an emergency command proves that only the marked session has
// emergencies on.
func TestEmergenciesOnWithoutEmergenciesOnPresets(t *testing.T) {
	t.Parallel()
	for name, config := range map[string]project.Config{"london-central": scenarios.LondonCentral(), "rail-hub": scenarios.RailHub()} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			config.Demand.Enabled = true
			if config.Demand.PerMinute == 0 {
				config.Demand.PerMinute = 12
			}
			config.IncidentContract = sim.IncidentV1Contract
			off, err := NewWithProject(config)
			if err != nil {
				t.Fatal(err)
			}
			config.EmergencyContract, config.Emergencies = project.EmergencyV1Contract, &project.EmergencyConfig{}
			on, err := NewWithProject(config)
			if err != nil {
				t.Fatal(err)
			}
			for tick := 1; tick <= 36_000; tick++ {
				off.step()
				on.step()
				if tick%600 == 0 && !bytes.Equal(paritySnapshot(t, off), paritySnapshot(t, on)) {
					t.Fatalf("tick %d: the sessions differ", tick)
				}
			}
			if off.simulation.Snapshot().Completed == 0 {
				t.Fatal("no trip completed")
			}
			pod := config.Fleet[0].ID
			if _, err := off.simulation.Emergency(pod, 0); err == nil || err.Error() != "emergencies are not enabled" {
				t.Fatalf("the session without the emergency marker: %v", err)
			}
			if _, err := on.simulation.Emergency(pod, 0); err != nil && err.Error() == "emergencies are not enabled" {
				t.Fatal("the session with the emergency marker has emergencies off")
			}
		})
	}
}
