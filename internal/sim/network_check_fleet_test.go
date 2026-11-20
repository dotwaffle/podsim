package sim

import (
	"reflect"
	"testing"
)

// TestNetworkCheckFleetMatchesNewFleet checks that the fleet of a
// NetworkCheck is the fleet of NewFleetWithOrderContract: the same error,
// in the same order of the checks, for each network, fleet, and contract
// of TestNetworkCheckMatchesValidation, and the same state for some ticks
// of each fleet that starts. The fleet uses the preparation of the check,
// so that a caller that validated the network with the check does not
// prepare it again.
func TestNetworkCheckFleetMatchesNewFleet(t *testing.T) {
	t.Parallel()
	base := BankExample()
	fleet := []Placement{{ID: "01", StationID: "origin", BerthID: "origin-1"}, {ID: "02", StationID: "hub", BerthID: "bank-a-1"}}
	fleets := map[string][]Placement{"valid": fleet, "none": nil, "unknown station": {{ID: "01", StationID: "nowhere"}}}
	broken := base
	broken.Nodes = nil
	networks := map[string]Network{"valid": base, "example": Example(), "reach": reachIsland(t, base), "no nodes": broken}
	started := 0
	for name, network := range networks {
		for fleetName, placements := range fleets {
			for _, contract := range []OrderContract{"", ExpressOrderContract, "unknown"} {
				for _, validated := range []bool{false, true} {
					check := NewNetworkCheck(network, LaneClearance{})
					if validated {
						_ = check.ValidateFleet(placements, contract)
					}
					got, gotErr := check.NewFleetWithOrderContract(placements, contract)
					want, wantErr := NewFleetWithOrderContract(network, placements, contract)
					if checkErrorText(gotErr) != checkErrorText(wantErr) {
						t.Fatalf("%s, fleet %s, contract %q: error = %s, want %s", name, fleetName, contract, checkErrorText(gotErr), checkErrorText(wantErr))
					}
					if gotErr != nil {
						continue
					}
					started++
					if prepared, _ := check.preparation(); &got.network.Lanes[0] != &prepared.network.Lanes[0] {
						t.Fatalf("%s, fleet %s, contract %q: the fleet does not use the preparation of the check", name, fleetName, contract)
					}
					for range 600 {
						got.Step()
						want.Step()
					}
					if !reflect.DeepEqual(got.ExportState(), want.ExportState()) {
						t.Fatalf("%s, fleet %s, contract %q: the states differ after 600 ticks", name, fleetName, contract)
					}
				}
			}
		}
	}
	if started == 0 {
		t.Fatal("no fleet starts")
	}
}
