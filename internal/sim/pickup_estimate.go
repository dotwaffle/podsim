package sim

import "math"

const (
	maxDispatchDeferral    = 30 * TicksPerSecond
	pickupAdvantageSeconds = 2.0
)

// waitForFinishingPod is advisory. It never assigns a busy pod or reserves a berth.
func (s *Simulation) waitForFinishingPod(trip *waitingTrip, idle *vehicle) bool {
	if trip.deferUntil != 0 && s.tick >= trip.deferUntil {
		return false
	}
	station, _ := s.network.Station(trip.request.From)
	route, ok := s.pickupRoute(idle, trip.request.From)
	if !ok {
		return false
	}
	idleETA := s.pickupSeconds(idle, route)
	bestETA := idleETA - pickupAdvantageSeconds
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
		if eta < bestETA {
			best, bestETA = v, eta
		}
	}
	if best == nil {
		return false
	}
	if trip.deferUntil == 0 {
		trip.deferUntil = s.tick + maxDispatchDeferral
	}
	trip.request.DispatchReason = "Waiting for pod " + best.Pod.ID + " to finish"
	return true
}

// availableAfter includes every committed leg before a busy pod becomes available.
func (s *Simulation) availableAfter(v *vehicle) (string, float64, bool) {
	if v.Pod.Activity == Idle || (v.Pod.WaitReason != NoWait && v.Pod.Speed < 0.1) {
		return "", 0, false
	}
	if v.Pod.Activity == Unloading {
		station, _ := s.network.Station(v.Pod.StationID)
		berth, _ := station.berth(v.Pod.BerthID)
		return berth.Node, float64(v.phaseTicks) / TicksPerSecond, true
	}
	seconds := float64(v.phaseTicks)/TicksPerSecond + s.routeSeconds(v.Route, motionEstimate{distance: v.distance, speed: v.Pod.Speed})
	if v.RelocatingTo == "" {
		return v.destination.Node, seconds + float64(unloadingTicks)/TicksPerSecond, true
	}
	for _, trip := range s.waiting {
		if trip.request.PodID != v.Pod.ID {
			continue
		}
		destination, _ := s.network.Station(trip.request.To)
		seconds += float64(boardingTicks+unloadingTicks)/TicksPerSecond + s.routeSeconds(trip.route, motionEstimate{})
		return destination.Berths[0].Node, seconds, true
	}
	return v.destination.Node, seconds, true
}

func (s *Simulation) emptySeconds(from, to string) float64 {
	route, err := s.network.Route(from, to)
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
		length := s.network.Length(lane)
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
