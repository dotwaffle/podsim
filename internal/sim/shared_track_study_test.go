package sim

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// This file is the study of docs/design/shared-track-preset.md, sections 2
// and 3. It does not test the simulator. It runs only when the variable
// that sharedTrackEnv names holds the path of a LondonFull project file
// from cmd/scenario (see sharedTrackInputs).
//
// The study builds the networks of the design from the file: the network
// of section 2.1, with Express on the route between Waterloo and Bank, and
// the geometry changes of section 3. It runs a copy of the pair scan of
// auditBankGeometry that records each refusal, in place of the first one,
// with the envelope reach of the audit or with a copy whose ahead walk takes
// a front bound for each lane (section 3.1). The file keeps its copies of
// the per-lane bound, the local front of the oracle, and the small-pod
// rules. The code has them since slice 1 of section 7, and the study requires the code to give the results of the copies.
//
// When the variable that sharedTrackOutEnv names holds a directory, the
// study also writes the built projects there, with the Express order
// contract, both Express services, and 4 Express pods at free hub berths,
// for cmd/projectcheck and for the layout audit of the scenarios study.

const (
	sharedTrackEnv    = "PODSIM_SHARED_TRACK_STUDY"
	sharedTrackOutEnv = "PODSIM_SHARED_TRACK_OUT"
	sharedWaterloo    = "940GZZLUWLO"
	sharedBank        = "940GZZLUBNK"
	// sharedThroatDepth and sharedThroatOffset are the distances of the
	// diverge and merge nodes of a London station from its junction center
	// and from its axis (londonStationThroatDepth and
	// londonStationThroatOffset in internal/scenarios).
	sharedThroatDepth  = 80.0
	sharedThroatOffset = 30.0
)

// sharedTrackInputs are the project files of the study, from cmd/scenario
// after the removal of the lateralMerges and shapeClearance members, with
// the SHA-256 of the file and the network SHA-256 of the summary line of
// cmd/scenario.
var sharedTrackInputs = []struct {
	name, flags, file, network string
}{
	{"tight", "-preset london-full", "f9b42a168fbec23652695ed241ac9ba5736d67f7a4a2dc8cda560ff63d5d1da2", "69baad6a8077b3f22c1e250023b4eaeefba10f1ff9bb0812b773634951d84225"},
	{"wide", "-preset london-full -wide-stations", "55a15f15bfe637ce429ea8a369c1ddf771326b5957b8b88e19a566e9ec137815", "c8a1cbb11fd6880c1da86f3a4b365370693660bb13e23566e81890cff4174726"},
	{"ramps", "-preset london-full -junctions ramps", "e8519687a157b91b273ad52805ef3612b90505c1ceac812c9cc83d321c88218e", "9bd2aae1925bcdc2c8503f2e73be5ca9a8aef9c29720e1885108450af89e73f8"},
}

// sharedTrackBuilt holds the network SHA-256 of each network that the study
// builds, by input and network.
var sharedTrackBuilt = map[string]string{
	"tight 2.1": "8906fd2b8f1c36fbf4e1898420ce0989bdb5ad3fa0922c9e3594ef0c9102d12c",
	"tight 3.5": "09b8fdea19e1b87e438fe2539878e3bf232eaa13af5e4e954216919ff67614df",
	"tight 3.6": "750ce680a51c247868c7e4dbbb80dd756e83eb81848186b120918388ab909372",
	"wide 2.1":  "45ad760fb65b47297e33fb5acb55a5b6f5ebf829b0ddb1825d87b987dc7409b9",
	"ramps 2.1": "5dd64bb14d6efb29167829b65919d4646a31e5ac0ea571ee80cc2a0eb90cef43",
}

// sharedTrackCounts holds the refusals, the refusals under 4.0 m, and the
// shape pairs of the audit copy, by input, network, and reach options.
var sharedTrackCounts = map[string][3]int{
	"tight 2.1, each lane":                  {7, 5, 39},
	"tight 2.1, each lane, small-pod rules": {7, 5, 39},
	"tight 3.5, each lane":                  {0, 0, 38},
	"tight 3.5, each lane, small-pod rules": {0, 0, 38},
	"tight 3.6, each lane":                  {0, 0, 23},
	"tight 3.6, each lane, small-pod rules": {0, 0, 23},
	"wide 2.1, each lane":                   {7, 5, 39},
	"wide 2.1, each lane, small-pod rules":  {7, 5, 39},
	"ramps 2.1, each lane":                  {14, 11, 14},
	"ramps 2.1, each lane, small-pod rules": {14, 11, 14},
}

// sharedTrackInput is a loaded project file.
type sharedTrackInput struct {
	name    string
	raw     []byte
	network Network
}

func sharedTrackLoad(t *testing.T) sharedTrackInput {
	t.Helper()
	path := os.Getenv(sharedTrackEnv)
	if path == "" {
		t.Skip("set " + sharedTrackEnv + " to a LondonFull project file to run the shared-track study")
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
	sum := sha256.Sum256(data)
	file, network := hex.EncodeToString(sum[:]), sharedTrackHash(t, project.Network)
	for _, input := range sharedTrackInputs {
		if input.network != network {
			continue
		}
		if input.file != file {
			t.Errorf("%s: file SHA-256 %s, want %s", input.name, file, input.file)
		}
		t.Logf("input %s (cmd/scenario %s): file %s, network %s", input.name, input.flags, file, network)
		return sharedTrackInput{name: input.name, raw: data, network: project.Network}
	}
	t.Fatalf("unknown input: file SHA-256 %s, network SHA-256 %s", file, network)
	return sharedTrackInput{}
}

// sharedTrackHash returns the network SHA-256 of cmd/scenario.
func sharedTrackHash(t *testing.T, n Network) string {
	t.Helper()
	data, err := json.Marshal(n, jsonv1.DefaultOptionsV1())
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// sharedTrackBase returns the network of section 2.1: the study classes on
// the free-flow routes between Waterloo and Bank, on each lane of the two
// hubs except the roads off the route, and on the hubs and their berths.
func sharedTrackBase(t *testing.T, base Network) Network {
	t.Helper()
	n := base.clone()
	first, okFirst := n.Station(sharedWaterloo)
	second, okSecond := n.Station(sharedBank)
	if !okFirst || !okSecond {
		t.Fatal("the network has no Waterloo or Bank")
	}
	keep := make(map[string]bool)
	for _, route := range [][]Lane{shapeStudyHubRoute(t, n, first, second), shapeStudyHubRoute(t, n, second, first)} {
		for _, lane := range route {
			keep[lane.ID] = true
		}
	}
	setShapeStudyClasses(&n, keep, false, first.ID, second.ID)
	return n
}

// sharedTrackChange is a set of the geometry changes of section 3.
type sharedTrackChange struct {
	// part is the size of each part of the tight shape at the two hubs:
	// side, throat to entry, entry to the first row, and pitch. Zero keeps
	// the shape of the generator.
	part float64
	// throat holds the depth of the diverge and the merge from the
	// junction center of a hub with parts, by station.
	throat map[string]float64
	// turn holds the turn of a hub about its junction center, in degrees,
	// by station. A positive turn increases the heading of the axis.
	turn map[string]float64
	// curve is a lane that gets a control point offset meters from the
	// middle of its chord, on the side away from the junction center, or
	// toward it for a negative offset.
	curve  string
	offset float64
	// remove holds the lanes that the change removes.
	remove []string
}

// sharedTrackFrame is the frame of a London station: its junction center,
// the axis from the diverge to the merge, and the outward axis to the
// berth rows.
type sharedTrackFrame struct {
	center, along, outward Point
}

func (f sharedTrackFrame) at(along, outward float64) Point {
	return Point{X: f.center.X + along*f.along.X + outward*f.outward.X, Y: f.center.Y + along*f.along.Y + outward*f.outward.Y}
}

// sharedTrackStationFrame recovers the frame of a station of the generator
// from its diverge, merge, entry, and exit.
func sharedTrackStationFrame(t *testing.T, positions map[string]Point, station Station) sharedTrackFrame {
	t.Helper()
	diverge, merge := positions[station.ID+"-diverge"], positions[station.ID+"-merge"]
	entry, exit := positions[station.Entry], positions[station.Exit]
	width := pointDistance(diverge, merge)
	if math.Abs(width-2*sharedThroatOffset) > 1e-6 {
		t.Fatalf("%s: throat width %g m", station.ID, width)
	}
	along := Point{X: (merge.X - diverge.X) / width, Y: (merge.Y - diverge.Y) / width}
	middle := Point{X: (diverge.X + merge.X) / 2, Y: (diverge.Y + merge.Y) / 2}
	core := Point{X: (entry.X+exit.X)/2 - middle.X, Y: (entry.Y+exit.Y)/2 - middle.Y}
	length := math.Hypot(core.X, core.Y)
	outward := Point{X: core.X / length, Y: core.Y / length}
	if math.Abs(along.X*outward.X+along.Y*outward.Y) > 1e-9 {
		t.Fatalf("%s: the axes of the frame are not perpendicular", station.ID)
	}
	return sharedTrackFrame{center: Point{X: middle.X - sharedThroatDepth*outward.X, Y: middle.Y - sharedThroatDepth*outward.Y}, along: along, outward: outward}
}

// apply returns a copy of a network with the changes.
func (c sharedTrackChange) apply(t *testing.T, base Network) Network {
	t.Helper()
	n := base.clone()
	nodes := make(map[string]int, len(n.Nodes))
	for index, node := range n.Nodes {
		nodes[node.ID] = index
	}
	set := func(id string, position Point) {
		index, ok := nodes[id]
		if !ok {
			t.Fatalf("unknown node %q", id)
		}
		n.Nodes[index].Position = position
	}
	frames := make(map[string]sharedTrackFrame)
	for _, id := range []string{sharedWaterloo, sharedBank} {
		positions := indexNodePositions(n.Nodes)
		station, _ := n.Station(id)
		frame := sharedTrackStationFrame(t, positions, station)
		frames[id] = frame
		if c.part > 0 {
			throat := cmp.Or(c.throat[id], sharedThroatDepth)
			set(id+"-diverge", frame.at(-sharedThroatOffset, throat))
			set(id+"-merge", frame.at(sharedThroatOffset, throat))
			set(station.Entry, frame.at(-c.part, throat+c.part))
			set(station.Exit, frame.at(c.part, throat+c.part))
			for row, berth := range station.Berths {
				depth := throat + c.part + c.part*float64(row+1)
				set(berth.ID+"-arrival", frame.at(-c.part, depth))
				set(berth.Node, frame.at(0, depth))
				set(berth.ID+"-departure", frame.at(c.part, depth))
			}
		}
		turn := c.turn[id] * math.Pi / 180
		if turn == 0 {
			continue
		}
		ids := []string{id + "-diverge", id + "-merge", station.Entry, station.Exit}
		for _, berth := range station.Berths {
			ids = append(ids, berth.ID+"-arrival", berth.Node, berth.ID+"-departure")
		}
		cos, sin := math.Cos(turn), math.Sin(turn)
		for _, node := range ids {
			p := n.Nodes[nodes[node]].Position
			dx, dy := p.X-frame.center.X, p.Y-frame.center.Y
			set(node, Point{X: frame.center.X + dx*cos - dy*sin, Y: frame.center.Y + dx*sin + dy*cos})
		}
	}
	if c.curve != "" {
		index := slices.IndexFunc(n.Lanes, func(lane Lane) bool { return lane.ID == c.curve })
		if index < 0 {
			t.Fatalf("unknown lane %q", c.curve)
		}
		lane := &n.Lanes[index]
		frame, ok := frames[lane.StationID]
		if !ok {
			t.Fatalf("lane %q is not at a hub", c.curve)
		}
		from, to := n.Nodes[nodes[lane.From]].Position, n.Nodes[nodes[lane.To]].Position
		middle := Point{X: (from.X + to.X) / 2, Y: (from.Y + to.Y) / 2}
		chord := pointDistance(from, to)
		normal := Point{X: -(to.Y - from.Y) / chord, Y: (to.X - from.X) / chord}
		if normal.X*(middle.X-frame.center.X)+normal.Y*(middle.Y-frame.center.Y) < 0 {
			normal = Point{X: -normal.X, Y: -normal.Y}
		}
		lane.Control = &Point{X: middle.X + c.offset*normal.X, Y: middle.Y + c.offset*normal.Y}
	}
	for _, id := range c.remove {
		before := len(n.Lanes)
		n.Lanes = slices.DeleteFunc(n.Lanes, func(lane Lane) bool { return lane.ID == id })
		if len(n.Lanes) == before {
			t.Fatalf("unknown lane %q", id)
		}
	}
	return n
}

// The lanes that section 3 removes.
const (
	sharedMove0501 = sharedWaterloo + "-move-05-01"
	sharedMove0806 = sharedBank + "-move-08-06"
	sharedMove0608 = sharedBank + "-move-06-08"
	sharedMove0802 = sharedBank + "-move-08-02"
	sharedRoadIn05 = sharedWaterloo + "-road-in-05"
)

// sharedTrackAll returns the changes of section 3.5.
func sharedTrackAll() sharedTrackChange {
	return sharedTrackChange{
		part: 41, throat: map[string]float64{sharedWaterloo: 90}, turn: map[string]float64{sharedBank: 5},
		curve: sharedRoadIn05, offset: 30, remove: []string{sharedMove0501, sharedMove0806},
	}
}

// sharedTrackFallback returns the changes of section 3.6.
func sharedTrackFallback() sharedTrackChange {
	c := sharedTrackAll()
	c.remove = append(c.remove, sharedMove0608, sharedMove0802)
	return c
}

// sharedTrackNetworks returns the networks of the study for an input: the
// network of section 2.1, and for the tight input the networks of
// sections 3.5 and 3.6.
func sharedTrackNetworks(t *testing.T, input sharedTrackInput) ([]string, map[string]Network) {
	t.Helper()
	base := sharedTrackBase(t, input.network)
	networks := map[string]Network{"2.1": base}
	if input.name != "tight" {
		return []string{"2.1"}, networks
	}
	networks["3.5"] = sharedTrackAll().apply(t, base)
	networks["3.6"] = sharedTrackFallback().apply(t, base)
	return []string{"2.1", "3.5", "3.6"}, networks
}

// The small-pod rules of section 3.1. The grown body of a small pod
// (classShapeBody), moved by the guideway tolerance sharedTolerance, is in
// a disk of radius sharedSmallRadius about the reference. sharedSmallCap is
// (2 + sqrt 2) times that radius, rounded up to 0.1 m: the longest arc of
// one lane from the reference to a lane end in the disk.
const sharedTolerance = 0.25

var (
	sharedSmallRadius = math.Hypot(sweepFront+shapeGrowth, sweepHalfWidth+shapeGrowth) + sharedTolerance
	sharedSmallCap    = math.Ceil((2+math.Sqrt2)*sharedSmallRadius*10) / 10
)

// sharedChordProjection returns the least projection of a segment of a
// polyline on its chord, as oldLargePathBound computes it, or 0 when a
// segment does not advance.
func sharedChordProjection(points []Point) float64 {
	first, last := points[0], points[len(points)-1]
	chord := pointDistance(first, last)
	if chord <= 0 || !finite(chord) {
		return 0
	}
	ux, uy := (last.X-first.X)/chord, (last.Y-first.Y)/chord
	minimum := 1.0
	for index := 1; index < len(points); index++ {
		a, b := points[index-1], points[index]
		length := pointDistance(a, b)
		if length == 0 {
			continue
		}
		projection := (b.X-a.X)/length*ux + (b.Y-a.Y)/length*uy
		if !(projection > 0) {
			return 0
		}
		minimum = min(minimum, projection)
	}
	return minimum
}

// sharedSmallFront returns the small-pod front of a curved lane with no
// geometry tail: min(6 m / c, sharedSmallCap), and sharedSmallCap when a
// segment does not advance on the chord.
func sharedSmallFront(points []Point) float64 {
	c := sharedChordProjection(points)
	if c <= 0 {
		return sharedSmallCap
	}
	return min(largeEnvelopeRadius/(c*(1-conflictSlack)), sharedSmallCap)
}

// sharedTrackFronts returns the front bound of each lane, by lane index: the
// front of the tail index, and with small set, the small-pod front of each
// curved lane with no geometry tail.
func sharedTrackFronts(n Network, tails map[string]geometryTails, small bool) []float64 {
	positions := indexNodePositions(n.Nodes)
	fronts := make([]float64, len(n.Lanes))
	for index, lane := range n.Lanes {
		bounds := tails[lane.ID]
		fronts[index] = bounds.front
		if small && bounds == (geometryTails{}) && lane.Control != nil {
			fronts[index] = sharedSmallFront(lanePointsBetween(lane, positions[lane.From], positions[lane.To], make([]Point, 0, 65)))
		}
	}
	return fronts
}

// sharedTrackClusters returns the locations that the small-pod rule of the
// maneuver groups adds for each lane of a group: each lane at a node of its
// cluster. A cluster is a group with each group that shares a node with it,
// and so on.
func sharedTrackClusters(n Network) map[int][]int {
	if len(n.ManeuverGroups) == 0 {
		return nil
	}
	maneuvers, err := newManeuverIndex(n, indexNodePositions(n.Nodes))
	if err != nil || maneuvers == nil {
		return nil
	}
	parent := make([]int, len(maneuvers.groups))
	for index := range parent {
		parent[index] = index
	}
	var find func(int) int
	find = func(group int) int {
		if parent[group] != group {
			parent[group] = find(parent[group])
		}
		return parent[group]
	}
	nodeGroup := make(map[string]int)
	for _, lane := range n.Lanes {
		group, ok := maneuvers.laneGroups[lane.ID]
		if !ok {
			continue
		}
		for _, node := range []string{lane.From, lane.To} {
			if other, seen := nodeGroup[node]; seen {
				parent[find(other)] = find(group)
			} else {
				nodeGroup[node] = group
			}
		}
	}
	nodes := make(map[int]map[string]bool)
	for node, group := range nodeGroup {
		root := find(group)
		if nodes[root] == nil {
			nodes[root] = make(map[string]bool)
		}
		nodes[root][node] = true
	}
	lanes := make(map[int][]int)
	for index, lane := range n.Lanes {
		for root, set := range nodes {
			if set[lane.From] || set[lane.To] {
				lanes[root] = append(lanes[root], index)
			}
		}
	}
	clusters := make(map[int][]int)
	for index, lane := range n.Lanes {
		if group, ok := maneuvers.laneGroups[lane.ID]; ok {
			clusters[index] = lanes[find(group)]
		}
	}
	return clusters
}

// sharedTrackReachOptions selects the envelope reach of the study: the
// index of section 3.1, with the anchor front of each lane and the anchors
// within the slack.
type sharedTrackReachOptions struct {
	// small adds the small-pod rules: the small-pod fronts, and the
	// clusters of the maneuver groups.
	small bool
}

func (o sharedTrackReachOptions) String() string {
	text := "each lane"
	if o.small {
		text += ", small-pod rules"
	}
	return text
}

// sharedTrackReach returns the envelope reach of a network with the
// options, with the spans of each lane, and the anchor front of each lane.
// The study changes the fronts and the clusters of the index to add or
// remove the small-pod rules.
func sharedTrackReach(n Network, tails map[string]geometryTails, options sharedTrackReachOptions) (*envelopeReach, []float64) {
	r := newEnvelopeReach(n, tails)
	r.graph.fronts = sharedTrackFronts(n, tails, options.small)
	r.clusterOf, r.clusterLanes = nil, nil
	if options.small {
		r.clusterOf, r.clusterLanes = locationClusters(n, r.locations)
	}
	sharedRebuild(r)
	fronts := make([]float64, len(n.Lanes))
	for lane := range fronts {
		fronts[lane] = r.graph.anchorFront(lane)
	}
	return r, fronts
}

// sharedNetworkFront returns the largest front bound of the lanes of a
// network, and at least largeEnvelopeRadius.
func sharedNetworkFront(fronts []float64) float64 {
	front := largeEnvelopeRadius
	for _, value := range fronts {
		front = max(front, value)
	}
	return front
}

// sharedTrackRefusal is a pair that the audit copy refuses. distance is the
// smallest distance of the parts that the reach does not separate. held is
// true when only the reach denies the plane exemption. cause names the
// first part of the closest piece and two planes of the reach that are not
// separated there.
type sharedTrackRefusal struct {
	first, second string
	distance      float64
	held          bool
	sweep         string
	cause         string
	// crossing gives the first point at which two lanes cross, as
	// distances from the start of each lane.
	crossing string
}

// sharedTrackAuditResult is the result of the audit copy.
type sharedTrackAuditResult struct {
	refusals []sharedTrackRefusal
	shapes   int
}

func (r sharedTrackAuditResult) under(limit float64) int {
	count := 0
	for _, refusal := range r.refusals {
		if refusal.distance < limit {
			count++
		}
	}
	return count
}

// sharedTrackAudit is the pair scan of auditBankGeometry with an envelope
// reach index, recording each refusal in place of the first one. The
// indexes are those of ValidateBankGeometry.
func sharedTrackAudit(t *testing.T, n Network, tails map[string]geometryTails, index *envelopeReach) sharedTrackAuditResult {
	t.Helper()
	audit := bankAudit{tails: tails, retention: newLaneRetention(n, tails)}
	nodes := indexNodePositions(n.Nodes)
	if len(n.Depots) > 0 {
		depots, err := newDepotIndex(n, nodes)
		if err != nil {
			t.Fatal(err)
		}
		audit.depots = depots
		audit.lateral = newDepotLateral(n, depots, tails, indexBerthKeys(n))
	}
	if len(n.ManeuverGroups) > 0 {
		maneuvers, err := newManeuverIndex(n, nodes)
		if err != nil {
			t.Fatal(err)
		}
		audit.maneuvers = maneuvers
		if audit.certified, err = certifyManeuvers(n, maneuvers, indexBerthKeys(n)); err != nil {
			t.Fatal(err)
		}
	}
	retention, maneuvers := audit.retention, audit.maneuvers
	groups := maneuvers != nil
	incident := incidentLanes(n)
	audit.reach = &lazyPlaneReach{network: n}
	audit.envelope = &lazyEnvelopeReach{network: n, tails: tails}
	// Consume the once, so that get returns the index of the study.
	audit.envelope.once.Do(func() { audit.envelope.index = index })
	paths := make([]bankAuditPath, 0, len(n.Lanes))
	for laneIndex, lane := range n.Lanes {
		points := lanePointsBetween(lane, nodes[lane.From], nodes[lane.To], make([]Point, 0, 65))
		path := bankAuditPath{id: lane.ID, location: SafetyLocation{SeparationGroup: lane.SeparationGroup, From: lane.From, To: lane.To}, points: points, min: points[0], max: points[0], large: largeClassSet(lane.VehicleClasses), key: laneKey(laneIndex)}
		path.member = auditMember{spans: retention.laneSpans(lane.ID), speed: lane.SpeedLimit, large: path.large}
		for point := 1; point < len(points); point++ {
			path.member.length += pointDistance(points[point-1], points[point])
		}
		path.locations = []SafetyLocation{path.location}
		for _, point := range points {
			path.min.X, path.min.Y = min(path.min.X, point.X), min(path.min.Y, point.Y)
			path.max.X, path.max.Y = max(path.max.X, point.X), max(path.max.Y, point.Y)
		}
		paths = append(paths, path)
	}
	berthIndex := 0
	for _, station := range n.Stations {
		for _, berth := range station.Berths {
			position := nodes[berth.Node]
			path := bankAuditPath{id: berth.ID, location: SafetyLocation{SeparationGroup: berth.SeparationGroup, From: berth.Node, To: berth.Node}, points: []Point{position, position}, min: position, max: position, large: largeBerth(station, berth), key: berthKey(berthIndex)}
			berthIndex++
			path.member = auditMember{spans: retention.nodeSpans(berth.Node), large: path.large}
			path.locations = berthGeometryLocations(berth, incident[berth.Node])
			paths = append(paths, path)
		}
	}
	slices.SortStableFunc(paths, func(a, b bankAuditPath) int { return cmp.Compare(a.min.X, b.min.X) })
	audit.shapes = audit.shapePairs(n, paths, largeClearance)
	result := sharedTrackAuditResult{shapes: len(audit.shapes.pairs)}
	certifier := newShapeCertifier(n, shapeOptions)
	for i, first := range paths {
		for _, second := range paths[i+1:] {
			if second.min.X-first.max.X >= largeClearance {
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
			refused, distance := false, math.Inf(1)
			var closest pairPiece
			pieces := audit.pieces(first, second)
			for _, piece := range pieces {
				for _, part := range planes.cut(piece) {
					a, b := first.points, second.points
					if len(pieces) > 1 || planes.ranges != nil {
						a, b = polylinePiece(a, part.first[0], part.first[1]), polylinePiece(b, part.second[0], part.second[1])
					}
					if gap := shapeStudyGap(a, b); gap < distance {
						distance, closest = gap, part
					}
					refused = refused || bankPathsClose(a, b, part.clearance)
				}
			}
			if !refused {
				continue
			}
			refusal := sharedTrackRefusal{first: first.id, second: second.id, distance: distance, held: planes.held, sweep: "-"}
			if first.key.kind == laneKeyKind && second.key.kind == laneKeyKind {
				refusal.sweep = sharedSweepNames[certifier.certify(first.key.index, second.key.index, planes.ranges)]
			}
			if planes.ranges != nil {
				refusal.cause = sharedTrackCause(n, index, first.key, second.key, closest)
			}
			if first.key.kind == laneKeyKind && second.key.kind == laneKeyKind {
				refusal.crossing = sharedTrackCrossing(first.points, second.points)
			}
			result.refusals = append(result.refusals, refusal)
		}
	}
	return result
}

var sharedSweepNames = map[sweepStatus]string{sweepClear: "clear", sweepPotential: "potential", sweepUnresolved: "unresolved", sweepCollision: "collision"}

// sharedTrackCause names the parts of a piece and two planes of the reach,
// one of each path, that are not separated in the middle of the parts.
func sharedTrackCause(n Network, r *envelopeReach, first, second separationKey, part pairPiece) string {
	at := func(key separationKey, bounds [2]float64) *reachSpan {
		if key.kind == berthKeyKind {
			return &r.berths[key.index]
		}
		return r.laneAt(key.index, (bounds[0]+bounds[1])/2)
	}
	a, b := at(first, part.first), at(second, part.second)
	name := func(number int) string {
		if number < len(n.Lanes) {
			return n.Lanes[number].ID
		}
		return fmt.Sprintf("berth %d", number-len(n.Lanes))
	}
	for _, x := range a.locations {
		for _, y := range b.locations {
			if !safetyLocationsSeparated(r.locations[x], r.locations[y]) {
				return fmt.Sprintf("parts %.2f to %.2f m and %.2f to %.2f m, planes %s and %s", part.first[0], part.first[1], part.second[0], part.second[1], name(x), name(y))
			}
		}
	}
	return fmt.Sprintf("parts %.2f to %.2f m and %.2f to %.2f m", part.first[0], part.first[1], part.second[0], part.second[1])
}

// sharedTrackCrossing returns the first point at which two polylines
// cross, as distances from the start and to the end of each, or "".
func sharedTrackCrossing(first, second []Point) string {
	along := 0.0
	for i := 1; i < len(first); i++ {
		a, b := first[i-1], first[i]
		other := 0.0
		for j := 1; j < len(second); j++ {
			c, d := second[j-1], second[j]
			if segmentsCross(a, b, c, d) {
				denominator := (b.X-a.X)*(d.Y-c.Y) - (b.Y-a.Y)*(d.X-c.X)
				if denominator != 0 {
					u := ((c.X-a.X)*(d.Y-c.Y) - (c.Y-a.Y)*(d.X-c.X)) / denominator
					at := Point{X: a.X + u*(b.X-a.X), Y: a.Y + u*(b.Y-a.Y)}
					x, y := along+pointDistance(a, at), other+pointDistance(c, at)
					return fmt.Sprintf(", lanes cross at %.1f m of the first (%.1f m before its end) and %.1f m of the second (%.1f m before its end)", x, polylineLength(first)-x, y, polylineLength(second)-y)
				}
			}
			other += pointDistance(c, d)
		}
		along += pointDistance(a, b)
	}
	return ""
}

func polylineLength(points []Point) float64 {
	length := 0.0
	for index := 1; index < len(points); index++ {
		length += pointDistance(points[index-1], points[index])
	}
	return length
}

// sharedTrackPlace returns the hub of a refusal, or "links".
func sharedTrackPlace(refusal sharedTrackRefusal) string {
	for _, id := range []string{refusal.first, refusal.second} {
		for _, hub := range []string{sharedWaterloo, sharedBank} {
			if strings.HasPrefix(id, hub) {
				return hub[len(hub)-3:]
			}
		}
	}
	return "links"
}

// sharedTrackRefusals runs the audit copy with the options, and logs the
// counts and each refusal.
func sharedTrackRefusals(t *testing.T, name string, n Network, options sharedTrackReachOptions) sharedTrackAuditResult {
	t.Helper()
	tails := indexGeometryTails(n)
	index, _ := sharedTrackReach(n, tails, options)
	result := sharedTrackAudit(t, n, tails, index)
	t.Logf("%s, %s: %d refusals, %d under 4.0 m, %d shape pairs", name, options, len(result.refusals), result.under(4), result.shapes)
	key := fmt.Sprintf("%s, %s", name, options)
	if got, want := [3]int{len(result.refusals), result.under(4), result.shapes}, sharedTrackCounts[key]; got != want {
		t.Errorf("%s: refusals, under 4.0 m, and shape pairs %v, want %v", key, got, want)
	}
	for _, refusal := range result.refusals {
		t.Logf("%s, %s: refuses %s and %s, %.2f m, %s, held %v, sweep %s, %s%s", name, options, refusal.first, refusal.second, refusal.distance, sharedTrackPlace(refusal), refusal.held, refusal.sweep, refusal.cause, refusal.crossing)
	}
	return result
}

// sharedTrackBuiltRefusals runs the audit copy with the index of the
// network. Its counts must be the counts of the
// index of each lane with the small-pod rules, and its first refusal must
// be the error of ValidateBankGeometry. So the pinned counts come from the
// index of the code.
func sharedTrackBuiltRefusals(t *testing.T, name string, n Network) {
	t.Helper()
	tails := indexGeometryTails(n)
	result := sharedTrackAudit(t, n, tails, newEnvelopeReach(n, tails))
	key := fmt.Sprintf("%s, %s", name, sharedTrackReachOptions{small: true})
	got := [3]int{len(result.refusals), result.under(4), result.shapes}
	t.Logf("%s, index of the code: %d refusals, %d under 4.0 m, %d shape pairs", name, got[0], got[1], got[2])
	if want := sharedTrackCounts[key]; got != want {
		t.Errorf("%s, index of the code: refusals, under 4.0 m, and shape pairs %v, want %v of %s", name, got, want, key)
	}
	err := n.ValidateBankGeometry()
	switch {
	case len(result.refusals) == 0 && err != nil:
		t.Errorf("%s: ValidateBankGeometry refuses %v, and the audit copy refuses no pair", name, err)
	case len(result.refusals) > 0 && (err == nil || err.Error() != planeGapText(result.refusals[0].first, result.refusals[0].second, largeClearance)):
		t.Errorf("%s: the first refusal of the audit copy is not that of ValidateBankGeometry: %v", name, err)
	}
}

// TestSharedTrackStudyRefusals runs the audit copy on each network of the
// study with each front bound (sections 2.2, 3.1, 3.5, 3.6, and 2.5).
func TestSharedTrackStudyRefusals(t *testing.T) {
	input := sharedTrackLoad(t)
	names, networks := sharedTrackNetworks(t, input)
	// The index of each lane with the small-pod rules gives the spans of
	// the index of the network.
	base := networks["2.1"]
	tails := indexGeometryTails(base)
	for _, tie := range []struct {
		built   *envelopeReach
		options sharedTrackReachOptions
	}{
		{newEnvelopeReach(base, tails), sharedTrackReachOptions{small: true}},
	} {
		copied, _ := sharedTrackReach(base, tails, tie.options)
		for lane := range base.Lanes {
			if !sharedSpansEqual(tie.built, copied, lane) {
				t.Fatalf("%s: lane %q: the spans of the copy differ from the index", tie.options, base.Lanes[lane].ID)
			}
		}
		if !slices.EqualFunc(tie.built.berths, copied.berths, func(a, b reachSpan) bool {
			return slices.Equal(slices.Compact(slices.Sorted(slices.Values(a.locations))), slices.Compact(slices.Sorted(slices.Values(b.locations))))
		}) {
			t.Fatalf("%s: the berth spans of the copy differ from the index", tie.options)
		}
	}
	for _, name := range names {
		n := networks[name]
		hash := sharedTrackHash(t, n)
		t.Logf("%s %s: %d nodes, %d lanes, network SHA-256 %s", input.name, name, len(n.Nodes), len(n.Lanes), hash)
		if want := sharedTrackBuilt[input.name+" "+name]; want != hash {
			t.Errorf("%s %s: network SHA-256 %s, want %s", input.name, name, hash, want)
		}
		for _, options := range []sharedTrackReachOptions{{}, {small: true}} {
			sharedTrackRefusals(t, input.name+" "+name, n, options)
		}
		sharedTrackBuiltRefusals(t, input.name+" "+name, n)
		sharedTrackLogPreparation(t, input.name+" "+name, n)
	}
	if dir := os.Getenv(sharedTrackOutEnv); dir != "" {
		for _, name := range names {
			sharedTrackWrite(t, input, filepath.Join(dir, fmt.Sprintf("%s-%s.json", input.name, name)), networks[name])
		}
	}
}

// sharedSpansEqual reports whether two indexes give a lane the same spans
// with the same planes.
func sharedSpansEqual(a, b *envelopeReach, lane int) bool {
	first, second := a.laneSpans(lane), b.laneSpans(lane)
	if len(first) != len(second) {
		return false
	}
	for index := range first {
		if first[index].from != second[index].from || first[index].to != second[index].to {
			return false
		}
		x, y := slices.Sorted(slices.Values(first[index].locations)), slices.Sorted(slices.Values(second[index].locations))
		if !slices.Equal(slices.Compact(x), slices.Compact(y)) {
			return false
		}
	}
	return true
}

// sharedTrackLogPreparation logs the large lanes under 40 m, the error of
// preparation, and its plane reach refusal.
func sharedTrackLogPreparation(t *testing.T, name string, n Network) {
	t.Helper()
	t.Logf("%s: large lanes under 40 m: %d %v", name, len(shortStudyLanes(n)), shortStudyLanes(n))
	prepared, err := prepareNetworkTails(n, nil)
	t.Logf("%s: preparation error %v, plane reach refusal %v", name, err, prepared.reach)
}

// sharedTrackWrite writes a project with the network of the study, the
// Express order contract, both Express services, and 4 Express pods at
// the free berths of the two hubs.
func sharedTrackWrite(t *testing.T, input sharedTrackInput, path string, n Network) {
	t.Helper()
	var project map[string]jsontext.Value
	if err := json.Unmarshal(input.raw, &project); err != nil {
		t.Fatal(err)
	}
	var fleet []Placement
	if err := json.Unmarshal(project["fleet"], &fleet); err != nil {
		t.Fatal(err)
	}
	used := make(map[string]bool)
	for _, placement := range fleet {
		used[placement.BerthID] = true
	}
	added := 0
	for _, id := range []string{sharedWaterloo, sharedBank} {
		station, _ := n.Station(id)
		for _, berth := range station.Berths {
			if used[berth.ID] || added == 4 {
				continue
			}
			added++
			fleet = append(fleet, Placement{ID: fmt.Sprintf("express-%02d", added), Class: ExpressClass, StationID: id, BerthID: berth.ID})
		}
	}
	services := []ExpressService{
		{ID: "xs-WLO-BNK", From: sharedWaterloo, To: sharedBank, Class: ExpressClass, PartyLimit: 20},
		{ID: "xs-BNK-WLO", From: sharedBank, To: sharedWaterloo, Class: ExpressClass, PartyLimit: 20},
	}
	for key, value := range map[string]any{"network": n, "fleet": fleet, "expressServices": services, "orderContract": ExpressOrderContract} {
		data, err := json.Marshal(value, jsonv1.DefaultOptionsV1())
		if err != nil {
			t.Fatal(err)
		}
		project[key] = data
	}
	data, err := json.Marshal(project, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s with %d Express pods", path, added)
}

// sharedTrackHubRefusals returns the refusals of the audit copy with the
// index of each lane that have a path of a hub.
func sharedTrackHubRefusals(t *testing.T, n Network, hub string) []sharedTrackRefusal {
	t.Helper()
	tails := indexGeometryTails(n)
	index, _ := sharedTrackReach(n, tails, sharedTrackReachOptions{})
	var refusals []sharedTrackRefusal
	for _, refusal := range sharedTrackAudit(t, n, tails, index).refusals {
		if sharedTrackPlace(refusal) == hub[len(hub)-3:] {
			refusals = append(refusals, refusal)
		}
	}
	return refusals
}

func sharedTrackRefusalText(refusals []sharedTrackRefusal) string {
	parts := make([]string, 0, len(refusals))
	for _, refusal := range refusals {
		parts = append(parts, fmt.Sprintf("%s and %s %.2f m", refusal.first, refusal.second, refusal.distance))
	}
	return fmt.Sprintf("%d [%s]", len(refusals), strings.Join(parts, ", "))
}

// sharedTrackLength returns the length of a lane of a network.
func sharedTrackLength(n Network, id string) float64 {
	positions := indexNodePositions(n.Nodes)
	for _, lane := range n.Lanes {
		if lane.ID == id {
			return indexedLaneLength(lane, positions[lane.From], positions[lane.To])
		}
	}
	return math.NaN()
}

// sharedTrackRouteLengths returns the lengths of the free-flow routes from
// the exit of Waterloo to the entry of Bank and back.
func sharedTrackRouteLengths(t *testing.T, n Network) [2]float64 {
	t.Helper()
	first, _ := n.Station(sharedWaterloo)
	second, _ := n.Station(sharedBank)
	positions := indexNodePositions(n.Nodes)
	var lengths [2]float64
	for index, route := range [][]Lane{shapeStudyHubRoute(t, n, first, second), shapeStudyHubRoute(t, n, second, first)} {
		for _, lane := range route {
			lengths[index] += indexedLaneLength(lane, positions[lane.From], positions[lane.To])
		}
	}
	return lengths
}

// sharedTrackHubPaths returns the shortest and the longest path from the
// diverge of a hub to one of its berths.
func sharedTrackHubPaths(t *testing.T, n Network, hub string) (float64, float64) {
	t.Helper()
	station, _ := n.Station(hub)
	positions := indexNodePositions(n.Nodes)
	shortest, longest := math.Inf(1), math.Inf(-1)
	for _, berth := range station.Berths {
		route, err := n.route(networkRouteInput{from: hub + "-diverge", to: berth.Node})
		if err != nil {
			t.Fatal(err)
		}
		length := 0.0
		for _, lane := range route {
			length += indexedLaneLength(lane, positions[lane.From], positions[lane.To])
		}
		shortest, longest = min(shortest, length), max(longest, length)
	}
	return shortest, longest
}

// TestSharedTrackStudyGeometry logs the front bounds of section 2.3, the
// lane lengths of sections 2.4 and 3.2 to 3.5, and the sweeps of the
// Waterloo curve and the Bank turn in sections 3.3 and 3.4.
func TestSharedTrackStudyGeometry(t *testing.T) {
	input := sharedTrackLoad(t)
	names, networks := sharedTrackNetworks(t, input)
	for _, name := range names {
		n := networks[name]
		tails := indexGeometryTails(n)
		over, near := 0, 0
		for _, lane := range n.Lanes {
			bounds := tails[lane.ID]
			if bounds.front <= largeEnvelopeRadius {
				continue
			}
			over++
			if bounds.front < 6.01 {
				near++
				continue
			}
			t.Logf("%s %s: %s front %.2f m, tail %.2f m, length %.2f m", input.name, name, lane.ID, bounds.front, bounds.tail, sharedTrackLength(n, lane.ID))
		}
		t.Logf("%s %s: %d lanes with a front over 6 m, %d of them under 6.01 m, bound of the network %.2f m", input.name, name, over, near, sharedNetworkFront(sharedTrackFronts(n, tails, false)))
		small, capped, chains, shortest := 0, 0, 0, math.Inf(1)
		retention := newLaneRetention(n, tails)
		for index, front := range sharedTrackFronts(n, tails, true) {
			lane := n.Lanes[index]
			if tails[lane.ID] != (geometryTails{}) {
				continue
			}
			if lane.Control != nil {
				small++
				if front >= sharedSmallCap {
					capped++
				}
			}
			if retention.of(lane.ID) > 0 {
				chains++
				shortest = min(shortest, sharedTrackLength(n, lane.ID))
			}
		}
		clusters := sharedTrackClusters(n)
		held := make(map[int]bool)
		for _, lanes := range clusters {
			for _, lane := range lanes {
				held[lane] = true
			}
		}
		t.Logf("%s %s: small-pod rules: radius %.4f m, %d curved lanes with no geometry tail, %d of them at the cap of %.1f m, %d maneuver group lanes, %d lanes at a node of a cluster, %d lanes with a reduced retention, the shortest %.2f m", input.name, name, sharedSmallRadius, small, capped, sharedSmallCap, len(clusters), len(held), chains, shortest)
		if shortest <= sharedSmallRadius {
			t.Errorf("%s %s: a lane with a reduced retention is %.2f m long, not longer than the radius %.4f m", input.name, name, shortest, sharedSmallRadius)
		}
		for _, options := range []sharedTrackReachOptions{{}, {small: true}} {
			_, bounds := sharedTrackReach(n, tails, options)
			var large []string
			for lane, bound := range bounds {
				if bound >= 6.01 {
					large = append(large, fmt.Sprintf("%s %.2f m", n.Lanes[lane].ID, bound))
				}
			}
			whole := largeEnvelopeRadius
			for _, front := range sharedTrackFronts(n, tails, options.small) {
				whole = max(whole, front)
			}
			if len(large) > 40 {
				large = append(large[:0:0], "the first "+large[0])
			}
			t.Logf("%s %s, %s: bound of the network %.2f m, anchor front of 6.01 m or more on %d lanes: %v", input.name, name, options, whole, len(slices.DeleteFunc(slices.Clone(bounds), func(bound float64) bool { return bound < 6.01 })), large)
		}
		lengths := sharedTrackRouteLengths(t, n)
		t.Logf("%s %s: routes %.1f m from Waterloo to Bank and %.1f m back", input.name, name, lengths[0], lengths[1])
		for _, hub := range []string{sharedWaterloo, sharedBank} {
			shortest, longest := sharedTrackHubPaths(t, n, hub)
			t.Logf("%s %s: %s diverge to berths %.1f to %.1f m, access-in %.2f m, access-out %.2f m", input.name, name, hub, shortest, longest, sharedTrackLength(n, hub+"-access-in"), sharedTrackLength(n, hub+"-access-out"))
		}
		if name == "2.1" {
			positions := indexNodePositions(n.Nodes)
			for _, lane := range n.Lanes {
				if lane.ID != "london-link-098-ab-1" {
					continue
				}
				points := lanePointsBetween(lane, positions[lane.From], positions[lane.To], nil)
				diverge := positions[sharedBank+"-diverge"]
				nearest, at, along := math.Inf(1), 0.0, 0.0
				for index := 1; index < len(points); index++ {
					a, b := points[index-1], points[index]
					if distance := segmentPointDistance(diverge, a, b); distance < nearest {
						length := pointDistance(a, b)
						foot := ((diverge.X-a.X)*(b.X-a.X) + (diverge.Y-a.Y)*(b.Y-a.Y)) / length
						nearest, at = distance, along+min(length, max(0, foot))
					}
					along += pointDistance(points[index-1], points[index])
				}
				t.Logf("%s %s: london-link-098-ab-1 passes %.2f m from the Bank diverge, %.1f m from its start", input.name, name, nearest, at)
			}
			station, _ := n.Station(sharedBank)
			frame := sharedTrackStationFrame(t, indexNodePositions(n.Nodes), station)
			t.Logf("%s %s: heading of the Bank axis %.1f degrees", input.name, name, math.Atan2(frame.outward.Y, frame.outward.X)*180/math.Pi)
		}
	}
	if input.name != "tight" {
		return
	}
	base := networks["2.1"]
	all := networks["3.5"]
	var roads []string
	for _, lane := range base.Lanes {
		if strings.HasPrefix(lane.ID, sharedWaterloo+"-road-") {
			roads = append(roads, fmt.Sprintf("%s %.1f to %.1f m", lane.ID[len(sharedWaterloo)+1:], sharedTrackLength(base, lane.ID), sharedTrackLength(all, lane.ID)))
		}
	}
	t.Logf("Waterloo roads, section 2.1 to section 3.5: %s", strings.Join(roads, ", "))
	// Section 3.3: the control point of road-in-05, with each other change
	// of section 3.5.
	for _, offset := range []float64{0, 10, 20, 30, 40, -10, -20, -30, -40} {
		c := sharedTrackAll()
		c.offset = offset
		if offset == 0 {
			c.curve = ""
		}
		n := c.apply(t, base)
		t.Logf("road-in-05 control %+.0f m (positive away from the center): length %.1f m, Waterloo refusals %s", offset, sharedTrackLength(n, sharedRoadIn05), sharedTrackRefusalText(sharedTrackHubRefusals(t, n, sharedWaterloo)))
	}
	// Section 3.4: the turn of Bank, with the changes of section 3.5 at
	// Waterloo and the hub shape at Bank.
	for _, removed := range [][]string{nil, {sharedMove0806}, {sharedMove0806, sharedMove0608}} {
		for _, turn := range []float64{-20, -15, -10, -5, 0, 5, 10, 15, 20} {
			c := sharedTrackAll()
			c.turn = map[string]float64{sharedBank: turn}
			c.remove = append([]string{sharedMove0501}, removed...)
			n := c.apply(t, base)
			t.Logf("Bank turn %+.0f degrees, removed %v: Bank refusals %s", turn, removed, sharedTrackRefusalText(sharedTrackHubRefusals(t, n, sharedBank)))
		}
	}
}

// sharedTrackCosts holds the free-flow cost of the route from the exit of
// each station to the entry of each other station, by station index, and
// the number of such routes that use each lane.
type sharedTrackCosts struct {
	stations []string
	cost     [][]float64
	uses     map[string]int
}

// newSharedTrackCosts searches from the exit of each station to each node,
// with the station entries out of the forbidden set, so that a route ends
// at an entry and passes no other station.
func newSharedTrackCosts(n Network) sharedTrackCosts {
	graph := newRouteGraph(n)
	forbidden := n.stationForbidden()
	for _, station := range n.Stations {
		delete(forbidden, station.Entry)
	}
	costs := sharedTrackCosts{uses: make(map[string]int)}
	var work routeSearchWork
	for _, from := range n.Stations {
		costs.stations = append(costs.stations, from.ID)
		start := graph.nodes[from.Exit]
		n.searchFrom(networkRouteInput{forbidden: forbidden}, start, -1, graph, &work)
		row := make([]float64, len(n.Stations))
		for index, to := range n.Stations {
			if to.ID == from.ID {
				continue
			}
			end := graph.nodes[to.Entry]
			row[index] = work.distance[end]
			if math.IsInf(row[index], 1) {
				continue
			}
			for node := end; node != start; node = graph.edges[work.previous[node]].from {
				costs.uses[n.Lanes[work.previous[node]].ID]++
			}
		}
		costs.cost = append(costs.cost, row)
	}
	return costs
}

// TestSharedTrackStudyRouteCosts logs the cost of the changes of section
// 3.5 and 3.6 to the free-flow routes between stations, step by step, and
// the routes that use the junction lanes with a large front (sections 2.3
// and 3.5).
func TestSharedTrackStudyRouteCosts(t *testing.T) {
	input := sharedTrackLoad(t)
	if input.name != "tight" {
		t.Skip("the route costs are for the tight input")
	}
	base := sharedTrackBase(t, input.network)
	before := newSharedTrackCosts(base)
	pairs := len(before.stations) * (len(before.stations) - 1)
	for _, id := range []string{sharedMove0501, sharedMove0608, sharedMove0802, sharedMove0806} {
		t.Logf("station pairs whose route uses %s: %d of %d", id, before.uses[id], pairs)
	}
	steps := []struct {
		name   string
		change sharedTrackChange
	}{
		{"hub shapes, Waterloo throat, and Bank turn", sharedTrackChange{part: 41, throat: map[string]float64{sharedWaterloo: 90}, turn: map[string]float64{sharedBank: 5}}},
		{"and the curve of road-in-05", sharedTrackChange{part: 41, throat: map[string]float64{sharedWaterloo: 90}, turn: map[string]float64{sharedBank: 5}, curve: sharedRoadIn05, offset: 30}},
		{"and move-05-01 removed", sharedTrackChange{part: 41, throat: map[string]float64{sharedWaterloo: 90}, turn: map[string]float64{sharedBank: 5}, curve: sharedRoadIn05, offset: 30, remove: []string{sharedMove0501}}},
		{"and move-08-06 removed (section 3.5)", sharedTrackAll()},
		{"and move-06-08 and move-08-02 removed (section 3.6)", sharedTrackFallback()},
	}
	for _, step := range steps {
		after := newSharedTrackCosts(step.change.apply(t, base))
		longer, shorter, unreachable, total, worst := 0, 0, 0, 0.0, 0.0
		var worstPair string
		for i := range before.cost {
			for j := range before.cost[i] {
				if i == j {
					continue
				}
				delta := after.cost[i][j] - before.cost[i][j]
				switch {
				case math.IsInf(after.cost[i][j], 1):
					unreachable++
				case delta > 1e-9:
					longer++
					total += delta
					if delta > worst {
						worst, worstPair = delta, before.stations[i]+" to "+before.stations[j]
					}
				case delta < -1e-9:
					shorter++
				}
			}
		}
		t.Logf("%s: %d of %d pairs longer, %d shorter, %d unreachable, worst +%.1f s (%s), total +%.1f s", step.name, longer, pairs, shorter, unreachable, worst, worstPair, total)
	}
}

// sharedOracle is a copy of appendVehicleSafetyLocations of the earlier
// builds, for a traveling pod and for a pod at a berth. Without local, it
// is the oracle without ShapeClearance, with the largest front of the
// route. With local set, it is the oracle of the first build of slice 1,
// with the local front of the first draft of section 3.1: the least front
// that is at least 6 m and at least the front of each route lane that ends
// at the route distance minus the slack or later and starts within the
// front. The study compares the planes of these oracles with the oracle of
// the code. fronts holds the
// front bound of each lane by lane index, and clusters the locations that
// the small-pod rule of the maneuver groups adds for a group lane. The rule
// applies to each location of the envelope: the lanes of the route, the
// origin berth and the lanes at its node before the origin release, and the
// berth and the lanes at its node of a pod at a berth.
type sharedOracle struct {
	s        *Simulation
	fronts   []float64
	local    bool
	clusters map[SafetyLocation][]SafetyLocation
}

func (o *sharedOracle) envelope(v *vehicle, distance float64) []SafetyLocation {
	var locations []SafetyLocation
	add := func(location SafetyLocation) {
		if !slices.Contains(locations, location) {
			locations = append(locations, location)
		}
	}
	if v.Pod.LaneID != "" {
		add(o.s.laneSafety[v.Pod.LaneID])
	}
	if v.Pod.BerthID != "" {
		add(o.s.berthSafety[v.Pod.BerthID])
	}
	if v.Pod.Activity == Traveling {
		front := largeEnvelopeRadius
		for index, lane := range v.Route {
			start, end := v.blocks.lanes[index].start, v.blocks.lanes[index+1].start
			if o.local {
				if end < distance-envelopeReachSlack {
					continue
				}
				if start > distance+front {
					break
				}
			}
			front = max(front, o.fronts[o.s.graph.lanes[lane.ID]])
		}
		for index, lane := range v.Route {
			start, end := v.blocks.lanes[index].start, v.blocks.lanes[index+1].start
			if start > distance+front {
				break
			}
			tail := largeClearance
			if cells := v.blocks.lanes[index].cells; cells != nil {
				tail = max(tail, cells.tail)
			}
			if end >= distance-tail {
				add(o.s.laneSafety[lane.ID])
			}
		}
		if !v.originReleased && v.origin.ID != "" {
			add(o.s.berthSafety[v.origin.ID])
		}
	}
	if v.Pod.Activity != Traveling || !v.originReleased {
		node := v.origin.Node
		if berthNode, ok := o.s.berthNodes[v.Pod.BerthID]; ok && v.Pod.BerthID != "" {
			node = berthNode
		}
		for _, lane := range o.s.network.Lanes {
			if lane.From == node || lane.To == node {
				add(o.s.laneSafety[lane.ID])
			}
		}
	}
	for _, location := range slices.Clone(locations) {
		for _, extra := range o.clusters[location] {
			add(extra)
		}
	}
	return locations
}

// sharedBody is a body of the containment test about the reference of a
// pod. Each body, moved by the guideway tolerance, is in the disk of the
// proof: radius 5.404 m for a large pod and sharedSmallRadius for a small
// pod.
type sharedBody struct {
	name string
	kind diskStudyKind
	body shapeBody
}

// sharedBodies are the bodies of the containment test: the 10 m by 2.5 m
// gate body of SC6 and the grown large body for a large pod, and the small
// body of the sweep and the grown small body for a small pod.
var sharedBodies = []sharedBody{
	{"gate", diskLarge, shapeBody{rear: 5, front: 5, halfWidth: 1.25}},
	{"large grown", diskLarge, classShapeBody(ExpressClass)},
	{"small", diskSmall, shapeBody{rear: lateralRear, front: sweepFront, halfWidth: sweepHalfWidth}},
	{"small grown", diskSmall, classShapeBody(CompactClass)},
}

// sharedContainment drives the containment test on one network.
type sharedContainment struct {
	name      string
	s         *Simulation
	points    [][]Point
	arcs      [][]float64
	low, high []Point
	in, out   map[string][]int
	// bounds holds the anchor front of each lane, and behind the longest
	// tail or origin tail.
	bounds []float64
	behind float64
	// main is the index of the code, which the oracle of the code reads.
	// plain and six are simulations whose oracles read controls of the
	// index: without the small-pod rules, and with the anchor front 6 m on
	// each lane. previous and gate are the copies of the earlier oracles
	// (see sharedOracle).
	main           *envelopeReach
	plain, six     *Simulation
	previous, gate *sharedOracle
	// reference adds the parity of the oracle with the reference
	// construction (see referenceVehicleSafetyLocations) at each pose. It
	// scans the network, so only the tests of small networks set it.
	reference bool
	counts    map[string]int
	examples  map[string][]string
}

// sharedVariant returns a copy of a simulation whose oracle reads an index
// with the given front of each lane, and with the small-pod rule of the
// maneuver groups when rules is set.
func sharedVariant(s *Simulation, fronts []float64, rules bool) *Simulation {
	indexes := *s.networkIndexes
	r := newEnvelopeReach(s.network, s.tails)
	r.graph.fronts = fronts
	if !rules {
		r.clusterOf, r.clusterLanes = nil, nil
		indexes.clusterSafety = nil
	}
	sharedRebuild(r)
	indexes.envelopes = r
	return &Simulation{networkIndexes: &indexes}
}

// sharedRebuild builds the spans of each berth again, and the spans of each
// lane, after a change of the fronts or of the clusters of an index.
func sharedRebuild(r *envelopeReach) {
	r.berths = r.berths[:0]
	for _, node := range r.graph.berthNode {
		r.items = r.graph.nodeItems(r.items[:0], node, math.Inf(1))
		r.berths = append(r.berths, r.spans(r.items, 0)...)
	}
	for lane := range r.lanes {
		r.lanes[lane] = nil
		r.laneSpans(lane)
	}
}

func newSharedContainment(t *testing.T, name string, n Network) *sharedContainment {
	t.Helper()
	s := diskStudyPrepare(t, n)
	network, tails := s.network, s.tails
	c := &sharedContainment{name: name, s: s, in: make(map[string][]int), out: make(map[string][]int), counts: make(map[string]int), examples: make(map[string][]string)}
	positions := indexNodePositions(network.Nodes)
	for index, lane := range network.Lanes {
		points := lanePointsBetween(lane, positions[lane.From], positions[lane.To], make([]Point, 0, 65))
		arcs := make([]float64, len(points))
		low, high := points[0], points[0]
		for point := 1; point < len(points); point++ {
			arcs[point] = arcs[point-1] + pointDistance(points[point-1], points[point])
			low = Point{X: min(low.X, points[point].X), Y: min(low.Y, points[point].Y)}
			high = Point{X: max(high.X, points[point].X), Y: max(high.Y, points[point].Y)}
		}
		c.points, c.arcs = append(c.points, points), append(c.arcs, arcs)
		c.low, c.high = append(c.low, low), append(c.high, high)
		c.in[lane.To] = append(c.in[lane.To], index)
		c.out[lane.From] = append(c.out[lane.From], index)
	}
	clusters := make(map[SafetyLocation][]SafetyLocation)
	for lane, cluster := range sharedTrackClusters(network) {
		for _, other := range cluster {
			clusters[s.laneSafety[network.Lanes[lane].ID]] = append(clusters[s.laneSafety[network.Lanes[lane].ID]], s.laneSafety[network.Lanes[other].ID])
		}
	}
	small, plain := sharedTrackFronts(network, tails, true), sharedTrackFronts(network, tails, false)
	c.previous = &sharedOracle{s: s, fronts: small, local: true, clusters: clusters}
	c.gate = &sharedOracle{s: s, fronts: plain}
	// When the network has no compiled index, main is nil, and the
	// controls are the simulation itself.
	c.main, c.plain, c.six = s.envelopes, s, s
	index := s.envelopes
	if index == nil {
		index = newEnvelopeReach(network, tails)
	} else {
		c.plain = sharedVariant(s, plain, false)
		c.six = sharedVariant(s, make([]float64, len(network.Lanes)), true)
	}
	for lane := range network.Lanes {
		c.bounds = append(c.bounds, max(largeEnvelopeRadius, small[lane]))
		if got := index.graph.anchorFront(lane); c.main != nil && got != c.bounds[lane] {
			c.record("gate: the anchor front of the index differs from the copy", fmt.Sprintf("lane %s: %g m, the copy %g m", network.Lanes[lane].ID, got, c.bounds[lane]))
		}
	}
	c.behind = index.graph.behind
	return c
}

func (c *sharedContainment) record(key, example string) {
	c.counts[key]++
	if len(c.examples[key]) < 3 {
		c.examples[key] = append(c.examples[key], example)
	}
}

// pointAt returns the point and the heading of a lane at a distance.
func (c *sharedContainment) pointAt(lane int, distance float64) (Point, float64) {
	points, arcs := c.points[lane], c.arcs[lane]
	heading := 0.0
	for segment := 1; segment < len(points); segment++ {
		a, b := points[segment-1], points[segment]
		if a == b {
			continue
		}
		heading = math.Atan2(b.Y-a.Y, b.X-a.X)
		if distance <= arcs[segment] || segment == len(points)-1 {
			f := min(1, max(0, (distance-arcs[segment-1])/(arcs[segment]-arcs[segment-1])))
			return Point{X: a.X + f*(b.X-a.X), Y: a.Y + f*(b.Y-a.Y)}, heading
		}
	}
	return points[len(points)-1], heading
}

// support walks a route from a lane at a route index, ahead and behind, from
// a position, while each vertex is in the body. It returns the route
// indexes of the first and last lanes that the walk meets, and whether the
// walk reached the end or the start of the route inside the body.
func (c *sharedContainment) support(route []int, at int, position float64, inside func(Point) bool) (lo, hi int, ahead, behind bool) {
	hi = at
	points, arcs := c.points[route[at]], c.arcs[route[at]]
	vertex := slices.IndexFunc(arcs, func(arc float64) bool { return arc > position })
	if vertex < 0 {
		vertex = len(points)
	}
	for {
		for vertex < len(points) && inside(points[vertex]) {
			vertex++
		}
		if vertex < len(points) {
			break
		}
		if hi+1 == len(route) {
			ahead = true
			break
		}
		hi++
		points, vertex = c.points[route[hi]], 1
	}
	lo = at
	points, arcs = c.points[route[at]], c.arcs[route[at]]
	vertex = len(arcs) - 1
	for vertex >= 0 && arcs[vertex] >= position {
		vertex--
	}
	for {
		for vertex >= 0 && inside(points[vertex]) {
			vertex--
		}
		if vertex >= 0 {
			break
		}
		if lo == 0 {
			behind = true
			break
		}
		lo--
		points = c.points[route[lo]]
		vertex = len(points) - 2
	}
	return lo, hi, ahead, behind
}

// sharedSameLocations reports whether two envelopes hold the same planes.
func sharedSameLocations(a, b []SafetyLocation) bool {
	return len(a) == len(b) && !slices.ContainsFunc(a, func(location SafetyLocation) bool { return !slices.Contains(b, location) })
}

// held reports whether a span holds a location, with the planes of rule 2.
func (c *sharedContainment) held(index *envelopeReach, span *reachSpan, location SafetyLocation) bool {
	return slices.ContainsFunc(index.closure(span.locations), func(number int) bool { return index.locations[number] == location })
}

// open reports whether a route can go on at a node: whether the node has a
// lane that admits a kind and is not on the route, out of the node when
// ahead is set, and into it otherwise. A route does not use a lane two
// times. So a support that reaches an end of a route at a node that is not
// open reaches a real end of the route.
func (c *sharedContainment) open(route []int, node string, ahead bool, kind diskStudyKind) bool {
	lanes := c.in[node]
	if ahead {
		lanes = c.out[node]
	}
	return slices.ContainsFunc(lanes, func(lane int) bool {
		return kind.allows(c.s.network.Lanes[lane].VehicleClasses) && !slices.Contains(route, lane)
	})
}

// sharedShifts are the shifts of the route distance and of the published
// position that each pose checks: two restoreTolerance steps each way.
var sharedShifts = []float64{0, -2 * restoreTolerance, 2 * restoreTolerance}

// sharedPublished is a published lane position of a pose: a route index
// and a position on its lane.
type sharedPublished struct {
	at       int
	position float64
}

// published returns the published lane positions of a pose at a position
// of the lane at a route index: the position shifted by each of
// sharedShifts, on the lane or, past an end of the lane, on the lane next
// to it on the route. At an end of the lane, the published lane can also
// be the next lane, so each such position is also on that lane.
func (c *sharedContainment) published(v *vehicle, at int, position float64) []sharedPublished {
	length := func(index int) float64 { return v.blocks.lanes[index+1].start - v.blocks.lanes[index].start }
	var list []sharedPublished
	for _, shift := range sharedShifts {
		q := position + shift
		list = append(list, sharedPublished{at, min(length(at), max(0, q))})
		if q >= length(at) && at+1 < len(v.Route) {
			list = append(list, sharedPublished{at + 1, max(0, q-length(at))})
		}
		if q <= 0 && at > 0 {
			list = append(list, sharedPublished{at - 1, min(length(at-1), length(at-1)+q)})
		}
	}
	return list
}

// pose checks a pod of a kind on a route at a position of the lane at a
// route index, with a heading and a reference point. It reads the oracle
// at each published position of the pose (see published), and each body at
// the reference and moved by the guideway tolerance to each side.
func (c *sharedContainment) pose(route []int, at int, position float64, reference Point, heading float64, kind diskStudyKind, v *vehicle) {
	lanes := c.s.network.Lanes
	lane := route[at]
	where := func() string {
		ids := make([]string, len(route))
		for index, id := range route {
			ids[index] = lanes[id].ID
		}
		origin := ""
		if !v.originReleased {
			origin = ", origin " + v.origin.ID + " held"
		}
		return fmt.Sprintf("%s pod on %s at %.2f m, heading %.1f degrees%s, route %s", diskStudyKindNames[kind], lanes[lane].ID, position, heading*180/math.Pi, origin, strings.Join(ids, " "))
	}
	name := diskStudyKindNames[kind]
	c.counts["poses "+name]++
	if !v.originReleased {
		c.counts["poses with the origin held, "+name]++
	}
	type envelopes struct{ main, plain, six, previous, built []SafetyLocation }
	var shifted []envelopes
	for _, published := range c.published(v, at, position) {
		v.Pod.LaneID, v.Pod.LaneDistance = lanes[route[published.at]].ID, published.position
		distance := v.blocks.lanes[published.at].start + published.position
		e := envelopes{
			c.s.appendVehicleSafetyLocations(nil, v), c.plain.appendVehicleSafetyLocations(nil, v), c.six.appendVehicleSafetyLocations(nil, v),
			c.previous.envelope(v, distance), c.gate.envelope(v, distance),
		}
		shifted = append(shifted, e)
		if c.main == nil {
			continue
		}
		span := c.main.laneAt(route[published.at], published.position)
		if missing := slices.IndexFunc(e.main, func(location SafetyLocation) bool { return !c.held(c.main, span, location) }); missing >= 0 {
			c.record("gate: the oracle holds a plane that its span does not hold", fmt.Sprintf("%s, published %s at %g m: plane %+v", where(), lanes[route[published.at]].ID, published.position, e.main[missing]))
		}
		if c.reference && !slices.Equal(referenceVehicleSafetyLocations(c.s, v), e.main) {
			c.record("gate: the oracle differs from the reference construction", fmt.Sprintf("%s, published %s at %g m", where(), lanes[route[published.at]].ID, published.position))
		}
	}
	v.Pod.LaneID, v.Pod.LaneDistance = lanes[lane].ID, position
	c.counts["planes, poses, "+name]++
	c.counts["planes, oracle, "+name] += len(shifted[0].main)
	c.counts["planes, oracle of the first build of slice 1, "+name] += len(shifted[0].previous)
	c.counts["planes, oracle with the route maximum, "+name] += len(shifted[0].built)
	cos, sin := math.Cos(heading), math.Sin(heading)
	for _, body := range sharedBodies {
		if body.kind != kind {
			continue
		}
		for _, side := range []float64{0, -sharedTolerance, sharedTolerance} {
			moved := Point{X: reference.X - side*sin, Y: reference.Y + side*cos}
			inside := func(q Point) bool {
				dx, dy := q.X-moved.X, q.Y-moved.Y
				x, y := dx*cos+dy*sin, -dx*sin+dy*cos
				return x >= -body.body.rear && x <= body.body.front && math.Abs(y) <= body.body.halfWidth
			}
			c.body(route, at, position, moved, heading, side, kind, v, body, inside, shifted[0].main, where, func(location SafetyLocation, index int, between string) {
				for _, check := range []struct {
					key      string
					envelope func(envelopes) []SafetyLocation
				}{
					{"support miss: oracle, " + body.name, func(e envelopes) []SafetyLocation { return e.main }},
					{"control, support miss: oracle without the small-pod rules, " + body.name, func(e envelopes) []SafetyLocation { return e.plain }},
					{"control, support miss: oracle with the anchor front 6 m, " + body.name, func(e envelopes) []SafetyLocation { return e.six }},
					{"earlier oracle, support miss: oracle of the first build of slice 1, " + body.name, func(e envelopes) []SafetyLocation { return e.previous }},
					{"earlier oracle, support miss: oracle with the route maximum, " + body.name, func(e envelopes) []SafetyLocation { return e.built }},
				} {
					if slices.ContainsFunc(shifted, func(e envelopes) bool { return !slices.Contains(check.envelope(e), location) }) {
						c.record(check.key+", "+between, fmt.Sprintf("%s: support lane %s", where(), lanes[route[index]].ID))
					}
				}
			})
		}
	}
}

// body checks one body of a pose at a reference point. It computes the
// support, checks each support lane with check, and classifies each route
// lane off the support that enters the body.
func (c *sharedContainment) body(route []int, at int, position float64, reference Point, heading, side float64, kind diskStudyKind, v *vehicle, body sharedBody, inside func(Point) bool, envelope []SafetyLocation, where func() string, check func(SafetyLocation, int, string)) {
	lanes := c.s.network.Lanes
	if side != 0 {
		c.counts["body poses moved by the guideway tolerance, "+body.name]++
	}
	lo, hi, ahead, behind := c.support(route, at, position, inside)
	if ahead {
		if c.open(route, lanes[route[hi]].To, true, kind) {
			c.record("support miss: the support reaches the end of the route window, "+body.name, where())
		} else {
			c.counts["support reaches a real end of the route, "+body.name]++
		}
	}
	if behind {
		switch {
		case !v.originReleased && lo == 0:
			c.counts["support reaches the held origin node, "+body.name]++
		case c.open(route, lanes[route[lo]].From, false, kind):
			c.record("support miss: the support reaches the start of the route window, "+body.name, where())
		default:
			c.counts["support reaches a real end of the route, "+body.name]++
		}
	}
	if hi-lo+1 > 2 {
		with := "no maneuver group lane"
		if slices.ContainsFunc(route[lo:hi+1], func(other int) bool { return c.s.maneuvers.groupLane(lanes[other].ID) }) {
			with = "a maneuver group lane"
		}
		c.record(fmt.Sprintf("support of more than 2 lanes with %s, %s", with, body.name), where())
	} else {
		c.counts[fmt.Sprintf("support of %d lanes, %s", hi-lo+1, body.name)]++
	}
	for index := lo; index <= hi; index++ {
		check(c.s.laneSafety[lanes[route[index]].ID], index, c.missKind(route, at, index))
	}
	// The route lanes off the support that enter the body.
	reach := math.Hypot(max(body.body.front, body.body.rear), body.body.halfWidth)
	for index, other := range route {
		if index >= lo && index <= hi || c.low[other].X > reference.X+reach || c.high[other].X < reference.X-reach || c.low[other].Y > reference.Y+reach || c.high[other].Y < reference.Y-reach {
			continue
		}
		points := c.points[other]
		hit := false
		for segment := 1; segment < len(points) && !hit; segment++ {
			hit = segmentHitsBody(points[segment-1], points[segment], reference, heading, body.body)
		}
		if !hit {
			continue
		}
		location := c.s.laneSafety[lanes[other].ID]
		key := "off-support lane in the body, declared separated from the envelope (A1), " + body.name
		if holds(envelope, location) {
			key = "off-support lane in the body, tested through the envelope, " + body.name
		}
		c.record(key, fmt.Sprintf("%s: lane %s, route index %d", where(), lanes[other].ID, index))
	}
}

// missKind names the lanes between the lane at a route index and a missed
// support lane: "maneuver group" when one of them is a group lane, and
// otherwise "curve" or "straight" for the lane of the pod.
func (c *sharedContainment) missKind(route []int, at, index int) string {
	for between := min(at, index); between <= max(at, index); between++ {
		if c.s.maneuvers.groupLane(c.s.network.Lanes[route[between]].ID) {
			return "maneuver group"
		}
	}
	if c.s.network.Lanes[route[at]].Control != nil {
		return "curve"
	}
	return "straight"
}

// positions returns the positions of a lane at a route index that the test
// checks: each 0.5 m of the first and last 16 m, the middle, and the
// positions at which the oracle or the index adds or drops a plane.
func (c *sharedContainment) positions(v *vehicle, route []int, at int) []float64 {
	blocks := v.blocks.lanes
	start, length := blocks[at].start, blocks[at+1].start-blocks[at].start
	positions := []float64{length / 2}
	for step := 0.0; step <= 16; step += 0.5 {
		positions = append(positions, step, length-step)
	}
	front := largeEnvelopeRadius
	for _, lane := range route {
		front = max(front, c.gate.fronts[lane])
	}
	for index := range v.Route {
		tail := largeClearance
		if cells := blocks[index].cells; cells != nil {
			tail = max(tail, cells.tail)
		}
		for _, edge := range []float64{blocks[index+1].start + tail - start, blocks[index].start - c.bounds[route[at]] - start, blocks[index].start - front - start} {
			positions = append(positions, edge, edge-2*envelopeReachSlack, edge+2*envelopeReachSlack)
		}
	}
	kept := positions[:0]
	for _, position := range positions {
		if position >= 0 && position <= length {
			kept = append(kept, position)
		}
	}
	slices.Sort(kept)
	return slices.Compact(kept)
}

// drive checks each pose of the lane at each route index of a route, for
// a pod of a kind, after its origin release.
func (c *sharedContainment) drive(route []int, kind diskStudyKind, from, to int) {
	c.driveFrom(route, kind, from, to, Berth{}, 0)
}

// driveFrom is drive with an origin berth that the pod holds up to the route
// distance held. Each pose at a route distance after held has the origin
// released. With no origin, each pose has it released.
func (c *sharedContainment) driveFrom(route []int, kind diskStudyKind, from, to int, origin Berth, held float64) {
	lanes := make([]Lane, len(route))
	for index, lane := range route {
		lanes[index] = c.s.network.Lanes[lane]
	}
	v := &vehicle{Pod: Pod{ID: "study", Class: kind.class(), Activity: Traveling}}
	c.s.setVehicleRoute(v, lanes)
	v.origin = origin
	for at := from; at < to; at++ {
		v.Pod.LaneID = lanes[at].ID
		lane := route[at]
		start := v.blocks.lanes[at].start
		for _, position := range c.positions(v, route, at) {
			v.originReleased = origin.ID == "" || start+position > held
			reference, heading := c.pointAt(lane, position)
			c.pose(route, at, position, reference, heading, kind, v)
		}
		if at+1 == len(route) {
			continue
		}
		// The join turn toward the next lane.
		end, before := c.pointAt(lane, c.arcs[lane][len(c.arcs[lane])-1])
		_, after := c.pointAt(route[at+1], 0)
		sweep := math.Remainder(after-before, 2*math.Pi)
		v.originReleased = origin.ID == "" || start+c.arcs[lane][len(c.arcs[lane])-1] > held
		for part := 1; part < diskStudyTurns; part++ {
			c.pose(route, at, c.arcs[lane][len(c.arcs[lane])-1], end, before+sweep*float64(part)/diskStudyTurns, kind, v)
		}
	}
}

// origins drives, for a kind, the routes out of each berth that admits the
// kind, with the origin held up to the origin tail of its node.
func (c *sharedContainment) origins(kind diskStudyKind) {
	g := c.main.graph
	nodes := make(map[string]int)
	for lane, id := range c.s.network.Lanes {
		nodes[id.From] = g.from[lane]
	}
	for _, station := range c.s.network.Stations {
		for _, berth := range station.Berths {
			if !kind.allows(sharedBerthClasses(station, berth)) {
				continue
			}
			node, ok := nodes[berth.Node]
			if !ok {
				continue
			}
			held := g.originAt[node]
			for _, first := range c.out[berth.Node] {
				if !kind.allows(c.s.network.Lanes[first].VehicleClasses) {
					continue
				}
				route := c.extension(first, kind, true, held+60, 0, nil)
				to := 0
				for distance := 0.0; to < len(route) && distance <= held; to++ {
					distance += c.arcs[route[to]][len(c.arcs[route[to]])-1]
				}
				c.driveFrom(route, kind, 0, to, berth, held)
			}
		}
	}
}

// sharedBerthClasses returns the classes of a berth, or of its station when
// the berth has none.
func sharedBerthClasses(station Station, berth Berth) ClassSet {
	if berth.VehicleClasses != 0 {
		return berth.VehicleClasses
	}
	return station.VehicleClasses
}

// stationary checks a pod of a kind at each berth that admits the kind:
// the oracle against the built oracle and the berth span of the index, and
// each body at the berth node, at the heading of each lane at the node and
// moved by the guideway tolerance to each side. The support of a body at a
// berth is each lane at the node that enters the body, and each lane at the
// other node of a support lane when that node is in the body.
func (c *sharedContainment) stationary(kind diskStudyKind) {
	network := c.s.network
	lanes := network.Lanes
	positions := indexNodePositions(network.Nodes)
	number := -1
	for _, station := range network.Stations {
		for _, berth := range station.Berths {
			number++
			if !kind.allows(sharedBerthClasses(station, berth)) {
				continue
			}
			v := &vehicle{Pod: Pod{ID: "study", Class: kind.class(), Activity: Idle, BerthID: berth.ID}, originReleased: true}
			where := func() string { return fmt.Sprintf("%s pod at berth %s", diskStudyKindNames[kind], berth.ID) }
			c.counts["stationary berths "+diskStudyKindNames[kind]]++
			envelope, plain, built := c.s.appendVehicleSafetyLocations(nil, v), c.plain.appendVehicleSafetyLocations(nil, v), c.gate.envelope(v, 0)
			if copied := c.previous.envelope(v, 0); !sharedSameLocations(copied, envelope) {
				c.record("gate: the oracle at a berth differs from the copy of the oracle of the first build of slice 1", where())
			}
			if c.reference && !slices.Equal(referenceVehicleSafetyLocations(c.s, v), envelope) {
				c.record("gate: the oracle at a berth differs from the reference construction", where())
			}
			if missing := slices.IndexFunc(envelope, func(location SafetyLocation) bool {
				return c.main != nil && !c.held(c.main, &c.main.berths[number], location)
			}); missing >= 0 {
				c.record("gate: the oracle at a berth holds a plane that the span of the berth does not hold", fmt.Sprintf("%s: plane %+v", where(), envelope[missing]))
			}
			reference := positions[berth.Node]
			var headings []float64
			for _, lane := range c.out[berth.Node] {
				_, heading := c.pointAt(lane, 0)
				headings = append(headings, heading)
			}
			for _, lane := range c.in[berth.Node] {
				_, heading := c.pointAt(lane, c.arcs[lane][len(c.arcs[lane])-1])
				headings = append(headings, heading)
			}
			for _, heading := range headings {
				cos, sin := math.Cos(heading), math.Sin(heading)
				for _, body := range sharedBodies {
					if body.kind != kind {
						continue
					}
					for _, side := range []float64{0, -sharedTolerance, sharedTolerance} {
						moved := Point{X: reference.X - side*sin, Y: reference.Y + side*cos}
						inside := func(q Point) bool {
							dx, dy := q.X-moved.X, q.Y-moved.Y
							x, y := dx*cos+dy*sin, -dx*sin+dy*cos
							return x >= -body.body.rear && x <= body.body.front && math.Abs(y) <= body.body.halfWidth
						}
						hits := func(lane int) bool {
							points := c.points[lane]
							for segment := 1; segment < len(points); segment++ {
								if segmentHitsBody(points[segment-1], points[segment], moved, heading, body.body) {
									return true
								}
							}
							return false
						}
						c.counts["stationary body poses, "+body.name]++
						support := make(map[int]bool)
						queue := []string{berth.Node}
						seen := map[string]bool{berth.Node: true}
						for len(queue) > 0 {
							node := queue[0]
							queue = queue[1:]
							for _, lane := range append(slices.Clone(c.in[node]), c.out[node]...) {
								if support[lane] || !hits(lane) {
									continue
								}
								support[lane] = true
								for _, other := range []string{lanes[lane].From, lanes[lane].To} {
									if !seen[other] && inside(positions[other]) {
										seen[other] = true
										queue = append(queue, other)
									}
								}
							}
						}
						if len(seen) > 1 {
							c.record("stationary support with more than one node, "+body.name, where())
						}
						for lane := range support {
							location := c.s.laneSafety[lanes[lane].ID]
							for _, check := range []struct {
								key      string
								envelope []SafetyLocation
							}{
								{"support miss: oracle at a berth, " + body.name, envelope},
								{"control, support miss: oracle without the small-pod rules at a berth, " + body.name, plain},
								{"earlier oracle, support miss: oracle at a berth without ShapeClearance, " + body.name, built},
							} {
								if !slices.Contains(check.envelope, location) {
									c.record(check.key, fmt.Sprintf("%s, heading %.1f degrees: support lane %s", where(), heading*180/math.Pi, lanes[lane].ID))
								}
							}
						}
						reach := math.Hypot(max(body.body.front, body.body.rear), body.body.halfWidth)
						for lane := range lanes {
							if support[lane] || c.low[lane].X > moved.X+reach || c.high[lane].X < moved.X-reach || c.low[lane].Y > moved.Y+reach || c.high[lane].Y < moved.Y-reach || !hits(lane) {
								continue
							}
							location := c.s.laneSafety[lanes[lane].ID]
							key := "off-support lane in the body at a berth, declared separated from the envelope (A1), " + body.name
							if holds(envelope, location) {
								key = "off-support lane in the body at a berth, tested through the envelope, " + body.name
							}
							c.record(key, fmt.Sprintf("%s, heading %.1f degrees: lane %s", where(), heading*180/math.Pi, lanes[lane].ID))
						}
					}
				}
			}
		}
	}
}

// extension returns a list of lanes that extends a route, behind or ahead,
// from a first lane up to a distance, with lanes that admit a kind. The first lane
// is first, and each later lane is the one at choice modulo the count.
func (c *sharedContainment) extension(first int, kind diskStudyKind, ahead bool, distance float64, choice int, used []int) []int {
	var lanes []int
	total := 0.0
	for lane := first; lane >= 0; {
		if slices.Contains(lanes, lane) || slices.Contains(used, lane) || len(lanes) == 256 {
			break
		}
		lanes = append(lanes, lane)
		total += c.arcs[lane][len(c.arcs[lane])-1]
		if total >= distance {
			break
		}
		next := c.out[c.s.network.Lanes[lane].To]
		if !ahead {
			next = c.in[c.s.network.Lanes[lane].From]
		}
		next = slices.DeleteFunc(slices.Clone(next), func(other int) bool {
			return !kind.allows(c.s.network.Lanes[other].VehicleClasses) || slices.Contains(lanes, other) || slices.Contains(used, other)
		})
		lane = -1
		if len(next) > 0 {
			lane = next[choice%len(next)]
			choice = (choice*31 + 7) % 1000003
		}
	}
	if !ahead {
		slices.Reverse(lanes)
	}
	return lanes
}

// lane drives the routes through a lane for a kind: one for each lane into
// its From node and each lane out of its To node, and at least count.
func (c *sharedContainment) lane(lane int, kind diskStudyKind, behind, ahead float64, count int) {
	network := c.s.network
	admits := func(other int) bool { return kind.allows(network.Lanes[other].VehicleClasses) }
	in := slices.DeleteFunc(slices.Clone(c.in[network.Lanes[lane].From]), func(other int) bool { return !admits(other) || other == lane })
	out := slices.DeleteFunc(slices.Clone(c.out[network.Lanes[lane].To]), func(other int) bool { return !admits(other) || other == lane })
	routes := max(count, len(in), len(out))
	for choice := range routes {
		var before, after []int
		if len(in) > 0 {
			before = c.extension(in[choice%len(in)], kind, false, behind, choice, []int{lane})
		}
		// The first lane ahead is one that the route does not use yet.
		used := append([]int{lane}, before...)
		if next := slices.DeleteFunc(slices.Clone(out), func(other int) bool { return slices.Contains(used, other) }); len(next) > 0 {
			after = c.extension(next[choice%len(next)], kind, true, ahead, choice, used)
		}
		route := append(append(slices.Clone(before), lane), after...)
		c.drive(route, kind, len(before), len(before)+1)
	}
}

func (c *sharedContainment) log(t *testing.T) {
	t.Helper()
	keys := make([]string, 0, len(c.counts))
	for key := range c.counts {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		t.Logf("%s: %s: %d", c.name, key, c.counts[key])
		for _, example := range c.examples[key] {
			t.Logf("%s:   %s", c.name, example)
		}
	}
}

// failures returns the count of the failures of the proposal: a plane of
// the local oracle that the index does not hold, a support lane of a body
// of the proof that the local oracle does not hold, and a pose at which the
// copy of the built oracle differs from the built oracle.
func (c *sharedContainment) failures() int {
	count := 0
	for key, value := range c.counts {
		if sharedFailure(key) {
			count += value
		}
	}
	return count
}

// sharedFailure reports whether a count of the containment test counts
// failures of the proposal.
func sharedFailure(key string) bool {
	return strings.HasPrefix(key, "index miss") || strings.HasPrefix(key, "support miss") || strings.HasPrefix(key, "gate")
}

// sharedHookNetwork returns a route of small lanes for the small-pod rule
// of the curved lanes: a straight lane, a quadratic lane whose end turns
// back past the normal of its chord, and two straight lanes. Near the end
// of the curved lane, the body of a small pod holds the end node while the
// route distance to it is more than 6 m. Each lane has its own plane.
func sharedHookNetwork() Network {
	lane := func(id, from, to string, control *Point) Lane {
		return Lane{ID: id, From: from, To: to, SpeedLimit: 14, SeparationGroup: id, Control: control}
	}
	return Network{
		Nodes: []Node{
			{ID: "a", Position: Point{X: -80, Y: -1.35}}, {ID: "f", Position: Point{X: -28, Y: -1.35}}, {ID: "t", Position: Point{X: 0, Y: 0.45}},
			{ID: "u", Position: Point{X: -40, Y: 4}}, {ID: "w", Position: Point{X: -80, Y: 4}},
		},
		Lanes: []Lane{
			lane("in", "a", "f", nil),
			lane("hook", "f", "t", &Point{X: 14, Y: -0.45}),
			lane("next", "t", "u", nil),
			lane("after", "u", "w", nil),
		},
	}
}

// TestSharedTrackStudyContainment is the containment test of section 3.1:
// oriented bodies at poses along each lane and across each join, on routes
// that can turn back, against the local oracle and the per-lane index. It
// runs on the networks of the study and on the turn-back and curves
// networks of the other studies. It fails on each failure of the proposal,
// on each control that does not fail at least its minimum count, and on each
// coverage count under its minimum.
func TestSharedTrackStudyContainment(t *testing.T) {
	input := sharedTrackLoad(t)
	names, networks := sharedTrackNetworks(t, input)
	run := func(c *sharedContainment, minimum map[string]int) {
		if f := c.failures(); f > 0 {
			t.Errorf("%s: %d failures of the proposal", c.name, f)
		}
		for key, want := range minimum {
			if got := c.counts[key]; got < want {
				t.Errorf("%s: %s: %d, want at least %d", c.name, key, got, want)
			}
		}
		c.log(t)
	}
	for shape := range diskShapes {
		for _, kind := range []diskStudyKind{diskSmall, diskLarge} {
			classes := ClassSet(0)
			if kind == diskLarge {
				classes = shapeStudyClasses
			}
			n, _, _ := diskStudyTurnBack(shape, classes)
			c := newSharedContainment(t, fmt.Sprintf("turn-back %s, %s lanes", diskStudyShapeNames[shape], diskStudyKindNames[kind]), n)
			if !finiteTails(c.s.tails) {
				t.Errorf("%s: a tail is not finite", c.name)
				continue
			}
			route := make([]int, len(n.Lanes))
			for index := range route {
				route[index] = index
			}
			minimum := map[string]int{}
			for _, k := range []diskStudyKind{diskSmall, diskLarge} {
				if k.allows(classes) {
					c.drive(route, k, 0, len(route))
					minimum["poses "+diskStudyKindNames[k]] = 1
				}
			}
			if kind == diskLarge {
				minimum["support of 2 lanes, gate"] = 1
			}
			run(c, minimum)
		}
	}
	hook := sharedHookNetwork()
	c := newSharedContainment(t, "hook", hook)
	t.Logf("hook: lane hook %.1f m, chord projection %.2f, small-pod front %.2f m", c.arcs[1][len(c.arcs[1])-1], sharedChordProjection(c.points[1]), c.bounds[1])
	c.drive([]int{0, 1, 2, 3}, diskSmall, 0, 4)
	run(c, map[string]int{
		"poses small": 1,
		"control, support miss: oracle without the small-pod rules, small, curve":         1,
		"control, support miss: oracle without the small-pod rules, small grown, curve":   1,
		"earlier oracle, support miss: oracle with the route maximum, small, curve":       1,
		"earlier oracle, support miss: oracle with the route maximum, small grown, curve": 1,
		"control, support miss: oracle with the anchor front 6 m, small grown, curve":     1,
	})
	curves := envelopeReachCurves(t)
	c = newSharedContainment(t, "curves", curves)
	for lane := range curves.Lanes {
		for _, kind := range []diskStudyKind{diskSmall, diskLarge} {
			if kind.allows(curves.Lanes[lane].VehicleClasses) {
				c.lane(lane, kind, c.behind+1, c.bounds[lane]+60, 4)
			}
		}
	}
	run(c, map[string]int{
		"poses small":              1,
		"poses large":              1,
		"support of 2 lanes, gate": 1,
	})
	for _, name := range names {
		if name == "3.6" {
			continue
		}
		n := networks[name]
		c := newSharedContainment(t, input.name+" "+name, n)
		for lane := range n.Lanes {
			for _, kind := range []diskStudyKind{diskSmall, diskLarge} {
				if !kind.allows(n.Lanes[lane].VehicleClasses) {
					continue
				}
				// A pod on a lane with a geometry tail also gets long
				// routes, so that the route maximum of the built oracle
				// can come from far lanes.
				if kind == diskLarge {
					c.lane(lane, kind, c.behind+1, c.bounds[lane]+60, 12)
					c.lane(lane, kind, 1100, 1100, 12)
					continue
				}
				c.lane(lane, kind, c.behind+1, c.bounds[lane]+60, 2)
				if c.s.tails[n.Lanes[lane].ID] != (geometryTails{}) {
					c.lane(lane, kind, 1100, 1100, 12)
				}
			}
		}
		for _, kind := range []diskStudyKind{diskSmall, diskLarge} {
			c.origins(kind)
			c.stationary(kind)
		}
		run(c, map[string]int{
			"poses small":                                             1,
			"poses large":                                             1,
			"poses with the origin held, small":                       1,
			"poses with the origin held, large":                       1,
			"stationary berths small":                                 1,
			"stationary berths large":                                 1,
			"body poses moved by the guideway tolerance, gate":        1,
			"body poses moved by the guideway tolerance, small grown": 1,
			"support of 2 lanes, gate":                                1,
			"support of more than 2 lanes with a maneuver group lane, small grown":                     1,
			"support of more than 2 lanes with no maneuver group lane, small grown":                    1,
			"support reaches the held origin node, small grown":                                        1,
			"off-support lane in the body, declared separated from the envelope (A1), gate":            1,
			"off-support lane in the body, tested through the envelope, gate":                          1,
			"control, support miss: oracle without the small-pod rules, small, maneuver group":         1,
			"control, support miss: oracle without the small-pod rules, small grown, maneuver group":   1,
			"earlier oracle, support miss: oracle with the route maximum, small, maneuver group":       1,
			"earlier oracle, support miss: oracle with the route maximum, small grown, maneuver group": 1,
			"control, support miss: oracle without the small-pod rules at a berth, small":              1,
			"control, support miss: oracle without the small-pod rules at a berth, small grown":        1,
			"earlier oracle, support miss: oracle at a berth without ShapeClearance, small":            1,
			"earlier oracle, support miss: oracle at a berth without ShapeClearance, small grown":      1,
			"stationary support with more than one node, small grown":                                  1,
		})
	}
}
