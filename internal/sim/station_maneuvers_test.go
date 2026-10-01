package sim

import (
	"reflect"
	"strconv"
	"testing"
)

func TestStationPhaseCacheTransitions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		role StationLaneRole
		want StationPhase
	}{
		{StationApproachRole, ApproachingStation},
		{StationEntryRole, EnteringStation},
		{StationBerthAccessRole, AccessingBerth},
		{StationThroughRole, PassingStation},
		{StationDepartureRole, DepartingBerth},
		{StationExitRole, ExitingStation},
		{"", ""},
	} {
		t.Run(string(tc.role), func(t *testing.T) {
			t.Parallel()
			s := &Simulation{network: Network{Nodes: []Node{{ID: "a"}, {ID: "b", Position: Point{X: 100}}}}}
			v := &vehicle{}
			s.setVehicleRoute(v, []Lane{{ID: "lane", From: "a", To: "b", SpeedLimit: 14, StationID: "station", StationRole: tc.role}})
			station := "station"
			if tc.want == "" {
				station = ""
			}
			check := func(phase StationPhase, station string) {
				t.Helper()
				s.updateStationPhase(v)
				if v.Pod.StationPhase != phase || v.Pod.ManeuverStationID != station {
					t.Fatalf("phase=%q station=%q, want %q %q", v.Pod.StationPhase, v.Pod.ManeuverStationID, phase, station)
				}
			}
			check(tc.want, station)
			v.Pod.StationPhase, v.Pod.ManeuverStationID = "stale", "stale"
			check(tc.want, station)
			v.Pod.BerthID, v.Pod.StationID = "berth", "first"
			check(AtBerth, "first")
			v.Pod.StationID = "second"
			check(AtBerth, "second")
			v.Pod.BerthID = ""
			check(tc.want, station)
			for _, index := range []int{-1, v.blocks.len(), 0} {
				v.blockIndex = index
				if index == 0 {
					check(tc.want, station)
				} else {
					check("", "")
				}
			}
			// Route replacement must invalidate the cache at a saturated version.
			v.routeVersion = ^uint64(0)
			s.setVehicleRoute(v, []Lane{{ID: "replacement", From: "a", To: "b", SpeedLimit: 14, StationID: "replacement-station", StationRole: StationExitRole}})
			check(ExitingStation, "replacement-station")
			s.setVehicleRoute(v, nil)
			check("", "")
		})
	}
}

func TestStationPhaseCacheDemoParity(t *testing.T) {
	t.Parallel()
	live := newTraffic(t)
	if err := live.StartDemo(); err != nil {
		t.Fatal(err)
	}
	control := live.Clone()
	for live.demo != nil {
		if live.tick >= demoTickLimit {
			t.Fatal("demo did not finish")
		}
		live.Step()
		control.Step()
		for index := range control.vehicles {
			uncachedStationPhase(&control.vehicles[index])
		}
		if !reflect.DeepEqual(live.Snapshot(), control.Snapshot()) {
			t.Fatalf("tick %d: snapshot differs from uncached phases", live.tick)
		}
		if live.tick%137 == 0 {
			if !reflect.DeepEqual(live.ExportState(), control.ExportState()) {
				t.Fatalf("tick %d: saved state differs", live.tick)
			}
			restored, result, err := RestoreState(RestoreStateInput{Network: Example(), Fleet: demoFleet(), State: live.ExportState()})
			if err != nil || !cleanRestore(result) {
				t.Fatalf("tick %d: restore %+v, %v", live.tick, result, err)
			}
			checkRestoredMatches(t, live, restored)
			for index := range restored.vehicles {
				pod := restored.vehicles[index].Pod
				uncachedStationPhase(&restored.vehicles[index])
				if pod != restored.vehicles[index].Pod {
					t.Fatalf("tick %d: restored phase differs", live.tick)
				}
			}
		}
	}
}

// uncachedStationPhase retains the phase calculation before the cache.
func uncachedStationPhase(v *vehicle) {
	if v.Pod.BerthID != "" {
		v.Pod.StationPhase, v.Pod.ManeuverStationID = AtBerth, v.Pod.StationID
		return
	}
	v.Pod.StationPhase, v.Pod.ManeuverStationID = "", ""
	lane := currentManeuverLane(v)
	if lane == nil {
		return
	}
	if phase := phaseForStationRole(lane.StationRole); phase != "" {
		v.Pod.StationPhase, v.Pod.ManeuverStationID = phase, lane.StationID
		return
	}
	index := v.blocks.locate(v.firstBlockForLane(lane.ID), 0)
	if index >= 0 && index+1 < len(v.Route) {
		next := v.Route[index+1]
		if next.StationRole == StationEntryRole || next.StationRole == StationBerthAccessRole {
			v.Pod.StationPhase, v.Pod.ManeuverStationID = ApproachingStation, next.StationID
		}
	}
}

func TestStationManeuverRouteIndex(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		indexed bool
	}{
		{name: "indexed", indexed: true},
		{name: "unindexed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := &Simulation{network: Network{Nodes: []Node{{ID: "a"}, {ID: "b", Position: Point{X: 100}}, {ID: "c", Position: Point{X: 200}}}}}
			through := Lane{ID: "through", From: "a", To: "b", SpeedLimit: 14}
			entry := Lane{ID: "entry", From: "b", To: "c", SpeedLimit: 14, StationID: "target", StationRole: StationEntryRole}
			back := Lane{ID: "back", From: "c", To: "a", SpeedLimit: 14}
			plain := Lane{ID: "plain", From: "b", To: "c", SpeedLimit: 14}
			s.network.Lanes = []Lane{through, entry, back, plain}
			v := &vehicle{}
			s.setVehicleRoute(v, []Lane{through, entry, back, through, plain})
			v.blockIndex = v.blocks.laneFirst(3)
			if !tc.indexed {
				v.blockStarts = nil
			}
			s.updateStationPhase(v)
			if v.Pod.StationPhase != ApproachingStation || v.Pod.ManeuverStationID != "target" {
				t.Fatalf("repeated lane phase %q station %q", v.Pod.StationPhase, v.Pod.ManeuverStationID)
			}
			s.setVehicleRoute(v, []Lane{through, plain})
			v.blockIndex = 0
			if !tc.indexed {
				v.blockStarts = nil
			}
			s.updateStationPhase(v)
			if v.Pod.StationPhase != "" || v.Pod.ManeuverStationID != "" {
				t.Fatalf("replacement route retained phase %q station %q", v.Pod.StationPhase, v.Pod.ManeuverStationID)
			}
		})
	}
}

func BenchmarkStationManeuverRouteIndex(b *testing.B) {
	for _, lanes := range []int{16, 64, 256} {
		b.Run(strconv.Itoa(lanes), func(b *testing.B) {
			s := &Simulation{network: Network{Nodes: []Node{{ID: "a"}, {ID: "b", Position: Point{X: 100}}}}}
			route := make([]Lane, lanes)
			for i := range route {
				route[i] = Lane{ID: "lane-" + strconv.Itoa(i), From: "a", To: "b", SpeedLimit: 14}
			}
			route[lanes-1].StationID = "target"
			route[lanes-1].StationRole = StationEntryRole
			v := &vehicle{}
			s.setVehicleRoute(v, route)
			v.blockIndex = v.blocks.laneFirst(lanes - 2)
			b.ReportAllocs()
			for b.Loop() {
				s.updateStationPhase(v)
			}
		})
	}
}
