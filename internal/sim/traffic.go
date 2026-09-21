package sim

import (
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
type block struct {
	lane       Lane
	cell       int
	start, end float64
	laneStart  float64
	resources  []resource
	last       bool
}

type laneConflict struct {
	junction   string
	start, end float64
}

// routeBlocks uses the same cell boundaries for every route through a lane.
func (s *Simulation) routeBlocks(route []Lane) []block {
	if s.junctionConflicts == nil {
		s.junctionConflicts = buildJunctionConflicts(s.network)
	}
	var blocks []block
	distance := 0.0
	for _, lane := range route {
		length := s.laneLength(lane)
		count := max(2, int(math.Ceil(length/30)))
		for cell := range count {
			b := block{lane: lane, cell: cell, start: distance + float64(cell)*length/float64(count), end: distance + float64(cell+1)*length/float64(count), laneStart: distance, last: cell == count-1}
			for _, station := range s.network.Stations {
				for _, berth := range station.Berths {
					if b.last && lane.To == berth.Node {
						b.resources = append(b.resources, resource{kind: berthResource, id: berth.ID})
					}
				}
			}
			if cell == 0 {
				b.resources = append(b.resources, resource{kind: nodeResource, id: lane.From})
			}
			if b.last {
				b.resources = append(b.resources, resource{kind: nodeResource, id: lane.To})
			}
			for _, conflict := range s.junctionConflicts[lane.ID] {
				laneEnd := b.end - b.laneStart
				laneStart := b.start - b.laneStart
				if laneStart < conflict.end && conflict.start < laneEnd {
					b.resources = append(b.resources, resource{kind: junctionResource, id: conflict.junction})
				}
			}
			b.resources = append(b.resources, resource{kind: trackResource, id: lane.ID, cell: cell})
			blocks = append(blocks, b)
		}
		distance += length
	}
	return blocks
}

type intent struct {
	index, block int
	since        int64
	id           string
}

func (s *Simulation) admit() {
	var intents []intent
	for i := range s.vehicles {
		v := &s.vehicles[i]
		ready := (v.Pod.Activity == Boarding || v.Pod.Activity == DepartingEmpty) && v.phaseTicks == 0
		if !ready && v.Pod.Activity != Traveling {
			continue
		}
		if !s.assignTerminalBerth(v) {
			continue
		}
		s.reevaluateTerminalBerth(v)
		v.Pod.WaitReason, v.Pod.BlockedBy = NoWait, ""
		next := v.reservedThrough + 1
		if next >= len(v.blocks) {
			continue
		}
		// Reserve enough track for cruising speed plus two ticks of travel.
		// A denied extension leaves the existing stopping boundary intact.
		speed := math.Max(v.Pod.Speed, v.blocks[v.blockIndex].lane.SpeedLimit)
		horizon := speed*speed/(2*acceleration) + 2*speed/TicksPerSecond
		if v.reservedThrough >= 0 && v.blocks[v.reservedThrough].end-v.distance >= horizon {
			continue
		}
		if v.pending != next {
			v.pending, v.waitSince = next, s.tick
		}
		intents = append(intents, intent{index: i, block: next, since: v.waitSince, id: v.Pod.ID})
	}
	slices.SortFunc(intents, func(a, b intent) int {
		if a.since < b.since {
			return -1
		}
		if a.since > b.since {
			return 1
		}
		if a.id < b.id {
			return -1
		}
		if a.id > b.id {
			return 1
		}
		return 0
	})
	for _, in := range intents {
		s.grant(in)
	}
}

func (s *Simulation) grant(in intent) {
	v := &s.vehicles[in.index]
	through := reservationEnd(v.blocks, in.block)
	for _, b := range v.blocks[in.block : through+1] {
		for _, r := range b.resources {
			if owner := s.owners[r]; owner != "" && owner != v.Pod.ID {
				v.Pod.BlockedBy = owner
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
	for _, b := range v.blocks[in.block : through+1] {
		for _, r := range b.resources {
			s.owners[r] = v.Pod.ID
		}
	}
	v.reservedThrough = through
	v.pending = -1
}

// reservationEnd reserves each contiguous conflict zone as one movement.
// A lane endpoint also needs its downstream cell before admission.
func reservationEnd(blocks []block, start int) int {
	through := start
	for i := start; i <= through; i++ {
		if blocks[i].last && i+1 < len(blocks) {
			through = max(through, i+1)
		}
		for _, r := range blocks[i].resources {
			if r.kind != junctionResource {
				continue
			}
			for j := i + 1; j < len(blocks) && slices.Contains(blocks[j].resources, r); j++ {
				through = max(through, j)
			}
		}
	}
	return through
}

func (s *Simulation) move(v *vehicle) {
	limit := v.blocks[v.reservedThrough].end
	available := math.Max(0, limit-v.distance)
	dt := 1.0 / TicksPerSecond
	// Semi-implicit integration preserves enough owned track to stop on the next tick.
	safe := math.Sqrt(acceleration*acceleration*dt*dt+2*acceleration*available) - acceleration*dt
	v.Pod.Speed = math.Min(v.Pod.Speed+acceleration*dt, math.Min(v.blocks[v.blockIndex].lane.SpeedLimit, math.Max(0, safe)))
	travel := math.Min(available, v.Pod.Speed*dt)
	v.distance += travel
	if limit-v.distance < 1e-5 {
		v.distance = limit
		v.Pod.Speed = 0
	}
	for v.distance >= v.blocks[v.blockIndex].end {
		if v.blockIndex+1 == len(v.blocks) {
			s.arrive(v)
			return
		}
		if v.blockIndex == v.reservedThrough {
			break
		}
		v.blockIndex++
	}
	b := v.blocks[v.blockIndex]
	v.Pod.LaneID, v.Pod.LaneDistance = b.lane.ID, v.distance-b.laneStart
	v.Pod.Position = s.network.Position(b.lane, v.Pod.LaneDistance)
}

func (s *Simulation) releaseCleared() {
	keep := make(map[resource]bool, len(s.owners))
	for i := range s.vehicles {
		s.retainResources(&s.vehicles[i], keep)
	}
	for r := range s.owners {
		if !keep[r] {
			delete(s.owners, r)
		}
	}
}

func (s *Simulation) retainResources(v *vehicle, keep map[resource]bool) {
	retain := func(r resource) {
		if s.owners[r] == v.Pod.ID {
			keep[r] = true
		}
	}
	if v.Pod.Activity != Traveling {
		station, _ := s.network.Station(v.Pod.StationID)
		berth, _ := station.berth(v.Pod.BerthID)
		retain(resource{kind: berthResource, id: berth.ID})
		retain(resource{kind: nodeResource, id: berth.Node})
	} else {
		for i, b := range v.blocks {
			if i > v.reservedThrough {
				break
			}
			for _, r := range b.resources {
				clearAt := b.end + Clearance
				// Conflict extents already include physical clearance.
				if r.kind == junctionResource {
					clearAt = b.end
				}
				// A departure clears the node before it clears the first downstream cell.
				if r.kind == nodeResource && r.id == b.lane.From {
					clearAt = b.start + Clearance
				}
				if clearAt > v.distance {
					retain(r)
				}
			}
		}
		if v.distance < Clearance {
			retain(resource{kind: berthResource, id: v.origin.ID})
			retain(resource{kind: nodeResource, id: v.origin.Node})
		}
	}
	if v.RelocatingTo != "" {
		retain(resource{kind: berthResource, id: v.destination.ID})
		retain(resource{kind: nodeResource, id: v.destination.Node})
	}
}
