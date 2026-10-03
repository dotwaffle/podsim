package sim

import (
	"fmt"
	"math"
	"slices"
)

type bankAuditPath struct {
	id        string
	location  SafetyLocation
	points    []Point
	min, max  Point
	large     bool
	locations []SafetyLocation
}

// ValidateBankGeometry audits nonincident paths affected by banks or large classes.
// Shared endpoints remain subject to ordinary junction resource control.
func (n Network) ValidateBankGeometry() error {
	banks, large := n.hasStationBanks(), n.hasLargeGeometry()
	if !banks && !large {
		return nil
	}
	var incident map[string][]Lane
	if large {
		incident = incidentLanes(n)
	}
	paths := make([]bankAuditPath, 0, len(n.Lanes))
	for _, lane := range n.Lanes {
		points := n.Polyline(lane)
		path := bankAuditPath{id: lane.ID, location: SafetyLocation{SeparationGroup: lane.SeparationGroup, From: lane.From, To: lane.To}, points: points, min: points[0], max: points[0], large: largeClassSet(lane.VehicleClasses)}
		if large {
			path.locations = []SafetyLocation{path.location}
		}
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
			path := bankAuditPath{id: berth.ID, location: SafetyLocation{SeparationGroup: berth.SeparationGroup, From: berth.Node, To: berth.Node}, points: []Point{node.Position, node.Position}, min: node.Position, max: node.Position, large: largeBerth(station, berth)}
			if large {
				path.locations = berthGeometryLocations(berth, incident[berth.Node])
			}
			paths = append(paths, path)
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
	scanClearance := Clearance
	if large {
		scanClearance = largeClearance
	}
	for i, first := range paths {
		for _, second := range paths[i+1:] {
			if second.min.X-first.max.X >= scanClearance {
				break
			}
			if !banks && !first.large && !second.large {
				continue
			}
			clearance := Clearance
			if first.large || second.large {
				clearance = largeClearance
			}
			if first.min.Y-second.max.Y >= clearance || second.min.Y-first.max.Y >= clearance || safetyLocationsShareNode(first.location, second.location) || first.separated(second) {
				continue
			}
			for a := 1; a < len(first.points); a++ {
				for b := 1; b < len(second.points); b++ {
					if bankSegmentGap(first.points[a-1], first.points[a], second.points[b-1], second.points[b], clearance) < clearance-separationTolerance {
						return fmt.Errorf("nonincident paths %q and %q are less than %.0f meters apart", first.id, second.id, clearance)
					}
				}
			}
		}
	}
	return nil
}

func (path bankAuditPath) separated(other bankAuditPath) bool {
	if path.large || other.large {
		return geometryPlanesSeparated(path.locations, other.locations)
	}
	return safetyLocationsSeparated(path.location, other.location)
}

func (n Network) hasLargeGeometry() bool {
	for _, lane := range n.Lanes {
		if largeClassSet(lane.VehicleClasses) {
			return true
		}
	}
	for _, station := range n.Stations {
		for _, berth := range station.Berths {
			if largeBerth(station, berth) {
				return true
			}
		}
	}
	return false
}

func bankSegmentGap(a, b, c, d Point, clearance float64) float64 {
	if math.Max(a.X, b.X)+clearance < math.Min(c.X, d.X) || math.Max(c.X, d.X)+clearance < math.Min(a.X, b.X) || math.Max(a.Y, b.Y)+clearance < math.Min(c.Y, d.Y) || math.Max(c.Y, d.Y)+clearance < math.Min(a.Y, b.Y) {
		return math.Inf(1)
	}
	cross := func(a, b, c Point) float64 { return (b.X-a.X)*(c.Y-a.Y) - (b.Y-a.Y)*(c.X-a.X) }
	u, v, w, z := cross(a, b, c), cross(a, b, d), cross(c, d, a), cross(c, d, b)
	if (u < 0 && v > 0 || u > 0 && v < 0) && (w < 0 && z > 0 || w > 0 && z < 0) {
		return 0
	}
	return min(segmentPointDistance(a, c, d), segmentPointDistance(b, c, d), segmentPointDistance(c, a, b), segmentPointDistance(d, a, b))
}
