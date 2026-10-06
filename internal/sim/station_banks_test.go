package sim

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestBankTopologyAndClone(t *testing.T) {
	n := BankExample()
	prepared, err := PrepareNetwork(n)
	if err != nil {
		t.Fatal(err)
	}
	n.Stations[1].Banks[0].BerthIDs[0] = "changed"
	if got := prepared.Network().Stations[1].Banks[0].BerthIDs[0]; got != "bank-a-1" {
		t.Fatalf("clone leaked membership: %s", got)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*Network)
	}{
		{"empty", func(n *Network) { n.Stations[1].Banks = []StationBank{} }},
		{"duplicate membership", func(n *Network) { n.Stations[1].Banks[1].BerthIDs = []string{"bank-a-1"} }},
		{"alias", func(n *Network) { n.Stations[1].Entry = "bank-b-entry" }},
		{"shared gate", func(n *Network) { n.Stations[1].Banks[1].Entry = "bank-a-entry" }},
		{"external interior", func(n *Network) {
			n.Lanes = append(n.Lanes, Lane{ID: "shortcut", From: "split", To: "bank-a-berth", SpeedLimit: 14})
		}},
		{"cross bank", func(n *Network) {
			n.Lanes = append(n.Lanes, Lane{ID: "shortcut", From: "bank-a-entry", To: "bank-b-berth", SpeedLimit: 14, StationID: "hub", StationRole: StationBerthAccessRole})
		}},
		{"orphan", func(n *Network) {
			n.Lanes = append(n.Lanes, Lane{ID: "orphan", From: "split", To: "merge", SpeedLimit: 14, StationID: "hub", StationRole: StationBerthAccessRole})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := BankExample()
			tc.mutate(&n)
			if err := n.ValidateStationBanks(); err == nil {
				t.Fatal("accepted invalid bank")
			}
		})
	}
}

// With two faults, validation reports the one that its pass order reaches
// first: bank gates, then bank membership and paths, then external lanes.
func TestBankValidationErrorOrder(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Network)
		want   string
	}{
		{"gates_before_aliases", func(n *Network) {
			n.Stations[1].Entry = "bank-b-entry"
			n.Stations[1].Banks[1].Entry = "bank-a-entry"
		}, `station "hub" has an invalid or shared bank gate "bank-a-entry"`},
		{"banks_before_lanes", func(n *Network) {
			n.Stations[1].Banks[1].BerthIDs = []string{"bank-a-1"}
			n.Lanes = append(n.Lanes, Lane{ID: "shortcut", From: "split", To: "bank-a-berth", SpeedLimit: 14})
		}, `station "hub" has invalid bank berth "bank-a-1"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := BankExample()
			tc.mutate(&n)
			if err := n.ValidateStationBanks(); err == nil || err.Error() != tc.want {
				t.Fatalf("error = %v, want %s", err, tc.want)
			}
		})
	}
}

func TestBankRoutesUseOwningGates(t *testing.T) {
	n := BankExample()
	for _, tc := range []struct {
		from, to string
		want     []string
	}{
		{"bank-b-berth", "origin-berth", []string{"bank-b-out", "b-exit"}},
		{"origin-berth", "bank-b-berth", []string{"b-entry", "bank-b-in"}},
		{"bank-a-berth", "bank-b-berth", []string{"bank-a-out", "a-exit", "b-entry", "bank-b-in"}},
	} {
		route, err := n.Route(tc.from, tc.to)
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, lane := range route {
			ids = append(ids, lane.ID)
		}
		for _, id := range tc.want {
			if !slices.Contains(ids, id) {
				t.Fatalf("%s -> %s: %v lacks %s", tc.from, tc.to, ids, id)
			}
		}
	}
	s, err := NewFleet(n, []Placement{{ID: "pod", StationID: "origin"}})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := s.stationBankEntryMatching("origin-berth", n.Stations[1], func(b Berth) int {
		if b.ID == "bank-a-1" {
			return 10
		}
		return 0
	}, LegacyClass, nil)
	if err != nil || entry != "bank-b-entry" {
		t.Fatalf("tie selection: %s %v", entry, err)
	}
	s.owners[resource{kind: berthResource, id: "bank-a-1"}] = podResourceOwner("busy")
	route, berth, err := s.stationRoute("bank-a-entry", "hub")
	if err != nil || berth.ID != "bank-a-1" || len(route) != 2 {
		t.Fatalf("committed bank switched: %v %v %v", route, berth, err)
	}
	if _, err := s.stationPath("bank-a-entry", "bank-b-berth"); err == nil {
		t.Fatal("local path escaped bank")
	}
}

func TestBankGeometryRejectsUnconnectedNearCrossing(t *testing.T) {
	n := BankExample()
	n.Nodes = append(n.Nodes, Node{ID: "near-a", Position: Point{550, -100}}, Node{ID: "near-b", Position: Point{550, 100}})
	n.Lanes = append(n.Lanes, Lane{ID: "near", From: "near-a", To: "near-b", SpeedLimit: 14})
	if err := n.ValidateBankGeometry(); err == nil || !strings.Contains(err.Error(), "nonincident") {
		t.Fatalf("near crossing: %v", err)
	}
	if _, err := PrepareNetwork(n); err == nil {
		t.Fatal("unsafe bank network admitted")
	}
}

func TestBankRestoreRejectsMismatchBeforeBothTiers(t *testing.T) {
	n := BankExample()
	fleet := []Placement{{ID: "pod", StationID: "origin"}}
	s, err := NewFleet(n, fleet)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RequestJourney("pod", "hub"); err != nil {
		t.Fatal(err)
	}
	state := s.ExportState()
	state.Pods[0].Destination = "bank-b-1"
	for _, logical := range []bool{false, true} {
		if _, _, err := RestoreState(RestoreStateInput{Network: n, Fleet: fleet, State: state, LogicalOnly: logical}); err == nil {
			t.Fatal("mismatched bank fell through restore")
		}
	}
}

func TestBankJourneyPhysicalRestoreSafety(t *testing.T) {
	n := BankExample()
	fleet := []Placement{{ID: "pod", StationID: "origin"}, {ID: "parked", StationID: "parking"}}
	s, err := NewFleet(n, fleet)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RequestJourney("pod", "hub"); err != nil {
		t.Fatal(err)
	}
	before := n.clone()
	for tick := range 6000 {
		s.Step()
		if _, err := s.SafetyObservation().Check(); err != nil {
			t.Fatal(err)
		}
		if tick%100 == 0 {
			restored, result, err := RestoreState(RestoreStateInput{Network: n, Fleet: fleet, State: s.ExportState()})
			if err != nil || result.Tier != RestorePhysical {
				t.Fatalf("tick %d restore: %+v %v", tick, result, err)
			}
			for range 10 {
				restored.Step()
				if _, err := restored.SafetyObservation().Check(); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	if s.completed != 1 {
		t.Fatalf("completed %d", s.completed)
	}
	if !reflect.DeepEqual(n, before) {
		t.Fatal("simulation changed bank input")
	}
}

func TestBankIndependentGatesAndSharedMerge(t *testing.T) {
	for _, policy := range []RoutingPolicy{FreeFlowRouting, CongestionRouting, QueueRouting, PredictiveRouting} {
		t.Run(fmt.Sprint(policy), func(t *testing.T) {
			n := BankExample()
			fleet := []Placement{{ID: "a", StationID: "hub", BerthID: "bank-a-1"}, {ID: "b", StationID: "hub", BerthID: "bank-b-1"}}
			s, err := NewFleet(n, fleet)
			if err != nil {
				t.Fatal(err)
			}
			s.SetRoutingPolicy(policy)
			for _, id := range []string{"a", "b"} {
				if err := s.RequestJourney(id, "origin"); err != nil {
					t.Fatal(err)
				}
			}
			overlap, mergeWait := false, false
			for range 2500 {
				s.Step()
				if _, err := s.SafetyObservation().Check(); err != nil {
					t.Fatal(err)
				}
				if !s.owners[resource{kind: nodeResource, id: "bank-a-exit"}].isZero() && !s.owners[resource{kind: nodeResource, id: "bank-b-exit"}].isZero() {
					overlap = true
				}
				for _, v := range s.vehicles {
					if v.Pod.WaitReason == JunctionOccupied {
						mergeWait = true
					}
				}
			}
			if !overlap {
				t.Fatal("independent gates never overlap")
			}
			if !mergeWait {
				t.Fatal("shared merge never applies ordinary resource wait")
			}
		})
	}
}

func TestBankBufferKeepsItsGateAndBlocker(t *testing.T) {
	n := BankExample()
	for i := range n.Nodes {
		id := n.Nodes[i].ID
		if strings.HasPrefix(id, "bank-") || strings.HasSuffix(id, "-departure") || id == "merge" || id == "east" {
			n.Nodes[i].Position.X += 500
		}
	}
	s, err := NewFleet(n, []Placement{{ID: "pod", StationID: "origin"}, {ID: "occupied-a", StationID: "hub", BerthID: "bank-a-1"}, {ID: "occupied-b", StationID: "hub", BerthID: "bank-b-1"}})
	if err != nil {
		t.Fatal(err)
	}
	s.SetStationBuffers(true)
	v := s.findVehicle("pod")
	v.destinationStation = "hub"
	route, err := s.route("origin-berth", "bank-b-entry")
	if err != nil {
		t.Fatal(err)
	}
	s.setVehicleRoute(v, route)
	v.Pod.Activity = Traveling
	plan, ok := s.bufferPlan(v)
	if !ok {
		t.Fatal("long independent approach has no buffer")
	}
	v.buffered = true
	s.grantBufferedHead(intent{index: slices.IndexFunc(s.vehicles, func(v vehicle) bool { return v.Pod.ID == "pod" })}, plan)
	if v.bufferBerth != "bank-b-1" || v.Pod.BlockedBy != "occupied-b" {
		t.Fatalf("buffer used another bank: %s %s", v.bufferBerth, v.Pod.BlockedBy)
	}
	if _, err := s.stationPath("bank-b-entry", "bank-a-berth"); err == nil {
		t.Fatal("buffer suffix can change banks")
	}
}

func TestBankRetainedDepartureOriginAndArrivalEscape(t *testing.T) {
	n := BankExample()
	// One shared interior stays inside bank B, but cannot join arrival to departure.
	n.Nodes = append(n.Nodes, Node{ID: "b-mouth", Position: Point{550, 210}})
	for i, lane := range n.Lanes {
		if lane.ID == "bank-b-in" {
			n.Lanes[i].To = "b-mouth"
		}
		if lane.ID == "bank-b-out" {
			n.Lanes[i].From = "b-mouth"
		}
	}
	n.Lanes = append(n.Lanes, Lane{ID: "b-inlet", From: "b-mouth", To: "bank-b-berth", SpeedLimit: 14, StationID: "hub", StationRole: StationBerthAccessRole}, Lane{ID: "b-outlet", From: "bank-b-berth", To: "b-mouth", SpeedLimit: 14, StationID: "hub", StationRole: StationDepartureRole})
	graph := newRouteGraph(n)
	if graph.banks.err != nil {
		t.Fatal(graph.banks.err)
	}
	for _, tc := range []struct {
		name, origin string
		lanes        []string
	}{
		{"wrong truncated origin", "bank-a-1", []string{"bank-b-out", "b-exit", "b-merge", "return-east", "parking-entry", "parking-through", "return-west", "origin-entry", "origin-in"}},
		{"arrival escape", "", []string{"bank-b-in", "bank-b-out", "b-exit", "b-merge", "return-east", "parking-entry", "parking-through", "return-west", "origin-entry", "origin-through", "origin-exit", "b-approach", "b-entry", "bank-b-in", "b-inlet"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var indexes []int
			for _, id := range tc.lanes {
				indexes = append(indexes, graph.lanes[id])
			}
			station, destination := "hub", "bank-b-1"
			if tc.origin != "" {
				station, destination = "origin", "origin-1"
			}
			if err := checkSavedBankRoute(n, graph, indexes, station, destination, tc.origin); err == nil {
				t.Fatal("retained inconsistent route accepted")
			}
		})
	}
}

func TestBankBoundsAndCommittedDetour(t *testing.T) {
	s, err := NewFleet(BankExample(), []Placement{{ID: "pod", StationID: "origin"}})
	if err != nil {
		t.Fatal(err)
	}
	station, _ := s.station("hub")
	berth, _ := station.berth("bank-b-1")
	route, err := s.route("origin-berth", berth.Node)
	if err != nil {
		t.Fatal(err)
	}
	if ratio := s.plannedDetour("origin-berth", []string{"hub"}, detourStart{ridden: s.lanesMeters(route), berth: berth}); ratio != 1 {
		t.Fatalf("own-bank direct detour %g", ratio)
	}
	bounds := s.stationPickupBounds("hub")
	for _, node := range s.network.Nodes {
		for _, b := range station.Berths {
			route, err := s.route(node.ID, b.Node)
			if err != nil {
				continue
			}
			cost := 0.0
			for _, lane := range route {
				cost += s.laneLength(lane) / lane.SpeedLimit
			}
			if bounds[s.graph.nodes[node.ID]] > cost+1e-9 {
				t.Fatalf("bound at %s excludes owning bank: %g > %g", node.ID, bounds[s.graph.nodes[node.ID]], cost)
			}
		}
	}
}

// TestCheckSavedBankRouteRefusalOrder pins the error that a retained bank
// route with two faults gets. checkSavedBankRoute checks the lane indexes,
// each bank lane in route order, and then the end of the route. For each
// lane it checks the start and the end of its bank part, and then the
// arrival or departure fields. Most cases break two adjacent checks, and
// the earlier check gives the refusal. A route with an invalid lane index
// gets no error.
func TestCheckSavedBankRouteRefusalOrder(t *testing.T) {
	t.Parallel()
	n := BankExample()
	graph := newRouteGraph(n)
	if graph.banks.err != nil {
		t.Fatal(graph.banks.err)
	}
	// origin-through is in a station with no banks.
	const outside = "origin-through"
	for _, test := range []struct {
		name                           string
		lanes                          []string
		stationID, destination, origin string
		want                           string
	}{
		{"index_before_lanes", []string{outside, "bank-a-in", ""}, "hub", "", "", ""},
		{"arrival_start_before_end", []string{outside, "bank-a-in", outside}, "hub", "", "", "arrival starts inside a bank"},
		{"arrival_end_before_station", []string{"bank-a-arrival", outside}, "origin", "", "", "retained arrival escapes its bank"},
		{"station_before_destination_bank", []string{"bank-a-arrival", "bank-a-in"}, "origin", "bank-b-1", "", "arrival uses another station's bank"},
		{"destination_bank_before_berth", []string{"bank-a-in", "bank-a-in"}, "hub", "bank-b-1", "", "arrival and destination use different banks"},
		{"intermediate_berth", []string{"bank-a-in", "bank-a-in"}, "hub", "bank-a-1", "", "arrival crosses an intermediate berth"},
		{"departure_start_before_end", []string{outside, "bank-a-out", outside}, "", "", "", "departure starts after the retained origin"},
		{"departure_end_before_origin", []string{"bank-a-out", outside}, "", "", "bank-b-1", "departure leaves before its bank exit"},
		{"route_order", []string{"bank-a-out", "bank-a-out", outside}, "", "", "bank-b-1", "departure and origin use different banks"},
		{"lanes_before_end", []string{"bank-a-arrival", "bank-a-in"}, "hub", "bank-b-1", "", "arrival and destination use different banks"},
		{"unknown_destination", []string{outside}, "hub", "missing", "", "unknown bank destination berth"},
		{"outside_destination_bank", []string{outside}, "hub", "bank-a-1", "", "retained route ends outside its destination bank"},
		{"berthless", []string{outside}, "hub", "", "", "berthless route does not end at a bank entry"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			indexes := make([]int, len(test.lanes))
			for i, id := range test.lanes {
				index, ok := graph.lanes[id]
				if !ok {
					index = -1
				}
				indexes[i] = index
			}
			err := checkSavedBankRoute(n, graph, indexes, test.stationID, test.destination, test.origin)
			if test.want == "" && err != nil || test.want != "" && (err == nil || err.Error() != test.want) {
				t.Fatalf("got %v, want %q", err, test.want)
			}
		})
	}
}
