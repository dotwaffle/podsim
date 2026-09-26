package sim

import "slices"

// assignTerminalBerth chooses a berth when the next reservation enters the
// final station-access lane. The road route remains berth-independent.
func (s *Simulation) assignTerminalBerth(v *vehicle) bool {
	if v.destination.ID != "" || len(v.Route) == 0 {
		return true
	}
	passenger := v.Pod.Occupied || v.Pod.Activity == Boarding && v.Request != nil || s.assigned(v.Pod.ID)
	if !passenger {
		return true
	}
	next := v.reservedThrough + 1
	if next < 0 || next >= len(v.blocks) || v.blocks[next].lane.ID != v.Route[len(v.Route)-1].ID {
		return true
	}
	station, ok := s.station(v.destinationStation)
	if !ok {
		return false
	}
	suffix, berth, err := s.stationRoute(station.Entry, station.ID)
	if err != nil {
		return false
	}
	s.setVehicleRoute(v, append(slices.Clone(v.Route), suffix...))
	v.destination = berth
	v.pending = -1
	return true
}

// reevaluateTerminalBerth chooses a free inlet before the pod commits to its
// final branch. Existing track ownership and movement state remain unchanged.
func (s *Simulation) reevaluateTerminalBerth(v *vehicle) {
	next := v.reservedThrough + 1
	if next < 0 || next >= len(v.blocks) || len(v.Route) == 0 {
		return
	}
	eligible, through, ok := s.terminalLane(v)
	if !ok {
		return
	}
	passenger := v.Pod.Occupied || v.Pod.Activity == Boarding && v.Request != nil || s.assigned(v.Pod.ID)
	if !passenger || s.berthAvailableFor(v, v.destination) {
		return
	}
	// terminalLane found the station, and the network does not change.
	station, _ := s.station(v.destinationStation)
	for routeIndex := eligible; routeIndex < len(v.Route); routeIndex++ {
		lane := v.Route[routeIndex]
		first := v.firstBlockForLane(lane.ID)
		if first > through {
			return
		}
		for _, berth := range station.Berths {
			if berth.ID == v.destination.ID || !s.berthAvailableFor(v, berth) {
				continue
			}
			suffix, err := s.stationPath(lane.From, berth.Node)
			if err != nil || len(suffix) == 0 || suffix[0].ID == lane.ID {
				continue
			}
			route := append(slices.Clone(v.Route[:routeIndex]), suffix...)
			blocks := s.routeBlocks(route)
			if first >= len(blocks) || blocks[first].lane.ID != suffix[0].ID {
				continue
			}
			v.Route, v.blocks, v.destination = route, blocks, berth
			v.blockStarts = indexBlockStarts(blocks, len(route))
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
// pod keeps its berth.
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
// returns -1 in place of false. When eligible is -1, through has no
// meaning.
func (s *Simulation) findTerminalLane(v *vehicle) (eligible, through int) {
	station, ok := s.station(v.destinationStation)
	if !ok {
		return -1, 0
	}
	stationStart := stationRouteStart(v.Route, station.Entry)
	through = reservationEnd(v.blocks, v.reservedThrough+1)
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

func firstBlockForLane(blocks []block, laneID string) int {
	for i, block := range blocks {
		if block.lane.ID == laneID {
			return i
		}
	}
	return len(blocks)
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
