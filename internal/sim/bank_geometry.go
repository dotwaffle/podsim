package sim

import (
	"cmp"
	"errors"
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
	depots    *depotIndex
	lateral   *depotLateral
	// tails is the tail index of the network. Only a network with a large
	// path reads it, for the envelope reach.
	tails map[string]geometryTails
	// reach, envelope, and shapes are set by the audit: the whole-lane plane
	// reach, the envelope reach, and the shape pairs (see shapePairs).
	reach    *lazyPlaneReach
	envelope *lazyEnvelopeReach
	shapes   *depotLateral
	// certifier sweeps the candidate shape pairs. A nil certifier gets
	// one with shapeOptions when a pair first needs it. Tests set it to
	// change the options.
	certifier *shapeCertifier
	// verdicts holds the close tests of shapePairs, so that the audit does
	// not repeat them. A nil value keeps no result. auditBankGeometry sets
	// one when it is nil.
	verdicts *pairVerdicts
}

// pairVerdicts holds the results of bankPathsClose on the whole polylines
// of pairs of audit paths, by the keys of the two paths, in scan order, and
// the clearance. hits counts the results that the audit used again.
type pairVerdicts struct {
	near map[pairVerdictKey]bool
	hits int
}

type pairVerdictKey struct {
	pair      [2]separationKey
	clearance float64
}

// record keeps the result of bankPathsClose(first.points, second.points,
// clearance).
func (v *pairVerdicts) record(first, second bankAuditPath, clearance float64, near bool) {
	if v == nil {
		return
	}
	if v.near == nil {
		v.near = make(map[pairVerdictKey]bool)
	}
	v.near[pairVerdictKey{pair: [2]separationKey{first.key, second.key}, clearance: clearance}] = near
}

// wholeClose returns bankPathsClose(first.points, second.points,
// clearance), from the kept result when there is one.
func (v *pairVerdicts) wholeClose(first, second bankAuditPath, clearance float64) bool {
	if v != nil {
		if near, ok := v.near[pairVerdictKey{pair: [2]separationKey{first.key, second.key}, clearance: clearance}]; ok {
			v.hits++
			return near
		}
	}
	return bankPathsClose(first.points, second.points, clearance)
}

// ValidateBankGeometry audits each pair of nonincident paths of the
// network (see auditBankGeometry). Shared endpoints remain subject to
// ordinary junction resource control.
func (n Network) ValidateBankGeometry() error {
	audit, err := n.newBankAudit()
	if err != nil {
		return err
	}
	return n.validateBankGeometry(audit)
}

// BankGeometryShapePairs is ValidateBankGeometry that also returns the number
// of shape pairs of the network (see shapePairs). The count is zero when the
// audit returns an error.
func (n Network) BankGeometryShapePairs() (int, error) {
	audit, err := n.newBankAudit()
	if err != nil {
		return 0, err
	}
	shapes, err := n.auditBankGeometry(audit)
	if err != nil || shapes == nil {
		return 0, err
	}
	return len(shapes.pairs), nil
}

// newBankAudit returns the indexes of the network that the bank audit
// reads.
func (n Network) newBankAudit() (bankAudit, error) {
	var audit bankAudit
	tails := indexGeometryTails(n)
	audit.retention = newLaneRetention(n, tails)
	audit.tails = tails
	if len(n.Depots) > 0 {
		nodes, err := n.validateNodes()
		if err != nil {
			return bankAudit{}, err
		}
		if audit.depots, err = newDepotIndex(n, nodes); err != nil {
			return bankAudit{}, err
		}
		audit.lateral = newDepotLateral(n, audit.depots, tails, indexBerthKeys(n))
	}
	if len(n.ManeuverGroups) > 0 {
		nodes, err := n.validateNodes()
		if err != nil {
			return bankAudit{}, err
		}
		if audit.maneuvers, err = newManeuverIndex(n, nodes); err != nil {
			return bankAudit{}, err
		}
		if audit.certified, err = certifyManeuvers(n, audit.maneuvers, indexBerthKeys(n)); err != nil {
			return bankAudit{}, err
		}
	}
	return audit, nil
}

// validateBankGeometry is auditBankGeometry with no shape pairs in the
// result.
func (n Network) validateBankGeometry(audit bankAudit) error {
	_, err := n.auditBankGeometry(audit)
	return err
}

// auditBankGeometry is ValidateBankGeometry with the indexes of the
// network. It returns the shape pairs of the network (see shapePairs). The
// audit checks each pair, small pairs included (decision D12-A of the
// shape clearance design). Each pair of positions needs the clearance that
// pairPieces gives, so a piece that the safety observation certifies needs
// less than Clearance. With a maneuver group, it skips the certified
// pairs, two lanes of one group, which one pod at a time uses, and a lane
// of a closed bay chain with its own berth, which the chain cells hold. A
// lateral pair of the depot lanes (see depotLateral) needs its lateral
// clearance along its whole length. A large pair on separated planes takes
// the planes of each path by position from the envelope reach, and it
// needs its clearance only at the parts that are not separated. A shape
// pair needs the side-by-side distance of the bodies of its lanes.
//
// A pair that only reach denies the plane exemption reports its refusal
// after the scan, so that the first refusal of a pair without an exemption
// stays the first refusal of the audit. The refusal is a PlaneReachError, so
// that a caller can report it after its own later checks.
func (n Network) auditBankGeometry(audit bankAudit) (*depotLateral, error) {
	retention, maneuvers := audit.retention, audit.maneuvers
	large, groups := n.hasLargeGeometry(), maneuvers != nil
	positions := indexNodePositions(n.Nodes)
	var incident map[string][]Lane
	if large {
		incident = incidentLanes(n)
	}
	audit.reach = &lazyPlaneReach{network: n}
	if large {
		audit.envelope = newLazyEnvelopeReach(n, audit.tails)
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
				return nil, fmt.Errorf("berth %q has an unknown node", berth.ID)
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
	if audit.verdicts == nil {
		audit.verdicts = &pairVerdicts{}
	}
	audit.shapes = audit.shapePairs(n, paths, scanClearance)
	var reachRefusal *PlaneReachError
	for i, first := range paths {
		for _, second := range paths[i+1:] {
			if second.min.X-first.max.X >= scanClearance {
				break
			}
			clearance := Clearance
			if first.large || second.large {
				clearance = largeClearance
			}
			if first.min.Y-second.max.Y >= clearance || second.min.Y-first.max.Y >= clearance || safetyLocationsShareNode(first.location, second.location) ||
				groups && (maneuvers.auditSkips(first, second) || audit.certified.certified(first.key, second.key)) {
				continue
			}
			planes := audit.planes(first, second)
			if planes.exempt {
				continue
			}
			pieces := audit.pieces(first, second)
			for _, piece := range pieces {
				for _, part := range planes.cut(piece) {
					// With one piece and no plane ranges, the test is on the
					// whole polylines, as shapePairs tests a pair.
					var near bool
					if len(pieces) > 1 || planes.ranges != nil {
						near = bankPathsClose(polylinePiece(first.points, part.first[0], part.first[1]), polylinePiece(second.points, part.second[0], part.second[1]), part.clearance)
					} else {
						near = audit.verdicts.wholeClose(first, second, part.clearance)
					}
					if near {
						if !planes.held {
							return nil, errors.New(planeGapText(first.id, second.id, part.clearance))
						}
						reachRefusal = cmp.Or(reachRefusal, &PlaneReachError{first.id, second.id, part.clearance})
					}
				}
			}
		}
	}
	if reachRefusal != nil {
		return nil, reachRefusal
	}
	return audit.shapes, nil
}

// PlaneReachError refuses a pair of paths on different planes that are too
// close, when only the locations that a pod on a path can reach deny the
// pair the plane exemption. Each other check of a network or a project comes
// before this refusal: a caller with later checks holds it until they pass.
type PlaneReachError struct {
	First, Second string
	Clearance     float64
}

// Error returns the text of a refusal of a pair of paths.
func (e *PlaneReachError) Error() string {
	return planeGapText(e.First, e.Second, e.Clearance)
}

func planeGapText(first, second string, clearance float64) string {
	return fmt.Sprintf("nonincident paths %q and %q are less than %g meters apart", first, second, clearance)
}

// pieces returns the pieces of a pair of paths: one piece at the clearance
// of a lateral pair or a shape pair, and otherwise the pieces of
// pairPieces.
func (audit bankAudit) pieces(first, second bankAuditPath) []pairPiece {
	for _, table := range []*depotLateral{audit.lateral, audit.shapes} {
		if clearance, ok := table.clearance(first.key, second.key); ok {
			return []pairPiece{{first: [2]float64{0, first.member.length}, second: [2]float64{0, second.member.length}, clearance: clearance}}
		}
	}
	return audit.retention.pairPieces(first.member, second.member)
}

// bankPathsClose reports whether two polylines come closer than clearance.
// It skips a segment of first whose box is more than clearance from the box
// of second on one axis. For such a segment, the first test of
// bankSegmentGap gives +Inf with each segment of second, so the result does
// not change. A NaN or infinite coordinate makes the skip test false.
func bankPathsClose(first, second []Point, clearance float64) bool {
	if len(first) < 2 || len(second) < 2 {
		return false
	}
	low, high := second[0], second[0]
	for _, point := range second[1:] {
		low.X, low.Y = math.Min(low.X, point.X), math.Min(low.Y, point.Y)
		high.X, high.Y = math.Max(high.X, point.X), math.Max(high.Y, point.Y)
	}
	for a := 1; a < len(first); a++ {
		p, q := first[a-1], first[a]
		if math.Max(p.X, q.X)+clearance < low.X || high.X+clearance < math.Min(p.X, q.X) || math.Max(p.Y, q.Y)+clearance < low.Y || high.Y+clearance < math.Min(p.Y, q.Y) {
			continue
		}
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

// reachSeparated reports whether every cross-pair of the reach of a large
// pair is on different planes (see planeReach). Other pairs have no reach.
func (path bankAuditPath) reachSeparated(other bankAuditPath, reach *lazyPlaneReach) bool {
	if !path.large && !other.large {
		return true
	}
	index := reach.get()
	return index.path(path.key).separated(index.path(other.key))
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
