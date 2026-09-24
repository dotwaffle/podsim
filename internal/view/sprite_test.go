package view

import (
	"math"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/dotwaffle/podsim/internal/sim"
)

// fakeSprites returns a sprite cache that does not draw. It counts the
// images that it builds and records the images that it releases.
func fakeSprites(t *testing.T) (cache *spriteCache, built *int, released map[*ebiten.Image]bool) {
	t.Helper()
	built = new(int)
	released = make(map[*ebiten.Image]bool)
	var all []*ebiten.Image
	cache = &spriteCache{
		build: func(spriteKey) *ebiten.Image {
			*built++
			image := ebiten.NewImage(1, 1)
			all = append(all, image)
			return image
		},
		release: func(image *ebiten.Image) { released[image] = true },
	}
	t.Cleanup(func() {
		for _, image := range all {
			image.Deallocate()
		}
	})
	return cache, built, released
}

func TestSpriteCache(t *testing.T) {
	t.Parallel()
	first := spriteScale{unit: 1, markerRadius: 10}
	disc := discSprite(4, foreground)
	ring := ringSprite(9, 1.5, foreground)
	tests := []struct {
		name string
		// key and scale are the key and the scale of the second request.
		// The first request is always disc at first.
		key          spriteKey
		scale        spriteScale
		same         bool
		wantBuilt    int
		wantReleased bool
		wantCached   int
	}{
		{name: "same key and scale", key: disc, scale: first, same: true, wantBuilt: 1, wantCached: 1},
		{name: "other key", key: ring, scale: first, wantBuilt: 2, wantCached: 2},
		{name: "unit change", key: disc, scale: spriteScale{unit: 2, markerRadius: 10}, wantBuilt: 2, wantReleased: true, wantCached: 1},
		{name: "marker radius change", key: disc, scale: spriteScale{unit: 1, markerRadius: 6}, wantBuilt: 2, wantReleased: true, wantCached: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			cache, built, released := fakeSprites(t)
			old := cache.sprite(first, disc)
			got := cache.sprite(test.scale, test.key)
			if (got == old) != test.same {
				t.Errorf("same image = %t, want %t", got == old, test.same)
			}
			if *built != test.wantBuilt {
				t.Errorf("built %d images, want %d", *built, test.wantBuilt)
			}
			if released[old] != test.wantReleased {
				t.Errorf("released old image = %t, want %t", released[old], test.wantReleased)
			}
			if released[got] {
				t.Error("released the returned image")
			}
			if len(cache.sprites) != test.wantCached {
				t.Errorf("cache keeps %d images, want %d", len(cache.sprites), test.wantCached)
			}
		})
	}
}

func TestSpriteCacheReleasesAllOnScaleChange(t *testing.T) {
	t.Parallel()
	cache, _, released := fakeSprites(t)
	scale := spriteScale{unit: 1, markerRadius: 10}
	var old []*ebiten.Image
	for _, key := range []spriteKey{discSprite(4, foreground), ringSprite(9, 1.5, amber), markerSprite(10, 1)} {
		old = append(old, cache.sprite(scale, key))
	}
	cache.sprite(spriteScale{unit: 2, markerRadius: 20}, discSprite(8, foreground))
	for i, image := range old {
		if !released[image] {
			t.Errorf("image %d not released", i)
		}
	}
	if len(cache.sprites) != 1 {
		t.Errorf("cache keeps %d images, want 1", len(cache.sprites))
	}
}

func TestSpriteSide(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		key  spriteKey
		want int
	}{
		{name: "disc", key: discSprite(4, foreground), want: 10},
		{name: "ring adds half the line", key: ringSprite(13, 2, foreground), want: 30},
		{name: "fractional size rounds up", key: ringSprite(9, 1.5, foreground), want: 22},
		{name: "marker outline", key: markerSprite(10, 1), want: 24},
		{name: "pod with a hole", key: podMarkSprite(purposeEmpty, 2), want: 18},
		{name: "side is even", key: ringSprite(11.25, 1.875, foreground), want: 28},
		{name: "route arrow", key: arrowSprite(arrow{size: routeArrowSize, unit: 1}), want: 20},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := test.key.side(); got != test.want {
				t.Errorf("side = %d, want %d", got, test.want)
			}
		})
	}
}

// TestSpriteOrigin checks that a sprite center is at most half a device
// pixel from the center of the vector shape that it replaces.
func TestSpriteOrigin(t *testing.T) {
	t.Parallel()
	for _, side := range []int{10, 20, 22, 30} {
		for _, center := range []sim.Point{{X: 0, Y: 0}, {X: 100.25, Y: 50.5}, {X: 333.49, Y: 12.51}, {X: 7.99, Y: 8.01}, {X: -2.5, Y: -0.5}} {
			x, y := spriteOrigin(center, side)
			if x != math.Trunc(x) || y != math.Trunc(y) {
				t.Errorf("side %d center %v: origin (%v, %v) is not on whole pixels", side, center, x, y)
			}
			half := float64(side) / 2
			if dx, dy := x+half-center.X, y+half-center.Y; math.Abs(dx) > 0.5 || math.Abs(dy) > 0.5 {
				t.Errorf("side %d center %v: sprite center is (%v, %v) away", side, center, dx, dy)
			}
		}
	}
}

// TestConcentricSprites checks that the sprites that the map draws around
// one pod or berth get the same center at each display unit. The Waiting
// and Selected rings then stay centered on the pod.
func TestConcentricSprites(t *testing.T) {
	t.Parallel()
	centers := []sim.Point{{X: 100.3, Y: 200.7}, {X: 0.5, Y: -2.5}, {X: -3.5, Y: 7.5}}
	for _, unit := range []float64{0.8026, 1, 1.25, 1.6053, 2, 3} {
		keys := []spriteKey{
			podMarkSprite(purposeIdle, unit),
			podMarkSprite(purposeEmpty, unit),
			ringSprite(11*unit, 2*unit, amber),
			ringSprite(9*unit, 1.5*unit, foreground),
			ringSprite(7*unit, 1.5*unit, foreground),
			ringSprite(berthRingRadius*unit, 2*unit, foreground),
			markerSprite(6*unit, unit),
		}
		for _, center := range centers {
			var want sim.Point
			for i, key := range keys {
				side := key.side()
				if side%2 != 0 {
					t.Errorf("unit %v key %d: side %d is odd", unit, i, side)
				}
				x, y := spriteOrigin(center, side)
				got := sim.Point{X: x + float64(side)/2, Y: y + float64(side)/2}
				if i == 0 {
					want = got
				} else if got != want {
					t.Errorf("unit %v center %v key %d: sprite center %v, want %v", unit, center, i, got, want)
				}
			}
		}
	}
}

func TestSpriteOptions(t *testing.T) {
	t.Parallel()
	t.Run("without direction", func(t *testing.T) {
		t.Parallel()
		center := sim.Point{X: 40.3, Y: 12.6}
		key := ringSprite(9, 1.5, foreground)
		options := spriteOptions(spriteDraw{center: center, key: key})
		wantX, wantY := spriteOrigin(center, key.side())
		if x, y := options.GeoM.Apply(0, 0); x != wantX || y != wantY {
			t.Errorf("origin = (%v, %v), want (%v, %v)", x, y, wantX, wantY)
		}
		side := float64(key.side())
		if x, y := options.GeoM.Apply(side, side); x != wantX+side || y != wantY+side {
			t.Errorf("far corner = (%v, %v), want (%v, %v)", x, y, wantX+side, wantY+side)
		}
		if options.Filter != ebiten.FilterNearest {
			t.Errorf("filter = %v, want nearest", options.Filter)
		}
	})
	// A turned arrow sprite must put the tip and the leg ends where
	// drawArrow puts them on the screen. The arrow image has
	// turnedResolution pixels in each device pixel.
	for _, test := range []struct {
		name      string
		direction sim.Point
	}{
		{name: "arrow to the right", direction: sim.Point{X: 1}},
		{name: "arrow down and right", direction: sim.Point{X: 3, Y: 4}},
		{name: "arrow to the left", direction: sim.Point{X: -2, Y: 0.5}},
		{name: "arrow up", direction: sim.Point{Y: -1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			onScreen := arrow{tip: sim.Point{X: 50.3, Y: 20.6}, direction: test.direction, size: routeArrowSize, unit: 1.25}
			d, ok := onScreen.spriteDraw(spriteScale{unit: 1.25})
			if !ok {
				t.Fatal("arrow has no sprite")
			}
			options := spriteOptions(d)
			if options.Filter != ebiten.FilterLinear {
				t.Errorf("filter = %v, want linear", options.Filter)
			}
			half := float64(d.key.side()*turnedResolution) / 2
			inSprite := arrow{tip: sim.Point{X: half, Y: half}, direction: sim.Point{X: 1}, size: routeArrowSize, unit: 1.25 * turnedResolution}
			spriteEnds, _ := inSprite.legEnds()
			screenEnds, _ := onScreen.legEnds()
			points := [][2]sim.Point{{inSprite.tip, onScreen.tip}, {spriteEnds[0], screenEnds[0]}, {spriteEnds[1], screenEnds[1]}}
			for _, point := range points {
				x, y := options.GeoM.Apply(point[0].X, point[0].Y)
				if math.Abs(x-point[1].X) > 1e-9 || math.Abs(y-point[1].Y) > 1e-9 {
					t.Errorf("sprite point %v goes to (%v, %v), want %v", point[0], x, y, point[1])
				}
			}
		})
	}
}

func TestArrowSpriteDraw(t *testing.T) {
	t.Parallel()
	scale := spriteScale{unit: 2, markerRadius: 10}
	if _, ok := (arrow{tip: sim.Point{X: 5}, size: routeArrowSize, unit: 2}).spriteDraw(scale); ok {
		t.Error("arrow without a direction has a sprite")
	}
	a := arrow{tip: sim.Point{X: 5, Y: 6}, direction: sim.Point{X: 0, Y: 3}, size: routeArrowSize, color: amber, antialias: true, unit: 2}
	d, ok := a.spriteDraw(scale)
	if !ok {
		t.Fatal("arrow has no sprite")
	}
	want := spriteDraw{center: a.tip, key: arrowSprite(arrow{size: routeArrowSize, color: amber, unit: 2}), scale: scale, direction: a.direction}
	if d != want {
		t.Errorf("sprite draw = %+v, want %+v", d, want)
	}
}

func TestPodMarkSprite(t *testing.T) {
	t.Parallel()
	for _, purpose := range []podPurpose{purposeIdle, purposePickup, purposePassengers, purposeParking, purposeRedistribution, purposeEmpty} {
		key := podMarkSprite(purpose, 2)
		if key.layers[0] != (circleLayer{radius: podRadius * 2, color: purpose.color()}) {
			t.Errorf("%s: body = %+v", purpose.label(), key.layers[0])
		}
		hole := key.layers[1].radius > 0
		if hole != purpose.emptyMove() {
			t.Errorf("%s: hole = %t, want %t", purpose.label(), hole, purpose.emptyMove())
		}
		if hole && key.layers[1].color != track {
			t.Errorf("%s: hole color = %06x, want the lane color", purpose.label(), key.layers[1].color)
		}
	}
}
