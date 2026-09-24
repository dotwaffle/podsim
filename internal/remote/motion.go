package remote

import (
	"math"
	"slices"
	"time"

	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

const motionDelay = 150 * time.Millisecond
const motionGap = 500 * time.Millisecond
const maxMotionFrames = 8

type motionFrame struct {
	state    session.State
	at       time.Time
	vehicles map[string]int
}

// Motion buffers authoritative snapshots for display only. Use it on the render thread.
// It delays the map by 150 ms and never predicts past received movement.
type Motion struct {
	frames   []motionFrame
	geometry *motionGeometry
}

// Observe adds a new revision. Playback discontinuities discard old motion.
// A state from a new server process starts a new stream, as a new epoch
// does, because a restored older save can lower the revision.
func (m *Motion) Observe(state session.State, at time.Time) {
	restart := false
	if len(m.frames) > 0 {
		previous := m.frames[len(m.frames)-1]
		restart = newServerStart(previous.state, state)
		if state.Epoch == previous.state.Epoch && state.Revision <= previous.state.Revision && !restart {
			return
		}
		a, b := previous.state.Simulation, state.Simulation
		if restart || state.Epoch != previous.state.Epoch || state.Generation != previous.state.Generation || b.Tick < a.Tick || b.Submitted < a.Submitted || b.Paused != a.Paused || state.Speed != previous.state.Speed || b.Demo != a.Demo || at.Sub(previous.at) > motionGap {
			m.frames = nil
		}
	}
	if restart || m.geometry == nil || m.geometry.epoch != state.Epoch || m.geometry.generation != state.Generation {
		m.geometry = newMotionGeometry(state)
	}
	m.frames = append(m.frames, newMotionFrame(state, at))
	if len(m.frames) > maxMotionFrames {
		m.frames = slices.Clone(m.frames[len(m.frames)-maxMotionFrames:])
	}
}

// Sample returns map state at a buffered time. Controls should use the latest server state.
func (m *Motion) Sample(at time.Time) sim.Snapshot {
	if len(m.frames) == 0 {
		return sim.Snapshot{}
	}
	latest := m.frames[len(m.frames)-1]
	if latest.state.Simulation.Paused {
		return latest.state.Simulation
	}
	target := at.Add(-motionDelay)
	if !target.After(m.frames[0].at) {
		return m.frames[0].state.Simulation
	}
	for i := 1; i < len(m.frames); i++ {
		next := m.frames[i]
		if target.Before(next.at) {
			previous := m.frames[i-1]
			fraction := float64(target.Sub(previous.at)) / float64(next.at.Sub(previous.at))
			return interpolateFrames(previous, next, fraction, m.geometry)
		}
	}
	return latest.state.Simulation
}

func interpolate(a, b session.State, fraction float64) sim.Snapshot {
	return interpolateFrames(newMotionFrame(a, time.Time{}), newMotionFrame(b, time.Time{}), fraction, newMotionGeometry(a))
}

func newMotionFrame(state session.State, at time.Time) motionFrame {
	vehicles := make(map[string]int, len(state.Simulation.Vehicles))
	for index := range state.Simulation.Vehicles {
		vehicles[state.Simulation.Vehicles[index].Pod.ID] = index
	}
	return motionFrame{state: state, at: at, vehicles: vehicles}
}

type motionGeometry struct {
	epoch      string
	generation uint64
	maxSpeed   float64
	lanes      map[string]motionLane
}

type motionLane struct {
	points   []sim.Point
	segments []float64
	length   float64
}

func interpolateFrames(a, b motionFrame, fraction float64, geometry *motionGeometry) sim.Snapshot {
	snapshot := a.state.Simulation
	snapshot.Vehicles = slices.Clone(snapshot.Vehicles)
	maxTravel := geometry.maxSpeed*float64(b.state.Simulation.Tick-a.state.Simulation.Tick)/sim.TicksPerSecond + 0.1
	for i := range snapshot.Vehicles {
		before := &snapshot.Vehicles[i]
		afterIndex, ok := b.vehicles[before.Pod.ID]
		if !ok {
			continue
		}
		after := b.state.Simulation.Vehicles[afterIndex]
		if position, ok := geometry.interpolatePosition(*before, after, fraction, maxTravel); ok {
			before.Pod.Position = position
			before.Pod.Speed += (after.Pod.Speed - before.Pod.Speed) * fraction
		}
	}
	return snapshot
}

// Follow a known route so interpolation cannot cut across a corner or choose a branch.
func (g *motionGeometry) interpolatePosition(before, after sim.Vehicle, fraction, maxTravel float64) (sim.Point, bool) {
	if before.Pod.Position == after.Pod.Position {
		return before.Pod.Position, true
	}
	for _, route := range [][]sim.Lane{before.Route, after.Route} {
		start, startOK := g.routeOffset(route, before.Pod)
		end, endOK := g.routeOffset(route, after.Pod)
		if !startOK || !endOK || end < start || end-start > maxTravel {
			continue
		}
		distance := start + (end-start)*fraction
		for _, lane := range route {
			geometry, ok := g.lanes[lane.ID]
			if !ok {
				break
			}
			length := geometry.length
			if distance <= length && length > 0 {
				return geometry.position(distance), true
			}
			distance -= length
		}
	}
	return sim.Point{}, false
}

func (g *motionGeometry) routeOffset(route []sim.Lane, pod sim.Pod) (float64, bool) {
	distance := 0.0
	for _, lane := range route {
		geometry, ok := g.lanes[lane.ID]
		if !ok {
			return 0, false
		}
		if pod.LaneID == lane.ID {
			return distance + pod.LaneDistance, true
		}
		if pod.LaneID == "" {
			from := geometry.points[0]
			if math.Hypot(from.X-pod.Position.X, from.Y-pod.Position.Y) < 0.001 {
				return distance, true
			}
		}
		distance += geometry.length
	}
	if pod.LaneID == "" && len(route) > 0 {
		geometry, ok := g.lanes[route[len(route)-1].ID]
		if !ok {
			return 0, false
		}
		to := geometry.points[len(geometry.points)-1]
		if math.Hypot(to.X-pod.Position.X, to.Y-pod.Position.Y) < 0.001 {
			return distance, true
		}
	}
	return 0, false
}

func newMotionGeometry(state session.State) *motionGeometry {
	nodes := make(map[string]sim.Point, len(state.Network.Nodes))
	for _, node := range state.Network.Nodes {
		nodes[node.ID] = node.Position
	}
	geometry := &motionGeometry{
		epoch: state.Epoch, generation: state.Generation,
		lanes: make(map[string]motionLane, len(state.Network.Lanes)),
	}
	for _, lane := range state.Network.Lanes {
		points := []sim.Point{nodes[lane.From], nodes[lane.To]}
		if lane.Control != nil {
			points = make([]sim.Point, 65)
			a, b := nodes[lane.From], nodes[lane.To]
			for i := range points {
				t := float64(i) / float64(len(points)-1)
				u := 1 - t
				points[i] = sim.Point{X: u*u*a.X + 2*u*t*lane.Control.X + t*t*b.X, Y: u*u*a.Y + 2*u*t*lane.Control.Y + t*t*b.Y}
			}
		}
		segments := make([]float64, len(points)-1)
		length := 0.0
		for i := range segments {
			segments[i] = math.Hypot(points[i+1].X-points[i].X, points[i+1].Y-points[i].Y)
			length += segments[i]
		}
		geometry.lanes[lane.ID] = motionLane{points: points, segments: segments, length: length}
		geometry.maxSpeed = max(geometry.maxSpeed, lane.SpeedLimit)
	}
	return geometry
}

func (l motionLane) position(distance float64) sim.Point {
	for i, length := range l.segments {
		if distance <= length && length > 0 {
			t := max(0, distance) / length
			return sim.Point{X: l.points[i].X + t*(l.points[i+1].X-l.points[i].X), Y: l.points[i].Y + t*(l.points[i+1].Y-l.points[i].Y)}
		}
		distance -= length
	}
	return l.points[len(l.points)-1]
}
