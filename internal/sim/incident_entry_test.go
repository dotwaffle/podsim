package sim

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"testing"
)

// routeMemos holds copies of each cache and counter that a route search can
// write, and the saved state.
type routeMemos struct {
	routes, staticRoutes, accessRoutes map[routeKey]routeCopy
	staticConnected                    map[routeKey]bool
	routeOrder                         []routeKey
	routeStations                      map[stopKey][]string
	approachStations                   map[string][]string
	lengths                            map[string]float64
	pickupBounds                       map[string][]float64
	pickupMisses                       map[pickupMiss]bool
	emergencyMisses                    []emergencyMiss
	searches                           searchCounters
	faults                             faultCounters
	work                               *routeSearchWork
	workCopy                           routeSearchWork
	view                               *routeView
	blocked                            blockedSet
	rerouteDue                         bool
	state                              SavedState
}

// routeCopy is a route result, with the text of its error.
type routeCopy struct {
	lanes []Lane
	err   string
}

func copyRoutes(routes map[routeKey]routeResult) map[routeKey]routeCopy {
	copies := make(map[routeKey]routeCopy, len(routes))
	for key, result := range routes {
		copies[key] = routeCopy{lanes: slices.Clone(result.lanes), err: fmt.Sprint(result.err)}
	}
	return copies
}

func memosOf(s *Simulation) routeMemos {
	memos := routeMemos{
		routes: copyRoutes(s.routes), staticRoutes: copyRoutes(s.staticRoutes), accessRoutes: copyRoutes(s.accessRoutes),
		staticConnected: maps.Clone(s.staticConnected), routeOrder: slices.Clone(s.routeOrder), routeStations: maps.Clone(s.routeStations),
		approachStations: maps.Clone(s.approachStations), lengths: maps.Clone(s.lengths), pickupBounds: maps.Clone(s.pickupBounds),
		pickupMisses: maps.Clone(s.pickupMisses), emergencyMisses: slices.Clone(s.emergencyMisses), searches: s.searchCounters,
		faults: s.faultCounters, work: s.routeWork, view: s.routeView, blocked: s.blocked, rerouteDue: s.rerouteDue, state: s.ExportState(),
	}
	if s.routeWork != nil {
		memos.workCopy = *s.routeWork
		work := &memos.workCopy
		work.distance, work.previous, work.visited = slices.Clone(work.distance), slices.Clone(work.previous), slices.Clone(work.visited)
		work.queue, work.touched, work.path = slices.Clone(work.queue), slices.Clone(work.touched), slices.Clone(work.path)
	}
	return memos
}

// TestBoardingLegBlockedChangesNothing compares the state, the memos, and the
// counters before and after the call, with full caches and after a new route
// epoch, when each search of the entry misses the cache. It also checks that a
// run with a call at each tick gives the state of a run with no call.
func TestBoardingLegBlockedChangesNothing(t *testing.T) {
	t.Parallel()
	s, _, _, _ := yieldFleet(t, false)
	check := func(name string) {
		t.Helper()
		before := memosOf(s)
		if !s.BoardingLegBlockedForTest("02") {
			t.Fatalf("%s: the leg of pod 02 does not count as blocked", name)
		}
		if after := memosOf(s); !reflect.DeepEqual(after, before) {
			t.Fatalf("%s: the call changes the simulation:\n%+v\nthen\n%+v", name, before, after)
		}
	}
	check("full caches")
	s.startRouteEpoch()
	s.rerouteDue = false
	if len(s.routes) != 0 {
		t.Fatalf("the new epoch keeps %d routes", len(s.routes))
	}
	check("empty caches")

	plain, probed := s.Clone(), s.Clone()
	plain.monitor, probed.monitor = nil, nil
	for range 600 {
		plain.Step()
		probed.Step()
		probed.BoardingLegBlockedForTest("02")
	}
	if got, want := probed.ExportState(), plain.ExportState(); !reflect.DeepEqual(got, want) {
		t.Fatalf("the calls change the run:\n%+v\nthen\n%+v", want, got)
	}
}

// TestBoardingLegBlocked checks each case of the entry. Only an idle pod whose
// named trip fails its leg now, and succeeds with an empty blocked set, counts.
func TestBoardingLegBlocked(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		edit func(t *testing.T, s *Simulation, debris string)
		pod  string
		want bool
	}{
		{name: "leg blocked by the fault", pod: "02", want: true},
		{name: "no such pod", pod: "missing"},
		{name: "pod not idle", pod: "02", edit: func(_ *testing.T, s *Simulation, _ string) {
			s.findVehicle("02").Pod.Activity = Unloading
		}},
		{name: "no trip names the pod", pod: "02", edit: func(_ *testing.T, s *Simulation, _ string) {
			s.waiting[0].request.PodID = ""
		}},
		{name: "trip at another station", pod: "02", edit: func(_ *testing.T, s *Simulation, _ string) {
			s.waiting[0].request.LegFrom = "s2"
		}},
		{name: "open leg", pod: "02", edit: func(_ *testing.T, s *Simulation, _ string) {
			s.waiting[0].request.To = "s2"
		}},
		{name: "leg fails with an empty blocked set too", pod: "02", edit: func(_ *testing.T, s *Simulation, _ string) {
			s.waiting[0].request.To = "missing"
		}},
		{name: "no active fault", pod: "02", edit: func(t *testing.T, s *Simulation, debris string) {
			t.Helper()
			if err := s.clearFault(debris); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, _, _, debris := yieldFleet(t, false)
			s.monitor = nil
			if test.edit != nil {
				test.edit(t, s, debris)
			}
			if got := s.BoardingLegBlockedForTest(test.pod); got != test.want {
				t.Fatalf("got %t, want %t", got, test.want)
			}
		})
	}
}

// TestEmptyMoveBlocked checks each case of the entry. Only an idle pod that no
// trip names, whose empty move fails now and succeeds with an empty blocked set,
// counts.
func TestEmptyMoveBlocked(t *testing.T) {
	t.Parallel()
	const free, full = "free", "full"
	tests := []struct {
		name  string
		fleet string
		lane  string
		edit  func(s *Simulation)
		pod   string
		want  bool
	}{
		{name: "the fault keeps the pod in", fleet: free, lane: "s1-1-out", pod: "01", want: true},
		{name: "no such pod", fleet: free, lane: "s1-1-out", pod: "missing"},
		{name: "pod not idle", fleet: free, lane: "s1-1-out", pod: "01", edit: func(s *Simulation) { s.findVehicle("01").Pod.Activity = Unloading }},
		{name: "pod occupied", fleet: free, lane: "s1-1-out", pod: "01", edit: func(s *Simulation) { s.findVehicle("01").Pod.Occupied = true }},
		{name: "a trip names the pod", fleet: free, lane: "s1-1-out", pod: "01", edit: func(s *Simulation) {
			trip := newTrip(s, "s1", "s0")
			trip.request.PodID = "01"
			s.waiting = append(s.waiting, trip)
		}},
		{name: "the pod has a move past the fault", fleet: free, lane: "s0-link", pod: "01"},
		{name: "no move with an empty blocked set too", fleet: full, lane: "s1-1-out", pod: "02"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			berths := []string{"s1-1"}
			pod := test.pod
			if test.fleet == full {
				berths = []string{"p-1", "s1-1", "s0-1", "s2-1"}
			}
			s, err := NewFleet(lineNetwork(lineStations(1, 1, 1, 1)), place(berths...))
			if err != nil {
				t.Fatal(err)
			}
			if err := s.SetFaults(true, FaultSettings{EvacuationSeconds: 300}); err != nil {
				t.Fatal(err)
			}
			debris := startDebris(t, s, test.lane, 40, 44, 0)
			if test.edit != nil {
				test.edit(s)
			}
			before := memosOf(s)
			if got := s.EmptyMoveBlockedForTest(pod); got != test.want {
				t.Fatalf("got %t, want %t", got, test.want)
			}
			if after := memosOf(s); !reflect.DeepEqual(after, before) {
				t.Fatalf("the call changes the simulation:\n%+v\nthen\n%+v", before, after)
			}
			if test.want {
				if err := s.clearFault(debris); err != nil {
					t.Fatal(err)
				}
				if s.EmptyMoveBlockedForTest(pod) {
					t.Fatal("the pod counts as blocked with no active fault")
				}
			}
		})
	}
}
