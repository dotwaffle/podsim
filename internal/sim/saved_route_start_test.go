package sim

import (
	"fmt"
	"math/rand/v2"
	"testing"
)

// referenceSavedRouteStart is savedRouteStart as it was before it walked
// the lanes. It walks each block up to the current block.
func (v *vehicle) referenceSavedRouteStart() (start int, offset float64, current int) {
	start = -1
	for index, b := range v.blocks.peekSpan(0, v.blockIndex+1) {
		if index > 0 && b.cell == 0 {
			current++
		}
		if start < 0 && b.last && b.end+blockTail(b) > v.distance {
			start, offset = current, b.laneStart
		}
	}
	if start < 0 {
		start, offset = current, v.blocks.lanes[v.blocks.locate(v.blockIndex, v.blocks.scan)].start
	}
	return start, offset, current
}

// savedRouteStartCounts counts the cases of checkSavedRouteStart.
type savedRouteStartCounts struct {
	// pods counts the checked pods. earlier counts pods whose saved route
	// starts before the current lane.
	pods, earlier int
}

// checkSavedRouteStart requires that savedRouteStart gives the result of
// referenceSavedRouteStart, with the same bits, for each traveling pod.
func checkSavedRouteStart(t *testing.T, s *Simulation, counts *savedRouteStartCounts) {
	t.Helper()
	for index := range s.vehicles {
		v := &s.vehicles[index]
		if v.Pod.Activity != Traveling || v.blocks.len() == 0 {
			continue
		}
		start, offset, current := v.savedRouteStart()
		wantStart, wantOffset, wantCurrent := v.referenceSavedRouteStart()
		if start != wantStart || offset != wantOffset || current != wantCurrent {
			t.Fatalf("tick %d pod %s: savedRouteStart gives %d, %v, %d, reference %d, %v, %d",
				s.tick, v.Pod.ID, start, offset, current, wantStart, wantOffset, wantCurrent)
		}
		counts.pods++
		if start < current {
			counts.earlier++
		}
	}
}

// TestSavedRouteStartMatchesReference compares savedRouteStart with the walk
// of each block at each tick, on a busy London network.
func TestSavedRouteStartMatchesReference(t *testing.T) {
	t.Parallel()
	skipLong(t)
	network := scenarioNetworks(t)["London"]
	var placements []Placement
	for _, station := range network.Stations {
		for _, berth := range station.Berths {
			if len(placements) < 160 {
				placements = append(placements, Placement{ID: fmt.Sprintf("%03d", len(placements)+1), StationID: station.ID, BerthID: berth.ID})
			}
		}
	}
	s, err := NewFleet(network, placements)
	if err != nil {
		t.Fatal(err)
	}
	rng := rand.New(rand.NewPCG(20261111, 1))
	var counts savedRouteStartCounts
	for tick := range 300 * TicksPerSecond {
		if tick%TicksPerSecond == 0 && len(s.waiting) < 120 {
			for range 3 {
				from := network.Stations[rng.IntN(len(network.Stations))].ID
				to := network.Stations[rng.IntN(len(network.Stations))].ID
				if from != to {
					_ = s.RequestTrip(from, to)
				}
			}
		}
		s.Step()
		checkSavedRouteStart(t, s, &counts)
	}
	t.Logf("%+v", counts)
	if counts.pods == 0 || counts.earlier == 0 {
		t.Fatalf("%+v: want traveling pods, and saved routes that start before the current lane", counts)
	}
}
