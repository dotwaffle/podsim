package sim

import (
	"bytes"
	"encoding/json"
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
	on := newScreenSimulation(t, Example(), placements, 3, SharedRideDropOffs)
	off := newScreenSimulation(t, Example(), placements, 3, SharedRideDropOffs)
	off.SetExperimentRecords(false)
	// Each burst fills a pod at harbor for market, and then a party for
	// garden finds the pod full. The pod could add garden as a stop.
	burst := [][2]string{
		{"harbor", "market"}, {"harbor", "market"}, {"harbor", "market"}, {"harbor", "garden"},
		{"garden", "market"}, {"market", "harbor"},
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
	screen := on.SeatScreen()
	departures := 0
	for _, count := range screen.Aboard {
		departures += count
	}
	if screen.FullPodRefusals == 0 || screen.FullDepartures == 0 || screen.DepartureBacklog == 0 || departures == 0 {
		t.Fatalf("the run did not use the seat screen: %+v", screen)
	}
}
