package sim

import "slices"

// pickupBerthFilter returns the filter of the pickup berths of request for
// pod v. With an empty blocked set, it is berthFilterForStops(class,
// []string{request.To}). While the blocked set is not empty, it accepts
// only a compatible pickup berth (section 9.4 of the incident suspension
// contract): a berth of the leg origin that is not blocked, that the stop
// filter accepts, and from which pickupBerthFitsRequest finds the onward
// route on the routing graph. This holds also on a network without class
// restrictions, where the stop filter is nil. Each pickup search and each
// pickup installation uses it, so dispatch cannot bind a trip again
// through a berth that pickupAccess refuses.
func (s *Simulation) pickupBerthFilter(v *vehicle, request Request) func(Berth) bool {
	stops := s.berthFilterForStops(v.Pod.Class, []string{request.To})
	if !s.blockedActive() {
		return stops
	}
	return func(berth Berth) bool {
		return !s.berthBlocked(berth) && (stops == nil || stops(berth)) && s.pickupBerthFitsRequest(v, request, berth)
	}
}

// pickupAccess reports whether pod v, bound to request, has an executable
// forward continuation to a berth of request.legOrigin() (section 9.4 of
// the incident suspension contract). It is true when the blocked set is
// empty. It asks only whether a complete forward continuation exists. It
// does not look at resource owners or at the time to arrive, so ordinary
// traffic, an occupied berth, and a later arrival never unbind a trip. It
// writes nothing.
func (s *Simulation) pickupAccess(v *vehicle, request Request) bool {
	if !s.blockedActive() {
		return true
	}
	origin := request.legOrigin()
	// A pod idle at the origin boards there. A blocked onward leg stays a
	// destination access wait at boarding.
	if v.Pod.Activity == Idle && v.Pod.StationID == origin {
		return true
	}
	compatible := s.pickupBerthFilter(v, request)
	if v.RelocatingTo == origin {
		return slices.ContainsFunc(s.reachableEndBerths(v), compatible)
	}
	// The pod is idle at another station, or it finishes other work first.
	var node string
	if v.Pod.Activity == Idle {
		node = s.podBerthNode(v)
	} else {
		var ok bool
		if !s.routeExecutable(v) {
			return false
		}
		if node, ok = s.continuationNode(v); !ok {
			return false
		}
	}
	_, _, err := s.stationRouteByLoad(stationRouteInput{class: v.Pod.Class, from: node, station: origin, load: noBerthLoad, accept: compatible})
	return err == nil
}

// routeExecutable reports whether the current route of v is executable:
// its remaining route has no blocked lane, or endpointRoute gives a route.
// An open route is executable also when the pod cannot divert. A pod that
// is idle or unloads has no current route to travel.
func (s *Simulation) routeExecutable(v *vehicle) bool {
	switch v.Pod.Activity {
	case Traveling, DepartingEmpty, Boarding, Continuing:
	default:
		return true
	}
	if !s.remainingRouteBlocked(v) {
		return true
	}
	_, ok := s.endpointRoute(v)
	return ok
}

// reachableEndBerths returns the berths where the current work of v can
// end. With a route that is not executable, there is none. A route with a
// berth end gives its destination berth when it is not blocked. A route
// with an entry end gives each berth of the destination station that is
// not blocked, that the berth filter of the pod accepts, that the entry
// serves, and that has a station path from the entry on the routing graph.
func (s *Simulation) reachableEndBerths(v *vehicle) []Berth {
	if !s.routeExecutable(v) {
		return nil
	}
	if v.destination.ID != "" {
		if s.berthBlocked(v.destination) {
			return nil
		}
		return []Berth{v.destination}
	}
	station, ok := s.station(v.destinationStation)
	if !ok {
		return nil
	}
	entry := station.routeEntry(v.Route, Berth{})
	accept := s.berthFilterForVehicle(v)
	var berths []Berth
	for _, berth := range station.Berths {
		if s.berthBlocked(berth) || accept != nil && !accept(berth) || station.Banks != nil && station.berthEntry(berth) != entry {
			continue
		}
		if _, err := s.stationPathForClass(entry, berth.Node, v.Pod.Class); err == nil {
			berths = append(berths, berth)
		}
	}
	return berths
}

// continuationNode returns the node where the busy pod v becomes available,
// for rule 4 of pickupAccess. It is the node of finishEstimate, which
// reaches a berth that is not blocked and includes the later stops. For a
// pod with an assigned passenger leg, it does not trust the two shortcuts
// of finishEstimate: the cached trip route and the first berth of the
// destination station. It searches the leg on the routing graph, from the
// berth at the leg origin to the destination of the leg, and it takes the
// final berth from stationRouteByLoad, which skips a blocked berth. It
// reports false when a search fails.
func (s *Simulation) continuationNode(v *vehicle) (string, bool) {
	if v.RelocatingTo != "" {
		for _, trip := range s.waiting {
			if trip.request.PodID == v.Pod.ID {
				return s.legEndNode(v, trip.request)
			}
		}
	}
	node, _, ok := s.finishEstimate(v)
	return node, ok
}

// legEndNode returns the berth node where pod v ends the passenger leg of
// request, which it picks up at its destination station. See
// continuationNode.
func (s *Simulation) legEndNode(v *vehicle, request Request) (string, bool) {
	from := v.destination.Node
	if from == "" {
		station, ok := s.station(v.destinationStation)
		if !ok {
			return "", false
		}
		_, berth, err := s.stationRouteByLoad(stationRouteInput{class: v.Pod.Class, from: station.routeEntry(v.Route, v.destination), station: station.ID, load: noBerthLoad, accept: s.berthFilterForVehicle(v)})
		if err != nil {
			return "", false
		}
		from = berth.Node
	}
	route, err := s.stationApproachRouteForClass(from, request.To, v.Pod.Class)
	if err != nil {
		return "", false
	}
	destination, ok := s.station(request.To)
	if !ok {
		return "", false
	}
	_, berth, err := s.stationRouteByLoad(stationRouteInput{class: v.Pod.Class, from: destination.routeEntry(route, Berth{}), station: destination.ID, load: noBerthLoad})
	if err != nil {
		return "", false
	}
	return berth.Node, true
}

// keptLaneBlocked reports whether a lane of the route of v from its
// current lane to the end of the kept prefix is blocked.
func (s *Simulation) keptLaneBlocked(v *vehicle, prefix int) bool {
	current := 0
	if v.blocks.len() > 0 {
		current = v.blocks.locate(v.blockIndex, 0)
	}
	return current < prefix && s.routeBlocked(v.Route[current:prefix])
}
