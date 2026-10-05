package sim

import (
	"math"
	"reflect"
	"slices"
	"testing"
)

// blockLanes sets a blocked set that holds the first track cell of each
// lane, so it blocks only those lanes.
func blockLanes(t *testing.T, s *Simulation, lanes ...string) {
	t.Helper()
	s.ensureNetworkIndexes()
	footprint := faultFootprint{id: "i1.1"}
	for _, id := range lanes {
		if _, ok := s.graph.lanes[id]; !ok {
			t.Fatalf("no lane %s", id)
		}
		footprint.resources = append(footprint.resources, resource{kind: trackResource, id: id})
	}
	s.setBlocked([]faultFootprint{footprint})
}

// blockBerth sets a blocked set that holds the berth resource of a berth.
func blockBerth(s *Simulation, berthID string) {
	s.setBlocked([]faultFootprint{{id: "i1.1", resources: []resource{{kind: berthResource, id: berthID}}}})
}

func usesLane(route []Lane, id string) bool {
	return slices.ContainsFunc(route, func(lane Lane) bool { return lane.ID == id })
}

func blockedLaneIDs(s *Simulation) []string {
	var ids []string
	for index, blocked := range s.blocked.lanes {
		if blocked {
			ids = append(ids, s.network.Lanes[index].ID)
		}
	}
	slices.Sort(ids)
	return ids
}

func TestRoutingGraphWithEmptyBlockedSet(t *testing.T) {
	t.Parallel()
	s := newTraffic(t)
	s.ensureNetworkIndexes()
	// The graph holds the bank index error, which is nil here.
	if graph := s.routingGraph(); graph.blocked != nil || !reflect.DeepEqual(graph, s.graph) { //nolint:govet // deepequalerrors: the error is nil on both sides.
		t.Fatal("the routing graph of an empty blocked set is not the static graph")
	}
	if _, err := s.route("harbor-exit", "market-entry"); err != nil {
		t.Fatal(err)
	}
	s.setBlocked(nil)
	if s.blockedActive() || s.rerouteDue || len(s.routes) == 0 {
		t.Fatalf("an empty footprint started an epoch: active %v, reroute %v, routes %d", s.blockedActive(), s.rerouteDue, len(s.routes))
	}
	blockLanes(t, s, "bypass-in")
	s.setBlocked(nil)
	if graph := s.routingGraph(); s.blockedActive() || graph.blocked != nil || s.blocked.by != nil {
		t.Fatal("the last clear did not empty the blocked set")
	}
}

func TestBlockedSetFromFootprints(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		r      resource
		lanes  []string
		berths []string
	}{
		{"track cell", resource{kind: trackResource, id: "bypass-in"}, []string{"bypass-in"}, nil},
		{"junction node", resource{kind: nodeResource, id: "branch"}, []string{"approach-branch", "bypass-in", "garden-approach"}, nil},
		{"berth", resource{kind: berthResource, id: "garden-1"}, []string{"garden-in"}, []string{"garden-1"}},
		{"berth node", resource{kind: nodeResource, id: "garden-berth"}, []string{"garden-in", "garden-out"}, []string{"garden-1"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s := newTraffic(t)
			s.setBlocked([]faultFootprint{{id: "i1.1", resources: []resource{test.r}}})
			if got := blockedLaneIDs(s); !slices.Equal(got, test.lanes) {
				t.Fatalf("blocked lanes %v, want %v", got, test.lanes)
			}
			var berths []string
			for _, station := range s.network.Stations {
				for _, berth := range station.Berths {
					if s.berthBlocked(berth) {
						berths = append(berths, berth.ID)
					}
				}
			}
			if !slices.Equal(berths, test.berths) {
				t.Fatalf("blocked berths %v, want %v", berths, test.berths)
			}
			if !s.blockedActive() || s.blocked.by[test.r] != "i1.1" || len(s.blocked.by) != 1 {
				t.Fatalf("blocked by %v", s.blocked.by)
			}
		})
	}
	t.Run("first fault names a shared resource", func(t *testing.T) {
		t.Parallel()
		s := newTraffic(t)
		shared := resource{kind: nodeResource, id: "branch"}
		s.setBlocked([]faultFootprint{
			{id: "i1.1", resources: []resource{shared}},
			{id: "i1.2", resources: []resource{shared, {kind: trackResource, id: "return"}}},
		})
		if s.blocked.by[shared] != "i1.1" || s.blocked.by[resource{kind: trackResource, id: "return"}] != "i1.2" {
			t.Fatalf("blocked by %v", s.blocked.by)
		}
	})
}

// TestBlockedRouteSearches checks that each operational reader of section
// 8.2 of the incident suspension contract avoids a blocked lane or berth.
func TestBlockedRouteSearches(t *testing.T) {
	t.Parallel()
	t.Run("route", func(t *testing.T) {
		t.Parallel()
		s := newTraffic(t)
		if route, err := s.route("harbor-exit", "market-entry"); err != nil || !usesLane(route, "bypass-in") {
			t.Fatalf("the free route %v, %v does not use bypass-in", route, err)
		}
		blockLanes(t, s, "bypass-in")
		if route, err := s.route("harbor-exit", "market-entry"); err != nil || usesLane(route, "bypass-in") || !usesLane(route, "garden-through") {
			t.Fatalf("the blocked route is %v, %v", route, err)
		}
	})
	t.Run("assigned route", func(t *testing.T) {
		t.Parallel()
		for _, policy := range []RoutingPolicy{FreeFlowRouting, CongestionRouting, QueueRouting, PredictiveRouting} {
			s := newTraffic(t)
			if err := s.SetRoutingPolicy(policy); err != nil {
				t.Fatal(err)
			}
			v := &s.vehicles[0]
			if route, err := s.assignedRoute(v, "harbor-exit", "market-entry"); err != nil || !usesLane(route, "bypass-in") {
				t.Fatalf("policy %v: the free route %v, %v does not use bypass-in", policy, route, err)
			}
			blockLanes(t, s, "bypass-in")
			if route, err := s.assignedRoute(v, "harbor-exit", "market-entry"); err != nil || usesLane(route, "bypass-in") {
				t.Fatalf("policy %v: the blocked route is %v, %v", policy, route, err)
			}
		}
	})
	t.Run("station path", func(t *testing.T) {
		t.Parallel()
		s, err := New(ladderNetwork(), "harbor")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.stationPath("market-entry", "market-berth-2"); err != nil {
			t.Fatal(err)
		}
		blockLanes(t, s, "market-arrival-next")
		if path, err := s.stationPath("market-entry", "market-berth-2"); err == nil {
			t.Fatalf("the station path %v crosses a blocked lane", path)
		}
	})
	t.Run("multi-target search", func(t *testing.T) {
		t.Parallel()
		s, err := New(ladderNetwork(), "harbor")
		if err != nil {
			t.Fatal(err)
		}
		blockLanes(t, s, "bypass-in")
		market, _ := s.station("market")
		s.cacheStationRoutes("harbor-exit", market.Berths)
		for _, berth := range market.Berths {
			cached, ok := s.routes[routeKey{from: "harbor-exit", to: berth.Node}]
			if !ok || cached.err != nil || usesLane(cached.lanes, "bypass-in") {
				t.Fatalf("berth %s: cached %v, %+v", berth.ID, ok, cached)
			}
		}
	})
	t.Run("bank searches", func(t *testing.T) {
		t.Parallel()
		s, err := New(BankExample(), "origin")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.route("origin-berth", "bank-a-berth"); err != nil {
			t.Fatal(err)
		}
		if route, err := s.stationApproachRoute("origin-berth", "hub"); err != nil || route[len(route)-1].To != "bank-a-entry" {
			t.Fatalf("the free approach is %v, %v", route, err)
		}
		blockLanes(t, s, "bank-a-arrival")
		if route, err := s.route("origin-berth", "bank-a-berth"); err == nil {
			t.Fatalf("the bank route %v crosses a blocked lane", route)
		}
		if route, err := s.stationApproachRoute("origin-berth", "hub"); err != nil || route[len(route)-1].To != "bank-b-entry" {
			t.Fatalf("the blocked approach is %v, %v", route, err)
		}
	})
	// bankNearest gives the nearest destination of nearestFreeBerth and the
	// nearest source of preferredFleetSource on a network with banks. The
	// two banks of hub are at the same cost, so berth order gives bank a.
	t.Run("bank nearest destination", func(t *testing.T) {
		t.Parallel()
		s, err := NewFleet(BankExample(), []Placement{{ID: "01", StationID: "origin"}})
		if err != nil {
			t.Fatal(err)
		}
		v := s.findVehicle("01")
		if berth, _, ok := s.nearestFreeBerth(v, "split"); !ok || berth.ID != "bank-a-1" {
			t.Fatalf("the free berth is %s, %v", berth.ID, ok)
		}
		blockLanes(t, s, "bank-a-arrival")
		if berth, _, ok := s.nearestFreeBerth(v, "split"); !ok || berth.ID != "bank-b-1" {
			t.Fatalf("with bank-a-arrival blocked, the berth is %s, %v", berth.ID, ok)
		}
	})
	t.Run("bank nearest source", func(t *testing.T) {
		t.Parallel()
		s, err := NewFleet(BankExample(), []Placement{{ID: "01", StationID: "hub", BerthID: "bank-a-1"}, {ID: "02", StationID: "hub", BerthID: "bank-b-1"}})
		if err != nil {
			t.Fatal(err)
		}
		s.ensureNetworkIndexes()
		rank := make([]int, len(s.network.Nodes))
		for node := range rank {
			rank[node] = -1
		}
		rank[s.graph.nodes["bank-a-berth"]], rank[s.graph.nodes["bank-b-berth"]] = 0, 1
		input := preferredNearestInput{from: "merge", rank: rank, reverse: true}
		if node, ok := s.preferredFleetSource(input); !ok || s.network.Nodes[node].ID != "bank-a-berth" {
			t.Fatalf("the free source is %v, %v", node, ok)
		}
		blockLanes(t, s, "bank-a-departure")
		if node, ok := s.preferredFleetSource(input); !ok || s.network.Nodes[node].ID != "bank-b-berth" {
			t.Fatalf("the blocked source is %v, %v", node, ok)
		}
	})
	t.Run("class sources", func(t *testing.T) {
		t.Parallel()
		// With two classes, the far pod has another class, so a search on
		// the static graph would leave only the far pod.
		for _, classes := range [][3]VehicleClass{{LegacyClass, LegacyClass, LegacyClass}, {LegacyClass, CompactClass, CompactClass}} {
			s, err := NewFleet(Example(), []Placement{
				{ID: "01", StationID: "parking", Class: classes[0]}, {ID: "02", StationID: "garden", Class: classes[1]}, {ID: "03", StationID: "harbor", Class: classes[2]},
			})
			if err != nil {
				t.Fatal(err)
			}
			s.ensureNetworkIndexes()
			rank := make([]int, len(s.network.Nodes))
			for node := range rank {
				rank[node] = -1
			}
			for index, node := range []string{"parking-berth-1", "garden-berth", "harbor-berth"} {
				rank[s.graph.nodes[node]] = index
			}
			input := preferredNearestInput{from: "market-berth", rank: rank, reverse: true}
			if node, ok := s.preferredFleetSource(input); !ok || s.network.Nodes[node].ID != "garden-berth" {
				t.Fatalf("classes %v: the free source is %v, %v", classes, node, ok)
			}
			blockLanes(t, s, "garden-merge")
			if node, ok := s.preferredFleetSource(input); !ok || s.network.Nodes[node].ID != "harbor-berth" {
				t.Fatalf("classes %v: the blocked source is %v, %v", classes, node, ok)
			}
		}
	})
	t.Run("guarded bump to a deficit", func(t *testing.T) {
		t.Parallel()
		for _, blocked := range []bool{false, true} {
			network := lineNetwork(lineStations(2, 2, 2, 2))
			for index := range network.Lanes {
				if network.Lanes[index].ID == "s0-through" {
					network.Lanes[index].SpeedLimit = 0.1
				}
			}
			network.Lanes = append(network.Lanes, Lane{ID: "alternative", From: "p-exit", To: "s2-entry", SpeedLimit: 6})
			s, err := NewFleet(network, place("p-1", "s0-1"))
			if err != nil {
				t.Fatal(err)
			}
			if err := s.SetPositioning(PositioningGuarded); err != nil {
				t.Fatal(err)
			}
			if err := s.SetDemandWeights(map[string]float64{"s1": 1, "s2": 1}); err != nil {
				t.Fatal(err)
			}
			openGate(s)
			if blocked {
				blockLanes(t, s, "alternative")
			}
			blocker := s.findVehicle("01")
			if bumped := s.guardedBumpToDeficit(guardedBump{blocker: blocker, from: "p-1", gate: s.guardedGate()}); bumped == blocked {
				t.Fatalf("blocked %v: bumped %v to %s", blocked, bumped, blocker.destination.ID)
			}
		}
	})
	t.Run("guarded bump to parking", func(t *testing.T) {
		t.Parallel()
		for _, test := range []struct {
			blocked bool
			want    string
		}{{false, "parking-1"}, {true, "parking-2"}} {
			s := newTraffic(t)
			if test.blocked {
				blockLanes(t, s, "parking-in-1")
			}
			blocker := &s.vehicles[0]
			if !s.guardedBumpToParking(guardedBump{blocker: blocker, from: "harbor-berth", gate: s.guardedGate()}) || blocker.destination.ID != test.want {
				t.Fatalf("blocked %v: bumped to %q, want %s", test.blocked, blocker.destination.ID, test.want)
			}
		}
	})
	t.Run("nearest free berth", func(t *testing.T) {
		t.Parallel()
		s := newTraffic(t)
		v := s.findVehicle("02")
		if berth, _, ok := s.nearestFreeBerth(v, "garden-berth"); !ok || berth.ID != "garden-1" {
			t.Fatalf("the free berth is %s, %v", berth.ID, ok)
		}
		// The pod is at the blocked berth, so only the berth test refuses it.
		blockBerth(s, "garden-1")
		if berth, _, ok := s.nearestFreeBerth(v, "garden-berth"); !ok || berth.ID != "market-1" {
			t.Fatalf("with garden-1 blocked, the berth is %s, %v", berth.ID, ok)
		}
		s.setBlocked([]faultFootprint{{id: "i1.1", resources: []resource{{kind: berthResource, id: "garden-1"}, {kind: trackResource, id: "market-in"}}}})
		if berth, _, ok := s.nearestFreeBerth(v, "garden-berth"); !ok || berth.ID == "market-1" || berth.ID == "garden-1" {
			t.Fatalf("with market-in blocked, the berth is %s, %v", berth.ID, ok)
		}
	})
	t.Run("station route by load", func(t *testing.T) {
		t.Parallel()
		s, err := New(ladderNetwork(), "harbor")
		if err != nil {
			t.Fatal(err)
		}
		for _, test := range []struct {
			name, from, want string
			block            func()
		}{
			{"free", "harbor-exit", "market-1", func() {}},
			{"blocked inlet", "harbor-exit", "market-2", func() { blockLanes(t, s, "market-in") }},
			// A route from the berth node itself crosses no lane.
			{"blocked berth", "market-berth", "market-2", func() { blockBerth(s, "market-1") }},
		} {
			test.block()
			if _, berth, err := s.stationRouteByLoad(stationRouteInput{from: test.from, station: "market"}); err != nil || berth.ID != test.want {
				t.Fatalf("%s: berth %s, %v, want %s", test.name, berth.ID, err, test.want)
			}
		}
	})
}

// TestBlockedRoutingKeepsStaticChecks checks that the structural readers of
// section 8.2 of the incident suspension contract ignore the blocked set.
func TestBlockedRoutingKeepsStaticChecks(t *testing.T) {
	t.Parallel()
	s, _, _ := expressTestFleet(t)
	harbor, _ := s.station("harbor")
	market, _ := s.station("market")
	var inlets []string
	for _, lane := range s.network.Lanes {
		if lane.To == market.Entry {
			inlets = append(inlets, lane.ID)
		}
	}
	blockLanes(t, s, inlets...)
	if _, err := s.routeForClass(harbor.Berths[0].Node, market.Berths[0].Node, ExpressClass); err == nil {
		t.Fatal("the blocked set does not cut off market")
	}
	services := []ExpressService{{ID: "harbor-market", Class: ExpressClass, From: "harbor", To: "market", PartyLimit: 20}}
	if err := s.SetExpressServices(services); err != nil {
		t.Fatalf("express service validation reads the blocked set: %v", err)
	}
	if !networkStationsConnected(s.network, s.graph, harbor, market, ExpressClass) {
		t.Fatal("networkStationsConnected reads the blocked set")
	}
}

// TestDivertStartArrivalChainStaysStatic places a pod inside the arrival
// chain of market-2, on a route that omits the lanes behind it. The station
// path from the entry to the end of its committed prefix crosses a blocked
// lane. divertStart must still refuse the pod.
func TestDivertStartArrivalChainStaysStatic(t *testing.T) {
	t.Parallel()
	for _, blocked := range []bool{false, true} {
		s, err := NewFleet(ladderNetwork(), []Placement{{ID: "01", StationID: "harbor"}})
		if err != nil {
			t.Fatal(err)
		}
		s.ensureNetworkIndexes()
		market, _ := s.station("market")
		v := &s.vehicles[0]
		v.Pod.Activity = Traveling
		v.destination, _ = market.berth("market-2")
		v.destinationStation = market.ID
		s.setVehicleRoute(v, []Lane{s.network.Lanes[s.graph.lanes["market-arrival-next"]], s.network.Lanes[s.graph.lanes["market-in-2"]]})
		v.reservedThrough = 0
		if blocked {
			blockLanes(t, s, "market-arrival-link")
			if _, err := s.stationPath("market-entry", "market-arrival-2"); err == nil {
				t.Fatal("the operational station path does not see the blocked lane")
			}
		}
		if prefix, from, ok := s.divertStart(v); ok {
			t.Fatalf("blocked %v: divertStart allows a pod in the arrival chain to divert from %s after %d lanes", blocked, from, prefix)
		}
	}
}

// TestRouteEpochs checks that each change of the blocked set clears the
// cached routes, also cached failures, before the call returns.
func TestRouteEpochs(t *testing.T) {
	t.Parallel()
	t.Run("cached success", func(t *testing.T) {
		t.Parallel()
		s := newTraffic(t)
		if route, err := s.route("harbor-exit", "market-entry"); err != nil || !usesLane(route, "bypass-in") {
			t.Fatalf("the free route %v, %v does not use bypass-in", route, err)
		}
		if stations := s.stationsOnRoute("harbor-berth", "market"); len(stations) == 0 {
			t.Fatal("the free stops are empty")
		}
		blockLanes(t, s, "bypass-in")
		if !s.rerouteDue {
			t.Fatal("the new epoch does not ask for a reroute pass")
		}
		if route, err := s.route("harbor-exit", "market-entry"); err != nil || usesLane(route, "bypass-in") {
			t.Fatalf("the route %v, %v is from the earlier epoch", route, err)
		}
	})
	t.Run("cached failure", func(t *testing.T) {
		t.Parallel()
		s := newTraffic(t)
		if stations := s.stationsOnRoute("harbor-berth", "market"); len(stations) == 0 {
			t.Fatal("the free stops are empty")
		}
		blockLanes(t, s, "market-approach")
		if _, err := s.route("harbor-exit", "market-entry"); err == nil {
			t.Fatal("the blocked set does not cut off market")
		}
		if stations := s.stationsOnRoute("harbor-berth", "market"); len(stations) != 0 {
			t.Fatalf("the stops %v are from the earlier epoch", stations)
		}
		s.setBlocked(nil)
		if route, err := s.route("harbor-exit", "market-entry"); err != nil || !usesLane(route, "bypass-in") {
			t.Fatalf("the route %v, %v is from the earlier epoch", route, err)
		}
	})
	t.Run("congestion route", func(t *testing.T) {
		t.Parallel()
		s := newTraffic(t)
		if err := s.SetRoutingPolicy(CongestionRouting); err != nil {
			t.Fatal(err)
		}
		v := &s.vehicles[0]
		if route, err := s.assignedRoute(v, "harbor-exit", "market-entry"); err != nil || !usesLane(route, "bypass-in") {
			t.Fatalf("the free route %v, %v does not use bypass-in", route, err)
		}
		blockLanes(t, s, "bypass-in")
		if route, err := s.assignedRoute(v, "harbor-exit", "market-entry"); err != nil || usesLane(route, "bypass-in") {
			t.Fatalf("the route %v, %v is from the earlier epoch", route, err)
		}
	})
	t.Run("same set", func(t *testing.T) {
		t.Parallel()
		s := newTraffic(t)
		blockLanes(t, s, "bypass-in")
		s.rerouteDue = false
		if _, err := s.route("harbor-exit", "market-entry"); err != nil {
			t.Fatal(err)
		}
		// Another fault ID for the same lanes and berths is the same epoch.
		s.setBlocked([]faultFootprint{{id: "i1.2", resources: []resource{{kind: trackResource, id: "bypass-in"}}}})
		if _, cached := s.routes[routeKey{from: "harbor-exit", to: "market-entry"}]; !cached || s.rerouteDue {
			t.Fatalf("the same blocked set started an epoch: cached %v, reroute %v", cached, s.rerouteDue)
		}
		if s.blocked.by[resource{kind: trackResource, id: "bypass-in"}] != "i1.2" {
			t.Fatalf("blocked by %v", s.blocked.by)
		}
	})
}

// TestBlockedOrderAdmissionStaysStatic checks section 8.4 of the incident
// suspension contract: a fault does not refuse an order or unbind a trip.
func TestBlockedOrderAdmissionStaysStatic(t *testing.T) {
	t.Parallel()
	t.Run("new order", func(t *testing.T) {
		t.Parallel()
		s := newTraffic(t)
		blockLanes(t, s, "market-approach")
		if err := s.RequestTrip("garden", "market"); err != nil {
			t.Fatalf("an order to a cut-off station is refused: %v", err)
		}
		for range 10 * TicksPerSecond {
			s.Step()
		}
		if len(s.waiting) != 1 || s.completed != 0 {
			t.Fatalf("the order does not wait: waiting %d, completed %d", len(s.waiting), s.completed)
		}
		if err := s.CheckContract(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("bound trip", func(t *testing.T) {
		t.Parallel()
		s := newTraffic(t)
		trip := newTrip(s, "garden", "market")
		trip.request.PodID = "01"
		s.waiting = append(s.waiting, trip)
		blockLanes(t, s, "market-approach")
		s.dispatch()
		if len(s.waiting) != 1 || s.waiting[0].request.PodID != "01" {
			t.Fatalf("dispatch unbound the trip: %+v", s.waiting)
		}
	})
}

// detourNetwork returns stations a and b. A direct lane of 100 m joins them,
// and a bypass of 250 m. Station b has a short inlet and a long inlet.
func detourNetwork() Network {
	bypass := math.Sqrt(125*125 - 50*50)
	return Network{
		Nodes: []Node{
			{ID: "a-entry", Position: Point{0, 0}}, {ID: "a-berth", Position: Point{20, 20}}, {ID: "a-exit", Position: Point{40, 0}},
			{ID: "bypass", Position: Point{90, bypass}},
			{ID: "b-entry", Position: Point{140, 0}}, {ID: "b-berth", Position: Point{160, 20}}, {ID: "b-exit", Position: Point{180, 0}},
			{ID: "b-mid", Position: Point{150, 60}},
			{ID: "return-east", Position: Point{180, -60}}, {ID: "return-west", Position: Point{0, -60}},
		},
		Lanes: []Lane{
			{ID: "a-through", From: "a-entry", To: "a-exit", SpeedLimit: 14, StationID: "a", StationRole: StationThroughRole},
			{ID: "a-in", From: "a-entry", To: "a-berth", SpeedLimit: 14, StationID: "a", StationRole: StationBerthAccessRole},
			{ID: "a-out", From: "a-berth", To: "a-exit", SpeedLimit: 14, StationID: "a", StationRole: StationDepartureRole},
			{ID: "direct", From: "a-exit", To: "b-entry", SpeedLimit: 14},
			{ID: "bypass-out", From: "a-exit", To: "bypass", SpeedLimit: 14},
			{ID: "bypass-in", From: "bypass", To: "b-entry", SpeedLimit: 14},
			{ID: "b-through", From: "b-entry", To: "b-exit", SpeedLimit: 14, StationID: "b", StationRole: StationThroughRole},
			{ID: "b-in", From: "b-entry", To: "b-berth", SpeedLimit: 14, StationID: "b", StationRole: StationBerthAccessRole},
			{ID: "b-in-long", From: "b-entry", To: "b-mid", SpeedLimit: 14, StationID: "b", StationRole: StationBerthAccessRole},
			{ID: "b-in-mid", From: "b-mid", To: "b-berth", SpeedLimit: 14, StationID: "b", StationRole: StationBerthAccessRole},
			{ID: "b-out", From: "b-berth", To: "b-exit", SpeedLimit: 14, StationID: "b", StationRole: StationDepartureRole},
			{ID: "return-start", From: "b-exit", To: "return-east", SpeedLimit: 14},
			{ID: "return", From: "return-east", To: "return-west", SpeedLimit: 14},
			{ID: "return-end", From: "return-west", To: "a-entry", SpeedLimit: 14},
		},
		Stations: []Station{
			{ID: "a", Name: "A", Entry: "a-entry", Exit: "a-exit", Berths: []Berth{{ID: "a-1", Node: "a-berth"}}},
			{ID: "b", Name: "B", Entry: "b-entry", Exit: "b-exit", Berths: []Berth{{ID: "b-1", Node: "b-berth"}}},
		},
	}
}

// TestDetourBaselinesStayStatic blocks the direct lane and the short inlet
// of b. Riders from a then ride the bypass and the long inlet. Each detour
// baseline is still the direct distance of the static graph, so the ratio
// is above the limit for each kind of rider and plan end, and at alighting.
func TestDetourBaselinesStayStatic(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(detourNetwork(), []Placement{{ID: "01", StationID: "a"}})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := s.station("b")
	berth := b.Berths[0]
	direct := s.directDistance("a-berth", "b", berth)
	directMeters, _ := s.routeMeters("a-berth", "b")
	blockLanes(t, s, "direct", "b-in")
	approach, err := s.route("a-berth", "b-entry")
	if err != nil || !usesLane(approach, "bypass-in") {
		t.Fatalf("the blocked approach is %v, %v", approach, err)
	}
	inlet, err := s.stationPath("b-entry", "b-berth")
	if err != nil || !usesLane(inlet, "b-in-long") {
		t.Fatalf("the blocked inlet is %v, %v", inlet, err)
	}
	toEntry := s.lanesMeters(approach)
	ridden := toEntry + s.lanesMeters(inlet)
	want := ridden / direct
	if want <= maxSharedRideDetour {
		t.Fatalf("the fixture ratio %g is not above the limit", want)
	}
	if got := s.directDistance("a-berth", "b", berth); got != direct {
		t.Fatalf("direct distance %g, want the static %g", got, direct)
	}
	if got, ok := s.routeMeters("a-berth", "b"); !ok || got != directMeters {
		t.Fatalf("route meters %g, want the static %g", got, directMeters)
	}
	ends := map[string]detourStart{
		"berth end": {class: LegacyClass, from: "a-berth", ridden: ridden, berth: berth},
		"entry end": {class: LegacyClass, from: "a-berth", ridden: toEntry, entry: "b-entry"},
	}
	for name, start := range ends {
		if got := s.plannedDetour("a-berth", []string{"b"}, start); math.Abs(got-want) > 1e-9 {
			t.Errorf("legacy rider, %s: ratio %g, want %g", name, got, want)
		}
		if got := s.plannedRiderDetour(riderDetour{origin: "a-berth", destination: "b"}, []string{"b"}, start); math.Abs(got-want) > 1e-9 {
			t.Errorf("recorded rider, %s: ratio %g, want %g", name, got, want)
		}
		v := &s.vehicles[0]
		v.journeyOrigin, _ = s.network.Stations[0].berth("a-1")
		v.Riders = []Request{{ID: 1, From: "a", To: "b", PartySize: 1}}
		v.Boardings = nil
		if s.keepsRiderDetours(v, []string{"b"}, start) {
			t.Errorf("legacy rider, %s: the plan keeps the detour limit", name)
		}
		v.Boardings = []RiderBoarding{{BerthID: "a-1"}}
		if s.keepsRiderDetours(v, []string{"b"}, start) {
			t.Errorf("recorded rider, %s: the plan keeps the detour limit", name)
		}
	}
	v := &s.vehicles[0]
	v.journeyOrigin, _ = s.network.Stations[0].berth("a-1")
	v.Riders = []Request{{ID: 1, From: "a", To: "b", PartySize: 1}}
	v.Boardings = nil
	v.destination = berth
	s.completeRider(v, 0, ridden)
	if s.directDistanceMeters != direct || math.Abs(s.maxDetourRatio-want) > 1e-9 {
		t.Fatalf("at alighting: direct %g, ratio %g, want %g and %g", s.directDistanceMeters, s.maxDetourRatio, direct, want)
	}
}

// bankDetourNetwork is BankExample with a bypass of 730 m beside the
// approach lane of bank a, which is 170 m long.
func bankDetourNetwork() Network {
	network := BankExample()
	network.Nodes = append(network.Nodes, Node{ID: "a-bypass", Position: Point{360, -300}})
	network.Lanes = append(network.Lanes,
		Lane{ID: "a-bypass-out", From: "split", To: "a-bypass", SpeedLimit: 14},
		Lane{ID: "a-bypass-in", From: "a-bypass", To: "a-approach", SpeedLimit: 14},
	)
	return network
}

// TestBankDetourBaselinesStayStatic blocks the approach lane of bank a.
// Riders from origin to bank a then ride the bypass. The baselines of the
// bank detour plan are still the direct distances of the static graph.
func TestBankDetourBaselinesStayStatic(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(bankDetourNetwork(), []Placement{{ID: "01", StationID: "origin"}})
	if err != nil {
		t.Fatal(err)
	}
	hub, _ := s.station("hub")
	berth, _ := hub.berth("bank-a-1")
	direct := s.directDistance("origin-berth", "hub", berth)
	blockLanes(t, s, "a-approach")
	approach, err := s.route("origin-berth", "bank-a-entry")
	if err != nil || !usesLane(approach, "a-bypass-in") {
		t.Fatalf("the blocked approach is %v, %v", approach, err)
	}
	inlet, err := s.stationPath("bank-a-entry", "bank-a-berth")
	if err != nil {
		t.Fatal(err)
	}
	toEntry := s.lanesMeters(approach)
	ridden := toEntry + s.lanesMeters(inlet)
	want := ridden / direct
	if want <= maxSharedRideDetour {
		t.Fatalf("the fixture ratio %g is not above the limit", want)
	}
	if got := s.directDistance("origin-berth", "hub", berth); got != direct {
		t.Fatalf("direct distance %g, want the static %g", got, direct)
	}
	ends := map[string]detourStart{
		"berth end": {class: LegacyClass, from: "origin-berth", ridden: ridden, berth: berth},
		"entry end": {class: LegacyClass, from: "origin-berth", ridden: toEntry, entry: "bank-a-entry"},
	}
	origin, _ := s.network.Stations[0].berth("origin-1")
	for name, start := range ends {
		if got := s.plannedDetour("origin-berth", []string{"hub"}, start); math.Abs(got-want) > 1e-9 {
			t.Errorf("legacy rider, %s: ratio %g, want %g", name, got, want)
		}
		if got := s.plannedRiderDetour(riderDetour{origin: "origin-berth", destination: "hub"}, []string{"hub"}, start); math.Abs(got-want) > 1e-9 {
			t.Errorf("recorded rider, %s: ratio %g, want %g", name, got, want)
		}
		v := &s.vehicles[0]
		v.journeyOrigin = origin
		v.Riders = []Request{{ID: 1, From: "origin", To: "hub", PartySize: 1}}
		v.Boardings = nil
		if s.keepsRiderDetours(v, []string{"hub"}, start) {
			t.Errorf("legacy rider, %s: the plan keeps the detour limit", name)
		}
		v.Boardings = []RiderBoarding{{BerthID: "origin-1"}}
		if s.keepsRiderDetours(v, []string{"hub"}, start) {
			t.Errorf("recorded rider, %s: the plan keeps the detour limit", name)
		}
	}
}
