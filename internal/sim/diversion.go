package sim

import (
	"errors"
	"slices"
)

// pickupRoute includes the track already committed by a pod moving toward parking.
func (s *Simulation) pickupRoute(v *vehicle, stationID string) ([]Lane, Berth, bool) {
	if v.Pod.Occupied || s.assigned(v.Pod.ID) {
		return nil, Berth{}, false
	}
	if v.Pod.Activity == Idle {
		from, _ := s.network.Station(v.Pod.StationID)
		berth, _ := from.berth(v.Pod.BerthID)
		route, destination, err := s.stationRoute(berth.Node, stationID)
		return route, destination, err == nil
	}
	destination, ok := s.network.Station(v.RelocatingTo)
	if !ok || (!destination.ParkingOnly && !v.Rebalancing) {
		return nil, Berth{}, false
	}
	if v.Pod.Activity != Traveling && v.Pod.Activity != DepartingEmpty {
		return nil, Berth{}, false
	}
	prefix := 0
	from := v.origin.Node
	if v.reservedThrough >= 0 {
		committed := v.blocks[v.reservedThrough]
		end := committed.laneStart + s.laneLength(committed.lane)
		distance := 0.0
		for i, lane := range v.Route {
			// Finish a committed parking inlet before returning to service.
			if lane.To == v.destination.Node {
				return nil, Berth{}, false
			}
			distance += s.laneLength(lane)
			prefix, from = i+1, lane.To
			if distance >= end-1e-9 {
				break
			}
		}
	}
	suffix, berth, err := s.stationRoute(from, stationID)
	if err != nil {
		return nil, Berth{}, false
	}
	return append(slices.Clone(v.Route[:prefix]), suffix...), berth, true
}

func (s *Simulation) pickupSeconds(v *vehicle, route []Lane) float64 {
	motion := motionEstimate{}
	if v.Pod.Activity != Idle {
		motion = motionEstimate{distance: v.distance, speed: v.Pod.Speed}
	}
	return s.routeSeconds(route, motion)
}

func (s *Simulation) sendPickup(v *vehicle, stationID string) error {
	station, _ := s.network.Station(stationID)
	if v.Pod.Activity == Idle {
		from, _ := s.network.Station(v.Pod.StationID)
		origin, _ := from.berth(v.Pod.BerthID)
		_, berth, err := s.stationRoute(origin.Node, stationID)
		if err != nil {
			return err
		}
		return s.startEmptyMove(v, emptyDestination{station: stationID, berth: berth})
	}
	route, berth, ok := s.pickupRoute(v, stationID)
	if !ok {
		return errors.New("pod cannot divert before its committed maneuver finishes")
	}
	// Only the unused parking claims are released. Admitted track stays owned.
	for _, r := range []resource{{kind: berthResource, id: v.destination.ID}, {kind: nodeResource, id: v.destination.Node}} {
		if s.owners[r] == v.Pod.ID {
			delete(s.owners, r)
		}
	}
	v.Route, v.blocks = route, s.routeBlocks(route)
	v.destination, v.destinationStation = berth, station.ID
	v.RelocatingTo = stationID
	v.Rebalancing = false
	if len(route) == 0 {
		v.Pod.Activity = Idle
		v.RelocatingTo = ""
	}
	v.pending = -1
	v.Pod.WaitReason, v.Pod.BlockedBy = NoWait, ""
	return nil
}
