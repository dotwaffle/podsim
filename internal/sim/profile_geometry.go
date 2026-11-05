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
// of a body on this lane.
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
			bounds.tail = largePathTail(lanePointsBetween(lane, positions[lane.From], positions[lane.To], make([]Point, 0, 65)))
			bounds.front = largeEnvelopeRadius * bounds.tail / largeClearance
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

// largePathTail bounds physical separation by projection on the lane chord.
// Each segment must advance on that axis. For a minimum projection ratio c,
// a path gap of 20/c meters gives at least 20 meters of physical separation.
// An infinite result means this proof cannot admit the authored path.
func largePathTail(points []Point) float64 {
	if len(points) < 2 {
		return math.Inf(1)
	}
	first, last := points[0], points[len(points)-1]
	chord := pointDistance(first, last)
	if chord <= 0 || !finite(chord) {
		return math.Inf(1)
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
			return math.Inf(1)
		}
		minimum = min(minimum, projection)
	}
	if minimum == 1 {
		return largeClearance
	}
	return largeClearance / (minimum * (1 - conflictSlack))
}
