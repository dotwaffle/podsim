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
