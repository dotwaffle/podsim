package scenarios

import (
	"math"
	"strings"

	"github.com/dotwaffle/podsim/internal/sim"
)

// The road fillets of docs/contracts/london-fillets-proposal.md. A station
// road or a Parking road gets the quadratic curve that leaves the lane
// before it and joins the lane after it at the analytic heading of each.
// The lane before a road-in is the link lane that ends at its arrival
// portal, and the lane after it is the access-in lane of its station. The
// lane before a road-out is the access-out lane of its station, and the
// lane after it is the link lane that starts at its departure portal.

const (
	// londonFilletParallel is the cross product of the unit headings
	// below which the two rays are parallel.
	londonFilletParallel = 1e-9
	// londonFilletBehind is the ray distance in meters at or below which
	// the rays meet behind an end of the road.
	londonFilletBehind = 1e-6
	// londonFilletTangent is the angle in radians within which an end of
	// a road is already tangent.
	londonFilletTangent = math.Pi / 180
)

// londonFilletOutcome tells what the rule does with a road.
type londonFilletOutcome int

const (
	// londonFilletCurved gives the road its curve and the speed limit of
	// the curve.
	londonFilletCurved londonFilletOutcome = iota
	// londonFilletNoNeighbor keeps the chord: a lane before or after the
	// road is missing, or a heading or the chord has no direction.
	londonFilletNoNeighbor
	// londonFilletControl keeps the road, which already has a control
	// point.
	londonFilletControl
	// londonFilletParallelRays keeps the chord: the headings are
	// parallel.
	londonFilletParallelRays
	// londonFilletRaysBehind keeps the chord: the rays meet behind an end.
	londonFilletRaysBehind
	// londonFilletTangentEnd keeps the chord: an end is already tangent.
	londonFilletTangentEnd
	// londonFilletSlow keeps the chord: the smallest radius of the curve
	// is below that of londonRampMinimumSpeed.
	londonFilletSlow
)

// londonFillet is the result of the rule for one road.
type londonFillet struct {
	// lane is the index of the road in the network.
	lane    int
	outcome londonFilletOutcome
	// control and speed are the control point and the speed limit of the
	// curve. They are set for londonFilletCurved and londonFilletSlow.
	control sim.Point
	speed   float64
}

// filletLondonRoads gives each station road and Parking road of network
// its curve, by the rule of londonRoadFillets.
func filletLondonRoads(network *sim.Network) {
	for _, fillet := range londonRoadFillets(*network) {
		if fillet.outcome != londonFilletCurved {
			continue
		}
		lane := &network.Lanes[fillet.lane]
		lane.Control, lane.SpeedLimit = new(fillet.control), fillet.speed
	}
}

// londonRoadFillets returns the result of the rule for each road of
// network, in lane order. A road is a lane <station>-road-in-NN or
// <station>-road-out-NN of its station. The rule takes the heading u at
// the start a of the road and the heading v at its end b, and the control
// point C = a + s u = b - t v. It keeps the road when it already has a
// control point. It keeps the chord, in this order of the checks, when u,
// v, or the chord has no direction, when the rays are parallel, when an end
// is already tangent, when s or t is at most londonFilletBehind, or when
// the curve is slower than londonRampMinimumSpeed.
func londonRoadFillets(network sim.Network) []londonFillet {
	positions := make(map[string]sim.Point, len(network.Nodes))
	for _, node := range network.Nodes {
		positions[node.ID] = node.Position
	}
	lanes := make(map[string]int, len(network.Lanes))
	// linksIn and linksOut count the link lanes that end and start at
	// each node, by the index of the last one.
	type links struct{ lane, count int }
	linksIn, linksOut := make(map[string]links), make(map[string]links)
	for index, lane := range network.Lanes {
		lanes[lane.ID] = index
		if strings.HasPrefix(lane.ID, "london-link-") {
			linksIn[lane.To] = links{lane: index, count: linksIn[lane.To].count + 1}
			linksOut[lane.From] = links{lane: index, count: linksOut[lane.From].count + 1}
		}
	}
	tangent := func(lane sim.Lane, end bool) sim.Point {
		from, to := positions[lane.From], positions[lane.To]
		if lane.Control != nil && end && *lane.Control != to {
			from = *lane.Control
		} else if lane.Control != nil && !end && *lane.Control != from {
			to = *lane.Control
		}
		return londonUnit(sim.Point{X: to.X - from.X, Y: to.Y - from.Y})
	}
	var fillets []londonFillet
	for index, lane := range network.Lanes {
		in := lane.StationID != "" && strings.HasPrefix(lane.ID, lane.StationID+"-road-in-")
		out := lane.StationID != "" && strings.HasPrefix(lane.ID, lane.StationID+"-road-out-")
		if !in && !out {
			continue
		}
		fillet := londonFillet{lane: index}
		var before, after links
		var access int
		var ok bool
		if in {
			before = linksIn[lane.From]
			access, ok = lanes[lane.StationID+"-access-in"]
			after = links{lane: access, count: 1}
		} else {
			access, ok = lanes[lane.StationID+"-access-out"]
			before = links{lane: access, count: 1}
			after = linksOut[lane.To]
		}
		if !ok || before.count != 1 || after.count != 1 {
			fillet.outcome = londonFilletNoNeighbor
			fillets = append(fillets, fillet)
			continue
		}
		u, v := tangent(network.Lanes[before.lane], true), tangent(network.Lanes[after.lane], false)
		fillet.outcome, fillet.control, fillet.speed = londonFilletCurve(lane, positions[lane.From], positions[lane.To], u, v)
		fillets = append(fillets, fillet)
	}
	return fillets
}

// londonFilletCurve returns the outcome of the rule for the road lane from
// a to b, with the unit heading u at a and v at b, and the control point
// and the speed limit of its curve.
func londonFilletCurve(lane sim.Lane, a, b, u, v sim.Point) (londonFilletOutcome, sim.Point, float64) {
	if lane.Control != nil {
		return londonFilletControl, sim.Point{}, 0
	}
	// A zero or non-finite heading or chord makes NaN values that pass each
	// check below.
	d := sim.Point{X: b.X - a.X, Y: b.Y - a.Y}
	if !londonDirected(u) || !londonDirected(v) || !londonDirected(d) {
		return londonFilletNoNeighbor, sim.Point{}, 0
	}
	cross := u.X*v.Y - u.Y*v.X
	if math.Abs(cross) < londonFilletParallel {
		return londonFilletParallelRays, sim.Point{}, 0
	}
	s := (d.X*v.Y - d.Y*v.X) / cross
	t := (u.X*d.Y - u.Y*d.X) / cross
	chord := londonUnit(d)
	if londonAngle(u, chord) < londonFilletTangent || londonAngle(chord, v) < londonFilletTangent {
		return londonFilletTangentEnd, sim.Point{}, 0
	}
	if s <= londonFilletBehind || t <= londonFilletBehind {
		return londonFilletRaysBehind, sim.Point{}, 0
	}
	control := sim.Point{X: a.X + s*u.X, Y: a.Y + s*u.Y}
	speed := londonRampSpeed(a, control, b)
	if speed < londonRampMinimumSpeed {
		return londonFilletSlow, control, speed
	}
	return londonFilletCurved, control, speed
}

// londonUnit returns the direction of p.
func londonUnit(p sim.Point) sim.Point {
	length := math.Hypot(p.X, p.Y)
	return sim.Point{X: p.X / length, Y: p.Y / length}
}

// londonDirected reports whether p has a direction: its length is finite
// and above zero.
func londonDirected(p sim.Point) bool {
	length := math.Hypot(p.X, p.Y)
	return length > 0 && !math.IsInf(length, 0)
}

// londonAngle returns the angle in radians between the unit directions u
// and v.
func londonAngle(u, v sim.Point) float64 {
	return math.Abs(math.Atan2(u.X*v.Y-u.Y*v.X, u.X*v.X+u.Y*v.Y))
}
