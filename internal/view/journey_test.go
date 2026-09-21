package view

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"golang.org/x/image/font/gofont/goregular"

	"github.com/dotwaffle/podsim/internal/sim"
)

func TestStationPagesFitControls(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		layout layoutInput
	}{
		{name: "minimum window", layout: layoutInput{outsideWidth: minimumWidth, outsideHeight: minimumHeight, deviceScale: 1}},
		{name: "wide high density window", layout: layoutInput{outsideWidth: 1920, outsideHeight: 1080, deviceScale: 2}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game := journeyTestGame(t, 40)
			game.layoutFor(test.layout)
			pages := game.stationPages()
			if len(pages) < 2 {
				t.Fatalf("station pages = %d, want pagination", len(pages))
			}
			for page := range pages {
				game.stationPage = page
				for _, control := range game.buttons() {
					if !control.expandsWithMap {
						continue
					}
					if control.x < stationControlLeft*game.layout.unit || control.x+control.w > stationControlRight*game.layout.unit+game.layout.extraX {
						t.Fatalf("page %d control %q escaped station area: %+v", page, control.action, control)
					}
					width, _ := text.Measure(control.label, game.textFace(control.fontSize), 0)
					if control.fontSize > 0 && width > control.w-stationChipPadding*game.layout.unit+0.01 {
						t.Fatalf("label %q width %g does not fit control width %g", control.label, width, control.w)
					}
				}
			}
		})
	}
}

func TestStationPagingAndSelection(t *testing.T) {
	game := journeyTestGame(t, 40)
	pages := game.stationPages()
	game.click(centerOfButton(findButton(t, game.buttons(), "stations-next")))
	if game.stationPage != 1 {
		t.Fatalf("station page = %d, want 1", game.stationPage)
	}
	want := pages[1][0].station.ID
	game.click(centerOfButton(findButton(t, game.buttons(), want)))
	if game.destination != want {
		t.Fatalf("destination = %q, want %q", game.destination, want)
	}
	game.click(centerOfButton(findButton(t, game.buttons(), "stations-prev")))
	if game.stationPage != 0 {
		t.Fatalf("station page = %d, want 0", game.stationPage)
	}
}

func TestCompactStationName(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"Station 01":   "S01",
		"Station 100":  "S100",
		"Harbor":       "Harbor",
		"Station West": "Station West",
	}
	for input, want := range tests {
		if got := compactStationName(input); got != want {
			t.Errorf("compactStationName(%q) = %q, want %q", input, got, want)
		}
	}
}

func journeyTestGame(t *testing.T, count int) *Game {
	t.Helper()
	font, err := text.NewGoTextFaceSource(bytes.NewReader(goregular.TTF))
	if err != nil {
		t.Fatalf("load font: %v", err)
	}
	stations := make([]sim.Station, count)
	for index := range stations {
		stations[index] = sim.Station{ID: fmt.Sprintf("station-%02d", index+1), Name: fmt.Sprintf("Station %02d", index+1)}
	}
	game := &Game{network: sim.Network{Stations: stations}, font: font, connected: true, origin: stations[0].ID, destination: stations[1].ID}
	game.ensureLayout()
	return game
}

func findButton(t *testing.T, buttons []button, action string) button {
	t.Helper()
	for _, control := range buttons {
		if control.action == action {
			return control
		}
	}
	t.Fatalf("button %q not found", action)
	return button{}
}

func centerOfButton(control button) sim.Point {
	return sim.Point{X: control.x + control.w/2, Y: control.y + control.h/2}
}
