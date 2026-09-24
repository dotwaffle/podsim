package sim

import "slices"

// releasePickup marks an empty pod as released when dispatch takes its
// pickup order away. It reports false and changes nothing when the pod is
// not an empty pod on its way to a station. A released pod can divert at
// once, so a later trip in the same dispatch pass can take it.
func (s *Simulation) releasePickup(v *vehicle) bool {
	if !releasable(v) {
		return false
	}
	v.released = true
	return true
}

// releasable reports whether v is an empty pod on its way to a station that
// is not a rebalancing move.
func releasable(v *vehicle) bool {
	return !v.Pod.Occupied && v.RelocatingTo != "" && !v.Rebalancing &&
		(v.Pod.Activity == Traveling || v.Pod.Activity == DepartingEmpty)
}

// parkReleased sends a released pod that no trip took to the nearest free
// berth. A berth is free when no other pod holds it, is at it, or goes to
// it, and no waiting trip goes to it. The pod keeps the track that it
// reserved and starts the new route where divertStart says. Route cost is
// the same as in route. Between berths with the same cost, the current
// destination berth wins, then the other berths of the current destination
// station, then the berths of the other stations in network order. The pod
// reserves the chosen berth, as a pod on its way to parking does. A
// departing pod that has not left its berth can choose that berth. It then
// stays there and becomes idle. A moving pod cannot choose its origin berth
// until it is Clearance from that berth.
//
// The pod keeps its route when it must finish a committed inlet, when no
// free berth is reachable, or when its current destination berth is the
// nearest free berth.
func (s *Simulation) parkReleased(v *vehicle) {
	prefix, from, ok := s.divertStart(v)
	if !ok {
		return
	}
	berth, station, ok := s.nearestFreeBerth(v, from)
	if !ok {
		return
	}
	if berth.ID != v.destination.ID {
		suffix, err := s.route(from, berth.Node)
		if err != nil {
			return
		}
		s.redirect(v, redirection{route: append(slices.Clone(v.Route[:prefix]), suffix...), berth: berth, station: station})
	}
	if v.RelocatingTo == "" {
		return
	}
	for _, r := range berthResources(berth) {
		s.owners[r] = v.Pod.ID
	}
}

// parkUnclaimedReleased sends each released pod that holds no claim on its
// destination berth to the nearest free berth. A pod has no claim after
// dispatch or a restore releases it from a pickup, or after it yields the
// claim to a passenger pod. The pods go in fleet order. A pod that holds
// its claim costs one map lookup.
func (s *Simulation) parkUnclaimedReleased() {
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if v.released && s.owners[resource{kind: berthResource, id: v.destination.ID}] != v.Pod.ID {
			s.parkReleased(v)
		}
	}
}

// nearestFreeBerth returns the free berth with the lowest route cost from
// the node and the station of that berth. It uses the rank order of
// parkReleased. It reports false when no free berth is reachable.
func (s *Simulation) nearestFreeBerth(v *vehicle, from string) (Berth, string, bool) {
	s.ensureNetworkIndexes()
	busy := make(map[string]bool)
	for i := range s.vehicles {
		other := &s.vehicles[i]
		if other != v && other.Pod.Activity != Idle && other.destination.ID != "" {
			busy[other.destination.ID] = true
		}
	}
	for _, trip := range s.waiting {
		if trip.destination.ID != "" {
			busy[trip.destination.ID] = true
		}
	}
	type candidate struct {
		berth   Berth
		station string
	}
	rank := make([]int, len(s.network.Nodes))
	for i := range rank {
		rank[i] = -1
	}
	var candidates []candidate
	add := func(berth Berth, station string) {
		node, ok := s.graph.nodes[berth.Node]
		if !ok || rank[node] >= 0 || busy[berth.ID] || !s.berthAvailableTo(v, berth) {
			return
		}
		// A moving pod cannot stop at the node where its new route starts.
		if berth.Node == from && v.Pod.Activity == Traveling {
			return
		}
		// A moving pod gives up its origin berth at Clearance, and that
		// release would also remove a new claim on the berth. So the pod
		// cannot choose its origin before Clearance.
		if v.Pod.Activity == Traveling && !v.originReleased && berth.ID == v.origin.ID {
			return
		}
		rank[node] = len(candidates)
		candidates = append(candidates, candidate{berth: berth, station: station})
	}
	current, hasCurrent := s.station(v.destinationStation)
	if hasCurrent {
		if berth, ok := current.berth(v.destination.ID); ok {
			add(berth, current.ID)
		}
		for _, berth := range current.Berths {
			add(berth, current.ID)
		}
	}
	for _, station := range s.network.Stations {
		if hasCurrent && station.ID == current.ID {
			continue
		}
		for _, berth := range station.Berths {
			add(berth, station.ID)
		}
	}
	if len(candidates) == 0 {
		return Berth{}, "", false
	}
	node, ok := s.network.nearestIndexed(nearestInput{from: from, rank: rank, extraCost: s.routeExtraCosts()}, s.graph)
	if !ok {
		return Berth{}, "", false
	}
	chosen := candidates[rank[node]]
	return chosen.berth, chosen.station, true
}

// berthAvailableTo reports whether no pod other than v holds the berth or
// its node.
func (s *Simulation) berthAvailableTo(v *vehicle, berth Berth) bool {
	for _, r := range berthResources(berth) {
		if owner := s.owners[r]; owner != "" && owner != v.Pod.ID {
			return false
		}
	}
	return true
}
