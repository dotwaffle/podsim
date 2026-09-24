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
// by the pod at the station. For an idle pod at the pickup station, it
// returns no lanes and the berth of the pod, so the pickup costs no travel.
// It reports false when the pod is occupied or claimed, when it is not idle
// and cannot divert, when it must first finish a committed parking inlet,
// or when it cannot reach the station.
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
		if v.Pod.StationID == stationID {
			// The pod can board where it is. Its own berth has a load of at
			// least one because the pod holds it, so stationRouteByLoad
			// would choose a free berth and a loop around the network.
			return nil, berth, true
		}
		route, destination, err := s.stationRouteByLoad(stationRouteInput{from: berth.Node, station: stationID, load: input.load})
		return route, destination, err == nil
	}
	destination, ok := s.station(v.RelocatingTo)
	if !ok || (!destination.ParkingOnly && !v.Rebalancing && !v.released) {
		return nil, Berth{}, false
	}
	prefix, from, ok := s.divertStart(v)
	if !ok {
		return nil, Berth{}, false
	}
	suffix, berth, err := s.stationRouteByLoad(stationRouteInput{from: from, station: stationID, load: input.load})
	if err != nil {
		return nil, Berth{}, false
	}
	return append(slices.Clone(v.Route[:prefix]), suffix...), berth, true
}

// divertStart returns the number of route lanes that a moving empty pod
// must keep and the node where a new route can start. The pod keeps each
// lane that its reserved blocks touch. It reports false when the pod is not
// departing or traveling, or when its reserved blocks enter its destination
// berth. Such a pod must finish its committed inlet.
func (s *Simulation) divertStart(v *vehicle) (int, string, bool) {
	if v.Pod.Activity != Traveling && v.Pod.Activity != DepartingEmpty {
		return 0, "", false
	}
	prefix, from := 0, v.origin.Node
	if v.reservedThrough < 0 {
		return prefix, from, true
	}
	committed := v.blocks[v.reservedThrough]
	end := committed.laneStart + s.laneLength(committed.lane)
	distance := 0.0
	for i, lane := range v.Route {
		// Finish a committed inlet before returning to service.
		if lane.To == v.destination.Node {
			return 0, "", false
		}
		distance += s.laneLength(lane)
		prefix, from = i+1, lane.To
		if distance >= end-1e-9 {
			break
		}
	}
	return prefix, from, true
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
	s.redirect(v, redirection{route: route, berth: berth, station: station.ID})
	v.released = false
	return nil
}

// redirection is the input of redirect.
type redirection struct {
	route   []Lane
	berth   Berth
	station string
}

// redirect gives an empty moving pod a new route to a berth of a station.
// The route must start with the lanes that divertStart keeps. The pod
// releases its unused destination claims. It keeps the track that it
// already reserved. A pod with an empty route stops at its origin berth
// and becomes idle.
func (s *Simulation) redirect(v *vehicle, to redirection) {
	for _, r := range berthResources(v.destination) {
		if s.owners[r] == v.Pod.ID {
			delete(s.owners, r)
		}
	}
	s.setVehicleRoute(v, to.route)
	v.destination, v.destinationStation = to.berth, to.station
	v.RelocatingTo = to.station
	v.Rebalancing = false
	if len(to.route) == 0 {
		v.Pod.Activity = Idle
		v.RelocatingTo = ""
		v.released = false
	}
	v.pending = -1
	v.Pod.WaitReason, v.Pod.BlockedBy = NoWait, ""
}
