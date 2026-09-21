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
	mapPanMargin     = 40
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
}

type cameraFit struct {
	bounds   worldBounds
	viewport image.Rectangle
	unit     float64
}

func (c *mapCamera) fit(input cameraFit) {
	bounds := input.bounds
	c.viewport = input.viewport
	c.panMargin = mapPanMargin * input.unit
	c.dragThreshold = mapDragThreshold * input.unit
	c.world = bounds
	worldWidth := max(1, bounds.right-bounds.left)
	worldHeight := max(1, bounds.bottom-bounds.top)
	c.minScale = min((float64(c.viewport.Dx())-112*input.unit)/worldWidth, (float64(c.viewport.Dy())-84*input.unit)/worldHeight)
	c.maxScale = c.minScale * mapMaxZoom
	c.scale = c.minScale
	c.origin = sim.Point{
		X: float64(c.viewport.Min.X) + (float64(c.viewport.Dx())-worldWidth*c.scale)/2 - bounds.left*c.scale,
		Y: float64(c.viewport.Min.Y) + (float64(c.viewport.Dy())-worldHeight*c.scale)/2 - bounds.top*c.scale,
	}
	c.dragging, c.dragged = false, false
	c.initialized = true
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
