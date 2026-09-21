package sim

import (
	"math"
	"reflect"
	"testing"
)

func newTraffic(t *testing.T) *Simulation {
	t.Helper()
	s, err := NewFleet(Example(), []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"}})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// checkTraffic measures visible positions, independently of controller ownership.
func checkTraffic(t *testing.T, state Snapshot) {
	t.Helper()
	for i, a := range state.Vehicles {
		if !finite(a.Pod.Position.X) || !finite(a.Pod.Position.Y) || a.Pod.Speed < 0 || a.Pod.Speed > 14 {
			t.Fatalf("invalid pod at tick %d: %+v", state.Tick, a.Pod)
		}
		for _, b := range state.Vehicles[i+1:] {
			gap := math.Hypot(a.Pod.Position.X-b.Pod.Position.X, a.Pod.Position.Y-b.Pod.Position.Y)
			if gap < Clearance-1e-6 {
				t.Fatalf("tick %d: pods %s and %s only %.5fm apart: %+v %+v", state.Tick, a.Pod.ID, b.Pod.ID, gap, a.Pod, b.Pod)
			}
		}
	}
	for _, b := range state.Berths {
		occupants := 0
		for _, v := range state.Vehicles {
			if v.Pod.BerthID == b.ID {
				occupants++
				if b.Occupant != v.Pod.ID || b.ReservedBy != v.Pod.ID {
					t.Fatalf("invalid berth state %+v", b)
				}
			}
		}
		if occupants > 1 {
			t.Fatalf("berth capacity exceeded: %+v", b)
		}
	}
}

func TestTrafficDemoSafetyAndProgress(t *testing.T) {
	t.Parallel()
	s := newTraffic(t)
	if err := s.StartDemo(); err != nil {
		t.Fatal(err)
	}
	waits := make(map[WaitReason]int)
	slows := make(map[WaitReason]int)
	mergeSlows := 0
	mergePasses := make(map[string]bool)
	for range 900 * TicksPerSecond {
		s.Step()
		state := s.Snapshot()
		checkTraffic(t, state)
		for _, v := range state.Vehicles {
			if v.Pod.WaitReason != NoWait {
				if waits[v.Pod.WaitReason] == 0 {
					t.Logf("first %s: tick %d pod %s", v.Pod.WaitReason, state.Tick, v.Pod.ID)
				}
				waits[v.Pod.WaitReason]++
				if v.Pod.Speed < 13 {
					slows[v.Pod.WaitReason]++
					if v.Pod.LaneID == "garden-merge" || v.Pod.LaneID == "bypass-merge" {
						mergeSlows++
					}
				}
			}
			if v.Pod.LaneID == "market-approach" {
				mergePasses[v.Pod.ID] = true
			}
		}
		if state.Completed == demoJourneys && !state.Demo {
			break
		}
	}
	state := s.Snapshot()
	t.Logf("finished tick %d, waits %v, slows %v, passes %v", state.Tick, waits, slows, mergePasses)
	if state.Completed != demoJourneys || state.Demo {
		t.Fatalf("demo did not finish: %+v", state)
	}
	if len(mergePasses) != 4 {
		t.Fatalf("all pods did not pass merge: %v", mergePasses)
	}
	if waits[JunctionOccupied] == 0 || mergeSlows == 0 {
		t.Fatal("demo did not exercise junction contention")
	}
	if waits[BerthOccupied] == 0 {
		t.Fatal("demo did not exercise a full berth")
	}
}

func TestFullBerthWaitAndClearance(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(withoutParking(), []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "market"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RequestJourney("01", "market"); err != nil {
		t.Fatal(err)
	}
	blockPassengerClearingRoutes(s)
	advance(s, 180*TicksPerSecond)
	state := s.Snapshot()
	checkTraffic(t, state)
	waiting := state.Vehicles[0].Pod
	if waiting.WaitReason != ParkingUnavailable || waiting.Speed > 0.01 || waiting.LaneID != "market-in" || waiting.BerthID != "" {
		t.Fatalf("arrival must wait on the inlet outside the occupied berth: %+v", waiting)
	}
	if state.Completed != 0 {
		t.Fatal("blocked arrival completed")
	}
	delete(s.routes, routeKey{from: "market-berth", to: "garden-berth"})
	if err = s.RequestJourney("02", "garden"); err != nil {
		t.Fatal(err)
	}
	for range 360 * TicksPerSecond {
		s.Step()
		checkTraffic(t, s.Snapshot())
		if s.Snapshot().Completed == 2 {
			break
		}
	}
	if state = s.Snapshot(); state.Completed != 2 {
		t.Fatalf("berth departure did not release waiting arrival: %+v", state)
	}
}

func TestFleetResetPauseAndOrder(t *testing.T) {
	t.Parallel()
	a := newTraffic(t)
	b, err := NewFleet(Example(), []Placement{{ID: "02", StationID: "garden"}, {ID: "01", StationID: "harbor"}})
	if err != nil {
		t.Fatal(err)
	}
	initial := a.Snapshot()
	for _, s := range []*Simulation{a, b} {
		if err := s.StartDemo(); err != nil {
			t.Fatal(err)
		}
	}
	for range 120 * TicksPerSecond {
		a.Step()
		b.Step()
		if !reflect.DeepEqual(a.Snapshot(), b.Snapshot()) {
			t.Fatalf("fleet order changed outcome at tick %d", a.Snapshot().Tick)
		}
	}
	a.SetPaused(true)
	paused := a.Snapshot()
	advance(a, 600)
	if !reflect.DeepEqual(paused, a.Snapshot()) {
		t.Fatal("pause advanced fleet or script")
	}
	a.Reset()
	if !reflect.DeepEqual(initial, a.Snapshot()) {
		t.Fatal("reset left fleet or resource state")
	}
}

func TestFleetValidation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		placements []Placement
	}{
		{"empty", nil},
		{"duplicate ID", []Placement{{ID: "01", StationID: "harbor"}, {ID: "01", StationID: "garden"}}},
		{"duplicate berth", []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "harbor"}}},
		{"unknown station", []Placement{{ID: "01", StationID: "missing"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewFleet(Example(), tc.placements); err == nil {
				t.Fatal("invalid fleet accepted")
			}
		})
	}
}

func TestQueueBehindStoppedPod(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(withoutParking(), []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"}, {ID: "03", StationID: "market"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"01", "02"} {
		if err := s.RequestJourney(id, "market"); err != nil {
			t.Fatal(err)
		}
	}
	blockPassengerClearingRoutes(s)
	followed := false
	for range 240 * TicksPerSecond {
		s.Step()
		state := s.Snapshot()
		checkTraffic(t, state)
		for _, v := range state.Vehicles {
			if v.Pod.WaitReason == TrackOccupied {
				followed = true
			}
		}
	}
	state := s.Snapshot()
	a, b := state.Vehicles[0].Pod, state.Vehicles[1].Pod
	if !followed || a.LaneID != "market-in" || b.LaneID != "market-in" || a.Speed > 0.01 || b.Speed > 0.01 {
		t.Fatalf("no stopped inlet queue: followed=%v pods=%+v %+v", followed, a, b)
	}
	if state.Completed != 0 {
		t.Fatal("a blocked arrival completed")
	}
}

func TestFullBerthDoesNotBlockThroughLane(t *testing.T) {
	t.Parallel()
	for _, short := range []bool{false, true} {
		name := "supplied inlet"
		if short {
			name = "two-cell inlet"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			network := withoutParking()
			if short {
				for i := range network.Nodes {
					if network.Nodes[i].ID == "market-berth" {
						network.Nodes[i].Position = Point{X: 730, Y: 310}
					}
				}
			}
			checkThroughTraffic(t, network)
		})
	}
}

func checkThroughTraffic(t *testing.T, network Network) {
	t.Helper()
	s, err := NewFleet(network, []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "market"}, {ID: "03", StationID: "garden"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RequestJourney("01", "market"); err != nil {
		t.Fatal(err)
	}
	blockPassengerClearingRoutes(s)
	advance(s, 180*TicksPerSecond)
	if got := s.Snapshot().Vehicles[0].Pod; got.WaitReason != ParkingUnavailable {
		t.Fatalf("expected berth queue: %+v", got)
	}
	if err := s.RequestJourney("03", "harbor"); err != nil {
		t.Fatal(err)
	}
	passed := false
	for range 360 * TicksPerSecond {
		s.Step()
		state := s.Snapshot()
		checkTraffic(t, state)
		if state.Vehicles[2].Pod.LaneID == "market-through" {
			passed = true
		}
		if state.Completed == 1 {
			break
		}
	}
	state := s.Snapshot()
	if !passed || state.Completed != 1 || state.Vehicles[2].Pod.StationID != "harbor" {
		t.Fatalf("through traffic blocked: passed=%v state=%+v", passed, state)
	}
	if state.Vehicles[0].Pod.WaitReason != ParkingUnavailable || state.Vehicles[1].Pod.BerthID != "market-1" {
		t.Fatal("the occupied berth unexpectedly cleared")
	}
}

func TestDemoRejectsBrokenScriptBeforeReset(t *testing.T) {
	t.Parallel()
	for _, broken := range []string{"bypass-in", "garden-merge", "return-start"} {
		t.Run(broken, func(t *testing.T) {
			t.Parallel()
			n := Example()
			for i := range n.Lanes {
				if n.Lanes[i].ID != broken {
					continue
				}
				if broken == "bypass-in" {
					n.Lanes[i].ID = "renamed"
				} else {
					n.Lanes = append(n.Lanes[:i], n.Lanes[i+1:]...)
				}
				break
			}
			s, err := NewFleet(n, []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"}})
			if err != nil {
				t.Fatal(err)
			}
			advance(s, 50)
			before := s.Snapshot()
			if err := s.StartDemo(); err == nil {
				t.Fatal("invalid demo accepted")
			}
			if !reflect.DeepEqual(before, s.Snapshot()) {
				t.Fatal("invalid demo reset the current simulation")
			}
		})
	}
}

func TestDemoReportsInterruptedRequest(t *testing.T) {
	t.Parallel()
	s := newTraffic(t)
	if err := s.StartDemo(); err != nil {
		t.Fatal(err)
	}
	if err := s.RequestJourney("02", "harbor"); err != nil {
		t.Fatal(err)
	}
	for range 120 * TicksPerSecond {
		s.Step()
		if !s.Snapshot().Demo {
			break
		}
	}
	state := s.Snapshot()
	if state.Demo || state.DemoError == "" {
		t.Fatalf("demo failed silently: %+v", state)
	}
}

// withoutParking retains the track geometry but provides no empty-pod destination.
func withoutParking() Network {
	n := Example()
	n.Stations = n.Stations[:3]
	return n
}

// blockPassengerClearingRoutes keeps these tests focused on an unavailable berth destination.
func blockPassengerClearingRoutes(s *Simulation) {
	for _, destination := range []string{"harbor-berth", "garden-berth"} {
		s.routes[routeKey{from: "market-berth", to: destination}] = routeResult{err: ErrUnreachable}
	}
}

func TestDemoStartsWithParkedPodsAndDispatchesBoth(t *testing.T) {
	t.Parallel()
	s := newTraffic(t)
	initial := s.Snapshot()
	if err := s.StartDemo(); err != nil {
		t.Fatal(err)
	}
	state := s.Snapshot()
	if len(state.Vehicles) != 4 {
		t.Fatalf("fleet size %d", len(state.Vehicles))
	}
	for _, id := range []string{"03", "04"} {
		v := s.findVehicle(id)
		if v.Pod.StationID != "parking" || v.Pod.Activity != Idle || v.Pod.Occupied {
			t.Fatalf("pod %s did not start parked", id)
		}
	}
	carried := make(map[string]bool)
	maxPending := 0
	for range 900 * TicksPerSecond {
		s.Step()
		state = s.Snapshot()
		maxPending = max(maxPending, len(state.Pending))
		for _, v := range state.Vehicles {
			if v.Pod.Occupied {
				carried[v.Pod.ID] = true
			}
		}
		if !state.Demo {
			break
		}
	}
	if state.DemoError != "" || state.Completed != demoJourneys || state.Submitted != demoJourneys || len(state.Pending) != 0 {
		t.Fatalf("incomplete demo: %+v", state)
	}
	if !carried["03"] || !carried["04"] || maxPending < 2 {
		t.Fatalf("parked pods or queue not exercised: carried%v pending%d", carried, maxPending)
	}
	s.Reset()
	if !reflect.DeepEqual(initial, s.Snapshot()) {
		t.Fatal("reset retained demo pods")
	}
}
