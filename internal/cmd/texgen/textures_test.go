package main

import (
	"bytes"
	"flag"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

var updateTextures = flag.Bool("update-textures", false, "rewrite the files in web/textures")

// textureDirectory is web/textures, relative to this package.
var textureDirectory = filepath.Join("..", "..", "..", "web", "textures")

// TestTextureFiles fails when a file in web/textures does not have the
// pixels of the generator output. It compares pixels, not bytes, so a
// change of the PNG compressor in a new Go release does not fail it.
func TestTextureFiles(t *testing.T) {
	if *updateTextures {
		if err := writeTextures(textureDirectory); err != nil {
			t.Fatal(err)
		}
	}
	total := 0
	for _, texture := range textures {
		data, err := os.ReadFile(filepath.Join(textureDirectory, texture.file))
		if err != nil {
			t.Fatalf("read the texture, or run go test -run TestTextureFiles -update-textures: %v", err)
		}
		total += len(data)
		decoded, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("%s: %v", texture.file, err)
		}
		got, ok := decoded.(*image.Gray)
		want := texture.draw()
		if !ok || got.Rect != want.Rect || !bytes.Equal(got.Pix, want.Pix) {
			t.Fatalf("web/textures/%s is not the generator output; run go test -run TestTextureFiles -update-textures", texture.file)
		}
	}
	// The page loads all textures, so they stay small.
	if total > 300*1024 {
		t.Fatalf("the textures have %d bytes, more than 300 KiB", total)
	}
}

// TestTexturesTile checks that each texture repeats with no seam: the step
// across an edge, from the last column to the first or from the last row
// to the first, is no larger than a step inside the texture. The weave of
// the fabric repeats every 8 pixels, and its steps depend on the place in
// that repeat, so the steps inside are those at the place of the edge: from
// pixel 8k+7 to pixel 8k+8.
func TestTexturesTile(t *testing.T) {
	for _, texture := range textures {
		img := texture.draw()
		size := img.Rect.Dx()
		if size != img.Rect.Dy() || size > 256 || size&(size-1) != 0 {
			t.Fatalf("%s is %v, want a square with a power of 2 side of at most 256", texture.file, img.Rect)
		}
		pixel := func(x, y int) int { return int(img.Pix[y*img.Stride+x]) }
		step := func(a, b int) int { return max(a-b, b-a) }
		// edge and inside are the sums of the steps across the edge and
		// between the other neighbors, for columns and for rows.
		var edge, inside [2]int
		pairs := 0
		for i := range size {
			edge[0] += step(pixel(size-1, i), pixel(0, i))
			edge[1] += step(pixel(i, size-1), pixel(i, 0))
			for j := 7; j < size-1; j += 8 {
				inside[0] += step(pixel(j, i), pixel(j+1, i))
				inside[1] += step(pixel(i, j), pixel(i, j+1))
				if i == 0 {
					pairs++
				}
			}
		}
		for axis, name := range []string{"left and right", "top and bottom"} {
			edgeMean := float64(edge[axis]) / float64(size)
			insideMean := float64(inside[axis]) / float64(size*pairs)
			if edgeMean > 1.25*insideMean+0.5 {
				t.Errorf("%s: the mean step across the %s edges is %.2f, and inside it is %.2f", texture.file, name, edgeMean, insideMean)
			}
		}
	}
}

// The page multiplies the textures by the surface colors, so a dark
// texture makes the scene dark.
func TestTexturesAreLight(t *testing.T) {
	for _, texture := range textures {
		img := texture.draw()
		sum := 0
		for _, value := range img.Pix {
			sum += int(value)
		}
		if mean := float64(sum) / float64(len(img.Pix)); mean < 180 {
			t.Errorf("%s has a mean of %.1f, want at least 180", texture.file, mean)
		}
	}
}
