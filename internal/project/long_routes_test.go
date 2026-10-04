package project

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

// longRoutes returns a project in which 200 pods at four stations share one
// path of about 33,000 blocks to a fifth station. Each lane of the path goes
// from one side of the coordinate square to the other.
func longRoutes() Config {
	const (
		stations = 4
		berths   = 50
	)
	config := Config{Version: CurrentVersion, Name: "Long routes", Demand: DemandConfig{PerMinute: 1, Pattern: "balanced", Seed: 1}}
	network := &config.Network
	node := func(id string, x, y float64) {
		network.Nodes = append(network.Nodes, sim.Node{ID: id, Position: sim.Point{X: x, Y: y}})
	}
	lane := func(id, from, to string, station string, role sim.StationLaneRole) {
		network.Lanes = append(network.Lanes, sim.Lane{ID: id, From: from, To: to, SpeedLimit: 14, StationID: station, StationRole: role})
	}
	for index := range stations + 1 {
		id := fmt.Sprintf("s%d", index)
		count := berths
		if index == stations {
			id, count = "far", 4
		}
		x := 1_000 * float64(index)
		node(id+"-entry", x, 0)
		node(id+"-exit", x+400, 0)
		station := sim.Station{ID: id, Name: "Station " + id, Entry: id + "-entry", Exit: id + "-exit"}
		for berth := range count {
			berthNode := fmt.Sprintf("%s-berth-%d", id, berth)
			node(berthNode, x+200, 100+30*float64(berth))
			lane(berthNode+"-in", id+"-entry", berthNode, id, sim.StationBerthAccessRole)
			lane(berthNode+"-out", berthNode, id+"-exit", id, sim.StationDepartureRole)
			station.Berths = append(station.Berths, sim.Berth{ID: berthNode, Node: berthNode})
		}
		lane(id+"-through", id+"-entry", id+"-exit", id, sim.StationThroughRole)
		network.Stations = append(network.Stations, station)
	}
	for index := range stations - 1 {
		lane(fmt.Sprintf("link-%d", index), fmt.Sprintf("s%d-exit", index), fmt.Sprintf("s%d-entry", index+1), "", "")
	}
	// The long path goes from s3 to the far station.
	corners := []sim.Point{{X: 90_000, Y: 90_000}, {X: -90_000, Y: -85_000}, {X: 90_000, Y: 80_000}, {X: -90_000, Y: -75_000}}
	previous := "s3-exit"
	for index, corner := range corners {
		id := fmt.Sprintf("corner-%d", index)
		node(id, corner.X, corner.Y)
		lane("long-"+id, previous, id, "", "")
		previous = id
	}
	lane("long-far", previous, "far-entry", "", "")
	lane("return", "far-exit", "s0-entry", "", "")
	for index := range stations * berths {
		config.Fleet = append(config.Fleet, sim.Placement{
			ID: fmt.Sprintf("%03d", index), StationID: fmt.Sprintf("s%d", index/berths), BerthID: fmt.Sprintf("s%d-berth-%d", index/berths, index%berths),
		})
	}
	return config
}

// TestLongRoutesShareLaneBlocks sends the 200 pods of longRoutes to the far
// station and runs the fleet for one minute. The routes share the blocks of
// each lane, so the fleet keeps memory for the blocks of the network and
// for the lanes of each route, and not for the blocks of each route. When
// each route held its own blocks, the fleet kept more than 1.5 GB. It does
// not run in parallel, because it measures the heap.
func TestLongRoutesShareLaneBlocks(t *testing.T) {
	config := longRoutes()
	if err := Validate(config); err != nil {
		t.Fatal(err)
	}
	blocks := 0
	laneBlocks := make(map[string]int, len(config.Network.Lanes))
	for index, count := range config.Network.LaneBlocks() {
		blocks += count
		laneBlocks[config.Network.Lanes[index].ID] = count
	}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	fleet, err := sim.NewFleet(config.Network, config.Fleet)
	if err != nil {
		t.Fatal(err)
	}
	for index := range config.Fleet {
		if err := fleet.RequestTrip(config.Fleet[index].StationID, "far"); err != nil {
			t.Fatal(err)
		}
	}
	for range 60 * sim.TicksPerSecond {
		fleet.Step()
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	routeBlocks := 0
	for _, v := range fleet.Snapshot().Vehicles {
		if len(v.Route) == 0 {
			t.Fatalf("pod %s has no route", v.Pod.ID)
		}
		for _, lane := range v.Route {
			routeBlocks += laneBlocks[lane.ID]
		}
	}
	retained := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	// A block has at most six resources of 32 bytes each. The bound
	// allows 1,024 bytes for each block of the network, for the blocks
	// and for the other memory of the fleet.
	bound := int64(blocks) * 1024
	t.Logf("%d pods on routes of %d blocks in a network of %d blocks retain %d bytes, bound %d", len(config.Fleet), routeBlocks, blocks, retained, bound)
	if retained > bound {
		t.Fatalf("the fleet retains %d bytes, more than %d", retained, bound)
	}
	runtime.KeepAlive(fleet)
}
