package view

import (
	"cmp"
	"errors"
	"strings"
	"testing"
)

func TestRideButton(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		noOpener      bool
		panel         string
		operationsOff bool
		want          bool
	}{
		{name: "inspector", want: true},
		{name: "no ride page", noOpener: true},
		{name: "orders panel", panel: "orders"},
		{name: "demand panel", panel: "demand"},
		{name: "incidents panel", panel: incidentsAction},
		// The Incidents panel hides with the operations, so the inspector shows.
		{name: "incidents with operations off", panel: incidentsAction, operationsOff: true, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game := controlTestGame(t, controlLayouts[0].input)
			game.selected = 2
			if test.noOpener {
				game.openRide = nil
			}
			game.showOrders, game.showDemand, game.showIncidents = test.panel == "orders", test.panel == "demand", test.panel == incidentsAction
			game.state.Simulation.OperationsOff = test.operationsOff
			control, ok := game.rideButton(game.state.Simulation)
			if ok != test.want {
				t.Fatalf("shown = %v, want %v", ok, test.want)
			}
			if ok && control.action != rideActionPrefix+"03" {
				t.Fatalf("action = %q", control.action)
			}
		})
	}
}

func TestRidePress(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, before, want string
		err                error
	}{
		{name: "opens", before: "old error", want: ""},
		{name: "blocked", err: errors.New("blocked"), want: "blocked"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game := controlTestGame(t, controlLayouts[0].input)
			var opened []string
			game.openRide = func(podID string) error {
				opened = append(opened, podID)
				return test.err
			}
			game.message = test.before
			game.press(rideActionPrefix + "07")
			if len(opened) != 1 || opened[0] != "07" {
				t.Fatalf("opened = %q", opened)
			}
			if game.message != test.want {
				t.Fatalf("message = %q, want %q", game.message, test.want)
			}
			// A pod button press selects the pod and opens nothing.
			game.press("pod/05")
			if len(opened) != 1 {
				t.Fatalf("pod press opened %q", opened)
			}
		})
	}
}

func TestRideButtonFits(t *testing.T) {
	t.Parallel()
	for _, layout := range controlLayouts {
		t.Run(layout.name, func(t *testing.T) {
			t.Parallel()
			game := controlTestGame(t, layout.input)
			control, ok := game.rideButton(game.state.Simulation)
			if !ok {
				t.Fatal("ride button does not show")
			}
			control = game.layoutButton(control)
			if got := game.fitButtonText(control.label, cmp.Or(control.fontSize, 14), control.w); got != control.label {
				t.Errorf("label %q shortened to %q in width %g", control.label, got, control.w)
			}
			panel, got := game.rightPanelArea(), buttonArea(control)
			if got.left < panel.left || got.top < panel.top || got.right > panel.right || got.bottom > panel.bottom {
				t.Errorf("ride button %+v escapes right panel %+v", got, panel)
			}
			// The fourth inspector row, the station name of a maneuver, is
			// the lowest text of the inspector, and it can span the width.
			name := game.fitInspectionValue(inspectionRow{value: strings.Repeat("Edgware Road ", 10)})
			row := label{x: inspectionLeft, y: inspectionRowsTop + 3*inspectionRowSpacing, size: 14, value: name, color: foreground}
			if value := game.labelArea(row); value.overlaps(got) {
				t.Errorf("last inspector row %+v overlaps ride button %+v", value, got)
			}
		})
	}
}
