package sim

import (
	"fmt"
	"math"
)

// RoutingPolicy selects the route that a pod gets when it starts a route.
// Estimates and the choices of dispatch, positioning and parking use
// free-flow costs with each policy. Existing routes and movement
// arbitration do not change.
type RoutingPolicy int

const (
	// FreeFlowRouting gives the route with the lowest free-flow travel time.
	FreeFlowRouting RoutingPolicy = iota
	// CongestionRouting adds a cost for each owned track cell and each
	// stopped pod on a lane. It keeps the costs for
	// congestionRouteRefreshTicks.
	CongestionRouting
	// QueueRouting adds the time that each queue of stopped pods needs to
	// clear after the pod gets to the queue. See queueRoute.
	QueueRouting
)

const (
	// queueHeadwaySeconds is the time that each stopped pod in a queue adds
	// to the time that the queue needs to clear.
	queueHeadwaySeconds = 3.0
	// queueStoppedSpeed is the speed in m/s below which a waiting pod is in
	// a queue.
	queueStoppedSpeed = 0.1
	// queueMinimumSavingSeconds and queueMinimumSavingShare give the
	// smallest saving for which queueRoute leaves the free-flow route. The
	// saving must be at least the larger of the two values.
	queueMinimumSavingSeconds = 15.0
	queueMinimumSavingShare   = 0.05
	// queueDetourLimit is the largest free-flow time of a queue route, as
	// a multiple of the free-flow time of the free-flow route.
	queueDetourLimit = 1.2
)

// SetRoutingPolicy selects the routing policy for the routes that pods
// start after the call.
func (s *Simulation) SetRoutingPolicy(policy RoutingPolicy) error {
	if policy < FreeFlowRouting || policy > QueueRouting {
		return fmt.Errorf("unknown routing policy %d", policy)
	}
	s.routingPolicy = policy
	s.nextCongestionRouteRefresh = 0
	s.congestionRouteCosts = nil
	s.congestionRoutes = nil
	return nil
}

// SetCongestionRouting selects CongestionRouting when enabled is true, and
// FreeFlowRouting when it is false.
func (s *Simulation) SetCongestionRouting(enabled bool) {
	policy := FreeFlowRouting
	if enabled {
		policy = CongestionRouting
	}
	_ = s.SetRoutingPolicy(policy)
}

// queueRoute returns the route of QueueRouting for pod v from node from to
// node to. Pod v can be nil.
//
// Each lane with stopped pods has a queue that needs queueHeadwaySeconds
// for each pod to clear. The search adds the part of this time that
// remains when the pod gets to the start of the lane, with free-flow travel
// times from from. Thus a queue that clears before the pod gets to it costs
// nothing. The time at the end of a lane does not decrease when the time at
// its start increases, so the search finds the route with the lowest cost.
// The counts come from the current pods at each call, so the policy keeps
// no state. Pod v does not count in a queue.
//
// The route does not go through the berths of a third station. queueRoute
// returns the free-flow route when:
//   - no route avoids the berths of each third station
//   - no queue delays the free-flow route
//   - the queue route saves less than the larger of
//     queueMinimumSavingSeconds and queueMinimumSavingShare of the cost of
//     the free-flow route
//   - the free-flow time of the queue route is more than queueDetourLimit
//     times the free-flow time of the free-flow route
func (s *Simulation) queueRoute(v *vehicle, from, to string) ([]Lane, error) {
	free, err := s.route(from, to)
	if err != nil || len(free) == 0 {
		return free, err
	}
	discharge := s.queueDischarge(v)
	if discharge == nil {
		return free, nil
	}
	freeSeconds, freeCost, delayed := s.queueCost(free, discharge)
	if !delayed {
		return free, nil
	}
	// The free-flow route exists, so the search can fail only when each
	// route goes through an intermediate berth.
	queued, err := s.searchRoute(networkRouteInput{from: from, to: to, discharge: discharge, terminalBerthsOnly: true})
	if err == nil {
		seconds, cost, _ := s.queueCost(queued, discharge)
		saving := freeCost - cost
		if saving >= math.Max(queueMinimumSavingSeconds, queueMinimumSavingShare*freeCost) && seconds <= queueDetourLimit*freeSeconds {
			return queued, nil
		}
	}
	return free, nil
}

// queueDischarge returns the time in seconds that the queue on each lane
// needs to clear, by lane index. A pod is in a queue when it travels with a
// wait reason and a speed below queueStoppedSpeed. Pod v does not count. It
// returns nil when no lane has a queue.
func (s *Simulation) queueDischarge(v *vehicle) []float64 {
	s.ensureNetworkIndexes()
	var discharge []float64
	for index := range s.vehicles {
		other := &s.vehicles[index]
		pod := &other.Pod
		if other == v || pod.Activity != Traveling || pod.WaitReason == NoWait || pod.Speed >= queueStoppedSpeed {
			continue
		}
		lane, ok := s.graph.lanes[pod.LaneID]
		if !ok {
			continue
		}
		if discharge == nil {
			discharge = make([]float64, len(s.network.Lanes))
		}
		discharge[lane] += queueHeadwaySeconds
	}
	return discharge
}

// queueCost returns the free-flow time of the route, and its cost with the
// queue delays of discharge as the route search computes it. delayed
// reports whether a queue adds to the cost.
func (s *Simulation) queueCost(route []Lane, discharge []float64) (seconds, cost float64, delayed bool) {
	for _, lane := range route {
		index := s.graph.lanes[lane.ID]
		edge := s.graph.edges[index]
		delay := queueDelay(discharge[index], cost)
		delayed = delayed || delay > 0
		seconds += edge.seconds
		cost += edge.seconds + delay
	}
	return seconds, cost, delayed
}

// queueDelay returns the part of the discharge time of a queue that remains
// when a pod gets to the start of its lane at time at.
func queueDelay(discharge, at float64) float64 {
	return math.Max(0, discharge-at)
}
