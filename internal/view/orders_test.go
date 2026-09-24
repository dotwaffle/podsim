package view

import (
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

func TestOutstandingOrderCountIncludesSharedParties(t *testing.T) {
	t.Parallel()
	state := sim.Snapshot{
		Pending: []sim.Request{{ID: 3}},
		Vehicles: []sim.Vehicle{{
			Pod: sim.Pod{ID: "01"}, Request: &sim.Request{ID: 1}, Parties: 3,
		}},
	}
	if got := outstandingOrderCount(state); got != 4 {
		t.Fatalf("outstanding order count = %d", got)
	}
	rows := outstandingOrders(state)
	if len(rows) != 2 || rows[0].parties != 3 || !rows[0].active {
		t.Fatalf("outstanding rows = %+v", rows)
	}
}

func TestFleetDispatchReason(t *testing.T) {
	t.Parallel()
	fleet := fleetNumbers([]sim.Vehicle{{Pod: sim.Pod{ID: "london-pod-001"}}, {Pod: sim.Pod{ID: "london-pod-008"}}})
	tests := []struct {
		name, reason, want string
	}{
		{name: "traveling to pickup", reason: "Pod london-pod-008 traveling to pickup", want: "Pod 02 traveling to pickup"},
		{name: "waiting in traffic", reason: "Pod london-pod-001 waiting in traffic", want: "Pod 01 waiting in traffic"},
		{name: "waiting for a pod to finish", reason: "Waiting for pod london-pod-008 to finish", want: "Waiting for pod 02 to finish"},
		{name: "no pod", reason: "Waiting for destination access", want: "Waiting for destination access"},
		{name: "empty", reason: "", want: ""},
		{name: "unknown pod", reason: "Pod other traveling to pickup", want: "Pod other traveling to pickup"},
		{name: "pod ID without the word pod", reason: "Behind london-pod-001", want: "Behind london-pod-001"},
		{name: "part of a pod ID", reason: "Pod london-pod-00 traveling to pickup", want: "Pod london-pod-00 traveling to pickup"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := fleetDispatchReason(test.reason, fleet); got != test.want {
				t.Fatalf("fleetDispatchReason(%q) = %q, want %q", test.reason, got, test.want)
			}
		})
	}
}

// TestOutstandingOrdersUseFleetNumbers checks that the status of queued and
// active orders names pods by their fleet number and not by their pod ID.
func TestOutstandingOrdersUseFleetNumbers(t *testing.T) {
	t.Parallel()
	state := sim.Snapshot{
		Pending: []sim.Request{
			{ID: 2, PodID: "london-pod-008", DispatchReason: "Pod london-pod-008 traveling to pickup"},
			{ID: 4, DispatchReason: "Waiting for pod london-pod-003 to finish"},
		},
		Vehicles: []sim.Vehicle{
			{Pod: sim.Pod{ID: "london-pod-003", Activity: sim.Traveling}, Request: &sim.Request{ID: 1}, Parties: 2},
			{Pod: sim.Pod{ID: "london-pod-008", Activity: sim.Idle}},
		},
	}
	var got []string
	for _, row := range outstandingOrders(state) {
		got = append(got, row.status)
	}
	want := []string{"Pod 01 / " + string(sim.Traveling) + " / 2 parties", "Pod 02 traveling to pickup", "Waiting for pod 01 to finish"}
	if !slices.Equal(got, want) {
		t.Fatalf("statuses = %q, want %q", got, want)
	}
}

func TestPageOrders(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input orderPageInput
		want  orderPage
	}{
		{name: "no rows", input: orderPageInput{perPage: 5}, want: orderPage{pages: 1}},
		{name: "one page", input: orderPageInput{rows: 5, perPage: 5}, want: orderPage{end: 5, pages: 1}},
		{name: "first of three", input: orderPageInput{rows: 12, perPage: 5}, want: orderPage{end: 5, pages: 3}},
		{name: "second of three", input: orderPageInput{rows: 12, perPage: 5, page: 1}, want: orderPage{start: 5, end: 10, pages: 3, page: 1}},
		{name: "short last page", input: orderPageInput{rows: 12, perPage: 5, page: 2}, want: orderPage{start: 10, end: 12, pages: 3, page: 2}},
		{name: "past the last page", input: orderPageInput{rows: 12, perPage: 5, page: 9}, want: orderPage{start: 10, end: 12, pages: 3, page: 2}},
		{name: "negative page", input: orderPageInput{rows: 12, perPage: 5, page: -1}, want: orderPage{end: 5, pages: 3}},
		{name: "no page size", input: orderPageInput{rows: 2}, want: orderPage{end: 1, pages: 2}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := pageOrders(test.input); got != test.want {
				t.Fatalf("pageOrders(%+v) = %+v, want %+v", test.input, got, test.want)
			}
		})
	}
}

// ordersTestGame returns a game in the layout of input that shows the
// Orders panel with count queued orders between two stations with long
// names.
func ordersTestGame(t *testing.T, input layoutInput, count int) *Game {
	t.Helper()
	game := controlTestGame(t, input)
	game.network.Stations[0].Name = "Battersea Power Station Underground Station"
	game.network.Stations[1].Name = "Cannon Street Underground Station"
	game.showOrders = true
	for i := range count {
		game.state.Simulation.Pending = append(game.state.Simulation.Pending, sim.Request{
			ID: i + 1, From: "station-01", To: "station-02", PodID: "01",
			DispatchReason: "Pod 01 waiting in traffic behind a very long queue of other pods",
		})
	}
	return game
}

// TestOrderLabelsFitThePanel checks that every order row and the page count
// fit between the text edges of the Orders panel, above the pod selector,
// and do not overlap the page arrows.
func TestOrderLabelsFitThePanel(t *testing.T) {
	t.Parallel()
	for _, layout := range controlLayouts {
		t.Run(layout.name, func(t *testing.T) {
			t.Parallel()
			game := ordersTestGame(t, layout.input, 200)
			left, right := game.layout.right(orderTextLeft), game.layout.right(orderTextRight)
			controls := game.buttons()
			selector := findButton(t, controls, "pod/01")
			arrows := []button{findButton(t, controls, "orders-prev"), findButton(t, controls, "orders-next")}
			for _, arrow := range arrows {
				if got := buttonArea(arrow); got.bottom > selector.y || got.top < game.layout.y(orderRowsTop) {
					t.Errorf("arrow %q %+v is not between the rows and the pod selector at %g", arrow.action, got, selector.y)
				}
			}
			labels := game.orderLabels(game.state.Simulation)
			if rows := (len(labels) - 3) / 2; rows != orderRowLimit(game.layout.extraY/game.layout.unit) {
				t.Errorf("page shows %d rows, want %d", rows, orderRowLimit(game.layout.extraY/game.layout.unit))
			}
			for _, value := range labels {
				got := game.labelArea(value)
				if got.left < left-1e-9 || got.right > right+1e-9 || got.bottom > selector.y {
					t.Errorf("label %q %+v is not inside x %g to %g above y %g", value.value, got, left, right, selector.y)
				}
				for _, arrow := range arrows {
					if got.overlaps(buttonArea(arrow)) {
						t.Errorf("label %q %+v overlaps arrow %q", value.value, got, arrow.action)
					}
				}
			}
		})
	}
}

// TestOrderPageArrows checks that the page arrows turn the Orders pages,
// stop at the first and last page, and hide when all rows fit on one page.
func TestOrderPageArrows(t *testing.T) {
	t.Parallel()
	game := ordersTestGame(t, layoutInput{outsideWidth: 1100, outsideHeight: 760, deviceScale: 1}, 12)
	firstRow := func() string { return game.orderLabels(game.state.Simulation)[2].value }
	pageCount := func() string {
		labels := game.orderLabels(game.state.Simulation)
		return labels[len(labels)-1].value
	}
	press := func(action string) {
		t.Helper()
		control := findButton(t, game.buttons(), action)
		if control.disabled {
			t.Fatalf("arrow %q is disabled", action)
		}
		game.click(centerOfButton(control))
	}
	if !findButton(t, game.buttons(), "orders-prev").disabled || pageCount() != "Page 1 of 3" {
		t.Fatalf("first page: prev disabled %t, count %q", findButton(t, game.buttons(), "orders-prev").disabled, pageCount())
	}
	press("orders-next")
	press("orders-next")
	if !strings.HasPrefix(firstRow(), "#11 ") || pageCount() != "Page 3 of 3" || !findButton(t, game.buttons(), "orders-next").disabled {
		t.Fatalf("last page: first row %q, count %q", firstRow(), pageCount())
	}
	press("orders-prev")
	if !strings.HasPrefix(firstRow(), "#6 ") || pageCount() != "Page 2 of 3" {
		t.Fatalf("second page: first row %q, count %q", firstRow(), pageCount())
	}
	game.state.Simulation.Pending = game.state.Simulation.Pending[:3]
	for _, control := range game.buttons() {
		if strings.HasPrefix(control.action, "orders-") {
			t.Fatalf("one page shows arrow %q", control.action)
		}
	}
	if !strings.HasPrefix(firstRow(), "#1 ") {
		t.Fatalf("one page: first row %q", firstRow())
	}
}
