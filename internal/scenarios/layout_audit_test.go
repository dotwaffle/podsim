package scenarios

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// TestPresetOutputIsPinned pins the JSON form of each preset. A change to a
// generator that changes a preset must also change this test.
func TestPresetOutputIsPinned(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		config func() project.Config
		sha256 string
	}{
		{name: "small", config: Small, sha256: "8ddaef835b7f91751b311dcfd6b4d9238dc8c5977da40512ae753ef10ab4f09a"},
		{name: "busy", config: Busy, sha256: "a79b34e250155e4e8e1ee883dad535fb50e2896e892ec5bd03f7e5575a2faea2"},
		{name: "parking constrained", config: ParkingConstrained, sha256: "70efdca391170884c1ba79376750512ae4c020c42d7fc25574c383c1cbcd6400"},
		{name: "rail hub", config: RailHub, sha256: "ce145a92886574f527ce3f2a078c6188a8dbe4874688a49b17dd8c24d1588069"},
		{name: "scale 100", config: Scale100, sha256: "3c9377cde8efee8fa954dd5a8d7168b0ad3013dfcd8ec805e0444c96e3bb54a0"},
		{name: "LondonCentral", config: LondonCentral, sha256: "8eef2953e1ea2a5cc9d135a61f69356af6c37483610739dd091afcf3ee3622bc"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			data, err := json.Marshal(test.config())
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(data)
			if got := hex.EncodeToString(sum[:]); got != test.sha256 {
				t.Fatalf("preset SHA-256 = %s, want %s", got, test.sha256)
			}
		})
	}
}

func TestAuditPresetsHaveNoHardConflicts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		config func() project.Config
	}{
		{name: "small", config: Small},
		{name: "busy", config: Busy},
		{name: "parking constrained", config: ParkingConstrained},
		{name: "rail hub", config: RailHub},
		{name: "scale 100", config: Scale100},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			network := test.config().Network
			if err := layoutError(network, auditPlanarLayout(network)); err != nil {
				t.Fatal(err)
			}
		})
	}
	t.Run("London", func(t *testing.T) {
		t.Parallel()
		var source londonSource
		if err := decodeLondonSource(&source); err != nil {
			t.Fatal(err)
		}
		network := LondonCentral().Network
		conflicts := auditLondonLayout(network, newLondonAuditInput(source))
		if err := layoutError(network, conflicts); err != nil {
			t.Fatal(err)
		}
		// Each heading of Mansion House crosses a guideway or puts its
		// berths nearer to another station.
		if len(conflicts) != 1 || conflicts[0].station != "940GZZLUMSH" {
			t.Fatalf("soft conflicts = %+v, want one at Mansion House", conflicts)
		}
	})
}

func TestAuditPlanarLayout(t *testing.T) {
	t.Parallel()
	node := func(id string, x, y float64) sim.Node {
		return sim.Node{ID: id, Position: sim.Point{X: x, Y: y}}
	}
	lane := func(id, from, to string) sim.Lane {
		return sim.Lane{ID: id, From: from, To: to, StationID: "station-01"}
	}
	nodes := []sim.Node{
		node("a", 0, 0), node("b", 100, 0), node("c", 50, -50), node("d", 50, 50),
		node("e", 0, 10), node("f", 100, 10), node("g", 0, 22), node("h", 100, 22),
	}
	tests := []struct {
		name  string
		lanes []sim.Lane
		// want is the number of hard conflicts.
		want   int
		reason string
	}{
		{name: "crossing", lanes: []sim.Lane{lane("ab", "a", "b"), lane("cd", "c", "d")}, want: 1, reason: `lane "ab" crosses lane "cd"`},
		{name: "nearer than the clearance", lanes: []sim.Lane{lane("ab", "a", "b"), lane("ef", "e", "f")}, want: 1, reason: `lane "ab" is nearer than 12 meters to lane "ef"`},
		{name: "at the clearance", lanes: []sim.Lane{lane("ef", "e", "f"), lane("gh", "g", "h"), lane("ab", "a", "b")}, want: 1},
		{name: "common node", lanes: []sim.Lane{lane("ab", "a", "b"), lane("ac", "a", "c"), lane("cb", "c", "b")}, want: 0},
		{name: "apart", lanes: []sim.Lane{lane("ab", "a", "b"), lane("gh", "g", "h")}, want: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			network := sim.Network{
				Nodes: nodes, Lanes: test.lanes,
				Stations: []sim.Station{{ID: "station-01", Name: "Station 01"}},
			}
			conflicts := auditPlanarLayout(network)
			if len(conflicts) != test.want {
				t.Fatalf("got %d conflicts, want %d: %+v", len(conflicts), test.want, conflicts)
			}
			err := layoutError(network, conflicts)
			if (err != nil) != (test.want > 0) {
				t.Fatalf("layoutError = %v", err)
			}
			if test.reason != "" && !strings.Contains(err.Error(), "station Station 01 (station-01): "+test.reason) {
				t.Fatalf("error %q does not name the station and %q", err, test.reason)
			}
		})
	}
}

// TestAuditBankApproachNearCrossing retains the geometry of the two rejected
// service-study arms. The road misses the bank endpoint but violates clearance.
func TestAuditBankApproachNearCrossing(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name      string
		rowY      float64
		conflicts int
	}{
		{name: "rejected row", rowY: -320, conflicts: 2},
		{name: "row clear of road", rowY: -290, conflicts: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			network := sim.Network{
				Nodes: []sim.Node{
					{ID: "road-exit", Position: sim.Point{X: 848.1436689394465, Y: -935.2284837610192}},
					{ID: "entry", Position: sim.Point{X: 1260, Y: -80}},
					{ID: "arrival-1", Position: sim.Point{X: 1140, Y: -230}},
					{ID: "arrival-2", Position: sim.Point{X: 1140, Y: test.rowY}},
					{ID: "berth", Position: sim.Point{X: 1020, Y: test.rowY}},
				},
				Lanes: []sim.Lane{
					{ID: "road", From: "road-exit", To: "entry", SpeedLimit: 14},
					{ID: "arrival", From: "arrival-1", To: "arrival-2", SpeedLimit: 14, StationID: "hub", StationRole: sim.StationBerthAccessRole},
					{ID: "inlet", From: "arrival-2", To: "berth", SpeedLimit: 14, StationID: "hub", StationRole: sim.StationBerthAccessRole},
				},
				Stations: []sim.Station{{ID: "hub", Name: "Rail Hub"}},
			}
			lanes := newAuditLanes(network)
			if lanes[0].crosses(lanes[1]) || lanes[0].crosses(lanes[2]) {
				t.Fatal("fixture must retain a near-crossing without a segment intersection")
			}
			conflicts := auditPlanarLayout(network)
			if len(conflicts) != test.conflicts {
				t.Fatalf("conflicts = %+v, want %d", conflicts, test.conflicts)
			}
			for _, conflict := range conflicts {
				if conflict.station != "hub" || conflict.first != `lane "road"` || conflict.reason != "is nearer than 12 meters to" {
					t.Fatalf("unexpected conflict: %+v", conflict)
				}
			}
			if err := layoutError(network, conflicts); (err != nil) != (test.conflicts > 0) {
				t.Fatalf("layoutError = %v", err)
			}
		})
	}
}

func TestAuditLondonLayoutFindsConflicts(t *testing.T) {
	t.Parallel()
	var source londonSource
	if err := decodeLondonSource(&source); err != nil {
		t.Fatal(err)
	}
	input := newLondonAuditInput(source)
	tests := []struct {
		name string
		// change moves a node of the London network.
		change func(network *sim.Network)
		want   string
	}{
		{
			name: "core lane across a guideway",
			change: func(network *sim.Network) {
				moveNode(network, "940GZZLUKSX-01-node", nodePosition(*network, laneByID(*network, "940GZZLUKSX-road-in-01").From))
			},
			want: "King's Cross St. Pancras (940GZZLUKSX): lane",
		},
		{
			name: "short road lane",
			change: func(network *sim.Network) {
				portal := nodePosition(*network, laneByID(*network, "940GZZLUKSX-road-in-01").From)
				moveNode(network, "940GZZLUKSX-diverge", add(portal, sim.Point{X: 5}))
			},
			want: `(940GZZLUKSX): road lane "940GZZLUKSX-road-in-01" is 5.0 meters`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			network := LondonCentral().Network
			test.change(&network)
			err := layoutError(network, auditLondonLayout(network, input))
			t.Log(err)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("layoutError = %v, want an error with %q", err, test.want)
			}
		})
	}
}

// TestAuditLondonCoreLaneTouches checks that a core lane conflicts with a
// guideway that it touches, unless the two lanes touch only at a common
// node.
func TestAuditLondonCoreLaneTouches(t *testing.T) {
	t.Parallel()
	node := func(id string, x, y float64) sim.Node {
		return sim.Node{ID: id, Position: sim.Point{X: x, Y: y}}
	}
	guideway := func(id, from, to string) sim.Lane { return sim.Lane{ID: id, From: from, To: to} }
	core := func(from, to string) sim.Lane { return sim.Lane{ID: "core", From: from, To: to, StationID: "station"} }
	nodes := []sim.Node{
		node("west", -50, 0), node("split", 0, 0), node("east", 50, 0),
		node("south", 0, -50), node("north", 0, 50), node("inner-west", -20, 0), node("inner-east", 20, 0), node("north-east", 20, 50),
	}
	split := []sim.Lane{guideway("west-split", "west", "split"), guideway("split-east", "split", "east")}
	whole := []sim.Lane{guideway("west-east", "west", "east")}
	tests := []struct {
		name  string
		lanes []sim.Lane
		want  bool
	}{
		{name: "through a split point", lanes: append(split, core("south", "north")), want: true},
		{name: "end on a guideway", lanes: append(whole, core("split", "north")), want: true},
		{name: "overlap", lanes: append(whole, core("inner-west", "inner-east")), want: true},
		{name: "overlap from a common node", lanes: append(split, core("west", "inner-west")), want: true},
		{name: "common node", lanes: append(split, core("split", "north")), want: false},
		{name: "apart", lanes: append(whole, core("north", "north-east")), want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			network := sim.Network{
				Nodes: nodes, Lanes: test.lanes,
				Stations: []sim.Station{{ID: "station", Name: "Station", Entry: "south", Exit: "north", Berths: []sim.Berth{{ID: "berth", Node: "north"}}}},
			}
			err := layoutError(network, auditLondonLayout(network, londonAuditInput{}))
			if got := err != nil && strings.Contains(err.Error(), `lane "core" crosses guideway`); got != test.want {
				t.Fatalf("layoutError = %v, want a crossing: %t", err, test.want)
			}
		})
	}
}

func nodePosition(network sim.Network, id string) sim.Point {
	node, ok := network.Node(id)
	if !ok {
		panic("unknown node " + id)
	}
	return node.Position
}

func laneByID(network sim.Network, id string) sim.Lane {
	for _, lane := range network.Lanes {
		if lane.ID == id {
			return lane
		}
	}
	panic("unknown lane " + id)
}

func moveNode(network *sim.Network, id string, position sim.Point) {
	for index := range network.Nodes {
		if network.Nodes[index].ID == id {
			network.Nodes[index].Position = position
			return
		}
	}
	panic("unknown node " + id)
}
