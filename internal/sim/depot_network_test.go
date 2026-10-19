package sim

import (
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// testDepotNetwork is a valid depot of 3 lanes with 4 slots at a 12 m
// pitch. Each chain lane is at least 2R long, so it needs no short lane
// rule.
func testDepotNetwork() Network {
	return depotNetwork(depotLayout{lanes: 3, slots: 4, pitch: 12, width: 12})
}

// rotateDepotLane turns the lead-in start, the slots and the lead-out end
// of a lane about the lead-in start.
func rotateDepotLane(network *Network, lane int, angle float64) {
	center := findNode(network, "parking-lead-"+string(rune('0'+lane))).Position
	ids := []string{"parking-tail-" + string(rune('0'+lane))}
	for slot := 1; slot <= 4; slot++ {
		ids = append(ids, depotSlotID(lane, slot))
	}
	for _, id := range ids {
		node := findNode(network, id)
		dx, dy := node.Position.X-center.X, node.Position.Y-center.Y
		node.Position = Point{X: center.X + dx*math.Cos(angle) - dy*math.Sin(angle), Y: center.Y + dx*math.Sin(angle) + dy*math.Cos(angle)}
	}
}

// TestDepotValidationRefusals checks each depot refusal of validate, with
// its text, and the order of pairs of refusals.
func TestDepotValidationRefusals(t *testing.T) {
	t.Parallel()
	station := func(network *Network) *Station { return &network.Stations[len(network.Stations)-1] }
	for _, tc := range []struct {
		name   string
		change func(*Network)
		want   string
	}{
		{"unknown station", func(n *Network) { n.Depots[0].Station = "nowhere" }, `depot 0 has an unknown or repeated station "nowhere"`},
		{"repeated station", func(n *Network) { n.Depots = append(n.Depots, n.Depots[0]) }, `depot 1 has an unknown or repeated station "parking"`},
		{"station before parking", func(n *Network) { n.Depots[0].Station, station(n).ParkingOnly = "nowhere", false }, `depot 0 has an unknown or repeated station "nowhere"`},
		{"passenger station", func(n *Network) { station(n).ParkingOnly = false }, `depot station "parking" must be a parking station with no banks`},
		{"banks", func(n *Network) { station(n).Banks = []StationBank{} }, `depot station "parking" must be a parking station with no banks`},
		{"parking before lanes", func(n *Network) { station(n).ParkingOnly, n.Depots[0].Lanes = false, nil }, `depot station "parking" must be a parking station with no banks`},
		{"no lanes", func(n *Network) { n.Depots[0].Lanes = nil }, `depot station "parking" needs 1 to 8 lanes`},
		{"too many lanes", func(n *Network) {
			n.Depots[0].Lanes = slices.Repeat(n.Depots[0].Lanes, 3)
		}, `depot station "parking" needs 1 to 8 lanes`},
		{"lanes before slots", func(n *Network) { n.Depots[0].Lanes = slices.Repeat([]DepotLane{{}}, 9) }, `depot station "parking" needs 1 to 8 lanes`},
		{"empty lane", func(n *Network) { n.Depots[0].Lanes[1].BerthIDs = nil }, `depot station "parking" must list each berth of the station once`},
		{"unknown berth", func(n *Network) { n.Depots[0].Lanes[1].BerthIDs[0] = "harbor-1" }, `depot station "parking" must list each berth of the station once`},
		{"repeated berth", func(n *Network) { n.Depots[0].Lanes[1].BerthIDs[0] = depotSlotID(1, 1) }, `depot station "parking" must list each berth of the station once`},
		{"missing berth", func(n *Network) { n.Depots[0].Lanes = n.Depots[0].Lanes[:2] }, `depot station "parking" must list each berth of the station once`},
		{"slots before classes", func(n *Network) {
			n.Depots[0].Lanes = n.Depots[0].Lanes[:2]
			station(n).Berths[0].VehicleClasses = compactClassSet
		}, `depot station "parking" must list each berth of the station once`},
		{"two class sets", func(n *Network) { station(n).Berths[5].VehicleClasses = compactClassSet }, `depot station "parking" slots need one class set with no large class`},
		{"large class", func(n *Network) {
			for i := range station(n).Berths {
				station(n).Berths[i].VehicleClasses = classBit(string(GroupClass))
			}
			station(n).VehicleClasses = classBit(string(GroupClass))
		}, `depot station "parking" slots need one class set with no large class`},
		{"two incoming lanes", func(n *Network) {
			n.Lanes = append(n.Lanes, Lane{ID: "extra", From: "branch", To: "parking-entry", SpeedLimit: 14})
		}, `depot station "parking" entry needs exactly one incoming lane`},
		{"classes before entry", func(n *Network) {
			station(n).Berths[5].VehicleClasses = compactClassSet
			n.Lanes = append(n.Lanes, Lane{ID: "extra", From: "branch", To: "parking-entry", SpeedLimit: 14})
		}, `depot station "parking" slots need one class set with no large class`},
		{"slot with a second lane", func(n *Network) {
			n.Lanes = append(n.Lanes, Lane{ID: "extra", From: depotSlotID(2, 2), To: "parking-exit", SpeedLimit: 14})
		}, `depot station "parking" lane 1 has no lane path`},
		{"slots out of chain order", func(n *Network) {
			ids := n.Depots[0].Lanes[1].BerthIDs
			ids[1], ids[2] = ids[2], ids[1]
		}, `depot station "parking" lane 1 has no lane path`},
		{"fan that branches", func(n *Network) {
			n.Lanes = append(n.Lanes, Lane{ID: "extra", From: "parking-lead-3", To: "parking-exit", SpeedLimit: 14})
		}, `depot station "parking" lane 2 has no lane path`},
		{"throat that misses the exit", func(n *Network) {
			findLane(n, "parking-throat-1").To = "return-west"
		}, `depot station "parking" lane 0 has no lane path`},
		{"curved chain", func(n *Network) {
			findLane(n, "parking-chain-2-2").Control = &Point{X: findNode(n, depotSlotID(2, 2)).Position.X, Y: 600}
		}, `depot station "parking" lane 1 is not straight and ordered`},
		{"curved lead-in", func(n *Network) {
			findLane(n, "parking-in-1").Control = &Point{X: 500, Y: 480}
		}, `depot station "parking" lane 0 is not straight and ordered`},
		{"folded lane", func(n *Network) { findNode(n, depotSlotID(2, 3)).Position.Y += 0.01 }, `depot station "parking" lane 1 is not straight and ordered`},
		{"unordered slots", func(n *Network) {
			a, b := findNode(n, depotSlotID(3, 2)), findNode(n, depotSlotID(3, 3))
			a.Position, b.Position = b.Position, a.Position
		}, `depot station "parking" lane 2 is not straight and ordered`},
		{"path before parallel", func(n *Network) {
			rotateDepotLane(n, 2, 0.01)
			findNode(n, depotSlotID(3, 3)).Position.Y += 0.01
		}, `depot station "parking" lane 2 is not straight and ordered`},
		{"not parallel", func(n *Network) { rotateDepotLane(n, 2, 0.01) }, `depot station "parking" lanes are not parallel`},
		{"slot separation group", func(n *Network) { station(n).Berths[2].SeparationGroup = "g" }, `depot station "parking" has a separation group`},
		{"chain separation group", func(n *Network) { findLane(n, "parking-chain-1-1").SeparationGroup = "g" }, `depot station "parking" has a separation group`},
		{"fan separation group", func(n *Network) { findLane(n, "parking-fan-3").SeparationGroup = "g" }, `depot station "parking" has a separation group`},
		{"throat separation group", func(n *Network) { findLane(n, "parking-throat-2").SeparationGroup = "g" }, `depot station "parking" has a separation group`},
		{"parallel before groups", func(n *Network) {
			rotateDepotLane(n, 2, 0.01)
			findLane(n, "parking-fan-3").SeparationGroup = "g"
		}, `depot station "parking" lanes are not parallel`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			network := testDepotNetwork()
			tc.change(&network)
			if err := network.validate(); err == nil || err.Error() != tc.want {
				t.Fatalf("got %v, want %s", err, tc.want)
			}
		})
	}
}

// TestDepotIndexPaths checks the lane paths that validation derives, the
// depot lanes, the station routes through the chains, and the roles of the
// chain lanes.
func TestDepotIndexPaths(t *testing.T) {
	t.Parallel()
	prepared, err := PrepareNetwork(testDepotNetwork())
	if err != nil {
		t.Fatal(err)
	}
	depots := prepared.depots
	if len(depots.depots) != 1 || depots.depots[0].incoming.ID != "return-to-parking" {
		t.Fatalf("depots %+v", depots.depots)
	}
	path := depots.depots[0].lanes[1]
	var ids []string
	for _, lane := range path.lanes() {
		ids = append(ids, lane.ID)
	}
	want := []string{"parking-fan-2", "parking-in-2", "parking-chain-2-3", "parking-chain-2-2", "parking-chain-2-1", "parking-out-2", "parking-throat-2"}
	if !slices.Equal(ids, want) {
		t.Fatalf("lane path %v, want %v", ids, want)
	}
	for _, id := range want {
		if !depots.depotLane(id) {
			t.Errorf("lane %s is not a depot lane", id)
		}
	}
	for _, id := range []string{"parking-through", "return-to-parking", "return"} {
		if depots.depotLane(id) {
			t.Errorf("lane %s is a depot lane", id)
		}
	}
	if slot, ok := depots.slot(depotSlotID(3, 2)); !ok || slot != (depotSlot{depot: 0, lane: 2, slot: 1}) {
		t.Fatalf("slot %+v %v", slot, ok)
	}
	for _, lane := range prepared.network.Lanes {
		if strings.HasPrefix(lane.ID, "parking-chain") && lane.StationRole != StationBerthAccessRole {
			t.Errorf("chain lane %s has role %q", lane.ID, lane.StationRole)
		}
	}
}

// TestDepotStationRoutes checks that the station route check reaches each
// slot through the chain of its lane, and only through it.
func TestDepotStationRoutes(t *testing.T) {
	t.Parallel()
	network := testDepotNetwork()
	if err := network.validate(); err != nil {
		t.Fatal(err)
	}
	network.Depots = nil
	if err := network.validate(); err == nil || err.Error() != `station "parking" needs entry, exit, and through lanes` {
		t.Fatalf("without the depot: %v", err)
	}
}

// TestDepotAuditScope checks that the bank audit runs for a network whose
// only feature is a depot: a lateral spacing below Clearance is refused,
// and so is a fan that passes near a slot of another lane. A spacing of
// Clearance passes, and pods in every slot keep the safety observation
// clean.
func TestDepotAuditScope(t *testing.T) {
	t.Parallel()
	narrow := depotNetwork(depotLayout{lanes: 3, slots: 4, pitch: 12, width: 11.99})
	if _, err := PrepareNetwork(narrow); err == nil || !strings.Contains(err.Error(), "are less than 12 meters apart") {
		t.Fatalf("11.99 m lanes: %v", err)
	}
	bent := testDepotNetwork()
	slot := findNode(&bent, depotSlotID(1, 4)).Position
	findLane(&bent, "parking-fan-2").Control = &Point{X: slot.X + 30, Y: slot.Y - 4}
	if err := bent.validate(); err != nil {
		t.Fatalf("validate the bent fan: %v", err)
	}
	if _, err := PrepareNetwork(bent); err == nil || !strings.Contains(err.Error(), `"parking-fan-2"`) {
		t.Fatalf("bent fan: %v", err)
	}
	network := testDepotNetwork()
	s, err := NewFleet(network, depotPlacements(network, 12))
	if err != nil {
		t.Fatal(err)
	}
	for range 3 * TicksPerSecond {
		s.Step()
		if _, err := s.SafetyObservation().Check(); err != nil {
			t.Fatalf("tick %d: %v", s.tick, err)
		}
	}
}

// TestDepotNetworkClone checks that a network copy owns its depots, their
// lanes and their berth IDs, and that a network with no depot keeps nil.
func TestDepotNetworkClone(t *testing.T) {
	t.Parallel()
	network := testDepotNetwork()
	clone := network.clone()
	network.Depots[0].Station = "changed"
	network.Depots[0].Lanes[0].BerthIDs[0] = "changed"
	if !reflect.DeepEqual(clone.Depots, testDepotNetwork().Depots) {
		t.Fatalf("clone depots %+v", clone.Depots)
	}
	if Example().clone().Depots != nil {
		t.Fatal("clone made depots for a network with none")
	}
}

// TestDepotExpressRefusal checks that the Express order contract refuses a
// network with a depot, after the station records.
func TestDepotExpressRefusal(t *testing.T) {
	t.Parallel()
	network := testDepotNetwork()
	if err := validateContractNetworkRecords(network); err == nil || err.Error() != "the Express order contract does not allow depots" {
		t.Fatalf("depot: %v", err)
	}
	network.Stations[0].Name = ""
	if err := validateContractNetworkRecords(network); err == nil || err.Error() != "invalid Express station record" {
		t.Fatalf("station before depot: %v", err)
	}
}
