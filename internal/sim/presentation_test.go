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
	if len(state.Vehicles[0].Route) != 0 || r.Current != 2500 || r.Start != 2500-MotionRouteLimit/2 || len(r.Lanes) != MotionRouteLimit || len(r.Display) != 1 || !r.Before || !r.After {
		t.Fatalf("bad bounded route: %+v", r)
	}
	version := r.Identity
	v.blockIndex = v.blocks.laneFirst(2600)
	_, routes, err = s.PresentationSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if routes[0].Identity != version || routes[0].Start != 2600-MotionRouteLimit/2 {
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
func TestPresentationSharesWindowAndRefusesMalformedBlocks(t *testing.T) {
	s := newExample(t)
	v := &s.vehicles[0]
	lane := s.network.Lanes[0]
	s.setVehicleRoute(v, []Lane{lane, lane})
	v.Pod.LaneID = lane.ID
	_, routes, err := s.PresentationSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	_, again, err := s.PresentationSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if &routes[0].Lanes[0] != &again[0].Lanes[0] || &routes[0].Display[0] != &again[0].Display[0] {
		t.Fatal("an unchanged window was built again")
	}
	if len(routes[0].Lanes) != cap(routes[0].Lanes) || len(routes[0].Display) != cap(routes[0].Display) {
		t.Fatal("a shared window has spare capacity")
	}
	v.blocks = blockList{}
	if _, _, err = s.PresentationSnapshot(); err == nil {
		t.Fatal("accepted missing blocks")
	}
}

func TestPresentationDisplayWindow(t *testing.T) {
	t.Parallel()
	for _, finished := range []bool{false, true} {
		t.Run(fmt.Sprintf("finished-%t", finished), func(t *testing.T) {
			t.Parallel()
			s := newExample(t)
			detachIndexes(s)
			v := &s.vehicles[0]
			route := make([]Lane, 2*MotionRouteLimit+1)
			first := len(s.network.Lanes)
			for i := range route {
				route[i] = s.network.Lanes[0]
				route[i].ID = fmt.Sprintf("window-%d", i)
			}
			s.network.Lanes = append(s.network.Lanes, route...)
			s.graph = newRouteGraph(s.network)
			v.replaceRoute(route)
			v.Pod.LaneID = ""
			if finished {
				v.distance = 1
			}
			_, routes, err := s.PresentationSnapshot()
			if err != nil {
				t.Fatal(err)
			}
			r := routes[0]
			start := 0
			if finished {
				start = len(route) - MotionRouteLimit/2
			}
			end := min(len(route), start+MotionRouteLimit)
			want := make([]int, end-start)
			for i := range want {
				want[i] = first + start + i
			}
			if !slices.Equal(r.Display, want) || !slices.Equal(r.Lanes, want) || len(r.Display) > MotionRouteLimit {
				t.Fatalf("display or motion differs from window [%d, %d): %+v", start, end, r)
			}
			if r.Origin != s.graph.nodes[route[0].From] || len(v.Route) != len(route) {
				t.Fatal("window changed the origin or live route")
			}
		})
	}
}

func TestPresentationIdentityExhaustion(t *testing.T) {
	s := newExample(t)
	v := &s.vehicles[0]
	v.routeVersion = MaxCounter - 1
	v.replaceRoute(nil)
	v.replaceRoute(nil)
	if v.routeVersion != MaxCounter {
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
		if &routes[0].Display[0] == &routes[1].Display[0] {
			t.Fatal("vehicle displays share storage")
		}
	}
}

func BenchmarkPresentationFleet(b *testing.B) {
	for _, size := range []int{1, 300} {
		b.Run(fmt.Sprintf("vehicles-%d", size), func(b *testing.B) {
			s, err := NewFleet(Example(), []Placement{{ID: "01", StationID: "harbor"}})
			if err != nil {
				b.Fatal(err)
			}
			route := slices.Repeat(s.network.Lanes[:2], 32)
			// Extra lanes size the presentation index without changing the route.
			detachIndexes(s)
			for len(s.network.Lanes) < 20000 {
				lane := s.network.Lanes[0]
				lane.ID = fmt.Sprintf("extra-%d", len(s.network.Lanes))
				s.network.Lanes = append(s.network.Lanes, lane)
			}
			s.vehicles = slices.Repeat(s.vehicles, size)
			s.graph = newRouteGraph(s.network)
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

// distinctRoute returns a route of length lanes that changes its lanes along
// the route, so each move of the window changes the display lanes.
func distinctRoute(s *Simulation, length int) []Lane {
	route := make([]Lane, length)
	for i := range route {
		route[i] = s.network.Lanes[(i*len(s.network.Lanes)/length)%len(s.network.Lanes)]
	}
	return route
}

// checkWindows compares the cached windows of s with a new build.
func checkWindows(t *testing.T, s *Simulation) []RoutePresentation {
	t.Helper()
	_, routes, err := s.PresentationSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	rebuilt, err := s.RebuiltPresentationRoutesForTest()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(routes, rebuilt) {
		t.Fatalf("cached windows differ from a new build\ncached  %+v\nrebuilt %+v", routes[0], rebuilt[0])
	}
	return routes
}

func TestRouteWindowCacheFollowsWindow(t *testing.T) {
	t.Parallel()
	s := newExample(t)
	v := &s.vehicles[0]
	route := distinctRoute(s, 3*MotionRouteLimit)
	s.setVehicleRoute(v, route)
	v.Pod.LaneID = route[0].ID
	var shared *int
	for current := 0; current < len(route); current += 97 {
		v.blockIndex = v.blocks.laneFirst(current)
		v.Pod.LaneID = route[current].ID
		routes := checkWindows(t, s)
		if routes[0].Current != uint64(current) {
			t.Fatalf("current = %d, want %d", routes[0].Current, current)
		}
		if current < MotionRouteLimit/2 {
			if shared != nil && shared != &routes[0].Lanes[0] {
				t.Fatal("a window that did not move was built again")
			}
			shared = &routes[0].Lanes[0]
		}
	}
}

func TestRouteWindowCacheFollowsRoute(t *testing.T) {
	t.Parallel()
	s := newExample(t)
	v := &s.vehicles[0]
	lanes := s.network.Lanes
	s.setVehicleRoute(v, []Lane{lanes[0], lanes[1]})
	checkWindows(t, s)
	// The new route has the same window bounds and a new version.
	s.setVehicleRoute(v, []Lane{lanes[1], lanes[0]})
	if routes := checkWindows(t, s); routes[0].Lanes[0] != s.graph.lanes[lanes[1].ID] {
		t.Fatalf("stale window %v", routes[0].Lanes)
	}
	s.setVehicleRoute(v, nil)
	if routes := checkWindows(t, s); routes[0].Lanes != nil || routes[0].Display != nil {
		t.Fatalf("empty route has a window %+v", routes[0])
	}
}

func TestRouteWindowCacheFirstBuild(t *testing.T) {
	t.Parallel()
	s := newExample(t)
	v := &s.vehicles[0]
	// Version 0 and start 0 are also the values of the zero cache.
	v.Route = []Lane{s.network.Lanes[0]}
	if v.routeVersion != 0 {
		t.Fatalf("version = %d", v.routeVersion)
	}
	if routes := checkWindows(t, s); len(routes[0].Lanes) != 1 {
		t.Fatalf("no window was built: %+v", routes[0])
	}
}

func TestRouteWindowCacheRefusesExhaustedIdentity(t *testing.T) {
	t.Parallel()
	s := newExample(t)
	v := &s.vehicles[0]
	lanes := s.network.Lanes
	v.routeVersion = MaxCounter - 2
	s.setVehicleRoute(v, []Lane{lanes[0], lanes[1]})
	checkWindows(t, s)
	// The version reaches the limit, and the next route cannot change it.
	s.setVehicleRoute(v, []Lane{lanes[1], lanes[0]})
	s.setVehicleRoute(v, []Lane{lanes[0], lanes[0]})
	if v.routeVersion != MaxCounter {
		t.Fatalf("version = %d", v.routeVersion)
	}
	if _, _, err := s.PresentationSnapshot(); err == nil {
		t.Fatal("served a window at the identity limit")
	}
}

func TestRouteWindowCacheLivesWithTheVehicle(t *testing.T) {
	t.Parallel()
	s := newExample(t)
	v := &s.vehicles[0]
	lanes := s.network.Lanes
	s.setVehicleRoute(v, []Lane{lanes[0], lanes[1]})
	before := checkWindows(t, s)

	clone := s.Clone()
	if &clone.vehicles[0].window.lanes[0] != &v.window.lanes[0] {
		t.Fatal("a clone did not share the window")
	}
	// The same version on each side holds another route.
	s.setVehicleRoute(v, []Lane{lanes[1], lanes[0]})
	clone.setVehicleRoute(&clone.vehicles[0], []Lane{lanes[1], lanes[1]})
	checkWindows(t, s)
	checkWindows(t, clone)
	rewound := clone.Clone()
	if routes := checkWindows(t, rewound); len(routes[0].Display) != 1 {
		t.Fatalf("clone display %v", routes[0].Display)
	}
	if !slices.Equal(before[0].Lanes, []int{s.graph.lanes[lanes[0].ID], s.graph.lanes[lanes[1].ID]}) {
		t.Fatalf("a later route wrote an earlier window: %v", before[0].Lanes)
	}

	s.Reset()
	if s.vehicles[0].window.lanes != nil {
		t.Fatal("reset kept a window")
	}
}

func TestRouteWindowCacheIsNotInSave(t *testing.T) {
	t.Parallel()
	s := newExample(t)
	s.setVehicleRoute(&s.vehicles[0], []Lane{s.network.Lanes[0], s.network.Lanes[1]})
	before := s.ExportState()
	checkWindows(t, s)
	if !reflect.DeepEqual(before, s.ExportState()) {
		t.Fatal("a window changed the saved state")
	}

	moving := newExample(t)
	if err := moving.RequestTrip("harbor", "market"); err != nil {
		t.Fatal(err)
	}
	for range 2 * TicksPerSecond {
		moving.Step()
	}
	if routes := checkWindows(t, moving); routes[0].Lanes == nil {
		t.Fatal("the pod has no route")
	}
	restored, _, err := RestoreState(RestoreStateInput{Network: moving.network, Fleet: moving.initial, State: moving.ExportState()})
	if err != nil {
		t.Fatal(err)
	}
	for i := range restored.vehicles {
		if restored.vehicles[i].window.lanes != nil {
			t.Fatal("a restore has a window")
		}
	}
	checkWindows(t, restored)
}

func TestBuildRouteWindowRefusesUnknownLane(t *testing.T) {
	t.Parallel()
	s := newExample(t)
	v := &s.vehicles[0]
	s.setVehicleRoute(v, []Lane{s.network.Lanes[0]})
	v.Route = []Lane{{ID: "unknown", From: s.network.Lanes[0].From}}
	v.routeVersion++
	if _, _, err := s.PresentationSnapshot(); err == nil || err.Error() != `unknown route lane "unknown"` {
		t.Fatalf("error = %v", err)
	}
	if v.window.lanes != nil {
		t.Fatal("a refused build reached the cache")
	}
}
