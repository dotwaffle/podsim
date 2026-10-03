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
type geometryTails struct {
	tail, fromTail float64
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
	for _, lane := range network.Lanes {
		var bounds geometryTails
		if nodes[lane.From] || nodes[lane.To] {
			bounds.tail = largePathTail(network.Polyline(lane))
		}
		if nodes[lane.From] {
			bounds.fromTail = bounds.tail
		}
		tails[lane.ID] = bounds
	}
	return tails
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
