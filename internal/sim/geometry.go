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

func buildLaneGeometry(network Network) map[string]laneGeometry {
	geometry := make(map[string]laneGeometry, len(network.Lanes))
	for _, lane := range network.Lanes {
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
		geometry[lane.ID] = indexed
	}
	return geometry
}

func (s *Simulation) position(lane Lane, distance float64) Point {
	geometry, ok := s.geometry[lane.ID]
	if !ok || len(geometry.segments) == 0 {
		return s.network.Position(lane, distance)
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
