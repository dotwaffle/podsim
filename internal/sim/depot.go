package sim

import (
	"cmp"
	"slices"
)

// A depot takes arrivals at its entry. A pod that goes to park in a depot
// routes to the entry and holds no slot. When it owns the last cell of the
// incoming lane, bindDepotArrivals binds it: its route goes on to the slot
// behind the rearmost taken slot of the lane with the fewest pods, and it
// names that slot as its destination. The binding writes no owner. The pod
// takes the slot resources with its grants, as with any other lane cell.

// depotLaneState is the state of one depot lane in a lane view. rear is the
// index of the rearmost taken slot, or -1 when no slot is taken. pods is
// the number of pods of the lane, and stopped is true when one of them is
// faulted or withdrawn.
type depotLaneState struct {
	rear, pods int
	stopped    bool
}

// isDepot reports whether a station is a depot station.
func (d *depotIndex) isDepot(station string) bool {
	if d == nil {
		return false
	}
	_, ok := d.stations[station]
	return ok
}

// depotSlotOf returns the slot of a pod of a depot lane: the slot at which
// it is idle, or the slot that it names as its destination.
func (s *Simulation) depotSlotOf(v *vehicle) (depotSlot, bool) {
	if s.depots == nil {
		return depotSlot{}, false
	}
	if v.Pod.Activity == Idle {
		return s.depots.slot(v.Pod.BerthID)
	}
	return s.depots.slot(v.destination.ID)
}

// depotLaneView returns the state of each lane of a depot. It scans the
// fleet in order and skips exclude, which can be nil. A slot is taken when
// a pod is idle at it or names it as its destination. The view reads no
// owner: a pod that crosses a slot holds its node for a moment, and that
// does not take the slot.
func (s *Simulation) depotLaneView(depot int, exclude *vehicle) []depotLaneState {
	lanes := make([]depotLaneState, len(s.depots.depots[depot].lanes))
	for i := range lanes {
		lanes[i].rear = -1
	}
	for i := range s.vehicles {
		v := &s.vehicles[i]
		slot, ok := s.depotSlotOf(v)
		if v == exclude || !ok || slot.depot != depot {
			continue
		}
		lane := &lanes[slot.lane]
		lane.rear, lane.pods = max(lane.rear, slot.slot), lane.pods+1
		lane.stopped = lane.stopped || v.faulted || !v.inService()
	}
	return lanes
}

// suffix returns the lanes from the entry to the slot at index slot.
func (p depotPath) suffix(slot int) []Lane {
	lanes := slices.Concat(p.fan, []Lane{p.leadIn})
	for i := len(p.chain) - 1; i >= slot; i-- {
		lanes = append(lanes, p.chain[i])
	}
	return lanes
}

// depotTarget returns the slot that a pod of a class binds in a lane in the
// state of the view, and the route from the entry to it. It reports false
// when the lane is not eligible: the slot behind the rearmost taken slot
// must exist, no pod of the lane can be faulted or withdrawn, each lane of
// the route must be open, and each lane of the lane path must admit the
// class. The pod moves up through the chain lanes ahead of its slot and
// leaves by the lead-out and the throat path, so a lane of the path that
// does not admit the class would trap the pod in the lane. The class set
// of a lane includes the class sets of its nodes, of their berths, and of
// their stations (see indexRouteClasses), so the path also checks that the
// slots admit the class.
func (s *Simulation) depotTarget(depot, lane int, state depotLaneState, class VehicleClass) ([]Lane, Berth, bool) {
	info := &s.depots.depots[depot]
	path := &info.lanes[lane]
	slot := state.rear + 1
	if state.stopped || slot >= len(path.slots) {
		return nil, Berth{}, false
	}
	graph := s.routingGraph()
	for _, lane := range path.lanes() {
		if index, ok := graph.lanes[lane.ID]; !ok || !graph.laneAllows(index, class) {
			return nil, Berth{}, false
		}
	}
	suffix := path.suffix(slot)
	for _, lane := range suffix {
		if index, ok := graph.lanes[lane.ID]; !ok || !graph.laneOpen(index) {
			return nil, Berth{}, false
		}
	}
	return suffix, path.slots[slot], true
}

// depotChoice returns the route from the entry and the slot that v binds
// in a depot: the eligible lane with the fewest pods wins, and then the
// lowest lane index. It reports false when no lane is eligible for v.
func (s *Simulation) depotChoice(v *vehicle, depot int) ([]Lane, Berth, bool) {
	view := s.depotLaneView(depot, v)
	best, pods := -1, 0
	var suffix []Lane
	var target Berth
	for lane, state := range view {
		if best >= 0 && state.pods >= pods {
			continue
		}
		if route, slot, ok := s.depotTarget(depot, lane, state, v.Pod.Class); ok {
			best, pods, suffix, target = lane, state.pods, route, slot
		}
	}
	return suffix, target, best >= 0
}

// depotGoal reports whether v can go to park at the entry of a depot
// station. Some lane must be eligible for v, and the free slots of the
// eligible lanes must outnumber the other pods that go to the entry and
// have no slot.
func (s *Simulation) depotGoal(v *vehicle, station Station) bool {
	depot, ok := s.depots.stations[station.ID]
	if !ok {
		return false
	}
	free := 0
	for lane, state := range s.depotLaneView(depot, v) {
		if _, _, ok := s.depotTarget(depot, lane, state, v.Pod.Class); ok {
			free += len(s.depots.depots[depot].lanes[lane].slots) - state.pods
		}
	}
	for i := range s.vehicles {
		if other := &s.vehicles[i]; other != v && s.depotUnbound(other, station.ID) {
			free--
		}
	}
	return free > 0
}

// depotUnbound reports whether v goes to park at the entry of a depot
// station and has no slot.
func (s *Simulation) depotUnbound(v *vehicle, station string) bool {
	return v.RelocatingTo == station && v.destinationStation == station && v.destination.ID == "" && s.depots.isDepot(station)
}

// depotEntryWait reports whether v waits at the entry of a depot with no
// slot.
func (s *Simulation) depotEntryWait(v *vehicle) bool {
	if !s.depotUnbound(v, v.destinationStation) || len(v.Route) == 0 {
		return false
	}
	station, _ := s.station(v.destinationStation)
	return v.Route[len(v.Route)-1].To == station.Entry
}

// bindDepotArrivals binds the pod that owns the last cell of the incoming
// lane of each depot, when the pod goes to park there with no slot, is in
// service and is not faulted. Thus a depot binds at most one pod in each
// tick, in the order in which the pods reach the entry. The pod keeps its
// blocks, its distance and its reservation, because its route keeps its
// lanes. Binding clears released, so the pod is no longer sent to the
// nearest free berth. With no eligible lane, the pod looks for an open
// berth elsewhere (see parkReleased), and it waits at the entry when it
// finds none.
func (s *Simulation) bindDepotArrivals() {
	if s.depots == nil {
		return
	}
	for depot := range s.depots.depots {
		info := &s.depots.depots[depot]
		cells := s.laneCells[info.incoming.ID]
		owner := s.owners[resource{kind: trackResource, id: info.incoming.ID, cell: cells.count() - 1}]
		v := s.findVehicle(owner.podID())
		if v == nil || v.faulted || !v.inService() || !s.depotEntryWait(v) || v.destinationStation != info.station.ID {
			continue
		}
		suffix, slot, ok := s.depotChoice(v, depot)
		if !ok {
			s.parkReleased(v)
			continue
		}
		s.setVehicleRoute(v, append(slices.Clone(v.Route), suffix...))
		v.destination, v.released, v.pending = slot, false, -1
		if v.Pod.WaitReason == ParkingUnavailable {
			v.Pod.WaitReason, v.Pod.BlockedBy = NoWait, ""
		}
	}
}

// depotIdleSlot returns the slot at which v is idle.
func (s *Simulation) depotIdleSlot(v *vehicle) (depotSlot, bool) {
	if s.depots == nil || v.Pod.Activity != Idle {
		return depotSlot{}, false
	}
	return s.depots.slot(v.Pod.BerthID)
}

// depotOldestHead returns the head of a depot that leaves first, or nil. A
// head is a pod idle at slot 1 of its lane. The head must be in service,
// not faulted, and accepted by accept, and no lane of its lead-out or
// throat path can be blocked. The head that parked first wins, and then
// the lowest lane index.
func (s *Simulation) depotOldestHead(depot int, accept func(*vehicle) bool) *vehicle {
	type head struct {
		v    *vehicle
		lane int
	}
	var heads []head
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if slot, ok := s.depotIdleSlot(v); ok && slot.depot == depot && slot.slot == 0 {
			heads = append(heads, head{v: v, lane: slot.lane})
		}
	}
	slices.SortFunc(heads, func(a, b head) int {
		return cmp.Or(cmp.Compare(a.v.parkedSince, b.v.parkedSince), cmp.Compare(a.lane, b.lane))
	})
	for _, h := range heads {
		if h.v.inService() && !h.v.faulted && !s.depotExitBlocked(depot, h.lane) && accept(h.v) {
			return h.v
		}
	}
	return nil
}

// depotExitBlocked reports whether an active fault blocks a lane of the
// lead-out or the throat path of a depot lane.
func (s *Simulation) depotExitBlocked(depot, lane int) bool {
	if s.blocked.lanes == nil {
		return false
	}
	path := &s.depots.depots[depot].lanes[lane]
	return slices.ContainsFunc(slices.Concat([]Lane{path.leadOut}, path.throat), func(lane Lane) bool {
		index, ok := s.graph.lanes[lane.ID]
		return ok && s.blocked.lanes[index]
	})
}

// depotGate keeps the result of depotOldestHead for each depot, for one
// caller and one accept function.
type depotGate struct {
	s      *Simulation
	accept func(*vehicle) bool
	heads  map[int]*vehicle
}

// releasable reports whether v can leave its place: v is not idle at a
// depot slot, or it is the oldest head of its depot that accept accepts.
func (g *depotGate) releasable(v *vehicle) bool {
	slot, ok := g.s.depotIdleSlot(v)
	if !ok {
		return true
	}
	head, known := g.heads[slot.depot]
	if !known {
		head = g.s.depotOldestHead(slot.depot, g.accept)
		if g.heads == nil {
			g.heads = make(map[int]*vehicle)
		}
		g.heads[slot.depot] = head
	}
	return head == v
}

// depotReleasable is releasable for one pod.
func (s *Simulation) depotReleasable(v *vehicle, accept func(*vehicle) bool) bool {
	gate := depotGate{s: s, accept: accept}
	return gate.releasable(v)
}

// advanceDepots moves pods up in each depot lane. A pod idle at a slot
// moves to the slot ahead when no pod of the lane takes that slot in the
// lane view, and when the pod is in service, not faulted, empty, and named
// by no waiting trip. The move reserves nothing: the grants of the pod take
// the slot resources. The view is the one at the start of the pass, so a
// pod behind a pod that starts to move up follows it in the next tick.
func (s *Simulation) advanceDepots() {
	if s.depots == nil {
		return
	}
	taken := make(map[depotSlot]bool)
	for i := range s.vehicles {
		if slot, ok := s.depotSlotOf(&s.vehicles[i]); ok {
			taken[slot] = true
		}
	}
	for i := range s.vehicles {
		v := &s.vehicles[i]
		slot, ok := s.depotIdleSlot(v)
		if !ok || slot.slot == 0 {
			continue
		}
		ahead := slot
		ahead.slot--
		if taken[ahead] || !v.inService() || v.faulted || v.Pod.Occupied || s.assigned(v.Pod.ID) {
			continue
		}
		info := &s.depots.depots[ahead.depot]
		_ = s.startEmptyMove(v, emptyDestination{station: info.station.ID, berth: info.lanes[ahead.lane].slots[ahead.slot]})
	}
}

// arriveDepot handles the arrival of v at a depot slot. It marks the
// arrival for releaseDepotOrigin. An arrival from the slot behind is a
// move-up and keeps parkedSince. Each other arrival sets it to the tick,
// at least 1.
func (s *Simulation) arriveDepot(v *vehicle) {
	slot, ok := s.depots.slot(v.destination.ID)
	if !ok {
		return
	}
	v.depotArrived = true
	from, ok := s.depots.slot(v.origin.ID)
	if !ok || from.depot != slot.depot || from.lane != slot.lane || from.slot != slot.slot+1 {
		v.parkedSince = max(s.tick, 1)
	}
}

// releaseDepotOrigin releases the origin of a pod that arrived at a depot
// slot in this tick. motion releases the origin only while the pod
// travels, and a pod that arrives in the tick in which it passes the
// origin tail is idle, so it would keep its origin. The release skips the
// berth and the node of the destination.
func (s *Simulation) releaseDepotOrigin(v *vehicle) {
	v.depotArrived = false
	if v.originReleased || v.distance < v.originTail() {
		return
	}
	destination := berthResources(v.destination)
	for _, r := range berthResources(v.origin) {
		if !slices.Contains(destination[:], r) {
			s.releaseOwned(v, r)
		}
	}
	v.originReleased = true
}
