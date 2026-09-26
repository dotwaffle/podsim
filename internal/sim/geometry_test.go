package sim

import (
	"math"
	"testing"
)

func TestCurvedLaneGeometry(t *testing.T) {
	n := Network{Nodes: []Node{{ID: "a", Position: Point{}}, {ID: "b", Position: Point{X: 100}}}}
	lane := Lane{ID: "curve", From: "a", To: "b", SpeedLimit: 14, Control: &Point{X: 50, Y: 100}}
	length := n.Length(lane)
	if math.Abs(length-147.8943) > 0.02 {
		t.Fatalf("arc length %v", length)
	}
	for _, tc := range []struct {
		distance float64
		want     Point
	}{{-1, Point{}}, {length / 2, Point{X: 50, Y: 50}}, {length + 1, Point{X: 100}}} {
		if got := n.Position(lane, tc.distance); pointDistance(got, tc.want) > 0.001 {
			t.Fatalf("distance %v: got %+v want %+v", tc.distance, got, tc.want)
		}
	}
	n.Lanes = []Lane{lane}
	cloned := n.clone()
	cloned.Lanes[0].Control.Y = 200
	if n.Lanes[0].Control.Y != 100 {
		t.Fatal("curve control aliases clone")
	}
}

func TestCachedLaneGeometryMatchesNetwork(t *testing.T) {
	t.Parallel()
	network := Example()
	network.Lanes[0].Control = &Point{X: 150, Y: 100}
	simulation, err := New(network, "harbor")
	if err != nil {
		t.Fatal(err)
	}
	for _, lane := range network.Lanes {
		length := network.Length(lane)
		for _, distance := range []float64{-1, 0, length / 3, length, length + 1} {
			want := network.Position(lane, distance)
			got := simulation.position(lane, distance)
			if pointDistance(got, want) > 1e-9 {
				t.Fatalf("lane %s distance %v: got %+v want %+v", lane.ID, distance, got, want)
			}
		}
	}
}

func TestCurveRejectsNonfiniteControl(t *testing.T) {
	n := Example()
	n.Lanes[0].Control = &Point{X: math.NaN()}
	if _, err := New(n, "harbor"); err == nil {
		t.Fatal("accepted nonfinite control")
	}
}

// curvedExample returns the example network with two curved lanes. Pods
// from harbor to market use both of them.
func curvedExample() Network {
	network := Example()
	for index, lane := range network.Lanes {
		switch lane.ID {
		case "approach-branch":
			network.Lanes[index].Control = &Point{X: 250, Y: 200}
		case "bypass-in":
			network.Lanes[index].Control = &Point{X: 400, Y: 330}
		}
	}
	return network
}

// laneDistances returns distances along a lane of a length that include each
// segment end, the lane ends, and points outside the lane.
func laneDistances(geometry *laneGeometry, length float64) []float64 {
	distances := []float64{-1, 0, length / 3, length, length + 1}
	for _, segment := range geometry.segments {
		distances = append(distances, segment.start, (segment.start+segment.end)/2, segment.end)
	}
	return distances
}

func TestBlockPositionMatchesPosition(t *testing.T) {
	t.Parallel()
	s, err := New(curvedExample(), "harbor")
	if err != nil {
		t.Fatal(err)
	}
	for _, lane := range s.network.Lanes {
		geometry := s.geometry[lane.ID]
		blocks, _ := s.routeBlocks([]Lane{lane})
		// A block without geometry uses the lookup by lane ID.
		blocks = append(blocks, block{lane: lane})
		for index := range blocks {
			b := &blocks[index]
			if b.geometry != nil && b.geometry != geometry {
				t.Fatalf("lane %s block %d: the block has the geometry of another lane", lane.ID, index)
			}
			for _, distance := range laneDistances(geometry, s.laneLength(lane)) {
				if got, want := s.blockPosition(b, distance), s.position(lane, distance); got != want {
					t.Fatalf("lane %s block %d distance %v: got %+v, want %+v", lane.ID, index, distance, got, want)
				}
			}
		}
		if blocks[0].geometry == nil {
			t.Fatalf("lane %s: routeBlocks did not keep the lane geometry", lane.ID)
		}
	}
}

// TestMovingPodPositionMatchesLaneGeometry checks that move puts a pod at
// the point that position gives for its lane and lane distance.
func TestMovingPodPositionMatchesLaneGeometry(t *testing.T) {
	t.Parallel()
	s, err := New(curvedExample(), "harbor")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RequestJourney("01", "market"); err != nil {
		t.Fatal(err)
	}
	lanes := make(map[string]Lane)
	for _, lane := range s.network.Lanes {
		lanes[lane.ID] = lane
	}
	curved := make(map[string]bool)
	for range 300 * TicksPerSecond {
		s.Step()
		v := &s.vehicles[0]
		for index := range v.blocks {
			if b := &v.blocks[index]; b.geometry != s.geometry[b.lane.ID] {
				t.Fatalf("tick %d: block %d of lane %s has the wrong geometry", s.tick, index, b.lane.ID)
			}
		}
		if v.Pod.Activity != Traveling || v.Pod.LaneID == "" {
			continue
		}
		lane := lanes[v.Pod.LaneID]
		if got, want := v.Pod.Position, s.position(lane, v.Pod.LaneDistance); got != want {
			t.Fatalf("tick %d lane %s distance %v: position %+v, want %+v", s.tick, lane.ID, v.Pod.LaneDistance, got, want)
		}
		if lane.Control != nil {
			curved[lane.ID] = true
		}
	}
	if len(curved) != 2 {
		t.Fatalf("the pod traveled on the curved lanes %v, want both", curved)
	}
}
