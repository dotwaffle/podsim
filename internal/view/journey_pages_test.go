package view

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"golang.org/x/image/font/gofont/goregular"

	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

func TestStationPageCacheMatchesOriginal(t *testing.T) {
	t.Parallel()
	networks := []sim.Network{scenarios.LondonCentral().Network, scenarios.LondonFull().Network, {Stations: []sim.Station{{ID: "first", Name: "First"}, {ID: "long", Name: strings.Repeat("Long station name ", 30)}}}}
	layouts := []layoutInput{{outsideWidth: minimumWidth, outsideHeight: minimumHeight, deviceScale: 1}, {outsideWidth: 1366, outsideHeight: 617, deviceScale: 1}, {outsideWidth: 1920, outsideHeight: 930, deviceScale: 1.5}}
	for _, network := range networks {
		game := journeyNetworkGame(t, network)
		for _, layout := range layouts {
			game.layout = newDisplayLayout(layout)
			for _, query := range []string{"", "arc", "CHX", "  CHX  ", "940GZZLUCHX", "no match"} {
				game.journeySearch.filter, game.journeySearch.query[0] = 1, query
				want := game.stationPagesBeforeCache()
				got := game.stationPages()
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("query%q unit%g cached pages differ", query, game.layout.unit)
				}
				cache := game.stationPagesCache
				if again := game.stationPages(); !reflect.DeepEqual(again, want) || game.stationPagesCache != cache {
					t.Fatal("unchanged inputs rebuilt or changed pages")
				}
			}
		}
	}
}

func TestStationPageCacheUsesEffectiveQuery(t *testing.T) {
	t.Parallel()
	game := journeyNetworkGame(t, scenarios.LondonFull().Network)
	game.journeySearch.filter, game.journeySearch.query[0] = 1, " CHX "
	pages := game.stationPages()
	cache := game.stationPagesCache
	game.journeySearch.filter, game.journeySearch.query[1] = 2, "chx"
	game.origin, game.destination = "940GZZLUACY", "940GZZLUCHX"
	game.stationPage = 5
	game.state.Revision++
	game.state.Simulation.Tick++
	if !reflect.DeepEqual(pages, game.stationPages()) || game.stationPagesCache != cache {
		t.Fatal("equivalent From/To query or runtime state rebuilt pages")
	}
	if len(pages) != 1 || len(pages[0]) != 1 || pages[0][0].station.ID != "940GZZLUCHX" {
		t.Fatal("fixture did not match Charing Cross")
	}
	game.journeySearch.query[1] = "arc"
	if got := game.stationPages(); game.stationPagesCache == cache || len(got) != 1 || got[0][0].station.ID != "940GZZLUACY" {
		t.Fatal("query change kept old pages")
	}
	game.journeySearch.filter = 0
	game.stationPages()
	cache = game.stationPagesCache
	game.journeySearch.filter, game.journeySearch.query[0] = 1, "   "
	game.stationPages()
	if game.stationPagesCache != cache {
		t.Fatal("empty effective query rebuilt pages")
	}
}

func TestStationPageCacheFollowsNetworkIdentity(t *testing.T) {
	t.Parallel()
	first := session.State{Epoch: "one", ProjectRevision: 3, Generation: 5, ServerStart: "a"}
	for _, tc := range []struct {
		name  string
		state session.State
	}{
		{"apply", session.State{Epoch: "one", ProjectRevision: 4, Generation: 6, ServerStart: "a"}},
		{"rewind", session.State{Epoch: "one", ProjectRevision: 2, Generation: 6, ServerStart: "a"}},
		{"epoch", session.State{Epoch: "two", ProjectRevision: 3, Generation: 5, ServerStart: "a"}},
		{"generation", session.State{Epoch: "one", ProjectRevision: 3, Generation: 6, ServerStart: "a"}},
		{"restart", session.State{Epoch: "one", ProjectRevision: 3, Generation: 5, ServerStart: "b"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			game := journeyTestGame(t, 3)
			game.state = first
			game.stationPages()
			cache := game.stationPagesCache
			game.network = sim.Network{Stations: []sim.Station{{ID: "changed", Name: "Changed"}, {ID: "parking", Name: "Parking", ParkingOnly: true}}}
			game.state = tc.state
			got := game.stationPages()
			if game.stationPagesCache == cache || len(got) != 1 || len(got[0]) != 1 || got[0][0].label != "Changed" {
				t.Fatal("new network retained old pages")
			}
			game.network = sim.Network{}
			game.state.ProjectRevision++
			if got := game.stationPages(); got != nil {
				t.Fatal("empty network retained old pages")
			}
			cache = game.stationPagesCache
			game.stationPages()
			if game.stationPagesCache != cache {
				t.Fatal("empty page result was not cached")
			}
		})
	}
}

func TestStationPageCacheFollowsFontAndLayout(t *testing.T) {
	t.Parallel()
	game := journeyTestGame(t, 40)
	game.stationPages()
	cache := game.stationPagesCache
	game.layout.extraX += 100
	game.stationPages()
	if game.stationPagesCache == cache {
		t.Fatal("width change retained old pages")
	}
	cache = game.stationPagesCache
	game.layout.unit *= 1.5
	if got, want := game.stationPages(), game.stationPagesBeforeCache(); game.stationPagesCache == cache || !reflect.DeepEqual(got, want) {
		t.Fatal("scale change retained wrong pages")
	}
	cache = game.stationPagesCache
	font, err := text.NewGoTextFaceSource(bytes.NewReader(goregular.TTF))
	if err != nil {
		t.Fatal(err)
	}
	game.font = font
	if got, want := game.stationPages(), game.stationPagesBeforeCache(); game.stationPagesCache == cache || !reflect.DeepEqual(got, want) {
		t.Fatal("font change retained wrong pages")
	}
}

// stationPagesBeforeCache retains the original page layout from 0d4613d.
func (g *Game) stationPagesBeforeCache() [][]stationChip {
	available := stationControlRight - stationControlLeft - stationSearchWidth - stationChipGap + g.layout.extraX/g.layout.unit
	chipSpace := available - 2*(stationArrowWidth+stationChipGap)
	face := g.textFace(stationFontSize)
	var pages [][]stationChip
	var page []stationChip
	used := 0.0
	for _, station := range g.journeyStations() {
		label := compactStationName(station.Name)
		measured, _ := text.Measure(label, face, 0)
		width := max(44, measured/g.layout.unit+stationChipPadding)
		// The measured label fits its chip. At fractional scales, the
		// conversion to units and back can make the width a little smaller
		// than the label. Fit the label only when the station row caps the
		// chip width.
		if width > chipSpace {
			width = chipSpace
			label = fitText(label, textFit{face: face, width: (width - stationChipPadding) * g.layout.unit})
		}
		needed := width
		if len(page) > 0 {
			needed += stationChipGap
		}
		if len(page) > 0 && used+needed > chipSpace {
			pages = append(pages, page)
			page = nil
			used = 0
			needed = width
		}
		page = append(page, stationChip{station: station, label: label, width: width})
		used += needed
	}
	if len(page) > 0 {
		pages = append(pages, page)
	}
	return pages
}
