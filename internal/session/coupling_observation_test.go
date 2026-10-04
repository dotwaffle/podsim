package session

import (
	"errors"
	"reflect"
	"testing"
)

func TestCouplingObservationRestoreAndRewind(t *testing.T) {
	t.Parallel()
	data := couplingPhaseFixtures(t)
	input := couplingPhaseInput(t, data, data.Frames[0])
	store := &fakeStore{data: encodeTestState(t, couplingPhaseFile(t, input))}
	s, err := NewFromStore(t.Context(), StoreInput{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if s.couplingObservation == nil || s.CouplingError() != nil {
		t.Fatal("restore did not derive a healthy observation cache")
	}
	client := newTestClient(s, "observation")
	checkpoint := client.mustApply(t, Command{Action: "checkpoint"}).Checkpoint
	target := s.State()
	if saveErr := s.SaveState(t.Context(), SavePeriodic); saveErr != nil {
		t.Fatal(saveErr)
	}
	cold, err := NewFromStore(t.Context(), StoreInput{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cold.Close)
	if cold.couplingObservation == nil || cold.CouplingError() != nil ||
		cold.couplingObservation.Simulation.Tick != target.Simulation.Tick || len(cold.couplingObservation.Simulation.CouplingGroups) != 1 {
		t.Fatal("cold restore did not rebuild its own mechanical observation")
	}
	client.mustApply(t, Command{Action: "pause", Paused: false})
	s.advance()
	if s.State().Simulation.Tick <= target.Simulation.Tick {
		t.Fatal("lifecycle control did not advance before rewind")
	}
	cause := errors.New("observation lifecycle test")
	s.mu.Lock()
	s.retainCouplingViewError(cause)
	s.mu.Unlock()
	if !errors.Is(s.CouplingError(), cause) {
		t.Fatal("view fault lost its cause")
	}
	client.mustApply(t, Command{Action: "rewind", Checkpoint: checkpoint})
	got := s.State()
	if s.CouplingError() != nil || s.couplingObservation == nil || got.Simulation.Tick != target.Simulation.Tick ||
		!got.Simulation.Paused || !reflect.DeepEqual(got.Simulation.CouplingGroups, target.Simulation.CouplingGroups) {
		t.Fatal("rewind kept the failed observation or lost its checked train")
	}
	if s.couplingObservation.Revision != got.Revision || s.couplingObservation.Generation != got.Generation {
		t.Fatal("rewind rebuilt the cache before its new publication metadata")
	}
	got.Simulation.CouplingGroups[0].Members[0] = "caller mutation"
	got.Simulation.Vehicles[0].Pod.Position.X++
	next := s.State()
	if next.Simulation.CouplingGroups[0].Members[0] != target.Simulation.CouplingGroups[0].Members[0] ||
		next.Simulation.Vehicles[0].Pod.Position != target.Simulation.Vehicles[0].Pod.Position {
		t.Fatal("caller changed the restored mechanical cache")
	}
}
