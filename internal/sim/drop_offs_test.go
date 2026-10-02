package sim

import (
	"math"
	"slices"
	"testing"
)

// newDropOffsSimulation returns the sharing fleet in drop-offs mode with a
// limit of 4 parties and the stop limit.
func newDropOffsSimulation(t *testing.T, maxStops int) *Simulation {
	t.Helper()
	s := newSharingSimulation(t)
	if err := s.SetSharedRidePartyLimit(4); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSharedRideMode(SharedRideDropOffs, maxStops); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestStationsOnRoute(t *testing.T) {
	t.Parallel()
	s := newSharingSimulation(t)
	for _, test := range []struct {
		from, to string
		want     []string
	}{
		{"harbor-berth", "market", []string{"garden", "market"}},
		{"harbor-berth", "garden", []string{"garden"}},
		{"garden-berth", "market", []string{"market"}},
		{"market-berth", "harbor", []string{"harbor"}},
	} {
		if got := s.stationsOnRoute(test.from, test.to); !slices.Equal(got, test.want) {
			t.Errorf("stations from %s to %s: %v, want %v", test.from, test.to, got, test.want)
		}
	}
}

func TestDropOffsJoinAddsStops(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name         string
		mode         SharedRideMode
		destinations []string
		wantStops    []string
		wantPending  int
	}{
		{name: "earlier stop", mode: SharedRideDropOffs, destinations: []string{"market", "garden"}, wantStops: []string{"garden", "market"}},
		{name: "later stop", mode: SharedRideDropOffs, destinations: []string{"garden", "market"}, wantStops: []string{"garden", "market"}},
		{name: "same stop", mode: SharedRideDropOffs, destinations: []string{"market", "garden", "market"}, wantStops: []string{"garden", "market"}},
		{name: "destination mode", mode: SharedRideDestination, destinations: []string{"market", "garden"}, wantStops: []string{"market"}, wantPending: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s := newDropOffsSimulation(t, DefaultSharedRideMaxStops)
			if err := s.SetSharedRideMode(test.mode, DefaultSharedRideMaxStops); err != nil {
				t.Fatal(err)
			}
			for _, destination := range test.destinations {
				if err := submitSharedTrip(s, "harbor", destination); err != nil {
					t.Fatal(err)
				}
			}
			v := s.findVehicle("01")
			if !slices.Equal(v.Stops, test.wantStops) || len(s.waiting) != test.wantPending || v.destinationStation != test.wantStops[0] ||
				v.Route[len(v.Route)-1].StationID != test.wantStops[0] || len(v.Riders) != len(test.destinations)-test.wantPending {
				t.Fatalf("stops %v to %s, %d riders, %d pending", v.Stops, v.destinationStation, len(v.Riders), len(s.waiting))
			}
			advance(s, 300*TicksPerSecond)
			state := s.Snapshot()
			// Another pod serves a party that did not join.
			if state.Completed != len(test.destinations) || state.SharedParties != len(test.destinations)-1-test.wantPending {
				t.Fatalf("completed %d, shared %d", state.Completed, state.SharedParties)
			}
			if wantDetour := len(test.wantStops) > 1; (state.MaxDetourRatio > 1+1e-9) != wantDetour {
				t.Fatalf("detour ratio %.6f with stops %v", state.MaxDetourRatio, test.wantStops)
			}
		})
	}
}

func TestDropOffStopsLimits(t *testing.T) {
	t.Parallel()
	s := newDropOffsSimulation(t, 1)
	if err := submitSharedTrip(s, "harbor", "market"); err != nil {
		t.Fatal(err)
	}
	v := s.findVehicle("01")
	if stops, ok := s.dropOffStops(v, "garden"); !ok || !slices.Equal(stops, []string{"garden", "market"}) {
		t.Fatalf("stops %v, %t", stops, ok)
	}
	v.Stops = []string{"garden", "market"}
	if _, ok := s.dropOffStops(v, "harbor"); ok {
		t.Fatal("a stop beyond the stop limit was added")
	}
	if stops, ok := s.dropOffStops(v, "market"); !ok || !slices.Equal(stops, v.Stops) {
		t.Fatalf("an existing stop was refused: %v, %t", stops, ok)
	}
	v.Stops, v.reservedThrough = []string{"market"}, 0
	if _, ok := s.dropOffStops(v, "garden"); ok {
		t.Fatal("a pod with track added a stop")
	}
	v.reservedThrough, v.destination = -1, Berth{ID: "market-1"}
	if _, ok := s.dropOffStops(v, "garden"); ok {
		t.Fatal("a pod with a berth at its next stop added a stop")
	}
}

// TestDropOffsDetourCap boards a party at harbor, and then a party for the
// other destination. The route from harbor to market passes garden, so each
// join makes the stops garden and market. With the garden berth moved far
// from the guideway, the stop at garden takes the rider for market over
// maxSharedRideDetour, and the pod refuses the second party. The planned
// ratio is equal to the ratio that alight measures, because each station
// of the network has one berth.
func TestDropOffsDetourCap(t *testing.T) {
	t.Parallel()
	far := Example()
	for index := range far.Nodes {
		if far.Nodes[index].ID == "garden-berth" {
			far.Nodes[index].Position.Y = -600
		}
	}
	for _, test := range []struct {
		name         string
		network      Network
		destinations []string
		wantStops    []string
	}{
		{name: "rider aboard within the cap", network: Example(), destinations: []string{"market", "garden"}, wantStops: []string{"garden", "market"}},
		{name: "new rider within the cap", network: Example(), destinations: []string{"garden", "market"}, wantStops: []string{"garden", "market"}},
		{name: "rider aboard over the cap", network: far, destinations: []string{"market", "garden"}, wantStops: []string{"market"}},
		{name: "new rider over the cap", network: far, destinations: []string{"garden", "market"}, wantStops: []string{"garden"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, err := NewFleet(test.network, []Placement{
				{ID: "01", StationID: "harbor", BerthID: "harbor-1"},
				{ID: "02", StationID: "garden", BerthID: "garden-1"},
			})
			if err != nil {
				t.Fatal(err)
			}
			monitorContract(t, s)
			if err := s.SetSharedRidePartyLimit(4); err != nil {
				t.Fatal(err)
			}
			if err := s.SetSharedRideMode(SharedRideDropOffs, DefaultSharedRideMaxStops); err != nil {
				t.Fatal(err)
			}
			ridden, ok := s.routeMeters("harbor-berth", "garden")
			if !ok {
				t.Fatal("no route to garden")
			}
			planned := s.plannedDetour("harbor-berth", []string{"garden", "market"}, detourStart{ridden: ridden})
			if over := planned > maxSharedRideDetour; over != (len(test.wantStops) == 1) {
				t.Fatalf("planned detour ratio %.6f", planned)
			}
			for _, destination := range test.destinations {
				if err := submitSharedTrip(s, "harbor", destination); err != nil {
					t.Fatal(err)
				}
			}
			if v := s.findVehicle("01"); !slices.Equal(v.Stops, test.wantStops) {
				t.Fatalf("stops %v, want %v", v.Stops, test.wantStops)
			}
			advance(s, 600*TicksPerSecond)
			state := s.Snapshot()
			if state.Completed != len(test.destinations) {
				t.Fatalf("completed %d of %d", state.Completed, len(test.destinations))
			}
			want := 1.0
			if len(test.wantStops) > 1 {
				want = planned
			}
			if math.Abs(state.MaxDetourRatio-want) > 1e-9 {
				t.Fatalf("measured detour ratio %.9f, want %.9f", state.MaxDetourRatio, want)
			}
		})
	}
}

// loopNetwork returns the example network with a second path from the
// harbor exit to the branch. The path goes through the node loop at the
// height y.
func loopNetwork(y float64) Network {
	network := Example()
	network.Nodes = append(network.Nodes, Node{ID: "loop", Position: Point{X: 250, Y: y}})
	network.Lanes = append(network.Lanes,
		Lane{ID: "loop-out", From: "harbor-exit", To: "loop", SpeedLimit: 14},
		Lane{ID: "loop-in", From: "loop", To: "branch", SpeedLimit: 14},
	)
	return network
}

// TestDropOffsCostedRouteKeepsCap gives the lane approach-branch a large
// congestion cost, so the congestion route from harbor goes through the
// long loop. The free-flow plan accepts the party for garden, but the
// congestion route to garden takes the rider for market over
// maxSharedRideDetour. The pod must take the free-flow route.
func TestDropOffsCostedRouteKeepsCap(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(loopNetwork(1260), []Placement{
		{ID: "01", StationID: "harbor", BerthID: "harbor-1"},
		{ID: "02", StationID: "garden", BerthID: "garden-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	monitorContract(t, s)
	if err = s.SetSharedRidePartyLimit(4); err != nil {
		t.Fatal(err)
	}
	if err = s.SetSharedRideMode(SharedRideDropOffs, DefaultSharedRideMaxStops); err != nil {
		t.Fatal(err)
	}
	s.SetCongestionRouting(true)
	s.ensureNetworkIndexes()
	costs := make([]float64, len(s.network.Lanes))
	costs[s.graph.lanes["approach-branch"]] = 1e6
	s.congestionRouteCosts, s.nextCongestionRouteRefresh = costs, math.MaxInt64
	s.congestionRoutes = make(map[routeKey]routeResult)

	v := s.findVehicle("01")
	costed, err := s.assignedApproachRoute(v, "harbor-berth", "garden")
	if err != nil || !slices.ContainsFunc(costed, func(lane Lane) bool { return lane.ID == "loop-out" }) {
		t.Fatalf("congestion route %v, %v", routeIDs(costed), err)
	}
	if planned := s.plannedDetour("harbor-berth", []string{"garden", "market"}, detourStart{ridden: s.lanesMeters(costed)}); planned <= maxSharedRideDetour {
		t.Fatalf("planned detour ratio on the congestion route %.6f", planned)
	}
	for _, destination := range []string{"market", "garden"} {
		if err := submitSharedTrip(s, "harbor", destination); err != nil {
			t.Fatal(err)
		}
	}
	if !slices.Equal(v.Stops, []string{"garden", "market"}) {
		t.Fatalf("stops %v", v.Stops)
	}
	if slices.ContainsFunc(v.Route, func(lane Lane) bool { return lane.ID == "loop-out" }) {
		t.Fatalf("route %v goes through the loop", routeIDs(v.Route))
	}
	advance(s, 600*TicksPerSecond)
	state := s.Snapshot()
	if state.Completed != 2 || state.SharedParties != 1 || state.MaxDetourRatio > maxSharedRideDetour+1e-9 {
		t.Fatalf("completed %d, shared %d, detour ratio %.6f", state.Completed, state.SharedParties, state.MaxDetourRatio)
	}
}

// directBerthNetwork returns ladderNetwork with the second market arrival
// node far from the station, and a direct lane from the market entry to
// the second market berth. The station path from the entry to that berth
// is the direct lane. A pod that keeps the lanes to the first arrival node
// must go through the far node.
func directBerthNetwork() Network {
	network := ladderNetwork()
	for index := range network.Nodes {
		if network.Nodes[index].ID == "market-arrival-2" {
			network.Nodes[index].Position = Point{X: 760, Y: 3000}
		}
	}
	network.Lanes = append(network.Lanes, Lane{ID: "market-in-direct", From: "market-entry", To: "market-berth-2", SpeedLimit: 14})
	return network
}

// TestDropOffsBerthRerouteKeepsCap moves pod 01 to the branch before the
// inlet of market-1, with market-1 taken. The reroute to market-2 keeps the
// lanes to the first arrival node and goes through the far node. In
// drop-offs mode, this takes the rider over maxSharedRideDetour, so the pod
// keeps market-1. In destination mode, the pod takes market-2.
func TestDropOffsBerthRerouteKeepsCap(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name            string
		mode            SharedRideMode
		wantDestination string
	}{
		{name: "destination", mode: SharedRideDestination, wantDestination: "market-2"},
		{name: "drop-offs", mode: SharedRideDropOffs, wantDestination: "market-1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, err := NewFleet(directBerthNetwork(), []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"}})
			if err != nil {
				t.Fatal(err)
			}
			if err := s.SetSharedRidePartyLimit(4); err != nil {
				t.Fatal(err)
			}
			if err := s.SetSharedRideMode(test.mode, DefaultSharedRideMaxStops); err != nil {
				t.Fatal(err)
			}
			if path, err := s.stationPath("market-entry", "market-berth-2"); err != nil || len(path) != 1 || path[0].ID != "market-in-direct" {
				t.Fatalf("station path %v, %v", routeIDs(path), err)
			}
			if err := requestSharedJourney(s, "01", "market"); err != nil {
				t.Fatal(err)
			}
			v := s.findVehicle("01")
			assignPassengerBerthForTest(t, assignPassengerBerthInput{simulation: s, vehicle: v})
			s.owners[resource{kind: berthResource, id: "market-1"}] = "02"
			s.owners[resource{kind: nodeResource, id: "market-berth"}] = "02"
			positionBeforeTerminalInlet(t, terminalInletPosition{simulation: s, vehicle: v})

			s.reevaluateTerminalBerth(v)

			if v.destination.ID != test.wantDestination {
				t.Fatalf("destination %q via %v, want %q", v.destination.ID, routeIDs(v.Route), test.wantDestination)
			}
		})
	}
}

// TestDropOffsDetourCapWithRoutingPolicies runs trips in drop-offs mode on
// a network with a short loop beside the lane approach-branch, and the
// garden berth a small distance from the guideway. With each routing
// policy, no rider may go over maxSharedRideDetour. Without the check of
// legRoute, the congestion routes through the loop take riders for market
// over the cap.
func TestDropOffsDetourCapWithRoutingPolicies(t *testing.T) {
	t.Parallel()
	network := loopNetwork(400)
	for index := range network.Nodes {
		if network.Nodes[index].ID == "garden-berth" {
			network.Nodes[index].Position.Y = 10
		}
	}
	for name, policy := range map[string]RoutingPolicy{"free flow": FreeFlowRouting, "congestion": CongestionRouting, "queue": QueueRouting, "predictive": PredictiveRouting} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s, err := NewFleet(network, []Placement{
				{ID: "01", StationID: "harbor", BerthID: "harbor-1"},
				{ID: "02", StationID: "garden", BerthID: "garden-1"},
				{ID: "03", StationID: "parking", BerthID: "parking-1"},
				{ID: "04", StationID: "parking", BerthID: "parking-2"},
			})
			if err != nil {
				t.Fatal(err)
			}
			monitorContract(t, s)
			if err := s.SetSharedRidePartyLimit(4); err != nil {
				t.Fatal(err)
			}
			if err := s.SetSharedRideMode(SharedRideDropOffs, DefaultSharedRideMaxStops); err != nil {
				t.Fatal(err)
			}
			if err := s.SetRoutingPolicy(policy); err != nil {
				t.Fatal(err)
			}
			trips := [][2]string{{"harbor", "market"}, {"harbor", "garden"}, {"harbor", "market"}, {"garden", "market"}, {"market", "harbor"}, {"market", "garden"}}
			for tick := range 600 * TicksPerSecond {
				if tick%(3*TicksPerSecond) == 0 {
					trip := trips[tick/(3*TicksPerSecond)%len(trips)]
					if err := submitSharedTrip(s, trip[0], trip[1]); err != nil {
						t.Fatal(err)
					}
				}
				s.Step()
			}
			state := s.Snapshot()
			if state.Completed < 30 || state.SharedParties == 0 || state.MaxDetourRatio > maxSharedRideDetour+1e-9 {
				t.Fatalf("completed %d, shared %d, detour ratio %.6f", state.Completed, state.SharedParties, state.MaxDetourRatio)
			}
		})
	}
}

// twoHarborBerthNetwork returns the example network with the first harbor
// berth far from the station exit, a second harbor berth near it, and the
// garden berth far from the guideway. The stop at garden takes a rider for
// market from harbor-1 within maxSharedRideDetour, but not from harbor-2.
func twoHarborBerthNetwork() Network {
	network := Example()
	for index := range network.Nodes {
		switch network.Nodes[index].ID {
		case "harbor-berth":
			network.Nodes[index].Position.Y = 600
		case "garden-berth":
			network.Nodes[index].Position.Y = -100
		}
	}
	network.Nodes = append(network.Nodes, Node{ID: "harbor-berth-2", Position: Point{X: 140, Y: 300}})
	network.Lanes = append(network.Lanes,
		Lane{ID: "harbor-in-2", From: "harbor-entry", To: "harbor-berth-2", SpeedLimit: 14, StationID: "harbor", StationRole: StationBerthAccessRole},
		Lane{ID: "harbor-out-2", From: "harbor-berth-2", To: "harbor-exit", SpeedLimit: 14, StationID: "harbor", StationRole: StationDepartureRole},
	)
	for index := range network.Stations {
		if network.Stations[index].ID == "harbor" {
			network.Stations[index].Berths = append(network.Stations[index].Berths, Berth{ID: "harbor-2", Node: "harbor-berth-2"})
		}
	}
	return network
}

// TestDropOffsRestoreBoardsWithinCap restores pod 01 with parties for garden
// and market on a wrong lane, so the restore boards the parties again at a
// free harbor berth. From harbor-1, the stops keep each party within
// maxSharedRideDetour, and the pod boards. From harbor-2, the stops take the
// party for market over the cap, so the parties go back to the queue.
func TestDropOffsRestoreBoardsWithinCap(t *testing.T) {
	t.Parallel()
	f := newRestoreFleetFixture(t, twoHarborBerthNetwork(), []Placement{
		{ID: "01", StationID: "harbor", BerthID: "harbor-1"},
		{ID: "02", StationID: "harbor", BerthID: "harbor-2"},
		{ID: "03", StationID: "parking", BerthID: "parking-1"},
	})
	stops := []string{"garden", "market"}
	for node, over := range map[string]bool{"harbor-berth": false, "harbor-berth-2": true} {
		ridden, ok := f.s.routeMeters(node, "garden")
		if planned := f.s.plannedDetour(node, stops, detourStart{ridden: ridden}); !ok || (planned > maxSharedRideDetour) != over {
			t.Fatalf("planned detour ratio from %s %.6f", node, planned)
		}
	}
	pod := f.traveling(t, travelInput{id: "01", from: "harbor-1", to: "garden-1", lane: "approach-branch", distance: 20})
	pod.Occupied, pod.Stops, pod.JourneyOrigin, pod.LaneID = true, stops, "harbor-1", "market-in"
	for index, stop := range stops {
		pod.Riders = append(pod.Riders, SavedRequest{
			SharingConsent: SharedConsent, Service: OnDemandService, ID: index + 1, From: "harbor", To: stop, PartySize: 1, PodID: "01", RequestedTick: 10, BoardedTick: 20,
		})
	}
	for _, test := range []struct {
		name       string
		other      string
		wantBerth  string
		wantQueued []int
	}{
		{name: "harbor-1 free", other: "harbor-2", wantBerth: "harbor-1"},
		{name: "harbor-2 free", other: "harbor-1", wantBerth: "garden-1", wantQueued: []int{1, 2}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			state := f.state(pod, f.idle(t, "02", test.other))
			state.SharedRidePartyLimit, state.SharedRideMode = 4, SharedRideDropOffs
			s, result, err := f.restore(state)
			if err != nil || result.Tier != RestorePhysical || !slices.Equal(result.Demoted, []string{"01"}) {
				t.Fatalf("restore: %v, %+v", err, result)
			}
			v := findVehicle(t, s, "01")
			if v.Pod.BerthID != test.wantBerth || !slices.Equal(result.Requeued, test.wantQueued) || len(s.waiting) != len(test.wantQueued) {
				t.Fatalf("pod 01 at %q, requeued %v, %d waiting", v.Pod.BerthID, result.Requeued, len(s.waiting))
			}
			monitorContract(t, s)
			advance(s, 600*TicksPerSecond)
			snapshot := s.Snapshot()
			if snapshot.Completed != 2 || snapshot.MaxDetourRatio > maxSharedRideDetour+1e-9 {
				t.Fatalf("completed %d, detour ratio %.6f", snapshot.Completed, snapshot.MaxDetourRatio)
			}
		})
	}
}
