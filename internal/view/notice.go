package view

import (
	"time"

	"github.com/dotwaffle/podsim/internal/sim"
)

// noticeDuration is the time in game ticks that a notice shows. The game
// runs sim.TicksPerSecond ticks each second, so a notice shows for 3 s. The
// text of each confirmation gives this time.
const noticeDuration = 3 * sim.TicksPerSecond

// noticeState is the notice in the hint line. A notice can also be a
// confirmation. See confirmSubmit.
type noticeState struct {
	// text is the text of the notice. action names the command or the
	// prompt that caused the notice. ticks is the time in game ticks until
	// the notice ends. See show and tick.
	text   string
	action string
	ticks  int
	// confirmDeadline is the wall-clock time at which the confirmation
	// that shows ends. See confirmSubmit.
	confirmDeadline time.Time
}

// show shows value in the hint line for noticeDuration ticks. action names
// the command or the prompt that caused the notice.
func (n *noticeState) show(action, value string) {
	n.text, n.action, n.ticks = value, action, noticeDuration
}

// clear removes the notice from the hint line.
func (n *noticeState) clear() {
	n.text, n.action, n.ticks = "", "", 0
}

// tick counts down the notice time by one tick. It clears the notice when
// the time ends.
func (n *noticeState) tick() {
	if n.ticks == 0 {
		return
	}
	n.ticks--
	if n.ticks == 0 {
		n.text, n.action = "", ""
	}
}

// confirmation is a notice that asks for a second press of a control. It
// guards a command that resets the shared session.
type confirmation struct {
	// action is the notice action of the confirmation, and notice is its
	// text.
	action, notice string
}

// isConfirmation reports whether action is the notice action of a
// confirmation.
func isConfirmation(action string) bool {
	return action == resetConfirmAction || action == demoConfirmAction
}

// confirmWindow is the wall-clock time in which a second press sends the
// command of a confirmation. It is the time of noticeDuration.
const confirmWindow = 3 * time.Second

// takeConfirmation reports whether prompt shows and the confirmation window
// is open at the time now. If so, it removes prompt.
func (n *noticeState) takeConfirmation(prompt confirmation, now time.Time) bool {
	if n.action != prompt.action || n.ticks <= 0 || !now.Before(n.confirmDeadline) {
		return false
	}
	n.clear()
	n.confirmDeadline = time.Time{}
	return true
}

// ask shows prompt and starts its confirmation window at the time now.
func (n *noticeState) ask(prompt confirmation, now time.Time) {
	n.show(prompt.action, prompt.notice)
	n.confirmDeadline = now.Add(confirmWindow)
}
