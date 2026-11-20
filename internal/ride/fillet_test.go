package ride

import (
	"bytes"
	"encoding/json/v2"
	"flag"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dotwaffle/podsim/internal/sim"
)

var updateFillets = flag.Bool("update-fillets", false, "rewrite testdata/fillets.json")

// cornerNetwork has a 100 m lane east from a to b and a 100 m lane south
// from b to c, so the join at b turns by 90 degrees.
func cornerNetwork() sim.Network {
	return sim.Network{
		Nodes: []sim.Node{
			{ID: "a", Position: sim.Point{X: 0, Y: 0}},
			{ID: "b", Position: sim.Point{X: 100, Y: 0}},
			{ID: "c", Position: sim.Point{X: 100, Y: 100}},
		},
		Lanes: []sim.Lane{
			{ID: "east", From: "a", To: "b", SpeedLimit: 10},
			{ID: "south", From: "b", To: "c", SpeedLimit: 10},
		},
	}
}

func filletBridge(network sim.Network) *Bridge {
	lanes := newLaneTable(network)
	lanes.addDistances(network)
	return &Bridge{lanes: lanes, pieces: pieceIndex(lanes.addFillets(network))}
}

// angleBetween returns the angle in radians between the vectors a and b.
func angleBetween(a, b sim.Point) float64 {
	return math.Abs(wrap(math.Atan2(b.Y, b.X) - math.Atan2(a.Y, a.X)))
}

// checkTangents fails when a piece does not continue the direction of its
// two lanes at its two ends.
func checkTangents(t *testing.T, lanes laneTable, p piece) {
	t.Helper()
	_, in := lanes[p.key.in].at(lanes.length(p.key.in) - lanes[p.key.in].trimEnd)
	_, out := lanes[p.key.out].at(lanes[p.key.out].trimStart)
	start := sim.Point{X: p.control.X - p.start.X, Y: p.control.Y - p.start.Y}
	end := sim.Point{X: p.end.X - p.control.X, Y: p.end.Y - p.control.Y}
	if got := angleBetween(start, in); got > 1e-6 {
		t.Errorf("piece %v starts %v rad off the lane direction", p.key, got)
	}
	if got := angleBetween(end, out); got > 1e-6 {
		t.Errorf("piece %v ends %v rad off the lane direction", p.key, got)
	}
}

func TestFilletGeometry(t *testing.T) {
	network := cornerNetwork()
	// A curve leaves b to the south-east. A 10 m lane leaves c, so its trims
	// are 40% of its length, and a curve leaves its end to the south.
	network.Nodes = append(network.Nodes,
		sim.Node{ID: "d", Position: sim.Point{X: 200, Y: 100}},
		sim.Node{ID: "e", Position: sim.Point{X: 110, Y: 100}},
		sim.Node{ID: "f", Position: sim.Point{X: 120, Y: 110}},
	)
	network.Lanes = append(network.Lanes,
		sim.Lane{ID: "bend", From: "b", To: "d", Control: &sim.Point{X: 150, Y: 50}, SpeedLimit: 10},
		sim.Lane{ID: "short", From: "c", To: "e", SpeedLimit: 10},
		sim.Lane{ID: "hook", From: "e", To: "f", Control: &sim.Point{X: 110, Y: 110}, SpeedLimit: 10},
	)
	lanes := newLaneTable(network)
	pieces := lanes.addFillets(network)
	wantTrims := map[string][2]float64{"east": {0, 8}, "south": {8, 8}, "bend": {8, 0}, "short": {4, 4}, "hook": {math.Round(0.4*lanes.length("hook")*1000) / 1000, 0}}
	for id, want := range wantTrims {
		if got := [2]float64{lanes[id].trimStart, lanes[id].trimEnd}; !near(got[0], want[0]) || !near(got[1], want[1]) {
			t.Errorf("trims of %s = %v, want %v", id, got, want)
		}
	}
	keys := []pieceKey{{"east", "south"}, {"east", "bend"}, {"south", "short"}, {"short", "hook"}}
	if len(pieces) != len(keys) {
		t.Fatalf("pieces = %+v", pieces)
	}
	for i, p := range pieces {
		if p.key != keys[i] {
			t.Fatalf("piece %d = %v, want %v", i, p.key, keys[i])
		}
		checkTangents(t, lanes, p)
		// Each end of a piece is on its lane at the trim.
		if start, _ := lanes[p.key.in].at(lanes.length(p.key.in) - lanes[p.key.in].trimEnd); start != p.start {
			t.Errorf("piece %v starts at %v, not on its lane at %v", p.key, p.start, start)
		}
	}
	// Two straight lanes have their node as the control point.
	if pieces[0].control != (sim.Point{X: 100, Y: 0}) || pieces[0].start != (sim.Point{X: 92, Y: 0}) || pieces[0].end != (sim.Point{X: 100, Y: 8}) {
		t.Errorf("corner piece = %+v", pieces[0])
	}
}

func TestFilletExclusions(t *testing.T) {
	tests := []struct {
		name   string
		change func(*sim.Network)
		round  bool
	}{
		{"corner", func(*sim.Network) {}, true},
		{"berth node", func(n *sim.Network) {
			n.Stations = []sim.Station{{ID: "s", Berths: []sim.Berth{{ID: "s1", Node: "b"}}}}
		}, false},
		// A lane from b back to the west turns by 170 degrees after east.
		{"reversal", func(n *sim.Network) {
			n.Nodes = append(n.Nodes, sim.Node{ID: "w", Position: sim.Point{X: 0, Y: 17.6}})
			n.Lanes = append(n.Lanes, sim.Lane{ID: "back", From: "b", To: "w", SpeedLimit: 2})
		}, false},
		// A reverse lane from b to the south-east turns by 45 degrees after
		// east, so only the reverse lane rule keeps the joins sharp.
		{"reverse lane", func(n *sim.Network) {
			n.Nodes = append(n.Nodes, sim.Node{ID: "r", Position: sim.Point{X: 150, Y: 50}})
			n.Lanes = append(n.Lanes, sim.Lane{ID: "bay", From: "b", To: "r", SpeedLimit: 2})
			n.ManeuverGroups = []sim.ManeuverGroup{{ID: "g", LaneIDs: []string{"east"}, ReverseLaneIDs: []string{"bay"}}}
		}, false},
		// A lane on the same nodes in the other direction is a reversal, so
		// the lanes of a twin pair never get a trim.
		{"twin lane", func(n *sim.Network) {
			n.Lanes = append(n.Lanes, sim.Lane{ID: "west", From: "b", To: "a", SpeedLimit: 10})
		}, false},
		{"straight", func(n *sim.Network) {
			n.Nodes[2].Position = sim.Point{X: 200, Y: 5}
		}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			network := cornerNetwork()
			test.change(&network)
			lanes := newLaneTable(network)
			pieces := lanes.addFillets(network)
			trimmed := false
			for _, shape := range lanes {
				trimmed = trimmed || shape.trimStart != 0 || shape.trimEnd != 0
			}
			if trimmed != test.round || (len(pieces) > 0) != test.round {
				t.Fatalf("trims %v and %d pieces, want round %v: %+v", trimmed, len(pieces), test.round, lanes)
			}
		})
	}
}

// kinked returns the position of a pod on the corner network at route
// distance u from the trim of the lane east.
func kinked(u float64) (sim.Pod, sim.Point) {
	if s := 92 + u; s <= 100 {
		return sim.Pod{ID: "p", LaneID: "east", LaneDistance: s, Position: sim.Point{X: s}}, sim.Point{X: s}
	}
	s := u - 8
	return sim.Pod{ID: "p", LaneID: "south", LaneDistance: s, Position: sim.Point{X: 100, Y: s}}, sim.Point{X: 100, Y: s}
}

func TestFilletPose(t *testing.T) {
	network := cornerNetwork()
	bridge := filletBridge(network)
	route := network.Lanes
	shown := func(pod sim.Pod) sim.Point {
		return bridge.show(sim.Vehicle{Pod: pod, Route: route}, heading{}).pod.Position
	}
	// The piece meets the lanes at the trims with no gap.
	for _, cut := range []float64{0, 16} {
		for _, side := range []float64{-1e-6, 1e-6} {
			pod, want := kinked(cut + side)
			if got := shown(pod); math.Hypot(got.X-want.X, got.Y-want.Y) > 0.01 {
				t.Fatalf("at route distance %v the pod is at %v, want %v", cut+side, got, want)
			}
		}
	}
	// With the node as the control point, the curve passes the node at
	// 2 * sqrt(2) m, half way along, where the pod in the simulation is
	// at the node.
	bound, largest := 2*math.Sqrt2, 0.0
	for u := 0.0; u <= 16; u += 0.01 {
		pod, want := kinked(u)
		got := shown(pod)
		largest = max(largest, math.Hypot(got.X-want.X, got.Y-want.Y))
	}
	if largest > bound+1e-6 || largest < bound-0.01 {
		t.Fatalf("largest offset from the simulated position = %v m, want %v m", largest, bound)
	}
	// The heading follows the piece: half way, the pod moves south-east.
	before, _ := kinked(7.9)
	after, _ := kinked(8.1)
	last := heading{anchor: shown(before)}
	moved := bridge.show(sim.Vehicle{Pod: after, Route: route}, last)
	if got := bridge.heading(moved.pod, moved.along, map[string]heading{"p": last}).angle; math.Abs(got-math.Pi/4) > 0.01 {
		t.Fatalf("heading half way = %v, want about pi/4", got)
	}
}

func TestFilletPoseNeedsNeighbor(t *testing.T) {
	bridge := filletBridge(cornerNetwork())
	end := sim.Pod{ID: "p", LaneID: "east", LaneDistance: 96, Position: sim.Point{X: 96}}
	start := sim.Pod{ID: "p", LaneID: "south", LaneDistance: 4, Position: sim.Point{X: 100, Y: 4}}
	tests := []struct {
		name  string
		pod   sim.Pod
		last  heading
		moves bool
	}{
		{"no next lane", end, heading{}, false},
		{"no previous lane", start, heading{}, false},
		{"remembered previous lane", start, heading{lane: "east"}, true},
		{"remembered lane before the same lane", start, heading{lane: "south", previous: "east"}, true},
		{"remembered lane that does not join", start, heading{lane: "south"}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := bridge.show(sim.Vehicle{Pod: test.pod}, test.last).pod
			if moved := got.Position != test.pod.Position; moved != test.moves {
				t.Fatalf("pod at %v drawn at %v, want moved %v", test.pod.Position, got.Position, test.moves)
			}
		})
	}
}

func TestLocateFollowsPresentation(t *testing.T) {
	network := cornerNetwork()
	lanes := newLaneTable(network)
	// The motion buffer moved the pod 5 m past b, and kept the lane and the
	// lane distance of the earlier frame. A stream view holds the route in
	// Presentation, and Route then has the lanes in network order.
	vehicle := sim.Vehicle{
		Pod:          sim.Pod{ID: "p", LaneID: "east", LaneDistance: 99, Position: sim.Point{X: 100, Y: 5}},
		Route:        []sim.Lane{network.Lanes[1], network.Lanes[0]},
		Presentation: &sim.RoutePresentation{Start: 3, Current: 3, Motion: []sim.Lane{network.Lanes[0], network.Lanes[1]}},
	}
	got, ok := lanes.locate(vehicle)
	if want := (place{lane: "south", previous: "east", distance: 5}); !ok || got.lane != want.lane || got.previous != want.previous || got.next != "" || !near(got.distance, want.distance) {
		t.Fatalf("locate = %+v, %v, want %+v", got, ok, want)
	}
	// A position off the route is not found.
	vehicle.Pod.Position = sim.Point{X: 50, Y: 50}
	if got, ok := lanes.locate(vehicle); ok {
		t.Fatalf("locate off the route = %+v", got)
	}
}

func TestFramePutsPodOnPiece(t *testing.T) {
	var bridge Bridge
	now := time.Unix(1000, 0)
	state := pausedState(1)
	state.Network = cornerNetwork()
	pod, want := kinked(8)
	state.Simulation.Vehicles = []sim.Vehicle{{Pod: pod, Route: state.Network.Lanes}}
	bridge.Observe(state, now)
	poses, _ := bridge.Frame(now, nil)
	if got := (sim.Point{X: poses[0], Y: poses[1]}); !near(got.X, 98) || !near(got.Y, 2) || math.Hypot(got.X-want.X, got.Y-want.Y) > 2*math.Sqrt2+1e-9 {
		t.Fatalf("pose = %v, want the middle of the piece", poses)
	}
	if !near(poses[5], levels.MainTop) {
		t.Fatalf("height = %v", poses[5])
	}
	// A new pod half way along the piece faces along the piece, not along
	// the lane east.
	if !near(poses[2], math.Pi/4) {
		t.Fatalf("heading = %v, want pi/4", poses[2])
	}
}

func TestTopologyPieces(t *testing.T) {
	state := pausedState(1)
	state.Network = cornerNetwork()
	topology := NewTopology(state)
	want := TopologyPiece{In: 0, Out: 1, Control: [2]float64{100, 0}}
	if len(topology.Pieces) != 1 || topology.Pieces[0] != want {
		t.Fatalf("pieces = %+v", topology.Pieces)
	}
	if lane := topology.Lanes[0]; lane.TrimStart != 0 || lane.TrimEnd != 8 {
		t.Fatalf("lane = %+v", lane)
	}
	if lane := topology.Lanes[1]; lane.TrimStart != 8 || lane.TrimEnd != 0 {
		t.Fatalf("lane = %+v", lane)
	}
}

// TestFilletFixture writes the topology of a join of curved lanes on a
// ramp. The node tests of web/ride.js read the same file and check that the
// beams of the lanes meet the pieces.
func TestFilletFixture(t *testing.T) {
	network := sim.Network{
		Nodes: []sim.Node{
			{ID: "m", Position: sim.Point{X: 0, Y: 0}},
			{ID: "n", Position: sim.Point{X: 100, Y: 100}},
			{ID: "o", Position: sim.Point{X: 0, Y: 200}},
			{ID: "p", Position: sim.Point{X: 200, Y: 100}},
			// A lane of 10.0787 m leaves q, so its trim of 40% is not a
			// whole number of millimeters before the rounding.
			{ID: "e", Position: sim.Point{X: 50, Y: 0.00049}},
			{ID: "q", Position: sim.Point{X: 0.00049, Y: 0.00049}},
			{ID: "r", Position: sim.Point{X: -6.621139952, Y: -7.597813131}},
		},
		Lanes: []sim.Lane{
			{ID: "arc", From: "m", To: "n", Control: &sim.Point{X: 100, Y: 0}, SpeedLimit: 10},
			{ID: "away", From: "n", To: "o", Control: &sim.Point{X: 60, Y: 160}, SpeedLimit: 10, StationID: "s"},
			{ID: "east", From: "n", To: "p", SpeedLimit: 10},
			{ID: "into", From: "e", To: "q", SpeedLimit: 10},
			{ID: "odd", From: "q", To: "r", SpeedLimit: 10},
		},
	}
	state := pausedState(1)
	state.Network = network
	topology := NewTopology(state)
	if len(topology.Pieces) != 3 {
		t.Fatalf("pieces = %+v", topology.Pieces)
	}
	// ends holds the start and the end of each piece of the bridge, with no
	// rounding, so the page can check that it finds the same points.
	lanes := newLaneTable(network)
	var ends [][4]float64
	for _, p := range lanes.addFillets(network) {
		ends = append(ends, [4]float64{p.start.X, p.start.Y, p.end.X, p.end.Y})
	}
	want, err := json.Marshal(struct {
		Nodes  []sim.Node      `json:"nodes"`
		Lanes  []TopologyLane  `json:"lanes"`
		Pieces []TopologyPiece `json:"pieces"`
		Levels Levels          `json:"levels"`
		Ends   [][4]float64    `json:"ends"`
	}{topology.Nodes, topology.Lanes, topology.Pieces, topology.Levels, ends}, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	want = append(want, '\n')
	path := filepath.Join("testdata", "fillets.json")
	if *updateFillets {
		if werr := os.WriteFile(path, want, 0o644); werr != nil {
			t.Fatal(werr)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("the fixture differs, run go test -run TestFilletFixture -update-fillets: %v\n%s", err, want)
	}
}

// TestFrameFindsInterpolatedPodOnPiece moves a pod through the corner with
// remote.Motion. Between the two frames, the sample keeps the lane east of
// the earlier frame, but the position is past the node on the lane south.
func TestFrameFindsInterpolatedPodOnPiece(t *testing.T) {
	var bridge Bridge
	start := time.Unix(1000, 0)
	frame := func(revision uint64, tick int64, pod sim.Pod) {
		state := pausedState(revision)
		state.Network = cornerNetwork()
		state.Simulation.Paused = false
		state.Simulation.Tick = tick
		state.Simulation.Vehicles = []sim.Vehicle{{Pod: pod, Route: state.Network.Lanes}}
		bridge.Observe(state, start.Add(time.Duration(revision-1)*200*time.Millisecond))
	}
	// One second of travel at 10 m/s covers the 10 m between the frames.
	frame(1, 0, sim.Pod{ID: "p", LaneID: "east", LaneDistance: 95, Position: sim.Point{X: 95}})
	frame(2, sim.TicksPerSecond, sim.Pod{ID: "p", LaneID: "south", LaneDistance: 5, Position: sim.Point{X: 100, Y: 5}})
	// The motion buffer shows the time 250 ms before the frame time, so this
	// is three quarters of the way: 2.5 m along the lane south.
	poses, _ := bridge.Frame(start.Add(400*time.Millisecond), nil)
	want, _ := bridge.pieces[pieceKey{"east", "south"}].at((8 + 2.5) / 16)
	if got := (sim.Point{X: poses[0], Y: poses[1]}); !near(got.X, want.X) || !near(got.Y, want.Y) || near(got.X, 100) {
		t.Fatalf("pose = %v, want %v on the piece", poses[:2], want)
	}
}

func TestLocateRefusesTwoPlaces(t *testing.T) {
	network := cornerNetwork()
	lanes := newLaneTable(network)
	// A Route that holds the pod lane twice does not tell which one the pod
	// is on.
	twice := sim.Vehicle{
		Pod:   sim.Pod{ID: "p", LaneID: "east", LaneDistance: 50, Position: sim.Point{X: 50}},
		Route: []sim.Lane{network.Lanes[0], network.Lanes[1], network.Lanes[0]},
	}
	if got, ok := lanes.locate(twice); ok {
		t.Fatalf("locate on a route with the pod lane twice = %+v", got)
	}
	// A short loop comes back through the sampled position: 5 m along the
	// lane down and 5 m along the lane up are the same point.
	loop := sim.Network{
		Nodes: []sim.Node{{ID: "a", Position: sim.Point{X: 0, Y: 0}}, {ID: "b", Position: sim.Point{X: 10, Y: 0}}, {ID: "c", Position: sim.Point{X: 10, Y: 10}}},
		Lanes: []sim.Lane{{ID: "east", From: "a", To: "b"}, {ID: "down", From: "b", To: "c"}, {ID: "up", From: "c", To: "b"}},
	}
	vehicle := sim.Vehicle{Pod: sim.Pod{ID: "p", LaneID: "east", LaneDistance: 9, Position: sim.Point{X: 10, Y: 5}}, Route: loop.Lanes}
	if got, ok := newLaneTable(loop).locate(vehicle); ok {
		t.Fatalf("locate at two places of the route = %+v", got)
	}
	// At a node, the end of one lane and the start of the next are the same
	// place.
	corner := sim.Vehicle{Pod: sim.Pod{ID: "p", LaneID: "east", LaneDistance: 99, Position: sim.Point{X: 100}}, Route: network.Lanes}
	if got, ok := lanes.locate(corner); !ok || got.lane != "east" || !near(got.distance, 100) {
		t.Fatalf("locate at the node = %+v, %v", got, ok)
	}
}

func TestFilletNodeNeedsTangentPieces(t *testing.T) {
	// The tangent lines at the trim points meet behind the start of the
	// piece, so a piece cannot continue both lanes, and the node stays sharp.
	network := sim.Network{
		Nodes: []sim.Node{{ID: "a", Position: sim.Point{X: 24, Y: -11}}, {ID: "b", Position: sim.Point{X: 0, Y: 0}}, {ID: "c", Position: sim.Point{X: -14, Y: 8}}},
		Lanes: []sim.Lane{
			{ID: "in", From: "a", To: "b", Control: &sim.Point{X: -1, Y: 0}},
			{ID: "out", From: "b", To: "c", Control: &sim.Point{X: 0, Y: 1}},
		},
	}
	lanes := newLaneTable(network)
	if !lanes.roundNode([]string{"in"}, []string{"out"}) {
		t.Fatal("the turn rule keeps the node sharp, so the test does not reach the piece check")
	}
	pieces := lanes.addFillets(network)
	if len(pieces) != 0 || lanes["in"].trimEnd != 0 || lanes["out"].trimStart != 0 {
		t.Fatalf("pieces %+v, trims %v and %v", pieces, lanes["in"].trimEnd, lanes["out"].trimStart)
	}
}

func TestFilletCurveWithControlAtEnd(t *testing.T) {
	// The control point of the curve is its end, so the derivative there is
	// zero, and the direction is the limit: east.
	network := sim.Network{
		Nodes: []sim.Node{{ID: "a", Position: sim.Point{X: 0, Y: 0}}, {ID: "b", Position: sim.Point{X: 100, Y: 0}}, {ID: "c", Position: sim.Point{X: 100, Y: 100}}},
		Lanes: []sim.Lane{
			{ID: "flat", From: "a", To: "b", Control: &sim.Point{X: 100, Y: 0}},
			{ID: "south", From: "b", To: "c"},
		},
	}
	lanes := newLaneTable(network)
	if angle, ok := lanes.tangent("flat", math.Inf(1)); !ok || !near(angle, 0) {
		t.Fatalf("end direction = %v, %v", angle, ok)
	}
	pieces := lanes.addFillets(network)
	if len(pieces) != 1 || lanes["flat"].trimEnd != 8 {
		t.Fatalf("pieces = %+v", pieces)
	}
	checkTangents(t, lanes, pieces[0])
	// A lane with no length has no direction.
	network.Nodes[1].Position = sim.Point{}
	network.Lanes[0].Control = &sim.Point{}
	if _, ok := newLaneTable(network).tangent("flat", 0); ok {
		t.Fatal("a lane with no length has a direction")
	}
}

// TestFrameKeepsSharpJoin moves a pod into the trim at the end of the lane
// east with no route, so the bridge does not know the next lane. When the
// route comes, the pod keeps the sharp path until it leaves the trims of the
// join.
func TestFrameKeepsSharpJoin(t *testing.T) {
	var bridge Bridge
	now := time.Unix(1000, 0)
	network := cornerNetwork()
	steps := []struct {
		pod   sim.Pod
		route []sim.Lane
		sharp bool
	}{
		{sim.Pod{ID: "p", LaneID: "east", LaneDistance: 95, Position: sim.Point{X: 95}}, nil, true},
		{sim.Pod{ID: "p", LaneID: "east", LaneDistance: 95.05, Position: sim.Point{X: 95.05}}, network.Lanes, true},
		{sim.Pod{ID: "p", LaneID: "south", LaneDistance: 2, Position: sim.Point{X: 100, Y: 2}}, network.Lanes, true},
		{sim.Pod{ID: "p", LaneID: "south", LaneDistance: 9, Position: sim.Point{X: 100, Y: 9}}, network.Lanes, false},
	}
	var last sim.Point
	for i, step := range steps {
		state := pausedState(uint64(i + 1))
		state.Network = network
		state.Simulation.Vehicles = []sim.Vehicle{{Pod: step.pod, Route: step.route}}
		bridge.Observe(state, now)
		poses, _ := bridge.Frame(now, nil)
		got := sim.Point{X: poses[0], Y: poses[1]}
		if got != step.pod.Position || bridge.headings["p"].sharp != step.sharp {
			t.Fatalf("step %d: pod at %v drawn at %v, sharp %v", i, step.pod.Position, got, bridge.headings["p"].sharp)
		}
		if i == 1 && math.Hypot(got.X-last.X, got.Y-last.Y) > 0.05+0.01 {
			t.Fatalf("the pod jumped from %v to %v", last, got)
		}
		last = got
	}
}
