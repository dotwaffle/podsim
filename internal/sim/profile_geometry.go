package sim

import "math"

// Large profiles use a centered simulation envelope. The bounds come from
// the approved candidate and do not certify a manufactured vehicle.
const (
	largeClearance      = 20.0
	largeEnvelopeRadius = 6.0
)

func largeVehicleClass(class VehicleClass) bool {
	return class == GroupClass || class == ExpressClass
}

func largeClassSet(classes ClassSet) bool {
	return classes.Allows(string(GroupClass)) || classes.Allows(string(ExpressClass))
}

func classPairClearance(first, second VehicleClass) float64 {
	if largeVehicleClass(first) || largeVehicleClass(second) {
		return largeClearance
	}
	return Clearance
}

func classSetPairClearance(first, second ClassSet) float64 {
	if largeClassSet(first) || largeClassSet(second) {
		return largeClearance
	}
	return Clearance
}

// LaneMinimumLength returns the minimum length for an authored class mask.
// Validate the mask before using this geometry bound.
func LaneMinimumLength(lane Lane) float64 {
	return 2 * classSetPairClearance(lane.VehicleClasses, lane.VehicleClasses)
}

func largeBerth(station Station, berth Berth) bool {
	for _, class := range []VehicleClass{GroupClass, ExpressClass} {
		if station.VehicleClasses.Allows(string(class)) && berth.VehicleClasses.Allows(string(class)) {
			return true
		}
	}
	return false
}

// geometryTails stores only larger bounds. Zero retains ordinary arithmetic.
// front is the distance past the center that the observer takes as the front
// of a body on this lane. It comes from the projection bound of the lane and
// not from the tail, so a shorter tail does not move the front.
type geometryTails struct {
	tail, fromTail, front float64
}

// indexGeometryTails includes small lanes incident to large-admitting nodes.
// A small owner can precede a large follower at those shared resources.
func indexGeometryTails(network Network) map[string]geometryTails {
	nodes := make(map[string]bool)
	for _, lane := range network.Lanes {
		if largeClassSet(lane.VehicleClasses) {
			nodes[lane.From], nodes[lane.To] = true, true
		}
	}
	for _, station := range network.Stations {
		for _, berth := range station.Berths {
			if largeBerth(station, berth) {
				nodes[berth.Node] = true
			}
		}
	}
	tails := make(map[string]geometryTails, len(network.Lanes))
	var positions map[string]Point
	for _, lane := range network.Lanes {
		var bounds geometryTails
		if nodes[lane.From] || nodes[lane.To] {
			if positions == nil {
				positions = indexNodePositions(network.Nodes)
			}
			var oldBound float64
			bounds.tail, oldBound = largePathBounds(lanePointsBetween(lane, positions[lane.From], positions[lane.To], make([]Point, 0, 65)))
			bounds.front = largeEnvelopeRadius * oldBound / largeClearance
		}
		if nodes[lane.From] {
			bounds.fromTail = bounds.tail
		}
		tails[lane.ID] = bounds
	}
	return tails
}

// LaneTailOverrun is a lane whose large-class tail is longer than the lane.
// A pod that enters such a lane keeps its resources past the end of the lane.
type LaneTailOverrun struct {
	LaneID       string
	Tail, Length float64
}

// LaneTailOverruns returns the lanes of a validated network, in lane order,
// whose tail from indexGeometryTails exceeds their length. A path with an
// infinite tail is not in the result, because validation refuses it.
func (n Network) LaneTailOverruns() []LaneTailOverrun {
	return laneTailOverruns(n, indexGeometryTails(n))
}

// TailOverruns is LaneTailOverruns with the tails of the index, for the
// network of the index.
func (c LaneClearance) TailOverruns(network Network) []LaneTailOverrun {
	return laneTailOverruns(network, c.tails)
}

func laneTailOverruns(n Network, tails map[string]geometryTails) []LaneTailOverrun {
	var overruns []LaneTailOverrun
	var positions map[string]Point
	for _, lane := range n.Lanes {
		tail := tails[lane.ID].tail
		if tail == 0 || !finite(tail) {
			continue
		}
		if positions == nil {
			positions = indexNodePositions(n.Nodes)
		}
		if length := indexedLaneLength(lane, positions[lane.From], positions[lane.To]); tail > length {
			overruns = append(overruns, LaneTailOverrun{LaneID: lane.ID, Tail: tail, Length: length})
		}
	}
	return overruns
}

// oldLargePathBound bounds physical separation by projection on the lane
// chord. Each segment must advance on that axis. For a minimum projection
// ratio c, a path gap of 20/c meters gives at least 20 meters of physical
// separation. An infinite result means this proof cannot admit the authored
// path.
func oldLargePathBound(points []Point) float64 {
	if len(points) < 2 {
		return math.Inf(1)
	}
	minimum := chordProjection(points)
	if minimum <= 0 {
		return math.Inf(1)
	}
	if minimum == 1 {
		return largeClearance
	}
	return largeClearance / (minimum * (1 - conflictSlack))
}

// chordProjection returns the least projection of a segment of a polyline
// on its chord, as a ratio of the length of the segment. It returns 0 when
// the chord has no length or a segment does not advance on the chord.
func chordProjection(points []Point) float64 {
	first, last := points[0], points[len(points)-1]
	chord := pointDistance(first, last)
	if chord <= 0 || !finite(chord) {
		return 0
	}
	ux, uy := (last.X-first.X)/chord, (last.Y-first.Y)/chord
	minimum := 1.0
	for index := 1; index < len(points); index++ {
		a, b := points[index-1], points[index]
		length := pointDistance(a, b)
		if length == 0 {
			continue
		}
		projection := (b.X-a.X)/length*ux + (b.Y-a.Y)/length*uy
		if projection <= 0 || !finite(projection) {
			return 0
		}
		minimum = min(minimum, projection)
	}
	return minimum
}

// smallFrontCap is the largest front bound of rule 1 (see envelopeFront):
// (2 + sqrt 2) times the radius of the disk that holds the grown small body
// with the guideway tolerance of 0.25 m, rounded up to 0.1 m. The radius is
// the corner radius 4.4769 m of classShapeBody(CompactClass) plus 0.25 m.
// An arc of one lane from the reference to an end of the lane in that disk
// is at most this long (B2 of section 3.1 of the shared-track design).
const smallFrontCap = 16.2

// envelopeFront returns the front bound of a lane for the envelope of a
// pod: the front of the tail index. When the network sets ShapeClearance,
// rule 1 gives a curved lane with no geometry tail the front
// min(6 / (c (1 - conflictSlack)), smallFrontCap), where c is the chord
// projection of the lane, and smallFrontCap when c is 0. The front then
// holds the next lane of the route while the body of a small pod can reach
// it. points returns the polyline of the lane. Only rule 1 calls it.
func envelopeFront(lane Lane, bounds geometryTails, shapeClearance bool, points func() []Point) float64 {
	if !shapeClearance || bounds != (geometryTails{}) || lane.Control == nil {
		return bounds.front
	}
	c := chordProjection(points())
	if c <= 0 {
		return smallFrontCap
	}
	return min(largeEnvelopeRadius/(c*(1-conflictSlack)), smallFrontCap)
}
