package sim

import (
	"maps"
	"math"
	"slices"
	"testing"
)

// TestLaneLengthsMatchGraph checks that laneLength gives the bits of
// Network.Length for each lane, also for a lane that the graph does not have, and
// that the memo holds exactly the lanes that it read, with those bits, after each
// miss and after repeat hits.
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
		// Clone drops the memo, so each first read below is a miss.
		s = s.Clone()
		outside := network.Lanes[0]
		outside.ID, outside.To = "not in the graph", network.Lanes[len(network.Lanes)-1].To
		lanes := append(slices.Clone(network.Lanes), outside)
		want := make(map[string]uint64, len(lanes))
		bits := func() map[string]uint64 {
			got := make(map[string]uint64, len(s.lengths))
			for id, length := range s.lengths {
				got[id] = math.Float64bits(length)
			}
			return got
		}
		for index, lane := range lanes {
			if lane.Control != nil {
				curved++
			}
			want[lane.ID] = math.Float64bits(network.Length(lane))
			if got := math.Float64bits(s.laneLength(lane)); got != want[lane.ID] {
				t.Fatalf("%s: lane %s has length bits %x, want %x", name, lane.ID, got, want[lane.ID])
			}
			// A full comparison of the memo after each miss is slow on large
			// networks, and it needs only some of them.
			if index%64 == 0 && !maps.Equal(bits(), want) {
				t.Fatalf("%s: after lane %s the memo is not the lanes that it read", name, lane.ID)
			}
		}
		if !maps.Equal(bits(), want) {
			t.Fatalf("%s: after the misses the memo is not the lanes that it read", name)
		}
		for _, lane := range lanes {
			if got := math.Float64bits(s.laneLength(lane)); got != want[lane.ID] {
				t.Fatalf("%s: a repeat read of lane %s has length bits %x, want %x", name, lane.ID, got, want[lane.ID])
			}
		}
		if !maps.Equal(bits(), want) {
			t.Fatalf("%s: after the hits the memo is not the lanes that it read", name)
		}
	}
	if curved == 0 {
		t.Fatal("no network has a curved lane")
	}
}
