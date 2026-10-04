package sim

import (
	"errors"
	"math"
	"testing"
)

func TestCouplingScheduleStrictEuler(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name                                 string
		start, end, bound, acceleration, cap float64
	}{
		{"maneuver", 60, 67.5, 600, .5, .5},
		{"nonround_limit", 60, 67.5, 600, .5, .173123456789},
		{"nonround_coordinates", 130.123456789, 137.623456789, 137.623456789, .5, .5},
		{"long_connected", 1024.125, 16384.875, 20000, 2, 13.123456789},
		{"short_leg", 60, 60 + math.Ldexp(1, -30), 600, 2, 12},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			quantum, err := couplingMotionQuantum(test.start, test.end, test.bound)
			if err != nil {
				t.Fatal(err)
			}
			schedule, err := prepareCouplingSchedule(couplingScheduleInput{Start: test.start, End: test.end, Quantum: quantum, Acceleration: test.acceleration, SpeedCap: test.cap})
			if err != nil {
				t.Fatal(err)
			}
			if schedule.ticks > 100000 {
				t.Fatal("fixture exceeds declared oracle cap")
			}
			previousDistance, previousSpeed := test.start, 0.0
			for tick := uint64(1); tick <= schedule.ticks; tick++ {
				distance, speed, err := schedule.sample(tick)
				if err != nil {
					t.Fatal(err)
				}
				if distance != previousDistance+speed/60 || speed > test.cap || speed < 0 || math.Abs(speed-previousSpeed) > test.acceleration/60 {
					t.Fatalf("tick %d: x %.17g -> %.17g, v %.17g -> %.17g; Euler %.17g", tick, previousDistance, distance, previousSpeed, speed, previousDistance+speed/60)
				}
				if distance > test.end {
					t.Fatal("moving target overshoot")
				}
				if tick == schedule.ticks-1 && (distance != test.end || speed <= 0) {
					t.Fatal("last positive tick did not reach exact target")
				}
				previousDistance, previousSpeed = distance, speed
			}
			if previousDistance != test.end || previousSpeed != 0 {
				t.Fatal("terminal sample is not exact and stopped")
			}
			t.Logf("ticks=%d quantum=%.17g length=%.17g cap=%.17g", schedule.ticks, quantum, test.end-test.start, test.cap)
		})
	}
}

func TestCouplingScheduleRepresentationRefusal(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		input couplingScheduleInput
	}{
		{"exponent_boundary", couplingScheduleInput{Start: 123.123456789, End: 130.623456789, Quantum: math.Ldexp(1, -45), Acceleration: .5, SpeedCap: .5}},
		{"coarse_short_target", couplingScheduleInput{Start: 60, End: 60.000000001, Quantum: math.Ldexp(1, -43), Acceleration: 2, SpeedCap: 12}},
		{"off_grid", couplingScheduleInput{Start: math.Nextafter(60, 61), End: 67.5, Quantum: math.Ldexp(1, -40), Acceleration: .5, SpeedCap: .5}},
		{"tiny_limit", couplingScheduleInput{Start: 60, End: 67.5, Quantum: math.Ldexp(1, -43), Acceleration: .5, SpeedCap: math.SmallestNonzeroFloat64}},
		{"unrepresentable_acceleration", couplingScheduleInput{Start: 0, End: 1, Quantum: math.Ldexp(1, -52), Acceleration: 1e-12, SpeedCap: 1e-12}},
		{"nonfinite", couplingScheduleInput{Start: 0, End: math.Inf(1), Quantum: 1, Acceleration: .5, SpeedCap: .5}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := prepareCouplingSchedule(test.input)
			if !errors.Is(err, errCouplingReservationDenied) {
				t.Fatalf("missing typed representation refusal: %v", err)
			}
		})
	}
}
