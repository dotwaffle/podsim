package sim

import (
	"errors"
	"math"
	"math/rand/v2"
	"reflect"
	"testing"
)

func TestCompactQueueRecoveryFinite(t *testing.T) {
	for _, test := range []struct {
		name   string
		speeds []float64
		margin float64
	}{
		{"two stopped", []float64{0, 0}, 1},
		{"four stopped", []float64{0, 0, 0, 0}, 1},
		{"equal nine km/h", []float64{2.5, 2.5, 2.5, 2.5}, 1},
		{"closing followers", []float64{0, 2.5, 1, 2}, 1},
		{"alternating speeds", []float64{2.5, 0, 2.5, 0}, 1},
		{"derived small allowance", []float64{0, 0, 0, 0}, 1e-8},
	} {
		t.Run(test.name, func(t *testing.T) {
			states, bounds := compactQueueTestStates(test.speeds, 0, test.margin)
			recovery, err := compactQueueRecoveryAdmission(states, bounds)
			if err != nil {
				t.Fatal(err)
			}
			compactQueueFinishRecovery(t, states, recovery)
		})
	}

	// One representable unit of extra owned track is enough at ordinary route
	// coordinates. It must be positive. No fixed extra distance is imposed.
	for members := 2; members <= 4; members++ {
		states, bounds := compactQueueTestStates(make([]float64, members), 0, 1)
		targets, err := compactQueueRecoveryTargets(states, bounds)
		if err != nil {
			t.Fatal(err)
		}
		bounds.frontier = math.Nextafter(targets[0], math.Inf(1))
		for i := range states {
			states[i].stopBoundary = bounds.frontier
		}
		recovery, err := compactQueueRecoveryAdmission(states, bounds)
		if err != nil {
			t.Fatal(err)
		}
		compactQueueFinishRecovery(t, states, recovery)
	}
}

func TestCompactQueueRecoveryRandomFinite(t *testing.T) {
	random := rand.New(rand.NewPCG(281, 719))
	for range 120 {
		speeds := make([]float64, 2+random.IntN(3))
		for i := range speeds {
			speeds[i] = 2.5 * random.Float64()
		}
		states, bounds := compactQueueTestStates(speeds, 1e-8+random.Float64()*4, 1e-5+random.Float64())
		recovery, err := compactQueueRecoveryAdmission(states, bounds)
		if err != nil {
			t.Fatal(err)
		}
		compactQueueFinishRecovery(t, states, recovery)
	}
}

func TestCompactQueueRecoveryConsumesRoom(t *testing.T) {
	states, bounds := compactQueueTestStates([]float64{0, 0, 0, 0}, 0, 1)
	recovery, err := compactQueueRecoveryAdmission(states, bounds)
	if err != nil {
		t.Fatal(err)
	}
	consumed := false
	for range 2500 {
		planned, done, err := compactQueueRecoveryStep(states, recovery)
		if err != nil {
			t.Fatal(err)
		}
		compactQueueCheckRecoveryStep(t, states, planned, recovery)
		if compactQueueValidate(planned, bounds) != nil {
			consumed = true
		}
		states = planned
		if done {
			if !consumed {
				t.Fatal("fixture did not consume the original admission budget")
			}
			return
		}
	}
	t.Fatal("retained recovery did not finish after consuming its reserved room")
}

func TestCompactQueueRecoveryExactFrontierRejects(t *testing.T) {
	states := []compactQueueState{{position: 6.01, stopBoundary: 12.01}, {position: 0, stopBoundary: 12.01}}
	bounds := compactQueueBounds{start: 0, frontier: 12.01}
	if err := compactQueueValidate(states, bounds); err != nil {
		t.Fatal("counterexample must satisfy the old symbolic admission guard", err)
	}
	before := append([]compactQueueState(nil), states...)
	if recovery, err := compactQueueRecoveryAdmission(states, bounds); err == nil || recovery.targets != nil {
		t.Fatal("exact-frontier target admitted without legal finite landing room")
	}
	if got, err := compactQueueRecoverablePlan(states, bounds, 0); err == nil || got != nil {
		t.Fatal("compact motion bypassed constructive admission")
	}
	if !reflect.DeepEqual(states, before) {
		t.Fatal("infeasible admission changed the caller's snapshot")
	}
}

func TestCompactQueueRecoveryKeepsAdmissionBudget(t *testing.T) {
	states := []compactQueueState{{position: 109, stopBoundary: 114}, {position: 100, stopBoundary: 114}}
	bounds := compactQueueBounds{start: 0, frontier: 114}
	// Destinations could fit, but the six-meter admission reserve does not.
	if recovery, err := compactQueueRecoveryAdmission(states, bounds); err == nil || recovery.targets != nil {
		t.Fatal("constructive recovery waived the approved admission reserve")
	}
}

func TestCompactQueueRecoveryOwnsLandingAllowance(t *testing.T) {
	states, bounds := compactQueueTestStates([]float64{0, 0}, 0, 10)
	targets, err := compactQueueRecoveryTargets(states, bounds)
	if err != nil {
		t.Fatal(err)
	}
	states[0].stopBoundary = targets[0]
	if _, admissionErr := compactQueueRecoveryAdmission(states, bounds); admissionErr == nil {
		t.Fatal("plain frontier alone granted unowned landing track")
	}
	states[0].stopBoundary = math.Nextafter(targets[0], math.Inf(1))
	recovery, err := compactQueueRecoveryAdmission(states, bounds)
	if err != nil {
		t.Fatal(err)
	}
	landing := recovery.landingSpeeds[0]
	if landing <= 0 || landing > 2.0/60 || landing*landing/4 > states[0].stopBoundary-targets[0] {
		t.Fatal("landing speed did not derive from its owned positive allowance")
	}
	compactQueueFinishRecovery(t, states, recovery)
}

func TestCompactQueueRecoverablePlanChecksNextState(t *testing.T) {
	states, bounds := compactQueueTestStates([]float64{0, 0}, 1, 1)
	targets, err := compactQueueRecoveryTargets(states, bounds)
	if err != nil {
		t.Fatal(err)
	}
	// The old tail is already done and needs no landing allowance. After compact
	// acceleration it requires recovery, whose target equals its owned boundary.
	states[1].stopBoundary = states[1].position + (2.0/60)*(1.0/60) + (2.0/60)*(2.0/60)/4
	got, err := compactQueuePlan(states, bounds, 2.0/60)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compactQueueRecoveryAdmission(states, bounds); err != nil {
		t.Fatal("fixture does not admit its old stopped snapshot", err)
	}
	if got[1].position+got[1].speed*got[1].speed/4 != states[1].stopBoundary || targets[1] != states[1].position {
		t.Fatal("fixture did not reach its unowned next recovery landing")
	}
	if got, err := compactQueueRecoverablePlan(states, bounds, 2.0/60); err == nil || got != nil {
		t.Fatal("compact plan failed to recheck constructive next-state recovery")
	}
}

func TestCompactQueueRecoveryRejectsRoundedAllowance(t *testing.T) {
	states, bounds := compactQueueTestStates([]float64{0, 0}, 0, 1)
	targets, err := compactQueueRecoveryTargets(states, bounds)
	if err != nil {
		t.Fatal(err)
	}
	bounds.frontier = math.Nextafter(targets[0], math.Inf(1))
	for i := range states {
		states[i].stopBoundary = bounds.frontier
	}
	recovery, err := compactQueueRecoveryAdmission(states, bounds)
	if err != nil {
		t.Fatal(err)
	}
	margin := bounds.frontier - targets[0]
	recovery.landingSpeeds[0] = math.Sqrt(4 * 1.25 * margin)
	landing := recovery.landingSpeeds[0]
	if landing*landing/4 <= margin || targets[0]+landing*landing/4 != bounds.frontier {
		t.Fatal("fixture did not hide excessive landing distance through addition rounding")
	}
	if planned, done, planErr := compactQueueRecoveryStep(states, recovery); planErr == nil || planned != nil || done {
		t.Fatal("saved landing proof exceeded its actual owned interval")
	}
}

func TestCompactQueueHoldingAnticipatesAndReleases(t *testing.T) {
	bounds := compactQueueBounds{start: 0, frontier: 100}
	state := compactQueueState{position: 60, speed: 2.5, stopBoundary: 100}
	lo, _ := compactQueueSpeedRange(state.speed)
	got, err := compactQueueHoldingStep(state, bounds, 4, lo)
	if err != nil || got.speed != lo || got.position != state.position+lo/60 {
		t.Fatal("valid anticipatory hold failed", got, err)
	}
	late := compactQueueState{position: 82, stopBoundary: 100}
	if planned, holdErr := compactQueueHoldingStep(late, bounds, 4, 0); holdErr == nil || planned != late {
		t.Fatal("exact future-capacity boundary did not reject unchanged")
	}
	late.position = 99
	if planned, holdErr := compactQueueHoldingStep(late, bounds, 4, 0); holdErr == nil || planned != late {
		t.Fatal("late hold changed a head without obtaining room")
	}
	// Releasing an unused hold changes no numeric rule and forms no certificate.
	released, err := compactQueueHoldingStep(late, bounds, 1, 2.0/60)
	if err != nil || released.position <= late.position || released.speed != 2.0/60 {
		t.Fatal("standalone release could not continue on owned ordinary track", err)
	}
	for _, capacity := range []int{0, 5} {
		if got, err := compactQueueHoldingStep(state, bounds, capacity, lo); err == nil || got != state {
			t.Fatal("invalid future capacity did not reject unchanged")
		}
	}
	for _, speed := range []float64{math.NaN(), math.Inf(1), 0, 2.6} {
		if got, err := compactQueueHoldingStep(state, bounds, 4, speed); err == nil || got != state {
			t.Fatal("invalid holding speed did not reject unchanged")
		}
	}
	near := compactQueueState{position: 100 - 18 - 1.0/4 - 0.001, speed: 1, stopBoundary: 100}
	if planned, holdErr := compactQueueHoldingStep(near, bounds, 4, 1+2.0/60); holdErr == nil || planned != near {
		t.Fatal("holding failed to preserve next-state future capacity")
	}
	if _, holdErr := compactQueueHoldingStep(near, bounds, 4, 1-2.0/60); holdErr != nil {
		t.Fatal("legal braking could not preserve a near-boundary future hold", holdErr)
	}
}

func TestCompactQueueSingletonRecoveryFinite(t *testing.T) {
	for _, speed := range []float64{0, 1e-12, 2.0 / 60, 1, 2.5} {
		state := compactQueueState{position: 100, speed: speed, stopBoundary: 100 + speed*speed/4}
		bounds := compactQueueBounds{start: 0, frontier: state.stopBoundary}
		recovery, err := compactQueueSingletonRecovery(state, bounds)
		if err != nil {
			t.Fatal(err)
		}
		final := compactQueueFinishRecovery(t, []compactQueueState{state}, recovery)
		if final[0].speed != 0 || final[0].position > state.stopBoundary {
			t.Fatal("singleton recovery passed its owned frontier")
		}
	}
}

func TestCompactQueueRecoveryRestoreImmutable(t *testing.T) {
	states, bounds := compactQueueTestStates([]float64{2.5, 2.5, 1, 0}, 0, 1)
	recovery, err := compactQueueRecoveryAdmission(states, bounds)
	if err != nil {
		t.Fatal(err)
	}
	for range 100 {
		states, _, err = compactQueueRecoveryStep(states, recovery)
		if err != nil {
			t.Fatal(err)
		}
	}
	stateCopy := append([]compactQueueState(nil), states...)
	recoveryCopy := compactQueueCopyRecovery(recovery)
	left := compactQueueFinishRecovery(t, states, recovery)
	right := compactQueueFinishRecovery(t, stateCopy, recoveryCopy)
	if !reflect.DeepEqual(left, right) || !reflect.DeepEqual(recovery, recoveryCopy) {
		t.Fatal("restored fixed recovery plan changed its result or certificate inputs")
	}
}

func TestCompactQueueRecoveryLandingThenStop(t *testing.T) {
	states := []compactQueueState{{position: 112.009877, stopBoundary: 113.01}, {position: 100, stopBoundary: 113.01}}
	recovery := compactQueueRecovery{
		bounds:        compactQueueBounds{start: 0, frontier: 113.01},
		targets:       []float64{112.01, 100},
		landingSpeeds: []float64{2.0 / 60, 0},
	}
	planned, done, err := compactQueueRecoveryStep(states, recovery)
	if err != nil || done || planned[0].position != recovery.targets[0] || planned[0].speed <= 0 {
		t.Fatal("finite landing lost its positive speed or ended the certificate early", err)
	}
	compactQueueCheckRecoveryStep(t, states, planned, recovery)
	stopped, done, err := compactQueueRecoveryStep(planned, recovery)
	if err != nil || !done || stopped[0].speed != 0 || stopped[0].position != planned[0].position {
		t.Fatal("landing did not end with a separate legal stopping tick", err)
	}
	compactQueueCheckRecoveryStep(t, planned, stopped, recovery)

	// Rounded position plateaus must still brake to zero. Maximizing a tiny
	// positive speed at the target would keep this certificate forever.
	planned[0].speed = 4.2632564145606006e-13
	stopped, done, err = compactQueueRecoveryStep(planned, recovery)
	if err != nil || !done || stopped[0].speed != 0 {
		t.Fatal("terminal rounded plateau did not brake to zero", err)
	}
}

func TestCompactQueueRecoveryInvalidInputsReject(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func([]compactQueueState, *compactQueueRecovery)
	}{
		{"target NaN", func(_ []compactQueueState, r *compactQueueRecovery) { r.targets[0] = math.NaN() }},
		{"target infinity", func(_ []compactQueueState, r *compactQueueRecovery) { r.targets[0] = math.Inf(1) }},
		{"frontier NaN", func(_ []compactQueueState, r *compactQueueRecovery) { r.bounds.frontier = math.NaN() }},
		{"malformed target count", func(_ []compactQueueState, r *compactQueueRecovery) { r.targets = r.targets[:1] }},
		{"malformed landing count", func(_ []compactQueueState, r *compactQueueRecovery) { r.landingSpeeds = nil }},
		{"lost ordinary target gap", func(_ []compactQueueState, r *compactQueueRecovery) { r.targets[0] = r.targets[1] + 12 }},
		{"landing NaN", func(_ []compactQueueState, r *compactQueueRecovery) { r.landingSpeeds[0] = math.NaN() }},
		{"landing negative", func(_ []compactQueueState, r *compactQueueRecovery) { r.landingSpeeds[0] = -1 }},
		{"landing above brake tick", func(_ []compactQueueState, r *compactQueueRecovery) { r.landingSpeeds[0] = 0.1 }},
		{"lost landing allowance", func(s []compactQueueState, r *compactQueueRecovery) { s[0].stopBoundary = r.targets[0] }},
		{"missing landing speed", func(_ []compactQueueState, r *compactQueueRecovery) { r.landingSpeeds[0] = 0 }},
		{"passed target", func(s []compactQueueState, r *compactQueueRecovery) { s[0].position = r.targets[0] + 1 }},
		{"cannot brake to target", func(s []compactQueueState, r *compactQueueRecovery) {
			r.bounds.frontier += 10
			for i := range s {
				s[i].stopBoundary = r.bounds.frontier
			}
			s[0].position = r.targets[0] - 0.1
			s[0].speed = 2.5
		}},
		{"follower cannot brake to target", func(s []compactQueueState, r *compactQueueRecovery) {
			s[0].position = 107
			s[1].position = r.targets[1] - 0.1
			s[1].speed = 1
			r.landingSpeeds[1] = 2.0 / 60
		}},
		{"invalid speed", func(s []compactQueueState, _ *compactQueueRecovery) { s[0].speed = 2.6 }},
		{"speed NaN", func(s []compactQueueState, _ *compactQueueRecovery) { s[0].speed = math.NaN() }},
		{"position NaN", func(s []compactQueueState, _ *compactQueueRecovery) { s[0].position = math.NaN() }},
		{"owned boundary NaN", func(s []compactQueueState, _ *compactQueueRecovery) { s[0].stopBoundary = math.NaN() }},
		{"owned boundary outside frontier", func(s []compactQueueState, r *compactQueueRecovery) { s[0].stopBoundary = r.bounds.frontier + 1 }},
		{"follower owned boundary outside frontier", func(s []compactQueueState, r *compactQueueRecovery) { s[1].stopBoundary = r.bounds.frontier + 1 }},
		{"unsafe physical gap", func(s []compactQueueState, _ *compactQueueRecovery) { s[0].position = s[1].position + 6 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			states, bounds := compactQueueTestStates([]float64{0, 0}, 0, 1)
			recovery, err := compactQueueRecoveryAdmission(states, bounds)
			if err != nil {
				t.Fatal(err)
			}
			test.change(states, &recovery)
			before := append([]compactQueueState(nil), states...)
			beforeTargets := append([]float64(nil), recovery.targets...)
			beforeLanding := append([]float64(nil), recovery.landingSpeeds...)
			planned, done, err := compactQueueRecoveryStep(states, recovery)
			if err == nil || planned != nil || done {
				t.Fatal("invalid retained state produced a recovery plan")
			}
			if bound, boundErr := compactQueueRecoveryTickBound(states, recovery); boundErr == nil || bound != 0 {
				t.Fatal("invalid retained state produced a finite completion bound")
			}
			if !compactQueueSameBits(states, before) || !compactQueueFloatBitsEqual(recovery.targets, beforeTargets) ||
				!compactQueueFloatBitsEqual(recovery.landingSpeeds, beforeLanding) {
				t.Fatal("invalid retained-state rejection changed its inputs")
			}
		})
	}

	state := compactQueueState{position: 1e15, stopBoundary: 1e15 + 100}
	bounds := compactQueueBounds{start: 0, frontier: state.stopBoundary}
	if got, err := compactQueueHoldingStep(state, bounds, 4, 0); err == nil || got != state {
		t.Fatal("an unrepresentable low-speed step admitted future recovery")
	}

	for _, members := range []int{0, 5} {
		states := make([]compactQueueState, members)
		recovery := compactQueueRecovery{bounds: compactQueueBounds{start: 0, frontier: 200},
			targets: make([]float64, members), landingSpeeds: make([]float64, members)}
		for i := range states {
			states[i] = compactQueueState{position: float64(members-i) * 20, stopBoundary: 200}
			recovery.targets[i] = states[i].position
		}
		if planned, done, err := compactQueueRecoveryStep(states, recovery); err == nil || planned != nil || done {
			t.Fatal("otherwise valid illegal recovery group size was accepted")
		}
	}

	singleton := compactQueueState{position: 100, speed: 1, stopBoundary: 200}
	recovery, err := compactQueueSingletonRecovery(singleton, compactQueueBounds{start: 0, frontier: 200})
	if err != nil {
		t.Fatal(err)
	}
	recovery.targets[0]++
	if planned, done, err := compactQueueRecoveryStep([]compactQueueState{singleton}, recovery); err == nil || planned != nil || done {
		t.Fatal("an altered singleton braking endpoint was accepted")
	}
}

func compactQueueFinishRecovery(t *testing.T, states []compactQueueState, recovery compactQueueRecovery) []compactQueueState {
	t.Helper()
	initial := append([]compactQueueState(nil), states...)
	certificate := compactQueueCopyRecovery(recovery)
	bound, err := compactQueueRecoveryTickBound(states, recovery)
	if err != nil || bound == 0 {
		t.Fatal("recovery has no representable finite tick bound", err)
	}
	for ticks := range 2500 {
		before := append([]compactQueueState(nil), states...)
		planned, done, err := compactQueueRecoveryStep(states, recovery)
		if err != nil {
			t.Fatalf("recovery tick %d: %v; states=%+v targets=%v", ticks, err, states, recovery.targets)
		}
		if !reflect.DeepEqual(states, before) || !reflect.DeepEqual(recovery, certificate) {
			t.Fatal("recovery controller changed an input snapshot")
		}
		compactQueueCheckRecoveryStep(t, states, planned, recovery)
		states = planned
		if done {
			if uint64(ticks+1) > bound {
				t.Fatal("recovery exceeded its derived finite tick bound")
			}
			for i, state := range states {
				if state.speed != 0 || state.position != recovery.targets[i] || state.position < initial[i].position {
					t.Fatal("recovery completed before a forward stopped destination")
				}
				if i != 0 && states[i-1].position-state.position < 12.01 {
					t.Fatal("recovery completed before ordinary stopped spacing")
				}
			}
			return states
		}
	}
	t.Fatalf("recovery did not finish within the focused fixture bound: states=%+v targets=%v", states, recovery.targets)
	return nil
}

func compactQueueCheckRecoveryStep(t *testing.T, old, planned []compactQueueState, recovery compactQueueRecovery) {
	t.Helper()
	active := -1
	for i, state := range old {
		if state.position != recovery.targets[i] || state.speed != 0 {
			active = i
			break
		}
	}
	for i, state := range planned {
		if state.speed < 0 || state.speed > 2.5 || math.Abs(state.speed-old[i].speed) > 2.0/60+1e-15 {
			t.Fatal("recovery violated a numeric speed or braking bound")
		}
		if state.position != old[i].position+state.speed*(1.0/60) || state.position < old[i].position ||
			state.stopBoundary != old[i].stopBoundary || state.position > recovery.targets[i] {
			t.Fatal("recovery snapped, moved backward, or changed an owned boundary")
		}
		if state.position+state.speed*state.speed/4 > state.stopBoundary || state.stopBoundary > recovery.bounds.frontier {
			t.Fatal("recovery passed its owned stopping frontier")
		}
		if i != 0 {
			leader := planned[i-1]
			envelope := 6.01 + 0.5*state.speed + max(0, (state.speed*state.speed-leader.speed*leader.speed)/4)
			if leader.position-state.position < envelope-1e-13 {
				t.Fatal("recovery violated the independent moving envelope")
			}
		}
		if i > 1 && planned[i-2].position-state.position < 12 {
			t.Fatal("recovery violated unrelated/nonadjacent ordinary separation")
		}
	}
	if active != -1 && len(old) > 1 && old[active].position < recovery.targets[active] {
		travel := recovery.landingSpeeds[active] * (1.0 / 60)
		remaining := recovery.targets[active] - old[active].position
		if remaining >= travel && planned[active].position-old[active].position < travel/2 {
			t.Fatal("active recovery lost its proved minimum forward progress")
		}
		if remaining < travel && planned[active].position != recovery.targets[active] {
			t.Fatal("active recovery missed its finite final landing interval")
		}
	}
}

func compactQueueCopyRecovery(recovery compactQueueRecovery) compactQueueRecovery {
	recovery.targets = append([]float64(nil), recovery.targets...)
	recovery.landingSpeeds = append([]float64(nil), recovery.landingSpeeds...)
	return recovery
}

func compactQueueFloatBitsEqual(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if math.Float64bits(a[i]) != math.Float64bits(b[i]) {
			return false
		}
	}
	return true
}

// compactQueueRecoveryTickBound derives a conservative finite completion bound.
// A positive landing speed guarantees at least half its tick distance until the
// final landing interval. Each member then needs at most one full braking run.
func compactQueueRecoveryTickBound(states []compactQueueState, recovery compactQueueRecovery) (uint64, error) {
	if err := compactQueueRecoveryValidate(states, recovery); err != nil {
		return 0, err
	}
	brakingTicks := uint64(0)
	for speed := compactQueueSpeedLimit; speed > 0; {
		speed, _ = compactQueueSpeedRange(speed)
		brakingTicks++
	}
	bound := brakingTicks + 1
	for i, state := range states {
		bound += brakingTicks + 1
		if len(states) == 1 || state.position == recovery.targets[i] {
			continue
		}
		steps := math.Ceil(2 * (recovery.targets[i] - state.position) / (recovery.landingSpeeds[i] * compactQueueTickSeconds))
		if !finite(steps) || steps >= float64(math.MaxUint64-bound) {
			return 0, errors.New("compact queue: finite recovery bound cannot be represented")
		}
		bound += uint64(steps) + 1
	}
	return bound, nil
}
