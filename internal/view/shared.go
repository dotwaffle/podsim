package view

import (
	"fmt"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/dotwaffle/podsim/internal/session"
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
			g.notice = fmt.Sprintf("Order #%d accepted: %s > %s. See Orders for status.", result.Reply.OrderID, from.Name, to.Name)
			g.noticeTicks = 180
		case result.Command.Action == "reset" || result.Command.Action == "demo":
			g.showOrders, g.showDemand = false, false
			g.notice, g.noticeTicks = "", 0
			g.selected, g.podPage = 0, 0
			g.normalizeSelection()
		}
	default:
	}
}

func (g *Game) submit(command session.Command) {
	if err := g.client.Submit(command); err != nil {
		g.message = err.Error()
		return
	}
	g.pending = true
	g.message = "Sending command..."
	g.notice, g.noticeTicks = "", 0
}

func (g *Game) connectionLabel() string {
	if !g.connected {
		return "Connection lost or connecting. Controls resume when the server is available."
	}
	if g.pending {
		return "Shared session / waiting for command confirmation"
	}
	return "Shared session / connected. Playback, orders, and demand are shared across all browsers. Pod inspection stays local."
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
		if config.Pattern != "balanced" {
			config.Pattern = "balanced"
			config.Destination = ""
		} else {
			config.Pattern = "destination"
			config.Destination = g.destination
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
