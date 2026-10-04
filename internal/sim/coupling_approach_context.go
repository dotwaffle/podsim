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
func prepareCouplingApproach(input couplingApproachPrepareInput) (*couplingApproachContext, couplingApproachState, error) {
	s, n := input.Simulation, input.Network
	if s == nil || n == nil || n.prepared == nil || input.Prepared != n.prepared || !input.Enabled || s.platooning != PlatooningVirtual ||
		n.contract != CompactPairV1CouplingContract || s.tick < 0 || ValidateOrderContract(s.orderContract) != nil ||
		!reflect.DeepEqual(s.network, n.prepared.network) || input.Members[0] == input.Members[1] {
		return nil, couplingApproachState{}, couplingDenied("invalid native approach contract, mode, or network")
	}
	corridor, exists := n.corridors[input.CorridorID]
	if !exists {
		return nil, couplingApproachState{}, couplingDenied("unknown native approach corridor")
	}
	front, rear := s.findVehicle(input.Members[0]), s.findVehicle(input.Members[1])
	if front == nil || rear == nil || front.link.leader != 0 || rear.follower != 0 ||
		front.follower <= 0 || front.follower > len(s.vehicles) || &s.vehicles[front.follower-1] != rear ||
		rear.link.leader <= 0 || rear.link.leader > len(s.vehicles) || &s.vehicles[rear.link.leader-1] != front ||
		rear.link.buffer || rear.link.compact || rear.link.draining || rear.link.turn != 0 || rear.link.clearance != linkClearance(0) {
		return nil, couplingApproachState{}, couplingDenied("approach needs one existing straight virtual pair")
	}
	profile, _ := LookupCouplingProfile(n.contract)
	c := &couplingApproachContext{network: n, orderContract: s.orderContract, corridorID: input.CorridorID,
		link: rear.link, tick: s.tick, waitTicks: int64(profile.PartnerWaitTicks)}
	assembly := n.sites[corridor.AssemblySiteID]
	for i, v := range []*vehicle{front, rear} {
		if !boundedContractID(v.Pod.ID) || v.Pod.Class != CompactClass || v.Pod.Activity != Traveling || v.Pod.Speed != 0 ||
			v.Pod.StationPhase != "" || v.Pod.ManeuverStationID != "" || v.Pod.BerthID != "" || v.Pod.StationID != "" ||
			v.Pod.LaneID != assembly.LaneID || s.compactGroup(v) != nil || v.Pod.Occupied != (v.PassengersAboard() > 0) || v.LegacyCohort ||
			v.Pod.Occupied != front.Pod.Occupied || v.Presentation != nil || v.PlatoonID != "" || v.PlatoonIndex != 0 {
			return nil, couplingApproachState{}, couplingDenied("approach member has incompatible occupancy or maneuver")
		}
		if err := couplingCabinFacts(couplingApproachVehicle(v), s.orderContract, s.tick, s.network); err != nil {
			return nil, couplingApproachState{}, err
		}
		for _, trip := range s.waiting {
			if trip.request.PodID == v.Pod.ID {
				return nil, couplingApproachState{}, couplingDenied("approach member has a pending pickup")
			}
		}
		if len(v.Route) > newRouteLimits(s.network).pod {
			return nil, couplingApproachState{}, couplingDenied("approach full route exceeds native route bound")
		}
		blocks, err := n.routeBlocks(v.Route)
		if err != nil {
			return nil, couplingApproachState{}, err
		}
		if err := couplingApproachPose(v); err != nil {
			return nil, couplingApproachState{}, err
		}
		if v.reservedThrough < v.blockIndex || v.reservedThrough >= v.blocks.len() {
			return nil, couplingApproachState{}, couplingDenied("approach has invalid native grant bounds")
		}
		first := slices.IndexFunc(v.Route, func(lane Lane) bool { return lane.ID == corridor.LaneIDs[0] })
		if first < 0 || first+len(corridor.LaneIDs) > len(v.Route) {
			return nil, couplingApproachState{}, couplingDenied("approach route lacks full corridor")
		}
		for j, id := range corridor.LaneIDs {
			if v.Route[first+j].ID != id || s.laneCells[id] != n.prepared.laneCells[id] {
				return nil, couplingApproachState{}, couplingDenied("approach route or native cell identity changed")
			}
		}
		static := couplingReservationPlan{network: n}
		static.routes[i] = blocks
		static.members[i] = couplingMemberSnapshot{Vehicle: v.Vehicle, Distance: v.distance, Destination: v.destination, DestinationStation: v.destinationStation}
		if err := static.prepareExit(i, first+len(corridor.LaneIDs)-1, corridor); err != nil {
			return nil, couplingApproachState{}, err
		}
		origin := blocks.lanes[first].start
		if i == 0 {
			c.start, c.target = origin+assembly.RearStagingMeters, origin+assembly.FrontStagingMeters
			if v.distance != c.start || v.Pod.LaneDistance != assembly.RearStagingMeters {
				return nil, couplingApproachState{}, couplingDenied("approach front is not at its original ordinary frontier")
			}
		} else {
			c.rearTarget = origin + assembly.RearStagingMeters
			if v.distance > c.rearTarget || leaderPosition(v, front, v.link)-v.distance < v.link.clearance {
				return nil, couplingApproachState{}, couplingDenied("approach rear is past its target or virtual clearance")
			}
			c.ceiling = blocks.laneFirst(first)
			for c.ceiling < blocks.laneFirst(first+1) && blocks.end(c.ceiling) < c.rearTarget {
				c.ceiling++
			}
			if c.ceiling >= blocks.laneFirst(first+1) || blocks.end(c.ceiling) != c.rearTarget ||
				c.ceiling > v.link.end || c.ceiling < blocks.laneFirst(v.link.lane) || reservationEnd(&blocks, c.ceiling) != c.ceiling ||
				v.reservedThrough > c.ceiling || blockTail(blocks.at(c.ceiling)) > Clearance {
				return nil, couplingApproachState{}, couplingDenied("approach rear ceiling spills or retains a larger tail")
			}
		}
		c.members[i] = couplingApproachMember{id: v.Pod.ID, routeVersion: v.routeVersion, route: cloneLanes(v.Route), cabin: couplingApproachCabin(v),
			origin: v.origin, journeyOrigin: v.journeyOrigin, destination: v.destination, destinationID: v.destinationStation, riddenBase: v.riddenBase}
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
	quantum, err := couplingMotionQuantum(c.start, c.target)
	if err != nil {
		return nil, couplingApproachState{}, err
	}
	speedLimit := min(assemblyLaneLimit(n, assembly), math.Sqrt(acceleration*(c.target-c.start)))
	c.schedule, err = prepareCouplingSchedule(couplingScheduleInput{Start: c.start, End: c.target, Quantum: quantum, Acceleration: acceleration, SpeedCap: speedLimit})
	if err != nil {
		return nil, couplingApproachState{}, err
	}
	state := couplingApproachState{context: c, Tick: s.tick, Phase: couplingApproachGrant, WaitTick: -1, DeadlineTick: -1}
	return c, state, nil
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
	for resources := range v.blocks.spanResources(v.blockIndex, v.reservedThrough+1) {
		for _, r := range resources {
			if !s.owners[r].isPod(v.Pod.ID) {
				return 0, couplingMotionInvariant("approach front lost its actual granted owner")
			}
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
