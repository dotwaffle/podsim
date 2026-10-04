package sim

import (
	"cmp"
	"fmt"
	"math"
	"slices"
)

// Platooning selects whether pods in a queue couple into virtual platoons.
type Platooning int

const (
	// PlatooningOff couples no pods. Each pod stops within its own cells.
	PlatooningOff Platooning = iota
	// PlatooningVirtual couples a slow pod to the pod ahead of it on its
	// lane when both routes share the next lanes. A coupled follower can
	// move into cells that its predecessor holds. See formPlatoons.
	PlatooningVirtual
)

const (
	// MinPlatoonLimit is the smallest limit of pods in one platoon.
	MinPlatoonLimit = 2
	// MaxPlatoonLimit is the largest limit of pods in one platoon, and the
	// default limit.
	MaxPlatoonLimit = 4
	// platoonReactionSeconds is the reaction time of a link. A follower
	// keeps its speed times this time as a gap in addition to the link
	// clearance.
	platoonReactionSeconds = 0.5
	// platoonSlowFraction is the fraction of the lane speed limit below
	// which a pod is slow. Only a slow pod couples, because pods at the
	// limit keep a constant gap.
	platoonSlowFraction = 0.5
	// platoonHorizon is the distance in meters ahead of a new follower
	// within which the lanes of the shared route after the next lane can
	// join the run of a new link.
	platoonHorizon = 300.0
	// platoonMaxTurn is the largest total turn in radians of the run of a
	// link.
	platoonMaxTurn = 2 * math.Pi / 3
	// platoonTurnSlack is the rounding error in radians that a comparison of
	// two turns accepts. The margin in the link clearance covers it.
	platoonTurnSlack = 1e-9
	// platoonMargin is the distance in meters that a link adds to its
	// clearance, so that rounding cannot put two pods closer than
	// Clearance.
	platoonMargin = 0.01
	// platoonDrainSlack is the distance in meters that a link keeps between
	// the release distance of its last shared block and the end of its run.
	platoonDrainSlack = 1.0
)

// platoonLink couples a follower to its predecessor. The two routes share
// a run of lanes. The link certifies the path along the run: its total turn
// from the lane of the follower to the end of the run is at most turn, so a
// path distance of clearance along the run puts two pods at least Clearance
// apart. The link does not change turn and clearance after it forms.
//
// While the follower reserves only blocks up to end that the predecessor
// also reserved, a resource that a pod ahead in the platoon holds is free
// for the follower. The follower then must stop within its cap (see
// platoonCaps). Each pod that holds such a resource releases it at most
// Clearance past its block, which is before the end of the run. Thus each
// pod that the follower can come close to is on the run, and the zero value
// is no link.
type platoonLink struct {
	// buffer fixes the stopping frontier to terminalCell on one entry lane.
	buffer       bool
	compact      bool
	terminalCell int
	first        int
	// leader is one plus the index in Simulation.vehicles of the
	// predecessor, or 0 when the pod has no predecessor.
	leader int
	// lane and leaderLane are the route indexes of the first lane of the
	// run in the follower route and in the predecessor route. lanes is the
	// number of lanes in the run. Each of these lanes is followed by
	// another lane in both routes.
	lane, leaderLane, lanes int
	// end is the last block of the follower route that the follower can
	// reserve as a platoon member. The release distance of each of its
	// resources is at least platoonDrainSlack before the end of the run.
	end int
	// turn is the largest total turn in radians of the run from the lane of
	// the follower. All links of one platoon have the same turn. clearance
	// is the path distance that the link keeps between the two pods and
	// between their stop points.
	turn, clearance float64
	// draining is true after the follower reserved the end block, until
	// the run grows. The follower then makes no coupled grants, also when a
	// restore gives it an earlier reservation frontier, and the link ends
	// when the follower holds no resource of a pod ahead.
	draining bool
}

// laneShape holds the headings of a lane in radians at its start and at
// its end, and the total turn along the lane. empty is true for a lane
// with no length, which has no heading.
type laneShape struct {
	start, end, curve float64
	empty             bool
}

// turnSum adds the turns along a path of lanes. It counts the turn along
// each lane and the turn at each lane join one time. A lane with no length
// does not change the heading.
type turnSum struct {
	turn, heading float64
	started       bool
}

// add returns the sum with the next lane of the path.
func (t turnSum) add(shape laneShape) turnSum {
	if shape.empty {
		return t
	}
	if t.started {
		t.turn += math.Abs(math.Remainder(shape.start-t.heading, 2*math.Pi))
	}
	t.turn += shape.curve
	t.heading, t.started = shape.end, true
	return t
}

// runTurn returns the total turn in radians of the path along the lanes of
// route from index from through index to.
func (s *Simulation) runTurn(route []Lane, from, to int) float64 {
	shapes := s.platoonIndexes().shapes
	var sum turnSum
	for _, lane := range route[from : to+1] {
		sum = sum.add(shapes[lane.ID])
	}
	return sum.turn
}

// linkClearance returns the path distance that puts two pods at least
// Clearance apart on a path that turns by at most turn.
func linkClearance(turn float64) float64 {
	return Clearance/math.Cos(turn/2) + platoonMargin
}

// linkEnds returns the route distance at the end of the run of link, and
// the last block whose resources a follower releases at least
// platoonDrainSlack before that distance, or -1.
func linkEnds(blocks *blockList, link platoonLink) (geometry float64, end int) {
	last := link.lane + link.lanes - 1
	terminal := blocks.laneFirst(last+1) - 1
	if link.buffer {
		terminal = blocks.laneFirst(last) + link.terminalCell
	}
	geometry = blocks.cellEnd(last, terminal-blocks.laneFirst(last))
	lane := last
	first := blocks.laneFirst(link.lane)
	if link.buffer {
		first = link.first
	}
	for index := terminal; index >= first; index-- {
		for index < blocks.laneFirst(lane) {
			lane--
		}
		if blocks.cellEnd(lane, index-blocks.laneFirst(lane))+Clearance+platoonDrainSlack <= geometry {
			return geometry, index
		}
	}
	return geometry, -1
}

// SetPlatooning selects the platooning mode. It returns an error for an
// unknown mode and then changes nothing. With PlatooningOff, no new link
// forms and a follower reserves only free cells. A link that exists stays
// until its follower holds no cell of a pod ahead, so that no pod has to
// stop at once. Reset keeps the mode. The saved state does not keep the
// mode, so the session sets it again after a restore.
func (s *Simulation) SetPlatooning(mode Platooning) error {
	if mode < PlatooningOff || mode > PlatooningVirtual {
		return fmt.Errorf("unknown platooning mode %d", mode)
	}
	s.platooning = mode
	if mode == PlatooningVirtual {
		s.platoonIndexes()
	}
	return nil
}

// Platooning returns the platooning mode.
func (s *Simulation) Platooning() Platooning { return s.platooning }

// SetPlatoonLimit sets the largest number of pods in one platoon. It
// returns an error for a limit out of range and then changes nothing. A
// platoon that is already longer stays until it splits. NewFleet selects
// MaxPlatoonLimit, and Reset keeps the limit.
func (s *Simulation) SetPlatoonLimit(limit int) error {
	if limit < MinPlatoonLimit || limit > MaxPlatoonLimit {
		return fmt.Errorf("platoon limit must be between %d and %d", MinPlatoonLimit, MaxPlatoonLimit)
	}
	if err := s.prepareCompactLimit(limit); err != nil {
		return err
	}
	s.platoonLimit = limit
	return nil
}

// PlatoonLimit returns the largest number of pods in one platoon.
func (s *Simulation) PlatoonLimit() int { return s.platoonLimit }

// CoupledPods returns the number of traveling pods that have a
// predecessor or a follower in a platoon.
func (s *Simulation) CoupledPods() int {
	if s.platoonLinks == 0 {
		return 0
	}
	coupled := 0
	for i := range s.vehicles {
		if v := &s.vehicles[i]; v.Pod.Activity == Traveling && v.coupled() {
			coupled++
		}
	}
	return coupled
}

// coupled reports whether v has a predecessor or a follower.
func (v *vehicle) coupled() bool {
	return v.link.leader != 0 || v.follower != 0
}

// platoonPosition returns the index in s.vehicles of the first pod of the
// platoon of the pod at index, and the position of the pod in the platoon,
// 1 for the first pod. A platoon has no loop, so the walk ends.
func (s *Simulation) platoonPosition(index int) (first, position int) {
	first, position = index, 1
	for leader := s.vehicles[index].link.leader; leader != 0; leader = s.vehicles[leader-1].link.leader {
		first, position = leader-1, position+1
	}
	return first, position
}

// aheadInPlatoon reports whether the pod with the given ID is ahead of v in
// its platoon.
func (s *Simulation) aheadInPlatoon(v *vehicle, id string) bool {
	for leader := v.link.leader; leader != 0; leader = s.vehicles[leader-1].link.leader {
		if s.vehicles[leader-1].Pod.ID == id {
			return true
		}
	}
	return false
}

// releaseRouteResource removes v as the owner of r. The first pod behind v
// in its platoon that reserved r and has not passed it gets r. When no such
// pod exists, r becomes free. The caller must check that v owns r.
func (s *Simulation) releaseRouteResource(v *vehicle, r resource) {
	if !s.owners[r].isPod(v.Pod.ID) {
		return
	}
	for follower := v.follower; follower != 0; follower = s.vehicles[follower-1].follower {
		member := &s.vehicles[follower-1]
		if releaseAt, ok := member.routeReleases[r]; ok && releaseAt > member.distance {
			s.owners[r] = podResourceOwner(member.Pod.ID)
			return
		}
	}
	delete(s.owners, r)
}

// coupledSpan reports whether a follower can reserve blocks from from
// through through as a platoon member. The blocks must be in the run, not
// after the end block of the link, and the predecessor must have reserved
// the same blocks. A link that drains has no coupled grants.
func (s *Simulation) coupledSpan(v *vehicle, from, through int) bool {
	if s.platooning == PlatooningOff || v.link.draining || through > v.link.end || from < v.blocks.laneFirst(v.link.lane) {
		return false
	}
	if v.link.buffer && from < v.link.first {
		return false
	}
	leader := &s.vehicles[v.link.leader-1]
	if leader.Pod.Activity != Traveling {
		return false
	}
	return leaderBlock(v, leader, v.link, through) <= leader.reservedThrough
}

// returnsToShared reports whether the blocks from from through through
// hold a resource that v kept from an earlier reservation and that a pod
// behind v in its platoon also holds. A route that passes a node again
// has such a resource. If v reserved it again, v would keep it past the
// end of the run of the link of that pod, so v must wait until that pod
// passes it.
func (s *Simulation) returnsToShared(v *vehicle, from, through int) bool {
	for resources := range v.blocks.spanResources(from, through+1) {
		for _, r := range resources {
			if _, kept := v.routeReleases[r]; !kept {
				continue
			}
			for follower := v.follower; follower != 0; follower = s.vehicles[follower-1].follower {
				if _, held := s.vehicles[follower-1].routeReleases[r]; held {
					return true
				}
			}
		}
	}
	return false
}

// inLinkRun reports whether the lane at route index index of v is in the
// run of a link of v, as the follower or as the predecessor. A new route
// from that lane would not follow the run.
func (s *Simulation) inLinkRun(v *vehicle, index int) bool {
	if s.compactGroup(v) != nil {
		return true
	}
	if v.link.leader != 0 && index < v.link.lane+v.link.lanes {
		return true
	}
	if v.follower == 0 {
		return false
	}
	link := &s.vehicles[v.follower-1].link
	return index < link.leaderLane+link.lanes
}

// leaderBlock returns the block of the route of leader that is the block at
// index of the route of v. The block must be in the run of link.
func leaderBlock(v, leader *vehicle, link platoonLink, index int) int {
	lane := v.blocks.routeLane(index)
	return leader.blocks.laneFirst(lane-link.lane+link.leaderLane) + index - v.blocks.laneFirst(lane)
}

// holdsPending reports whether v reserved a resource that another pod
// still owns.
func (s *Simulation) holdsPending(v *vehicle) bool {
	for r := range v.routeReleases {
		if s.owners[r] != podResourceOwner(v.Pod.ID) {
			return true
		}
	}
	return false
}

// formPlatoons ends the links that are over and makes new links. It runs
// before admission, from the state after the last movement. maintainLink
// extends or ends each link. Then, in pod ID order, each slow pod without a
// predecessor couples to the pod ahead on its lane when tryLink accepts
// the pair.
func (s *Simulation) formPlatoons() {
	s.formCompactQueues()
	if s.platooning == PlatooningOff && s.platoonLinks == 0 {
		return
	}
	ahead := s.lanePredecessors()
	for i := range s.vehicles {
		if s.vehicles[i].link.leader != 0 {
			s.maintainLink(i, ahead[i])
		}
	}
	if s.platooning == PlatooningOff {
		return
	}
	for i := range s.vehicles {
		if s.vehicles[i].link.leader == 0 && ahead[i] != 0 {
			s.tryLink(i, ahead[i]-1)
		}
	}
}

// lanePredecessors returns, for each pod that has a predecessor or can
// link, one plus the index of the nearest traveling pod ahead of it on its
// lane, or 0. For the other pods, it returns 0. Only the pods on the lanes
// of the first set go into the sort, because at most ticks few pods are
// slow.
func (s *Simulation) lanePredecessors() []int {
	if s.platoonLanes == nil {
		s.platoonLanes = make(map[string]bool)
	}
	lanes := s.platoonLanes
	clear(lanes)
	for i := range s.vehicles {
		if v := &s.vehicles[i]; v.Pod.Activity == Traveling && v.Pod.LaneID != "" && (v.link.leader != 0 || s.canLink(v)) {
			lanes[v.Pod.LaneID] = true
		}
	}
	order := s.platoonOrder[:0]
	if len(lanes) != 0 {
		for i := range s.vehicles {
			if v := &s.vehicles[i]; v.Pod.Activity == Traveling && lanes[v.Pod.LaneID] {
				order = append(order, i)
			}
		}
	}
	slices.SortFunc(order, func(a, b int) int {
		first, second := &s.vehicles[a].Pod, &s.vehicles[b].Pod
		return cmp.Or(cmp.Compare(first.LaneID, second.LaneID), cmp.Compare(second.LaneDistance, first.LaneDistance), cmp.Compare(a, b))
	})
	ahead := s.platoonAhead
	if len(ahead) != len(s.vehicles) {
		ahead = make([]int, len(s.vehicles))
	}
	clear(ahead)
	for k := 1; k < len(order); k++ {
		if s.vehicles[order[k]].Pod.LaneID == s.vehicles[order[k-1]].Pod.LaneID {
			ahead[order[k]] = order[k-1] + 1
		}
	}
	s.platoonOrder, s.platoonAhead = order, ahead
	return ahead
}

// maintainLink extends the run of the link of the pod at index i, or ends
// the link when it is over. ahead is the value of lanePredecessors for the
// pod. A link is over when the mode is off, the predecessor or the
// follower stopped traveling, another pod is between them on the lane of
// the follower, the follower reserved to the end block of the link, or the
// predecessor is Clearance past the end of the run. After the follower
// reserved the end block, the link stays over until the run grows, also
// when a restore gives the follower an earlier reservation frontier. The link ends only
// when the follower holds no resource of a pod ahead. Until then it drains:
// the follower takes no new resource of a pod ahead, and the pods ahead
// hand their resources to it as they pass them.
func (s *Simulation) maintainLink(i, ahead int) {
	v := &s.vehicles[i]
	if v.link.compact {
		return
	}
	leader := &s.vehicles[v.link.leader-1]
	if v.link.buffer && !s.stationBuffers {
		v.link.draining = true
	}
	over := s.platooning == PlatooningOff || v.Pod.Activity != Traveling || leader.Pod.Activity != Traveling ||
		ahead != 0 && ahead != v.link.leader
	if !over {
		if !s.couplingApproachMember(v.Pod.ID) {
			s.extendLink(v, leader)
		}
		geometry, _ := linkEnds(&v.blocks, v.link)
		v.link.draining = v.link.draining || v.reservedThrough >= v.link.end
		over = v.link.draining || leaderPosition(v, leader, v.link) >= geometry+Clearance
	}
	if over && !s.holdsPending(v) {
		s.unlink(v)
	}
}

// unlink ends the link of v.
func (s *Simulation) unlink(v *vehicle) {
	s.vehicles[v.link.leader-1].follower = 0
	v.link = platoonLink{}
	s.platoonLinks--
}

// extendLink adds lanes to the run of a link while the follower has
// reserved into the lane of the end block of the link. A lane joins when it
// is the next lane of both routes, both routes continue after it, it does
// not enter a station, and the total turn from the lane of the follower to
// the lane stays within the turn of the link. The turn and the clearance
// of the link do not change. When the end block moves, the link stops
// draining.
func (s *Simulation) extendLink(v, leader *vehicle) {
	if s.couplingApproachMember(v.Pod.ID) || s.couplingApproachMember(leader.Pod.ID) {
		return
	}
	if v.link.buffer {
		return
	}
	for v.link.end >= 0 && v.reservedThrough >= v.blocks.laneFirst(v.blocks.routeLane(v.link.end)) {
		lane, leaderLane := v.link.lane+v.link.lanes, v.link.leaderLane+v.link.lanes
		if !s.sharedLane(v, leader, lane, leaderLane) {
			return
		}
		current := v.blocks.find(v.blockIndex, &v.blocks.cursors[podCursor]).lane
		if s.runTurn(v.Route, current, lane) > v.link.turn+platoonTurnSlack {
			return
		}
		v.link.lanes++
		if _, end := linkEnds(&v.blocks, v.link); end > v.link.end {
			v.link.end, v.link.draining = end, false
		}
	}
}

// sharedLane reports whether the lane at route index lane of v and the lane
// at route index leaderLane of leader can join a run. It must be one lane,
// it must not enter a station, and both routes must continue after it.
func (s *Simulation) sharedLane(v, leader *vehicle, lane, leaderLane int) bool {
	if lane+1 >= len(v.Route) || leaderLane+1 >= len(leader.Route) {
		return false
	}
	current := &v.Route[lane]
	return current.ID == leader.Route[leaderLane].ID &&
		current.StationRole != StationEntryRole && current.StationRole != StationBerthAccessRole
}

// canLink reports whether platooning is on and the traveling pod v is
// slow, below platoonSlowFraction of the speed limit of its lane.
func (s *Simulation) canLink(v *vehicle) bool {
	if v.couplingID != "" || largeVehicleClass(v.Pod.Class) {
		return false
	}
	lane := v.blocks.find(v.blockIndex, &v.blocks.cursors[podCursor]).lane
	return s.platooning != PlatooningOff && v.Pod.Speed < platoonSlowFraction*v.Route[lane].SpeedLimit
}

// tryLink couples the pod at index i to the pod at index ahead, the
// nearest pod ahead of it on its lane, when the rules allow it. The pod
// must be slow, the pod ahead must have no follower, and the platoon must
// stay within the platoon limit. Both routes must have one speed limit,
// and so must the lanes of both destination stations. A platoon has one
// turn, so when the pod ahead has a predecessor or the pod has a follower,
// the new link takes the turn of those links, and two platoons with
// different turns do not join. Then planLink must accept the pair. The
// pods must also be in one queue: the path distance between them must be
// at most the link clearance plus the stopping distance at the speed
// limit. Inside a fixed entry buffer, one holding-cell pitch also qualifies.
func (s *Simulation) tryLink(i, ahead int) {
	v, leader := &s.vehicles[i], &s.vehicles[ahead]
	if s.couplingApproachMember(v.Pod.ID) || s.couplingApproachMember(leader.Pod.ID) {
		return
	}
	if largeVehicleClass(v.Pod.Class) || largeVehicleClass(leader.Pod.Class) {
		return
	}
	if s.compactGroup(v) != nil || s.compactGroup(leader) != nil || s.compactEnabled() && v.buffered {
		return
	}
	if !s.canLink(v) || leader.follower != 0 || s.platoonSize(v, leader) > s.platoonLimit {
		return
	}
	current := v.blocks.find(v.blockIndex, &v.blocks.cursors[podCursor]).lane
	limit := v.Route[current].SpeedLimit
	leaderLane := leader.blocks.find(leader.blockIndex, &leader.blocks.cursors[podCursor]).lane
	if !s.oneSpeedLimit(v, current, limit) || !s.oneSpeedLimit(leader, leaderLane, limit) {
		return
	}
	turn, fixed := platoonMaxTurn, false
	if leader.link.leader != 0 {
		turn, fixed = leader.link.turn, true
	}
	if v.follower != 0 {
		behind := s.vehicles[v.follower-1].link.turn
		if fixed && behind != turn {
			return
		}
		turn, fixed = behind, true
	}
	link, ok := s.planLink(linkPlan{v: v, leader: leader, lane: current, leaderLane: leaderLane, turn: turn, fixed: fixed})
	if ok && leaderPosition(v, leader, link)-v.distance <= max(link.clearance+stoppingDistance(limit), s.bufferRecruitmentDistance(v, leader, link)) {
		s.link(i, ahead, link)
	}
}

// platoonSize returns the number of pods in one platoon when v couples to
// leader.
func (s *Simulation) platoonSize(v, leader *vehicle) int {
	size := 2
	for index := leader.link.leader; index != 0; index = s.vehicles[index-1].link.leader {
		size++
	}
	for index := v.follower; index != 0; index = s.vehicles[index-1].follower {
		size++
	}
	return size
}

// oneSpeedLimit reports whether each lane of the route of v from route
// index from, and each lane of its destination station, has the given
// speed limit. The route can grow only by lanes of the destination
// station.
func (s *Simulation) oneSpeedLimit(v *vehicle, from int, limit float64) bool {
	for _, lane := range v.Route[from:] {
		if lane.SpeedLimit != limit {
			return false
		}
	}
	limits, ok := s.platoonIndexes().stationLimits[v.destinationStation]
	return !ok || limits == [2]float64{limit, limit}
}

// link couples the pod at index i to the pod at index ahead.
func (s *Simulation) link(i, ahead int, link platoonLink) {
	link.leader = ahead + 1
	s.vehicles[i].link = link
	s.vehicles[ahead].follower = i + 1
	s.platoonLinks++
}

// linkPlan is the input of planLink.
type linkPlan struct {
	v, leader *vehicle
	// lane and leaderLane are the route indexes of the lane of v in the
	// routes of v and leader.
	lane, leaderLane int
	// turn is the turn of the platoon when fixed is true. Otherwise it is
	// platoonMaxTurn.
	turn  float64
	fixed bool
}

// turnBound returns the largest total turn of the run of plan. A new
// platoon takes the turn of the run as its turn, so that turn must be
// within platoonMaxTurn, the bound that a restore checks. A fixed turn is
// the turn of the platoon. The run turn of the new link is a sum from
// another lane, so it can differ from the fixed turn by rounding.
func (plan linkPlan) turnBound() float64 {
	if plan.fixed {
		return plan.turn + platoonTurnSlack
	}
	return platoonMaxTurn
}

// planLink returns a new link of plan.v to plan.leader. The run starts at
// the lane of v and holds the next lanes that both routes share and that
// keep the total turn of the run within plan.turnBound. After the next lane,
// each lane must start within platoonHorizon. For a new platoon, the turn of the link is the turn of
// the run. The follower must be able to reserve at least one more block as
// a platoon member before the end block. The path distance between the
// pods and between their stop points must be at least the clearance, so
// the cap of the follower is not behind its stop point.
func (s *Simulation) planLink(plan linkPlan) (platoonLink, bool) {
	v, leader := plan.v, plan.leader
	if largeVehicleClass(v.Pod.Class) || largeVehicleClass(leader.Pod.Class) {
		return platoonLink{}, false
	}
	if v.Route[plan.lane].StationRole == StationEntryRole {
		return s.planBufferLink(plan)
	}
	shapes := s.platoonIndexes().shapes
	link := platoonLink{lane: plan.lane, leaderLane: plan.leaderLane}
	var sum turnSum
	for s.sharedLane(v, leader, plan.lane+link.lanes, plan.leaderLane+link.lanes) {
		index := plan.lane + link.lanes
		if link.lanes > 1 && v.blocks.lanes[index].start-v.distance > platoonHorizon {
			break
		}
		next := sum.add(shapes[v.Route[index].ID])
		if next.turn > plan.turnBound() {
			break
		}
		sum = next
		link.lanes++
	}
	if link.lanes == 0 {
		return platoonLink{}, false
	}
	link.turn = sum.turn
	if plan.fixed {
		link.turn = plan.turn
	}
	link.clearance = linkClearance(link.turn)
	_, link.end = linkEnds(&v.blocks, link)
	position := leaderPosition(v, leader, link)
	if link.end <= v.reservedThrough || position-v.distance < link.clearance ||
		v.distance+stoppingDistance(v.Pod.Speed)+link.clearance > position+stoppingDistance(leader.Pod.Speed) {
		return platoonLink{}, false
	}
	return link, true
}

// stoppingDistance returns the distance in which a pod at speed stops.
func stoppingDistance(speed float64) float64 {
	return speed * speed / (2 * acceleration)
}

// leaderPosition returns the position of the predecessor of a link as a
// route distance of the follower. A predecessor after the run counts its
// distance from the start of the last lane of the run.
func leaderPosition(v, leader *vehicle, link platoonLink) float64 {
	current := leader.blocks.find(leader.blockIndex, &leader.blocks.cursors[podCursor]).lane
	leaderLane := min(max(current, link.leaderLane), link.leaderLane+link.lanes-1)
	lane := link.lane + leaderLane - link.leaderLane
	return v.blocks.lanes[lane].start + leader.distance - leader.blocks.lanes[leaderLane].start
}

// platoonCaps sets the cap of each follower from the state before the
// pods move, so the order of the pods does not change a result. The stop
// point of a predecessor does not move back, because it brakes at no more
// than acceleration and its route has one speed limit. The cap is that
// stop point less the link clearance and the reaction distance of the
// follower. The cap is never behind the stop point of the follower while
// that point is within the clearance limit. Thus a follower never brakes
// harder than acceleration, and its own stop point does not move back.
func (s *Simulation) platoonCaps() {
	if s.platoonLinks == 0 {
		return
	}
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if v.link.leader == 0 || v.link.compact {
			continue
		}
		leader := &s.vehicles[v.link.leader-1]
		if leader.Pod.Activity != Traveling {
			v.platoonCap = math.Inf(1)
			continue
		}
		limit := leaderPosition(v, leader, v.link) + stoppingDistance(leader.Pod.Speed) - v.link.clearance
		own := v.distance + stoppingDistance(v.Pod.Speed)
		v.platoonCap = max(min(own, limit), limit-v.Pod.Speed*platoonReactionSeconds)
		if v.link.buffer {
			geometry, _ := linkEnds(&v.blocks, v.link)
			v.platoonCap = min(v.platoonCap, geometry)
		}
	}
}

// platoonIndexData holds the network data that links read. No code writes
// to it in place, so clones share it.
type platoonIndexData struct {
	// shapes holds the shape of each lane by lane ID.
	shapes map[string]laneShape
	// stationLimits holds the lowest and the highest speed limit of the
	// lanes of each station, by station ID.
	stationLimits map[string][2]float64
}

// platoonIndexes returns the network data that links read. It builds the
// data when the network has no data yet.
func (s *Simulation) platoonIndexes() *platoonIndexData {
	if s.platoonData != nil && len(s.platoonData.shapes) == len(s.network.Lanes) {
		return s.platoonData
	}
	data := &platoonIndexData{shapes: make(map[string]laneShape, len(s.network.Lanes)), stationLimits: make(map[string][2]float64)}
	points := make([]Point, 0, 65)
	for _, lane := range s.network.Lanes {
		data.shapes[lane.ID] = newLaneShape(s.network.lanePoints(lane, points[:0]))
		if lane.StationID == "" {
			continue
		}
		limits, ok := data.stationLimits[lane.StationID]
		if !ok {
			limits = [2]float64{lane.SpeedLimit, lane.SpeedLimit}
		}
		data.stationLimits[lane.StationID] = [2]float64{min(limits[0], lane.SpeedLimit), max(limits[1], lane.SpeedLimit)}
	}
	s.platoonData = data
	return data
}

// newLaneShape returns the shape of a lane with the given points.
func newLaneShape(points []Point) laneShape {
	shape := laneShape{empty: true}
	for index := 1; index < len(points); index++ {
		dx, dy := points[index].X-points[index-1].X, points[index].Y-points[index-1].Y
		if dx == 0 && dy == 0 {
			continue
		}
		heading := math.Atan2(dy, dx)
		if shape.empty {
			shape.start, shape.empty = heading, false
		} else {
			shape.curve += math.Abs(math.Remainder(heading-shape.end, 2*math.Pi))
		}
		shape.end = heading
	}
	return shape
}
