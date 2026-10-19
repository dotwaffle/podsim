package sim

import (
	"slices"
	"testing"
)

// TestManeuverGroupScopes runs two compact stations, each with its own
// maneuver group, 1 km apart. In each station, a pod leaves bay 1 for the
// stop, and a pod in bay 3 goes round the loop to bay 2, all at the same
// tick. The pods of different groups move on their group lanes at the same
// time, the pods of one group never do, and each pod reaches its berth.
// Then two pods with disjoint paths in one group wait for each other.
func TestManeuverGroupScopes(t *testing.T) {
	t.Parallel()
	prefixes := []string{"a-", "b-"}
	var parts []Network
	var placements []Placement
	for index, prefix := range prefixes {
		parts = append(parts, scopedNetwork(compactStationNetwork(6, 28.40), prefix, Point{Y: 1000 * float64(index)}))
		placements = append(placements, scopedPlacements([]Placement{
			{ID: "first", StationID: "bays", BerthID: "bay-1", Class: CompactClass},
			{ID: "third", StationID: "bays", BerthID: "bay-3", Class: CompactClass},
		}, prefix)...)
	}
	network := joinNetworks(parts...)
	s, err := NewFleet(network, placements)
	if err != nil {
		t.Fatal(err)
	}
	type move struct {
		pod, station, berth string
	}
	var moves []move
	for _, prefix := range prefixes {
		moves = append(moves, move{prefix + "first", prefix + "stop", prefix + "stop-1"}, move{prefix + "third", prefix + "bays", prefix + "bay-2"})
	}
	for _, m := range moves {
		station, _ := s.station(m.station)
		berth, _ := station.berth(m.berth)
		if err := s.startEmptyMove(s.findVehicle(m.pod), emptyDestination{station: m.station, berth: berth, reserveBerth: true}); err != nil {
			t.Fatal(err)
		}
	}
	both := false
	stepDepot(t, s, 600*TicksPerSecond, func() bool {
		active := make(map[string][]string)
		for i := range s.vehicles {
			v := &s.vehicles[i]
			if group, ok := s.maneuvers.laneGroups[v.Pod.LaneID]; ok && v.Pod.Activity != Idle {
				id := s.maneuvers.groups[group].ID
				active[id] = append(active[id], v.Pod.ID)
			}
		}
		for group, pods := range active {
			if len(pods) > 1 {
				t.Fatalf("tick %d: pods %q move in group %s at once", s.tick, pods, group)
			}
		}
		both = both || len(active) == len(prefixes)
		return !slices.ContainsFunc(moves, func(m move) bool {
			v := s.findVehicle(m.pod)
			return v.Pod.Activity != Idle || v.Pod.BerthID != m.berth
		})
	})
	if !both {
		t.Fatal("the groups never moved pods at the same time")
	}
	// The pod at the stop goes to bay 1. When it is on the approach, the
	// pod in bay 2 leaves for the stop. The two paths in the group have
	// no lane and no node in common, so only the group junction keeps the
	// pods apart.
	first, third := s.findVehicle("a-first"), s.findVehicle("a-third")
	enter, _ := s.station("a-bays")
	bay1, _ := enter.berth("a-bay-1")
	if err := s.startEmptyMove(first, emptyDestination{station: "a-bays", berth: bay1, reserveBerth: true}); err != nil {
		t.Fatal(err)
	}
	stop, _ := s.station("a-stop")
	started := false
	stepDepot(t, s, 600*TicksPerSecond, func() bool {
		if !started && first.Pod.LaneID == "a-approach" {
			if err := s.startEmptyMove(third, emptyDestination{station: "a-stop", berth: stop.Berths[0], reserveBerth: true}); err != nil {
				t.Fatal(err)
			}
			started = true
		}
		_, firstIn := s.maneuvers.laneGroups[first.Pod.LaneID]
		_, thirdIn := s.maneuvers.laneGroups[third.Pod.LaneID]
		if firstIn && thirdIn && first.Pod.Activity != Idle && third.Pod.Activity != Idle {
			t.Fatalf("tick %d: both pods move in the group, on %s and %s", s.tick, first.Pod.LaneID, third.Pod.LaneID)
		}
		return started && first.Pod.Activity == Idle && first.Pod.BerthID == "a-bay-1" && third.Pod.Activity == Idle && third.Pod.BerthID == "a-stop-1"
	})
}
