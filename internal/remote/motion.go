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

type motionFrame struct {
	state session.State
	at    time.Time
}

// Motion buffers authoritative snapshots for display only. Use it on the render thread.
// It delays the map by 150 ms and never predicts past received movement.
type Motion struct{ frames []motionFrame }

// Observe adds a new revision. Playback discontinuities discard old motion.
func (m *Motion) Observe(state session.State, at time.Time) {
	if len(m.frames) > 0 {
		previous := m.frames[len(m.frames)-1]
		if state.Epoch == previous.state.Epoch && state.Revision <= previous.state.Revision {
			return
		}
		a, b := previous.state.Simulation, state.Simulation
		if state.Epoch != previous.state.Epoch || b.Tick < a.Tick || b.Submitted < a.Submitted || b.Paused != a.Paused || state.Speed != previous.state.Speed || b.Demo != a.Demo || at.Sub(previous.at) > motionGap {
			m.frames = nil
		}
	}
	m.frames = append(m.frames, motionFrame{state: state, at: at})
	if len(m.frames) > 32 {
		m.frames = slices.Clone(m.frames[len(m.frames)-32:])
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
			return interpolate(previous.state, next.state, fraction)
		}
	}
	return latest.state.Simulation
}

func interpolate(a, b session.State, fraction float64) sim.Snapshot {
	snapshot := a.Simulation
	snapshot.Vehicles = slices.Clone(snapshot.Vehicles)
	maxSpeed := 0.0
	for _, lane := range a.Network.Lanes {
		maxSpeed = max(maxSpeed, lane.SpeedLimit)
	}
	maxTravel := maxSpeed*float64(b.Simulation.Tick-a.Simulation.Tick)/sim.TicksPerSecond + 0.1
	for i := range snapshot.Vehicles {
		before := &snapshot.Vehicles[i]
		for _, after := range b.Simulation.Vehicles {
			if before.Pod.ID != after.Pod.ID {
				continue
			}
			if position, ok := interpolatePosition(a.Network, *before, after, fraction, maxTravel); ok {
				before.Pod.Position = position
				before.Pod.Speed += (after.Pod.Speed - before.Pod.Speed) * fraction
			}
			break
		}
	}
	return snapshot
}

// Follow a known route so interpolation cannot cut across a corner or choose a branch.
func interpolatePosition(network sim.Network, before, after sim.Vehicle, fraction, maxTravel float64) (sim.Point, bool) {
	if before.Pod.Position == after.Pod.Position {
		return before.Pod.Position, true
	}
	for _, route := range [][]sim.Lane{before.Route, after.Route} {
		start, startOK := routeOffset(network, route, before.Pod)
		end, endOK := routeOffset(network, route, after.Pod)
		if !startOK || !endOK || end < start || end-start > maxTravel {
			continue
		}
		distance := start + (end-start)*fraction
		for _, lane := range route {
			length := network.Length(lane)
			if distance <= length && length > 0 {
				from, _ := network.Node(lane.From)
				to, _ := network.Node(lane.To)
				ratio := distance / length
				return sim.Point{X: from.Position.X + (to.Position.X-from.Position.X)*ratio, Y: from.Position.Y + (to.Position.Y-from.Position.Y)*ratio}, true
			}
			distance -= length
		}
	}
	return sim.Point{}, false
}

func routeOffset(network sim.Network, route []sim.Lane, pod sim.Pod) (float64, bool) {
	distance := 0.0
	for _, lane := range route {
		if pod.LaneID == lane.ID {
			return distance + pod.LaneDistance, true
		}
		if pod.LaneID == "" {
			from, _ := network.Node(lane.From)
			if math.Hypot(from.Position.X-pod.Position.X, from.Position.Y-pod.Position.Y) < 0.001 {
				return distance, true
			}
		}
		distance += network.Length(lane)
	}
	if pod.LaneID == "" && len(route) > 0 {
		to, _ := network.Node(route[len(route)-1].To)
		if math.Hypot(to.Position.X-pod.Position.X, to.Position.Y-pod.Position.Y) < 0.001 {
			return distance, true
		}
	}
	return 0, false
}
