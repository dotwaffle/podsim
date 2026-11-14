package ride

import (
	"encoding/json/v2"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dotwaffle/podsim/internal/sim"
)

var updateLevels = flag.Bool("update-levels", false, "rewrite testdata/levels.json")

// levelNetwork has a mainline from m1 to m2. Station lanes go from m1 down
// to s2, and a short exit joins s2 to m2. The island lane has no path to the
// mainline.
func levelNetwork() sim.Network {
	return sim.Network{
		Nodes: []sim.Node{
			{ID: "m1", Position: sim.Point{X: 0, Y: 0}},
			{ID: "m2", Position: sim.Point{X: 1000, Y: 0}},
			{ID: "s1", Position: sim.Point{X: 0, Y: 60}},
			{ID: "s2", Position: sim.Point{X: 0, Y: 260}},
			{ID: "s3", Position: sim.Point{X: 0, Y: 600}},
			{ID: "spur", Position: sim.Point{X: 30, Y: 60}},
			{ID: "i1", Position: sim.Point{X: 5000, Y: 0}},
			{ID: "i2", Position: sim.Point{X: 5100, Y: 0}},
		},
		Lanes: []sim.Lane{
			{ID: "main", From: "m1", To: "m2", SpeedLimit: 20},
			{ID: "in", From: "m1", To: "s1", SpeedLimit: 10, StationID: "st"},
			{ID: "platform", From: "s1", To: "s2", SpeedLimit: 10, StationID: "st"},
			{ID: "far", From: "s2", To: "s3", SpeedLimit: 10, StationID: "st"},
			// The spur node is two lanes from the mainline: 60 m and 30 m.
			{ID: "spur", From: "s1", To: "spur", SpeedLimit: 10, StationID: "st"},
			// The exit points to the mainline. A distance uses lanes in either
			// direction, so s2 is 50 m from m2, not 260 m from m1.
			{ID: "exit", From: "s2", To: "m2", SpeedLimit: 10, StationID: "st"},
			{ID: "island", From: "i1", To: "i2", SpeedLimit: 10, StationID: "depot"},
		},
		Stations: []sim.Station{
			{ID: "st", Name: "Station", Berths: []sim.Berth{{ID: "b1", Node: "m1"}, {ID: "b2", Node: "s2"}}},
			{ID: "depot", ParkingOnly: true, Berths: []sim.Berth{{ID: "d1", Node: "i2"}}},
			{ID: "empty"},
		},
	}
}

func TestLevelHeight(t *testing.T) {
	tests := []struct {
		distance, want float64
	}{
		{-10, 5}, {0, 5}, {30, 4.375}, {60, 3}, {90, 1.625}, {120, 1}, {500, 1},
	}
	for _, test := range tests {
		if got := levels.height(test.distance); !near(got, test.want) {
			t.Errorf("height(%v) = %v, want %v", test.distance, got, test.want)
		}
	}
}

func TestNodeDistances(t *testing.T) {
	network := levelNetwork()
	network.Nodes[1].Position = sim.Point{X: 0, Y: 310}
	got := nodeDistances(network, newLaneTable(network), 120)
	want := map[string]float64{"m1": 0, "m2": 0, "s1": 60, "spur": 90, "s2": 50, "s3": 120, "i1": 120, "i2": 120}
	if len(got) != len(want) {
		t.Fatalf("distances = %v", got)
	}
	for id, distance := range want {
		if !near(got[id], distance) {
			t.Errorf("distance of %s = %v, want %v", id, got[id], distance)
		}
	}
}

func TestPodHeight(t *testing.T) {
	network := levelNetwork()
	network.Nodes[1].Position = sim.Point{X: 0, Y: 310}
	state := pausedState(1)
	state.Network = network
	var bridge Bridge
	bridge.Observe(state, time.Unix(0, 0))
	tests := []struct {
		name string
		pod  sim.Pod
		want float64
	}{
		{"half way down the entry", sim.Pod{LaneID: "in", LaneDistance: 30}, levels.height(30)},
		{"past the lane end", sim.Pod{LaneID: "in", LaneDistance: 90}, levels.height(60)},
		{"before the lane start", sim.Pod{LaneID: "in", LaneDistance: -5}, 5},
		{"on the mainline", sim.Pod{LaneID: "main", LaneDistance: 100}, 5},
		{"at a station berth", sim.Pod{BerthID: "b2"}, levels.height(50)},
		{"at a mainline berth", sim.Pod{BerthID: "b1"}, 5},
		{"at a depot berth", sim.Pod{BerthID: "d1"}, 1},
		{"nowhere", sim.Pod{}, 5},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := bridge.podHeight(test.pod); !near(got, test.want) {
				t.Fatalf("height = %v, want %v", got, test.want)
			}
		})
	}
}

func TestTopologyLevels(t *testing.T) {
	network := levelNetwork()
	network.Nodes[1].Position = sim.Point{X: 0, Y: 310}
	state := pausedState(1)
	state.Network = network
	topology := NewTopology(state)
	if topology.Levels != levels {
		t.Fatalf("levels = %+v", topology.Levels)
	}
	lanes := make(map[string]TopologyLane)
	for _, lane := range topology.Lanes {
		lanes[lane.ID] = lane
	}
	if lane := lanes["platform"]; !near(lane.FromDistance, 60) || !near(lane.ToDistance, 50) {
		t.Fatalf("platform lane = %+v", lane)
	}
	if lane := lanes["main"]; lane.FromDistance != 0 || lane.ToDistance != 0 {
		t.Fatalf("main lane = %+v", lane)
	}
	wantHeights := []float64{(5 + levels.height(50)) / 2, 1, 5}
	for i, station := range topology.Stations {
		if !near(station.Height, wantHeights[i]) {
			t.Fatalf("height of %s = %v, want %v", station.ID, station.Height, wantHeights[i])
		}
	}
}

// levelFixture holds the level values and heights at sample distances. The
// node tests of web/ride.js read the same file, so the two versions of the
// height curve cannot differ.
type levelFixture struct {
	Levels Levels      `json:"levels"`
	Cases  []levelCase `json:"cases"`
}

type levelCase struct {
	Distance float64 `json:"distance"`
	Height   float64 `json:"height"`
}

func TestLevelFixture(t *testing.T) {
	want := levelFixture{Levels: levels}
	for _, distance := range []float64{-10, 0, 7.5, 30, 60, 90, 119, 120, 400} {
		want.Cases = append(want.Cases, levelCase{distance, levels.height(distance)})
	}
	path := filepath.Join("testdata", "levels.json")
	if *updateLevels {
		data, err := json.Marshal(want, json.Deterministic(true))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the fixture, or run go test -run TestLevelFixture -update-levels: %v", err)
	}
	var got levelFixture
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Levels != want.Levels || len(got.Cases) != len(want.Cases) {
		t.Fatalf("fixture = %+v, want %+v; run go test -run TestLevelFixture -update-levels", got, want)
	}
	for i := range want.Cases {
		if got.Cases[i].Distance != want.Cases[i].Distance || !near(got.Cases[i].Height, want.Cases[i].Height) {
			t.Fatalf("case %d = %+v, want %+v; run go test -run TestLevelFixture -update-levels", i, got.Cases[i], want.Cases[i])
		}
	}
}
