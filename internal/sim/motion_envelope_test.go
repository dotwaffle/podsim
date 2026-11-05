package sim

import (
	"math"
	"testing"
)

// seatEnvelope is the envelope of a seat-only class.
var seatEnvelope = motionEnvelopeFor(LegacyClass)

// distinctEnvelope has a different value for each rate, so that a test can
// tell which rate a site reads. The planned rate is the lowest, so a scan
// that reads another rate has a shorter reach. It is not the envelope of a
// class.
var distinctEnvelope = motionEnvelope{acceleration: 3, plannedBraking: 1, maximumBraking: 5}

// seatClasses are the classes that keep the one rate of the motion constant.
var seatClasses = []VehicleClass{"", LegacyClass, CompactClass, GroupClass}

// TestMotionEnvelopeValues pins each rate of each class.
func TestMotionEnvelopeValues(t *testing.T) {
	t.Parallel()
	for _, class := range seatClasses {
		want := motionEnvelope{acceleration: 2, plannedBraking: 2, maximumBraking: 2}
		if got := motionEnvelopeFor(class); got != want {
			t.Fatalf("class %q: envelope %+v, want %+v", class, got, want)
		}
	}
	want := motionEnvelope{acceleration: 1.3, plannedBraking: 0.8, maximumBraking: 1.15}
	if got := motionEnvelopeFor(ExpressClass); got != want {
		t.Fatalf("Express: envelope %+v, want %+v", got, want)
	}
}

func requireSameBits(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Float64bits(got) != math.Float64bits(want) {
		t.Fatalf("%s = %.17g, want %.17g", name, got, want)
	}
}

// TestMoveStepCeilingReadsAcceleration checks the speed rise of the ordinary
// and the coast step, far from each limit.
func TestMoveStepCeilingReadsAcceleration(t *testing.T) {
	t.Parallel()
	_, v := laneSpeedFixture([]float64{4000}, []float64{14})
	dt := 1.0 / TicksPerSecond
	want := 3 + distinctEnvelope.acceleration*dt
	requireSameBits(t, "ordinary", ordinaryMoveStep(&v.blocks, distinctEnvelope, 0, 10, 3, 4000).speed, want)
	requireSameBits(t, "coast", coastMoveStep(&v.blocks, distinctEnvelope, 0, 10, 3, 4000, math.Inf(1)).speed, want)
}

// TestMoveStepSafeRootReadsMaximumBraking checks each step toward a near
// limit, where the safe root binds the command.
func TestMoveStepSafeRootReadsMaximumBraking(t *testing.T) {
	t.Parallel()
	_, v := laneSpeedFixture([]float64{4000}, []float64{14})
	dt := 1.0 / TicksPerSecond
	m := distinctEnvelope.maximumBraking
	want := math.Sqrt(m*m*dt*dt+2*m*5) - m*dt
	const speed = 10.0
	requireSameBits(t, "ordinary", ordinaryMoveStep(&v.blocks, distinctEnvelope, 0, 10, speed, 15).commandedSpeed, want)
	requireSameBits(t, "coast", coastMoveStep(&v.blocks, distinctEnvelope, 0, 10, speed, 15, math.Inf(1)).commandedSpeed, want)
	requireSameBits(t, "fault", faultMoveStep(&v.blocks, distinctEnvelope, 0, 10, speed, 4000, 15).commandedSpeed, want)
}

// TestSpeedBeforeLaneReadsPlannedBraking checks the reach of the scan and the
// speed that it allows before a lower limit.
func TestSpeedBeforeLaneReadsPlannedBraking(t *testing.T) {
	t.Parallel()
	_, v := laneSpeedFixture([]float64{200, 100}, []float64{14, 2})
	dt := 1.0 / TicksPerSecond
	p := distinctEnvelope.plannedBraking
	const speed, limit, remaining = 14.0, 2.0, 50.0
	step := p * dt
	want := math.Sqrt(step*step+limit*limit+2*p*remaining) - step
	if want >= speed || remaining <= speed*speed/(2*seatEnvelope.plannedBraking)+speed*dt {
		t.Fatalf("the fixture does not bind: allowed %g", want)
	}
	requireSameBits(t, "inside the reach", v.blocks.speedBeforeLane(distinctEnvelope, 0, 200-remaining, speed), want)
	outside := 200 - (speed*speed/(2*p) + speed*dt) - 0.001
	requireSameBits(t, "outside the reach", v.blocks.speedBeforeLane(distinctEnvelope, 0, outside, speed), speed)
}

// TestMoveStepPassesEnvelopeToLaneScan checks that the kernel gives its
// envelope to the scan of the lower limits ahead.
func TestMoveStepPassesEnvelopeToLaneScan(t *testing.T) {
	t.Parallel()
	_, v := laneSpeedFixture([]float64{100, 100}, []float64{14, 2})
	// The planned floor does not bind at this speed.
	const speed = 5.0
	want := v.blocks.speedBeforeLane(distinctEnvelope, 0, 90, speed+distinctEnvelope.acceleration/TicksPerSecond)
	got := ordinaryMoveStep(&v.blocks, distinctEnvelope, 0, 90, speed, 200)
	requireSameBits(t, "command", got.commandedSpeed, want)
	if want == v.blocks.speedBeforeLane(seatEnvelope, 0, 90, speed+distinctEnvelope.acceleration/TicksPerSecond) {
		t.Fatal("the fixture does not tell the two envelopes apart")
	}
}

func TestEnvelopeStoppingReadsMaximumBraking(t *testing.T) {
	t.Parallel()
	const speed = 14.0
	stop := speed * speed / (2 * distinctEnvelope.maximumBraking)
	requireSameBits(t, "stoppingDistance", distinctEnvelope.stoppingDistance(speed), stop)
	requireSameBits(t, "reservationHorizon", distinctEnvelope.reservationHorizon(speed), stop+speed*reservationLookaheadSeconds)
}

// TestRouteSecondsReadsEnvelope checks the first term of the estimate with
// the acceleration, and the last term with the planned braking.
func TestRouteSecondsReadsEnvelope(t *testing.T) {
	t.Parallel()
	s, v := laneSpeedFixture([]float64{100, 50}, []float64{14, 7})
	base := 100.0/14 + 50.0/7
	route := v.Route
	motion := motionEstimate{speed: 4}
	want := base + 10.0*10.0/(2*distinctEnvelope.acceleration*14) + 7/(2*distinctEnvelope.plannedBraking)
	requireSameBits(t, "routeSeconds", s.routeSeconds(distinctEnvelope, route, motion), want)
	requireSameBits(t, "routeSecondsWith", s.routeSecondsWith(distinctEnvelope, route, []float64{100, 50}, motion), want)
}

// TestSeatEstimateHasFrozenBits compares the estimate of each class that has
// the seat-only rates with the expression before the envelope.
func TestSeatEstimateHasFrozenBits(t *testing.T) {
	t.Parallel()
	s, v := laneSpeedFixture([]float64{100, 50}, []float64{14, 7})
	for _, class := range seatClasses {
		for _, speed := range []float64{0, 0.1, 3, 13.999, 14, 20} {
			base := 100.0/14 + 50.0/7
			missing := math.Max(0, 14-speed)
			want := base + missing*missing/(2*acceleration*14) + 7.0/(2*acceleration)
			got := s.routeSeconds(motionEnvelopeFor(class), v.Route, motionEstimate{speed: speed})
			requireSameBits(t, string(class)+" estimate", got, want)
		}
	}
}

// TestSeatKernelHasFrozenBits compares each step of each class with the
// expressions before the envelope, over a grid that binds each limit.
func TestSeatKernelHasFrozenBits(t *testing.T) {
	t.Parallel()
	_, v := laneSpeedFixture([]float64{100, 100}, []float64{14, 2.5})
	dt := 1.0 / TicksPerSecond
	for _, class := range seatClasses {
		envelope := motionEnvelopeFor(class)
		for _, distance := range []float64{0, 1.0 / 3, 50, 90, 99.9} {
			for _, speed := range []float64{0, 1.0 / 7, 3, 13.99, 14} {
				for _, gap := range []float64{1e-6, 0.3, 1.0 / 3, 7, 50, 150} {
					limit := distance + gap
					available := math.Max(0, limit-distance)
					safe := math.Sqrt(acceleration*acceleration*dt*dt+2*acceleration*available) - acceleration*dt
					command := math.Min(speed+acceleration*dt, math.Min(14, math.Max(0, safe)))
					got := ordinaryMoveStep(&v.blocks, envelope, 0, distance, speed, limit)
					// The lane scan keeps its own frozen test, and it can only
					// lower the command.
					if got.commandedSpeed > command || got.commandedSpeed != frozenLaneScan(&v.blocks, 0, distance, command) {
						t.Fatalf("class %q %g m at %g m/s to %g: command %.17g from %.17g", class, distance, speed, limit, got.commandedSpeed, command)
					}
					requireSameBits(t, "stop", envelope.stoppingDistance(speed), speed*speed/(2*acceleration))
					requireSameBits(t, "horizon", envelope.reservationHorizon(speed), speed*speed/(2*acceleration)+speed*reservationLookaheadSeconds)
				}
			}
		}
	}
}

// frozenLaneScan is speedBeforeLane before the envelope.
func frozenLaneScan(l *blockList, current int, distance, speed float64) float64 {
	dt := 1.0 / TicksPerSecond
	reach := speed*speed/(2*acceleration) + speed*dt
	for lane := current + 1; lane < len(l.route); lane++ {
		remaining := l.lanes[lane].start - distance
		if remaining > reach {
			break
		}
		limit := l.route[lane].SpeedLimit
		if limit >= speed {
			continue
		}
		step := acceleration * dt
		safe := math.Sqrt(step*step+limit*limit+2*acceleration*math.Max(0, remaining)) - step
		speed = math.Min(speed, math.Max(limit, safe))
	}
	return speed
}
