package view

import (
	"cmp"
	"fmt"
	"image"
	"math"
	"slices"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"github.com/dotwaffle/podsim/internal/observe"
	"github.com/dotwaffle/podsim/internal/sim"
)

// mapHintLabel names the map input above the map. A click selects a pod or
// sets From, Shift+click sets To, Tab and Shift+Tab select the next and the
// previous pod, the wheel zooms, and a drag pans. It ends to the left of the
// zoom buttons.
var mapHintLabel = label{x: 136, y: 81, size: 11, value: "CLICK POD / CLICK FROM / SHIFT+CLICK TO / TAB POD / SCROLL ZOOM / DRAG PAN", color: muted}

func (g *Game) drawNetwork(screen *ebiten.Image, state sim.Snapshot) {
	g.label(screen, label{x: 44, y: 81, size: 12, value: "NETWORK", color: muted})
	g.label(screen, mapHintLabel)
	mapScreen, ok := screen.SubImage(g.layout.mapViewport).(*ebiten.Image)
	if !ok {
		return
	}
	markers := g.collapsedStationMarkers()
	style := g.currentNetworkStyle(markers)
	detailed := style.detailed
	var lanes []lanePath
	var lanesShift sim.Point
	if detailed {
		g.releaseNetworkBase()
		lanes = g.drawBaseNetwork(mapScreen, baseNetworkInput{style: style, area: g.layout.mapViewport})
	} else {
		lanes, lanesShift = g.drawCachedNetworkBase(mapScreen, style)
	}
	// Pods, rings, markers and route arrows are antialiased sprites on all
	// networks.
	scale := spriteScale{unit: g.layout.unit, markerRadius: style.markerRadius}
	markerKey := markerSprite(style.markerRadius, g.layout.unit)
	for _, station := range g.network.Stations {
		if marker, ok := markers[station.ID]; ok {
			g.sprites.draw(mapScreen, spriteDraw{center: marker, key: markerKey, scale: scale})
		}
	}
	selected, hasSelected := selectedVehicle(state, g.selected)
	collapsedStations := make(map[string]bool)
	stationMetrics := g.displayIndex().stationMonitor.Summarize(state)
	labelRanks := g.currentStationLabelRanks()
	var collapsedLabels []collapsedStationLabel
	var expanded []expandedStationText
	for index, station := range g.network.Stations {
		status := stationMetrics[index]
		if marker, ok := markers[station.ID]; ok {
			collapsedStations[station.ID] = true
			if len(station.Berths) > 0 {
				collapsedLabels = append(collapsedLabels, g.collapsedStationLabel(collapsedStationLabelInput{station: station, status: status, marker: marker, rank: labelRanks[station.ID]}))
			}
			continue
		}
		stationText := g.expandedStationText(expandedStationInput{station: station, status: status, state: state})
		for _, berth := range stationText.berths {
			g.sprites.draw(mapScreen, spriteDraw{center: berth.center, key: ringSprite(berthRingRadius*g.layout.unit, 2*g.layout.unit, berth.shade), scale: scale})
		}
		expanded = append(expanded, stationText)
	}
	// The station text keeps off the lanes at their place on the screen.
	if len(expanded) > 0 {
		lanes = movedLanePaths(lanes, lanesShift)
	}
	// Draw the station text after all berth rings, so that no ring covers
	// the text.
	placedText := g.placeExpandedStationText(expanded, lanes)
	g.drawStationText(mapScreen, placedText)
	podLabels := g.podMapLabels(state.Vehicles, collapsedStations)
	// The selected index can be past the pods of an empty or smaller state.
	var selectedPodLabel label
	if g.selected >= 0 && g.selected < len(podLabels) {
		selectedPodLabel = podLabels[g.selected]
	}
	g.drawCollapsedStationLabels(mapScreen, collapsedLabelsInput{
		labels: collapsedLabels, selected: selected,
		markers: markers, markerRadius: style.markerRadius, selectedPodLabel: selectedPodLabel,
	})
	// Draw the route after the station markers and labels, and before the
	// pods. A marker can sit on a line junction, and a label can cover a
	// line. The route must stay visible through them, and the pods stay on
	// top of the route.
	if hasSelected && selected.Pod.Activity != sim.Idle {
		shade := g.podPurpose(selected, state).color()
		// The route lines and arrows have one color, so the arrows can go
		// after all the lines. Ebiten can then draw the arrow sprites in one
		// batch.
		arrows := make([]arrow, 0, len(selected.Route))
		for _, lane := range selected.Route {
			geometry := g.laneGeometry(lane, detailed)
			geometry.draw(mapScreen, style.routeStroke(shade))
			arrows = append(arrows, arrow{tip: geometry.arrowTip, direction: geometry.arrowDirection, size: routeArrowSize, color: shade, unit: g.layout.unit})
		}
		for _, routeArrow := range arrows {
			if d, ok := routeArrow.spriteDraw(scale); ok {
				g.sprites.draw(mapScreen, d)
			}
		}
	}
	// A line joins each coupled pod to the pod ahead of it in its platoon.
	// The pods go on top of the lines.
	for _, link := range platoonLinks(state.Vehicles) {
		from, to := g.mapPoint(state.Vehicles[link.follower].Pod.Position), g.mapPoint(state.Vehicles[link.ahead].Pod.Position)
		vector.StrokeLine(mapScreen, float32(from.X), float32(from.Y), float32(to.X), float32(to.Y), float32(platoonLinkWidth*g.layout.unit), rgb(foreground), style.antialias)
	}
	for i, v := range state.Vehicles {
		parkedInCluster := v.Pod.Activity == sim.Idle && collapsedStations[v.Pod.StationID]
		if parkedInCluster && i != g.selected {
			continue
		}
		p := g.mapPoint(v.Pod.Position)
		purpose := g.podPurpose(v, state)
		shade := purpose.color()
		if v.Pod.WaitReason != sim.NoWait {
			g.sprites.draw(mapScreen, spriteDraw{center: p, key: ringSprite(11*g.layout.unit, 2*g.layout.unit, amber), scale: scale})
		}
		if i == g.selected {
			g.sprites.draw(mapScreen, spriteDraw{center: p, key: ringSprite(9*g.layout.unit, 1.5*g.layout.unit, foreground), scale: scale})
		}
		g.sprites.draw(mapScreen, spriteDraw{center: p, key: podMarkSprite(purpose, g.layout.unit), scale: scale})
		if podLabel := podLabels[i]; podLabel.value != "" {
			podLabel.color = shade
			g.label(mapScreen, podLabel)
		}
	}
	// Without a network, the map has no scale.
	if bar, ok := newScaleBar(g.mapScale, g.layout.unit); ok && len(g.network.Nodes) > 0 {
		vector.StrokeLine(screen, float32(g.layout.x(48)), float32(g.layout.bottom(508)), float32(g.layout.x(48+bar.length)), float32(g.layout.bottom(508)), float32(2*g.layout.unit), rgb(muted), style.antialias)
		g.label(screen, label{x: 64 + bar.length, y: 499, size: 12, value: bar.label, color: muted})
	}
	g.drawPodLegend(screen, scale)
}

// selectedVehicle returns the vehicle at index in state. It returns false
// when state has no vehicle at index, for example before the first state
// frame.
func selectedVehicle(state sim.Snapshot, index int) (sim.Vehicle, bool) {
	if index < 0 || index >= len(state.Vehicles) {
		return sim.Vehicle{}, false
	}
	return state.Vehicles[index], true
}

// showStationBerths reports whether the berths of a station separate on the
// screen. A station with fewer than two berth nodes in the network always
// shows its berths.
func (g *Game) showStationBerths(station sim.Station) bool {
	spacing, ok := g.displayIndex().berthSpacing[station.ID]
	return !ok || spacing*g.mapScale >= 34*g.layout.unit
}

// overviewNameRunes is the largest number of runes of a station name in an
// overview label. A longer name ends with an ellipsis. The limit keeps the
// names of the London Parking facilities complete.
const overviewNameRunes = 20

// collapsedStationLabel is the overview label of a collapsed station.
type collapsedStationLabel struct {
	stationID string
	// rank is the place of the station in the label order. See
	// stationLabelRanks and selectCollapsedStationLabels.
	rank    int
	primary label
	// collisionValue is the primary text with all berths occupied. The label
	// bounds use it, so they do not change when a pod arrives or departs.
	collisionValue string
	// secondary is the queue line below the primary text. It is empty when
	// the station has no queue.
	secondary string
}

// collapsedStationLabelInput holds the station state of an overview label.
type collapsedStationLabelInput struct {
	station sim.Station
	status  observe.StationMetrics
	// marker is the screen position of the collapsed station marker.
	marker sim.Point
	rank   int
}

// collapsedStationLabel returns the overview label of a collapsed station.
// The label starts to the right of the marker and above it.
func (g *Game) collapsedStationLabel(input collapsedStationLabelInput) collapsedStationLabel {
	name := shortText(strings.TrimPrefix(input.station.Name, "Station "), overviewNameRunes)
	berths := len(input.station.Berths)
	return collapsedStationLabel{
		stationID: input.station.ID,
		rank:      input.rank,
		primary: label{
			x: input.marker.X + 16*g.layout.unit, y: input.marker.Y - 14*g.layout.unit,
			size: 10, value: fmt.Sprintf("%s  %d/%d", name, input.status.Occupied, berths), color: foreground, mapLabel: true,
		},
		collisionValue: fmt.Sprintf("%s  %d/%d", name, berths, berths),
		secondary:      stationQueueAlert(input.status),
	}
}

// secondaryLabel returns the queue line of the overview label. deviceScale
// is the number of screen pixels in one CSS pixel. The line spacing is in
// CSS pixels, as the label size is.
func (candidate collapsedStationLabel) secondaryLabel(deviceScale float64) label {
	return label{x: candidate.primary.x, y: candidate.primary.y + 16*deviceScale, size: 9, value: candidate.secondary, color: amber, mapLabel: true}
}

// boundedStationLabel holds the screen areas of an overview label and of its
// station marker.
type boundedStationLabel struct {
	stationID string
	rank      int
	// queued is true when the label has a queue line.
	queued bool
	bounds image.Rectangle
	// marker is the screen area of the station marker with its outline.
	marker image.Rectangle
}

// collapsedLabelsInput holds the overview labels and the map items that the
// labels must not cover.
type collapsedLabelsInput struct {
	labels   []collapsedStationLabel
	selected sim.Vehicle
	// markers holds the screen position of each collapsed station marker by
	// station ID. markerRadius is the marker radius in screen pixels.
	markers      map[string]sim.Point
	markerRadius float64
	// selectedPodLabel is the map label of the selected pod. Its value is
	// empty when the pod has no map label.
	selectedPodLabel label
}

// drawCollapsedStationLabels draws the overview labels that
// visibleCollapsedStationLabels selects.
func (g *Game) drawCollapsedStationLabels(screen *ebiten.Image, input collapsedLabelsInput) {
	_, visible := g.visibleCollapsedStationLabels(input)
	for index, candidate := range input.labels {
		if !visible[index] {
			continue
		}
		g.label(screen, candidate.primary)
		if candidate.secondary != "" {
			g.label(screen, candidate.secondaryLabel(g.layout.deviceScale))
		}
	}
}

// visibleCollapsedStationLabels returns the screen areas of the overview
// labels and reports which labels show. The labels of the From and To
// stations and of the stations of the selected pod always show. On a dense
// map, the other labels do not cover the label of the selected pod. See
// selectCollapsedStationLabels.
func (g *Game) visibleCollapsedStationLabels(input collapsedLabelsInput) ([]boundedStationLabel, []bool) {
	selected := input.selected
	preferred := map[string]bool{g.origin: true, g.destination: true, selected.Pod.StationID: true, selected.RelocatingTo: true}
	addRiderStations(preferred, selected)
	delete(preferred, "")
	selection := collapsedLabelSelection{labels: g.boundedStationLabels(input), preferred: preferred, dense: len(g.network.Stations) > 30}
	if selection.dense && input.selectedPodLabel.value != "" {
		selection.occupied = append(selection.occupied, g.labelBounds(input.selectedPodLabel))
	}
	return selection.labels, selectCollapsedStationLabels(selection)
}

// addRiderStations adds the leg origin and the destination of each rider
// of a pod to stations.
func addRiderStations(stations map[string]bool, vehicle sim.Vehicle) {
	for _, rider := range vehicle.Riders {
		stations[cmp.Or(rider.LegFrom, rider.From)] = true
		stations[rider.To] = true
	}
}

// boundedStationLabels returns the screen areas of the overview labels and of
// their station markers.
func (g *Game) boundedStationLabels(input collapsedLabelsInput) []boundedStationLabel {
	// The outline is centered on the marker radius.
	markerRadius := input.markerRadius + markerOutlineWidth*g.layout.unit/2
	bounded := make([]boundedStationLabel, len(input.labels))
	for index, candidate := range input.labels {
		marker := input.markers[candidate.stationID]
		bounded[index] = boundedStationLabel{
			stationID: candidate.stationID,
			rank:      candidate.rank,
			queued:    candidate.secondary != "",
			bounds:    g.collapsedStationLabelBounds(candidate),
			marker: image.Rect(
				int(math.Floor(marker.X-markerRadius)), int(math.Floor(marker.Y-markerRadius)),
				int(math.Ceil(marker.X+markerRadius)), int(math.Ceil(marker.Y+markerRadius)),
			),
		}
	}
	return bounded
}

// labelBounds returns the screen area of the text of a map label.
func (g *Game) labelBounds(value label) image.Rectangle {
	width, height := g.measureLabel(value)
	return image.Rect(
		int(math.Floor(value.x)), int(math.Floor(value.y)),
		int(math.Ceil(value.x+width)), int(math.Ceil(value.y+height)),
	)
}

// collapsedStationLabelBounds returns the screen area of an overview label
// with a padding of 3 units. The area holds only the lines that the map
// draws. The primary line has the width of the text with all berths occupied.
func (g *Game) collapsedStationLabelBounds(candidate collapsedStationLabel) image.Rectangle {
	primary := candidate.primary
	primary.value = candidate.collisionValue
	bounds := g.labelBounds(primary)
	if candidate.secondary != "" {
		bounds = bounds.Union(g.labelBounds(candidate.secondaryLabel(g.layout.deviceScale)))
	}
	return bounds.Inset(-int(math.Ceil(3 * g.layout.unit)))
}

// collapsedLabelSelection holds the overview labels for
// selectCollapsedStationLabels.
type collapsedLabelSelection struct {
	labels []boundedStationLabel
	// preferred holds the IDs of the stations whose labels always show.
	preferred map[string]bool
	// occupied holds the screen areas that no label other than a preferred
	// label can cover, such as the label of the selected pod.
	occupied []image.Rectangle
	// dense is true on a map with more than 30 stations. On other maps, all
	// labels show.
	dense bool
}

// selectCollapsedStationLabels reports which overview labels show. On a
// dense map, it places the preferred labels first and then the other labels.
// In each of the two groups, the labels with a queue line come first, and
// then the rank order applies. A label that is not preferred shows only when
// it does not cover an occupied area, an earlier label or the marker of an
// earlier station. Its own marker also must not be under an earlier label.
//
// Thus, when neither label has a queue line, the label of a large station
// can cover the marker of a smaller station, but not the opposite. At Fit,
// the markers of a dense map are too close for most labels to stay clear of
// all of them. The queue line is an alert, and it makes the label taller.
// Because the labels with a queue line come first, a label without a queue
// line cannot hide them.
func selectCollapsedStationLabels(selection collapsedLabelSelection) []bool {
	labels := selection.labels
	visible := make([]bool, len(labels))
	if !selection.dense {
		for index := range visible {
			visible[index] = true
		}
		return visible
	}
	order := make([]int, len(labels))
	for index := range order {
		order[index] = index
	}
	slices.SortStableFunc(order, func(a, b int) int {
		return cmp.Or(trueFirst(labels[a].queued, labels[b].queued), cmp.Compare(labels[a].rank, labels[b].rank))
	})
	occupied := slices.Clone(selection.occupied)
	var placed []image.Rectangle
	for _, priority := range []bool{true, false} {
		for _, index := range order {
			candidate := labels[index]
			if selection.preferred[candidate.stationID] != priority {
				continue
			}
			blocked := slices.ContainsFunc(occupied, candidate.bounds.Overlaps) ||
				slices.ContainsFunc(placed, candidate.marker.Overlaps)
			occupied = append(occupied, candidate.marker)
			if blocked && !priority {
				continue
			}
			visible[index] = true
			occupied = append(occupied, candidate.bounds)
			placed = append(placed, candidate.bounds)
		}
	}
	return visible
}

// podMapLabels returns the map label of each pod by fleet index, without a
// color. The label of a pod without a map label has an empty value. Other
// than the selected pod, a pod that is parked in a collapsed station has no
// map label. collapsedStations is true for the ID of each collapsed station.
// See showPodMapLabel for the zoom rule.
func (g *Game) podMapLabels(vehicles []sim.Vehicle, collapsedStations map[string]bool) []label {
	labels := make([]label, len(vehicles))
	for index, vehicle := range vehicles {
		parkedInCluster := vehicle.Pod.Activity == sim.Idle && collapsedStations[vehicle.Pod.StationID]
		if (parkedInCluster && index != g.selected) || !g.showPodMapLabel(index) {
			continue
		}
		p := g.mapPoint(vehicle.Pod.Position)
		tag := label{x: p.X + podLabelLeft*g.layout.unit, y: p.Y + podLabelTop*g.layout.unit, size: 11, value: fleetPodLabel(index), mapLabel: true}
		if code := podDestinationCode(vehicle); code != "" {
			tag.value += "\n" + code
			tag.lineSpacing = 1.2 * g.layout.mapLabelSize(tag.size)
		}
		labels[index] = tag
	}
	return labels
}

func (g *Game) showPodMapLabel(index int) bool {
	return index == g.selected || len(g.network.Stations) <= 30 || g.mapScale >= 4*g.camera.minScale
}

func (g *Game) podHiddenInCluster(vehicle sim.Vehicle, index int) bool {
	if index == g.selected || vehicle.Pod.Activity != sim.Idle {
		return false
	}
	station, ok := g.network.Station(vehicle.Pod.StationID)
	return ok && !g.showStationBerths(station)
}

type laneGeometry struct {
	points [33]sim.Point
	count  int
	// arrowTip is the point on the drawn lane at 61 percent of its screen
	// length. arrowDirection points in the direction of travel at the tip.
	arrowTip, arrowDirection sim.Point
}

type laneStroke struct {
	width     float32
	color     uint32
	antialias bool
}

// screenLength returns the length of the lane on the screen in pixels.
func (geometry laneGeometry) screenLength() float64 {
	length := 0.0
	for i := 1; i < geometry.count; i++ {
		from, to := geometry.points[i-1], geometry.points[i]
		length += math.Hypot(to.X-from.X, to.Y-from.Y)
	}
	return length
}

func (geometry laneGeometry) draw(screen *ebiten.Image, stroke laneStroke) {
	for i := 1; i < geometry.count; i++ {
		from, to := geometry.points[i-1], geometry.points[i]
		vector.StrokeLine(screen, float32(from.X), float32(from.Y), float32(to.X), float32(to.Y), stroke.width, rgb(stroke.color), stroke.antialias)
	}
}

func (g *Game) laneGeometry(lane sim.Lane, detailed bool) laneGeometry {
	from, to := g.nodePosition(lane.From), g.nodePosition(lane.To)
	position := func(t float64) sim.Point {
		if lane.Control == nil {
			return sim.Point{X: from.X + (to.X-from.X)*t, Y: from.Y + (to.Y-from.Y)*t}
		}
		u := 1 - t
		return sim.Point{X: u*u*from.X + 2*u*t*lane.Control.X + t*t*to.X, Y: u*u*from.Y + 2*u*t*lane.Control.Y + t*t*to.Y}
	}
	segments := 1
	if lane.Control != nil {
		segments = 32
		if !detailed {
			extent := math.Hypot(lane.Control.X-from.X, lane.Control.Y-from.Y) + math.Hypot(to.X-lane.Control.X, to.Y-lane.Control.Y)
			segments = min(32, max(4, int(math.Ceil(extent*g.mapScale/8))))
		}
	}
	length := 0.0
	if detailed {
		length = g.network.Length(lane)
	}
	geometry := laneGeometry{count: segments + 1}
	for i := 0; i <= segments; i++ {
		point := position(float64(i) / float64(segments))
		if detailed {
			point = g.network.Position(lane, length*float64(i)/float64(segments))
		}
		geometry.points[i] = g.mapPoint(point)
	}
	geometry.arrowTip, geometry.arrowDirection = geometry.along(.61)
	return geometry
}

// along returns the point at a fraction of the screen length of the lane,
// and the direction of the lane segment at that point. On a lane with no
// screen length, the direction is zero.
func (geometry laneGeometry) along(fraction float64) (point, direction sim.Point) {
	remaining := fraction * geometry.screenLength()
	point = geometry.points[0]
	for i := 1; i < geometry.count; i++ {
		from, to := geometry.points[i-1], geometry.points[i]
		direction = sim.Point{X: to.X - from.X, Y: to.Y - from.Y}
		length := math.Hypot(direction.X, direction.Y)
		if length > 0 && remaining <= length {
			t := remaining / length
			return sim.Point{X: from.X + direction.X*t, Y: from.Y + direction.Y*t}, direction
		}
		remaining -= length
		point = to
	}
	return point, direction
}

// arrowSize is the size of a direction arrow in display units. Each of the
// two legs goes back from the tip by length and to one side by halfWidth.
type arrowSize struct {
	length, halfWidth float64
}

// arrowLineWidth is the width in display units of the two arrow legs.
const arrowLineWidth = 1.5

var (
	// routeArrowSize is the arrow size on the route of the selected pod.
	routeArrowSize = arrowSize{length: 7, halfWidth: 4}
	// laneArrowSize is the arrow size on the lanes of the network base
	// layer. The arrow stays on a lane that is 5 units wide.
	laneArrowSize = arrowSize{length: 4, halfWidth: 2}
)

type arrow struct {
	tip sim.Point
	// direction is a vector in the direction of the arrow. Its length does
	// not change the arrow.
	direction sim.Point
	size      arrowSize
	color     uint32
	antialias bool
	unit      float64
}

// legEnds returns the free ends of the two arrow legs. The other end of each
// leg is at the tip. ok is false if the arrow has no direction.
func (a arrow) legEnds() (ends [2]sim.Point, ok bool) {
	length := math.Hypot(a.direction.X, a.direction.Y)
	if length < 0.001 {
		return ends, false
	}
	ux, uy := a.direction.X/length, a.direction.Y/length
	back, side := a.size.length*a.unit, a.size.halfWidth*a.unit
	for i, sign := range []float64{-1, 1} {
		ends[i] = sim.Point{X: a.tip.X - ux*back + uy*side*sign, Y: a.tip.Y - uy*back - ux*side*sign}
	}
	return ends, true
}

func drawArrow(screen *ebiten.Image, a arrow) {
	ends, ok := a.legEnds()
	if !ok {
		return
	}
	for _, end := range ends {
		vector.StrokeLine(screen, float32(a.tip.X), float32(a.tip.Y), float32(end.X), float32(end.Y), float32(arrowLineWidth*a.unit), rgb(a.color), a.antialias)
	}
}

// spriteDraw returns the sprite of a at scale, turned to the direction of
// a. ok is false if a has no direction.
func (a arrow) spriteDraw(scale spriteScale) (d spriteDraw, ok bool) {
	if _, hasDirection := a.legEnds(); !hasDirection {
		return d, false
	}
	return spriteDraw{center: a.tip, key: arrowSprite(a), scale: scale, direction: a.direction}, true
}
