package view

import (
	"cmp"
	"slices"
	"strings"
	"unicode"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"github.com/dotwaffle/podsim/internal/sim"
)

const stationSearchWidth = 200.0

type journeySearch struct {
	focus      int
	filter     int
	query      [2]string
	unresolved [2]bool
	selectAll  bool
}

// stationCode uses the London NaPTAN station suffix. The two Northern line
// extension stations have longer suffixes, so they use explicit short codes.
func stationCode(id string) string {
	switch id {
	case "940GZZBPSUST":
		return "BPS"
	case "940GZZNEUGST":
		return "NEL"
	}
	if suffix, ok := strings.CutPrefix(id, "940GZZLU"); ok && len(suffix) == 3 {
		return suffix
	}
	return ""
}

func stationMatches(stations []sim.Station, query string) []sim.Station {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return stations
	}
	var exact, partial []sim.Station
	for _, station := range stations {
		name := strings.ToLower(station.Name)
		code := strings.ToLower(stationCode(station.ID))
		if name == query || code == query || strings.EqualFold(station.ID, query) {
			exact = append(exact, station)
		}
		if strings.Contains(name, query) || (code != "" && strings.HasPrefix(code, query)) {
			partial = append(partial, station)
		}
	}
	if len(exact) > 0 {
		return exact
	}
	return partial
}

func (g *Game) journeyStations() []sim.Station {
	stations := g.passengerStations()
	slices.SortFunc(stations, func(a, b sim.Station) int {
		if order := cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); order != 0 {
			return order
		}
		return cmp.Compare(a.ID, b.ID)
	})
	if g.journeySearch.filter != 0 {
		return stationMatches(stations, g.journeySearch.query[g.journeySearch.filter-1])
	}
	return stations
}

func (g *Game) startJourneySearch(side int) {
	g.journeySearch.focus = side
	g.journeySearch.filter = side
	g.journeySearch.query[side-1] = ""
	g.journeySearch.unresolved[side-1] = false
	g.journeySearch.selectAll = false
	g.stationPage = 0
	g.message = ""
}

func (g *Game) chooseJourneyStation(side int, id string) {
	if side == 1 {
		g.origin = id
	} else {
		g.destination = id
	}
	g.journeySearch.query[side-1] = ""
	g.journeySearch.unresolved[side-1] = false
	g.journeySearch.focus = 0
	g.journeySearch.filter = 0
	g.stationPage = 0
}

func (g *Game) searchJourney(query string) {
	side := g.journeySearch.focus
	if side == 0 {
		return
	}
	g.journeySearch.filter = side
	g.journeySearch.query[side-1] = query
	g.journeySearch.unresolved[side-1] = true
	g.stationPage = 0
	matches := stationMatches(g.passengerStations(), query)
	switch {
	case strings.TrimSpace(query) == "" || len(matches) == 0:
		g.message = "Type a station name or code. No station selected."
	case len(matches) > 1:
		g.message = "Several stations match. Type more or choose a station."
	default:
		if side == 1 {
			g.origin = matches[0].ID
		} else {
			g.destination = matches[0].ID
		}
		g.journeySearch.unresolved[side-1] = false
		g.message = ""
	}
}

// updateJourneyInput consumes typing before playback shortcuts. Pointer and
// touch handling still run while a search field has focus.
func (g *Game) updateJourneyInput() bool {
	if g.browserJourney == nil {
		g.browserJourney = new(browserJourney)
	}
	if handled, native := g.browserJourney.update(g); native {
		return handled
	}
	side := g.journeySearch.focus
	if side == 0 {
		return false
	}
	if g.state.Simulation.Demo || !g.connected {
		g.journeySearch.focus = 0
		return true
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
		g.journeySearch.query[side-1] = ""
		g.journeySearch.unresolved[side-1] = false
		g.journeySearch.focus = 0
		g.journeySearch.filter = 0
		g.message = ""
		return true
	}
	control := ebiten.IsKeyPressed(ebiten.KeyControl) || ebiten.IsKeyPressed(ebiten.KeyMeta)
	if control && inpututil.IsKeyJustPressed(ebiten.KeyA) {
		g.journeySearch.selectAll = true
	}
	query := []rune(g.journeySearch.query[side-1])
	changed := false
	if inpututil.IsKeyJustPressed(ebiten.KeyBackspace) {
		if g.journeySearch.selectAll {
			query = nil
		} else if len(query) > 0 {
			query = query[:len(query)-1]
		}
		g.journeySearch.selectAll = false
		changed = true
	}
	if !control {
		for _, char := range ebiten.AppendInputChars(nil) {
			if !unicode.IsPrint(char) {
				continue
			}
			if g.journeySearch.selectAll {
				query = nil
				g.journeySearch.selectAll = false
			}
			if len(query) < 80 {
				query = append(query, char)
				changed = true
			}
		}
	}
	if changed {
		g.searchJourney(string(query))
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyTab) {
		g.startJourneySearch(3 - side)
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyEnter) && !g.journeySearch.unresolved[side-1] {
		if side == 1 {
			g.startJourneySearch(2)
		} else {
			g.journeySearch.focus = 0
			g.request()
		}
	}
	return true
}

func (g *Game) journeySearchButtons(disabled bool) []button {
	var buttons []button
	for index, id := range []string{g.origin, g.destination} {
		value := "Name or code"
		if station, ok := g.network.Station(id); ok {
			value = station.Name
			if code := stationCode(id); code != "" {
				value = code + "  " + value
			}
		}
		if g.journeySearch.focus == index+1 || g.journeySearch.unresolved[index] {
			value = g.journeySearch.query[index]
			if value == "" {
				value = "Name or code"
			}
			if g.journeySearch.focus == index+1 {
				value += " |"
			}
		}
		action := "search-from"
		if index == 1 {
			action = "search-to"
		}
		buttons = append(buttons, button{x: stationControlLeft, y: 600 + 36*float64(index), w: stationSearchWidth, h: 28, label: g.fitText(value, 12, stationSearchWidth-20), selected: g.journeySearch.focus == index+1, disabled: disabled, action: action, expandsWithMap: true, fontSize: 12})
	}
	return buttons
}
