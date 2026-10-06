package sim

import (
	"errors"
	"fmt"
	"math"
	"sort"
)

func (input RestoreStateInput) fleetContracts() FleetContracts {
	return FleetContracts{
		OrderContract: input.OrderContract, CouplingContract: input.CouplingContract,
		CouplingEnabled: input.CouplingEnabled, CouplingSites: input.CouplingSites,
		CouplingCorridors: input.CouplingCorridors, IncidentContract: input.IncidentContract,
		FaultContract: input.FaultContract, EmergencyContract: input.EmergencyContract,
	}
}

func checkCouplingRestoreInput(input RestoreStateInput) error {
	contract := input.CouplingContract
	if contract != "" {
		if _, ok := LookupCouplingProfile(contract); !ok {
			return ErrUnknownCouplingContract
		}
	} else if input.CouplingEnabled || input.CouplingSites != nil || input.CouplingCorridors != nil || input.State.CouplingGroups != nil {
		return errors.New("coupling restore fields require a coupling contract")
	}
	if input.State.CouplingContract != contract {
		return errors.New("saved and input coupling contracts differ")
	}
	if len(input.State.CouplingGroups) > len(input.State.Pods)/2 || len(input.State.CouplingGroups) > expressMaxPods/2 {
		return errors.New("saved coupling groups exceed the pair bound")
	}
	if len(input.State.CouplingGroups) == 0 {
		return nil
	}
	if input.LogicalOnly {
		return errors.New("committed coupling groups require physical restoration")
	}
	corridors := make(map[string]CouplingCorridor, len(input.CouplingCorridors))
	for _, corridor := range input.CouplingCorridors {
		corridors[corridor.ID] = corridor
	}
	pods := make(map[string]SavedPod, len(input.State.Pods))
	for _, pod := range input.State.Pods {
		pods[pod.ID] = pod
	}
	ids, members := make(map[string]bool), make(map[string]bool)
	for _, group := range input.State.CouplingGroups {
		if !boundedContractID(group.ID) || ids[group.ID] || group.FormationTick < 0 || group.FormationTick > input.State.Tick {
			return errors.New("invalid coupling group identity or formation tick")
		}
		ids[group.ID] = true
		corridor, ok := corridors[group.CorridorID]
		if !ok || group.AssemblySiteID != corridor.AssemblySiteID || group.SplitSiteID != corridor.SplitSiteID {
			return errors.New("saved coupling group has inconsistent sites or corridor")
		}
		if err := checkSavedCouplingProgress(group); err != nil {
			return err
		}
		var occupied [2]bool
		for i, id := range group.Members {
			pod, ok := pods[id]
			if !boundedContractID(id) || !ok || members[id] || pod.Class != CompactClass || pod.Activity != "traveling" ||
				pod.Platoon != nil || pod.CompactQueue != nil || pod.Waiting || pod.Released || len(pod.Route) == 0 {
				return errors.New("invalid committed coupling member")
			}
			members[id] = true
			occupied[i] = pod.Occupied
		}
		if occupied[0] != occupied[1] {
			return errors.New("coupling group has mixed cabin occupancy")
		}
	}
	for _, trip := range input.State.Waiting {
		if members[trip.Request.PodID] {
			return errors.New("pending order binds a committed coupling member")
		}
	}
	return nil
}

func checkSavedCouplingProgress(group SavedCouplingGroup) error {
	if group.Progress.DrainFirstMember < 0 || group.Progress.DrainFirstMember > 1 || group.DwellTicks < 0 {
		return errors.New("invalid coupling dwell or drain order")
	}
	leg := group.Progress.Leg
	profile, _ := LookupCouplingProfile(CompactPairV1CouplingContract)
	valid := false
	switch group.Phase {
	case CouplingClosing:
		valid = leg == 0 && group.DwellTicks == 0
	case CouplingLatching:
		valid = leg == 0 && group.DwellTicks <= profile.LatchTicks
	case CouplingConnected:
		valid = leg == 1 && group.DwellTicks == 0
	case CouplingUnlatching:
		valid = leg == 1 && group.DwellTicks <= profile.UnlatchTicks
	case CouplingOpening:
		valid = leg == 2 && group.DwellTicks == 0
	case CouplingDraining:
		valid = (leg == 3 || leg == 4) && group.DwellTicks == 0
	}
	if !valid {
		return errors.New("invalid saved coupling phase or progress")
	}
	return nil
}

func checkCouplingRestoreResult(result RestoreResult) error {
	if len(result.Demoted) != 0 || len(result.Requeued) != 0 || len(result.Dropped) != 0 || len(result.Interrupted) != 0 ||
		len(result.LogicalCompleted) != 0 || result.DroppedParties != 0 || result.OverCap != 0 || result.OverBudget != 0 {
		return errors.New("committed coupling restore cannot demote, requeue, drop, interrupt, or omit saved routes")
	}
	return nil
}

func (state SavedState) couplingMember(id string) bool {
	for _, group := range state.CouplingGroups {
		if group.Members[0] == id || group.Members[1] == id {
			return true
		}
	}
	return false
}

func nativeCouplingPhase(phase CouplingPhase) couplingReservationPhase {
	switch phase {
	case CouplingClosing:
		return couplingClosing
	case CouplingLatching:
		return couplingLatching
	case CouplingConnected:
		return couplingConnected
	case CouplingUnlatching:
		return couplingUnlatching
	case CouplingOpening:
		return couplingOpening
	case CouplingDraining:
		return couplingDraining
	default:
		return 0
	}
}

func (r *physicalRestore) restoreCouplingGroups() error {
	if len(r.state.CouplingGroups) == 0 {
		return nil
	}
	if err := checkCouplingRestoreResult(r.result); err != nil {
		return err
	}
	if r.s.couplingNetwork == nil {
		return ErrUnknownCouplingContract
	}
	// Restore only the individual receiving claims that the saved members held.
	for _, saved := range r.state.CouplingGroups {
		for _, id := range saved.Members {
			v := r.s.findVehicle(id)
			if v == nil || len(v.Route) == 0 {
				return errors.New("committed coupling member lost its full receiving route")
			}
			member := r.state.Pods[r.s.vehicleIndexes[id]]
			if !member.ClaimsDestination {
				if v.RidersAboard() == 0 {
					return errors.New("empty coupling member lost its receiving claim")
				}
				continue
			}
			if v.destination.ID == "" {
				return errors.New("saved coupling receiving claim lacks a berth")
			}
			for _, claim := range berthResources(v.destination) {
				owner := r.s.owners[claim]
				if !owner.isZero() && !owner.isPod(id) {
					return errors.New("committed coupling receiving claim conflicts with another owner")
				}
				r.s.owners[claim] = podResourceOwner(id)
			}
		}
	}
	for _, saved := range r.state.CouplingGroups {
		current := couplingReservationInput{
			Network: r.s.couplingNetwork, Prepared: r.s.couplingNetwork.prepared,
			CorridorID: saved.CorridorID, OrderContract: r.s.orderContract,
			Tick: r.state.Tick, Owners: r.s.owners,
		}
		for _, trip := range r.state.Waiting {
			current.Waiting = append(current.Waiting, Request(trip.Request))
		}
		for i, id := range saved.Members {
			index := r.s.vehicleIndexes[id]
			member, err := r.couplingMemberSnapshot(index)
			if err != nil {
				return fmt.Errorf("restore coupling member %s: %w", id, err)
			}
			current.Members[i] = member
		}
		var foreign []string
		for _, pod := range r.state.Pods {
			if pod.ID != saved.Members[0] && pod.ID != saved.Members[1] {
				foreign = append(foreign, pod.ID)
			}
		}
		c, initial, err := prepareRemainingCouplingMotion(couplingRemainingInput{
			Current: current, GroupID: saved.ID, FormationTick: saved.FormationTick,
			Phase: nativeCouplingPhase(saved.Phase), Leg: saved.Progress.Leg,
			DwellTicks: saved.DwellTicks, DrainFirstMember: saved.Progress.DrainFirstMember, ForeignIDs: foreign,
		})
		if err != nil {
			return fmt.Errorf("restore coupling group %s: %w", saved.ID, err)
		}
		for _, write := range initial.Writes {
			if r.s.owners[write.Resource] != write.Expected {
				return errors.New("coupling restore owner changed before publication")
			}
		}
		for _, write := range initial.Writes {
			r.s.owners[write.Resource] = write.Next
		}
		for i, id := range saved.Members {
			v := r.s.findVehicle(id)
			v.Pod = initial.Members[i].Pod
			v.couplingID = saved.ID
			v.distance, v.blockIndex, v.reservedThrough = initial.State.Distances[i], initial.State.Cells[i], c.through[i]
			v.originReleased = v.distance >= v.originTail()
			v.pending = -1
			for _, dependency := range c.dependencies {
				if dependency.MemberUse[i] && dependency.MemberRelease[i] > v.distance {
					v.retainRouteResource(dependency.Resource, dependency.MemberRelease[i])
				}
			}
		}
		r.s.couplingGroups = append(r.s.couplingGroups, couplingNativeGroup{context: c, state: initial.State, formationTick: saved.FormationTick})
	}
	return nil
}

func (r *physicalRestore) couplingMemberSnapshot(index int) (couplingMemberSnapshot, error) {
	v, saved := &r.s.vehicles[index], r.state.Pods[index]
	if r.demoted[index] || len(v.Route) == 0 || saved.RouteIndex < 0 || saved.RouteIndex >= len(v.Route) || !finite(saved.Distance) {
		return couplingMemberSnapshot{}, errors.New("committed member route or progress is unavailable")
	}
	lane, position, _, err := couplingMotionPose(&v.blocks, saved.Distance)
	if err != nil {
		return couplingMemberSnapshot{}, err
	}
	entry := v.blocks.lanes[lane]
	if lane != saved.RouteIndex || saved.LaneID != v.Route[lane].ID ||
		!finite(saved.LaneDistance) || math.Abs(saved.LaneDistance-(saved.Distance-entry.start)) > conflictSlack {
		return couplingMemberSnapshot{}, errors.New("committed member distance differs from its canonical lane")
	}
	count := v.blocks.lanes[lane+1].first - entry.first
	cell := entry.first + sort.Search(count, func(cell int) bool { return v.blocks.cellEnd(lane, cell) > saved.Distance })
	if cell >= v.blocks.len() {
		return couplingMemberSnapshot{}, errors.New("committed member leaves its bounded route")
	}
	v.Pod.LaneID, v.Pod.LaneDistance, v.Pod.Position, v.Pod.Speed = saved.LaneID, saved.LaneDistance, position, 0
	v.distance, v.blockIndex = saved.Distance, cell
	v.reservedThrough = reservationEnd(&v.blocks, cell)
	vehicle := v.Vehicle
	vehicle.RiddenMeters = 0
	if v.RidersAboard() > 0 || len(v.Boardings) > 0 {
		vehicle.RiddenMeters = v.riddenMeters()
	}
	return couplingMemberSnapshot{
		Vehicle: vehicle, RouteVersion: v.routeVersion, Distance: v.distance,
		BlockIndex: cell, ReservedThrough: v.reservedThrough,
		Origin: v.origin, Destination: v.destination, DestinationStation: v.destinationStation,
		Retained:         v.routeReleases,
		nativeRiddenBase: new(v.riddenBase),
	}, nil
}
