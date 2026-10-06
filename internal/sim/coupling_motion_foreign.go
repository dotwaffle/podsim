package sim

import (
	"math"
	"reflect"
	"slices"
)

// A path owns its route and cursor seeds. It shares only prepared immutable cells.
type couplingForeignPath struct {
	network  *couplingReservationNetwork
	contract OrderContract
	id       string
	class    VehicleClass
	blocks   blockList
	parked   bool
	berth    Berth
	position Point
}

type couplingForeignSweep struct {
	Path                                     *couplingForeignPath
	Tick                                     int64
	Distance, Speed, NextDistance, NextSpeed float64
	ReservedThrough                          int
	Owners                                   *couplingForeignOwnerView
	native                                   *nativeForeignProof
}

func prepareCouplingForeignPath(n *couplingReservationNetwork, contract OrderContract, id string, class VehicleClass, route []Lane) (*couplingForeignPath, error) {
	if n == nil || !boundedContractID(id) || len(route) == 0 || len(route) > expressMaxLanes {
		return nil, couplingDenied("invalid foreign immutable path")
	}
	profile, ok := LookupVehicleClassWithOrderContract(class, contract)
	if !ok || !profile.PhysicalSupported {
		return nil, couplingDenied("foreign class has no approved profile")
	}
	blocks := blockList{route: cloneLanes(route), lanes: make([]routeLaneCells, len(route)+1)}
	for i, lane := range route {
		canonical, exists := n.lanes[lane.ID]
		if !exists || !reflect.DeepEqual(canonical.lane, lane) || !lane.VehicleClasses.Allows(string(profile.Class)) || i > 0 && route[i-1].To != lane.From {
			return nil, couplingDenied("foreign route differs from its actual prepared class path")
		}
		entry := &blocks.lanes[i]
		entry.cells = n.prepared.laneCells[lane.ID]
		entry.geometry = entry.cells.geometry
		entry.length = n.prepared.graph.lengths[n.prepared.graph.lanes[lane.ID]]
		blocks.lanes[i+1].first = entry.first + entry.cells.count()
		blocks.lanes[i+1].start = entry.start + entry.length
	}
	blocks.blocks = blocks.lanes[len(route)].first
	return &couplingForeignPath{network: n, contract: contract, id: id, class: profile.Class, blocks: blocks}, nil
}

// Completeness of the declared fleet is a future live-adapter precondition.
// Every declared identity needs one raw sweep from the same previous tick.
func (c *couplingMotionContext) checkForeignSweeps(previous, next couplingMotionState, sweeps []couplingForeignSweep) error {
	if len(sweeps) != len(c.foreignIDs) {
		return couplingMotionInvariant("foreign sweep set is incomplete")
	}
	seen := make(map[string]bool, len(sweeps))
	var memberSegments [2][]laneSegment
	for i := range memberSegments {
		var err error
		memberSegments[i], err = couplingMotionSegments(&c.reservation.routes[i], previous.Distances[i], next.Distances[i])
		if err != nil {
			return err
		}
	}
	for _, sweep := range sweeps {
		path := sweep.Path
		if path == nil || path.network != c.reservation.network || path.contract != c.reservation.orderContract || !couplingDeclaredForeign(c.foreignIDs, path.id) || seen[path.id] || sweep.Tick != previous.Tick {
			return couplingMotionInvariant("unknown, duplicate, or stale foreign sweep")
		}
		seen[path.id] = true
		if sweep.native != nil && (sweep.native.frame == nil || sweep.native.frame.fleet == nil || !nativeForeignHasContext(sweep.native.frame.fleet, c)) {
			return couplingMotionInvariant("native foreign certificate belongs to another motion context")
		}
		if err := couplingCheckForeignMotion(sweep); err != nil {
			return err
		}
		foreign, err := couplingForeignSegments(sweep)
		if err != nil {
			return err
		}
		if sweep.native != nil && sweep.native.pair.proof != nil && sweep.native.pair.member == 0 {
			if err := c.checkNativePairConnector(previous, next, sweep.native.pair.proof, memberSegments); err != nil {
				return err
			}
		}
		clearance := classPairClearance(CompactClass, path.class)
		for _, member := range memberSegments {
			for _, a := range member {
				for _, b := range foreign {
					if couplingSegmentsDistance(a.from, a.to, b.from, b.to) < clearance-conflictSlack {
						return couplingMotionInvariant("foreign swept class-pair exclusion failed")
					}
				}
			}
		}
		if next.Phase != couplingDraining {
			if err := c.checkConnectorSweep(previous, next, foreign, path.class); err != nil {
				return err
			}
		}
	}
	return nil
}

// This inactive certificate excludes ordinary stopping snaps and compact queue motion.
// A live adapter must prove the native next sweep before runtime activation.
func couplingCheckForeignMotion(sweep couplingForeignSweep) error {
	if sweep.native != nil {
		return checkNativeForeignSweep(sweep)
	}
	view := sweep.Owners
	if view == nil || view.path != sweep.Path || view.through != sweep.ReservedThrough || sweep.Distance < view.distance {
		return couplingMotionInvariant("foreign grant view has a stale path or bounds")
	}
	if sweep.Path.parked {
		if sweep.Distance != 0 || sweep.NextDistance != 0 || sweep.Speed != 0 || sweep.NextSpeed != 0 {
			return couplingMotionInvariant("parked foreign moved without a canonical route")
		}
		return nil
	}
	blocks := sweep.Path.blocks
	if !finite(sweep.Distance) || !finite(sweep.NextDistance) || !finite(sweep.Speed) || !finite(sweep.NextSpeed) || sweep.Distance < 0 || sweep.Speed < 0 || sweep.NextSpeed < 0 ||
		sweep.ReservedThrough < 0 || sweep.ReservedThrough >= blocks.len() || sweep.NextDistance != sweep.Distance+sweep.NextSpeed/TicksPerSecond || math.Abs(sweep.NextSpeed-sweep.Speed) > acceleration/TicksPerSecond {
		return couplingMotionInvariant("foreign raw motion cannot prove bounded Euler movement")
	}
	frontier := blocks.at(sweep.ReservedThrough).end
	for _, pair := range [][2]float64{{sweep.Distance, sweep.Speed}, {sweep.NextDistance, sweep.NextSpeed}} {
		if pair[0]+pair[1]*pair[1]/(2*acceleration)+pair[1]/TicksPerSecond > frontier {
			return couplingMotionInvariant("foreign current grants cannot stop its body")
		}
		if err := couplingMotionSpeedProof(&blocks, pair[0], pair[0], pair[1], acceleration); err != nil {
			return err
		}
	}
	if err := couplingMotionSpeedProof(&blocks, sweep.Distance, sweep.NextDistance, sweep.NextSpeed, acceleration); err != nil {
		return err
	}

	return nil
}

// Large profiles use the existing six-meter circle, not an invented width.
// Small foreign profiles retain their existing center exclusion contract.
func (c *couplingMotionContext) checkConnectorSweep(previous, next couplingMotionState, foreign []laneSegment, class VehicleClass) error {
	before, err := c.connectedFootprint(previous)
	if err != nil {
		return err
	}
	after, err := c.connectedFootprint(next)
	if err != nil {
		return err
	}
	corridor := c.reservation.network.corridors[c.reservation.corridorID]
	direction := c.reservation.network.lanes[corridor.LaneIDs[0]].direction
	length := pointDistance(before.Pins[1], after.Pins[0])
	end := couplingOffset(before.Pins[1], direction, length)
	radius := classPairClearance(CompactClass, class) / 2
	if largeVehicleClass(class) {
		radius = largeEnvelopeRadius
	}
	profile, _ := LookupCouplingProfile(c.reservation.network.contract)
	for _, segment := range foreign {
		if couplingSegmentsDistance(before.Pins[1], end, segment.from, segment.to) < radius+profile.ConnectorWidthMeters/2-conflictSlack {
			return couplingMotionInvariant("foreign exclusion envelope touches swept connector")
		}
	}
	return nil
}

type couplingForeignOwnerView struct {
	path     *couplingForeignPath
	through  int
	distance float64
	owners   map[resource]resourceOwner
}

func prepareCouplingParkedForeign(n *couplingReservationNetwork, contract OrderContract, id string, class VehicleClass, stationID, berthID string) (*couplingForeignPath, error) {
	if n == nil || !boundedContractID(id) {
		return nil, couplingDenied("invalid parked foreign identity")
	}
	profile, ok := LookupVehicleClassWithOrderContract(class, contract)
	if !ok || !profile.PhysicalSupported {
		return nil, couplingDenied("parked foreign class lacks an approved profile")
	}
	station, exists := n.prepared.network.Station(stationID)
	berth, found := station.berth(berthID)
	if !exists || !found || !station.VehicleClasses.Allows(string(profile.Class)) || !berth.VehicleClasses.Allows(string(profile.Class)) {
		return nil, couplingDenied("parked foreign has no compatible actual berth")
	}
	node, exists := n.prepared.network.Node(berth.Node)
	if !exists {
		return nil, couplingDenied("parked foreign node disappeared")
	}
	return &couplingForeignPath{network: n, contract: contract, id: id, class: profile.Class, parked: true, berth: berth, position: node.Position}, nil
}

func couplingForeignSegments(sweep couplingForeignSweep) ([]laneSegment, error) {
	if sweep.Path.parked {
		return []laneSegment{{from: sweep.Path.position, to: sweep.Path.position}}, nil
	}
	return couplingMotionSegments(&sweep.Path.blocks, sweep.Distance, sweep.NextDistance)
}

func couplingDeclaredForeign(ids []string, id string) bool {
	_, found := slices.BinarySearch(ids, id)
	return found
}

func nativeForeignHasContext(f *nativeForeignFleet, c *couplingMotionContext) bool {
	if f.context == c {
		return true
	}
	_, ok := f.pairs[c]
	return ok
}
