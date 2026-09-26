package sim

import (
	"fmt"
	"math"
)

// separationTolerance is the distance in meters by which a gap can be less
// than Clearance before Check reports a fault.
const separationTolerance = 1e-6

// SeparationError reports two pods on one plane that are closer than
// Clearance.
type SeparationError struct {
	Tick          int64
	First, Second string
	// Gap is the distance in meters between the two pods.
	Gap float64
}

// Error names the tick, the two pods and the gap between them.
func (e *SeparationError) Error() string {
	return fmt.Sprintf("tick %d: pods %s and %s are %.5f meters apart", e.Tick, e.First, e.Second, e.Gap)
}

// Check verifies finite positions, a finite and non-negative speed, pod
// separation and berth use. It returns the smallest gap in meters between two
// pods on one plane. The gap is +Inf when no two pods share a plane. When two
// pods are too close, the error is a *SeparationError. Check does not apply a
// speed limit, because each lane has its own limit.
func (o SafetyObservation) Check() (float64, error) {
	for _, pod := range o.Pods {
		if !finite(pod.Position.X) || !finite(pod.Position.Y) || !finite(pod.Speed) || pod.Speed < 0 {
			return 0, fmt.Errorf("invalid pod at tick %d: %+v", o.Tick, pod)
		}
	}
	gap, err := o.checkSeparation()
	if err != nil {
		return 0, err
	}
	if err := o.checkBerths(); err != nil {
		return 0, err
	}
	return gap, nil
}

// checkSeparation returns the smallest gap between two pods on one plane.
// It reads the location of each pod once, before it compares the pairs.
func (o SafetyObservation) checkSeparation() (float64, error) {
	const minimumGapSquared = (Clearance - separationTolerance) * (Clearance - separationTolerance)
	locations := make([]SafetyLocation, len(o.Pods))
	for index, pod := range o.Pods {
		locations[index] = o.Locations[pod.ID]
	}
	smallestSquared := math.Inf(1)
	for index, first := range o.Pods {
		for offset, second := range o.Pods[index+1:] {
			if safetyLocationsSeparated(locations[index], locations[index+1+offset]) {
				continue
			}
			dx := first.Position.X - second.Position.X
			dy := first.Position.Y - second.Position.Y
			gapSquared := dx*dx + dy*dy
			smallestSquared = min(smallestSquared, gapSquared)
			if gapSquared < minimumGapSquared {
				return 0, &SeparationError{Tick: o.Tick, First: first.ID, Second: second.ID, Gap: math.Sqrt(gapSquared)}
			}
		}
	}
	return math.Sqrt(smallestSquared), nil
}

// checkBerths verifies that each berth has at most one pod, and that the
// berth names that pod as its occupant and as its reservation holder.
func (o SafetyObservation) checkBerths() error {
	type occupancy struct {
		count int
		podID string
	}
	occupants := make(map[string]occupancy, len(o.Pods))
	for _, pod := range o.Pods {
		if pod.BerthID == "" {
			continue
		}
		occupied := occupants[pod.BerthID]
		occupied.count++
		occupied.podID = pod.ID
		occupants[pod.BerthID] = occupied
	}
	for _, berth := range o.Berths {
		occupied := occupants[berth.ID]
		if occupied.count > 1 {
			return fmt.Errorf("berth capacity exceeded at tick %d: %+v", o.Tick, berth)
		}
		if occupied.count == 1 && (berth.Occupant != occupied.podID || berth.ReservedBy != occupied.podID) {
			return fmt.Errorf("invalid berth state at tick %d: %+v", o.Tick, berth)
		}
	}
	return nil
}

// safetyLocationsSeparated reports whether two locations are on different
// planes. Locations in different separation groups are on different planes
// only when they have no node in common.
func safetyLocationsSeparated(first, second SafetyLocation) bool {
	if first.SeparationGroup == "" || second.SeparationGroup == "" || first.SeparationGroup == second.SeparationGroup {
		return false
	}
	return !safetyLocationsShareNode(first, second)
}

func safetyLocationsShareNode(first, second SafetyLocation) bool {
	return first.From != "" && (first.From == second.From || first.From == second.To) ||
		first.To != "" && (first.To == second.From || first.To == second.To)
}
