package view

import (
	"fmt"
	"slices"

	"github.com/dotwaffle/podsim/internal/remote"
	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

// rewindUnsavedMessage tells the user that the server applied a rewind but
// could not save the session state before its reply.
const rewindUnsavedMessage = "The server could not save the session state. A server crash can undo this rewind."

// handleResult shows the outcome of one command. Errors go to the message
// line. Some accepted commands show a notice.
func (g *Game) handleResult(result remote.Result) {
	g.message = ""
	if result.Err == nil && result.Reply.Error == "" && startsGeneration(result.Command.Action) {
		g.commands.ownEpoch, g.commands.ownGeneration, g.commands.ownStart = result.Reply.Epoch, result.Reply.Generation, g.commands.sentStart
	}
	switch {
	case result.Err != nil:
		g.message = result.Err.Error()
	case result.Reply.Error != "":
		g.message = result.Reply.Error
	case result.Command.Action == "trip":
		g.showOrders, g.showDemand = true, false
		g.commands.acceptedOrigin, g.commands.acceptedDestination = result.Command.Origin, result.Command.Destination
		from, _ := g.network.Station(result.Command.Origin)
		to, _ := g.network.Station(result.Command.Destination)
		g.notice.show("trip", fmt.Sprintf("Order #%d accepted: %s > %s. See Orders for status.", result.Reply.OrderID, from.Name, to.Name))
	case result.Command.Action == "checkpoint":
		g.commands.savedEpoch, g.commands.savedRevision, g.commands.savedStart = result.Reply.Epoch, result.Reply.Revision, g.commands.sentStart
		g.notice.show("checkpoint", fmt.Sprintf("Save point #%d saved.", result.Reply.Checkpoint))
	case result.Command.Action == "rewind":
		// Panels and selection stay as they are. readRemote clamps the
		// selection, so it stays valid after a project restore.
		g.notice.show("rewind", rewindNotice(g.state.Checkpoints, result))
		// The amber message line shows the warning over the notice.
		if saved := result.Reply.StateSaved; saved != nil && !*saved {
			g.message = rewindUnsavedMessage
		}
	case result.Command.Action == "reset" || result.Command.Action == "demo":
		g.showOrders, g.showDemand = false, false
		g.notice.clear()
		g.selected, g.podPage = 0, 0
		g.normalizeSelection()
		if result.Command.Action == "reset" {
			g.notice.show("reset", resetNotice)
		}
	}
}

// rewindNotice describes an accepted rewind. It gives the time of the save
// point when checkpoints still lists it. When the reply shows that the rewind
// restored a different project, the notice says so.
func rewindNotice(checkpoints []session.Checkpoint, result remote.Result) string {
	id := result.Command.Checkpoint
	notice := fmt.Sprintf("Rewound to save point #%d. Paused.", id)
	index := slices.IndexFunc(checkpoints, func(entry session.Checkpoint) bool { return entry.ID == id })
	if index >= 0 {
		seconds := float64(checkpoints[index].Tick) / sim.TicksPerSecond
		notice = fmt.Sprintf("Rewound to save point #%d (%.1f s). Paused.", id, seconds)
	}
	if result.Reply.ProjectRestored {
		notice += " Project settings restored."
	}
	return notice
}

// submit sends command to the server. When the client rejects the command,
// the error shows in the hint line. A sent command clears the message and
// the notice. The line below the panels shows that the command waits for the
// server. The hint line does not show this wait, because a wait is not an
// error.
func (g *Game) submit(command session.Command) {
	if err := g.client.Submit(command); err != nil {
		g.message = err.Error()
		return
	}
	g.pending, g.commands.sentAction, g.commands.sentStart = true, command.Action, g.state.ServerStart
	g.message = ""
	g.notice.clear()
}
