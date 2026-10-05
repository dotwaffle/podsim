package sim

import (
	"math"
	"slices"
)

const (
	pickupSwapScanLimit     = 256
	pickupSwapRouteLimit    = 8
	pickupSwapCooldownTicks = 30 * TicksPerSecond
	pickupSwapMinimumGain   = 10.0
)

// PickupSwapStats reports work and decisions of the experimental controller.
// Estimates exclude future traffic and do not promise a realized wait saving.
type PickupSwapStats struct {
	ScannedPairs, RoutePairs, Swaps                        int
	Transfers, AssignmentChecks                            int
	IneligiblePairs, CooldownPairs, SameOriginPairs        int
	RouteFailures, NoBenefitPairs, UnsupportedPolicyChecks int
	PredictedSecondsSaved                                  float64
}

type pickupSwapController struct {
	enabled       bool
	nextTick      int64
	left, right   int
	alternative   int
	budgetSecond  int64
	scans, routes int
	cooldown      map[string]int64
	stats         PickupSwapStats
	records       []PickupReassignment
}

// SetPickupSwaps enables experimental pickup reassignment. It is off
// by default and runs only with free-flow routing. Reset keeps enablement but
// clears its history. Saved states do not retain the policy or its history.
func (s *Simulation) SetPickupSwaps(enabled bool) {
	if s.pickupSwaps == nil {
		if !enabled {
			return
		}
		s.pickupSwaps = &pickupSwapController{right: 1, cooldown: make(map[string]int64)}
	}
	if enabled && !s.pickupSwaps.enabled {
		s.pickupSwaps.nextTick = s.tick
	}
	s.pickupSwaps.enabled = enabled
}

// PickupSwapStats returns an independent value with cumulative counters since
// the last reset. Disabling the controller keeps these counters available.
func (s *Simulation) PickupSwapStats() PickupSwapStats {
	if s.pickupSwaps == nil {
		return PickupSwapStats{}
	}
	return s.pickupSwaps.stats
}

// swapPickups runs after ordinary dispatch, before new track admission. Its
// pair cursor uses stable fleet indexes and advances even on rejected pairs.
func (s *Simulation) swapPickups() {
	c := s.pickupSwaps
	if c == nil || !c.enabled || s.tick < c.nextTick {
		return
	}
	c.nextTick = s.tick + TicksPerSecond
	if s.routingPolicy != FreeFlowRouting {
		c.stats.UnsupportedPolicyChecks++
		return
	}
	n := len(s.vehicles)
	if n < 2 || len(s.waiting) == 0 {
		return
	}
	c.resetBudget(s.tick)
	if !c.hasBudget() {
		return
	}
	assigned := s.swapAssignments()
	limit := pickupSwapScanLimit
	if n <= pickupSwapScanLimit {
		limit = min(limit, n*(n-1)/2)
	}
	for scanned := 0; scanned < limit && c.hasBudget(); scanned++ {
		left, right := c.nextPair(n)
		if s.checkPickupPair(assigned, left, right) {
			s.parkUnclaimedReleased()
			return
		}
	}
}

// swapAssignments uses -1 for absent and -2 for duplicate bindings. Duplicate
// bindings are never eligible, even if a third occurrence follows the second.
func (s *Simulation) swapAssignments() map[string]int {
	assigned := make(map[string]int, len(s.vehicles))
	for _, v := range s.vehicles {
		assigned[v.Pod.ID] = -1
	}
	for index, trip := range s.waiting {
		if old, exists := assigned[trip.request.PodID]; exists {
			if old != -1 {
				assigned[trip.request.PodID] = -2
			} else {
				assigned[trip.request.PodID] = index
			}
		}
	}
	return assigned
}

func (c *pickupSwapController) nextPair(n int) (int, int) {
	if c.left >= n-1 || c.right >= n || c.right <= c.left {
		c.left, c.right = 0, 1
	}
	left, right := c.left, c.right
	c.right++
	if c.right == n {
		c.left++
		c.right = c.left + 1
		if c.left == n-1 {
			c.left, c.right = 0, 1
		}
	}
	return left, right
}

func (s *Simulation) swapEligible(v *vehicle, trip *waitingTrip) bool {
	if !v.inService() || !releasable(v) || v.released || v.RidersAboard() != 0 ||
		trip.request.Completed || trip.request.PodID != v.Pod.ID ||
		v.RelocatingTo != trip.request.legOrigin() || v.destinationStation != trip.request.legOrigin() {
		return false
	}
	if v.buffered {
		plan, ok := s.bufferPlan(v)
		if !ok || v.reservedThrough >= plan.first {
			return false
		}
	}
	_, _, ok := s.divertStart(v)
	return ok
}

// tryPickupSwap prepares both routes before changing either assignment. The
// estimates compare each request's old pod with that request's replacement.
func (s *Simulation) tryPickupSwap(i, j int) bool {
	a := s.findVehicle(s.waiting[i].request.PodID)
	b := s.findVehicle(s.waiting[j].request.PodID)
	if a == nil || b == nil || !s.podFitsRequest(a, s.waiting[j].request) || !s.podFitsRequest(b, s.waiting[i].request) {
		return false
	}
	// Each receiving pod must have access to its new pickup.
	if !s.pickupAccess(a, s.waiting[j].request) || !s.pickupAccess(b, s.waiting[i].request) {
		return false
	}
	routeA, berthA, okA := s.candidateRouteForRequest(a, s.waiting[j].request, nil)
	routeB, berthB, okB := s.candidateRouteForRequest(b, s.waiting[i].request, nil)
	c := s.pickupSwaps
	if !okA || !okB || !s.pickupBerthFitsRequest(a, s.waiting[j].request, berthA) || !s.pickupBerthFitsRequest(b, s.waiting[i].request, berthB) {
		c.stats.RouteFailures++
		return false
	}
	oldA, oldB := s.assignedPickupSeconds(a), s.assignedPickupSeconds(b)
	newA, newB := s.pickupSeconds(a, routeA), s.pickupSeconds(b, routeB)
	if !pickupSwapImproves(oldA, oldB, newA, newB) {
		c.stats.NoBenefitPairs++
		return false
	}
	s.recordPickupReassignment(s.waiting[i].request, b.Pod.ID, oldA, newB)
	s.recordPickupReassignment(s.waiting[j].request, a.Pod.ID, oldB, newA)
	// Neither redirect can fail. Both prepared routes keep their pod's
	// reserved lanes, and unused destination claims are released normally.
	s.redirectPickupSwap(a, redirection{route: routeA, berth: berthA, station: s.waiting[j].request.legOrigin()})
	s.redirectPickupSwap(b, redirection{route: routeB, berth: berthB, station: s.waiting[i].request.legOrigin()})
	s.bufferPickup(a)
	s.bufferPickup(b)
	assignPickup(&s.waiting[i], b)
	assignPickup(&s.waiting[j], a)
	for _, index := range []int{i, j} {
		trip := &s.waiting[index]
		trip.route, trip.destination = nil, Berth{}
		trip.deferUntil, trip.deferCheck, trip.deferPodID = 0, 0, ""
		trip.request.DispatchReason = ""
	}
	c.cooldown[a.Pod.ID], c.cooldown[b.Pod.ID] = s.tick+pickupSwapCooldownTicks, s.tick+pickupSwapCooldownTicks
	c.stats.Swaps++
	c.stats.PredictedSecondsSaved += oldA + oldB - newA - newB
	return true
}

func pickupSwapImproves(oldA, oldB, newA, newB float64) bool {
	for _, seconds := range []float64{oldA, oldB, newA, newB} {
		if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 0 {
			return false
		}
	}
	return newB < oldA-1e-9 && newA < oldB-1e-9 && oldA+oldB-newA-newB >= pickupSwapMinimumGain
}

func (s *Simulation) redirectPickupSwap(v *vehicle, to redirection) {
	before := *v
	s.redirect(v, to)
	if samePendingReservation(&before, v) {
		v.pending, v.waitSince = before.pending, before.waitSince
	}
}

// samePendingReservation compares the complete next resource group. An equal
// block number alone would transfer aging priority to an unrelated movement.
func samePendingReservation(a, b *vehicle) bool {
	first := a.pending
	if first < 0 || first >= a.blocks.len() || first >= b.blocks.len() {
		return false
	}
	last := reservationEnd(&a.blocks, first)
	if last != reservationEnd(&b.blocks, first) {
		return false
	}
	for index := first; index <= last; index++ {
		old, next := a.blocks.at(index), b.blocks.at(index)
		if old.lane.ID != next.lane.ID || old.start != next.start || old.end != next.end || !slices.Equal(old.resources, next.resources) {
			return false
		}
	}
	return true
}
