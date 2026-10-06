package session

import (
	"bytes"
	"encoding/json/v2"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
)

// TestFaultsOnWithoutFaultsOnPresets runs LondonCentral and the rail-hub
// preset with demand in two sessions, one with the incident marker only
// and one with the incident marker, the fault marker and faults, and no
// fault. At every 600th tick of 36,000 ticks, the exported states, the
// demand states and the demand random streams must be equal. At the end,
// a fault command proves that only the marked session has faults on.
func TestFaultsOnWithoutFaultsOnPresets(t *testing.T) {
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
			config.FaultContract, config.Faults = project.FaultV1Contract, &project.FaultConfig{}
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
			if _, err := off.simulation.Fault(sim.FaultRequest{PodID: pod}); err == nil {
				t.Fatal("the session without the fault marker started a fault")
			}
			if _, err := on.simulation.Fault(sim.FaultRequest{PodID: pod}); err != nil && err.Error() == "faults are not enabled" {
				t.Fatal("the session with the fault marker has faults off")
			}
		})
	}
}

// paritySnapshot returns the exported simulation state, the demand state
// and the state of the demand random stream of a session.
func paritySnapshot(t *testing.T, s *Session) []byte {
	t.Helper()
	state, err := json.Marshal(s.simulation.ExportState())
	if err != nil {
		t.Fatal(err)
	}
	demand, err := json.Marshal(s.demand.state)
	if err != nil {
		t.Fatal(err)
	}
	random, err := s.demand.pcg.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	return bytes.Join([][]byte{state, demand, random}, []byte{0})
}
