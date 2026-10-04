package sim

import (
	"maps"
	"reflect"
	"testing"
)

func TestCouplingPassengerReceivingClaims(t *testing.T) {
	t.Parallel()
	input := couplingMotionFixture(t, true, false)
	for _, member := range input.Members {
		for _, claim := range berthResources(member.Destination) {
			delete(input.Owners, claim)
		}
	}
	before := maps.Clone(input.Owners)
	plan, err := planCouplingReservation(input)
	if err != nil {
		t.Fatal("ordinary passenger continuations need no distant receiving claim", err)
	}
	if len(plan.PreservedClaims) != 0 || !maps.Equal(before, input.Owners) {
		t.Fatal("passenger preparation manufactured receiving owners")
	}
	claims := berthResources(input.Members[0].Destination)
	input.Owners[claims[0]] = podResourceOwner(input.Members[0].Vehicle.Pod.ID)
	if _, err = planCouplingReservation(input); err == nil {
		t.Fatal("partial individual receiving claim was accepted")
	}
}

func TestCouplingNativeReceivingClaimsRestore(t *testing.T) {
	t.Parallel()
	t.Run("passenger_without_claims", func(t *testing.T) {
		t.Parallel()
		input := nativeCouplingSavedFixture(t, true, couplingConnected, 1)
		for i := range input.State.Pods {
			input.State.Pods[i].ClaimsDestination = false
		}
		for range 3 {
			s, _, err := RestoreState(input)
			if err != nil {
				t.Fatal("passenger group without receiving claims did not restore", err)
			}
			for i := range s.vehicles {
				for _, claim := range berthResources(s.vehicles[i].destination) {
					if !s.owners[claim].isZero() {
						t.Fatal("restore manufactured a distant passenger receiving owner")
					}
				}
			}
			if !maps.Equal(s.owners, s.retainedOwners()) || !reflect.DeepEqual(s.ExportState(), input.State) {
				t.Fatal("cold restore changed receiving facts or group state")
			}
			input.State = s.ExportState()
		}
	})
	t.Run("empty_missing_claim", func(t *testing.T) {
		t.Parallel()
		input := nativeCouplingSavedFixture(t, false, couplingConnected, 1)
		input.State.Pods[0].ClaimsDestination = false
		if s, _, err := RestoreState(input); err == nil || s != nil {
			t.Fatal("empty committed member recovered without its receiving claim")
		}
	})
}

func TestCouplingNativeReceivingClaimFlags(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"committed", "unmarked", "unknown_contract", "ungrouped", "missing_berth"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			state := nativeCouplingSavedFixture(t, true, couplingConnected, 1).State
			pod := state.Pods[0]
			if !pod.ClaimsDestination {
				t.Fatal("fixture lacks the retained receiving claim")
			}
			switch name {
			case "unmarked":
				state.CouplingContract = ""
			case "unknown_contract":
				state.CouplingContract = "unknown"
			case "ungrouped":
				state.CouplingGroups = nil
			case "missing_berth":
				pod.Destination = ""
			}
			err := state.checkPod(pod)
			if name == "committed" && err != nil {
				t.Fatal("committed passenger receiving claim was rejected", err)
			}
			if name != "committed" && err == nil {
				t.Fatal("invalid passenger receiving flag was accepted")
			}
		})
	}
}
