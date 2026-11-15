package main

import (
	"bytes"
	"image"
	"image/png"
)

// texture is one generated texture: its file name in web/textures and the
// function that draws it.
type texture struct {
	file string
	draw func() *image.Gray
}

// The textures. Each texture is gray, and the ride page multiplies it by
// the color of the surface, so the textures are light. Each texture tiles:
// its noise repeats with the size of the image.
var textures = []texture{
	{"concrete.png", drawConcrete},
	{"panel.png", drawPanel},
	{"fabric.png", drawFabric},
	{"dash.png", drawDash},
}

// encodePNG returns img as a PNG file.
func encodePNG(img image.Image) ([]byte, error) {
	var out bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestCompression}
	if err := encoder.Encode(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// The noise uses integer arithmetic only, so each machine gives the same
// pixels. Go can fuse a floating-point multiply and add on some machines,
// and that can change a rounded pixel value.

// hash returns a pseudorandom value for the integer point (x, y) and seed.
func hash(x, y, seed int) uint32 {
	// #nosec G115 -- The conversion only mixes bits.
	h := uint32(x)*0x8da6b343 ^ uint32(y)*0xd8163841 ^ uint32(seed)*0xcb1ab31f
	h ^= h >> 16
	h *= 0x7feb352d
	h ^= h >> 15
	h *= 0x846ca68b
	h ^= h >> 16
	return h
}

// unitScale is the fixed-point scale of the noise: unitScale is 1.
const unitScale = 1 << 16

// smooth returns the smoothstep of t/unitScale, scaled by unitScale.
func smooth(t int64) int64 {
	return t * t / unitScale * (3*unitScale - 2*t) / unitScale
}

// valueNoise returns smooth noise at pixel (x, y), from 0 to unitScale.
// The lattice has a value every cell pixels and repeats every period
// pixels, so the noise tiles an image of that size. cell must divide
// period.
func valueNoise(x, y, cell, period, seed int) int64 {
	cells := period / cell
	cx, cy := x/cell, y/cell
	lattice := func(i, j int) int64 {
		return int64(hash((cx+i)%cells, (cy+j)%cells, seed) % unitScale)
	}
	tx := smooth(int64(x%cell) * unitScale / int64(cell))
	ty := smooth(int64(y%cell) * unitScale / int64(cell))
	top := lattice(0, 0) + (lattice(1, 0)-lattice(0, 0))*tx/unitScale
	bottom := lattice(0, 1) + (lattice(1, 1)-lattice(0, 1))*tx/unitScale
	return top + (bottom-top)*ty/unitScale
}

// fractalNoise returns the sum of octaves of value noise at pixel (x, y),
// from the cell size largest down to 2 pixels, each with half the weight
// of the one before. The result is from 0 to unitScale.
func fractalNoise(x, y, largest, period, seed int) int64 {
	var sum, weights int64
	weight := int64(256)
	for cell := largest; cell >= 2; cell /= 2 {
		sum += valueNoise(x, y, cell, period, seed+cell) * weight
		weights += weight
		weight /= 2
	}
	return sum / weights
}

// grain returns a value from -amplitude to amplitude for pixel (x, y).
func grain(x, y, seed, amplitude int) int {
	return int(hash(x, y, seed)%uint32(2*amplitude+1)) - amplitude // #nosec G115 -- amplitude is small and positive.
}

// gray returns value clamped to a pixel value.
func gray(value int) uint8 {
	return uint8(min(255, max(0, value))) // #nosec G115 -- Clamped above.
}

// fill draws img with value(x, y) at each pixel.
func fill(size int, value func(x, y int) int) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			img.Pix[y*img.Stride+x] = gray(value(x, y))
		}
	}
	return img
}

// noiseAt returns fractal noise at pixel (x, y) scaled to -amplitude to
// amplitude.
func noiseAt(x, y, largest, period, seed, amplitude int) int {
	return int((fractalNoise(x, y, largest, period, seed) - unitScale/2) * int64(2*amplitude) / unitScale)
}

// drawConcrete draws cast concrete: soft blotches, a fine grain, and small
// dark pits. One repeat covers 4 m on the beams, the pillars, and the
// station decks.
func drawConcrete() *image.Gray {
	const size = 256
	return fill(size, func(x, y int) int {
		value := 222 + noiseAt(x, y, 64, size, 1, 26) + grain(x, y, 2, 7)
		if hash(x, y, 3)%61 == 0 {
			value -= 45
		}
		return value
	})
}

// drawPanel draws the pod shell: a light surface with a panel seam down
// the middle and a dark livery stripe across the middle, with a thin light
// line in it near each edge. The class color of the pod tints it. The
// stripe and the seam are symmetric about the middle, so the texture
// looks the same when a viewer reads it upside down or mirrored.
func drawPanel() *image.Gray {
	const size = 256
	return fill(size, func(x, y int) int {
		value := 246 + noiseAt(x, y, 32, size, 4, 4)
		switch {
		case y == 118 || y == 137:
			value = 200
		case y >= 116 && y < 140:
			value = 62 + grain(x, y, 5, 3)
		case x == 127 || x == 128:
			value -= 90
		case x == 126 || x == 129:
			value -= 30
		}
		return value
	})
}

// drawFabric draws a basket weave of seat fabric. Threads are 4 pixels
// wide, and each square of 4 by 4 pixels shows a thread across or along,
// in turn. A thread is lighter in its middle than at its edges.
func drawFabric() *image.Gray {
	const size = 128
	// profile is the shade across a thread, from its edge to its middle.
	profile := [4]int{-26, 4, 10, -6}
	return fill(size, func(x, y int) int {
		across := (x/4+y/4)%2 == 0
		var position, thread int
		if across {
			position, thread = y%4, y/4
		} else {
			position, thread = x%4, x/4+size
		}
		return 210 + profile[position] + grain(thread, 0, 6, 8) + grain(x, y, 7, 6) + noiseAt(x, y, 32, size, 8, 6)
	})
}

// drawDash draws the matte grain of the dashboard and the cabin trim.
func drawDash() *image.Gray {
	const size = 128
	return fill(size, func(x, y int) int {
		return 228 + noiseAt(x, y, 32, size, 9, 10) + grain(x, y, 10, 7)
	})
}
