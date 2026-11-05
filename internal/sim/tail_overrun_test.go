package sim

import (
	"math"
	"reflect"
	"testing"
)

// TestLaneTailOverruns uses a large lane from a to b. Other lanes touch its
// nodes at their From or To node, or touch neither. The curves are U-turns
// with a chord under 20 m, so a tail exceeds their length.
func TestLaneTailOverruns(t *testing.T) {
	t.Parallel()
	large := largeGeometryClasses(t, "legacy", "group")
	network := Network{
		Nodes: []Node{
			{ID: "a", Position: Point{X: 0, Y: 0}},
			{ID: "b", Position: Point{X: 100, Y: 0}},
			{ID: "c", Position: Point{X: 100, Y: 20}},
			{ID: "d", Position: Point{X: 0, Y: 15}},
			{ID: "e", Position: Point{X: 110, Y: 0}},
			{ID: "f", Position: Point{X: -10, Y: 0}},
			{ID: "g", Position: Point{X: 300, Y: 15}},
		},
		Lanes: []Lane{
			{ID: "large-straight", From: "a", To: "b", VehicleClasses: large},
			{ID: "exact-fit", From: "b", To: "c"},
			{ID: "to-only-short", From: "d", To: "a"},
			{ID: "curve-from", From: "b", To: "e", Control: &Point{X: 105, Y: 400}},
			{ID: "curve-to", From: "f", To: "a", Control: &Point{X: -5, Y: 400}},
			{ID: "reverses", From: "a", To: "c", Control: &Point{X: -300, Y: -300}},
			{ID: "far-curve", From: "f", To: "g", Control: &Point{X: 400, Y: 400}},
		},
	}
	tails := indexGeometryTails(network)
	lanes := make(map[string]Lane, len(network.Lanes))
	for _, lane := range network.Lanes {
		lanes[lane.ID] = lane
	}
	var ids []string
	for _, overrun := range network.LaneTailOverruns() {
		ids = append(ids, overrun.LaneID)
		if want := tails[overrun.LaneID].tail; overrun.Tail != want {
			t.Errorf("lane %s has tail %g, want %g", overrun.LaneID, overrun.Tail, want)
		}
		if want := network.Length(lanes[overrun.LaneID]); overrun.Length != want {
			t.Errorf("lane %s has length %g, want %g", overrun.LaneID, overrun.Length, want)
		}
	}
	// exact-fit has a tail of 20 m on 20 m. to-only-short has a tail of 20 m
	// on 15 m and no From tail. The large class does not reach far-curve,
	// and reverses has an infinite tail that validation refuses.
	want := []string{"to-only-short", "curve-from", "curve-to"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("overruns = %v, want %v", ids, want)
	}
	if bounds := tails["to-only-short"]; bounds.fromTail != 0 || bounds.tail != largeClearance {
		t.Fatalf("to-only-short bounds = %+v", bounds)
	}
	if bounds := tails["far-curve"]; bounds.tail != 0 {
		t.Fatalf("far-curve bounds = %+v", bounds)
	}
	if !math.IsInf(tails["reverses"].tail, 1) {
		t.Fatalf("reverses bounds = %+v", tails["reverses"])
	}
}

func TestLaneTailOverrunsNoLargeClass(t *testing.T) {
	t.Parallel()
	network := Network{
		Nodes: []Node{{ID: "a"}, {ID: "b", Position: Point{X: 100}}},
		Lanes: []Lane{{ID: "curve", From: "a", To: "b", Control: &Point{X: 0, Y: 400}}},
	}
	if got := network.LaneTailOverruns(); got != nil {
		t.Fatalf("overruns = %+v", got)
	}
}
