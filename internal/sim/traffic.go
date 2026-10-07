package sim

import (
	"cmp"
	"fmt"
	"iter"
	"math"
	"slices"
)

type resourceKind int

const (
	berthResource resourceKind = iota
	nodeResource
	trackResource
	junctionResource
)

type resource struct {
	kind resourceKind
	id   string
	cell int
}

// block is one cell of a route lane. blockList makes it from the shared
// cells of the lane. start, end and laneStart are route distances.
type block struct {
	lane       Lane
	cell       int
	start, end float64
	laneStart  float64
	// resources is the shared slice of the cell. No code writes to it.
	resources []resource
	last      bool
	// geometry is the geometry of lane, or nil when the geometry index did
	// not have lane when newLaneCells made the cells of lane.
	geometry *laneGeometry
	// Larger immutable geometry bounds apply to every owner of the cell.
	tail, fromTail float64
}

type laneConflict struct {
	junction   string
	start, end float64
}

// laneBlockCount returns the number of track cells in a lane of a length in
// meters.
func laneBlockCount(length float64) int {
	return max(2, int(math.Ceil(length/30)))
}

// LaneBlocks returns the number of track cells (blocks) of each lane, in
// lane order. A route through a lane has these blocks, and the restore of a
// saved state sets its budget from their total. For an unknown node, it
// uses the origin.
func (n Network) LaneBlocks() []int {
	positions := make(map[string]Point, len(n.Nodes))
	for _, node := range n.Nodes {
		// Network.Node gives the first node with an ID.
		if _, ok := positions[node.ID]; !ok {
			positions[node.ID] = node.Position
		}
	}
	blocks := make([]int, len(n.Lanes))
	for index, lane := range n.Lanes {
		blocks[index] = laneBlockCount(indexedLaneLength(lane, positions[lane.From], positions[lane.To]))
	}
	return blocks
}

// indexBerthResources returns the berth resources at each node, in station
// order and then in berth order.
func indexBerthResources(network Network) map[string][]resource {
	resources := make(map[string][]resource)
	for _, station := range network.Stations {
		for _, berth := range station.Berths {
			resources[berth.Node] = append(resources[berth.Node], resource{kind: berthResource, id: berth.ID})
		}
	}
	return resources
}

// laneCells holds the resources of the blocks of one lane in the form that
// each route through the lane shares. NewFleet and ensureNetworkIndexes
// build the cells of each network lane, and no code writes to them.
type laneCells struct {
	tail, fromTail float64
	// geometry is the geometry of the lane, or nil when the geometry index
	// does not have the lane.
	geometry *laneGeometry
	// resources holds the resources of all cells in cell order. ends holds
	// the index in resources after the last resource of each cell.
	resources []resource
	ends      []int
}

// count returns the number of cells.
func (c *laneCells) count() int {
	return len(c.ends)
}

// cell returns the resources of a cell.
func (c *laneCells) cell(cell int) []resource {
	start, end := 0, c.ends[cell]
	if cell > 0 {
		start = c.ends[cell-1]
	}
	return c.resources[start:end:end]
}

// add adds a cell with the given resources.
func (c *laneCells) add(resources []resource) {
	c.resources = append(c.resources, resources...)
	c.ends = append(c.ends, len(c.resources))
}

// cellOffset returns the distance from the start of a lane to the start of
// a cell, when count cells of equal length divide the lane.
func cellOffset(cell, count int, length float64) float64 {
	return float64(cell) * length / float64(count)
}

// laneCellsInput holds the lane data that newLaneCells uses. berths holds
// the berth resources at the To node of the lane.
type laneCellsInput struct {
	tail, fromTail float64
	lane           Lane
	length         float64
	geometry       *laneGeometry
	berths         []resource
	conflicts      []laneConflict
}

// newLaneCells divides a lane into laneBlockCount cells of equal length.
func newLaneCells(input laneCellsInput) *laneCells {
	lane, length := input.lane, input.length
	count := laneBlockCount(length)
	cells := laneCells{tail: input.tail, fromTail: input.fromTail}
	if largeClassSet(lane.VehicleClasses) {
		cells.tail, cells.fromTail = max(largeClearance, cells.tail), max(largeClearance, cells.fromTail)
	}
	var resources []resource
	for cell := range count {
		last := cell == count-1
		start, end := cellOffset(cell, count, length), cellOffset(cell+1, count, length)
		resources = resources[:0]
		if last {
			resources = append(resources, input.berths...)
		}
		if cell == 0 || start < cells.fromTail {
			resources = append(resources, resource{kind: nodeResource, id: lane.From})
		}
		if last || end > length-cells.tail && cells.tail > 0 {
			resources = append(resources, resource{kind: nodeResource, id: lane.To})
		}
		// A lane has conflicts only at its From and To nodes, so a cell gets
		// at most two junction resources. Many lanes can conflict with the
		// lane at one node, but a second copy of a resource does not change
		// a reservation, so the cell keeps only the first. Thus a cell has
		// at most six resources.
		for _, conflict := range input.conflicts {
			junction := resource{kind: junctionResource, id: conflict.junction}
			if start < conflict.end && conflict.start < end && !slices.Contains(resources, junction) {
				resources = append(resources, junction)
			}
		}
		cells.add(append(resources, resource{kind: trackResource, id: lane.ID, cell: cell}))
	}
	// The cells keep only the memory that they use.
	return &laneCells{tail: cells.tail, fromTail: cells.fromTail, geometry: input.geometry, resources: slices.Clone(cells.resources), ends: slices.Clone(cells.ends)}
}

// indexLaneCells returns the cells of each network lane. The cells of all
// lanes hold one block for each block that Network.LaneBlocks counts, so
// project validation bounds them.
func indexLaneCells(input laneCellsIndexInput) map[string]*laneCells {
	network := input.network
	tails := indexGeometryTails(network)
	positions := make(map[string]Point, len(network.Nodes))
	for _, node := range network.Nodes {
		if _, ok := positions[node.ID]; !ok {
			positions[node.ID] = node.Position
		}
	}
	cells := make(map[string]*laneCells, len(network.Lanes))
	for _, lane := range network.Lanes {
		if _, ok := cells[lane.ID]; ok {
			continue
		}
		cells[lane.ID] = newLaneCells(laneCellsInput{
			tail: tails[lane.ID].tail, fromTail: tails[lane.ID].fromTail,
			lane: lane, length: indexedLaneLength(lane, positions[lane.From], positions[lane.To]), geometry: input.geometry[lane.ID],
			berths: input.berths[lane.To], conflicts: input.conflicts[lane.ID],
		})
	}
	return cells
}

// laneCellsIndexInput holds the network and the indexes that
// indexLaneCells uses.
type laneCellsIndexInput struct {
	network   Network
	geometry  map[string]*laneGeometry
	conflicts map[string][]laneConflict
	berths    map[string][]resource
}

// routeLaneCells holds the blocks of one lane of a route.
type routeLaneCells struct {
	// first is the index of the first block of the lane.
	first int
	// start is the route distance at the start of the lane. length is the
	// lane length that the cells divide.
	start, length float64
	cells         *laneCells
	// geometry is cells.geometry. A moving pod reads it at each tick, and
	// this copy saves a memory read.
	geometry *laneGeometry
}

// blockCursor keeps the result of a lookup of a block.
type blockCursor struct {
	// index is the index of the block plus one, or 0 when the cursor is
	// empty.
	index int
	// lane is the route index of the lane of the block. After a lookup of
	// another block, it is the lane to try first.
	lane int
	// end is the route distance at the end of the block.
	end float64
}

// blockList holds the blocks of a route. For each route lane, it holds the
// shared cells of the lane and the route distance at the lane start. Thus
// its size grows with the route lanes, and not with the blocks.
type blockList struct {
	route []Lane
	// lanes holds the blocks of each route lane, and then an entry whose
	// first is the number of blocks.
	lanes []routeLaneCells
	// blocks is the number of blocks.
	blocks int
	// At each tick, a pod looks up the block that it is in with
	// cursors[podCursor] (see currentLane), and the last block that it
	// reserved with cursors[endCursor] (see end). Most of these lookups
	// find the same block as the last lookup. scan is the route index of
	// the lane that another lookup found last. Most other lookups find a
	// block in the same lane or in the next lane. The cursors and scan do
	// not change a result, but a lookup writes them, so two goroutines
	// must not use one list at the same time.
	cursors [2]blockCursor
	scan    int
}

// The cursors of a blockList.
const (
	podCursor = iota
	endCursor
)

// len returns the number of blocks.
func (l *blockList) len() int {
	return l.blocks
}

// laneFirst returns the index of the first block of the lane at a route
// index.
func (l *blockList) laneFirst(lane int) int {
	return l.lanes[lane].first
}

// find returns cursor after a lookup of the block at index.
func (l *blockList) find(index int, cursor *blockCursor) *blockCursor {
	if cursor.index != index+1 {
		l.seek(index, cursor)
	}
	return cursor
}

// seek moves cursor to the block at index.
func (l *blockList) seek(index int, cursor *blockCursor) {
	lane := l.locate(index, cursor.lane)
	*cursor = blockCursor{index: index + 1, lane: lane, end: l.cellEnd(lane, index-l.lanes[lane].first)}
}

// holds reports whether the lane at a route index holds the block at index.
func (l *blockList) holds(lane, index int) bool {
	return lane >= 0 && lane+1 < len(l.lanes) && l.lanes[lane].first <= index && index < l.lanes[lane+1].first
}

// locate returns the route index of the lane of a block. It tries the lane
// at route index hint and the next lane before it searches all lanes.
func (l *blockList) locate(index, hint int) int {
	switch {
	case l.holds(hint, index):
		return hint
	case l.holds(hint+1, index):
		return hint + 1
	}
	found, _ := slices.BinarySearchFunc(l.lanes[:len(l.lanes)-1], index+1, func(entry routeLaneCells, target int) int {
		return cmp.Compare(entry.first, target)
	})
	return found - 1
}

// routeLane returns the route index of the lane of a block.
func (l *blockList) routeLane(index int) int {
	if !l.holds(l.scan, index) {
		l.scan = l.locate(index, l.scan)
	}
	return l.scan
}

// cell returns the route index of the lane of a block, and the cell of the
// block in that lane.
func (l *blockList) cell(index int) (lane, cell int) {
	lane = l.routeLane(index)
	return lane, index - l.lanes[lane].first
}

// lane returns the lane of a block.
func (l *blockList) lane(index int) *Lane {
	return &l.route[l.routeLane(index)]
}

// currentLane returns the lane of the block that the pod is in, at index.
func (l *blockList) currentLane(index int) *Lane {
	return &l.route[l.find(index, &l.cursors[podCursor]).lane]
}

// end returns the route distance at the end of the last block that the pod
// reserved, at index.
func (l *blockList) end(index int) float64 {
	return l.find(index, &l.cursors[endCursor]).end
}

// cellEnd returns the route distance at the end of a cell of the lane at a
// route index.
func (l *blockList) cellEnd(lane, cell int) float64 {
	entry := &l.lanes[lane]
	return entry.start + cellOffset(cell+1, l.lanes[lane+1].first-entry.first, entry.length)
}

// next returns the route index of the lane of the block at index. lane is
// the route index of the lane of the block or of an earlier block. A walk
// in index order uses next, so it does not search for each block.
func (l *blockList) next(lane, index int) int {
	for index >= l.lanes[lane+1].first {
		lane++
	}
	return lane
}

// at returns the block at an index.
func (l *blockList) at(index int) block {
	lane, cell := l.cell(index)
	return l.block(lane, cell)
}

// block returns a cell of the lane at a route index.
func (l *blockList) block(lane, cell int) block {
	entry := &l.lanes[lane]
	count := l.lanes[lane+1].first - entry.first
	start, end := l.cellBounds(lane, cell)
	return block{
		lane: l.route[lane], geometry: entry.geometry, cell: cell, start: start, end: end,
		laneStart: entry.start, resources: entry.cells.cell(cell), last: cell == count-1,
		tail: entry.cells.tail, fromTail: entry.cells.fromTail,
	}
}

// cellBounds returns the route distances at the start and at the end of a
// cell of the lane at a route index.
func (l *blockList) cellBounds(lane, cell int) (start, end float64) {
	entry := &l.lanes[lane]
	count := l.lanes[lane+1].first - entry.first
	return entry.start + cellOffset(cell, count, entry.length), entry.start + cellOffset(cell+1, count, entry.length)
}

// span returns the blocks from index from up to but not including index to,
// with their indexes.
func (l *blockList) span(from, to int) iter.Seq2[int, block] {
	return func(yield func(int, block) bool) {
		if from >= to {
			return
		}
		lane := l.routeLane(from)
		for index := from; index < to; index++ {
			for index >= l.lanes[lane+1].first {
				lane++
			}
			if !yield(index, l.block(lane, index-l.lanes[lane].first)) {
				return
			}
		}
	}
}

// peekSpan is span with no write to the list. It finds the lane of from
// with scan as a hint and does not move scan, so a reader that must not
// change any state, such as a refused debris start, can walk the blocks.
// span keeps its own walk, because the motion of each tick uses it.
func (l *blockList) peekSpan(from, to int) iter.Seq2[int, block] {
	return func(yield func(int, block) bool) {
		if from >= to {
			return
		}
		lane := l.locate(from, l.scan)
		for index := from; index < to; index++ {
			for index >= l.lanes[lane+1].first {
				lane++
			}
			if !yield(index, l.block(lane, index-l.lanes[lane].first)) {
				return
			}
		}
	}
}

// spanResources returns the resources of the blocks from index from up to
// but not including index to. It does not make the blocks.
func (l *blockList) spanResources(from, to int) iter.Seq[[]resource] {
	return func(yield func([]resource) bool) {
		if from >= to {
			return
		}
		lane := l.routeLane(from)
		for index := from; index < to; index++ {
			for index >= l.lanes[lane+1].first {
				lane++
			}
			if !yield(l.lanes[lane].cells.cell(index - l.lanes[lane].first)) {
				return
			}
		}
	}
}

// routeBlocks returns the blocks of a route. The route uses the shared cells
// of each lane, so all routes through a lane have the same cell boundaries.
// A lane that has no cells, as in a Simulation literal of a test, gets new
// cells. routeBlocks also returns the value of laneLength for each route
// lane.
func (s *Simulation) routeBlocks(route []Lane) (blockList, []float64) {
	var lengths []float64
	if len(route) == 0 {
		return blockList{}, lengths
	}
	blocks := blockList{route: route, lanes: make([]routeLaneCells, len(route)+1)}
	lengths = make([]float64, 0, len(route))
	distance := 0.0
	var fallbackTails map[string]geometryTails
	for index, lane := range route {
		length := s.laneLength(lane)
		lengths = append(lengths, length)
		cells := s.laneCells[lane.ID]
		if cells == nil {
			if fallbackTails == nil {
				fallbackTails = indexGeometryTails(s.network)
			}
			cells = newLaneCells(laneCellsInput{
				tail: fallbackTails[lane.ID].tail, fromTail: fallbackTails[lane.ID].fromTail,
				lane: lane, length: length, geometry: s.geometry[lane.ID],
				berths: s.berthResources[lane.To], conflicts: s.junctionConflicts[lane.ID],
			})
		}
		entry := &blocks.lanes[index]
		entry.start, entry.length, entry.cells, entry.geometry = distance, length, cells, cells.geometry
		blocks.lanes[index+1].first = entry.first + cells.count()
		distance += length
	}
	blocks.lanes[len(route)].start = distance
	blocks.blocks = blocks.lanes[len(route)].first
	return blocks, lengths
}

type intent struct {
	index, block int
	since        int64
	priority     int
	id           string
	// emergency is the serial of the emergency record of the pod, or 0
	// (section 9.3 of the incident emergency contract).
	emergency uint64
}

// admissionWork belongs to one simulation. Each pass clears pod IDs before
// retaining the storage for the next pass.
type admissionWork struct {
	intents []intent
	pickups map[string]bool
}

func (s *Simulation) setVehicleRoute(v *vehicle, route []Lane) {
	v.replaceRoute(route)
	v.blocks, v.routeLengths = s.routeBlocks(route)
	v.blockStarts = indexBlockStarts(&v.blocks, len(route))
	v.terminal = terminalCheck{}
}

func indexBlockStarts(blocks *blockList, capacity int) map[string]int {
	starts := make(map[string]int, capacity)
	for index, lane := range blocks.route {
		if _, exists := starts[lane.ID]; !exists {
			starts[lane.ID] = blocks.laneFirst(index)
		}
	}
	return starts
}

func (v *vehicle) firstBlockForLane(laneID string) int {
	if first, ok := v.blockStarts[laneID]; ok {
		return first
	}
	return firstBlockForLane(&v.blocks, laneID)
}

// SetReservationLookahead controls how early pods request track beyond their
// braking distance. It does not change physical clearance.
func (s *Simulation) SetReservationLookahead(seconds float64) error {
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 0 || seconds > maxReservationLookaheadSeconds {
		return fmt.Errorf("reservation lookahead must be between 0 and %.0f seconds", maxReservationLookaheadSeconds)
	}
	s.reservationLookaheadSeconds = seconds
	return nil
}

func (s *Simulation) admit() {
	if s.admissionWork == nil {
		s.admissionWork = &admissionWork{pickups: make(map[string]bool)}
	}
	work := s.admissionWork
	s.surrenderWaitingClaims()
	intents := work.intents[:0]
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if v.faulted {
			// A faulted pod requests no grant.
			s.reportFault(v)
			continue
		}
		ready := departs(v.Pod.Activity) && v.phaseTicks == 0
		if !ready && v.Pod.Activity != Traveling {
			continue
		}
		if !s.assignTerminalBerth(v) {
			// The failed attempt replaces the report of the previous
			// tick while the blocked set is not empty.
			s.reportBlockedBerths(v)
			continue
		}
		s.reevaluateTerminalBerth(v)
		v.Pod.WaitReason, v.Pod.BlockedBy = NoWait, ""
		// Reserve enough track for cruising speed plus the configured lookahead.
		// A denied extension leaves the existing stopping boundary intact.
		next, ok := s.admissionRequest(v)
		if !ok {
			continue
		}
		if v.pending != next {
			v.pending, v.waitSince = next, s.tick
		}
		intents = append(intents, intent{index: i, block: next, since: v.waitSince, id: v.Pod.ID})
	}
	pickups := work.pickups
	for _, trip := range s.waiting {
		if trip.request.PodID != "" {
			pickups[trip.request.PodID] = true
		}
	}
	for i := range intents {
		v := &s.vehicles[intents[i].index]
		intents[i].priority = admissionPriority(v, pickups[v.Pod.ID])
		if record := s.emergencyOf(v); record >= 0 {
			intents[i].emergency = s.emergencies[record].serial
		}
	}
	slices.SortFunc(intents, func(a, b intent) int {
		return compareAdmission(a, b, s.tick)
	})
	for _, in := range intents {
		s.grant(in)
	}
	clear(intents)
	work.intents = intents[:0]
	clear(pickups)
}

const admissionAgeTicks = 10 * TicksPerSecond

func admissionPriority(v *vehicle, pickup bool) int {
	if v.carriesPassengers() {
		return 0
	}
	if pickup {
		return 1
	}
	return 2
}

// compareAdmission gives the requests of emergency pods precedence over
// every other request, in the order of their record serials (section 9.3
// of the incident emergency contract). Among the other requests, a request
// that waited ten seconds has precedence over younger requests. Existing
// reservations do not participate in this order, so the emergency tier
// takes only free resources.
func compareAdmission(a, b intent, tick int64) int {
	if a.emergency != 0 || b.emergency != 0 {
		switch {
		case b.emergency == 0:
			return -1
		case a.emergency == 0:
			return 1
		}
		return cmp.Or(cmp.Compare(a.emergency, b.emergency), cmp.Compare(a.id, b.id))
	}
	agedA, agedB := tick-a.since >= admissionAgeTicks, tick-b.since >= admissionAgeTicks
	if agedA != agedB {
		if agedA {
			return -1
		}
		return 1
	}
	if !agedA {
		if order := cmp.Compare(a.priority, b.priority); order != 0 {
			return order
		}
	}
	if order := cmp.Compare(a.since, b.since); order != 0 {
		return order
	}
	return cmp.Compare(a.id, b.id)
}

// grant reserves the blocks of an intent up to the end of their conflict
// zones when no other pod holds a resource of them. A follower that
// linkedSpan accepts can also reserve a resource that a pod ahead in its
// platoon holds, except a berth. That pod stays the owner, and the
// resource passes to the follower when that pod releases it.
func (s *Simulation) grant(in intent) {
	v := &s.vehicles[in.index]
	through := reservationEnd(&v.blocks, in.block)
	linked := v.link.leader != 0 && s.linkedSpan(v, in.block, through)
	if v.link.leader != 0 && !linked && s.holdsPending(v) {
		// A follower that holds a cell of a pod ahead can be as far
		// forward in its reservation as that pod. If it took free cells
		// that its predecessor did not reserve yet, it would block its
		// own predecessor. Thus it takes only the cells of the pods ahead
		// until it owns each cell that it holds.
		v.Pod.BlockedBy = s.vehicles[v.link.leader-1].Pod.ID
		v.Pod.WaitReason = TrackOccupied
		return
	}
	// A route that passes a node again must not keep a resource that a
	// pod behind in the platoon shares past the run of that pod.
	if v.follower != 0 && s.returnsToShared(v, in.block, through) {
		v.Pod.BlockedBy = s.vehicles[v.follower-1].Pod.ID
		v.Pod.WaitReason = TrackOccupied
		return
	}
	for resources := range v.blocks.spanResources(in.block, through+1) {
		for _, r := range resources {
			if owner := s.owners[r]; !owner.isZero() && !owner.isPod(v.Pod.ID) &&
				(!linked || r.kind == berthResource || !s.ownerAheadInPlatoon(v, owner)) {
				if s.reportIncident(v, owner) {
					return
				}
				v.Pod.BlockedBy = owner.String()
				switch r.kind {
				case berthResource:
					v.Pod.WaitReason = BerthOccupied
				case nodeResource:
					v.Pod.WaitReason = JunctionOccupied
				case junctionResource:
					v.Pod.WaitReason = JunctionOccupied
				case trackResource:
					v.Pod.WaitReason = TrackOccupied
				}
				return
			}
		}
	}
	// This walk does not make the blocks, because a block holds a copy of
	// its lane. It uses the same values as span.
	blocks := &v.blocks
	lane := blocks.routeLane(in.block)
	for index := in.block; index <= through; index++ {
		lane = blocks.next(lane, index)
		cell := index - blocks.lanes[lane].first
		start, end := blocks.cellBounds(lane, cell)
		cells := blocks.lanes[lane].cells
		for _, r := range cells.cell(cell) {
			if !linked || s.owners[r].isZero() {
				s.owners[r] = podResourceOwner(v.Pod.ID)
			}
			v.retainRouteResource(r, releaseDistance(r, releaseInput{from: blocks.route[lane].From, start: start, end: end, tail: cells.tail, fromTail: cells.fromTail}))
		}
	}
	v.reservedThrough = through
	v.pending = -1
}

// reservationEnd reserves each contiguous conflict zone as one movement.
// A lane endpoint also needs its downstream cell before admission.
//
// For each junction resource of a block, the zone goes to the last block of
// the run of blocks after it that have the resource. The blocks of that run
// give the same last block, so reservationEnd keeps the run and does not
// scan it again. A kept run holds block i when it ends at i or later, and
// then block i has its resource. Thus runs has at most one entry for each
// junction resource of a block, and each block is scanned at most once for
// each of its junction resources.
func reservationEnd(blocks *blockList, start int) int {
	type junctionRun struct {
		junction resource
		end      int
	}
	var storage [4]junctionRun
	runs := storage[:0]
	through := start
	lane := blocks.routeLane(start)
	for i := start; i <= through; i++ {
		lane = blocks.next(lane, i)
		entry := &blocks.lanes[lane]
		// Block i is the last block of its lane.
		if i+1 == blocks.lanes[lane+1].first && i+1 < blocks.len() {
			through = max(through, i+1)
		}
		runs = slices.DeleteFunc(runs, func(run junctionRun) bool { return run.end < i })
		for _, r := range entry.cells.cell(i - entry.first) {
			if r.kind != junctionResource {
				continue
			}
			if k := slices.IndexFunc(runs, func(run junctionRun) bool { return run.junction == r }); k >= 0 {
				through = max(through, runs[k].end)
				continue
			}
			end, endLane := i, lane
			for end+1 < blocks.len() {
				endLane = blocks.next(endLane, end+1)
				if !slices.Contains(blocks.lanes[endLane].cells.cell(end+1-blocks.lanes[endLane].first), r) {
					break
				}
				end++
			}
			runs = append(runs, junctionRun{junction: r, end: end})
			through = max(through, end)
		}
	}
	return through
}

func (s *Simulation) move(v *vehicle) {
	limit := v.blocks.end(v.reservedThrough)
	if v.link.leader != 0 && v.platoonCap < limit {
		limit = v.platoonCap
	}
	blocks := &v.blocks
	current := blocks.find(v.blockIndex, &blocks.cursors[podCursor])
	var next ordinaryMoveResult
	if v.faulted {
		next = faultMoveStep(blocks, current.lane, v.distance, v.Pod.Speed, limit, v.faultCap)
	} else {
		next = ordinaryMoveStep(blocks, current.lane, v.distance, v.Pod.Speed, limit)
	}
	s.publishVehicleTravel(v, next.distance, next.speed)
}

func (s *Simulation) publishVehicleTravel(v *vehicle, distance, speed float64) {
	blocks := &v.blocks
	current := blocks.find(v.blockIndex, &blocks.cursors[podCursor])
	// A pod with no lane has left its berth, so it enters its current lane
	// in this tick.
	entered := current.lane + 1
	if v.Pod.LaneID == "" {
		entered = current.lane
	}
	v.distance, v.Pod.Speed = distance, speed
	for v.distance >= current.end {
		if v.blockIndex+1 == blocks.len() {
			s.recordLaneEntries(v, entered, current.lane)
			if v.destination.ID == "" {
				v.Pod.Speed = 0
				// A faulted pod keeps the fault report of admission.
				if !v.faulted {
					v.Pod.WaitReason = BerthOccupied
				}
				return
			}
			s.arrive(v)
			return
		}
		if v.blockIndex == v.reservedThrough {
			break
		}
		v.blockIndex++
		current = blocks.find(v.blockIndex, current)
	}
	lane := current.lane
	s.recordLaneEntries(v, entered, lane)
	v.Pod.LaneID, v.Pod.LaneDistance = blocks.route[lane].ID, v.distance-blocks.lanes[lane].start
	v.Pod.Position = s.lanePosition(blocks.lanes[lane].geometry, &blocks.route[lane], v.Pod.LaneDistance)
}

// speedBeforeLane limits the next speed so a pod can brake before each
// lower-speed lane. It checks lane starts, including lanes that one tick
// can cross. Reservation cells do not bound this scan.
func (l *blockList) speedBeforeLane(current int, distance, speed float64) float64 {
	dt := 1.0 / TicksPerSecond
	reach := speed*speed/(2*acceleration) + speed*dt
	for lane := current + 1; lane < len(l.route); lane++ {
		remaining := l.lanes[lane].start - distance
		if remaining > reach {
			break
		}
		limit := l.route[lane].SpeedLimit
		if limit >= speed {
			continue
		}
		// Above limit, the next stop point must stay before the lane start
		// plus the stopping distance at limit. At limit, entry is allowed.
		step := acceleration * dt
		safe := math.Sqrt(step*step+limit*limit+2*acceleration*math.Max(0, remaining)) - step
		speed = math.Min(speed, math.Max(limit, safe))
	}
	return speed
}

func (s *Simulation) releaseCleared() {
	for i := range s.vehicles {
		s.releaseVehicleResources(&s.vehicles[i])
	}
}

func resourceReleaseDistance(b block, r resource) float64 {
	return releaseDistance(r, releaseInput{from: b.lane.From, start: b.start, end: b.end, tail: b.tail, fromTail: b.fromTail})
}

func blockTail(b block) float64 { return max(Clearance, b.tail) }

func (v *vehicle) originTail() float64 {
	minimum := Clearance
	if largeVehicleClass(v.Pod.Class) {
		minimum = largeClearance
	}
	if len(v.blocks.lanes) > 1 && v.blocks.lanes[0].cells != nil {
		return max(minimum, v.blocks.lanes[0].cells.fromTail)
	}
	return minimum
}

// releaseInput holds the block values that releaseDistance uses. from is
// the From node of the lane of the block. start and end are route
// distances.
type releaseInput struct {
	from           string
	start, end     float64
	tail, fromTail float64
}

// releaseDistance returns the route distance at which a pod releases r of
// a block.
func releaseDistance(r resource, b releaseInput) float64 {
	if r.kind == junctionResource {
		return b.end
	}
	// A departure clears the node before it clears the first downstream cell.
	if r.kind == nodeResource && r.id == b.from {
		return b.start + max(Clearance, b.fromTail)
	}
	return b.end + max(Clearance, b.tail)
}

// retainRouteResource keeps r until the pod is at releaseAt. The pod or a
// pod ahead of it in its platoon must own r.
func (v *vehicle) retainRouteResource(r resource, releaseAt float64) {
	if v.routeReleases == nil {
		v.routeReleases = make(map[resource]float64)
	}
	v.routeReleases[r] = max(v.routeReleases[r], releaseAt)
	if v.nextRelease != 0 {
		v.nextRelease = min(v.nextRelease, releaseAt)
	}
}

func (s *Simulation) releaseVehicleResources(v *vehicle) {
	if v.Pod.Activity != Traveling {
		if len(v.routeReleases) == 0 {
			return
		}
		s.releaseRouteResourcesExcept(v,
			resource{kind: berthResource, id: v.Pod.BerthID},
			resource{kind: nodeResource, id: s.podBerthNode(v)},
		)
		return
	}
	s.releasePassedResources(v)
	if !v.originReleased && v.distance >= v.originTail() {
		for _, r := range []resource{
			{kind: berthResource, id: v.origin.ID},
			{kind: nodeResource, id: v.origin.Node},
		} {
			if releaseAt, retained := v.routeReleases[r]; !retained || releaseAt <= v.distance {
				s.releaseOwned(v, r)
			}
		}
		v.originReleased = true
	}
}

// releasePassedResources releases each resource in v.routeReleases that the
// pod passed. It removes the entry of a resource that neither the pod nor a
// pod ahead of it in its platoon owns. Then it sets v.nextRelease to the
// smallest release distance that stays, or to +Inf when no entry stays. An
// owner check is necessary only when v.nextRelease is 0. When v.nextRelease
// is not 0 and v.distance is less, no entry changes, so the function
// returns at once.
func (s *Simulation) releasePassedResources(v *vehicle) {
	checkOwners := v.nextRelease == 0
	if !checkOwners && v.distance < v.nextRelease {
		return
	}
	next := math.Inf(1)
	for r, releaseAt := range v.routeReleases {
		if owner := s.owners[r]; checkOwners && !owner.isPod(v.Pod.ID) && !s.ownerAheadInPlatoon(v, owner) {
			delete(v.routeReleases, r)
			continue
		}
		if releaseAt <= v.distance {
			if s.owners[r] == podResourceOwner(v.Pod.ID) {
				s.releaseRouteResource(v, r)
			}
			delete(v.routeReleases, r)
			continue
		}
		next = min(next, releaseAt)
	}
	v.nextRelease = next
}

func (s *Simulation) releaseRouteResourcesExcept(v *vehicle, retained ...resource) {
	for r := range v.routeReleases {
		if !slices.Contains(retained, r) {
			s.releaseOwned(v, r)
		}
	}
	clear(v.routeReleases)
}

// releaseOwned releases r when the owner is v, as releaseRouteResource
// does. An entry for r in v.routeReleases then names a resource that v does
// not own, so releaseOwned sets v.nextRelease to 0.
func (s *Simulation) releaseOwned(v *vehicle, r resource) {
	if s.owners[r] == podResourceOwner(v.Pod.ID) {
		s.releaseRouteResource(v, r)
		v.nextRelease = 0
	}
}

func (s *Simulation) podBerthNode(v *vehicle) string {
	station, _ := s.station(v.Pod.StationID)
	berth, _ := station.berth(v.Pod.BerthID)
	return berth.Node
}

// replaceRoute updates presentation identity without changing movement storage.
func (v *vehicle) replaceRoute(route []Lane) {
	v.stationPhase = stationPhaseCheck{}
	if v.routeVersion < ^uint64(0) {
		v.routeVersion++
	}
	v.Route = route
}
