package sim

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
)

// monitorReassign makes s check the berth and track owners after each tick
// and each command, and the contract at the observations that a
// contractSampler selects. The test fails at the first break.
func monitorReassign(t *testing.T, s *Simulation) {
	t.Helper()
	sampler := &contractSampler{every: contractCheckTicks}
	s.monitor = func(s *Simulation) {
		t.Helper()
		sampler.check(t, s)
		checkIncrementalOwners(t, s)
	}
	s.observe()
}

// newReassignSimulation returns a simulation of network with the pods of
// placements, the party limit, the sharing mode, the join policy, and the
// experiment records on. The simulation checks the contract and the owners.
func newReassignSimulation(t *testing.T, network Network, placements []Placement, limit int, mode SharedRideMode, join SharedRideJoin) *Simulation {
	t.Helper()
	s := newScreenSimulation(t, network, placements, limit, mode)
	monitorReassign(t, s)
	if err := s.SetSharedRideJoin(join); err != nil {
		t.Fatal(err)
	}
	return s
}

// parkedPair places pods 01 and 02 in Parking.
var parkedPair = []Placement{
	{ID: "01", StationID: "parking", BerthID: "parking-1"},
	{ID: "02", StationID: "parking", BerthID: "parking-2"},
}

// startReassign sends pods 01 and 02 from Parking to harbor, for a party to
// market and a second party to the station to, in this order. It steps s
// until pod 01 boards the first party.
func startReassign(t *testing.T, s *Simulation, to string) {
	t.Helper()
	for _, destination := range []string{"market", to} {
		if err := submitSharedTrip(s, "harbor", destination); err != nil {
			t.Fatal(err)
		}
	}
	if s.waiting[1].request.PodID != "02" {
		t.Fatalf("the second party does not have pod 02: %+v", s.waiting)
	}
	host := s.findVehicle("01")
	stepUntil(t, s, "pod 01 boards", func() bool { return host.Pod.Activity == Boarding })
}

// checkReassigned checks that the second party of startReassign joined pod
// 01, and that dispatch released pod 02.
func checkReassigned(t *testing.T, s *Simulation) {
	t.Helper()
	host, own := s.findVehicle("01"), s.findVehicle("02")
	waiting := slices.ContainsFunc(s.waiting, func(trip waitingTrip) bool { return trip.request.ID == 2 })
	if waiting || len(host.Riders) != 2 || host.Riders[1].ID != 2 || host.Riders[1].PodID != "01" {
		t.Fatalf("the second party did not join pod 01: waiting %+v, pod %+v", s.waiting, host.Vehicle)
	}
	if !own.released || s.assigned("02") || s.pass.assigned["02"] {
		t.Fatalf("pod 02 is not released: released %v, assigned %v", own.released, s.assigned("02"))
	}
	if screen := s.SeatScreen(); screen.ReassignedParties != 1 || s.sharedParties != 1 {
		t.Fatalf("reassigned %d, shared %d, want 1 and 1", screen.ReassignedParties, s.sharedParties)
	}
	timings := s.RequestTimings()
	if len(timings) != 2 || timings[0].Reassigned || !timings[1].Reassigned || timings[1].SharedWith != 1 {
		t.Fatalf("timings %+v", timings)
	}
}

// TestReassignJoinsExistingStops sends a pod to a party at harbor that
// another pod boards for market. With the join policy
// SharedRideJoinReassignExisting, the party joins the boarding pod only
// when the pod already stops at its destination. The party for garden
// needs a new stop, so it keeps its pod.
func TestReassignJoinsExistingStops(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		mode   SharedRideMode
		join   SharedRideJoin
		to     string
		joins  bool
		census [2]int
	}{
		{name: "unassigned", mode: SharedRideDropOffs, join: SharedRideJoinUnassigned, to: "market", census: [2]int{1, 1}},
		{name: "existing stop", mode: SharedRideDropOffs, join: SharedRideJoinReassignExisting, to: "market", joins: true, census: [2]int{1, 1}},
		{name: "added stop", mode: SharedRideDropOffs, join: SharedRideJoinReassignExisting, to: "garden", census: [2]int{1, 0}},
		{name: "same destination", mode: SharedRideDestination, join: SharedRideJoinReassignExisting, to: "market", joins: true, census: [2]int{1, 1}},
		{name: "other destination", mode: SharedRideDestination, join: SharedRideJoinReassignExisting, to: "garden"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s := newReassignSimulation(t, Example(), parkedPair, 4, test.mode, test.join)
			startReassign(t, s, test.to)
			// The census runs before the join, so it also counts a party that
			// joins.
			checkJoinCensus(t, s, test.census)
			host, own := s.findVehicle("01"), s.findVehicle("02")
			if test.joins {
				checkReassigned(t, s)
				// Pod 02 is in its committed inlet behind pod 01, so it keeps
				// its route to harbor-1.
				if own.destination.ID != "harbor-1" || own.Pod.LaneID != "harbor-in" {
					t.Fatalf("pod 02 is not in the harbor-1 inlet: %+v", own.Vehicle)
				}
			} else {
				checkKeepsPod(t, s)
				advanceUntilDeparted(t, s)
				if len(s.waiting) != 1 || s.waiting[0].request.PodID != "02" || len(host.Riders) != 1 {
					t.Fatalf("the second party did not keep pod 02: %+v", s.waiting)
				}
				if screen := s.SeatScreen(); screen.ReassignedParties != 0 {
					t.Fatalf("reassigned %d, want 0", screen.ReassignedParties)
				}
			}
			stepUntil(t, s, "both parties arrive", func() bool { return s.completed == 2 })
			if len(s.waiting) != 0 || own.Pod.Activity != Idle || own.released {
				t.Fatalf("pod 02 is not idle after the run: %+v, released %v", own.Vehicle, own.released)
			}
			checkJoinCensus(t, s, test.census)
		})
	}
}

// checkKeepsPod runs a dispatch pass, and fails the test when the pass
// changes the pod, the route or the berth of the second waiting party, or
// the stops or the route of pod 01.
func checkKeepsPod(t *testing.T, s *Simulation) {
	t.Helper()
	host := s.findVehicle("01")
	trip := s.waiting[len(s.waiting)-1]
	stops, route := slices.Clone(host.Stops), slices.Clone(host.Route)
	s.dispatch()
	after := s.waiting[len(s.waiting)-1]
	if after.request.ID != trip.request.ID || after.request.PodID != trip.request.PodID ||
		!slices.Equal(after.route, trip.route) || after.destination != trip.destination {
		t.Fatalf("the waiting party changed from %+v to %+v", trip, after)
	}
	if !slices.Equal(host.Stops, stops) || !slices.Equal(host.Route, route) {
		t.Fatalf("pod 01 changed its stops from %v to %v", stops, host.Stops)
	}
	if s.findVehicle(trip.request.PodID).released {
		t.Fatalf("dispatch released pod %s", trip.request.PodID)
	}
}

// TestReassignRefusedByFullPod sends three pods to three parties for
// market at harbor, with a limit of 2. The second party joins pod 01 and
// releases pod 02. The third party then finds pod 01 full, so it keeps pod
// 03, and the seat screen counts a refusal.
func TestReassignRefusedByFullPod(t *testing.T) {
	t.Parallel()
	s := newReassignSimulation(t, Example(), append(slices.Clone(parkedPair), Placement{ID: "03", StationID: "garden", BerthID: "garden-1"}),
		2, SharedRideDropOffs, SharedRideJoinReassignExisting)
	for range 3 {
		if err := submitSharedTrip(s, "harbor", "market"); err != nil {
			t.Fatal(err)
		}
	}
	host := s.findVehicle("01")
	stepUntil(t, s, "pod 01 boards", func() bool { return host.Pod.Activity == Boarding })
	checkReassigned(t, s)
	if len(s.waiting) != 1 || s.waiting[0].request.PodID != "03" {
		t.Fatalf("the third party does not have pod 03: %+v", s.waiting)
	}
	checkKeepsPod(t, s)
	if screen := s.SeatScreen(); screen.FullPodRefusals != 1 || screen.ReassignedParties != 1 {
		t.Fatalf("refusals %d, reassigned %d, want 1 and 1", screen.FullPodRefusals, screen.ReassignedParties)
	}
	stepUntil(t, s, "each party arrives", func() bool { return s.completed == 3 })
}

// reassignRestoreFixture restores pod 01 as it boards a party for market
// at harbor-1. The waiting trips and the other pods come from the caller.
// The party limit is 4, the join policy is SharedRideJoinReassignExisting,
// and the experiment records are on.
func reassignRestoreFixture(t *testing.T, f restoreFixture, trips []SavedTrip, pods ...SavedPod) *Simulation {
	t.Helper()
	host := f.boarding(t, "01", "harbor-1")
	host.PhaseTicks = boardingTicks
	state := f.state(append([]SavedPod{host}, pods...)...)
	for _, trip := range trips {
		state.RequestID = max(state.RequestID, trip.Request.ID)
	}
	state.Waiting = trips
	state.SharedRidePartyLimit, state.SharedRideJoin = 4, SharedRideJoinReassignExisting
	s, result, err := f.restore(roundTripState(t, state))
	if err != nil || !cleanRestore(result) {
		t.Fatalf("restore: %v, %+v", err, result)
	}
	monitorReassign(t, s)
	s.SetExperimentRecords(true)
	return s
}

// harborFleet places pods 01 and 02 at harbor, and pods 03 and 04 in
// Parking.
var harborFleet = []Placement{
	{ID: "01", StationID: "harbor", BerthID: "harbor-1"},
	{ID: "02", StationID: "harbor", BerthID: "harbor-2"},
	{ID: "03", StationID: "parking", BerthID: "parking-1"},
	{ID: "04", StationID: "parking", BerthID: "parking-2"},
}

// TestReassignSkipsIdlePods restores pod 01 as it boards a party for market
// at harbor-1, and pod 02 idle at harbor-2. A party for market with pod 02
// boards pod 02 and does not join pod 01. With promotion, the older party
// has pod 03 on its way, and promotion gives it pod 02. It boards pod 02.
// The newer party then has pod 03 on its way, so it joins pod 01, and
// dispatch releases pod 03.
func TestReassignSkipsIdlePods(t *testing.T) {
	t.Parallel()
	for _, promote := range []bool{false, true} {
		t.Run(fmt.Sprintf("promote %v", promote), func(t *testing.T) {
			t.Parallel()
			f := newRestoreFleetFixture(t, harborTwoBerths(), harborFleet)
			trips := []SavedTrip{{Request: SavedRequest{SharingConsent: SharedConsent, Service: OnDemandService, ID: 2, From: "harbor", To: "market", PartySize: 1, PodID: "02", RequestedTick: restoreTick - 10}}}
			var pods []SavedPod
			if promote {
				trips = []SavedTrip{
					{Request: SavedRequest{SharingConsent: SharedConsent, Service: OnDemandService, ID: 2, From: "harbor", To: "market", PartySize: 1, PodID: "03", RequestedTick: restoreTick - 20}},
					{Request: SavedRequest{SharingConsent: SharedConsent, Service: OnDemandService, ID: 3, From: "harbor", To: "market", PartySize: 1, PodID: "02", RequestedTick: restoreTick - 10}},
				}
				pods = append(pods, relocating(f.traveling(t, travelInput{id: "03", from: "parking-1", to: "harbor-2", lane: "return", distance: 20})))
			}
			s := reassignRestoreFixture(t, f, trips, pods...)
			s.Step()
			own := s.findVehicle("02")
			if len(s.waiting) != 0 || own.Pod.Activity != Boarding || own.Riders[0].ID != 2 {
				t.Fatalf("the oldest party did not board pod 02: waiting %+v, pod 02 %+v", s.waiting, own.Vehicle)
			}
			host := s.findVehicle("01")
			timings := s.RequestTimings()
			if timings[0].RequestID != 2 || timings[0].SharedWith != 0 || timings[0].Reassigned {
				t.Fatalf("timings %+v", timings)
			}
			if promote {
				released := s.findVehicle("03")
				if len(host.Riders) != 2 || host.Riders[1].ID != 3 || !released.released || s.assigned("03") ||
					len(timings) != 2 || !timings[1].Reassigned {
					t.Fatalf("the newer party did not join pod 01: pod 01 %+v, pod 03 %+v, timings %+v", host.Vehicle, released.Vehicle, timings)
				}
			} else if len(host.Riders) != 1 || len(timings) != 1 {
				t.Fatalf("pod 01 took the party: %+v", host.Vehicle)
			}
			stepUntil(t, s, "each party arrives", func() bool { return s.completed == s.requestID })
		})
	}
}

// TestReassignedPodTakesLaterTrip releases pod 02 on its way to harbor, and
// a later party at garden that has no pod takes pod 02 in the same
// dispatch pass. No other pod is free.
func TestReassignedPodTakesLaterTrip(t *testing.T) {
	t.Parallel()
	f := newRestoreFleetFixture(t, harborTwoBerths(), []Placement{
		{ID: "01", StationID: "harbor", BerthID: "harbor-1"},
		{ID: "02", StationID: "parking", BerthID: "parking-1"},
	})
	s := reassignRestoreFixture(t, f, []SavedTrip{
		{Request: SavedRequest{SharingConsent: SharedConsent, Service: OnDemandService, ID: 2, From: "harbor", To: "market", PartySize: 1, PodID: "02", RequestedTick: restoreTick - 20}},
		{Request: SavedRequest{SharingConsent: SharedConsent, Service: OnDemandService, ID: 3, From: "garden", To: "market", PartySize: 1, RequestedTick: restoreTick - 10}},
	}, relocating(f.traveling(t, travelInput{id: "02", from: "parking-1", to: "harbor-2", lane: "return", distance: 20})))
	s.Step()
	own := s.findVehicle("02")
	if len(s.waiting) != 1 || s.waiting[0].request.ID != 3 || s.waiting[0].request.PodID != "02" ||
		own.RelocatingTo != "garden" || own.released {
		t.Fatalf("the party at garden did not take pod 02: waiting %+v, pod 02 %+v", s.waiting, own.Vehicle)
	}
	if screen := s.SeatScreen(); screen.ReassignedParties != 1 {
		t.Fatalf("reassigned %d, want 1", screen.ReassignedParties)
	}
	stepUntil(t, s, "each party arrives", func() bool { return s.completed == s.requestID })
}

// TestReassignedPodRoutes releases pod 02 on its way to harbor. A pod that
// holds its claim on harbor-2 and a pod in its committed inlet keep their
// routes. A pod on its way to harbor-1, which pod 01 holds, goes to the
// nearest free berth after the pass. This is harbor-2, and the pod claims
// it. Each pod becomes idle at harbor-2.
func TestReassignedPodRoutes(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		to, lane string
		distance float64
		claims   bool
		// committed is true when the pod cannot divert.
		committed bool
	}{
		{name: "no claim", to: "harbor-1", lane: "return", distance: 20},
		{name: "claim", to: "harbor-2", lane: "return", distance: 20, claims: true},
		{name: "committed inlet", to: "harbor-2", lane: "harbor-in-2", distance: 5, committed: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			f := newRestoreFleetFixture(t, harborTwoBerths(), []Placement{
				{ID: "01", StationID: "harbor", BerthID: "harbor-1"},
				{ID: "02", StationID: "parking", BerthID: "parking-1"},
			})
			own := relocating(f.traveling(t, travelInput{id: "02", from: "parking-1", to: test.to, lane: test.lane, distance: test.distance}))
			own.ClaimsDestination = test.claims
			s := reassignRestoreFixture(t, f, []SavedTrip{
				{Request: SavedRequest{SharingConsent: SharedConsent, Service: OnDemandService, ID: 2, From: "harbor", To: "market", PartySize: 1, PodID: "02", RequestedTick: restoreTick - 20}},
			}, own)
			v := s.findVehicle("02")
			if _, _, ok := s.divertStart(v); ok == test.committed {
				t.Fatalf("divertStart reports %v, want %v", ok, !test.committed)
			}
			claim := resource{kind: berthResource, id: "harbor-2"}
			if held := s.owners[claim] == podResourceOwner("02"); held != test.claims {
				t.Fatalf("pod 02 holds harbor-2: %v", held)
			}
			route := slices.Clone(v.Route)
			s.Step()
			if len(s.waiting) != 0 || !v.released {
				t.Fatalf("the party did not join pod 01: waiting %+v, pod 02 %+v", s.waiting, v.Vehicle)
			}
			if !test.committed {
				checkReleasedTo(t, s, v, "harbor-2")
			}
			if (test.claims || test.committed) && !slices.Equal(v.Route, route) {
				t.Fatalf("pod 02 changed its route from %v to %v", route, v.Route)
			}
			stepUntil(t, s, "pod 02 arrives", func() bool { return v.Pod.Activity == Idle })
			if v.Pod.BerthID != "harbor-2" || v.released {
				t.Fatalf("pod 02 stopped at %s, released %v", v.Pod.BerthID, v.released)
			}
			stepUntil(t, s, "the parties arrive", func() bool { return s.completed == s.requestID })
		})
	}
}

// TestReassignChangesNoDecisionWithRecords runs the demand of
// TestJoinCensusChangesNoDecision with the join policy
// SharedRideJoinReassignExisting, with the experiment records on and off.
// Parties with a pod on their way must join. The census runs before the
// join, so each reassigned party is also an existing-stop party of the
// census.
func TestReassignChangesNoDecisionWithRecords(t *testing.T) {
	t.Parallel()
	placements := append(slices.Clone(parkedPair), Placement{ID: "03", StationID: "garden", BerthID: "garden-1"})
	burst := [][2]string{{"harbor", "market"}, {"harbor", "garden"}, {"harbor", "market"}, {"garden", "market"}}
	screen := runRecordsOnAndOff(t, placements, 4, SharedRideJoinReassignExisting, burst)
	if screen.ReassignedParties == 0 || screen.ReassignedParties > screen.JoinEligibleExistingStop {
		t.Fatalf("reassigned %d parties with %d existing-stop parties in the census", screen.ReassignedParties, screen.JoinEligibleExistingStop)
	}
}

// TestReassignWithoutSharing runs demand at a party limit of 1 with each
// join policy. The snapshots must be equal at each simulated second.
func TestReassignWithoutSharing(t *testing.T) {
	t.Parallel()
	placements := append(slices.Clone(parkedPair), Placement{ID: "03", StationID: "garden", BerthID: "garden-1"})
	unassigned := newReassignSimulation(t, Example(), placements, 1, SharedRideDropOffs, SharedRideJoinUnassigned)
	reassign := newReassignSimulation(t, Example(), placements, 1, SharedRideDropOffs, SharedRideJoinReassignExisting)
	burst := [][2]string{{"harbor", "market"}, {"harbor", "market"}, {"harbor", "market"}, {"garden", "market"}}
	for tick := range 450 * TicksPerSecond {
		if tick%(30*TicksPerSecond) == 0 && tick < 300*TicksPerSecond {
			for _, trip := range burst {
				for _, s := range []*Simulation{unassigned, reassign} {
					if err := submitSharedTrip(s, trip[0], trip[1]); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
		unassigned.Step()
		reassign.Step()
		if (tick+1)%TicksPerSecond != 0 {
			continue
		}
		want, err := json.Marshal(unassigned.Snapshot())
		if err != nil {
			t.Fatal(err)
		}
		got, err := json.Marshal(reassign.Snapshot())
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("tick %d: the snapshots differ", tick+1)
		}
	}
	if unassigned.completed == 0 {
		t.Fatal("no party arrived")
	}
}

// TestReassignRestoreAndContinue saves a state just after a party joins pod
// 01 and dispatch releases pod 02. Each restore tier keeps the join policy
// and the orders, and the restored simulation takes each party to its
// destination once. The physical tier keeps the released pod.
func TestReassignRestoreAndContinue(t *testing.T) {
	t.Parallel()
	for _, tier := range restoreTiers {
		t.Run(string(tier), func(t *testing.T) {
			t.Parallel()
			s := newReassignSimulation(t, Example(), parkedPair, 4, SharedRideDropOffs, SharedRideJoinReassignExisting)
			startReassign(t, s, "market")
			checkReassigned(t, s)
			state := roundTripState(t, s.ExportState())
			if state.SharedRideJoin != SharedRideJoinReassignExisting {
				t.Fatalf("the saved join policy is %q", state.SharedRideJoin)
			}
			restored, result, err := RestoreState(RestoreStateInput{
				Network: Example(), Fleet: parkedPair, State: state, LogicalOnly: tier == RestoreLogical,
			})
			if err != nil || result.Tier != tier || len(result.Dropped) > 0 {
				t.Fatalf("restore: %v, %+v", err, result)
			}
			if restored.sharedRideJoin != SharedRideJoinReassignExisting {
				t.Fatalf("the restored join policy is %q", restored.sharedRideJoin)
			}
			if v := restored.findVehicle("02"); tier == RestorePhysical && (!releasable(v) || !v.released) {
				t.Fatalf("the restore did not keep the released pod: %+v, released %v", v.Vehicle, v.released)
			}
			monitorReassign(t, restored)
			restored.SetExperimentRecords(true)
			stepUntil(t, restored, "each party arrives", func() bool { return restored.completed == restored.requestID })
			advance(restored, 60*TicksPerSecond)
			if restored.completed != 2 || len(restored.waiting) > 0 || countUnaccounted(t, restored) != 0 {
				t.Fatalf("completed %d of 2 orders, %d waiting", restored.completed, len(restored.waiting))
			}
			if restored.sharedParties != 1 || restored.SeatScreen().ReassignedParties != 0 {
				t.Fatalf("shared %d, reassigned %d after the restore", restored.sharedParties, restored.SeatScreen().ReassignedParties)
			}
		})
	}
}

// TestReassignRequeuedRider restores a new party for market at harbor and
// a rider for market that the restore queued again. Pods 01 and 02 go to
// the two parties, and the rider joins pod 01 when it boards the new
// party. The rider keeps its recorded wait and its ticks, and the counters
// of new orders do not count it.
func TestReassignRequeuedRider(t *testing.T) {
	t.Parallel()
	f := newRestoreFleetFixture(t, Example(), parkedPair)
	state := f.state()
	state.RequestID, state.Boarded, state.TotalWaitTicks, state.MaxWaitTicks = 2, 1, 10, 10
	state.Waiting = []SavedTrip{
		{Request: SavedRequest{SharingConsent: SharedConsent, Service: OnDemandService, ID: 1, From: "harbor", To: "market", PartySize: 1, RequestedTick: restoreTick - 5}},
		{Request: SavedRequest{SharingConsent: SharedConsent, Service: OnDemandService, ID: 2, From: "harbor", To: "market", PartySize: 1, RequestedTick: 10, BoardedTick: 20}, Boarded: true},
	}
	state.SharedRidePartyLimit, state.SharedRideJoin = 4, SharedRideJoinReassignExisting
	s, result, err := f.restore(roundTripState(t, state))
	if err != nil || !cleanRestore(result) {
		t.Fatalf("restore: %v, %+v", err, result)
	}
	monitorReassign(t, s)
	s.SetExperimentRecords(true)
	s.Step()
	if len(s.waiting) != 2 || s.waiting[1].request.PodID != "02" || !s.waiting[1].boarded {
		t.Fatalf("the requeued rider does not have pod 02: %+v", s.waiting)
	}
	host := s.findVehicle("01")
	stepUntil(t, s, "pod 01 boards", func() bool { return host.Pod.Activity == Boarding })
	if len(s.waiting) != 0 || len(host.Riders) != 2 || !s.findVehicle("02").released {
		t.Fatalf("the requeued rider did not join pod 01: waiting %+v, pod 01 %+v", s.waiting, host.Vehicle)
	}
	rider := host.Riders[1]
	if rider.ID != 2 || rider.RequestedTick != 10 || rider.BoardedTick != 20 {
		t.Fatalf("the requeued rider changed: %+v", rider)
	}
	wait := host.Riders[0].BoardedTick - host.Riders[0].RequestedTick
	if s.boarded != 2 || s.totalWaitTicks != 10+wait || s.maxWaitTicks != max(10, wait) {
		t.Fatalf("boarded %d, total wait %d, maximum wait %d", s.boarded, s.totalWaitTicks, s.maxWaitTicks)
	}
	if timings := s.RequestTimings(); len(timings) != 1 || timings[0].RequestID != 1 {
		t.Fatalf("timings %+v", timings)
	}
	if s.sharedParties != 0 || s.SeatScreen().ReassignedParties != 0 {
		t.Fatalf("shared %d, reassigned %d", s.sharedParties, s.SeatScreen().ReassignedParties)
	}
	stepUntil(t, s, "each party arrives", func() bool { return s.completed == 2 })
}

// TestSharedRideJoinSetting checks the setter, Reset, and the restore of
// the join policy in each tier.
func TestSharedRideJoinSetting(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(Example(), parkedPair)
	if err != nil {
		t.Fatal(err)
	}
	if s.sharedRideJoin != DefaultSharedRideJoin || DefaultSharedRideJoin != SharedRideJoinUnassigned {
		t.Fatalf("a new simulation has the join policy %q", s.sharedRideJoin)
	}
	for _, join := range []SharedRideJoin{"", "reassign", "Unassigned", "reassign-existing "} {
		if err := s.SetSharedRideJoin(join); err == nil || s.sharedRideJoin != DefaultSharedRideJoin {
			t.Fatalf("the setter accepted %q: policy %q", join, s.sharedRideJoin)
		}
	}
	if err := s.SetSharedRideJoin(SharedRideJoinReassignExisting); err != nil {
		t.Fatal(err)
	}
	s.Reset()
	if s.sharedRideJoin != SharedRideJoinReassignExisting {
		t.Fatalf("Reset changed the join policy to %q", s.sharedRideJoin)
	}
	f := newRestoreFleetFixture(t, Example(), parkedPair)
	for _, tier := range restoreTiers {
		for _, test := range []struct {
			saved, want SharedRideJoin
		}{
			{saved: "", want: SharedRideJoinUnassigned},
			{saved: SharedRideJoinUnassigned, want: SharedRideJoinUnassigned},
			{saved: SharedRideJoinReassignExisting, want: SharedRideJoinReassignExisting},
			{saved: "reassign"},
		} {
			state := f.state()
			state.SharedRideJoin = test.saved
			restored, _, err := f.restoreTier(roundTripState(t, state), tier)
			if test.want == "" {
				if err == nil {
					t.Fatalf("%s: the restore accepted the join policy %q", tier, test.saved)
				}
				continue
			}
			if err != nil {
				t.Fatalf("%s: %v", tier, err)
			}
			if restored.sharedRideJoin != test.want || restored.ExportState().SharedRideJoin != test.want {
				t.Fatalf("%s: saved %q restores as %q, want %q", tier, test.saved, restored.sharedRideJoin, test.want)
			}
		}
	}
}

// loopCorridor returns the merge corridor with lanes from the destination
// station to the origin station and from the origin station to the start
// of the main lane. A pod that boards at the destination station can then
// go to the origin station.
func loopCorridor() Network {
	network := mergeCorridor(false, 0)
	network.Lanes = append(network.Lanes,
		Lane{ID: "loop-back", From: "dest-exit", To: "origin-entry", SpeedLimit: 14},
		Lane{ID: "loop-out", From: "origin-exit", To: "main-start", SpeedLimit: 14},
	)
	return network
}

// TestReassignReleasesPlatoon restores pods p01 and p02 empty on the main
// lane of loopCorridor, on their way to parties for origin at dest. Pod p03
// boards a party for origin at dest. At the first step, p02 couples to
// p01. Then both parties join p03, and dispatch releases p01, the platoon
// leader, and p02, the follower. The pods cannot divert, so they keep their
// routes and the platoon stays valid until they stop at dest.
func TestReassignReleasesPlatoon(t *testing.T) {
	t.Parallel()
	network := loopCorridor()
	laneIndex := make(map[string]int, len(network.Lanes))
	for index, lane := range network.Lanes {
		laneIndex[lane.ID] = index
	}
	indexes := func(lanes ...string) []int {
		var route []int
		for _, lane := range lanes {
			route = append(route, laneIndex[lane])
		}
		return route
	}
	var fleet []Placement
	state := SavedState{Tick: restoreTick, SharedRidePartyLimit: 4, RequestID: 3, Boarded: 1}
	for index := range 3 {
		id := fmt.Sprintf("p%02d", index+1)
		berth := fmt.Sprintf("dest-%02d", index+1)
		fleet = append(fleet, Placement{ID: id, StationID: "dest", BerthID: berth})
		if index == 2 {
			state.Pods = append(state.Pods, SavedPod{
				ID: id, Activity: activityCode(Boarding), StationID: "dest", BerthID: berth, PhaseTicks: boardingTicks,
				Riders: []SavedRequest{{SharingConsent: SharedConsent, Service: OnDemandService, ID: 1, From: "dest", To: "origin", PartySize: 1, PodID: id, RequestedTick: 10, BoardedTick: 20}},
				Stops:  []string{"origin"}, Origin: berth, DestinationStation: "origin",
				Route: indexes("dest-berth-03-out", "loop-back"),
			})
			continue
		}
		distance := 2000 - corridorQueueGap*float64(index)
		state.Pods = append(state.Pods, SavedPod{
			ID: id, Activity: activityCode(Traveling), RelocatingTo: "dest", Origin: berth, Destination: berth, DestinationStation: "dest",
			Route: indexes("main", "exit", "approach", fmt.Sprintf("dest-berth-%02d-in", index+1)), LaneID: "main",
			LaneDistance: distance, Distance: distance,
		})
		state.Waiting = append(state.Waiting, SavedTrip{Request: SavedRequest{
			SharingConsent: SharedConsent, Service: OnDemandService, ID: index + 2, From: "dest", To: "origin", PartySize: 1, PodID: id, RequestedTick: restoreTick - 10,
		}})
	}
	s, result, err := RestoreState(RestoreStateInput{Network: network, Fleet: fleet, State: roundTripState(t, state)})
	if err != nil || !cleanRestore(result) {
		t.Fatalf("restore: %v, %+v", err, result)
	}
	monitorReassign(t, s)
	if err := s.SetPlatoonLimit(2); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPlatooning(PlatooningVirtual); err != nil {
		t.Fatal(err)
	}
	s.SetExperimentRecords(true)
	monitor := newPlatoonMonitor(s)
	s.Step()
	monitor.check(t)
	leader, follower, host := s.findVehicle("p01"), s.findVehicle("p02"), s.findVehicle("p03")
	if len(s.waiting) != 2 || follower.link.leader != 1 || leader.follower != 2 {
		t.Fatalf("p02 did not couple to p01 before the join: waiting %+v, link %+v", s.waiting, follower.link)
	}
	routes := [][]Lane{slices.Clone(leader.Route), slices.Clone(follower.Route)}
	if err := s.SetSharedRideJoin(SharedRideJoinReassignExisting); err != nil {
		t.Fatal(err)
	}
	s.Step()
	monitor.check(t)
	if len(s.waiting) != 0 || len(host.Riders) != 3 || s.SeatScreen().ReassignedParties != 2 {
		t.Fatalf("the parties did not join p03: waiting %+v, p03 %+v", s.waiting, host.Vehicle)
	}
	for index, v := range []*vehicle{leader, follower} {
		if !v.released || !slices.Equal(v.Route, routes[index]) {
			t.Fatalf("pod %s changed its route or is not released: %+v, released %v", v.Pod.ID, v.Vehicle, v.released)
		}
	}
	if follower.link.leader != 1 {
		t.Fatal("the release ended the platoon")
	}
	for range 1200 * TicksPerSecond {
		if leader.Pod.Activity == Idle && follower.Pod.Activity == Idle {
			break
		}
		s.Step()
		checkTraffic(t, s.Snapshot())
		monitor.check(t)
	}
	if leader.Pod.BerthID != "dest-01" || follower.Pod.BerthID != "dest-02" || leader.released || follower.released {
		t.Fatalf("the released pods did not stop at their berths: %+v, %+v", leader.Vehicle, follower.Vehicle)
	}
}
