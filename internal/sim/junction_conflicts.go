package sim

import "math"

const conflictSampleStep = 0.5

// buildJunctionConflicts uses the movement polylines to bound conflicts between
// incident lanes. Unconnected crossings remain outside junction control.
func buildJunctionConflicts(network Network) map[string][]laneConflict {
	conflicts := make(map[string][]laneConflict)
	points := make(map[string][]Point, len(network.Lanes))
	incident := make(map[string][]Lane)
	for _, lane := range network.Lanes {
		points[lane.ID] = network.lanePoints(lane, make([]Point, 0, 65))
		incident[lane.From] = append(incident[lane.From], lane)
		incident[lane.To] = append(incident[lane.To], lane)
	}
	for _, node := range network.Nodes {
		for _, lane := range incident[node.ID] {
			for _, other := range incident[node.ID] {
				if lane.ID == other.ID {
					continue
				}
				start, end := conflictExtent(points[lane.ID], points[other.ID])
				if !math.IsInf(start, 1) {
					conflicts[lane.ID] = append(conflicts[lane.ID], laneConflict{junction: node.ID, start: start, end: end})
				}
			}
		}
	}
	return conflicts
}

func conflictExtent(points, other []Point) (float64, float64) {
	start, end, distance := math.Inf(1), math.Inf(-1), 0.0
	for i := 1; i < len(points); i++ {
		a, b := points[i-1], points[i]
		length := pointDistance(a, b)
		for offset := 0.0; ; {
			fraction := 0.0
			if length > 0 {
				fraction = offset / length
			}
			point := Point{X: a.X + fraction*(b.X-a.X), Y: a.Y + fraction*(b.Y-a.Y)}
			separation := pointToPolylineDistance(point, other)
			// Distance to a polyline changes by at most the distance traveled.
			// Pad both the threshold and extent so samples cannot miss a conflict.
			if separation <= Clearance+conflictSampleStep {
				at := distance + offset
				start, end = min(start, at), max(end, at)
			}
			if offset == length {
				break
			}
			// Skip only distances that cannot reach the padded conflict threshold.
			step := max(conflictSampleStep, separation-Clearance-conflictSampleStep)
			offset = min(length, offset+step)
		}
		distance += length
	}
	return max(0, start-conflictSampleStep), min(distance, end+conflictSampleStep)
}

func pointToPolylineDistance(point Point, points []Point) float64 {
	best := math.Inf(1)
	for i := 1; i < len(points); i++ {
		start, end := points[i-1], points[i]
		dx, dy := end.X-start.X, end.Y-start.Y
		lengthSquared := dx*dx + dy*dy
		fraction := 0.0
		if lengthSquared > 0 {
			fraction = max(0, min(1, ((point.X-start.X)*dx+(point.Y-start.Y)*dy)/lengthSquared))
		}
		closest := Point{X: start.X + fraction*dx, Y: start.Y + fraction*dy}
		best = min(best, pointDistance(point, closest))
	}
	return best
}
