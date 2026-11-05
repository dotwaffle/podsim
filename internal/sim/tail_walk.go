package sim

import "math"

// These values set the polyline walk of largePathBounds.
const (
	// tailMinimumStep is the smallest step in meters between the samples
	// of the rear position.
	tailMinimumStep = 0.25
	// tailStepsPerLane limits the samples of a long lane to one more than
	// this number.
	tailStepsPerLane = 1024
	// tailWalkFloor is the smallest step in meters of the front position.
	tailWalkFloor = 0.001
	// tailWalkBudget is the number of distance evaluations of one lane, 8
	// for each sample of a long lane.
	tailWalkBudget = 8 * tailStepsPerLane
)

// largePathTail returns the tail of largePathBounds.
func largePathTail(points []Point) float64 {
	tail, _ := largePathBounds(points)
	return tail
}

// largePathBounds returns the tail of a lane and the projection bound of
// the lane. A straight lane has a tail of exactly 20 meters. The tail of a
// curved lane comes from a walk over its polyline, and it is not more than
// the projection bound, which is itself a valid tail. A lane that passes the
// budget of the walk uses the projection bound. Each result is infinite for
// a path that the projection bound cannot admit.
func largePathBounds(points []Point) (tail, oldBound float64) {
	oldBound = oldLargePathBound(points)
	if !finite(oldBound) {
		return oldBound, oldBound
	}
	if oldBound == largeClearance || len(points) == 2 {
		return largeClearance, oldBound
	}
	walk := newTailWalk(points)
	if !finite(walk.length) {
		return oldBound, oldBound
	}
	scan, _, ok := walk.scan(tailWalkBudget)
	if !ok {
		return oldBound, oldBound
	}
	return min(oldBound, max(largeClearance, walk.gridUp(scan+walk.step))), oldBound
}

// tailWalk finds the largest path gap between two close points of a polyline.
type tailWalk struct {
	points []Point
	// arcs holds the path distance at each point.
	arcs [65]float64
	// length is the path length. step is the distance between the samples
	// of the rear position, and limit is the distance that counts as close.
	length, step, limit float64
}

func newTailWalk(points []Point) tailWalk {
	walk := tailWalk{points: points}
	for index := 1; index < len(points); index++ {
		walk.arcs[index] = walk.arcs[index-1] + pointDistance(points[index-1], points[index])
	}
	walk.length = walk.arcs[len(points)-1]
	walk.step = max(tailMinimumStep, walk.length/tailStepsPerLane)
	walk.limit = largeClearance + walk.step/2 + tailWalkFloor
	return walk
}

// scan returns the largest path gap of a pair closer than the limit, with
// the rear position on the samples and the front position at the last close
// point. A front position moves back from the lane end by the amount over
// the limit, because the distance changes by at most the path distance. A
// walk stops at the largest gap so far, because a later close point cannot
// raise it. scan returns the evaluations that it made, and false when they
// pass the budget.
func (w *tailWalk) scan(budget int) (best float64, used int, ok bool) {
	limitSquared := float64(w.limit * w.limit)
	samples := int(math.Ceil(w.length / w.step))
	rearSegment := 1
	for sample := 0; sample <= samples; sample++ {
		rear := w.length
		if sample < samples {
			rear = float64(float64(sample) * w.step)
		}
		if w.length-rear <= best {
			break
		}
		for rearSegment < len(w.points)-1 && rear > w.arcs[rearSegment] {
			rearSegment++
		}
		a := w.at(rear, rearSegment)
		front, segment := w.length, len(w.points)-1
		for front-rear > best {
			if used == budget {
				return 0, used, false
			}
			used++
			for segment > 1 && front < w.arcs[segment-1] {
				segment--
			}
			b := w.at(front, segment)
			dx, dy := b.X-a.X, b.Y-a.Y
			squared := float64(dx*dx) + float64(dy*dy)
			if squared < limitSquared {
				best = front - rear
				break
			}
			front -= max(math.Sqrt(squared)-w.limit, tailWalkFloor)
		}
	}
	return best, used, true
}

// at returns the point at a path distance on the segment that ends at the
// point with the given index.
func (w *tailWalk) at(distance float64, segment int) Point {
	a, b := w.points[segment-1], w.points[segment]
	span := w.arcs[segment] - w.arcs[segment-1]
	if span == 0 {
		return b
	}
	fraction := (distance - w.arcs[segment-1]) / span
	return Point{X: a.X + float64(fraction*(b.X-a.X)), Y: a.Y + float64(fraction*(b.Y-a.Y))}
}

// gridUp rounds a distance up to a multiple of the sample step.
func (w *tailWalk) gridUp(distance float64) float64 {
	return float64(math.Ceil(distance/w.step) * w.step)
}
