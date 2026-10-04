package sim

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"sort"
)

var errCouplingMotionInvariant = errors.New("coupling motion invariant failed")

type couplingMotionState struct {
	context               *couplingMotionContext
	Tick                  int64
	Elapsed               uint64
	Phase                 couplingReservationPhase
	Leg                   int
	Cursor                uint64
	Dwell                 int
	Distances, Speeds     [2]float64
	Positions, Directions [2]Point
	Lanes                 [2]string
	Cells                 [2]int
	Finished              bool
}

type couplingOwnerWrite struct {
	Resource       resource
	Expected, Next resourceOwner
	Role           couplingMotionRole
}

type couplingMotionInput struct {
	Context  *couplingMotionContext
	Previous couplingMotionState
	Owners   *couplingMotionOwnerView
	Foreign  []couplingForeignSweep
}

type couplingCabinMotion struct {
	Pod          Pod
	Riders       []Request
	Stops        []string
	Boardings    []RiderBoarding
	RiddenMeters float64
	RelocatingTo string
	RouteVersion uint64
}

type couplingMotionStep struct {
	State     couplingMotionState
	Members   [2]couplingCabinMotion
	Samples   [2]MotionSample
	Writes    []couplingOwnerWrite
	Bodies    [2]CouplingRectangle
	Connector *CouplingRectangle
}

func couplingMotionInvariant(reason string) error {
	return fmt.Errorf("%s: %w", reason, errCouplingMotionInvariant)
}

// Initial writes are a proposal. No caller owner map is changed.
func initialCouplingMotion(c *couplingMotionContext, current couplingReservationInput) (couplingMotionStep, error) {
	var step couplingMotionStep
	if c == nil {
		return step, couplingDenied("missing immutable motion context")
	}
	commitment := c.reservation
	commitment.waiting = current.Waiting
	if _, err := revalidateCouplingReservation(commitment, current); err != nil {
		return step, err
	}
	owners := current.Owners
	state, err := c.stateAt(0)
	if err != nil {
		return couplingMotionStep{}, err
	}
	for i, claim := range c.claims {
		if owners[claim.Resource] != claim.Expected {
			return couplingMotionStep{}, couplingDenied("initial motion claim changed")
		}
		step.Writes = append(step.Writes, couplingOwnerWrite{Resource: claim.Resource, Expected: claim.Expected, Next: c.dependencyOwner(c.dependencies[i], state), Role: couplingDependencyRole(c.dependencies[i], c.dependencyOwner(c.dependencies[i], state))})
	}
	for _, claim := range c.reservation.PreservedClaims {
		if owners[claim.Resource] != claim.Expected {
			return couplingMotionStep{}, couplingDenied("initial receiving claim changed")
		}
	}
	step.State = state
	step.Members = c.membersAt(state)
	step.Bodies, step.Connector, err = c.motionBodiesAt(state)
	if err != nil {
		return couplingMotionStep{}, err
	}
	return step, nil
}

// A failed committed proof returns no motion or owner publication.
func planCouplingMotion(input couplingMotionInput) (couplingMotionStep, error) {
	var result couplingMotionStep
	c := input.Context
	if c == nil || input.Previous.context != c || input.Previous.Finished || input.Previous.Elapsed >= c.ticks {
		return result, couplingMotionInvariant("invalid committed context or terminal cursor")
	}
	previous, err := c.stateAt(input.Previous.Elapsed)
	if err != nil || previous != input.Previous {
		return result, couplingMotionInvariant("previous motion differs from its exact certificate")
	}
	if ownerErr := c.checkMotionOwners(previous, input.Owners); ownerErr != nil {
		return result, ownerErr
	}
	next, err := c.stateAt(previous.Elapsed + 1)
	if err != nil {
		return result, couplingMotionInvariant("next motion cannot be represented")
	}
	if motionErr := c.checkTickMotion(previous, next); motionErr != nil {
		return result, motionErr
	}
	if foreignErr := c.checkForeignSweeps(previous, next, input.Foreign); foreignErr != nil {
		return result, foreignErr
	}
	first, end := c.ownerEventCursor(previous.Elapsed), c.ownerEventCursor(next.Elapsed)
	for _, event := range c.events[first:end] {
		if input.Owners.owners[event.write.Resource] != event.write.Expected {
			return couplingMotionStep{}, couplingMotionInvariant("affected owner differs from sealed atomic release proof")
		}
		result.Writes = append(result.Writes, event.write)
	}
	result.State = next
	result.Members = c.membersAt(next)
	result.Bodies, result.Connector, err = c.motionBodiesAt(next)
	if err != nil {
		return couplingMotionStep{}, err
	}
	for i, m := range &c.reservation.members {
		result.Samples[i] = MotionSample{ID: m.Vehicle.Pod.ID, Class: CompactClass, DistanceMeters: next.Distances[i] - previous.Distances[i], StartSpeed: previous.Speeds[i], EndSpeed: next.Speeds[i]}
	}
	return result, nil
}

func (c *couplingMotionContext) stateAt(elapsed uint64) (couplingMotionState, error) {
	state := couplingMotionState{context: c, Elapsed: elapsed, Leg: -1}
	if elapsed > math.MaxInt64 {
		return state, couplingMotionInvariant("motion clock cannot be represented")
	}
	ticks := int64(elapsed)
	if elapsed > c.ticks || c.reservation.tick < 0 || c.reservation.tick > math.MaxInt64-ticks {
		return state, couplingMotionInvariant("motion cursor exceeds its finite schedule")
	}
	state.Tick = c.reservation.tick + ticks
	remaining := elapsed
	if c.initialDwell > 0 {
		if remaining < uint64(c.initialDwell) {
			state.Phase, state.Distances, state.Dwell = c.initialPhase, c.initialDistances, c.initialDwell-int(remaining)
			return c.poseState(state)
		}
		remaining -= uint64(c.initialDwell)
	}
	for i := c.firstLeg; i < len(c.legs); i++ {
		leg := c.legs[i]
		if remaining <= leg.ticks {
			state.Phase, state.Leg, state.Cursor = leg.phase, i, remaining
			state.Distances = leg.start
			for member, moving := range leg.moving {
				if !moving {
					continue
				}
				var err error
				state.Distances[member], state.Speeds[member], err = leg.schedules[member].sample(remaining)
				if err != nil {
					return state, err
				}
			}
			if remaining == leg.ticks {
				switch i {
				case 0:
					state.Phase, state.Leg, state.Cursor, state.Dwell = couplingLatching, -1, 0, c.reservation.LatchTicks
					if state.Dwell == 0 {
						state.Phase, state.Leg = couplingConnected, 1
					}
				case 1:
					state.Phase, state.Leg, state.Cursor, state.Dwell = couplingUnlatching, -1, 0, c.reservation.UnlatchTicks
					if state.Dwell == 0 {
						state.Phase, state.Leg = couplingOpening, 2
					}
				case 2, 3:
					state.Phase, state.Leg, state.Cursor = couplingDraining, i+1, 0
				case 4:
					state.Finished = true
				}
			}
			return c.poseState(state)
		}
		remaining -= leg.ticks
		dwell := 0
		phase := couplingLatching
		if i == 0 {
			dwell = c.reservation.LatchTicks
		}
		if i == 1 {
			dwell = c.reservation.UnlatchTicks
			phase = couplingUnlatching
		}
		if dwell != 0 && remaining <= uint64(dwell) {
			if remaining > math.MaxInt {
				return state, couplingMotionInvariant("remaining dwell cannot be represented")
			}
			state.Phase, state.Distances, state.Dwell = phase, leg.end, dwell-int(remaining)
			if remaining == uint64(dwell) {
				state.Phase, state.Leg, state.Cursor = c.legs[i+1].phase, i+1, 0
			}
			return c.poseState(state)
		}
		remaining -= uint64(dwell)
	}
	return state, couplingMotionInvariant("motion cursor has no certified phase")
}

func (c *couplingMotionContext) poseState(state couplingMotionState) (couplingMotionState, error) {
	for i := range state.Distances {
		blocks := c.reservation.routes[i]
		lane, point, direction, err := couplingMotionPose(&blocks, state.Distances[i])
		if err != nil {
			return state, err
		}
		state.Lanes[i], state.Positions[i], state.Directions[i] = blocks.route[lane].ID, point, direction
		// Equality selects the next owned cell, except at the final granted end.
		entry := blocks.lanes[lane]
		count := blocks.lanes[lane+1].first - entry.first
		local := sort.Search(count, func(cell int) bool { return blocks.cellEnd(lane, cell) > state.Distances[i] })
		cell := entry.first + local
		if cell > c.through[i] && state.Distances[i] == blocks.at(c.through[i]).end {
			cell = c.through[i]
		}
		if cell > c.through[i] || state.Distances[i] > blocks.at(c.through[i]).end {
			return state, couplingMotionInvariant("member leaves complete physical grants")
		}
		state.Cells[i] = cell
	}
	return state, nil
}

func (c *couplingMotionContext) checkTickMotion(previous, next couplingMotionState) error {
	braking := .5
	if next.Leg >= 0 {
		braking = c.legs[next.Leg].braking
	} else if previous.Leg >= 0 {
		braking = c.legs[previous.Leg].braking
	}
	for i := range next.Distances {
		if next.Distances[i] != previous.Distances[i]+next.Speeds[i]/TicksPerSecond || math.Abs(next.Speeds[i]-previous.Speeds[i]) > braking/TicksPerSecond {
			return couplingMotionInvariant("rounded motion violates strict Euler or acceleration")
		}
		blocks := c.reservation.routes[i]
		stop := next.Speeds[i]*next.Speeds[i]/(2*braking) + next.Speeds[i]/TicksPerSecond
		if next.Distances[i]+stop > blocks.at(c.through[i]).end {
			return couplingMotionInvariant("physical owned frontier cannot stop next motion")
		}
		if err := couplingMotionSpeedProof(&blocks, previous.Distances[i], next.Distances[i], next.Speeds[i], braking); err != nil {
			return err
		}
	}
	if next.Phase == couplingDraining {
		for moving := range next.Distances {
			segments, err := couplingMotionSegments(&c.reservation.routes[moving], previous.Distances[moving], next.Distances[moving])
			if err != nil {
				return err
			}
			for _, segment := range segments {
				if segmentPointDistance(previous.Positions[1-moving], segment.from, segment.to) < Clearance-conflictSlack {
					return couplingMotionInvariant("ordinary drain swept separation failed")
				}
			}
		}
	} else {
		if _, err := c.connectedFootprint(next); err != nil {
			return err
		}
	}
	return nil
}

func (c *couplingMotionContext) connectedFootprint(state couplingMotionState) (CouplingFootprint, error) {
	front := state.Distances[0] - c.reservation.axisOrigins[0]
	rear := state.Distances[1] - c.reservation.axisOrigins[1]
	corridor := c.reservation.network.corridors[c.reservation.corridorID]
	direction := c.reservation.network.lanes[corridor.LaneIDs[0]].direction
	footprint, err := CouplingFootprintAt(CouplingFootprintInput{Contract: c.reservation.network.contract, Front: state.Positions[0], Direction: direction, SpacingMeters: front - rear})
	if err != nil || pointDistance(footprint.Centers[1], state.Positions[1]) > conflictSlack {
		return CouplingFootprint{}, couplingMotionInvariant("actual members differ from body and connector geometry")
	}
	return footprint, nil
}

func couplingMotionSpeedProof(blocks *blockList, start, end, speed, braking float64) error {
	lane, _, _, err := couplingMotionPose(blocks, start)
	if err != nil {
		return err
	}
	reach := end + speed*speed/(2*braking) + speed/TicksPerSecond
	for i := lane; i < len(blocks.route) && blocks.lanes[i].start <= reach; i++ {
		limit := blocks.route[i].SpeedLimit
		if blocks.lanes[i].start <= end && speed > limit {
			return couplingMotionInvariant("current or crossed lane speed exceeded")
		}
		if limit < speed && reach > blocks.lanes[i].start+limit*limit/(2*braking) {
			return couplingMotionInvariant("lower lane limit lacks discrete braking room")
		}
	}
	return nil
}

func (c *couplingMotionContext) membersAt(state couplingMotionState) [2]couplingCabinMotion {
	var members [2]couplingCabinMotion
	for i, m := range &c.reservation.members {
		v := m.Vehicle
		member := couplingCabinMotion{Pod: v.Pod, Riders: slices.Clone(v.Riders), Stops: slices.Clone(v.Stops), Boardings: slices.Clone(v.Boardings), RiddenMeters: v.RiddenMeters, RelocatingTo: v.RelocatingTo, RouteVersion: m.RouteVersion}
		member.Pod.Position, member.Pod.LaneID, member.Pod.Speed = state.Positions[i], state.Lanes[i], state.Speeds[i]
		blocks := c.reservation.routes[i]
		lane := blocks.routeLane(state.Cells[i])
		member.Pod.LaneDistance = state.Distances[i] - blocks.lanes[lane].start
		member.RiddenMeters = m.cabinMeters(state.Distances[i])
		members[i] = member
	}
	return members
}

func (c *couplingMotionContext) motionBodiesAt(state couplingMotionState) ([2]CouplingRectangle, *CouplingRectangle, error) {
	var bodies [2]CouplingRectangle
	profile, _ := LookupCouplingProfile(c.reservation.network.contract)
	for i := range bodies {
		bodies[i] = couplingRectangle(state.Positions[i], state.Directions[i], profile.BodyLengthMeters, profile.BodyWidthMeters)
	}
	if state.Phase == couplingDraining {
		return bodies, nil, nil
	}
	footprint, err := c.connectedFootprint(state)
	if err != nil {
		return bodies, nil, err
	}
	connector := footprint.Connector
	return bodies, &connector, nil
}
