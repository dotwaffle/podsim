package sim

import "math"

// The command and published speed differ at the existing ordinary endpoint snap.
type ordinaryMoveResult struct {
	commandedSpeed float64
	distance       float64
	speed          float64
}

// moveStep is the one motion kernel of a pod in move and in each motion
// proof. The commanded speed is at most ceiling, and the pod stops by
// limit. ordinaryMoveStep and faultMoveStep differ only in these bounds.
// speed is the speed of the pod before the step.
func moveStep(blocks *blockList, envelope motionEnvelope, lane int, distance, speed, ceiling, limit float64) ordinaryMoveResult {
	available := math.Max(0, limit-distance)
	dt := 1.0 / TicksPerSecond
	safe := math.Sqrt(envelope.maximumBraking*envelope.maximumBraking*dt*dt+2*envelope.maximumBraking*available) - envelope.maximumBraking*dt
	command := math.Min(ceiling, math.Min(blocks.route[lane].SpeedLimit, math.Max(0, safe)))
	allowed := command
	command = blocks.speedBeforeLane(envelope, lane, distance, allowed)
	if envelope.plansBelowMaximum() {
		command = math.Min(allowed, math.Max(command, envelope.plannedFloor(speed)))
	}
	travel := math.Min(available, command*dt)
	next := ordinaryMoveResult{commandedSpeed: command, distance: distance + travel, speed: command}
	if limit-next.distance < 1e-5 {
		next.distance = limit
		next.speed = 0
	}
	return next
}

// ordinaryMoveStep is the step of a pod without a fault, with the ceiling
// speed + envelope.acceleration × dt. It preserves the ordinary controller's float
// operations and order. Its caller derives the limit from actual grants and
// the frozen predecessor cap. An envelope that plans below its maximum rate
// also has the plan cap toward the route end in the ceiling.
func ordinaryMoveStep(blocks *blockList, envelope motionEnvelope, lane int, distance, speed, limit float64) ordinaryMoveResult {
	dt := 1.0 / TicksPerSecond
	ceiling := speed + envelope.acceleration*dt
	if envelope.plansBelowMaximum() {
		ceiling = math.Min(ceiling, envelope.planCap(blocks.end(blocks.len()-1)-distance, speed))
	}
	return moveStep(blocks, envelope, lane, distance, speed, ceiling, limit)
}

// coastMoveStep is ordinaryMoveStep with the commanded speed also at most
// ceiling, the coast ceiling of coastCaps. The limit does not change, so
// the pod still stops by the end of its reservation. With an infinite
// ceiling it is ordinaryMoveStep.
func coastMoveStep(blocks *blockList, envelope motionEnvelope, lane int, distance, speed, limit, ceiling float64) ordinaryMoveResult {
	dt := 1.0 / TicksPerSecond
	return moveStep(blocks, envelope, lane, distance, speed, math.Min(speed+envelope.acceleration*dt, ceiling), limit)
}

// faultMoveStep is the motion step of a faulted pod (incident suspension
// contract, section 6.1). It is moveStep with the limit min(limit,
// faultCap) and the ceiling speed, so the commanded speed never exceeds
// speed. move and each motion proof use it with the same frozen inputs.
//
// At onset, faultCap - distance is the stopping distance at speed, or less when
// the grant end is nearer. The kernel then commands a speed that is at
// most speed and at least speed - envelope.maximumBraking × dt, and after the step
// the remaining distance is the stopping distance at the new speed. The
// ceiling removes a rounding rise above speed, and the snap brings the pod
// to rest at the cap. The plan cap does not apply, because it would stop the
// pod short of the cap.
func faultMoveStep(blocks *blockList, envelope motionEnvelope, lane int, distance, speed, limit, faultCap float64) ordinaryMoveResult {
	return moveStep(blocks, envelope, lane, distance, speed, speed, math.Min(limit, faultCap))
}
