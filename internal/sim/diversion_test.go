package sim

import (
	"reflect"
	"testing"
)

func TestParkingDiversionPreservesMotion(t *testing.T) {
	t.Parallel()
	for _, phase := range []string{"before departure", "on return lane"} {
		t.Run(phase, func(t *testing.T) {
			t.Parallel()
			s, err := New(Example(), "market")
			if err != nil {
				t.Fatal(err)
			}
			v := s.findVehicle("01")
			if !s.park(v) {
				t.Fatal("could not start parking move")
			}
			if phase == "on return lane" {
				for range 90 * TicksPerSecond {
					s.Step()
					if v.Pod.LaneID == "return-start" && v.Pod.LaneDistance > 40 {
						break
					}
				}
				if v.Pod.LaneID != "return-start" {
					t.Fatal("did not reach moving diversion point")
				}
			}
			before := v.Pod
			oldDestination := v.destination
			reserved := append([]block(nil), v.blocks.all()[:v.reservedThrough+1]...)
			if err := s.RequestTrip("harbor", "garden"); err != nil {
				t.Fatal(err)
			}
			if v.RelocatingTo != "harbor" || s.Snapshot().Pending[0].PodID != "01" {
				t.Fatal("parking pod was not diverted to pickup")
			}
			if v.Pod.Position != before.Position || v.Pod.Speed != before.Speed || v.Pod.LaneDistance != before.LaneDistance || v.Pod.LaneID != before.LaneID {
				t.Fatal("diversion teleported or changed current motion")
			}
			if len(reserved) > 0 && !reflect.DeepEqual(reserved, v.blocks.all()[:v.reservedThrough+1]) {
				t.Fatal("diversion changed committed track")
			}
			if s.owners[resource{kind: berthResource, id: oldDestination.ID}] != "" || s.owners[resource{kind: nodeResource, id: oldDestination.Node}] != "" {
				t.Fatal("diversion retained unused parking claim")
			}
			for range 400 * TicksPerSecond {
				s.Step()
				checkTraffic(t, s.Snapshot())
				if v.Pod.StationID == "parking" {
					t.Fatal("diverted pod still visited parking")
				}
				if s.completed == 1 {
					break
				}
			}
			if s.completed != 1 || v.Pod.StationID != "garden" || s.requestID != 1 {
				t.Fatalf("diverted pod did not serve original order: %+v", s.Snapshot())
			}
		})
	}
}

func TestCommittedParkingInletFinishesBeforePickup(t *testing.T) {
	t.Parallel()
	s, err := New(Example(), "market")
	if err != nil {
		t.Fatal(err)
	}
	v := s.findVehicle("01")
	if !s.park(v) {
		t.Fatal("could not start parking move")
	}
	for range 120 * TicksPerSecond {
		s.Step()
		if v.Pod.LaneID == "parking-in-1" {
			break
		}
	}
	if v.Pod.LaneID != "parking-in-1" {
		t.Fatal("did not enter parking inlet")
	}
	if err := s.RequestTrip("harbor", "garden"); err != nil {
		t.Fatal(err)
	}
	if v.RelocatingTo != "parking" || s.Snapshot().Pending[0].PodID != "" {
		t.Fatal("diverted during committed parking maneuver")
	}
	parked := false
	for range 400 * TicksPerSecond {
		s.Step()
		checkTraffic(t, s.Snapshot())
		parked = parked || v.Pod.StationID == "parking"
		if s.completed == 1 {
			break
		}
	}
	if !parked || s.completed != 1 {
		t.Fatalf("late order was not served after parking: %+v", s.Snapshot())
	}
}

func TestParkingDepartureCanceledForLocalOrder(t *testing.T) {
	t.Parallel()
	s, err := New(Example(), "market")
	if err != nil {
		t.Fatal(err)
	}
	v := s.findVehicle("01")
	if !s.park(v) {
		t.Fatal("could not start parking move")
	}
	if err := s.RequestTrip("market", "garden"); err != nil {
		t.Fatal(err)
	}
	if v.Pod.Activity != Boarding || v.RelocatingTo != "" || len(v.Riders) == 0 || v.Riders[0].From != "market" || len(s.waiting) != 0 {
		t.Fatalf("local order did not cancel unstarted parking move: %+v", s.Snapshot())
	}
	if s.owners[resource{kind: berthResource, id: "parking-1"}] != "" {
		t.Fatal("local pickup retained unused parking berth")
	}
	for range 360 * TicksPerSecond {
		s.Step()
		checkTraffic(t, s.Snapshot())
		if s.completed == 1 {
			break
		}
	}
	if s.completed != 1 {
		t.Fatal("local order did not complete")
	}
}

// The shared arrival lane leads to either parking berth. A diversion from
// that lane can cross berth 1 even when the pod was going to berth 2.
func chainedParkingArrival() Network {
	network := Example()
	network.Nodes = append(network.Nodes, Node{ID: "parking-arrival", Position: Point{580, 150}}, Node{ID: "parking-arrival-branch", Position: Point{180, 150}})
	for index := range network.Lanes {
		lane := &network.Lanes[index]
		if lane.ID == "parking-in-1" || lane.ID == "parking-in-2" {
			lane.From = "parking-arrival-branch"
		}
	}
	network.Lanes = append(network.Lanes,
		Lane{ID: "parking-arrival-link", From: "parking-entry", To: "parking-arrival", SpeedLimit: 14},
		Lane{ID: "parking-arrival-next", From: "parking-arrival", To: "parking-arrival-branch", SpeedLimit: 14},
	)
	return network
}

func TestCommittedParkingAccessChainFinishesBeforePickup(t *testing.T) {
	t.Parallel()
	testCommittedStationAccessChainFinishesBeforePickup(t, true)
}

func TestCommittedPassengerAccessChainFinishesBeforePickup(t *testing.T) {
	t.Parallel()
	testCommittedStationAccessChainFinishesBeforePickup(t, false)
}

func testCommittedStationAccessChainFinishesBeforePickup(t *testing.T, parkingOnly bool) {
	t.Helper()
	for _, test := range []struct {
		name, lane string
		distance   float64
		restore    bool
	}{
		{name: "live entry", lane: "parking-arrival-link", distance: 5},
		{name: "restored entry", lane: "parking-arrival-link", distance: 5, restore: true},
		{name: "live chain", lane: "parking-arrival-next", distance: 60},
		{name: "restored chain", lane: "parking-arrival-next", distance: 60, restore: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			network := chainedParkingArrival()
			for index := range network.Stations {
				if network.Stations[index].ID == "parking" {
					network.Stations[index].ParkingOnly = parkingOnly
				}
			}
			fleet := []Placement{
				{ID: "01", StationID: "market", BerthID: "market-1"},
				{ID: "02", StationID: "garden", BerthID: "garden-1"},
			}
			s, err := NewFleet(network, fleet)
			if err != nil {
				t.Fatal(err)
			}
			station, _ := s.station("parking")
			lead := s.findVehicle("01")
			if moveErr := s.startEmptyMove(lead, emptyDestination{station: station.ID, berth: station.Berths[1], reserveBerth: true}); moveErr != nil {
				t.Fatal(moveErr)
			}
			stepUntil(t, s, "pod enters shared parking access", func() bool {
				return lead.Pod.LaneID == test.lane && lead.Pod.LaneDistance > test.distance
			})
			if moveErr := s.startEmptyMove(s.findVehicle("02"), emptyDestination{station: station.ID, berth: station.Berths[0], reserveBerth: true}); moveErr != nil {
				t.Fatal(moveErr)
			}
			if !parkingOnly {
				lead.released = true
			}
			if test.restore {
				var result RestoreResult
				s, result, err = RestoreState(RestoreStateInput{Network: network, Fleet: fleet, State: s.ExportState()})
				if err != nil || result.Tier != RestorePhysical || len(result.Demoted) != 0 {
					t.Fatalf("physical restore: result %+v, error %v", result, err)
				}
				lead = s.findVehicle("01")
				if test.lane == "parking-arrival-next" {
					for _, lane := range lead.Route {
						if lane.From == station.Entry {
							t.Fatal("restored chain still includes the station entry")
						}
					}
				}
			}
			if err := s.SetFinishingPodWait(FinishingPodWaitNone); err != nil {
				t.Fatal(err)
			}
			if _, _, ok := s.pickupRoute(lead, "harbor"); ok {
				t.Fatal("pickup route can leave a committed station access chain")
			}
			before := lead.Pod
			if err := s.RequestTrip("harbor", "garden"); err != nil {
				t.Fatal(err)
			}
			if lead.RelocatingTo != "parking" || lead.destination.ID != "parking-2" {
				t.Fatalf("pod diverted inside shared station access: %+v", lead.Vehicle)
			}
			if lead.Pod.Position != before.Position || lead.Pod.Speed != before.Speed || lead.Pod.LaneID != before.LaneID || lead.Pod.LaneDistance != before.LaneDistance {
				t.Fatal("pickup changed the parking pod's motion")
			}
			for _, claim := range berthResources(station.Berths[1]) {
				if s.owners[claim] != "01" {
					t.Fatal("pickup released the committed parking destination")
				}
			}
			parked := false
			for range 600 * TicksPerSecond {
				s.Step()
				checkTraffic(t, s.Snapshot())
				parked = parked || lead.Pod.BerthID == "parking-2"
				if s.completed == 1 {
					break
				}
			}
			if !parked || s.completed != 1 {
				t.Fatalf("parking arrival or pickup stalled: parked %v, state %+v", parked, s.Snapshot())
			}
		})
	}
}

func TestParkingArrivalReservationBoundary(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name      string
		committed bool
	}{
		{name: "approaching entry"},
		{name: "arrival reserved ahead", committed: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, err := New(chainedParkingArrival(), "market")
			if err != nil {
				t.Fatal(err)
			}
			v := s.findVehicle("01")
			if !s.park(v) {
				t.Fatal("cannot start parking move")
			}
			stepUntil(t, s, test.name, func() bool {
				if v.Pod.LaneID != "return-to-parking" || v.reservedThrough < 0 {
					return false
				}
				lane := v.blocks.at(v.reservedThrough).lane
				return (lane.ID == "parking-arrival-link") == test.committed
			})
			before := s.ExportState()
			if _, _, ok := s.pickupRoute(v, "harbor"); ok == test.committed {
				t.Fatalf("pickup candidacy is %v, want %v", ok, !test.committed)
			}
			if !reflect.DeepEqual(before, s.ExportState()) {
				t.Fatal("pickup query changed simulation state")
			}
		})
	}
}

func TestPassengerArrivalReservationBoundary(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name      string
		committed bool
		restore   bool
	}{
		{name: "approaching entry"},
		{name: "arrival reserved ahead", committed: true},
		{name: "restored approach", restore: true},
		{name: "restored approach after lookahead", committed: true, restore: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			network := chainedParkingArrival()
			for index := range network.Stations {
				if network.Stations[index].ID == "parking" {
					network.Stations[index].ParkingOnly = false
				}
			}
			fleet := []Placement{{ID: "01", StationID: "market", BerthID: "market-1"}}
			s, err := NewFleet(network, fleet)
			if err != nil {
				t.Fatal(err)
			}
			v := s.findVehicle("01")
			station, _ := s.station("parking")
			if moveErr := s.startEmptyMove(v, emptyDestination{station: station.ID, berth: station.Berths[0], reserveBerth: true}); moveErr != nil {
				t.Fatal(moveErr)
			}
			v.released = true
			stepUntil(t, s, test.name, func() bool {
				if v.Pod.LaneID != "return-to-parking" || v.reservedThrough < 0 {
					return false
				}
				lane := v.blocks.at(v.reservedThrough).lane
				return (lane.ID == "parking-arrival-link") == test.committed
			})
			wantCommitted := test.committed
			if test.restore {
				var result RestoreResult
				s, result, err = RestoreState(RestoreStateInput{Network: network, Fleet: fleet, State: s.ExportState()})
				if err != nil || result.Tier != RestorePhysical || len(result.Demoted) != 0 {
					t.Fatalf("physical restore: result %+v, error %v", result, err)
				}
				v = s.findVehicle("01")
				// Physical restore rebuilds the current footprint, not every
				// live lookahead claim. The pod has not entered the access chain.
				if lane := v.blocks.at(v.reservedThrough).lane.ID; lane != "return-to-parking" {
					t.Fatalf("restored footprint ends on %s, want approach lane", lane)
				}
				wantCommitted = false
			}
			before := s.ExportState()
			if _, _, ok := s.pickupRoute(v, "harbor"); ok == wantCommitted {
				t.Fatalf("pickup candidacy is %v, want %v", ok, !wantCommitted)
			}
			if !reflect.DeepEqual(before, s.ExportState()) {
				t.Fatal("pickup query changed simulation state")
			}
		})
	}
}
