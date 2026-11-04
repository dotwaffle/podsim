package view

import (
	"fmt"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

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
	case "rail-arrivals":
		pattern = "Rail arrivals"
	case "rail-services":
		pattern = "Rail services"
	case "profile":
		pattern = config.Band + " / " + config.Profile
	case "profile-daily":
		pattern = "Daily / " + config.Profile
	}
	return "Pattern: " + pattern
}

// demandButtons returns the buttons of the Demand panel for state. network
// gives the name of the destination station. busy disables the buttons.
func demandButtons(state session.State, network sim.Network, busy bool) []button {
	config := state.Demand.Config
	destination, _ := network.Station(config.Destination)
	toggle := "Start demand"
	if config.Enabled {
		toggle = "Stop demand"
	}
	rateLabel := fmt.Sprintf("Rate: %d orders/min", config.PerMinute)
	if config.Pattern == "rail-arrivals" {
		rateLabel = "Rate: scheduled arrivals"
	}
	if config.Pattern == "rail-services" {
		rateLabel = "Rate: scheduled services"
	}
	buttons := []button{
		{x: 810, y: 144, w: 250, h: 24, label: rateLabel, disabled: busy || config.Pattern == "rail-arrivals" || config.Pattern == "rail-services", action: "demand-rate"},
		{x: 810, y: 174, w: 250, h: 24, label: demandPatternLabel(config, destination.Name), disabled: busy, action: "demand-pattern"},
		{x: 810, y: 204, w: 250, h: 24, label: fmt.Sprintf("Seed: %d", config.Seed), disabled: busy, action: "demand-seed"},
		{x: 810, y: 234, w: 250, h: 24, label: toggle, selected: config.Enabled, disabled: busy, action: "demand-toggle"},
	}
	// The server starts the demo only for the supplied example project.
	if state.DemoAvailable {
		buttons = append(buttons, button{x: demoButtonLeft, y: demoButtonTop, w: demoButtonWidth, h: demoButtonHeight, label: demoButtonLabel, disabled: busy, action: "demo", fontSize: demoButtonFontSize})
	}
	return buttons
}

const (
	// demoButtonLabel is the label of the button that starts the traffic
	// demo.
	demoButtonLabel = "Start traffic demo"
	// demoButtonTop is the top of Start traffic demo in design units. The
	// button moves down with the pod selector below it, so it stays at the
	// bottom of the Demand panel in a tall window.
	demoButtonTop = podSelectorTop - 32
	// demoButtonLeft, demoButtonWidth and demoButtonHeight set the other
	// edges of Start traffic demo in design units. The button ends at the
	// right edge of the panel buttons.
	demoButtonLeft   = 940.0
	demoButtonWidth  = 120.0
	demoButtonHeight = 26.0
	// demoButtonFontSize is the font size of Start traffic demo.
	demoButtonFontSize = 12.0
)

// nextDemandConfig returns config after a press of the Demand panel button
// with action. destination is the selected To station.
func nextDemandConfig(config session.DemandConfig, action, destination string) session.DemandConfig {
	switch action {
	case "demand-rate":
		config.PerMinute = nextDemandRate(config.PerMinute)
	case "demand-pattern":
		config = nextDemandPattern(config, destination)
	case "demand-seed":
		config.Seed = nextDemandSeed(config.Seed)
	case "demand-toggle":
		config.Enabled = !config.Enabled
	}
	return config
}

func nextDemandPattern(config session.DemandConfig, destination string) session.DemandConfig {
	config.DailyStartMinute = 0
	switch {
	case config.Pattern == "balanced":
		config.Pattern, config.Destination = "destination", destination
	case config.Pattern == "destination" && config.Profile != "" && config.Band != "":
		config.Pattern = "profile"
	case config.Pattern == "profile":
		config.Pattern, config.Destination = "balanced", ""
	default:
		config.Pattern = "balanced"
	}
	return config
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
	for _, value := range demandLabels(g.state, g.demandTextFit()) {
		g.label(screen, value)
	}
}

// demandTextFit returns the fit of the small text in the Demand panel.
func (g *Game) demandTextFit() textFit {
	return textFit{face: g.textFace(10), width: demandTextWidth * g.layout.unit}
}

const (
	// demandTextWidth is the width of the Demand panel text, from the
	// left edge of the text to the right edge of the buttons.
	demandTextWidth = 244
	// demandErrorLines is the largest number of lines for the demand
	// error.
	demandErrorLines = 2
)

// demandLabels returns the text of the Demand panel for state. fit is the
// fit of the small text. Below the counters, the last demand error shows in
// amber on at most demandErrorLines lines.
func demandLabels(state session.State, fit textFit) []label {
	demand := state.Demand
	labels := []label{
		{x: 816, y: 83, size: 12, value: "PASSENGER DEMAND", color: muted},
		{x: 816, y: 108, size: 11, value: "Per simulated minute / shared settings", color: foreground},
		{x: 816, y: 126, size: 10, value: demandSavesNote, color: muted},
		{x: 816, y: 264, size: 11, value: fmt.Sprintf("Generated %d / skipped %d", demand.Generated, demand.Skipped), color: foreground},
		{x: 816, y: 282, size: 10, value: fitText(redistributionText(state), fit), color: muted},
	}
	if c := demand.Connections; c.Made+c.Missed+c.Unserved+c.Unresolved > 0 {
		labels[1].value = fmt.Sprintf("Trains: %d made / %d missed", c.Made, c.Missed)
		labels[2].value = fmt.Sprintf("%d unserved / %d unresolved", c.Unserved, c.Unresolved)
	}
	for index, line := range wrapText(demand.Error, fit, demandErrorLines) {
		labels = append(labels, label{x: 816, y: 299 + 13*float64(index), size: 10, value: line, color: amber})
	}
	return labels
}

// redistributionText returns the redistribution line of the Demand panel
// for state. The line tells if redistribution is on. It gives the number of
// guarded positioning moves and the empty share of all pod distance since
// reset, including pickup trips. A guarded positioning move sends an idle
// empty pod to a busy station that has no pod.
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
	share := "-"
	empty := state.Simulation.EmptyDistanceMeters
	if total := empty + state.Simulation.PassengerDistanceMeters; total > 0 {
		share = fmt.Sprintf("%.1f%%", 100*empty/total)
	}
	return fmt.Sprintf("Redistribution: %s / %d %s / %s distance empty", setting, moves, unit, share)
}
