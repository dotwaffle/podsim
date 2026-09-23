package view

import (
	"fmt"
	"slices"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/dotwaffle/podsim/internal/remote"
	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

func (g *Game) readRemote() {
	state, connected, pending := g.client.View()
	g.connected, g.pending = connected, pending
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
}

// handleResult shows the outcome of one command. Errors go to the message
// line. Some accepted commands show a notice.
func (g *Game) handleResult(result remote.Result) {
	g.message = ""
	switch {
	case result.Err != nil:
		g.message = result.Err.Error()
	case result.Reply.Error != "":
		g.message = result.Reply.Error
	case result.Command.Action == "trip":
		g.showOrders, g.showDemand = true, false
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
	case result.Command.Action == "reset" || result.Command.Action == "demo":
		g.showOrders, g.showDemand = false, false
		g.notice, g.noticeAction, g.noticeTicks = "", "", 0
		g.selected, g.podPage = 0, 0
		g.normalizeSelection()
	}
}

// showNotice shows text in the hint line for 180 ticks. action is the command
// that caused the notice.
func (g *Game) showNotice(action, text string) {
	g.notice, g.noticeAction, g.noticeTicks = text, action, 180
}

// rewindNotice describes an accepted rewind. It gives the time of the save
// point when the state still lists it. A reply project revision above the one
// at the click shows that the rewind restored a different project. The notice
// then says so.
func (g *Game) rewindNotice(result remote.Result) string {
	id := result.Command.Checkpoint
	notice := fmt.Sprintf("Rewound to save point #%d. Paused.", id)
	index := slices.IndexFunc(g.state.Checkpoints, func(entry session.Checkpoint) bool { return entry.ID == id })
	if index >= 0 {
		seconds := float64(g.state.Checkpoints[index].Tick) / sim.TicksPerSecond
		notice = fmt.Sprintf("Rewound to save point #%d (%.1f s). Paused.", id, seconds)
	}
	if result.Reply.ProjectRevision > g.rewindProjectRevision {
		notice += " Project settings restored."
	}
	return notice
}

func (g *Game) submit(command session.Command) {
	if err := g.client.Submit(command); err != nil {
		g.message = err.Error()
		return
	}
	g.pending = true
	g.message = "Sending command..."
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

func (g *Game) demandButtons() []button {
	config := g.state.Demand.Config
	pattern := "Balanced"
	switch config.Pattern {
	case "market":
		pattern = "Market-bound"
	case "destination":
		station, _ := g.network.Station(config.Destination)
		pattern = station.Name + "-bound"
	case "profile":
		pattern = config.Profile + " / " + config.Band
	}
	toggle := "Start demand"
	if config.Enabled {
		toggle = "Stop demand"
	}
	disabled := !g.connected || g.pending || g.state.Simulation.Demo
	return []button{
		{x: 810, y: 180, w: 250, h: 32, label: fmt.Sprintf("Rate: %d orders/min", config.PerMinute), disabled: disabled, action: "demand-rate"},
		{x: 810, y: 224, w: 250, h: 32, label: "Pattern: " + pattern, disabled: disabled, action: "demand-pattern"},
		{x: 810, y: 268, w: 250, h: 32, label: fmt.Sprintf("Seed: %d", config.Seed), disabled: disabled, action: "demand-seed"},
		{x: 810, y: 312, w: 250, h: 32, label: toggle, selected: config.Enabled, disabled: disabled, action: "demand-toggle"},
	}
}

func (g *Game) changeDemand(action string) {
	config := g.state.Demand.Config
	switch action {
	case "demand-rate":
		next := 1
		for _, rate := range []int{1, 2, 4, 8, 12} {
			if rate > config.PerMinute {
				next = rate
				break
			}
		}
		config.PerMinute = next
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
		config.Seed = config.Seed%9 + 1
	case "demand-toggle":
		config.Enabled = !config.Enabled
	}
	g.submit(session.Command{Action: "demand", Demand: config})
}

func (g *Game) drawDemand(screen *ebiten.Image) {
	demand := g.state.Demand
	g.label(screen, label{x: 816, y: 115, size: 12, value: "PASSENGER DEMAND", color: muted})
	g.label(screen, label{x: 816, y: 140, size: 11, value: "Per simulated minute / shared settings", color: foreground})
	g.label(screen, label{x: 816, y: 352, size: 11, value: fmt.Sprintf("Generated %d / skipped %d", demand.Generated, demand.Skipped), color: foreground})
	g.label(screen, label{x: 816, y: 371, size: 10, value: fmt.Sprintf("Reposition: %t / %d moves / %.0f m empty", g.state.Redistribution, g.state.Simulation.RebalanceMoves, g.state.Simulation.EmptyDistanceMeters), color: muted})
}
