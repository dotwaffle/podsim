package sim

import (
	"testing"
)

// TestFaultView checks the faults of a snapshot (section 13.4 of the
// incident suspension contract). With faults off, a snapshot has no fault
// member. With faults on, each record has the members of its kind, and
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
	view := s.Snapshot()
	if len(view.Faults.Active) != 1 || view.Faults.Counters != (FaultCounters{Started: 1}) {
		t.Fatalf("snapshot: %+v", view.Faults)
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
	lane := s.network.Lanes[laneIndex(t, s, "s2-link")]
	id := startDebris(t, s, lane.ID, 0, 2, 0)
	fault := s.Snapshot().Faults.Active[0]
	if fault.ID != id || fault.Kind != FaultKindDebris || fault.LaneID != lane.ID || fault.FromMeters == nil || *fault.FromMeters != 0 ||
		fault.ToMeters == nil || *fault.ToMeters != 2 || fault.PodID != "" || fault.Phase != "" || fault.EvacuateTick != nil || fault.EndTick != 0 {
		t.Fatalf("debris %+v", fault)
	}
}

// TestOperationsOffSnapshot checks operationsOff in a snapshot. A new fleet
// has both switches off. The value is false only with both on. The traffic
// demo turns both off, and a reset keeps them.
func TestOperationsOffSnapshot(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(Example(), demoFleet())
	if err != nil {
		t.Fatal(err)
	}
	check := func(stage string, want bool) {
		t.Helper()
		if got := s.Snapshot().OperationsOff; got != want {
			t.Fatalf("%s: operationsOff %t, want %t", stage, got, want)
		}
	}
	check("new fleet", true)
	if err := s.SetFaults(true, FaultSettings{EvacuationSeconds: 300}); err != nil {
		t.Fatal(err)
	}
	check("faults on", true)
	if err := s.SetEmergencies(true); err != nil {
		t.Fatal(err)
	}
	check("both on", false)
	if err := s.SetFaults(false, FaultSettings{}); err != nil {
		t.Fatal(err)
	}
	check("emergencies on", true)
	if err := s.SetFaults(true, FaultSettings{EvacuationSeconds: 300}); err != nil {
		t.Fatal(err)
	}
	s.Reset()
	check("reset with both on", false)
	if err := s.StartDemo(); err != nil {
		t.Fatal(err)
	}
	check("demo", true)
	if view := s.Snapshot(); s.faultsOn || s.emergenciesOn || view.Faults.Active != nil || view.Emergencies.Active != nil {
		t.Fatalf("demo: faults on %t, emergencies on %t, view %+v %+v", s.faultsOn, s.emergenciesOn, view.Faults, view.Emergencies)
	}
	s.Reset()
	check("reset after the demo", true)
}
