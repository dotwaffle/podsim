package view

import (
	"math"
	"strconv"

	"github.com/dotwaffle/podsim/internal/sim"
)

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

const (
	// laneArrowGap is the shortest distance in display units between the
	// tips of two lane arrows in about the same direction. Closer arrows
	// overlap.
	laneArrowGap = 6
	// sameDirection is the cosine of 45 degrees. Two arrows that point less
	// than 45 degrees apart are in about the same direction.
	sameDirection = math.Sqrt2 / 2
)

// pixels returns the size in screen pixels at the map scale in pixels for
// each meter. The unit is the number of pixels in one display unit.
func (size mapSize) pixels(scale, unit float64) float64 {
	return min(size.maximum*unit, max(size.minimum*unit, size.meters*scale))
}

// scaleBarMaximum is the maximum length of the map scale bar in display
// units.
const scaleBarMaximum = 80

// scaleBar is the map scale bar at one map scale.
type scaleBar struct {
	// meters is the distance that the bar shows.
	meters float64
	// length is the length of the bar in display units.
	length float64
	// label is the distance as text, for example "500 m" or "2 km".
	label string
}

// newScaleBar returns the scale bar for the map scale in screen pixels for
// each meter. The unit is the number of pixels in one display unit. The bar
// shows the largest distance of 1, 2 or 5 times a power of ten that fits in
// scaleBarMaximum units. The label uses m below 1000 m and km from 1000 m.
// It returns false when the scale or the unit is not a positive finite
// number, or when the distance that fits is too small or too large for a
// float64.
func newScaleBar(scale, unit float64) (scaleBar, bool) {
	if !(scale > 0 && unit > 0) || math.IsInf(scale, 0) || math.IsInf(unit, 0) {
		return scaleBar{}, false
	}
	fit := scaleBarMaximum * unit / scale
	// A very small or large ratio of unit to scale gives a fit that is not
	// a positive finite number.
	if !(fit > 0) || math.IsInf(fit, 0) {
		return scaleBar{}, false
	}
	// The tolerance keeps a distance that fits exactly, such as 1000 m, when
	// the logarithm rounds down.
	limit := fit * (1 + 1e-9)
	power := math.Floor(math.Log10(fit))
	meters := 0.0
	for exponent := power - 1; exponent <= power+1; exponent++ {
		for _, step := range []float64{1, 2, 5} {
			if value := step * math.Pow(10, exponent); value <= limit {
				meters = value
			}
		}
	}
	text := strconv.FormatFloat(meters, 'f', -1, 64) + " m"
	if meters >= 1000 {
		text = strconv.FormatFloat(meters/1000, 'f', -1, 64) + " km"
	}
	return scaleBar{meters: meters, length: meters * scale / unit, label: text}, true
}

// networkStyle holds the screen sizes of the network base layer and of the
// collapsed station markers at one map scale.
type networkStyle struct {
	// detailed is true for a network with detailedLanes lanes or fewer.
	detailed bool
	// unit is the number of screen pixels in one display unit.
	unit         float64
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
		unit:               unit,
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

// currentLineLanes returns the station line lanes of the current network
// from the display index.
func (g *Game) currentLineLanes() map[string]bool {
	return g.displayIndex().lineLanes
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
	if style.dimLane(lane) {
		return laneStroke{width: float32(style.collapsedLaneWidth), color: collapsedStationTrack, antialias: style.detailed}
	}
	return laneStroke{width: float32(style.laneWidth), color: track, antialias: style.detailed}
}

// laneArrow returns the direction arrow of a lane in the network base layer.
// ok is false if the lane is too short on the screen for an arrow.
func (style networkStyle) laneArrow(lane sim.Lane, geometry laneGeometry) (a arrow, ok bool) {
	if !style.showArrow(geometry) {
		return arrow{}, false
	}
	return arrow{
		tip:       geometry.arrowTip,
		direction: geometry.arrowDirection,
		size:      laneArrowSize,
		color:     style.laneArrowColor(lane),
		antialias: style.detailed,
		unit:      style.unit,
	}, true
}

// laneArrowColor returns the color of the direction arrow on a lane in the
// network base layer. The arrow is lighter than its lane, so it shows on the
// lane. A dim station lane has a dim arrow.
func (style networkStyle) laneArrowColor(lane sim.Lane) uint32 {
	if style.dimLane(lane) {
		return track
	}
	return muted
}

// dimLane reports whether a lane is in a collapsed station and does not
// continue a line. Such a lane is thinner and dimmer than the other lanes.
func (style networkStyle) dimLane(lane sim.Lane) bool {
	_, collapsed := style.markers[lane.StationID]
	return collapsed && !style.lineLanes[lane.ID]
}

// showArrow reports whether the lane is long enough on the screen for a
// direction arrow.
func (style networkStyle) showArrow(geometry laneGeometry) bool {
	return geometry.screenLength() >= style.shortestArrowLane
}

// spacedArrows returns the arrows in their order, without each arrow that
// crowds an arrow before it. An arrow crowds another arrow if their tips are
// less than laneArrowGap units apart and they point in about the same
// direction. Arrows in other directions stay, so each direction of a two-way
// link keeps its arrow.
func (style networkStyle) spacedArrows(arrows []arrow) []arrow {
	grid := arrowGrid{gap: laneArrowGap * style.unit, cells: make(map[[2]int][]arrow)}
	var spaced []arrow
	for _, a := range arrows {
		if grid.crowded(a) {
			continue
		}
		grid.add(a)
		spaced = append(spaced, a)
	}
	return spaced
}

// arrowGrid holds arrows in square cells one gap wide, by the cell of their
// tip. A tip less than one gap from another tip is in the same cell or in one
// of the eight cells around it.
type arrowGrid struct {
	gap   float64
	cells map[[2]int][]arrow
}

func (grid arrowGrid) cell(point sim.Point) [2]int {
	return [2]int{int(math.Floor(point.X / grid.gap)), int(math.Floor(point.Y / grid.gap))}
}

func (grid arrowGrid) add(a arrow) {
	cell := grid.cell(a.tip)
	grid.cells[cell] = append(grid.cells[cell], a)
}

// crowded reports whether the arrow crowds an arrow in the grid.
func (grid arrowGrid) crowded(a arrow) bool {
	home := grid.cell(a.tip)
	for x := home[0] - 1; x <= home[0]+1; x++ {
		for y := home[1] - 1; y <= home[1]+1; y++ {
			for _, other := range grid.cells[[2]int{x, y}] {
				if a.crowds(other, grid.gap) {
					return true
				}
			}
		}
	}
	return false
}

// crowds reports whether the tips of the two arrows are less than gap pixels
// apart and the arrows point in about the same direction.
func (a arrow) crowds(other arrow, gap float64) bool {
	if math.Hypot(a.tip.X-other.tip.X, a.tip.Y-other.tip.Y) >= gap {
		return false
	}
	dot := a.direction.X*other.direction.X + a.direction.Y*other.direction.Y
	return dot > sameDirection*math.Hypot(a.direction.X, a.direction.Y)*math.Hypot(other.direction.X, other.direction.Y)
}
