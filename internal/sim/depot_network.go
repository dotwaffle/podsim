package sim

import (
	"fmt"
	"maps"
	"math"
	"slices"
)

// depotIndex holds the depots of a validated network and the lane path of
// each depot lane. prepareNetwork builds it, and no code writes to it. A
// network with no depot has a nil index.
type depotIndex struct {
	depots []depotInfo
	// stations holds the index of the depot of each depot station.
	stations map[string]int
	// slots holds the place of each slot, by berth ID, and nodes by the
	// node of the slot.
	slots, nodes map[string]depotSlot
	// lanes holds the depot lanes: the fan path, the lead-in, the chain
	// lanes, the lead-out, and the throat path of each depot lane.
	lanes map[string]bool
	// chains holds the slot at the end of each chain lane, by lane ID.
	chains map[string]Berth
}

// depotInfo is one depot. incoming is the one lane that ends at the entry.
type depotInfo struct {
	station  Station
	incoming Lane
	lanes    []depotPath
}

// depotPath is the lane path of one depot lane. slots holds the slots from
// the head to the tail, and chain[i] goes from slots[i+1] to slots[i]. fan
// goes from the entry to the start of leadIn, and throat from the end of
// leadOut to the exit. Either can be empty.
type depotPath struct {
	slots   []Berth
	fan     []Lane
	leadIn  Lane
	chain   []Lane
	leadOut Lane
	throat  []Lane
}

// depotSlot is the place of a slot: the depot, the lane, and the slot in
// the lane, with 0 at the head.
type depotSlot struct {
	depot, lane, slot int
}

// depotDirectionTolerance bounds the difference of two unit directions
// that validateDepots treats as one direction.
const depotDirectionTolerance = 1e-9

// slot returns the place of a slot berth.
func (d *depotIndex) slot(berthID string) (depotSlot, bool) {
	if d == nil {
		return depotSlot{}, false
	}
	slot, ok := d.slots[berthID]
	return slot, ok
}

// depotLane reports whether a lane is a depot lane.
func (d *depotIndex) depotLane(id string) bool {
	return d != nil && d.lanes[id]
}

// chainMinimum returns the minimum length of a lane with a reduced
// retention. A chain lane needs only its retention, because its cells make
// it one atomic span (see everyCell). Any other lane needs twice its
// retention. A lane that does not qualify keeps LaneMinimumLength.
func (d *depotIndex) chainMinimum(lane Lane, retention *laneRetention) float64 {
	if r := retention.of(lane.ID); r > 0 && d != nil {
		if _, ok := d.chains[lane.ID]; ok {
			return r
		}
	}
	return retention.laneMinimum(lane)
}

// everyCell returns the resources that each cell of a lane holds: for a
// qualifying chain lane shorter than twice its retention, the slot at its
// end, its From node and its To node. Then no pod can enter the lane while
// another pod holds the slot ahead, and two pods are never in one chain
// lane.
func (d *depotIndex) everyCell(lane Lane, retention, length float64) []resource {
	if d == nil || !(retention > 0) || length >= 2*retention {
		return nil
	}
	slot, ok := d.chains[lane.ID]
	if !ok {
		return nil
	}
	return []resource{{kind: berthResource, id: slot.ID}, {kind: nodeResource, id: lane.From}, {kind: nodeResource, id: lane.To}}
}

// auditPath reports whether a path of the bank audit is a depot lane or a
// slot.
func (d *depotIndex) auditPath(path bankAuditPath) bool {
	if d == nil {
		return false
	}
	switch path.key.kind {
	case laneKeyKind:
		return d.lanes[path.id]
	case berthKeyKind:
		_, ok := d.slots[path.id]
		return ok
	default:
		return false
	}
}

// depotForbidden returns forbidden without the slot nodes of the depot lane
// of a berth, so that a route to or from a slot can pass the other slots of
// its lane. It returns forbidden for a berth that is not a slot.
func (n Network) depotForbidden(forbidden map[string]bool, berthID string) map[string]bool {
	for _, depot := range n.Depots {
		for _, lane := range depot.Lanes {
			if !slices.Contains(lane.BerthIDs, berthID) {
				continue
			}
			station, _ := n.Station(depot.Station)
			forbidden = maps.Clone(forbidden)
			for _, id := range lane.BerthIDs {
				if berth, ok := station.berth(id); ok && id != "" {
					delete(forbidden, berth.Node)
				}
			}
			return forbidden
		}
	}
	return forbidden
}

// validateDepots checks the depots, in depot order. nodes holds the node
// positions. For each depot, it refuses, in order: an unknown station or a
// second depot of a station; a station that is not a parking station or
// has banks; no lanes or more than MaxDepotLanes; a lane with no slot, or
// slots that do not list each berth of the station once; slots with more
// than one class set or a large class; an
// entry without exactly one incoming lane; a depot lane with no lane path;
// a lane path that is not straight and ordered; lanes that are not
// parallel; and a separation group on a slot or a depot lane.
func (n Network) validateDepots(nodes map[string]Point) error {
	_, err := newDepotIndex(n, nodes)
	return err
}

// newDepotIndex checks the depots of a network, in the order of
// validateDepots, and returns their index. It returns nil when the network
// has no depot.
func newDepotIndex(n Network, nodes map[string]Point) (*depotIndex, error) {
	if len(n.Depots) == 0 {
		return nil, nil
	}
	d := &depotIndex{
		stations: make(map[string]int), slots: make(map[string]depotSlot), nodes: make(map[string]depotSlot),
		lanes: make(map[string]bool), chains: make(map[string]Berth),
	}
	incoming, outgoing := make(map[string][]int), make(map[string][]int)
	for index, lane := range n.Lanes {
		incoming[lane.To] = append(incoming[lane.To], index)
		outgoing[lane.From] = append(outgoing[lane.From], index)
	}
	for index, depot := range n.Depots {
		station, ok := n.Station(depot.Station)
		if _, repeated := d.stations[depot.Station]; !ok || repeated {
			return nil, fmt.Errorf("depot %d has an unknown or repeated station %q", index, depot.Station)
		}
		d.stations[depot.Station] = index
		if !station.ParkingOnly || station.Banks != nil {
			return nil, fmt.Errorf("depot station %q must be a parking station with no banks", station.ID)
		}
		if len(depot.Lanes) == 0 || len(depot.Lanes) > MaxDepotLanes {
			return nil, fmt.Errorf("depot station %q needs 1 to %d lanes", station.ID, MaxDepotLanes)
		}
		info := depotInfo{station: station}
		listed := 0
		for laneIndex, lane := range depot.Lanes {
			if len(lane.BerthIDs) == 0 {
				return nil, fmt.Errorf("depot station %q must list each berth of the station once", station.ID)
			}
			path := depotPath{}
			for slotIndex, id := range lane.BerthIDs {
				berth, known := station.berth(id)
				if _, repeated := d.slots[id]; id == "" || !known || repeated {
					return nil, fmt.Errorf("depot station %q must list each berth of the station once", station.ID)
				}
				slot := depotSlot{depot: index, lane: laneIndex, slot: slotIndex}
				d.slots[id], d.nodes[berth.Node] = slot, slot
				path.slots = append(path.slots, berth)
				listed++
			}
			info.lanes = append(info.lanes, path)
		}
		if listed != len(station.Berths) {
			return nil, fmt.Errorf("depot station %q must list each berth of the station once", station.ID)
		}
		if slices.ContainsFunc(station.Berths, func(berth Berth) bool {
			return berth.VehicleClasses != station.Berths[0].VehicleClasses || largeBerth(station, berth)
		}) {
			return nil, fmt.Errorf("depot station %q slots need one class set with no large class", station.ID)
		}
		if len(incoming[station.Entry]) != 1 {
			return nil, fmt.Errorf("depot station %q entry needs exactly one incoming lane", station.ID)
		}
		info.incoming = n.Lanes[incoming[station.Entry][0]]
		walk := depotWalk{network: n, incoming: incoming, outgoing: outgoing, station: station, slots: d.nodes}
		for laneIndex := range info.lanes {
			if !walk.path(&info.lanes[laneIndex]) {
				return nil, fmt.Errorf("depot station %q lane %d has no lane path", station.ID, laneIndex)
			}
		}
		directions := make([]Point, len(info.lanes))
		for laneIndex, path := range info.lanes {
			if directions[laneIndex], ok = path.direction(nodes); !ok {
				return nil, fmt.Errorf("depot station %q lane %d is not straight and ordered", station.ID, laneIndex)
			}
		}
		if slices.ContainsFunc(directions, func(direction Point) bool {
			return math.Abs(direction.X-directions[0].X) > depotDirectionTolerance || math.Abs(direction.Y-directions[0].Y) > depotDirectionTolerance
		}) {
			return nil, fmt.Errorf("depot station %q lanes are not parallel", station.ID)
		}
		for _, path := range info.lanes {
			for _, lane := range path.lanes() {
				d.lanes[lane.ID] = true
			}
			for slot, lane := range path.chain {
				d.chains[lane.ID] = path.slots[slot]
			}
		}
		if slices.ContainsFunc(station.Berths, func(berth Berth) bool { return berth.SeparationGroup != "" }) ||
			slices.ContainsFunc(info.lanes, func(path depotPath) bool {
				return slices.ContainsFunc(path.lanes(), func(lane Lane) bool { return lane.SeparationGroup != "" })
			}) {
			return nil, fmt.Errorf("depot station %q has a separation group", station.ID)
		}
		d.depots = append(d.depots, info)
	}
	return d, nil
}

// lanes returns the lanes of the path, from the entry to the exit.
func (p depotPath) lanes() []Lane {
	lanes := slices.Concat(p.fan, []Lane{p.leadIn})
	for _, lane := range slices.Backward(p.chain) {
		lanes = append(lanes, lane)
	}
	return slices.Concat(lanes, []Lane{p.leadOut}, p.throat)
}

// direction returns the unit direction of the lead-in, the chain lanes and
// the lead-out. They need no control point, and their nodes must lie on
// one line in strictly increasing order from the tail to the head.
func (p depotPath) direction(nodes map[string]Point) (Point, bool) {
	ids := []string{p.leadIn.From}
	for _, slot := range slices.Backward(p.slots) {
		ids = append(ids, slot.Node)
	}
	ids = append(ids, p.leadOut.To)
	if p.leadIn.Control != nil || p.leadOut.Control != nil || slices.ContainsFunc(p.chain, func(lane Lane) bool { return lane.Control != nil }) {
		return Point{}, false
	}
	start, end := nodes[ids[0]], nodes[ids[len(ids)-1]]
	length := pointDistance(start, end)
	if !(length > 0) {
		return Point{}, false
	}
	direction := Point{X: (end.X - start.X) / length, Y: (end.Y - start.Y) / length}
	previous := 0.0
	for _, id := range ids[1:] {
		point := nodes[id]
		along := (point.X-start.X)*direction.X + (point.Y-start.Y)*direction.Y
		across := (point.X-start.X)*direction.Y - (point.Y-start.Y)*direction.X
		if !(along > previous) || math.Abs(across) > separationTolerance {
			return Point{}, false
		}
		previous = along
	}
	return direction, true
}

// depotWalk finds the lane paths of the lanes of one depot. slots holds the
// place of each slot node of the network so far.
type depotWalk struct {
	network            Network
	incoming, outgoing map[string][]int
	station            Station
	slots              map[string]depotSlot
}

// path fills the lanes of a depot lane path from its slots. Each slot node
// has exactly one incoming and one outgoing lane, and the chain lanes join
// the slots in order. The fan path and the throat path are simple chains:
// each of their inner nodes has exactly one incoming and one outgoing lane
// and is not a slot node or a gate of the station.
func (w depotWalk) path(p *depotPath) bool {
	for _, slot := range p.slots {
		if len(w.incoming[slot.Node]) != 1 || len(w.outgoing[slot.Node]) != 1 {
			return false
		}
	}
	for i := 1; i < len(p.slots); i++ {
		lane := w.network.Lanes[w.outgoing[p.slots[i].Node][0]]
		if lane.To != p.slots[i-1].Node {
			return false
		}
		p.chain = append(p.chain, lane)
	}
	p.leadIn = w.network.Lanes[w.incoming[p.slots[len(p.slots)-1].Node][0]]
	p.leadOut = w.network.Lanes[w.outgoing[p.slots[0].Node][0]]
	var ok bool
	if p.fan, ok = w.chain(p.leadIn.From, true, w.station.Entry); !ok {
		return false
	}
	slices.Reverse(p.fan)
	p.throat, ok = w.chain(p.leadOut.To, false, w.station.Exit)
	return ok
}

// chain walks from node to the gate, against the lanes when backward is
// true, and returns the lanes in walk order. It reports false when an inner
// node is not a simple chain node or when the walk does not reach the gate.
func (w depotWalk) chain(node string, backward bool, gate string) ([]Lane, bool) {
	var lanes []Lane
	for node != gate {
		_, slot := w.slots[node]
		if slot || node == w.station.Entry || node == w.station.Exit || len(w.incoming[node]) != 1 || len(w.outgoing[node]) != 1 || len(lanes) == len(w.network.Lanes) {
			return nil, false
		}
		if backward {
			lane := w.network.Lanes[w.incoming[node][0]]
			lanes, node = append(lanes, lane), lane.From
		} else {
			lane := w.network.Lanes[w.outgoing[node][0]]
			lanes, node = append(lanes, lane), lane.To
		}
	}
	return lanes, true
}
