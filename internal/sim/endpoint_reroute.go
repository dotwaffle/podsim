package sim

import "slices"

// rerouteIntervalTicks is the cadence of the reroute pass while the
// blocked set is not empty (product choice P2 of the incident suspension
// contract). It is the cadence of refreshCongestionCosts.
const rerouteIntervalTicks = 300

// reroutePass runs the reroute pass of the fault stage (section 9.2 of the
// incident suspension contract) when a new epoch asked for it, and every
// rerouteIntervalTicks while the blocked set is not empty. The cadence
// retries the pods that the last pass skipped. The pass visits the pods in
// pod ID order, which is the fleet order. It gives each healthy pod whose
// remaining route has a blocked lane one route search to the endpoint of
// its route. A pod in a group waits, and a later stage adds its reroute.
func (s *Simulation) reroutePass() {
	if !s.rerouteDue && (!s.blockedActive() || s.tick%rerouteIntervalTicks != 0) {
		return
	}
	s.rerouteDue = false
	for index := range s.vehicles {
		if v := &s.vehicles[index]; s.rerouteCandidate(v) {
			s.rerouteToEndpoint(v)
		}
	}
}

// rerouteCandidate reports whether the reroute pass visits v. The pod is
// healthy and in no group. It travels or departs empty, or it boards or
// continues at a berth with no grant. Its remaining route has a blocked
// lane.
func (s *Simulation) rerouteCandidate(v *vehicle) bool {
	if v.faulted || s.operationalMember(v) != nil {
		return false
	}
	switch v.Pod.Activity {
	case Traveling, DepartingEmpty:
	case Boarding, Continuing:
		if v.Pod.BerthID == "" || v.reservedThrough >= 0 {
			return false
		}
	default:
		return false
	}
	return s.remainingRouteBlocked(v)
}

// endpointRoute returns a forward route for v to the endpoint of its
// current route that avoids the blocked set (section 9.3 of the incident
// suspension contract). The route starts with the lanes that v must keep.
// The endpoint is the destination berth, or the station entry when v has
// no berth yet, so the endpoint kind, the berth and the station stay. It
// reports false when the pod cannot divert, when a kept lane is blocked,
// when no route avoids the blocked set, or when the route takes a rider
// over the detour limit. It searches under the routing view of v, so it
// writes nothing, also no routing-policy state and no route memo.
func (s *Simulation) endpointRoute(v *vehicle) ([]Lane, bool) {
	if len(v.Route) == 0 {
		return nil, false
	}
	defer s.leaveRouteView(s.enterRouteView(v))
	var prefix int
	var from string
	switch v.Pod.Activity {
	case Traveling, DepartingEmpty:
		var ok bool
		if prefix, from, ok = s.divertStart(v); !ok {
			return nil, false
		}
	case Boarding, Continuing:
		// A pod at a berth with no grant starts at its berth, as
		// divertStart gives for a pod with no grant.
		if v.Pod.BerthID == "" || v.reservedThrough >= 0 {
			return nil, false
		}
		prefix, from = 0, v.origin.Node
	default:
		return nil, false
	}
	// A blocked kept lane traps the pod. Reverse motion is a later stage.
	if s.keptLaneBlocked(v, prefix) {
		return nil, false
	}
	// The lane into a blocked berth holds the berth node in its last
	// cell, so it is blocked, and no route ends at a blocked berth.
	target := v.Route[len(v.Route)-1].To
	berthEnd := v.destination.ID != "" && target == v.destination.Node
	suffix, err := s.assignedRoute(v, from, target)
	if err != nil {
		return nil, false
	}
	route := append(slices.Clone(v.Route[:prefix]), suffix...)
	switch {
	case len(route) == 0 || route[len(route)-1].To != target:
		return nil, false
	case s.routeBlocked(suffix):
		// With the kept-lane test, no lane from the current lane on is
		// blocked.
		return nil, false
	case !s.endpointKeepsDetours(v, route, berthEnd):
		return nil, false
	}
	return route, true
}

// endpointKeepsDetours reports whether route keeps each rider of v within
// the detour limit, as rerouteKeepsDetours does. A berth end tests the
// destination berth. An entry end tests the entry of the new route with no
// berth, as legRoute does, so a banked station checks the entry that the
// route reaches. An emergency unload passes: every rider leaves at the
// emergency station, so no rider has an onward stop (section 9.2 of the
// incident emergency contract).
func (s *Simulation) endpointKeepsDetours(v *vehicle, route []Lane, berthEnd bool) bool {
	if v.op.purpose == opEmergencyUnload {
		return true
	}
	if berthEnd {
		return s.rerouteKeepsDetours(v, route, v.destination)
	}
	if (!s.cappedDetours() && len(v.Boardings) == 0) || v.RidersAboard() == 0 {
		return true
	}
	station, _ := s.station(v.destinationStation)
	start := detourStart{class: v.Pod.Class, from: v.origin.Node, ridden: v.riddenBase + s.lanesMeters(route), entry: station.routeEntry(route, Berth{})}
	return s.keepsRiderDetours(v, v.Stops, start)
}

// rerouteToEndpoint installs the route of endpointRoute and reports true.
// It changes only the route and the indexes derived from it. The pod keeps
// its activity, its phase, its grants, its distance, its retained
// resources and owners, its riders and stops, its destination, its
// relocation, its purpose, and its pickups. The route keeps the lanes up
// to the grants, so the blocks of the grants and their owners stay valid.
// It does not use redirect, which would make an occupied pod an empty
// move. The search and the installation read one routing view, so the
// installed route is the route of the search. When endpointRoute fails,
// it changes nothing and reports false.
func (s *Simulation) rerouteToEndpoint(v *vehicle) bool {
	defer s.leaveRouteView(s.enterRouteView(v))
	route, ok := s.endpointRoute(v)
	if !ok {
		return false
	}
	s.setVehicleRoute(v, route)
	v.pending = -1
	countFault(&s.faultCounters.reroutes)
	return true
}
