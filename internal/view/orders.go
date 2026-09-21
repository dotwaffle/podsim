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
}

func outstandingOrders(state sim.Snapshot) []orderRow {
	rows := make([]orderRow, 0, len(state.Pending)+len(state.Vehicles))
	for _, request := range state.Pending {
		rows = append(rows, orderRow{request: request, status: request.DispatchReason})
	}
	for _, v := range state.Vehicles {
		if v.Request == nil || v.Request.Completed {
			continue
		}
		rows = append(rows, orderRow{request: *v.Request, status: fmt.Sprintf("Pod %s / %s", v.Pod.ID, v.Pod.Activity)})
	}
	slices.SortFunc(rows, func(a, b orderRow) int { return a.request.ID - b.request.ID })
	return rows
}

func (g *Game) drawOrders(screen *ebiten.Image, state sim.Snapshot) {
	rows := outstandingOrders(state)
	g.label(screen, label{x: 816, y: 115, size: 12, value: "OUTSTANDING ORDERS", color: muted})
	g.label(screen, label{x: 816, y: 140, size: 14, value: fmt.Sprintf("Queued %d / active %d", len(state.Pending), len(rows)-len(state.Pending)), color: foreground})
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
