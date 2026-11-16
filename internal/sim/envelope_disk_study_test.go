package sim

import (
	"cmp"
	"container/heap"
	"encoding/json/v2"
	"fmt"
	"math"
	"os"
	"slices"
	"strings"
	"testing"
)

// This file is the disk study of docs/design/shared-track-preset.md,
// sections 9 and 10. It does not test the simulator. It runs only when the
// variable that diskStudyEnv names holds the path of a project file from
// cmd/scenario.
//
// The study puts a pod at each sampled pose of each lane, for each kind of
// class that the lane admits. A pose is a point on the lane with the heading
// of the lane, or a point at the To node of the lane during the join turn
// toward a lane out of the node. At each pose, the study finds each lane
// whose polyline enters the oriented grown body of the class
// (classShapeBody), or comes within largeEnvelopeRadius of the reference.
// It keeps the lanes that a route reaches within diskStudyRange ahead of
// the pose or behind it, and so it also finds a route that leaves the
// disk and comes back.
//
// For each kept lane, the study asks whether the oracle holds its plane:
// whether a location of the envelope of appendVehicleSafetyLocations, on a
// route from the pose to the lane, is not separated from the lane
// (safetyLocationsSeparated). This is the test of pair in safety.go for a
// pair with a large pod, and it uses the envelope of both pods, small pods
// included. A small pair uses only the location of the lane of each pod,
// so the study reports that test too, as raw.
//
// A miss is a lane that the oracle does not hold on at least one route.
// The classes of section 10 of the design are:
//   - (a) The lane is within the disk, outside the body, and missed.
//   - (b) The body touches the lane, and the oracle holds its plane.
//   - (c) The body touches the lane, and the oracle misses it.
//
// For each pair in class (c), the study puts a second pod on the missed
// lane at each position at which its grown body overlaps the body of the
// first pod, and asks whether pair would skip the clearance: whether the
// two envelopes are separated. It first takes the shortest routes, which
// hold the fewest planes. It then confirms with real windows: the lanes
// behind the start of each route and ahead of its end, up to
// diskStudyWindow, for both pods. It also asks whether the envelope reach
// of the audit with ShapeClearance separates the two positions, which
// exempts the pair from the audit.

const diskStudyEnv = "PODSIM_DISK_STUDY"

const (
	// diskStudyRange is the route distance ahead of and behind a pose that
	// the study walks.
	diskStudyRange = 200.0
	// diskStudyStep is the distance between two poses on a lane, and
	// between two positions of the second pod.
	diskStudyStep = 0.5
	// diskStudyTurns is the number of parts of a join turn.
	diskStudyTurns = 8
	// diskStudyCell is the size of a cell of the segment grid.
	diskStudyCell = 8.0
	// diskStudyPaths bounds the routes from a pose to one lane.
	diskStudyPaths = 256
	// diskStudyWindow and diskStudyWindows bound the windows that confirm
	// a pair in class (c).
	diskStudyWindow  = 80.0
	diskStudyWindows = 24
)

// diskStudyKind is a kind of class: small or large.
type diskStudyKind int

const (
	diskSmall diskStudyKind = iota
	diskLarge
	diskKinds
)

var diskStudyKindNames = [diskKinds]string{"small", "large"}

// class returns a class of the kind. The envelope does not depend on the
// class, and the grown body is the same for each class of a kind.
func (k diskStudyKind) class() VehicleClass {
	if k == diskLarge {
		return ExpressClass
	}
	return CompactClass
}

// allows reports whether a class set admits a class of the kind.
func (k diskStudyKind) allows(classes ClassSet) bool {
	if k == diskLarge {
		return largeClassSet(classes)
	}
	return classes.Allows(string(LegacyClass)) || classes.Allows(string(CompactClass))
}

// diskStudySegment is a segment of the polyline of a lane in the grid.
type diskStudySegment struct {
	lane, index int
}

// diskStudyPose is a reference point and a heading of a pod on a lane. next
// is the lane out of the To node of a join turn, or -1.
type diskStudyPose struct {
	lane     int
	position float64
	at       Point
	heading  float64
	next     int
}

// diskStudyPath is a route from the lane of a pose to a lane that the body
// or the disk touches. lanes holds the lanes after the lane of the pose up
// to the touched lane for a path ahead, and the lanes from the touched lane
// up to the lane before the pose for a path behind. offset is the distance
// from the To node of the lane of the pose to the start of the touched
// lane, or from the end of the touched lane to the From node of the lane of
// the pose.
type diskStudyPath struct {
	lanes  []int
	offset float64
	ahead  bool
}

// diskStudyPathSet holds the paths from a lane to a touched lane, and
// whether diskStudyPaths cut them short.
type diskStudyPathSet struct {
	paths     []diskStudyPath
	truncated bool
}

// diskStudyCounts counts the poses and the lane pairs of each class.
type diskStudyCounts struct {
	// poses counts the poses of each kind.
	poses [diskKinds]int
	// classes counts pose and lane results by kind and class name.
	classes map[string]int
}

// diskStudyWitness is a pose of a pair in class (c), with the route that
// misses the lane and the result of the pair test.
type diskStudyWitness struct {
	kind              diskStudyKind
	raw               bool
	pose              diskStudyPose
	touched           int
	path              diskStudyPath
	gap, distance     float64
	minimal, confirm  string
	shortest, longest float64
	// apart is the smallest distance between the polylines of the two
	// lanes, and crossing is true when they intersect.
	apart    float64
	crossing bool
	// audit is the result of the envelope reach of the audit at the pose
	// and the positions of the second pod.
	audit string
}

// diskStudyPlacement is a position of the second pod of a pair, and its
// kind.
type diskStudyPlacement struct {
	kind     diskStudyKind
	position float64
}

type diskStudy struct {
	s        *Simulation
	lanes    []Lane
	points   [][]Point
	arcs     [][]float64
	location []SafetyLocation
	byID     map[string]int
	incoming map[string][]int
	outgoing map[string][]int
	grid     map[[2]int][]diskStudySegment
	tails    map[string]geometryTails
	paths    map[[4]int]*diskStudyPathSet
	// reach is the envelope reach of the audit with ShapeClearance, or nil
	// when a tail is not finite.
	reach *envelopeReach
}

// diskStudyPrepare builds the indexes of a network without the bank audit
// and the length checks of prepareNetworkTails, which refuse the networks
// of the study. The envelope does not read them.
func diskStudyPrepare(t *testing.T, n Network) *Simulation {
	t.Helper()
	if err := n.validate(); err != nil {
		t.Fatal(err)
	}
	owned := n.clone()
	inferStationLaneRoles(&owned)
	tails := indexGeometryTails(owned)
	positions := indexNodePositions(owned.Nodes)
	maneuvers, _ := newManeuverIndex(owned, positions)
	berthKeys := indexBerthKeys(owned)
	certified, err := certifyManeuvers(owned, maneuvers, berthKeys)
	if err != nil {
		t.Fatal(err)
	}
	depots, _ := newDepotIndex(owned, positions)
	p := newPreparedNetwork(networkPreparation{
		network: owned, graph: newRouteGraph(owned), tails: tails, retention: newLaneRetention(owned, tails),
		maneuvers: maneuvers, certified: certified, depots: depots, berthKeys: berthKeys,
		lateral: newDepotLateral(owned, depots, tails, berthKeys),
	})
	return &Simulation{networkIndexes: &p.networkIndexes}
}

func newDiskStudy(t *testing.T, n Network) *diskStudy {
	t.Helper()
	s := diskStudyPrepare(t, n)
	d := &diskStudy{
		s: s, lanes: s.network.Lanes, byID: make(map[string]int), incoming: make(map[string][]int), outgoing: make(map[string][]int),
		grid: make(map[[2]int][]diskStudySegment), tails: s.tails, paths: make(map[[4]int]*diskStudyPathSet),
	}
	if finiteTails(s.tails) {
		d.reach = newEnvelopeReach(s.network, s.tails)
	}
	positions := indexNodePositions(s.network.Nodes)
	for index, lane := range d.lanes {
		points := lanePointsBetween(lane, positions[lane.From], positions[lane.To], make([]Point, 0, 65))
		arcs := make([]float64, len(points))
		for point := 1; point < len(points); point++ {
			arcs[point] = arcs[point-1] + pointDistance(points[point-1], points[point])
		}
		d.points, d.arcs = append(d.points, points), append(d.arcs, arcs)
		d.location = append(d.location, laneStudyLocation(lane))
		d.byID[lane.ID] = index
		d.incoming[lane.To] = append(d.incoming[lane.To], index)
		d.outgoing[lane.From] = append(d.outgoing[lane.From], index)
		for segment := 1; segment < len(points); segment++ {
			d.addSegment(index, segment, points[segment-1], points[segment])
		}
	}
	return d
}

func finiteTails(tails map[string]geometryTails) bool {
	for _, bounds := range tails {
		if !finite(bounds.tail) || !finite(bounds.fromTail) || !finite(bounds.front) {
			return false
		}
	}
	return true
}

// addSegment puts a segment in the cell of each point at half a cell along
// it. A query then looks one cell further than its radius.
func (d *diskStudy) addSegment(lane, segment int, a, b Point) {
	length := pointDistance(a, b)
	steps := max(1, int(math.Ceil(length/(diskStudyCell/2))))
	var last [2]int
	for step := 0; step <= steps; step++ {
		f := float64(step) / float64(steps)
		cell := diskStudyCellOf(Point{X: a.X + f*(b.X-a.X), Y: a.Y + f*(b.Y-a.Y)})
		if step > 0 && cell == last {
			continue
		}
		last = cell
		entries := d.grid[cell]
		if len(entries) == 0 || entries[len(entries)-1] != (diskStudySegment{lane, segment}) {
			d.grid[cell] = append(entries, diskStudySegment{lane, segment})
		}
	}
}

func diskStudyCellOf(p Point) [2]int {
	return [2]int{int(math.Floor(p.X / diskStudyCell)), int(math.Floor(p.Y / diskStudyCell))}
}

func (d *diskStudy) length(lane int) float64 {
	arcs := d.arcs[lane]
	return arcs[len(arcs)-1]
}

// pointAt returns the point and the heading of a lane at a distance.
func (d *diskStudy) pointAt(lane int, distance float64) (Point, float64) {
	points, arcs := d.points[lane], d.arcs[lane]
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

// poses returns the poses of a lane: one at each diskStudyStep, one at
// the end, and the inner parts of the join turn toward each lane out of
// the To node that admits the kind.
func (d *diskStudy) poses(lane int, kind diskStudyKind) []diskStudyPose {
	length := d.length(lane)
	var poses []diskStudyPose
	for position := 0.0; ; position += diskStudyStep {
		position = min(position, length)
		at, heading := d.pointAt(lane, position)
		poses = append(poses, diskStudyPose{lane: lane, position: position, at: at, heading: heading, next: -1})
		if position == length {
			break
		}
	}
	end, before := d.pointAt(lane, length)
	for _, next := range d.outgoing[d.lanes[lane].To] {
		if !kind.allows(d.lanes[next].VehicleClasses) {
			continue
		}
		_, after := d.pointAt(next, 0)
		sweep := math.Remainder(after-before, 2*math.Pi)
		for part := 1; part < diskStudyTurns; part++ {
			poses = append(poses, diskStudyPose{lane: lane, position: length, at: end, heading: before + sweep*float64(part)/diskStudyTurns, next: next})
		}
	}
	return poses
}

// segmentHitsBody reports whether a segment enters a grown body at a
// pose, by Liang-Barsky clipping in the frame of the body.
func segmentHitsBody(a, b, at Point, heading float64, body shapeBody) bool {
	c, s := math.Cos(heading), math.Sin(heading)
	ax, ay := (a.X-at.X)*c+(a.Y-at.Y)*s, -(a.X-at.X)*s+(a.Y-at.Y)*c
	bx, by := (b.X-at.X)*c+(b.Y-at.Y)*s, -(b.X-at.X)*s+(b.Y-at.Y)*c
	dx, dy := bx-ax, by-ay
	lo, hi := 0.0, 1.0
	for _, edge := range [4][2]float64{{-dx, ax + body.rear}, {dx, body.front - ax}, {-dy, ay + body.halfWidth}, {dy, body.halfWidth - ay}} {
		p, q := edge[0], edge[1]
		if p == 0 {
			if q < 0 {
				return false
			}
			continue
		}
		if r := q / p; p < 0 {
			lo = max(lo, r)
		} else {
			hi = min(hi, r)
		}
		if lo > hi {
			return false
		}
	}
	return true
}

// bodyCorners returns the corners of a grown body at a pose.
func bodyCorners(at Point, heading float64, body shapeBody) [4]Point {
	c, s := math.Cos(heading), math.Sin(heading)
	var corners [4]Point
	for index, local := range [4][2]float64{{-body.rear, -body.halfWidth}, {body.front, -body.halfWidth}, {body.front, body.halfWidth}, {-body.rear, body.halfWidth}} {
		corners[index] = Point{X: at.X + local[0]*c - local[1]*s, Y: at.Y + local[0]*s + local[1]*c}
	}
	return corners
}

// bodiesOverlap reports whether two grown bodies are not disjoint, by the
// separating axis test. Bodies that touch overlap, as in the sweep.
func bodiesOverlap(first, second [4]Point) bool {
	for _, corners := range [][4]Point{first, second} {
		for edge := range 2 {
			axis := Point{X: corners[edge+1].X - corners[edge].X, Y: corners[edge+1].Y - corners[edge].Y}
			loA, hiA, loB, hiB := math.Inf(1), math.Inf(-1), math.Inf(1), math.Inf(-1)
			for index := range 4 {
				a := first[index].X*axis.X + first[index].Y*axis.Y
				b := second[index].X*axis.X + second[index].Y*axis.Y
				loA, hiA, loB, hiB = min(loA, a), max(hiA, a), min(loB, b), max(hiB, b)
			}
			if hiA < loB || hiB < loA {
				return false
			}
		}
	}
	return true
}

// diskStudyNear is a lane near a pose: the distance of its polyline from
// the reference, and whether it enters the body of the pose.
type diskStudyNear struct {
	lane     int
	distance float64
	body     bool
}

// near returns the lanes whose polylines come within largeEnvelopeRadius
// of the reference of a pose or enter its body.
func (d *diskStudy) near(pose diskStudyPose, body shapeBody) []diskStudyNear {
	reach := int(math.Ceil((largeEnvelopeRadius + diskStudyCell/2) / diskStudyCell))
	center := diskStudyCellOf(pose.at)
	byLane := make(map[int]*diskStudyNear)
	var order []int
	for x := center[0] - reach; x <= center[0]+reach; x++ {
		for y := center[1] - reach; y <= center[1]+reach; y++ {
			for _, segment := range d.grid[[2]int{x, y}] {
				points := d.points[segment.lane]
				a, b := points[segment.index-1], points[segment.index]
				distance := segmentPointDistance(pose.at, a, b)
				if distance >= largeEnvelopeRadius && distance > body.front+body.rear+body.halfWidth {
					continue
				}
				hit := segmentHitsBody(a, b, pose.at, pose.heading, body)
				if distance >= largeEnvelopeRadius && !hit {
					continue
				}
				entry, ok := byLane[segment.lane]
				if !ok {
					entry = &diskStudyNear{lane: segment.lane, distance: distance}
					byLane[segment.lane] = entry
					order = append(order, segment.lane)
				}
				entry.distance = min(entry.distance, distance)
				entry.body = entry.body || hit
			}
		}
	}
	slices.Sort(order)
	near := make([]diskStudyNear, 0, len(order))
	for _, lane := range order {
		near = append(near, *byLane[lane])
	}
	return near
}

// diskStudyQueue is a priority queue of nodes by distance.
type diskStudyQueue []diskStudyVisit

type diskStudyVisit struct {
	node     string
	distance float64
}

func (q *diskStudyQueue) Len() int           { return len(*q) }
func (q *diskStudyQueue) Less(i, j int) bool { return (*q)[i].distance < (*q)[j].distance }
func (q *diskStudyQueue) Swap(i, j int)      { (*q)[i], (*q)[j] = (*q)[j], (*q)[i] }
func (q *diskStudyQueue) Push(x any) {
	if visit, ok := x.(diskStudyVisit); ok {
		*q = append(*q, visit)
	}
}

func (q *diskStudyQueue) Pop() any {
	old := *q
	visit := old[len(old)-1]
	*q = old[:len(old)-1]
	return visit
}

// distances returns the shortest distance from a node to each node within
// diskStudyRange, over lanes that admit the kind, forward along the lanes
// or backward against them.
func (d *diskStudy) distances(start string, kind diskStudyKind, forward bool) map[string]float64 {
	best := map[string]float64{start: 0}
	queue := &diskStudyQueue{{node: start}}
	for queue.Len() > 0 {
		visit, _ := heap.Pop(queue).(diskStudyVisit)
		if visit.distance > best[visit.node] {
			continue
		}
		lanes, far := d.outgoing[visit.node], func(lane Lane) string { return lane.To }
		if !forward {
			lanes, far = d.incoming[visit.node], func(lane Lane) string { return lane.From }
		}
		for _, lane := range lanes {
			if !kind.allows(d.lanes[lane].VehicleClasses) {
				continue
			}
			node, distance := far(d.lanes[lane]), visit.distance+d.length(lane)
			if old, ok := best[node]; distance <= diskStudyRange && (!ok || distance < old) {
				best[node] = distance
				heap.Push(queue, diskStudyVisit{node: node, distance: distance})
			}
		}
	}
	return best
}

// routePaths returns the paths from a lane to a touched lane, ahead and
// behind, within diskStudyRange. A path holds no lane twice.
func (d *diskStudy) routePaths(lane, touched int, kind diskStudyKind) *diskStudyPathSet {
	key := [4]int{lane, touched, int(kind)}
	if set, ok := d.paths[key]; ok {
		return set
	}
	set := &diskStudyPathSet{}
	d.paths[key] = set
	// Ahead: from the To node of the lane to the From node of the touched
	// lane. toTarget is the distance from each node to that node.
	toTarget := d.distances(d.lanes[touched].From, kind, false)
	d.walk(set, d.lanes[lane].To, toTarget, touched, kind, true, []int{lane})
	// Behind: from the From node of the lane back to the To node of the
	// touched lane.
	fromTarget := d.distances(d.lanes[touched].To, kind, true)
	d.walk(set, d.lanes[lane].From, fromTarget, touched, kind, false, []int{lane})
	return set
}

// walk adds the paths from a node to the touched lane, ahead or behind.
// bound holds the shortest distance from each node to the touched lane, so
// the walk drops each branch that cannot reach it within the range.
func (d *diskStudy) walk(set *diskStudyPathSet, start string, bound map[string]float64, touched int, kind diskStudyKind, ahead bool, used []int) {
	var visit func(node string, offset float64, path []int)
	visit = func(node string, offset float64, path []int) {
		if len(set.paths) >= diskStudyPaths {
			set.truncated = true
			return
		}
		lanes := d.outgoing[node]
		if !ahead {
			lanes = d.incoming[node]
		}
		for _, lane := range lanes {
			if !kind.allows(d.lanes[lane].VehicleClasses) || slices.Contains(path, lane) || slices.Contains(used, lane) {
				continue
			}
			if lane == touched {
				found := slices.Clone(path)
				found = append(found, lane)
				if !ahead {
					slices.Reverse(found)
				}
				set.paths = append(set.paths, diskStudyPath{lanes: found, offset: offset, ahead: ahead})
				continue
			}
			far, distance := d.lanes[lane].To, offset+d.length(lane)
			if !ahead {
				far = d.lanes[lane].From
			}
			if rest, ok := bound[far]; !ok || distance+rest > diskStudyRange {
				continue
			}
			visit(far, distance, append(path, lane))
		}
	}
	visit(start, 0, nil)
}

// route returns the lanes of a path with the lane of the pose, and the
// index of the lane of the pose in it.
func (d *diskStudy) route(lane int, path diskStudyPath) ([]int, int) {
	if path.ahead {
		return append([]int{lane}, path.lanes...), 0
	}
	return append(slices.Clone(path.lanes), lane), len(path.lanes)
}

// envelope returns the envelope of a pod of a kind on a route at a
// position on the lane at a route index, after the origin release.
func (d *diskStudy) envelope(route []int, at int, position float64, kind diskStudyKind) []SafetyLocation {
	lanes := make([]Lane, len(route))
	for index, lane := range route {
		lanes[index] = d.lanes[lane]
	}
	v := &vehicle{Pod: Pod{ID: "study", Class: kind.class(), Activity: Traveling, LaneID: lanes[at].ID}}
	d.s.setVehicleRoute(v, lanes)
	v.distance = v.blocks.lanes[at].start + position
	v.originReleased = true
	return d.s.appendVehicleSafetyLocations(nil, v, v.distance)
}

// holds reports whether an envelope holds the plane of a location.
func holds(envelope []SafetyLocation, location SafetyLocation) bool {
	return slices.ContainsFunc(envelope, func(held SafetyLocation) bool { return !safetyLocationsSeparated(held, location) })
}

// extensions returns lane lists that extend a route at a node, behind its
// start or ahead of its end, up to diskStudyWindow. A list can be shorter
// when the network ends. With no lane at the node, it returns the empty
// list.
func (d *diskStudy) extensions(node string, kind diskStudyKind, ahead bool) [][]int {
	var result [][]int
	var visit func(node string, length float64, path []int)
	visit = func(node string, length float64, path []int) {
		lanes := d.outgoing[node]
		if !ahead {
			lanes = d.incoming[node]
		}
		for _, lane := range lanes {
			if len(result) >= diskStudyWindows {
				return
			}
			if !kind.allows(d.lanes[lane].VehicleClasses) || slices.Contains(path, lane) {
				continue
			}
			extended := append(slices.Clone(path), lane)
			total := length + d.length(lane)
			far := d.lanes[lane].To
			if !ahead {
				far = d.lanes[lane].From
			}
			next := d.outgoing[far]
			if !ahead {
				next = d.incoming[far]
			}
			if total >= diskStudyWindow || len(next) == 0 {
				found := slices.Clone(extended)
				if !ahead {
					slices.Reverse(found)
				}
				result = append(result, found)
				continue
			}
			visit(far, total, extended)
		}
	}
	visit(node, 0, nil)
	if len(result) == 0 {
		return [][]int{nil}
	}
	return result
}

// diskStudyResult is the result of the study on one network.
type diskStudyResult struct {
	counts    diskStudyCounts
	witnesses []diskStudyWitness
	// truncated counts the lane pairs whose paths passed diskStudyPaths.
	truncated int
	// nearest holds the smallest route distance from a pose to a lane that
	// it misses, by kind and test, over each pose of class (c).
	nearest map[string]float64
}

func (r *diskStudyResult) add(name string, count int) {
	r.counts.classes[name] += count
}

// pairKey is a pose lane, a touched lane, and a kind.
type pairKey [3]int

// run walks each pose of each lane of a network.
func (d *diskStudy) run() diskStudyResult {
	result := diskStudyResult{counts: diskStudyCounts{classes: make(map[string]int)}, nearest: make(map[string]float64)}
	pairs := make(map[pairKey]map[string]bool)
	mark := func(key pairKey, class string) {
		if pairs[key] == nil {
			pairs[key] = make(map[string]bool)
		}
		pairs[key][class] = true
		result.add("poses "+diskStudyKindNames[key[2]]+" "+class, 1)
	}
	witnessed := make(map[[4]int]bool)
	for lane := range d.lanes {
		for kind := range diskKinds {
			if !kind.allows(d.lanes[lane].VehicleClasses) {
				continue
			}
			body := classShapeBody(kind.class())
			ahead := d.distances(d.lanes[lane].To, kind, true)
			behind := d.distances(d.lanes[lane].From, kind, false)
			for _, pose := range d.poses(lane, kind) {
				result.counts.poses[kind]++
				for _, near := range d.near(pose, body) {
					if near.lane == lane {
						continue
					}
					d.classify(&result, pose, kind, near, ahead, behind, mark, witnessed)
				}
			}
		}
	}
	for key, classes := range pairs {
		for class := range classes {
			result.add("pairs "+diskStudyKindNames[key[2]]+" "+class, 1)
		}
	}
	for _, set := range d.paths {
		if set.truncated {
			result.truncated++
		}
	}
	return result
}

// onRoute reports whether a route reaches a touched lane within the range
// from a pose, ahead or behind.
func (d *diskStudy) onRoute(pose diskStudyPose, touched int, ahead, behind map[string]float64) bool {
	remaining := d.length(pose.lane) - pose.position
	if offset, ok := ahead[d.lanes[touched].From]; ok && remaining+offset <= diskStudyRange {
		return true
	}
	offset, ok := behind[d.lanes[touched].To]
	return ok && pose.position+offset <= diskStudyRange
}

// classify records the classes of one touched lane at a pose.
func (d *diskStudy) classify(result *diskStudyResult, pose diskStudyPose, kind diskStudyKind, near diskStudyNear, ahead, behind map[string]float64, mark func(pairKey, string), witnessed map[[4]int]bool) {
	lane, touched := pose.lane, near.lane
	key := pairKey{lane, touched, int(kind)}
	disk := near.distance < largeEnvelopeRadius
	separated := safetyLocationsSeparated(d.location[lane], d.location[touched])
	if !d.onRoute(pose, touched, ahead, behind) {
		if near.body {
			mark(key, fmt.Sprintf("off-route body, separated %v", separated))
		}
		return
	}
	// The plane test on each route from the pose to the touched lane. A
	// lane that shares a node or a group with the lane of the pose is held
	// on each route, because the envelope holds the lane of the pose.
	planeMiss, listMiss := false, false
	missGap, routeGap := math.Inf(1), math.Inf(1)
	var missing diskStudyPath
	if !separated {
		listMiss = d.listMiss(pose, touched, kind)
	} else {
		set := d.routePaths(lane, touched, kind)
		fits := false
		for _, path := range set.paths {
			if !d.pathFits(pose, path) {
				continue
			}
			fits = true
			gap := d.gap(pose, path)
			routeGap = min(routeGap, gap)
			route, at := d.route(lane, path)
			envelope := d.envelope(route, at, pose.position, kind)
			if !slices.Contains(envelope, d.location[touched]) {
				listMiss = true
			}
			if !holds(envelope, d.location[touched]) {
				missGap = min(missGap, gap)
				if !planeMiss {
					planeMiss, missing = true, path
				}
			}
		}
		// A join turn continues on one lane only, so a lane that only the
		// other lanes out of the node reach is off the route of the pose.
		if !fits {
			if near.body {
				mark(key, "off-route body, separated true")
			}
			return
		}
	}
	nearest := func(name string, gap float64) {
		if old, ok := result.nearest[name]; !ok || gap < old {
			result.nearest[name] = gap
		}
	}
	if near.body && planeMiss {
		nearest(diskStudyKindNames[kind]+" envelope", missGap)
	}
	if near.body && kind == diskSmall && separated {
		nearest("small raw", routeGap)
	}
	switch {
	case near.body && planeMiss:
		mark(key, "c envelope miss")
	case near.body:
		mark(key, "b held")
	case disk && planeMiss:
		mark(key, "a disk-only miss")
	case disk:
		mark(key, "disk held")
	}
	if listMiss {
		mark(key, fmt.Sprintf("list miss, body %v", near.body))
	}
	if kind == diskSmall && separated {
		switch {
		case near.body:
			mark(key, "c raw miss")
		case disk:
			mark(key, "a raw disk-only miss")
		}
	}
	witness := [4]int{lane, touched, int(kind), 0}
	if near.body && planeMiss && !witnessed[witness] {
		witnessed[witness] = true
		result.witnesses = append(result.witnesses, d.witness(pose, kind, touched, missing, false))
	}
	witness[3] = 1
	if near.body && kind == diskSmall && separated && !witnessed[witness] {
		set := d.routePaths(lane, touched, kind)
		for _, path := range set.paths {
			if d.pathFits(pose, path) {
				witnessed[witness] = true
				result.witnesses = append(result.witnesses, d.witness(pose, kind, touched, path, true))
				break
			}
		}
	}
}

// gap returns the route distance from the reference of a pose to the start
// of the touched lane of a path ahead, or to the end of the touched lane of
// a path behind.
func (d *diskStudy) gap(pose diskStudyPose, path diskStudyPath) float64 {
	if path.ahead {
		return d.length(pose.lane) - pose.position + path.offset
	}
	return pose.position + path.offset
}

// pathFits reports whether a path suits a pose: the touched lane is within
// the range, and a join turn continues on its next lane.
func (d *diskStudy) pathFits(pose diskStudyPose, path diskStudyPath) bool {
	if path.ahead {
		if pose.next >= 0 && path.lanes[0] != pose.next {
			return false
		}
		return d.length(pose.lane)-pose.position+path.offset <= diskStudyRange
	}
	return pose.position+path.offset <= diskStudyRange
}

// listMiss reports whether the envelope on the shortest route to a lane
// next to the lane of the pose omits the location of that lane. It is the
// count of the draft probe, which tested the envelope list.
func (d *diskStudy) listMiss(pose diskStudyPose, touched int, kind diskStudyKind) bool {
	lane := pose.lane
	if d.lanes[touched].From != d.lanes[lane].To || pose.next >= 0 && pose.next != touched {
		return false
	}
	envelope := d.envelope([]int{lane, touched}, 0, pose.position, kind)
	return !slices.Contains(envelope, d.location[touched])
}

// witness tests a pair in class (c): a second pod on the touched lane at
// each position at which its grown body overlaps the body of the pose.
// With raw set, the pair is a small pair, which compares the lanes only.
func (d *diskStudy) witness(pose diskStudyPose, kind diskStudyKind, touched int, path diskStudyPath, raw bool) diskStudyWitness {
	w := diskStudyWitness{kind: kind, raw: raw, pose: pose, touched: touched, path: path, distance: math.Inf(1), shortest: math.Inf(1), longest: math.Inf(-1)}
	for _, near := range d.near(pose, classShapeBody(kind.class())) {
		if near.lane == touched {
			w.distance = near.distance
		}
	}
	w.apart, w.crossing = d.apart(pose.lane, touched)
	w.gap = d.gap(pose, path)
	route, at := d.route(pose.lane, path)
	others := []diskStudyKind{diskLarge}
	if raw {
		others = []diskStudyKind{diskSmall}
	} else if kind == diskLarge {
		others = []diskStudyKind{diskSmall, diskLarge}
	}
	corners := bodyCorners(pose.at, pose.heading, classShapeBody(kind.class()))
	var placements []diskStudyPlacement
	for _, other := range others {
		if !other.allows(d.lanes[touched].VehicleClasses) {
			continue
		}
		bodyB := classShapeBody(other.class())
		for position := 0.0; position <= d.length(touched); position += diskStudyStep {
			at, heading := d.pointAt(touched, position)
			if bodiesOverlap(corners, bodyCorners(at, heading, bodyB)) {
				placements = append(placements, diskStudyPlacement{other, position})
				w.shortest, w.longest = min(w.shortest, position), max(w.longest, position)
			}
		}
	}
	if len(placements) == 0 {
		w.minimal, w.confirm = "no overlap with a pod that the lane admits", "-"
		return w
	}
	if raw {
		w.minimal = "skipped: lanes separated"
		w.confirm = d.rawContext(pose.lane, touched)
		return w
	}
	w.audit = d.auditReach(pose, touched, placements)
	envelopeA := d.envelope(route, at, pose.position, kind)
	w.minimal = "not skipped"
	for _, place := range placements {
		if envelopeLocationsSeparated(envelopeA, d.envelope([]int{touched}, 0, place.position, place.kind)) {
			w.minimal = fmt.Sprintf("skipped: %s pod at %.1f m", diskStudyKindNames[place.kind], place.position)
			break
		}
	}
	w.confirm = d.confirm(pose, kind, touched, route, at, func(yield func(diskStudyKind, float64) bool) {
		for _, place := range placements {
			if !yield(place.kind, place.position) {
				return
			}
		}
	})
	return w
}

// auditReach returns how many positions of the second pod the envelope
// reach of the audit separates from the pose: the audit with
// ShapeClearance exempts the pair at those positions.
func (d *diskStudy) auditReach(pose diskStudyPose, touched int, placements []diskStudyPlacement) string {
	if d.reach == nil {
		return "no envelope reach"
	}
	span := d.reach.laneAt(pose.lane, pose.position)
	separated := 0
	for _, place := range placements {
		if span.separated(d.reach.laneAt(touched, place.position)) {
			separated++
		}
	}
	return fmt.Sprintf("audit reach separates %d of %d positions", separated, len(placements))
}

// apart returns the smallest distance between the polylines of two lanes,
// and whether they intersect away from a shared node.
func (d *diskStudy) apart(first, second int) (float64, bool) {
	a, b := d.points[first], d.points[second]
	distance := math.Inf(1)
	for i := 1; i < len(a); i++ {
		for j := 1; j < len(b); j++ {
			distance = min(distance, segmentPointDistance(a[i-1], b[j-1], b[j]), segmentPointDistance(a[i], b[j-1], b[j]),
				segmentPointDistance(b[j-1], a[i-1], a[i]), segmentPointDistance(b[j], a[i-1], a[i]))
			if segmentsCross(a[i-1], a[i], b[j-1], b[j]) {
				distance = 0
			}
		}
	}
	return distance, distance == 0 && !safetyLocationsShareNode(d.location[first], d.location[second])
}

// segmentsCross reports whether two segments intersect.
func segmentsCross(a, b, c, e Point) bool {
	side := func(p, q, r Point) float64 { return (q.X-p.X)*(r.Y-p.Y) - (q.Y-p.Y)*(r.X-p.X) }
	return side(a, b, c)*side(a, b, e) <= 0 && side(c, e, a)*side(c, e, b) <= 0
}

// confirm extends the routes of both pods with real windows, and returns
// the first combination in which the first pod still misses the touched
// lane and pair would skip the clearance.
func (d *diskStudy) confirm(pose diskStudyPose, kind diskStudyKind, touched int, route []int, at int, places func(func(diskStudyKind, float64) bool)) string {
	type envelopeB struct {
		text     string
		envelope []SafetyLocation
	}
	var others []envelopeB
	for other, position := range places {
		for _, before := range d.extensions(d.lanes[touched].From, other, false) {
			for _, after := range d.extensions(d.lanes[touched].To, other, true) {
				routeB := append(append(slices.Clone(before), touched), after...)
				others = append(others, envelopeB{
					text:     fmt.Sprintf("%s pod at %.1f m on %s", diskStudyKindNames[other], position, d.routeText(routeB)),
					envelope: d.envelope(routeB, len(before), position, other),
				})
			}
		}
	}
	misses, windows := 0, 0
	for _, before := range d.extensions(d.lanes[route[0]].From, kind, false) {
		for _, after := range d.extensions(d.lanes[route[len(route)-1]].To, kind, true) {
			windows++
			routeA := append(append(slices.Clone(before), route...), after...)
			envelopeA := d.envelope(routeA, at+len(before), pose.position, kind)
			if holds(envelopeA, d.location[touched]) {
				continue
			}
			misses++
			for _, other := range others {
				if envelopeLocationsSeparated(envelopeA, other.envelope) {
					return fmt.Sprintf("skipped in %d of %d windows tried: first pod on %s, second %s", misses, windows, d.routeText(routeA), other.text)
				}
			}
		}
	}
	return fmt.Sprintf("not skipped: the first pod misses the lane in %d of %d windows, and no second envelope is separated", misses, windows)
}

// rawContext names the rules of a small pair that run before or beside
// the plane test: a certified maneuver pair, a maneuver group lane, and a
// depot lane.
func (d *diskStudy) rawContext(lane, touched int) string {
	var parts []string
	for _, index := range []int{lane, touched} {
		id := d.lanes[index].ID
		if d.s.maneuvers.groupLane(id) {
			parts = append(parts, id+" is a maneuver group lane")
		}
		if d.s.depots.depotLane(id) {
			parts = append(parts, id+" is a depot lane")
		}
	}
	if d.s.certified.certified(laneKey(lane), laneKey(touched)) {
		parts = append(parts, "certified pair")
	}
	if len(parts) == 0 {
		return "no other rule"
	}
	return strings.Join(parts, ", ")
}

func (d *diskStudy) routeText(route []int) string {
	ids := make([]string, len(route))
	for index, lane := range route {
		ids[index] = d.lanes[lane].ID
	}
	return strings.Join(ids, " ")
}

// fronts returns the front bound and the tail of a lane, for the log.
func (d *diskStudy) fronts(lane int) string {
	bounds := d.tails[d.lanes[lane].ID]
	return fmt.Sprintf("tail %.2f m, front %.2f m", bounds.tail, bounds.front)
}

func (d *diskStudy) logWitness(t *testing.T, name string, w diskStudyWitness) {
	t.Helper()
	test := "envelope"
	if w.raw {
		test = "raw"
	}
	lane, touched := d.lanes[w.pose.lane], d.lanes[w.touched]
	turn := ""
	if w.pose.next >= 0 {
		turn = fmt.Sprintf(", join turn toward %s at %.1f degrees", d.lanes[w.pose.next].ID, w.pose.heading*180/math.Pi)
	}
	direction := "behind"
	if w.path.ahead {
		direction = "ahead"
	}
	t.Logf("%s c %s %s: pod on %s (group %q, %s, length %.1f m) at %.1f m%s, reference (%.2f, %.2f); touches %s (group %q, %s, classes %v) at %.2f m from the reference, %.1f m %s on route %s; lanes apart %.2f m, crossing %v; second pod on %.1f to %.1f m: shortest routes %s; windows %s; %s",
		name, diskStudyKindNames[w.kind], test, lane.ID, lane.SeparationGroup, d.fronts(w.pose.lane), d.length(w.pose.lane), w.pose.position, turn, w.pose.at.X, w.pose.at.Y,
		touched.ID, touched.SeparationGroup, d.fronts(w.touched), touched.VehicleClasses, w.distance, w.gap, direction, d.routeText(w.path.lanes), w.apart, w.crossing, w.shortest, w.longest, w.minimal, w.confirm, w.audit)
}

// diskStudyExamples is the number of witnesses that the log shows for a
// group of witnesses that pair cannot skip.
const diskStudyExamples = 3

// logWitnesses logs a summary of the witnesses of class (c) by test, pair
// result, and geometry. It logs each witness that pair skips with a pod of
// the class that the touched lane admits, and a few of each other group.
func (d *diskStudy) logWitnesses(t *testing.T, name string, witnesses []diskStudyWitness) {
	t.Helper()
	type group struct {
		count, crossing    int
		nearest, farthest  float64
		widest, closestGap float64
	}
	groups := make(map[string]*group)
	var keys []string
	for _, w := range witnesses {
		test := "envelope"
		if w.raw {
			test = "raw"
		}
		outcome, _, _ := strings.Cut(w.minimal, ":")
		key := fmt.Sprintf("%s %s, shortest routes %s", diskStudyKindNames[w.kind], test, outcome)
		if w.raw {
			key += ", " + w.confirm
		}
		g, ok := groups[key]
		if !ok {
			g = &group{nearest: math.Inf(1), farthest: math.Inf(-1), widest: math.Inf(-1), closestGap: math.Inf(1)}
			groups[key] = g
			keys = append(keys, key)
		}
		g.count++
		if w.crossing {
			g.crossing++
		}
		g.nearest, g.farthest = min(g.nearest, w.gap), max(g.farthest, w.gap)
		g.widest, g.closestGap = max(g.widest, w.apart), min(g.closestGap, w.apart)
		if outcome == "skipped" && !w.raw || g.count <= diskStudyExamples {
			d.logWitness(t, name, w)
		}
	}
	slices.Sort(keys)
	for _, key := range keys {
		g := groups[key]
		t.Logf("%s c witnesses %s: %d, %d crossing, route distance %.1f to %.1f m, lanes apart %.2f to %.2f m", name, key, g.count, g.crossing, g.nearest, g.farthest, g.closestGap, g.widest)
	}
}

func diskStudyNetwork(t *testing.T) Network {
	t.Helper()
	path := os.Getenv(diskStudyEnv)
	if path == "" {
		t.Skip("set " + diskStudyEnv + " to a project file to run the envelope disk study")
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

// diskStudyVariants returns the networks of the study: the network of the
// file, and, for a network with no large lane and the first hub pair, the
// network of section 2.1 of the draft, with the study classes on the
// routes between the two hubs and on the hubs.
func diskStudyVariants(t *testing.T, base Network) map[string]Network {
	t.Helper()
	variants := map[string]Network{"base": base}
	first, okFirst := base.Station(shapeStudyHubs[0][0])
	second, okSecond := base.Station(shapeStudyHubs[0][1])
	if base.hasLargeGeometry() || !okFirst || !okSecond {
		return variants
	}
	n := base.clone()
	keep := make(map[string]bool)
	for _, route := range [][]Lane{shapeStudyHubRoute(t, n, first, second), shapeStudyHubRoute(t, n, second, first)} {
		for _, lane := range route {
			keep[lane.ID] = true
		}
	}
	setShapeStudyClasses(&n, keep, false, first.ID, second.ID)
	variants["route"] = n
	return variants
}

// TestEnvelopeDiskStudy classifies the lanes that the body or the disk of
// a pod touches on each network of the study, with ShapeClearance off and
// on. The field does not change the tails, the cells, or the envelope, so
// the two results must be equal.
func TestEnvelopeDiskStudy(t *testing.T) {
	base := diskStudyNetwork(t)
	variants := diskStudyVariants(t, base)
	names := make([]string, 0, len(variants))
	for name := range variants {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		var results [2]diskStudyResult
		for index, field := range []bool{false, true} {
			n := variants[name].clone()
			n.ShapeClearance = field
			d := newDiskStudy(t, n)
			results[index] = d.run()
			if index > 0 {
				continue
			}
			result := results[0]
			t.Logf("%s: %d lanes, %d large lanes, poses small %d, large %d, lane pairs with truncated paths %d",
				name, len(d.lanes), countLargeLanes(d.lanes), result.counts.poses[diskSmall], result.counts.poses[diskLarge], result.truncated)
			keys := make([]string, 0, len(result.counts.classes))
			for key := range result.counts.classes {
				keys = append(keys, key)
			}
			slices.Sort(keys)
			for _, key := range keys {
				t.Logf("%s %s: %d", name, key, result.counts.classes[key])
			}
			t.Logf("%s: front bound of the network %.2f m, large lanes under 40 m %d", name, reachFront(d.s.network, d.tails), len(shortStudyLanes(n)))
			tests := make([]string, 0, len(result.nearest))
			for test := range result.nearest {
				tests = append(tests, test)
			}
			slices.Sort(tests)
			for _, test := range tests {
				t.Logf("%s c %s: smallest route distance to a missed lane over each pose %.1f m", name, test, result.nearest[test])
			}
			slices.SortFunc(result.witnesses, func(a, b diskStudyWitness) int {
				return cmp.Or(cmp.Compare(a.kind, b.kind), cmp.Compare(boolInt(a.raw), boolInt(b.raw)), cmp.Compare(d.lanes[a.pose.lane].ID, d.lanes[b.pose.lane].ID), cmp.Compare(d.lanes[a.touched].ID, d.lanes[b.touched].ID))
			})
			d.logWitnesses(t, name, result.witnesses)
		}
		if fmt.Sprint(results[0].counts) != fmt.Sprint(results[1].counts) || len(results[0].witnesses) != len(results[1].witnesses) {
			t.Errorf("%s: ShapeClearance changes the result: %v and %v", name, results[0].counts, results[1].counts)
		}
	}
}

func countLargeLanes(lanes []Lane) int {
	count := 0
	for _, lane := range lanes {
		if largeClassSet(lane.VehicleClasses) {
			count++
		}
	}
	return count
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

// diskStudyShape is a route shape of item 4 of the study.
type diskStudyShape int

const (
	// diskHairpin is the construction of the review: lane "first", then
	// one curved lane that turns back, so that its far node and the start
	// of lane "back" are 3 m beside "first", 20 m before its end.
	diskHairpin diskStudyShape = iota
	// diskTriangle is a route of three straight lanes whose third lane
	// crosses "first" 30 m before its end and 45 m after its own start.
	diskTriangle
	// diskLoop is a route of straight lanes that turns back, so that lane
	// "back" crosses "first" 184 m ahead on the route, 4 lanes after it.
	diskLoop
	// diskBridgedLoop is diskLoop with one more lane, so that the lanes at
	// the ends of "first" and "back" share no node with each other.
	diskBridgedLoop
	diskShapes
)

var diskStudyShapeNames = [diskShapes]string{"hairpin", "triangle", "loop", "bridged loop"}

// diskStudyTurnBack returns a network of a shape, with each lane on its
// own plane, the position on "first" of a pod, and the position on "back"
// of a second pod whose body overlaps the body of the first.
func diskStudyTurnBack(shape diskStudyShape, classes ClassSet) (n Network, first, back float64) {
	lane := func(id, from, to string) Lane {
		return Lane{ID: id, From: from, To: to, SpeedLimit: 14, SeparationGroup: id, VehicleClasses: classes}
	}
	n.Nodes = []Node{{ID: "a", Position: Point{X: 0, Y: 0}}, {ID: "b", Position: Point{X: 60, Y: 0}}}
	n.Lanes = []Lane{lane("first", "a", "b")}
	switch shape {
	case diskHairpin:
		// The control point is on the perpendicular bisector of the chord,
		// so the curve advances along its chord and has a finite tail.
		n.Nodes = append(n.Nodes, Node{ID: "c", Position: Point{X: 40, Y: 3}}, Node{ID: "f", Position: Point{X: 40, Y: 60}})
		turn := lane("turn", "b", "c")
		chord := Point{X: -20, Y: 3}
		length := math.Hypot(chord.X, chord.Y)
		turn.Control = &Point{X: 50 + 40*chord.Y/length, Y: 1.5 - 40*chord.X/length}
		n.Lanes = append(n.Lanes, turn, lane("back", "c", "f"))
		return n, 40, 1
	case diskTriangle:
		n.Nodes = append(n.Nodes, Node{ID: "c", Position: Point{X: 30, Y: 45}}, Node{ID: "f", Position: Point{X: 30, Y: -45}})
		n.Lanes = append(n.Lanes, lane("turn", "b", "c"), lane("back", "c", "f"))
		return n, 30, 45
	case diskLoop, diskBridgedLoop:
		n.Nodes = append(n.Nodes, Node{ID: "c", Position: Point{X: 101, Y: 0}}, Node{ID: "d", Position: Point{X: 101, Y: 41}},
			Node{ID: "e", Position: Point{X: 50, Y: 41}}, Node{ID: "f", Position: Point{X: 50, Y: -45}})
		n.Lanes = append(n.Lanes, lane("out", "b", "c"), lane("up", "c", "d"), lane("over", "d", "e"))
		if shape == diskLoop {
			n.Lanes = append(n.Lanes, lane("back", "e", "f"))
			return n, 50, 41
		}
		n.Nodes[4].Position = Point{X: 101, Y: 82}
		n.Nodes = append(n.Nodes, Node{ID: "g", Position: Point{X: 50, Y: 82}})
		n.Lanes = append(n.Lanes, lane("across", "e", "g"), lane("back", "g", "f"))
		return n, 50, 82
	case diskShapes:
	}
	panic("unknown shape")
}

// TestEnvelopeDiskStudyTurnBack is item 4 of the study: routes that turn
// back beside or across their own first lane on another plane, with small
// or large lanes, with ShapeClearance off and on. For each network that
// ValidateNetwork accepts, it logs whether the envelope of a pod on the
// first lane holds the plane of the lane that comes back.
func TestEnvelopeDiskStudyTurnBack(t *testing.T) {
	if os.Getenv(diskStudyEnv) == "" {
		t.Skip("set " + diskStudyEnv + " to run the envelope disk study")
	}
	for shape := range diskShapes {
		for _, kind := range []diskStudyKind{diskSmall, diskLarge} {
			classes := ClassSet(0)
			if kind == diskLarge {
				classes = shapeStudyClasses
			}
			for _, field := range []bool{false, true} {
				n, position, onBack := diskStudyTurnBack(shape, classes)
				n.ShapeClearance = field
				name := fmt.Sprintf("%s, %s lanes, shapeClearance %v", diskStudyShapeNames[shape], diskStudyKindNames[kind], field)
				err := ValidateNetwork(n)
				t.Logf("%s: ValidateNetwork: %v", name, err)
				d := newDiskStudy(t, n)
				route := make([]int, len(n.Lanes))
				for index := range route {
					route[index] = index
				}
				back := len(route) - 1
				offset := 0.0
				for _, lane := range route[:back] {
					offset += d.length(lane)
				}
				for index, lane := range route {
					t.Logf("%s: lane %s, length %.1f m, %s", name, n.Lanes[lane].ID, d.length(index), d.fronts(index))
				}
				for _, other := range []diskStudyKind{diskSmall, diskLarge} {
					if !other.allows(classes) {
						continue
					}
					envelopeA := d.envelope(route, 0, position, kind)
					envelopeB := d.envelope(route, back, onBack, other)
					atA, headingA := d.pointAt(0, position)
					atB, headingB := d.pointAt(back, onBack)
					overlap := bodiesOverlap(bodyCorners(atA, headingA, classShapeBody(kind.class())), bodyCorners(atB, headingB, classShapeBody(other.class())))
					t.Logf("%s: %s pod on first at %.0f m, %s pod on back at %.1f m, %.1f m ahead on the route: bodies overlap %v, envelope of the first holds back %v, raw separated %v, envelopes separated %v, envelopes %v and %v",
						name, diskStudyKindNames[kind], position, diskStudyKindNames[other], onBack, offset+onBack-position, overlap, holds(envelopeA, d.location[back]),
						safetyLocationsSeparated(d.location[0], d.location[back]), envelopeLocationsSeparated(envelopeA, envelopeB), envelopeA, envelopeB)
				}
			}
		}
	}
}
