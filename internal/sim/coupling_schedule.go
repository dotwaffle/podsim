package sim

import (
	"math"
	"math/bits"
)

// A schedule stores integer prefix weights, not one value per elapsed tick.
// Its distance grid makes each published Euler addition exact in float64.
type couplingSchedule struct {
	startUnits, distanceUnits uint64
	quantum                   float64
	ramp, cap, rampTicks      uint64
	ticks, area               uint64
	acceleration, speedCap    float64
}

type couplingScheduleInput struct {
	Start, End, Quantum, Acceleration, SpeedCap float64
}

func prepareCouplingSchedule(input couplingScheduleInput) (couplingSchedule, error) {
	s := couplingSchedule{quantum: input.Quantum, acceleration: input.Acceleration, speedCap: input.SpeedCap}
	if !finite(input.Start) || !finite(input.End) || input.Start < 0 || input.End <= input.Start ||
		!finite(input.Quantum) || input.Quantum <= 0 || !finite(input.Acceleration) || input.Acceleration <= 0 || !finite(input.SpeedCap) || input.SpeedCap <= 0 {
		return s, couplingDenied("invalid finite motion schedule")
	}
	_, exponent := math.Frexp(input.Quantum)
	if math.Ldexp(1, exponent-1) != input.Quantum {
		return s, couplingDenied("motion distance grid is not a power of two")
	}
	start, ok := couplingGridUnits(input.Start, input.Quantum)
	if !ok {
		return s, couplingDenied("motion start is not representable on its distance grid")
	}
	end, ok := couplingGridUnits(input.End, input.Quantum)
	if !ok || end <= start {
		return s, couplingDenied("motion target is not representable on its distance grid")
	}
	s.startUnits, s.distanceUnits = start, end-start
	step := 60 * input.Quantum
	// Two grid units cover the change caused by rounding each prefix down.
	ramp := math.Floor((input.Acceleration/TicksPerSecond)/step) - 2
	capUnits := math.Min(math.Floor(input.SpeedCap/step)-1, float64((uint64(1)<<53)/15))
	if !finite(step) || step <= 0 || ramp < 1 || capUnits < 1 {
		return s, couplingDenied("motion speed or acceleration is not representable")
	}
	s.cap = uint64(capUnits)
	s.ramp = uint64(math.Min(ramp, capUnits))
	if couplingGridSpeed(s.ramp+2, s.quantum) > input.Acceleration/TicksPerSecond || couplingGridSpeed(s.cap, s.quantum) > input.SpeedCap {
		return s, couplingDenied("rounded motion coefficients exceed profile bounds")
	}
	s.rampTicks = (s.cap - 1) / s.ramp
	rampArea, ok := couplingProduct(s.rampTicks, s.rampTicks+1)
	if !ok {
		return s, couplingDenied("motion ramp prefix overflows")
	}
	rampArea, ok = couplingProduct(rampArea, s.ramp)
	if !ok || s.rampTicks > (math.MaxInt64-2)/2 {
		return s, couplingDenied("motion duration or ramp area overflows")
	}
	plateau := uint64(1)
	if s.distanceUnits > rampArea {
		remaining := s.distanceUnits - rampArea
		plateau = remaining / s.cap
		if remaining%s.cap != 0 {
			plateau++
		}
	}
	if plateau > math.MaxInt64-2*s.rampTicks-1 {
		return s, couplingDenied("motion duration cannot fit its tick representation")
	}
	s.ticks = 2*s.rampTicks + 1 + plateau
	flatArea, ok := couplingProduct(s.cap, plateau)
	if !ok || flatArea > math.MaxUint64-rampArea {
		return s, couplingDenied("motion prefix area overflows")
	}
	s.area = rampArea + flatArea
	if s.area < s.distanceUnits {
		return s, couplingDenied("motion schedule area is too short")
	}
	return s, nil
}

func couplingGridUnits(distance, quantum float64) (uint64, bool) {
	units := distance / quantum
	if !finite(units) || units < 0 || units > 1<<53 || units != math.Trunc(units) || units*quantum != distance {
		return 0, false
	}
	return uint64(units), true
}

func couplingGridSpeed(units uint64, quantum float64) float64 {
	return float64(units*15) * (4 * quantum)
}

func couplingProduct(a, b uint64) (uint64, bool) {
	high, low := bits.Mul64(a, b)
	return low, high == 0
}

func (s couplingSchedule) prefix(index uint64) uint64 {
	if index <= s.rampTicks {
		return s.ramp * (index * (index + 1) / 2)
	}
	if index >= s.ticks-s.rampTicks-1 {
		remaining := s.ticks - index
		return s.area - s.ramp*(remaining*(remaining-1)/2)
	}
	return s.ramp*(s.rampTicks*(s.rampTicks+1)/2) + s.cap*(index-s.rampTicks)
}

func (s couplingSchedule) traveled(index uint64) uint64 {
	high, low := bits.Mul64(s.distanceUnits, s.prefix(index))
	quotient, _ := bits.Div64(high, low, s.area)
	return quotient
}

func (s couplingSchedule) sample(index uint64) (distance, speed float64, err error) {
	if s.area == 0 || index > s.ticks {
		return 0, 0, couplingDenied("invalid motion schedule cursor")
	}
	traveled := s.traveled(index)
	distance = float64(s.startUnits+traveled) * s.quantum
	if index != 0 {
		units := traveled - s.traveled(index-1)
		if units > (uint64(1)<<53)/15 {
			return 0, 0, couplingDenied("motion speed product cannot be represented exactly")
		}
		speed = couplingGridSpeed(units, s.quantum)
	}
	if !finite(distance) || !finite(speed) || speed > s.speedCap {
		return 0, 0, couplingDenied("motion sample exceeds its finite speed certificate")
	}
	return distance, speed, nil
}

func couplingMotionQuantum(distances ...float64) (float64, error) {
	bound := Clearance
	for _, distance := range distances {
		if !finite(distance) || distance < 0 {
			return 0, couplingDenied("invalid motion grid extent")
		}
		bound = max(bound, distance)
	}
	quantum := math.Nextafter(bound, math.Inf(1)) - bound
	if !finite(quantum) || quantum <= 0 {
		return 0, couplingDenied("motion grid extent cannot be represented")
	}
	return quantum, nil
}
