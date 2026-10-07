package session

import (
	"fmt"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

func checkExperimentalPolicies(t *testing.T, shared *Session, reassignment bool) {
	t.Helper()
	before := shared.simulation.PickupSwapStats().AssignmentChecks
	if err := shared.simulation.RequestTrip("market", "garden"); err != nil {
		t.Fatal(err)
	}
	checked := shared.simulation.PickupSwapStats().AssignmentChecks > before
	if checked != reassignment {
		t.Fatalf("reassignment check=%t want%t", checked, reassignment)
	}
}

func TestExperimentalPolicySessionLifecycle(t *testing.T) {
	t.Parallel()
	for _, reassignment := range []bool{false, true} {
		t.Run(fmt.Sprintf("reassignment%t", reassignment), func(t *testing.T) {
			t.Parallel()
			config := project.Default()
			config.PickupReassignment = project.PolicyFlag(reassignment)
			shared, err := NewWithProject(config)
			if err != nil {
				t.Fatal(err)
			}
			defer shared.Close()
			client := newTestClient(shared, "experimental-lifecycle")
			checkExperimentalPolicies(t, shared, reassignment)
			client.mustApply(t, Command{Action: "reset"})
			checkExperimentalPolicies(t, shared, reassignment)
			client.mustApply(t, Command{Action: "reset"})
			checkpoint := client.mustApply(t, Command{Action: "checkpoint"}).Checkpoint
			client.mustApply(t, Command{Action: "pause", Paused: true})
			disabled := config
			disabled.PickupReassignment = false
			client.mustApply(t, Command{Action: "project", ProjectRevision: shared.Project().Revision, Project: &disabled})
			checkExperimentalPolicies(t, shared, false)
			client.mustApply(t, Command{Action: "rewind", Checkpoint: checkpoint})
			if shared.Project().Project.PickupReassignment != config.PickupReassignment {
				t.Fatal("rewind did not recover project policies")
			}
			checkExperimentalPolicies(t, shared, reassignment)
			client.mustApply(t, Command{Action: "rewind", Checkpoint: checkpoint})
			if shared.simulation.PickupSwapStats() != (sim.PickupSwapStats{}) {
				t.Fatal("checkpoint shares mutable reassignment history")
			}
		})
	}
}

func TestExperimentalPolicyDemo(t *testing.T) {
	t.Parallel()
	config := project.Default()
	config.PickupReassignment = true
	shared, err := NewWithProject(config)
	if err != nil {
		t.Fatal(err)
	}
	defer shared.Close()
	client := newTestClient(shared, "experimental-demo")
	client.mustApply(t, Command{Action: "demo"})
	client.mustApply(t, Command{Action: "reset"})
	checkExperimentalPolicies(t, shared, true)
}

func TestExperimentalPolicyFileRestore(t *testing.T) {
	t.Parallel()
	config := project.Default()
	config.PickupReassignment = true
	store := &fakeStore{}
	shared := startFromStore(t, StoreInput{Store: store, Project: &config})
	checkExperimentalPolicies(t, shared, true)
	shared.Close()
	if err := shared.SaveState(t.Context(), SaveFinal); err != nil {
		t.Fatal(err)
	}
	data := store.writeList()[len(store.writeList())-1]
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("enabled%t", enabled), func(t *testing.T) {
			t.Parallel()
			effective := config
			effective.PickupReassignment = project.PolicyFlag(enabled)
			restored := startFromStore(t, StoreInput{Store: &fakeStore{data: data}, Project: &effective})
			defer restored.Close()
			info := restored.State().Restore
			if info.Tier != "physical" || info.Demoted+info.Requeued+info.Dropped != 0 {
				t.Fatalf("policy override did not preserve physical restore: %+v", info)
			}
			if restored.simulation.PickupSwapStats() != (sim.PickupSwapStats{}) || len(restored.simulation.PickupReassignments()) != 0 {
				t.Fatal("file restore retained reassignment history")
			}
			restored.simulation.Reset()
			checkExperimentalPolicies(t, restored, enabled)
		})
	}
}
