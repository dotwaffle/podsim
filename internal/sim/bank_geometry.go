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
	member    auditMember
	// key is the separation key of the lane or the berth.
	key separationKey
}

// bankAudit holds the indexes of a network that the bank audit reads.
type bankAudit struct {
	retention *laneRetention
	maneuvers *maneuverIndex
	certified certifiedPairs
}

// ValidateBankGeometry audits nonincident paths affected by banks, large
// classes or maneuver groups. Shared endpoints remain subject to ordinary
// junction resource control.
func (n Network) ValidateBankGeometry() error {
	var audit bankAudit
	if n.hasStationBanks() || len(n.ManeuverGroups) > 0 {
		audit.retention = newLaneRetention(n, indexGeometryTails(n))
	}
	if len(n.ManeuverGroups) > 0 {
		nodes, err := n.validateNodes()
		if err != nil {
			return err
		}
		if audit.maneuvers, err = newManeuverIndex(n, nodes); err != nil {
			return err
		}
		if audit.certified, err = certifyManeuvers(n, audit.maneuvers); err != nil {
			return err
		}
	}
	return n.validateBankGeometry(audit)
}

// validateBankGeometry is ValidateBankGeometry with the indexes of the
// network. Each pair of positions needs the clearance that pairPieces
// gives, so a piece that the safety observation certifies needs less than
// Clearance. With a maneuver group, the audit checks each pair, as with
// banks. It skips the certified pairs, two lanes of one group, which one
// pod at a time uses, and a lane of a closed bay chain with its own berth,
// which the chain cells hold.
func (n Network) validateBankGeometry(audit bankAudit) error {
	retention, maneuvers := audit.retention, audit.maneuvers
	banks, large, groups := n.hasStationBanks(), n.hasLargeGeometry(), maneuvers != nil
	if !banks && !large && !groups {
		return nil
	}
	positions := indexNodePositions(n.Nodes)
	var incident map[string][]Lane
	if large {
		incident = incidentLanes(n)
	}
	paths := make([]bankAuditPath, 0, len(n.Lanes))
	for index, lane := range n.Lanes {
		points := lanePointsBetween(lane, positions[lane.From], positions[lane.To], make([]Point, 0, 65))
		path := bankAuditPath{id: lane.ID, location: SafetyLocation{SeparationGroup: lane.SeparationGroup, From: lane.From, To: lane.To}, points: points, min: points[0], max: points[0], large: largeClassSet(lane.VehicleClasses), key: laneKey(index)}
		path.member = auditMember{spans: retention.laneSpans(lane.ID), speed: lane.SpeedLimit, large: path.large}
		for index := 1; index < len(points); index++ {
			path.member.length += pointDistance(points[index-1], points[index])
		}
		if large {
			path.locations = []SafetyLocation{path.location}
		}
		for _, point := range points {
			path.min.X, path.min.Y = min(path.min.X, point.X), min(path.min.Y, point.Y)
			path.max.X, path.max.Y = max(path.max.X, point.X), max(path.max.Y, point.Y)
		}
		paths = append(paths, path)
	}
	berthIndex := 0
	for _, station := range n.Stations {
		for _, berth := range station.Berths {
			position, ok := positions[berth.Node]
			if !ok {
				return fmt.Errorf("berth %q has an unknown node", berth.ID)
			}
			path := bankAuditPath{id: berth.ID, location: SafetyLocation{SeparationGroup: berth.SeparationGroup, From: berth.Node, To: berth.Node}, points: []Point{position, position}, min: position, max: position, large: largeBerth(station, berth), key: berthKey(berthIndex)}
			berthIndex++
			path.member = auditMember{spans: retention.nodeSpans(berth.Node), large: path.large}
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
			if !banks && !groups && !first.large && !second.large {
				continue
			}
			clearance := Clearance
			if first.large || second.large {
				clearance = largeClearance
			}
			if first.min.Y-second.max.Y >= clearance || second.min.Y-first.max.Y >= clearance || safetyLocationsShareNode(first.location, second.location) || first.separated(second) ||
				groups && (maneuvers.auditSkips(first, second) || audit.certified.certified(first.key, second.key)) {
				continue
			}
			pieces := retention.pairPieces(first.member, second.member)
			for _, piece := range pieces {
				a, b := first.points, second.points
				if len(pieces) > 1 {
					a, b = polylinePiece(a, piece.first[0], piece.first[1]), polylinePiece(b, piece.second[0], piece.second[1])
				}
				if bankPathsClose(a, b, piece.clearance) {
					return fmt.Errorf("nonincident paths %q and %q are less than %g meters apart", first.id, second.id, piece.clearance)
				}
			}
		}
	}
	return nil
}

// bankPathsClose reports whether two polylines come closer than clearance.
func bankPathsClose(first, second []Point, clearance float64) bool {
	for a := 1; a < len(first); a++ {
		for b := 1; b < len(second); b++ {
			if bankSegmentGap(first[a-1], first[a], second[b-1], second[b], clearance) < clearance-separationTolerance {
				return true
			}
		}
	}
	return false
}

func (path bankAuditPath) separated(other bankAuditPath) bool {
	if path.large || other.large {
		return envelopeLocationsSeparated(path.locations, other.locations)
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
