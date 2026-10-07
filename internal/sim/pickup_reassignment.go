package sim

import (
	"math"
	"slices"
)

// Assignment and periodic checks share a budget for each simulated second.
func (c *pickupSwapController) resetBudget(tick int64) {
	second := tick / TicksPerSecond
	if second != c.budgetSecond {
		c.budgetSecond, c.scans, c.routes = second, 0, 0
	}
}

func (c *pickupSwapController) hasBudget() bool {
	return c.scans < pickupSwapScanLimit && c.routes < pickupSwapRouteLimit
}

// reassignPickup checks a new assignment before dispatch uses its pod again.
// The alternative cursor rotates independently of the periodic pair cursor.
func (s *Simulation) reassignPickup(index int) bool {
	c := s.pickupSwaps
	if c == nil || !c.enabled || s.routingPolicy != FreeFlowRouting || len(s.vehicles) < 2 {
		return false
	}
	c.resetBudget(s.tick)
	if !c.hasBudget() {
		return false
	}
	pod := s.waiting[index].request.PodID
	left := slices.IndexFunc(s.vehicles, func(v vehicle) bool { return v.Pod.ID == pod })
	if left < 0 || !s.swapEligible(&s.vehicles[left], &s.waiting[index]) {
		return false
	}
	c.stats.AssignmentChecks++
	assigned := s.swapAssignments()
	for range len(s.vehicles) {
		if !c.hasBudget() {
			return false
		}
		right := c.alternative % len(s.vehicles)
		c.alternative = (right + 1) % len(s.vehicles)
		if right != left && s.checkPickupPair(assigned, left, right) {
			return true
		}
	}
	return false
}

func (s *Simulation) checkPickupPair(assigned map[string]int, left, right int) bool {
	c := s.pickupSwaps
	c.scans++
	c.stats.ScannedPairs++
	a, b := &s.vehicles[left], &s.vehicles[right]
	i, j := assigned[a.Pod.ID], assigned[b.Pod.ID]
	if i == -1 && j >= 0 {
		a, b, i, j = b, a, j, i
	}
	if i < 0 || !s.swapEligible(a, &s.waiting[i]) ||
		j < -1 || j >= 0 && !s.swapEligible(b, &s.waiting[j]) ||
		j == -1 && !s.freePickupAlternative(b) {
		c.stats.IneligiblePairs++
		return false
	}
	if c.cooldown[a.Pod.ID] > s.tick || c.cooldown[b.Pod.ID] > s.tick {
		c.stats.CooldownPairs++
		return false
	}
	if j >= 0 && s.waiting[i].request.legOrigin() == s.waiting[j].request.legOrigin() {
		c.stats.SameOriginPairs++
		return false
	}
	// No swap or transfer gives a trip the pod that it excludes.
	if s.waiting[i].excludes(b.Pod.ID) || j >= 0 && s.waiting[j].excludes(a.Pod.ID) {
		c.stats.IneligiblePairs++
		return false
	}
	c.routes++
	c.stats.RoutePairs++
	if j >= 0 {
		return s.tryPickupSwap(i, j)
	}
	return s.tryPickupTransfer(i, b)
}

func (s *Simulation) freePickupAlternative(v *vehicle) bool {
	if v.linked() || v.RidersAboard() != 0 || !s.pickupCandidate(v, nil) {
		return false
	}
	if v.Pod.Activity == Idle {
		return true
	}
	_, _, ok := s.divertStart(v)
	return ok
}

// tryPickupTransfer prepares the replacement before releasing the old pickup.
// An idle replacement keeps its origin claim until ordinary departure.
func (s *Simulation) tryPickupTransfer(index int, replacement *vehicle) bool {
	trip := &s.waiting[index]
	old := s.findVehicle(trip.request.PodID)
	if !s.podFitsRequest(replacement, trip.request) || !s.pickupAccess(replacement, trip.request) {
		return false
	}
	route, berth, ok := s.candidateRouteForRequest(replacement, trip.request, nil)
	c := s.pickupSwaps
	if !ok || !s.pickupBerthFitsRequest(replacement, trip.request, berth) {
		c.stats.RouteFailures++
		return false
	}
	before, after := s.pickupSeconds(old, old.Route), s.pickupSeconds(replacement, route)
	if !pickupTransferImproves(before, after) {
		c.stats.NoBenefitPairs++
		return false
	}
	if replacement.Pod.Activity == Idle && replacement.Pod.StationID != trip.request.legOrigin() {
		candidate := *replacement
		if err := s.startEmptyMove(&candidate, emptyDestination{station: trip.request.legOrigin(), berth: berth}); err != nil {
			c.stats.RouteFailures++
			return false
		}
		*replacement = candidate
	} else if replacement.Pod.Activity != Idle {
		s.redirectPickupSwap(replacement, redirection{route: route, berth: berth, station: trip.request.legOrigin()})
		replacement.released = false
	}
	s.releasePickup(old)
	s.recordPickupReassignment(trip.request, replacement.Pod.ID, before, after)
	assignPickup(trip, replacement)
	trip.route, trip.destination = nil, Berth{}
	trip.deferUntil, trip.deferCheck, trip.deferPodID = 0, 0, ""
	trip.request.DispatchReason = ""
	c.cooldown[old.Pod.ID], c.cooldown[replacement.Pod.ID] = s.tick+pickupSwapCooldownTicks, s.tick+pickupSwapCooldownTicks
	c.stats.Transfers++
	c.stats.PredictedSecondsSaved += before - after
	return true
}

func pickupTransferImproves(before, after float64) bool {
	return !math.IsNaN(before) && !math.IsNaN(after) && !math.IsInf(before, 0) && !math.IsInf(after, 0) &&
		before >= 0 && after >= 0 && after < before-1e-9 && before-after >= pickupSwapMinimumGain
}

// PickupReassignment records one request's predicted pickup change.
// Future traffic can change the realized pickup time.
type PickupReassignment struct {
	Tick                   int64
	RequestID              int
	OldPod, NewPod         string
	OldSeconds, NewSeconds float64
}

// PickupReassignments returns experiment records since reset or restore.
// It returns an owned copy, or nil when experiment recording is off.
func (s *Simulation) PickupReassignments() []PickupReassignment {
	if !s.recordExperiments || s.pickupSwaps == nil {
		return nil
	}
	return slices.Clone(s.pickupSwaps.records)
}

func (s *Simulation) recordPickupReassignment(request Request, pod string, before, after float64) {
	if s.recordExperiments {
		s.pickupSwaps.records = append(s.pickupSwaps.records, PickupReassignment{
			Tick: s.tick, RequestID: request.ID, OldPod: request.PodID, NewPod: pod,
			OldSeconds: before, NewSeconds: after,
		})
	}
}
