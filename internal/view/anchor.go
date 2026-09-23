package view

import "github.com/dotwaffle/podsim/internal/sim"

// anchorCacheKey identifies the network of the cached station anchors. The
// session sends new network geometry only with a new epoch or a new project
// revision.
type anchorCacheKey struct {
	epoch           string
	projectRevision uint64
}

// stationAnchors returns the collapsed station anchors of the current
// network. It builds them again only when the epoch or the project revision
// changes, not on each frame.
func (g *Game) stationAnchors() map[string]sim.Point {
	key := anchorCacheKey{epoch: g.state.Epoch, projectRevision: g.state.ProjectRevision}
	if g.anchors == nil || g.anchorsKey != key {
		g.anchors = collapsedStationAnchors(g.network)
		g.anchorsKey = key
	}
	return g.anchors
}

// collapsedStationMarkers returns the screen position of each collapsed
// station marker by station ID. A station is collapsed when its berths do not
// separate on the screen.
func (g *Game) collapsedStationMarkers() map[string]sim.Point {
	anchors := g.stationAnchors()
	markers := make(map[string]sim.Point)
	for _, station := range g.network.Stations {
		anchor, ok := anchors[station.ID]
		if !ok || g.showStationBerths(station) {
			continue
		}
		markers[station.ID] = g.mapPoint(anchor)
	}
	return markers
}

// collapsedStationAnchors returns the world position of each collapsed
// station marker by station ID.
//
// On a network with more than detailedLanes lanes, a passenger station with
// approach lanes uses the mean start of these lanes. In London, these are
// the arrival portals at the junction of the station. The berths are about
// 250 to 450 m away on the station siding.
//
// All other stations use the berth centroid. On a detailed network, such as
// the example, an approach lane can start at a node far from the station.
// In London, the approach lanes of a Parking facility start at the portals
// of its gateway station. A junction anchor would put the Parking marker on
// the gateway marker. A station has no anchor when it has no junction anchor
// and no known berth nodes.
func collapsedStationAnchors(network sim.Network) map[string]sim.Point {
	positions := make(map[string]sim.Point, len(network.Nodes))
	for _, node := range network.Nodes {
		positions[node.ID] = node.Position
	}
	approaches := make(map[string]pointMean)
	if len(network.Lanes) > detailedLanes {
		for _, lane := range network.Lanes {
			if lane.StationRole != sim.StationApproachRole {
				continue
			}
			approach := approaches[lane.StationID]
			approach.add(positions, lane.From)
			approaches[lane.StationID] = approach
		}
	}
	anchors := make(map[string]sim.Point, len(network.Stations))
	for _, station := range network.Stations {
		approach := approaches[station.ID]
		if anchor, ok := approach.mean(); ok && !station.ParkingOnly {
			anchors[station.ID] = anchor
			continue
		}
		var berths pointMean
		for _, berth := range station.Berths {
			berths.add(positions, berth.Node)
		}
		if anchor, ok := berths.mean(); ok {
			anchors[station.ID] = anchor
		}
	}
	return anchors
}

// pointMean adds node positions and returns their mean.
type pointMean struct {
	sum   sim.Point
	count int
}

// add adds the position of the node. It ignores an unknown node.
func (m *pointMean) add(positions map[string]sim.Point, nodeID string) {
	position, ok := positions[nodeID]
	if !ok {
		return
	}
	m.sum.X += position.X
	m.sum.Y += position.Y
	m.count++
}

// mean returns the mean position. It returns false when no node was added.
func (m *pointMean) mean() (sim.Point, bool) {
	if m.count == 0 {
		return sim.Point{}, false
	}
	return sim.Point{X: m.sum.X / float64(m.count), Y: m.sum.Y / float64(m.count)}, true
}
