package sim

import (
	"reflect"
	"slices"
	"testing"
)

// TestManeuverGroupRefusals pins the order and the text of the maneuver
// group refusals. Each case breaks one rule of a valid network.
func TestManeuverGroupRefusals(t *testing.T) {
	t.Parallel()
	group := func(n *Network) *ManeuverGroup { return &n.ManeuverGroups[0] }
	for _, test := range []struct {
		name   string
		change func(*Network)
		want   string
	}{
		{name: "duplicate ID", change: func(n *Network) {
			n.ManeuverGroups = append(n.ManeuverGroups, ManeuverGroup{ID: "bays-group", StationID: "bays"})
		},
			want: `maneuver group "bays-group" has an invalid or duplicate ID`},
		{name: "node ID", change: func(n *Network) { group(n).ID = "hold" }, want: `maneuver group "hold" has an invalid or duplicate ID`},
		{name: "empty ID", change: func(n *Network) { group(n).ID = "" }, want: `maneuver group "" has an invalid or duplicate ID`},
		{name: "unknown station", change: func(n *Network) { group(n).StationID = "nowhere" }, want: `maneuver group "bays-group" has unknown station "nowhere"`},
		{name: "unknown lane", change: func(n *Network) { group(n).LaneIDs = append(group(n).LaneIDs, "nowhere") },
			want: `maneuver group "bays-group" has an unknown or repeated lane "nowhere"`},
		{name: "repeated lane", change: func(n *Network) { group(n).LaneIDs = append(group(n).LaneIDs, "aisle-1") },
			want: `maneuver group "bays-group" has an unknown or repeated lane "aisle-1"`},
		{name: "lane in two groups", change: func(n *Network) {
			n.ManeuverGroups = append(n.ManeuverGroups, ManeuverGroup{ID: "second", StationID: "bays", LaneIDs: []string{"aisle-1"}})
		}, want: `maneuver group "second" has an unknown or repeated lane "aisle-1"`},
		{name: "unknown berth", change: func(n *Network) { group(n).BerthIDs = append(group(n).BerthIDs, "nowhere") },
			want: `maneuver group "bays-group" has an unknown, repeated, or foreign berth "nowhere"`},
		{name: "repeated berth", change: func(n *Network) { group(n).BerthIDs = append(group(n).BerthIDs, "bay-1") },
			want: `maneuver group "bays-group" has an unknown, repeated, or foreign berth "bay-1"`},
		{name: "foreign berth", change: func(n *Network) { group(n).BerthIDs = append(group(n).BerthIDs, "stop-1") },
			want: `maneuver group "bays-group" has an unknown, repeated, or foreign berth "stop-1"`},
		{name: "reverse lane outside", change: func(n *Network) { group(n).ReverseLaneIDs = append(group(n).ReverseLaneIDs, "approach") },
			want: `maneuver group "bays-group" reverse lane "approach" is not a unique lane of the group`},
		{name: "repeated reverse lane", change: func(n *Network) { group(n).ReverseLaneIDs = append(group(n).ReverseLaneIDs, "bay-1-out-branch-1") },
			want: `maneuver group "bays-group" reverse lane "bay-1-out-branch-1" is not a unique lane of the group`},
		{name: "legacy lane", change: func(n *Network) { n.Lanes[networkLaneIndex(*n, "aisle-1")].VehicleClasses = 0 },
			want: `maneuver group "bays-group" lane "aisle-1" admits a class other than compact`},
		{name: "group lane", change: func(n *Network) { n.Lanes[networkLaneIndex(*n, "aisle-1")].VehicleClasses = 1 << 2 },
			want: `maneuver group "bays-group" lane "aisle-1" admits a class other than compact`},
		{name: "legacy berth", change: func(n *Network) { n.Stations[0].Berths[0].VehicleClasses = 0 },
			want: `maneuver group "bays-group" berth "bay-1" admits a class other than compact`},
		{name: "extra lane at an inner node", change: func(n *Network) {
			n.Lanes = append(n.Lanes, Lane{ID: "spur", From: "receive", To: "inner-1", SpeedLimit: 14})
		}, want: `maneuver group "bays-group" berth "bay-1" has no closed bay chain`},
		{name: "lane from an inner node", change: func(n *Network) {
			n.Lanes = append(n.Lanes, Lane{ID: "spur", From: "inner-1", To: "receive", SpeedLimit: 14})
		}, want: `maneuver group "bays-group" berth "bay-1" has no closed bay chain`},
		{name: "missing reverse lane", change: func(n *Network) { *n = withoutLane(*n, "bay-1-out-inner-1") },
			want: `maneuver group "bays-group" berth "bay-1" has no closed bay chain`},
		{name: "chain lane outside the group", change: func(n *Network) {
			group(n).LaneIDs = slices.DeleteFunc(group(n).LaneIDs, func(id string) bool { return id == "bay-1-in-berth-1" })
		}, want: `maneuver group "bays-group" berth "bay-1" has no closed bay chain`},
		{name: "chain to another berth", change: func(n *Network) {
			group(n).BerthIDs = []string{"bay-1"}
			n.Stations[0].Berths = append(n.Stations[0].Berths, Berth{ID: "bay-0", Node: "inner-1", VehicleClasses: compactClassSet})
		}, want: `maneuver group "bays-group" berth "bay-1" has no closed bay chain`},
		{name: "reverse lane not flagged", change: func(n *Network) { group(n).ReverseLaneIDs = group(n).ReverseLaneIDs[1:] },
			want: `maneuver group "bays-group" reverse lanes differ from the reverse lanes of its bay chains`},
		{name: "chain lane swapped for an aisle lane", change: func(n *Network) { group(n).ReverseLaneIDs[0] = "aisle-1" },
			want: `maneuver group "bays-group" reverse lanes differ from the reverse lanes of its bay chains`},
		{name: "extra lane at a berth node", change: func(n *Network) {
			n.Lanes = append(n.Lanes, Lane{ID: "spur", From: "berth-1", To: "receive", SpeedLimit: 2, VehicleClasses: compactClassSet})
			group(n).LaneIDs = append(group(n).LaneIDs, "spur")
		}, want: `maneuver group "bays-group" berth "bay-1" has no closed bay chain`},
		{name: "aisle lane flagged", change: func(n *Network) { group(n).ReverseLaneIDs = append(group(n).ReverseLaneIDs, "aisle-1") },
			want: `maneuver group "bays-group" reverse lanes differ from the reverse lanes of its bay chains`},
		{name: "forward chain lane flagged", change: func(n *Network) { group(n).ReverseLaneIDs = append(group(n).ReverseLaneIDs, "bay-2-in-inner-2") },
			want: `maneuver group "bays-group" reverse lanes differ from the reverse lanes of its bay chains`},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			network := tallBayNetwork(2, 7)
			test.change(&network)
			if _, err := PrepareNetwork(network); err == nil || err.Error() != test.want {
				t.Fatalf("PrepareNetwork error %v, want %q", err, test.want)
			}
		})
	}
}

// TestManeuverGroupRefusalOrder checks that the group refusals come after
// the lane station refusal and before the station route refusal.
func TestManeuverGroupRefusalOrder(t *testing.T) {
	t.Parallel()
	network := tallBayNetwork(1, 7)
	network.ManeuverGroups[0].StationID = "nowhere"
	network.Lanes[0].StationID, network.Lanes[0].StationRole = "nowhere", StationApproachRole
	if _, err := PrepareNetwork(network); err == nil || err.Error() != `lane "approach" has unknown station "nowhere"` {
		t.Fatalf("lane station error %v", err)
	}
	network = withoutLane(tallBayNetwork(1, 7), "bay-1-out-branch-1")
	network.ManeuverGroups[0].StationID = "nowhere"
	if _, err := PrepareNetwork(network); err == nil || err.Error() != `maneuver group "bays-group" has unknown station "nowhere"` {
		t.Fatalf("group error %v", err)
	}
	// Without its last reverse piece, the chain of bay 1 ends at its inner
	// node. The group is valid, and the station has no route from the
	// berth to its exit.
	network.ManeuverGroups[0].StationID = "bays"
	if _, err := PrepareNetwork(network); err == nil || err.Error() != `station "bays" needs entry, exit, and through lanes` {
		t.Fatalf("station route error %v", err)
	}
}

// TestManeuverGroupShortLane checks that a group lane needs only a positive
// length, and that the same lane outside a group keeps its minimum.
func TestManeuverGroupShortLane(t *testing.T) {
	t.Parallel()
	network := tallBayNetwork(1, 0.1)
	if _, err := PrepareNetwork(network); err != nil {
		t.Fatal(err)
	}
	network.ManeuverGroups = nil
	if _, err := PrepareNetwork(network); err == nil || err.Error() != `lane "bay-1-in-inner-1" must be at least 24 meters long` {
		t.Fatalf("error %v", err)
	}
}

// TestManeuverGroupOwnership checks that a change to the groups of the
// caller does not reach a prepared network or a clone.
func TestManeuverGroupOwnership(t *testing.T) {
	t.Parallel()
	network := tallBayNetwork(1, 7)
	p, err := PrepareNetwork(network)
	if err != nil {
		t.Fatal(err)
	}
	clone := network.clone()
	expected := p.Network()
	group := &network.ManeuverGroups[0]
	group.ID, group.LaneIDs[0], group.BerthIDs[0], group.ReverseLaneIDs[0] = "changed", "changed", "changed", "changed"
	if !reflect.DeepEqual(p.Network().ManeuverGroups, expected.ManeuverGroups) || expected.ManeuverGroups[0].LaneIDs[0] != "aisle-1" {
		t.Fatal("prepared network aliases the groups of the caller")
	}
	if !reflect.DeepEqual(clone.ManeuverGroups, tallBayNetwork(1, 7).ManeuverGroups) {
		t.Fatal("clone aliases the groups of the caller")
	}
	detached := p.Network()
	detached.ManeuverGroups[0].BerthIDs[0] = "changed"
	if p.Network().ManeuverGroups[0].BerthIDs[0] != "bay-1" {
		t.Fatal("Network returns the groups of the prepared network")
	}
	if (Network{}).clone().ManeuverGroups != nil {
		t.Fatal("clone made groups for a network with none")
	}
}

// TestManeuverGroupCells checks the cells of the group lanes: each cell
// holds the group junction, and each cell of a chain lane holds its
// berth. An entry run reaches the berth cell, and a departure run passes
// the receiving node.
func TestManeuverGroupCells(t *testing.T) {
	t.Parallel()
	network := tallBayNetwork(2, 7)
	s, err := NewFleet(network, []Placement{{ID: "01", StationID: "stop", Class: CompactClass}})
	if err != nil {
		t.Fatal(err)
	}
	junction := resource{kind: junctionResource, id: "bays-group"}
	for _, lane := range s.network.Lanes {
		cells := s.laneCells[lane.ID]
		group := slices.Contains(network.ManeuverGroups[0].LaneIDs, lane.ID)
		berth, chain := s.maneuvers.chainBerth(lane.ID)
		for cell := range cells.count() {
			resources := cells.cell(cell)
			if slices.Contains(resources, junction) != group {
				t.Errorf("lane %s cell %d junction %v, want %v", lane.ID, cell, !group, group)
			}
			if chain && !slices.Contains(resources, resource{kind: berthResource, id: berth}) {
				t.Errorf("lane %s cell %d does not hold berth %s", lane.ID, cell, berth)
			}
		}
		if want := map[string]string{"bay-1-in-inner-1": "bay-1", "bay-1-out-branch-1": "bay-1", "bay-2-out-inner-2": "bay-2"}[lane.ID]; want != "" && berth != want {
			t.Errorf("lane %s chain berth %q, want %q", lane.ID, berth, want)
		}
	}
	if _, chain := s.maneuvers.chainBerth("aisle-2"); chain {
		t.Error("an aisle lane is a chain lane")
	}
	entry, err := network.RouteForClass("entry", "berth-2", CompactClass)
	if err != nil {
		t.Fatal(err)
	}
	blocks, _ := s.routeBlocks(entry)
	// The run from the first group cell holds every cell to the berth.
	first := blocks.laneFirst(slices.IndexFunc(entry, func(lane Lane) bool { return lane.ID == "aisle-1" }))
	if through := reservationEnd(&blocks, first); through != blocks.len()-1 {
		t.Fatalf("entry run ends at block %d of %d", through, blocks.len())
	}
	departure, err := network.RouteForClass("berth-1", "loop-east", CompactClass)
	if err != nil {
		t.Fatal(err)
	}
	blocks, _ = s.routeBlocks(departure)
	leave := slices.IndexFunc(departure, func(lane Lane) bool { return lane.ID == "leave" })
	if through := reservationEnd(&blocks, 0); through < blocks.laneFirst(leave) {
		t.Fatalf("departure run ends at block %d, before the first block %d of the lane after the receiving node", through, blocks.laneFirst(leave))
	}
}

// TestManeuverGroupUnshareable checks that a linked follower cannot share
// the group junction that a pod ahead holds, that a group lane does not
// join a platoon run, and that a saved link with a group lane in its run
// is refused.
func TestManeuverGroupUnshareable(t *testing.T) {
	t.Parallel()
	network := tallBayNetwork(2, 7)
	s, err := NewFleet(network, []Placement{{ID: "01", StationID: "bays", BerthID: "bay-1", Class: CompactClass}, {ID: "02", StationID: "bays", BerthID: "bay-2", Class: CompactClass}})
	if err != nil {
		t.Fatal(err)
	}
	// As in TestResourceOwnerVirtualBorrow, the follower shares a track
	// cell of the pod ahead, but not the group junction or a berth.
	for _, test := range []struct {
		r     resource
		share bool
	}{
		{r: resource{kind: trackResource, id: "aisle-1"}, share: true},
		{r: resource{kind: junctionResource, id: "bays-group"}},
		{r: resource{kind: berthResource, id: "bay-1"}},
	} {
		clone := s.Clone()
		blocks := blockListOf([]block{{lane: Lane{ID: "aisle-1", SpeedLimit: 2}, end: 30, resources: []resource{test.r}}})
		front := vehicle{Pod: Pod{ID: "01", Activity: Traveling}, blocks: blocks, reservedThrough: 0, follower: 2}
		rear := vehicle{Pod: Pod{ID: "02", Activity: Traveling}, blocks: blocks, reservedThrough: -1,
			link: platoonLink{leader: 1, lane: 0, lanes: 1, end: 0}}
		clone.vehicles, clone.platooning = []vehicle{front, rear}, PlatooningVirtual
		clone.owners = map[resource]resourceOwner{test.r: podResourceOwner("01")}
		clone.grant(intent{index: 1, block: 0})
		if shared := clone.vehicles[1].reservedThrough == 0; shared != test.share {
			t.Errorf("follower shares %v: %v, want %v", test.r, shared, test.share)
		}
	}
	// sharedLane refuses a group lane, and accepts a loop lane.
	route, err := network.RouteForClass("loop-west", "berth-2", CompactClass)
	if err != nil {
		t.Fatal(err)
	}
	v, leader := &s.vehicles[1], &s.vehicles[0]
	s.setVehicleRoute(v, route)
	s.setVehicleRoute(leader, route)
	aisle := slices.IndexFunc(route, func(lane Lane) bool { return lane.ID == "aisle-1" })
	if s.sharedLane(v, leader, aisle, aisle) {
		t.Error("a group lane joins a platoon run")
	}
	if !s.sharedLane(v, leader, 0, 0) {
		t.Error("a loop lane does not join a platoon run")
	}
	// checkSavedLink refuses a run with a group lane after its other
	// checks. A run on the loop has the refusal of the speed limits.
	r := &physicalRestore{s: s, leaders: []int{0, 1}, state: SavedState{Pods: []SavedPod{{ID: "01"}, {ID: "02"}}}}
	for _, run := range []struct {
		lane, lanes int
		want        string
	}{
		{lane: 0, lanes: 1, want: "the routes of the platoon have more than one speed limit"},
		{lane: aisle, lanes: 1, want: "the platoon run holds a lane of a maneuver group"},
	} {
		r.state.Pods[1].Platoon = &SavedPlatoonLink{Leader: "01", Lane: run.lane, LeaderLane: run.lane, Lanes: run.lanes}
		r.state.Pods[0].RouteIndex, r.state.Pods[1].RouteIndex = run.lane, run.lane
		err := r.checkSavedLink(1)
		if err == nil || err.Error() != run.want {
			t.Errorf("run at %d: error %v, want %q", run.lane, err, run.want)
		}
	}
}

// TestManeuverGroupBankAudit checks the audit of a network with a group:
// it checks each pair, skips the own berth of a chain lane and two lanes of
// one group, and refuses an ordinary lane 11.9 meters from a group lane.
func TestManeuverGroupBankAudit(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		x    float64
		want string
	}{
		{x: 12.1},
		{x: 11.9, want: `nonincident paths "side" and "bay-1-in-berth-1" are less than 12 meters apart`},
	} {
		network := tallBayNetwork(1, 7)
		network.Nodes = append(network.Nodes, Node{ID: "side-a", Position: Point{-test.x, 13}}, Node{ID: "side-b", Position: Point{-test.x, 40}})
		network.Lanes = append(network.Lanes, Lane{ID: "side", From: "side-a", To: "side-b", SpeedLimit: 14})
		_, err := PrepareNetwork(network)
		if test.want == "" && err != nil || test.want != "" && (err == nil || err.Error() != test.want) {
			t.Errorf("side lane at %g m: error %v, want %q", test.x, err, test.want)
		}
		if err := network.ValidateBankGeometry(); test.want == "" && err != nil || test.want != "" && (err == nil || err.Error() != test.want) {
			t.Errorf("ValidateBankGeometry at %g m: error %v, want %q", test.x, err, test.want)
		}
	}
	// Without the group, the audit does not run.
	network := tallBayNetwork(1, 7)
	network.ManeuverGroups = nil
	network.Nodes = append(network.Nodes, Node{ID: "side-a", Position: Point{-11.9, 13}}, Node{ID: "side-b", Position: Point{-11.9, 40}})
	network.Lanes = append(network.Lanes, Lane{ID: "side", From: "side-a", To: "side-b", SpeedLimit: 14})
	if err := network.ValidateBankGeometry(); err != nil {
		t.Fatal(err)
	}
}

// TestManeuverGroupPhases checks the station roles and phases of a group.
// Role inference skips group lanes. A group lane in the run at the start
// of a route is DepartingBerth, and one in the run at its end is
// AccessingBerth.
func TestManeuverGroupPhases(t *testing.T) {
	t.Parallel()
	network := tallBayNetwork(2, 7)
	s, err := NewFleet(network, []Placement{{ID: "01", StationID: "stop", Class: CompactClass}})
	if err != nil {
		t.Fatal(err)
	}
	for _, lane := range s.network.Lanes {
		if s.maneuvers.groupLane(lane.ID) && lane.StationRole != "" {
			t.Errorf("group lane %s has role %s", lane.ID, lane.StationRole)
		}
		if lane.ID == "approach" && lane.StationRole != StationBerthAccessRole || lane.ID == "leave" && lane.StationRole != StationDepartureRole {
			t.Errorf("lane %s has role %q", lane.ID, lane.StationRole)
		}
	}
	for _, test := range []struct {
		from, to, lane string
		want           StationPhase
	}{
		{from: "entry", to: "berth-2", lane: "aisle-2", want: AccessingBerth},
		{from: "berth-1", to: "stop-berth", lane: "aisle-2", want: DepartingBerth},
		{from: "berth-1", to: "berth-2", lane: "bay-2-in-berth-2", want: DepartingBerth},
		{from: "entry", to: "berth-2", lane: "approach", want: AccessingBerth},
	} {
		route, err := s.network.RouteForClass(test.from, test.to, CompactClass)
		if err != nil {
			t.Fatal(err)
		}
		v := &s.vehicles[0]
		s.setVehicleRoute(v, route)
		v.blockIndex = v.blocks.laneFirst(slices.IndexFunc(route, func(lane Lane) bool { return lane.ID == test.lane }))
		if phase, station := routeStationPhase(v, s.maneuvers); phase != test.want || station != "bays" {
			t.Errorf("%s to %s on %s: phase %q at %q, want %q", test.from, test.to, test.lane, phase, station, test.want)
		}
	}
}
