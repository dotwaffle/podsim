package view

import (
	"image"
	"math"
)

const (
	minimumWidth  = 1100
	minimumHeight = 760
)

type displayLayout struct {
	width, height  int
	unit           float64
	extraX, extraY float64
	mapViewport    image.Rectangle
}

type layoutInput struct {
	outsideWidth, outsideHeight int
	deviceScale                 float64
}

func newDisplayLayout(input layoutInput) displayLayout {
	outsideWidth, outsideHeight, deviceScale := input.outsideWidth, input.outsideHeight, input.deviceScale
	if outsideWidth <= 0 {
		outsideWidth = minimumWidth
	}
	if outsideHeight <= 0 {
		outsideHeight = minimumHeight
	}
	if deviceScale <= 0 || math.IsNaN(deviceScale) || math.IsInf(deviceScale, 0) {
		deviceScale = 1
	}
	cssScale := min(1, min(float64(outsideWidth)/minimumWidth, float64(outsideHeight)/minimumHeight))
	unit := deviceScale * cssScale
	physicalWidth := int(math.Ceil(float64(outsideWidth) * deviceScale))
	physicalHeight := int(math.Ceil(float64(outsideHeight) * deviceScale))
	extraX := float64(physicalWidth) - minimumWidth*unit
	extraY := float64(physicalHeight) - minimumHeight*unit
	return displayLayout{
		width: physicalWidth, height: physicalHeight, unit: unit,
		extraX: extraX, extraY: extraY,
		mapViewport: image.Rect(
			int(math.Round(24*unit)), int(math.Round(136*unit)),
			int(math.Round(772*unit+extraX)), int(math.Round(520*unit+extraY)),
		),
	}
}

func (layout displayLayout) x(value float64) float64      { return value * layout.unit }
func (layout displayLayout) y(value float64) float64      { return value * layout.unit }
func (layout displayLayout) right(value float64) float64  { return value*layout.unit + layout.extraX }
func (layout displayLayout) bottom(value float64) float64 { return value*layout.unit + layout.extraY }

func (layout displayLayout) labelPosition(x, y float64) (float64, float64) {
	if x >= 796 {
		x = layout.right(x)
	} else {
		x = layout.x(x)
	}
	if y >= 531 {
		y = layout.bottom(y)
	} else {
		y = layout.y(y)
	}
	return x, y
}
