package view

import (
	"image"
	"math"

	"github.com/dotwaffle/podsim/internal/sim"
)

const (
	mapDragThreshold = 5
	mapZoomStep      = 1.25
	mapMaxZoom       = 40
	// minimumZoomSpacing is a berth spacing in meters. A smaller spacing
	// raises the largest zoom only as much as this spacing. See maxMapScale.
	minimumZoomSpacing = 1
	mapPanMargin       = 40
)

type worldBounds struct {
	left, top, right, bottom float64
}

type mapCamera struct {
	scale, minScale, maxScale float64
	origin                    sim.Point
	world                     worldBounds
	initialized               bool
	dragStart, dragLast       sim.Point
	dragging, dragged         bool
	viewport                  image.Rectangle
	panMargin                 float64
	dragThreshold             float64
	// unit is the layout unit of the last fit. See limitZoom.
	unit float64
}

type cameraFit struct {
	bounds   worldBounds
	viewport image.Rectangle
	unit     float64
	// berthSpacing is the smallest berth spacing in meters of a station,
	// or zero. See maxMapScale.
	berthSpacing float64
}

func (c *mapCamera) fit(input cameraFit) {
	bounds := input.bounds
	c.viewport = input.viewport
	c.panMargin = mapPanMargin * input.unit
	c.dragThreshold = mapDragThreshold * input.unit
	c.unit = input.unit
	c.world = bounds
	worldWidth := max(1, bounds.right-bounds.left)
	worldHeight := max(1, bounds.bottom-bounds.top)
	c.minScale = min((float64(c.viewport.Dx())-112*input.unit)/worldWidth, (float64(c.viewport.Dy())-84*input.unit)/worldHeight)
	c.maxScale = maxMapScale(c.minScale, input.berthSpacing, input.unit)
	c.scale = c.minScale
	c.origin = sim.Point{
		X: float64(c.viewport.Min.X) + (float64(c.viewport.Dx())-worldWidth*c.scale)/2 - bounds.left*c.scale,
		Y: float64(c.viewport.Min.Y) + (float64(c.viewport.Dy())-worldHeight*c.scale)/2 - bounds.top*c.scale,
	}
	c.dragging, c.dragged = false, false
	c.initialized = true
}

// maxMapScale returns the largest map scale for the Fit scale fit and the
// layout unit. It is mapMaxZoom times fit. When the berths that are
// spacing meters apart do not separate at that scale (see
// showStationBerths), it is one zoom step past the scale at which they
// separate. A spacing below minimumZoomSpacing counts as
// minimumZoomSpacing, so that the scale stays finite. A spacing of zero
// keeps mapMaxZoom times fit.
func maxMapScale(fit, spacing, unit float64) float64 {
	scale := fit * mapMaxZoom
	if spacing <= 0 || spacing*scale >= berthSeparation*unit {
		return scale
	}
	return max(scale, berthSeparation*unit/max(spacing, minimumZoomSpacing)*mapZoomStep)
}

// limitZoom sets the largest scale for the berth spacing of the network
// without a new fit. The Fit scale, the zoom and the pan stay. A scale
// past the new largest scale becomes the largest scale at the center of
// the viewport. limitZoom reports whether the scale or the origin changed.
func (c *mapCamera) limitZoom(spacing float64) bool {
	if !c.initialized {
		return false
	}
	c.maxScale = maxMapScale(c.minScale, spacing, c.unit)
	if c.scale <= c.maxScale {
		return false
	}
	center := sim.Point{X: float64(c.viewport.Min.X+c.viewport.Max.X) / 2, Y: float64(c.viewport.Min.Y+c.viewport.Max.Y) / 2}
	world := c.worldPoint(center)
	c.scale = c.maxScale
	c.origin = sim.Point{X: center.X - world.X*c.scale, Y: center.Y - world.Y*c.scale}
	c.clamp()
	return true
}

// atMaxZoom reports whether the map is at its largest zoom. A camera that
// has no scale limits yet is not at its largest zoom.
func (c *mapCamera) atMaxZoom() bool {
	return c.initialized && c.scale >= c.maxScale
}

func (c *mapCamera) screenPoint(world sim.Point) sim.Point {
	return sim.Point{X: c.origin.X + world.X*c.scale, Y: c.origin.Y + world.Y*c.scale}
}

func (c *mapCamera) worldPoint(screen sim.Point) sim.Point {
	return sim.Point{X: (screen.X - c.origin.X) / c.scale, Y: (screen.Y - c.origin.Y) / c.scale}
}

func (c *mapCamera) zoomAt(screen sim.Point, factor float64) bool {
	if !c.initialized || factor <= 0 {
		return false
	}
	oldScale := c.scale
	world := c.worldPoint(screen)
	c.scale = min(c.maxScale, max(c.minScale, c.scale*factor))
	if c.scale == oldScale {
		return false
	}
	c.origin = sim.Point{X: screen.X - world.X*c.scale, Y: screen.Y - world.Y*c.scale}
	c.clamp()
	return true
}

func (c *mapCamera) pan(delta sim.Point) bool {
	if !c.initialized || (delta.X == 0 && delta.Y == 0) {
		return false
	}
	before := c.origin
	c.origin.X += delta.X
	c.origin.Y += delta.Y
	c.clamp()
	return c.origin != before
}

func (c *mapCamera) centerOn(world sim.Point) bool {
	if !c.initialized {
		return false
	}
	before := c.origin
	c.origin = sim.Point{
		X: float64(c.viewport.Min.X+c.viewport.Max.X)/2 - world.X*c.scale,
		Y: float64(c.viewport.Min.Y+c.viewport.Max.Y)/2 - world.Y*c.scale,
	}
	c.clamp()
	return c.origin != before
}

func (c *mapCamera) clamp() {
	c.origin.X = clampOrigin(axisBounds{origin: c.origin.X, contentMin: c.world.left * c.scale, contentMax: c.world.right * c.scale, viewMin: float64(c.viewport.Min.X), viewMax: float64(c.viewport.Max.X), margin: c.panMargin})
	c.origin.Y = clampOrigin(axisBounds{origin: c.origin.Y, contentMin: c.world.top * c.scale, contentMax: c.world.bottom * c.scale, viewMin: float64(c.viewport.Min.Y), viewMax: float64(c.viewport.Max.Y), margin: c.panMargin})
}

type axisBounds struct {
	origin, contentMin, contentMax, viewMin, viewMax, margin float64
}

func clampOrigin(bounds axisBounds) float64 {
	contentSize, viewSize := bounds.contentMax-bounds.contentMin, bounds.viewMax-bounds.viewMin
	if contentSize <= viewSize-2*bounds.margin {
		return bounds.viewMin + (viewSize-contentSize)/2 - bounds.contentMin
	}
	return min(bounds.viewMin+bounds.margin-bounds.contentMin, max(bounds.viewMax-bounds.margin-bounds.contentMax, bounds.origin))
}

func (c *mapCamera) beginDrag(point sim.Point) {
	c.dragStart, c.dragLast = point, point
	c.dragging, c.dragged = true, false
}

func (c *mapCamera) drag(point sim.Point) bool {
	if !c.dragging {
		return false
	}
	if !c.dragged && math.Hypot(point.X-c.dragStart.X, point.Y-c.dragStart.Y) >= c.dragThreshold {
		c.dragged = true
	}
	changed := false
	if c.dragged {
		changed = c.pan(sim.Point{X: point.X - c.dragLast.X, Y: point.Y - c.dragLast.Y})
	}
	c.dragLast = point
	return changed
}

func (c *mapCamera) endDrag() (click bool) {
	click = c.dragging && !c.dragged
	c.dragging = false
	return click
}

func (c *mapCamera) contains(point sim.Point) bool {
	return image.Pt(int(point.X), int(point.Y)).In(c.viewport)
}

// Browsers report wheel distance in pixels; native mouse wheels report steps.
func wheelZoomFactor(delta float64, pixels bool) float64 {
	if pixels {
		delta /= 100
	}
	return math.Pow(mapZoomStep, min(3, max(-3, delta)))
}
