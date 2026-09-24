package sim

import (
	"errors"
	"reflect"
	"slices"
	"testing"
)

func TestStationRequestDispatch(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, origin, destination, pod string
		placements                     []Placement
	}{
		{"local", "harbor", "garden", "01", []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "parking"}}},
		{"nearest parked", "harbor", "garden", "02", []Placement{{ID: "01", StationID: "garden"}, {ID: "02", StationID: "parking"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, err := NewFleet(Example(), tc.placements)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.RequestTrip(tc.origin, tc.destination); err != nil {
				t.Fatal(err)
			}
			expectedID := s.requestID
			pod := s.findVehicle(tc.pod)
			if tc.name == "local" {
				if pod.Pod.Activity != Boarding || len(s.waiting) != 0 {
					t.Fatal("local pod did not board immediately")
				}
			} else {
				if len(s.waiting) != 1 || s.waiting[0].request.PodID != tc.pod || pod.RelocatingTo != tc.origin {
					t.Fatalf("wrong pickup assignment: %+v", s.Snapshot())
				}
			}
			pickupSeen := false
			for range 400 * TicksPerSecond {
				s.Step()
				checkTraffic(t, s.Snapshot())
				if pod.RelocatingTo != "" {
					pickupSeen = true
					if pod.Pod.Occupied || s.completed != 0 {
						t.Fatal("empty pickup counted as passenger travel")
					}
				}
				if pod.Pod.Activity == Idle && s.assigned(tc.pod) {
					if err := s.RequestJourney(tc.pod, "market"); !errors.Is(err, ErrBusy) {
						t.Fatal("assigned pickup pod could be stolen")
					}
				}
				if s.completed == 1 {
					break
				}
			}
			if tc.name != "local" && !pickupSeen {
				t.Fatal("missing empty pickup")
			}
			if s.completed != 1 || s.requestID != expectedID || pod.Request == nil || pod.Request.ID != expectedID || !pod.Request.Completed || pod.Pod.StationID != tc.destination || len(s.waiting) != 0 {
				t.Fatalf("pickup did not preserve original passenger journey: %+v", s.Snapshot())
			}
		})
	}
}

func TestBoardRoutesFromActualPickupBerth(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(ladderNetwork(), []Placement{{ID: "01", StationID: "market", BerthID: "market-2"}})
	if err != nil {
		t.Fatal(err)
	}
	staleRoute, err := s.stationApproachRoute("market-berth", "garden")
	if err != nil {
		t.Fatal(err)
	}
	trip := waitingTrip{
		request: Request{ID: 1, From: "market", To: "garden", PodID: "01"},
		route:   staleRoute,
	}
	if err := s.board(s.findVehicle("01"), trip); err != nil {
		t.Fatal(err)
	}
	vehicle := s.findVehicle("01")
	if len(vehicle.Route) == 0 || vehicle.Route[0].From != "market-berth-2" {
		t.Fatalf("passenger route starts at %q, want actual berth market-berth-2", vehicle.Route[0].From)
	}
}

func TestRemotePickupYieldsToNewLocalPod(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(Example(), []Placement{
		{ID: "01", StationID: "parking", BerthID: "parking-1"},
		{ID: "02", StationID: "market"},
		{ID: "03", StationID: "parking", BerthID: "parking-2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	local := s.findVehicle("02")
	local.Pod.Activity, local.Pod.Occupied = Unloading, true
	local.Request = &Request{ID: 99, From: "garden", To: "market", PodID: local.Pod.ID}
	local.phaseTicks = 180 * TicksPerSecond
	if err := s.RequestTrip("market", "garden"); err != nil {
		t.Fatal(err)
	}
	remote := s.findVehicle("01")
	if len(s.waiting) != 1 || s.waiting[0].request.PodID != remote.Pod.ID || remote.RelocatingTo != "market" {
		t.Fatalf("fixture did not assign remote pickup: %+v", s.Snapshot())
	}

	local.phaseTicks = 1
	s.Step()

	if local.Pod.Activity != Boarding || local.Request == nil || local.Request.ID != 1 || len(s.waiting) != 0 {
		t.Fatalf("new local pod did not replace remote pickup: %+v", s.Snapshot())
	}
	if s.assigned(remote.Pod.ID) {
		t.Fatal("remote pickup retained the passenger assignment")
	}
	// The released pod had not left its berth, so it stays there.
	if remote.Pod.Activity != Idle || remote.Pod.BerthID != "parking-1" || remote.RelocatingTo != "" || remote.released {
		t.Fatalf("released pod did not stay at its berth: %+v", remote.Vehicle)
	}
}

func TestQueuedRequestsReusePod(t *testing.T) {
	t.Parallel()
	s, err := New(Example(), "parking")
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.RequestTrip("harbor", "garden"); err != nil {
			t.Fatal(err)
		}
	}
	state := s.Snapshot()
	if len(state.Pending) != 2 || state.Pending[0].PodID != "01" || state.Pending[1].PodID != "" {
		t.Fatalf("wrong queue: %+v", state.Pending)
	}
	state.Pending[0].From = "mutated"
	if s.Snapshot().Pending[0].From != "harbor" {
		t.Fatal("snapshot exposes mutable requests")
	}
	s.SetPaused(true)
	paused := s.Snapshot()
	advance(s, 100)
	if !reflect.DeepEqual(paused, s.Snapshot()) {
		t.Fatal("pause advanced dispatch")
	}
	s.SetPaused(false)
	for range 900 * TicksPerSecond {
		s.Step()
		checkTraffic(t, s.Snapshot())
		if s.completed == 2 {
			break
		}
	}
	if s.completed != 2 || len(s.waiting) != 0 || s.requestID != 2 || s.vehicles[0].Request.ID != 2 {
		t.Fatalf("queued request did not reuse pod: %+v", s.Snapshot())
	}
	s.Reset()
	if s.requestID != 0 || len(s.waiting) != 0 || s.vehicles[0].Pod.StationID != "parking" {
		t.Fatal("reset retained dispatch state")
	}
}

func TestInfeasiblePickupDoesNotBlockOtherStation(t *testing.T) {
	t.Parallel()
	n := Example()
	for i, lane := range n.Lanes {
		if lane.ID == "market-approach" {
			n.Lanes = append(n.Lanes[:i], n.Lanes[i+1:]...)
			break
		}
	}
	s, err := NewFleet(n, []Placement{{ID: "01", StationID: "market"}, {ID: "02", StationID: "harbor"}, {ID: "03", StationID: "parking"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RequestJourney("01", "garden"); err != nil {
		t.Fatal(err)
	}
	if err := s.RequestTrip("market", "harbor"); err != nil {
		t.Fatal(err)
	}
	if err := s.RequestTrip("harbor", "garden"); err != nil {
		t.Fatal(err)
	}
	if len(s.waiting) != 1 || s.waiting[0].request.PodID != "" || s.findVehicle("02").Pod.Activity != Boarding || s.findVehicle("03").Pod.Activity != Idle {
		t.Fatalf("blocked origin stalled other work or claimed a pod: %+v", s.Snapshot())
	}
}

func TestLocalDemandPrecedesParking(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(Example(), []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "market"}})
	if err != nil {
		t.Fatal(err)
	}
	local := s.findVehicle("02")
	local.Pod.Activity, local.Pod.Occupied = Unloading, true
	local.phaseTicks = 180 * TicksPerSecond
	local.Request = &Request{ID: 1, From: "garden", To: "market", PartySize: 1, PodID: "02"}
	s.requestID = 1
	if err := s.RequestJourney("01", "market"); err != nil {
		t.Fatal(err)
	}
	if err := s.RequestTrip("market", "garden"); err != nil {
		t.Fatal(err)
	}
	for range 120 * TicksPerSecond {
		s.Step()
	}
	if s.findVehicle("01").Pod.WaitReason != BerthOccupied {
		t.Fatal("fixture did not queue an occupied arrival")
	}
	local.phaseTicks = 1
	s.Step()
	if local.Pod.Activity != Boarding || local.RelocatingTo != "" || local.Request.ID != 3 || len(s.waiting) != 0 {
		t.Fatalf("local request lost pod to parking: %+v", s.Snapshot())
	}
}

func TestRejectedStationRequestsDoNotMutate(t *testing.T) {
	t.Parallel()
	for _, pair := range [][2]string{{"missing", "market"}, {"harbor", "missing"}, {"parking", "market"}, {"harbor", "parking"}, {"harbor", "harbor"}} {
		t.Run(pair[0]+" to "+pair[1], func(t *testing.T) {
			t.Parallel()
			s := newTraffic(t)
			before := s.Snapshot()
			if err := s.RequestTrip(pair[0], pair[1]); err == nil {
				t.Fatal("invalid trip accepted")
			}
			if s.requestID != 0 || !reflect.DeepEqual(before, s.Snapshot()) {
				t.Fatal("rejected trip mutated state")
			}
		})
	}
}

func TestStationRequestsAfterExpandedDemo(t *testing.T) {
	t.Parallel()
	s := newTraffic(t)
	if err := s.StartDemo(); err != nil {
		t.Fatal(err)
	}
	for range 900 * TicksPerSecond {
		s.Step()
		checkTraffic(t, s.Snapshot())
		if !s.Snapshot().Demo {
			break
		}
	}
	if s.completed != demoJourneys || s.demo != nil {
		t.Fatal("demo did not finish")
	}
	for range 2 {
		if err := s.RequestTrip("harbor", "garden"); err != nil {
			t.Fatal(err)
		}
	}
	for range 900 * TicksPerSecond {
		s.Step()
		checkTraffic(t, s.Snapshot())
		if s.completed == demoJourneys+2 {
			break
		}
	}
	if s.completed != demoJourneys+2 || len(s.waiting) != 0 {
		t.Fatalf("parked fleet did not serve queued trips: %+v", s.Snapshot())
	}
}

func TestPickupWaitsForIncomingPassengerPod(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(Example(), []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "parking"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RequestJourney("01", "market"); err != nil {
		t.Fatal(err)
	}
	for range 120 * TicksPerSecond {
		s.Step()
		if s.findVehicle("01").Pod.LaneID == "market-in" {
			break
		}
	}
	if s.findVehicle("01").Pod.LaneID != "market-in" || s.owners[resource{kind: berthResource, id: "market-1"}] != "" {
		t.Fatal("fixture needs an incoming pod before berth admission")
	}
	if err := s.RequestTrip("market", "garden"); err != nil {
		t.Fatal(err)
	}
	for range 360 * TicksPerSecond {
		s.Step()
		checkTraffic(t, s.Snapshot())
		if s.completed == 2 {
			break
		}
	}
	if s.completed != 2 || len(s.waiting) != 0 {
		t.Fatalf("remote pickup trapped an incoming passenger pod: %+v", s.Snapshot())
	}
}

func TestPassengerAndPickupShareBerthAdmission(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(Example(), []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "parking"}})
	if err != nil {
		t.Fatal(err)
	}
	local := s.findVehicle("01")
	local.Pod.Activity, local.Pod.Occupied = Unloading, true
	local.phaseTicks = 120 * TicksPerSecond
	local.Request = &Request{ID: 1, From: "garden", To: "harbor", PartySize: 1, PodID: "01"}
	s.requestID = 1
	if err := s.RequestTrip("market", "garden"); err != nil {
		t.Fatal(err)
	}
	local.phaseTicks = 1
	s.Step()
	if err := s.RequestTrip("harbor", "market"); err != nil {
		t.Fatal(err)
	}
	if s.findVehicle("01").Pod.Activity != Boarding {
		t.Fatal("passenger departure was delayed by a remote pickup claim")
	}
	for range 600 * TicksPerSecond {
		s.Step()
		checkTraffic(t, s.Snapshot())
		if s.completed == 3 {
			break
		}
	}
	if s.completed != 3 || len(s.waiting) != 0 {
		t.Fatalf("pickup reservation stalled new passenger trip: %+v", s.Snapshot())
	}
}

func TestPickupDepartsBeforeBerthClears(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(Example(), []Placement{{ID: "01", StationID: "garden"}, {ID: "02", StationID: "market"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RequestJourney("01", "market"); err != nil {
		t.Fatal(err)
	}
	s.findVehicle("01").phaseTicks = 180 * TicksPerSecond
	if err := s.RequestTrip("garden", "market"); err != nil {
		t.Fatal(err)
	}
	pickup := s.findVehicle("02")
	if pickup.RelocatingTo != "garden" || pickup.Pod.Activity != DepartingEmpty {
		t.Fatal("pickup did not leave for occupied berth")
	}
	if s.owners[resource{kind: berthResource, id: "garden-1"}] != "01" {
		t.Fatal("pickup stole occupied berth")
	}
	queued := false
	for range 700 * TicksPerSecond {
		s.Step()
		checkTraffic(t, s.Snapshot())
		if pickup.Pod.LaneID == "garden-in" && pickup.Pod.Speed < 0.1 && pickup.Pod.WaitReason == BerthOccupied {
			queued = true
			if pickup.Pod.Occupied {
				t.Fatal("passenger boarded outside berth")
			}
		}
		if s.completed == 2 {
			break
		}
	}
	if !queued || s.completed != 2 {
		t.Fatalf("pickup did not queue safely then serve order: queued=%v state=%+v", queued, s.Snapshot())
	}
}

func TestPickupArrivalOrderDoesNotReorderPassengers(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(Example(), []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "market"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RequestTrip("garden", "market"); err != nil {
		t.Fatal(err)
	}
	s.findVehicle("01").phaseTicks = 180 * TicksPerSecond
	if err := s.RequestTrip("garden", "harbor"); err != nil {
		t.Fatal(err)
	}
	if s.waiting[0].request.PodID != "01" || s.waiting[1].request.PodID != "02" {
		t.Fatal("fixture did not assign two pickup pods")
	}
	boarded := make(map[int]bool)
	for range 800 * TicksPerSecond {
		s.Step()
		checkTraffic(t, s.Snapshot())
		for _, v := range s.vehicles {
			if v.Pod.Activity != Boarding || v.Request == nil {
				continue
			}
			if v.Request.ID == 2 && !boarded[1] {
				t.Fatal("later passenger boarded first")
			}
			if v.Request.ID == 1 && v.Pod.ID != "02" {
				t.Fatal("oldest passenger did not take first arriving pod")
			}
			boarded[v.Request.ID] = true
		}
		if s.completed == 2 {
			break
		}
	}
	if s.completed != 2 || len(boarded) != 2 {
		t.Fatalf("out-of-order pickups failed: %+v", s.Snapshot())
	}
}

// boardingCounters holds the counters that change when a party boards.
type boardingCounters struct {
	boarded, sharedParties       int
	totalWaitTicks, maxWaitTicks int64
}

func boardingCountersOf(s *Simulation) boardingCounters {
	return boardingCounters{
		boarded: s.boarded, sharedParties: s.sharedParties,
		totalWaitTicks: s.totalWaitTicks, maxWaitTicks: s.maxWaitTicks,
	}
}

// completeRequest steps s until the pod completes its request, for at most
// 300 simulated seconds.
func completeRequest(t *testing.T, s *Simulation, pod *vehicle) {
	t.Helper()
	for range 300 * TicksPerSecond {
		s.Step()
		if pod.Request.Completed {
			return
		}
	}
	t.Fatalf("pod %s did not complete request %d: %+v", pod.Pod.ID, pod.Request.ID, s.Snapshot())
}

func TestQueuedTripBoardsWithItsParties(t *testing.T) {
	t.Parallel()
	const wait = 30 * TicksPerSecond
	// saved holds the counters of an earlier run, which a restore keeps.
	saved := boardingCounters{boarded: 5, sharedParties: 2, totalWaitTicks: 40 * TicksPerSecond, maxWaitTicks: 20 * TicksPerSecond}
	for _, tc := range []struct {
		name        string
		parties     int
		wantParties int
		want        boardingCounters
	}{
		{
			name: "new order", parties: 0, wantParties: 1,
			want: boardingCounters{boarded: 6, sharedParties: 2, totalWaitTicks: 70 * TicksPerSecond, maxWaitTicks: wait},
		},
		{name: "requeued party", parties: 1, wantParties: 1, want: saved},
		{name: "requeued shared ride", parties: 3, wantParties: 3, want: saved},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := newSharingSimulation(t)
			advance(s, wait)
			s.boarded, s.sharedParties, s.totalWaitTicks, s.maxWaitTicks =
				saved.boarded, saved.sharedParties, saved.totalWaitTicks, saved.maxWaitTicks
			s.requestID = 1
			s.waiting = append(s.waiting, waitingTrip{
				request: Request{ID: 1, From: "harbor", To: "market", PartySize: tc.wantParties},
				parties: tc.parties,
			})
			// The wait statistics must not change when the trip boards.
			queued := s.waitStats()
			s.dispatch()
			if boarded := s.waitStats(); boarded != queued {
				t.Fatalf("wait statistics are %+v while queued and %+v after boarding", queued, boarded)
			}
			pod := s.findVehicle("01")
			if len(s.waiting) != 0 || pod.Pod.Activity != Boarding || pod.Parties != tc.wantParties ||
				pod.Request == nil || pod.Request.PartySize != tc.wantParties {
				t.Fatalf("pod %s did not board %d parties: %+v", pod.Pod.ID, tc.wantParties, s.Snapshot())
			}
			if got := boardingCountersOf(s); got != tc.want {
				t.Fatalf("boarding counters are %+v, want %+v", got, tc.want)
			}
			completed := s.completed
			completeRequest(t, s, pod)
			if got := s.completed - completed; got != tc.wantParties || pod.Parties != 0 {
				t.Fatalf("unloading completed %d parties and kept %d, want %d and 0", got, pod.Parties, tc.wantParties)
			}
		})
	}
}

func TestSharedRideCountsQueuedParties(t *testing.T) {
	t.Parallel()
	const wait = TicksPerSecond
	for _, tc := range []struct {
		name        string
		parties     int
		joined      bool
		wantParties int
		want        boardingCounters
	}{
		{
			name: "new order", parties: 0, joined: true, wantParties: 2,
			want: boardingCounters{boarded: 2, sharedParties: 1, totalWaitTicks: wait, maxWaitTicks: wait},
		},
		{name: "requeued party", parties: 1, joined: true, wantParties: 2, want: boardingCounters{boarded: 1}},
		{name: "requeued shared ride that fits", parties: 3, joined: true, wantParties: 4, want: boardingCounters{boarded: 1}},
		{name: "requeued shared ride over the limit", parties: 4, wantParties: 1, want: boardingCounters{boarded: 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := newSharingSimulation(t)
			if err := s.SetSharedRidePartyLimit(4); err != nil {
				t.Fatal(err)
			}
			if err := s.RequestTrip("harbor", "market"); err != nil {
				t.Fatal(err)
			}
			pod := s.findVehicle("01")
			advance(s, wait)
			if pod.Pod.Activity != Boarding || pod.Parties != 1 {
				t.Fatalf("pod %s is not boarding one party: %+v", pod.Pod.ID, s.Snapshot())
			}
			s.requestID = 2
			s.waiting = append(s.waiting, waitingTrip{
				request: Request{ID: 2, From: "harbor", To: "market", PartySize: max(1, tc.parties)},
				parties: tc.parties,
			})
			s.dispatch()
			joined := !slices.ContainsFunc(s.waiting, func(trip waitingTrip) bool { return trip.request.ID == 2 })
			if joined != tc.joined {
				t.Fatalf("the trip joined the shared ride: %t, want %t", joined, tc.joined)
			}
			if pod.Parties != tc.wantParties || pod.Request.PartySize != tc.wantParties {
				t.Fatalf("pod %s has %d parties of size %d, want %d", pod.Pod.ID, pod.Parties, pod.Request.PartySize, tc.wantParties)
			}
			if got := boardingCountersOf(s); got != tc.want {
				t.Fatalf("boarding counters are %+v, want %+v", got, tc.want)
			}
			completeRequest(t, s, pod)
			if s.completed != tc.wantParties {
				t.Fatalf("completed %d parties, want %d", s.completed, tc.wantParties)
			}
		})
	}
}

// TestOnePassDoesNotReuseAClaimedPickupPod checks that dispatch chooses the
// pickup pod again after an assignment in the same pass. Two trips from one
// station wait in the queue and two parked pods are available. Each trip must
// get a different pod.
func TestOnePassDoesNotReuseAClaimedPickupPod(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(Example(), []Placement{
		{ID: "01", StationID: "parking", BerthID: "parking-1"},
		{ID: "02", StationID: "parking", BerthID: "parking-2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	s.requestID = 2
	for id := 1; id <= 2; id++ {
		s.waiting = append(s.waiting, waitingTrip{request: Request{ID: id, From: "harbor", To: "garden", PartySize: 1}})
	}
	s.dispatch()
	if len(s.waiting) != 2 {
		t.Fatalf("queue has %d trips, want 2", len(s.waiting))
	}
	pods := make(map[string]bool)
	for _, trip := range s.waiting {
		v := s.findVehicle(trip.request.PodID)
		if v == nil || v.RelocatingTo != "harbor" {
			t.Fatalf("trip %d has no pod on the way to harbor: %+v", trip.request.ID, s.Snapshot())
		}
		pods[v.Pod.ID] = true
	}
	if len(pods) != 2 {
		t.Fatalf("the two trips share one pod: %+v", s.Snapshot().Pending)
	}
}
