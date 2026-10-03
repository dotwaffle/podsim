package sim

import "slices"

type compactDischargeProbe struct {
	available    bool
	blockedBerth string
	blockedOwner string
}

// probeCompactDischarge checks legal berth paths without changing ownership.
func (s *Simulation) probeCompactDischarge(group *compactBufferGroup) compactDischargeProbe {
	head := &s.vehicles[group.members[0]]
	station, ok := s.station(head.destinationStation)
	if !ok {
		return compactDischargeProbe{}
	}
	probe := compactDischargeProbe{}
	accept := s.berthFilterForVehicle(head)
	for _, berth := range station.Berths {
		if !berthAllows(station, berth, head.Pod.Class) || accept != nil && !accept(berth) ||
			station.Banks != nil && station.berthEntry(berth) != head.Route[len(head.Route)-1].To {
			continue
		}
		suffix, err := s.stationPathForClass(head.Route[len(head.Route)-1].To, berth.Node, head.Pod.Class)
		if err != nil || !s.bufferSuffixValid(head, suffix, berth) {
			continue
		}
		route := append(slices.Clone(head.Route), suffix...)
		if !s.rerouteKeepsDetours(head, route, berth) {
			continue
		}
		claims, available := s.bufferBerthClaims(head, berth)
		if !available {
			owner := s.owners[resource{kind: berthResource, id: berth.ID}]
			blocker := s.findVehicle(owner)
			if owner != "" && (probe.blockedOwner == "" || blocker != nil && blocker.Pod.Activity == Idle) {
				probe.blockedBerth, probe.blockedOwner = berth.ID, owner
			}
			continue
		}
		blocks, _ := s.routeBlocks(suffix)
		free := true
		for resources := range blocks.spanResources(0, blocks.len()) {
			for _, r := range resources {
				owner := s.owners[r]
				yielded := slices.ContainsFunc(claims[:], func(c bufferBerthClaim) bool { return c.resource == r && c.owner != nil && c.owner.Pod.ID == owner })
				if owner != "" && owner != head.Pod.ID && !yielded {
					free = false
				}
			}
		}
		if free {
			return compactDischargeProbe{available: true}
		}
	}
	return probe
}

// compactBerthBlocker supplies the ordinary idle-clearing signal before recovery.
// The head can hold far before stock lookahead requests its exclusive suffix.
func (s *Simulation) compactBerthBlocker(index int) {
	v := &s.vehicles[index]
	group := s.compactGroup(v)
	if group == nil || group.members[0] != index {
		return
	}
	if _, err := s.compactStates(group); err != nil {
		return
	}
	probe := s.probeCompactDischarge(group)
	if !probe.available && probe.blockedOwner != "" {
		v.Pod.WaitReason, v.Pod.BlockedBy, v.bufferBerth = BerthOccupied, probe.blockedOwner, probe.blockedBerth
	}
}
