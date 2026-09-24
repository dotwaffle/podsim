package view

import (
	"fmt"
	"image"
	"math"
	"slices"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/dotwaffle/podsim/internal/sim"
)

type orderRow struct {
	request sim.Request
	status  string
	parties int
	active  bool
}

// outstandingOrders returns the queued and active orders of state in order
// ID order. The status text names each pod by its fleet number.
func outstandingOrders(state sim.Snapshot) []orderRow {
	fleet := fleetNumbers(state.Vehicles)
	rows := make([]orderRow, 0, len(state.Pending)+len(state.Vehicles))
	for _, request := range state.Pending {
		rows = append(rows, orderRow{request: request, status: fleetDispatchReason(request.DispatchReason, fleet), parties: 1})
	}
	for i, v := range state.Vehicles {
		if v.Request == nil || v.Request.Completed {
			continue
		}
		parties := max(1, v.Parties)
		status := fmt.Sprintf("Pod %s / %s", fleetPodLabel(i), v.Pod.Activity)
		if parties > 1 {
			status += fmt.Sprintf(" / %d parties", parties)
		}
		rows = append(rows, orderRow{request: *v.Request, status: status, parties: parties, active: true})
	}
	slices.SortFunc(rows, func(a, b orderRow) int { return a.request.ID - b.request.ID })
	return rows
}

// fleetNumbers returns the fleet number of each pod by pod ID. The fleet
// number is the label of the pod button.
func fleetNumbers(vehicles []sim.Vehicle) map[string]string {
	numbers := make(map[string]string, len(vehicles))
	for i, v := range vehicles {
		numbers[v.Pod.ID] = fleetPodLabel(i)
	}
	return numbers
}

// fleetDispatchReason returns reason with fleet numbers in place of pod IDs.
// The simulation names a pod with the word "Pod" or "pod" and then the pod
// ID, for example "Pod london-pod-008 traveling to pickup" or "Waiting for
// pod london-pod-021 to finish". A word that is not a pod ID in fleet does
// not change.
func fleetDispatchReason(reason string, fleet map[string]string) string {
	words := strings.Split(reason, " ")
	for i := 1; i < len(words); i++ {
		if words[i-1] != "Pod" && words[i-1] != "pod" {
			continue
		}
		if number, ok := fleet[words[i]]; ok {
			words[i] = number
		}
	}
	return strings.Join(words, " ")
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
	orderRowsTop = 168.0
	// orderRowSpacing is the distance between order rows in design units.
	orderRowSpacing = 37.0
	// orderStatusOffset is the distance in design units from the top of an
	// order row to its status line.
	orderStatusOffset = 18.0
	// orderTextLeft and orderTextRight are the edges of the order text in
	// design units.
	orderTextLeft  = 816.0
	orderTextRight = 1060.0
	// orderPagerTop is the top of the page controls in design units. The
	// page controls move down with the pod selector below them.
	orderPagerTop = podSelectorTop - 32
	// orderPagerHeight is the height of the page controls in design units.
	orderPagerHeight = 24.0
	// orderPagerArrowWidth is the width of a page arrow in design units.
	orderPagerArrowWidth = 32.0
	// minimumOrderRows is the number of order rows in a window of the
	// minimum size.
	minimumOrderRows = 5
)

// orderRowLimit returns the number of order rows that fit above the page
// controls when the right panel is extra design units taller than in a
// window of the minimum size.
func orderRowLimit(extra float64) int {
	fit := int((orderPagerTop + extra - orderRowsTop) / orderRowSpacing)
	return max(minimumOrderRows, fit)
}

// orderPage is one page of the Orders panel.
type orderPage struct {
	// start and end are the indexes of the first row and the row after the
	// last row of the page.
	start, end int
	// page is the zero-based page number, and pages is the number of pages.
	pages, page int
}

// orderPageInput holds the values that select a page of order rows.
type orderPageInput struct {
	// rows is the number of order rows.
	rows int
	// perPage is the number of rows on one page.
	perPage int
	// page is the requested zero-based page. A page past the last page
	// selects the last page.
	page int
}

// pageOrders returns the page of order rows that input selects. With no
// rows, there is one empty page.
func pageOrders(input orderPageInput) orderPage {
	perPage := max(1, input.perPage)
	pages := max(1, (input.rows+perPage-1)/perPage)
	page := min(max(0, input.page), pages-1)
	start := page * perPage
	return orderPage{start: start, end: min(input.rows, start+perPage), pages: pages, page: page}
}

// currentOrderPage returns the page of rows that the Orders panel shows.
func (g *Game) currentOrderPage(rows []orderRow) orderPage {
	return pageOrders(orderPageInput{rows: len(rows), perPage: orderRowLimit(g.layout.extraY / g.layout.unit), page: g.orderPage})
}

// orderPagerButtons returns the previous and next page arrows of the Orders
// panel. There are no arrows when all the rows fit on one page.
func (g *Game) orderPagerButtons(state sim.Snapshot) []button {
	page := g.currentOrderPage(outstandingOrders(state))
	if page.pages < 2 {
		return nil
	}
	return []button{
		{x: 810, y: orderPagerTop, w: orderPagerArrowWidth, h: orderPagerHeight, label: "‹", disabled: page.page == 0, action: "orders-prev"},
		{x: 1060 - orderPagerArrowWidth, y: orderPagerTop, w: orderPagerArrowWidth, h: orderPagerHeight, label: "›", disabled: page.page == page.pages-1, action: "orders-next"},
	}
}

// turnOrderPage moves the Orders panel by step pages from the page that it
// shows.
func (g *Game) turnOrderPage(step int) {
	page := g.currentOrderPage(outstandingOrders(g.state.Simulation))
	g.orderPage = min(max(0, page.page+step), page.pages-1)
}

func (g *Game) drawOrders(screen *ebiten.Image, state sim.Snapshot) {
	for _, value := range g.orderLabels(state) {
		g.label(screen, value)
	}
}

// orderLabels returns the text of the Orders panel for state. A tall window
// shows more rows on each page. Each row fits the panel width. The labels
// use physical pixels. In design units, rows at or below the top of the pod
// selector would move down with the pod selector.
func (g *Game) orderLabels(state sim.Snapshot) []label {
	panelLabel := func(y, size float64, value string, shade uint32) label {
		value = g.fitText(value, size, orderTextRight-orderTextLeft)
		return label{x: g.layout.right(orderTextLeft), y: g.layout.y(y), size: size, value: value, color: shade, physical: true}
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
	page := g.currentOrderPage(rows)
	for i, row := range rows[page.start:page.end] {
		from, _ := g.network.Station(row.request.From)
		to, _ := g.network.Station(row.request.To)
		y := orderRowsTop + float64(i)*orderRowSpacing
		labels = append(labels,
			panelLabel(y, 14, fmt.Sprintf("#%d  %s > %s", row.request.ID, from.Name, to.Name), foreground),
			panelLabel(y+orderStatusOffset, 11, row.status, muted))
	}
	if page.pages > 1 {
		unit := g.layout.unit
		left := g.layout.right(810 + orderPagerArrowWidth)
		top := g.layout.bottom(orderPagerTop)
		area := image.Rect(int(math.Round(left)), int(math.Round(top)), int(math.Round(g.layout.right(1060-orderPagerArrowWidth))), int(math.Round(top+orderPagerHeight*unit)))
		value := fmt.Sprintf("Page %d of %d", page.page+1, page.pages)
		labels = append(labels, g.centerLabel(area, label{size: 12, value: value, color: muted}))
	}
	return labels
}
