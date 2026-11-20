package sim

import "math"

// This file holds the entries that the tail gates of internal/scenarios read.
// They run with the presets, which this package cannot import.

// LaneTailBounds holds the bounds of one lane of a tail index.
type LaneTailBounds struct {
	Tail, FromTail, Front float64
}

// LaneTailsForTest returns the bounds of each lane of the tail index of a
// network.
func LaneTailsForTest(network Network) map[string]LaneTailBounds {
	tails := indexGeometryTails(network)
	bounds := make(map[string]LaneTailBounds, len(tails))
	for id, tail := range tails {
		bounds[id] = LaneTailBounds{Tail: tail.tail, FromTail: tail.fromTail, Front: tail.front}
	}
	return bounds
}

// TailWalkBudgetForTest is the number of distance evaluations of one lane.
const TailWalkBudgetForTest = tailWalkBudget

// TailWalkEvaluationsForTest returns the distance evaluations that the walk
// of a polyline needs with no budget.
func TailWalkEvaluationsForTest(points []Point) int {
	walk := newTailWalk(points)
	_, used, _ := walk.scan(math.MaxInt)
	return used
}

// CellJunctionsForTest returns the junctions that the cell at a distance on a
// lane holds.
func (p *PreparedNetwork) CellJunctionsForTest(laneID string, distance, length float64) []string {
	cells := p.laneCells[laneID]
	count := cells.count()
	cell := min(count-1, int(distance*float64(count)/length))
	var junctions []string
	for _, held := range cells.cell(cell) {
		if held.kind == junctionResource {
			junctions = append(junctions, held.id)
		}
	}
	return junctions
}

// MergeClearanceForTest returns the side-by-side distance of the lateral
// merge or split pair of two lanes, and false when the lanes form no such
// pair.
func (p *PreparedNetwork) MergeClearanceForTest(first, second string) (float64, bool) {
	a, okA := p.graph.lanes[first]
	b, okB := p.graph.lanes[second]
	if !okA || !okB {
		return 0, false
	}
	return p.merges.clearance(laneKey(a), laneKey(b))
}
