package sim

import (
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
