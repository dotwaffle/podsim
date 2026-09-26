package sim

import "sort"

type laneGeometry struct {
	segments []laneSegment
	end      Point
}

type laneSegment struct {
	from, to Point
	start    float64
	end      float64
}

// buildLaneGeometry returns the geometry of each lane by lane ID. Route
// blocks point to the values, so no code writes to them.
func buildLaneGeometry(network Network) map[string]*laneGeometry {
	storage := make([]laneGeometry, len(network.Lanes))
	geometry := make(map[string]*laneGeometry, len(network.Lanes))
	for laneIndex, lane := range network.Lanes {
		points := network.lanePoints(lane, make([]Point, 0, 65))
		indexed := laneGeometry{end: points[len(points)-1]}
		for index := 1; index < len(points); index++ {
			length := pointDistance(points[index-1], points[index])
			if length == 0 {
				continue
			}
			start := 0.0
			if len(indexed.segments) > 0 {
				start = indexed.segments[len(indexed.segments)-1].end
			}
			indexed.segments = append(indexed.segments, laneSegment{
				from: points[index-1], to: points[index], start: start, end: start + length,
			})
		}
		storage[laneIndex] = indexed
		geometry[lane.ID] = &storage[laneIndex]
	}
	return geometry
}

func (s *Simulation) position(lane Lane, distance float64) Point {
	return s.positionOn(s.geometry[lane.ID], &lane, distance)
}

// blockPosition returns the same point as position for the lane of b.
func (s *Simulation) blockPosition(b *block, distance float64) Point {
	if b.geometry == nil {
		return s.position(b.lane, distance)
	}
	return s.positionOn(b.geometry, &b.lane, distance)
}

// positionOn returns the point at a distance along lane. geometry is the
// geometry of lane, or nil.
func (s *Simulation) positionOn(geometry *laneGeometry, lane *Lane, distance float64) Point {
	if geometry == nil || len(geometry.segments) == 0 {
		return s.network.Position(*lane, distance)
	}
	index := sort.Search(len(geometry.segments), func(index int) bool {
		return geometry.segments[index].end >= distance
	})
	if index == len(geometry.segments) {
		return geometry.end
	}
	segment := geometry.segments[index]
	fraction := max(0, distance-segment.start) / (segment.end - segment.start)
	return Point{
		X: segment.from.X + fraction*(segment.to.X-segment.from.X),
		Y: segment.from.Y + fraction*(segment.to.Y-segment.from.Y),
	}
}
