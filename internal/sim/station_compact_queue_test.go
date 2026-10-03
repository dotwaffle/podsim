package sim

import (
	"math"
	"reflect"
	"testing"
)

func TestStationCompactPolicy(t *testing.T) {
	t.Parallel()
	s, err := New(Example(), "harbor")
	if err != nil {
		t.Fatal(err)
	}
	if s.StationQueueSpacing() != StationQueueOrdinary {
		t.Fatal("default is not ordinary")
	}
	for _, mode := range []StationQueueSpacing{"", "other", StationQueueCompactV1} {
		before := s.ExportState()
		if s.SetStationQueueSpacing(mode) == nil || !reflect.DeepEqual(before, s.ExportState()) || s.StationQueueSpacing() != StationQueueOrdinary {
			t.Fatal("invalid policy changed state")
		}
	}
	s.SetStationBuffers(true)
	if err := s.SetStationQueueSpacing(StationQueueCompactV1); err != nil {
		t.Fatal(err)
	}
	if s.StationQueueSpacing() != StationQueueCompactV1 {
		t.Fatal("policy was not selected")
	}
}

// compactTick checks actual physical motion independently of the group planner.
func compactTick(t *testing.T, s *Simulation) {
	t.Helper()
	before := s.Snapshot()
	wasCompact := make([]bool, len(s.vehicles))
	for i := range s.vehicles {
		wasCompact[i] = s.compactGroup(&s.vehicles[i]) != nil
	}
	s.Step()
	if err := s.CompactQueueError(); err != nil {
		t.Fatalf("tick %d: %v", s.tick, err)
	}
	if _, err := s.SafetyObservation().Check(); err != nil {
		t.Fatal(err)
	}
	checkIncrementalOwners(t, s)
	for i := range s.vehicles {
		v := &s.vehicles[i]
		old := before.Vehicles[i].Pod
		if v.link.buffer {
			checkBufferCertificate(t, v)
		}
		if old.Activity != Traveling || v.Pod.Activity != Traveling || !wasCompact[i] && s.compactGroup(v) == nil && before.Vehicles[i].PlatoonID == "" && !v.coupled() {
			continue
		}
		if v.Pod.Speed > v.Route[v.blocks.routeLane(v.blockIndex)].SpeedLimit || v.Pod.Speed < 0 || math.Abs(v.Pod.Speed-old.Speed) > acceleration/TicksPerSecond+1e-12 {
			t.Fatalf("invalid speed transition %.17g -> %.17g", old.Speed, v.Pod.Speed)
		}
		if (wasCompact[i] || s.compactGroup(v) != nil) && old.LaneID == v.Pod.LaneID && math.Abs(v.Pod.LaneDistance-old.LaneDistance-v.Pod.Speed/TicksPerSecond) > 1e-12 {
			t.Fatal("compact movement clipped or snapped")
		}
		if v.distance+stoppingDistance(v.Pod.Speed) > v.blocks.end(v.reservedThrough) {
			t.Fatal("unowned continuous stopping point")
		}
	}
}

func TestStationCompactOccupiedQueue(t *testing.T) {
	t.Parallel()
	s := occupiedBufferQueue(t, PlatooningVirtual)
	if err := s.SetStationQueueSpacing(StationQueueCompactV1); err != nil {
		t.Fatal(err)
	}
	stable := 0
	for range 200 * TicksPerSecond {
		compactTick(t, s)
		stopped := true
		for _, v := range s.vehicles[:4] {
			stopped = stopped && v.Pod.Speed == 0
		}
		if stopped {
			stable++
		} else {
			stable = 0
		}
		if stable == TicksPerSecond {
			break
		}
	}
	span := s.vehicles[0].Pod.LaneDistance - s.vehicles[3].Pod.LaneDistance
	t.Logf("span %.12f groups=%d coupled=%d tick=%d", span, len(s.compactGroups), s.CoupledPods(), s.tick)

	if stable < TicksPerSecond || math.Abs(span-3*6.01) > 1e-9 || s.CoupledPods() != 4 {
		t.Fatalf("compact queue did not settle: span %.12f groups=%d coupled=%d state=%+v", span, len(s.compactGroups), s.CoupledPods(), s.Snapshot())
	}
	if s.findVehicle("05").Pod.Activity != Idle || s.completed != 0 {
		t.Fatal("real occupied berth ceased blocking")
	}
}

func TestStationCompactDeparture(t *testing.T) {
	t.Parallel()
	s := departingBufferQueue(t)
	if err := s.SetStationQueueSpacing(StationQueueCompactV1); err != nil {
		t.Fatal(err)
	}
	coupled := false
	for range 1000 * TicksPerSecond {
		compactTick(t, s)
		coupled = coupled || s.CoupledPods() == 4
		if s.completed == 5 {
			break
		}
	}
	if !coupled || s.completed != 5 || len(s.compactGroups) != 0 {
		t.Fatalf("departure failed coupled=%v completed=%d compact=%d state=%+v", coupled, s.completed, len(s.compactGroups), s.Snapshot())
	}
	t.Logf("completed=%d final_tick=%d", s.completed, s.tick)
}
