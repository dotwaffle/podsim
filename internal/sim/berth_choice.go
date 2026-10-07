package sim

import "slices"

// assignTerminalBerth chooses a berth when the next reservation starts on
// the final lane of the road route, or when it reaches the last block of
// that route. With platoons on, it also tries earlier (see
// earlyBerthChoice). The road route remains berth-independent.
func (s *Simulation) assignTerminalBerth(v *vehicle) bool {
	if v.destination.ID != "" || len(v.Route) == 0 {
		return true
	}
	// An empty recovery chooses its berth as a passenger route does.
	passenger := v.Pod.Occupied || v.Pod.Activity == Boarding && len(v.Riders) > 0 || s.assigned(v.Pod.ID) ||
		v.op.purpose == opEmptyRecovery
	if !passenger {
		return true
	}
	next := v.reservedThrough + 1
	if next < 0 || next >= v.blocks.len() {
		return true
	}
	early := false
	if v.blocks.lane(next).ID != v.Route[len(v.Route)-1].ID {
		// A reservation that starts before the final lane can reach the
		// last block of the route. For example, the junction zone of the
		// station entry can cover all of a short final lane. After that
		// reservation, the pod reserves no more blocks of the road route.
		// Thus it must choose its berth now, or it arrives with no berth.
		if _, through, _ := s.terminalLane(v); through < v.blocks.len()-1 {
			if !s.earlyBerthChoice(v, through) {
				return true
			}
			early = true
		}
	}
	// A failed early try does not refuse the pod. The usual point tries
	// again and reports the failure.
	station, ok := s.station(v.destinationStation)
	if !ok {
		return early
	}
	suffix, berth, err := s.stationRouteByLoad(stationRouteInput{from: station.routeEntry(v.Route, v.destination), station: station.ID, class: v.Pod.Class, accept: s.berthFilterForVehicle(v)})
	if err != nil {
		return early
	}
	s.setVehicleRoute(v, append(slices.Clone(v.Route), suffix...))
	v.destination = berth
	v.pending = -1
	return true
}

// earlyBerthChoice reports whether v tries its berth choice before the
// usual point, when through is the last block of its next reservation.
// With platoons on, a pod that is not of a large class tries when the
// final lane of its route is the entry lane of its destination station,
// and its next reservation reaches that lane or the lane starts within
// platoonHorizon. Then its run can grow onto the entry lane (see
// sharedLane), which needs a lane after the entry lane in both routes.
// The berth choice of a large class reads the station load, so it keeps
// the usual point.
func (s *Simulation) earlyBerthChoice(v *vehicle, through int) bool {
	if s.platooning == PlatooningOff || largeVehicleClass(v.Pod.Class) {
		return false
	}
	final := len(v.Route) - 1
	if lane := &v.Route[final]; lane.StationRole != StationEntryRole || lane.StationID != v.destinationStation {
		return false
	}
	return through >= v.blocks.laneFirst(final) || v.blocks.lanes[final].start-v.distance <= platoonHorizon
}

// reevaluateTerminalBerth chooses a free inlet before the pod commits to its
// final branch. Existing track ownership and movement state remain unchanged.
// The new route keeps the lanes before the inlet, so it can be longer than
// the station path to the berth. The pod does not take a berth that takes a
// rider over maxSharedRideDetour. See rerouteKeepsDetours. An emergency
// unload skips this test: every rider leaves at the emergency station, so
// no rider has an onward stop (section 9.2 of the incident emergency
// contract).
func (s *Simulation) reevaluateTerminalBerth(v *vehicle) {
	next := v.reservedThrough + 1
	if next < 0 || next >= v.blocks.len() || len(v.Route) == 0 {
		return
	}
	eligible, through, ok := s.terminalLane(v)
	if !ok {
		return
	}
	// An empty recovery leaves a taken berth as a passenger route does.
	// No other path moves it, because it is withdrawn.
	passenger := v.Pod.Occupied || v.Pod.Activity == Boarding && len(v.Riders) > 0 || s.assigned(v.Pod.ID) ||
		v.op.purpose == opEmptyRecovery
	accept := s.berthFilterForVehicle(v)
	if !passenger || s.berthAvailableFor(v, v.destination) && (accept == nil || accept(v.destination)) {
		return
	}
	// terminalLane found the station, and the network does not change.
	station, _ := s.station(v.destinationStation)
	for routeIndex := eligible; routeIndex < len(v.Route); routeIndex++ {
		lane := v.Route[routeIndex]
		first := v.firstBlockForLane(lane.ID)
		if first > through || s.inLinkRun(v, routeIndex) {
			return
		}
		for _, berth := range station.Berths {
			if berth.ID == v.destination.ID || accept != nil && !accept(berth) || !s.berthAvailableFor(v, berth) {
				continue
			}
			suffix, err := s.stationPathForClass(lane.From, berth.Node, v.Pod.Class)
			if err != nil || len(suffix) == 0 || suffix[0].ID == lane.ID {
				continue
			}
			route := append(slices.Clone(v.Route[:routeIndex]), suffix...)
			if v.op.purpose != opEmergencyUnload && !s.rerouteKeepsDetours(v, route, berth) {
				continue
			}
			blocks, lengths := s.routeBlocks(route)
			if first >= blocks.len() || blocks.at(first).lane.ID != suffix[0].ID {
				continue
			}
			v.replaceRoute(route)
			v.blocks, v.routeLengths, v.destination = blocks, lengths, berth
			v.blockStarts = indexBlockStarts(&blocks, len(route))
			v.terminal = terminalCheck{}
			v.pending = -1
			return
		}
	}
}

// terminalCheck is the result of terminalLane for one reservation of a
// route. It is valid while known is true, the route does not change, and
// the pod has the same destination station and reservedThrough.
type terminalCheck struct {
	station           string
	reservedThrough   int
	eligible, through int
	known             bool
}

// terminalLane finds the first route lane, from the station entry on, whose
// first block the pod has not reserved. It returns the index of that lane
// and the last block of the next reservation. It returns false if the
// station is not known, if the pod has reserved the first block of each of
// these lanes, or if the next reservation stops before that lane. Then the
// pod keeps its berth. When the station is known, through is the last block
// of the next reservation also when ok is false.
//
// The result depends only on the route, the blocks, the destination
// station, reservedThrough, and the station entry in the network. The
// network does not change during a run. A pod keeps the same reservedThrough for
// many ticks between two grants, so the function keeps the result in
// v.terminal until one of these values changes. The route must not be
// empty, and the next block must exist.
func (s *Simulation) terminalLane(v *vehicle) (eligible, through int, ok bool) {
	c := &v.terminal
	if !c.known || c.reservedThrough != v.reservedThrough || c.station != v.destinationStation {
		eligible, through = s.findTerminalLane(v)
		*c = terminalCheck{
			station: v.destinationStation, reservedThrough: v.reservedThrough,
			eligible: eligible, through: through, known: true,
		}
	}
	return c.eligible, c.through, c.eligible >= 0
}

// findTerminalLane does the work of terminalLane without the cache. It
// returns -1 in place of false. through is the last block of the next
// reservation, also when eligible is -1. It is 0 when the station is not
// known.
func (s *Simulation) findTerminalLane(v *vehicle) (eligible, through int) {
	station, ok := s.station(v.destinationStation)
	if !ok {
		return -1, 0
	}
	stationStart := stationRouteStart(v.Route, station.routeEntry(v.Route, v.destination))
	through = reservationEnd(&v.blocks, v.reservedThrough+1)
	for routeIndex := stationStart; routeIndex < len(v.Route); routeIndex++ {
		first := v.firstBlockForLane(v.Route[routeIndex].ID)
		if first <= v.reservedThrough {
			continue
		}
		if first > through {
			return -1, through
		}
		return routeIndex, through
	}
	return -1, through
}

func stationRouteStart(route []Lane, entry string) int {
	for i, lane := range route {
		if lane.From == entry {
			return i
		}
	}
	return len(route) - 1
}

func firstBlockForLane(blocks *blockList, laneID string) int {
	for index, lane := range blocks.route {
		if lane.ID == laneID {
			return blocks.laneFirst(index)
		}
	}
	return blocks.len()
}

func (s *Simulation) berthAvailableFor(v *vehicle, berth Berth) bool {
	if !s.berthAvailableTo(v, berth) {
		return false
	}
	for i := range s.vehicles {
		other := &s.vehicles[i]
		if other != v && other.Pod.Activity != Idle && other.destination.ID == berth.ID {
			return false
		}
	}
	return true
}
