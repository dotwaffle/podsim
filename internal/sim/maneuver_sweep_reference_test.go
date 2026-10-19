package sim

import (
	"math"
	"math/rand/v2"
	"testing"
)

// The reference sweep is the sweep before the obstacle and span values:
// each axis of a separation test computes the frames, the corners and
// the trigonometric values again.

func referenceSweepOne(step sweepStep, body sweepBody, obstacle sweepRectangle, options sweepOptions, visits *int) sweepStatus {
	for _, fraction := range [2]float64{0, 1} {
		if !referenceSweepSeparated(step, body, obstacle, fraction, fraction) {
			return sweepCollision
		}
	}
	if step.dx == 0 && step.dy == 0 && step.sweep == 0 {
		return sweepClear
	}
	type interval struct {
		lo, hi float64
		depth  int
	}
	var stack [sweepDepth + 1]interval
	stack[0] = interval{lo: 0, hi: 1}
	size, status, travel := 1, sweepClear, step.travel(body)
	for size > 0 {
		size--
		current := stack[size]
		if *visits >= options.visits {
			return max(status, sweepUnresolved)
		}
		*visits++
		mid := (current.lo + current.hi) / 2
		if !referenceSweepSeparated(step, body, obstacle, mid, mid) {
			return sweepCollision
		}
		if referenceSweepSeparated(step, body, obstacle, current.lo, current.hi) {
			continue
		}
		if travel*(current.hi-current.lo) <= options.tolerance || current.depth >= sweepDepth {
			status = sweepPotential
			continue
		}
		stack[size] = interval{lo: mid, hi: current.hi, depth: current.depth + 1}
		stack[size+1] = interval{lo: current.lo, hi: mid, depth: current.depth + 1}
		size += 2
	}
	return status
}

func referenceSweepSeparated(step sweepStep, body sweepBody, obstacle sweepRectangle, lo, hi float64) bool {
	mid := step.at((lo + hi) / 2)
	for _, heading := range [2]float64{mid.heading, obstacle.heading} {
		for _, axis := range sweepAxes(heading) {
			if intervalsDisjoint(referenceMovingProjection(step, body, axis, lo, hi), referenceStaticProjection(obstacle, axis)) {
				return true
			}
		}
	}
	return false
}

func referenceStaticProjection(r sweepRectangle, axis Point) projection {
	low, high := math.Inf(1), math.Inf(-1)
	frame := frameOf(sweepPose{x: r.x, y: r.y, heading: r.heading})
	for _, corner := range [4]Point{{-r.halfLength, -r.halfWidth}, {r.halfLength, -r.halfWidth}, {r.halfLength, r.halfWidth}, {-r.halfLength, r.halfWidth}} {
		v := dotPoint(frame.worldCorner(corner), axis)
		low, high = min(low, v), max(high, v)
	}
	return padded(low, high)
}

func referenceMovingProjection(step sweepStep, body sweepBody, axis Point, lo, hi float64) projection {
	low, high := math.Inf(1), math.Inf(-1)
	var first, last sweepFrame
	if step.sweep == 0 {
		first, last = frameOf(step.at(lo)), frameOf(step.at(hi))
	}
	for _, corner := range body.corners {
		if step.sweep == 0 {
			v0, v1 := dotPoint(first.worldCorner(corner), axis), dotPoint(last.worldCorner(corner), axis)
			low, high = min(low, v0, v1), max(high, v0, v1)
			continue
		}
		a := axis.X*corner.X + axis.Y*corner.Y
		b := axis.Y*corner.X - axis.X*corner.Y
		span := referenceTrigRange(a, b, step.start.heading+step.sweep*lo, step.start.heading+step.sweep*hi)
		constant := dotPoint(Point{X: step.start.x, Y: step.start.y}, axis)
		low, high = min(low, constant+span.low), max(high, constant+span.high)
	}
	return padded(low, high)
}

func referenceTrigRange(a, b, low, high float64) projection {
	if low > high {
		low, high = high, low
	}
	amplitude := math.Hypot(a, b)
	if high-low >= 2*math.Pi-sweepAnglePad {
		return projection{-amplitude, amplitude}
	}
	f := func(h float64) float64 { return a*math.Cos(h) + b*math.Sin(h) }
	v0, v1 := f(low), f(high)
	result := projection{min(v0, v1), max(v0, v1)}
	phase := math.Atan2(b, a)
	first := math.Ceil((low - sweepAnglePad - phase) / math.Pi)
	last := math.Floor((high + sweepAnglePad - phase) / math.Pi)
	for k := first; k <= last; k++ {
		value := amplitude
		if int(k)%2 != 0 {
			value = -amplitude
		}
		result.low, result.high = min(result.low, value), max(result.high, value)
	}
	return result
}

// checkSweepsMatchReference sweeps the steps against an obstacle step by
// step, and checks that each step has the result and the visit count of
// the reference. It returns the most severe result.
func checkSweepsMatchReference(t *testing.T, name string, steps []sweepStep, body sweepBody, obstacle sweepRectangle, options sweepOptions) sweepStatus {
	t.Helper()
	shape := newSweepObstacle(obstacle)
	visits, referenceVisits, worst := 0, 0, sweepClear
	for index, step := range steps {
		got := sweepOne(step, body, &shape, options, &visits)
		want := referenceSweepOne(step, body, obstacle, options, &referenceVisits)
		if got != want || visits != referenceVisits {
			t.Fatalf("%s step %d: result %d after %d visits, want %d after %d", name, index, got, visits, want, referenceVisits)
		}
		worst = max(worst, got)
	}
	return worst
}

// TestManeuverSweepsMatchReference checks that the sweep gives the result
// and the visit count of the reference for each step of each pair of a
// group lane and a berth of the compact station, also turned and moved as
// in a London station, and for random steps near random obstacles. It
// also checks the projections of random steps, as in the depot lateral
// enclosures, and of random obstacles.
func TestManeuverSweepsMatchReference(t *testing.T) {
	t.Parallel()
	compact, _ := loadCompactStation(t)
	results := make(map[sweepStatus]int)
	for _, angle := range []float64{0, 0.3, 2.1, -1.7} {
		network := rotateNetwork(compact, angle)
		for index := range network.Nodes {
			network.Nodes[index].Position.X += 51234.5678
			network.Nodes[index].Position.Y -= 18765.4321
		}
		nodes, err := network.validateNodes()
		if err != nil {
			t.Fatal(err)
		}
		maneuvers, err := newManeuverIndex(network, nodes)
		if err != nil {
			t.Fatal(err)
		}
		c := newManeuverCertifier(network, maneuvers, indexBerthKeys(network))
		for _, group := range maneuvers.groups {
			for _, id := range group.LaneIDs {
				for _, berth := range group.BerthIDs {
					for _, options := range []sweepOptions{certifyOptions, {tolerance: 1e-2, visits: 3}} {
						results[checkSweepsMatchReference(t, id+" "+berth, c.steps(id), maneuverBody, c.envelopes[berth], options)]++
					}
				}
			}
		}
	}
	rng := rand.New(rand.NewPCG(3, 5))
	for index := range 4000 {
		start := sweepPose{x: rng.Float64()*20 - 10, y: rng.Float64()*20 - 10, heading: rng.Float64()*4*math.Pi - 2*math.Pi}
		step := sweepStep{start: start}
		switch index % 3 {
		case 0:
			step.dx, step.dy = rng.Float64()*16-8, rng.Float64()*16-8
		case 1:
			step.sweep = rng.Float64()*4*math.Pi - 2*math.Pi
		}
		obstacle := parkedEnvelope(rng.Float64()*20-10, rng.Float64()*20-10, rng.Float64()*2*math.Pi)
		options := sweepOptions{tolerance: []float64{1e-3, 0.5}[index%2], visits: []int{8192, 6}[index%5/4]}
		results[checkSweepsMatchReference(t, "random", []sweepStep{step}, maneuverBody, obstacle, options)]++
		for _, axis := range sweepAxes(rng.Float64() * 2 * math.Pi) {
			lo := rng.Float64()
			hi := lo + rng.Float64()*(1-lo)
			if got, want := movingProjection(step, maneuverBody, axis, lo, hi), referenceMovingProjection(step, maneuverBody, axis, lo, hi); got != want {
				t.Fatalf("random projection %+v, want %+v", got, want)
			}
			if got, want := staticProjection(obstacle, axis), referenceStaticProjection(obstacle, axis); got != want {
				t.Fatalf("random obstacle projection %+v, want %+v", got, want)
			}
		}
	}
	for status := range sweepCollision + 1 {
		if results[status] == 0 {
			t.Errorf("no sweep with the result %d: %v", status, results)
		}
	}
}
