package sim

import (
	"slices"
	"strings"
	"testing"
)

// depotContracts are the contracts of the depot restore tests, so that a
// save holds the faults.
var depotContracts = FleetContracts{IncidentContract: IncidentV1Contract, FaultContract: FaultV1Contract}

// depotFaultSettings are the fault settings of the depot restore tests.
var depotFaultSettings = FaultSettings{EvacuationSeconds: 300}

// newDepotFleet returns a fleet on a depot network with the fault
// operations on.
func newDepotFleet(t *testing.T, network Network, placements []Placement) *Simulation {
	t.Helper()
	s, err := NewFleetWithContracts(network, placements, depotContracts)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetFaults(true, depotFaultSettings); err != nil {
		t.Fatal(err)
	}
	return s
}

// restoreDepot restores the saved state of s, and checks that the restore
// keeps every pod and order in place and matches s. With no fault, the state
// takes a strict JSON round trip first. The session adapter saves the pod of
// a fault record, which the plain JSON of SavedFault omits.
func restoreDepot(t *testing.T, network Network, s *Simulation) *Simulation {
	t.Helper()
	state := s.ExportState()
	if len(s.faults) == 0 {
		state = roundTripState(t, state)
	}
	restored, result, err := RestoreState(RestoreStateInput{
		IncidentContract: IncidentV1Contract, FaultContract: FaultV1Contract, Faults: depotFaultSettings,
		Network: network, Fleet: s.initial, State: state,
	})
	if err != nil || !cleanRestore(result) {
		t.Fatalf("tick %d: restore %+v, error %v", s.tick, result, err)
	}
	checkRestoredMatches(t, s, restored)
	for i := range s.vehicles {
		if live := &s.vehicles[i]; s.depotParked(live) && restored.vehicles[i].parkedSince != live.parkedSince {
			t.Fatalf("tick %d: pod %s has the parked tick %d after the restore, want %d", s.tick, live.Pod.ID, restored.vehicles[i].parkedSince, live.parkedSince)
		}
	}
	return restored
}

// depotPodKinds names the kinds of depot pods that a restore must keep.
var depotPodKinds = []string{
	"idle", "moving up", "bound with its slot", "bound without its slot", "unbound at the entry", "departing to the entry",
	"departing", "faulted",
}

// depotPodKind returns the kind of a pod of a depot, or "".
func (s *Simulation) depotPodKind(v *vehicle) string {
	_, bound := s.depots.slot(v.destination.ID)
	last := resource{kind: trackResource, id: s.depots.depots[0].incoming.ID, cell: s.laneCells[s.depots.depots[0].incoming.ID].count() - 1}
	switch {
	case v.faulted && (bound || s.depotParked(v)):
		return "faulted"
	case v.Pod.Activity == Idle && s.depotParked(v):
		return "idle"
	case s.depotParked(v):
		return "moving up"
	case bound && s.owners[resource{kind: berthResource, id: v.destination.ID}].isPod(v.Pod.ID):
		return "bound with its slot"
	case bound:
		return "bound without its slot"
	case s.depotUnbound(v, "parking") && s.owners[last].isPod(v.Pod.ID):
		return "unbound at the entry"
	case v.Pod.Activity == DepartingEmpty && s.depotUnbound(v, "parking"):
		return "departing to the entry"
	case v.Pod.Activity != Idle && s.depots.isDepot(v.origin.ID) || v.Pod.Activity != Idle && v.Pod.StationID == "parking":
		return "departing"
	}
	if _, ok := s.depots.slot(v.origin.ID); ok && v.Pod.Activity != Idle && !v.originReleased {
		return "departing"
	}
	return ""
}

// TestDepotRestore restores the state of a congested depot run at many
// ticks, from the tick at which three pods leave for the depot. Each
// restore must keep every pod in place and match the live simulation, and
// the samples must hold each kind of depot pod.
func TestDepotRestore(t *testing.T) {
	t.Parallel()
	network := shortDepotNetwork(2, 3)
	s := newDepotFleet(t, network, depotPlacements(network, 6))
	for _, id := range []string{"harbor-pod", "garden-pod", "market-pod"} {
		sendToDepot(t, s, id)
	}
	stations := []string{"harbor", "garden", "market"}
	const demandEnd = 6 * 60 * TicksPerSecond
	seconds := int64(10)
	seen := make(map[string]int)
	// sample restores the state when it holds a pod of a short-lived kind,
	// and at some other ticks.
	sample := func() {
		kinds := make(map[string]bool)
		for i := range s.vehicles {
			if kind := s.depotPodKind(&s.vehicles[i]); kind != "" {
				kinds[kind] = true
			}
		}
		if kinds["unbound at the entry"] || kinds["departing to the entry"] || kinds["moving up"] && s.tick%5 == 0 || s.tick%41 == 0 {
			restoreDepot(t, network, s)
			for kind := range kinds {
				seen[kind]++
			}
		}
	}
	sample()
	trips := 0
	stepDepot(t, s, 60*60*TicksPerSecond, func() bool {
		if s.tick < demandEnd && s.tick%(8*TicksPerSecond) == 0 {
			if _, err := s.SubmitTrip(stations[trips%3], stations[(trips+1+trips/3%2)%3]); err != nil {
				t.Fatal(err)
			}
			trips++
		}
		if s.tick < demandEnd && s.tick%(45*TicksPerSecond) == 0 {
			for i := range s.vehicles {
				if v := &s.vehicles[i]; !v.faulted && v.inService() && (s.depotParked(v) || s.depotPodKind(v) == "bound without its slot") {
					if _, err := s.Fault(FaultRequest{PodID: v.Pod.ID, DurationSeconds: &seconds}); err != nil {
						t.Fatal(err)
					}
					break
				}
			}
		}
		sample()
		return s.tick > demandEnd && len(s.faults) == 0 && s.completed == trips
	})
	for _, kind := range depotPodKinds {
		if seen[kind] == 0 {
			t.Errorf("no restore with a pod %s: %v", kind, seen)
		}
	}
}

// restoreDepotDeparture checks that v departs empty to the depot entry
// with no destination berth, and that both restore tiers accept the saved
// state. The physical restore must match s.
func restoreDepotDeparture(t *testing.T, network Network, s *Simulation, v *vehicle) {
	t.Helper()
	if v.Pod.Activity != DepartingEmpty || v.destination.ID != "" || v.RelocatingTo != "parking" || s.depotPodKind(v) != "departing to the entry" {
		t.Fatalf("pod %s is %s to %s %q", v.Pod.ID, v.Pod.Activity, v.RelocatingTo, v.destination.ID)
	}
	restoreDepot(t, network, s)
	_, result, err := RestoreState(RestoreStateInput{
		IncidentContract: IncidentV1Contract, FaultContract: FaultV1Contract, Faults: depotFaultSettings,
		Network: network, Fleet: s.initial, State: s.ExportState(), LogicalOnly: true,
	})
	if err != nil || result.Tier != RestoreLogical {
		t.Fatalf("logical restore %+v, error %v", result, err)
	}
}

// TestDepotDepartureRestore restores a pod that departs empty to the entry
// of a depot station, for each path that starts such a move. The pod has
// no destination berth until it binds a slot at the entry.
func TestDepotDepartureRestore(t *testing.T) {
	t.Parallel()
	t.Run("empty move", func(t *testing.T) {
		t.Parallel()
		network := shortDepotNetwork(2, 3)
		s := newDepotFleet(t, network, depotPlacements(network, 6))
		restoreDepotDeparture(t, network, s, sendToDepot(t, s, "harbor-pod"))
	})
	t.Run("blocking pod", func(t *testing.T) {
		t.Parallel()
		network := shortDepotNetwork(2, 3)
		s := newDepotFleet(t, network, depotPlacements(network, 1))
		v := s.findVehicle("garden-pod")
		if !s.park(v) {
			t.Fatal("no parking move")
		}
		restoreDepotDeparture(t, network, s, v)
	})
	t.Run("guarded bump", func(t *testing.T) {
		t.Parallel()
		network := shortDepotNetwork(2, 3)
		s := newDepotFleet(t, network, depotPlacements(network, 1))
		v := s.findVehicle("market-pod")
		if !s.guardedBumpToParking(guardedBump{blocker: v, from: "market-berth"}) {
			t.Fatal("no bump")
		}
		restoreDepotDeparture(t, network, s, v)
	})
	t.Run("released pod", func(t *testing.T) {
		t.Parallel()
		network := shortDepotNetwork(1, 3)
		s := newDepotFleet(t, network, depotPlacements(network, 0))
		// The pods at market and garden trade berths, so each passenger
		// berth is held or named (see TestDepotReleasedPark).
		v, other := s.findVehicle("market-pod"), s.findVehicle("garden-pod")
		if err := s.startEmptyMove(v, emptyDestination{station: "garden", berth: Berth{ID: "garden-1", Node: "garden-berth"}}); err != nil {
			t.Fatal(err)
		}
		if err := s.startEmptyMove(other, emptyDestination{station: "market", berth: Berth{ID: "market-1", Node: "market-berth"}}); err != nil {
			t.Fatal(err)
		}
		v.released = true
		s.parkUnclaimedReleased()
		restoreDepotDeparture(t, network, s, v)
	})
}

// TestDepotDepartureOverLimit departs a pod to the depot entry on a route
// that circles the network until it is longer than the saved route limit.
// The saved state leaves out the route. The physical tier must demote the
// pod to a berth, and the logical tier must accept the state.
func TestDepotDepartureOverLimit(t *testing.T) {
	t.Parallel()
	network := shortDepotNetwork(2, 3)
	s := newDepotFleet(t, network, depotPlacements(network, 6))
	lanes := func(ids ...string) []Lane {
		var route []Lane
		for _, id := range ids {
			route = append(route, s.network.Lanes[s.graph.lanes[id]])
		}
		return route
	}
	// toDepot runs from the exit of harbor to the depot entry, and loop
	// goes on from there back to the exit of harbor.
	toDepot := lanes("approach-branch", "bypass-in", "bypass-merge", "market-approach", "market-through", "return-start", "return-to-parking")
	loop := lanes("parking-through", "return", "harbor-approach", "harbor-through")
	route := lanes("harbor-out")
	for len(route) <= newRouteLimits(s.network).pod {
		route = append(append(route, toDepot...), loop...)
	}
	route = append(route, toDepot...)
	v := s.findVehicle("harbor-pod")
	s.installEmptyMove(v, emptyDestination{station: "parking", depot: true}, route)
	state := roundTripState(t, s.ExportState())
	if pod := state.Pods[slices.IndexFunc(state.Pods, func(p SavedPod) bool { return p.ID == "harbor-pod" })]; pod.Route != nil ||
		pod.Activity != activityCode(DepartingEmpty) || pod.Destination != "" || pod.RelocatingTo != "parking" {
		t.Fatalf("saved pod %+v", pod)
	}
	for _, logicalOnly := range []bool{false, true} {
		restored, result, err := RestoreState(RestoreStateInput{
			IncidentContract: IncidentV1Contract, FaultContract: FaultV1Contract, Faults: depotFaultSettings,
			Network: network, Fleet: s.initial, State: state, LogicalOnly: logicalOnly,
		})
		if err != nil {
			t.Fatalf("logical only %t: restore %+v, error %v", logicalOnly, result, err)
		}
		if logicalOnly {
			continue
		}
		if result.Tier != RestorePhysical || !slices.Equal(result.Demoted, []string{"harbor-pod"}) || result.OverCap != 1 {
			t.Fatalf("restore %+v", result)
		}
		checkAtBerth(t, restored, "harbor-pod", Idle, "harbor-1")
	}
}

// TestDepotDepartureRefusals checks that a pod that departs empty with no
// destination berth is refused, with the text and at the place of the
// refusal of any such pod, unless it goes to the entry of a depot station.
// A traveling pod with a refuge needs a destination berth, also when its
// route ends at a depot entry.
func TestDepotDepartureRefusals(t *testing.T) {
	t.Parallel()
	network := shortDepotNetwork(2, 3)
	s := newDepotFleet(t, network, depotPlacements(network, 6))
	sendToDepot(t, s, "harbor-pod")
	saved := s.ExportState()
	index := slices.IndexFunc(saved.Pods, func(p SavedPod) bool { return p.ID == "harbor-pod" })
	lane := func(id string) int { return s.graph.lanes[id] }
	noDepot := network
	noDepot.Depots = nil
	const noBerth = "pod harbor-pod: the pod has no destination berth"
	tests := []struct {
		name    string
		network Network
		change  func(*SavedPod)
		want    string
	}{
		{"no depot in the network", noDepot, func(*SavedPod) {}, noBerth},
		{"a passenger station", network, func(p *SavedPod) {
			p.RelocatingTo, p.DestinationStation = "garden", "garden"
			p.Route = []int{lane("harbor-out"), lane("approach-branch"), lane("garden-approach")}
		}, noBerth},
		{"a route short of the entry", network, func(p *SavedPod) { p.Route = p.Route[:len(p.Route)-1] }, noBerth},
		{"a route lane out of range", network, func(p *SavedPod) { p.Route[len(p.Route)-1] = len(network.Lanes) }, noBerth},
		{"no relocation", network, func(p *SavedPod) { p.RelocatingTo = "" }, "pod harbor-pod: the empty move does not go to the destination station"},
	}
	for _, test := range tests {
		state := saved
		state.Pods = slices.Clone(saved.Pods)
		state.Pods[index].Route = slices.Clone(state.Pods[index].Route)
		test.change(&state.Pods[index])
		if _, err := state.checkContract(test.network); err == nil || err.Error() != test.want {
			t.Errorf("%s: contract error %v, want %q", test.name, err, test.want)
		}
		for _, logicalOnly := range []bool{false, true} {
			_, result, err := RestoreState(RestoreStateInput{
				IncidentContract: IncidentV1Contract, FaultContract: FaultV1Contract, Faults: depotFaultSettings,
				Network: test.network, Fleet: s.initial, State: state, LogicalOnly: logicalOnly,
			})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Errorf("%s, logical only %t: restore %+v, error %v, want %q", test.name, logicalOnly, result, err, test.want)
			}
		}
	}
	refuge := saved.Pods[index]
	refuge.Activity, refuge.StationID, refuge.BerthID, refuge.Occupied = activityCode(Traveling), "", "", true
	refuge.RelocatingTo, refuge.Purpose = "", uint8(opRefuge)
	if !ruleForPod(refuge, phaseTravelingOccupied, network).hasDestination {
		t.Error("a refuge at a depot entry needs no destination berth")
	}
}

// TestDepotRestoreDemotion checks the slot of a demoted pod B. B takes the
// slot behind the rear of a lane where no pod moves, with the restore tick
// as its parked tick, and no slot in a lane where pod A moves, because A
// would have to pass B to reach its slot.
func TestDepotRestoreDemotion(t *testing.T) {
	t.Parallel()
	network := shortDepotNetwork(1, 3)
	s, err := NewFleet(network, []Placement{
		{ID: "harbor-pod", StationID: "harbor"}, {ID: "garden-pod", StationID: "garden"},
		{ID: "a-pod", StationID: "parking", BerthID: depotSlotID(1, 1)}, {ID: "b-pod", StationID: "parking", BerthID: depotSlotID(1, 2)},
	})
	if err != nil {
		t.Fatal(err)
	}
	for range 10 {
		s.Step()
	}
	lanes := func(ids ...string) []int {
		var indexes []int
		for _, id := range ids {
			indexes = append(indexes, s.graph.lanes[id])
		}
		return indexes
	}
	// bind makes a saved pod travel on the incoming lane, bound to a slot
	// and with no grant after the entry. Its origin is the berth at garden.
	bind := func(p *SavedPod, slot int, route []int) {
		p.Activity, p.StationID, p.BerthID, p.LaneID, p.LaneDistance, p.Distance = "traveling", "", "", "return-to-parking", 50, 50
		p.RelocatingTo, p.DestinationStation, p.Destination, p.Origin, p.Route, p.RouteIndex = "parking", "parking", depotSlotID(1, slot), "garden-1", route, 0
	}
	restore := func(t *testing.T, state SavedState) *vehicle {
		t.Helper()
		restored, result, err := RestoreState(RestoreStateInput{Network: network, Fleet: s.initial, State: state})
		if err != nil || result.Tier != RestorePhysical || len(result.Demoted) != 1 || result.Demoted[0] != "b-pod" {
			t.Fatalf("restore %+v, error %v", result, err)
		}
		return restored.findVehicle("b-pod")
	}
	t.Run("open lane", func(t *testing.T) {
		t.Parallel()
		// A is idle at slot 1, and B names the tail slot with a route that
		// demotes.
		state := s.ExportState()
		b := &state.Pods[slices.IndexFunc(state.Pods, func(p SavedPod) bool { return p.ID == "b-pod" })]
		bind(b, 3, lanes("return-to-parking", "parking-fan-1", "parking-in-1"))
		b.RouteIndex = -1
		v := restore(t, state)
		if v.Pod.Activity != Idle || v.Pod.BerthID != depotSlotID(1, 2) || v.parkedSince != state.Tick {
			t.Fatalf("B is at %s %s with the parked tick %d", v.Pod.StationID, v.Pod.BerthID, v.parkedSince)
		}
	})
	t.Run("moving lane", func(t *testing.T) {
		t.Parallel()
		// A travels to slot 1, and B names slot 2 with a route that
		// demotes.
		state := s.ExportState()
		a := &state.Pods[slices.IndexFunc(state.Pods, func(p SavedPod) bool { return p.ID == "a-pod" })]
		b := &state.Pods[slices.IndexFunc(state.Pods, func(p SavedPod) bool { return p.ID == "b-pod" })]
		bind(a, 1, lanes("return-to-parking", "parking-fan-1", "parking-in-1", "parking-chain-1-2", "parking-chain-1-1"))
		bind(b, 2, lanes("return-to-parking", "parking-fan-1", "parking-in-1", "parking-chain-1-2"))
		b.LaneID, b.RouteIndex = "return-start", -1
		if v := restore(t, state); v.Pod.BerthID != "market-1" {
			t.Fatalf("B is at %s %s", v.Pod.StationID, v.Pod.BerthID)
		}
	})
}

// TestDepotSavedRefusals checks the refusals of the depot fields of a saved
// state, their text, and their order, and that the lane order check accepts
// a rear pod that names a slot which the pod ahead still crosses.
func TestDepotSavedRefusals(t *testing.T) {
	t.Parallel()
	network := shortDepotNetwork(1, 3)
	s, err := NewFleet(network, depotPlacements(network, 1))
	if err != nil {
		t.Fatal(err)
	}
	for range 10 {
		s.Step()
	}
	// pod returns the saved pod with an ID.
	pod := func(state *SavedState, id string) *SavedPod {
		for i := range state.Pods {
			if state.Pods[i].ID == id {
				return &state.Pods[i]
			}
		}
		t.Fatalf("no pod %s", id)
		return nil
	}
	lane := func(id string) int { return s.graph.lanes[id] }
	// bind makes a saved pod travel on a lane of the depot lane, bound to a
	// slot.
	bind := func(p *SavedPod, laneID string, distance float64, slot int) {
		route := []int{lane(laneID)}
		for _, next := range []string{"parking-in-1", "parking-chain-1-2", "parking-chain-1-1"} {
			if lane(next) > 0 && len(route) > 0 && s.network.Lanes[route[len(route)-1]].To == s.network.Lanes[lane(next)].From {
				route = append(route, lane(next))
			}
		}
		p.Activity, p.StationID, p.BerthID, p.LaneID, p.LaneDistance, p.Distance = "traveling", "", "", laneID, distance, distance
		p.RelocatingTo, p.DestinationStation, p.Destination, p.Route, p.RouteIndex = "parking", "parking", depotSlotID(1, slot), route, 0
	}
	tests := []struct {
		name   string
		change func(*SavedState)
		want   string
	}{
		{"negative", func(state *SavedState) { pod(state, "depot-pod-01").ParkedSince = -1 }, `pod "depot-pod-01" has the negative parked tick -1`},
		{"after the saved tick", func(state *SavedState) { pod(state, "depot-pod-01").ParkedSince = 11 }, `pod "depot-pod-01" has the parked tick 11 after the saved tick 10`},
		{"first pod first", func(state *SavedState) {
			pod(state, "depot-pod-01").ParkedSince = 11
			pod(state, "harbor-pod").ParkedSince = -1
		}, `pod "depot-pod-01" has the parked tick 11 after the saved tick 10`},
		{"not parked", func(state *SavedState) { pod(state, "harbor-pod").ParkedSince = 5 }, `pod "harbor-pod" has a parked tick and is not parked in a depot lane`},
		{"range before place", func(state *SavedState) {
			pod(state, "depot-pod-01").ParkedSince = 11
			pod(state, "harbor-pod").ParkedSince = 5
		}, `pod "depot-pod-01" has the parked tick 11 after the saved tick 10`},
		{"one slot", func(state *SavedState) { bind(pod(state, "market-pod"), "parking-in-1", 1, 1) }, `depot station "parking" lane 1 has pods out of slot order`},
		{"reverse order", func(state *SavedState) {
			bind(pod(state, "market-pod"), "parking-in-1", 1, 3)
			bind(pod(state, "garden-pod"), "parking-fan-1", 1, 2)
		}, `depot station "parking" lane 1 has pods out of slot order`},
		{"place before order", func(state *SavedState) {
			bind(pod(state, "market-pod"), "parking-in-1", 1, 1)
			pod(state, "harbor-pod").ParkedSince = 5
		}, `pod "harbor-pod" has a parked tick and is not parked in a depot lane`},
		{"crossed slot", func(state *SavedState) {
			bind(pod(state, "market-pod"), "parking-chain-1-2", 1, 2)
			bind(pod(state, "garden-pod"), "parking-in-1", 1, 3)
		}, ""},
	}
	for _, test := range tests {
		state := s.ExportState()
		test.change(&state)
		got := checkSavedDepotFields(RestoreStateInput{Network: network, Fleet: s.initial, State: state})
		switch {
		case test.want == "" && got != nil:
			t.Errorf("%s: %v", test.name, got)
		case test.want != "" && (got == nil || got.Error() != test.want):
			t.Errorf("%s: got %v, want %q", test.name, got, test.want)
		}
		if test.want == "" {
			continue
		}
		if _, _, got := RestoreState(RestoreStateInput{Network: network, Fleet: s.initial, State: state}); got == nil || got.Error() != test.want {
			t.Errorf("%s: restore got %v, want %q", test.name, got, test.want)
		}
	}
	// A network without a depot refuses each parked tick.
	plain, err := NewFleet(Example(), []Placement{{ID: "01", StationID: "harbor"}})
	if err != nil {
		t.Fatal(err)
	}
	plain.Step()
	state := plain.ExportState()
	state.Pods[0].ParkedSince = state.Tick
	want := `pod "01" has a parked tick and is not parked in a depot lane`
	if _, _, err := RestoreState(RestoreStateInput{Network: Example(), Fleet: plain.initial, State: state}); err == nil || err.Error() != want {
		t.Errorf("no depot: restore got %v, want %q", err, want)
	}
}

// TestDepotSavedPlatoonLink checks that a saved platoon run that holds a
// depot lane fails the physical tier after the other link checks, and that
// the logical tier restores the state.
func TestDepotSavedPlatoonLink(t *testing.T) {
	t.Parallel()
	network := shortDepotNetwork(1, 3)
	// One speed limit lets the run reach the depot check.
	for i := range network.Lanes {
		network.Lanes[i].SpeedLimit = depotSpeed
	}
	s, err := NewFleet(network, depotPlacements(network, 2))
	if err != nil {
		t.Fatal(err)
	}
	state := s.ExportState()
	route := func(ids ...string) []int {
		var indexes []int
		for _, id := range ids {
			indexes = append(indexes, s.graph.lanes[id])
		}
		return indexes
	}
	out := route("parking-out-1", "parking-throat-1", "return", "harbor-approach", "harbor-in")
	for i := range state.Pods {
		p := &state.Pods[i]
		var distance float64
		switch p.ID {
		case "depot-pod-01":
			p.Origin, distance = depotSlotID(1, 1), 20
		case "depot-pod-02":
			p.Origin, distance = depotSlotID(1, 2), 5
			p.Platoon = &SavedPlatoonLink{Leader: "depot-pod-01", Lane: 0, LeaderLane: 0, Lanes: 1}
		default:
			continue
		}
		p.Activity, p.StationID, p.BerthID, p.LaneID, p.LaneDistance, p.Distance = "traveling", "", "", "parking-out-1", distance, distance
		p.RelocatingTo, p.DestinationStation, p.Destination, p.Route, p.RouteIndex = "harbor", "harbor", "harbor-1", out, 0
	}
	for i := range state.Pods {
		if state.Pods[i].ID == "harbor-pod" {
			state.Pods[i].StationID, state.Pods[i].BerthID = "garden", "garden-1"
		}
		if state.Pods[i].ID == "garden-pod" {
			state.Pods[i].StationID, state.Pods[i].BerthID = "parking", depotSlotID(1, 1)
		}
	}
	_, result, err := RestoreState(RestoreStateInput{Network: network, Fleet: s.initial, State: state})
	if err != nil || result.Tier != RestoreLogical || result.PhysicalError == nil || !strings.HasSuffix(result.PhysicalError.Error(), "pod depot-pod-02: the platoon run holds a lane of a depot") {
		t.Fatalf("restore %+v, error %v", result, err)
	}
}
