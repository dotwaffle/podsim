package sim

import (
	"math"
	"slices"
)

type couplingRemainingInput struct {
	Current                           couplingReservationInput
	GroupID                           string
	FormationTick                     int64
	Phase                             couplingReservationPhase
	Leg, DwellTicks, DrainFirstMember int
	ForeignIDs                        []string
}

// Preparation owns its inputs and proposes writes without changing the ledger.
func prepareRemainingCouplingMotion(input couplingRemainingInput) (*couplingMotionContext, couplingMotionStep, error) {
	var empty couplingMotionStep
	current := input.Current
	if current.Network == nil || current.Prepared != current.Network.prepared || current.Network.contract != CompactPairV1CouplingContract ||
		ValidateOrderContract(current.OrderContract) != nil || input.FormationTick < 0 || current.Tick < input.FormationTick ||
		!boundedContractID(input.GroupID) || len(input.ForeignIDs)+2 > expressMaxPods {
		return nil, empty, couplingDenied("invalid remaining network, contract, identities, or clock")
	}
	if err := couplingMemberEligibility(current); err != nil {
		return nil, empty, err
	}
	corridor, ok := current.Network.corridors[current.CorridorID]
	if !ok {
		return nil, empty, couplingDenied("unknown remaining corridor")
	}
	profile, _ := LookupCouplingProfile(current.Network.contract)
	c := &couplingMotionContext{owner: resourceOwner{kind: groupOwnerKind, id: input.GroupID}, formationTick: input.FormationTick,
		foreignIDs: slices.Clone(input.ForeignIDs), reservation: couplingReservationPlan{network: current.Network, corridorID: current.CorridorID,
			orderContract: current.OrderContract, tick: current.Tick, LatchTicks: profile.LatchTicks, UnlatchTicks: profile.UnlatchTicks}}
	dependencies := make(map[resource]couplingDependency)
	for i, member := range &current.Members {
		c.reservation.members[i] = cloneCouplingMember(member)
		if err := c.bindRemainingMember(i, corridor, dependencies); err != nil {
			return nil, empty, err
		}
	}
	if err := c.remainingForeignIDs(); err != nil {
		return nil, empty, err
	}
	if err := c.reservation.checkExitSeparation(); err != nil {
		return nil, empty, err
	}
	c.bindRemainingSites(corridor, dependencies)
	for _, dependency := range dependencies {
		c.reservation.Dependencies = append(c.reservation.Dependencies, dependency)
	}
	if err := c.bindExits(); err != nil {
		return nil, empty, err
	}
	if err := c.prepareRemainingLegs(input); err != nil {
		return nil, empty, err
	}
	state, err := c.stateAt(0)
	if err != nil || state.Finished {
		return nil, empty, couplingDenied("remaining motion is terminal or lacks a canonical initial state")
	}
	for i, member := range &current.Members {
		blocks := c.reservation.routes[i]
		lane := blocks.routeLane(state.Cells[i])
		if member.BlockIndex != state.Cells[i] || member.ReservedThrough < member.BlockIndex || member.ReservedThrough >= blocks.len() ||
			member.Vehicle.Pod.LaneID != state.Lanes[i] || !finite(member.Vehicle.Pod.LaneDistance) ||
			math.Abs(member.Vehicle.Pod.LaneDistance-(member.Distance-blocks.lanes[lane].start)) > conflictSlack ||
			!finite(member.Vehicle.Pod.Position.X) || !finite(member.Vehicle.Pod.Position.Y) || pointDistance(member.Vehicle.Pod.Position, state.Positions[i]) > conflictSlack {
			return nil, empty, couplingDenied("remaining member pose differs from its canonical route")
		}
	}
	step, err := c.remainingInitialWrites(state, current.Owners)
	if err != nil {
		return nil, empty, err
	}
	if err := c.prepareOwnerEvents(); err != nil {
		return nil, empty, err
	}
	return c, step, nil
}

func (c *couplingMotionContext) remainingForeignIDs() error {
	slices.Sort(c.foreignIDs)
	for i, id := range c.foreignIDs {
		if !boundedContractID(id) || id == c.reservation.members[0].Vehicle.Pod.ID || id == c.reservation.members[1].Vehicle.Pod.ID || i > 0 && id == c.foreignIDs[i-1] {
			return couplingDenied("invalid remaining foreign identities")
		}
	}
	return nil
}

// Bind actual routes without requiring their members to occupy formation poses.
func (c *couplingMotionContext) bindRemainingMember(index int, corridor CouplingCorridor, dependencies map[resource]couplingDependency) error {
	p := &c.reservation
	m := p.members[index]
	blocks, err := p.network.routeBlocks(m.Vehicle.Route)
	if err != nil {
		return err
	}
	first := slices.IndexFunc(blocks.route, func(lane Lane) bool { return lane.ID == corridor.LaneIDs[0] })
	if first < 0 || first+len(corridor.LaneIDs) > len(blocks.route) {
		return couplingDenied("remaining route lacks its complete corridor")
	}
	for i, id := range corridor.LaneIDs {
		if blocks.route[first+i].ID != id {
			return couplingDenied("remaining corridor route diverges")
		}
	}
	p.routes[index], p.axisOrigins[index] = blocks, blocks.lanes[first].start
	assembly := p.network.sites[corridor.AssemblySiteID]
	profile, _ := LookupCouplingProfile(p.network.contract)
	p.ClosingStops[index] = p.axisOrigins[index] + assembly.FrontStagingMeters
	if index == 1 {
		p.ClosingStops[index] -= profile.CenterSpacingMeters
	}
	if err := p.prepareExit(index, first+len(corridor.LaneIDs)-1, corridor); err != nil {
		return err
	}
	if err := p.network.proveInteractions(&blocks, blocks.lanes[first].first, p.Exits[index].Through); err != nil {
		return err
	}
	for _, b := range blocks.span(0, p.Exits[index].Through+1) {
		for _, r := range b.resources {
			addCouplingDependency(dependencies, r, index, resourceReleaseDistance(b, r), p.axisOrigins[index], b.lane.ID, corridor.LaneIDs)
		}
	}
	if m.Origin.ID != "" {
		for _, r := range berthResources(m.Origin) {
			addCouplingDependency(dependencies, r, index, max(Clearance, blocks.lanes[0].cells.fromTail), p.axisOrigins[index], "", nil)
		}
	}
	return nil
}

func (c *couplingMotionContext) bindRemainingSites(corridor CouplingCorridor, dependencies map[resource]couplingDependency) {
	p := &c.reservation
	room, _ := CouplingSiteRoom(p.network.contract)
	for _, id := range []string{corridor.AssemblySiteID, corridor.SplitSiteID} {
		site := p.network.sites[id]
		r := resource{kind: couplingSiteResourceKind(), id: id}
		d := couplingDependency{Resource: r, Site: true, NotBefore: couplingConnected}
		if id == corridor.SplitSiteID {
			d.NotBefore = couplingDraining
		}
		for i, blocks := range p.routes {
			lane := slices.IndexFunc(blocks.route, func(lane Lane) bool { return lane.ID == site.LaneID })
			d.MemberUse[i] = true
			d.MemberRelease[i] = blocks.lanes[lane].start + site.EndMeters + room.BoundaryMarginMeters
		}
		dependencies[r] = d
	}
}

func (c *couplingMotionContext) prepareRemainingLegs(input couplingRemainingInput) error {
	p := &c.reservation
	profile, _ := LookupCouplingProfile(p.network.contract)
	distances := [2]float64{p.members[0].Distance, p.members[1].Distance}
	if input.DrainFirstMember < 0 || input.DrainFirstMember > 1 || input.Leg < 0 || input.Leg > 4 || input.DwellTicks < 0 {
		return couplingDenied("invalid bounded remaining progress")
	}
	if err := couplingPhasePositions(couplingFrontierInput{Plan: *p, Phase: input.Phase, Distances: distances}, profile); err != nil {
		return err
	}
	c.drainOrder = [2]int{input.DrainFirstMember, 1 - input.DrainFirstMember}
	c.initialDistances, c.initialPhase = distances, input.Phase
	c.firstLeg = input.Leg
	switch input.Phase {
	case couplingClosing:
		if input.Leg != 0 || input.DwellTicks != 0 || distances[1] < p.axisOrigins[1]+p.network.sites[p.network.corridors[p.corridorID].AssemblySiteID].RearStagingMeters {
			return couplingDenied("invalid closing progress")
		}
		if distances == p.ClosingStops {
			c.firstLeg, c.initialPhase, c.initialDwell = 1, couplingLatching, p.LatchTicks
		}
	case couplingLatching:
		if input.Leg != 0 || input.DwellTicks > p.LatchTicks {
			return couplingDenied("invalid remaining latch dwell")
		}
		c.firstLeg, c.initialDwell = 1, input.DwellTicks
	case couplingConnected:
		if input.Leg != 1 || input.DwellTicks != 0 || distances[0] < p.ClosingStops[0] || distances[0] > p.SplitStops[0] {
			return couplingDenied("invalid connected progress")
		}
		if distances == p.SplitStops {
			c.firstLeg, c.initialPhase, c.initialDwell = 2, couplingUnlatching, p.UnlatchTicks
		}
	case couplingUnlatching:
		if input.Leg != 1 || input.DwellTicks > p.UnlatchTicks {
			return couplingDenied("invalid remaining unlatch dwell")
		}
		c.firstLeg, c.initialDwell = 2, input.DwellTicks
	case couplingOpening:
		if input.Leg != 2 || input.DwellTicks != 0 {
			return couplingDenied("invalid opening progress")
		}
		if distances == p.OpeningStops {
			c.firstLeg = 3
		}
	case couplingDraining:
		if (input.Leg != 3 && input.Leg != 4) || input.DwellTicks != 0 {
			return couplingDenied("invalid serial drain progress")
		}
	default:
		return couplingDenied("unknown remaining phase")
	}
	first, second := c.drainOrder[0], c.drainOrder[1]
	middle := p.OpeningStops
	middle[first] = c.terminal[first]
	starts := [5][2]float64{{p.ClosingStops[0], p.axisOrigins[1] + p.network.sites[p.network.corridors[p.corridorID].AssemblySiteID].RearStagingMeters}, p.ClosingStops, p.SplitStops, p.OpeningStops, middle}
	ends := [5][2]float64{p.ClosingStops, p.SplitStops, p.OpeningStops, middle, c.terminal}
	if c.firstLeg == 3 && distances == middle {
		c.firstLeg = 4
	}
	if c.firstLeg >= 3 {
		moving := c.drainOrder[c.firstLeg-3]
		fixed := 1 - moving
		if distances[fixed] != starts[c.firstLeg][fixed] || distances[moving] < starts[c.firstLeg][moving] || distances[moving] > ends[c.firstLeg][moving] || distances == c.terminal {
			return couplingDenied("invalid or terminal serial drain coordinates")
		}
	}
	origin := starts[c.firstLeg]
	starts[c.firstLeg] = distances
	moving := [5][2]bool{{false, true}, {true, true}, {true, false}, {first == 0, first == 1}, {second == 0, second == 1}}
	for leg := c.firstLeg; leg < len(c.legs); leg++ {
		phase, acceleration, speedCap := couplingDraining, profile.Acceleration, math.Inf(1)
		switch leg {
		case 0:
			phase, acceleration, speedCap = couplingClosing, profile.ManeuverAcceleration, profile.ManeuverSpeed
		case 1:
			phase = couplingConnected
		case 2:
			phase, acceleration, speedCap = couplingOpening, profile.ManeuverAcceleration, profile.ManeuverSpeed
		}
		if leg >= 3 && !c.serialSeparation(c.drainOrder[leg-3], starts[leg], ends[leg]) {
			return couplingDenied("remaining drain lacks ordinary swept separation")
		}
		var err error
		// A resumed leg keeps the speed cap of its original start. The lanes
		// behind the saved position can be slower than the lanes ahead.
		if leg == c.firstLeg {
			if speedCap, err = c.legSpeedCap(moving[leg], origin, ends[leg], acceleration, speedCap); err != nil {
				return err
			}
		}
		c.legs[leg], err = c.prepareLeg(phase, moving[leg], starts[leg], ends[leg], acceleration, speedCap)
		if err != nil {
			return err
		}
	}
	return c.finishRemainingClock()
}

func (c *couplingMotionContext) finishRemainingClock() error {
	initialDwell, latchTicks, unlatchTicks := c.initialDwell, c.reservation.LatchTicks, c.reservation.UnlatchTicks
	if initialDwell < 0 || latchTicks < 0 || unlatchTicks < 0 {
		return couplingDenied("negative remaining motion dwell")
	}
	c.ticks = uint64(initialDwell)
	for leg := c.firstLeg; leg < len(c.legs); leg++ {
		add := c.legs[leg].ticks
		if add > math.MaxInt64 {
			return couplingDenied("remaining leg duration overflows")
		}
		if leg == 0 {
			add += uint64(latchTicks)
		}
		if leg == 1 {
			add += uint64(unlatchTicks)
		}
		if add > math.MaxInt64-c.ticks {
			return couplingDenied("remaining motion duration overflows")
		}
		c.ticks += add
	}
	if c.ticks > math.MaxInt64 {
		return couplingDenied("remaining motion duration overflows")
	}
	motionTicks := int64(c.ticks)
	if c.reservation.tick > math.MaxInt64-motionTicks {
		return couplingDenied("remaining motion clock overflows")
	}
	return nil
}

func (c *couplingMotionContext) remainingInitialWrites(state couplingMotionState, owners map[resource]resourceOwner) (couplingMotionStep, error) {
	step := couplingMotionStep{State: state, Members: c.membersAt(state)}
	for i, d := range c.dependencies {
		next := c.dependencyOwner(d, state)
		if next.isZero() {
			continue
		}
		owner := owners[d.Resource]
		if !owner.isZero() && owner != c.owner && !owner.isPod(c.reservation.members[0].Vehicle.Pod.ID) && !owner.isPod(c.reservation.members[1].Vehicle.Pod.ID) {
			return couplingMotionStep{}, couplingDenied("unresolved remaining dependency has a foreign owner")
		}
		c.claims[i].Expected = owner
		step.Writes = append(step.Writes, couplingOwnerWrite{Resource: d.Resource, Expected: owner, Next: next, Role: couplingDependencyRole(d, next)})
	}
	adopted := make(map[resource]couplingDependency, len(c.dependencies))
	for _, d := range c.dependencies {
		if !c.dependencyOwner(d, state).isZero() {
			adopted[d.Resource] = d
		}
	}
	input := couplingReservationInput{Members: c.reservation.members, Owners: owners}
	if err := c.reservation.preserveReceivingClaims(input, adopted); err != nil {
		return couplingMotionStep{}, err
	}
	var err error
	step.Bodies, step.Connector, err = c.motionBodiesAt(state)
	if err != nil {
		return couplingMotionStep{}, err
	}
	return step, nil
}
