package sim

import (
	"fmt"
	"math"
	"reflect"
	"testing"
)

func TestAutomaticBerthClearing(t *testing.T) {
	t.Parallel()
	for _, occupied := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty blocker", true: "unloading blocker"}[occupied], func(t *testing.T) {
			t.Parallel()
			s, err := NewFleet(Example(), []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "market"}})
			if err != nil {
				t.Fatal(err)
			}
			blocker := s.findVehicle("02")
			if occupied {
				blocker.Pod.Activity, blocker.Pod.Occupied = Unloading, true
				blocker.phaseTicks = 90 * TicksPerSecond
				blocker.Request = &Request{ID: 1, From: "garden", To: "market", PartySize: 1}
				s.requestID = 1
			}
			if err := s.RequestJourney("01", "market"); err != nil {
				t.Fatal(err)
			}
			relocated, retainedOrigin := false, false
			for range 240 * TicksPerSecond {
				s.Step()
				state := s.Snapshot()
				checkTraffic(t, state)
				if blocker.RelocatingTo != "" {
					relocated = true
					if blocker.Pod.Occupied {
						t.Fatal("relocation carried a passenger")
					}
					if occupied && !blocker.Request.Completed {
						t.Fatal("relocation interrupted unloading")
					}
					if s.owners[resource{kind: berthResource, id: blocker.destination.ID}] != "02" {
						t.Fatal("lost parking reservation")
					}
					if blocker.Pod.Activity == Traveling && blocker.distance < Clearance {
						retainedOrigin = true
						if s.owners[resource{kind: berthResource, id: "market-1"}] != "02" {
							t.Fatal("released occupied origin early")
						}
					}
				}
				if state.Vehicles[0].Pod.Activity == Idle && blocker.Pod.StationID == "parking" {
					break
				}
			}
			want := 1
			if occupied {
				want = 2
			}
			if !relocated || !retainedOrigin || s.completed != want || s.requestID != want || blocker.Pod.StationID != "parking" || blocker.Pod.Occupied {
				t.Fatalf("incorrect empty movement/accounting: %+v", s.Snapshot())
			}
			before := s.Snapshot()
			if err := s.RequestJourney("02", "harbor"); err == nil {
				t.Fatal("accepted passenger pickup from parking")
			}
			if !reflect.DeepEqual(before, s.Snapshot()) {
				t.Fatal("rejected parking request mutated state")
			}
			s.Reset()
			if s.Snapshot().Vehicles[1].Pod.StationID != "market" {
				t.Fatal("reset retained relocation")
			}
		})
	}
}

func TestUnavailableParking(t *testing.T) {
	t.Parallel()
	n := Example()
	for i, lane := range n.Lanes {
		if lane.ID == "return-to-parking" {
			n.Lanes = append(n.Lanes[:i], n.Lanes[i+1:]...)
			break
		}
	}
	s, err := NewFleet(n, []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "market"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RequestJourney("01", "market"); err != nil {
		t.Fatal(err)
	}
	for range 180 * TicksPerSecond {
		s.Step()
		checkTraffic(t, s.Snapshot())
	}
	state := s.Snapshot()
	if state.Completed != 0 || state.Vehicles[0].Pod.WaitReason != ParkingUnavailable || state.Vehicles[0].Pod.Speed != 0 || state.Vehicles[1].Pod.BerthID != "market-1" {
		t.Fatalf("unavailable parking did not retain safe wait: %+v", state)
	}
}

func TestPassengerBerthClearingPrefersUntargetedBerth(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		allTargeted bool
		want        string
	}{
		{name: "free berth first", want: "garden-1"},
		{name: "targeted berth as fallback", allTargeted: true, want: "harbor-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, err := NewFleet(Example(), []Placement{
				{ID: "01", StationID: "market"},
				{ID: "02", StationID: "parking", BerthID: "parking-1"},
				{ID: "03", StationID: "parking", BerthID: "parking-2"},
			})
			if err != nil {
				t.Fatal(err)
			}
			arrival := s.findVehicle("02")
			arrival.Pod.Activity = Traveling
			arrival.destination = Berth{ID: "harbor-1", Node: "harbor-berth"}
			if tc.allTargeted {
				arrival = s.findVehicle("03")
				arrival.Pod.Activity = Traveling
				arrival.destination = Berth{ID: "garden-1", Node: "garden-berth"}
			}
			blocker := s.findVehicle("01")
			if !s.clearToPassengerBerth(blocker) || blocker.destination.ID != tc.want {
				t.Fatalf("clearing destination %q, want %q", blocker.destination.ID, tc.want)
			}
		})
	}
}

func TestPassengerBerthClearingYieldsClaimToPassengerTarget(t *testing.T) {
	t.Parallel()
	for _, passengerFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("passenger_first_%t", passengerFirst), func(t *testing.T) {
			t.Parallel()
			s, err := NewFleet(Example(), []Placement{
				{ID: "01", StationID: "parking", BerthID: "parking-1"},
				{ID: "02", StationID: "garden"},
			})
			if err != nil {
				t.Fatal(err)
			}
			request := func() {
				if err := s.RequestJourney("02", "market"); err != nil {
					t.Fatal(err)
				}
			}
			if passengerFirst {
				request()
			}
			clearing := s.findVehicle("01")
			destination := Berth{ID: "market-1", Node: "market-berth"}
			if err := s.startEmptyMove(clearing, emptyDestination{station: "market", berth: destination, reserveBerth: true}); err != nil {
				t.Fatal(err)
			}
			if !passengerFirst {
				request()
			}
			s.Step()
			for _, claimed := range []resource{{kind: berthResource, id: destination.ID}, {kind: nodeResource, id: destination.Node}} {
				if s.owners[claimed] == clearing.Pod.ID {
					t.Fatalf("clearing move kept passenger destination claim %+v", claimed)
				}
			}
			for range 15 * 60 * TicksPerSecond {
				s.Step()
				state := s.Snapshot()
				checkTraffic(t, state)
				if state.Completed == 1 && state.Vehicles[0].Pod.Activity == Idle && state.Vehicles[1].Pod.Activity == Idle {
					return
				}
			}
			t.Fatalf("passenger and yielding empty pod did not settle: %+v", s.Snapshot())
		})
	}
}

func TestBerthClearingUsesFreePassengerBerthWhenParkingIsFull(t *testing.T) {
	t.Parallel()
	network := Example()
	network.Nodes = append(network.Nodes, Node{ID: "market-berth-2", Position: Point{X: 760, Y: 250}})
	network.Lanes = append(network.Lanes,
		Lane{ID: "market-in-2", From: "market-entry", To: "market-berth-2", SpeedLimit: 14},
		Lane{ID: "market-out-2", From: "market-berth-2", To: "market-exit", SpeedLimit: 14},
	)
	for i := range network.Stations {
		if network.Stations[i].ID == "market" {
			network.Stations[i].Berths = append(network.Stations[i].Berths, Berth{ID: "market-2", Node: "market-berth-2"})
		}
	}
	s, err := NewFleet(network, []Placement{
		{ID: "01", StationID: "harbor"},
		{ID: "02", StationID: "market", BerthID: "market-2"},
		{ID: "03", StationID: "parking", BerthID: "parking-1"},
		{ID: "04", StationID: "parking", BerthID: "parking-2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RequestJourney("01", "market"); err != nil {
		t.Fatal(err)
	}
	arrival := s.findVehicle("01")
	for range 180 * TicksPerSecond {
		s.Step()
		if arrival.Pod.LaneID == "market-in" {
			break
		}
	}
	if arrival.Pod.LaneID != "market-in" {
		t.Fatal("passenger arrival did not commit to the occupied branch")
	}
	blocker := s.findVehicle("02")
	delete(s.owners, resource{kind: berthResource, id: "market-2"})
	delete(s.owners, resource{kind: nodeResource, id: "market-berth-2"})
	s.owners[resource{kind: berthResource, id: "market-1"}] = blocker.Pod.ID
	s.owners[resource{kind: nodeResource, id: "market-berth"}] = blocker.Pod.ID
	blocker.Pod.BerthID = "market-1"
	blocker.Pod.Position = Point{X: 800, Y: 350}
	for range 300 * TicksPerSecond {
		s.Step()
		checkTraffic(t, s.Snapshot())
		if s.completed == 1 && blocker.Pod.BerthID == "market-2" {
			break
		}
	}
	if s.completed != 1 || blocker.Pod.StationID != "market" || blocker.Pod.BerthID != "market-2" {
		t.Fatalf("passenger did not disembark after local berth clearing: %+v", s.Snapshot())
	}
}

func TestCruiseOnClearStraight(t *testing.T) {
	t.Parallel()
	s, err := New(Example(), "market")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RequestJourney("01", "harbor"); err != nil {
		t.Fatal(err)
	}
	samples := 0
	for range 180 * TicksPerSecond {
		s.Step()
		pod := s.Snapshot().Vehicles[0].Pod
		if pod.LaneID == "return-to-parking" && pod.LaneDistance > 60 && pod.LaneDistance < 220 {
			samples++
			if math.Abs(pod.Speed-14) > 1e-6 {
				t.Fatalf("free straight speed cycled at %.2fm: %.5fm/s", pod.LaneDistance, pod.Speed)
			}
		}
	}
	if samples < 500 {
		t.Fatalf("insufficient cruise samples: %d", samples)
	}
}

func TestParkingReservationsDoNotCollide(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(Example(), []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"}, {ID: "03", StationID: "market"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"01", "02"} {
		if err := s.RequestJourney(id, "market"); err != nil {
			t.Fatal(err)
		}
	}
	sharedRun := false
	for range 300 * TicksPerSecond {
		s.Step()
		state := s.Snapshot()
		checkTraffic(t, state)
		moving := 0
		claims := make(map[string]bool)
		for _, v := range s.vehicles {
			if v.RelocatingTo == "" {
				continue
			}
			moving++
			if claims[v.destination.ID] {
				t.Fatal("two pods reserved the same parking berth")
			}
			claims[v.destination.ID] = true
		}
		sharedRun = sharedRun || moving == 2
	}
	state := s.Snapshot()
	parked := 0
	for _, v := range state.Vehicles {
		if v.Pod.StationID == "parking" {
			parked++
		}
	}
	if !sharedRun || parked != 2 || state.Completed != 2 {
		t.Fatalf("parking moves failed: concurrent=%v state=%+v", sharedRun, state)
	}
}
