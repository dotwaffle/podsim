package sim

import (
	"errors"
	"testing"
)

// incidentMarkerFleets returns a plain, a prepared and a coupling fleet
// with the incident marker, and the restore input of each without its
// saved state.
func incidentMarkerFleets(t *testing.T) map[string]RestoreStateInput {
	t.Helper()
	demo := []Placement{{ID: "01", StationID: "harbor", BerthID: "harbor-1"}, {ID: "02", StationID: "garden", BerthID: "garden-1"}}
	return map[string]RestoreStateInput{
		"plain": {IncidentContract: IncidentV1Contract, Network: Example(), Fleet: demo},
		"coupling": {IncidentContract: IncidentV1Contract, CouplingContract: CompactPairV1CouplingContract, CouplingEnabled: true,
			Network: expressNetwork(largeRestoreNetwork()), Fleet: []Placement{{ID: "01", Class: CompactClass, StationID: "harbor", BerthID: "harbor-1"}}},
	}
}

// TestIncidentMarkerSurvivesFleetChanges checks that each constructor, the
// clone, a reset, the traffic demo and both restore tiers keep the incident
// marker in the snapshot. The full frame copies the marker from there.
func TestIncidentMarkerSurvivesFleetChanges(t *testing.T) {
	t.Parallel()
	for name, input := range incidentMarkerFleets(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			check := func(stage string, s *Simulation, err error) {
				t.Helper()
				if err != nil {
					t.Fatal(stage, err)
				}
				if got := s.Snapshot().IncidentContract; got != IncidentV1Contract {
					t.Fatalf("%s: marker %q, want %q", stage, got, IncidentV1Contract)
				}
			}
			s, err := NewFleetWithContracts(input.Network, input.Fleet, input.fleetContracts())
			check("new fleet", s, err)
			p, err := PrepareNetwork(input.Network)
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := p.NewFleetWithContracts(input.Fleet, input.fleetContracts())
			check("prepared fleet", prepared, err)
			check("clone", s.Clone(), nil)
			s.Step()
			input.State = s.ExportState()
			for _, logical := range []bool{false, true} {
				input.LogicalOnly = logical
				restored, _, restoreErr := RestoreState(input)
				check("restore", restored, restoreErr)
				restored, _, restoreErr = p.RestoreState(PreparedRestoreInput{IncidentContract: input.IncidentContract,
					CouplingContract: input.CouplingContract, CouplingEnabled: input.CouplingEnabled,
					Fleet: input.Fleet, State: input.State, LogicalOnly: logical})
				check("prepared restore", restored, restoreErr)
			}
			s.Reset()
			check("reset", s, nil)
			if name == "plain" {
				check("demo", s, s.StartDemo())
			}
		})
	}
}

// TestIncidentMarkerUnknownRefused checks that a fleet refuses a marker
// that is not incident-v1, and that a fleet without the marker has none.
func TestIncidentMarkerUnknownRefused(t *testing.T) {
	t.Parallel()
	input := incidentMarkerFleets(t)["plain"]
	contracts := input.fleetContracts()
	contracts.IncidentContract = "incident-v2"
	if _, err := NewFleetWithContracts(input.Network, input.Fleet, contracts); !errors.Is(err, ErrUnknownIncidentContract) {
		t.Fatalf("new fleet error %v, want an unknown incident contract", err)
	}
	if err := ValidateFleetWithContracts(input.Network, input.Fleet, contracts); !errors.Is(err, ErrUnknownIncidentContract) {
		t.Fatalf("validation error %v, want an unknown incident contract", err)
	}
	contracts.IncidentContract = ""
	s, err := NewFleetWithContracts(input.Network, input.Fleet, contracts)
	if err != nil || s.Snapshot().IncidentContract != "" {
		t.Fatalf("unmarked fleet has marker %q: %v", s.Snapshot().IncidentContract, err)
	}
}
