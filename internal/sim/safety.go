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
// speed limit, because each lane has its own limit. Only certified direct
// compact neighbors may use their validated envelope below Clearance.
func (o SafetyObservation) Check() (float64, error) {
	if o.compactError != nil {
		return 0, o.compactError
	}
	if err := o.checkCouplingSafety(); err != nil {
		return 0, err
	}
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
// It sorts the pods by X and, for each pod, compares only the pods that
// follow it in that order until the squared X distance is at least both the
// smallest squared gap so far and reach, the largest squared minimum of any
// pair. The result is the same as a comparison of all pairs:
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
//     reach. Its squared gap is also at least the smallest squared gap so
//     far, so it cannot change the smallest gap. Certified coupling pairs
//     only change the smallest gap, so the same bound covers them.
//   - Each pair test uses the pods in their order in o.Pods, and the result
//     of a pair does not depend on the other pairs. The first violating pair
//     in the order of o.Pods is the smallest violating (i, j), which the
//     sweep finds because it does not skip a violating pair.
//   - (a-b)*(a-b) and (b-a)*(b-a) are equal, so the order of the
//     subtraction does not change the gap.
func (o SafetyObservation) checkSeparation() (float64, error) {
	index := o.separationIndex()
	order := make([]int, len(o.Pods))
	for i := range order {
		order[i] = i
	}
	slices.SortFunc(order, func(a, b int) int { return cmp.Compare(o.Pods[a].Position.X, o.Pods[b].Position.X) })
	smallestSquared := math.Inf(1)
	first, second, violationSquared := -1, -1, 0.0
	for offset, a := range order {
		for _, b := range order[offset+1:] {
			dx := o.Pods[b].Position.X - o.Pods[a].Position.X
			if dx*dx >= smallestSquared && dx*dx >= index.reach {
				break
			}
			i, j := min(a, b), max(a, b)
			gapSquared, counted, violates := index.pair(o.Pods, i, j)
			if !counted {
				continue
			}
			smallestSquared = min(smallestSquared, gapSquared)
			if violates && (first < 0 || i < first || i == first && j < second) {
				first, second, violationSquared = i, j, gapSquared
			}
		}
	}
	if first >= 0 {
		return 0, &SeparationError{Tick: o.Tick, First: o.Pods[first].ID, Second: o.Pods[second].ID, Gap: math.Sqrt(violationSquared)}
	}
	return math.Sqrt(smallestSquared), nil
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
	// links holds the certified pairs (i, j) with i < j in links[i].
	links [][]separationLink
	// reach is the largest squared separation threshold of any pair.
	reach float64
}

// separationLink records a certified coupling pair or a compact pair.
type separationLink struct {
	other    int
	coupled  bool
	compact  bool
	envelope float64
}

func (o SafetyObservation) separationIndex() separationIndex {
	index := separationIndex{
		locations: make([]SafetyLocation, len(o.Pods)),
		reach:     separationThresholdSquared(Clearance),
	}
	if len(o.envelopes) > 0 {
		index.envelopes = make([][]SafetyLocation, len(o.Pods))
	}
	for i, pod := range o.Pods {
		index.locations[i] = o.Locations[pod.ID]
		if envelope, ok := o.envelopes[pod.ID]; ok && envelope.pod == pod {
			index.envelopes[i] = envelope.locations
		}
		if largeVehicleClass(pod.Class) {
			index.reach = max(index.reach, separationThresholdSquared(largeClearance))
		}
	}
	if len(o.couplingPairs) == 0 && len(o.compactPairs) == 0 {
		return index
	}
	ids := make(map[string][]int, len(o.Pods))
	for i, pod := range o.Pods {
		ids[pod.ID] = append(ids[pod.ID], i)
	}
	index.links = make([][]separationLink, len(o.Pods))
	// Each key can match only the pods with its two IDs. The pair result
	// comes from the lookups of the original pair loop, with the pods in
	// their order in o.Pods.
	for key := range o.couplingPairs {
		for _, pair := range idPairs(ids, key) {
			if o.certifiedCouplingPair(o.Pods[pair[0]], o.Pods[pair[1]]) {
				index.link(pair[0], pair[1]).coupled = true
			}
		}
	}
	for key, certified := range o.compactPairs {
		if threshold := separationThresholdSquared(certified.minimum); threshold > index.reach {
			index.reach = threshold
		}
		for _, pair := range idPairs(ids, key) {
			if envelope, ok := o.compactPairMinimum(o.Pods[pair[0]], o.Pods[pair[1]]); ok {
				link := index.link(pair[0], pair[1])
				link.compact, link.envelope = true, envelope
			}
		}
	}
	return index
}

// idPairs returns each pair (i, j) with i < j of pods with the two IDs.
func idPairs(ids map[string][]int, key [2]string) [][2]int {
	var pairs [][2]int
	for _, a := range ids[key[0]] {
		for _, b := range ids[key[1]] {
			if a != b {
				pairs = append(pairs, [2]int{min(a, b), max(a, b)})
			}
		}
	}
	return pairs
}

// link returns the link of the pair (i, j) with i < j, and adds it when
// it is not there.
func (index *separationIndex) link(i, j int) *separationLink {
	for k := range index.links[i] {
		if index.links[i][k].other == j {
			return &index.links[i][k]
		}
	}
	index.links[i] = append(index.links[i], separationLink{other: j})
	return &index.links[i][len(index.links[i])-1]
}

// pair tests the pods i < j. It returns their squared gap, whether the gap
// counts toward the smallest gap, and whether they are too close.
func (index *separationIndex) pair(pods []Pod, i, j int) (gapSquared float64, counted, violates bool) {
	var link separationLink
	if index.links != nil {
		for _, candidate := range index.links[i] {
			if candidate.other == j {
				link = candidate
			}
		}
	}
	first, second := pods[i], pods[j]
	dx := first.Position.X - second.Position.X
	dy := first.Position.Y - second.Position.Y
	if link.coupled {
		return dx*dx + dy*dy, true, false
	}
	minimum := classPairClearance(first.Class, second.Class)
	var separated bool
	if minimum > Clearance {
		separated = len(index.envelopes) > 0 && envelopeLocationsSeparated(index.envelopes[i], index.envelopes[j])
	} else {
		separated = safetyLocationsSeparated(index.locations[i], index.locations[j])
	}
	if separated {
		return 0, false, false
	}
	gapSquared = dx*dx + dy*dy
	if minimum == Clearance && link.compact {
		minimum = link.envelope
	}
	return gapSquared, true, gapSquared < separationThresholdSquared(minimum)
}

// separationThresholdSquared returns the squared gap below which a pair with
// the given minimum is too close.
func separationThresholdSquared(minimum float64) float64 {
	return (minimum - separationTolerance) * (minimum - separationTolerance)
}

// compactPairMinimum returns the validated envelope of a certified compact
// pair. It tries the key in the order of the pods first.
func (o SafetyObservation) compactPairMinimum(first, second Pod) (float64, bool) {
	if certified, ok := o.compactPairs[[2]string{first.ID, second.ID}]; ok && certified.first == first && certified.second == second {
		return certified.minimum, true
	}
	if certified, ok := o.compactPairs[[2]string{second.ID, first.ID}]; ok && certified.first == second && certified.second == first {
		return certified.minimum, true
	}
	return 0, false
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
