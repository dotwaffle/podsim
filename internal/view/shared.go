package view

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"github.com/dotwaffle/podsim/internal/remote"
	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

func (g *Game) readRemote() {
	state, connected, pending := g.client.View()
	g.connected, g.pending = connected, pending
	g.lastFrame = g.client.LastFrame()
	previous := g.state
	if state.Epoch != "" {
		g.motion.Observe(state, time.Now())
		g.state = state
		if g.selected >= len(state.Simulation.Vehicles) {
			g.selected = 0
		}
		g.network = state.Network
		g.normalizeSelection()
	}
	select {
	case result := <-g.client.Results():
		g.handleResult(result)
	default:
	}
	// Compare the states after the command result. A session change notice
	// then replaces a notice or an error of a command result in the same
	// update.
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

// rewindUnsavedMessage tells the user that the server applied a rewind but
// could not save the session state before its reply.
const rewindUnsavedMessage = "The server could not save the session state. A server crash can undo this rewind."

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

// handleResult shows the outcome of one command. Errors go to the message
// line. Some accepted commands show a notice.
func (g *Game) handleResult(result remote.Result) {
	g.message = ""
	if result.Err == nil && result.Reply.Error == "" && startsGeneration(result.Command.Action) {
		g.ownEpoch, g.ownGeneration = result.Reply.Epoch, result.Reply.Generation
	}
	switch {
	case result.Err != nil:
		g.message = result.Err.Error()
	case result.Reply.Error != "":
		g.message = result.Reply.Error
	case result.Command.Action == "trip":
		g.showOrders, g.showDemand = true, false
		g.acceptedOrigin, g.acceptedDestination = result.Command.Origin, result.Command.Destination
		from, _ := g.network.Station(result.Command.Origin)
		to, _ := g.network.Station(result.Command.Destination)
		g.showNotice("trip", fmt.Sprintf("Order #%d accepted: %s > %s. See Orders for status.", result.Reply.OrderID, from.Name, to.Name))
	case result.Command.Action == "checkpoint":
		g.savedEpoch, g.savedRevision = result.Reply.Epoch, result.Reply.Revision
		g.showNotice("checkpoint", fmt.Sprintf("Save point #%d saved.", result.Reply.Checkpoint))
	case result.Command.Action == "rewind":
		// Panels and selection stay as they are. readRemote clamps the
		// selection, so it stays valid after a project restore.
		g.showNotice("rewind", g.rewindNotice(result))
		// The amber message line shows the warning over the notice.
		if saved := result.Reply.StateSaved; saved != nil && !*saved {
			g.message = rewindUnsavedMessage
		}
	case result.Command.Action == "reset" || result.Command.Action == "demo":
		g.showOrders, g.showDemand = false, false
		g.notice, g.noticeAction, g.noticeTicks = "", "", 0
		g.selected, g.podPage = 0, 0
		g.normalizeSelection()
		if result.Command.Action == "reset" {
			g.showNotice("reset", resetNotice)
		}
	}
}

// startsGeneration reports whether a command with action starts a new
// generation of the shared session. A reset, a demo, a rewind, and a
// project apply do this.
func startsGeneration(action string) bool {
	switch action {
	case "reset", "demo", "rewind", "project":
		return true
	default:
		return false
	}
}

const (
	// sessionChangeAction is the notice action of restartNotice and
	// otherBrowserNotice.
	sessionChangeAction = "session-change"
	// restartNotice tells the user that the server restarted. The server
	// keeps save points in memory only. The -state option does not save
	// them, so each restart clears them.
	restartNotice = "Server restarted. Save points cleared."
	// otherBrowserNotice tells the user that another browser started a new
	// generation of the shared session.
	otherBrowserNotice = "Another browser reset or rewound the session."
)

// sessionChangeInput holds the inputs of sessionChangeNotice.
type sessionChangeInput struct {
	// previous is the state before the update, and current is the state
	// after it.
	previous, current session.State
	// inFlight is true while a command of this game that starts a new
	// generation waits for its reply.
	inFlight bool
	// ownEpoch and ownGeneration come from the last accepted reply to such
	// a command.
	ownEpoch      string
	ownGeneration uint64
}

// sessionChangeNotice returns the notice for a change from the previous
// state to the current state that this game did not cause. It returns an
// empty string when there is no such change, and for the first state frame.
//
// Only a server restart gives a new epoch. A restart with the -state option
// can keep the epoch. Then the generation changes, and keptEpochRestart is
// true for the current state.
//
// Other changes of the generation come from a reset, a demo, a rewind, or a
// project apply. They show otherBrowserNotice, but not while a command of
// this game that starts a new generation waits for its reply. They also
// show no notice when the last reply to such a command gave the current
// epoch, and the current generation or a later one.
func sessionChangeNotice(input sessionChangeInput) string {
	previous, current := input.previous, input.current
	switch {
	case previous.Epoch == "":
		return ""
	case current.Epoch != previous.Epoch:
		return restartNotice
	case current.Generation == previous.Generation:
		return ""
	case keptEpochRestart(current):
		return restartNotice
	case input.inFlight:
		return ""
	case current.Epoch == input.ownEpoch && current.Generation <= input.ownGeneration:
		return ""
	default:
		return otherBrowserNotice
	}
}

// keptEpochRestart reports whether a state with a new generation comes from
// a server restart that kept the epoch. The server keeps the epoch only
// after a physical or a logical restore, and a restart clears the save
// points. A reset, a demo, and a project apply clear the restore tier, and
// a rewind keeps the save points. Thus no other change gives a state with
// both properties.
func keptEpochRestart(state session.State) bool {
	tier := sim.RestoreTier(state.Restore.Tier)
	return len(state.Checkpoints) == 0 && (tier == sim.RestorePhysical || tier == sim.RestoreLogical)
}

// announceSessionChange shows a notice when the state changed from previous
// in a way that this game did not cause. The notice replaces the old
// message and notice, also the reset confirmation, because they are about
// the session before the change.
func (g *Game) announceSessionChange(previous session.State) {
	notice := sessionChangeNotice(sessionChangeInput{
		previous: previous, current: g.state,
		inFlight: g.pending && startsGeneration(g.sentAction),
		ownEpoch: g.ownEpoch, ownGeneration: g.ownGeneration,
	})
	if notice == "" {
		return
	}
	g.message = ""
	g.showNotice(sessionChangeAction, notice)
}

// noticeDuration is the time in game ticks that a notice shows. The game
// runs sim.TicksPerSecond ticks each second, so a notice shows for 3 s. The
// reset confirmation text gives this time.
const noticeDuration = 3 * sim.TicksPerSecond

// showNotice shows value in the hint line for noticeDuration ticks. action
// names the command or the prompt that caused the notice.
func (g *Game) showNotice(action, value string) {
	g.notice, g.noticeAction, g.noticeTicks = value, action, noticeDuration
}

// rewindNotice describes an accepted rewind. It gives the time of the save
// point when the state still lists it. When the reply shows that the rewind
// restored a different project, the notice says so.
func (g *Game) rewindNotice(result remote.Result) string {
	id := result.Command.Checkpoint
	notice := fmt.Sprintf("Rewound to save point #%d. Paused.", id)
	index := slices.IndexFunc(g.state.Checkpoints, func(entry session.Checkpoint) bool { return entry.ID == id })
	if index >= 0 {
		seconds := float64(g.state.Checkpoints[index].Tick) / sim.TicksPerSecond
		notice = fmt.Sprintf("Rewound to save point #%d (%.1f s). Paused.", id, seconds)
	}
	if result.Reply.ProjectRestored {
		notice += " Project settings restored."
	}
	return notice
}

// submit sends command to the server. When the client rejects the command,
// the error shows in the hint line. A sent command clears the message and
// the notice. The line below the panels shows that the command waits for the
// server. The hint line does not show this wait, because a wait is not an
// error.
func (g *Game) submit(command session.Command) {
	if err := g.client.Submit(command); err != nil {
		g.message = err.Error()
		return
	}
	g.pending, g.sentAction = true, command.Action
	g.message = ""
	g.notice, g.noticeAction, g.noticeTicks = "", "", 0
}

func (g *Game) connectionLabel() string {
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

// demandPatternLabel returns the label of the Pattern button for config.
// destination is the name of the destination station. Only the destination
// pattern uses it. A profile label gives the band ID before the profile
// ID. The button cuts the end of a long label, and the band then stays
// visible.
func demandPatternLabel(config session.DemandConfig, destination string) string {
	pattern := "Balanced"
	switch config.Pattern {
	case "market":
		pattern = "Market-bound"
	case "destination":
		pattern = destination + "-bound"
	case "profile":
		pattern = config.Band + " / " + config.Profile
	}
	return "Pattern: " + pattern
}

func (g *Game) demandButtons() []button {
	config := g.state.Demand.Config
	destination, _ := g.network.Station(config.Destination)
	toggle := "Start demand"
	if config.Enabled {
		toggle = "Stop demand"
	}
	disabled := !g.connected || g.pending || g.state.Simulation.Demo
	return []button{
		{x: 810, y: 176, w: 250, h: 30, label: fmt.Sprintf("Rate: %d orders/min", config.PerMinute), disabled: disabled, action: "demand-rate"},
		{x: 810, y: 214, w: 250, h: 30, label: demandPatternLabel(config, destination.Name), disabled: disabled, action: "demand-pattern"},
		{x: 810, y: 252, w: 250, h: 30, label: fmt.Sprintf("Seed: %d", config.Seed), disabled: disabled, action: "demand-seed"},
		{x: 810, y: 290, w: 250, h: 30, label: toggle, selected: config.Enabled, disabled: disabled, action: "demand-toggle"},
	}
}

func (g *Game) changeDemand(action string) {
	config := g.state.Demand.Config
	switch action {
	case "demand-rate":
		config.PerMinute = nextDemandRate(config.PerMinute)
	case "demand-pattern":
		switch {
		case config.Pattern == "balanced":
			config.Pattern = "destination"
			config.Destination = g.destination
		case config.Pattern == "destination" && config.Profile != "":
			config.Pattern = "profile"
		case config.Pattern == "profile":
			config.Pattern = "balanced"
			config.Destination = ""
		default:
			config.Pattern = "balanced"
		}
	case "demand-seed":
		config.Seed = nextDemandSeed(config.Seed)
	case "demand-toggle":
		config.Enabled = !config.Enabled
	}
	g.submit(session.Command{Action: "demand", Demand: config})
}

// nextDemandRate returns the rate in orders per simulated minute that the
// Rate button selects after current. The cycle contains the rates 1, 2, 4,
// 8, 12, 20, 30, and 60 and the current rate, in ascending order. Thus the
// London rate of 20 is in the cycle. A project rate that is not in the list
// stays in the cycle until the rate changes. After the highest rate, the
// cycle starts again at 1.
func nextDemandRate(current int) int {
	for _, rate := range []int{1, 2, 4, 8, 12, 20, 30, 60} {
		if rate > current {
			return rate
		}
	}
	return 1
}

// nextDemandSeed returns the seed that the Seed button selects after seed.
// After the largest seed, the next seed is 0. project.ValidateDemand
// accepts all seeds, 0 included.
func nextDemandSeed(seed uint64) uint64 {
	return seed + 1
}

// demandSavesNote tells the user that each change in the Demand panel
// changes the project.
const demandSavesNote = "Changes save to the project"

func (g *Game) drawDemand(screen *ebiten.Image) {
	for _, value := range g.demandLabels() {
		g.label(screen, value)
	}
}

const (
	// demandTextWidth is the width of the Demand panel text, from the
	// left edge of the text to the right edge of the buttons.
	demandTextWidth = 244
	// demandErrorLines is the largest number of lines for the demand
	// error.
	demandErrorLines = 2
)

// demandLabels returns the text of the Demand panel. Below the counters,
// the last demand error shows in amber on at most demandErrorLines lines.
func (g *Game) demandLabels() []label {
	demand := g.state.Demand
	labels := []label{
		{x: 816, y: 115, size: 12, value: "PASSENGER DEMAND", color: muted},
		{x: 816, y: 140, size: 11, value: "Per simulated minute / shared settings", color: foreground},
		{x: 816, y: 158, size: 10, value: demandSavesNote, color: muted},
		{x: 816, y: 328, size: 11, value: fmt.Sprintf("Generated %d / skipped %d", demand.Generated, demand.Skipped), color: foreground},
		{x: 816, y: 346, size: 10, value: g.fitText(redistributionText(g.state), 10, demandTextWidth), color: muted},
	}
	errorFit := textFit{face: g.textFace(10), width: demandTextWidth * g.layout.unit}
	for index, line := range wrapText(demand.Error, errorFit, demandErrorLines) {
		labels = append(labels, label{x: 816, y: 363 + 13*float64(index), size: 10, value: line, color: amber})
	}
	return labels
}

// redistributionText returns the redistribution line of the Demand panel
// for state. The line tells if redistribution is on. It gives the number of
// redistribution moves and the distance that pods traveled with no
// passenger.
func redistributionText(state session.State) string {
	setting := "off"
	if state.Redistribution {
		setting = "on"
	}
	moves := state.Simulation.RebalanceMoves
	unit := "moves"
	if moves == 1 {
		unit = "move"
	}
	return fmt.Sprintf("Redistribution: %s / %d %s / %s empty", setting, moves, unit, travelDistanceText(state.Simulation.EmptyDistanceMeters))
}

// travelDistanceText returns meters as a distance to show. A distance
// below 1 km shows in whole meters. A longer distance shows in km with one
// decimal.
func travelDistanceText(meters float64) string {
	if math.Round(meters) < 1000 {
		return fmt.Sprintf("%.0f m", meters)
	}
	return fmt.Sprintf("%.1f km", meters/1000)
}

// wrapText splits value at spaces into lines that fit the width of fit.
// It returns at most limit lines, and no lines for a value with no words.
// When value needs more lines, the last line ends with an ellipsis. A word
// that does not fit on a line is cut.
func wrapText(value string, fit textFit, limit int) []string {
	words := strings.Fields(value)
	var lines []string
	for len(words) > 0 && len(lines) < limit {
		count := len(words)
		if len(lines) < limit-1 {
			count = lineWordCount(words, fit)
		}
		lines = append(lines, fitText(strings.Join(words[:count], " "), fit))
		words = words[count:]
	}
	return lines
}

// lineWordCount returns the number of words from the start of words that
// fit on one line of fit. The count is 1 or more, so a word that does not
// fit gets its own line.
func lineWordCount(words []string, fit textFit) int {
	count := 1
	for count < len(words) {
		if width, _ := text.Measure(strings.Join(words[:count+1], " "), fit.face, 0); width > fit.width {
			break
		}
		count++
	}
	return count
}
