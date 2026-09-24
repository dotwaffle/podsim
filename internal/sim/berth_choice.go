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
	station, ok := s.station(v.destinationStation)
	if !ok {
		return
	}
	stationStart := stationRouteStart(v.Route, station.Entry)
	through := reservationEnd(v.blocks, next)
	eligible := len(v.Route)
	for routeIndex := stationStart; routeIndex < len(v.Route); routeIndex++ {
		lane := v.Route[routeIndex]
		first := v.firstBlockForLane(lane.ID)
		if first <= v.reservedThrough {
			continue
		}
		if first > through {
			return
		}
		eligible = routeIndex
		break
	}
	passenger := v.Pod.Occupied || v.Pod.Activity == Boarding && v.Request != nil || s.assigned(v.Pod.ID)
	if eligible == len(v.Route) || !passenger || s.berthAvailableFor(v, v.destination) {
		return
	}
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
			v.pending = -1
			return
		}
	}
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
