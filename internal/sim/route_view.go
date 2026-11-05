package sim

// routeView is the routing view of one query. The endpoint route and the
// pickup access of the incident suspension contract (sections 9.3, 9.4 and
// 11) read it, and so does the station choice of the incident emergency
// contract (section 9.1). While s.routeView is set, a route search reads the
// free-flow memo and does not write it.
//
// The view is a function of the state at the start of the query. It
// builds each part on its first use. The query writes nothing before it
// installs a route, so a part built later is equal to a part built at the
// start. The view is nil outside a query, so no save or clone holds it.
type routeView struct {
	// self is the pod of the query. Its own queue does not delay it.
	self *vehicle
	// discharge holds queueDischarge(self) when dischargeSet is true.
	discharge    []float64
	dischargeSet bool
	// access is true for the view of pickupAccess, whose free-flow searches
	// read and write accessRoutes. See accessRoute.
	access bool
}

// enterRouteView sets a routing view for pod v, and returns the previous
// view for leaveRouteView. A nested query for the same pod keeps the view
// of the outer query.
func (s *Simulation) enterRouteView(v *vehicle) *routeView {
	previous := s.routeView
	if previous == nil || previous.self != v {
		s.routeView = &routeView{self: v}
	}
	return previous
}

// leaveRouteView restores the view that enterRouteView returned.
func (s *Simulation) leaveRouteView(previous *routeView) {
	s.routeView = previous
}

// routeDischarge returns queueDischarge(v). The routing view keeps the
// value of its pod.
func (s *Simulation) routeDischarge(v *vehicle) []float64 {
	view := s.routeView
	if view == nil || view.self != v {
		return s.queueDischarge(v)
	}
	if !view.dischargeSet {
		view.discharge, view.dischargeSet = s.queueDischarge(v), true
	}
	return view.discharge
}
