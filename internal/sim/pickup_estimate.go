package sim

import (
	"cmp"
	"fmt"
	"math"
	"slices"
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
// A hold never names the pod that the trip excludes. finishingPod skips
// that pod, and a hold that names it ends.
func (s *Simulation) waitForFinishingPod(trip *waitingTrip, idle *vehicle, assigned map[string]bool) bool {
	if s.finishingPodWait == FinishingPodWaitNone {
		return false
	}
	if trip.deferUntil != 0 && s.tick >= trip.deferUntil {
		return false
	}
	if trip.deferCheck > s.tick {
		if v := s.findVehicle(trip.deferPodID); v == nil || !v.inService() || trip.excludes(v.Pod.ID) || !s.podFitsRequest(v, trip.request) || !s.pickupAccess(v, trip.request) {
			return false
		}
		trip.request.DispatchReason = finishingPodReason(trip.deferPodID)
		return true
	}
	station, _ := s.station(trip.request.legOrigin())
	route, _, ok := s.pickupRouteWithAssignments(pickupRouteInput{pod: idle, station: trip.request.legOrigin(), assigned: assigned, accept: s.pickupBerthFilter(idle, trip.request)})
	if !ok {
		return false
	}
	idleETA := s.pickupSeconds(idle, route)
	holdSeconds := math.Inf(1)
	if s.finishingPodWait == FinishingPodWaitStrict {
		holdUntil := trip.deferUntil
		if holdUntil == 0 {
			holdUntil = s.tick + maxDispatchDeferral
		}
		holdSeconds = float64(holdUntil-s.tick) / TicksPerSecond
	}
	best := s.finishingPod(trip, idle, station.ID, idleETA-pickupAdvantageSeconds, holdSeconds)
	if best == nil {
		return false
	}
	if trip.deferUntil == 0 {
		trip.deferUntil = s.tick + maxDispatchDeferral
	}
	trip.deferCheck = s.tick + TicksPerSecond
	trip.deferPodID = best.Pod.ID
	trip.request.DispatchReason = finishingPodReason(best.Pod.ID)
	return true
}

// finishingCandidate is a busy pod that finishingPod can hold for. bound
// is a lower bound of its ETA.
type finishingCandidate struct {
	index            int
	node             string
	remaining, bound float64
}

// finishingPod returns the busy pod with the least ETA to the station when
// that ETA is less than limit and not more than holdSeconds. Of pods with
// an equal ETA, it returns the first in fleet order. It returns nil when no
// pod qualifies.
//
// Network validation keeps each lane speed positive, so routeSeconds is not
// negative. Thus the ETA of a busy pod is not less than the time before the
// pod is available, and a pod that cannot win on that time gets no route.
// The bound of a pod adds the immutable travel bound from its finish node.
// That bound can end at any berth and omits acceleration and braking, so it
// does not exceed the ETA. finishingPod computes the routes in order of the
// bound, so an early pod with a small ETA lets it skip more routes. It stops
// at the first pod whose bound cannot improve on the best ETA, because each
// later pod has a bound that is not less. pickupCannotImprove keeps a pod
// whose bound can equal the best ETA, so each pod with the least ETA gets a
// route, and the fleet index decides between them.
func (s *Simulation) finishingPod(trip *waitingTrip, idle *vehicle, stationID string, limit, holdSeconds float64) *vehicle {
	var candidates []finishingCandidate
	fits := classFits{s: s, request: &trip.request}
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if v == idle || !v.inService() || trip.excludes(v.Pod.ID) || !fits.pod(v) {
			continue
		}
		node, remaining, ok := s.passAvailableAfter(i)
		if ok && remaining < limit && remaining <= holdSeconds {
			candidates = append(candidates, finishingCandidate{index: i, node: node, remaining: remaining, bound: remaining})
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	bounds := s.stationPickupBounds(stationID)
	for k := range candidates {
		c := &candidates[k]
		if index, found := s.graph.nodes[c.node]; found && index < len(bounds) {
			c.bound += bounds[index]
		}
	}
	slices.SortFunc(candidates, func(a, b finishingCandidate) int {
		return cmp.Or(cmp.Compare(a.bound, b.bound), cmp.Compare(a.index, b.index))
	})
	var best *vehicle
	bestIndex, bestETA := 0, limit
	for _, c := range candidates {
		if pickupCannotImprove(c.bound, min(bestETA, holdSeconds)) {
			break
		}
		v := &s.vehicles[c.index]
		// The held pod must have access to the pickup.
		if !s.pickupAccess(v, trip.request) {
			continue
		}
		route, _, err := s.stationRouteByLoad(stationRouteInput{class: v.Pod.Class, from: c.node, station: stationID, load: noBerthLoad, accept: s.pickupBerthFilter(v, trip.request)})
		if err != nil {
			continue
		}
		eta := c.remaining + s.routeSeconds(motionEnvelopeFor(v.Pod.Class), route, motionEstimate{})
		if eta > holdSeconds {
			continue
		}
		if eta < bestETA || best != nil && eta == bestETA && c.index < bestIndex {
			best, bestIndex, bestETA = v, c.index, eta
		}
	}
	return best
}

// keepHold keeps a trip on hold for a finishing pod until the next check of
// waitForFinishingPod. Before that check, waitForFinishingPod holds the trip
// for each pickup pod that is away from the pickup station. Thus the choice
// of pickupPod does not change the result, and keepHold does not call
// pickupPod. It sets the dispatch reason that the full pass sets.
//
// keepHold reports false, and dispatch does the full pass, when:
//   - the trip is not on hold until a later check
//   - the pod of the hold is withdrawn or excluded
//   - a pod is idle at the pickup station, because that pod can board at once
func (s *Simulation) keepHold(trip *waitingTrip, pass *dispatchPass) bool {
	if s.finishingPodWait == FinishingPodWaitNone || trip.deferUntil != 0 && s.tick >= trip.deferUntil || trip.deferCheck <= s.tick {
		return false
	}
	if v := s.findVehicle(trip.deferPodID); v == nil || !v.inService() || trip.excludes(v.Pod.ID) || !s.podFitsRequest(v, trip.request) || !s.pickupAccess(v, trip.request) {
		return false
	}
	if s.localPickupForRequest(trip.request, trip.excludedPod, pass) != nil {
		return false
	}
	trip.request.DispatchReason = reasonAvailablePod
	if s.pickupAvailable(trip.request.legOrigin(), trip.excludedPod, pass) {
		trip.request.DispatchReason = finishingPodReason(trip.deferPodID)
	}
	return true
}

// pickupAvailable reports whether pickupPod finds a pod for the station.
// Network validation keeps each route time finite, so pickupPod finds a pod
// when pickupRouteWithAssignments accepts one. pickupRouteWithAssignments
// rejects each pod that pickupCandidate rejects, and it does not change the
// simulation when it does so. Thus pickupAvailable reads only the pickup
// candidates of the pass, in fleet order. A berth load changes only the berth that stationRouteByLoad
// chooses. It does not change whether the station has a berth that the pod
// can reach. Thus pickupAvailable does not compute the berth loads. It
// skips the pod excluded, as pickupPodForRequest does, and a depot pod
// that is not the oldest head that can serve the station.
func (s *Simulation) pickupAvailable(stationID, excluded string, pass *dispatchPass) bool {
	serves := func(v *vehicle) bool {
		if excluded != "" && v.Pod.ID == excluded {
			return false
		}
		_, _, _, ok := s.candidateRouteParts(v, stationID, noBerthLoad)
		return ok
	}
	gate := depotGate{s: s, accept: func(head *vehicle) bool {
		return s.pickupCandidate(head, pass.assigned) && serves(head)
	}}
	for _, v := range s.pickupCandidates(pass) {
		if gate.releasable(v) && serves(v) {
			return true
		}
	}
	return false
}

// noBerthLoad gives each berth a load of zero.
func noBerthLoad(Berth) int { return 0 }

// passAvailableAfter returns availableAfter for the pod at fleet index i.
// During a dispatch pass, it keeps the result until the next reset of the
// pass, so the trips that one pass checks for a finishing pod compute the
// estimate of each pod one time. availableAfter reads the pods, the waiting
// trips and the routes. The pass resets after each change to the pods or
// the waiting trips, and a cached route is equal to a new route. A hold
// writes only the deferral fields and the dispatch reason of its trip, and
// availableAfter reads neither. The Express clear of a trip route before
// the check is on a trip with no pod, and availableAfter reads only the
// trips of the pod that it estimates. Outside a pass, it keeps nothing.
func (s *Simulation) passAvailableAfter(i int) (string, float64, bool) {
	pass := s.pass
	if pass == nil || !pass.active {
		return s.availableAfter(&s.vehicles[i])
	}
	if len(pass.finish) != len(s.vehicles) {
		pass.finish = make([]finishMemo, len(s.vehicles))
	}
	memo := &pass.finish[i]
	if !memo.known {
		memo.node, memo.remaining, memo.ok = s.availableAfter(&s.vehicles[i])
		memo.known = true
	}
	return memo.node, memo.remaining, memo.ok
}

// availableAfter includes every committed leg before a busy pod becomes available.
// It reports false for an idle pod and for a pod that waits at rest.
func (s *Simulation) availableAfter(v *vehicle) (string, float64, bool) {
	if v.Pod.Activity == Idle || (v.Pod.WaitReason != NoWait && v.Pod.Speed < 0.1) {
		return "", 0, false
	}
	return s.finishEstimate(v)
}

// finishEstimate returns the node where a pod that is not idle becomes
// available, and the estimated time in seconds until then. It includes
// every committed leg of the pod. It reports false when it cannot make
// the estimate.
func (s *Simulation) finishEstimate(v *vehicle) (string, float64, bool) {
	if v.Pod.Activity == Unloading {
		station, _ := s.station(v.Pod.StationID)
		berth, _ := station.berth(v.Pod.BerthID)
		return s.laterStops(v, berth.Node, float64(v.phaseTicks)/TicksPerSecond, v.Stops)
	}
	envelope := motionEnvelopeFor(v.Pod.Class)
	seconds := float64(v.phaseTicks)/TicksPerSecond + s.routeSecondsWith(envelope, v.Route, v.routeLengths, motionEstimate{distance: v.distance, speed: v.Pod.Speed})
	if v.RelocatingTo == "" {
		destination := v.destination.Node
		if destination == "" {
			station, ok := s.station(v.destinationStation)
			if !ok {
				return "", 0, false
			}
			suffix, berth, err := s.stationRouteByLoad(stationRouteInput{class: v.Pod.Class, from: station.routeEntry(v.Route, v.destination), station: station.ID, load: noBerthLoad, accept: s.berthFilterForVehicle(v)})
			if err != nil {
				return "", 0, false
			}
			seconds += s.routeSeconds(envelope, suffix, motionEstimate{})
			destination = berth.Node
		}
		seconds += float64(v.stopUnloadingPhase(v.destinationStation)) / TicksPerSecond
		if len(v.Stops) > 1 {
			return s.laterStops(v, destination, seconds, v.Stops[1:])
		}
		return destination, seconds, true
	}
	for _, trip := range s.waiting {
		if trip.request.PodID != v.Pod.ID {
			continue
		}
		destination, _ := s.station(trip.request.To)
		party := trip.request.PartySize
		seconds += float64(boardingPhase(v.Pod.Class, party)+unloadingPhase(v.Pod.Class, party))/TicksPerSecond + s.routeSeconds(envelope, trip.route, motionEstimate{})
		if destination.Banks == nil && !s.graph.classRestrictions {
			return destination.Berths[0].Node, seconds, true
		}
		suffix, berth, err := s.stationRouteByLoad(stationRouteInput{class: v.Pod.Class, from: destination.routeEntry(trip.route, Berth{}), station: destination.ID, load: noBerthLoad})
		if err != nil {
			return "", 0, false
		}
		return berth.Node, seconds + s.routeSeconds(envelope, suffix, motionEstimate{}), true
	}
	return v.destination.Node, seconds, true
}

// laterStops adds the legs to the later stops of a pod to an estimate. The
// pod is at the node after the given seconds. Each leg goes to the first
// berth of the stop and includes the unloading time. laterStops returns the
// node of the last stop and the total seconds. It reports false when a leg
// has no route.
func (s *Simulation) laterStops(v *vehicle, node string, seconds float64, stops []string) (string, float64, bool) {
	class := v.Pod.Class
	envelope := motionEnvelopeFor(class)
	for index, stop := range stops {
		route, err := s.stationApproachForStops(node, stops[index:], class)
		if err != nil {
			return "", 0, false
		}
		station, _ := s.station(stop)
		suffix, berth, err := s.stationRouteByLoad(stationRouteInput{class: class, from: station.routeEntry(route, Berth{}), station: station.ID, load: noBerthLoad, accept: s.berthFilterForStops(class, stops[index+1:])})
		if err != nil {
			return "", 0, false
		}
		seconds += s.routeSeconds(envelope, route, motionEstimate{}) + s.routeSeconds(envelope, suffix, motionEstimate{}) +
			float64(v.stopUnloadingPhase(stop))/TicksPerSecond
		node = berth.Node
	}
	return node, seconds, true
}

type motionEstimate struct{ distance, speed float64 }

// routeSeconds estimates free-flow travel with acceleration and final braking allowances
// of the envelope. Traffic delays are unknown; the dispatch deferral limit bounds forecast error.
func (s *Simulation) routeSeconds(envelope motionEnvelope, route []Lane, motion motionEstimate) float64 {
	return s.routeSecondsWith(envelope, route, nil, motion)
}

// routeSecondsWith returns the same value as routeSeconds. When lengths has
// one entry for each lane of route, it must hold the value of laneLength for
// each lane. Then the function does not look up the lane lengths.
func (s *Simulation) routeSecondsWith(envelope motionEnvelope, route []Lane, lengths []float64, motion motionEstimate) float64 {
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
	return seconds + missingSpeed*missingSpeed/(2*envelope.acceleration*firstSpeed) + lastSpeed/(2*envelope.plannedBraking)
}
