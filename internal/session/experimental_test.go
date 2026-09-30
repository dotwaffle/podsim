package session

import (
	"fmt"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

func checkExperimentalPolicies(t *testing.T, shared *Session, buffers, reassignment bool) {
	t.Helper()
	if shared.simulation.NeedsBufferState() != buffers {
		t.Fatal("simulation buffer policy does not match the project")
	}
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
	for _, buffers := range []bool{false, true} {
		for _, reassignment := range []bool{false, true} {
			t.Run(fmt.Sprintf("buffers%t-reassignment%t", buffers, reassignment), func(t *testing.T) {
				t.Parallel()
				config := project.Default()
				config.StationBuffers, config.PickupReassignment = project.PolicyFlag(buffers), project.PolicyFlag(reassignment)
				shared, err := NewWithProject(config)
				if err != nil {
					t.Fatal(err)
				}
				defer shared.Close()
				client := newTestClient(shared, "experimental-lifecycle")
				checkExperimentalPolicies(t, shared, buffers, reassignment)
				client.mustApply(t, Command{Action: "reset"})
				checkExperimentalPolicies(t, shared, buffers, reassignment)
				client.mustApply(t, Command{Action: "reset"})
				checkpoint := client.mustApply(t, Command{Action: "checkpoint"}).Checkpoint
				client.mustApply(t, Command{Action: "pause", Paused: true})
				disabled := config
				disabled.StationBuffers, disabled.PickupReassignment = false, false
				client.mustApply(t, Command{Action: "project", ProjectRevision: shared.Project().Revision, Project: &disabled})
				checkExperimentalPolicies(t, shared, false, false)
				client.mustApply(t, Command{Action: "rewind", Checkpoint: checkpoint})
				if shared.Project().Project.StationBuffers != config.StationBuffers || shared.Project().Project.PickupReassignment != config.PickupReassignment {
					t.Fatal("rewind did not recover project policies")
				}
				checkExperimentalPolicies(t, shared, buffers, reassignment)
				client.mustApply(t, Command{Action: "rewind", Checkpoint: checkpoint})
				if shared.simulation.PickupSwapStats() != (sim.PickupSwapStats{}) {
					t.Fatal("checkpoint shares mutable reassignment history")
				}
			})
		}
	}
}

func TestExperimentalPolicyDemo(t *testing.T) {
	t.Parallel()
	config := project.Default()
	config.StationBuffers, config.PickupReassignment = true, true
	shared, err := NewWithProject(config)
	if err != nil {
		t.Fatal(err)
	}
	defer shared.Close()
	client := newTestClient(shared, "experimental-demo")
	client.mustApply(t, Command{Action: "demo"})
	if !shared.simulation.NeedsBufferState() {
		t.Fatal("demo lost the project's buffer policy")
	}
	client.mustApply(t, Command{Action: "reset"})
	checkExperimentalPolicies(t, shared, true, true)
}

func TestExperimentalPolicyFileRestore(t *testing.T) {
	t.Parallel()
	config := project.Default()
	config.StationBuffers, config.PickupReassignment = true, true
	store := &fakeStore{}
	shared := startFromStore(t, StoreInput{Store: store, Project: &config})
	checkExperimentalPolicies(t, shared, true, true)
	shared.Close()
	if err := shared.SaveState(t.Context(), SaveFinal); err != nil {
		t.Fatal(err)
	}
	data := store.writeList()[len(store.writeList())-1]
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("enabled%t", enabled), func(t *testing.T) {
			t.Parallel()
			effective := config
			effective.StationBuffers, effective.PickupReassignment = project.PolicyFlag(enabled), project.PolicyFlag(enabled)
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
			checkExperimentalPolicies(t, restored, enabled, enabled)
		})
	}
}

func TestExperimentalBufferedPolicyOverride(t *testing.T) {
	t.Parallel()
	config := project.Default()
	config.Fleet = config.Fleet[:1]
	config.StationBuffers, config.PickupReassignment = true, true
	for index := range config.Network.Nodes {
		config.Network.Nodes[index].Position.X *= 4
		config.Network.Nodes[index].Position.Y *= 4
	}
	for index := range config.Network.Lanes {
		if config.Network.Lanes[index].ID == "market-approach" {
			config.Network.Lanes[index].StationRole = sim.StationEntryRole
		}
	}
	store := &fakeStore{}
	shared := startFromStore(t, StoreInput{Store: store, Project: &config})
	if err := shared.simulation.RequestTrip("market", "garden"); err != nil {
		t.Fatal(err)
	}
	if !shared.simulation.ExportState().Pods[0].StationBuffered {
		t.Fatal("fixture did not create a buffered pickup")
	}
	shared.Close()
	if err := shared.SaveState(t.Context(), SaveFinal); err != nil {
		t.Fatal(err)
	}
	data := store.writeList()[len(store.writeList())-1]
	for _, test := range []struct {
		name                  string
		buffers, reassignment bool
		projectFile           bool
	}{
		{name: "saved project", buffers: true, reassignment: true},
		{name: "both disabled", projectFile: true},
		{name: "buffers only", buffers: true, projectFile: true},
		{name: "reassignment only", reassignment: true, projectFile: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var selected *project.Config
			if test.projectFile {
				effective := config
				effective.StationBuffers = project.PolicyFlag(test.buffers)
				effective.PickupReassignment = project.PolicyFlag(test.reassignment)
				selected = &effective
			}
			restored := startFromStore(t, StoreInput{Store: &fakeStore{data: data}, Project: selected})
			defer restored.Close()
			info := restored.State().Restore
			if info.Tier != "physical" || info.Demoted+info.Requeued+info.Dropped != 0 || !restored.simulation.ExportState().Pods[0].StationBuffered {
				t.Fatalf("policy override lost pending buffer membership: %+v", info)
			}
			for range 1200 * sim.TicksPerSecond {
				restored.advance()
				if _, err := restored.simulation.SafetyObservation().Check(); err != nil {
					t.Fatal(err)
				}
				if restored.simulation.Snapshot().Completed == 1 {
					break
				}
			}
			if restored.simulation.Snapshot().Completed != 1 || restored.simulation.NeedsBufferState() != test.buffers {
				t.Fatal("restored membership did not drain under the selected buffer policy")
			}
			if restored.Project().Project.PickupReassignment != project.PolicyFlag(test.reassignment) {
				t.Fatal("restore did not use the selected reassignment policy")
			}
		})
	}
}
