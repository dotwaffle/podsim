package sim

import (
	"bytes"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"testing"
)

// plainFleetInput returns the restore input of a plain fleet, without its
// saved state.
func plainFleetInput() RestoreStateInput {
	demo := []Placement{{ID: "01", StationID: "harbor", BerthID: "harbor-1"}, {ID: "02", StationID: "garden", BerthID: "garden-1"}}
	return RestoreStateInput{Network: Example(), Fleet: demo}
}

// TestRestoreSwitchesForwarded checks that each form of the restore gives
// the simulation the fault and emergency switches of its input, with the
// fault settings, in both tiers.
func TestRestoreSwitchesForwarded(t *testing.T) {
	t.Parallel()
	input := plainFleetInput()
	s, err := NewFleet(input.Network, input.Fleet)
	if err != nil {
		t.Fatal(err)
	}
	p, err := PrepareNetwork(input.Network)
	if err != nil {
		t.Fatal(err)
	}
	s.Step()
	input.State = s.ExportState()
	input.Faults = FaultSettings{EvacuationSeconds: 77}
	for _, test := range []struct{ faults, emergencies bool }{{false, false}, {true, false}, {false, true}, {true, true}} {
		for _, logical := range []bool{false, true} {
			input.FaultsOn, input.EmergenciesOn, input.LogicalOnly = test.faults, test.emergencies, logical
			direct, _, err := RestoreState(input)
			if err != nil {
				t.Fatal(err)
			}
			prepared, _, err := p.RestoreState(PreparedRestoreInput{FaultsOn: test.faults, Faults: input.Faults, EmergenciesOn: test.emergencies,
				Fleet: input.Fleet, State: input.State, LogicalOnly: logical})
			if err != nil {
				t.Fatal(err)
			}
			wantSeconds := int64(0)
			if test.faults {
				wantSeconds = 77
			}
			for name, restored := range map[string]*Simulation{"direct": direct, "prepared": prepared} {
				if restored.faultsOn != test.faults || restored.emergenciesOn != test.emergencies || restored.faultSettings.evacuationSeconds != wantSeconds {
					t.Errorf("%s, logical %t, input %+v: faults %t, emergencies %t, settings %+v", name, logical, test, restored.faultsOn, restored.emergenciesOn, restored.faultSettings)
				}
				if want := !test.faults || !test.emergencies; restored.Snapshot().OperationsOff != want {
					t.Errorf("%s, logical %t, input %+v: operationsOff %t", name, logical, test, restored.Snapshot().OperationsOff)
				}
			}
		}
	}
}

// TestIncidentIDs checks the ID scheme of section 11.4 of the incident
// contract. The serial only increases, a new generation does not reset it,
// and a rewind to a save point with a new generation gives IDs that the
// abandoned timeline did not use.
func TestIncidentIDs(t *testing.T) {
	t.Parallel()
	input := plainFleetInput()
	s, err := NewFleet(input.Network, input.Fleet)
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
// serial, and that a state without a record has no serial member.
func TestIncidentSerialSaved(t *testing.T) {
	t.Parallel()
	input := plainFleetInput()
	s, err := NewFleet(input.Network, input.Fleet)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := jsonv2.Marshal(s.ExportState(), json.DefaultOptionsV1())
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
}
