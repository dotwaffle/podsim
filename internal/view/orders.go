package view

import (
	"fmt"
	"slices"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/dotwaffle/podsim/internal/sim"
)

type orderRow struct {
	request sim.Request
	status  string
	parties int
	active  bool
}

func outstandingOrders(state sim.Snapshot) []orderRow {
	rows := make([]orderRow, 0, len(state.Pending)+len(state.Vehicles))
	for _, request := range state.Pending {
		rows = append(rows, orderRow{request: request, status: request.DispatchReason, parties: 1})
	}
	for _, v := range state.Vehicles {
		if v.Request == nil || v.Request.Completed {
			continue
		}
		parties := max(1, v.Parties)
		status := fmt.Sprintf("Pod %s / %s", v.Pod.ID, v.Pod.Activity)
		if parties > 1 {
			status += fmt.Sprintf(" / %d parties", parties)
		}
		rows = append(rows, orderRow{request: *v.Request, status: status, parties: parties, active: true})
	}
	slices.SortFunc(rows, func(a, b orderRow) int { return a.request.ID - b.request.ID })
	return rows
}

func outstandingOrderCount(state sim.Snapshot) int {
	count := len(state.Pending)
	for _, vehicle := range state.Vehicles {
		if vehicle.Request != nil && !vehicle.Request.Completed {
			count += max(1, vehicle.Parties)
		}
	}
	return count
}

func (g *Game) drawOrders(screen *ebiten.Image, state sim.Snapshot) {
	rows := outstandingOrders(state)
	g.label(screen, label{x: 816, y: 115, size: 12, value: "OUTSTANDING ORDERS", color: muted})
	active := 0
	for _, row := range rows {
		if row.active {
			active += row.parties
		}
	}
	g.label(screen, label{x: 816, y: 140, size: 14, value: fmt.Sprintf("Queued %d / active %d", len(state.Pending), active), color: foreground})
	if len(rows) == 0 {
		g.label(screen, label{x: 816, y: 185, size: 13, value: "No outstanding orders.", color: muted})
		return
	}
	for i, row := range rows[:min(4, len(rows))] {
		from, _ := g.network.Station(row.request.From)
		to, _ := g.network.Station(row.request.To)
		y := 180 + float64(i*47)
		g.label(screen, label{x: 816, y: y, size: 14, value: fmt.Sprintf("#%d  %s > %s", row.request.ID, from.Name, to.Name), color: foreground})
		g.label(screen, label{x: 816, y: y + 21, size: 11, value: row.status, color: muted})
	}
	if len(rows) > 4 {
		g.label(screen, label{x: 816, y: 365, size: 11, value: fmt.Sprintf("+%d more orders", len(rows)-4), color: muted})
	}
}
