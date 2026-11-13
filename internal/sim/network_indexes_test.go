package sim

import (
	"math"
	"reflect"
	"testing"
)

// ensureNetworkIndexes builds the indexes of a network that a test changed or
// wrote in a literal. A prepared network builds them once, and no production
// code changes a network after that.
func (s *Simulation) ensureNetworkIndexes() {
	if len(s.graph.nodes) == len(s.network.Nodes) && len(s.graph.lengths) == len(s.network.Lanes) {
		return
	}
	// Other simulations share the indexes, so this builds a new value. It
	// keeps the safety indexes.
	p := *s.networkIndexes
	p.graph = newRouteGraph(p.network)
	p.stationIndexes = indexStations(p.network)
	p.stationForbidden = p.network.stationForbidden()
	p.geometry = buildLaneGeometry(p.network)
	p.junctionConflicts = buildJunctionConflicts(p.network)
	p.berthResources = indexBerthResources(p.network)
	tails := indexGeometryTails(p.network)
	p.tails = tails
	p.retention = newLaneRetention(p.network, tails)
	p.maneuvers, _ = newManeuverIndex(p.network, indexNodePositions(p.network.Nodes))
	p.depots, _ = newDepotIndex(p.network, indexNodePositions(p.network.Nodes))
	p.berthKeys = indexBerthKeys(p.network)
	p.lateral = newDepotLateral(p.network, p.depots, tails, p.berthKeys)
	p.laneCells = indexLaneCells(laneCellsIndexInput{network: p.network, tails: tails, geometry: p.geometry, conflicts: p.junctionConflicts, berths: p.berthResources, retention: p.retention, maneuvers: p.maneuvers, depots: p.depots})
	p.resourceLanes = indexResourceLanes(p.network, p.laneCells)
	p.platoonData = newPlatoonIndexData(p.network)
	s.networkIndexes = &p
	s.routes = nil
	s.routeOrder = nil
	s.routeTrees = nil
	s.pickupBounds = nil
	s.routeWork = nil
	// The blocked set holds lane indexes of the old network, and the
	// no-candidate memo node indexes.
	s.blocked = blockedSet{}
	s.emergencyMisses = nil
	s.pickupMisses = nil
	s.staticConnected, s.staticRoutes = nil, nil
}

// TestEnsureNetworkIndexesKeepsTails rebuilds the indexes of a network with a
// hairpin, and the rebuilt tail index and cell bounds must equal the prepared
// ones.
func TestEnsureNetworkIndexesKeepsTails(t *testing.T) {
	t.Parallel()
	lanes := []geometryLane{{to: Point{}}, {to: Point{X: 30}, control: &Point{X: 15, Y: 200}}, {to: Point{X: 30, Y: -500}}, {to: Point{X: 30, Y: -600}}}
	network := admitLargeMotionClasses(stationNetwork(Point{Y: -1000}, lanes, 2, Point{X: 60, Y: -60}, Point{Y: -120}))
	for index := range network.Nodes {
		if network.Nodes[index].ID == "origin-berth-02" {
			network.Nodes[index].Position = Point{X: -4960, Y: 4940}
		}
	}
	prepared, err := PrepareNetwork(network)
	if err != nil {
		t.Fatal(err)
	}
	if tail := prepared.tails["l1"].tail; math.Abs(tail-94.25) > 0.001 {
		t.Fatalf("prepared hairpin tail %v, want 94.25", tail)
	}
	stale := prepared.networkIndexes
	stale.graph, stale.tails = routeGraph{}, nil
	literal := &Simulation{networkIndexes: &stale}
	literal.ensureNetworkIndexes()
	if !reflect.DeepEqual(literal.tails, prepared.tails) {
		t.Errorf("rebuilt tails = %+v, want %+v", literal.tails, prepared.tails)
	}
	for id, want := range prepared.laneCells {
		got := literal.laneCells[id]
		if got.tail != want.tail || got.fromTail != want.fromTail || got.front != want.front {
			t.Errorf("lane %s: rebuilt tail %v, from tail %v, front %v, want %v, %v, %v", id, got.tail, got.fromTail, got.front, want.tail, want.fromTail, want.front)
		}
	}
}
