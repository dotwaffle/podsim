package sim

import (
	"fmt"
	"math"
)

const (
	maxDispatchDeferral    = 30 * TicksPerSecond
	pickupAdvantageSeconds = 2.0
)

// FinishingPodWait selects when dispatch holds a request for a busy pod that
// will finish soon, instead of sending an available pod that is away from
// the pickup station.
type FinishingPodWait int

const (
	// FinishingPodWaitCurrent holds the request when a busy pod's estimated
	// finish plus its empty travel to the pickup beats the idle pod by
	// pickupAdvantageSeconds. The estimate can be longer than the hold time.
	// The hold ends after maxDispatchDeferral, and then the idle pod goes.
	FinishingPodWaitCurrent FinishingPodWait = iota
	// FinishingPodWaitStrict holds the request only when the busy pod's
	// estimated finish plus its empty travel to the pickup is not more than
	// the hold time that remains from now, and still beats the idle pod by
	// pickupAdvantageSeconds. The hold time is maxDispatchDeferral from the
	// first hold for the request. Dispatch checks the hold again each second
	// with the time that remains. When the forecast moves past the end of the
	// hold, the idle pod goes at that check.
	FinishingPodWaitStrict
	// FinishingPodWaitNone never holds a request. Dispatch sends the idle pod
	// at once.
	FinishingPodWaitNone
)

// SetFinishingPodWait selects the rule that decides when dispatch holds a
// request for a busy pod that will finish soon. The default is
// FinishingPodWaitCurrent. Reset keeps the rule. The saved state does not
// keep it, so RestoreState returns a simulation with the default rule. The
// rule applies from the next dispatch pass.
func (s *Simulation) SetFinishingPodWait(rule FinishingPodWait) error {
	if rule < FinishingPodWaitCurrent || rule > FinishingPodWaitNone {
		return fmt.Errorf("unknown finishing pod wait rule %d", rule)
	}
	s.finishingPodWait = rule
	return nil
}

// waitForFinishingPod is advisory. It never assigns a busy pod or reserves a berth.
// s.finishingPodWait selects when it holds the trip.
//
// Network validation keeps each lane speed positive, so emptySeconds is not
// negative. Thus the ETA of a busy pod is not less than the time before the
// pod is available. When that time cannot win, the loop does not compute
// the empty route. But while the congestion costs are due for a refresh,
// the loop computes each empty route, because the first route query
// refreshes the costs.
func (s *Simulation) waitForFinishingPod(trip *waitingTrip, idle *vehicle, assigned map[string]bool) bool {
	if s.finishingPodWait == FinishingPodWaitNone {
		return false
	}
	if trip.deferUntil != 0 && s.tick >= trip.deferUntil {
		return false
	}
	if trip.deferCheck > s.tick {
		trip.request.DispatchReason = "Waiting for pod " + trip.deferPodID + " to finish"
		return true
	}
	station, _ := s.station(trip.request.From)
	route, _, ok := s.pickupRouteWithAssignments(pickupRouteInput{pod: idle, station: trip.request.From, assigned: assigned})
	if !ok {
		return false
	}
	idleETA := s.pickupSeconds(idle, route)
	bestETA := idleETA - pickupAdvantageSeconds
	holdSeconds := math.Inf(1)
	if s.finishingPodWait == FinishingPodWaitStrict {
		holdUntil := trip.deferUntil
		if holdUntil == 0 {
			holdUntil = s.tick + maxDispatchDeferral
		}
		holdSeconds = float64(holdUntil-s.tick) / TicksPerSecond
	}
	var best *vehicle
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if v == idle {
			continue
		}
		node, remaining, ok := s.availableAfter(v)
		if !ok {
			continue
		}
		if canWin := remaining < bestETA && remaining <= holdSeconds; !canWin && !s.congestionRefreshDue() {
			continue
		}
		eta := remaining + s.emptySeconds(node, station.Berths[0].Node)
		if eta < bestETA && eta <= holdSeconds {
			best, bestETA = v, eta
		}
	}
	if best == nil {
		return false
	}
	if trip.deferUntil == 0 {
		trip.deferUntil = s.tick + maxDispatchDeferral
	}
	trip.deferCheck = s.tick + TicksPerSecond
	trip.deferPodID = best.Pod.ID
	trip.request.DispatchReason = "Waiting for pod " + best.Pod.ID + " to finish"
	return true
}

// keepHold keeps a trip on hold for a finishing pod until the next check of
// waitForFinishingPod. Before that check, waitForFinishingPod holds the trip
// for each pickup pod that is away from the pickup station. Thus the choice
// of pickupPod does not change the result, and keepHold does not call
// pickupPod. It sets the dispatch reason that the full pass sets.
//
// keepHold reports false, and dispatch does the full pass, when:
//   - the trip is not on hold until a later check
//   - a pod is idle at the pickup station, because that pod can board at once
//   - the congestion costs are due for a refresh, because the first route
//     query refreshes them, and pickupPod can make that query
func (s *Simulation) keepHold(trip *waitingTrip, assigned map[string]bool) bool {
	if s.finishingPodWait == FinishingPodWaitNone || trip.deferUntil != 0 && s.tick >= trip.deferUntil || trip.deferCheck <= s.tick {
		return false
	}
	if s.localPickup(trip.request.From, assigned) != nil || s.congestionRefreshDue() {
		return false
	}
	trip.request.DispatchReason = "Waiting for an available pod"
	if s.pickupAvailable(trip.request.From, assigned) {
		trip.request.DispatchReason = "Waiting for pod " + trip.deferPodID + " to finish"
	}
	return true
}

// pickupAvailable reports whether pickupPod finds a pod for the station.
// Network validation keeps each route time finite, so pickupPod finds a pod
// when pickupRouteWithAssignments accepts one.
func (s *Simulation) pickupAvailable(stationID string, assigned map[string]bool) bool {
	load := s.berthLoads()
	for i := range s.vehicles {
		if _, _, ok := s.pickupRouteWithAssignments(pickupRouteInput{pod: &s.vehicles[i], station: stationID, assigned: assigned, load: load}); ok {
			return true
		}
	}
	return false
}

// availableAfter includes every committed leg before a busy pod becomes available.
func (s *Simulation) availableAfter(v *vehicle) (string, float64, bool) {
	if v.Pod.Activity == Idle || (v.Pod.WaitReason != NoWait && v.Pod.Speed < 0.1) {
		return "", 0, false
	}
	if v.Pod.Activity == Unloading {
		station, _ := s.station(v.Pod.StationID)
		berth, _ := station.berth(v.Pod.BerthID)
		return berth.Node, float64(v.phaseTicks) / TicksPerSecond, true
	}
	seconds := float64(v.phaseTicks)/TicksPerSecond + s.routeSecondsWith(v.Route, v.routeLengths, motionEstimate{distance: v.distance, speed: v.Pod.Speed})
	if v.RelocatingTo == "" {
		destination := v.destination.Node
		if destination == "" {
			station, ok := s.station(v.destinationStation)
			if !ok {
				return "", 0, false
			}
			berth := station.Berths[0]
			suffix, err := s.stationPath(station.Entry, berth.Node)
			if err != nil {
				return "", 0, false
			}
			seconds += s.routeSeconds(suffix, motionEstimate{})
			destination = berth.Node
		}
		return destination, seconds + float64(unloadingTicks)/TicksPerSecond, true
	}
	for _, trip := range s.waiting {
		if trip.request.PodID != v.Pod.ID {
			continue
		}
		destination, _ := s.station(trip.request.To)
		seconds += float64(boardingTicks+unloadingTicks)/TicksPerSecond + s.routeSeconds(trip.route, motionEstimate{})
		return destination.Berths[0].Node, seconds, true
	}
	return v.destination.Node, seconds, true
}

func (s *Simulation) emptySeconds(from, to string) float64 {
	result := s.cachedRoute(from, to)
	if result.err != nil {
		return math.Inf(1)
	}
	if !result.timed {
		return s.routeSeconds(result.lanes, motionEstimate{})
	}
	return result.seconds
}

type motionEstimate struct{ distance, speed float64 }

// routeSeconds estimates free-flow travel with acceleration and final braking allowances.
// Traffic delays are unknown; the dispatch deferral limit bounds forecast error.
func (s *Simulation) routeSeconds(route []Lane, motion motionEstimate) float64 {
	return s.routeSecondsWith(route, nil, motion)
}

// routeSecondsWith returns the same value as routeSeconds. When lengths has
// one entry for each lane of route, it must hold the value of laneLength for
// each lane. Then the function does not look up the lane lengths.
func (s *Simulation) routeSecondsWith(route []Lane, lengths []float64, motion motionEstimate) float64 {
	seconds, firstSpeed, lastSpeed := 0.0, 0.0, 0.0
	for i := range route {
		lane := &route[i]
		var length float64
		if len(lengths) == len(route) {
			length = lengths[i]
		} else {
			length = s.laneLength(*lane)
		}
		if motion.distance >= length {
			motion.distance -= length
			continue
		}
		seconds += (length - motion.distance) / lane.SpeedLimit
		motion.distance = 0
		if firstSpeed == 0 {
			firstSpeed = lane.SpeedLimit
		}
		lastSpeed = lane.SpeedLimit
	}
	if firstSpeed == 0 {
		return 0
	}
	missingSpeed := math.Max(0, firstSpeed-motion.speed)
	return seconds + missingSpeed*missingSpeed/(2*acceleration*firstSpeed) + lastSpeed/(2*acceleration)
}
