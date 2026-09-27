package view

// ShellRequest is a request from the game to the shell page.
type ShellRequest string

const (
	// ShowEditor asks the shell page to show the scenario editor.
	ShowEditor ShellRequest = "editor"
	// CaptureDebugState asks the shell page to download a debug capture of
	// the server state.
	CaptureDebugState ShellRequest = "debug"
	// ShellReady tells the shell page that the game runs and that it uses
	// the shell messages of ShellVersion. The game sends it once, in its
	// first update.
	ShellReady ShellRequest = "ready"
)

// ShellVersion is the version of the messages between the game and the
// shell page. The shell page shows its own Edit scenario and Download debug
// state controls until the game in its frame sends ShellReady with the
// version that the shell page uses. Change it when a message changes, so a
// shell page and a game of different builds do not rely on each other. See
// SHELL_VERSION in web/shell.js.
const ShellVersion = 1

// ShellNotice is a status text that the shell page sends to the game, such
// as the result of a debug capture. Error is true when the text tells of a
// failure.
type ShellNotice struct {
	Text  string
	Error bool
}

// Shell is the shell page index.html in the browser. The shell page shows
// the game and the scenario editor in two frames. The browser entry point
// sets it only when game.html runs in a frame of the shell page. The
// desktop client and a game page outside the shell page have no shell.
type Shell interface {
	// Send asks the shell page to do request.
	Send(request ShellRequest)
	// Hidden reports if the shell page hides the game frame. While the
	// frame is hidden, the game does not draw.
	Hidden() bool
	// Notices gives the status texts that the shell page sends to the game.
	Notices() <-chan ShellNotice
}

// WithShell sets the shell page of the game. With a shell, the header shows
// Edit scenario and Download debug state. With a nil shell, the header does
// not show them.
func WithShell(shell Shell) Option {
	return func(g *Game) { g.shell = shell }
}

// Actions of the header buttons that send a request to the shell page.
const (
	shellEditorAction = "shell-editor"
	shellDebugAction  = "shell-debug"
)

// shellButtons returns the header buttons that send a request to the shell
// page. Without a shell, there are none. The buttons end at the right edge
// of the map panel, and they move with it in a wide window. They send no
// command to the server, so they stay enabled without a connection.
func (g *Game) shellButtons() []button {
	if g.shell == nil {
		return nil
	}
	return []button{
		{x: 528, y: headerButtonTop, w: 140, h: headerButtonHeight, label: "Download debug state", action: shellDebugAction, fontSize: 12},
		{x: 676, y: headerButtonTop, w: 96, h: headerButtonHeight, label: "Edit scenario", action: shellEditorAction, fontSize: 12},
	}
}

// readShell tells the shell page that the game is ready, the first time
// only. Then it reads if the shell page hides the game, and a status text
// from the shell page. A text that tells of a failure shows in the message
// line, so it stays until the next action or the next capture result. Other
// text shows as a notice and clears the message only while the message is
// the last failure from the shell page. Thus a capture result does not hide
// a command error or a warning. A notice does not replace a confirmation of
// Reset or Start traffic demo, because a second press then sends the
// command.
func (g *Game) readShell() {
	if g.shell == nil {
		return
	}
	if !g.shellReadySent {
		g.shell.Send(ShellReady)
		g.shellReadySent = true
	}
	g.hidden = g.shell.Hidden()
	select {
	case notice := <-g.shell.Notices():
		if notice.Error {
			g.message, g.shellFailure = notice.Text, notice.Text
			return
		}
		if g.message == g.shellFailure {
			g.message = ""
		}
		g.shellFailure = ""
		if !isConfirmation(g.noticeAction) {
			g.showNotice(shellNoticeAction, notice.Text)
		}
	default:
	}
}

// shellNoticeAction is the notice action of a status text from the shell
// page.
const shellNoticeAction = "shell"
