package sim

import (
	"math"
)

type couplingApproachPhase uint8

const (
	couplingApproachGrant couplingApproachPhase = iota + 1
	couplingApproachMoving
	couplingApproachWaiting
	couplingApproachAborting
	couplingApproachFinished
)

type couplingApproachState struct {
	context       *couplingApproachContext
	Tick          int64
	Phase         couplingApproachPhase
	Cursor        uint64
	WaitTick      int64
	DeadlineTick  int64
	BrakeComplete bool
}

type couplingApproachCommand struct {
	ID                         string
	Tick                       int64
	PreviousDistance, Distance float64
	PreviousSpeed, Speed       float64
}

type couplingApproachInput struct {
	Context    *couplingApproachContext
	Previous   couplingApproachState
	Simulation *Simulation
	Enabled    bool
}

// The native adapter applies admission and link actions before movement.
// It applies the front command once and releases resources at the global boundary.
type couplingApproachStep struct {
	State               couplingApproachState
	Front               *couplingApproachCommand
	RearID              string
	RearGrantCeiling    int
	SuppressLinkChanges bool
	RequestDrain        bool
	Unlink              bool
	HoldFront           bool
	Ready               bool
	Aborted             bool
	Reason              string
}

// Plan after the native tick increment, before any member moves.
// No native field, link, owner, route, or cabin changes here.
func planCouplingApproach(input couplingApproachInput) (couplingApproachStep, error) {
	c, s, previous := input.Context, input.Simulation, input.Previous
	if c == nil || s == nil || previous.context != c || previous.Tick != s.tick-1 || previous.Tick < c.tick ||
		previous.Tick == math.MaxInt64 || previous.Phase < couplingApproachGrant || previous.Phase >= couplingApproachFinished ||
		previous.BrakeComplete && previous.Phase != couplingApproachAborting {
		return couplingApproachStep{}, couplingMotionInvariant("invalid approach clock or context")
	}
	if previous.WaitTick < -1 || previous.WaitTick > previous.Tick ||
		(previous.WaitTick == -1 && previous.DeadlineTick != -1) ||
		(previous.WaitTick >= 0 && (previous.WaitTick > math.MaxInt64-c.waitTicks || previous.DeadlineTick != previous.WaitTick+c.waitTicks)) {
		return couplingApproachStep{}, couplingMotionInvariant("approach deadline changed")
	}
	step := couplingApproachStep{State: previous, RearID: c.members[1].id, RearGrantCeiling: c.ceiling, SuppressLinkChanges: true}
	step.State.Tick++
	front, rear := s.findVehicle(c.members[0].id), s.findVehicle(c.members[1].id)
	if front == nil || rear == nil {
		return couplingApproachStep{}, couplingMotionInvariant("approach member disappeared")
	}
	if previous.Phase == couplingApproachAborting {
		return c.brakeApproach(step, s, front, rear)
	}
	if reason := c.changed(s, front, rear, input.Enabled); reason != "" {
		step.Reason = reason
		return c.brakeApproach(step, s, front, rear)
	}
	if previous.Phase == couplingApproachWaiting {
		return c.waitApproach(step, s, front, rear)
	}
	if err := c.checkScheduledPose(previous, front); err != nil {
		return couplingApproachStep{}, err
	}
	if previous.Phase == couplingApproachGrant {
		frontier, err := couplingApproachOwnedFrontier(s, front)
		if err != nil {
			return couplingApproachStep{}, err
		}
		if frontier < c.target {
			// E is still the ordinary stopping frontier. No partner hold starts.
			if frontier != c.start {
				step.Reason = "partial grant cannot cover the rest segment"
				return c.brakeApproach(step, s, front, rear)
			}
			return step, nil
		}
	}
	step.State.Phase = couplingApproachMoving
	step.State.Cursor++
	distance, speed, err := c.schedule.sample(step.State.Cursor)
	if err != nil {
		return couplingApproachStep{}, err
	}
	command, err := couplingApproachFrontCommand(s, front, distance, speed, step.State.Tick)
	if err != nil {
		step.Reason = "scheduled motion lost its owned stopping frontier"
		return c.brakeApproach(step, s, front, rear)
	}
	step.Front = &command
	if step.State.Cursor == c.schedule.ticks {
		if distance != c.target || speed != 0 {
			return couplingApproachStep{}, couplingMotionInvariant("approach schedule missed its exact rest target")
		}
		step.State.Phase = couplingApproachWaiting
		if err := c.startApproachWait(&step.State); err != nil {
			return couplingApproachStep{}, err
		}
		step.RequestDrain = true
	}
	return step, nil
}

func (c *couplingApproachContext) checkScheduledPose(state couplingApproachState, front *vehicle) error {
	distance, speed, err := c.schedule.sample(state.Cursor)
	if err != nil || front.distance != distance || front.Pod.Speed != speed ||
		(state.Phase == couplingApproachGrant && state.Cursor != 0) {
		return couplingMotionInvariant("actual front differs from its approach schedule")
	}
	return nil
}

func (c *couplingApproachContext) startApproachWait(state *couplingApproachState) error {
	if state.WaitTick != -1 {
		return nil
	}
	if state.Tick > math.MaxInt64-c.waitTicks {
		return couplingMotionInvariant("approach wait clock overflows")
	}
	state.WaitTick, state.DeadlineTick = state.Tick, state.Tick+c.waitTicks
	return nil
}

func (c *couplingApproachContext) waitApproach(step couplingApproachStep, s *Simulation, front, rear *vehicle) (couplingApproachStep, error) {
	if front.distance != c.target || front.Pod.Speed != 0 || step.State.WaitTick < 0 {
		return couplingApproachStep{}, couplingMotionInvariant("approach hold differs from its actual rest target")
	}
	if step.State.Tick <= step.State.DeadlineTick && rear.link.leader == 0 && front.follower == 0 && rear.Pod.Speed == 0 && rear.distance == c.rearTarget {
		if _, err := planCouplingReservation(c.formationInput(s, front, rear)); err == nil {
			step.Ready, step.HoldFront, step.State.Phase = true, true, couplingApproachFinished
			return step, nil
		}
	}
	if step.State.Tick > step.State.DeadlineTick {
		step.Reason = "identified partner wait expired"
		return c.brakeApproach(step, s, front, rear)
	}
	step.HoldFront = true
	if rear.link.leader != 0 {
		step.RequestDrain = true
		step.Unlink = !s.holdsPending(rear)
	}
	return step, nil
}

func (c *couplingApproachContext) brakeApproach(step couplingApproachStep, s *Simulation, front, rear *vehicle) (couplingApproachStep, error) {
	step.State.Phase = couplingApproachAborting
	step.HoldFront, step.Ready, step.RequestDrain, step.Unlink = false, false, false, false
	step.State.Cursor = 0
	if front.Pod.Activity != Traveling {
		return couplingApproachStep{}, couplingMotionInvariant("moving approach left ordinary travel before braking")
	}
	if !step.State.BrakeComplete && front.Pod.Speed == 0 {
		// An already stopped invalidation needs no extra held publication.
		step.State.BrakeComplete = true
	}
	if !step.State.BrakeComplete {
		speed := max(0, front.Pod.Speed-acceleration/TicksPerSecond)
		distance := front.distance + speed/TicksPerSecond
		command, err := couplingApproachFrontCommand(s, front, distance, speed, step.State.Tick)
		if err != nil {
			return couplingApproachStep{}, err
		}
		step.Front = &command
		step.State.BrakeComplete = speed == 0
	}
	step.Aborted = step.State.BrakeComplete
	if rear.link.leader != 0 {
		step.RequestDrain = true
		step.Unlink = !s.holdsPending(rear)
	} else if step.State.BrakeComplete {
		step.State.Phase = couplingApproachFinished
	}
	return step, nil
}

func couplingApproachFrontCommand(s *Simulation, front *vehicle, distance, speed float64, tick int64) (couplingApproachCommand, error) {
	var command couplingApproachCommand
	if !finite(distance) || !finite(speed) || speed < 0 || distance < front.distance ||
		math.Abs(speed-front.Pod.Speed) > acceleration/TicksPerSecond+conflictSlack || distance != front.distance+speed/TicksPerSecond {
		return command, couplingMotionInvariant("approach command violates ordinary Euler motion")
	}
	frontier, err := couplingApproachOwnedFrontier(s, front)
	if err != nil {
		return command, err
	}
	if distance+stoppingDistance(speed)+speed/TicksPerSecond > frontier ||
		speed > front.blocks.speedBeforeLane(front.blocks.routeLane(front.blockIndex), front.distance, speed) {
		return command, couplingMotionInvariant("approach command exceeds its actual stopping or lane bound")
	}
	lane := front.blocks.routeLane(front.blockIndex)
	if speed > front.Route[lane].SpeedLimit {
		return command, couplingMotionInvariant("approach command exceeds its authored lane limit")
	}
	return couplingApproachCommand{ID: front.Pod.ID, Tick: tick, PreviousDistance: front.distance, Distance: distance, PreviousSpeed: front.Pod.Speed, Speed: speed}, nil
}
