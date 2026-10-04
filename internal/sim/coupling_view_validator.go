package sim

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"sort"
)

// CouplingViewValidator checks received train data against immutable geometry.
// It checks observable facts. The native producer checks hidden reservations.
type CouplingViewValidator struct {
	contract  CouplingContract
	profile   CouplingProfile
	lanes     map[string]couplingViewLane
	sites     map[string]CouplingSite
	corridors map[string]CouplingCorridor
}

type couplingViewLane struct {
	lane     Lane
	geometry *laneGeometry
}

// NewCouplingViewValidator takes a detached copy of the geometry it needs.
func NewCouplingViewValidator(input CouplingGeometryInput) (*CouplingViewValidator, error) {
	if err := ValidateCouplingGeometry(input); err != nil {
		return nil, err
	}
	profile, known := LookupCouplingProfile(input.Contract)
	if !known {
		return nil, ErrUnknownCouplingContract
	}
	if err := validateContractNetworkRecords(input.Network); err != nil {
		return nil, err
	}
	if err := validateContractGeometryBudget(input.Network); err != nil {
		return nil, err
	}
	if err := input.Network.validate(); err != nil {
		return nil, err
	}
	v := &CouplingViewValidator{contract: input.Contract, profile: profile,
		lanes: make(map[string]couplingViewLane, len(input.Network.Lanes)),
		sites: make(map[string]CouplingSite, len(input.Sites)), corridors: make(map[string]CouplingCorridor, len(input.Corridors))}
	geometry := buildLaneGeometry(input.Network)
	for _, lane := range input.Network.Lanes {
		lane.Control = nil // Sampled segments own the canonical curve geometry.
		v.lanes[lane.ID] = couplingViewLane{lane: lane, geometry: geometry[lane.ID]}
	}
	for _, site := range input.Sites {
		v.sites[site.ID] = site
	}
	for _, corridor := range input.Corridors {
		corridor.LaneIDs = slices.Clone(corridor.LaneIDs)
		v.corridors[corridor.ID] = corridor
	}
	return v, nil
}

// Validate rejects inconsistent membership, phase, motion, and body geometry.
// It does not mutate the candidate or retain its containers.
func (v *CouplingViewValidator) Validate(state Snapshot) error {
	if state.CouplingContract != v.contract || state.Tick < 0 || len(state.Vehicles) > expressMaxPods ||
		len(state.CouplingGroups) > len(state.Vehicles)/2 {
		return errors.New("invalid coupling view contract, clock, or pair count")
	}
	pods := make(map[string]Vehicle, len(state.Vehicles))
	for _, vehicle := range state.Vehicles {
		if !boundedContractID(vehicle.Pod.ID) {
			return errors.New("invalid coupling view vehicle identity")
		}
		if _, exists := pods[vehicle.Pod.ID]; exists {
			return errors.New("duplicate coupling view vehicle identity")
		}
		pods[vehicle.Pod.ID] = vehicle
	}
	groups, members := make(map[string]bool, len(state.CouplingGroups)), make(map[string]bool, len(state.CouplingGroups)*2)
	for _, group := range state.CouplingGroups {
		if !boundedContractID(group.ID) || groups[group.ID] || group.OwnerID != group.ID || group.Profile != v.contract ||
			group.ResourceClaims < 0 || group.FormationTick < 0 || group.FormationTick > state.Tick {
			return errors.New("invalid coupling view group identity, profile, or ownership")
		}
		groups[group.ID] = true
		if err := checkSavedCouplingProgress(group.SavedCouplingGroup); err != nil {
			return err
		}
		corridor, exists := v.corridors[group.CorridorID]
		if !exists || group.AssemblySiteID != corridor.AssemblySiteID || group.SplitSiteID != corridor.SplitSiteID {
			return errors.New("coupling view has inconsistent corridor or sites")
		}
		var cabins [2]Vehicle
		for i, id := range group.Members {
			cabin, exists := pods[id]
			if !exists || members[id] || cabin.CouplingID != group.ID || cabin.Pod.Class != CompactClass || cabin.Pod.Activity != Traveling ||
				cabin.PlatoonID != "" || cabin.PlatoonIndex != 0 || cabin.Pod.Occupied != (cabin.RidersAboard() > 0) ||
				cabin.RidersAboard() > v.profile.SeatsPerMember {
				return errors.New("invalid coupling view cabin membership or occupancy")
			}
			members[id], cabins[i] = true, cabin
			if err := v.body(cabin.Pod, group.Bodies[i]); err != nil {
				return fmt.Errorf("coupling member %q: %w", id, err)
			}
		}
		if cabins[0].Pod.Occupied != cabins[1].Pod.Occupied {
			return errors.New("coupling view has mixed cabin occupancy")
		}
		if err := v.phase(group, corridor, cabins); err != nil {
			return err
		}
	}
	for _, cabin := range state.Vehicles {
		if cabin.CouplingID != "" && !members[cabin.Pod.ID] {
			return errors.New("coupling cabin has no matching train registry record")
		}
	}
	for _, request := range state.Pending {
		if members[request.PodID] {
			return errors.New("pending order binds a committed coupling member")
		}
	}
	return nil
}

func (v *CouplingViewValidator) body(pod Pod, body CouplingRectangle) error {
	lane, exists := v.lanes[pod.LaneID]
	if !exists || !finite(pod.LaneDistance) || pod.LaneDistance < 0 || !finite(pod.Speed) ||
		pod.Speed < 0 || pod.Speed > lane.lane.SpeedLimit || !contractPointFits(pod.Position) {
		return errors.New("invalid coupling cabin lane or motion")
	}
	segments := lane.geometry.segments
	i := sort.Search(len(segments), func(i int) bool { return segments[i].end >= pod.LaneDistance })
	if i == len(segments) {
		return errors.New("coupling cabin leaves its canonical lane")
	}
	s := segments[i]
	point := couplingSegmentPoint(s, pod.LaneDistance)
	direction := Point{X: (s.to.X - s.from.X) / (s.end - s.start), Y: (s.to.Y - s.from.Y) / (s.end - s.start)}
	expected := couplingRectangle(point, direction, v.profile.BodyLengthMeters, v.profile.BodyWidthMeters)
	if pointDistance(point, pod.Position) > conflictSlack || !couplingViewRectangleEqual(body, expected) {
		return errors.New("coupling body differs from its canonical cabin pose")
	}
	return nil
}

func couplingViewRectangleEqual(actual, expected CouplingRectangle) bool {
	for i, point := range actual.Corners {
		if !finite(point.X) || !finite(point.Y) || pointDistance(point, expected.Corners[i]) > conflictSlack {
			return false
		}
	}
	return true
}

func (v *CouplingViewValidator) phase(group CouplingGroupView, corridor CouplingCorridor, cabins [2]Vehicle) error {
	front, rear := cabins[0].Pod, cabins[1].Pod
	connected := group.Phase == CouplingLatching || group.Phase == CouplingConnected || group.Phase == CouplingUnlatching
	maneuver := group.Phase == CouplingClosing || group.Phase == CouplingOpening
	if (group.CommonSpeed != nil) != connected || (group.Connector != nil) != connected || (group.ManeuverEnvelope != nil) != maneuver {
		return errors.New("coupling view geometry does not match its phase")
	}
	if connected && (!finite(*group.CommonSpeed) || *group.CommonSpeed != front.Speed || front.Speed != rear.Speed) {
		return errors.New("coupling view has inconsistent common speed")
	}
	if group.Phase == CouplingDraining {
		stationary := 1 - group.Progress.DrainFirstMember
		if group.Progress.Leg == 4 {
			stationary = group.Progress.DrainFirstMember
		}
		if cabins[stationary].Pod.Speed != 0 || pointDistance(front.Position, rear.Position) < Clearance-conflictSlack {
			return errors.New("coupling drain lacks serial motion or ordinary separation")
		}
		return nil
	}
	axis := v.lanes[corridor.LaneIDs[0]].geometry.segments[0]
	direction := Point{X: (axis.to.X - axis.from.X) / (axis.end - axis.start), Y: (axis.to.Y - axis.from.Y) / (axis.end - axis.start)}
	spacing := (front.Position.X-rear.Position.X)*direction.X + (front.Position.Y-rear.Position.Y)*direction.Y
	if !finite(spacing) || spacing < v.profile.CenterSpacingMeters-conflictSlack || spacing > Clearance+conflictSlack ||
		pointDistance(couplingOffset(front.Position, direction, -spacing), rear.Position) > conflictSlack ||
		!slices.Contains(corridor.LaneIDs, front.LaneID) || !slices.Contains(corridor.LaneIDs, rear.LaneID) {
		return errors.New("coupling view leaves its ordered straight corridor")
	}
	shape := group.Connector
	if maneuver {
		shape = group.ManeuverEnvelope
	}
	center := Point{X: (front.Position.X + rear.Position.X) / 2, Y: (front.Position.Y + rear.Position.Y) / 2}
	expected := couplingRectangle(center, direction, spacing-2*v.profile.PinOffsetMeters, v.profile.ConnectorWidthMeters)
	if !couplingViewRectangleEqual(*shape, expected) || connected && math.Abs(spacing-v.profile.CenterSpacingMeters) > conflictSlack {
		return errors.New("coupling connector differs from its ordered cabin geometry")
	}
	return v.sitePhase(group.Phase, corridor, front, rear)
}

func (v *CouplingViewValidator) sitePhase(phase CouplingPhase, corridor CouplingCorridor, front, rear Pod) error {
	assembly, split := v.sites[corridor.AssemblySiteID], v.sites[corridor.SplitSiteID]
	frontAt := func(site CouplingSite) bool {
		return front.LaneID == site.LaneID && math.Abs(front.LaneDistance-site.FrontStagingMeters) <= conflictSlack
	}
	rearAtSplit := rear.LaneID == split.LaneID && math.Abs(rear.LaneDistance-(split.FrontStagingMeters-v.profile.CenterSpacingMeters)) <= conflictSlack
	valid := false
	switch phase {
	case CouplingClosing:
		valid = frontAt(assembly) && front.Speed == 0 && rear.Speed <= v.profile.ManeuverSpeed
	case CouplingLatching:
		valid = frontAt(assembly) && front.Speed == 0 && rear.Speed == 0
	case CouplingConnected:
		valid = (front.LaneID != assembly.LaneID || front.LaneDistance >= assembly.FrontStagingMeters-conflictSlack) &&
			(front.LaneID != split.LaneID || front.LaneDistance <= split.FrontStagingMeters+conflictSlack)
	case CouplingUnlatching:
		valid = frontAt(split) && rearAtSplit && front.Speed == 0 && rear.Speed == 0
	case CouplingOpening:
		valid = rearAtSplit && rear.Speed == 0 && front.Speed <= v.profile.ManeuverSpeed
	case CouplingDraining:
		valid = false // Draining uses its separate serial-motion check.
	}
	if !valid {
		return errors.New("coupling phase differs from its site pose or motion")
	}
	return nil
}
