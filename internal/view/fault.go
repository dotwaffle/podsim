package view

import (
	"slices"
	"strings"

	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

// The actions of the fault button of the pod inspector. The pod ID or the
// fault ID follows the prefix, so a click sends the target that the button
// showed.
const (
	faultActionPrefix      = "fault/"
	clearFaultActionPrefix = "clear-fault/"
)

// podFault returns the active pod fault of the pod with podID. Debris has
// no pod, so it never matches.
func podFault(faults sim.FaultsView, podID string) (sim.FaultView, bool) {
	i := slices.IndexFunc(faults.Active, func(fault sim.FaultView) bool {
		return fault.Kind == sim.FaultKindPod && fault.PodID == podID
	})
	if i < 0 {
		return sim.FaultView{}, false
	}
	return faults.Active[i], true
}

// faultButton returns the fault control of the pod inspector (section 12.6
// of the incident suspension contract). It shows only with the fault
// marker and only while the inspector shows the selected pod. On a pod
// without a fault, it reads Fault and starts a fault without a duration.
// On a faulted pod, it reads Clear fault and clears that fault. The view
// does not check the other preconditions of the commands: the server
// refuses a command that fails one, and the message line shows the error.
// The button is right of the activity label, which ends before it.
func (g *Game) faultButton(state sim.Snapshot) (button, bool) {
	if state.FaultContract == "" || g.showOrders || g.showDemand {
		return button{}, false
	}
	vehicle, ok := selectedVehicle(state, g.selected)
	if !ok {
		return button{}, false
	}
	control := button{x: 986, y: 112, w: 74, h: 24, label: "Fault", action: faultActionPrefix + vehicle.Pod.ID, fontSize: 11, disabled: !g.connected || g.pending}
	if fault, ok := podFault(state.Faults, vehicle.Pod.ID); ok {
		control.label, control.action = "Clear fault", clearFaultActionPrefix+fault.ID
	}
	return control, true
}

// faultCommand returns the command of a fault button action. It reports
// false for any other action.
func faultCommand(action string) (session.Command, bool) {
	if podID, ok := strings.CutPrefix(action, faultActionPrefix); ok {
		return session.Command{Action: "fault", PodID: podID}, true
	}
	if faultID, ok := strings.CutPrefix(action, clearFaultActionPrefix); ok {
		return session.Command{Action: "clearFault", FaultID: faultID}, true
	}
	return session.Command{}, false
}

// frameControl is a command button of the pod inspector that a click
// takes from the last drawn frame. match finds the button, control is its
// copy from the last frame, and shown reports whether that frame had one.
type frameControl struct {
	match   func(button) bool
	control *button
	shown   *bool
}

// frameControls returns the fault button and the emergency button of the
// last drawn frame.
func (g *Game) frameControls() []frameControl {
	return []frameControl{
		{match: isFaultButton, control: &g.shownFault, shown: &g.faultShown},
		{match: isEmergencyButton, control: &g.shownEmergency, shown: &g.emergencyShown},
	}
}

// frameButtons returns the buttons of a new frame. It keeps the fault
// button and the emergency button of the frame for clickButtons.
func (g *Game) frameButtons() []button {
	buttons := g.buttons()
	for _, frame := range g.frameControls() {
		i := slices.IndexFunc(buttons, frame.match)
		*frame.shown = i >= 0
		*frame.control = button{}
		if i >= 0 {
			*frame.control = buttons[i]
		}
	}
	return buttons
}

// clickButtons returns the buttons that a click can press. The state can
// change after the last frame, before the click. The fault button and the
// emergency button are thus the ones of the last frame, with their
// positions and their targets: a fault that clears, an emergency that
// starts, or a pod that moves in the fleet must not change the command of
// a button that the user saw. Only the connection and a waiting command,
// which make each command button disabled, come from the current state.
func (g *Game) clickButtons() []button {
	buttons := g.buttons()
	for _, frame := range g.frameControls() {
		buttons = slices.DeleteFunc(buttons, frame.match)
		if *frame.shown {
			control := *frame.control
			control.disabled = !g.connected || g.pending
			buttons = append(buttons, control)
		}
	}
	return buttons
}

// isFaultButton reports whether control is the fault button.
func isFaultButton(control button) bool {
	_, ok := faultCommand(control.action)
	return ok
}
