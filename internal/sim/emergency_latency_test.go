package sim

import (
	"errors"
	"fmt"
	"maps"
	"math/rand/v2"
	"runtime"
	"slices"
	"testing"
	"time"
)

// choiceTickLimit is the threshold of the worst choice tick on the CI
// runner class (product choice P23 of the incident emergency contract).
const choiceTickLimit = 50 * time.Millisecond

// streetGrid returns a one-way street grid of columns by rows nodes, 100 m
// apart, as section 9.1 of the incident emergency contract describes it.
// Rows alternate direction. A vertical street runs on each even column
// and on each extra column, with alternating direction. The speeds are
// 14, 14, and 10 m/s in turn.
func streetGrid(columns, rows int, extra ...int) Network {
	var network Network
	node := func(column, row int) string { return fmt.Sprintf("g%d-%d", column, row) }
	for row := range rows {
		for column := range columns {
			network.Nodes = append(network.Nodes, Node{ID: node(column, row), Position: Point{X: float64(column) * 100, Y: float64(row) * 100}})
		}
	}
	lane := func(from, to string) {
		speed := []float64{14, 14, 10}[len(network.Lanes)%3]
		network.Lanes = append(network.Lanes, Lane{ID: from + ">" + to, From: from, To: to, SpeedLimit: speed})
	}
	for row := range rows {
		for column := range columns - 1 {
			if row%2 == 0 {
				lane(node(column, row), node(column+1, row))
			} else {
				lane(node(column+1, row), node(column, row))
			}
		}
	}
	streets := 0
	for column := range columns {
		if column%2 != 0 && !slices.Contains(extra, column) {
			continue
		}
		for row := range rows - 1 {
			if streets%2 == 0 {
				lane(node(column, row), node(column, row+1))
			} else {
				lane(node(column, row+1), node(column, row))
			}
		}
		streets++
	}
	return network
}

// BenchmarkEmergencyLimitSearch repeats the search measurement of section
// 9.1 of the incident emergency contract on the grid of 50 by 100 nodes
// with 7,375 lanes: one search from a corner to the far side, with
// fresh and with reused work arrays. The unreachable case blocks the
// lanes into the target, so the search visits every node that it can
// reach, as the pruning tree does.
func BenchmarkEmergencyLimitSearch(b *testing.B) {
	network := streetGrid(50, 100)
	graph := newRouteGraph(network)
	if len(network.Nodes) != 5000 || len(network.Lanes) != 7375 {
		b.Fatalf("%d nodes and %d lanes", len(network.Nodes), len(network.Lanes))
	}
	// The node of the far corner has no lane into it, because row 99 runs
	// to the left and column 49 has no street, so the target is the last
	// node of the last vertical street.
	input := networkRouteInput{from: "g0-0", to: "g48-99"}
	blocked := make([]bool, len(network.Lanes))
	for index, lane := range network.Lanes {
		blocked[index] = lane.To == input.to
	}
	for _, reachable := range []bool{true, false} {
		for _, reused := range []bool{false, true} {
			b.Run(fmt.Sprintf("reachable=%t/reused=%t", reachable, reused), func(b *testing.B) {
				search := graph
				if !reachable {
					search.blocked = blocked
				}
				var work routeSearchWork
				for b.Loop() {
					var err error
					if reused {
						_, err = network.routeIndexedWithWork(input, search, &work)
					} else {
						_, err = network.routeIndexed(input, search)
					}
					if (err == nil) != reachable {
						b.Fatalf("reachable %t: %v", reachable, err)
					}
				}
			})
		}
	}
}

// delayedNetwork returns the network of the delayed-routes fixture at the
// project limits: a street grid of 41 by 100 nodes with four more vertical
// streets, and 300 stations with one berth each, one in each of 300 grid
// cells, entered from the lower left corner of the cell and left to the
// lower right corner. It has 5,000 nodes and 7,975 lanes.
func delayedNetwork() Network {
	network := streetGrid(41, 100, 1, 11, 21, 31)
	for index := range 300 {
		column, row := index%40, 6+12*(index/40)
		id := fmt.Sprintf("d%d", index)
		x, y := float64(column)*100, float64(row)*100
		station := Station{ID: id, Name: id, Entry: id + "-entry", Exit: id + "-exit", Berths: []Berth{{ID: id + "-1", Node: id + "-1"}}}
		network.Nodes = append(network.Nodes,
			Node{ID: station.Entry, Position: Point{X: x + 20, Y: y + 30}},
			Node{ID: station.Exit, Position: Point{X: x + 80, Y: y + 30}},
			Node{ID: id + "-1", Position: Point{X: x + 50, Y: y + 65}})
		network.Lanes = append(network.Lanes,
			Lane{ID: id + "-through", From: station.Entry, To: station.Exit, SpeedLimit: 14},
			Lane{ID: id + "-1-in", From: station.Entry, To: id + "-1", SpeedLimit: 14},
			Lane{ID: id + "-1-out", From: id + "-1", To: station.Exit, SpeedLimit: 14},
			Lane{ID: "to-" + id, From: fmt.Sprintf("g%d-%d", column, row), To: station.Entry, SpeedLimit: 14},
			Lane{ID: "from-" + id, From: station.Exit, To: fmt.Sprintf("g%d-%d", column+1, row), SpeedLimit: 14})
		network.Stations = append(network.Stations, station)
	}
	return network
}

// missNetwork returns the network of the no-candidate fixture at the
// project limits: 300 stations with one bank of 10 berths each, in a line
// along the x axis, in which passenger stations and parking stations
// alternate. Each station leaves to a node from which the next station is
// entered, and a road of 500 lanes returns from the last station to the
// first. It has 5,000 nodes and 8,000 lanes. When the through lanes of the
// parking stations are blocked, no route search leaves a bank: a road to
// another station passes the berths of a parking station, which the bank
// rules forbid, and which the pruning tree allows.
func missNetwork() Network {
	var network Network
	node := func(id string, x, y float64) {
		network.Nodes = append(network.Nodes, Node{ID: id, Position: Point{X: x, Y: y}})
	}
	lane := func(id, from, to, station string, role StationLaneRole) {
		network.Lanes = append(network.Lanes, Lane{ID: id, From: from, To: to, SpeedLimit: 14, StationID: station, StationRole: role})
	}
	const stations, berths = 300, 10
	previous := "return-end"
	for index := range stations {
		id := fmt.Sprintf("m%d", index)
		x := float64(index) * 1000
		entry, arrival, departure, exit, after := id+"-entry", id+"-arrival", id+"-departure", id+"-exit", id+"-after"
		node(entry, x, 0)
		node(arrival, x+60, 80)
		node(departure, x+540, 80)
		node(exit, x+600, 0)
		node(after, x+800, 0)
		lane(id+"-in", previous, entry, id, StationEntryRole)
		lane(id+"-through", entry, exit, id, StationThroughRole)
		lane(id+"-arrive", entry, arrival, id, StationBerthAccessRole)
		lane(id+"-depart", departure, exit, id, StationDepartureRole)
		lane(id+"-out", exit, after, id, StationExitRole)
		station := Station{ID: id, Name: id, Entry: entry, Exit: exit, ParkingOnly: index%2 == 1}
		var ids []string
		for berth := range berths {
			berthID := fmt.Sprintf("%s-%d", id, berth+1)
			node(berthID, x+300, 120+60*float64(berth))
			lane(berthID+"-in", arrival, berthID, id, StationBerthAccessRole)
			lane(berthID+"-out", berthID, departure, id, StationDepartureRole)
			station.Berths = append(station.Berths, Berth{ID: berthID, Node: berthID})
			ids = append(ids, berthID)
		}
		station.Banks = []StationBank{{ID: "a", Entry: entry, Exit: exit, BerthIDs: ids}}
		network.Stations = append(network.Stations, station)
		previous = after
	}
	// The return road starts at the node after the last station.
	end := float64(stations-1)*1000 + 800
	const returns = 499
	for index := range returns {
		id := fmt.Sprintf("return-%d", index)
		node(id, end-(end+200)*float64(index)/returns, -1000)
		lane(id, previous, id, "", "")
		previous = id
	}
	node("return-end", -200, 0)
	lane("return-close", previous, "return-end", "", "")
	return network
}

// choiceTickFixture is a simulation with four occupied pods that travel
// and can divert, ready for four emergencies on one tick. cold reports
// that the simulation must have no route memo and no routing-policy state.
type choiceTickFixture struct {
	s    *Simulation
	pods []string
	cold bool
}

// newChoiceTickFixture places a pod at each berth of the passenger
// stations of the network, up to the fleet limit, with the routing
// policy. When warm is true, it first runs 600 ticks of traffic, with a
// trip between random passenger stations every 20 ticks. It then makes
// four idle pods board for another passenger station, and steps until the
// four pods travel and can divert. When warm is false, it returns a clone
// with no route memo and no routing-policy state. ready can refuse a
// tick at which the pods can divert.
func newChoiceTickFixture(t *testing.T, network Network, policy RoutingPolicy, warm bool, ready func(*Simulation, *vehicle) bool) choiceTickFixture {
	t.Helper()
	var fleet []Placement
	var passenger []string
	for _, station := range network.Stations {
		if station.ParkingOnly {
			continue
		}
		passenger = append(passenger, station.ID)
		for _, berth := range station.Berths {
			if len(fleet) < 300 {
				fleet = append(fleet, Placement{ID: fmt.Sprintf("%03d", len(fleet)+1), StationID: station.ID, BerthID: berth.ID})
			}
		}
	}
	s, err := NewFleet(network, fleet)
	if err != nil {
		t.Fatal(err)
	}
	s.incidentContract, s.emergenciesOn = IncidentV1Contract, true
	if err := s.SetRoutingPolicy(policy); err != nil {
		t.Fatal(err)
	}
	rng := rand.New(rand.NewPCG(uint64(len(network.Nodes)), uint64(policy)))
	if warm {
		for tick := range 600 {
			if tick%20 == 0 {
				from, to := passenger[rng.IntN(len(passenger))], passenger[rng.IntN(len(passenger))]
				if from != to {
					_ = s.RequestTrip(from, to)
				}
			}
			s.Step()
		}
	}
	var idle, pods []*vehicle
	for index := range s.vehicles {
		if v := &s.vehicles[index]; v.Pod.Activity == Idle && !s.assigned(v.Pod.ID) {
			idle = append(idle, v)
		}
	}
	for index := range min(4, len(idle)) {
		v := idle[index*len(idle)/4]
		station := slices.Index(passenger, v.Pod.StationID)
		to := passenger[(station+1+len(passenger)/2)%len(passenger)]
		if err := s.board(v, newTrip(s, v.Pod.StationID, to)); err != nil {
			t.Fatal(err)
		}
		pods = append(pods, v)
	}
	if len(pods) != 4 {
		t.Fatalf("%d idle pods board", len(pods))
	}
	stepUntil(t, s, "four divertible pods", func() bool {
		for _, v := range pods {
			if _, _, ok := s.divertStart(v); !ok || v.Pod.Activity != Traveling || ready != nil && !ready(s, v) {
				return false
			}
		}
		return true
	})
	fixture := choiceTickFixture{s: s, cold: !warm}
	for _, v := range pods {
		fixture.pods = append(fixture.pods, v.Pod.ID)
	}
	if !warm {
		fixture.s = s.Clone()
		if err := fixture.s.SetRoutingPolicy(policy); err != nil {
			t.Fatal(err)
		}
	}
	return fixture
}

// copyWarm returns a clone of s with copies of the route memo and the
// static caches, which Clone drops.
func copyWarm(s *Simulation) *Simulation {
	c := s.Clone()
	c.routes, c.routeOrder = cloneRoutes(s.routes), slices.Clone(s.routeOrder)
	c.staticConnected, c.staticRoutes = maps.Clone(s.staticConnected), cloneRoutes(s.staticRoutes)
	return c
}

// choiceTick is the measurement of one choice tick: the slowest, the
// median, and the fastest time of three runs, and the search counts of
// one run.
type choiceTick struct {
	median, fastest, slowest time.Duration
	counts                   searchCounters
}

// measureChoiceTick starts an emergency on each pod of the fixture at one
// command boundary, three times from copies of one state, and measures
// the time of the four starts. Each start makes a station choice. check
// tests each run after the starts. A cold fixture must be cold before
// each run.
func measureChoiceTick(t *testing.T, fixture choiceTickFixture, check func(*Simulation) error) choiceTick {
	t.Helper()
	var times []time.Duration
	var result choiceTick
	for range 3 {
		s := copyWarm(fixture.s)
		if fixture.cold {
			if err := coldState(s); err != nil {
				t.Fatal(err)
			}
		}
		before := s.searchCounters
		runtime.GC()
		start := time.Now()
		for _, id := range fixture.pods {
			if _, err := s.Emergency(id, 0); err != nil {
				t.Fatal(err)
			}
		}
		times = append(times, time.Since(start))
		if err := check(s); err != nil {
			t.Fatal(err)
		}
		result.counts = subtractCounters(s.searchCounters, before)
	}
	slices.Sort(times)
	result.median, result.fastest, result.slowest = times[1], times[0], times[2]
	return result
}

// coldState reports an error when s has a route memo, a static cache, or
// routing-policy state.
func coldState(s *Simulation) error {
	if len(s.routes) != 0 || len(s.routeOrder) != 0 || len(s.staticConnected) != 0 || len(s.staticRoutes) != 0 {
		return fmt.Errorf("the cold fixture has %d memo routes, %d static connections, and %d static routes", len(s.routes), len(s.staticConnected), len(s.staticRoutes))
	}
	if s.congestionRouteCosts != nil || len(s.congestionRoutes) != 0 || s.nextCongestionRouteRefresh != 0 ||
		s.predictiveQueues != nil || s.predictivePodQueues != nil || s.predictiveQueueTick != 0 {
		return errors.New("the cold fixture has routing-policy state")
	}
	return nil
}

// subtractCounters returns the counts of a minus the counts of b.
func subtractCounters(a, b searchCounters) searchCounters {
	return searchCounters{
		graph: a.graph - b.graph, static: a.static - b.static, localFailed: a.localFailed - b.localFailed,
		routes: a.routes - b.routes, failed: a.failed - b.failed, congestion: a.congestion - b.congestion,
		queue: a.queue - b.queue, predictive: a.predictive - b.predictive, forecasts: a.forecasts - b.forecasts,
		trees: a.trees - b.trees, choices: a.choices - b.choices, memoHits: a.memoHits - b.memoHits,
	}
}

// allBound reports an error when a pod of the fixture is not bound.
func allBound(fixture choiceTickFixture) func(*Simulation) error {
	return func(s *Simulation) error {
		for _, id := range fixture.pods {
			if v := s.findVehicle(id); v.op.purpose != opEmergencyUnload {
				return fmt.Errorf("pod %s is not bound", id)
			}
		}
		return nil
	}
}

// policyName returns the name of a routing policy for a test name.
func policyName(policy RoutingPolicy) string {
	return [...]string{"free flow", "congestion", "queue", "predictive"}[policy]
}

// TestEmergencyChoiceLatency measures the worst choice tick of section 15
// of the incident emergency contract: four emergencies that start on one
// tick, under each routing policy, cold and warm. The slowest tick of
// LondonCentral and of the rail-hub preset, and of the delayed-routes
// fixture at the project limits, is at most choiceTickLimit (product
// choice P23). The median is only a diagnostic. The test runs alone,
// without the race detector, because it measures time. The qualify task
// runs it.
func TestEmergencyChoiceLatency(t *testing.T) {
	skipLong(t)
	if raceEnabled {
		t.Skip("the race detector slows the measured code")
	}
	networks := scenarioNetworks(t)
	gate := func(t *testing.T, tick choiceTick) {
		t.Helper()
		t.Logf("slowest %v, median %v, fastest %v; searches %+v", tick.slowest, tick.median, tick.fastest, tick.counts)
		if tick.slowest > choiceTickLimit {
			t.Errorf("the slowest choice tick takes %v, more than %v", tick.slowest, choiceTickLimit)
		}
	}
	for _, name := range []string{"London", "rail hub"} {
		for policy := range RoutingPolicy(4) {
			for _, warm := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s, %s, warm %t", name, policyName(policy), warm), func(t *testing.T) {
					fixture := newChoiceTickFixture(t, networks[name], policy, warm, nil)
					gate(t, measureChoiceTick(t, fixture, allBound(fixture)))
				})
			}
		}
	}
	for _, policy := range []RoutingPolicy{CongestionRouting, QueueRouting, PredictiveRouting} {
		for _, warm := range []bool{false, true} {
			t.Run(fmt.Sprintf("delayed routes, %s, warm %t", policyName(policy), warm), func(t *testing.T) {
				fixture := newChoiceTickFixture(t, delayedNetwork(), policy, warm, nil)
				delayRoutes(t, fixture)
				tick := measureChoiceTick(t, fixture, allBound(fixture))
				gate(t, tick)
				if counts := tick.counts; policy == CongestionRouting && counts.congestion == 0 || policy == QueueRouting && counts.queue == 0 ||
					policy == PredictiveRouting && (counts.forecasts == 0 || counts.predictive == 0) {
					t.Errorf("the stopped pods delay no route search: %+v", counts)
				}
			})
		}
	}
}

// TestEmergencyNoCandidateLatency reports the choice tick of the
// no-candidate fixture of section 15 of the incident emergency contract
// at the project limits, under each routing policy, cold and warm. It is
// reported against choiceTickLimit and not gated (product choice P23).
// The search counts are within the bound of section 9.1. The qualify
// task runs it, so that the report has the times of the CI runner class.
func TestEmergencyNoCandidateLatency(t *testing.T) {
	skipLong(t)
	if raceEnabled {
		t.Skip("the race detector slows the measured code")
	}
	for policy := range RoutingPolicy(4) {
		for _, warm := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s, warm %t", policyName(policy), warm), func(t *testing.T) {
				fixture := missFixture(t, policy, warm)
				tick := measureChoiceTick(t, fixture, func(s *Simulation) error {
					if len(s.emergencyMisses) != len(fixture.pods) {
						return fmt.Errorf("memo %+v", s.emergencyMisses)
					}
					return nil
				})
				t.Logf("reported, not gated: slowest %v, median %v, fastest %v, against %v; searches %+v", tick.slowest, tick.median, tick.fastest, choiceTickLimit, tick.counts)
				// The bound of section 9.1 for a tick of four choices.
				if counts := tick.counts; counts.failed == 0 || counts.localFailed == 0 || counts.graph+counts.trees > 240_052 || counts.trees != 4 {
					t.Errorf("searches %+v", counts)
				}
			})
		}
	}
}

// delayRoutes makes stopped pods on the first lanes of the free-flow route
// of each fixture pod to its station choice: two pods on each of the
// first three lanes after the divert node. The pods that it stops are idle
// pods that are not in the fixture. The searches run on a clone, so a cold
// fixture stays cold.
func delayRoutes(t *testing.T, fixture choiceTickFixture) {
	t.Helper()
	s := fixture.s
	var idle []*vehicle
	for index := range s.vehicles {
		if v := &s.vehicles[index]; v.Pod.Activity == Idle && !slices.Contains(fixture.pods, v.Pod.ID) {
			idle = append(idle, v)
		}
	}
	for _, id := range fixture.pods {
		c := s.Clone()
		w := c.findVehicle(id)
		prefix, from, _ := c.divertStart(w)
		candidate, ok := func() (stationCandidate, bool) {
			defer c.leaveRouteView(c.enterRouteView(w))
			return c.chooseStation(w, prefix, from, true)
		}()
		if !ok {
			t.Fatal("no candidate")
		}
		route, err := c.routeForClass(from, candidate.berth.Node, w.Pod.Class)
		if err != nil {
			t.Fatal(err)
		}
		for _, lane := range route[:min(3, len(route))] {
			for slot := range 2 {
				if len(idle) == 0 {
					t.Fatal("no idle pod to stop")
				}
				stopOn(idle[0], lane.ID, s.laneLength(lane)*float64(slot+1)/3)
				idle = idle[1:]
			}
		}
	}
}

// missFixture returns the no-candidate fixture on missNetwork with the
// routing policy. Four pods ride from a passenger station to the next one
// and are on the departure path of their bank. The through lanes of the
// parking stations are then blocked, so no station has a candidate for
// them, and the pruning tree reaches each berth.
func missFixture(t *testing.T, policy RoutingPolicy, warm bool) choiceTickFixture {
	t.Helper()
	fixture := newChoiceTickFixture(t, missNetwork(), policy, warm, func(s *Simulation, v *vehicle) bool {
		_, from, _ := s.divertStart(v)
		return s.graph.banks.nodes[s.graph.nodes[from]] >= 0
	})
	var footprint faultFootprint
	for _, station := range fixture.s.network.Stations {
		if station.ParkingOnly {
			footprint.resources = append(footprint.resources, track(station.ID+"-through", 0))
		}
	}
	footprint.id = "i0.1"
	fixture.s.setBlocked([]faultFootprint{footprint})
	return fixture
}
