package sim

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"reflect"
	"slices"
)

var errCouplingReservationDenied = errors.New("coupling reservation denied")

// This kind has no live consumer or owner in this inactive phase.
func couplingSiteResourceKind() resourceKind { return junctionResource + 1 }

type couplingReservationNetwork struct {
	prepared  *PreparedNetwork
	contract  CouplingContract
	sites     map[string]CouplingSite
	corridors map[string]CouplingCorridor
	lanes     map[string]couplingLaneGeometry
	obstacles []couplingObstacle
	index     *couplingBoxIndex
	// discovery lists the corridors in ID order with the lane of each
	// assembly site. Preparation sets it, and nothing changes it later.
	discovery []couplingDiscoveryCorridor
}

type couplingDiscoveryCorridor struct{ id, assemblyLane string }

// assemblyLane reports whether laneID is the assembly lane of a corridor.
func (n *couplingReservationNetwork) assemblyLane(laneID string) bool {
	return slices.ContainsFunc(n.discovery, func(c couplingDiscoveryCorridor) bool { return c.assemblyLane == laneID })
}

type couplingObstacle struct {
	lane      string
	cell      int
	from, to  Point
	resources []resource
	clearance float64
	release   releaseInput
	box       couplingBox
}

type couplingBox struct{ low, high Point }

type couplingBoxIndex struct {
	box         couplingBox
	left, right *couplingBoxIndex
	items       []int
}

func couplingDenied(reason string) error {
	return fmt.Errorf("%s: %w", reason, errCouplingReservationDenied)
}

// Preparation owns descriptors and shares only immutable prepared records.
func prepareCouplingReservations(prepared *PreparedNetwork, input CouplingGeometryInput) (*couplingReservationNetwork, error) {
	if prepared == nil || prepared.check() != nil {
		return nil, couplingDenied("network is not prepared")
	}
	if len(input.Network.Nodes) > expressMaxNodes || len(input.Network.Lanes) > expressMaxLanes || len(input.Network.Stations) > expressMaxStations {
		return nil, couplingDenied("network exceeds existing bounds")
	}
	if !reflect.DeepEqual(input.Network, prepared.network) {
		return nil, couplingDenied("authored and prepared networks differ")
	}
	if err := ValidateCouplingGeometry(input); err != nil {
		return nil, fmt.Errorf("authored coupling geometry: %w", err)
	}
	n := &couplingReservationNetwork{prepared: prepared, contract: input.Contract,
		sites: make(map[string]CouplingSite), corridors: make(map[string]CouplingCorridor), lanes: prepareCouplingLanes(prepared.network)}
	for _, site := range input.Sites {
		n.sites[site.ID] = site
	}
	for _, corridor := range input.Corridors {
		corridor.LaneIDs = slices.Clone(corridor.LaneIDs)
		n.corridors[corridor.ID] = corridor
	}
	for _, id := range slices.Sorted(maps.Keys(n.corridors)) {
		n.discovery = append(n.discovery, couplingDiscoveryCorridor{id: id, assemblyLane: n.sites[n.corridors[id].AssemblySiteID].LaneID})
	}
	if len(n.corridors) == 0 {
		return n, nil
	}
	n.obstacles = couplingObstacles(prepared)
	items := make([]int, len(n.obstacles))
	for i := range items {
		items[i] = i
	}
	n.index = buildCouplingBoxIndex(n.obstacles, items)
	return n, nil
}

func couplingObstacles(p *PreparedNetwork) []couplingObstacle {
	var obstacles []couplingObstacle
	for _, lane := range p.network.Lanes {
		geometry, cells := p.geometry[lane.ID], p.laneCells[lane.ID]
		length := p.graph.lengths[p.graph.lanes[lane.ID]]
		clearance := classSetPairClearance(classBit(string(CompactClass)), lane.VehicleClasses)
		for cell := range cells.count() {
			start, end := cellOffset(cell, cells.count(), length), cellOffset(cell+1, cells.count(), length)
			for _, segment := range geometry.segments {
				lo, hi := max(start, segment.start), min(end, segment.end)
				if lo >= hi {
					continue
				}
				from, to := couplingSegmentPoint(segment, lo), couplingSegmentPoint(segment, hi)
				obstacles = append(obstacles, couplingObstacle{lane: lane.ID, cell: cell, from: from, to: to,
					resources: cells.cell(cell), clearance: clearance, release: releaseInput{from: lane.From, start: start, end: end, tail: cells.tail, fromTail: cells.fromTail}, box: couplingSegmentBox(from, to, clearance)})
			}
		}
	}
	positions := make(map[string]Point, len(p.network.Nodes))
	for _, node := range p.network.Nodes {
		positions[node.ID] = node.Position
	}
	for _, station := range p.network.Stations {
		for _, berth := range station.Berths {
			clearance := Clearance
			if largeBerth(station, berth) {
				clearance = largeClearance
			}
			point := positions[berth.Node]
			claimed := berthResources(berth)
			obstacles = append(obstacles, couplingObstacle{cell: -1, from: point, to: point, resources: slices.Clone(claimed[:]),
				clearance: clearance, box: couplingSegmentBox(point, point, clearance)})
		}
	}
	return obstacles
}

func couplingSegmentPoint(segment laneSegment, distance float64) Point {
	fraction := (distance - segment.start) / (segment.end - segment.start)
	return Point{X: segment.from.X + (segment.to.X-segment.from.X)*fraction, Y: segment.from.Y + (segment.to.Y-segment.from.Y)*fraction}
}

func couplingSegmentBox(a, b Point, margin float64) couplingBox {
	return couplingBox{low: Point{X: min(a.X, b.X) - margin, Y: min(a.Y, b.Y) - margin},
		high: Point{X: max(a.X, b.X) + margin, Y: max(a.Y, b.Y) + margin}}
}

func (box couplingBox) intersects(other couplingBox) bool {
	return box.low.X <= other.high.X && other.low.X <= box.high.X && box.low.Y <= other.high.Y && other.low.Y <= box.high.Y
}

func couplingUnionBox(a, b couplingBox) couplingBox {
	return couplingBox{low: Point{X: min(a.low.X, b.low.X), Y: min(a.low.Y, b.low.Y)}, high: Point{X: max(a.high.X, b.high.X), Y: max(a.high.Y, b.high.Y)}}
}

func buildCouplingBoxIndex(obstacles []couplingObstacle, items []int) *couplingBoxIndex {
	if len(items) == 0 {
		return nil
	}
	box := obstacles[items[0]].box
	for _, i := range items[1:] {
		box = couplingUnionBox(box, obstacles[i].box)
	}
	node := &couplingBoxIndex{box: box}
	if len(items) == 1 {
		node.items = items
		return node
	}
	x := box.high.X-box.low.X >= box.high.Y-box.low.Y
	slices.SortFunc(items, func(a, b int) int {
		first, second := obstacles[a].box, obstacles[b].box
		one, two := first.low.Y+first.high.Y, second.low.Y+second.high.Y
		if x {
			one, two = first.low.X+first.high.X, second.low.X+second.high.X
		}
		if one < two {
			return -1
		}
		if one > two {
			return 1
		}
		return a - b
	})
	middle := len(items) / 2
	node.left = buildCouplingBoxIndex(obstacles, items[:middle:middle])
	node.right = buildCouplingBoxIndex(obstacles, items[middle:])
	return node
}

func (node *couplingBoxIndex) visit(box couplingBox, visit func(int) error) error {
	if node == nil || !node.box.intersects(box) {
		return nil
	}
	for _, item := range node.items {
		if err := visit(item); err != nil {
			return err
		}
	}
	if err := node.left.visit(box, visit); err != nil {
		return err
	}
	return node.right.visit(box, visit)
}

// Check every potentially contacting foreign cell, not only lane endpoints.
func (n *couplingReservationNetwork) proveInteractions(route *blockList, first, through int) error {
	for _, b := range route.span(first, through+1) {
		geometry := b.geometry
		for _, segment := range geometry.segments {
			lo, hi := max(b.start-b.laneStart, segment.start), min(b.end-b.laneStart, segment.end)
			if lo >= hi {
				continue
			}
			from, to := couplingSegmentPoint(segment, lo), couplingSegmentPoint(segment, hi)
			if err := n.index.visit(couplingSegmentBox(from, to, conflictSlack), func(index int) error {
				return n.proveCellInteraction(b, from, to, lo+b.laneStart, n.obstacles[index])
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (n *couplingReservationNetwork) proveCellInteraction(b block, from, to Point, segmentStart float64, obstacle couplingObstacle) error {
	if couplingSegmentsDistance(from, to, obstacle.from, obstacle.to) > obstacle.clearance+conflictSlack {
		return nil
	}
	if obstacle.lane == b.lane.ID {
		// Ordinary directed track ownership and release tails protect followers.
		if max(Clearance, b.tail) < obstacle.clearance {
			return couplingDenied("same-lane tail lacks external clearance")
		}
		return nil
	}
	for _, r := range b.resources {
		if r.kind == trackResource || !slices.Contains(obstacle.resources, r) {
			continue
		}
		targetEnd := b.end
		if obstacle.cell < 0 {
			targetEnd = couplingPointContactEnd(from, to, obstacle.from, obstacle.clearance, segmentStart)
		}
		if resourceReleaseDistance(b, r) < targetEnd-conflictSlack {
			continue
		}
		if obstacle.cell >= 0 && releaseDistance(r, obstacle.release) < obstacle.release.end {
			continue
		}
		return nil
	}
	return couplingDenied(fmt.Sprintf("lane %q cell %d lacks a shared exclusion resource", b.lane.ID, b.cell))
}

func couplingPointContactEnd(from, to, point Point, clearance, start float64) float64 {
	length := pointDistance(from, to)
	if length == 0 {
		return start
	}
	ux, uy := (to.X-from.X)/length, (to.Y-from.Y)/length
	dx, dy := from.X-point.X, from.Y-point.Y
	projection := dx*ux + dy*uy
	perpendicular := max(0, dx*dx+dy*dy-projection*projection)
	reach := math.Sqrt(max(0, clearance*clearance-perpendicular))
	return start + min(length, max(0, -projection+reach))
}

func couplingSegmentsDistance(a, b, c, d Point) float64 {
	cross := func(p, q, r Point) float64 { return (q.X-p.X)*(r.Y-p.Y) - (q.Y-p.Y)*(r.X-p.X) }
	one, two, three, four := cross(a, b, c), cross(a, b, d), cross(c, d, a), cross(c, d, b)
	if ((one < 0 && two > 0) || (one > 0 && two < 0)) && ((three < 0 && four > 0) || (three > 0 && four < 0)) {
		return 0
	}
	return math.Min(math.Min(segmentPointDistance(a, c, d), segmentPointDistance(b, c, d)), math.Min(segmentPointDistance(c, a, b), segmentPointDistance(d, a, b)))
}

// Build fresh cursor state from immutable cells without Simulation cache writes.
func (n *couplingReservationNetwork) routeBlocks(route []Lane) (blockList, error) {
	if len(route) == 0 || len(route) > expressMaxLanes {
		return blockList{}, couplingDenied("route exceeds existing bounds")
	}
	blocks := blockList{route: cloneLanes(route), lanes: make([]routeLaneCells, len(route)+1)}
	for i, lane := range route {
		geometry, exists := n.lanes[lane.ID]
		if !exists || !reflect.DeepEqual(geometry.lane, lane) || !lane.VehicleClasses.Allows(string(CompactClass)) {
			return blockList{}, couplingDenied("route lane differs from prepared Compact path")
		}
		if i > 0 && route[i-1].To != lane.From {
			return blockList{}, couplingDenied("route is disconnected")
		}
		length := n.prepared.graph.lengths[n.prepared.graph.lanes[lane.ID]]
		entry := &blocks.lanes[i]
		entry.length, entry.cells = length, n.prepared.laneCells[lane.ID]
		entry.geometry = entry.cells.geometry
		blocks.lanes[i+1].first = entry.first + entry.cells.count()
		blocks.lanes[i+1].start = entry.start + length
	}
	blocks.blocks = blocks.lanes[len(route)].first
	return blocks, nil
}
