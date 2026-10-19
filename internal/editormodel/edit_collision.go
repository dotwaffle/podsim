package editormodel

import "math"

type geometryPath struct {
	lane      map[string]any
	points    []geometryPoint
	low, high geometryPoint
	length    float64
}

type geometryConflict struct {
	lane, other  string
	gap, minimum float64
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

// geometryPolylinePiece returns the part of a polyline between two path
// distances.
func geometryPolylinePiece(points []geometryPoint, from, to float64) []geometryPoint {
	piece := make([]geometryPoint, 0, len(points)+1)
	distance := 0.0
	for index := 1; index < len(points); index++ {
		a, b := points[index-1], points[index]
		length := math.Hypot(b.X-a.X, b.Y-a.Y)
		at := func(target float64) geometryPoint {
			if length == 0 {
				return a
			}
			t := min(1, max(0, (target-distance)/length))
			return geometryPoint{a.X + t*(b.X-a.X), a.Y + t*(b.Y-a.Y)}
		}
		if len(piece) == 0 && from <= distance+length {
			piece = append(piece, at(from))
		}
		if len(piece) > 0 {
			if to <= distance+length {
				return append(piece, at(to))
			}
			piece = append(piece, b)
		}
		distance += length
	}
	if len(piece) == 0 {
		piece = append(piece, points[len(points)-1])
	}
	return append(piece, points[len(points)-1])
}

// pairConflict returns the first piece of two paths that comes closer than
// its clearance, as the bank audit of the server judges it.
func (c draftClearance) pairConflict(path, other geometryPath) (gap, minimum float64, conflict bool) {
	pieces := c.pairPieces(path.lane, other.lane, path.length, other.length)
	for _, piece := range pieces {
		a, b := path.points, other.points
		if len(pieces) > 1 {
			a, b = geometryPolylinePiece(a, piece.First[0], piece.First[1]), geometryPolylinePiece(b, piece.Second[0], piece.Second[1])
		}
		if gap := geometryPathGap(a, b); gap < piece.Clearance {
			return gap, piece.Clearance, true
		}
	}
	return 0, 0, false
}

func (g geometryDraft) laneConflict(ids []string, separationGroups bool) *geometryConflict {
	clearance := newDraftClearance(g.network)
	paths, byID := []geometryPath{}, map[string]geometryPath{}
	for _, item := range items(g.network["lanes"]) {
		lane := object(item)
		if lane == nil {
			continue
		}
		from, err := g.point(text(lane["from"]))
		if err != nil {
			continue
		}
		to, err := g.point(text(lane["to"]))
		if err != nil {
			continue
		}
		var control *geometryPoint
		if value := lane["control"]; value != nil {
			if !finite(member(value, "x")) || !finite(member(value, "y")) {
				continue
			}
			control = &geometryPoint{number(member(value, "x")), number(member(value, "y"))}
		}
		path := geometryPath{lane: lane, points: geometryPolyline(from, to, control), low: geometryPoint{math.Inf(1), math.Inf(1)}, high: geometryPoint{math.Inf(-1), math.Inf(-1)}}
		for index, p := range path.points {
			path.low.X, path.low.Y = min(path.low.X, p.X), min(path.low.Y, p.Y)
			path.high.X, path.high.Y = max(path.high.X, p.X), max(path.high.Y, p.Y)
			if index > 0 {
				path.length += math.Hypot(p.X-path.points[index-1].X, p.Y-path.points[index-1].Y)
			}
		}
		paths = append(paths, path)
		byID[text(lane["id"])] = path
	}
	for _, id := range ids {
		path, found := byID[id]
		if !found {
			continue
		}
		for _, other := range paths {
			// No piece needs more than the class clearance of the pair.
			minimum := max(draftClassMinimum(path.lane), draftClassMinimum(other.lane)) / 2
			if other.lane["id"] == id || path.low.X-other.high.X >= minimum || other.low.X-path.high.X >= minimum || path.low.Y-other.high.Y >= minimum || other.low.Y-path.high.Y >= minimum {
				continue
			}
			if path.lane["from"] == other.lane["from"] || path.lane["from"] == other.lane["to"] || path.lane["to"] == other.lane["from"] || path.lane["to"] == other.lane["to"] {
				continue
			}
			first, second := text(path.lane["separationGroup"]), text(other.lane["separationGroup"])
			if separationGroups && first != "" && second != "" && first != second {
				continue
			}
			// The lanes of one maneuver group never move at once, and the
			// server certifies them against the parked bays.
			if group, grouped := clearance.grouped[id]; grouped {
				if otherGroup, otherGrouped := clearance.grouped[text(other.lane["id"])]; otherGrouped && otherGroup == group {
					continue
				}
			}
			if gap, minimum, conflict := clearance.pairConflict(path, other); conflict {
				return &geometryConflict{id, text(other.lane["id"]), gap, minimum}
			}
		}
	}
	return nil
}
