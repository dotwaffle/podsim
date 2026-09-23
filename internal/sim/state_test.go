package sim

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"flag"
	"maps"
	"math"
	"os"
	"reflect"
	"slices"
	"testing"
)

var update = flag.Bool("update", false, "write the golden files in testdata again")

const (
	// goldenStatePath holds the saved state of the traffic demo at
	// goldenStateTick.
	goldenStatePath = "testdata/saved_state_v1.json"
	goldenStateTick = 1500
	// demoTickLimit is more ticks than the traffic demo needs.
	demoTickLimit = 30000
)

// demoFleet is the fleet that StartDemo needs.
func demoFleet() []Placement {
	return []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"}}
}

// roundTripState encodes a saved state and decodes it strictly, as a state
// file does.
func roundTripState(t *testing.T, state SavedState) SavedState {
	t.Helper()
	data, err := json.Marshal(state, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	var decoded SavedState
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		t.Fatal(err)
	}
	return decoded
}

// comparablePod clears the pod fields that a restore sets to their start
// values.
func comparablePod(pod Pod) Pod {
	pod.Speed, pod.WaitReason, pod.BlockedBy = 0, NoWait, ""
	return pod
}

// cleanRestore reports whether a physical restore kept each pod and each
// order in place.
func cleanRestore(result RestoreResult) bool {
	return result.Tier == RestorePhysical && result.PhysicalError == nil &&
		len(result.Demoted)+len(result.Requeued)+len(result.Dropped) == 0 &&
		result.DroppedParties+result.OverCap+result.OverBudget == 0
}

// savedCounters clears the pods and the queue of a saved state.
func savedCounters(state SavedState) SavedState {
	state.Pods, state.Waiting = nil, nil
	return state
}

// checkRestoredMatches checks that a restore of the live state matches the
// live simulation, except for the pod speeds and wait reasons.
func checkRestoredMatches(t *testing.T, live, restored *Simulation) {
	t.Helper()
	if !maps.Equal(restored.owners, restored.retainedOwners()) {
		t.Fatalf("tick %d: the restored owners differ from the retention rules", live.tick)
	}
	if _, err := restored.SafetyObservation().Check(); err != nil {
		t.Fatalf("tick %d: %v", live.tick, err)
	}
	want, got := live.ExportState(), restored.ExportState()
	if !reflect.DeepEqual(savedCounters(got), savedCounters(want)) {
		t.Fatalf("tick %d: counters\n got %+v\nwant %+v", live.tick, savedCounters(got), savedCounters(want))
	}
	if !reflect.DeepEqual(got.Waiting, want.Waiting) {
		t.Fatalf("tick %d: queue\n got %+v\nwant %+v", live.tick, got.Waiting, want.Waiting)
	}
	for index := range live.vehicles {
		source, copied := &live.vehicles[index], &restored.vehicles[index]
		if comparablePod(copied.Pod) != comparablePod(source.Pod) {
			t.Fatalf("tick %d: pod\n got %+v\nwant %+v", live.tick, copied.Pod, source.Pod)
		}
		if (copied.pending >= 0) != (source.pending >= 0) ||
			copied.pending >= 0 && (copied.pending != copied.reservedThrough+1 || copied.waitSince != source.waitSince) {
			t.Fatalf("tick %d: pod %s waits for block %d since %d, want a wait since %d",
				live.tick, copied.Pod.ID, copied.pending, copied.waitSince, source.waitSince)
		}
		gotPod, wantPod := got.Pods[index], want.Pods[index]
		if math.Abs(gotPod.Distance-wantPod.Distance) > restoreTolerance {
			t.Fatalf("tick %d: pod %s distance %v, want %v", live.tick, gotPod.ID, gotPod.Distance, wantPod.Distance)
		}
		gotPod.Distance = wantPod.Distance
		if !reflect.DeepEqual(gotPod, wantPod) {
			t.Fatalf("tick %d: saved pod\n got %+v\nwant %+v", live.tick, gotPod, wantPod)
		}
	}
	if gap := restored.ordersGap(); gap != 0 {
		t.Fatalf("tick %d: order gap %d", live.tick, gap)
	}
}

// savedWork records the largest saved routes and the largest restore cost of
// the states that a run saves.
type savedWork struct {
	podRoute, tripRoute, cost int
}

func (w *savedWork) add(network Network, state SavedState) {
	for _, pod := range state.Pods {
		w.podRoute = max(w.podRoute, len(pod.Route))
	}
	for _, trip := range state.Waiting {
		w.tripRoute = max(w.tripRoute, len(trip.Route))
	}
	w.cost = max(w.cost, savedStateCost(network, state))
}

// check fails when a saved pod route uses a quarter of its limit or more,
// when a saved trip route uses tripShare of its limit or more, or when the
// restore cost is more than a quarter of the block budget.
func (w *savedWork) check(t *testing.T, network Network, tripShare float64) {
	t.Helper()
	limits, budget := newRouteLimits(network), blockBudget(network)
	t.Logf("largest pod route %d of %d, largest trip route %d of %d, largest cost %d of %d",
		w.podRoute, limits.pod, w.tripRoute, limits.trip, w.cost, budget)
	if 4*w.podRoute >= limits.pod {
		t.Errorf("a saved pod route uses a quarter of its limit or more")
	}
	if float64(w.tripRoute) >= tripShare*float64(limits.trip) {
		t.Errorf("a saved trip route uses %v of its limit or more", tripShare)
	}
	if 4*w.cost > budget {
		t.Errorf("the restore cost is more than a quarter of the budget")
	}
}

// savedStateCost counts the blocks of the saved pod routes and the lanes of
// the saved trip routes.
func savedStateCost(network Network, state SavedState) int {
	cost := 0
	for _, pod := range state.Pods {
		for _, lane := range pod.Route {
			cost += laneBlockCount(network.Length(network.Lanes[lane]))
		}
	}
	for _, trip := range state.Waiting {
		cost += len(trip.Route)
	}
	return cost
}

// blockBudget returns the largest restore cost for a network.
func blockBudget(network Network) int {
	blocks := 0
	for _, lane := range network.Lanes {
		blocks += laneBlockCount(network.Length(lane))
	}
	return budgetNetworkMultiple*blocks + budgetLaneBlocks*len(network.Lanes)
}

func TestRestorePhysicalDemo(t *testing.T) {
	t.Parallel()
	live := newTraffic(t)
	if err := live.StartDemo(); err != nil {
		t.Fatal(err)
	}
	var work savedWork
	restores, trimmed := 0, 0
	for live.demo != nil {
		if live.tick >= demoTickLimit {
			t.Fatalf("the demo did not end in %d ticks", demoTickLimit)
		}
		live.Step()
		if live.tick%10 != 0 {
			continue
		}
		state := roundTripState(t, live.ExportState())
		restored, result, err := RestoreState(RestoreStateInput{Network: Example(), Fleet: demoFleet(), State: state})
		if err != nil {
			t.Fatalf("tick %d: %v", live.tick, err)
		}
		if !cleanRestore(result) {
			t.Fatalf("tick %d: result %+v", live.tick, result)
		}
		checkRestoredMatches(t, live, restored)
		work.add(Example(), state)
		for index, pod := range state.Pods {
			if len(pod.Route) < len(live.vehicles[index].Route) {
				trimmed++
			}
		}
		restores++
	}
	if live.demoError != "" || live.completed < demoJourneys {
		t.Fatalf("the demo ended with %d journeys: %s", live.completed, live.demoError)
	}
	// A restore at each lane of a route does not test the trimmed routes.
	if trimmed == 0 {
		t.Fatal("no saved route was trimmed")
	}
	// A trip route is one shortest path. In the example network, the path
	// from Market to Garden already has 9 lanes, which is half the trip limit.
	work.check(t, Example(), 0.6)
	t.Logf("restores=%d trimmed=%d ticks=%d", restores, trimmed, live.tick)
}

// TestExportStateLimitsRoutes saves live routes at and over their limits, and
// restores the result.
func TestExportStateLimitsRoutes(t *testing.T) {
	t.Parallel()
	network := cycleNetwork(2)
	fleet := []Placement{{ID: "01", StationID: "s", BerthID: "s-0"}, {ID: "02", StationID: "s", BerthID: "s-1"}}
	limits := newRouteLimits(network)
	for _, tc := range []struct {
		name string
		// entries is the length of the live route. A pod is on the lane at
		// routeIndex. When trip is true, a queued trip has the route.
		entries, routeIndex int
		trip                bool
		// saved is the length of the saved route. It is 0 when the export
		// leaves out the route.
		saved int
	}{
		{name: "pod route at the limit", entries: limits.pod, saved: limits.pod},
		{name: "pod route over the limit", entries: limits.pod + 1},
		{name: "pod route within the limit after the trim", entries: limits.pod + 5, routeIndex: 8, saved: limits.pod - 3},
		{name: "trip route at the limit", entries: limits.trip, trip: true, saved: limits.trip},
		{name: "trip route over the limit", entries: limits.trip + 1, trip: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, err := NewFleet(network, fleet)
			if err != nil {
				t.Fatal(err)
			}
			s.tick = restoreTick
			route := make([]Lane, tc.entries)
			for position, index := range cycleRoute(s, tc.entries) {
				route[position] = network.Lanes[index]
			}
			if tc.trip {
				s.requestID = 1
				s.waiting = []waitingTrip{{
					request: Request{ID: 1, From: "s", To: "t", PartySize: 1, PodID: "02", RequestedTick: 500},
					route:   route, deferUntil: restoreTick + 100, deferCheck: restoreTick + 30, deferPodID: "02",
				}}
			} else {
				// Pod 01 relocates to berth t-1. It is 50 meters into the lane
				// at routeIndex.
				v := &s.vehicles[0]
				v.Pod = Pod{ID: v.Pod.ID, Activity: Traveling, LaneID: route[tc.routeIndex].ID, LaneDistance: 50}
				v.RelocatingTo, v.destinationStation = "t", "t"
				v.origin, v.destination = network.Stations[0].Berths[0], network.Stations[1].Berths[0]
				s.setVehicleRoute(v, route)
				first, _ := routeLaneBlocks(v.blocks, tc.routeIndex)
				v.distance = v.blocks[first].laneStart + 50
				v.blockIndex = first + slices.IndexFunc(v.blocks[first:], func(b block) bool { return b.end >= v.distance })
				v.reservedThrough = v.blockIndex
			}
			state := s.ExportState()
			restored, result, err := RestoreState(RestoreStateInput{Network: network, Fleet: fleet, State: roundTripState(t, state)})
			if err != nil {
				t.Fatal(err)
			}
			if tc.trip {
				// Without its route, the trip also loses its pod bindings.
				trip, bound := state.Waiting[0], tc.saved > 0
				if len(trip.Route) != tc.saved || (trip.Request.PodID != "") != bound || (trip.DeferCheck != 0) != bound ||
					(trip.DeferPodID != "") != bound || trip.DeferUntil != restoreTick+100 {
					t.Fatalf("saved trip %+v", trip)
				}
				if got := restored.ExportState().Waiting; !cleanRestore(result) || !reflect.DeepEqual(got, state.Waiting) {
					t.Fatalf("restored queue %+v, result %+v", got, result)
				}
				return
			}
			// A pod without a saved route has no saved distance.
			pod, distance := state.Pods[0], 50.0
			if tc.saved == 0 {
				distance = 0
			}
			if len(pod.Route) != tc.saved || pod.RouteIndex != 0 || math.Abs(pod.Distance-distance) > restoreTolerance {
				t.Fatalf("saved pod has %d lanes, at %v in lane %d", len(pod.Route), pod.Distance, pod.RouteIndex)
			}
			if tc.saved == 0 {
				if !slices.Equal(result.Demoted, []string{"01"}) || result.OverCap != 1 {
					t.Fatalf("result %+v", result)
				}
				return
			}
			if v := findVehicle(t, restored, "01"); !cleanRestore(result) || v.Pod.Activity != Traveling || len(v.Route) != tc.saved {
				t.Fatalf("pod 01 is %q with %d lanes, result %+v", v.Pod.Activity, len(v.Route), result)
			}
		})
	}
}

// encodeGolden encodes a saved state in the golden file format.
func encodeGolden(t *testing.T, state SavedState) []byte {
	t.Helper()
	data, err := json.Marshal(state, json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		t.Fatal(err)
	}
	return append(data, '\n')
}

// TestSavedStateGolden fixes the encoding of the traffic demo state at
// goldenStateTick. That state does not use each member of the format. The
// test does not run Step, so the result does not depend on the
// floating-point behavior of a machine. Run the test with -update to write
// the file again.
func TestSavedStateGolden(t *testing.T) {
	t.Parallel()
	if *update {
		live := newTraffic(t)
		if err := live.StartDemo(); err != nil {
			t.Fatal(err)
		}
		advance(live, goldenStateTick)
		if err := os.WriteFile(goldenStatePath, encodeGolden(t, live.ExportState()), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(goldenStatePath)
	if err != nil {
		t.Fatal(err)
	}
	var state SavedState
	if err = json.Unmarshal(data, &state, json.RejectUnknownMembers(true)); err != nil {
		t.Fatal(err)
	}
	if state.Tick != goldenStateTick || state.Demo == nil ||
		!slices.ContainsFunc(state.Pods, func(pod SavedPod) bool { return pod.Activity == activityCode(Traveling) }) {
		t.Fatalf("the golden state has no demo or traveling pod: %+v", savedCounters(state))
	}
	restored, result, err := RestoreState(RestoreStateInput{Network: Example(), Fleet: demoFleet(), State: state})
	if err != nil {
		t.Fatal(err)
	}
	if !cleanRestore(result) {
		t.Fatalf("result %+v", result)
	}
	if got := encodeGolden(t, restored.ExportState()); !bytes.Equal(got, data) {
		t.Fatalf("the restored state encodes differently:\n%s", got)
	}
}

func TestOrdersGapCountsPartiesInSharedRides(t *testing.T) {
	t.Parallel()
	s := newTraffic(t)
	if err := s.StartDemo(); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSharedRidePartyLimit(4); err != nil {
		t.Fatal(err)
	}
	for second := range 900 {
		if second%30 == 0 {
			for _, trip := range [][2]string{{"harbor", "market"}, {"harbor", "market"}, {"harbor", "market"}, {"garden", "harbor"}, {"garden", "harbor"}} {
				if err := s.RequestTrip(trip[0], trip[1]); err != nil {
					t.Fatal(err)
				}
			}
		}
		advance(s, TicksPerSecond)
		state := s.ExportState()
		if gap, savedGap := s.ordersGap(), state.ordersGap(); gap != 0 || savedGap != 0 {
			t.Fatalf("second %d: order gap %d, saved order gap %d", second, gap, savedGap)
		}
		if second%30 != 0 {
			continue
		}
		restored, _, err := RestoreState(RestoreStateInput{Network: Example(), Fleet: demoFleet(), State: state})
		if err != nil {
			t.Fatalf("second %d: %v", second, err)
		}
		if gap := restored.ordersGap(); gap != 0 {
			t.Fatalf("second %d: restored order gap %d", second, gap)
		}
	}
	if s.sharedParties == 0 {
		t.Fatal("no party shared a ride")
	}
	t.Logf("submitted=%d completed=%d shared=%d", s.requestID, s.completed, s.sharedParties)
}
