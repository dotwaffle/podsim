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
			minimum := classPairClearance(first.Class, second.Class)
			if o.certifiedCouplingPair(first, second) {
				dx, dy := first.Position.X-second.Position.X, first.Position.Y-second.Position.Y
				smallestSquared = min(smallestSquared, dx*dx+dy*dy)
				continue
			}
			separated := safetyLocationsSeparated(locations[index], locations[index+1+offset])
			if minimum > Clearance {
				separated = o.largePairSeparated(first, second)
			}
			if separated {
				continue
			}
			dx := first.Position.X - second.Position.X
			dy := first.Position.Y - second.Position.Y
			gapSquared := dx*dx + dy*dy
			smallestSquared = min(smallestSquared, gapSquared)
			if certified, ok := o.compactPairs[[2]string{first.ID, second.ID}]; minimum == Clearance && ok && certified.first == first && certified.second == second {
				minimum = certified.minimum
			} else if certified, ok := o.compactPairs[[2]string{second.ID, first.ID}]; minimum == Clearance && ok && certified.first == second && certified.second == first {
				minimum = certified.minimum
			}
			if gapSquared < (minimum-separationTolerance)*(minimum-separationTolerance) {
				return 0, &SeparationError{Tick: o.Tick, First: first.ID, Second: second.ID, Gap: math.Sqrt(gapSquared)}
			}
		}
	}
	return math.Sqrt(smallestSquared), nil
}

// checkReference is Check with checkSeparationReference.
func (o SafetyObservation) checkReference() (float64, error) {
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
	r := rand.New(rand.NewPCG(19, 1041))
	var violations, gaps, infinite, coupled, compact int
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
		index := o.separationIndex()
		for _, links := range index.links {
			for _, link := range links {
				if link.coupled {
					coupled++
				}
				if link.compact {
					compact++
				}
			}
		}
	}
	t.Logf("%d violations, %d finite gaps, %d infinite gaps, %d coupled pairs, %d compact pairs", violations, gaps, infinite, coupled, compact)
	// The generator must reach each kind of result.
	if violations == 0 || gaps == 0 || infinite == 0 || coupled == 0 || compact == 0 {
		t.Fatalf("weak coverage: %d violations, %d finite gaps, %d infinite gaps, %d coupled pairs, %d compact pairs", violations, gaps, infinite, coupled, compact)
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
// boundary positions, mixed classes and planes, duplicate IDs, and coupling,
// compact and envelope entries that match their pods or do not.
func randomSeparationObservation(r *rand.Rand) SafetyObservation {
	classes := []VehicleClass{"", LegacyClass, CompactClass, GroupClass, ExpressClass}
	groups := []string{"", "g1", "g2", "g3"}
	nodes := []string{"", "a", "b", "c", "d"}
	location := func() SafetyLocation {
		return SafetyLocation{SeparationGroup: groups[r.IntN(len(groups))], From: nodes[r.IntN(len(nodes))], To: nodes[r.IntN(len(nodes))]}
	}
	offsets := []float64{0, Clearance, largeClearance, Clearance - separationTolerance, largeClearance - 2*separationTolerance, 3, 7.5}
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
	// pick returns two pods, sometimes changed so that their values do not
	// match the observation.
	pick := func() (Pod, Pod) {
		a, b := r.IntN(n), r.IntN(n)
		first, second := o.Pods[a], o.Pods[b]
		switch r.IntN(6) {
		case 0:
			first.Speed++
		case 1:
			second, first = first, second
		}
		return first, second
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
		o.couplingPairs = make(map[[2]string]couplingSafetyPair)
		for range 1 + r.IntN(4) {
			first, second := pick()
			key := [2]string{first.ID, second.ID}
			if r.IntN(8) == 0 {
				key[0], key[1] = key[1], key[0]
			}
			o.couplingPairs[key] = couplingSafetyPair{pods: [2]Pod{first, second}}
		}
	}
	if r.IntN(2) == 0 {
		minimums := []float64{2, 5.5, Clearance, 15, largeClearance + 5, -3, math.NaN(), math.Inf(1)}
		o.compactPairs = make(map[[2]string]compactSafetyPair)
		for range 1 + r.IntN(6) {
			first, second := pick()
			key := [2]string{first.ID, second.ID}
			if r.IntN(8) == 0 {
				key[0], key[1] = key[1], key[0]
			}
			minimum := r.Float64() * Clearance
			if r.IntN(4) == 0 {
				minimum = minimums[r.IntN(len(minimums))]
			}
			o.compactPairs[key] = compactSafetyPair{first: first, second: second, minimum: minimum}
		}
	}
	return o
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
	const compactMinimum = 5.5
	thresholds := []struct {
		name    string
		class   VehicleClass
		minimum float64
		compact bool
	}{
		{name: "clearance", class: CompactClass, minimum: Clearance},
		{name: "large", class: GroupClass, minimum: largeClearance},
		{name: "compact", class: CompactClass, minimum: compactMinimum, compact: true},
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
				if threshold.compact {
					o.compactPairs = map[[2]string]compactSafetyPair{{"a", "b"}: {first: pods[0], second: pods[1], minimum: compactMinimum}}
				}
				add(fmt.Sprintf("%s distance %d sign %v", threshold.name, k, sign), o)
			}
		}
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
	zeroPods := []Pod{{ID: "a", Position: Point{X: zero, Y: zero}}, {ID: "b"}}
	add("signed zeros coupled", SafetyObservation{Pods: zeroPods, couplingPairs: map[[2]string]couplingSafetyPair{{"a", "b"}: {pods: [2]Pod{zeroPods[0], zeroPods[1]}}}})
	// Certificates that name absent pods.
	ghosts := pair(CompactClass, 3)
	ghost := Pod{ID: "ghost", Class: CompactClass, Position: Point{X: 3}}
	add("certificates of absent pods", SafetyObservation{
		Pods:          ghosts,
		couplingPairs: map[[2]string]couplingSafetyPair{{"a", "ghost"}: {pods: [2]Pod{ghosts[0], ghost}}, {"ghost", "b"}: {pods: [2]Pod{ghost, ghosts[1]}}},
		compactPairs:  map[[2]string]compactSafetyPair{{"ghost", "a"}: {first: ghost, second: ghosts[0], minimum: 1}, {"b", "ghost"}: {first: ghosts[1], second: ghost, minimum: 1}},
		envelopes:     map[string]safetyEnvelope{"ghost": {pod: ghost, locations: []SafetyLocation{{SeparationGroup: "g1"}}}},
	})
	// A far certified coupled pair gives the smallest gap, because all
	// other pairs are on separate planes.
	far := []Pod{
		{ID: "a"},
		{ID: "x", Position: Point{X: 1}},
		{ID: "y", Position: Point{X: 2}},
		{ID: "b", Position: Point{X: 1000, Y: 1}},
	}
	add("far coupled pair", SafetyObservation{
		Pods: far,
		Locations: map[string]SafetyLocation{
			"a": {SeparationGroup: "g1", From: "n1", To: "n2"},
			"x": {SeparationGroup: "g2", From: "n3", To: "n4"},
			"y": {SeparationGroup: "g3", From: "n5", To: "n6"},
			"b": {SeparationGroup: "g4", From: "n7", To: "n8"},
		},
		couplingPairs: map[[2]string]couplingSafetyPair{{"b", "a"}: {pods: [2]Pod{far[3], far[0]}}},
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
