package sim

import "math"

// This inverse control is the exact base038 ordinary move caller, test only.
func (s *Simulation) oldMoveNativeForeign(v *vehicle) {
	if s.moveCompact(v) {
		return
	}
	limit := v.blocks.end(v.reservedThrough)
	if v.link.leader != 0 && v.platoonCap < limit {
		limit = v.platoonCap
	}
	available := math.Max(0, limit-v.distance)
	dt := 1.0 / TicksPerSecond
	// Semi-implicit integration preserves enough owned track to stop on the next tick.
	safe := math.Sqrt(acceleration*acceleration*dt*dt+2*acceleration*available) - acceleration*dt
	blocks := &v.blocks
	current := blocks.find(v.blockIndex, &blocks.cursors[podCursor])
	// A pod with no lane has left its berth, so it enters its current lane
	// in this tick.
	entered := current.lane + 1
	if v.Pod.LaneID == "" {
		entered = current.lane
	}
	speed := math.Min(v.Pod.Speed+acceleration*dt, math.Min(blocks.route[current.lane].SpeedLimit, math.Max(0, safe)))
	v.Pod.Speed = blocks.speedBeforeLane(current.lane, v.distance, speed)
	travel := math.Min(available, v.Pod.Speed*dt)
	v.distance += travel
	if limit-v.distance < 1e-5 {
		v.distance = limit
		v.Pod.Speed = 0
	}
	for v.distance >= current.end {
		if v.blockIndex+1 == blocks.len() {
			s.recordLaneEntries(v, entered, current.lane)
			if v.destination.ID == "" {
				v.Pod.Speed = 0
				v.Pod.WaitReason = BerthOccupied
				return
			}
			s.arrive(v)
			return
		}
		if v.blockIndex == v.reservedThrough {
			break
		}
		v.blockIndex++
		current = blocks.find(v.blockIndex, current)
	}
	lane := current.lane
	s.recordLaneEntries(v, entered, lane)
	v.Pod.LaneID, v.Pod.LaneDistance = blocks.route[lane].ID, v.distance-blocks.lanes[lane].start
	v.Pod.Position = s.lanePosition(blocks.lanes[lane].geometry, &blocks.route[lane], v.Pod.LaneDistance)
}

// This inverse wrapper copies the base038 native accounting caller.
func (s *Simulation) oldMoveAndMeasureNativeForeign(v *vehicle) {
	sample := MotionSample{ID: v.Pod.ID, Class: v.Pod.Class, StartSpeed: v.Pod.Speed}
	before := v.distance
	beforeLocal := v.Pod.LaneDistance
	compact := s.compactGroup(v) != nil
	occupied := v.Pod.Occupied
	s.oldMoveNativeForeign(v)
	travel := v.distance - before
	if compact {
		travel = v.Pod.LaneDistance - beforeLocal
	}
	sample.DistanceMeters, sample.EndSpeed = travel, v.Pod.Speed
	s.recordMotion(sample)
	if occupied {
		s.passengerDistanceMeters += travel
	} else {
		s.emptyDistanceMeters += travel
	}
}
