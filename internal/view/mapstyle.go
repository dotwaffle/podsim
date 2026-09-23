package view

import "github.com/dotwaffle/podsim/internal/sim"

// collapsedStationTrack is the lane color of a collapsed station on a dense
// map. It is between track and panel, so the lines of the network stay
// clear of the station lanes.
const collapsedStationTrack = 0x243545

// mapSize is a size in meters on the map. On the screen, the size stays
// between a minimum and a maximum number of display units.
type mapSize struct {
	meters, minimum, maximum float64
}

// Dense maps scale the station markers and the lanes with the zoom.
var (
	denseMarkerRadius = mapSize{meters: 150, minimum: 3, maximum: 10}
	denseLaneWidth    = mapSize{meters: 30, minimum: 2, maximum: 5}
)

// pixels returns the size in screen pixels at the map scale in pixels for
// each meter. The unit is the number of pixels in one display unit.
func (size mapSize) pixels(scale, unit float64) float64 {
	return min(size.maximum*unit, max(size.minimum*unit, size.meters*scale))
}

// networkStyle holds the screen sizes of the network base layer and of the
// collapsed station markers at one map scale.
type networkStyle struct {
	// detailed is true for a network with detailedLanes lanes or fewer.
	detailed     bool
	markerRadius float64
	laneWidth    float64
	// collapsedLaneWidth applies to the lanes of the collapsed stations.
	collapsedLaneWidth float64
	// markers holds the collapsed station markers by station ID. The lanes
	// of these stations use collapsedLaneWidth and collapsedStationTrack.
	markers map[string]sim.Point
	// lineLanes holds the IDs of the station lanes that continue a line.
	// These lanes keep the line style.
	lineLanes map[string]bool
	// shortestArrowLane is the shortest screen length of a lane with a
	// direction arrow.
	shortestArrowLane float64
	nodeDots          bool
}

// networkStyleInput holds the map state that sets the network style.
type networkStyleInput struct {
	network sim.Network
	// markers holds the collapsed station markers by station ID.
	markers map[string]sim.Point
	// lineLanes holds the IDs of the station lanes that continue a line.
	lineLanes map[string]bool
	// scale is the map scale in screen pixels for each meter.
	scale float64
	// unit is the number of screen pixels in one display unit.
	unit float64
}

// newNetworkStyle returns the network style for the map state.
//
// A detailed network keeps fixed sizes at all zoom levels. A dense network
// scales its markers and lanes with the zoom. It draws no direction arrow on
// a lane shorter than 24 units on the screen. It draws the lanes of a
// collapsed station thinner and dimmer, but not the lanes that continue a
// line. It draws node dots only from the berth expansion scale.
func newNetworkStyle(input networkStyleInput) networkStyle {
	unit := input.unit
	style := networkStyle{
		detailed:           len(input.network.Lanes) <= detailedLanes,
		markerRadius:       10 * unit,
		laneWidth:          5 * unit,
		collapsedLaneWidth: 5 * unit,
		nodeDots:           true,
	}
	if style.detailed {
		return style
	}
	style.markerRadius = denseMarkerRadius.pixels(input.scale, unit)
	style.laneWidth = denseLaneWidth.pixels(input.scale, unit)
	style.collapsedLaneWidth = max(unit, style.laneWidth/2)
	style.markers = input.markers
	style.lineLanes = input.lineLanes
	style.shortestArrowLane = 24 * unit
	style.nodeDots = berthsExpanded(input.network.Stations, input.markers)
	return style
}

// berthsExpanded reports whether the map is at or past the berth expansion
// scale, where the first station that can collapse shows its berths. The
// markers hold the collapsed stations. A station with fewer than two berths
// cannot collapse. Without a station that can collapse, the map is always
// expanded.
func berthsExpanded(stations []sim.Station, markers map[string]sim.Point) bool {
	canCollapse := false
	for _, station := range stations {
		if len(station.Berths) < 2 {
			continue
		}
		if _, collapsed := markers[station.ID]; !collapsed {
			return true
		}
		canCollapse = true
	}
	return !canCollapse
}

// currentLineLanes returns the station line lanes of the current network. It
// builds them again only when the epoch or the project revision changes, not
// on each frame.
func (g *Game) currentLineLanes() map[string]bool {
	key := anchorCacheKey{epoch: g.state.Epoch, projectRevision: g.state.ProjectRevision}
	if g.lineLanes == nil || g.lineLanesKey != key {
		g.lineLanes = stationLineLanes(g.network)
		g.lineLanesKey = key
	}
	return g.lineLanes
}

// stationLineLanes returns the IDs of the station lanes that continue a line.
// Such a lane starts where a lane without a station ends, and it ends where
// a lane without a station starts. In a generated ring, the through lane of
// each station is part of the ring.
func stationLineLanes(network sim.Network) map[string]bool {
	lineEnds := make(map[string]bool)
	lineStarts := make(map[string]bool)
	for _, lane := range network.Lanes {
		if lane.StationID == "" {
			lineEnds[lane.To] = true
			lineStarts[lane.From] = true
		}
	}
	lineLanes := make(map[string]bool)
	for _, lane := range network.Lanes {
		if lane.StationID != "" && lineEnds[lane.From] && lineStarts[lane.To] {
			lineLanes[lane.ID] = true
		}
	}
	return lineLanes
}

// laneStroke returns the stroke of a lane in the network base layer.
func (style networkStyle) laneStroke(lane sim.Lane) laneStroke {
	if _, collapsed := style.markers[lane.StationID]; collapsed && !style.lineLanes[lane.ID] {
		return laneStroke{width: float32(style.collapsedLaneWidth), color: collapsedStationTrack, antialias: style.detailed}
	}
	return laneStroke{width: float32(style.laneWidth), color: track, antialias: style.detailed}
}

// showArrow reports whether the lane is long enough on the screen for a
// direction arrow.
func (style networkStyle) showArrow(geometry laneGeometry) bool {
	return geometry.screenLength() >= style.shortestArrowLane
}
