package sim

import (
	"math"
	"math/rand/v2"
	"reflect"
	"testing"
)

func TestCompactQueueProfile(t *testing.T) {
	if compactQueueBodyLength != 4 || compactQueueStandstillGap != 6.01 ||
		compactQueueOrdinaryGap != 12.01 || compactQueueRecoveryPerPair != 6 ||
		compactQueueReactionSeconds != 0.5 || compactQueueSpeedLimit != 2.5 || compactQueueMaxMembers != 4 {
		t.Fatal("compact-v1 numeric profile does not match the approved constants")
	}
	if compactQueueAcceleration != acceleration || compactQueueTickSeconds != 1.0/TicksPerSecond ||
		compactQueueOrdinaryGap != Clearance+platoonMargin {
		t.Fatal("compact-v1 numeric profile does not match the approved simulator model")
	}
	for _, test := range []struct {
		name             string
		follower, leader float64
		want             float64
	}{
		{"stopped", 0, 0, 6.01},
		{"equal nine km/h", 2.5, 2.5, 7.26},
		{"closing nine km/h", 2.5, 0, 8.8225},
		{"leader faster", 0, 2.5, 6.01},
		{"relative braking", 2, 1, 7.76},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := compactQueueEnvelope(test.follower, test.leader); math.Abs(got-test.want) > 1e-14 {
				t.Fatalf("envelope = %.17g, want %.17g", got, test.want)
			}
		})
	}
}

func TestCompactQueueDiscreteFeasibility(t *testing.T) {
	// Include each side of the final braking tick, the relative-braking kink,
	// the speed cap, and a dense speed grid. Near-tight gaps expose a controller
	// that checks old gaps but fails to check the next positions and speeds.
	speeds := []float64{0, 1e-12, math.Nextafter(2.0/60, 0), 2.0 / 60,
		math.Nextafter(2.0/60, 1), 2.5 - 1e-12, 2.5}
	for i := range 76 {
		speeds = append(speeds, float64(i)/30)
	}
	for _, leaderSpeed := range speeds {
		for _, followerSpeed := range speeds {
			states, bounds := compactQueueTestStates([]float64{leaderSpeed, followerSpeed}, 1e-8, 100)
			lo, hi := compactQueueSpeedRange(leaderSpeed)
			for _, headSpeed := range []float64{lo, (lo + hi) / 2, hi} {
				before := append([]compactQueueState(nil), states...)
				got, err := compactQueuePlan(states, bounds, headSpeed)
				if err != nil {
					t.Fatalf("leader %g, follower %g, next head %g: %v", leaderSpeed, followerSpeed, headSpeed, err)
				}
				compactQueueCheckStep(t, before, got, bounds)
				if !reflect.DeepEqual(states, before) {
					t.Fatal("planner changed its input")
				}
				if got[0].speed != headSpeed {
					t.Fatal("planner clipped the requested head speed")
				}
				_, upper := compactQueueSpeedRange(followerSpeed)
				// A larger representable speed must be infeasible, unless the
				// follower already reached its one-tick upper speed bound.
				larger := math.Nextafter(got[1].speed, math.Inf(1))
				if larger <= upper && compactQueueFits(compactQueueStep(before[1], larger), bounds, &got[0]) {
					t.Fatalf("follower speed %g is not maximal; %g also fits", got[1].speed, larger)
				}
			}
		}
	}
}

func TestCompactQueueOwnedStopAndRecoveryBounds(t *testing.T) {
	states, bounds := compactQueueTestStates([]float64{2.5, 2.5, 2.5, 2.5}, 1e-8, 10)
	for i := range states {
		states[i].stopBoundary = states[i].position + compactQueueStoppingDistance(states[i].speed)
	}
	lo, hi := compactQueueSpeedRange(states[0].speed)
	if got, err := compactQueuePlan(states, bounds, hi); err == nil || got != nil {
		t.Fatal("head acceleration exceeded its stopping boundary")
	}
	got, err := compactQueuePlan(states, bounds, lo)
	if err != nil {
		t.Fatal(err)
	}
	compactQueueCheckStep(t, states, got, bounds)
	for i, state := range got {
		if state.speed > states[i].speed {
			t.Fatal("a member accelerated past its exact owned stopping bound")
		}
	}

	// Recovery room itself is a head-motion constraint, even with more owned
	// track. No extra reservation can waive the plain entry frontier.
	states, bounds = compactQueueTestStates([]float64{2.5, 2.5, 2.5, 2.5}, 1e-8, 0)
	if got, err := compactQueuePlan(states, bounds, hi); err == nil || got != nil {
		t.Fatal("head acceleration consumed future recovery room")
	}
	if _, err := compactQueuePlan(states, bounds, lo); err != nil {
		t.Fatal(err)
	}
}

func TestCompactQueueRepeatedLeaderBraking(t *testing.T) {
	for _, initial := range [][]float64{{2.5, 2.5, 2.5, 2.5}, {0, 2.5, 1, 2}, {2.5, 0, 2.5, 0}} {
		states, bounds := compactQueueTestStates(initial, 1e-8, 0)
		for range 200 {
			headSpeed, _ := compactQueueSpeedRange(states[0].speed)
			got, err := compactQueuePlan(states, bounds, headSpeed)
			if err != nil {
				t.Fatalf("initial speeds %v: %v", initial, err)
			}
			compactQueueCheckStep(t, states, got, bounds)
			states = got
		}
		if states[0].speed != 0 {
			t.Fatal("head did not finish legal maximum braking")
		}
	}
}

func TestCompactQueueRecoveryTargets(t *testing.T) {
	for members := 2; members <= compactQueueMaxMembers; members++ {
		states, bounds := compactQueueTestStates(make([]float64, members), 1e-8, 0)
		before := append([]compactQueueState(nil), states...)
		targets, err := compactQueueRecoveryTargets(states, bounds)
		if err != nil {
			t.Fatal(err)
		}
		compactQueueCheckTargets(t, states, bounds, targets)
		wantHead := states[len(states)-1].position + compactQueueOrdinaryGap*float64(members-1)
		if math.Abs(targets[0]-wantHead) > 1e-12 {
			t.Fatalf("head target %g, want %g", targets[0], wantHead)
		}
		if !reflect.DeepEqual(states, before) {
			t.Fatal("target construction changed its input")
		}
	}

	// Random unequal speeds and close gaps exercise the stopping-point proof.
	random := rand.New(rand.NewPCG(47, 113))
	for range 5000 {
		members := 2 + random.IntN(3)
		speeds := make([]float64, members)
		for i := range speeds {
			speeds[i] = random.Float64() * compactQueueSpeedLimit
		}
		states, bounds := compactQueueTestStates(speeds, random.Float64()*10+1e-8, 1e-8)
		targets, err := compactQueueRecoveryTargets(states, bounds)
		if err != nil {
			t.Fatal(err)
		}
		compactQueueCheckTargets(t, states, bounds, targets)
		headSpeed, _ := compactQueueSpeedRange(states[0].speed)
		planned, err := compactQueuePlan(states, bounds, headSpeed)
		if err != nil {
			t.Fatal(err)
		}
		compactQueueCheckStep(t, states, planned, bounds)
		plannedTargets, err := compactQueueRecoveryTargets(planned, bounds)
		if err != nil {
			t.Fatal(err)
		}
		compactQueueCheckTargets(t, planned, bounds, plannedTargets)
	}
}

func TestCompactQueueRecoveryRoundingRejects(t *testing.T) {
	// Every old gap fits and the nominal 18-meter budget fits exactly. Rounding
	// each ordinary target upward needs one more ULP, so admission must reject.
	positions := []float64{29.326107670288884, 23.316107670288883, 17.30610767028888, 11.29610767028888}
	bounds := compactQueueBounds{start: 0, frontier: 47.326107670288884}
	states := make([]compactQueueState, len(positions))
	for i, position := range positions {
		states[i] = compactQueueState{position: position, stopBoundary: bounds.frontier}
		if i != 0 && positions[i-1]-position < 6.01 {
			t.Fatal("rounding fixture has an invalid old gap")
		}
	}
	if !compactQueueRecoveryFits(states[0], len(states), bounds) {
		t.Fatal("rounding fixture does not fit the nominal recovery budget")
	}
	before := append([]compactQueueState(nil), states...)
	if got, err := compactQueuePlan(states, bounds, 0); err == nil || got != nil {
		t.Fatal("nominal budget admitted a state with no rounded recovery targets")
	}
	if got, err := compactQueueRecoveryTargets(states, bounds); err == nil || got != nil {
		t.Fatal("recovery targets crossed the exact plain frontier")
	}
	if !reflect.DeepEqual(states, before) {
		t.Fatal("rounding rejection changed its input")
	}
}

func TestCompactQueueInvalidInputsRejectUnchanged(t *testing.T) {
	tests := []struct {
		name   string
		change func(*[]compactQueueState, *compactQueueBounds, *float64)
	}{
		{"empty group", func(s *[]compactQueueState, _ *compactQueueBounds, _ *float64) { *s = nil }},
		{"singleton", func(s *[]compactQueueState, _ *compactQueueBounds, _ *float64) { *s = (*s)[:1] }},
		{"oversized group", func(s *[]compactQueueState, b *compactQueueBounds, _ *float64) {
			*s, *b = compactQueueTestStates([]float64{1, 1, 1, 1, 1}, 1e-8, 10)
		}},
		{"position NaN", func(s *[]compactQueueState, _ *compactQueueBounds, _ *float64) { (*s)[1].position = math.NaN() }},
		{"position infinity", func(s *[]compactQueueState, _ *compactQueueBounds, _ *float64) { (*s)[0].position = math.Inf(1) }},
		{"speed NaN", func(s *[]compactQueueState, _ *compactQueueBounds, _ *float64) { (*s)[1].speed = math.NaN() }},
		{"speed infinity", func(s *[]compactQueueState, _ *compactQueueBounds, _ *float64) { (*s)[1].speed = math.Inf(1) }},
		{"negative speed", func(s *[]compactQueueState, _ *compactQueueBounds, _ *float64) { (*s)[1].speed = -1e-12 }},
		{"speed above cap", func(s *[]compactQueueState, _ *compactQueueBounds, _ *float64) {
			(*s)[1].speed = math.Nextafter(2.5, 3)
		}},
		{"boundary NaN", func(s *[]compactQueueState, _ *compactQueueBounds, _ *float64) { (*s)[0].stopBoundary = math.NaN() }},
		{"boundary infinity", func(s *[]compactQueueState, _ *compactQueueBounds, _ *float64) { (*s)[1].stopBoundary = math.Inf(1) }},
		{"start NaN", func(_ *[]compactQueueState, b *compactQueueBounds, _ *float64) { b.start = math.NaN() }},
		{"frontier infinity", func(_ *[]compactQueueState, b *compactQueueBounds, _ *float64) { b.frontier = math.Inf(1) }},
		{"reversed region", func(_ *[]compactQueueState, b *compactQueueBounds, _ *float64) { b.start = b.frontier + 1 }},
		{"behind region", func(s *[]compactQueueState, b *compactQueueBounds, _ *float64) { b.start = (*s)[1].position + 1e-8 }},
		{"outside region", func(s *[]compactQueueState, b *compactQueueBounds, _ *float64) {
			(*s)[0].stopBoundary = b.frontier + 1e-8
		}},
		{"unowned stopping distance", func(s *[]compactQueueState, _ *compactQueueBounds, _ *float64) {
			(*s)[1].stopBoundary = (*s)[1].position
		}},
		{"reversed pods", func(s *[]compactQueueState, _ *compactQueueBounds, _ *float64) {
			(*s)[1].position = (*s)[0].position + 1
		}},
		{"unsafe old moving gap", func(s *[]compactQueueState, _ *compactQueueBounds, _ *float64) {
			(*s)[1].position = (*s)[0].position - compactQueueStandstillGap
		}},
		{"missing recovery room", func(s *[]compactQueueState, b *compactQueueBounds, _ *float64) {
			b.frontier = (*s)[0].position + compactQueueStoppingDistance((*s)[0].speed) + compactQueueRecoveryPerPair - 1e-8
			for i := range *s {
				(*s)[i].stopBoundary = b.frontier
			}
		}},
		{"head speed NaN", func(_ *[]compactQueueState, _ *compactQueueBounds, h *float64) { *h = math.NaN() }},
		{"head speed infinity", func(_ *[]compactQueueState, _ *compactQueueBounds, h *float64) { *h = math.Inf(1) }},
		{"head instant stop", func(s *[]compactQueueState, _ *compactQueueBounds, h *float64) {
			(*s)[1].speed = 0
			*h = 0
		}},
		{"head excess acceleration", func(_ *[]compactQueueState, _ *compactQueueBounds, h *float64) { *h = 1 + 2.0/60 + 1e-8 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			states, bounds := compactQueueTestStates([]float64{1, 1}, 1e-8, 10)
			headSpeed := 1.0
			test.change(&states, &bounds, &headSpeed)
			before := append([]compactQueueState(nil), states...)
			got, err := compactQueuePlan(states, bounds, headSpeed)
			if err == nil || got != nil {
				t.Fatal("invalid input produced a plan")
			}
			// Compare float bits so invalid NaNs can still be checked for mutation.
			if !compactQueueSameBits(states, before) {
				t.Fatal("rejection changed the caller's input")
			}
			if test.name != "head speed NaN" && test.name != "head speed infinity" &&
				test.name != "head instant stop" && test.name != "head excess acceleration" {
				if targets, err := compactQueueRecoveryTargets(states, bounds); err == nil || targets != nil {
					t.Fatal("invalid old state produced recovery targets")
				}
			}
		})
	}
}

func compactQueueTestStates(speeds []float64, gapExtra, recoveryExtra float64) ([]compactQueueState, compactQueueBounds) {
	states := make([]compactQueueState, len(speeds))
	position := 100.0
	for i := len(speeds) - 1; i >= 0; i-- {
		if i+1 < len(speeds) {
			position += compactQueueEnvelope(speeds[i+1], speeds[i]) + gapExtra
		}
		states[i] = compactQueueState{position: position, speed: speeds[i]}
	}
	frontier := states[0].position + compactQueueStoppingDistance(states[0].speed) +
		compactQueueRecoveryPerPair*float64(len(states)-1) + recoveryExtra
	for i := range states {
		states[i].stopBoundary = frontier
	}
	return states, compactQueueBounds{start: 0, frontier: frontier}
}

func compactQueueCheckStep(t *testing.T, old, got []compactQueueState, bounds compactQueueBounds) {
	t.Helper()
	if len(got) != len(old) {
		t.Fatal("planned member count changed")
	}
	for i, state := range got {
		lo, hi := compactQueueSpeedRange(old[i].speed)
		if state.speed < lo || state.speed > hi || state.speed < 0 || state.speed > 2.5 {
			t.Fatalf("member %d violated its speed interval: %g -> %g", i, old[i].speed, state.speed)
		}
		if math.Abs(state.speed-old[i].speed) > 2.0/60+1e-15 {
			t.Fatalf("member %d exceeded the independent two-meter acceleration/braking bound", i)
		}
		if state.position != old[i].position+state.speed*(1.0/60) || state.position < old[i].position || state.stopBoundary != old[i].stopBoundary {
			t.Fatalf("member %d violated semi-implicit movement or immutable ownership", i)
		}
		if state.position+state.speed*state.speed/4 > state.stopBoundary {
			t.Fatalf("member %d exceeded owned stopping distance", i)
		}
		if i != 0 {
			leader := got[i-1]
			want := 6.01 + 0.5*state.speed + max(0, (state.speed*state.speed-leader.speed*leader.speed)/4)
			if leader.position-state.position < want-1e-13 {
				t.Fatalf("member %d violated the independently calculated moving envelope", i)
			}
		}
		if i > 1 && got[i-2].position-state.position < 12 {
			t.Fatal("nonadjacent pods violated ordinary separation")
		}
	}
	if got[0].position+got[0].speed*got[0].speed/4+6*float64(len(got)-1) > bounds.frontier {
		t.Fatal("planned head consumed future recovery room")
	}
}

func compactQueueCheckTargets(t *testing.T, states []compactQueueState, bounds compactQueueBounds, targets []float64) {
	t.Helper()
	if len(targets) != len(states) {
		t.Fatal("target count changed")
	}
	for i, target := range targets {
		stop := states[i].position + states[i].speed*states[i].speed/4
		if target < stop || target > bounds.frontier || target < states[i].position {
			t.Fatal("recovery target violates forward stopping or plain frontier bounds")
		}
		if i != 0 && targets[i-1]-target < 12.01 {
			t.Fatal("recovery target gap is below ordinary stopped spacing")
		}
	}
	maxHead := states[0].position + states[0].speed*states[0].speed/4 + 6*float64(len(states)-1)
	if targets[0] > maxHead+1e-12 {
		t.Fatal("recovery targets exceeded the proved six-meter-per-pair budget")
	}
}

func compactQueueSameBits(a, b []compactQueueState) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if math.Float64bits(a[i].position) != math.Float64bits(b[i].position) ||
			math.Float64bits(a[i].speed) != math.Float64bits(b[i].speed) ||
			math.Float64bits(a[i].stopBoundary) != math.Float64bits(b[i].stopBoundary) {
			return false
		}
	}
	return true
}
