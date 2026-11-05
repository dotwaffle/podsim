package sim

import "math"

// motionEnvelope holds the rates of a vehicle class in m/s². A class has one
// envelope for each load, so an estimate and a restore do not depend on the
// riders.
type motionEnvelope struct {
	// acceleration bounds a speed rise.
	acceleration float64
	// plannedBraking is the rate toward a lower speed that is known ahead.
	plannedBraking float64
	// maximumBraking is the hardest service rate. A pod always owns track to
	// stop at this rate, and a fault stops at it.
	maximumBraking float64
}

// motionEnvelopeFor returns the envelope of a class. An omitted class is
// legacy. coast.go, platoon.go, retention.go, and the depot code stay on the
// acceleration constant, because a large class cannot reach them. Express
// carries standing passengers, so it has lower rates and plans its stops
// below its maximum rate.
func motionEnvelopeFor(class VehicleClass) motionEnvelope {
	if class == ExpressClass {
		return motionEnvelope{acceleration: 1.3, plannedBraking: 0.8, maximumBraking: 1.15}
	}
	return motionEnvelope{acceleration: acceleration, plannedBraking: acceleration, maximumBraking: acceleration}
}

// plansBelowMaximum reports whether the planned rate is under the maximum
// rate. A seat-only class has one rate, so it skips the planned floor and the
// plan cap, and it keeps its float operations.
func (e motionEnvelope) plansBelowMaximum() bool {
	return e.plannedBraking < e.maximumBraking
}

// plannedFloor returns the lowest speed that a planned rule can command for a
// pod at speed. A route change can bring a stop or a lower limit nearer than
// the planned rate reaches. The floor keeps that pod within the maximum rate.
func (e motionEnvelope) plannedFloor(speed float64) float64 {
	return speed - e.maximumBraking/TicksPerSecond
}

// planCap returns the highest speed for a pod at speed that stops at the
// planned rate in the remaining route, at the floor or above. The reservation
// limit stays the hard bound, and this cap only lowers the command.
func (e motionEnvelope) planCap(remaining, speed float64) float64 {
	step := e.plannedBraking / TicksPerSecond
	return math.Max(e.plannedFloor(speed), math.Sqrt(step*step+2*e.plannedBraking*math.Max(0, remaining))-step)
}

// stoppingDistance returns the distance in which a pod at speed stops at the
// maximum service rate.
func (e motionEnvelope) stoppingDistance(speed float64) float64 {
	return speed * speed / (2 * e.maximumBraking)
}

// reservationHorizon returns how far ahead of a pod at speed its reservation
// must reach before it stops requesting cells.
func (e motionEnvelope) reservationHorizon(speed float64) float64 {
	return e.stoppingDistance(speed) + speed*reservationLookaheadSeconds
}
