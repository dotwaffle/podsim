package sim

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
// acceleration constant, because a large class cannot reach them.
func motionEnvelopeFor(class VehicleClass) motionEnvelope {
	if class == ExpressClass {
		return motionEnvelope{acceleration: acceleration, plannedBraking: acceleration, maximumBraking: acceleration}
	}
	return motionEnvelope{acceleration: acceleration, plannedBraking: acceleration, maximumBraking: acceleration}
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
