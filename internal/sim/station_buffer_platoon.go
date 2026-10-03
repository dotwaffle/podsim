package sim

import "slices"

// bufferRecruitmentSlack absorbs route-distance rounding in recruitment only.
const bufferRecruitmentSlack = 1e-9

// bufferRecruitmentDistance lets adjacent holding cells recruit at low speed.
// Both pods must be inside the certified holding region. It changes no clearance.
func (s *Simulation) bufferRecruitmentDistance(v, leader *vehicle, link platoonLink) float64 {
	if !link.buffer {
		return 0
	}
	plan, ok := s.bufferPlan(v)
	if !ok || v.distance+bufferRecruitmentSlack < v.blocks.end(plan.entryStop) ||
		leaderPosition(v, leader, link) > v.blocks.end(plan.frontier)+bufferRecruitmentSlack {
		return 0
	}
	return s.laneLength(plan.lane)/float64(s.laneCells[plan.lane.ID].count()) + bufferRecruitmentSlack
}

// planBufferLink keeps both pods inside one fixed entry certificate.
func (s *Simulation) planBufferLink(plan linkPlan) (platoonLink, bool) {
	v, leader := plan.v, plan.leader
	if largeVehicleClass(v.Pod.Class) || largeVehicleClass(leader.Pod.Class) {
		return platoonLink{}, false
	}
	if !s.stationBuffers || !v.buffered || !leader.buffered || v.destinationStation != leader.destinationStation ||
		plan.lane != len(v.Route)-1 || plan.leaderLane != len(leader.Route)-1 ||
		v.Route[plan.lane].ID != leader.Route[plan.leaderLane].ID ||
		v.Pod.LaneID != v.Route[plan.lane].ID || leader.Pod.LaneID != v.Pod.LaneID {
		return platoonLink{}, false
	}
	if leader.link.leader != 0 && !leader.link.buffer || v.follower != 0 && !s.vehicles[v.follower-1].link.buffer {
		return platoonLink{}, false
	}
	buffer, ok := s.bufferPlan(v)
	if !ok || !s.bufferHasDischarge(v) || !s.bufferHasDischarge(leader) {
		return platoonLink{}, false
	}
	turn := s.runTurn(v.Route, plan.lane, plan.lane)
	if turn > plan.turnBound() {
		return platoonLink{}, false
	}
	if plan.fixed {
		turn = plan.turn
	}
	link := platoonLink{buffer: true, terminalCell: buffer.frontier - buffer.first, first: buffer.entryStop + 1, lane: plan.lane, leaderLane: plan.leaderLane, lanes: 1, turn: turn, clearance: linkClearance(turn)}
	geometry, end := linkEnds(&v.blocks, link)
	link.end = end
	position := leaderPosition(v, leader, link)
	if link.end <= v.reservedThrough || position-v.distance < link.clearance ||
		v.distance+stoppingDistance(v.Pod.Speed)+link.clearance > position+stoppingDistance(leader.Pod.Speed) ||
		v.distance+stoppingDistance(v.Pod.Speed) > geometry {
		return platoonLink{}, false
	}
	return link, true
}

// bufferHasDischarge proves that at least one exclusive suffix can keep the prefix.
func (s *Simulation) bufferHasDischarge(v *vehicle) bool {
	station, ok := s.station(v.destinationStation)
	if !ok {
		return false
	}
	for _, berth := range station.Berths {
		suffix, err := s.stationPathForClass(station.routeEntry(v.Route, v.destination), berth.Node, v.Pod.Class)
		if err == nil && s.bufferSuffixValid(v, suffix, berth) {
			return true
		}
	}
	return false
}

// bufferSuffixValid checks speed, station identity, and immutable prefix cells.
func (s *Simulation) bufferSuffixValid(v *vehicle, suffix []Lane, berth Berth) bool {
	if len(v.Route) == 0 || len(suffix) == 0 || suffix[0].From != v.Route[len(v.Route)-1].To || suffix[len(suffix)-1].To != berth.Node {
		return false
	}
	entry := v.Route[len(v.Route)-1]
	for _, lane := range suffix {
		if lane.StationID != v.destinationStation || lane.SpeedLimit != entry.SpeedLimit || lane.ID == entry.ID ||
			lane.StationRole == StationApproachRole || lane.StationRole == StationEntryRole {
			return false
		}
	}
	route := append(slices.Clone(v.Route), suffix...)
	blocks, _ := s.routeBlocks(route)
	for lane := range v.Route {
		if blocks.lanes[lane].first != v.blocks.lanes[lane].first || blocks.lanes[lane].start != v.blocks.lanes[lane].start ||
			blocks.lanes[lane].cells != v.blocks.lanes[lane].cells {
			return false
		}
	}
	return blocks.laneFirst(len(v.Route)) == v.blocks.len()
}
