package sim

import (
	"fmt"
	"reflect"
	"slices"
	"testing"
)

func TestPresentationRepeatedOccurrences(t *testing.T) {
	s := newExample(t)
	v := &s.vehicles[0]
	lane := s.network.Lanes[0]
	route := slices.Repeat([]Lane{lane}, 4096)
	s.setVehicleRoute(v, route)
	v.Pod.LaneID = lane.ID
	v.blockIndex = v.blocks.laneFirst(2500)
	state, routes, err := s.PresentationSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	r := routes[0]
	if len(state.Vehicles[0].Route) != 0 || r.Current != 2500 || r.Start != 1476 || len(r.Lanes) != 2048 || len(r.Display) != 1 || !r.Before || !r.After {
		t.Fatalf("bad bounded route: %+v", r)
	}
	version := r.Identity
	v.blockIndex = v.blocks.laneFirst(2600)
	_, routes, err = s.PresentationSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if routes[0].Identity != version || routes[0].Start != 1576 {
		t.Fatal("window shift changed route identity")
	}
	s.setVehicleRoute(v, slices.Clone(route))
	_, routes, err = s.PresentationSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if routes[0].Identity == version {
		t.Fatal("route replacement kept identity")
	}
	if !reflect.DeepEqual(s.Snapshot().Vehicles[0].Route, route) {
		t.Fatal("legacy snapshot lost complete route")
	}
}
func TestPresentationIsolationAndMalformedBlocks(t *testing.T) {
	s := newExample(t)
	v := &s.vehicles[0]
	lane := s.network.Lanes[0]
	s.setVehicleRoute(v, []Lane{lane})
	v.Pod.LaneID = lane.ID
	_, routes, err := s.PresentationSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	routes[0].Lanes[0] = -1
	_, again, err := s.PresentationSnapshot()
	if err != nil || again[0].Lanes[0] < 0 {
		t.Fatal("presentation exposed mutable storage")
	}
	v.blocks = blockList{}
	if _, _, err = s.PresentationSnapshot(); err == nil {
		t.Fatal("accepted missing blocks")
	}
}

func TestPresentationIdentityExhaustion(t *testing.T) {
	s := newExample(t)
	v := &s.vehicles[0]
	v.routeVersion = ^uint64(0) - 1
	v.replaceRoute(nil)
	v.replaceRoute(nil)
	if v.routeVersion != ^uint64(0) {
		t.Fatal("identity wrapped")
	}
	if _, _, err := s.PresentationSnapshot(); err == nil {
		t.Fatal("exhausted identity accepted")
	}
}

func TestPresentationVehicleMarksAreIndependent(t *testing.T) {
	t.Parallel()
	s := newExample(t)
	s.vehicles = slices.Repeat(s.vehicles, 4)
	a, b := s.network.Lanes[0], s.network.Lanes[1]
	inputs := [][]Lane{{a, a, b}, {b, a, b}, nil, {a}}
	for i, route := range inputs {
		s.setVehicleRoute(&s.vehicles[i], route)
		s.vehicles[i].Pod.LaneID = ""
	}
	for range 2 {
		_, routes, err := s.PresentationSnapshot()
		if err != nil {
			t.Fatal(err)
		}
		expected := [][]int{{0, 1}, {0, 1}, nil, {0}}
		for i, want := range expected {
			if !slices.Equal(routes[i].Display, want) {
				t.Fatalf("vehicle %d display = %v, want %v", i, routes[i].Display, want)
			}
		}
		routes[0].Display[0] = -1
		if routes[1].Display[0] != 0 {
			t.Fatal("vehicle displays share storage")
		}
	}
}

func BenchmarkPresentationFleet(b *testing.B) {
	for _, size := range []int{1, 300} {
		b.Run(fmt.Sprintf("vehicles-%d", size), func(b *testing.B) {
			s, err := New(Example(), "harbor")
			if err != nil {
				b.Fatal(err)
			}
			route := slices.Repeat(s.network.Lanes[:2], 32)
			// Extra lanes size the presentation index without changing the route.
			for len(s.network.Lanes) < 8000 {
				lane := s.network.Lanes[0]
				lane.ID = fmt.Sprintf("extra-%d", len(s.network.Lanes))
				s.network.Lanes = append(s.network.Lanes, lane)
			}
			s.vehicles = slices.Repeat(s.vehicles, size)
			for i := range s.vehicles {
				s.setVehicleRoute(&s.vehicles[i], route)
				s.vehicles[i].Pod.LaneID = ""
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, _, err := s.PresentationSnapshot(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
