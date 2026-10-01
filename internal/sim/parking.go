package sim

import (
	"errors"
	"fmt"
	"slices"
)

// ErrBerthUnavailable means an empty move cannot claim its destination.
var ErrBerthUnavailable = errors.New("destination berth is occupied or reserved")

// clearBlockedBerths runs after admission. Empty departures compete for track on the next tick.
// An idle empty pod leaves its berth when it blocks a pod that must stop at
// that berth or pass through it. In guarded mode, guardedClear chooses the
// berth first.
func (s *Simulation) clearBlockedBerths() {
	for i := range s.vehicles {
		arrival := &s.vehicles[i]
		if arrival.Pod.WaitReason != BerthOccupied || (!arrival.Pod.Occupied && arrival.RelocatingTo == "" && !s.assigned(arrival.Pod.ID)) {
			continue
		}
		blocker := s.findVehicle(arrival.Pod.BlockedBy)
		if blocker == nil || blocker.Pod.Activity != Idle || blocker.Pod.Occupied || s.assigned(blocker.Pod.ID) || !arrival.entersBerth(blocker.Pod.BerthID) && (!arrival.buffered || arrival.bufferBerth != blocker.Pod.BerthID) {
			continue
		}
		if s.positioning == PositioningGuarded && s.guardedClear(blocker) {
			continue
		}
		if !s.park(blocker) && !s.clearToPassengerBerth(blocker) {
			arrival.Pod.WaitReason = ParkingUnavailable
		}
	}
}

// entersBerth reports whether the part of the route of v that it has not
// reserved enters the berth. The pod can stop at the berth, or it can pass
// through the berth to leave the station. A pod passes through a berth when
// dispatch releases or diverts it after its reserved track enters the berth
// access lanes of a station. From there, each way out of the station goes
// through a berth.
func (v *vehicle) entersBerth(berthID string) bool {
	claim := resource{kind: berthResource, id: berthID}
	for resources := range v.blocks.spanResources(v.reservedThrough+1, v.blocks.len()) {
		if slices.Contains(resources, claim) {
			return true
		}
	}
	return false
}

// clearToPassengerBerth reserves a free passenger berth when dedicated parking is full.
func (s *Simulation) clearToPassengerBerth(v *vehicle) bool {
	from, _ := s.station(v.Pod.StationID)
	origin, _ := from.berth(v.Pod.BerthID)
	for _, requireAvailable := range []bool{true, false} {
		for _, local := range []bool{true, false} {
			for _, station := range s.network.Stations {
				if station.ParkingOnly || (station.ID == from.ID) != local {
					continue
				}
				for _, berth := range station.Berths {
					if berth.ID == origin.ID || (requireAvailable && !s.berthAvailable(berth)) {
						continue
					}
					route, err := s.route(origin.Node, berth.Node)
					if err != nil || len(route) == 0 {
						continue
					}
					if s.startEmptyMove(v, emptyDestination{station: station.ID, berth: berth, reserveBerth: true}) == nil {
						return true
					}
				}
			}
		}
	}
	return false
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
	route, err := s.prepareEmptyMove(v, to)
	if err != nil {
		return err
	}
	s.installEmptyMove(v, to, route)
	return nil
}

func (s *Simulation) prepareEmptyMove(v *vehicle, to emptyDestination) ([]Lane, error) {
	space := resource{kind: berthResource, id: to.berth.ID}
	node := resource{kind: nodeResource, id: to.berth.Node}
	if to.reserveBerth && (s.owners[space] != "" || s.owners[node] != "") {
		return nil, ErrBerthUnavailable
	}
	from, _ := s.station(v.Pod.StationID)
	origin, _ := from.berth(v.Pod.BerthID)
	route, err := s.assignedRoute(v, origin.Node, to.berth.Node)
	if err != nil {
		return nil, fmt.Errorf("empty route %s to %s: %w", from.ID, to.station, err)
	}
	return route, nil
}

func (s *Simulation) installEmptyMove(v *vehicle, to emptyDestination, route []Lane) {
	from, _ := s.station(v.Pod.StationID)
	origin, _ := from.berth(v.Pod.BerthID)
	if to.reserveBerth {
		s.owners[resource{kind: berthResource, id: to.berth.ID}], s.owners[resource{kind: nodeResource, id: to.berth.Node}] = v.Pod.ID, v.Pod.ID
	}
	v.origin, v.destination, v.destinationStation = origin, to.berth, to.station
	s.setVehicleRoute(v, route)
	v.RelocatingTo = to.station
	v.Rebalancing = to.rebalance
	v.released = false
	v.Pod.Activity, v.Pod.WaitReason, v.Pod.BlockedBy = DepartingEmpty, NoWait, ""
	v.phaseTicks, v.blockIndex, v.reservedThrough = 0, 0, -1
	v.originReleased = false
	v.distance, v.pending = 0, -1
}
