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
	seconds := float64(v.phaseTicks)/TicksPerSecond + s.routeSeconds(v.Route, motionEstimate{distance: v.distance, speed: v.Pod.Speed})
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
	route, err := s.route(from, to)
	if err != nil {
		return math.Inf(1)
	}
	return s.routeSeconds(route, motionEstimate{})
}

type motionEstimate struct{ distance, speed float64 }

// routeSeconds estimates free-flow travel with acceleration and final braking allowances.
// Traffic delays are unknown; the dispatch deferral limit bounds forecast error.
func (s *Simulation) routeSeconds(route []Lane, motion motionEstimate) float64 {
	seconds, firstSpeed, lastSpeed := 0.0, 0.0, 0.0
	for _, lane := range route {
		length := s.laneLength(lane)
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
