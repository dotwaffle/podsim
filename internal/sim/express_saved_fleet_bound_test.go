package sim

import (
	"fmt"
	"testing"
)

func TestSavedFleetContractBounds(t *testing.T) {
	for _, test := range []struct {
		name     string
		contract OrderContract
		count    int
		want     string
	}{
		{"plain empty", "", 0, "the saved state has 0 pods, want 1 to 1000"},
		{"plain minimum", "", 1, ""},
		{"plain 600", "", 600, ""},
		{"plain 601", "", 601, ""},
		{"plain maximum", "", 1000, ""},
		{"plain overflow", "", 1001, "the saved state has 1001 pods, want 1 to 1000"},
		{"Express empty", ExpressOrderContract, 0, "the saved state has 0 pods, want 1 to 600"},
		{"Express minimum", ExpressOrderContract, 1, ""},
		{"Express maximum", ExpressOrderContract, 600, ""},
		{"Express overflow", ExpressOrderContract, 601, "the saved state has 601 pods, want 1 to 600"},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := savedFleetCountContract(test.contract, test.count)
			unaccounted, err := state.checkContract(Network{})
			if test.want != "" {
				if err == nil || err.Error() != test.want {
					t.Fatalf("checkContract error = %v, want %q", err, test.want)
				}
				return
			}
			if err != nil || unaccounted != 0 {
				t.Fatalf("checkContract = (%d, %v), want (0, nil)", unaccounted, err)
			}
		})
	}
}

// savedFleetCountContract checks no geometry or berth ownership.
// Its idle pods satisfy the saved phase contract, not a physical fleet certificate.
func savedFleetCountContract(contract OrderContract, count int) SavedState {
	state := SavedState{OrderContract: contract, SharedRidePartyLimit: 1, Pods: make([]SavedPod, count)}
	class := LegacyClass
	if contract == ExpressOrderContract {
		class = ExpressClass
	}
	for index := range state.Pods {
		state.Pods[index] = SavedPod{
			ID: fmt.Sprintf("p%04d", index), Class: class, Activity: activityCode(Idle),
			StationID: "station", BerthID: "berth",
		}
	}
	return state
}

func TestExpressSavedFleetRefusalOrder(t *testing.T) {
	for _, test := range []struct {
		name   string
		count  int
		change func(*SavedState)
		want   string
	}{
		{"overflow", 601, func(*SavedState) {}, "the saved state has 601 pods, want 1 to 600"},
		{"counter at maximum", 600, func(state *SavedState) { state.Tick = -1 }, "a saved counter is negative or not finite"},
		{"counter with overflow", 601, func(state *SavedState) { state.Tick = -1 }, "the saved state has 601 pods, want 1 to 600"},
		{"route at maximum", 600, invalidSavedFleetWaitingRoute, "the Express waiting route needs a unique existing pod"},
		{"route with overflow", 601, invalidSavedFleetWaitingRoute, "the Express waiting route needs a unique existing pod"},
		{"class at maximum", 600, func(state *SavedState) { state.Pods[0].Class = "unknown" }, "pod p0000: unknown vehicle class"},
		{"class with overflow", 601, func(state *SavedState) { state.Pods[0].Class = "unknown" }, "the saved state has 601 pods, want 1 to 600"},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := savedFleetCountContract(ExpressOrderContract, test.count)
			test.change(&state)
			if _, err := state.checkContract(Network{}); err == nil || err.Error() != test.want {
				t.Fatalf("checkContract error = %v, want %q", err, test.want)
			}
			for _, logicalOnly := range []bool{false, true} {
				s, result, err := RestoreState(RestoreStateInput{
					OrderContract: ExpressOrderContract, State: state, LogicalOnly: logicalOnly,
				})
				if err == nil || err.Error() != test.want {
					t.Fatalf("RestoreState(logicalOnly=%t) error = %v, want %q", logicalOnly, err, test.want)
				}
				if s != nil || result.Tier != "" || result.PhysicalError != nil {
					t.Fatalf("RestoreState(logicalOnly=%t) entered a restore tier: %+v", logicalOnly, result)
				}
			}
		})
	}
}

func invalidSavedFleetWaitingRoute(state *SavedState) {
	state.RequestID = 1
	state.Waiting = []SavedTrip{{
		Request: SavedRequest{ID: 1, From: "station", To: "other", PartySize: 1, SharingConsent: PrivateConsent, Service: OnDemandService},
		Route:   []int{0},
	}}
}

func TestPlainSavedFleetOverflowRestore(t *testing.T) {
	_, _, err := RestoreState(RestoreStateInput{State: savedFleetCountContract("", 1001), LogicalOnly: true})
	want := "restore the saved simulation: logical tier: the saved state has 1001 pods, want 1 to 1000"
	if err == nil || err.Error() != want {
		t.Fatalf("RestoreState error = %v, want %q", err, want)
	}
}

func TestExpressSavedFleetMaximumRestore(t *testing.T) {
	specs := make([]lineStation, 300)
	for index := range specs {
		specs[index] = lineStation{id: fmt.Sprintf("s%d", index), berths: 2}
	}
	network := expressNetwork(lineNetwork(specs))
	fleet := make([]Placement, 0, 600)
	for _, station := range network.Stations {
		for _, berth := range station.Berths {
			fleet = append(fleet, Placement{
				ID: fmt.Sprintf("p%04d", len(fleet)), Class: ExpressClass, StationID: station.ID, BerthID: berth.ID,
			})
		}
	}
	s, err := NewFleetWithOrderContract(network, fleet, ExpressOrderContract)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CheckContract(); err != nil {
		t.Fatal(err)
	}
	state := s.ExportState()
	if len(state.Pods) != 600 {
		t.Fatalf("saved fleet has %d pods, want 600", len(state.Pods))
	}
	for _, test := range []struct {
		name        string
		logicalOnly bool
		tier        RestoreTier
	}{
		{"physical", false, RestorePhysical},
		{"logical", true, RestoreLogical},
	} {
		t.Run(test.name, func(t *testing.T) {
			restored, result, err := RestoreState(RestoreStateInput{
				Network: network, Fleet: fleet, OrderContract: ExpressOrderContract, State: state, LogicalOnly: test.logicalOnly,
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.Tier != test.tier || result.PhysicalError != nil || len(restored.ExportState().Pods) != 600 {
				t.Fatalf("restore result = %+v, pods = %d", result, len(restored.ExportState().Pods))
			}
			if err := restored.CheckContract(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
