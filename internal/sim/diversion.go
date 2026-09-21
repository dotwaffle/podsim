package sim

import (
	"errors"
	"slices"
)

// pickupRoute includes the track already committed by a pod moving toward parking.
func (s *Simulation) pickupRoute(v *vehicle, stationID string) ([]Lane, bool) {
	if v.Pod.Occupied || s.assigned(v.Pod.ID) {
		return nil, false
	}
	station, _ := s.network.Station(stationID)
	target := station.Berths[0].Node
	if v.Pod.Activity == Idle {
		from, _ := s.network.Station(v.Pod.StationID)
		berth, _ := from.berth(v.Pod.BerthID)
		route, err := s.network.Route(berth.Node, target)
		return route, err == nil
	}
	parking, ok := s.network.Station(v.RelocatingTo)
	if !ok || !parking.ParkingOnly {
		return nil, false
	}
	if v.Pod.Activity != Traveling && v.Pod.Activity != DepartingEmpty {
		return nil, false
	}
	prefix := 0
	from := v.origin.Node
	if v.reservedThrough >= 0 {
		committed := v.blocks[v.reservedThrough]
		end := committed.laneStart + s.network.Length(committed.lane)
		distance := 0.0
		for i, lane := range v.Route {
			// Finish a committed parking inlet before returning to service.
			if lane.To == v.destination.Node {
				return nil, false
			}
			distance += s.network.Length(lane)
			prefix, from = i+1, lane.To
			if distance >= end-1e-9 {
				break
			}
		}
	}
	suffix, err := s.network.Route(from, target)
	if err != nil {
		return nil, false
	}
	return append(slices.Clone(v.Route[:prefix]), suffix...), true
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
		return s.startEmptyMove(v, emptyDestination{station: stationID, berth: station.Berths[0]})
	}
	route, ok := s.pickupRoute(v, stationID)
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
	v.destination, v.destinationStation = station.Berths[0], stationID
	v.RelocatingTo = stationID
	if len(route) == 0 {
		v.Pod.Activity = Idle
		v.RelocatingTo = ""
	}
	v.pending = -1
	v.Pod.WaitReason, v.Pod.BlockedBy = NoWait, ""
	return nil
}
