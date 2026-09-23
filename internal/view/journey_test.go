package view

import (
	"bytes"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"golang.org/x/image/font/gofont/goregular"

	"github.com/dotwaffle/podsim/internal/scenarios"
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
	game.click(centerOfButton(findButton(t, game.buttons(), "to/"+want)))
	if game.destination != want {
		t.Fatalf("destination = %q, want %q", game.destination, want)
	}
	game.click(centerOfButton(findButton(t, game.buttons(), "stations-prev")))
	if game.stationPage != 0 {
		t.Fatalf("station page = %d, want 0", game.stationPage)
	}
}

// TestStationChipsDoNotTriggerControls uses station IDs that are also control
// actions. A station chip must only select its station.
func TestStationChipsDoNotTriggerControls(t *testing.T) {
	t.Parallel()
	game := journeyTestGame(t, 4)
	ids := []string{"rewind", "checkpoint", "reset", "pause"}
	for index, id := range ids {
		game.network.Stations[index].ID = id
	}
	game.origin, game.destination = ids[3], ids[2]
	for _, id := range ids {
		for _, chip := range []struct {
			action string
			got    func() string
		}{
			{action: "from/" + id, got: func() string { return game.origin }},
			{action: "to/" + id, got: func() string { return game.destination }},
		} {
			control := findButton(t, game.buttons(), chip.action)
			if control.disabled {
				t.Fatalf("chip %q is disabled", chip.action)
			}
			game.click(centerOfButton(control))
			if chip.got() != id || game.pending {
				t.Fatalf("chip %q selected %q with pending %t, want %q and no command", chip.action, chip.got(), game.pending, id)
			}
		}
	}
}

// TestStationChipLabelsFit checks the station chip labels at unit and
// fractional scales. A chip shows the full station name. Only a name that is
// wider than the station row gets a shorter label.
func TestStationChipLabelsFit(t *testing.T) {
	t.Parallel()
	london := scenarios.London().Network
	long := sim.Network{Stations: []sim.Station{
		{ID: "bank", Name: "Bank"},
		{ID: "long", Name: strings.Repeat("Very Long Station Name ", 20)},
	}}
	minimum := layoutInput{outsideWidth: minimumWidth, outsideHeight: minimumHeight, deviceScale: 1}
	short := layoutInput{outsideWidth: 1366, outsideHeight: 617, deviceScale: 1}
	dense := layoutInput{outsideWidth: 1920, outsideHeight: 930, deviceScale: 1.5}
	tests := []struct {
		name    string
		network sim.Network
		layout  layoutInput
		unit    float64
		cut     string
	}{
		{name: "London at unit 1", network: london, layout: minimum, unit: 1},
		{name: "London at unit 0.8118", network: london, layout: short, unit: 0.8118},
		{name: "London at unit 1.5", network: london, layout: dense, unit: 1.5},
		{name: "long name at unit 0.8118", network: long, layout: short, unit: 0.8118, cut: "long"},
		{name: "long name at unit 1.5", network: long, layout: dense, unit: 1.5, cut: "long"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game := journeyNetworkGame(t, test.network)
			game.layoutFor(test.layout)
			if math.Abs(game.layout.unit-test.unit) > 1e-4 {
				t.Fatalf("unit = %g, want %g", game.layout.unit, test.unit)
			}
			face := game.textFace(stationFontSize)
			chips := 0
			for _, page := range game.stationPages() {
				for _, chip := range page {
					chips++
					full := compactStationName(chip.station.Name)
					if chip.station.ID != test.cut {
						if chip.label != full {
							t.Errorf("chip label = %q, want %q", chip.label, full)
						}
						continue
					}
					if chip.label == full || !strings.HasSuffix(chip.label, "…") {
						t.Errorf("chip label = %q, want a cut label", chip.label)
					}
					limit := (chip.width - stationChipPadding) * game.layout.unit
					if width, _ := text.Measure(chip.label, face, 0); width > limit {
						t.Errorf("label %q width %g px exceeds chip text width %g px", chip.label, width, limit)
					}
				}
			}
			if want := len(game.passengerStations()); chips != want {
				t.Fatalf("chips = %d, want %d", chips, want)
			}
			for page := range game.stationPages() {
				game.stationPage = page
				for _, control := range game.buttons() {
					if control.fontSize != stationFontSize {
						continue
					}
					if drawn := game.fitButtonText(control.label, control.fontSize, control.w); drawn != control.label {
						t.Errorf("drawn label = %q, want %q", drawn, control.label)
					}
				}
			}
		})
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
	stations := make([]sim.Station, count)
	for index := range stations {
		stations[index] = sim.Station{ID: fmt.Sprintf("station-%02d", index+1), Name: fmt.Sprintf("Station %02d", index+1)}
	}
	return journeyNetworkGame(t, sim.Network{Stations: stations})
}

// journeyNetworkGame returns a connected game on network. The journey goes
// from the first station to the second station.
func journeyNetworkGame(t *testing.T, network sim.Network) *Game {
	t.Helper()
	font, err := text.NewGoTextFaceSource(bytes.NewReader(goregular.TTF))
	if err != nil {
		t.Fatalf("load font: %v", err)
	}
	game := &Game{network: network, font: font, connected: true, origin: network.Stations[0].ID, destination: network.Stations[1].ID}
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
