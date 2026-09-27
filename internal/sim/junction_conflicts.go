package sim

import "math"

const (
	conflictSampleStep = 0.5
	// conflictChunk is the number of segments that share one bounding box.
	conflictChunk = 8
	// conflictRun is the shortest distance in meters that conflictExtent tries
	// to clear in one check.
	conflictRun = 4.0
	// conflictWindow is the number of segments on each side of the nearest
	// segment that separation measures at each point.
	conflictWindow = 2
	// conflictHysteresis is the distance in meters beyond the nearest
	// segment within which a scan measures each segment.
	conflictHysteresis = 16.0
	// conflictSlack widens the bounds that skip distance calculations. It is
	// much larger than the rounding error of the distance arithmetic, so the
	// bounds do not change a result.
	conflictSlack = 1e-9
	// conflictScaleLimit keeps the squared bounds finite. Larger coordinates
	// use the full scan.
	conflictScaleLimit = 1e100
)

// buildJunctionConflicts uses the movement polylines to bound conflicts between
// incident lanes. Unconnected crossings remain outside junction control.
func buildJunctionConflicts(network Network) map[string][]laneConflict {
	conflicts := make(map[string][]laneConflict)
	polylines := make(map[string]*conflictPolyline, len(network.Lanes))
	for _, lane := range network.Lanes {
		polylines[lane.ID] = newConflictPolyline(network.lanePoints(lane, make([]Point, 0, 65)))
	}
	forEachJunctionPair(network, func(node string, lane, other Lane) {
		start, end := conflictExtent(polylines[lane.ID], polylines[other.ID])
		if !math.IsInf(start, 1) {
			conflicts[lane.ID] = append(conflicts[lane.ID], laneConflict{junction: node, start: start, end: end})
		}
	})
	return conflicts
}

// JunctionPairs returns, for each node in node order, the number of lane
// pairs that the simulation compares at the node when it builds the
// junction conflicts. Each comparison measures the paths of two lanes, so
// the total sets most of the time that NewFleet uses on a large network.
// It counts the lanes and does not compare them.
func (n Network) JunctionPairs() []int {
	incident := incidentLanes(n)
	pairs := make([]int, len(n.Nodes))
	for index, node := range n.Nodes {
		lanes := incident[node.ID]
		// forEachJunctionPair gives each ordered pair of entries, less the
		// pairs of an entry with an entry of the same lane. Only a loop
		// lane has two entries, and each of them has one entry of the same
		// lane.
		pairs[index] = len(lanes) * (len(lanes) - 1)
		for _, lane := range lanes {
			if lane.From == lane.To {
				pairs[index]--
			}
		}
	}
	return pairs
}

// forEachJunctionPair calls compare for each ordered pair of lanes at each
// node, in node order. A lane is at its From node and at its To node, so a
// loop lane has two entries at its node. compare does not get a lane
// paired with the same lane.
func forEachJunctionPair(network Network, compare func(node string, lane, other Lane)) {
	incident := incidentLanes(network)
	for _, node := range network.Nodes {
		for _, lane := range incident[node.ID] {
			for _, other := range incident[node.ID] {
				if lane.ID != other.ID {
					compare(node.ID, lane, other)
				}
			}
		}
	}
}

// incidentLanes returns the lanes at each node, in lane order. A loop lane
// is two times in the lanes of its node.
func incidentLanes(network Network) map[string][]Lane {
	incident := make(map[string][]Lane)
	for _, lane := range network.Lanes {
		incident[lane.From] = append(incident[lane.From], lane)
		incident[lane.To] = append(incident[lane.To], lane)
	}
	return incident
}

func conflictExtent(lane, other *conflictPolyline) (float64, float64) {
	const limit = Clearance + conflictSampleStep
	points := lane.points
	scale := max(lane.scale, other.scale)
	margin := conflictSlack * (1 + scale)
	start, end, distance := math.Inf(1), math.Inf(-1), 0.0
	run := conflictRun
	for i := 1; i < len(points); i++ {
		a, b := points[i-1], points[i]
		length := pointDistance(a, b)
		for offset := 0.0; ; {
			separation, segment := other.separation(lanePoint(a, b, length, offset), limit, scale)
			// Distance to a polyline changes by at most the distance traveled.
			// Pad both the threshold and extent so samples cannot miss a conflict.
			if separation <= limit {
				at := distance + offset
				start, end = min(start, at), max(end, at)
				if segment >= 0 && separation <= limit-margin && offset < length {
					// Skip the samples of a run that is within the limit.
					// at grows with the offset, so the last sample of the
					// run sets end.
					var cleared float64
					cleared, run = other.clearRun(a, b, length, offset, run, segment, limit-margin)
					offset = lastSample(offset, cleared, length)
					end = max(end, distance+offset)
				}
			}
			if offset == length {
				break
			}
			// Skip only distances that cannot reach the padded conflict threshold.
			// Each separation within the limit gives the minimum step.
			step := max(conflictSampleStep, separation-Clearance-conflictSampleStep)
			offset = min(length, offset+step)
		}
		distance += length
	}
	return max(0, start-conflictSampleStep), min(distance, end+conflictSampleStep)
}

// lastSample returns the last offset that is not more than cleared in the
// sequence of sample offsets from offset. Each offset in the sequence is
// min(length, previous+conflictSampleStep), as in conflictExtent.
func lastSample(offset, cleared, length float64) float64 {
	for {
		next := min(length, offset+conflictSampleStep)
		if offset == length || next > cleared {
			return offset
		}
		offset = next
		if offset < conflictSampleStep || offset >= 1<<52 {
			continue
		}
		// Below the next power of two, each addition of the step is exact,
		// so a jump of n steps gives the same offset as n additions.
		_, exponent := math.Frexp(offset)
		steps := min(math.Ceil((math.Ldexp(1, exponent)-offset)/conflictSampleStep)-1, math.Floor((cleared-offset)/conflictSampleStep))
		for steps > 0 && offset+steps*conflictSampleStep > cleared {
			steps--
		}
		if steps > 0 {
			offset += steps * conflictSampleStep
		}
	}
}

// lanePoint returns the sample point at offset along the segment from a to b
// of the given length.
func lanePoint(a, b Point, length, offset float64) Point {
	fraction := 0.0
	if length > 0 {
		fraction = offset / length
	}
	return Point{X: a.X + fraction*(b.X-a.X), Y: a.Y + fraction*(b.Y-a.Y)}
}

// conflictPolyline is a lane polyline with the bounding boxes of its segments
// and of each chunk of conflictChunk segments.
type conflictPolyline struct {
	points   []Point
	segments []conflictBox
	chunks   []conflictBox
	// scale is the largest absolute coordinate, or NaN for a non-finite one.
	scale float64
	// nearest is the segment nearest to the last point that separation
	// measured. Each segment outside the window from low to high is at
	// least clear from anchor. These fields change only the work that
	// separation does, not its result.
	nearest, low, high int
	anchor             Point
	clear              float64
	// bounds holds the bound for each segment in the window.
	bounds []float64
	// lows, gaps, far and measured hold the work of a scan.
	lows     []float64
	gaps     []float64
	far      float64
	measured []int
}

type conflictBox struct {
	minX, minY, maxX, maxY float64
}

func newConflictPolyline(points []Point) *conflictPolyline {
	polyline := &conflictPolyline{points: points}
	for _, point := range points {
		if !finite(point.X) || !finite(point.Y) {
			polyline.scale = math.NaN()
			break
		}
		polyline.scale = max(polyline.scale, math.Abs(point.X), math.Abs(point.Y))
	}
	for i := 1; i < len(points); i++ {
		a, b := points[i-1], points[i]
		box := conflictBox{minX: min(a.X, b.X), minY: min(a.Y, b.Y), maxX: max(a.X, b.X), maxY: max(a.Y, b.Y)}
		polyline.segments = append(polyline.segments, box)
		if (i-1)%conflictChunk == 0 {
			polyline.chunks = append(polyline.chunks, box)
			continue
		}
		chunk := &polyline.chunks[len(polyline.chunks)-1]
		chunk.minX, chunk.minY = min(chunk.minX, box.minX), min(chunk.minY, box.minY)
		chunk.maxX, chunk.maxY = max(chunk.maxX, box.maxX), max(chunk.maxY, box.maxY)
	}
	polyline.lows = make([]float64, len(polyline.segments))
	polyline.gaps = make([]float64, len(polyline.chunks))
	return polyline
}

// separation returns pointToPolylineDistance(point, polyline.points) when that
// distance is more than limit. Otherwise it returns a value that is not more
// than limit. It also returns the segment that gives the value, or -1 when it
// scans every segment. scale is the largest absolute coordinate of the point
// and the polyline.
func (polyline *conflictPolyline) separation(point Point, limit, scale float64) (float64, int) {
	points := polyline.points
	if len(points) < 2 || !(scale <= conflictScaleLimit) {
		return pointToPolylineDistance(point, points), -1
	}
	margin := conflictSlack * (1 + scale)
	nearest := polyline.nearest
	best := polyline.segmentDistance(point, nearest)
	// The minimum over all segments is at most this segment's distance.
	if best <= limit {
		return best, nearest
	}
	// The distance to each segment outside the window is at least clear
	// minus the distance that the point moved from anchor. The same holds
	// for each segment in the window with its own bound.
	moved := (math.Abs(point.X-polyline.anchor.X) + math.Abs(point.Y-polyline.anchor.Y)) * (1 + conflictSlack)
	if !(best < polyline.clear-moved-margin) {
		return polyline.scan(point, best, limit, margin)
	}
	for segment := polyline.low; segment <= polyline.high; segment++ {
		if segment == polyline.nearest || best < polyline.bounds[segment-polyline.low]-moved-margin {
			continue
		}
		start, end := points[segment], points[segment+1]
		if segmentPointSquared(point, start, end) > best*best*(1+conflictSlack) {
			continue
		}
		if distance := segmentPointDistance(point, start, end); distance < best {
			best, nearest = distance, segment
		}
	}
	polyline.nearest = nearest
	return best, nearest
}

// clearRun returns an offset up to which each sample on the lane segment
// from a to b is within bound of the given segment of the polyline, and the
// run length for the next call. The sample at offset must be within bound.
// The distance from a point that moves along a line to a segment is convex,
// so the samples between two ends that are within bound are within bound.
// The bound is below the limit by enough to cover the rounding of the
// sample points.
func (polyline *conflictPolyline) clearRun(a, b Point, length, offset, run float64, segment int, bound float64) (float64, float64) {
	target := min(length, offset+run)
	if polyline.segmentDistance(lanePoint(a, b, length, target), segment) <= bound {
		return target, 2 * run
	}
	return offset, max(conflictRun, run/2)
}

// scan finds the segment nearest to point. best is the distance to the
// segment that was nearest before. The minimum does not depend on the scan
// order, so scan gives the same minimum as the full scan when it skips only
// segments that cannot be nearer than best. Near the limit, scan also sets
// the window and its bounds, because the next points are likely near too.
func (polyline *conflictPolyline) scan(point Point, best, limit, margin float64) (float64, int) {
	nearest := polyline.nearest
	polyline.clear = math.Inf(-1)
	if best > limit+conflictHysteresis {
		return polyline.nearestFar(point, best, margin)
	}
	// Measure the segments a little beyond best, so that the bounds stay
	// valid while the point moves.
	low := max(0, best-margin)
	polyline.lows[nearest] = low * low
	reach := (best + conflictHysteresis) * (best + conflictHysteresis) * (1 + conflictSlack)
	best, nearest = polyline.nearestSegment(point, best, margin, reach)
	polyline.low = max(0, nearest-conflictWindow)
	polyline.high = min(len(polyline.segments)-1, nearest+conflictWindow)
	lowest := polyline.far
	for _, chunk := range polyline.measured {
		for segment := chunk * conflictChunk; segment < min(len(polyline.segments), (chunk+1)*conflictChunk); segment++ {
			if (segment < polyline.low || segment > polyline.high) && polyline.lows[segment] < lowest {
				lowest = polyline.lows[segment]
			}
		}
	}
	polyline.anchor, polyline.clear = point, math.Sqrt(lowest)*(1-conflictSlack)
	polyline.bounds = polyline.bounds[:0]
	for segment := polyline.low; segment <= polyline.high; segment++ {
		// lows holds bounds only for the segments of measured chunks.
		low := polyline.lows[segment]
		if polyline.gaps[segment/conflictChunk] >= 0 {
			low = polyline.segments[segment].gap(point, margin)
		}
		polyline.bounds = append(polyline.bounds, math.Sqrt(low)*(1-conflictSlack))
	}
	return best, nearest
}

// nearestFar finds the segment nearest to point. It measures the chunks in
// the order of their distance from point, so that best falls early and
// bounds out most other chunks.
func (polyline *conflictPolyline) nearestFar(point Point, best, margin float64) (float64, int) {
	points, nearest, gaps := polyline.points, polyline.nearest, polyline.gaps
	for chunk, box := range polyline.chunks {
		gaps[chunk] = box.gap(point, margin)
	}
	for {
		chunk := -1
		for index, gap := range gaps {
			if gap <= best*best*(1+conflictSlack) && (chunk < 0 || gap < gaps[chunk]) {
				chunk = index
			}
		}
		if chunk < 0 {
			break
		}
		gaps[chunk] = math.Inf(1)
		for segment := chunk * conflictChunk; segment < min(len(polyline.segments), (chunk+1)*conflictChunk); segment++ {
			if segment == polyline.nearest || polyline.segments[segment].gap(point, margin) > best*best*(1+conflictSlack) {
				continue
			}
			start, end := points[segment], points[segment+1]
			if segmentPointSquared(point, start, end) > best*best*(1+conflictSlack) {
				continue
			}
			if distance := segmentPointDistance(point, start, end); distance < best {
				best, nearest = distance, segment
			}
		}
	}
	polyline.nearest = nearest
	return best, nearest
}

// nearestSegment finds the segment nearest to point. It measures each
// segment whose box is within the square root of reach, and it keeps a lower
// bound on the squared distance to each segment of a measured chunk in lows.
// far is a lower bound on the squared distance to each other segment. gaps
// holds the bound for each chunk that it skips, and -1 for each chunk that
// it measures.
func (polyline *conflictPolyline) nearestSegment(point Point, best, margin, reach float64) (float64, int) {
	points, nearest, lows := polyline.points, polyline.nearest, polyline.lows
	polyline.far, polyline.measured = math.Inf(1), polyline.measured[:0]
	for chunk, box := range polyline.chunks {
		if gap := box.gap(point, margin); gap > reach {
			polyline.far, polyline.gaps[chunk] = min(polyline.far, gap), gap
			continue
		}
		polyline.measured, polyline.gaps[chunk] = append(polyline.measured, chunk), -1
		for segment := chunk * conflictChunk; segment < min(len(lows), (chunk+1)*conflictChunk); segment++ {
			if segment == polyline.nearest {
				continue
			}
			if gap := polyline.segments[segment].gap(point, margin); gap > reach {
				lows[segment] = gap
				continue
			}
			start, end := points[segment], points[segment+1]
			squared := segmentPointSquared(point, start, end)
			low := 0.0
			if root := math.Sqrt(squared)*(1-conflictSlack) - margin; root > 0 {
				low = root
			}
			lows[segment] = low * low
			if squared > best*best*(1+conflictSlack) {
				continue
			}
			if distance := segmentPointDistance(point, start, end); distance < best {
				best, nearest = distance, segment
			}
		}
	}
	polyline.nearest = nearest
	return best, nearest
}

func (polyline *conflictPolyline) segmentDistance(point Point, segment int) float64 {
	return segmentPointDistance(point, polyline.points[segment], polyline.points[segment+1])
}

// gap returns a lower bound on the squared distance from point to the box.
// margin covers the rounding of the bound and of segmentPointDistance for
// coordinates of magnitude up to margin/conflictSlack.
func (box conflictBox) gap(point Point, margin float64) float64 {
	return (boxGap(point.X, box.minX, box.maxX, margin) + boxGap(point.Y, box.minY, box.maxY, margin)) * (1 - conflictSlack)
}

// boxGap returns the squared distance from v to the range from low to high,
// less margin. It uses comparisons because the min and max builtins handle
// NaN and signed zeros, which cannot occur here, at a cost.
func boxGap(v, low, high, margin float64) float64 {
	gap := 0.0
	if d := low - v - margin; d > 0 {
		gap = d
	} else if d := v - high - margin; d > 0 {
		gap = d
	}
	return gap * gap
}

func pointToPolylineDistance(point Point, points []Point) float64 {
	best := math.Inf(1)
	for i := 1; i < len(points); i++ {
		best = min(best, segmentPointDistance(point, points[i-1], points[i]))
	}
	return best
}

func segmentPointDistance(point, start, end Point) float64 {
	return pointDistance(point, segmentClosest(point, start, end))
}

// segmentPointSquared returns the squared distance from point to the point
// of the segment that segmentPointDistance measures to. It is within a few
// units of rounding of the square of segmentPointDistance.
func segmentPointSquared(point, start, end Point) float64 {
	closest := segmentClosest(point, start, end)
	dx, dy := closest.X-point.X, closest.Y-point.Y
	return dx*dx + dy*dy
}

func segmentClosest(point, start, end Point) Point {
	dx, dy := end.X-start.X, end.Y-start.Y
	lengthSquared := dx*dx + dy*dy
	fraction := 0.0
	if lengthSquared > 0 {
		fraction = max(0, min(1, ((point.X-start.X)*dx+(point.Y-start.Y)*dy)/lengthSquared))
	}
	return Point{X: start.X + fraction*dx, Y: start.Y + fraction*dy}
}
