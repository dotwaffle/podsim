package view

import (
	"fmt"
	"image"
	"testing"

	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
)

// headlessImageLimit is the image limit in headless Chrome. Its SwiftShader
// WebGL reports a maximum texture size of 8192 pixels.
const headlessImageLimit = 8192 - 1

func TestImageSideLimit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		maxImageSize, want int
	}{
		{maxImageSize: 8192, want: 8191},
		{maxImageSize: 16384, want: 16383},
		{maxImageSize: 0, want: 0},
		{maxImageSize: -1, want: 0},
	}
	for _, test := range tests {
		if got := imageSideLimit(test.maxImageSize); got != test.want {
			t.Errorf("imageSideLimit(%d) = %d, want %d", test.maxImageSize, got, test.want)
		}
	}
}

// TestVectorAntialias checks the largest stencil image of the Ebiten vector
// package, and when antialiased vector drawing keeps it within the limit. A
// 5120 by 2880 screen gives the 10240 pixel stencil image that stopped the
// game in headless Chrome.
func TestVectorAntialias(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		size      image.Point
		limit     int
		stencil   int
		antialias bool
	}{
		{name: "minimum window", size: image.Pt(1100, 760), limit: headlessImageLimit, stencil: vectorStencilMinimum, antialias: true},
		{name: "laptop", size: image.Pt(1366, 768), limit: headlessImageLimit, stencil: vectorStencilMinimum, antialias: true},
		{name: "1920 by 930 at 1.5", size: image.Pt(2880, 1395), limit: headlessImageLimit, stencil: 5760, antialias: true},
		{name: "widest", size: image.Pt(4095, 2304), limit: headlessImageLimit, stencil: 8190, antialias: true},
		{name: "one pixel too wide", size: image.Pt(4096, 2304), limit: headlessImageLimit, stencil: 8192},
		{name: "2560 by 1440 at 2", size: image.Pt(5120, 2880), limit: headlessImageLimit, stencil: 10240},
		{name: "2560 by 1440 at 2 on a larger GPU", size: image.Pt(5120, 2880), limit: 16383, stencil: 10240, antialias: true},
		{name: "unknown limit", size: image.Pt(5120, 2880), stencil: 10240, antialias: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := vectorStencilSide(test.size, true); got != test.stencil {
				t.Errorf("vectorStencilSide(%v, true) = %d, want %d", test.size, got, test.stencil)
			}
			if got := vectorAntialias(test.size, test.limit); got != test.antialias {
				t.Errorf("vectorAntialias(%v, %d) = %t, want %t", test.size, test.limit, got, test.antialias)
			}
		})
	}
}

// TestMapImagesFitImageLimit checks the images that the map can make at a
// large device scale: the cached base layer of a dense network, and the
// stencil images of the Ebiten vector package for the lanes on the screen
// and on the cached layer. No side is larger than the image limit, and the
// example network keeps its antialiasing where the limit allows it. The
// lanes, the lane arrows and the route line get their antialiasing from
// the style. The node dots and the scale bar read style.antialias directly,
// so this test does not cover them. The test does not make images and does
// not draw.
func TestMapImagesFitImageLimit(t *testing.T) {
	t.Parallel()
	networks := []struct {
		name    string
		network sim.Network
	}{
		{name: "example", network: sim.Example()},
		{name: "London", network: scenarios.London().Network},
	}
	tests := []struct {
		layout layoutInput
		limit  int
		// antialias is true when the example network is antialiased.
		antialias bool
	}{
		{layout: layoutInput{outsideWidth: 1366, outsideHeight: 768, deviceScale: 1}, limit: headlessImageLimit, antialias: true},
		// The 4480 pixel screen is too wide for antialiasing, but the 3864
		// pixel map viewport is not. The style must use the screen.
		{layout: layoutInput{outsideWidth: 2560, outsideHeight: 1440, deviceScale: 1.75}, limit: headlessImageLimit},
		{layout: layoutInput{outsideWidth: 2560, outsideHeight: 1440, deviceScale: 2}, limit: headlessImageLimit},
		{layout: layoutInput{outsideWidth: 2560, outsideHeight: 1440, deviceScale: 2}, limit: 16383, antialias: true},
		{layout: layoutInput{outsideWidth: 3840, outsideHeight: 2160, deviceScale: 2}, limit: headlessImageLimit},
	}
	for _, network := range networks {
		for _, test := range tests {
			name := fmt.Sprintf("%s %dx%d at %g limit %d", network.name, test.layout.outsideWidth, test.layout.outsideHeight, test.layout.deviceScale, test.limit)
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				game := zoomedGame(network.network, test.layout, 1)
				game.imageLimit = test.limit
				style := game.currentNetworkStyle(game.collapsedStationMarkers())
				if want := test.antialias && style.detailed; style.antialias != want {
					t.Errorf("antialias = %t, want %t", style.antialias, want)
				}
				if got := style.routeStroke(accent); got.antialias != style.antialias {
					t.Errorf("route stroke antialias = %t, want %t", got.antialias, style.antialias)
				}
				screen := image.Pt(game.layout.width, game.layout.height)
				if got := vectorStencilSide(screen, style.antialias); got > test.limit {
					t.Errorf("stencil image for the %v screen = %d pixels, want at most %d", screen, got, test.limit)
				}
				for _, lane := range game.network.Lanes {
					if got := style.laneStroke(lane); got.antialias != style.antialias {
						t.Fatalf("lane %s stroke antialias = %t, want %t", lane.ID, got.antialias, style.antialias)
					}
					if a, ok := style.laneArrow(lane, game.laneGeometry(lane, style.detailed)); ok && a.antialias != style.antialias {
						t.Fatalf("lane %s arrow antialias = %t, want %t", lane.ID, a.antialias, style.antialias)
					}
				}
				if style.detailed {
					return
				}
				area := baseLayerArea(game.layout.mapViewport, style.imageLimit)
				if area.Dx() > test.limit || area.Dy() > test.limit {
					t.Errorf("cached layer %v = %dx%d pixels, want at most %d", area, area.Dx(), area.Dy(), test.limit)
				}
				if !game.layout.mapViewport.In(area) {
					t.Errorf("cached layer %v does not cover the viewport %v", area, game.layout.mapViewport)
				}
				if got := vectorStencilSide(area.Size(), style.antialias); got > test.limit {
					t.Errorf("stencil image for the cached layer = %d pixels, want at most %d", got, test.limit)
				}
			})
		}
	}
}
