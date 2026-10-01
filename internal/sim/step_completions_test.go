package sim

import (
	"reflect"
	"slices"
	"testing"
)

func TestSubmitTripPreservesRequestTrip(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ from, to string }{
		{"harbor", "market"}, {"garden", "harbor"}, {"harbor", "harbor"},
		{"missing", "market"}, {"harbor", "missing"}, {"parking", "market"}, {"market", "parking"},
	} {
		t.Run(tc.from+"/"+tc.to, func(t *testing.T) {
			t.Parallel()
			old, receipt := newSharingSimulation(t), newSharingSimulation(t)
			oldCalls, receiptCalls := 0, 0
			old.monitor = func(*Simulation) { oldCalls++ }
			receipt.monitor = func(*Simulation) { receiptCalls++ }
			oldErr := old.RequestTrip(tc.from, tc.to)
			id, newErr := receipt.SubmitTrip(tc.from, tc.to)
			if (oldErr == nil) != (newErr == nil) || oldErr != nil && oldErr.Error() != newErr.Error() {
				t.Fatalf("errors: old=%v, receipt=%v", oldErr, newErr)
			}
			if oldCalls != 1 || receiptCalls != 1 || newErr != nil && id != 0 || newErr == nil && id != 1 {
				t.Fatalf("ID %d, observer calls %d/%d", id, oldCalls, receiptCalls)
			}
			if !reflect.DeepEqual(old.Snapshot(), receipt.Snapshot()) || !reflect.DeepEqual(old.ExportState(), receipt.ExportState()) {
				t.Fatal("receipt changed dispatch or physical state")
			}
		})
	}
}

func TestStepCompletionsFollowUnloading(t *testing.T) {
	t.Parallel()
	s := newSharingSimulation(t)
	if err := s.SetSharedRidePartyLimit(2); err != nil {
		t.Fatal(err)
	}
	for want := 1; want <= 2; want++ {
		if id, err := s.SubmitTrip("harbor", "market"); err != nil || id != want {
			t.Fatalf("submit: ID %d, error %v", id, err)
		}
	}
	var completions []StepCompletion
	for range 300 * TicksPerSecond {
		before := s.completed
		s.Step()
		got := s.StepCompletions()
		if len(got) != s.completed-before || len(got) > len(s.vehicles)*MaxSharedRideParties {
			t.Fatalf("tick %d, completions %v, completed %d/%d", s.tick, got, before, s.completed)
		}
		if len(got) == 0 {
			continue
		}
		completions = got
		break
	}
	want := []StepCompletion{{RequestID: 1, AlightedTick: s.tick}, {RequestID: 2, AlightedTick: s.tick}}
	if !slices.Equal(completions, want) || s.completed != 2 {
		t.Fatalf("completions %v, want %v", completions, want)
	}
	if s.recordExperiments || s.requestCompletions != nil || s.requestBoardings != nil {
		t.Fatal("completion receipts enabled unbounded experiment records")
	}
	completions[0].RequestID = 99
	if !slices.Equal(s.StepCompletions(), want) {
		t.Fatal("read aliases internal completion storage")
	}
	s.SetPaused(true)
	s.Step()
	if !slices.Equal(s.StepCompletions(), want) {
		t.Fatal("paused step cleared latest advanced tick")
	}
	clone := s.Clone()
	clone.stepCompletions[0].RequestID = 42
	if !slices.Equal(s.StepCompletions(), want) {
		t.Fatal("clone aliases completion storage")
	}
	restored, result, err := RestoreState(RestoreStateInput{Network: s.network, Fleet: s.initial, State: s.ExportState()})
	if err != nil || result.Tier != RestorePhysical || len(restored.StepCompletions()) != 0 {
		t.Fatalf("restore: %+v, %v", result, err)
	}
	s.SetPaused(false)
	s.Step()
	if len(s.StepCompletions()) != 0 {
		t.Fatal("next advanced tick retained prior completions")
	}
	clone.Reset()
	if clone.StepCompletions() != nil {
		t.Fatal("reset retained completion records")
	}
}

func TestLogicalRestoreReportsUntimedCompletions(t *testing.T) {
	t.Parallel()
	f := newRestoreFleetFixture(t, Example(), demoFleet())
	state := logicalState(t, f)
	logical, result, err := RestoreState(RestoreStateInput{Network: f.network, Fleet: f.fleet, State: state, LogicalOnly: true})
	if err != nil || !slices.Equal(result.LogicalCompleted, []int{2, 3}) || len(logical.StepCompletions()) != 0 {
		t.Fatalf("logical completion receipts: %+v, %v", result, err)
	}
	if logical.completed != state.Completed+len(result.LogicalCompleted) {
		t.Fatal("logical completion receipt changed accounting")
	}
	physical, result, err := RestoreState(RestoreStateInput{Network: f.network, Fleet: f.fleet, State: state})
	if err != nil || result.Tier != RestorePhysical || len(result.LogicalCompleted) != 0 || len(physical.StepCompletions()) != 0 {
		t.Fatalf("physical restore fabricated completion receipts: %+v, %v", result, err)
	}
}

func TestStepCompletionsRespectLaterStops(t *testing.T) {
	t.Parallel()
	s := newTwoStopRide(t)
	var got []StepCompletion
	for range 600 * TicksPerSecond {
		beforeStation := s.findVehicle("01").Pod.StationID
		s.Step()
		for _, completion := range s.StepCompletions() {
			wantStation := "garden"
			if completion.RequestID == 2 {
				wantStation = "market"
			}
			if completion.AlightedTick != s.tick || beforeStation != wantStation {
				t.Fatalf("receipt %+v at %s, want %s", completion, beforeStation, wantStation)
			}
			got = append(got, completion)
		}
		if s.completed == 2 {
			break
		}
	}
	if len(got) != 2 || got[0].RequestID != 1 || got[1].RequestID != 2 || got[0].AlightedTick >= got[1].AlightedTick {
		t.Fatalf("later-stop completion receipts: %+v", got)
	}
}
