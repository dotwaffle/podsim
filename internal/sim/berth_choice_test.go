package sim

import (
	"maps"
	"reflect"
	"slices"
	"testing"
)

func TestBerthChoiceAtTerminalBranch(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name            string
		addAlternate    bool
		wantDestination string
		wantInlet       string
	}{
		{name: "free alternate", addAlternate: true, wantDestination: "market-2", wantInlet: "market-in-2"},
		{name: "occupied fallback", wantDestination: "market-1", wantInlet: "market-in"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := berthChoiceSimulation(t)
			v := s.findVehicle("01")
			if tc.addAlternate {
				addMarketBerth(s)
			}
			s.owners[resource{kind: berthResource, id: "market-1"}] = "02"
			s.owners[resource{kind: nodeResource, id: "market-berth"}] = "02"
			positionBeforeTerminalInlet(t, terminalInletPosition{simulation: s, vehicle: v})

			s.admit()

			if v.destination.ID != tc.wantDestination || v.Route[len(v.Route)-1].ID != tc.wantInlet {
				t.Fatalf("destination = %q via %q, want %q via %q", v.destination.ID, v.Route[len(v.Route)-1].ID, tc.wantDestination, tc.wantInlet)
			}
		})
	}
}

func TestCommittedTerminalBranchDoesNotReroute(t *testing.T) {
	t.Parallel()
	s := berthChoiceSimulation(t)
	addMarketBerth(s)
	v := s.findVehicle("01")
	s.owners[resource{kind: berthResource, id: "market-1"}] = "02"
	s.owners[resource{kind: nodeResource, id: "market-berth"}] = "02"
	positionBeforeTerminalInlet(t, terminalInletPosition{simulation: s, vehicle: v, committed: true})
	route, blocks := slices.Clone(v.Route), slices.Clone(v.blocks)
	owners := maps.Clone(s.owners)
	destination := v.destination
	blockIndex, reservedThrough := v.blockIndex, v.reservedThrough
	distance, pending := v.distance, v.pending

	s.reevaluateTerminalBerth(v)

	if !reflect.DeepEqual(v.Route, route) || !reflect.DeepEqual(v.blocks, blocks) || v.destination != destination ||
		v.blockIndex != blockIndex || v.reservedThrough != reservedThrough || v.distance != distance || v.pending != pending ||
		!maps.Equal(s.owners, owners) {
		t.Fatal("committed terminal branch state changed")
	}
}

func TestCompetingArrivalsCompleteOnSeparateTerminalBranches(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(Example(), []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"01", "02"} {
		if err := s.RequestJourney(id, "market"); err != nil {
			t.Fatal(err)
		}
	}
	if first, second := s.findVehicle("01"), s.findVehicle("02"); first.destination.ID != "market-1" || second.destination.ID != "market-1" {
		t.Fatal("fixture needs both arrivals assigned before another berth becomes available")
	}
	addMarketBerth(s)
	for range 240 * TicksPerSecond {
		s.Step()
		checkTraffic(t, s.Snapshot())
		if s.Snapshot().Completed == 2 {
			break
		}
	}
	first, second := s.findVehicle("01"), s.findVehicle("02")
	if first.destination.ID == second.destination.ID {
		t.Fatalf("competing arrivals retained shared berth %q", first.destination.ID)
	}
	if first.Route[len(first.Route)-1].ID == second.Route[len(second.Route)-1].ID {
		t.Fatalf("competing arrivals retained shared terminal inlet %q", first.Route[len(first.Route)-1].ID)
	}
	if state := s.Snapshot(); state.Completed != 2 || first.Pod.StationID != "market" || second.Pod.StationID != "market" || first.Pod.BerthID == second.Pod.BerthID {
		t.Fatalf("competing arrivals did not complete at separate berths: %+v", state)
	}
}

func berthChoiceSimulation(t *testing.T) *Simulation {
	t.Helper()
	s, err := NewFleet(Example(), []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RequestJourney("01", "market"); err != nil {
		t.Fatal(err)
	}
	return s
}

type terminalInletPosition struct {
	simulation *Simulation
	vehicle    *vehicle
	committed  bool
}

func positionBeforeTerminalInlet(t *testing.T, position terminalInletPosition) {
	t.Helper()
	s, v := position.simulation, position.vehicle
	inlet := v.Route[len(v.Route)-1].ID
	first := -1
	for i, b := range v.blocks {
		if b.lane.ID == inlet {
			first = i
			break
		}
	}
	if first < 2 {
		t.Fatalf("route has no approach before terminal inlet %q", inlet)
	}
	v.Pod.Activity, v.Pod.Occupied = Traveling, true
	v.phaseTicks, v.blockIndex = 0, first-2
	v.reservedThrough = first - 2
	v.distance = v.blocks[v.blockIndex].start
	if position.committed {
		v.blockIndex, v.reservedThrough = first, first
		v.distance = v.blocks[first].start
	}
	for _, b := range v.blocks[:v.reservedThrough+1] {
		for _, r := range b.resources {
			s.owners[r] = v.Pod.ID
		}
	}
}
