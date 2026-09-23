package view

import (
	"cmp"
	"testing"

	"github.com/dotwaffle/podsim/internal/remote"
	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

// TestResetAsksAgain presses Reset in a game that is connected to a session
// server. Only a second press within 3 s sends the reset command, and only
// while the hint line shows the confirmation. The test counts game ticks,
// not wall time.
func TestResetAsksAgain(t *testing.T) {
	t.Parallel()
	const (
		window       = 3 * sim.TicksPerSecond
		staleMessage = "save point #1 is no longer available"
	)
	type press struct {
		// ticks is the number of game ticks before the press.
		ticks int
		// notice replaces the notice before the press when it is set.
		notice string
		// message sets the message line before the press when it is set.
		message string
		// wantReset is true when the press sends the reset command.
		wantReset bool
	}
	tests := []struct {
		name    string
		presses []press
	}{
		{name: "one press", presses: []press{{}}},
		{name: "second press at once", presses: []press{{}, {wantReset: true}}},
		{name: "second press in the last tick", presses: []press{{}, {ticks: window - 1, wantReset: true}}},
		{name: "second press after the window", presses: []press{{}, {ticks: window}}},
		{name: "third press after the window", presses: []press{{}, {ticks: window}, {ticks: 1, wantReset: true}}},
		{name: "press after a reset", presses: []press{{}, {wantReset: true}, {}}},
		{name: "other notice", presses: []press{{}, {notice: "Save point #1 saved."}}},
		{name: "press clears an old message", presses: []press{{message: staleMessage}}},
		{name: "message after the first press", presses: []press{{}, {message: staleMessage, wantReset: true}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game := sharedTestGame(t)
			clickCommand(t, game, "speed")
			syncGame(t, game, func() bool { return game.state.Speed == 2 })
			wantSpeed := 2
			for index, press := range test.presses {
				for range press.ticks {
					game.tickNotice()
				}
				if press.notice != "" {
					game.showNotice("checkpoint", press.notice)
				}
				if press.message != "" {
					game.message = press.message
				}
				if press.wantReset {
					// The user must see the confirmation before the press
					// that resets the session.
					if got := game.hintLine(game.state.Simulation, ""); got.value != resetConfirmNotice {
						t.Fatalf("hint line before press %d = %q, want %q", index+1, got.value, resetConfirmNotice)
					}
					result := clickCommand(t, game, "reset")
					if result.Command.Action != "reset" || game.notice != resetNotice {
						t.Fatalf("press %d sent %q with notice %q, want reset with notice %q", index+1, result.Command.Action, game.notice, resetNotice)
					}
					// A reset returns to 1x playback.
					wantSpeed = 1
					syncGame(t, game, func() bool { return game.state.Speed == wantSpeed })
					continue
				}
				game.click(centerOfButton(findButton(t, game.buttons(), "reset")))
				if _, _, pending := game.client.View(); pending || game.pending || game.message != "" {
					t.Fatalf("press %d sent a command: pending %t message %q", index+1, pending, game.message)
				}
				if game.notice != resetConfirmNotice || game.noticeTicks != window {
					t.Fatalf("press %d notice = %q for %d ticks, want %q for %d ticks", index+1, game.notice, game.noticeTicks, resetConfirmNotice, window)
				}
			}
			syncGame(t, game, func() bool { return game.state.Speed == wantSpeed })
		})
	}
}

// TestResetLabelFits checks that the Reset button shows its full shortcut
// in each control layout.
func TestResetLabelFits(t *testing.T) {
	t.Parallel()
	for _, layout := range controlLayouts {
		t.Run(layout.name, func(t *testing.T) {
			t.Parallel()
			game := controlTestGame(t, layout.input)
			control := findButton(t, game.buttons(), "reset")
			if control.label != "Reset [Shift+R]" {
				t.Fatalf("label = %q, want %q", control.label, "Reset [Shift+R]")
			}
			if got := game.fitButtonText(control.label, cmp.Or(control.fontSize, 14), control.w); got != control.label {
				t.Fatalf("label %q shortened to %q in width %g", control.label, got, control.w)
			}
		})
	}
}

// TestHintLine checks which text the line below the journey controls
// shows when more than one text is set.
func TestHintLine(t *testing.T) {
	t.Parallel()
	const (
		hint      = "Choose pickup and destination."
		notice    = "Save point #1 saved."
		demoError = "Traffic demo stopped: no route"
		message   = "waiting for the server connection"
	)
	tests := []struct {
		name                  string
		message, noticeAction string
		notice, demoError     string
		wantValue             string
		wantColor             uint32
	}{
		{name: "hint", wantValue: hint, wantColor: muted},
		{name: "notice", noticeAction: "checkpoint", notice: notice, wantValue: notice, wantColor: accent},
		{name: "demo error over notice", noticeAction: "checkpoint", notice: notice, demoError: demoError, wantValue: demoError, wantColor: amber},
		{name: "reset confirmation over demo error", noticeAction: resetConfirmAction, notice: resetConfirmNotice, demoError: demoError, wantValue: resetConfirmNotice, wantColor: accent},
		{name: "reset notice under demo error", noticeAction: "reset", notice: resetNotice, demoError: demoError, wantValue: demoError, wantColor: amber},
		{name: "message over demo error", message: message, noticeAction: "checkpoint", notice: notice, demoError: demoError, wantValue: message, wantColor: amber},
		{name: "reset confirmation over message", message: message, noticeAction: resetConfirmAction, notice: resetConfirmNotice, demoError: demoError, wantValue: resetConfirmNotice, wantColor: accent},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game := journeyTestGame(t, 2)
			game.message = test.message
			game.notice, game.noticeAction, game.noticeTicks = test.notice, test.noticeAction, 1
			got := game.hintLine(sim.Snapshot{DemoError: test.demoError}, hint)
			if got.value != test.wantValue || got.color != test.wantColor {
				t.Errorf("hint line = %q color %#06x, want %q color %#06x", got.value, got.color, test.wantValue, test.wantColor)
			}
		})
	}
}

// TestResetFailedSend presses Reset twice while another command is on the
// way. The second press cannot send the reset command. It shows the error
// and ends the confirmation, so the next press asks again.
func TestResetFailedSend(t *testing.T) {
	t.Parallel()
	game := sharedTestGame(t)
	game.reset()
	// Submit on the client directly, so the confirmation stays.
	if err := game.client.Submit(session.Command{Action: "speed", Speed: 2}); err != nil {
		t.Fatalf("submit speed: %v", err)
	}
	game.reset()
	const want = "waiting for the previous command"
	if game.message != want || game.noticeAction != "" || game.noticeTicks != 0 {
		t.Fatalf("after second press: message %q, notice action %q for %d ticks, want message %q and no notice", game.message, game.noticeAction, game.noticeTicks, want)
	}
	if got := game.hintLine(game.state.Simulation, ""); got.value != want {
		t.Fatalf("hint line = %q, want %q", got.value, want)
	}
	syncGame(t, game, func() bool { return game.state.Speed == 2 })
	game.reset()
	if _, _, pending := game.client.View(); pending || game.pending || game.noticeAction != resetConfirmAction {
		t.Fatalf("third press: pending %t, notice action %q, want no command and the confirmation", pending, game.noticeAction)
	}
	syncGame(t, game, func() bool { return game.state.Speed == 2 })
}

// TestResetResultNotice checks that only an accepted reset shows "Session
// reset.". A demo result uses the same code path and shows no notice.
func TestResetResultNotice(t *testing.T) {
	t.Parallel()
	tests := []struct {
		action                 string
		wantNotice, wantAction string
	}{
		{action: "reset", wantNotice: resetNotice, wantAction: "reset"},
		{action: "demo"},
	}
	for _, test := range tests {
		t.Run(test.action, func(t *testing.T) {
			t.Parallel()
			game := journeyTestGame(t, 2)
			game.showNotice(resetConfirmAction, resetConfirmNotice)
			game.handleResult(remote.Result{Command: session.Command{Action: test.action}})
			if game.notice != test.wantNotice || game.noticeAction != test.wantAction {
				t.Fatalf("notice %q action %q, want notice %q action %q", game.notice, game.noticeAction, test.wantNotice, test.wantAction)
			}
		})
	}
}
