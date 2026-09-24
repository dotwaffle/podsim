package sim

import (
	"errors"
	"math"
	"reflect"
	"slices"
	"testing"
)

func newExample(t *testing.T) *Simulation {
	t.Helper()
	s, err := New(Example(), "harbor")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func advance(s *Simulation, ticks int) {
	for range ticks {
		s.Step()
	}
}

func TestRoutes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, from, to string
		want           []string
	}{
		{"branch", "harbor-berth", "garden-berth", []string{"harbor-out", "approach-branch", "garden-approach", "garden-in"}},
		{"bypass", "harbor-berth", "market-berth", []string{"harbor-out", "approach-branch", "bypass-in", "bypass-merge", "market-approach", "market-in"}},
		{"merge", "garden-berth", "market-berth", []string{"garden-out", "garden-merge", "market-approach", "market-in"}},
		{"return", "market-berth", "harbor-berth", []string{"market-out", "return-start", "return-to-parking", "parking-through", "return", "harbor-approach", "harbor-in"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			route, err := Example().Route(tc.from, tc.to)
			if err != nil {
				t.Fatal(err)
			}
			var ids []string
			for _, lane := range route {
				ids = append(ids, lane.ID)
			}
			if !slices.Equal(ids, tc.want) {
				t.Fatalf("route = %v, want %v", ids, tc.want)
			}
		})
	}
}

func TestJourneyLifecycle(t *testing.T) {
	t.Parallel()
	for _, destination := range []string{"garden", "market"} {
		t.Run(destination, func(t *testing.T) {
			t.Parallel()
			s := newExample(t)
			if err := s.RequestJourney("01", destination); err != nil {
				t.Fatal(err)
			}
			advance(s, boardingTicks-1)
			before := s.Snapshot()
			if before.Vehicles[0].Pod.Activity != Boarding || before.Vehicles[0].Pod.BerthID != "harbor-1" || before.Vehicles[0].Pod.Occupied {
				t.Fatalf("boarding state: %+v", before.Vehicles[0].Pod)
			}
			s.Step()
			departed := s.Snapshot()
			if departed.Vehicles[0].Pod.Activity != Traveling || departed.Vehicles[0].Pod.BerthID != "" || !departed.Vehicles[0].Pod.Occupied {
				t.Fatalf("departure state: %+v", departed.Vehicles[0].Pod)
			}
			for range 300 * TicksPerSecond {
				s.Step()
				state := s.Snapshot()
				if state.Vehicles[0].Pod.Speed < 0 || state.Vehicles[0].Pod.Speed > 14 || !finite(state.Vehicles[0].Pod.Position.X) || !finite(state.Vehicles[0].Pod.Position.Y) {
					t.Fatalf("invalid movement: %+v", state.Vehicles[0].Pod)
				}
				if state.Vehicles[0].Pod.Activity != Traveling {
					break
				}
			}
			arrived := s.Snapshot()
			station, _ := Example().Station(destination)
			node, _ := Example().Node(station.Berths[0].Node)
			if arrived.Vehicles[0].Pod.Activity != Unloading || arrived.Vehicles[0].Pod.Position != node.Position || arrived.Vehicles[0].Pod.BerthID != station.Berths[0].ID || arrived.Vehicles[0].Pod.Speed != 0 {
				t.Fatalf("arrival state: %+v", arrived.Vehicles[0].Pod)
			}
			if arrived.Completed != 0 || arrived.Vehicles[0].Request.Completed {
				t.Fatal("journey completed before unloading")
			}
			advance(s, unloadingTicks-1)
			if s.Snapshot().Vehicles[0].Pod.Activity != Unloading {
				t.Fatal("unloading ended early")
			}
			s.Step()
			completed := s.Snapshot()
			if completed.Vehicles[0].Pod.Activity != Idle || completed.Vehicles[0].Pod.Occupied || completed.Completed != 1 || !completed.Vehicles[0].Request.Completed {
				t.Fatalf("completion state: %+v", completed)
			}
			if err := s.RequestJourney("01", "harbor"); err != nil {
				t.Fatalf("request return journey: %v", err)
			}
			advance(s, 300*TicksPerSecond)
			if got := s.Snapshot(); got.Completed != 2 || got.Vehicles[0].Pod.StationID != "harbor" {
				t.Fatalf("return journey: %+v", got)
			}
		})
	}
}

func TestStationManeuverPhases(t *testing.T) {
	t.Parallel()
	network := Example()
	roles := map[string]struct {
		station string
		role    StationLaneRole
	}{
		"approach-branch": {station: "harbor", role: StationExitRole},
		"bypass-merge":    {station: "market", role: StationApproachRole},
		"market-approach": {station: "market", role: StationEntryRole},
	}
	for index := range network.Lanes {
		if role, ok := roles[network.Lanes[index].ID]; ok {
			network.Lanes[index].StationID = role.station
			network.Lanes[index].StationRole = role.role
		}
	}
	s, err := New(network, "harbor")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RequestJourney("01", "market"); err != nil {
		t.Fatal(err)
	}
	type observation struct {
		phase   StationPhase
		station string
	}
	var got []observation
	record := func() {
		pod := s.Snapshot().Vehicles[0].Pod
		current := observation{phase: pod.StationPhase, station: pod.ManeuverStationID}
		if current.phase != "" && (len(got) == 0 || got[len(got)-1] != current) {
			got = append(got, current)
		}
	}
	record()
	for range 300 * TicksPerSecond {
		s.Step()
		record()
		if s.Snapshot().Completed == 1 {
			break
		}
	}
	want := []observation{
		{phase: AtBerth, station: "harbor"},
		{phase: DepartingBerth, station: "harbor"},
		{phase: ExitingStation, station: "harbor"},
		{phase: ApproachingStation, station: "market"},
		{phase: EnteringStation, station: "market"},
		{phase: AccessingBerth, station: "market"},
		{phase: AtBerth, station: "market"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("station phases = %+v, want %+v", got, want)
	}
}

func TestRejectedRequestsDoNotMutate(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, to string
		busy     bool
		want     error
	}{
		{"same station", "harbor", false, ErrSameStation},
		{"unknown station", "missing", false, nil},
		{"busy", "market", true, ErrBusy},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := newExample(t)
			if tc.busy {
				if err := s.RequestJourney("01", "garden"); err != nil {
					t.Fatal(err)
				}
			}
			before := s.Snapshot()
			err := s.RequestJourney("01", tc.to)
			if err == nil || (tc.want != nil && !errors.Is(err, tc.want)) {
				t.Fatalf("request error = %v, want %v", err, tc.want)
			}
			if !reflect.DeepEqual(before, s.Snapshot()) {
				t.Fatal("rejected request changed state")
			}
		})
	}
}

func TestPauseResetAndRepeatability(t *testing.T) {
	t.Parallel()
	for _, ticks := range []int{0, 30, boardingTicks + 600, 300 * TicksPerSecond} {
		t.Run(stringPhase(ticks), func(t *testing.T) {
			t.Parallel()
			s := newExample(t)
			initial := s.Snapshot()
			if err := s.RequestJourney("01", "market"); err != nil {
				t.Fatal(err)
			}
			advance(s, ticks)
			s.SetPaused(true)
			paused := s.Snapshot()
			advance(s, 500)
			if !reflect.DeepEqual(paused, s.Snapshot()) {
				t.Fatal("pause changed state")
			}
			s.Reset()
			if !reflect.DeepEqual(initial, s.Snapshot()) {
				t.Fatal("reset did not restore the initial state")
			}
			if err := s.RequestJourney("01", "market"); err != nil {
				t.Fatal(err)
			}
			advance(s, ticks)
			s.SetPaused(true)
			if !reflect.DeepEqual(paused, s.Snapshot()) {
				t.Fatal("repeated run changed state")
			}
		})
	}
}

func stringPhase(ticks int) string {
	switch ticks {
	case 0:
		return "request"
	case 30:
		return "boarding"
	case boardingTicks + 600:
		return "travel"
	default:
		return "complete"
	}
}

func TestPlaybackGrouping(t *testing.T) {
	t.Parallel()
	a, b := newExample(t), newExample(t)
	for _, s := range []*Simulation{a, b} {
		if err := s.RequestJourney("01", "market"); err != nil {
			t.Fatal(err)
		}
	}
	advance(a, 8000)
	for range 1000 {
		advance(b, 8)
	}
	if !reflect.DeepEqual(a.Snapshot(), b.Snapshot()) {
		t.Fatal("step grouping changed simulation results")
	}
}

func TestScenarioAndSnapshotIsolation(t *testing.T) {
	t.Parallel()
	network := Example()
	s, err := New(network, "harbor")
	if err != nil {
		t.Fatal(err)
	}
	network.Nodes[0].Position.X = -999
	network.Lanes[0].SpeedLimit = -1
	network.Stations[0].Berths[0].Node = "missing"
	if err := s.RequestJourney("01", "market"); err != nil {
		t.Fatal(err)
	}
	snapshot := s.Snapshot()
	snapshot.Vehicles[0].Request.To = "missing"
	snapshot.Vehicles[0].Route[0].To = "missing"
	advance(s, 300*TicksPerSecond)
	if got := s.Snapshot(); got.Completed != 1 || got.Vehicles[0].Pod.StationID != "market" {
		t.Fatalf("external mutation changed simulation: %+v", got)
	}
}

func TestSafetyObservationMatchesSnapshotAndIsIsolated(t *testing.T) {
	s := newExample(t)
	assertSafetyObservation(t, safetyObservationInput{simulation: s, name: "initial idle"})
	if err := s.RequestTrip("market", "harbor"); err != nil {
		t.Fatal(err)
	}
	assertSafetyObservation(t, safetyObservationInput{simulation: s, name: "pending"})
	advanceToObservationState(t, safetyObservationInput{simulation: s, name: "moving to pickup", match: func(state Snapshot) bool {
		return state.Vehicles[0].Pod.Activity == Traveling && state.Vehicles[0].Pod.LaneID != ""
	}})
	advanceToObservationState(t, safetyObservationInput{simulation: s, name: "boarding", match: func(state Snapshot) bool {
		return state.Vehicles[0].Pod.Activity == Boarding
	}})
	advanceToObservationState(t, safetyObservationInput{simulation: s, name: "moving with passenger", match: func(state Snapshot) bool {
		return state.Vehicles[0].Pod.Activity == Traveling && state.Vehicles[0].Pod.Occupied && state.Vehicles[0].Pod.LaneID != ""
	}})
	advanceToObservationState(t, safetyObservationInput{simulation: s, name: "unloading", match: func(state Snapshot) bool {
		return state.Vehicles[0].Pod.Activity == Unloading
	}})
	advanceToObservationState(t, safetyObservationInput{simulation: s, name: "completed idle", match: func(state Snapshot) bool {
		return state.Completed == 1 && state.Vehicles[0].Pod.Activity == Idle
	}})

	observation := s.SafetyObservation()
	observation.Pods[0].ID = "changed"
	observation.Berths[0].Occupant = "changed"
	observation.Locations[observation.Pods[0].ID] = SafetyLocation{SeparationGroup: "changed"}
	got := s.SafetyObservation()
	if got.Pods[0].ID == "changed" || got.Berths[0].Occupant == "changed" || got.Locations["changed"].SeparationGroup == "changed" {
		t.Fatalf("safety observation exposes simulation storage: %+v", got)
	}
}

type safetyObservationInput struct {
	simulation *Simulation
	name       string
	match      func(Snapshot) bool
}

func advanceToObservationState(t *testing.T, input safetyObservationInput) {
	t.Helper()
	for range 300 * TicksPerSecond {
		if input.match(input.simulation.Snapshot()) {
			assertSafetyObservation(t, input)
			return
		}
		input.simulation.Step()
	}
	t.Fatalf("did not reach %s: %+v", input.name, input.simulation.Snapshot())
}

func assertSafetyObservation(t *testing.T, input safetyObservationInput) {
	t.Helper()
	s := input.simulation
	snapshot := s.Snapshot()
	observation := s.SafetyObservation()
	pods := make([]Pod, len(snapshot.Vehicles))
	for index := range snapshot.Vehicles {
		pods[index] = snapshot.Vehicles[index].Pod
	}
	var berths []BerthState
	locations := make(map[string]SafetyLocation, len(s.vehicles))
	for _, vehicle := range s.vehicles {
		if vehicle.Pod.LaneID != "" {
			locations[vehicle.Pod.ID] = s.laneSafety[vehicle.Pod.LaneID]
		} else if vehicle.Pod.BerthID != "" {
			locations[vehicle.Pod.ID] = s.berthSafety[vehicle.Pod.BerthID]
		}
	}
	for _, station := range s.network.Stations {
		for _, berth := range station.Berths {
			state := BerthState{ID: berth.ID, ReservedBy: s.owners[resource{kind: berthResource, id: berth.ID}]}
			for _, vehicle := range s.vehicles {
				if vehicle.Pod.BerthID == berth.ID {
					state.Occupant = vehicle.Pod.ID
				}
			}
			berths = append(berths, state)
		}
	}
	if observation.Tick != snapshot.Tick || observation.Completed != snapshot.Completed || observation.Pending != len(snapshot.Pending) ||
		!reflect.DeepEqual(observation.Pods, pods) || !reflect.DeepEqual(observation.Berths, berths) ||
		!reflect.DeepEqual(observation.Locations, locations) || !reflect.DeepEqual(snapshot.Berths, berths) {
		t.Fatalf("%s safety observation differs: observation=%+v snapshot=%+v independent_berths=%+v", input.name, observation, snapshot, berths)
	}
}

func TestInvalidNetwork(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		change func(*Network)
	}{
		{"duplicate node", func(n *Network) { n.Nodes[1].ID = n.Nodes[0].ID }},
		{"missing endpoint", func(n *Network) { n.Lanes[0].To = "missing" }},
		{"zero speed", func(n *Network) { n.Lanes[0].SpeedLimit = 0 }},
		{"nan coordinate", func(n *Network) { n.Nodes[0].Position.X = math.NaN() }},
		{"missing berth", func(n *Network) { n.Stations[0].Berths = nil }},
		{"shared berth node", func(n *Network) { n.Stations[1].Berths[0].Node = n.Stations[0].Berths[0].Node }},
		{"same entry and exit", func(n *Network) { n.Stations[0].Exit = n.Stations[0].Entry }},
		{"missing through lane", func(n *Network) {
			n.Lanes = slices.DeleteFunc(n.Lanes, func(l Lane) bool { return l.ID == "harbor-through" })
		}},
		{"berth on entry", func(n *Network) { n.Stations[0].Berths[0].Node = n.Stations[0].Entry }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			network := Example()
			tc.change(&network)
			if _, err := New(network, "harbor"); err == nil {
				t.Fatal("invalid network accepted")
			}
		})
	}
}

func TestUnreachableRequest(t *testing.T) {
	t.Parallel()
	n := Example()
	n.Lanes = slices.DeleteFunc(n.Lanes, func(l Lane) bool { return l.ID == "garden-approach" })
	s, err := New(n, "harbor")
	if err != nil {
		t.Fatal(err)
	}
	before := s.Snapshot()
	if err := s.RequestJourney("01", "garden"); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("error = %v, want ErrUnreachable", err)
	}
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("unreachable request changed state")
	}
}

// checkVehicleIndex checks that vehicleIndexes gives the position of each pod
// and has no other entry, so that findVehicle does not scan. It also checks
// the lookups of an empty ID and of an unknown ID.
func checkVehicleIndex(t *testing.T, name string, s *Simulation) {
	t.Helper()
	if len(s.vehicleIndexes) != len(s.vehicles) {
		t.Errorf("%s: the index has %d entries for %d pods", name, len(s.vehicleIndexes), len(s.vehicles))
	}
	for index := range s.vehicles {
		id := s.vehicles[index].Pod.ID
		if got, ok := s.vehicleIndexes[id]; !ok || got != index {
			t.Errorf("%s: pod %s has index %d (found %t), want %d", name, id, got, ok, index)
		}
		if s.findVehicle(id) != &s.vehicles[index] {
			t.Errorf("%s: findVehicle(%q) is not the pod at %d", name, id, index)
		}
	}
	if s.findVehicle("") != nil || s.findVehicle("unknown") != nil {
		t.Errorf("%s: findVehicle found a pod for an empty or unknown ID", name)
	}
}

func TestVehicleIndexFollowsFleetChanges(t *testing.T) {
	t.Parallel()
	s := newTraffic(t)
	checkVehicleIndex(t, "new fleet", s)
	if err := s.StartDemo(); err != nil {
		t.Fatal(err)
	}
	checkVehicleIndex(t, "demo", s)
	advance(s, 20*TicksPerSecond)
	checkVehicleIndex(t, "clone", s.Clone())
	// The saved state has the parked demo pods, which the fleet does not have.
	state := roundTripState(t, s.ExportState())
	for _, logicalOnly := range []bool{false, true} {
		restored, result, err := RestoreState(RestoreStateInput{Network: Example(), Fleet: demoFleet(), State: state, LogicalOnly: logicalOnly})
		if err != nil {
			t.Fatal(err)
		}
		if !logicalOnly && (result.Tier != RestorePhysical || len(restored.vehicles) != 4) {
			t.Fatalf("the restore has tier %s and %d pods, want the physical tier and 4 pods", result.Tier, len(restored.vehicles))
		}
		checkVehicleIndex(t, "restore tier "+string(result.Tier), restored)
	}
	s.Reset()
	checkVehicleIndex(t, "reset", s)
}

func TestFindVehicleScansAStaleIndex(t *testing.T) {
	t.Parallel()
	s := newTraffic(t)
	s.vehicles[0], s.vehicles[1] = s.vehicles[1], s.vehicles[0]
	for _, indexes := range []map[string]int{s.vehicleIndexes, nil} {
		s.vehicleIndexes = indexes
		for index := range s.vehicles {
			if id := s.vehicles[index].Pod.ID; s.findVehicle(id) != &s.vehicles[index] {
				t.Errorf("findVehicle(%q) is not the pod at %d", id, index)
			}
		}
	}
}
