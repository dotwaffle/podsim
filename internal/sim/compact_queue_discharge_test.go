package sim

import (
	"encoding/json/v2"
	"fmt"
	"maps"
	"os"
	"reflect"
	"slices"
	"testing"
)

func compactDischargeGeometry(t *testing.T, roadSpeed float64) (Network, []Placement) {
	t.Helper()
	geometry := occupiedBufferQueue(t, PlatooningVirtual)
	for _, lane := range Example().Lanes {
		if lane.ID == "return" {
			geometry.network.Lanes = append(geometry.network.Lanes, lane)
		}
	}
	for i := range geometry.network.Lanes {
		if geometry.network.Lanes[i].StationRole == "" {
			geometry.network.Lanes[i].SpeedLimit = roadSpeed
		}
	}
	return geometry.network, geometry.initial
}

// compactDischargeEndpoint is the cold endpoint of the matched 2,000-second
// service screen. Pod 02 completed at market while head 04 retained its queue.
func compactDischargeEndpoint(t *testing.T, mode StationQueueSpacing) *Simulation {
	t.Helper()
	network, fleet := compactDischargeGeometry(t, 2.5)
	raw, err := os.ReadFile("testdata/compact_discharge_endpoint.json")
	if err != nil {
		t.Fatal(err)
	}
	var state SavedState
	if err = json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	s, result, err := RestoreState(RestoreStateInput{Network: network, Fleet: fleet, State: state, StationBuffers: true, BufferPlatoons: true, CompactQueues: true, StationQueueSpacing: mode, PlatoonLimit: 4})
	if err != nil || result.Tier != RestorePhysical || result.PhysicalError != nil || len(result.Demoted)+len(result.Requeued)+len(result.Dropped) != 0 {
		t.Fatalf("restore: %+v %v", result, err)
	}
	if err := s.SetPlatooning(PlatooningVirtual); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestStationCompactDischargeIdleBerth(t *testing.T) {
	t.Parallel()
	for _, mode := range []StationQueueSpacing{StationQueueCompactV1, StationQueueOrdinary} {
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()
			s := compactDischargeEndpoint(t, mode)
			head, blocker := s.findVehicle("04"), s.findVehicle("02")
			group := s.compactGroup(head)
			if group == nil || blocker.Pod.Activity != Idle || head.Pod.LaneDistance+Clearance >= group.bounds.frontier ||
				head.Pod.WaitReason != NoWait || head.Pod.BlockedBy != "" || head.bufferBerth != "" {
				t.Fatal("endpoint does not reproduce the hidden far-frontier blocker")
			}
			before := s.ExportState()
			probe := s.probeCompactDischarge(group)
			if probe.available || probe.blockedBerth != "market-1" || probe.blockedOwner != "02" || !reflect.DeepEqual(before, s.ExportState()) {
				t.Fatalf("read-only blocker probe: %+v", probe)
			}
			compactTick(t, s)
			if blocker.Pod.Activity != DepartingEmpty || head.Pod.WaitReason != BerthOccupied || head.Pod.BlockedBy != "02" || head.bufferBerth != "market-1" {
				t.Fatalf("idle berth did not clear: head=%+v blocker=%+v", head.Pod, blocker.Pod)
			}
			if s.compactGroup(head) == nil || head.destination.ID != "" {
				t.Fatal("exclusive suffix admitted before ordinary recovery")
			}
		})
	}
}

func TestStationCompactDischargeProbeGuards(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		edit func(*testing.T, *Simulation, *vehicle)
	}{
		{"berth class", func(t *testing.T, s *Simulation, _ *vehicle) {
			t.Helper()
			classes, err := NewClassSet("compact")
			if err != nil {
				t.Fatal(err)
			}
			s.network.Stations[s.stationIndexes["market"]].Berths[0].VehicleClasses = classes
		}},
		{"continuation", func(t *testing.T, s *Simulation, head *vehicle) {
			t.Helper()
			classes, err := NewClassSet("compact")
			if err != nil {
				t.Fatal(err)
			}
			for i := range s.network.Lanes {
				lane := &s.network.Lanes[i]
				if lane.StationID == "market" && lane.StationRole == StationDepartureRole {
					lane.VehicleClasses = classes
				}
			}
			s.graph = newRouteGraph(s.network)
			s.routes = make(map[routeKey]routeResult)
			head.Stops = []string{"market", "harbor"}
		}},
		{"detour", func(t *testing.T, s *Simulation, head *vehicle) {
			t.Helper()
			if err := s.SetSharedRidePartyLimit(2); err != nil {
				t.Fatal(err)
			}
			head.riddenBase = 1e6
		}},
		{"suffix speed", func(_ *testing.T, s *Simulation, _ *vehicle) {
			for i := range s.network.Lanes {
				if s.network.Lanes[i].StationID == "market" && s.network.Lanes[i].StationRole == StationBerthAccessRole {
					s.network.Lanes[i].SpeedLimit = 14
				}
			}
			s.graph = newRouteGraph(s.network)
			s.routes = make(map[routeKey]routeResult)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := compactDischargeEndpoint(t, StationQueueCompactV1)
			head := s.findVehicle("04")
			tc.edit(t, s, head)
			before := s.ExportState()
			owners := maps.Clone(s.owners)
			probe := s.probeCompactDischarge(s.compactGroup(head))
			if probe.available || probe.blockedOwner != "" || probe.blockedBerth != "" {
				t.Fatalf("rejected berth published a probe: %+v", probe)
			}
			s.compactBerthBlocker(s.vehicleIndexes[head.Pod.ID])
			if !reflect.DeepEqual(before, s.ExportState()) || !maps.Equal(owners, s.owners) || head.Pod.WaitReason != NoWait || head.bufferBerth != "" {
				t.Fatal("rejected probe changed route, certificate, resources, or blocker signal")
			}
		})
	}
}

func TestStationCompactDischargeProbeResources(t *testing.T) {
	t.Parallel()
	s := compactDischargeEndpoint(t, StationQueueCompactV1)
	head := s.findVehicle("04")
	station, _ := s.station("market")
	berth := station.Berths[0]
	for _, r := range berthResources(berth) {
		delete(s.owners, r)
	}
	suffix, err := s.stationPathForClass(head.Route[len(head.Route)-1].To, berth.Node, head.Pod.Class)
	if err != nil {
		t.Fatal(err)
	}
	blocks, _ := s.routeBlocks(suffix)
	var barrier resource
	found := false
	for resources := range blocks.spanResources(0, blocks.len()) {
		for _, r := range resources {
			if r.kind == trackResource {
				barrier, found = r, true
			}
		}
	}
	if !found {
		t.Fatal("suffix has no track resource")
	}
	s.owners[barrier] = "external"
	before, owners := s.ExportState(), maps.Clone(s.owners)
	if probe := s.probeCompactDischarge(s.compactGroup(head)); probe.available {
		t.Fatal("occupied suffix reported free")
	}
	if !reflect.DeepEqual(before, s.ExportState()) || !maps.Equal(owners, s.owners) {
		t.Fatal("failed suffix probe changed state or claims")
	}
	delete(s.owners, barrier)
	before, owners = s.ExportState(), maps.Clone(s.owners)
	if probe := s.probeCompactDischarge(s.compactGroup(head)); !probe.available || probe.blockedOwner != "" {
		t.Fatalf("free suffix probe: %+v", probe)
	}
	if !reflect.DeepEqual(before, s.ExportState()) || !maps.Equal(owners, s.owners) || head.destination.ID != "" {
		t.Fatal("available probe granted a suffix")
	}
}

func TestStationCompactDischargeProbeMembers(t *testing.T) {
	t.Parallel()
	for _, id := range []string{"01", "03", "05"} {
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			s := compactDischargeEndpoint(t, StationQueueCompactV1)
			before, owners := s.ExportState(), maps.Clone(s.owners)
			s.compactBerthBlocker(s.vehicleIndexes[id])
			if !reflect.DeepEqual(before, s.ExportState()) || !maps.Equal(owners, s.owners) || s.findVehicle(id).Pod.WaitReason != NoWait {
				t.Fatal("non-head pod published a compact berth blocker")
			}
		})
	}
}

func TestStationCompactDischargeFaultNoMotion(t *testing.T) {
	t.Parallel()
	s := compactDischargeEndpoint(t, StationQueueCompactV1)
	head := s.findVehicle("04")
	plan, _ := s.bufferPlan(head)
	s.owners[head.blocks.at(plan.frontier).resources[0]] = "external"
	before := s.Snapshot()
	owners := maps.Clone(s.owners)
	s.Step()
	if s.CompactQueueError() == nil || !s.paused {
		t.Fatal("invalid ownership did not pause")
	}
	for i, v := range s.vehicles {
		old := before.Vehicles[i].Pod
		if old.Position != v.Pod.Position || old.Speed != v.Pod.Speed {
			t.Fatal("fault moved a pod")
		}
	}
	if s.findVehicle("02").Pod.Activity != Idle || !maps.Equal(owners, s.owners) {
		t.Fatal("invalid group evicted a berth blocker")
	}
}

func TestStationCompactDischargeFreshIdleBlocker(t *testing.T) {
	t.Parallel()
	for _, speed := range []float64{2.5, 14} {
		t.Run(fmt.Sprint(speed), func(t *testing.T) {
			t.Parallel()
			network, fleet := compactDischargeGeometry(t, speed)
			s, err := NewFleet(network, fleet)
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
			for _, p := range fleet[:4] {
				if err := s.RequestJourney(p.ID, "market"); err != nil {
					t.Fatal(err)
				}
			}
			cleared := false
			for tick := range 2000 * TicksPerSecond {
				if tick == 900*TicksPerSecond {
					if _, err := s.SubmitTrip("market", "harbor"); err != nil {
						t.Fatal(err)
					}
				}
				compactTick(t, s)
				cleared = cleared || slices.ContainsFunc(s.vehicles, func(v vehicle) bool {
					return v.RelocatingTo != "" && slices.ContainsFunc(v.Riders, func(r Request) bool { return r.Completed && r.To == "market" })
				})
			}
			aboard := 0
			for i := range s.vehicles {
				aboard += s.vehicles[i].RidersAboard()
			}
			minimum := 2
			if speed == 14 {
				minimum = 3
			}
			if !cleared || s.completed < minimum || s.completed+len(s.waiting)+aboard != 5 {
				t.Fatalf("fresh service: cleared=%t completed=%d pending=%d aboard=%d", cleared, s.completed, len(s.waiting), aboard)
			}
			t.Logf("roads=%g completed=%d/5 tick=%d", speed, s.completed, s.tick)
		})
	}
}
