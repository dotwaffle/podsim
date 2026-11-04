package view

import (
	"fmt"

	"github.com/dotwaffle/podsim/internal/session"
)

// startsGeneration reports whether a command with action starts a new
// generation of the shared session. A reset, a demo, a rewind, and a
// project apply do this.
func startsGeneration(action string) bool {
	switch action {
	case "reset", "demo", "rewind", "project":
		return true
	default:
		return false
	}
}

const (
	// sessionChangeAction is the notice action of restartNotice and
	// otherBrowserNotice.
	sessionChangeAction = "session-change"
	// restartNotice tells the user that the server restarted. The server
	// keeps save points in memory only. The -state option does not save
	// them, so each restart clears them.
	restartNotice = "Server restarted. Save points cleared."
	// otherBrowserNotice tells the user that another browser started a new
	// generation of the shared session.
	otherBrowserNotice = "Another browser reset or rewound the session."
)

// sessionChangeInput holds the inputs of sessionChangeNotice.
type sessionChangeInput struct {
	// previous is the state before the update, and current is the state
	// after it.
	previous, current session.State
	// inFlight is true while a command of this game that starts a new
	// generation waits for its reply.
	inFlight bool
	// ownEpoch and ownGeneration come from the last accepted reply to such
	// a command. ownStart is the server start ID of the state when the game
	// sent that command.
	ownEpoch      string
	ownGeneration uint64
	ownStart      string
}

// sessionChangeNotice returns the notice for a change from the previous
// state to the current state that this game did not cause. It returns an
// empty string when there is no such change, and for the first state frame.
//
// Each server process sends its own start ID, and the stream refuses a frame
// without one. A new start ID shows that the server restarted, also when the
// epoch stays the same and the game did not get the first frames after the
// restart. A new epoch also shows a restart.
//
// Other changes of the generation come from a reset, a demo, a rewind, or a
// project apply. They show otherBrowserNotice, but not while a command of
// this game that starts a new generation waits for its reply. They also
// show no notice when the last reply to such a command gave the current
// epoch, and the current generation or a later one, and the state had the
// current server start ID when the game sent that command. A restart can
// keep the epoch and lower the generation, so a reply from an earlier server
// process does not apply.
func sessionChangeNotice(input sessionChangeInput) string {
	previous, current := input.previous, input.current
	switch {
	case previous.Epoch == "":
		return ""
	case current.ServerStart != previous.ServerStart:
		return restartNotice
	case current.Epoch != previous.Epoch:
		return restartNotice
	case current.Generation == previous.Generation:
		return ""
	case input.inFlight:
		return ""
	case current.Epoch == input.ownEpoch && current.ServerStart == input.ownStart && current.Generation <= input.ownGeneration:
		return ""
	default:
		return otherBrowserNotice
	}
}

// announceSessionChange shows a notice when the state changed from previous
// in a way that this game did not cause. The notice replaces the old
// message and notice, also a confirmation, because they are about
// the session before the change.
func (g *Game) announceSessionChange(previous session.State) {
	notice := sessionChangeNotice(sessionChangeInput{
		previous: previous, current: g.state,
		inFlight: g.pending && startsGeneration(g.commands.sentAction),
		ownEpoch: g.commands.ownEpoch, ownGeneration: g.commands.ownGeneration, ownStart: g.commands.ownStart,
	})
	if notice == "" {
		return
	}
	g.message = ""
	g.notice.show(sessionChangeAction, notice)
}

// speedReductionNotice returns the notice for a server speed reduction in
// current that previous does not have. A first connection or a new process
// does not repeat a historical reduction, so it returns an empty string.
func speedReductionNotice(previous, current session.State) string {
	reduction := current.SpeedReduction
	if previous.Epoch == "" || previous.ServerStart != current.ServerStart ||
		previous.Epoch != current.Epoch || reduction.Sequence <= previous.SpeedReduction.Sequence {
		return ""
	}
	return fmt.Sprintf("Speed reduced from %dx to %dx: the simulation could not keep up.", reduction.From, reduction.To)
}
