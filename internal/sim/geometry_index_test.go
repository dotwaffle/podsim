package sim

import (
	"math"
	"reflect"
	"testing"
)

// referenceLanePoints keeps the direct lookups and interpolation before
// endpoint indexing, so the comparison also pins the float arithmetic.
func referenceLanePoints(n Network, lane Lane, points []Point) []Point {
	a, _ := n.Node(lane.From)
	b, _ := n.Node(lane.To)
	if lane.Control == nil {
		return append(points, a.Position, b.Position)
	}
	points = points[:65]
	for i := range points {
		t := float64(i) / 64
		u := 1 - t
		points[i] = Point{X: u*u*a.Position.X + 2*u*t*lane.Control.X + t*t*b.Position.X, Y: u*u*a.Position.Y + 2*u*t*lane.Control.Y + t*t*b.Position.Y}
	}
	return points
}

func referenceLaneGeometry(n Network) map[string]*laneGeometry {
	storage := make([]laneGeometry, len(n.Lanes))
	geometry := make(map[string]*laneGeometry, len(n.Lanes))
	for laneIndex, lane := range n.Lanes {
		points := referenceLanePoints(n, lane, make([]Point, 0, 65))
		indexed := laneGeometry{end: points[len(points)-1]}
		for i := 1; i < len(points); i++ {
			length := pointDistance(points[i-1], points[i])
			if length == 0 {
				continue
			}
			start := 0.0
			if len(indexed.segments) > 0 {
				start = indexed.segments[len(indexed.segments)-1].end
			}
			indexed.segments = append(indexed.segments, laneSegment{from: points[i-1], to: points[i], start: start, end: start + length})
		}
		storage[laneIndex] = indexed
		geometry[lane.ID] = &storage[laneIndex]
	}
	return geometry
}

func TestIndexedLanePointsMatchDirectLookups(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"normal", "duplicate node", "missing endpoint"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			n := polylineNetwork()
			switch name {
			case "duplicate node":
				n.Nodes = append(n.Nodes, Node{ID: "a", Position: Point{X: 900, Y: 700}})
			case "missing endpoint":
				n.Lanes[0].To = "missing"
				n.Lanes[1].From = "missing"
			}
			positions := indexNodePositions(n.Nodes)
			for _, lane := range n.Lanes {
				got := lanePointsBetween(lane, positions[lane.From], positions[lane.To], make([]Point, 0, 65))
				want := referenceLanePoints(n, lane, make([]Point, 0, 65))
				if len(got) != len(want) {
					t.Fatalf("lane %s: %d points, want %d", lane.ID, len(got), len(want))
				}
				for i := range got {
					if math.Float64bits(got[i].X) != math.Float64bits(want[i].X) || math.Float64bits(got[i].Y) != math.Float64bits(want[i].Y) {
						t.Fatalf("lane %s point %d: %+v, want exact bits of %+v", lane.ID, i, got[i], want[i])
					}
				}
			}
			if got, want := buildLaneGeometry(n), referenceLaneGeometry(n); !reflect.DeepEqual(got, want) {
				t.Fatal("indexed lane geometry differs from direct lookups")
			}
			checkJunctionConflictsMatchReference(t, n)
		})
	}
}

func TestGeometryOperationsUseCurrentNodes(t *testing.T) {
	t.Parallel()
	n := polylineNetwork()
	before := buildLaneGeometry(n)
	_ = buildJunctionConflicts(n)
	original := before["straight"].segments[0].from
	n.Nodes[0].Position = Point{X: 17, Y: -31}
	if got, want := buildLaneGeometry(n), referenceLaneGeometry(n); !reflect.DeepEqual(got, want) {
		t.Fatal("rebuilt geometry retained old node positions")
	}
	if before["straight"].segments[0].from != original {
		t.Fatal("network edit changed earlier geometry")
	}
	checkJunctionConflictsMatchReference(t, n)

	large := largeGeometryOrdinaryStation(t)
	for i := range large.Lanes {
		large.Lanes[i].VehicleClasses = largeGeometryClasses(t, "legacy", "group")
	}
	large.Lanes[1].Control = &Point{X: -55, Y: 60}
	previous := indexGeometryTails(large)
	large.Nodes[2].Position.Y += 13
	tails := indexGeometryTails(large)
	changed := false
	for _, lane := range large.Lanes {
		want := largePathTail(referenceLanePoints(large, lane, make([]Point, 0, 65)))
		if tails[lane.ID].tail != want {
			t.Fatalf("lane %s tail = %v, want %v", lane.ID, tails[lane.ID].tail, want)
		}
		changed = changed || tails[lane.ID] != previous[lane.ID]
	}
	if !changed {
		t.Fatal("the node edit did not change any geometry tail")
	}
}

func TestBankGeometryRefreshesNodePositions(t *testing.T) {
	t.Parallel()
	n := Network{
		Nodes: []Node{{ID: "a"}, {ID: "b", Position: Point{X: 100}}, {ID: "c", Position: Point{Y: 25}}, {ID: "d", Position: Point{X: 100, Y: 25}}},
		Lanes: []Lane{{ID: "first", From: "a", To: "b", SpeedLimit: 14, VehicleClasses: largeGeometryClasses(t, "group")}, {ID: "second", From: "c", To: "d", SpeedLimit: 14}},
	}
	if err := n.ValidateBankGeometry(); err != nil {
		t.Fatal(err)
	}
	n.Nodes[2].Position.Y, n.Nodes[3].Position.Y = 15, 15
	const refusal = `nonincident paths "first" and "second" are less than 20 meters apart`
	if err := n.ValidateBankGeometry(); err == nil || err.Error() != refusal {
		t.Fatalf("edited geometry = %v, want %s", err, refusal)
	}
	n.Nodes[2].Position.Y, n.Nodes[3].Position.Y = 25, 25
	if err := n.ValidateBankGeometry(); err != nil {
		t.Fatalf("corrected geometry retained a stale refusal: %v", err)
	}
	unknown := largeGeometryOrdinaryStation(t)
	unknown.Stations[0].Berths[0].Node = "missing"
	if err := unknown.ValidateBankGeometry(); err == nil || err.Error() != `berth "berth" has an unknown node` {
		t.Fatalf("unknown berth node = %v", err)
	}
}

func BenchmarkLaneGeometryNodeLookups(b *testing.B) {
	n := gridNetwork(40)
	for _, test := range []struct {
		name string
		run  func(Network) map[string]*laneGeometry
	}{
		{"indexed", buildLaneGeometry},
		{"direct", referenceLaneGeometry},
	} {
		b.Run(test.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = test.run(n)
			}
		})
	}
}
