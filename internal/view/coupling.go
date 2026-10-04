package view

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/dotwaffle/podsim/internal/sim"
)

const (
	// couplingBodyWidth is the line width in display units of the outline
	// of a cabin body in a mechanical train.
	couplingBodyWidth = 1.5
	// couplingEnvelopeWidth is the line width in display units of the
	// outline of the protected space of a docking or opening maneuver.
	couplingEnvelopeWidth = 1.0
	// couplingConnectorWidth is the smallest width in display units of the
	// connector between two latched cabins.
	couplingConnectorWidth = 2.0
	// couplingMarkPadding is the smallest distance in display units from
	// the center of a cabin to the side of its train mark. The mark then
	// stays clear of the pod sprite, which has the radius podRadius.
	couplingMarkPadding = podRadius + 2
	// couplingMarkWidth is the line width in display units of a train mark.
	couplingMarkWidth = 2.0
	// couplingTrainColor is the color of train marks and body outlines. It
	// differs from the purpose colors of the selected route and from the
	// foreground color of the virtual platoon line. The connector between
	// the bodies has the accent color.
	couplingTrainColor = muted
)

// couplingLine is one line of a mechanical train on the screen.
type couplingLine struct {
	from, to sim.Point
	width    float64
	color    uint32
}

// couplingDrawInput holds the mechanical trains of a map state and the
// values that place them on the screen.
type couplingDrawInput struct {
	groups []sim.CouplingGroupView
	// screen returns the screen point of a world point.
	screen func(sim.Point) sim.Point
	unit   float64
}

// couplingLines returns the lines of the mechanical trains in drawing order.
// The map draws them after the pods, so that no pod sprite covers them.
//
// When the bodies are wider on the screen than a train mark, the lines show
// the geometry that the server sent: the maneuver envelope of a docking or
// opening pair, an outline of each body, and the connector between the body
// pins at its own width or wider. Otherwise a train mark replaces them. The
// mark is a rectangle around both bodies along the train axis, at least
// couplingMarkPadding units from each cabin center. A latched train has a
// closed mark. A pair that docks, opens, or drains has only the corners of
// the mark. A group with a body corner that is not finite is not drawn.
// The connector has the accent color, and all other lines have
// couplingTrainColor.
func couplingLines(input couplingDrawInput) []couplingLine {
	var lines []couplingLine
	for _, group := range input.groups {
		mark, legible, ok := couplingMark(group, input)
		switch {
		case !ok:
		case !legible && group.Connector != nil:
			lines = appendOutline(lines, mark, couplingMarkWidth*input.unit)
		case !legible:
			lines = appendCorners(lines, mark, couplingMarkWidth*input.unit)
		default:
			lines = appendGeometry(lines, input, group)
		}
	}
	return lines
}

// appendGeometry appends the envelope, body, and connector lines of group.
func appendGeometry(lines []couplingLine, input couplingDrawInput, group sim.CouplingGroupView) []couplingLine {
	if shape := group.ManeuverEnvelope; shape != nil && finiteRectangle(*shape) {
		lines = appendOutline(lines, input.worldRectangle(*shape), couplingEnvelopeWidth*input.unit)
	}
	for _, body := range group.Bodies {
		lines = appendOutline(lines, input.worldRectangle(body), couplingBodyWidth*input.unit)
	}
	if group.Connector != nil && finiteRectangle(*group.Connector) {
		// The first and last corners are at the front pin, and the middle
		// corners are at the rear pin.
		c := input.worldRectangle(*group.Connector)
		width := max(distance(c[0], c[3]), couplingConnectorWidth*input.unit)
		lines = append(lines, couplingLine{from: midpoint(c[0], c[3]), to: midpoint(c[1], c[2]), width: width, color: accent})
	}
	return lines
}

// couplingMark returns the train mark of group in screen points, in the
// corner order of the server: front left, rear left, rear right, front
// right. legible is true when the bodies are at least as wide as the mark.
// ok is false when a body corner is not finite.
func couplingMark(group sim.CouplingGroupView, input couplingDrawInput) (mark [4]sim.Point, legible, ok bool) {
	if !finiteRectangle(group.Bodies[0]) || !finiteRectangle(group.Bodies[1]) {
		return mark, false, false
	}
	front, rear := input.screen(center(group.Bodies[0])), input.screen(center(group.Bodies[1]))
	axis := sim.Point{X: front.X - rear.X, Y: front.Y - rear.Y}
	if distance(front, rear) < 1e-9 {
		// The cabin centers meet on the screen. The front body gives the axis.
		a, b := input.screen(group.Bodies[0].Corners[0]), input.screen(group.Bodies[0].Corners[1])
		axis = sim.Point{X: a.X - b.X, Y: a.Y - b.Y}
	}
	if length := math.Hypot(axis.X, axis.Y); length > 1e-9 {
		axis = sim.Point{X: axis.X / length, Y: axis.Y / length}
	} else {
		axis = sim.Point{X: 1}
	}
	normal, middle := sim.Point{X: -axis.Y, Y: axis.X}, midpoint(front, rear)
	var length, width float64
	for _, body := range group.Bodies {
		for _, corner := range input.worldRectangle(body) {
			offset := sim.Point{X: corner.X - middle.X, Y: corner.Y - middle.Y}
			length = max(length, math.Abs(offset.X*axis.X+offset.Y*axis.Y))
			width = max(width, math.Abs(offset.X*normal.X+offset.Y*normal.Y))
		}
	}
	padding := couplingMarkPadding * input.unit
	// Each body must clear its own pod sprite. The width across the axis
	// can be larger than a body when the bodies turn apart, as when they
	// drain.
	legible = width >= padding && input.halfWidth(group.Bodies[0]) >= padding && input.halfWidth(group.Bodies[1]) >= padding
	length, width = max(length, distance(front, rear)/2+padding), max(width, padding)
	at := func(along, across float64) sim.Point {
		return sim.Point{X: middle.X + axis.X*along + normal.X*across, Y: middle.Y + axis.Y*along + normal.Y*across}
	}
	return [4]sim.Point{at(length, width), at(-length, width), at(-length, -width), at(length, -width)}, legible, true
}

// halfWidth returns half the shorter side of rectangle on the screen. It is
// the smallest distance from the center of the rectangle to a side.
func (input couplingDrawInput) halfWidth(rectangle sim.CouplingRectangle) float64 {
	corners := input.worldRectangle(rectangle)
	return min(distance(corners[0], corners[1]), distance(corners[1], corners[2])) / 2
}

// worldRectangle returns the screen points of the corners of rectangle.
func (input couplingDrawInput) worldRectangle(rectangle sim.CouplingRectangle) [4]sim.Point {
	var corners [4]sim.Point
	for i, corner := range rectangle.Corners {
		corners[i] = input.screen(corner)
	}
	return corners
}

// mapCouplingLines returns the lines of the mechanical trains of state on the
// map screen.
func (g *Game) mapCouplingLines(state sim.Snapshot) []couplingLine {
	return couplingLines(couplingDrawInput{groups: state.CouplingGroups, screen: g.mapPoint, unit: g.layout.unit})
}

// appendOutline appends the four sides of a screen rectangle to lines.
func appendOutline(lines []couplingLine, corners [4]sim.Point, width float64) []couplingLine {
	for i, corner := range corners {
		lines = append(lines, couplingLine{from: corner, to: corners[(i+1)%len(corners)], width: width, color: couplingTrainColor})
	}
	return lines
}

// appendCorners appends the corners of a screen rectangle to lines. Each
// corner has two arms, each a quarter of its side.
func appendCorners(lines []couplingLine, corners [4]sim.Point, width float64) []couplingLine {
	for i, corner := range corners {
		next := corners[(i+1)%len(corners)]
		quarter := sim.Point{X: (next.X - corner.X) / 4, Y: (next.Y - corner.Y) / 4}
		lines = append(lines,
			couplingLine{from: corner, to: sim.Point{X: corner.X + quarter.X, Y: corner.Y + quarter.Y}, width: width, color: couplingTrainColor},
			couplingLine{from: sim.Point{X: next.X - quarter.X, Y: next.Y - quarter.Y}, to: next, width: width, color: couplingTrainColor})
	}
	return lines
}

func finiteRectangle(rectangle sim.CouplingRectangle) bool {
	for _, corner := range rectangle.Corners {
		for _, value := range []float64{corner.X, corner.Y} {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return false
			}
		}
	}
	return true
}

// center returns the mean of the corners of rectangle.
func center(rectangle sim.CouplingRectangle) sim.Point {
	var sum sim.Point
	for _, corner := range rectangle.Corners {
		sum.X, sum.Y = sum.X+corner.X, sum.Y+corner.Y
	}
	return sim.Point{X: sum.X / 4, Y: sum.Y / 4}
}

func midpoint(a, b sim.Point) sim.Point {
	return sim.Point{X: (a.X + b.X) / 2, Y: (a.Y + b.Y) / 2}
}

func distance(a, b sim.Point) float64 { return math.Hypot(a.X-b.X, a.Y-b.Y) }

// couplingRow returns the Train row of the pod inspector for the pod at
// index. The row gives the phase of the mechanical train and names the other
// cabin by its fleet number. It reads the train registry of state and does
// not use virtual platoon fields. A pod outside a train has no row.
func couplingRow(state sim.Snapshot, index int) (inspectionRow, bool) {
	if index < 0 || index >= len(state.Vehicles) || state.Vehicles[index].CouplingID == "" {
		return inspectionRow{}, false
	}
	vehicle := state.Vehicles[index]
	i := slices.IndexFunc(state.CouplingGroups, func(group sim.CouplingGroupView) bool { return group.ID == vehicle.CouplingID })
	if i < 0 {
		return inspectionRow{}, false
	}
	group := state.CouplingGroups[i]
	other := group.Members[0]
	if other == vehicle.Pod.ID {
		other = group.Members[1]
	}
	if j := slices.IndexFunc(state.Vehicles, func(v sim.Vehicle) bool { return v.Pod.ID == other }); j >= 0 {
		other = fleetPodLabel(j)
	}
	phase := string(group.Phase)
	if phase != "" {
		phase = strings.ToUpper(phase[:1]) + phase[1:]
	}
	return inspectionRow{"Train", fmt.Sprintf("%s / %s", phase, other)}, true
}

// podInspectionRows returns the rows of the pod inspector for the pod at
// index. The Train row of a mechanical train comes before the station phase
// rows.
func (g *Game) podInspectionRows(state sim.Snapshot, index int) []inspectionRow {
	rows := g.inspectionRows(state.Vehicles[index])
	if row, ok := couplingRow(state, index); ok {
		rows = slices.Insert(rows, 2, row)
	}
	return rows
}
