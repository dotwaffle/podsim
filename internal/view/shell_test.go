package view

import (
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/dotwaffle/podsim/internal/remote"
	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

// fakeShell records the requests of the game and gives it notices. hidden
// is true while the fake shell page hides the game.
type fakeShell struct {
	requests []ShellRequest
	notices  chan ShellNotice
	hidden   bool
}

func newFakeShell() *fakeShell { return &fakeShell{notices: make(chan ShellNotice, 4)} }

func (s *fakeShell) Send(request ShellRequest)   { s.requests = append(s.requests, request) }
func (s *fakeShell) Notices() <-chan ShellNotice { return s.notices }
func (s *fakeShell) Hidden() bool                { return s.hidden }

// shellActions returns the actions of the header buttons that send a request
// to the shell page.
func shellActions(buttons []button) []string {
	var actions []string
	for _, control := range buttons {
		if strings.HasPrefix(control.action, "shell-") {
			actions = append(actions, control.action)
		}
	}
	return actions
}

// TestShellButtons checks that the header shows Edit scenario and Download
// debug state only in the shell page. Each button sends its request to the
// shell page and no command to the server. The buttons stay enabled while
// the connection is lost or a command waits for its reply.
func TestShellButtons(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name               string
		shell              bool
		connected, pending bool
		// want are the header buttons and the requests of a click on each.
		want []string
	}{
		{name: "desktop or outside the shell", connected: true},
		{name: "desktop without a connection"},
		{name: "shell", shell: true, connected: true, want: []string{shellDebugAction, shellEditorAction}},
		{name: "shell without a connection", shell: true, want: []string{shellDebugAction, shellEditorAction}},
		{name: "shell with a pending command", shell: true, connected: true, pending: true, want: []string{shellDebugAction, shellEditorAction}},
	}
	wantRequests := map[string]ShellRequest{shellEditorAction: ShowEditor, shellDebugAction: CaptureDebugState}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game := exampleTestGame(t)
			shell := newFakeShell()
			if test.shell {
				game.shell = shell
			}
			game.connected, game.pending = test.connected, test.pending
			if got := shellActions(game.buttons()); !slices.Equal(got, test.want) {
				t.Fatalf("shell buttons = %q, want %q", got, test.want)
			}
			for _, action := range test.want {
				control := findButton(t, game.buttons(), action)
				if control.disabled {
					t.Fatalf("button %q is disabled", action)
				}
				shell.requests = nil
				if game.click(centerOfButton(control)) {
					t.Errorf("click on %q reports a session command", action)
				}
				if want := []ShellRequest{wantRequests[action]}; !slices.Equal(shell.requests, want) {
					t.Errorf("click on %q sends %q, want %q", action, shell.requests, want)
				}
				if game.pending != test.pending {
					t.Errorf("click on %q sends a command to the server", action)
				}
			}
		})
	}
}

// TestShellButtonsFollowMapPanel checks that the shell buttons end at the
// right edge of the map panel in each window, also in wide windows.
func TestShellButtonsFollowMapPanel(t *testing.T) {
	t.Parallel()
	for _, layout := range controlLayouts {
		t.Run(layout.name, func(t *testing.T) {
			t.Parallel()
			game := controlTestGame(t, layout.input)
			editor := findButton(t, game.buttons(), shellEditorAction)
			debug := findButton(t, game.buttons(), shellDebugAction)
			if right := editor.x + editor.w; !closeTo(right, game.layout.right(772)) {
				t.Errorf("Edit scenario ends at %g, map panel ends at %g", right, game.layout.right(772))
			}
			if gap := (editor.x - debug.x - debug.w) / game.layout.unit; !closeTo(gap, 8) {
				t.Errorf("gap between the shell buttons is %g units, want 8", gap)
			}
		})
	}
}

// TestShellNotices checks how the game shows a status text from the shell
// page. A failure shows in the message line. Other text shows as a notice,
// but it does not replace a confirmation of a shared reset.
func TestShellNotices(t *testing.T) {
	t.Parallel()
	const done = "Debug state downloaded: tick 1200."
	const failed = "Capture failed. State HTTP 503. Try again."
	tests := []struct {
		name          string
		notice        ShellNotice
		confirm       bool
		wantNotice    string
		wantMessage   string
		wantHintValue string
	}{
		{name: "result", notice: ShellNotice{Text: done}, wantNotice: done, wantHintValue: done},
		{name: "failure", notice: ShellNotice{Text: failed, Error: true}, wantMessage: failed, wantHintValue: failed},
		{name: "result during a reset confirmation", notice: ShellNotice{Text: done}, confirm: true, wantNotice: resetConfirmNotice, wantHintValue: resetConfirmNotice},
		{name: "failure during a reset confirmation", notice: ShellNotice{Text: failed, Error: true}, confirm: true, wantNotice: resetConfirmNotice, wantMessage: failed, wantHintValue: resetConfirmNotice},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game := exampleTestGame(t)
			shell := newFakeShell()
			game.shell = shell
			if test.confirm {
				game.showNotice(resetConfirmAction, resetConfirmNotice)
			}
			shell.notices <- test.notice
			game.readShell()
			if game.notice.text != test.wantNotice || game.message != test.wantMessage {
				t.Errorf("notice %q and message %q, want %q and %q", game.notice.text, game.message, test.wantNotice, test.wantMessage)
			}
			if got := game.hintLine(game.state.Simulation, "hint").value; got != test.wantHintValue {
				t.Errorf("hint line = %q, want %q", got, test.wantHintValue)
			}
		})
	}
}

// TestShellFailureClears checks that a debug capture failure in the message
// line goes when a later capture succeeds, and when the Download debug state
// button starts a new capture. The button clears all messages, as other
// actions do. The hint line then shows the new result.
func TestShellFailureClears(t *testing.T) {
	t.Parallel()
	const done = "Debug state downloaded: tick 1200."
	const failed = "Capture failed. State HTTP 503. Try again."
	fail := func(t *testing.T, game *Game, shell *fakeShell) {
		t.Helper()
		shell.notices <- ShellNotice{Text: failed, Error: true}
		game.readShell()
		if got := game.hintLine(game.state.Simulation, "hint").value; got != failed {
			t.Fatalf("hint line after a failure = %q, want %q", got, failed)
		}
	}

	t.Run("capture succeeds", func(t *testing.T) {
		t.Parallel()
		game := exampleTestGame(t)
		shell := newFakeShell()
		game.shell = shell
		fail(t, game, shell)
		shell.notices <- ShellNotice{Text: done}
		game.readShell()
		if game.message != "" || game.notice.text != done {
			t.Errorf("message %q and notice %q, want no message and %q", game.message, game.notice.text, done)
		}
		if got := game.hintLine(game.state.Simulation, "hint").value; got != done {
			t.Errorf("hint line = %q, want %q", got, done)
		}
	})

	for _, test := range []struct {
		name    string
		message string
	}{
		{name: "new capture after a failure", message: failed},
		{name: "new capture after a command error", message: "command rejected"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game := exampleTestGame(t)
			shell := newFakeShell()
			game.shell = shell
			if test.message == failed {
				fail(t, game, shell)
			} else {
				game.message = test.message
			}
			shell.requests = nil
			game.click(centerOfButton(findButton(t, game.buttons(), shellDebugAction)))
			if want := []ShellRequest{CaptureDebugState}; !slices.Equal(shell.requests, want) {
				t.Errorf("click sends %q, want %q", shell.requests, want)
			}
			if game.message != "" {
				t.Errorf("message after the click = %q, want none", game.message)
			}
			shell.notices <- ShellNotice{Text: done}
			game.readShell()
			if got := game.hintLine(game.state.Simulation, "hint").value; got != done {
				t.Errorf("hint line after the new capture = %q, want %q", got, done)
			}
		})
	}
}

// TestShellResultKeepsOtherMessages checks that a capture result clears
// only a capture failure. A command error or the unsaved rewind warning stays
// in the message line. The tests call handleResult and then readShell, in
// the order of Update, so each result arrives in the same update as the
// capture result. A capture failure before the command result does not
// change this.
func TestShellResultKeepsOtherMessages(t *testing.T) {
	t.Parallel()
	const done = "Debug state downloaded: tick 1200."
	const failed = "Capture failed. State HTTP 503. Try again."
	tests := []struct {
		name        string
		failure     bool
		result      remote.Result
		wantMessage string
	}{
		{
			name:        "command error",
			result:      remote.Result{Command: session.Command{Action: "pause"}, Reply: session.Reply{Error: "command rejected"}},
			wantMessage: "command rejected",
		},
		{
			name:        "command error after a capture failure",
			failure:     true,
			result:      remote.Result{Command: session.Command{Action: "pause"}, Reply: session.Reply{Error: "command rejected"}},
			wantMessage: "command rejected",
		},
		{
			name:        "unsaved rewind",
			result:      remote.Result{Command: session.Command{Action: "rewind", Checkpoint: 2}, Reply: session.Reply{Generation: 2, StateSaved: new(false)}},
			wantMessage: rewindUnsavedMessage,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game := exampleTestGame(t)
			shell := newFakeShell()
			game.shell = shell
			if test.failure {
				shell.notices <- ShellNotice{Text: failed, Error: true}
				game.readShell()
			}
			game.handleResult(test.result)
			shell.notices <- ShellNotice{Text: done}
			game.readShell()
			if game.message != test.wantMessage {
				t.Errorf("message = %q, want %q", game.message, test.wantMessage)
			}
			if got := game.hintLine(game.state.Simulation, "hint").value; got != test.wantMessage {
				t.Errorf("hint line = %q, want %q", got, test.wantMessage)
			}
		})
	}
}

// TestHeaderFitsShellButtons checks that the title, the longest run status
// and the counts do not overlap the header buttons, and that all of them
// stay in the header above the panels.
func TestHeaderFitsShellButtons(t *testing.T) {
	t.Parallel()
	for _, layout := range controlLayouts {
		t.Run(layout.name, func(t *testing.T) {
			t.Parallel()
			game := controlTestGame(t, layout.input)
			game.state.Epoch, game.state.Speed = "a", 8
			game.state.Simulation.Paused = true
			game.state.Simulation.Tick = 99999999 * sim.TicksPerSecond / 10
			game.state.Simulation.Completed = 9999999
			game.state.Simulation.Vehicles = make([]sim.Vehicle, 9999)
			game.state.Checkpoints = []session.Checkpoint{{ID: 1, Tick: 999999 * sim.TicksPerSecond / 10, RestoresProject: true}}
			bottom := game.layout.y(headerHeight)
			var header []area
			for _, value := range game.headerLabels() {
				header = append(header, game.labelArea(value))
			}
			for _, control := range game.buttons() {
				if control.y < bottom {
					header = append(header, buttonArea(control))
				}
			}
			// The title, the run status, the counts, Save point, Rewind and
			// the two shell buttons.
			if len(header) != 7 {
				t.Fatalf("header has %d items, want 7", len(header))
			}
			for i, item := range header {
				if item.top < 0 || item.bottom > bottom || item.left < 0 || item.right > float64(game.layout.width) {
					t.Errorf("header item %+v is not in the header above %g", item, bottom)
				}
				for _, other := range header[i+1:] {
					if item.overlaps(other) {
						t.Errorf("header item %+v overlaps %+v", item, other)
					}
				}
			}
		})
	}
}

// TestDrawSkipsWhileHidden checks that Draw does not draw while the shell
// page hides the game, and that it draws again in the first frame after the
// game shows. The test calls Draw one time in each state.
func TestDrawSkipsWhileHidden(t *testing.T) {
	t.Parallel()
	game := exampleTestGame(t)
	shell := newFakeShell()
	game.shell = shell
	screen := ebiten.NewImage(game.layout.width, game.layout.height)
	defer screen.Deallocate()
	for _, step := range []struct {
		name       string
		hidden     bool
		wantFrames int
	}{
		{name: "hidden", hidden: true, wantFrames: 0},
		{name: "shown", wantFrames: 1},
	} {
		shell.hidden = step.hidden
		game.readShell()
		game.Draw(screen)
		if game.drawnFrames != step.wantFrames {
			t.Fatalf("%s: Draw drew %d frames, want %d", step.name, game.drawnFrames, step.wantFrames)
		}
	}
}

// TestHiddenGameReadsState checks that a hidden game still reads the shared
// state and the shell notices in Update, so that it is current when it
// shows again. The game also tells the shell page once that it is ready.
func TestHiddenGameReadsState(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(buildTestHandler(t, "build-a"))
	t.Cleanup(server.Close)
	shell := newFakeShell()
	shell.hidden = true
	game, err := New(t.Context(), server.URL, WithShell(shell))
	if err != nil {
		t.Fatalf("create game: %v", err)
	}
	shell.notices <- ShellNotice{Text: "Debug state downloaded: tick 1."}
	deadline := time.Now().Add(5 * time.Second)
	for game.state.Epoch == "" && time.Now().Before(deadline) {
		if err := game.Update(); err != nil {
			t.Fatalf("update: %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if game.state.Epoch == "" || !game.hidden {
		t.Fatalf("hidden game has epoch %q and hidden %t, want a state and true", game.state.Epoch, game.hidden)
	}
	if game.notice.text != "Debug state downloaded: tick 1." {
		t.Errorf("notice = %q, want the shell notice", game.notice.text)
	}
	if want := []ShellRequest{ShellReady}; !slices.Equal(shell.requests, want) {
		t.Errorf("requests = %q, want %q", shell.requests, want)
	}
}

// TestShellReady checks that the game sends ShellReady to the shell page
// once, in its first update, also while the shell page hides the game.
// Without a shell, the game sends nothing.
func TestShellReady(t *testing.T) {
	t.Parallel()
	if ShellVersion != 1 {
		t.Errorf("ShellVersion = %d, want 1", ShellVersion)
	}
	for _, hidden := range []bool{false, true} {
		game := exampleTestGame(t)
		shell := newFakeShell()
		shell.hidden = hidden
		game.shell = shell
		for range 3 {
			game.readShell()
		}
		if want := []ShellRequest{ShellReady}; !slices.Equal(shell.requests, want) {
			t.Errorf("hidden %t: requests = %q, want %q", hidden, shell.requests, want)
		}
	}
	game := exampleTestGame(t)
	game.readShell()
	if game.shellReadySent {
		t.Error("the game without a shell sent ShellReady")
	}
}
