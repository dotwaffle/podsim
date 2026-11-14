package ride

import (
	"math"
	"testing"
	"time"

	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

func testNetwork() sim.Network {
	return sim.Network{
		Nodes: []sim.Node{
			{ID: "a", Position: sim.Point{X: 0, Y: 0}},
			{ID: "b", Position: sim.Point{X: 100, Y: 0}},
			{ID: "c", Position: sim.Point{X: 100, Y: 100}},
		},
		Lanes: []sim.Lane{
			{ID: "east", From: "a", To: "b", SpeedLimit: 10},
			{ID: "south", From: "b", To: "c", SpeedLimit: 10},
			// The curve starts east and ends south.
			{ID: "curve", From: "a", To: "c", Control: &sim.Point{X: 100, Y: 0}, SpeedLimit: 8, StationID: "s"},
			// A pod on back moves west and faces east.
			{ID: "back", From: "b", To: "a", SpeedLimit: 2, StationID: "s"},
		},
		ManeuverGroups: []sim.ManeuverGroup{{ID: "g", StationID: "s", LaneIDs: []string{"east"}, ReverseLaneIDs: []string{"back"}}},
		Stations:       []sim.Station{{ID: "s", Name: "Stop", Berths: []sim.Berth{{ID: "s1", Node: "c"}}}},
	}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestLaneTangent(t *testing.T) {
	table := newLaneTable(testNetwork())
	curve := table["curve"]
	end := curve.lengths[len(curve.lengths)-1]
	tests := []struct {
		name     string
		lane     string
		distance float64
		want     float64
		ok       bool
	}{
		{"straight east", "east", 50, 0, true},
		// World y points south, so a lane to +y has a heading of +pi/2.
		{"straight south", "south", 10, math.Pi / 2, true},
		{"curve start", "curve", 0, 0, true},
		{"curve before start", "curve", -1, 0, true},
		{"curve end", "curve", end, math.Pi / 2, true},
		{"curve past end", "curve", end + 5, math.Pi / 2, true},
		{"curve middle", "curve", end / 2, math.Pi / 4, true},
		{"unknown lane", "none", 0, 0, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := table.tangent(test.lane, test.distance)
			if ok != test.ok || !near(got, test.want) {
				t.Fatalf("tangent(%q, %v) = %v, %v, want %v, %v", test.lane, test.distance, got, ok, test.want, test.ok)
			}
		})
	}
}

func TestBerthHeadings(t *testing.T) {
	network := testNetwork()
	// Berth s2 is at node a. Only the reverse lane back arrives there.
	network.Stations[0].Berths = append(network.Stations[0].Berths, sim.Berth{ID: "s2", Node: "a"})
	// Berth t1 is at node b. The lane east arrives there, and it is not a
	// lane of station t.
	network.Stations = append(network.Stations, sim.Station{ID: "t", Berths: []sim.Berth{{ID: "t1", Node: "b"}}})
	// At node c, the lane south comes first, but the lane west is the only
	// lane of station s there.
	network.Lanes[2].StationID = ""
	network.Nodes = append(network.Nodes, sim.Node{ID: "d", Position: sim.Point{X: 200, Y: 100}})
	network.Lanes = append(network.Lanes, sim.Lane{ID: "west", From: "d", To: "c", SpeedLimit: 2, StationID: "s"})
	headings := newLaneTable(network).berthHeadings(network)
	want := map[string]float64{"s1": math.Pi, "t1": 0}
	if len(headings) != len(want) {
		t.Fatalf("berth headings = %v, want %v", headings, want)
	}
	for id, angle := range want {
		if got, ok := headings[id]; !ok || !near(got, angle) {
			t.Fatalf("berth headings = %v, want %v", headings, want)
		}
	}
}

func TestHeading(t *testing.T) {
	network := testNetwork()
	lanes := newLaneTable(network)
	bridge := Bridge{lanes: lanes, berths: lanes.berthHeadings(network)}
	at := func(angle float64) map[string]heading { return map[string]heading{"p": {angle: angle}} }
	tests := []struct {
		name       string
		pod        sim.Pod
		previous   map[string]heading
		want       float64
		wantAnchor sim.Point
	}{
		{"movement sets heading", sim.Pod{ID: "p", Position: sim.Point{X: 0, Y: 5}, LaneID: "east"}, at(0), math.Pi / 2, sim.Point{X: 0, Y: 5}},
		// A short movement keeps the heading and the anchor, so a slow pod
		// turns only after it moves headingStep from the anchor.
		{"short movement keeps heading", sim.Pod{ID: "p", Position: sim.Point{X: 0.05}, LaneID: "south"}, at(1), 1, sim.Point{}},
		{"stopped pod keeps heading", sim.Pod{ID: "p", LaneID: "south"}, at(1), 1, sim.Point{}},
		{"reverse lane faces against travel", sim.Pod{ID: "p", Position: sim.Point{X: -1}, LaneID: "back"}, at(0), 0, sim.Point{X: -1}},
		{"jump uses lane", sim.Pod{ID: "p", Position: sim.Point{X: 0, Y: 80}, LaneID: "south"}, at(0), math.Pi / 2, sim.Point{X: 0, Y: 80}},
		{"jump on reverse lane", sim.Pod{ID: "p", Position: sim.Point{X: 0, Y: 80}, LaneID: "back"}, at(1), 0, sim.Point{X: 0, Y: 80}},
		{"jump without lane keeps heading", sim.Pod{ID: "p", Position: sim.Point{X: 0, Y: 80}}, at(1), 1, sim.Point{X: 0, Y: 80}},
		{"new pod uses lane", sim.Pod{ID: "p", LaneID: "south"}, nil, math.Pi / 2, sim.Point{}},
		{"new pod on reverse lane", sim.Pod{ID: "p", LaneID: "back"}, nil, 0, sim.Point{}},
		{"new pod without lane", sim.Pod{ID: "p"}, nil, 0, sim.Point{}},
		// Berth s1 is at node c. The lane south arrives there, and the curve
		// of the same station also arrives there going south.
		{"new pod at berth", sim.Pod{ID: "p", StationID: "s", BerthID: "s1"}, nil, math.Pi / 2, sim.Point{}},
		{"jump to berth", sim.Pod{ID: "p", Position: sim.Point{X: 0, Y: 80}, StationID: "s", BerthID: "s1"}, at(1), math.Pi / 2, sim.Point{X: 0, Y: 80}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := bridge.heading(test.pod, test.previous)
			if !near(got.angle, test.want) || got.anchor != test.wantAnchor {
				t.Fatalf("heading = %+v, want angle %v anchor %v", got, test.want, test.wantAnchor)
			}
		})
	}
}

func pausedState(revision uint64, pods ...sim.Pod) session.State {
	vehicles := make([]sim.Vehicle, len(pods))
	for i, pod := range pods {
		vehicles[i] = sim.Vehicle{Pod: pod}
	}
	return session.State{
		Epoch: "e", ServerStart: "s", Generation: 1, Revision: revision, Network: testNetwork(),
		Simulation: sim.Snapshot{Paused: true, Vehicles: vehicles},
	}
}

func TestFramePacksPoses(t *testing.T) {
	var bridge Bridge
	now := time.Unix(1000, 0)
	first := sim.Pod{ID: "01", Class: sim.GroupClass, Position: sim.Point{X: 3, Y: 4}, Speed: 2, LaneID: "south"}
	second := sim.Pod{ID: "02", Position: sim.Point{X: 7, Y: 8}, LaneID: "east"}
	bridge.Observe(pausedState(1, first, second), now)
	poses, info := bridge.Frame(now, nil)
	want := []float64{3, 4, math.Pi / 2, 2, 2, 7, 8, 0, 0, 0}
	if info.Count != 2 || info.Generation != 1 || info.Session != "s/e/1" || len(poses) != len(want) {
		t.Fatalf("frame = %v, %+v", poses, info)
	}
	for i := range want {
		if !near(poses[i], want[i]) {
			t.Fatalf("poses = %v, want %v", poses, want)
		}
	}
	if ids := bridge.IDs(); len(ids) != 2 || ids[0] != "01" || ids[1] != "02" {
		t.Fatalf("IDs = %v", ids)
	}

	// The same pods keep the version. A moved pod gets a heading from its
	// movement.
	moved := first
	moved.Position = sim.Point{X: 13, Y: 4}
	bridge.Observe(pausedState(2, moved, second), now)
	poses, next := bridge.Frame(now, poses)
	if next.Version != info.Version || !near(poses[2], 0) {
		t.Fatalf("after move: version %d then %d, heading %v", info.Version, next.Version, poses[2])
	}

	// A new pod order changes the version.
	bridge.Observe(pausedState(3, second, moved), now)
	_, last := bridge.Frame(now, poses)
	if last.Version == next.Version {
		t.Fatalf("version did not change after reorder: %d", last.Version)
	}
}

func TestNewTopology(t *testing.T) {
	topology := NewTopology(pausedState(1))
	if len(topology.Lanes) != 4 || topology.Lanes[2].Control == nil || topology.Lanes[2].StationID != "s" {
		t.Fatalf("lanes = %+v", topology.Lanes)
	}
	if len(topology.Stations) != 1 || len(topology.Stations[0].Berths) != 1 || topology.Stations[0].Berths[0] != (sim.Point{X: 100, Y: 100}) {
		t.Fatalf("stations = %+v", topology.Stations)
	}
	wantLengths := []float64{4, 4, 6, 6}
	for i, class := range topology.Classes {
		if class.LengthMeters != wantLengths[i] || class.Name != Classes[i] {
			t.Fatalf("classes = %+v", topology.Classes)
		}
	}
}

func TestPods(t *testing.T) {
	state := pausedState(1, sim.Pod{ID: "01", Activity: sim.Traveling}, sim.Pod{ID: "02", Class: sim.CompactClass})
	state.Simulation.Vehicles[0].Riders = []sim.Request{
		{ID: 1, From: "x", To: "y", Completed: true},
		// Station s has the name Stop, and station t is not in the network.
		{ID: 2, From: "s", To: "t"},
		{ID: 3, From: "u", To: "v"},
	}
	pods := Pods(state)
	if len(pods) != 2 {
		t.Fatalf("pods = %+v", pods)
	}
	if got := pods[0]; got.Class != sim.LegacyClass || got.Riders != 2 || got.From != "Stop" || got.To != "t" || got.Activity != sim.Traveling {
		t.Fatalf("first pod = %+v", got)
	}
	if got := pods[1]; got.Class != sim.CompactClass || got.Riders != 0 || got.From != "" {
		t.Fatalf("second pod = %+v", got)
	}
}
