package sim

import (
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"
	"testing"
)

func TestForecastDischargeBoundaries(t *testing.T) {
	t.Parallel()
	f := laneForecast{initial: 6, arrivals: []plannedLaneArrival{{at: 90}, {at: 2}, {at: 2}, {at: 10}}}
	f.finish()
	for _, tc := range []struct{ at, delay float64 }{
		{0, 6}, {1, 5}, {2, 10}, {9, 3}, {10, 5}, {15, 0}, {89, 0}, {90, 3}, {93, 0}, {120, 0},
	} {
		if got := f.delay(tc.at); got != tc.delay {
			t.Errorf("entry %v: delay %v, want %v", tc.at, got, tc.delay)
		}
	}
	previous := -1.0
	for step := range 501 {
		at := float64(step) / 4
		exit := at + f.delay(at) + 7
		if exit < previous {
			t.Fatalf("exit time decreases at %v: %v to %v", at, previous, exit)
		}
		previous = exit
	}
}

func TestPredictiveRouteGuards(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		count  int
		detour float64
		want   string
	}{
		{"empty forecast", 0, 30, "fast-1"},
		{"material saving", 9, 30, "alternate-1"},
		{"small saving", 4, 30, "fast-1"},
		{"excessive detour", 9, 80, "fast-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := queueSimulation(queueNetwork(tc.detour), "fast-1", tc.count)
			if err := s.SetRoutingPolicy(PredictiveRouting); err != nil {
				t.Fatal(err)
			}
			route, err := s.assignedRoute(nil, "a", "d")
			if err != nil {
				t.Fatal(err)
			}
			if route[0].ID != tc.want {
				t.Fatalf("route %v, want first lane %s", routeIDs(route), tc.want)
			}
		})
	}
}

func plannedTestPod(s *Simulation, id string, activity Activity, phase int, distance float64) vehicle {
	var v vehicle
	v.Pod = Pod{ID: id, Activity: activity, LaneID: "fast-1", LaneDistance: distance}
	v.phaseTicks = phase
	s.setVehicleRoute(&v, s.network.Lanes[:2])
	return v
}

func TestPredictiveCountsCurrentLaneOnce(t *testing.T) {
	t.Parallel()
	s := &Simulation{networkIndexes: &networkIndexes{network: queueNetwork(30)}}
	s.ensureNetworkIndexes()
	v := plannedTestPod(s, "stopped", Traveling, 0, 50)
	v.Pod.WaitReason = TrackOccupied
	s.vehicles = append(s.vehicles, v)
	f := s.routeForecasts(nil)
	current, next := f[s.graph.lanes["fast-1"]], f[s.graph.lanes["fast-2"]]
	if current.initial != 3 || len(current.arrivals) != 0 || len(next.arrivals) != 1 || next.arrivals[0].at != 5 {
		t.Fatalf("current %+v, next %+v", current, next)
	}
	self := s.routeForecasts(&s.vehicles[0])
	for index, lane := range self {
		if lane.initial != 0 || len(lane.arrivals) != 0 {
			t.Fatalf("self counted on lane %d: %+v", index, lane)
		}
	}
}

func TestPredictiveUsesSameTickPlannedRoutes(t *testing.T) {
	t.Parallel()
	s := &Simulation{networkIndexes: &networkIndexes{network: queueNetwork(30)}}
	if err := s.SetRoutingPolicy(PredictiveRouting); err != nil {
		t.Fatal(err)
	}
	before, err := s.assignedRoute(nil, "a", "d")
	if err != nil || before[0].ID != "fast-1" {
		t.Fatalf("initial route %v: %v", routeIDs(before), err)
	}
	for index := range 12 {
		s.vehicles = append(s.vehicles, plannedTestPod(s, strconv.Itoa(index), DepartingEmpty, 0, 0))
	}
	after, err := s.assignedRoute(nil, "a", "d")
	if err != nil || after[0].ID != "alternate-1" {
		t.Fatalf("planned route %v: %v", routeIDs(after), err)
	}
	f := s.routeForecasts(nil)
	if len(f[s.graph.lanes["fast-1"]].arrivals) != 12 {
		t.Fatal("predeparture routes missing")
	}
	queues := slices.Clone(s.predictiveQueues)
	slices.Reverse(s.vehicles)
	reversed, err := s.assignedRoute(nil, "a", "d")
	if err != nil || !slices.Equal(routeIDs(after), routeIDs(reversed)) || !slices.Equal(queues, s.predictiveQueues) {
		t.Fatal("fleet order or repeated same-tick query changed forecast")
	}
}

func TestPredictiveHorizonAndPhaseDelay(t *testing.T) {
	t.Parallel()
	s := &Simulation{networkIndexes: &networkIndexes{network: queueNetwork(30)}}
	s.ensureNetworkIndexes()
	for _, tc := range []struct {
		activity Activity
		phase    int
		entries  int
	}{
		{Boarding, 90 * TicksPerSecond, 1},
		{Continuing, 90*TicksPerSecond + 1, 0},
		{DepartingEmpty, 89 * TicksPerSecond, 1},
		{Idle, 0, 0},
		{Unloading, 0, 0},
	} {
		v := plannedTestPod(s, "phase", tc.activity, tc.phase, 0)
		forecasts := make([]laneForecast, len(s.network.Lanes))
		s.addPlannedArrivals(forecasts, &v)
		first, second := forecasts[s.graph.lanes["fast-1"]], forecasts[s.graph.lanes["fast-2"]]
		if len(first.arrivals) != tc.entries || len(second.arrivals) != 0 {
			t.Fatalf("activity %s phase %d: %+v / %+v", tc.activity, tc.phase, first, second)
		}
	}
}

func TestPredictionSmoothingUsesElapsedTicks(t *testing.T) {
	t.Parallel()
	s := queueSimulation(queueNetwork(30), "fast-1", 10)
	s.ensureNetworkIndexes()
	lane := s.graph.lanes["fast-1"]
	s.predictionQueues()
	for index := range s.vehicles {
		s.vehicles[index].Pod.WaitReason = NoWait
	}
	s.predictionQueues()
	if s.predictiveQueues[lane] != 30 {
		t.Fatal("same-tick query changed history")
	}
	s.tick = 5 * TicksPerSecond
	s.predictionQueues()
	if math.Abs(s.predictiveQueues[lane]-30/math.E) > 1e-9 {
		t.Fatalf("five-second history %v", s.predictiveQueues[lane])
	}
	for range 10 {
		s.predictionQueues()
	}
	if math.Abs(s.predictiveQueues[lane]-30/math.E) > 1e-9 {
		t.Fatal("query count changed decay")
	}
	clone := s.Clone()
	clone.predictiveQueues[lane] = 900
	clone.predictivePodQueues["q00"].lanes[lane] = 900
	if s.predictiveQueues[lane] == 900 || s.predictivePodQueues["q00"].lanes[lane] == 900 {
		t.Fatal("clone shares mutable history")
	}
	if err := clone.SetRoutingPolicy(PredictiveRouting); err != nil {
		t.Fatal(err)
	}
	if clone.predictiveQueues != nil || clone.predictiveQueueTick != 0 || clone.predictivePodQueues != nil {
		t.Fatal("policy switch retains history")
	}
	s.Reset()
	if s.predictiveQueues != nil || s.predictiveQueueTick != 0 || s.predictivePodQueues != nil {
		t.Fatal("reset retains history")
	}
}

func TestPredictionMovingSuffixStartsAfterPrefix(t *testing.T) {
	t.Parallel()
	s := &Simulation{networkIndexes: &networkIndexes{network: queueNetwork(30)}}
	s.ensureNetworkIndexes()
	v := plannedTestPod(s, "moving", Traveling, 0, 50)
	before := v.blocks
	if got := s.predictionStart(&v, "b"); got != 5 {
		t.Fatalf("prefix arrival %v, want5", got)
	}
	if got := s.predictionStart(&v, "d"); got != 15 {
		t.Fatalf("longer prefix arrival %v, want15", got)
	}
	if !reflect.DeepEqual(before, v.blocks) {
		t.Fatal("prefix lookup mutates block cursors")
	}
}

func TestPredictiveSearchMatchesExhaustivePaths(t *testing.T) {
	t.Parallel()
	const size = 4
	id := func(x, y int) string { return fmt.Sprintf("%d-%d", x, y) }
	var network Network
	for x := range size {
		for y := range size {
			network.Nodes = append(network.Nodes, Node{ID: id(x, y), Position: Point{X: float64(x) * 100, Y: float64(y) * 100}})
			if x+1 < size {
				network.Lanes = append(network.Lanes, Lane{ID: id(x, y) + "e", From: id(x, y), To: id(x+1, y), SpeedLimit: 10})
			}
			if y+1 < size {
				network.Lanes = append(network.Lanes, Lane{ID: id(x, y) + "n", From: id(x, y), To: id(x, y+1), SpeedLimit: 10})
			}
		}
	}
	graph := newRouteGraph(network)
	for seed := range 20 {
		forecast := make([]laneForecast, len(network.Lanes))
		for index := range forecast {
			f := &forecast[index]
			f.initial = float64((index*7+seed*3)%11) * 3
			for event := range 4 {
				f.arrivals = append(f.arrivals, plannedLaneArrival{at: float64((index*13 + seed*7 + event*19) % 91)})
			}
			f.finish()
		}
		start := float64(seed * 3)
		route, err := network.routeIndexed(networkRouteInput{from: id(0, 0), to: id(size-1, size-1), forecasts: forecast, forecastStart: start}, graph)
		if err != nil {
			t.Fatal(err)
		}
		// This oracle walks each path and evaluates service events directly,
		// without using laneForecast.delay or the route search's heap.
		pathCost := func(path []int) float64 {
			at := start
			for _, lane := range path {
				until := forecast[lane].initial
				for _, event := range forecast[lane].arrivals {
					if event.at <= at {
						until = math.Max(until, event.at) + queueHeadwaySeconds
					}
				}
				at = math.Max(at, until) + graph.edges[lane].seconds
			}
			return at - start
		}
		var got []int
		for _, lane := range route {
			got = append(got, graph.lanes[lane.ID])
		}
		best := math.Inf(1)
		var walk func(int, []int)
		walk = func(node int, path []int) {
			if node == graph.nodes[id(size-1, size-1)] {
				best = math.Min(best, pathCost(path))
				return
			}
			for _, lane := range graph.outgoing[node] {
				walk(graph.edges[lane].to, append(slices.Clone(path), lane))
			}
		}
		walk(graph.nodes[id(0, 0)], nil)
		if math.Abs(pathCost(got)-best) > 1e-9 {
			t.Fatalf("seed %d: got%v, best%v", seed, pathCost(got), best)
		}
	}
}

func TestPredictiveBerthPreferenceAndFallback(t *testing.T) {
	t.Parallel()
	for _, fallback := range []bool{false, true} {
		t.Run(strconv.FormatBool(fallback), func(t *testing.T) {
			t.Parallel()
			network := berthGuardNetwork()
			if fallback {
				network.Lanes = slices.DeleteFunc(network.Lanes, func(l Lane) bool { return l.ID == "b-through" })
			}
			s := queueSimulation(network, "b-in", 20)
			if err := s.SetRoutingPolicy(PredictiveRouting); err != nil {
				t.Fatal(err)
			}
			route, err := s.assignedRoute(nil, "a-berth", "c-berth")
			if err != nil {
				t.Fatal(err)
			}
			if got := slices.Contains(routeIDs(route), "b-in"); got != fallback {
				t.Fatalf("route%v fallback%v", routeIDs(route), fallback)
			}
		})
	}
}

func TestPredictiveDiversionPreservesCommitment(t *testing.T) {
	t.Parallel()
	s, err := New(Example(), "market")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetRoutingPolicy(PredictiveRouting); err != nil {
		t.Fatal(err)
	}
	v := s.findVehicle("01")
	if !s.park(v) {
		t.Fatal("cannot start parking")
	}
	for range 90 * TicksPerSecond {
		s.Step()
		if v.Pod.LaneID == "return-start" && v.Pod.LaneDistance > 40 {
			break
		}
	}
	if v.Pod.LaneID != "return-start" {
		t.Fatal("moving diversion point not reached")
	}
	before := v.Pod
	reserved := slices.Clone(v.blocks.all()[:v.reservedThrough+1])
	if err := s.RequestTrip("harbor", "garden"); err != nil {
		t.Fatal(err)
	}
	if v.RelocatingTo != "harbor" {
		t.Fatal("pod not diverted")
	}
	if v.Pod.Position != before.Position || v.Pod.Speed != before.Speed || v.Pod.LaneDistance != before.LaneDistance || v.Pod.LaneID != before.LaneID {
		t.Fatal("changed current motion")
	}
	if !reflect.DeepEqual(reserved, v.blocks.all()[:len(reserved)]) {
		t.Fatal("changed committed blocks")
	}
	for range 400 * TicksPerSecond {
		s.Step()
		checkTraffic(t, s.Snapshot())
		if s.completed == 1 {
			break
		}
	}
	if s.completed != 1 || v.Pod.StationID != "garden" {
		t.Fatal("diverted request did not finish")
	}
}

func TestPredictiveExcludesHistoricalSelfOnly(t *testing.T) {
	t.Parallel()
	for _, newlyStopped := range []bool{false, true} {
		t.Run(strconv.FormatBool(newlyStopped), func(t *testing.T) {
			t.Parallel()
			s := queueSimulation(queueNetwork(30), "fast-1", 1)
			s.ensureNetworkIndexes()
			lane := s.graph.lanes["fast-1"]
			s.routeForecasts(nil)
			s.vehicles[0].Pod.WaitReason = NoWait
			s.tick++
			self := &s.vehicles[0]
			want := 0.0
			if newlyStopped {
				self = &s.vehicles[1]
				self.Pod.Speed = 0
				self.Pod.WaitReason = TrackOccupied
				want = queueHeadwaySeconds * math.Exp(-1.0/(TicksPerSecond*predictionDecaySeconds))
			}
			f := s.routeForecasts(self)
			if math.Abs(f[lane].initial-want) > 1e-9 {
				t.Fatalf("self-excluded history %v, want%v", f[lane].initial, want)
			}
		})
	}
}
