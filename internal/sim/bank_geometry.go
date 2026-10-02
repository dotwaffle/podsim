package sim

import (
	"fmt"
	"math"
	"slices"
)

type bankAuditPath struct {
	id       string
	location SafetyLocation
	points   []Point
	min, max Point
}

// ValidateBankGeometry audits all nonincident paths when banks are present.
// Shared endpoints remain subject to ordinary junction resource control.
func (n Network) ValidateBankGeometry() error {
	if !n.hasStationBanks() {
		return nil
	}
	paths := make([]bankAuditPath, 0, len(n.Lanes))
	for _, lane := range n.Lanes {
		points := n.Polyline(lane)
		path := bankAuditPath{id: lane.ID, location: SafetyLocation{SeparationGroup: lane.SeparationGroup, From: lane.From, To: lane.To}, points: points, min: points[0], max: points[0]}
		for _, point := range points {
			path.min.X, path.min.Y = min(path.min.X, point.X), min(path.min.Y, point.Y)
			path.max.X, path.max.Y = max(path.max.X, point.X), max(path.max.Y, point.Y)
		}
		paths = append(paths, path)
	}
	for _, station := range n.Stations {
		for _, berth := range station.Berths {
			node, ok := n.Node(berth.Node)
			if !ok {
				return fmt.Errorf("berth %q has an unknown node", berth.ID)
			}
			paths = append(paths, bankAuditPath{id: berth.ID, location: SafetyLocation{SeparationGroup: berth.SeparationGroup, From: berth.Node, To: berth.Node}, points: []Point{node.Position, node.Position}, min: node.Position, max: node.Position})
		}
	}
	slices.SortStableFunc(paths, func(a, b bankAuditPath) int {
		if a.min.X < b.min.X {
			return -1
		}
		if a.min.X > b.min.X {
			return 1
		}
		return 0
	})
	for i, first := range paths {
		for _, second := range paths[i+1:] {
			if second.min.X-first.max.X >= Clearance {
				break
			}
			if first.min.Y-second.max.Y >= Clearance || second.min.Y-first.max.Y >= Clearance || safetyLocationsShareNode(first.location, second.location) || safetyLocationsSeparated(first.location, second.location) {
				continue
			}
			for a := 1; a < len(first.points); a++ {
				for b := 1; b < len(second.points); b++ {
					if bankSegmentGap(first.points[a-1], first.points[a], second.points[b-1], second.points[b]) < Clearance-separationTolerance {
						return fmt.Errorf("nonincident paths %q and %q are less than %.0f meters apart", first.id, second.id, Clearance)
					}
				}
			}
		}
	}
	return nil
}

func bankSegmentGap(a, b, c, d Point) float64 {
	if math.Max(a.X, b.X)+Clearance < math.Min(c.X, d.X) || math.Max(c.X, d.X)+Clearance < math.Min(a.X, b.X) || math.Max(a.Y, b.Y)+Clearance < math.Min(c.Y, d.Y) || math.Max(c.Y, d.Y)+Clearance < math.Min(a.Y, b.Y) {
		return math.Inf(1)
	}
	cross := func(a, b, c Point) float64 { return (b.X-a.X)*(c.Y-a.Y) - (b.Y-a.Y)*(c.X-a.X) }
	u, v, w, z := cross(a, b, c), cross(a, b, d), cross(c, d, a), cross(c, d, b)
	if (u < 0 && v > 0 || u > 0 && v < 0) && (w < 0 && z > 0 || w > 0 && z < 0) {
		return 0
	}
	return min(segmentPointDistance(a, c, d), segmentPointDistance(b, c, d), segmentPointDistance(c, a, b), segmentPointDistance(d, a, b))
}
