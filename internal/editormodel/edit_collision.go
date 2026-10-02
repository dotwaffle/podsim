package editormodel

import "math"

type geometryPath struct {
	lane      map[string]any
	points    []geometryPoint
	low, high geometryPoint
}

type geometryConflict struct {
	lane, other string
	gap         float64
}

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

func (g geometryDraft) laneConflict(ids []string, separationGroups bool) *geometryConflict {
	paths, byID := []geometryPath{}, map[string]geometryPath{}
	for _, item := range items(g.network["Lanes"]) {
		lane := object(item)
		if lane == nil {
			continue
		}
		from, err := g.point(text(lane["From"]))
		if err != nil {
			continue
		}
		to, err := g.point(text(lane["To"]))
		if err != nil {
			continue
		}
		var control *geometryPoint
		if value := lane["Control"]; value != nil {
			if !finite(member(value, "X")) || !finite(member(value, "Y")) {
				continue
			}
			control = &geometryPoint{number(member(value, "X")), number(member(value, "Y"))}
		}
		path := geometryPath{lane: lane, points: geometryPolyline(from, to, control), low: geometryPoint{math.Inf(1), math.Inf(1)}, high: geometryPoint{math.Inf(-1), math.Inf(-1)}}
		for _, p := range path.points {
			path.low.X, path.low.Y = min(path.low.X, p.X), min(path.low.Y, p.Y)
			path.high.X, path.high.Y = max(path.high.X, p.X), max(path.high.Y, p.Y)
		}
		paths = append(paths, path)
		byID[text(lane["ID"])] = path
	}
	for _, id := range ids {
		path, found := byID[id]
		if !found {
			continue
		}
		for _, other := range paths {
			if other.lane["ID"] == id || path.low.X-other.high.X >= 12 || other.low.X-path.high.X >= 12 || path.low.Y-other.high.Y >= 12 || other.low.Y-path.high.Y >= 12 {
				continue
			}
			if path.lane["From"] == other.lane["From"] || path.lane["From"] == other.lane["To"] || path.lane["To"] == other.lane["From"] || path.lane["To"] == other.lane["To"] {
				continue
			}
			first, second := text(path.lane["SeparationGroup"]), text(other.lane["SeparationGroup"])
			if separationGroups && first != "" && second != "" && first != second {
				continue
			}
			if gap := geometryPathGap(path.points, other.points); gap < 12 {
				return &geometryConflict{id, text(other.lane["ID"]), gap}
			}
		}
	}
	return nil
}
