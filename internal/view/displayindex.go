package view

import (
	"math"

	"github.com/dotwaffle/podsim/internal/sim"
)

// networkIndexKey identifies the network of the display index. The session
// sends a new network only with a new epoch or a new project revision. A
// project apply and a rewind that restores a project also start a new
// generation. A restart that restores an older final save can repeat all
// three values with a different network, so the key also holds the server
// start ID. Thus the index cannot keep the data of an earlier network.
type networkIndexKey struct {
	epoch           string
	generation      uint64
	projectRevision uint64
	serverStart     string
}

// networkIndex holds the display data of one network. The game builds it
// once for each network, not on each frame. On a dense map, such as London,
// a scan of all nodes for each lane end or berth takes too long for a frame.
type networkIndex struct {
	// positions holds the position of each node by node ID.
	positions map[string]sim.Point
	// anchors holds the collapsed station anchors by station ID. See
	// collapsedStationAnchors.
	anchors map[string]sim.Point
	// berthSpacing holds the shortest distance in meters between two berth
	// nodes of a station, by station ID. It holds only the stations with two
	// or more berth nodes in the network.
	berthSpacing map[string]float64
	// lineLanes holds the IDs of the station lanes that continue a line.
	lineLanes map[string]bool
	// labelRanks holds the station label rank by station ID.
	labelRanks map[string]int
}

// newNetworkIndex builds the display index of network.
func newNetworkIndex(network sim.Network) *networkIndex {
	positions := make(map[string]sim.Point, len(network.Nodes))
	for _, node := range network.Nodes {
		positions[node.ID] = node.Position
	}
	return &networkIndex{
		positions:    positions,
		anchors:      collapsedStationAnchors(network),
		berthSpacing: berthSpacings(network.Stations, positions),
		lineLanes:    stationLineLanes(network),
		labelRanks:   stationLabelRanks(network),
	}
}

// berthSpacings returns the shortest distance in meters between two berth
// nodes of each station. positions holds the node positions by node ID. A
// berth node that is not in positions does not count. A station with fewer
// than two berth nodes in positions has no spacing.
func berthSpacings(stations []sim.Station, positions map[string]sim.Point) map[string]float64 {
	spacings := make(map[string]float64)
	for _, station := range stations {
		if len(station.Berths) < 2 {
			continue
		}
		shortest := math.Inf(1)
		for i, berth := range station.Berths {
			from, ok := positions[berth.Node]
			if !ok {
				continue
			}
			for _, other := range station.Berths[i+1:] {
				if to, found := positions[other.Node]; found {
					shortest = min(shortest, math.Hypot(from.X-to.X, from.Y-to.Y))
				}
			}
		}
		if !math.IsInf(shortest, 1) {
			spacings[station.ID] = shortest
		}
	}
	return spacings
}

// currentNetworkIndexKey returns the key of the network in the current
// state.
func (g *Game) currentNetworkIndexKey() networkIndexKey {
	return networkIndexKey{epoch: g.state.Epoch, generation: g.state.Generation, projectRevision: g.state.ProjectRevision, serverStart: g.state.ServerStart}
}

// displayIndex returns the display index of the current network. It builds
// the index again only when the epoch, the generation, the project
// revision or the server start ID changes.
func (g *Game) displayIndex() *networkIndex {
	key := g.currentNetworkIndexKey()
	if g.index == nil || g.indexKey != key {
		g.index = newNetworkIndex(g.network)
		g.indexKey = key
	}
	return g.index
}

// nodePosition returns the position of a node of the current network. An
// unknown node has the zero position.
func (g *Game) nodePosition(id string) sim.Point {
	return g.displayIndex().positions[id]
}
