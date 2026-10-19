package sim

import (
	"maps"
	"slices"
	"testing"
)

// TestValidateNetworkMatchesPrepare checks that ValidateNetwork gives the
// error of PrepareNetwork, on valid networks and on networks that each
// stage of the preparation refuses.
func TestValidateNetworkMatchesPrepare(t *testing.T) {
	t.Parallel()
	networks := scenarioNetworks(t)
	compact, _ := loadCompactStation(t)
	networks["compact station"] = compact
	networks["bank example"] = BankExample()
	networks["depot"] = testDepotNetwork()
	// The bays of the compact station overlap, so the certification
	// refuses the network.
	overlap := compact.clone()
	findNode(&overlap, "berth-3").Position.X--
	networks["overlapping bays"] = overlap
	// A slot of the depot moves off the line of its lane.
	bent := testDepotNetwork()
	findNode(&bent, depotSlotID(1, 2)).Position.Y += 3
	networks["bent depot lane"] = bent
	// A slot moves 2 m from the slot behind it, so the bank audit refuses
	// the chain lanes.
	short := testDepotNetwork()
	findNode(&short, depotSlotID(1, 2)).Position.X = findNode(&short, depotSlotID(1, 3)).Position.X - 2
	networks["close slots"] = short
	missing := networks["small"].clone()
	missing.Lanes[0].To = "nowhere"
	networks["unknown node"] = missing
	var refused int
	for _, name := range slices.Sorted(maps.Keys(networks)) {
		_, want := PrepareNetwork(networks[name])
		got := ValidateNetwork(networks[name])
		if want != nil {
			refused++
		}
		if (got == nil) != (want == nil) || got != nil && got.Error() != want.Error() {
			t.Errorf("%s: got %v, want %v", name, got, want)
		}
	}
	if refused < 4 {
		t.Fatalf("only %d networks are refused", refused)
	}
}
