package sim

import (
	"strconv"
	"testing"
)

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
