package sim

import (
	"errors"
	"fmt"
	"math"
)

// compactQueueRecovery retains fixed destinations and a positive landing speed.
// It does not grant track. Each state's stopBoundary must still prove ownership.
type compactQueueRecovery struct {
	bounds        compactQueueBounds
	targets       []float64
	landingSpeeds []float64
}

// compactQueueRecoveryAdmission proves recovery before a group can form or grow.
// The symbolic six-meter budget alone cannot admit an exact-frontier target.
func compactQueueRecoveryAdmission(states []compactQueueState, bounds compactQueueBounds) (compactQueueRecovery, error) {
	targets, err := compactQueueRecoveryTargets(states, bounds)
	if err != nil {
		return compactQueueRecovery{}, err
	}
	recovery := compactQueueRecovery{bounds: bounds, targets: targets, landingSpeeds: make([]float64, len(states))}
	for i, state := range states {
		if state.position == targets[i] && state.speed == 0 {
			continue
		}
		recovery.landingSpeeds[i], err = compactQueueLandingSpeed(state, targets[i])
		if err != nil {
			return compactQueueRecovery{}, fmt.Errorf("compact queue: member %d recovery landing: %w", i, err)
		}
	}
	if err := compactQueueRecoveryValidate(states, recovery); err != nil {
		return compactQueueRecovery{}, err
	}
	return recovery, nil
}

// compactQueueRecoverablePlan retains constructive recovery during compact motion.
// Callers must use this admission guard before they change group membership.
func compactQueueRecoverablePlan(states []compactQueueState, bounds compactQueueBounds, headSpeed float64) ([]compactQueueState, error) {
	if _, err := compactQueueRecoveryAdmission(states, bounds); err != nil {
		return nil, err
	}
	planned, err := compactQueuePlan(states, bounds, headSpeed)
	if err != nil {
		return nil, err
	}
	if _, err := compactQueueRecoveryAdmission(planned, bounds); err != nil {
		return nil, fmt.Errorf("compact queue: planned recovery is infeasible: %w", err)
	}
	return planned, nil
}

// compactQueueRecoveryStep consumes reserved recovery room without reserving it again.
// It brakes all other members and moves the first unfinished member toward its
// fixed target. A legal positive-speed landing ends with a separate braking tick.
// Errors return no partial plan and leave both input snapshots unchanged.
func compactQueueRecoveryStep(states []compactQueueState, recovery compactQueueRecovery) ([]compactQueueState, bool, error) {
	if err := compactQueueRecoveryValidate(states, recovery); err != nil {
		return nil, false, err
	}
	planned := make([]compactQueueState, len(states))
	active := -1
	for i, state := range states {
		if active == -1 && (state.position != recovery.targets[i] || state.speed != 0) {
			active = i
		}
		lo, _ := compactQueueSpeedRange(state.speed)
		planned[i] = compactQueueStep(state, lo)
	}
	if active != -1 && len(states) > 1 && states[active].position != recovery.targets[active] {
		state, target := states[active], recovery.targets[active]
		lo, hi := compactQueueSpeedRange(state.speed)
		fits := func(speed float64) bool {
			return compactQueueRecoveryCandidate(state, speed, target, recovery.bounds)
		}
		if !fits(lo) {
			return nil, false, errors.New("compact queue: recovery cannot brake within its fixed target")
		}
		planned[active] = compactQueueStep(state, compactQueueLargestSpeed(lo, hi, fits))
	}
	if err := compactQueueRecoveryValidate(planned, recovery); err != nil {
		return nil, false, fmt.Errorf("compact queue: invalid recovery transition: %w", err)
	}
	done := true
	for i, state := range planned {
		done = done && state.position == recovery.targets[i] && state.speed == 0
	}
	return planned, done, nil
}

// compactQueueSingletonRecovery stops an unlinked head without extra recovery room.
// Its destination is the exact endpoint of legal discrete maximum braking.
func compactQueueSingletonRecovery(state compactQueueState, bounds compactQueueBounds) (compactQueueRecovery, error) {
	if !compactQueueBoundsValid(bounds) || !compactQueueFits(state, bounds, nil) {
		return compactQueueRecovery{}, errors.New("compact queue: invalid singleton state")
	}
	end := compactQueueBrakingEnd(state)
	recovery := compactQueueRecovery{bounds: bounds, targets: []float64{end.position}, landingSpeeds: []float64{0}}
	if err := compactQueueRecoveryValidate([]compactQueueState{state}, recovery); err != nil {
		return compactQueueRecovery{}, err
	}
	return recovery, nil
}

// compactQueueRecoveryTickBound derives a conservative finite completion bound.
// A positive landing speed guarantees at least half its tick distance until the
// final landing interval. Each member then needs at most one full braking run.
func compactQueueRecoveryTickBound(states []compactQueueState, recovery compactQueueRecovery) (uint64, error) {
	if err := compactQueueRecoveryValidate(states, recovery); err != nil {
		return 0, err
	}
	brakingTicks := uint64(0)
	for speed := compactQueueSpeedLimit; speed > 0; {
		speed, _ = compactQueueSpeedRange(speed)
		brakingTicks++
	}
	bound := brakingTicks + 1
	for i, state := range states {
		bound += brakingTicks + 1
		if len(states) == 1 || state.position == recovery.targets[i] {
			continue
		}
		steps := math.Ceil(2 * (recovery.targets[i] - state.position) / (recovery.landingSpeeds[i] * compactQueueTickSeconds))
		if !compactQueueFinite(steps) || steps >= float64(math.MaxUint64-bound) {
			return 0, errors.New("compact queue: finite recovery bound cannot be represented")
		}
		bound += uint64(steps) + 1
	}
	return bound, nil
}

// compactQueueHoldingStep checks future capacity before a head enters the frontier.
// Capacity one removes the anticipatory hold, retaining the physical speed and
// owned-track rules. Late holds fail unchanged. This helper does not form a link.
func compactQueueHoldingStep(state compactQueueState, bounds compactQueueBounds, capacity int, nextSpeed float64) (compactQueueState, error) {
	if err := compactQueueHoldingValidate(state, bounds, capacity); err != nil {
		return state, err
	}
	lo, hi := compactQueueSpeedRange(state.speed)
	if !compactQueueFinite(nextSpeed) || nextSpeed < lo || nextSpeed > hi {
		return state, errors.New("compact queue: holding speed outside one-tick bounds")
	}
	planned := compactQueueStep(state, nextSpeed)
	if err := compactQueueHoldingValidate(planned, bounds, capacity); err != nil {
		return state, err
	}
	return planned, nil
}

func compactQueueHoldingValidate(state compactQueueState, bounds compactQueueBounds, capacity int) error {
	if capacity < 1 || capacity > compactQueueMaxMembers || !compactQueueBoundsValid(bounds) || !compactQueueFits(state, bounds, nil) {
		return errors.New("compact queue: invalid holding state or capacity")
	}
	if capacity == 1 {
		return nil
	}
	target := state.position + compactQueueStoppingDistance(state.speed) + compactQueueRecoveryPerPair*float64(capacity-1)
	if !compactQueueFinite(target) || target > bounds.frontier {
		return errors.New("compact queue: insufficient anticipatory recovery room")
	}
	_, err := compactQueueLandingSpeed(state, target)
	return err
}

func compactQueueRecoveryValidate(states []compactQueueState, recovery compactQueueRecovery) error {
	if len(states) < 1 || len(states) > compactQueueMaxMembers || len(recovery.targets) != len(states) ||
		len(recovery.landingSpeeds) != len(states) || !compactQueueBoundsValid(recovery.bounds) {
		return errors.New("compact queue: invalid retained recovery dimensions")
	}
	var leader *compactQueueState
	for i, state := range states {
		if !compactQueueFits(state, recovery.bounds, leader) {
			return fmt.Errorf("compact queue: invalid retained member %d", i)
		}
		if err := compactQueueRecoveryMember(state, recovery, i); err != nil {
			return fmt.Errorf("compact queue: invalid retained member %d: %w", i, err)
		}
		leader = &states[i]
	}
	return nil
}

func compactQueueRecoveryMember(state compactQueueState, recovery compactQueueRecovery, i int) error {
	target, landing := recovery.targets[i], recovery.landingSpeeds[i]
	if !compactQueueFinite(target) || target < state.position || target > recovery.bounds.frontier ||
		!compactQueueFinite(landing) || landing < 0 || landing > compactQueueAcceleration*compactQueueTickSeconds {
		return errors.New("invalid target or landing speed")
	}
	if i != 0 && recovery.targets[i-1]-target < compactQueueOrdinaryGap {
		return errors.New("recovery destinations violate ordinary stopped spacing")
	}
	end := compactQueueBrakingEnd(state)
	if !compactQueueFinite(end.position) || end.position > target {
		return errors.New("discrete braking passes the recovery target")
	}
	if len(recovery.targets) == 1 {
		if landing != 0 || end.position != target {
			return errors.New("singleton target is not its discrete braking endpoint")
		}
		return nil
	}
	if landing == 0 {
		if state.position != target || state.speed != 0 {
			return errors.New("unfinished recovery has no positive landing speed")
		}
		return nil
	}
	if target >= state.stopBoundary || compactQueueStoppingDistance(landing) > state.stopBoundary-target ||
		target+compactQueueStoppingDistance(landing) > state.stopBoundary ||
		!compactQueueProgressFits(state.position, target, landing) {
		return errors.New("recovery landing allowance is not owned or cannot advance")
	}
	return nil
}

// A landing speed w<=a*dt can advance w*dt and then stop in one tick.
// Its continuous stopping allowance D(w) must remain owned past the destination.
func compactQueueLandingSpeed(state compactQueueState, target float64) (float64, error) {
	margin := state.stopBoundary - target
	if !compactQueueFinite(target) || !compactQueueFinite(margin) || margin <= 0 {
		return 0, errors.New("no positive owned landing allowance")
	}
	w := compactQueueAcceleration * compactQueueTickSeconds
	if compactQueueStoppingDistance(w) > margin {
		w = math.Sqrt(2 * compactQueueAcceleration * margin)
	}
	for compactQueueStoppingDistance(w) > margin || target+compactQueueStoppingDistance(w) > state.stopBoundary {
		w = math.Nextafter(w, 0)
	}
	if !compactQueueFinite(w) || w <= 0 || !compactQueueProgressFits(state.position, target, w) {
		return 0, errors.New("landing allowance cannot produce a representable forward step")
	}
	return w, nil
}

func compactQueueRecoveryCandidate(state compactQueueState, speed, target float64, bounds compactQueueBounds) bool {
	planned := compactQueueStep(state, speed)
	if !compactQueueFits(planned, bounds, nil) || planned.position > target {
		return false
	}
	return compactQueueBrakingEnd(planned).position <= target
}

func compactQueueBrakingEnd(state compactQueueState) compactQueueState {
	for state.speed > 0 {
		lo, _ := compactQueueSpeedRange(state.speed)
		state = compactQueueStep(state, lo)
	}
	return state
}

func compactQueueLargestSpeed(lo, hi float64, fits func(float64) bool) float64 {
	if fits(hi) {
		return hi
	}
	lowBits, highBits := math.Float64bits(lo), math.Float64bits(hi)
	for highBits-lowBits > 1 {
		midBits := lowBits + (highBits-lowBits)/2
		if fits(math.Float64frombits(midBits)) {
			lowBits = midBits
		} else {
			highBits = midBits
		}
	}
	return math.Float64frombits(lowBits)
}

func compactQueueProgressFits(position, target, speed float64) bool {
	magnitude := max(math.Abs(position), math.Abs(target))
	ulp := math.Nextafter(magnitude, math.Inf(1)) - magnitude
	travel := speed * compactQueueTickSeconds
	return compactQueueFinite(ulp) && travel > 0 && travel >= ulp
}

func compactQueueBoundsValid(bounds compactQueueBounds) bool {
	return compactQueueFinite(bounds.start) && compactQueueFinite(bounds.frontier) && bounds.start <= bounds.frontier
}
