package sim

import (
	"bytes"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"reflect"
	"slices"
	"testing"
)

// TestRequestTimingsFollowBoardingAndCompletion checks each timing against
// the snapshots. Request 1 boards the pod at harbor at tick 0. Request 2
// joins the boarding pod after 1 s. Both parties leave the pod at the tick
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
		{RequestID: 2, RequestedTick: TicksPerSecond, BoardedTick: TicksPerSecond, CompletedTick: -1, SharedWith: 1},
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
		{RequestID: 2, RequestedTick: TicksPerSecond, BoardedTick: TicksPerSecond, CompletedTick: completedTick, RiddenMeters: ridden, DirectMeters: ridden, SharedWith: 1},
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

// newRecordsSimulation returns a simulation of network with the pods of
// placements, the party limit, the drop-offs mode, and the experiment
// records on.
func newRecordsSimulation(t *testing.T, network Network, placements []Placement, limit int) *Simulation {
	t.Helper()
	s, err := NewFleet(network, placements)
	if err != nil {
		t.Fatal(err)
	}
	monitorContract(t, s)
	if err := s.SetSharedRidePartyLimit(limit); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSharedRideMode(SharedRideDropOffs, DefaultSharedRideMaxStops); err != nil {
		t.Fatal(err)
	}
	s.SetExperimentRecords(true)
	return s
}

// TestExperimentRecordsChangeNoDecision runs the same shared demand with the
// experiment records on and off. The snapshots must be equal at each
// simulated second, as in the A/B check of compare.
func TestExperimentRecordsChangeNoDecision(t *testing.T) {
	t.Parallel()
	skipLong(t)
	placements := []Placement{
		{ID: "01", StationID: "harbor", BerthID: "harbor-1"},
		{ID: "02", StationID: "garden", BerthID: "garden-1"},
		{ID: "03", StationID: "parking", BerthID: "parking-1"},
	}
	// Each burst fills a pod at harbor for market, and then a party for
	// garden finds the pod full. The pod could add garden as a stop.
	burst := [][2]string{
		{"harbor", "market"}, {"harbor", "market"}, {"harbor", "market"}, {"harbor", "garden"},
		{"garden", "market"}, {"market", "harbor"},
	}
	runRecordsOnAndOff(t, placements, 3, burst)
}

// runRecordsOnAndOff runs a burst of trips every 30 s for 300 s in the
// example network in drop-offs mode, with the experiment records on and
// off, and steps to 450 s. It fails the test when the snapshots differ at a
// simulated second, ownership differs from the independent retention scan
// at any tick, or the run with the records has no timing.
func runRecordsOnAndOff(t *testing.T, placements []Placement, limit int, burst [][2]string) {
	t.Helper()
	on := newRecordsSimulation(t, Example(), placements, limit)
	off := newRecordsSimulation(t, Example(), placements, limit)
	off.SetExperimentRecords(false)
	for tick := range 450 * TicksPerSecond {
		if tick%(30*TicksPerSecond) == 0 && tick < 300*TicksPerSecond {
			for _, trip := range burst {
				for _, s := range []*Simulation{on, off} {
					if err := submitSharedTrip(s, trip[0], trip[1]); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
		on.Step()
		off.Step()
		checkIncrementalOwners(t, on)
		checkIncrementalOwners(t, off)
		if (tick+1)%TicksPerSecond != 0 {
			continue
		}
		onState, err := jsonv2.Marshal(on.Snapshot(), json.DefaultOptionsV1())
		if err != nil {
			t.Fatal(err)
		}
		offState, err := jsonv2.Marshal(off.Snapshot(), json.DefaultOptionsV1())
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(onState, offState) {
			t.Fatalf("tick %d: the snapshots differ", tick+1)
		}
	}
	if len(on.RequestTimings()) == 0 {
		t.Fatal("the run has no request timing")
	}
}
