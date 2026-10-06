package view

import (
	"math"
	"slices"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// TestDemoButtonState checks when Start traffic demo accepts a press. The
// button sends a command, so a lost connection, a command that waits, and
// a running demo disable it. A press of the disabled button shows no
// confirmation. The button shows only in the Demand panel, and only when
// the topology allows the demo.
func TestDemoButtonState(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                     string
		connected, pending, demo bool
		wantDisabled             bool
	}{
		{name: "connected", connected: true},
		{name: "connection lost", wantDisabled: true},
		{name: "command waits", connected: true, pending: true, wantDisabled: true},
		{name: "traffic demo", connected: true, demo: true, wantDisabled: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game := journeyTestGame(t, 2)
			game.connected, game.pending = test.connected, test.pending
			game.state.Simulation.Demo = test.demo
			game.showDemand = true
			if slices.ContainsFunc(game.buttons(), func(control button) bool { return control.action == "demo" }) {
				t.Fatal("Start traffic demo shows when the topology does not allow the demo")
			}
			game.state.DemoAvailable = true
			game.showDemand = false
			if slices.ContainsFunc(game.buttons(), func(control button) bool { return control.action == "demo" }) {
				t.Fatal("Start traffic demo shows with the Demand panel closed")
			}
			game.showDemand = true
			control := findButton(t, game.buttons(), "demo")
			if control.label != demoButtonLabel || control.disabled != test.wantDisabled {
				t.Fatalf("button %q disabled %t, want %q disabled %t", control.label, control.disabled, demoButtonLabel, test.wantDisabled)
			}
			game.click(centerOfButton(control))
			if got := game.notice.action == demoConfirmAction; got == test.wantDisabled {
				t.Errorf("press shows the confirmation %t, want %t", got, !test.wantDisabled)
			}
		})
	}
}

// TestDemoAsksAgain presses Start traffic demo and Reset in a game that is
// connected to a session of the example project. Only a second press of
// Start traffic demo within 3 s sends the demo command, and only while the
// hint line shows the demo confirmation. A press of the other button shows
// the confirmation of that button and sends nothing. The test counts game
// ticks, not wall time.
func TestDemoAsksAgain(t *testing.T) {
	t.Parallel()
	const window = 3 * sim.TicksPerSecond
	type press struct {
		// ticks is the number of game ticks before the press.
		ticks int
		// action is the action of the pressed button.
		action string
		// wantDemo is true when the press sends the demo command.
		wantDemo bool
	}
	confirmations := map[string]string{"demo": demoConfirmNotice, "reset": resetConfirmNotice}
	tests := []struct {
		name    string
		presses []press
	}{
		{name: "one press", presses: []press{{action: "demo"}}},
		{name: "second press at once", presses: []press{{action: "demo"}, {action: "demo", wantDemo: true}}},
		{name: "second press in the last tick", presses: []press{{action: "demo"}, {ticks: window - 1, action: "demo", wantDemo: true}}},
		{name: "second press after the window", presses: []press{{action: "demo"}, {ticks: window, action: "demo"}}},
		{name: "third press after the window", presses: []press{{action: "demo"}, {ticks: window, action: "demo"}, {ticks: 1, action: "demo", wantDemo: true}}},
		{name: "press after the reset confirmation", presses: []press{{action: "reset"}, {action: "demo"}}},
		{name: "reset after the demo confirmation", presses: []press{{action: "demo"}, {action: "reset"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game := sharedTestGame(t)
			for index, press := range test.presses {
				for range press.ticks {
					game.tickNotice()
				}
				game.showDemand = true
				if press.wantDemo {
					// The user must see the confirmation before the press
					// that starts the demo.
					if got := game.hintLine(game.state.Simulation, ""); got.value != demoConfirmNotice {
						t.Fatalf("hint line before press %d = %q, want %q", index+1, got.value, demoConfirmNotice)
					}
					if result := clickCommand(t, game, "demo"); result.Command.Action != "demo" {
						t.Fatalf("press %d sent %q, want demo", index+1, result.Command.Action)
					}
					syncGame(t, game, func() bool { return game.state.Simulation.Demo })
					// An accepted demo closes the Demand panel. The button
					// is disabled while the demo runs.
					if game.showDemand {
						t.Fatal("Demand panel is open after the demo started")
					}
					game.showDemand = true
					if control := findButton(t, game.buttons(), "demo"); !control.disabled {
						t.Fatal("Start traffic demo is enabled while the demo runs")
					}
					continue
				}
				game.click(centerOfButton(findButton(t, game.buttons(), press.action)))
				if _, _, pending := game.client.View(); pending || game.pending || game.message != "" {
					t.Fatalf("press %d sent a command: pending %t message %q", index+1, pending, game.message)
				}
				if want := confirmations[press.action]; game.notice.text != want || game.notice.ticks != window {
					t.Fatalf("press %d notice = %q for %d ticks, want %q for %d ticks", index+1, game.notice.text, game.notice.ticks, want, window)
				}
			}
			if !slices.ContainsFunc(test.presses, func(value press) bool { return value.wantDemo }) {
				syncGame(t, game, func() bool { return true })
				if game.state.Simulation.Demo {
					t.Fatal("the demo runs, but no press sent the demo command")
				}
			}
		})
	}
}

// TestDemoHiddenOutsideExample connects to sessions of projects that are
// not the supplied example. The server does not start the demo in them,
// so the Demand panel does not show Start traffic demo.
func TestDemoHiddenOutsideExample(t *testing.T) {
	t.Parallel()
	fleet := project.Default()
	fleet.Fleet = fleet.Fleet[:1]
	network := project.Default()
	network.Network.Lanes[0].SpeedLimit++
	for name, config := range map[string]project.Config{"fleet": fleet, "network": network} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			game := sharedProjectGame(t, config)
			game.showDemand = true
			if game.state.DemoAvailable || slices.ContainsFunc(game.buttons(), func(control button) bool { return control.action == "demo" }) {
				t.Errorf("demo available %t: Start traffic demo shows", game.state.DemoAvailable)
			}
		})
	}
}

// TestDemoButtonFollowsPanel checks that Start traffic demo stays at the
// bottom right of the Demand panel in every window. The button keeps the
// same distance to the pod selector and to the right edge of the panel,
// and its label shows in full.
func TestDemoButtonFollowsPanel(t *testing.T) {
	t.Parallel()
	for _, layout := range controlLayouts {
		t.Run(layout.name, func(t *testing.T) {
			t.Parallel()
			game := controlTestGame(t, layout.input)
			game.showDemand = true
			controls := game.buttons()
			control := findButton(t, controls, "demo")
			selector := findButton(t, controls, "pod/"+fleetPodLabel(0, len(game.state.Simulation.Vehicles)))
			unit := game.layout.unit
			if gap := (selector.y - control.y - control.h) / unit; math.Abs(gap-6) > 1e-9 {
				t.Errorf("button ends %g units above the pod selector, want 6", gap)
			}
			if gap := (game.rightPanelArea().right - control.x - control.w) / unit; math.Abs(gap-16) > 1e-9 {
				t.Errorf("button ends %g units left of the panel edge, want 16", gap)
			}
			if got := game.fitButtonText(control.label, control.fontSize, control.w); got != control.label {
				t.Errorf("label %q shortened to %q", control.label, got)
			}
		})
	}
}
