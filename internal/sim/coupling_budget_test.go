package sim

import (
	"errors"
	"maps"
	"reflect"
	"slices"
	"testing"
)

func couplingBudgetFixture(t *testing.T) *Simulation {
	t.Helper()
	input := nativeCouplingSavedFixture(t, false, couplingConnected, 1)
	s, _, err := RestoreState(input)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func assertCouplingBudgetReadOnly(t *testing.T, before, after *Simulation) {
	t.Helper()
	if !reflect.DeepEqual(before.ExportState(), after.ExportState()) || !maps.Equal(before.owners, after.owners) ||
		!reflect.DeepEqual(before.vehicles, after.vehicles) {
		t.Fatal("checkpoint work check changed routes, cabins, counters, or owners")
	}
}

func TestCouplingCheckpointRouteBounds(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		change func(*Simulation)
		deny   bool
	}{
		{name: "actual full routes"},
		{name: "full route at limit", change: func(s *Simulation) {
			v := s.findVehicle("front")
			v.Route = slices.Repeat(v.Route[:1], newRouteLimits(s.network).pod)
		}},
		{name: "full route over limit", deny: true, change: func(s *Simulation) {
			v := s.findVehicle("front")
			v.Route = slices.Repeat(v.Route[:1], newRouteLimits(s.network).pod+1)
		}},
		{name: "unknown full route lane", deny: true, change: func(s *Simulation) {
			v := s.findVehicle("front")
			v.Route = cloneLanes(v.Route)
			v.Route[0].ID = "unknown"
		}},
		{name: "waiting route over limit", deny: true, change: func(s *Simulation) {
			v := s.findVehicle("front")
			s.waiting = []waitingTrip{{route: slices.Repeat(v.Route[:1], newRouteLimits(s.network).trip+1)}}
		}},
		{name: "waiting route unknown lane", deny: true, change: func(s *Simulation) {
			s.waiting = []waitingTrip{{route: []Lane{{ID: "unknown"}}}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := couplingBudgetFixture(t)
			if tc.change != nil {
				tc.change(s)
			}
			before := s.Clone()
			err := s.checkCouplingCheckpointWork([2]string{"front", "rear"})
			if tc.deny && !errors.Is(err, errCouplingCheckpointWork) || !tc.deny && err != nil {
				t.Fatal("unexpected checkpoint work decision", err)
			}
			assertCouplingBudgetReadOnly(t, before, s)
		})
	}
}

func TestCouplingCheckpointWorkBoundary(t *testing.T) {
	t.Parallel()
	for _, extra := range []int{0, 1} {
		t.Run(map[int]string{0: "at budget", 1: "over budget"}[extra], func(t *testing.T) {
			t.Parallel()
			s := couplingBudgetFixture(t)
			state := s.ExportState()
			remaining := blockBudget(s.network) - savedStateCost(s.network, state) + extra
			route := s.findVehicle("front").Route
			for remaining > 0 {
				count := min(remaining, len(route))
				s.waiting = append(s.waiting, waitingTrip{route: slices.Clone(route[:count])})
				remaining -= count
			}
			before := s.Clone()
			err := s.checkCouplingCheckpointWork([2]string{"front", "rear"})
			if extra == 0 && err != nil || extra != 0 && !errors.Is(err, errCouplingCheckpointWork) {
				t.Fatal("checkpoint aggregate boundary was not enforced", err)
			}
			assertCouplingBudgetReadOnly(t, before, s)
		})
	}
}

// Controlled routes isolate aggregate cost. This is not a formation fixture.
func TestCouplingCheckpointBatchWork(t *testing.T) {
	t.Parallel()
	s := couplingBudgetFixture(t)
	s.couplingGroups = nil
	s.vehicles = append(s.vehicles, s.vehicles[0], s.vehicles[1])
	s.vehicles[2].Pod.ID, s.vehicles[3].Pod.ID = "other-front", "other-rear"
	s.vehicleIndexes = indexVehicles(s.vehicles)
	for i := range s.vehicles {
		v := &s.vehicles[i]
		v.couplingID = ""
		v.blockIndex = v.blocks.len() - 1
		v.distance = v.blocks.at(v.blockIndex).end
		start, _, _ := s.savedStart(v)
		if start == 0 {
			t.Fatal("fixture has no discarded ordinary route prefix")
		}
	}
	pairs := [][2]string{{"front", "rear"}, {"other-front", "other-rear"}}
	laneBlocks, budget := physicalBlockBudget(s)
	fullCost := 0
	for _, v := range s.vehicles {
		for _, lane := range v.Route {
			fullCost += laneBlocks[s.graph.lanes[lane.ID]]
		}
	}
	remaining := budget - fullCost + 1
	if remaining <= 0 {
		t.Fatal("fixture already exceeds checkpoint budget")
	}
	route := s.vehicles[0].Route
	for remaining > 0 {
		count := min(remaining, len(route), newRouteLimits(s.network).trip)
		s.waiting = append(s.waiting, waitingTrip{route: slices.Clone(route[:count])})
		remaining -= count
	}
	before := s.Clone()
	for _, pair := range pairs {
		if err := s.checkCouplingCheckpointWork(pair); err != nil {
			t.Fatal("individual pair should fit with the other pair's ordinary prefix", err)
		}
	}
	if err := s.checkCouplingCheckpointBatchWork(pairs); !errors.Is(err, errCouplingCheckpointWork) {
		t.Fatal("combined full prefixes exceeded the checkpoint budget", err)
	}
	assertCouplingBudgetReadOnly(t, before, s)
	last := &s.waiting[len(s.waiting)-1]
	last.route = last.route[:len(last.route)-1]
	before = s.Clone()
	if err := s.checkCouplingCheckpointBatchWork(pairs); err != nil {
		t.Fatal("combined pairs at the exact checkpoint budget were denied", err)
	}
	assertCouplingBudgetReadOnly(t, before, s)
}

func TestCouplingCheckpointBatchMembers(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		pairs [][2]string
	}{
		{name: "empty member", pairs: [][2]string{{"front", ""}}},
		{name: "same pair member", pairs: [][2]string{{"front", "front"}}},
		{name: "member in two pairs", pairs: [][2]string{{"front", "rear"}, {"rear", "front"}}},
		{name: "unknown member", pairs: [][2]string{{"front", "unknown"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := couplingBudgetFixture(t)
			before := s.Clone()
			if err := s.checkCouplingCheckpointBatchWork(tc.pairs); !errors.Is(err, errCouplingCheckpointWork) {
				t.Fatal("invalid batch members were accepted", err)
			}
			assertCouplingBudgetReadOnly(t, before, s)
		})
	}
}

func TestCouplingRestoreRejectsWorkBeforeBuilding(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		change func(*RestoreStateInput)
	}{
		{name: "pod route cap", change: func(input *RestoreStateInput) {
			input.State.Pods[0].Route = slices.Repeat(input.State.Pods[0].Route[:1], newRouteLimits(input.Network).pod+1)
		}},
		{name: "aggregate waiting work", change: func(input *RestoreStateInput) {
			route := slices.Clone(input.State.Pods[0].Route)
			needed := (blockBudget(input.Network)-savedStateCost(input.Network, input.State))/len(route) + 1
			if needed > MaxSavedWaitingTrips {
				t.Fatal("fixture cannot exceed the budget within the waiting bound")
			}
			for index := range needed {
				input.State.Waiting = append(input.State.Waiting, SavedTrip{
					Request: SavedRequest{ID: index + 1, From: "origin", To: "front-goal", PartySize: 1,
						SharingConsent: PrivateConsent, Service: OnDemandService},
					Route: slices.Clone(route),
				})
			}
			input.State.RequestID = needed
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			input := nativeCouplingSavedFixture(t, false, couplingConnected, 1)
			tc.change(&input)
			var created *Simulation
			restored, _, err := restoreState(input, func() (*Simulation, error) {
				var createErr error
				created, createErr = NewFleetWithContracts(input.Network, input.Fleet, input.fleetContracts())
				return created, createErr
			})
			if created == nil {
				t.Fatal("restore failed before reaching the actual physical work check", err)
			}
			for i := range created.vehicles {
				v := &created.vehicles[i]
				if v.routeVersion != 0 || v.blocks.len() != 0 || v.Pod.Activity != Idle || created.tick != 0 {
					t.Fatal("committed over-budget restore started rebuilding routes or counters")
				}
			}
			if restored != nil || !errors.Is(err, errCouplingCheckpointWork) {
				t.Fatal("committed restore did not refuse its work bounds", err)
			}
		})
	}
}
