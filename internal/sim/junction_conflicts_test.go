package sim

import (
	"math"
	"reflect"
	"testing"
)

func TestFleetBuildsJunctionConflictsOnce(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		network Network
	}{
		{name: "example", network: Example()},
		{name: "ladder", network: ladderNetwork()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, err := NewFleet(tc.network, []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"}})
			if err != nil {
				t.Fatal(err)
			}
			// NewFleet builds the table from its owned copy of the network.
			want := buildJunctionConflicts(s.network)
			if len(want) == 0 {
				t.Fatal("fixture has no junction conflicts")
			}
			if s.junctionConflicts == nil || !reflect.DeepEqual(s.junctionConflicts, want) {
				t.Fatalf("NewFleet table = %v, want %v", s.junctionConflicts, want)
			}
			built := reflect.ValueOf(s.junctionConflicts).Pointer()
			for _, trip := range [][2]string{{"harbor", "market"}, {"garden", "market"}} {
				if err := s.RequestTrip(trip[0], trip[1]); err != nil {
					t.Fatal(err)
				}
			}
			for range 240 * TicksPerSecond {
				s.Step()
				if s.completed == 2 {
					break
				}
			}
			if s.completed != 2 {
				t.Fatalf("trips did not complete: %+v", s.Snapshot())
			}
			if reflect.ValueOf(s.junctionConflicts).Pointer() != built {
				t.Fatal("steps replaced the junction conflict table")
			}
			if !reflect.DeepEqual(s.junctionConflicts, want) {
				t.Fatalf("steps changed the junction conflict table: %v, want %v", s.junctionConflicts, want)
			}
		})
	}
}

func TestNetworkIndexesRebuildJunctionConflicts(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		lane  string
		build func(*testing.T) *Simulation
	}{
		{
			name: "changed fleet network",
			lane: "market-in-2",
			build: func(t *testing.T) *Simulation {
				t.Helper()
				s := newTraffic(t)
				addMarketBerth(s)
				return s
			},
		},
		{
			name: "literal simulation",
			lane: "bypass-merge",
			build: func(*testing.T) *Simulation {
				s := &Simulation{networkIndexes: &networkIndexes{network: Example()}}
				s.ensureNetworkIndexes()
				return s
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := tc.build(t)
			want := buildJunctionConflicts(s.network)
			if len(want[tc.lane]) == 0 {
				t.Fatalf("fixture lane %q has no junction conflicts", tc.lane)
			}
			if !reflect.DeepEqual(s.junctionConflicts, want) {
				t.Fatalf("junction conflicts = %v, want %v", s.junctionConflicts, want)
			}
		})
	}
}

func TestAcuteJunctionLanesPreservePhysicalClearance(t *testing.T) {
	t.Parallel()
	for _, reverse := range []bool{false, true} {
		name := "outbound first"
		if reverse {
			name = "inbound first"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			n := Network{Nodes: []Node{{ID: "junction"}}}
			for _, station := range []struct {
				id                 string
				entry, berth, exit Point
			}{
				{"a", Point{X: -40, Y: -100}, Point{Y: -100}, Point{Y: -60}},
				{"b", Point{X: 190, Y: 30}, Point{X: 150, Y: 30}, Point{X: 100, Y: 30}},
				{"c", Point{X: 100}, Point{X: 150}, Point{X: 190}},
				{"d", Point{Y: 100}, Point{Y: 150}, Point{Y: 190}},
			} {
				n.Nodes = append(n.Nodes, Node{ID: station.id + "-entry", Position: station.entry}, Node{ID: station.id, Position: station.berth}, Node{ID: station.id + "-exit", Position: station.exit})
				n.Stations = append(n.Stations, Station{ID: station.id, Entry: station.id + "-entry", Exit: station.id + "-exit", Berths: []Berth{{ID: station.id, Node: station.id}}})
				n.Lanes = append(n.Lanes, Lane{ID: station.id + "-in", From: station.id + "-entry", To: station.id, SpeedLimit: 14}, Lane{ID: station.id + "-out", From: station.id, To: station.id + "-exit", SpeedLimit: 14}, Lane{ID: station.id + "-through", From: station.id + "-entry", To: station.id + "-exit", SpeedLimit: 14})
			}
			n.Lanes = append(n.Lanes,
				Lane{ID: "a-approach", From: "a-exit", To: "junction", SpeedLimit: 14},
				Lane{ID: "acute-in", From: "b-exit", To: "junction", SpeedLimit: 14},
				Lane{ID: "acute-out", From: "junction", To: "c-entry", SpeedLimit: 14},
				Lane{ID: "d-approach", From: "junction", To: "d-entry", SpeedLimit: 14})
			a, b := "01", "02"
			if reverse {
				a, b = b, a
			}
			s, err := NewFleet(n, []Placement{{ID: a, StationID: "a"}, {ID: b, StationID: "b"}})
			if err != nil {
				t.Fatal(err)
			}
			if err := s.RequestJourney(a, "c"); err != nil {
				t.Fatal(err)
			}
			if err := s.RequestJourney(b, "d"); err != nil {
				t.Fatal(err)
			}
			checkTraffic(t, s.Snapshot())
			for range 120 * TicksPerSecond {
				s.Step()
				checkTraffic(t, s.Snapshot())
				if s.completed == 2 {
					return
				}
			}
			t.Fatalf("acute junction did not drain: %+v", s.Snapshot())
		})
	}
}

func TestReservationEnd(t *testing.T) {
	t.Parallel()
	a := resource{kind: junctionResource, id: "a"}
	b := resource{kind: junctionResource, id: "b"}
	for _, tc := range []struct {
		name   string
		blocks []block
		want   int
	}{
		{"single block", []block{{resources: []resource{a}}, {}}, 0},
		{"contiguous zone", []block{{resources: []resource{a}}, {resources: []resource{a}}, {}}, 1},
		{"separate return", []block{{resources: []resource{a}}, {}, {resources: []resource{a}}}, 0},
		{"overlapping zones", []block{{resources: []resource{a}}, {resources: []resource{a, b}}, {resources: []resource{b}}, {}}, 2},
		{"downstream conflict", []block{{last: true}, {resources: []resource{a}}, {resources: []resource{a}}, {}}, 2},
		{"terminal zone", []block{{resources: []resource{a}}, {resources: []resource{a}, last: true}}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			list := blockListOf(tc.blocks)
			if got := reservationEnd(&list, 0); got != tc.want {
				t.Fatalf("reservation ends at %d, want %d", got, tc.want)
			}
		})
	}
}

func TestConflictExtentIncludesShortSegmentsAndCurves(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name               string
		points, other      []Point
		wantStart, wantEnd float64
	}{
		{"long safe approach", []Point{{}, {X: 1000}}, []Point{{X: 1000}, {X: 1000, Y: 100}}, 988, 1000},
		{"short endpoint", []Point{{X: 12.1}, {X: 11.9}}, []Point{{}, {Y: 1}}, 0.1, 0.2},
		{"interior bend", []Point{{X: 30}, {X: 10}, {X: 30, Y: 10}}, []Point{{Y: -1}, {Y: 1}}, 19, 21},
		{"separated", []Point{{X: 30}, {X: 40}}, []Point{{}, {Y: 1}}, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			start, end := conflictExtent(newConflictPolyline(tc.points), newConflictPolyline(tc.other))
			if tc.name == "separated" {
				if !math.IsInf(start, 1) {
					t.Fatalf("unexpected conflict: %g to %g", start, end)
				}
				return
			}
			if start > tc.wantStart+1e-9 || end < tc.wantEnd-1e-9 {
				t.Fatalf("extent %g to %g omits known conflict %g to %g", start, end, tc.wantStart, tc.wantEnd)
			}
		})
	}
}

func TestJunctionAdmissionWaitsForWholeConflictZone(t *testing.T) {
	t.Parallel()
	junction := resource{kind: junctionResource, id: "merge"}
	exit := resource{kind: trackResource, id: "out", cell: 1}
	s := &Simulation{
		owners: map[resource]resourceOwner{exit: podResourceOwner("leader")},
		vehicles: []vehicle{{
			Pod:             Pod{ID: "follower", Activity: Traveling},
			reservedThrough: -1,
			blocks:          blockListOf([]block{{resources: []resource{junction}}, {resources: []resource{junction, exit}}, {}}),
		}},
	}
	s.grant(intent{index: 0, block: 0})
	v := &s.vehicles[0]
	if v.reservedThrough != -1 || !s.owners[junction].isZero() || v.Pod.BlockedBy != "leader" {
		t.Fatalf("pod entered conflict without its exit: reserved=%d owners=%v blocker=%s", v.reservedThrough, s.owners, v.Pod.BlockedBy)
	}
	delete(s.owners, exit)
	s.grant(intent{index: 0, block: 0})
	if v.reservedThrough != 1 || s.owners[junction] != podResourceOwner("follower") || s.owners[exit] != podResourceOwner("follower") {
		t.Fatalf("pod did not acquire cleared conflict atomically: reserved=%d owners=%v", v.reservedThrough, s.owners)
	}
}

// TestJunctionPairsCountsBuildPairs checks that JunctionPairs gives the
// number of pairs that buildJunctionConflicts compares at each node. A loop
// lane has two entries at its node, and the build does not compare them.
func TestJunctionPairsCountsBuildPairs(t *testing.T) {
	t.Parallel()
	networks := scenarioNetworks(t)
	networks["example"] = Example()
	loop := Example().clone()
	node := loop.Nodes[0]
	loop.Lanes = append(loop.Lanes, Lane{ID: "loop", From: node.ID, To: node.ID, SpeedLimit: 12, Control: &Point{X: node.Position.X, Y: node.Position.Y + 100}})
	networks["loop"] = loop
	for name, network := range networks {
		index := make(map[string]int, len(network.Nodes))
		for position, node := range network.Nodes {
			index[node.ID] = position
		}
		compared := make([]int, len(network.Nodes))
		forEachJunctionPair(network, func(node string, _, _ Lane) { compared[index[node]]++ })
		if got := network.JunctionPairs(); !reflect.DeepEqual(got, compared) {
			t.Errorf("%s: JunctionPairs = %v, want %v", name, got, compared)
		}
	}
	// Each of the k lanes at the node pairs with each of the two loop
	// entries, in both orders.
	k := 0
	for _, lane := range Example().Lanes {
		if lane.From == node.ID || lane.To == node.ID {
			k++
		}
	}
	if got, want := loop.JunctionPairs()[0], k*(k-1)+4*k; got != want {
		t.Errorf("pairs at the loop node = %d, want %d", got, want)
	}
}
