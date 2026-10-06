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
	p.ClosingStops[index] = p.network.closingStop(index, p.axisOrigins[index], p.network.sites[corridor.AssemblySiteID])
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

// couplingRemainingLeg is the plan of one motion leg before it resumes: its
// phase, its moving members, its coordinates, and its acceleration and
// profile speed cap.
type couplingRemainingLeg struct {
	phase                  couplingReservationPhase
	moving                 [2]bool
	start, end             [2]float64
	acceleration, speedCap float64
}

// prepareRemainingLegs resumes the motion from the saved progress. The
// checks run in a fixed order, because the first refusal is the result: the
// progress bounds, the phase positions, the saved phase, the serial drain
// coordinates, and then each remaining leg in leg order.
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
	if err := c.resumeRemainingPhase(input, distances); err != nil {
		return err
	}
	legs := c.remainingLegPlans(profile)
	if err := c.resumeSerialDrain(&legs, distances); err != nil {
		return err
	}
	origin := legs[c.firstLeg].start
	legs[c.firstLeg].start = distances
	for leg := c.firstLeg; leg < len(c.legs); leg++ {
		if err := c.prepareRemainingLeg(leg, legs[leg], origin); err != nil {
			return err
		}
	}
	return c.finishRemainingClock()
}

// resumeRemainingPhase checks the saved leg and dwell of the saved phase. It
// sets the leg, the phase, and the dwell from which the motion resumes.
func (c *couplingMotionContext) resumeRemainingPhase(input couplingRemainingInput, distances [2]float64) error {
	p := &c.reservation
	switch input.Phase {
	case couplingClosing:
		return c.resumeClosing(input, distances)
	case couplingLatching:
		return c.resumeDwell(input, 0, p.LatchTicks, "invalid remaining latch dwell")
	case couplingConnected:
		return c.resumeConnected(input, distances)
	case couplingUnlatching:
		return c.resumeDwell(input, 1, p.UnlatchTicks, "invalid remaining unlatch dwell")
	case couplingOpening:
		return c.resumeOpening(input, distances)
	case couplingDraining:
		if (input.Leg != 3 && input.Leg != 4) || input.DwellTicks != 0 {
			return couplingDenied("invalid serial drain progress")
		}
		return nil
	default:
		return couplingDenied("unknown remaining phase")
	}
}

// resumeClosing resumes the closing leg. The rear member cannot be behind
// its rear staging stop. Members at the closing stops resume in the latch
// dwell.
func (c *couplingMotionContext) resumeClosing(input couplingRemainingInput, distances [2]float64) error {
	p := &c.reservation
	if input.Leg != 0 || input.DwellTicks != 0 || distances[1] < c.rearStagingStop() {
		return couplingDenied("invalid closing progress")
	}
	if distances == p.ClosingStops {
		c.firstLeg, c.initialPhase, c.initialDwell = 1, couplingLatching, p.LatchTicks
	}
	return nil
}

// resumeConnected resumes the connected leg between the closing and split
// stops of the front member. Members at the split stops resume in the
// unlatch dwell.
func (c *couplingMotionContext) resumeConnected(input couplingRemainingInput, distances [2]float64) error {
	p := &c.reservation
	if input.Leg != 1 || input.DwellTicks != 0 || distances[0] < p.ClosingStops[0] || distances[0] > p.SplitStops[0] {
		return couplingDenied("invalid connected progress")
	}
	if distances == p.SplitStops {
		c.firstLeg, c.initialPhase, c.initialDwell = 2, couplingUnlatching, p.UnlatchTicks
	}
	return nil
}

// resumeOpening resumes the opening leg. Members at the opening stops resume
// in the first serial drain leg.
func (c *couplingMotionContext) resumeOpening(input couplingRemainingInput, distances [2]float64) error {
	if input.Leg != 2 || input.DwellTicks != 0 {
		return couplingDenied("invalid opening progress")
	}
	if distances == c.reservation.OpeningStops {
		c.firstLeg = 3
	}
	return nil
}

// resumeDwell resumes a latch or unlatch dwell that follows the saved leg.
// The saved dwell cannot exceed the profile dwell.
func (c *couplingMotionContext) resumeDwell(input couplingRemainingInput, leg, dwellTicks int, reason string) error {
	if input.Leg != leg || input.DwellTicks > dwellTicks {
		return couplingDenied(reason)
	}
	c.firstLeg, c.initialDwell = leg+1, input.DwellTicks
	return nil
}

// rearStagingStop returns the route distance of the rear staging stop of the
// assembly site for the rear member.
func (c *couplingMotionContext) rearStagingStop() float64 {
	p := &c.reservation
	return p.axisOrigins[1] + p.network.sites[p.network.corridors[p.corridorID].AssemblySiteID].RearStagingMeters
}

// remainingLegPlans returns the plans of the five legs from the start of
// each leg. The drain order selects the member that moves in each drain leg.
func (c *couplingMotionContext) remainingLegPlans(profile CouplingProfile) [5]couplingRemainingLeg {
	p := &c.reservation
	first, second := c.drainOrder[0], c.drainOrder[1]
	middle := p.OpeningStops
	middle[first] = c.terminal[first]
	formation := couplingFormationLegs([2]float64{p.ClosingStops[0], c.rearStagingStop()}, p, profile)
	return [5]couplingRemainingLeg{
		formation[0], formation[1], formation[2],
		{couplingDraining, [2]bool{first == 0, first == 1}, p.OpeningStops, middle, profile.Acceleration, math.Inf(1)},
		{couplingDraining, [2]bool{second == 0, second == 1}, middle, c.terminal, profile.Acceleration, math.Inf(1)},
	}
}

// resumeSerialDrain moves a first drain leg that is complete to the second
// drain leg. A resumed drain leg needs its fixed member at its start, and its
// moving member between its start and end, short of the terminal stops.
func (c *couplingMotionContext) resumeSerialDrain(legs *[5]couplingRemainingLeg, distances [2]float64) error {
	if c.firstLeg == 3 && distances == legs[4].start {
		c.firstLeg = 4
	}
	if c.firstLeg < 3 {
		return nil
	}
	leg := legs[c.firstLeg]
	moving := c.drainOrder[c.firstLeg-3]
	fixed := 1 - moving
	if distances[fixed] != leg.start[fixed] || distances[moving] < leg.start[moving] || distances[moving] > leg.end[moving] || distances == c.terminal {
		return couplingDenied("invalid or terminal serial drain coordinates")
	}
	return nil
}

// prepareRemainingLeg prepares one leg that is not complete. A drain leg
// first needs ordinary swept separation.
func (c *couplingMotionContext) prepareRemainingLeg(leg int, plan couplingRemainingLeg, origin [2]float64) error {
	if leg >= 3 && !c.serialSeparation(c.drainOrder[leg-3], plan.start, plan.end) {
		return couplingDenied("remaining drain lacks ordinary swept separation")
	}
	speedCap := plan.speedCap
	var err error
	// A resumed leg keeps the speed cap of its original start. The lanes
	// behind the saved position can be slower than the lanes ahead.
	if leg == c.firstLeg {
		if speedCap, err = c.legSpeedCap(plan.moving, origin, plan.end, plan.acceleration, speedCap); err != nil {
			return err
		}
	}
	c.legs[leg], err = c.prepareLeg(plan.phase, plan.moving, plan.start, plan.end, plan.acceleration, speedCap)
	return err
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
