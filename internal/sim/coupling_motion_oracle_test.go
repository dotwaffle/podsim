package sim

import (
	"math"
	"testing"
)

func couplingIndependentBodyOracle(t *testing.T, c *couplingMotionContext, previous couplingMotionState, step couplingMotionStep) {
	t.Helper()
	state := step.State
	for member, rectangle := range step.Bodies {
		center, direction, laneID, local := couplingRawPose(t, c.reservation.network.prepared.network, c.reservation.members[member].Vehicle.Route, state.Distances[member])
		if math.Hypot(state.Positions[member].X-center.X, state.Positions[member].Y-center.Y) > 1e-9 || math.Hypot(state.Directions[member].X-direction.X, state.Directions[member].Y-direction.Y) > 1e-9 || state.Lanes[member] != laneID || math.Abs(step.Members[member].Pod.LaneDistance-local) > 1e-9 {
			t.Fatal("published pose differs from independent raw polyline arc lengths")
		}
		for i, sign := range [][2]float64{{2, 1}, {-2, 1}, {-2, -1}, {2, -1}} {
			corner := rectangle.Corners[i]
			x := center.X + direction.X*sign[0] - direction.Y*sign[1]
			y := center.Y + direction.Y*sign[0] + direction.X*sign[1]
			if math.Abs(corner.X-x) > 1e-9 || math.Abs(corner.Y-y) > 1e-9 || math.Hypot(corner.X-center.X, corner.Y-center.Y) > math.Sqrt(5)+1e-9 {
				t.Fatal("independent four-by-two body corner/rotation oracle failed")
			}
		}
	}
	if state.Phase == couplingDraining {
		if step.Connector != nil {
			t.Fatal("post-opening connector exemption remains")
		}
		for member := range state.Distances {
			other, _, _, _ := couplingRawPose(t, c.reservation.network.prepared.network, c.reservation.members[1-member].Vehicle.Route, previous.Distances[1-member])
			for _, segment := range couplingRawTrace(t, c.reservation.network.prepared.network, c.reservation.members[member].Vehicle.Route, previous.Distances[member], state.Distances[member]) {
				if couplingRawPointSegment(other, segment[0], segment[1]) < 12-1e-9 {
					t.Fatal("independent canonical swept drain separation failed")
				}
			}
		}
		return
	}
	if step.Connector == nil {
		t.Fatal("connected envelope omitted the real connector")
	}
	front := state.Distances[0] - c.reservation.axisOrigins[0]
	rear := state.Distances[1] - c.reservation.axisOrigins[1]
	if front-rear < 4.5-1e-9 || front-rear > 12+1e-9 {
		t.Fatal("independent connected spacing bound failed")
	}
	corridor := c.reservation.network.corridors[c.reservation.corridorID]
	axis := c.reservation.network.lanes[corridor.LaneIDs[0]].direction
	// Pins lie two meters toward the other cabin. The connector has half-width .15.
	pinRear := Point{X: state.Positions[1].X + 2*axis.X, Y: state.Positions[1].Y + 2*axis.Y}
	pinFront := Point{X: state.Positions[0].X - 2*axis.X, Y: state.Positions[0].Y - 2*axis.Y}
	for i, sign := range []float64{1, 1, -1, -1} {
		pin := pinFront
		if i == 1 || i == 2 {
			pin = pinRear
		}
		want := Point{X: pin.X - axis.Y*.15*sign, Y: pin.Y + axis.X*.15*sign}
		corner := step.Connector.Corners[i]
		if math.Hypot(corner.X-want.X, corner.Y-want.Y) > 1e-9 {
			t.Fatal("independent real pin/connector corner oracle failed")
		}
	}
	if state.Phase == couplingClosing || state.Phase == couplingLatching || state.Phase == couplingOpening || state.Phase == couplingUnlatching {
		siteID := corridor.AssemblySiteID
		if state.Phase == couplingOpening || state.Phase == couplingUnlatching {
			siteID = corridor.SplitSiteID
		}
		site := c.reservation.network.sites[siteID]
		for i := range state.Positions {
			local := step.Members[i].Pod.LaneDistance
			stop := state.Speeds[i]*state.Speeds[i] + state.Speeds[i]/60
			if local-2-12-stop < site.StartMeters-1e-9 || local+2+12+stop > site.EndMeters+1e-9 {
				t.Fatal("independent body/stop/site room oracle failed")
			}
		}
	}
	if state.Phase != couplingDraining {
		oldGap := (previous.Distances[0] - c.reservation.axisOrigins[0]) - (previous.Distances[1] - c.reservation.axisOrigins[1])
		if min(oldGap, front-rear) < 2*math.Sqrt(5) {
			t.Fatal("synchronized swept body circles overlap")
		}
	}
}

// This oracle uses raw route lane offsets, not planner cap or speed helpers.
func couplingIndependentLaneOracle(t *testing.T, c *couplingMotionContext, previous, next couplingMotionState) {
	t.Helper()
	braking := 2.0
	if previous.Phase == couplingClosing || previous.Phase == couplingOpening {
		braking = .5
	}
	for member := range next.Distances {
		start, end, speed := previous.Distances[member], next.Distances[member], next.Speeds[member]
		reach, offset := end+speed*speed/(2*braking)+speed/60, 0.0
		for _, lane := range c.reservation.members[member].Vehicle.Route {
			points := c.reservation.network.prepared.network.Polyline(lane)
			length := 0.0
			for k := 1; k < len(points); k++ {
				length += math.Hypot(points[k].X-points[k-1].X, points[k].Y-points[k-1].Y)
			}
			lo, hi := offset, offset+length
			offset = hi
			if hi < start || lo > reach {
				continue
			}
			if lo <= end && speed > lane.SpeedLimit {
				t.Fatalf("crossed lane %s exceeded", lane.ID)
			}
			if lo > end && lane.SpeedLimit < speed && reach > lo+lane.SpeedLimit*lane.SpeedLimit/(2*braking) {
				t.Fatalf("future low lane %s lacks braking room", lane.ID)
			}
		}
	}
}

// Raw polylines and their accumulated arc lengths are independent of planner caches.
func couplingRawPose(t *testing.T, network Network, route []Lane, distance float64) (Point, Point, string, float64) {
	t.Helper()
	offset := 0.0
	for i, lane := range route {
		points := network.Polyline(lane)
		length := 0.0
		for k := 1; k < len(points); k++ {
			length += math.Hypot(points[k].X-points[k-1].X, points[k].Y-points[k-1].Y)
		}
		if distance >= offset+length && i+1 < len(route) {
			offset += length
			continue
		}
		local, arc := distance-offset, 0.0
		for k := 1; k < len(points); k++ {
			dx, dy := points[k].X-points[k-1].X, points[k].Y-points[k-1].Y
			segment := math.Hypot(dx, dy)
			if segment == 0 {
				continue
			}
			if local > arc+segment && k+1 < len(points) {
				arc += segment
				continue
			}
			fraction := (local - arc) / segment
			return Point{X: points[k-1].X + fraction*dx, Y: points[k-1].Y + fraction*dy}, Point{X: dx / segment, Y: dy / segment}, lane.ID, local
		}
	}
	t.Fatal("raw route cannot locate published distance")
	return Point{}, Point{}, "", 0
}

func couplingRawTrace(t *testing.T, network Network, route []Lane, start, end float64) [][2]Point {
	t.Helper()
	var result [][2]Point
	arc := 0.0
	for _, lane := range route {
		points := network.Polyline(lane)
		for k := 1; k < len(points); k++ {
			a, b := points[k-1], points[k]
			length := math.Hypot(b.X-a.X, b.Y-a.Y)
			lo, hi := arc, arc+length
			arc = hi
			if length == 0 || hi < start || lo > end {
				continue
			}
			fraction0, fraction1 := (max(start, lo)-lo)/length, (min(end, hi)-lo)/length
			result = append(result, [2]Point{{X: a.X + fraction0*(b.X-a.X), Y: a.Y + fraction0*(b.Y-a.Y)}, {X: a.X + fraction1*(b.X-a.X), Y: a.Y + fraction1*(b.Y-a.Y)}})
		}
	}
	if len(result) == 0 {
		t.Fatal("raw trace omitted the whole tick interval")
	}
	return result
}

func couplingRawPointSegment(point, a, b Point) float64 {
	dx, dy := b.X-a.X, b.Y-a.Y
	denominator := dx*dx + dy*dy
	fraction := 0.0
	if denominator > 0 {
		fraction = max(0, min(1, ((point.X-a.X)*dx+(point.Y-a.Y)*dy)/denominator))
	}
	return math.Hypot(point.X-a.X-fraction*dx, point.Y-a.Y-fraction*dy)
}

func couplingIndependentParkedSweep(t *testing.T, c *couplingMotionContext, previous, next couplingMotionState, path *couplingForeignPath) {
	t.Helper()
	clearance := 12.0
	large := path.class == GroupClass || path.class == ExpressClass
	if large {
		clearance = 20
	}
	for member := range next.Distances {
		for _, segment := range couplingRawTrace(t, c.reservation.network.prepared.network, c.reservation.members[member].Vehicle.Route, previous.Distances[member], next.Distances[member]) {
			distance := couplingRawPointSegment(path.position, segment[0], segment[1])
			if distance < clearance {
				t.Fatal("independent canonical foreign sweep exclusion failed")
			}
			if large && distance < 6+math.Sqrt(5) {
				t.Fatal("large approved circle touches swept real body radius")
			}
		}
	}
	if next.Phase != couplingDraining {
		front, axis, _, _ := couplingRawPose(t, c.reservation.network.prepared.network, c.reservation.members[0].Vehicle.Route, next.Distances[0])
		rear, _, _, _ := couplingRawPose(t, c.reservation.network.prepared.network, c.reservation.members[1].Vehicle.Route, previous.Distances[1])
		pinRear := Point{X: rear.X + 2*axis.X, Y: rear.Y + 2*axis.Y}
		pinFront := Point{X: front.X - 2*axis.X, Y: front.Y - 2*axis.Y}
		if couplingRawPointSegment(path.position, pinRear, pinFront) < 6+.15 {
			t.Fatal("approved circle/exclusion radius touches swept connector")
		}
	}
}
