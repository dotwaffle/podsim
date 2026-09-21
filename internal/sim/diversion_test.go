package sim

import (
	"reflect"
	"testing"
)

func TestParkingDiversionPreservesMotion(t *testing.T) {
	t.Parallel()
	for _, phase := range []string{"before departure", "on return lane"} {
		t.Run(phase, func(t *testing.T) {
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
			reserved := append([]block(nil), v.blocks[:v.reservedThrough+1]...)
			if err := s.RequestTrip("harbor", "garden"); err != nil {
				t.Fatal(err)
			}
			if v.RelocatingTo != "harbor" || s.Snapshot().Pending[0].PodID != "01" {
				t.Fatal("parking pod was not diverted to pickup")
			}
			if v.Pod.Position != before.Position || v.Pod.Speed != before.Speed || v.Pod.LaneDistance != before.LaneDistance || v.Pod.LaneID != before.LaneID {
				t.Fatal("diversion teleported or changed current motion")
			}
			if len(reserved) > 0 && !reflect.DeepEqual(reserved, v.blocks[:v.reservedThrough+1]) {
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
	if v.Pod.Activity != Boarding || v.RelocatingTo != "" || v.Request == nil || v.Request.From != "market" || len(s.waiting) != 0 {
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
