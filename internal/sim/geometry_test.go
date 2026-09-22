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
