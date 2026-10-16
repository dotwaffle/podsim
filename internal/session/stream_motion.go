package session

import (
	"errors"
	"math"

	"github.com/dotwaffle/podsim/internal/sim"
)

// podMotion replaces only the continuous position and speed of a pod.
// Lane identity, activity and station transitions still replace the full pod.
type podMotion struct {
	Position     sim.Point `json:"position"`
	LaneDistance float64   `json:"laneDistance"`
	Speed        float64   `json:"speed"`
}

func motionOf(p sim.Pod) podMotion {
	return podMotion{Position: p.Position, LaneDistance: p.LaneDistance, Speed: p.Speed}
}

func (m podMotion) replace(p *sim.Pod) {
	p.Position, p.LaneDistance, p.Speed = m.Position, m.LaneDistance, m.Speed
}

func sameStationaryPod(a, b sim.Pod) bool {
	zero := podMotion{}
	zero.replace(&a)
	zero.replace(&b)
	return a == b
}

func validateMotionDelta(v VehicleDelta) error {
	if v.Motion == nil {
		return nil
	}
	if v.Pod != nil {
		return errors.New("conflicting pod and motion replacements")
	}
	m := v.Motion.Value
	finite := func(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }
	if !finite(m.Position.X) || !finite(m.Position.Y) || !finite(m.LaneDistance) || !finite(m.Speed) || m.LaneDistance < 0 || m.Speed < 0 {
		return errors.New("invalid motion replacement")
	}
	return nil
}

// scanMotionReplacement requires a complete replacement, including explicit
// zero values. The ordinary typed decode cannot distinguish absent numbers.
func scanMotionReplacement(raw []byte) error {
	members, err := boardingMembers(raw)
	if err != nil || len(members) != 1 || members["value"] == nil {
		return errors.New("motion replacement needs exactly one value")
	}
	var value struct {
		Position *struct {
			X *float64 `json:"x"`
			Y *float64 `json:"y"`
		} `json:"position"`
		LaneDistance *float64 `json:"laneDistance"`
		Speed        *float64 `json:"speed"`
	}
	if err := decodeStreamJSON(members["value"], &value); err != nil || value.Position == nil || value.Position.X == nil || value.Position.Y == nil || value.LaneDistance == nil || value.Speed == nil {
		return errors.New("invalid motion replacement")
	}
	motion := podMotion{Position: sim.Point{X: *value.Position.X, Y: *value.Position.Y}, LaneDistance: *value.LaneDistance, Speed: *value.Speed}
	return validateMotionDelta(VehicleDelta{Motion: &Replacement[podMotion]{Value: motion}})
}
