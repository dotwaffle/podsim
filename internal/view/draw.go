package view

import (
	"cmp"
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"
	"time"
	"unicode"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

// podPagerLabel returns the page count of the pod selector, for example
// "3 / 19". It shows between the page arrows. It returns false when all pods
// fit on one page.
func (g *Game) podPagerLabel() (label, bool) {
	pages := podPageCount(len(g.state.Simulation.Vehicles))
	if pages < 2 {
		return label{}, false
	}
	left := g.layout.right(podPagerLeft + podPagerArrowWidth)
	right := g.layout.right(1060 - podPagerArrowWidth)
	top := g.layout.bottom(podSelectorTop)
	area := image.Rect(int(math.Round(left)), int(math.Round(top)), int(math.Round(right)), int(math.Round(top+34*g.layout.unit)))
	return g.centerLabel(area, label{size: 10, value: fmt.Sprintf("%d / %d", g.podPage+1, pages), color: muted}), true
}

// Draw renders an independent snapshot without changing simulation state.
// While the shell page hides the game, Draw does nothing. Update still reads
// the shared state, so the first frame after the game shows again is
// current. The screen is not cleared between frames, so it keeps the last
// frame until then. See ebiten.SetScreenClearedEveryFrame in cmd/podsim.
func (g *Game) Draw(screen *ebiten.Image) {
	if g.hidden {
		g.mapBackground.publish(g.currentMapView())
		return
	}
	g.drawnFrames++
	g.ensureLayout()
	g.fitNetwork()
	g.imageLimit = imageSideLimit(ebiten.MaxImageSize())
	mapEnabled := g.mapBackground.publish(g.currentMapView())
	screen.Fill(rgb(background))
	for _, header := range g.headerLabels() {
		g.label(screen, header)
	}
	vector.FillRect(screen, float32(g.layout.x(24)), float32(g.layout.y(headerHeight)), float32(g.layout.x(748)+g.layout.extraX), float32(g.layout.y(474)+g.layout.extraY), rgb(panel), false)
	vector.FillRect(screen, float32(g.layout.right(796)), float32(g.layout.y(headerHeight)), float32(g.layout.x(280)), float32(g.layout.y(474)+g.layout.extraY), rgb(panel), false)
	vector.FillRect(screen, float32(g.layout.x(24)), float32(g.layout.bottom(558)), float32(g.layout.x(1052)+g.layout.extraX), float32(g.layout.y(142)), rgb(panel), false)
	if mapEnabled {
		if mapScreen, ok := screen.SubImage(g.layout.mapViewport).(*ebiten.Image); ok {
			mapScreen.Clear()
		}
	}
	state := g.state.Simulation
	g.drawNetwork(screen, g.mapSnapshot())
	g.drawConnectionState(screen, time.Now())
	switch {
	case g.showDemand:
		g.drawDemand(screen)
	case g.showOrders:
		g.drawOrders(screen, state)
	default:
		g.drawInspection(screen, state)
	}
	g.drawControls(screen, state)
	for _, b := range g.frameButtons() {
		g.drawButton(screen, b)
	}
	g.label(screen, g.connectionFooter(ebiten.IsFocused()))
}

// headerHeight is the height of the header above the panels in design
// units. headerButtonTop and headerButtonHeight set the row of header
// buttons, at the vertical center of the header.
const (
	headerHeight       = 64.0
	headerButtonTop    = 18.0
	headerButtonHeight = 28.0
)

// headerLabels returns the title and the header text above the panels.
// Beside the title, the run status shows in muted text, or in amber while
// the run is paused. The station and pod counts show below the run status.
// Before the first state frame, the game has no run state, no network, and
// no pods, so the header shows only the title.
func (g *Game) headerLabels() []label {
	labels := []label{{x: 28, y: 14, size: 30, value: "podsim", color: foreground}}
	if g.state.Epoch == "" {
		return labels
	}
	status := label{x: 157, y: 12, size: 14, value: runStatus(g.state), color: muted}
	if g.state.Simulation.Paused {
		status.color = amber
	}
	return append(labels, status, label{x: 157, y: 34, size: 14, value: fmt.Sprintf("%d STATIONS     %d PODS", len(g.passengerStations()), len(g.state.Simulation.Vehicles)), color: accent})
}

// runStatus returns the run status of state for the header. The status
// gives PAUSED while the run is paused, then the playback speed, the
// simulated time, and the number of completed journeys. An example is
// "PAUSED  2x  1234.5 s  57 completed". Before the first state frame, the
// state has no epoch, and the status is empty.
func runStatus(state session.State) string {
	if state.Epoch == "" {
		return ""
	}
	status := fmt.Sprintf("%dx  %.1f s  %d completed", state.Speed, float64(state.Simulation.Tick)/sim.TicksPerSecond, state.Simulation.Completed)
	if state.Simulation.Paused {
		status = "PAUSED  " + status
	}
	return status
}

// focusHint tells the user how to give the keyboard focus to the simulation.
const focusHint = "Click the simulation to use keyboard shortcuts"

// connectionFooter returns the connection status line below the panels.
// focused is true when the simulation has the keyboard focus. Without the
// focus, the line shows the focus hint in amber.
func (g *Game) connectionFooter(focused bool) label {
	footer := label{x: 28, y: 708, size: 11, value: g.connectionLabel(), color: muted}
	// A lost connection and a pending command win over the focus hint. In
	// these states, the client rejects commands until the connection comes
	// back or the server confirms the command. The focus hint shows after
	// that.
	if g.connected && !g.pending && !focused {
		footer.value, footer.color = focusHint, amber
	}
	return footer
}

func (g *Game) drawControls(screen *ebiten.Image, state sim.Snapshot) {
	title, hint := journeyText(state.Demo)
	g.label(screen, label{x: 44, y: 575, size: 13, value: title, color: foreground})
	if from, fromOK := g.network.Station(g.origin); fromOK {
		if to, toOK := g.network.Station(g.destination); toOK {
			value := fitText(from.Name+" > "+to.Name, textFit{face: g.textFace(11), width: 560 * g.layout.unit})
			g.label(screen, label{x: 230, y: 577, size: 11, value: value, color: muted})
		}
	}
	g.label(screen, label{x: 44, y: 606, size: 11, value: "FROM", color: muted})
	g.label(screen, label{x: 44, y: 642, size: 11, value: "TO", color: muted})
	if pages := g.stationPages(); len(pages) > 1 {
		g.label(screen, label{x: 180, y: 577, size: 10, value: fmt.Sprintf("%d / %d", g.stationPage+1, len(pages)), color: muted})
	}
	for _, value := range fleetStatLabels(state) {
		g.label(screen, value)
	}
	if pager, ok := g.podPagerLabel(); ok {
		g.label(screen, pager)
	}
	g.label(screen, g.hintLine(state, hint))
}

// journeyText returns the title and the hint of the journey controls. The
// title uses the verb of the Order button. While the traffic demo runs, they
// describe the demo.
func journeyText(demo bool) (title, hint string) {
	if demo {
		return "TRAFFIC DEMO", "Four pods, eight journeys: Market arrivals, pickups from parking, and follow-up orders. Reset stops the demo."
	}
	return "ORDER A JOURNEY", "Choose From and To. An available pod collects the passenger. Orders wait when all pods are busy."
}

// fleetStatLabels returns the Pickup wait and Fleet use lines for state.
// They are the last lines of the right panel, below Orders and Demand.
func fleetStatLabels(state sim.Snapshot) []label {
	use := summarizeFleet(state)
	orders := outstandingOrderCount(state)
	return []label{
		{x: 816, y: 495, size: 10, value: fmt.Sprintf("Orders: %d active / %d queued", orders-len(state.Pending), len(state.Pending)), color: muted},
		{x: 816, y: 509, size: 10, value: fmt.Sprintf("Pickup wait: avg %.0f s / max %.0f s", state.Wait.AverageSeconds, state.Wait.MaxSeconds), color: muted},
		{x: 816, y: 523, size: 10, value: fmt.Sprintf("Fleet use: %d%% active / %d%% passenger", use.activePercent(), use.passengerPercent()), color: muted},
	}
}

// sameStationHint tells the user why Order is disabled when From and To are
// the same station.
const sameStationHint = "Choose a different destination."

// hintLine returns the line below the journey controls. It shows the first
// text that is set, in this order: a confirmation, an unresolved search,
// the message, the demo
// error, the notice, sameStationHint, and hint. A second press of Reset or
// Start traffic demo resets the session while its confirmation is set, so
// the confirmation shows in place of all other text. The server update
// message of the desktop client and a demo error can stay for a long time.
// They must not hide the confirmation. sameStationHint shows while From and
// To are the same station, but not in the traffic demo. It stays until the
// user changes the selection, so a notice shows before it.
func (g *Game) hintLine(state sim.Snapshot, hint string) label {
	value, shade := hint, uint32(muted)
	switch {
	case isConfirmation(g.notice.action):
		value, shade = g.notice.text, accent
	case g.journeySearch.unresolved[0] || g.journeySearch.unresolved[1]:
		value, shade = "Choose a matching station for each search before ordering.", amber
	case g.message != "":
		value, shade = g.message, amber
	case state.DemoError != "":
		value, shade = state.DemoError, amber
	case g.notice.text != "":
		value, shade = g.notice.text, accent
	case !state.Demo && g.origin != "" && g.origin == g.destination:
		value, shade = sameStationHint, amber
	}
	return label{x: 44, y: 669, size: 13, value: value, color: shade}
}

func (g *Game) drawButton(screen *ebiten.Image, b button) {
	fill, ink := uint32(track), uint32(foreground)
	selectedFill := uint32(accent)
	if id, ok := strings.CutPrefix(b.action, "pod/"); ok {
		fill, ink = podButtonFill, g.podButtonColor(id)
		selectedFill = ink
	}
	if b.selected {
		fill, ink = selectedFill, background
	}
	if b.disabled {
		fill, ink = 0x1b2a36, 0x63788a
	}
	vector.FillRect(screen, float32(b.x), float32(b.y), float32(b.w), float32(b.h), rgb(fill), false)
	value, fontSize := g.buttonText(b)
	textWidth, textHeight := text.Measure(value, g.textFace(fontSize), 0)
	g.label(screen, label{x: b.x + (b.w-textWidth)/2, y: b.y + (b.h-textHeight)/2, size: fontSize, value: value, color: ink, physical: true})
}

// buttonText returns the label that button b shows and its font size. A
// pod button is narrow, so it uses a smaller font and a smaller padding
// than other buttons. Then a three-digit fleet number fits.
func (g *Game) buttonText(b button) (string, float64) {
	if strings.HasPrefix(b.action, "pod/") {
		const size, padding = 12, 4
		return fitText(shortText(b.label, 3), textFit{face: g.textFace(size), width: max(1, b.w-padding*g.layout.unit)}), size
	}
	size := cmp.Or(b.fontSize, 14)
	return g.fitButtonText(b.label, size, b.w), size
}

func (g *Game) fitButtonText(value string, size, width float64) string {
	return fitText(value, textFit{face: g.textFace(size), width: max(1, width-12*g.layout.unit)})
}

func (g *Game) fitText(value string, size, width float64) string {
	return fitText(value, textFit{face: g.textFace(size), width: width * g.layout.unit})
}

func (g *Game) textFace(size float64) *text.GoTextFace {
	return &text.GoTextFace{Source: g.font, Size: size * g.layout.unit}
}

type label struct {
	x, y, size float64
	// lineSpacing is the baseline distance in pixels for a multiline tag.
	lineSpacing float64
	value       string
	color       uint32
	physical    bool
	// mapLabel is true for a map label, such as a station name, a station
	// count or a pod label. Its size is in CSS pixels. See
	// displayLayout.mapLabelSize.
	mapLabel bool
}

// labelFace returns the font face of a label. The size of a map label does
// not follow the display unit. See displayLayout.mapLabelSize.
func (g *Game) labelFace(value label) *text.GoTextFace {
	return &text.GoTextFace{Source: g.font, Size: g.labelFaceSize(value)}
}

func (g *Game) labelFaceSize(value label) float64 {
	if value.mapLabel {
		return g.layout.mapLabelSize(value.size)
	}
	return value.size * g.layout.unit
}

func (g *Game) label(screen *ebiten.Image, label label) {
	if !label.physical && screen.Bounds() == image.Rect(0, 0, g.layout.width, g.layout.height) {
		label.x, label.y = g.layout.labelPosition(label.x, label.y)
	}
	options := &text.DrawOptions{}
	options.LineSpacing = label.lineSpacing
	options.GeoM.Translate(label.x, label.y)
	options.ColorScale.ScaleWithColor(rgb(label.color))
	text.Draw(screen, label.value, g.labelFace(label), options)
}

// centerLabel returns value as a physical label in the center of area.
func (g *Game) centerLabel(area image.Rectangle, value label) label {
	width, height := text.Measure(value.value, g.labelFace(value), value.lineSpacing)
	value.x = float64(area.Min.X) + (float64(area.Dx())-width)/2
	value.y = float64(area.Min.Y) + (float64(area.Dy())-height)/2
	value.physical = true
	return value
}

func rgb(hex uint32) color.RGBA {
	return color.RGBA{R: uint8((hex >> 16) & 255), G: uint8((hex >> 8) & 255), B: uint8(hex & 255), A: 255}
}

func shortText(value string, limit int) string {
	letters := []rune(value)
	if len(letters) <= limit {
		return value
	}
	return strings.TrimRightFunc(string(letters[:limit-1]), unicode.IsSpace) + "…"
}
