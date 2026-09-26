package view

import (
	"image"
	"math"
)

const (
	minimumWidth  = 1100
	minimumHeight = 728
	// mapLabelMinimum is the smallest size in CSS pixels of a map label.
	mapLabelMinimum = 10
)

type displayLayout struct {
	width, height int
	unit          float64
	// deviceScale is the number of physical pixels in one CSS pixel. Map
	// labels use it in place of unit. See mapLabelSize.
	deviceScale    float64
	extraX, extraY float64
	mapViewport    image.Rectangle
}

type layoutInput struct {
	outsideWidth, outsideHeight int
	deviceScale                 float64
	// imageLimit is the largest side in pixels of the screen image. A
	// limit of 0 is not known. See imageSideLimit and screenScale.
	imageLimit int
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
	deviceScale = screenScale(screenScaleInput{side: max(outsideWidth, outsideHeight), deviceScale: deviceScale, limit: input.imageLimit})
	cssScale := min(1, min(float64(outsideWidth)/minimumWidth, float64(outsideHeight)/minimumHeight))
	unit := deviceScale * cssScale
	physicalWidth := int(math.Ceil(float64(outsideWidth) * deviceScale))
	physicalHeight := int(math.Ceil(float64(outsideHeight) * deviceScale))
	extraX := float64(physicalWidth) - minimumWidth*unit
	extraY := float64(physicalHeight) - minimumHeight*unit
	return displayLayout{
		width: physicalWidth, height: physicalHeight, unit: unit, deviceScale: deviceScale,
		extraX: extraX, extraY: extraY,
		mapViewport: image.Rect(
			int(math.Round(24*unit)), int(math.Round(104*unit)),
			int(math.Round(772*unit+extraX)), int(math.Round(488*unit+extraY)),
		),
	}
}

// screenScaleInput holds the values for screenScale.
type screenScaleInput struct {
	// side is the longer side of the window in CSS pixels.
	side        int
	deviceScale float64
	// limit is the largest side in pixels of the screen image. A limit of
	// 0 is not known.
	limit int
}

// screenScale returns the number of screen image pixels in one CSS pixel. It
// is the device scale, but when the screen image at the device scale would be
// larger than the limit, it is smaller, so that the image fits. Ebiten makes
// its screen image with the size that Layout returns, and a larger image stops
// Ebiten before the view draws. Ebiten then scales the screen image to the
// window. The limit is one pixel less than ebiten.MaxImageSize, because the
// stencil image of the Ebiten vector package can be as wide as the screen,
// and Ebiten adds one pixel of padding to that image. See imageSideLimit and
// vectorStencilSide. A limit of 0 does not change the device scale.
func screenScale(input screenScaleInput) float64 {
	scale, side, limit := input.deviceScale, float64(input.side), float64(input.limit)
	if input.limit <= 0 || math.Ceil(side*scale) <= limit {
		return scale
	}
	scale = limit / side
	// The division can round up, and then the image is one pixel too large.
	for math.Ceil(side*scale) > limit {
		scale = math.Nextafter(scale, 0)
	}
	return scale
}

// mapLabelSize returns the size in physical pixels of a map label of size CSS
// pixels. In a window smaller than the minimum window, the rest of the view
// becomes smaller, but a map label keeps its size. A map label is also never
// smaller than mapLabelMinimum CSS pixels. Thus station names, station counts
// and pod labels stay readable on a short laptop screen. Other text on the
// map, such as the connection notice, follows unit.
func (layout displayLayout) mapLabelSize(size float64) float64 {
	return max(size, mapLabelMinimum) * layout.deviceScale
}

func (layout displayLayout) x(value float64) float64      { return value * layout.unit }
func (layout displayLayout) y(value float64) float64      { return value * layout.unit }
func (layout displayLayout) right(value float64) float64  { return value*layout.unit + layout.extraX }
func (layout displayLayout) bottom(value float64) float64 { return value*layout.unit + layout.extraY }

// movesDown reports if an item at x, y in design units moves down with the
// bottom edge of a tall window. The bottom panel moves down. In the right
// panel, the pod selector and all controls below it also move down as one
// group. So the inspector and the Orders panel above them get the extra
// height, and no gap opens between the controls.
func movesDown(x, y float64) bool {
	return y >= 497 || x >= 796 && y >= podSelectorTop
}

func (layout displayLayout) labelPosition(x, y float64) (float64, float64) {
	moves := movesDown(x, y)
	if x >= 796 {
		x = layout.right(x)
	} else {
		x = layout.x(x)
	}
	if moves {
		y = layout.bottom(y)
	} else {
		y = layout.y(y)
	}
	return x, y
}
