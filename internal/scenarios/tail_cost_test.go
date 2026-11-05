package scenarios_test

import (
	"fmt"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// worstTailNetwork returns a network at the lane and node limits of the
// project format. Each pair of nodes has 3 or 4 curved lanes of the given
// shape, and the pairs are 1 km apart.
func worstTailNetwork(tb testing.TB, end, control sim.Point) sim.Network {
	tb.Helper()
	classes, err := sim.NewClassSet("legacy", "compact", "group", "express")
	if err != nil {
		tb.Fatal(err)
	}
	var network sim.Network
	const pairs, columns, spacing = project.MaxNodes / 2, 80, 1000.0
	for pair := range pairs {
		origin := sim.Point{X: spacing * float64(pair%columns-columns/2), Y: spacing * float64(pair/columns-columns/2)}
		from, to := fmt.Sprintf("n%d-a", pair), fmt.Sprintf("n%d-b", pair)
		network.Nodes = append(network.Nodes, sim.Node{ID: from, Position: origin}, sim.Node{ID: to, Position: sim.Point{X: origin.X + end.X, Y: origin.Y + end.Y}})
	}
	for index := range project.MaxLanes {
		pair := index % pairs
		origin := network.Nodes[2*pair].Position
		variant := float64(index/pairs) * 0.5
		network.Lanes = append(network.Lanes, sim.Lane{
			ID: fmt.Sprintf("l%d", index), From: network.Nodes[2*pair].ID, To: network.Nodes[2*pair+1].ID, SpeedLimit: 14, VehicleClasses: classes,
			Control: &sim.Point{X: origin.X + control.X + variant, Y: origin.Y + control.Y + variant},
		})
	}
	return network
}

// worstShape is the lane under 90 m with the most walk evaluations that a
// search found, so 20,000 of these lanes stay inside the block limit.
var worstShape = struct{ end, control sim.Point }{sim.Point{X: 31.6465, Y: -0.013}, sim.Point{X: 11.2776, Y: 64.2719}}

// TestWorstTailProjectLimits checks that the network of the benchmarks is
// inside the count limits of the project format, and that it prepares.
func TestWorstTailProjectLimits(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("long test")
	}
	network := worstTailNetwork(t, worstShape.end, worstShape.control)
	blocks, junctions := 0, 0
	for _, count := range network.LaneBlocks() {
		blocks += count
	}
	for _, count := range network.JunctionPairs() {
		junctions += count
	}
	if len(network.Lanes) != project.MaxLanes || len(network.Nodes) != project.MaxNodes || blocks > project.MaxNetworkBlocks || junctions > project.MaxJunctionPairs {
		t.Fatalf("%d lanes, %d nodes, %d blocks, %d junction pairs", len(network.Lanes), len(network.Nodes), blocks, junctions)
	}
	if _, err := sim.PrepareNetwork(network); err != nil {
		t.Fatal(err)
	}
}

// BenchmarkTailIndexWorstProject measures one tail index of the network at
// the limits.
func BenchmarkTailIndexWorstProject(b *testing.B) {
	network := worstTailNetwork(b, worstShape.end, worstShape.control)
	for b.Loop() {
		sim.LaneTailsForTest(network)
	}
}

// BenchmarkPrepareWorstProject measures the preparation of the network at the
// limits.
func BenchmarkPrepareWorstProject(b *testing.B) {
	network := worstTailNetwork(b, worstShape.end, worstShape.control)
	for b.Loop() {
		if _, err := sim.PrepareNetwork(network); err != nil {
			b.Fatal(err)
		}
	}
}

// worstPairsNetwork returns a network with the most lanes and nodes, and more
// than 2.5 times the junction pairs of worstTailNetwork. Each pair of nodes has
// 7 or 8 lanes of worstShape. The nodes that no lane uses come first, so a
// search of the node list passes them.
func worstPairsNetwork(tb testing.TB) sim.Network {
	tb.Helper()
	classes, err := sim.NewClassSet("legacy", "compact", "group", "express")
	if err != nil {
		tb.Fatal(err)
	}
	var network sim.Network
	const pairs, columns, spacing = project.MaxLanes / 7, 80, 1000.0
	unused := project.MaxNodes - 2*pairs
	for index := range unused {
		network.Nodes = append(network.Nodes, sim.Node{ID: fmt.Sprintf("u%d", index), Position: sim.Point{X: 500 + float64(index%100)*10, Y: 500 + float64(index/100)*10}})
	}
	for pair := range pairs {
		origin := sim.Point{X: spacing * float64(pair%columns-columns/2), Y: spacing * float64(pair/columns-columns/2)}
		network.Nodes = append(network.Nodes,
			sim.Node{ID: fmt.Sprintf("n%d-a", pair), Position: origin},
			sim.Node{ID: fmt.Sprintf("n%d-b", pair), Position: sim.Point{X: origin.X + worstShape.end.X, Y: origin.Y + worstShape.end.Y}})
	}
	for index := range project.MaxLanes {
		pair := index % pairs
		from, to := network.Nodes[unused+2*pair], network.Nodes[unused+2*pair+1]
		variant := float64(index/pairs) * 0.5
		network.Lanes = append(network.Lanes, sim.Lane{
			ID: fmt.Sprintf("l%d", index), From: from.ID, To: to.ID, SpeedLimit: 14, VehicleClasses: classes,
			Control: &sim.Point{X: from.Position.X + worstShape.control.X + variant, Y: from.Position.Y + worstShape.control.Y + variant},
		})
	}
	return network
}

// TestWorstPairsProjectLimits checks that the network of
// BenchmarkPrepareWorstPairs is inside the count limits of the project format.
// It counts and does not prepare, so it runs with -short.
func TestWorstPairsProjectLimits(t *testing.T) {
	t.Parallel()
	network := worstPairsNetwork(t)
	blocks, junctions := 0, 0
	for _, count := range network.LaneBlocks() {
		blocks += count
	}
	for _, count := range network.JunctionPairs() {
		junctions += count
	}
	if len(network.Lanes) != project.MaxLanes || len(network.Nodes) != project.MaxNodes || blocks > project.MaxNetworkBlocks || junctions != 240_016 || junctions > project.MaxJunctionPairs {
		t.Fatalf("%d lanes, %d nodes, %d blocks, %d junction pairs", len(network.Lanes), len(network.Nodes), blocks, junctions)
	}
}

// BenchmarkPrepareWorstPairs measures the preparation of a hand-built network
// near the junction pair limit. The junction conflicts lead this preparation,
// and BenchmarkPrepareWorstProject has too few pairs to show their worst case.
func BenchmarkPrepareWorstPairs(b *testing.B) {
	network := worstPairsNetwork(b)
	for b.Loop() {
		if _, err := sim.PrepareNetwork(network); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkTailIndexBudgetCeiling measures 20,000 lanes that each use the
// whole budget. The block limit refuses such a project, so this is the ceiling
// of the work of one index and not a project that validates.
func BenchmarkTailIndexBudgetCeiling(b *testing.B) {
	network := worstTailNetwork(b, sim.Point{X: 228.2874, Y: -0.1606}, sim.Point{X: 158.4429, Y: 98983.9033})
	for index := range network.Nodes {
		network.Nodes[index].Position = sim.Point{X: 0, Y: 0}
		if index%2 == 1 {
			network.Nodes[index].Position = sim.Point{X: 228.2874, Y: -0.1606}
		}
	}
	for index := range network.Lanes {
		network.Lanes[index].Control = &sim.Point{X: 158.4429, Y: 98983.9033}
	}
	for b.Loop() {
		sim.LaneTailsForTest(network)
	}
}

// BenchmarkTailLaneCoordinateLimit measures one lane at the coordinate limit.
func BenchmarkTailLaneCoordinateLimit(b *testing.B) {
	network := coordinateNetwork(b, sim.Point{X: -50, Y: -project.MaxCoordinate}, sim.Point{X: 50, Y: -project.MaxCoordinate}, sim.Point{Y: project.MaxCoordinate})
	for b.Loop() {
		sim.LaneTailsForTest(network)
	}
}
