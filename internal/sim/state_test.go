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
	"runtime"
	"slices"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "write the golden files in testdata again")

const (
	// goldenStatePath holds the saved state of the traffic demo at
	// goldenStateTick.
	goldenStatePath = "testdata/saved_state.json"
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

// comparablePod returns the pod of v with the fields that a restore sets
// to their start values cleared. A traveling pod keeps its speed, except a
// faulted pod, which restores at rest.
func comparablePod(v *vehicle) Pod {
	pod := v.Pod
	pod.WaitReason, pod.BlockedBy = NoWait, ""
	if v.faulted || pod.Activity != Traveling {
		pod.Speed = 0
	}
	return pod
}

// cleanRestore reports whether a physical restore kept each pod and each
// order in place.
func cleanRestore(result RestoreResult) bool {
	return result.Tier == RestorePhysical && result.PhysicalError == nil &&
		len(result.Demoted)+len(result.Requeued)+len(result.Dropped) == 0 &&
		result.DroppedParties+result.OverCap+result.OverBudget == 0
}

// exportTrimmed is the export of s when the whole routes do not fit the
// restore budget: each traveling pod saves its trimmed route.
func (s *Simulation) exportTrimmed() SavedState {
	return s.exportState(-1)
}

// savedCounters clears the pods and the queue of a saved state.
func savedCounters(state SavedState) SavedState {
	state.Pods, state.Waiting = nil, nil
	return state
}

// checkRestoredMatches checks that a restore of the live state matches the
// live simulation, except for the wait reasons and the speeds of faulted
// pods.
func checkRestoredMatches(t *testing.T, live, restored *Simulation) {
	t.Helper()
	if !maps.Equal(restored.owners, restored.retainedOwners()) {
		t.Fatalf("tick %d: the restored owners differ from the retention rules", live.tick)
	}
	if _, err := restored.SafetyObservation().Check(); err != nil {
		t.Fatalf("tick %d: %v", live.tick, err)
	}
	// The trimmed exports of the two do not depend on whether the restore
	// read a whole route or a trimmed route.
	want, got := live.exportTrimmed(), restored.exportTrimmed()
	if !reflect.DeepEqual(savedCounters(got), savedCounters(want)) {
		t.Fatalf("tick %d: counters\n got %+v\nwant %+v", live.tick, savedCounters(got), savedCounters(want))
	}
	if !reflect.DeepEqual(got.Waiting, want.Waiting) {
		t.Fatalf("tick %d: queue\n got %+v\nwant %+v", live.tick, got.Waiting, want.Waiting)
	}
	for index := range live.vehicles {
		source, copied := &live.vehicles[index], &restored.vehicles[index]
		if comparablePod(copied) != comparablePod(source) {
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
		// A restore of a trimmed route starts the route of the pod at the
		// saved start, so its own export of the same lanes has no marker.
		gotPod.Distance, gotPod.RouteTrimmed = wantPod.Distance, wantPod.RouteTrimmed
		if !reflect.DeepEqual(gotPod, wantPod) {
			t.Fatalf("tick %d: saved pod\n got %+v\nwant %+v", live.tick, gotPod, wantPod)
		}
	}
	if n := countUnaccounted(t, restored); n != 0 {
		t.Fatalf("tick %d: %d unaccounted orders", live.tick, n)
	}
	checkRestoredFrontiers(t, live, restored)
}

// checkRestoredFrontiers requires that each restored traveling pod has the
// frontier of the live pod, at the same route occurrence and cell. A
// trimmed saved route starts at a lane start, and the lanes share their
// cells, so the blocks of the two routes differ by the blocks before the
// saved route. It also requires that the export of each live pod finds no
// reservation that ends before the derived frontier, which exportPod would
// write as a lead of 0.
func checkRestoredFrontiers(t *testing.T, live, restored *Simulation) {
	t.Helper()
	state := live.ExportState()
	_, budget := physicalBlockBudget(live)
	plan := live.planSavedRoutes(newRouteLimits(live.network), budget, state.Waiting)
	for index := range live.vehicles {
		v, got, pod := &live.vehicles[index], &restored.vehicles[index], state.Pods[index]
		if v.Pod.Activity != Traveling || pod.Route == nil {
			continue
		}
		// A saved route is the end of the live route that starts at the
		// difference of their lengths, and so is a restored route.
		if lead, _ := live.savedMotion(v, len(v.Route)-len(pod.Route), pod, plan.frame(index)); lead < 0 {
			t.Fatalf("tick %d: pod %s reserves %d blocks ahead of the derived frontier", live.tick, pod.ID, lead)
		}
		start := len(v.Route) - len(got.Route)
		if want := v.reservedThrough - v.blocks.laneFirst(start); got.reservedThrough != want {
			t.Fatalf("tick %d: pod %s restores the frontier %d, want %d", live.tick, pod.ID, got.reservedThrough, want)
		}
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
	return min(budgetNetworkMultiple*blocks+budgetLaneBlocks*len(network.Lanes), budgetMaxBlocks)
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

// TestRestoreStateRefusalOrder pins the error that an invalid save gets.
// The checks before the tiers refuse the order contract first. An error of
// both tiers names each tier that the restore tried.
func TestRestoreStateRefusalOrder(t *testing.T) {
	t.Parallel()
	const counter = "a saved counter is negative or not finite"
	base := RestoreStateInput{Network: Example(), Fleet: demoFleet(), State: newTraffic(t).ExportState()}
	for _, test := range []struct {
		name     string
		edit     func(*RestoreStateInput)
		want     string
		physical bool
	}{
		{"order_before_faults", func(in *RestoreStateInput) {
			in.State.OrderContract, in.State.Faults = ExpressOrderContract, &SavedFaults{}
		}, "saved and input order contracts differ", false},
		{"order_before_counter", func(in *RestoreStateInput) {
			in.State.OrderContract, in.State.Tick = ExpressOrderContract, -1
		}, "saved and input order contracts differ", false},
		{"faults_before_emergencies", func(in *RestoreStateInput) {
			in.State.Faults, in.State.Emergencies = &SavedFaults{}, &SavedEmergencies{}
		}, "saved faults need faults on", false},
		{"emergencies", func(in *RestoreStateInput) { in.State.Emergencies = &SavedEmergencies{} }, "saved emergencies need emergencies on", false},
		{"both_tiers", func(in *RestoreStateInput) { in.State.Tick = -1 },
			"restore the saved simulation: physical tier: " + counter + ", logical tier: " + counter, true},
		{"logical_only", func(in *RestoreStateInput) { in.State.Tick, in.LogicalOnly = -1, true },
			"restore the saved simulation: logical tier: " + counter, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			in := base
			test.edit(&in)
			s, result, err := RestoreState(in)
			if s != nil || err == nil || err.Error() != test.want || (result.PhysicalError != nil) != test.physical {
				t.Fatalf("got %v, physical error %v, want %q", err, result.PhysicalError, test.want)
			}
		})
	}
}

// TestRestoreInputCheckOrder pins the order of the restore input checks.
// The first check that fails gives the refusal.
func TestRestoreInputCheckOrder(t *testing.T) {
	t.Parallel()
	want := []string{
		"checkRestoreOrderContract",
		"checkExpressSavedState",
		"checkContractRestoreSemantics",
		"checkSavedServiceLimits",
		"checkLargeLinkFields",
		"checkBoardingFields",
		"checkOperationalFields",
		"checkSavedFaults",
		"checkSavedEmergencies",
		"checkSavedServices",
		"checkSavedBankRoutes",
		"checkSavedDepotFields",
		"checkContractDispatchReasons",
	}
	var got []string
	for _, check := range restoreInputChecks {
		name := runtime.FuncForPC(reflect.ValueOf(check).Pointer()).Name()
		got = append(got, name[strings.LastIndex(name, ".")+1:])
	}
	if !slices.Equal(got, want) {
		t.Fatalf("restore input checks are %v, want %v", got, want)
	}
	// After these, checkRestoreInput checks the fault footprints, the saved
	// motion, the trimmed route markers, and last the reversal dwell
	// latches (exact restore coordinates proposal, EC-D3 and EC-D8).
	idle := RestoreStateInput{Network: Example(), Fleet: demoFleet(), State: newTraffic(t).ExportState()}
	for _, test := range []struct {
		edit func(*SavedPod)
		want string
	}{
		{func(pod *SavedPod) { pod.Speed, pod.RouteTrimmed, pod.ReversalLane = 1, true, 1 }, "pod 01 is not traveling and has a speed or a reservation ahead"},
		{func(pod *SavedPod) { pod.RouteTrimmed, pod.ReversalLane = true, 1 }, "pod 01 is not traveling and has a trimmed route"},
		{func(pod *SavedPod) { pod.ReversalLane = 1 }, "pod 01 is not traveling and has a reversal dwell latch"},
	} {
		input := idle
		input.State = withPod(input.State, 0, test.edit)
		if _, _, err := RestoreState(input); err == nil || err.Error() != test.want {
			t.Fatalf("restore %v, want %q", err, test.want)
		}
	}
}

func TestSavedRouteLimits(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name         string
		nodes, lanes int
		want         routeLimits
	}{
		{"small", 3, 5, routeLimits{pod: 8, trip: 3}},
		{"pod cap", MaxSavedRouteLanes - 1, 2, routeLimits{pod: MaxSavedRouteLanes, trip: MaxSavedRouteLanes - 1}},
		{"both caps", MaxSavedRouteLanes + 1, MaxSavedRouteLanes + 1, routeLimits{pod: MaxSavedRouteLanes, trip: MaxSavedRouteLanes}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			network := Network{Nodes: make([]Node, tc.nodes), Lanes: make([]Lane, tc.lanes)}
			if got := newRouteLimits(network); got != tc.want {
				t.Fatalf("route limits %+v, want %+v", got, tc.want)
			}
		})
	}
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
					request: Request{SharingConsent: SharedConsent, Service: OnDemandService, ID: 1, From: "s", To: "t", PartySize: 1, PodID: "02", RequestedTick: 500},
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
				first, _ := routeLaneBlocks(&v.blocks, tc.routeIndex)
				v.distance = v.blocks.at(first).laneStart + 50
				v.blockIndex = first + slices.IndexFunc(v.blocks.all()[first:], func(b block) bool { return b.end >= v.distance })
				v.reservedThrough = v.blockIndex
			}
			checkWholeRouteBlocks(t, s)
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

func TestSharedRidesAccountForEachOrder(t *testing.T) {
	t.Parallel()
	skipLong(t)
	s := newTraffic(t)
	monitorContractEachTick(t, s)
	if err := s.StartDemo(); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSharedRidePartyLimit(4); err != nil {
		t.Fatal(err)
	}
	for second := range 900 {
		if second%30 == 0 {
			for _, trip := range [][2]string{{"harbor", "market"}, {"harbor", "market"}, {"harbor", "market"}, {"garden", "harbor"}, {"garden", "harbor"}} {
				if err := submitSharedTrip(s, trip[0], trip[1]); err != nil {
					t.Fatal(err)
				}
			}
		}
		advance(s, TicksPerSecond)
		state := s.ExportState()
		if n, saved := countUnaccounted(t, s), countSavedUnaccounted(t, state); n != 0 || saved != 0 {
			t.Fatalf("second %d: %d unaccounted orders, %d saved", second, n, saved)
		}
		if second%30 != 0 {
			continue
		}
		restored, result, err := RestoreState(RestoreStateInput{Network: Example(), Fleet: demoFleet(), State: state})
		if err != nil || result.Tier != RestorePhysical {
			t.Fatalf("second %d: %v, %+v", second, err, result)
		}
		if n := countUnaccounted(t, restored); n != 0 {
			t.Fatalf("second %d: %d unaccounted orders after the restore", second, n)
		}
	}
	if s.sharedParties == 0 {
		t.Fatal("no party shared a ride")
	}
	t.Logf("submitted=%d completed=%d shared=%d", s.requestID, s.completed, s.sharedParties)
}

// checkWholeRouteBlocks checks that the count of the whole routes of s
// from its live routes, which ExportState compares with the budget, is the
// count of the saved routes of its export of the whole routes.
func checkWholeRouteBlocks(tb testing.TB, s *Simulation) {
	tb.Helper()
	if live, saved := s.WholeRouteBlocksForTest(); live != saved {
		tb.Fatalf("tick %d: the live routes have %d blocks, and the saved whole routes %d", s.tick, live, saved)
	}
}

// TestWholeRouteBlocks checks the count of the whole routes from the live
// routes at each tick of fixtures with traveling, departing, depot,
// Express, and maneuver group pods, and with a waiting queue.
func TestWholeRouteBlocks(t *testing.T) {
	t.Parallel()
	for name, build := range map[string]func(t *testing.T) *Simulation{
		"traffic": func(t *testing.T) *Simulation {
			t.Helper()
			s := newTraffic(t)
			if err := s.StartDemo(); err != nil {
				t.Fatal(err)
			}
			return s
		},
		"debris": func(t *testing.T) *Simulation {
			t.Helper()
			s := debrisFleet(t)
			startFault(t, s, returnTraveler(t, s), 0)
			return s
		},
		"depot": func(t *testing.T) *Simulation {
			t.Helper()
			network := shortDepotNetwork(2, 3)
			s := newDepotFleet(t, network, depotPlacements(network, 3))
			sendToDepot(t, s, "harbor-pod")
			return s
		},
		"express": func(t *testing.T) *Simulation {
			t.Helper()
			fleet := []Placement{{ID: "01", Class: ExpressClass, StationID: "harbor", BerthID: "harbor-1"}}
			s, err := expressPhysicalFleet(largeRestoreNetwork(), fleet)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.RequestJourneyOptions("01", TripOptions{To: "market", PartySize: 20, SharingConsent: PrivateConsent}); err != nil {
				t.Fatal(err)
			}
			return s
		},
		"maneuver group": func(t *testing.T) *Simulation {
			t.Helper()
			s, err := NewFleet(tallBayNetwork(2, 7), []Placement{{ID: "01", StationID: "bays", BerthID: "bay-1", Class: CompactClass}})
			if err != nil {
				t.Fatal(err)
			}
			if err := s.RequestJourney("01", "stop"); err != nil {
				t.Fatal(err)
			}
			return s
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := build(t)
			for range 90 * TicksPerSecond {
				s.Step()
				if s.tick%10 == 0 {
					checkWholeRouteBlocks(t, s)
				}
			}
		})
	}
}

// TestExportStateRouteBudget saves pods on the last lane of trimLanes, each
// with a route from l0. A save keeps the whole routes when their blocks
// fit the block budget of the restore, also exactly. With one block less,
// it saves the trimmed route of each pod, with the marker, and the restore
// keeps each pod in place. A save of the whole routes would lose pods to
// the budget (exact restore coordinates proposal, EC-D7 as amended).
func TestExportStateRouteBudget(t *testing.T) {
	t.Parallel()
	network := geometryNetwork(Point{}, trimLanes, 3)
	base := restoreGeometry(t, network, len(trimLanes), 3, 200, 60)
	state := base.ExportState()
	for index, at := range []float64{250, 200, 150} {
		state = withPod(state, index, func(pod *SavedPod) {
			pod.RouteIndex, pod.LaneID, pod.LaneDistance, pod.Distance = 3, "l3", at, 722+at
		})
	}
	live, result := restoreMotion(t, network, base.initial, state)
	if !cleanRestore(result) {
		t.Fatalf("result %+v", result)
	}
	laneBlocks, budget := physicalBlockBudget(live)
	whole, trimmed := live.exportState(math.MaxInt), live.exportTrimmed()
	wholeBlocks, trimmedBlocks := live.savedRouteBlocks(whole, laneBlocks), live.savedRouteBlocks(trimmed, laneBlocks)
	checkWholeRouteBlocks(t, live)
	if trimmedBlocks >= wholeBlocks || wholeBlocks > budget || !reflect.DeepEqual(live.ExportState(), whole) {
		t.Fatalf("the whole routes have %d blocks and the trimmed routes %d, with the budget %d", wholeBlocks, trimmedBlocks, budget)
	}
	for _, pod := range trimmed.Pods {
		if !pod.RouteTrimmed || len(pod.Route) != 1 {
			t.Fatalf("pod %s saves %d lanes with the marker %t, want the last lane with the marker", pod.ID, len(pod.Route), pod.RouteTrimmed)
		}
	}
	if got := live.exportState(wholeBlocks); !reflect.DeepEqual(got, whole) {
		t.Fatalf("the save at a budget of %d blocks is not the whole save", wholeBlocks)
	}
	over := live.exportState(wholeBlocks - 1)
	if !reflect.DeepEqual(over, trimmed) {
		t.Fatalf("the save at a budget of %d blocks is not the trimmed save", wholeBlocks-1)
	}
	input := RestoreStateInput{Network: network, Fleet: base.initial, State: roundTripState(t, over), blockBudget: wholeBlocks - 1}
	restored, result, err := RestoreState(input)
	if err != nil || !cleanRestore(result) || len(result.AtRest) != 0 || result.EarlierRestore {
		t.Fatalf("the trimmed save restores %+v, %v, want each pod in place", result, err)
	}
	checkRestoredMatches(t, live, restored)
	input.State = whole
	built := countRouteBuilds(&input)
	_, result, err = RestoreState(input)
	if err != nil || result.OverBudget == 0 || len(result.Demoted) == 0 {
		t.Fatalf("the whole save restores %+v, %v, want pods over the budget", result, err)
	}
	// The physical restores, the physical tier and the restore before the
	// saved motion, build no route of a pod that limitWork demotes, and
	// build the route of each other traveling pod. The save has no debris,
	// so the scratch fleet of checkSavedFaultFootprints, which the count
	// does not cover, builds no route.
	for index, pod := range whole.Pods {
		if demoted := slices.Contains(result.Demoted, pod.ID); demoted != (built[index] == 0) || pod.Activity != activityCode(Traveling) {
			t.Fatalf("pod %s, demoted %t, has %d route builds in the physical restores", pod.ID, demoted, built[index])
		}
	}
}

// countRouteBuilds sets input to count the route builds of each saved pod
// in the physical restores (see buildRoute), and returns the counts. It
// does not count the builds of the scratch fleet of
// checkSavedFaultFootprints.
func countRouteBuilds(input *RestoreStateInput) map[int]int {
	built := make(map[int]int)
	input.routeBuilt = func(index int) { built[index]++ }
	return built
}

// savedStart returns the trimmed start of the saved route of the traveling
// pod v (see exportPlan.trim).
func (s *Simulation) savedStart(v *vehicle) int {
	plan := exportPlan{s: s, trims: make([]routePlan, len(s.vehicles)), trimmed: make([]bool, len(s.vehicles))}
	return plan.trim(s.vehicleIndexes[v.Pod.ID]).start
}

// beforeMotion runs the restore before the saved motion on input, with the
// projected draining flags of the first physical restore.
func beforeMotion(input RestoreStateInput, newFleet func() (*Simulation, error)) (*Simulation, RestoreResult, error) {
	r, _, err := newRestoreEngine(input, newFleet, nil)
	if err != nil {
		return nil, RestoreResult{}, err
	}
	return restoreBeforeMotion(input, newFleet, r.projected)
}
