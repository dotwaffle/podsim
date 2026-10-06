package sim

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
)

// incidentMarkerFleets returns a plain fleet with the incident marker, and
// its restore input without its saved state.
func incidentMarkerFleets(t *testing.T) map[string]RestoreStateInput {
	t.Helper()
	demo := []Placement{{ID: "01", StationID: "harbor", BerthID: "harbor-1"}, {ID: "02", StationID: "garden", BerthID: "garden-1"}}
	return map[string]RestoreStateInput{
		"plain": {IncidentContract: IncidentV1Contract, Network: Example(), Fleet: demo},
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

// ValidateFleetWithContracts checks the same startup rules without live state.
func ValidateFleetWithContracts(network Network, placements []Placement, contracts FleetContracts) error {
	if err := validateFleetContracts(contracts); err != nil {
		return err
	}
	return ValidateFleetWithOrderContract(network, placements, contracts.OrderContract)
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

// TestIncidentIDs checks the ID scheme of section 11.4 of the incident
// contract. The serial only increases, a new generation does not reset it,
// and a rewind to a save point with a new generation gives IDs that the
// abandoned timeline did not use.
func TestIncidentIDs(t *testing.T) {
	t.Parallel()
	input := incidentMarkerFleets(t)["plain"]
	s, err := NewFleetWithContracts(input.Network, input.Fleet, input.fleetContracts())
	if err != nil {
		t.Fatal(err)
	}
	s.SetIncidentGeneration(3)
	if first, second := s.nextIncidentID(), s.nextIncidentID(); first != "i3.1" || second != "i3.2" {
		t.Fatalf("IDs %q and %q, want i3.1 and i3.2", first, second)
	}
	savePoint := s.Clone()
	abandoned := map[string]bool{"i3.1": true, "i3.2": true}
	for range 2 {
		abandoned[s.nextIncidentID()] = true
	}
	rewound := savePoint.Clone()
	rewound.SetIncidentGeneration(4)
	if id := rewound.nextIncidentID(); id != "i4.3" || abandoned[id] {
		t.Fatalf("ID after the rewind %q, want i4.3", id)
	}
	s.SetIncidentGeneration(5)
	if id := s.nextIncidentID(); id != "i5.5" {
		t.Fatalf("ID after a new generation %q, want i5.5: the serial must not reset", id)
	}
	s.Reset()
	if id := s.nextIncidentID(); id != "i5.6" {
		t.Fatalf("ID after a reset %q, want i5.6", id)
	}
	if err := s.StartDemo(); err != nil {
		t.Fatal(err)
	}
	if id := s.nextIncidentID(); id != "i5.7" {
		t.Fatalf("ID after the demo %q, want i5.7", id)
	}
}

// TestIncidentSerialSaved checks that both restore tiers keep the saved
// serial, that a state without a record has no serial member, and that a
// restore without the incident marker refuses a serial.
func TestIncidentSerialSaved(t *testing.T) {
	t.Parallel()
	input := incidentMarkerFleets(t)["plain"]
	s, err := NewFleetWithContracts(input.Network, input.Fleet, input.fleetContracts())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(s.ExportState())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("incidentSerial")) {
		t.Fatal("a state without a record has an incident serial")
	}
	for range 3 {
		s.nextIncidentID()
	}
	input.State = s.ExportState()
	if input.State.IncidentSerial != 3 {
		t.Fatalf("saved serial %d, want 3", input.State.IncidentSerial)
	}
	for _, logical := range []bool{false, true} {
		input.LogicalOnly = logical
		restored, _, restoreErr := RestoreState(input)
		if restoreErr != nil {
			t.Fatal(restoreErr)
		}
		if id := restored.nextIncidentID(); id != "i0.4" {
			t.Fatalf("logical %v: ID after the restore %q, want i0.4", logical, id)
		}
	}
	input.IncidentContract = ""
	if _, _, err := RestoreState(input); err == nil {
		t.Fatal("a restore without the marker accepted a serial")
	}
}
