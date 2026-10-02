package sim

import (
	"errors"
	"fmt"
	"math"
)

// These constants fix the numeric profile of compact-v1. The caller must prove
// a straight, plain fixed-entry region, four-meter bodies, and resource ownership.
const (
	compactQueueBodyLength      = 4.0
	compactQueueStandstillGap   = 6.01
	compactQueueOrdinaryGap     = 12.01
	compactQueueReactionSeconds = 0.5
	compactQueueAcceleration    = 2.0
	compactQueueSpeedLimit      = 2.5
	compactQueueTickSeconds     = 1.0 / 60.0
	compactQueueMaxMembers      = 4
	compactQueueRecoveryPerPair = compactQueueOrdinaryGap - compactQueueStandstillGap
)

// compactQueueState uses reference positions on one shared route coordinate.
// stopBoundary is the last owned stopping position, capped at the plain frontier.
type compactQueueState struct {
	position, speed, stopBoundary float64
}

type compactQueueBounds struct {
	start, frontier float64
}

// compactQueueEnvelope includes reaction time and positive relative braking.
func compactQueueEnvelope(followerSpeed, leaderSpeed float64) float64 {
	relative := (followerSpeed - leaderSpeed) * (followerSpeed + leaderSpeed) / (2 * compactQueueAcceleration)
	return compactQueueStandstillGap + compactQueueReactionSeconds*followerSpeed + max(0, relative)
}

func compactQueueStoppingDistance(speed float64) float64 {
	return speed * speed / (2 * compactQueueAcceleration)
}

// compactQueuePlan plans one tick from an immutable head-to-tail snapshot.
// headSpeed is the caller's next speed, not a desired speed that can be clipped.
// Each follower takes the largest feasible speed within its acceleration bounds.
// Errors return no partial plan. No endpoint snap or instant stop is applied.
func compactQueuePlan(states []compactQueueState, bounds compactQueueBounds, headSpeed float64) ([]compactQueueState, error) {
	if err := compactQueueValidate(states, bounds); err != nil {
		return nil, err
	}
	lo, hi := compactQueueSpeedRange(states[0].speed)
	if !compactQueueFinite(headSpeed) || headSpeed < lo || headSpeed > hi {
		return nil, errors.New("compact queue: head speed outside one-tick bounds")
	}
	planned := make([]compactQueueState, len(states))
	planned[0] = compactQueueStep(states[0], headSpeed)
	if !compactQueueFits(planned[0], bounds, nil) || !compactQueueRecoveryFits(planned[0], len(states), bounds) {
		return nil, errors.New("compact queue: head has insufficient owned stop or recovery room")
	}
	for i := 1; i < len(states); i++ {
		lo, hi = compactQueueSpeedRange(states[i].speed)
		leader := &planned[i-1]
		if !compactQueueFits(compactQueueStep(states[i], lo), bounds, leader) {
			return nil, fmt.Errorf("compact queue: member %d cannot brake within the next-state envelope", i)
		}
		// Feasibility decreases monotonically with speed. Keep a feasible lower
		// endpoint, so rounding cannot select a speed above the safe root.
		if !compactQueueFits(compactQueueStep(states[i], hi), bounds, leader) {
			// Nonnegative float representations have the same order as their
			// values. Searching the bits reaches adjacent values even near zero.
			lowBits, highBits := math.Float64bits(lo), math.Float64bits(hi)
			for highBits-lowBits > 1 {
				midBits := lowBits + (highBits-lowBits)/2
				mid := math.Float64frombits(midBits)
				if compactQueueFits(compactQueueStep(states[i], mid), bounds, leader) {
					lowBits = midBits
				} else {
					highBits = midBits
				}
			}
			hi = math.Float64frombits(lowBits)
		}
		planned[i] = compactQueueStep(states[i], hi)
	}
	if err := compactQueueValidate(planned, bounds); err != nil {
		return nil, fmt.Errorf("compact queue: invalid planned state: %w", err)
	}
	return planned, nil
}

// compactQueueRecoveryTargets builds stopped ordinary gaps, tail first.
// Let S_i = x_i + D(v_i). The compact envelope proves S_(i-1)-S_i >= 6.01.
// Thus z_i=max(S_i,z_(i+1)+12.01) gives z_head <= S_head+6*(n-1).
// The headroom guard keeps every target in the plain entry region. Targets are
// forward only and no earlier than each pod's current continuous stopping point.
// This constructs destinations; it does not authorize a discharge or a snap.
func compactQueueRecoveryTargets(states []compactQueueState, bounds compactQueueBounds) ([]float64, error) {
	if err := compactQueueValidate(states, bounds); err != nil {
		return nil, err
	}
	targets := make([]float64, len(states))
	if err := compactQueueBuildRecoveryTargets(states, bounds, targets); err != nil {
		return nil, err
	}
	return targets, nil
}

// A nil output checks the actual rounded targets without allocating a slice.
func compactQueueBuildRecoveryTargets(states []compactQueueState, bounds compactQueueBounds, targets []float64) error {
	var target float64
	for i := len(states) - 1; i >= 0; i-- {
		stop := states[i].position + compactQueueStoppingDistance(states[i].speed)
		if i+1 < len(states) {
			followerTarget := target
			target = max(stop, followerTarget+compactQueueOrdinaryGap)
			// Round upward if addition rounded the actual target gap downward.
			if target-followerTarget < compactQueueOrdinaryGap {
				target = math.Nextafter(target, math.Inf(1))
			}
		} else {
			target = stop
		}
		if !compactQueueFinite(target) || target > bounds.frontier {
			return errors.New("compact queue: recovery target outside the plain frontier")
		}
		if targets != nil {
			targets[i] = target
		}
	}
	return nil
}

func compactQueueValidate(states []compactQueueState, bounds compactQueueBounds) error {
	if len(states) < 2 || len(states) > compactQueueMaxMembers {
		return errors.New("compact queue: expected two to four members")
	}
	if !compactQueueFinite(bounds.start) || !compactQueueFinite(bounds.frontier) || bounds.start > bounds.frontier {
		return errors.New("compact queue: invalid plain entry bounds")
	}
	for i, state := range states {
		var leader *compactQueueState
		if i != 0 {
			leader = &states[i-1]
		}
		if !compactQueueFits(state, bounds, leader) {
			return fmt.Errorf("compact queue: invalid member %d or neighbor envelope", i)
		}
	}
	if !compactQueueRecoveryFits(states[0], len(states), bounds) {
		return errors.New("compact queue: insufficient future recovery room")
	}
	return compactQueueBuildRecoveryTargets(states, bounds, nil)
}

func compactQueueFits(state compactQueueState, bounds compactQueueBounds, leader *compactQueueState) bool {
	if !compactQueueFinite(state.position) || !compactQueueFinite(state.speed) || !compactQueueFinite(state.stopBoundary) ||
		state.speed < 0 || state.speed > compactQueueSpeedLimit || state.position < bounds.start ||
		state.stopBoundary > bounds.frontier || state.position > state.stopBoundary {
		return false
	}
	stop := state.position + compactQueueStoppingDistance(state.speed)
	if !compactQueueFinite(stop) || stop > state.stopBoundary {
		return false
	}
	if leader != nil {
		gap := leader.position - state.position
		return compactQueueFinite(gap) && gap >= compactQueueEnvelope(state.speed, leader.speed)
	}
	return true
}

func compactQueueRecoveryFits(head compactQueueState, members int, bounds compactQueueBounds) bool {
	stop := head.position + compactQueueStoppingDistance(head.speed)
	end := stop + compactQueueRecoveryPerPair*float64(members-1)
	return compactQueueFinite(end) && end <= bounds.frontier
}

func compactQueueSpeedRange(speed float64) (float64, float64) {
	step := compactQueueAcceleration * compactQueueTickSeconds
	return max(0, speed-step), min(compactQueueSpeedLimit, speed+step)
}

func compactQueueStep(state compactQueueState, speed float64) compactQueueState {
	state.position += speed * compactQueueTickSeconds
	state.speed = speed
	return state
}

func compactQueueFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
