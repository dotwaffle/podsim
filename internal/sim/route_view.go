package sim

import "math"

// routeView is the routing view of one query. The endpoint route and the
// pickup access of the incident suspension contract (sections 9.3, 9.4 and
// 11) read it, and so does the station choice of the incident emergency
// contract (section 9.1). While s.routeView is set, assignedRoute and its
// policy routes read the view. They write no routing-policy state and no
// route memo:
//
//   - Congestion routing uses congestionRouteCosts as they are, also when
//     the refresh is due. Without stored costs, the view holds the costs of
//     congestionCosts and does not store them. It reads the congestion memo
//     only with the stored costs.
//   - Queue routing uses queueDischarge, which writes nothing.
//   - Predictive routing uses the raw sample without the pod, and the
//     history of predictiveQueues without the history of the pod. With no
//     history, it uses zero history. The history is not decayed to the
//     current tick.
//   - A route search reads the free-flow memo and does not write it.
//
// The view is a function of the state at the start of the query. It
// builds each part on its first use. The query writes nothing before it
// installs a route, so a part built later is equal to a part built at the
// start. The view is nil outside a query, so no save or clone holds it.
type routeView struct {
	// self is the pod of the query. Its own queue does not delay it.
	self *vehicle
	// costs holds the congestion costs. stored reports that they are
	// congestionRouteCosts, so that the congestion memo applies.
	costs  []float64
	stored bool
	// discharge holds queueDischarge(self) when dischargeSet is true.
	discharge    []float64
	dischargeSet bool
	// forecasts holds the predictive forecasts of self.
	forecasts []laneForecast
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

// viewCongestionRoute is congestionRouteForClass under the routing view.
func (s *Simulation) viewCongestionRoute(from, to string, class VehicleClass) routeResult {
	view := s.routeView
	if view.costs == nil {
		view.costs, view.stored = s.congestionRouteCosts, true
		if view.costs == nil {
			view.costs, view.stored = s.congestionCosts(), false
		}
	}
	if view.stored {
		if cached, ok := s.congestionRoutes[routeKey{from: from, to: to, class: routeClass(class)}]; ok {
			return cached
		}
	}
	lanes, err := s.searchRoute(networkRouteInput{from: from, to: to, class: class, extraCost: view.costs, terminalBerthsOnly: true})
	if err != nil {
		lanes, err = s.routeForClass(from, to, class)
	}
	return s.withSeconds(routeResult{lanes: lanes, err: err})
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

// viewForecasts returns the forecasts of routeForecasts(v) under the
// routing view. It writes no predictive state. The view keeps the
// forecasts of its pod.
func (s *Simulation) viewForecasts(v *vehicle) []laneForecast {
	view := s.routeView
	if view.self == v && view.forecasts != nil {
		return view.forecasts
	}
	s.ensureNetworkIndexes()
	raw := make([]float64, len(s.network.Lanes))
	s.searchCounters.forecasts++
	for index := range s.vehicles {
		other := &s.vehicles[index]
		if stoppedForPrediction(other) {
			if lane, ok := s.graph.lanes[other.Pod.LaneID]; ok {
				raw[lane] += queueHeadwaySeconds
			}
		}
	}
	// The pod does not wait for its own queue, by the rule of
	// routeForecasts.
	if stoppedForPrediction(v) {
		if index, ok := s.graph.lanes[v.Pod.LaneID]; ok {
			raw[index] = math.Max(0, raw[index]-queueHeadwaySeconds)
		}
	}
	history := make([]float64, len(raw))
	if s.predictivePodQueues != nil && len(s.predictiveQueues) == len(raw) {
		own := s.predictivePodQueues[v.Pod.ID].lanes
		for index := range history {
			history[index] = math.Max(0, s.predictiveQueues[index]-own[index])
		}
	}
	forecasts := make([]laneForecast, len(raw))
	for index := range forecasts {
		forecasts[index].initial = math.Max(raw[index], history[index])
	}
	for index := range s.vehicles {
		if other := &s.vehicles[index]; other != v {
			s.addPlannedArrivals(forecasts, other)
		}
	}
	for index := range forecasts {
		forecasts[index].finish()
	}
	if view.self == v {
		view.forecasts = forecasts
	}
	return forecasts
}
