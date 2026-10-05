package sim

import (
	"fmt"
	"math"
	"reflect"
	"testing"
)

func TestStationCompactActualApproach(t *testing.T) {
	t.Parallel()
	for _, roadSpeed := range []float64{2.5, 14} {
		t.Run(fmt.Sprint(roadSpeed), func(t *testing.T) {
			t.Parallel()
			geometry := occupiedBufferQueue(t, PlatooningVirtual)
			for i := range geometry.network.Lanes {
				lane := &geometry.network.Lanes[i]
				if lane.StationRole == "" {
					lane.SpeedLimit = roadSpeed
				}
			}
			s, err := NewFleet(geometry.network, geometry.initial)
			if err != nil {
				t.Fatal(err)
			}
			s.SetStationBuffers(true)
			if err := s.SetPlatooning(PlatooningVirtual); err != nil {
				t.Fatal(err)
			}
			if err := s.SetStationQueueSpacing(StationQueueCompactV1); err != nil {
				t.Fatal(err)
			}
			for _, p := range geometry.initial[:4] {
				if err := s.RequestJourney(p.ID, "market"); err != nil {
					t.Fatal(err)
				}
			}
			settled, largest := 0, 0
			for range 3000 * TicksPerSecond {
				compactTick(t, s)
				stopped := false
				for _, g := range s.compactGroups {
					largest = max(largest, len(g.members))
					if len(g.members) == 4 {
						stopped = true
						for _, i := range g.members {
							stopped = stopped && s.vehicles[i].Pod.Speed == 0
						}
						if stopped {
							span := s.vehicles[g.members[0]].Pod.LaneDistance - s.vehicles[g.members[3]].Pod.LaneDistance
							if math.Abs(span-18.03) > 1e-9 {
								t.Fatalf("stopped span %.17g", span)
							}
						}
					}
				}
				if stopped {
					settled++
				} else {
					settled = 0
				}
				if settled >= TicksPerSecond {
					break
				}
			}
			t.Logf("speed=%g tick=%d largest=%d settled=%d", roadSpeed, s.tick, largest, settled)
			if settled < TicksPerSecond {
				t.Fatalf("real departures failed formation: %+v", s.Snapshot())
			}
			if s.completed != 0 || s.findVehicle("05").Pod.Activity != Idle {
				t.Fatal("lost occupied berth")
			}
		})
	}
}

func TestStationCompactColdDisabledNoSetters(t *testing.T) {
	t.Parallel()
	s := compactStateFixture(t)
	r, result, err := RestoreState(RestoreStateInput{Network: s.network, Fleet: s.initial, State: roundTripState(t, s.ExportState()), StationQueueSpacing: StationQueueOrdinary, PlatoonLimit: 4})
	if err != nil || result.Tier != RestorePhysical || result.PhysicalError != nil {
		t.Fatalf("%+v %v", result, err)
	}
	for range 1000 * TicksPerSecond {
		compactTick(t, r)
		if len(r.compactGroups) == 0 {
			break
		}
	}
	if len(r.compactGroups) != 0 {
		t.Fatal("direct cold disabled recovery did not finish")
	}
	for i := 1; i < 4; i++ {
		if r.vehicles[i-1].Pod.LaneDistance-r.vehicles[i].Pod.LaneDistance < 12.01 {
			t.Fatal("lost ordinary spacing")
		}
	}
	t.Logf("recovered tick=%d", r.tick)
}

func TestStationCompactFaultStepNoMotion(t *testing.T) {
	t.Parallel()
	s := compactStateFixture(t)
	head := &s.vehicles[s.compactGroups[0].members[0]]
	plan, _ := s.bufferPlan(head)
	s.owners[head.blocks.at(plan.frontier).resources[0]] = podResourceOwner("05")
	before := s.Snapshot()
	s.Step()
	if s.CompactQueueError() == nil || !s.paused {
		t.Fatal("fault did not stop controller")
	}
	after := s.Snapshot()
	for i := range before.Vehicles {
		a, b := before.Vehicles[i].Pod, after.Vehicles[i].Pod
		if a.Position != b.Position || a.Speed != b.Speed || a.LaneDistance != b.LaneDistance {
			t.Fatalf("fault moved pod %s", a.ID)
		}
	}
	state := s.ExportState()
	s.Step()
	if !reflect.DeepEqual(state, s.ExportState()) {
		t.Fatal("paused fault changed state")
	}
	t.Log(s.CompactQueueError())
}
