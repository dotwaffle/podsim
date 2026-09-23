package sim

import (
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// logicalState returns a saved state of the traffic demo that the physical
// tier accepts:
//   - Pod 01 boards request 8 at Garden.
//   - Pod 02 travels with a shared ride of three parties, request 4.
//   - The demo pod 03 unloads the two parties of request 2 at Market.
//   - The demo pod 04 travels empty to a pickup at Harbor with its completed
//     request 1.
//   - The queue holds request 9, which pod 04 picks up, and request 10 from
//     the parking station, which is not valid.
//
// Orders 3, 5 and 6 joined shared rides, and order 7 is complete.
func logicalState(t *testing.T, f restoreFixture) SavedState {
	t.Helper()
	boarding := f.boarding(t, "01", "garden-1")
	boarding.Request.ID, boarding.Request.RequestedTick = 8, 400
	shared := f.carrying(t, f.traveling(t, travelInput{id: "02", from: "garden-1", to: "market-1", lane: "garden-merge", distance: 60}), 4, 3)
	unloading := SavedPod{
		ID: "03", Activity: activityCode(Unloading), StationID: "market", BerthID: "market-1", Occupied: true,
		Request: &SavedRequest{ID: 2, From: "harbor", To: "market", PartySize: 2, PodID: "03", RequestedTick: 100},
		Parties: 2, PhaseTicks: unloadingTicks / 2, Origin: "harbor-1", Destination: "market-1", DestinationStation: "market",
	}
	pickup := relocating(f.traveling(t, travelInput{id: "04", from: "garden-1", to: "harbor-1", lane: "return-to-parking", distance: 100}))
	pickup.Request = &SavedRequest{ID: 1, From: "harbor", To: "garden", PartySize: 1, PodID: "04", Completed: true, RequestedTick: 10}
	route, err := f.s.stationApproachRoute("harbor-berth", "market")
	if err != nil {
		t.Fatal(err)
	}
	state := f.state(boarding, shared)
	state.Pods = append(state.Pods, unloading, pickup)
	state.Demo = &SavedDemo{SecondSent: true}
	state.Waiting = []SavedTrip{
		{
			Request: SavedRequest{ID: 9, From: "harbor", To: "market", PartySize: 1, PodID: "04", RequestedTick: 500},
			Route:   f.s.laneIndexes(route, len(route)), DeferUntil: restoreTick + 100, DeferCheck: restoreTick + 30, DeferPodID: "02",
		},
		{Request: SavedRequest{ID: 10, From: "parking", To: "market", PartySize: 1, RequestedTick: 550}},
	}
	state.RequestID, state.Completed, state.Boarded = 10, 2, 8
	state.SharedRidePartyLimit, state.SharedParties = 4, 3
	state.TotalWaitTicks, state.MaxWaitTicks, state.NextRedistributionTick = 2400, 700, restoreTick+60
	state.PassengerDistanceMeters, state.EmptyDistanceMeters, state.RebalanceMoves = 3200, 1400, 1
	return state
}

func TestRestoreLogical(t *testing.T) {
	t.Parallel()
	f := newRestoreFleetFixture(t, Example(), demoFleet())
	// The fixture caches routes, so build the state before the parallel
	// subtests.
	base := logicalState(t, f)
	if gap := base.ordersGap(); gap != 0 {
		t.Fatalf("the saved order gap is %d", gap)
	}
	if _, result, err := f.restore(roundTripState(t, base)); err != nil || result.Tier != RestorePhysical ||
		len(result.Demoted)+len(result.Requeued) > 0 || !slices.Equal(result.Dropped, []int{10}) {
		t.Fatalf("the physical tier does not accept the state: %v, %+v", err, result)
	}
	fresh, err := NewFleet(f.network, f.fleet)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name        string
		edit        func(*SavedState)
		logicalOnly bool
		// physical is a part of the error of the physical tier. It is empty
		// when the restore skips the physical tier.
		physical string
		// completed counts the parties that the restore completes.
		completed         int
		requeued, dropped []int
		droppedParties    int
	}{
		{
			name: "logical only", edit: func(*SavedState) {}, logicalOnly: true,
			completed: 2, requeued: []int{4, 8}, dropped: []int{10}, droppedParties: 1,
		},
		{
			name: "two pods at one berth", physical: "pods 01 and 03 are at berth garden-1",
			edit:      func(state *SavedState) { state.Pods[2].StationID, state.Pods[2].BerthID = "garden", "garden-1" },
			completed: 2, requeued: []int{4, 8}, dropped: []int{10}, droppedParties: 1,
		},
		{
			// The requeued shared ride is not valid. The restore drops it with
			// its three parties.
			name: "shared ride from a parking station", physical: "does not join two passenger stations",
			edit:      func(state *SavedState) { state.Pods[1].Request.From = "parking" },
			completed: 2, requeued: []int{8}, dropped: []int{4, 10}, droppedParties: 4,
		},
		{
			name: "unloading pod without a request", physical: "no active request",
			edit:     func(state *SavedState) { state.Pods[2].Request = nil },
			requeued: []int{4, 8}, dropped: []int{10}, droppedParties: 1,
		},
		{
			name: "demo stopped with an error", logicalOnly: true,
			edit:      func(state *SavedState) { state.Demo, state.DemoError = nil, "Traffic demo stopped: test" },
			completed: 2, requeued: []int{4, 8}, dropped: []int{10}, droppedParties: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			state := roundTripState(t, base)
			tc.edit(&state)
			s, result, err := RestoreState(RestoreStateInput{Network: f.network, Fleet: f.fleet, State: state, LogicalOnly: tc.logicalOnly})
			if err != nil {
				t.Fatal(err)
			}
			if result.Tier != RestoreLogical || len(result.Demoted) > 0 || result.OverCap+result.OverBudget > 0 ||
				(result.PhysicalError == nil) != (tc.physical == "") ||
				result.PhysicalError != nil && !strings.Contains(result.PhysicalError.Error(), tc.physical) {
				t.Fatalf("result %+v, want the physical tier error %q", result, tc.physical)
			}
			if !slices.Equal(result.Requeued, tc.requeued) || !slices.Equal(result.Dropped, tc.dropped) ||
				result.DroppedParties != tc.droppedParties {
				t.Fatalf("requeued %v, dropped %v with %d parties", result.Requeued, result.Dropped, result.DroppedParties)
			}
			// Each fleet pod waits empty at its initial berth, and the demo
			// pods are gone.
			if !reflect.DeepEqual(s.vehicles, fresh.vehicles) || !maps.Equal(s.owners, fresh.owners) {
				t.Fatalf("pods %+v", s.Snapshot().Vehicles)
			}
			want := savedCounters(state)
			want.Completed += tc.completed
			want.Demo, want.DemoError = nil, ""
			if got := savedCounters(s.ExportState()); !reflect.DeepEqual(got, want) {
				t.Fatalf("counters\n got %+v\nwant %+v", got, want)
			}
			checkLogicalQueue(t, state, s, tc.requeued)
			if _, err := s.SafetyObservation().Check(); err != nil {
				t.Fatal(err)
			}
			if !maps.Equal(s.owners, s.retainedOwners()) {
				t.Fatal("the owners differ from the retention rules")
			}
			gap := s.ordersGap()
			if want := state.ordersGap() + tc.droppedParties; gap != want {
				t.Fatalf("the order gap is %d, want %d", gap, want)
			}
			// In 10 minutes, the pods take each queued party to its
			// destination.
			for second := range 600 {
				advance(s, TicksPerSecond)
				if got := s.ordersGap(); got != gap {
					t.Fatalf("second %d: the order gap is %d, want %d", second, got, gap)
				}
			}
			if len(s.waiting) > 0 || slices.ContainsFunc(s.vehicles, func(v vehicle) bool { return v.carriesPassengers() }) ||
				s.completed != s.requestID-gap || s.boarded != state.Boarded+1 {
				t.Fatalf("after 10 minutes: %d queued, completed %d of %d, boarded %d", len(s.waiting), s.completed, s.requestID, s.boarded)
			}
		})
	}
}

// checkLogicalQueue checks the queue after a logical restore. The queue holds
// the requeued requests and the saved request 9 in ID order. No trip keeps a
// pod binding. A requeued request keeps the party count of its pod, and
// request 9 keeps its deferral deadline.
func checkLogicalQueue(t *testing.T, state SavedState, s *Simulation, requeued []int) {
	t.Helper()
	parties := make(map[int]int)
	for _, pod := range state.Pods {
		if pod.carriesPassengers() {
			parties[pod.Request.ID] = max(1, pod.Parties)
		}
	}
	ids := make([]int, len(s.waiting))
	for index, trip := range s.waiting {
		ids[index] = trip.request.ID
		if trip.request.PodID != "" || trip.route != nil || trip.deferCheck != 0 || trip.deferPodID != "" ||
			trip.parties != parties[trip.request.ID] {
			t.Fatalf("queued trip %+v", trip)
		}
	}
	if want := slices.Sorted(slices.Values(append(slices.Clone(requeued), 9))); !slices.Equal(ids, want) {
		t.Fatalf("queued requests %v, want %v", ids, want)
	}
	if trip := s.waiting[slices.Index(ids, 9)]; trip.deferUntil != restoreTick+100 {
		t.Fatalf("request 9 waits until %d", trip.deferUntil)
	}
}

// TestRestoreLogicalKeepsSharedRides saves six orders from Harbor to Market
// with a shared ride party limit of 4 while four of them share a ride. After
// a logical restore, each order completes.
func TestRestoreLogicalKeepsSharedRides(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		// activity and parties describe a pod that the saved state must
		// have.
		activity Activity
		parties  int
	}{
		{name: "four parties board", activity: Boarding, parties: 4},
		{name: "four parties travel", activity: Traveling, parties: 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			live, err := NewFleet(Example(), demoFleet())
			if err != nil {
				t.Fatal(err)
			}
			if err = live.SetSharedRidePartyLimit(4); err != nil {
				t.Fatal(err)
			}
			for range 6 {
				if err = live.RequestTrip("harbor", "market"); err != nil {
					t.Fatal(err)
				}
			}
			for !slices.ContainsFunc(live.vehicles, func(v vehicle) bool {
				return v.carriesPassengers() && v.Pod.Activity == tc.activity && v.Parties == tc.parties
			}) {
				if live.tick >= 5*60*TicksPerSecond {
					t.Fatalf("no pod is %q with %d parties", tc.activity, tc.parties)
				}
				live.Step()
			}
			state := roundTripState(t, live.ExportState())
			s, result, err := RestoreState(RestoreStateInput{Network: Example(), Fleet: demoFleet(), State: state, LogicalOnly: true})
			if err != nil {
				t.Fatal(err)
			}
			queued := 0
			for _, trip := range s.waiting {
				queued += trip.partyCount()
			}
			if len(result.Requeued) != 1 || len(result.Dropped) > 0 || queued+s.completed != 6 {
				t.Fatalf("result %+v, %d parties queued, %d completed", result, queued, s.completed)
			}
			advance(s, 30*60*TicksPerSecond)
			if s.requestID != 6 || s.completed != 6 {
				t.Fatalf("submitted %d, completed %d", s.requestID, s.completed)
			}
		})
	}
}

// TestRestoreStateReportsEachFailedTier restores counters that neither tier
// accepts.
func TestRestoreStateReportsEachFailedTier(t *testing.T) {
	t.Parallel()
	f := newRestoreFixture(t, Example())
	state := f.state()
	state.Completed = state.RequestID + 1
	const cause = "logical tier: the saved state completed or boarded more orders"
	for _, tc := range []struct {
		name        string
		logicalOnly bool
	}{
		{name: "both tiers"},
		{name: "logical only", logicalOnly: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, result, err := RestoreState(RestoreStateInput{Network: f.network, Fleet: f.fleet, State: state, LogicalOnly: tc.logicalOnly})
			if err == nil || s != nil || result.Tier != "" {
				t.Fatalf("the restore succeeded with %+v", result)
			}
			physical := strings.Contains(err.Error(), "physical tier: the saved state completed or boarded more orders")
			if !strings.Contains(err.Error(), cause) || physical == tc.logicalOnly || (result.PhysicalError != nil) == tc.logicalOnly {
				t.Fatalf("error %q with %+v", err, result)
			}
		})
	}
}
