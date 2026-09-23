package sim

// retainedOwners returns the owner of each resource that the retention rules
// require. After each step, the incremental map s.owners is equal to this
// result.
//
// A pod that is not traveling holds its berth and the berth node. A traveling
// pod holds each resource of its reserved blocks until it passes the release
// distance of that resource. It also holds its origin berth and node until it
// is Clearance from the origin. A relocating pod keeps each destination claim
// that s.owners gives to it.
func (s *Simulation) retainedOwners() map[resource]string {
	owners := make(map[resource]string, len(s.owners))
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if v.Pod.Activity == Traveling {
			addRouteOwners(owners, v)
		} else {
			station, _ := s.station(v.Pod.StationID)
			berth, _ := station.berth(v.Pod.BerthID)
			owners[resource{kind: berthResource, id: berth.ID}] = v.Pod.ID
			owners[resource{kind: nodeResource, id: berth.Node}] = v.Pod.ID
		}
		if v.RelocatingTo == "" {
			continue
		}
		for _, r := range []resource{
			{kind: berthResource, id: v.destination.ID},
			{kind: nodeResource, id: v.destination.Node},
		} {
			if s.owners[r] == v.Pod.ID {
				owners[r] = v.Pod.ID
			}
		}
	}
	return owners
}

// addRouteOwners adds the resources that a traveling pod holds to owners.
func addRouteOwners(owners map[resource]string, v *vehicle) {
	for blockIndex := range v.reservedThrough + 1 {
		b := v.blocks[blockIndex]
		for _, r := range b.resources {
			if resourceReleaseDistance(b, r) > v.distance {
				owners[r] = v.Pod.ID
			}
		}
	}
	if v.distance < Clearance {
		owners[resource{kind: berthResource, id: v.origin.ID}] = v.Pod.ID
		owners[resource{kind: nodeResource, id: v.origin.Node}] = v.Pod.ID
	}
}
