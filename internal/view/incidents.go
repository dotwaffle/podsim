package view

import (
	"fmt"
	"slices"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

const (
	// incidentsAction is the action of the Incidents tab, and
	// incidentsPagerPrefix starts the actions of the page arrows.
	incidentsAction      = "incidents"
	incidentsPagerPrefix = "incidents-"
	// incidentClearPrefix starts the action of a Clear button of the panel.
	// The fault ID follows. The prefix differs from clearFaultActionPrefix,
	// because a frame keeps one fault button of the inspector and each
	// Clear button of the panel.
	incidentClearPrefix = "incident-clear/"
	// incidentRowsTop and incidentRowSpacing place the rows of the panel in
	// design units, and incidentRowHeight is the height of a Clear button.
	incidentRowsTop    = 214.0
	incidentRowSpacing = 24.0
	incidentRowHeight  = 20.0
	// incidentClearLeft and incidentClearWidth set the Clear buttons.
	incidentClearLeft  = 1004.0
	incidentClearWidth = 56.0
	// minimumIncidentRows is the number of rows in a window of the minimum
	// size.
	minimumIncidentRows = 4
)

// incidentRow is one row of the Incidents panel. A row with a fault ID has
// a Clear button. An emergency has none, because a user cannot cancel it.
type incidentRow struct {
	text, faultID string
}

// incidentRows returns the rows of the active incidents: the faults in
// record order, and then the emergencies. Each pod has its fleet number.
func incidentRows(state sim.Snapshot) []incidentRow {
	fleet := fleetNumbers(state.Vehicles)
	pod := func(id string) string {
		if number, ok := fleet[id]; ok {
			return number
		}
		return id
	}
	rows := make([]incidentRow, 0, len(state.Faults.Active)+len(state.Emergencies.Active))
	for _, fault := range state.Faults.Active {
		text := "Debris / " + fault.LaneID
		if fault.Kind == sim.FaultKindPod {
			text = fmt.Sprintf("Pod %s / %s / %s", pod(fault.PodID), fault.Phase, faultEndText(fault.EndTick))
		}
		rows = append(rows, incidentRow{text: text, faultID: fault.ID})
	}
	for _, emergency := range state.Emergencies.Active {
		rows = append(rows, incidentRow{text: fmt.Sprintf("Emergency / Pod %s / %s", pod(emergency.PodID), emergency.Phase)})
	}
	return rows
}

// faultEndText returns the end of a fault in simulated seconds. A fault with
// the end tick 0 lasts until a clear.
func faultEndText(tick int64) string {
	if tick == 0 {
		return "no end"
	}
	return "ends " + ticksText(tick)
}

// ticksText returns a number of ticks in simulated seconds. The counters of
// a kind move only while its sampler is live, so 0 shows as no number.
func ticksText(ticks int64) string {
	if ticks <= 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f s", float64(ticks)/sim.TicksPerSecond)
}

// meanTicksText returns the mean of total over count in simulated seconds.
func meanTicksText(total, count int64) string {
	if total <= 0 || count <= 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f s", float64(total)/float64(count)/sim.TicksPerSecond)
}

// incidentSummary returns the summary lines of the Incidents panel. The mean
// record length is EndedTicks / Ended, by section 5.5 of the incident stage
// 7 contract. EmergencyTicks also counts active and dropped records.
func incidentSummary(state sim.Snapshot) []string {
	faults, emergencies := state.Faults.Counters, state.Emergencies.Counters
	return []string{
		fmt.Sprintf("Active: %d faults / %d emergencies", len(state.Faults.Active), len(state.Emergencies.Active)),
		fmt.Sprintf("Sampled: %d faults / %d emergencies", faults.Sampled, emergencies.Sampled),
		fmt.Sprintf("Skipped: %d faults / %d emergencies", faults.Skipped, emergencies.Skipped),
		fmt.Sprintf("Interrupted orders: %d", state.Interrupted),
		fmt.Sprintf("Faults: mean %s / rider delay %s", meanTicksText(faults.ClearedTicks, faults.Cleared), ticksText(faults.DelayedRiderTicks)),
		fmt.Sprintf("Emergencies: mean %s / longest %s", meanTicksText(emergencies.EndedTicks, emergencies.Ended), ticksText(emergencies.MaxTicks)),
	}
}

// incidentsOpen reports whether the frame shows the Incidents panel. The
// panel hides with the operations.
func (g *Game) incidentsOpen(state sim.Snapshot) bool {
	return g.showIncidents && !state.OperationsOff
}

// closeIncidents closes the Incidents panel and ends an armed debris click.
func (g *Game) closeIncidents() {
	g.showIncidents, g.debrisArmed = false, false
}

// incidentRowLimit returns the number of rows that fit above the page
// controls when the right panel is extra design units taller than in a
// window of the minimum size.
func incidentRowLimit(extra float64) int {
	return max(minimumIncidentRows, int((orderPagerTop+extra-incidentRowsTop)/incidentRowSpacing))
}

// currentIncidentPage returns the page of rows that the panel shows.
func (g *Game) currentIncidentPage(rows []incidentRow) orderPage {
	return pageOrders(orderPageInput{rows: len(rows), perPage: incidentRowLimit(g.layout.extraY / g.layout.unit), page: g.incidentPage})
}

// turnIncidentPage moves the panel by step pages from the page that it
// shows.
func (g *Game) turnIncidentPage(step int) {
	page := g.currentIncidentPage(incidentRows(g.state.Simulation))
	g.incidentPage = min(max(0, page.page+step), page.pages-1)
}

// incidentButtons returns the buttons of the open Incidents panel: Place
// debris, a Clear button for each fault row of the page, and the page arrows.
func (g *Game) incidentButtons(state sim.Snapshot) []button {
	rows := incidentRows(state)
	page := g.currentIncidentPage(rows)
	buttons := []button{g.placeDebrisButton()}
	for i, row := range rows[page.start:page.end] {
		if row.faultID != "" {
			buttons = append(buttons, button{
				x: incidentClearLeft, y: incidentRowsTop + float64(i)*incidentRowSpacing, w: incidentClearWidth, h: incidentRowHeight,
				label: "Clear", action: incidentClearPrefix + row.faultID, fontSize: 11, disabled: !g.connected || g.pending,
			})
		}
	}
	return append(buttons, pagerButtons(page, incidentsPagerPrefix)...)
}

// isIncidentClear reports whether control is a Clear button of the panel.
func isIncidentClear(control button) bool {
	return strings.HasPrefix(control.action, incidentClearPrefix)
}

// pressIncidents applies a press of the Incidents tab, of Place debris, of a
// page arrow of the panel, or of a Clear button. It returns false for any other action.
func (g *Game) pressIncidents(action string) bool {
	switch action {
	case incidentsAction:
		g.showIncidents, g.debrisArmed = !g.showIncidents, false
		g.showOrders, g.showDemand = false, false
	case placeDebrisAction:
		g.debrisArmed = !g.debrisArmed
		g.message = ""
	case incidentsPagerPrefix + "prev":
		g.turnIncidentPage(-1)
	case incidentsPagerPrefix + "next":
		g.turnIncidentPage(1)
	default:
		faultID, ok := strings.CutPrefix(action, incidentClearPrefix)
		if !ok {
			return false
		}
		g.submit(session.Command{Action: "clearFault", FaultID: faultID})
	}
	return true
}

// keepIncidentClears keeps the Clear buttons of a new frame for
// clickButtons.
func (g *Game) keepIncidentClears(buttons []button) {
	g.shownClears = g.shownClears[:0]
	for _, control := range buttons {
		if isIncidentClear(control) {
			g.shownClears = append(g.shownClears, control)
		}
	}
}

// shownIncidentClears replaces the Clear buttons in buttons with those of
// the last drawn frame, so that a click sends the fault ID of the row that
// the user saw. The server refuses an ID that is gone, and the message line
// shows the error.
func (g *Game) shownIncidentClears(buttons []button) []button {
	buttons = slices.DeleteFunc(buttons, isIncidentClear)
	for _, control := range g.shownClears {
		control.disabled = !g.connected || g.pending
		buttons = append(buttons, control)
	}
	return buttons
}

func (g *Game) drawIncidents(screen *ebiten.Image, state sim.Snapshot) {
	for _, value := range g.incidentLabels(state) {
		g.label(screen, value)
	}
}

// incidentLabels returns the text of the Incidents panel for state. The
// labels use physical pixels, as the Orders panel does, so that a row in a
// tall window stays beside its Clear button.
func (g *Game) incidentLabels(state sim.Snapshot) []label {
	panelLabel := func(y, size, width float64, value string, shade uint32) label {
		value = g.fitText(value, size, width)
		return label{x: g.layout.right(orderTextLeft), y: g.layout.y(y), size: size, value: value, color: shade, physical: true}
	}
	const fullWidth = orderTextRight - orderTextLeft
	labels := []label{panelLabel(83, 12, fullWidth, "INCIDENTS", muted)}
	for i, line := range incidentSummary(state) {
		size, shade := 10.0, uint32(muted)
		if i == 0 {
			size, shade = 11, foreground
		}
		labels = append(labels, panelLabel(127+float64(i)*14, size, fullWidth, line, shade))
	}
	rows := incidentRows(state)
	if len(rows) == 0 {
		return append(labels, panelLabel(incidentRowsTop+3, 13, fullWidth, "No active incidents.", muted))
	}
	page := g.currentIncidentPage(rows)
	for i, row := range rows[page.start:page.end] {
		width := fullWidth
		if row.faultID != "" {
			width = incidentClearLeft - 6 - orderTextLeft
		}
		labels = append(labels, panelLabel(incidentRowsTop+float64(i)*incidentRowSpacing+2, 11, width, row.text, foreground))
	}
	if page.pages > 1 {
		labels = append(labels, g.pagerLabel(page))
	}
	return labels
}
