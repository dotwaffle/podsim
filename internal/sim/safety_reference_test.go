package sim

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"testing"
)

// referenceSlot is the entry of pod i of o.Pods in the per-pod slices, or
// -1. It searches the bound pods one by one.
func (o SafetyObservation) referenceSlot(i int) int {
	id := o.Pods[i].ID
	if len(o.bound) == 0 || i < len(o.bound) && o.bound[i].ID == id {
		return i
	}
	slot := -1
	for k, pod := range o.bound {
		if pod.ID == id {
			slot = k
		}
	}
	return slot
}

// referenceBound reports whether pod i of o.Pods is equal to its bound pod.
func (o SafetyObservation) referenceBound(i int) bool {
	slot := o.referenceSlot(i)
	return slot >= 0 && slot < len(o.bound) && o.bound[slot] == o.Pods[i]
}

// referenceKey is the key of pod i of o.Pods, and whether the pod has one.
func (o SafetyObservation) referenceKey(i int) (separationKey, bool) {
	if slot := o.referenceSlot(i); o.referenceBound(i) && slot < len(o.keys) {
		return o.keys[slot], true
	}
	return separationKey{}, false
}

// checkSeparationReference is the pair-by-pair checkSeparation that compares
// every pair. The differential test compares checkSeparation with it.
func (o SafetyObservation) checkSeparationReference() (float64, error) {
	locations := make([]SafetyLocation, len(o.Pods))
	for index := range o.Pods {
		if slot := o.referenceSlot(index); slot >= 0 && slot < len(o.Locations) {
			locations[index] = o.Locations[slot]
		}
	}
	smallestSquared := math.Inf(1)
	for index, first := range o.Pods {
		for offset, second := range o.Pods[index+1:] {
			a, aOK := o.referenceKey(index)
			b, bOK := o.referenceKey(index + 1 + offset)
			bound := aOK && bOK
			if bound && o.certified.certified(a, b) {
				continue
			}
			minimum := classPairClearance(first.Class, second.Class)
			separated := safetyLocationsSeparated(locations[index], locations[index+1+offset])
			if minimum > Clearance {
				separated = o.largePairSeparated(index, index+1+offset)
			}
			if separated {
				continue
			}
			if clearance, ok := o.lateral.clearance(a, b); bound && minimum == Clearance && ok {
				minimum = clearance
			}
			dx := first.Position.X - second.Position.X
			dy := first.Position.Y - second.Position.Y
			gapSquared := dx*dx + dy*dy
			smallestSquared = min(smallestSquared, gapSquared)
			if gapSquared < (minimum-separationTolerance)*(minimum-separationTolerance) {
				return 0, &SeparationError{Tick: o.Tick, First: first.ID, Second: second.ID, Gap: math.Sqrt(gapSquared)}
			}
		}
	}
	return math.Sqrt(smallestSquared), nil
}

// checkReference is Check with checkSeparationReference.
func (o SafetyObservation) checkReference() (float64, error) {
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
	gap, err := o.checkSeparationReference()
	if err != nil {
		return 0, err
	}
	if err := o.checkBerths(); err != nil {
		return 0, err
	}
	return gap, nil
}

// largePairSeparated is the envelope test of the reference pair loop, for
// the pods i and j of o.Pods.
func (o SafetyObservation) largePairSeparated(i, j int) bool {
	a, b := o.referenceSlot(i), o.referenceSlot(j)
	return o.referenceBound(i) && o.referenceBound(j) && a < len(o.envelopes) && b < len(o.envelopes) &&
		envelopeLocationsSeparated(o.envelopes[a], o.envelopes[b])
}

// TestCheckSeparationMatchesReference compares checkSeparation with the
// reference on random observations. The gap and the error must be equal to
// the bit.
func TestCheckSeparationMatchesReference(t *testing.T) {
	t.Parallel()
	skipLong(t)
	r := rand.New(rand.NewPCG(19, 1041))
	var violations, gaps, infinite int
	for iteration := range 20000 {
		o := randomSeparationObservation(r)
		gap, err := o.checkSeparation()
		wantGap, wantErr := o.checkSeparationReference()
		if math.Float64bits(gap) != math.Float64bits(wantGap) || !sameSeparationError(err, wantErr) {
			t.Fatalf("observation %d: got (%v, %v), want (%v, %v)", iteration, gap, err, wantGap, wantErr)
		}
		switch {
		case wantErr != nil:
			violations++
		case math.IsInf(wantGap, 1):
			infinite++
		default:
			gaps++
		}
	}
	t.Logf("%d violations, %d finite gaps, %d infinite gaps", violations, gaps, infinite)
	// The generator must reach each kind of result.
	if violations == 0 || gaps == 0 || infinite == 0 {
		t.Fatalf("weak coverage: %d violations, %d finite gaps, %d infinite gaps", violations, gaps, infinite)
	}
}

func sameSeparationError(got, want error) bool {
	if got == nil || want == nil {
		return got == nil && want == nil
	}
	var a, b *SeparationError
	if !errors.As(got, &a) || !errors.As(want, &b) {
		return false
	}
	return a.Tick == b.Tick && a.First == b.First && a.Second == b.Second && math.Float64bits(a.Gap) == math.Float64bits(b.Gap)
}

// randomSeparationObservation makes an observation with dense, tied and
// boundary positions, mixed classes and planes, duplicate IDs, envelopes
// and separation keys with certified and lateral pairs, bound pods that
// match their pods or do not, and pods that a caller removed or moved.
func randomSeparationObservation(r *rand.Rand) SafetyObservation {
	classes := []VehicleClass{"", LegacyClass, CompactClass, GroupClass, ExpressClass}
	groups := []string{"", "g1", "g2", "g3"}
	nodes := []string{"", "a", "b", "c", "d"}
	location := func() SafetyLocation {
		return SafetyLocation{SeparationGroup: groups[r.IntN(len(groups))], From: nodes[r.IntN(len(nodes))], To: nodes[r.IntN(len(nodes))]}
	}
	offsets := []float64{0, Clearance, largeClearance, Clearance - separationTolerance, largeClearance - 2*separationTolerance, 3, 7.5, lateralClearance(0), lateralClearance(2), lateralClearance(2) - 2*separationTolerance}
	n := r.IntN(60)
	if r.IntN(4) == 0 {
		n = r.IntN(3)
	}
	span := float64(1 + n*r.IntN(48))
	if r.IntN(8) == 0 {
		span = 1e5
	}
	step := []float64{0.25, 1, 4, 6}[r.IntN(4)]
	stacked := r.IntN(6) == 0
	boundaries := r.IntN(2) == 0
	o := SafetyObservation{Tick: r.Int64N(1000), Locations: make([]SafetyLocation, n)}
	for i := range n {
		pod := Pod{ID: fmt.Sprintf("p%d", i), Class: classes[r.IntN(len(classes))], Speed: float64(r.IntN(3))}
		if i > 0 && r.IntN(20) == 0 {
			pod.ID = o.Pods[r.IntN(i)].ID
		}
		pod.Position = Point{X: math.Round(r.Float64()*span/step) * step, Y: math.Round(r.Float64()*span/step) * step}
		if stacked {
			pod.Position.X = 0
		}
		if boundaries && i > 0 && r.IntN(3) == 0 {
			// Place the pod at a boundary distance from an earlier pod.
			other := o.Pods[r.IntN(i)].Position
			offset := offsets[r.IntN(len(offsets))]
			if r.IntN(2) == 0 {
				pod.Position = Point{X: other.X + offset, Y: other.Y}
			} else {
				pod.Position = Point{X: other.X, Y: other.Y - offset}
			}
		}
		if r.IntN(5) != 0 {
			o.Locations[i] = location()
		}
		o.Pods = append(o.Pods, pod)
	}
	if n < 2 {
		return o
	}
	if r.IntN(8) != 0 {
		// Some bound pods differ from their pods.
		o.bound = slices.Clone(o.Pods)
		for i := range o.bound {
			if r.IntN(6) == 0 {
				o.bound[i].Speed++
			}
		}
	}
	if r.IntN(2) == 0 {
		o.envelopes = make([][]SafetyLocation, n)
		for i := range o.envelopes {
			if r.IntN(4) == 0 {
				continue
			}
			for range r.IntN(3) {
				o.envelopes[i] = append(o.envelopes[i], location())
			}
		}
	}
	if r.IntN(2) == 0 {
		addRandomKeys(r, &o)
	}
	switch r.IntN(8) {
	case 0:
		// A caller removed pods in place, as the restore does.
		o.Pods = slices.DeleteFunc(slices.Clone(o.Pods), func(Pod) bool { return r.IntN(3) == 0 })
	case 1:
		o.Pods = slices.Clone(o.Pods)
		r.Shuffle(len(o.Pods), func(i, j int) { o.Pods[i], o.Pods[j] = o.Pods[j], o.Pods[i] })
	case 2:
		// The per-pod slices are shorter than the pods.
		o.Locations = o.Locations[:r.IntN(n)]
		if o.envelopes != nil {
			o.envelopes = o.envelopes[:r.IntN(n)]
		}
		if o.keys != nil {
			o.keys = o.keys[:r.IntN(n)]
		}
	}
	return o
}

// addRandomKeys gives separation keys to the pods of an observation, with
// random certified and lateral pairs of those keys. Some pods have no key.
func addRandomKeys(r *rand.Rand, o *SafetyObservation) {
	keys := []separationKey{laneKey(0), laneKey(1), laneKey(2), berthKey(0), berthKey(1), berthKey(2)}
	key := func() separationKey { return keys[r.IntN(len(keys))] }
	o.keys = make([]separationKey, len(o.Pods))
	for i := range o.keys {
		if r.IntN(4) != 0 {
			o.keys[i] = key()
		}
	}
	clearances := []float64{lateralClearance(0), lateralClearance(2), lateralClearance(3.5)}
	o.lateral = &depotLateral{pairs: make(map[[2]separationKey]float64)}
	for range r.IntN(8) {
		o.lateral.pairs[keyPair(key(), key())] = clearances[r.IntN(len(clearances))]
	}
	if r.IntN(2) == 0 {
		o.certified = make(certifiedPairs)
		for range r.IntN(3) {
			o.certified[keyPair(key(), key())] = true
		}
	}
}

// TestCheckSeparationBoundaryCases compares checkSeparation and Check with
// the references on fixed cases that random positions rarely reach.
func TestCheckSeparationBoundaryCases(t *testing.T) {
	t.Parallel()
	for _, c := range separationBoundaryCases() {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			gap, err := c.observation.checkSeparation()
			wantGap, wantErr := c.observation.checkSeparationReference()
			if math.Float64bits(gap) != math.Float64bits(wantGap) || !sameSeparationError(err, wantErr) {
				t.Fatalf("checkSeparation: got (%v, %v), want (%v, %v)", gap, err, wantGap, wantErr)
			}
			gap, err = c.observation.Check()
			wantGap, wantErr = c.observation.checkReference()
			if math.Float64bits(gap) != math.Float64bits(wantGap) || fmt.Sprint(err) != fmt.Sprint(wantErr) {
				t.Fatalf("Check: got (%v, %v), want (%v, %v)", gap, err, wantGap, wantErr)
			}
		})
	}
}

type separationBoundaryCase struct {
	name        string
	observation SafetyObservation
}

func separationBoundaryCases() []separationBoundaryCase {
	var cases []separationBoundaryCase
	add := func(name string, o SafetyObservation) {
		cases = append(cases, separationBoundaryCase{name: name, observation: o})
	}
	pair := func(class VehicleClass, x float64) []Pod {
		return []Pod{{ID: "a", Class: class}, {ID: "b", Class: class, Position: Point{X: x}}}
	}
	// Distances just below, at and just above each squared threshold. The
	// threshold compares squares, so step the distance around its root.
	thresholds := []struct {
		name    string
		class   VehicleClass
		minimum float64
	}{
		{name: "clearance", class: CompactClass, minimum: Clearance},
		{name: "large", class: GroupClass, minimum: largeClearance},
	}
	// lateral binds the keys of a lateral pair to the pods, and certified
	// those of a certified pair.
	lateral := func(pods []Pod, o SafetyObservation) SafetyObservation {
		o.bound, o.keys = slices.Clone(pods), []separationKey{laneKey(0), berthKey(0)}
		o.lateral = &depotLateral{pairs: map[[2]separationKey]float64{keyPair(laneKey(0), berthKey(0)): lateralClearance(2)}}
		return o
	}
	certified := func(pods []Pod, o SafetyObservation) SafetyObservation {
		o.bound, o.keys = slices.Clone(pods), []separationKey{laneKey(0), laneKey(1)}
		o.certified = certifiedPairs{keyPair(laneKey(0), laneKey(1)): true}
		return o
	}
	for _, threshold := range thresholds {
		root := math.Sqrt(separationThresholdSquared(threshold.minimum))
		distances := []float64{
			math.Nextafter(math.Nextafter(root, 0), 0), math.Nextafter(root, 0), root,
			math.Nextafter(root, math.Inf(1)), math.Nextafter(math.Nextafter(root, math.Inf(1)), math.Inf(1)),
			threshold.minimum - separationTolerance, threshold.minimum,
		}
		for k, distance := range distances {
			for _, sign := range []float64{1, -1} {
				pods := pair(threshold.class, sign*distance)
				o := SafetyObservation{Tick: 1, Pods: pods}
				add(fmt.Sprintf("%s distance %d sign %v", threshold.name, k, sign), o)
				add(fmt.Sprintf("%s distance %d sign %v with lateral keys", threshold.name, k, sign), lateral(pods, o))
				add(fmt.Sprintf("%s distance %d sign %v with certified keys", threshold.name, k, sign), certified(pods, o))
			}
		}
	}
	// Distances about the threshold of a lateral pair, and the same pair
	// with a key bound to a changed pod.
	root := math.Sqrt(separationThresholdSquared(lateralClearance(2)))
	for k, distance := range []float64{math.Nextafter(root, 0), root, math.Nextafter(root, math.Inf(1)), lateralClearance(2)} {
		pods := pair(CompactClass, distance)
		add(fmt.Sprintf("lateral distance %d", k), lateral(pods, SafetyObservation{Tick: 1, Pods: pods}))
		unbound := lateral(pods, SafetyObservation{Tick: 1, Pods: pods})
		unbound.bound[1].Speed++
		add(fmt.Sprintf("lateral distance %d with a changed pod", k), unbound)
	}
	// Finite coordinates whose dx*dx overflows to +Inf.
	add("overflow", SafetyObservation{Pods: []Pod{
		{ID: "a", Position: Point{X: -1e200}},
		{ID: "b", Position: Point{X: 1e200}},
		{ID: "c", Position: Point{X: 1e160, Y: -1e160}},
	}})
	add("overflow beside a near pair", SafetyObservation{Pods: []Pod{
		{ID: "a", Position: Point{X: -math.MaxFloat64}},
		{ID: "b", Position: Point{X: math.MaxFloat64}},
		{ID: "c", Position: Point{X: math.MaxFloat64, Y: 30}},
	}})
	// Signed zero positions.
	zero := math.Copysign(0, -1)
	add("signed zeros", SafetyObservation{Pods: []Pod{
		{ID: "a", Position: Point{X: zero, Y: zero}},
		{ID: "b", Position: Point{X: 0, Y: Clearance}},
		{ID: "c", Position: Point{X: Clearance, Y: zero}},
	}})
	add("signed zeros apart", SafetyObservation{Pods: []Pod{
		{ID: "a", Position: Point{X: zero}},
		{ID: "b", Position: Point{X: 0, Y: largeClearance}},
	}})
	// An envelope that belongs to an absent pod.
	ghosts := pair(CompactClass, 3)
	ghost := Pod{ID: "ghost", Class: CompactClass, Position: Point{X: 3}}
	add("envelope of an absent pod", SafetyObservation{
		Pods:      ghosts,
		bound:     append(slices.Clone(ghosts), ghost),
		envelopes: [][]SafetyLocation{nil, nil, {{SeparationGroup: "g1"}}},
	})
	return cases
}

// TestCheckSeparationAllocations bounds the allocations of Check on separated
// ordinary pods without envelopes. The bound is the reference plus two: one
// for the sort order of the pods, and one in case the sort closure escapes
// in a later compiler. An allocation for each pod or each pair exceeds it.
func TestCheckSeparationAllocations(t *testing.T) {
	var o SafetyObservation
	for i := range 300 {
		id := fmt.Sprintf("p%d", i)
		o.Pods = append(o.Pods, Pod{ID: id, Position: Point{X: float64(i)}})
		o.Locations = append(o.Locations, SafetyLocation{SeparationGroup: id, From: id + "-from", To: id + "-to"})
	}
	if gap, err := o.Check(); err != nil || !math.IsInf(gap, 1) {
		t.Fatalf("Check() = %v, %v, want +Inf and no error", gap, err)
	}
	got := testing.AllocsPerRun(20, func() { _, _ = o.Check() })
	want := testing.AllocsPerRun(20, func() { _, _ = o.checkReference() })
	t.Logf("Check: %v allocations, reference: %v", got, want)
	if got > want+2 {
		t.Fatalf("Check makes %v allocations, want at most %v", got, want+2)
	}
}

// TestCheckSeparationTestsNearPairsOnly checks that checkSeparation does not
// test the far pairs when a pair below reach exists. The pods are 100 m
// apart on a line, each on its own plane, and one more pod is 15 m from the
// last of them on the same plane. An Express pod raises reach above 15 m. A
// sweep with no ceiling tests each pair of the line.
func TestCheckSeparationTestsNearPairsOnly(t *testing.T) {
	t.Parallel()
	const count = 200
	var o SafetyObservation
	add := func(id string, class VehicleClass, x float64, group string) {
		o.Pods = append(o.Pods, Pod{ID: id, Class: class, Position: Point{X: x}})
		o.Locations = append(o.Locations, SafetyLocation{SeparationGroup: group})
	}
	for i := range count {
		add(fmt.Sprintf("pod-%03d", i), CompactClass, float64(100*i), fmt.Sprintf("plane-%03d", i))
	}
	last := float64(100 * (count - 1))
	add("near", CompactClass, last+15, fmt.Sprintf("plane-%03d", count-1))
	add("express", ExpressClass, last+1000, "")
	result := o.sweepSeparation()
	if result.first >= 0 || result.smallestSquared != 15*15 {
		t.Fatalf("got violation %d and squared gap %v, want none and 225", result.first, result.smallestSquared)
	}
	if result.tested != 1 {
		t.Errorf("tested %d pairs, want 1", result.tested)
	}
	gap, err := o.checkSeparation()
	wantGap, wantErr := o.checkSeparationReference()
	if math.Float64bits(gap) != math.Float64bits(wantGap) || !sameSeparationError(err, wantErr) {
		t.Fatalf("got (%v, %v), want (%v, %v)", gap, err, wantGap, wantErr)
	}
}

// TestSeparationThresholdsWithinReach checks the assumption of the first
// sweep of checkSeparation: no pair needs more than the reach of its
// observation. A pair of two small pods needs Clearance, the certified
// clearance of a window, or the lateral clearance of a depot pair. The
// lateral clearance grows with the speed, and only lateralSlow limits it.
func TestSeparationThresholdsWithinReach(t *testing.T) {
	t.Parallel()
	classes := []VehicleClass{"", LegacyClass, CompactClass, GroupClass, ExpressClass}
	for _, first := range classes {
		for _, second := range classes {
			o := SafetyObservation{Pods: []Pod{{ID: "a", Class: first}, {ID: "b", Class: second}}}
			index := o.separationIndex()
			if threshold := separationThresholdSquared(classPairClearance(first, second)); threshold > index.reach {
				t.Errorf("classes %q and %q: squared threshold %v is more than reach %v", first, second, threshold, index.reach)
			}
		}
	}
	// The speeds are the two sides of the gate, the largest lane speed
	// limit, and steps of 1 mm/s to 100 m/s.
	speeds := []float64{math.Nextafter(reducedClearanceSpeed, 0), reducedClearanceSpeed, 1e3, 1e6, MaxCounter}
	for step := range 100001 {
		speeds = append(speeds, float64(step)/1000)
	}
	for _, speed := range speeds {
		if got := compactClearance(speed); got > Clearance {
			t.Fatalf("compactClearance(%v) = %v, more than Clearance", speed, got)
		}
		if got := lateralClearance(speed); lateralSlow(speed) && got > Clearance {
			t.Fatalf("lateralClearance(%v) = %v for a slow member, more than Clearance", speed, got)
		}
	}
}
