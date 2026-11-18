package sim

import "iter"

// emergencyTiers holds the queue promotion of one admit (section 9.3.1 of
// the incident emergency contract). admit derives it from the records, the
// routes, and the owners at each tick, so no saved state holds it.
type emergencyTiers struct {
	// promoted maps the index of each pod in the queue of a record to the
	// lowest serial of such a record. It also holds the record pods.
	promoted map[int]uint64
	// queue is the storage of the search of promoteQueue.
	queue []int
}

// build sets the tiers of the records of s. With no record, it leaves the
// map empty, so admission keeps its order.
func (t *emergencyTiers) build(s *Simulation) {
	clear(t.promoted)
	if len(s.emergencies) == 0 {
		return
	}
	if t.promoted == nil {
		t.promoted = make(map[int]uint64)
	}
	// The records are in serial order (invariant E1), so the first record
	// that reaches a pod has the lowest serial.
	for _, record := range s.emergencies {
		t.queue = s.promoteQueue(record.pod, record.serial, t.promoted, t.queue)
	}
}

// promoteQueue adds to promoted the record pod at index pod and each pod
// that it waits for, directly or through other pods of the set, with the
// serial of the record (see queueSpans). Each pod that the set gets brings
// the pods behind it in its platoon (see promotePlatoon). A pod that is in
// the set already keeps its serial. The set is the closure of these rules,
// so the order of the search changes no entry of promoted. queue is
// storage for the search, and promoteQueue returns it.
func (s *Simulation) promoteQueue(pod int, serial uint64, promoted map[int]uint64, queue []int) []int {
	queue = s.promotePlatoon(pod, serial, promoted, queue[:0])
	for next := 0; next < len(queue); next++ {
		for owner := range s.queueSpans(&s.vehicles[queue[next]]) {
			queue = s.promotePlatoon(owner, serial, promoted, queue)
		}
	}
	return queue[:0]
}

// promotePlatoon adds the pod at index pod and the pods behind it in its
// platoon to promoted, with serial, and to queue. It stops at the first pod
// that promoted holds, because the pods behind that pod are in promoted
// already. A linked follower can hold a cell that a pod ahead owns, so no
// owner names it, and a pod can wait for its follower (see
// returnsToShared).
func (s *Simulation) promotePlatoon(pod int, serial uint64, promoted map[int]uint64, queue []int) []int {
	for member := pod; member >= 0; member = s.vehicles[member].follower - 1 {
		if _, ok := promoted[member]; ok {
			break
		}
		promoted[member] = serial
		queue = append(queue, member)
	}
	return queue
}

// queueSpans yields the index of each other pod that owns a resource of
// the queue spans of v, and, for a linked follower, of each other pod that
// owns a resource that v retains (see holdsPending). The queue spans are
// the prospective reservations of v, one after another, from the first
// block that v has not reserved, up to and including the first reservation
// with a junction resource, or to the end of the route. They do not depend
// on the reservation horizon of this tick or on linked sharing, so they
// over-approximate the pods that v waits for: an owner ahead in a platoon
// does not refuse a linked grant. A pod that requests no grant in any
// tick, a faulted pod or a pod that is neither ready nor Traveling, has no
// queue spans. A pod whose terminal berth choice fails in this tick still
// has them. queueSpans writes nothing but the block cursors of v, which
// change no result. The yield order follows a map for the retained
// resources, so callers must not depend on it.
func (s *Simulation) queueSpans(v *vehicle) iter.Seq[int] {
	return func(yield func(int) bool) {
		if ready := departs(v.Pod.Activity) && v.phaseTicks == 0; v.faulted || !ready && v.Pod.Activity != Traveling {
			return
		}
		if v.link.leader != 0 {
			for r := range v.routeReleases {
				if owner := s.ownerVehicle(s.owners[r]); owner != nil && owner != v && !yield(s.vehicleIndex(owner)) {
					return
				}
			}
		}
		for start := v.reservedThrough + 1; start < v.blocks.len(); {
			through := reservationEnd(&v.blocks, start)
			junction := false
			for resources := range v.blocks.spanResources(start, through+1) {
				for _, r := range resources {
					junction = junction || r.kind == junctionResource
					owner := s.ownerVehicle(s.owners[r])
					if owner != nil && owner != v && !yield(s.vehicleIndex(owner)) {
						return
					}
				}
			}
			if junction {
				return
			}
			start = through + 1
		}
	}
}
