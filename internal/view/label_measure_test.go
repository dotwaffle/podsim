package view

import (
	"bytes"
	"fmt"
	"image"
	"math"
	"strings"
	"testing"

	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"golang.org/x/image/font/gofont/gobold"
)

// originalLabelBounds preserves the uncached bounds calculation and face
// construction, including separate CSS map-label and display-unit sizes.
func originalLabelBounds(g *Game, value label) image.Rectangle {
	size := value.size * g.layout.unit
	if value.mapLabel {
		size = g.layout.mapLabelSize(value.size)
	}
	face := &text.GoTextFace{Source: g.font, Size: size}
	width, height := text.Measure(value.value, face, value.lineSpacing)
	return image.Rect(int(math.Floor(value.x)), int(math.Floor(value.y)),
		int(math.Ceil(value.x+width)), int(math.Ceil(value.y+height)))
}

func TestLabelMeasuresMatchOriginalBounds(t *testing.T) {
	t.Parallel()
	layouts := []layoutInput{
		{outsideWidth: 1100, outsideHeight: 728, deviceScale: 1},
		{outsideWidth: 1920, outsideHeight: 1080, deviceScale: 2},
		{outsideWidth: 1366, outsideHeight: 610, deviceScale: 1.5},
		{outsideWidth: 800, outsideHeight: 560, deviceScale: 1.25},
	}
	values := []label{
		{value: "Paddington  3/3", size: 10, mapLabel: true},
		{value: "133\nCHX", size: 11, lineSpacing: 16, mapLabel: true},
		{value: "Queue 12", size: 9, mapLabel: true},
		{value: "Å Ω 東京", size: 12},
		{value: "", size: 10},
		{value: "Zero size", size: 0},
	}
	game := journeyTestGame(t, 2)
	for _, input := range layouts {
		game.layoutFor(input)
		for _, value := range values {
			spacing := value.lineSpacing
			for _, position := range []float64{-14.01, 0, 35.99, 210.125} {
				value.x, value.y = position, position/3
				for _, spacing := range []float64{spacing, spacing + 3.25} {
					value.lineSpacing = spacing
					for range 2 {
						if got, want := game.labelBounds(value), originalLabelBounds(game, value); got != want {
							t.Fatalf("layout %+v label %+v: got %v, want %v", input, value, got, want)
						}
					}
				}
			}
		}
	}
}

func TestLabelMeasureHitsKeepPlacementFresh(t *testing.T) {
	t.Parallel()
	game := journeyTestGame(t, 2)
	value := label{value: "01\nACT", size: 11, lineSpacing: 16, mapLabel: true}
	first := game.labelBounds(value)
	for _, position := range []float64{-0.25, 15.25, 150.75} {
		value.x, value.y, value.color, value.physical = position, position*2, amber, true
		got := game.labelBounds(value)
		if got != originalLabelBounds(game, value) || got == first {
			t.Fatalf("cached placement = %v for %+v", got, value)
		}
	}
	if len(game.labelMeasures.values) != 1 {
		t.Fatal("placement or color created a new dimension entry")
	}
}

func TestLabelMeasureFontReplacementAndOversizedBypass(t *testing.T) {
	t.Parallel()
	game := journeyTestGame(t, 2)
	value := label{value: "Wider widths: MW", size: 16}
	first := game.labelBounds(value)
	font, err := text.NewGoTextFaceSource(bytes.NewReader(gobold.TTF))
	if err != nil {
		t.Fatal(err)
	}
	game.font = font
	game.labelMeasures.observations, game.labelMeasures.misses = 17, 13
	oversized := label{value: strings.Repeat("W", labelMeasureBytes+1), size: 12}
	if got := game.labelBounds(oversized); got != originalLabelBounds(game, oversized) {
		t.Fatal("oversized bypass changed bounds")
	}
	if game.labelMeasures.font != font || len(game.labelMeasures.values) != 0 {
		t.Fatal("font replacement retained old entries during bypass")
	}
	if game.labelMeasures.observations != 0 || game.labelMeasures.misses != 0 {
		t.Fatal("font replacement retained the old admission window")
	}
	if got := game.labelBounds(value); got != originalLabelBounds(game, value) || got == first {
		t.Fatalf("font replacement bounds = %v, old %v", got, first)
	}
}

func TestLabelMeasureOversizedWorkingSetRetainsUsefulEntries(t *testing.T) {
	t.Parallel()
	game := journeyTestGame(t, 2)
	values := make([]label, 1500)
	for index := range values {
		values[index] = label{value: fmt.Sprintf("Pod %04d\nACT", index), size: 11, lineSpacing: 16}
	}
	for pass := range 6 {
		for _, value := range values {
			if got := game.labelBounds(value); got != originalLabelBounds(game, value) {
				t.Fatalf("pass %d: bounds changed for %q", pass, value.value)
			}
		}
		key := labelMeasureKey{value: values[0].value, size: game.labelFaceSize(values[0]), lineSpacing: 16}
		if _, ok := game.labelMeasures.values[key]; !ok {
			t.Fatalf("pass %d: recurring working set discarded its useful first entry", pass)
		}
		if len(game.labelMeasures.values) != labelMeasureEntries {
			t.Fatalf("pass %d: cache has %d entries", pass, len(game.labelMeasures.values))
		}
	}
}

func TestLabelMeasureAdmissionRecoversAfterSceneChange(t *testing.T) {
	t.Parallel()
	game := journeyTestGame(t, 2)
	for index := range labelMeasureEntries {
		game.labelBounds(label{value: fmt.Sprintf("Old station %04d", index), size: 10})
	}
	for index := range labelMeasureEntries {
		value := label{value: fmt.Sprintf("New station %04d", index), size: 12}
		if got := game.labelBounds(value); got != originalLabelBounds(game, value) {
			t.Fatalf("changed scene bounds changed for %q", value.value)
		}
	}
	if len(game.labelMeasures.values) != 1 {
		t.Fatal("an obsolete full cache did not admit the new scene")
	}
	for index := range labelMeasureEntries {
		game.labelBounds(label{value: fmt.Sprintf("New station %04d", index), size: 12})
	}
	for key := range game.labelMeasures.values {
		if !strings.HasPrefix(key.value, "New station ") {
			t.Fatalf("old scene entry retained: %q", key.value)
		}
	}
	if len(game.labelMeasures.values) != labelMeasureEntries {
		t.Fatal("new scene did not fill the cache")
	}
}

func TestLabelMeasureAdmissionThreshold(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name        string
		misses      int
		lastHit     bool
		wantEntries int
	}{
		{name: "exactly three quarters", misses: 768, lastHit: true, wantEntries: labelMeasureEntries},
		{name: "above threshold ending on miss", misses: 769, wantEntries: 1},
		{name: "above threshold ending on hit", misses: 769, lastHit: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game := journeyTestGame(t, 2)
			first := label{value: "Old 0", size: 11}
			for index := range labelMeasureEntries {
				game.labelBounds(label{value: fmt.Sprintf("Old %d", index), size: 11})
			}
			misses := test.misses
			if !test.lastHit {
				misses--
			}
			for index := range labelMeasureEntries - 1 {
				value := first
				if index < misses {
					value.value = fmt.Sprintf("New %d", index)
				}
				game.labelBounds(value)
			}
			last := first
			if !test.lastHit {
				last.value = "Last miss"
			}
			if got := game.labelBounds(last); got != originalLabelBounds(game, last) {
				t.Fatal("the final lookup changed bounds when the admission window ended")
			}
			if got := len(game.labelMeasures.values); got != test.wantEntries {
				t.Fatalf("retained %d entries, want %d", got, test.wantEntries)
			}
			if game.labelMeasures.observations != 0 || game.labelMeasures.misses != 0 {
				t.Fatal("completed admission window retained counters")
			}
		})
	}
}

func TestLabelMeasureCapacityAndGameOwnership(t *testing.T) {
	t.Parallel()
	game := journeyTestGame(t, 2)
	other := &Game{font: game.font, layout: game.layout}
	for index := range 2*labelMeasureEntries + 3 {
		value := label{value: fmt.Sprintf("Station %04d", index), size: 10, mapLabel: true}
		if got := game.labelBounds(value); got != originalLabelBounds(game, value) {
			t.Fatalf("entry %d bounds changed", index)
		}
		if len(game.labelMeasures.values) > labelMeasureEntries {
			t.Fatal("dimension cache exceeded its entry limit")
		}
	}
	value := label{value: "Other game", size: 11}
	other.labelBounds(value)
	if other.labelMeasures == game.labelMeasures || len(other.labelMeasures.values) != 1 {
		t.Fatal("games share mutable dimension storage")
	}
	clear(game.labelMeasures.values)
	if len(other.labelMeasures.values) != 1 {
		t.Fatal("clearing one game changed the other")
	}
}
