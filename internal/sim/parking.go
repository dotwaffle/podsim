package sim

import (
	"errors"
	"fmt"
)

// ErrBerthUnavailable means an empty move cannot claim its destination.
var ErrBerthUnavailable = errors.New("destination berth is occupied or reserved")

// clearBlockedBerths runs after admission. Empty departures compete for track on the next tick.
func (s *Simulation) clearBlockedBerths() {
	for i := range s.vehicles {
		arrival := &s.vehicles[i]
		if (!arrival.Pod.Occupied && !s.assigned(arrival.Pod.ID)) || arrival.Pod.WaitReason != BerthOccupied {
			continue
		}
		blocker := s.findVehicle(arrival.Pod.BlockedBy)
		if blocker == nil || blocker.Pod.Activity != Idle || blocker.Pod.Occupied || s.assigned(blocker.Pod.ID) || blocker.Pod.BerthID != arrival.destination.ID {
			continue
		}
		if !s.park(blocker) {
			arrival.Pod.WaitReason = ParkingUnavailable
		}
	}
}

// park reserves a reachable destination before an empty pod leaves its passenger berth.
func (s *Simulation) park(v *vehicle) bool {
	for _, station := range s.network.Stations {
		if !station.ParkingOnly {
			continue
		}
		for _, berth := range station.Berths {
			if s.startEmptyMove(v, emptyDestination{station: station.ID, berth: berth, reserveBerth: true}) == nil {
				return true
			}
		}
	}
	return false
}

type emptyDestination struct {
	station      string
	berth        Berth
	reserveBerth bool
	rebalance    bool
}

func (s *Simulation) startEmptyMove(v *vehicle, to emptyDestination) error {
	space := resource{kind: berthResource, id: to.berth.ID}
	node := resource{kind: nodeResource, id: to.berth.Node}
	if to.reserveBerth && (s.owners[space] != "" || s.owners[node] != "") {
		return ErrBerthUnavailable
	}
	from, _ := s.network.Station(v.Pod.StationID)
	origin, _ := from.berth(v.Pod.BerthID)
	route, err := s.network.Route(origin.Node, to.berth.Node)
	if err != nil {
		return fmt.Errorf("empty route %s to %s: %w", from.ID, to.station, err)
	}
	if to.reserveBerth {
		s.owners[space], s.owners[node] = v.Pod.ID, v.Pod.ID
	}
	v.origin, v.destination, v.destinationStation = origin, to.berth, to.station
	v.Route, v.blocks = route, s.routeBlocks(route)
	v.RelocatingTo = to.station
	v.Rebalancing = to.rebalance
	v.Pod.Activity, v.Pod.WaitReason, v.Pod.BlockedBy = DepartingEmpty, NoWait, ""
	v.phaseTicks, v.blockIndex, v.reservedThrough = 0, 0, -1
	v.distance, v.pending = 0, -1
	return nil
}
