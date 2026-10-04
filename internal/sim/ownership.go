package sim

// retainedOwners returns the owner of each resource that the retention rules
// require. After each step, the incremental map s.owners is equal to this
// result.
//
// A pod that is not traveling holds its berth and the berth node. A traveling
// pod holds each resource of its reserved blocks until it passes the release
// distance of that resource. It also holds its origin berth and node until it
// passes the retention tail of the origin. A relocating pod keeps each
// destination claim that s.owners gives to it. When pods of one platoon hold
// a resource, the
// pod nearest to the front of the platoon owns it.
func (s *Simulation) retainedOwners() map[resource]resourceOwner {
	owners := make(map[resource]resourceOwner, len(s.owners))
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if s.couplingMember(v.Pod.ID) {
			continue
		}
		if v.Pod.Activity == Traveling {
			s.addRouteOwners(owners, v)
		} else {
			station, _ := s.station(v.Pod.StationID)
			berth, _ := station.berth(v.Pod.BerthID)
			owners[resource{kind: berthResource, id: berth.ID}] = podResourceOwner(v.Pod.ID)
			owners[resource{kind: nodeResource, id: berth.Node}] = podResourceOwner(v.Pod.ID)
		}
		if v.RelocatingTo == "" {
			continue
		}
		for _, r := range []resource{
			{kind: berthResource, id: v.destination.ID},
			{kind: nodeResource, id: v.destination.Node},
		} {
			if s.owners[r] == podResourceOwner(v.Pod.ID) {
				owners[r] = podResourceOwner(v.Pod.ID)
			}
		}
	}
	for _, group := range s.couplingGroups {
		c := group.context
		for _, dependency := range c.dependencies {
			if owner := c.dependencyOwner(dependency, group.state); !owner.isZero() {
				owners[dependency.Resource] = owner
			}
		}
		for _, claim := range c.reservation.PreservedClaims {
			owners[claim.Resource] = claim.Expected
		}
	}
	return owners
}

// addRouteOwners adds the resources that a traveling pod holds to owners.
// It keeps an owner that is ahead of the pod in its platoon.
func (s *Simulation) addRouteOwners(owners map[resource]resourceOwner, v *vehicle) {
	for _, b := range v.blocks.span(0, v.reservedThrough+1) {
		for _, r := range b.resources {
			if resourceReleaseDistance(b, r) <= v.distance {
				continue
			}
			if owner, ok := owners[r]; !ok || !s.ownerAheadInPlatoon(v, owner) {
				owners[r] = podResourceOwner(v.Pod.ID)
			}
		}
	}
	if v.distance < v.originTail() {
		owners[resource{kind: berthResource, id: v.origin.ID}] = podResourceOwner(v.Pod.ID)
		owners[resource{kind: nodeResource, id: v.origin.Node}] = podResourceOwner(v.Pod.ID)
	}
}
