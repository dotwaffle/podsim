package session

import (
	"reflect"
	"testing"

	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
)

// TestPresetPoliciesRestore runs the incident preset with its default policies
// over faults and emergencies, saves it, and restores it. Both policies stay
// active, and the restore keeps the physical state.
func TestPresetPoliciesRestore(t *testing.T) {
	t.Parallel()
	if raceEnabled {
		t.Skip("one simulation on one goroutine, so the race detector finds nothing")
	}
	config := scenarios.RailHubIncidents()
	config.Demand.Enabled = true
	store := &fakeStore{}
	shared := startFromStore(t, StoreInput{Store: store, Project: &config})
	defer shared.Close()
	if mode := shared.simulation.Positioning(); mode != sim.PositioningGuarded {
		t.Fatalf("positioning = %v, want guarded", mode)
	}
	for range 30 * 60 * sim.TicksPerSecond {
		shared.advance()
	}
	if _, err := shared.simulation.SafetyObservation().Check(); err != nil {
		t.Fatal(err)
	}
	state := shared.simulation.Snapshot()
	if state.Faults.Counters.Started == 0 || state.Emergencies.Counters.Sampled == 0 {
		t.Fatalf("the run has no incident: %+v, %+v", state.Faults.Counters, state.Emergencies.Counters)
	}
	if shared.simulation.PickupSwapStats().AssignmentChecks == 0 {
		t.Fatal("pickup reassignment made no check")
	}
	want := shared.simulation.ExportState()
	shared.Close()
	if err := shared.SaveState(t.Context(), SaveFinal); err != nil {
		t.Fatal(err)
	}
	writes := store.writeList()
	restored := startFromStore(t, StoreInput{Store: &fakeStore{data: writes[len(writes)-1]}, Project: &config})
	defer restored.Close()
	info := restored.State().Restore
	if info.Tier != "physical" || info.Demoted+info.Requeued+info.Dropped != 0 {
		t.Fatalf("the restore is not physical: %+v", info)
	}
	if got := restored.simulation.ExportState(); !reflect.DeepEqual(got, want) {
		t.Fatal("the restore changed the physical state")
	}
	if mode := restored.simulation.Positioning(); mode != sim.PositioningGuarded {
		t.Fatalf("restored positioning = %v, want guarded", mode)
	}
	for range 5 * 60 * sim.TicksPerSecond {
		restored.advance()
	}
	if restored.simulation.PickupSwapStats().AssignmentChecks == 0 {
		t.Fatal("restored pickup reassignment made no check")
	}
	if _, err := restored.simulation.SafetyObservation().Check(); err != nil {
		t.Fatal(err)
	}
}
