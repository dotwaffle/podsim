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

func TestBerthChoiceAtMultiLaneBranch(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(ladderNetwork(), []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RequestJourney("01", "market"); err != nil {
		t.Fatal(err)
	}
	v := s.findVehicle("01")
	assignPassengerBerthForTest(t, assignPassengerBerthInput{simulation: s, vehicle: v})
	s.owners[resource{kind: berthResource, id: "market-1"}] = "02"
	s.owners[resource{kind: nodeResource, id: "market-berth"}] = "02"
	positionBeforeTerminalInlet(t, terminalInletPosition{simulation: s, vehicle: v})
	granted := slices.Clone(v.blocks[:v.reservedThrough+1])

	s.reevaluateTerminalBerth(v)

	if v.destination.ID != "market-2" || v.Route[len(v.Route)-1].ID != "market-in-2" {
		t.Fatalf("destination = %q via %q, want market-2 via market-in-2", v.destination.ID, v.Route[len(v.Route)-1].ID)
	}
	if !reflect.DeepEqual(v.blocks[:v.reservedThrough+1], granted) {
		t.Fatal("reroute changed granted blocks")
	}
}

func TestCommittedMultiLaneBranchDoesNotReroute(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(ladderNetwork(), []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RequestJourney("01", "market"); err != nil {
		t.Fatal(err)
	}
	v := s.findVehicle("01")
	assignPassengerBerthForTest(t, assignPassengerBerthInput{simulation: s, vehicle: v})
	s.owners[resource{kind: berthResource, id: "market-1"}] = "02"
	s.owners[resource{kind: nodeResource, id: "market-berth"}] = "02"
	positionBeforeTerminalInlet(t, terminalInletPosition{simulation: s, vehicle: v, committed: true})
	route, blocks := slices.Clone(v.Route), slices.Clone(v.blocks)

	s.reevaluateTerminalBerth(v)

	if !reflect.DeepEqual(v.Route, route) || !reflect.DeepEqual(v.blocks, blocks) || v.destination.ID != "market-1" {
		t.Fatal("committed multi-lane branch state changed")
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
	assignPassengerBerthForTest(t, assignPassengerBerthInput{simulation: s, vehicle: s.findVehicle("01")})
	return s
}

type assignPassengerBerthInput struct {
	simulation *Simulation
	vehicle    *vehicle
}

func assignPassengerBerthForTest(t *testing.T, input assignPassengerBerthInput) {
	t.Helper()
	s, v := input.simulation, input.vehicle
	route, destination, err := s.stationRoute(v.origin.Node, v.destinationStation)
	if err != nil {
		t.Fatal(err)
	}
	s.setVehicleRoute(v, route)
	v.destination = destination
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

func TestTerminalLaneFollowsReservation(t *testing.T) {
	t.Parallel()
	s := berthChoiceSimulation(t)
	addMarketBerth(s)
	v := s.findVehicle("01")
	s.owners[resource{kind: berthResource, id: "market-1"}] = "02"
	s.owners[resource{kind: nodeResource, id: "market-berth"}] = "02"
	positionBeforeTerminalInlet(t, terminalInletPosition{simulation: s, vehicle: v})
	near := v.reservedThrough
	v.blockIndex, v.reservedThrough, v.distance = 0, 0, v.blocks[0].start

	s.reevaluateTerminalBerth(v)
	if v.destination.ID != "market-1" || !v.terminal.known || v.terminal.eligible >= 0 {
		t.Fatalf("far from the branch: destination %q, check %+v", v.destination.ID, v.terminal)
	}
	// A grant moves the reservation to the branch without a route change.
	v.blockIndex, v.reservedThrough, v.distance = near, near, v.blocks[near].start
	s.reevaluateTerminalBerth(v)

	if v.destination.ID != "market-2" || v.Route[len(v.Route)-1].ID != "market-in-2" {
		t.Fatalf("destination = %q via %q, want market-2 via market-in-2", v.destination.ID, v.Route[len(v.Route)-1].ID)
	}
}

func TestRouteWritesResetTerminalLane(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		// change changes an input of terminalLane, but not reservedThrough.
		change func(*Simulation, *vehicle)
	}{
		{name: "new route", change: func(s *Simulation, v *vehicle) {
			// The route stops at the station entry, as before
			// assignTerminalBerth adds the station lanes.
			s.setVehicleRoute(v, v.Route[:len(v.Route)-1])
		}},
		{name: "unknown destination station", change: func(_ *Simulation, v *vehicle) {
			v.destinationStation = "missing"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := berthChoiceSimulation(t)
			v := s.findVehicle("01")
			positionBeforeTerminalInlet(t, terminalInletPosition{simulation: s, vehicle: v})
			if _, _, ok := s.terminalLane(v); !ok {
				t.Fatal("no eligible lane before the change")
			}

			tc.change(s, v)

			eligible, through, ok := s.terminalLane(v)
			wantEligible, wantThrough := s.findTerminalLane(v)
			if ok || eligible != wantEligible || through != wantThrough {
				t.Fatalf("terminalLane = %d, %d, %t, want %d, %d, false", eligible, through, ok, wantEligible, wantThrough)
			}
		})
	}
}

func TestTerminalBranchRerouteResetsTerminalLane(t *testing.T) {
	t.Parallel()
	s := berthChoiceSimulation(t)
	addMarketBerth(s)
	v := s.findVehicle("01")
	s.owners[resource{kind: berthResource, id: "market-1"}] = "02"
	s.owners[resource{kind: nodeResource, id: "market-berth"}] = "02"
	positionBeforeTerminalInlet(t, terminalInletPosition{simulation: s, vehicle: v})

	s.reevaluateTerminalBerth(v)

	if v.destination.ID != "market-2" {
		t.Fatalf("destination = %q, want market-2", v.destination.ID)
	}
	if v.terminal != (terminalCheck{}) {
		t.Fatalf("check %+v kept after the reroute", v.terminal)
	}
}

// TestTerminalLaneCacheMatchesFullCheck runs pods that compete for the
// market berths. After each step, each cached lane check that is valid for
// its pod must be equal to a check without the cache.
func TestTerminalLaneCacheMatchesFullCheck(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(Example(), []Placement{
		{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"},
		{ID: "03", StationID: "parking", BerthID: "parking-1"}, {ID: "04", StationID: "parking", BerthID: "parking-2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	addMarketBerth(s)
	trips := [][2]string{{"harbor", "market"}, {"garden", "market"}, {"market", "harbor"}, {"market", "garden"}}
	checked := 0
	for tick := range 600 * TicksPerSecond {
		if tick%(20*TicksPerSecond) == 0 {
			trip := trips[tick/(20*TicksPerSecond)%len(trips)]
			if err := s.RequestTrip(trip[0], trip[1]); err != nil {
				t.Fatal(err)
			}
		}
		s.Step()
		for i := range s.vehicles {
			v := &s.vehicles[i]
			next := v.reservedThrough + 1
			c := v.terminal
			if !c.known || c.reservedThrough != v.reservedThrough || c.station != v.destinationStation ||
				next < 0 || next >= len(v.blocks) || len(v.Route) == 0 {
				continue
			}
			checked++
			if eligible, through := s.findTerminalLane(v); c.eligible != eligible || c.through != through {
				t.Fatalf("tick %d pod %s: cached %d, %d, want %d, %d", s.tick, v.Pod.ID, c.eligible, c.through, eligible, through)
			}
		}
	}
	if checked < 1000 || s.Snapshot().Completed < 10 {
		t.Fatalf("checked %d cached results for %d trips", checked, s.Snapshot().Completed)
	}
}
