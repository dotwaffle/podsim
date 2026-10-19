package scenarios

import (
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

// TestLondonValidateNetworkMatchesPrepare checks that sim.ValidateNetwork
// gives the error of sim.PrepareNetwork on the London presets with depots
// and the suggested Compact stations, and on each one with the second bay
// of its first maneuver group moved onto the first bay. Without -short it
// also checks LondonFull.
func TestLondonValidateNetworkMatchesPrepare(t *testing.T) {
	t.Parallel()
	central := DefaultLondonCentralOptions()
	central.ParkingLanes, central.ParkingBerths, central.CompactStations = 2, 6, suggestedCompactStations[:7]
	builds := []func() (sim.Network, error){func() (sim.Network, error) {
		config, err := LondonCentralWith(central)
		return config.Network, err
	}}
	if !testing.Short() {
		full := DefaultLondonFullOptions()
		full.ParkingLanes, full.ParkingBerths, full.CompactStations = 2, 6, suggestedCompactStations
		builds = append(builds, func() (sim.Network, error) {
			config, err := LondonFullWith(full)
			return config.Network, err
		})
	}
	for _, build := range builds {
		network, err := build()
		if err != nil {
			t.Fatal(err)
		}
		checkValidateNetwork(t, network, false)
		checkValidateNetwork(t, overlappingBays(network), true)
	}
}

// overlappingBays returns a copy of a network with the node of the second
// bay of its first maneuver group 0.5 m from the node of the first bay.
func overlappingBays(network sim.Network) sim.Network {
	group := network.ManeuverGroups[0]
	nodes := make(map[string]string)
	for _, station := range network.Stations {
		for _, berth := range station.Berths {
			nodes[berth.ID] = berth.Node
		}
	}
	positions := make(map[string]sim.Point, len(network.Nodes))
	for _, node := range network.Nodes {
		positions[node.ID] = node.Position
	}
	first := positions[nodes[group.BerthIDs[0]]]
	network.Nodes = append([]sim.Node(nil), network.Nodes...)
	for index := range network.Nodes {
		if network.Nodes[index].ID == nodes[group.BerthIDs[1]] {
			network.Nodes[index].Position = sim.Point{X: first.X + 0.5, Y: first.Y}
		}
	}
	return network
}

// checkValidateNetwork compares the two errors for a network, and checks
// that the network is refused when refused is true.
func checkValidateNetwork(t *testing.T, network sim.Network, refused bool) {
	t.Helper()
	_, want := sim.PrepareNetwork(network)
	got := sim.ValidateNetwork(network)
	if (got == nil) != (want == nil) || got != nil && got.Error() != want.Error() {
		t.Errorf("ValidateNetwork %v, PrepareNetwork %v", got, want)
	}
	if (want != nil) != refused {
		t.Errorf("PrepareNetwork %v, want a refusal: %t", want, refused)
	}
}
