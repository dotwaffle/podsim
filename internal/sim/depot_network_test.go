package sim

import (
	"fmt"
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
// only feature is a depot. Slow lanes 4 m apart, below the lateral
// clearance, are refused, and so is a throat above the speed gate beside
// the lead-out of the next lane. Slow lanes 11.99 m apart pass, also in
// ValidateBankGeometry, because the lateral certification clears them. A
// slow fan that bends near the lead-in of another lane passes when the
// sweep clears it, and is refused above the speed gate. A spacing of
// Clearance passes, and pods in every slot keep the safety observation
// clean.
func TestDepotAuditScope(t *testing.T) {
	t.Parallel()
	refusals := []struct {
		name    string
		network Network
		want    string
	}{
		{"slow lanes 4 m apart", depotNetwork(depotLayout{lanes: 3, slots: 4, pitch: 12, width: 4, throat: 1.4}), `nonincident paths "parking-throat-1" and "parking-out-2" are less than 12 meters apart`},
		{"a fast throat 11.99 m apart", depotNetwork(depotLayout{lanes: 3, slots: 4, pitch: 12, width: 11.99}), `nonincident paths "parking-throat-1" and "parking-out-2" are less than 12 meters apart`},
		{"a fast bent fan", bentFan(t, 4), `nonincident paths "parking-in-1" and "parking-fan-2" are less than 12 meters apart`},
	}
	for _, refusal := range refusals {
		if _, err := PrepareNetwork(refusal.network); err == nil || err.Error() != refusal.want {
			t.Errorf("%s: %v", refusal.name, err)
		}
	}
	for name, network := range map[string]Network{
		"slow lanes 11.99 m apart": depotNetwork(depotLayout{lanes: 3, slots: 4, pitch: 12, width: 11.99, throat: 1.4}),
		"a slow bent fan":          bentFan(t, depotSpeed),
	} {
		if _, err := PrepareNetwork(network); err != nil {
			t.Errorf("%s: %v", name, err)
		}
		if err := network.ValidateBankGeometry(); err != nil {
			t.Errorf("%s: bank geometry: %v", name, err)
		}
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

// bentFan returns the test depot network with the fan of lane 2 bent
// toward the lead-in of lane 1, at a speed limit.
func bentFan(t *testing.T, speed float64) Network {
	t.Helper()
	bent := testDepotNetwork()
	slot := findNode(&bent, depotSlotID(1, 4)).Position
	fan := findLane(&bent, "parking-fan-2")
	fan.Control, fan.SpeedLimit = &Point{X: slot.X + 30, Y: slot.Y - 4}, speed
	if err := bent.validate(); err != nil {
		t.Fatalf("validate the bent fan: %v", err)
	}
	return bent
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

// splitLeadOut ends the lead-out of a lane length meters after the head
// slot, at a new node, and adds a straight run lane at 5 m/s from that node
// to the throat, as the ring depot preset does.
func splitLeadOut(network *Network, lane int, length float64) {
	out := findLane(network, fmt.Sprintf("parking-out-%d", lane))
	from, to := findNode(network, out.From).Position, findNode(network, out.To).Position
	t := length / pointDistance(from, to)
	cleared, tail := fmt.Sprintf("parking-clear-%d", lane), out.To
	out.To = cleared
	network.Nodes = append(network.Nodes, Node{ID: cleared, Position: Point{X: from.X + t*(to.X-from.X), Y: from.Y + t*(to.Y-from.Y)}})
	network.Lanes = append(network.Lanes, Lane{ID: fmt.Sprintf("parking-run-%d", lane), From: cleared, To: tail, SpeedLimit: 5})
}

// TestDepotSplitLeadOut checks a lead-out that ends just past Clearance
// from the head slot, with a faster straight lane after it. The lead-out
// qualifies with no turn, because the run lane keeps the throat out of its
// window. The throat path holds the run lane, and a blocked run lane keeps
// the head in the depot. A run lane less than Clearance from the
// chain lane to the head slot fails the lane gap audit.
func TestDepotSplitLeadOut(t *testing.T) {
	t.Parallel()
	// The run lane is a 2*Clearance lane plus 1 cm.
	layout := depotLayout{lanes: 2, slots: 2, pitch: 6, width: 12, leadOut: 3*Clearance + 0.02}
	network := depotNetwork(layout)
	splitLeadOut(&network, 1, Clearance+0.01)
	prepared, err := PrepareNetwork(network)
	if err != nil {
		t.Fatal(err)
	}
	path := prepared.depots.depots[0].lanes[0]
	var throat []string
	for _, lane := range path.throat {
		throat = append(throat, lane.ID)
	}
	if path.leadOut.ID != "parking-out-1" || !slices.Equal(throat, []string{"parking-run-1", "parking-throat-1"}) || !prepared.depots.depotLane("parking-run-1") {
		t.Fatalf("lead-out %s, throat path %v", path.leadOut.ID, throat)
	}
	if prepared.retention.of("parking-out-1") != reducedRetention(stopDistance(depotSpeed), 0) || prepared.retention.of("parking-run-1") != 0 {
		t.Fatalf("retention of the lead-out %g, of the run lane %g", prepared.retention.of("parking-out-1"), prepared.retention.of("parking-run-1"))
	}
	s := headFleetOn(t, network)
	pickup := func() string {
		trip := newTrip(s, "harbor", "garden")
		if v := s.pickupPodForRequest(trip.request, "", &dispatchPass{assigned: map[string]bool{}}); v != nil {
			return v.Pod.ID
		}
		return ""
	}
	if got := pickup(); got != "lane-1-head" {
		t.Fatalf("pickup with open lanes %q", got)
	}
	s.blocked.lanes = make([]bool, len(s.network.Lanes))
	s.blocked.lanes[s.graph.lanes["parking-run-1"]] = true
	if got := pickup(); got != "lane-2-head" {
		t.Fatalf("pickup with a blocked run lane %q", got)
	}
	short := depotNetwork(layout)
	splitLeadOut(&short, 1, Clearance-0.01)
	if _, err := PrepareNetwork(short); err == nil || err.Error() != `nonincident paths "parking-run-1" and "parking-chain-1-1" are less than 12 meters apart` {
		t.Fatalf("run lane 11.99 m from the head slot: %v", err)
	}
}
