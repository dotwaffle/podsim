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

const (
	// orderRowsTop is the top of the first order row in design units.
	orderRowsTop = 180.0
	// orderRowSpacing is the distance between order rows in design units.
	orderRowSpacing = 47.0
	// orderMoreHeight is the height in design units that the "+N more
	// orders" line needs below the last row. It includes the space above
	// the pod selector.
	orderMoreHeight = 15.0
	// minimumOrderRows is the number of order rows in a window of the
	// minimum size.
	minimumOrderRows = 4
)

// orderRowLimit returns the number of order rows that fit above the pod
// selector when the right panel is extra design units taller than in a
// window of the minimum size.
func orderRowLimit(extra float64) int {
	fit := int((podSelectorTop + extra - orderRowsTop - orderMoreHeight) / orderRowSpacing)
	return max(minimumOrderRows, fit)
}

func (g *Game) drawOrders(screen *ebiten.Image, state sim.Snapshot) {
	for _, value := range g.orderLabels(state) {
		g.label(screen, value)
	}
}

// orderLabels returns the text of the Orders panel for state. A tall window
// shows more rows. The labels use physical pixels. In design units, rows at
// or below the top of the pod selector would move down with the pod selector.
func (g *Game) orderLabels(state sim.Snapshot) []label {
	panelLabel := func(y, size float64, value string, shade uint32) label {
		return label{x: g.layout.right(816), y: g.layout.y(y), size: size, value: value, color: shade, physical: true}
	}
	rows := outstandingOrders(state)
	active := 0
	for _, row := range rows {
		if row.active {
			active += row.parties
		}
	}
	labels := []label{
		panelLabel(115, 12, "OUTSTANDING ORDERS", muted),
		panelLabel(140, 14, fmt.Sprintf("Queued %d / active %d", len(state.Pending), active), foreground),
	}
	if len(rows) == 0 {
		return append(labels, panelLabel(185, 13, "No outstanding orders.", muted))
	}
	limit := orderRowLimit(g.layout.extraY / g.layout.unit)
	for i, row := range rows[:min(limit, len(rows))] {
		from, _ := g.network.Station(row.request.From)
		to, _ := g.network.Station(row.request.To)
		y := orderRowsTop + float64(i)*orderRowSpacing
		labels = append(labels,
			panelLabel(y, 14, fmt.Sprintf("#%d  %s > %s", row.request.ID, from.Name, to.Name), foreground),
			panelLabel(y+21, 11, row.status, muted))
	}
	if len(rows) > limit {
		y := orderRowsTop + float64(limit)*orderRowSpacing - 3
		labels = append(labels, panelLabel(y, 11, fmt.Sprintf("+%d more orders", len(rows)-limit), muted))
	}
	return labels
}
