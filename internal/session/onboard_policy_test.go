package session

import (
	"errors"
	"reflect"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

func TestRestoreOnboardPolicyCompatibility(t *testing.T) {
	for _, savedPolicy := range []bool{false, true} {
		for _, selectedPolicy := range []bool{false, true} {
			saved := project.Default()
			saved.SharedRidePartyLimit = 2
			saved.OnboardPickups = savedPolicy
			selected := project.Clone(saved)
			selected.OnboardPickups = selectedPolicy
			got, err := restoreProject(loadInput{project: &selected, steps: realRestoreSteps()}, saved)
			if err != nil || got.OnboardPickups != selectedPolicy {
				t.Fatalf("saved=%t selected=%t: %v", savedPolicy, selectedPolicy, err)
			}
			if !reflect.DeepEqual(got, selected) {
				t.Fatal("policy compatibility changed another project setting")
			}
			selected.Network.Stations[0].Berths[0].ID = "changed"
			if _, restoreErr := restoreProject(loadInput{project: &selected, steps: realRestoreSteps()}, saved); restoreErr == nil {
				t.Fatal("policy compatibility admitted a source topology change")
			}
		}
	}
}

func TestRestoreOnboardUsesSelectedRuntimePolicy(t *testing.T) {
	config := project.Default()
	config.SharedRidePartyLimit = 2
	config.OnboardPickups = true
	shared, err := NewWithProject(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shared.Close)
	file := sessionStateFile(t, shared)
	file.RestoreAttempts = 0
	for _, selectedPolicy := range []bool{false, true} {
		selected := project.Clone(config)
		selected.OnboardPickups = selectedPolicy
		called := false
		sentinel := errors.New("stop after restore input validation")
		steps := realRestoreSteps()
		steps.restoreSimulation = func(input sim.RestoreStateInput) (*sim.Simulation, sim.RestoreResult, error) {
			called = true
			if input.OnboardPickups != selectedPolicy {
				t.Fatal("restore ignored the selected runtime policy")
			}
			if !reflect.DeepEqual(input.State, file.Simulation) {
				t.Fatal("runtime policy changed the saved simulation")
			}
			return nil, sim.RestoreResult{}, sentinel
		}
		_, restoreErr := shared.loadState(loadInput{data: encodeTestState(t, file), project: &selected, steps: steps})
		if !called || !errors.Is(restoreErr, sentinel) {
			t.Fatalf("restore did not reach native policy boundary: %v", restoreErr)
		}
	}
}
