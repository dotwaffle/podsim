package sim

import (
	"fmt"
	"maps"
	"math"
	"reflect"
	"slices"
	"testing"
)

// referenceRouteBlocks is routeBlocks before the berth resource index. For
// each block, it scans each berth of each station.
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
				laneEnd := b.end - b.laneStart
				laneStart := b.start - b.laneStart
				if laneStart < conflict.end && conflict.start < laneEnd {
					b.resources = append(b.resources, resource{kind: junctionResource, id: conflict.junction})
				}
			}
			b.resources = append(b.resources, resource{kind: trackResource, id: lane.ID, cell: cell})
			blocks = append(blocks, b)
		}
		distance += length
	}
	return blocks, lengths
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
		if !reflect.DeepEqual(gotBlocks, wantBlocks) || !reflect.DeepEqual(gotLengths, wantLengths) {
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
	literal := &Simulation{network: Example()}
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
		build func([]Lane) ([]block, []float64)
	}{
		{name: "new", build: s.routeBlocks},
		{name: "reference", build: s.referenceRouteBlocks},
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
			if want := laneBlockCount(s.laneLength(lane)); got[index] != want || len(blocks) != want {
				t.Errorf("%s: lane %q has %d blocks, want %d and routeBlocks gives %d", name, lane.ID, got[index], want, len(blocks))
			}
		}
	}
}
