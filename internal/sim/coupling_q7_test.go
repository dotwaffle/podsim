package sim

import (
	"errors"
	"maps"
	"strings"
	"testing"
)

// q7Cause makes one pod out of service for the Q7 tests (section 5.8 of
// the incident emergency contract). The recovery and hold causes are
// states that the fault operations make. The purpose and record causes
// set one field alone, so that each condition of changed has its own
// test; they break W5 and F1, and the tests that use them do not check
// the contract.
type q7Cause struct {
	name  string
	apply func(v *vehicle)
}

var (
	q7Recovery = q7Cause{"fault recovery", func(v *vehicle) {
		v.withdrawn, v.op = faultHold, operationalDestination{purpose: opEmptyRecovery, owner: faultHold}
	}}
	q7Hold    = q7Cause{"fault hold", func(v *vehicle) { v.withdrawn = faultHold }}
	q7Purpose = q7Cause{"purpose", func(v *vehicle) {
		v.op = operationalDestination{purpose: opEmptyRecovery, owner: faultHold}
	}}
	q7Record = q7Cause{"fault record", func(v *vehicle) { v.faulted = true }}
)

// A pod in its fault recovery and a pod with the fault hold are not
// discovered for coupling, as the front or as the rear of a platoon pair
// on a coupling corridor.
func TestCouplingQ7Recruitment(t *testing.T) {
	t.Parallel()
	for _, cause := range []q7Cause{{"control", func(*vehicle) {}}, q7Recovery, q7Hold} {
		for _, id := range []string{"front", "rear"} {
			input := couplingApproachFixture(t, false)
			s := input.Simulation
			s.couplingNetwork, s.couplingEnabled = input.Network, true
			cause.apply(s.findVehicle(id))
			s.discoverCouplingApproaches()
			if err := s.CheckContract(); err != nil {
				t.Fatalf("%s %s: %v", cause.name, id, err)
			}
			want := 0
			if cause.name == "control" {
				want = 1
			}
			if len(s.couplingApproaches) != want || len(s.couplingAttempts) != want {
				t.Fatalf("%s %s: %d approaches and %d attempts, want %d", cause.name, id, len(s.couplingApproaches), len(s.couplingAttempts), want)
			}
		}
	}
}

// An approach member that gains a hold, a purpose, or a fault record
// aborts the approach through changed.
func TestCouplingQ7ApproachChanged(t *testing.T) {
	t.Parallel()
	for _, cause := range []q7Cause{q7Hold, q7Purpose, q7Record} {
		for _, id := range []string{"front", "rear"} {
			input := couplingApproachFixture(t, false)
			s := input.Simulation
			c, state, err := prepareCouplingApproach(input)
			if err != nil {
				t.Fatal(err)
			}
			s.grant(intent{index: 0, block: 2, through: 2})
			for range 30 {
				var step couplingApproachStep
				step, err = planCouplingApproachTest(couplingApproachInput{Context: c, Previous: state, Simulation: s, Enabled: true})
				if err != nil || step.Reason != "" {
					t.Fatalf("%s %s: control step failed: %v %q", cause.name, id, err, step.Reason)
				}
				applyCouplingApproachTestStep(t, s, step)
				state = step.State
			}
			cause.apply(s.findVehicle(id))
			step, err := planCouplingApproachTest(couplingApproachInput{Context: c, Previous: state, Simulation: s, Enabled: true})
			if err != nil {
				t.Fatal(err)
			}
			if step.Reason != "approach member is out of service" || step.State.Phase != couplingApproachAborting {
				t.Fatalf("%s %s: reason %q, phase %v", cause.name, id, step.Reason, step.State.Phase)
			}
		}
	}
}

// In Step, an approach whose member gains a hold aborts at once, and the
// pair never forms a group. Without the hold, the pair forms one.
func TestCouplingQ7ApproachAbortsInStep(t *testing.T) {
	t.Parallel()
	for _, cause := range []q7Cause{{"control", func(*vehicle) {}}, q7Recovery, q7Hold} {
		for _, id := range []string{"front", "rear"} {
			s := controlledCouplingApproachRuntime(t)
			s.grant(intent{index: 0, block: 2, through: 2})
			cause.apply(s.findVehicle(id))
			s.Step()
			if err := s.CouplingError(); err != nil {
				t.Fatal(err)
			}
			if aborting := s.couplingApproaches[0].state.Phase == couplingApproachAborting; aborting == (cause.name == "control") {
				t.Fatalf("%s %s: approach phase %v", cause.name, id, s.couplingApproaches[0].state.Phase)
			}
			for range 1000 {
				if len(s.couplingApproaches) == 0 || len(s.couplingGroups) != 0 {
					break
				}
				s.Step()
				if err := s.CouplingError(); err != nil {
					t.Fatal(err)
				}
			}
			if formed := len(s.couplingGroups) != 0; formed != (cause.name == "control") {
				t.Fatalf("%s %s: %d groups and %d approaches", cause.name, id, len(s.couplingGroups), len(s.couplingApproaches))
			}
		}
	}
}

// prepareCouplingAdoption denies a ready pair with an out-of-service
// member before the reservation, and it changes no owner.
func TestCouplingQ7AdoptionDenied(t *testing.T) {
	t.Parallel()
	for _, cause := range []q7Cause{{"control", func(*vehicle) {}}, q7Hold, q7Purpose} {
		for _, id := range []string{"front", "rear"} {
			input := couplingApproachFixture(t, false)
			c, state := runCouplingApproachTest(t, input)
			s := input.Simulation
			s.couplingNetwork = input.Network
			cause.apply(s.findVehicle(id))
			before := maps.Clone(s.owners)
			group, _, err := s.prepareCouplingAdoption(couplingApproachTransition{context: c, step: couplingApproachStep{State: state, Ready: true}}, 0)
			if cause.name == "control" {
				if err != nil || group.context == nil {
					t.Fatalf("control %s: %v", id, err)
				}
				continue
			}
			if !errors.Is(err, errCouplingReservationDenied) || !strings.Contains(err.Error(), "member is out of service") || group.context != nil || !maps.Equal(before, s.owners) {
				t.Fatalf("%s %s: %v", cause.name, id, err)
			}
		}
	}
}

// CheckContract fails a state in which an approach member or a coupling
// member has a hold or a purpose (invariant E6).
func TestCouplingQ7E6(t *testing.T) {
	t.Parallel()
	approach := func(t *testing.T) *Simulation {
		t.Helper()
		return controlledCouplingApproachRuntime(t)
	}
	group := func(t *testing.T) *Simulation {
		t.Helper()
		input := couplingApproachFixture(t, false)
		c, state := runCouplingApproachTest(t, input)
		s := input.Simulation
		s.couplingNetwork = input.Network
		g, _, err := s.prepareCouplingAdoption(couplingApproachTransition{context: c, step: couplingApproachStep{State: state, Ready: true}}, 0)
		if err != nil {
			t.Fatal(err)
		}
		s.couplingGroups = append(s.couplingGroups, g)
		for _, m := range &g.context.reservation.members {
			s.findVehicle(m.Vehicle.Pod.ID).couplingID = g.context.owner.id
		}
		return s
	}
	for _, kind := range []struct {
		name  string
		build func(*testing.T) *Simulation
	}{{"approach", approach}, {"group", group}} {
		for _, cause := range []q7Cause{q7Recovery, q7Hold, q7Purpose} {
			for _, id := range []string{"front", "rear"} {
				s := kind.build(t)
				if err := s.CheckContract(); err != nil {
					t.Fatalf("%s control: %v", kind.name, err)
				}
				cause.apply(s.findVehicle(id))
				if err := s.CheckContract(); err == nil || !strings.HasPrefix(err.Error(), "E6: coupling member "+id+" ") {
					t.Fatalf("%s %s %s: %v", kind.name, cause.name, id, err)
				}
			}
		}
	}
}
