package view

import "image"

// imageSideLimit returns the largest side in pixels of an image that the
// view can give to Ebiten. maxImageSize is the value of ebiten.MaxImageSize,
// which comes from the GPU. Ebiten keeps one pixel of padding next to an
// image on its atlas, so the limit is one pixel less. Ebiten reports 0
// before the game starts, for example in tests. The limit is then 0, which
// means that the limit is not known.
func imageSideLimit(maxImageSize int) int {
	return max(0, maxImageSize-1)
}

// vectorStencilMinimum is the smallest side in pixels that the Ebiten vector
// package gives to its stencil image.
const vectorStencilMinimum = 4093

// vectorStencilSide returns the largest side in pixels of the stencil image
// that the Ebiten vector package can make for fills and strokes on a
// destination image of the given size. For a sub-image, the destination is
// the full image. The package puts the stencils of all queued fills side by
// side in one image. That image can become as large as the destination. With
// antialiasing, it can become twice as wide. Ebiten 2.10 does not keep that
// image within ebiten.MaxImageSize, so a stencil image larger than the limit
// stops the game.
func vectorStencilSide(size image.Point, antialias bool) int {
	width := size.X
	if antialias {
		width *= 2
	}
	return max(vectorStencilMinimum, width, size.Y)
}

// vectorAntialias reports whether antialiased vector drawing on a
// destination image of the given size keeps the stencil image of the Ebiten
// vector package within limit. A limit of 0 is not known, and then
// vectorAntialias reports true.
func vectorAntialias(size image.Point, limit int) bool {
	return limit <= 0 || vectorStencilSide(size, true) <= limit
}
