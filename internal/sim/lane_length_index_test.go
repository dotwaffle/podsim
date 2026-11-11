package sim

import (
	"math"
	"slices"
	"testing"
)

// TestLaneLengthsMatchGraph checks that laneLength gives the bits of
// Network.Length for each lane, also for a lane that the graph does not have.
func TestLaneLengthsMatchGraph(t *testing.T) {
	t.Parallel()
	networks := scenarioNetworks(t)
	networks["no candidate"] = missNetwork(8, 5, 16)
	networks["bank example"] = BankExample()
	curved := 0
	for name, network := range networks {
		station := network.Stations[0]
		s, err := NewFleet(network, []Placement{{ID: "01", StationID: station.ID, BerthID: station.Berths[0].ID}})
		if err != nil {
			t.Fatal(name, err)
		}
		outside := network.Lanes[0]
		outside.ID, outside.To = "not in the graph", network.Lanes[len(network.Lanes)-1].To
		for _, lane := range append(slices.Clone(network.Lanes), outside) {
			if lane.Control != nil {
				curved++
			}
			want := math.Float64bits(network.Length(lane))
			if got := math.Float64bits(s.laneLength(lane)); got != want {
				t.Fatalf("%s: lane %s has length bits %x, want %x", name, lane.ID, got, want)
			}
		}
	}
	if curved == 0 {
		t.Fatal("no network has a curved lane")
	}
}
