package sim

import "testing"

// wantFaultReport checks the kept ID of v, and that one more tick writes the
// report with it. The tick moves s.
func wantFaultReport(t *testing.T, s *Simulation, v *vehicle, id string) {
	t.Helper()
	if v.faultID != id {
		t.Fatalf("pod %s keeps the fault ID %q, want %q", v.Pod.ID, v.faultID, id)
	}
	if err := s.checkFaults(); err != nil {
		t.Fatal(err)
	}
	s.Step()
	if !v.faulted || v.Pod.BlockedBy != id {
		t.Fatalf("pod %s: faulted %t, report by %q, want %q", v.Pod.ID, v.faulted, v.Pod.BlockedBy, id)
	}
}

// wantNoFaultID checks that no pod keeps a fault ID.
func wantNoFaultID(t *testing.T, s *Simulation) {
	t.Helper()
	for index := range s.vehicles {
		if id := s.vehicles[index].faultID; id != "" {
			t.Fatalf("pod %s keeps the fault ID %q", s.vehicles[index].Pod.ID, id)
		}
	}
}

// TestFaultIDFollowsStartAndClear checks a fault start, a clear, and a second
// fault on the same pod, whose report names the second record.
func TestFaultIDFollowsStartAndClear(t *testing.T) {
	t.Parallel()
	s, traveling, idle := faultFixture(t)
	first := startFault(t, s, idle, 0)
	other := startFault(t, s, traveling, 0)
	wantFaultReport(t, s, idle, first)
	wantFaultReport(t, s, traveling, other)
	if err := s.clearFault(first); err != nil {
		t.Fatal(err)
	}
	if idle.faultID != "" {
		t.Fatalf("after the clear, the pod keeps the fault ID %q", idle.faultID)
	}
	wantFaultReport(t, s, traveling, other)
	second := startFault(t, s, idle, 0)
	if second == first {
		t.Fatalf("the second fault has the ID %s of the first", first)
	}
	wantFaultReport(t, s, idle, second)
}

// TestFaultIDEndsWithDuration checks the clear of the fault stage.
func TestFaultIDEndsWithDuration(t *testing.T) {
	t.Parallel()
	s, _, idle := faultFixture(t)
	id := startFault(t, s, idle, 1)
	wantFaultReport(t, s, idle, id)
	for range TicksPerSecond {
		s.Step()
	}
	if idle.faulted {
		t.Fatal("the fault outlives its duration")
	}
	wantNoFaultID(t, s)
}

// TestFaultIDPhysicalRestore checks that the restore derives the ID from the
// saved record: the save does not hold it.
func TestFaultIDPhysicalRestore(t *testing.T) {
	t.Parallel()
	s, _, idle := faultFixture(t)
	id := startFault(t, s, idle, 0)
	restored, result, err := RestoreState(faultRestoreInput(s, s.ExportState()))
	if err != nil || !cleanRestore(result) {
		t.Fatalf("restore: %v, %+v", err, result)
	}
	wantFaultReport(t, restored, restored.findVehicle(idle.Pod.ID), id)
}

// TestFaultIDLogicalRestore checks that the logical tier, which ends each
// record, leaves no ID.
func TestFaultIDLogicalRestore(t *testing.T) {
	t.Parallel()
	s, _, idle := faultFixture(t)
	startFault(t, s, idle, 0)
	input := faultRestoreInput(s, s.ExportState())
	input.LogicalOnly = true
	restored, _, err := RestoreState(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(restored.faults) != 0 {
		t.Fatalf("the logical tier keeps the records %v", faultIDs(restored))
	}
	wantNoFaultID(t, restored)
}

// TestFaultIDClone checks that a clone keeps the ID, and that a clear on one
// side does not change the other.
func TestFaultIDClone(t *testing.T) {
	t.Parallel()
	s, _, idle := faultFixture(t)
	id := startFault(t, s, idle, 0)
	clone := s.Clone()
	cloned := clone.findVehicle(idle.Pod.ID)
	if cloned.faultID != id {
		t.Fatalf("the clone keeps the fault ID %q, want %q", cloned.faultID, id)
	}
	if err := s.clearFault(id); err != nil {
		t.Fatal(err)
	}
	wantFaultReport(t, clone, cloned, id)
	again := s.Clone()
	wantNoFaultID(t, again)
	second := startFault(t, s, idle, 0)
	wantFaultReport(t, s, idle, second)
	wantNoFaultID(t, again)
}

// TestFaultIDEndsAtReset checks that Reset leaves no ID.
func TestFaultIDEndsAtReset(t *testing.T) {
	t.Parallel()
	s, _, idle := faultFixture(t)
	startFault(t, s, idle, 0)
	s.Reset()
	wantNoFaultID(t, s)
}

// TestIncidentBlockerNamesFault checks the ID that the report of a healthy pod
// behind a faulted pod reads, and that no ID comes from a pod with no fault.
func TestIncidentBlockerNamesFault(t *testing.T) {
	t.Parallel()
	s, traveling, idle := faultFixture(t)
	id := startFault(t, s, idle, 0)
	if got, ok := s.incidentBlocker(podResourceOwner(idle.Pod.ID)); !ok || got != id {
		t.Fatalf("the faulted owner gives %q, %t, want %s", got, ok, id)
	}
	if got, ok := s.incidentBlocker(podResourceOwner(traveling.Pod.ID)); ok || got != "" {
		t.Fatalf("the healthy owner gives %q, %t", got, ok)
	}
}
