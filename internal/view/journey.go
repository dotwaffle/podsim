package view

import (
	"strconv"
	"strings"

	"github.com/hajimehoshi/ebiten/v2/text/v2"

	"github.com/dotwaffle/podsim/internal/sim"
)

const (
	stationControlLeft  = 100.0
	stationControlRight = 916.0
	stationArrowWidth   = 28.0
	stationChipGap      = 8.0
	stationChipPadding  = 20.0
	stationFontSize     = 12.0
)

type stationChip struct {
	station sim.Station
	label   string
	width   float64
}

func (g *Game) stationPages() [][]stationChip {
	available := stationControlRight - stationControlLeft + g.layout.extraX/g.layout.unit
	chipSpace := available - 2*(stationArrowWidth+stationChipGap)
	face := g.textFace(stationFontSize)
	var pages [][]stationChip
	var page []stationChip
	used := 0.0
	for _, station := range g.passengerStations() {
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

// journeyButtons returns the station page arrows and the From and To chips
// of the current station page. The chips are disabled when disabled is true.
func (g *Game) journeyButtons(disabled bool) []button {
	pages := g.stationPages()
	if len(pages) == 0 {
		return nil
	}
	g.stationPage = min(g.stationPage, len(pages)-1)
	hasPages := len(pages) > 1
	left := stationControlLeft
	if hasPages {
		left += stationArrowWidth + stationChipGap
	}
	buttons := make([]button, 0, len(pages[g.stationPage])*2+2)
	if hasPages {
		buttons = append(buttons,
			button{x: stationControlLeft, y: 632, w: stationArrowWidth, h: 64, label: "‹", disabled: g.stationPage == 0, action: "stations-prev", expandsWithMap: true},
			button{x: stationControlRight + g.layout.extraX/g.layout.unit - stationArrowWidth, y: 632, w: stationArrowWidth, h: 64, label: "›", disabled: g.stationPage == len(pages)-1, action: "stations-next", expandsWithMap: true},
		)
	}
	for _, chip := range pages[g.stationPage] {
		buttons = append(buttons,
			button{x: left, y: 632, w: chip.width, h: 28, label: chip.label, selected: g.origin == chip.station.ID, disabled: disabled, action: "from/" + chip.station.ID, expandsWithMap: true, fontSize: stationFontSize},
			button{x: left, y: 668, w: chip.width, h: 28, label: chip.label, selected: g.destination == chip.station.ID, disabled: disabled, action: "to/" + chip.station.ID, expandsWithMap: true, fontSize: stationFontSize},
		)
		left += chip.width + stationChipGap
	}
	return buttons
}

type textFit struct {
	face  text.Face
	width float64
}

func fitText(value string, fit textFit) string {
	if measured, _ := text.Measure(value, fit.face, 0); measured <= fit.width {
		return value
	}
	letters := []rune(value)
	for len(letters) > 1 {
		letters = letters[:len(letters)-1]
		candidate := string(letters) + "…"
		if measured, _ := text.Measure(candidate, fit.face, 0); measured <= fit.width {
			return candidate
		}
	}
	return "…"
}

func compactStationName(name string) string {
	number, found := strings.CutPrefix(name, "Station ")
	if !found {
		return name
	}
	if _, err := strconv.Atoi(number); err != nil {
		return name
	}
	return "S" + number
}
