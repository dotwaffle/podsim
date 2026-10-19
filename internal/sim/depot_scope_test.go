package sim

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// depotScope is one part of the depot scope network: an Example copy with
// a depot of lanes lanes of slots slots, and an ID prefix.
type depotScope struct {
	prefix       string
	lanes, slots int
}

// depotScopes are the parts of the depot scope network. Their lane counts
// differ, so a lane index of one depot is not a lane of each other depot.
var depotScopes = []depotScope{{"a-", 2, 3}, {"b-", 3, 2}, {"c-", 1, 4}}

// depotScopeNetwork returns the depot scope network: the parts 5 km apart,
// each with its own Parking depot.
func depotScopeNetwork(scopes []depotScope) Network {
	parts := make([]Network, 0, len(scopes))
	for index, scope := range scopes {
		parts = append(parts, scopedNetwork(shortDepotNetwork(scope.lanes, scope.slots), scope.prefix, Point{Y: 5000 * float64(index)}))
	}
	return joinNetworks(parts...)
}

// depotScopePlacements returns, for each part, the pods of depotPlacements
// with the count of the part, with or without the passenger pods.
func depotScopePlacements(scopes []depotScope, counts []int, passengers bool) []Placement {
	var placements []Placement
	for index, scope := range scopes {
		part := depotPlacements(shortDepotNetwork(scope.lanes, scope.slots), counts[index])
		if !passengers {
			part = part[3:]
		}
		placements = append(placements, scopedPlacements(part, scope.prefix)...)
	}
	return placements
}

// TestDepotScopeBinding sends a pod to each of three depots at the same
// tick. Each binds in its own depot, in the same tick as the others, to
// the lane of that depot with the fewest pods, and parks there.
func TestDepotScopeBinding(t *testing.T) {
	t.Parallel()
	network := depotScopeNetwork(depotScopes)
	s, err := NewFleet(network, depotScopePlacements(depotScopes, []int{1, 2, 2}, true))
	if err != nil {
		t.Fatal(err)
	}
	var pods []*vehicle
	for _, scope := range depotScopes {
		v := s.findVehicle(scope.prefix + "market-pod")
		if err := s.startEmptyMove(v, emptyDestination{station: scope.prefix + "parking", depot: true}); err != nil {
			t.Fatal(err)
		}
		pods = append(pods, v)
	}
	ticks := make(map[string]int64)
	stepDepot(t, s, 300*TicksPerSecond, func() bool {
		idle := true
		for _, v := range pods {
			if _, ok := s.depots.slot(v.destination.ID); ok && ticks[v.Pod.ID] == 0 {
				ticks[v.Pod.ID] = s.tick
			}
			idle = idle && v.Pod.Activity == Idle
		}
		return idle
	})
	want := []string{"a-parking-2-1", "b-parking-3-1", "c-parking-1-3"}
	for index, v := range pods {
		if v.Pod.StationID != depotScopes[index].prefix+"parking" || v.Pod.BerthID != want[index] || v.parkedSince <= ticks[v.Pod.ID] {
			t.Fatalf("%s parks at %s %s with the parked tick %d, want %s after the binding at %d", v.Pod.ID, v.Pod.StationID, v.Pod.BerthID, v.parkedSince, want[index], ticks[v.Pod.ID])
		}
		if ticks[v.Pod.ID] != ticks[pods[0].Pod.ID] {
			t.Fatalf("bindings at ticks %v, want one tick", ticks)
		}
	}
}

// TestDepotScopeOldestHead gives the heads of three depots parked ticks,
// so that the oldest head of each depot is in another lane, and the oldest
// head of all depots is in depot b. A trip in each part then takes the
// oldest head of its own depot, and the other heads stay.
func TestDepotScopeOldestHead(t *testing.T) {
	t.Parallel()
	network := depotScopeNetwork(depotScopes)
	s, err := NewFleet(network, depotScopePlacements(depotScopes, []int{4, 3, 2}, false))
	if err != nil {
		t.Fatal(err)
	}
	for range 60 {
		s.Step()
	}
	parked := map[string]int64{"a-depot-pod-01": 50, "a-depot-pod-02": 20, "b-depot-pod-01": 30, "b-depot-pod-02": 10, "b-depot-pod-03": 40, "c-depot-pod-01": 25}
	for id, tick := range parked {
		s.findVehicle(id).parkedSince = tick
	}
	oldest := []string{"a-depot-pod-02", "b-depot-pod-02", "c-depot-pod-01"}
	all := func(*vehicle) bool { return true }
	for index, scope := range depotScopes {
		depot := s.depots.stations[scope.prefix+"parking"]
		if head := s.depotOldestHead(depot, all); head == nil || head.Pod.ID != oldest[index] {
			t.Fatalf("depot %s: oldest head %v, want %s", scope.prefix, head, oldest[index])
		}
	}
	gate := depotGate{s: s, accept: all}
	for _, id := range slices.Sorted(func(yield func(string) bool) {
		for id := range parked {
			if !yield(id) {
				return
			}
		}
	}) {
		if got, want := gate.releasable(s.findVehicle(id)), slices.Contains(oldest, id); got != want {
			t.Fatalf("%s releasable %t, want %t", id, got, want)
		}
	}
	for _, scope := range depotScopes {
		if _, err := s.SubmitTrip(scope.prefix+"harbor", scope.prefix+"garden"); err != nil {
			t.Fatal(err)
		}
	}
	stepDepot(t, s, 60*TicksPerSecond, func() bool {
		return !slices.ContainsFunc(oldest, func(id string) bool { return s.findVehicle(id).Pod.Activity == Idle })
	})
	for id := range parked {
		if v := s.findVehicle(id); !slices.Contains(oldest, id) && (v.Pod.Activity != Idle || s.assigned(id)) {
			t.Fatalf("head %s left with the oldest heads", id)
		}
	}
}

// TestDepotScopeSavedState checks the depot scope of the saved state
// checks. A saved move from slot 2 of lane 1 of one depot to slot 1 of
// lane 1 of another depot is not a move-up. A demoted pod takes the slot
// behind the rear of its lane when a pod moves in lane 1 of another depot.
func TestDepotScopeSavedState(t *testing.T) {
	t.Parallel()
	scopes := []depotScope{{"a-", 1, 3}, {"b-", 1, 3}}
	network := depotScopeNetwork(scopes)
	s, err := NewFleet(network, []Placement{
		{ID: "a-harbor-pod", StationID: "a-harbor"}, {ID: "a-garden-pod", StationID: "a-garden"},
		{ID: "a-head", StationID: "a-parking", BerthID: "a-parking-1-1"}, {ID: "a-rear", StationID: "a-parking", BerthID: "a-parking-1-2"},
		{ID: "b-head", StationID: "b-parking", BerthID: "b-parking-1-1"}, {ID: "b-rear", StationID: "b-parking", BerthID: "b-parking-1-2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for range 10 {
		s.Step()
	}
	pod := func(state *SavedState, id string) *SavedPod {
		return &state.Pods[slices.IndexFunc(state.Pods, func(p SavedPod) bool { return p.ID == id })]
	}
	lanes := func(prefix string, ids ...string) []int {
		var indexes []int
		for _, id := range ids {
			indexes = append(indexes, s.graph.lanes[prefix+id])
		}
		return indexes
	}
	t.Run("move-up across depots", func(t *testing.T) {
		t.Parallel()
		state := s.ExportState()
		p := pod(&state, "b-rear")
		p.Activity, p.StationID, p.BerthID, p.Origin, p.Destination, p.ParkedSince = "traveling", "", "", "b-parking-1-2", "a-parking-1-1", 5
		want := `pod "b-rear" has a parked tick and is not parked in a depot lane`
		if err := checkSavedDepotFields(RestoreStateInput{Network: network, Fleet: s.initial, State: state}); err == nil || err.Error() != want {
			t.Fatalf("got %v, want %q", err, want)
		}
	})
	t.Run("demotion beside a moving lane", func(t *testing.T) {
		t.Parallel()
		// The head of depot b travels to its slot, and the rear of depot
		// a names the tail slot with a route that demotes.
		state := s.ExportState()
		bind := func(p *SavedPod, prefix string, slot int, route []int) {
			p.Activity, p.StationID, p.BerthID, p.LaneID, p.LaneDistance, p.Distance = "traveling", "", "", prefix+"return-to-parking", 50, 50
			p.RelocatingTo, p.DestinationStation, p.Destination, p.Origin, p.Route, p.RouteIndex = prefix+"parking", prefix+"parking", fmt.Sprintf("%sparking-1-%d", prefix, slot), prefix+"garden-1", route, 0
		}
		pod(&state, "b-rear").StationID, pod(&state, "b-rear").BerthID = "b-harbor", "b-harbor-1"
		bind(pod(&state, "b-head"), "b-", 1, lanes("b-", "return-to-parking", "parking-fan-1", "parking-in-1", "parking-chain-1-2", "parking-chain-1-1"))
		rear := pod(&state, "a-rear")
		bind(rear, "a-", 3, lanes("a-", "return-to-parking", "parking-fan-1", "parking-in-1"))
		rear.RouteIndex = -1
		restored, result, err := RestoreState(RestoreStateInput{Network: network, Fleet: s.initial, State: state})
		if err != nil || result.Tier != RestorePhysical || !slices.Equal(result.Demoted, []string{"a-rear"}) {
			t.Fatalf("restore %+v, error %v", result, err)
		}
		if v := restored.findVehicle("a-rear"); v.Pod.Activity != Idle || v.Pod.BerthID != "a-parking-1-2" || v.parkedSince != state.Tick {
			t.Fatalf("the demoted pod is at %s %s with the parked tick %d", v.Pod.StationID, v.Pod.BerthID, v.parkedSince)
		}
		if v := restored.findVehicle("b-head"); v.Pod.Activity == Idle || !strings.HasPrefix(v.destination.ID, "b-parking-1-1") {
			t.Fatalf("the head of depot b is %s to %s", v.Pod.Activity, v.destination.ID)
		}
	})
}
