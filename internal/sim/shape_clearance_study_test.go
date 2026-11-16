package sim

import (
	"cmp"
	"encoding/json/v2"
	"fmt"
	"math"
	"os"
	"slices"
	"strings"
	"testing"
)

// This file is the study probe of docs/design/shape-clearance.md, section
// 2. It does not test the simulator, and it runs only when the variable
// that shapeStudyEnv names holds the path of a project file from
// cmd/scenario. It copies the pair scan of validateBankGeometry and, for
// each pair with a large path, records the smallest distance under each
// plane rule of the study, in place of the first refusal.

const shapeStudyEnv = "PODSIM_SHAPE_STUDY"

// shapeStudyRule is a plane rule of the study.
type shapeStudyRule int

const (
	// studyWhole is the plane reach of today (newPlaneReach).
	studyWhole shapeStudyRule = iota
	// studyEnvelope keeps the planes of the envelope of
	// appendVehicleSafetyLocations, by position on the path: the lanes into
	// the From node in the first max(20 m, tail) of a lane, with the berth
	// and the lanes of a From berth node, and the lanes out of the To node in
	// the last max(6 m, front).
	studyEnvelope
	// studyBody keeps the lanes into the From node in the first 6 m and the
	// lanes out of the To node in the last 6 m.
	studyBody
	// studyLanePlane gives a large path the plane of its own lane or berth.
	studyLanePlane
	studyRules
)

var shapeStudyRuleNames = [studyRules]string{"whole", "envelope", "body", "lane-plane"}

// shapeStudyClearances are the clearances of the study columns: today, the
// small clearance, the union disk bound, the corrected per-class disk
// bound, the old disk bound, and the Express side-by-side distance.
var shapeStudyClearances = []float64{20, 12, 9.31, 8.96, 8.65, 4}

// shapeStudyBins are the upper bounds of the residual bins.
var shapeStudyBins = []float64{4, 8.65, 8.96, 9.31, 12, 20}

// shapeStudyPair is a pair of paths with a large path, and the smallest
// distance between pieces whose planes overlap, under each rule. A
// distance of +Inf means that the rule exempts the pair.
type shapeStudyPair struct {
	first, second string
	station       string
	distance      [studyRules]float64
}

// shapeStudyResult holds the pairs with a large path that are closer than
// 20 m under a rule, and the count of small pairs that the production rule
// refuses.
type shapeStudyResult struct {
	pairs []shapeStudyPair
	small int
}

func (r shapeStudyResult) count(rule shapeStudyRule, clearance float64) int {
	count := 0
	for _, pair := range r.pairs {
		if pair.distance[rule] < clearance-separationTolerance {
			count++
		}
	}
	return count
}

func (r shapeStudyResult) bins(rule shapeStudyRule) []int {
	counts := make([]int, len(shapeStudyBins))
	for _, pair := range r.pairs {
		for index, bound := range shapeStudyBins {
			if pair.distance[rule] < bound-separationTolerance {
				counts[index]++
				break
			}
		}
	}
	return counts
}

func (r shapeStudyResult) stations(rule shapeStudyRule) string {
	counts := make(map[string]int)
	for _, pair := range r.pairs {
		if pair.distance[rule] < largeClearance-separationTolerance {
			counts[pair.station]++
		}
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b string) int { return cmp.Or(cmp.Compare(counts[b], counts[a]), cmp.Compare(a, b)) })
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s %d", key, counts[key]))
	}
	return strings.Join(parts, ", ")
}

// shapeStudySegment is a part of a path and the planes of a pod in it.
type shapeStudySegment struct {
	from, to  float64
	locations []SafetyLocation
}

// shapeStudyIndex holds the lanes and berths at each node.
type shapeStudyIndex struct {
	incoming, outgoing map[string][]Lane
	incident           map[string][]Lane
	berths             map[string]Berth
	tails              map[string]geometryTails
}

func newShapeStudyIndex(n Network, tails map[string]geometryTails) shapeStudyIndex {
	index := shapeStudyIndex{incoming: make(map[string][]Lane), outgoing: make(map[string][]Lane), incident: incidentLanes(n), berths: make(map[string]Berth), tails: tails}
	for _, lane := range n.Lanes {
		index.incoming[lane.To] = append(index.incoming[lane.To], lane)
		index.outgoing[lane.From] = append(index.outgoing[lane.From], lane)
	}
	for _, station := range n.Stations {
		for _, berth := range station.Berths {
			index.berths[berth.Node] = berth
		}
	}
	return index
}

func laneStudyLocation(lane Lane) SafetyLocation {
	return SafetyLocation{SeparationGroup: lane.SeparationGroup, From: lane.From, To: lane.To}
}

// segments returns the segments of a lane path under the envelope or the
// body rule. Each segment holds the lane itself, so a position in two
// segments is in their union.
func (index shapeStudyIndex) segments(rule shapeStudyRule, lane Lane, length float64) []shapeStudySegment {
	own := laneStudyLocation(lane)
	rear, front := largeEnvelopeRadius, largeEnvelopeRadius
	head := []SafetyLocation{own}
	for _, previous := range index.incoming[lane.From] {
		head = append(head, laneStudyLocation(previous))
		if rule == studyEnvelope {
			rear = max(rear, largeClearance, index.tails[previous.ID].tail)
		}
	}
	if berth, ok := index.berths[lane.From]; ok && rule == studyEnvelope {
		// The origin berth and its incident planes until origin release.
		head = append(head, berthGeometryLocations(berth, index.incident[lane.From])...)
		rear = max(rear, largeClearance, index.tails[lane.ID].fromTail)
	}
	if rule == studyEnvelope {
		front = max(front, index.tails[lane.ID].front)
	}
	tail := []SafetyLocation{own}
	for _, next := range index.outgoing[lane.To] {
		tail = append(tail, laneStudyLocation(next))
	}
	return []shapeStudySegment{
		{from: 0, to: length, locations: []SafetyLocation{own}},
		{from: 0, to: min(rear, length), locations: head},
		{from: max(0, length-front), to: length, locations: tail},
	}
}

// shapeStudyLaneStation returns the station of a lane, or of a junction
// movement lane of a London station, or "" for a link.
func shapeStudyLaneStation(lane Lane) string {
	if lane.StationID != "" {
		return lane.StationID
	}
	if station, _, ok := strings.Cut(lane.ID, "-move-"); ok {
		return station
	}
	return ""
}

// shapeStudyPath is a path of the audit with its segments under the
// envelope and the body rules.
type shapeStudyPath struct {
	bankAuditPath
	lane     bool
	station  string
	segments [studyRules][]shapeStudySegment
}

func shapeStudyPaths(n Network, audit bankAudit, index shapeStudyIndex) []shapeStudyPath {
	positions := indexNodePositions(n.Nodes)
	paths := make([]shapeStudyPath, 0, len(n.Lanes))
	for laneIndex, lane := range n.Lanes {
		points := lanePointsBetween(lane, positions[lane.From], positions[lane.To], make([]Point, 0, 65))
		path := shapeStudyPath{lane: true, station: shapeStudyLaneStation(lane), id: lane.ID, location: laneStudyLocation(lane), points: points, min: points[0], max: points[0], large: largeClassSet(lane.VehicleClasses), key: laneKey(laneIndex)}
		path.member = auditMember{spans: audit.retention.laneSpans(lane.ID), speed: lane.SpeedLimit, large: path.large}
		for point := 1; point < len(points); point++ {
			path.member.length += pointDistance(points[point-1], points[point])
		}
		path.locations = []SafetyLocation{path.location}
		for _, point := range points {
			path.min.X, path.min.Y = min(path.min.X, point.X), min(path.min.Y, point.Y)
			path.max.X, path.max.Y = max(path.max.X, point.X), max(path.max.Y, point.Y)
		}
		for _, rule := range []shapeStudyRule{studyEnvelope, studyBody} {
			path.segments[rule] = index.segments(rule, lane, path.member.length)
		}
		paths = append(paths, path)
	}
	berthIndex := 0
	for _, station := range n.Stations {
		for _, berth := range station.Berths {
			position := positions[berth.Node]
			path := shapeStudyPath{station: station.ID, id: berth.ID, location: SafetyLocation{SeparationGroup: berth.SeparationGroup, From: berth.Node, To: berth.Node}, points: []Point{position, position}, min: position, max: position, large: largeBerth(station, berth), key: berthKey(berthIndex)}
			berthIndex++
			path.member = auditMember{spans: audit.retention.nodeSpans(berth.Node), large: path.large}
			path.locations = berthGeometryLocations(berth, index.incident[berth.Node])
			for _, rule := range []shapeStudyRule{studyEnvelope, studyBody} {
				path.segments[rule] = []shapeStudySegment{{locations: path.locations}}
			}
			paths = append(paths, path)
		}
	}
	slices.SortStableFunc(paths, func(a, b shapeStudyPath) int { return cmp.Compare(a.min.X, b.min.X) })
	return paths
}

// piece returns the points of a segment of a path.
func (path shapeStudyPath) piece(segment shapeStudySegment) []Point {
	if !path.lane || segment.from <= 0 && segment.to >= path.member.length {
		return path.points
	}
	return polylinePiece(path.points, segment.from, segment.to)
}

func shapeStudyGap(first, second []Point) float64 {
	gap := math.Inf(1)
	for a := 1; a < len(first); a++ {
		for b := 1; b < len(second); b++ {
			gap = min(gap, bankSegmentGap(first[a-1], first[a], second[b-1], second[b], largeClearance))
		}
	}
	return gap
}

// distance returns the smallest distance of a pair under a rule, or +Inf
// when the rule exempts the pair.
func (path shapeStudyPath) distance(other shapeStudyPath, rule shapeStudyRule, reach *lazyPlaneReach) float64 {
	switch rule {
	case studyWhole:
		if path.separated(other.bankAuditPath) && path.reachSeparated(other.bankAuditPath, reach) {
			return math.Inf(1)
		}
		return shapeStudyGap(path.points, other.points)
	case studyLanePlane:
		if safetyLocationsSeparated(path.location, other.location) {
			return math.Inf(1)
		}
		return shapeStudyGap(path.points, other.points)
	case studyEnvelope, studyBody:
		gap := math.Inf(1)
		for _, a := range path.segments[rule] {
			for _, b := range other.segments[rule] {
				if !envelopeLocationsSeparated(a.locations, b.locations) {
					gap = min(gap, shapeStudyGap(path.piece(a), other.piece(b)))
				}
			}
		}
		return gap
	case studyRules:
	}
	panic("unknown rule")
}

// shapeStudyAudit is the pair scan of validateBankGeometry, with the indexes
// that prepareNetworkTails builds, recording each pair in place of the first
// refusal.
func shapeStudyAudit(t *testing.T, n Network) shapeStudyResult {
	t.Helper()
	tails := indexGeometryTails(n)
	maneuvers, _ := newManeuverIndex(n, indexNodePositions(n.Nodes))
	berthKeys := indexBerthKeys(n)
	certified, err := certifyManeuvers(n, maneuvers, berthKeys)
	if err != nil {
		t.Fatal(err)
	}
	depots, _ := newDepotIndex(n, indexNodePositions(n.Nodes))
	audit := bankAudit{retention: newLaneRetention(n, tails), maneuvers: maneuvers, certified: certified, depots: depots, lateral: newDepotLateral(n, depots, tails, berthKeys)}
	paths := shapeStudyPaths(n, audit, newShapeStudyIndex(n, tails))
	banks, groups := n.hasStationBanks(), maneuvers != nil
	reach := &lazyPlaneReach{network: n}
	var result shapeStudyResult
	for i, first := range paths {
		for _, second := range paths[i+1:] {
			if second.min.X-first.max.X >= largeClearance {
				break
			}
			if !banks && !groups && !first.large && !second.large && !audit.depots.auditPath(first.bankAuditPath) && !audit.depots.auditPath(second.bankAuditPath) {
				continue
			}
			clearance := Clearance
			if first.large || second.large {
				clearance = largeClearance
			}
			if first.min.Y-second.max.Y >= clearance || second.min.Y-first.max.Y >= clearance || safetyLocationsShareNode(first.location, second.location) ||
				groups && (maneuvers.auditSkips(first.bankAuditPath, second.bankAuditPath) || audit.certified.certified(first.key, second.key)) {
				continue
			}
			if !first.large && !second.large {
				if !first.separated(second.bankAuditPath) && shapeStudySmallRefused(audit, first, second) {
					result.small++
				}
				continue
			}
			pair := shapeStudyPair{first: first.id, second: second.id, station: cmp.Or(first.station, second.station, "links")}
			near := false
			for rule := range studyRules {
				pair.distance[rule] = first.distance(second, rule, reach)
				near = near || pair.distance[rule] < largeClearance-separationTolerance
			}
			if near {
				result.pairs = append(result.pairs, pair)
			}
		}
	}
	return result
}

// shapeStudySmallRefused reports whether the production rule refuses a
// small pair on one plane.
func shapeStudySmallRefused(audit bankAudit, first, second shapeStudyPath) bool {
	pieces := audit.pieces(first.bankAuditPath, second.bankAuditPath)
	for _, piece := range pieces {
		a, b := first.points, second.points
		if len(pieces) > 1 {
			a, b = polylinePiece(a, piece.first[0], piece.first[1]), polylinePiece(b, piece.second[0], piece.second[1])
		}
		if bankPathsClose(a, b, piece.clearance) {
			return true
		}
	}
	return false
}

func shapeStudyNetwork(t *testing.T) Network {
	t.Helper()
	path := os.Getenv(shapeStudyEnv)
	if path == "" {
		t.Skip("set " + shapeStudyEnv + " to a project file to run the shape clearance study")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var project struct {
		Network Network `json:"network"`
	}
	if err := json.Unmarshal(data, &project); err != nil {
		t.Fatal(err)
	}
	return project.Network
}

// shapeStudyClasses are the classes of the study: Legacy, Compact, and
// Express.
const shapeStudyClasses ClassSet = 0b1011

// shapeStudyHubs are the hub pairs of section 2.1, by station ID.
var shapeStudyHubs = [][2]string{
	{"940GZZLUWLO", "940GZZLUBNK"}, {"940GZZLUPAC", "940GZZLULVT"}, {"940GZZLUWLO", "940GZZLUCYF"},
	{"940GZZLUTCR", "940GZZLULVT"}, {"940GZZLUVIC", "940GZZLUOXC"}, {"940GZZLULNB", "940GZZLUCYF"},
	{"940GZZLULNB", "940GZZLUWLO"}, {"940GZZLUKSX", "940GZZLUVIC"}, {"940GZZLUKSX", "940GZZLULVT"},
	{"940GZZLUKSX", "940GZZLUWLO"}, {"940GZZLUPAC", "940GZZLUKSX"}, {"940GZZLUEUS", "940GZZLUWLO"},
}

// shapeStudyHubRoute returns the free-flow route from the exit of one
// station to the entry of the other, through no other station.
func shapeStudyHubRoute(t *testing.T, n Network, from, to Station) []Lane {
	t.Helper()
	route, err := n.route(networkRouteInput{from: from.Exit, to: to.Entry, forbidden: n.stationForbidden()})
	if err != nil {
		t.Fatalf("%s to %s: %v", from.ID, to.ID, err)
	}
	return route
}

// setShapeStudyClasses sets the study classes on the lanes of keep, on
// each lane of the stations except the roads that keep does not hold, and
// on the stations and their berths.
func setShapeStudyClasses(n *Network, keep map[string]bool, roads bool, stations ...string) {
	for index := range n.Lanes {
		lane := &n.Lanes[index]
		hub := slices.Contains(stations, lane.StationID) && (roads || !strings.Contains(lane.ID, "-road-"))
		if keep[lane.ID] || hub {
			lane.VehicleClasses = shapeStudyClasses
		}
	}
	for index := range n.Stations {
		station := &n.Stations[index]
		if !slices.Contains(stations, station.ID) {
			continue
		}
		station.VehicleClasses = shapeStudyClasses
		for berth := range station.Berths {
			station.Berths[berth].VehicleClasses = shapeStudyClasses
		}
	}
}

// shortStudyLanes lists the large lanes under the large lane minimum.
func shortStudyLanes(n Network) []string {
	positions := indexNodePositions(n.Nodes)
	var short []string
	for _, lane := range n.Lanes {
		if length := indexedLaneLength(lane, positions[lane.From], positions[lane.To]); largeClassSet(lane.VehicleClasses) && length < 2*largeClearance {
			short = append(short, fmt.Sprintf("%s %.1f m", lane.ID, length))
		}
	}
	return short
}

func logShapeStudy(t *testing.T, name string, result shapeStudyResult) {
	t.Helper()
	for rule := range studyRules {
		counts := make([]string, 0, len(shapeStudyClearances))
		for _, clearance := range shapeStudyClearances {
			counts = append(counts, fmt.Sprintf("%g:%d", clearance, result.count(rule, clearance)))
		}
		t.Logf("%s %s refusals %s", name, shapeStudyRuleNames[rule], strings.Join(counts, " "))
	}
	for _, rule := range []shapeStudyRule{studyEnvelope, studyBody} {
		t.Logf("%s %s bins under %v m: %v", name, shapeStudyRuleNames[rule], shapeStudyBins, result.bins(rule))
		t.Logf("%s %s where: %s", name, shapeStudyRuleNames[rule], result.stations(rule))
	}
	// The pairs that the union disk bound does not clear under the envelope
	// rule need the oriented sweep or new geometry.
	for _, pair := range result.pairs {
		if distance := pair.distance[studyEnvelope]; distance < shapeStudyBins[3]-separationTolerance {
			t.Logf("%s envelope residual %.2f m: %s and %s", name, distance, pair.first, pair.second)
		}
	}
	if result.small > 0 {
		t.Logf("%s small refusals %d", name, result.small)
	}
}

// TestShapeClearanceStudyBase runs the audit copy on the unchanged network.
// On london-full-express it must refuse no pair, as the audit does.
func TestShapeClearanceStudyBase(t *testing.T) {
	n := shapeStudyNetwork(t)
	result := shapeStudyAudit(t, n)
	logShapeStudy(t, "base", result)
	if count := result.count(studyWhole, largeClearance); count != 0 || result.small != 0 {
		t.Errorf("the audit copy refuses %d large and %d small pairs, want none", count, result.small)
	}
}

// TestShapeClearanceStudyRoutes sets the classes on the route of each hub
// pair in each direction and on the two hubs (section 2.1).
func TestShapeClearanceStudyRoutes(t *testing.T) {
	base := shapeStudyNetwork(t)
	for _, hubs := range shapeStudyHubs {
		n := base.clone()
		first, okFirst := n.Station(hubs[0])
		second, okSecond := n.Station(hubs[1])
		if !okFirst || !okSecond {
			t.Fatalf("unknown hub in %v", hubs)
		}
		keep := make(map[string]bool)
		passed := make(map[string]bool)
		lengths := make([]float64, 0, 2)
		positions := indexNodePositions(n.Nodes)
		for _, route := range [][]Lane{shapeStudyHubRoute(t, n, first, second), shapeStudyHubRoute(t, n, second, first)} {
			length := 0.0
			for _, lane := range route {
				keep[lane.ID] = true
				length += indexedLaneLength(lane, positions[lane.From], positions[lane.To])
				if station := shapeStudyLaneStation(lane); station != "" && station != first.ID && station != second.ID {
					passed[station] = true
				}
			}
			lengths = append(lengths, length)
		}
		setShapeStudyClasses(&n, keep, false, first.ID, second.ID)
		name := hubs[0][len(hubs[0])-3:] + "-" + hubs[1][len(hubs[1])-3:]
		t.Logf("%s routes %.0f m and %.0f m, stations passed %d", name, lengths[0], lengths[1], len(passed))
		logShapeStudy(t, name, shapeStudyAudit(t, n))
		t.Logf("%s large lanes under 40 m: %v", name, shortStudyLanes(n))
	}
}

// TestShapeClearanceStudyHubs sets the classes on all lanes of one hub,
// roads included, with no route (section 2.3).
func TestShapeClearanceStudyHubs(t *testing.T) {
	base := shapeStudyNetwork(t)
	seen := make(map[string]bool)
	hubs := []string{"940GZZLUCHX", "940GZZLUCST", "940GZZLUTWH", "940GZZLUMYB"}
	for _, pair := range shapeStudyHubs {
		hubs = append(hubs, pair[0], pair[1])
	}
	for _, hub := range hubs {
		if seen[hub] {
			continue
		}
		seen[hub] = true
		n := base.clone()
		setShapeStudyClasses(&n, nil, true, hub)
		name := hub[len(hub)-3:]
		logShapeStudy(t, name, shapeStudyAudit(t, n))
		t.Logf("%s large lanes under 40 m: %v", name, shortStudyLanes(n))
	}
}
