package sim

import (
	"math"
	"slices"
)

type couplingDependency struct {
	Resource      resource
	MemberUse     [2]bool
	MemberRelease [2]float64
	AxisUse       bool
	AxisRelease   float64
	Site          bool
	NotBefore     couplingReservationPhase
}

func addCouplingDependency(dependencies map[resource]couplingDependency, r resource, member int, releaseAt, axisOrigin float64, laneID string, corridor []string) {
	dependency := dependencies[r]
	dependency.Resource = r
	dependency.MemberUse[member] = true
	dependency.MemberRelease[member] = max(dependency.MemberRelease[member], releaseAt)
	if slices.Contains(corridor, laneID) {
		dependency.AxisUse = true
		dependency.AxisRelease = max(dependency.AxisRelease, releaseAt-axisOrigin)
	}
	dependencies[r] = dependency
}

type couplingReservationPhase uint8

const (
	couplingClosing couplingReservationPhase = iota + 1
	couplingLatching
	couplingConnected
	couplingUnlatching
	couplingOpening
	couplingDraining
)

type couplingFrontierInput struct {
	Plan         couplingReservationPlan
	Phase        couplingReservationPhase
	Distances    [2]float64
	Speeds       [2]float64
	Owners       map[resource]resourceOwner
	SitesCleared map[resource]bool
	Owner        resourceOwner
}

func couplingPhasePositions(input couplingFrontierInput, profile CouplingProfile) error {
	front := input.Distances[0] - input.Plan.axisOrigins[0]
	rear := input.Distances[1] - input.Plan.axisOrigins[1]
	if !finite(front) || !finite(rear) {
		return couplingDenied("nonfinite member coordinates")
	}
	switch input.Phase {
	case couplingClosing:
		if input.Distances[0] != input.Plan.ClosingStops[0] || front-rear < profile.CenterSpacingMeters-conflictSlack || front-rear > Clearance+conflictSlack {
			return couplingDenied("closing members leave their protected targets")
		}
	case couplingLatching:
		if input.Distances != input.Plan.ClosingStops {
			return couplingDenied("latch geometry differs from closing targets")
		}
	case couplingConnected, couplingUnlatching:
		if math.Abs(front-rear-profile.CenterSpacingMeters) > conflictSlack {
			return couplingDenied("connected members lack rigid spacing")
		}
		if input.Phase == couplingUnlatching && input.Distances != input.Plan.SplitStops {
			return couplingDenied("unlatch geometry differs from split targets")
		}
	case couplingOpening:
		if input.Distances[1] != input.Plan.SplitStops[1] || input.Distances[0] < input.Plan.SplitStops[0] || input.Distances[0] > input.Plan.OpeningStops[0] {
			return couplingDenied("opening members leave their protected targets")
		}
	case couplingDraining:
		for i, distance := range input.Distances {
			if distance < input.Plan.OpeningStops[i] {
				return couplingDenied("draining member has not opened")
			}
		}
		if pointDistance(couplingRoutePoint(&input.Plan.routes[0], input.Distances[0]), couplingRoutePoint(&input.Plan.routes[1], input.Distances[1])) < Clearance-conflictSlack {
			return couplingDenied("draining members lack ordinary separation")
		}
	default:
		return couplingDenied("unknown reservation phase")
	}
	return nil
}
