package project_test

import (
	"math/rand/v2"
	"reflect"
	"slices"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
)

// moveNodes returns config with each node moved by up to 2 m on each axis,
// and with the control point of each curved lane moved by as much.
func moveNodes(config project.Config, random *rand.Rand) project.Config {
	network := config.Network
	network.Nodes, network.Lanes = slices.Clone(network.Nodes), slices.Clone(network.Lanes)
	for index := range network.Nodes {
		network.Nodes[index].Position.X += 4*random.Float64() - 2
		network.Nodes[index].Position.Y += 4*random.Float64() - 2
	}
	for index, lane := range network.Lanes {
		if lane.Control != nil {
			network.Lanes[index].Control = &sim.Point{X: lane.Control.X + 4*random.Float64() - 2, Y: lane.Control.Y + 4*random.Float64() - 2}
		}
	}
	config.Network = network
	return config
}

// TestParkingRoutesMatchGapsOnPresets compares the kept parking route result
// with a new check on each preset, after moves of each node. It does so for
// the preset, which has no gap, and for the preset without the lanes that
// enter its parking stations, which has gaps. The LondonFull presets run
// only without -short.
func TestParkingRoutesMatchGapsOnPresets(t *testing.T) {
	t.Parallel()
	presets := []struct {
		name   string
		config func() project.Config
	}{
		{name: "default", config: project.Default},
		{name: "small", config: scenarios.Small},
		{name: "parking-constrained", config: scenarios.ParkingConstrained},
		{name: "rail-hub", config: scenarios.RailHub},
		{name: "london", config: scenarios.LondonCentral},
	}
	if !testing.Short() {
		presets = append(presets, struct {
			name   string
			config func() project.Config
		}{name: "london-full", config: scenarios.LondonFull}, struct {
			name   string
			config func() project.Config
		}{name: "london-full-express", config: scenarios.LondonFullExpress})
	}
	for _, preset := range presets {
		t.Run(preset.name, func(t *testing.T) {
			t.Parallel()
			random := rand.New(rand.NewPCG(5, 6))
			config := preset.config()
			cut := config
			for _, station := range config.Network.Stations {
				if station.ParkingOnly {
					cut.Network.Lanes = slices.DeleteFunc(slices.Clone(cut.Network.Lanes), func(lane sim.Lane) bool { return lane.To == station.Entry })
				}
			}
			if gaps := project.ParkingGaps(cut); len(gaps) == 0 {
				t.Fatal("the preset without the lanes into parking has no gap")
			}
			var kept project.ParkingRoutes
			for _, state := range []project.Config{config, cut, config} {
				want := project.ParkingGaps(state)
				if got := kept.Gaps(state); !reflect.DeepEqual(got, want) {
					t.Fatalf("%d gaps, want %d", len(got), len(want))
				}
				for range 3 {
					moved := moveNodes(state, random)
					got, fresh := kept.Gaps(moved), project.ParkingGaps(moved)
					if !reflect.DeepEqual(got, fresh) || !reflect.DeepEqual(got, want) {
						t.Fatalf("after a move: %d kept gaps, %d gaps of a new check, %d gaps before the move", len(got), len(fresh), len(want))
					}
				}
			}
		})
	}
}
