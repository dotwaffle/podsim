package editormodel

import (
	"math"
)

func geometryPolyline(from, to geometryPoint, control *geometryPoint) []geometryPoint {
	if control == nil {
		return []geometryPoint{from, to}
	}
	points := make([]geometryPoint, 65)
	for index := range points {
		t := float64(index) / 64
		u := 1 - t
		points[index] = geometryPoint{u*u*from.X + 2*u*t*control.X + t*t*to.X, u*u*from.Y + 2*u*t*control.Y + t*t*to.Y}
	}
	return points
}

func geometryPointGap(at, a, b geometryPoint) float64 {
	x, y := b.X-a.X, b.Y-a.Y
	size := x*x + y*y
	t := 0.0
	if size != 0 {
		t = max(0, min(1, ((at.X-a.X)*x+(at.Y-a.Y)*y)/size))
	}
	return math.Hypot(at.X-a.X-t*x, at.Y-a.Y-t*y)
}

func geometrySegmentGap(a, b, c, d geometryPoint) float64 {
	side := func(p, q, r geometryPoint) float64 {
		value := (q.X-p.X)*(r.Y-p.Y) - (q.Y-p.Y)*(r.X-p.X)
		if value < 0 {
			return -1
		}
		if value > 0 {
			return 1
		}
		return 0
	}
	if side(a, b, c)*side(a, b, d) < 0 && side(c, d, a)*side(c, d, b) < 0 {
		return 0
	}
	return min(geometryPointGap(a, c, d), geometryPointGap(b, c, d), geometryPointGap(c, a, b), geometryPointGap(d, a, b))
}

func geometryPathGap(first, second []geometryPoint) float64 {
	gap := math.Inf(1)
	for i := 1; i < len(first); i++ {
		for j := 1; j < len(second); j++ {
			gap = min(gap, geometrySegmentGap(first[i-1], first[i], second[j-1], second[j]))
		}
	}
	return gap
}
