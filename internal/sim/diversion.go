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
	// pickupRouteWithAssignments uses berthLoad. The load does not change
	// ok, so a caller that uses only ok can give noBerthLoad instead.
	load   func(Berth) int
	accept func(Berth) bool
}

// pickupRouteWithAssignments returns the route and the berth for a pickup
// by the pod at the station. For an idle pod at the pickup station, it
// returns no lanes and the berth of the pod, so the pickup costs no travel.
// It reports false when the pod is occupied or claimed, when it is not idle
// and cannot divert, when it must first finish a committed parking inlet,
// or when it cannot reach the station.
func (s *Simulation) pickupRouteWithAssignments(input pickupRouteInput) ([]Lane, Berth, bool) {
	if !s.pickupCandidate(input.pod, input.assigned) {
		return nil, Berth{}, false
	}
	return s.candidateRouteMatching(input.pod, input.station, input.load, input.accept)
}

// pickupCandidate holds the tests of pickupRouteWithAssignments that do not
// depend on the pickup station. It reports false when the pod is occupied,
// claimed, or withdrawn, or when it is not idle and cannot divert. assigned
// is as in pickupRouteInput. pickupCandidate only reads the simulation.
func (s *Simulation) pickupCandidate(v *vehicle, assigned map[string]bool) bool {
	if v.Pod.Occupied || !v.inService() {
		return false
	}
	claimed := assigned[v.Pod.ID]
	if assigned == nil {
		claimed = s.assigned(v.Pod.ID)
	}
	if claimed {
		return false
	}
	if v.Pod.Activity == Idle {
		return true
	}
	destination, ok := s.station(v.RelocatingTo)
	return ok && (destination.ParkingOnly || v.Rebalancing || v.released)
}

// candidateRoute is pickupRouteWithAssignments for a pod that
// pickupCandidate accepts. load is as in pickupRouteInput.
func (s *Simulation) candidateRoute(v *vehicle, stationID string, load func(Berth) int) ([]Lane, Berth, bool) {
	return s.candidateRouteMatching(v, stationID, load, nil)
}

func (s *Simulation) candidateRouteMatching(v *vehicle, stationID string, load func(Berth) int, accept func(Berth) bool) ([]Lane, Berth, bool) {
	prefix, suffix, berth, ok := s.candidateRoutePartsMatching(v, stationID, load, accept)
	if v.Pod.Activity == Idle {
		return suffix, berth, ok
	}
	if !ok {
		return nil, Berth{}, false
	}
	return append(slices.Clone(prefix), suffix...), berth, true
}

// candidateRouteParts checks the same route without joining its parts.
// The parts borrow vehicle routes and cached routes. Callers must not change them.
func (s *Simulation) candidateRouteParts(v *vehicle, stationID string, load func(Berth) int) ([]Lane, []Lane, Berth, bool) {
	return s.candidateRoutePartsMatching(v, stationID, load, nil)
}

func (s *Simulation) candidateRoutePartsMatching(v *vehicle, stationID string, load func(Berth) int, accept func(Berth) bool) ([]Lane, []Lane, Berth, bool) {
	if v.Pod.Activity == Idle {
		from, _ := s.station(v.Pod.StationID)
		berth, _ := from.berth(v.Pod.BerthID)
		if v.Pod.StationID == stationID {
			// The pod can board where it is. Its own berth has a load of at
			// least one because the pod holds it, so stationRouteByLoad
			// would choose a free berth and a loop around the network.
			return nil, nil, berth, accept == nil || accept(berth)
		}
		route, destination, err := s.stationRouteByLoad(stationRouteInput{class: v.Pod.Class, from: berth.Node, station: stationID, load: load, accept: accept})
		return nil, route, destination, err == nil
	}
	prefix, from, ok := s.divertStart(v)
	// A candidate whose kept lanes cross a blocked lane cannot reach the
	// pickup.
	if !ok || s.keptLaneBlocked(v, prefix) {
		return nil, nil, Berth{}, false
	}
	suffix, berth, err := s.stationRouteByLoad(stationRouteInput{class: v.Pod.Class, from: from, station: stationID, load: load, accept: accept})
	if err != nil {
		return nil, nil, Berth{}, false
	}
	return v.Route[:prefix], suffix, berth, true
}

// divertStart returns the route prefix that a moving empty pod must keep
// and the node where its new route can start. The prefix keeps every lane
// touched by reserved track. Only departing or traveling pods can divert.
// A pod must finish its committed berth inlet and the full arrival chain
// once it reserves a lane leaving the destination station's entry.
// Pods in a platoon cannot divert because their links depend on the routes.
func (s *Simulation) divertStart(v *vehicle) (int, string, bool) {
	if v.Pod.Activity != Traveling && v.Pod.Activity != DepartingEmpty || v.linked() {
		return 0, "", false
	}
	prefix, from := 0, v.origin.Node
	if v.reservedThrough < 0 {
		return prefix, from, true
	}
	station, _ := s.station(v.destinationStation)
	committed := v.blocks.at(v.reservedThrough)
	end := committed.laneStart + s.laneLength(committed.lane)
	distance := 0.0
	for i, lane := range v.Route {
		// A new route inside the arrival chain could cross another berth
		// that a following pod reserved, leaving both pods blocked.
		if station.isEntry(lane.From) || lane.To == v.destination.Node {
			return 0, "", false
		}
		distance += s.laneLength(lane)
		prefix, from = i+1, lane.To
		if distance >= end-1e-9 {
			break
		}
	}
	// A restored route can omit the entry lane already behind the pod.
	// Check whether the remaining endpoint is inside the arrival chain.
	// stationPathForClass cannot pass a berth or a station boundary. The
	// check is structural, so it asks the static graph: a blocked lane must
	// not make it allow a diversion.
	if !station.isEntry(from) && !station.isExit(from) {
		if s.staticConnection(station.routeEntry(v.Route, v.destination), from, true, v.Pod.Class) {
			return 0, "", false
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
	return s.sendPickupMatching(v, stationID, nil)
}

func (s *Simulation) sendPickupForRequest(v *vehicle, request Request) error {
	return s.sendPickupMatching(v, request.legOrigin(), s.pickupBerthFilter(v, request))
}

func (s *Simulation) sendPickupMatching(v *vehicle, stationID string, accept func(Berth) bool) error {
	station, _ := s.station(stationID)
	if v.Pod.Activity == Idle {
		from, _ := s.station(v.Pod.StationID)
		origin, _ := from.berth(v.Pod.BerthID)
		_, berth, err := s.stationRouteByLoad(stationRouteInput{from: origin.Node, station: stationID, class: v.Pod.Class, accept: accept})
		if err != nil {
			return err
		}
		if err := s.startEmptyMove(v, emptyDestination{station: stationID, berth: berth}); err != nil {
			return err
		}
		s.bufferPickup(v)
		return nil
	}
	route, berth, ok := s.pickupRouteWithAssignments(pickupRouteInput{pod: v, station: stationID, accept: accept})
	if !ok {
		return errors.New("pod cannot divert before its committed maneuver finishes")
	}
	// pickupRoute chose the berth with free-flow routes, as pickupPod did.
	// The pod keeps that berth. Only the route from the divert node to the
	// berth can change. Inside the station, the route has no alternative.
	if prefix, from, _ := s.divertStart(v); s.costedRouting() && !station.isEntry(from) {
		if suffix, err := s.assignedRoute(v, from, berth.Node); err == nil {
			route = append(slices.Clone(v.Route[:prefix]), suffix...)
		}
	}
	s.redirect(v, redirection{route: route, berth: berth, station: station.ID})
	s.bufferPickup(v)
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
		s.releaseOwned(v, r)
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
