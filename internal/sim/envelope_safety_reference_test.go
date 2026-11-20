package sim

import (
	"maps"
	"slices"
	"sync"
	"testing"
)

// referenceVehicleSafetyLocations is the construction of
// vehicleSafetyLocations before the berth and node indexes. It scans every
// station berth and every network lane. It builds the envelope of section
// 3.1 of the shared-track design from its definition (see
// envelopeReference), and it does not read the envelope reach, its spans,
// its items, or its bounds.
func referenceVehicleSafetyLocations(s *Simulation, v *vehicle) []SafetyLocation {
	return referenceSafetyLocationsWith(s, nil, v)
}

// referenceSafetyLocationsWith is referenceVehicleSafetyLocations with the
// tables of the reference construction of s in r, so that a run builds
// them one time. When r is nil, it builds them.
func referenceSafetyLocationsWith(s *Simulation, r *envelopeReference, v *vehicle) []SafetyLocation {
	_, lane := s.graph.lanes[v.Pod.LaneID]
	_, berth := s.berthNodes[v.Pod.BerthID]
	if s.envelopes == nil || v.Pod.Activity == Traveling && !lane || v.Pod.Activity != Traveling && !berth {
		// A traveling pod always has a lane, and each other pod a berth.
		return nil
	}
	if r == nil {
		r = newEnvelopeReference(s)
	}
	return r.envelope(v)
}

// envelopeReference builds the envelope of a network
// from the definition of section 3.1 of the shared-track design. Its walks
// are label-correcting walks by node ID with a queue, in place of the
// ordered walks of the index, and they give the same least sums.
type envelopeReference struct {
	s      *Simulation
	lanes  []Lane
	lane   map[string]int
	length []float64
	// in and out hold the lanes into and out of each node ID.
	in, out map[string][]int
	// berths holds the berth IDs at each node, and number the location
	// number of each berth: the lanes, then the berths in station and berth
	// order.
	berths map[string][]string
	number map[string]int
	// tail, front, and origin hold the retained tail and the anchor front of
	// each lane, and the origin tail of each node with a lane out of it.
	tail, front []float64
	origin      map[string]float64
	behind      float64
	// clusters holds the lanes of rule 2 of each plane that an envelope
	// met, so that the reference computes each cluster one time.
	clusters map[SafetyLocation][]Lane
}

// referenceSlack is the slack u of the definition.
const referenceSlack = 1e-3

func newEnvelopeReference(s *Simulation) *envelopeReference {
	n := s.network
	r := &envelopeReference{s: s, lanes: n.Lanes, lane: make(map[string]int), in: make(map[string][]int), out: make(map[string][]int),
		berths: make(map[string][]string), number: make(map[string]int), origin: make(map[string]float64),
		clusters: make(map[SafetyLocation][]Lane)}
	positions := indexNodePositions(n.Nodes)
	for index, lane := range n.Lanes {
		r.lane[lane.ID] = index
		points := lanePointsBetween(lane, positions[lane.From], positions[lane.To], make([]Point, 0, 65))
		length := 0.0
		for k := 1; k < len(points); k++ {
			length += pointDistance(points[k-1], points[k])
		}
		r.length = append(r.length, length)
		r.in[lane.To] = append(r.in[lane.To], index)
		r.out[lane.From] = append(r.out[lane.From], index)
		bounds := s.tails[lane.ID]
		r.tail = append(r.tail, max(20, bounds.tail))
		front := bounds.front
		if bounds == (geometryTails{}) && lane.Control != nil {
			front = sharedSmallFront(points)
		}
		r.front = append(r.front, max(6, front))
		r.origin[lane.From] = max(r.origin[lane.From], 20, bounds.fromTail)
	}
	for _, station := range n.Stations {
		for _, berth := range station.Berths {
			r.number[berth.ID] = len(n.Lanes) + len(r.number)
			r.berths[berth.Node] = append(r.berths[berth.Node], berth.ID)
		}
	}
	r.behind = 20
	for _, tail := range r.tail {
		r.behind = max(r.behind, tail)
	}
	for node := range r.berths {
		r.behind = max(r.behind, r.origin[node])
	}
	return r
}

// walk returns the least sum of lane lengths, added from the start node,
// of each node that a walk reaches along lanes ahead (or back, with ahead
// false) with a sum of at most limit.
func (r *envelopeReference) walk(start string, ahead bool, limit float64) map[string]float64 {
	distance := map[string]float64{start: 0}
	queue := []string{start}
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		lanes := r.in[node]
		if ahead {
			lanes = r.out[node]
		}
		for _, lane := range lanes {
			next := r.lanes[lane].From
			if ahead {
				next = r.lanes[lane].To
			}
			sum := distance[node] + r.length[lane]
			if old, seen := distance[next]; sum <= limit && (!seen || sum < old) {
				distance[next] = sum
				queue = append(queue, next)
			}
		}
	}
	return distance
}

// referenceItem is a location of the envelope of an anchor, and the node of
// its origin, or "" for none.
type referenceItem struct {
	location int
	origin   string
}

// anchor returns the planes of the envelope of an anchor on a lane A, at
// some position of A from lo to hi:
//   - A.
//   - The planes behind A (see back), from the From node of A.
//   - The planes ahead of A (see ahead), from the To node of A.
func (r *envelopeReference) anchor(lane int, lo, hi float64, held map[referenceItem]bool) {
	held[referenceItem{location: lane}] = true
	r.back(r.lanes[lane].From, 0, lo, held)
	r.ahead(lane, r.lanes[lane].To, 0, hi, held)
}

// back adds the planes behind an anchor at some position from lo, with
// the walk back from a node, where the walk distance d of a node is base
// plus the least sum from the start node:
//   - A lane P into a node, when lo <= (tail(P) - d) + u.
//   - The berths and the lanes at a node of a berth, when
//     lo <= (origin tail - d) + u, with the node as their origin.
//
// The walk reaches the nodes at the longest tail plus u or less.
func (r *envelopeReference) back(start string, base, lo float64, held map[referenceItem]bool) {
	for node, w := range r.walk(start, false, r.behind+referenceSlack) {
		d := base + w
		for _, previous := range r.in[node] {
			if lo <= r.tail[previous]-d+referenceSlack {
				held[referenceItem{location: previous}] = true
			}
		}
		if len(r.berths[node]) > 0 && lo <= r.origin[node]-d+referenceSlack {
			r.unit(node, held)
		}
	}
}

// unit adds the berths and the lanes at a node, with the node as their
// origin.
func (r *envelopeReference) unit(node string, held map[referenceItem]bool) {
	for _, id := range r.berths[node] {
		held[referenceItem{location: r.number[id], origin: node}] = true
	}
	for index, other := range r.lanes {
		if other.From == node || other.To == node {
			held[referenceItem{location: index, origin: node}] = true
		}
	}
}

// ahead adds the planes ahead of an anchor on a lane A at some position up
// to hi, with the walk ahead from a node, where the walk distance d of a
// node is base plus the least sum from the start node: a lane M out of a
// node, when hi >= ((length(A) + d) - B(A)) - u. The walk reaches the nodes
// at B(A) plus u or less from the start node.
func (r *envelopeReference) ahead(lane int, start string, base, hi float64, held map[referenceItem]bool) {
	front := r.front[lane]
	for node, w := range r.walk(start, true, front+referenceSlack) {
		if hi >= r.length[lane]+(base+w)-front-referenceSlack {
			for _, next := range r.out[node] {
				held[referenceItem{location: next}] = true
			}
		}
	}
}

// through returns the planes of the envelope of an anchor on a lane A next
// to the lane L of the published position, at some position of A from lo
// to hi, along the paths through L only. near holds the nodes within u of
// L on the side of A, and d is the walk distance between A and L.
//   - A, L, and each lane from a node of near to a node of near.
//   - When A is before L: the planes behind A, and the planes ahead of A
//     from the To node of L with base d + length(L).
//   - When A is after L: the planes ahead of A, the units of the nodes of
//     near with a berth, and the planes behind A from the From node of L
//     with base d + length(L).
func (r *envelopeReference) through(anchor, lane int, d float64, near map[string]float64, before bool, lo, hi float64, held map[referenceItem]bool) {
	held[referenceItem{location: anchor}] = true
	held[referenceItem{location: lane}] = true
	for index, other := range r.lanes {
		_, from := near[other.From]
		_, to := near[other.To]
		if from && to {
			held[referenceItem{location: index}] = true
		}
	}
	base := d + r.length[lane]
	if before {
		r.back(r.lanes[anchor].From, 0, lo, held)
		r.ahead(anchor, r.lanes[lane].To, base, hi, held)
		return
	}
	for node := range near {
		if len(r.berths[node]) > 0 {
			r.unit(node, held)
		}
	}
	r.ahead(anchor, r.lanes[anchor].To, 0, hi, held)
	r.back(r.lanes[lane].From, base, lo, held)
}

// at returns the planes of the envelope at a published position p of a
// lane L: the union of the envelopes of the anchors within u of p, with p
// at least 0 and at most the length of L.
//   - An anchor on L, at p.
//   - While p <= u - d, each anchor on a lane A into a node at the walk
//     distance d <= u behind the From node of L, at some position of A
//     from max(0, length(A) - u) to length(A), along the paths through L
//     (see through).
//   - While p >= (length(L) + d) - u, each anchor on a lane A out of a node
//     at the walk distance d <= u ahead of the To node of L, at some
//     position of A from 0 to min(u, length(A)), along the paths through
//     L.
func (r *envelopeReference) at(lane int, p float64) map[referenceItem]bool {
	p = min(max(p, 0), r.length[lane])
	held := make(map[referenceItem]bool)
	r.anchor(lane, p, p, held)
	before := r.walk(r.lanes[lane].From, false, referenceSlack)
	for node, d := range before {
		if p <= referenceSlack-d {
			for _, previous := range r.in[node] {
				r.through(previous, lane, d, before, true, max(0, r.length[previous]-referenceSlack), r.length[previous], held)
			}
		}
	}
	after := r.walk(r.lanes[lane].To, true, referenceSlack)
	for node, d := range after {
		if p >= r.length[lane]+d-referenceSlack {
			for _, next := range r.out[node] {
				r.through(next, lane, d, after, false, 0, min(referenceSlack, r.length[next]), held)
			}
		}
	}
	return held
}

// envelope returns the envelope of a pod. A traveling pod takes the
// planes at its published lane position, and a pod at a berth the planes
// of the berth and the lanes at its node. A lane is selected when it is on
// the route of a traveling pod, or when it comes from the origin unit of
// the origin node of a traveling pod before its origin release, or of the
// node of the berth of a pod at a berth. A berth is selected when it is the berth of the pod, or the
// origin berth from the unit of its node. The planes come in the order of
// the location numbers, each plane one time, and then rule 2 adds the
// clusters (see referenceClusterLanes).
func (r *envelopeReference) envelope(v *vehicle) []SafetyLocation {
	var held map[referenceItem]bool
	node, originBerth := "", ""
	if v.Pod.Activity == Traveling {
		held = r.at(r.lane[v.Pod.LaneID], v.Pod.LaneDistance)
		if v.origin.ID != "" && !v.originReleased {
			node, originBerth = v.origin.Node, v.origin.ID
		}
	} else {
		for _, station := range r.s.network.Stations {
			for _, berth := range station.Berths {
				if berth.ID == v.Pod.BerthID {
					node, originBerth = berth.Node, berth.ID
				}
			}
		}
		held = make(map[referenceItem]bool)
		for _, id := range r.berths[node] {
			held[referenceItem{location: r.number[id], origin: node}] = true
		}
		for index, lane := range r.lanes {
			if lane.From == node || lane.To == node {
				held[referenceItem{location: index, origin: node}] = true
			}
		}
	}
	selected := make(map[int]bool)
	for item := range held {
		switch {
		case item.location >= len(r.lanes):
			for id, number := range r.number {
				if number == item.location && (id == v.Pod.BerthID || id == originBerth && item.origin == node && node != "") {
					selected[item.location] = true
				}
			}
		case item.origin != "" && item.origin == node:
			selected[item.location] = true
		case v.Pod.Activity == Traveling && slices.ContainsFunc(v.Route, func(lane Lane) bool { return lane.ID == r.lanes[item.location].ID }):
			selected[item.location] = true
		}
	}
	var locations []SafetyLocation
	add := func(location SafetyLocation) {
		if !slices.Contains(locations, location) {
			locations = append(locations, location)
		}
	}
	for _, number := range slices.Sorted(maps.Keys(selected)) {
		if number < len(r.lanes) {
			add(r.s.laneSafety[r.lanes[number].ID])
			continue
		}
		for id, other := range r.number {
			if other == number {
				add(r.s.berthSafety[id])
			}
		}
	}
	if len(r.s.network.ManeuverGroups) > 0 {
		byID := referenceLanesByID(r.s.network)
		for _, location := range slices.Clone(locations) {
			lanes, ok := r.clusters[location]
			if !ok {
				lanes = referenceClusterLanes(r.s.network, byID, location)
				r.clusters[location] = lanes
			}
			for _, lane := range lanes {
				add(r.s.laneSafety[lane.ID])
			}
		}
	}
	return locations
}

// referenceLanes holds the lanes by ID of each network of the reference,
// by its first lane.
var referenceLanes sync.Map

// referenceLanesByID returns the lanes of a network by ID.
func referenceLanesByID(n Network) map[string]Lane {
	if cached, ok := referenceLanes.Load(&n.Lanes[0]); ok {
		if byID, ok := cached.(map[string]Lane); ok {
			return byID
		}
	}
	byID := make(map[string]Lane, len(n.Lanes))
	for _, lane := range n.Lanes {
		byID[lane.ID] = lane
	}
	referenceLanes.Store(&n.Lanes[0], byID)
	return byID
}

// referenceClusterLanes returns the lanes of rule 2 for a plane, in lane
// order: when the plane is the plane of a maneuver group lane, each lane at
// a node of the cluster of that lane. It grows the cluster from the group
// of the lane, one group at a time, until no other group shares a node with
// it. byID holds the lanes of the network by ID.
func referenceClusterLanes(n Network, byID map[string]Lane, location SafetyLocation) []Lane {
	plane := func(lane Lane) SafetyLocation {
		return SafetyLocation{SeparationGroup: lane.SeparationGroup, From: lane.From, To: lane.To}
	}
	nodes := func(group ManeuverGroup) []string {
		var nodes []string
		for _, id := range group.LaneIDs {
			if lane, ok := byID[id]; ok {
				nodes = append(nodes, lane.From, lane.To)
			}
		}
		return nodes
	}
	first := slices.IndexFunc(n.ManeuverGroups, func(group ManeuverGroup) bool {
		return slices.ContainsFunc(group.LaneIDs, func(id string) bool {
			lane, ok := byID[id]
			return ok && plane(lane) == location
		})
	})
	if first < 0 {
		return nil
	}
	in := map[int]bool{first: true}
	held := nodes(n.ManeuverGroups[first])
	for grown := true; grown; {
		grown = false
		for index, group := range n.ManeuverGroups {
			if other := nodes(group); !in[index] && slices.ContainsFunc(other, func(node string) bool { return slices.Contains(held, node) }) {
				in[index], held, grown = true, append(held, other...), true
			}
		}
	}
	var lanes []Lane
	for _, lane := range n.Lanes {
		if slices.Contains(held, lane.From) || slices.Contains(held, lane.To) {
			lanes = append(lanes, lane)
		}
	}
	return lanes
}

// checkSafetyLocationParity requires the indexed envelope locations of
// each pod to equal the reference construction, in the same order. It also
// requires each per-pod entry of the observation to belong to the pod at
// its index.
func checkSafetyLocationParity(t *testing.T, s *Simulation) {
	t.Helper()
	checkSafetyLocationParityWith(t, s, nil)
}

// checkSafetyLocationParityWith is checkSafetyLocationParity with the
// tables of the reference construction of s in r (see
// referenceSafetyLocationsWith).
func checkSafetyLocationParityWith(t *testing.T, s *Simulation, r *envelopeReference) {
	t.Helper()
	o := s.SafetyObservation()
	if len(o.Locations) != len(s.vehicles) || len(o.bound) != len(s.vehicles) {
		t.Fatalf("tick %d: %d locations and %d bound pods for %d pods", s.tick, len(o.Locations), len(o.bound), len(s.vehicles))
	}
	for index := range s.vehicles {
		v := &s.vehicles[index]
		got, want := s.appendVehicleSafetyLocations(nil, v), referenceSafetyLocationsWith(s, r, v)
		if !slices.Equal(got, want) {
			t.Fatalf("tick %d pod %s safety locations %v, reference %v", s.tick, v.Pod.ID, got, want)
		}
		if o.Pods[index] != v.Pod || o.bound[index] != v.Pod {
			t.Fatalf("tick %d pod %s: the observation has %+v bound to %+v", s.tick, v.Pod.ID, o.Pods[index], o.bound[index])
		}
		var location SafetyLocation
		if v.Pod.LaneID != "" {
			location = s.laneSafety[v.Pod.LaneID]
		} else if v.Pod.BerthID != "" {
			location = s.berthSafety[v.Pod.BerthID]
		}
		if o.Locations[index] != location {
			t.Fatalf("tick %d pod %s: location %+v, want %+v", s.tick, v.Pod.ID, o.Locations[index], location)
		}
		if &o.Pods[index] == &o.bound[index] {
			t.Fatalf("tick %d: the pods and the bound pods share storage", s.tick)
		}
		if len(o.envelopes) > 0 {
			if envelope := o.envelopes[index]; !slices.Equal(envelope, want) {
				t.Fatalf("tick %d pod %s: observation envelope %v, reference %v", s.tick, v.Pod.ID, envelope, want)
			} else if cap(envelope) != len(envelope) {
				t.Fatalf("tick %d pod %s: the envelope has spare capacity in the shared array", s.tick, v.Pod.ID)
			}
		}
		if len(o.keys) > 0 {
			if key, _ := s.separationKey(v); o.keys[index] != key {
				t.Fatalf("tick %d pod %s: key %+v, want %+v", s.tick, v.Pod.ID, o.keys[index], key)
			}
		}
		if len(o.chains) > 0 {
			var chain safetyChain
			if !largeVehicleClass(v.Pod.Class) {
				node, speed := "", v.Pod.Speed
				if v.Pod.LaneID == "" {
					node, speed = s.berthSafety[v.Pod.BerthID].From, 0
				}
				if positions := s.retention.chainPositions(v.Pod, node); len(positions) > 0 {
					chain = safetyChain{positions: positions, speed: speed}
				}
			}
			if got := o.chains[index]; got.speed != chain.speed || !slices.Equal(got.positions, chain.positions) {
				t.Fatalf("tick %d pod %s: chain %+v, want %+v", s.tick, v.Pod.ID, got, chain)
			}
		}
	}
}

// TestBerthEnvelopeMatchesReference compares appendVehicleSafetyLocations with
// the reference on the pods at a berth.
// Two lanes into the berth node have one location. A traveling pod that is
// still at its berth, and a stopped pod that has a lane, do not take the
// berth envelope.
func TestBerthEnvelopeMatchesReference(t *testing.T) {
	t.Parallel()
	n := Network{Nodes: []Node{{ID: "a"}, {ID: "b", Position: Point{X: 200}}, {ID: "c", Position: Point{X: 100}}},
		Lanes: []Lane{
			{ID: "in-1", From: "a", To: "c", SeparationGroup: "g"},
			{ID: "in-2", From: "a", To: "c", SeparationGroup: "g"},
			{ID: "out", From: "c", To: "b", SeparationGroup: "h"},
		},
		Stations: []Station{{ID: "s", Berths: []Berth{{ID: "x", Node: "c", SeparationGroup: "g"}, {ID: "y", Node: "b", SeparationGroup: "h"}}}}}
	s := &Simulation{networkIndexes: spanEnvelopeIndexes(n)}
	parked := vehicle{
		Pod: Pod{ID: "parked", Class: GroupClass, Activity: Idle, BerthID: "x"}}
	leaving := vehicle{origin: n.Stations[0].Berths[0], blocks: blockList{lanes: []routeLaneCells{{start: 0}, {start: 100}}}}
	leaving.Pod, leaving.Route = Pod{ID: "leaving", Class: GroupClass, Activity: Traveling, BerthID: "x"}, n.Lanes[2:]
	stopped := vehicle{
		Pod: Pod{ID: "stopped", Class: GroupClass, Activity: Idle, LaneID: "out", BerthID: "y"}}
	for _, v := range []*vehicle{&parked, &leaving, &stopped} {
		got, want := s.appendVehicleSafetyLocations(nil, v), referenceVehicleSafetyLocations(s, v)
		if !slices.Equal(got, want) {
			t.Errorf("pod %s: safety locations %v, reference %v", v.Pod.ID, got, want)
		}
	}
	if got := s.appendVehicleSafetyLocations(nil, &parked); len(got) != 3 {
		t.Errorf("parked pod: %d locations, want 3: %v", len(got), got)
	}
}
