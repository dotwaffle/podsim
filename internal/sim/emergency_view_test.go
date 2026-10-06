package sim

import (
	"errors"
	"testing"
)

// TestEmergencyContractMarker checks the emergency marker of a fleet. It
// needs the incident marker, and only emergency-v1 is known. The traffic
// demo and a reset keep the marker, and the snapshot copies it.
func TestEmergencyContractMarker(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name      string
		contracts FleetContracts
		err       error
	}{
		{"without the incident marker", FleetContracts{EmergencyContract: EmergencyV1Contract}, nil},
		{"unknown", FleetContracts{IncidentContract: IncidentV1Contract, EmergencyContract: "emergency-v2"}, ErrUnknownEmergencyContract},
	} {
		if _, err := NewFleetWithContracts(Example(), demoFleet(), test.contracts); err == nil || test.err != nil && !errors.Is(err, test.err) {
			t.Errorf("%s: %v", test.name, err)
		}
	}
	s, err := NewFleetWithContracts(Example(), demoFleet(), FleetContracts{IncidentContract: IncidentV1Contract, EmergencyContract: EmergencyV1Contract})
	if err != nil {
		t.Fatal(err)
	}
	if s.Snapshot().EmergencyContract != EmergencyV1Contract {
		t.Fatal("the snapshot has no emergency marker")
	}
	if err := s.StartDemo(); err != nil {
		t.Fatal(err)
	}
	if view := s.Snapshot(); view.EmergencyContract != EmergencyV1Contract || view.Emergencies.Active != nil {
		t.Fatalf("demo: marker %q, emergencies %+v", view.EmergencyContract, view.Emergencies)
	}
	s.Reset()
	if s.Snapshot().EmergencyContract != EmergencyV1Contract {
		t.Fatal("the reset lost the emergency marker")
	}
}
