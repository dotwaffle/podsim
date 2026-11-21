package sim

import "slices"

// envelopeLocationsPerPod is the capacity for envelope locations that
// largeSafety starts with, for each pod.
const envelopeLocationsPerPod = 4

// largeSafety adds the envelope of each pod when the fleet has a large pod.
// The envelopes share one backing array while it has capacity. Each
// envelope has no spare capacity, so an append to one envelope does not
// write into the next.
func (s *Simulation) largeSafety(observation *SafetyObservation) {
	if !slices.ContainsFunc(s.vehicles, func(v vehicle) bool { return largeVehicleClass(v.Pod.Class) }) {
		return
	}
	observation.envelopes = make([][]SafetyLocation, len(s.vehicles))
	locations := make([]SafetyLocation, 0, envelopeLocationsPerPod*len(s.vehicles))
	for index := range s.vehicles {
		v := &s.vehicles[index]
		start := len(locations)
		locations = s.appendVehicleSafetyLocations(locations, v)
		observation.envelopes[index] = locations[start:len(locations):len(locations)]
	}
}

// appendVehicleSafetyLocations appends the planes that the body of a pod
// occupies to locations, each plane one time. The envelope comes from the
// spans of the prepared envelope reach (see appendSpanEnvelope). A pod with
// no span gets no plane, and an empty envelope is separated from no other
// envelope (see envelopeLocationsSeparated). A traveling pod always has a
// lane of the network, and each other pod a berth, so this does not occur.
func (s *Simulation) appendVehicleSafetyLocations(locations []SafetyLocation, v *vehicle) []SafetyLocation {
	if s.envelopes == nil {
		return locations
	}
	span, choice, ok := s.envelopeSpan(v)
	if !ok {
		return locations
	}
	return s.appendSpanEnvelope(locations, v, span, choice)
}

// envelopeChoice holds what the envelope of a pod selects from a span of
// the envelope reach (see appendSpanEnvelope). origin is the reach node
// number of the origin unit that the pod selects, and -1 for none.
// originBerth and berth are the location numbers of the origin berth and of
// the berth of the pod, and -1 for none.
type envelopeChoice struct {
	origin, originBerth, berth int
}

// envelopeSpan returns the span of the prepared envelope reach for a pod,
// and what the pod selects from it. A traveling pod reads the span of its
// lane at its published lane position. Before its origin release, it also
// selects the unit of its origin node and its origin berth. A pod at a berth reads the span of its berth,
// and selects the unit of the berth node and its berth. ok is false when
// the reach has no such span. A traveling pod always has a lane of the
// network, and each other pod a berth, so this does not occur.
func (s *Simulation) envelopeSpan(v *vehicle) (*reachSpan, envelopeChoice, bool) {
	r := s.envelopes
	choice := envelopeChoice{origin: -1, originBerth: -1, berth: -1}
	if number, ok := r.berthLocations[v.Pod.BerthID]; ok {
		choice.berth = number
	}
	if v.Pod.Activity == Traveling {
		lane, ok := s.graph.lanes[v.Pod.LaneID]
		if !ok || v.Pod.LaneID == "" {
			return nil, choice, false
		}
		if v.origin.ID != "" && !v.originReleased {
			if node, ok := r.graph.nodes[v.origin.Node]; ok {
				choice.origin = node
			}
			if number, ok := r.berthLocations[v.origin.ID]; ok {
				choice.originBerth = number
			}
		}
		return r.laneAt(lane, v.Pod.LaneDistance), choice, true
	}
	if choice.berth < 0 {
		return nil, choice, false
	}
	choice.origin, choice.originBerth = r.graph.berthNode[choice.berth-len(r.lanes)], choice.berth
	return &r.berths[choice.berth-len(r.lanes)], choice, true
}

// appendSpanEnvelope appends the envelope of a pod: the planes of a span
// that the pod selects, then rule 2.
// A lane of the span is selected when it is a lane of the route of the
// pod, or when its item comes from the unit of the node that the choice
// selects. A berth is selected when it is the selected origin berth and its
// item comes from the selected unit. A pod at a berth selects its berth as
// its origin berth (see envelopeSpan).
// The selected planes come in the order of their location numbers, each
// plane one time, and then rule 2 adds the clusters (see appendClusters).
// The index holds rule 2 for each location of the span (see closure), so
// the index holds each plane of this envelope.
func (s *Simulation) appendSpanEnvelope(locations []SafetyLocation, v *vehicle, span *reachSpan, choice envelopeChoice) []SafetyLocation {
	r := s.envelopes
	lanes := len(r.lanes)
	var selected, candidates []int
	for index, number := range span.locations {
		origin := r.keyOrigin(span.keys[index])
		switch {
		case number >= lanes:
			if number == choice.originBerth && origin == choice.origin {
				selected = append(selected, number)
			}
		case origin >= 0 && origin == choice.origin:
			selected = append(selected, number)
		default:
			candidates = append(candidates, number)
		}
	}
	if len(candidates) > 0 && v.Pod.Activity == Traveling {
		slices.Sort(candidates)
		for position := range v.Route {
			if number, ok := s.routeLaneIndex(&v.blocks, position, &v.Route[position]); ok {
				if _, found := slices.BinarySearch(candidates, number); found {
					selected = append(selected, number)
				}
			}
		}
	}
	return s.appendSelected(locations, selected)
}

// envelopePlanesStack is the number of plane numbers of an envelope that
// appendSelected keeps on the stack.
const envelopePlanesStack = 64

// appendSelected appends the envelope of the selected location numbers to
// locations: their planes in the order of the location numbers, each plane
// one time, and then the planes of rule 2 (see appendClusters). It sorts
// selected.
func (n *networkIndexes) appendSelected(locations []SafetyLocation, selected []int) []SafetyLocation {
	r := n.envelopes
	slices.Sort(selected)
	// planes holds the plane number of each plane of the envelope.
	var stack [envelopePlanesStack]int
	planes := stack[:0]
	for _, number := range slices.Compact(selected) {
		if plane := r.planeOf[number]; !slices.Contains(planes, plane) {
			planes = append(planes, plane)
			locations = append(locations, r.locations[number])
		}
	}
	return n.appendClusters(locations, planes)
}

// appendClusters applies rule 2 to the envelope at the end of locations,
// whose plane numbers are planes: for each plane of a maneuver group lane,
// it appends the planes of the lanes at the nodes of the cluster of the
// lane (see maneuverClusters), each plane one time. The planes come in the
// order of the envelope, and the planes of one cluster in lane order. The
// planes of rule 2 come from the envelope reach (see
// envelopeReach.planeClusters), which holds the planes of clusterSafety by
// plane number. clusterSafety is empty when the network has no maneuver
// group.
func (n *networkIndexes) appendClusters(locations []SafetyLocation, planes []int) []SafetyLocation {
	if len(n.clusterSafety) == 0 {
		return locations
	}
	r := n.envelopes
	end := len(planes)
	var applied []*int
	for index := range end {
		cluster := r.planeClusters[planes[index]]
		// The group lanes of one cluster share one slice of plane numbers,
		// so the envelope adds each cluster one time.
		if len(cluster) == 0 || slices.Contains(applied, &cluster[0]) {
			continue
		}
		applied = append(applied, &cluster[0])
		for _, extra := range cluster {
			if !slices.Contains(planes, extra) {
				planes = append(planes, extra)
				locations = append(locations, r.planes[extra])
			}
		}
	}
	return locations
}

// numberPlanes builds planeOf, planes, and planeClusters of r from the
// planes of rule 2 of the network of r (see clusterSafetyOf).
func (r *envelopeReach) numberPlanes(clusters map[SafetyLocation][]SafetyLocation) {
	numbers := make(map[SafetyLocation]int, len(r.locations))
	number := func(location SafetyLocation) int {
		plane, ok := numbers[location]
		if !ok {
			plane = len(r.planes)
			numbers[location] = plane
			r.planes = append(r.planes, location)
		}
		return plane
	}
	r.planeOf = make([]int, len(r.locations))
	for index, location := range r.locations {
		r.planeOf[index] = number(location)
	}
	// The clusters of a network are planes of its lanes, which have plane
	// numbers now. Each other plane gets a number before planeClusters is
	// made.
	for location, cluster := range clusters {
		number(location)
		for _, plane := range cluster {
			number(plane)
		}
	}
	r.planeClusters = make([][]int, len(r.planes))
	shared := make(map[*SafetyLocation][]int)
	for location, cluster := range clusters {
		if len(cluster) == 0 {
			continue
		}
		numbered, done := shared[&cluster[0]]
		if !done {
			numbered = make([]int, len(cluster))
			for index, plane := range cluster {
				numbered[index] = number(plane)
			}
			shared[&cluster[0]] = numbered
		}
		r.planeClusters[number(location)] = numbered
	}
}

// maneuverClusters returns the lanes of rule 2 for each lane of a maneuver
// group, by lane index: each lane at a node of the cluster of the lane, in
// lane order. A cluster is a group with each group that shares a node with
// it, and so on. The nodes of a group are the nodes of its lanes. The lanes
// of one cluster share one slice, and each other lane has none. It returns
// nil for a network with no group.
func maneuverClusters(n Network) [][]int {
	if len(n.ManeuverGroups) == 0 {
		return nil
	}
	lanes := make(map[string]int, len(n.Lanes))
	for index, lane := range n.Lanes {
		if _, repeated := lanes[lane.ID]; !repeated {
			lanes[lane.ID] = index
		}
	}
	// parent joins the groups that share a node. nodeGroup holds a group at
	// each node of a group lane.
	parent := make([]int, len(n.ManeuverGroups))
	for group := range parent {
		parent[group] = group
	}
	find := func(group int) int {
		for parent[group] != group {
			parent[group] = parent[parent[group]]
			group = parent[group]
		}
		return group
	}
	nodeGroup := make(map[string]int)
	for group, maneuver := range n.ManeuverGroups {
		for _, id := range maneuver.LaneIDs {
			index, known := lanes[id]
			if !known {
				continue
			}
			for _, node := range []string{n.Lanes[index].From, n.Lanes[index].To} {
				if other, seen := nodeGroup[node]; seen {
					parent[find(other)] = find(group)
				} else {
					nodeGroup[node] = group
				}
			}
		}
	}
	members := make(map[int][]int)
	for index, lane := range n.Lanes {
		first := -1
		for _, node := range []string{lane.From, lane.To} {
			if group, ok := nodeGroup[node]; ok {
				if root := find(group); root != first {
					members[root] = append(members[root], index)
					first = root
				}
			}
		}
	}
	clusters := make([][]int, len(n.Lanes))
	for group, maneuver := range n.ManeuverGroups {
		for _, id := range maneuver.LaneIDs {
			if index, known := lanes[id]; known {
				clusters[index] = members[find(group)]
			}
		}
	}
	return clusters
}

func envelopeLocationsSeparated(first, second []SafetyLocation) bool {
	if len(first) == 0 || len(second) == 0 {
		return false
	}
	for _, a := range first {
		for _, b := range second {
			if !safetyLocationsSeparated(a, b) {
				return false
			}
		}
	}
	return true
}
