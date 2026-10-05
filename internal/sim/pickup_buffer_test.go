package sim

import (
	"maps"
	"reflect"
	"slices"
	"testing"
)

func pickupBufferFixture(t *testing.T, enabled bool, scale float64) *Simulation {
	t.Helper()
	s, err := NewFleet(stationBufferNetwork(Example(), scale), []Placement{{ID: "01", StationID: "harbor"}})
	if err != nil {
		t.Fatal(err)
	}
	s.SetStationBuffers(enabled)
	if err := s.RequestTrip("market", "garden"); err != nil {
		t.Fatal(err)
	}
	return s
}

func finishPickupBufferRequest(t *testing.T, s *Simulation) {
	t.Helper()
	for range 1200 * TicksPerSecond {
		s.Step()
		if _, err := s.SafetyObservation().Check(); err != nil {
			t.Fatal(err)
		}
		if _, err := s.ExportState().checkContract(); err != nil {
			t.Fatal(err)
		}
		if s.completed == 1 {
			return
		}
	}
	t.Fatal("pickup request did not complete within the fixture's travel allowance")
}

func TestPickupBufferDispatch(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		enabled bool
		scale   float64
		buffer  bool
	}{
		{name: "enabled", enabled: true, scale: 4, buffer: true},
		{name: "disabled", scale: 4},
		{name: "short approach", enabled: true, scale: 0.5},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s := pickupBufferFixture(t, test.enabled, test.scale)
			v := s.findVehicle("01")
			if v.buffered != test.buffer || v.Pod.Activity != DepartingEmpty || !s.assigned(v.Pod.ID) {
				t.Fatalf("wrong departure: buffered=%t pod=%+v", v.buffered, v.Pod)
			}
			if test.buffer {
				if v.destination.ID != "" || v.Route[len(v.Route)-1].To != "market-entry" {
					t.Fatal("buffer pickup chose a berth before arrival")
				}
			} else if v.destination.ID != "market-1" {
				t.Fatal("ordinary pickup lost its berth assignment")
			}
			if s.owners[resource{kind: berthResource, id: "harbor-1"}] != podResourceOwner(v.Pod.ID) {
				t.Fatal("pickup departure released its origin before movement")
			}
			if _, err := s.ExportState().checkContract(); err != nil {
				t.Fatal(err)
			}
			finishPickupBufferRequest(t, s)
		})
	}
}

func TestPickupBufferRestore(t *testing.T) {
	t.Parallel()
	for _, phase := range []string{"departing", "upstream", "holding"} {
		t.Run(phase, func(t *testing.T) {
			t.Parallel()
			s := pickupBufferFixture(t, true, 4)
			barrier := resource{kind: berthResource, id: "market-1"}
			s.owners[barrier] = podResourceOwner("external")
			switch phase {
			case "upstream":
				stepUntil(t, s, phase, func() bool {
					v := s.findVehicle("01")
					return v.Pod.Activity == Traveling && v.Pod.LaneID != "market-approach"
				})
			case "holding":
				stepUntil(t, s, phase, func() bool {
					v := s.findVehicle("01")
					plan, ok := s.bufferPlan(v)
					return ok && v.Pod.Speed == 0 && v.distance == v.blocks.end(plan.frontier)
				})
			}
			state := s.ExportState()
			if _, err := state.checkContract(); err != nil {
				t.Fatal(err)
			}
			if phase == "departing" {
				legacy := s.ExportState()
				legacy.Pods[0].StationBuffered = false
				if _, err := legacy.checkContract(); err == nil {
					t.Fatal("ordinary empty departure accepted no destination berth")
				}
			}
			restored, result, err := RestoreState(RestoreStateInput{
				Network: s.network, Fleet: s.initial, State: state,
			})
			if err != nil || result.Tier != RestorePhysical || len(result.Demoted)+len(result.Requeued)+len(result.Dropped) != 0 {
				t.Fatalf("restore failed: %+v %v", result, err)
			}
			v := restored.findVehicle("01")
			if restored.stationBuffers || !v.buffered || !restored.assigned(v.Pod.ID) || v.Pod.Position != s.findVehicle("01").Pod.Position {
				t.Fatal("restore lost pickup pose, assignment, or disabled drain membership")
			}
			if !reflect.DeepEqual(restored.Snapshot().Pending, s.Snapshot().Pending) {
				t.Fatal("restore changed pending request identity or timing")
			}
			finishPickupBufferRequest(t, restored)
			if restored.NeedsBufferState() {
				t.Fatal("completed pickup retained buffer membership")
			}
		})
	}
}

func TestPickupBufferDiversionKeepsReservedPrefix(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(stationBufferNetwork(Example(), 4), []Placement{{ID: "01", StationID: "harbor"}})
	if err != nil {
		t.Fatal(err)
	}
	v := s.findVehicle("01")
	if !s.park(v) {
		t.Fatal("pod could not start parking")
	}
	stepUntil(t, s, "pod on bypass", func() bool { return v.Pod.LaneID == "bypass-in" && v.Pod.LaneDistance > 20 })
	prefix, _, ok := s.divertStart(v)
	if !ok {
		t.Fatal("parking pod cannot divert in this fixture")
	}
	route := slices.Clone(v.Route[:prefix])
	position, speed, distance, reserved := v.Pod.Position, v.Pod.Speed, v.distance, v.reservedThrough
	owners := maps.Clone(s.owners)
	s.SetStationBuffers(true)
	if err := s.RequestTrip("market", "garden"); err != nil {
		t.Fatal(err)
	}
	if !v.buffered || v.destination.ID != "" || !reflect.DeepEqual(route, v.Route[:prefix]) ||
		v.Pod.Position != position || v.Pod.Speed != speed || v.distance != distance || v.reservedThrough != reserved {
		t.Fatal("buffer diversion changed the physical or reserved prefix")
	}
	for resource, owner := range owners {
		if resource.kind == trackResource && s.owners[resource] != owner {
			t.Fatal("buffer diversion revoked admitted track")
		}
	}
	finishPickupBufferRequest(t, s)
}

func TestPickupBufferPreservesEarlierAssignment(t *testing.T) {
	t.Parallel()
	s := pickupBufferFixture(t, false, 4)
	v := s.findVehicle("01")
	s.SetStationBuffers(true)
	stepUntil(t, s, "ordinary pickup reaches its berth", func() bool {
		if v.RelocatingTo == "market" && (v.buffered || v.destination.ID != "market-1") {
			t.Fatal("enabling buffers revoked an existing pickup berth assignment")
		}
		return v.Pod.Activity == Boarding && v.Pod.StationID == "market"
	})
}
