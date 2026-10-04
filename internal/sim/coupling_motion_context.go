package sim

import (
	"math"
	"slices"
	"sort"
)

type couplingMotionLeg struct {
	phase        couplingReservationPhase
	moving       [2]bool
	start, end   [2]float64
	schedules    [2]couplingSchedule
	ticks        uint64
	cap, braking float64
}

type couplingMotionContext struct {
	reservation    couplingReservationPlan
	owner          resourceOwner
	claims         []couplingClaim
	dependencies   []couplingDependency
	through        [2]int
	terminal       [2]float64
	legs           [5]couplingMotionLeg
	drainOrder     [2]int
	ticks          uint64
	foreignIDs     []string
	events         []couplingOwnerEvent
	drainageProved bool
}

type couplingMotionContextInput struct {
	Reservation couplingReservationPlan
	Current     couplingReservationInput
	GroupID     string
	ForeignIDs  []string
}

// The context is private and inactive. Preparation writes no live grants.
func prepareCouplingMotionContext(input couplingMotionContextInput) (*couplingMotionContext, error) {
	if !boundedContractID(input.GroupID) || len(input.ForeignIDs)+2 > expressMaxPods {
		return nil, couplingDenied("invalid motion group or fleet identities")
	}
	if _, err := revalidateCouplingReservation(input.Reservation, input.Current); err != nil {
		return nil, err
	}
	c := &couplingMotionContext{reservation: input.Reservation, owner: resourceOwner{kind: groupOwnerKind, id: input.GroupID}, foreignIDs: slices.Clone(input.ForeignIDs)}
	for i := range c.reservation.members {
		c.reservation.members[i] = cloneCouplingMember(c.reservation.members[i])
		blocks, err := c.reservation.network.routeBlocks(c.reservation.members[i].Vehicle.Route)
		if err != nil {
			return nil, err
		}
		c.reservation.routes[i] = blocks
	}
	c.reservation.waiting = slices.Clone(c.reservation.waiting)
	c.reservation.Claims = slices.Clone(c.reservation.Claims)
	c.reservation.PreservedClaims = slices.Clone(c.reservation.PreservedClaims)
	c.reservation.Dependencies = slices.Clone(c.reservation.Dependencies)
	slices.Sort(c.foreignIDs)
	for i, id := range c.foreignIDs {
		if !boundedContractID(id) || id == c.reservation.members[0].Vehicle.Pod.ID || id == c.reservation.members[1].Vehicle.Pod.ID || i > 0 && id == c.foreignIDs[i-1] {
			return nil, couplingDenied("invalid complete foreign fleet identity set")
		}
	}
	if err := c.closeExits(input.Current.Owners); err != nil {
		return nil, err
	}
	profile, _ := LookupCouplingProfile(c.reservation.network.contract)
	initial := [2]float64{c.reservation.members[0].Distance, c.reservation.members[1].Distance}
	var err error
	c.legs[0], err = c.prepareLeg(couplingClosing, [2]bool{false, true}, initial, c.reservation.ClosingStops, profile.ManeuverAcceleration, profile.ManeuverSpeed)
	if err != nil {
		return nil, err
	}
	c.legs[1], err = c.prepareLeg(couplingConnected, [2]bool{true, true}, c.reservation.ClosingStops, c.reservation.SplitStops, profile.Acceleration, math.Inf(1))
	if err != nil {
		return nil, err
	}
	c.legs[2], err = c.prepareLeg(couplingOpening, [2]bool{true, false}, c.reservation.SplitStops, c.reservation.OpeningStops, profile.ManeuverAcceleration, profile.ManeuverSpeed)
	if err != nil {
		return nil, err
	}
	if err := c.prepareDrain(profile); err != nil {
		return nil, err
	}
	if profile.LatchTicks < 0 || profile.UnlatchTicks < 0 {
		return nil, couplingDenied("negative motion dwell")
	}
	c.ticks = uint64(profile.LatchTicks) + uint64(profile.UnlatchTicks)
	if c.ticks > math.MaxInt64 {
		return nil, couplingDenied("motion dwell overflows")
	}
	for _, leg := range &c.legs {
		if leg.ticks > math.MaxInt64-c.ticks {
			return nil, couplingDenied("combined motion duration overflows")
		}
		c.ticks += leg.ticks
	}
	if c.ticks > math.MaxInt64 {
		return nil, couplingDenied("combined motion duration overflows")
	}
	motionTicks := int64(c.ticks)
	if input.Current.Tick > math.MaxInt64-motionTicks {
		return nil, couplingDenied("motion clock would overflow")
	}
	if err := c.prepareOwnerEvents(); err != nil {
		return nil, err
	}
	return c, nil
}

func couplingJointDependency(d couplingDependency) bool {
	return d.Site || d.AxisUse || d.MemberUse == ([2]bool{true, true})
}

// Each extension visits new canonical cells. Shared occurrences only extend bounds.
func (c *couplingMotionContext) closeExits(owners map[resource]resourceOwner) error {
	p := &c.reservation
	dependencies := make(map[resource]couplingDependency, len(p.Dependencies))
	for _, d := range p.Dependencies {
		dependencies[d.Resource] = d
	}
	bounds := p.OpeningStops
	for _, d := range p.Dependencies {
		if couplingJointDependency(d) {
			for i, used := range d.MemberUse {
				if used {
					bounds[i] = max(bounds[i], d.MemberRelease[i])
				}
			}
		}
	}
	var boundary, stopRoom [2]float64
	for i := range p.routes {
		c.through[i] = p.Exits[i].Through
		var err error
		boundary[i], err = c.receivingBoundary(i)
		if err != nil {
			return err
		}
		lane, _, _, err := couplingMotionPose(&p.routes[i], p.OpeningStops[i])
		if err != nil {
			return err
		}
		speedLimit := p.routes[i].route[lane].SpeedLimit
		stopRoom[i] = speedLimit*speedLimit/4 + speedLimit/TicksPerSecond
		if !finite(stopRoom[i]) {
			return couplingDenied("exit stopping envelope is not finite")
		}
	}
	for {
		changed := false
		for i := range p.routes {
			q, err := couplingMotionQuantum(bounds[i], p.OpeningStops[i])
			if err != nil {
				return err
			}
			c.terminal[i] = math.Ceil(bounds[i]/q) * q
			// A handoff requires a positive serial leg and room before the actual stop.
			if c.terminal[i] <= p.OpeningStops[i] || c.terminal[i]+stopRoom[i] >= boundary[i] {
				return couplingDenied("actual continuation cannot close joint release and stopping room")
			}
			blocks := p.routes[i]
			old := c.through[i]
			for c.through[i]+1 < blocks.len() && blocks.at(c.through[i]).end < c.terminal[i]+stopRoom[i] {
				c.through[i]++
			}
			c.through[i] = reservationEnd(&blocks, c.through[i])
			if c.through[i] >= blocks.len() || blocks.at(c.through[i]).end >= boundary[i] {
				return couplingDenied("exit closure reaches an actual receiving boundary")
			}
			if c.through[i] == old {
				continue
			}
			changed = true
			if err := p.network.proveInteractions(&blocks, old+1, c.through[i]); err != nil {
				return err
			}
			corridor := p.network.corridors[p.corridorID]
			for _, b := range blocks.span(old+1, c.through[i]+1) {
				for _, r := range b.resources {
					owner := owners[r]
					if !owner.isZero() && !owner.isPod(p.members[0].Vehicle.Pod.ID) && !owner.isPod(p.members[1].Vehicle.Pod.ID) {
						return couplingDenied("exit closure has a foreign typed owner")
					}
					addCouplingDependency(dependencies, r, i, resourceReleaseDistance(b, r), p.axisOrigins[i], b.lane.ID, corridor.LaneIDs)
					d := dependencies[r]
					if couplingJointDependency(d) {
						for member, used := range d.MemberUse {
							if used {
								bounds[member] = max(bounds[member], d.MemberRelease[member])
							}
						}
					}
				}
			}
		}
		if !changed {
			break
		}
	}
	for r, d := range dependencies {
		owner := owners[r]
		if !owner.isZero() && !owner.isPod(p.members[0].Vehicle.Pod.ID) && !owner.isPod(p.members[1].Vehicle.Pod.ID) {
			return couplingDenied("complete closure has a foreign typed owner")
		}
		c.claims = append(c.claims, couplingClaim{Resource: r, Expected: owner})
		c.dependencies = append(c.dependencies, d)
	}
	slices.SortFunc(c.claims, func(a, b couplingClaim) int { return compareCouplingResource(a.Resource, b.Resource) })
	slices.SortFunc(c.dependencies, func(a, b couplingDependency) int { return compareCouplingResource(a.Resource, b.Resource) })
	return nil
}

func (c *couplingMotionContext) receivingBoundary(member int) (float64, error) {
	p := &c.reservation
	m := p.members[member]
	blocks := p.routes[member]
	boundary := blocks.lanes[len(blocks.route)].start
	for _, id := range m.Vehicle.Stops {
		station, ok := p.network.prepared.network.Station(id)
		if !ok {
			return 0, couplingDenied("motion stop disappeared")
		}
		for i, lane := range blocks.route {
			end := blocks.lanes[i+1].start
			if end <= m.Distance {
				continue
			}
			if station.isEntry(lane.To) || slices.ContainsFunc(station.Berths, func(b Berth) bool { return b.Node == lane.To }) {
				boundary = min(boundary, end)
			}
		}
	}
	return boundary, nil
}

func (c *couplingMotionContext) prepareLeg(phase couplingReservationPhase, moving [2]bool, start, end [2]float64, acceleration, profileCap float64) (couplingMotionLeg, error) {
	leg := couplingMotionLeg{phase: phase, moving: moving, start: start, end: end, cap: profileCap, braking: acceleration}
	for i := range moving {
		if !moving[i] {
			if start[i] != end[i] {
				return leg, couplingDenied("stationary motion leg changes distance")
			}
			continue
		}
		speedLimit, err := c.legCap(i, start[i], end[i], profileCap, acceleration)
		if err != nil {
			return leg, err
		}
		leg.cap = min(leg.cap, speedLimit)
	}
	quantum, err := couplingMotionQuantum(start[0], start[1], end[0], end[1])
	if err != nil {
		return leg, err
	}
	for i := range moving {
		if !moving[i] {
			continue
		}
		leg.schedules[i], err = prepareCouplingSchedule(couplingScheduleInput{Start: start[i], End: end[i], Quantum: quantum, Acceleration: acceleration, SpeedCap: leg.cap})
		if err != nil {
			return leg, err
		}
		if leg.ticks != 0 && (leg.ticks != leg.schedules[i].ticks || leg.schedules[0].distanceUnits != leg.schedules[1].distanceUnits) {
			return leg, couplingDenied("connected members lack one exact speed schedule")
		}
		leg.ticks = leg.schedules[i].ticks
	}
	return leg, nil
}

func (c *couplingMotionContext) legCap(member int, start, end, profileCap, braking float64) (float64, error) {
	blocks := c.reservation.routes[member]
	first, _, _, err := couplingMotionPose(&blocks, start)
	if err != nil {
		return 0, err
	}
	speedLimit := min(profileCap, blocks.route[first].SpeedLimit)
	for i := first + 1; i < len(blocks.route) && blocks.lanes[i].start <= end; i++ {
		speedLimit = min(speedLimit, blocks.route[i].SpeedLimit)
	}
	reach := end + speedLimit*speedLimit/(2*braking) + speedLimit/TicksPerSecond
	if !finite(reach) || reach > blocks.at(c.through[member]).end {
		return 0, couplingDenied("physical frontier lacks the complete leg stopping envelope")
	}
	for i := first; i < len(blocks.route) && blocks.lanes[i].start <= reach; i++ {
		speedLimit = min(speedLimit, blocks.route[i].SpeedLimit)
	}
	if !finite(speedLimit) || speedLimit <= 0 {
		return 0, couplingDenied("invalid bounded leg speed speedLimit")
	}
	return speedLimit, nil
}

func (c *couplingMotionContext) prepareDrain(profile CouplingProfile) error {
	start := c.reservation.OpeningStops
	for _, order := range [][2]int{{0, 1}, {1, 0}} {
		middle := start
		middle[order[0]] = c.terminal[order[0]]
		if !c.serialSeparation(order[0], start, middle) || !c.serialSeparation(order[1], middle, c.terminal) {
			continue
		}
		first, err := c.prepareLeg(couplingDraining, [2]bool{order[0] == 0, order[0] == 1}, start, middle, profile.Acceleration, math.Inf(1))
		if err != nil {
			continue
		}
		second, err := c.prepareLeg(couplingDraining, [2]bool{order[1] == 0, order[1] == 1}, middle, c.terminal, profile.Acceleration, math.Inf(1))
		if err != nil {
			continue
		}
		c.legs[3], c.legs[4], c.drainOrder = first, second, order
		return nil
	}
	return couplingDenied("neither serial drain proves the full ordinary swept trace")
}

// Twelve-meter center separation encloses both rotated four-by-two bodies.
func (c *couplingMotionContext) serialSeparation(moving int, start, end [2]float64) bool {
	stationary := 1 - moving
	_, fixed, _, err := couplingMotionPose(&c.reservation.routes[stationary], start[stationary])
	if err != nil {
		return false
	}
	segments, err := couplingMotionSegments(&c.reservation.routes[moving], start[moving], end[moving])
	if err != nil {
		return false
	}
	for _, s := range segments {
		if segmentPointDistance(fixed, s.from, s.to) < Clearance-conflictSlack {
			return false
		}
	}
	return true
}

// Binary searches use private route values. They never write prepared caches.
func couplingMotionPose(blocks *blockList, distance float64) (int, Point, Point, error) {
	if !finite(distance) || distance < 0 || len(blocks.route) == 0 {
		return 0, Point{}, Point{}, couplingDenied("invalid canonical motion distance")
	}
	lane := sort.Search(len(blocks.route), func(i int) bool { return blocks.lanes[i+1].start > distance })
	if lane == len(blocks.route) {
		return 0, Point{}, Point{}, couplingDenied("motion leaves its actual route")
	}
	geometry := blocks.lanes[lane].geometry
	local := distance - blocks.lanes[lane].start
	index := sort.Search(len(geometry.segments), func(i int) bool { return geometry.segments[i].end >= local })
	if index == len(geometry.segments) {
		return 0, Point{}, Point{}, couplingDenied("motion leaves canonical lane segments")
	}
	s := geometry.segments[index]
	length := s.end - s.start
	return lane, couplingSegmentPoint(s, local), Point{X: (s.to.X - s.from.X) / length, Y: (s.to.Y - s.from.Y) / length}, nil
}

func couplingMotionSegments(blocks *blockList, start, end float64) ([]laneSegment, error) {
	lane, point, _, err := couplingMotionPose(blocks, start)
	if err != nil {
		return nil, err
	}
	if end < start {
		return nil, couplingDenied("reverse motion sweep")
	}
	if start == end {
		return []laneSegment{{from: point, to: point}}, nil
	}
	var result []laneSegment
	for i := lane; i < len(blocks.route) && blocks.lanes[i].start < end; i++ {
		entry := blocks.lanes[i]
		lo, hi := max(start-entry.start, 0), min(end-entry.start, entry.length)
		for _, s := range entry.geometry.segments {
			a, b := max(lo, s.start), min(hi, s.end)
			if a < b {
				result = append(result, laneSegment{from: couplingSegmentPoint(s, a), to: couplingSegmentPoint(s, b), start: a + entry.start, end: b + entry.start})
			}
		}
	}
	if len(result) == 0 || result[len(result)-1].end != end {
		return nil, couplingDenied("motion sweep lacks its complete canonical end")
	}
	return result, nil
}
