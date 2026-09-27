package project_test

import (
	"slices"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
)

// TestPassengerRoutesMatchReferenceOnPresets compares the passenger route
// check with the reference on each preset. It also removes lanes from each
// preset, which makes most of them fail the check.
func TestPassengerRoutesMatchReferenceOnPresets(t *testing.T) {
	t.Parallel()
	for _, preset := range []struct {
		name   string
		config project.Config
	}{
		{name: "default", config: project.Default()},
		{name: "small", config: scenarios.Small()},
		{name: "busy", config: scenarios.Busy()},
		{name: "parking-constrained", config: scenarios.ParkingConstrained()},
		{name: "rail-hub", config: scenarios.RailHub()},
		{name: "scale100", config: scenarios.Scale100()},
		{name: "london", config: scenarios.London()},
	} {
		t.Run(preset.name, func(t *testing.T) {
			t.Parallel()
			network := preset.config.Network
			if err := project.ComparePassengerRoutes(t, network); err != nil {
				t.Fatalf("preset fails the check: %v", err)
			}
			// Remove about 60 lanes of each preset, one at a time.
			step := max(1, len(network.Lanes)/60)
			failed := 0
			for index := 0; index < len(network.Lanes); index += step {
				changed := network
				changed.Lanes = slices.Delete(slices.Clone(network.Lanes), index, index+1)
				if project.ComparePassengerRoutes(t, changed) != nil {
					failed++
				}
			}
			if failed == 0 {
				t.Fatal("no removed lane made the check fail")
			}
		})
	}
}
