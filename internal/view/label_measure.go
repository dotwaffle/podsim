package view

import (
	"strings"

	"github.com/hajimehoshi/ebiten/v2/text/v2"
)

const (
	labelMeasureEntries = 1024
	labelMeasureBytes   = 512
)

type labelMeasureKey struct {
	value             string
	size, lineSpacing float64
}

type labelDimensions struct{ width, height float64 }

type labelMeasureCache struct {
	font                 *text.GoTextFaceSource
	values               map[labelMeasureKey]labelDimensions
	observations, misses int
}

// observe keeps full caches with useful entries. A window with more than
// three quarters misses clears obsolete entries for a new working set.
func (c *labelMeasureCache) observe(hit bool) {
	if len(c.values) < labelMeasureEntries {
		return
	}
	c.observations++
	if !hit {
		c.misses++
	}
	if c.observations == labelMeasureEntries {
		if c.misses*4 > labelMeasureEntries*3 {
			clear(c.values)
		}
		c.observations, c.misses = 0, 0
	}
}

// measureLabel caches text dimensions. Position, color, and visibility remain
// outside the cache. labelFace sets only the source and size of each face.
func (g *Game) measureLabel(value label) (width, height float64) {
	cache := g.labelMeasures
	if cache != nil && cache.font != g.font {
		clear(cache.values)
		cache.observations, cache.misses = 0, 0
		cache.font = g.font
	}
	if len(value.value) > labelMeasureBytes {
		return text.Measure(value.value, g.labelFace(value), value.lineSpacing)
	}
	key := labelMeasureKey{value: value.value, size: g.labelFaceSize(value), lineSpacing: value.lineSpacing}
	if cache != nil {
		if dimensions, ok := cache.values[key]; ok {
			cache.observe(true)
			return dimensions.width, dimensions.height
		}
		cache.observe(false)
	}
	width, height = text.Measure(value.value, g.labelFace(value), value.lineSpacing)
	if cache == nil {
		cache = &labelMeasureCache{font: g.font, values: make(map[labelMeasureKey]labelDimensions)}
		g.labelMeasures = cache
	}
	if len(cache.values) >= labelMeasureEntries {
		return width, height
	}
	// A short substring can share a much larger backing string.
	key.value = strings.Clone(key.value)
	cache.values[key] = labelDimensions{width: width, height: height}
	return width, height
}
