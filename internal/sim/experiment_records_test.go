package sim

import (
	"reflect"
	"slices"
	"testing"
)

// TestRequestTimingsFollowBoardingAndCompletion checks each timing against
// the snapshots. Request 1 boards the pod at harbor at tick 0. Request 2
// joins the boarding pod at tick 60. Both parties leave the pod at the tick
// at which the completed count becomes 2, and both rode the passenger
// distance of the one pod journey.
func TestRequestTimingsFollowBoardingAndCompletion(t *testing.T) {
	t.Parallel()
	s := newSharingSimulation(t)
	s.SetExperimentRecords(true)
	if err := s.SetSharedRidePartyLimit(2); err != nil {
		t.Fatal(err)
	}
	if err := submitSharedTrip(s, "harbor", "market"); err != nil {
		t.Fatal(err)
	}
	if state := s.Snapshot(); state.Vehicles[0].Pod.Activity != Boarding {
		t.Fatalf("request 1 did not board at once: %+v", state.Vehicles[0])
	}
	advance(s, TicksPerSecond)
	if err := submitSharedTrip(s, "harbor", "market"); err != nil {
		t.Fatal(err)
	}
	if state := s.Snapshot(); state.SharedParties != 1 {
		t.Fatalf("request 2 did not join the pod: %+v", state)
	}
	wantOpen := []RequestTiming{
		{RequestID: 1, RequestedTick: 0, BoardedTick: 0, CompletedTick: -1},
		{RequestID: 2, RequestedTick: 60, BoardedTick: 60, CompletedTick: -1, SharedWith: 1},
	}
	if got := s.RequestTimings(); !reflect.DeepEqual(got, wantOpen) {
		t.Fatalf("timings before completion = %+v, want %+v", got, wantOpen)
	}
	completedTick := int64(-1)
	for range 300 * TicksPerSecond {
		s.Step()
		if state := s.Snapshot(); state.Completed == 2 {
			completedTick = state.Tick
			break
		}
	}
	state := s.Snapshot()
	if completedTick < 0 || state.PassengerDistanceMeters <= 0 {
		t.Fatalf("the shared ride did not complete: %+v", state)
	}
	ridden := state.PassengerDistanceMeters
	want := []RequestTiming{
		{RequestID: 1, RequestedTick: 0, BoardedTick: 0, CompletedTick: completedTick, RiddenMeters: ridden, DirectMeters: ridden},
		{RequestID: 2, RequestedTick: 60, BoardedTick: 60, CompletedTick: completedTick, RiddenMeters: ridden, DirectMeters: ridden, SharedWith: 1},
	}
	if got := s.RequestTimings(); !reflect.DeepEqual(got, want) {
		t.Fatalf("timings after completion = %+v, want %+v", got, want)
	}
}

// TestRequestTimingsReadOnly checks that the records and a read do not
// change the simulation, that a clone keeps the setting and its own
// timings, and that Reset clears them.
func TestRequestTimingsReadOnly(t *testing.T) {
	t.Parallel()
	read, plain := newSharingSimulation(t), newSharingSimulation(t)
	read.SetExperimentRecords(true)
	for _, s := range []*Simulation{read, plain} {
		if err := submitSharedTrip(s, "harbor", "market"); err != nil {
			t.Fatal(err)
		}
	}
	for range 200 * TicksPerSecond {
		read.RequestTimings()
		read.Step()
		plain.Step()
	}
	if !reflect.DeepEqual(read.Snapshot(), plain.Snapshot()) || !reflect.DeepEqual(read.ExportState(), plain.ExportState()) {
		t.Fatal("RequestTimings changed the simulation")
	}
	clone := read.Clone()
	if err := submitSharedTrip(clone, "garden", "harbor"); err != nil {
		t.Fatal(err)
	}
	advance(clone, 200*TicksPerSecond)
	if got, want := len(read.RequestTimings()), 1; got != want {
		t.Fatalf("source has %d timings after the clone ran, want %d", got, want)
	}
	if got := clone.RequestTimings(); len(got) != 2 || got[1].RequestID != 2 || got[1].CompletedTick < 0 {
		t.Fatalf("clone timings = %+v", got)
	}
	if got := read.RequestTimings(); got[0].CompletedTick < 0 || len(read.requestCompletions) != 1 {
		t.Fatalf("source timings after the clone ran = %+v", got)
	}
	read.Reset()
	if got := read.RequestTimings(); len(got) != 0 {
		t.Fatalf("timings after reset = %+v", got)
	}
}

// TestRequestTimingsOffByDefault checks that a simulation keeps no record
// until SetExperimentRecords turns the records on, and that turning them off
// clears them.
func TestRequestTimingsOffByDefault(t *testing.T) {
	t.Parallel()
	s := newSharingSimulation(t)
	if err := submitSharedTrip(s, "harbor", "market"); err != nil {
		t.Fatal(err)
	}
	advance(s, 200*TicksPerSecond)
	if state := s.Snapshot(); state.Completed != 1 {
		t.Fatalf("the trip did not complete: %+v", state)
	}
	if got := s.RequestTimings(); got != nil || s.requestBoardings != nil || s.requestCompletions != nil {
		t.Fatalf("default simulation recorded timings: %+v", got)
	}
	if got := s.NodePasses(); got != nil || s.nodePasses != nil {
		t.Fatalf("default simulation recorded node passes: %+v", got)
	}
	s.SetExperimentRecords(true)
	if err := submitSharedTrip(s, "garden", "harbor"); err != nil {
		t.Fatal(err)
	}
	if got := s.RequestTimings(); len(got) != 1 || got[0].RequestID != 2 {
		t.Fatalf("timings after SetExperimentRecords(true) = %+v", got)
	}
	advance(s, 10*TicksPerSecond)
	if len(s.NodePasses()) == 0 {
		t.Fatal("no node pass after SetExperimentRecords(true)")
	}
	s.SetExperimentRecords(false)
	if got := s.RequestTimings(); got != nil || s.requestBoardings != nil || s.nodePasses != nil {
		t.Fatalf("timings after SetExperimentRecords(false) = %+v", got)
	}
}

// TestNodePassesFollowRoute follows one trip from harbor to market. The pod
// passes the start node of each lane of its route, the first at its
// departure from the harbor berth. It enters the last lane and then arrives
// at the market berth, and it does not pass the berth node at the end of
// the route. Each pass has the tick at which the snapshot first shows the
// pod on the lane.
func TestNodePassesFollowRoute(t *testing.T) {
	t.Parallel()
	s := newSharingSimulation(t)
	s.SetExperimentRecords(true)
	if err := submitSharedTrip(s, "harbor", "market"); err != nil {
		t.Fatal(err)
	}
	var want []NodePass
	var route []Lane
	lane := ""
	for s.Snapshot().Completed == 0 {
		s.Step()
		state := s.Snapshot()
		// The route can get a new suffix at the destination station, so the
		// test reads the route of each snapshot.
		route = state.Vehicles[0].Route
		if id := state.Vehicles[0].Pod.LaneID; id != "" && id != lane {
			index := slices.IndexFunc(route, func(l Lane) bool { return l.ID == id })
			want = append(want, NodePass{Tick: state.Tick, Node: route[index].From})
			lane = id
		}
		if state.Tick > 300*TicksPerSecond {
			t.Fatal("the trip did not complete")
		}
	}
	got := s.NodePasses()
	if !reflect.DeepEqual(got, want) || len(got) != len(route) || len(route) < 2 {
		t.Fatalf("node passes = %+v, want %+v", got, want)
	}
	for index, pass := range got {
		if pass.Node != route[index].From || pass.Node == route[len(route)-1].To {
			t.Fatalf("pass %d = %+v, route lane %+v", index, pass, route[index])
		}
	}
}
