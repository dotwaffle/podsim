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

// This hook proves an owned stop. It does not plan or publish a motion step.
func couplingOwnedFrontier(input couplingFrontierInput) ([2]float64, error) {
	var frontiers [2]float64
	if input.Plan.network == nil || input.Owner.id == "" || (input.Owner.kind != podOwnerKind && input.Owner.kind != groupOwnerKind) {
		return frontiers, couplingDenied("invalid proposed frontier owner")
	}
	if len(input.Plan.Claims) == 0 || len(input.Plan.Claims) != len(input.Plan.Dependencies) {
		return frontiers, couplingDenied("invalid frontier certificate")
	}
	for i, claim := range input.Plan.Claims {
		if claim.Resource != input.Plan.Dependencies[i].Resource {
			return frontiers, couplingDenied("frontier dependency keys differ")
		}
	}
	for i, route := range input.Plan.routes {
		if len(route.route) == 0 || len(route.lanes) != len(route.route)+1 || input.Plan.Exits[i].Through < 0 || input.Plan.Exits[i].Through >= route.len() {
			return frontiers, couplingDenied("invalid member frontier route")
		}
	}
	for _, dependency := range input.Plan.Dependencies {
		if input.Owners[dependency.Resource] == input.Owner {
			continue
		}
		clearance := couplingClearanceInput{Dependency: dependency, Phase: input.Phase, Distances: input.Distances, FrontAxis: input.Distances[0] - input.Plan.axisOrigins[0], SitesCleared: input.SitesCleared[dependency.Resource]}
		if !couplingDependencyCleared(clearance) {
			return frontiers, couplingDenied("unresolved frontier dependency lacks an owner")
		}
	}
	profile, ok := LookupCouplingProfile(input.Plan.network.contract)
	if !ok {
		return frontiers, couplingDenied("invalid frontier model")
	}
	if err := couplingPhasePositions(input, profile); err != nil {
		return frontiers, err
	}
	braking, speedCap, err := couplingPhaseBounds(input.Phase, profile, input.Speeds)
	if err != nil {
		return frontiers, err
	}
	for i := range input.Distances {
		blocks := input.Plan.routes[i]
		distance, speed := input.Distances[i], input.Speeds[i]
		if !finite(distance) || !finite(speed) || speed < 0 || speed > speedCap || distance < input.Plan.members[i].Distance {
			return frontiers, couplingDenied("invalid current phase motion facts")
		}
		through := input.Plan.Exits[i].Through
		if through < 0 || through >= blocks.len() {
			return frontiers, couplingDenied("invalid exit grant bounds")
		}
		limit := blocks.at(through).end
		switch input.Phase {
		case couplingClosing, couplingLatching:
			limit = min(limit, input.Plan.ClosingStops[i])
		case couplingConnected, couplingUnlatching:
			limit = min(limit, input.Plan.SplitStops[i])
		case couplingOpening:
			limit = min(limit, input.Plan.OpeningStops[i])
		case couplingDraining:
			// Draining uses the ordinary actual-exit grant frontier.
		}
		frontiers[i] = limit
		stop := speed*speed/(2*braking) + speed/TicksPerSecond
		if distance+stop > limit+conflictSlack {
			return frontiers, couplingDenied("owned frontier is shorter than discrete stopping room")
		}
		if err := couplingLaneSpeedProof(&blocks, distance, speed, braking); err != nil {
			return frontiers, err
		}
	}
	return frontiers, nil
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

func couplingPhaseBounds(phase couplingReservationPhase, profile CouplingProfile, speeds [2]float64) (float64, float64, error) {
	switch phase {
	case couplingClosing:
		if speeds[0] != 0 {
			return 0, 0, couplingDenied("closing head is not stopped")
		}
		return profile.ManeuverBraking, profile.ManeuverSpeed, nil
	case couplingOpening:
		if speeds[1] != 0 {
			return 0, 0, couplingDenied("opening rear is not stopped")
		}
		return profile.ManeuverBraking, profile.ManeuverSpeed, nil
	case couplingLatching, couplingUnlatching:
		if speeds != ([2]float64{}) {
			return 0, 0, couplingDenied("dwell members are not stopped")
		}
		return profile.ManeuverBraking, 0, nil
	case couplingConnected:
		if speeds[0] != speeds[1] {
			return 0, 0, couplingDenied("connected speeds differ")
		}
		return profile.Braking, math.Inf(1), nil
	case couplingDraining:
		return profile.Braking, math.Inf(1), nil
	}
	return 0, 0, couplingDenied("unknown reservation phase")
}

func couplingLaneSpeedProof(blocks *blockList, distance, speed, braking float64) error {
	current := -1
	for i := range blocks.route {
		if distance <= blocks.lanes[i].start+blocks.lanes[i].length {
			current = i
			break
		}
	}
	if current < 0 || speed > blocks.route[current].SpeedLimit {
		return couplingDenied("current member lane speed exceeded")
	}
	reach := speed*speed/(2*braking) + speed/TicksPerSecond
	for i := current + 1; i < len(blocks.route); i++ {
		remaining := blocks.lanes[i].start - distance
		if remaining > reach {
			break
		}
		limit := blocks.route[i].SpeedLimit
		if limit < speed && speed*speed/(2*braking)+speed/TicksPerSecond > remaining+limit*limit/(2*braking)+conflictSlack {
			return couplingDenied("lower-speed lane lies inside unproved braking reach")
		}
	}
	return nil
}

type couplingClearanceInput struct {
	Dependency   couplingDependency
	Phase        couplingReservationPhase
	Distances    [2]float64
	FrontAxis    float64
	SitesCleared bool
}

// Dependencies remain joint until both local and connector bounds pass.
func couplingDependencyCleared(input couplingClearanceInput) bool {
	if input.Phase < couplingClosing || input.Phase > couplingDraining || !finite(input.FrontAxis) {
		return false
	}
	for i, used := range input.Dependency.MemberUse {
		if !finite(input.Distances[i]) || !finite(input.Dependency.MemberRelease[i]) || used && input.Distances[i] < input.Dependency.MemberRelease[i] {
			return false
		}
	}
	if input.Dependency.Site {
		return input.Dependency.NotBefore != 0 && input.Phase >= input.Dependency.NotBefore && input.SitesCleared
	}
	if input.Dependency.AxisUse && input.Phase != couplingDraining {
		spacing := Clearance
		if input.Phase == couplingConnected || input.Phase == couplingUnlatching {
			profile, _ := LookupCouplingProfile(CompactPairV1CouplingContract)
			spacing = profile.CenterSpacingMeters
		}
		if !finite(input.Dependency.AxisRelease) {
			return false
		}
		if input.FrontAxis < input.Dependency.AxisRelease+spacing {
			return false
		}
	}
	return true
}
