package sim

import (
	"fmt"
	"maps"
	"math"
	"math/rand/v2"
	"reflect"
	"slices"
	"testing"
)

// referenceRouteBlocks is routeBlocks before the berth resource index. For
// each block, it scans each berth of each station. It keeps one copy of
// each junction resource of a block, as routeBlocks does.
//
// The reference compares the conflicts of a lane with the cell offsets
// from the lane start, as newLaneCells does. Before the routes shared the
// cells, the offset was (d+x)-d for a lane start at route distance d, and
// the result could differ from x in the last bit. At an exact conflict
// boundary, a cell then got a junction resource only on some routes (see
// TestJunctionOverlapUsesLaneOffsets).
func (s *Simulation) referenceRouteBlocks(route []Lane) ([]block, []float64) {
	var blocks []block
	var lengths []float64
	if len(route) > 0 {
		lengths = make([]float64, 0, len(route))
	}
	distance := 0.0
	for _, lane := range route {
		length := s.laneLength(lane)
		lengths = append(lengths, length)
		geometry := s.geometry[lane.ID]
		count := laneBlockCount(length)
		for cell := range count {
			b := block{lane: lane, geometry: geometry, cell: cell, start: distance + float64(cell)*length/float64(count), end: distance + float64(cell+1)*length/float64(count), laneStart: distance, last: cell == count-1}
			for _, station := range s.network.Stations {
				for _, berth := range station.Berths {
					if b.last && lane.To == berth.Node {
						b.resources = append(b.resources, resource{kind: berthResource, id: berth.ID})
					}
				}
			}
			if cell == 0 {
				b.resources = append(b.resources, resource{kind: nodeResource, id: lane.From})
			}
			if b.last {
				b.resources = append(b.resources, resource{kind: nodeResource, id: lane.To})
			}
			for _, conflict := range s.junctionConflicts[lane.ID] {
				laneStart := float64(cell) * length / float64(count)
				laneEnd := float64(cell+1) * length / float64(count)
				junction := resource{kind: junctionResource, id: conflict.junction}
				if laneStart < conflict.end && conflict.start < laneEnd && !slices.Contains(b.resources, junction) {
					b.resources = append(b.resources, junction)
				}
			}
			b.resources = append(b.resources, resource{kind: trackResource, id: lane.ID, cell: cell})
			blocks = append(blocks, b)
		}
		distance += length
	}
	return blocks, lengths
}

// all returns the blocks of the list.
func (l *blockList) all() []block {
	var blocks []block
	for _, b := range l.span(0, l.len()) {
		blocks = append(blocks, b)
	}
	return blocks
}

// blockListOf returns a list of the given blocks. A lane of the list ends
// at each block that has last set, before a block of another lane ID, and
// at the end of the blocks. The lane, the geometry and the lane start of a
// lane come from its first block, and its length from its last block. The
// list divides each lane into cells of equal length, so the start and the
// end of a block can differ from the given block.
func blockListOf(blocks []block) blockList {
	var list blockList
	for from := 0; from < len(blocks); {
		to := from + 1
		for to < len(blocks) && !blocks[to-1].last && blocks[to].lane.ID == blocks[from].lane.ID {
			to++
		}
		head := blocks[from]
		cells := &laneCells{geometry: head.geometry}
		for _, b := range blocks[from:to] {
			cells.add(b.resources)
		}
		list.route = append(list.route, head.lane)
		list.lanes = append(list.lanes, routeLaneCells{
			first: from, start: head.laneStart, length: blocks[to-1].end - head.laneStart, cells: cells, geometry: cells.geometry,
		})
		from = to
	}
	list.lanes = append(list.lanes, routeLaneCells{first: len(blocks)})
	list.blocks = len(blocks)
	return list
}

// checkRouteBlocks fails t when routeBlocks and the reference give
// different blocks or lengths. It checks a route of each lane. It also
// checks the routes from the first berth of each station to the first berth
// of the next station, and from the first berth of the first station to the
// first berth of each station.
func checkRouteBlocks(t *testing.T, s *Simulation) {
	t.Helper()
	check := func(route []Lane) {
		t.Helper()
		gotBlocks, gotLengths := s.routeBlocks(route)
		wantBlocks, wantLengths := s.referenceRouteBlocks(route)
		if !reflect.DeepEqual(gotBlocks.all(), wantBlocks) || !reflect.DeepEqual(gotLengths, wantLengths) {
			t.Fatalf("routeBlocks differs from the reference for route %v", route)
		}
	}
	check(nil)
	for _, lane := range s.network.Lanes {
		check([]Lane{lane})
	}
	stations := s.network.Stations
	for index, station := range stations {
		for _, pair := range [][2]Station{{station, stations[(index+1)%len(stations)]}, {stations[0], station}} {
			if pair[0].ID == pair[1].ID {
				continue
			}
			route, err := s.route(pair[0].Berths[0].Node, pair[1].Berths[0].Node)
			if err != nil {
				continue
			}
			check(route)
		}
	}
}

// circleRing returns a one-way ring of stations on a circle. Each station
// has berths berths. The network has stations*(2+berths) nodes.
func circleRing(stations, berths int) Network {
	var network Network
	radius := 200 * float64(stations) / math.Pi
	at := func(angle, distance float64) Point {
		return Point{X: distance * math.Cos(angle), Y: distance * math.Sin(angle)}
	}
	for index := range stations {
		id := fmt.Sprintf("s%03d", index)
		angle := 2 * math.Pi * float64(index) / float64(stations)
		step := 2 * math.Pi / float64(stations)
		entry, exit := id+"-entry", id+"-exit"
		network.Nodes = append(network.Nodes,
			Node{ID: entry, Position: at(angle, radius)},
			Node{ID: exit, Position: at(angle+step/2, radius)},
		)
		network.Lanes = append(network.Lanes,
			Lane{ID: id + "-through", From: entry, To: exit, SpeedLimit: 14, StationID: id, StationRole: StationThroughRole},
			Lane{ID: id + "-next", From: exit, To: fmt.Sprintf("s%03d-entry", (index+1)%stations), SpeedLimit: 14},
		)
		station := Station{ID: id, Name: "Station " + id, Entry: entry, Exit: exit}
		for berth := range berths {
			node := fmt.Sprintf("%s-berth-%02d", id, berth)
			network.Nodes = append(network.Nodes, Node{ID: node, Position: at(angle+step/4, radius+60+30*float64(berth))})
			network.Lanes = append(network.Lanes,
				Lane{ID: node + "-in", From: entry, To: node, SpeedLimit: 14, StationID: id, StationRole: StationBerthAccessRole},
				Lane{ID: node + "-out", From: node, To: exit, SpeedLimit: 14, StationID: id, StationRole: StationDepartureRole},
			)
			station.Berths = append(station.Berths, Berth{ID: fmt.Sprintf("%s-%02d", id, berth+1), Node: node})
		}
		network.Stations = append(network.Stations, station)
	}
	return network
}

func TestRouteBlocksMatchReference(t *testing.T) {
	t.Parallel()
	skipLong(t)
	networks := map[string]Network{
		"example":       Example(),
		"curved":        curvedExample(),
		"ladder":        ladderNetwork(),
		"cycle":         cycleNetwork(40),
		"circle 40x6":   circleRing(40, 6),
		"circle 100x18": circleRing(100, 18),
	}
	// The presets and the rings that package scenarios writes.
	maps.Copy(networks, scenarioNetworks(t))
	for _, name := range slices.Sorted(maps.Keys(networks)) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s, err := New(networks[name], networks[name].Stations[0].ID)
			if err != nil {
				t.Fatal(err)
			}
			checkRouteBlocks(t, s)
		})
	}
	t.Run("changed network", func(t *testing.T) {
		t.Parallel()
		s := newTraffic(t)
		addMarketBerth(s)
		checkRouteBlocks(t, s)
	})
}

func TestNetworkIndexesRebuildBerthResources(t *testing.T) {
	t.Parallel()
	s := newTraffic(t)
	if want := indexBerthResources(s.network); !reflect.DeepEqual(s.berthResources, want) {
		t.Fatalf("NewFleet index = %v, want %v", s.berthResources, want)
	}
	addMarketBerth(s)
	want := []resource{{kind: berthResource, id: "market-2"}}
	if got := s.berthResources["market-berth-2"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("index after the new berth = %v, want %v", got, want)
	}
	literal := &Simulation{networkIndexes: &networkIndexes{network: Example()}}
	literal.ensureNetworkIndexes()
	if want := indexBerthResources(literal.network); len(want) == 0 || !reflect.DeepEqual(literal.berthResources, want) {
		t.Fatalf("literal index = %v, want %v", literal.berthResources, want)
	}
}

// BenchmarkRouteBlocks makes the blocks of a route half way round a ring
// of 200 stations with 18 berths each: 4,000 nodes and 7,600 lanes. The
// setup takes some seconds, so run it with -benchtime=10x or similar.
func BenchmarkRouteBlocks(b *testing.B) {
	s, err := New(circleRing(200, 18), "s000")
	if err != nil {
		b.Fatal(err)
	}
	route, err := s.route("s000-berth-00", "s100-berth-17")
	if err != nil {
		b.Fatal(err)
	}
	for _, bench := range []struct {
		name  string
		build func([]Lane)
	}{
		{name: "new", build: func(route []Lane) { s.routeBlocks(route) }},
		{name: "reference", build: func(route []Lane) { s.referenceRouteBlocks(route) }},
	} {
		b.Run(bench.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				bench.build(route)
			}
		})
	}
}

// TestLaneBlocksMatchRouteBlocks checks that Network.LaneBlocks gives the
// blocks that routeBlocks and the restore budget use for each lane of a
// fleet.
func TestLaneBlocksMatchRouteBlocks(t *testing.T) {
	t.Parallel()
	networks := scenarioNetworks(t)
	networks["example"] = Example()
	for name, network := range networks {
		s, err := NewFleet(network, []Placement{{ID: "01", StationID: network.Stations[0].ID}})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got := network.LaneBlocks()
		for index, lane := range s.network.Lanes {
			blocks, _ := s.routeBlocks([]Lane{lane})
			if want := laneBlockCount(s.laneLength(lane)); got[index] != want || blocks.len() != want {
				t.Errorf("%s: lane %q has %d blocks, want %d and routeBlocks gives %d", name, lane.ID, got[index], want, blocks.len())
			}
		}
	}
}

// referenceReservationEnd is reservationEnd before it kept the junction
// runs. It scans the run of each junction resource from each block.
func referenceReservationEnd(blocks []block, start int) int {
	through := start
	for i := start; i <= through; i++ {
		if blocks[i].last && i+1 < len(blocks) {
			through = max(through, i+1)
		}
		for _, r := range blocks[i].resources {
			if r.kind != junctionResource {
				continue
			}
			for j := i + 1; j < len(blocks) && slices.Contains(blocks[j].resources, r); j++ {
				through = max(through, j)
			}
		}
	}
	return through
}

// TestReservationEndMatchesReference compares reservationEnd with the
// reference from each block of random block lists. The lists have few
// junctions, so that the runs of a junction are long, overlap other runs,
// and start again after a gap.
func TestReservationEndMatchesReference(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewPCG(5, 8))
	for range 500 {
		blocks := make([]block, 1+rng.IntN(60))
		for index := range blocks {
			b := &blocks[index]
			b.last = rng.IntN(4) == 0
			for junction := range 3 {
				if rng.IntN(3) > 0 {
					b.resources = append(b.resources, resource{kind: junctionResource, id: fmt.Sprintf("j%d", junction)})
				}
			}
			b.resources = append(b.resources, resource{kind: trackResource, id: "lane", cell: index})
		}
		list := blockListOf(blocks)
		blocks = list.all()
		for start := range blocks {
			if got, want := reservationEnd(&list, start), referenceReservationEnd(blocks, start); got != want {
				t.Fatalf("reservationEnd from block %d = %d, want %d", start, got, want)
			}
		}
	}
}

// checkSharedCells fails t when the route of a pod does not use the shared
// cells of each of its lanes.
func checkSharedCells(t *testing.T, s *Simulation) {
	t.Helper()
	for _, v := range s.vehicles {
		if !slices.Equal(v.blocks.route, v.Route) {
			t.Fatalf("tick %d: the blocks of pod %s are not for its route", s.tick, v.Pod.ID)
		}
		for index, lane := range v.blocks.route {
			if cells := s.laneCells[lane.ID]; cells == nil || v.blocks.lanes[index].cells != cells {
				t.Fatalf("tick %d: pod %s does not use the shared cells of lane %s", s.tick, v.Pod.ID, lane.ID)
			}
		}
	}
}

// TestRoutesShareLaneCells runs the demo with guarded positioning, so that
// pods board, relocate and change their routes. At each tick, each pod
// route must use the shared cells of its lanes. A restore of the state at
// each 100 ticks must also use them.
func TestRoutesShareLaneCells(t *testing.T) {
	t.Parallel()
	s := newTraffic(t)
	if err := s.SetPositioning(PositioningGuarded); err != nil {
		t.Fatal(err)
	}
	if err := s.StartDemo(); err != nil {
		t.Fatal(err)
	}
	for s.demo != nil {
		if s.tick >= demoTickLimit {
			t.Fatalf("the demo did not end in %d ticks", demoTickLimit)
		}
		s.Step()
		checkSharedCells(t, s)
		if s.tick%100 != 0 {
			continue
		}
		restored, result, err := RestoreState(RestoreStateInput{Network: Example(), Fleet: demoFleet(), State: s.ExportState()})
		if err != nil || !cleanRestore(result) {
			t.Fatalf("tick %d: %v, %+v", s.tick, err, result)
		}
		checkSharedCells(t, restored)
	}
}

// TestJunctionOverlapUsesLaneOffsets checks a conflict that ends at the
// exact start of a cell. The lanes go straight from (-24.3, 0) to (0, 0)
// and then to (26, 0). The out lane has two cells of 13 m, and its conflict
// with the in lane ends at 13 m. The second cell of the out lane starts at
// 13 m, so it does not hold the junction resource. The reservation from the
// first block then ends at the first cell of the out lane. Before the routes
// shared the cells, the second cell started at (24.3+13)-24.3, which is
// less than 13, so it held the resource and the reservation went one cell
// further.
func TestJunctionOverlapUsesLaneOffsets(t *testing.T) {
	t.Parallel()
	s := &Simulation{networkIndexes: &networkIndexes{network: Network{
		Nodes: []Node{{ID: "a", Position: Point{X: -24.3}}, {ID: "b"}, {ID: "c", Position: Point{X: 26}}},
		Lanes: []Lane{{ID: "in", From: "a", To: "b", SpeedLimit: 14}, {ID: "out", From: "b", To: "c", SpeedLimit: 14}},
	}}}
	s.ensureNetworkIndexes()
	conflicts := s.junctionConflicts["out"]
	if len(conflicts) != 1 || conflicts[0].end != 13 {
		t.Fatalf("conflicts of the out lane = %+v, want one that ends at 13", conflicts)
	}
	junction := resource{kind: junctionResource, id: conflicts[0].junction}
	blocks, _ := s.routeBlocks(s.network.Lanes)
	if start := blocks.at(3).start; start-blocks.at(3).laneStart >= 13 {
		t.Fatalf("the second cell of the out lane starts at %v, which does not test the old offset", start)
	}
	want := [][]resource{
		{{kind: nodeResource, id: "a"}, junction, {kind: trackResource, id: "in", cell: 0}},
		{{kind: nodeResource, id: "b"}, junction, {kind: trackResource, id: "in", cell: 1}},
		{{kind: nodeResource, id: "b"}, junction, {kind: trackResource, id: "out", cell: 0}},
		{{kind: nodeResource, id: "c"}, {kind: trackResource, id: "out", cell: 1}},
	}
	all := blocks.all()
	if len(all) != len(want) {
		t.Fatalf("the route has %d blocks, want %d", len(all), len(want))
	}
	for index, b := range all {
		if !slices.Equal(b.resources, want[index]) {
			t.Errorf("block %d has %v, want %v", index, b.resources, want[index])
		}
	}
	if got := reservationEnd(&blocks, 0); got != 2 {
		t.Errorf("the reservation from block 0 ends at block %d, want 2", got)
	}
}
