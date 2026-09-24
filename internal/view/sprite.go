package view

import (
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"github.com/dotwaffle/podsim/internal/sim"
)

// circleLayer is one circle of a sprite. The radius and the width are in
// device pixels. A layer with width 0 is a filled disc. A layer with a width
// is a ring, and the line is centered on the radius. A layer with radius 0
// is empty.
type circleLayer struct {
	radius, width float32
	color         uint32
}

// outerRadius returns the distance from the center to the outer edge of the
// layer.
func (l circleLayer) outerRadius() float32 {
	return l.radius + l.width/2
}

// arrowLayer is a direction arrow of a sprite. The tip is at the center of
// the sprite, and the arrow points in the +X direction. unit is the display
// unit in device pixels. A layer with a zero size is empty.
type arrowLayer struct {
	size  arrowSize
	unit  float64
	color uint32
}

// outerRadius returns the distance from the tip to the farthest edge of the
// arrow lines.
func (l arrowLayer) outerRadius() float32 {
	if l.size == (arrowSize{}) {
		return 0
	}
	return float32((math.Hypot(l.size.length, l.size.halfWidth) + arrowLineWidth/2) * l.unit)
}

// spriteKey names the shape of one sprite. The sprite draws the circle
// layers in order, so the second layer covers the first. Then it draws the
// arrow.
type spriteKey struct {
	layers [2]circleLayer
	arrow  arrowLayer
}

// turnedResolution is the number of image pixels in each device pixel of a
// sprite with an arrow. The map turns such a sprite with linear filtering.
// The finer image keeps the arrow lines sharp and bright after the turn.
const turnedResolution = 2

// resolution returns the number of image pixels in each device pixel of the
// sprite.
func (k spriteKey) resolution() float64 {
	if k.arrow.size != (arrowSize{}) {
		return turnedResolution
	}
	return 1
}

// side returns the width and the height of the sprite in device pixels.
// The sprite has one empty pixel around the shape for the antialiased edge.
// The side is even, so the sprite center is on a pixel corner. Sprites with
// the same center then stay concentric after spriteOrigin moves them to
// whole pixels.
func (k spriteKey) side() int {
	outer := k.arrow.outerRadius()
	for _, layer := range k.layers {
		outer = max(outer, layer.outerRadius())
	}
	return 2*int(math.Ceil(float64(outer))) + 2
}

// discSprite returns the sprite of a filled disc.
func discSprite(radius float64, shade uint32) spriteKey {
	return spriteKey{layers: [2]circleLayer{{radius: float32(radius), color: shade}}}
}

// ringSprite returns the sprite of a ring with a line of the given width.
func ringSprite(radius, width float64, shade uint32) spriteKey {
	return spriteKey{layers: [2]circleLayer{{radius: float32(radius), width: float32(width), color: shade}}}
}

// markerSprite returns the sprite of a collapsed station marker: a disc in
// the lane color with a muted outline.
func markerSprite(radius, unit float64) spriteKey {
	return spriteKey{layers: [2]circleLayer{
		{radius: float32(radius), color: track},
		{radius: float32(radius), width: float32(markerOutlineWidth * unit), color: muted},
	}}
}

// arrowSprite returns the sprite of the arrow a, with the arrow turned to
// the +X direction.
func arrowSprite(a arrow) spriteKey {
	return spriteKey{arrow: arrowLayer{size: a.size, unit: a.unit, color: a.color}}
}

// spriteScale holds the values that the sprite sizes depend on. unit is the
// display unit in device pixels. markerRadius is the radius of a collapsed
// station marker, which changes with the map zoom on a dense map.
type spriteScale struct {
	unit, markerRadius float64
}

// spriteCache keeps antialiased shapes as small images. The map draws each
// pod, ring, marker and route arrow with one DrawImage of a sprite. This
// costs less than an antialiased vector fill or stroke in each frame. The
// zero value is ready to use.
type spriteCache struct {
	scale   spriteScale
	sprites map[spriteKey]*ebiten.Image
	// build makes the image of a sprite. When it is nil, the cache uses
	// buildSprite. Tests set it, so that they do not draw.
	build func(spriteKey) *ebiten.Image
	// release frees the image of a sprite. When it is nil, the cache calls
	// Deallocate.
	release func(*ebiten.Image)
}

// sprite returns the image of key at scale. It returns the same image for
// the same key until the scale changes. A scale change releases all images,
// so that images of old sizes do not collect in the cache.
func (c *spriteCache) sprite(scale spriteScale, key spriteKey) *ebiten.Image {
	if scale != c.scale {
		c.clear()
		c.scale = scale
	}
	if image, ok := c.sprites[key]; ok {
		return image
	}
	build := c.build
	if build == nil {
		build = buildSprite
	}
	image := build(key)
	if c.sprites == nil {
		c.sprites = make(map[spriteKey]*ebiten.Image)
	}
	c.sprites[key] = image
	return image
}

// clear releases all sprite images.
func (c *spriteCache) clear() {
	for key, image := range c.sprites {
		if c.release != nil {
			c.release(image)
		} else {
			image.Deallocate()
		}
		delete(c.sprites, key)
	}
}

// spriteDraw is one sprite on the screen. center is in device pixels.
type spriteDraw struct {
	center sim.Point
	key    spriteKey
	scale  spriteScale
	// direction turns the sprite about its center, so that the +X axis of
	// the sprite points along direction. A zero direction does not turn the
	// sprite.
	direction sim.Point
}

// draw draws the sprite of d.key with its center near d.center.
func (c *spriteCache) draw(screen *ebiten.Image, d spriteDraw) {
	screen.DrawImage(c.sprite(d.scale, d.key), spriteOptions(d))
}

// spriteOptions returns the options that draw the sprite image of d. A
// sprite without a direction moves by whole pixels, so its center is at most
// half a pixel from d.center and the image is copied without resampling. A
// sprite with a direction turns about its center, and the center is on
// d.center. Linear filtering then keeps the turned edges smooth.
func spriteOptions(d spriteDraw) *ebiten.DrawImageOptions {
	options := &ebiten.DrawImageOptions{}
	side := d.key.side()
	resolution := d.key.resolution()
	options.GeoM.Scale(1/resolution, 1/resolution)
	if d.direction == (sim.Point{}) {
		options.GeoM.Translate(spriteOrigin(d.center, side))
		return options
	}
	half := float64(side) / 2
	options.GeoM.Translate(-half, -half)
	options.GeoM.Rotate(math.Atan2(d.direction.Y, d.direction.X))
	options.GeoM.Translate(d.center.X, d.center.Y)
	options.Filter = ebiten.FilterLinear
	return options
}

// spriteOrigin returns the top-left corner of a sprite image of the given
// side, so that the sprite center is nearest to center. The corner is on
// whole pixels. Ties round up, also for negative values, so sprites of
// different even sides with one center get the same center.
func spriteOrigin(center sim.Point, side int) (x, y float64) {
	half := float64(side) / 2
	return math.Floor(center.X - half + 0.5), math.Floor(center.Y - half + 0.5)
}

// buildSprite draws the layers of key with antialiasing in a new image. The
// image has key.resolution() pixels in each device pixel.
func buildSprite(key spriteKey) *ebiten.Image {
	resolution := key.resolution()
	side := int(float64(key.side()) * resolution)
	image := ebiten.NewImage(side, side)
	center := float32(side) / 2
	scale := float32(resolution)
	for _, layer := range key.layers {
		switch {
		case layer.radius <= 0:
		case layer.width == 0:
			vector.FillCircle(image, center, center, layer.radius*scale, rgb(layer.color), true)
		default:
			vector.StrokeCircle(image, center, center, layer.radius*scale, layer.width*scale, rgb(layer.color), true)
		}
	}
	if key.arrow.size != (arrowSize{}) {
		drawArrow(image, arrow{
			tip: sim.Point{X: float64(center), Y: float64(center)}, direction: sim.Point{X: 1},
			size: key.arrow.size, color: key.arrow.color, antialias: true, unit: key.arrow.unit * resolution,
		})
	}
	return image
}
