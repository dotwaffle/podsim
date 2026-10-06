package view

import (
	"image"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"github.com/dotwaffle/podsim/internal/sim"
)

// networkCacheKey identifies the content of the cached base layer. The
// camera origin is not in the key. A pan moves the cached layer on the
// screen. See baseLayerShift.
type networkCacheKey struct {
	network networkIndexKey
	scale   float64
	unit    float64
	// viewport is the map viewport. The cached layer covers it and a
	// margin around it.
	viewport image.Rectangle
}

// networkBaseCache holds the cached base layer of the map. See
// planBaseLayer and drawCachedNetworkBase.
type networkBaseCache struct {
	image *ebiten.Image
	key   networkCacheKey
	valid bool
	lanes []lanePath
	// origin is the camera origin of the cached base layer.
	origin sim.Point
}

// baseNetworkInput holds the style and the place of a network base layer.
type baseNetworkInput struct {
	style networkStyle
	// area is the screen area of the layer. drawBaseNetwork returns the
	// paths of the lanes in this area.
	area image.Rectangle
	// offset moves each screen point before drawBaseNetwork draws it. The
	// cached layer uses it, because its image starts at the corner of area.
	offset sim.Point
}

// drawBaseNetwork draws the lanes, their direction arrows, and the node dots
// in the network style. It draws the arrows after all the lanes, so that no
// lane covers an arrow. It returns the screen paths of the lanes in the
// area of the layer, without the offset.
func (g *Game) drawBaseNetwork(screen *ebiten.Image, input baseNetworkInput) []lanePath {
	style := input.style
	var arrows []arrow
	var paths []lanePath
	for _, lane := range g.network.Lanes {
		geometry, path, ok := g.baseLane(lane, style.detailed, input.area)
		if ok {
			paths = append(paths, path)
		}
		geometry = geometry.moved(input.offset)
		geometry.draw(screen, style.laneStroke(lane))
		if a, ok := style.laneArrow(lane, geometry); ok {
			arrows = append(arrows, a)
		}
	}
	for _, a := range style.spacedArrows(arrows) {
		drawArrow(screen, a)
	}
	if !style.nodeDots {
		return paths
	}
	for _, node := range g.network.Nodes {
		point := movePoint(g.mapPoint(node.Position), input.offset)
		vector.FillCircle(screen, float32(point.X), float32(point.Y), float32(3*g.layout.unit), rgb(muted), style.antialias)
	}
	return paths
}

// baseLane returns the screen geometry of a lane and its screen path. ok is
// false when no part of the lane is in area. It does not draw.
func (g *Game) baseLane(lane sim.Lane, detailed bool, area image.Rectangle) (geometry laneGeometry, path lanePath, ok bool) {
	geometry = g.laneGeometry(lane, detailed)
	path, ok = geometry.pathIn(area)
	return geometry, path, ok
}

// movePoint returns point moved by offset.
func movePoint(point, offset sim.Point) sim.Point {
	return sim.Point{X: point.X + offset.X, Y: point.Y + offset.Y}
}

// moved returns the lane geometry moved on the screen by offset.
func (geometry laneGeometry) moved(offset sim.Point) laneGeometry {
	if offset == (sim.Point{}) {
		return geometry
	}
	for i := range geometry.count {
		geometry.points[i] = movePoint(geometry.points[i], offset)
	}
	geometry.arrowTip = movePoint(geometry.arrowTip, offset)
	return geometry
}

// movedLanePaths returns the lane paths moved on the screen by offset.
func movedLanePaths(paths []lanePath, offset sim.Point) []lanePath {
	if offset == (sim.Point{}) {
		return paths
	}
	moved := make([]lanePath, len(paths))
	for i, path := range paths {
		moved[i] = make(lanePath, len(path))
		for j, point := range path {
			moved[i][j] = movePoint(point, offset)
		}
	}
	return moved
}

// baseLayerMargin returns the margin in screen pixels of the cached base
// layer around the map viewport. It is a quarter of the shorter side of the
// viewport. When the layer image with that margin is larger than limit on a
// side, the margin is smaller, so that the image fits. The layout keeps the
// screen within the limit, and the panels make the viewport narrower and
// shorter than the screen, so the viewport fits the limit. A limit of 0 is
// not known and does not change the margin. See imageSideLimit and
// screenScale.
func baseLayerMargin(viewport image.Rectangle, limit int) int {
	margin := min(viewport.Dx(), viewport.Dy()) / 4
	if limit <= 0 {
		return margin
	}
	fit := (limit - max(viewport.Dx(), viewport.Dy())) / 2
	return max(0, min(margin, fit))
}

// baseLayerInput holds the cached base layer state and the current map
// state for baseLayerShift.
type baseLayerInput struct {
	// valid is false when the cache has no layer.
	valid           bool
	cached, current networkCacheKey
	// drawnOrigin is the camera origin of the cached layer. origin is the
	// current camera origin.
	drawnOrigin, origin sim.Point
	// margin is the margin in screen pixels of the cached layer around the
	// map viewport.
	margin float64
}

// baseLayerShift returns the screen distance from the cached base layer to
// its current place. ok is false when the cache must draw the layer again:
// when it has no layer, when the network, the scale, the unit or the
// viewport changed, or when a pan moved the viewport past the margin of the
// layer.
func baseLayerShift(input baseLayerInput) (shift sim.Point, ok bool) {
	if !input.valid || input.cached != input.current {
		return sim.Point{}, false
	}
	shift = sim.Point{X: input.origin.X - input.drawnOrigin.X, Y: input.origin.Y - input.drawnOrigin.Y}
	if math.Abs(shift.X) > input.margin || math.Abs(shift.Y) > input.margin {
		return sim.Point{}, false
	}
	return shift, true
}

// baseLayerArea returns the screen area of the cached base layer: the map
// viewport and the margin around it. limit is the largest side in pixels of
// the layer image. See baseLayerMargin.
func baseLayerArea(viewport image.Rectangle, limit int) image.Rectangle {
	return viewport.Inset(-baseLayerMargin(viewport, limit))
}

// baseLayerPlan tells drawCachedNetworkBase how to show the cached base
// layer in the current frame.
type baseLayerPlan struct {
	// area is the screen area of the layer when the cache drew it.
	area image.Rectangle
	// shift is the screen distance from the cached layer to its current
	// place.
	shift sim.Point
	// redraw is true when the cache must draw the layer again.
	redraw bool
}

// imageOffset returns the distance that moves a screen point to its point
// in the layer image. The image starts at the corner of the layer area.
func (plan baseLayerPlan) imageOffset() sim.Point {
	return sim.Point{X: -float64(plan.area.Min.X), Y: -float64(plan.area.Min.Y)}
}

// planBaseLayer decides how to show the cached base layer in the current
// frame. limit is the largest side in pixels of the layer image. It does
// not draw. When the layer must be drawn again, it records the new key and
// camera origin of the cache, so a later pan is measured from the new layer.
func (g *Game) planBaseLayer(limit int) baseLayerPlan {
	viewport := g.layout.mapViewport
	key := g.currentNetworkCacheKey()
	shift, ok := baseLayerShift(baseLayerInput{
		valid: g.networkBase.valid, cached: g.networkBase.key, current: key,
		drawnOrigin: g.networkBase.origin, origin: g.mapOrigin, margin: float64(baseLayerMargin(viewport, limit)),
	})
	plan := baseLayerPlan{area: baseLayerArea(viewport, limit), shift: shift, redraw: !ok}
	if plan.redraw {
		g.networkBase.key = key
		g.networkBase.origin = g.mapOrigin
		g.networkBase.valid = true
	}
	return plan
}

// drawCachedNetworkBase draws the cached base network. The cached layer
// covers the map viewport and a margin around it. See planBaseLayer. It
// returns the screen paths of the lanes in the layer, as they were when it
// drew the layer, and the distance to move them.
func (g *Game) drawCachedNetworkBase(screen *ebiten.Image, style networkStyle) ([]lanePath, sim.Point) {
	area := baseLayerArea(g.layout.mapViewport, style.imageLimit)
	if g.networkBase.image == nil || g.networkBase.image.Bounds().Size() != area.Size() {
		if g.networkBase.image != nil {
			g.networkBase.image.Deallocate()
		}
		g.networkBase.image = ebiten.NewImage(area.Dx(), area.Dy())
		g.networkBase.valid = false
	}
	plan := g.planBaseLayer(style.imageLimit)
	if plan.redraw {
		g.networkBase.image.Clear()
		g.networkBase.lanes = g.drawBaseNetwork(g.networkBase.image, baseNetworkInput{
			style: style, area: plan.area, offset: plan.imageOffset(),
		})
	}
	options := &ebiten.DrawImageOptions{}
	options.GeoM.Translate(float64(plan.area.Min.X)+plan.shift.X, float64(plan.area.Min.Y)+plan.shift.Y)
	screen.DrawImage(g.networkBase.image, options)
	return g.networkBase.lanes, plan.shift
}

func (g *Game) currentNetworkCacheKey() networkCacheKey {
	return networkCacheKey{network: g.currentNetworkIndexKey(), scale: g.mapScale, unit: g.layout.unit, viewport: g.layout.mapViewport}
}

func (g *Game) releaseNetworkBase() {
	if g.networkBase.image == nil {
		return
	}
	g.networkBase.image.Deallocate()
	g.networkBase = networkBaseCache{}
}
