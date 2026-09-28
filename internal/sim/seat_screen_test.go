package sim

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
)

// newScreenSimulation returns a simulation of network with the pods of
// placements, the party limit, the sharing mode, and the experiment
// records on.
func newScreenSimulation(t *testing.T, network Network, placements []Placement, limit int, mode SharedRideMode) *Simulation {
	t.Helper()
	s, err := NewFleet(network, placements)
	if err != nil {
		t.Fatal(err)
	}
	monitorContract(t, s)
	if err := s.SetSharedRidePartyLimit(limit); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSharedRideMode(mode, DefaultSharedRideMaxStops); err != nil {
		t.Fatal(err)
	}
	s.SetExperimentRecords(true)
	return s
}

// advanceUntilDeparted steps s until pod 01 no longer boards.
func advanceUntilDeparted(t *testing.T, s *Simulation) {
	t.Helper()
	for range 30 * TicksPerSecond {
		if s.findVehicle("01").Pod.Activity != Boarding {
			return
		}
		s.Step()
	}
	t.Fatal("pod 01 did not depart")
}

// TestSeatScreenCountsFullPods boards parties at harbor. The route from
// harbor to market passes garden. In the far network, a stop at garden
// takes a rider for market over maxSharedRideDetour.
func TestSeatScreenCountsFullPods(t *testing.T) {
	t.Parallel()
	far := Example()
	for index := range far.Nodes {
		if far.Nodes[index].ID == "garden-berth" {
			far.Nodes[index].Position.Y = -600
		}
	}
	five := []string{"market", "market", "market", "market", "market"}
	for _, test := range []struct {
		name         string
		network      Network
		limit        int
		mode         SharedRideMode
		destinations []string
		want         SeatScreen
	}{
		{
			name: "fifth party refused", network: Example(), limit: 4, mode: SharedRideDropOffs, destinations: five,
			want: seatScreenOf(1, 1, 1, 4, 5),
		},
		{
			name: "fifth party joins eight seats", network: Example(), limit: 8, mode: SharedRideDropOffs, destinations: five,
			want: seatScreenOf(0, 0, 0, 5, 5),
		},
		{
			name: "drop-off party refused", network: Example(), limit: 2, mode: SharedRideDropOffs, destinations: []string{"market", "market", "garden"},
			want: seatScreenOf(1, 1, 1, 2, 3),
		},
		{
			name: "drop-off party over the cap", network: far, limit: 2, mode: SharedRideDropOffs, destinations: []string{"market", "market", "garden"},
			want: seatScreenOf(0, 1, 0, 2, 2),
		},
		{
			name: "other destination", network: Example(), limit: 2, mode: SharedRideDestination, destinations: []string{"market", "market", "garden"},
			want: seatScreenOf(0, 1, 0, 2, 2),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s := newScreenSimulation(t, test.network, []Placement{
				{ID: "01", StationID: "harbor", BerthID: "harbor-1"},
				{ID: "02", StationID: "garden", BerthID: "garden-1"},
			}, test.limit, test.mode)
			for _, destination := range test.destinations {
				if err := s.RequestTrip("harbor", destination); err != nil {
					t.Fatal(err)
				}
			}
			aboard := min(len(test.destinations), test.limit)
			if riders := len(s.findVehicle("01").Riders); riders != aboard {
				t.Fatalf("pod 01 has %d riders, want %d", riders, aboard)
			}
			if got := s.SeatScreen().FullPodRefusals; got != test.want.FullPodRefusals {
				t.Fatalf("refusals before the departure: %d, want %d", got, test.want.FullPodRefusals)
			}
			advanceUntilDeparted(t, s)
			if got := s.SeatScreen(); got != test.want {
				t.Fatalf("seat screen %+v, want %+v", got, test.want)
			}
		})
	}
}

// seatScreenOf returns the seat screen of one departure with the parties
// aboard and the parties aboard plus the backlog.
func seatScreenOf(refusals, full, backlog, aboard, demand int) SeatScreen {
	screen := SeatScreen{FullPodRefusals: refusals, FullDepartures: full, DepartureBacklog: backlog}
	screen.Aboard[aboard] = 1
	screen.Demand[demand] = 1
	return screen
}

// TestSeatScreenCountsEachPartyOnce keeps a refused party with no pod over
// many dispatch passes. Each pass tries the party again.
func TestSeatScreenCountsEachPartyOnce(t *testing.T) {
	t.Parallel()
	s := newScreenSimulation(t, Example(), []Placement{{ID: "01", StationID: "harbor", BerthID: "harbor-1"}}, 2, SharedRideDropOffs)
	for range 3 {
		if err := s.RequestTrip("harbor", "market"); err != nil {
			t.Fatal(err)
		}
	}
	advance(s, boardingTicks-1)
	if len(s.waiting) != 1 || s.waiting[0].request.PodID != "" {
		t.Fatalf("waiting trips: %+v", s.waiting)
	}
	advanceUntilDeparted(t, s)
	if got, want := s.SeatScreen(), seatScreenOf(1, 1, 1, 2, 3); got != want {
		t.Fatalf("seat screen %+v, want %+v", got, want)
	}
}

func TestSeatScreenOff(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		limit   int
		records bool
	}{
		{name: "records off", limit: 4},
		{name: "sharing off", limit: 1, records: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s := newScreenSimulation(t, Example(), []Placement{
				{ID: "01", StationID: "harbor", BerthID: "harbor-1"},
				{ID: "02", StationID: "garden", BerthID: "garden-1"},
			}, test.limit, SharedRideDropOffs)
			s.SetExperimentRecords(test.records)
			for range 5 {
				if err := s.RequestTrip("harbor", "market"); err != nil {
					t.Fatal(err)
				}
			}
			advance(s, 60*TicksPerSecond)
			if got := s.SeatScreen(); got != (SeatScreen{}) {
				t.Fatalf("seat screen %+v", got)
			}
		})
	}
}

// TestSeatScreenChangesNoDecision runs the same shared demand with the
// experiment records on and off. The snapshots must be equal at each
// simulated second, as in the A/B check of compare, and the run with the
// records must count refusals and departures.
func TestSeatScreenChangesNoDecision(t *testing.T) {
	t.Parallel()
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
	screen := runRecordsOnAndOff(t, placements, 3, SharedRideJoinUnassigned, burst)
	departures := 0
	for _, count := range screen.Aboard {
		departures += count
	}
	if screen.FullPodRefusals == 0 || screen.FullDepartures == 0 || screen.DepartureBacklog == 0 || departures == 0 {
		t.Fatalf("the run did not use the seat screen: %+v", screen)
	}
}

// runRecordsOnAndOff runs a burst of trips every 30 s for 300 s in the
// example network in drop-offs mode with the join policy, with the
// experiment records on and off, and steps to 450 s. It fails the test when
// the snapshots differ at a simulated second. It returns the seat screen of
// the run with the records.
func runRecordsOnAndOff(t *testing.T, placements []Placement, limit int, join SharedRideJoin, burst [][2]string) SeatScreen {
	t.Helper()
	on := newScreenSimulation(t, Example(), placements, limit, SharedRideDropOffs)
	off := newScreenSimulation(t, Example(), placements, limit, SharedRideDropOffs)
	off.SetExperimentRecords(false)
	for _, s := range []*Simulation{on, off} {
		if err := s.SetSharedRideJoin(join); err != nil {
			t.Fatal(err)
		}
	}
	for tick := range 450 * TicksPerSecond {
		if tick%(30*TicksPerSecond) == 0 && tick < 300*TicksPerSecond {
			for _, trip := range burst {
				for _, s := range []*Simulation{on, off} {
					if err := s.RequestTrip(trip[0], trip[1]); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
		on.Step()
		off.Step()
		if (tick+1)%TicksPerSecond != 0 {
			continue
		}
		onState, err := json.Marshal(on.Snapshot())
		if err != nil {
			t.Fatal(err)
		}
		offState, err := json.Marshal(off.Snapshot())
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(onState, offState) {
			t.Fatalf("tick %d: the snapshots differ", tick+1)
		}
	}
	return on.SeatScreen()
}

// TestJoinCensusChangesNoDecision runs demand with parties that have a pod
// on its way while a pod boards at their origin, with the experiment
// records on and off. The run with the records must count existing-stop
// parties and added-stop parties.
func TestJoinCensusChangesNoDecision(t *testing.T) {
	t.Parallel()
	placements := []Placement{
		{ID: "01", StationID: "parking", BerthID: "parking-1"},
		{ID: "02", StationID: "parking", BerthID: "parking-2"},
		{ID: "03", StationID: "garden", BerthID: "garden-1"},
	}
	burst := [][2]string{{"harbor", "market"}, {"harbor", "garden"}, {"harbor", "market"}, {"garden", "market"}}
	screen := runRecordsOnAndOff(t, placements, 4, SharedRideJoinUnassigned, burst)
	if screen.JoinEligibleExistingStop == 0 || screen.JoinEligibleAssigned <= screen.JoinEligibleExistingStop {
		t.Fatalf("the run did not use the join census: %+v", screen)
	}
}

// joinCensusOf returns the join census of a seat screen.
func joinCensusOf(screen SeatScreen) [2]int {
	return [2]int{screen.JoinEligibleAssigned, screen.JoinEligibleExistingStop}
}

// checkJoinCensus fails the test when the join census of s is not want, or
// when JoinEligibleExistingStop is more than JoinEligibleAssigned.
func checkJoinCensus(t *testing.T, s *Simulation, want [2]int) {
	t.Helper()
	got := joinCensusOf(s.SeatScreen())
	if got[1] > got[0] {
		t.Fatalf("join census %v: more existing-stop parties than eligible parties", got)
	}
	if got != want {
		t.Fatalf("join census %v, want %v", got, want)
	}
}

// startCensus sends pods 01 and 02 from Parking to harbor, for a party to
// market and a second party to the station to, in this order. It steps s
// until pod 01 boards the first party. Then the second party has pod 02 on
// its way. When requeued is set, the second party is a rider that a
// restore queued again.
func startCensus(t *testing.T, s *Simulation, to string, requeued bool) {
	t.Helper()
	for _, destination := range []string{"market", to} {
		if err := s.RequestTrip("harbor", destination); err != nil {
			t.Fatal(err)
		}
	}
	if requeued {
		s.waiting[1].boarded = true
	}
	host, own := s.findVehicle("01"), s.findVehicle("02")
	stepUntil(t, s, "pod 01 boards", func() bool { return host.Pod.Activity == Boarding })
	if len(s.waiting) != 1 || s.waiting[0].request.PodID != "02" || !releasable(own) {
		t.Fatalf("the second party does not have pod 02 on its way: %+v", s.waiting)
	}
}

// TestJoinCensusCountsEachPartyOnce keeps a party with pod 02 on its way
// at harbor while pod 01 boards there. Each dispatch pass of the dwell
// finds the party again. In the far network, a stop at garden takes the
// party for market over maxSharedRideDetour.
func TestJoinCensusCountsEachPartyOnce(t *testing.T) {
	t.Parallel()
	far := Example()
	for index := range far.Nodes {
		if far.Nodes[index].ID == "garden-berth" {
			far.Nodes[index].Position.Y = -600
		}
	}
	for _, test := range []struct {
		name     string
		network  Network
		mode     SharedRideMode
		to       string
		requeued bool
		want     [2]int
	}{
		{name: "existing stop", network: Example(), mode: SharedRideDropOffs, to: "market", want: [2]int{1, 1}},
		{name: "added stop", network: Example(), mode: SharedRideDropOffs, to: "garden", want: [2]int{1, 0}},
		{name: "added stop over the cap", network: far, mode: SharedRideDropOffs, to: "garden"},
		{name: "same destination", network: Example(), mode: SharedRideDestination, to: "market", want: [2]int{1, 1}},
		{name: "other destination", network: Example(), mode: SharedRideDestination, to: "garden"},
		{name: "requeued rider", network: Example(), mode: SharedRideDropOffs, to: "market", requeued: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s := newScreenSimulation(t, test.network, []Placement{
				{ID: "01", StationID: "parking", BerthID: "parking-1"},
				{ID: "02", StationID: "parking", BerthID: "parking-2"},
			}, 4, test.mode)
			startCensus(t, s, test.to, test.requeued)
			checkJoinCensus(t, s, test.want)
			advanceUntilDeparted(t, s)
			if len(s.waiting) != 1 {
				t.Fatalf("the second party left the queue during the dwell: %+v", s.waiting)
			}
			checkJoinCensus(t, s, test.want)
		})
	}
}

// TestJoinCensusRestart counts a party for garden as an added-stop party.
// Then a party for garden joins pod 01, so garden becomes a stop, and the
// first party is an existing-stop party. A restart of the records between
// the two clears the counters and the flags of the party, so the census
// counts the party again in both members.
func TestJoinCensusRestart(t *testing.T) {
	t.Parallel()
	for _, restart := range []bool{false, true} {
		t.Run(fmt.Sprintf("restart %v", restart), func(t *testing.T) {
			t.Parallel()
			s := newScreenSimulation(t, Example(), []Placement{
				{ID: "01", StationID: "parking", BerthID: "parking-1"},
				{ID: "02", StationID: "parking", BerthID: "parking-2"},
			}, 4, SharedRideDropOffs)
			startCensus(t, s, "garden", false)
			checkJoinCensus(t, s, [2]int{1, 0})
			if restart {
				s.SetExperimentRecords(false)
				if trip := s.waiting[0]; trip.joinEligibleAssigned || trip.joinEligibleExistingStop {
					t.Fatalf("records off kept the census flags: %+v", trip)
				}
				s.SetExperimentRecords(true)
				checkJoinCensus(t, s, [2]int{})
			}
			if err := s.RequestTrip("harbor", "garden"); err != nil {
				t.Fatal(err)
			}
			host := s.findVehicle("01")
			if len(host.Riders) != 2 || !slices.Equal(host.Stops, []string{"garden", "market"}) {
				t.Fatalf("the party for garden did not join pod 01: %+v", host.Vehicle)
			}
			s.Step()
			checkJoinCensus(t, s, [2]int{1, 1})
		})
	}
}

// harborTwoBerths returns the example network with a second harbor berth.
func harborTwoBerths() Network {
	network := Example()
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

// TestJoinCensusCountsPartyThatBoardsDuringDwell restores pod 01 as it
// boards a party for market at harbor-1, and pod 02 near harbor-2 on its
// way to a second party for market. Pod 02 arrives during the dwell of pod
// 01, and the second party boards it. The departure backlog of pod 01 does
// not count the party, but the join census counts it.
func TestJoinCensusCountsPartyThatBoardsDuringDwell(t *testing.T) {
	t.Parallel()
	f := newRestoreFleetFixture(t, harborTwoBerths(), []Placement{
		{ID: "01", StationID: "harbor", BerthID: "harbor-1"},
		{ID: "02", StationID: "parking", BerthID: "parking-1"},
	})
	if err := f.s.SetSharedRidePartyLimit(4); err != nil {
		t.Fatal(err)
	}
	host := f.boarding(t, "01", "harbor-1")
	host.PhaseTicks = boardingTicks
	own := relocating(f.traveling(t, travelInput{id: "02", from: "parking-1", to: "harbor-2", lane: "harbor-in-2", distance: 70}))
	state := f.state(host, own)
	state.RequestID++
	state.Waiting = []SavedTrip{{Request: SavedRequest{
		ID: state.RequestID, From: "harbor", To: "market", PartySize: 1, PodID: "02", RequestedTick: restoreTick - 10,
	}}}
	s, result, err := f.restore(state)
	if err != nil || !cleanRestore(result) {
		t.Fatalf("restore: %v, %+v", err, result)
	}
	monitorContract(t, s)
	s.SetExperimentRecords(true)
	s.Step()
	checkJoinCensus(t, s, [2]int{1, 1})
	advanceUntilDeparted(t, s)
	if len(s.waiting) != 0 || s.findVehicle("02").Pod.Activity == Traveling {
		t.Fatalf("the second party did not board pod 02 during the dwell: %+v", s.Snapshot())
	}
	if screen := s.SeatScreen(); screen.DepartureBacklog != 0 {
		t.Fatalf("departure backlog %d, want 0", screen.DepartureBacklog)
	}
	checkJoinCensus(t, s, [2]int{1, 1})
}
