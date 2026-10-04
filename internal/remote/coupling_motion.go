package remote

import (
	"math"
	"slices"

	"github.com/dotwaffle/podsim/internal/sim"
)

// couplingMotionSlack is the largest distance in meters between a moved
// corner of the earlier frame and the same corner of the later frame.
const couplingMotionSlack = 1e-6

// couplingStep holds two frames and the display time between them.
type couplingStep struct {
	a, b      motionFrame
	fraction  float64
	maxTravel float64
	geometry  *motionGeometry
}

// couplingMarked reports whether a state carries physical coupling data.
func couplingMarked(state sim.Snapshot) bool {
	return state.CouplingContract != "" || len(state.CouplingGroups) != 0
}

// detachCoupling gives a marked state its own vehicle slice and train
// registry. A caller can then change the cabins and train geometry of a
// sample without changing a buffered frame. Nested cabin slices, such as
// routes and riders, stay shared, as in the interpolation of cabins. An
// unmarked state returns as is.
func detachCoupling(state sim.Snapshot) sim.Snapshot {
	if !couplingMarked(state) {
		return state
	}
	state.Vehicles = slices.Clone(state.Vehicles)
	state.CouplingGroups = slices.Clone(state.CouplingGroups)
	for i := range state.CouplingGroups {
		state.CouplingGroups[i] = detachCouplingGroup(state.CouplingGroups[i])
	}
	return state
}

// detachCouplingGroup returns group with its own optional values.
func detachCouplingGroup(group sim.CouplingGroupView) sim.CouplingGroupView {
	if group.CommonSpeed != nil {
		group.CommonSpeed = new(*group.CommonSpeed)
	}
	if group.Connector != nil {
		group.Connector = new(*group.Connector)
	}
	if group.ManeuverEnvelope != nil {
		group.ManeuverEnvelope = new(*group.ManeuverEnvelope)
	}
	return group
}

// interpolateCoupling replaces the groups of snapshot and the cabins of their
// members. A group that is the same in both frames moves with its cabins.
// Other groups and cabins that leave a group show their latest state. It
// returns false when the frames cannot be combined. The caller then shows
// the latest frame.
func interpolateCoupling(snapshot *sim.Snapshot, step couplingStep) bool {
	before, after := step.a.state.Simulation, step.b.state.Simulation
	if before.CouplingContract != after.CouplingContract {
		return false
	}
	earlier := make(map[string]sim.CouplingGroupView, len(before.CouplingGroups))
	for _, group := range before.CouplingGroups {
		earlier[group.ID] = group
	}
	groups := make([]sim.CouplingGroupView, 0, len(after.CouplingGroups))
	placed := make(map[string]bool, 2*len(after.CouplingGroups))
	for _, latest := range after.CouplingGroups {
		group, cabins, ok := step.group(earlier[latest.ID], latest)
		if !ok {
			if cabins, ok = step.b.cabins(latest); !ok {
				return false
			}
			group = detachCouplingGroup(latest)
		}
		for i, id := range group.Members {
			index, exists := step.a.vehicles[id]
			if !exists || placed[id] {
				return false
			}
			snapshot.Vehicles[index], placed[id] = cabins[i], true
		}
		groups = append(groups, group)
	}
	for _, group := range before.CouplingGroups {
		for _, id := range group.Members {
			if placed[id] {
				continue
			}
			// The cabin left its group. It shows its latest state.
			index, exists := step.a.vehicles[id]
			latest, found := step.b.vehicles[id]
			if !exists || !found {
				return false
			}
			snapshot.Vehicles[index] = after.Vehicles[latest]
		}
	}
	if len(groups) == 0 {
		groups = nil
	}
	snapshot.CouplingGroups = groups
	return true
}

// cabins returns the members of group in frame order. Each member must be in
// the frame and bound to the group.
func (f motionFrame) cabins(group sim.CouplingGroupView) ([2]sim.Vehicle, bool) {
	var cabins [2]sim.Vehicle
	for i, id := range group.Members {
		index, ok := f.vehicles[id]
		if !ok || f.state.Simulation.Vehicles[index].CouplingID != group.ID {
			return cabins, false
		}
		cabins[i] = f.state.Simulation.Vehicles[index]
	}
	return cabins, true
}

// group moves the earlier group toward the latest one. It returns false
// unless both frames show the same group and the earlier geometry, moved with
// the cabins, becomes the latest geometry.
func (s couplingStep) group(earlier, latest sim.CouplingGroupView) (sim.CouplingGroupView, [2]sim.Vehicle, bool) {
	from, fromOK := s.a.cabins(earlier)
	to, toOK := s.b.cabins(latest)
	if !fromOK || !toOK || !sameCouplingGroup(earlier, latest) || !finiteCoupling(earlier, from) || !finiteCoupling(latest, to) {
		return sim.CouplingGroupView{}, from, false
	}
	moves, ends, ok := s.moves(earlier, latest, from, to)
	if !ok {
		return sim.CouplingGroupView{}, from, false
	}
	// The end move must give the latest geometry and the latest rear cabin.
	// Otherwise the frames do not show one motion of the group.
	pins := [2]sim.Point{from[0].Pod.Position, from[1].Pod.Position}
	rear := ends[1].apply(pins[1])
	if shapes, count := movedShapes(earlier, pins, ends); !sameCouplingShapes(shapes[:count], latest) ||
		!(math.Hypot(rear.X-to[1].Pod.Position.X, rear.Y-to[1].Pod.Position.Y) <= couplingMotionSlack) {
		return sim.CouplingGroupView{}, from, false
	}
	group, cabins := moveCouplingGroup(earlier, pins, moves), from
	for i := range cabins {
		cabins[i].Pod.Position = moves[i].apply(pins[i])
		cabins[i].Pod.Speed += (to[i].Pod.Speed - from[i].Pod.Speed) * s.fraction
	}
	if group.CommonSpeed != nil {
		speed := *earlier.CommonSpeed + (*latest.CommonSpeed-*earlier.CommonSpeed)*s.fraction
		group.CommonSpeed = &speed
	}
	return group, cabins, true
}

// moves returns the move of each cabin at the step fraction and at the end of
// the step. Each cabin follows its route. A group with a connector moves as
// one rigid unit: the front cabin follows its route, and the group turns as
// the front body turns from one frame to the next.
func (s couplingStep) moves(earlier, latest sim.CouplingGroupView, from, to [2]sim.Vehicle) (moves, ends [2]rigidMove, ok bool) {
	for i := range from {
		if i > 0 && earlier.Connector != nil {
			break // The rear cabin moves with the front cabin.
		}
		position, ok := s.geometry.interpolatePosition(from[i], to[i], s.fraction, s.maxTravel)
		if !ok {
			return moves, ends, false
		}
		moves[i] = rigidMove{pivot: from[i].Pod.Position, to: position}
		ends[i] = rigidMove{pivot: from[i].Pod.Position, to: to[i].Pod.Position}
	}
	if earlier.Connector != nil {
		turn := math.Remainder(bodyHeading(latest.Bodies[0])-bodyHeading(earlier.Bodies[0]), 2*math.Pi)
		moves[0].turn, ends[0].turn = turn*s.fraction, turn
		moves[1], ends[1] = moves[0], ends[0]
	}
	return moves, ends, true
}

// rigidMove turns a point about pivot and then moves pivot to to.
type rigidMove struct {
	pivot, to sim.Point
	turn      float64
}

func (m rigidMove) apply(p sim.Point) sim.Point {
	sin, cos := math.Sincos(m.turn)
	x, y := p.X-m.pivot.X, p.Y-m.pivot.Y
	return sim.Point{X: m.to.X + x*cos - y*sin, Y: m.to.Y + x*sin + y*cos}
}

// bodyHeading returns the direction of a body from its rear to its front.
// The first corner is at the front and the second corner is at the rear.
func bodyHeading(body sim.CouplingRectangle) float64 {
	front, rear := body.Corners[0], body.Corners[1]
	return math.Atan2(front.Y-rear.Y, front.X-rear.X)
}

// movedShapes returns the rectangles of group in couplingShapes order,
// moved with the cabins. Body i moves with cabin i. A connector or envelope
// corner moves with the nearer cabin. count is the number of rectangles.
func movedShapes(group sim.CouplingGroupView, pins [2]sim.Point, moves [2]rigidMove) (shapes [4]sim.CouplingRectangle, count int) {
	shapes, count = couplingShapes(group)
	for i := range shapes[:count] {
		for c, corner := range shapes[i].Corners {
			cabin := min(i, 1)
			if i >= len(group.Bodies) {
				cabin = 0
				if math.Hypot(corner.X-pins[1].X, corner.Y-pins[1].Y) < math.Hypot(corner.X-pins[0].X, corner.Y-pins[0].Y) {
					cabin = 1
				}
			}
			shapes[i].Corners[c] = moves[cabin].apply(corner)
		}
	}
	return shapes, count
}

// moveCouplingGroup returns a copy of group with its moved shapes.
func moveCouplingGroup(group sim.CouplingGroupView, pins [2]sim.Point, moves [2]rigidMove) sim.CouplingGroupView {
	shapes, _ := movedShapes(group, pins, moves)
	group.Bodies = [2]sim.CouplingRectangle{shapes[0], shapes[1]}
	next := len(group.Bodies)
	if group.Connector != nil {
		group.Connector = new(shapes[next])
		next++
	}
	if group.ManeuverEnvelope != nil {
		group.ManeuverEnvelope = new(shapes[next])
	}
	return group
}

// sameCouplingGroup reports whether two frames show the same group in the
// same phase. Progress and dwell change on each tick and are not compared.
func sameCouplingGroup(a, b sim.CouplingGroupView) bool {
	return a.ID != "" && a.ID == b.ID && a.Members == b.Members && a.FormationTick == b.FormationTick && a.Phase == b.Phase &&
		a.CorridorID == b.CorridorID && a.AssemblySiteID == b.AssemblySiteID && a.SplitSiteID == b.SplitSiteID &&
		a.Profile == b.Profile && a.OwnerID == b.OwnerID && (a.CommonSpeed == nil) == (b.CommonSpeed == nil) &&
		(a.Connector == nil) == (b.Connector == nil) && (a.ManeuverEnvelope == nil) == (b.ManeuverEnvelope == nil)
}

// sameCouplingShapes compares rectangles in couplingShapes order with the
// geometry of group.
func sameCouplingShapes(first []sim.CouplingRectangle, group sim.CouplingGroupView) bool {
	second, count := couplingShapes(group)
	if len(first) != count {
		return false
	}
	for i, shape := range first {
		for c, corner := range shape.Corners {
			other := second[i].Corners[c]
			if !(math.Hypot(corner.X-other.X, corner.Y-other.Y) <= couplingMotionSlack) {
				return false
			}
		}
	}
	return true
}

// finiteCoupling reports whether all geometry and motion of a group and its
// cabins are finite.
func finiteCoupling(group sim.CouplingGroupView, cabins [2]sim.Vehicle) bool {
	finite := func(values ...float64) bool {
		for _, value := range values {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return false
			}
		}
		return true
	}
	shapes, count := couplingShapes(group)
	for _, shape := range shapes[:count] {
		for _, corner := range shape.Corners {
			if !finite(corner.X, corner.Y) {
				return false
			}
		}
	}
	for i := range cabins {
		if pod := cabins[i].Pod; !finite(pod.Position.X, pod.Position.Y, pod.Speed) {
			return false
		}
	}
	return group.CommonSpeed == nil || finite(*group.CommonSpeed)
}

// couplingShapes lists the rectangles of a group in a fixed order: both
// bodies, then the connector or envelope that the group has. count is the
// number of rectangles. A fixed array keeps a map sample from allocating
// for each check.
func couplingShapes(group sim.CouplingGroupView) (shapes [4]sim.CouplingRectangle, count int) {
	shapes[0], shapes[1], count = group.Bodies[0], group.Bodies[1], 2
	for _, shape := range []*sim.CouplingRectangle{group.Connector, group.ManeuverEnvelope} {
		if shape != nil {
			shapes[count] = *shape
			count++
		}
	}
	return shapes, count
}
