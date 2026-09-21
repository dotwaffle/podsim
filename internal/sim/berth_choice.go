package sim

import "slices"

// reevaluateTerminalBerth chooses a free inlet before the pod commits to its
// final branch. Existing track ownership and movement state remain unchanged.
func (s *Simulation) reevaluateTerminalBerth(v *vehicle) {
	next := v.reservedThrough + 1
	if next < 0 || next+1 >= len(v.blocks) || !v.blocks[next].last || v.blocks[next+1].cell != 0 {
		return
	}
	terminal := v.Route[len(v.Route)-1]
	if v.blocks[next+1].lane.ID != terminal.ID {
		return
	}
	passenger := v.Pod.Occupied || v.Pod.Activity == Boarding && v.Request != nil || s.assigned(v.Pod.ID)
	if !passenger || s.berthAvailableFor(v, v.destination) {
		return
	}
	station, ok := s.network.Station(v.destinationStation)
	if !ok {
		return
	}
	fork := terminal.From
	for _, berth := range station.Berths {
		if berth.ID == v.destination.ID || !s.berthAvailableFor(v, berth) {
			continue
		}
		suffix, err := s.route(fork, berth.Node)
		if err != nil || len(suffix) != 1 || suffix[0].ID == terminal.ID {
			continue
		}
		route := append(slices.Clone(v.Route[:len(v.Route)-1]), suffix...)
		blocks := s.routeBlocks(route)
		if next+1 >= len(blocks) || blocks[next].lane.ID != v.blocks[next].lane.ID || blocks[next+1].lane.ID != suffix[0].ID {
			continue
		}
		v.Route, v.blocks, v.destination = route, blocks, berth
		v.pending = -1
		return
	}
}

func (s *Simulation) berthAvailableFor(v *vehicle, berth Berth) bool {
	for _, r := range []resource{{kind: berthResource, id: berth.ID}, {kind: nodeResource, id: berth.Node}} {
		if owner := s.owners[r]; owner != "" && owner != v.Pod.ID {
			return false
		}
	}
	for i := range s.vehicles {
		other := &s.vehicles[i]
		if other != v && other.Pod.Activity != Idle && other.destination.ID == berth.ID {
			return false
		}
	}
	return true
}
