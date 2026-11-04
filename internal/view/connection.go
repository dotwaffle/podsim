package view

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"github.com/dotwaffle/podsim/internal/remote"
)

func (g *Game) readRemote() {
	state, connected, pending := g.client.View()
	g.connected, g.pending = connected, pending
	g.lastFrame = g.client.LastFrame()
	previous := g.state
	if state.Epoch != "" {
		g.motion.Observe(state, time.Now())
		g.state = state
		// A new session or generation can replace the network and the fleet,
		// and the user armed the click for the old ones.
		if remote.SessionOf(previous) != remote.SessionOf(state) {
			g.debrisArmed = false
		}
		if g.selected >= len(state.Simulation.Vehicles) {
			g.selected = 0
		}
		g.network = state.Network
		g.normalizeSelection()
		if state.Simulation.OperationsOff {
			g.closeIncidents()
		}
	}
	select {
	case result := <-g.client.Results():
		g.handleResult(result)
	default:
	}
	// Compare the states after the command result. A session change notice
	// then replaces a notice or an error of a command result in the same
	// update.
	if notice := speedReductionNotice(previous, g.state); notice != "" {
		g.notice.show("speed", notice)
	}
	g.announceSessionChange(previous)
	// Check the build after the command result. handleResult clears the
	// message, and the update message must then show again at once.
	if g.client.BuildChanged() {
		g.handleServerUpdate()
	}
}

// serverUpdateMessage tells a desktop client user to restart the client
// after a server upgrade.
const serverUpdateMessage = "The server was updated. Restart the desktop client to load the new version."

// handleServerUpdate runs on each update after the server build changes.
// The browser reloads the page once, so it runs the files of the new server
// build. The desktop client cannot reload. It shows a message each time the
// message line is empty, so the message comes back after other messages
// clear.
func (g *Game) handleServerUpdate() {
	switch {
	case g.reload == nil:
		if g.message == "" {
			g.message = serverUpdateMessage
		}
	case !g.serverUpdated:
		g.reload()
	}
	g.serverUpdated = true
}

func (g *Game) connectionLabel() string {
	if !g.connected && g.client != nil {
		if message := g.client.ConnectionError(); message != "" {
			return message
		}
	}
	if !g.connected {
		return "Connection lost or connecting. Controls resume when the server is available."
	}
	if g.pending {
		return "Shared session / waiting for command confirmation"
	}
	return "Shared session / connected. Playback, orders, demand, and save points are shared across all browsers. Pod inspection stays local."
}

// connectingMessage shows in the map until the first state frame arrives.
const connectingMessage = "Connecting to server..."

// mapDimAlpha is the opacity of the background color over the map while the
// connection is lost.
const mapDimAlpha = 160

// mapNotice returns the text over the map at the time now. stale is true
// when the connection is lost and the map shows old state. Before the first
// state frame, the text is connectingMessage, because the game has no
// network and no pods. With a connection, the text is empty.
func (g *Game) mapNotice(now time.Time) (value string, stale bool) {
	switch {
	case g.state.Epoch == "":
		return connectingMessage, false
	case !g.connected:
		return staleStateText(now.Sub(g.lastFrame)), true
	default:
		return "", false
	}
}

// staleStateText tells the user that the connection is lost. It gives the
// age of the shown state in whole seconds. age is the time since the last
// state frame.
func staleStateText(age time.Duration) string {
	return fmt.Sprintf("Connection lost. Showing state from %d s ago.", int64(max(0, age)/time.Second))
}

// drawConnectionState draws the map notice for the time now. The
// connecting message shows in the center of the empty map. When the
// connection is lost, the map is dimmed, and the notice shows in an amber
// banner at the top of the map. Pods that stop because of a lost connection
// then do not look like a traffic jam.
func (g *Game) drawConnectionState(screen *ebiten.Image, now time.Time) {
	value, stale := g.mapNotice(now)
	viewport := g.layout.mapViewport
	switch {
	case stale:
		shade := rgb(background)
		vector.FillRect(screen, float32(viewport.Min.X), float32(viewport.Min.Y), float32(viewport.Dx()), float32(viewport.Dy()), color.NRGBA{R: shade.R, G: shade.G, B: shade.B, A: mapDimAlpha}, false)
		banner := g.mapBanner()
		vector.FillRect(screen, float32(banner.Min.X), float32(banner.Min.Y), float32(banner.Dx()), float32(banner.Dy()), rgb(amber), false)
		g.label(screen, g.mapBannerLabel(value))
	case value != "":
		g.label(screen, g.centerLabel(viewport, label{size: 16, value: value, color: muted}))
	}
}

// mapBanner returns the area of the banner at the top of the map, in
// physical pixels.
func (g *Game) mapBanner() image.Rectangle {
	viewport := g.layout.mapViewport
	return image.Rect(viewport.Min.X, viewport.Min.Y, viewport.Max.X, viewport.Min.Y+int(math.Round(32*g.layout.unit)))
}

// mapBannerLabel returns value as a label in the center of the map banner.
func (g *Game) mapBannerLabel(value string) label {
	return g.centerLabel(g.mapBanner(), label{size: 13, value: value, color: background})
}
