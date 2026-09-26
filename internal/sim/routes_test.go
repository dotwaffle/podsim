package sim

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"testing"
)

func TestCachedRoutesMatchNetwork(t *testing.T) {
	t.Parallel()
	simulation, err := New(Example(), "harbor")
	if err != nil {
		t.Fatal(err)
	}
	for _, from := range simulation.network.Nodes {
		for _, to := range simulation.network.Nodes {
			want, wantErr := simulation.network.Route(from.ID, to.ID)
			for range 2 {
				got, gotErr := simulation.route(from.ID, to.ID)
				if !reflect.DeepEqual(got, want) || !errors.Is(gotErr, wantErr) {
					t.Fatalf("route %s to %s differs: %v", from.ID, to.ID, gotErr)
				}
			}
		}
	}
	simulation.Reset()
	if err := simulation.RequestJourney("01", "market"); err != nil {
		t.Fatal(err)
	}
	snapshot := simulation.Snapshot()
	snapshot.Vehicles[0].Route[0].To = "changed"
	state := simulation.Snapshot()
	if state.Vehicles[0].Route[0].To == "changed" {
		t.Fatal("snapshot changed cached route")
	}
}

func TestRouteCacheCapacity(t *testing.T) {
	t.Parallel()
	simulation, err := New(Example(), "harbor")
	if err != nil {
		t.Fatal(err)
	}
	simulation.routes = make(map[routeKey]routeResult)
	for index := range routeCacheLimit {
		simulation.routes[routeKey{from: string(rune(index)), to: "unused"}] = routeResult{}
	}
	want, wantErr := simulation.network.Route("harbor-berth", "market-berth")
	got, gotErr := simulation.route("harbor-berth", "market-berth")
	if !reflect.DeepEqual(got, want) || !errors.Is(gotErr, wantErr) {
		t.Fatal("eviction changed route")
	}
	if len(simulation.routes) > routeCacheLimit {
		t.Fatal("route cache exceeded limit")
	}
}

func TestRouteCacheEvictsOldestEntry(t *testing.T) {
	t.Parallel()
	simulation, err := New(Example(), "harbor")
	if err != nil {
		t.Fatal(err)
	}
	simulation.routes = make(map[routeKey]routeResult)
	for index := range routeCacheLimit {
		key := routeKey{from: fmt.Sprintf("from-%d", index), to: "destination"}
		simulation.cacheRoute(key, routeResult{})
	}
	oldest := routeKey{from: "from-0", to: "destination"}
	newest := routeKey{from: "new", to: "destination"}
	simulation.cacheRoute(newest, routeResult{})
	if _, exists := simulation.routes[oldest]; exists {
		t.Fatal("oldest route remained after capacity eviction")
	}
	if _, exists := simulation.routes[newest]; !exists {
		t.Fatal("new route missing after capacity eviction")
	}
	if len(simulation.routes) != routeCacheLimit {
		t.Fatalf("route cache size = %d, want %d", len(simulation.routes), routeCacheLimit)
	}
}

func TestStationRoutesFillRouteCache(t *testing.T) {
	t.Parallel()
	for _, congestion := range []bool{false, true} {
		simulation, err := New(ladderNetwork(), "harbor")
		if err != nil {
			t.Fatal(err)
		}
		simulation.SetCongestionRouting(congestion)
		filled := 0
		for _, node := range simulation.network.Nodes {
			for _, station := range simulation.network.Stations {
				simulation.cacheStationRoutes(node.ID, station.Berths)
				for _, berth := range station.Berths {
					cached, ok := simulation.routes[routeKey{from: node.ID, to: berth.Node}]
					if !ok {
						continue
					}
					want, wantErr := simulation.network.Route(node.ID, berth.Node)
					if !reflect.DeepEqual(cached.lanes, want) || fmt.Sprint(cached.err) != fmt.Sprint(wantErr) {
						t.Fatalf("cached route %s to %s = %v, %v, want %v, %v", node.ID, berth.Node, cached.lanes, cached.err, want, wantErr)
					}
					filled++
				}
			}
		}
		if congestion && (filled > 0 || simulation.congestionRouteCosts != nil) {
			t.Fatalf("congestion routing filled %d routes or refreshed its costs", filled)
		}
		if !congestion && filled == 0 {
			t.Fatal("no station route in the cache")
		}
	}
}

// checkRouteLengths checks that each pod has the value of laneLength for
// each lane of its route.
func checkRouteLengths(t *testing.T, s *Simulation) {
	t.Helper()
	for index := range s.vehicles {
		v := &s.vehicles[index]
		if len(v.routeLengths) != len(v.Route) {
			t.Fatalf("tick %d: pod %s has %d route lengths for %d lanes", s.tick, v.Pod.ID, len(v.routeLengths), len(v.Route))
		}
		for lane, length := range v.routeLengths {
			if want := s.laneLength(v.Route[lane]); math.Float64bits(length) != math.Float64bits(want) {
				t.Fatalf("tick %d: pod %s lane %s has length %v, want %v", s.tick, v.Pod.ID, v.Route[lane].ID, length, want)
			}
		}
	}
}

func TestRouteLengthsFollowPodRoutes(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(curvedExample(), []Placement{
		{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"}, {ID: "03", StationID: "parking", BerthID: "parking-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	s.SetRedistribution(true)
	if err := s.SetDemandWeights(map[string]float64{"harbor": 2, "garden": 1, "market": 1}); err != nil {
		t.Fatal(err)
	}
	trips := map[int64][2]string{
		0: {"harbor", "market"}, 1: {"garden", "market"}, 40 * TicksPerSecond: {"market", "garden"},
		60 * TicksPerSecond: {"garden", "harbor"}, 90 * TicksPerSecond: {"harbor", "market"},
	}
	routed := 0
	for range 240 * TicksPerSecond {
		if trip, ok := trips[s.tick]; ok {
			if err := s.RequestTrip(trip[0], trip[1]); err != nil {
				t.Fatal(err)
			}
		}
		s.Step()
		checkRouteLengths(t, s)
		for index := range s.vehicles {
			if len(s.vehicles[index].Route) > 0 {
				routed++
			}
		}
	}
	if routed == 0 || s.completed == 0 {
		t.Fatalf("the pods completed %d trips in %d pod ticks on a route", s.completed, routed)
	}
}

func TestRouteLengthsFollowTerminalBerthChoice(t *testing.T) {
	t.Parallel()
	s := berthChoiceSimulation(t)
	v := s.findVehicle("01")
	addMarketBerth(s)
	s.owners[resource{kind: berthResource, id: "market-1"}] = "02"
	s.owners[resource{kind: nodeResource, id: "market-berth"}] = "02"
	positionBeforeTerminalInlet(t, terminalInletPosition{simulation: s, vehicle: v})
	checkRouteLengths(t, s)

	s.admit()

	if v.Route[len(v.Route)-1].ID != "market-in-2" {
		t.Fatalf("the pod did not change its inlet: %s", v.Route[len(v.Route)-1].ID)
	}
	checkRouteLengths(t, s)
}

func TestRouteSecondsWithLengths(t *testing.T) {
	t.Parallel()
	s, err := New(curvedExample(), "harbor")
	if err != nil {
		t.Fatal(err)
	}
	for _, from := range s.network.Nodes {
		for _, to := range s.network.Nodes {
			route, err := s.route(from.ID, to.ID)
			if err != nil || len(route) == 0 {
				continue
			}
			_, lengths := s.routeBlocks(route)
			// Test the distances at the lane ends and near them, where
			// routeSeconds skips a lane or starts in it.
			distances := []float64{0, 1}
			end := 0.0
			for _, length := range lengths {
				end += length
				distances = append(distances, end-1e-9, end, end+1e-9, end-length/2)
			}
			for _, distance := range distances {
				for _, speed := range []float64{0, 3, 14, 20} {
					motion := motionEstimate{distance: distance, speed: speed}
					want := s.routeSeconds(route, motion)
					if got := s.routeSecondsWith(route, lengths, motion); math.Float64bits(got) != math.Float64bits(want) {
						t.Fatalf("route %s to %s motion %+v: got %v, want %v", from.ID, to.ID, motion, got, want)
					}
				}
			}
		}
	}
}

// TestRouteCacheKeepsTravelTime checks that each cached route holds the
// travel time of its lanes for a pod at rest, and that emptySeconds gives
// that time.
func TestRouteCacheKeepsTravelTime(t *testing.T) {
	t.Parallel()
	for _, congestion := range []bool{false, true} {
		s, err := NewFleet(curvedExample(), []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"}})
		if err != nil {
			t.Fatal(err)
		}
		s.SetCongestionRouting(congestion)
		if err := s.RequestJourney("01", "market"); err != nil {
			t.Fatal(err)
		}
		for step := range 3 {
			advance(s, 30*TicksPerSecond*step)
			for _, from := range s.network.Nodes {
				for _, to := range s.network.Nodes {
					want := math.Inf(1)
					if route, err := s.route(from.ID, to.ID); err == nil {
						want = s.routeSeconds(route, motionEstimate{})
					}
					if got := s.emptySeconds(from.ID, to.ID); math.Float64bits(got) != math.Float64bits(want) {
						t.Fatalf("congestion %v: emptySeconds %s to %s = %v, want %v", congestion, from.ID, to.ID, got, want)
					}
				}
			}
			cached := 0
			for _, routes := range []map[routeKey]routeResult{s.routes, s.congestionRoutes} {
				for key, result := range routes {
					want := s.routeSeconds(result.lanes, motionEstimate{})
					if !result.timed || math.Float64bits(result.seconds) != math.Float64bits(want) {
						t.Fatalf("congestion %v: route %+v has time %v (%v), want %v", congestion, key, result.seconds, result.timed, want)
					}
					cached++
				}
			}
			if cached == 0 {
				t.Fatalf("congestion %v: no cached route", congestion)
			}
		}
	}
}
