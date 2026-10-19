package sim

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"testing"
)

// checkSeparationReference is the pair-by-pair checkSeparation that compares
// every pair. The differential test compares checkSeparation with it.
func (o SafetyObservation) checkSeparationReference() (float64, error) {
	locations := make([]SafetyLocation, len(o.Pods))
	for index, pod := range o.Pods {
		locations[index] = o.Locations[pod.ID]
	}
	smallestSquared := math.Inf(1)
	for index, first := range o.Pods {
		for offset, second := range o.Pods[index+1:] {
			a, aOK := o.keys[first.ID]
			b, bOK := o.keys[second.ID]
			bound := aOK && bOK && a.pod == first && b.pod == second
			if bound && o.certified.certified(a.key, b.key) {
				continue
			}
			minimum := classPairClearance(first.Class, second.Class)
			separated := safetyLocationsSeparated(locations[index], locations[index+1+offset])
			if minimum > Clearance {
				separated = o.largePairSeparated(first, second)
			}
			if separated {
				continue
			}
			if clearance, ok := o.lateral.clearance(a.key, b.key); bound && minimum == Clearance && ok {
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

// largePairSeparated is the envelope test of the reference pair loop.
func (o SafetyObservation) largePairSeparated(first, second Pod) bool {
	a, aOK := o.envelopes[first.ID]
	b, bOK := o.envelopes[second.ID]
	return aOK && bOK && a.pod == first && b.pod == second && envelopeLocationsSeparated(a.locations, b.locations)
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
// boundary positions, mixed classes and planes, duplicate IDs, envelope
// entries that match their pods or do not, and separation keys that match
// their pods or do not, with certified and lateral pairs.
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
	o := SafetyObservation{Tick: r.Int64N(1000), Locations: make(map[string]SafetyLocation)}
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
			o.Locations[pod.ID] = location()
		}
		o.Pods = append(o.Pods, pod)
	}
	if n < 2 {
		return o
	}
	if r.IntN(2) == 0 {
		o.envelopes = make(map[string]safetyEnvelope)
		for _, pod := range o.Pods {
			if r.IntN(4) == 0 {
				continue
			}
			if r.IntN(6) == 0 {
				pod.Speed++
			}
			var locations []SafetyLocation
			for range r.IntN(3) {
				locations = append(locations, location())
			}
			o.envelopes[pod.ID] = safetyEnvelope{pod: pod, locations: locations}
		}
	}
	if r.IntN(2) == 0 {
		addRandomKeys(r, &o)
	}
	return o
}

// addRandomKeys binds separation keys to the pods of an observation, with
// random certified and lateral pairs of those keys. Some keys are bound to
// a changed pod, and some pods have no key.
func addRandomKeys(r *rand.Rand, o *SafetyObservation) {
	keys := []separationKey{laneKey(0), laneKey(1), laneKey(2), berthKey(0), berthKey(1), berthKey(2)}
	key := func() separationKey { return keys[r.IntN(len(keys))] }
	o.keys = make(map[string]podKey)
	for _, pod := range o.Pods {
		if r.IntN(4) == 0 {
			continue
		}
		if r.IntN(6) == 0 {
			pod.Speed++
		}
		o.keys[pod.ID] = podKey{pod: pod, key: key()}
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
		o.keys = map[string]podKey{pods[0].ID: {pod: pods[0], key: laneKey(0)}, pods[1].ID: {pod: pods[1], key: berthKey(0)}}
		o.lateral = &depotLateral{pairs: map[[2]separationKey]float64{keyPair(laneKey(0), berthKey(0)): lateralClearance(2)}}
		return o
	}
	certified := func(pods []Pod, o SafetyObservation) SafetyObservation {
		o.keys = map[string]podKey{pods[0].ID: {pod: pods[0], key: laneKey(0)}, pods[1].ID: {pod: pods[1], key: laneKey(1)}}
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
		changed := pods[1]
		changed.Speed++
		unbound.keys[changed.ID] = podKey{pod: changed, key: berthKey(0)}
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
	// An envelope that names an absent pod.
	ghosts := pair(CompactClass, 3)
	ghost := Pod{ID: "ghost", Class: CompactClass, Position: Point{X: 3}}
	add("envelope of an absent pod", SafetyObservation{
		Pods:      ghosts,
		envelopes: map[string]safetyEnvelope{"ghost": {pod: ghost, locations: []SafetyLocation{{SeparationGroup: "g1"}}}},
	})
	return cases
}

// TestCheckSeparationAllocations bounds the allocations of Check on separated
// ordinary pods without envelopes. The bound is the reference plus two: one
// for the sort order of the pods, and one in case the sort closure escapes
// in a later compiler. An allocation for each pod or each pair exceeds it.
func TestCheckSeparationAllocations(t *testing.T) {
	o := SafetyObservation{Locations: make(map[string]SafetyLocation)}
	for i := range 300 {
		id := fmt.Sprintf("p%d", i)
		o.Pods = append(o.Pods, Pod{ID: id, Position: Point{X: float64(i)}})
		o.Locations[id] = SafetyLocation{SeparationGroup: id, From: id + "-from", To: id + "-to"}
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
