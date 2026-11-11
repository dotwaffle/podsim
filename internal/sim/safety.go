package sim

import (
	"cmp"
	"fmt"
	"math"
	"slices"
)

// separationTolerance is the distance in meters by which a gap can be less
// than Clearance before Check reports a fault.
const separationTolerance = 1e-6

// SeparationError reports two pods on one plane that are closer than
// their validated separation requirement.
type SeparationError struct {
	Tick          int64
	First, Second string
	// Gap is the distance in meters between the two pods.
	Gap float64
}

// Error names the tick, the two pods and the gap between them.
func (e *SeparationError) Error() string {
	return fmt.Sprintf("tick %d: pods %s and %s are %.5f meters apart", e.Tick, e.First, e.Second, e.Gap)
}

// Check verifies finite positions, a finite and non-negative speed, pod
// separation and berth use. It returns the smallest gap in meters between two
// pods on one plane. The gap is +Inf when no two pods share a plane. When two
// pods are too close, the error is a *SeparationError. Check does not apply a
// speed limit, because each lane has its own limit.
func (o SafetyObservation) Check() (float64, error) {
	for _, pod := range o.Pods {
		if !finite(pod.Position.X) || !finite(pod.Position.Y) || !finite(pod.Speed) || pod.Speed < 0 {
			return 0, fmt.Errorf("invalid pod at tick %d: %+v", o.Tick, pod)
		}
		if largeVehicleClass(pod.Class) {
			if err := ValidateVehicleClassProfileWithOrderContract(pod.Class, o.OrderContract); err != nil {
				return 0, fmt.Errorf("invalid pod at tick %d: %w", o.Tick, err)
			}
		}
	}
	gap, err := o.checkSeparation()
	if err != nil {
		return 0, err
	}
	if err := o.checkBerths(); err != nil {
		return 0, err
	}
	return gap, nil
}

// checkSeparation returns the smallest gap between two pods on one plane.
// When pairs are too close, it reports the first pair (i, j) with i < j in
// the order of o.Pods.
//
// It sorts the pods by X and sweeps them (see sweep). The first sweep has a
// ceiling of reach, the largest squared minimum of any pair, so it skips
// each pair whose squared gap is at least reach. If that sweep counts no
// pair, a second sweep with no ceiling finds the smallest gap. The result
// is the same as a comparison of all pairs:
//   - Check calls checkSeparation only with finite positions. Rounded
//     subtraction and multiplication are monotone, so the squared X distance
//     does not decrease along the sorted order, and the smallest gap so far
//     does not increase. After the first skipped pod, all later pods are
//     skipped too.
//   - dy*dy is not negative and rounding is monotone, so the squared gap of a
//     pair is at least its squared X distance, also when the compiler fuses
//     the multiply and the add.
//   - A skipped pair has a squared gap of at least reach, so it is not a
//     violation, because each pair compares with a threshold of at most
//     reach. Each sweep thus finds all violating pairs.
//   - The first sweep tests each pair whose squared gap is less than reach,
//     and no other pair. If it counts a pair, the smallest gap is less than
//     reach, so the pairs that it skips cannot change the smallest gap.
//   - The second sweep skips a pair only when its squared gap is also at
//     least the smallest squared gap so far, so the pair cannot change the
//     smallest gap.
//   - Each pair test uses the pods in their order in o.Pods, and the result
//     of a pair does not depend on the other pairs. The first violating pair
//     in the order of o.Pods is the smallest violating (i, j).
//   - (a-b)*(a-b) and (b-a)*(b-a) are equal, so the order of the
//     subtraction does not change the gap.
func (o SafetyObservation) checkSeparation() (float64, error) {
	index := o.separationIndex()
	order := make([]int, len(o.Pods))
	for i := range order {
		order[i] = i
	}
	slices.SortFunc(order, func(a, b int) int { return cmp.Compare(o.Pods[a].Position.X, o.Pods[b].Position.X) })
	result := index.sweep(o.Pods, order, index.reach)
	if math.IsInf(result.smallestSquared, 1) {
		result = index.sweep(o.Pods, order, math.Inf(1))
	}
	if result.first >= 0 {
		return 0, &SeparationError{Tick: o.Tick, First: o.Pods[result.first].ID, Second: o.Pods[result.second].ID, Gap: math.Sqrt(result.violationSquared)}
	}
	return math.Sqrt(result.smallestSquared), nil
}

// separationIndex holds the per-pod data of checkSeparation by the index of
// the pod in o.Pods, so a pair test compares integers and not string keys.
type separationIndex struct {
	locations []SafetyLocation
	// envelopes holds the envelope locations of each pod whose envelope is
	// bound to its value. It is nil when the observation has no envelopes.
	// Empty locations never prove a separation, so a pod without an
	// envelope has none.
	envelopes [][]SafetyLocation
	// chains holds the window positions of each small pod in a window. It
	// is nil when the observation has no positions.
	chains []safetyChain
	// keys holds the separation key of each pod whose key is bound to its
	// value, certified the certified pairs, and lateral the lateral pairs.
	// keys is nil when the observation has no keys.
	keys      []separationKey
	certified certifiedPairs
	lateral   *depotLateral
	// reach is the largest squared separation threshold of any pair.
	reach float64
}

func (o SafetyObservation) separationIndex() separationIndex {
	index := separationIndex{
		locations: make([]SafetyLocation, len(o.Pods)),
		reach:     separationThresholdSquared(Clearance),
	}
	if len(o.envelopes) > 0 {
		index.envelopes = make([][]SafetyLocation, len(o.Pods))
	}
	if len(o.chains) > 0 {
		index.chains = make([]safetyChain, len(o.Pods))
	}
	if len(o.keys) > 0 {
		index.keys, index.certified, index.lateral = make([]separationKey, len(o.Pods)), o.certified, o.lateral
	}
	for i, pod := range o.Pods {
		index.locations[i] = o.Locations[pod.ID]
		if envelope, ok := o.envelopes[pod.ID]; ok && envelope.pod == pod {
			index.envelopes[i] = envelope.locations
		}
		if chain, ok := o.chains[pod.ID]; ok && chain.pod == pod {
			index.chains[i] = chain
		}
		if key, ok := o.keys[pod.ID]; ok && key.pod == pod {
			index.keys[i] = key.key
		}
		if largeVehicleClass(pod.Class) {
			index.reach = max(index.reach, separationThresholdSquared(largeClearance))
		}
	}
	return index
}

// sweepResult is the result of one sweep of checkSeparation.
type sweepResult struct {
	// smallestSquared is the smallest squared gap of a counted pair, or
	// +Inf when the sweep counts no pair.
	smallestSquared float64
	// first and second are the first violating pair (i, j) in the order of
	// o.Pods, and violationSquared is its squared gap. first is -1 when no
	// pair violates.
	first, second    int
	violationSquared float64
}

// sweep compares each pod with the pods that follow it in order, which holds
// the pod indexes sorted by X. It skips a pair when its squared gap is at
// least reach and at least ceiling or the smallest squared gap so far. It
// stops at a pod when the squared X distance passes the same test.
func (index *separationIndex) sweep(pods []Pod, order []int, ceiling float64) sweepResult {
	result := sweepResult{smallestSquared: math.Inf(1), first: -1, second: -1}
	for offset, a := range order {
		for _, b := range order[offset+1:] {
			dx := pods[b].Position.X - pods[a].Position.X
			if index.skip(dx*dx, ceiling, result.smallestSquared) {
				break
			}
			i, j := min(a, b), max(a, b)
			gx := pods[i].Position.X - pods[j].Position.X
			gy := pods[i].Position.Y - pods[j].Position.Y
			gapSquared := gx*gx + gy*gy
			if index.skip(gapSquared, ceiling, result.smallestSquared) {
				continue
			}
			counted, violates := index.pair(pods, i, j, gapSquared)
			if !counted {
				continue
			}
			result.smallestSquared = min(result.smallestSquared, gapSquared)
			if violates && (result.first < 0 || i < result.first || i == result.first && j < result.second) {
				result.first, result.second, result.violationSquared = i, j, gapSquared
			}
		}
	}
	return result
}

// skip reports whether a sweep can skip a squared gap.
func (index *separationIndex) skip(squared, ceiling, smallestSquared float64) bool {
	return squared >= index.reach && (squared >= ceiling || squared >= smallestSquared)
}

// pair tests the pods i < j with the squared gap between them. It returns
// whether the gap counts toward the smallest gap, and whether the pods are
// too close. A pair whose bound keys form a certified pair of the maneuver
// certification is skipped. A small pair that a window certifies needs
// compactClearance at the speed of the follower (see certifiedClearance). A
// small pair whose bound keys form a lateral pair needs the clearance of
// that pair (see lateralClearance). Every other small pair needs Clearance.
func (index *separationIndex) pair(pods []Pod, i, j int, gapSquared float64) (counted, violates bool) {
	if len(index.keys) > 0 && index.certified.certified(index.keys[i], index.keys[j]) {
		return false, false
	}
	first, second := pods[i], pods[j]
	minimum := classPairClearance(first.Class, second.Class)
	var separated bool
	if minimum > Clearance {
		separated = len(index.envelopes) > 0 && envelopeLocationsSeparated(index.envelopes[i], index.envelopes[j])
	} else {
		separated = safetyLocationsSeparated(index.locations[i], index.locations[j])
	}
	if separated {
		return false, false
	}
	if minimum == Clearance && len(index.chains) > 0 {
		if clearance, ok := certifiedClearance(index.chains[i], index.chains[j]); ok {
			minimum = clearance
		}
	}
	if minimum == Clearance && len(index.keys) > 0 {
		if clearance, ok := index.lateral.clearance(index.keys[i], index.keys[j]); ok {
			minimum = clearance
		}
	}
	return true, gapSquared < separationThresholdSquared(minimum)
}

// separationThresholdSquared returns the squared gap below which a pair with
// the given minimum is too close.
func separationThresholdSquared(minimum float64) float64 {
	return (minimum - separationTolerance) * (minimum - separationTolerance)
}

// checkBerths verifies that each berth has at most one pod, and that the
// berth names that pod as its occupant and as its reservation holder.
func (o SafetyObservation) checkBerths() error {
	type occupancy struct {
		count int
		podID string
	}
	occupants := make(map[string]occupancy, len(o.Pods))
	for _, pod := range o.Pods {
		if pod.BerthID == "" {
			continue
		}
		occupied := occupants[pod.BerthID]
		occupied.count++
		occupied.podID = pod.ID
		occupants[pod.BerthID] = occupied
	}
	for _, berth := range o.Berths {
		occupied := occupants[berth.ID]
		if occupied.count > 1 {
			return fmt.Errorf("berth capacity exceeded at tick %d: %+v", o.Tick, berth)
		}
		if occupied.count == 1 && (berth.Occupant != occupied.podID || berth.ReservedBy != occupied.podID) {
			return fmt.Errorf("invalid berth state at tick %d: %+v", o.Tick, berth)
		}
	}
	return nil
}

// safetyLocationsSeparated reports whether two locations are on different
// planes. Locations in different separation groups are on different planes
// only when they have no node in common.
func safetyLocationsSeparated(first, second SafetyLocation) bool {
	if first.SeparationGroup == "" || second.SeparationGroup == "" || first.SeparationGroup == second.SeparationGroup {
		return false
	}
	return !safetyLocationsShareNode(first, second)
}

func safetyLocationsShareNode(first, second SafetyLocation) bool {
	return first.From != "" && (first.From == second.From || first.From == second.To) ||
		first.To != "" && (first.To == second.From || first.To == second.To)
}
