package sim

import (
	"maps"
	"math"
	"reflect"
	"slices"
)

type couplingApproachMember struct {
	id            string
	routeVersion  uint64
	route         []Lane
	cabin         Vehicle
	origin        Berth
	journeyOrigin Berth
	destination   Berth
	destinationID string
	riddenBase    float64
}

type couplingApproachContext struct {
	network         *couplingReservationNetwork
	orderContract   OrderContract
	corridorID      string
	members         [2]couplingApproachMember
	link            platoonLink
	tick, waitTicks int64
	start, target   float64
	rearTarget      float64
	ceiling         int
	schedule        couplingSchedule
}

type couplingApproachPrepareInput struct {
	Simulation *Simulation
	Network    *couplingReservationNetwork
	Prepared   *PreparedNetwork
	CorridorID string
	Members    [2]string
	Enabled    bool
}

// Preparation binds actual native members. It does not construct a stopped pose.
// The checks run in a fixed order, because the first refusal is part of the
// result: the input and corridor, the virtual pair, each member in turn
// (front first), the staging profile, the rear claims, and the front frontier.
func prepareCouplingApproach(input couplingApproachPrepareInput) (*couplingApproachContext, couplingApproachState, error) {
	s, n := input.Simulation, input.Network
	if !couplingApproachInputValid(input) {
		return nil, couplingApproachState{}, couplingDenied("invalid native approach contract, mode, or network")
	}
	corridor, exists := n.corridors[input.CorridorID]
	if !exists {
		return nil, couplingApproachState{}, couplingDenied("unknown native approach corridor")
	}
	front, rear := s.findVehicle(input.Members[0]), s.findVehicle(input.Members[1])
	if !couplingApproachVirtualPair(s, front, rear) || !couplingApproachStraightLink(rear.link) {
		return nil, couplingApproachState{}, couplingDenied("approach needs one existing straight virtual pair")
	}
	profile, _ := LookupCouplingProfile(n.contract)
	c := &couplingApproachContext{network: n, orderContract: s.orderContract, corridorID: input.CorridorID,
		link: rear.link, tick: s.tick, waitTicks: int64(profile.PartnerWaitTicks)}
	assembly := n.sites[corridor.AssemblySiteID]
	for i, v := range []*vehicle{front, rear} {
		if err := c.bindMember(s, i, v, front, corridor, assembly); err != nil {
			return nil, couplingApproachState{}, err
		}
	}
	if c.target-c.start != Clearance || front.blocks.end(front.reservedThrough) != c.start || c.waitTicks != 5*TicksPerSecond {
		return nil, couplingApproachState{}, couplingDenied("approach target, original frontier, or wait profile changed")
	}
	if err := c.checkRearClaims(s, front, rear); err != nil {
		return nil, couplingApproachState{}, err
	}
	if _, err := couplingApproachOwnedFrontier(s, front); err != nil {
		return nil, couplingApproachState{}, err
	}
	if err := c.prepareSchedule(assembly); err != nil {
		return nil, couplingApproachState{}, err
	}
	state := couplingApproachState{context: c, Tick: s.tick, Phase: couplingApproachGrant, WaitTick: -1, DeadlineTick: -1}
	return c, state, nil
}

// couplingApproachInputValid reports whether the input binds the prepared
// network, an enabled virtual pair mode, a valid order contract, and two
// different members.
func couplingApproachInputValid(input couplingApproachPrepareInput) bool {
	s, n := input.Simulation, input.Network
	if s == nil || n == nil || n.prepared == nil || input.Prepared != n.prepared {
		return false
	}
	return input.Enabled && s.platooning == PlatooningVirtual && n.contract == CompactPairV1CouplingContract &&
		s.tick >= 0 && ValidateOrderContract(s.orderContract) == nil &&
		reflect.DeepEqual(s.network, n.prepared.network) && input.Members[0] != input.Members[1]
}

// couplingApproachVirtualPair reports whether rear is the virtual follower
// of front, and neither member has another virtual partner.
func couplingApproachVirtualPair(s *Simulation, front, rear *vehicle) bool {
	if front == nil || rear == nil || front.link.leader != 0 || rear.follower != 0 {
		return false
	}
	return front.follower > 0 && front.follower <= len(s.vehicles) && &s.vehicles[front.follower-1] == rear &&
		rear.link.leader > 0 && rear.link.leader <= len(s.vehicles) && &s.vehicles[rear.link.leader-1] == front
}

// couplingApproachStraightLink reports whether the rear link is an
// ordinary straight link: no buffer, compact, or drain state, no turn, and
// the clearance of a straight link.
func couplingApproachStraightLink(link platoonLink) bool {
	return !link.buffer && !link.compact && !link.draining && link.turn == 0 && link.clearance == linkClearance(0)
}

// bindMember checks member i (0 is the front, 1 is the rear) and records
// its binding. A member completes all its checks before the next member
// starts.
func (c *couplingApproachContext) bindMember(s *Simulation, i int, v, front *vehicle, corridor CouplingCorridor, assembly CouplingSite) error {
	if !couplingApproachMemberReady(s, v, front, assembly.LaneID) {
		return couplingDenied("approach member has incompatible occupancy or maneuver")
	}
	if err := couplingCabinFacts(couplingApproachVehicle(v), s.orderContract, s.tick, s.network); err != nil {
		return err
	}
	if s.assigned(v.Pod.ID) {
		return couplingDenied("approach member has a pending pickup")
	}
	blocks, first, err := c.memberRoute(s, i, v, corridor)
	if err != nil {
		return err
	}
	origin := blocks.lanes[first].start
	if i == 0 {
		err = c.stageFront(v, origin, assembly)
	} else {
		err = c.stageRear(v, front, &blocks, first, origin, assembly)
	}
	if err != nil {
		return err
	}
	c.members[i] = couplingApproachMember{id: v.Pod.ID, routeVersion: v.routeVersion, route: cloneLanes(v.Route), cabin: couplingApproachCabin(v),
		origin: v.origin, journeyOrigin: v.journeyOrigin, destination: v.destination, destinationID: v.destinationStation, riddenBase: v.riddenBase}
	return nil
}

// couplingApproachMemberReady reports whether v travels at rest on the
// assembly lane with no station, maneuver, group, or presentation state,
// and with the same occupancy as the front.
func couplingApproachMemberReady(s *Simulation, v, front *vehicle, laneID string) bool {
	return boundedContractID(v.Pod.ID) && v.Pod.Class == CompactClass && v.Pod.Activity == Traveling && v.Pod.Speed == 0 &&
		v.Pod.StationPhase == "" && v.Pod.ManeuverStationID == "" && v.Pod.BerthID == "" && v.Pod.StationID == "" &&
		v.Pod.LaneID == laneID && s.compactGroup(v) == nil && v.Pod.Occupied == (v.PassengersAboard() > 0) &&
		v.Pod.Occupied == front.Pod.Occupied && v.Presentation == nil && v.PlatoonID == "" && v.PlatoonIndex == 0
}

// memberRoute checks the native route of member i. The route must be in
// bounds, match the pose and grants, contain the full corridor, and have
// an exit plan. It returns the route blocks and the route index of the
// first corridor lane.
func (c *couplingApproachContext) memberRoute(s *Simulation, i int, v *vehicle, corridor CouplingCorridor) (blockList, int, error) {
	n := c.network
	if len(v.Route) > newRouteLimits(s.network).pod {
		return blockList{}, 0, couplingDenied("approach full route exceeds native route bound")
	}
	blocks, err := n.routeBlocks(v.Route)
	if err != nil {
		return blockList{}, 0, err
	}
	err = couplingApproachPose(v)
	if err != nil {
		return blockList{}, 0, err
	}
	if v.reservedThrough < v.blockIndex || v.reservedThrough >= v.blocks.len() {
		return blockList{}, 0, couplingDenied("approach has invalid native grant bounds")
	}
	first, err := couplingApproachCorridorStart(s, n, v.Route, corridor)
	if err != nil {
		return blockList{}, 0, err
	}
	static := couplingReservationPlan{network: n}
	static.routes[i] = blocks
	static.members[i] = couplingMemberSnapshot{Vehicle: v.Vehicle, Distance: v.distance, Destination: v.destination, DestinationStation: v.destinationStation}
	if err := static.prepareExit(i, first+len(corridor.LaneIDs)-1, corridor); err != nil {
		return blockList{}, 0, err
	}
	return blocks, first, nil
}

// couplingApproachCorridorStart returns the route index of the first
// corridor lane. The route must contain the full corridor in order, and
// each corridor lane must keep its prepared native cells.
func couplingApproachCorridorStart(s *Simulation, n *couplingReservationNetwork, route []Lane, corridor CouplingCorridor) (int, error) {
	first := slices.IndexFunc(route, func(lane Lane) bool { return lane.ID == corridor.LaneIDs[0] })
	if first < 0 || first+len(corridor.LaneIDs) > len(route) {
		return 0, couplingDenied("approach route lacks full corridor")
	}
	for j, id := range corridor.LaneIDs {
		if route[first+j].ID != id || s.laneCells[id] != n.prepared.laneCells[id] {
			return 0, couplingDenied("approach route or native cell identity changed")
		}
	}
	return first, nil
}

// stageFront binds the approach start and target. The front must stand at
// its original ordinary frontier, the rear staging point of the assembly
// site.
func (c *couplingApproachContext) stageFront(v *vehicle, origin float64, assembly CouplingSite) error {
	c.start, c.target = origin+assembly.RearStagingMeters, origin+assembly.FrontStagingMeters
	// The route distance binds exactly. Its lane-local copy can differ
	// from the authored offset by rounding, as in the reservation check.
	if v.distance != c.start || math.Abs(v.Pod.LaneDistance-assembly.RearStagingMeters) > conflictSlack {
		return couplingDenied("approach front is not at its original ordinary frontier")
	}
	return nil
}

// stageRear binds the rear target and the rear grant ceiling. The ceiling
// is the first block of the first corridor lane that ends at the rear
// target.
func (c *couplingApproachContext) stageRear(v, front *vehicle, blocks *blockList, first int, origin float64, assembly CouplingSite) error {
	c.rearTarget = origin + assembly.RearStagingMeters
	if v.distance > c.rearTarget || leaderPosition(v, front, v.link)-v.distance < v.link.clearance {
		return couplingDenied("approach rear is past its target or virtual clearance")
	}
	c.ceiling = blocks.laneFirst(first)
	for c.ceiling < blocks.laneFirst(first+1) && blocks.end(c.ceiling) < c.rearTarget {
		c.ceiling++
	}
	if c.ceiling >= blocks.laneFirst(first+1) || blocks.end(c.ceiling) != c.rearTarget ||
		c.ceiling > v.link.end || c.ceiling < blocks.laneFirst(v.link.lane) || reservationEnd(blocks, c.ceiling) != c.ceiling ||
		v.reservedThrough > c.ceiling || blockTail(blocks.at(c.ceiling)) > Clearance {
		return couplingDenied("approach rear ceiling spills or retains a larger tail")
	}
	return nil
}

// prepareSchedule plans the front motion from the start to the target.
func (c *couplingApproachContext) prepareSchedule(assembly CouplingSite) error {
	quantum, err := couplingMotionQuantum(c.start, c.target)
	if err != nil {
		return err
	}
	speedLimit := min(assemblyLaneLimit(c.network, assembly), math.Sqrt(acceleration*(c.target-c.start)))
	c.schedule, err = prepareCouplingSchedule(couplingScheduleInput{Start: c.start, End: c.target, Quantum: quantum, Acceleration: acceleration, SpeedCap: speedLimit})
	return err
}

func assemblyLaneLimit(n *couplingReservationNetwork, site CouplingSite) float64 {
	return n.lanes[site.LaneID].lane.SpeedLimit
}

func couplingApproachCabin(v *vehicle) Vehicle {
	cabin := v.Vehicle
	cabin.Pod = Pod{ID: v.Pod.ID, Class: v.Pod.Class, Occupied: v.Pod.Occupied}
	cabin.Route = nil
	cabin.Riders, cabin.Stops, cabin.Boardings = slices.Clone(v.Riders), slices.Clone(v.Stops), slices.Clone(v.Boardings)
	return cabin
}

func couplingApproachVehicle(v *vehicle) Vehicle {
	visible := v.Vehicle
	visible.Boardings, visible.RiddenMeters = nil, 0
	if !v.legacyBoardingRecords() {
		visible.Boardings = slices.Clone(v.Boardings)
	}
	if v.RidersAboard() > 0 || len(v.Boardings) > 0 {
		visible.RiddenMeters = v.riddenMeters()
	}
	return visible
}

func (c *couplingApproachContext) changed(s *Simulation, front, rear *vehicle, enabled bool) string {
	if !enabled || s.platooning != PlatooningVirtual || s.orderContract != c.orderContract {
		return "approach policy changed"
	}
	for i, v := range []*vehicle{front, rear} {
		m := c.members[i]
		// A deferred member with an emergency record has no hold and no
		// purpose until it leaves the approach (section 5.6 of the
		// incident emergency contract).
		if s.emergencyOf(v) >= 0 {
			return "approach member has an emergency"
		}
		// A member with a hold, a purpose, or a fault record is out of
		// service, and the pair does not couple (Q7).
		if v.withdrawn != 0 || v.op.purpose != opService || v.faulted {
			return "approach member is out of service"
		}
		if v.Pod.Activity != Traveling || v.Pod.StationPhase != "" || v.Pod.ManeuverStationID != "" || s.compactGroup(v) != nil ||
			v.routeVersion != m.routeVersion || !reflect.DeepEqual(v.Route, m.route) || !reflect.DeepEqual(couplingApproachCabin(v), m.cabin) ||
			v.origin != m.origin || v.journeyOrigin != m.journeyOrigin || v.destination != m.destination || v.destinationStation != m.destinationID || v.riddenBase != m.riddenBase {
			return "approach route or cabin binding changed"
		}
		if err := couplingApproachPose(v); err != nil {
			return "approach member pose differs from its actual route"
		}
		for _, trip := range s.waiting {
			if trip.request.PodID == v.Pod.ID {
				return "approach member acquired a pending pickup"
			}
		}
	}
	if front.link.leader != 0 || rear.follower != 0 {
		return "approach acquired another virtual partner"
	}
	if rear.link.leader != 0 {
		link := rear.link
		link.draining = c.link.draining
		if link != c.link || front.follower <= 0 || front.follower > len(s.vehicles) || &s.vehicles[front.follower-1] != rear {
			return "approach virtual certificate changed"
		}
	} else if front.follower != 0 {
		return "approach acquired another virtual partner"
	}
	if err := c.checkRearClaims(s, front, rear); err != nil {
		return "approach rear grants or retained owners changed"
	}
	return ""
}

func (c *couplingApproachContext) checkRearClaims(s *Simulation, front, rear *vehicle) error {
	if rear.reservedThrough < rear.blockIndex || rear.reservedThrough > c.ceiling {
		return couplingDenied("rear grants escaped the staging ceiling")
	}
	if err := couplingApproachRetention(rear); err != nil {
		return err
	}
	for _, r := range rear.footprint(rear.reservedThrough, rear.distance) {
		if !s.owners[r].isPod(rear.Pod.ID) && !s.owners[r].isPod(front.Pod.ID) {
			return couplingDenied("rear current footprint lacks its actual virtual owner")
		}
		if _, retained := rear.routeReleases[r]; !retained && rear.distance >= rear.originTail() {
			return couplingDenied("rear current footprint lacks its retention ledger")
		}
	}
	for r, release := range rear.routeReleases {
		owner := s.owners[r]
		if !finite(release) || release <= rear.distance {
			return couplingDenied("rear retained an invalid release threshold")
		}
		if owner.isPod(rear.Pod.ID) {
			continue
		}
		frontRelease, retained := front.routeReleases[r]
		if !owner.isPod(front.Pod.ID) || !retained || !finite(frontRelease) || frontRelease > c.target ||
			rear.link.leader == 0 || r.kind == trackResource && r.id == rear.Pod.LaneID && r.cell > c.ceiling-rear.blocks.laneFirst(rear.blocks.routeLane(c.ceiling)) {
			return couplingDenied("rear holds a future, foreign, or nontransferable resource")
		}
	}
	return nil
}

func couplingApproachOwnedFrontier(s *Simulation, v *vehicle) (float64, error) {
	if !finite(v.distance) || !finite(v.Pod.Speed) || v.Pod.Speed < 0 || v.blockIndex < 0 || v.reservedThrough < v.blockIndex || v.reservedThrough >= v.blocks.len() {
		return 0, couplingMotionInvariant("approach current grants or motion are invalid")
	}
	if err := couplingApproachPose(v); err != nil {
		return 0, err
	}
	if err := couplingApproachRetention(v); err != nil {
		return 0, err
	}
	// The footprint holds each granted resource that the front has not
	// passed by its ordinary release distance. The front does not own a
	// resource that it passed, such as the From node of its current cell.
	for _, r := range v.footprint(v.reservedThrough, v.distance) {
		if !s.owners[r].isPod(v.Pod.ID) {
			return 0, couplingMotionInvariant("approach front lost its current footprint owner")
		}
		if _, retained := v.routeReleases[r]; !retained && v.distance >= v.originTail() {
			return 0, couplingMotionInvariant("approach front lost its footprint retention ledger")
		}
	}
	for r, release := range v.routeReleases {
		if !finite(release) || release <= v.distance || !s.owners[r].isPod(v.Pod.ID) {
			return 0, couplingMotionInvariant("approach front lost its retained owner")
		}
	}
	frontier := v.blocks.end(v.reservedThrough)
	if v.distance+stoppingDistance(v.Pod.Speed) > frontier {
		return 0, couplingMotionInvariant("approach front cannot brake in its actual grants")
	}
	return frontier, nil
}

func couplingApproachRetention(v *vehicle) error {
	expected := make(map[resource]float64)
	for _, b := range v.blocks.span(0, v.reservedThrough+1) {
		for _, r := range b.resources {
			expected[r] = max(expected[r], resourceReleaseDistance(b, r))
		}
	}
	maps.DeleteFunc(expected, func(_ resource, release float64) bool { return release <= v.distance })
	if !maps.Equal(expected, v.routeReleases) {
		return couplingMotionInvariant("approach retention differs from actual granted route resources")
	}
	return nil
}

func couplingApproachPose(v *vehicle) error {
	if v.blockIndex < 0 || v.blockIndex >= v.blocks.len() || len(v.blocks.lanes) != len(v.Route)+1 ||
		!reflect.DeepEqual(v.blocks.route, v.Route) || !finite(v.distance) || !finite(v.Pod.Speed) || v.Pod.Speed < 0 {
		return couplingMotionInvariant("approach pose has invalid native route state")
	}
	b := v.blocks.at(v.blockIndex)
	if v.distance < b.start || v.distance > b.end || v.Pod.LaneID != b.lane.ID || v.Pod.LaneDistance != v.distance-b.laneStart {
		return couplingMotionInvariant("approach current cell differs from its canonical distance")
	}
	_, point, _, err := couplingMotionPose(&v.blocks, v.distance)
	if err != nil || pointDistance(point, v.Pod.Position) > conflictSlack {
		return couplingMotionInvariant("approach position differs from native route geometry")
	}
	return nil
}

func (c *couplingApproachContext) formationInput(s *Simulation, front, rear *vehicle) couplingReservationInput {
	input := couplingReservationInput{Network: c.network, Prepared: c.network.prepared, CorridorID: c.corridorID,
		OrderContract: s.orderContract, Tick: s.tick, Owners: s.owners}
	for _, trip := range s.waiting {
		input.Waiting = append(input.Waiting, trip.request)
	}
	for i, v := range []*vehicle{front, rear} {
		input.Members[i] = couplingMemberSnapshot{Vehicle: couplingApproachVehicle(v), RouteVersion: v.routeVersion, Distance: v.distance,
			BlockIndex: v.blockIndex, ReservedThrough: v.reservedThrough, Origin: v.origin, Destination: v.destination,
			DestinationStation: v.destinationStation, Retained: maps.Clone(v.routeReleases), nativeRiddenBase: new(v.riddenBase), VirtualLeader: v.link.leader != 0,
			VirtualFollower: v.follower != 0, CompactQueue: s.compactGroup(v) != nil, StationManeuver: v.Pod.StationPhase != "" || v.Pod.ManeuverStationID != ""}
	}
	return input
}
