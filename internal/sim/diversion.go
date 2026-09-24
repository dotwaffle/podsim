package sim

import (
	"errors"
	"slices"
)

// pickupRoute includes the track already committed by a pod moving toward parking.
func (s *Simulation) pickupRoute(v *vehicle, stationID string) ([]Lane, Berth, bool) {
	return s.pickupRouteWithAssignments(pickupRouteInput{pod: v, station: stationID})
}

// pickupRouteInput is the input of pickupRouteWithAssignments.
type pickupRouteInput struct {
	pod     *vehicle
	station string
	// assigned holds the pods of the waiting trips. When it is nil,
	// pickupRouteWithAssignments reads the waiting trips.
	assigned map[string]bool
	// load gives the same value as berthLoad. When it is nil,
	// pickupRouteWithAssignments uses berthLoad.
	load func(Berth) int
}

// pickupRouteWithAssignments returns the route and the berth for a pickup
// by the pod at the station. It reports false when the pod is occupied or
// claimed, when it is not idle and cannot divert, when it must first finish
// a committed parking inlet, or when it cannot reach the station.
func (s *Simulation) pickupRouteWithAssignments(input pickupRouteInput) ([]Lane, Berth, bool) {
	v, stationID := input.pod, input.station
	claimed := input.assigned[v.Pod.ID]
	if input.assigned == nil {
		claimed = s.assigned(v.Pod.ID)
	}
	if v.Pod.Occupied || claimed {
		return nil, Berth{}, false
	}
	if v.Pod.Activity == Idle {
		from, _ := s.station(v.Pod.StationID)
		berth, _ := from.berth(v.Pod.BerthID)
		route, destination, err := s.stationRouteByLoad(stationRouteInput{from: berth.Node, station: stationID, load: input.load})
		return route, destination, err == nil
	}
	destination, ok := s.station(v.RelocatingTo)
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
	suffix, berth, err := s.stationRouteByLoad(stationRouteInput{from: from, station: stationID, load: input.load})
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
	station, _ := s.station(stationID)
	if v.Pod.Activity == Idle {
		from, _ := s.station(v.Pod.StationID)
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
	s.setVehicleRoute(v, route)
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
