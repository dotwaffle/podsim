package view

import (
	"cmp"
	"fmt"
	"image"
	"image/color"
	"math"
	"slices"
	"strconv"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"github.com/dotwaffle/podsim/internal/observe"
	"github.com/dotwaffle/podsim/internal/sim"
)

const (
	// stationTextGap is the space in display units between a text block and
	// the berth rings that it names.
	stationTextGap = 2
	// stationTextAwayCost is the cost in display units of a text place at
	// right angles to the siding direction. See stationTextPlacement.
	stationTextAwayCost = 64
	// stationTextPadding is the space in display units around a text block
	// that other text blocks do not cover. The line backing extends this far
	// to the left and right of the text.
	stationTextPadding = 2
	// stationTextBackingAlpha is the alpha of the panel-colored backing
	// behind station text, about 80%.
	stationTextBackingAlpha = 204
)

// stationTextBlock holds the map text lines that name a berth or a station.
// The x and y of each line are offsets in screen pixels from the top left
// of the text.
type stationTextBlock struct {
	lines []label
}

// berthText holds the berth ring and the text of one berth of an expanded
// station.
type berthText struct {
	// center is the screen position of the berth node.
	center sim.Point
	// ring is the screen area of the berth ring with its stroke.
	ring image.Rectangle
	// shade is the ring color.
	shade uint32
	// text is the berth number. At a station with one passenger berth, it
	// is the station name and counts.
	text stationTextBlock
}

// expandedStationText holds the berths and the text of a station whose
// berths show on the map.
type expandedStationText struct {
	berths []berthText
	// station is the station name and counts. It has no lines at a station
	// with one passenger berth, because the berth text holds them.
	station stationTextBlock
	// away is the direction from the siding to the berths. See
	// stationTextDirection.
	away sim.Point
}

// placedStationText is a text block at its place on the screen.
type placedStationText struct {
	// area is the screen area of the text with a padding on each side.
	area  image.Rectangle
	block stationTextBlock
}

// expandedStationInput holds the station state for expandedStationText.
type expandedStationInput struct {
	station sim.Station
	status  observe.StationMetrics
	state   sim.Snapshot
}

// expandedStationText returns the berth rings and the text of a station
// whose berths show on the map.
func (g *Game) expandedStationText(input expandedStationInput) expandedStationText {
	station, status, unit := input.station, input.status, g.layout.unit
	single := !station.ParkingOnly && len(station.Berths) == 1
	expanded := expandedStationText{away: stationTextDirection(g.network, station)}
	// The stroke is centered on the ring radius.
	radius := (berthRingRadius + 1) * unit
	for index, berth := range station.Berths {
		node, _ := g.network.Node(berth.Node)
		center := g.mapPoint(node.Position)
		shade := g.berthShade(berth, input.state)
		number := stationTextBlock{lines: []label{{size: 16, value: strconv.Itoa(index + 1), color: foreground}}}
		if single {
			number = stationTextBlock{lines: []label{
				{size: 16, value: station.Name, color: foreground},
				{y: 21 * unit, size: 10, value: berthOccupancy(berth, input.state), color: shade},
				{y: 37 * unit, size: 9, value: fmt.Sprintf("%d occupied · %d reserved empty · %d free", status.Occupied, status.ReservedEmpty, status.Free), color: muted},
				{y: 52 * unit, size: 9, value: stationQueueText(status), color: muted},
			}}
		}
		expanded.berths = append(expanded.berths, berthText{
			center: center, ring: squareAround(center, radius), shade: shade, text: number,
		})
	}
	if !single && len(station.Berths) > 0 {
		expanded.station = stationTextBlock{lines: []label{
			{size: 16, value: station.Name, color: foreground},
			{y: 23 * unit, size: 10, value: fmt.Sprintf("%d/%d occupied · %d reserved empty · %d free", status.Occupied, len(station.Berths), status.ReservedEmpty, status.Free), color: muted},
			{y: 40 * unit, size: 9, value: stationQueueText(status), color: muted},
		}}
	}
	return expanded
}

// stationQueueText returns the queue line of expanded station text.
func stationQueueText(status observe.StationMetrics) string {
	return fmt.Sprintf("In %d stopped / %d approaching · Out %d stopped", status.EntranceStopped, status.Approaching, status.ExitStopped)
}

// squareAround returns the screen area of a circle.
func squareAround(center sim.Point, radius float64) image.Rectangle {
	return image.Rect(
		int(math.Floor(center.X-radius)), int(math.Floor(center.Y-radius)),
		int(math.Ceil(center.X+radius)), int(math.Ceil(center.Y+radius)),
	)
}

// berthShade returns the ring color of a berth. A berth with a moving or
// busy pod shows the pod color. A berth with an idle pod or no pod keeps the
// muted ring. A white ring marks the selected pod, and the white disc shows
// the idle pod.
func (g *Game) berthShade(berth sim.Berth, state sim.Snapshot) uint32 {
	shade := uint32(muted)
	for _, v := range state.Vehicles {
		if v.Pod.BerthID != berth.ID {
			continue
		}
		if purpose := g.podPurpose(v, state); purpose != purposeIdle {
			shade = purpose.color()
		}
	}
	return shade
}

// berthOccupancy returns the occupancy line of a berth: occupied or free,
// or the pod that arrives at the berth or departs from it.
func berthOccupancy(berth sim.Berth, state sim.Snapshot) string {
	occupancy := "BERTH 0/1"
	for _, b := range state.Berths {
		if b.ID != berth.ID {
			continue
		}
		if b.Occupant != "" {
			occupancy = "BERTH 1/1"
		}
		if b.ReservedBy != "" && b.Occupant == "" {
			occupancy = "ARRIVING " + b.ReservedBy
			for _, v := range state.Vehicles {
				if v.Pod.ID == b.ReservedBy && len(v.Route) > 0 && v.Route[0].From == berth.Node {
					occupancy = "DEPARTING " + b.ReservedBy
				}
			}
		}
	}
	return occupancy
}

// stationTextDirection returns the direction from the siding of a station to
// its berths: from the midpoint of the entry and exit nodes to the center of
// the berth nodes. The lanes of the station come from the siding side, so
// the station text goes the other way. The direction is zero when the
// network does not have these nodes.
func stationTextDirection(network sim.Network, station sim.Station) sim.Point {
	var siding, berths pointMean
	add := func(mean *pointMean, nodeID string) {
		if node, ok := network.Node(nodeID); ok {
			mean.sum.X += node.Position.X
			mean.sum.Y += node.Position.Y
			mean.count++
		}
	}
	add(&siding, station.Entry)
	add(&siding, station.Exit)
	for _, berth := range station.Berths {
		add(&berths, berth.Node)
	}
	from, hasSiding := siding.mean()
	to, hasBerths := berths.mean()
	if !hasSiding || !hasBerths {
		return sim.Point{}
	}
	return sim.Point{X: to.X - from.X, Y: to.Y - from.Y}
}

// placeExpandedStationText returns the text blocks of the expanded stations
// at their places on the screen. It places the berth numbers of a station
// first and then the station text, beside the berth rings and the numbers.
// No block covers a berth ring or an earlier block, and each block covers
// as little of the lanes as possible. A block that has no free place does not
// show, and a station with no berth ring in the map viewport has no text.
//
// It places the stations in network order, and again with the stations with
// the most berths first, because a large station can need the room that a
// small station takes first. It keeps the order that shows more blocks. At
// the same number of blocks, it keeps the order with the lower total cost.
func (g *Game) placeExpandedStationText(stations []expandedStationText, lanes []lanePath) []placedStationText {
	larger := func(a, b expandedStationText) int { return cmp.Compare(len(b.berths), len(a.berths)) }
	placed, cost := g.placeStationTextInOrder(stations, lanes)
	if slices.IsSortedFunc(stations, larger) {
		return placed
	}
	stations = slices.Clone(stations)
	slices.SortStableFunc(stations, larger)
	other, otherCost := g.placeStationTextInOrder(stations, lanes)
	if len(other) > len(placed) || len(other) == len(placed) && otherCost < cost {
		return other
	}
	return placed
}

// placeStationTextInOrder places the text of the stations in their order. It
// returns the text blocks at their places and the sum of their costs. See
// placeStationText for the cost.
func (g *Game) placeStationTextInOrder(stations []expandedStationText, lanes []lanePath) ([]placedStationText, float64) {
	viewport := g.layout.mapViewport
	var occupied []image.Rectangle
	for _, station := range stations {
		for _, berth := range station.berths {
			if berth.ring.Overlaps(viewport) {
				occupied = append(occupied, berth.ring)
			}
		}
	}
	gap := int(math.Ceil(stationTextGap * g.layout.unit))
	padding := int(stationTextPadding * g.layout.unit)
	var placed []placedStationText
	total := 0.0
	place := func(item image.Rectangle, block stationTextBlock, sides []textSide, away sim.Point) (image.Rectangle, bool) {
		area, cost, ok := placeStationText(stationTextPlacement{
			item: item, size: g.stationTextSize(block), sides: sides, away: away,
			gap: gap, padding: padding, viewport: viewport, occupied: occupied,
			lanes: lanes, awayCost: stationTextAwayCost * g.layout.unit,
		})
		if ok {
			occupied = append(occupied, area)
			placed = append(placed, placedStationText{area: area, block: block})
			total += cost
		}
		return area, ok
	}
	for _, station := range stations {
		var item image.Rectangle
		for _, berth := range station.berths {
			item = item.Union(berth.ring)
		}
		for _, berth := range station.berths {
			if len(berth.text.lines) == 0 {
				continue
			}
			sides := berthNumberSides
			if len(station.station.lines) == 0 {
				// The berth text is the station text.
				sides = textSides
			}
			if area, ok := place(berth.ring, berth.text, sides, station.away); ok {
				item = item.Union(area)
			}
		}
		if len(station.station.lines) > 0 && !item.Empty() {
			place(item, station.station, textSides, station.away)
		}
	}
	return placed, total
}

// appendStationTextAreas returns areas with the areas of the placed text
// blocks added.
func appendStationTextAreas(areas []image.Rectangle, placed []placedStationText) []image.Rectangle {
	for _, text := range placed {
		areas = append(areas, text.area)
	}
	return areas
}

// stationTextSize returns the size in screen pixels of a text block with a
// padding on each side.
func (g *Game) stationTextSize(block stationTextBlock) image.Point {
	var width, height float64
	for _, line := range block.lines {
		lineWidth, lineHeight := text.Measure(line.value, g.textFace(line.size), 0)
		width = max(width, line.x+lineWidth)
		height = max(height, line.y+lineHeight)
	}
	padding := 2 * stationTextPadding * g.layout.unit
	return image.Pt(int(math.Ceil(width+padding)), int(math.Ceil(height+padding)))
}

// drawStationText draws the text blocks. Each line has a panel-colored
// backing, so that a lane under the text does not make it hard to read.
// Between the lines and beside short lines, the lanes stay visible.
func (g *Game) drawStationText(screen *ebiten.Image, placed []placedStationText) {
	backing := color.NRGBA(rgb(panel))
	backing.A = stationTextBackingAlpha
	padding := stationTextPadding * g.layout.unit
	for _, block := range placed {
		for _, line := range block.block.lines {
			line.x += float64(block.area.Min.X) + padding
			line.y += float64(block.area.Min.Y) + padding
			width, height := text.Measure(line.value, g.textFace(line.size), 0)
			vector.FillRect(screen, float32(line.x-padding), float32(line.y), float32(width+2*padding), float32(height), backing, false)
			g.label(screen, line)
		}
	}
}

// textSide is one of eight places of a text block around the item that it
// names. x and y are -1, 0 or 1. For example, {x: 0, y: 1} is below the item
// and {x: -1, y: -1} is above the item and to its left.
type textSide struct{ x, y int }

// textSides holds the eight places. When two places point equally far from
// the siding, the earlier place wins. Below and left come first, because a
// pod label shows above and to the right of its pod.
var textSides = []textSide{{0, 1}, {-1, 0}, {-1, 1}, {1, 1}, {-1, -1}, {0, -1}, {1, 0}, {1, -1}}

// berthNumberSides holds the places of a berth number: below, left, above
// and right of the ring. A number at a corner of its ring can look like the
// number of the next berth in a row.
var berthNumberSides = []textSide{{0, 1}, {-1, 0}, {0, -1}, {1, 0}}

// alignment returns how far the place points in the direction away. A
// larger value points more in that direction.
func (side textSide) alignment(away sim.Point) float64 {
	return (float64(side.x)*away.X + float64(side.y)*away.Y) / math.Hypot(float64(side.x), float64(side.y))
}

// stationTextPlacement holds the input of placeStationText. All areas are in
// screen pixels.
type stationTextPlacement struct {
	// item is the area that the text names, for example a berth ring.
	item image.Rectangle
	// size is the size of the text block.
	size image.Point
	// sides holds the places to try, in tie-break order. See textSides.
	sides []textSide
	// away is the direction in which the text goes from the item. A zero
	// direction puts the text below the item.
	away sim.Point
	// gap is the space between the item and the text block.
	gap int
	// padding is the space at each edge of the text block that has no text.
	// A lane in this space does not count as covered.
	padding  int
	viewport image.Rectangle
	// occupied holds the areas that the text block must not cover.
	occupied []image.Rectangle
	// lanes holds the screen paths of the lanes. The text block covers as
	// little of them as possible.
	lanes []lanePath
	// awayCost is the cost of a place at right angles to the away
	// direction, as a length of covered lanes in screen pixels. A place
	// against the away direction costs twice as much.
	awayCost float64
}

// placeStationText returns the area of a text block beside an item and the
// cost of that place. It reports false when the item is outside the
// viewport or when no place is free.
//
// It tries the places around the item. A place directly above, below, left
// or right of the item slides along the item edge to stay inside the
// viewport. Of the places that are inside the viewport and cover no occupied
// area, the place with the lowest cost wins. The cost is the length of the
// lanes that the text covers, plus awayCost for each step that the place
// turns from the away direction. A lane that crosses a corner of the text
// hides less than a lane that runs along a text line. When two places have
// the same cost, the place that points more in the away direction wins.
// When no place fits, the text moves into the viewport, and the same rule
// applies to the places that then cover no occupied area. When all places
// cover an occupied area, it reports false, so that the text does not cover
// other text.
func placeStationText(placement stationTextPlacement) (image.Rectangle, float64, bool) {
	if !placement.item.Overlaps(placement.viewport) {
		return image.Rectangle{}, 0, false
	}
	away := placement.away
	if away == (sim.Point{}) {
		away = sim.Point{Y: 1}
	}
	sides := slices.Clone(placement.sides)
	slices.SortStableFunc(sides, func(a, b textSide) int {
		return cmp.Compare(b.alignment(away), a.alignment(away))
	})
	var inside, moved []textPlace
	for _, side := range sides {
		// turn is 0 in the away direction, 1 at right angles to it, and 2
		// against it.
		turn := 1 - side.alignment(away)/math.Hypot(away.X, away.Y)
		area := side.area(placement)
		if area.In(placement.viewport) {
			inside = append(inside, textPlace{area: area, turn: turn})
		}
		moved = append(moved, textPlace{area: moveInside(area, placement.viewport), turn: turn})
	}
	if area, cost, ok := placement.best(inside); ok {
		return area, cost, true
	}
	return placement.best(moved)
}

// textPlace is a place of a text block. turn tells how far the place turns
// from the away direction.
type textPlace struct {
	area image.Rectangle
	turn float64
}

// best returns the first of the places that covers no occupied area and has
// the lowest cost, and that cost. It reports false when all places cover an
// occupied area.
func (placement stationTextPlacement) best(places []textPlace) (image.Rectangle, float64, bool) {
	var found image.Rectangle
	lowest := math.Inf(1)
	for _, place := range places {
		if slices.ContainsFunc(placement.occupied, place.area.Overlaps) {
			continue
		}
		cost := placement.awayCost * place.turn
		inside := place.area.Inset(placement.padding)
		for _, lane := range placement.lanes {
			cost += lane.covered(inside)
		}
		// A later place must cost at least half a pixel less, so that
		// rounding errors do not decide between equal places.
		if cost < lowest-0.5 {
			found, lowest = place.area, cost
		}
	}
	if math.IsInf(lowest, 1) {
		return image.Rectangle{}, 0, false
	}
	return found, lowest, true
}

// lanePath holds the screen points of a drawn lane, from its start to its
// end.
type lanePath []sim.Point

// pathIn returns the screen path of the lane. It reports false when no part
// of the path is in the viewport.
func (geometry laneGeometry) pathIn(viewport image.Rectangle) (lanePath, bool) {
	path := lanePath(slices.Clone(geometry.points[:geometry.count]))
	for i := 1; i < len(path); i++ {
		if _, ok := segmentIn(path[i-1], path[i], viewport); ok {
			return path, true
		}
	}
	return nil, false
}

// covered returns the length of the path in the area.
func (path lanePath) covered(area image.Rectangle) float64 {
	length := 0.0
	for i := 1; i < len(path); i++ {
		if part, ok := segmentIn(path[i-1], path[i], area); ok {
			length += part
		}
	}
	return length
}

// segmentIn returns the length of the segment from a to b in the area. It
// reports false when no part of the segment is in the area. It clips the
// segment to each edge of the area in turn (Liang-Barsky).
func segmentIn(a, b sim.Point, area image.Rectangle) (float64, bool) {
	low, high := 0.0, 1.0
	// clip keeps the part of the segment where p*t <= q.
	clip := func(p, q float64) bool {
		switch {
		case p == 0:
			return q >= 0
		case p < 0:
			low = max(low, q/p)
		default:
			high = min(high, q/p)
		}
		return low <= high
	}
	dx, dy := b.X-a.X, b.Y-a.Y
	if clip(-dx, a.X-float64(area.Min.X)) && clip(dx, float64(area.Max.X)-a.X) &&
		clip(-dy, a.Y-float64(area.Min.Y)) && clip(dy, float64(area.Max.Y)-a.Y) {
		return (high - low) * math.Hypot(dx, dy), true
	}
	return 0, false
}

// area returns the area of the text block at this place. A place directly
// above or below the item slides left or right to stay inside the viewport.
// A place directly left or right of the item slides up or down. A place at a
// corner moves in toward the item, so that it is as near to a round item,
// such as a berth ring, as the other places.
func (side textSide) area(placement stationTextPlacement) image.Rectangle {
	item, size, gap := placement.item, placement.size, placement.gap
	if side.x != 0 && side.y != 0 {
		gap -= int(math.Round(float64(min(item.Dx(), item.Dy())) / 2 * (1 - math.Sqrt2/2)))
	}
	x := (item.Min.X + item.Max.X - size.X) / 2
	switch side.x {
	case -1:
		x = item.Min.X - gap - size.X
	case 1:
		x = item.Max.X + gap
	}
	y := (item.Min.Y + item.Max.Y - size.Y) / 2
	switch side.y {
	case -1:
		y = item.Min.Y - gap - size.Y
	case 1:
		y = item.Max.Y + gap
	}
	area := image.Rect(x, y, x+size.X, y+size.Y)
	viewport := placement.viewport
	if side.x == 0 {
		area = area.Add(image.Pt(shiftInside(area.Min.X, area.Max.X, viewport.Min.X, viewport.Max.X), 0))
	}
	if side.y == 0 {
		area = area.Add(image.Pt(0, shiftInside(area.Min.Y, area.Max.Y, viewport.Min.Y, viewport.Max.Y)))
	}
	return area
}

// moveInside returns the area moved into the viewport. An area larger than
// the viewport keeps its top left corner inside the viewport.
func moveInside(area, viewport image.Rectangle) image.Rectangle {
	return area.Add(image.Pt(
		shiftInside(area.Min.X, area.Max.X, viewport.Min.X, viewport.Max.X),
		shiftInside(area.Min.Y, area.Max.Y, viewport.Min.Y, viewport.Max.Y),
	))
}

// shiftInside returns the shift that moves the span from start to end into
// the span from low to high. A span that is too long starts at low.
func shiftInside(start, end, low, high int) int {
	shift := 0
	if end > high {
		shift = high - end
	}
	if start+shift < low {
		shift = low - start
	}
	return shift
}
