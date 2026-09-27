package sim

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"math"
	"os"
	"reflect"
	"testing"
)

// lanePolylinePath holds the lane polylines of polylineNetwork. The editor
// test web/editor_test.cjs reads the file and compares its own lane
// polylines with it, so that the editor clearance check follows the path
// of a pod.
const lanePolylinePath = "testdata/lane_polylines.json"

// lanePolylineFile is the form of the file at lanePolylinePath. Points
// holds the polyline of each lane, in lane order.
type lanePolylineFile struct {
	Network Network   `json:"network"`
	Points  [][]Point `json:"points"`
}

// polylineNetwork has a straight lane and curved lanes with a small, a
// large, and an off-center curve.
func polylineNetwork() Network {
	return Network{
		Nodes: []Node{
			{ID: "a", Position: Point{X: 0, Y: 0}},
			{ID: "b", Position: Point{X: 120, Y: 0}},
			{ID: "c", Position: Point{X: -37.5, Y: 211.25}},
			{ID: "d", Position: Point{X: 1043.7, Y: -612.9}},
		},
		Lanes: []Lane{
			{ID: "straight", From: "a", To: "b", SpeedLimit: 12},
			{ID: "shallow", From: "a", To: "b", SpeedLimit: 12, Control: &Point{X: 60, Y: 9}},
			{ID: "tight", From: "b", To: "c", SpeedLimit: 12, Control: &Point{X: 140.25, Y: 260.5}},
			{ID: "long", From: "c", To: "d", SpeedLimit: 14, Control: &Point{X: 811.3, Y: 402.7}},
		},
		Stations: []Station{},
	}
}

// TestLanePolylineGolden fixes the lane polylines that the editor test
// compares with. Run the test with -update to write the file again.
func TestLanePolylineGolden(t *testing.T) {
	t.Parallel()
	network := polylineNetwork()
	if *update {
		written := lanePolylineFile{Network: network}
		for _, lane := range network.Lanes {
			written.Points = append(written.Points, network.lanePoints(lane, make([]Point, 0, 65)))
		}
		data, err := json.Marshal(written, json.Deterministic(true), jsontext.WithIndent("  "))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(lanePolylinePath, append(data, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(lanePolylinePath)
	if err != nil {
		t.Fatal(err)
	}
	var file lanePolylineFile
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(file.Network, network) {
		t.Fatalf("the golden network differs from polylineNetwork: %+v", file.Network)
	}
	if len(file.Points) != len(network.Lanes) {
		t.Fatalf("the golden file has %d polylines, want %d", len(file.Points), len(network.Lanes))
	}
	for index, lane := range network.Lanes {
		got := network.lanePoints(lane, make([]Point, 0, 65))
		want := file.Points[index]
		if len(got) != len(want) {
			t.Fatalf("lane %s has %d points, want %d", lane.ID, len(got), len(want))
		}
		for point := range got {
			if math.Abs(got[point].X-want[point].X) > 1e-9 || math.Abs(got[point].Y-want[point].Y) > 1e-9 {
				t.Fatalf("lane %s point %d is %+v, want %+v", lane.ID, point, got[point], want[point])
			}
		}
	}
}
