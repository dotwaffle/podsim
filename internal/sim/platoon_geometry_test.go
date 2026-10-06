package sim

import (
	"fmt"
	"math"
	"testing"
)

// geometryLane is a lane of geometryNetwork. It ends at to. With a control
// point, the lane is a curve.
type geometryLane struct {
	to      Point
	control *Point
}

// geometryNetwork returns a path of lanes l0, l1, and so on from start,
// with a destination station of one berth at the end of the path. The
// first pod to arrive stays at the berth, so the pods after it queue on
// the path. A small origin station, not connected to the path, holds the
// fleet and gives the requests a passenger origin. All lanes have a
// 14 m/s limit.
func geometryNetwork(start Point, lanes []geometryLane, fleet int) Network {
	return stationNetwork(start, lanes, fleet, Point{X: 60, Y: 60}, Point{Y: 120})
}

// stationNetwork returns the network of geometryNetwork, with the berth and
// the exit of the destination station at the given offsets from the end of
// the path.
func stationNetwork(start Point, lanes []geometryLane, fleet int, berth, exit Point) Network {
	end := lanes[len(lanes)-1].to
	network := Network{
		Nodes: []Node{
			{ID: "g0", Position: start},
			{ID: "dest-berth", Position: Point{X: end.X + berth.X, Y: end.Y + berth.Y}},
			{ID: "dest-exit", Position: Point{X: end.X + exit.X, Y: end.Y + exit.Y}},
			{ID: "origin-entry", Position: Point{X: -5000, Y: 5000}},
			{ID: "origin-exit", Position: Point{X: -4000, Y: 5000}},
		},
		Lanes: []Lane{
			{ID: "dest-through", From: "dest-entry", To: "dest-exit", SpeedLimit: 14},
			{ID: "dest-in", From: "dest-entry", To: "dest-berth", SpeedLimit: 14},
			{ID: "dest-out", From: "dest-berth", To: "dest-exit", SpeedLimit: 14},
			{ID: "origin-through", From: "origin-entry", To: "origin-exit", SpeedLimit: 14},
		},
		Stations: []Station{
			{ID: "origin", Name: "Origin", Entry: "origin-entry", Exit: "origin-exit"},
			{ID: "dest", Name: "Destination", Entry: "dest-entry", Exit: "dest-exit", Berths: []Berth{{ID: "dest-1", Node: "dest-berth"}}},
		},
	}
	for index := range fleet {
		node := fmt.Sprintf("origin-berth-%02d", index+1)
		network.Nodes = append(network.Nodes, Node{ID: node, Position: Point{X: -4960 + 25*float64(index), Y: 5060}})
		network.Lanes = append(network.Lanes,
			Lane{ID: node + "-in", From: "origin-entry", To: node, SpeedLimit: 14},
			Lane{ID: node + "-out", From: node, To: "origin-exit", SpeedLimit: 14})
		network.Stations[0].Berths = append(network.Stations[0].Berths, Berth{ID: fmt.Sprintf("origin-%02d", index+1), Node: node})
	}
	for index, lane := range lanes {
		from, to := fmt.Sprintf("g%d", index), fmt.Sprintf("g%d", index+1)
		if index == len(lanes)-1 {
			to = "dest-entry"
		}
		network.Nodes = append(network.Nodes, Node{ID: to, Position: lane.to})
		network.Lanes = append(network.Lanes, Lane{ID: fmt.Sprintf("l%d", index), From: from, To: to, SpeedLimit: 14, Control: lane.control})
	}
	return network
}

// restoreGeometry restores pods pods at rest on the first lane of network,
// gap meters apart, with the first pod at head. Each pod carries a party
// along the whole path to the destination station.
func restoreGeometry(t *testing.T, network Network, lanes, pods int, head, gap float64) *Simulation {
	t.Helper()
	laneIndex := make(map[string]int, len(network.Lanes))
	for index, lane := range network.Lanes {
		laneIndex[lane.ID] = index
	}
	route := make([]int, lanes)
	for index := range lanes {
		route[index] = laneIndex[fmt.Sprintf("l%d", index)]
	}
	var fleet []Placement
	state := SavedState{SharedRidePartyLimit: 1}
	for index := range pods {
		id := fmt.Sprintf("p%02d", index+1)
		fleet = append(fleet, Placement{ID: id, StationID: "origin", BerthID: fmt.Sprintf("origin-%02d", index+1)})
		state.RequestID++
		state.Boarded++
		distance := head - gap*float64(index)
		state.Pods = append(state.Pods, SavedPod{
			ID: id, Activity: "traveling", Occupied: true, Origin: fleet[index].BerthID, DestinationStation: "dest",
			Riders: []SavedRequest{{SharingConsent: SharedConsent, Service: OnDemandService, ID: state.RequestID, From: "origin", To: "dest", PartySize: 1, PodID: id}},
			Stops:  []string{"dest"},
			Route:  route, LaneID: "l0", LaneDistance: distance, Distance: distance,
		})
	}
	s, result, err := RestoreState(RestoreStateInput{Network: network, Fleet: fleet, State: state})
	if err != nil {
		t.Fatal(err)
	}
	if result.Tier != RestorePhysical || len(result.Demoted) != 0 {
		t.Fatalf("the pods did not restore in place: %+v", result)
	}
	return s
}

// TestPlatoonTurnSum checks the total turn of a run of lanes. Each change of
// heading counts one time, with its size and not its sign.
func TestPlatoonTurnSum(t *testing.T) {
	t.Parallel()
	corner := Point{X: 30}
	tests := []struct {
		name  string
		start Point
		lanes []geometryLane
		want  float64
	}{
		{name: "hairpin", start: Point{X: -100}, lanes: []geometryLane{{to: Point{}}, {to: Point{Y: 24}}, {to: Point{X: -24, Y: 24}}}, want: math.Pi},
		{name: "opposite turns", start: Point{X: -100}, lanes: []geometryLane{{to: Point{}}, {to: Point{Y: 24}}, {to: Point{X: 24, Y: 24}}}, want: math.Pi},
		{name: "curved lane", start: Point{X: -100}, lanes: []geometryLane{{to: Point{}}, {to: Point{X: 30, Y: 30}, control: &corner}, {to: Point{X: 30, Y: 90}}}, want: math.Pi / 2},
		// The headings are near pi and near -pi, so a plain difference of
		// the two is near 2 pi.
		{name: "heading wrap", start: Point{X: 100}, lanes: []geometryLane{{to: Point{Y: 1}}, {to: Point{X: -100, Y: -1}}}, want: math.Atan2(1, 100) + math.Atan2(2, 100)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			network := geometryNetwork(test.start, test.lanes, 1)
			s, err := NewFleet(network, []Placement{{ID: "p01", StationID: "origin", BerthID: "origin-01"}})
			if err != nil {
				t.Fatal(err)
			}
			route := make([]Lane, len(test.lanes))
			for index := range route {
				route[index] = network.Lanes[len(network.Lanes)-len(test.lanes)+index]
			}
			if got := s.runTurn(route, 0, len(route)-1); math.Abs(got-test.want) > 1e-6 {
				t.Fatalf("turn %v, want %v", got, test.want)
			}
		})
	}
}

// TestPlatoonLaneShape checks that a lane shape skips segments with no
// length, and that a sum of turns skips a lane with no length.
func TestPlatoonLaneShape(t *testing.T) {
	t.Parallel()
	shape := newLaneShape([]Point{{}, {}, {X: 1}, {X: 1}, {X: 1, Y: 1}})
	if shape != (laneShape{start: 0, end: math.Pi / 2, curve: math.Pi / 2}) {
		t.Fatalf("shape %+v", shape)
	}
	empty := newLaneShape([]Point{{X: 1}, {X: 1}})
	if !empty.empty {
		t.Fatalf("shape %+v of a lane with no length", empty)
	}
	north := laneShape{start: math.Pi / 2, end: math.Pi / 2}
	if got := (turnSum{}).add(shape).add(empty).add(north).turn; got != math.Pi/2 {
		t.Fatalf("turn %v, want %v", got, math.Pi/2)
	}
}

// TestPlatoonGeometry runs a queue of pods in platoons of 4 through paths
// that turn. The queue backs up from the berth at the end of the path, so
// pods stop close together on the turns. checkTraffic checks the distance
// between each two pods at each tick, and the monitor checks the owners
// and the run of each link at each tick.
func TestPlatoonGeometry(t *testing.T) {
	t.Parallel()
	skipLong(t)
	corner := Point{X: 30}
	tests := []struct {
		name  string
		lanes []geometryLane
		// maxTurn is the largest turn of a link.
		maxTurn float64
		// close is true when pods on the path come closer than Clearance
		// also without platoons. The test then checks the distance only
		// between each follower and the pods ahead that it shares with.
		close bool
		// berth and exit are the offsets of the destination station from
		// the end of the path, or zero for those of geometryNetwork.
		berth, exit Point
	}{
		// A lane is at least 24 m long, so the two legs of the hairpin
		// are 24 m apart.
		{name: "hairpin", lanes: []geometryLane{
			{to: Point{}}, {to: Point{Y: 24}}, {to: Point{X: -24, Y: 24}}, {to: Point{X: -24, Y: 100}},
		}, maxTurn: math.Pi / 2},
		{name: "opposite turns", lanes: []geometryLane{
			{to: Point{}}, {to: Point{Y: 24}}, {to: Point{X: 70, Y: 24}},
		}, maxTurn: math.Pi / 2},
		{name: "curved lane", lanes: []geometryLane{
			{to: Point{}}, {to: Point{X: 30, Y: 30}, control: &corner}, {to: Point{X: 30, Y: 90}},
		}, maxTurn: math.Pi / 2},
		// The return lane is 12.5 m from the first lane, so pods on the
		// two lanes are apart without platoons. A platoon of 4 folds
		// back through three turns of 90 degrees.
		{name: "fold back", lanes: []geometryLane{
			{to: Point{}}, {to: Point{Y: 36.5}}, {to: Point{X: -24, Y: 36.5}}, {to: Point{X: -24, Y: 12.5}}, {to: Point{X: -124, Y: 12.5}},
		}, maxTurn: math.Pi / 2},
		// Each lane is at least 24 m long, but the end of the return lane
		// is 5 m from the first lane, and the last lane leaves the fold
		// from there. Pods on the first lane and on the lanes after the
		// fold come closer than Clearance also without platoons. No run
		// can hold the two turns of 90 degrees before the return lane, so
		// a follower does not share with a pod after the fold.
		{name: "close fold", lanes: []geometryLane{
			{to: Point{}}, {to: Point{Y: 29}}, {to: Point{X: -24, Y: 29}}, {to: Point{X: -24, Y: 5}}, {to: Point{X: -84, Y: 17}},
		}, maxTurn: math.Pi / 2, close: true, berth: Point{X: -60, Y: 12}, exit: Point{X: -120}},
		// The sharp turn is more than the largest turn of a run, so each
		// link drains before it.
		{name: "sharp turn", lanes: []geometryLane{
			{to: Point{}}, {to: Point{X: -60 * math.Cos(math.Pi/6), Y: 60 * math.Sin(math.Pi/6)}},
		}, maxTurn: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			const pods = 10
			network := geometryNetwork(Point{X: -500}, test.lanes, pods)
			if test.exit != (Point{}) {
				network = stationNetwork(Point{X: -500}, test.lanes, pods, test.berth, test.exit)
			}
			s := restoreGeometry(t, network, len(test.lanes), pods, 450, corridorQueueGap)
			if err := s.SetPlatooning(PlatooningVirtual); err != nil {
				t.Fatal(err)
			}
			monitor := newPlatoonMonitor(s)
			ancestors := 0
			for range 150 * TicksPerSecond {
				s.Step()
				if !test.close {
					checkTraffic(t, s.Snapshot())
				}
				ancestors += checkSharing(t, s)
				monitor.check(t)
			}
			t.Logf("linked %d, largest platoon %d, largest turn %.4f, ticks of pods that share with a pod two or more places ahead %d",
				len(monitor.linked), monitor.largest, monitor.largestTurn, ancestors)
			if monitor.largest < 3 || monitor.largestTurn > test.maxTurn+1e-6 {
				t.Fatalf("largest platoon %d and largest turn %v, want at least 3 pods and a turn of at most %v",
					monitor.largest, monitor.largestTurn, test.maxTurn)
			}
		})
	}
}

// checkSharing checks that each follower is at least Clearance from each pod
// ahead of it in its platoon, up to the last pod ahead that owns a
// resource that the follower holds. It returns the number of such pods
// that are two or more places ahead of their followers.
func checkSharing(t *testing.T, s *Simulation) int {
	t.Helper()
	count := 0
	for index := range s.vehicles {
		v := &s.vehicles[index]
		owners := make(map[string]bool)
		for r := range v.routeReleases {
			if owner := s.owners[r]; owner != podResourceOwner(v.Pod.ID) {
				owners[owner.podID()] = true
			}
		}
		var chain []*vehicle
		last := -1
		for ahead := v.link.leader; ahead != 0; ahead = s.vehicles[ahead-1].link.leader {
			chain = append(chain, &s.vehicles[ahead-1])
			if owners[s.vehicles[ahead-1].Pod.ID] {
				last = len(chain) - 1
			}
		}
		for place, ahead := range chain[:last+1] {
			if gap := math.Hypot(v.Pod.Position.X-ahead.Pod.Position.X, v.Pod.Position.Y-ahead.Pod.Position.Y); gap < Clearance-1e-6 {
				t.Fatalf("tick %d: pod %s is %.5f m from pod %s, which is %d places ahead and owns a resource that it holds",
					s.tick, v.Pod.ID, gap, ahead.Pod.ID, place+1)
			}
			if place > 0 {
				count++
			}
		}
	}
	return count
}

// TestPlatoonNoDrainRoom plans a link for two pods 13 m apart on a short
// lane before a sharp turn, so the run can hold only the short lane. A
// lane of 24 m has two cells of 12 m, and no cell ends Clearance and the
// drain slack before the end of the lane. Thus the link has no block to
// share, and planLink rejects it. A lane of 90 m has three cells of 30 m,
// and the follower can share the second cell.
func TestPlatoonNoDrainRoom(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		length float64
		link   bool
	}{{length: 24}, {length: 90, link: true}} {
		t.Run(fmt.Sprint(test.length), func(t *testing.T) {
			t.Parallel()
			turn := Point{X: test.length - 60*math.Cos(math.Pi/6), Y: 60 * math.Sin(math.Pi/6)}
			beyond := Point{X: turn.X - 100*math.Cos(math.Pi/6), Y: turn.Y + 100*math.Sin(math.Pi/6)}
			lanes := []geometryLane{{to: Point{}}, {to: Point{X: test.length}}, {to: turn}, {to: beyond}}
			s := restoreGeometry(t, geometryNetwork(Point{X: -500}, lanes, 2), len(lanes), 2, 450, corridorQueueGap)
			// Put the pods at rest on the short lane.
			leader, v := &s.vehicles[0], &s.vehicles[1]
			first := v.blocks.laneFirst(1)
			start := v.blocks.lanes[1].start
			leader.distance, v.distance = start+14, start+1
			leader.blockIndex, v.blockIndex = first, first
			for v.blocks.at(leader.blockIndex).end < leader.distance {
				leader.blockIndex++
			}
			leader.reservedThrough, v.reservedThrough = leader.blockIndex, first
			link, ok := s.planLink(linkPlan{v: v, leader: leader, lane: 1, leaderLane: 1, turn: platoonMaxTurn})
			if ok != test.link || link.lanes > 1 {
				t.Fatalf("link %+v, planned %v, want %v", link, ok, test.link)
			}
			if _, end := linkEnds(&v.blocks, platoonLink{lane: 1, lanes: 1}); (end >= first) != test.link {
				t.Fatalf("end block %d, first block of the run %d", end, first)
			}
		})
	}
}
