package sim

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
	p.retention = newLaneRetention(p.network, tails)
	p.maneuvers, _ = newManeuverIndex(p.network, indexNodePositions(p.network.Nodes))
	p.depots, _ = newDepotIndex(p.network, indexNodePositions(p.network.Nodes))
	p.berthKeys = indexBerthKeys(p.network)
	p.lateral = newDepotLateral(p.network, p.depots, tails, p.berthKeys)
	p.laneCells = indexLaneCells(laneCellsIndexInput{network: p.network, geometry: p.geometry, conflicts: p.junctionConflicts, berths: p.berthResources, retention: p.retention, maneuvers: p.maneuvers, depots: p.depots})
	p.resourceLanes = indexResourceLanes(p.network, p.laneCells)
	p.platoonData = newPlatoonIndexData(p.network)
	s.networkIndexes = &p
	s.lengths = nil
	s.routes = nil
	s.routeOrder = nil
	s.pickupBounds = nil
	s.routeWork = nil
	// The blocked set holds lane indexes of the old network, and the
	// no-candidate memo node indexes.
	s.blocked = blockedSet{}
	s.emergencyMisses = nil
	s.pickupMisses = nil
	s.accessRoutes = nil
	s.staticConnected, s.staticRoutes = nil, nil
}
