package sim

import (
	"errors"
	"maps"
	"strings"
	"testing"
)

// q7Cause makes one pod out of service for the Q7 tests (section 5.8 of
// the incident emergency contract). The recovery and fault hold causes are
// states that the fault operations make, and the emergency hold cause is a
// state of the stage 1 operations without the emergency switch. The
// purpose, fault record, and emergency record causes set one field alone,
// so that each condition of changed has its own test; they break W5, F1,
// and E7, and the tests that use them do not check the contract.
type q7Cause struct {
	name  string
	apply func(s *Simulation, v *vehicle)
}

var (
	q7Control  = q7Cause{"control", func(*Simulation, *vehicle) {}}
	q7Recovery = q7Cause{"fault recovery", func(_ *Simulation, v *vehicle) {
		v.withdrawn, v.op = faultHold, operationalDestination{purpose: opEmptyRecovery, owner: faultHold}
	}}
	q7Hold          = q7Cause{"fault hold", func(_ *Simulation, v *vehicle) { v.withdrawn = faultHold }}
	q7EmergencyHold = q7Cause{"emergency hold", func(_ *Simulation, v *vehicle) { v.withdrawn = emergencyHold }}
	q7Purpose       = q7Cause{"purpose", func(_ *Simulation, v *vehicle) {
		v.op = operationalDestination{purpose: opEmptyRecovery, owner: faultHold}
	}}
	q7Record          = q7Cause{"fault record", func(_ *Simulation, v *vehicle) { v.faulted = true }}
	q7EmergencyRecord = q7Cause{"emergency record", func(s *Simulation, v *vehicle) {
		s.emergencies = append(s.emergencies, emergencyRecord{serial: 1, pod: s.vehicleIndex(v), order: 1})
	}}
)

// A pod in its fault recovery, a pod with the fault hold, and a pod with
// the emergency hold are not discovered for coupling, as the front or as
// the rear of a platoon pair on a coupling corridor.
func TestCouplingQ7Recruitment(t *testing.T) {
	t.Parallel()
	for _, cause := range []q7Cause{q7Control, q7Recovery, q7Hold, q7EmergencyHold} {
		for _, id := range []string{"front", "rear"} {
			input := couplingApproachFixture(t, false)
			s := input.Simulation
			s.couplingNetwork, s.couplingEnabled = input.Network, true
			cause.apply(s, s.findVehicle(id))
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

// An approach member that gains a hold, a purpose, a fault record, or an
// emergency record aborts the approach through changed. A deferred member
// with an emergency record has no hold and no purpose, so its record has
// its own reason (section 5.6 of the incident emergency contract).
func TestCouplingQ7ApproachChanged(t *testing.T) {
	t.Parallel()
	for _, cause := range []q7Cause{q7Hold, q7Purpose, q7Record, q7EmergencyRecord} {
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
			cause.apply(s, s.findVehicle(id))
			step, err := planCouplingApproachTest(couplingApproachInput{Context: c, Previous: state, Simulation: s, Enabled: true})
			if err != nil {
				t.Fatal(err)
			}
			want := "approach member is out of service"
			if cause.name == q7EmergencyRecord.name {
				want = "approach member has an emergency"
			}
			if step.Reason != want || step.State.Phase != couplingApproachAborting {
				t.Fatalf("%s %s: reason %q, phase %v", cause.name, id, step.Reason, step.State.Phase)
			}
		}
	}
}

// In Step, an approach whose member gains a hold aborts at once, and the
// pair never forms a group. Without the hold, the pair forms one.
func TestCouplingQ7ApproachAbortsInStep(t *testing.T) {
	t.Parallel()
	for _, cause := range []q7Cause{q7Control, q7Recovery, q7Hold} {
		for _, id := range []string{"front", "rear"} {
			s := controlledCouplingApproachRuntime(t)
			s.grant(intent{index: 0, block: 2, through: 2})
			cause.apply(s, s.findVehicle(id))
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
	for _, cause := range []q7Cause{q7Control, q7Hold, q7Purpose} {
		for _, id := range []string{"front", "rear"} {
			input := couplingApproachFixture(t, false)
			c, state := runCouplingApproachTest(t, input)
			s := input.Simulation
			s.couplingNetwork = input.Network
			cause.apply(s, s.findVehicle(id))
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
				cause.apply(s, s.findVehicle(id))
				if err := s.CheckContract(); err == nil || !strings.HasPrefix(err.Error(), "E6: coupling member "+id+" ") {
					t.Fatalf("%s %s %s: %v", kind.name, cause.name, id, err)
				}
			}
		}
	}
}

// A coupling member with an emergency record has no hold until its group
// ends. When the pod is in a platoon pair at the start of the next tick,
// discovery does not recruit it, and the emergency stage of that tick
// withdraws it (sections 5.6 and 5.8 of the incident emergency contract).
// The state is synthetic: the test starts the emergency while the pod has
// a coupling ID, and then clears the ID, as finishNativeCoupling does at
// the split, but the pod keeps the platoon link of the fixture. A real
// split leaves no link, so this test checks the record term of the skip,
// not a reachable run. Without an emergency, discovery recruits the pair
// in that tick. The riders of the fixture have no orders, so the test does
// not check the contract.
func TestCouplingQ7RecordAfterSplit(t *testing.T) {
	t.Parallel()
	for _, id := range []string{"control", "front", "rear"} {
		input := couplingApproachFixture(t, true)
		s := input.Simulation
		s.couplingNetwork, s.couplingEnabled, s.emergenciesOn = input.Network, true, true
		v := s.findVehicle(id)
		if v != nil {
			v.couplingID = "train"
			startEmergency(t, s, v, 0)
			if v.withdrawn != 0 {
				t.Fatalf("%s: the coupling member has the holds %#x", id, v.withdrawn)
			}
			v.couplingID = ""
		}
		s.Step()
		if err := s.CouplingError(); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if v == nil {
			if len(s.couplingApproaches) != 1 {
				t.Fatalf("control: %d approaches, want 1", len(s.couplingApproaches))
			}
			continue
		}
		if len(s.couplingApproaches) != 0 || len(s.couplingAttempts) != 0 {
			t.Fatalf("%s: discovery recruits the pod: %d approaches and %d attempts", id, len(s.couplingApproaches), len(s.couplingAttempts))
		}
		if v.withdrawn != emergencyHold {
			t.Fatalf("%s: the emergency stage after the split did not withdraw the pod: holds %#x", id, v.withdrawn)
		}
	}
}
