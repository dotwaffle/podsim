package sim

import (
	"errors"
	"testing"
)

// TestFaultView checks the faults of a snapshot (section 13.4 of the
// incident suspension contract). Without the fault marker, a snapshot has
// no fault member. With it, each record has the members of its kind, and
// the phase of a pod fault follows the pod: braking while it moves,
// stopped at rest while it has an active rider, and evacuated at rest
// from its evacuation tick when it has no active rider. The pod has no
// evacuation delay, so its evacuation tick is its start tick, and it
// reaches rest after that tick with its riders aboard. The next fault
// stage evacuates it.
func TestFaultView(t *testing.T) {
	t.Parallel()
	s := faultLegFleet(t)
	s.faultSettings.evacuationSeconds = 0
	v := boardParties(t, s, "s2")
	cruiseOn(t, s, v, "s0-link")
	id := startFault(t, s, v, 10)
	if view := s.Snapshot(); view.FaultContract != "" || view.Faults.Active != nil || view.Faults.Counters != (FaultCounters{}) {
		t.Fatalf("unmarked snapshot: %q %+v", view.FaultContract, view.Faults)
	}
	s.faultContract = FaultV1Contract
	view := s.Snapshot()
	if view.FaultContract != FaultV1Contract || len(view.Faults.Active) != 1 || view.Faults.Counters != (FaultCounters{Started: 1}) {
		t.Fatalf("marked snapshot: %q %+v", view.FaultContract, view.Faults)
	}
	fault := view.Faults.Active[0]
	start := s.faults[0].start
	if fault.ID != id || fault.Kind != FaultKindPod || fault.PodID != "01" || fault.Phase != FaultPhaseBraking ||
		fault.StartTick != start || fault.EndTick != start+10*TicksPerSecond || fault.EvacuateTick == nil || *fault.EvacuateTick != start ||
		fault.LaneID != "" || fault.FromMeters != nil || fault.ToMeters != nil {
		t.Fatalf("braking fault %+v", fault)
	}
	for v.Pod.Speed > 0 {
		s.Step()
	}
	if phase := s.Snapshot().Faults.Active[0].Phase; phase != FaultPhaseStopped || v.RidersAboard() == 0 {
		t.Fatalf("phase at rest with %d riders %q", v.RidersAboard(), phase)
	}
	s.Step()
	if phase := s.Snapshot().Faults.Active[0].Phase; phase != FaultPhaseEvacuated || v.RidersAboard() != 0 {
		t.Fatalf("phase after the evacuation with %d riders %q", v.RidersAboard(), phase)
	}

	// A pod at a berth without riders is evacuated from its start tick.
	if err := s.clearFault(id); err != nil {
		t.Fatal(err)
	}
	parked := startFault(t, s, s.findVehicle("02"), 0)
	view = s.Snapshot()
	fault = view.Faults.Active[0]
	if fault.ID != parked || fault.Phase != FaultPhaseEvacuated || fault.EndTick != 0 || *fault.EvacuateTick != fault.StartTick ||
		view.Faults.Counters != (FaultCounters{Started: 2, Cleared: 1, Evacuations: 1}) {
		t.Fatalf("parked fault %+v, counters %+v", fault, view.Faults.Counters)
	}
}

// TestDebrisFaultView checks the members of debris in a snapshot. A
// segment that starts at 0 m has a fromMeters of 0.
func TestDebrisFaultView(t *testing.T) {
	t.Parallel()
	s := debrisFleet(t)
	s.faultContract = FaultV1Contract
	lane := s.network.Lanes[laneIndex(t, s, "s2-link")]
	id := startDebris(t, s, lane.ID, 0, 2, 0)
	fault := s.Snapshot().Faults.Active[0]
	if fault.ID != id || fault.Kind != FaultKindDebris || fault.LaneID != lane.ID || fault.FromMeters == nil || *fault.FromMeters != 0 ||
		fault.ToMeters == nil || *fault.ToMeters != 2 || fault.PodID != "" || fault.Phase != "" || fault.EvacuateTick != nil || fault.EndTick != 0 {
		t.Fatalf("debris %+v", fault)
	}
}

// TestFaultContractMarker checks the fault marker of a fleet. It needs the
// incident marker, and only fault-v1 is known. The traffic demo and a
// reset keep the marker, and the demo turns the fault operations off.
func TestFaultContractMarker(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name      string
		contracts FleetContracts
		err       error
	}{
		{"without the incident marker", FleetContracts{FaultContract: FaultV1Contract}, nil},
		{"unknown", FleetContracts{IncidentContract: IncidentV1Contract, FaultContract: "fault-v2"}, ErrUnknownFaultContract},
	} {
		if _, err := NewFleetWithContracts(Example(), demoFleet(), test.contracts); err == nil || test.err != nil && !errors.Is(err, test.err) {
			t.Errorf("%s: %v", test.name, err)
		}
	}
	s, err := NewFleetWithContracts(Example(), demoFleet(), FleetContracts{IncidentContract: IncidentV1Contract, FaultContract: FaultV1Contract})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetFaults(true, FaultSettings{EvacuationSeconds: 300}); err != nil {
		t.Fatal(err)
	}
	if err := s.StartDemo(); err != nil {
		t.Fatal(err)
	}
	if view := s.Snapshot(); s.faultsOn || view.FaultContract != FaultV1Contract || view.Faults.Active != nil {
		t.Fatalf("demo: faults on %t, marker %q, faults %+v", s.faultsOn, view.FaultContract, view.Faults)
	}
	s.Reset()
	if s.Snapshot().FaultContract != FaultV1Contract {
		t.Fatal("the reset lost the fault marker")
	}
}
