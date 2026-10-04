package sim

import (
	"fmt"
	"maps"
	"reflect"
	"testing"
)

// These frames come from the private motion certificate, not live recruitment.
func nativeCouplingSavedFixture(t *testing.T, occupied bool, phase couplingReservationPhase, leg int) RestoreStateInput {
	t.Helper()
	input := couplingMotionFixture(t, occupied, false)
	plan, err := planCouplingReservation(input)
	if err != nil {
		t.Fatal(err)
	}
	c, err := prepareCouplingMotionContext(couplingMotionContextInput{Reservation: plan, Current: input, GroupID: "physical-pair"})
	if err != nil {
		t.Fatal(err)
	}
	state := remainingTestFind(t, c, phase, leg)
	n := input.Network
	contracts := FleetContracts{
		CouplingContract:  CompactPairV1CouplingContract,
		CouplingSites:     []CouplingSite{n.sites["assembly"], n.sites["split"]},
		CouplingCorridors: []CouplingCorridor{n.corridors["corridor"]},
	}
	fleet := []Placement{
		{ID: "front", Class: CompactClass, StationID: "origin"},
		{ID: "rear", Class: CompactClass, StationID: "front-goal"},
	}
	s, err := NewFleetWithContracts(input.Prepared.Network(), fleet, contracts)
	if err != nil {
		t.Fatal(err)
	}
	s.tick = state.Tick
	if occupied {
		s.requestID, s.boarded = 2, 2
	}
	s.owners = make(map[resource]resourceOwner)
	for _, dependency := range c.dependencies {
		if owner := c.dependencyOwner(dependency, state); !owner.isZero() {
			s.owners[dependency.Resource] = owner
		}
	}
	for _, claim := range c.reservation.PreservedClaims {
		s.owners[claim.Resource] = claim.Expected
	}
	members := c.membersAt(state)
	for i, member := range &input.Members {
		v := s.findVehicle(member.Vehicle.Pod.ID)
		v.Vehicle = member.Vehicle
		s.setVehicleRoute(v, member.Vehicle.Route)
		v.Pod = members[i].Pod
		v.origin, v.destination, v.destinationStation = member.Origin, member.Destination, member.DestinationStation
		v.distance, v.blockIndex, v.reservedThrough = state.Distances[i], state.Cells[i], c.through[i]
		v.originReleased = v.distance >= v.originTail()
		v.pending = -1
	}
	s.couplingGroups = []couplingNativeGroup{{context: c, state: state, formationTick: input.Tick}}
	if err := s.CheckContract(); err != nil {
		t.Fatal("native fixture violates the existing cabin contract", err)
	}
	if _, err := s.SafetyObservation().Check(); err != nil {
		t.Fatal("native fixture violates certified geometry", err)
	}
	return RestoreStateInput{
		Network: input.Prepared.Network(), Fleet: fleet, CouplingContract: contracts.CouplingContract,
		CouplingSites: contracts.CouplingSites, CouplingCorridors: contracts.CouplingCorridors, State: s.ExportState(),
	}
}

func TestCouplingNativePhaseRestore(t *testing.T) {
	t.Parallel()
	for _, occupied := range []bool{false, true} {
		for _, phase := range []struct {
			phase couplingReservationPhase
			leg   int
		}{{couplingClosing, 0}, {couplingLatching, -1}, {couplingConnected, 1}, {couplingUnlatching, -1}, {couplingOpening, 2}, {couplingDraining, 3}, {couplingDraining, 4}} {
			t.Run(fmt.Sprintf("occupied=%t/phase=%d/leg=%d", occupied, phase.phase, phase.leg), func(t *testing.T) {
				t.Parallel()
				input := nativeCouplingSavedFixture(t, occupied, phase.phase, phase.leg)
				original := input.State
				for range 3 {
					s, result, err := RestoreState(input)
					if err != nil || result.Tier != RestorePhysical || checkCouplingRestoreResult(result) != nil {
						t.Fatal("strict native phase restore failed", result, err)
					}
					next := s.ExportState()
					if !reflect.DeepEqual(original, next) {
						t.Fatalf("cold restore changed cabin or saved phase\noriginal=%+v\nrestored=%+v", original, next)
					}
					if !maps.Equal(s.owners, s.retainedOwners()) {
						t.Fatal("reconstructed group owners differ from retention")
					}
					if err := s.SetCouplingEnabled(false); err != nil || len(s.couplingGroups) != 1 {
						t.Fatal("off deleted a committed group", err)
					}
					clone := s.Clone()
					clone.couplingGroups[0].formationTick++
					if clone.couplingGroups[0].formationTick == s.couplingGroups[0].formationTick {
						t.Fatal("clone shares mutable group storage")
					}
					clone.Reset()
					if len(clone.couplingGroups) != 0 || clone.CouplingContract() != input.CouplingContract || len(s.couplingGroups) != 1 {
						t.Fatal("reset did not replace only the clone's group epoch")
					}
					input.State = next
				}
			})
		}
	}
}

func TestCouplingNativeRestoreRefusesPartialRecovery(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		change func(*RestoreStateInput)
	}{
		{"logical", func(i *RestoreStateInput) { i.LogicalOnly = true }},
		{"reverse", func(i *RestoreStateInput) {
			g := &i.State.CouplingGroups[0]
			g.Members[0], g.Members[1] = g.Members[1], g.Members[0]
		}},
		{"repeat", func(i *RestoreStateInput) { g := &i.State.CouplingGroups[0]; g.Members[1] = g.Members[0] }},
		{"unknown phase", func(i *RestoreStateInput) { i.State.CouplingGroups[0].Phase = "unknown" }},
		{"lost route", func(i *RestoreStateInput) { i.State.Pods[1].Route = nil }},
		{"wrong lane", func(i *RestoreStateInput) { i.State.Pods[1].LaneID = "wrong" }},
		{"wrong distance", func(i *RestoreStateInput) { i.State.Pods[1].Distance += 1 }},
		{"over cap", func(i *RestoreStateInput) { i.State.Pods[1].Route = make([]int, newRouteLimits(i.Network).pod+1) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			input := nativeCouplingSavedFixture(t, true, couplingConnected, 1)
			tc.change(&input)
			if s, _, err := RestoreState(input); err == nil || s != nil {
				t.Fatal("invalid coupling state returned a partially recovered simulation", err)
			}
		})
	}
}
